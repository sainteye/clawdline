// The Git panel's part of the bridge.
//
// `input/git-panel.js` is not copied: it reaches `els["git-body"]` through
// `core/dom.js`, which looks every id up once at import — before React has
// drawn any of them — and it drives the menu, the shell panel and the terminal
// through the original's composition root, none of which exists here. So its
// four markup builders (`shortened`, `mark`, `row` and the body of `render`,
// lines 26–74) are restated here line for line, with the same escaping and the
// same catalog words, and `session/GitPanel.tsx` owns the state and the
// clicks. If `input/git-panel.js` is ever copied, these go and its exports are
// used instead.
import type { GitDiffReply, GitFile, GitFileDiff, GitSnapshot } from "@clawdline/contract"
import { T } from "./js/core/i18n.js"
import { esc } from "./js/core/esc.js"
import { phone } from "./js/core/env.js"
import { failureSentence as failureSentenceOriginal } from "./js/core/failure-text.js"
import { nextWord } from "../next-strings.js"

const S = T as Record<string, string>
const e = esc as (s: unknown) => string
const narrow = phone as () => boolean

/** `shortened`: a path too long for the row keeps its head and its tail. */
export function shortened(path: string): string {
  path = String(path || "")
  const limit = narrow() ? 34 : 72
  if (path.length <= limit) return path
  const tail = Math.floor(limit * 0.65)
  return path.slice(0, limit - tail - 1) + "…" + path.slice(-tail)
}

/** `mark`: the one or two characters in front of a row, and what they mean. */
export function mark(file: GitFile): { text: string; label: string } {
  if (file.kind === "conflict") return { text: "!", label: S.webGitConflict }
  if (file.kind === "untracked") return { text: "?", label: S.webGitUntracked }
  let text = ""
  const labels: string[] = []
  if (file.staged) {
    text += "+"
    labels.push(S.webGitStaged)
  }
  if (file.unstaged) {
    text += "*"
    labels.push(S.webGitUnstaged)
  }
  return { text: text || "·", label: labels.join(", ") }
}

/**
 * `row`: one file. The counts are drawn only when both are numbers — a file in
 * neither diff has no measurement, and an empty `stats` span is what the
 * original leaves in its place so the row keeps its columns.
 */
export function row(
  file: GitFile,
  expanded = false,
  diffLoading = false,
  diffError: string | null = null,
  diff: GitFileDiff | null = null,
): string {
  const state = mark(file)
  const title = file.from ? String(file.from) + " → " + String(file.path) : String(file.path)
  const hasStats = typeof file.additions === "number" && typeof file.deletions === "number"
  const stats = hasStats
    ? '<span class="stats"><span class="add">+' + e(file.additions) + '</span> <span class="del">−' + e(file.deletions) + "</span></span>"
    : '<span class="stats"></span>'
  return (
    '<li class="git-file" data-kind="' +
    e(file.kind || "modified") +
    '">' +
    '<button class="git-file-toggle" type="button" data-git-path="' + e(file.path) +
    '" aria-expanded="' + (expanded ? "true" : "false") + '">' +
    '<span class="mark" aria-label="' +
    e(state.label) +
    '" title="' +
    e(state.label) +
    '">' +
    e(state.text) +
    "</span>" +
    '<span class="path" title="' +
    e(title) +
    '">' +
    e(shortened(file.path)) +
    "</span>" +
    stats +
    "</button>" +
    (expanded ? diffHTML(diffLoading, diffError, diff) : "") +
    "</li>"
  )
}

function diffHTML(loading: boolean, error: string | null, diff: GitFileDiff | null): string {
  if (loading) return '<div class="git-diff-note" role="status">' + e(S.webLoading) + "</div>"
  if (error) return '<div class="git-diff-note err" role="alert">' + e(error) + "</div>"
  if (!diff || !diff.patches.length) return '<div class="git-diff-note">' + e(S.webGitClean) + "</div>"
  return '<div class="git-diff">' + diff.patches.map((patch) => {
    const label = patch.scope === "staged" ? S.webGitStaged
      : patch.scope === "untracked" ? S.webGitUntracked : S.webGitUnstaged
    const lines = String(patch.unifiedDiff || "").replace(/\n$/, "").split("\n")
    return '<section class="git-patch"><h3>' + e(label) + '</h3><pre>' + lines.map((line) => {
      const kind = line.startsWith("+") && !line.startsWith("+++") ? "add"
        : line.startsWith("-") && !line.startsWith("---") ? "del"
          : line.startsWith("@@") ? "hunk" : "ctx"
      return '<code data-kind="' + kind + '">' + e(line || " ") + "</code>"
    }).join("\n") + "</pre></section>"
  }).join("") + "</div>"
}

/** `render`, whichever of its three states the panel is in. */
export function bodyHTML(state: {
  loading: boolean
  error: string | null
  snapshot: GitSnapshot | null
  openPath?: string | null
  diffLoading?: boolean
  diffError?: string | null
  diff?: GitFileDiff | null
}): string {
  if (state.loading) {
    return '<div class="git-note" role="status">' + e(S.webLoading) + "</div>"
  }
  if (state.error) {
    return '<div class="git-note err" role="alert">' + e(state.error) + "</div>"
  }
  const git = state.snapshot || ({} as Partial<GitSnapshot>)
  const branch = "⎇ " + (git.branch || String(git.head || "").slice(0, 8)) + " ↑" + (git.ahead || 0) + " ↓" + (git.behind || 0)
  const files = git.files || []
  return (
    '<div class="git-branch">' +
    e(branch) +
    "</div>" +
    (git.clean || !files.length
      ? '<div class="git-note">' + e(S.webGitClean) + "</div>"
      : '<ul class="git-files">' + files.map((file) => row(
        file,
        state.openPath === file.path,
        !!state.diffLoading,
        state.diffError || null,
        state.diff || null,
      )).join("") + "</ul>")
  )
}

/** A refusal as `net/fetch.js` hands it on: the code, with the sentence as the message. */
export type GitFailure = Error & { code?: string }

/** `core/failure-text.js`'s `failureSentence`, typed for this module's callers. */
const failureSentence = failureSentenceOriginal as (
  error: unknown,
  fallback?: string | { sentence?: string; fallback?: string },
) => string

/**
 * What the panel says about a failed read, decided by the code and never by
 * the machine's English `message` (`docs/cloud-error-transparency.md` §5
 * rule 1).
 *
 * **Every refusal used to arrive here as one sentence.** The panel branched on
 * `not_a_repo` and sent everything else to `webGitFailed` — "無法讀取 Git
 * 變更" — so a hosted reader was told a read had failed and nothing about why:
 * not that the line was down, not that this Mac was busy, not that this
 * console was refusing its own request. That last one was the case for months
 * (`cloud/carry.ts`, `git` in `DEFERRED`), and it is the shape
 * `docs/work-system-review.md` §3.2 names: **the thing that blocks people is
 * not a strict rule, it is silence.** Three of the four "擋到我" that day were
 * refusals nobody was told about.
 *
 * So: the panel's own two sentences where it has better words than the general
 * catalog, this app's sentence for `cloud_not_carried` — which is this
 * generation's code and is not in the copied catalog, so it fell through to
 * "請求失敗" (`session/Snippets.tsx` took the same turn first) — and otherwise
 * the catalog's own sentence for the code, with `code · ref` after it, which is
 * the pair somebody debugging it will be asked for.
 */
export function gitSentence(failure: unknown, fallback: string): string {
  const code = (failure as { code?: unknown } | null)?.code
  if (code === "not_a_repo") return S.webGitNotRepo
  if (code === "cloud_not_carried") return nextWord("cloudNotCarried")
  return failureSentence(failure, fallback)
}

/**
 * `net/live.js`'s `git`: the open session's repository, read when the panel is
 * opened and never on the event stream. A read, so no Idempotency-Key and no
 * body — asking what a repository has changed changes nothing.
 */
export async function readGit(id: string): Promise<{ git?: GitSnapshot }> {
  let res: Response
  try {
    res = await fetch("/v1/sessions/" + encodeURIComponent(id) + "/git")
  } catch {
    const dead: GitFailure = new Error(S.webOffline)
    dead.code = "offline"
    throw dead
  }
  const text = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const raw = data?.error
    const err: { code?: string; message?: string } =
      typeof raw === "string"
        ? { code: raw, message: typeof data?.detail === "string" ? data.detail : raw }
        : raw && typeof raw === "object"
          ? (raw as { code?: string; message?: string })
          : { code: "http_" + res.status, message: res.statusText || S.webRequestFailed }
    const failure: GitFailure = new Error(err.message || err.code)
    failure.code = err.code
    throw failure
  }
  if (!data) throw new Error(S.webNotJSON)
  return data as { git?: GitSnapshot }
}

/** The patch for one status row. The daemon rechecks that the path is changed. */
export async function readGitDiff(id: string, path: string): Promise<GitDiffReply> {
  let res: Response
  try {
    res = await fetch("/v1/sessions/" + encodeURIComponent(id) + "/git/diff?path=" + encodeURIComponent(path))
  } catch {
    const dead: GitFailure = new Error(S.webOffline)
    dead.code = "offline"
    throw dead
  }
  const text = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const raw = data?.error
    const err = typeof raw === "string"
      ? { code: raw, message: typeof data?.detail === "string" ? data.detail : raw }
      : raw && typeof raw === "object"
        ? raw as { code?: string; message?: string }
        : { code: "http_" + res.status, message: res.statusText || S.webRequestFailed }
    const failure: GitFailure = new Error(err.message || err.code)
    failure.code = err.code
    throw failure
  }
  if (!data) throw new Error(S.webNotJSON)
  return data as unknown as GitDiffReply
}
