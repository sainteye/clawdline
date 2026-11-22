package http

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Seed only the pending note, never a cancellation receipt or execution result.
func TestProjectAssistantFutureFollowUpBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_FOLLOWUP_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_FOLLOWUP_BROWSER_DIR for the synthetic browser flow")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, p, server := newRoutineHumanFixture(t)
	repo := store.New(f.s.store.ProjectAssistantDatabase())
	ctx := context.Background()
	for _, id := range []string{"future-en", "future-zh"} {
		x, err := repo.Export(ctx, f.key)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		d := pa.Routine{ID: id, Key: f.key, Project: x.Bot.Project, ConversationID: x.Conversation.ID, Name: id, Instruction: "Synthetic pending reminder " + id, TimeoutSeconds: 3600, Enabled: true, Version: 1, FollowUp: true, MemoryRevision: x.Memory.Revision}
		d, err = pa.SetRoutineSchedule(d, pa.Routine{}, []pa.RoutineTrigger{{Type: "once", At: now.Add(time.Hour).UnixMilli()}}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Commit(ctx, app.Mutation{Command: libraryFixtureCommand(f.key, id), ExpectedVersion: x.Bot.Version, Routine: &d, Receipt: pa.Receipt{ID: id, State: pa.EffectDelivered}}); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-followup.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("follow-up browser %v\n%s", err, log)
	}
	x, err := repo.Export(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Routines) != 2 || len(x.RoutineRuns) != 0 {
		t.Fatal("cancellation changed stored notes or started a run", x.Routines, x.RoutineRuns)
	}
	for _, d := range x.Routines {
		if !d.Deleted || d.Enabled || d.Version != 2 {
			t.Fatal("pending note was not cancelled exactly once", d)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 0 || len(p.opens) != 0 || f.providerCalls.Load() != 0 {
		t.Fatal("cancelling pending notes initialized a provider")
	}
	t.Log("production Routines/SourceClient -> authenticated HTTP/Host/SQLite: bilingual cancellation, read-only refusal, unknown response and exact replay; zero provider/model calls")
}
