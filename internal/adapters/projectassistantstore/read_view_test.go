package projectassistantstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"modernc.org/sqlite"
)

func viewQuery(b pa.Bot) app.ReadViewQuery {
	return app.ReadViewQuery{Request: pa.ReadViewRequest{Key: b.Key, ConversationID: b.ConversationID, Mode: pa.ReadViewSnapshot, PageSize: pa.MaxPageRows}, AuthorizationBinding: "synthetic/account/project", ByteLimit: cloudops.ReadPlaintextBytesLimit, CursorByteLimit: pa.MaxReadViewCursorBytes}
}

func newTestViews(t *testing.T, db *sql.DB) *ViewStore {
	t.Helper()
	s, err := NewReadViews(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readView(t *testing.T, s *ViewStore, q app.ReadViewQuery) app.ReadView {
	t.Helper()
	v, err := s.Read(testContext, q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func viewFixture(t *testing.T, messages, works int, controls bool) (app.Export, pa.Fence, pa.Fence) {
	t.Helper()
	_, s, _ := testDB(t)
	b := seed(t, s, "target")
	e, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	e.Conversation.Generation, e.Conversation.BindingID = 2, "current"
	e.Conversation.LastMessageSeq = int64(messages)
	old := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "old", Generation: 1}
	current := old
	current.BindingID, current.Generation = "current", 2
	e.Bindings = []pa.ExecutionBinding{{Fence: old, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}, {Fence: current, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}}
	e.Events, e.Receipts, e.Messages, e.Turns, e.Permissions, e.WorkLinks = nil, nil, nil, nil, nil, nil
	for i := 1; i <= messages; i++ {
		m := pa.Message{ID: fmt.Sprintf("m%03d", i), Fence: current, Seq: int64(i), Kind: pa.AssistantMessage, Text: fmt.Sprintf("message %03d", i), At: b.CreatedAt}
		if i == 1 || controls && i <= 17+25 {
			m.Kind, m.TurnID = pa.UserMessage, fmt.Sprintf("t%d", i)
			state := pa.TurnSucceeded
			if controls {
				switch {
				case i <= 17:
					state, m.Fence = pa.TurnUnknown, old
				case i == 18:
					state = pa.TurnRunning
				default:
					state = pa.TurnQueued
				}
			}
			e.Turns = append(e.Turns, pa.Turn{ID: m.TurnID, Fence: m.Fence, MessageID: m.ID, State: state, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "send/" + m.TurnID})
		}
		e.Messages = append(e.Messages, m)
		mc := m
		e.Events = append(e.Events, pa.Event{Version: pa.ContractVersion, ID: m.ID, Seq: int64(i), Fence: m.Fence, Kind: pa.MessageCreated, At: b.CreatedAt, Message: &mc})
	}
	workFence := current
	if controls {
		workFence = old
		e.Conversation.ActiveTurnID = "t18"
	}
	for i := 0; i < works; i++ {
		e.WorkLinks = append(e.WorkLinks, pa.WorkLink{ID: fmt.Sprintf("w%03d", i), Fence: workFence, TurnID: "t1", TaskID: fmt.Sprintf("task%d", i), State: pa.WorkUnknown})
	}
	if controls {
		for i := 0; i <= pa.MaxPageRows; i++ {
			options := []pa.PermissionOption{}
			for j := 0; j <= pa.MaxPageRows; j++ {
				options = append(options, pa.PermissionOption{ID: fmt.Sprintf("o%d", j), Label: "reject", Kind: pa.RejectOnce})
			}
			e.Permissions = append(e.Permissions, pa.PermissionOffer{ID: fmt.Sprintf("p%03d", i), Fence: current, TurnID: "t18", NativeRequestID: fmt.Sprintf("native%d", i), State: pa.PermissionPending, Options: options, ExpiresAt: b.CreatedAt.Add(time.Hour)})
		}
		trace := pa.Trace{TurnID: "t18", Category: "agent_message_chunk", Text: "initial trace"}
		e.Events = append(e.Events, pa.Event{Version: pa.ContractVersion, ID: "initial-trace", Seq: int64(messages + 1), Fence: current, Kind: pa.TraceUpdated, At: b.CreatedAt, Trace: &trace})
	}
	return e, old, current
}

func restoreViews(t *testing.T, e app.Export) (*sql.DB, *Store, *ViewStore, string) {
	t.Helper()
	db, s, path := testDB(t)
	if err := s.Restore(testContext, e); err != nil {
		t.Fatal("legal fixture Restore", err)
	}
	return db, s, newTestViews(t, db), path
}

func TestReadViewSQLCapabilitiesAndLegacyCollections(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "empty")
	views := newTestViews(t, db)
	var version string
	var nullRows, emptyRows, window int
	if err := db.QueryRow(`WITH x AS MATERIALIZED(SELECT 1 n) SELECT sqlite_version(),(SELECT count(*) FROM json_each('null')),(SELECT count(*) FROM json_each('[]')),(SELECT row_number() OVER(ORDER BY n) FROM x)`).Scan(&version, &nullRows, &emptyRows, &window); err != nil {
		t.Fatal(err)
	}
	t.Logf("SQLite %s JSON/null=%d empty=%d materialized/window=%d", version, nullRows, emptyRows, window)
	// Collections live in member rows. A JSON null where one was once
	// inline reads as empty; any inline value is a row this store refuses.
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Messages',json('null'))`); err != nil {
		t.Fatal(err)
	}
	got := readView(t, views, viewQuery(b))
	if got.Snapshot == nil || !reflect.DeepEqual(got.Snapshot.Bot, b) || len(got.Items) != 0 || got.Items == nil || got.Snapshot.PendingTurns == nil || got.Snapshot.QueuedMessages == nil || got.Head.EventFrontier != 1 {
		t.Fatalf("null collection: %+v", got)
	}
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Messages',json('[]'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := views.Read(testContext, viewQuery(b)); pa.Code(err) != pa.Invalid {
		t.Fatalf("inline collection was read: %v", err)
	}
	if _, err := s.Bot(testContext, b.Key); pa.Code(err) != pa.Invalid {
		t.Fatalf("inline collection was loaded: %v", err)
	}
}

func TestReadViewExplainsUnknownTurnWithoutCapturedFailureNarrative(t *testing.T) {
	for _, field := range []string{"prompt_timeout", "owned_cleanup_timeout", "provider_usage_limit", "restart", strings.Repeat("private narrative", cloudops.ReadPlaintextBytesLimit)} {
		t.Run(field[:min(len(field), 20)], func(t *testing.T) {
			e, _, _ := viewFixture(t, 1, 0, false)
			e.Turns[0].State = pa.TurnUnknown
			e.Turns[0].Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: field}
			_, _, views, _ := restoreViews(t, e)
			v := readView(t, views, viewQuery(e.Bot))
			last := v.Snapshot.LastTurn
			want := field
			if strings.HasPrefix(field, "private narrative") {
				want = ""
			}
			if last == nil || last.FailureField != want || last.FailureCode != pa.ExecutionEffectUnknown || last.State != pa.TurnUnknown || v.Head.UnknownTurnCount != 1 {
				t.Fatalf("lost safe diagnostic or changed uncertainty: %+v", last)
			}
		})
	}
}

// The read view carries why a follow-up was left queued, so the page need not
// guess it.
func TestReadViewCarriesTheSteerFallbackOfAQueuedTurn(t *testing.T) {
	e, _, _ := viewFixture(t, 1, 0, false)
	e.Turns[0].State = pa.TurnQueued
	e.Turns[0].StartedAt = nil
	e.Turns[0].SteerFallback = pa.SteerFallbackImagesUnsupported
	_, _, views, _ := restoreViews(t, e)
	v := readView(t, views, viewQuery(e.Bot))
	if len(v.Snapshot.PendingTurns) != 1 || v.Snapshot.PendingTurns[0].SteerFallback != pa.SteerFallbackImagesUnsupported {
		t.Fatalf("pending turns lost steer_fallback: %+v", v.Snapshot.PendingTurns)
	}
}

func TestReadViewTreatsFutureSteerFallbackAsPlainQueued(t *testing.T) {
	e, _, _ := viewFixture(t, 1, 0, false)
	e.Turns[0].State = pa.TurnQueued
	db, _, views, _ := restoreViews(t, e)
	if _, err := db.Exec(`UPDATE project_assistant_bot_facts SET data=json_set(CAST(data AS TEXT),'$.steer_fallback','future_reason') WHERE bot=? AND collection='Turns' AND member='t1'`, e.Bot.Key.BotID); err != nil {
		t.Fatal(err)
	}
	v := readView(t, views, viewQuery(e.Bot))
	if len(v.Snapshot.PendingTurns) != 1 || v.Snapshot.PendingTurns[0].State != pa.TurnQueued || v.Snapshot.PendingTurns[0].SteerFallback != "unknown" {
		t.Fatalf("expected plain queued: %+v", v.Snapshot.PendingTurns)
	}
}

func TestReadViewKeepsAnOpenDecisionAfterTheChatRebinds(t *testing.T) {
	e, old, current := viewFixture(t, 2, 0, false)
	offer := pa.PermissionOffer{
		ID: "decision", NativeRequestID: "decision", Kind: pa.DecisionOfferKind, Fence: old,
		Decision: &pa.DecisionRequest{Question: "Which option should I use?"}, AnchorMessageSeq: 2,
		State: pa.PermissionPending, ExpiresAt: e.Bot.CreatedAt.Add(time.Hour),
		Options: []pa.PermissionOption{{ID: pa.DecisionOptionPrefix + "1", Label: "Use the first option", Kind: pa.AllowOnce}},
	}
	e.Permissions = append(e.Permissions, offer)
	_, store, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	q.Request.PageSize = 1
	snapshot := readView(t, views, q)
	if len(snapshot.Items) != 1 || snapshot.Items[0].Message == nil || !snapshot.HasMore || snapshot.Head.PendingPermissionCount != 1 {
		t.Fatalf("missing history or decision count: %+v", snapshot)
	}
	historyQuery := q
	historyQuery.Request.Mode, historyQuery.Request.SourceEpoch, historyQuery.Request.Cursor = pa.ReadViewHistory, snapshot.SourceEpoch, snapshot.Items[0].AfterCursor
	history := readView(t, views, historyQuery)
	if len(history.Items) != 1 || history.Items[0].Message == nil || history.Items[0].Message.Seq != 1 {
		t.Fatalf("missing earlier message: %+v", history)
	}
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewPermissions, snapshot.SourceEpoch
	collection := readView(t, views, q)
	if len(collection.Items) != 1 || collection.Items[0].PermissionSummary == nil {
		t.Fatalf("missing decision summary: %+v", collection)
	}
	summary := collection.Items[0].PermissionSummary
	if summary.ID != offer.ID || summary.TurnID != "" || summary.ToolKind != pa.DecisionOfferKind || summary.Title != offer.Decision.Question || summary.Fence != current {
		t.Fatalf("wrong decision summary: %+v", summary)
	}
	q.Request.Mode, q.Request.Cursor, q.Request.PageSize = pa.ReadViewDetail, summary.DetailCursor, 1
	detail := readView(t, views, q)
	want := offer
	want.Fence = current
	if len(detail.Items) != 1 || !reflect.DeepEqual(detail.Items[0].Permission, &want) {
		t.Fatalf("decision detail lost its answer options: %+v", detail)
	}
	stored, err := store.Permission(testContext, e.Bot.Key, offer.ID)
	if err != nil || !reflect.DeepEqual(stored, offer) {
		t.Fatalf("reading changed the original offer: %+v, %v", stored, err)
	}
}

func TestReadViewKeepsAnsweredMemoryCardsAndCountsOnlyPendingOffers(t *testing.T) {
	e, old, current := viewFixture(t, 2, 0, false)
	memory := pa.PermissionOffer{
		ID: "a-memory", NativeRequestID: "a-memory", Kind: pa.DecisionOfferKind, Fence: old,
		Decision: &pa.DecisionRequest{Question: pa.MemoryProposalQuestion, Memory: "The meeting is on Wednesday."},
		State:    pa.PermissionResolved, SelectedOptionID: pa.MemorySaveOptionID, AnchorMessageSeq: 2,
		Options: []pa.PermissionOption{
			{ID: pa.MemorySaveOptionID, Label: "Save", Kind: pa.AllowOnce},
			{ID: pa.MemorySkipOptionID, Label: "Don't save", Kind: pa.RejectOnce},
		},
		ExpiresAt: e.Bot.CreatedAt.Add(time.Hour),
	}
	skipped := memory
	skipped.ID, skipped.NativeRequestID, skipped.SelectedOptionID = "b-memory", "b-memory", pa.MemorySkipOptionID
	ordinary := memory
	ordinary.ID, ordinary.NativeRequestID = "c-ordinary", "c-ordinary"
	ordinary.Decision = &pa.DecisionRequest{Question: "Does this work?", AnswerText: "Yes"}
	ordinary.Options = []pa.PermissionOption{{ID: "reply-1", Label: "Yes", Kind: pa.AllowOnce}}
	ordinary.SelectedOptionID = "reply-1"
	pending := ordinary
	pending.ID, pending.NativeRequestID, pending.State, pending.SelectedOptionID = "d-pending", "d-pending", pa.PermissionPending, ""
	pending.Decision = &pa.DecisionRequest{Question: "What happens next?"}
	e.Permissions = []pa.PermissionOffer{memory, skipped, ordinary, pending}
	_, _, views, _ := restoreViews(t, e)

	q := viewQuery(e.Bot)
	snapshot := readView(t, views, q)
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewPermissions, snapshot.SourceEpoch
	page := readView(t, views, q)
	if page.Head.PendingPermissionCount != 1 {
		t.Fatalf("pending count = %d, want 1", page.Head.PendingPermissionCount)
	}
	var ids []string
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	if !reflect.DeepEqual(ids, []string{memory.ID, skipped.ID, pending.ID}) {
		t.Fatalf("permission summaries = %v, want saved and skipped memory cards alongside the pending offer", ids)
	}
	for i, want := range []pa.PermissionOffer{memory, skipped} {
		summary := page.Items[i].PermissionSummary
		if summary == nil || summary.State != pa.PermissionResolved || summary.Fence != current || summary.Title != want.Decision.Question {
			t.Fatalf("answered memory summary = %+v", summary)
		}
		detailQuery := q
		detailQuery.Request.Mode, detailQuery.Request.Cursor, detailQuery.Request.PageSize = pa.ReadViewDetail, summary.DetailCursor, 1
		detail := readView(t, views, detailQuery)
		want.Fence = current
		if len(detail.Items) != 1 || !reflect.DeepEqual(detail.Items[0].Permission, &want) {
			t.Fatalf("answer detail = %+v, want %+v", detail.Items, want)
		}
	}
}

func TestReadViewTimelineAndAllControlsStayBounded(t *testing.T) {
	e, old, _ := viewFixture(t, 600, 101, true)
	e.Turns[18].State, e.Turns[19].State = pa.TurnAccepted, pa.TurnWaitingPermission
	// An admission queue from an older fence is still part of mandatory
	// pending state. Only permission/trace authority is current-fence filtered.
	e.Turns[32].Fence, e.Messages[32].Fence, e.Events[32].Fence, e.Events[32].Message.Fence = old, old, old, old
	db, store, views, _ := restoreViews(t, e)
	seed(t, store, "other") // Another Bot owns the machine frontier, 602.
	q := viewQuery(e.Bot)
	v := readView(t, views, q)
	if v.Head.EventFrontier != 602 || v.Head.UnknownTurnCount != 17 || v.Head.PendingPermissionCount != 101 || v.Head.TraceCategoryCount != 1 || len(v.Snapshot.PendingTurns) != 25 || len(v.Snapshot.QueuedMessages) != 25 || v.Snapshot.LastTurn != nil {
		t.Fatalf("mandatory controls/frontier: %+v %+v", v.Head, v.Snapshot)
	}
	seen := map[string]bool{}
	pages := 0
	for {
		pages++
		if len(v.Items) > pa.MaxPageRows {
			t.Fatal("page overflow")
		}
		for _, item := range v.Items {
			identity := string(item.Kind) + "/" + item.ID
			if seen[identity] || item.ObservedEventSeq != 602 || item.Failure != nil || item.CursorFailure != nil {
				t.Fatalf("duplicate/bad item %+v", item)
			}
			seen[identity] = true
		}
		if !v.HasMore {
			break
		}
		q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, v.SourceEpoch, v.Items[len(v.Items)-1].AfterCursor
		v = readView(t, views, q)
	}
	if len(seen) != 701 || pages != 8 || !seen["message/m001"] || !seen["message/m600"] || !seen["work/w000"] || !seen["work/w100"] {
		t.Fatalf("all-once traversal: %d rows, %d pages", len(seen), pages)
	}
	q.Request.Mode, q.Request.Cursor = pa.ReadViewPermissions, ""
	v = readView(t, views, q)
	if len(v.Items) != 100 || !v.HasMore || v.Items[0].PermissionSummary == nil || v.Items[0].Permission != nil {
		t.Fatalf("permission summaries: %+v", v)
	}
	detail := q
	detail.Request.Mode, detail.Request.PageSize, detail.Request.Cursor = pa.ReadViewDetail, 1, v.Items[0].PermissionSummary.DetailCursor
	full := readView(t, views, detail)
	if len(full.Items) != 1 || len(full.Items[0].Permission.Options) != 101 || full.HasMore {
		t.Fatalf("full offer: %+v", full)
	}
	q.Request.Cursor = v.Items[len(v.Items)-1].AfterCursor
	v = readView(t, views, q)
	if len(v.Items) != 1 || v.HasMore || v.Items[0].ID != "p100" {
		t.Fatalf("offer final page: %+v", v)
	}
	q.Request.Mode, q.Request.Cursor = pa.ReadViewTraces, ""
	v = readView(t, views, q)
	if len(v.Items) != 1 || v.Items[0].TraceSummary.Category != "agent_message_chunk" || v.HasMore {
		t.Fatalf("trace summaries: %+v", v)
	}
	detail.Request.Cursor = v.Items[0].TraceSummary.DetailCursor
	full = readView(t, views, detail)
	if full.Items[0].Trace.Text != "initial trace" {
		t.Fatalf("trace detail: %+v", full)
	}
	// Read-only operations neither change bytes nor append receipts/events.
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM project_assistant_receipts WHERE bot='target'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read wrote receipts: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM project_assistant_events`).Scan(&count); err != nil || count != 602 {
		t.Fatalf("read wrote events: %d %v", count, err)
	}
}

func TestReadViewRejectsMalformedAndMissingMetadata(t *testing.T) {
	for _, mutation := range []string{
		`'not json'`, `json_remove(CAST(data AS TEXT),'$.Bot')`,
		`json_set(CAST(data AS TEXT),'$.Bot.key.bot_id','foreign')`,
		`json_set(CAST(data AS TEXT),'$.Memory.project.canonical_id','foreign')`,
		`json_set(CAST(data AS TEXT),'$.Conversation.key.machine_id','foreign')`,
		`json_set(CAST(data AS TEXT),'$.Conversation.generation','bad')`,
		`json_set(CAST(data AS TEXT),'$.Messages',json('{}'))`,
		`json_set(CAST(data AS TEXT),'$.Turns',json('[]'))`,
	} {
		t.Run(mutation, func(t *testing.T) {
			db, s, _ := testDB(t)
			b := seed(t, s, "target")
			if _, err := db.Exec(`UPDATE project_assistant_bots SET data=` + mutation); err != nil {
				t.Fatal(err)
			}
			_, err := newTestViews(t, db).Read(testContext, viewQuery(b))
			if pa.Code(err) != pa.Invalid && pa.Code(err) != pa.ScopeDenied {
				t.Fatalf("accepted malformed shape or untyped failure: %v", err)
			}
		})
	}
	db, s, _ := testDB(t)
	b := seed(t, s, "target")
	views := newTestViews(t, db)
	q := viewQuery(b)
	q.Request.Key.BotID = "missing"
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.NotFound)
	q = viewQuery(b)
	q.Request.ConversationID = "wrong"
	_, err = views.Read(testContext, q)
	requireCode(t, err, pa.NotFound)
}

// Broken shapes and relations are refused when they are written, so a read
// does not walk the whole history to look for them again.
func TestRestoreRefusesBrokenFactRelations(t *testing.T) {
	for name, corrupt := range map[string]func(*app.Export){
		"duplicate message id":    func(e *app.Export) { e.Messages[0].ID = "m002" },
		"message position":        func(e *app.Export) { e.Messages[0].Seq = 2 },
		"foreign message fence":   func(e *app.Export) { e.Messages[0].Fence.Project.CanonicalID = "foreign" },
		"turn without message":    func(e *app.Export) { e.Turns[0].MessageID = "missing-message" },
		"work without turn":       func(e *app.Export) { e.WorkLinks[0].TurnID = "absent" },
		"permission without turn": func(e *app.Export) { e.Permissions[0].TurnID = "absent" },
		"foreign binding": func(e *app.Export) {
			for i := range e.Bindings {
				if e.Bindings[i].Fence.BindingID == "current" {
					e.Bindings[i].Fence.Key.BotID = "foreign"
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e, _, _ := viewFixture(t, 40, 1, true)
			corrupt(&e)
			_, s, _ := testDB(t)
			if err := s.Restore(testContext, e); pa.Code(err) != pa.Invalid && pa.Code(err) != pa.ScopeDenied {
				t.Fatalf("corrupt fact was stored: %v", err)
			}
		})
	}
}

// A page checks the rows it returns. A corrupt row outside it, which no
// writer of this store can produce, does not stop the newest page.
func TestReadViewChecksReturnedRowsOnly(t *testing.T) {
	e, _, _ := viewFixture(t, 40, 1, true)
	db, _, views, _ := restoreViews(t, e)
	if _, err := db.Exec(`UPDATE project_assistant_bot_facts SET data=json_set(CAST(data AS TEXT),'$.fence.project.canonical_id','foreign') WHERE collection='Messages' AND ord=1`); err != nil {
		t.Fatal(err)
	}
	q := viewQuery(e.Bot)
	q.Request.PageSize = 1
	v := readView(t, views, q)
	if len(v.Items) != 1 || v.Items[0].Message == nil || v.Items[0].Message.ID != "m040" {
		t.Fatalf("newest page = %+v", v.Items)
	}
	q.Request.PageSize = pa.MaxPageRows
	if _, err := views.Read(testContext, q); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("returned corrupt row was accepted: %v", err)
	}
}

func TestReadViewBudgetsRefuseWholeObjectsWithoutTruncation(t *testing.T) {
	e, _, _ := viewFixture(t, 3, 0, false)
	db, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	initial := readView(t, views, q)
	// Lower the trusted byte budget: a raw row larger than it must never be
	// copied as a partial message. The preceding legal prefix remains usable.
	if _, err := db.Exec(`UPDATE project_assistant_bot_facts SET data=json_set(CAST(data AS TEXT),'$.text',?) WHERE collection='Messages' AND ord=2`, strings.Repeat("x", 4096)); err != nil {
		t.Fatal(err)
	}
	q.ByteLimit = 2048
	v := readView(t, views, q)
	if len(v.Items) != 2 || v.Items[0].ID != "m003" || v.Items[1].Failure == nil || !v.HasMore || v.Items[0].AfterCursor == "" {
		t.Fatalf("oversized row lost prefix/continuation: %+v", v)
	}
	q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, initial.SourceEpoch, v.Items[0].AfterCursor
	v = readView(t, views, q)
	if len(v.Items) != 1 || v.Items[0].Failure == nil || !v.HasMore {
		t.Fatalf("oversized row was skipped: %+v", v)
	}
	q = viewQuery(e.Bot)
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Bot.instructions.role_snapshot_ref',?)`, strings.Repeat("r", q.ByteLimit+1)); err != nil {
		t.Fatal(err)
	}
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.CapacityFull)
}

func TestReadViewCandidateBytesAreOneSharedPrefixBudget(t *testing.T) {
	e, _, _ := viewFixture(t, 3, 6, false)
	for i := range e.WorkLinks {
		e.WorkLinks[i].BoardItemID = strings.Repeat("<界", 400)
	}
	_, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	q.ByteLimit = 8192
	seen := map[string]bool{}
	for {
		v := readView(t, views, q)
		payloadBytes, complete, last := 0, 0, ""
		for _, item := range v.Items {
			if item.Failure != nil {
				if item.Message != nil || item.Work != nil || item.ID != "" {
					t.Fatal("oversized candidate copied payload/ID into Go")
				}
				break
			}
			var raw []byte
			if item.Message != nil {
				raw, _ = json.Marshal(item.Message)
			} else {
				raw, _ = json.Marshal(item.Work)
			}
			payloadBytes += len(raw)
			complete++
			if seen[string(item.Kind)+"/"+item.ID] {
				t.Fatal("byte boundary repeated a row")
			}
			seen[string(item.Kind)+"/"+item.ID] = true
			last = item.AfterCursor
		}
		if payloadBytes > q.ByteLimit || complete == 0 {
			t.Fatalf("candidate prefix unbounded or unable to continue: %d bytes %d rows", payloadBytes, complete)
		}
		if !v.HasMore {
			break
		}
		if v.Items[len(v.Items)-1].Failure == nil || last == "" {
			t.Fatal("SQL byte prefix needs a failure sentinel and legal continuation")
		}
		q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, v.SourceEpoch, last
	}
	if len(seen) != 9 {
		t.Fatalf("byte prefix skipped rows: %d", len(seen))
	}
}

func TestReadViewLongQueueKeepsCountAndTranscriptWhenItCannotAllFit(t *testing.T) {
	e, _, _ := viewFixture(t, 600, 0, true)
	// Add a large, valid message behind the running turn. The transport can
	// keep showing the transcript and exact queue count when only a prefix fits.
	e.Messages[18].Text = strings.Repeat("x", 12<<10)
	_, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	complete := readView(t, views, q)
	if complete.Head.QueuedTurnCount != 24 || len(complete.Snapshot.QueuedMessages) != 25 {
		t.Fatal("complete view rejected a queue over the former turn count")
	}
	q.ByteLimit = 20 << 10
	bounded := readView(t, views, q)
	if bounded.Head.QueuedTurnCount != 24 || len(bounded.Snapshot.PendingTurns) != 1 || bounded.Snapshot.PendingTurns[0].State != pa.TurnRunning || len(bounded.Items) == 0 {
		t.Fatalf("bounded view hid queue controls or recent chat: %+v", bounded)
	}
	// The turns that did not fit are counted on the snapshot itself.
	if complete.Snapshot.PendingOmitted != 0 || bounded.Snapshot.PendingOmitted != len(complete.Snapshot.PendingTurns)-1 {
		t.Fatalf("omitted pending turns: complete %d, bounded %d of %d", complete.Snapshot.PendingOmitted, bounded.Snapshot.PendingOmitted, len(complete.Snapshot.PendingTurns))
	}
}

func TestReadViewEventsUseLastIncludedSequence(t *testing.T) {
	e, _, _ := viewFixture(t, 600, 0, false)
	_, store, views, _ := restoreViews(t, e)
	seed(t, store, "other")
	q := viewQuery(e.Bot)
	v := readView(t, views, q)
	q.Request.Mode, q.Request.SourceEpoch, q.Request.PageSize = pa.ReadViewEvents, v.SourceEpoch, 2
	q.Request.AfterSeq = 598
	v = readView(t, views, q)
	if len(v.Items) != 2 || v.NextSeq != 600 || v.HasMore || v.Head.EventFrontier != 601 || v.Items[0].ObservedEventSeq != 599 {
		t.Fatalf("event cursor jumped to head: %+v", v)
	}
	q.Request.AfterSeq = 601
	v = readView(t, views, q)
	if len(v.Items) != 0 || v.NextSeq != 601 || v.HasMore {
		t.Fatalf("empty event cursor: %+v", v)
	}
	q.Request.AfterSeq = 602
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.CursorExpired)
}

func TestReadViewEventsRejectForeignNestedPayload(t *testing.T) {
	e, _, _ := viewFixture(t, 3, 0, false)
	db, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	v := readView(t, views, q)
	if _, err := db.Exec(`UPDATE project_assistant_events SET data=json_set(CAST(data AS TEXT),'$.message.fence.project.canonical_id','foreign') WHERE seq=3`); err != nil {
		t.Fatal(err)
	}
	q.Request.Mode, q.Request.SourceEpoch, q.Request.AfterSeq = pa.ReadViewEvents, v.SourceEpoch, 2
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.ScopeDenied)
}

func TestReadViewEventsValidateEveryPayloadScope(t *testing.T) {
	for _, name := range []string{"message", "turn", "permission", "work", "memory", "memory revision", "bot", "summary reader", "summary source", "summary capability", "read", "source", "source capability", "trace turn", "event binding"} {
		t.Run(name, func(t *testing.T) {
			e, _, current := viewFixture(t, 3, 0, false)
			db, _, views, _ := restoreViews(t, e)
			q := viewQuery(e.Bot)
			initial := readView(t, views, q)
			event := e.Events[0]
			event.Message = nil
			foreign := current
			foreign.Key.BotID = "foreign-bot"
			summary := pa.BotSummary{Bot: e.Bot}
			source := pa.SourceStatus{MachineID: e.Bot.Key.MachineID}
			switch name {
			case "message":
				message := *e.Events[0].Message
				message.Fence = foreign
				event.Message = &message
			case "turn":
				event.Kind, event.Turn = pa.TurnUpdated, &pa.Turn{ID: "t1", Fence: foreign}
			case "permission":
				event.Kind, event.Permission = pa.PermissionOffered, &pa.PermissionOffer{ID: "p", Fence: foreign}
			case "work":
				event.Kind, event.Work = pa.WorkUpdated, &pa.WorkLink{ID: "w", Fence: foreign}
			case "memory", "memory revision":
				memory := e.Memory
				if name == "memory" {
					memory.Project.CanonicalID = "foreign"
				} else {
					memory.Revision++
				}
				event.Kind, event.Memory = pa.MemoryChanged, &memory
			case "bot", "summary reader", "summary source", "summary capability":
				switch name {
				case "bot":
					summary.Bot.Key.BotID = "foreign-bot"
				case "summary reader":
					summary.ReadCursor.Key = foreign.Key
				case "summary source":
					summary.Source.MachineID = "foreign-machine"
				case "summary capability":
					summary.Source.Capabilities = []pa.Capabilities{{Key: foreign.Key, Project: current.Project}}
				}
				event.Kind, event.Bot = pa.BotUpdated, &summary
			case "read":
				event.Kind, event.Read = pa.ReadAdvanced, &pa.ReadCursor{Key: current.Key, ConversationID: "foreign-chat"}
			case "source", "source capability":
				if name == "source" {
					source.MachineID = "foreign-machine"
				} else {
					source.Capabilities = []pa.Capabilities{{Key: foreign.Key, Project: current.Project}}
				}
				event.Kind, event.Source = pa.SourceUpdated, &source
			case "trace turn":
				event.Kind, event.Trace = pa.TraceUpdated, &pa.Trace{TurnID: "absent", Category: "agent_message_chunk", Text: "trace"}
			case "event binding":
				event.Kind, event.Fence.BindingID = pa.FeatureDisabled, "absent"
			}
			raw, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE project_assistant_events SET data=? WHERE seq=1`, raw); err != nil {
				t.Fatal(err)
			}
			q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewEvents, initial.SourceEpoch
			_, err = views.Read(testContext, q)
			requireCode(t, err, pa.ScopeDenied)
		})
	}
}

func TestEventFactChecksKeepBindingGenerationAndTurnScope(t *testing.T) {
	export, old, current := viewFixture(t, 1, 0, false)
	db, _, views, _ := restoreViews(t, export)
	head := readView(t, views, viewQuery(export.Bot)).Head
	tx, err := db.BeginTx(testContext, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var checks viewEventFactChecks
	event := pa.Event{Fence: current, Trace: &pa.Trace{TurnID: "t1"}}
	for range 2 {
		if err = checks.verify(testContext, tx, event, head); err != nil {
			t.Fatal(err)
		}
	}
	event.Fence.Generation = old.Generation
	if err = checks.verify(testContext, tx, event, head); !reflect.DeepEqual(err, failure(pa.ScopeDenied, "read_view_event_binding")) {
		t.Fatalf("a previously checked binding ID was accepted with the wrong generation: %v", err)
	}
	event.Fence = old
	if err = checks.verify(testContext, tx, event, head); !reflect.DeepEqual(err, failure(pa.ScopeDenied, "read_view_event_trace")) {
		t.Fatalf("a previously checked turn ID was accepted with a different binding: %v", err)
	}
}

func TestReadViewDoesNotMutateOrCloseBorrowedPool(t *testing.T) {
	db, store, _ := testDB(t)
	b := seed(t, store, "target")
	var before []byte
	if err := db.QueryRow(`SELECT data FROM project_assistant_bots`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	readView(t, newTestViews(t, db), viewQuery(b))
	var after []byte
	if err := db.QueryRow(`SELECT data FROM project_assistant_bots`).Scan(&after); err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("aggregate mutated: %v", err)
	}
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=data`); err != nil {
		t.Fatal("read retained transaction or closed/changed caller pool", err)
	}
	var a aggregate
	if err := json.Unmarshal(after, &a); err != nil || a.Bot.Key != b.Key {
		t.Fatal(err)
	}
}

type viewBarrierDriver struct {
	ready chan struct{}
	done  chan error
	once  sync.Once
}

type viewBarrierConn struct {
	driver.Conn
	barrier *viewBarrierDriver
}

type viewBarrierRows struct {
	driver.Rows
	barrier *viewBarrierDriver
}

var viewBarrierID atomic.Int64

func (d *viewBarrierDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &viewBarrierConn{Conn: c, barrier: d}, nil
}

func (c *viewBarrierConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}

func (c *viewBarrierConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err == nil && strings.Contains(query, "WITH raw AS MATERIALIZED") {
		return &viewBarrierRows{Rows: rows, barrier: c.barrier}, nil
	}
	return rows, err
}

func (r *viewBarrierRows) Next(values []driver.Value) error {
	err := r.Rows.Next(values)
	if err == nil {
		r.barrier.once.Do(func() {
			close(r.barrier.ready)
			select {
			case err = <-r.barrier.done:
			case <-time.After(time.Minute):
				err = fmt.Errorf("WAL writer did not reach barrier")
			}
		})
	}
	return err
}

func TestReadViewWALSnapshotAndLateOldGenerationEvents(t *testing.T) {
	e, old, _ := viewFixture(t, 600, 101, true)
	db, writer, _, path := restoreViews(t, e)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	seed(t, writer, "other") // seq602 is not a target-Bot event.
	barrier := &viewBarrierDriver{ready: make(chan struct{}), done: make(chan error, 1)}
	name := fmt.Sprintf("read-view-barrier-%d", viewBarrierID.Add(1))
	sql.Register(name, barrier)
	reader, err := sql.Open(name, path)
	if err != nil {
		t.Fatal(err)
	}
	reader.SetMaxOpenConns(1)
	t.Cleanup(func() { reader.Close() })
	if _, err = reader.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	views := newTestViews(t, reader)
	updated := e.WorkLinks[0]
	updated.State, updated.ResultMessageID = pa.WorkCompleted, "late-result"
	late := pa.Message{ID: "late-result", Fence: old, Seq: 601, TurnID: "t1", Kind: pa.WorkMessage, Text: "late old-generation result"}
	writerFinished := make(chan struct{})
	go func() {
		defer close(writerFinished)
		select {
		case <-barrier.ready:
		case <-time.After(time.Minute):
			barrier.done <- fmt.Errorf("reader did not establish snapshot")
			return
		}
		_, writeErr := writer.Commit(testContext, app.Mutation{Command: command(e.Bot.Key, pa.DispatchOperation, "late"), ExpectedVersion: e.Bot.Version,
			WorkLinks: []pa.WorkLink{updated}, Messages: []pa.Message{late}, Receipt: pa.Receipt{ID: "late", State: pa.EffectDelivered},
			Events: []pa.Event{{Version: pa.ContractVersion, ID: "late-work", Fence: old, Kind: pa.WorkUpdated, Work: &updated}, {Version: pa.ContractVersion, ID: "late-message", Fence: old, Kind: pa.MessageCreated, Message: &late}}})
		barrier.done <- writeErr
	}()
	q := viewQuery(e.Bot)
	v := readView(t, views, q)
	<-writerFinished
	if v.Head.EventFrontier != 602 || v.Head.BotVersion != 1 || v.Head.Conversation.LastMessageSeq != 600 || v.Items[0].Message.Seq != 600 {
		t.Fatalf("fractured first-select snapshot: %+v", v)
	}
	q.Request.Mode, q.Request.SourceEpoch, q.Request.AfterSeq = pa.ReadViewEvents, v.SourceEpoch, 602
	v = readView(t, views, q)
	if len(v.Items) != 2 || v.NextSeq != 604 || v.Items[0].Event.Seq != 603 || v.Items[0].Event.Work.State != pa.WorkCompleted || v.Items[1].Event.Message.ID != late.ID {
		t.Fatalf("lost late old-generation facts: %+v", v)
	}
	// The read has returned and closed its transaction/rows. Another writer
	// can advance the database; no borrowed pool close or write pragma leaked.
	if _, err = db.Exec(`UPDATE project_assistant_bots SET data=data`); err != nil {
		t.Fatal(err)
	}
}

func TestReadViewGiantTraceAndCapturedInstructionsStayOutOfMandatoryState(t *testing.T) {
	e, _, _ := viewFixture(t, 40, 0, true)
	// These legal persisted payloads are larger than the entire Cloud budget.
	// Compact pending summaries and trace summaries must remain readable.
	e.Turns[17].Instructions.Text = strings.Repeat("i", cloudops.ReadPlaintextBytesLimit+1)
	e.Events[len(e.Events)-1].Trace.Text = strings.Repeat("t", cloudops.ReadPlaintextBytesLimit+1)
	_, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	v := readView(t, views, q)
	if len(v.Snapshot.PendingTurns) != 23 || v.Head.TraceCategoryCount != 1 {
		t.Fatal("giant optional objects broke mandatory summaries")
	}
	q.Request.Mode, q.Request.SourceEpoch = pa.ReadViewTraces, v.SourceEpoch
	v = readView(t, views, q)
	if len(v.Items) != 1 || v.Items[0].TraceSummary == nil || v.Items[0].Failure != nil {
		t.Fatalf("giant trace summary: %+v", v)
	}
	q.Request.Mode, q.Request.PageSize, q.Request.Cursor = pa.ReadViewDetail, 1, v.Items[0].TraceSummary.DetailCursor
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.CapacityFull)
}

// This is the legal long-history shape that exceeded the existing source read
// deadline. Message positions remain contiguous; source events are independent.
func viewDeadlineFixture(t *testing.T) (*sql.DB, *Store, *ViewStore, pa.Bot, []pa.Message) {
	t.Helper()
	started := time.Now()
	_, template, _ := testDB(t)
	b := seed(t, template, strings.Repeat("b", 36))
	b.Key.MachineID = strings.Repeat("m", 36)
	b.Project = pa.ProjectScope{MachineID: b.Key.MachineID, ProjectID: strings.Repeat("p", 36), CanonicalID: strings.Repeat("c", 72), Kind: pa.RepositoryProject}
	b.ConversationID = strings.Repeat("v", 36)
	db, store, _ := testDB(t)
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}
	summary := pa.BotSummary{Bot: b}
	if _, err := store.Commit(testContext, app.Mutation{Command: command(b.Key, pa.CreateOperation, "create"), Bot: &b, Conversation: &c,
		Receipt: pa.Receipt{ID: "create", State: pa.EffectDelivered}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "create", Fence: fence, Kind: pa.BotCreated, Bot: &summary}}}); err != nil {
		t.Fatal(err)
	}
	messages := make([]pa.Message, 9001)
	for i := range messages {
		messages[i] = pa.Message{ID: fmt.Sprintf("message-%d", i+1), Fence: fence, Seq: int64(i + 1), Kind: pa.AssistantMessage, Text: "synthetic visible fact", At: b.CreatedAt, ForPerson: pa.ForPersonFlag(false)}
	}
	events := make([]pa.Event, 600)
	for i := range events {
		events[i] = pa.Event{Version: pa.ContractVersion, ID: fmt.Sprintf("source-%d", i+1), Fence: fence, Kind: pa.SourceUpdated, At: b.CreatedAt,
			Source: &pa.SourceStatus{MachineID: b.Key.MachineID, State: pa.SourceOnline, Fresh: true, Readable: true, Writable: true, Access: pa.Interactive}}
	}
	if _, err := store.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "history"), ExpectedVersion: 1, Messages: messages, Events: events,
		Receipt: pa.Receipt{ID: "history", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	other := b
	other.Key.BotID, other.ConversationID = "other-bot", "other-conversation"
	otherConversation := pa.Conversation{Key: other.Key, ID: other.ConversationID, Version: 1, MemoryRevision: 1}
	otherSummary := pa.BotSummary{Bot: other}
	if _, err := store.Commit(testContext, app.Mutation{Command: command(other.Key, pa.CreateOperation, "other"), Bot: &other, Conversation: &otherConversation,
		Receipt: pa.Receipt{ID: "other", State: pa.EffectDelivered}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "other", Fence: pa.Fence{Key: other.Key, Project: other.Project, ConversationID: other.ConversationID}, Kind: pa.BotCreated, Bot: &otherSummary}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("legal long-history fixture rows=%d messages_json_bytes=%d events=600 machine_frontier=602 build_write=%s", len(messages), len(raw), time.Since(started))
	return db, store, newTestViews(t, db), b, messages
}

func TestReadViewLongHistoryWithinSourceDeadline(t *testing.T) {
	db, _, views, b, messages := viewDeadlineFixture(t)
	q := viewQuery(b)
	read := func() app.ReadView {
		t.Helper()
		ctx, cancel := context.WithTimeout(testContext, time.Minute)
		defer cancel()
		started := time.Now()
		v, err := views.Read(ctx, q)
		t.Logf("read mode=%s after_seq=%d rows=%d elapsed=%s error=%v", q.Request.Mode, q.Request.AfterSeq, len(v.Items), time.Since(started), err)
		if err != nil {
			t.Fatal(err)
		}
		if db.Stats().InUse != 0 || v.Head.EventFrontier != 602 {
			t.Fatal("read retained its transaction or confused the machine frontier")
		}
		return v
	}
	v := read()
	if v.Snapshot == nil || len(v.Items) != 100 || !v.HasMore {
		t.Fatal("long-history snapshot lost mandatory state or page")
	}
	check := func(v app.ReadView, first int) {
		t.Helper()
		for i, item := range v.Items {
			if item.Kind != app.ReadItemMessage || item.Failure != nil || item.CursorFailure != nil || item.AfterCursor == "" || item.ObservedEventSeq != 602 || item.Message == nil || !reflect.DeepEqual(*item.Message, messages[first-i-1]) {
				t.Fatalf("page changed complete stored message at %d: %+v", first-i, item)
			}
		}
	}
	check(v, 9001)
	q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, v.SourceEpoch, v.Items[len(v.Items)-1].AfterCursor
	v = read()
	if len(v.Items) != 100 || !v.HasMore || v.NextSeq != 0 {
		t.Fatal("long-history continuation lost its position")
	}
	check(v, 8901)
	q.Request.Mode, q.Request.Cursor, q.Request.AfterSeq = pa.ReadViewEvents, "", 1
	v = read()
	if len(v.Items) != 100 || v.NextSeq != 101 || !v.HasMore {
		t.Fatal("long-history event page lost its complete prefix")
	}
	for i, item := range v.Items {
		expected := pa.Event{Version: pa.ContractVersion, ID: fmt.Sprintf("source-%d", i+1), Seq: int64(i + 2),
			Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}, Kind: pa.SourceUpdated, At: b.CreatedAt,
			Source: &pa.SourceStatus{MachineID: b.Key.MachineID, State: pa.SourceOnline, Fresh: true, Readable: true, Writable: true, Access: pa.Interactive}}
		if item.Kind != app.ReadItemEvent || item.ID != expected.ID || item.Failure != nil || item.ObservedEventSeq != expected.Seq || item.Event == nil || !reflect.DeepEqual(*item.Event, expected) {
			t.Fatalf("event page changed complete stored event at %d: %+v", expected.Seq, item)
		}
	}
	q.Request.Mode, q.Request.Cursor, q.Request.AfterSeq, q.Request.PageSize = pa.ReadViewEvents, "", 602, 256
	v = read()
	if len(v.Items) != 0 || v.NextSeq != 602 || v.HasMore {
		t.Fatal("snapshot replayed its initial events")
	}
	q.Request.AfterSeq = 603
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.CursorExpired)
}

func TestReadViewDefersTraceValidationUntilCollectionIsOpened(t *testing.T) {
	e, _, current := viewFixture(t, 100, 0, false)
	e.Events = append(e.Events, pa.Event{Version: pa.ContractVersion, ID: "trace-1", Seq: 101, Fence: current,
		Kind: pa.TraceUpdated, At: e.Bot.CreatedAt, Trace: &pa.Trace{TurnID: "t1", Category: "thought", Text: "stored trace"}})
	db, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	q.Request.PageSize = 20
	legacy := readView(t, views, q)
	if legacy.Head.TraceCategoryCount != 1 || !legacy.HasMore || len(legacy.Items) != 20 {
		t.Fatal("fixture needs a trace and a paged transcript")
	}

	// A damaged trace cannot block chat reads that explicitly defer the
	// trace projection. Opening the collection must still reject it.
	if _, err := db.Exec(`UPDATE project_assistant_events
		SET data=json_set(CAST(data AS TEXT), '$.trace.text', 42)
		WHERE machine=? AND bot=? AND json_extract(CAST(data AS TEXT), '$.kind')='trace_updated'`,
		e.Bot.Key.MachineID, e.Bot.Key.BotID); err != nil {
		t.Fatal(err)
	}
	_, err := views.Read(testContext, q)
	requireCode(t, err, pa.ScopeDenied)

	q.Request.DeferTraceCount = true
	snapshot := readView(t, views, q)
	if snapshot.Head.TraceCategoryCount != 0 || !reflect.DeepEqual(snapshot.Items, legacy.Items) {
		t.Fatal("opting out changed the messages or returned a trace count")
	}

	q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, snapshot.SourceEpoch, snapshot.Items[19].AfterCursor
	history := readView(t, views, q)
	if len(history.Items) != 20 || history.Items[0].AnchorMessageSeq != snapshot.Items[19].AnchorMessageSeq-1 {
		t.Fatal("older messages did not continue from the newest page")
	}

	q.Request.Mode, q.Request.Cursor, q.Request.AfterSeq = pa.ReadViewEvents, "", snapshot.Head.EventFrontier
	events := readView(t, views, q)
	if len(events.Items) != 0 || events.NextSeq != snapshot.Head.EventFrontier {
		t.Fatal("empty event poll lost the snapshot frontier")
	}

	q.Request.Mode, q.Request.AfterSeq = pa.ReadViewTraces, 0
	_, err = views.Read(testContext, q)
	requireCode(t, err, pa.ScopeDenied)
}

func TestReadViewKeepsDistinctTraceStepsAndLatestStepContent(t *testing.T) {
	e, _, current := viewFixture(t, 1, 0, false)
	for i, trace := range []pa.Trace{
		{ID: "text-1", TurnID: "t1", Category: "agent_message_chunk", Text: "First "},
		{ID: "tool-1", TurnID: "t1", Category: "tool_call", Text: "Read source"},
		{ID: "text-1", TurnID: "t1", Category: "agent_message_chunk", Text: "finding."},
		{ID: "text-2", TurnID: "t1", Category: "agent_message_chunk", Text: "Final result."},
		{TurnID: "t1", Category: "legacy", Text: "Old snapshot"},
		{TurnID: "t1", Category: "legacy", Text: "Updated old snapshot"},
	} {
		x := trace
		e.Events = append(e.Events, pa.Event{Version: pa.ContractVersion, ID: fmt.Sprintf("step-%d", i), Seq: int64(i + 2), Fence: current, Kind: pa.TraceUpdated, At: e.Bot.CreatedAt, Trace: &x})
	}
	_, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	snapshot := readView(t, views, q)
	if snapshot.Head.TraceCategoryCount != 4 {
		t.Fatalf("lost steps: got %d, want 4", snapshot.Head.TraceCategoryCount)
	}
	q.Request.Mode = pa.ReadViewTraces
	q.Request.SourceEpoch = snapshot.SourceEpoch
	page := readView(t, views, q)
	// A later chunk cannot invalidate the signed detail link already shown.
	oldCursor, failure := views.cursor(q, snapshot.Head, viewPosition{Collection: "trace", Anchor: 2, ID: viewTraceID("t1", "text-1")})
	if failure != nil {
		t.Fatal(failure)
	}
	old := q
	old.Request.Mode = pa.ReadViewDetail
	old.Request.Cursor = oldCursor
	old.Request.PageSize = 1
	if got := readView(t, views, old).Items[0].Trace.Text; got != "First " {
		t.Fatalf("pinned revision changed: %q", got)
	}
	wantIDs := []string{viewTraceID("t1", "legacy"), viewTraceID("t1", "text-2"), viewTraceID("t1", "tool-1"), viewTraceID("t1", "text-1")}
	for i, row := range page.Items {
		if row.ID != wantIDs[i] {
			t.Fatalf("step moved after update: %+v", page.Items)
		}
	}
	texts := map[string]string{}
	for _, row := range page.Items {
		detail := q
		detail.Request.Mode = pa.ReadViewDetail
		detail.Request.Cursor = row.TraceSummary.DetailCursor
		detail.Request.PageSize = 1
		v := readView(t, views, detail)
		trace := v.Items[0].Trace
		texts[trace.ID+trace.Category] = trace.Text
	}
	if texts["text-1agent_message_chunk"] != "First finding." || texts["text-2agent_message_chunk"] != "Final result." || texts["legacy"] != "Updated old snapshot" {
		t.Fatalf("trace contents: %+v", texts)
	}
}

func TestReadViewProgressPagesOnlyNarrationAtItsFirstEvent(t *testing.T) {
	exported, _, current := viewFixture(t, 1, 0, false)
	for i, trace := range []pa.Trace{
		{ID: "first", TurnID: "t1", Category: "agent_message_chunk", Text: "Checking "},
		{ID: "command", TurnID: "t1", Category: "tool_call", Text: "Run tests"},
		{ID: "second", TurnID: "t1", Category: "agent_message_chunk", Text: "Tests passed."},
		{ID: "first", TurnID: "t1", Category: "agent_message_chunk", Text: "the build."},
		{ID: "thought", TurnID: "t1", Category: "agent_thought_chunk", Text: "Internal deliberation"},
		{TurnID: "t1", Category: "agent_message_chunk", Text: "Legacy snapshot"},
		{TurnID: "t1", Category: "agent_message_chunk", Text: "Current legacy snapshot"},
	} {
		exported.Events = append(exported.Events, pa.Event{Version: pa.ContractVersion, ID: fmt.Sprintf("progress-%d", i),
			Seq: int64(i + 2), Fence: current, Kind: pa.TraceUpdated, At: exported.Bot.CreatedAt.Add(time.Duration(i+1) * time.Second), Trace: &trace})
	}
	_, _, views, _ := restoreViews(t, exported)
	q := viewQuery(exported.Bot)
	snapshot := readView(t, views, q)
	q.Request.Mode, q.Request.SourceEpoch, q.Request.PageSize = pa.ReadViewProgress, snapshot.SourceEpoch, 1
	for i, want := range []struct {
		text    string
		seconds int
	}{{"Current legacy snapshot", 7}, {"Tests passed.", 3}, {"Checking the build.", 1}} {
		page := readView(t, views, q)
		if len(page.Items) != 1 || page.Items[0].Trace == nil || page.Items[0].Trace.Text != want.text ||
			!page.Items[0].OccurredAt.Equal(exported.Bot.CreatedAt.Add(time.Duration(want.seconds)*time.Second)) ||
			page.HasMore != (i < 2) {
			t.Fatalf("progress page %d: %+v", i, page)
		}
		q.Request.Cursor = page.Items[0].AfterCursor
	}
}

func TestReadViewTraceCountSeparatesRoomAndChecksSupersededChunks(t *testing.T) {
	exported, old, current := viewFixture(t, 3, 0, false)
	roomTurn := exported.Turns[0]
	roomTurn.ID, roomTurn.MessageID, roomTurn.IdempotencyKey = "room-turn", exported.Messages[1].ID, "room-send"
	roomTurn.Origin = pa.HostNoticeOrigin
	roomTurn.Room = &pa.RoomTicket{
		Group:  pa.BotKey{MachineID: exported.Bot.Key.MachineID, BotID: "group"},
		Member: exported.Bot.Key, Epoch: 1, JoinedRevision: 1,
	}
	exported.Turns = append(exported.Turns, roomTurn)
	exported.Messages[1].TurnID, exported.Messages[1].Kind = roomTurn.ID, pa.NoticeMessage
	exported.Events[1].Message = &exported.Messages[1]
	secondTurn := exported.Turns[0]
	secondTurn.ID, secondTurn.MessageID, secondTurn.IdempotencyKey = "t2", exported.Messages[2].ID, "second-send"
	exported.Turns = append(exported.Turns, secondTurn)
	exported.Messages[2].TurnID, exported.Messages[2].Kind = secondTurn.ID, pa.UserMessage
	exported.Events[2].Message = &exported.Messages[2]
	for i, turnID := range []string{"t1", "t1", roomTurn.ID, secondTurn.ID} {
		exported.Events = append(exported.Events, pa.Event{
			Version: pa.ContractVersion, ID: fmt.Sprintf("trace-%d", i), Seq: int64(4 + i),
			Fence: current, Kind: pa.TraceUpdated, At: exported.Bot.CreatedAt,
			Trace: &pa.Trace{ID: "step", TurnID: turnID, Category: "agent_message_chunk", Text: fmt.Sprint(i)},
		})
	}
	db, _, views, _ := restoreViews(t, exported)
	q := viewQuery(exported.Bot)
	snapshot := readView(t, views, q)
	if got := snapshot.Head.TraceCategoryCount; got != 2 {
		t.Fatalf("own chat trace count = %d, want 2", got)
	}
	summary := q
	summary.Request.Mode, summary.Request.SourceEpoch = pa.ReadViewTraces, snapshot.SourceEpoch
	want := map[string]string{"t1": "01", secondTurn.ID: "3"}
	for _, row := range readView(t, views, summary).Items {
		detail := summary
		detail.Request.Mode, detail.Request.Cursor, detail.Request.PageSize = pa.ReadViewDetail, row.TraceSummary.DetailCursor, 1
		chunk := readView(t, views, detail).Items[0].Trace
		if expected, present := want[chunk.TurnID]; !present || chunk.Text != expected {
			t.Fatalf("unexpected detail for %q: %q", chunk.TurnID, chunk.Text)
		}
		delete(want, chunk.TurnID)
	}
	if len(want) != 0 {
		t.Fatalf("missing direct-conversation details: %v", want)
	}
	firstChunk := exported.Events[3]
	tests := []struct {
		name   string
		change func(pa.Event) pa.Event
	}{
		{"different binding", func(event pa.Event) pa.Event {
			event.Fence = old
			return event
		}},
		{"missing category", func(event pa.Event) pa.Event {
			trace := *event.Trace
			trace.Category = ""
			event.Trace = &trace
			return event
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.change(firstChunk))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE project_assistant_events SET data=? WHERE seq=?`, raw, firstChunk.Seq); err != nil {
				t.Fatal(err)
			}
			_, err = views.Read(testContext, q)
			requireCode(t, err, pa.ScopeDenied)
		})
	}
}

func TestReadViewOmitsContextUsageFromResetSession(t *testing.T) {
	exported, _, current := viewFixture(t, 1, 0, false)
	exported.Conversation.ActiveTurnID = ""
	exported.Conversation.ContextUsage = pa.ContextSample{
		TurnID: "t1", BindingID: current.BindingID, Generation: current.Generation,
		MemoryRevision: exported.Memory.Revision, UsedTokens: 402600, WindowTokens: 1000000,
	}
	_, _, views, _ := restoreViews(t, exported)
	q := viewQuery(exported.Bot)
	before := readView(t, views, q)
	if before.Head.ContextUsage == nil || before.Head.ContextUsage.UsedTokens != 402600 {
		t.Fatalf("expected stored context usage before reset, got %+v", before.Head.ContextUsage)
	}

	exported.Conversation.SessionResetGeneration = current.Generation
	exported.Conversation.SessionResetAfter = exported.Conversation.LastMessageSeq
	_, _, resetViews, _ := restoreViews(t, exported)
	snap := readView(t, resetViews, q)
	if snap.Head.ContextUsage != nil {
		t.Fatalf("expected nil context usage after reset in snapshot, got %+v", snap.Head.ContextUsage)
	}
	evQuery := q
	evQuery.Request.Mode, evQuery.Request.SourceEpoch = pa.ReadViewEvents, snap.SourceEpoch
	ev := readView(t, resetViews, evQuery)
	if ev.Head.ContextUsage != nil {
		t.Fatalf("expected nil context usage after reset in events, got %+v", ev.Head.ContextUsage)
	}
}

// The read view shows the queue in the order the worker runs it: the
// person's message that released a restart hold before the automatic report
// that waited in that queue.
func TestReadViewShowsTheReleasingMessageAheadOfHeldWork(t *testing.T) {
	e, _, current := viewFixture(t, 3, 0, false)
	e.Messages[1].Kind, e.Messages[1].TurnID = pa.NoticeMessage, "report"
	e.Messages[2].Kind, e.Messages[2].TurnID = pa.UserMessage, "person"
	e.Turns = append(e.Turns,
		pa.Turn{ID: "report", Fence: current, MessageID: e.Messages[1].ID, State: pa.TurnQueued, Origin: pa.HostNoticeOrigin, Instructions: e.Bot.Instructions, MemoryRevision: 1, IdempotencyKey: "report"},
		pa.Turn{ID: "person", Fence: current, MessageID: e.Messages[2].ID, State: pa.TurnQueued, Origin: pa.UserOrigin, ReleasedRestart: true, Instructions: e.Bot.Instructions, MemoryRevision: 1, IdempotencyKey: "person"})
	_, store, views, _ := restoreViews(t, e)
	pending, err := store.PendingTurns(testContext, e.Bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ID != "person" || pending[1].ID != "report" {
		t.Fatalf("worker order = %+v", pending)
	}
	v := readView(t, views, viewQuery(e.Bot))
	got := v.Snapshot.PendingTurns
	if len(got) != 2 || got[0].ID != "person" || got[1].ID != "report" ||
		v.Snapshot.QueuedMessages[0].ID != e.Messages[2].ID || v.Snapshot.QueuedMessages[1].ID != e.Messages[1].ID {
		t.Fatalf("read view order = %+v", got)
	}
	if !got[0].ReleasedRestart || got[0].Origin != pa.UserOrigin || got[1].ReleasedRestart || got[1].Origin != pa.HostNoticeOrigin {
		t.Fatalf("read view turn facts = %+v", got)
	}
}

// A chat page can carry exchange excerpts. Without the flag, the same page
// carries the stored text unchanged.
func TestReadViewExchangeExcerptsShortenOnlyWhenAsked(t *testing.T) {
	e, _, _ := viewFixture(t, 3, 0, false)
	db, _, views, _ := restoreViews(t, e)
	long := strings.Repeat("回", pa.MaxExchangeExcerptRunes+1)
	exchange, _ := json.Marshal(pa.Exchange{ID: "x1", Request: long, Reply: long})
	if _, err := db.Exec(`UPDATE project_assistant_bot_facts SET data=json_set(CAST(data AS TEXT),'$.text',?,'$.exchange',json(?)) WHERE collection='Messages' AND ord=3`, long, string(exchange)); err != nil {
		t.Fatal(err)
	}
	q := viewQuery(e.Bot)
	full := readView(t, views, q)
	q.Request.ExchangeExcerpts = true
	cut := readView(t, views, q)
	if m := full.Items[0].Message; m == nil || m.ID != "m003" || m.Text != long || m.Exchange.Request != long || m.Exchange.Reply != long {
		t.Fatalf("full page = %+v", full.Items[0].Message)
	}
	m := cut.Items[0].Message
	for _, s := range []string{m.Text, m.Exchange.Request, m.Exchange.Reply} {
		if s != strings.Repeat("回", pa.MaxExchangeExcerptRunes) {
			t.Fatalf("excerpt has %d characters", len([]rune(s)))
		}
	}
	if cut.Items[1].Message.Text != full.Items[1].Message.Text {
		t.Fatal("a message without an exchange changed")
	}
}
