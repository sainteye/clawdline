import type { SessionAgent, SessionRow, TaskRow } from "@clawdline/contract"

export type WorkState = "running" | "done" | "failed" | "unknown"

export type WorkNode =
  | { source: "provider"; id: string; state: WorkState; exactState: string; agent: SessionAgent }
  | { source: "broker"; id: string; state: WorkState; exactState: string; task: TaskRow }

const LIVE = new Set(["queued", "spawning", "briefed"])
const FAILED = new Set(["failure", "timeout", "cancelled", "spawn_failed"])

export function taskWorkState(state: TaskRow["state"]): WorkState {
  if (LIVE.has(state)) return "running"
  if (state === "success") return "done"
  if (FAILED.has(state)) return "failed"
  return "unknown"
}

export function workTree(row: SessionRow, tasks: TaskRow[], nowSeconds = Date.now() / 1000): WorkNode[] {
  const provider: WorkNode[] = (row.agents ?? []).map((agent) => ({
    source: "provider",
    id: agent.id,
    state: agent.state,
    exactState: agent.state,
    agent,
  }))
  const broker: WorkNode[] = tasks
    .filter((task) => {
      const root = task.root
      const ours = root?.terminalId === row.id || (!!row.sessionId && root?.sessionId === row.sessionId)
      if (!ours) return false
      if (LIVE.has(task.state)) return true
      return !!task.finishedAt && nowSeconds-task.finishedAt < 180
    })
    .map((task) => ({
      source: "broker" as const,
      id: task.id,
      state: taskWorkState(task.state),
      exactState: task.state,
      task,
    }))
  return [...broker, ...provider]
}
