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
    case "offline": return "網路或 Clawdline 連線中斷。請確認連線，恢復後按「重試讀取」。"
    case "timeout": return "角色小隊服務回應逾時。請確認連線，稍後按「重試讀取」。"
    case "forbidden": case "unauthorized": return "目前的連線沒有讀取或修改權限。請重新登入或使用已配對的裝置，再重試。"
    case "invalid_response": return "角色小隊服務回傳的資料格式無法讀取。請重新整理頁面；若持續發生，稍後再試。"
    case "catalog_inconsistent": return "角色目錄與設定資料不一致。請重新讀取；若持續發生，稍後再試。"
    case "version_conflict": return "設定已有新版本。請重新讀取後檢查目前內容，再決定是否儲存。"
    case "unknown_project": return "找不到這個 Project 範圍。請重新讀取清單後再試。"
    case "archive_too_large": return "資料包超過 512 KiB 上限。請選擇較小的 ZIP 檔。"
    case "archive_invalid": case "manifest_invalid": case "invalid_archive": return "資料包格式無法通過驗證。請檢查 ZIP 檔後重新預覽。"
    case "private_confirmation_required": return "請先選擇並確認要包含的私人設定範圍。"
    case "skill_folder_export_unsupported": return "目錄含有完整技能資料夾；目前資料包無法包含附檔，因此未匯出。"
    case "project_required": return "請先在角色小隊頁面選擇已登錄的 Project，再列出該 Project 的技能。"
    case "skill_source_missing": case "skill_source_changed": return "技能來源已變更。請重新讀取清單後再選。"
    case "unsupported_route": return "這部機器尚未支援技能來源清單。請更新 Clawdline 後重試。"
    case "skill_collision": return "同一技能識別已有不同內容，請重新整理後檢查。"
    default: {
      const status = errorStatus(error)
      return `角色小隊服務暫時無法完成請求${status ? `（HTTP ${status}）` : ""}。請稍後重試；若持續發生，重新整理頁面。`
    }
  }
}
function readErrorTitle(code: string): string {
  switch (code) {
    case "offline": case "timeout": return "目前無法連線至角色小隊"
    case "forbidden": case "unauthorized": return "沒有權限讀取角色小隊"
    case "invalid_response": return "角色小隊回應無法讀取"
    default: return "角色小隊暫時無法讀取"
  }
}

function IconCanvas({ icon, size = 3 }: { icon: Icon; size?: number }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => { L.paintIcon(ref.current, icon, size) }, [icon, size])
  return <canvas ref={ref} className="squad-icon" width={0} height={0} aria-hidden="true" />
}

function valueSource(source: "default" | "global" | "project", project: boolean) {
  return project && source !== "project" ? "繼承「" + sourceLabel(source) + "」" : sourceLabel(source)
}

function skillSourceName(source: string): string {
  const imported = /^imported:(project|claude-code|codex):(folder|text)$/.exec(source)
  if (!imported) return source
  const provider = imported[1] === "project" ? "Project" : imported[1] === "claude-code" ? "Claude Code" : "Codex"
  return `${provider} · ${imported[2] === "folder" ? "完整資料夾" : "SKILL.md 文字"}`
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
      {expanded ? "收合角色定義" : "顯示完整角色定義"}
    </button>}
  </>
}

function PersonaCard({ persona, selected, animated, onPick }: { persona: SquadPersona; selected: boolean; animated: boolean; onPick: () => void }) {
  return <button type="button" className="squad-persona-card" data-selected={selected || undefined} data-use={animated || undefined}
    aria-pressed={selected} onClick={onPick}>
    <span className="squad-card-head"><IconCanvas icon={persona.icon} /><span className="squad-card-names"><strong>{persona.name}</strong><small>{persona.subtitle}</small></span></span>
    <span className="squad-card-summary">{persona.summary}</span>
    <span className="squad-card-foot"><span>{persona.enabled.value ? "● 已啟用" : "○ 已停用"}</span><span>{persona.skills.length} 項技能</span></span>
  </button>
}

function SkillCard({ skill, open, animated, onPick }: { skill: SquadSkill; open: boolean; animated: boolean; onPick: () => void }) {
  return <button type="button" className="squad-skill-card" data-open={open || undefined} data-use={animated || undefined}
    aria-expanded={open} aria-controls="squad-skill-detail" onClick={onPick}>
    <span className="squad-skill-title"><span>{skill.icon && <IconCanvas icon={skill.icon} size={2} />}<strong>{skill.order}. {skill.name}</strong></span><span aria-hidden="true">{open ? "⌄" : "›"}</span></span>
    <span>{skill.purpose}</span>
    <small>{skill.status === "available" ? "已採納" : skill.status === "unavailable" ? "目前不可用" : "待審，尚未安裝"} · {skill.enabled.value ? "已啟用" : "已停用"} · {sourceLabel(skill.enabled.source)}</small>
  </button>
}

function SkillDetail({ skill }: { skill: SquadSkill | null }) {
  return <section className="squad-skill-detail" id="squad-skill-detail" aria-label="技能詳情">
    {skill ? <>
      <h4>{skill.name}</h4>
      <dl className="squad-facts"><div><dt>用途</dt><dd>{skill.purpose}</dd></div><div><dt>來源</dt><dd><LinkedText text={skillSourceName(skill.source)} /></dd></div>
        <div><dt>版本</dt><dd>{skill.version}</dd></div><div><dt>授權</dt><dd>{skill.license || "未提供"}</dd></div>
        <div><dt>狀態</dt><dd>{skill.status === "available" ? "已採納，可供此角色使用" : skill.status === "unavailable" ? "目前不可用" : "待審，尚未安裝或採納"}</dd></div></dl>
      <h5>技能內容</h5><div className="squad-long-text">{skill.body || "此技能尚無可讀內容。"}</div>
      {skill.folder && <><h5>資料夾附檔</h5>{skill.files?.length ? <ul>{skill.files.map((file) => <li key={file.path}>{file.path}</li>)}</ul> : <p>此資料夾只有 SKILL.md。</p>}</>}
    </> : <p>選擇一項技能以閱讀完整內容與來源。</p>}
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
      setNewSkillName(detail.name); setNewSkillPurpose(detail.purpose || "匯入的技能")
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
  const scopeLabel = scope ? data?.project?.name ?? "Project" : "全域"
  const canWrite = reading.kind === "ready" && !!data?.canWrite && !data.partial
  const draftKey = persona ? keyOf(scope, persona.id) : ""
  const draft = drafts[draftKey]
  const handbookText = draft?.text ?? persona?.handbook.value ?? ""

  const chooseScope = (next: string) => {
    if (next === scope) return
    if (Object.values(drafts).some((draft) => draft.scopeId === scope) && !window.confirm("目前範圍有未儲存的手冊文字。切換後草稿仍會保留在原範圍，確定切換？")) return
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
    setBusy("handbook"); setNotice({ text: "正在儲存角色手冊…" })
    try {
      await api.saveHandbook(requestScope, persona.id, text, expectedVersion)
      setDrafts((all) => { const next = { ...all }; delete next[key]; return next })
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "角色手冊已儲存，並已重新讀取生效值。" }) }
    } catch (error) {
      if (errorCode(error) === "version_conflict") {
        try {
          const fresh = await api.read(requestScope)
          if (currentScope.current === requestScope) setReading({ kind: "ready", data: fresh })
          const server = fresh.personas.find((row) => row.id === persona.id)
          if (server) setDrafts((all) => ({ ...all, [key]: { ...all[key], text, serverText: server.handbook.value, serverVersion: server.settingsVersion, conflict: true } }))
          if (currentScope.current === requestScope) setNotice({ text: "手冊已有新版本。你的文字仍在下方，請比較後再決定是否覆寫。", error: true })
        } catch (rereadError) { if (currentScope.current === requestScope) setNotice({ text: `版本衝突且無法重讀：${errorDetail(rereadError)} 你的文字仍保留，請再試一次。`, error: true }) }
      } else if (currentScope.current === requestScope) setNotice({ text: `手冊未儲存：${errorDetail(error)}`, error: true })
    } finally { setBusy("") }
  }

  const restoreHandbook = async () => {
    if (!persona || !scope) return
    if (draft && !window.confirm("還原繼承會放棄這個 Project 的未儲存手冊文字。確定還原？")) return
    const requestScope = scope
    const personaId = persona.id
    setBusy("handbook"); setNotice({ text: "正在還原繼承…" })
    try {
      await api.restoreHandbook(requestScope, personaId, persona.settingsVersion)
      setDrafts((all) => { const next = { ...all }; delete next[keyOf(requestScope, personaId)]; return next })
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "已還原繼承，並已重新讀取生效值。" }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: `未能還原繼承：${errorDetail(error)}`, error: true }) }
    finally { setBusy("") }
  }

  const saveToggle = async (kind: "enabled" | "motion", value: boolean) => {
    if (!data || (kind === "enabled" && !persona)) return
    const requestScope = scope
    const personaId = persona?.id ?? ""
    setBusy(kind); setNotice({ text: "正在儲存設定…" })
    try {
      if (kind === "motion") await api.saveMotion(requestScope, value, data.motionSettingsVersion)
      else await api.saveEnabled(requestScope, personaId, value, persona!.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "設定已儲存，並已重新讀取生效值。" }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: `設定未儲存：${errorDetail(error)}`, error: true }) }
    finally { setBusy("") }
  }

  const restoreToggle = async (kind: "enabled" | "motion") => {
    if (!data || (kind === "enabled" && !persona)) return
    const requestScope = scope
    setBusy(kind); setNotice({ text: "正在還原繼承…" })
    try {
      if (kind === "motion") await api.restoreMotion(requestScope, data.motionSettingsVersion)
      else await api.restoreEnabled(requestScope, persona!.id, persona!.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "已還原繼承，並已重新讀取生效值。" }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: `未能還原繼承：${errorDetail(error)}`, error: true }) }
    finally { setBusy("") }
  }

  const saveSkills = async (choices: { id: string; version: string; enabled: boolean }[]) => {
    if (!data || !persona) return
    const requestScope = scope
    const personaId = persona.id
    setBusy("skills"); setSkillWriteError(null); setNotice({ text: "正在儲存技能設定…" })
    try {
      await api.saveSkills(requestScope, personaId, choices, persona.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "技能設定已儲存，並已重新讀取排序與來源。" }) }
    } catch (error) { if (currentScope.current === requestScope) {
      const text = `技能設定未儲存：${errorDetail(error)}`
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
        setNotice({ text: "已重新讀取技能設定；請確認目前生效值後再操作。" })
        requestAnimationFrame(() => skillCheckbox.current?.focus())
      }
    } catch (error) { setSkillWriteError({ ...skillWriteError, text: `重新讀取失敗：${errorDetail(error)}` }) }
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
      setNotice({ text: pending.catalogSaved ? "技能已存入目錄。若尚未加入角色，可重新開啟「新增技能」並選「加入已建立技能」。" : "正在重新讀取技能目錄。若技能其實已建立，可重新開啟「新增技能」並選「加入已建立技能」。" })
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
      setSkillError(sourceFolderError || "請先從技能清單選擇並讀取一項技能。")
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
      if (!selected) { setSkillError("請選擇要加入的技能；若目錄已更新，請重新讀取後再選。 "); return }
      target = { id: selected.id, version: selected.version, body: selected.body, files: selected.files }
    }
    if (activeSkillBytes(data, persona, target) > ACTIVE_SKILL_BYTES) {
      setSkillError("已啟用技能的內容合計超過安全額度；請先停用其他技能或縮短內容，以免新 Session 無法啟動。")
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
            if (!sameSkill(found, transaction.skill)) throw Object.assign(new Error("同一技能識別已有不同內容，請重新整理後檢查。"), { code: "skill_collision" })
            transaction.catalogSaved = true
          } else {
            transaction.expectedVersion = catalog.catalog_version
            transaction.key = crypto.randomUUID()
            setSkillError("技能目錄已有新版本；你的內容仍保留，請按「重試建立」以新的目錄版本送出。")
            return
          }
        }
      }
      // A settings write replaces the whole list. Read the target scope again
      // on every attempt, including after an uncertain response.
      const fresh = await api.read(originalScope)
      const current = fresh.personas.find((row) => row.id === originalPersona)
      if (!current) throw Object.assign(new Error("角色已不在目錄中。"), { code: "catalog_inconsistent" })
      const latest = fresh.catalogSkills.find((skill) => skill.id === target!.id)
      if (skillMode === "attach" && (!latest || latest.version !== target.version)) {
        if (currentScope.current === originalScope) setReading({ kind: "ready", data: fresh })
        setSkillError("這項技能已有新版本；請重新選擇目前版本，再加入角色。")
        return
      }
      const existing = current.skillsSetting.value.find((choice) => choice.id === target!.id)
      if (existing && (existing.version !== target.version || !existing.enabled)) {
        setSkillError("角色已使用此技能的其他版本或已停用；請先在技能清單檢查，避免覆蓋現有設定。")
        return
      }
      if (!existing) {
        if (activeSkillBytes(fresh, current, target) > ACTIVE_SKILL_BYTES) {
          setSkillError("目前已啟用技能超過安全額度；請先停用其他技能後再加入。")
          return
        }
        await api.saveSkills(originalScope, originalPersona, [...current.skillsSetting.value, { id: target.id, version: target.version, enabled: true }], current.settingsVersion)
      }
      const confirmed = await api.read(originalScope)
      const confirmedPersona = confirmed.personas.find((row) => row.id === originalPersona)
      if (!confirmedPersona?.skillsSetting.value.some((choice) => choice.id === target!.id && choice.version === target!.version && choice.enabled)) {
        setSkillError("技能目錄已建立，但角色設定尚未確認。請按重試；不會重複建立技能。")
        return
      }
      const globalChanged = originalScope && skillGlobalAtOpen.current !== JSON.stringify(confirmedPersona.skillsSetting.global)
      const missingGlobal = globalChanged ? confirmedPersona.skillsSetting.global.filter((choice) =>
        !confirmedPersona.skillsSetting.value.some((local) => local.id === choice.id && local.version === choice.version)) : []
      const missingNames = missingGlobal.map((choice) => confirmed.catalogSkills.find((skill) => skill.id === choice.id && skill.version === choice.version)?.name ?? choice.id)
      if (currentScope.current === originalScope) {
        setReading({ kind: "ready", data: confirmed }); setSkillId(target.id)
        setNotice({ text: globalChanged ? `技能已加入並啟用。儲存期間全域技能清單也更新了；此 Project 尚未加入：${missingNames.join("、") || "請比對全域與 Project 清單"}。可從「加入已建立技能」補入，或還原全域繼承。` : "技能已加入並啟用，並已重新讀取生效設定。" })
      }
      delete skillDrafts.current[`${originalScope}\u0000${originalPersona}`]
      skillTransaction.current = null
      skillDialog.current?.close(); setSkillDialogOpen(false)
    } catch (error) {
      setSkillError(`${skillTransaction.current?.catalogSaved ? "技能目錄已建立，角色尚未確認加入。" : "技能尚未確認建立。"}${errorDetail(error)} 請重試；已輸入內容仍保留。`)
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
    setBusy("skills"); setNotice({ text: "正在還原技能繼承…" })
    try {
      await api.restoreSkills(requestScope, persona.id, persona.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "已還原技能繼承，並已重新讀取生效值。" }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: `未能還原技能繼承：${errorDetail(error)}`, error: true }) }
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
    if (!packFile) { setPackError("請先選擇資料包檔案。"); return }
    setBusy("preview"); setPackError("")
    try {
      const preview = await api.preview(packFile, scope || "global")
      setPackPreview(preview)
      setPackChoices((all) => Object.fromEntries(Object.entries(all).filter(([id]) => preview.conflicts.some((row) => row.id === id))))
      setImportPrivateScopes((all) => all.filter((id) => preview.privateScopes.includes(id)))
      setImportPrivateConfirm(false)
    }
    catch (error) { setPackError(`無法預覽資料包：${errorDetail(error)}`) }
    finally { setBusy("") }
  }
  const adoptPack = async () => {
    if (!packPreview) return
    if (importPrivateScopes.length && !importPrivateConfirm) { setPackError("請確認資料包中的私人設定範圍。"); return }
    setBusy("adopt"); setPackError("")
    try { await api.adopt(packPreview, packChoices, importPrivateScopes); closePack(); await load(scope); setNotice({ text: "資料包已採納，目錄已重新讀取。" }) }
    catch (error) {
      if (["version_conflict", "settings_changed", "preview_expired", "preview_invalid"].includes(errorCode(error))) setPackError("預覽已過期或目錄、私人設定已變更。請重新預覽，舊的採納選擇不會直接送出。")
      else setPackError(`資料包未採納：${errorDetail(error)}`)
      if (["version_conflict", "settings_changed", "preview_expired", "preview_invalid"].includes(errorCode(error))) setPackPreview(null)
    } finally { setBusy("") }
  }
  const exportPack = async () => {
    if ((includeGlobal || includeProjects.length > 0) && !privateConfirm) { setPackError("請先確認將包含的私人設定範圍。"); return }
    setBusy("export"); setPackError("")
    try {
      const { blob, fileName } = await api.exportPackage(includeGlobal, includeProjects)
      const href = URL.createObjectURL(blob)
      const link = document.createElement("a"); link.href = href; link.download = fileName; link.click()
      setTimeout(() => URL.revokeObjectURL(href), 1000)
      closePack(); setNotice({ text: "資料包已下載。" })
    } catch (error) { setPackError(`無法匯出資料包：${errorDetail(error)}`) }
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
      <header className="squad-header"><div><p className="squad-eyebrow">AI SQUAD / 角色小隊</p><h1 id="squad-title" tabIndex={-1}>選擇並了解你的角色小隊</h1>
        <p>查看角色定義、Project 手冊和已採納技能。設定變更只影響之後建立的 Session。</p></div>
        <label className="squad-scope">設定範圍<select value={scope} onChange={(event) => chooseScope(event.target.value)} disabled={!data}>
          <option value="">全域（預設）</option>{data?.projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
        </select></label></header>
      {notice && <p className="squad-notice" role="status" aria-live="polite" data-error={notice.error || undefined}>{notice.text}</p>}
      {reading.kind === "loading" && (data ? <p className="squad-notice" role="status">正在重新讀取角色小隊…</p> : <div className="squad-loading" role="status" aria-live="polite"><div className="squad-loading-rail" /><div className="squad-loading-roster" /><div className="squad-loading-detail" /><span>正在讀取角色小隊…</span></div>)}
      {reading.kind === "error" && <div className="squad-error" role="alert"><h2>{readErrorTitle(reading.code)}</h2><p>{reading.detail}</p><button type="button" onClick={() => void load(scope)}>重試讀取</button></div>}
      {data?.partial && <div className="squad-notice" role="status">部分資料暫時無法讀取。下列內容可能不是最新狀態。<button type="button" onClick={() => void load(scope)}>重試讀取</button></div>}
      {data && !data.canWrite && <p className="squad-notice" role="status">此連線只有讀取權限；設定與私人資料包操作需要可寫入的配對裝置。</p>}
      {data && <div className="squad-layout" data-mobile-view={mobileView}>
        <aside className="squad-rail" aria-labelledby="squad-teams-title"><div className="squad-panel-head"><h2 id="squad-teams-title">小隊</h2><span>{data.teams.length} 組</span></div>
          <div className="squad-teams" role="group" aria-label="小隊篩選"><button type="button" aria-pressed={!team} onClick={() => setTeam("")}>全部角色 <small>{data.personas.length}</small></button>
            {data.teams.map((item) => <button key={item.id} type="button" aria-pressed={team === item.id} onClick={() => setTeam(item.id)}>{item.name}<small>{data.personas.filter((row) => row.teamIds.includes(item.id)).length}</small></button>)}
          </div><p className="squad-scroll-hint">左右捲動查看更多小隊</p><div className="squad-pack-actions"><h3>技能資料包</h3><p>讀入前會預覽來源、授權與衝突；預設匯出不含私人設定。</p>
            <button type="button" onClick={(event) => openPack("import", event.currentTarget)}>讀入資料包</button>
            <button type="button" onClick={(event) => openPack("export", event.currentTarget)}>匯出資料包</button></div></aside>
        <div className="squad-roster" aria-labelledby="squad-roster-title"><div className="squad-panel-head"><div><h2 id="squad-roster-title">角色名冊</h2><p>選一位角色查看完整定義與技能。</p></div><span>{shownPersonas.length} 位角色</span></div>
          <label className="squad-search">搜尋角色<input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="名稱或用途" /></label>
          {shownPersonas.length ? <div className="squad-cards">{shownPersonas.map((row) => <div key={row.id} ref={(node) => { if (node) cardRefs.current.set(row.id, node); else cardRefs.current.delete(row.id) }}>
            <PersonaCard persona={row} selected={selected === row.id} animated={using?.persona === row.id && data.motion.value && !motionReduced} onPick={() => choosePersona(row.id)} />
          </div>)}</div> : <div className="squad-empty"><h3>{data.personas.length ? "沒有符合的角色" : "這個目錄目前沒有角色"}</h3><p>{data.personas.length ? "清除搜尋或選擇其他小隊。" : "讀入資料包後，新角色會顯示在這裡。"}</p>
            {data.personas.length > 0 && <button type="button" onClick={() => { setTeam(""); setSearch("") }}>顯示全部角色</button>}</div>}</div>
        <div className="squad-detail" ref={detailPanel} aria-labelledby="squad-detail-title">{persona ? <>
          <button className="squad-back" type="button" onClick={backToRoster}>← 返回角色名冊</button>
          <div className="squad-profile"><IconCanvas icon={persona.icon} size={5} /><div><p>角色檔案 / 詳情</p><h2 id="squad-detail-title" ref={detailTitle} tabIndex={-1}>{persona.name}</h2><span>{persona.subtitle}</span></div></div>
          <div className="squad-meta"><span>{persona.teamIds.map((id) => data.teams.find((row) => row.id === id)?.name ?? id).join(" · ") || "未分隊"}</span>
            <details className="squad-meta-detail"><summary>來源與版本</summary>
              <dl><div><dt>來源</dt><dd><LinkedText text={persona.source} /></dd></div>
                <div><dt>版本</dt><dd>{persona.version}</dd></div></dl>
              {persona.version.startsWith("sha256:") && <p className="squad-version-explanation">SHA-256 是角色定義內容的指紋；內容變動時會更新，用來辨識使用的版本。</p>}
            </details></div>
          <button className="squad-skill-jump" type="button" onClick={() => { skillHeading.current?.scrollIntoView({ block: "start" }); skillHeading.current?.focus() }}>查看／新增技能</button>
          <section className="squad-detail-block"><h3>角色定義</h3><PersonaDefinition key={persona.id} persona={persona} /></section>
          <section className="squad-detail-block"><div className="squad-setting-heading"><div><h3>允許管理 agent 自動指派</h3><p>停用只影響管理 agent 的自動候選；你仍可手動指定此角色。</p></div>
            <label className="squad-switch"><input type="checkbox" aria-label={`允許管理 agent 自動指派：${persona.enabled.value ? "已啟用" : "已停用"}`} checked={persona.enabled.value} disabled={!canWrite || !!busy} onChange={(event) => void saveToggle("enabled", event.target.checked)} /><span>{persona.enabled.value ? "已啟用" : "已停用"}</span></label></div>
            <p className="squad-source">全域：{persona.enabled.global ? "已啟用" : "已停用"} · 生效：{persona.enabled.value ? "已啟用" : "已停用"} · 來源：{valueSource(persona.enabled.source, !!scope)}</p>
            {hasLocalOverride(persona.enabled.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreToggle("enabled")}>還原{scope ? "全域繼承" : "內建預設"}</button>}</section>
          <section className="squad-detail-block"><h3>角色手冊</h3><p>保存背景知識與固定流程；Project 採整份覆寫。</p>
            {scope && <div className="squad-handbook-global"><h4>全域手冊（唯讀）</h4><div className="squad-long-text">{persona.handbook.global || "目前空白"}</div></div>}
            <label className="squad-handbook-editor">{scope ? `${scopeLabel} 手冊` : "全域手冊"}<textarea value={handbookText} onChange={(event) => editHandbook(event.target.value)} disabled={!canWrite || busy === "handbook"} rows={6} /></label>
            <p className="squad-source">全域：{persona.handbook.global ? "有內容" : "空白"} · 生效：{persona.handbook.value ? "有內容" : "空白"} · 來源：{valueSource(persona.handbook.source, !!scope)}{scope && persona.handbook.source === "project" && !persona.handbook.value ? "（明確覆寫為空白）" : ""}</p>
            {draft?.conflict && <div className="squad-conflict" role="alert"><h4>手冊版本衝突</h4><p>伺服器目前版本 {draft.serverVersion}：</p><div className="squad-long-text">{draft.serverText || "空白"}</div><p>你的未儲存文字仍在編輯區。比較後可用目前版本重新儲存，或放棄草稿。</p>
              <button type="button" disabled={!!busy} onClick={() => void saveHandbook(true)}>確認以目前版本儲存我的文字</button>
              <button type="button" disabled={!!busy} onClick={() => setDrafts((all) => { const next = { ...all }; delete next[draftKey]; return next })}>放棄我的草稿</button></div>}
            <div className="squad-actions"><button type="button" disabled={!canWrite || !!busy || !draft || draft.conflict} onClick={() => void saveHandbook()}>{busy === "handbook" ? "處理中…" : "儲存手冊"}</button>
              {scope && persona.handbook.source === "project" && <button type="button" disabled={!canWrite || !!busy} onClick={() => void restoreHandbook()}>還原繼承</button>}</div></section>
          <section className="squad-detail-block"><div className="squad-panel-head"><h3 ref={skillHeading} tabIndex={-1}>專屬技能</h3><span>{persona.skills.length} 項</span></div><p>已採納的技能依優先順序排列；閱讀詳情不代表 Session 已使用。</p>
            <button ref={skillOpener} className="squad-add-skill" type="button" disabled={!canWrite || !!busy} onClick={openSkillDialog}>新增技能</button>
            {scope && <p className="squad-source">Project 技能清單是整份覆寫；加入後，未來全域新增的技能不會自動出現在此 Project，可還原全域繼承。</p>}
            <p className="squad-source">全域：{persona.skillsSetting.global.length} 項 · 生效：{persona.skillsSetting.value.length} 項 · 來源：{valueSource(persona.skillsSetting.source, !!scope)}</p>
            {persona.skills.length ? <div className="squad-skills">{persona.skills.map((skill) => <SkillCard key={skill.id} skill={skill} open={skill.id === skillId} animated={using?.skill === skill.id && using.persona === persona.id && data.motion.value && !motionReduced} onPick={() => setSkillId(skill.id === skillId ? "" : skill.id)} />)}
              <SkillDetail skill={openSkill} />
              {openSkill && <div className="squad-skill-controls"><label className="squad-check"><input ref={skillCheckbox} type="checkbox" checked={openSkill.enabled.value} disabled={!canWrite || !!busy} onChange={(event) => void saveSkills(currentChoices.map((choice) => choice.id === openSkill.id ? { ...choice, enabled: event.target.checked } : choice))} />此角色可使用此技能</label>
                <div><button type="button" disabled={!canWrite || !!busy || openSkill.order <= 1} onClick={() => moveSkill(openSkill.id, -1)}>上移優先順序</button>
                  <button type="button" disabled={!canWrite || !!busy || openSkill.order >= persona.skills.length} onClick={() => moveSkill(openSkill.id, 1)}>下移優先順序</button></div>
                {skillWriteError?.id === openSkill.id && <div className="squad-skill-write-error" role="alert"><p>{skillWriteError.text}</p><button type="button" disabled={!!busy} onClick={() => void rereadSkill()}>重新讀取技能</button></div>}
                <p className="squad-source">生效：{openSkill.enabled.value ? "啟用" : "停用"} · 來源：{valueSource(openSkill.enabled.source, !!scope)}</p></div>}</div> : <div className="squad-empty"><h4>尚無已採納技能</h4><p>這位角色仍可使用；公開研究候選不會自動安裝。</p></div>}
            {hasLocalOverride(persona.skillsSetting.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreSkills()}>還原技能{scope ? "全域繼承" : "內建預設"}</button>}</section>
          <section className="squad-detail-block"><div className="squad-setting-heading"><div><h3>技能使用動畫</h3><p>只在收到 Session 的新「已套用」收據時顯示；減少動態效果時改為靜態文字。</p></div>
            <label className="squad-switch"><input type="checkbox" aria-label={`技能使用動畫：${data.motion.value ? "開啟" : "關閉"}`} checked={data.motion.value} disabled={!canWrite || !!busy} onChange={(event) => void saveToggle("motion", event.target.checked)} /><span>{data.motion.value ? "開啟" : "關閉"}</span></label></div>
            <p className="squad-source">全域：{data.motion.global ? "開啟" : "關閉"} · 生效：{data.motion.value ? "開啟" : "關閉"} · 來源：{valueSource(data.motion.source, !!scope)}</p>
            {hasLocalOverride(data.motion.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreToggle("motion")}>還原{scope ? "全域繼承" : "內建預設"}</button>}
            {using && using.persona === persona.id && <p className="squad-use" data-animate={data.motion.value && !motionReduced || undefined} role="status">{using.session} 回報使用 {persona.skills.find((row) => row.id === using.skill)?.name ?? "技能"}</p>}</section>
          <p className="squad-privacy">同機已配對讀者可讀取此機器的 Project 手冊；目前沒有逐 Project 讀者隔離。</p>
        </> : <div className="squad-empty"><h2 id="squad-detail-title">選擇角色</h2><p>從名冊選一位角色，即可閱讀定義、手冊和技能。</p></div>}</div>
      </div>}
      <dialog ref={packDialog} className="squad-dialog" aria-labelledby="squad-dialog-title" onClose={() => { setPack(null); setPackPreview(null); setPackFile(null); packOpener.current?.focus() }}>
        {pack && <><div className="squad-dialog-head"><h2 id="squad-dialog-title">{pack === "import" ? "讀入角色小隊資料包" : "匯出角色小隊資料包"}</h2><button type="button" aria-label="關閉資料包對話框" onClick={closePack}>×</button></div>
          {pack === "import" ? <><p>讀入前先預覽新增、更新、衝突及受影響的角色與技能。來源與授權資訊仍須人工審查。目標範圍：{scopeLabel}。</p>
            <label className="squad-file">選擇本機 ZIP 檔<input type="file" accept=".zip,application/zip" onChange={(event) => { setPackFile(event.target.files?.[0] ?? null); setPackPreview(null); setImportPrivateScopes([]); setImportPrivateConfirm(false) }} /></label>
            <button type="button" disabled={!!busy} onClick={() => void previewPack()}>{busy === "preview" ? "驗證中…" : "驗證並預覽"}</button>
            {packPreview && <div className="squad-pack-preview"><h3>資料包預覽</h3><p>目錄版本 {packPreview.catalogVersion} · 摘要 {packPreview.digest}</p><p>來源：{packPreview.source || "未提供"}（待審） · 授權：{packPreview.license || "未提供"}（待審）</p>
              <h4>新增</h4><ul>{packPreview.additions.length ? packPreview.additions.map((item) => <li key={item}>{item}</li>) : <li>無</li>}</ul>
              <h4>更新</h4><ul>{packPreview.updates.length ? packPreview.updates.map((item) => <li key={item}>{item}</li>) : <li>無</li>}</ul>
              <h4>依賴影響</h4><ul>{packPreview.dependencies.length ? packPreview.dependencies.map((item) => <li key={item}>{item}</li>) : <li>無</li>}</ul>
              {packPreview.privateScopes.length > 0 && <><h4>包內私人設定（預設不採納）</h4><p>逐一選擇要採納的範圍；同機已配對讀者具有機器範圍讀取權。</p>
                {packPreview.privateScopes.map((id) => <label className="squad-check" key={id}><input type="checkbox" checked={importPrivateScopes.includes(id)} onChange={(event) => { setImportPrivateScopes((all) => event.target.checked ? [...all, id] : all.filter((row) => row !== id)); setImportPrivateConfirm(false) }} />採納 {id} 私人設定</label>)}
                {importPrivateScopes.length > 0 && <label className="squad-check"><input type="checkbox" checked={importPrivateConfirm} onChange={(event) => setImportPrivateConfirm(event.target.checked)} />我確認採納上述私人設定</label>}</>}
              {packPreview.conflicts.length > 0 && <><h4>衝突</h4>{packPreview.conflicts.map((conflict) => <label className="squad-conflict-choice" key={`${conflict.kind}:${conflict.id}`}>{conflict.kind} · {conflict.id}：{conflict.reason}
                <select value={packChoices[conflict.id] ?? ""} onChange={(event) => setPackChoices((all) => ({ ...all, [conflict.id]: event.target.value }))}><option value="">取消此次採納</option><option value="keep">保留現有並採納其餘項目</option></select></label>)}</>}
              <button type="button" disabled={!canWrite || !!busy || importPrivateScopes.length > 0 && !importPrivateConfirm || packPreview.conflicts.some((row) => !packChoices[row.id])} onClick={() => void adoptPack()}>{busy === "adopt" ? "採納中…" : "確認採納資料包"}</button></div>}</>
            : <><p>預設只匯出可分享的定義。全域與每個 Project 的私人手冊及覆寫必須分別勾選；同機已配對讀者具有機器範圍讀取權。</p>
              <label className="squad-check"><input type="checkbox" checked={includeGlobal} onChange={(event) => { setIncludeGlobal(event.target.checked); setPrivateConfirm(false) }} />包含全域私人設定</label>
              {data?.projects.map((project) => <label className="squad-check" key={project.id}><input type="checkbox" checked={includeProjects.includes(project.id)} onChange={(event) => { setIncludeProjects((rows) => event.target.checked ? [...rows, project.id] : rows.filter((id) => id !== project.id)); setPrivateConfirm(false) }} />包含 {project.name} 的私人設定</label>)}
          <div className="squad-export-summary"><strong>即將下載：</strong>可分享定義（包含自寫技能全文）{includeGlobal ? "、全域私人設定" : ""}{includeProjects.map((id) => `、${data?.projects.find((project) => project.id === id)?.name ?? id} 私人設定`).join("")}</div>
              {data?.catalogSkills.some((skill) => skill.folder) && <p role="alert">目錄含有完整技能資料夾。現有資料包格式無法保存附檔，因此暫時無法匯出。</p>}
              {(includeGlobal || includeProjects.length > 0) && <label className="squad-check"><input type="checkbox" checked={privateConfirm} onChange={(event) => setPrivateConfirm(event.target.checked)} />我確認將上述私人設定放入下載檔</label>}
              <button type="button" disabled={!!busy || !!data?.catalogSkills.some((skill) => skill.folder) || !canWrite && (includeGlobal || includeProjects.length > 0)} onClick={() => void exportPack()}>{busy === "export" ? "匯出中…" : "下載資料包"}</button></>}
          {packError && <p className="squad-dialog-error" role="alert">{packError}</p>}</>}
      </dialog>
      <dialog ref={skillDialog} className="squad-dialog squad-skill-dialog" aria-labelledby="squad-skill-dialog-title" aria-describedby="squad-skill-disclosure" onCancel={(event) => { event.preventDefault(); if (skillCloseConfirm) setSkillCloseConfirm(false); else closeSkillDialog() }} onClose={() => { recoverSavedSkill(); skillOpener.current?.focus() }}>
        <div className="squad-dialog-head"><h2 id="squad-skill-dialog-title">為角色新增技能</h2><button type="button" aria-label="關閉新增技能對話框" disabled={skillBusy || skillCloseConfirm} onClick={closeSkillDialog}>×</button></div>
        <p>角色：{persona?.name ?? ""} · 範圍：{scopeLabel}。加入後只影響之後建立的 Session。</p>
        <p className="squad-skill-disclosure" id="squad-skill-disclosure">技能全文存於這部機器的全域目錄，同機已配對讀者可讀；純文字技能會包含在預設可分享資料包中。所選範圍只控制此角色能否使用。</p>
        {skillCloseConfirm ? <div className="squad-skill-confirm" role="group" aria-label="關閉技能草稿"><h3>要保留未送出的技能草稿嗎？</h3><p>保留後，可在這位角色的同一範圍重新開啟「新增技能」繼續編輯。</p>
          <div className="squad-actions"><button ref={skillKeepButton} type="button" onClick={() => closeSkillDraft(true)}>保留草稿並關閉</button><button type="button" onClick={() => closeSkillDraft(false)}>捨棄草稿</button><button type="button" onClick={() => setSkillCloseConfirm(false)}>繼續編輯</button></div></div> :
        <form onSubmit={(event) => { event.preventDefault(); void addSkill() }}>
        <div className="squad-skill-mode" role="group" aria-label="新增技能方式">
          <label className="squad-check"><input type="radio" name="squad-skill-mode" checked={skillMode === "create"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { if (skillMode === "import") { setNewSkillName(""); setNewSkillPurpose(""); setNewSkillContent(""); setImportAttachedFiles([]) } setSkillMode("create"); setSkillError("") }} />建立新技能</label>
          <label className="squad-check"><input type="radio" name="squad-skill-mode" checked={skillMode === "attach"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setSkillMode("attach"); setSkillError("") }} />加入已建立技能</label>
          <label className="squad-check"><input type="radio" name="squad-skill-mode" checked={skillMode === "import"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setSkillMode("import"); setNewSkillName(""); setNewSkillPurpose(""); setNewSkillContent(""); setImportAttachedFiles([]); setSkillError("") }} />匯入 Project／Claude Code／Codex 技能</label>
        </div>
        {skillMode === "create" ? <div className="squad-skill-form">
          <label>技能名稱<input ref={skillNameInput} required value={newSkillName} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillName(event.target.value)} /></label>
          <label>用途摘要<input required value={newSkillPurpose} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillPurpose(event.target.value)} /></label>
          <p id="squad-skill-content-hint">建立後預設啟用，可回到技能詳情停用。內容最多 64 KiB。</p>
          <label>技能內容<textarea required rows={8} aria-describedby="squad-skill-content-hint" value={newSkillContent} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillContent(event.target.value)} /></label>
        </div> : skillMode === "attach" ? <div className="squad-skill-form"><label>選擇技能<select value={existingSkillId} disabled={skillBusy} onChange={(event) => { setExistingSkillId(event.target.value); setSkillError("") }}>
          <option value="">請選擇</option>{data?.catalogSkills.filter((skill) => !persona?.skills.some((row) => row.id === skill.id)).map((skill) => <option key={skill.id} value={skill.id}>{skill.name} · {skillSourceName(skill.source)} · 版本 {skill.version}</option>)}
        </select></label><p>可選擇內建或先前建立的技能。已加入角色的技能可在清單中啟用、停用與排序。</p></div> :
        <div className="squad-skill-form">
          <label>技能來源<select value={importSource} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setImportSource(event.target.value as typeof importSource)}><option value="project">Project</option><option value="claude-code">Claude Code</option><option value="codex">Codex</option></select></label>
          <div className="squad-skill-mode" role="group" aria-label="匯入方式">
            <label className="squad-check"><input type="radio" name="squad-import-kind" checked={importKind === "folder"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setImportKind("folder"); setNewSkillContent(""); setImportAttachedFiles([]); setSkillError("") }} />完整技能資料夾</label>
            <label className="squad-check"><input type="radio" name="squad-import-kind" checked={importKind === "text"} disabled={skillBusy || !!skillTransaction.current} onChange={() => { setImportKind("text"); setNewSkillContent(""); setImportAttachedFiles([]); setSkillError("") }} />只複製 SKILL.md 文字</label>
          </div>
          <p>從這部機器的技能目錄選擇。{importSource === "project" ? "會列出目前選取 Project 的技能。" : `會列出${importSource === "codex" ? " Codex" : " Claude Code"} 的個人、擴充與目前 Project 技能。`}完整資料夾會保存 SKILL.md 與附檔；文字方式只保存 SKILL.md。合計最多 64 KiB。Cloud 介面會經配對連線讀取清單，並只在選取技能後讀取其內容與附檔。</p>
          <div className="squad-source-list" aria-label="可加入的技能">
            <div className="squad-source-list-head"><strong>可用技能</strong><button type="button" disabled={sourceLoading} onClick={() => setSourceReload((value) => value + 1)}>重新讀取</button></div>
            {sourceRows.length > 0 && <label>搜尋技能<input type="search" value={sourceSearch} onChange={(event) => setSourceSearch(event.target.value)} placeholder="名稱、用途或位置" /></label>}
            {sourceLoading ? <p role="status">正在讀取技能清單…</p> : sourceError ? <p role="alert">{sourceError}</p> : sourceRows.length === 0 ? <p role="status">這個來源沒有找到技能。請確認已選擇正確的 Project，或改選其他來源。</p> :
              <div className="squad-source-options" role="group" aria-label="選擇現有技能">{sourceRows.filter((row) => `${row.name} ${row.purpose} ${row.location}`.toLocaleLowerCase().includes(sourceSearch.toLocaleLowerCase())).map((row) =>
                <button key={row.id} type="button" className="squad-source-option" aria-pressed={selectedSourceID === row.id} disabled={skillBusy || !!skillTransaction.current} onClick={() => setSelectedSourceID(row.id)}>
                  <strong>{row.name}</strong><span>{row.purpose || "未提供用途"}</span><small>{row.location}</small>
                </button>)}{sourceRows.length > 0 && !sourceRows.some((row) => `${row.name} ${row.purpose} ${row.location}`.toLocaleLowerCase().includes(sourceSearch.toLocaleLowerCase())) && <p>沒有符合搜尋的技能。可清除搜尋再選。</p>}</div>}
          </div>
          {sourceDetailLoading && <p role="status">正在讀取所選技能…</p>}
          {sourceFolderError && <p role="alert">{sourceFolderError}</p>}
          {newSkillContent && <><label>技能名稱<input required value={newSkillName} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillName(event.target.value)} /></label>
            <label>用途摘要<input required value={newSkillPurpose} disabled={skillBusy || !!skillTransaction.current} onChange={(event) => setNewSkillPurpose(event.target.value)} /></label>
            <p role="status">已讀取 SKILL.md{importKind === "folder" && !sourceFolderError ? `，另含 ${importAttachedFiles.length} 個附檔` : ""}。請確認名稱與用途後加入。</p>
            <details><summary>預覽 SKILL.md</summary><pre className="squad-long-text">{newSkillContent}</pre></details></>}
        </div>}
        {skillTransaction.current?.catalogSaved && <p className="squad-skill-stage" role="status">技能已建立在目錄中，正在加入角色；重試不會重複建立。</p>}
        {skillError && <p className="squad-dialog-error" role="alert">{skillError}</p>}
        <div className="squad-actions"><button type="submit" disabled={skillBusy || !canWrite || (skillMode === "import" && (sourceDetailLoading || !!sourceFolderError || !selectedSourceID || !newSkillContent))}>{skillBusy ? "處理中…" : skillTransaction.current ? skillTransaction.current.catalogSaved ? "重試加入技能" : "重試建立" : skillMode === "create" ? "建立並啟用" : "加入並啟用"}</button>
          <button type="button" disabled={skillBusy} onClick={closeSkillDialog}>取消</button></div>
        </form>}
      </dialog>
    </div>
  </section>
}

export const page: PageModule = { id: "squad", Component: SquadPageView }
