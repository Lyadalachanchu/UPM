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
	err, papers := fetchPapersWithReferencesAndEnrichWithEmbeddings(10)

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
