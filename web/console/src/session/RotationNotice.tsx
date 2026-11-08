import { useEffect, useRef, useState } from "react"
import type { SessionInfo, SessionRow } from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import { deliverUntilSeen, pendingSends } from "./send.js"
import { rotationReason } from "./rotation.js"
import { interventionTarget, sameInterventionTarget } from "./intervention-composer.js"
import { ActionSpinner } from "../ui/ActionProgress.js"
import { assistantProtocolOwned } from "./antigravity-ui.js"
import { SessionFacts } from "../overlays/facts.js"

/** An optional card in the existing attention panel, never a footer surface. */
export function useRotationNote(row: SessionRow | null, onReplySent?: () => void, onDismiss?: () => void) {
  const [, refresh] = useState(0)
  useEffect(() => pendingSends.subscribe(() => refresh(n => n + 1)), [])
  const target = interventionTarget(row)
  const latest = useRef(target)
  latest.current = target
  const sending = useRef(false)
  const [requested, setRequested] = useState("")
  const key = target ? JSON.stringify(target) : ""
  const storageKey = key ? "clawdline.rotation-dismissed:" + key : ""
  const [dismissed, setDismissed] = useState("")
  const [answer, setAnswer] = useState<{ id: string; info: SessionInfo } | null>(null)
  const id = row?.id ?? ""
  useEffect(() => SessionFacts.subscribe((from, info) => {
    if (from === id) setAnswer({ id: from, info })
  }), [id])
  useEffect(() => {
    const update = (event: StorageEvent) => { if (event.key === storageKey) refresh(n => n + 1) }
    window.addEventListener("storage", update)
    return () => window.removeEventListener("storage", update)
  }, [storageKey])
  const info = answer?.id === id ? answer.info : SessionFacts.peek(id)
  const message = nextWord("rotationRequest")
  const reason = rotationReason(row, info, Date.now() / 1000)
  let savedDismissal = false
  try { savedDismissal = !!storageKey && localStorage.getItem(storageKey) === "1" } catch { /* In-memory dismissal still works when browser storage is unavailable. */ }
  if (savedDismissal || (!!key && dismissed === key)) return null
  if (!row || !reason) return null
  const previous = pendingSends.all().find(card => card.session === row.id && card.conversation === row.sessionId && card.text === message)
  const asked = !!previous || (!!key && requested === key)
  // Claude Code and Codex take input mid-turn in a terminal, exactly as the
  // composer sends it. Only a protocol-owned conversation, whose composer may
  // hold input in the browser until the turn ends, waits for idle here.
  const idle = row.state === "idle" || !assistantProtocolOwned(row)
  const request = async () => {
    if (sending.current || asked || !idle || !target || !sameInterventionTarget(target, latest.current)) return
    sending.current = true
    setRequested(key)
    try {
      const card = pendingSends.add(row.id, message, [], Date.now(), false, row.sessionId)
      const code = await deliverUntilSeen(card)
      if (!code && sameInterventionTarget(target, latest.current)) onReplySent?.()
    } catch { /* The pending card retains the send outcome and recovery controls. */
    } finally { sending.current = false }
  }
  const dismiss = () => {
    if (!target || !sameInterventionTarget(target, latest.current)) return
    try { localStorage.setItem(storageKey, "1") } catch { /* Keep the current view dismissible without storage. */ }
    setDismissed(key)
    onDismiss?.()
  }
  return <article className="human-intervention-card session-rotation-note" aria-label={nextWord("rotationTitle")}>
    <div className="human-intervention-title"><h3>{nextWord("rotationTitle")}</h3></div>
    <p>{nextWord(reason === "size" ? "rotationSize" : "rotationIdle", {
      size: ((info?.record?.bytes ?? 0) / 1048576).toFixed(1),
    })}</p>
    <p className="human-intervention-reason">{nextWord("rotationSafety")}</p>
    <div className="human-intervention-actions">
    <button className="human-intervention-resolve" type="button" disabled={!idle || asked || !target} onClick={() => void request()}>
      {previous?.state === "sending" ? <ActionSpinner /> : null}
      {nextWord(asked ? "rotationRequested" : idle ? "rotationPrepare" : "rotationWait")}
    </button>
    <button type="button" onClick={dismiss}>{nextWord("rotationDismiss")}</button>
    </div>
  </article>
}
