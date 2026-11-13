/**
 * Where the voice copilot last reported each Session up to, so "what happened
 * since" reads only what came after (`get_session_updates`). Kept by this
 * browser, like the call memory beside it: a per-viewer convenience, which a
 * cleared or private browser simply starts again from the recent part.
 */

import type { UpdateMark } from "./copilot-tools.js"

/** How many Sessions' marks this browser keeps; the least recently saved is dropped. */
export const UPDATE_MARKS = 100

const KEY = 'clawdline.copilot.reportMarks'

/** `marks` with `key` set to `mark`, keeping only the newest `limit` by when each was saved. */
export function withMark(marks: Record<string, UpdateMark>, key: string, mark: UpdateMark, limit = UPDATE_MARKS): Record<string, UpdateMark> {
  const next = { ...marks, [key]: mark }
  const keys = Object.keys(next).sort((a, b) => (next[b].saved || 0) - (next[a].saved || 0))
  return Object.fromEntries(keys.slice(0, limit).map((k) => [k, next[k]]))
}

function readAll(): Record<string, UpdateMark> {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '{}')
    return raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, UpdateMark> : {}
  } catch {
    // refusal-ok: unreadable or blocked storage starts every Session from its recent part.
    return {}
  }
}

export const browserMarks = {
  get(key: string): UpdateMark | undefined { return readAll()[key] },
  set(key: string, mark: UpdateMark): void {
    try {
      localStorage.setItem(KEY, JSON.stringify(withMark(readAll(), key, mark)))
    } catch {
      // refusal-ok: a mark that cannot be kept only means the next report repeats some of this one.
    }
  },
}
