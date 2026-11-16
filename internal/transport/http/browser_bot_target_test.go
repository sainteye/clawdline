package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// botTargetHost stands in for the native host behind the production
// Profiles and Bridge: it Polls and Delivers, and can hold one screenshot
// until the test lets it go. It is not Chrome.
type botTargetHost struct {
	bridge *browserbridge.Bridge
	mu     sync.Mutex
	seen   []browserbridge.Command
	next   int
	hold   chan struct{}
	held   chan struct{}
	// url, when set, is the page address every result reports.
	url string
}

func (h *botTargetHost) commands() []browserbridge.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]browserbridge.Command(nil), h.seen...)
}

func (h *botTargetHost) serve(ctx context.Context, t *testing.T) {
	for ctx.Err() == nil {
		for _, c := range h.bridge.Poll(ctx, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"}) {
			h.mu.Lock()
			h.seen = append(h.seen, c)
			hold, held, page := h.hold, h.held, h.url
			h.mu.Unlock()
			result := browserbridge.Result{ID: c.ID, OK: true, Tab: c.Tab, URL: page, Value: json.RawMessage(`{"ok":true}`)}
			switch c.Op {
			case "open":
				h.mu.Lock()
				h.next++
				result.Tab = 60 + h.next
				h.mu.Unlock()
			case "screenshot":
				result.Value = json.RawMessage(`{"data":"c2NyaXB0ZWQ=","url":"http://127.0.0.1:9/page"}`)
				if hold != nil {
					close(held)
					<-hold
				}
			}
			if err := h.bridge.Deliver([]browserbridge.Result{result}); err != nil {
				t.Errorf("deliver %s: %v", c.Op, err)
			}
		}
	}
}

type botTargetFixture struct {
	*paServerFixture
	host       *botTargetHost
	keyB       pa.BotKey
	convA      string
	convB      string
	projectA   pa.ProjectScope
	projectB   pa.ProjectScope
	terminal   string
	sessionTab int
}

func newBotTargetFixture(t *testing.T) *botTargetFixture {
	t.Helper()
	f := &botTargetFixture{paServerFixture: newPAServerFixture(t, true), terminal: "20000000-0000-4000-8000-000000000071"}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	// A second Bot works in a second Project.
	second := filepath.Join(t.TempDir(), "second")
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatal(err)
	}
	registered, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Add([]string{second}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.keyB = pa.BotKey{MachineID: f.key.MachineID, BotID: "synthetic-bot-b"}
	create := f.create()
	create.Command.Key = paWireBotKey(f.keyB)
	create.Command.IdempotencyKey = "create-b"
	for _, place := range registered {
		if place.Path != f.project {
			create.Settings.Target.ProjectID = projects.PlaceID(place.Path)
		}
	}
	create.Settings.Name = "Synthetic Bot B"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
	a := f.paServerFixture.get(t, f.local)
	f.convA, f.projectA = a.Conversation.ID, paDomainProjectScope(a.Bot.Project)
	w := f.request(t, "GET", "/v1/project-assistants/"+f.keyB.BotID+"?machine_id="+f.keyB.MachineID, f.local, nil)
	paServerOK(t, w)
	var b contract.ProjectAssistantGetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	f.convB, f.projectB = b.Conversation.ID, paDomainProjectScope(b.Bot.Project)
	if f.projectA == f.projectB || f.convA == f.convB {
		t.Fatalf("the two Bots share a Project or a conversation: %+v %s %+v %s", f.projectA, f.convA, f.projectB, f.convB)
	}

	if _, err := f.s.settingsFile().Set(map[string]any{"browser_policy": json.RawMessage(`{"order":["isolated","native","user_chrome"],"automatic":{"native":false,"isolated":true,"user_chrome":false}}`)}); err != nil {
		t.Fatal(err)
	}
	bridge, err := f.s.browsers().Channel("isolated")
	if err != nil {
		t.Fatal(err)
	}
	probe, cancel := context.WithCancel(t.Context())
	cancel()
	bridge.Poll(probe, browserbridge.HostInfo{Mode: "isolated", Version: "scripted"})
	f.host = &botTargetHost{bridge: bridge}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.host.serve(ctx, t) }()
	t.Cleanup(func() { stop(); <-done })

	// A terminal Session's tab sits beside the Bots' tabs.
	if _, err := f.s.browsers().DoSession(t.Context(), f.terminal, "open", json.RawMessage(`{"url":"http://127.0.0.1:9/session"}`), mustBrowserPolicy(t, f.s), "", false); err != nil {
		t.Fatal(err)
	}
	f.sessionTab = bridge.Status().Tabs[0].Tab
	return f
}

func (f *botTargetFixture) owner(key pa.BotKey, project pa.ProjectScope, conversation string, forgotten int64) string {
	return projectassistanthost.BrowserOwner(pa.Fence{Key: key, Project: project, ConversationID: conversation}, forgotten)
}

// botOpen is the Bot's own turn opening a tab, through the same composition
// callback its owned MCP tool uses.
func (f *botTargetFixture) botOpen(t *testing.T, owner string) int {
	t.Helper()
	before := map[int]bool{}
	bridge, _ := f.s.browsers().Channel("isolated")
	for _, tab := range bridge.Status().Tabs {
		before[tab.Tab] = true
	}
	if _, err := f.s.projectAssistantBrowser().Command(t.Context(), owner, app.BrowserCommand{Op: "open", Args: json.RawMessage(`{"url":"http://127.0.0.1:9/bot"}`)}); err != nil {
		t.Fatal(err)
	}
	for _, tab := range bridge.Status().Tabs {
		if !before[tab.Tab] && tab.Conversation == owner {
			return tab.Tab
		}
	}
	t.Fatal("the Bot's open made no tab")
	return 0
}

func (f *botTargetFixture) get(t *testing.T, token, path string, query url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, "GET", path+"?"+query.Encode(), token, nil)
}

func (f *botTargetFixture) post(t *testing.T, token, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, "POST", path, token, body)
}

func botTarget(bot, conversation string, extra ...string) url.Values {
	q := url.Values{"bot_id": {bot}, "conversation": {conversation}}
	for i := 0; i+1 < len(extra); i += 2 {
		q.Set(extra[i], extra[i+1])
	}
	return q
}

func wantCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || !strings.Contains(w.Body.String(), `"`+code+`"`) {
		t.Fatalf("want %d %s, got %d %s", status, code, w.Code, w.Body)
	}
}

type botStatus struct {
	Owner        string               `json:"owner"`
	BotID        string               `json:"bot_id"`
	Conversation string               `json:"conversation"`
	Status       browserbridge.Status `json:"status"`
}

func decodeBotStatus(t *testing.T, w *httptest.ResponseRecorder) botStatus {
	t.Helper()
	paServerOK(t, w)
	var s botStatus
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// A person's view of a Bot's browser goes through the real HTTP gate, Host
// and SQLite, the production Profiles and Bridge, and a scripted native host.
// Each Bot sees only its own tabs; a request naming no Bot sees none of them.
func TestABotTargetReachesOnlyThatBotsTabsThroughTheRealHost(t *testing.T) {
	f := newBotTargetFixture(t)
	ownerA := f.owner(f.key, f.projectA, f.convA, 0)
	ownerB := f.owner(f.keyB, f.projectB, f.convB, 0)
	tabA := f.botOpen(t, ownerA)
	tabB := f.botOpen(t, ownerB)

	statusA := f.get(t, f.readonly, "/v1/browser/status", botTarget(f.key.BotID, f.convA))
	t.Logf("sample targeted status: %s", statusA.Body)
	st := decodeBotStatus(t, statusA)
	if st.Owner != ownerA || st.BotID != f.key.BotID || st.Conversation != f.convA {
		t.Fatalf("the daemon derived %q for Bot A, want %q (%+v)", st.Owner, ownerA, st)
	}
	if len(st.Status.Tabs) != 1 || st.Status.Tabs[0].Tab != tabA || st.Status.Tabs[0].Conversation != ownerA {
		t.Fatalf("Bot A's view lists %+v", st.Status.Tabs)
	}
	if b := decodeBotStatus(t, f.get(t, f.local, "/v1/browser/status", botTarget(f.keyB.BotID, f.convB))); b.Owner != ownerB || len(b.Status.Tabs) != 1 || b.Status.Tabs[0].Tab != tabB {
		t.Fatalf("Bot B's view: %+v", b)
	}
	// A view naming no Bot lists the Session's tab and no Bot's.
	all := decodeBotStatus(t, f.get(t, f.local, "/v1/browser/status", url.Values{}))
	if len(all.Status.Tabs) != 1 || all.Status.Tabs[0].Tab != f.sessionTab || all.Owner != "" {
		t.Fatalf("the untargeted view: %+v", all.Status.Tabs)
	}

	// Partial or spoofed targets are bad requests; a stale or another Bot's
	// conversation is refused by the Host.
	wantCode(t, f.get(t, f.local, "/v1/browser/status", url.Values{"bot_id": {f.key.BotID}}), 400, "browser_bad_request")
	wantCode(t, f.get(t, f.local, "/v1/browser/status", url.Values{"conversation": {ownerA}}), 400, "browser_bad_request")
	stale := f.get(t, f.local, "/v1/browser/status", botTarget(f.key.BotID, f.convB))
	t.Logf("sample stale conversation: %d %s", stale.Code, stale.Body)
	wantCode(t, stale, 409, string(pa.StaleVersion))
	wantCode(t, f.get(t, f.local, "/v1/browser/status", botTarget("no-such-bot", f.convA)), 404, string(pa.NotFound))
	wantCode(t, f.get(t, f.local, "/v1/browser/activity", url.Values{"bot_id": {f.key.BotID}, "conversation": {""}}), 400, "browser_bad_request")
	// The machine token is not a person and cannot name a Bot.
	machine := httptest.NewRequest("GET", "/v1/browser/status?"+botTarget(f.key.BotID, f.convA).Encode(), nil)
	machine.Host = "127.0.0.1"
	machine.Header.Set(machineHeader, f.machine)
	mw := httptest.NewRecorder()
	f.h.ServeHTTP(mw, machine)
	wantCode(t, mw, 403, string(pa.ScopeDenied))

	// The picture of Bot A's own tab, for a read-only device.
	shot := f.get(t, f.readonly, "/v1/browser/screenshot", botTarget(f.key.BotID, f.convA, "tab", itoa(tabA), "browser_mode", "isolated"))
	paServerOK(t, shot)
	if !strings.Contains(shot.Body.String(), "c2NyaXB0ZWQ=") {
		t.Fatalf("screenshot: %s", shot.Body)
	}
	// Not of Bot B's tab, the Session's tab, or an unowned tab, under Bot A.
	before := len(f.host.commands())
	wantCode(t, f.get(t, f.readonly, "/v1/browser/screenshot", botTarget(f.key.BotID, f.convA, "tab", itoa(tabB), "browser_mode", "isolated")), 409, string(browserbridge.OtherSessionsTab))
	wantCode(t, f.get(t, f.readonly, "/v1/browser/screenshot", botTarget(f.key.BotID, f.convA, "tab", itoa(f.sessionTab), "browser_mode", "isolated")), 409, string(browserbridge.OtherSessionsTab))
	if w := f.get(t, f.readonly, "/v1/browser/screenshot", botTarget(f.key.BotID, f.convA, "tab", "999", "browser_mode", "isolated")); w.Code < 400 {
		t.Fatalf("an unowned tab answered %d", w.Code)
	}
	// And a request naming no Bot cannot look at or take over a Bot's tab.
	wantCode(t, f.get(t, f.readonly, "/v1/browser/screenshot", url.Values{"tab": {itoa(tabA)}, "browser_mode": {"isolated"}}), 409, string(browserbridge.OtherSessionsTab))
	wantCode(t, f.post(t, f.lan, "/v1/browser/control", map[string]any{"tab": tabA, "person": true, "browser_mode": "isolated"}), 409, string(browserbridge.OtherSessionsTab))
	wantCode(t, f.post(t, f.lan, "/v1/browser/control", map[string]any{"tab": tabA, "person": true, "conversation": f.convA}), 400, "browser_bad_request")
	if after := len(f.host.commands()); after != before {
		t.Fatalf("refused requests reached the host: %d commands", after-before)
	}

	// A read-only device may look but not act.
	wantCode(t, f.post(t, f.readonly, "/v1/browser/control", map[string]any{"tab": tabA, "person": true, "browser_mode": "isolated", "bot_id": f.key.BotID, "conversation": f.convA}), 403, "forbidden")
	wantCode(t, f.post(t, f.readonly, "/v1/browser/person", map[string]any{"op": "click", "tab": tabA, "args": map[string]int{"x": 1, "y": 1}, "bot_id": f.key.BotID, "conversation": f.convA}), 403, "forbidden")
	wantCode(t, f.post(t, f.readonly, "/v1/browser/open", map[string]any{"conversation": f.convA, "url": "http://127.0.0.1:9/x", "bot_id": f.key.BotID}), 403, "forbidden")
	wantCode(t, f.post(t, f.readonly, "/v1/browser/mute", map[string]any{"conversation": f.convA, "muted": true, "bot_id": f.key.BotID}), 403, "forbidden")

	// Bot B's target cannot take over Bot A's tab.
	wantCode(t, f.post(t, f.lan, "/v1/browser/control", map[string]any{"tab": tabA, "person": true, "browser_mode": "isolated", "bot_id": f.keyB.BotID, "conversation": f.convB}), 409, string(browserbridge.OtherSessionsTab))

	// Takeover, the person's input, and hand back on Bot A's own tab.
	target := map[string]any{"bot_id": f.key.BotID, "conversation": f.convA, "browser_mode": "isolated"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range target {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	wantCode(t, f.post(t, f.lan, "/v1/browser/person", with(map[string]any{"op": "click", "tab": tabA, "args": map[string]int{"x": 1, "y": 1}})), 409, string(browserbridge.NotTakenOver))
	paServerOK(t, f.post(t, f.lan, "/v1/browser/control", with(map[string]any{"tab": tabA, "person": true})))
	if _, err := f.s.projectAssistantBrowser().Command(t.Context(), ownerA, app.BrowserCommand{Op: "read", Args: json.RawMessage(`{"tab":` + itoa(tabA) + `}`)}); err == nil || !strings.Contains(err.Error(), string(browserbridge.PersonControlling)) {
		t.Fatalf("the Bot acted on a tab the person holds: %v", err)
	}
	paServerOK(t, f.post(t, f.lan, "/v1/browser/person", with(map[string]any{"op": "click", "tab": tabA, "args": map[string]int{"x": 3, "y": 4}})))
	if held := decodeBotStatus(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.key.BotID, f.convA))); len(held.Status.PersonTabs) != 1 || held.Status.PersonTabs[0] != tabA {
		t.Fatalf("taken over: %+v", held.Status)
	}
	paServerOK(t, f.post(t, f.lan, "/v1/browser/control", with(map[string]any{"tab": tabA, "person": false})))
	if _, err := f.s.projectAssistantBrowser().Command(t.Context(), ownerA, app.BrowserCommand{Op: "read", Args: json.RawMessage(`{"tab":` + itoa(tabA) + `}`)}); err != nil {
		t.Fatalf("after hand back the Bot reads its tab: %v", err)
	}
	var personClick bool
	for _, c := range f.host.commands() {
		if c.Op == "click" && c.Tab == tabA && c.Person {
			personClick = true
		}
	}
	if !personClick {
		t.Fatal("the person's click never reached the host")
	}

	// The person opens a tab for Bot A; it belongs to Bot A and starts taken over.
	paServerOK(t, f.post(t, f.lan, "/v1/browser/open", map[string]any{"conversation": f.convA, "url": "http://127.0.0.1:9/person", "bot_id": f.key.BotID}))
	opened := decodeBotStatus(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.key.BotID, f.convA)))
	if len(opened.Status.Tabs) != 2 || len(opened.Status.PersonTabs) != 1 || opened.Status.PersonTabs[0] == tabA {
		t.Fatalf("after the person's open: %+v", opened.Status)
	}
	// Muting is the Bot's own; the Session view never lists a Bot's key.
	paServerOK(t, f.post(t, f.lan, "/v1/browser/mute", map[string]any{"conversation": f.convA, "muted": true, "bot_id": f.key.BotID}))
	if m := decodeBotStatus(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.key.BotID, f.convA))); len(m.Status.Muted) != 1 || m.Status.Muted[0] != ownerA {
		t.Fatalf("muted: %+v", m.Status.Muted)
	}
	if m := decodeBotStatus(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.keyB.BotID, f.convB))); len(m.Status.Muted) != 0 {
		t.Fatalf("Bot B sees Bot A's mute: %+v", m.Status.Muted)
	}
	if m := decodeBotStatus(t, f.get(t, f.readonly, "/v1/browser/status", url.Values{})); len(m.Status.Muted) != 0 {
		t.Fatalf("the untargeted view lists a Bot's mute: %+v", m.Status.Muted)
	}

	// Activity and audit are the Bot's own rows; the untargeted audit has none.
	var activity struct {
		Rows []browserbridge.Activity `json:"rows"`
	}
	w := f.get(t, f.readonly, "/v1/browser/activity", botTarget(f.key.BotID, f.convA))
	paServerOK(t, w)
	_ = json.Unmarshal(w.Body.Bytes(), &activity)
	if len(activity.Rows) == 0 {
		t.Fatal("Bot A has no activity")
	}
	for _, row := range activity.Rows {
		if row.Conversation != ownerA {
			t.Fatalf("Bot A's activity has %q", row.Conversation)
		}
	}
	var audit struct {
		Rows []browserbridge.AuditRow `json:"rows"`
	}
	w = f.get(t, f.readonly, "/v1/browser/audit", botTarget(f.key.BotID, f.convA, "limit", "50"))
	paServerOK(t, w)
	_ = json.Unmarshal(w.Body.Bytes(), &audit)
	ops := map[string]bool{}
	for _, row := range audit.Rows {
		if row.Conversation != ownerA {
			t.Fatalf("Bot A's audit has %q", row.Conversation)
		}
		ops[row.Op] = true
	}
	if !ops["open"] || !ops["takeover"] || !ops["handback"] {
		t.Fatalf("Bot A's audit ops: %v", ops)
	}
	w = f.get(t, f.readonly, "/v1/browser/audit", url.Values{"limit": {"200"}})
	paServerOK(t, w)
	audit.Rows = nil
	_ = json.Unmarshal(w.Body.Bytes(), &audit)
	if len(audit.Rows) == 0 {
		t.Fatal("the untargeted audit lost the Session's rows")
	}
	for _, row := range audit.Rows {
		if strings.HasPrefix(row.Conversation, browserbridge.BotOwnerPrefix) {
			t.Fatalf("the untargeted audit lists a Bot row: %+v", row)
		}
	}
	wantCode(t, f.get(t, f.readonly, "/v1/browser/audit", url.Values{"conversation": {ownerA}}), 400, "browser_bad_request")

	// The Session view and its routes are unchanged.
	paServerOK(t, f.get(t, f.readonly, "/v1/browser/screenshot", url.Values{"tab": {itoa(f.sessionTab)}, "browser_mode": {"isolated"}}))
	paServerOK(t, f.post(t, f.lan, "/v1/browser/control", map[string]any{"tab": f.sessionTab, "person": true, "browser_mode": "isolated"}))
	paServerOK(t, f.post(t, f.lan, "/v1/browser/control", map[string]any{"tab": f.sessionTab, "person": false, "browser_mode": "isolated"}))
}

// A forget gives the Bot a new owner: the person's view follows it, and the
// old epoch's tab can no longer be read or acted on through the Bot target.
// A disabled feature or a deleted Bot shows no browser at all.
func TestABotTargetFollowsForgetFeatureAndDeletion(t *testing.T) {
	f := newBotTargetFixture(t)
	ownerA := f.owner(f.key, f.projectA, f.convA, 0)
	oldTab := f.botOpen(t, ownerA)
	got := f.get(t, f.local, "/v1/project-assistants/"+f.key.BotID, url.Values{"machine_id": {f.key.MachineID}})
	paServerOK(t, got)
	var bot contract.ProjectAssistantGetResponse
	_ = json.Unmarshal(got.Body.Bytes(), &bot)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/memory/forget", f.local, contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation, "forget"), Project: bot.Bot.Project, ExpectedRevision: bot.Conversation.MemoryRevision, SourceIds: []string{"synthetic-forgotten"}}))
	after := decodeBotStatus(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.key.BotID, f.convA)))
	if after.Owner == ownerA || after.Owner != f.owner(f.key, f.projectA, f.convA, bot.Conversation.MemoryRevision+1) || len(after.Status.Tabs) != 0 {
		t.Fatalf("after forget: %+v", after)
	}
	wantCode(t, f.get(t, f.readonly, "/v1/browser/screenshot", botTarget(f.key.BotID, f.convA, "tab", itoa(oldTab), "browser_mode", "isolated")), 409, string(browserbridge.OtherSessionsTab))
	wantCode(t, f.post(t, f.lan, "/v1/browser/control", map[string]any{"tab": oldTab, "person": true, "browser_mode": "isolated", "bot_id": f.key.BotID, "conversation": f.convA}), 409, string(browserbridge.OtherSessionsTab))

	// Bot B still works; then the feature is turned off.
	paServerOK(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.keyB.BotID, f.convB)))
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	wantCode(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.keyB.BotID, f.convB)), 403, string(pa.Disabled))
	wantCode(t, f.post(t, f.lan, "/v1/browser/mute", map[string]any{"conversation": f.convB, "muted": true, "bot_id": f.keyB.BotID}), 403, string(pa.ReadOnly))
	f.enable(t)
	paServerOK(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.keyB.BotID, f.convB)))

	commandB := contract.ProjectAssistantDeleteRequest{Command: contract.ProjectAssistantCommand{Key: paWireBotKey(f.keyB), Operation: contract.ProjectAssistantOperation(pa.DeleteOperation), IdempotencyKey: "delete-b", ContentHash: "not-authority"}}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.keyB.BotID+"/delete", f.local, commandB))
	wantCode(t, f.get(t, f.readonly, "/v1/browser/status", botTarget(f.keyB.BotID, f.convB)), 404, string(pa.NotFound))
}

// A source that changes between the first resolution and admission is caught
// at admission or before publication: a forget while the host holds Bot A's
// picture keeps the picture from the person, and nothing is replayed.
func TestABotTargetRechecksItsAuthorityAroundTheBrowserEffect(t *testing.T) {
	f := newBotTargetFixture(t)
	ownerA := f.owner(f.key, f.projectA, f.convA, 0)
	tabA := f.botOpen(t, ownerA)
	got := f.get(t, f.local, "/v1/project-assistants/"+f.key.BotID, url.Values{"machine_id": {f.key.MachineID}})
	var bot contract.ProjectAssistantGetResponse
	_ = json.Unmarshal(got.Body.Bytes(), &bot)

	f.host.mu.Lock()
	f.host.hold, f.host.held = make(chan struct{}), make(chan struct{})
	held, hold := f.host.held, f.host.hold
	f.host.mu.Unlock()
	answer := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answer <- f.get(t, f.readonly, "/v1/browser/screenshot", botTarget(f.key.BotID, f.convA, "tab", itoa(tabA), "browser_mode", "isolated"))
	}()
	<-held
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/memory/forget", f.local, contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation, "forget-race"), Project: bot.Bot.Project, ExpectedRevision: bot.Conversation.MemoryRevision, SourceIds: []string{"synthetic-forgotten"}}))
	close(hold)
	w := <-answer
	wantCode(t, w, 409, string(pa.StaleVersion))
	if strings.Contains(w.Body.String(), "c2NyaXB0ZWQ=") {
		t.Fatal("the old epoch's picture was published")
	}
	f.host.mu.Lock()
	f.host.hold, f.host.held = nil, nil
	f.host.mu.Unlock()
	shots := 0
	for _, c := range f.host.commands() {
		if c.Op == "screenshot" {
			shots++
		}
	}
	if shots != 1 {
		t.Fatalf("the screenshot ran %d times", shots)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

var _ = http.StatusOK

// A Bot's tab reaching a site for the first time sends the person's new-site
// notice naming that Bot, in English and Taiwan Traditional Chinese, and
// never lands on a terminal Session. A terminal Session keeps its own words.
// An owner no current Bot derives is not announced as any Bot or Session.
func TestABotsNewSiteNoticeNamesTheBotNotASession(t *testing.T) {
	f := newBotTargetFixture(t)
	if f.s.broker == nil {
		t.Fatal("the fixture has no broker")
	}
	type pushed struct{ title, body, terminal string }
	got := make(chan pushed, 8)
	f.s.broker.Push = func(_ context.Context, title, body, terminal, _ string) (int, int, error) {
		got <- pushed{title, body, terminal}
		return 1, 0, nil
	}
	f.s.broker.NotifyEnabled = func() bool { return true }
	if _, err := f.s.settingsFile().Set(map[string]any{"browser_notify_new_site": true}); err != nil {
		t.Fatal(err)
	}
	next := func(what string) pushed {
		t.Helper()
		select {
		case n := <-got:
			return n
		case <-time.After(5 * time.Second):
			t.Fatalf("no notice for %s", what)
		}
		return pushed{}
	}
	f.host.mu.Lock()
	f.host.url = "https://bot-site.example/form"
	f.host.mu.Unlock()
	ownerA := f.owner(f.key, f.projectA, f.convA, 0)
	f.botOpen(t, ownerA)
	n := next("the Bot")
	if n.title != "Clawdline: Bot Synthetic Bot is using a new site" || n.body != "bot-site.example. Open Synthetic Bot's chat in Bots to watch or take over." || n.terminal != "" {
		t.Fatalf("Bot notice = %+v", n)
	}

	notice, ok := f.s.browserSiteNotice(t.Context(), browserbridge.NewSite{Conversation: ownerA, Site: "bot-site.example"})
	if !ok || notice.bot != f.key || notice.conversation != f.convA || notice.session != "" {
		t.Fatalf("Bot notice target = %+v %v", notice, ok)
	}
	if _, err := f.s.settingsFile().Set(map[string]any{"product_language": "zh-Hant"}); err != nil {
		t.Fatal(err)
	}
	notice, _ = f.s.browserSiteNotice(t.Context(), browserbridge.NewSite{Conversation: ownerA, Site: "bot-site.example"})
	if notice.title != "Bot Synthetic Bot 開始操作新網站" || notice.body != "bot-site.example。到 Bots 打開 Synthetic Bot 的對話，即可查看畫面或接手。" {
		t.Fatalf("zh-Hant Bot notice = %+v", notice)
	}
	if _, err := f.s.settingsFile().Set(map[string]any{"product_language": "en"}); err != nil {
		t.Fatal(err)
	}

	// The second Bot's notice names the second Bot.
	ownerB := f.owner(f.keyB, f.projectB, f.convB, 0)
	f.botOpen(t, ownerB)
	if n = next("the second Bot"); n.title != "Clawdline: Bot Synthetic Bot B is using a new site" || n.terminal != "" {
		t.Fatalf("second Bot notice = %+v", n)
	}

	// A terminal Session that is not watched keeps the Session words.
	f.host.mu.Lock()
	f.host.url = "https://session-site.example/"
	f.host.mu.Unlock()
	if _, err := f.s.browsers().DoSession(t.Context(), f.terminal, "open", json.RawMessage(`{"url":"https://session-site.example/"}`), mustBrowserPolicy(t, f.s), "", false); err != nil {
		t.Fatal(err)
	}
	if n = next("the Session"); n.title != "Clawdline: Session is using a new site" || n.body != "session-site.example. Open the Session to watch or take over." {
		t.Fatalf("Session notice = %+v", n)
	}

	// After a forget the old owner names no Bot, and nothing is sent.
	if _, ok := f.s.browserSiteNotice(t.Context(), browserbridge.NewSite{Conversation: f.owner(f.key, f.projectA, f.convA, 7), Site: "x.example"}); ok {
		t.Fatal("an owner no current Bot derives was announced")
	}
	select {
	case n := <-got:
		t.Fatalf("unexpected notice %+v", n)
	case <-time.After(100 * time.Millisecond):
	}
}
