import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { statusLimitCells, statusLimitRow, statusCreditCell } from "./status-limits.ts"

const responsiveStyles = readFileSync(new URL("./git-status.css", import.meta.url), "utf8")
const infoCard = readFileSync(new URL("../overlays/info.ts", import.meta.url), "utf8")

test("a Codex weekly window reaches the Status Line", () => {
  assert.deepEqual(statusLimitCells([{ name: "7d", usedPercent: 84 }], "unknown"), [
    { name: "7d", value: "84%", level: "warn" },
  ])
})

test("phones keep provider limits visible alongside Git status", () => {
  assert.match(responsiveStyles, /@media \(max-width: 520px\)[\s\S]*?footer\.status-line \.limits \{[\s\S]*?display: flex;/)
})

test("Status Line percentages are rounded, bounded and honest about unknown values", () => {
  assert.deepEqual(statusLimitCells([
    { name: "5h", usedPercent: -2 },
    { name: "7d", usedPercent: 100.4 },
    { name: "later", usedPercent: Number.NaN },
  ], "unknown"), [
    { name: "5h", value: "0%", level: "ok" },
    { name: "7d", value: "100%", level: "bad" },
    { name: "later", value: "unknown", level: "" },
  ])
})

// The Linux gap: Claude Code hands its 5h and 7d percentages to the stdin of
// whatever `statusLine.command` names and to nothing else — not the
// transcript, not a file. A machine with no status line configured therefore
// has no record, the daemon answers `no_record`, and the row used to draw
// nothing at all: the same corner an account with quota to spare draws.
const WORDS = { unknown: "unknown", limits: "Plan limits" }

test("a machine that has never had a status line says so instead of drawing nothing", () => {
  const cells = statusLimitRow({
    windows: [],
    unknownReason: "no_record",
    detail: "unknown: nothing has been written here to read; ~/.claude/statusline-cache/rate-limits.json is not there; Claude Code's status line writes it while a session runs",
  }, WORDS)
  assert.equal(cells.length, 1)
  assert.equal(cells[0].name, "Plan limits")
  assert.equal(cells[0].value, "unknown")
  assert.equal(cells[0].level, "")
  assert.match(cells[0].title ?? "", /rate-limits\.json/)
})

test("every other kind of nothing is drawn too, each carrying its own reason", () => {
  for (const reason of ["no_window", "unreadable", "stale"]) {
    const cells = statusLimitRow({ windows: [], unknownReason: reason, detail: reason + " here" }, WORDS)
    assert.equal(cells.length, 1, reason)
    assert.equal(cells[0].title, reason + " here")
  }
})

test("a daemon that has not answered yet stays blank; only a stated reason draws a cell", () => {
  assert.deepEqual(statusLimitRow(null, WORDS), [])
  assert.deepEqual(statusLimitRow(undefined, WORDS), [])
  assert.deepEqual(statusLimitRow({ windows: [] }, WORDS), [])
  assert.deepEqual(statusLimitRow({}, WORDS), [])
})

test("a reading with windows draws them and never the unknown cell", () => {
  assert.deepEqual(statusLimitRow({
    windows: [{ name: "5h", usedPercent: 96 }, { name: "7d", usedPercent: 45 }],
    unknownReason: "stale",
    detail: "an older reading",
  }, WORDS), [
    { name: "5h", value: "96%", level: "bad" },
    { name: "7d", value: "45%", level: "ok" },
  ])
})

// Not covered: the Session info card passes the daemon's own reason on instead of saying only
// unknown. What stood here read the component's source; asserting it needs a DOM runner.


test("an account quota reading names its scope and visibly marks historical data", () => {
  assert.deepEqual(statusLimitRow({ windows: [{name:"7d",usedPercent:26}],stale:true },
    {...WORDS,stale:"Outdated",source:"This machine's Grok account"}),
    [{name:"7d",value:"26% · Outdated",level:"ok",title:"This machine's Grok account"}])
  assert.deepEqual(statusLimitRow({ windows: [{name:"7d",usedPercent:0}],stale:false },
    {...WORDS,stale:"Outdated",source:"Grok"}),
    [{name:"7d",value:"0%",level:"ok",title:"Grok"}])
})

const creditWords = { name: "Credits", inUse: "In use", balance: (b: string) => `Balance ${b}`, explain: "Each turn is charged to your credits." }

test("a spent window whose turns are charged to credits says so beside the window", () => {
  const reading = { windows: [{ name: "7d", usedPercent: 100 }], creditsInUse: true, creditBalance: 44718.83 }
  assert.deepEqual(statusLimitRow(reading, { unknown: "unknown", limits: "Limits", credits: creditWords }, "en-US"), [
    { name: "7d", value: "100%", level: "bad" },
    { name: "Credits", value: "In use · 44,719", level: "bad", title: "Each turn is charged to your credits. Balance 44,719", kind: "credits" },
  ])
})

test("credits are not shown as in use unless the daemon says so", () => {
  assert.equal(statusCreditCell({ windows: [{ name: "7d", usedPercent: 100 }] }, creditWords), null)
  assert.equal(statusCreditCell({ windows: [{ name: "7d", usedPercent: 100 }], creditsInUse: true }, undefined), null)
  assert.deepEqual(statusCreditCell({ creditsInUse: true }, creditWords),
    { name: "Credits", value: "In use", level: "bad", title: "Each turn is charged to your credits.", kind: "credits" })
})

test("phones keep the credits word and leave its balance to the info card", () => {
  assert.match(responsiveStyles, /@media \(max-width: 520px\)[\s\S]*?\.limit\[data-kind="credits"\] b \{ display: none; \}/)
})
