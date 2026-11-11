import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

// Render the real panel with English words; only the strings module is swapped
// for one that answers each key with its English source, so the test does not
// depend on a fetched catalog.
const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("./BrowserInvites.tsx", import.meta.url))],
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic",
  loader: { ".css": "empty" },
  plugins: [{ name: "browser-invites-words", setup(builder) {
    builder.onResolve({ filter: /next-strings\.js$/ }, () => ({ path: "words", namespace: "fixture" }))
    builder.onLoad({ filter: /.*/, namespace: "fixture" }, () => ({ contents:
      "export const nextWord = (key, holes) => key + (holes ? JSON.stringify(holes) : '')" }))
  } }],
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { BrowserInvites } = compiled.exports

const manager = (overrides = {}) => ({
  apiOrigin: "https://api.example", deviceID: () => "web_owner", carrier: () => null, tryDirect: async () => ({ kind: "unsupported" }),
  rename: async () => ({ kind: "blank" }), forget: async () => ({ kind: "refused" }), pair: () => {},
  serves: () => true, ask: async () => ({}), invited: () => false, ...overrides,
})
const render = (m) => renderToStaticMarkup(React.createElement(BrowserInvites, { machineID: "mac_1", machineName: "Studio", manager: m }))

test("an owner's machine that serves invitations offers the three expiries and a start button", () => {
  const html = render(manager())
  for (const key of ["invTitle", "invIntro", "invExpiry8h", "invExpiry7d", "invExpiryNever", "invStart", "invListTitle"]) {
    assert.ok(html.includes(key), key)
  }
  // 8 hours is the default; the never warning is not shown until never is chosen.
  assert.match(html, /checked="" value="8h"/)
  assert.ok(!html.includes("invNeverWarning"))
})

test("an older machine and an invited browser are told why, and offered nothing", () => {
  const old = render(manager({ serves: () => false }))
  assert.ok(old.includes("invUnavailable") && !old.includes("invStart"))
  const invited = render(manager({ invited: () => true }))
  assert.ok(invited.includes("invInvitedHere") && !invited.includes("invStart"))
})
