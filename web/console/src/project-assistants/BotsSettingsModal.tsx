import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { catalogWord } from "../catalog.js"
import { useModalBackdrop } from "../modal-backdrop.js"
import { PaSelect } from "./PaSelect"
import "./bots-settings.css"

// Machine-wide Bots settings: dispatch rules and memory shared by every Bot.
// The daemon document is the source of truth; this modal edits a copy and
// writes the whole document with the version it read.

export type SharedScope = { all: boolean; projects?: string[] }
export type RuleAgent = { agent: string; model?: string; effort?: string }
export type DispatchRule = { id: string; kind: string; hint?: string; agents: RuleAgent[]; scope: SharedScope }
export type SharedMemory = { id: string; text: string; scope: SharedScope }
export type SharedSettings = { version: number; rules: DispatchRule[]; memory: SharedMemory[]; memory_opt_out?: string[]; project_overrides?: unknown }
export type AgentOption = { agent: string; efforts: string[] }
export type SharedSettingsReply = { settings: SharedSettings; agents: AgentOption[]; limits: Record<string, number> }
export type ProjectChoice = { id: string; name: string }

export const sharedSettingsPath = "/v1/project-assistants/shared-settings"

const word = (key: string) => catalogWord("botsSettings", key)
const agentName = (agent: string) => agent === "claude" ? "Claude Code" : agent === "codex" ? "Codex" : agent === "antigravity" ? "Gemini" : agent === "grok" ? "Grok" : agent
const newID = () => Math.random().toString(36).slice(2, 12)

export async function readSharedSettings(fetcher: typeof fetch = fetch): Promise<SharedSettingsReply> {
  const response = await fetcher(sharedSettingsPath, { headers: { Accept: "application/json" } })
  if (!response.ok) throw new Error(String(response.status))
  return response.json() as Promise<SharedSettingsReply>
}

export async function writeSharedSettings(settings: SharedSettings, fetcher: typeof fetch = fetch): Promise<SharedSettingsReply> {
  const { version, ...rest } = settings
  const response = await fetcher(sharedSettingsPath, { method: "PUT", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ expected_version: version, settings: { ...rest, version } }) })
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { code?: string; field?: string }
    throw new Error(body.code === "pa_stale_version" ? word("stale") : body.code === "pa_capacity_full" ? word("full") : word("saveFailed"))
  }
  return response.json() as Promise<SharedSettingsReply>
}

/** Tab and Shift+Tab stay inside the dialog while it is open. */
export function trapFocus(event: ReactKeyboardEvent<HTMLElement> | KeyboardEvent, container: HTMLElement | null) {
  if (event.key !== "Tab" || !container) return
  const items = [...container.querySelectorAll<HTMLElement>("button:not([disabled]),input:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex='-1'])")].filter(e => !e.hasAttribute("hidden"))
  if (!items.length) return
  const first = items[0], last = items[items.length - 1]
  const active = container.ownerDocument.activeElement
  if (event.shiftKey && (active === first || !container.contains(active))) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (active === last || !container.contains(active))) { event.preventDefault(); first.focus() }
}

function Chevron() {
  return <svg className="pa-bs-chevron" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 6l4 4 4-4"/></svg>
}

function CloseIcon() {
  return <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M4 4l8 8M12 4L4 12"/></svg>
}

export function scopeLabel(scope: SharedScope, projects: readonly ProjectChoice[]): string {
  if (scope.all) return word("allProjects")
  const names = (scope.projects ?? []).map(id => projects.find(p => p.id === id)?.name ?? id)
  return names.length ? names.join("、") : word("noProjects")
}

/** A checkbox popover: All Projects (including future ones) or chosen Projects. */
export function ScopePicker({ scope, projects, onChange }: { scope: SharedScope; projects: readonly ProjectChoice[]; onChange: (scope: SharedScope) => void }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const away = (e: PointerEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false) }
    const keys = (e: KeyboardEvent) => { if (e.key === "Escape") { e.stopPropagation(); setOpen(false) } }
    document.addEventListener("pointerdown", away)
    root.current?.addEventListener("keydown", keys)
    const node = root.current
    return () => { document.removeEventListener("pointerdown", away); node?.removeEventListener("keydown", keys) }
  }, [open])
  const chosen = scope.projects ?? []
  const toggle = (id: string) => {
    const next = chosen.includes(id) ? chosen.filter(p => p !== id) : [...chosen, id]
    onChange({ all: false, projects: next })
  }
  return <div className="pa-bs-scope" ref={root}>
    <button type="button" className="pa-bs-scope-button" aria-haspopup="true" aria-expanded={open} aria-label={`${word("appliesTo")}: ${scopeLabel(scope, projects)}`} onClick={() => setOpen(!open)}>
      <span className="pa-bs-scope-value">{scopeLabel(scope, projects)}</span><Chevron/>
    </button>
    {open && <div className="pa-bs-scope-pop" role="group" aria-label={word("appliesTo")}>
      <label><input type="checkbox" checked={scope.all} onChange={() => onChange(scope.all ? { all: false, projects: [] } : { all: true })}/><span>{word("allProjectsFuture")}</span></label>
      {projects.map(p => <label key={p.id}><input type="checkbox" disabled={scope.all} checked={!scope.all && chosen.includes(p.id)} onChange={() => toggle(p.id)}/><span>{p.name}</span></label>)}
    </div>}
  </div>
}

function RuleEditor({ rule, agents, projects, onChange, onDelete }: { rule: DispatchRule; agents: readonly AgentOption[]; projects: readonly ProjectChoice[]; onChange: (rule: DispatchRule) => void; onDelete: () => void }) {
  const known = agents.map(a => a.agent)
  const agentOptions = (current: string) => [...agents.map(a => ({ value: a.agent, label: agentName(a.agent) })), ...(known.includes(current) ? [] : [{ value: current, label: `${agentName(current)} · ${word("unavailable")}` }])]
  const setAgent = (index: number, next: RuleAgent) => onChange({ ...rule, agents: rule.agents.map((a, i) => i === index ? next : a) })
  return <li className="pa-bs-rule">
    <div className="pa-bs-rule-head">
      <input className="pa-bs-input" aria-label={word("kind")} placeholder={word("kindPlaceholder")} value={rule.kind} maxLength={64} onChange={e => onChange({ ...rule, kind: e.target.value })}/>
      <ScopePicker scope={rule.scope} projects={projects} onChange={scope => onChange({ ...rule, scope })}/>
      <button type="button" className="pa-bs-icon" aria-label={word("deleteRule")} title={word("deleteRule")} onClick={onDelete}><CloseIcon/></button>
    </div>
    <input className="pa-bs-input pa-bs-hint" aria-label={word("hint")} placeholder={word("hintPlaceholder")} value={rule.hint ?? ""} maxLength={300} onChange={e => onChange({ ...rule, hint: e.target.value })}/>
    <ol className="pa-bs-agents" aria-label={word("agentsInOrder")}>
      {rule.agents.map((a, i) => {
        const efforts = agents.find(o => o.agent === a.agent)?.efforts ?? []
        return <li key={i} className="pa-bs-agent">
          <span className="pa-bs-order" aria-hidden="true">{i + 1}</span>
          <PaSelect compact ariaLabel={word("agent")} value={a.agent} options={agentOptions(a.agent)} onChange={agent => setAgent(i, { agent })}/>
          <input className="pa-bs-input pa-bs-model" aria-label={word("model")} placeholder={word("modelDefault")} value={a.model ?? ""} maxLength={64} onChange={e => setAgent(i, { ...a, model: e.target.value.trim() || undefined })}/>
          {efforts.length > 0 && <PaSelect compact ariaLabel={word("effort")} value={a.effort ?? ""} options={[{ value: "", label: word("effortDefault") }, ...efforts.map(e => ({ value: e, label: e }))]} onChange={effort => setAgent(i, { ...a, effort: effort || undefined })}/>}
          <button type="button" className="pa-bs-icon" aria-label={word("removeAgent")} title={word("removeAgent")} disabled={rule.agents.length < 2} onClick={() => onChange({ ...rule, agents: rule.agents.filter((_, j) => j !== i) })}><CloseIcon/></button>
        </li>
      })}
    </ol>
    {rule.agents.length < 4 && agents.length > 0 && <button type="button" className="pa-bs-link" onClick={() => onChange({ ...rule, agents: [...rule.agents, { agent: agents[0].agent }] })}>{word("addFallback")}</button>}
  </li>
}

function MemoryRow({ entry, projects, onChange, onDelete }: { entry: SharedMemory; projects: readonly ProjectChoice[]; onChange: (entry: SharedMemory) => void; onDelete: () => void }) {
  return <li className="pa-bs-memory">
    <textarea className="pa-bs-input" aria-label={word("memoryText")} value={entry.text} maxLength={1024} rows={2} onChange={e => onChange({ ...entry, text: e.target.value })}/>
    <div className="pa-bs-memory-foot">
      <ScopePicker scope={entry.scope} projects={projects} onChange={scope => onChange({ ...entry, scope })}/>
      <button type="button" className="pa-bs-icon" aria-label={word("deleteMemory")} title={word("deleteMemory")} onClick={onDelete}><CloseIcon/></button>
    </div>
  </li>
}

export function BotsSettingsModal({ projects, onClose, fetcher = fetch }: { projects: readonly ProjectChoice[]; onClose: () => void; fetcher?: typeof fetch }) {
  const panel = useRef<HTMLElement>(null)
  const opener = useRef<Element | null>(typeof document === "undefined" ? null : document.activeElement)
  const [reply, setReply] = useState<SharedSettingsReply>()
  const [draft, setDraft] = useState<SharedSettings>()
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)
  const close = () => { onClose(); (opener.current as HTMLElement | null)?.focus?.() }
  const backdrop = useModalBackdrop(close)
  useEffect(() => {
    let live = true
    readSharedSettings(fetcher).then(r => { if (live) { setReply(r); setDraft(r.settings) } }, () => { if (live) setError(word("loadFailed")) })
    return () => { live = false }
  }, [fetcher])
  useEffect(() => { panel.current?.querySelector<HTMLElement>(".pa-bs-close")?.focus() }, [])
  const save = async () => {
    if (!draft) return
    setSaving(true); setError("")
    try {
      const clean = { ...draft, rules: draft.rules.filter(r => r.kind.trim()), memory: draft.memory.filter(m => m.text.trim()) }
      const next = await writeSharedSettings(clean, fetcher)
      setReply(next); setDraft(next.settings)
    } catch (e) { setError(e instanceof Error ? e.message : word("saveFailed")) }
    finally { setSaving(false) }
  }
  const agents = reply?.agents ?? []
  const limits = reply?.limits ?? {}
  const dirty = !!draft && JSON.stringify(draft) !== JSON.stringify(reply?.settings)
  return <div className="pa-form-scrim pa-bs-scrim" {...backdrop}>
    <section ref={panel} className="pa-bs-panel" role="dialog" aria-modal="true" aria-labelledby="pa-bs-title"
      onKeyDown={e => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close() } else trapFocus(e, panel.current) }}>
      <header className="pa-bs-header">
        <h2 id="pa-bs-title">{word("title")}</h2>
        <button type="button" className="pa-bs-icon pa-bs-close" aria-label={word("close")} title={word("close")} onClick={close}><CloseIcon/></button>
      </header>
      <div className="pa-bs-body">
        {!draft && !error && <p className="pa-bs-note">{word("loading")}</p>}
        {draft && <>
          <section className="pa-bs-group" aria-labelledby="pa-bs-rules">
            <h3 id="pa-bs-rules">{word("rulesTitle")}</h3>
            <p className="pa-bs-note">{word("rulesNote")}</p>
            <ul className="pa-bs-list">
              {draft.rules.map((rule, i) => <RuleEditor key={rule.id} rule={rule} agents={agents} projects={projects}
                onChange={next => setDraft({ ...draft, rules: draft.rules.map((r, j) => j === i ? next : r) })}
                onDelete={() => setDraft({ ...draft, rules: draft.rules.filter((_, j) => j !== i) })}/>)}
            </ul>
            {draft.rules.length === 0 && <p className="pa-bs-empty">{word("rulesEmpty")}</p>}
            {draft.rules.length < (limits.rules ?? 12) && agents.length > 0 && <button type="button" className="pa-bs-add" onClick={() => setDraft({ ...draft, rules: [...draft.rules, { id: newID(), kind: "", agents: [{ agent: agents[0].agent }], scope: { all: true } }] })}>{word("addRule")}</button>}
          </section>
          <section className="pa-bs-group" aria-labelledby="pa-bs-memory">
            <h3 id="pa-bs-memory">{word("memoryTitle")}</h3>
            <p className="pa-bs-note">{word("memoryNote")}</p>
            <ul className="pa-bs-list">
              {draft.memory.map((entry, i) => <MemoryRow key={entry.id} entry={entry} projects={projects}
                onChange={next => setDraft({ ...draft, memory: draft.memory.map((m, j) => j === i ? next : m) })}
                onDelete={() => setDraft({ ...draft, memory: draft.memory.filter((_, j) => j !== i) })}/>)}
            </ul>
            {draft.memory.length === 0 && <p className="pa-bs-empty">{word("memoryEmpty")}</p>}
            {draft.memory.length < (limits.memory_entries ?? 20) && <button type="button" className="pa-bs-add" onClick={() => setDraft({ ...draft, memory: [...draft.memory, { id: newID(), text: "", scope: { all: true } }] })}>{word("addMemory")}</button>}
          </section>
        </>}
        {error && <p className="pa-bs-error" role="alert">{error}</p>}
      </div>
      <footer className="pa-bs-footer">
        <button type="button" className="pa-bs-secondary" onClick={close}>{word("cancel")}</button>
        <button type="button" className="pa-bs-primary" disabled={!dirty || saving} onClick={() => void save()}>{saving ? word("saving") : word("save")}</button>
      </footer>
    </section>
  </div>
}

/** The per-Bot "use shared memory" switch lives in the same document. */
export async function setBotUsesSharedMemory(botID: string, use: boolean, fetcher: typeof fetch = fetch): Promise<SharedSettingsReply> {
  const current = await readSharedSettings(fetcher)
  const optOut = (current.settings.memory_opt_out ?? []).filter(id => id !== botID)
  if (!use) optOut.push(botID)
  return writeSharedSettings({ ...current.settings, memory_opt_out: optOut }, fetcher)
}

/** Project choices from the roster's [machine, project] name map, one per Project. */
export function sharedSettingsProjects(names: ReadonlyMap<string, string>, personal: string): ProjectChoice[] {
  const out = new Map<string, string>()
  for (const [key, name] of names) {
    try {
      const [, id] = JSON.parse(key) as [string, string]
      if (id && !out.has(id)) out.set(id, name || (id.startsWith("personal") ? personal : id))
    } catch { /* not a roster key */ }
  }
  return [...out].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
}

/** The Bot's "use shared memory" switch: on by default, saved at once. */
export function useSharedMemorySwitch(botID: string, fetcher: typeof fetch = fetch): { use: boolean; available: boolean; set: (use: boolean) => void } {
  const [state, setState] = useState<{ use: boolean; available: boolean }>({ use: true, available: false })
  useEffect(() => {
    let live = true
    readSharedSettings(fetcher).then(r => { if (live) setState({ use: !(r.settings.memory_opt_out ?? []).includes(botID), available: true }) }, () => {})
    return () => { live = false }
  }, [botID, fetcher])
  const set = (use: boolean) => {
    setState({ use, available: true })
    setBotUsesSharedMemory(botID, use, fetcher).catch(() => setState({ use: !use, available: true }))
  }
  return { ...state, set }
}
