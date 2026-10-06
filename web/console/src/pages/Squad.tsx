import { catalogFormat } from "../catalog.js"
import { catalogWord } from "../catalog.js"
import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import type { Icon } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import type { PageModule } from "./types.js"
import { squadApi, type PackPreview, type SkillSourceRow, type SquadAPI } from "./squad/api.js"
import { ReceiptGate, sourceLabel, visiblePersonas, type SquadDraft, type SquadPersona, type SquadReadState, type SquadSkill, type SquadView } from "./squad/model.js"
import { ACTIVE_SKILL_BYTES, activeSkillBytes, makeSkill, sameSkill, skillInputError, type SkillTransaction } from "./squad/skill-create.js"
import "./squad/squad.css"

const keyOf = (scope: string, persona: string) => `${scope}\u0000${persona}\u0000handbook`
const errorCode = (error: unknown) => typeof (error as { code?: unknown })?.code === "string" ? (error as { code: string }).code : "read_failed"
const errorStatus = (error: unknown) => typeof (error as { status?: unknown })?.status === "number" ? (error as { status: number }).status : 0
function errorDetail(error: unknown): string {
  switch (errorCode(error)) {
    case "offline": return catalogWord("literal", "104c1cd9aa9b")
    case "timeout": return catalogWord("literal", "7b8613470413")
    case "forbidden": case "unauthorized": return catalogWord("literal", "1b73a16a77bc")
    case "invalid_response": return catalogWord("literal", "82ebaf38e367")
    case "catalog_inconsistent": return catalogWord("literal", "d793d12ce9f0")
    case "version_conflict": return catalogWord("literal", "a102d79d8935")
    case "unknown_project": return catalogWord("literal", "9b1a9e00e745")
    case "archive_too_large": return catalogWord("literal", "70e59c98260c")
    case "archive_invalid": case "manifest_invalid": case "invalid_archive": return catalogWord("literal", "b1841a4d668e")
    case "private_confirmation_required": return catalogWord("literal", "723875f08620")
    case "skill_folder_export_unsupported": return catalogWord("literal", "6f0efc37b91f")
    case "project_required": return catalogWord("literal", "5b1368f9b563")
    case "skill_source_missing": case "skill_source_changed": return catalogWord("literal", "86d122327ca8")
    case "unsupported_route": return catalogWord("literal", "7d6137152666")
    case "skill_collision": return catalogWord("literal", "55c20c96a7cb")
    default: {
      const status = errorStatus(error)
      return catalogFormat("template", "0efa5a61894d", [status ? `（HTTP ${status}）` : ""])
    }
  }
}
function readErrorTitle(code: string): string {
  switch (code) {
    case "offline": case "timeout": return catalogWord("literal", "0617e0306ab4")
    case "forbidden": case "unauthorized": return catalogWord("literal", "96f51882b0b5")
    case "invalid_response": return catalogWord("literal", "d7dc16add85b")
    default: return catalogWord("literal", "23c894b01687")
  }
}

function IconCanvas({ icon, size = 3 }: { icon: Icon; size?: number }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => { L.paintIcon(ref.current, icon, size) }, [icon, size])
  return <canvas ref={ref} className="squad-icon" width={0} height={0} aria-hidden="true" />
}

function valueSource(source: "default" | "global" | "project", project: boolean) {
  return project && source !== "project" ? catalogWord("literal", "2ab97cab85ef") + sourceLabel(source) + "」" : sourceLabel(source)
}

function skillSourceName(source: string): string {
  const imported = /^imported:(project|claude-code|codex):(folder|text)$/.exec(source)
  if (!imported) return source
  const provider = imported[1] === "project" ? "Project" : imported[1] === "claude-code" ? "Claude Code" : "Codex"
  return `${provider} · ${imported[2] === "folder" ? catalogWord("literal", "12a88fbe7e6a") : catalogWord("literal", "c58e17097c46")}`
}

function hasLocalOverride(source: "default" | "global" | "project", project: boolean): boolean {
  return source === (project ? "project" : "global")
}

function LinkedText({ text }: { text: string }) {
  return <>{text.split(/(https?:\/\/[^\s<>"']+)/g).map((part, index) => {
    const target = part.replace(/[),.;]+$/, "")
    const suffix = part.slice(target.length)
    let url: URL | null = null
    try { url = new URL(target) } catch { url = null }
    const link = url && (url.protocol === "https:" || url.protocol === "http:")
    return <Fragment key={`${index}-${part}`}>{link
      ? <><a href={target} target="_blank" rel="noopener noreferrer">{target}</a>{suffix}</> : part}</Fragment>
  })}</>
}

function PersonaDefinition({ persona }: { persona: SquadPersona }) {
  const body = useRef<HTMLDivElement>(null)
  const [long, setLong] = useState(false)
  const [expanded, setExpanded] = useState(false)
  useLayoutEffect(() => {
    const node = body.current
    if (!node) return
    const measure = () => setLong(node.scrollHeight > 8 * parseFloat(getComputedStyle(node).lineHeight) + 1)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [persona.body])
  return <>
    <div ref={body} id={`squad-definition-${persona.id}`} className="squad-long-text squad-definition-text" data-expanded={expanded}><LinkedText text={persona.body} /></div>
    {long && <button className="squad-definition-toggle" type="button" aria-expanded={expanded}
      aria-controls={`squad-definition-${persona.id}`} onClick={() => setExpanded((shown) => !shown)}>
      {expanded ? catalogWord("literal", "de0fe59826cf") : catalogWord("literal", "ee20e2bb4c67")}
    </button>}
  </>
}

function PersonaCard({ persona, selected, animated, onPick }: { persona: SquadPersona; selected: boolean; animated: boolean; onPick: () => void }) {
  return <button type="button" className="squad-persona-card" data-selected={selected || undefined} data-use={animated || undefined}
    aria-pressed={selected} onClick={onPick}>
    <span className="squad-card-head"><IconCanvas icon={persona.icon} /><span className="squad-card-names"><strong>{persona.name}</strong><small>{persona.subtitle}</small></span></span>
    <span className="squad-card-summary">{persona.summary}</span>
    <span className="squad-card-foot"><span>{persona.enabled.value ? catalogWord("literal", "2c34d6bb47d7") : catalogWord("literal", "23748aa2bb8c")}</span><span>{catalogFormat("count", "squadSkills", [persona.skills.length])}</span></span>
  </button>
}

function SkillCard({ skill, open, animated, onPick }: { skill: SquadSkill; open: boolean; animated: boolean; onPick: () => void }) {
  return <button type="button" className="squad-skill-card" data-open={open || undefined} data-use={animated || undefined}
    aria-expanded={open} aria-controls="squad-skill-detail" onClick={onPick}>
    <span className="squad-skill-title"><span>{skill.icon && <IconCanvas icon={skill.icon} size={2} />}<strong>{skill.order}. {skill.name}</strong></span><span aria-hidden="true">{open ? "⌄" : "›"}</span></span>
    <span>{skill.purpose}</span>
    <small>{skill.status === "available" ? catalogWord("literal", "46926d0c9ced") : skill.status === "unavailable" ? catalogWord("literal", "c3209054441f") : catalogWord("literal", "2ffc1c13422d")} · {skill.enabled.value ? catalogWord("literal", "fad748d33598") : catalogWord("literal", "f4cd146147ab")} · {sourceLabel(skill.enabled.source)}</small>
  </button>
}

function SkillDetail({ skill }: { skill: SquadSkill | null }) {
  return <section className="squad-skill-detail" id="squad-skill-detail" aria-label={catalogWord("inline", "df43ea8cf144")}>
    {skill ? <>
      <h4>{skill.name}</h4>
      <dl className="squad-facts"><div><dt>{catalogWord("inline", "05b36669c4ad")}</dt><dd>{skill.purpose}</dd></div><div><dt>{catalogWord("inline", "c21db84c0653")}</dt><dd><LinkedText text={skillSourceName(skill.source)} /></dd></div>
        <div><dt>{catalogWord("inline", "5f76b2bf82dd")}</dt><dd>{skill.version}</dd></div><div><dt>{catalogWord("inline", "551356fbbc1e")}</dt><dd>{skill.license || catalogWord("literal", "48f8f7971da9")}</dd></div>
        <div><dt>{catalogWord("inline", "e01b94ddd5d5")}</dt><dd>{skill.status === "available" ? catalogWord("literal", "d15524ad623c") : skill.status === "unavailable" ? catalogWord("literal", "c3209054441f") : catalogWord("literal", "9b24865cd11e")}</dd></div></dl>
      <h5>{catalogWord("inline", "7c7215c1a1ee")}</h5><div className="squad-long-text">{skill.body || catalogWord("literal", "a17f7f60b071")}</div>
      {skill.folder && <><h5>{catalogWord("inline", "b189237d831b")}</h5>{skill.files?.length ? <ul>{skill.files.map((file) => <li key={file.path}>{file.path}</li>)}</ul> : <p>{catalogWord("inline", "f975303092e1")}</p>}</>}
    </> : <p>{catalogWord("inline", "401707655263")}</p>}
  </section>
}

function SquadPageView({ shown, api = squadApi }: { shown: boolean; api?: SquadAPI }) {
  const [scope, setScope] = useState("")
  const [reading, setReading] = useState<SquadReadState>({ kind: "loading" })
  const [team, setTeam] = useState("")
  const [search, setSearch] = useState("")
  const [selected, setSelected] = useState("")
  const [skillId, setSkillId] = useState("")
  const [mobileView, setMobileView] = useState<"roster" | "detail">("roster")
  const [drafts, setDrafts] = useState<Record<string, SquadDraft>>({})
  const [busy, setBusy] = useState("")
  const [notice, setNotice] = useState<{ text: string; error?: boolean } | null>(null)
  const [pack, setPack] = useState<"import" | "export" | null>(null)
  const [skillMode, setSkillMode] = useState<"create" | "attach" | "import">("create")
  const [importKind, setImportKind] = useState<"text" | "folder">("folder")
  const [importSource, setImportSource] = useState<"project" | "claude-code" | "codex">("project")
  const [importAttachedFiles, setImportAttachedFiles] = useState<NonNullable<SkillTransaction["skill"]["files"]>>([])
  const [skillDialogOpen, setSkillDialogOpen] = useState(false)
  const [sourceRows, setSourceRows] = useState<SkillSourceRow[]>([])
  const [sourceLoading, setSourceLoading] = useState(false)
  const [sourceReload, setSourceReload] = useState(0)
  const [sourceError, setSourceError] = useState("")
  const [sourceSearch, setSourceSearch] = useState("")
  const [selectedSourceID, setSelectedSourceID] = useState("")
  const [sourceFolderError, setSourceFolderError] = useState("")
  const [sourceDetailLoading, setSourceDetailLoading] = useState(false)
  const [newSkillName, setNewSkillName] = useState("")
  const [newSkillPurpose, setNewSkillPurpose] = useState("")
  const [newSkillContent, setNewSkillContent] = useState("")
  const [existingSkillId, setExistingSkillId] = useState("")
  const [skillError, setSkillError] = useState("")
  const [skillBusy, setSkillBusy] = useState(false)
  const [skillCloseConfirm, setSkillCloseConfirm] = useState(false)
  const [skillWriteError, setSkillWriteError] = useState<{ id: string; text: string } | null>(null)
  const [packPreview, setPackPreview] = useState<PackPreview | null>(null)
  const [packFile, setPackFile] = useState<File | null>(null)
  const [packError, setPackError] = useState("")
  const [packChoices, setPackChoices] = useState<Record<string, string>>({})
  const [importPrivateScopes, setImportPrivateScopes] = useState<string[]>([])
  const [importPrivateConfirm, setImportPrivateConfirm] = useState(false)
  const [includeGlobal, setIncludeGlobal] = useState(false)
  const [includeProjects, setIncludeProjects] = useState<string[]>([])
  const [privateConfirm, setPrivateConfirm] = useState(false)
  const [using, setUsing] = useState<{ persona: string; skill: string; session: string } | null>(null)
  const [motionReduced, setMotionReduced] = useState(() => matchMedia("(prefers-reduced-motion: reduce)").matches)
  const request = useRef(0)
  const currentScope = useRef("")
  const wasShown = useRef(false)
  const cardRefs = useRef(new Map<string, HTMLDivElement>())
  const returnScroll = useRef(0)
  const detailTitle = useRef<HTMLHeadingElement>(null)
  const detailPanel = useRef<HTMLDivElement>(null)
  const pageElement = useRef<HTMLElement>(null)
  const packDialog = useRef<HTMLDialogElement>(null)
  const skillDialog = useRef<HTMLDialogElement>(null)
  const skillOpener = useRef<HTMLButtonElement>(null)
  const skillNameInput = useRef<HTMLInputElement>(null)
  const skillKeepButton = useRef<HTMLButtonElement>(null)
  const skillHeading = useRef<HTMLHeadingElement>(null)
  const skillCheckbox = useRef<HTMLInputElement>(null)
  const skillDrafts = useRef<Record<string, { name: string; purpose: string; content: string; mode: "create" | "import"; kind: "text" | "folder"; source: typeof importSource; sourceID: string; files: typeof importAttachedFiles }>>({})
  const skillTransaction = useRef<SkillTransaction | null>(null)
  const restoringImportDraft = useRef(false)
  const sourceRequest = useRef(0)
  const sourceDetailRequest = useRef(0)
  const skillGlobalAtOpen = useRef("")
  const packOpener = useRef<HTMLElement | null>(null)
  const receiptGate = useRef(new ReceiptGate())
  const useTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const useQueue = useRef<{ persona: string; skill: string; session: string }[]>([])
  const usePlaying = useRef(false)
  const dataRef = useRef<SquadView | null>(null)

  const playNext = useCallback(() => {
    const next = useQueue.current.shift() ?? null
    setUsing(next)
    usePlaying.current = !!next
    if (next) useTimer.current = setTimeout(playNext, 2300)
  }, [])
  const enqueueUse = useCallback((event: { persona: string; skill: string; session: string }) => {
    useQueue.current.push(event)
    if (!usePlaying.current) playNext()
  }, [playNext])

  const load = useCallback(async (scopeId: string) => {
    const mine = ++request.current
    setReading((old) => {
      const prior = old.kind === "ready" ? old.data : old.previous
      return { kind: "loading", previous: prior?.scopeId === (scopeId || "global") ? prior : undefined }
    })
    try {
      const data = await api.read(scopeId)
      if (mine !== request.current || scopeId !== currentScope.current) return
      setReading({ kind: "ready", data })
      setSelected((current) => data.personas.some((persona) => persona.id === current) ? current : data.personas[0]?.id ?? "")
      setNotice(null)
    } catch (error) {
      if (mine !== request.current || scopeId !== currentScope.current) return
      setReading((old) => ({ kind: "error", code: errorCode(error), detail: errorDetail(error), previous: old.kind === "ready" ? old.data : old.previous }))
    }
  }, [api])

  useEffect(() => {
    if (shown && !wasShown.current) {
      currentScope.current = ""
      setScope(""); setTeam(""); setSearch(""); setSelected(""); setSkillId(""); setMobileView("roster")
      setUsing(null)
      receiptGate.current = new ReceiptGate()
      useQueue.current = []; usePlaying.current = false
      void load("")
    }
    if (!shown && wasShown.current) { request.current++; if (useTimer.current) clearTimeout(useTimer.current); useQueue.current = []; usePlaying.current = false; packDialog.current?.close(); skillDialog.current?.close(); setPack(null); setPackPreview(null); setPackFile(null); setUsing(null) }
    wasShown.current = shown
  }, [shown, load])

  useEffect(() => {
    const media = matchMedia("(prefers-reduced-motion: reduce)")
    const changed = () => setMotionReduced(media.matches)
    media.addEventListener("change", changed)
    return () => media.removeEventListener("change", changed)
  }, [])

  useEffect(() => {
    if (skillError) skillDialog.current?.querySelector(".squad-dialog-error")?.scrollIntoView({ block: "nearest" })
  }, [skillError])
  useEffect(() => { if (skillCloseConfirm) skillKeepButton.current?.focus() }, [skillCloseConfirm])

  useEffect(() => {
    if (!skillDialogOpen || skillMode !== "import") return
    const mine = ++sourceRequest.current
    const preserveDraft = restoringImportDraft.current
    setSourceRows([]); setSourceError(""); setSourceLoading(true); setSourceDetailLoading(false)
    if (!preserveDraft) {
      setSelectedSourceID(""); setNewSkillName(""); setNewSkillPurpose(""); setNewSkillContent(""); setImportAttachedFiles([]); setSourceFolderError("")
    }
    void api.skillSources(scope, importSource).then((rows) => {
      if (mine === sourceRequest.current) setSourceRows(rows)
    }, (error) => {
      if (mine === sourceRequest.current) setSourceError(errorDetail(error))
    }).finally(() => { if (mine === sourceRequest.current) setSourceLoading(false) })
    return () => { sourceRequest.current++ }
  }, [api, skillDialogOpen, skillMode, scope, importSource, sourceReload])

  useEffect(() => {
    if (!skillDialogOpen || skillMode !== "import" || !selectedSourceID) return
    if (restoringImportDraft.current) { restoringImportDraft.current = false; return }
    const mine = ++sourceDetailRequest.current
    setSourceDetailLoading(true)
    setSkillError(""); setSourceFolderError(""); setNewSkillName(""); setNewSkillPurpose(""); setNewSkillContent(""); setImportAttachedFiles([])
    void api.skillSource(scope, importSource, selectedSourceID, importKind === "folder").then((detail) => {
      if (mine !== sourceDetailRequest.current) return
      setNewSkillName(detail.name); setNewSkillPurpose(detail.purpose || catalogWord("literal", "96b1e1626bcb"))
      setNewSkillContent(detail.content); setImportAttachedFiles(detail.files)
      setSourceFolderError(detail.folder_error || "")
    }, (error) => { if (mine === sourceDetailRequest.current) setSkillError(errorDetail(error)) })
      .finally(() => { if (mine === sourceDetailRequest.current) setSourceDetailLoading(false) })
    return () => { sourceDetailRequest.current++ }
  }, [api, skillDialogOpen, skillMode, scope, importSource, selectedSourceID, importKind])

  const data = reading.kind === "ready" ? reading.data : reading.previous
  dataRef.current = data ?? null
  const shownPersonas = useMemo(() => data ? visiblePersonas(data, team, search) : [], [data, team, search])
  useEffect(() => {
    if (!data || shownPersonas.some((row) => row.id === selected)) return
    const next = shownPersonas[0]?.id ?? ""
    if (next !== selected) { setSelected(next); setSkillId("") }
  }, [data, shownPersonas, selected])
  const persona = data?.personas.find((row) => row.id === selected) ?? null
  const openSkill = persona?.skills.find((row) => row.id === skillId) ?? null
  const scopeLabel = scope ? data?.project?.name ?? "Project" : catalogWord("literal", "1cc39b45c9cb")
  const canWrite = reading.kind === "ready" && !!data?.canWrite && !data.partial
  const draftKey = persona ? keyOf(scope, persona.id) : ""
  const draft = drafts[draftKey]
  const handbookText = draft?.text ?? persona?.handbook.value ?? ""

  const chooseScope = (next: string) => {
    if (next === scope) return
    if (Object.values(drafts).some((draft) => draft.scopeId === scope) && !window.confirm(catalogWord("literal", "2c2913f4f722"))) return
    if (useTimer.current) clearTimeout(useTimer.current)
    useQueue.current = []; usePlaying.current = false; setUsing(null)
    currentScope.current = next
    setScope(next); setTeam(""); setSearch(""); setSelected(""); setSkillId(""); setMobileView("roster")
    void load(next)
  }
  const choosePersona = (id: string) => {
    returnScroll.current = pageElement.current?.scrollTop ?? 0
    setSelected(id); setSkillId(""); setMobileView("detail")
    requestAnimationFrame(() => {
      if (matchMedia("(max-width: 760px)").matches) detailPanel.current?.scrollIntoView({ block: "start" })
      else detailTitle.current?.scrollIntoView({ block: "start" })
      detailTitle.current?.focus({ preventScroll: true })
    })
  }
  const backToRoster = () => {
    setMobileView("roster")
    requestAnimationFrame(() => { cardRefs.current.get(selected)?.querySelector("button")?.focus(); pageElement.current?.scrollTo({ top: returnScroll.current }) })
  }

  const editHandbook = (text: string) => {
    if (!persona) return
    const key = keyOf(scope, persona.id)
    setDrafts((all) => ({ ...all, [key]: {
      scopeId: scope, personaId: persona.id, field: "handbook", baseVersion: all[key]?.baseVersion ?? persona.settingsVersion,
      text, serverText: all[key]?.serverText ?? persona.handbook.value,
      serverVersion: all[key]?.serverVersion ?? persona.settingsVersion, conflict: all[key]?.conflict ?? false,
    } }))
  }

  const saveHandbook = async (retry = false) => {
    if (!persona || !data) return
    const requestScope = scope
    const key = keyOf(requestScope, persona.id)
    const current = drafts[key]
    const text = current?.text ?? persona.handbook.value
    const expectedVersion = retry ? current?.serverVersion ?? persona.settingsVersion : current?.baseVersion ?? persona.settingsVersion
    setBusy("handbook"); setNotice({ text: catalogWord("literal", "5a54ddb48d6b") })
    try {
      await api.saveHandbook(requestScope, persona.id, text, expectedVersion)
      setDrafts((all) => { const next = { ...all }; delete next[key]; return next })
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: catalogWord("literal", "73b1d8d2dd8b") }) }
    } catch (error) {
      if (errorCode(error) === "version_conflict") {
        try {
          const fresh = await api.read(requestScope)
          if (currentScope.current === requestScope) setReading({ kind: "ready", data: fresh })
          const server = fresh.personas.find((row) => row.id === persona.id)
          if (server) setDrafts((all) => ({ ...all, [key]: { ...all[key], text, serverText: server.handbook.value, serverVersion: server.settingsVersion, conflict: true } }))
          if (currentScope.current === requestScope) setNotice({ text: catalogWord("literal", "6e12c4771df2"), error: true })
        } catch (rereadError) { if (currentScope.current === requestScope) setNotice({ text: catalogFormat("template", "dec5e1afdd7e", [errorDetail(rereadError)]), error: true }) }
      } else if (currentScope.current === requestScope) setNotice({ text: catalogFormat("template", "00ac5e87de81", [errorDetail(error)]), error: true })
    } finally { setBusy("") }
  }

  const restoreHandbook = async () => {
    if (!persona || !scope) return
    if (draft && !window.confirm(catalogWord("literal", "8e61b20dddaf"))) return
    const requestScope = scope
    const personaId = persona.id
    setBusy("handbook"); setNotice({ text: catalogWord("literal", "26462666ea13") })
    try {
      await api.restoreHandbook(requestScope, personaId, persona.settingsVersion)
      setDrafts((all) => { const next = { ...all }; delete next[keyOf(requestScope, personaId)]; return next })
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: catalogWord("literal", "ef4393acfe9e") }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: catalogFormat("template", "10f87c17d2f8", [errorDetail(error)]), error: true }) }
    finally { setBusy("") }
  }

  const saveToggle = async (kind: "enabled" | "motion", value: boolean) => {
    if (!data || (kind === "enabled" && !persona)) return
    const requestScope = scope
    const personaId = persona?.id ?? ""
    setBusy(kind); setNotice({ text: catalogWord("literal", "70a7ff14d82d") })
    try {
      if (kind === "motion") await api.saveMotion(requestScope, value, data.motionSettingsVersion)
      else await api.saveEnabled(requestScope, personaId, value, persona!.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: catalogWord("literal", "d294b20a8375") }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: catalogFormat("template", "dad86b25350d", [errorDetail(error)]), error: true }) }
    finally { setBusy("") }
  }

  const restoreToggle = async (kind: "enabled" | "motion") => {
    if (!data || (kind === "enabled" && !persona)) return
    const requestScope = scope
    setBusy(kind); setNotice({ text: catalogWord("literal", "26462666ea13") })
    try {
      if (kind === "motion") await api.restoreMotion(requestScope, data.motionSettingsVersion)
      else await api.restoreEnabled(requestScope, persona!.id, persona!.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: catalogWord("literal", "ef4393acfe9e") }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: catalogFormat("template", "10f87c17d2f8", [errorDetail(error)]), error: true }) }
    finally { setBusy("") }
  }

  const saveSkills = async (choices: { id: string; version: string; enabled: boolean }[]) => {
    if (!data || !persona) return
    const requestScope = scope
    const personaId = persona.id
    setBusy("skills"); setSkillWriteError(null); setNotice({ text: catalogWord("literal", "baa32ae67a58") })
    try {
      await api.saveSkills(requestScope, personaId, choices, persona.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: catalogWord("literal", "b1a5f601c188") }) }
    } catch (error) { if (currentScope.current === requestScope) {
      const text = catalogFormat("template", "75eb1ec08d49", [errorDetail(error)])
      setNotice({ text, error: true }); setSkillWriteError({ id: skillId, text })
    } }
    finally { setBusy("") }
  }
  const rereadSkill = async () => {
    if (!skillWriteError) return
    const requestScope = scope
    try {
      const fresh = await api.read(requestScope)
      if (currentScope.current === requestScope) {
        setReading({ kind: "ready", data: fresh }); setSkillWriteError(null)
        setNotice({ text: catalogWord("literal", "d7fb6eee1033") })
        requestAnimationFrame(() => skillCheckbox.current?.focus())
      }
    } catch (error) { setSkillWriteError({ ...skillWriteError, text: catalogFormat("template", "fda343356bac", [errorDetail(error)]) }) }
  }
  const currentChoices = persona?.skills.map((skill) => ({ id: skill.id, version: skill.version, enabled: skill.enabled.value })) ?? []

  const openSkillDialog = () => {
    if (!canWrite || !persona) return
    const draft = skillDrafts.current[`${scope}\u0000${persona.id}`]
    restoringImportDraft.current = !!(draft?.mode === "import" && draft.sourceID && draft.content)
    skillTransaction.current = null
    skillGlobalAtOpen.current = JSON.stringify(persona.skillsSetting.global)
    setSkillMode(draft?.mode ?? "create"); setImportKind(draft?.kind ?? "folder"); setImportSource(draft?.source ?? (scope ? "project" : "codex"))
    setNewSkillName(draft?.name ?? ""); setNewSkillPurpose(draft?.purpose ?? ""); setNewSkillContent(draft?.content ?? ""); setSelectedSourceID(draft?.sourceID ?? ""); setExistingSkillId(""); setSkillError(""); setSkillCloseConfirm(false); setImportAttachedFiles(draft?.files ?? [])
    skillDialog.current?.showModal()
    setSkillDialogOpen(true)
    requestAnimationFrame(() => (skillNameInput.current ?? skillDialog.current?.querySelector<HTMLInputElement>('input[name="squad-skill-mode"]:checked'))?.focus())
  }
  const recoverSavedSkill = () => {
    const pending = skillTransaction.current
    skillTransaction.current = null
    if (pending && currentScope.current === scope) {
      void load(scope)
      setNotice({ text: pending.catalogSaved ? catalogWord("literal", "332b027e23f9") : catalogWord("literal", "0d5ad51260a4") })
    }
  }
  const closeSkillDialog = () => {
    if (skillBusy) return
    if (!skillTransaction.current && skillMode !== "attach" && (newSkillName || newSkillPurpose || newSkillContent)) {
      setSkillCloseConfirm(true); return
    }
    recoverSavedSkill(); skillDialog.current?.close(); setSkillDialogOpen(false)
  }
  const closeSkillDraft = (keep: boolean) => {
    if (!persona) return
    const key = `${scope}\u0000${persona.id}`
    if (keep) skillDrafts.current[key] = { name: newSkillName, purpose: newSkillPurpose, content: newSkillContent,
      mode: skillMode === "import" ? "import" : "create", kind: importKind, source: importSource, sourceID: selectedSourceID, files: importAttachedFiles }
    else delete skillDrafts.current[key]
    setSkillCloseConfirm(false)
    skillDialog.current?.close(); setSkillDialogOpen(false)
  }
  const addSkill = async () => {
    if (!canWrite || !persona || !data || skillBusy) return
    if (skillMode === "import" && (sourceFolderError || !selectedSourceID || !newSkillContent)) {
      setSkillError(sourceFolderError || catalogWord("literal", "5f7801717fea"))
      return
    }
    const originalScope = scope
    const originalPersona = persona.id
    let target: { id: string; version: string; body: string; files?: { path: string; content_base64: string }[] } | null = null
    if (skillMode !== "attach") {
      const inputError = skillInputError(newSkillName, newSkillPurpose, newSkillContent)
      if (inputError) { setSkillError(inputError); return }
      if (!skillTransaction.current) {
        skillTransaction.current = {
          skill: { ...makeSkill(newSkillName, newSkillPurpose, newSkillContent, persona.icon),
            ...(skillMode === "import" ? { source: `imported:${importSource}:${importKind}`, content: newSkillContent,
              folder: importKind === "folder",
              files: importKind === "folder" ? importAttachedFiles : [] } : {}) },
          expectedVersion: data.catalogVersion, key: crypto.randomUUID(), scopeId: originalScope, personaId: originalPersona, catalogSaved: false,
        }
        delete skillDrafts.current[`${originalScope}\u0000${originalPersona}`]
      }
      const transaction = skillTransaction.current
      target = { id: transaction.skill.skill_id, version: transaction.skill.version, body: transaction.skill.content, files: transaction.skill.files }
    } else {
      const selected = data.catalogSkills.find((skill) => skill.id === existingSkillId)
      if (!selected) { setSkillError(catalogWord("literal", "743c4d79a088")); return }
      target = { id: selected.id, version: selected.version, body: selected.body, files: selected.files }
    }
    if (activeSkillBytes(data, persona, target) > ACTIVE_SKILL_BYTES) {
      setSkillError(catalogWord("literal", "e0ad76a1ac2d"))
      return
    }
    setSkillBusy(true); setSkillError("")
    try {
      const transaction = skillTransaction.current
      if (skillMode !== "attach" && transaction && !transaction.catalogSaved) {
        try {
          await api.createSkill(transaction.skill, transaction.expectedVersion, transaction.key)
          transaction.catalogSaved = true
        } catch (error) {
          if (errorCode(error) !== "version_conflict" && errorCode(error) !== "definition_version_conflict") throw error
          const catalog = await api.readCatalog()
          const found = catalog.skills.find((skill) => skill.skill_id === transaction.skill.skill_id && skill.version === transaction.skill.version)
          if (found) {
            if (!sameSkill(found, transaction.skill)) throw Object.assign(new Error(catalogWord("literal", "55c20c96a7cb")), { code: "skill_collision" })
            transaction.catalogSaved = true
          } else {
            transaction.expectedVersion = catalog.catalog_version
            transaction.key = crypto.randomUUID()
            setSkillError(catalogWord("literal", "e8f46a1b7fe3"))
            return
          }
        }
      }
      // A settings write replaces the whole list. Read the target scope again
      // on every attempt, including after an uncertain response.
      const fresh = await api.read(originalScope)
      const current = fresh.personas.find((row) => row.id === originalPersona)
      if (!current) throw Object.assign(new Error(catalogWord("literal", "7e58ba1e511e")), { code: "catalog_inconsistent" })
      const latest = fresh.catalogSkills.find((skill) => skill.id === target!.id)
      if (skillMode === "attach" && (!latest || latest.version !== target.version)) {
        if (currentScope.current === originalScope) setReading({ kind: "ready", data: fresh })
        setSkillError(catalogWord("literal", "19e3b676481b"))
        return
      }
      const existing = current.skillsSetting.value.find((choice) => choice.id === target!.id)
      if (existing && (existing.version !== target.version || !existing.enabled)) {
        setSkillError(catalogWord("literal", "1f76dfcc4d46"))
        return
      }
      if (!existing) {
        if (activeSkillBytes(fresh, current, target) > ACTIVE_SKILL_BYTES) {
          setSkillError(catalogWord("literal", "aa2c7bae8939"))
          return
        }
        await api.saveSkills(originalScope, originalPersona, [...current.skillsSetting.value, { id: target.id, version: target.version, enabled: true }], current.settingsVersion)
      }
      const confirmed = await api.read(originalScope)
      const confirmedPersona = confirmed.personas.find((row) => row.id === originalPersona)
      if (!confirmedPersona?.skillsSetting.value.some((choice) => choice.id === target!.id && choice.version === target!.version && choice.enabled)) {
        setSkillError(catalogWord("literal", "4cd467a2eb33"))
        return
      }
      const globalChanged = originalScope && skillGlobalAtOpen.current !== JSON.stringify(confirmedPersona.skillsSetting.global)
      const missingGlobal = globalChanged ? confirmedPersona.skillsSetting.global.filter((choice) =>
        !confirmedPersona.skillsSetting.value.some((local) => local.id === choice.id && local.version === choice.version)) : []
      const missingNames = missingGlobal.map((choice) => confirmed.catalogSkills.find((skill) => skill.id === choice.id && skill.version === choice.version)?.name ?? choice.id)
      if (currentScope.current === originalScope) {
        setReading({ kind: "ready", data: confirmed }); setSkillId(target.id)
        setNotice({ text: globalChanged ? catalogFormat("template", "ddd756350b15", [missingNames.join("、") || catalogWord("literal", "a2ebfc6e487d")]) : catalogWord("literal", "fecb7d98be72") })
      }
      delete skillDrafts.current[`${originalScope}\u0000${originalPersona}`]
      skillTransaction.current = null
      skillDialog.current?.close(); setSkillDialogOpen(false)
    } catch (error) {
      setSkillError(catalogFormat("template", "f8ba53a245d4", [skillTransaction.current?.catalogSaved ? catalogWord("literal", "d356ce36d4f9") : catalogWord("literal", "5c64311ba406"), errorDetail(error)]))
    } finally { setSkillBusy(false) }
  }
  const moveSkill = (id: string, direction: -1 | 1) => {
    const choices = [...currentChoices]
    const index = choices.findIndex((item) => item.id === id)
    const next = index + direction
    if (index < 0 || next < 0 || next >= choices.length) return
    ;[choices[index], choices[next]] = [choices[next], choices[index]]
    void saveSkills(choices)
  }
  const restoreSkills = async () => {
    if (!persona) return
    const requestScope = scope
    setBusy("skills"); setNotice({ text: catalogWord("literal", "65c0d2b7f595") })
    try {
      await api.restoreSkills(requestScope, persona.id, persona.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: catalogWord("literal", "a3cdb451cc2d") }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: catalogFormat("template", "403eadebb370", [errorDetail(error)]), error: true }) }
    finally { setBusy("") }
  }

  const openPack = (kind: "import" | "export", opener: HTMLElement) => {
    packOpener.current = opener; setPackError(""); setPackPreview(null); setPackFile(null); setPackChoices({})
    setImportPrivateScopes([]); setImportPrivateConfirm(false)
    setIncludeGlobal(false); setIncludeProjects([]); setPrivateConfirm(false); setPack(kind)
  }
  const closePack = () => { packDialog.current?.close(); setPack(null); setPackPreview(null); setPackFile(null); packOpener.current?.focus() }
  useEffect(() => { if (pack && packDialog.current && !packDialog.current.open) packDialog.current.showModal() }, [pack])

  const previewPack = async () => {
    if (!packFile) { setPackError(catalogWord("literal", "864e8653cc52")); return }
    setBusy("preview"); setPackError("")
    try {
      const preview = await api.preview(packFile, scope || "global")
      setPackPreview(preview)
      setPackChoices((all) => Object.fromEntries(Object.entries(all).filter(([id]) => preview.conflicts.some((row) => row.id === id))))
      setImportPrivateScopes((all) => all.filter((id) => preview.privateScopes.includes(id)))
      setImportPrivateConfirm(false)
    }
    catch (error) { setPackError(catalogFormat("template", "9d654f40d3f4", [errorDetail(error)])) }
    finally { setBusy("") }
  }
  const adoptPack = async () => {
    if (!packPreview) return
    if (importPrivateScopes.length && !importPrivateConfirm) { setPackError(catalogWord("literal", "9d1b0d17990f")); return }
    setBusy("adopt"); setPackError("")
    try { await api.adopt(packPreview, packChoices, importPrivateScopes); closePack(); await load(scope); setNotice({ text: catalogWord("literal", "9a9a62474225") }) }
    catch (error) {
      if (["version_conflict", "settings_changed", "preview_expired", "preview_invalid"].includes(errorCode(error))) setPackError(catalogWord("literal", "be71ec45cc65"))
      else setPackError(catalogFormat("template", "4cc0e44397c7", [errorDetail(error)]))
      if (["version_conflict", "settings_changed", "preview_expired", "preview_invalid"].includes(errorCode(error))) setPackPreview(null)
    } finally { setBusy("") }
  }
  const exportPack = async () => {
    if ((includeGlobal || includeProjects.length > 0) && !privateConfirm) { setPackError(catalogWord("literal", "fc23e60a64d6")); return }
    setBusy("export"); setPackError("")
    try {
      const { blob, fileName } = await api.exportPackage(includeGlobal, includeProjects)
      const href = URL.createObjectURL(blob)
      const link = document.createElement("a"); link.href = href; link.download = fileName; link.click()
      setTimeout(() => URL.revokeObjectURL(href), 1000)
      closePack(); setNotice({ text: catalogWord("literal", "3445e3bbfc67") })
    } catch (error) { setPackError(catalogFormat("template", "2ee62aa05e92", [errorDetail(error)])) }
    finally { setBusy("") }
  }

  useEffect(() => {
    if (!shown || !dataRef.current) return
    let active = true
    let timer: ReturnType<typeof setTimeout> | null = null
    let live = true
    let delay = 3000
    const poll = async () => {
      try {
        const page = await api.events(receiptGate.current.after)
        if (!active) return
        const sessions = page.events.length ? await api.boundSessions().catch(() => []) : dataRef.current?.sessions ?? []
        if (!active || currentScope.current !== scope) return
        const current = dataRef.current
        if (!current || current.scopeId !== (scope || "global")) return
        for (const receipt of page.events) {
          const owner = current.personas.find((row) => row.id === receipt.definitionId)
          const session = sessions.find((row) => row.conversationId === receipt.conversationId && row.snapshotId === receipt.snapshotId && row.definitionId === receipt.definitionId && row.scopeId === receipt.scopeId)
          if (!owner || !session || !receiptGate.current.take(receipt, live, sessions, current.scopeId, owner.id, owner.skills)) continue
          enqueueUse({ persona: owner.id, skill: receipt.skillId, session: session.label })
        }
        receiptGate.current.advance(page.nextAfter)
        // A reconnect can return many pages. Keep every catch-up page quiet,
        // including the final page, then allow only later polls to animate.
        if (!page.hasMore) live = true
        delay = 3000
        timer = setTimeout(poll, page.hasMore ? 0 : delay)
      } catch { if (!active) return; live = false; delay = Math.min(delay * 2, 30000); timer = setTimeout(poll, delay) }
    }
    void api.eventHead().then((seq) => { if (active) { receiptGate.current.baseline(seq); void poll() } }).catch(() => { if (active) { live = false; timer = setTimeout(poll, delay) } })
    return () => { active = false; if (timer) clearTimeout(timer) }
  }, [shown, !!data, scope, api, enqueueUse])

  return <section id="squad" ref={pageElement} className="page squad-page" hidden={!shown} aria-labelledby="squad-title">
    <div className="squad-wrap">
      <header className="squad-header"><div><p className="squad-eyebrow">{catalogWord("inline", "23a2521dacc1")}</p><h1 id="squad-title" tabIndex={-1}>{catalogWord("inline", "7b2e2228ee05")}</h1>
        <p>{catalogWord("inline", "c2ba5dc09493")}</p></div>
        <label className="squad-scope">{catalogWord("inline", "279e7da32fd3")}<select value={scope} onChange={(event) => chooseScope(event.target.value)} disabled={!data}>
          <option value="">{catalogWord("inline", "6a1c5fd6909b")}</option>{data?.projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
        </select></label></header>
      {notice && <p className="squad-notice" role="status" aria-live="polite" data-error={notice.error || undefined}>{notice.text}</p>}
      {reading.kind === "loading" && (data ? <p className="squad-notice" role="status">{catalogWord("inline", "78427b9ef6b7")}</p> : <div className="squad-loading" role="status" aria-live="polite"><div className="squad-loading-rail" /><div className="squad-loading-roster" /><div className="squad-loading-detail" /><span>{catalogWord("inline", "d30b77c6b717")}</span></div>)}
      {reading.kind === "error" && <div className="squad-error" role="alert"><h2>{readErrorTitle(reading.code)}</h2><p>{reading.detail}</p><button type="button" onClick={() => void load(scope)}>{catalogWord("inline", "07244b042280")}</button></div>}
      {data?.partial && <div className="squad-notice" role="status">{catalogWord("inline", "08228e7394ef")}<button type="button" onClick={() => void load(scope)}>{catalogWord("inline", "07244b042280")}</button></div>}
      {data && !data.canWrite && <p className="squad-notice" role="status">{catalogWord("inline", "7bdb8640f903")}</p>}
      {data && <div className="squad-layout" data-mobile-view={mobileView}>
        <aside className="squad-rail" aria-labelledby="squad-teams-title"><div className="squad-panel-head"><h2 id="squad-teams-title">{catalogWord("inline", "d13ccaf2da5f")}</h2><span>{catalogFormat("count", "squadGroups", [data.teams.length])}</span></div>
          <div className="squad-teams" role="group" aria-label={catalogWord("inline", "7a889ffdecb5")}><button type="button" aria-pressed={!team} onClick={() => setTeam("")}>{catalogWord("inline", "2878fe586472")} <small>{data.personas.length}</small></button>
            {data.teams.map((item) => <button key={item.id} type="button" aria-pressed={team === item.id} onClick={() => setTeam(item.id)}>{item.name}<small>{data.personas.filter((row) => row.teamIds.includes(item.id)).length}</small></button>)}
          </div><div className="squad-pack-actions"><h3>{catalogWord("inline", "f438effc4cef")}</h3><p>{catalogWord("inline", "e035783117a9")}</p>
            <button type="button" onClick={(event) => openPack("import", event.currentTarget)}>{catalogWord("inline", "3d94747fe80d")}</button>
            <button type="button" onClick={(event) => openPack("export", event.currentTarget)}>{catalogWord("inline", "21ef789fd56d")}</button></div></aside>
        <div className="squad-roster" aria-labelledby="squad-roster-title"><div className="squad-panel-head"><div><h2 id="squad-roster-title">{catalogWord("inline", "9d19a6eff5a7")}</h2><p>{catalogWord("inline", "7f40e97b7bdb")}</p></div><span>{catalogFormat("count", "squadRoles", [shownPersonas.length])}</span></div>
          <label className="squad-search">{catalogWord("inline", "b5ddf97c42d3")}<input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder={catalogWord("inline", "1fa038b1af83")} /></label>
          {shownPersonas.length ? <div className="squad-cards">{shownPersonas.map((row) => <div key={row.id} ref={(node) => { if (node) cardRefs.current.set(row.id, node); else cardRefs.current.delete(row.id) }}>
            <PersonaCard persona={row} selected={selected === row.id} animated={using?.persona === row.id && data.motion.value && !motionReduced} onPick={() => choosePersona(row.id)} />
          </div>)}</div> : <div className="squad-empty"><h3>{data.personas.length ? catalogWord("literal", "03e05dc9c519") : catalogWord("literal", "b9f440f7bb3d")}</h3><p>{data.personas.length ? catalogWord("literal", "7942cb67343e") : catalogWord("literal", "3dfb93dd1061")}</p>
            {data.personas.length > 0 && <button type="button" onClick={() => { setTeam(""); setSearch("") }}>{catalogWord("inline", "6dc87a09ba68")}</button>}</div>}</div>
        <div className="squad-detail" ref={detailPanel} aria-labelledby="squad-detail-title">{persona ? <>
          <button className="squad-back" type="button" onClick={backToRoster}>{catalogWord("inline", "a614ac7abbc2")}</button>
          <div className="squad-profile"><IconCanvas icon={persona.icon} size={5} /><div><p>{catalogWord("inline", "863e4b25f68c")}</p><h2 id="squad-detail-title" ref={detailTitle} tabIndex={-1}>{persona.name}</h2><span>{persona.subtitle}</span></div></div>
          <div className="squad-meta"><span>{persona.teamIds.map((id) => data.teams.find((row) => row.id === id)?.name ?? id).join(" · ") || catalogWord("literal", "800a6095679f")}</span>
            <details className="squad-meta-detail"><summary>{catalogWord("inline", "230d9c9e189c")}</summary>
              <dl><div><dt>{catalogWord("inline", "c21db84c0653")}</dt><dd><LinkedText text={persona.source} /></dd></div>
                <div><dt>{catalogWord("inline", "5f76b2bf82dd")}</dt><dd>{persona.version}</dd></div></dl>
              {persona.version.startsWith("sha256:") && <p className="squad-version-explanation">{catalogWord("inline", "1056aea86d65")}</p>}
            </details></div>
          <button className="squad-skill-jump" type="button" onClick={() => { skillHeading.current?.scrollIntoView({ block: "start" }); skillHeading.current?.focus() }}>{catalogWord("inline", "ed1d465abc34")}</button>
          <section className="squad-detail-block"><h3>{catalogWord("inline", "3d6b6be71577")}</h3><PersonaDefinition key={persona.id} persona={persona} /></section>
          <section className="squad-detail-block"><div className="squad-setting-heading"><div><h3>{catalogWord("inline", "c4ad0c22f59e")}</h3><p>{catalogWord("inline", "4cbb34265f55")}</p></div>
            <label className="squad-switch"><input type="checkbox" aria-label={catalogFormat("template", "5c513a465185", [persona.enabled.value ? catalogWord("literal", "fad748d33598") : catalogWord("literal", "f4cd146147ab")])} checked={persona.enabled.value} disabled={!canWrite || !!busy} onChange={(event) => void saveToggle("enabled", event.target.checked)} /><span>{persona.enabled.value ? catalogWord("literal", "fad748d33598") : catalogWord("literal", "f4cd146147ab")}</span></label></div>
            <p className="squad-source">{catalogWord("inline", "a53d571b6694")}{persona.enabled.global ? catalogWord("literal", "fad748d33598") : catalogWord("literal", "f4cd146147ab")}{catalogWord("inline", "d3f92dfb1617")}{persona.enabled.value ? catalogWord("literal", "fad748d33598") : catalogWord("literal", "f4cd146147ab")}{catalogWord("inline", "017bc5141405")}{valueSource(persona.enabled.source, !!scope)}</p>
            {hasLocalOverride(persona.enabled.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreToggle("enabled")}>{catalogWord("inline", "c07fb8c4d3e4")}{scope ? catalogWord("literal", "d2520bd9c850") : catalogWord("literal", "0d9f5fa59ac1")}</button>}</section>
          <section className="squad-detail-block"><h3>{catalogWord("inline", "f9e0aed45830")}</h3><p>{catalogWord("inline", "f3ad63524f05")}</p>
            {scope && <div className="squad-handbook-global"><h4>{catalogWord("inline", "aeca8d75fe70")}</h4><div className="squad-long-text">{persona.handbook.global || catalogWord("literal", "97cfc2e0f30f")}</div></div>}
            <label className="squad-handbook-editor">{scope ? catalogFormat("template", "697a707c20a0", [scopeLabel]) : catalogWord("literal", "1e4888cc9d8d")}<textarea value={handbookText} onChange={(event) => editHandbook(event.target.value)} disabled={!canWrite || busy === "handbook"} rows={6} /></label>
            <p className="squad-source">{catalogWord("inline", "a53d571b6694")}{persona.handbook.global ? catalogWord("literal", "9512ab1d42d9") : catalogWord("literal", "4eb1c8ce4af2")}{catalogWord("inline", "d3f92dfb1617")}{persona.handbook.value ? catalogWord("literal", "9512ab1d42d9") : catalogWord("literal", "4eb1c8ce4af2")}{catalogWord("inline", "017bc5141405")}{valueSource(persona.handbook.source, !!scope)}{catalogWord("literal", "76d3e6a81a7d")}</p>
            {draft?.conflict && <div className="squad-conflict" role="alert"><h4>{catalogWord("inline", "cc133613eb04")}</h4><p>{catalogWord("inline", "ca257eafc46c")} {draft.serverVersion}：</p><div className="squad-long-text">{draft.serverText || catalogWord("literal", "4eb1c8ce4af2")}</div><p>{catalogWord("inline", "4d25dfc6d2aa")}</p>
              <button type="button" disabled={!!busy} onClick={() => void saveHandbook(true)}>{catalogWord("inline", "b6a364cc96dc")}</button>
              <button type="button" disabled={!!busy} onClick={() => setDrafts((all) => { const next = { ...all }; delete next[draftKey]; return next })}>{catalogWord("inline", "2b48bf432592")}</button></div>}
            <div className="squad-actions"><button type="button" disabled={!canWrite || !!busy || !draft || draft.conflict} onClick={() => void saveHandbook()}>{busy === "handbook" ? catalogWord("literal", "fb04fcf18a82") : catalogWord("literal", "abdf0198b028")}</button>
              {scope && persona.handbook.source === "project" && <button type="button" disabled={!canWrite || !!busy} onClick={() => void restoreHandbook()}>{catalogWord("inline", "5d696ff0caa5")}</button>}</div></section>
          <section className="squad-detail-block"><div className="squad-panel-head"><h3 ref={skillHeading} tabIndex={-1}>{catalogWord("inline", "acf536aa74c6")}</h3><span>{catalogFormat("count", "squadItems", [persona.skills.length])}</span></div><p>{catalogWord("inline", "ff8a8ba1428d")}</p>
            <button ref={skillOpener} className="squad-add-skill" type="button" disabled={!canWrite || !!busy} onClick={openSkillDialog}>{catalogWord("inline", "467910ac6e5a")}</button>
            {scope && <p className="squad-source">{catalogWord("inline", "b857d02a42b9")}</p>}
            <p className="squad-source">{catalogFormat("count", "squadSkillSources", [persona.skillsSetting.global.length, persona.skillsSetting.value.length, valueSource(persona.skillsSetting.source, !!scope)])}</p>
            {persona.skills.length ? <div className="squad-skills">{persona.skills.map((skill) => <SkillCard key={skill.id} skill={skill} open={skill.id === skillId} animated={using?.skill === skill.id && using.persona === persona.id && data.motion.value && !motionReduced} onPick={() => setSkillId(skill.id === skillId ? "" : skill.id)} />)}
              <SkillDetail skill={openSkill} />
              {openSkill && <div className="squad-skill-controls"><label className="squad-check"><input ref={skillCheckbox} type="checkbox" checked={openSkill.enabled.value} disabled={!canWrite || !!busy} onChange={(event) => void saveSkills(currentChoices.map((choice) => choice.id === openSkill.id ? { ...choice, enabled: event.target.checked } : choice))} />{catalogWord("inline", "e15fbe835198")}</label>
                <div><button type="button" disabled={!canWrite || !!busy || openSkill.order <= 1} onClick={() => moveSkill(openSkill.id, -1)}>{catalogWord("inline", "53b2ef10cb82")}</button>
                  <button type="button" disabled={!canWrite || !!busy || openSkill.order >= persona.skills.length} onClick={() => moveSkill(openSkill.id, 1)}>{catalogWord("inline", "0bbb3f0291f8")}</button></div>
                {skillWriteError?.id === openSkill.id && <div className="squad-skill-write-error" role="alert"><p>{skillWriteError.text}</p><button type="button" disabled={!!busy} onClick={() => void rereadSkill()}>{catalogWord("inline", "6e9cbfe78038")}</button></div>}
                <p className="squad-source">{catalogWord("inline", "279e8d725148")}{openSkill.enabled.value ? catalogWord("literal", "55cee0dc6306") : catalogWord("literal", "255e0ddb097b")}{catalogWord("inline", "017bc5141405")}{valueSource(openSkill.enabled.source, !!scope)}</p></div>}</div> : <div className="squad-empty"><h4>{catalogWord("inline", "359872b069c2")}</h4><p>{catalogWord("inline", "88f8f99241d8")}</p></div>}
            {hasLocalOverride(persona.skillsSetting.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreSkills()}>{catalogWord("inline", "9f517bb49515")}{scope ? catalogWord("literal", "d2520bd9c850") : catalogWord("literal", "0d9f5fa59ac1")}</button>}</section>
          <section className="squad-detail-block"><div className="squad-setting-heading"><div><h3>{catalogWord("inline", "dbdafa0c066d")}</h3><p>{catalogWord("inline", "49e8eaded35a")}</p></div>
            <label className="squad-switch"><input type="checkbox" aria-label={catalogFormat("template", "4308c1893804", [data.motion.value ? catalogWord("literal", "f423cf998c53") : catalogWord("literal", "53bb291bfdd8")])} checked={data.motion.value} disabled={!canWrite || !!busy} onChange={(event) => void saveToggle("motion", event.target.checked)} /><span>{data.motion.value ? catalogWord("literal", "f423cf998c53") : catalogWord("literal", "53bb291bfdd8")}</span></label></div>
            <p className="squad-source">{catalogWord("inline", "a53d571b6694")}{data.motion.global ? catalogWord("literal", "f423cf998c53") : catalogWord("literal", "53bb291bfdd8")}{catalogWord("inline", "d3f92dfb1617")}{data.motion.value ? catalogWord("literal", "f423cf998c53") : catalogWord("literal", "53bb291bfdd8")}{catalogWord("inline", "017bc5141405")}{valueSource(data.motion.source, !!scope)}</p>
            {hasLocalOverride(data.motion.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreToggle("motion")}>{catalogWord("inline", "c07fb8c4d3e4")}{scope ? catalogWord("literal", "d2520bd9c850") : catalogWord("literal", "0d9f5fa59ac1")}</button>}
            {using && using.persona === persona.id && <p className="squad-use" data-animate={data.motion.value && !motionReduced || undefined} role="status">{using.session}{catalogWord("inline", "a9142a836fef")} {persona.skills.find((row) => row.id === using.skill)?.name ?? catalogWord("literal", "15fd0de63f3e")}</p>}</section>
          <p className="squad-privacy">{catalogWord("inline", "79071ba84527")}</p>
        </> : <div className="squad-empty"><h2 id="squad-detail-title">{catalogWord("inline", "50d3c28324cf")}</h2><p>{catalogWord("inline", "7abc9233fed1")}</p></div>}</div>
      </div>}
      <dialog ref={packDialog} className="squad-dialog" aria-labelledby="squad-dialog-title" onClose={() => { setPack(null); setPackPreview(null); setPackFile(null); packOpener.current?.focus() }}>
        {pack && <><div className="squad-dialog-head"><h2 id="squad-dialog-title">{pack === "import" ? catalogWord("literal", "d6b8206d3925") : catalogWord("literal", "e42975cbadd3")}</h2><button type="button" aria-label={catalogWord("inline", "32639fb00553")} onClick={closePack}>×</button></div>
          {pack === "import" ? <><p>{catalogWord("inline", "29868b5f3c58")}{scopeLabel}。</p>
            <label className="squad-file">{catalogWord("inline", "de4290b91ba9")}<input type="file" accept=".zip,application/zip" onChange={(event) => { setPackFile(event.target.files?.[0] ?? null); setPackPreview(null); setImportPrivateScopes([]); setImportPrivateConfirm(false) }} /></label>
            <button type="button" disabled={!!busy} onClick={() => void previewPack()}>{busy === "preview" ? catalogWord("literal", "c1db8f8f3067") : catalogWord("literal", "0f245f311d10")}</button>
            {packPreview && <div className="squad-pack-preview"><h3>{catalogWord("inline", "6c1b7c5ced5a")}</h3><p>{catalogWord("inline", "3dd3d0b71ef3")} {packPreview.catalogVersion}{catalogWord("inline", "6176ae2b39f4")} {packPreview.digest}</p><p>{catalogWord("inline", "d3dded3e1fce")}{packPreview.source || catalogWord("literal", "48f8f7971da9")}{catalogWord("inline", "feb19f096641")}{packPreview.license || catalogWord("literal", "48f8f7971da9")}{catalogWord("inline", "52144edb58e1")}</p>
              <h4>{catalogWord("inline", "0006d696d8e1")}</h4><ul>{packPreview.additions.length ? packPreview.additions.map((item) => <li key={item}>{item}</li>) : <li>{catalogWord("inline", "da5930e53d54")}</li>}</ul>
              <h4>{catalogWord("inline", "3055a035f0eb")}</h4><ul>{packPreview.updates.length ? packPreview.updates.map((item) => <li key={item}>{item}</li>) : <li>{catalogWord("inline", "da5930e53d54")}</li>}</ul>
              <h4>{catalogWord("inline", "a4678e1de0c9")}</h4><ul>{packPreview.dependencies.length ? packPreview.dependencies.map((item) => <li key={item}>{item}</li>) : <li>{catalogWord("inline", "da5930e53d54")}</li>}</ul>
              {packPreview.privateScopes.length > 0 && <><h4>{catalogWord("inline", "d87dc3ea34a9")}</h4><p>{catalogWord("inline", "322b7c72c2cc")}</p>
                {packPreview.privateScopes.map((id) => <label className="squad-check" key={id}><input type="checkbox" checked={importPrivateScopes.includes(id)} onChange={(event) => { setImportPrivateScopes((all) => event.target.checked ? [...all, id] : all.filter((row) => row !== id)); setImportPrivateConfirm(false) }} />{catalogWord("inline", "33520f6091bc")} {id}{catalogWord("inline", "49e7168fd1fe")}</label>)}
                {importPrivateScopes.length > 0 && <label className="squad-check"><input type="checkbox" checked={importPrivateConfirm} onChange={(event) => setImportPrivateConfirm(event.target.checked)} />{catalogWord("inline", "e40e8e4c90a3")}</label>}</>}
              {packPreview.conflicts.length > 0 && <><h4>{catalogWord("inline", "6cfed87bc1c2")}</h4>{packPreview.conflicts.map((conflict) => <label className="squad-conflict-choice" key={`${conflict.kind}:${conflict.id}`}>{conflict.kind} · {conflict.id}：{conflict.reason}
                <select value={packChoices[conflict.id] ?? ""} onChange={(event) => setPackChoices((all) => ({ ...all, [conflict.id]: event.target.value }))}><option value="">{catalogWord("inline", "7bb80895727d")}</option><option value="keep">{catalogWord("inline", "c055370cad67")}</option></select></label>)}</>}
              <button type="button" disabled={!canWrite || !!busy || importPrivateScopes.length > 0 && !importPrivateConfirm || packPreview.conflicts.some((row) => !packChoices[row.id])} onClick={() => void adoptPack()}>{busy === "adopt" ? catalogWord("literal", "d9418f721a0e") : catalogWord("literal", "da1278fb9df3")}</button></div>}</>
            : <><p>{catalogWord("inline", "eb4698e366ec")}</p>
              <label className="squad-check"><input type="checkbox" checked={includeGlobal} onChange={(event) => { setIncludeGlobal(event.target.checked); setPrivateConfirm(false) }} />{catalogWord("inline", "c4ea52effc80")}</label>
              {data?.projects.map((project) => <label className="squad-check" key={project.id}><input type="checkbox" checked={includeProjects.includes(project.id)} onChange={(event) => { setIncludeProjects((rows) => event.target.checked ? [...rows, project.id] : rows.filter((id) => id !== project.id)); setPrivateConfirm(false) }} />{catalogWord("inline", "107319b8b39d")} {project.name}{catalogWord("inline", "9817fd932985")}</label>)}
          <div className="squad-export-summary"><strong>{catalogWord("inline", "c96fe5d5524b")}</strong>{catalogWord("inline", "dd926ed1c017")}{catalogWord("literal", "4572689872bb")}{includeProjects.map((id) => catalogFormat("template", "8f1566b07b7d", [data?.projects.find((project) => project.id === id)?.name ?? id])).join("")}</div>
              {data?.catalogSkills.some((skill) => skill.folder) && <p role="alert">{catalogWord("inline", "d7eb99194227")}</p>}
              {(includeGlobal || includeProjects.length > 0) && <label className="squad-check"><input type="checkbox" checked={privateConfirm} onChange={(event) => setPrivateConfirm(event.target.checked)} />{catalogWord("inline", "1b8abcd48389")}</label>}
              <button type="button" disabled={!!busy || !!data?.catalogSkills.some((skill) => skill.folder) || !canWrite && (includeGlobal || includeProjects.length > 0)} onClick={() => void exportPack()}>{busy === "export" ? catalogWord("literal", "dc0649ef049d") : catalogWord("literal", "ce5716358490")}</button></>}
          {packError && <p className="squad-dialog-error" role="alert">{packError}</p>}</>}
      </dialog>
      <dialog ref={skillDialog} className="squad-dialog squad-skill-dialog" aria-labelledby="squad-skill-dialog-title" aria-describedby="squad-skill-disclosure" onCancel={(event) => { event.preventDefault(); if (skillCloseConfirm) setSkillCloseConfirm(false); else closeSkillDialog() }} onClose={() => { recoverSavedSkill(); skillOpener.current?.focus() }}>
        <div className="squad-dialog-head"><h2 id="squad-skill-dialog-title">{catalogWord("inline", "45b244c624a8")}</h2><button type="button" aria-label={catalogWord("inline", "1aef1dbef95c")} disabled={skillBusy || skillCloseConfirm} onClick={closeSkillDialog}>×</button></div>
        <p>{catalogWord("inline", "c0506b0b753a")}{persona?.name ?? ""}{catalogWord("inline", "83249fdc0b14")}{scopeLabel}{catalogWord("inline", "5f8e73fff7dd")}</p>
        <p className="squad-skill-disclosure" id="squad-skill-disclosure">{catalogWord("inline", "9737be2133ca")}</p>
        {skillCloseConfirm ? <div className="squad-skill-confirm" role="group" aria-label={catalogWord("inline", "f3405adf34b0")}><h3>{catalogWord("inline", "f8534db79f9d")}</h3><p>{catalogWord("inline", "fc7d5b5c2ceb")}</p>
          <div className="squad-actions"><button ref={skillKeepButton} type="button" onClick={() => closeSkillDraft(true)}>{catalogWord("inline", "1e18232c29fd")}</button><button type="button" onClick={() => closeSkillDraft(false)}>{catalogWord("inline", "1357dccf1e9b")}</button><button type="button" onClick={() => setSkillCloseConfirm(false)}>{catalogWord("inline", "24a05e3c2c3b")}</button></div></div> :
        <form onSubmit={(event) => { event.preventDefault(); void addSkill() }}>
        <div className="squad-skill-mode" role="group" aria-label={catalogWord("inline", "a0c8796dcf10")}>
          <label className="squad-check"><input type="radio" name="squad-skill-mode" checked={skillMode === "create"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { if (skillMode === "import") { setNewSkillName(""); setNewSkillPurpose(""); setNewSkillContent(""); setImportAttachedFiles([]) } setSkillMode("create"); setSkillError("") }} />{catalogWord("inline", "1621c89c005c")}</label>
          <label className="squad-check"><input type="radio" name="squad-skill-mode" checked={skillMode === "attach"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setSkillMode("attach"); setSkillError("") }} />{catalogWord("inline", "2b2f66c0beb2")}</label>
          <label className="squad-check"><input type="radio" name="squad-skill-mode" checked={skillMode === "import"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setSkillMode("import"); setNewSkillName(""); setNewSkillPurpose(""); setNewSkillContent(""); setImportAttachedFiles([]); setSkillError("") }} />{catalogWord("inline", "0509576cc5a4")}</label>
        </div>
        {skillMode === "create" ? <div className="squad-skill-form">
          <label>{catalogWord("inline", "d8ac7c87e3f0")}<input ref={skillNameInput} required value={newSkillName} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillName(event.target.value)} /></label>
          <label>{catalogWord("inline", "9c9a28f5de45")}<input required value={newSkillPurpose} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillPurpose(event.target.value)} /></label>
          <p id="squad-skill-content-hint">{catalogWord("inline", "9d30e17eeefd")}</p>
          <label>{catalogWord("inline", "7c7215c1a1ee")}<textarea required rows={8} aria-describedby="squad-skill-content-hint" value={newSkillContent} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillContent(event.target.value)} /></label>
        </div> : skillMode === "attach" ? <div className="squad-skill-form"><label>{catalogWord("inline", "02973078b98f")}<select value={existingSkillId} disabled={skillBusy} onChange={(event) => { setExistingSkillId(event.target.value); setSkillError("") }}>
          <option value="">{catalogWord("inline", "33c19ff0b762")}</option>{data?.catalogSkills.filter((skill) => !persona?.skills.some((row) => row.id === skill.id)).map((skill) => <option key={skill.id} value={skill.id}>{skill.name} · {skillSourceName(skill.source)}{catalogWord("inline", "b9a72aaf401e")} {skill.version}</option>)}
        </select></label><p>{catalogWord("inline", "97bf64285368")}</p></div> :
        <div className="squad-skill-form">
          <label>{catalogWord("inline", "087e4878b476")}<select value={importSource} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setImportSource(event.target.value as typeof importSource)}><option value="project">{catalogWord("inline", "985959785319")}</option><option value="claude-code">{catalogWord("inline", "246ef8c1130d")}</option><option value="codex">{catalogWord("inline", "616efbe96852")}</option></select></label>
          <div className="squad-skill-mode" role="group" aria-label={catalogWord("inline", "2ba66ba73eff")}>
            <label className="squad-check"><input type="radio" name="squad-import-kind" checked={importKind === "folder"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setImportKind("folder"); setNewSkillContent(""); setImportAttachedFiles([]); setSkillError("") }} />{catalogWord("inline", "73c3351b4ca4")}</label>
            <label className="squad-check"><input type="radio" name="squad-import-kind" checked={importKind === "text"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setImportKind("text"); setNewSkillContent(""); setImportAttachedFiles([]); setSkillError("") }} />{catalogWord("inline", "c4a401f3689f")}</label>
          </div>
          <p>{catalogWord("inline", "eb5b504885d3")}{importSource === "project" ? catalogWord("literal", "a078de280d6e") : catalogFormat("template", "6eac929a1893", [importSource === "codex" ? " Codex" : " Claude Code"])}{catalogWord("inline", "be530b5551a2")}</p>
          <div className="squad-source-list" aria-label={catalogWord("inline", "b71045effb84")}>
            <div className="squad-source-list-head"><strong>{catalogWord("inline", "113db9dc3197")}</strong><button type="button" disabled={sourceLoading} onClick={() => setSourceReload((value) => value + 1)}>{catalogWord("inline", "358a13c304aa")}</button></div>
            {sourceRows.length > 0 && <label>{catalogWord("inline", "f57aace56ce8")}<input type="search" value={sourceSearch} onChange={(event) => setSourceSearch(event.target.value)} placeholder={catalogWord("inline", "50c8b05ea375")} /></label>}
            {sourceLoading ? <p role="status">{catalogWord("inline", "9fb52113c298")}</p> : sourceError ? <p role="alert">{sourceError}</p> : sourceRows.length === 0 ? <p role="status">{catalogWord("inline", "b0c972ae30e5")}</p> :
              <div className="squad-source-options" role="group" aria-label={catalogWord("inline", "e627673e6a28")}>{sourceRows.filter((row) => `${row.name} ${row.purpose} ${row.location}`.toLocaleLowerCase().includes(sourceSearch.toLocaleLowerCase())).map((row) =>
                <button key={row.id} type="button" className="squad-source-option" aria-pressed={selectedSourceID === row.id} disabled={skillBusy || !!skillTransaction.current} onClick={() => setSelectedSourceID(row.id)}>
                  <strong>{row.name}</strong><span>{row.purpose || catalogWord("literal", "9eb1948e6879")}</span><small>{row.location}</small>
                </button>)}{sourceRows.length > 0 && !sourceRows.some((row) => `${row.name} ${row.purpose} ${row.location}`.toLocaleLowerCase().includes(sourceSearch.toLocaleLowerCase())) && <p>{catalogWord("inline", "e93b3a514ab9")}</p>}</div>}
          </div>
          {sourceDetailLoading && <p role="status">{catalogWord("inline", "80a6f335a94e")}</p>}
          {sourceFolderError && <p role="alert">{sourceFolderError}</p>}
          {newSkillContent && <><label>{catalogWord("inline", "d8ac7c87e3f0")}<input required value={newSkillName} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillName(event.target.value)} /></label>
            <label>{catalogWord("inline", "9c9a28f5de45")}<input required value={newSkillPurpose} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillPurpose(event.target.value)} /></label>
            <p role="status">{catalogWord("inline", "57005f2f16cf")}{importKind === "folder" && !sourceFolderError ? catalogFormat("template", "c18a3d5d3927", [importAttachedFiles.length]) : ""}{catalogWord("inline", "dd6ebd6aaecd")}</p>
            <details><summary>{catalogWord("inline", "7deeb1ebd38e")}</summary><pre className="squad-long-text">{newSkillContent}</pre></details></>}
        </div>}
        {skillTransaction.current?.catalogSaved && <p className="squad-skill-stage" role="status">{catalogWord("inline", "7713cf1052e8")}</p>}
        {skillError && <p className="squad-dialog-error" role="alert">{skillError}</p>}
        <div className="squad-actions"><button type="submit" disabled={skillBusy || !canWrite || (skillMode === "import" && (sourceDetailLoading || !!sourceFolderError || !selectedSourceID || !newSkillContent))}>{skillBusy ? catalogWord("literal", "fb04fcf18a82") : skillTransaction.current ? skillTransaction.current.catalogSaved ? catalogWord("literal", "8ba083abc4e7") : catalogWord("literal", "3ec4ae599e2d") : skillMode === "create" ? catalogWord("literal", "c368508d691f") : catalogWord("literal", "68a0a3e0231e")}</button>
          <button type="button" disabled={skillBusy} onClick={closeSkillDialog}>{catalogWord("inline", "2cd0f3be8738")}</button></div>
        </form>}
      </dialog>
    </div>
  </section>
}

export const page: PageModule = { id: "squad", Component: SquadPageView }
