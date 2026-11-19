/** Real App shell, synthetic local data, no daemon/provider access or package installation. */
import { hostBundle } from "./project-assistants-host.e2e.mjs"
import { createServer } from "node:http"
import { readFile, mkdir, writeFile } from "node:fs/promises"
import { pathToFileURL } from "node:url"
import assert from "node:assert/strict"
const artifacts = process.env.STATUS_BAR_ARTIFACTS
if (!artifacts || !process.env.PLAYWRIGHT_MODULE) throw Error("Set STATUS_BAR_ARTIFACTS and PLAYWRIGHT_MODULE")
await mkdir(artifacts, { recursive: true })
const { html, files } = await hostBundle(), network = [], results = []
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, "http://fixture")
    if (url.pathname.startsWith("/v1/")) { network.push(url.pathname); res.writeHead(500); res.end(); return }
    let body = url.pathname === "/" ? html : files.get(url.pathname)
    if (/^\/catalogs\/(en|zh-Hant)\.json$/.test(url.pathname)) body = await readFile(new URL("../public" + url.pathname, import.meta.url))
    if (body === undefined) { res.writeHead(404); res.end(); return }
    res.setHeader("Content-Type", url.pathname.endsWith(".js") ? "text/javascript" : url.pathname.endsWith(".css") ? "text/css" : url.pathname.endsWith(".json") ? "application/json" : "text/html"); res.end(body)
  } catch (e) { res.writeHead(500); res.end(String(e)) }
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const pw = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
try {
  for (const engine of ["chromium", "webkit"]) {
    const browser = await pw[engine].launch({ headless: true, ...(engine === "chromium" ? { args: ["--disable-features=OmniboxEverywhere"] } : {}) })
    try { for (const width of [320, 390, 1440]) for (const lang of ["en", "zh-Hant"]) {
      const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" }), errors = []
      page.on("pageerror", e => errors.push(e.message))
      const name = `${engine}-${width}-${lang}`
      const check = async (label, run) => {
        try { await run(); results.push({ name, label, passed: true }) }
        catch (e) { results.push({ name, label, passed: false, error: String(e) }) }
        await page.screenshot({ path: `${artifacts}/${name}-${label}.png` })
      }
      try {
        await page.route("**/v1/**", route => { network.push(route.request().url()); return route.abort() })
        await page.goto(`http://127.0.0.1:${server.address().port}/?lang=${lang}&quota=hold&hold-status`)
        await page.locator("#filter").waitFor()
        const bar = page.locator(".provider-quota-bar")
        const bots = bar.locator(".pa-dock"), views = bar.locator(".vp-dock"), boards = bar.locator(".session-board-dock")
        const shortcutsVisible = async () => {
          for (const button of [bots, views, boards]) {
            const box = await button.boundingBox()
            assert.ok(box && box.x >= 0 && box.x + box.width <= width && box.y >= 0 && box.y + box.height <= 900, JSON.stringify(box))
          }
        }
        const shortcutsHidden = async () => {
          assert.equal(await bar.count(), 0)
          assert.equal(await page.locator(".pa-dock,.vp-dock,.session-board-dock").count(), 0)
          assert.equal(await page.evaluate(() => document.documentElement.style.getPropertyValue("--quota-footer-height")), "")
        }
        const layout = width < 900 ? "mobile" : "desktop"
        await check("initial-loading", async () => {
          await page.waitForFunction(() => fixtureHost.log.some(row => row.method === "status-wait"))
          await shortcutsVisible()
          assert.equal(await bots.isDisabled(), true)
          assert.equal(await boards.isDisabled(), true)
          assert.equal(await views.isEnabled(), true)
          assert.equal(await bar.locator('[data-reading-state="loading"]').count(), 1)
          assert.ok(await page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--quota-footer-height"))) > 0)
        })
        await page.evaluate(() => { fixtureHost.releaseStatus(); fixtureHost.allowBoards() })
        await page.waitForFunction(() => document.querySelector(".pa-dock:enabled"))
        await check("sources-ready", async () => {
          await shortcutsVisible()
          assert.equal(await boards.isEnabled(), true)
        })
        await page.evaluate(() => fixtureHost.quota("empty")); await page.waitForTimeout(80)
        await check("empty", async () => {
          await shortcutsVisible()
          assert.equal(await bar.locator('[data-reading-state="empty"]').count(), 1)
        })
        await page.evaluate(() => fixtureHost.quota("ready"))
        // A remount uses the new source and its own initial reading, like a Cloud machine switch.
        await page.evaluate(() => fixtureHost.remount()); await page.waitForTimeout(100)
        await check("zero-reading", async () => { assert.equal(await bar.locator('[data-provider="codex"]').count(), 1) })
        await page.evaluate(() => fixtureHost.quota("error")); await bar.locator(".quota-refresh").click(); await page.waitForTimeout(100)
        await check("stale-reading", async () => { assert.equal(await bar.locator('[data-provider="codex"][data-stale]').count(), 1) })
        await page.evaluate(layout => fixtureHost.quotaVisible(layout, false), layout)
        await bar.waitFor({ state: "detached" })
        await check("quotas-off", shortcutsHidden)
        await page.evaluate(layout => fixtureHost.quotaVisible(layout, true), layout)
        await bar.waitFor()
        await check("quotas-on", shortcutsVisible)
        await views.click()
        await page.locator(".vp-host").waitFor()
        await page.locator(".vp-close").click()
        await page.locator(".vp-host").waitFor({state:"hidden"})
        await check("views-return", shortcutsVisible)

        await boards.click()
        const board = page.locator(".session-board-dialog[open]")
        await board.waitFor()
        await board.locator("header button").first().click()
        await board.waitFor({ state: "hidden" })
        await check("boards-return", shortcutsVisible)

        await bots.click()
        await page.locator(".pa-window .pa-bot-row").first().waitFor()
        await page.locator(".pa-window .pa-collapse").click()
        await page.locator(".pa-window").waitFor({ state: "hidden" })
        await page.waitForFunction(() => document.activeElement?.classList.contains("pa-dock"))
        await check("bots-return", shortcutsVisible)

        await page.evaluate(() => fixtureHost.quota("ready"))
        if (width < 900) {
          await page.locator("#rows > li.row").first().click()
          const detail = page.locator("#app[data-view=detail] .pane-detail").first()
          await detail.waitFor()
          await check("mobile-detail", async () => {
            await shortcutsHidden()
            const pane = await detail.boundingBox()
            assert.ok(pane && Math.abs(pane.y + pane.height - 900) <= 1, "the detail pane reaches the bottom of the viewport")
          })
          await page.evaluate(() => history.back())
          await page.locator("#app[data-view=list]").waitFor()
          await check("mobile-list-return", async () => {
            await shortcutsVisible()
            assert.equal(await bar.locator(".quota-providers").count(), 1)
          })
        }
        await page.locator("#brand").click()
        await page.locator("#nav-work").click()
        await page.locator('#nav-work[aria-current="page"]').waitFor({ state: "attached" })
        await check("work-page", layout === "mobile" ? shortcutsHidden : shortcutsVisible)
        assert.deepEqual(errors, [])
      } finally { await page.close() }
    } } finally { await browser.close() }
  }
  assert.deepEqual(network, [])
} finally { await new Promise(resolve => server.close(resolve)); await writeFile(`${artifacts}/results.json`, JSON.stringify({ results, network }, null, 2)) }
const failures = results.filter(r => !r.passed)
console.log(JSON.stringify({ checks: results.length, failures }, null, 2))
assert.equal(failures.length, 0)
