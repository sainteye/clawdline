package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

// activations records which activation a bind chose; purposeErr is what the
// investigation activation answers.
type activations struct {
	plain, investigation []string
	purposeErr           error
}

func (a *activations) wire(b *ScheduleBook) {
	b.Activate = func(_ context.Context, hook, request string, at int64) (int64, error) {
		a.plain = append(a.plain, request)
		return at + 1, nil
	}
	b.ActivateInvestigation = func(_ context.Context, hook, request string, at int64) (int64, error) {
		a.investigation = append(a.investigation, request)
		if a.purposeErr != nil {
			return 0, a.purposeErr
		}
		return at + 1, nil
	}
}

func storeBindable(t *testing.T, f *w4Fixture, n int, kind string, when map[string]any, enabled bool) string {
	t.Helper()
	id := fmt.Sprintf("5c0000%02d-0000-4000-8000-0000000000%02d", 40+n, 40+n)
	task := map[string]any{"assistant": "claude", "project_dir": f.dir, "title": "look",
		"instructions": "look into it", "claims": []string{}}
	if kind != "" {
		task["kind"] = kind
	}
	body, _ := json.Marshal(map[string]any{"clawdline_schedule": 1, "schedule_id": id, "title": "look",
		"when": when, "enabled": enabled, "created_at": f.at().Add(-time.Hour).Unix(), "task": task})
	if err := f.st.CreateScheduleFile(context.Background(), id, body, f.at().Add(-time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestOnlyAnEnabledTriggerOnlyInvestigationDeclaresItsPurpose: the purpose is
// chosen from the schedule as stored. Every other schedule activates with the
// body it always had.
func TestOnlyAnEnabledTriggerOnlyInvestigationDeclaresItsPurpose(t *testing.T) {
	triggerOnly := map[string]any{"trigger_only": true}
	daily := map[string]any{"at": "09:00", "days": []any{"mon"}}
	cases := []struct {
		name    string
		kind    string
		when    map[string]any
		enabled bool
		purpose bool
	}{
		{"an ordinary daily schedule", "", daily, true, false},
		{"an ordinary trigger-only schedule", "", triggerOnly, true, false},
		{"a disabled investigation", schedulewebhook.InvestigationKind, triggerOnly, false, false},
		{"an enabled trigger-only investigation", schedulewebhook.InvestigationKind, triggerOnly, true, true},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newW4(t)
			a := &activations{}
			a.wire(f.book)
			id := storeBindable(t, f, i, c.kind, c.when, c.enabled)
			if _, ok, _ := f.book.named(context.Background(), id); !ok {
				t.Fatalf("the fixture schedule is not valid")
			}
			request := fmt.Sprintf("b20000%02d-0000-4000-8000-000000000001", i)
			hook := "swh_" + strings.Repeat("p", 26)
			status, body := f.book.BindWebhook(context.Background(), request, hook, id, nil, nil, "test")
			if status != 200 {
				t.Fatalf("bind answered %d %s", status, body)
			}
			if got := len(a.investigation) == 1 && len(a.plain) == 0; got != c.purpose {
				t.Fatalf("purpose declared %v, want %v (plain %v, investigation %v)", got, c.purpose, a.plain, a.investigation)
			}
			if len(a.plain)+len(a.investigation) != 1 {
				t.Fatalf("Cloud was asked %d times", len(a.plain)+len(a.investigation))
			}
		})
	}
}

// TestACloudWithoutThePurposeIsNotAskedAgainWithoutIt: an old Cloud refusing
// the field leaves the bind unanswered as `cloud_upgrade_required`, never an
// activation without the purpose, and the same request succeeds once Cloud
// knows it.
func TestACloudWithoutThePurposeIsNotAskedAgainWithoutIt(t *testing.T) {
	f := newW4(t)
	a := &activations{purposeErr: fmt.Errorf("%w: 400", adaptercloud.ErrCloudUpgradeRequired)}
	a.wire(f.book)
	id := storeBindable(t, f, 9, schedulewebhook.InvestigationKind, map[string]any{"trigger_only": true}, true)
	request := "b2000099-0000-4000-8000-000000000001"
	hook := "swh_" + strings.Repeat("q", 26)
	status, body := f.book.BindWebhook(context.Background(), request, hook, id, nil, nil, "test")
	if status != 503 || !strings.Contains(string(body), "cloud_upgrade_required") {
		t.Fatalf("an old Cloud was answered as %d %s", status, body)
	}
	if len(a.plain) != 0 || len(a.investigation) != 1 {
		t.Fatalf("plain %v, investigation %v: the purpose was dropped or retried", a.plain, a.investigation)
	}
	a.purposeErr = nil
	if status, body := f.book.BindWebhook(context.Background(), request, hook, id, nil, nil, "test"); status != 200 {
		t.Fatalf("the retry after the upgrade answered %d %s", status, body)
	}
	if len(a.plain) != 0 || len(a.investigation) != 2 || a.investigation[1] != request {
		t.Fatalf("the retry changed shape: plain %v, investigation %v", a.plain, a.investigation)
	}
	// Recorded now: the same request replays without asking Cloud again.
	if status, _ := f.book.BindWebhook(context.Background(), request, hook, id, nil, nil, "test"); status != 200 || len(a.investigation) != 2 {
		t.Fatalf("the replay answered %d after %d activations", status, len(a.investigation))
	}

	// A book with no investigation activation refuses instead of activating unmarked.
	f.book.ActivateInvestigation = nil
	other := "b2000099-0000-4000-8000-000000000002"
	second := storeBindable(t, f, 8, schedulewebhook.InvestigationKind, map[string]any{"trigger_only": true}, true)
	if status, body := f.book.BindWebhook(context.Background(), other, "swh_"+strings.Repeat("r", 26), second, nil, nil, "test"); status != 503 ||
		!strings.Contains(string(body), "cloud_upgrade_required") || len(a.plain) != 0 {
		t.Fatalf("an unwired investigation activation answered %d %s, plain %v", status, body, a.plain)
	}
}
