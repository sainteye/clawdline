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
      : path.includes("bridge") ? "export const assistantLogoHTML = () => ''; export const failureSentence = () => ''"
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
function render({ providers = [], pending = false, error = null, mobile = false, mobileListShown = true, enabled = true, apiLevel = 56, offline = false, quotaScope = undefined, unread = false } = {}) {
  const prior = ["window", "quotaFixture"].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)])
  try {
    globalThis.window = { matchMedia: () => ({ matches: mobile }) }
    globalThis.quotaFixture = { poll: { data: pending || unread ? null : { providers }, pending, error, poke() {} }, version: { apiLevel }, preference: { desktop: enabled, mobile: enabled } }
    return renderToStaticMarkup(React.createElement(QuotaBar, { mobileListShown, offline, quotaScope, accessory: React.createElement("button", null, "Dock entry") }))
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
test("shortcuts follow the original quota bar visibility", () => {
  for (const state of [{ enabled: false }, { enabled: false, mobile: true }, { mobile: true, mobileListShown: false }, { apiLevel: 27 }]) {
    assert.equal(render({ providers: [provider], ...state }), "")
  }
})
test("a remounted footer whose first read fails keeps the last reading, marked outdated", () => {
  for (const mobile of [false, true]) {
    const quotaScope = `mac-remount-${mobile}`
    const used = { ...provider, windows: [{ name: "5h", used_percent: 55 }] }
    assert.match(render({ providers: [used], quotaScope, mobile }), /value="55"/)
    // A new mount: the poller has nothing of its own yet and its first read failed.
    const html = render({ unread: true, error: "timeout", quotaScope, mobile })
    assert.match(html, /data-provider="codex"/)
    assert.match(html, /value="55"/)
    assert.match(html, /data-stale="true"/)
    assert.doesNotMatch(html, /quotaReadUnavailable/)
  }
})
test("only a machine never read shows that its quota could not be read", () => {
  const html = render({ unread: true, error: "timeout", quotaScope: "mac-never-read" })
  assert.match(html, /quotaReadUnavailable/)
  assert.doesNotMatch(html, /<progress/)
})
test("a saved reading shown while the first read is out is marked outdated", () => {
  const quotaScope = "mac-loading"
  render({ providers: [provider], quotaScope })
  const html = render({ pending: true, quotaScope })
  assert.match(html, /data-provider="codex"/)
  assert.match(html, /data-stale="true"/)
  assert.doesNotMatch(html, /quotaLoading/)
})
