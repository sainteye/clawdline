import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

// Render the real banner and model, with only the machine transport and native
// shell replaced. The fixture cannot contact a release host or install anything.
const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("./UpdateBanner.tsx", import.meta.url))],
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false,
  jsx: "automatic", loader: { ".css": "empty" },
  plugins: [{ name: "update-banner-machine-fixture", setup(builder) {
    builder.onResolve({ filter: /^(\.\/update\.js|\.\.\/client\.js|\.\/restart-app\.js)$/ }, ({ path }) => ({ path, namespace: "fixture" }))
    builder.onLoad({ filter: /.*/, namespace: "fixture" }, ({ path }) => ({ contents:
      path.includes("client") ? "export const followsRelay = () => false"
        : path.includes("restart-app") ? "export const canRestartStagedApp = () => false; export const restartStagedApp = () => false"
          : `export const currentUpdateRead = () => globalThis.updateBannerFixture.read;
             export const subscribeUpdate = () => () => {};
             export const applyUpdate = () => { throw new Error('Unexpected install') };
             export const checkUpdateNow = () => { throw new Error('Unexpected network check') };
             export const publishUpdateRead = () => {}; export const refreshUpdate = () => {};
             export const requestDevRelease = () => { throw new Error('Unexpected release') };`,
    }))
  } }],
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { UpdateBanner } = compiled.exports

function render({ latest = "v2.2.0", dismissed = null, apply = { state: "healthy", from: "v2.1.1", to: "v2.1.2" } } = {}) {
  const prior = ["updateBannerFixture", "localStorage"].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)])
  try {
    globalThis.updateBannerFixture = { read: { kind: "status", status: {
      install_kind: "release", state: latest === "v2.1.2" ? "current" : "update_available",
      running: { stamp: "running", version: "v2.1.2" }, latest: { stamp: "latest", version: latest }, apply,
    } } }
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: { getItem: () => dismissed } })
    return renderToStaticMarkup(React.createElement(UpdateBanner))
  } finally {
    for (const [key, descriptor] of prior) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else delete globalThis[key]
    }
  }
}

test("a newer release replaces the previous completion with its install action", () => {
  for (const dismissed of [null, "v2.1.2"]) {
    const html = render({ dismissed })
    assert.match(html, /Clawdline v2\.2\.0 is available/)
    assert.match(html, />Update now<\/button>/)
    assert.doesNotMatch(html, /v2\.1\.2 is ready/, "a completed older update must not obscure a newer release")
  }
})

test("dismissing the new release does not resurrect the older completion on reload", () => {
  assert.equal(render({ dismissed: "v2.2.0" }), "")
  assert.match(render({ latest: "v2.3.0", dismissed: "v2.2.0" }), /Clawdline v2\.3\.0 is available/)
})

test("a current release keeps its completion until that result is dismissed", () => {
  assert.match(render({ latest: "v2.1.2" }), /v2\.1\.2 is ready/)
  assert.equal(render({ latest: "v2.1.2", dismissed: "v2.1.2" }), "")
})

test("a moving update keeps its progress and hides a second install action", () => {
  const html = render({ apply: { state: "downloading", from: "v2.1.2", to: "v2.2.0" } })
  assert.match(html, /Downloading v2\.2\.0/)
  assert.doesNotMatch(html, />Update now<\/button>/, "an update already moving cannot be started again")
})
