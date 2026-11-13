package projectassistantstore

import (
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestReadViewCursorBindsAuthorityScopeEpochAndCollection(t *testing.T) {
	e, _, _ := viewFixture(t, 3, 0, false)
	db, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	q.Request.PageSize = 1
	v := readView(t, views, q)
	q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, v.SourceEpoch, v.Items[0].AfterCursor
	for _, test := range []struct {
		name string
		edit func(*app.ReadViewQuery)
		code pa.ErrorCode
	}{
		{"authority", func(q *app.ReadViewQuery) { q.AuthorizationBinding += "/other" }, pa.CursorExpired},
		{"epoch", func(q *app.ReadViewQuery) { q.Request.SourceEpoch += "x" }, pa.CursorExpired},
		{"tamper", func(q *app.ReadViewQuery) { q.Request.Cursor = "X" + q.Request.Cursor[1:] }, pa.CursorExpired},
		{"collection", func(q *app.ReadViewQuery) { q.Request.Mode = pa.ReadViewPermissions }, pa.CursorExpired},
		{"no authority", func(q *app.ReadViewQuery) { q.AuthorizationBinding = "" }, pa.ScopeDenied},
		{"no bytes", func(q *app.ReadViewQuery) { q.ByteLimit = 0 }, pa.Invalid},
		{"excess bytes", func(q *app.ReadViewQuery) { q.ByteLimit++ }, pa.Invalid},
		{"no cursor budget", func(q *app.ReadViewQuery) { q.CursorByteLimit = 0 }, pa.Invalid},
		{"excess cursor budget", func(q *app.ReadViewQuery) { q.CursorByteLimit++ }, pa.Invalid},
		{"lower cursor budget", func(q *app.ReadViewQuery) { q.CursorByteLimit = len(q.Request.Cursor) - 1 }, pa.CapacityFull},
		{"UTF-8 cursor lower budget", func(q *app.ReadViewQuery) {
			q.Request.Cursor, q.CursorByteLimit = strings.Repeat("界", 200), 500
		}, pa.CapacityFull},
		{"UTF-8 cursor maximum", func(q *app.ReadViewQuery) {
			q.Request.Cursor = strings.Repeat("界", 1400)
		}, pa.CapacityFull},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := q
			test.edit(&changed)
			_, err := views.Read(testContext, changed)
			requireCode(t, err, test.code)
		})
	}
	// A reader rebuilt over identical restored bytes/seq/version has a new
	// incarnation; no comparison of those values substitutes for the epoch.
	_, err := newTestViews(t, db).Read(testContext, q)
	requireCode(t, err, pa.CursorExpired)
	_, _, restoredViews, _ := restoreViews(t, e)
	_, err = restoredViews.Read(testContext, q)
	requireCode(t, err, pa.CursorExpired)
}

func TestReadViewHistorySurvivesMarkReadAndNewGeneration(t *testing.T) {
	e, _, current := viewFixture(t, 40, 1, true)
	// One offer establishes reference/fence isolation here. The independent
	// timeline/control test retains the full101-offer/101-option traversal.
	e.Permissions = e.Permissions[:1]
	_, store, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	q.Request.PageSize = 1
	v := readView(t, views, q)
	history := q
	history.Request.Mode, history.Request.SourceEpoch, history.Request.Cursor = pa.ReadViewHistory, v.SourceEpoch, v.Items[0].AfterCursor
	_, err := store.MarkRead(testContext, command(e.Bot.Key, pa.ReadOperation, "read"), pa.ReadCursor{Key: e.Bot.Key, ConversationID: e.Bot.ConversationID, ReaderID: "synthetic", MessageSeq: 1})
	if err != nil {
		t.Fatal(err)
	}
	v = readView(t, views, history)
	if v.Items[0].ID != "m039" || v.Head.BotVersion <= e.Bot.Version {
		t.Fatalf("MarkRead expired history: %+v", v)
	}
	permission := q
	permission.Request.Mode, permission.Request.SourceEpoch = pa.ReadViewPermissions, v.SourceEpoch
	v = readView(t, views, permission)
	detail := permission
	detail.Request.Mode, detail.Request.Cursor = pa.ReadViewDetail, v.Items[0].PermissionSummary.DetailCursor
	fresh := current
	fresh.Generation, fresh.BindingID = 3, "fresh"
	binding := pa.ExecutionBinding{Fence: fresh, Agent: e.Bot.Agent, MemoryRevision: 1, InstructionRevision: 1}
	c := e.Conversation
	c.Generation, c.BindingID, c.ActiveTurnID = 3, "fresh", ""
	b, err := store.Bot(testContext, e.Bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "generation"), ExpectedVersion: b.Version, Conversation: &c, Binding: &binding, Receipt: pa.Receipt{ID: "generation", State: pa.EffectDelivered}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = views.Read(testContext, detail)
	requireCode(t, err, pa.CursorExpired)
	_, err = views.Read(testContext, app.ReadViewQuery{Request: pa.ReadViewRequest{Key: history.Request.Key, ConversationID: history.Request.ConversationID, Mode: pa.ReadViewPermissions, SourceEpoch: history.Request.SourceEpoch, Cursor: v.Items[0].AfterCursor, PageSize: 1}, AuthorizationBinding: history.AuthorizationBinding, ByteLimit: history.ByteLimit, CursorByteLimit: history.CursorByteLimit})
	requireCode(t, err, pa.CursorExpired)
	// Continue the old signed position; the old-generation work remains a
	// durable conversation fact even when no offer is actionable anymore.
	history.Request.PageSize = pa.MaxPageRows
	found := false
	for {
		v = readView(t, views, history)
		for _, item := range v.Items {
			if item.Work != nil {
				found = item.Work.Fence.Generation == 1
			}
		}
		if !v.HasMore {
			break
		}
		history.Request.Cursor = v.Items[len(v.Items)-1].AfterCursor
	}
	if !found {
		t.Fatal("new generation hid historical work")
	}
}

func TestReadViewCursorBudgetKeepsEveryPrefixDecision(t *testing.T) {
	e, _, _ := viewFixture(t, 3, 0, false)
	// Legal persisted IDs are unbounded. The middle position exceeds4096 UTF-8
	// bytes but a later complete prefix can still have a small valid cursor.
	e.Messages[1].ID = strings.Repeat("界", pa.MaxReadViewCursorBytes)
	e.Events[1].Message.ID = e.Messages[1].ID
	_, _, views, _ := restoreViews(t, e)
	q := viewQuery(e.Bot)
	v := readView(t, views, q)
	if len(v.Items) != 3 || v.Items[0].CursorFailure != nil || v.Items[1].CursorFailure == nil || v.Items[2].CursorFailure != nil || v.Items[2].AfterCursor == "" || v.HasMore {
		t.Fatalf("intermediate cursor incorrectly rejected a later prefix: %+v", v)
	}
	q.Request.Mode, q.Request.SourceEpoch, q.Request.Cursor = pa.ReadViewHistory, v.SourceEpoch, v.Items[2].AfterCursor
	v = readView(t, views, q)
	if len(v.Items) != 0 || v.HasMore {
		t.Fatal("final prefix cursor did not reach true end")
	}
	// A page-size boundary that forces a stop at an unencodable position is
	// explicitly marked. The transport must refuse, never invent an end or
	// resume past the skipped middle row.
	q = viewQuery(e.Bot)
	q.Request.PageSize = 2
	v = readView(t, views, q)
	if len(v.Items) != 2 || !v.HasMore || v.Items[1].CursorFailure == nil || v.Items[1].AfterCursor != "" {
		t.Fatalf("forced bad continuation was hidden: %+v", v)
	}
}
