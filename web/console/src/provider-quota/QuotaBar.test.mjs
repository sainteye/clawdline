import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

// Only transport, browser preferences and catalog are replaced; render the real footer.
const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("./QuotaBar.tsx", import.meta.url))],
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false,
  jsx: "automatic", loader: { ".css": "empty" },
  plugins: [{ name: "quota-boundaries", setup(builder) {
    builder.onResolve({ filter: /(?:client|next-strings|useFleet|NeedsUpdate|machine-switch|bridge|visibility)\.js$/ }, ({ path }) => ({ path, namespace: "fixture" }))
    builder.onLoad({ filter: /.*/, namespace: "fixture" }, ({ path }) => ({ contents:
      path.includes("client") ? "export const client = {}"
      : path.includes("next-strings") ? "export const nextWord = key => key"
      : path.includes("useFleet") ? "export const usePoll = () => globalThis.quotaFixture.poll"
      : path.includes("NeedsUpdate") ? "export const useMachineVersion = () => globalThis.quotaFixture.version"
      : path.includes("machine-switch") ? "export const onMachineChanged = () => () => {}"
      : path.includes("bridge") ? "export const assistantLogoHTML = () => ''"
      : "export const subscribeQuotaVisible = () => () => {}; export const quotaVisible = () => globalThis.quotaFixture.preference",
    }))
  } }],
})
// Supply a server snapshot for the client-only external-store hook; React still
// owns every other hook and the component's render.
const require = createRequire(import.meta.url)
const renderRequire = id => id === "react" ? { ...require(id), useSyncExternalStore: (_subscribe, read) => read() } : require(id)
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(renderRequire, compiled, compiled.exports)
const { QuotaBar } = compiled.exports
const provider = { id: "codex", label: "Codex", windows: [{ name: "7d", used_percent: 0 }], stale: false }
function render({ providers = [], pending = false, error = null, mobile = false, mobileListShown = true, enabled = true, apiLevel = 56, offline = false } = {}) {
  const prior = ["window", "quotaFixture"].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)])
  try {
    globalThis.window = { matchMedia: () => ({ matches: mobile }) }
    globalThis.quotaFixture = { poll: { data: pending ? null : { providers }, pending, error, poke() {} }, version: { apiLevel }, preference: { desktop: enabled, mobile: enabled } }
    return renderToStaticMarkup(React.createElement(QuotaBar, { mobileListShown, offline, accessory: React.createElement("button", null, "Dock entry") }))
  } finally {
    for (const [key, descriptor] of prior) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else delete globalThis[key] }
  }
}

test("the status bar and its dock remain while readings load, are empty, or fail", () => {
  for (const state of [{ pending: true }, {}, { error: "timeout" }, { offline: true }]) {
    const html = render(state)
    assert.match(html, /<footer/)
    assert.match(html, /Dock entry/)
    assert.match(html, /role="status"/)
    assert.doesNotMatch(html, /<progress/)
  }
})
test("a real zero remains a usable reading and refresh failures retain it", () => {
  for (const error of [null, "timeout"]) {
    const html = render({ providers: [provider], error })
    assert.match(html, /data-provider="codex"/)
    assert.match(html, /value="0"/)
    if (error) assert.match(html, /data-stale="true"/)
  }
})
test("quota preferences, mobile detail and older machines hide quotas while keeping shortcuts", () => {
  for (const state of [{ enabled: false }, { mobile: true, mobileListShown: false }, { apiLevel: 27 }]) {
    const html = render({ providers: [provider], ...state })
    assert.match(html, /<footer/)
    assert.match(html, /Dock entry/)
    assert.doesNotMatch(html, /data-provider=|<dialog|quota-refresh/)
  }
})
