package projectassistantstore

import (
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestDurableEventPaginationAndExpiredCursor(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	populate(t, s, b, pa.EffectDelivered)
	other := seed(t, s, "other")
	q := pa.EventQuery{Key: b.Key, PageSize: 1}
	p, err := s.Read(testContext, q)
	if err != nil || len(p.Items) != 1 || !p.HasMore || p.Items[0].Kind != pa.BotCreated {
		t.Fatalf("first %+v %v", p, err)
	}
	q.AfterSeq = p.NextSeq
	p, err = s.Read(testContext, q)
	if err != nil || len(p.Items) != 1 || p.HasMore || p.Items[0].Kind != pa.MessageCreated {
		t.Fatalf("second %+v %v", p, err)
	}
	q.AfterSeq = p.NextSeq
	p, err = s.Read(testContext, q)
	if err != nil || len(p.Items) != 0 {
		t.Fatalf("end %+v %v", p, err)
	}
	q.Key = other.Key
	q.AfterSeq = 0
	p, err = s.Read(testContext, q)
	if err != nil || len(p.Items) != 1 || p.Items[0].Fence.Key != other.Key {
		t.Fatalf("foreign event %+v %v", p, err)
	}
	q.AfterSeq = 999
	_, err = s.Read(testContext, q)
	requireCode(t, err, pa.CursorExpired)
	q.AfterSeq = -1
	_, err = s.Read(testContext, q)
	requireCode(t, err, pa.CursorExpired)
	q.AfterSeq = 0
	q.PageSize = pa.MaxEventPageRows + 1
	_, err = s.Read(testContext, q)
	requireCode(t, err, pa.Invalid)
}

func TestEventsDoNotCountAsUnreadAndDuplicateEventRollsBack(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	f := populate(t, s, b, pa.EffectDelivered)
	trace := pa.Trace{TurnID: "turn", Category: "tool", Text: "reading", Complete: true}
	cmd := command(b.Key, pa.SendOperation, "trace")
	m := app.Mutation{Command: cmd, ExpectedVersion: 2, Receipt: pa.Receipt{ID: "trace", State: pa.EffectDelivered}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "trace", Fence: f, Kind: pa.TraceUpdated, Trace: &trace}}}
	if _, err := s.Commit(testContext, m); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: 1}, "alice")
	if err != nil || p.Items[0].UnreadCount != 1 {
		t.Fatalf("trace inflated unread %+v %v", p, err)
	}
	before, _ := s.Bot(testContext, b.Key)
	m.Command = command(b.Key, pa.SendOperation, "duplicate")
	m.ExpectedVersion = before.Version
	m.Receipt.ID = "duplicate"
	if _, err = s.Commit(testContext, m); err == nil {
		t.Fatal("duplicate event accepted")
	}
	after, _ := s.Bot(testContext, b.Key)
	if after.Version != before.Version {
		t.Fatal("duplicate event advanced CAS")
	}
	r, err := s.Receipt(testContext, m.Command)
	if err != nil || r != nil {
		t.Fatalf("duplicate event saved receipt %+v %v", r, err)
	}
}
