package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate/entities/models"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Entry struct {
	ID    string `xml:"id"`
	Title string `xml:"title"`
}

type ArxivResponse struct {
	Entries []Entry `xml:"entry"`
}

type Reference struct {
	Title     string    `json:"title"`
	Abstract  string    `json:"abstract"`
	Authors   []string  `json:"authors"`
	Embedding []float64 `json:"embedding"`
}

type Paper struct {
	Title      string      `json:"title"`
	Abstract   string      `json:"abstract"`
	Authors    []string    `json:"authors"`
	References []Reference `json:"references"`
	Embedding  []float64   `json:"embedding"`
}

func getSession() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
	}
}

func fetchPapersFromArxiv(category, startDate, endDate string, batchSize, maxPapers int) ([]string, error) {
	baseURL := "http://export.arxiv.org/api/query"
	var arxivIDs []string

	start := 0
	for len(arxivIDs) < maxPapers {
		params := fmt.Sprintf("search_query=cat:%s+AND+submittedDate:[%s+TO+%s]&start=%d&max_results=%d", category, startDate, endDate, start, batchSize)
		url := fmt.Sprintf("%s?%s", baseURL, params)

		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("error fetching from Arxiv: %v", err)
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Printf("Error closing response body: %v", err)
			}
		}(resp.Body)

		var arxivResponse ArxivResponse
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading Arxiv response: %v", err)
		}

		// Use xml.Unmarshal instead of json.Unmarshal
		if err := xml.Unmarshal(body, &arxivResponse); err != nil {
			return nil, fmt.Errorf("error decoding Arxiv XML: %v", err)
		}

		for _, entry := range arxivResponse.Entries {
			parts := strings.Split(entry.ID, "/")
			if len(parts) > 0 {
				arxivId := parts[len(parts)-1]
				arxivId = strings.Split(arxivId, "v")[0]
				arxivIDs = append(arxivIDs, arxivId)
			}
		}

		if len(arxivResponse.Entries) < batchSize {
			break
		}
		start += batchSize
	}

	return arxivIDs, nil
}

func fetchBatchFromSemanticScholar(arxivIDs []string, batchSize int) (map[string]Paper, error) {
	const baseURL = "https://api.semanticscholar.org/graph/v1/paper/batch"
	const fields = "title,abstract,authors,references.title,references.abstract,references.authors"
	results := make(map[string]Paper)
	client := getSession()

	for i := 0; i < len(arxivIDs); i += batchSize {
		batch := arxivIDs[i:min(i+batchSize, len(arxivIDs))]

		payload := map[string]interface{}{
			"ids": formatIDs(batch),
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("error marshalling payload: %v", err)
		}

		req, err := http.NewRequest("POST", baseURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			return nil, fmt.Errorf("error creating request: %v", err)
		}

		req.Header.Set("Content-Type", "application/json")
		query := req.URL.Query()
		query.Add("fields", fields)
		req.URL.RawQuery = query.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error sending request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Error fetching batch: %s", string(body))
			continue
		}

		var batchResponse []map[string]interface{}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		if err := json.Unmarshal(body, &batchResponse); err != nil {
			return nil, fmt.Errorf("error unmarshalling response: %v", err)
		}

		for j, paper := range batchResponse {
			arxivID := batch[j]
			results[arxivID] = processPaper(paper)
		}
	}

	return results, nil
}

func processPaper(raw map[string]interface{}) Paper {
	var authors []string
	if authorsRaw, ok := raw["authors"].([]interface{}); ok {
		for _, a := range authorsRaw {
			if author, ok := a.(map[string]interface{}); ok {
				if name, ok := author["name"].(string); ok {
					authors = append(authors, name)
				}
			}
		}
	}

	var references []Reference
	references = processReference(raw, references)

	title := ""
	if t, ok := raw["title"].(string); ok {
		title = t
	}
	abstract := ""
	if a, ok := raw["abstract"].(string); ok {
		abstract = a
	}

	return Paper{
		Title:      title,
		Abstract:   abstract,
		Authors:    authors,
		References: references,
	}
}

func processReference(raw map[string]interface{}, references []Reference) []Reference {
	if referencesRaw, ok := raw["references"].([]interface{}); ok {
		for _, ref := range referencesRaw {
			if refMap, ok := ref.(map[string]interface{}); ok {
				title := ""
				if t, ok := refMap["title"].(string); ok {
					title = t
				}
				abstract := ""
				if a, ok := refMap["abstract"].(string); ok {
					abstract = a
				}

				var refAuthors []string
				if authorsField, ok := refMap["authors"].([]interface{}); ok {
					for _, author := range authorsField {
						if authorMap, ok := author.(map[string]interface{}); ok {
							if name, ok := authorMap["name"].(string); ok {
								refAuthors = append(refAuthors, name)
							}
						}
					}
				}

				references = append(references, Reference{
					Title:    title,
					Abstract: abstract,
					Authors:  refAuthors,
				})
			}
		}
	}
	return references
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatIDs(ids []string) []string {
	formatted := make([]string, len(ids))
	for i, id := range ids {
		formatted[i] = fmt.Sprintf("ARXIV:%s", id)
	}
	return formatted
}

func calculateEmbeddingsBatch(texts []string, total int) ([][]float64, error) {
	const batchSize = 10
	embeddings := [][]float64{}

	progress := 0

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		payload := map[string]interface{}{
			"texts": batch,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("error marshalling payload: %v", err)
		}

		resp, err := http.Post("http://localhost:5001/calculate_embeddings", "application/json", bytes.NewBuffer(payloadBytes))
		if err != nil {
			return nil, fmt.Errorf("error sending request to embedding service: %v", err)
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Printf("Error closing response body: %v", err)
			}
		}(resp.Body)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("embedding service returned error: %s", string(body))
		}

		var response map[string]interface{}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}

		err = json.Unmarshal(body, &response)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling response: %v", err)
		}

		embeddingsRaw, ok := response["embeddings"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid response format from embedding service")
		}

		for _, embeddingRaw := range embeddingsRaw {
			embeddingSlice, ok := embeddingRaw.([]interface{})
			if !ok {
				continue
			}
			embedding := []float64{}
			for _, value := range embeddingSlice {
				if floatVal, ok := value.(float64); ok {
					embedding = append(embedding, floatVal)
				}
			}
			embeddings = append(embeddings, embedding)
		}

		progress += len(batch)
		printProgressBar(progress, total)
	}

	printProgressBar(total, total)
	fmt.Println()

	return embeddings, nil
}

func printProgressBar(current, total int) {
	barLength := 50
	percent := float64(current) / float64(total)
	hashes := int(percent * float64(barLength))
	spaces := barLength - hashes

	fmt.Printf("\r[%s%s] %d%% (%d/%d)", strings.Repeat("#", hashes), strings.Repeat(" ", spaces), int(percent*100), current, total)
}

func enrichPapersWithEmbeddings(papers map[string]Paper) error {
	texts := []string{}
	paperToTextIndex := map[int]string{}
	index := 0

	for id, paper := range papers {
		text := paper.Title + " " + paper.Abstract
		texts = append(texts, text)
		paperToTextIndex[index] = id
		index++

		for _, ref := range paper.References {
			refText := ref.Title + " " + ref.Abstract
			texts = append(texts, refText)
			paperToTextIndex[index] = id
			index++
		}
	}
	total := len(texts)
	fmt.Printf("Enriching embeddings for %d texts\n", total)

	embeddings, err := calculateEmbeddingsBatch(texts, total)
	if err != nil {
		return fmt.Errorf("error calculating embeddings: %v", err)
	}

	index = 0
	for id, paper := range papers {
		if index < len(embeddings) {
			paper.Embedding = embeddings[index]
			index++
		}
		for i := range paper.References {
			if index < len(embeddings) {
				paper.References[i].Embedding = embeddings[index]
				index++
			}
		}
		papers[id] = paper
	}
	return nil
}

func setupSchema(client *weaviate.Client) error {
	// Define Reader class
	readerClass := &models.Class{
		Class: "Reader",
		Properties: []*models.Property{
			{Name: "name", DataType: []string{"string"}},
			{Name: "averageEmbedding", DataType: []string{"number[]"}},
		},
	}

	// Define Paper class
	paperClass := &models.Class{
		Class: "Paper",
		Properties: []*models.Property{
			{Name: "title", DataType: []string{"string"}},
			{Name: "abstract", DataType: []string{"string"}},
			{Name: "embedding", DataType: []string{"number[]"}},
		},
	}

	// Create Reader and Paper classes
	for _, class := range []*models.Class{readerClass, paperClass} {
		err := client.Schema().ClassCreator().WithClass(class).Do(context.Background())
		if err != nil && !containsAlreadyExistsError(err) {
			return err
		}
	}

	// Add reference properties after both classes are created
	err := client.Schema().PropertyCreator().
		WithClassName("Reader").
		WithProperty(&models.Property{
			Name:     "readPapers",
			DataType: []string{"Paper"},
		}).
		Do(context.Background())
	if err != nil && !containsAlreadyExistsError(err) {
		return err
	}

	err = client.Schema().PropertyCreator().
		WithClassName("Paper").
		WithProperty(&models.Property{
			Name:     "authors",
			DataType: []string{"Reader"},
		}).
		Do(context.Background())
	if err != nil && !containsAlreadyExistsError(err) {
		return err
	}

	return nil
}

func containsAlreadyExistsError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "already exists"))
}

func buildReferencePayload(uuids []string) []map[string]string {
	refs := make([]map[string]string, len(uuids))
	for i, uuid := range uuids {
		refs[i] = map[string]string{"beacon": fmt.Sprintf("weaviate://localhost/%s", uuid)}
	}
	return refs
}

func createReader(client *weaviate.Client, readerName string) (string, error) {
	id := uuid.New().String()
	object := &models.Object{
		Class: "Reader",
		Properties: map[string]interface{}{
			"name": readerName,
		},
		ID: strfmt.UUID(id),
	}

	_, err := client.Data().Creator().
		WithClassName("Reader").
		WithProperties(object.Properties).
		WithID(id).
		Do(context.Background())
	if err != nil {
		return "", err
	}

	return id, nil
}

func linkReaderToPaper(client *weaviate.Client, readerID, paperID string) error {
	err := client.Data().ReferenceCreator().
		WithClassName("Reader").
		WithID(readerID).
		WithReferenceProperty("readPapers").
		WithReference(client.Data().ReferencePayloadBuilder().
			WithClassName("Paper").
			WithID(paperID).
			Payload()).
		Do(context.Background())
	if err != nil {
		return err
	}

	return nil
}

func saveToDatabase(client *weaviate.Client, papers map[string]Paper) error {
	for _, paper := range papers {
		readerUUIDs := []string{}
		for _, author := range paper.Authors {
			readerUUID, err := createReader(client, author)
			if err != nil {
				log.Printf("Error creating Reader %s: %v", author, err)
				continue
			}
			readerUUIDs = append(readerUUIDs, readerUUID)
		}

		paperUUID, err := createPaperWithAuthors(client, paper, readerUUIDs)
		if err != nil {
			log.Printf("Error creating Paper %s: %v", paper.Title, err)
			continue
		}

		for _, readerUUID := range readerUUIDs {
			if err := linkReaderToPaper(client, readerUUID, paperUUID); err != nil {
				log.Printf("Error linking Paper %s to Reader %s: %v", paperUUID, readerUUID, err)
			}
		}

		for _, reference := range paper.References {
			referenceUUIDs := []string{}
			for _, refAuthor := range reference.Authors {
				refReaderUUID, err := createReader(client, refAuthor)
				if err != nil {
					log.Printf("Error creating Reader %s for Reference: %v", refAuthor, err)
					continue
				}
				referenceUUIDs = append(referenceUUIDs, refReaderUUID)
			}

			refPaper := Paper{
				Title:     reference.Title,
				Abstract:  reference.Abstract,
				Authors:   reference.Authors,
				Embedding: reference.Embedding,
			}

			refPaperUUID, err := createPaperWithAuthors(client, refPaper, referenceUUIDs)
			if err != nil {
				log.Printf("Error creating referenced Paper %s: %v", refPaper.Title, err)
				continue
			}

			for _, refReaderUUID := range referenceUUIDs {
				if err := linkReaderToPaper(client, refReaderUUID, refPaperUUID); err != nil {
					log.Printf("Error linking Referenced Paper %s to Reader %s: %v", refPaperUUID, refReaderUUID, err)
				}
			}
		}
	}
	return nil
}

func createPaperWithAuthors(client *weaviate.Client, paper Paper, authorUUIDs []string) (string, error) {
	id := uuid.New().String()
	object := &models.Object{
		Class: "Paper",
		Properties: map[string]interface{}{
			"title":     paper.Title,
			"abstract":  paper.Abstract,
			"embedding": paper.Embedding,
			"authors":   buildReferencePayload(authorUUIDs),
		},
		ID: strfmt.UUID(id),
	}

	_, err := client.Data().Creator().
		WithClassName("Paper").
		WithProperties(object.Properties).
		WithID(id).
		Do(context.Background())
	if err != nil {
		return "", fmt.Errorf("error creating Paper: %v", err)
	}

	log.Printf("Paper created: %s (UUID: %s)", paper.Title, id)
	return id, nil
}

type Reader struct {
	Name             string    `json:"name"`
	ReaderID         string    `json:"readerID"`
	ReadPapers       []string  `json:"readPapers"`
	AverageEmbedding []float64 `json:"averageEmbedding"`
}

func saveToJSONFile(papers map[string]Paper, readers map[string]Reader, filePath string) error {
	data := map[string]interface{}{
		"papers":  papers,
		"readers": readers,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling data: %v", err)
	}

	if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
		return fmt.Errorf("error writing JSON file: %v", err)
	}

	log.Printf("Data successfully saved to %s", filePath)
	return nil
}

func main() {
	arxivIDs, err := fetchPapersFromArxiv("cs.IR", "2020-01-01", "2024-12-31", 3, 3)
	if err != nil {
		log.Fatalf("Error fetching Arxiv papers: %v", err)
	}
	log.Printf("Fetched %d Arxiv IDs", len(arxivIDs))
	log.Printf("Arxiv IDs: %v", arxivIDs)

	papers, err := fetchBatchFromSemanticScholar(arxivIDs, 50)
	if err != nil {
		log.Fatalf("Error fetching from Semantic Scholar: %v", err)
	}

	log.Printf("Fetched %d papers from Semantic Scholar", len(papers))
	err = enrichPapersWithEmbeddings(papers)
	if err != nil {
		log.Fatalf("Error enriching papers with embeddings: %v", err)
	}

	readers := map[string]Reader{}
	err = saveToJSONFile(papers, readers, "papers.json")

	if err != nil {
		log.Fatalf("Error saving to JSON file: %v", err)
	}

	client, err := weaviate.NewClient(weaviate.Config{
		Host:   "localhost:8080",
		Scheme: "http",
	})
	if err != nil {
		log.Fatalf("Error initializing Weaviate client: %v", err)
	}

	if err := setupSchema(client); err != nil {
		log.Fatalf("Error setting up schema: %v", err)
	}

	if err := saveToDatabase(client, papers); err != nil {
		log.Fatalf("Error saving to database: %v", err)
	}
}
