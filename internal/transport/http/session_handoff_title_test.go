package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

const handoffSummaryForNaming = `## Goal
Finish the sixth batch of white-background product images.

## Verified decisions
None.

## Blockers
None.

## Evidence
- /project/report.md

## Next step
Compare the eight baseline files against the candidate test run.
`

// storedMilestoneHandoff records a delivered milestone handoff the way the
// broker leaves it, with its summary in the package directory it chose.
func storedMilestoneHandoff(t *testing.T, s *Server, id, title string) orchestrator.Handoff {
	t.Helper()
	dir := filepath.Join(s.broker.HandoffRoot(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff.md"), []byte(handoffSummaryForNaming), 0o600); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"handoff_id": id, "state": orchestrator.HandoffDelivered, "project_dir": "/project",
		"dir": dir, "title": title, "from_session": "%1", "assistant": "codex", "created": 1_790_000_000,
		"milestone": true, "opened": map[string]any{"terminal_id": "%9", "backend": "tmux", "opened_at": 1_790_000_001}}
	body, _ := json.Marshal(record)
	at := time.Unix(1_790_000_000, 0)
	if err := s.store.CreateOpened(t.Context(), store.TableHandoffs, store.Opened{ID: id,
		State: orchestrator.HandoffDelivered, Record: body, CreatedAt: at, UpdatedAt: at}, nil); err != nil {
		t.Fatal(err)
	}
	h, err := s.broker.HandoffByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Milestone || h.Opened == nil {
		t.Fatalf("fixture did not decode as an opened milestone handoff: %+v", h)
	}
	return h
}

// The receiver's first message is the generic handoff line, so an assistant
// naming itself from it called every receiver "picking up a milestone
// handoff". An untitled handoff is named from the summary's Goal and Next step.
func TestAnUntitledMilestoneHandoffIsNamedFromItsGoal(t *testing.T) {
	s := capacityServer(t)
	s.broker = newBroker(s)
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true, "auto_name_assistant": "session"}); err != nil {
		t.Fatal(err)
	}
	var asked, by string
	s.nameSession = func(_ context.Context, text, assistant string) (string, error) {
		asked, by = text, assistant
		return "Sixth batch white backgrounds: baseline vs candidate", nil
	}
	h := storedMilestoneHandoff(t, s, "handoff-untitled", "")

	s.nameUntitledHandoff(h)

	got, err := s.broker.HandoffByID(t.Context(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Sixth batch white backgrounds: baseline vs candidate" {
		t.Fatalf("title = %q", got.Title)
	}
	if by != "codex" {
		t.Fatalf("the session's own assistant should name it, got %q", by)
	}
	for _, want := range []string{"sixth batch of white-background", "eight baseline files"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the namer read %q, missing %q", asked, want)
		}
	}
	if strings.Contains(asked, "picking up") || strings.Contains(asked, "handoff.md") || strings.Contains(asked, "report.md") {
		t.Fatalf("the namer read the launch line or the evidence: %q", asked)
	}

	// The naming model reads the same thing when a person asks for a name
	// from the receiver's first message.
	line := orchestrator.MilestoneHandoffLine(h.Dir)
	if resolved := s.resolveWrapper(t.Context(), line); resolved != asked {
		t.Fatalf("the launch line resolved to %q, want the summary's Goal and Next step", resolved)
	}
	elsewhere := orchestrator.MilestoneHandoffLine(filepath.Join(t.TempDir(), "unrecorded"))
	if resolved := s.resolveWrapper(t.Context(), elsewhere); resolved != elsewhere {
		t.Fatalf("a line naming a directory this daemon never recorded was rewritten to %q", resolved)
	}
}

func TestAHandoffTitleIsNeverReplacedAndNamingIsOptIn(t *testing.T) {
	s := capacityServer(t)
	s.broker = newBroker(s)
	var calls atomic.Int32
	s.nameSession = func(context.Context, string, string) (string, error) {
		calls.Add(1)
		return "Model title", nil
	}

	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true}); err != nil {
		t.Fatal(err)
	}
	titled := storedMilestoneHandoff(t, s, "handoff-titled", "Sender's title")
	s.nameUntitledHandoff(titled)
	if got, _ := s.broker.HandoffByID(t.Context(), titled.ID); got.Title != "Sender's title" || calls.Load() != 0 {
		t.Fatalf("a sender's title became %q after %d model turns", got.Title, calls.Load())
	}
	if _, set, err := s.broker.NameHandoff(t.Context(), titled.ID, "Later"); err != nil || set {
		t.Fatalf("NameHandoff replaced a title: set=%v err=%v", set, err)
	}

	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": false}); err != nil {
		t.Fatal(err)
	}
	untitled := storedMilestoneHandoff(t, s, "handoff-off", "")
	s.nameUntitledHandoff(untitled)
	if got, _ := s.broker.HandoffByID(t.Context(), untitled.ID); got.Title != "" || calls.Load() != 0 {
		t.Fatalf("automatic naming is off, yet the handoff was named %q after %d model turns", got.Title, calls.Load())
	}
}
