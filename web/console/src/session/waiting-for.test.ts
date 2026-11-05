// What a waiting Session waits for: `node --test web/console/src/session/waiting-for.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { waitingForWords, runningShellCount } from "./waiting-for.ts"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { nextWord } from "../next-strings.ts"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { withCatalog, inCatalog } from "../catalog-testing.ts"

withCatalog("zh-Hant")

test("a Session waiting on heavy commands names each command and its place at the compile slot", () => {
  const said = waitingForWords({ waiting_for: [
    { task_id: "a", kind: "heavy", label: "go test ./...", phase: "queued" },
    { task_id: "b", kind: "heavy", label: "npm run build" },
  ] }, nextWord)
  assert.equal(said.text, "等待：go test ./...（排隊中） · npm run build")
  assert.equal(said.covered, false)
})

test("a Session waiting only on dispatched children names them as Sessions", () => {
  const said = waitingForWords({ waiting_for: [{ task_id: "a", kind: "session", label: "Fix the list" }], waiting_for_omitted: 2 }, nextWord)
  assert.equal(said.text, "等待 Session：Fix the list  +2")
})

test("the running heavy command its 🏗️ marker already shows is not said twice", () => {
  const row = { heavy_work: { task_id: "a", reason: "go test ./..." },
    waiting_for: [{ task_id: "a", kind: "heavy", label: "go test ./...", phase: "running" }] }
  assert.deepEqual(waitingForWords(row, nextWord), { text: "", title: "", covered: true })
  // Control: the same wait without the marker is said.
  assert.equal(waitingForWords({ waiting_for: row.waiting_for }, nextWord).text, "等待：go test ./...（執行中）")
})

test("a row with nothing named says nothing, and the list falls back to its generic wait", () => {
  assert.deepEqual(waitingForWords({}, nextWord), { text: "", title: "", covered: false })
})

for (const state of ["working", "unknown", "waiting", "idle"] as const) {
  test(`a ${state} terminal still names its queued broker command`, () => {
    const row = { state, waiting_for: [{ task_id: "q", kind: "heavy", label: "go test ./...", phase: "queued" }] }
    assert.equal(waitingForWords(row, nextWord).text, "等待：go test ./...（排隊中）")
  })
}

test("queued callbacks do not count as running shells; ordinary shells remain counted", () => {
  const queued = { id: "q", at: 1, state: "queued", kind: "callback" }
  assert.equal(runningShellCount({ shells: [queued] }), 0)
  assert.equal(runningShellCount({ shells: [queued, { id: "s", at: 1 }, { ...queued, id: "r", state: "running" }] }), 2)
  assert.equal(runningShellCount({ shells: [{ ...queued, state: "running" }] }), 1)
})

for (const language of ["en", "zh-Hant"] as const) {
  test(`a live callback is named explicitly in ${language} even without waiting_for`, () => {
    inCatalog(language, () => {
      const shells = [{ id: "callback-ci", kind: "callback", state: "running", at: 1, what: "Wait for CI" }]
      const said = waitingForWords({ shells }, nextWord)
      assert.equal(said.text, language === "en" ? "Waiting for callback: Wait for CI" : "等待回呼：Wait for CI")
      assert.equal(said.title, "Wait for CI")
      assert.deepEqual(waitingForWords({ shells: [] }, nextWord), { text: "", title: "", covered: false })
    })
  })
}

test("callback sources are deduplicated and terminal callbacks and ordinary shells are excluded", () => {
  const active = { id: "callback-ci", kind: "callback", state: "running", at: 1, what: "Wait for CI" }
  const waiting_for = [{ task_id: "ci", kind: "callback", label: "Wait for CI" }]
  assert.equal(waitingForWords({ waiting_for, shells: [active] }, nextWord).text, "等待回呼：Wait for CI")
  for (const state of ["success", "failure", "timeout", "cancelled", undefined]) {
    assert.equal(waitingForWords({ shells: [{ ...active, state }] }, nextWord).text, "")
  }
  assert.equal(waitingForWords({ shells: [{ ...active, kind: undefined }] }, nextWord).text, "")
  assert.equal(waitingForWords({ heavy_work: { task_id: "ci", reason: "build" }, shells: [active] }, nextWord).text, "")
})

test("a queued callback names its phase without implying that its command has started", () => {
  const said = waitingForWords({ shells: [{ id: "callback-q", kind: "callback", state: "queued", at: 1, command: "go build ./..." }] }, nextWord)
  assert.equal(said.text, "等待回呼：go build ./...（排隊中）")
})
