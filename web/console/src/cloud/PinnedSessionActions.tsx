import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import {
  PinnedSessionActions, problemCode, receiptPath, receiptStages, type Action, type ActionContext,
  type ActionInput, type ActionProblem, type ActionRecord, type ActionSource, type PinnedClient,
} from "./pinned-session-actions.js"
import "./pinned-session-actions.css"

const actions: readonly Action[] = ["send", "answer", "interrupt", "end"]
const labels = { send: "cloudActionSend", answer: "cloudActionAnswer", interrupt: "cloudActionInterrupt", end: "cloudActionClose" } as const
const stageLabels = { unknown: "cloudActionStageUnknown", accepted: "cloudActionStageAccepted",
  completed: "cloudActionStageCompleted", rejected: "cloudActionStageRejected", pending: "cloudActionStagePending",
  missing: "cloudActionStageMissing", observed: "cloudActionStageObserved", acknowledged: "cloudActionStageAcknowledged" } as const
const problems: Record<ActionProblem, "cloudActionOffline" | "cloudActionStale" | "cloudActionUnknown" |
  "cloudActionChanged" | "cloudActionRevoked" | "cloudActionContentPermission" | "cloudActionOperationPermission" |
  "cloudActionUnsupported" | "cloudActionUnavailable" | "cloudActionStorageUnavailable" |
  "cloudActionMenuUnverified" | "cloudActionCloseBlocked"> = {
  offline: "cloudActionOffline", stale: "cloudActionStale", unknown: "cloudActionUnknown",
  changed: "cloudActionChanged", revoked: "cloudActionRevoked", content_permission: "cloudActionContentPermission",
  operation_permission: "cloudActionOperationPermission", unsupported: "cloudActionUnsupported",
  unavailable: "cloudActionUnavailable", storage_unavailable: "cloudActionStorageUnavailable",
  menu_unverified: "cloudActionMenuUnverified",
  closeability_blocked: "cloudActionCloseBlocked",
}

function targetLabel(context: ActionContext): string {
  return nextWord("cloudActionTarget", { machine: context.machine.name, session: context.destination.sessionID,
    generation: context.destination.executionGeneration })
}

/** Mount through AllMachineSessions.detailActions(context), never through the chosen-machine writer. */
export function PinnedSessionActionPanel({ context, source, current }: {
  context: ActionContext
  source: ActionSource
  current: () => PinnedClient | null
}) {
  const service = useMemo(() => new PinnedSessionActions(source, current, localStorage, () => crypto.randomUUID()), [source, current])
  const [availability, setAvailability] = useState<Partial<Record<Action, ActionProblem | null>>>({})
  const [records, setRecords] = useState<Partial<Record<Action, ActionRecord>>>({})
  const [busy, setBusy] = useState<Action | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState("")
  const [confirm, setConfirm] = useState<Action | null>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const destination = context.destination
  const question = context.content?.kind === "ready" ? context.content.question : null
  const key = JSON.stringify([destination.machineID, destination.sessionID, destination.executionGeneration])

  useEffect(() => {
    setRecords(Object.fromEntries(actions.map((action) => [action, service.load(context, action)]).filter(([, record]) => !!record)))
    setError(null)
  }, [service, key])

  useEffect(() => {
    let live = true
    const check = () => void Promise.all(actions.map(async (action) => [action, await service.availability(context, action)] as const))
      .then((values) => { if (live) setAvailability(Object.fromEntries(values)) })
      .catch(() => { if (live) setAvailability(Object.fromEntries(actions.map((action) => [action, "unknown"]))) })
    check()
    const stop = (source as ActionSource & { subscribe?: (listener: (event: { machineID: string }) => void) => () => void })
      .subscribe?.((event) => { if (event.machineID === destination.machineID) check() })
    return () => { live = false; stop?.() }
  }, [service, key, context.content?.kind, question?.fingerprint, context.machine.freshness])

  useLayoutEffect(() => { if (confirm) cancelRef.current?.focus({ preventScroll: true }) }, [confirm])

  async function run(action: Action, input: ActionInput = {}) {
    if (busy) return
    setConfirm(null)
    setBusy(action)
    setError(null)
    try {
      const record = await service.perform(context, action, input)
      setRecords((before) => ({ ...before, [action]: record }))
    } catch (problem) { setError(problemCode(problem)) }
    finally { setBusy(null) }
  }

  async function checkReceipt(action: Action, record: ActionRecord) {
    if (busy) return
    setBusy(action)
    setError(null)
    try {
      const result = await service.lookup(record)
      setRecords((before) => ({ ...before, [action]: result }))
    }
    catch (problem) { setError(problemCode(problem)) }
    finally { setBusy(null) }
  }

  function disabled(action: Action): boolean {
    return busy !== null || availability[action] !== null || !!records[action] && !records[action]!.acknowledged
  }

  function reason(action: Action): string | null {
    const problem = availability[action]
    if (problem === undefined) return nextWord("cloudActionChecking")
    if (problem) return nextWord(problems[problem])
    if (records[action] && !records[action]!.acknowledged) return nextWord("cloudActionCheckFirst")
    return null
  }

  return <section className="cloud-pinned-actions" aria-label={nextWord("cloudActionHeading")}>
    <h2>{nextWord("cloudActionHeading")}</h2>
    <p className="cloud-pinned-target">{targetLabel(context)}</p>
    <form onSubmit={(event) => { event.preventDefault(); void run("send", { text: message }) }}>
      <label htmlFor="cloud-pinned-message">{nextWord("cloudActionMessage")}</label>
      <textarea id="cloud-pinned-message" value={message} onChange={(event) => setMessage(event.target.value)} rows={3} />
      <button type="submit" disabled={disabled("send") || !message.trim()}>{nextWord(labels.send)}</button>
      {reason("send") && <p role="status" data-code={availability.send}>{reason("send")}</p>}
    </form>
    <div className="cloud-pinned-question">
      <h3>{nextWord(labels.answer)}</h3>
      {question?.options.length ? <div role="group" aria-label={nextWord("cloudActionQuestion")}>
        {question.text && <p>{question.text}</p>}
        {question.options.map((option) => <button key={option.key} type="button" disabled={disabled("answer")}
          onClick={() => void run("answer", { answer: option.key, expect: question.fingerprint })}>{option.label}</button>)}
      </div> : <p>{nextWord("cloudActionNoQuestion")}</p>}
      {reason("answer") && <p role="status" data-code={availability.answer}>{reason("answer")}</p>}
    </div>
    <div className="cloud-pinned-risk">
      {(["interrupt", "end"] as const).map((action) => <div key={action}>
        <button type="button" disabled={disabled(action)} onClick={() => setConfirm(action)}>{nextWord(labels[action])}</button>
        {reason(action) && <p role="status" data-code={availability[action]}>{reason(action)}</p>}
      </div>)}
    </div>
    {confirm && <div role="alertdialog" aria-modal="true" aria-label={nextWord("cloudActionConfirm")}
      onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); setConfirm(null) } }}>
      <p>{nextWord("cloudActionConfirmQuestion", { action: nextWord(labels[confirm]) })}</p>
      <p>{targetLabel(context)}</p>
      <button type="button" ref={cancelRef} onClick={() => setConfirm(null)}>{nextWord("cloudActionCancel")}</button>
      <button type="button" onClick={() => void run(confirm)}>{nextWord("cloudActionConfirm")}</button>
    </div>}
    {error && <p role="alert" data-code={error}>{nextWord("cloudActionFailure", { code: error })}</p>}
    <div className="cloud-pinned-receipts" aria-live="polite">
      {actions.map((action) => records[action] && <article key={action}>
        <h3>{nextWord(labels[action])}</h3>
        <p>{targetLabel(context)}</p>
        <p>{nextWord("cloudActionRequest", { request: records[action]!.request })}</p>
        <p>{nextWord("cloudActionReceiptPath", { path: receiptPath(records[action]!) })}</p>
        <ol>
          {receiptStages(records[action]!).map((stage, index) => <li key={index}>
            {nextWord((["cloudActionRelayAccepted", "cloudActionMachineExecution", "cloudActionReplyDelivered",
              "cloudActionViewerObserved", "cloudActionUserAcknowledged"] as const)[index])}: {nextWord(stageLabels[stage])}
          </li>)}
        </ol>
        {records[action]!.problem && <p role="status" data-code={records[action]!.problem}>
          {records[action]!.problem === "receipt_check_required" ? nextWord("cloudActionCheckRequired") :
            nextWord("cloudActionReceiptProblem", { code: records[action]!.problem! })}</p>}
        {records[action]!.receipt?.code && <p role="status" data-code={records[action]!.receipt!.code}>
          {nextWord("cloudActionReceiptProblem", { code: records[action]!.receipt!.code })}</p>}
        <button type="button" disabled={busy !== null} onClick={() => void checkReceipt(action, records[action]!)}>
          {nextWord("cloudActionCheckReceipt")}</button>
        {!records[action]!.acknowledged && ["completed", "rejected", "missing"].includes(records[action]!.receipt?.machine_execution ?? "") &&
          <button type="button" onClick={() => setRecords((before) => ({ ...before,
            [action]: service.acknowledge(records[action]!) }))}>{nextWord("cloudActionAcknowledge")}</button>}
      </article>)}
    </div>
  </section>
}
