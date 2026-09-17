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
import type { GitFile, GitSnapshot } from "@clawdline/contract"
import { T } from "./js/core/i18n.js"
import { esc } from "./js/core/esc.js"
import { phone } from "./js/core/env.js"

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
export function row(file: GitFile): string {
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
    "</li>"
  )
}

/** `render`, whichever of its three states the panel is in. */
export function bodyHTML(state: { loading: boolean; error: string | null; snapshot: GitSnapshot | null }): string {
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
      : '<ul class="git-files">' + files.map(row).join("") + "</ul>")
  )
}

/** A refusal as `net/fetch.js` hands it on: the code, with the sentence as the message. */
export type GitFailure = Error & { code?: string }

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
