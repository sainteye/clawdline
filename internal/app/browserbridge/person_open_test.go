package browserbridge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
)

func personOpenPolicy() browserpolicy.Policy {
	return browserpolicy.Policy{Order: []string{"isolated", "native", "user_chrome"}, Automatic: map[string]bool{"native": true, "isolated": true, "user_chrome": false}}
}

// The person opens a Session's tab: it belongs to that Session, starts taken
// over, refuses the agent until hand back, and is recorded as the person's
// without a new-site notification.
func TestThePersonOpensASessionTabTheyAlreadyControl(t *testing.T) {
	var mu sync.Mutex
	var sites []NewSite
	h, err := OpenProfiles("", func(n NewSite) { mu.Lock(); sites = append(sites, n); mu.Unlock() }, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := profilesHost(t, h, "isolated", func(c Command) Result {
		tab := c.Tab
		if c.Op == "open" {
			tab = 21
		}
		return Result{ID: c.ID, OK: true, Tab: tab, URL: "https://github.com/login"}
	})
	policy := personOpenPolicy()
	if _, err := h.PersonOpen(context.Background(), "s1", "https://github.com/login", policy); err != nil {
		t.Fatal(err)
	}
	st := b.Status()
	if len(st.Tabs) != 1 || st.Tabs[0].Tab != 21 || st.Tabs[0].Conversation != "s1" || !st.Tabs[0].Person {
		t.Fatalf("tabs = %+v, want tab 21 of s1 taken over by the person", st.Tabs)
	}
	if _, err := h.DoSession(context.Background(), "s1", "screenshot", nil, policy, "", false); err != PersonControlling {
		t.Fatalf("agent command while the person controls: %v, want %s", err, PersonControlling)
	}
	if err := h.SetPersonControl("isolated", 21, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoSession(context.Background(), "s1", "screenshot", nil, policy, "", false); err != nil {
		t.Fatalf("agent after hand back: %v", err)
	}
	rows, _ := b.Records("s1", 10)
	var opened *AuditRow
	for i := range rows {
		if rows[i].Op == "open" {
			opened = &rows[i]
		}
	}
	if opened == nil || !opened.Person || !opened.OK || opened.Site == "" {
		t.Fatalf("open row = %+v, want an OK row marked as the person's", opened)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sites) != 0 {
		t.Fatalf("new-site notifications = %+v, want none for the person's own open", sites)
	}
}

func TestThePersonCannotOpenWhileStoppedOrWithoutAWebAddress(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := profilesHost(t, h, "isolated", nil)
	policy := personOpenPolicy()
	for _, address := range []string{"", "github.com", "javascript:alert(1)", "file:///etc/passwd", "chrome://settings"} {
		if _, err := h.PersonOpen(context.Background(), "s1", address, policy); err != BadRequest {
			t.Fatalf("%q: %v, want %s", address, err, BadRequest)
		}
	}
	if err := h.SetStopped(true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PersonOpen(context.Background(), "s1", "https://example.com", policy); err != Stopped {
		t.Fatalf("while stopped: %v, want %s", err, Stopped)
	}
	if len(b.Status().Tabs) != 0 {
		t.Fatalf("a refused open left tabs %+v", b.Status().Tabs)
	}
}

// Native tools first in priority, and every automatic switch off for
// Chrome: the person's open still uses the connected Chrome, explicitly, and
// the agent then continues in it. With no Chrome connected it is refused.
func TestThePersonOpensInChromeEvenWhenNativeToolsComeFirst(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	native := browserpolicy.Policy{Order: []string{"native", "user_chrome", "isolated"}, Automatic: map[string]bool{"native": true, "isolated": false, "user_chrome": false}}
	if got := h.PersonOpenMode("s1", native); got != "" {
		t.Fatalf("with no Chrome connected PersonOpenMode = %q, want none", got)
	}
	if _, err := h.PersonOpen(context.Background(), "s1", "https://example.com", native); err != SetupRequired {
		t.Fatalf("no Chrome connected: %v, want %s", err, SetupRequired)
	}
	profilesHost(t, h, "isolated", nil)
	if got := h.PersonOpenMode("s1", native); got != "isolated" {
		t.Fatalf("PersonOpenMode = %q, want isolated", got)
	}
	if _, err := h.PersonOpen(context.Background(), "s1", "https://example.com", native); err != nil {
		t.Fatal(err)
	}
	mode, st, _ := h.SelectedStatus("s1", native)
	if mode != "isolated" || len(st.Tabs) != 1 || !st.Tabs[0].Person {
		t.Fatalf("mode %q tabs %+v, want the person's isolated tab", mode, st.Tabs)
	}
	if err := h.SetPersonControl("isolated", st.Tabs[0].Tab, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoSession(context.Background(), "s1", "screenshot", nil, native, "", false); err != nil {
		t.Fatalf("the agent after hand back: %v", err)
	}
}

// Only an open may carry the person's mark through the agent path.
func TestThePersonsMarkCannotCarryAnotherOp(t *testing.T) {
	b, ctx := connected(t)
	if _, err := b.do(ctx, "s1", "click", json.RawMessage(`{"tab":1}`), nil, true); err != BadRequest {
		t.Fatalf("person click through do: %v, want %s", err, BadRequest)
	}
}
