package orchestrator

import (
	"fmt"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// ChildGuideTopic is the part of `clawdline guide` that prints ChildGuide.
const ChildGuideTopic = "child"

// childGuideCommand is the line CHILD.md points at, named by this daemon's
// own path for the same reason the finishing line is: a child's PATH may not
// have `clawdline` on it.
func (b *Broker) childGuideCommand() string {
	return projects.ShellQuoted(b.taskExecutable()) + " guide " + ChildGuideTopic
}

// ChildGuide is the protocol every child follows, and the reason behind each
// command its CHILD.md names. It holds nothing particular to one task: the
// paths, the timeout, the tab rule and the house rules are in CHILD.md, which
// is read on every call, and this is read once.
func ChildGuide() string {
	var s strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&s, format+"\n", args...) }
	w("# How a Clawdline child works")
	w("")
	w("Your CHILD.md holds your task and the exact commands with your task's paths in them. This is")
	w("what every child is told alike, and why. Where your CHILD.md or your task says otherwise, it wins.")
	w("")
	w("## Language")
	w("")
	w("CHILD.md is in English only so that every assistant reads it the same way. Everything you say,")
	w("and the `summary` in result.json, is in the language the person watching your terminal reads.")
	w("Your first line is the announcement CHILD.md quotes, verbatim: it is how the person tells a")
	w("Clawdline child's tab from any other before it has done anything.")
	w("")
	w("## Signing for the briefing")
	w("")
	w("Run the accept command once, before the work. It is the only proof the briefing reached you. The")
	w("secret goes in through the environment, never argv, because `ps` shows argv to anybody on the")
	w("machine. When the broker cannot be reached it leaves the receipt in the task directory for the")
	w("broker to collect.")
	w("")
	w("## Rules")
	w("")
	w("- **You are the bottom of this tree: you cannot dispatch Clawdline tasks of your own.** When part")
	w("  of the work needs to run in parallel, use your own assistant's built-in subagents.")
	w("- Do not read any task directory but your own.")
	w("- Landing records belong to the root after delivery; a child does not call its own `/landing`.")
	w("- Do not do work the task did not ask for.")
	w("- Heavyweight temporary work — repository copies, build outputs, compiler indexes — goes in the")
	w("  task's `work/`, not your assistant's scratchpad. `work/` is deleted when the task ends, so copy")
	w("  anything worth keeping into `artifacts/` **before** writing `result.json`.")
	w("- The timeout is counted on the wall clock from when the task was dispatched, not from when you")
	w("  read CHILD.md.")
	w("")
	w("## Your checkout")
	w("")
	w("A dispatched checkout is fresh: uncommitted files from the base repository are deliberately")
	w("absent, and so is anything gitignore hides — dependencies, build caches, local environment files.")
	w("When CHILD.md says to commit, commit early and often on your branch only: the branch is the")
	w("delivery, and uncommitted changes can be lost when the checkout is cleaned. When it says not to,")
	w("a linked worktree's git metadata lives outside what your sandbox may write, so every commit fails")
	w("on `index.lock`; leave the changes in the working tree and the root commits them.")
	w("")
	w("## Verification budget")
	w("")
	w("Do the cheapest verification pass that materially reduces the risk of the whole delivery.")
	w("Accumulate related edits first, then compile and run the relevant groups once near the end.")
	w("Verification stops after one third of the task's timeout, the minutes CHILD.md names. At the")
	w("limit, stop and report the state reached in `result.json`.")
	w("")
	w("## Your tab when the task ends")
	w("")
	w("The broker closes a finished child's tab by the one rule CHILD.md states, and only while the tab")
	w("is still the one it opened, is at rest at its prompt, and nobody has used it since the task ended.")
	w("")
	w("## Notifications and progress notes")
	w("")
	w("A notification is for a person who is waiting on you and has a deadline: a choice only they can")
	w("make, a credential about to expire. Progress, \"done\" and anything you can carry on with are not.")
	w("")
	w("**`--fail-with-body` is not decoration.** Plain `curl` exits 0 whatever the server says, so a `403`")
	w("from a stale secret and a `200` look identical. With the flag the typed error prints and the")
	w("command exits non-zero. Look at that status before you say you sent something.")
	w("")
	w("Do not echo a clear task back as a progress note and do not send heartbeats. A note is one")
	w("sentence, sent only when the write set, approach, dependency, risk or blocker materially differs")
	w("from the briefing. When `curl` cannot connect — some sandboxes have no loopback — write the task's")
	w("`progress.json` instead, replacing the whole file each time; the broker collects it.")
	w("")
	w("## result.json")
	w("")
	w("`result.json` is the only completion signal. Write `result.json.tmp` with the JSON CHILD.md shows,")
	w("then run its finish command. The command checks the file, puts it in place as `result.json` and")
	w("asks the broker to collect it, with nothing but the daemon's own binary — no node, nothing else on")
	w("your PATH. When the check fails it says why and writes nothing: correct the tmp file and run the")
	w("same command again. Asking the broker to collect it is a courtesy, so when the broker cannot be")
	w("reached the command still exits 0 and the work is already reported. A non-zero exit means it was not.")
	w("")
	w("- `status` is `success` or `failure`; `failure` when you could not do it.")
	w("- `verification.last` is `pass`, `fail` or `skipped`. `scope` names what you ran in one line of at")
	w("  most %d characters; the check refuses a longer one.", taskdir.VerificationScopeLimit)
	w("- **`symbols` is how your work is told apart from everybody else's.** List what you introduced:")
	w("  new functions and types, new fields, new string keys, the names of test groups you added.")
	w("  Names, not descriptions.")
	w("- **`leftovers` is the paragraph you were going to write anyway.** Your report ends in what you did")
	w("  not do and what your root has to pick up; put those lines here too, one entry each, at most %d.", work.LeftoversLimit)
	w("  It is optional and not a new obligation: a result with no `leftovers` is a complete delivery,")
	w("  and writing one does not make anything happen by itself. Your root reads them when it integrates")
	w("  and may put one to the person, who answers whether to register it. For each title: %s", work.OutcomeTitleGuide)
	w("")
	w("**Write the tmp file with your file-writing tool, not with a shell command.** A shell line that")
	w("builds JSON gets refused by command screening on its own shape, and that refusal is a prompt with")
	w("no \"always allow\" on a tab nobody is watching.")
	return s.String()
}
