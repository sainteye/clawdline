// Package http is an inbound adapter: it decodes requests, authenticates them
// and calls application commands. It owns no domain state, and nothing in
// internal/domain may import it.
//
// While the rewrite is in progress this adapter also proxies every route it has
// not taken over to the Swift app. That is scaffolding, not shipped behaviour:
// P4 removes it.
package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/sainteye/clawdline-go/internal/config"
)

type Server struct {
	cfg   config.Config
	proxy *httputil.ReverseProxy
}

func New(cfg config.Config) (*Server, error) {
	upstream, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.UpstreamPort))
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	// Server-sent events must reach the client as they arrive. Without this the
	// transport buffers and the console's stream looks dead.
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// A refusal has to name which hop failed. "The upstream is not running"
		// and "this daemon is broken" are different problems for the reader.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":    "upstream_unreachable",
			"upstream": upstream.String(),
			"detail":   err.Error(),
		})
	}
	return &Server{cfg: cfg, proxy: proxy}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.health)
	mux.Handle("/", s.proxy)
	return mux
}

// health is the first route this daemon owns. It answers for itself and says so,
// so that a reader can tell which of the two daemons replied.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"served_by": "clawdline-go",
		"port":     s.cfg.Port,
		"upstream": s.cfg.UpstreamPort,
		"dir":      s.cfg.Dir,
		"at":       time.Now().Unix(),
	})
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	log.Printf("clawdline-go listening on http://%s (proxying to :%d)", addr, s.cfg.UpstreamPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}
