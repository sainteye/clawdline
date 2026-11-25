import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { CompactStatusRow } from "./CompactStatusRow"
import { followCompaction, type CompactStatus } from "./compact-status"
import type { Words } from "./words"

/**
 * Tracks the quiet status row above the Bot composer for "Compact now" and
 * "New session": compacting progress/result, or the fresh-context confirmation
 * that clears once the next turn begins or after 8 seconds.
 */
export function useContextActionStatus({
  turns,
  messages,
  w,
  newSessionCount,
  activeTurnID,
  onClearContextUsage,
  onRefresh,
}: {
  turns: readonly Pick<W.ProjectAssistantReadTurnSummary, "id" | "state" | "failure_code" | "failure_field">[]
  messages: readonly Pick<W.ProjectAssistantMessage, "id" | "kind" | "text" | "notice">[]
  w: Words
  newSessionCount?: number
  activeTurnID?: string
  onClearContextUsage: () => void
  onRefresh: () => void
}) {
  const [compactRequest, setCompactRequest] = useState<CompactStatus | null>(null)
  const [freshSessionNotice, setFreshSessionNotice] = useState(false)
  const handledNewSession = useRef(0)
  const clearUsageRef = useRef(onClearContextUsage)
  const refreshRef = useRef(onRefresh)
  clearUsageRef.current = onClearContextUsage
  refreshRef.current = onRefresh

  const compact = followCompaction(compactRequest, turns, messages, w)
  const compacting = compact?.state === "sending" || compact?.state === "running"

  useEffect(() => {
    if (compact !== compactRequest) setCompactRequest(compact)
  }, [compact, compactRequest])

  useEffect(() => {
    if (compact?.state !== "done") return
    const timer = setTimeout(() => setCompactRequest(current => current === compact ? null : current), 8000)
    return () => clearTimeout(timer)
  }, [compact])

  useEffect(() => {
    if (!newSessionCount || handledNewSession.current === newSessionCount) return
    handledNewSession.current = newSessionCount
    setCompactRequest(null)
    setFreshSessionNotice(true)
    clearUsageRef.current()
    refreshRef.current()
  }, [newSessionCount])

  useEffect(() => {
    if (!freshSessionNotice) return
    if (activeTurnID) {
      setFreshSessionNotice(false)
      return
    }
    const timer = setTimeout(() => setFreshSessionNotice(false), 8000)
    return () => clearTimeout(timer)
  }, [freshSessionNotice, activeTurnID])

  return {
    compact,
    compacting,
    setCompactRequest,
    freshSessionNotice,
    dismissFreshSession: () => setFreshSessionNotice(false),
  }
}

export function ContextActionStatusRow({
  compact,
  freshSessionNotice,
  w,
  onRetryCompact,
  onDismissCompact,
  onDismissFreshSession,
}: {
  compact: CompactStatus | null
  freshSessionNotice: boolean
  w: Words
  onRetryCompact: () => void
  onDismissCompact: () => void
  onDismissFreshSession: () => void
}) {
  if (compact) {
    return <CompactStatusRow status={compact} w={w} onRetry={onRetryCompact} onDismiss={onDismissCompact}/>
  }
  if (!freshSessionNotice) return null
  return <div className="pa-compact-status" data-state="done" data-new-session-status="" role="status" aria-live="polite">
    <p><strong>{w.newSessionDone}</strong> {w.newSessionDoneHint}</p>
    <button type="button" className="pa-compact-dismiss" aria-label={w.compactDismiss} onClick={onDismissFreshSession}>×</button>
  </div>
}
