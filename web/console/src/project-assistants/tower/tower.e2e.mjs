/** Free local browser harness for the Bots tower: Chromium and WebKit, 390 and
 * 1440 wide, Traditional Chinese and English. It serves only an in-memory
 * bundle of the synthetic Bots fixture, the tower pictures and the fixture
 * HTML; any /v1 request fails the run. It installs nothing and calls no model.
 *
 *   PLAYWRIGHT_MODULE=<existing-playwright-index.mjs> \
 *   BOTS_ARTIFACTS=<task-evidence-directory> \
 *   node web/console/src/project-assistants/tower/tower.e2e.mjs
 *
 * It checks what has broken before: the phone's floor button showing over the
 * desktop HUD, a floor picture that never arrives, "+N" sliding the floor
 * sideways, and on a phone the chat rising as a sheet with the tower above. */
import assert from "node:assert/strict"
import { createServer } from "node:http"
import { readFile, mkdir, writeFile } from "node:fs/promises"
import { fileURLToPath, pathToFileURL } from "node:url"
import { createRequire } from "node:module"
import path from "node:path"
import { build } from "esbuild"

const artifacts = process.env.BOTS_ARTIFACTS
if (!artifacts) throw new Error("Set BOTS_ARTIFACTS to a task-owned evidence directory")
await mkdir(artifacts, { recursive: true })
if (!process.env.PLAYWRIGHT_MODULE) throw new Error("Set PLAYWRIGHT_MODULE to an existing local Playwright index.mjs; this harness installs nothing")
const { chromium, webkit } = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const here = fileURLToPath(new URL(".", import.meta.url)), bots = path.join(here, "..")

const bundle = await build({ entryPoints: [path.join(bots, "fixtures.ts")], outfile: "project-assistants-fixture.js", bundle: true, write: false, format: "esm", platform: "browser", jsx: "automatic", define: { "process.env.NODE_ENV": '"development"' } })
const content = new Map(bundle.outputFiles.map(f => ["/" + path.basename(f.path), f.text]))
const html = await readFile(new URL("../../../tools/fixtures/project-assistants.html", import.meta.url), "utf8")
const pictures = new Map()
for (const name of ["ramen", "skyline", "rain", "platform", "building"]) pictures.set(`/${name}.webp`, await readFile(path.join(here, `${name}.webp`)))
// The labels come from the console's own words, never copied here.
const wordsBundle = await build({ stdin: { contents: 'export { words } from "./words"', resolveDir: bots, loader: "ts" }, bundle: true, platform: "node", format: "cjs", write: false })
const loaded = { exports: {} }
new Function("require", "module", "exports", wordsBundle.outputFiles[0].text)(createRequire(import.meta.url), loaded, loaded.exports)
const { words } = loaded.exports

let forbidden = 0
const server = createServer((req, res) => {
  const url = new URL(req.url, "http://fixture")
  if (url.pathname.startsWith("/v1")) { forbidden++; res.writeHead(500); res.end("No live API in fixture"); return }
  const picture = pictures.get(url.pathname)
  if (picture) { res.setHeader("Content-Type", "image/webp"); res.end(picture); return }
  const text = url.pathname === "/" ? html : content.get(url.pathname)
  if (text === undefined) { res.writeHead(404); res.end(); return }
  res.setHeader("Content-Type", url.pathname.endsWith(".js") ? "text/javascript" : url.pathname.endsWith(".css") ? "text/css" : "text/html")
  res.end(text)
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const origin = `http://127.0.0.1:${server.address().port}`, results = [], failures = []

const box = locator => locator.boundingBox()
const overlap = (a, b) => !!a && !!b && a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height

async function towerChecks(page, width, language, shot) {
  const w = words(language), phone = width < 701, checks = []
  await page.goto(`${origin}/?lang=${language}`)
  await page.evaluate(() => { try { localStorage.clear() } catch {} ; window.botsFixture.open() })
  // Every fixture Bot, so the building is offered (it needs seven across two Projects).
  const more = page.getByRole("button", { name: w.more, exact: true })
  await more.waitFor({ timeout: 10_000 }).catch(() => {})
  for (let i = 0; i < 5 && await more.isVisible().catch(() => false); i++) { await more.click(); await page.waitForTimeout(300) }
  // A wide screen opens on the tower until the person chooses; a phone on the list.
  const toggle = page.getByRole("button", { name: w.towerView, exact: true })
  if (await toggle.getAttribute("aria-pressed") !== "true") await toggle.click()
  const scene = page.locator(".pa-tower-scene")
  await scene.waitFor()
  // Twelve Bots on two machines' Projects: the building is offered.
  if (phone) await page.getByRole("button", { name: w.towerFloorsMenu, exact: true }).click()
  await page.getByRole("button", { name: w.towerLayoutBuilding, exact: true }).click()
  // A phone folds the elevator back behind its floor button once a layout is chosen.
  await page.locator(".pa-tower-elevator").waitFor({ state: "attached" })
  checks.push("building offered and chosen")

  // The phone's floor button never shows on a wide screen, and nothing sits on the HUD.
  const floorButton = page.locator(".pa-tower-floorbtn")
  if (!phone) {
    assert.equal(await floorButton.isVisible(), false, "the phone's floor button shows on a wide screen")
    const hud = await box(page.locator(".pa-hud-facts"))
    assert.equal(overlap(hud, await box(page.locator(".pa-tower-controls"))), false, "the controls cover the HUD")
    checks.push("no phone button or controls over the HUD")
  } else {
    assert.equal(await floorButton.isVisible(), true)
    checks.push("phone floor button shown")
  }

  // The floor's picture arrives as an image, not an HTML stand-in.
  await page.waitForFunction(() => { const img = document.querySelector(".pa-tower-scene img"); return !!img && img.complete && img.naturalWidth > 0 })
  assert.equal(await page.locator(".pa-tower-picture-missing").count(), 0, "a floor picture did not load")
  // The elevator doors end open, with or without motion (this run reduces it).
  await page.waitForTimeout(700)
  const shut = await page.evaluate(() => { const d = document.querySelector(".pa-tower-doors"); if (!d) return false; const t = getComputedStyle(d, "::before").transform; return t === "none" || t === "matrix(1, 0, 0, 1, 0, 0)" })
  assert.equal(shut, false, "the elevator doors stay shut over the floor")
  checks.push("floor picture loaded")
  await shot("tower-building")

  // A crowded lounge counts the rest; opening the list does not move the floor.
  const crowd = page.locator(".pa-tower-more > button").first()
  if (await crowd.count()) {
    const before = await scene.evaluate(el => [el.scrollLeft, el.scrollTop])
    await crowd.click()
    await page.locator(".pa-tower-more ul").waitFor()
    assert.deepEqual(await scene.evaluate(el => [el.scrollLeft, el.scrollTop]), before, "opening +N moved the floor")
    await shot("tower-more")
    await crowd.click()
    checks.push("+N list opens in place")
  }

  // A Bot's card, then its chat: a phone keeps the tower above a bottom sheet.
  const bot = page.locator(".pa-tower-bot").first()
  await bot.click()
  await page.locator(".pa-tower-card").waitFor()
  await page.getByRole("button", { name: w.towerOpenChat, exact: true }).click()
  const main = page.locator(".pa-main")
  await page.getByRole("button", { name: w.towerBack }).waitFor()
  if (phone) {
    assert.equal(await main.evaluate(el => getComputedStyle(el).position), "fixed", "the chat is not a sheet")
    const sheet = await box(main), viewport = page.viewportSize()
    assert.ok(sheet && sheet.y > 60 && sheet.y + sheet.height <= viewport.height + 1, "the sheet leaves the tower in view")
    assert.equal(await page.locator(".pa-tower").isVisible(), true, "the tower is hidden behind the chat")
    checks.push("phone chat is a bottom sheet over the tower")
    // A short slow drag springs back; a long one closes the sheet to the tower.
    const grip = await box(page.locator(".pa-sheet-grip")), x = grip.x + grip.width / 2, y = grip.y + grip.height / 2
    await page.mouse.move(x, y); await page.mouse.down(); await page.mouse.move(x, y + 30, { steps: 10 }); await page.waitForTimeout(400); await page.mouse.up()
    await page.waitForTimeout(300)
    assert.equal(await page.getByRole("button", { name: w.towerBack }).isVisible(), true, "a short drag closed the sheet")
    await page.mouse.move(x, y); await page.mouse.down(); await page.mouse.move(x, y + 160, { steps: 12 }); await page.mouse.up()
    await page.getByRole("button", { name: w.towerBack }).waitFor({ state: "hidden" })
    await scene.waitFor()
    checks.push("swiping the sheet down closes it")
    await shot("tower-swiped")
    return checks
  } else {
    assert.equal(await main.evaluate(el => getComputedStyle(el).position), "static")
    checks.push("wide chat in the main area")
  }
  await shot("tower-chat")
  await page.getByRole("button", { name: w.towerBack }).click()
  await scene.waitFor()
  checks.push("back to the tower")
  return checks
}

try {
  for (const engine of [chromium, webkit].filter(e => !process.env.BOTS_ENGINE || e.name() === process.env.BOTS_ENGINE))
    for (const width of process.env.BOTS_WIDTH ? [Number(process.env.BOTS_WIDTH)] : [390, 1440])
      for (const language of process.env.BOTS_LANGUAGE ? [process.env.BOTS_LANGUAGE] : ["zh-Hant", "en"]) {
        const browser = await engine.launch({ headless: true, ...(engine.name() === "chromium" ? { args: ["--disable-features=OmniboxEverywhere"] } : {}) }), name = `${engine.name()}-${width}-${language}`
        const page = await browser.newPage({ viewport: { width, height: width === 1440 ? 960 : 844 }, reducedMotion: "reduce" })
        const errors = []; page.on("pageerror", error => errors.push(error.message))
        await page.route("**/*", route => { const url = new URL(route.request().url()); if (url.origin !== origin || url.pathname.startsWith("/v1")) { forbidden++; return route.abort() } return route.continue() })
        const shot = code => page.screenshot({ path: path.join(artifacts, `${code}-${name}.png`) })
        try {
          const checks = await towerChecks(page, width, language, shot)
          assert.deepEqual(errors, [])
          results.push({ engine: engine.name(), width, language, status: "pass", checks })
        } catch (error) {
          await shot("tower-failure").catch(() => {})
          failures.push({ name, error: String(error?.stack ?? error) })
        } finally { await page.close(); await browser.close() }
      }
  assert.equal(forbidden, 0, "the fixture touched a /v1 API or another origin")
  if (failures.length) throw new Error(failures.map(f => `${f.name}: ${f.error}`).join("\n\n"))
} finally {
  await new Promise(resolve => server.close(resolve))
  await writeFile(path.join(artifacts, "tower-results.json"), JSON.stringify({ results, failures, forbidden, origin, synthetic: true, modelInvocations: 0 }, null, 2))
}
console.log(`tower e2e: ${results.length} passed`)
