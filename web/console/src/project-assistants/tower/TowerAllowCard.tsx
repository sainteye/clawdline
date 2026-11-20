import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "../words"
import { AllowError, type AllowReason, type Waiting } from "./tower-allow"

/** What the tower needs to read and answer one waiting offer; Bots builds it
 * from the Bot's own source so the tower never picks a source itself. */
export interface TowerAllow {
  read(item: W.ProjectAssistantBotSummary, signal: AbortSignal): Promise<Waiting>
  send(waiting: Waiting, signal: AbortSignal): Promise<W.ProjectAssistantReceipt>
}

type Phase =
  | { kind: "reading" }
  | { kind: "ready", waiting: Waiting }
  | { kind: "sending", waiting: Waiting }
  | { kind: "allowed", waiting: Waiting }
  | { kind: "refused", reason: AllowReason, waiting?: Waiting }

const reasonWord: Record<AllowReason, keyof Words> = {
  none: "towerAllowNone", expired: "towerAllowExpired", answered: "towerAllowAnswered", changed: "towerAllowChanged",
  revoked: "towerAllowRevoked", read_only: "towerAllowReadOnly", offline: "towerAllowOffline", unknown: "towerAllowUnknown",
  failed: "towerAllowFailed", invalid: "towerAllowInvalid",
}
const reasonOf = (error: unknown): AllowReason => error instanceof AllowError ? error.reason : "offline"

/** The full request behind one waiting Bot, and "Allow once" when the offer has
 * it and this device may answer. `attempted` is the tower's record of offers
 * already sent from here: one is never offered again, whatever came back. */
export function TowerAllowCard({ item, allow, attempted, w, onChat }: { item: W.ProjectAssistantBotSummary, allow: TowerAllow, attempted: Map<string, AllowReason | "allowed">, w: Words, onChat: () => void }) {
  const [phase, setPhase] = useState<Phase>({ kind: "reading" })
  const live = useRef(true), control = useRef<AbortController | null>(null)
  const read = () => {
    control.current?.abort()
    const c = new AbortController(); control.current = c
    setPhase({ kind: "reading" })
    allow.read(item, c.signal).then(waiting => {
      if (!live.current || c.signal.aborted) return
      const sent = attempted.get(waiting.offer.id)
      setPhase(sent === "allowed" ? { kind: "allowed", waiting } : sent ? { kind: "refused", reason: sent, waiting } : { kind: "ready", waiting })
    }, error => { if (live.current && !c.signal.aborted && (error as Error)?.name !== "AbortError") setPhase({ kind: "refused", reason: reasonOf(error) }) })
  }
  useEffect(() => { live.current = true; read(); return () => { live.current = false; control.current?.abort() } }, [])
  const send = async (waiting: Waiting) => {
    if (phase.kind !== "ready" || attempted.has(waiting.offer.id)) return
    setPhase({ kind: "sending", waiting })
    const c = new AbortController(); control.current = c
    try {
      await allow.send(waiting, c.signal)
      attempted.set(waiting.offer.id, "allowed")
      if (live.current) setPhase({ kind: "allowed", waiting })
    } catch (error) {
      const reason = reasonOf(error)
      // Only a refusal that proves nothing left may be tried again by reading anew.
      if (reason === "unknown" || reason === "failed") attempted.set(waiting.offer.id, reason)
      if (live.current) setPhase({ kind: "refused", reason, waiting })
    }
  }

  const waiting = "waiting" in phase ? phase.waiting : undefined
  const action = waiting?.offer.action
  const canAllow = phase.kind === "ready" && !!waiting?.once && waiting.writable
  return <div className="pa-tower-allow" data-phase={phase.kind}>
    {phase.kind === "reading" && <p role="status">{w.towerAllowReading}</p>}
    {waiting && <>
      {action?.title && <p className="pa-tower-allow-title">{action.title}</p>}
      {action?.detail && <pre>{action.detail}</pre>}
      {action?.diffs.map((d, i) => <div className="pa-tower-allow-diff" key={i}><code>{d.path}</code>{d.old_text && <pre data-old="">{d.old_text}</pre>}<pre>{d.new_text}</pre></div>)}
      {action?.shortened && <small>{w.permissionShortened}</small>}
      <p className="pa-tower-allow-options">{w.towerAllowChoices.replace("{list}", waiting.offer.options.map(o => o.label || w[o.kind]).join(" · "))}</p>
      {!waiting.once && <p>{w.towerAllowChatOnly}</p>}
      {waiting.once && !waiting.writable && <p>{w.towerAllowViewOnly}</p>}
      {waiting.more > 0 && <p>{w.towerAllowMore.replace("{count}", String(waiting.more))}</p>}
    </>}
    {phase.kind === "allowed" && <p role="status" className="pa-tower-allow-done">{w.towerAllowDone}</p>}
    {phase.kind === "refused" && <p role="alert" data-reason={phase.reason}>{w[reasonWord[phase.reason]]}</p>}
    <div className="pa-tower-allow-actions">
      {(canAllow || phase.kind === "sending") && <button type="button" className="pa-tower-allow-once" disabled={phase.kind === "sending"} aria-busy={phase.kind === "sending" || undefined} onClick={() => waiting && void send(waiting)}>{w.allow_once}</button>}
      <button type="button" onClick={onChat}>{w.towerOpenChat}</button>
      {phase.kind === "refused" && phase.reason === "offline" && <button type="button" onClick={read}>{w.towerAllowReread}</button>}
    </div>
  </div>
}
