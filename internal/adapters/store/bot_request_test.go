package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	br "github.com/sainteye/clawdline/internal/domain/botrequest"
)

var brT0 = time.UnixMilli(1_795_500_000_000)

func brCreate(t *testing.T, s *Store, bot, message string, in br.CreateInput) br.Card {
	t.Helper()
	auth := br.Authority{Bot: "lead", ConversationID: "conv", TurnID: "turn-" + message, MessageID: message}
	c, err := br.NewCard(br.CardID("machine", "lead", "conv", message), "project", "/project", bot, auth, in, brT0)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.CreateBotRequest(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func brCheck(a br.Actor, marks ...br.Mark) func(*br.Card) ([]br.Event, error) {
	return func(c *br.Card) ([]br.Event, error) {
		changed, err := c.Check(a, marks, brT0)
		if err != nil || !changed {
			return nil, err
		}
		return []br.Event{{Kind: br.EventChecklistMark}}, nil
	}
}

func brDone(a br.Actor) func(*br.Card) ([]br.Event, error) {
	return func(c *br.Card) ([]br.Event, error) {
		changed, err := c.Done(a, brT0)
		if err != nil || !changed {
			return nil, err
		}
		return []br.Event{{Kind: br.EventDone}}, nil
	}
}

// One person message passed through two Bots yields one card with both in
// its chain.
func TestOnePersonMessageThroughTwoBotsIsOneCard(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := brCreate(t, s, "lead", "m1", br.CreateInput{Title: "feature", Kind: "development"})
	b := brCreate(t, s, "helper", "m1", br.CreateInput{Title: "another title", Kind: "publishing"})
	if a.ID != b.ID || b.Title != "feature" || len(b.Checklist) != 2 {
		t.Fatalf("second create made or changed a card: %+v", b)
	}
	if len(b.Chain) != 2 || b.Chain[1] != "helper" || b.CreatedBy != "lead" {
		t.Fatalf("chain %v creator %s", b.Chain, b.CreatedBy)
	}
	again := brCreate(t, s, "helper", "m1", br.CreateInput{Title: "x", Kind: "development"})
	if again.Version != b.Version {
		t.Fatalf("a repeated create wrote: %d → %d", b.Version, again.Version)
	}
	list, _, err := s.BotRequestCards(context.Background(), BotRequestFilter{}, "", 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %d %v", len(list), err)
	}
}

// A publishing card with no task, commit or verdict goes created → done with
// a URL note; done is refused while an entry is open; the completion is
// queued exactly once across a restart, and again after a reopen.
func TestCompletionNotifiesOncePerCycleAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var boardBefore int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_v2_items`).Scan(&boardBefore); err != nil {
		t.Fatal(err)
	}
	lead := br.Actor{Bot: "lead"}
	c := brCreate(t, s, "lead", "post", br.CreateInput{Title: "weekly post", Kind: "publishing"})
	_, err = s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Expected: c.Version, Actor: lead, Key: "d0", Apply: brDone(lead)})
	if br.CodeOf(err) != br.CodeChecklistOpen {
		t.Fatalf("done with open entries: %v", err)
	}
	res, err := s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Expected: c.Version, Actor: lead, Key: "k1", Apply: brCheck(lead,
		br.Mark{Label: "draft", State: br.EntryDone, Note: "in chat"},
		br.Mark{Label: "reviewed by the person", State: br.EntryDone, Note: "approved"},
		br.Mark{Label: "published", State: br.EntryDone, Note: "https://example.invalid/p/1"})})
	if err != nil || !res.Changed {
		t.Fatal(res, err)
	}
	res, err = s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Expected: res.Card.Version, Actor: lead, Key: "done", Apply: brDone(lead)})
	if err != nil || res.Card.State != br.StateDone {
		t.Fatal(res, err)
	}
	if got := res.Card.Checklist[2].Note; got != "https://example.invalid/p/1" {
		t.Fatalf("last note %q", got)
	}
	s.Close()

	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The retry after a restart replays its receipt; a fresh key finds the
	// card already done. Neither queues a second notice.
	if res, err := s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Actor: lead, Key: "done", Apply: brDone(lead)}); err != nil || !res.Replayed {
		t.Fatalf("retry %+v %v", res, err)
	}
	if res, err := s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Actor: br.Actor{Person: true}, Key: "p1", Apply: brDone(br.Actor{Person: true})}); err != nil || res.Changed {
		t.Fatalf("second done %+v %v", res, err)
	}
	effects, err := s.Effects(ctx, EffectBotRequestDone, br.NotifyKey(c.ID, 1))
	if err != nil || len(effects) != 1 {
		t.Fatalf("cycle 1 notices: %d %v", len(effects), err)
	}
	var p BotRequestDonePayload
	if err := json.Unmarshal(effects[0].Payload, &p); err != nil || !p.Person || len(p.Bots) != 0 {
		t.Fatalf("payload %+v %v (the lead marked it done, so no Bot is told)", p, err)
	}

	person := br.Actor{Person: true}
	res, err = s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Actor: person, Key: "reopen", Apply: func(c *br.Card) ([]br.Event, error) {
		ok, err := c.Reopen(person)
		if err != nil || !ok {
			return nil, err
		}
		return []br.Event{{Kind: br.EventReopened}}, nil
	}})
	if err != nil || res.Card.Cycle != 2 {
		t.Fatal(res, err)
	}
	if _, err := s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Expected: res.Card.Version, Actor: person, Key: "done2", Apply: brDone(person)}); err != nil {
		t.Fatal(err)
	}
	effects, _ = s.Effects(ctx, EffectBotRequestDone, br.NotifyKey(c.ID, 2))
	if len(effects) != 1 {
		t.Fatalf("cycle 2 notices: %d", len(effects))
	}
	if err := json.Unmarshal(effects[0].Payload, &p); err != nil || len(p.Bots) != 1 || p.Bots[0] != "lead" {
		t.Fatalf("cycle 2 payload %+v", p)
	}
	var boardAfter int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_v2_items`).Scan(&boardAfter); err != nil || boardAfter != boardBefore {
		t.Fatalf("Board items changed: %d → %d (%v)", boardBefore, boardAfter, err)
	}
}

// A batch tick on several cards answers per card and is idempotent.
func TestBatchTickIsPerCardAndIdempotent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	lead := br.Actor{Bot: "lead"}
	var targets []BotRequestCheckTarget
	for _, m := range []string{"a", "b", "c"} {
		c := brCreate(t, s, "lead", m, br.CreateInput{Title: "card " + m, Kind: "development"})
		targets = append(targets, BotRequestCheckTarget{CardID: c.ID, Expected: c.Version})
	}
	other := brCreate(t, s, "other", "d", br.CreateInput{Title: "not ours", Kind: "development"})
	targets = append(targets, BotRequestCheckTarget{CardID: other.ID, Expected: other.Version})
	targets = append(targets, BotRequestCheckTarget{CardID: "missing", Expected: 1})
	marks := []br.Mark{{Label: "main", State: br.EntryDone, Note: "0123abc"}, {Label: "GitHub", State: br.EntryDone, Note: "0123abc"}}
	first, err := s.CheckBotRequests(ctx, lead, "batch-1", targets, marks, brT0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ok", "ok", "ok", "refused", "refused"}
	for i, r := range first {
		if r.Outcome != want[i] {
			t.Fatalf("card %d: %+v", i, r)
		}
	}
	if first[3].Code != br.CodeForbidden || first[4].Code != br.CodeNotFound {
		t.Fatalf("refusals %+v %+v", first[3], first[4])
	}
	second, err := s.CheckBotRequests(ctx, lead, "batch-1", targets, marks, brT0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if second[i].Outcome != "unchanged" || second[i].Version != first[i].Version {
			t.Fatalf("retried batch wrote card %d: %+v", i, second[i])
		}
	}
	// A new key with the old versions: a stale version, refused per card.
	third, _ := s.CheckBotRequests(ctx, lead, "batch-2", targets[:1], marks, brT0)
	if third[0].Code != br.CodeVersionConflict || third[0].Version != first[0].Version {
		t.Fatalf("stale batch %+v", third[0])
	}
	targets[0].Expected = first[0].Version
	fourth, _ := s.CheckBotRequests(ctx, lead, "batch-3", targets[:1], marks, brT0)
	if fourth[0].Outcome != "unchanged" {
		t.Fatalf("same marks with a fresh key %+v", fourth[0])
	}
	if _, err := s.CheckBotRequests(ctx, lead, "x", make([]BotRequestCheckTarget, br.MaxBatchCards+1), marks, brT0); br.CodeOf(err) != br.CodeFull {
		t.Fatalf("oversized batch: %v", err)
	}
}

// A retried handoff queues one notice for the new holder.
func TestRetriedHandoffQueuesOneNotice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	c := brCreate(t, s, "lead", "m", br.CreateInput{Title: "f", Kind: "development"})
	brCreate(t, s, "reviewer", "m", br.CreateInput{Title: "f", Kind: "development"})
	c, _ = s.BotRequestCard(ctx, c.ID)
	lead := br.Actor{Bot: "lead"}
	hand := func(c *br.Card) ([]br.Event, error) {
		if err := c.Handoff(lead, "reviewer", br.RoleReviewer, "review 0123abc"); err != nil {
			return nil, err
		}
		return []br.Event{{Kind: br.EventHanded, Detail: "review 0123abc"}}, nil
	}
	w := BotRequestWrite{CardID: c.ID, Expected: c.Version, Actor: lead, Key: "h1", Apply: hand}
	first, err := s.UpdateBotRequest(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := s.UpdateBotRequest(ctx, w); err != nil || !res.Replayed {
		t.Fatalf("retry %+v %v", res, err)
	}
	effects, _ := s.Effects(ctx, EffectBotRequestHandoff, "bot-request-handoff-"+c.ID+"-"+itoaStore(first.Card.Version))
	if len(effects) != 1 {
		t.Fatalf("handoff notices %d", len(effects))
	}
	var p BotRequestHandoffPayload
	if err := json.Unmarshal(effects[0].Payload, &p); err != nil || p.ToBot != "reviewer" || p.Note != "review 0123abc" {
		t.Fatalf("payload %+v", p)
	}
	// The old holder's stale write is refused with the current version.
	_, err = s.UpdateBotRequest(ctx, BotRequestWrite{CardID: c.ID, Expected: c.Version, Actor: lead, Key: "h2", Apply: hand})
	if br.CodeOf(err) != br.CodeVersionConflict {
		t.Fatalf("stale handoff %v", err)
	}
	events, err := s.BotRequestEvents(ctx, c.ID)
	if err != nil || len(events) != 3 || events[2].Kind != br.EventHanded {
		t.Fatalf("events %+v %v", events, err)
	}
}
