import assert from "node:assert/strict"
import { test } from "node:test"
import { fileURLToPath } from "node:url"
import { resolveConfig } from "vite"
import { productionCloud, validateHostedDeclaration } from "./hosted-declaration.mjs"
import { validateHostedBundle } from "./hosted-bundle.mjs"

const configFile = fileURLToPath(new URL("../vite.config.ts", import.meta.url))

test("the actual Vite build refuses the incident's truthy non-object declaration", async () => {
  const previous = process.env.VITE_HOSTED_CONSOLE
  process.env.VITE_HOSTED_CONSOLE = "1"
  try {
    await assert.rejects(resolveConfig({ configFile, mode: "production" }, "build"), /VITE_HOSTED_CONSOLE/)
  } finally {
    if (previous === undefined) delete process.env.VITE_HOSTED_CONSOLE
    else process.env.VITE_HOSTED_CONSOLE = previous
  }
})

test("local builds remain local while malformed hosted declarations fail closed", () => {
  assert.equal(validateHostedDeclaration(undefined), null)
  assert.equal(validateHostedDeclaration(""), null)
  for (const raw of ["1", "true", "null", "[]", "{}", "not JSON",
    JSON.stringify({ ...productionCloud, relay_url: "https://relay.clawdline.com/v1/connect" }),
    JSON.stringify({ ...productionCloud, api_origin: "http://api.clawdline.com" }),
    JSON.stringify({ ...productionCloud, v: 2 })]) {
    assert.throws(() => validateHostedDeclaration(raw), /VITE_HOSTED_CONSOLE/)
  }
  assert.deepEqual(validateHostedDeclaration(JSON.stringify(productionCloud)), {
    appOrigin: productionCloud.app_origin, apiOrigin: productionCloud.api_origin,
    relayURL: productionCloud.relay_url, build: "", strings: {},
  })
})

test("the served bundle check refuses CloudGate with the incident's invalid declaration", () => {
  assert.throws(() => validateHostedBundle('const declared="1";import("./CloudGate.js")'), /no usable production/)
  assert.throws(() => validateHostedBundle('import("./CloudGate.js")'), /no usable production/)
  const valid = `const declared=${JSON.stringify(JSON.stringify(productionCloud))};import("./CloudGate.js")`
  assert.equal(validateHostedBundle(valid), true)
  assert.throws(() => validateHostedBundle(valid + "))"), /not valid JavaScript/)
  const wrong = { ...productionCloud, api_origin: "https://example.com" }
  assert.throws(() => validateHostedBundle(`const declared=${JSON.stringify(JSON.stringify(wrong))}`), /no usable production/)
})
