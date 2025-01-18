package main

import (
	"context"
	"fmt"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
)

type Reference struct {
	Title     string   `json:"title"`
	Abstract  string   `json:"abstract"`
	Authors   []string `json:"authors"`
	Embedding []int    `json:"embedding"`
}

type Paper struct {
	PaperID    int64       `json:"paperId"`
	Embeddings []int       `json:"embeddings"`
	Title      string      `json:"title"`
	Abstract   string      `json:"abstract"`
	References []Reference `json:"references"`
}

type DataWrapper struct {
	Data []Paper `json:"data"`
}

func GetSchema() {
	cfg := weaviate.Config{
		Host:   "localhost:8080",
		Scheme: "http",
	}
	client, err := weaviate.NewClient(cfg)
	if err != nil {
		panic(err)
	}

	schema, err := client.Schema().Getter().Do(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Printf("%v", schema)
}

func main() {
	GetSchema()
}
