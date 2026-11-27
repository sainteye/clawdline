import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { BotAvatar } from "./BotAvatar"
import { palette } from "./avatar-paths"
import { charterFormat, charterWord } from "./charter-words"
import { charterWorkflow, proposalKind, type CharterChangeTarget } from "./charter-api"
import { layoutWorkflow, layoutWorkflowDiff, readingOrder, workflowDiff, type PlacedWorkflowState, type RoutedTransition, type WorkflowChange, type WorkflowLayout, type WorkflowTransition } from "./workflow-layout"
import "./charter-workflow.css"

// The charter's "一件事怎麼走" drawn as its state machine, read-only: the lead
// proposes a change and the person applies it, or a terminal Session writes it
// on the person's words, so the page has no editor. A pending workflow
// proposal is drawn above the graph as only what it changes. The drawing is
// hidden from assistive technology; an off-screen list reads the same states
// and transitions as sentences.

type Workflow = W.ProjectAssistantCharterWorkflow
type Proposal = W.ProjectAssistantCharterProposal

/** A Bot as a state card shows it: its name and, once the roster has it, its saved look. */
export interface WorkflowBot { name: string; appearance?: W.ProjectAssistantAppearance }

const headings = new Set(["一件事怎麼走", "How one piece of work flows"])
/** Whether a charter section is the free text the workflow replaces. */
export const isWorkflowHeading = (heading: string) => headings.has(heading.trim())

// A phone scales the drawing down to cards this wide, then scrolls it sideways in its own box.
const minScale = 0.7
const edgeColour: Record<string, string> = { normal: "#6b6b73", pass: "#86c997", return: "#e39567" }
const changeColour: Partial<Record<WorkflowChange, string>> = { added: "#86c997", changed: "#86c997", removed: "#db8578" }

const botColour = (bot: WorkflowBot | undefined) => palette.find(c => c === bot?.appearance?.color.toLowerCase()) ?? palette[0]
const fallbackAppearance = { shape: "blob", color: "" } as W.ProjectAssistantAppearance

/** The sentence for one transition, e.g. 從畫面驗收退回派工・審證據，不通過時. */
export function transitionSentence(t: WorkflowTransition, title: (id: string) => string) {
  const line = charterFormat(t.kind === "return" ? "workflowEdgeBack" : "workflowEdge", title(t.from), title(t.to))
  return t.label ? charterFormat("workflowEdgeWhen", line, t.label) : line
}

function stateSentence(placed: PlacedWorkflowState, botOf: (id: string) => WorkflowBot | undefined) {
  const s = placed.state
  const who = s.bot_id ? botOf(s.bot_id)?.name || charterWord("workflowOtherBot") : charterWord("workflowAnyBot")
  const line = s.kind === "done" ? charterFormat("workflowStateDone", s.title) : charterFormat(s.kind === "start" ? "workflowStateStart" : "workflowState", s.title, who)
  return placed.unreached ? charterFormat("workflowStateUnreached", line) : line
}

/** The drawing's scale for the width its box has: never larger than drawn, never below minScale. */
function useFit(width: number) {
  const box = useRef<HTMLDivElement>(null)
  const [room, setRoom] = useState(0)
  useEffect(() => {
    const el = box.current
    if (!el || typeof ResizeObserver === "undefined") return
    const seen = new ResizeObserver(([entry]) => setRoom(entry!.contentRect.width))
    seen.observe(el)
    return () => seen.disconnect()
  }, [])
  return { box, scale: room > 0 ? Math.max(minScale, Math.min(1, room / width)) : 1 }
}

function Arrowheads({ id, colours }: { id: string; colours: readonly string[] }) {
  return <defs>{colours.map(colour => <marker key={colour} id={`${id}-${colour.slice(1)}`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
    <path d="M0.5,0.8 L7,4 L0.5,7.2 Z" fill={colour}/>
  </marker>)}</defs>
}

function Edge({ edge, marker, change }: { edge: RoutedTransition; marker: string; change?: WorkflowChange }) {
  const kind = edge.transition.kind
  const colour = change && changeColour[change] || edgeColour[kind] || edgeColour.normal
  const dashed = change === "added" || change === "changed" || (!change && kind === "return")
  return <g className="pa-wf-edge" data-kind={kind} data-change={change}>
    <path d={edge.path} fill="none" stroke={colour} strokeWidth="1.6" strokeDasharray={dashed ? "6 5" : undefined} markerEnd={`url(#${marker}-${colour.slice(1)})`}/>
    {edge.label && <text x={edge.label.textX} y={edge.label.textY} textAnchor={edge.label.anchor} fill={colour} fontSize="12" fontWeight="550">{edge.label.text}</text>}
  </g>
}

function StateCard({ placed, bot, change }: { placed: PlacedWorkflowState; bot?: WorkflowBot; change?: WorkflowChange }) {
  const s = placed.state
  const box = { left: placed.x, top: placed.y, width: placed.width, height: placed.height }
  if (s.kind === "done") return <div className="pa-wf-done" style={box} data-change={change} data-unreached={placed.unreached || undefined}>
    <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M3.5 8.5 6.5 11.5 12.5 4.5"/></svg>
    <span>{s.title}</span>
  </div>
  const colour = s.bot_id ? botColour(bot) : undefined
  return <div className="pa-wf-state" style={{ ...box, ...(colour ? { "--pa-wf-bot": colour } as object : {}) }} data-change={change} data-unreached={placed.unreached || undefined} data-bot={s.bot_id ? "" : undefined}>
    {s.bot_id ? <BotAvatar appearance={bot?.appearance ?? fallbackAppearance} size={36}/> : <span className="pa-wf-card-icon"><CardIcon/></span>}
    <span className="pa-wf-state-text">
      <strong>{s.title}</strong>
      <small>{s.bot_id ? bot?.name || charterWord("workflowOtherBot") : charterWord("workflowAnyBot")}</small>
    </span>
  </div>
}

/** One drawing: the edges under the cards, scaled to its box, with the sentences it shows. */
function WorkflowCanvas({ layout, botOf, label, states, transitions }: {
  layout: WorkflowLayout; botOf: (id: string) => WorkflowBot | undefined; label: string
  states?: ReadonlyMap<string, WorkflowChange>; transitions?: ReadonlyMap<string, WorkflowChange>
}) {
  const marker = useId().replace(/:/g, "")
  const { box, scale } = useFit(layout.width)
  const title = (id: string) => layout.states.find(s => s.state.id === id)?.state.title ?? id
  const changed = (line: string, change: WorkflowChange | undefined) => change ? charterFormat(`workflowChange_${change}`, line) : line
  const start = layout.states.find(s => s.state.id === layout.start)
  const colours = [...new Set([...Object.values(edgeColour), ...Object.values(changeColour)])]
  // A drawing wider than its box opens with the centre column in view, not the left branch.
  const centre = layout.states.find(s => s.col === 0)
  useEffect(() => {
    const el = box.current
    if (el && centre && el.scrollWidth > el.clientWidth) el.scrollLeft = (centre.x + centre.width / 2) * scale - el.clientWidth / 2
  }, [scale, layout.width, centre?.x])
  return <>
    <div ref={box} className="pa-wf-box">
      <div className="pa-wf-canvas" aria-hidden="true" style={{ width: layout.width, height: layout.height, zoom: scale }}>
        <svg className="pa-wf-lines" width={layout.width} height={layout.height} viewBox={`0 0 ${layout.width} ${layout.height}`}>
          <Arrowheads id={marker} colours={colours}/>
          {layout.transitions.map(edge => <Edge key={edge.key} edge={edge} marker={marker} change={transitions?.get(edge.key)}/>)}
          {start && <path d={`M${start.x + start.width / 2},${start.y - 10} L${start.x + start.width / 2},${start.y - 2}`} stroke="#86c997" strokeWidth="1.6" markerEnd={`url(#${marker}-86c997)`}/>}
        </svg>
        {start && <span className="pa-wf-start" style={{ left: start.x + start.width / 2, top: start.y - 30 }}>{charterWord("workflowStart")}</span>}
        {layout.unreachedTop !== undefined && <span className="pa-wf-unreached" style={{ top: layout.unreachedTop }}>{charterWord("workflowUnreached")}</span>}
        {layout.states.map(placed => <StateCard key={placed.state.id} placed={placed} bot={placed.state.bot_id ? botOf(placed.state.bot_id) : undefined} change={states?.get(placed.state.id)}/>)}
      </div>
    </div>
    <ul className="pa-visually-hidden" aria-label={label}>
      {readingOrder(layout).map(placed => <li key={placed.state.id}>{changed(stateSentence(placed, botOf), states?.get(placed.state.id))}</li>)}
      {layout.transitions.map(edge => <li key={edge.key}>{changed(transitionSentence(edge.transition, title), transitions?.get(edge.key))}</li>)}
    </ul>
  </>
}

function Legend({ diff }: { diff?: boolean }) {
  const items = diff
    ? [["added", "#86c997", true, "workflowDiffAdded"], ["removed", "#db8578", false, "workflowDiffRemoved"], ["same", "#6b6b73", false, "workflowDiffSame"]] as const
    : [["normal", edgeColour.normal, false, "workflowLegendNext"], ["pass", edgeColour.pass, false, "workflowLegendPass"], ["return", edgeColour.return, true, "workflowLegendReturn"]] as const
  return <p className="pa-wf-legend" aria-hidden="true">
    {items.map(([kind, colour, dashed, key]) => <span key={kind} data-kind={kind}>
      <svg width="22" height="8" viewBox="0 0 22 8"><path d="M1 4H21" stroke={colour} strokeWidth="1.6" strokeDasharray={dashed ? "4 3" : undefined} opacity={kind === "same" ? 0.5 : 1}/></svg>
      {charterWord(key)}
    </span>)}
    {!diff && <span data-kind="done"><svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="#86c997" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M3.5 8.5 6.5 11.5 12.5 4.5"/></svg>{charterWord("workflowLegendDone")}</span>}
  </p>
}

/** A way back up the channel names whom the work goes back to: 不通過 直接退回 Atlas; beside its card it needs not. */
function returnLabels(workflow: Workflow, botOf: (id: string) => WorkflowBot | undefined) {
  const states = new Map(workflow.states.map(s => [s.id, s]))
  return (t: WorkflowTransition, inline: boolean) => {
    const to = states.get(t.to), name = to?.bot_id ? botOf(to.bot_id)?.name : ""
    if (t.kind !== "return" || !name || inline) return t.label
    return t.label ? charterFormat("workflowReturnLabel", t.label, name) : charterFormat("workflowReturnPlain", name)
  }
}

export interface WorkflowProposalView { proposal: Proposal; author: string; next: number; stale: boolean; busy: boolean; onApply: () => void; onDismiss: () => void; onRetouch: () => void }

function WorkflowProposalCard({ view, current, botOf }: { view: WorkflowProposalView; current: Workflow | null; botOf: (id: string) => WorkflowBot | undefined }) {
  const { proposal, author, next, stale, busy } = view
  const diff = useMemo(() => workflowDiff(current, proposal.workflow ?? current), [current, proposal.workflow])
  const layout = useMemo(() => layoutWorkflowDiff(diff, returnLabels(diff.workflow, botOf)), [diff, botOf])
  return <article className="pa-charter-proposal pa-wf-proposal" aria-label={charterFormat("proposalFrom", author)}>
    <p className="pa-charter-proposal-title"><strong>{charterFormat("proposalFrom", author)}</strong></p>
    {proposal.summary && <p className="pa-charter-proposal-why">{proposal.summary}</p>}
    {stale && <p className="pa-charter-proposal-stale">{charterFormat("proposalStale", proposal.base_version)}</p>}
    {diff.shownStates.size > 0
      ? <><WorkflowCanvas layout={layout} botOf={botOf} label={charterWord("workflowDiffLabel")} states={diff.states} transitions={diff.transitions}/><Legend diff/></>
      : <p className="pa-charter-hint">{charterWord("workflowProposalNoChange")}</p>}
    <p className="pa-charter-hint">{charterWord("workflowProposalOnly")} {charterWord("proposalUntilApply")}</p>
    <div className="pa-charter-actions">
      <button type="button" className="pa-charter-primary" disabled={busy} onClick={view.onApply}>{charterFormat("workflowApply", next)}</button>
      <button type="button" disabled={busy} onClick={view.onDismiss}>{charterWord("workflowDismiss")}</button>
      <button type="button" disabled={busy} onClick={view.onRetouch}>{charterWord("workflowRetouch")}</button>
    </div>
  </article>
}

/**
 * The workflow section: its header and the ask button, a pending proposal,
 * the graph and its legend, and the free text it replaces behind a disclosure
 * until the person removes that text.
 */
export function CharterWorkflowSection({ workflow, version, proposal, botOf, onAsk, busy, oldText }: {
  workflow: Workflow | null; version: number; proposal?: WorkflowProposalView; botOf: (id: string) => WorkflowBot | undefined
  onAsk: () => void; busy: boolean; oldText?: ReactNode
}) {
  const layout = useMemo(() => workflow && layoutWorkflow(workflow, returnLabels(workflow, botOf)), [workflow, botOf])
  return <section className="pa-wf" aria-label={charterWord("workflowTitle")}>
    <header className="pa-wf-head">
      <div>
        <h3>{charterWord("workflowTitle")}</h3>
        <p>{charterFormat("workflowMeta", version)}</p>
      </div>
      <button type="button" className="pa-pill" disabled={busy} onClick={onAsk}>{charterWord("workflowAsk")}</button>
    </header>
    {proposal && <WorkflowProposalCard view={proposal} current={workflow} botOf={botOf}/>}
    {layout && <><WorkflowCanvas layout={layout} botOf={botOf} label={charterWord("workflowListLabel")}/><Legend/></>}
    {oldText && <details className="pa-wf-old">
      <summary>{charterWord("workflowOldText")}</summary>
      {oldText}
    </details>}
  </section>
}

/**
 * The charter page's workflow wiring: which proposal is the workflow's, where
 * the section goes (in place of its free-text section, kept behind a
 * disclosure, or after the sections when there is none), and whether the
 * composer's next request is for the workflow. A machine older than API 128
 * is not enabled, so it shows nothing of this.
 */
export function useCharterWorkflow(input: {
  enabled: boolean; doc: W.ProjectAssistantCharterResponse | null; proposals: readonly Proposal[]; headings: readonly string[]
  readers: readonly { bot_id: string; name: string }[]; appearance?: (botID: string) => W.ProjectAssistantAppearance | undefined
  next: number; busy: boolean; author: (botID: string) => string
  /** The page's retouch: the proposal the composer's next request revises. */
  setRetouch: (id: string | null) => void; focus: () => void
  onApply: (p: Proposal) => void; onDismiss: (p: Proposal) => void
}) {
  const { enabled, doc, readers, appearance } = input
  const [target, setTarget] = useState<CharterChangeTarget>("text")
  const proposal = enabled ? input.proposals.find(item => proposalKind(item) === "workflow") : undefined
  const workflow = enabled && doc ? charterWorkflow(doc) : null
  const shown = !!doc && (!!workflow || !!proposal)
  const at = shown ? input.headings.findIndex(isWorkflowHeading) : -1
  const botOf = (id: string): WorkflowBot => ({ name: readers.find(r => r.bot_id === id)?.name ?? "", appearance: appearance?.(id) })
  const retouch = (item: Proposal) => { input.setRetouch(item.id); setTarget(proposalKind(item)) }
  const cancel = () => { input.setRetouch(null); setTarget("text") }
  const section = (oldText?: ReactNode) => doc && <CharterWorkflowSection workflow={workflow} version={doc.version} busy={input.busy} oldText={oldText} botOf={botOf}
    onAsk={() => { input.setRetouch(null); setTarget("workflow"); input.focus() }}
    proposal={proposal && { proposal, author: input.author(proposal.bot_id), next: input.next, busy: input.busy, stale: proposal.stale || proposal.base_version !== doc.version,
      onApply: () => input.onApply(proposal), onDismiss: () => input.onDismiss(proposal), onRetouch: () => retouch(proposal) }}/>
  return {
    proposal, target, retouch, cancel,
    /** Above the composer: what its next request is for, with × to go back to a plain request. */
    hint: (retouching: string | null) => (retouching || target === "workflow") && <p className="pa-charter-retouch">{charterWord(retouching ? "retouchHint" : "workflowRequestHint")}
      <button type="button" onClick={cancel} aria-label={charterWord("retouchCancel")}>×</button></p>,
    /** The section card at this position, or the workflow holding it as its old text. */
    place: (position: number, key: number, card: ReactNode) => position === at ? <div key={key}>{section(card)}</div> : card,
    after: shown && at < 0 ? section() : null,
  }
}

function CardIcon() {
  return <svg viewBox="0 0 20 20" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="3" y="4" width="14" height="12" rx="2.5"/><path d="M6.5 8h7M6.5 11h4.5"/>
  </svg>
}
