package http

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

type rosterProjects struct {
	scope   pa.ProjectScope
	revoked atomic.Bool
}

func (p *rosterProjects) Resolve(_ context.Context, q pa.ProjectSelection) (pa.ProjectResolution, error) {
	if p.revoked.Load() || q.ProjectID != p.scope.ProjectID || q.MachineID != p.scope.MachineID {
		return pa.ProjectResolution{}, paHTTPFailure(pa.ScopeDenied, "fixture_project")
	}
	return pa.ProjectResolution{Scope: p.scope, Current: true, Available: true}, nil
}

type rosterHTTPFixture struct {
	store      *store.Store
	views      *store.ViewStore
	service    *app.Service
	projects   *rosterProjects
	handler    http.Handler
	server     *httptest.Server
	revoked    atomic.Bool
	ids        atomic.Int64
	scope      pa.ProjectScope
	transcript bytes.Buffer
}

func newRosterHTTPFixture(t *testing.T, beforeSnapshot, beforePublish func(*rosterHTTPFixture)) *rosterHTTPFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "roster.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if err = (store.Installer{}).Install(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	read, err := sql.Open("sqlite", path+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	views, err := store.NewReadViews(read)
	if err != nil {
		t.Fatal(err)
	}
	f := &rosterHTTPFixture{store: store.New(db), views: views, scope: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}}
	f.projects = &rosterProjects{scope: f.scope}
	source := pa.SourceStatus{MachineID: "machine", State: pa.SourceOnline, Access: pa.Interactive, Readable: true, Writable: true, Fresh: true}
	f.service, err = app.NewService(app.ServiceOptions{Repository: f.store, Memory: f.store, Events: f.store, Deleter: f.store, ReadViews: views, ReadViewByteLimit: cloudops.ReadPlaintextBytesLimit, Runtime: &paHTTPRuntime{}, Projects: f.projects, Clock: time.Now, ID: func() string { return fmt.Sprintf("id-%d", f.ids.Add(1)) }, Authorize: func(ctx context.Context, key pa.BotKey, write bool) (app.Authority, error) {
		reader := "reader"
		if value, ok := ctx.Value(rosterReaderKey{}).(string); ok {
			reader = value
		}
		if f.revoked.Load() && reader == "reader" {
			return app.Authority{}, paHTTPFailure(pa.Revoked, "fixture_grant")
		}
		current := source
		if reader == "read-only" {
			current.Writable = false
			if write {
				return app.Authority{}, paHTTPFailure(pa.ReadOnly, "fixture_write")
			}
		}
		return app.Authority{PrincipalID: reader, ReaderID: reader, AuthorizationRevision: "grant", Feature: pa.FeatureState{Enabled: true}, Source: current}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := newProjectAssistantHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: "machine", Status: func(context.Context) (pa.SourceStatus, error) { return source, nil }, BeforeRosterSnapshot: func() {
		if beforeSnapshot != nil {
			beforeSnapshot(f)
		}
	}, BeforeViewPublish: func() {
		if beforePublish != nil {
			beforePublish(f)
		}
	}})
	f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reader := r.Header.Get("X-Fixture-Reader"); reader != "" {
			r = r.WithContext(context.WithValue(r.Context(), rosterReaderKey{}, reader))
		}
		handler.ServeHTTP(w, r)
	})
	f.server = httptest.NewServer(f.handler)
	t.Cleanup(func() {
		f.server.Close()
		read.Close()
		db.Close()
		if dir := os.Getenv("CLAWDLINE_ROSTER_SSE_EVIDENCE"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+".sse"), f.transcript.Bytes(), 0600); err != nil {
				t.Error(err)
			}
		}
	})
	return f
}

type rosterReaderKey struct{}

func (f *rosterHTTPFixture) seed(t *testing.T, id string) pa.Bot {
	t.Helper()
	now := time.Now().UTC()
	b := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: id}, Project: f.scope, ConversationID: "chat/" + id, Name: "Bot " + id, Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "Synthetic instructions"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	_, err := f.store.Commit(context.Background(), app.Mutation{Command: pa.Command{Key: b.Key, Operation: pa.CreateOperation, IdempotencyKey: "create/" + id, ContentHash: strings.Repeat("a", 64)}, Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: "create/" + id, State: pa.EffectDelivered}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *rosterHTTPFixture) mutate(t *testing.T, key pa.BotKey, m app.Mutation) {
	t.Helper()
	b, err := f.store.Bot(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("change-%d", f.ids.Add(1))
	m.Command = pa.Command{Key: key, Operation: pa.EditOperation, IdempotencyKey: id, ContentHash: strings.Repeat("a", 64)}
	m.ExpectedVersion = b.Version
	m.Receipt = pa.Receipt{ID: id, State: pa.EffectDelivered}
	if m.Bot != nil {
		m.Bot.Version = b.Version + 1
	}
	if _, err = f.store.Commit(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

type rosterSSEReader struct {
	body       io.ReadCloser
	reader     *bufio.Reader
	transcript *bytes.Buffer
}

func (f *rosterHTTPFixture) stream(t *testing.T, cursor, reader, project string) *rosterSSEReader {
	t.Helper()
	query := url.Values{"machine_id": {"machine"}, "cursor": {cursor}, "project_id": {project}}
	req, _ := http.NewRequest("GET", f.server.URL+"/v1/project-assistants/roster-events?"+query.Encode(), nil)
	req.Header.Set("X-Fixture-Reader", reader)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("SSE status %d %s", resp.StatusCode, b)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return &rosterSSEReader{body: resp.Body, reader: bufio.NewReader(resp.Body), transcript: &f.transcript}
}

func (r *rosterSSEReader) next(t *testing.T) pa.RosterFrame {
	t.Helper()
	var data string
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			t.Fatal("SSE read", err)
		}
		r.transcript.WriteString(line)
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		}
		if line == "\n" && data != "" {
			var frame pa.RosterFrame
			if err = json.Unmarshal([]byte(data), &frame); err != nil {
				t.Fatal(err)
			}
			return frame
		}
	}
}

func TestRosterSSESubscribeBeforeSnapshotAndReconnectConverges(t *testing.T) {
	var once atomic.Bool
	f := newRosterHTTPFixture(t, func(f *rosterHTTPFixture) {
		if once.CompareAndSwap(false, true) {
			f.seed(t, "gap")
		}
	}, nil)
	b := f.seed(t, "one")
	r := f.stream(t, "", "reader", "")
	if hello := r.next(t); hello.Type != "hello" || hello.Cursor != "" {
		t.Fatal(hello)
	}
	page := r.next(t)
	if page.Type != "snapshot-complete" || len(page.Changes) != 2 {
		t.Fatal("subscribe/snapshot gap lost", page)
	}
	// Consume the gap's wake-up: its durable state was already in snapshot.
	checkpoint := r.next(t)
	if checkpoint.Type != "changes" || len(checkpoint.Changes) != 0 {
		t.Fatal(checkpoint)
	}
	r.body.Close()
	// Offline transitions: active tool, permission, idle, error, unread/read,
	// hide/unhide and removal. No provider/model exists in this fixture.
	x, _ := f.store.Export(context.Background(), b.Key)
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "binding", Generation: 1}
	conversation := x.Conversation
	conversation.BindingID = "binding"
	conversation.Generation = 1
	conversation.Version++
	conversation.ActiveTurnID = "turn"
	turn := pa.Turn{ID: "turn", Fence: fence, MessageID: "message", State: pa.TurnRunning, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "send"}
	f.mutate(t, b.Key, app.Mutation{Conversation: &conversation, Binding: &pa.ExecutionBinding{Fence: fence, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}, Turn: &turn, Messages: []pa.Message{{ID: "message", Fence: fence, Seq: 1, TurnID: "turn", Kind: pa.UserMessage, Text: "Synthetic request", At: time.Now().UTC()}}})
	turn.State = pa.TurnWaitingPermission
	offer := pa.PermissionOffer{ID: "permission", Fence: fence, TurnID: "turn", NativeRequestID: "native", State: pa.PermissionPending, ExpiresAt: time.Now().Add(time.Hour), Options: []pa.PermissionOption{{ID: "deny", Kind: pa.RejectOnce}}}
	f.mutate(t, b.Key, app.Mutation{Turn: &turn, Permissions: []pa.PermissionOffer{offer}})
	offer.State = pa.PermissionResolved
	offer.SelectedOptionID = "deny"
	turn.State = pa.TurnRunning
	f.mutate(t, b.Key, app.Mutation{Turn: &turn, Permissions: []pa.PermissionOffer{offer}})
	turn.State = pa.TurnSucceeded
	conversation.ActiveTurnID = ""
	conversation.LastMessageSeq = 1
	conversation.Version++
	f.mutate(t, b.Key, app.Mutation{Turn: &turn, Conversation: &conversation, Permissions: []pa.PermissionOffer{offer}, Messages: []pa.Message{{ID: "reply", Fence: fence, Seq: 2, TurnID: "turn", Kind: pa.AssistantMessage, Text: "Synthetic reply", At: time.Now().UTC()}}})
	// A later failed turn supersedes the successful idle turn.
	turn.ID, turn.MessageID, turn.IdempotencyKey, turn.State = "second", "second-message", "second-send", pa.TurnRunning
	conversation.LastMessageSeq = 2
	conversation.ActiveTurnID = turn.ID
	conversation.Version++
	f.mutate(t, b.Key, app.Mutation{Turn: &turn, Conversation: &conversation, Messages: []pa.Message{{ID: turn.MessageID, Fence: fence, Seq: 3, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Second request", At: time.Now().UTC()}}})
	turn.State = pa.TurnFailed
	turn.Failure = &pa.Failure{Code: pa.AdapterNotFound}
	conversation.LastMessageSeq = 3
	conversation.ActiveTurnID = ""
	conversation.Version++
	f.mutate(t, b.Key, app.Mutation{Turn: &turn, Conversation: &conversation})
	read := pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "reader", MessageSeq: 3}
	if _, err := f.store.MarkRead(context.Background(), pa.Command{Key: b.Key, Operation: pa.ReadOperation, IdempotencyKey: "read", ContentHash: strings.Repeat("a", 64)}, read); err != nil {
		t.Fatal(err)
	}
	next, _ := f.store.Bot(context.Background(), b.Key)
	next.Hidden = true
	f.mutate(t, b.Key, app.Mutation{Bot: &next})
	next.Hidden = false
	f.mutate(t, b.Key, app.Mutation{Bot: &next})
	if err := f.store.DeleteBot(context.Background(), pa.BotKey{MachineID: "machine", BotID: "gap"}); err != nil {
		t.Fatal(err)
	}
	r = f.stream(t, checkpoint.Cursor, "reader", "")
	_ = r.next(t)
	replay := r.next(t)
	// The journal keeps each Bot's newest row, so the replay carries one
	// change per Bot that changed: the converged state, not every step.
	if replay.Type != "changes" || len(replay.Changes) != 2 {
		t.Fatalf("replay %+v", replay)
	}
	seen := map[pa.BotKey]pa.RosterChange{}
	for _, change := range replay.Changes {
		seen[change.Key] = change
	}
	if !seen[pa.BotKey{MachineID: "machine", BotID: "gap"}].Removed || seen[b.Key].Summary.Status != "error" || seen[b.Key].Summary.UnreadCount != 0 {
		t.Fatalf("not converged %+v", seen)
	}
	want, err := f.store.ListForReader(context.Background(), pa.ListBots{MachineID: "machine", PageSize: 100}, "reader")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(seen[b.Key].Summary)
	want.Items[0].Bot.BackgroundThreads = want.Items[0].Bot.BackgroundConcurrency()
	want.Items[0].Bot.ReplyInput = want.Items[0].Bot.ReplyInput.Effective()
	// The settings revision fences host writes and is absent from the roster wire contract.
	want.Items[0].Bot.SettingsVersion = 0
	expected, _ := json.Marshal(want.Items[0])
	var actual pa.BotSummary
	_ = json.Unmarshal(got, &actual)
	actual.Source = want.Items[0].Source
	got, _ = json.Marshal(actual)
	if !bytes.Equal(got, expected) {
		t.Fatalf("authority differs\n%s\n%s", got, expected)
	}
	r.body.Close()
	r = f.stream(t, checkpoint.Cursor, "reader", "")
	_ = r.next(t)
	duplicate := r.next(t)
	for _, change := range duplicate.Changes {
		if change.Summary != nil && change.Summary.UnreadCount != 0 {
			t.Fatal("duplicate unread inflated", change)
		}
	}
}

func TestRosterSSEReadOnlyScopeCursorAndPublicationRevocation(t *testing.T) {
	f := newRosterHTTPFixture(t, nil, nil)
	f.seed(t, "one")
	r := f.stream(t, "", "read-only", "project")
	_ = r.next(t)
	snapshot := r.next(t)
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Summary.Source.Writable {
		t.Fatal("read-only viewer", snapshot)
	}
	r.body.Close()
	for _, query := range []url.Values{
		{"machine_id": {"machine"}, "cursor": {snapshot.Cursor}},
		{"machine_id": {"machine"}, "project_id": {"other"}},
		{"machine_id": {"foreign"}},
	} {
		resp, err := http.Get(f.server.URL + "/v1/project-assistants/roster-events?" + query.Encode())
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == 200 {
			resp.Body.Close()
			t.Fatal("scope/cursor admitted", query)
		}
		resp.Body.Close()
	}
	var publications atomic.Int64
	blocked := newRosterHTTPFixture(t, nil, func(f *rosterHTTPFixture) {
		if publications.Add(1) == 2 {
			f.revoked.Store(true)
		}
	})
	blocked.seed(t, "protected")
	r = blocked.stream(t, "", "reader", "")
	if frame := r.next(t); frame.Type != "hello" {
		t.Fatal(frame)
	}
	body, err := io.ReadAll(r.reader)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("protected")) || bytes.Contains(body, []byte("Synthetic instructions")) {
		t.Fatalf("revocation leaked bytes %s", body)
	}
}

// Bots that are working commit in bursts. Commits that land while a frame is
// being published are delivered by the next read; they must not cut the feed,
// because a cut makes every Bot in the list look idle until it reconnects.
func TestRosterSSEBurstIsDeliveredWithoutResync(t *testing.T) {
	var publications atomic.Int64
	f := newRosterHTTPFixture(t, nil, func(f *rosterHTTPFixture) {
		if publications.Add(1) == 2 {
			f.seed(t, "burst-a")
			f.seed(t, "burst-b")
		}
	})
	f.seed(t, "one")
	r := f.stream(t, "", "reader", "")
	_ = r.next(t)
	_ = r.next(t)
	burst := r.next(t)
	if burst.Type != "changes" || len(burst.Changes) != 2 ||
		burst.Changes[0].Key.BotID != "burst-a" || burst.Changes[1].Key.BotID != "burst-b" {
		t.Fatal("burst was not delivered as one change frame", burst)
	}
	if _, err := f.views.ReadRoster(context.Background(), app.RosterQuery{Request: pa.RosterRequest{MachineID: "machine"}, ReaderID: "other", AuthorizationBinding: "other", ByteLimit: cloudops.ReadPlaintextBytesLimit}); err != nil {
		t.Fatal("unrelated read blocked", err)
	}
}

// The production composition must share one writer bus with the HTTP reader,
// including viewer authentication. No test-owned Store is used to write here.
func TestRosterSSEProductionCompositionUsesSamePostCommitBus(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/v1/project-assistants/roster-events?machine_id="+f.key.MachineID, nil)
	req.Header.Set("Authorization", "Bearer "+f.readonly)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("production SSE %d %s", resp.StatusCode, body)
	}
	r := &rosterSSEReader{body: resp.Body, reader: bufio.NewReader(resp.Body), transcript: &bytes.Buffer{}}
	_ = r.next(t)
	_ = r.next(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	change := r.next(t)
	if change.Type != "changes" || len(change.Changes) != 1 || change.Changes[0].Key != f.key {
		t.Fatal("production writer did not wake reader", change)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("feed started provider")
	}
}

// Inject a writer that blocks exactly at the protected frame. It forwards
// actual hello bytes through httptest.NewServer, and honors transport deadlines
// without retaining a SQL transaction or adding a producer/output goroutine.
type rosterBlockedWriter struct {
	http.ResponseWriter
	entered   chan struct{}
	interrupt chan struct{}
	deadline  atomic.Int64
}

func (w *rosterBlockedWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline.Store(deadline.UnixNano())
	if !deadline.After(time.Now()) {
		select {
		case w.interrupt <- struct{}{}:
		default:
		}
	}
	return nil
}
func (w *rosterBlockedWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }
func (w *rosterBlockedWriter) Write(body []byte) (int, error) {
	if bytes.Contains(body, []byte("event: snapshot-complete")) {
		close(w.entered)
		timer := time.NewTimer(time.Until(time.Unix(0, w.deadline.Load())))
		defer timer.Stop()
		select {
		case <-w.interrupt:
		case <-timer.C:
		}
		return 0, context.DeadlineExceeded
	}
	return w.ResponseWriter.Write(body)
}

func TestRosterSSEBlockedWriteRevocationClosesAndReleasesResources(t *testing.T) {
	f := newRosterHTTPFixture(t, nil, nil)
	f.seed(t, "protected")
	entered, done := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		writer := &rosterBlockedWriter{ResponseWriter: w, entered: entered, interrupt: make(chan struct{}, 1)}
		f.server.Config.Handler.ServeHTTP(writer, r)
	}))
	defer server.Close()
	resp, err := http.Get(server.URL + "/v1/project-assistants/roster-events?machine_id=machine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("protected frame did not block")
	}
	f.revoked.Store(true)
	// A separately authorized read completes while the first writer is stuck.
	ctx := context.WithValue(context.Background(), rosterReaderKey{}, "read-only")
	if page, err := f.service.List(ctx, pa.ListBots{MachineID: "machine", PageSize: 1}); err != nil || len(page.Items) != 1 {
		t.Fatalf("unrelated read unavailable %+v %v", page, err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("revocation leaked blocked handler/monitor")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("event: hello")) || bytes.Contains(body, []byte("protected")) {
		t.Fatalf("protected bytes escaped %s", body)
	}
	if resp.Trailer.Get("X-Clawdline-Roster-Error") != string(pa.CursorExpired) {
		t.Fatal("missing typed closure", resp.Trailer)
	}
	// All slots are available again: HTTP cancellation joined the monitor and
	// closed the application's subscription, not just its response body.
	for i := 0; i < pa.MaxRosterSubscribers; i++ {
		_, close, err := f.store.SubscribeRoster("machine")
		if err != nil {
			t.Fatal("subscription leaked", i, err)
		}
		defer close()
	}
}

func TestRosterSSESnapshotPaginationReplaysEarlierConcurrentKey(t *testing.T) {
	var publications atomic.Int64
	f := newRosterHTTPFixture(t, nil, func(f *rosterHTTPFixture) {
		if publications.Add(1) == 2 {
			f.seed(t, "000-before")
		}
	})
	for i := 0; i < pa.MaxPageRows+2; i++ {
		f.seed(t, fmt.Sprintf("bot%03d", i))
	}
	r := f.stream(t, "", "reader", "")
	_ = r.next(t)
	first, last, replay := r.next(t), r.next(t), r.next(t)
	if first.Type != "snapshot" || len(first.Changes) != 100 || last.Type != "snapshot-complete" || len(last.Changes) != 2 || replay.Type != "changes" || len(replay.Changes) != 1 || replay.Changes[0].Key.BotID != "000-before" {
		t.Fatalf("lost concurrent key: %+v %+v %+v", first, last, replay)
	}
}
