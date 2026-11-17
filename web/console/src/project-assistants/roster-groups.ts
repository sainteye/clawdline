import type { ProjectAssistantBotSummary } from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botIdentity } from "./view-state.ts"
// @ts-expect-error Node executes source extensions.
import { orderedKeys } from "./roster-state.ts"

export type RosterGroup = { key: string; kind: "pinned" | "project"; machineID: string; projectID: string; items: ProjectAssistantBotSummary[] }
export const projectKey = (machineID: string, projectID: string) => JSON.stringify([machineID, projectID])
export const pinnedGroupKey = JSON.stringify(["roster", "section", "pinned"])

/** Uncustomized Projects retain newest-activity order; pins use a distinct section identity. */
export function groupByProject(items: readonly ProjectAssistantBotSummary[], pinnedKeys: readonly string[] = [], projectOrder: readonly string[] = [], botOrder: readonly string[] = []): RosterGroup[] {
  const byBot = new Map(items.map(item => [botIdentity(item.bot.key), item]))
  const ordered = orderedKeys([...byBot.keys()], botOrder).map(key => byBot.get(key)!)
  const groups = new Map<string, RosterGroup>()
  const pinned: RosterGroup = {key: pinnedGroupKey, kind: "pinned", machineID: "", projectID: "", items: []}
  // Project placement follows activity, independently of a custom Bot order.
  for (const item of items) {
    if (pinnedKeys.includes(botIdentity(item.bot.key))) continue
    const machineID = item.bot.key.machine_id, projectID = item.bot.project.project_id, key = projectKey(machineID, projectID)
    if (!groups.has(key)) groups.set(key, {key, kind: "project", machineID, projectID, items: []})
  }
  for (const item of ordered) {
    if (pinnedKeys.includes(botIdentity(item.bot.key))) pinned.items.push(item)
    else groups.get(projectKey(item.bot.key.machine_id, item.bot.project.project_id))!.items.push(item)
  }
  return [...(pinned.items.length ? [pinned] : []), ...orderedKeys([...groups.keys()], projectOrder).map(key => groups.get(key)!)]
}
