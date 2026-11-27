// Where each state of a charter workflow sits and how each transition is drawn,
// as a pure function of the states and transitions: no DOM, no React.
//
// The graph is read top-down from the start, like a hand-drawn flow chart. The spine is the shortest forward path from the start to the nearest
// done state (pass transitions tried first, returns never), one state per row
// in a centre column. Every other reachable state is a side branch beside the
// state that first reaches it: on the left when its row is crossed by a way
// back (whose channel runs on the right), otherwise on the right; a branch of
// a branch goes one row lower. States the start cannot reach are laid out in
// rows of three under everything else and marked unreached; validation keeps
// them out, but a graph is never refused here.
//
// Edges are orthogonal. Down a row they turn halfway between the rows; within
// a row they run straight between neighbours; up (and to themselves) they run
// in a channel outside the cards, each in its own lane. A label sits beside or
// above its line and is measured so it never covers a card.
import type * as W from "@clawdline/contract"

export type WorkflowState = W.ProjectAssistantCharterWorkflowState
export type WorkflowTransition = W.ProjectAssistantCharterWorkflowTransition

export const workflowGeometry = {
  cardWidth: 208, cardHeight: 64, doneWidth: 132, doneHeight: 44, rowPitch: 120,
  pad: 16, startMarker: 30, unreachedHeader: 30, labelSize: 12, labelHeight: 16, lane: 12,
} as const

/** A state's row and column; a column may be fractional for the unreached rows. */
export interface WorkflowCell { row: number; col: number; unreached: boolean }

export interface Box { x: number; y: number; width: number; height: number }

export interface PlacedWorkflowState extends Box { state: WorkflowState; row: number; col: number; unreached: boolean }

export interface WorkflowLabel extends Box { text: string; anchor: "start" | "middle" | "end"; textX: number; textY: number }

export interface RoutedTransition {
  /** from->to, unique: a second transition between the same states is dropped. */
  key: string
  transition: WorkflowTransition
  /** The corners of the line, first to last; the last point touches the target. */
  points: [number, number][]
  path: string
  label?: WorkflowLabel
}

export interface WorkflowLayout {
  width: number; height: number
  states: PlacedWorkflowState[]
  transitions: RoutedTransition[]
  /** The top of the unreached rows' band, or undefined when every state is reached. */
  unreachedTop?: number
  /** The start state's id, when there is one to mark. */
  start?: string
}

export const transitionKey = (t: Pick<WorkflowTransition, "from" | "to">) => `${t.from}->${t.to}`

/** An estimate of a label's drawn width: SVG text has no measured overflow before render. */
export function labelWidth(text: string, size: number = workflowGeometry.labelSize) {
  let width = 0
  for (const ch of text) width += ch.charCodeAt(0) > 255 ? size : size * 0.6
  return Math.ceil(width)
}

/** The transitions whose endpoints both exist, the first one per from->to pair. */
function drawable(workflow: W.ProjectAssistantCharterWorkflow) {
  const ids = new Set(workflow.states.map(s => s.id)), seen = new Set<string>()
  return workflow.transitions.filter(t => {
    const key = transitionKey(t)
    if (!ids.has(t.from) || !ids.has(t.to) || seen.has(key)) return false
    seen.add(key)
    return true
  })
}

/** Breadth-first discovery order and first parent; `forward` leaves out returns and tries pass first. */
function discover(start: string | undefined, transitions: readonly WorkflowTransition[], forward: boolean) {
  const order: string[] = [], parent = new Map<string, string>()
  if (start === undefined) return { order, parent }
  const rank = (t: WorkflowTransition) => t.kind === "pass" ? 0 : 1
  const out = (id: string) => transitions.filter(t => t.from === id && (!forward || t.kind !== "return"))
    .map((t, i) => ({ t, i })).sort((a, b) => forward ? rank(a.t) - rank(b.t) || a.i - b.i : a.i - b.i).map(x => x.t.to)
  const seen = new Set([start])
  for (let queue = [start]; queue.length;) {
    const id = queue.shift()!
    order.push(id)
    for (const to of out(id)) if (!seen.has(to)) { seen.add(to); parent.set(to, id); queue.push(to) }
  }
  return { order, parent }
}

function spineOf(start: string | undefined, kinds: Map<string, string>, forward: ReturnType<typeof discover>) {
  if (start === undefined) return []
  const end = forward.order.find(id => kinds.get(id) === "done") ?? forward.order.at(-1)!
  const spine = [end]
  while (spine[0] !== start) spine.unshift(forward.parent.get(spine[0])!)
  return spine
}

/** Rows crossed by a way back between two spine states; a branch there goes left. */
function returnRows(spine: readonly string[], transitions: readonly WorkflowTransition[]) {
  const row = new Map(spine.map((id, i) => [id, i])), rows = new Set<number>()
  for (const t of transitions) {
    const from = row.get(t.from), to = row.get(t.to)
    if (from === undefined || to === undefined || to >= from) continue
    for (let r = to; r <= from; r++) rows.add(r)
  }
  return rows
}

/** Every state's row and column. Pure and stable: the same graph always places the same way. */
export function placeWorkflow(workflow: W.ProjectAssistantCharterWorkflow): Map<string, WorkflowCell> {
  const transitions = drawable(workflow), kinds = new Map(workflow.states.map(s => [s.id, s.kind]))
  const start = (workflow.states.find(s => s.kind === "start") ?? workflow.states[0])?.id
  const forward = discover(start, transitions, true), all = discover(start, transitions, false)
  const spine = spineOf(start, kinds, forward), left = returnRows(spine, transitions)
  const cells = new Map<string, WorkflowCell>(spine.map((id, row) => [id, { row, col: 0, unreached: false }]))
  const taken = new Set(spine.map((_, row) => `${row},0`))
  const claim = (id: string, row: number, col: number, step: number) => {
    while (taken.has(`${row},${col}`)) col += step
    taken.add(`${row},${col}`); cells.set(id, { row, col, unreached: false })
  }
  const branches = [...forward.order, ...all.order.filter(id => !forward.parent.has(id) && id !== start)]
  for (const id of branches) {
    if (cells.has(id)) continue
    const from = cells.get(forward.parent.get(id) ?? all.parent.get(id) ?? "")
    if (!from) continue
    if (from.col === 0) { const step = left.has(from.row) ? -1 : 1; claim(id, from.row, step, step) }
    else claim(id, from.row + 1, from.col, Math.sign(from.col))
  }
  const unreached = workflow.states.map(s => s.id).filter(id => !cells.has(id))
  let row = Math.max(-1, ...[...cells.values()].map(c => c.row)) + 1
  for (let i = 0; i < unreached.length; i += 3, row++) {
    const group = unreached.slice(i, i + 3)
    group.forEach((id, j) => cells.set(id, { row, col: j - (group.length - 1) / 2, unreached: true }))
  }
  return cells
}

/**
 * The same cells with only `keep`, emptied rows and columns closed up and the
 * centre column kept at 0: a proposal's diff draws only what changes.
 */
export function focusCells(cells: ReadonlyMap<string, WorkflowCell>, keep: ReadonlySet<string>): Map<string, WorkflowCell> {
  const kept = [...cells].filter(([id]) => keep.has(id))
  const rows = [...new Set(kept.map(([, c]) => c.row))].sort((a, b) => a - b)
  const cols = [...new Set(kept.map(([, c]) => c.col))].sort((a, b) => a - b)
  const zero = Math.max(0, cols.findIndex(c => c >= 0))
  return new Map(kept.map(([id, c]) => [id, { row: rows.indexOf(c.row), col: cols.indexOf(c.col) - zero, unreached: c.unreached }]))
}

interface Frame { colGap: number; minCol: number; left: number; top: number; unreachedRow: number }

const isDone = (state: WorkflowState) => state.kind === "done"

function boxOf(state: WorkflowState, cell: WorkflowCell, frame: Frame): PlacedWorkflowState {
  const g = workflowGeometry, done = isDone(state)
  const width = done ? g.doneWidth : g.cardWidth, height = done ? g.doneHeight : g.cardHeight
  const cx = frame.left + (cell.col - frame.minCol) * (g.cardWidth + frame.colGap) + g.cardWidth / 2
  const band = cell.row >= frame.unreachedRow ? g.unreachedHeader : 0
  const cy = frame.top + band + cell.row * g.rowPitch + g.cardHeight / 2
  return { state, row: cell.row, col: cell.col, unreached: cell.unreached, x: cx - width / 2, y: cy - height / 2, width, height }
}

const cx = (b: Box) => b.x + b.width / 2
const cy = (b: Box) => b.y + b.height / 2
const right = (b: Box) => b.x + b.width
const bottom = (b: Box) => b.y + b.height

function label(text: string, x: number, y: number, anchor: WorkflowLabel["anchor"]): WorkflowLabel | undefined {
  if (!text) return undefined
  const g = workflowGeometry, width = labelWidth(text) + 8
  const left = anchor === "start" ? x : anchor === "end" ? x - width : x - width / 2
  return { text, anchor, x: left, y: y - g.labelHeight / 2, width, height: g.labelHeight, textX: anchor === "start" ? x + 4 : anchor === "end" ? x - 4 : x, textY: y + g.labelSize * 0.36 }
}

const pathOf = (points: [number, number][]) => points.map(([x, y], i) => `${i ? "L" : "M"}${round(x)},${round(y)}`).join(" ")
const round = (n: number) => Math.round(n * 10) / 10

interface Channel { side: 1 | -1; lane: number; from: number; to: number }

/** The lanes of the up channels, each up edge in the first lane free over its rows. */
function laneFor(channels: Channel[], side: 1 | -1, from: number, to: number) {
  const lo = Math.min(from, to), hi = Math.max(from, to)
  let lane = 0
  while (channels.some(c => c.side === side && c.lane === lane && c.from <= hi && c.to >= lo)) lane++
  channels.push({ side, lane, from: lo, to: hi })
  return lane
}

/** A transition's label; inline is true when it runs straight between two cards of one row. */
export type WorkflowLabelOf = (t: WorkflowTransition, inline: boolean) => string

type Router = { placed: Map<string, PlacedWorkflowState>; frame: Frame; channels: Channel[]; labelOf: WorkflowLabelOf; pairs: Set<string> }

function routeSameRow(a: PlacedWorkflowState, b: PlacedWorkflowState, t: WorkflowTransition, r: Router): RoutedTransition {
  const dir = b.x > a.x ? 1 : -1, text = r.labelOf(t, true)
  const between = [...r.placed.values()].some(s => s.row === a.row && s !== a && s !== b && (s.x - a.x) * (s.x - b.x) < 0)
  if (between) {
    const y = Math.max(bottom(a), bottom(b)) + 20
    const points: [number, number][] = [[cx(a), bottom(a)], [cx(a), y], [cx(b), y], [cx(b), bottom(b)]]
    return { key: transitionKey(t), transition: t, points, path: pathOf(points), label: label(text, (cx(a) + cx(b)) / 2, y + 12, "middle") }
  }
  // A pair drawn both ways keeps its two lines apart: the line leaving rightwards above, leftwards below.
  const offset = r.pairs.has(transitionKey({ from: t.to, to: t.from })) ? -dir * 10 : 0
  const y = cy(a) + offset, sx = dir > 0 ? right(a) : a.x, tx = dir > 0 ? b.x : right(b)
  const points: [number, number][] = [[sx, y], [tx, y]]
  const above = offset <= 0
  return { key: transitionKey(t), transition: t, points, path: pathOf(points), label: label(text, (sx + tx) / 2, above ? y - 11 : y + 11, "middle") }
}

function routeDown(a: PlacedWorkflowState, b: PlacedWorkflowState, t: WorkflowTransition, r: Router): RoutedTransition {
  const g = workflowGeometry, text = r.labelOf(t, false), key = transitionKey(t)
  const gapTop = bottom(a), gapBottom = b.y
  const blocked = [...r.placed.values()].some(s => s !== a && s !== b && s.col === a.col && s.row > a.row && s.row < b.row)
  const sameCol = Math.abs(cx(a) - cx(b)) < 1
  if (sameCol && !blocked && b.row === a.row + 1) {
    const points: [number, number][] = [[cx(a), gapTop], [cx(b), gapBottom]]
    return { key, transition: t, points, path: pathOf(points), label: label(text, cx(a) + 6, gapTop + 14, "start") }
  }
  const side = cx(b) >= cx(a) ? 1 : -1, sx = sameCol ? cx(a) : cx(a) + side * 40
  if (b.row === a.row + 1 && !blocked) {
    const mid = gapTop + (g.rowPitch - g.cardHeight) / 2 + (a.height < g.cardHeight ? (g.cardHeight - a.height) / 2 : 0)
    const points: [number, number][] = [[sx, gapTop], [sx, mid], [cx(b), mid], [cx(b), gapBottom]]
    return { key, transition: t, points, path: pathOf(points), label: label(text, (sx + cx(b)) / 2, mid - 11, "middle") }
  }
  // Rows apart, or a card in the way: down the gutter beside the columns.
  const half = (g.rowPitch - g.cardHeight) / 2
  const gx = side > 0 ? Math.max(right(a), right(b)) + r.frame.colGap / 2 : Math.min(a.x, b.x) - r.frame.colGap / 2
  const y1 = gapTop + half / 2, y2 = gapBottom - half / 2
  const points: [number, number][] = [[sx, gapTop], [sx, y1], [gx, y1], [gx, y2], [cx(b), y2], [cx(b), gapBottom]]
  return { key, transition: t, points, path: pathOf(points), label: label(text, (sx + gx) / 2, y1 - 11, "middle") }
}

function routeUp(a: PlacedWorkflowState, b: PlacedWorkflowState, t: WorkflowTransition, r: Router): RoutedTransition {
  const g = workflowGeometry, text = r.labelOf(t, false)
  const side: 1 | -1 = Math.max(a.col, b.col) <= 0 && Math.min(a.col, b.col) < 0 ? -1 : 1
  const lane = laneFor(r.channels, side, b.row, a.row)
  const span = [...r.placed.values()].filter(s => s.row >= Math.min(a.row, b.row) && s.row <= Math.max(a.row, b.row))
  const room = Math.max(24, (text ? labelWidth(text) + 8 : 0) + 16)
  const x = side > 0 ? Math.max(...span.map(right)) + room + lane * g.lane : Math.min(...span.map(s => s.x)) - room - lane * g.lane
  const sx = side > 0 ? right(a) : a.x, tx = side > 0 ? right(b) : b.x
  const y1 = cy(a) + 10, y2 = cy(b) - 10
  const points: [number, number][] = [[sx, y1], [x, y1], [x, y2], [tx, y2]]
  const midY = (y1 + y2) / 2
  return { key: transitionKey(t), transition: t, points, path: pathOf(points), label: label(text, side > 0 ? x - 4 : x + 4, a === b ? y1 + 16 : midY, side > 0 ? "end" : "start") }
}

/** Horizontal room between columns: the widest same-row label fits with margin. */
function columnGap(cells: ReadonlyMap<string, WorkflowCell>, transitions: readonly WorkflowTransition[], labelOf: WorkflowLabelOf) {
  const widths = transitions.filter(t => t.from !== t.to && cells.get(t.from)?.row === cells.get(t.to)?.row).map(t => labelWidth(labelOf(t, true)) + 28)
  return Math.min(200, Math.max(72, ...widths))
}

/**
 * The drawing for `cells` (placeWorkflow's, or focusCells' subset): each
 * state's box and each transition's line and label, in a frame whose origin
 * is the top-left of the drawing.
 */
export function routeWorkflow(workflow: W.ProjectAssistantCharterWorkflow, cells: ReadonlyMap<string, WorkflowCell>, options: { labelOf?: WorkflowLabelOf; only?: ReadonlySet<string> } = {}): WorkflowLayout {
  const g = workflowGeometry, labelOf = options.labelOf ?? (t => t.label)
  const transitions = drawable(workflow).filter(t => cells.has(t.from) && cells.has(t.to) && (!options.only || options.only.has(transitionKey(t))))
  const start = workflow.states.find(s => s.kind === "start" && cells.has(s.id) && !cells.get(s.id)!.unreached)?.id
  const list = [...cells.values()], minCol = Math.min(0, ...list.map(c => c.col))
  const reachedRows = list.filter(c => !c.unreached).map(c => c.row)
  const unreachedRow = list.some(c => c.unreached) ? Math.max(-1, ...reachedRows) + 1 : Infinity
  const frame: Frame = { colGap: columnGap(cells, transitions, labelOf), minCol, left: 0, top: g.pad + (start ? g.startMarker : 0), unreachedRow }
  const placed = new Map(workflow.states.filter(s => cells.has(s.id)).map(s => [s.id, boxOf(s, cells.get(s.id)!, frame)]))
  const r: Router = { placed, frame, channels: [], labelOf, pairs: new Set(transitions.map(transitionKey)) }
  const routed = transitions.map(t => {
    const a = placed.get(t.from)!, b = placed.get(t.to)!
    if (a === b || b.row < a.row) return routeUp(a, b, t, r)
    return b.row === a.row ? routeSameRow(a, b, t, r) : routeDown(a, b, t, r)
  })
  return frameLayout([...placed.values()], routed, start, unreachedRow, frame.top)
}

/** Moves everything so the leftmost line, label or card starts at the padding. */
function frameLayout(states: PlacedWorkflowState[], transitions: RoutedTransition[], start: string | undefined, unreachedRow: number, top: number): WorkflowLayout {
  const g = workflowGeometry
  const xs = [...states.flatMap(s => [s.x, right(s)]), ...transitions.flatMap(t => [...t.points.map(p => p[0]), ...(t.label ? [t.label.x, right(t.label)] : [])])]
  const ys = [...states.map(bottom), ...transitions.flatMap(t => [...t.points.map(p => p[1]), ...(t.label ? [bottom(t.label)] : [])])]
  const dx = g.pad - (xs.length ? Math.min(...xs) : 0)
  const width = Math.ceil((xs.length ? Math.max(...xs) : 0) + dx + g.pad), height = Math.ceil(Math.max(top, ...ys) + g.pad)
  const moved = (p: [number, number]): [number, number] => [p[0] + dx, p[1]]
  const out = transitions.map(t => {
    const points = t.points.map(moved)
    return { ...t, points, path: pathOf(points), label: t.label && { ...t.label, x: t.label.x + dx, textX: t.label.textX + dx } }
  })
  const unreached = states.filter(s => s.row >= unreachedRow)
  return { width, height, states: states.map(s => ({ ...s, x: s.x + dx })), transitions: out, start,
    unreachedTop: unreached.length ? Math.min(...unreached.map(s => s.y)) - g.unreachedHeader + 4 : undefined }
}

/** The whole workflow, placed and routed. */
export function layoutWorkflow(workflow: W.ProjectAssistantCharterWorkflow, labelOf?: WorkflowLabelOf): WorkflowLayout {
  return routeWorkflow(workflow, placeWorkflow(workflow), { labelOf })
}

export type WorkflowChange = "added" | "removed" | "changed" | "same"

export interface WorkflowDiff {
  /** The union: the proposed states, then those it removes. */
  workflow: W.ProjectAssistantCharterWorkflow
  states: Map<string, WorkflowChange>
  transitions: Map<string, WorkflowChange>
  /** What the diff draws: changed states, both ends of each changed transition, and those transitions. */
  shownStates: Set<string>
  shownTransitions: Set<string>
}

const sameState = (a: WorkflowState, b: WorkflowState) => a.title === b.title && a.bot_id === b.bot_id && a.kind === b.kind && a.note === b.note

/** What a proposed workflow changes against the current one (null: none yet). */
export function workflowDiff(before: W.ProjectAssistantCharterWorkflow | null, after: W.ProjectAssistantCharterWorkflow | null): WorkflowDiff {
  const old = before ?? { states: [], transitions: [] }, next = after ?? { states: [], transitions: [] }
  const oldStates = new Map(old.states.map(s => [s.id, s])), newIds = new Set(next.states.map(s => s.id))
  const states = new Map<string, WorkflowChange>()
  for (const s of next.states) { const was = oldStates.get(s.id); states.set(s.id, !was ? "added" : sameState(was, s) ? "same" : "changed") }
  for (const s of old.states) if (!newIds.has(s.id)) states.set(s.id, "removed")
  const oldEdges = new Map(drawable(old).map(t => [transitionKey(t), t])), newEdges = drawable(next)
  const transitions = new Map<string, WorkflowChange>()
  for (const t of newEdges) { const was = oldEdges.get(transitionKey(t)); transitions.set(transitionKey(t), !was ? "added" : was.label === t.label && was.kind === t.kind ? "same" : "changed") }
  const removedEdges = [...oldEdges.values()].filter(t => !transitions.has(transitionKey(t)))
  for (const t of removedEdges) transitions.set(transitionKey(t), "removed")
  const workflow = { states: [...next.states, ...old.states.filter(s => !newIds.has(s.id))], transitions: [...newEdges, ...removedEdges] }
  const shownTransitions = new Set([...transitions].filter(([, c]) => c !== "same").map(([k]) => k))
  const shownStates = new Set([...states].filter(([, c]) => c !== "same").map(([id]) => id))
  for (const t of workflow.transitions) if (shownTransitions.has(transitionKey(t))) { shownStates.add(t.from); shownStates.add(t.to) }
  return { workflow, states, transitions, shownStates, shownTransitions }
}

/** The diff's drawing: the union placed as a whole, then closed up around what changes. */
export function layoutWorkflowDiff(diff: WorkflowDiff, labelOf?: WorkflowLabelOf): WorkflowLayout {
  const cells = focusCells(placeWorkflow(diff.workflow), diff.shownStates)
  return routeWorkflow(diff.workflow, cells, { labelOf, only: diff.shownTransitions })
}

/** States in reading order: the drawing's rows top to bottom, each row left to right. */
export const readingOrder = (layout: WorkflowLayout) => [...layout.states].sort((a, b) => a.row - b.row || a.x - b.x)
