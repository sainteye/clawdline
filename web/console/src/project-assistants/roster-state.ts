import type { ProjectAssistantBotSummary } from "@clawdline/contract"

export interface RosterState {
  pinnedBotKeys: string[]
  projectSortOrder: string[]
}

export function getRosterState(): RosterState {
  return {
    pinnedBotKeys: [],
    projectSortOrder: []
  }
}
