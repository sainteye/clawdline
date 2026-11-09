import assert from "node:assert/strict"
import test from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
import type { SessionRow } from "@clawdline/contract"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { antigravitySlashDraft, assistantAdmits, assistantComposeAdmits, assistantLabel, assistantMenuAdmits, assistantProtocolOwned, assistantRolesAdmit, assistantTurnIncomplete, assistantUnavailable, isAssistant, sessionActionVisibility, type AssistantFacts } from "./antigravity-ui.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { startedRow } from "./started-row.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { startPath, resumePath } from "./place-routes.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { nativeTerminalAvailable, nativeTerminalCommand } from "./native-terminal-command.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { writeRoute } from "../cloud/relay-writer.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { statusProjection } from "../cloud/status-projection.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { statusSource } from "../cloud/status-source.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { RelayReader } from "../cloud/relay-reader.ts"
// @ts-expect-error -- Node's test runner resolves the TypeScript source.
import { NEW_SESSION_ASSISTANTS } from "../pages/work/session-assignment.ts"
import english from "../../public/catalogs/en.json" with { type: "json" }
import chinese from "../../public/catalogs/zh-Hant.json" with { type: "json" }

const active: AssistantFacts = { id: "grok", installed: true, authState: "ready", integrationState: "active",
  capabilities: { start: true, resume: true, send: true, menus: true, transcript: true } }
const owned = (extra: Record<string, unknown> = {}) => ({ id: "owned:fixture", assistant: "grok", backend: "owned",
  identity: "protocol", sessionId: "native-fixture", state: "unknown", ...extra }) as SessionRow

test("Grok is a distinct launch and assignment provider with explicit capability admission", () => {
  assert.equal(isAssistant("grok"), true)
  assert.equal(isAssistant("xai"), false)
  assert.equal(assistantLabel("grok"), "Grok")
  assert.ok(NEW_SESSION_ASSISTANTS.includes("grok"))
  for (const operation of ["start", "resume", "send", "menus", "transcript"] as const) {
    assert.equal(assistantAdmits(active, operation), true)
    assert.equal(assistantAdmits({ id: "grok" }, operation), false)
    assert.equal(assistantAdmits({ ...active, capabilities: { ...active.capabilities!, [operation]: false } }, operation), false)
  }
  for (const [patch, reason] of [
    [{ installed: false }, "missing"], [{ integrationState: "missing" }, "integrationMissing"],
    [{ integrationState: "incompatible" }, "incompatible"], [{ integrationState: "disabled" }, "disabled"],
    [{ authState: "required", capabilities: undefined }, "required"],
    [{ authState: "unknown", capabilities: undefined }, "unknownAuth"], [{ authState: "unsupported_account" }, "unsupported"],
  ] as const) assert.equal(assistantUnavailable({ ...active, ...patch }, "start"), reason)
  assert.equal(assistantAdmits({ ...active, authState: "unknown" }, "start"), true, "only advertised bootstrap bypasses a separate auth preflight")
  assert.equal(assistantRolesAdmit("grok"), false)
  assert.equal(assistantRolesAdmit("grok", { ...active, capabilities: { ...active.capabilities!, roles: true } }), true)
})

test("Grok start and resume preserve the exact provider over the Cloud writer routes", () => {
  assert.deepEqual(writeRoute("POST", startPath("fixture", "grok", "native-model", "reviewer")), {
    op: "start", word: "start", place: "fixture", assistant: "grok", model: "native-model", persona: "reviewer",
  })
  assert.deepEqual(writeRoute("POST", resumePath("fixture", "conversation", "grok")), {
    op: "resume", word: "resume", place: "fixture", assistant: "grok", past: "conversation", persona: "",
  })
  assert.equal(startedRow({ id: "owned:fixture", assistant: "grok", backend: "owned" }, null)?.assistant, "grok")
})

test("Grok sends and native permission replies require an owned protocol binding", () => {
  for (const state of ["unknown", "idle", "working", "waiting"]) {
    assert.equal(assistantComposeAdmits("grok", undefined, state, true, true), true, "stale account facts do not reject an explicit owned message")
    assert.equal(assistantComposeAdmits("grok", active, state, false, false), false)
  }
  const menu = { revision: "native-offer", question: "Vendor permission question", options: [{ n: 1, label: "Vendor choice", can: true }] }
  const waiting = owned({ state: "waiting", menu })
  assert.equal(assistantMenuAdmits(waiting), true)
  assert.equal(waiting.menu?.options[0].label, "Vendor choice")
  for (const patch of [{ backend: "tmux" }, { identity: "launch_hook" }, { state: "unknown" },
    { menu: { ...menu, revision: undefined } }, { menu: { ...menu, source: "transcript" } }]) {
    assert.equal(assistantMenuAdmits(owned({ state: "waiting", menu, ...patch }), active), false)
  }
  assert.deepEqual(sessionActionVisibility(owned()), { interrupt: true, focus: false, screen: false })
  for (const backend of ["owned", "tmux", "iterm"]) {
    assert.equal(nativeTerminalCommand("/help", "grok", backend), false)
    assert.equal(nativeTerminalAvailable("grok", backend), false)
  }
})

test("Grok command drafts reject effort and unsupported native commands while paths remain prompts", () => {
  for (const command of ["/", "/help", "/model", "/effort high", "/exit", "/future-command arg"]) {
    assert.equal(antigravitySlashDraft("grok", command, true), true, command)
  }
  for (const prompt of ["Explain the code", "/src/main.go", "/README.md", "Review /model", "/model native-model"]) {
    assert.equal(antigravitySlashDraft("grok", prompt, true), false, prompt)
  }
  assert.equal(antigravitySlashDraft("grok", "/model native-model", false), true)
  assert.equal(antigravitySlashDraft("antigravity", "/effort high", true), false)
})

test("Grok incomplete native turns retain error status and provider-specific bilingual copy", () => {
  for (const code of ["grok_turn_inactivity_timeout", "antigravity_process_exit", "antigravity_session_lost"]) {
    assert.equal(assistantTurnIncomplete("grok", code), true)
  }
  assert.equal(assistantTurnIncomplete("grok", "ordinary reply"), false)
  assert.equal(assistantTurnIncomplete("codex", "antigravity_process_exit"), false)
  for (const catalog of [english, chinese]) {
    assert.match(catalog["grok.turnIncomplete"], /Grok/)
    assert.doesNotMatch(catalog["grok.turnIncomplete"], /Gemini|Antigravity/)
    for (const key of ["placeholder", "sendUnavailable", "slashDraft"] as const) assert.match(catalog[`grok.${key}`], /Grok/)
  }
})

test("Grok tool cards preserve vendor titles, incomplete status and escaped native output", async () => {
  const { outputFiles } = await build({ stdin: { contents: 'export * from "./NativeToolActivity.tsx"',
    resolveDir: fileURLToPath(new URL(".", import.meta.url)), loader: "ts" }, bundle: true, platform: "node",
    format: "cjs", packages: "external", write: false, jsx: "automatic", loader: { ".css": "empty" } })
  const compiled: { exports: any } = { exports: {} }
  new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
  const { NativeToolCard, nativeToolTitle } = compiled.exports
  const entry = { role: "tool", tool: "read", text: "Fallback text", activity: { kind: "called", title: "Vendor read label",
    actions: [{ kind: "read", path: "/workspace/example.go" }], status: "in_progress", result: "<script>native output</script>" } }
  assert.equal(nativeToolTitle(entry, "grok"), "Vendor read label")
  assert.equal(nativeToolTitle(entry, "antigravity"), "Read example.go")
  const html = renderToStaticMarkup(React.createElement(NativeToolCard, { entry, assistant: "grok", toolLabel: "tool", expanded: true }))
  assert.match(html, /Vendor read label/)
  assert.match(html, /running/)
  assert.match(html, /&lt;script&gt;native output&lt;\/script&gt;/)
  assert.doesNotMatch(html, /<script>|completed/)
  entry.activity.status = "cancelled"
  const cancelled = renderToStaticMarkup(React.createElement(NativeToolCard, { entry, assistant: "grok", toolLabel: "tool", expanded: true }))
  assert.match(cancelled, /cancelled/)
  assert.doesNotMatch(cancelled, /running|completed/)
  assert.match(cancelled, /&lt;script&gt;native output&lt;\/script&gt;/)
})

function cloudFixture() {
  const machine = "machine-fixture", session = "owned:fixture", execution = "a".repeat(32), now = Date.now()
  const generation = "b".repeat(32), at = now / 1000
  const destination = { machineID: machine, sessionID: session, executionGeneration: execution }
  const hold = (session: string, payload: unknown) => ({ identity: { machine, session }, payload, observedAt: now, sequence: 1 })
  const statusSnapshots = new Map([
    [JSON.stringify([machine, "__clawdline_inventory_v1__"]), hold("__clawdline_inventory_v1__", {
      inventory: { version: 1, sessions: [session] }, at, complete: true, snapshot_generation: generation })],
    [JSON.stringify([machine, session]), hold(session, { machine_id: machine, session_id: session, assistant: "grok",
      execution_generation: execution, state: "unknown", source: { observed_at: at, freshness: "current" },
      projected_at: at, inventory_complete: true, snapshot_generation: generation })],
  ])
  const client = { ready: true, statusSnapshots, now: () => now,
    readContentCapabilities: new Map([[machine, { at, supported: true }]]),
    events: () => () => {},
    machines: async () => ({ machines: [{ id: machine, freshness: "current" }], syncing: false, retryAfterMs: 0 }),
    sessions: async () => ({ sessions: [], at, scan: { emptyAuthoritative: true, recovering: [], failures: [] } }),
    listPresentationsForMachine: async () => ({ at, complete: true, sessions: [{ id: session,
      execution_generation: execution, title: "Native conversation", assistant: "grok", status: {
        state: "unknown", work_state: "unknown", identity: "protocol", sessionId: "native-conversation" } }] }),
  }
  return { machine, session, execution, now, destination, client }
}

test("signed and direct Cloud projections preserve Grok identity and owned protocol input", async () => {
  for (const direct of [false, true]) {
    const { machine, now, client } = cloudFixture()
    assert.equal(statusProjection(client, machine, now).rows?.[0].assistant, "grok")
    if (direct) client.statusSnapshots.clear()
    const source = statusSource(() => client as never)
    const signal = new AbortController().signal
    const reading = await source.readMachine(machine, signal)
    assert.equal(reading.rows?.[0].assistant, "grok")
    const presentations = await source.readMachinePresentations!(machine, signal)
    assert.equal(presentations?.[0].assistant, "grok")
    for (const path of direct ? ["projected", "list"] : ["projected", "list", "fleet"]) {
      const reader: RelayReader = new RelayReader(machine, { classicStatus: true, now: () => now,
        ...(path === "projected" ? { machineProjection: async () => reading } : {}),
        ...(path === "fleet" ? { fleetTarget: () => presentations![0].destination,
          fleetPresentation: () => presentations![0] } : { listPresentations: async () => presentations }) })
      reader.attach(client as never)
      const row: SessionRow = (await reader.snapshot()).sessions[0]
      assert.equal(row?.assistant, "grok", path)
      assert.equal(row.backend, "owned", path)
      assert.equal(assistantProtocolOwned(row), true, path)
      assert.equal(assistantComposeAdmits(row.assistant, undefined, row.state, false, assistantProtocolOwned(row)), true, path)
    }
  }
})
