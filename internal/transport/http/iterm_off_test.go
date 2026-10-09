package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// sourceHost is one terminal source answering a fixed reading. Only
// Inventory is called on it: the inventory reads it, nothing acts through it.
type sourceHost struct {
	ports.TerminalHost
	inv session.Inventory
}

func (h sourceHost) Inventory(context.Context) (session.Inventory, error) { return h.inv, nil }

// withITermOff adds a complete, empty tmux listing and an iTerm2 the person
// turned off (`iterm_scan=false`, D71) to the server's inventory.
func withITermOff(s *Server) {
	s.inventory.Terminals = append(s.inventory.Terminals,
		sourceHost{inv: session.Inventory{Provenance: "tmux", Complete: true}},
		sourceHost{inv: session.Inventory{Provenance: "iterm", Disabled: "setting",
			Notes: []string{"iTerm2 scanning is turned off"}}})
}

// A tmux pane tmux listed completely is gone whatever iTerm2's setting is;
// an iTerm2 session is not provably gone while iTerm2 is not read.
func TestACloseOfAGoneTmuxPaneSaysSoWhileITermIsOff(t *testing.T) {
	s, _ := ownTodoServer(t)
	withITermOff(s)
	status, code, _ := agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/%2599/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004"}`)
	if status != http.StatusNotFound || code != "session_not_found" {
		t.Fatalf("gone tmux pane with iTerm2 off: %d %s", status, code)
	}
	status, code, _ = agentCloseCode(t, s, http.MethodPost,
		"/v1/work/v2/agent/sessions/AAAAAAAA-0000-4000-8000-000000000001/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004"}`)
	if status != http.StatusConflict || code != "close_inventory_unavailable" {
		t.Fatalf("unseen iTerm2 session with iTerm2 off: %d %s", status, code)
	}
}

// The landings list's Sessions source is current when every source that was
// asked answered, and a source nobody asked does not make it stale.
func TestLandingSessionsAreCurrentWhileITermIsOff(t *testing.T) {
	s, _ := ownTodoServer(t)
	withITermOff(s)
	rec := httptest.NewRecorder()
	s.brokerLandings(rec, httptest.NewRequest(http.MethodGet, "/v1/orchestrator/landings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("landings: %d %s", rec.Code, rec.Body)
	}
	var out contract.BrokerLandingList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Sources.Sessions.Freshness != contract.SourceFreshnessCurrent {
		t.Fatalf("sessions freshness with iTerm2 off: %s", out.Sources.Sessions.Freshness)
	}
}
