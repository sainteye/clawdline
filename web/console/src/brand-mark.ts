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

export function sessionMark(row: SessionRow): Icon | undefined {
  return row.machine_scope || row.coordinator ? BRAND_MARK : row.icon
}
