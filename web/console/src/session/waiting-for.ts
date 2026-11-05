import type { SessionRow } from "@clawdline/contract"
import type { nextWord } from "../next-strings.js"

/**
 * What a waiting Session waits for, in words, from the row alone.
 *
 * Every list draws this from the daemon's `waiting_for` and live callback
 * shells, collected in the same pass that decides the row waits. The local list, the
 * selected-machine Cloud list and the all-machines Cloud list therefore say
 * the same thing about the same Session: none of them reads a task collection
 * of its own, which is what once made one scope name the work and another say
 * only that it waited.
 *
 * `word` is `nextWord`, passed in so node's test runner can load this file.
 *
 * `covered` means the row waits only on the heavy command its 🏗️ marker
 * already names, so the hourglass would repeat it.
 */
export function waitingForWords(row: Partial<SessionRow>, word: typeof nextWord): { text: string; title: string; covered: boolean } {
  const targets = (row.waiting_for ?? []).filter((target) => target.task_id !== row.heavy_work?.task_id)
  const known = new Set((row.waiting_for ?? []).map((target) => target.task_id))
  // Older list projections can carry live callback shells without waiting_for.
  // Both sources belong to this exact Session; never infer a wait from prose.
  for (const shell of row.shells ?? []) {
    if (shell.kind !== "callback" || (shell.state !== "queued" && shell.state !== "running")) continue
    const id = shell.id.replace(/^callback-/, "")
    if (known.has(id) || id === row.heavy_work?.task_id) continue
    targets.push({ task_id: id, kind: "callback", label: shell.what || shell.command || id, phase: shell.state })
    known.add(id)
  }
  const omitted = row.waiting_for_omitted ?? 0
  if (!targets.length && !omitted) return { text: "", title: "", covered: (row.waiting_for ?? []).length > 0 }
  const names = targets.map((target) => {
    if (target.kind === "callback") return target.phase === "queued"
      ? word("sessionWaitHeavyQueued", { command: target.label }) : target.label
    if (target.kind !== "heavy" || !target.phase) return target.label
    return word(target.phase === "running" ? "sessionWaitHeavyRunning" : "sessionWaitHeavyQueued", { command: target.label })
  })
  const list = names.join(" · ") + (omitted ? "  +" + String(omitted) : "")
  const text = targets.length && targets.every((target) => target.kind === "callback")
    ? word("sessionWaitingOnCallback", { work: list })
    : targets.every((target) => target.kind === "session")
    ? word("sessionWaitingOnNamedSession", { session: list })
    : word("sessionWaitingOnWork", { work: list })
  return { text, title: names.join("\n"), covered: false }
}

/** Queued callbacks have not started their command and are not running shells. */
export function runningShellCount(row: Partial<SessionRow>): number {
  return (row.shells ?? []).filter((shell) => shell.state !== "queued").length
}
