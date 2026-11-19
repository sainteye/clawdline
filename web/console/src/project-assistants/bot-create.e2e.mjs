import assert from "node:assert/strict"
import { createServer } from "node:http"
import { readFile } from "node:fs/promises"
import { fileURLToPath, pathToFileURL } from "node:url"
import { basename } from "node:path"
import { build } from "esbuild"

assert.ok(process.env.PLAYWRIGHT_MODULE, "Set PLAYWRIGHT_MODULE")
const pw = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const bundle = await build({
  entryPoints: [fileURLToPath(new URL("fixtures.ts", import.meta.url))],
  outfile: "project-assistants-fixture.js", bundle: true, write: false,
  format: "esm", platform: "browser", jsx: "automatic",
})
const files = new Map(bundle.outputFiles.map(file => ["/" + basename(file.path), file.contents]))
files.set("/", await readFile(new URL("../../tools/fixtures/project-assistants.html", import.meta.url)))
for (const tag of ["en", "zh-Hant"]) {
  files.set(`/catalog/${tag}.json`, await readFile(new URL(`../../public/catalogs/${tag}.json`, import.meta.url)))
}
const server = createServer((req, res) => {
  const path = new URL(req.url, "http://fixture").pathname
  if (!files.has(path)) return res.writeHead(404).end()
  res.setHeader("Content-Type", path === "/" ? "text/html" : path.endsWith(".css") ? "text/css" : path.endsWith(".json") ? "application/json" : "text/javascript")
  res.end(files.get(path))
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
try {
  for (const engine of (process.env.BOTS_ENGINES ?? "chromium,webkit").split(",")) {
    const browser = await pw[engine].launch(engine === "chromium" ? { args: ["--disable-features=OmniboxEverywhere"] } : {})
    try {
      for (const [language, width] of [["zh-Hant", 390], ["en", 1440]]) {
        const zh = language === "zh-Hant"
        const context = await browser.newContext({ viewport: { width, height: 844 }, hasTouch: zh })
        const page = await context.newPage()
        await page.addInitScript(tag => localStorage.setItem("ui_language", tag), language)
        page.setDefaultTimeout(6000)
        const dialog = page.getByRole("dialog", { name: zh ? "新增 Bot" : "Create Bot" })
        const button = dialog.locator(".pa-create-bottom")
        async function open() {
          await page.goto(`http://127.0.0.1:${server.address().port}/?lang=${language}&header=1`)
          await page.waitForFunction(() => !!window.botsFixture)
          await page.evaluate(() => {
            document.documentElement.style.setProperty("--indicator", "34px")
            window.botsFixture.open()
          })
          await page.getByRole("button", { name: zh ? "新增" : "Add", exact: true }).click()
          await page.getByRole("menuitem", { name: zh ? "新增 Bot" : "Create Bot", exact: true }).click()
          await dialog.getByRole("textbox", { name: zh ? "名稱" : "Name", exact: true }).fill("Synthetic create")
          await dialog.getByRole("button", { name: /^Project\b/ }).click()
          await dialog.getByRole("option").first().click()
        }
        const requests = method => page.evaluate(method => window.botsFixture.log.filter(row => row.method === method), method)
        if (zh) {
          await open()
          await button.evaluate(node => node.scrollIntoView({ block: "end" }))
          const hit = await button.evaluate(node => {
            const r = node.getBoundingClientRect()
            return node.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2))
          })
          assert.equal(hit, true, "the machine connection footer must not cover the Create button")
        }
        await open()
        await page.evaluate(() => window.botsFixture.a.loseCreateReply())
        await button.click()
        await dialog.waitFor({ state: "hidden" })
        assert.equal((await requests("create")).length, 1)
        assert.equal((await requests("get")).length, 1, "look up the original Bot id to recover a lost reply")

        await open()
        await page.evaluate(() => window.botsFixture.a.unknownCreate(true))
        await button.click()
        await dialog.getByRole("alert").filter({ hasText: zh ? "還沒找到" : "hasn't found" }).waitFor()
        assert.equal((await requests("create")).length, 1, "never silently repeat a create")
        await page.evaluate(() => window.botsFixture.a.unknownCreate(false))
        await button.click()
        await dialog.waitFor({ state: "hidden" })
        const attempts = await requests("create")
        assert.equal(attempts.length, 2)
        assert.deepEqual(attempts[0].request, attempts[1].request)
        await context.close()
        console.log(`PASS ${engine} ${language}: create button, lost reply, same-command retry`)
      }
    } finally { await browser.close() }
  }
} finally {
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
}
