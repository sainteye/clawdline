// Package http is an inbound adapter: it decodes requests, authenticates them
// and calls application commands. It owns no domain state, and nothing in
// internal/domain may import it.
//
// While the rewrite is in progress this adapter also proxies every route it has
// not taken over to the Swift app. That is scaffolding, not shipped behaviour:
// P4 removes it.
package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/process"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/config"
)

type Server struct {
	cfg        config.Config
	proxy      *httputil.ReverseProxy
	inventory  app.Inventory
	store      *store.Store
	dispatcher app.Dispatcher
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
	// A store that cannot be opened is a refusal at startup, not a daemon that
	// runs without durability and discovers it later.
	st, err := store.Open(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("could not open the store at %s: %w", cfg.Dir, err)
	}
	return &Server{
		store: st,
		cfg:   cfg,
		proxy: proxy,
		dispatcher: app.Dispatcher{
			Store:    st,
			Tasks:    taskdir.New(cfg.Dir),
			Terminal: terminal.NewTmux(),
		},
		inventory: app.Inventory{
			Process:   process.New(),
			Terminals: terminal.Hosts(),
			Identity:  transcript.NewHost(),
			Screen:    terminal.NewTmux(),
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.health)
	// A shadow route, not the real one. It runs beside /v1/sessions so both can
	// be read for the same machine at the same moment; the real route is taken
	// over only once the payloads agree.
	mux.HandleFunc("/v1/next/sessions", s.nextSessions)
	mux.HandleFunc("/v1/sessions", s.sessions)
	mux.HandleFunc("/v1/events", s.events)
	mux.HandleFunc("/v1/next/obligations", s.obligations)
	mux.HandleFunc("/v1/next/schedules", s.schedules)
	mux.HandleFunc("/v1/next/coordinator", s.coordinatorRoute)
	mux.HandleFunc("/v1/next/board", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.boardWrite(w, r)
			return
		}
		s.boardRead(w, r)
	})
	mux.Handle("/", s.proxy)
	return mux
}

// health is the first route this daemon owns. It answers for itself and says so,
// so that a reader can tell which of the two daemons replied.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"served_by": "clawdline-go",
		"port":      s.cfg.Port,
		"upstream":  s.cfg.UpstreamPort,
		"dir":       s.cfg.Dir,
		"at":        time.Now().Unix(),
	})
}

// nextSessions publishes this daemon's own reading of the machine.
func (s *Server) nextSessions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if h, ok := s.inventory.Identity.(*transcript.Host); ok {
		h.Refresh()
	}
	inv := s.inventory.Read(ctx)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"served_by": "clawdline-go",
		"sessions":  inv.Sessions,
		"scan": map[string]any{
			"complete":   inv.Complete,
			"provenance": inv.Provenance,
			"notes":      inv.Notes,
		},
		"at": inv.ObservedAt.Unix(),
	})
}

// StartScheduler runs the clock that fires stored templates. It is started by
// the daemon rather than by the first request, because a schedule nobody
// happens to visit is still due.
func (s *Server) StartScheduler(ctx context.Context) {
	tick := time.Minute
	if v := os.Getenv("CLAWDLINE_NEXT_TICK"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			tick = d
		}
	}
	go app.Scheduler{
		Store:      s.store,
		Dispatcher: s.dispatcher,
		Tick:       tick,
		NewID:      newID,
	}.Run(ctx)
	log.Printf("scheduler ticking every %s", tick)
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
