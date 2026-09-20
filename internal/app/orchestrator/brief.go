package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/domain/work"
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
	return fmt.Sprintf("%s task finish --port %d %s", projects.ShellQuoted(exe), port, projects.ShellQuoted(dir))
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

// ChildBrief is CHILD.md: the protocol, this machine's house rules, and the
// exact commands that report.
//
// It is generated rather than copied because three things in it are decided per
// task — the directory, the checkout, and the timeout — and a briefing with a
// placeholder in it is a briefing somebody has to interpret.
func (b *Broker) ChildBrief(r Record, cwd string) string {
	dir := b.Tasks.Path(r.ID)
	port := b.Port
	if port == 0 {
		port = 7727
	}
	base := fmt.Sprintf("http://127.0.0.1:%d/v1/orchestrator/tasks/%s", port, r.ID)
	verification := r.TimeoutMinutes / 3

	var s strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&s, format+"\n", args...) }

	w("# Clawdline child briefing — task %s", r.ID)
	w("")
	if r.Root == nil && r.ScheduleID != "" {
		// A scheduled task has nobody to report back to (D07): it was started
		// by a clock, and saying "a root session" would send it looking for one.
		w("You are a CHILD session started by the schedule %q on this machine. No session", r.ScheduleTitle)
		w("dispatched you and none is waiting on this tab: `result.json` is how the schedule learns")
		w("you finished. Your one job is the task described in %s/task.json — read that file now.", dir)
	} else {
		w("You are a CHILD session working for a Clawdline root session. Your one job is the task")
		w("described in %s/task.json — read that file now.", dir)
	}
	w("")
	w("## Language, and the first thing you say")
	w("")
	w("Everything you say in this session, and the `summary` you write into result.json, is in the")
	w("language the person watching this terminal reads. This briefing is in English only so that")
	w("every assistant reads it the same way.")
	w("")
	w("Before you read task.json or touch anything, say exactly this line, on its own:")
	w("")
	w("%s", announce(r.Title, b.Language))
	w("")
	w("Then, once you have read task.json, one more line saying in your own words what you are")
	w("about to do and where the output will go.")
	w("")
	w("## Sign for this briefing, before you start the work")
	w("")
	w("Once you have read task.json, tell the broker you have it. This receipt is the only thing")
	w("that proves the briefing reached you: typing it into your terminal proved nothing, and your")
	w("tab starting a turn proves only that something is running. Send it once; a repeat is harmless.")
	w("")
	w("```bash")
	w("curl --fail-with-body -sS -X POST %s/accepted \\", base)
	w(`  -H "X-Clawdline-Task-Secret: <TASK_SECRET>"`)
	w("```")
	w("")
	w("If that `curl` cannot connect — some sandboxes have no loopback — write %s/accepted.json", dir)
	w("instead, and the broker collects it:")
	w("")
	w("```json")
	w(`{"task_secret": "<the TASK_SECRET value from your first message>"}`)
	w("```")
	w("")
	w("## Rules")
	w("")
	w("- Work inside %s. Put non-repository artifacts in %s/artifacts/.", cwd, dir)
	w("- Put heavyweight temporary work — repository copies, build outputs, compiler indexes — in")
	w("  %s/work/, not in your assistant's scratchpad. Everything there is deleted when the task", dir)
	w("  ends, so copy anything worth keeping into `artifacts/` **before** writing `result.json`.")
	w("- **You are the bottom of this tree: you cannot dispatch Clawdline tasks of your own.** When")
	w("  part of this needs to run in parallel, use your own assistant's built-in subagents.")
	w("- Do not read any task directory but your own.")
	w("- Landing records belong to the root after delivery; a child does not call its own `/landing`.")
	w("- Do not do work the task did not ask for.")
	w("- You have %d minutes before the task is marked timed out, counted on the wall clock from", r.TimeoutMinutes)
	w("  when this task was dispatched — not from when you read this.")
	w("")

	// The tab policy, stated before the work starts (linger.go): what this
	// task's end will do to this tab is read here, not inferred later.
	w("## What happens to this tab when the task ends")
	w("")
	w("The broker closes a finished child's tab by one rule, and only while the tab is still the one")
	w("it opened, is at rest at its prompt, and nobody has used it since the task ended.")
	for _, line := range tabPolicyBrief(r, b.childLinger()) {
		w("%s", line)
	}
	w("")

	if r.Worktree != nil {
		w("## Your isolated checkout")
		w("")
		w("This is a fresh checkout of commit `%s` on branch `%s`.", r.Worktree.Base, r.Worktree.Branch)
		w("Uncommitted files from the base repository are deliberately absent, and so is anything")
		w("gitignore hides — dependencies, build caches, local environment files.")
		w("")
		if r.Assistant == "claude" {
			w("**Commit early and often, on this branch only.** The branch is the delivery, and")
			w("uncommitted changes can be lost when the checkout is cleaned.")
		} else {
			w("**Do not commit.** A linked worktree's git metadata lives outside what this sandbox may")
			w("write, so every commit here fails on `index.lock`. Leave the changes in the working")
			w("tree; the root commits them.")
		}
		w("Do not push, switch branches, rebase, merge, hard-reset, stash, or run any `git worktree`")
		w("command.")
		w("")
	}

	if policy := b.policy(); policy != "" {
		w("## What this machine says")
		w("")
		w("House rules from this machine's dispatch policy. They are the person's, not this app's;")
		w("where they and your own judgement disagree, follow them and say so in your summary.")
		w("")
		w("%s", policy)
		w("")
	}

	w("## Verification budget")
	w("")
	w("Do the cheapest verification pass that materially reduces the risk of the whole delivery.")
	w("Accumulate related edits first, then compile and run the relevant groups once near the end.")
	w("Verification stops after one third of this task's timeout (%d minutes). At the limit, stop", verification)
	w("and report the state reached in `result.json`.")
	w("")

	w("## Up to 5 timely notifications, when the user is waiting")
	w("")
	w("```bash")
	w("curl --fail-with-body -sS -X POST %s/notify \\", base)
	w(`  -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \`)
	w(`  -H 'Content-Type: application/json' \`)
	w(`  -d '{"title":"<at most 80 characters>","body":"<at most 500 characters>"}'`)
	w("```")
	w("")
	w("**`--fail-with-body` is not decoration.** Plain `curl` exits 0 whatever the server says, so a")
	w("`403` from a stale secret and a `200` look identical. With the flag the typed error prints and")
	w("the command exits non-zero. Look at that status before you say you sent something.")
	w("")

	w("## Report only a material boundary change")
	w("")
	w("Do not echo a clear `task.json` back as a progress message and do not send heartbeats. Send")
	w("one short note only when the write set, approach, dependency, risk or blocker materially")
	w("differs from the briefing.")
	w("")
	w("```bash")
	w("curl --fail-with-body -sS -X POST %s/progress \\", base)
	w(`  -H "X-Clawdline-Task-Secret: <TASK_SECRET>" \`)
	w(`  -H 'Content-Type: application/json' \`)
	w(`  -d '{"note":"<one sentence, at most 300 characters>"}'`)
	w("```")
	w("")
	w("If that `curl` cannot connect — some sandboxes have no loopback — write %s/progress.json", dir)
	w("instead, replacing the whole file each time, and the broker collects it:")
	w("")
	w("```json")
	w(`{"task_secret": "<the TASK_SECRET value from your first message>",`)
	w(` "note": "<one sentence, at most 300 characters>"}`)
	w("```")
	w("")

	w("## Before you start work you believe is new, look")
	w("")
	w("Another session's isolated checkout is invisible from the shared tree: a finished delivery")
	w("sitting on a branch nobody merged shows up in no `git status` and no file listing. So")
	w(`"nothing here does that yet" is not evidence. This is:`)
	w("")
	w("```bash")
	w("curl --fail-with-body -sS %s/inflight \\", base)
	w(`  -H "X-Clawdline-Task-Secret: <TASK_SECRET>"`)
	w("```")
	w("")
	w("**If this one fails you have no answer, which is not the same as an empty board.** On a")
	w("non-zero exit, say in your result what you checked and what it told you.")
	w("")

	w("## Reporting — this is the completion signal, do it exactly")
	w("")
	w("When the work is done, or has failed for good, first write %s/result.json.tmp:", dir)
	w("")
	w("```json")
	w(`{"clawdline_protocol": 1,`)
	w(` "task_id": %q,`, r.ID)
	w(` "task_secret": "<the TASK_SECRET value from your first message>",`)
	w(` "status": "success",`)
	w(` "summary": "<one paragraph: what you did, or why it failed>",`)
	w(` "symbols": ["<every name your change introduced>", "..."],`)
	w(` "artifacts": ["artifacts/<file>", "..."],`)
	w(` "verification": {"runs": 1, "seconds": 0, "last": "pass", "scope": "<what you ran>"},`)
	w(` "leftovers": [{"title": "<one thing you did not do>", "why": "<why you did not>",`)
	w(`               "suggested_acceptance": "<what would count as done>"}],`)
	w(` "finished_at": "<ISO8601 UTC>"}`)
	w("```")
	w("")
	w(`Use "status": "failure" when you could not do it. Then run this exact command. It checks the`)
	w("file, puts it in place as `result.json` and asks the broker to collect it, with nothing but this")
	w("daemon's own binary — no node, nothing else on your PATH:")
	w("")
	w("```bash")
	w("%s", b.finishCommand(dir, port))
	w("```")
	w("")
	w("If the check fails it says why and writes nothing: correct the tmp file and run the same command")
	w("again. `result.json` remains the only completion signal. Asking the broker to collect it now is a")
	w("courtesy, so when the broker cannot be reached — some sandboxes have no loopback — the command")
	w("still exits 0 and the work is already reported. A non-zero exit means it was not.")
	w("")
	w("**`symbols` is how your work is told apart from everybody else's.** List what you introduced:")
	w("new functions and types, new fields, new string keys, the names of test groups you added.")
	w("Names, not descriptions.")
	w("")
	w("**`leftovers` is the paragraph you were going to write anyway.** Your report ends in what you")
	w("did not do and what your root has to pick up; put those lines here too, one entry each, at")
	w("most %d. It is optional and it is not a new obligation: a result with no `leftovers` is a", work.LeftoversLimit)
	w("complete delivery, and writing one does not make anything happen by itself. Your root reads")
	w("them when it integrates and may put one to the person, who answers whether to register it.")
	w("Leave it out when you finished everything you were asked.")
	w("")
	w("**Write the tmp file with your file-writing tool, not with a shell command.** A shell line")
	w("that builds JSON gets refused by command screening on its own shape, and that refusal is a")
	w("prompt with no \"always allow\" on a tab nobody is watching.")
	return s.String()
}

var _ = strconv.Itoa
