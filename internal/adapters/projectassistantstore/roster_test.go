package projectassistantstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func rosterQuery() app.RosterQuery {
	return app.RosterQuery{Request: pa.RosterRequest{MachineID: "machine"}, AuthorizationBinding: "trusted-grant", ReaderID: "reader", ByteLimit: cloudops.ReadPlaintextBytesLimit}
}

func rosterRead(t *testing.T, v *ViewStore, q app.RosterQuery) app.RosterPage {
	t.Helper()
	out, err := v.ReadRoster(testContext, q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRosterSnapshotPagesReplayConcurrentChangesAndDeletion(t *testing.T) {
	db, s, _ := testDB(t)
	views := newTestViews(t, db)
	for i := 0; i < pa.MaxPageRows+2; i++ {
		seed(t, s, fmt.Sprintf("bot%03d", i))
	}
	q := rosterQuery()
	first := rosterRead(t, views, q)
	if !first.More || first.Frame.Type != "snapshot" || len(first.Frame.Changes) != pa.MaxPageRows {
		t.Fatalf("first page %+v", first)
	}
	seen := map[pa.BotKey]pa.RosterChange{}
	apply := func(page app.RosterPage) {
		for _, row := range page.Frame.Changes {
			if row.Revision >= seen[row.Key].Revision {
				seen[row.Key] = row
			}
		}
	}
	apply(first)
	// A newly inserted key sorts BEFORE the page boundary; a removal happens
	// in the unfinished suffix; hide changes a row already enumerated.
	seed(t, s, "000-before")
	b, err := s.Bot(testContext, pa.BotKey{MachineID: "machine", BotID: "bot000"})
	if err != nil {
		t.Fatal(err)
	}
	b.Version++
	b.Hidden = true
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "hide"), ExpectedVersion: b.Version - 1, Bot: &b, Receipt: pa.Receipt{ID: "hide", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteBot(testContext, pa.BotKey{MachineID: "machine", BotID: "bot101"}); err != nil {
		t.Fatal(err)
	}
	q.Request.Cursor = first.Frame.Cursor
	page := rosterRead(t, views, q)
	apply(page)
	if page.More || page.Frame.Type != "snapshot-complete" {
		t.Fatalf("completion %+v", page)
	}
	q.Request.Cursor = page.Frame.Cursor
	page = rosterRead(t, views, q)
	apply(page)
	if len(page.Frame.Changes) != 3 {
		t.Fatalf("lost concurrent changes %+v", page.Frame)
	}
	// Duplicate replay replaces absolute unread; it cannot increment it.
	apply(rosterRead(t, views, q))
	actual, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: pa.MaxPageRows}, "reader")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range seen {
		if !row.Removed {
			count++
		}
	}
	// There are exactly 101 visible rows; the old list only returns 100 here.
	if count != pa.MaxPageRows+1 || !seen[b.Key].Removed || !seen[pa.BotKey{MachineID: "machine", BotID: "bot101"}].Removed || seen[pa.BotKey{MachineID: "machine", BotID: "000-before"}].Removed {
		t.Fatalf("not converged: %d %+v", count, seen)
	}
	for _, row := range actual.Items {
		if row.Bot.Hidden {
			continue
		}
		if !reflect.DeepEqual(*seen[row.Bot.Key].Summary, row) {
			t.Fatalf("authoritative mismatch %s", row.Bot.Key.BotID)
		}
	}
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	q.Request.Cursor = page.Frame.Cursor
	if got := rosterRead(t, views, q); len(got.Frame.Changes) != 0 {
		t.Fatal("repeat installer invented changes", got)
	}
}

func TestRosterProjectionReadsOnlyCurrentSummaryFacts(t *testing.T) {
	e, _, _ := viewFixture(t, 1800, 600, true)
	db, s, views, _ := restoreViews(t, e)
	q := rosterQuery()
	started := time.Now()
	got := rosterRead(t, views, q)
	t.Logf("1800 messages/600 work links roster projection: %s", time.Since(started))
	want, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, q.ReaderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Frame.Changes) != 1 || !reflect.DeepEqual(*got.Frame.Changes[0].Summary, want.Items[0]) {
		t.Fatalf("summary mismatch\ngot %+v\nwant %+v", got.Frame.Changes, want.Items)
	}
	// This old turn's instructions cannot decode as the aggregate type. The
	// roster must not load them, work history or obsolete permission options.
	if _, err = db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Turns.t1.instructions',json('[1,2,3]'))`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Export(testContext, e.Bot.Key); err == nil {
		t.Fatal("negative control did not break aggregate decoding")
	}
	if got = rosterRead(t, views, q); got.Frame.Changes[0].Summary.UnreadCount != want.Items[0].UnreadCount || want.Items[0].UnreadCount == 0 || want.Items[0].UnreadCount >= 1800 {
		t.Fatal("reader count", got)
	}
	q.ByteLimit = 1
	_, err = views.ReadRoster(testContext, q)
	requireCode(t, err, pa.CapacityFull)
}

func TestRosterProjectionRetainsFilesOnlyPreview(t *testing.T) {
	for _, text := range []string{"", "Please read these files"} {
		t.Run(fmt.Sprintf("text=%q", text), func(t *testing.T) {
			e, _, _ := viewFixture(t, 1, 0, false)
			e.Messages[0].Text = text
			e.Messages[0].Files = []pa.InputFile{
				{ID: "11111111-1111-4111-8111-111111111111", Name: "report.txt", SHA256: strings.Repeat("a", 64)},
				{ID: "22222222-2222-4222-8222-222222222222", Name: "notes.json", SHA256: strings.Repeat("b", 64)},
			}
			e.Events[0].Message = &e.Messages[0]
			_, s, views, _ := restoreViews(t, e)
			q := rosterQuery()
			got := rosterRead(t, views, q)
			want, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, q.ReaderID)
			if err != nil {
				t.Fatal(err)
			}
			preview := text
			if preview == "" {
				preview = "report.txt · notes.json"
			}
			if len(got.Frame.Changes) != 1 || got.Frame.Changes[0].Summary.LatestMessage != preview || !reflect.DeepEqual(*got.Frame.Changes[0].Summary, want.Items[0]) {
				t.Fatalf("stream summary lost the existing input-file preview: got %+v; want %+v", got.Frame.Changes, want.Items)
			}
		})
	}
}

func TestRosterHiddenRowsKeepManagementMetadataUntilDeletion(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "hidden")
	views := newTestViews(t, db)
	q := rosterQuery()
	initial := rosterRead(t, views, q)
	b.Hidden = true
	b.Version++
	if _, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "hide"), ExpectedVersion: b.Version - 1, Bot: &b, Receipt: pa.Receipt{ID: "hidden", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	q.Request.Cursor = initial.Frame.Cursor
	hidden := rosterRead(t, views, q)
	if len(hidden.Frame.Changes) != 1 || !hidden.Frame.Changes[0].Removed || hidden.Frame.Changes[0].Summary == nil || !hidden.Frame.Changes[0].Summary.Bot.Hidden {
		t.Fatal("hiding a Bot lost the metadata needed to unhide it", hidden.Frame)
	}
	if err := s.DeleteBot(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	q.Request.Cursor = hidden.Frame.Cursor
	deleted := rosterRead(t, views, q)
	if len(deleted.Frame.Changes) != 1 || !deleted.Frame.Changes[0].Removed || deleted.Frame.Changes[0].Summary != nil || deleted.Frame.Changes[0].Revision <= hidden.Frame.Changes[0].Revision {
		t.Fatal("deletion did not replace hidden management metadata with a tombstone", deleted.Frame)
	}
}

func TestRosterCursorScopeReaderGrantRestartAndGenerationFences(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "one")
	views := newTestViews(t, db)
	q := rosterQuery()
	page := rosterRead(t, views, q)
	q.Request.Cursor = page.Frame.Cursor
	for _, mutate := range []func(*app.RosterQuery){
		func(q *app.RosterQuery) { q.ReaderID = "other" }, func(q *app.RosterQuery) { q.AuthorizationBinding = "revoked" },
		func(q *app.RosterQuery) { q.Request.MachineID = "other" }, func(q *app.RosterQuery) { q.Request.ProjectID = "other" },
		func(q *app.RosterQuery) { q.Request.Cursor = "A" + q.Request.Cursor[1:] },
	} {
		other := q
		mutate(&other)
		_, err := views.ReadRoster(testContext, other)
		requireCode(t, err, pa.CursorExpired)
	}
	_, err := newTestViews(t, db).ReadRoster(testContext, q)
	requireCode(t, err, pa.CursorExpired)
	q.Request.Cursor = ""
	q.Request.ProjectID = "foreign"
	if got := rosterRead(t, views, q); len(got.Frame.Changes) != 0 {
		t.Fatal("cross Project disclosure", got)
	}
	if err = views.ValidateRoster(testContext, page.Identities); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Conversation.generation',1,'$.Conversation.binding_id','replaced') WHERE bot=?`, b.Key.BotID); err != nil {
		t.Fatal(err)
	}
	requireCode(t, views.ValidateRoster(testContext, page.Identities), pa.CursorExpired)
}

func TestRosterWakeupsAreBoundedPostCommitAndContentFree(t *testing.T) {
	db, s, _ := testDB(t)
	wake, close, err := s.SubscribeRoster("machine")
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	b := seed(t, s, "one")
	<-wake
	seed(t, s, "one") // identical command receipt, no write and no wake-up.
	select {
	case <-wake:
		t.Fatal("duplicate woke feed")
	default:
	}
	var before int
	if err = db.QueryRow(`SELECT count(*) FROM project_assistant_roster_changes`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	next := b
	next.Version = 2
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "rollback"), ExpectedVersion: 1, Bot: &next, Receipt: pa.Receipt{ID: "created/one", State: pa.EffectDelivered}})
	if err == nil {
		t.Fatal("late receipt collision did not roll back")
	}
	select {
	case <-wake:
		t.Fatal("rollback woke feed")
	default:
	}
	var after int
	_ = db.QueryRow(`SELECT count(*) FROM project_assistant_roster_changes`).Scan(&after)
	if before != after {
		t.Fatal("phantom journal row")
	}
	populate(t, s, b, pa.EffectDelivered)
	<-wake
	read := pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "reader", MessageSeq: 1}
	if _, err = s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read"), read); err != nil {
		t.Fatal(err)
	}
	<-wake
	if _, err = s.Observe(testContext, command(b.Key, pa.ObserveOperation, "observe"), "sent", 2); err != nil {
		t.Fatal(err)
	}
	<-wake
	if err = s.DeleteBot(testContext, b.Key); err != nil {
		t.Fatal(err)
	}
	<-wake
	// The reader always reads from its durable cursor, so a wake-up that is
	// already pending covers later commits; a burst must not cut the feed.
	seed(t, s, "two")
	seed(t, s, "three")
	<-wake
	select {
	case <-wake:
		t.Fatal("coalesced commits woke the feed twice")
	default:
	}
	for i := 1; i < pa.MaxRosterSubscribers; i++ {
		_, cleanup, e := s.SubscribeRoster("machine")
		if e != nil {
			t.Fatal(e)
		}
		defer cleanup()
	}
	_, _, err = s.SubscribeRoster("machine")
	requireCode(t, err, pa.CapacityFull)
	if !strings.Contains(fmt.Sprint(err), "roster_subscribers") {
		t.Fatal(err)
	}
}

func TestRosterMemoryAndRestoreCommitWakeups(t *testing.T) {
	_, source, _ := testDB(t)
	b := seed(t, source, "restore")
	populate(t, source, b, pa.EffectDelivered)
	exported, err := source.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, target, _ := testDB(t)
	wake, close, err := target.SubscribeRoster("machine")
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if err = target.Restore(testContext, exported); err != nil {
		t.Fatal(err)
	}
	<-wake
	write := memoryWrite(b, 1, "remembered", "message", "Synthetic memory")
	if _, err = target.Save(testContext, write); err != nil {
		t.Fatal(err)
	}
	<-wake
	if _, err = target.Save(testContext, write); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
		t.Fatal("duplicate memory save woke feed")
	default:
	}
	if _, err = target.Forget(testContext, app.ForgetRequest{Command: command(b.Key, pa.ForgetOperation, "forget"), Project: b.Project, ExpectedRevision: 2, SourceIDs: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	<-wake
}

func TestRosterProjectionNamesTheLatestMessageKind(t *testing.T) {
	e, _, _ := viewFixture(t, 2, 0, false)
	e.Messages[1].Kind, e.Messages[1].TurnID = pa.WorkMessage, "t1"
	e.Messages[1].Text = "Four fixes are merged on the task branch."
	e.Events[1].Message = &e.Messages[1]
	_, s, views, _ := restoreViews(t, e)
	q := rosterQuery()
	got := rosterRead(t, views, q)
	want, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, q.ReaderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Frame.Changes) != 1 || got.Frame.Changes[0].Summary.LatestMessage != e.Messages[1].Text || got.Frame.Changes[0].Summary.LatestMessageKind != pa.WorkMessage || !reflect.DeepEqual(*got.Frame.Changes[0].Summary, want.Items[0]) {
		t.Fatalf("a Session report preview must say it is one: got %+v; want %+v", got.Frame.Changes, want.Items)
	}
}

func TestRosterKeepsCompactionAndLetsMemoryProposalsWaitQuietly(t *testing.T) {
	e, _, current := viewFixture(t, 2, 0, false)
	e.Messages[1].Kind = pa.NoticeMessage
	e.Messages[1].Text = "Earlier context was summarized to make room: 26K → 22K"
	notice := pa.NoticeRef{Key: pa.NoticeCompacted, TokensBefore: 26182, TokensAfter: 22113}
	e.Messages[1].Notice = &notice
	e.Events[1].Message = &e.Messages[1]
	offer := rosterDecision(current, "memory", pa.MemoryProposalQuestion, 2, time.Now().Add(time.Hour))
	offer.Decision.Memory = "A fact visible only inside the card"
	e.Permissions = append(e.Permissions, offer)
	_, s, views, _ := restoreViews(t, e)
	q := rosterQuery()
	roster := rosterRead(t, views, q).Frame.Changes[0].Summary
	list, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, q.ReaderID)
	if err != nil {
		t.Fatal(err)
	}
	for label, row := range map[string]pa.BotSummary{"list": list.Items[0], "roster": *roster} {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var display struct {
			Notice   pa.NoticeRef `json:"latest_message_notice"`
			Decision string       `json:"pending_decision_kind"`
		}
		if err := json.Unmarshal(data, &display); err != nil {
			t.Fatal(err)
		}
		if display.Notice != notice || display.Decision != "" || row.PendingPermissions != 0 || row.Status == "needsInput" || strings.Contains(string(data), offer.Decision.Memory) {
			t.Errorf("%s preview should preserve the compaction notice without asking for memory review: got %+v", label, display)
		}
	}
}
