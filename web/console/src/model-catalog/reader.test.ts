import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { ModelCatalogReader, MODEL_CATALOG_MAX_POLLS } from "./reader.ts"

type Agent = { agent: string; models: { value: string; name: string }[]; default_name: string; efforts: string[]; source: string; observed_at: string; refreshing: boolean; problem?: string }
const agent = (name: string, refreshing: boolean, models: string[] = []): Agent =>
  ({ agent: name, models: models.map(value => ({ value, name: value })), default_name: "", efforts: [], source: refreshing ? "native" : "agent", observed_at: "", refreshing })

function harness(answers: (() => Promise<unknown>)[], canModelCatalog = true) {
  const timers: (() => void)[] = []
  let changes = 0, asked = 0
  const source = {
    machineID: "m1",
    canModelCatalog: () => canModelCatalog,
    modelCatalog: () => { const next = answers[Math.min(asked, answers.length - 1)]; asked++; return next() },
  }
  const reader = new ModelCatalogReader(source as never, () => { changes++ }, { set: run => { timers.push(run); return timers.length }, clear: () => {} })
  return { reader, timers, asked: () => asked, changes: () => changes }
}
const settle = () => new Promise(r => setTimeout(r, 0))

test("loading until the first answer, then ready", async () => {
  const h = harness([async () => ({ agents: [agent("codex", false, ["model-sol"])] })])
  assert.deepEqual(h.reader.get("codex"), { status: "loading" })
  h.reader.start()
  await settle()
  const got = h.reader.get("codex")
  assert.equal(got.status, "ready")
  assert.equal(got.status === "ready" && got.waiting, false)
  assert.equal(h.timers.length, 0, "nothing is refreshing, so nothing polls")
})

test("a refreshing answer polls until the agent has answered", async () => {
  const h = harness([async () => ({ agents: [agent("codex", true, ["model-sol"])] }), async () => ({ agents: [agent("codex", false, ["model-sol", "model-astra"])] })])
  h.reader.start()
  await settle()
  const first = h.reader.get("codex")
  assert.ok(first.status === "ready" && first.waiting && first.catalog.models.length === 1)
  assert.equal(h.timers.length, 1)
  h.timers.shift()!()
  await settle()
  const next = h.reader.get("codex")
  assert.ok(next.status === "ready" && !next.waiting && next.catalog.models.length === 2)
  assert.equal(h.timers.length, 0)
})

test("polling stops after its bound and the list stops waiting", async () => {
  const h = harness([async () => ({ agents: [agent("grok", true)] })])
  h.reader.start()
  await settle()
  for (let i = 0; i < MODEL_CATALOG_MAX_POLLS; i++) { h.timers.shift()!(); await settle() }
  assert.equal(h.timers.length, 0)
  const got = h.reader.get("grok")
  assert.ok(got.status === "ready" && !got.waiting, "never an endless spinner")
})

test("a failure says so, and retry reads again", async () => {
  const h = harness([async () => { throw { error: { code: "pa_offline" } } }, async () => ({ agents: [agent("codex", false, ["x"])] })])
  h.reader.start()
  await settle()
  assert.deepEqual(h.reader.get("codex"), { status: "failed", code: "pa_offline" })
  h.reader.retry()
  assert.deepEqual(h.reader.get("codex"), { status: "loading" })
  await settle()
  assert.equal(h.reader.get("codex").status, "ready")
  assert.equal(h.asked(), 2)
})

test("an older daemon, or a relay that does not carry the word, is unsupported", async () => {
  const old = harness([async () => ({ agents: [] })], false)
  old.reader.start()
  await settle()
  assert.deepEqual(old.reader.get("codex"), { status: "unsupported" })
  assert.equal(old.asked(), 0, "nothing is asked of a daemon that cannot answer")
  const relay = harness([async () => { throw { error: { code: "cloud_not_carried" } } }])
  relay.reader.start()
  await settle()
  assert.deepEqual(relay.reader.get("codex"), { status: "unsupported" })
})

test("a stopped reader ignores a late answer", async () => {
  let resolve!: (v: unknown) => void
  const h = harness([() => new Promise(r => { resolve = r })])
  h.reader.start()
  h.reader.stop()
  resolve({ agents: [agent("codex", true, ["x"])] })
  await settle()
  assert.deepEqual(h.reader.get("codex"), { status: "loading" })
  assert.equal(h.timers.length, 0)
})
