/** An ordinal display, never a slider: reading settings must not change them. */
export function reasoningEffortHTML(
  assistant: string,
  effort: string | undefined,
  word: (key: "infoEffortUnknown" | "infoEffortOrdinal" | "infoEffortLastTurn" | "infoReasoningEffort" | `infoEffort${"None" | "Minimal" | "Low" | "Medium" | "High" | "Xhigh" | "Max" | "Ultra"}`) => string,
  esc: (text: string) => string,
): string {
  const levels = assistant === "codex"
    ? ["low", "medium", "high", "xhigh", "max", "ultra"]
    : ["low", "medium", "high", "xhigh", "max"]
  // Preserve readings from older Codex models without adding irrelevant ticks to every model.
  if (assistant !== "antigravity" && (effort === "none" || effort === "minimal")) levels.unshift("none", "minimal")
  const keys = { none: "None", minimal: "Minimal", low: "Low", medium: "Medium", high: "High", xhigh: "Xhigh", max: "Max", ultra: "Ultra" } as const
  const index = levels.indexOf(effort || "")
  if (index < 0) {
    return '<p class="note effort-unknown">' + esc(effort || word("infoEffortUnknown")) + '</p>'
  }
  const label = (level: string) => word(`infoEffort${keys[level as keyof typeof keys]}`)
  const selected = label(effort!)
  const ticks = levels.map((level, i) => '<span class="effort-tick"' + (i === index ? ' data-current="1"' : "") +
    '><i aria-hidden="true"></i><span>' + esc(label(level)) + '</span></span>').join("")
  return '<div class="effort-reading"><strong>' + esc(selected) + '</strong></div>' +
    '<div class="effort-scale" role="meter" aria-label="' + esc(word("infoReasoningEffort")) +
    '" aria-valuemin="1" aria-valuemax="' + levels.length + '" aria-valuenow="' + (index + 1) +
    '" aria-valuetext="' + esc(selected) + '" title="' + esc(word("infoEffortOrdinal")) + '">' + ticks + '</div>' +
    '<p class="note effort-source">' + esc(word("infoEffortLastTurn")) + '</p>'
}
