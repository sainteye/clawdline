package http

import (
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestCloseUsesFreshIdleWorkEvidenceWithoutAnAttestation(t *testing.T) {
	for _, c := range []struct {
		state   contract.CloseabilityState
		force   bool
		version string
		want    bool
		code    string
	}{
		{"safe", false, "current", true, ""},
		{"safe", false, "stale", false, "close_not_proven"},
		{"blocked", false, "current", false, "close_blocked"},
		{"blocked", true, "current", true, ""},
		{"unknown", true, "current", false, "closeability_unknown"},
		{"unknown", false, "stale", false, "closeability_unknown"},
		{"needs_attestation", false, "current", false, "closeability_unknown"},
	} {
		allowed, code := closeEvidenceDecision(contract.Closeability{State: c.state, Version: "current"}, c.version, c.force)
		if allowed != c.want || code != c.code {
			t.Errorf("%s force=%v version=%s: allowed=%v code=%s", c.state, c.force, c.version, allowed, code)
		}
	}
	failedScan := contract.Closeability{State: contract.CloseabilityStateUnknown, Version: "fresh",
		Reasons: []contract.CloseReason{{Code: "session_inventory_stale"}}}
	if allowed, code := closeEvidenceDecision(failedScan, "previous", false); allowed || code != "close_inventory_unavailable" {
		t.Fatalf("failed fresh inventory: allowed=%v code=%s", allowed, code)
	}
	failedScan.Reasons = append(failedScan.Reasons, contract.CloseReason{Code: "own_records_unreadable", Kind: "evidence"})
	if allowed, code := closeEvidenceDecision(failedScan, "previous", false); allowed || code != "closeability_unknown" {
		t.Fatalf("multiple missing evidence sources: allowed=%v code=%s", allowed, code)
	}
}

// The status each rung of a close answers with (actions.go), and — which is
// the half that is easy to get wrong — whether a retry under the same
// Idempotency-Key is carried out again or answered with the first answer.
//
// A close types the assistant's own quit word into the session before it takes
// anything away, so the rungs that reached the terminal are kept: a retry of
// an unanswered iTerm2 close would put a second question on the same screen,
// which is exactly what the close this replaced did.
func TestEachCloseRefusalCarriesItsOwnStatusAndRetryRule(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
		// filed is whether an Idempotency-Key keeps this answer instead of
		// handing the request back to be carried out again.
		filed bool
	}{
		{"close_blocked", http.StatusConflict, false},
		{"closeability_unknown", http.StatusConflict, false},
		{"close_inventory_unavailable", http.StatusConflict, false},
		{"close_nothing_there", http.StatusNotFound, false},
		{"close_occupied", http.StatusConflict, false},
		{"close_unreadable", http.StatusConflict, false},
		{"close_assistant_running", http.StatusConflict, false},
		{"close_quit_refused", http.StatusBadGateway, true},
		{"close_unconfirmed", http.StatusBadGateway, true},
		{"close_needs_a_person", http.StatusBadGateway, true},
		{"close_failed", http.StatusInternalServerError, true},
	} {
		got := actionStatus(c.code)
		if got != c.status {
			t.Errorf("%s: %d, want %d", c.code, got, c.status)
		}
		if filed := sessionWriteFiled(got); filed != c.filed {
			t.Errorf("%s: kept by its key %v, want %v", c.code, filed, c.filed)
		}
	}
}

// The close route is given room for the whole ladder. The ordinary fifteen
// seconds is shorter than the word, the wait and the two signal rungs after
// it, so the route would have cut its own close off part way through.
func TestTheCloseRouteIsGivenRoomForTheWholeLadder(t *testing.T) {
	if closeBudget <= 15e9 {
		t.Fatalf("a close has %s, which is no more than an ordinary action", closeBudget)
	}
}
