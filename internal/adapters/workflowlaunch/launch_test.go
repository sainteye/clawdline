package workflowlaunch

import (
	"context"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/workflow"
	"github.com/sainteye/clawdline/skills"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshResumeAndChildrenKeepTheirFrozenWorkflow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	service := Service{Store: st, Dir: dir, Resolve: skills.ResolveWorkflow}
	root := t.TempDir()
	first, err := service.Prepare(ctx, root, "", "")
	if err != nil || first.Text != "" || first.ID == "" {
		t.Fatalf("standard added instructions or lost receipt: %+v %v", first, err)
	}
	if err := first.Record(ctx, "%1"); err != nil {
		t.Fatal(err)
	}
	if err := st.BindWorkflowConversation(ctx, first.ID, "%1", "original"); err != nil {
		t.Fatal(err)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"workflow_profile": "poteto", "codegraph_enabled": true}); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.Prepare(ctx, root, "", "")
	if err != nil || !strings.Contains(fresh.Text, "Poteto workflow") || !strings.Contains(fresh.Text, "ordinary source search") {
		t.Fatalf("poteto/codegraph not usable: %+v %v", fresh, err)
	}
	child, err := service.Prepare(ctx, root, "", "original")
	if err != nil || child.Text != "" {
		t.Fatalf("child reread machine choice: %+v %v", child, err)
	}
	resumed, err := service.Prepare(ctx, root, "original", "")
	if err != nil || resumed.Text != "" {
		t.Fatalf("resume gained new instructions: %+v %v", resumed, err)
	}
	for _, p := range []Prepared{child, resumed} {
		l, found, err := st.WorkflowLaunchByID(ctx, p.ID)
		if err != nil || !found || l.Snapshot.ProfileID != "clawdline" || l.Snapshot.CodeGraphEnabled {
			t.Fatalf("snapshot changed: %+v %v", l, err)
		}
	}
	for _, args := range [][2]string{{"legacy", ""}, {"", "legacy"}} {
		p, err := service.Prepare(ctx, root, args[0], args[1])
		if err != nil || p.ID != "" || p.Text != "" {
			t.Fatalf("legacy acquired today's method: %+v %v", p, err)
		}
	}
}

func TestSuperpowersUsesTheExistingInstructionBytes(t *testing.T) {
	s, err := skills.ResolveWorkflow("superpowers", false)
	if err != nil || s.Instructions != skills.WorkflowInstructions() {
		t.Fatalf("existing adaptation changed: %v", err)
	}
	standard, err := skills.ResolveWorkflow("clawdline", false)
	if err != nil || standard.Instructions != "" || len(standard.Stages) != 0 {
		t.Fatalf("standard gained extra ceremony: %+v %v", standard, err)
	}
}

func TestWrapperPreservesArgumentsInputEnvironmentAndExit(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("POSIX launch shell unavailable on this platform")
	}
	p := Prepared{ID: "abcdefghijklmnopqrstuv"}
	cmd := exec.Command("sh", "-c", p.Wrap(`sh -c 'read line; printf "%s|%s|%s" "$1" "$line" "$WORKFLOW_PROBE"; exit 7' sh 'a b'`))
	cmd.Env = []string{"PATH=/bin:/usr/bin", "WORKFLOW_PROBE=kept"}
	cmd.Stdin = strings.NewReader("original input\n")
	out, err := cmd.Output()
	if string(out) != "a b|original input|kept" {
		t.Fatalf("provider invocation changed: %q %v", out, err)
	}
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 7 {
		t.Fatalf("exit changed: %v", err)
	}
	if got := (Prepared{}).Wrap("original command"); got != "original command" {
		t.Fatalf("legacy wrapping changed: %s", got)
	}
}

func TestImageReviewSamplesEachNewExecutionButNeverChangesExistingLaunches(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	service := Service{Store: st, Dir: dir, Resolve: skills.ResolveWorkflow}
	root := t.TempDir()
	// These are the exact pre-feature instruction bytes, across all profiles.
	for _, profile := range []string{"clawdline", "superpowers", "poteto"} {
		before, err := skills.ResolveWorkflow(profile, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, settings := range []map[string]any{{"workflow_profile": profile}, {"workflow_profile": profile, "space_saving_images": false}} {
			if _, err := nextconfig.Open(dir).Set(settings); err != nil {
				t.Fatal(err)
			}
			p, err := service.Prepare(ctx, root, "", "")
			if err != nil || p.Text != before.Instructions {
				t.Fatalf("disabled %s changed bytes: %q %v", profile, p.Text, err)
			}
			l, found, err := st.WorkflowLaunchByID(ctx, p.ID)
			if err != nil || !found || l.SnapshotID != before.Digest {
				t.Fatalf("disabled snapshot changed: %+v %v", l, err)
			}
		}
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"workflow_profile": "clawdline", "space_saving_images": true}); err != nil {
		t.Fatal(err)
	}
	enabled, err := service.Prepare(ctx, root, "", "")
	if err != nil || enabled.Text != workflow.SpaceSavingImageInstructions() {
		t.Fatalf("missing image rules: %q %v", enabled.Text, err)
	}
	if err := enabled.Bind(ctx, "%71", "image-parent"); err != nil {
		t.Fatal(err)
	}
	// The next child uses current image consent, while retaining the parent's workflow.
	for _, on := range []bool{false, true, false} {
		if _, err := nextconfig.Open(dir).Set(map[string]any{"space_saving_images": on, "workflow_profile": "poteto"}); err != nil {
			t.Fatal(err)
		}
		child, err := service.Prepare(ctx, root, "", "image-parent")
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if on {
			want = workflow.SpaceSavingImageInstructions()
		}
		if child.Text != want {
			t.Fatalf("child on=%v: %q", on, child.Text)
		}
		resumed, err := service.Prepare(ctx, root, "image-parent", "")
		if err != nil || resumed.Text != "" {
			t.Fatalf("resume reinjected: %q %v", resumed.Text, err)
		}
		l, found, err := st.WorkflowLaunchByID(ctx, resumed.ID)
		if err != nil || !found || !l.Snapshot.SpaceSavingImages {
			t.Fatalf("resume rewrote frozen consent: %+v %v", l, err)
		}
	}
	if enabled.Text != workflow.SpaceSavingImageInstructions() {
		t.Fatal("existing launch mutated")
	}
}

func TestLegacyParentCanOptIntoImageReviewWithoutAcquiringTodaysWorkflow(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	service := Service{Store: st, Dir: dir, Resolve: skills.ResolveWorkflow}
	for _, on := range []bool{false, true, false} {
		if _, err := nextconfig.Open(dir).Set(map[string]any{"workflow_profile": "poteto", "space_saving_images": on}); err != nil {
			t.Fatal(err)
		}
		p, err := service.Prepare(ctx, t.TempDir(), "", "legacy")
		want := ""
		if on {
			want = workflow.SpaceSavingImageInstructions()
		}
		if err != nil || p.Text != want {
			t.Fatalf("legacy child on=%v: %q %v", on, p.Text, err)
		}
	}
}
