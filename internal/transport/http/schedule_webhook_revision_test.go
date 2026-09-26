package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	cloudadapter "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
)

// hookCloud is Cloud's activate rule and nothing else: a hook activates only
// from `pending_binding`, and only when `expected_revision` is the revision it
// is at, which `/move` raises. An Idempotency-Key replays its first answer.
type hookCloud struct {
	mu      sync.Mutex
	hooks   map[string]*hookAtCloud
	answers map[string][2]any
}

type hookAtCloud struct {
	state    string
	revision int64
}

func newHookCloud(t *testing.T) (*hookCloud, cloudadapter.ScheduleWebhookClient) {
	t.Helper()
	c := &hookCloud{hooks: map[string]*hookAtCloud{}, answers: map[string][2]any{}}
	server := httptest.NewServer(http.HandlerFunc(c.activate))
	t.Cleanup(server.Close)
	return c, cloudadapter.ScheduleWebhookClient{Client: cloudadapter.NewAccountClient(server.URL),
		Credential: "machine-secret"}
}

func (c *hookCloud) activate(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	answer := func(status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	key := r.Header.Get("Idempotency-Key")
	if prior, ok := c.answers[key]; ok {
		answer(prior[0].(int), prior[1])
		return
	}
	refuse := func(status int, code string) {
		body := map[string]any{"error": map[string]any{"code": code, "message": code}}
		c.answers[key] = [2]any{status, body}
		answer(status, body)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/schedule-webhooks/"), "/activate")
	var in struct {
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.ExpectedRevision == nil {
		refuse(400, "invalid_request")
		return
	}
	hook, ok := c.hooks[id]
	switch {
	case !ok:
		refuse(404, "not_found")
	case hook.revision != *in.ExpectedRevision:
		refuse(409, "stale_revision")
	case hook.state != "pending_binding":
		refuse(409, "invalid_state")
	default:
		hook.state, hook.revision = "active", hook.revision+1
		body := map[string]any{"schema": "clawdline.schedule_webhook.management.v1",
			"hook": map[string]any{"hook_id": id, "state": hook.state, "revision": hook.revision}}
		c.answers[key] = [2]any{200, body}
		answer(200, body)
	}
}

// TestAMovedWebhookActivatesAtTheRevisionItIsAt: a move raises the hook's
// revision at Cloud, and the bind on the target used to activate at 0 always,
// which Cloud refuses `stale_revision` — hidden behind a blanket 503. This
// wires the real client to a Cloud that enforces the rule.
func TestAMovedWebhookActivatesAtTheRevisionItIsAt(t *testing.T) {
	s := cloudStandIn(t)
	hooks, client := newHookCloud(t)
	s.server.scheduleBook().Activate = client.Activate
	seq := uint64(0)
	schedule := func(title string) string {
		seq++
		answer, payload := s.ask(t, seq, map[string]any{"type": "schedule-create",
			"session": cloudops.MachineReplySession, "request": fmt.Sprintf("req-hook-%d", seq),
			"schedule": dailySchedule(s.place, title, "09:00")})
		if !answer.OK() {
			t.Fatalf("the schedule was refused: %d/%q %s", answer.Status, answer.Code, answer.Payload)
		}
		made, _ := bodyOf(t, payload)["schedule"].(map[string]any)
		id, _ := made["id"].(string)
		return id
	}
	bind := func(request, hook, scheduleID string, revision *int64) (cloudops.Answer, map[string]any) {
		seq++
		body := map[string]any{"type": "schedule-webhook-bind-v1", "request_id": request,
			"hook_id": hook, "schedule_id": scheduleID, "replace_hook_id": nil}
		if revision != nil {
			body["hook_revision"] = *revision
		}
		return s.ask(t, seq, body)
	}
	two := int64(2)

	t.Run("a moved hook", func(t *testing.T) {
		hook := "swh_" + strings.Repeat("m", 26)
		hooks.hooks[hook] = &hookAtCloud{state: "pending_binding", revision: 2}
		answer, payload := bind("b1000001-0000-4000-8000-000000000001", hook, schedule("moved"), &two)
		if !answer.OK() {
			t.Fatalf("the moved hook was not activated: %d/%q %s", answer.Status, answer.Code, answer.Payload)
		}
		if got := bodyOf(t, payload)["hook_revision"]; got != float64(3) {
			t.Fatalf("the bind answered revision %v, want 3", got)
		}
		if at := hooks.hooks[hook]; at.state != "active" || at.revision != 3 {
			t.Fatalf("Cloud holds %+v, want active at 3", *at)
		}
	})
	t.Run("a new hook", func(t *testing.T) {
		hook := "swh_" + strings.Repeat("n", 26)
		hooks.hooks[hook] = &hookAtCloud{state: "pending_binding", revision: 0}
		id := schedule("new")
		answer, payload := bind("b1000002-0000-4000-8000-000000000002", hook, id, nil)
		if !answer.OK() || bodyOf(t, payload)["hook_revision"] != float64(1) {
			t.Fatalf("a new hook without a revision: %d/%q %s", answer.Status, answer.Code, answer.Payload)
		}
		// The ledger replays the same request, whose digest carries no revision.
		again, _ := bind("b1000002-0000-4000-8000-000000000002", hook, id, nil)
		if !again.OK() {
			t.Fatalf("the replay was refused: %d/%q %s", again.Status, again.Code, again.Payload)
		}
		// And the same request id with a revision is a different request.
		zero := int64(0)
		if other, _ := bind("b1000002-0000-4000-8000-000000000002", hook, id, &zero); other.Code != "idempotency_conflict" {
			t.Fatalf("a changed body under the same request id: %d/%q", other.Status, other.Code)
		}
	})
	t.Run("a stale revision", func(t *testing.T) {
		hook := "swh_" + strings.Repeat("s", 26)
		hooks.hooks[hook] = &hookAtCloud{state: "pending_binding", revision: 2}
		answer, _ := bind("b1000003-0000-4000-8000-000000000003", hook, schedule("stale"), nil)
		if answer.Status != http.StatusConflict || answer.Code != "stale_revision" {
			t.Fatalf("Cloud's refusal was answered as %d/%q, want 409/stale_revision", answer.Status, answer.Code)
		}
	})
	t.Run("a hook Cloud does not know", func(t *testing.T) {
		answer, _ := bind("b1000004-0000-4000-8000-000000000004", "swh_"+strings.Repeat("x", 26),
			schedule("unknown"), &two)
		if answer.Status != http.StatusNotFound || answer.Code != "not_found" {
			t.Fatalf("Cloud's refusal was answered as %d/%q, want 404/not_found", answer.Status, answer.Code)
		}
	})
}
