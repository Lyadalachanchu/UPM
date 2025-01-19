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
	readerClass := &models.Class{
		Class:           "Reader",
		Vectorizer:      "none",
		VectorIndexType: "hnsw",
		Properties: []*models.Property{
			{Name: "name", DataType: []string{"string"}},
			{Name: "averageEmbedding", DataType: []string{"number[]"}},
		},
	}

	paperClass := &models.Class{
		Class:           "Paper",
		Vectorizer:      "none",
		VectorIndexType: "hnsw",
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

	newEmbedding := convertToFloat32(paper.Embedding)

	_, err := client.Data().Creator().
		WithClassName("Paper").
		WithProperties(object.Properties).
		WithID(id).
		WithVector(newEmbedding).
		Do(context.Background())
	if err != nil {
		return "", fmt.Errorf("error creating Paper: %v", err)
	}
	return id, nil
}

func convertToFloat32(input []float64) []float32 {
	output := make([]float32, len(input))
	for i, v := range input {
		output[i] = float32(v)
	}
	return output
}

func updateAllReadersAverageEmbedding(client *weaviate.Client) error {
	query := `{
        Get {
            Reader {
                _id
            }
        }
    }`

	response, err := client.GraphQL().Raw().WithQuery(query).Do(context.Background())
	if err != nil {
		return fmt.Errorf("error fetching Readers data: %v", err)
	}

	readers, ok := response.Data["Get"].(map[string]interface{})["Reader"].([]interface{})
	if !ok || len(readers) == 0 {
		return fmt.Errorf("no Readers found in the database")
	}

	// Step 2: Iterate over each Reader and update their averageEmbedding
	for _, reader := range readers {
		r, ok := reader.(map[string]interface{})
		if !ok {
			continue
		}

		readerID, ok := r["_id"].(string)
		if !ok {
			log.Printf("Skipping invalid Reader entry: %+v", r)
			continue
		}

		// Fetch papers associated with the current Reader
		query := fmt.Sprintf(`{
            Get {
                Reader(where: {
                    path: ["_id"],
                    operator: Equal,
                    valueString: "%s"
                }) {
                    readPapers {
                        ... on Paper {
                            embedding
                        }
                    }
                }
            }
        }`, readerID)

		response, err := client.GraphQL().Raw().WithQuery(query).Do(context.Background())
		if err != nil {
			log.Printf("Error fetching Reader %s data: %v", readerID, err)
			continue
		}

		papers, ok := response.Data["Get"].(map[string]interface{})["Reader"].([]interface{})
		if !ok || len(papers) == 0 {
			log.Printf("No papers found for Reader %s", readerID)
			continue
		}

		paperEmbeddings := []([]float32){}
		for _, paper := range papers[0].(map[string]interface{})["readPapers"].([]interface{}) {
			embedding, ok := paper.(map[string]interface{})["embedding"].([]interface{})
			if !ok {
				continue
			}
			floatEmbedding := make([]float32, len(embedding))
			for i, v := range embedding {
				floatEmbedding[i] = float32(v.(float64))
			}
			paperEmbeddings = append(paperEmbeddings, floatEmbedding)
		}

		if len(paperEmbeddings) == 0 {
			log.Printf("No embeddings found for Reader %s", readerID)
			continue
		}

		avgEmbedding := make([]float32, len(paperEmbeddings[0]))
		for _, embedding := range paperEmbeddings {
			for i, value := range embedding {
				avgEmbedding[i] += value
			}
		}
		for i := range avgEmbedding {
			avgEmbedding[i] /= float32(len(paperEmbeddings))
		}

		// Update the Reader with the calculated averageEmbedding
		err = client.Data().Updater().
			WithClassName("Reader").
			WithID(readerID).
			WithProperties(map[string]interface{}{
				"averageEmbedding": avgEmbedding,
			}).
			Do(context.Background())

		if err != nil {
			log.Printf("Error updating averageEmbedding for Reader %s: %v", readerID, err)
			continue
		}

		log.Printf("Successfully updated averageEmbedding for Reader %s", readerID)
	}

	return nil
}
