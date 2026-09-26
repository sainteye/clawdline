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
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/adapters/projectlinks"
	psync "github.com/sainteye/clawdline/internal/adapters/projectsync"
	"github.com/sainteye/clawdline/internal/adapters/skillmenu"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/subagents"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/projectsync"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type Server struct {
	cfg       config.Config
	proxy     *httputil.ReverseProxy
	inventory app.Inventory
	store     *store.Store
	terminals []ports.TerminalHost
	// facts holds the model and spend read out of each record, keyed on the
	// file's size and time, so the status line's minute-by-minute read of an
	// unchanged session opens nothing.
	facts *transcript.RecordFacts
	// icons derives each project's mark by the same rules the Swift app uses,
	// reading the same registry, so the two draw the same creature.
	icons *icon.Registry
	// projectSync offers this machine's project settings and mirrors another's.
	projectSync *psync.Service
	// swift reads the Swift app's store, and only reads it: which session is
	// Clawdfather, which task opened a tab, who waits on whom, what was
	// delivered. See internal/adapters/swiftstore for the rules it keeps.
	swift *swiftstore.Store
	// pictures is this daemon's own picture stores, the pasteboard a send
	// lends pictures to, and the Swift app's picture store, read-only.
	pictures pictures
	// thumbs holds the reference-image thumbnails Board cards and to-do rows
	// ask for, at most the `cache.image_thumbs` row's limit of bytes. Nil
	// caches nothing: every thumbnail is drawn when it is asked for.
	thumbs *artifacts.ThumbnailCache
	// skillMenu holds each session's slash-menu skills for five minutes
	// (skills.go), at most the `cache.session_skills` row's limit of them.
	skillMenu *skillmenu.Cache
	// links holds where each project can be opened (links.go), one reading per
	// working directory, at most the `cache.session_links` row's limit of
	// them. A held reading is served however old it is and refreshed behind
	// the request, so the walk's one subprocess is never on a request a person
	// is waiting on twice.
	links *projectlinks.Cache
	// agents reads provider-native background threads. Broker children are
	// already first-class records and are joined with these in the console.
	agents *subagents.Reader
	// lastScreen is the sessions the last list was built from, so a task list
	// can place a task under its root without scanning the machine again.
	lastScreen atomic.Pointer[screenReading]
	// screens is the one owner of this daemon's `pipe-pane` state: who is
	// watching which terminal, and the leases that decide whether a pipe stays
	// on a pane. Nothing else may attach or detach one.
	screens *app.Screens
	// readings is the one producer of this machine's session reading. Every
	// loop that used to scan for itself — the event stream, the Cloud
	// publisher, the broker's beat — reads it instead, and each says whether
	// it may be answered from the held reading (`reading`) or must have one
	// taken for it (`freshReading`). See internal/app/inventory_reading.go.
	readings *app.InventoryReading
	// restore records which conversations this boot has open, from every
	// scan readings takes, and offers the previous boot's back
	// (session_restore.go, docs/session-restore.md).
	restore *app.SessionRestore
	// screenBus carries a moved screen's revision to every open event stream.
	screenBus *screenBus
	// broker is the loop from a root asking for work to a child reporting that
	// the work is done: /v1/orchestrator/*. It owns this daemon's own task
	// records — the Swift store is read for the Swift app's tasks and never
	// written (plan.md §4).
	broker *orchestrator.Broker
	// beat is the broker's account of its last pass, read by /v1/diagnostics.
	beat atomic.Pointer[orchestrator.Pulse]
	// pulse is the scheduler's own account of its last pass, read by
	// /v1/diagnostics.
	pulse atomic.Pointer[app.Pulse]
	tick  time.Duration
	// retired counts the calls the retired workflow route still gets
	// (workflow.go), for /v1/diagnostics.
	retired retiredWorkflow
	// planIntent is the paid model seam behind /v1/intents. Tests replace it
	// with a deterministic draft and never spend an account's quota.
	planIntent func(context.Context, string, []planner.Place, []string) (contract.IntentDraft, error)
	// nameSession is the paid model seam behind /v1/sessions/{id}/smart-title.
	// firstSessionRequest is the record-reading seam before it. Tests replace
	// both, so verification never consumes an assistant account's quota.
	nameSession         func(context.Context, string, string) (string, error)
	firstSessionRequest func(session.Session) (string, error)
	sessionTailRead     func(session.Session) (transcript.Page, error)
	intentContext       func(context.Context) ([]planner.Place, []string)
	intentMu            sync.Mutex
	intentRun           sync.Mutex
	intentQueued        int
}

// servedBy names which implementation answered. It is how a reader tells this
// daemon from the Swift app that held the same port until 2026-09-19, in a
// transcript, a log or a capture taken while both existed.
const servedBy = "clawdline-go"

func New(cfg config.Config) (*Server, error) {
	// No upstream is the ordinary case, and then there is no proxy at all:
	// nothing can accidentally forward to a port nobody is listening on.
	var proxy *httputil.ReverseProxy
	if port, ok := cfg.Upstream(); ok {
		upstream, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err != nil {
			return nil, err
		}
		proxy = httputil.NewSingleHostReverseProxy(upstream)
		// Server-sent events must reach the client as they arrive. Without this
		// the transport buffers and the console's stream looks dead.
		proxy.FlushInterval = -1
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			// A refusal has to name which hop failed. "The upstream is not
			// running" and "this daemon is broken" are different problems for
			// the reader.
			writeRefusalAbout(w, http.StatusBadGateway, "upstream_unreachable", err.Error(),
				contract.Refusal{Upstream: upstream.String()})
		}
	}
	// A store that cannot be opened is a refusal at startup, not a daemon that
	// runs without durability and discovers it later.
	st, err := store.Open(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("could not open the store at %s: %w", cfg.Dir, err)
	}
	icons, err := icon.NewRegistryWithOverrides(cfg.Dir)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("could not open project icons: %w", err)
	}
	mirror, err := projectsync.OpenMirror(cfg.Dir)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("could not open the project mirror: %w", err)
	}
	icons.SetMirror(mirrorLookup(mirror))
	home, _ := os.UserHomeDir()
	agents := subagents.New(home)
	srv := &Server{
		store:     st,
		cfg:       cfg,
		proxy:     proxy,
		terminals: terminal.Hosts(),
		facts:     transcript.NewRecordFacts(),
		icons:     icons,
		swift:     swiftstore.Open(swiftstore.Dir()),
		pictures:  newPictures(cfg.Dir),
		thumbs:    artifacts.NewThumbnailCache(int(CapacityLimit(capacity.CacheImageThumbs))),
		skillMenu: skillmenu.NewCache(),
		links:     projectlinks.NewCache(),
		agents:    agents,
		inventory: app.Inventory{
			Process:   process.New(),
			Terminals: terminal.Hosts(),
			Identity:  transcript.NewHost(),
			Agents:    agents,
			Screen:    terminal.NewScreens(),
			// The list's screens are held and refreshed behind the answer; the
			// live reader above stays what a keystroke and the broker read.
			Held: app.NewHeldScreens(terminal.NewScreens()),
			// And the bound on how many of its rows one reading may ask an
			// activity time of.
			Activity: app.NewActivityReads(),
		},
	}
	localPlanner := planner.New()
	localPlanner.Timeout = time.Duration(CapacityLimit(capacity.IntentPlannerSeconds)) * time.Second
	srv.planIntent = localPlanner.Draft
	srv.nameSession = localPlanner.Name
	// One producer in front of it, so three loops are one scan.
	srv.readings = app.NewInventoryReading(srv.inventory.Read, 0)
	srv.readings.SetRetentionAge(CapacityLimit(capacity.CacheSessionInventory))
	srv.restore = srv.newSessionRestore()
	srv.readings.Observe(srv.restore.Observe)
	srv.inventory.Held.SetLimits(CapacityLimit(capacity.ScreensCaptureSlots),
		CapacityLimit(capacity.CacheTerminalScreens))
	// The transcript caches and the skills cache hold their register rows' limits.
	srv.skillMenu.SetLimit(CapacityLimit(capacity.CacheSessionSkills))
	srv.links.SetLimit(CapacityLimit(capacity.CacheSessionLinks))
	srv.agents.SetLimit(CapacityLimit(capacity.CacheBackgroundAgents))
	srv.inventory.Activity.SetLimit(CapacityLimit(capacity.SessionsActivityReads))
	if h, ok := srv.inventory.Identity.(*transcript.Host); ok {
		h.Titles().SetLimit(CapacityLimit(capacity.CacheTranscriptTitles))
		h.Movements().SetLimit(CapacityLimit(capacity.CacheSessionActivity))
	}
	// The live screens, and the FIFO directory that is their ownership record.
	// A pane this daemon piped and did not take back is a `%N.fifo` left in
	// there, which is why the directory is under this daemon's own state and
	// not under a temporary one somebody else may empty.
	srv.broker = newBroker(srv)
	srv.screenBus = newScreenBus()
	hosts := terminal.Hosts()
	screenDir := filepath.Join(cfg.Dir, "screens")
	srv.screens = app.NewScreens(hosts, terminal.NewPaneSignal(screenDir, terminal.NewTmux()),
		srv.screenBus.publish)
	// Panes piped by a previous run, taken back. Once, at start, and off the
	// startup path: it is a subprocess, and a daemon that cannot reach tmux
	// must still come up.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if taken := srv.screens.Reclaim(ctx); len(taken) > 0 {
			log.Printf("live screens: took back %d pipe(s) from a previous run: %v", len(taken), taken)
		}
	}()
	srv.projectSync = srv.newProjectSync(mirror)
	return srv, nil
}

// reading is this machine's session list, as it was taken a moment ago.
//
// Every route that draws or projects rows reads this. It may be answered from
// the held reading while that is younger than app.InventoryTTL, which is what
// makes one scan serve the console, the Cloud publisher and the broker's pass
// at once instead of three.
func (s *Server) reading(ctx context.Context) session.Inventory {
	if s.readings == nil {
		return s.inventory.Read(ctx)
	}
	return s.readings.Recent(ctx)
}

// freshReading is a reading taken for this call.
//
// It is for the two callers that decide something from it rather than draw it:
// the broker's beat, which settles a task and closes a session when a child's
// tab is no longer there, and a person's action, which names a session and
// then types into it. Neither may be answered from a snapshot that had already
// finished before they asked (docs/design-decisions.md D05 ③).
func (s *Server) freshReading(ctx context.Context) session.Inventory {
	if s.readings == nil {
		return s.inventory.Read(ctx)
	}
	return s.readings.Fresh(ctx)
}

func (s *Server) Handler() http.Handler {
	// Every route is behind the gate, with no exception for loopback: see gate.go.
	gate := s.gate()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/", s.authRoute)
	mux.HandleFunc("/v1/health", s.health)
	mux.HandleFunc("/v1/diagnostics", s.diagnostics)
	mux.HandleFunc("/v1/capacity", s.capacityRoute)
	// What the line to app.clawdline.com is doing (cloud.go). This machine's
	// own token only.
	mux.HandleFunc("/v1/cloud/status", s.cloudStatusRoute)
	// Pairing a browser with this Mac, and throwing one out again. Same rule:
	// this machine's own token, because the first of them answers a link that
	// hands over the account key.
	mux.HandleFunc("/v1/cloud/pairing", s.cloudPairingRoute)
	mux.HandleFunc("/v1/cloud/pairing/offer", s.cloudPairingOfferRoute)
	mux.HandleFunc("/v1/cloud/devices/revoke", s.cloudDeviceRoute)
	mux.HandleFunc("/v1/cloud/keys/rotate", s.cloudRotateRoute)
	// The account-free way in from outside: what this daemon's cloudflared is
	// doing (tunnel.go). This machine's own token only — a quick tunnel's
	// address is its secret.
	mux.HandleFunc("/v1/tunnel", s.tunnelRoute)
	mux.HandleFunc("/v1/sessions", s.sessions)
	// Everything under a session id is about that session: its info, which is
	// a read, and otherwise an action on it. One handler rather than a route
	// each, because the id is a path segment and Go's mux matches prefixes,
	// not patterns.
	mux.HandleFunc("/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		// The sessions a reboot took away: three fixed paths, asked before
		// anything reads the next segment as a session id.
		if s.restorableRoute(w, r) {
			return
		}
		if sessionID, agentID, ok := agentPath(r); ok {
			s.sessionAgentRoute(w, r, sessionID, agentID)
			return
		}
		if sessionID, shellID, ok := shellPath(r); ok {
			s.sessionShellRoute(w, r, sessionID, shellID)
			return
		}
		if id, ok := infoPath(r); ok {
			s.sessionInfoRoute(w, r, id)
			return
		}
		// Watching a screen is a read and brings its own route (screen.go);
		// sessionAction below refuses anything that is not a POST, so this has
		// to be asked before it.
		if id, ok := screenPath(r); ok {
			s.sessionScreenRoute(w, r, id)
			return
		}
		// A file patch is the nested Git read and must be recognised before the
		// repository summary beside it.
		if id, ok := gitDiffPath(r); ok {
			s.sessionGitDiffRoute(w, r, id)
			return
		}
		// A read as well (git.go): what the session's repository has changed,
		// asked for when its panel opens.
		if id, ok := gitPath(r); ok {
			s.sessionGitRoute(w, r, id)
			return
		}
		if id, ok := focusPath(r); ok {
			s.sessionFocusRoute(w, r, id)
			return
		}
		// A read (todos.go): the session detail's to-do panel.
		if id, ok := todosPath(r); ok {
			s.sessionTodosRead(w, r, id)
			return
		}
		// A read (skills.go): the slash menu, asked for when `/` opens it.
		if id, ok := skillsPath(r); ok {
			s.sessionSkillsRoute(w, r, id)
			return
		}
		// A read (links.go): everything this project has an address for,
		// asked for when the Links sheet opens.
		if id, ok := linksPath(r); ok {
			s.sessionLinksRoute(w, r, id)
			return
		}
		s.sessionAction(w, r)
	})
	// What this feature has done to the machine, published (screen.go).
	mux.HandleFunc("/v1/screens", s.screensRoute)
	mux.HandleFunc("/v1/events", s.events)
	// Two `/v1/next/` names are still the only spelling of what they serve,
	// and the Dashboard reads both: what is owed (obligations.go, now the
	// broker's own records, D01) and the machine coordinator. The shadows
	// that had a real name beside them — sessions, schedules, board — are
	// gone (D07): a second spelling nobody reads is a second thing to keep
	// in step with nothing.
	mux.HandleFunc("/v1/next/obligations", s.obligations)
	mux.HandleFunc("/v1/next/coordinator", s.coordinatorRoute)

	mux.HandleFunc("/v1/board", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.boardWrite(w, r)
			return
		}
		s.boardRead(w, r)
	})
	mux.HandleFunc("/v1/board/tracks", s.boardTracks)
	// The new board and its Backlog (work.go, design-decisions T3): the
	// three structures' own resource, beside the old cards' read-only view.
	mux.HandleFunc("/v1/work/", s.workRoute)
	// Where a person takes part: proposals, decisions, digests (proposals.go, T4).
	s.participationRoutes(mux)
	mux.HandleFunc("/v1/orchestrator/schedules", s.schedules)
	// One schedule, its save, its removal and its run; the Cloud bind
	// command's local half; and moving schedules in and out (schedules.go).
	mux.HandleFunc("/v1/orchestrator/schedules/", s.scheduleRoute)
	mux.HandleFunc("/v1/orchestrator/schedule-webhooks/bind", s.scheduleWebhookBindRoute)
	mux.HandleFunc("/v1/orchestrator/schedule-imports", s.scheduleImportRoute)
	mux.HandleFunc("/v1/orchestrator/schedule-exports", s.scheduleExportRoute)
	// The text somebody wrote once and presses instead of typing it again
	// (snippets.go). This daemon's own store, never the Swift app's directory.
	mux.HandleFunc("/v1/snippets", s.snippetsRoute)
	mux.HandleFunc("/v1/snippets/", s.snippetRoute)
	mux.HandleFunc("/v1/orchestrator/tasks", s.tasksRoute)
	// The broker (orchestrator.go): everything under a task id, plus the five
	// routes beside it. A child's own routes are let through the gate by
	// `taskSecretRoute` and are authenticated by these handlers.
	mux.HandleFunc("/v1/orchestrator/tasks/", s.orchestratorTaskRoute)
	mux.HandleFunc("/v1/orchestrator/inventory", s.brokerInventory)
	mux.HandleFunc("/v1/orchestrator/inflight", s.brokerInflight)
	mux.HandleFunc("/v1/orchestrator/messages", s.brokerMessages)
	mux.HandleFunc("/v1/orchestrator/whoami", s.brokerWhoAmI)
	// What a run was: the message a person sent a session, which a relay of
	// their words names (runs.go).
	mux.HandleFunc("/v1/orchestrator/runs/", s.runRoute)
	// The address book a wait, a relay or a handoff names sessions from, and
	// under a session: its delivery receipt, its to-dos and the retired
	// workflow route (workflow.go).
	mux.HandleFunc("/v1/orchestrator/sessions", s.brokerAddressBook)
	mux.HandleFunc("/v1/orchestrator/sessions/", s.brokerSessionRoute)
	// What each assistant's account has left, every pending landing, a root's
	// own notification, and the durable-report promotion this daemon does not
	// keep (orchestrator.go).
	mux.HandleFunc("/v1/orchestrator/assistants", s.brokerAssistants)
	mux.HandleFunc("/v1/orchestrator/landings", s.brokerLandings)
	mux.HandleFunc("/v1/orchestrator/notify", s.brokerMachineNotify)
	mux.HandleFunc("/v1/orchestrator/durable-reports/promotions", s.brokerDurableReportPromotion)
	// The coordination plane (W5): the machine role, file waits, leases (the
	// compile slot and landing), the completion ledger's manual path, and
	// detached automation (coordinator.go, waits.go).
	mux.HandleFunc("/v1/orchestrator/coordinator", s.orchestratorCoordinatorRoute)
	mux.HandleFunc("/v1/orchestrator/coordinator/", s.orchestratorCoordinatorRoute)
	mux.HandleFunc("/v1/orchestrator/waits", s.waitsRoute)
	mux.HandleFunc("/v1/orchestrator/waits/", s.waitsRoute)
	mux.HandleFunc("/v1/orchestrator/leases", s.leasesRoute)
	mux.HandleFunc("/v1/orchestrator/leases/", s.leasesRoute)
	mux.HandleFunc("/v1/orchestrator/completions", s.completionsRoute)
	mux.HandleFunc("/v1/orchestrator/completions/", s.completionsRoute)
	mux.HandleFunc("/v1/orchestrator/detached-tasks", s.brokerDetached)
	// The hand-over plane (handoffs.go, W6).
	mux.HandleFunc("/v1/orchestrator/handoffs", s.handoffsRoute)
	mux.HandleFunc("/v1/orchestrator/handoffs/", s.handoffsRoute)
	mux.HandleFunc("/v1/orchestrator/root-assignments", s.rootAssignmentsRoute)
	mux.HandleFunc("/v1/orchestrator/root-assignments/", s.rootAssignmentsRoute)
	mux.HandleFunc("/v1/orchestrator/graphs", s.graphsRoute)
	mux.HandleFunc("/v1/orchestrator/reclaim", s.reclaimRoute)
	mux.HandleFunc("/v1/strings", s.strings)
	mux.HandleFunc("/v1/settings", s.settingsRoute)
	mux.HandleFunc("/v1/places", s.placesRoute)
	// The input bar's server list (devstacks.go): what the projects' own
	// `.devstack.json` files declare, and which declared ports answer.
	mux.HandleFunc("/v1/devstacks", s.devStacksRoute)
	// Starting and resuming a session in a place, and what was said there (start.go).
	mux.HandleFunc("/v1/places/", s.placeRoute)
	mux.HandleFunc("/v1/projects", s.projectCatalogRoute)
	mux.HandleFunc("/v1/projects/", s.projectsRoute)
	// Project settings a source machine offers and a mirror applies (project_sync.go).
	mux.HandleFunc("/v1/project-sync/", s.projectSyncRoute)
	// The Project Timeline reads this daemon's own history and stores nothing.
	mux.HandleFunc("/v1/timeline", s.timelineRoute)
	mux.HandleFunc("/v1/transcript", s.transcriptRoute)
	// The token ledger, read by a person or a session (usage.go).
	mux.HandleFunc("/v1/usage/", s.usageRoute)
	// Things waiting to be verified, kept until the person settles them (verify.go).
	mux.HandleFunc("/v1/verifications", s.verificationsRoute)
	mux.HandleFunc("/v1/verifications/", s.verificationsRoute)
	// Pictures: stored by a session (machine token), read by id (images.go).
	mux.HandleFunc("/v1/artifacts/images", s.imagesRoute)
	mux.HandleFunc("/v1/artifacts/images/", s.imageRoute)
	// Said out loud rather than typed (voice.go). Not a session route and not
	// a send: this machine transcribes and answers with the text, and what
	// happens to it afterwards is the composer's business.
	mux.HandleFunc("/v1/voice", s.voiceRoute)
	mux.HandleFunc("/v1/voice/language", s.voiceLanguageRoute)
	// One transcribed sentence becomes an editable draft. This spends one
	// local CLI model turn but starts no session (intent.go).
	mux.HandleFunc("/v1/intents", s.intentRoute)
	// Web Push: the key, the subscription, the test and the way back out
	// (push.go). Read-level, as in the Swift app.
	mux.HandleFunc("/v1/push/", s.pushRoute)
	// By default this daemon refuses what it has not implemented instead of
	// borrowing it. Proxying is a scaffold, and a scaffold that never says what
	// it is holding up cannot be removed on purpose — so it is asked for by
	// name (config.UpstreamPortEnv) and is off otherwise.
	if !s.proxying() {
		if root := WebRoot(); root != "" {
			mux.Handle("/app/", newPage(root))
			// The home-screen shell in front of the console (pwa.go): the
			// launch images are drawn, and everything else there is a file in
			// the bundle that `page` already serves.
			mux.Handle("/", s.withPWA(&fallback{page: newPage(root), miss: s.notImplemented}))
			return gate.wrap(s.withDocuments(boundBodies(mux)))
		}
		// No web root is not a route this daemon has yet to write: `/` is its
		// own, and it was not told where the files are. The page says that by
		// name (no_web_root); an unowned /v1 route still says not_implemented.
		mux.Handle("/", &fallback{page: newPage(""), miss: s.notImplemented})
		return gate.wrap(s.withDocuments(boundBodies(mux)))
	}
	mux.Handle("/", s.proxy)
	return gate.wrap(s.withDocuments(boundBodies(mux)))
}

// health is the first route this daemon owns. It answers for itself and says so,
// so that a reader can tell which of the two daemons replied.
//
// It is on the gate's open list, so it says nothing but that: no path, no
// port, nothing about the work. What a person diagnosing this machine wants is
// at /v1/diagnostics, behind this machine's own token.
//
// Two answers are about the door rather than the machine, and the Swift page
// reads both (net/live.js `check`): `authed`, whether the credential this
// request carried is one this machine lets in — the one way a page can tell
// "not let in" from "not running", since an event stream fails without a
// reason — and `password`, whether there is a password door to offer. Both
// come from the gate that judged this very request.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	a := accessOf(r)
	h := contract.Health{
		OK:       true,
		ServedBy: servedBy,
		At:       time.Now().Unix(),
		Authed:   a.verdict.Allowed,
		Password: a.gate != nil && a.gate.auth != nil && a.gate.auth.HasPassword(),
	}
	s.brokerHealth(&h)
	s.capacityHealth(&h)
	s.workHealth(&h)
	writeJSON(w, h)
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
	broker := s.brokerDiagnostics(r.Context())
	capacity, capacityOK := s.capacityDiagnostics()
	writeJSON(w, contract.Diagnostics{
		OK:        (broker == nil || !broker.Beat.Stalled) && capacityOK,
		Broker:    broker,
		Capacity:  capacity,
		Console:   consoleReading(WebRoot()),
		Platform:  s.platformDiagnostics(r.Context()),
		Proposals: s.proposalDiagnostics(r.Context()),
		Scheduler: s.schedulerPulse(),
		ServedBy:  servedBy,
		Port:      int64(s.cfg.Port),
		Upstream:  int64(s.cfg.UpstreamPort),
		Usage:     s.usageDiagnostics(),
		Dir:       s.cfg.Dir,
		At:        time.Now().Unix(),
	})
}

// StartScheduler runs the clock that fires stored templates. It is started by
// the daemon rather than by the first request, because a schedule nobody
// happens to visit is still due.
func (s *Server) StartScheduler(ctx context.Context) {
	tick := schedulerTick()
	s.tick = tick
	go app.Scheduler{
		Book:   s.scheduleBook(),
		Tick:   tick,
		Report: func(p app.Pulse) { s.pulse.Store(&p) },
	}.Run(ctx)
	log.Printf("scheduler ticking every %s", tick)
	// Reference-image files no row names: swept at Open, then on this clock.
	go s.store.SweepReferenceImagesEvery(ctx, store.ReferenceImageSweepIntervalLimit)
}

// schedulerTick is the clock's period: a minute, or CLAWDLINE_NEXT_TICK. The
// capacity beat keeps the same one (docs/limits.md §4.6).
func schedulerTick() time.Duration {
	if v := os.Getenv("CLAWDLINE_NEXT_TICK"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return time.Minute
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

// standaloneForced is CLAWDLINE_NEXT_STANDALONE=1, which says "never forward"
// whatever else is configured.
//
// It was the way to opt out while 7717 — the port the Swift app held until
// 2026-09-19 — was the default destination. Forwarding is now opt-in, so on an
// ordinary machine this changes nothing and setting it is harmless; it is still
// read so that a script or a bundle written before that flip keeps meaning what
// it meant.
func standaloneForced() bool { return os.Getenv("CLAWDLINE_NEXT_STANDALONE") == "1" }

// proxying reports whether there is another daemon behind this one to forward
// an unowned route to. False is the ordinary answer: an unowned route then
// names itself rather than borrowing an answer.
func (s *Server) proxying() bool {
	if s.proxy == nil {
		return false
	}
	if _, ok := s.cfg.Upstream(); !ok {
		return false
	}
	return !standaloneForced()
}

// forwardUpstream hands one request to the daemon behind this one.
//
// Every route that used to reach for `s.proxy` directly comes through here, so
// that "there is nobody behind me" is one answer written once, by name, instead
// of a nil dereference or a 502 pointing at a port nobody holds.
func (s *Server) forwardUpstream(w http.ResponseWriter, r *http.Request) {
	if !s.proxying() {
		s.notImplemented(w, r)
		return
	}
	s.proxy.ServeHTTP(w, r)
}

// notImplemented answers a route this daemon does not own yet, by name.
//
// The list of these is exactly what P4 costs, and it is measured rather than
// estimated: run the console against this daemon with nothing behind it and
// read what it asks for.
func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request) {
	log.Printf("not implemented: %s %s", r.Method, r.URL.Path)
	writeRefusalAbout(w, http.StatusNotImplemented, "not_implemented",
		"this daemon does not own that route yet", contract.Refusal{Route: r.URL.Path})
}

// Listen binds the daemon's address, and is the first thing `serve` does.
//
// Binding used to be the last. Everything before it had already run: the
// broker's beat, the cloud line, the tunnel's Reclaim — which stops whatever
// cloudflared the state directory's pid file names, the running daemon's own
// included — and a `listening` line in the shared log from a daemon that was
// about to fail to listen. On 2026-09-21 that line was written at 10:52:13 by a
// daemon that never held the port. A daemon that cannot have the port now
// finds out before it has done anything else.
func Listen(cfg config.Config) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port))
}

// Serve answers on a listener Listen bound.
func (s *Server) Serve(ln net.Listener) error {
	addr := ln.Addr().String()
	if s.cfg.Host != "127.0.0.1" && s.cfg.Host != "localhost" {
		// Said out loud, once, in the log a person reads when something is
		// wrong: this daemon is reachable from outside this machine.
		log.Printf("WARNING: binding to %s, which is not loopback", s.cfg.Host)
	}
	if port, ok := s.cfg.Upstream(); ok && !standaloneForced() {
		log.Printf("clawdline-go listening on http://%s (forwarding unowned routes to :%d, asked for with %s)",
			addr, port, config.UpstreamPortEnv)
	} else {
		log.Printf("clawdline-go listening on http://%s (nothing behind it: an unowned route answers 501 not_implemented and names itself)", addr)
		// Directly under `listening`, because that is the line somebody reads
		// after a restart, and whether this address shows a page is the thing
		// they restarted it for. A forwarding daemon's `/` is the upstream's.
		log.Print(consoleLogLine(consoleReading(WebRoot())))
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.Serve(ln)
}
