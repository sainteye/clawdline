export type ChevronDirection = "down" | "right" | "left" | "up"

// The existing Bots select's geometric canvas also serves the older DOM controls.
export const chevronPath: Record<ChevronDirection, string> = {
  down: "M4 6l4 4 4-4",
  right: "m6 4 4 4-4 4",
  left: "m10 4-4 4 4 4",
  up: "m4 10 4-4 4 4",
}

export function chevronHTML(direction: ChevronDirection): string {
  return '<svg class="control-chevron" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="' + chevronPath[direction] + '"></path></svg>'
}
