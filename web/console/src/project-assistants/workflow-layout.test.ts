import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import { sampleWorkflow, sampleWorkflowProposal } from "./charter-fixture.ts"
// @ts-expect-error Node executes source extensions.
import { labelWidth, layoutWorkflow, layoutWorkflowDiff, placeWorkflow, readingOrder, workflowDiff, workflowGeometry } from "./workflow-layout.ts"

type Box = { x: number; y: number; width: number; height: number }
type Layout = ReturnType<typeof layoutWorkflow>

const team = { atlas: "atlas", xavia: "xavia", fuse: "fuse", lens: "lens", beacon: "beacon" }
const sample = () => sampleWorkflow(team)
const state = (id: string, kind = "step") => ({ id, title: id, bot_id: "", kind, note: "" })
const go = (from: string, to: string, label = "", kind = "normal") => ({ from, to, label, kind })
const overlaps = (a: Box, b: Box) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height
const byId = (layout: Layout, id: string) => layout.states.find((s: { state: { id: string } }) => s.state.id === id)!
const edge = (layout: Layout, key: string) => layout.transitions.find((t: { key: string }) => t.key === key)!

/** Every label clear of every card and of every other label, and inside the drawing. */
function assertLabelsClear(layout: Layout) {
  const labels = layout.transitions.flatMap((t: { label?: Box & { text: string } }) => t.label ? [t.label] : [])
  for (const l of labels) {
    for (const s of layout.states) assert.ok(!overlaps(l, s), `label ${l.text} covers ${s.state.id}: ${JSON.stringify([l, { x: s.x, y: s.y, w: s.width, h: s.height }])}`)
    assert.ok(l.x >= 0 && l.x + l.width <= layout.width && l.y >= 0 && l.y + l.height <= layout.height, `label ${l.text} outside the drawing`)
  }
  labels.forEach((a: Box & { text: string }, i: number) => labels.slice(i + 1).forEach((b: Box & { text: string }) => assert.ok(!overlaps(a, b), `labels ${a.text} and ${b.text} overlap`)))
}

test("the spine runs top-down from the start to the nearest done state, one row each", () => {
  const cells = placeWorkflow(sample())
  const spine = ["card", "review", "integrate", "visual", "deploy", "done"]
  assert.deepEqual(spine.map(id => [cells.get(id)!.row, cells.get(id)!.col]), spine.map((_, row) => [row, 0]))
  const layout = layoutWorkflow(sample())
  const centres = spine.map(id => { const s = byId(layout, id); return s.x + s.width / 2 })
  assert.ok(centres.every(x => x === centres[0]), JSON.stringify(centres))
  const tops = spine.map(id => byId(layout, id).y + byId(layout, id).height / 2)
  tops.slice(1).forEach((y, i) => assert.equal(y - tops[i], workflowGeometry.rowPitch))
  assert.equal(layout.start, "card")
})

test("a side branch sits beside the state that reaches it, left where a way back crosses its row", () => {
  const cells = placeWorkflow(sample())
  // 可靠性審查 is reached from 派工・審證據, whose row the return from 畫面驗收 crosses.
  assert.deepEqual(cells.get("reliability"), { row: 1, col: -1, unreached: false })
  // 更新官網與 README is reached from 部署 Cloud, below every way back.
  assert.deepEqual(cells.get("website"), { row: 4, col: 1, unreached: false })
  const layout = layoutWorkflow(sample())
  const into = edge(layout, "review->reliability"), back = edge(layout, "reliability->integrate")
  const review = byId(layout, "review"), reliability = byId(layout, "reliability"), integrate = byId(layout, "integrate")
  assert.deepEqual(into.points, [[review.x, review.y + 32], [reliability.x + reliability.width, reliability.y + 32]])
  assert.deepEqual(back.points.at(-1), [integrate.x + integrate.width / 2, integrate.y])
  assert.equal(into.label?.text, "動到失敗處理")
  assert.ok(into.label!.x >= reliability.x + reliability.width && into.label!.x + into.label!.width <= review.x, "the label sits in the gap between the two cards")
})

test("a way back runs up a channel right of every card it passes", () => {
  const layout = layoutWorkflow(sample())
  const back = edge(layout, "visual->review"), visual = byId(layout, "visual"), review = byId(layout, "review")
  const [first, , , last] = back.points
  assert.equal(back.points.length, 4)
  assert.deepEqual(first, [visual.x + visual.width, visual.y + 42])
  assert.deepEqual(last, [review.x + review.width, review.y + 22])
  const channel = back.points[1][0]
  const passed = layout.states.filter((s: { row: number }) => s.row >= 1 && s.row <= 3)
  assert.ok(passed.every((s: Box) => s.x + s.width < channel), "the channel is right of the rows it spans")
  assert.equal(back.label?.text, "不通過")
  assert.ok(back.label!.x + back.label!.width <= channel, "the label is left of its channel")
})

test("labels are beside or above their lines and never over a card or another label", () => {
  assertLabelsClear(layoutWorkflow(sample()))
  // Longer labels, as a page writes them with the Bot sent back to.
  assertLabelsClear(layoutWorkflow(sample(), (t: { label: string; kind: string }, inline: boolean) => t.kind === "return" && !inline ? `${t.label}・直接退回 Atlas` : t.label))
  assertLabelsClear(layoutWorkflow(sampleWorkflowProposal(sample())))
})

test("an unreachable state is placed in a bottom row marked unreached instead of failing", () => {
  const flow = { states: [state("a", "start"), state("b"), state("z", "done"), state("lost"), state("lost2")], transitions: [go("a", "b"), go("b", "z"), go("lost", "z"), go("nowhere", "a")] }
  const cells = placeWorkflow(flow)
  assert.deepEqual(cells.get("lost"), { row: 3, col: -0.5, unreached: true })
  assert.deepEqual(cells.get("lost2"), { row: 3, col: 0.5, unreached: true })
  const layout = layoutWorkflow(flow)
  const lost = byId(layout, "lost"), done = byId(layout, "z")
  assert.ok(layout.unreachedTop !== undefined && layout.unreachedTop > done.y + done.height && layout.unreachedTop < lost.y)
  // A transition to a state that does not exist is not drawn.
  assert.deepEqual(layout.transitions.map((t: { key: string }) => t.key).sort(), ["a->b", "b->z", "lost->z"])
  assert.ok(layout.states.every((s: Box) => s.y + s.height <= layout.height))
  // No start state, no states: still a drawing.
  assert.equal(layoutWorkflow({ states: [], transitions: [] }).states.length, 0)
  assert.equal(placeWorkflow({ states: [state("x"), state("y")], transitions: [go("x", "y"), go("y", "x", "", "return")] }).get("y")!.row, 1)
})

test("the layout is stable: the same graph always draws the same, whatever runs before it", () => {
  const a = JSON.stringify(layoutWorkflow(sample()))
  layoutWorkflow(sampleWorkflowProposal(sample()))
  assert.equal(JSON.stringify(layoutWorkflow(sample())), a)
  assert.equal(JSON.stringify(layoutWorkflow(structuredClone(sample()))), a)
})

test("several ways back over the same rows each get their own lane", () => {
  const flow = { states: [state("a", "start"), state("b"), state("c"), state("d", "done")],
    transitions: [go("a", "b"), go("b", "c"), go("c", "d"), go("c", "a", "重來", "return"), go("c", "b", "再試", "return"), go("b", "b", "等一下")] }
  const layout = layoutWorkflow(flow)
  const lanes = ["c->a", "c->b"].map(k => edge(layout, k).points[1][0])
  assert.notEqual(lanes[0], lanes[1])
  const self = edge(layout, "b->b"), b = byId(layout, "b")
  assert.ok(self.points[1][0] > b.x + b.width, "a step back to itself loops on the right")
  assertLabelsClear(layout)
})

test("a proposal's diff draws only the changed transitions and the states they touch", () => {
  const before = sample(), after = sampleWorkflowProposal(before)
  const diff = workflowDiff(before, after)
  assert.deepEqual([...diff.transitions].filter(([, c]) => c !== "same"), [["reliability->review", "added"]])
  assert.deepEqual([...diff.shownStates].sort(), ["reliability", "review"])
  const layout = layoutWorkflowDiff(diff)
  assert.deepEqual(layout.states.map((s: { state: { id: string } }) => s.state.id).sort(), ["reliability", "review"])
  assert.deepEqual(layout.transitions.map((t: { key: string }) => t.key), ["reliability->review"])
  // Only one row is left, and it is the first.
  assert.deepEqual(layout.states.map((s: { row: number }) => s.row), [0, 0])
  assert.equal(layout.start, undefined)
  assertLabelsClear(layout)
})

test("a diff names added, removed and changed states and transitions", () => {
  const before = sample()
  const after = structuredClone(before)
  after.states = after.states.filter((s: { id: string }) => s.id !== "website")
  after.transitions = after.transitions.filter((t: { from: string; to: string }) => t.from !== "website" && t.to !== "website")
  after.states.find((s: { id: string }) => s.id === "visual")!.title = "畫面與文案驗收"
  after.states.push(state("notify"))
  after.transitions.push(go("deploy", "notify", "對外看得到"), go("notify", "done"))
  after.transitions.find((t: { from: string; to: string }) => t.from === "deploy" && t.to === "done")!.label = "內部"
  const diff = workflowDiff(before, after)
  assert.equal(diff.states.get("website"), "removed")
  assert.equal(diff.states.get("notify"), "added")
  assert.equal(diff.states.get("visual"), "changed")
  assert.equal(diff.states.get("review"), "same")
  assert.equal(diff.transitions.get("deploy->website"), "removed")
  assert.equal(diff.transitions.get("deploy->notify"), "added")
  assert.equal(diff.transitions.get("deploy->done"), "changed")
  assert.ok(diff.workflow.states.some((s: { id: string }) => s.id === "website"), "the union keeps a removed state to draw it struck")
  const layout = layoutWorkflowDiff(diff)
  assert.ok(!layout.states.some((s: { state: { id: string } }) => s.state.id === "card"), "an untouched state is left out")
  assertLabelsClear(layout)
  // A first workflow is all added.
  const first = workflowDiff(null, before)
  assert.ok([...first.states.values()].every(c => c === "added"))
  assert.equal(layoutWorkflowDiff(first).states.length, before.states.length)
})

test("reading order is row by row, left to right, as the off-screen list reads it", () => {
  const order = readingOrder(layoutWorkflow(sample())).map((s: { state: { id: string } }) => s.state.id)
  assert.deepEqual(order, ["card", "reliability", "review", "integrate", "visual", "deploy", "website", "done"])
})

test("label widths count a CJK character as a full em", () => {
  assert.equal(labelWidth("不通過"), 36)
  assert.equal(labelWidth("ok"), 15)
})
