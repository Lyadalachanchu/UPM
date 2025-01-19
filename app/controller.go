package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate/entities/models"
	"log"
	"net/http"
)

type AddPaperRequest struct {
	URL string `json:"url"`
}

type SearchPapersRequest struct {
	SearchTerm string `json:"searchTerm"`
}

type RelevantPapersRequest struct {
	Filters          Filters  `json:"filters"`
	SelectedPaperIDs []string `json:"selectedPaperIds"`
}

type Filters struct {
	MyOrganization     bool   `json:"myOrganization"`
	MyTeam             bool   `json:"myTeam"`
	OnlyFollowed       bool   `json:"onlyFollowed"`
	RelevanceThreshold [2]int `json:"relevanceThreshold"`
}

type AddPaperResponse struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Abstract    string      `json:"abstract"`
	Authors     []string    `json:"authors"`
	References  []Reference `json:"references"`
	Embedding   []float64   `json:"embedding"`
	Publication string      `json:"publicationDate"`
	Upvotes     int         `json:"upvotes"`
	Downvotes   int         `json:"downvotes"`
	IsUpvoted   bool        `json:"isUpvoted"`
	IsDownvoted bool        `json:"isDownvoted"`
	Reads       int         `json:"reads"`
}

// Controller for handling HTTP requests

func MyPapersHandler(w http.ResponseWriter, r *http.Request) {
	// Placeholder for user's papers
	response := map[string]interface{}{
		"papers": []Paper{
			{
				Title:    "My Saved Paper",
				Abstract: "Saved paper abstract",
				Authors:  []string{"Author 1", "Author 2"},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func SetupRoutes() {
	http.Handle("/api/core/add-paper", enableCORS(http.HandlerFunc(AddPaperHandler)))
	http.Handle("/api/core/search-papers", enableCORS(http.HandlerFunc(SearchPapersHandler)))
	http.Handle("/api/core/get-my-papers", enableCORS(http.HandlerFunc(MyPapersHandler)))
	http.Handle("/api/core/get-relevant-papers", enableCORS(http.HandlerFunc(RelevantPapersHandler)))
}

var weaviateClient *weaviate.Client

func init() {
	client, err := weaviate.NewClient(weaviate.Config{
		Host:   "localhost:8080",
		Scheme: "http",
	})
	if err != nil {
		log.Fatalf("Error initializing Weaviate client: %v", err)
	}
	weaviateClient = client
}

func AddPaperHandler(w http.ResponseWriter, r *http.Request) {
	var req AddPaperRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	query := `
	{
	  Get {
	    Paper(where: {
	      path: ["title"]
	      operator: Equal
	      valueString: "%s"
	    }) {
	      title
	      abstract
	      authors {
	        ... on Reader {
	          name
	        }
	      }
	      _additional {
	        id
	      }
	    }
	  }
	}`
	query = formatGraphQLQuery(query, req.URL)

	response, err := weaviateClient.GraphQL().Raw().WithQuery(query).Do(context.Background())
	if err != nil || response.Errors != nil {
		http.Error(w, "Error fetching paper data", http.StatusInternalServerError)
		return
	}

	paper := parsePaperFromResponse(response.Data)

	addPaperResponse := AddPaperResponse{
		ID:          uuid.New().String(),
		Title:       paper.Title,
		Abstract:    paper.Abstract,
		Authors:     paper.Authors,
		References:  []Reference{},  // Extend if needed
		Embedding:   []float64{0.0}, // Placeholder for embedding
		Publication: "2024-01-01",   // Placeholder for publication date
		Upvotes:     0,
		Downvotes:   0,
		IsUpvoted:   false,
		IsDownvoted: false,
		Reads:       0,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addPaperResponse)
}

func SearchPapersHandler(w http.ResponseWriter, r *http.Request) {
	var req SearchPapersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Query to get the paper ID by title
	queryForID := fmt.Sprintf(`
	{
	  Get {
	    Paper(where: {
	      path: ["title"]
	      operator: Equal
	      valueString: "%s"
	    }) {
	      _additional {
	        id
	      }
	      title
	      abstract
	      authors {
	        ... on Reader {
	          name
	        }
	      }
	    }
	  }
	}`, req.SearchTerm)

	idResponse, err := weaviateClient.GraphQL().Raw().WithQuery(queryForID).Do(context.Background())
	if err != nil || idResponse.Errors != nil {
		http.Error(w, "Error fetching paper ID", http.StatusInternalServerError)
		return
	}

	// Extract the ID of the first paper
	firstPaperID, err := extractFirstPaperID(idResponse.Data)
	if err != nil {
		http.Error(w, "Paper not found", http.StatusNotFound)
		return
	}

	// Query to find similar papers using the ID
	queryForSimilarPapers := fmt.Sprintf(`
	{
	  Get {
	    Paper(nearObject: {
	      id: "%s"
	    }, limit: 30) {
	      title
	      abstract
	      authors {
	        ... on Reader {
	          name
	        }
	      }
	      _additional {
	        score
	        distance
	        id
	      }
	    }
	  }
	}`, firstPaperID)

	similarPapersResponse, err := weaviateClient.GraphQL().Raw().WithQuery(queryForSimilarPapers).Do(context.Background())
	if err != nil || similarPapersResponse.Errors != nil {
		http.Error(w, "Error fetching similar papers", http.StatusInternalServerError)
		return
	}

	// Parse similar papers from the response
	similarPapers := parsePapersFromResponse(similarPapersResponse.Data)

	// Prepare the response
	response := map[string]interface{}{
		"originalPaperID": firstPaperID,
		"similarPapers":   similarPapers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func RelevantPapersHandler(w http.ResponseWriter, r *http.Request) {
	var req RelevantPapersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	selectedPaperID := req.SelectedPaperIDs[0] // For simplicity, take the first ID
	query := `
	{
	  Get {
	    Paper(nearObject: {
	      id: "%s"
	    }, limit: 30) {
	      title
	      abstract
	      authors {
	        ... on Reader {
	          name
	        }
	      }
	      _additional {
	        score
	        id
	      }
	    }
	  }
	}`
	query = formatGraphQLQuery(query, selectedPaperID)

	response, err := weaviateClient.GraphQL().Raw().WithQuery(query).Do(context.Background())
	if err != nil || response.Errors != nil {
		http.Error(w, "Error fetching relevant papers", http.StatusInternalServerError)
		return
	}

	papers := parsePapersFromResponse(response.Data)

	relevantPapersResponse := map[string]interface{}{
		"papers": papers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(relevantPapersResponse)
}

func formatGraphQLQuery(query, value string) string {
	return fmt.Sprintf(query, value)
}

func parsePaperFromResponse(data interface{}) Paper {
	return Paper{
		Title:    "Example Title",
		Abstract: "Example Abstract",
		Authors:  []string{"Author A", "Author B"},
	}
}

func parsePapersFromResponse(data interface{}) []Paper {
	getResult, ok := data.(map[string]models.JSONObject)["Get"]
	if !ok {
		log.Printf("Invalid response format: 'Get' key missing")
		return nil
	}

	getMap, ok := getResult.(map[string]interface{})
	if !ok {
		log.Printf("Invalid response format: 'Get' is not a valid map")
		return nil
	}

	papersData, ok := getMap["Paper"].([]interface{})
	if !ok {
		log.Printf("No papers found in response")
		return nil
	}

	var papers []Paper
	for _, paperData := range papersData {
		paperMap, ok := paperData.(map[string]interface{})
		if !ok {
			log.Printf("Invalid paper format in response")
			continue
		}

		// Extract details
		title, _ := paperMap["title"].(string)
		abstract, _ := paperMap["abstract"].(string)

		// Extract authors
		var authors []string
		if authorsData, ok := paperMap["authors"].([]interface{}); ok {
			for _, author := range authorsData {
				authorMap, ok := author.(map[string]interface{})
				if ok {
					name, _ := authorMap["name"].(string)
					authors = append(authors, name)
				}
			}
		}

		// Create the paper object
		papers = append(papers, Paper{
			Title:    title,
			Abstract: abstract,
			Authors:  authors,
		})
	}

	return papers
}

func extractFirstPaperID(data interface{}) (string, error) {
	getResult, ok := data.(map[string]models.JSONObject)["Get"]
	if !ok {
		return "", fmt.Errorf("invalid response format: 'Get' key missing or not a JSONObject")
	}
	// Access the "Paper" field, which should be a slice of interface{}
	getMap, ok := getResult.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("'Get' is not a valid map")
	}

	papers, ok := getMap["Paper"].([]interface{})
	if !ok || len(papers) == 0 {
		return "", fmt.Errorf("no papers found in response")
	}

	firstPaper, ok := papers[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid paper format in response")
	}

	additional, ok := firstPaper["_additional"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("missing '_additional' key in paper data")
	}

	id, ok := additional["id"].(string)
	if !ok {
		return "", fmt.Errorf("ID not found in '_additional'")
	}

	return id, nil
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")            // Allow your frontend
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS") // Allowed methods
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")     // Allowed headers
		w.Header().Set("Access-Control-Allow-Credentials", "true")                        // Allow cookies

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK) // Handle preflight requests
			return
		}

		next.ServeHTTP(w, r)
	})
}
