import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error -- a `.ts` path, for node's strip-types runner.
import { statusLimitCells, statusLimitRow } from "./status-limits.ts"

const responsiveStyles = readFileSync(new URL("./git-status.css", import.meta.url), "utf8")
const infoCard = readFileSync(new URL("../overlays/info.ts", import.meta.url), "utf8")

test("a Codex weekly window reaches the Status Line", () => {
  assert.deepEqual(statusLimitCells([{ name: "7d", usedPercent: 84 }], "unknown"), [
    { name: "7d", value: "84%", level: "warn" },
  ])
})

test("phones keep provider limits visible alongside Git status", () => {
  assert.match(responsiveStyles, /@media \(max-width: 520px\)[\s\S]*?footer\.status-line \.limits \{[\s\S]*?display: flex;/)
  assert.doesNotMatch(responsiveStyles, /footer\.status-line \.limits \{\s*display: none;/)
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

test("the Session info card passes the daemon's reason on rather than saying only 'unknown'", () => {
  // The status-line cell is one word wide; the card is where the sentence
  // naming the file has to land, because that is the only place a person
  // finds out a status line is what writes it.
  assert.match(infoCard, /if \(!limits\.windows\.length\) return note\(limits\.detail \|\| T\.webInfoUnknown\)/)
})
