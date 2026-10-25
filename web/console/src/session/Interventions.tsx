import { ActionProgress, ActionSpinner } from "../ui/ActionProgress.js"
import { catalogFormat } from "../catalog.js"
import { catalogWord } from "../catalog.js"
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FormEvent } from "react"
import type { SessionRow } from "@clawdline/contract"
import { asMachineNeedsUpdate, type MachineNeedsUpdate } from "@clawdline/core"
import { NeedsUpdate } from "../machine/NeedsUpdate.js"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import { humanInterventionActionV2, readHumanInterventionsV2, submitSecretHandoffV2, type HumanInterventionV2, type HumanInterventionsV2 } from "../pages/work/api.js"
import { failureWords, when } from "../pages/work/shared.js"
import { WorkIcon } from "../pages/work/WorkIcon.js"
import { interventionTarget, sameInterventionTarget, type InterventionTarget } from "./intervention-composer.js"
import { pendingFailureCanRetry, pendingFailureSentence } from "./pending-copy.js"
import { deliverUntilSeen, pendingSends } from "./send.js"
import { browserRefreshEnvironment, readWithOneRetry, TODO_REFRESH_MS, TODO_SAFETY_MS, watchTodoRefresh } from "./todo-refresh.js"
import { attentionReads, countMoveNeedsRead, unresolvedNoteCount } from "./attention-reads.js"
import { onWorkItemChanged } from "../pages/work/item-changed.js"
import { announceAttentionRead } from "./attention-changed.js"
import { samePageTarget } from "../same-page-links.js"
import { hasDocumentIntent } from "../legacy/documents-bridge.js"
import { requestPage } from "../overlays/index.js"
import { catalogLabel, clauseSeparator, labelled } from "../punctuation.js"
import "./interventions.css"

/** Keep the attention entry in the todo header while its panel stays independent. */
export function useInterventions(row: SessionRow | null, onReplySent?: () => void, onExpand?: () => void, onCompose?: (text: string) => void) {
  const destination = interventionTarget(row)
  const key = destination ? JSON.stringify(destination) : ""
  const latest = useRef<InterventionTarget | null>(destination)
  latest.current = destination
  const attentionCount = typeof row?.attention_count === "number" ? row.attention_count : null
  const rowReading = useRef({ sessionID: row?.id ?? "", count: attentionCount })
  rowReading.current = { sessionID: row?.id ?? "", count: attentionCount }
  // Whether the row counts the notes is read when the page opens: a count
  // that comes and goes must not restart the panel, or move where its news
  // comes from half way through.
  const opening = useRef({ key, counted: attentionCount !== null })
  if (opening.current.key !== key) opening.current = { key, counted: attentionCount !== null }
  const counted = opening.current.counted
  const ticket = useRef(0)
  const flight = useRef<{ key: string; promise: Promise<void> } | null>(null)
  const [page, setPage] = useState<HumanInterventionsV2 | null>(null)
  const [pageKey, setPageKey] = useState("")
  const [readError, setReadError] = useState("")
  // The read failed because this machine is too old for it (docs/updates.md).
  const [readUpdate, setReadUpdate] = useState<MachineNeedsUpdate | null>(null)
  const [actionError, setActionError] = useState("")
  const [actionStatus, setActionStatus] = useState("")
  const [reading, setReading] = useState(false)
  const [busy, setBusy] = useState("")
  const [secretReceipt, setSecretReceipt] = useState<{ id: string; phase: "sending" | "received" } | null>(null)
  const receiptTimer = useRef<number | null>(null)
  const collapseTimer = useRef<number | null>(null)
  const [collapsing, setCollapsing] = useState(false)
  const busyRef = useRef(false)
  const actionSerial = useRef(0)
  const [expanded, setExpanded] = useState(false)
  const panel = useRef<HTMLElement>(null)
  useLayoutEffect(() => {
    if (expanded) return
    // These details used to unmount on close; keep their reset semantics.
    for (const details of panel.current?.querySelectorAll("details[open]") ?? []) {
      (details as HTMLDetailsElement).open = false
    }
  }, [expanded])

  const load = useCallback((target: InterventionTarget): Promise<void> => {
    const targetKey = JSON.stringify(target)
    if (flight.current?.key === targetKey) return flight.current.promise
    const mine = ++ticket.current
    setReading(true)
    const promise = (async () => {
      try {
        // A transient failure is asked again once before it is shown.
        const next = await readWithOneRetry(() => readHumanInterventionsV2(target.conversation))
        if (mine === ticket.current && sameInterventionTarget(target, latest.current)) {
          setPage(next); setPageKey(targetKey); setReadError(""); setReadUpdate(null)
          const unresolved = next.rows.filter((note) => !note.resolved_at).length
          if (rowReading.current.sessionID && rowReading.current.count !== unresolved)
            announceAttentionRead(rowReading.current.sessionID, unresolved)
        }
      } catch (error) {
        if (mine === ticket.current && sameInterventionTarget(target, latest.current)) { setReadError(failureWords(error)); setReadUpdate(asMachineNeedsUpdate(error)) }
      } finally {
        if (mine === ticket.current) setReading(false)
      }
    })()
    flight.current = { key: targetKey, promise }
    void promise.finally(() => { if (flight.current?.promise === promise) flight.current = null })
    return promise
  }, [])

  useEffect(() => {
    if (receiptTimer.current) clearTimeout(receiptTimer.current)
    if (collapseTimer.current) clearTimeout(collapseTimer.current)
    receiptTimer.current = null
    ticket.current++
    actionSerial.current++
    busyRef.current = false
    setPage(null); setPageKey(""); setReadError(""); setReadUpdate(null); setActionError(""); setActionStatus(""); setBusy(""); setSecretReceipt(null); setExpanded(false); setCollapsing(false)
    return () => { ticket.current++; if (receiptTimer.current) clearTimeout(receiptTimer.current); if (collapseTimer.current) clearTimeout(collapseTimer.current) }
  // Identity is an immutable machine / route Session / conversation tuple.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  // What this panel reads: the notes themselves when there are any to show or
  // the panel is open, and a lane only where the row cannot say
  // (attention-reads.ts).
  const { page: needsNotes, lane } = attentionReads({ counted, expanded, count: attentionCount })
  const hasPage = pageKey === key && page !== null

  // A Session with notes reads them as the page opens, so that opening the
  // panel shows the words and not a wait; a Session with none reads nothing,
  // and the head's dot and number are the row's own count.
  useEffect(() => {
    if (!destination || !needsNotes || hasPage) return
    void load(destination)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, needsNotes, hasPage, load])

  // The safety lane: like the to-dos, ask again only while the page is
  // visible, and at once when it comes back, is focused, or a Board item
  // changed through this page. There is none while a counted row's panel is
  // closed — the count it would be read for is already arriving.
  const everyMs = lane === "none" ? null : lane === "safety" ? TODO_SAFETY_MS : TODO_REFRESH_MS
  useEffect(() => {
    if (!destination || everyMs === null) return
    return watchTodoRefresh(() => { void load(destination) }, browserRefreshEnvironment(onWorkItemChanged), everyMs)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, everyMs, load])

  // The row counts this conversation's unresolved notes, and the count arrives
  // on the stream: a note written or resolved elsewhere is read when it moves,
  // not on the next tick.
  const seenCount = useRef(attentionCount)
  useEffect(() => {
    if (seenCount.current === attentionCount) return
    seenCount.current = attentionCount
    // A count that arrives where the head had none, or moves while this panel
    // has read nothing, needs no read: the head is drawing that number
    // (attention-reads.ts).
    if (!countMoveNeedsRead({ expanded, hasPage })) return
    if (destination) void load(destination)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attentionCount, expanded, hasPage, load])

  const closeWithFocus = () => {
    if (receiptTimer.current) clearTimeout(receiptTimer.current)
    if (collapseTimer.current) clearTimeout(collapseTimer.current)
    receiptTimer.current = null
    collapseTimer.current = null
    // A completed handoff already ran its exit; the next open resets it.
    setExpanded(false)
    document.querySelector<HTMLElement>(".human-interventions-head")?.focus({ preventScroll: true })
  }
  useEffect(() => {
    if (!expanded) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault(); event.stopPropagation()
      if (receiptTimer.current) clearTimeout(receiptTimer.current)
      if (collapseTimer.current) clearTimeout(collapseTimer.current)
      receiptTimer.current = null; collapseTimer.current = null
      setCollapsing(false)
      setExpanded(false)
      document.querySelector<HTMLElement>(".human-interventions-head")?.focus({ preventScroll: true })
    }
    document.addEventListener("keydown", closeOnEscape, true)
    return () => document.removeEventListener("keydown", closeOnEscape, true)
  }, [expanded])

  if (!row || !destination) return { head: null, live: null, body: null, close: () => setExpanded(false) }
  const shown = hasPage ? page : null
  const active = shown?.rows.filter((note) => !note.resolved_at) ?? []
  const recent = shown?.rows.filter((note) => !!note.resolved_at) ?? []
  // How many notes the head says are unresolved: the page this panel read, or
  // the row's own count off the stream, or null while nobody has said.
  const unresolved = unresolvedNoteCount(shown ? active.length : null, counted ? attentionCount : null)
  const run = async (note: HumanInterventionV2, action: "resolve" | "reopen") => {
    if (busyRef.current || readError || !sameInterventionTarget(destination, latest.current) || pageKey !== key) return
    const serial = ++actionSerial.current
    busyRef.current = true
    setBusy(note.id); setActionError(""); setActionStatus("")
    try {
      await humanInterventionActionV2(destination.conversation, note, action)
      await load(destination)
      if (sameInterventionTarget(destination, latest.current)) {
        setActionStatus(action === "resolve" ? catalogWord("literal", "75420f3866c1") : catalogWord("literal", "e0e3789d893d"))
        window.setTimeout(() => {
          document.querySelector<HTMLElement>(".human-interventions-head")?.focus({ preventScroll: true })
        }, 0)
      }
    }
    catch (error) {
      if (sameInterventionTarget(destination, latest.current)) {
        setActionError(failureWords(error))
        await load(destination)
      }
    }
    finally {
      if (serial === actionSerial.current) { busyRef.current = false; setBusy("") }
    }
  }
  // A choice among multiple suggested replies sends to the Root conversation.
  // The note is resolved only after delivery, so a cleared dot means an
  // answer was sent. A single suggestion enters the composer for discussion.
  // An already resolved note's multiple-choice reply can be sent again.
  const reply = async (note: HumanInterventionV2, text: string) => {
    if (!row || readError || busyRef.current || !sameInterventionTarget(destination, latest.current) || pageKey !== key) return
    const serial = ++actionSerial.current
    busyRef.current = true
    setBusy(note.id); setActionError(""); setActionStatus(catalogWord("literal", "2c09858c5eed"))
    const current = () => sameInterventionTarget(destination, latest.current)
    try {
      const card = pendingSends.add(row.id, text, [], Date.now())
      const code = await deliverUntilSeen(card)
      if (code) {
        // A definite refusal: the note's button is the one way to try again,
        // so the transcript does not keep a second retry for the same words.
        // An uncertain one keeps its card, which can look before resending.
        const failed = pendingSends.card(card.token)?.state === "failed"
        if (failed) pendingSends.dismiss(card.token)
        if (current()) {
          setActionStatus("")
          setActionError(failed
            ? catalogFormat("template", "f02affbcd988", [pendingFailureSentence(code), catalogWord("literal", "94e5b8af285e")])
            : catalogFormat("template", "a11c575f16d3", [pendingFailureSentence(code)]))
        }
        return
      }
      onReplySent?.()
      if (note.resolved_at) {
        if (current()) setActionStatus(catalogWord("literal", "cc8e34d543a9"))
        return
      }
      try {
        await humanInterventionActionV2(destination.conversation, note, "resolve")
        await load(destination)
        if (current()) setActionStatus(catalogWord("literal", "44a711826eb0"))
      } catch (error) {
        if (current()) {
          setActionStatus("")
          setActionError(catalogFormat("template", "e22e6512792a", [failureWords(error)]))
          await load(destination)
        }
      }
    } finally {
      if (serial === actionSerial.current) { busyRef.current = false; setBusy("") }
    }
  }
  const compose = (note: HumanInterventionV2, draft: string) => {
    if (readError || busyRef.current || !sameInterventionTarget(destination, latest.current) || pageKey !== key) return
    if (!onCompose) {
      setActionError(catalogWord("literal", "4fdbe22d40a1"))
      return
    }
    onCompose(interventionReplyText(note, draft))
    setExpanded(false)
    setActionError("")
  }

  const handoff = async (note: HumanInterventionV2, value: string) => {
    if (note.kind !== "secret" || readError || busyRef.current || !sameInterventionTarget(destination, latest.current) || pageKey !== key) return
    if (receiptTimer.current) clearTimeout(receiptTimer.current)
    if (collapseTimer.current) clearTimeout(collapseTimer.current)
    receiptTimer.current = null
    collapseTimer.current = null
    setCollapsing(false)
    const serial = ++actionSerial.current
    busyRef.current = true
    setBusy(note.id); setActionError(""); setActionStatus(""); setSecretReceipt({ id: note.id, phase: "sending" })
    let received = false
    try {
      await submitSecretHandoffV2(destination.conversation, note.id, value)
      received = true
      if (sameInterventionTarget(destination, latest.current)) {
        setSecretReceipt({ id: note.id, phase: "received" })
        receiptTimer.current = window.setTimeout(() => {
          if (!sameInterventionTarget(destination, latest.current)) return
          setCollapsing(true)
          collapseTimer.current = window.setTimeout(() => closeWithFocus(), 260)
        }, 1300)
      }
      await load(destination)
      if (row) {
        const card = pendingSends.add(row.id, nextWord("secretHandoffReadyMessage", { id: note.id }), [], Date.now())
        const code = await deliverUntilSeen(card)
        if (code && sameInterventionTarget(destination, latest.current)) {
          if (receiptTimer.current) clearTimeout(receiptTimer.current)
          if (collapseTimer.current) clearTimeout(collapseTimer.current)
          receiptTimer.current = null
          collapseTimer.current = null
          setCollapsing(false)
          setActionError(nextWord("secretHandoffNoticeError"))
        }
        if (!code) onReplySent?.()
      }
    } catch (error) {
      // The request may have reached the machine. The value is cleared by
      // the form; never put it into an error, retry card or transcript.
      if (sameInterventionTarget(destination, latest.current)) {
        if (receiptTimer.current) clearTimeout(receiptTimer.current)
        if (collapseTimer.current) clearTimeout(collapseTimer.current)
        receiptTimer.current = null
        collapseTimer.current = null
        setCollapsing(false)
        if (!received) setSecretReceipt(null)
        setActionError(received ? nextWord("secretHandoffNoticeError") : failureWords(error))
      }
    } finally {
      if (serial === actionSerial.current) { busyRef.current = false; setBusy("") }
    }
  }

  const countWords = readUpdate ? `${catalogWord("inline", "a4f8e0fcaaf8")}: ${nextWord("machineNeedsUpdate")}` : readError ? catalogWord("literal", "ff569bc2e31d") : unresolved !== null ? catalogFormat("template", "f16df8b87a41", [unresolved]) : catalogWord("literal", "e44c005aa593")
  const head = <button className="human-interventions-head" type="button" aria-expanded={expanded} aria-controls={expanded ? "human-interventions-body" : undefined}
    aria-label={`${countWords}${actionError ? catalogWord("literal", "f095f6af37ff") : ""}`}
    onClick={(event) => { event.preventDefault(); event.stopPropagation(); if (receiptTimer.current) clearTimeout(receiptTimer.current); if (collapseTimer.current) clearTimeout(collapseTimer.current); receiptTimer.current = null; collapseTimer.current = null; setCollapsing(false); if (readError) { void load(destination); return } if (!expanded) { onExpand?.(); setActionStatus("") } setExpanded(!expanded) }}>
      <span>{catalogWord("inline", "a4f8e0fcaaf8")}</span>
      {(unresolved ?? 0) > 0 && <span className="human-interventions-dot" aria-hidden="true" />}
      {unresolved !== null && unresolved > 0 && <span className="human-interventions-count">{catalogFormat("count", "pendingInterventions", [unresolved])}</span>}
      {actionError && <span className="human-interventions-count human-interventions-failed">{catalogWord("inline", "aa065ac6118d")}</span>}
      {readError && <span className="human-interventions-count">{readUpdate ? nextWord("machineNeedsUpdateShort") : catalogWord("inline", "88cd4f4d97f1")}</span>}
      {reading && unresolved === null && !readError && <span className="human-interventions-count">{catalogWord("inline", "656c48dad32b")}</span>}
    </button>
  const live = <>
    <span className="human-interventions-live" role="status" aria-live="polite" aria-atomic="true">{unresolved !== null || readError ? countWords : ""}</span>
    {actionError && <span className="human-interventions-live" role="alert">{catalogWord("inline", "9aaa26b21b0c")}</span>}
  </>
  const body = <section ref={panel} className={`human-interventions${collapsing ? " is-collapsing" : ""}`} aria-labelledby="human-interventions-title"
    hidden={!expanded} inert={!expanded} data-motion-id="AN-004">
    <div className="human-interventions-content">
    <div className="human-interventions-panelbar"><h2 id="human-interventions-title">{catalogWord("inline", "0999edae13a4")}</h2><button type="button" onClick={closeWithFocus}><WorkIcon name="close" />{catalogWord("inline", "1fa4945f108c")}</button></div>
    <div className="human-interventions-body" id="human-interventions-body">
      {readUpdate ? <NeedsUpdate update={readUpdate} className="human-interventions-error" /> : readError && <p className="human-interventions-error" role="alert">{shown ? catalogWord("literal", "5789b797c9ef") : catalogWord("literal", "24fe20ab6d4d")} {readError} <button type="button" onClick={() => { void load(destination) }}>{catalogWord("inline", "7e59d0f16293")}</button></p>}
      {actionError && <p className="human-interventions-error" role="alert">{actionError}</p>}
      {actionStatus && <p className="human-interventions-status" role="status">{actionStatus}</p>}
      {!shown && !readError && <p className="human-interventions-empty human-interventions-loading" role="status"><span className="human-interventions-spinner" aria-hidden="true" />{catalogWord("inline", "656c48dad32b")}</p>}
      {shown && active.length === 0 && <p className="human-interventions-empty">{catalogWord("inline", "e43b3c54e4b6")}</p>}
      {/* Closing still unmounts secret fields immediately; only ordinary Note
          content remains mounted for the non-interactive exit picture. */}
      {active.filter((note) => expanded || note.kind !== "secret").map((note) => <InterventionCard key={note.id} note={note} disabled={!!busy || !!readError} sending={busy === note.id} secretReceived={!!note.secret_received || secretReceipt?.id === note.id && secretReceipt.phase === "received"} onReplace={() => { if (receiptTimer.current) clearTimeout(receiptTimer.current); if (collapseTimer.current) clearTimeout(collapseTimer.current); receiptTimer.current = null; collapseTimer.current = null; setCollapsing(false) }} onAction={run} onReply={reply} onCompose={compose} onHandoff={handoff} />)}
      {recent.length > 0 && <details className="human-interventions-recent"><summary>{catalogFormat("count", "recentInterventions", [recent.length])}</summary>
        {recent.map((note) => <InterventionCard key={note.id} note={note} disabled={!!busy || !!readError} sending={busy === note.id} secretReceived={false} onReplace={() => {}} onAction={run} onReply={reply} onCompose={compose} onHandoff={handoff} />)}
      </details>}
      {!!shown?.pruned_resolved && <p className="human-interventions-retention">{catalogFormat("count", "prunedInterventions", [shown.pruned_resolved])}</p>}
    </div>
    </div>
    <button className="human-interventions-backdrop" type="button" tabIndex={-1} aria-label={catalogWord("inline", "de6c2fb83e07")}
      onClick={closeWithFocus} />
  </section>
  return { head, live, body, close: () => setExpanded(false) }
}

function InterventionCard({ note, disabled, sending, secretReceived, onReplace, onAction, onReply, onCompose, onHandoff }: {
  note: HumanInterventionV2
  disabled: boolean
  sending: boolean
  secretReceived: boolean
  onReplace: () => void
  onAction: (note: HumanInterventionV2, action: "resolve" | "reopen") => void
  onReply: (note: HumanInterventionV2, text: string) => void
  onCompose: (note: HumanInterventionV2, draft: string) => void
  onHandoff: (note: HumanInterventionV2, value: string) => void
}) {
  const [replacingSecret, setReplacingSecret] = useState(false)
  const [pendingAction, setPendingAction] = useState("")
  useEffect(() => { if (secretReceived && !sending) setReplacingSecret(false) }, [secretReceived, sending])
  const fromAnotherSession = note.source_conversation !== note.target_conversation
  const isReading = note.kind === "read" || note.kind === "report"
  const composeOnly = note.options.length <= 1
  const documentHere = note.document_url ? samePageTarget(note.document_url, window.location) : null
  const internalDocument = documentHere && hasDocumentIntent(documentHere.hash)
  return <article className={`human-intervention-card${note.withdrawn ? " human-intervention-withdrawn" : ""}`} data-intervention-id={note.id} tabIndex={-1} aria-label={note.title}>
    <div className="human-intervention-title"><div><h3>{note.title}</h3><span className="human-intervention-stage">{note.withdrawn ? nextWord("noteWithdrawn") : note.resolved_at ? catalogWord("literal", "927c9ed33728") : catalogWord("literal", "77f6d2cceedd")}</span><time className="human-intervention-created" dateTime={new Date(note.created_at * 1000).toISOString()}>{catalogWord("inline", "0f6f6df015c6")} {when(note.created_at)}</time></div><span className="human-intervention-type"><WorkIcon name={isReading ? "eye" : "edit"} />{note.kind === "secret" ? nextWord("secretHandoffName") : isReading ? catalogWord("literal", "3d9157962dd2") : note.kind === "answer" ? catalogWord("literal", "e12820431ff9") : catalogWord("literal", "5a15e03eaa4a")}</span></div>
    {note.withdrawn && <p className="human-intervention-withdrawal">{nextWord("noteWithdrawalReason")} {note.resolution}</p>}
    {fromAnotherSession && <p className="human-intervention-source">{catalogWord("inline", "afc7f76a7d4f")} {note.source_label || catalogWord("literal", "92018c889270")}</p>}
    {(note.kind !== "secret" || note.resolved_at) && <p>{note.summary}</p>}
    <p><b>{catalogLabel("inline", "7ed8d242eabd")}</b>{note.action}</p>
    <p className="human-intervention-reason">{catalogLabel("inline", "b9fbb8ede4cb")}{note.reason}</p>
    {note.document_url && <p><a className="human-intervention-document" href={note.document_url}
      target={internalDocument ? undefined : "_blank"} rel="noopener noreferrer"
      onClick={(event) => {
        if (!documentHere || !internalDocument || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        requestPage({ page: "documents", hash: false })
        window.location.hash = documentHere.hash
      }}><WorkIcon name="file" />{catalogWord("inline", "740cf7e11430")}</a></p>}
    {note.detail && <details className="human-intervention-more"><summary>{catalogWord("inline", "61e5b2be1827")}</summary><div className="human-intervention-detail" dangerouslySetInnerHTML={{ __html: L.richTextHTML(note.detail) }} /></details>}
    {note.kind === "secret" && !note.resolved_at && (secretReceived && !replacingSecret
      ? <div className="secret-handoff-received" role="status"><WorkIcon name="check" /><span>{nextWord("secretHandoffReceived")}</span><button type="button" disabled={disabled} onClick={() => { onReplace(); setReplacingSecret(true) }}>{nextWord("secretHandoffReplace")}</button></div>
      : <SecretHandoffForm note={note} disabled={disabled} sending={sending} onHandoff={onHandoff} />)}
    {note.kind !== "secret" && !note.withdrawn && <div className="human-intervention-drafts" role="group" aria-label={catalogWord("inline", "c4636462e1de")} aria-busy={sending}>
      <h4>{catalogWord("inline", "c4636462e1de")}</h4>
      <p className="human-intervention-hint">{composeOnly ? catalogWord("literal", "981ea9fd14d5") : catalogWord("literal", "3e129c69ac26")}</p>
      {(note.options.length ? note.options : [{ label: catalogWord("literal", "cc6ade09e09b"), draft: note.action }]).map((option, index) =>
        <button className="human-intervention-option" type="button" key={`${index}:${option.label}`}
          disabled={disabled} aria-label={labelled(composeOnly ? catalogWord("literal", "e3949613add9") : note.resolved_at ? catalogWord("literal", "7d6cc5d58935") : catalogWord("literal", "2ffd9b695259"), option.label) + clauseSeparator() + option.draft} aria-busy={sending && pendingAction === "reply:" + index} onClick={() => { if (composeOnly) onCompose(note, option.draft); else { setPendingAction("reply:" + index); onReply(note, interventionReplyText(note, option.draft)) } }}>
          {sending && pendingAction === "reply:" + index && <span className="action-progress" role="status"><ActionSpinner />{catalogWord("literal", "84b95f498939")}</span>}
          {note.options.length > 0 && <strong>{option.label}</strong>}
          <span>{option.draft}</span>
        </button>)}
    </div>}
    <div className="human-intervention-state">
      <div className="human-intervention-actions">
        {!note.resolved_at ? <button className="human-intervention-resolve" type="button" disabled={disabled} aria-busy={sending && pendingAction === "resolve"} onClick={() => { setPendingAction("resolve"); onAction(note, "resolve") }}><ActionProgress busy={sending && pendingAction === "resolve"}>{!(sending && pendingAction === "resolve") && <WorkIcon name="check" />}{catalogWord("inline", "f51bfc3ed707")}</ActionProgress></button>
          : <button className="human-intervention-reopen" type="button" disabled={disabled} aria-busy={sending && pendingAction === "reopen"} onClick={() => { setPendingAction("reopen"); onAction(note, "reopen") }}><ActionProgress busy={sending && pendingAction === "reopen"}>{catalogWord("inline", "507ec6638095")}</ActionProgress></button>}
      </div>
    </div>
  </article>
}

function interventionReplyText(note: HumanInterventionV2, draft: string): string {
  const context = `(Clawdline 便條 ${note.id}：「${note.title}」；待回覆事項：${note.action})`
  return `${draft.replace(/[\r\n]+$/, "")}\n\n${context}`
}

function SecretHandoffForm({ note, disabled, sending, onHandoff }: {
  note: HumanInterventionV2
  disabled: boolean
  sending: boolean
  onHandoff: (note: HumanInterventionV2, value: string) => void
}) {
  const field = useRef<HTMLTextAreaElement | null>(null)
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const value = field.current?.value ?? ""
    if (!value || disabled || sending) return
    if (field.current) field.current.value = ""
    onHandoff(note, value)
  }
  return <form className="secret-handoff-form" onSubmit={submit} aria-busy={sending}>
    <label htmlFor={`secret-handoff-${note.id}`}>{note.summary || nextWord("secretHandoffLabel")}</label>
    <textarea ref={field} id={`secret-handoff-${note.id}`} rows={5} required disabled={disabled}
      placeholder={nextWord("secretHandoffPlaceholder")} autoComplete="off" autoCapitalize="off" spellCheck={false} data-1p-ignore="" data-lpignore="true" data-bwignore="" />
    <p>{nextWord("secretHandoffPrivacy")}</p>
    <button type="submit" disabled={disabled || sending}>{sending ? <><span className="human-interventions-spinner" aria-hidden="true" />{nextWord("secretHandoffSending")}</> : nextWord("secretHandoffSubmit")}</button>
  </form>
}
