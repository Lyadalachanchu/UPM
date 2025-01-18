package main

import (
	"encoding/json"
	"fmt"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"log"
	"os"
)

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
