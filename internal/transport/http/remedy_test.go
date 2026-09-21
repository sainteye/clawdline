package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// A remedy names a route this daemon serves and a credential its gate reads
// (broker-design #37). The Swift app's printed curl sent the landing route a
// header the server did not read (`c9e89ce6`); here the table remedies are
// built from is checked against the router's registrations and the gate's
// own header names, so that cannot be written without a test going red.
func TestEveryRemedyNamesARouteThisDaemonServes(t *testing.T) {
	if machineHeader != orchestrator.HeaderMachine {
		t.Fatalf("the gate reads %q, remedies send %q", machineHeader, orchestrator.HeaderMachine)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(orchestrator.HeaderTaskSecret, "x")
	if taskSecret(req) != "x" {
		t.Fatalf("the task routes do not read %q", orchestrator.HeaderTaskSecret)
	}
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	patterns := []string{}
	for _, m := range regexp.MustCompile(`mux\.HandleFunc\("([^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		patterns = append(patterns, m[1])
	}
	served := func(route string) bool {
		for _, p := range patterns {
			if p == route || (strings.HasSuffix(p, "/") && strings.HasPrefix(route, p)) {
				return true
			}
		}
		return false
	}
	if len(orchestrator.Remedies) == 0 {
		t.Fatal("no remedies")
	}
	for code, r := range orchestrator.Remedies {
		// A row that names no route is a refusal this build cannot clear. It
		// owes a reason instead, because "no way out" with nothing after it
		// is the silence this whole table was written against.
		if r.Route == "" {
			if strings.TrimSpace(r.Because) == "" {
				t.Errorf("%s offers no request and says nothing about why", code)
			}
			if r.Method != "" || r.Header != "" || len(r.Query) > 0 || len(r.Body) > 0 {
				t.Errorf("%s names no route and still describes a request: %+v", code, r)
			}
			continue
		}
		if !served(r.Route) {
			t.Errorf("%s names %s, which no mux registration serves", code, r.Route)
		}
		if r.Header == orchestrator.HeaderMachine && !machineScoped(r.Route) {
			t.Errorf("%s sends the orchestrator token to %s, which the gate does not accept it on", code, r.Route)
		}
	}
	// The control: a route nobody registered is caught.
	if served("/v1/orchestrator/no-such-route") {
		t.Fatal("the check passes a route that does not exist")
	}
}

// remedyHandlers is each remedy route served by the handler the mux registers
// for it. The test below fails when a remedy names a route that is not here,
// so a new way out cannot be added without something that serves it.
func remedyHandlers(s *Server) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/v1/orchestrator/inventory": s.brokerInventory,
		"/v1/orchestrator/handoffs":  s.handoffsRoute,
		"/v1/orchestrator/reclaim":   s.reclaimRoute,
	}
}

// remedyServer is a server with just enough behind it to answer a route.
func remedyServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}, store: st}
}

func serveRemedy(s *Server, h http.HandlerFunc, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader("{}"))
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true}))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// Following a remedy has to arrive somewhere. A route being registered is not
// the same fact as a route doing anything: `/v1/orchestrator/coordinator/` is
// one prefix registration, and under it `…/successions` answers 501 — which is
// what the succession remedy sent people to until this test existed. So every
// remedy that names a route is served here, and a 501 fails.
func TestFollowingEveryRemedyReachesARouteThatIsImplemented(t *testing.T) {
	s := remedyServer(t)
	handlers := remedyHandlers(s)
	for code, r := range orchestrator.Remedies {
		if r.Route == "" {
			continue
		}
		h, ok := handlers[r.Route]
		if !ok {
			t.Errorf("%s names %s, which this test cannot serve: add it to remedyHandlers", code, r.Route)
			continue
		}
		rec := serveRemedy(s, h, r.Method, r.Route)
		if rec.Code == http.StatusNotImplemented {
			t.Errorf("%s sends a caller to %s %s, which answers 501: %s", code, r.Method, r.Route, rec.Body)
		}
	}
}

// The refusal a live machine-role holder gets when it tries to hand its work
// over, followed to the end.
//
// The route its remedy used to name is still here and still 501 — that is the
// control, and it is what a caller following the old advice got. The remedy now
// says, in a field rather than in prose, that there is nothing to send, and why:
// `/rebind` is not a way round it, because it refuses a holder that is alive.
func TestTheSuccessionRemedyDoesNotSendAnybodyToTheRouteThatIsNotThere(t *testing.T) {
	s := remedyServer(t)
	rec := serveRemedy(s, s.orchestratorCoordinatorRoute, http.MethodPost, "/v1/orchestrator/coordinator/successions")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("the successions route answered %d; this test is about the one that does not exist", rec.Code)
	}
	var body struct {
		Error struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			Implemented *bool  `json:"implemented"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "succession_unavailable" || body.Error.Implemented == nil || *body.Error.Implemented {
		t.Fatalf("the 501 says %+v", body.Error)
	}
	// It used to claim this daemon has no handoffs. It has.
	if strings.Contains(body.Error.Message, "does not have yet") {
		t.Errorf("the 501 still says handoffs are missing: %q", body.Error.Message)
	}
	// And it used to offer /rebind to a caller who cannot use it.
	if !strings.Contains(body.Error.Message, "coordinator_online") {
		t.Errorf("the 501 offers a way out without saying what refuses it: %q", body.Error.Message)
	}

	rem, ok := orchestrator.RemedyFor("succession_required", map[string]any{})
	if !ok {
		t.Fatal("succession_required has no remedy at all; a caller is told nothing")
	}
	if rem.Available || rem.Route != "" || rem.Command != "" {
		t.Fatalf("the succession remedy still names a request: %+v", rem)
	}
	if !strings.Contains(rem.Because, "rebind") || !strings.Contains(rem.Because, "coordinator_online") {
		t.Errorf("the remedy does not say why the obvious substitute fails: %q", rem.Because)
	}
	// The control: a remedy that does name a route still carries a line to run.
	if inv, ok := orchestrator.RemedyFor("stale_inventory", map[string]any{"project": "/p"}); !ok ||
		!inv.Available || inv.Command == "" {
		t.Fatalf("an available remedy lost its command: %+v", inv)
	}
}
