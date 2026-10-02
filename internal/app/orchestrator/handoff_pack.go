package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// HandoffInput is what the Board knows about an in-flight item that is being
// moved to a new Session: everything the broker cannot read from its own task
// records. It never travels over the wire; it rides on a RootAssignmentRequest
// under `json:"-"` so the request's digest is the one it always had.
type HandoffInput struct {
	ItemID string
	Title  string
	Phase  string
	// OpenSteps are the item's steps not yet done, in order.
	OpenSteps []string
	// PreviousSession is the conversation that owned the item until now.
	PreviousSession string
	// LastMessage is that Session's last assistant message as the daemon read
	// it from its transcript; LastMessageUnread says why it could not be read.
	// Exactly one is set: unknown is not empty.
	LastMessage       string
	LastMessageUnread string
}

// The pack's bounds. At each the pack says what it left out rather than
// failing: a reassignment never waits on, or fails for, the pack.
const (
	MaxHandoffWorktrees  = 16
	MaxHandoffPatchBytes = 8 << 20
	MaxHandoffPackBytes  = 32 << 20
	handoffGitTimeout    = 20 * time.Second
	handoffLastMessage   = 4096
)

var handoffUnsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// writeHandoffPack assembles <dir>/handoff/HANDOFF.md and its patches from the
// broker's task records and git alone — never by asking the previous owner —
// and returns HANDOFF.md's path. It reads worktrees and never changes them: no
// stash, no add, no checkout, and git runs with optional locks off so not even
// the index's stat cache is rewritten. Whatever part cannot be read is written
// down as unreadable, with why.
func (b *Broker) writeHandoffPack(ctx context.Context, dir string, in HandoffInput) (string, error) {
	pack := filepath.Join(dir, "handoff")
	patches := filepath.Join(pack, "patches")
	if err := os.MkdirAll(patches, 0o700); err != nil {
		return "", err
	}
	var md strings.Builder
	md.WriteString("# Handoff pack for Board item " + in.ItemID + "\n\n")
	md.WriteString("Built by the Clawdline daemon from its task records and git when this item was reassigned. " +
		"Nothing in any worktree was changed to build it; the patches below are copies kept under the broker's " +
		"directory and survive the worktrees being removed.\n\n")
	md.WriteString("## Item\n\n")
	md.WriteString("- Title: " + in.Title + "\n- Phase: " + in.Phase + "\n")
	if len(in.OpenSteps) == 0 {
		md.WriteString("- Open steps: none\n")
	} else {
		md.WriteString("- Open steps:\n")
		for _, s := range in.OpenSteps {
			md.WriteString("  - [ ] " + s + "\n")
		}
	}
	md.WriteString("\n## Previous owner\n\n- Session: " + orUnknown(in.PreviousSession) + "\n")
	if in.LastMessageUnread != "" || in.LastMessage == "" {
		reason := in.LastMessageUnread
		if reason == "" {
			reason = "no message was found"
		}
		md.WriteString("- Last message: unknown / could not read: " + reason + "\n")
	} else {
		msg := in.LastMessage
		if len(msg) > handoffLastMessage {
			msg = msg[:handoffLastMessage] + "\n… (truncated: the message continues in the Session's transcript)"
		}
		md.WriteString("- Last message:\n\n```text\n" + msg + "\n```\n")
	}

	md.WriteString("\n## Tasks bound to this item\n\n")
	records, _, err := b.Records(ctx)
	if err != nil {
		md.WriteString("Could not read the broker's task records: " + err.Error() + "\n")
		return pack, writeFileSync(filepath.Join(pack, "HANDOFF.md"), []byte(md.String()))
	}
	var bound []Record
	for _, r := range records {
		if r.WorkID == in.ItemID || containsString(r.AlsoWorkIDs, in.ItemID) {
			// The list leaves the result's summary in its own table; the
			// one record read puts it back.
			if full, _, err := b.Record(ctx, r.ID); err == nil {
				r = full
			}
			bound = append(bound, r)
		}
	}
	if len(bound) == 0 {
		md.WriteString("None: no task names this item as its work_id.\n")
	}
	total, worktrees, n := 0, 0, 0
	for _, r := range bound {
		md.WriteString("### Task " + r.ID + " — " + r.Title + "\n\n- State: " + string(r.State) + "\n")
		if r.Result != nil {
			md.WriteString("- Result: " + r.Result.Status + " — " + oneParagraph(r.Result.Summary) + "\n")
		} else {
			md.WriteString("- Result: none written\n")
		}
		landing := "not recorded (not landed)"
		if r.Landing != nil {
			landing = string(r.Landing.State)
			if r.Landing.Commit != "" {
				landing += " at " + r.Landing.Commit
			}
		}
		md.WriteString("- Landing: " + landing + "\n")
		wt := r.Worktree
		if wt == nil || wt.Path == "" {
			md.WriteString("- Worktree: none recorded\n\n")
			continue
		}
		md.WriteString("- Worktree: " + wt.Path + "\n- Branch: " + orUnknown(wt.Branch) + "\n- Base: " + orUnknown(wt.Base) + "\n")
		if worktrees >= MaxHandoffWorktrees {
			md.WriteString("- truncated: the pack reads at most " + fmt.Sprint(MaxHandoffWorktrees) + " worktrees; read this one by hand\n\n")
			continue
		}
		worktrees++
		if _, err := os.Stat(wt.Path); err != nil {
			md.WriteString("- Worktree could not be read: " + err.Error() + "\n\n")
			continue
		}
		head, err := handoffGit(ctx, wt.Path, "rev-parse", "HEAD")
		if err != nil {
			md.WriteString("- HEAD could not be read: " + err.Error() + "\n\n")
			continue
		}
		head = strings.TrimSpace(head)
		md.WriteString("- HEAD: " + head + "\n")
		if r.Landing == nil || r.Landing.State != LandingLanded {
			if wt.Base != "" {
				log, err := handoffGit(ctx, wt.Path, "log", "--format=%H %s", wt.Base+"..HEAD")
				switch {
				case err != nil:
					md.WriteString("- Unlanded commits could not be read: " + err.Error() + "\n")
				case strings.TrimSpace(log) == "":
					md.WriteString("- Unlanded commits: none past the base\n")
				default:
					md.WriteString("- Unlanded commits on " + orUnknown(wt.Branch) + ":\n")
					for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
						md.WriteString("  - " + l + "\n")
					}
				}
			}
		}
		patch, perr := handoffPatch(ctx, wt.Path)
		if perr != nil {
			md.WriteString("- Uncommitted changes could not be read: " + perr.Error() + "\n\n")
			continue
		}
		if len(patch) == 0 {
			md.WriteString("- Uncommitted changes: none\n\n")
			continue
		}
		if len(patch) > MaxHandoffPatchBytes || total+len(patch) > MaxHandoffPackBytes {
			md.WriteString(fmt.Sprintf("- truncated: the uncommitted changes are %d bytes, over the pack's bound "+
				"(%d per worktree, %d in all); no patch was saved, read the worktree by hand\n\n",
				len(patch), MaxHandoffPatchBytes, MaxHandoffPackBytes))
			continue
		}
		n++
		name := fmt.Sprintf("%d-%s.patch", n, handoffUnsafeName.ReplaceAllString(filepath.Base(wt.Path), "_"))
		file := filepath.Join(patches, name)
		if err := writeFileSync(file, patch); err != nil {
			md.WriteString("- The patch could not be saved: " + err.Error() + "\n\n")
			continue
		}
		total += len(patch)
		sum := sha256.Sum256(patch)
		md.WriteString("- Uncommitted changes (tracked and untracked): " + file + "\n")
		md.WriteString("  - sha256: " + hex.EncodeToString(sum[:]) + "\n")
		md.WriteString("  - re-apply on a checkout of " + head + ": `git apply --binary '" + file + "'`\n\n")
	}
	return pack, writeFileSync(filepath.Join(pack, "HANDOFF.md"), []byte(md.String()))
}

// handoffPatch is the worktree's uncommitted state against HEAD as one patch:
// tracked changes, then each untracked file as a new file. It reads only.
func handoffPatch(ctx context.Context, wt string) ([]byte, error) {
	tracked, err := handoffGit(ctx, wt, "diff", "--binary", "HEAD")
	if err != nil {
		return nil, err
	}
	out := []byte(tracked)
	list, err := handoffGit(ctx, wt, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, f := range strings.Split(list, "\x00") {
		if f == "" {
			continue
		}
		// --no-index exits 1 when the files differ, which a new file always does.
		d, err := handoffGitStatus(ctx, wt, 1, "diff", "--binary", "--no-index", "--", "/dev/null", f)
		if err != nil {
			return nil, err
		}
		out = append(out, d...)
		if len(out) > MaxHandoffPatchBytes {
			return out, nil
		}
	}
	return out, nil
}

func handoffGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := handoffGitStatus(ctx, dir, 0, args...)
	return string(out), err
}

// handoffGitStatus runs git read-only in dir, accepting exit status ok too.
func handoffGitStatus(ctx context.Context, dir string, ok int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, handoffGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exit, isExit := err.(*exec.ExitError); isExit && ok != 0 && exit.ExitCode() == ok {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// HandoffSection is what ASSIGNMENT.md says about a pack: read it first.
func HandoffSection(path string, err error) string {
	if err != nil {
		return "HANDOFF\nThis item was taken over from another Session. The daemon tried to build a handoff pack at " +
			path + " and could not finish it: " + err.Error() + ". Read what is there, then look for the previous " +
			"Session's branch and worktrees yourself before starting over.\n\n"
	}
	return "HANDOFF\nThis item was taken over from another Session. Read " + filepath.Join(path, "HANDOFF.md") +
		" first, before planning: it lists the tasks bound to this item and their results, the commits not yet " +
		"landed, every worktree's uncommitted changes saved as patches with their sha256 and how to re-apply them, " +
		"the previous owner's last message, and the phase and open steps. Continue from there.\n\n"
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func oneParagraph(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "(empty summary)"
	}
	return s
}
