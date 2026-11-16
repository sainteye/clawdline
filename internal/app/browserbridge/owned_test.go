package browserbridge

import (
	"context"
	"encoding/json"
	"strings"
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

// A Bot's late page text or picture never crosses a takeover and handback, or
// a stop and resume, that happened while its command was with the host. The
// late result is dropped, not replayed, and the next command works again.
func TestAnOwnedLateResultCannotCrossAPersonsABA(t *testing.T) {
	for _, change := range []struct {
		name   string
		code   Refusal
		toggle func(h *Profiles, tab int, on bool) error
	}{
		{"takeover", PersonControlling, func(h *Profiles, tab int, on bool) error { return h.SetPersonControl("isolated", tab, on) }},
		{"stop", Stopped, func(h *Profiles, _ int, on bool) error { return h.SetStopped(on) }},
	} {
		for _, op := range []string{"read", "screenshot"} {
			t.Run(change.name+"/"+op, func(t *testing.T) {
				h, err := OpenProfiles("", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				started, release := make(chan struct{}, 1), make(chan struct{})
				hold := false
				var sent []string
				profilesHost(t, h, "isolated", func(c Command) Result {
					sent = append(sent, c.Op)
					if c.Op == "open" {
						return Result{ID: c.ID, OK: true, Tab: 31, Value: json.RawMessage(`{"ok":true}`)}
					}
					if hold {
						started <- struct{}{}
						<-release
						return Result{ID: c.ID, OK: true, Tab: 31, Value: json.RawMessage(`{"text":"private page","data":"cHJpdmF0ZQ=="}`), Thumb: "AAAA"}
					}
					return Result{ID: c.ID, OK: true, Tab: 31, Value: json.RawMessage(`{"text":"fresh"}`)}
				})
				policy, ctx := ownedPolicy(), context.Background()
				if _, err := h.DoOwned(ctx, "bot-a", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, ""); err != nil {
					t.Fatal(err)
				}
				hold = true
				type answer struct {
					value json.RawMessage
					err   error
				}
				done := make(chan answer, 1)
				go func() {
					v, err := h.DoOwned(ctx, "bot-a", op, nil, policy, "")
					done <- answer{v, err}
				}()
				<-started
				if err := change.toggle(h, 31, true); err != nil {
					t.Fatal(err)
				}
				if err := change.toggle(h, 31, false); err != nil {
					t.Fatal(err)
				}
				hold = false
				close(release)
				got := <-done
				if got.err != change.code || len(got.value) != 0 {
					t.Fatalf("late %s after %s ABA = %s %v, want %s and no value", op, change.name, got.value, got.err, change.code)
				}
				v, err := h.DoOwned(ctx, "bot-a", "read", nil, policy, "")
				if err != nil || string(v) != `{"text":"fresh"}` {
					t.Fatalf("after the person's ABA = %s %v", v, err)
				}
				if want := "open," + op + ",read"; strings.Join(sent, ",") != want {
					t.Fatalf("host received %v, want %s (no replay)", sent, want)
				}
			})
		}
	}
}

// A person's request scoped to one owner reaches only that owner's tabs; one
// scoped away from Bots reaches every other tab. Admit runs at admission and
// its refusal admits nothing: no queued command, no control change, no saved
// choice. A tab forgotten while its picture travels returns no picture.
func TestAPersonTargetIsCheckedAtAdmissionAndOnTheAnswer(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sent []Command
	var b *Bridge
	forgetOnShot := 0
	b = profilesHost(t, h, "isolated", func(c Command) Result {
		sent = append(sent, c)
		tab := c.Tab
		if c.Op == "open" {
			tab = 30 + len(sent)
		}
		if c.Op == "screenshot" && c.Tab == forgetOnShot {
			b.Forget(c.Tab)
		}
		return Result{ID: c.ID, OK: true, Tab: tab, Value: json.RawMessage(`{"data":"eA=="}`)}
	})
	policy := ownedPolicy()
	ctx := context.Background()
	const botA, botB = BotOwnerPrefix + "a", BotOwnerPrefix + "b"
	for _, owner := range []string{botA, botB} {
		if _, err := h.DoOwned(ctx, owner, "open", json.RawMessage(`{"url":"https://example.com"}`), policy, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.DoSession(ctx, "session", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "", false); err != nil {
		t.Fatal(err)
	}
	tabA, tabB, tabSession := 31, 32, 33
	if b.OwnerOf(tabA) != botA || b.OwnerOf(tabB) != botB || b.OwnerOf(tabSession) != "session" {
		t.Fatalf("owners: %q %q %q", b.OwnerOf(tabA), b.OwnerOf(tabB), b.OwnerOf(tabSession))
	}
	onlyA := PersonTarget{Owner: botA}
	noBots := PersonTarget{Exclude: BotOwnerPrefix}
	before := len(sent)
	for _, c := range []struct {
		target PersonTarget
		tab    int
	}{{onlyA, tabB}, {onlyA, tabSession}, {noBots, tabA}, {noBots, tabB}} {
		if _, err := h.PersonDoFor(ctx, c.target, "isolated", "screenshot", c.tab, nil); err != OtherSessionsTab {
			t.Fatalf("%+v on tab %d: %v", c.target, c.tab, err)
		}
		if err := h.SetPersonControlFor(c.target, "isolated", c.tab, true); err != OtherSessionsTab {
			t.Fatalf("%+v took over tab %d: %v", c.target, c.tab, err)
		}
	}
	if len(sent) != before || len(b.Status().PersonTabs) != 0 {
		t.Fatalf("refused targets reached the host or a tab: %+v %+v", sent[before:], b.Status().PersonTabs)
	}
	if _, err := h.PersonDoFor(ctx, onlyA, "isolated", "screenshot", tabA, nil); err != nil {
		t.Fatalf("Bot A's own picture: %v", err)
	}
	if _, err := h.PersonDoFor(ctx, noBots, "isolated", "screenshot", tabSession, nil); err != nil {
		t.Fatalf("the Session's picture: %v", err)
	}

	// A refusal at admission admits nothing.
	refused := &pa{}
	failing := PersonTarget{Owner: botA, Admit: func() error { refused.calls++; return errStale }}
	before = len(sent)
	if _, err := h.PersonDoFor(ctx, failing, "isolated", "screenshot", tabA, nil); err != errStale {
		t.Fatalf("picture after a failed admission: %v", err)
	}
	if err := h.SetPersonControlFor(failing, "isolated", tabA, true); err != errStale {
		t.Fatalf("takeover after a failed admission: %v", err)
	}
	if _, err := h.PersonOpenFor(ctx, failing, botA, "https://example.com/x", "", policy); err != errStale {
		t.Fatalf("open after a failed admission: %v", err)
	}
	if err := h.SetMutedFor(failing, botA, true); err != errStale {
		t.Fatalf("mute after a failed admission: %v", err)
	}
	if refused.calls != 4 || len(sent) != before || len(b.Status().PersonTabs) != 0 || len(b.Status().Muted) != 0 {
		t.Fatalf("a failed admission had an effect: calls=%d sent=%+v status=%+v", refused.calls, sent[before:], b.Status())
	}
	if _, err := h.PersonOpenFor(ctx, onlyA, botB, "https://example.com/x", "", policy); err != BadRequest {
		t.Fatalf("an open under another owner's target: %v", err)
	}

	// Admit runs under the selector fence: a Session's command cannot be
	// admitted while it runs.
	entered, release := make(chan struct{}), make(chan struct{})
	slow := PersonTarget{Owner: botA, Admit: func() error { close(entered); <-release; return nil }}
	controlled := make(chan error, 1)
	go func() { controlled <- h.SetPersonControlFor(slow, "isolated", tabA, true) }()
	<-entered
	sessionDone := make(chan error, 1)
	go func() {
		_, err := h.DoSession(ctx, "session", "read", nil, policy, "", false)
		sessionDone <- err
	}()
	select {
	case err := <-sessionDone:
		t.Fatalf("a Session command was admitted inside Admit: %v", err)
	default:
	}
	close(release)
	if err := <-controlled; err != nil {
		t.Fatal(err)
	}
	if err := <-sessionDone; err != nil {
		t.Fatal(err)
	}
	if err := h.SetPersonControlFor(onlyA, "isolated", tabA, false); err != nil {
		t.Fatal(err)
	}

	// A tab forgotten while its picture travels returns no picture.
	forgetOnShot = tabA
	if _, err := h.PersonDoFor(ctx, onlyA, "isolated", "screenshot", tabA, nil); err != OtherSessionsTab {
		t.Fatalf("a forgotten tab's late picture: %v", err)
	}
	// The unscoped engine call, used by the Bot's own tests, is unchanged.
	if err := h.SetPersonControl("isolated", tabB, true); err != nil {
		t.Fatalf("the unscoped takeover changed: %v", err)
	}
}

type pa struct{ calls int }

var errStale = &ExtensionError{Message: "stale"}

// An audit view that names no Bot leaves every Bot's rows out.
func TestAnAuditFilterCanLeaveBotsOut(t *testing.T) {
	f := AuditFilter{Exclude: BotOwnerPrefix}
	if f.matches(AuditRow{Conversation: BotOwnerPrefix + "a"}) || !f.matches(AuditRow{Conversation: "session"}) {
		t.Fatal("Exclude did not leave out the Bot's row")
	}
	if !strings.HasPrefix(BotOwnerPrefix+"x", "bot-") {
		t.Fatal("prefix")
	}
}
