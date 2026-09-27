package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxJSONBody = 1 << 20

var errRequestBodyTooLarge = errors.New("request body too large")

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
	Token string `json:"-"`
}

type Note struct {
	ID      int    `json:"id"`
	OwnerID int    `json:"owner_id"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

type App struct {
	mu         sync.Mutex
	users      map[string]*User // by token
	notes      map[int]*Note
	nextNote   int
	filesDir   string
	adminToken string
}

func NewApp(filesDir, adminToken string) *App {
	a := &App{users: map[string]*User{}, notes: map[int]*Note{}, nextNote: 1, filesDir: filesDir, adminToken: adminToken}
	a.users["tok-alice"] = &User{ID: 1, Name: "Alice", Email: "<email>", Role: "user", Token: "tok-alice"}
	a.users["tok-bob"] = &User{ID: 2, Name: "Bob", Email: "<email>", Role: "user", Token: "tok-bob"}
	a.users["tok-carol"] = &User{ID: 3, Name: "Carol", Email: "<email>", Role: "admin", Token: "tok-carol"}
	a.addNote(1, "alice private", "alice's secret plans")
	a.addNote(2, "bob private", "bob's diary")
	return a
}

func (a *App) addNote(owner int, title, body string) *Note {
	n := &Note{ID: a.nextNote, OwnerID: owner, Title: title, Body: body}
	a.notes[n.ID] = n
	a.nextNote++
	return n
}

func (a *App) currentUser(r *http.Request) *User {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.Count(auth, " ") != 1 {
		return nil
	}
	tok := strings.TrimPrefix(auth, "Bearer ")
	if tok == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.users[tok]
	if u == nil {
		return nil
	}
	copy := *u
	return &copy
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func errorJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errRequestBodyTooLarge
		}
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing json")
		}
		return err
	}
	return nil
}

func handleJSONError(w http.ResponseWriter, err error) {
	if errors.Is(err, errRequestBodyTooLarge) {
		errorJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	errorJSON(w, http.StatusBadRequest, "bad json")
}

func validNoteID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func canAccessNote(u *User, n *Note) bool {
	return n.OwnerID == u.ID || u.Role == "admin"
}

// accentColor picks a cosmetic avatar colour for the UI.
func accentColor() string {
	colors := []string{"#e57373", "#64b5f6", "#81c784", "#ffd54f"}
	return colors[rand.Intn(len(colors))]
}

func NewServer(a *App) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		writeJSON(w, 200, map[string]any{"user": u, "accent": accentColor()})
	})

	// PUT /me lets a user change their profile.
	mux.HandleFunc("PUT /me", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var in struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := decodeJSON(w, r, &in); err != nil {
			handleJSONError(w, err)
			return
		}
		a.mu.Lock()
		stored := a.users[u.Token]
		stored.Name, stored.Email = in.Name, in.Email
		out := *stored
		a.mu.Unlock()
		writeJSON(w, 200, &out)
	})

	mux.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var in struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := decodeJSON(w, r, &in); err != nil {
			handleJSONError(w, err)
			return
		}
		if strings.TrimSpace(in.Title) == "" {
			errorJSON(w, 400, "title required")
			return
		}
		a.mu.Lock()
		n := a.addNote(u.ID, in.Title, in.Body)
		out := *n
		a.mu.Unlock()
		// Log line for the audit trail; nothing here reaches a database.
		log.Printf("audit: created note id=%d title=%q", n.ID, strings.ReplaceAll(strings.ReplaceAll(in.Title, "\r", "\\r"), "\n", "\\n"))
		writeJSON(w, 201, &out)
	})

	// POST /notes/import restores notes exported by the old client.
	mux.HandleFunc("POST /notes/import", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var notes []struct {
			ID      int    `json:"id"`
			OwnerID int    `json:"owner_id"`
			Title   string `json:"title"`
			Body    string `json:"body"`
		}
		if err := decodeJSON(w, r, &notes); err != nil {
			handleJSONError(w, err)
			return
		}
		a.mu.Lock()
		for i := range notes {
			a.addNote(u.ID, notes[i].Title, notes[i].Body)
		}
		a.mu.Unlock()
		writeJSON(w, 201, map[string]int{"imported": len(notes)})
	})

	mux.HandleFunc("GET /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, valid := validNoteID(r)
		if !valid {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		n, ok := a.notes[id]
		if ok {
			copy := *n
			n = &copy
		}
		a.mu.Unlock()
		if !ok || !canAccessNote(u, n) {
			errorJSON(w, 404, "not found")
			return
		}
		sum := sha256.Sum256([]byte(n.Title + "\x00" + n.Body))
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		writeJSON(w, 200, n)
	})

	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		owner := u.ID
		if rawOwner := r.URL.Query().Get("owner"); rawOwner != "" {
			parsed, err := strconv.Atoi(rawOwner)
			if err != nil || parsed <= 0 {
				errorJSON(w, 400, "invalid owner")
				return
			}
			if parsed != u.ID && u.Role != "admin" {
				errorJSON(w, 403, "forbidden")
				return
			}
			owner = parsed
		}
		q := strings.ToLower(r.URL.Query().Get("q"))
		a.mu.Lock()
		defer a.mu.Unlock()
		var found []Note
		for _, n := range a.notes {
			if n.OwnerID == owner && (q == "" || strings.Contains(strings.ToLower(n.Title+" "+n.Body), q)) {
				found = append(found, *n)
			}
		}
		writeJSON(w, 200, map[string]any{"notes": found})
	})

	mux.HandleFunc("PUT /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, valid := validNoteID(r)
		if !valid {
			errorJSON(w, 404, "not found")
			return
		}
		var in struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := decodeJSON(w, r, &in); err != nil {
			handleJSONError(w, err)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		n, ok := a.notes[id]
		if !ok || (n.OwnerID != u.ID && u.Role != "admin") {
			errorJSON(w, 404, "not found")
			return
		}
		n.Title, n.Body = in.Title, in.Body
		writeJSON(w, 200, n)
	})

	mux.HandleFunc("DELETE /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, valid := validNoteID(r)
		if !valid {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		n, ok := a.notes[id]
		if !ok || !canAccessNote(u, n) {
			errorJSON(w, 404, "not found")
			return
		}
		delete(a.notes, id)
		w.WriteHeader(204)
	})

	// GET /files?name=docs/readme.txt serves shared documents.
	mux.HandleFunc("GET /files", func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		name := r.URL.Query().Get("name")
		root, rootErr := filepath.EvalSymlinks(a.filesDir)
		target, targetErr := filepath.EvalSymlinks(filepath.Join(a.filesDir, name))
		if name == "" || filepath.IsAbs(name) || rootErr != nil || targetErr != nil {
			errorJSON(w, 400, "invalid name")
			return
		}
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			errorJSON(w, 400, "invalid name")
			return
		}
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			errorJSON(w, 404, "not found")
			return
		}
		b, err := os.ReadFile(target)
		if err != nil {
			errorJSON(w, 404, "not found")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(b)
	})

	// GET /admin/stats needs the X-Admin-Token header.
	mux.HandleFunc("GET /admin/stats", func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Admin-Token")
		providedHash := sha256.Sum256([]byte(provided))
		expectedHash := sha256.Sum256([]byte(a.adminToken))
		if provided == "" || a.adminToken == "" || subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
			errorJSON(w, 403, "forbidden")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		writeJSON(w, 200, map[string]int{"users": len(a.users), "notes": len(a.notes)})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8082"
	}
	tok := os.Getenv("ADMIN_TOKEN")
	if tok == "" {
		log.Fatal("ADMIN_TOKEN must be set")
	}
	fmt.Println("listening on", addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           NewServer(NewApp("files", tok)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Fatal(srv.ListenAndServe())
}
