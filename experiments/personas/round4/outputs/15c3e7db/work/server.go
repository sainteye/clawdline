package main

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

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
	a.users["tok-alice"] = &User{ID: 1, Name: "Alice", Email: "alice@example.test", Role: "user", Token: "tok-alice"}
	a.users["tok-bob"] = &User{ID: 2, Name: "Bob", Email: "bob@example.test", Role: "user", Token: "tok-bob"}
	a.users["tok-carol"] = &User{ID: 3, Name: "Carol", Email: "carol@example.test", Role: "admin", Token: "tok-carol"}
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
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.users[tok]
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func errorJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
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
	// Only name/email are attacker-controlled; role, id and token must never
	// be settable from the request body or a user could grant themselves
	// admin or take over another user's id.
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
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		u.Name, u.Email = in.Name, in.Email
		a.mu.Unlock()
		writeJSON(w, 200, u)
	})

	mux.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var in struct{ Title, Body string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Title == "" {
			errorJSON(w, 400, "title required")
			return
		}
		a.mu.Lock()
		n := a.addNote(u.ID, in.Title, in.Body)
		a.mu.Unlock()
		// %q quotes and escapes the title so a newline or quote in it can't
		// forge a fake log line.
		log.Printf("audit: created note id=%d owner=%d title=%q", n.ID, u.ID, in.Title)
		writeJSON(w, 201, n)
	})

	mux.HandleFunc("GET /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(r.PathValue("id"))
		a.mu.Lock()
		n, ok := a.notes[id]
		a.mu.Unlock()
		if !ok || (n.OwnerID != u.ID && u.Role != "admin") {
			errorJSON(w, 404, "not found")
			return
		}
		sum := md5.Sum([]byte(n.Title + n.Body))
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		writeJSON(w, 200, n)
	})

	mux.HandleFunc("PUT /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, _ := strconv.Atoi(r.PathValue("id"))
		a.mu.Lock()
		defer a.mu.Unlock()
		n, ok := a.notes[id]
		if !ok || (n.OwnerID != u.ID && u.Role != "admin") {
			errorJSON(w, 404, "not found")
			return
		}
		var in struct{ Title, Body string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			errorJSON(w, 400, "bad json")
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
		id, _ := strconv.Atoi(r.PathValue("id"))
		a.mu.Lock()
		defer a.mu.Unlock()
		n, ok := a.notes[id]
		if !ok || (n.OwnerID != u.ID && u.Role != "admin") {
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
		// Rooting the name before cleaning (same technique http.Dir uses)
		// guarantees "../" segments can never walk the join above filesDir,
		// regardless of where in the string they appear.
		safe := filepath.FromSlash(path.Clean("/" + name))
		b, err := os.ReadFile(filepath.Join(a.filesDir, safe))
		if err != nil {
			errorJSON(w, 404, "not found")
			return
		}
		w.Write(b)
	})

	// GET /admin/stats needs the X-Admin-Token header.
	mux.HandleFunc("GET /admin/stats", func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Admin-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.adminToken)) != 1 {
			errorJSON(w, 403, "forbidden")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		writeJSON(w, 200, map[string]int{"users": len(a.users), "notes": len(a.notes)})
	})

	return mux
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8082"
	}
	tok := os.Getenv("ADMIN_TOKEN")
	if tok == "" {
		log.Fatal("ADMIN_TOKEN must be set (no default admin token is provided)")
	}
	fmt.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, NewServer(NewApp("files", tok))))
}
