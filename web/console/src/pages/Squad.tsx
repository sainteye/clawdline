import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { Icon } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import type { PageModule } from "./types.js"
import { squadApi, type PackPreview, type SquadAPI } from "./squad/api.js"
import { ReceiptGate, sourceLabel, visiblePersonas, type SquadDraft, type SquadPersona, type SquadReadState, type SquadSkill, type SquadView } from "./squad/model.js"
import "./squad/squad.css"

const keyOf = (scope: string, persona: string) => `${scope}\u0000${persona}\u0000handbook`
const errorCode = (error: unknown) => typeof (error as { code?: unknown })?.code === "string" ? (error as { code: string }).code : "read_failed"
const errorDetail = (error: unknown) => error instanceof Error ? error.message : "請重試。"

function IconCanvas({ icon, size = 3 }: { icon: Icon; size?: number }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => { L.paintIcon(ref.current, icon, size) }, [icon, size])
  return <canvas ref={ref} className="squad-icon" width={0} height={0} aria-hidden="true" />
}

function valueSource(source: "default" | "global" | "project", project: boolean) {
  return project && source !== "project" ? "繼承「" + sourceLabel(source) + "」" : sourceLabel(source)
}

function hasLocalOverride(source: "default" | "global" | "project", project: boolean): boolean {
  return source === (project ? "project" : "global")
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
      <dl className="squad-facts"><div><dt>用途</dt><dd>{skill.purpose}</dd></div><div><dt>來源</dt><dd>{skill.source}</dd></div>
        <div><dt>版本</dt><dd>{skill.version}</dd></div><div><dt>授權</dt><dd>{skill.license || "未提供"}</dd></div>
        <div><dt>狀態</dt><dd>{skill.status === "available" ? "已採納，可供此角色使用" : skill.status === "unavailable" ? "目前不可用" : "待審，尚未安裝或採納"}</dd></div></dl>
      <h5>技能內容</h5><div className="squad-long-text">{skill.body || "此技能尚無可讀內容。"}</div>
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
    if (!shown && wasShown.current) { request.current++; if (useTimer.current) clearTimeout(useTimer.current); useQueue.current = []; usePlaying.current = false; packDialog.current?.close(); setPack(null); setPackPreview(null); setPackFile(null); setUsing(null) }
    wasShown.current = shown
  }, [shown, load])

  useEffect(() => {
    const media = matchMedia("(prefers-reduced-motion: reduce)")
    const changed = () => setMotionReduced(media.matches)
    media.addEventListener("change", changed)
    return () => media.removeEventListener("change", changed)
  }, [])

  const data = reading.kind === "ready" ? reading.data : reading.previous
  dataRef.current = data ?? null
  const shownPersonas = useMemo(() => data ? visiblePersonas(data, team, search) : [], [data, team, search])
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
        } catch { setNotice({ text: "版本衝突且無法重讀。你的文字仍保留，請再試一次。", error: true }) }
      } else setNotice({ text: `手冊未儲存：${errorDetail(error)}`, error: true })
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
    } catch (error) { setNotice({ text: `未能還原繼承：${errorDetail(error)}`, error: true }) }
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
    } catch (error) { setNotice({ text: `設定未儲存：${errorDetail(error)}`, error: true }) }
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
    setBusy("skills"); setNotice({ text: "正在儲存技能設定…" })
    try {
      await api.saveSkills(requestScope, personaId, choices, persona.settingsVersion)
      if (currentScope.current === requestScope) { await load(requestScope); setNotice({ text: "技能設定已儲存，並已重新讀取排序與來源。" }) }
    } catch (error) { if (currentScope.current === requestScope) setNotice({ text: `技能設定未儲存：${errorDetail(error)}`, error: true }) }
    finally { setBusy("") }
  }
  const currentChoices = persona?.skills.map((skill) => ({ id: skill.id, version: skill.version, enabled: skill.enabled.value })) ?? []
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
        const current = dataRef.current
        if (!current) return
        const sessions = page.events.length ? await api.boundSessions().catch(() => []) : current.sessions
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
      {reading.kind === "error" && <div className="squad-error" role="alert"><h2>{reading.code === "forbidden" ? "沒有權限讀取角色小隊" : "無法讀取角色小隊"}</h2><p>{reading.detail}</p><button type="button" onClick={() => void load(scope)}>重試讀取</button></div>}
      {data?.partial && <p className="squad-notice" role="status">部分資料暫時無法讀取。下列內容可能不是最新狀態，請重試讀取後再變更設定。</p>}
      {data && !data.canWrite && <p className="squad-notice" role="status">此連線只有讀取權限；設定與私人資料包操作需要可寫入的配對裝置。</p>}
      {data && <div className="squad-layout" data-mobile-view={mobileView}>
        <aside className="squad-rail" aria-labelledby="squad-teams-title"><div className="squad-panel-head"><h2 id="squad-teams-title">小隊</h2><span>{data.teams.length} 組</span></div>
          <div className="squad-teams" role="group" aria-label="小隊篩選"><button type="button" aria-pressed={!team} onClick={() => setTeam("")}>全部角色 <small>{data.personas.length}</small></button>
            {data.teams.map((item) => <button key={item.id} type="button" aria-pressed={team === item.id} onClick={() => setTeam(item.id)}>{item.name}<small>{data.personas.filter((row) => row.teamIds.includes(item.id)).length}</small></button>)}
          </div><div className="squad-pack-actions"><h3>技能資料包</h3><p>讀入前會預覽來源、授權與衝突；預設匯出不含私人設定。</p>
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
          <div className="squad-meta"><span>{persona.teamIds.map((id) => data.teams.find((row) => row.id === id)?.name ?? id).join(" · ") || "未分隊"}</span><span>{persona.source}</span><span>版本 {persona.version}</span></div>
          <section className="squad-detail-block"><h3>角色定義</h3><div className="squad-long-text">{persona.body}</div></section>
          <section className="squad-detail-block"><div className="squad-setting-heading"><div><h3>允許管理 agent 自動指派</h3><p>停用只影響管理 agent 的自動候選；你仍可手動指定此角色。</p></div>
            <label className="squad-switch"><input type="checkbox" checked={persona.enabled.value} disabled={!canWrite || !!busy} onChange={(event) => void saveToggle("enabled", event.target.checked)} /><span>{persona.enabled.value ? "已啟用" : "已停用"}</span></label></div>
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
          <section className="squad-detail-block"><div className="squad-panel-head"><h3>專屬技能</h3><span>{persona.skills.length} 項</span></div><p>已採納的技能依優先順序排列；閱讀詳情不代表 Session 已使用。</p>
            <p className="squad-source">全域：{persona.skillsSetting.global.length} 項 · 生效：{persona.skillsSetting.value.length} 項 · 來源：{valueSource(persona.skillsSetting.source, !!scope)}</p>
            {persona.skills.length ? <div className="squad-skills">{persona.skills.map((skill) => <SkillCard key={skill.id} skill={skill} open={skill.id === skillId} animated={using?.skill === skill.id && using.persona === persona.id && data.motion.value && !motionReduced} onPick={() => setSkillId(skill.id === skillId ? "" : skill.id)} />)}
              <SkillDetail skill={openSkill} />
              {openSkill && <div className="squad-skill-controls"><label className="squad-check"><input type="checkbox" checked={openSkill.enabled.value} disabled={!canWrite || !!busy} onChange={(event) => void saveSkills(currentChoices.map((choice) => choice.id === openSkill.id ? { ...choice, enabled: event.target.checked } : choice))} />此角色可使用此技能</label>
                <div><button type="button" disabled={!canWrite || !!busy || openSkill.order <= 1} onClick={() => moveSkill(openSkill.id, -1)}>上移優先順序</button>
                  <button type="button" disabled={!canWrite || !!busy || openSkill.order >= persona.skills.length} onClick={() => moveSkill(openSkill.id, 1)}>下移優先順序</button></div>
                <p className="squad-source">生效：{openSkill.enabled.value ? "啟用" : "停用"} · 來源：{valueSource(openSkill.enabled.source, !!scope)}</p></div>}</div> : <div className="squad-empty"><h4>尚無已採納技能</h4><p>這位角色仍可使用；公開研究候選不會自動安裝。</p></div>}
            {hasLocalOverride(persona.skillsSetting.source, !!scope) && <button className="squad-restore" type="button" disabled={!canWrite || !!busy} onClick={() => void restoreSkills()}>還原技能{scope ? "全域繼承" : "內建預設"}</button>}</section>
          <section className="squad-detail-block"><div className="squad-setting-heading"><div><h3>技能使用動畫</h3><p>只在收到 Session 的新「已套用」收據時顯示；減少動態效果時改為靜態文字。</p></div>
            <label className="squad-switch"><input type="checkbox" checked={data.motion.value} disabled={!canWrite || !!busy} onChange={(event) => void saveToggle("motion", event.target.checked)} /><span>{data.motion.value ? "開啟" : "關閉"}</span></label></div>
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
              {packPreview.conflicts.length > 0 && <><h4>衝突</h4>{packPreview.conflicts.map((conflict) => <label className="squad-conflict-choice" key={conflict.id}>{conflict.id}：{conflict.reason}
                <select value={packChoices[conflict.id] ?? ""} onChange={(event) => setPackChoices((all) => ({ ...all, [conflict.id]: event.target.value }))}><option value="">取消此次採納</option><option value="keep">保留現有並採納其餘項目</option></select></label>)}</>}
              <button type="button" disabled={!canWrite || !!busy || importPrivateScopes.length > 0 && !importPrivateConfirm || packPreview.conflicts.some((row) => !packChoices[row.id])} onClick={() => void adoptPack()}>{busy === "adopt" ? "採納中…" : "確認採納資料包"}</button></div>}</>
            : <><p>預設只匯出可分享的定義。全域與每個 Project 的私人手冊及覆寫必須分別勾選；同機已配對讀者具有機器範圍讀取權。</p>
              <label className="squad-check"><input type="checkbox" checked={includeGlobal} onChange={(event) => { setIncludeGlobal(event.target.checked); setPrivateConfirm(false) }} />包含全域私人設定</label>
              {data?.projects.map((project) => <label className="squad-check" key={project.id}><input type="checkbox" checked={includeProjects.includes(project.id)} onChange={(event) => { setIncludeProjects((rows) => event.target.checked ? [...rows, project.id] : rows.filter((id) => id !== project.id)); setPrivateConfirm(false) }} />包含 {project.name} 的私人設定</label>)}
              <div className="squad-export-summary"><strong>即將下載：</strong>可分享定義{includeGlobal ? "、全域私人設定" : ""}{includeProjects.map((id) => `、${data?.projects.find((project) => project.id === id)?.name ?? id} 私人設定`).join("")}</div>
              {(includeGlobal || includeProjects.length > 0) && <label className="squad-check"><input type="checkbox" checked={privateConfirm} onChange={(event) => setPrivateConfirm(event.target.checked)} />我確認將上述私人設定放入下載檔</label>}
              <button type="button" disabled={!!busy || !canWrite && (includeGlobal || includeProjects.length > 0)} onClick={() => void exportPack()}>{busy === "export" ? "匯出中…" : "下載資料包"}</button></>}
          {packError && <p className="squad-dialog-error" role="alert">{packError}</p>}</>}
      </dialog>
    </div>
  </section>
}

export const page: PageModule = { id: "squad", Component: SquadPageView }
