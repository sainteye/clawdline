/** Free local browser harness for the Bots tower: Chromium and WebKit, 390 and
 * 1440 wide, Traditional Chinese and English. It serves only an in-memory
 * bundle of the synthetic Bots fixture, the tower pictures and the fixture
 * HTML; any /v1 request fails the run. It installs nothing and calls no model.
 *
 *   PLAYWRIGHT_MODULE=<existing-playwright-index.mjs> \
 *   BOTS_ARTIFACTS=<task-evidence-directory> \
 *   node web/console/src/project-assistants/tower/tower.e2e.mjs
 *
 * It checks what has broken before: name tags and the collapsed controls on both
 * screen sizes, a floor picture that never arrives, "+N" sliding the floor
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
  const workingName = await page.evaluate(async () => {
    localStorage.clear()
    const { a, b, open } = window.botsFixture
    for (const source of [a, b]) {
      const working = source.bots[2]
      const list = source.list.bind(source)
      source.list = async (...args) => {
        const result = await list(...args)
        return { ...result, items: result.items.map(item => item.bot.key.bot_id === working.key.bot_id
          ? { ...item, status: "working", activity: "thinking" } : item) }
      }
      const bot = source.bots[5]
      await source.markRead({
        command: { key: bot.key, action: "read", idempotency_key: "fixture-read" },
        cursor: { key: bot.key, conversation_id: bot.conversation_id, message_seq: 2, reader_id: "fixture-reader" },
      }, new AbortController().signal)
      source.scriptMessage(bot, "extra-reply", "assistant", "A reply from a crowded zone.", "extra-turn")
    }
    open()
    return a.bots[2].name
  })
  // Every fixture Bot, so the building is offered (it needs seven across two Projects).
  const more = page.getByRole("button", { name: w.more, exact: true })
  await more.waitFor({ timeout: 10_000 }).catch(() => {})
  for (let i = 0; i < 5 && await more.isVisible().catch(() => false); i++) { await more.click(); await page.waitForTimeout(300) }
  // A wide screen opens on the tower until the person chooses; a phone on the list.
  const toggle = page.getByRole("button", { name: w.towerView, exact: true })
  if (await toggle.getAttribute("aria-pressed") !== "true") await toggle.click()
  const scene = page.locator(".pa-tower-scene")
  await scene.waitFor()
  const restingBot = scene.locator('.pa-tower-bot:is([data-mood="idle"], [data-mood="recent"], [data-mood="sleep"])').first()
  const restingTag = restingBot.locator(".pa-tower-tip")
  const workingTag = scene.locator('.pa-tower-bot[data-mood="work"] .pa-tower-tip').first()
  const assertWorkingTag = async () => {
    assert.equal(await workingTag.getByText(workingName, { exact: true }).isVisible(), true, "a working Bot has no pinned name")
    assert.equal(await workingTag.getByText(w.towerMoodWork).isVisible(), true, "a working Bot has no visible working state")
  }
  await restingBot.waitFor()
  const menu = page.getByRole("button", { name: w.towerFloorsMenu, exact: true })
  const controls = page.locator(".pa-tower-controls")
  const toggleTags = page.getByRole("button", { name: w.towerTags, exact: true })
  assert.equal(await menu.isVisible(), true, "the floor menu button is missing")
  assert.equal(await controls.isVisible(), false, "the controls start expanded")
  assert.equal(await restingTag.isVisible(), true, "resting Bot has no name tag with tags on")
  await menu.click()
  await toggleTags.click()
  assert.equal(await toggleTags.getAttribute("aria-pressed"), "false")
  assert.equal(await restingTag.isVisible(), false, "a name tag is still pinned with tags off")
  await assertWorkingTag()
  assert.equal(await workingTag.getByText(w.thinking, { exact: true }).isVisible(), true, "a working Bot's current activity is hidden on the floor")
  await shot("tower-working-tags-off")
  const unreadBadge = scene.locator(".pa-tower-bot").getByText(w.towerMoodUnread, { exact: true }).first()
  assert.equal(await unreadBadge.isVisible(), true, "a Bot with a new reply has no visible badge with name tags off")
  if (!phone) {
    await restingBot.hover()
    assert.equal(await restingTag.isVisible(), true, "a resting Bot has no tooltip on hover with tags off")
  }
  await toggleTags.click()
  assert.equal(await restingTag.isVisible(), true, "turning name tags back on did not restore the resting Bot's name")
  await assertWorkingTag()
  await toggleTags.press("Escape")
  assert.equal(await controls.isVisible(), false, "Escape did not close the controls")
  assert.equal(await menu.evaluate(el => document.activeElement === el), true, "Escape did not return focus to the menu button")
  checks.push("name tags toggle on resting Bots and menu closes with Escape")

  // Twelve Bots on two machines' Projects: the building is offered.
  await menu.click()
  if (!phone) {
    const hud = await box(page.locator(".pa-hud-facts"))
    assert.equal(overlap(hud, await box(menu)), false, "the menu button covers the HUD")
    assert.equal(overlap(hud, await box(controls)), false, "the open controls cover the HUD")
  }
  const chooseBuilding = page.getByRole("button", { name: w.towerLayoutBuilding, exact: true })
  await chooseBuilding.click()
  assert.equal(await controls.isVisible(), false, "the controls stay open after choosing a layout")
  await menu.click()
  await shot("tower-controls")
  const elevator = page.getByRole("navigation", { name: w.towerElevator })
  await elevator.getByRole("button", { name: w.towerSection, exact: true }).click()
  assert.equal(await controls.isVisible(), false, "the controls stay open after choosing the building overview")
  const sectionTag = restingTag
  assert.equal(await sectionTag.isVisible(), true, "the building overview has no pinned name tags")
  await menu.click()
  await toggleTags.click()
  assert.equal(await sectionTag.isVisible(), false, "a resting name tag is still pinned in the building overview with tags off")
  await assertWorkingTag()
  checks.push("working name tags and states stay visible with tags on and off on the floor and with tags off in the building overview")
  assert.equal(await unreadBadge.isVisible(), true, "a Bot with a new reply has no visible badge in the building overview")
  checks.push("new reply badges stay visible on a floor and in the building overview with name tags off")
  await toggleTags.click()
  await elevator.locator("button[aria-pressed]:enabled").nth(1).click()
  assert.equal(await controls.isVisible(), false, "the controls stay open after choosing a floor")
  checks.push("building and floor selection close the menu and name tags toggle in the building overview")

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
  const crowd = page.locator(".pa-tower-more > button").filter({ hasText: w.towerMoodUnread }).first()
  await crowd.waitFor({ timeout: 5000 })
  const before = await scene.evaluate(el => [el.scrollLeft, el.scrollTop])
  await crowd.click()
  const hiddenBot = page.locator(".pa-tower-more ul").getByRole("button", { name: new RegExp(w.towerMoodUnread) })
  await hiddenBot.first().waitFor({ timeout: 5000 })
  assert.deepEqual(await scene.evaluate(el => [el.scrollLeft, el.scrollTop]), before, "opening +N moved the floor")
  await shot("tower-more")
  await crowd.click()
  checks.push("+N shows hidden unread Bots and opens in place")

  // A Bot's card, then its chat: a phone keeps the tower above a bottom sheet.
  const bot = scene.locator(".pa-tower-bot").filter({ has: page.getByText(w.towerMoodUnread, { exact: true }) }).first()
  const key = await bot.getAttribute("data-bot-key")
  const readBot = scene.locator(`[data-bot-key=${JSON.stringify(key)}]`)
  await bot.getByText(w.towerMoodUnread, { exact: true }).click()
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
    // Swiped up the sheet fills the screen; swiped down it returns to half, then closes.
    await page.mouse.move(x, y); await page.mouse.down(); await page.mouse.move(x, y - 160, { steps: 12 }); await page.mouse.up()
    await page.waitForFunction(() => { const r = document.querySelector(".pa-main").getBoundingClientRect(); return r.top <= 1 && r.height >= innerHeight - 1 })
    checks.push("swiping the sheet up fills the screen")
    await shot("tower-sheet-full")
    const fullGrip = await box(page.locator(".pa-sheet-grip")), fx = fullGrip.x + fullGrip.width / 2, fy = fullGrip.y + fullGrip.height / 2
    await page.mouse.move(fx, fy); await page.mouse.down(); await page.mouse.move(fx, fy + 160, { steps: 12 }); await page.mouse.up()
    await page.waitForFunction(() => document.querySelector(".pa-main").getBoundingClientRect().top > 60)
    assert.equal(await page.getByRole("button", { name: w.towerBack }).isVisible(), true, "swiping down from full closed the chat instead of returning to half")
    checks.push("swiping a full sheet down returns it to half")
    const half = await box(page.locator(".pa-sheet-grip")), hx = half.x + half.width / 2, hy = half.y + half.height / 2
    await page.mouse.move(hx, hy); await page.mouse.down(); await page.mouse.move(hx, hy + 160, { steps: 12 }); await page.mouse.up()
    await page.getByRole("button", { name: w.towerBack }).waitFor({ state: "hidden" })
    await scene.waitFor()
    await readBot.waitFor()
    await readBot.getByText(w.towerMoodUnread, { exact: true }).waitFor({ state: "hidden" })
    checks.push("reading a reply clears its scene badge", "swiping the sheet down closes it")
    await shot("tower-swiped")
    return checks
  } else {
    assert.equal(await main.evaluate(el => getComputedStyle(el).position), "static")
    checks.push("wide chat in the main area")
  }
  await shot("tower-chat")
  await page.getByRole("button", { name: w.towerBack }).click()
  await scene.waitFor()
  await readBot.waitFor()
  await readBot.getByText(w.towerMoodUnread, { exact: true }).waitFor({ state: "hidden" })
  checks.push("reading a reply clears its scene badge", "back to the tower")
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
