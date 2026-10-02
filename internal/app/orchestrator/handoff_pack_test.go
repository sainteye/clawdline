package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func packGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// snapshot is every file under dir but .git, by relative path.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == ".git" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			out[rel] = string(b)
		}
		return nil
	})
	return out
}

// An in-flight item moved to a new Session: ASSIGNMENT.md points at a pack
// that lists the bound task and its result, the commit its branch has not
// landed, the dirty worktree as a patch whose sha256 matches and which
// re-applies byte for byte onto its HEAD, the previous owner's last message,
// the phase and the open steps. The worktree is untouched.
func TestATakeoverOpensWithAHandoffPack(t *testing.T) {
	b, ctx := newTestBroker(t)
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Launcher = &recordingLauncher{pane: "%66"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%66", Assistant: session.AssistantClaude}}
	}

	repo := t.TempDir()
	packGit(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "gone.txt"), []byte("bye\n"), 0o644)
	packGit(t, repo, "add", ".")
	packGit(t, repo, "commit", "-qm", "base")
	base := packGit(t, repo, "rev-parse", "HEAD")
	wt := filepath.Join(t.TempDir(), "child")
	packGit(t, repo, "worktree", "add", "-q", "-b", "clawdline/task/x", wt)
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("committed on the branch\n"), 0o644)
	packGit(t, wt, "add", "b.txt")
	packGit(t, wt, "commit", "-qm", "Unlanded work")
	head := packGit(t, wt, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.Remove(filepath.Join(wt, "gone.txt"))
	os.MkdirAll(filepath.Join(wt, "new"), 0o755)
	os.WriteFile(filepath.Join(wt, "new", "untracked.bin"), []byte{0, 1, 2, 255}, 0o644)
	os.WriteFile(filepath.Join(wt, "new", "note.txt"), []byte("untracked\n"), 0o644)
	before := snapshot(t, wt)
	statusBefore := packGit(t, wt, "status", "--porcelain")

	item := "0f0f0f0f-0000-4000-8000-0000000000b1"
	r := Record{Protocol: Protocol, ID: "7a5c0000-0000-4000-8000-0000000000aa", Assistant: "codex", Title: "Child work",
		State: StateSuccess, CreatedAt: time.Now(), Claims: []string{}, WorkID: item, ProjectDir: repo,
		Worktree: &Worktree{Repository: repo, Path: wt, Branch: "clawdline/task/x", Base: base},
		Result:   &taskdir.Result{Status: "failure", Summary: "Stopped: the weekly quota ran out."}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}

	req := RootAssignmentRequest{RequestID: "c6500004-0000-4000-8000-000000000004", Assistant: "claude",
		ProjectDir: repo, Label: "Item", Assignment: Assignment{Objective: "o", Scope: "s", Constraints: "c",
			RelevantReferences: "r", Acceptance: "a"},
		Handoff: &HandoffInput{ItemID: item, Title: "Item", Phase: "verifying", OpenSteps: []string{"Write the test"},
			PreviousSession: "prev-conv", LastMessageUnread: "its terminal %9 is gone"}}
	digest := assignmentDigest(req)
	a, _, err := b.OpenRootAssignment(ctx, req.RequestID, req)
	if err != nil || a.State != AssignmentBriefed {
		t.Fatalf("%+v %v", a, err)
	}
	req.Handoff = nil
	if assignmentDigest(req) != digest {
		t.Fatal("the handoff changed the request's digest")
	}

	brief, _ := os.ReadFile(a.BriefPath)
	packFile := filepath.Join(filepath.Dir(a.BriefPath), "handoff", "HANDOFF.md")
	if !strings.Contains(string(brief), "HANDOFF\n") || !strings.Contains(string(brief), packFile) ||
		strings.Index(string(brief), "HANDOFF\n") > strings.Index(string(brief), "OBJECTIVE\n") {
		t.Fatalf("ASSIGNMENT.md does not point to the pack first:\n%s", brief)
	}
	md, err := os.ReadFile(packFile)
	if err != nil {
		t.Fatal(err)
	}
	pack := string(md)
	for _, want := range []string{r.ID, "Stopped: the weekly quota ran out.", head + " Unlanded work",
		"Phase: verifying", "- [ ] Write the test", "unknown / could not read: its terminal %9 is gone", "HEAD: " + head} {
		if !strings.Contains(pack, want) {
			t.Errorf("HANDOFF.md lacks %q:\n%s", want, pack)
		}
	}
	m := regexp.MustCompile(`(?m)^- Uncommitted changes \(tracked and untracked\): (\S+)\n  - sha256: ([0-9a-f]{64})`).FindStringSubmatch(pack)
	if m == nil {
		t.Fatalf("HANDOFF.md names no patch:\n%s", pack)
	}
	patch, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(patch); hex.EncodeToString(sum[:]) != m[2] {
		t.Fatal("the patch's sha256 is not the one HANDOFF.md lists")
	}

	if got := snapshot(t, wt); !equalMaps(got, before) || packGit(t, wt, "status", "--porcelain") != statusBefore {
		t.Fatal("building the pack changed the worktree")
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	packGit(t, repo, "worktree", "add", "-q", "--detach", fresh, head)
	packGit(t, fresh, "apply", "--binary", m[1])
	if got := snapshot(t, fresh); !equalMaps(got, before) {
		t.Fatalf("the patch did not re-apply to the dirty tree:\n got %v\nwant %v", got, before)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
