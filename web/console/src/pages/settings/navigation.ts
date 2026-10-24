/** Stable machine pane names are independent of the standalone window's tab indices. */
export const MACHINE_PANES = ["general", "voice", "remote", "orchestrator", "dispatch-policy"] as const
export type SettingsPane = typeof MACHINE_PANES[number]

export function settingsPanes(cloud: boolean): readonly string[] {
  return cloud ? ["browser", "work", "status", "workflow"] : ["browser", "work", "status", "workflow", ...MACHINE_PANES]
}

/** A tablist's automatic activation, including wraparound and direct endpoint keys. */
export function nextSettingsTab(index: number, key: string, count: number): number | null {
  if (key === "Home") return 0
  if (key === "End") return count - 1
  if (key === "ArrowLeft") return (index + count - 1) % count
  if (key === "ArrowRight") return (index + 1) % count
  return null
}
