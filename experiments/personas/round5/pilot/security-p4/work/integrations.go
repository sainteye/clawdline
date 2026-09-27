package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxIntegrationResponse = 1 << 20

type Integration struct {
	ID       int                 `json:"id"`
	OwnerID  int                 `json:"owner_id"`
	Name     string              `json:"name"`
	Secret   string              `json:"-"`
	Endpoint string              `json:"endpoint"`
	Events   int                 `json:"events"`
	Nonces   map[string]struct{} `json:"-"`
}

func (a *App) addIntegration(owner int, name, secret, endpoint string) *Integration {
	in := &Integration{ID: a.nextIntegration, OwnerID: owner, Name: name, Secret: secret, Endpoint: endpoint, Nonces: make(map[string]struct{})}
	a.integrations[in.ID] = in
	a.nextIntegration++
	return in
}

func integrationID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

func validatePublicURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("invalid endpoint")
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("unresolvable endpoint")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, fmt.Errorf("private endpoint")
		}
	}
	return u, nil
}

func integrationHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("unresolvable endpoint")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, fmt.Errorf("private endpoint")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}}
	client := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		_, err := validatePublicURL(req.Context(), req.URL.String())
		return err
	}
	return client
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
		if _, err := url.ParseRequestURI(in.Endpoint); err != nil {
			errorJSON(w, 400, "invalid endpoint")
			return
		}
		a.mu.Lock()
		created := a.addIntegration(u.ID, in.Name, in.Secret, in.Endpoint)
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
		a.mu.Lock()
		out := make([]Integration, 0)
		for _, in := range a.integrations {
			if in.OwnerID == owner {
				copy := *in
				copy.Nonces = nil
				out = append(out, copy)
			}
		}
		a.mu.Unlock()
		writeJSON(w, 200, map[string]any{"integrations": out})
	})
	mux.HandleFunc("GET /integrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, ok := integrationID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		in := a.integrations[id]
		if in != nil {
			copy := *in
			copy.Nonces = nil
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
		id, ok := integrationID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		in := a.integrations[id]
		if in == nil || !canAccess(in.OwnerID, u) {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		delete(a.integrations, id)
		a.mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("PUT /integrations/{id}/secret", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, ok := integrationID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		var body struct {
			Secret string `json:"secret"`
		}
		if err := decodeJSON(w, r, &body); err != nil || body.Secret == "" {
			errorJSON(w, 400, "bad json")
			return
		}
		a.mu.Lock()
		in := a.integrations[id]
		if in == nil || !canAccess(in.OwnerID, u) {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		in.Secret = body.Secret
		in.Nonces = make(map[string]struct{})
		out := *in
		out.Nonces = nil
		a.mu.Unlock()
		writeJSON(w, 200, &out)
	})
	mux.HandleFunc("POST /integrations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			errorJSON(w, 401, "unauthorized")
			return
		}
		id, ok := integrationID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		a.mu.Lock()
		in := a.integrations[id]
		if in == nil || !canAccess(in.OwnerID, u) {
			a.mu.Unlock()
			errorJSON(w, 404, "not found")
			return
		}
		endpoint := in.Endpoint
		a.mu.Unlock()
		if _, err := validatePublicURL(r.Context(), endpoint); err != nil {
			errorJSON(w, 400, "invalid endpoint")
			return
		}
		resp, err := integrationHTTPClient().Get(endpoint)
		if err != nil {
			errorJSON(w, 502, "endpoint failed")
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxIntegrationResponse+1))
		if err != nil || len(body) > maxIntegrationResponse {
			errorJSON(w, 502, "endpoint response too large")
			return
		}
		writeJSON(w, 200, map[string]any{"status": resp.StatusCode, "body": string(body)})
	})
	mux.HandleFunc("POST /integrations/{id}/hook", func(w http.ResponseWriter, r *http.Request) {
		id, ok := integrationID(r)
		if !ok {
			errorJSON(w, 404, "not found")
			return
		}
		signature, nonce := r.Header.Get("X-Hook-Signature"), r.Header.Get("X-Hook-Nonce")
		a.mu.Lock()
		defer a.mu.Unlock()
		in := a.integrations[id]
		if in == nil {
			errorJSON(w, 404, "not found")
			return
		}
		if signature == "" || len(signature) != len(in.Secret) || subtle.ConstantTimeCompare([]byte(signature), []byte(in.Secret)) != 1 {
			errorJSON(w, 403, "forbidden")
			return
		}
		if strings.TrimSpace(nonce) == "" {
			errorJSON(w, 400, "nonce required")
			return
		}
		if _, used := in.Nonces[nonce]; used {
			errorJSON(w, 409, "nonce already used")
			return
		}
		in.Nonces[nonce] = struct{}{}
		in.Events++
		_, _ = fmt.Fprint(w, "accepted")
	})
}
