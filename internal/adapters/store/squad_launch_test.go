package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSquadLaunchPersistsOriginalSnapshotAndBindsOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	document := json.RawMessage(`{"definition_id":"community.example.persona.editor","scope_id":"project-test","handbook":"first"}`)
	first, err := s.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PrepareSquadLaunch(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ActorCapability == second.ActorCapability || first.SnapshotID != second.SnapshotID {
		t.Fatal("launch identity, capability or content-addressed snapshot was not isolated")
	}
	if _, ok, err := s.AuthenticateSquadActor(ctx, first.ActorCapability); err != nil || ok {
		t.Fatalf("pending actor authenticated: ok=%t err=%v", ok, err)
	}
	if err := s.BindSquadConversation(ctx, first.ID, "conversation-1"); !errors.Is(err, ErrSquadLaunchConflict) {
		t.Fatalf("bound without terminal: %v", err)
	}
	if err := s.RecordSquadTerminal(ctx, first.ID, "terminal-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSquadTerminal(ctx, first.ID, "terminal-1"); err != nil {
		t.Fatalf("same terminal retry: %v", err)
	}
	if err := s.RecordSquadTerminal(ctx, first.ID, "terminal-other"); !errors.Is(err, ErrSquadLaunchConflict) {
		t.Fatalf("terminal reassignment: %v", err)
	}
	if err := s.BindSquadConversation(ctx, first.ID, "conversation-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindSquadConversation(ctx, first.ID, "conversation-1"); err != nil {
		t.Fatalf("same conversation retry: %v", err)
	}
	if err := s.BindSquadConversation(ctx, first.ID, "conversation-other"); !errors.Is(err, ErrSquadLaunchConflict) {
		t.Fatalf("conversation reassignment: %v", err)
	}
	if err := s.RecordSquadTerminal(ctx, second.ID, "terminal-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindSquadConversation(ctx, second.ID, "conversation-1"); !errors.Is(err, ErrSquadConversationTaken) {
		t.Fatalf("duplicate conversation: %v", err)
	}
	actor, ok, err := s.AuthenticateSquadActor(ctx, first.ActorCapability)
	if err != nil || !ok || actor.LaunchID != first.ID || actor.ConversationID != "conversation-1" ||
		actor.DefinitionID != "community.example.persona.editor" || actor.ScopeID != "project-test" {
		t.Fatalf("bound actor = %+v, %t, %v", actor, ok, err)
	}
	if _, ok, err := s.AuthenticateSquadActor(ctx, second.ActorCapability); err != nil || ok {
		t.Fatalf("unbound actor authenticated: ok=%t err=%v", ok, err)
	}
	if binding, ok, err := s.SquadBindingForSession(ctx, "terminal-1", "conversation-1"); err != nil || !ok ||
		binding.SnapshotID != first.SnapshotID || binding.DefinitionID != "community.example.persona.editor" ||
		binding.ScopeID != "project-test" {
		t.Fatalf("authoritative binding = %+v, %t, %v", binding, ok, err)
	}
	if _, ok, err := s.SquadBindingForSession(ctx, "terminal-2", "conversation-1"); err != nil || ok {
		t.Fatalf("wrong terminal inherited a binding: %t, %v", ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, snapshotID, ok, err := s.SquadSnapshotForConversation(ctx, "conversation-1")
	if err != nil || !ok || snapshotID != first.SnapshotID || !bytes.Equal(got, document) {
		t.Fatalf("reopened snapshot = %q, %q, %t, %v", got, snapshotID, ok, err)
	}
	if _, _, ok, err := s.SquadSnapshotForConversation(ctx, "legacy-conversation"); err != nil || ok {
		t.Fatalf("legacy lookup = %t, %v", ok, err)
	}
	if _, err := s.PrepareSquadResume(ctx, "legacy-conversation"); !errors.Is(err, ErrSquadLaunchUnknown) {
		t.Fatalf("legacy resume = %v", err)
	}
	if _, err := s.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"community.example.persona.editor","scope_id":"project-test","handbook":"changed"}`)); err != nil {
		t.Fatal(err)
	}
	resume, err := s.PrepareSquadResume(ctx, "conversation-1")
	if err != nil || resume.SnapshotID != first.SnapshotID || resume.ID == first.ID {
		t.Fatalf("resume identity = %q, %q, %v", resume.ID, resume.SnapshotID, err)
	}
	if err := s.RecordSquadTerminal(ctx, resume.ID, "terminal-resumed"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindSquadConversation(ctx, resume.ID, "conversation-1"); err != nil {
		t.Fatalf("same conversation restore = %v", err)
	}
	if actor, ok, err := s.AuthenticateSquadActor(ctx, resume.ActorCapability); err != nil || !ok ||
		actor.ConversationID != "conversation-1" || actor.SnapshotID != first.SnapshotID {
		t.Fatalf("resumed actor = %+v, %t, %v", actor, ok, err)
	}
	if binding, ok, err := s.SquadBindingForSession(ctx, "terminal-resumed", "conversation-1"); err != nil || !ok ||
		binding.SnapshotID != first.SnapshotID {
		t.Fatalf("resumed binding = %+v, %t, %v", binding, ok, err)
	}
	got, snapshotID, ok, err = s.SquadSnapshotForConversation(ctx, "conversation-1")
	if err != nil || !ok || snapshotID != first.SnapshotID || !bytes.Equal(got, document) {
		t.Fatalf("resumed snapshot drifted = %q, %q, %t, %v", got, snapshotID, ok, err)
	}
}

func TestSquadLaunchPrepareIsAtomicAndBounded(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.PrepareSquadLaunch(ctx, json.RawMessage(`not-json`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	tooLarge := append([]byte(`{"x":"`), bytes.Repeat([]byte("x"), MaxSquadSnapshotBytes)...)
	tooLarge = append(tooLarge, []byte(`"}`)...)
	if _, err := s.PrepareSquadLaunch(ctx, tooLarge); !errors.Is(err, ErrSquadSnapshotTooLarge) {
		t.Fatalf("large snapshot = %v", err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER refuse_squad_intent BEFORE INSERT ON squad_launches
		BEGIN SELECT RAISE(ABORT, 'injected intent failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"x"}`)); err == nil {
		t.Fatal("injected intent failure was ignored")
	}
	var snapshots, launches int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM squad_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM squad_launches`).Scan(&launches); err != nil {
		t.Fatal(err)
	}
	if snapshots != 0 || launches != 0 {
		t.Fatalf("partial prepare persisted: %d snapshots, %d launches", snapshots, launches)
	}
}
