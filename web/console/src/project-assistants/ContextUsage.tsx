import type { ProjectAssistantContextUsage } from "@clawdline/contract"
import type { Words } from "./words"

export function ContextUsage({usage, w, language}: {
  usage: ProjectAssistantContextUsage
  w: Words
  language?: string
}) {
  const format = new Intl.NumberFormat(language)
  const remaining = Math.max(0, usage.window_tokens - usage.used_tokens)
  const fraction = remaining / usage.window_tokens
  const level = fraction <= 0.15 ? "low" : fraction <= 0.35 ? "medium" : "high"

  return <small className="pa-context-usage">
    <span>{usage.live ? w.contextUsage : w.contextUsageLast}</span>
    <bdi>{format.format(usage.used_tokens)} / {format.format(usage.window_tokens)}</bdi>
    <span className="pa-context-meter" data-level={level} role="meter"
      aria-label={w.contextRemaining} aria-valuemin={0} aria-valuemax={usage.window_tokens} aria-valuenow={remaining}>
      <span style={{inlineSize: `${fraction * 100}%`}}/>
    </span>
  </small>
}
