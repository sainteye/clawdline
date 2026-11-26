import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import type * as W from "@clawdline/contract"
import { catalogWord } from "../catalog.js"
import { useModalBackdrop } from "../modal-backdrop.js"
import { AgentMark } from "./AgentMark"
import { agentName } from "./fallback-settings"
import { PaSelect } from "./PaSelect"
import { useModelCatalog, type ModelCatalog } from "../model-catalog/useModelCatalog"
import { catalogPicker, paSelectStatus } from "../model-catalog/picker"
import { errorCode, failure, type SharedSettingsAnswer, type SharedSettingsDocument, type SourceClient } from "./client"
import "./bots-settings.css"

// Machine-wide Bots settings: the default agent, dispatch rules and memory
// shared by every Bot. The daemon document is the source of truth; this modal
// edits a copy and writes the whole document with the version it read.

export type SharedScope = { all: boolean; projects?: string[] }
export type RuleAgent = { agent: string; model?: string; effort?: string }
export type DispatchRule = { id: string; kind: string; hint?: string; agents: RuleAgent[]; scope: SharedScope }
export type SharedMemory = { id: string; text: string; scope: SharedScope }
export type SharedSettings = Omit<SharedSettingsDocument, "rules" | "memory"> & { default?: RuleAgent; rules: DispatchRule[]; memory: SharedMemory[] }
export type AgentOption = { agent: string; efforts: string[] }
export type SharedSettingsReply = { machine_id?: string; settings: SharedSettings; agents: AgentOption[]; limits: Record<string, number> }
export type ProjectChoice = { id: string; name: string }

// Every agent a Bot can dispatch, in the order the Session list uses. An agent
// this machine cannot run is still shown, marked, so the choice is never a mystery.
const ALL_AGENTS: readonly W.ProjectAssistantAgent[] = ["claude", "codex", "antigravity", "grok"]
// The daemon's policy agent when no default is saved (projectAssistantDispatchPolicy).
const BUILT_IN_DEFAULT = "claude"
const MAX_RULE_AGENTS = 4

const word = (key: string) => catalogWord("botsSettings", key)
const newID = () => Math.random().toString(36).slice(2, 12)
const displaySettingsListeners = new Set<(source: SourceClient, enabled: boolean) => void>()

/** Why one machine's settings could not be read or saved, in words a person can act on. */
export function sharedSettingsProblem(error: unknown, saving: boolean): string {
  const code = errorCode(error)
  switch (code) {
    case "pa_unsupported": return word("problemUnsupported")
    case "pa_offline": return word("problemOffline")
    case "pa_unpaired": case "pa_revoked": return word("problemUnpaired")
    case "pa_disabled": return word("problemDisabled")
    case "pa_read_only": return saving ? word("problemReadOnly") : word("problemDenied")
    case "pa_scope_denied": return word("problemDenied")
    case "pa_stale_version": return word("stale")
    case "pa_capacity_full": return word("full")
    case "pa_outcome_unknown": return word("problemUnknownSave")
    default: return (saving ? word("saveFailed") : word("loadFailed")) + (code !== "unavailable" ? ` (${code})` : "")
  }
}

/** Reads one machine's document through its captured source, never a relative fetch. */
export async function readSharedSettings(source: SourceClient, signal: AbortSignal = new AbortController().signal): Promise<SharedSettingsReply> {
  if (typeof source.sharedSettings !== "function") throw failure("pa_unsupported")
  const answer = await source.sharedSettings(signal) as SharedSettingsAnswer<SharedSettings>
  if (answer.machine_id !== source.machineID || !answer.settings || !Array.isArray(answer.settings.rules) || !Array.isArray(answer.settings.memory) ||
    answer.settings.peer_dispatch_cards !== undefined && typeof answer.settings.peer_dispatch_cards !== "boolean") throw failure("pa_invalid")
  return answer
}

/** Replaces the whole document, naming the version it was read at. */
export async function writeSharedSettings(source: SourceClient, settings: SharedSettings, signal: AbortSignal = new AbortController().signal): Promise<SharedSettingsReply> {
  if (typeof source.saveSharedSettings !== "function") throw failure("pa_unsupported")
  const id = crypto.randomUUID()
  const answer = await source.saveSharedSettings({ machine_id: source.machineID, idempotency_key: id, expected_version: settings.version, settings }, signal) as SharedSettingsAnswer<SharedSettings>
  if (answer.machine_id !== source.machineID || !answer.settings) throw failure("pa_invalid")
  for (const listener of displaySettingsListeners) listener(source, answer.settings.peer_dispatch_cards === true)
  return answer
}

export function usePeerDispatchCards(source: SourceClient | undefined): boolean {
  const [setting, setSetting] = useState<{ source: SourceClient; enabled: boolean }>()
  useEffect(() => {
    if (!source?.sharedSettings) return
    const read = new AbortController()
    const saved = (changed: SourceClient, enabled: boolean) => {
      if (changed.machineID !== source.machineID || changed.authorizationRevision !== source.authorizationRevision) return
      read.abort()
      setSetting({ source, enabled })
    }
    displaySettingsListeners.add(saved)
    void readSharedSettings(source, read.signal).then(answer => {
      if (!read.signal.aborted) setSetting({ source, enabled: answer.settings.peer_dispatch_cards === true })
    }, () => {})
    return () => { read.abort(); displaySettingsListeners.delete(saved) }
  }, [source])
  return !!setting && setting.source === source && setting.enabled
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

function Chevron({ dir = "down" }: { dir?: "down" | "right" | "left" }) {
  const d = dir === "right" ? "M6 4l4 4-4 4" : dir === "left" ? "M10 4L6 8l4 4" : "M4 6l4 4 4-4"
  return <svg className="pa-bs-chevron" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={d}/></svg>
}

function CloseIcon() {
  return <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M4 4l8 8M12 4L4 12"/></svg>
}

export function scopeLabel(scope: SharedScope, projects: readonly ProjectChoice[]): string {
  if (scope.all) return word("allProjects")
  const names = (scope.projects ?? []).map(id => projects.find(p => p.id === id)?.name ?? id)
  return names.length ? names.join(", ") : word("noProjects")
}

/** A checkbox popover: All Projects (including future ones) or chosen Projects. */
export function ScopePicker({ scope, projects, onChange }: { scope: SharedScope; projects: readonly ProjectChoice[]; onChange: (scope: SharedScope) => void }) {
  const [open, setOpen] = useState(false)
  const [place, setPlace] = useState<{ side: "start" | "end"; width: number } | null>(null)
  const root = useRef<HTMLDivElement>(null), pop = useRef<HTMLDivElement>(null)
  const id = useId()
  // The list opens on whichever side of its button has room inside the scrolling
  // pane, and is never wider than that pane, so no option is cut off.
  useLayoutEffect(() => {
    if (!open) { setPlace(null); return }
    const button = root.current, list = pop.current
    const pane = button?.closest(".pa-bs-body") ?? button?.closest("[role=dialog]")
    if (!button || !list || !pane) return
    const b = button.getBoundingClientRect(), p = pane.getBoundingClientRect(), gap = 8
    const toEnd = p.right - b.left - gap, toStart = b.right - p.left - gap
    const width = Math.min(Math.max(list.getBoundingClientRect().width, 220), 360, p.width - 2 * gap)
    const side = toEnd >= width || toEnd >= toStart ? "start" : "end"
    setPlace({ side, width: Math.max(0, Math.min(width, Math.max(toEnd, toStart))) })
  }, [open, projects])
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
    {open && <div ref={pop} className="pa-bs-scope-pop" role="group" aria-label={word("appliesTo")} data-side={place?.side} style={place ? { width: place.width } : { visibility: "hidden", width: "max-content" }}>
      <label><input type="checkbox" aria-labelledby={`${id}-all`} checked={scope.all} onChange={() => onChange(scope.all ? { all: false, projects: [] } : { all: true })}/><span id={`${id}-all`}>{word("allProjectsFuture")}</span></label>
      {projects.map((p, i) => <label key={p.id}><input type="checkbox" aria-labelledby={`${id}-${i}`} disabled={scope.all} checked={!scope.all && chosen.includes(p.id)} onChange={() => toggle(p.id)}/><span id={`${id}-${i}`}>{p.name}</span></label>)}
    </div>}
  </div>
}

type Catalogs = ModelCatalog

/** A model drop-down for one agent: the machine's model catalog, with its loading and failure states inside the list. */
function ModelSelect({ agent, value, onChange, catalogs, disabled }: { agent: string; value?: string; onChange: (model?: string) => void; catalogs: Catalogs; disabled?: boolean }) {
  const name = agentName(agent)
  const picker = catalogPicker(catalogs.get(agent), value, name, {
    defaultNamed: word("modelDefaultNamed"), defaultPlain: word("modelDefaultPlain"), loading: word("modelLoading"), failed: word("modelFailed"),
    empty: word("modelEmpty"), older: word("modelOlderComputer"), missing: word("modelMissing"), retry: word("retry"),
  })
  // A disabled agent is not being asked: no spinner for it.
  const status = disabled && picker.status?.kind === "loading" ? undefined : paSelectStatus(picker.status, catalogs.retry)
  return <PaSelect compact ariaLabel={`${name} ${word("model")}`} value={value ?? ""} options={picker.options} busy={!disabled && picker.loading} status={status} disabled={disabled} onChange={next => onChange(next || undefined)}/>
}

function EffortSelect({ agent, value, efforts, onChange, disabled }: { agent: string; value?: string; efforts: readonly string[]; onChange: (effort?: string) => void; disabled?: boolean }) {
  if (!efforts.length) return null
  return <PaSelect compact ariaLabel={`${agentName(agent)} ${word("effort")}`} value={value ?? ""} disabled={disabled}
    options={[{ value: "", label: word("effortDefault") }, ...efforts.map(e => ({ value: e, label: e }))]} onChange={next => onChange(next || undefined)}/>
}

function DefaultAgent({ value, agents, catalogs, onChange }: { value?: RuleAgent; agents: readonly AgentOption[]; catalogs: Catalogs; onChange: (next?: RuleAgent) => void }) {
  const chosen = value?.agent ?? BUILT_IN_DEFAULT
  const available = (agent: string) => agents.some(a => a.agent === agent)
  // Claude Code with nothing else set is the built-in default, so it is stored as no default at all.
  const set = (next: RuleAgent) => onChange(next.agent === BUILT_IN_DEFAULT && !next.model && !next.effort ? undefined : next)
  const efforts = agents.find(a => a.agent === chosen)?.efforts ?? []
  return <section className="pa-bs-group" aria-labelledby="pa-bs-default">
    <h3 id="pa-bs-default">{word("defaultTitle")}</h3>
    <p className="pa-bs-note">{word("defaultNote")}</p>
    <div className="pa-bs-tiles" role="radiogroup" aria-labelledby="pa-bs-default">
      {ALL_AGENTS.map(agent => {
        const on = agent === chosen, usable = available(agent)
        return <button key={agent} type="button" role="radio" aria-checked={on} className="pa-bs-tile" data-on={on || undefined} disabled={!usable && !on}
          onClick={() => { if (!on) set({ agent }) }}>
          <AgentMark agent={agent} size={20}/>
          <span className="pa-bs-tile-name">{agentName(agent)}</span>
          <span className="pa-bs-tile-sub">{on ? word("defaultBadge") : usable ? word("installed") : word("notInstalled")}</span>
        </button>
      })}
    </div>
    {!available(chosen) && <p className="pa-bs-note pa-bs-warn">{word("defaultUnavailable").replace("{agent}", agentName(chosen))}</p>}
    <div className="pa-bs-model-row">
      <ModelSelect agent={chosen} value={value?.model} catalogs={catalogs} disabled={!available(chosen)} onChange={model => set({ agent: chosen, model, effort: value?.effort })}/>
      <EffortSelect agent={chosen} value={value?.effort} efforts={efforts} disabled={!available(chosen)} onChange={effort => set({ agent: chosen, model: value?.model, effort })}/>
    </div>
  </section>
}

function RuleRow({ rule, projects, onOpen }: { rule: DispatchRule; projects: readonly ProjectChoice[]; onOpen: () => void }) {
  const first = rule.agents[0]
  const kind = rule.kind.trim()
  return <li>
    <button type="button" className="pa-bs-kind-row" onClick={onOpen}>
      <span className="pa-bs-kind-name">
        <span className={kind ? undefined : "pa-bs-dim"}>{kind || word("unnamedKind")}</span>
        <small>{scopeLabel(rule.scope, projects)}</small>
      </span>
      <span className="pa-bs-kind-agent">
        {first && <AgentMark agent={first.agent as W.ProjectAssistantAgent} size={16}/>}
        <span>{first ? agentName(first.agent) : ""}</span>
        {rule.agents.length > 1 && <small>{word("fallbackCount").replace("{n}", String(rule.agents.length - 1))}</small>}
      </span>
      <Chevron dir="right"/>
    </button>
  </li>
}

function RuleDetail({ rule, agents, projects, catalogs, onChange, onDelete, onBack }: {
  rule: DispatchRule; agents: readonly AgentOption[]; projects: readonly ProjectChoice[]; catalogs: Catalogs
  onChange: (rule: DispatchRule) => void; onDelete: () => void; onBack: () => void
}) {
  const kindInput = useRef<HTMLInputElement>(null)
  useEffect(() => { if (!rule.kind) kindInput.current?.focus() }, [rule.id])
  const available = (agent: string) => agents.some(a => a.agent === agent)
  const setAgent = (agent: string, next: RuleAgent) => onChange({ ...rule, agents: rule.agents.map(a => a.agent === agent ? next : a) })
  // Agents saved by an older version but not among the four still show, so nothing is dropped silently.
  const listed = [...ALL_AGENTS, ...rule.agents.map(a => a.agent).filter(a => !ALL_AGENTS.includes(a as W.ProjectAssistantAgent))]
  return <section className="pa-bs-group pa-bs-detail" aria-labelledby="pa-bs-detail-title">
    <div className="pa-bs-detail-head">
      <button type="button" className="pa-bs-icon" aria-label={word("back")} title={word("back")} onClick={onBack}><Chevron dir="left"/></button>
      <h3 id="pa-bs-detail-title">{rule.kind.trim() || word("unnamedKind")}</h3>
    </div>
    <div className="pa-bs-fields">
      <label className="pa-bs-field"><span>{word("kind")}</span>
        <input ref={kindInput} className="pa-bs-input" placeholder={word("kindPlaceholder")} value={rule.kind} maxLength={64} onChange={e => onChange({ ...rule, kind: e.target.value })}/>
      </label>
      <div className="pa-bs-field"><span>{word("appliesTo")}</span><ScopePicker scope={rule.scope} projects={projects} onChange={scope => onChange({ ...rule, scope })}/></div>
    </div>
    <label className="pa-bs-field"><span>{word("hintLabel")}</span>
      <input className="pa-bs-input" placeholder={word("hintPlaceholder")} value={rule.hint ?? ""} maxLength={300} onChange={e => onChange({ ...rule, hint: e.target.value })}/>
    </label>
    <div className="pa-bs-field">
      <span id="pa-bs-order-label">{word("agentsPick")}</span>
      <ul className="pa-bs-order-list" aria-labelledby="pa-bs-order-label">
        {listed.map(agent => {
          const at = rule.agents.findIndex(a => a.agent === agent), included = at >= 0, usable = available(agent)
          const last = included && rule.agents.length < 2, full = !included && rule.agents.length >= MAX_RULE_AGENTS
          const entry = rule.agents[at]
          const efforts = agents.find(a => a.agent === agent)?.efforts ?? []
          return <li key={agent} className="pa-bs-order-item" data-on={included || undefined}>
            <button type="button" className="pa-bs-order-toggle" aria-pressed={included} disabled={last || full || (!included && !usable)}
              title={last ? word("agentLast") : undefined}
              onClick={() => onChange({ ...rule, agents: included ? rule.agents.filter(a => a.agent !== agent) : [...rule.agents, { agent }] })}>
              <span className="pa-bs-order" data-first={at === 0 || undefined}>{included ? at + 1 : ""}</span>
              <AgentMark agent={agent as W.ProjectAssistantAgent} size={18}/>
              <span className="pa-bs-order-name">{agentName(agent)}</span>
              {!usable && <small>{word("notInstalled")}</small>}
              {last && <small>{word("agentLast")}</small>}
            </button>
            {included && entry && <div className="pa-bs-model-row pa-bs-order-settings">
              <ModelSelect agent={agent} value={entry.model} catalogs={catalogs} disabled={!usable} onChange={model => setAgent(agent, { ...entry, model })}/>
              <EffortSelect agent={agent} value={entry.effort} efforts={efforts} disabled={!usable} onChange={effort => setAgent(agent, { ...entry, effort })}/>
            </div>}
          </li>
        })}
      </ul>
    </div>
    <div className="pa-bs-detail-foot">
      <button type="button" className="pa-bs-danger" onClick={onDelete}>{word("deleteRule")}</button>
    </div>
  </section>
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

export type SettingsMachine = { source: SourceClient; name: string }

type Tab = "dispatch" | "memory"

export function BotsSettingsModal({ projects, onClose, machines, initialMachine }: {
  projects: readonly ProjectChoice[]; onClose: () => void; machines: readonly SettingsMachine[]; initialMachine?: string
}) {
  const panel = useRef<HTMLElement>(null)
  const opener = useRef<Element | null>(typeof document === "undefined" ? null : document.activeElement)
  const [machine, setMachine] = useState(() => machines.find(m => m.source.machineID === initialMachine)?.source.machineID ?? machines[0]?.source.machineID ?? "")
  const chosen = machines.find(m => m.source.machineID === machine)
  const source = chosen?.source
  const [reply, setReply] = useState<SharedSettingsReply>()
  const [draft, setDraft] = useState<SharedSettings>()
  const [loadError, setLoadError] = useState("")
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [tab, setTab] = useState<Tab>("dispatch")
  const [editing, setEditing] = useState<string | null>(null)
  const catalogs = useModelCatalog(source)
  const close = () => { onClose(); (opener.current as HTMLElement | null)?.focus?.() }
  const backdrop = useModalBackdrop(close)
  useEffect(() => {
    const control = new AbortController()
    setReply(undefined); setDraft(undefined); setLoadError(""); setError(""); setEditing(null)
    if (!source) { setLoadError(sharedSettingsProblem(failure("pa_unsupported"), false)); return }
    readSharedSettings(source, control.signal).then(r => { if (!control.signal.aborted) { setReply(r); setDraft(r.settings) } },
      e => { if (!control.signal.aborted) setLoadError(sharedSettingsProblem(e, false)) })
    return () => control.abort()
  }, [source, attempt])
  useEffect(() => { panel.current?.querySelector<HTMLElement>(".pa-bs-close")?.focus() }, [])
  const closeRef = useRef(close); closeRef.current = close
  useEffect(() => {
    // On the document: a control that becomes disabled (Save after saving)
    // drops focus to the body, and Esc and Tab must still work.
    const keys = (e: KeyboardEvent) => {
      if (e.defaultPrevented) return
      if (e.key === "Escape") { e.preventDefault(); closeRef.current() } else trapFocus(e, panel.current)
    }
    document.addEventListener("keydown", keys)
    return () => document.removeEventListener("keydown", keys)
  }, [])
  const save = async () => {
    if (!draft || !source) return
    setSaving(true); setError("")
    try {
      const clean = { ...draft, rules: draft.rules.filter(r => r.kind.trim()), memory: draft.memory.filter(m => m.text.trim()) }
      const next = await writeSharedSettings(source, clean)
      setReply(next); setDraft(next.settings)
      if (editing && !next.settings.rules.some(r => r.id === editing)) setEditing(null)
    } catch (e) { setError(sharedSettingsProblem(e, true)) }
    finally { setSaving(false) }
  }
  const agents = reply?.agents ?? []
  const limits = reply?.limits ?? {}
  const dirty = !!draft && JSON.stringify(draft) !== JSON.stringify(reply?.settings)
  const rule = draft?.rules.find(r => r.id === editing)
  const setRules = (rules: DispatchRule[]) => draft && setDraft({ ...draft, rules })
  const addRule = () => {
    if (!draft) return
    const first = agents[0]?.agent ?? BUILT_IN_DEFAULT
    const next = { id: newID(), kind: "", agents: [{ agent: first }], scope: { all: true } }
    setDraft({ ...draft, rules: [...draft.rules, next] })
    setEditing(next.id)
  }
  return <div className="pa-form-scrim pa-bs-scrim" {...backdrop}>
    <section ref={panel} className="pa-bs-panel" role="dialog" aria-modal="true" aria-labelledby="pa-bs-title">
      <header className="pa-bs-header">
        <h2 id="pa-bs-title">{word("title")}</h2>
        <button type="button" className="pa-bs-icon pa-bs-close" aria-label={word("close")} title={word("close")} onClick={close}><CloseIcon/></button>
      </header>
      <div className="pa-bs-layout">
        <nav className="pa-bs-nav" aria-label={word("title")}>
          {(["dispatch", "memory"] as const).map(t => <button key={t} type="button" className="pa-bs-nav-item" aria-current={tab === t ? "page" : undefined}
            onClick={() => { setTab(t); setEditing(null) }}>{word(t === "dispatch" ? "tabDispatch" : "tabMemory")}</button>)}
        </nav>
        <div className="pa-bs-body">
          {machines.length > 1 && <div className="pa-bs-machine">
            <span id="pa-bs-machine-label">{word("machine")}</span>
            <PaSelect compact ariaLabel={word("machine")} value={machine} disabled={saving} onChange={setMachine}
              options={machines.map(m => ({ value: m.source.machineID, label: m.name }))}/>
          </div>}
          {!draft && !loadError && <p className="pa-bs-note" role="status">{word("loading")}</p>}
          {loadError && <div className="pa-bs-problem" role="alert">
            <p className="pa-bs-error">{loadError}</p>
            <button type="button" className="pa-bs-secondary" onClick={() => setAttempt(a => a + 1)}>{word("retry")}</button>
          </div>}
          {draft && tab === "dispatch" && (rule
            ? <RuleDetail rule={rule} agents={agents} projects={projects} catalogs={catalogs}
              onChange={next => setRules(draft.rules.map(r => r.id === next.id ? next : r))}
              onDelete={() => { setRules(draft.rules.filter(r => r.id !== rule.id)); setEditing(null) }}
              onBack={() => setEditing(null)}/>
            : <>
              {reply?.settings.peer_dispatch_cards !== undefined && <div className="pa-bs-display-setting">
                <div>
                  <strong id="pa-bs-peer-dispatch-title">{word("peerDispatchCards")}</strong>
                  <small id="pa-bs-peer-dispatch-detail">{word("peerDispatchCardsHint")}</small>
                </div>
                <button type="button" role="switch" className="pa-switch" aria-checked={draft.peer_dispatch_cards === true}
                  aria-labelledby="pa-bs-peer-dispatch-title" aria-describedby="pa-bs-peer-dispatch-detail" disabled={saving}
                  onClick={() => setDraft({ ...draft, peer_dispatch_cards: !draft.peer_dispatch_cards })}><span/></button>
              </div>}
              <DefaultAgent value={draft.default} agents={agents} catalogs={catalogs} onChange={next => setDraft({ ...draft, default: next })}/>
              <section className="pa-bs-group" aria-labelledby="pa-bs-rules">
                <h3 id="pa-bs-rules">{word("kindsTitle")}</h3>
                <p className="pa-bs-note">{word("kindsNote")}</p>
                {draft.rules.length > 0 ? <ul className="pa-bs-kinds">
                  {draft.rules.map(r => <RuleRow key={r.id} rule={r} projects={projects} onOpen={() => setEditing(r.id)}/>)}
                </ul> : <p className="pa-bs-empty">{word("kindsEmpty")}</p>}
                {draft.rules.length < (limits.rules ?? 12) && <button type="button" className="pa-bs-add" onClick={addRule}>{word("addKind")}</button>}
              </section>
            </>)}
          {draft && tab === "memory" && <section className="pa-bs-group" aria-labelledby="pa-bs-memory">
            <h3 id="pa-bs-memory">{word("memoryTitle")}</h3>
            <p className="pa-bs-note">{word("memoryNote")}</p>
            <ul className="pa-bs-list">
              {draft.memory.map((entry, i) => <MemoryRow key={entry.id} entry={entry} projects={projects}
                onChange={next => setDraft({ ...draft, memory: draft.memory.map((m, j) => j === i ? next : m) })}
                onDelete={() => setDraft({ ...draft, memory: draft.memory.filter((_, j) => j !== i) })}/>)}
            </ul>
            {draft.memory.length === 0 && <p className="pa-bs-empty">{word("memoryEmpty")}</p>}
            {draft.memory.length < (limits.memory_entries ?? 20) && <button type="button" className="pa-bs-add" onClick={() => setDraft({ ...draft, memory: [...draft.memory, { id: newID(), text: "", scope: { all: true } }] })}>{word("addMemory")}</button>}
          </section>}
          {error && <p className="pa-bs-error" role="alert">{error}</p>}
        </div>
      </div>
      <footer className="pa-bs-footer">
        <button type="button" className="pa-bs-secondary" onClick={close}>{word("cancel")}</button>
        <button type="button" className="pa-bs-primary" disabled={!dirty || saving} onClick={() => void save()}>{saving ? word("saving") : word("save")}</button>
      </footer>
    </section>
  </div>
}

/** The per-Bot "use shared memory" switch lives in the same document. */
export async function setBotUsesSharedMemory(source: SourceClient, botID: string, use: boolean): Promise<SharedSettingsReply> {
  const current = await readSharedSettings(source)
  const optOut = (current.settings.memory_opt_out ?? []).filter(id => id !== botID)
  if (!use) optOut.push(botID)
  return writeSharedSettings(source, { ...current.settings, memory_opt_out: optOut })
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
export function useSharedMemorySwitch(source: SourceClient | undefined, botID: string): { use: boolean; available: boolean; set: (use: boolean) => void } {
  const [state, setState] = useState<{ use: boolean; available: boolean }>({ use: true, available: false })
  useEffect(() => {
    const control = new AbortController()
    setState({ use: true, available: false })
    if (source) readSharedSettings(source, control.signal).then(r => { if (!control.signal.aborted) setState({ use: !(r.settings.memory_opt_out ?? []).includes(botID), available: true }) }, () => {})
    return () => control.abort()
  }, [botID, source])
  const set = (use: boolean) => {
    if (!source) return
    setState({ use, available: true })
    setBotUsesSharedMemory(source, botID, use).catch(() => setState({ use: !use, available: true }))
  }
  return { ...state, set }
}
