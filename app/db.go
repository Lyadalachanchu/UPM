package main

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate/entities/models"
	"log"
	"strings"
)

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
