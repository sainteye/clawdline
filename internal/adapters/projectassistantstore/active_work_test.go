package projectassistantstore

import (
	"reflect"
	"slices"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// rosterWork reads the Bot's active work through the live roster projection.
func rosterWork(t *testing.T, views *ViewStore, key pa.BotKey) int64 {
	t.Helper()
	for _, row := range rosterRead(t, views, rosterQuery()).Frame.Changes {
		if row.Key == key && row.Summary != nil {
			return row.Summary.ActiveWork
		}
	}
	t.Fatalf("roster has no row for %+v", key)
	return 0
}

// rosterSince reads the live roster's changes after cursor and returns the
// owner's new active work, failing when the change journal never mentioned it.
func rosterSince(t *testing.T, views *ViewStore, cursor *string, key pa.BotKey, why string) pa.BotSummary {
	t.Helper()
	q := rosterQuery()
	q.Request.Cursor = *cursor
	page := rosterRead(t, views, q)
	*cursor = page.Frame.Cursor
	for _, row := range page.Frame.Changes {
		if row.Key == key && row.Summary != nil {
			return *row.Summary
		}
	}
	t.Fatalf("%s: the live roster sent no change for the owner", why)
	return pa.BotSummary{}
}

func listSummary(t *testing.T, s *Store, key pa.BotKey) pa.BotSummary {
	t.Helper()
	page, err := s.List(testContext, pa.ListBots{MachineID: key.MachineID, PageSize: pa.MaxPageRows})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.Bot.Key == key {
			return item
		}
	}
	t.Fatalf("list has no row for %+v", key)
	return pa.BotSummary{}
}

func drain(wake <-chan struct{}) (woke bool) {
	for {
		select {
		case <-wake:
			woke = true
		default:
			return woke
		}
	}
}

func TestActiveWorkCountsUnfinishedLinksAndBackgroundTasksAndWakesTheRoster(t *testing.T) {
	db, s, _ := testDB(t)
	views := newTestViews(t, db)
	bot := seed(t, s, "owner")
	other := seed(t, s, "other")
	fence := populate(t, s, bot, pa.EffectDelivered)
	// populate leaves one work link of its own; count from there.
	base := listSummary(t, s, bot.Key).ActiveWork
	if roster := rosterWork(t, views, bot.Key); roster != base {
		t.Fatalf("list %d and roster %d disagree", base, roster)
	}

	links := []pa.WorkLink{
		{ID: "running", Fence: fence, TurnID: "turn", TaskID: "task-a", BoardItemID: "item", State: pa.WorkExecuting},
		{ID: "asking", Fence: fence, TurnID: "turn", TaskID: "task-b", BoardItemID: "item", State: pa.WorkWaitingForPerson},
		{ID: "done", Fence: fence, TurnID: "turn", TaskID: "task-c", BoardItemID: "item", State: pa.WorkLanded},
	}
	b, err := s.Bot(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(bot.Key, pa.DispatchOperation, "links"), ExpectedVersion: b.Version,
		WorkLinks: links, Receipt: pa.Receipt{ID: "links", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	if got := listSummary(t, s, bot.Key).ActiveWork; got != base+2 {
		t.Fatalf("list counts %d active links, want 2 (landed excluded)", got)
	}
	if got := rosterWork(t, views, bot.Key); got != base+2 {
		t.Fatalf("roster counts %d active links, want 2", got)
	}

	cursor := rosterRead(t, views, rosterQuery()).Frame.Cursor
	wake, _, closeFeed, err := s.SubscribeRoster("machine")
	if err != nil {
		t.Fatal(err)
	}
	defer closeFeed()
	task := pa.BackgroundTask{ID: "bg", Fence: fence, OriginTurnID: "turn", Title: "Check facts", Brief: "Compare the public sources.", CreatedAt: bot.CreatedAt.Add(time.Minute)}
	if _, err = s.AdmitBackgroundTask(testContext, command(bot.Key, pa.DispatchOperation, "bg"), task); err != nil {
		t.Fatal(err)
	}
	if !drain(wake) {
		t.Fatal("a queued background task did not wake the roster")
	}
	check := func(stage, status string, total int64, want *pa.BackgroundWork) {
		t.Helper()
		list := listSummary(t, s, bot.Key)
		live := rosterSince(t, views, &cursor, bot.Key, stage)
		for name, row := range map[string]pa.BotSummary{"list": list, "live": live} {
			if row.ActiveWork != total || row.Status != status || !reflect.DeepEqual(row.BackgroundWork, want) {
				t.Fatalf("%s %s: status %q, total %d, background %+v; want %q, %d, %+v",
					stage, name, row.Status, row.ActiveWork, row.BackgroundWork, status, total, want)
			}
		}
	}
	check("queued", "needsInput", base+3, &pa.BackgroundWork{Waiting: 1})
	if got := listSummary(t, s, other.Key).ActiveWork; got != 0 {
		t.Fatalf("another Bot borrowed the owner's work: %d", got)
	}

	// The chat turn ends, the worker starts: still unfinished, and the roster hears of it.
	turn, err := s.Turn(testContext, bot.Key, "turn")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := s.Conversation(testContext, bot.Key, bot.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	b, err = s.Bot(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	turn.State, conversation.ActiveTurnID = pa.TurnSucceeded, ""
	if _, err = s.Commit(testContext, app.Mutation{Command: command(bot.Key, pa.SendOperation, "finish"), ExpectedVersion: b.Version,
		Conversation: &conversation, Turn: &turn, Receipt: pa.Receipt{ID: "finish", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	drain(wake)
	check("chat finished", "idle", base+3, &pa.BackgroundWork{Waiting: 1})
	worker, err := s.StartBackgroundTask(testContext, bot.Key, bot.CreatedAt.Add(2*time.Minute))
	if err != nil || worker.BotID != "thread-bg" {
		t.Fatalf("start = %+v, %v", worker, err)
	}
	if !drain(wake) {
		t.Fatal("a started background task did not wake the roster")
	}
	check("worker is waiting for an execution slot", "idle", base+3, &pa.BackgroundWork{Waiting: 1})

	pending, err := s.PendingTurns(testContext, worker)
	if err != nil || len(pending) != 1 {
		t.Fatalf("worker queue = %+v, %v", pending, err)
	}
	workerTurn := pending[0]
	workerChat, err := s.Conversation(testContext, worker, workerTurn.Fence.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	workerChat.ActiveTurnID = workerTurn.ID
	setWorkerTurn := func(id string, state pa.TurnState, permissions ...pa.PermissionOffer) {
		t.Helper()
		wb, err := s.Bot(testContext, worker)
		if err != nil {
			t.Fatal(err)
		}
		workerTurn.State = state
		workerTurn.Instructions, workerTurn.MemoryRevision = wb.Instructions, 1
		if _, err = s.Commit(testContext, app.Mutation{Command: command(worker, pa.SendOperation, id), ExpectedVersion: wb.Version,
			Conversation: &workerChat, Turn: &workerTurn, Permissions: permissions,
			Receipt: pa.Receipt{ID: id, State: pa.EffectDelivered}}); err != nil {
			t.Fatal(err)
		}
	}
	setWorkerTurn("worker-running", pa.TurnRunning)
	if !drain(wake) {
		t.Fatal("starting the background turn did not wake the owner's roster")
	}
	check("running", "idle", base+3, &pa.BackgroundWork{Working: 1})
	offer := pa.PermissionOffer{ID: "worker-permission", Fence: workerTurn.Fence, TurnID: workerTurn.ID,
		NativeRequestID: "worker-permission", State: pa.PermissionPending, ExpiresAt: bot.CreatedAt.Add(time.Hour),
		Options: []pa.PermissionOption{{ID: "reject", Kind: pa.RejectOnce}}}
	setWorkerTurn("worker-asks", pa.TurnWaitingPermission, offer)
	if !drain(wake) {
		t.Fatal("a background approval did not wake the owner's roster")
	}
	check("needs input", "idle", base+3, &pa.BackgroundWork{NeedsInput: 1})
	offer.State, offer.SelectedOptionID = pa.PermissionResolved, "reject"
	setWorkerTurn("worker-resumes", pa.TurnRunning, offer)
	if !drain(wake) {
		t.Fatal("answering a background approval did not wake the owner's roster")
	}
	check("resumed", "idle", base+3, &pa.BackgroundWork{Working: 1})

	workerChat.ActiveTurnID = ""
	setWorkerTurn("worker-done", pa.TurnFailed)
	if !drain(wake) {
		t.Fatal("a finished background task did not wake the roster")
	}
	check("finished", "idle", base+2, nil)
}

// The dispatched count is the roster's "clones" number and the source of the
// waiting-on-work state: accepted and executing run, waiting for the person
// needs you, a recent delivery is shown but not running, and failures,
// landings and stale or undated deliveries do not count.
func TestDispatchedWorkCountsSessionsByStateInListAndRoster(t *testing.T) {
	db, s, _ := testDB(t)
	views := newTestViews(t, db)
	bot := seed(t, s, "owner")
	fence := populate(t, s, bot, pa.EffectDelivered)
	b, err := s.Bot(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	// Replace populate's own link so the expected counts are absolute.
	export, err := s.Export(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	links := []pa.WorkLink{
		{ID: "queued", Fence: fence, TurnID: "turn", TaskID: "task-a", State: pa.WorkAccepted, Title: "Queued work", Project: "/p"},
		{ID: "running", Fence: fence, TurnID: "turn", TaskID: "task-b", State: pa.WorkExecuting, StartedAt: now.Add(-time.Minute)},
		{ID: "asking", Fence: fence, TurnID: "turn", TaskID: "task-c", State: pa.WorkWaitingForPerson},
		{ID: "delivered", Fence: fence, TurnID: "turn", TaskID: "task-d", State: pa.WorkCompleted, FinishedAt: now.Add(-time.Hour)},
		{ID: "old-delivery", Fence: fence, TurnID: "turn", TaskID: "task-e", State: pa.WorkCompleted, FinishedAt: now.Add(-pa.DeliveredWorkWindow - time.Minute)},
		{ID: "undated-delivery", Fence: fence, TurnID: "turn", TaskID: "task-f", State: pa.WorkCompleted},
		{ID: "failed", Fence: fence, TurnID: "turn", TaskID: "task-g", State: pa.WorkFailed},
		{ID: "landed", Fence: fence, TurnID: "turn", TaskID: "task-h", State: pa.WorkLanded},
		{ID: "unknown", Fence: fence, TurnID: "turn", TaskID: "task-i", State: pa.WorkUnknown},
	}
	for _, link := range export.WorkLinks {
		link.State = pa.WorkLanded
		links = append(links, link)
	}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(bot.Key, pa.DispatchOperation, "dispatched"), ExpectedVersion: b.Version,
		WorkLinks: links, Receipt: pa.Receipt{ID: "dispatched", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	want := &pa.DispatchedWork{Running: 2, NeedsYou: 1, Delivered: 1}
	if got := listSummary(t, s, bot.Key).DispatchedWork; !reflect.DeepEqual(got, want) {
		t.Fatalf("list dispatched_work = %+v, want %+v", got, want)
	}
	for _, row := range rosterRead(t, views, rosterQuery()).Frame.Changes {
		if row.Key == bot.Key && row.Summary != nil {
			if got := row.Summary.DispatchedWork; !reflect.DeepEqual(got, want) {
				t.Fatalf("roster dispatched_work = %+v, want %+v", got, want)
			}
			return
		}
	}
	t.Fatal("roster has no row for the owner")
}

func TestDispatchedWorkIsAbsentWhenNothingCounts(t *testing.T) {
	now := time.Now()
	none := []pa.WorkLink{{State: pa.WorkFailed}, {State: pa.WorkLanded}, {State: pa.WorkCompleted}, {State: pa.WorkUnknown}}
	if got := countDispatchedWork(slices.Values(none), now); got != nil {
		t.Fatalf("got %+v, want nil so the field is absent", got)
	}
}
