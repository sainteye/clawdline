import assert from "node:assert/strict"
import { readFileSync, unlinkSync } from "node:fs"
import { test } from "node:test"
import { dirname, resolve } from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const here = dirname(fileURLToPath(import.meta.url))

test("the narrow and keyboard-ready action panel names its fixed target", async () => {
  const output = resolve(here, `.pinned-action-test-${process.pid}.mjs`)
  try {
    await build({ entryPoints: [resolve(here, "PinnedSessionActions.tsx")], outfile: output,
      bundle: true, platform: "node", format: "esm", packages: "external", loader: { ".css": "empty" } })
    const { PinnedSessionActionPanel } = await import(pathToFileURL(output).href)
    globalThis.localStorage = { getItem: () => null, setItem: () => {} }
    const destination = { machineID: "machine-a", sessionID: "same-session",
      executionGeneration: "0123456789abcdef0123456789abcdef" }
    const context = { destination, machine: { id: "machine-a", name: "Office", freshness: "current" },
      row: { destination, freshness: "current" }, content: { kind: "ready", question: {
        text: "Allow this operation?", fingerprint: "a".repeat(64),
        options: [{ key: "1", label: "Approve the current request" }], observedAt: Date.now(),
      } } }
    const html = renderToStaticMarkup(React.createElement(PinnedSessionActionPanel, {
      context, source: { readMachine: async () => ({ kind: "ready", rows: [context.row] }) }, current: () => null,
    }))
    assert.match(html, /Machine Office · Session same-session · execution generation 0123456789abcdef0123456789abcdef/)
    assert.match(html, /<form[^>]*>.*?<textarea[^>]*id="cloud-pinned-message"/s)
    assert.match(html, /<button type="submit"[^>]*>Send message<\/button>/)
    assert.match(html, /role="group" aria-label="Current question"/)
    assert.match(html, /Allow this operation\?/)
    assert.match(html, /<button type="button"[^>]*>Approve the current request<\/button>/)
    assert.doesNotMatch(html, /id="cloud-pinned-expect"/)
    assert.match(html, /<section[^>]*aria-label="Session actions and receipts"/)
    const css = readFileSync(resolve(here, "pinned-session-actions.css"), "utf8")
    assert.match(css, /min-height:\s*44px/)
    assert.match(css, /@media\s*\(max-width:\s*600px\)/)
    const source = readFileSync(resolve(here, "PinnedSessionActions.tsx"), "utf8")
    assert.match(source, /role="alertdialog" aria-modal="true"/)
    assert.match(source, /event\.key === "Escape"/)
    assert.match(source, /cancelRef\.current\?\.focus/)
    assert.match(source, /<p>\{targetLabel\(context\)\}<\/p>/)
    const en = JSON.parse(readFileSync(resolve(here, "../../public/catalogs/en.json"), "utf8"))
    const zh = JSON.parse(readFileSync(resolve(here, "../../public/catalogs/zh-Hant.json"), "utf8"))
    for (const key of Object.keys(en).filter((key) => key.startsWith("next.cloudAction"))) {
      assert.equal(typeof zh[key], "string", `${key} needs Taiwan Traditional Chinese`)
      assert.deepEqual([...en[key].matchAll(/\{\w+\}/g)].map((match) => match[0]).sort(),
        [...zh[key].matchAll(/\{\w+\}/g)].map((match) => match[0]).sort(), key)
    }
  } finally {
    try { unlinkSync(output) } catch { /* build failed before writing */ }
  }
})
