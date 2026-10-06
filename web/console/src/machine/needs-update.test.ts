import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's strip-types test runner.
import { asMachineNeedsUpdate, machineNeedsUpdate, makeJSONFetch, RefusalError, MACHINE_NEEDS_UPDATE_CODES } from "../../../core/src/refusal.ts"
// @ts-expect-error -- a `.ts` path, for Node's strip-types test runner.
import { machineVersionFromHealth, needsUpdateWords, sameMachineVersion, SETTINGS_UPDATE_HREF } from "./needs-update-model.ts"
// @ts-expect-error -- a `.ts` path, for Node's strip-types test runner.
import { pageNeedsUpdate } from "../pages/registry.ts"
// @ts-expect-error -- a `.ts` path, for Node's strip-types test runner.
import { RelayReader, type CloudReadClient } from "../cloud/relay-reader.ts"
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { nextWord } from "../next-strings.ts"

// The daemon's answer to a route it has no handler for (`writeNoSuchRoute`,
// internal/transport/http/write.go): 501, `not_implemented`, the route named.
const unknownRoute = { error: "not_implemented", detail: "this daemon does not own that route yet", route: "/v1/work/v2/sessions/s1" }

test("a 501 not_implemented that names its route is a machine that needs an update", () => {
  assert.equal(machineNeedsUpdate(501, "not_implemented", "/v1/work/v2/x"), true)
  const error = new RefusalError(501, unknownRoute, "/v1/work/v2/sessions/s1")
  assert.equal(error.machineNeedsUpdate, true)
  assert.deepEqual(asMachineNeedsUpdate(error), { kind: "machine_needs_update", code: "not_implemented", route: "/v1/work/v2/sessions/s1" })
  // The wire code stays the code: callers that branch on it keep working.
  assert.equal(error.code, "not_implemented")
})

test("501 without a route, or not_implemented under another status, is not guessed", () => {
  assert.equal(machineNeedsUpdate(501, "not_implemented", undefined), false)
  assert.equal(machineNeedsUpdate(501, "not_implemented", ""), false)
  assert.equal(machineNeedsUpdate(500, "not_implemented", "/v1/x"), false)
  // The path asked is not the daemon naming a route: only the body's route counts.
  assert.equal(new RefusalError(501, { error: "not_implemented", detail: "x" }, "/v1/x").machineNeedsUpdate, false)
})

test("a 404 not_found stays not found: an older daemon's unknown sub-route and a missing record are the same answer", () => {
  const older = new RefusalError(404, { error: "not_found", detail: "No such work route.", route: "/v1/work/v2/x" }, "/v1/work/v2/x")
  assert.equal(older.machineNeedsUpdate, false)
  assert.equal(asMachineNeedsUpdate(older), null)
  assert.equal(machineNeedsUpdate(404, "not_found", "/v1/x"), false)
  assert.equal(machineNeedsUpdate(404, "item_not_found", "/v1/x"), false)
})

test("cloud_not_carried is this console's fact, not the machine's", () => {
  assert.equal(machineNeedsUpdate(501, "cloud_not_carried", "/v1/x"), false)
  assert.equal(asMachineNeedsUpdate(new RefusalError(501, { error: "cloud_not_carried", detail: "x", route: "/v1/x" })), null)
})

test("the copied Cloud client's three words for a word the machine lacks all need an update, at any status", () => {
  assert.deepEqual([...MACHINE_NEEDS_UPDATE_CODES].sort(), ["cloud_feature_unavailable", "cloud_machine_unsupported", "unknown_command"])
  for (const code of ["unknown_command", "cloud_feature_unavailable", "cloud_machine_unsupported"]) {
    assert.equal(machineNeedsUpdate(502, code, undefined), true, code)
    const error = new RefusalError(502, { error: code, detail: "x", route: "/v1/machine/usage" })
    assert.equal(asMachineNeedsUpdate(error)?.code, code)
  }
  for (const code of ["forbidden", "store_busy", "cloud_reconnecting", "machine_offline"]) {
    assert.equal(machineNeedsUpdate(503, code, "/v1/x"), false, code)
  }
})

test("anything else thrown is read duck-typed, and a value with no code is not a verdict", () => {
  assert.equal(asMachineNeedsUpdate(null), null)
  assert.equal(asMachineNeedsUpdate(new Error("boom")), null)
  assert.equal(asMachineNeedsUpdate("unknown_command"), null)
  assert.equal(asMachineNeedsUpdate({ code: "unknown_command" })?.kind, "machine_needs_update")
  assert.equal(asMachineNeedsUpdate({ code: "not_implemented", status: 501, route: "/v1/x", version: "1.2.3" })?.version, "1.2.3")
  assert.equal(asMachineNeedsUpdate({ code: "not_implemented", status: 501 }), null)
})

test("makeJSONFetch marks the copied pages' Error, keeps its code, and uses the host's sentence when it has one", async () => {
  const realFetch = globalThis.fetch
  try {
    let body: unknown = unknownRoute
    let status = 501
    globalThis.fetch = (async () => new Response(JSON.stringify(body), { status })) as typeof fetch
    const words = { offline: "off", requestFailed: "failed", notJSON: "json" }
    const plain = makeJSONFetch({ words })
    const said = makeJSONFetch({ words: { ...words, machineNeedsUpdate: "needs update" } })

    const a = await plain("/v1/work/v2/sessions/s1").catch((e: unknown) => e) as Error & Record<string, unknown>
    assert.equal(a.code, "not_implemented")
    assert.equal(a.machineNeedsUpdate, true)
    assert.equal(a.route, "/v1/work/v2/sessions/s1")
    assert.equal(a.message, unknownRoute.detail)
    assert.equal(asMachineNeedsUpdate(a)?.route, "/v1/work/v2/sessions/s1")

    const b = await said("/v1/x").catch((e: unknown) => e) as Error & Record<string, unknown>
    assert.equal(b.message, "needs update")

    body = { error: "not_found", detail: "No such work route.", route: "/v1/x" }
    status = 404
    const c = await said("/v1/x").catch((e: unknown) => e) as Error & Record<string, unknown>
    assert.equal(c.machineNeedsUpdate, undefined)
    assert.equal(c.message, "No such work route.")
    assert.equal(asMachineNeedsUpdate(c), null)
  } finally {
    globalThis.fetch = realFetch
  }
})

/** A copied client whose machine does not know the word, with a descriptor that names its version. */
function oldMachine(app: Record<string, unknown> | undefined): CloudReadClient {
  return {
    ready: true,
    events: () => () => {},
    sessions: async () => ({ sessions: [], at: 0, scan: {} }),
    transcript: async () => ({}),
    machines: async () => ({ machines: [{ id: "m1", freshness: "current" }], syncing: false, retryAfterMs: 0 }),
    orchestratorSnapshots: new Map(app ? [["m1", { app }]] : []),
    _machineRequest: async (_machine: string, word: string) => {
      const failure = new Error("this machine does not know " + word) as Error & { code: string }
      failure.code = "unknown_command"
      throw failure
    },
  }
}

async function relayAnswer(client: CloudReadClient, path: string): Promise<Response> {
  const reader = new RelayReader("m1")
  reader.attach(client)
  return reader.fetch(path)
}

test("over Cloud, a machine that lacks the word becomes a RefusalError that needs an update, with its version", async () => {
  const response = await relayAnswer(oldMachine({ version: "1.2.3", api_level: 1 }), "/v1/personas")
  assert.equal(response.ok, false)
  const body = await response.json()
  assert.equal(body.error, "unknown_command")
  assert.equal(body.version, "1.2.3")
  const error = new RefusalError(response.status, body, "/v1/personas")
  assert.equal(error.code, "unknown_command")
  assert.deepEqual(asMachineNeedsUpdate(error), { kind: "machine_needs_update", code: "unknown_command", route: "/v1/personas", version: "1.2.3" })
})

test("over Cloud, health carries the descriptor's version and level only when the machine said them", async () => {
  const told = await (await relayAnswer(oldMachine({ version: "1.2.3", api_level: 2, build: "x" }), "/v1/health")).json()
  assert.equal(told.version, "1.2.3")
  assert.equal(told.api_level, 2)
  assert.equal(told.served_by, "cloud-machine-via-relay")
  for (const app of [undefined, { build: "x" }, { version: "", api_level: "2" }]) {
    const silent = await (await relayAnswer(oldMachine(app), "/v1/health")).json()
    assert.equal(silent.ok, true)
    assert.equal("version" in silent, false, JSON.stringify(app))
    assert.equal("api_level" in silent, false, JSON.stringify(app))
  }
})

test("health's version and level are read only when present; absent is unknown, not zero", () => {
  assert.deepEqual(machineVersionFromHealth({ ok: true, version: "1.2.3", api_level: 1 }), { version: "1.2.3", apiLevel: 1 })
  assert.deepEqual(machineVersionFromHealth({ ok: true }), {})
  assert.deepEqual(machineVersionFromHealth({ version: " ", api_level: "1" }), {})
  assert.deepEqual(machineVersionFromHealth(null), {})
  assert.equal(sameMachineVersion({ version: "1" }, { version: "1" }), true)
  assert.equal(sameMachineVersion({ version: "1" }, { version: "1", apiLevel: 1 }), false)
})

test("the needs-update words, with and without a version, link to Settings and name the machine as a machine", () => {
  const without = needsUpdateWords({}, {}, nextWord)
  assert.equal(without.version, null)
  assert.equal(without.href, SETTINGS_UPDATE_HREF)
  assert.equal(SETTINGS_UPDATE_HREF, "#page=settings")
  assert.match(without.sentence, /Update it to use this|更新後就能使用/)
  assert.doesNotMatch(without.sentence + without.link, /\bMac\b/)

  const fromHealth = needsUpdateWords(null, { version: "v1.2.3" }, nextWord)
  assert.match(fromHealth.version!, /v1\.2\.3/)
  // The refusal's own version is the machine that refused; it wins over health's.
  const fromRefusal = needsUpdateWords({ version: "1.0.0" }, { version: "v1.2.3" }, nextWord)
  assert.match(fromRefusal.version!, /1\.0\.0/)
  assert.doesNotMatch(fromRefusal.version!, /1\.2\.3/)
})

test("requiresApiLevel withholds a page only from a machine whose level is known and lower", () => {
  const modules = {
    newer: { id: "newer", requiresApiLevel: 2, Component: (() => null) as never },
    plain: { id: "plain", Component: (() => null) as never },
  }
  assert.equal(pageNeedsUpdate("newer", modules, 1), true, "known and lower: disabled")
  assert.equal(pageNeedsUpdate("newer", modules, 0), true)
  assert.equal(pageNeedsUpdate("newer", modules, undefined), false, "unknown: offered, and the 501 speaks")
  assert.equal(pageNeedsUpdate("newer", modules, 2), false, "equal: offered")
  assert.equal(pageNeedsUpdate("newer", modules, 3), false, "higher: offered")
  assert.equal(pageNeedsUpdate("plain", modules, 0), false, "no requirement: offered")
  assert.equal(pageNeedsUpdate("absent", modules, 0), false)
})
