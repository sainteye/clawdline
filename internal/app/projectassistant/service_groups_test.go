package projectassistant_test

import (
	"context"
	"sync"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// personAuth is the human authority the host's pairing produces. Revision
// changes when the device is re-paired; readOnly is a viewer device.
type personAuth struct {
	mu       sync.Mutex
	revision string
	readOnly bool
	calls    int
	// after, when set, is the revision from the given call on.
	after     string
	afterCall int
}

func (p *personAuth) authorize(_ context.Context, key pa.BotKey, write bool) (app.Authority, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	rev := p.revision
	if p.after != "" && p.calls >= p.afterCall {
		rev = p.after
	}
	src := roomSource(key.MachineID)
	if p.readOnly {
		src.Writable = false
		if write {
			return app.Authority{}, &pa.Failure{Code: pa.ScopeDenied, Field: "read_only_device"}
		}
	}
	return app.Authority{PrincipalID: "person", ReaderID: "reader", AuthorizationRevision: rev, Feature: pa.FeatureState{Enabled: true}, Source: src}, nil
}

func (h *roomHarness) service(t *testing.T, auth *personAuth) *app.Service {
	t.Helper()
	s, err := app.NewService(app.ServiceOptions{Repository: h.bots, Memory: h.bots, Events: h.bots, Runtime: h.engine, Projects: roomResolver{h.scope},
		Groups: h.rooms, Rooms: h.driver, Authorize: auth.authorize, Clock: func() time.Time { return roomAt }, ID: func() string { return "svc-" + time.Now().Format("150405.000000000") }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServiceGroupOperationsUseThePersonsAuthority(t *testing.T) {
	h := newRoomHarness(t, "Alice", "Bob")
	alice, bob := h.members[0], h.members[1]
	other := h.scope
	other.ProjectID = "p2"
	stranger := storeBot(t, h.bots, "x", "Xavier", other)
	auth := &personAuth{revision: "1"}
	s := h.service(t, auth)
	key := pa.BotKey{MachineID: "m", BotID: "g"}
	target := pa.ProjectSelection{MachineID: "m", ProjectID: "p"}

	if _, _, err := s.CreateGroup(roomCtx, roomCommand(key, pa.CreateOperation, "bad"), app.CreateGroup{Target: target, Name: "Mixed", Members: []pa.BotKey{alice.Key, stranger.Key}}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a member from another Project: %v", err)
	}
	create := roomCommand(key, pa.CreateOperation, "create")
	in := app.CreateGroup{Target: target, Name: "Pair", Members: []pa.BotKey{alice.Key, bob.Key}}
	g, r, err := s.CreateGroup(roomCtx, create, in)
	if err != nil || g.Key != key || len(g.Members) != 2 || g.Project != h.scope {
		t.Fatalf("create %+v %v", g, err)
	}
	// A retry generates another conversation ID and still replays.
	if again, r2, err := s.CreateGroup(roomCtx, create, in); err != nil || r2.ID != r.ID || again.ConversationID != g.ConversationID {
		t.Fatalf("retried create %+v %v", r2, err)
	}
	// The same Bots under a new key open the group they share.
	twin, _, err := s.CreateGroup(roomCtx, roomCommand(pa.BotKey{MachineID: "m", BotID: "g2"}, pa.CreateOperation, "twin"), app.CreateGroup{Target: target, Name: "Again", Members: []pa.BotKey{bob.Key, alice.Key}})
	if err != nil || twin.Key != key {
		t.Fatalf("same members opened %+v %v", twin, err)
	}

	h.peer.reply("a", "On it.")
	say := roomCommand(key, pa.SendOperation, "say")
	entry, step, err := s.SendGroup(roomCtx, say, "@Alice status?", pa.BotKey{})
	if err != nil || step.Turn == nil || entry.Kind != pa.RoomPersonEntry {
		t.Fatalf("send %+v %+v %v", entry, step, err)
	}
	awaitRoom(t, "Alice's room answer", func() bool { return len(h.lines(t, key)) == 2 })
	h.driver.Idle()
	if replay, step, err := s.SendGroup(roomCtx, say, "@Alice status?", pa.BotKey{}); err != nil || step.Turn != nil || replay.Seq != entry.Seq {
		t.Fatalf("replayed send %+v %+v %v", replay, step, err)
	}
	if len(h.lines(t, key)) != 2 || len(h.roomTurns(t, alice)) != 1 {
		t.Fatal("a replayed send ran the room again")
	}
	if _, _, err = s.SendGroup(roomCtx, say, "something else", pa.BotKey{}); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("a changed send under the same key: %v", err)
	}

	// Re-paired between the first read and the write: refused, nothing written.
	auth.mu.Lock()
	auth.after, auth.afterCall = "2", auth.calls+2
	auth.mu.Unlock()
	if _, _, err = s.SendGroup(roomCtx, roomCommand(key, pa.SendOperation, "repaired"), "hello?", pa.BotKey{}); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a send across a re-pairing: %v", err)
	}
	if len(h.lines(t, key)) != 2 {
		t.Fatal("a send across a re-pairing wrote a line")
	}
	auth.mu.Lock()
	auth.revision, auth.after = "2", ""
	auth.mu.Unlock()

	// A read-only device reads the room and cannot write to it.
	viewer := h.service(t, &personAuth{revision: "2", readOnly: true})
	page, err := viewer.GroupHistory(roomCtx, app.GroupHistoryQuery{Key: key, PageSize: 10})
	if err != nil || len(page.Lines) != 2 || page.Lines[1].Author != alice.Key || page.Group.Key != key {
		t.Fatalf("history %+v %v", page, err)
	}
	if _, _, err = viewer.SendGroup(roomCtx, roomCommand(key, pa.SendOperation, "viewer"), "hi", pa.BotKey{}); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a read-only device sent: %v", err)
	}
	if _, err = viewer.StopGroup(roomCtx, key); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a read-only device stopped the room: %v", err)
	}
	if list, err := viewer.Groups(roomCtx, "m"); err != nil || len(list) != 1 {
		t.Fatalf("groups %+v %v", list, err)
	}

	if g, _, err = s.EditGroup(roomCtx, roomCommand(key, pa.EditOperation, "rename"), pa.GroupEdit{ExpectedVersion: g.Version, Name: "Renamed", Members: []pa.BotKey{alice.Key, bob.Key}}); err != nil || g.Name != "Renamed" {
		t.Fatalf("edit %+v %v", g, err)
	}
	if stop, err := s.StopGroup(roomCtx, key); err != nil || !stop.Stopped {
		t.Fatalf("stop %+v %v", stop, err)
	}
	if _, err = s.DeleteGroup(roomCtx, roomCommand(key, pa.DeleteOperation, "delete"), g.Version); err != nil {
		t.Fatal(err)
	}
	for _, b := range []pa.Bot{alice, bob} {
		if _, err = h.bots.Bot(roomCtx, b.Key); err != nil {
			t.Fatalf("deleting the group touched %s: %v", b.Name, err)
		}
	}
	if _, _, err = s.SendGroup(roomCtx, roomCommand(key, pa.SendOperation, "after-delete"), "anyone?", pa.BotKey{}); pa.Code(err) != pa.NotFound {
		t.Fatalf("a send to a deleted group: %v", err)
	}
}
