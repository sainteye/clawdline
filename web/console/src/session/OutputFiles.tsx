import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { destinationFromFragment, destinationFragment } from "../cloud/all-machine-sessions.js"
import { nextWord } from "../next-strings.js"
import { failureSentence } from "../legacy/js/core/failure-text.js"
import { htmlPreviewSource } from "../legacy/js/view/document-html.js"
import { DOCUMENT_MAX_BYTES } from "../legacy/js/net/document-links.js"
import { openImagePreview } from "../legacy/images-bridge.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { filterOutputs, outputCategory, outputSessionKey, readOutput, readOutputs, shareOutputLink, type OutputFile, type OutputListing } from "./output-files-api.js"
import "./output-files.css"

const w = nextWord
let thumbnails: Promise<unknown> = Promise.resolve()
function historyOpen(key: string, owner: string) {
  const state = { ...history.state, [key]: owner }
  delete state[key + "Closed"]
  if (history.state?.[key + "Closed"] === owner) history.replaceState(state, "")
  else history.pushState(state, "")
}
function historyClose(key: string, owner: string) {
  if (history.state?.[key] !== owner) return
  const state = { ...history.state, [key + "Closed"]: owner }
  delete state[key]
  history.replaceState(state, "")
}
function errorWord(error: unknown): string {
  const code = (error as { code?: string })?.code
  if (code === "output_not_found") return w("outputsMissing")
  if (code === "output_changed") return w("outputsChanged")
  if (code === "output_too_large") return w("outputsTooLarge")
  if (code === "output_index_full") return w("outputsFull")
  if (code === "output_identity_unavailable") return w("outputsIdentity")
  if (["unknown_command", "cloud_not_carried", "not_found", "read_only_channel"].includes(code ?? "")) return w("outputsUnsupported")
  return failureSentence(error, w("outputsFailed"))
}
const size = (bytes: number) => bytes < 1024 ? `${bytes} B` : bytes < 1024 * 1024 ? `${Math.ceil(bytes / 1024)} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`

/** One entrance on both layouts. Closing preserves the mounted conversation and draft. */
export function OutputFiles({ row, open, onClose, trigger }: {
  row: SessionRow | null; open: boolean; onClose(): void; trigger: React.RefObject<HTMLButtonElement | null>
}) {
  const identity = outputSessionKey(row)
  const [data, setData] = useState<OutputListing | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState("all")
  const [selected, setSelected] = useState<OutputFile | null>(null)
  const close = useRef<HTMLButtonElement>(null)
  const request = useRef<AbortController | null>(null)
  const lastPhase = useRef(row?.state)
  const owner = useRef(crypto.randomUUID())
  const current = useRef(identity)
  current.current = identity
  const load = () => {
    if (!row) return
    request.current?.abort()
    const abort = new AbortController(), mine = identity
    request.current = abort; setLoading(true); setError("")
    void readOutputs(row.id, abort.signal).then(value => {
      if (!abort.signal.aborted && mine === current.current) setData(value)
    }, caught => {
      if (!abort.signal.aborted && mine === current.current) setError(errorWord(caught))
    }).finally(() => { if (!abort.signal.aborted && mine === current.current) setLoading(false) })
  }
  useEffect(() => {
    setData(null); setError(""); setQuery(""); setFilter("all"); setSelected(null)
    if (!open || !row) return
    load()
    historyOpen("sessionOutputs", owner.current)
    const pop = (event: PopStateEvent) => {
      if (history.state?.sessionOutputs === owner.current) return
      event.stopImmediatePropagation(); onClose()
    }
    window.addEventListener("popstate", pop, true)
    return () => {
      request.current?.abort()
      historyClose("sessionOutputs", owner.current)
      window.removeEventListener("popstate", pop, true)
    }
    // Each identity owns one index and cancellation lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, identity])
  useEffect(() => {
    if (open && lastPhase.current === "working" && row?.state !== "working") load()
    lastPhase.current = row?.state
    // A completed turn refreshes the visible index without polling hidden views.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [row?.state, open, identity])
  useLayoutEffect(() => { if (open) close.current?.focus({ preventScroll: true }) }, [open])
  useEffect(() => {
    if (!open || !data) return
    const follow = () => {
      const id = new URLSearchParams(location.hash.slice(1)).get("output")
      const match = data.files.find(file => file.id === id)
      if (match) setSelected(match)
    }
    follow(); window.addEventListener("hashchange", follow)
    return () => window.removeEventListener("hashchange", follow)
  }, [open, data])
  if (!open || !row) return null
  const files = data?.files ?? []
  const shown = filterOutputs(files, filter, query)
  const target = import.meta.env.VITE_HOSTED_CONSOLE ? destinationFromFragment(location.hash) : null
  const shareBase = target && target.sessionID === row.id && target.executionGeneration === row.execution_generation
    ? "https://app.clawdline.com/" + destinationFragment(target) : ""
  const dismiss = () => { historyClose("sessionOutputs", owner.current); onClose(); trigger.current?.focus({ preventScroll: true }) }
  return <aside className="output-panel" aria-labelledby="output-title" onKeyDown={event => {
    if (event.key === "Escape" && !selected) { event.preventDefault(); event.stopPropagation(); dismiss() }
  }}>
    <header className="output-heading"><h2 id="output-title">{w("outputsTitle")} {data && <small>{files.length}</small>}</h2>
      <button type="button" className="chip" onClick={load} disabled={loading} aria-label={w("outputsRefresh")}>↻</button>
      <button ref={close} type="button" className="chip" onClick={dismiss} aria-label={w("outputsClose")}>×</button></header>
    <div className="output-body">
      <p className="output-caption">{w("outputsDescription")}</p>
      <input className="output-search" type="search" value={query} onChange={event => setQuery(event.target.value)} placeholder={w("outputsSearch")} aria-label={w("outputsSearch")} />
      <div className="output-filters" aria-label={w("outputsFilter")}>{(["all", "images", "documents", "other"] as const).map(kind =>
        <button type="button" key={kind} aria-pressed={kind === filter} onClick={() => setFilter(kind)}>{w(({ all: "outputsAll", images: "outputsImages", documents: "outputsDocuments", other: "outputsOther" } as const)[kind])} {data && (kind === "all" ? files.length : files.filter(f => outputCategory(f) === kind).length)}</button>)}</div>
      {loading && <p role="status">{w("outputsLoading")}</p>}
      {error && <p className="output-error" role="alert">{error}</p>}
      {data && <>
        <p className="output-caption">{w("outputsRecent")}</p>
        {(data.coverage !== "native_and_published" || data.more || data.partial) && <p className="output-note" role="status">{data.partial ? w("outputsFull") : data.coverage === "record_incomplete" ? w("outputsRecordIncomplete") : data.coverage === "record_unavailable" ? w("outputsRecordMissing") : data.coverage === "record_row_too_large" ? w("outputsRecordLarge") : data.more ? w("outputsMoreNote") : w("outputsCoverage")}</p>}
        {!shown.length && !loading && <p className="output-empty">{query || filter !== "all" ? w("outputsNoMatch") : w("outputsEmpty")}</p>}
        <div className="output-file-list">{shown.map(file => <FileCard key={file.id} file={file} session={row.id} shareURL={shareBase ? shareBase + "&output=" + file.id : ""} onPreview={() => setSelected(file)} />)}</div>
        {data.more && <button type="button" className="chip output-more" onClick={load} disabled={loading}>{w("outputsMore")}</button>}
        <p className="output-footnote">{w("outputsLocalPath")}</p>
      </>}
    </div>
    {selected && <OutputPreview key={selected.id} file={selected} session={row.id} onClose={() => setSelected(null)} />}
  </aside>
}

function FileCard({ file, session, shareURL, onPreview }: { file: OutputFile; session: string; shareURL: string; onPreview(): void }) {
  const [url, setURL] = useState("")
  const [copied, setCopied] = useState(false)
  const [linkCopied, setLinkCopied] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)
  const [error, setError] = useState("")
  const card = useRef<HTMLElement>(null)
  const operation = useRef<AbortController | null>(null)
  const downloadURL = useRef<string | null>(null)
  useEffect(() => {
    if (outputCategory(file) !== "images" || file.state !== "available" || !card.current) return
    const abort = new AbortController()
    let held = "", asked = false
    const observer = new IntersectionObserver(entries => {
      if (asked || !entries.some(e => e.isIntersecting)) return
      asked = true; observer.disconnect()
      const run = async () => {
        if (abort.signal.aborted) return
        try {
          const image = await readOutput(session, file, abort.signal, () => {}, true)
          if (!abort.signal.aborted) { held = URL.createObjectURL(image); setURL(held) }
        } catch { /* A thumbnail is optional; the file's explicit preview remains available. */ }
      }
      thumbnails = thumbnails.then(run, run)
    }, { root: card.current.closest(".output-body") })
    observer.observe(card.current)
    return () => { observer.disconnect(); abort.abort(); if (held) URL.revokeObjectURL(held) }
  }, [file.id, file.updated_at, file.state, session])
  useEffect(() => () => { operation.current?.abort(); if (downloadURL.current) URL.revokeObjectURL(downloadURL.current) }, [])
  return <article className="output-file" ref={card}>
    {outputCategory(file) === "images" && <button type="button" className="output-thumb" disabled={file.state !== "available"} onClick={onPreview} aria-label={w("outputsPreviewName", { name: file.name })}>{url ? <img src={url} alt={file.name} /> : <span aria-hidden="true">▧</span>}</button>}
    <div className="output-file-name"><strong title={file.name}>{file.name}</strong><span className="output-kind" data-kind={file.kind}>{w(file.kind === "created" ? "outputsCreated" : file.kind === "modified" ? "outputsModified" : "outputsPublished")}</span></div>
    <p className="output-path" title={file.absolute_path}>{file.path}</p>
    <p className="output-caption">{size(file.bytes)} · {(file.updated_at ? new Date(file.updated_at * 1000).toLocaleString() : w("outputsTimeUnknown"))} · {w("outputsUpdates", { count: file.updates })}</p>
    {file.state !== "available" && <p className="output-note">{w(file.state === "missing" ? "outputsMissing" : "outputsTooLarge")}</p>}
    <div className="output-actions">
      <button type="button" className="chip" disabled={file.state !== "available"} onClick={onPreview}>{w("outputsPreview")}</button>
      <button type="button" className="chip" onClick={() => {
        void navigator.clipboard.writeText(file.absolute_path).then(() => setCopied(true), () => setError(w("outputsCopyFailed")))
      }}>{w(copied ? "outputsCopied" : "outputsCopy")}</button>
      <button type="button" className="chip" disabled={file.state !== "available" || busy !== null} onClick={() => {
        const abort = new AbortController(); operation.current = abort; setBusy(0); setError("")
        void readOutput(session, file, abort.signal, setBusy).then(blob => {
          if (abort.signal.aborted) return
          const url = URL.createObjectURL(blob), a = document.createElement("a")
          if (downloadURL.current) URL.revokeObjectURL(downloadURL.current)
          downloadURL.current = url
          a.href = url; a.download = file.name; a.click()
          // Keep the Blob through the browser's download handoff; revoke on the next frame.
          requestAnimationFrame(() => { URL.revokeObjectURL(url); if (downloadURL.current === url) downloadURL.current = null })
        }, caught => { if (!abort.signal.aborted) setError(errorWord(caught)) }).finally(() => { if (!abort.signal.aborted) setBusy(null) })
      }}>{busy !== null ? `${busy}%` : w("outputsDownload")}</button>
      <button type="button" className="chip" disabled={!shareURL} title={shareURL ? w("outputsShare") : w("outputsShareCloud")} onClick={() => {
        setError("")
        void shareOutputLink(file.name, shareURL, {
          share: navigator.share?.bind(navigator), copy: text => navigator.clipboard.writeText(text),
        }).then(result => { if (result === "copied") setLinkCopied(true) }, () => setError(w("outputsShareFailed")))
      }}>{w(linkCopied ? "outputsCopied" : "outputsShare")}</button>
    </div>
    {error && <p className="output-error" role="alert">{error}</p>}
    {error === w("outputsCopyFailed") && <p className="output-path">{file.absolute_path}</p>}
    {error === w("outputsShareFailed") && <p className="output-path">{shareURL}</p>}
  </article>
}
function OutputPreview({ file, session, onClose }: { file: OutputFile; session: string; onClose(): void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [text, setText] = useState<string | null>(null)
  const [percent, setPercent] = useState(0)
  const [error, setError] = useState("")
  const owner = useRef(crypto.randomUUID())
  const previewable = file.media_type.startsWith("image/") || (file.media_type.startsWith("text/") && file.bytes <= DOCUMENT_MAX_BYTES)
  const dismiss = () => {
    historyClose("sessionOutputPreview", owner.current)
    const hash = location.hash.replace(/&output=[0-9a-f]{64}$/, "")
    if (hash !== location.hash) history.replaceState(history.state, "", hash)
    onClose()
  }
  useEffect(() => {
    const element = dialog.current!, opener = document.activeElement as HTMLElement | null
    showModalWithoutChoice(element); historyOpen("sessionOutputPreview", owner.current)
    const abort = new AbortController(); let held = "", releasePreview: (() => void) | undefined
    const pop = (event: PopStateEvent) => { if (history.state?.sessionOutputPreview !== owner.current) { event.stopImmediatePropagation(); onClose() } }
    window.addEventListener("popstate", pop, true)
    if (previewable) void (async () => {
      // The thumbnail read checks decoded image dimensions before the browser
      // is allowed to decode the original. Text uses the inert document budget.
      if (file.media_type.startsWith("image/")) await readOutput(session, file, abort.signal, () => {}, true)
      return readOutput(session, file, abort.signal, setPercent)
    })().then(async blob => {
      if (abort.signal.aborted) return
      if (file.media_type.startsWith("image/")) {
        held = URL.createObjectURL(blob)
        element.close()
        releasePreview = openImagePreview(opener ?? document.body, held, file.name, dismiss)
      }
      else { const raw = await blob.text(); if (!abort.signal.aborted) setText(raw) }
    }, caught => { if (!abort.signal.aborted) setError(errorWord(caught)) })
    return () => { abort.abort(); releasePreview?.(); element.close(); if (held) URL.revokeObjectURL(held); window.removeEventListener("popstate", pop, true); historyClose("sessionOutputPreview", owner.current); if (opener?.isConnected) opener.focus({ preventScroll: true }) }
    // The keyed preview holds one exact file and Session through the transfer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return <dialog className="output-preview" ref={dialog} aria-labelledby="output-preview-title" onCancel={event => { event.preventDefault(); event.stopPropagation(); dismiss() }}>
    <header><h2 id="output-preview-title">{file.name}</h2><button type="button" className="chip" onClick={dismiss} aria-label={w("outputsClose")}>×</button></header>
    <div className="output-preview-body">
      {error ? <p role="alert">{error}</p> : !previewable ? <p>{w("outputsNoPreview")}</p> : text !== null ? file.media_type === "text/html" ? <iframe title={file.name} sandbox="" referrerPolicy="no-referrer" srcDoc={htmlPreviewSource(text, window)} /> : <pre>{text}</pre> : <p role="status">{w("outputsReading", { percent })}</p>}
    </div>
    <footer><p className="output-path">{file.path}</p><p className="output-caption">{w("outputsLocalPath")}</p></footer>
  </dialog>
}
