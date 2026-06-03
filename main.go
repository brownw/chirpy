package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

type ChirpRequest struct {
	Body string `json:"body"`
}

// Response structure for validation errors
type ErrorResponse struct {
	Error string `json:"error"`
}

// Response structure for valid chirps
type ValidResponse struct {
	Valid bool `json:"valid"`
}

// Updated response structure to include the cleaned text
type ValidChirpResponse struct {
	CleanedBody string `json:"cleaned_body"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (cfg *apiConfig) requestsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load())))
}

func validationHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var req ChirpRequest

	// Decode JSON and handle malformed requests
	if err := decoder.Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	// Enforce length restriction (e.g., maximum 140 characters)
	const maxChirpLength = 140
	if len(req.Body) > maxChirpLength {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	// Enforce non-empty content
	if len(req.Body) == 0 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too short")
		return
	}

	// Filter the profanity from the text
	cleaned := getCleanedBody(req.Body)

	// Return the success code and the sanitized payload
	respondWithJSON(w, http.StatusOK, ValidChirpResponse{
		CleanedBody: cleaned,
	})
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, ErrorResponse{Error: msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

func getCleanedBody(body string) string {
	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}

	words := strings.Split(body, " ")
	for i, word := range words {
		// Normalize to lowercase to catch variants like "Kerfuffle" or "SHARBERT"
		loweredWord := strings.ToLower(word)
		if _, exists := badWords[loweredWord]; exists {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}

func main() {
	mux := http.NewServeMux()
	apiCfg := &apiConfig{}
	mux.HandleFunc("GET /api/healthz", readinessHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.requestsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("POST /api/validate_chirp", validationHandler)
	fileServer := http.FileServer(http.Dir("."))
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fileServer)))
	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServe())
}
