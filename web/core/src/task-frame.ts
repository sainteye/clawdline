import type { TaskList } from "@clawdline/contract"

/**
 * The `orchestrator` frame's task list, or null for one that is not a list.
 *
 * A frame that will not parse, or parses to something without a `tasks` array,
 * is not news that every task ended; it is dropped and the page keeps reading
 * the list itself.
 */
export function taskListFrame(data: string): TaskList | null {
  try {
    const list = JSON.parse(data) as TaskList
    return list && typeof list === "object" && Array.isArray(list.tasks) ? list : null
  } catch {
    return null
  }
}
