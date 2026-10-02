package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The two things a child is given: a file it reads, and one line typed into its
// composer.
//
// The line is the only place the secret travels. It is not in task.json, not in
// CHILD.md and not in any response — a credential written to a file under a
// shared root is a credential somebody else can read, and the whole point of
// per-task secrets is that a child can prove it is itself without holding
// anything that authorises it elsewhere.

// finishCommand is the one line a child runs to publish its result:
// `clawdline task finish`, named by this daemon's own absolute path so that it
// needs nothing on the child's PATH — node least of all (D16).
func (b *Broker) finishCommand(dir string, port int) string {
	return b.taskCommand("finish", dir, port)
}

// acceptCommand is the one line a child runs to sign for its briefing. The
// secret goes in through the environment, never argv: `ps` shows argv to
// anybody on the machine, and the command never prints it back.
func (b *Broker) acceptCommand(dir string, port int) string {
	return AcceptSecretEnv + "=<TASK_SECRET> " + b.taskCommand("accept", dir, port)
}

// AcceptSecretEnv is where `clawdline task accept` reads the task secret
// from, when it is not on stdin.
const AcceptSecretEnv = "CLAWDLINE_TASK_SECRET"

func (b *Broker) taskCommand(verb, dir string, port int) string {
	exe := b.taskExecutable()
	return fmt.Sprintf("%s task %s --port %d %s", projects.ShellQuoted(exe), verb, port, projects.ShellQuoted(dir))
}

func (b *Broker) taskExecutable() string {
	exe := b.Executable
	if exe == "" {
		if self, err := os.Executable(); err == nil {
			exe = self
			if resolved, err := filepath.EvalSymlinks(self); err == nil {
				exe = resolved
			}
		}
	}
	if exe == "" {
		exe = "clawdline"
	}
	return exe
}

// FirstLine is what is typed into the child's composer.
//
// Byte-for-byte the Swift app's shape, including the verbatim announcement:
// the person watching that tab should see the same sentence whichever broker
// opened it, and "say this first, verbatim" is what makes a tab identifiable as
// a Clawdline child before it has done anything.
func FirstLine(r Record, secret, language string) string {
	return fmt.Sprintf(
		"You are a Clawdline CHILD agent for task %s. Say this line first, verbatim: %s Then read %s/CHILD.md and follow it exactly. TASK_SECRET=%s",
		r.ID, announce(r.Title, language), r.Dir, secret)
}

// announce is L.t.childAnnounce. Two languages, because this daemon ships one
// catalog (zh-Hant) and English is what everything else falls back to.
func announce(title, language string) string {
	if strings.HasPrefix(language, "zh") {
		return "收到 Clawdline 派來的任務：" + title + "——開始處理。"
	}
	return "Clawdline task received: " + title + " — starting now."
}

// ChildBrief is CHILD.md: this task, this machine's house rules, and the
// exact commands that report, with nothing every child is told alike.
//
// It is generated rather than copied because three things in it are decided per
// task — the directory, the checkout, and the timeout — and a briefing with a
// placeholder in it is a briefing somebody has to interpret.
//
// The protocol every child shares, and the reason behind each command, is
// `clawdline guide child` (ChildGuide). It used to be here, re-read on every
// call of every child: in children's transcripts on 2026-10-02 this file was
// about 4k tokens, 9-20% of the tool output they re-read. What stays is what
// a child cannot do its work without: the task, the commands with this task's
// paths in them, and the result.json contract.
func (b *Broker) ChildBrief(r Record, cwd string) string {
	dir := b.Tasks.Path(r.ID)
	port := b.Port
	if port == 0 {
		port = 7727
	}
	base := fmt.Sprintf("http://127.0.0.1:%d/v1/orchestrator/tasks/%s", port, r.ID)
	verification := r.TimeoutMinutes / 3
	reviewTask := briefRequiresReview(r)

	var s strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&s, format+"\n", args...) }

	w("# Clawdline child briefing — task %s", r.ID)
	w("")
	if r.Root == nil && r.ScheduleID != "" {
		// A scheduled task has nobody to report back to (D07): it was started
		// by a clock, and saying "a root session" would send it looking for one.
		w("You are a CHILD session started by the schedule %q on this machine. No session", r.ScheduleTitle)
		w("dispatched you and none is waiting on this tab: `result.json` is how the schedule learns")
		w("you finished. Your one job is the task under \"The task\" below.")
	} else {
		w("You are a CHILD session working for a Clawdline root session. Your one job is the task under")
		w("\"The task\" below.")
	}
	// The daemon's copy is named once, so that a child that knows the old
	// protocol does not go and read it: it was 2.4 reads a session, for bytes
	// this file already carries (measured on 2026-09-26).
	w("%s/task.json holds the same; you need not read it.", dir)
	w("The rules every child follows, and why each command below is shaped as it is, are printed by")
	w("`%s`. Read them once before you start; this file holds only this task.", b.childGuideCommand())
	w("")
	w("## The first thing you say")
	w("")
	w("Speak, and write the `summary`, in the language of the person watching this terminal. First")
	w("say exactly this line on its own, then one line on what you will do and where the output goes:")
	w("")
	w("%s", announce(r.Title, b.Language))
	w("")
	writeTask(w, r)
	w("## Sign for this briefing, before you start the work")
	w("")
	w("```bash")
	w("%s", b.acceptCommand(dir, port))
	w("```")
	w("")
	if r.Gate != nil {
		b.writeGateCheckerBrief(w, r, cwd, dir, port)
		return s.String()
	}
	w("## Where and how long")
	w("")
	w("- Work inside %s. Keep artifacts in %s/artifacts/; put heavyweight temporary", cwd, dir)
	w("  work in %s/work/, which is deleted when the task ends.", dir)
	w("- You have %d minutes on the wall clock from dispatch; verification stops after %d minutes.", r.TimeoutMinutes, verification)
	if r.Worktree != nil {
		w("- A fresh checkout of commit `%s` on branch `%s`.", r.Worktree.Base, r.Worktree.Branch)
		if r.Assistant == "claude" {
			w("  **Commit early and often, on this branch only.** The branch is the delivery.")
		} else {
			w("  **Do not commit**: this sandbox cannot write the worktree's git metadata. The root commits.")
		}
		w("  Do not push, switch branches, rebase, merge, hard-reset, stash, or run any `git worktree` command.")
	}
	w("")

	if local, hasBase := b.childPolicy(); local != "" || hasBase {
		w("## What this machine says")
		w("")
		if local != "" {
			w("House rules the person wrote for this machine. They are the person's, not this app's;")
			w("where they and your own judgement disagree, follow them and say so in your summary.")
			w("")
			w("%s", local)
			w("")
		}
		if hasBase {
			w("How work is handed out here is in %s; read it only if the task asks", filepath.Join(b.Dir, PolicyBaseFile))
			w("you to plan work for others.")
			w("")
		}
	}

	w("## Telling the person, and a changed boundary")
	w("")
	w("Up to 5 notifications, only when the person is waiting on you:")
	w("")
	w("```bash")
	w("curl --fail-with-body -sS -X POST %s/notify \\", base)
	w(`  -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \`)
	w(`  -H 'Content-Type: application/json' \`)
	w(`  -d '{"title":"<at most 80 characters>","body":"<at most 500 characters>"}'`)
	w("```")
	w("")
	w("One note only when the write set, approach, dependency, risk or blocker materially differs")
	w("from this briefing. Without loopback, write the whole file %s/progress.json", dir)
	w(`as {"task_secret": "<TASK_SECRET>", "note": "<the same sentence>"} instead.`)
	w("")
	w("```bash")
	w("curl --fail-with-body -sS -X POST %s/progress \\", base)
	w(`  -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \`)
	w(`  -H 'Content-Type: application/json' \`)
	w(`  -d '{"note":"<one sentence, at most 300 characters>"}'`)
	w("```")
	w("")

	w("## Reporting — this is the completion signal, do it exactly")
	w("")
	w("When the work is done, or has failed for good, write %s/result.json.tmp", dir)
	w("with your file-writing tool, not a shell command:")
	w("")
	w("```json")
	w(`{"clawdline_protocol": 1,`)
	w(` "task_id": %q,`, r.ID)
	w(` "task_secret": "<the TASK_SECRET value from your first message>",`)
	w(` "status": "success",`)
	w(` "summary": "<one paragraph: what you did, or why it failed>",`)
	w(` "symbols": ["<every name your change introduced>", "..."],`)
	w(` "artifacts": ["artifacts/<file>", "..."],`)
	w(` "verification": {"runs": 0, "seconds": 0, "last": "skipped", "scope": "No verification run"},`)
	if reviewTask {
		w(` "review": {"verdict": "safe_to_land", "axes": [`)
		w(`   {"axis": "specification", "status": "pass", "findings": []},`)
		w(`   {"axis": "repository_invariants", "status": "pass", "findings": []},`)
		w(`   {"axis": "runtime_failure_behavior", "status": "pass", "findings": []}]},`)
	}
	w(` "leftovers": [{"title": "<one thing you did not do>", "why": "<why you did not>",`)
	w(`               "suggested_acceptance": "<what would count as done>"}],`)
	w(` "finished_at": "<ISO8601 UTC>"}`)
	w("```")
	w("")
	w(`- "status" is "failure" when you could not do it. `+"`last` is `pass`, `fail` or `skipped`; `scope` is one line of at most %d characters.", taskdir.VerificationScopeLimit)
	w("- `verification` is a placeholder. If you ran nothing, keep `skipped` or omit the field; never claim a pass.")
	if reviewTask {
		w("- A successful review needs `review`. Its verdict is `safe_to_land`, `proceed_with_findings` or `changes_required`.")
		w("  Each of the three named axes has status `pass` with no findings, or `findings` with at least one.")
		w(`  A finding is {"id": "<slug>", "severity": "blocking", "summary": "<one line>", "evidence": ["<file:line or command output>"]}.`)
		w("  `severity` is `blocking` (the work must not go on until it is fixed) or `non_blocking` (worth fixing, does not stop the work).")
		w("  Use `safe_to_land` with no findings, `proceed_with_findings` when every finding is `non_blocking`,")
		w("  and `changes_required` when any finding is `blocking`.")
	}
	w("- `symbols` names what you introduced: functions, types, fields, string keys, test groups.")
	w("- `leftovers`: It is optional and not a new obligation; at most %d entries.", work.LeftoversLimit)
	w("  For each title: %s", work.OutcomeTitleGuide)
	w("  Leave it out when you finished everything you were asked.")
	w("")
	w("Then run this exact command. It checks the file and puts it in place as `result.json`; when the")
	w("check fails it says why and writes nothing, so correct the tmp file and run it again. A non-zero")
	w("exit means the work was not reported.")
	w("")
	w("```bash")
	w("%s", b.finishCommand(dir, port))
	w("```")
	return s.String()
}

// Match the result preflight's review requirement, including graph review nodes.
func briefRequiresReview(r Record) bool {
	if r.Graph != nil {
		if node, ok := r.Graph.node(r.Graph.CurrentNode); ok {
			return node.Kind == "review"
		}
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(r.Kind), func(ch rune) bool {
		return !unicode.IsLetter(ch) && !unicode.IsNumber(ch)
	}) {
		if word == "review" {
			return true
		}
	}
	return false
}

func (b *Broker) writeGateCheckerBrief(w func(string, ...any), r Record, cwd, dir string, port int) {
	g := r.Gate
	scratch := b.gateScratch(r.ID)
	w("## Immutable verification checker")
	w("")
	w("You are an independent, read-only checker. The checkout and this task root are non-writable.")
	w("Only these daemon-created scratch directories are writable:")
	w("")
	w("- build: `%s` (`$CLAWDLINE_GATE_BUILD_DIR`)", scratch.Build)
	w("- cache: `%s` (`$CLAWDLINE_GATE_CACHE_DIR`)", scratch.Cache)
	w("- temporary files: `%s` (`$CLAWDLINE_GATE_TMP_DIR` and `$TMPDIR`)", scratch.Temp)
	w("")
	w("Do not edit, commit, switch, reset, stash or create repository files. Do not write `result.json`;")
	w("ordinary completion files cannot satisfy a verification gate. If loopback is unavailable, no local")
	w("fallback counts: say the gate is unverified and why, restore loopback, and rerun the same command.")
	w("")
	w("## Frozen candidate")
	w("")
	w("- round: `%s`, attempt: `%d`", g.RoundID, g.Attempt)
	w("- repository: `%s`", g.Candidate.Repository)
	w("- source receipt checkout: `%s`", g.Candidate.Worktree)
	w("- checker checkout: `%s`", cwd)
	w("- candidate commit: `%s`", g.Candidate.Commit)
	w("- candidate tree: `%s`", g.Candidate.Tree)
	w("- cycle base commit: `%s`", g.BaseCommit)
	w("- assignment: `%s`; maker: `%s`; cycle: `%d`", g.Candidate.AssignmentID, g.Candidate.OwnerSessionID, g.Candidate.Cycle)
	w("- criteria version: `%d`; SHA-256: `%s`", g.Acceptance.Version, g.Acceptance.Digest)
	w("")
	w("HEAD movement elsewhere has no authority here. Check only the detached commit and tree above.")
	w("")
	w("## Acceptance and required checks")
	w("")
	fence := strings.Repeat("`", max(3, longestRun(g.Acceptance.Criteria, '`')+1))
	w("%stext", fence)
	w("%s", strings.TrimRight(g.Acceptance.Criteria, "\n"))
	w("%s", fence)
	w("")
	w("Evaluate every acceptance claim against the frozen candidate. Run the material checks the claim")
	w("requires and attach bounded evidence. A claim is `passed`, `failed`, or `unverified`; when a check")
	w("cannot run, mark it `unverified` and say why. Never turn missing evidence into a pass.")
	w("")
	w("## Upload evidence")
	w("")
	w("Put an artifact under one of the writable scratch directories, then run:")
	w("")
	w("```bash")
	w("%s=<TASK_SECRET> %s task gate-evidence --port %d --artifact <stable-id> --media-type <media/type> %s <file>",
		AcceptSecretEnv, projects.ShellQuoted(b.taskExecutable()), port, projects.ShellQuoted(dir))
	w("```")
	w("")
	w("At most %d artifacts, %d bytes each and %d bytes total are accepted. Keep each returned artifact id;")
	w("name it in the claim's `evidence_artifacts`. The command derives a stable idempotency key, so rerunning")
	w("the exact command after response loss returns the original durable receipt; changed bytes are refused.",
		contract.WorkGateEvidenceArtifactsPerTaskLimit, contract.WorkGateEvidenceArtifactBytesLimit,
		contract.WorkGateEvidenceTotalBytesPerTaskLimit)
	w("")
	w("## Submit one closed verdict")
	w("")
	w("Write the JSON below under `$CLAWDLINE_GATE_TMP_DIR`, filling every claim. Use `PASS` only when every")
	w("claim passed, `FAIL` when at least one failed, and `NEEDS_WORK` when at least one is unverified.")
	w("")
	w("```json")
	w(`{"round_id":%q,"task_id":%q,"candidate_commit":%q,"candidate_tree":%q,`, g.RoundID, r.ID, g.Candidate.Commit, g.Candidate.Tree)
	w(` "criteria_version":%d,"criteria_digest":%q,"verdict":"PASS|FAIL|NEEDS_WORK",`, g.Acceptance.Version, g.Acceptance.Digest)
	w(` "claims":[{"criterion":"<exact criterion>","state":"passed|failed|unverified",`)
	w(` "evidence":["<short exact observation>"],"evidence_artifacts":["<uploaded id>"],"reason":"<required for failed/unverified>"}],`)
	w(` "summary":"<closed verdict summary>"}`)
	w("```")
	w("")
	w("Then run:")
	w("")
	w("```bash")
	w("%s=<TASK_SECRET> %s task gate-result --port %d %s <json-file>",
		AcceptSecretEnv, projects.ShellQuoted(b.taskExecutable()), port, projects.ShellQuoted(dir))
	w("```")
	w("")
	w("The daemon validates the exact result bytes, frozen identity, referenced durable artifacts, and the")
	w("checkout's unchanged commit/tree before settling. The command succeeds only on a matching durable receipt.")
}

// writeTask is the task itself, as admission validated it: what task.json
// holds, rendered once so the child reads one file.
//
// The instructions go in a fence one backtick longer than the longest run
// inside them, so a fence of their own cannot close it and a heading of
// their own is not one of this briefing's.
func writeTask(w func(string, ...any), r Record) {
	w("## The task")
	w("")
	w("- Title: %s", r.Title)
	w("- Kind: %s", r.Kind)
	w("- Timeout: %d minutes", r.TimeoutMinutes)
	switch writes := r.Claims; {
	case writes == nil:
		w("- Declared writes: none were declared")
	case len(writes) == 0:
		w("- Declared writes: none — this task writes nothing in the repository")
	default:
		w("- Declared writes (claims): %s", strings.Join(quoted(writes), ", "))
	}
	if len(r.Deliverables) > 0 {
		w("- Deliverables: %s", strings.Join(quoted(r.Deliverables), ", "))
	}
	w("")
	w("Instructions:")
	w("")
	fence := strings.Repeat("`", max(3, longestRun(r.Instructions, '`')+1))
	w("%stext", fence)
	w("%s", strings.TrimRight(r.Instructions, "\n"))
	w("%s", fence)
	w("")
}

func quoted(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = "`" + p + "`"
	}
	return out
}

// longestRun is the length of the longest run of c in s.
func longestRun(s string, c byte) int {
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	return longest
}
