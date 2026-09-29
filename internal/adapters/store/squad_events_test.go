package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func boundSquadLaunch(t *testing.T, s *Store) SquadLaunch {
	t.Helper()
	ctx := context.Background()
	launch, err := s.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test","skills":[{"id":"skill.a","version":"1","enabled":true},{"id":"skill.b","version":"1","enabled":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSquadTerminal(ctx, launch.ID, "terminal-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindSquadConversation(ctx, launch.ID, "conversation-a"); err != nil {
		t.Fatal(err)
	}
	return launch
}

func TestSquadSkillEventsValidateSnapshotAndReplayReceipt(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	launch := boundSquadLaunch(t, s)
	event := SquadSkillEvent{SkillID: "skill.a", SkillVersion: "1", ClientEventID: "event-1", Status: "applied"}
	receipt, err := s.RecordSquadSkillEvent(ctx, launch.ActorCapability, event)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Seq != 1 || receipt.Status != "applied" || receipt.DefinitionID != "clawdline.persona.architect" || receipt.ScopeID != "project-test" || receipt.ConversationID != "conversation-a" {
		t.Fatalf("receipt identity/status = %+v", receipt)
	}
	prior, err := s.RecordSquadSkillEvent(ctx, launch.ActorCapability, event)
	if err != nil || prior != receipt {
		t.Fatalf("retry did not replay original receipt: %+v, %v", prior, err)
	}
	changed := event
	changed.Status = "read"
	if _, err := s.RecordSquadSkillEvent(ctx, launch.ActorCapability, changed); !errors.Is(err, ErrSquadEventConflict) {
		t.Fatalf("changed retry = %v", err)
	}
	for _, invalid := range []SquadSkillEvent{
		{SkillID: "skill.b", SkillVersion: "1", ClientEventID: "event-2", Status: "applied"},
		{SkillID: "skill.a", SkillVersion: "old", ClientEventID: "event-3", Status: "applied"},
		{SkillID: "skill.a", SkillVersion: "1", ClientEventID: "event-4", Status: "invented"},
	} {
		if _, err := s.RecordSquadSkillEvent(ctx, launch.ActorCapability, invalid); !errors.Is(err, ErrSquadEventInvalid) {
			t.Fatalf("invalid event %+v = %v", invalid, err)
		}
	}
	if _, err := s.RecordSquadSkillEvent(ctx, "another-session-capability", event); !errors.Is(err, ErrSquadActorUnauthorized) {
		t.Fatalf("other actor = %v", err)
	}
	head, err := s.SquadEventHead(ctx)
	if err != nil || head != 1 {
		t.Fatalf("head = %d, %v", head, err)
	}
	page, more, err := s.SquadEventsAfter(ctx, 0, 1)
	if err != nil || more || len(page) != 1 || page[0] != receipt {
		t.Fatalf("first cursor page = %+v, %t, %v", page, more, err)
	}
	page, more, err = s.SquadEventsAfter(ctx, receipt.Seq, 1)
	if err != nil || more || len(page) != 0 {
		t.Fatalf("replay cursor page = %+v, %t, %v", page, more, err)
	}
	if _, _, err := s.SquadEventsAfter(ctx, 0, MaxSquadEventPageRows+1); !errors.Is(err, ErrSquadEventInvalid) {
		t.Fatalf("oversized page = %v", err)
	}
}

func TestSquadSkillEventFailureDoesNotReserveReceipt(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	launch := boundSquadLaunch(t, s)
	if _, err := s.db.Exec(`CREATE TRIGGER refuse_squad_event BEFORE INSERT ON squad_skill_events
		BEGIN SELECT RAISE(ABORT, 'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	event := SquadSkillEvent{SkillID: "skill.a", SkillVersion: "1", ClientEventID: "event-1", Status: "applied"}
	if _, err := s.RecordSquadSkillEvent(ctx, launch.ActorCapability, event); err == nil {
		t.Fatal("injected event failure was ignored")
	}
	head, err := s.SquadEventHead(ctx)
	if err != nil || head != 0 {
		t.Fatalf("failed event left a receipt: head=%d err=%v", head, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER refuse_squad_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordSquadSkillEvent(ctx, launch.ActorCapability, event); err != nil {
		t.Fatalf("retry after transient failure: %v", err)
	}
}
