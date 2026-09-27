package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Integration struct {
	ID       int    `json:"id"`
	OwnerID  int    `json:"owner_id"`
	Name     string `json:"name"`
	Secret   string `json:"-"`
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
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func integrationClient() *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("invalid destination")
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("cannot resolve destination")
			}
			for _, resolved := range ips {
				if !resolved.IP.IsGlobalUnicast() || resolved.IP.IsPrivate() || resolved.IP.IsLoopback() || resolved.IP.IsLinkLocalUnicast() {
					return nil, fmt.Errorf("private destination")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   3 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateEndpoint(req.URL)
		},
	}
}

func validateEndpoint(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("invalid endpoint")
	}
	return nil
}

func constantTimeEqual(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}

func registerIntegrationRoutes(mux *http.ServeMux, a *App) {
	mux.HandleFunc("POST /integrations", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		var in struct {
			Name     string `json:"name"`
			Secret   string `json:"secret"`
			Endpoint string `json:"endpoint"`
		}
		if err := decodeJSON(w, r, &in); err != nil || in.Name == "" || in.Secret == "" {
			errorJSON(w, 400, "bad json")
			return
		}
		endpoint, err := url.Parse(in.Endpoint)
		if err != nil || validateEndpoint(endpoint) != nil {
			errorJSON(w, 400, "invalid endpoint")
			return
		}
		a.mu.Lock()
		created := a.addIntegration(u.ID, in.Name, in.Secret, endpoint.String())
		out := *created
		a.mu.Unlock()
		writeJSON(w, 201, &out)
	})

	mux.HandleFunc("GET /integrations/export", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		owner := u.ID
		if u.Role == "admin" && r.URL.Query().Get("owner") != "" {
			var err error
			owner, err = strconv.Atoi(r.URL.Query().Get("owner"))
			if err != nil || owner < 0 {
				errorJSON(w, 400, "invalid owner")
				return
			}
		}
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
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		a.mu.Lock()
		stored := a.integrations[integrationID(r)]
		var in *Integration
		if stored != nil {
			copy := *stored
			in = &copy
		}
		a.mu.Unlock()
		if in == nil || !canAccess(in.OwnerID, u) {
			errorJSON(w, 404, "not found")
			return
		}
		writeJSON(w, 200, in)
	})

	mux.HandleFunc("DELETE /integrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		id := integrationID(r)
		in := a.integrations[id]
		if in == nil || !canAccess(in.OwnerID, u) {
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
		if decodeJSON(w, r, &body) != nil || body.Secret == "" {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		in := a.integrations[integrationID(r)]
		if in == nil || !canAccess(in.OwnerID, u) {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		in.Secret = body.Secret
		out := *in
		a.mu.Unlock()
		writeJSON(w, 200, &out)
	})

	mux.HandleFunc("POST /integrations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		a.mu.Lock()
		stored := a.integrations[integrationID(r)]
		var in *Integration
		if stored != nil {
			copy := *stored
			in = &copy
		}
		a.mu.Unlock()
		if in == nil || !canAccess(in.OwnerID, u) {
			errorJSON(w, 404, "not found")
			return
		}
		endpoint, err := url.Parse(in.Endpoint)
		if err != nil || validateEndpoint(endpoint) != nil {
			errorJSON(w, 400, "invalid endpoint")
			return
		}
		resp, err := integrationClient().Get(endpoint.String())
		if err != nil {
			errorJSON(w, 502, "endpoint failed")
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
		if err != nil || len(body) > maxJSONBody {
			errorJSON(w, 502, "endpoint response too large")
			return
		}
		writeJSON(w, 200, map[string]any{"status": resp.StatusCode, "body": string(body)})
	})

	mux.HandleFunc("POST /integrations/{id}/hook", func(w http.ResponseWriter, r *http.Request) {
		signature := r.Header.Get("X-Hook-Signature")
		nonce := r.Header.Get("X-Hook-Nonce")
		if signature == "" || nonce == "" {
			errorJSON(w, 403, "forbidden")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		in := a.integrations[integrationID(r)]
		if in == nil {
			errorJSON(w, 404, "not found")
			return
		}
		if !constantTimeEqual(in.Secret, signature) {
			errorJSON(w, 403, "forbidden")
			return
		}
		if a.hookNonces[in.ID] == nil {
			a.hookNonces[in.ID] = map[string]struct{}{}
		}
		if _, exists := a.hookNonces[in.ID][nonce]; exists {
			errorJSON(w, 409, "nonce already used")
			return
		}
		a.hookNonces[in.ID][nonce] = struct{}{}
		in.Events++
		fmt.Fprint(w, "accepted")
	})
}
