import { useEffect, useRef, useState } from "react"
import type { ProjectMemoryDream, ProjectMemoryDreamChange, ProjectMemoryEntry, ProjectMemoryList, ProjectMemoryReview, ProjectMemorySummary } from "@clawdline/contract"
import { catalogFormat, catalogWord, currentCatalogTag } from "../../catalog.js"
import { confirmAction } from "../../confirm-action.js"
import { LoadingSkeleton } from "../../LoadingSkeleton.js"
import {
  listProjectMemory, ProjectMemoryError, readProjectDream, readProjectMemory, restoreProjectMemory, reviewProjectMemory, searchProjectMemory,
} from "./project-memory-api.js"
import "./project-memory.css"

export const memoryWord = (key: string) => catalogWord("projectMemory", key)
const memoryFormat = (key: string, values: (string | number)[]) => catalogFormat("projectMemory", key, values)

type Tab = "entries" | "suggestions" | "archive"
type Selection = { name: string; comparison?: ProjectMemoryDreamChange; archived?: boolean }
type Detail = { source: ProjectMemoryEntry; replacement?: ProjectMemoryEntry }
type Overview = { list: ProjectMemoryList; dream: ProjectMemoryDream }
type Draft = { body: string; original: string }
type Mutation = { name: string; key: string; review?: ProjectMemoryReview }

const reasons: Record<string, string> = {
  archived_duplicate: "duplicate", archived_superseded: "superseded", archived_reviewed: "reviewed",
  same_description: "sameDescription", successor_chain: "chain", archive_full: "archiveFull",
  related_delivery: "relatedDelivery",
}
const kinds: Record<ProjectMemorySummary["type"], string> = {
  user: "preferences", feedback: "feedback", project: "projectNotes", reference: "reference",
}

function memoryError(error: unknown) {
  if (!(error instanceof ProjectMemoryError)) return memoryWord("unavailable")
  if (error.status === 501) return memoryWord("upgrade")
  if (error.status === 403) return memoryWord("permission")
  if (error.code === "memory_review_stale") return memoryWord("stale")
  if (error.code === "memory_full") return memoryWord("full")
  if (error.code === "memory_entry_invalid") return memoryWord("invalid")
  if (error.code === "memory_entry_not_found") return memoryWord("missing")
  return memoryWord("unavailable")
}

export function ProjectMemory({ project, onState }: {
  project: string; onState(state: { dirty: boolean; busy: boolean }): void
}) {
  const [tab, setTab] = useState<Tab>("entries")
  const [query, setQuery] = useState("")
  const [submitted, setSubmitted] = useState("")
  const [matching, setMatching] = useState<Set<string>>()
  const [searchError, setSearchError] = useState("")
  const [overview, setOverview] = useState<Overview>()
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState("")
  const [detailError, setDetailError] = useState("")
  const [notice, setNotice] = useState("")
  const [refresh, setRefresh] = useState(0)
  const [selected, setSelected] = useState<Selection>()
  const [detail, setDetail] = useState<Detail>()
  const [draft, setDraft] = useState<Draft>()
  const [busy, setBusy] = useState(false)
  const mutationLock = useRef(false)
  const lastMutation = useRef<Mutation | undefined>(undefined)
  const heading = useRef<HTMLHeadingElement>(null)
  const opener = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    const abort = new AbortController()
    setLoading(true)
    setLoadError("")
    void Promise.all([listProjectMemory(project, abort.signal), readProjectDream(project, abort.signal)])
      .then(([list, dream]) => { if (!abort.signal.aborted) setOverview({ list, dream }) })
      .catch(error => { if (!abort.signal.aborted) setLoadError(memoryError(error)) })
      .finally(() => { if (!abort.signal.aborted) setLoading(false) })
    return () => abort.abort()
  }, [project, refresh])

  useEffect(() => {
    const abort = new AbortController()
    setMatching(undefined)
    setSearchError("")
    if (!submitted) return
    void searchProjectMemory(project, submitted, abort.signal)
      .then(result => { if (!abort.signal.aborted) setMatching(new Set(result.matches)) })
      .catch(error => { if (!abort.signal.aborted) setSearchError(memoryError(error)) })
    return () => abort.abort()
  }, [project, submitted, refresh])

  useEffect(() => {
    setDetail(undefined)
    setDetailError("")
    if (!selected) return
    const abort = new AbortController()
    const source = readProjectMemory(project, selected.name, abort.signal)
    const replacement = selected.comparison && readProjectMemory(project, selected.comparison.replacement, abort.signal)
    void Promise.all([source, replacement])
      .then(([original, next]) => { if (!abort.signal.aborted) setDetail({ source: original, replacement: next }) })
      .catch(error => { if (!abort.signal.aborted) setDetailError(memoryError(error)) })
    return () => abort.abort()
  }, [project, selected])

  useEffect(() => {
    if (detail && heading.current) heading.current.focus()
  }, [detail])

  const dirty = !!draft && draft.body !== draft.original
  useEffect(() => {
    onState({ dirty, busy })
    return () => onState({ dirty: false, busy: false })
  }, [onState, dirty, busy])

  async function leaveDetail() {
    if (mutationLock.current) return false
    if (dirty && !await confirmAction(memoryWord("discardDraft"))) return false
    setDraft(undefined)
    setDetail(undefined)
    setSelected(undefined)
    setDetailError("")
    lastMutation.current = undefined
    return true
  }

  async function open(selection: Selection, button: HTMLButtonElement) {
    if (!await leaveDetail()) return
    opener.current = button
    setNotice("")
    setSelected(selection)
  }

  async function switchTab(next: Tab) {
    if (next === tab || !await leaveDetail()) return
    setTab(next)
    setNotice("")
  }

  async function mutate(review?: ProjectMemoryReview) {
    if (!selected || mutationLock.current) return
    mutationLock.current = true
    setBusy(true)
    onState({ busy: true, dirty })
    const name = selected.name
    const previous = lastMutation.current
    const key = previous && previous.name === name && JSON.stringify(previous.review) === JSON.stringify(review)
      ? previous.key : crypto.randomUUID()
    lastMutation.current = { name, review, key }
    setDetailError("")
    try {
      if (review) await reviewProjectMemory(project, name, key, review)
      else await restoreProjectMemory(project, name, key)
      lastMutation.current = undefined
      setDraft(undefined)
      setSelected(undefined)
      setNotice(memoryWord(review?.action === "merge" ? "merged" : review?.action === "archive" ? "archivedNotice" : review ? "kept" : "restored"))
      setRefresh(value => value + 1)
      if (opener.current?.isConnected) opener.current.focus()
    } catch (error) {
      setDetailError(memoryError(error))
    } finally {
      mutationLock.current = false
      setBusy(false)
    }
  }

  function review(action: ProjectMemoryReview["action"]) {
    if (!detail?.source.revision || !detail.replacement?.revision) return
    const input: ProjectMemoryReview = { action, expected: detail.source.revision }
    if (action !== "keep") {
      input.replacement = detail.replacement.name
      input.expected_replacement = detail.replacement.revision
    }
    if (action === "merge") input.body = draft?.body
    void mutate(input)
  }

  const entries = overview?.list.entries ?? []
  const dream = overview?.dream
  const byName = new Map(entries.map(entry => [entry.name, entry]))
  const suggestions = dream ? [...dream.archive, ...dream.review] : []
  const shownEntries = entries.filter(entry => !submitted || matching?.has(entry.name))
  const shownChanges = (tab === "archive" ? dream?.archived ?? [] : suggestions).filter(row =>
    !submitted || matching?.has(row.name) || (tab === "suggestions" && matching?.has(row.replacement)))
  const searching = !!submitted && !matching && !searchError
  const lastRun = dream?.last_run ? new Date(dream.last_run) : undefined

  return <section className="project-memory" aria-label={memoryWord("title")}>
    {dream && <header className="project-memory-head">
      <dl className="project-memory-counts">
        {(["active", "pending", "archived"] as const).map((kind, i) => <div key={kind}>
          <dt>{memoryWord(kind)}</dt>
          <dd>{memoryFormat("count", [dream.counts[kind], [dream.active_limit, dream.pending_limit, dream.archive_limit][i]!])}</dd>
        </div>)}
      </dl>
      {lastRun && !Number.isNaN(lastRun.getTime()) && <p className="project-memory-muted">
        {memoryFormat("lastRun", [new Intl.DateTimeFormat(currentCatalogTag(), { dateStyle: "medium", timeStyle: "short" }).format(lastRun)])}
      </p>}
    </header>}
    <nav className="project-memory-tabs" aria-label={memoryWord("title")}>
      {(["entries", "suggestions", "archive"] as const).map(id =>
        <button key={id} type="button" aria-pressed={tab === id} disabled={busy} onClick={() => void switchTab(id)}>{memoryWord(id)}</button>)}
    </nav>
    <form className="project-memory-search" onSubmit={event => {
      event.preventDefault()
      const text = query.trim()
      if (text && text === submitted) setRefresh(count => count + 1)
      else setSubmitted(text)
    }}>
      <input type="search" aria-label={memoryWord("search")} placeholder={memoryWord("search")} value={query} onChange={event => {
        setQuery(event.target.value)
        if (!event.target.value.trim()) setSubmitted("")
      }} />
      <button type="submit" disabled={busy}>{memoryWord("searchAction")}</button>
      <button type="button" disabled={busy || loading} onClick={() => setRefresh(value => value + 1)}>{memoryWord("refresh")}</button>
    </form>
    {searchError && <p role="alert">{searchError}</p>}
    {searching && <p role="status">{memoryWord("searching")}</p>}
    {notice && <p role="status">{notice}</p>}
    {loadError && <p role="alert">{loadError}</p>}
    {loading && !overview && <LoadingSkeleton id="project-memory" kind="cards" label={memoryWord("loading")} />}
    {selected && <section className="project-memory-detail" aria-label={memoryWord("detail")}>
      <header>
        <h3 tabIndex={-1} ref={heading}>{detail?.source.description || selected.name}</h3>
        <button type="button" disabled={busy} onClick={() => void leaveDetail().then(left => {
          if (left && opener.current?.isConnected) opener.current.focus()
        })}>{memoryWord("closeDetail")}</button>
      </header>
      {detailError && <p role="alert">{detailError}</p>}
      {!detail && !detailError && <p role="status">{memoryWord("loading")}</p>}
      {detail && <>
        {detail.replacement ? <MemoryComparison source={detail.source} replacement={detail.replacement} /> :
          <pre className="project-memory-body" tabIndex={0}>{detail.source.body}</pre>}
        {selected.archived && <button type="button" disabled={busy} onClick={() => void mutate()}>{memoryWord("restore")}</button>}
        {detail.replacement && detail.source.revision && detail.replacement.revision && <>
          {draft ? <form className="project-memory-merge" onSubmit={event => { event.preventDefault(); review("merge") }}>
            <label>{memoryWord("mergedBody")}<textarea rows={7} value={draft.body} disabled={busy}
              onChange={event => setDraft({ ...draft, body: event.target.value })} /></label>
            <div className="project-memory-actions">
              <button type="submit" disabled={busy || !draft.body.trim()}>{memoryWord("saveMerge")}</button>
            </div>
          </form> : <div className="project-memory-actions">
            <button type="button" disabled={busy} onClick={() => review("keep")}>{memoryWord("keep")}</button>
            <button type="button" disabled={busy} onClick={() => void confirmAction(memoryWord("confirmArchive")).then(ok => {
              if (ok) review("archive")
            })}>{memoryWord("archiveSource")}</button>
            <button type="button" disabled={busy} onClick={() => {
              if (detail.replacement) setDraft({ body: detail.replacement.body, original: detail.replacement.body })
            }}>{memoryWord("merge")}</button>
          </div>}
        </>}
      </>}
    </section>}
    {overview && !searching && !searchError && (tab === "entries" ? shownEntries.length > 0 ? <ul className="project-memory-list">
      {shownEntries.map(entry => <li key={entry.name}><button type="button" disabled={busy}
        aria-pressed={selected?.name === entry.name} onClick={event => void open({ name: entry.name }, event.currentTarget)}>
        <strong>{entry.description}</strong><span>{entry.name} · {entry.group || memoryWord(kinds[entry.type])}</span>
        {entry.pending && <small>{memoryWord("pending")}</small>}
      </button></li>)}
    </ul> : <p role="status">{memoryWord(submitted ? "noMatches" : "emptyEntries")}</p> :
      shownChanges.length > 0 ? <ul className="project-memory-list">
        {shownChanges.map(row => <li key={row.name}><button type="button" disabled={busy}
          aria-pressed={selected?.name === row.name} onClick={event => void open(tab === "archive"
            ? { name: row.name, archived: true } : { name: row.name, comparison: row }, event.currentTarget)}>
          <strong>{row.description ?? byName.get(row.name)?.description ?? row.name}</strong>
          <span>{row.name} → {row.replacement}</span>
          <small>{memoryWord(reasons[row.reason] ?? "reviewed")}</small>
        </button></li>)}
      </ul> : <p role="status">{memoryWord(submitted ? "noMatches" : tab === "archive" ? "emptyArchive" : "emptySuggestions")}</p>)}
  </section>
}

function MemoryComparison({ source, replacement }: { source: ProjectMemoryEntry; replacement: ProjectMemoryEntry }) {
  const destinationLines = new Set(replacement.body.split("\n").map(line => line.trim()))
  return <div className="project-memory-comparison">
    <section>
      <h4>{memoryWord("source")}</h4>
      <small>{source.name}</small>
      <pre className="project-memory-body" tabIndex={0}>{source.body.split("\n").map((line, i) =>
        line.trim() && !destinationLines.has(line.trim())
          ? <mark key={i}>{line}{"\n"}</mark>
          : <span key={i}>{line}{"\n"}</span>)}</pre>
      <p className="project-memory-muted">{memoryWord("uniqueLines")}</p>
    </section>
    <section>
      <h4>{memoryWord("replacement")}</h4>
      <small>{replacement.name}</small>
      <pre className="project-memory-body" tabIndex={0}>{replacement.body}</pre>
    </section>
  </div>
}
