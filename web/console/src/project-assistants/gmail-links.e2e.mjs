import assert from "node:assert/strict"
import { createServer } from "node:http"
import { readFile } from "node:fs/promises"
import { fileURLToPath, pathToFileURL } from "node:url"
import { basename } from "node:path"
import { build } from "esbuild"

const { PLAYWRIGHT_MODULE } = process.env
assert.ok(PLAYWRIGHT_MODULE, "Set PLAYWRIGHT_MODULE")
const pw = await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const bundle = await build({
  entryPoints: [fileURLToPath(new URL("fixtures.ts", import.meta.url))],
  outfile: "project-assistants-fixture.js", bundle: true, write: false,
  format: "esm", platform: "browser", jsx: "automatic",
})
const files = new Map(bundle.outputFiles.map(file => ["/" + basename(file.path), file.contents]))
files.set("/", await readFile(new URL("../../tools/fixtures/project-assistants.html", import.meta.url)))
const server = createServer((req, res) => {
  const path = new URL(req.url, "http://fixture").pathname
  const type = path === "/" ? "text/html" : path.endsWith(".css") ? "text/css" : "text/javascript"
  if (!files.has(path)) return res.writeHead(404).end()
  res.setHeader("Content-Type", type + "; charset=utf-8")
  res.end(files.get(path))
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const fixture = `http://127.0.0.1:${server.address().port}`
const original = "https://mail.google.com/mail/u/1/?view=pt#all/synthetic-thread"
const note = `[Gmail original](${original}) · [Other link](https://mail.google.com.example.invalid/mail/u/1/#all/thread)`

try {
  for (const engine of ["chromium", "webkit"]) {
    const browser = await pw[engine].launch(engine === "chromium" ? { args: ["--disable-features=OmniboxEverywhere"] } : {})
    try {
      for (const [language, width] of [["en", 1440], ["zh-Hant", 390]]) {
        const zh = language === "zh-Hant"
        const context = await browser.newContext({ viewport: { width, height: 844 }, hasTouch: zh })
        await context.route("**/*", route => {
          const url = new URL(route.request().url())
          if (url.origin === fixture) return route.continue()
          if (url.origin === "https://mail.google.com") return route.fulfill({ body: "Synthetic Gmail original" })
          return route.abort()
        })
        const page = await context.newPage()
        const errors = []
        page.on("pageerror", error => errors.push(error.message))
        await page.goto(fixture + "/?lang=" + language)
        await page.waitForFunction(() => !!window.botsFixture)
        await page.evaluate(text => {
          window.botsFixture.a.emitMessage(text)
          window.botsFixture.open()
          window.addEventListener("click", event => {
            if (event.target instanceof HTMLAnchorElement && event.target.closest(".pa-markdown")) {
              document.body.dataset.linkHandled = String(event.defaultPrevented)
              event.preventDefault()
            }
          })
        }, note)
        await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']').click()
        const gmail = page.getByRole("link", { name: "Gmail original", exact: true })
        await gmail.click()
        assert.equal(await page.locator("body").getAttribute("data-link-handled"), "false")
        await page.evaluate(() => {
          Object.defineProperty(navigator, "platform", { value: "iPhone" })
          Object.defineProperty(navigator, "standalone", { value: true })
        })
        await page.getByRole("link", { name: "Other link", exact: true }).click()
        assert.equal(await page.locator("body").getAttribute("data-link-handled"), "false")
        await gmail.press("Enter")
        const dialog = page.getByRole("dialog", { name: zh ? "開啟 Gmail 原信" : "Open Gmail message" })
        const safari = dialog.getByRole("link", { name: zh ? "用 Safari 開啟" : "Open in Safari" })
        assert.equal(await safari.getAttribute("href"), "x-safari-" + original)
        await page.keyboard.press("Escape")
        await dialog.waitFor({ state: "detached" })
        assert.equal(await gmail.evaluate(link => document.activeElement === link), true)
        await gmail.click()
        const [webMail] = await Promise.all([
          context.waitForEvent("page"),
          dialog.getByRole("link", { name: zh ? "用 Gmail 網頁開啟" : "Open Gmail on the web" }).click(),
        ])
        await webMail.getByText("Synthetic Gmail original").waitFor()
        assert.equal(webMail.url(), original)
        assert.deepEqual(errors, [])
        await context.close()
        console.log(`PASS ${engine} ${language}: Safari URL, web fallback, and ordinary links`)
      }
    } finally { await browser.close() }
  }
} finally {
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
}
