package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Integration struct {
	ID       int    `json:"id"`
	OwnerID  int    `json:"owner_id"`
	Name     string `json:"name"`
	Secret   string `json:"secret"`
	Endpoint string `json:"endpoint"`
	Events   int    `json:"events"`
}

func (a *App) addIntegration(owner int, name, secret, endpoint string) *Integration {
	in := &Integration{ID: a.nextIntegration, OwnerID: owner, Name: name, Secret: secret, Endpoint: endpoint}
	a.integrations[in.ID] = in
	a.nextIntegration++
	return in
}

func integrationID(r *http.Request) int {
	id, _ := strconv.Atoi(r.PathValue("id"))
	return id
}

func registerIntegrationRoutes(mux *http.ServeMux, a *App) {
	mux.HandleFunc("POST /integrations", func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var in Integration
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		created := a.addIntegration(in.OwnerID, in.Name, in.Secret, in.Endpoint)
		a.mu.Unlock()
		writeJSON(w, 201, created)
	})

	mux.HandleFunc("GET /integrations/export", func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		owner, _ := strconv.Atoi(r.URL.Query().Get("owner"))
		a.mu.Lock()
		defer a.mu.Unlock()
		var out []Integration
		for _, in := range a.integrations {
			if owner == 0 || in.OwnerID == owner {
				out = append(out, *in)
			}
		}
		writeJSON(w, 200, map[string]any{"integrations": out})
	})

	mux.HandleFunc("GET /integrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if a.currentUser(r) == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		a.mu.Lock()
		in := a.integrations[integrationID(r)]
		a.mu.Unlock()
		if in == nil {
			errorJSON(w, 404, "not found")
			return
		}
		writeJSON(w, 200, in)
	})

	mux.HandleFunc("DELETE /integrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		id := integrationID(r)
		if a.integrations[id] == nil {
			errorJSON(w, 404, "not found")
			return
		}
		delete(a.integrations, id)
		w.WriteHeader(204)
	})

	mux.HandleFunc("PUT /integrations/{id}/secret", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var body struct {
			Secret string `json:"secret"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		in := a.integrations[integrationID(r)]
		if in == nil || in.OwnerID != u.ID {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		in.Secret = body.Secret
		a.mu.Unlock()
		log.Printf("integration %d secret changed to %s", in.ID, body.Secret)
		writeJSON(w, 200, in)
	})

	mux.HandleFunc("POST /integrations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		a.mu.Lock()
		in := a.integrations[integrationID(r)]
		a.mu.Unlock()
		if in == nil || in.OwnerID != u.ID {
			errorJSON(w, 404, "not found")
			return
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(in.Endpoint)
		if err != nil {
			errorJSON(w, 502, "endpoint failed")
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		writeJSON(w, 200, map[string]any{"status": resp.StatusCode, "body": string(body)})
	})

	mux.HandleFunc("POST /integrations/{id}/hook", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		in := a.integrations[integrationID(r)]
		if in == nil {
			errorJSON(w, 404, "not found")
			return
		}
		if !strings.HasPrefix(in.Secret, r.Header.Get("X-Hook-Signature")) {
			errorJSON(w, 403, "forbidden")
			return
		}
		in.Events++
		fmt.Fprint(w, "accepted")
	})
}
