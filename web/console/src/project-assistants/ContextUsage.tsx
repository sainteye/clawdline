import type { ProjectAssistantContextUsage } from "@clawdline/contract"
import type { Words } from "./words"

export function ContextUsage({usage, w, language}: {
  usage: ProjectAssistantContextUsage
  w: Words
  language?: string
}) {
  const format = new Intl.NumberFormat(language)
  return <small className="pa-context-usage">
    {usage.live?w.contextUsage:w.contextUsageLast} · {format.format(usage.used_tokens)} / {format.format(usage.window_tokens)}
  </small>
}
