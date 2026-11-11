import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

// Render the real access section with each key answering as itself, so the
// test does not depend on a fetched catalog.
const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("./MachineAccess.tsx", import.meta.url))],
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic",
  loader: { ".css": "empty" },
  plugins: [{ name: "access-words", setup(builder) {
    builder.onResolve({ filter: /next-strings\.js$/ }, () => ({ path: "words", namespace: "fixture" }))
    builder.onLoad({ filter: /.*/, namespace: "fixture" }, () => ({ contents:
      "export const nextWord = (key, holes) => key + (holes ? JSON.stringify(holes) : '')" }))
  } }],
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { MachineAccess, localAccessSource, localInviteAsk, cloudAccessSource } = compiled.exports

const manager = (overrides = {}) => ({
  apiOrigin: "https://api.example", deviceID: () => "web_owner", carrier: () => null, tryDirect: async () => ({ kind: "unsupported" }),
  rename: async () => ({ kind: "blank" }), forget: async () => ({ kind: "refused" }), pair: () => {},
  serves: () => true, ask: async () => ({}), invited: () => false, ...overrides,
})
const render = (source) => renderToStaticMarkup(React.createElement(MachineAccess, { source }))

test("both consoles draw the same card and the same three groups", () => {
  const cloud = render(cloudAccessSource(manager(), "mac_1", "Studio"))
  const local = render(localAccessSource("Studio", true, React.createElement("div", null, "SIGNED-IN-BLOCK")))
  for (const html of [cloud, local]) {
    for (const key of ["accessPairTitle", "accessMakeLink", "invExpiry8h", "invExpiry7d", "invExpiryNever",
      "accessListTitle", "Studio", "accessGroupInvited", "accessGroupAccount", "accessGroupDirect"]) {
      assert.ok(html.includes(key), key)
    }
    assert.match(html, /checked="" value="8h"/)
  }
  // Each says how far revoking an account device reaches from where it is.
  assert.ok(cloud.includes("accessGroupAccountCloudNote") && !cloud.includes("accessGroupAccountLocalNote"))
  assert.ok(local.includes("accessGroupAccountLocalNote") && local.includes("SIGNED-IN-BLOCK"))
})

test("an older machine, an invited browser and a machine without Cloud are told why", () => {
  assert.ok(render(cloudAccessSource(manager({ serves: () => false }), "mac_1", "Studio")).includes("invUnavailable"))
  const invited = render(cloudAccessSource(manager({ invited: () => true }), "mac_1", "Studio"))
  assert.ok(invited.includes("invInvitedHere") && !invited.includes("accessMakeLink"))
  assert.ok(render(localAccessSource("Studio", false, null)).includes("accessCloudOff"))
})

test("the machine's own console asks the invitation words as its own routes", async () => {
  const asked = []
  const read = async (path, init) => {
    asked.push(`${init?.method ?? "GET"} ${path} ${init?.body ?? ""}`.trim())
    return new Response(JSON.stringify({ invite_id: "K7M4Q", phase: "waiting" }), { status: 200 })
  }
  const ask = localInviteAsk(read)
  await ask("browser-invite-start", { expiry: "7d" }, "action")
  await ask("browser-invite-status", { invite_id: "K7M4Q" }, "read")
  await ask("browser-invite-decide", { invite_id: "K7M4Q", approve: true }, "action")
  await ask("browser-invite-cancel", { invite_id: "K7M4Q" }, "action")
  await ask("browser-invite-revoke", { device_id: "web_1" }, "action")
  assert.deepEqual(asked, [
    'POST /v1/cloud/browser-invites {"expiry":"7d"}',
    "GET /v1/cloud/browser-invites/K7M4Q",
    'POST /v1/cloud/browser-invites/K7M4Q/decision {"approve":true}',
    "POST /v1/cloud/browser-invites/K7M4Q/cancel {}",
    "POST /v1/cloud/browser-invites/devices/web_1/revoke {}",
  ])
  const refused = localInviteAsk(async () => new Response(JSON.stringify({ error: { code: "invite_limit" } }), { status: 409 }))
  await assert.rejects(refused("browser-invite-start", { expiry: "8h" }, "action"), (e) => e.code === "invite_limit")
})

test("the local account group leaves out invited browsers and refused rows", async () => {
  const read = async (path) => new Response(JSON.stringify(path === "/v1/cloud/status" ? { devices: [
    { id: "a", kind: "ios", name: "iPhone", paired_at: 1794000000 },
    { id: "b", kind: "invited_browser", name: "Chrome · Linux", invited: true },
    { id: "c", kind: "browser", name: "Old", revoked: true },
    { id: "d", name: null },
  ] } : {}), { status: 200 })
  const rows = await localAccessSource("Studio", true, null, read).account()
  assert.deepEqual(rows.map((r) => r.id), ["a"])
})
