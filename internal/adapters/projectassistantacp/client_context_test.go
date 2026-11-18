package projectassistantacp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func nativeContextReader(t *testing.T, c any) app.NativeContextReader {
	t.Helper()
	reader, ok := c.(app.NativeContextReader)
	if !ok {
		t.Fatal("the ACP client has no qualified context-reading port")
	}
	return reader
}

func contextUpdate(t *testing.T, c *Client, session, update string) error {
	t.Helper()
	return c.handle(frame{Method: "session/update", Params: json.RawMessage(`{"sessionId":"` + session + `","update":` + update + `}`)})
}

func TestNativeContextReadsIdleACPMetadata(t *testing.T) {
	c := &Client{sessionID: "own-session", fence: testFence()}
	// The wire shapes are those of the pinned adapter; counts are synthetic.
	if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact","description":"Synthetic compact command"}]}`); err != nil {
		t.Fatal(err)
	}
	if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"usage_update","used":80000,"size":100000}`); err != nil {
		t.Fatal(err)
	}
	got := nativeContextReader(t, c).NativeContext()
	if got.Fence != testFence() || got.NativeSessionID != "own-session" || !got.CommandsKnown || !got.CompactAdvertised || !got.UsageKnown || got.UsedTokens != 80000 || got.WindowTokens != 100000 {
		t.Fatalf("idle metadata lost identity or actual readings: %+v", got)
	}
	if c.turnID != "" {
		t.Fatal("reading native metadata created a prompt turn")
	}
}

func TestNativeContextSeparatesUnknownFromZeroAndUnsupported(t *testing.T) {
	c := &Client{sessionID: "own-session", fence: testFence()}
	r := nativeContextReader(t, c)
	if got := r.NativeContext(); got.CommandsKnown || got.UsageKnown {
		t.Fatal("an unreported value became known", got)
	}
	for _, update := range []string{
		`{"sessionUpdate":"available_commands_update","availableCommands":[]}`,
		`{"sessionUpdate":"usage_update","used":0,"size":100000}`,
	} {
		if err := contextUpdate(t, c, "own-session", update); err != nil {
			t.Fatal(err)
		}
	}
	got := r.NativeContext()
	if !got.CommandsKnown || got.CompactAdvertised || !got.UsageKnown || got.UsedTokens != 0 || got.WindowTokens != 100000 {
		t.Fatal("known unsupported or reported zero was lost", got)
	}
	for _, update := range []string{
		`{"sessionUpdate":"usage_update","used":-1,"size":100000}`,
		`{"sessionUpdate":"usage_update","used":0,"size":0}`,
		`{"sessionUpdate":"usage_update","used":0.5,"size":100000}`,
		`{"sessionUpdate":"usage_update","used":"0","size":100000}`,
		`{"sessionUpdate":"usage_update","size":100000}`,
		`{"sessionUpdate":"usage_update","used":null,"size":100000}`,
		`{"sessionUpdate":"usage_update","used":9223372036854775808,"size":100000}`,
	} {
		if err := contextUpdate(t, c, "own-session", update); err != nil {
			t.Fatal("an unusable optional reading broke the conversation", err)
		}
		if got = r.NativeContext(); got.UsageKnown {
			t.Fatal("an invalid optional reading retained known usage", update, got)
		}
	}
	for _, commands := range []string{`null`, `{}`, `[{"name":7}]`, `[{}]`, `[{"name":"compact"},{}]`} {
		if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"available_commands_update","availableCommands":`+commands+`}`); err != nil {
			t.Fatal("unusable optional commands broke the conversation", err)
		}
		if got := r.NativeContext(); got.CommandsKnown || got.CompactAdvertised {
			t.Fatal("invalid command metadata became an unsupported verdict or authority", got)
		}
	}
}

func TestNativeContextRejectsForeignAndRetiredSessions(t *testing.T) {
	c := &Client{sessionID: "own-session", fence: testFence()}
	r := nativeContextReader(t, c)
	if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"usage_update","used":4,"size":100}`); err != nil {
		t.Fatal(err)
	}
	if err := contextUpdate(t, c, "foreign-session", `{"sessionUpdate":"usage_update","used":90,"size":100}`); pa.Code(err) != pa.StaleGeneration {
		t.Fatal("foreign context was not refused", err)
	}
	if r.NativeContext().UsedTokens != 4 {
		t.Fatal("foreign metadata changed the current session")
	}
	c.cancelled = true
	if got := r.NativeContext(); got.UsageKnown || got.CommandsKnown || got.CompactAdvertised {
		t.Fatal("a cancelled session still advertised usable context", got)
	}
	c.cancelled = false
	c.failure = refusal(pa.ExecutionEffectUnknown, "closed")
	if got := r.NativeContext(); got.UsageKnown || got.CommandsKnown || got.CompactAdvertised {
		t.Fatal("a closed session still advertised usable context", got)
	}
}

func TestNativeContextWaitsForTheAuthoritativeNativeIdentity(t *testing.T) {
	c := &Client{fence: testFence()}
	r := nativeContextReader(t, c)
	if err := contextUpdate(t, c, "pending-session", `{"sessionUpdate":"usage_update","used":8,"size":100}`); err != nil {
		t.Fatal("early session metadata was discarded before the session reply", err)
	}
	if r.NativeContext().UsageKnown {
		t.Fatal("unbound metadata acquired authority")
	}
	c.sessionID = "pending-session"
	if got := r.NativeContext(); !got.UsageKnown || got.UsedTokens != 8 || got.NativeSessionID != "pending-session" {
		t.Fatal("matching session reply did not qualify the reading", got)
	}
	c.sessionID = "replacement-session"
	if r.NativeContext().UsageKnown {
		t.Fatal("a replacement session inherited another session's reading")
	}
	if err := contextUpdate(t, c, "replacement-session", `{"sessionUpdate":"available_commands_update","availableCommands":[]}`); err != nil {
		t.Fatal(err)
	}
	if got := r.NativeContext(); !got.CommandsKnown || got.UsageKnown {
		t.Fatal("replacement command metadata revived old usage", got)
	}
}

func TestNativeContextDoesNotCombineUnboundIdentities(t *testing.T) {
	c := &Client{fence: testFence()}
	if err := contextUpdate(t, c, "first-session", `{"sessionUpdate":"usage_update","used":8,"size":100}`); err != nil {
		t.Fatal(err)
	}
	if err := contextUpdate(t, c, "other-session", `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact"}]}`); pa.Code(err) != pa.StaleGeneration {
		t.Fatal("unbound metadata mixed native identities", err)
	}
	c.sessionID = "other-session"
	if got := nativeContextReader(t, c).NativeContext(); got.UsageKnown || got.CommandsKnown {
		t.Fatal("the authoritative reply qualified another identity's report", got)
	}
}

func TestNativeContextSurvivesInstalledSessionIsolation(t *testing.T) {
	c := &Client{sessionID: "own-session", fence: testFence()}
	if err := contextUpdate(t, c, "own-session", `{"sessionUpdate":"usage_update","used":8,"size":100}`); err != nil {
		t.Fatal(err)
	}
	wrapped := &isolatedNativeSession{ProviderSession: c}
	if got := nativeContextReader(t, wrapped).NativeContext(); !got.UsageKnown || got.UsedTokens != 8 || got.Fence != testFence() {
		t.Fatal("installed-session isolation lost qualified context", got)
	}
	// Erasing an optional interface must leave the capability unknown, rather
	// than inventing counts or borrowing another session's readings.
	wrapped.ProviderSession = struct{ app.ProviderSession }{c}
	if got := nativeContextReader(t, wrapped).NativeContext(); got != (app.NativeContextState{}) {
		t.Fatal("a provider without the optional port acquired readings", got)
	}
}

func TestNativeContextTraversesTheACPWireWithoutARealModel(t *testing.T) {
	c, _ := openTestClient(t, "native-context", func(context.Context, app.ProtocolEvent) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// session/load's ordered reply drains the preceding session/new metadata.
	// The scripted peer opens no model and this barrier sends no prompt.
	if _, err := c.call(ctx, "session/load", map[string]string{"sessionId": c.SessionID()}); err != nil {
		t.Fatal(err)
	}
	r := nativeContextReader(t, c)
	if got := r.NativeContext(); got.Fence != testFence() || got.NativeSessionID != "scripted-session" || !got.CommandsKnown || !got.CompactAdvertised || !got.UsageKnown || got.UsedTokens != 0 || got.WindowTokens != 100000 {
		t.Fatal("idle pipe metadata did not survive initialization", got)
	}
	c.mu.Lock()
	turn := c.turnID
	c.mu.Unlock()
	if turn != "" {
		t.Fatal("metadata reading created a prompt", turn)
	}
	result, err := sendTestPrompt(t, c).Wait(ctx)
	if err != nil || result.FinalText != "Synthetic answer" {
		t.Fatal("context reports changed normal reply delivery", result, err)
	}
	if got := r.NativeContext(); !got.UsageKnown || got.UsedTokens != 12 || !got.CompactAdvertised {
		t.Fatal("prompt metadata did not precede the completed reply", got)
	}
	if err := c.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.NativeContext(); got.UsageKnown || got.CommandsKnown || got.CompactAdvertised {
		t.Fatal("actual cancellation left usable context", got)
	}
}
