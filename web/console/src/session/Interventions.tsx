import { catalogFormat } from "../catalog.js"
import { catalogWord } from "../catalog.js"
import { useCallback, useEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { asMachineNeedsUpdate, type MachineNeedsUpdate } from "@clawdline/core"
import { NeedsUpdate } from "../machine/NeedsUpdate.js"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import { humanInterventionActionV2, readHumanInterventionsV2, type HumanInterventionV2, type HumanInterventionsV2 } from "../pages/work/api.js"
import { failureWords, when } from "../pages/work/shared.js"
import { WorkIcon } from "../pages/work/WorkIcon.js"
import { interventionTarget, sameInterventionTarget, type InterventionTarget } from "./intervention-composer.js"
import { pendingFailureCanRetry, pendingFailureSentence } from "./pending-copy.js"
import { deliverUntilSeen, pendingSends } from "./send.js"
import { browserRefreshEnvironment, readWithOneRetry, watchTodoRefresh } from "./todo-refresh.js"
import { onWorkItemChanged } from "../pages/work/item-changed.js"
import "./interventions.css"

/** Keep the attention entry in the todo header while its panel stays independent. */
export function useInterventions(row: SessionRow | null, onReplySent?: () => void, onExpand?: () => void, onCompose?: (text: string) => void) {
  const destination = interventionTarget(row)
  const key = destination ? JSON.stringify(destination) : ""
  const latest = useRef<InterventionTarget | null>(destination)
  latest.current = destination
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
  const busyRef = useRef(false)
  const actionSerial = useRef(0)
  const [expanded, setExpanded] = useState(false)

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
    ticket.current++
    actionSerial.current++
    busyRef.current = false
    setPage(null); setPageKey(""); setReadError(""); setReadUpdate(null); setActionError(""); setActionStatus(""); setBusy(""); setExpanded(false)
    if (!destination) return
    void load(destination)
    // Like the to-dos: ask again only while the page is visible, and at once
    // when it comes back.
    const stop = watchTodoRefresh(() => { void load(destination) }, browserRefreshEnvironment(onWorkItemChanged))
    return () => { ticket.current++; stop() }
  // Identity is an immutable machine / route Session / conversation tuple.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, load])

  const closeWithFocus = () => {
    setExpanded(false)
    document.querySelector<HTMLElement>(".human-interventions-head")?.focus({ preventScroll: true })
  }
  useEffect(() => {
    if (!expanded) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault(); event.stopPropagation()
      setExpanded(false)
      document.querySelector<HTMLElement>(".human-interventions-head")?.focus({ preventScroll: true })
    }
    document.addEventListener("keydown", closeOnEscape, true)
    return () => document.removeEventListener("keydown", closeOnEscape, true)
  }, [expanded])

  if (!row || !destination) return { head: null, live: null, body: null, close: () => setExpanded(false) }
  const shown = pageKey === key ? page : null
  const active = shown?.rows.filter((note) => !note.resolved_at) ?? []
  const recent = shown?.rows.filter((note) => !!note.resolved_at) ?? []
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

  const countWords = readUpdate ? `${catalogWord("inline", "a4f8e0fcaaf8")}: ${nextWord("machineNeedsUpdate")}` : readError ? catalogWord("literal", "ff569bc2e31d") : shown ? catalogFormat("template", "f16df8b87a41", [active.length]) : catalogWord("literal", "e44c005aa593")
  const head = <button className="human-interventions-head" type="button" aria-expanded={expanded} aria-controls={expanded ? "human-interventions-body" : undefined}
    aria-label={`${countWords}${actionError ? catalogWord("literal", "f095f6af37ff") : ""}`}
    onClick={(event) => { event.preventDefault(); event.stopPropagation(); if (readError) { void load(destination); return } if (!expanded) { onExpand?.(); setActionStatus("") } setExpanded(!expanded) }}>
      <span>{catalogWord("inline", "a4f8e0fcaaf8")}</span>
      {active.length > 0 && <span className="human-interventions-dot" aria-hidden="true" />}
      {shown && active.length > 0 && <span className="human-interventions-count">{catalogFormat("count", "pendingInterventions", [active.length])}</span>}
      {actionError && <span className="human-interventions-count human-interventions-failed">{catalogWord("inline", "aa065ac6118d")}</span>}
      {readError && <span className="human-interventions-count">{readUpdate ? nextWord("machineNeedsUpdateShort") : catalogWord("inline", "88cd4f4d97f1")}</span>}
      {reading && !shown && !readError && <span className="human-interventions-count">{catalogWord("inline", "656c48dad32b")}</span>}
    </button>
  const live = <>
    <span className="human-interventions-live" role="status" aria-live="polite" aria-atomic="true">{shown || readError ? countWords : ""}</span>
    {actionError && <span className="human-interventions-live" role="alert">{catalogWord("inline", "9aaa26b21b0c")}</span>}
  </>
  const body = expanded && <section className="human-interventions" aria-labelledby="human-interventions-title">
    <div className="human-interventions-panelbar"><h2 id="human-interventions-title">{catalogWord("inline", "0999edae13a4")}</h2><button type="button" onClick={closeWithFocus}><WorkIcon name="close" />{catalogWord("inline", "1fa4945f108c")}</button></div>
    <div className="human-interventions-body" id="human-interventions-body">
      {readUpdate ? <NeedsUpdate update={readUpdate} className="human-interventions-error" /> : readError && <p className="human-interventions-error" role="alert">{shown ? catalogWord("literal", "5789b797c9ef") : catalogWord("literal", "24fe20ab6d4d")} {readError} <button type="button" onClick={() => { void load(destination) }}>{catalogWord("inline", "7e59d0f16293")}</button></p>}
      {actionError && <p className="human-interventions-error" role="alert">{actionError}</p>}
      {actionStatus && <p className="human-interventions-status" role="status">{actionStatus}</p>}
      {shown && active.length === 0 && <p className="human-interventions-empty">{catalogWord("inline", "e43b3c54e4b6")}</p>}
      {active.map((note) => <InterventionCard key={note.id} note={note} disabled={!!busy || !!readError} sending={busy === note.id} onAction={run} onReply={reply} onCompose={compose} />)}
      {recent.length > 0 && <details className="human-interventions-recent"><summary>{catalogFormat("count", "recentInterventions", [recent.length])}</summary>
        {recent.map((note) => <InterventionCard key={note.id} note={note} disabled={!!busy || !!readError} sending={busy === note.id} onAction={run} onReply={reply} onCompose={compose} />)}
      </details>}
      {!!shown?.pruned_resolved && <p className="human-interventions-retention">{catalogFormat("count", "prunedInterventions", [shown.pruned_resolved])}</p>}
    </div>
    <button className="human-interventions-backdrop" type="button" tabIndex={-1} aria-label={catalogWord("inline", "de6c2fb83e07")}
      onClick={closeWithFocus} />
  </section>
  return { head, live, body, close: () => setExpanded(false) }
}

function InterventionCard({ note, disabled, sending, onAction, onReply, onCompose }: {
  note: HumanInterventionV2
  disabled: boolean
  sending: boolean
  onAction: (note: HumanInterventionV2, action: "resolve" | "reopen") => void
  onReply: (note: HumanInterventionV2, text: string) => void
  onCompose: (note: HumanInterventionV2, draft: string) => void
}) {
  const fromAnotherSession = note.source_conversation !== note.target_conversation
  const isReading = note.kind === "read" || note.kind === "report"
  const composeOnly = note.options.length <= 1
  return <article className="human-intervention-card" data-intervention-id={note.id} tabIndex={-1} aria-label={note.title}>
    <div className="human-intervention-title"><div><h3>{note.title}</h3><span className="human-intervention-stage">{note.resolved_at ? catalogWord("literal", "927c9ed33728") : catalogWord("literal", "77f6d2cceedd")}</span><time className="human-intervention-created" dateTime={new Date(note.created_at * 1000).toISOString()}>{catalogWord("inline", "0f6f6df015c6")} {when(note.created_at)}</time></div><span className="human-intervention-type"><WorkIcon name={isReading ? "eye" : "edit"} />{isReading ? catalogWord("literal", "3d9157962dd2") : note.kind === "answer" ? catalogWord("literal", "e12820431ff9") : catalogWord("literal", "5a15e03eaa4a")}</span></div>
    {fromAnotherSession && <p className="human-intervention-source">{catalogWord("inline", "afc7f76a7d4f")} {note.source_label || catalogWord("literal", "92018c889270")}</p>}
    <p>{note.summary}</p>
    <p><b>{catalogWord("inline", "7ed8d242eabd")}</b>{note.action}</p>
    <p className="human-intervention-reason">{catalogWord("inline", "b9fbb8ede4cb")}{note.reason}</p>
    {note.document_url && <p><a className="human-intervention-document" href={note.document_url} target="_blank" rel="noopener noreferrer"><WorkIcon name="file" />{catalogWord("inline", "740cf7e11430")}</a></p>}
    {note.detail && <details className="human-intervention-more"><summary>{catalogWord("inline", "61e5b2be1827")}</summary><div className="human-intervention-detail" dangerouslySetInnerHTML={{ __html: L.richTextHTML(note.detail) }} /></details>}
    <div className="human-intervention-drafts" role="group" aria-label={catalogWord("inline", "c4636462e1de")} aria-busy={sending}>
      <h4>{catalogWord("inline", "c4636462e1de")}</h4>
      <p className="human-intervention-hint">{composeOnly ? catalogWord("literal", "981ea9fd14d5") : catalogWord("literal", "3e129c69ac26")}</p>
      {(note.options.length ? note.options : [{ label: catalogWord("literal", "cc6ade09e09b"), draft: note.action }]).map((option, index) =>
        <button className="human-intervention-option" type="button" key={`${index}:${option.label}`}
          disabled={disabled} aria-label={`${composeOnly ? catalogWord("literal", "e3949613add9") : note.resolved_at ? catalogWord("literal", "7d6cc5d58935") : catalogWord("literal", "2ffd9b695259")}：${option.label}，${option.draft}`} onClick={() => composeOnly ? onCompose(note, option.draft) : onReply(note, interventionReplyText(note, option.draft))}>
          {note.options.length > 0 && <strong>{option.label}</strong>}
          <span>{option.draft}</span>
        </button>)}
    </div>
    <div className="human-intervention-state">
      <div className="human-intervention-actions">
        {!note.resolved_at ? <button className="human-intervention-resolve" type="button" disabled={disabled} onClick={() => onAction(note, "resolve")}><WorkIcon name="check" />{catalogWord("inline", "f51bfc3ed707")}</button>
          : <button className="human-intervention-reopen" type="button" disabled={disabled} onClick={() => onAction(note, "reopen")}>{catalogWord("inline", "507ec6638095")}</button>}
      </div>
    </div>
  </article>
}

function interventionReplyText(note: HumanInterventionV2, draft: string): string {
  const context = `(Clawdline 便條 ${note.id}：「${note.title}」；待回覆事項：${note.action})`
  return `${draft.replace(/[\r\n]+$/, "")}\n\n${context}`
}
