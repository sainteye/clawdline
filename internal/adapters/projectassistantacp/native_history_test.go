package projectassistantacp

import (
	"os"
	"path/filepath"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const ownedSessionID = "a1000001-1111-4111-8111-111111111111"

func historyRequest() app.OpenRequest {
	f := testFence()
	return app.OpenRequest{
		Bot:     pa.Bot{Key: f.Key, Project: f.Project, ConversationID: f.ConversationID, Agent: pa.ClaudeCode, Instructions: pa.InstructionSnapshot{Revision: 3}},
		Binding: pa.ExecutionBinding{Fence: f, Agent: pa.ClaudeCode, MemoryRevision: 5, ForgottenThrough: 2, InstructionRevision: 3},
	}
}

func preparedHistory(t *testing.T, q app.OpenRequest) (string, nativeHistory) {
	t.Helper()
	state, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	h, e := historyForRequest(state, q)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range []string{h.Config, h.Owners} {
		if e := privateDirectory(state, d); e != nil {
			t.Fatal(e)
		}
	}
	return state, h
}

func TestNativeHistoryKeepsOneDirectoryAcrossGenerationsOnly(t *testing.T) {
	state := t.TempDir()
	base := historyRequest()
	first, e := historyForRequest(state, base)
	if e != nil {
		t.Fatal(e)
	}
	next := base
	next.Binding.Fence.Generation, next.Binding.Fence.BindingID = 9, "binding-9"
	if h, e := historyForRequest(state, next); e != nil || h.Config != first.Config {
		t.Fatalf("a new process generation moved history: %v %v", h.Config, e)
	}
	// As in Codync, memory that only gained or changed entries keeps the
	// session's history; the next prompt carries the memory as it is now.
	grown := base
	grown.Binding.MemoryRevision++
	if h, e := historyForRequest(state, grown); e != nil || h.Config != first.Config {
		t.Fatalf("a memory addition moved history: %v %v", h.Config, e)
	}
	changes := map[string]func(*app.OpenRequest){
		"forget":       func(q *app.OpenRequest) { q.Binding.ForgottenThrough++ },
		"instructions": func(q *app.OpenRequest) { q.Binding.InstructionRevision++; q.Bot.Instructions.Revision++ },
		"conversation": func(q *app.OpenRequest) {
			q.Binding.Fence.ConversationID = "other"
			q.Bot.ConversationID = "other"
		},
		"bot": func(q *app.OpenRequest) { q.Binding.Fence.Key.BotID = "other"; q.Bot.Key.BotID = "other" },
	}
	for name, change := range changes {
		q := historyRequest()
		change(&q)
		h, e := historyForRequest(state, q)
		if e != nil || h.Config == first.Config {
			t.Fatalf("%s did not get an empty epoch: %v %v", name, h.Config, e)
		}
	}
}

func TestNativeHistoryRefusesABindingThatDoesNotMatchTheBot(t *testing.T) {
	refusals := map[string]func(*app.OpenRequest){
		"revoked":      func(q *app.OpenRequest) { q.Binding.Revoked = true },
		"agent":        func(q *app.OpenRequest) { q.Binding.Agent = pa.Codex },
		"instructions": func(q *app.OpenRequest) { q.Binding.InstructionRevision++ },
		"bot":          func(q *app.OpenRequest) { q.Bot.Key.BotID = "other" },
		"fence":        func(q *app.OpenRequest) { q.Binding.Fence.Generation = 0 },
	}
	for name, change := range refusals {
		q := historyRequest()
		change(&q)
		if _, e := historyForRequest(t.TempDir(), q); e == nil {
			t.Fatalf("%s mismatch was admitted", name)
		}
	}
}

func TestNativeHistoryOwnerRecordsAdmitOnlyTheirOwnSession(t *testing.T) {
	q := historyRequest()
	state, h := preparedHistory(t, q)
	if e := h.verifyID(ownedSessionID); pa.Code(e) != pa.ResumeUnsupported {
		t.Fatalf("unrecorded native ID was admitted: %v", e)
	}
	for _, bad := range []string{"", "../../x", "A1000001-1111-4111-8111-111111111111", ownedSessionID + "\n"} {
		if e := h.recordID(bad); e == nil {
			t.Fatalf("malformed native ID %q was recorded", bad)
		}
		if e := h.verifyID(bad); e == nil {
			t.Fatalf("malformed native ID %q was admitted", bad)
		}
	}
	if e := h.recordID(ownedSessionID); e != nil {
		t.Fatal(e)
	}
	if e := h.recordID(ownedSessionID); e != nil {
		t.Fatalf("recording the same owned ID twice failed: %v", e)
	}
	if e := h.verifyID(ownedSessionID); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(h.Owners, ownedSessionID+".json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("owner record is not private: %v %v", info, e)
	}

	// Another Bot's record copied into this epoch names another identity.
	other := historyRequest()
	other.Binding.Fence.Key.BotID, other.Bot.Key.BotID = "other", "other"
	foreign, e := historyForRequest(state, other)
	if e != nil {
		t.Fatal(e)
	}
	if e := privateDirectory(state, foreign.Owners); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(h.Owners, ownedSessionID+".json"))
	if e := os.WriteFile(filepath.Join(foreign.Owners, ownedSessionID+".json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := foreign.verifyID(ownedSessionID); pa.Code(e) != pa.ScopeDenied {
		t.Fatalf("a copied owner record crossed Bots: %v", e)
	}
	if e := foreign.recordID(ownedSessionID); pa.Code(e) != pa.ScopeDenied {
		t.Fatalf("a copied owner record was accepted on record: %v", e)
	}
}

func TestNativeHistoryRefusesLinkedComponents(t *testing.T) {
	q := historyRequest()
	state, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	h, e := historyForRequest(state, q)
	if e != nil {
		t.Fatal(e)
	}
	elsewhere := t.TempDir()
	if e := os.MkdirAll(filepath.Join(state, "project-assistants"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(elsewhere, filepath.Join(state, "project-assistants", "native")); e != nil {
		t.Fatal(e)
	}
	if e := privateDirectory(state, h.Config); pa.Code(e) != pa.ScopeDenied {
		t.Fatalf("a linked component was followed: %v", e)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatal("refused history created files through the link")
	}
	if e := privateDirectory(state, filepath.Join(filepath.Dir(state), "outside")); pa.Code(e) != pa.ScopeDenied {
		t.Fatalf("a directory outside state was admitted: %v", e)
	}

	// A linked owner record is not read even if its content would match.
	_, owned := preparedHistory(t, q)
	target := filepath.Join(t.TempDir(), "record.json")
	if e := os.WriteFile(target, []byte(`{"Policy":"`+nativeHistoryPolicy+`","Identity":"`+owned.Identity+`","SessionID":"`+ownedSessionID+`"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, filepath.Join(owned.Owners, ownedSessionID+".json")); e != nil {
		t.Fatal(e)
	}
	if e := owned.verifyID(ownedSessionID); e == nil {
		t.Fatal("a linked owner record was admitted")
	}
}
