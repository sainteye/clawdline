import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { Composer } from "../session/Composer.js"
import { PendingRemoteTurn } from "./PendingRemoteTurn.js"
import {
  PinnedSessionActions, problemCode, receiptPath, receiptStages, watchActionAvailability, type Action, type ActionContext,
  type ActionAvailability, type ActionInput, type ActionProblem, type ActionRecord, type ActionSource, type PinnedClient,
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
  return nextWord("cloudActionTarget", { machine: `${context.machine.name} (${context.destination.machineID})`, session: context.destination.sessionID,
    generation: context.destination.executionGeneration })
}

/** Mount through AllMachineSessions.detailActions(context), never through the chosen-machine writer. */
export function PinnedSessionActionPanel({ context, source, current }: {
  context: ActionContext
  source: ActionSource
  current: () => PinnedClient | null
}) {
  const service = useMemo(() => new PinnedSessionActions(source, current, localStorage, () => crypto.randomUUID()), [source, current])
  const [availability, setAvailability] = useState<ActionAvailability>({})
  const [records, setRecords] = useState<Partial<Record<Action, ActionRecord>>>({})
  const [busy, setBusy] = useState<Action | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState<{ text: string; images: readonly string[]; sentAt: number;
    problem: string | null; safeRestore: boolean } | null>(null)
  const [restoredDraft, setRestoredDraft] = useState<{ id: number; text: string; images: readonly string[] } | null>(null)
  const draftSerial = useRef(0)
  const [confirm, setConfirm] = useState<Action | null>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const confirmDialog = useRef<HTMLDialogElement>(null)
  const confirmTrigger = useRef<HTMLButtonElement | null>(null)
  const destination = context.destination
  const question = context.content?.kind === "ready" ? context.content.question : null
  const key = JSON.stringify([destination.machineID, destination.sessionID, destination.executionGeneration])
  const skillsAbort = useRef<AbortController | null>(null)
  useEffect(() => () => skillsAbort.current?.abort(), [key])
  const loadSkills = useCallback(() => {
    if (!source.readSkills) return Promise.reject(Object.assign(new Error("Session skills are unavailable"), { code: "old_version" }))
    skillsAbort.current?.abort()
    const controller = new AbortController()
    skillsAbort.current = controller
    return source.readSkills(destination, controller.signal)
  }, [source, key])
  // Dictation uses the account's existing Cloud voice host and only inserts
  // text into the shared composer. The subsequent send still pins this target.
  const transcribe = useCallback(async (audio: string, rate: number) => {
    const client = current()
    if (!client?.voice) throw Object.assign(new Error("Cloud voice is unavailable"), { code: "cloud_voice_unavailable" })
    try { return await client.voice(audio, rate) }
    catch (error) {
      if ((error as { code?: string })?.code !== "cloud_voice_host_ambiguous" || !client.setVoiceHost) throw error
      await client.setVoiceHost(destination.machineID)
      return client.voice(audio, rate)
    }
  }, [current, destination.machineID])

  useEffect(() => {
    setRecords(Object.fromEntries(actions.map((action) => [action, service.load(context, action)]).filter(([, record]) => !!record)))
    setError(null)
    setPending(null)
  }, [service, key])

  useEffect(() => {
    if (!pending?.text || context.content?.kind !== "ready") return
    const found = context.content.entries?.some((entry) => entry.role === "user" &&
      typeof entry.at === "number" && entry.at * 1000 >= pending.sentAt - 120_000 &&
      entry.text.replace(/\[Image #\d+\]/gu, "").trim() === pending.text.trim())
    if (found) setPending(null)
  }, [context.content, pending])

  useEffect(() => {
    return watchActionAvailability(context, source, service, setAvailability)
  }, [service, key, context.content?.kind, question?.fingerprint, question?.observedAt,
    context.machine.freshness, context.row.observedAt])

  useEffect(() => {
    if (!confirm) return
    confirmDialog.current?.showModal()
    cancelRef.current?.focus({ preventScroll: true })
    return () => { confirmDialog.current?.close(); confirmTrigger.current?.focus({ preventScroll: true }) }
  }, [confirm])

  async function run(action: Action, input: ActionInput = {}) {
    if (busy) {
      if (action === "send") setPending({ text: input.text ?? "", images: [...(input.images ?? [])],
        sentAt: Date.now(), problem: "receipt_check_required", safeRestore: true })
      return
    }
    if (action === "send" && records.send && !records.send.acknowledged) {
      setError("receipt_check_required")
      setPending({ text: input.text ?? "", images: [...(input.images ?? [])],
        sentAt: Date.now(), problem: "receipt_check_required", safeRestore: true })
      return
    }
    if (action === "send") setPending({ text: input.text ?? "", images: [...(input.images ?? [])],
      sentAt: Date.now(), problem: null, safeRestore: false })
    setConfirm(null)
    setBusy(action)
    setError(null)
    try {
      const record = await service.perform(context, action, input)
      setRecords((before) => ({ ...before, [action]: record }))
    } catch (problem) {
      const code = problemCode(problem)
      setError(code)
      if (action === "send") {
        const persisted = service.load(context, "send")
        if (persisted) setRecords((before) => ({ ...before, send: persisted }))
        setPending((before) => before ? { ...before, problem: code, safeRestore: !persisted } : before)
      }
    }
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

  const restoreRejectedDraft = () => {
    if (!pending || busy || !pending.safeRestore && records.send?.receipt?.machine_execution !== "rejected") return
    if (records.send?.receipt?.machine_execution === "rejected" && !records.send.acknowledged) {
      setRecords((before) => ({ ...before, send: service.acknowledge(records.send!) }))
    }
    setRestoredDraft({ id: ++draftSerial.current, text: pending.text, images: pending.images })
    setPending(null)
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
    {question?.options.length ? <div className="cloud-pinned-question">
      {question.text && <p>{question.text}</p>}
      <div role="group" aria-label={nextWord("cloudActionQuestion")}>
        {question.options.map((option) => <button key={option.key} type="button" disabled={disabled("answer")}
          onClick={() => void run("answer", { answer: option.key, expect: question.fingerprint })}>{option.label}</button>)}
      </div>
      {reason("answer") && <p role="status" data-code={availability.answer}>{reason("answer")}</p>}
    </div> : null}
    {pending && <PendingRemoteTurn text={pending.text} images={pending.images} sentAt={pending.sentAt}
      state={busy === "send" ? "sending" : records.send?.receipt?.machine_execution === "completed" ? "accepted" : "unknown"}
      status={pending.problem || (busy === "send" ? nextWord("cloudActionChecking") :
        nextWord(stageLabels[records.send?.receipt?.machine_execution ?? "unknown"]))} />}
    {pending && (records.send?.receipt?.machine_execution === "rejected" || pending.safeRestore) &&
      <button type="button" className="cloud-pinned-restore" onClick={restoreRejectedDraft}>
        {nextWord("cloudActionRestoreDraft")}
      </button>}
    <div className="cloud-pinned-composer"><Composer key={key} row={null} onDid={() => undefined}
      remote={{ key, machineID: destination.machineID, assistant: context.content?.kind === "ready"
        ? context.content.info?.assistant : undefined, canSend: !disabled("send"), allowPictures: true,
        transcribe, loadSkills, restoreDraft: restoredDraft,
        onSend: async (text, pictures) => { await run("send", { text, images: pictures }) } }} />
      {reason("send") && <p role="status" data-code={availability.send}>{reason("send")}</p>}
    </div>
    <div className="cloud-pinned-status" aria-live="polite">{actions.map((action) => records[action] &&
      <p key={action}>{nextWord(labels[action])} · {nextWord(stageLabels[receiptStages(records[action]!)[1]])}</p>)}</div>
    <details className="cloud-pinned-more"><summary>{nextWord("cloudActionHeading")}</summary>
      {!question?.options.length && <div className="cloud-pinned-question">
        <h3>{nextWord(labels.answer)}</h3>
        <p>{nextWord("cloudActionNoQuestion")}</p>
        {reason("answer") && <p role="status" data-code={availability.answer}>{reason("answer")}</p>}
      </div>}
      <div className="cloud-pinned-risk">
        {(["interrupt", "end"] as const).map((action) => <div key={action}>
          <button type="button" disabled={disabled(action)} onClick={(event) => {
            confirmTrigger.current = event.currentTarget
            setConfirm(action)
          }}>{nextWord(labels[action])}</button>
          {reason(action) && <p role="status" data-code={availability[action]}>{reason(action)}</p>}
        </div>)}
      </div>
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
          {!records[action]!.acknowledged && ["completed", "rejected"].includes(records[action]!.receipt?.machine_execution ?? "") &&
            <button type="button" onClick={() => {
              setRecords((before) => ({ ...before, [action]: service.acknowledge(records[action]!) }))
              if (action === "send" && records.send?.receipt?.machine_execution === "completed") setPending(null)
            }}>{nextWord("cloudActionAcknowledge")}</button>}
        </article>)}
      </div>
    </details>
    {confirm && <dialog ref={confirmDialog} role="alertdialog" aria-modal="true" aria-label={nextWord("cloudActionConfirm")}
      onCancel={(event) => { event.preventDefault(); setConfirm(null) }}>
      <p>{nextWord("cloudActionConfirmQuestion", { action: nextWord(labels[confirm]) })}</p>
      <p>{targetLabel(context)}</p>
      <button type="button" ref={cancelRef} onClick={() => setConfirm(null)}>{nextWord("cloudActionCancel")}</button>
      <button type="button" onClick={() => void run(confirm)}>{nextWord("cloudActionConfirm")}</button>
    </dialog>}
    {error && <p role="alert" data-code={error}>{nextWord("cloudActionFailure", { code: error })}</p>}
  </section>
}
