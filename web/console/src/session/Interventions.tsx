import { useCallback, useEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
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
          setPage(next); setPageKey(targetKey); setReadError("")
        }
      } catch (error) {
        if (mine === ticket.current && sameInterventionTarget(target, latest.current)) setReadError(failureWords(error))
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
    setPage(null); setPageKey(""); setReadError(""); setActionError(""); setActionStatus(""); setBusy(""); setExpanded(false)
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
        setActionStatus(action === "resolve" ? "已移到最近已處理" : "已重新開啟")
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
    setBusy(note.id); setActionError(""); setActionStatus("正在送出回覆…")
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
            ? `回覆沒有送出，便條仍待處理。${pendingFailureSentence(code)}${pendingFailureCanRetry(code) ? " 可以再點一次建議回覆重試。" : ""}`
            : `無法確認回覆是否送出，便條仍待處理。請先在對話中查看這則訊息，確認沒送出再重試。${pendingFailureSentence(code)}`)
        }
        return
      }
      onReplySent?.()
      if (note.resolved_at) {
        if (current()) setActionStatus("回覆已送出")
        return
      }
      try {
        await humanInterventionActionV2(destination.conversation, note, "resolve")
        await load(destination)
        if (current()) setActionStatus("回覆已送出，便條已移到最近已處理")
      } catch (error) {
        if (current()) {
          setActionStatus("")
          setActionError(`回覆已送出，但便條仍待處理：${failureWords(error)} 請按「移到已處理」重試，不必再送一次回覆。`)
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
      setActionError("無法開啟對話輸入框，請先開啟這個 Session 再試一次。")
      return
    }
    onCompose(interventionReplyText(note, draft))
    setExpanded(false)
    setActionError("")
  }

  const countWords = readError ? "關注便條讀取失敗" : shown ? `需要你關注，${active.length} 筆未處理便條` : "關注便條載入中"
  const head = <button className="human-interventions-head" type="button" aria-expanded={expanded} aria-controls={expanded ? "human-interventions-body" : undefined}
    aria-label={`${countWords}${actionError ? "，操作失敗，請展開查看" : ""}`}
    onClick={(event) => { event.preventDefault(); event.stopPropagation(); if (readError) { void load(destination); return } if (!expanded) { onExpand?.(); setActionStatus("") } setExpanded(!expanded) }}>
      <span>關注</span>
      {active.length > 0 && <span className="human-interventions-dot" aria-hidden="true" />}
      {shown && active.length > 0 && <span className="human-interventions-count">待處理 {active.length}</span>}
      {actionError && <span className="human-interventions-count human-interventions-failed">操作失敗</span>}
      {readError && <span className="human-interventions-count">讀取失敗</span>}
      {reading && !shown && !readError && <span className="human-interventions-count">載入中</span>}
    </button>
  const live = <>
    <span className="human-interventions-live" role="status" aria-live="polite" aria-atomic="true">{shown || readError ? countWords : ""}</span>
    {actionError && <span className="human-interventions-live" role="alert">操作失敗，請展開關注便條查看。</span>}
  </>
  const body = expanded && <section className="human-interventions" aria-labelledby="human-interventions-title">
    <div className="human-interventions-panelbar"><h2 id="human-interventions-title">需要你關注</h2><button type="button" onClick={closeWithFocus}><WorkIcon name="close" />收起關注</button></div>
    <div className="human-interventions-body" id="human-interventions-body">
      {readError && <p className="human-interventions-error" role="alert">{shown ? "資料可能已過期，請重試；更新前不能操作便條。" : "無法讀取便條。"} {readError} <button type="button" onClick={() => { void load(destination) }}>重試</button></p>}
      {actionError && <p className="human-interventions-error" role="alert">{actionError}</p>}
      {actionStatus && <p className="human-interventions-status" role="status">{actionStatus}</p>}
      {shown && active.length === 0 && <p className="human-interventions-empty">目前沒有需要處理的便條。</p>}
      {active.map((note) => <InterventionCard key={note.id} note={note} disabled={!!busy || !!readError} sending={busy === note.id} onAction={run} onReply={reply} onCompose={compose} />)}
      {recent.length > 0 && <details className="human-interventions-recent"><summary>最近已處理（{recent.length}）</summary>
        {recent.map((note) => <InterventionCard key={note.id} note={note} disabled={!!busy || !!readError} sending={busy === note.id} onAction={run} onReply={reply} onCompose={compose} />)}
      </details>}
      {!!shown?.pruned_resolved && <p className="human-interventions-retention">本機已清理 {shown.pruned_resolved} 筆較舊的已處理便條。</p>}
    </div>
    <button className="human-interventions-backdrop" type="button" tabIndex={-1} aria-label="收起關注便條"
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
    <div className="human-intervention-title"><div><h3>{note.title}</h3><span className="human-intervention-stage">{note.resolved_at ? "已處理" : "待處理"}</span><time className="human-intervention-created" dateTime={new Date(note.created_at * 1000).toISOString()}>送達 {when(note.created_at)}</time></div><span className="human-intervention-type"><WorkIcon name={isReading ? "eye" : "edit"} />{isReading ? "請閱讀" : note.kind === "answer" ? "請回覆" : "請處理"}</span></div>
    {fromAnotherSession && <p className="human-intervention-source">來自 {note.source_label || "其他 Session"}</p>}
    <p>{note.summary}</p>
    <p><b>需要你做：</b>{note.action}</p>
    <p className="human-intervention-reason">原因：{note.reason}</p>
    {note.document_url && <p><a className="human-intervention-document" href={note.document_url} target="_blank" rel="noopener noreferrer"><WorkIcon name="file" />在新分頁開啟文件</a></p>}
    {note.detail && <details className="human-intervention-more"><summary>閱讀完整內容</summary><div className="human-intervention-detail" dangerouslySetInnerHTML={{ __html: L.richTextHTML(note.detail) }} /></details>}
    <div className="human-intervention-drafts" role="group" aria-label="建議回覆" aria-busy={sending}>
      <h4>建議回覆</h4>
      <p className="human-intervention-hint">{composeOnly ? "點一下填入對話框，確認或修改後再送出。" : "點一下就直接送出到對話。"}</p>
      {(note.options.length ? note.options : [{ label: "待辦文字", draft: note.action }]).map((option, index) =>
        <button className="human-intervention-option" type="button" key={`${index}:${option.label}`}
          disabled={disabled} aria-label={`${composeOnly ? "填入對話框" : note.resolved_at ? "再次送出這個回覆" : "送出這個回覆並移到已處理"}：${option.label}，${option.draft}`} onClick={() => composeOnly ? onCompose(note, option.draft) : onReply(note, interventionReplyText(note, option.draft))}>
          {note.options.length > 0 && <strong>{option.label}</strong>}
          <span>{option.draft}</span>
        </button>)}
    </div>
    <div className="human-intervention-state">
      <div className="human-intervention-actions">
        {!note.resolved_at ? <button className="human-intervention-resolve" type="button" disabled={disabled} onClick={() => onAction(note, "resolve")}><WorkIcon name="check" />移到已處理</button>
          : <button className="human-intervention-reopen" type="button" disabled={disabled} onClick={() => onAction(note, "reopen")}>重新開啟提醒</button>}
      </div>
    </div>
  </article>
}

function interventionReplyText(note: HumanInterventionV2, draft: string): string {
  const context = `(Clawdline 便條 ${note.id}：「${note.title}」；待回覆事項：${note.action})`
  return `${draft.replace(/[\r\n]+$/, "")}\n\n${context}`
}
