import type { ProjectAssistantBotSummary } from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botIdentity } from "./view-state.ts"

export type RosterGroup = { key: string; machineID: string; projectID: string; items: ProjectAssistantBotSummary[] }

export const projectKey = (machineID: string, projectID: string) => JSON.stringify([machineID, projectID])

export function groupByProject(items: readonly ProjectAssistantBotSummary[], pinnedKeys: readonly string[] = []): RosterGroup[] {
  const groups = new Map<string, RosterGroup>()
  for (const item of items) {
    const isPinned = pinnedKeys.includes(botIdentity(item.bot.key))
    const machineID = item.bot.key.machine_id, projectID = isPinned ? "pinned" : item.bot.project.project_id, key = projectKey(machineID, projectID)
    let group = groups.get(key)
    if (!group) groups.set(key, group = { key, machineID, projectID, items: [] })
    group.items.push(item)
  }
  return [...groups.values()].sort((a, b) => a.projectID === "pinned" ? -1 : 1)
}
