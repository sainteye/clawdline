import { useEffect, useRef, useState } from "react"
import type { SessionInfo, SessionRow } from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import { deliverUntilSeen, pendingSends } from "./send.js"
import { rotationReason } from "./rotation.js"
import { interventionTarget, sameInterventionTarget } from "./intervention-composer.js"
import { ActionSpinner } from "../ui/ActionProgress.js"
import "./rotation.css"

export function RotationNotice({ row, info }: { row: SessionRow | null; info: SessionInfo | null }) {
  const [, refresh] = useState(0)
  useEffect(() => pendingSends.subscribe(() => refresh(n => n + 1)), [])
  const target = interventionTarget(row)
  const latest = useRef(target)
  latest.current = target
  const sending = useRef(false)
  const [requested, setRequested] = useState("")
  const key = target ? JSON.stringify(target) : ""
  const message = nextWord("rotationRequest")
  const reason = rotationReason(row, info, Date.now() / 1000)
  if (!row || !reason) return null
  const previous = pendingSends.all().find(card => card.session === row.id && card.conversation === row.sessionId && card.text === message)
  const asked = !!previous || (!!key && requested === key)
  const idle = row.state === "idle"
  const request = async () => {
    if (sending.current || asked || !idle || !target || !sameInterventionTarget(target, latest.current)) return
    sending.current = true
    setRequested(key)
    try {
      const card = pendingSends.add(row.id, message, [], Date.now(), false, row.sessionId)
      await deliverUntilSeen(card)
    } catch { /* The pending card retains the send outcome and recovery controls. */
    } finally { sending.current = false }
  }
  return <aside className="session-rotation" aria-label={nextWord("rotationTitle")}>
    <div><strong>{nextWord("rotationTitle")}</strong><p>{nextWord(reason === "size" ? "rotationSize" : "rotationIdle", {
      size: ((info?.record?.bytes ?? 0) / 1048576).toFixed(1),
    })} {nextWord("rotationSafety")}</p></div>
    <button type="button" disabled={!idle || asked || !target} onClick={() => void request()}>
      {previous?.state === "sending" ? <ActionSpinner /> : null}
      {nextWord(asked ? "rotationRequested" : idle ? "rotationPrepare" : "rotationWait")}
    </button>
  </aside>
}
