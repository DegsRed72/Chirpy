package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DegsRed72/Chirpy/internal/auth"

	"github.com/google/uuid"

	"github.com/joho/godotenv"

	_ "github.com/lib/pq"

	"github.com/DegsRed72/Chirpy/internal/database"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	Queries        *database.Queries
	Secret         string
}
type User struct {
	ID             uuid.UUID `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Email          string    `json:"email"`
	HashedPassword string    `json:"-"`
	Token          string    `json:"token"`
}
type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	if len(dbURL) == 0 {
		log.Fatal("No dbURL")
	}
	secret := os.Getenv("SECRET")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Error opening sql")
	}
	dbQueries := database.New(db)
	cfg := &apiConfig{
		fileserverHits: atomic.Int32{},
		Queries:        dbQueries,
		Secret:         secret,
	}
	serveMux := http.NewServeMux()
	server := http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}
	serveMux.Handle("/app/", http.StripPrefix("/app", cfg.middlewareMetricsInc(http.FileServer(http.Dir(".")))))
	serveMux.HandleFunc("GET /api/healthz", readiness)
	serveMux.HandleFunc("GET /admin/metrics", cfg.requests)
	serveMux.HandleFunc("GET /api/chirps", cfg.getChirps)
	serveMux.HandleFunc("GET /api/chirps/{chirpID}", cfg.getChirp)
	serveMux.HandleFunc("POST /admin/reset", cfg.reset)
	serveMux.HandleFunc("POST /api/chirps", cfg.makeChirp)
	serveMux.HandleFunc("POST /api/users", cfg.makeUser)
	serveMux.HandleFunc("POST /api/login", cfg.login)
	log.Fatal(server.ListenAndServe())
}

func readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(http.StatusText(http.StatusOK)))
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) requests(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) reset(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits = atomic.Int32{}
	cfg.Queries.DeleteAllUsers(r.Context())
}

func (cfg *apiConfig) makeChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, fmt.Sprintf("%s", err))
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.Secret)
	if err != nil {
		respondWithError(w, 401, fmt.Sprintf("%s", err))
		return
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error decoding parameters: %s", err))
		return
	}
	respBody := params.Body
	if len(respBody) > 140 {
		respondWithError(w, 400, "Chirp too long")
		return
	}
	words := strings.Split(respBody, " ")
	for i, word := range words {
		lowercase_word := strings.ToLower(word)
		if lowercase_word == "kerfuffle" || lowercase_word == "sharbert" || lowercase_word == "fornax" {
			words[i] = "****"
		}
	}
	respBody = strings.Join(words, " ")
	dbChirp, err := cfg.Queries.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   respBody,
		UserID: userID,
	})
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error making Chirp: %s", err))
	}
	chirp := Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body:      dbChirp.Body,
		UserID:    dbChirp.UserID,
	}

	respondWithJSON(w, 201, chirp)
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	w.Write([]byte(msg))
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error marshalling JSON: %s", err))
		return
	}
	w.WriteHeader(code)
	w.Write([]byte(dat))
}

func (cfg *apiConfig) makeUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error decoding parameters: %s", err))
		return
	}
	pass, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error hashing password: %s", err))
	}
	DBuser, err := cfg.Queries.CreateUser(r.Context(), database.CreateUserParams{Email: params.Email, HashedPassword: pass})
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error creating user: %s", err))
		return
	}
	user := User{
		ID:        DBuser.ID,
		CreatedAt: DBuser.CreatedAt,
		UpdatedAt: DBuser.UpdatedAt,
		Email:     DBuser.Email,
	}
	respondWithJSON(w, 201, user)
}

func (cfg *apiConfig) getChirps(w http.ResponseWriter, r *http.Request) {
	chirps := []Chirp{}
	dbChirps, err := cfg.Queries.GetAllChirps(r.Context())
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error getting chirps: %s", err))
		return
	}
	for _, dbChirp := range dbChirps {
		chirps = append(chirps, Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			Body:      dbChirp.Body,
			UserID:    dbChirp.UserID,
		})
	}

	respondWithJSON(w, 200, chirps)
}

func (cfg *apiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	chirpIDStr := r.PathValue("chirpID")
	chirpID, err := uuid.Parse(chirpIDStr)
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Invalid chirp ID: %s", err))
		return
	}
	dbChirp, err := cfg.Queries.GetChirp(r.Context(), chirpID)
	if errors.Is(err, sql.ErrNoRows) {
		respondWithError(w, 404, fmt.Sprintf("Chirp not found: %s", err))
		return
	}
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error getting Chirp: %s", err))
		return
	}
	chirp := Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body:      dbChirp.Body,
		UserID:    dbChirp.UserID,
	}
	respondWithJSON(w, 200, chirp)

}

func (cfg *apiConfig) login(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email            string `json:"email"`
		Password         string `json:"password"`
		ExpiresInSeconds int    `json:"expires_in_seconds"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 400, fmt.Sprintf("Error decoding parameters: %s", err))
		return
	}
	dbUser, err := cfg.Queries.GetUser(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, 401, "Email not found")
		return
	}
	experationTime := params.ExpiresInSeconds
	if experationTime == 0 || experationTime > 3600 {
		experationTime = 3600
	}
	token, err := auth.MakeJWT(dbUser.ID, cfg.Secret, time.Duration(experationTime*int(time.Second)))
	match, err := auth.CheckPasswordHash(params.Password, dbUser.HashedPassword)
	if err != nil {
		respondWithError(w, 401, fmt.Sprintf("Error comparing Hash and Password: %s", err))
		return
	}
	if match == true {
		user := User{
			ID:        dbUser.ID,
			CreatedAt: dbUser.CreatedAt,
			UpdatedAt: dbUser.UpdatedAt,
			Email:     dbUser.Email,
			Token:     token,
		}
		respondWithJSON(w, 200, user)
	} else {
		respondWithError(w, 401, "Email or Password does not match")
	}

}
