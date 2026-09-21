import type { ProjectDeployQuiet, ProjectGitFailure, ProjectRepository } from "@clawdline/contract"
import type { nextWord } from "../next-strings.js"

/**
 * The sentence under the Links section: **which kind of nothing** this project
 * has instead of a deploy row.
 *
 * It is a module of its own rather than four functions inside `info.ts`
 * because `info.ts` is one `innerHTML` over a live document and cannot be
 * loaded by `node --test`, and this is the part with the decisions in it. The
 * catalog and the clock arrive as arguments for the same reason every other
 * module under test here imports only types from its siblings: a runtime
 * `.js` import of a `.ts` file resolves under Vite and not under node.
 *
 * The distinction is the whole reason the daemon answers `repository` at all:
 * a directory that is not a repository, one with no `origin`, one whose
 * `origin` is elsewhere and a git that would not answer all produce no deploy
 * row, and only the last leaves it unknown whether there was one to produce.
 * "No links" for all four tells somebody their project has no CI when what
 * happened is that git is wedged.
 *
 * **There was a fifth kind and it had no sentence.** A repository that *is* on
 * GitHub, read by a git that answered, whose workflow poller still produced
 * nothing fell off the end of the switch and returned `""` — so the most
 * ordinary setup of all got the blank cell. Asked about twice on 2026-09-21,
 * and both times the answer was that a named reason existed in a file
 * (`{"state":"none","why":"stale-fail"}`) and was thrown away between the file
 * and the screen. `deployQuiet` is that reason arriving, and `github` is no
 * longer the case with no words.
 */

/** What this module needs from the page, and the whole of it. */
export interface Say {
  /** The catalog, `next-strings.ts`'s own `nextWord`. */
  word: typeof nextWord
  /** A moment as a person reads it, `legacy/bridge.ts`'s `clock`. */
  clock: (unix: number) => string
}

export function repositoryNote(
  repo: ProjectRepository | undefined,
  failure: ProjectGitFailure | undefined,
  quiet: ProjectDeployQuiet | undefined,
  say: Say,
): string {
  switch (repo) {
    case "no_remote":
      return say.word("linksNoRemote")
    case "not_a_repository":
      return say.word("linksNotRepository")
    case "remote_not_github":
      return say.word("linksRemoteNotGitHub")
    case "unreadable":
      return say.word("linksGitUnreadable", { reason: gitFailureWord(failure, say) })
    case "github":
      return deployQuietNote(quiet, say)
  }
  return ""
}

export function gitFailureWord(failure: ProjectGitFailure | undefined, say: Say): string {
  switch (failure) {
    case "git_missing":
      return say.word("linksGitMissing")
    case "git_timeout":
      return say.word("linksGitTimeout")
    case "git_answer_too_large":
      return say.word("linksGitTooLarge")
  }
  return say.word("linksGitFailed")
}

/**
 * What the workflow file said on a beat it drew no row.
 *
 * `undefined` is a deploy row that *was* drawn, and then there is nothing to
 * explain — the row is the answer. Everything else is a sentence, including
 * the two kinds where nothing was read: "nobody is looking" and "there is no
 * run" are the pair this section exists to keep apart.
 */
export function deployQuietNote(quiet: ProjectDeployQuiet | undefined, say: Say): string {
  if (!quiet) return ""
  const at = quiet.updatedAt ? say.clock(quiet.updatedAt) : ""
  const when = at ? " " + say.word("linksDeployWhen", { when: at }) : ""
  const holes = { state: quiet.state || "", reason: deployWhyWord(quiet.why, say) }
  switch (quiet.kind) {
    case "no_file":
      return say.word("linksDeployNoFile")
    case "unreadable":
      return say.word("linksDeployUnreadable")
    case "state_not_drawn":
      return say.word("linksDeployNoRun", holes) + when
    case "no_address":
      return say.word("linksDeployNoPage", holes) + when
  }
  // A kind this build has no word for is still a named kind, and saying it is
  // better than the blank cell this whole file exists to end.
  return say.word("linksDeployWhyUnknown", { why: String(quiet.kind) })
}

/**
 * The producer's own reason as a sentence. **The vocabulary is not this app's
 * and is not closed**: these six are what `gh-run-status.py` writes today, that
 * tool is free to learn a seventh tomorrow, and the fall-through says the word
 * it was given rather than dropping it — which is the difference between this
 * list being a translation and it being a filter.
 */
export function deployWhyWord(why: string | undefined, say: Say): string {
  const because = (reason: string) => say.word("linksDeployBecause", { reason })
  switch (why) {
    case undefined:
    case "":
      return say.word("linksDeployNoWhy")
    case "no-gh":
      return because(say.word("linksDeployWhyNoGh"))
    case "no-branch":
      return because(say.word("linksDeployWhyNoBranch"))
    case "gh-failed":
      return because(say.word("linksDeployWhyGhFailed"))
    case "no-runs":
      return because(say.word("linksDeployWhyNoRuns"))
    case "workflow-disabled":
      return because(say.word("linksDeployWhyWorkflowDisabled"))
    case "stale-fail":
      return because(say.word("linksDeployWhyStaleFail"))
  }
  return because(say.word("linksDeployWhyUnknown", { why }))
}
