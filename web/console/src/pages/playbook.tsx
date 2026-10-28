import { ActionProgress } from "../ui/ActionProgress.js"
import { catalogWord } from "../catalog.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { useEffect, useRef, useState } from "react"
import { focusModalContainer } from "../modal-focus.js"
import type { Snippet, PlaybookSource } from "../legacy/snippets-bridge.js"
import { readProjectPlaces, type ProjectPlace } from "./work/api.js"
import { listProjectFiles, readProjectFile, type ProjectFile } from "./projects/project-files-api.js"
import { createPlaybook, deletePlaybook, readReusableTextForMachine, SnippetFailure, updatePlaybook } from "../session/snippets-api.js"
import type { PageModule } from "./types.js"
import "../page-skeleton.css"
import "./playbook/playbook.css"
import { playbookWord as W } from "../playbook-words.js"
import { playbookSourceMatches, playbookSourceText, playbookSourceTitle } from "../playbook-source.js"
import { Mark } from "../session/List.js"
import type { SessionRow } from "@clawdline/contract"

type Draft = { id: string; title: string; body: string; global: boolean; source: PlaybookSource | null }
const blank = (global: boolean): Draft => ({ id: "", title: "", body: "", global, source: null })
const machineOf = (place: ProjectPlace): string => (place as ProjectPlace & { machine?: string }).machine ?? ""
const availableSkills = (files: ProjectFile[]) => files.filter((file) => (file.kind === "skill" || file.kind === "command") && file.status === "ready")
const skillTitle = playbookSourceTitle
const launchTextFor = (file: ProjectFile) => playbookSourceText(file)

function GlobalMark() {
  return <svg className="playbook-global-mark" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
    <circle cx="12" cy="12" r="9" />
    <path d="M3 12h18M12 3c-2.5 2.4-4 5.6-4 9s1.5 6.6 4 9M12 3c2.5 2.4 4 5.6 4 9s-1.5 6.6-4 9" />
  </svg>
}

/** An independent Playbook owns its launch text; a linked one uses source-derived text. */
function PlaybookPage({ shown }: { shown: boolean }) {
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [place, setPlace] = useState<ProjectPlace | null>(null)
  const [globalView, setGlobalView] = useState(false)
  const [rows, setRows] = useState<Snippet[]>([])
  const [skills, setSkills] = useState<ProjectFile[]>([])
  const [skillsUnavailable, setSkillsUnavailable] = useState(false)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const [pendingAction, setPendingAction] = useState<"save" | "delete" | "">("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const [projectOpen, setProjectOpen] = useState(false)
  const [skillOpen, setSkillOpen] = useState(false)
  const [selectedSkill, setSelectedSkill] = useState<ProjectFile | null>(null)
  const [skillText, setSkillText] = useState("")
  const [skillVersion, setSkillVersion] = useState("")
  const [linkingDraft, setLinkingDraft] = useState(false)
  const [sourcePreview, setSourcePreview] = useState<PlaybookSource | null>(null)
  const [sourceText, setSourceText] = useState("")
  const [sourceVersion, setSourceVersion] = useState("")
  const [sourceError, setSourceError] = useState("")
  const [sourceBusy, setSourceBusy] = useState(false)
  const [sourceCheck, setSourceCheck] = useState<"checking" | "matched" | "unavailable">("checking")
  const sourceRequest = useRef(0)
  const sourceReturnFocus = useRef<HTMLElement | null>(null)
  const sourceDialog = useRef<HTMLDivElement>(null)
  const [skillBusy, setSkillBusy] = useState(false)
  const [skillError, setSkillError] = useState("")
  const skillRequest = useRef(0)
  const skillDialog = useRef<HTMLDivElement>(null)
  const skillReturnFocus = useRef<HTMLElement | null>(null)
  const projectPicker = useRef<HTMLDivElement>(null)
  const editor = useRef<HTMLDivElement>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  const openDraft = (next: Draft) => {
    if (!draft) returnFocus.current = document.activeElement as HTMLElement | null
    setError("")
    setDraft(next)
  }
  const closeSkill = () => {
    skillRequest.current++
    setSkillOpen(false); setSelectedSkill(null); setSkillText(""); setSkillVersion(""); setSkillError(""); setSkillBusy(false)
  }
  const chooseSkill = async (skill: ProjectFile) => {
    if (!place) return
    const request = ++skillRequest.current
    setSelectedSkill(skill); setSkillText(""); setSkillError(""); setSkillBusy(true)
    try {
      const answer = await readProjectFile(place.id, skill.id)
      if (request === skillRequest.current) { setSelectedSkill(answer.file); setSkillText(answer.text); setSkillVersion(answer.version) }
    } catch { if (request === skillRequest.current) setSkillError(W("skillReadFailed")) }
    finally { if (request === skillRequest.current) setSkillBusy(false) }
  }
  useEffect(() => {
    if (!skillOpen) return
    focusModalContainer(skillDialog.current)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); closeSkill(); return }
      if (event.key !== "Tab") return
      const controls = Array.from(skillDialog.current?.querySelectorAll<HTMLElement>("button:not(:disabled)") ?? [])
      if (!controls.length) return
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => { document.removeEventListener("keydown", onKey); skillReturnFocus.current?.focus(); skillReturnFocus.current = null }
  }, [skillOpen])
  useEffect(() => { closeSkill() }, [place?.id, shown])

  const showSource = async (source: PlaybookSource) => {
    sourceReturnFocus.current = document.activeElement as HTMLElement | null
    const request = ++sourceRequest.current
    setSourcePreview(source); setSourceText(""); setSourceVersion(""); setSourceError(""); setSourceBusy(true)
    try {
      const answer = await readProjectFile(source.place_id, source.file_id)
      if (answer.file.kind !== source.kind) throw new Error("source changed")
      if (request === sourceRequest.current) { setSourceText(answer.text); setSourceVersion(answer.version) }
    } catch { if (request === sourceRequest.current) setSourceError(W("sourceUnavailable")) }
    finally { if (request === sourceRequest.current) setSourceBusy(false) }
  }
  const closeSource = () => {
    sourceRequest.current++
    setSourcePreview(null)
    sourceReturnFocus.current?.focus()
    sourceReturnFocus.current = null
  }
  useEffect(() => {
    if (!sourcePreview) return
    const closeButton = sourceDialog.current?.querySelector<HTMLElement>("header button")
    focusModalContainer(sourceDialog.current)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); closeSource() }
      if (event.key === "Tab" && closeButton) { event.preventDefault(); closeButton.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [sourcePreview])

  useEffect(() => {
    if (!draft) return
    focusModalContainer(editor.current)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); if (!busyRef.current) setDraft(null); return }
      if (event.key !== "Tab") return
      const controls = Array.from(editor.current?.querySelectorAll<HTMLElement>("input, textarea, button:not(:disabled)") ?? [])
      if (!controls.length) return
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && (document.activeElement === first || document.activeElement === editor.current?.querySelector("form"))) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKey)
    return () => { document.removeEventListener("keydown", onKey); returnFocus.current?.focus(); returnFocus.current = null }
  }, [!!draft])

  useEffect(() => {
    const source = draft?.source
    if (!source) return
    let live = true
    setSourceCheck("checking")
    readProjectFile(source.place_id, source.file_id).then((answer) => {
      if (!live) return
      const matches = playbookSourceMatches({ source }, answer.file)
      setSourceCheck(matches ? "matched" : "unavailable")
      if (matches) setDraft((current) => current?.source?.file_id === source.file_id && current.source.place_id === source.place_id
        ? { ...current, body: launchTextFor(answer.file).body,
            source: { ...current.source, scope: answer.file.source, name: playbookSourceTitle(answer.file), version: answer.version } } : current)
    }).catch(() => { if (live) setSourceCheck("unavailable") })
    return () => { live = false }
  }, [draft?.source?.file_id, draft?.source?.place_id, draft?.source?.name, draft?.body])

  useEffect(() => {
    if (!projectOpen) return
    const closeOutside = (event: PointerEvent) => {
      if (!projectPicker.current?.contains(event.target as Node)) setProjectOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => { if (event.key === "Escape") { event.preventDefault(); setProjectOpen(false) } }
    document.addEventListener("pointerdown", closeOutside)
    document.addEventListener("keydown", closeOnEscape)
    return () => { document.removeEventListener("pointerdown", closeOutside); document.removeEventListener("keydown", closeOnEscape) }
  }, [projectOpen])

  useEffect(() => {
    if (!shown) return
    let live = true
    setLoading(true)
    readProjectPlaces().then((answer) => {
      if (!live) return
      setPlaces(answer.places ?? [])
      setPlace((current) => answer.places.find((candidate) => candidate.id === current?.id) ?? answer.places[0] ?? null)
      if (!answer.places.length) setGlobalView(true)
      setError("")
    }).catch(() => { if (live) setError(W("projectUnavailable")) })
      .finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [shown])

  const visibleRows = (items: Snippet[], selected: ProjectPlace | null, global: boolean) => items.filter((row) =>
    row.scope === "playbook" && (global ? !row.project : row.project === selected?.path))
  const visibleSkills = (files: ProjectFile[], global: boolean) => availableSkills(files).filter((file) => !global || file.source === "global")

  const refresh = async (selected: ProjectPlace | null, global = globalView) => {
    try {
      const [answer, files] = await Promise.all([
        readReusableTextForMachine(selected ? machineOf(selected) : ""),
        selected ? listProjectFiles(selected.id).then((files) => ({ files: files.files, unavailable: false })).catch(() => ({ files: [] as ProjectFile[], unavailable: true })) : Promise.resolve({ files: [] as ProjectFile[], unavailable: false }),
      ])
      setRows(visibleRows(answer.snippets ?? [], selected, global))
      setSkills(visibleSkills(files.files, global))
      setSkillsUnavailable(files.unavailable)
      setError("")
      return true
    } catch { setError(W("playbookUnavailable")); return false }
  }

  useEffect(() => {
    if (!shown || (!place && !globalView)) return
    let live = true
    setLoading(true)
    setRows([])
    setSkills([])
    setSkillsUnavailable(false)
    Promise.all([readReusableTextForMachine(place ? machineOf(place) : ""), place ? listProjectFiles(place.id).then((files) => ({ files: files.files, unavailable: false })).catch(() => ({ files: [] as ProjectFile[], unavailable: true })) : Promise.resolve({ files: [] as ProjectFile[], unavailable: false })])
      .then(([answer, files]) => {
        if (!live) return
        setRows(visibleRows(answer.snippets ?? [], place, globalView))
        setSkills(visibleSkills(files.files, globalView))
        setSkillsUnavailable(files.unavailable)
        setError("")
      }).catch(() => { if (live) setError(W("playbookUnavailable")) })
      .finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [shown, place, globalView])

  const save = async () => {
    if ((!place && !draft?.global) || !draft || busyRef.current) return
    if (draft.source && (sourceCheck !== "matched" || (draft.global && draft.source.scope === "project"))) {
      setError(W(draft.global && draft.source.scope === "project" ? "sourceScopeConflict" : "sourceUnavailable")); return
    }
    const title = draft.title.trim()
    const body = draft.body.trim()
    if (!title || !body) { setError(W("needsText")); return }
    if (new TextEncoder().encode(title).length > 200 || new TextEncoder().encode(body).length > 4000) {
      setError(W("tooLong")); return
    }
    busyRef.current = true; setPendingAction("save"); setBusy(true); setError("")
    try {
      if (draft.id) await updatePlaybook(place ? machineOf(place) : "", draft.id, title, body, draft.source)
      else await createPlaybook(place ? machineOf(place) : "", draft.global ? "" : place!.path, title, body, draft.source ?? undefined)
      if (await refresh(place)) setDraft(null)
    } catch (cause) { setError(cause instanceof SnippetFailure && cause.code === "snippet_limit_reached" ? W("limitReached")
      : cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("saveRefused") : W("saveUnknown")) }
    finally { busyRef.current = false; setBusy(false) }
  }

  const remove = async () => {
    if ((!place && !draft?.global) || !draft?.id || busyRef.current) return
    if (!window.confirm(W("confirmDelete", { name: draft.title }))) return
    busyRef.current = true; setPendingAction("delete"); setBusy(true); setError("")
    try { await deletePlaybook(place ? machineOf(place) : "", draft.id); if (await refresh(place)) setDraft(null) }
    catch (cause) { setError(cause instanceof SnippetFailure && cause.status !== null && cause.status < 500 ? W("deleteRefused") : W("deleteUnknown")) }
    finally { busyRef.current = false; setBusy(false) }
  }

  const importSkill = () => {
    if (!selectedSkill || !skillText || !skillVersion || skillBusy || !place) return
    const text = launchTextFor(selectedSkill)
    const source: PlaybookSource = { kind: selectedSkill.kind as "skill" | "command", file_id: selectedSkill.id, place_id: place.id, scope: selectedSkill.source, name: text.title, version: skillVersion }
    closeSkill()
    if (linkingDraft && draft) setDraft({ ...draft, ...text, source })
    else openDraft({ id: "", ...text, global: globalView, source })
    setLinkingDraft(false)
  }

  const skillPicker = <div className="playbook-skill-picker">
    <button type="button" disabled={!skills.length} onClick={() => {
      skillReturnFocus.current = document.activeElement as HTMLElement | null
      setLinkingDraft(false); setSkillOpen(true)
    }}>{W("importSkill")}</button>
    {!skills.length && !loading && <small>{W(skillsUnavailable ? "skillsUnavailable" : globalView ? "noGlobalSkills" : "noSkills")}</small>}
  </div>

  return <section className="page playbook-page" id="playbook" data-page-view="playbook" hidden={!shown}>
    <header className="playbook-head"><div><h1 tabIndex={-1}>{W("playbook")}</h1><p>{W("intro")}</p></div>
      <div className="playbook-head-tools"><div className="playbook-project" ref={projectPicker}><span>{W("scopeLabel")}</span>
        <button type="button" className="playbook-project-trigger" aria-label={`${W("scopeLabel")}: ${globalView ? W("global") : place?.label || place?.path || ""}`} aria-expanded={projectOpen}
          onClick={() => setProjectOpen(!projectOpen)}>{globalView ? <GlobalMark /> : place && <Mark icon={place.icon as SessionRow["icon"]} cellPx={3} />}
          <span>{globalView ? W("global") : place?.label || place?.path || W("projectLabel")}</span><span aria-hidden="true">⌄</span></button>
        {projectOpen && <div className="playbook-project-menu"><button type="button" aria-current={globalView ? "true" : undefined} onClick={() => {
          setDraft(null); setGlobalView(true); setProjectOpen(false)
        }}><GlobalMark /><span>{W("global")}</span>{globalView && <span aria-hidden="true">✓</span>}</button>
          {places.map((row) => <button
          type="button" aria-current={!globalView && row.id === place?.id ? "true" : undefined} key={row.id} onClick={() => {
            setDraft(null); setPlace(row); setGlobalView(false); setProjectOpen(false)
          }}><Mark icon={row.icon as SessionRow["icon"]} cellPx={3} /><span>{row.label || row.path}</span>
          {!globalView && row.id === place?.id && <span aria-hidden="true">✓</span>}</button>)}</div>}
      </div>
        <button className="playbook-page-reload" type="button" onClick={() => void refresh(place)} disabled={(!place && !globalView) || loading || busy}>{W("reload")}</button>
      </div>
    </header>
    {loading && <PlaybookSkeleton label={W("loading")} />}
    {error && !draft && <p role="alert" className="playbook-error">{error}</p>}
    {(place || globalView) && <>
      {!loading && rows.length > 0 && <div className="playbook-toolbar"><h2>{W("playbookCount")} <span>{rows.length}</span></h2>
        <div className="playbook-actions"><button className="playbook-primary" type="button" onClick={() => openDraft(blank(globalView))}>{W("new")}</button>{skillPicker}</div>
      </div>}
      {!loading && rows.length === 0 && <div className="playbook-empty"><div className="playbook-empty-mark" aria-hidden="true">↗</div>
        <h2>{W(globalView ? "emptyGlobalTitle" : "emptyTitle")}</h2><p>{W(globalView ? "emptyGlobalDetail" : "emptyDetail")}</p>
        <div className="playbook-actions"><button className="playbook-primary" type="button" onClick={() => openDraft(blank(globalView))}>{W("new")}</button>{skillPicker}</div>
      </div>}
      {!loading && <div className="playbook-list">{rows.map((row) => <div className="playbook-list-card" key={row.id}>
        <button type="button" className="playbook-list-main"
          onClick={() => openDraft({ id: row.id, title: row.title, body: row.body, global: !row.project, source: row.source ?? null })}>
          <span className="playbook-list-copy"><strong>{row.title}</strong><span>{row.body}</span></span><span className="playbook-edit-label">{W("editAction")} <span aria-hidden="true">→</span></span>
        </button>{row.source && <button type="button" className="playbook-list-source" onClick={() => void showSource(row.source!)}>
          <span aria-hidden="true">↗</span> {row.source.kind === "command" ? W("claudeCommand") : "Skill"} · {row.source.name}</button>}
      </div>)}</div>}
    </>}
    {draft && <div ref={editor} className="playbook-editor" role="dialog" aria-modal="true" aria-label={draft.id ? W("edit") : W("new")}
      onPointerDown={(event) => { if (event.target === event.currentTarget && !busy) setDraft(null) }}>
      <form tabIndex={-1} onSubmit={(event) => { event.preventDefault(); void save() }}>
        <h2>{draft.id ? W("edit") : W("new")}</h2>
        <div className="playbook-source-row"><span>{W("sourceLabel")}</span>{draft.source ? <div className="playbook-source-card">
          <button className="playbook-source-link" type="button" onClick={() => void showSource(draft.source!)}><span aria-hidden="true">↗</span><span><small>{draft.source.kind === "command" ? W("claudeCommand") : "Skill"}</small><strong>{draft.source.name}</strong></span></button>
          <button className="playbook-source-unlink" type="button" aria-label={W("unlinkSource")} title={W("unlinkSource")}
            onClick={() => setDraft({ ...draft, source: null })}><span aria-hidden="true">×</span></button>
        </div> : <span>{W("noSource")}</span>}
          <button type="button" onClick={() => { skillReturnFocus.current = document.activeElement as HTMLElement | null; setLinkingDraft(true); setSkillOpen(true) }} disabled={!skills.length}>{W(draft.source ? "changeSource" : "linkSource")}</button>
        </div>
        {draft.source && <p className="playbook-linked-hint">{W("linkedNameHint")}</p>}
        {draft.source && sourceCheck === "unavailable" && <p className="playbook-source-mismatch" role="status">{W("sourceUnavailable")}</p>}
        {draft.source && draft.global && draft.source.scope === "project" && <p className="playbook-source-mismatch" role="status">{W("sourceScopeConflict")}</p>}
        <label>{W("name")}<input value={draft.title} maxLength={200} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
        <p className="playbook-scope-hint">{W("availability")}: {draft.global ? W("global") : place?.label || place?.path}</p>
        {draft.global && <p className="playbook-scope-hint">{draft.source?.scope === "project" ? W("globalSourceHint") : W("globalHint")}</p>}
        <label className={draft.source ? "playbook-message-locked" : undefined}>{W("message")}
          {draft.source && <small>{W("linkedMessageHint")}</small>}
          <textarea value={draft.body} readOnly={!!draft.source} rows={8} maxLength={4000} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /></label>
        {error && <p role="alert" className="playbook-error">{error}</p>}
        <div className="playbook-actions"><button className="playbook-primary" type="submit" aria-busy={busy && pendingAction === "save"} disabled={busy || !!draft.source && (sourceCheck !== "matched" || draft.global && draft.source.scope === "project")}><ActionProgress busy={busy && pendingAction === "save"} label={catalogWord("literal", "21aa64dd7446")}>{W("save")}</ActionProgress></button>
          <button type="button" disabled={busy} onClick={() => setDraft(null)}>{W("cancel")}</button>
          {draft.id && <button className="playbook-delete" type="button" disabled={busy} aria-busy={busy && pendingAction === "delete"} onClick={() => void remove()}><ActionProgress busy={busy && pendingAction === "delete"} label={catalogWord("literal", "a5df1fce3ed1")}>{W("delete")}</ActionProgress></button>}</div>
      </form>
    </div>}
    {skillOpen && <div ref={skillDialog} className="playbook-skill-overlay" role="dialog" aria-modal="true" aria-labelledby="playbook-skill-dialog-title"
      onPointerDown={(event) => { if (event.target === event.currentTarget) closeSkill() }}>
      <div className="playbook-skill-dialog">
        <header><div><h2 id="playbook-skill-dialog-title">{selectedSkill ? selectedSkill.kind === "command" ? W("commandPreviewTitle") : W("skillPreviewTitle") : W("skillListTitle")}</h2>
          {selectedSkill && <p>{skillTitle(selectedSkill)}</p>}</div><button type="button" onClick={closeSkill} aria-label={W("cancel")}>×</button></header>
        {selectedSkill ? <>
          <div className="playbook-skill-content">
            <p className="playbook-skill-description">{selectedSkill.description || W("skillNoDescription")}</p>
            {skillBusy && <LoadingSkeleton id="SK-30" kind="text" label={W("loading")} />}
            {skillError && <p role="alert" className="playbook-error">{skillError}</p>}
            {skillText && <pre>{skillText}</pre>}
          </div>
          <footer><button type="button" onClick={() => { skillRequest.current++; setSelectedSkill(null); setSkillText(""); setSkillError(""); setSkillBusy(false) }}>{W("backToSkills")}</button>
            {skillError && <button type="button" onClick={() => void chooseSkill(selectedSkill)}>{W("reload")}</button>}
            <button className="playbook-primary" type="button" disabled={!skillText || skillBusy || !!(linkingDraft && draft?.global && selectedSkill?.source === "project")} onClick={importSkill}>{W(linkingDraft ? "replaceLinkText" : "useSkill")}</button></footer>
        </> : <div className="playbook-skill-content"><div className="playbook-skill-options">{skills.map((skill) => <button type="button" key={skill.id} onClick={() => void chooseSkill(skill)}>
          <span><strong>{skillTitle(skill)}</strong><small>{skill.source === "global" ? W("global") : W("projectOnly")} · {skill.kind === "command" ? W("claudeCommand") : skill.assistant}</small></span>
          <span className="playbook-skill-summary">{skill.description || W("skillNoDescription")}</span>
        </button>)}</div></div>}
        {!selectedSkill && <footer><button type="button" onClick={closeSkill}>{W("cancel")}</button></footer>}
        {selectedSkill && <p className="playbook-skill-hint">{linkingDraft && draft?.global && selectedSkill.source === "project" ? W("sourceScopeConflict") : linkingDraft ? W("linkHint") : W(selectedSkill.kind === "command" ? "commandImportHint" : "skillImportHint")}</p>}
      </div>
    </div>}
    {sourcePreview && <div className="playbook-skill-overlay" role="dialog" aria-modal="true" aria-label={W("sourceDetail")}
      onPointerDown={(event) => { if (event.target === event.currentTarget) closeSource() }}>
      <div className="playbook-skill-dialog" ref={sourceDialog}><header><div><h2>{W("sourceDetail")}</h2><p>{sourcePreview.kind === "command" ? W("claudeCommand") : "Skill"} · {sourcePreview.name}</p></div>
        <button type="button" onClick={closeSource} aria-label={W("cancel")}>×</button></header>
        <div className="playbook-skill-content">{sourceBusy && <LoadingSkeleton id="SK-30" kind="text" label={W("loading")} />}
          {sourceError && <p role="alert" className="playbook-error">{sourceError}</p>}
          {sourceText && <>{sourceVersion !== sourcePreview.version && <p role="status">{W("sourceChanged")}</p>}<pre>{sourceText}</pre></>}</div>
      </div>
    </div>}
  </section>
}

/** The first read: the count and three playbook cards, in the list's own geometry. */
function PlaybookSkeleton({ label }: { label: string }) {
  return <div className="page-skeleton" role="status" aria-label={label}>
    <div className="playbook-toolbar" aria-hidden="true"><span className="page-skeleton-line playbook-skeleton-count" /></div>
    <div className="playbook-list" aria-hidden="true">{[0, 1, 2].map((card) => <div className="playbook-list-card playbook-skeleton-card" key={card}>
      <span className="page-skeleton-line playbook-skeleton-title" /><span className="page-skeleton-line" /><span className="page-skeleton-line playbook-skeleton-short" />
    </div>)}</div>
  </div>
}

export const page: PageModule = { id: "playbook", requiresApiLevel: 1, Component: PlaybookPage }
