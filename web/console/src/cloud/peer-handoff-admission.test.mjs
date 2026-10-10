import assert from "node:assert/strict"
import { test } from "node:test"
import { dirname, resolve } from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"
import { unlinkSync } from "node:fs"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const here = dirname(fileURLToPath(import.meta.url))
const { outputFiles } = await build({ entryPoints: [resolve(here, "peer-handoff-admission.ts")],
  bundle: true, platform: "node", format: "esm", write: false })
const { checkedPeerAccessStatus, effectivePeerScopes, ensurePeerEndpointCurrent } = await import(
  `data:text/javascript;base64,${Buffer.from(outputFiles[0].contents).toString("base64")}`)
const generationA = "0123456789abcdef0123456789abcdef"
const generationB = "fedcba9876543210fedcba9876543210"
const sourceEndpoint = { machineID: "machine-a", sessionID: "shared", executionGeneration: generationA }
const targetEndpoint = { machineID: "machine-b", sessionID: "shared", executionGeneration: generationA }

function fixture() {
  const rows = new Map([["machine-a", { destination: sourceEndpoint, freshness: "current" }],
    ["machine-b", { destination: targetEndpoint, freshness: "current" }]])
  const machines = new Map([["machine-a", { id: "machine-a", freshness: "current", pairing: "paired" }],
    ["machine-b", { id: "machine-b", freshness: "current", pairing: "paired" }]])
  const source = { async readMachine(id) { return { kind: "ready", rows: [rows.get(id)] } } }
  const client = { async machines() { return { machines: [...machines.values()] } } }
  return { rows, machines, source, client }
}

test("peer write admission rejects a target generation changed after rendering", async () => {
  const f = fixture()
  f.rows.set("machine-b", { destination: { ...targetEndpoint, executionGeneration: generationB }, freshness: "current" })
  await assert.rejects(Promise.all([ensurePeerEndpointCurrent(sourceEndpoint, f.source, f.client),
    ensurePeerEndpointCurrent(targetEndpoint, f.source, f.client)]), /execution_generation_changed/)
})

test("peer write admission rejects a changed source and a stale target machine", async () => {
  const f = fixture()
  f.rows.set("machine-a", { destination: { ...sourceEndpoint, executionGeneration: generationB }, freshness: "current" })
  await assert.rejects(ensurePeerEndpointCurrent(sourceEndpoint, f.source, f.client), /execution_generation_changed/)
  f.rows.set("machine-a", { destination: sourceEndpoint, freshness: "current" })
  f.machines.set("machine-b", { id: "machine-b", freshness: "stale", pairing: "paired" })
  await assert.rejects(ensurePeerEndpointCurrent(targetEndpoint, f.source, f.client), /peer_machine_stale/)
})

test("peer write admission accepts two current exact destinations", async () => {
  const f = fixture()
  await Promise.all([ensurePeerEndpointCurrent(sourceEndpoint, f.source, f.client),
    ensurePeerEndpointCurrent(targetEndpoint, f.source, f.client)])
  assert.equal(f.rows.get("machine-a").destination.machineID, "machine-a")
  assert.equal(f.rows.get("machine-b").destination.machineID, "machine-b")
})

test("peer access discovery accepts only the named machine and explicit complete arrays", () => {
  const reply = { action: "status", state: "identity_read", local_machine_id: "machine-a",
    pairs: [{ pair_id: "pair-1", source_machine_id: "machine-a", target_machine_id: "machine-b",
      source_fingerprint: "source", target_fingerprint: "target", state: "active", expires_at: "2030-01-01T00:00:00Z" }],
    grants: [{ grant_id: "grant-1", pair_id: "pair-1",
      source: { machine_id: "machine-a", session_id: "shared", execution_generation: generationA },
      target: { machine_id: "machine-b", session_id: "shared", execution_generation: generationB },
      scopes: ["message"], expires_at: "2030-01-01T00:00:00Z" }] }
  assert.equal(checkedPeerAccessStatus(reply, "machine-a", 123).grants[0].grant_id, "grant-1")
  assert.equal(checkedPeerAccessStatus(reply, "machine-a", 123).observedAt, 123)
  assert.throws(() => checkedPeerAccessStatus(reply, "machine-b"), { code: "peer_access_bad_status" })
  assert.throws(() => checkedPeerAccessStatus({ ...reply, grants: undefined }, "machine-a"),
    { code: "peer_access_bad_status" })
  assert.throws(() => checkedPeerAccessStatus({ ...reply, grants: [{ ...reply.grants[0],
    target: { ...reply.grants[0].target, execution_generation: "stale" } }] }, "machine-a"),
  { code: "peer_access_bad_status" })
})

test("effective scope boxes require a fresh active pair and unexpired grant", () => {
  const now = Date.parse("2026-10-10T00:00:00Z")
  const pair = { pair_id: "pair-1", source_machine_id: "machine-a", target_machine_id: "machine-b",
    state: "active", expires_at: "2026-10-10T00:10:00Z" }
  const grant = { grant_id: "grant-1", pair_id: pair.pair_id,
    source: { machine_id: "machine-a" }, target: { machine_id: "machine-b" },
    scopes: ["message"], expires_at: "2026-10-10T00:05:00Z" }
  const snapshot = { machineID: "machine-a", observedAt: now, pairs: [pair], grants: [grant] }
  assert.deepEqual(effectivePeerScopes(snapshot, grant, now + 1_000), ["message"])
  assert.deepEqual(effectivePeerScopes(snapshot, grant, now + 301_000), [])
  assert.deepEqual(effectivePeerScopes({ ...snapshot, pairs: [{ ...pair, state: "waiting_for_target" }] }, grant, now + 1_000), [])
  assert.deepEqual(effectivePeerScopes({ ...snapshot, pairs: [] }, grant, now + 1_000), [])
  assert.deepEqual(effectivePeerScopes(snapshot, { ...grant, expires_at: "2026-10-09T23:59:59Z" }, now + 1_000), [])
})

test("the Cloud access page explains its purpose before any machine read or write", async () => {
  const output = resolve(here, `.peer-access-page-test-${process.pid}.mjs`)
  try {
    await build({ entryPoints: [resolve(here, "PeerAccessPage.tsx")], outfile: output,
      bundle: true, platform: "node", format: "esm", packages: "external", loader: { ".css": "empty" } })
    const { PeerAccessPage } = await import(pathToFileURL(output).href)
    let writes = 0
    const html = renderToStaticMarkup(React.createElement(PeerAccessPage, {
      machines: [{ id: "mac", name: "Main", freshness: "current", paired: true },
        { id: "linux", name: "Other", freshness: "stale", paired: true },
        { id: "new", name: "Unpaired", freshness: "unknown", paired: false }],
      source: null, current: () => { writes++; return null },
    }))
    assert.match(html, /Cross-machine access/)
    assert.match(html, /Choose which machines can exchange Agent messages/)
    assert.match(html, /Your machines/)
    assert.doesNotMatch(html, /Choose a Session|fingerprint|grant ID/i)
    assert.doesNotMatch(html, /Main · mac|Other · linux|Unpaired · new/)
    assert.equal(writes, 0)
  } finally {
    try { unlinkSync(output) } catch { /* build failed before writing */ }
  }
})
