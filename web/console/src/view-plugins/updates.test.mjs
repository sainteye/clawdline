import assert from "node:assert/strict"
import test from "node:test"
import { mkdtemp, readFile, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { pathToFileURL, fileURLToPath } from "node:url"
import { spawnSync } from "node:child_process"
import { build } from "esbuild"

const temp = await mkdtemp(join(tmpdir(), "cl-view-updates-test-")), file = join(temp, "updates.mjs")
await build({ entryPoints: [fileURLToPath(new URL("./updates.ts", import.meta.url))], bundle: true, platform: "node", format: "esm", outfile: file })
const { parseOfficialCatalog, isOfficialRevision, automaticUpdateAllowed, readOfficialCatalog, downloadOfficialRelease, updateBytes } = await import(pathToFileURL(file).href)
test.after(() => rm(temp, { recursive: true, force: true }))
const catalog = JSON.parse(await readFile(new URL("../../public/view-plugins/catalog.json", import.meta.url), "utf8")), allowed = catalog.packages.map(p => p.id), release = catalog.packages.find(p => p.id.endsWith("session-galaxy"))
const row = { id: release.id, official: true, autoUpdate: true, active: { digest: release.knownDigests[0], manifest: { version: "0.0.1", capabilities: ["sessions.read", "sessions.open"] } } }

test("The official catalog exactly identifies author manifests and shipped archive bytes", () => {
  assert.deepEqual(parseOfficialCatalog(catalog, allowed), catalog)
  const root = fileURLToPath(new URL("../../../../", import.meta.url)), result = spawnSync("python3", ["tools/build-view-plugin-catalog.py", "--check"], { cwd: root, encoding: "utf8" })
  assert.equal(result.status, 0, result.stderr + result.stdout)
})
test("Malformed, duplicate, unbounded and redirected catalog entries cannot become update sources", () => {
  for (const patch of [{ url: "https://other.example/plugin.zip" }, { url: "/view-plugins/../secret.zip" }, { sha256: "wrong" }, { bytes: Infinity }, { capabilities: ["sessions.read", "native.run"] }]) {
    assert.throws(() => parseOfficialCatalog({ ...catalog, packages: [{ ...release, ...patch }] }, allowed), e => e.code === "update_catalog_invalid")
  }
  assert.throws(() => parseOfficialCatalog({ ...catalog, packages: [release, release] }, allowed), e => e.code === "update_catalog_invalid")
  assert.deepEqual(parseOfficialCatalog(catalog, []), { schemaVersion: 1, packages: [] }, "A newer host's added views do not break older allowlists")
})
test("Only known exact official bytes and same-permission newer versions qualify automatically", () => {
  assert.equal(isOfficialRevision(row, release), true)
  assert.equal(isOfficialRevision({ ...row, active: { ...row.active, digest: "f".repeat(64) } }, release), false)
  assert.equal(automaticUpdateAllowed(row, release), true)
  for (const changed of [{ official: false }, { autoUpdate: false }, { active: { ...row.active, manifest: { ...row.active.manifest, capabilities: ["sessions.read"] } } }, { active: { ...row.active, manifest: { ...row.active.manifest, version: "99.0.0" } } }]) assert.equal(automaticUpdateAllowed({ ...row, ...changed }, release), false)
  assert.equal(automaticUpdateAllowed(row, { ...release, hostApi: 2 }), false)
})
test("Downloads ignore caches, reject redirects, match the catalog digest and keep failures typed", async () => {
  const original = globalThis.fetch, archive = await readFile(new URL("../../public" + release.url, import.meta.url)), calls = []
  try {
    globalThis.fetch = async (url, options) => { calls.push({ url, options }); return new Response(url.endsWith("catalog.json") ? JSON.stringify(catalog) : archive) }
    const signal = new AbortController().signal
    assert.deepEqual(await readOfficialCatalog(allowed, signal), catalog)
    const value = await downloadOfficialRelease(release, signal); assert.equal(value.pkg.digest, release.sha256)
    assert.ok(calls.every(call => call.options.cache === "no-store" && call.options.redirect === "error" && call.options.credentials === "same-origin"))
    await assert.rejects(downloadOfficialRelease({ ...release, sha256: "0".repeat(64) }, signal), e => e.code === "update_package_mismatch")
    await assert.rejects(updateBytes(release.url, 1, signal), e => e.code === "package_too_large")
    globalThis.fetch = async () => new Response("<html>SPA fallback</html>")
    await assert.rejects(readOfficialCatalog(allowed, signal), e => e.code === "update_catalog_invalid")
    globalThis.fetch = async () => new Response("offline", { status: 503 })
    await assert.rejects(readOfficialCatalog(allowed, signal), e => e.code === "package_download_failed")
  } finally { globalThis.fetch = original }
})
