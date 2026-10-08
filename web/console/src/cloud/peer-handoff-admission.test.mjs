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
const { ensurePeerEndpointCurrent } = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].contents).toString("base64")}`)
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

test("pair and grant revocation remain accessible without a current Session", async () => {
  const output = resolve(here, `.peer-revocation-test-${process.pid}.mjs`)
  try {
    await build({ entryPoints: [resolve(here, "PeerHandoffPanel.tsx")], outfile: output,
      bundle: true, platform: "node", format: "esm", packages: "external", loader: { ".css": "empty" } })
    const { PeerRevocationPanel } = await import(pathToFileURL(output).href)
    const html = renderToStaticMarkup(React.createElement(PeerRevocationPanel, {
      machines: [{ id: "machine-a", name: "First machine", freshness: "current" }], current: () => null,
    }))
    assert.match(html, /<details[^>]*class="cloud-peer-panel cloud-peer-revocation"/)
    assert.match(html, /Revoke machine pair or grant/)
    assert.match(html, /First machine/)
    assert.match(html, /Revoke pair/)
    assert.match(html, /Revoke grant/)
    assert.doesNotMatch(html, /execution generation/)
  } finally {
    try { unlinkSync(output) } catch { /* build failed before writing */ }
  }
})
