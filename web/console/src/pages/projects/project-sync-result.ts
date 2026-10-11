// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { localizedLiteralMap } from "../../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat } from "../../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { clauseSeparator, labelled } from "../../punctuation.ts"

// What one applied project says on the mirror's page (docs/project-sync.md),
// computed from the machine's answer and nothing else, so it can be tested as
// data.
//
// The answer is read defensively on purpose. A machine whose Go build still
// sends a nil slice spells an empty list `null`, and on 2026-10-11 eighteen
// projects a mirror did not have were each answered 200 with
// `"written":null`: counting them threw inside the row, and every line read
// "無法完成專案同步，請重試。（unexpected_error）" — the page's own
// TypeError wearing the sentence for a failure that never happened. A page
// that draws a successful answer must never fail on the shape of one.

const STATE_WORDS: Record<string, string> = localizedLiteralMap({
  applied: "3d74d47e0b33",
  unchanged: "860ee16cc39a",
  missing: "ea1537ae1e96",
  cloning: "8b07863dbce6",
  clone_failed: "75615ead8829",
})

const KEPT_WORDS: Record<string, string> = localizedLiteralMap({
  local_edit: "fbf7406f4710",
  tracked: "7e64e54d2baa",
  unsafe_path: "1d9552acce79",
})

/** The word for one mirror state, or the state itself when it is a newer one. */
export function syncStateWord(state: unknown): string {
  const name = typeof state === "string" ? state : ""
  return STATE_WORDS[name] ?? name
}

/** Only what is really a list of rows; `null`, a missing field and a number are none. */
function rows(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

/** One kept file: its path and why the mirror did not touch it. */
function keptLine(kept: unknown): string {
  const row = (kept ?? {}) as { path?: unknown; reason?: unknown }
  const path = typeof row.path === "string" ? row.path : ""
  const reason = typeof row.reason === "string" ? row.reason : ""
  return labelled(path, KEPT_WORDS[reason] ?? reason)
}

/** The sentence for one apply: its state, what it wrote and what it kept. */
export function describeSyncResult(result: unknown): string {
  const r = (result ?? {}) as { state?: unknown; written?: unknown; deleted?: unknown; kept?: unknown }
  const parts = [syncStateWord(r.state)]
  const written = rows(r.written).length
  const deleted = rows(r.deleted).length
  if (written) parts.push(catalogFormat("template", "407ed506af73", [written]))
  if (deleted) parts.push(catalogFormat("template", "be0401614399", [deleted]))
  for (const kept of rows(r.kept)) parts.push(keptLine(kept))
  return parts.join(clauseSeparator())
}
