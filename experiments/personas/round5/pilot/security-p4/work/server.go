package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
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
)

const maxJSONBody = 1 << 20

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
	mu              sync.Mutex
	users           map[string]*User
	notes           map[int]*Note
	nextNote        int
	filesDir        string
	adminToken      string
	integrations    map[int]*Integration
	nextIntegration int
}

func NewApp(filesDir, adminToken string) *App {
	a := &App{users: map[string]*User{}, notes: map[int]*Note{}, nextNote: 1, filesDir: filesDir, adminToken: adminToken, integrations: map[int]*Integration{}, nextIntegration: 1}
	a.users["tok-alice"] = &User{ID: 1, Name: "Alice", Email: "<email>", Role: "user", Token: "tok-alice"}
	a.users["tok-bob"] = &User{ID: 2, Name: "Bob", Email: "<email>", Role: "user", Token: "tok-bob"}
	a.users["tok-carol"] = &User{ID: 3, Name: "Carol", Email: "<email>", Role: "admin", Token: "tok-carol"}
	a.addNote(1, "alice private", "alice's secret plans")
	a.addNote(2, "bob private", "bob's diary")
	a.addIntegration(1, "alice-hook", "hook-secret", "https://hooks.example.test/events")
	return a
}

func (a *App) addNote(owner int, title, body string) *Note {
	n := &Note{ID: a.nextNote, OwnerID: owner, Title: title, Body: body}
	a.notes[n.ID] = n
	a.nextNote++
	return n
}

func (a *App) currentUser(r *http.Request) *User {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") || len(h) == len("Bearer ") || strings.Contains(h[len("Bearer "):], " ") {
		return nil
	}
	tok := h[len("Bearer "):]
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
	_ = json.NewEncoder(w).Encode(v)
}
func errorJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func accentColor() string {
	colors := []string{"#e57373", "#64b5f6", "#81c784", "#ffd54f"}
	return colors[rand.Intn(len(colors))]
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing json")
		}
		return err
	}
	return nil
}

func parseID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}
func canAccess(owner int, u *User) bool { return owner == u.ID || u.Role == "admin" }

func NewServer(a *App) http.Handler {
	mux := http.NewServeMux()
	registerIntegrationRoutes(mux, a)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		writeJSON(w, 200, map[string]any{"user": u, "accent": accentColor()})
	})
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
			errorJSON(w, 400, "bad json")
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
		if err := decodeJSON(w, r, &in); err != nil || strings.TrimSpace(in.Title) == "" {
			errorJSON(w, 400, "title required")
			return
		}
		a.mu.Lock()
		created := a.addNote(u.ID, in.Title, in.Body)
		out := *created
		a.mu.Unlock()
		log.Printf("audit: created note id=%d title=%q", out.ID, in.Title)
		writeJSON(w, 201, &out)
	})
	mux.HandleFunc("POST /notes/import", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var notes []Note
		if err := decodeJSON(w, r, &notes); err != nil {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		for _, item := range notes {
			a.addNote(u.ID, item.Title, item.Body)
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
		id, ok := parseID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		n := a.notes[id]
		if n != nil {
			copy := *n
			n = &copy
		}
		a.mu.Unlock()
		if n == nil || !canAccess(n.OwnerID, u) {
			errorJSON(w, 404, "not found")
			return
		}
		sum := sha256.Sum256([]byte(n.Title + n.Body))
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
		if raw := r.URL.Query().Get("owner"); raw != "" {
			requested, err := strconv.Atoi(raw)
			if err != nil || requested <= 0 {
				errorJSON(w, 400, "invalid owner")
				return
			}
			if requested != u.ID && u.Role != "admin" {
				errorJSON(w, 403, "forbidden")
				return
			}
			owner = requested
		}
		q := strings.ToLower(r.URL.Query().Get("q"))
		a.mu.Lock()
		found := make([]Note, 0)
		for _, n := range a.notes {
			if n.OwnerID == owner && (q == "" || strings.Contains(strings.ToLower(n.Title+" "+n.Body), q)) {
				found = append(found, *n)
			}
		}
		a.mu.Unlock()
		writeJSON(w, 200, map[string]any{"notes": found})
	})
	mux.HandleFunc("PUT /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, ok := parseID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		var in struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := decodeJSON(w, r, &in); err != nil {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		n := a.notes[id]
		if n == nil || !canAccess(n.OwnerID, u) {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		n.Title, n.Body = in.Title, in.Body
		out := *n
		a.mu.Unlock()
		writeJSON(w, 200, &out)
	})
	mux.HandleFunc("DELETE /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, ok := parseID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		n := a.notes[id]
		if n == nil || !canAccess(n.OwnerID, u) {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		delete(a.notes, id)
		a.mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /files", func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		root, err := filepath.EvalSymlinks(a.filesDir)
		if err != nil {
			errorJSON(w, 404, "not found")
			return
		}
		root, _ = filepath.Abs(root)
		name := r.URL.Query().Get("name")
		if name == "" || filepath.IsAbs(name) {
			errorJSON(w, 400, "invalid name")
			return
		}
		target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.Clean(name)))
		if err != nil {
			errorJSON(w, 404, "not found")
			return
		}
		target, _ = filepath.Abs(target)
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
		http.ServeFile(w, r, target)
	})
	mux.HandleFunc("GET /admin/stats", func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Admin-Token")
		if a.adminToken == "" || len(got) != len(a.adminToken) || subtle.ConstantTimeCompare([]byte(got), []byte(a.adminToken)) != 1 {
			errorJSON(w, 403, "forbidden")
			return
		}
		a.mu.Lock()
		out := map[string]int{"users": len(a.users), "notes": len(a.notes)}
		a.mu.Unlock()
		writeJSON(w, 200, out)
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
		log.Fatal("ADMIN_TOKEN must be set")
	}
	fmt.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, NewServer(NewApp("files", tok))))
}
