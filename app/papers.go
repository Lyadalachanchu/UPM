package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
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

func fetchPapersFromArxiv(category, startDate, endDate string, batchSize, maxPapers int) ([]string, error) {
	baseURL := "http://export.arxiv.org/api/query"
	var arxivIDs []string
	start := 0

	// Ensure batchSize doesn't exceed ArXiv's limit
	if batchSize > 100 {
		batchSize = 100
	}

	for len(arxivIDs) < maxPapers {
		// Prepare the query parameters
		params := fmt.Sprintf(
			"search_query=cat:%s+AND+submittedDate:[%s+TO+%s]&start=%d&max_results=%d",
			category, startDate, endDate, start, batchSize,
		)
		url := fmt.Sprintf("%s?%s", baseURL, params)

		// Make the API request
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("error fetching from ArXiv: %v", err)
		}
		defer resp.Body.Close()

		// Read and parse the response
		var arxivResponse ArxivResponse
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading ArXiv response: %v", err)
		}
		if err := xml.Unmarshal(body, &arxivResponse); err != nil {
			return nil, fmt.Errorf("error decoding ArXiv XML: %v", err)
		}

		// Extract IDs from the response
		for _, entry := range arxivResponse.Entries {
			parts := strings.Split(entry.ID, "/")
			if len(parts) > 0 {
				arxivId := parts[len(parts)-1]
				// Remove version suffix
				arxivId = strings.Split(arxivId, "v")[0]
				arxivIDs = append(arxivIDs, arxivId)
			}
		}

		// Update the `start` index for the next batch
		start += batchSize
	}

	// Limit the total number of IDs to `maxPapers`
	if len(arxivIDs) > maxPapers {
		arxivIDs = arxivIDs[:maxPapers]
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
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Printf("Error closing response body: %v", err)
			}
		}(resp.Body)

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

func getSession() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
	}
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

func fetchPapersWithReferencesAndEnrichWithEmbeddings(maxPapers int) (error, map[string]Paper) {
	arxivIDs, err := fetchPapersFromArxiv("cs.IR", "2020-01-01", "2024-12-31", 200, maxPapers)
	if err != nil {
		log.Fatalf("Error fetching Arxiv papers: %v", err)
	}
	log.Printf("Fetched %d Arxiv IDs", len(arxivIDs))

	papers, err := fetchBatchFromSemanticScholar(arxivIDs, 50)
	if err != nil {
		log.Fatalf("Error fetching from Semantic Scholar: %v", err)
	}

	log.Printf("Fetched %d papers from Semantic Scholar", len(papers))
	err = enrichPapersWithEmbeddings(papers)
	if err != nil {
		log.Fatalf("Error enriching papers with embeddings: %v", err)
	}
	return err, papers
}
