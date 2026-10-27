import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL("./UpdateChangelog.tsx", import.meta.url))],
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false,
  jsx: "automatic", loader: { ".css": "empty" },
  plugins: [{ name: "isolated-release-reader", setup(builder) {
    builder.onResolve({ filter: /^(\.\/update\.js|\.\.\/core\/util\.js)$/ }, ({ path }) => ({ path, namespace: "fixture" }))
    builder.onLoad({ filter: /.*/, namespace: "fixture" }, ({ path }) => ({ contents: path.includes("util")
      ? "export const toast = () => {}"
      : "export const checkUpdateNow = () => { throw new Error('Unexpected check') }; export const currentUpdateRead = () => null" }))
  } }],
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { UpdateChangelog, releaseChangelog } = compiled.exports

test("the initial review offers cancellation and cannot confirm before notes settle", () => {
  const html = renderToStaticMarkup(React.createElement(UpdateChangelog, {
    latest: { version: "v2.3.0", stamp: "release" }, onClose() {}, onConfirm() { assert.fail("an initial render cannot install") },
  }))
  assert.match(html, /What’s new in Clawdline v2\.3\.0/)
  assert.match(html, /Loading release notes/)
  assert.match(html, />Not now<\/button>/)
  assert.match(html, /disabled="">Confirm update<\/button>/)
})

test("release notes read the exact public tag without credentials", async () => {
  const prior = globalThis.fetch
  const signal = new AbortController().signal
  let calls = 0
  try {
    globalThis.fetch = async (url, options) => {
      calls++
      assert.equal(url, "https://api.github.com/repos/sainteye/clawdline/releases/tags/v2.3.0")
      assert.equal(options.credentials, "omit")
      assert.equal(options.signal, signal)
      return Response.json({ tag_name: "v2.3.0", body: "## Changes\n\n- Review before installing.", draft: false })
    }
    assert.match(await releaseChangelog("v2.3.0", signal), /Review before installing/)
    assert.equal(calls, 1)
  } finally { globalThis.fetch = prior }
})

test("missing, mismatched, draft, empty and failed notes are refused without retries", async () => {
  const prior = globalThis.fetch
  try {
    for (const answer of [
      new Response("", { status: 404 }), new Response("", { status: 429 }),
      Response.json({ tag_name: "v2.4.0", body: "Wrong release" }),
      Response.json({ tag_name: "v2.3.0", body: "Unpublished", draft: true }),
      Response.json({ tag_name: "v2.3.0", body: " " }),
      Response.json({ tag_name: "v2.3.0", body: null }),
    ]) {
      let calls = 0
      globalThis.fetch = async () => { calls++; return answer }
      await assert.rejects(releaseChangelog("v2.3.0", new AbortController().signal))
      assert.equal(calls, 1)
    }
  } finally { globalThis.fetch = prior }
})
