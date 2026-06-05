package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/brownw/chirpy/internal/auth"
	"github.com/brownw/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
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

type User struct {
	ID        uuid.UUID    `json:"id"`
	CreatedAt sql.NullTime `json:"created_at"`
	UpdatedAt sql.NullTime `json:"updated_at"`
	Email     string       `json:"email"`
	Password  string       `json:"password,omitempty"` // Omit password in JSON responses
}

type Chirp struct {
	ID        uuid.UUID    `json:"id"`
	CreatedAt sql.NullTime `json:"created_at"`
	UpdatedAt sql.NullTime `json:"updated_at"`
	Body      string       `json:"body"`
	UserID    uuid.UUID    `json:"user_id"`
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

func (cfg *apiConfig) chirpHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var req Chirp

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

	type parameters struct {
		Body    string    `json:"body"`
		User_id uuid.UUID `json:"user_id"`
	}

	parms := parameters{
		Body:    cleaned,
		User_id: req.UserID,
	}

	// Create the chirp using the DB query helper
	createdChirp, err := cfg.dbQueries.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   parms.Body,
		UserID: parms.User_id,
	})
	if err != nil {
		log.Printf("Error creating chirp: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to create chirp")
		return
	}

	respondWithJSON(w, http.StatusCreated, Chirp{
		ID:        createdChirp.ID,
		CreatedAt: createdChirp.CreatedAt,
		UpdatedAt: createdChirp.UpdatedAt,
		Body:      createdChirp.Body,
		UserID:    createdChirp.UserID,
	})
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	if cfg.platform == "production" {
		respondWithError(w, http.StatusForbidden, "Reset is not allowed in production")
		return
	}
	deletedRows, err := cfg.dbQueries.ResetUsers(r.Context())
	if err != nil {
		log.Printf("reset error: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to reset users")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"deleted_rows": deletedRows,
	})
}

func (cfg *apiConfig) createUserHandler(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	parms := parameters{}
	err := decoder.Decode(&parms)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	hashedPassword, err := auth.HashPassword(parms.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	createdUser, err := cfg.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
		Email:          parms.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		log.Printf("Error creating user: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	respondWithJSON(w, http.StatusCreated, User{
		ID:        createdUser.ID,
		CreatedAt: createdUser.CreatedAt,
		UpdatedAt: createdUser.UpdatedAt,
		Email:     createdUser.Email,
	})
}

func (cfg *apiConfig) getChirpsHandler(w http.ResponseWriter, r *http.Request) {
	chirps, err := cfg.dbQueries.GetChirps(r.Context())
	if err != nil {
		log.Printf("Error fetching chirps: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to fetch chirps")
		return
	}

	var response []Chirp
	for _, c := range chirps {
		response = append(response, Chirp{
			ID:        c.ID,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			Body:      c.Body,
			UserID:    c.UserID,
		})
	}

	respondWithJSON(w, http.StatusOK, response)
}
func (cfg *apiConfig) getChirpByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	parsedID, err := uuid.Parse(id)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid id")
		return
	}

	chirp, err := cfg.dbQueries.GetChirpByID(r.Context(), parsedID)
	if err != nil {
		log.Printf("Error fetching chirps: %v", err)
		respondWithError(w, http.StatusNotFound, "Failed to fetch chirp")
		return
	}

	var response Chirp
	response = Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}

	respondWithJSON(w, http.StatusOK, response)
}
func (cfg *apiConfig) loginHandler(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	parms := parameters{}
	err := decoder.Decode(&parms)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	user, err := cfg.dbQueries.GetUserByEmail(r.Context(), parms.Email)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	if !auth.CheckPasswordHash(parms.Password, user.HashedPassword) {
		log.Printf("Invalid password for user: %s", parms.Email)
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	respondWithJSON(w, http.StatusOK, User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})
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
	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	if platform == "" {
		log.Fatal("PLATFORM variable is not set")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	dbQueries := database.New(db)
	mux := http.NewServeMux()
	apiCfg := &apiConfig{dbQueries: dbQueries, platform: platform}
	mux.HandleFunc("GET /api/healthz", readinessHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.requestsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("POST /api/users", apiCfg.createUserHandler)
	mux.HandleFunc("POST /api/chirps", apiCfg.chirpHandler)
	mux.HandleFunc("POST /api/login", apiCfg.loginHandler)
	mux.HandleFunc("GET /api/chirps", apiCfg.getChirpsHandler)
	mux.HandleFunc("GET /api/chirps/{id}", apiCfg.getChirpByIDHandler)
	fileServer := http.FileServer(http.Dir("."))
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fileServer)))
	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServe())
}
