import type { ProjectAssistantBotSummary } from "@clawdline/contract"

export type RosterGroup = { key: string; machineID: string; projectID: string; items: ProjectAssistantBotSummary[] }

/** The machine-and-Project key every roster map is keyed by. */
export const projectKey = (machineID: string, projectID: string) => JSON.stringify([machineID, projectID])

/**
 * Bots under the Project they work in. A Project keeps the place of its most
 * recent Bot, and Bots inside it keep the order the roster gave them, so the
 * newest activity still reads first.
 */
export function groupByProject(items: readonly ProjectAssistantBotSummary[], pinnedKeys: readonly string[]): RosterGroup[] {
  const groups = new Map<string, RosterGroup>()
  for (const item of items) {
    const isPinned = pinnedKeys.includes(botIdentity(item.bot.key))
    const machineID = item.bot.key.machine_id, projectID = isPinned ? "pinned" : item.bot.project.project_id, key = projectKey(machineID, projectID)
    let group = groups.get(key)
    if (!group) groups.set(key, group = { key, machineID, projectID, items: [] })
    group.items.push(item)
  }
  // 確保 pinned 組永遠排在最前面
  return [...groups.values()].sort((a, b) => a.projectID === "pinned" ? -1 : 1)
}
