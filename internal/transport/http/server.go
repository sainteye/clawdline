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
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/process"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/icon"
)

type Server struct {
	cfg        config.Config
	proxy      *httputil.ReverseProxy
	inventory  app.Inventory
	store      *store.Store
	dispatcher app.Dispatcher
	terminals  []ports.TerminalHost
	// ledger remembers what has already been counted, so a transcript is read
	// once rather than once per request.
	ledger *transcript.Ledger
	// facts holds the model and spend read out of each record, keyed on the
	// file's size and time, so the status line's minute-by-minute read of an
	// unchanged session opens nothing.
	facts *transcript.RecordFacts
	// icons derives each project's mark by the same rules the Swift app uses,
	// reading the same registry, so the two draw the same creature.
	icons *icon.Registry
	// swift reads the Swift app's store, and only reads it: which session is
	// Clawdfather, which task opened a tab, who waits on whom, what was
	// delivered. See internal/adapters/swiftstore for the rules it keeps.
	swift *swiftstore.Store
	// lastScreen is the sessions the last list was built from, so a task list
	// can place a task under its root without scanning the machine again.
	lastScreen atomic.Pointer[screenReading]
	// pulse is the scheduler's own account of its last pass, read by
	// /v1/diagnostics.
	pulse atomic.Pointer[app.Pulse]
	tick  time.Duration
}

// servedBy names which implementation answered. It is how a reader tells the Go
// daemon from the Swift app when both can hold the same port.
const servedBy = "clawdline-go"

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
		writeRefusalAbout(w, http.StatusBadGateway, "upstream_unreachable", err.Error(),
			contract.Refusal{Upstream: upstream.String()})
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
		terminals: terminal.Hosts(),
		ledger:    transcript.NewLedger(),
		facts:     transcript.NewRecordFacts(),
		icons:     icon.NewRegistry(),
		swift:     swiftstore.Open(swiftstore.Dir()),
		inventory: app.Inventory{
			Process:   process.New(),
			Terminals: terminal.Hosts(),
			Identity:  transcript.NewHost(),
			Screen:    terminal.NewScreens(),
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	// Every route is behind the gate, with no exception for loopback: see gate.go.
	gate := s.gate()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/", s.authRoute)
	mux.HandleFunc("/v1/health", s.health)
	mux.HandleFunc("/v1/diagnostics", s.diagnostics)
	// A shadow route, not the real one. It runs beside /v1/sessions so both can
	// be read for the same machine at the same moment; the real route is taken
	// over only once the payloads agree.
	mux.HandleFunc("/v1/next/sessions", s.nextSessions)
	mux.HandleFunc("/v1/sessions", s.sessions)
	// Everything under a session id is about that session: its info, which is
	// a read, and otherwise an action on it. One handler rather than a route
	// each, because the id is a path segment and Go's mux matches prefixes,
	// not patterns.
	mux.HandleFunc("/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if id, ok := infoPath(r); ok {
			s.sessionInfoRoute(w, r, id)
			return
		}
		s.sessionAction(w, r)
	})
	mux.HandleFunc("/v1/events", s.events)
	mux.HandleFunc("/v1/next/obligations", s.obligations)
	mux.HandleFunc("/v1/next/schedules", s.schedules)
	mux.HandleFunc("/v1/next/coordinator", s.coordinatorRoute)

	// The console asks for these by their real names. They were served under
	// /v1/next/ while they were being proved beside the app; the shadow names
	// stay so a reader can still compare the two, but these are the routes.
	mux.HandleFunc("/v1/board", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.boardWrite(w, r)
			return
		}
		s.boardRead(w, r)
	})
	mux.HandleFunc("/v1/orchestrator/schedules", s.schedules)
	mux.HandleFunc("/v1/orchestrator/tasks", s.tasksRoute)
	mux.HandleFunc("/v1/orchestrator/tasks/", s.settleRoute)
	mux.HandleFunc("/v1/strings", s.strings)
	mux.HandleFunc("/v1/settings", s.settingsRoute)
	mux.HandleFunc("/v1/places", s.placesRoute)
	// Starting and resuming a session in a place, and what was said there (start.go).
	mux.HandleFunc("/v1/places/", s.placeRoute)
	mux.HandleFunc("/v1/projects", s.projectCatalogRoute)
	mux.HandleFunc("/v1/projects/", s.projectsRoute)
	mux.HandleFunc("/v1/orchestrator/usage", s.usageRoute)
	mux.HandleFunc("/v1/orchestrator/usage/analytics", s.usageAnalyticsRoute)
	mux.HandleFunc("/v1/orchestrator/usage/analytics.csv", s.usageAnalyticsRoute)
	mux.HandleFunc("/v1/orchestrator/usage/analytics.json", s.usageAnalyticsRoute)
	mux.HandleFunc("/v1/orchestrator/usage/project-worktrees", s.usageWorktreesRoute)
	mux.HandleFunc("/v1/transcript", s.transcriptRoute)
	mux.HandleFunc("/v1/next/board", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.boardWrite(w, r)
			return
		}
		s.boardRead(w, r)
	})
	// Standalone refuses what it has not implemented instead of borrowing it.
	// Proxying is a scaffold, and a scaffold that never says what it is holding
	// up cannot be removed on purpose.
	if standalone() {
		if root := WebRoot(); root != "" {
			mux.Handle("/app/", newPage(root))
			mux.Handle("/", &fallback{page: newPage(root), miss: s.notImplemented})
			return gate.wrap(s.withDocuments(mux))
		}
		mux.Handle("/", http.HandlerFunc(s.notImplemented))
		return gate.wrap(s.withDocuments(mux))
	}
	mux.Handle("/", s.proxy)
	return gate.wrap(s.withDocuments(mux))
}

// health is the first route this daemon owns. It answers for itself and says so,
// so that a reader can tell which of the two daemons replied.
//
// It is on the gate's open list, so it says nothing but that: no path, no
// port, nothing about the work. What a person diagnosing this machine wants is
// at /v1/diagnostics, behind this machine's own token.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, contract.Health{
		OK:       true,
		ServedBy: servedBy,
		At:       time.Now().Unix(),
	})
}

// diagnostics is what health used to say about this process: where its state
// is, which ports it holds, and the clock's last pass. Only this machine's own
// token reads it — a paired phone has no use for a path on this disk.
func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "Diagnostics are read with GET.")
		return
	}
	if !requireLocal(w, r) {
		return
	}
	writeJSON(w, contract.Diagnostics{
		OK:        true,
		Scheduler: s.schedulerPulse(),
		ServedBy:  servedBy,
		Port:      int64(s.cfg.Port),
		Upstream:  int64(s.cfg.UpstreamPort),
		Dir:       s.cfg.Dir,
		At:        time.Now().Unix(),
	})
}

// nextSessions publishes this daemon's own reading of the machine.
func (s *Server) nextSessions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	inv := s.inventory.Read(ctx)
	rows := make([]contract.InventorySession, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		rows = append(rows, contract.InventorySession{
			ID:             item.ID,
			Backend:        contract.Backend(item.Backend),
			TTY:            item.TTY,
			PID:            int64(item.PID),
			Assistant:      contract.Assistant(item.Assistant),
			CWD:            item.CWD,
			Label:          item.Label,
			State:          contract.SessionState(item.State),
			Evidence:       contract.Evidence(item.Evidence),
			ConversationID: item.ConversationID,
		})
	}
	writeJSON(w, contract.Inventory{
		ServedBy: servedBy,
		Sessions: rows,
		Scan: contract.InventoryScan{
			Complete:   inv.Complete,
			Provenance: inv.Provenance,
			Notes:      inv.Notes,
		},
		At: inv.ObservedAt.Unix(),
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
	s.tick = tick
	go app.Scheduler{
		Store:      s.store,
		Dispatcher: s.dispatcher,
		Tick:       tick,
		NewID:      newID,
		Report:     func(p app.Pulse) { s.pulse.Store(&p) },
	}.Run(ctx)
	log.Printf("scheduler ticking every %s", tick)
}

// schedulerPulse is what /v1/diagnostics says about the clock.
//
// Before the first pass there is no `at`, and that absence is the honest
// answer: a daemon thirty seconds old has not had a pass yet, and reporting a
// zero timestamp would read as a pass in 1970.
func (s *Server) schedulerPulse() contract.SchedulerPulse {
	out := contract.SchedulerPulse{TickSeconds: int64(s.tick / time.Second)}
	p := s.pulse.Load()
	if p == nil {
		return out
	}
	out.At = p.At.Unix()
	out.Considered = int64(p.Considered)
	out.Due = int64(p.Due)
	out.Fired = int64(p.Fired)
	out.Note = p.Note
	return out
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// fallback serves the console where it can and names what it cannot, so the
// list of unimplemented routes is produced by running the real client rather
// than by guessing at it.
type fallback struct {
	page *page
	miss http.HandlerFunc
}

func (f *fallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		f.miss(w, r)
		return
	}
	f.page.ServeHTTP(w, r)
}

// standalone reports whether this daemon runs without the Swift app behind it.
func standalone() bool { return os.Getenv("CLAWDLINE_NEXT_STANDALONE") == "1" }

// notImplemented answers a route this daemon does not own yet, by name.
//
// The list of these is exactly what P4 costs, and it is measured rather than
// estimated: run the console against a standalone daemon and read what it asks
// for.
func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request) {
	log.Printf("not implemented: %s %s", r.Method, r.URL.Path)
	writeRefusalAbout(w, http.StatusNotImplemented, "not_implemented",
		"this daemon does not own that route yet", contract.Refusal{Route: r.URL.Path})
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	if s.cfg.Host != "127.0.0.1" && s.cfg.Host != "localhost" {
		// Said out loud, once, in the log a person reads when something is
		// wrong: this daemon is reachable from outside this machine.
		log.Printf("WARNING: binding to %s, which is not loopback", s.cfg.Host)
	}
	log.Printf("clawdline-go listening on http://%s (proxying to :%d)", addr, s.cfg.UpstreamPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}
