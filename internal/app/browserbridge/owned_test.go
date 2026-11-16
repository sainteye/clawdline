package browserbridge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
)

func ownedPolicy() browserpolicy.Policy {
	return browserpolicy.Policy{Order: []string{"isolated", "user_chrome", "native"}, Automatic: map[string]bool{"isolated": true, "user_chrome": false, "native": false}}
}

// An owned caller (a Bot) can act only on tabs it opened itself in the chosen
// profile. The Session path keeps its older isolated-profile leniency.
func TestAnOwnedCallerActsOnlyOnItsOwnTabs(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sent []Command
	profilesHost(t, h, "isolated", func(c Command) Result {
		sent = append(sent, c)
		tab := c.Tab
		if c.Op == "open" {
			tab = 20 + len(sent)
		}
		return Result{ID: c.ID, OK: true, Tab: tab, Value: json.RawMessage(`{"ok":true}`)}
	})
	policy := ownedPolicy()
	ctx := context.Background()
	if _, err := h.DoOwned(ctx, "bot-a", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoSession(ctx, "session", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "", false); err != nil {
		t.Fatal(err)
	}
	// bot-a owns 21, the Session owns 22, and 99 is unowned in this profile.
	if _, err := h.DoOwned(ctx, "bot-a", "read", nil, policy, ""); err != nil {
		t.Fatalf("own current tab: %v", err)
	}
	before := len(sent)
	for _, tab := range []string{`{"tab":22}`, `{"tab":99}`} {
		if _, err := h.DoOwned(ctx, "bot-a", "read", json.RawMessage(tab), policy, ""); err != OtherSessionsTab {
			t.Fatalf("%s: err = %v, want %s", tab, err, OtherSessionsTab)
		}
	}
	if _, err := h.DoOwned(ctx, "bot-b", "read", json.RawMessage(`{"tab":21}`), policy, ""); err != OtherSessionsTab {
		t.Fatalf("another Bot read bot-a's tab: %v", err)
	}
	if _, err := h.DoOwned(ctx, "bot-b", "read", nil, policy, ""); err != NoTab {
		t.Fatalf("a Bot without a tab reached one: %v", err)
	}
	if _, err := h.DoOwned(ctx, "bot-a", "tabs", nil, policy, ""); err != BadRequest {
		t.Fatalf("an owned caller listed every tab: %v", err)
	}
	if len(sent) != before {
		t.Fatalf("refused commands reached the host: %+v", sent[before:])
	}
	// The older Session path is unchanged: an unowned isolated tab is still allowed.
	if _, err := h.DoSession(ctx, "session", "read", json.RawMessage(`{"tab":99}`), policy, "", false); err != nil {
		t.Fatalf("Session semantics changed: %v", err)
	}
	if _, err := h.DoOwned(ctx, "bot-a", "close", nil, policy, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoOwned(ctx, "bot-a", "read", json.RawMessage(`{"tab":21}`), policy, ""); err != OtherSessionsTab {
		t.Fatalf("a closed tab stayed usable: %v", err)
	}
}

// An owner never takes an explicit choice it was not given, and cannot reach
// a browser the person left off automatic use.
func TestAnOwnedCallerCannotChooseADisabledBrowser(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sent int
	profilesHost(t, h, "isolated", func(c Command) Result { sent++; return Result{ID: c.ID, OK: true, Tab: 5} })
	profilesHost(t, h, "user_chrome", func(c Command) Result { sent++; return Result{ID: c.ID, OK: true, Tab: 5} })
	if _, err := h.DoOwned(context.Background(), "bot-a", "open", json.RawMessage(`{"url":"https://example.com"}`), ownedPolicy(), "user_chrome"); err != Disabled {
		t.Fatalf("err = %v, want %s", err, Disabled)
	}
	if sent != 0 {
		t.Fatalf("%d commands reached a host", sent)
	}
}

// Two profiles number tabs independently. Owning tab 5 in one does not reach
// tab 5 in the other.
func TestAnOwnedTabNumberDoesNotCrossProfiles(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	both := browserpolicy.Policy{Order: []string{"isolated", "user_chrome", "native"}, Automatic: map[string]bool{"isolated": true, "user_chrome": true, "native": false}}
	profilesHost(t, h, "isolated", func(c Command) Result { return Result{ID: c.ID, OK: true, Tab: 5} })
	var chrome int
	profilesHost(t, h, "user_chrome", func(c Command) Result { chrome++; return Result{ID: c.ID, OK: true, Tab: 5} })
	ctx := context.Background()
	if _, err := h.DoOwned(ctx, "bot-a", "open", json.RawMessage(`{"url":"https://example.com"}`), both, "isolated"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoSession(ctx, "session", "open", json.RawMessage(`{"url":"https://example.com"}`), both, "user_chrome", false); err != nil {
		t.Fatal(err)
	}
	chrome = 0
	if _, err := h.DoOwned(ctx, "bot-b", "open", json.RawMessage(`{"url":"https://example.com"}`), both, "user_chrome"); err != nil {
		t.Fatal(err)
	}
	// bot-b now selected user_chrome, where tab 5 is the Session's.
	chrome = 0
	if _, err := h.DoOwned(ctx, "bot-a", "read", json.RawMessage(`{"tab":5}`), both, "user_chrome"); err != OtherSessionsTab {
		t.Fatalf("bot-a's isolated tab 5 reached user_chrome tab 5: %v", err)
	}
	if chrome != 0 {
		t.Fatal("the refused read reached Chrome")
	}
}
