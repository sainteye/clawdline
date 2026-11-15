package http

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var rosterLiveRows = []struct{ id, name, phase string }{
	{"reading", "Reading helper", "start"},
	{"approval", "Approval helper", "waiting"},
	{"failed", "Failed helper", "failed"},
	{"finished", "Finished helper", "done"},
	{"unknown", "Unknown helper", "unknown"},
}

// All phase writes use the Service's own Store, including its post-commit bus.
// This fixture never calls a runtime, native adapter, model or provider.
type rosterLiveFixture struct {
	f  *rosterHTTPFixture
	mu sync.Mutex
}

func (l *rosterLiveFixture) command(key pa.BotKey, op pa.Operation) pa.Command {
	return pa.Command{Key: key, Operation: op, IdempotencyKey: fmt.Sprintf("live-%d", l.f.ids.Add(1)), ContentHash: strings.Repeat("a", 64)}
}

func (l *rosterLiveFixture) commit(ctx context.Context, key pa.BotKey, version int64, m app.Mutation) error {
	m.Command = l.command(key, pa.EditOperation)
	m.ExpectedVersion = version
	m.Receipt = pa.Receipt{ID: m.Command.IdempotencyKey, State: pa.EffectDelivered}
	_, err := l.f.store.Commit(ctx, m)
	return err
}

func (l *rosterLiveFixture) reset(ctx context.Context) error {
	l.f.revoked.Store(false)
	for _, row := range rosterLiveRows {
		key := pa.BotKey{MachineID: "machine", BotID: row.id}
		if err := l.f.store.DeleteBot(ctx, key); err != nil {
			var failure *pa.Failure
			if !errors.As(err, &failure) || failure.Code != pa.NotFound {
				return err
			}
		}
		now := time.Now().UTC()
		b := pa.Bot{Key: key, Project: l.f.scope, ConversationID: "chat/" + row.id, Name: row.name, Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "Synthetic instructions"}, Version: 1, CreatedAt: now, UpdatedAt: now}
		c := pa.Conversation{Key: key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
		cmd := l.command(key, pa.CreateOperation)
		if _, err := l.f.store.Commit(ctx, app.Mutation{Command: cmd, Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: cmd.IdempotencyKey, State: pa.EffectDelivered}}); err != nil {
			return err
		}
		if err := l.change(ctx, key, row.phase); err != nil {
			return err
		}
	}
	return nil
}

func (l *rosterLiveFixture) change(ctx context.Context, key pa.BotKey, phase string) error {
	x, err := l.f.store.Export(ctx, key)
	if err != nil {
		return err
	}
	switch phase {
	case "delete":
		return l.f.store.DeleteBot(ctx, key)
	case "hide", "unhide":
		b := x.Bot
		b.Hidden = phase == "hide"
		return l.commit(ctx, key, b.Version, app.Mutation{Bot: &b})
	case "read":
		_, err := l.f.store.MarkRead(ctx, l.command(key, pa.ReadOperation), pa.ReadCursor{Key: key, ConversationID: x.Conversation.ID, ReaderID: "reader", MessageSeq: x.Conversation.LastMessageSeq})
		return err
	}
	id := fmt.Sprintf("live-fact-%d", l.f.ids.Add(1))
	c := x.Conversation
	fence := pa.Fence{Key: key, Project: x.Bot.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	m := app.Mutation{}
	for _, turn := range x.Turns {
		if turn.ID == c.ActiveTurnID {
			saved := turn
			m.Turn = &saved
			break
		}
	}
	if m.Turn != nil && m.Turn.State == pa.TurnWaitingPermission && (phase == "done" || phase == "failed" || phase == "unknown") {
		// Follow the actual domain transition instead of bypassing its guard.
		if err := l.change(ctx, key, "resume"); err != nil {
			return err
		}
		return l.change(ctx, key, phase)
	}
	if m.Turn == nil {
		if fence.BindingID == "" {
			fence.BindingID, fence.Generation = "live-binding", 1
			c.BindingID, c.Generation = fence.BindingID, fence.Generation
			m.Binding = &pa.ExecutionBinding{Fence: fence, Agent: x.Bot.Agent, MemoryRevision: 1, InstructionRevision: 1}
		}
		m.Turn = &pa.Turn{ID: id, Fence: fence, MessageID: "message-" + id, State: pa.TurnRunning, Instructions: x.Bot.Instructions, MemoryRevision: 1, IdempotencyKey: id}
		c.ActiveTurnID = id
		m.Messages = []pa.Message{{ID: m.Turn.MessageID, Fence: fence, TurnID: id, Kind: pa.UserMessage, Text: "Read the notes", At: time.Now().UTC()}}
	}
	// Pending offers must not override resumed or terminal activity.
	for _, offer := range x.Permissions {
		if offer.State == pa.PermissionPending {
			offer.State, offer.SelectedOptionID = pa.PermissionResolved, "allow"
			m.Permissions = append(m.Permissions, offer)
		}
	}
	trace := pa.Trace{TurnID: m.Turn.ID, Category: "tool_call", Text: "Read notes.md"}
	switch phase {
	case "start", "resume", "writing":
		m.Turn.State = pa.TurnRunning
		if phase == "writing" {
			trace.Category, trace.Text = "agent_message_chunk", "Synthetic partial reply"
		}
	case "waiting":
		m.Turn.State = pa.TurnWaitingPermission
		m.Permissions = append(m.Permissions, pa.PermissionOffer{ID: id, Fence: fence, TurnID: m.Turn.ID, NativeRequestID: id, State: pa.PermissionPending, ExpiresAt: time.Now().Add(time.Hour), Action: &pa.PermissionAction{Title: "Run unit tests"}, Options: []pa.PermissionOption{{ID: "allow", Kind: pa.AllowOnce, Allow: true}}})
	case "done", "failed", "unknown":
		c.ActiveTurnID = ""
		m.Turn.State = pa.TurnSucceeded
		if phase == "failed" {
			m.Turn.State, m.Turn.Failure = pa.TurnFailed, &pa.Failure{Code: pa.AdapterNotFound}
		}
		if phase == "unknown" {
			m.Turn.State, m.Turn.Failure = pa.TurnUnknown, &pa.Failure{Code: pa.ExecutionEffectUnknown}
		}
		m.Messages = append(m.Messages, pa.Message{ID: "reply-" + id, Fence: fence, TurnID: m.Turn.ID, Kind: pa.AssistantMessage, Text: "Fresh reply", At: time.Now().UTC()})
	default:
		return fmt.Errorf("unsupported phase %q", phase)
	}
	if c.ActiveTurnID != "" {
		m.Events = []pa.Event{{Version: pa.ContractVersion, ID: id, Fence: fence, Kind: pa.TraceUpdated, Trace: &trace}}
	}
	c.Version++
	m.Conversation = &c
	return l.commit(ctx, key, x.Bot.Version, m)
}

func (l *rosterLiveFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/fixture/phase" {
		l.f.handler.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	bot := r.URL.Query().Get("bot")
	if bot == "" {
		bot = "reading"
	}
	known := false
	for _, row := range rosterLiveRows {
		known = known || row.id == bot
	}
	if !known {
		http.Error(w, "unknown fixture bot", http.StatusBadRequest)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var err error
	switch phase := r.URL.Query().Get("phase"); phase {
	case "reset":
		err = l.reset(r.Context())
	case "revoked":
		l.f.revoked.Store(true)
	case "start", "writing", "done", "waiting", "resume", "read", "hide", "unhide", "delete":
		err = l.change(r.Context(), pa.BotKey{MachineID: "machine", BotID: bot}, phase)
	default:
		http.Error(w, "unknown fixture phase", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestProjectAssistantRosterLiveHTTP(t *testing.T) {
	f := newRosterHTTPFixture(t, nil, nil)
	live := &rosterLiveFixture{f: f}
	if err := live.reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(live)
	defer server.Close()
	phase := func(name, bot string) {
		t.Helper()
		resp, err := http.Post(server.URL+"/fixture/phase?"+url.Values{"phase": {name}, "bot": {bot}}.Encode(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("phase %s: %d %s", name, resp.StatusCode, body)
		}
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(server.URL + "/v1/project-assistants/roster-events?machine_id=machine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("outer handler did not delegate SSE", resp.Status)
	}
	r := &rosterSSEReader{body: resp.Body, reader: bufio.NewReader(resp.Body), transcript: &f.transcript}
	if hello := r.next(t); hello.Type != pa.RosterHello {
		t.Fatal(hello)
	}
	snapshot := r.next(t)
	if snapshot.Type != pa.RosterSnapshotComplete || snapshot.Cursor == "" || len(snapshot.Changes) != len(rosterLiveRows) {
		t.Fatal("incomplete baseline", snapshot)
	}
	rows := map[string]*pa.BotSummary{}
	for _, change := range snapshot.Changes {
		if change.Removed || change.Summary == nil {
			t.Fatal(change)
		}
		rows[change.Key.BotID] = change.Summary
	}
	for _, row := range rosterLiveRows {
		if rows[row.id] == nil || rows[row.id].Bot.Name != row.name {
			t.Fatal("missing named baseline", row, rows)
		}
	}
	if rows["reading"].Status != "working" || rows["reading"].Activity != "Read notes.md" || rows["approval"].Status != "needsInput" || rows["approval"].Activity != "Needs approval: Run unit tests" || rows["failed"].Status != "error" || rows["finished"].Status != "idle" || rows["unknown"].Status != "error" || rows["unknown"].Failure == nil || rows["unknown"].Failure.Code != pa.ExecutionEffectUnknown {
		t.Fatalf("baseline projection: %+v", rows)
	}
	cursor := snapshot.Cursor
	var revision int64
	changed := func(name string) pa.RosterChange {
		t.Helper()
		phase(name, "")
		frame := r.next(t)
		if frame.Type != pa.RosterChanges || frame.Cursor == "" || frame.Cursor == cursor || len(frame.Changes) == 0 {
			t.Fatalf("phase %s did not stream an absolute row: %+v", name, frame)
		}
		for _, change := range frame.Changes {
			if change.Key.BotID != "reading" || change.Revision <= revision {
				t.Fatal("wrong key or nonadvancing revision", change)
			}
			revision = change.Revision
		}
		cursor = frame.Cursor
		return frame.Changes[len(frame.Changes)-1]
	}
	row := changed("writing").Summary
	if row == nil || row.Status != "working" || row.Activity != "writing" || row.Bot.Name != "Reading helper" {
		t.Fatal("writing row", row)
	}
	row = changed("waiting").Summary
	if row == nil || row.Status != "needsInput" || row.PendingPermissions != 1 {
		t.Fatal("permission row", row)
	}
	row = changed("resume").Summary
	if row == nil || row.Status != "working" || row.Activity != "Read notes.md" || row.PendingPermissions != 0 {
		t.Fatal("resume retained permission", row)
	}
	row = changed("done").Summary
	if row == nil || row.Status != "idle" || row.LatestMessage != "Fresh reply" || row.PendingPermissions != 0 || row.UnreadCount == 0 {
		t.Fatal("completion row", row)
	}
	unread := row.UnreadCount
	row = changed("read").Summary
	if row == nil || row.UnreadCount != 0 || row.ReadCursor.MessageSeq == 0 {
		t.Fatal("read cursor did not stream", row)
	}
	other := f.stream(t, "", "second-reader", "")
	_ = other.next(t)
	second := other.next(t)
	other.body.Close()
	var otherRow *pa.BotSummary
	for _, change := range second.Changes {
		if change.Key.BotID == "reading" {
			otherRow = change.Summary
		}
	}
	if second.Type != pa.RosterSnapshotComplete || otherRow == nil || otherRow.UnreadCount != unread || otherRow.ReadCursor.MessageSeq != 0 {
		t.Fatal("default reader changed second reader", second)
	}
	if change := changed("hide"); !change.Removed {
		t.Fatal("hide did not remove visible row", change)
	}
	if change := changed("unhide"); change.Removed || change.Summary == nil || change.Summary.Bot.Hidden {
		t.Fatal("unhide did not restore row", change)
	}
	if change := changed("delete"); !change.Removed || change.Summary != nil {
		t.Fatal("deletion must be key-only", change)
	}
	phase("revoked", "")
	// Authority cancellation may terminate the socket before a control frame
	// can be written. EOF is a disconnect, never an applied-cursor receipt.
	remaining, err := io.ReadAll(r.reader)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("revocation did not close the stream", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("revocation published unexpected bytes: %q", remaining)
	}
	r.body.Close()
	phase("reset", "")
	restored := f.stream(t, "", "reader", "")
	_ = restored.next(t)
	if frame := restored.next(t); frame.Type != pa.RosterSnapshotComplete || len(frame.Changes) != len(rosterLiveRows) {
		t.Fatal("reset did not restore baseline", frame)
	}
	restored.body.Close()
	if dir := os.Getenv("CLAWDLINE_ROSTER_LIVE_E2E_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("node", "../../../web/console/src/project-assistants/roster-live.e2e.mjs")
		cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN=fixture-only", "BOTS_MACHINE=machine", "BOTS_ARTIFACTS="+dir)
		log, err := cmd.CombinedOutput()
		if e := os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600); e != nil {
			t.Fatal(e)
		}
		if err != nil {
			t.Fatalf("browser: %v\n%s", err, log)
		}
	}
}
