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
const server = createServer((req, res) => {
  const path = new URL(req.url, "http://fixture").pathname
  if (!files.has(path)) return res.writeHead(404).end()
  res.setHeader("Content-Type", path === "/" ? "text/html" : path.endsWith(".css") ? "text/css" : "text/javascript")
  res.end(files.get(path))
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const pageText = "BT /F1 24 Tf 30 100 Td (Verified Bot PDF) Tj ET"
const objects = [
  "<< /Type /Catalog /Pages 2 0 R >>",
  "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
  "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
  "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  `<< /Length ${pageText.length} >>\nstream\n${pageText}\nendstream`,
]
let pdf = "%PDF-1.4\n"
const offsets = objects.map((object, index) => {
  const offset = pdf.length
  pdf += `${index + 1} 0 obj\n${object}\nendobj\n`
  return String(offset).padStart(10, "0") + " 00000 n \n"
})
pdf += `xref\n0 6\n0000000000 65535 f \n${offsets.join("")}trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${pdf.length}\n%%EOF\n`

try {
  for (const engine of (process.env.BOTS_ENGINES ?? "chromium,webkit").split(",")) {
    const browser = await pw[engine].launch(engine === "chromium" ? { args: ["--disable-features=OmniboxEverywhere"] } : {})
    try {
      for (const [language, width] of [["zh-Hant", 390], ["en", 1440]]) {
        const zh = language === "zh-Hant"
        const context = await browser.newContext({ viewport: { width, height: 844 }, hasTouch: zh })
        const page = await context.newPage()
        await page.addInitScript(() => {
          const revoke = URL.revokeObjectURL.bind(URL)
          window.revokedPreviewURLs = new Set()
          URL.revokeObjectURL = url => {
            window.revokedPreviewURLs.add(url)
            revoke(url)
          }
        })
        page.setDefaultTimeout(15000)
        let downloads = 0
        page.on("download", () => downloads++)
        await page.goto(`http://127.0.0.1:${server.address().port}/?lang=${language}`)
        await page.waitForFunction(() => !!window.botsFixture)
        await page.evaluate(async pdf => {
          window.botsFixture.a.seedCollections(101,false)
          await window.botsFixture.a.shareFixtureFile("Synthetic report.pdf", pdf)
          window.botsFixture.open()
        }, pdf)
        await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']').click()
        const card = page.getByRole("button", { name: /Synthetic report\.pdf/ })
        await card.click()
        const dialog = page.getByRole("dialog", { name: "Synthetic report.pdf" })
        const open = dialog.getByRole("link", { name: zh ? "在瀏覽器開啟 PDF" : "Open PDF in browser" })
        const href = await open.getAttribute("href")
        assert.match(href, /^blob:/)
        assert.equal(await open.getAttribute("target"), "_blank")
        assert.equal(await open.getAttribute("download"), null)
        const result = await page.evaluate(async url => {
          const response = await fetch(url)
          return { type: response.headers.get("Content-Type"), content: await response.text() }
        }, href)
        assert.deepEqual(result, {type: "application/pdf", content: pdf})
        assert.equal(downloads, 0)
        await dialog.getByRole("button", { name: zh ? "關閉" : "Close", exact: true }).click()
        await dialog.waitFor({state:"detached"})
        await page.waitForFunction(url => window.revokedPreviewURLs.has(url), href)

        const traces = page.locator(".pa-trace").filter({hasText:zh ? "查看處理過程" : "View processing steps"}).last()
        await traces.waitFor()
        await page.locator(".pa-trace-more-btn").waitFor({state:"hidden"})
        await traces.locator("summary").click()
        const more = traces.getByRole("button", { name: zh ? "載入更早的處理過程" : "Load earlier processing steps" })
        await more.click()
        await page.waitForFunction(() => window.botsFixture.log.some(row => row.method === "view" && row.request.mode === "traces" && row.request.cursor))
        await more.waitFor({state:"hidden"})
        console.log(`PASS ${engine} ${language}: verified PDF view and trace pagination`)
        await page.reload()
        await page.waitForFunction(() => !!window.botsFixture)
        await page.evaluate(() => {
          window.botsFixture.a.seedInlineProgress()
          window.botsFixture.open()
        })
        await page.locator('.pa-bot-row[data-bot-key=\'["studio","bot-0"]\']').click()
        const chat=page.locator(".pa-chat")
        const update=chat.locator(".pa-progress").and(chat.getByRole("group",{name:zh?"進度更新":"Progress update"}))
        await update.getByText("I found another result.").waitFor()
        assert.deepEqual(await update.allTextContents(),["Checking the integration.","I found another result."])
        const readingOrder=await chat.locator(".pa-progress,.pa-message-bubble").allTextContents()
        const relevant=readingOrder.filter(text=>/Check this integration|Checking the integration|One check has finished|I found another result|Verified integration/.test(text))
        assert.deepEqual(relevant,["Check this integration","Checking the integration.","One check has finished.","I found another result.","Verified integration."])
        assert.equal(await chat.getByText("Run the fixture checks",{exact:true}).isVisible(),false)
        await chat.locator(".pa-trace").first().locator("summary").first().click()
        await chat.getByText("Run the fixture checks",{exact:true}).waitFor()
        assert.equal(await chat.getByText("Private scratch note",{exact:true}).isVisible(),false)
        await chat.getByText(zh?"思考過程":"Thinking",{exact:true}).click()
        await chat.getByText("Private scratch note",{exact:true}).waitFor()
        assert.equal(await update.getByText("Earlier checkpoint.").count(),0)
        await chat.getByRole("button",{name:zh?"較早的訊息":"Earlier messages",exact:true}).click()
        await update.getByText("Earlier checkpoint.",{exact:true}).waitFor()
        await page.evaluate(() => {
          window.botsFixture.a.workTurn("running")
          window.botsFixture.a.inlineProgress("live","Checking what changed…","2026-11-12T06:22:00Z")
        })
        await update.getByText("Checking what changed…",{exact:true}).waitFor()
        await page.evaluate(() => {
          window.botsFixture.a.inlineProgress("live"," confirmed.","2026-11-12T06:22:01Z")
        })
        await update.getByText("Checking what changed… confirmed.",{exact:true}).waitFor()
        assert.equal(await update.count(),4)
        console.log(`PASS ${engine} ${language}: progress stays in reading order, streams inline and does not duplicate replies`)
        await context.close()
      }
    } finally { await browser.close() }
  }
} finally {
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
}
