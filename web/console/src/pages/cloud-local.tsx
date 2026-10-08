import { useCallback, useEffect, useRef, useState } from "react"
import type { PageModule } from "./types.js"
import { destinationFragment, destinationFromFragment } from "../cloud/all-machine-sessions.js"
import "./cloud-local.css"

interface Destination { machine_id: string; session_id: string; execution_generation: string }
interface StatusRow { destination: Destination; state: string; freshness: string; needs_attention?: boolean }
interface Projection { kind: string; reason?: string; rows?: StatusRow[]; unknown_targets?: number }
interface Machine { id: string; name: string; fingerprint: string; pairing: string; status: string; reason?: string }
interface Question { text?: string; fingerprint: string; options: { key: string; label: string }[] }
interface TranscriptEntry { role: string; text: string; at?: number }
interface TranscriptPage { entries: TranscriptEntry[]; nextBefore?: number }
interface Detail { destination: Destination; info: unknown; transcript: TranscriptPage; next_before?: number; question?: Question | null }
interface OlderPage { destination: Destination; transcript: TranscriptPage; next_before?: number }
interface Receipt { request: string; action: string; execution_generation: string; machine_execution: string; status: number; code: string }

const copy = {
  en: {
    title: "Cloud Sessions", refresh: "Refresh", loading: "Checking Cloud Sessions…",
    disabled: "Turn Cloud on to use Sessions on other machines.",
    login: "Authorize this local viewer with clawdline cloud viewer login, then pair each machine with clawdline cloud viewer pair.",
    local: "Open this Console with clawdline open to use the local viewer authorization.",
    noMachines: "No Cloud machines are visible.", pair: "Pair this viewer with the machine before reading its Sessions.",
    noRows: "No Sessions in this verified snapshot.", unknown: "Some Sessions have no verified execution generation.",
    info: "Session info", transcript: "Transcript", back: "Back to machines", read: "Read detail",
    older: "Load older messages", loadingOlder: "Loading older messages…", allLoaded: "Beginning of conversation reached.",
    send: "Send", answer: "Answer", interrupt: "Interrupt", end: "End Session", text: "Message",
    answerKey: "Answer key", expect: "Question fingerprint", request: "Request ID", lookup: "Check receipt",
    receipt: "Machine execution", uncertain: "The outcome is unknown. Check the receipt before another action.",
    pending: "An earlier request is unresolved. Check and acknowledge its receipt before another action.",
    acknowledge: "Acknowledge result", action: "Action",
    changed: "This Session changed execution. Select its current row again.",
    stale: "The Session is not current. Refresh before acting.",
  },
  zh: {
    title: "跨機工作階段", refresh: "重新整理", loading: "正在檢查 Cloud 工作階段…",
    disabled: "請先開啟 Cloud，才能使用其他機器的工作階段。",
    login: "請執行 clawdline cloud viewer login 授權本機檢視端，再以 clawdline cloud viewer pair 配對各機器。",
    local: "請使用 clawdline open 開啟本機 Console，以使用本機檢視端授權。",
    noMachines: "目前看不到 Cloud 機器。", pair: "讀取工作階段前，請先將此檢視端與機器配對。",
    noRows: "已驗證的快照中沒有工作階段。", unknown: "部分工作階段尚無可驗證的執行世代。",
    info: "工作階段資訊", transcript: "逐字紀錄", back: "返回機器清單", read: "讀取詳情",
    older: "載入較早訊息", loadingOlder: "正在載入較早訊息…", allLoaded: "已載入對話開頭。",
    send: "傳送", answer: "回答", interrupt: "中斷", end: "結束工作階段", text: "訊息",
    answerKey: "答案代碼", expect: "問題指紋", request: "請求 ID", lookup: "查詢收據",
    receipt: "機器執行結果", uncertain: "結果尚未確定。再次操作前請先查詢收據。",
    pending: "前一筆請求尚未解決。再次操作前請先查詢並確認收據。",
    acknowledge: "確認結果", action: "操作",
    changed: "此工作階段已換成新的執行世代，請重新選取目前的資料列。",
    stale: "工作階段狀態已過期；操作前請重新整理。",
  },
}

function words() { return /^zh(?:-|$)/i.test(document.documentElement.lang) ? copy.zh : copy.en }

function same(a: Destination, b: Destination) {
  return a.machine_id === b.machine_id && a.session_id === b.session_id && a.execution_generation === b.execution_generation
}

function query(target: Destination): string {
  return new URLSearchParams({ machine: target.machine_id, session: target.session_id, generation: target.execution_generation }).toString()
}

async function localViewerFetch<T>(path: string, signal?: AbortSignal, body?: unknown): Promise<T> {
  const response = await fetch(path, { method: body === undefined ? "GET" : "POST", credentials: "same-origin", signal,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body) })
  const data = await response.json().catch(() => null) as { error?: { code?: string }; code?: string } | null
  if (!response.ok) throw new Error(data?.error?.code || data?.code || `http_${response.status}`)
  return data as unknown as T
}

function receiptStorage(target: Destination, action: string): string {
  return "clawdline.localviewer.receipt:" + JSON.stringify([target.machine_id, target.session_id, target.execution_generation, action])
}

function CloudLocalPage({ shown }: { shown: boolean }) {
  const t = words()
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [authorized, setAuthorized] = useState(false)
  const [machines, setMachines] = useState<Machine[]>([])
  const [projections, setProjections] = useState<Record<string, Projection>>({})
  const [selected, setSelected] = useState<Destination | null>(null)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [olderPages, setOlderPages] = useState<{ target: string; pages: OlderPage[] } | null>(null)
  const [olderBusy, setOlderBusy] = useState(false)
  const olderController = useRef<AbortController | null>(null)
  const authorizationEpoch = useRef(0)
  const detailEpoch = useRef(0)
  const selectedRef = useRef(selected)
  selectedRef.current = selected
  const [problem, setProblem] = useState("")
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const [request, setRequest] = useState("")
  const [lastAction, setLastAction] = useState("send")
  const [receipt, setReceipt] = useState<Receipt | null>(null)

  const clearSelectedContent = useCallback(() => {
    detailEpoch.current++
    olderController.current?.abort()
    olderController.current = null
    setSelected(null); setDetail(null)
    setOlderPages(null); setOlderBusy(false); setReceipt(null); setRequest("")
  }, [])
  const clearAuthorizedContent = useCallback(() => {
    authorizationEpoch.current++
    clearSelectedContent()
    setMachines([]); setProjections({})
  }, [clearSelectedContent])

  const refresh = useCallback(async () => {
    setBusy(true)
    setProblem("")
    try {
      const state = await localViewerFetch<{ enabled: boolean; authorized: boolean }>("/v1/cloud/viewer/status")
      setEnabled(state.enabled); setAuthorized(state.authorized)
      if (!state.enabled || !state.authorized) { clearAuthorizedContent(); return }
      const epoch = authorizationEpoch.current
      const listing = await localViewerFetch<Machine[]>("/v1/cloud/viewer/machines")
      if (epoch !== authorizationEpoch.current) return
      setMachines(listing)
      const next: Record<string, Projection> = {}
      await Promise.all(listing.map(async (machine) => {
        if (machine.pairing !== "paired") { next[machine.id] = { kind: "unavailable", reason: machine.pairing }; return }
        try { next[machine.id] = await localViewerFetch<Projection>("/v1/cloud/viewer/sessions?machine=" + encodeURIComponent(machine.id)) }
        catch (error) {
          const reason = String(error instanceof Error ? error.message : error)
          next[machine.id] = { kind: "unavailable", reason }
          if ((reason === "forbidden" || reason === "no_permission" || reason === "viewer_revoked") &&
            selectedRef.current?.machine_id === machine.id) clearSelectedContent()
        }
      }))
      if (epoch === authorizationEpoch.current) setProjections(next)
    } catch (error) {
      if (error instanceof Error && (error.message === "forbidden" || error.message === "viewer_revoked")) {
        setAuthorized(false); clearAuthorizedContent()
      }
      setProblem(String(error instanceof Error ? error.message : error))
    }
    finally { setBusy(false) }
  }, [clearAuthorizedContent, clearSelectedContent])

  useEffect(() => { if (shown) void refresh() }, [shown, refresh])
  useEffect(() => {
    const followAddress = () => {
      const target = destinationFromFragment(location.hash)
      setSelected(target ? { machine_id: target.machineID, session_id: target.sessionID,
        execution_generation: target.executionGeneration } : null)
    }
    followAddress()
    window.addEventListener("hashchange", followAddress)
    return () => window.removeEventListener("hashchange", followAddress)
  }, [])
  useEffect(() => {
    olderController.current?.abort()
    olderController.current = null
    setOlderPages(null)
    setOlderBusy(false)
    if (!shown || !selected || enabled !== true || !authorized) { setDetail(null); return }
    const controller = new AbortController()
    const epoch = authorizationEpoch.current
    const selectedEpoch = detailEpoch.current
    setDetail(null)
    setReceipt(null)
    try { setRequest(localStorage.getItem(receiptStorage(selected, lastAction)) || "") }
    catch { setRequest("") }
    void localViewerFetch<Detail>("/v1/cloud/viewer/detail?" + query(selected), controller.signal)
      .then((result) => { if (!controller.signal.aborted && epoch === authorizationEpoch.current &&
        selectedEpoch === detailEpoch.current &&
        same(result.destination, selected)) setDetail(result) })
      .catch((error) => { if (!controller.signal.aborted) {
        const reason = String(error instanceof Error ? error.message : error)
        if (reason === "forbidden" || reason === "no_permission" || reason === "viewer_revoked") clearSelectedContent()
        setProblem(reason)
      } })
    return () => { controller.abort(); olderController.current?.abort() }
  }, [shown, selected, enabled, authorized, clearSelectedContent])

  const current = selected ? projections[selected.machine_id]?.rows?.find((row) => same(row.destination, selected)) : null
  const canAct = !!selected && !!current && current.freshness === "current" && !busy
  const targetKey = selected ? JSON.stringify(selected) : ""
  const loadedOlder = olderPages?.target === targetKey ? olderPages.pages : []
  const nextBefore = loadedOlder.length ? loadedOlder[loadedOlder.length - 1].next_before : detail?.next_before

  async function loadOlder() {
    if (!selected || !detail || !current || current.freshness !== "current" ||
      typeof nextBefore !== "number" || !Number.isSafeInteger(nextBefore) || nextBefore <= 0 || olderBusy) return
    const target = selected
    const key = targetKey
    const controller = new AbortController()
    olderController.current = controller
    setOlderBusy(true); setProblem("")
    try {
      const page = await localViewerFetch<OlderPage>("/v1/cloud/viewer/detail?" + query(target) +
        "&before=" + encodeURIComponent(String(nextBefore)), controller.signal)
      if (!controller.signal.aborted && same(page.destination, target)) {
        setOlderPages((previous) => ({ target: key, pages: [...(previous?.target === key ? previous.pages : []), page] }))
      }
    } catch (error) {
      if (!controller.signal.aborted) {
        const reason = String(error instanceof Error ? error.message : error)
        if (reason === "forbidden" || reason === "no_permission" || reason === "viewer_revoked") clearSelectedContent()
        setProblem(reason)
      }
    } finally {
      if (olderController.current === controller) { olderController.current = null; setOlderBusy(false) }
    }
  }

  async function lookup(target: Destination, action: string, id: string) {
    const params = new URLSearchParams(query(target))
    params.set("action", action); params.set("request", id); params.set("query", crypto.randomUUID().replaceAll("-", ""))
    try { setReceipt(await localViewerFetch<Receipt>("/v1/cloud/viewer/receipts?" + params)) }
    catch (error) { setProblem(String(error instanceof Error ? error.message : error)) }
  }

  async function act(action: "send" | "answer" | "interrupt" | "end", answerKey = "", fingerprint = "") {
    if (!selected) return
    if (!canAct) { setProblem(current ? t.stale : t.changed); return }
    try {
      const earlier = localStorage.getItem(receiptStorage(selected, action))
      if (earlier) {
        setLastAction(action); setRequest(earlier); setProblem(t.pending)
        await lookup(selected, action, earlier)
        return
      }
    } catch { setProblem("storage_unavailable"); return }
    const id = crypto.randomUUID().replaceAll("-", "")
    try { localStorage.setItem(receiptStorage(selected, action), id) }
    catch { setProblem("storage_unavailable"); return }
    setLastAction(action); setRequest(id); setReceipt(null); setProblem(""); setBusy(true)
    try {
      await localViewerFetch("/v1/cloud/viewer/actions", undefined,
        { destination: selected, action, request: id, text: message, answer: answerKey, expect: fingerprint })
      await lookup(selected, action, id)
    } catch (error) {
      setProblem(String(error instanceof Error ? error.message : error) + ". " + t.uncertain)
    } finally { setBusy(false) }
  }

  return <section className="page page-cloud-local" id="cloud" data-page-view="cloud" hidden={!shown}>
    <div className="sheet">
      <h2>{t.title}</h2>
      <button className="chip" type="button" disabled={busy} onClick={() => void refresh()}>{t.refresh}</button>
      {busy && <p role="status">{t.loading}</p>}
      {problem && <p className="cloud-local-problem" role="alert">{problem === "forbidden" ? t.local : problem}</p>}
      {enabled === false && <p>{t.disabled}</p>}
      {enabled && !authorized && <p>{t.login}</p>}
      {enabled && authorized && machines.length === 0 && <p>{t.noMachines}</p>}
      {enabled && authorized && <div className="cloud-local-machines">
        {machines.map((machine) => {
          const projection = projections[machine.id]
          return <div className="block" key={machine.id}>
            <h3>{machine.name || machine.id}</h3><small>{machine.id} · {machine.fingerprint}</small>
            {machine.pairing !== "paired" ? <p>{t.pair} ({machine.pairing})</p> :
              projection?.kind !== "ready" ? <p role="status">{projection?.reason || machine.reason || t.loading}</p> : <>
                {projection.unknown_targets ? <p>{t.unknown}</p> : null}
                {projection.rows?.length === 0 && <p>{t.noRows}</p>}
                <ul>{projection.rows?.map((row) => <li key={row.destination.session_id}>
                  <button type="button" onClick={() => { location.hash = destinationFragment({ machineID: row.destination.machine_id,
                    sessionID: row.destination.session_id, executionGeneration: row.destination.execution_generation });
                    setSelected(row.destination); setProblem(""); setReceipt(null)
                    try { setRequest(localStorage.getItem(receiptStorage(row.destination, lastAction)) || "") } catch { setRequest("") } }}>
                    {row.destination.session_id} · {row.state} · {row.freshness} {row.needs_attention ? "!" : ""}
                  </button>
                  <small>{row.destination.execution_generation}</small>
                </li>)}</ul>
              </>}
          </div>
        })}
      </div>}
      {enabled && authorized && selected && <div className="block cloud-local-detail">
        <button className="chip" type="button" onClick={() => { location.hash = "#page=cloud"; setSelected(null); setDetail(null) }}>{t.back}</button>
        <h3>{selected.machine_id} / {selected.session_id}</h3><small>{selected.execution_generation}</small>
        {!current && <p role="alert">{t.changed}</p>}
        {current && current.freshness !== "current" && <p role="alert">{t.stale}</p>}
        {detail ? <>
          <h4>{t.info}</h4><pre>{JSON.stringify(detail.info, null, 2)}</pre>
          <h4>{t.transcript}</h4>
          <div className="cloud-local-transcript">
            {[...loadedOlder].reverse().map((page, pageIndex) =>
              page.transcript.entries.map((entry, index) => <article key={`older-${pageIndex}-${index}`}>
                <strong>{entry.role}</strong><p>{entry.text}</p>
              </article>))}
            {detail.transcript.entries.map((entry, index) => <article key={`latest-${index}`}>
              <strong>{entry.role}</strong><p>{entry.text}</p>
            </article>)}
          </div>
          {typeof nextBefore === "number" && Number.isSafeInteger(nextBefore) && nextBefore > 0 ?
            <button className="chip" type="button" disabled={olderBusy || busy || !current || current.freshness !== "current"}
              onClick={() => void loadOlder()}>{olderBusy ? t.loadingOlder : t.older}</button> :
            <p>{t.allLoaded}</p>}
        </> : <p>{t.read}…</p>}
        <label>{t.text}<textarea value={message} onChange={(event) => setMessage(event.target.value)} /></label>
        <div className="cloud-local-actions">
          <button className="chip" type="button" disabled={!canAct || !message.trim()} onClick={() => void act("send")}>{t.send}</button>
          <button className="chip" type="button" disabled={!canAct} onClick={() => void act("interrupt")}>{t.interrupt}</button>
          <button className="chip" type="button" disabled={!canAct} onClick={() => void act("end")}>{t.end}</button>
        </div>
        {detail?.question && <div className="cloud-local-question"><h4>{detail.question.text || t.answer}</h4>
          {detail.question.options.map((option) => <button className="chip" key={option.key} type="button" disabled={!canAct}
            onClick={() => void act("answer", option.key, detail.question!.fingerprint)}>{option.label}</button>)}
        </div>}
        <div className="cloud-local-receipt">
          <label>{t.action}<select value={lastAction} onChange={(event) => { const next = event.target.value; setLastAction(next); setReceipt(null)
            try { setRequest(localStorage.getItem(receiptStorage(selected, next)) || "") } catch { setRequest("") } }}>
            <option value="send">{t.send}</option><option value="answer">{t.answer}</option>
            <option value="interrupt">{t.interrupt}</option><option value="end">{t.end}</option>
          </select></label>
          <label>{t.request}<input value={request} onChange={(event) => setRequest(event.target.value)} /></label>
          <button className="chip" type="button" disabled={!request} onClick={() => void lookup(selected, lastAction, request)}>{t.lookup}</button>
          {receipt && <p>{t.receipt}: {receipt.machine_execution} {receipt.code}</p>}
          {receipt && (receipt.machine_execution === "completed" || receipt.machine_execution === "rejected") &&
            <button className="chip" type="button" onClick={() => { try { localStorage.removeItem(receiptStorage(selected, lastAction)) } catch { return }
              setRequest(""); setReceipt(null); setProblem("") }}>{t.acknowledge}</button>}
        </div>
      </div>}
    </div>
  </section>
}

export const page: PageModule = { id: "cloud", requiresApiLevel: 4, Component: CloudLocalPage }
