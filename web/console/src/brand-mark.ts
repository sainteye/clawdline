import type { Icon, SessionRow } from "@clawdline/contract"

// The wordmark and every machine workspace Session share the same mark.
export const BRAND_MARK: Icon = {
  accent: "#d97757",
  cells: [".######.", ".#o##o#.", "########", ".##..##."].map((row) =>
    row.split("").map((ch) => (ch === "#" ? "#d97757" : ch === "o" ? "#141416" : "#33201a")),
  ),
}

export function sessionName(row: SessionRow): string {
  return row.machine_scope || row.coordinator ? "Clawdfather" : row.label || row.tty || row.id
}

/** A pane can have a conversation identity before its display name reaches the list. */
export function detailSessionName(row: SessionRow): string {
  if (row.machine_scope || row.coordinator) return sessionName(row)
  if ((!row.label || row.label === row.id) && (!row.tty || row.tty === row.id)) {
    const assistant = row.assistant === "codex" ? "Codex" : row.assistant === "claude" ? "Claude" : row.assistant === "antigravity" ? "Gemini" : "Assistant"
    return `${assistant} Session`
  }
  return sessionName(row)
}

export function sessionMark(row: SessionRow): Icon | undefined {
  return row.machine_scope || row.coordinator ? BRAND_MARK : row.icon
}

/** The signed Cloud status identifies a bound role without carrying its commands. */
export function sessionCoordinatorBound(row: SessionRow | null | undefined): boolean {
  return !!row?.coordinator || (row as (SessionRow & { coordinator_bound?: boolean }) | null | undefined)?.coordinator_bound === true
}

export function projectedSessionIdentity(title: string, icon: Icon | undefined, machineScope: boolean,
  coordinatorBound: boolean): { title: string; icon: Icon; crown: boolean; pending: boolean } {
  return machineScope
    ? { title: "Clawdfather", icon: BRAND_MARK, crown: true, pending: !coordinatorBound }
    : { title, icon: icon ?? BRAND_MARK, crown: false, pending: false }
}
