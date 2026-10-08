import type { SessionRow, TaskRow } from "@clawdline/contract"

/** Callbacks have no child tab. Their durable root identifies the Session doing the work. */
export function activeCallbacksForSession(
  row: Pick<SessionRow, "id" | "sessionId">,
  tasks: readonly TaskRow[],
): TaskRow[] {
  if (!row.sessionId) return []
  return tasks.filter((task) =>
    task.kind === "callback" &&
    (task.state === "queued" || task.state === "spawning" || task.state === "briefed") &&
    task.root?.terminalId === row.id &&
    task.root.sessionId === row.sessionId,
  )
}
