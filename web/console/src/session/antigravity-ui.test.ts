import assert from "node:assert/strict"
import test from "node:test"
import { makeJSONFetch } from "@clawdline/core/refusal"
import english from "../../public/catalogs/en.json" with { type: "json" }
import chinese from "../../public/catalogs/zh-Hant.json" with { type: "json" }
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogRefusalDetail } from "../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { withCatalog } from "../catalog-testing.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { startPath, resumePath } from "./place-routes.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { writeRoute } from "../cloud/relay-writer.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { STATUS_FRESH_MS, statusProjection } from "../cloud/status-projection.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { pinnedQuestion } from "../cloud/status-source.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { antigravitySlashDraft, assistantAdmits, assistantComposeAdmits, assistantMenuAdmits, assistantStateCurrent, assistantLabel, assistantRolesAdmit, assistantUnavailable, isAssistant, type AssistantFacts } from "./antigravity-ui.ts"

const active: AssistantFacts = { id: "antigravity", installed: true, authState: "ready", integrationState: "active",
  capabilities: { start: true, resume: true, send: true, menus: true, transcript: true } }

test("an exact current native permission menu admits the ordinary waiting buttons", () => {
  const row = { id: "terminal-fixture", backend: "tmux", assistant: "antigravity", identity: "launch_hook",
    sessionId: "native-exact", execution_generation: "a".repeat(32), state: "waiting", line: "Run synthetic command?",
    source: { freshness: "current", observed_at: Date.now() / 1000 },
    menu: { revision: "offer-1", question: "Run synthetic command?", options: [{ n: 1, label: "Allow once", can: true }] } } as any
  assert.equal(assistantMenuAdmits(row, active), true)
  assert.equal(assistantMenuAdmits(row, { ...active, capabilities: { ...active.capabilities!, menus: false } }), false)
})

withCatalog("zh-Hant")

test("a found CLI never grants Antigravity operations without readiness evidence", () => {
  for (const facts of [{ id: "antigravity" }, { id: "antigravity", installed: true },
    { ...active, capabilities: undefined }, { ...active, authState: undefined },
    { ...active, integrationState: undefined }] as AssistantFacts[]) {
    for (const operation of ["start", "resume", "send", "menus", "transcript"] as const) {
      assert.equal(assistantAdmits(facts, operation), (operation === "start" || operation === "resume") && facts.authState === undefined && facts.integrationState === "active" && facts.capabilities?.[operation] === true)
    }
  }
})

test("each operation needs its own explicit permission and a usable installation", () => {
  for (const operation of ["start", "resume", "send", "menus", "transcript"] as const) {
    assert.equal(assistantAdmits(active, operation), true)
    assert.equal(assistantAdmits({ ...active, capabilities: { ...active.capabilities!, [operation]: false } }, operation), false)
    assert.equal(assistantAdmits({ ...active, installed: false }, operation), false)
  }
})

test("unknown auth, disabled integration, missing CLI and unsupported account remain distinct", () => {
  for (const [patch, reason] of [
    [{ installed: false }, "missing"], [{ integrationState: "missing" }, "integrationMissing"],
    [{ integrationState: "disabled" }, "disabled"], [{ integrationState: "incompatible" }, "incompatible"],
    [{ authState: "required", capabilities: undefined }, "required"], [{ authState: "unknown", capabilities: undefined }, "unknownAuth"],
    [{ authState: "unsupported_account" }, "unsupported"],
  ] as const) {
    assert.equal(assistantUnavailable({ ...active, ...patch }, "start"), reason)
  }
})

test("old daemons preserve original provider behavior without inventing a third-provider capability", () => {
  assert.equal(assistantAdmits({ id: "claude" }, "resume"), true)
  assert.equal(assistantAdmits({ id: "codex" }, "start"), true)
  assert.equal(assistantAdmits({ id: "antigravity" }, "start"), false)
  assert.equal(assistantAdmits({ id: "foreign" }, "start"), false)
  assert.equal(assistantAdmits(undefined, "start"), false)
  assert.equal(assistantAdmits({ id: "codex", capabilities: { ...active.capabilities!, start: false } }, "start"), false)
  assert.equal(isAssistant("antigravity"), true)
  assert.equal(isAssistant("gemini"), false)
  assert.equal(assistantLabel("antigravity"), "Gemini")
})

test("Antigravity slash commands stay drafts while ordinary prompts and other providers retain their behavior", () => {
  for (const text of ["/", "/model", "  /config", "\n/quit"]) assert.equal(antigravitySlashDraft("antigravity", text), true)
  for (const text of ["", "Explain /model", "A normal prompt", "/src/main.go is the file to explain", "/README.md", "/model.json", "/unknown-file"]) assert.equal(antigravitySlashDraft("antigravity", text), false)
  assert.equal(antigravitySlashDraft("codex", "/model"), false)
  assert.equal(antigravitySlashDraft("claude", "/exit"), false)
})

test("Antigravity compose requires that provider's readiness and an idle screen without a menu", () => {
  assert.equal(assistantComposeAdmits("antigravity", active, "idle", false), true)
  assert.equal(assistantComposeAdmits("antigravity", undefined, "idle", false), false)
  assert.equal(assistantComposeAdmits("antigravity", { ...active, id: "claude" }, "idle", false), false)
  assert.equal(assistantComposeAdmits("antigravity", active, "idle", true), false)
  for (const state of [undefined, "unknown", "waiting", "working"]) {
    assert.equal(assistantComposeAdmits("antigravity", active, state, false), false)
  }
  assert.equal(assistantComposeAdmits("antigravity", { ...active, capabilities: { ...active.capabilities!, send: false } }, "idle", false), false)
  assert.equal(assistantComposeAdmits("claude", undefined, "working", false), true)
  assert.equal(assistantComposeAdmits("codex", undefined, "unknown", true), true)
})

test("local start and exact resume routes carry Antigravity and the role through the existing Cloud route parser", () => {
  assert.deepEqual(writeRoute("POST", startPath("project fixture", "antigravity", undefined, "frontend")), {
    op: "start", word: "start", place: "project fixture", assistant: "antigravity", model: "", persona: "frontend",
  })
  assert.deepEqual(writeRoute("POST", resumePath("project fixture", "conversation-fixture", "antigravity", "frontend")), {
    op: "resume", word: "resume", place: "project fixture", assistant: "antigravity", past: "conversation-fixture", persona: "frontend",
  })
})

test("the start JSON transport keeps a producer detail key in either refusal envelope", async t => {
  const key = Object.keys(english).find(key => /^http\.[0-9a-f]{16}$/u.test(key))! as keyof typeof english
  const jsonFetch = makeJSONFetch({ words: { offline: "offline", requestFailed: "failed", notJSON: "invalid" } })
  for (const body of [
    { error: "assistant_unavailable", detail: "Synthetic refusal", detail_key: key },
    { error: { code: "assistant_unavailable", message: "Synthetic refusal", detail_key: key } },
  ]) {
    const mock = t.mock.method(globalThis, "fetch", async () => new Response(JSON.stringify(body), { status: 409 }))
    try {
      await assert.rejects(jsonFetch(startPath("fixture", "antigravity")), (error: unknown) => {
        assert.equal((error as { code: string }).code, "assistant_unavailable")
        assert.equal((error as { detailKey: string }).detailKey, key)
        assert.deepEqual(catalogRefusalDetail(error), { text: chinese[key], lang: "zh-Hant" })
        return true
      })
    } finally { mock.mock.restore() }
  }
})

test("signed Cloud projection preserves Antigravity without allowing a stale or foreign menu", () => {
  const machine = "machine-fixture", session = "pane-fixture"
  const generation = "a".repeat(32), execution = "b".repeat(32)
  const inventory = "__clawdline_inventory_v1__"
  const hold = (id: string, payload: unknown) => ({ identity: { machine, session: id }, payload, observedAt: 2000, sequence: 1 })
  const statusSnapshots = new Map([
    [JSON.stringify([machine, inventory]), hold(inventory, { inventory: { version: 1, sessions: [session] }, at: 2,
      complete: true, snapshot_generation: generation })],
    [JSON.stringify([machine, session]), hold(session, { machine_id: machine, session_id: session, assistant: "antigravity",
      execution_generation: execution, state: "waiting", source: { observed_at: 2, freshness: "current" },
      projected_at: 2, inventory_complete: true, snapshot_generation: generation })],
  ])
  const reading = statusProjection({ statusSnapshots }, machine, 2000)
  assert.equal(reading.kind, "ready")
  assert.equal(reading.rows?.[0].assistant, "antigravity")
  const destination = { machineID: machine, sessionID: session, executionGeneration: execution }
  const row = { identity: { machine, session }, machine, session, execution_generation: execution,
    source: { freshness: "current", observed_at: 2 }, menu: { question: "Allow synthetic command?",
      options: [{ n: 1, label: "Allow once", can: true }] } }
  const client = { detailSnapshots: new Map([[machine + "\u0000" + session, row]]) }
  assert.equal(pinnedQuestion(client, destination, 2000)?.options[0].key, "1")
  assert.equal(pinnedQuestion(client, { ...destination, executionGeneration: "c".repeat(32) }, 2000), null)
  assert.equal(pinnedQuestion(client, { ...destination, machineID: "foreign" }, 2000), null)
  assert.equal(pinnedQuestion(client, destination, STATUS_FRESH_MS + 3000), null)
})


test("explicit native start and resume remain available without a separate auth preflight", () => {
  for (const authState of ["required", "unknown", undefined] as const) {
    const facts = { ...active, authState }
    assert.equal(assistantAdmits(facts, "start"), true)
    assert.equal(assistantAdmits(facts, "send"), false)
    assert.equal(assistantAdmits(facts, "resume"), true)
    assert.equal(assistantAdmits({ ...facts, capabilities: { ...active.capabilities!, resume: false } }, "resume"), false)
    assert.equal(assistantAdmits({ ...facts, capabilities: { ...active.capabilities!, start: false } }, "start"), false)
  }
  assert.equal(assistantAdmits({ ...active, authState: "unsupported_account" }, "start"), false)
  assert.equal(assistantAdmits({ ...active, authState: "unsupported_account" }, "resume"), false)
})

test("Antigravity menu keys require capabilities, a current exact execution and a screen menu revision", () => {
  const row = { id: "fixture", assistant: "antigravity", execution_generation: "a".repeat(32), identity: "launch_hook", sessionId: "fixture-conversation", line: "synthetic revision",
    state: "waiting", source: { freshness: "current", observed_at: Date.now() / 1000 }, menu: { options: [{ n: 1, label: "Allow once", can: true }] } } as any
  assert.equal(assistantMenuAdmits(row, active), true)
  for (const patch of [{ execution_generation: undefined }, { identity: "unreadable" }, { line: undefined }, { sessionId: undefined }, { source: undefined },
    { source: { freshness: "stale" } }, { state: "unknown" }, { menu: undefined },
    { menu: { ...row.menu, source: "transcript" } }]) assert.equal(assistantMenuAdmits({ ...row, ...patch }, active), false)
  assert.equal(assistantMenuAdmits(row), false)
  assert.equal(assistantMenuAdmits({ ...row, assistant: "codex" }), true)
})


test("owned protocol identity admits a fresh composer without inventing a terminal menu", () => {
 const now=Date.now()
 const row={id:"owned:acp:synthetic",backend:"owned",assistant:"antigravity",identity:"protocol",sessionId:"synthetic-native",execution_generation:"a".repeat(32),state:"idle",source:{freshness:"current",observed_at:now/1000}} as any
 const preview:AssistantFacts={...active,capabilities:{start:true,send:true,transcript:true,resume:false,menus:false}}
 assert.equal(assistantStateCurrent(row,now),true)
 assert.equal(assistantComposeAdmits(row.assistant,preview,row.state,false),true)
 assert.equal(assistantStateCurrent({...row,backend:"tmux"},now),false)
 assert.equal(assistantStateCurrent({...row,execution_generation:undefined},now),false)
 assert.equal(assistantStateCurrent(row,now+16000),false)
 assert.equal(assistantMenuAdmits({...row,state:"waiting",line:"synthetic",menu:{options:[{n:1,label:"Allow",can:true}]}},preview),false)
})

test("an interrupted owned conversation can accept an explicit continuation without reopening a terminal", () => {
  assert.equal(assistantComposeAdmits("antigravity", undefined, "unknown", false, true), true)
  assert.equal(assistantComposeAdmits("antigravity", undefined, "working", false, true), true)
  assert.equal(assistantComposeAdmits("antigravity", undefined, "waiting", true, true), true)
  assert.equal(assistantComposeAdmits("antigravity", active, "unknown", false, false), false)
})

// A new hosted bundle can still reach an older installed daemon.
test("Gemini roles require the new daemon capability while original providers retain their picker", () => {
  assert.equal(assistantRolesAdmit("antigravity", active), false)
  assert.equal(assistantRolesAdmit("antigravity", { ...active, capabilities: { ...active.capabilities!, roles: true } }), true)
  assert.equal(assistantRolesAdmit("antigravity", { ...active, capabilities: { ...active.capabilities!, roles: false } }), false)
  assert.equal(assistantRolesAdmit("codex"), true)
  assert.equal(assistantRolesAdmit("claude"), true)
})


test("owned Gemini permission choices remain answerable without a terminal screen or account preflight", () => {
  const row = { id: "owned:acp:fixture", backend: "owned", assistant: "antigravity", identity: "protocol", state: "waiting",
    menu: { revision: "native-offer-exact", question: "Inspect the attached image?", options: [{ n: 1, label: "Allow once", can: true }, { n: 2, label: "Deny", can: true }] } } as any
  assert.equal(assistantMenuAdmits(row), true)
  assert.equal(assistantMenuAdmits({ ...row, identity: { machine: "fixture-machine", session: row.id }, sessionBinding: "protocol" }), true)
  for (const patch of [{ state: "idle" }, { menu: undefined }, { menu: { ...row.menu, revision: "" } },
    { menu: { ...row.menu, options: [] } }, { menu: { ...row.menu, source: "transcript" } }]) {
    assert.equal(assistantMenuAdmits({ ...row, ...patch }), false)
  }
})


test("owned Gemini model and effort operations reach the Host while unsupported terminal commands stay drafts", () => {
 for (const text of ["/model native-model", "/effort high", "/model   native-model"]) {
  assert.equal(antigravitySlashDraft("antigravity", text, true), false)
  assert.equal(antigravitySlashDraft("antigravity", text), true)
 }
 for (const text of ["/model", "/effort", "/exit", "/logout", "/model native\n/exit"]) assert.equal(antigravitySlashDraft("antigravity", text, true), true)
})
