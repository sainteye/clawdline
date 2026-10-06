// Render the frozen numeric catalog phrases with actual catalog.ts code in
// headless Chrome at 390px and desktop widths. The fixture uses localhost.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const shots = process.env.CLAWDLINE_SHOTS || ""
const tags = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]
const keys = ["skillFoldersSkipped", "unfinishedWork", "squadSkills", "squadGroups", "squadRoles", "squadItems", "squadSkillSources", "recentInterventions", "prunedInterventions", "pendingInterventions", "localFiles", "unsyncedProjects", "exportRounds"]
const machineKeys = ["legacy.webFailNotFound", "legacy.webShowOnMac", "settings.webCloudPairOrderScan"]

function fixture(): string {
  return `<!doctype html><html lang="en"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{font:16px system-ui;margin:0;padding:12px}p{max-width:100%;overflow-wrap:anywhere}</style><body><script src="/catalog.js"></script><script>
  (async () => {
    const errors = []
    const tags = ${JSON.stringify(tags)}
    const keys = ${JSON.stringify(keys)}
    const machineKeys = ${JSON.stringify(machineKeys)}
    for (const tag of tags) {
      const catalog = await (await fetch('/catalogs/' + tag + '.json')).json()
      if (Catalog.activateCatalog(catalog, tag) !== tag) errors.push(tag + ': fallback')
      for (const key of keys) for (const count of [0, 1, 123456]) {
        const values = [count, count + 1, 'A very long source label from a machine / with a path']
        const paragraph = document.createElement('p')
        paragraph.lang = tag
        paragraph.textContent = Catalog.catalogFormat('count', key, values)
        paragraph.dataset.key = key
        paragraph.dataset.count = count
        document.body.append(paragraph)
        if (!paragraph.textContent.includes(String(count)) || /\\{arg[0-9]+\\}/.test(paragraph.textContent)) errors.push(tag + ':' + key + ':' + count)
        if (key === 'squadSkillSources' && !paragraph.textContent.includes(values[2])) errors.push(tag + ': source changed')
        if (key === 'unfinishedWork' && paragraph.textContent.indexOf(String(count)) > paragraph.textContent.indexOf(String(count + 1))) errors.push(tag + ': count order')
      }
      for (const key of machineKeys) {
        const paragraph = document.createElement('p')
        paragraph.lang = tag
        paragraph.textContent = Catalog.catalogWord(...key.split('.'))
        paragraph.dataset.key = key
        document.body.append(paragraph)
        if (/\\b[M]acs?\\b/u.test(paragraph.textContent) || (tag === 'ko' && /\uB9E5/u.test(paragraph.textContent))) errors.push(tag + ':' + key + ': hardware-only term')
        if (!paragraph.textContent.trim()) errors.push(tag + ':' + key + ': blank')
      }
    }
    document.body.dataset.errors = errors.join('|')
    document.body.dataset.ready = 'true'
  })().catch(error => { document.body.dataset.error = String(error); document.body.dataset.ready = 'true' })
  </script></body></html>`
}

class Browser {
  private seq = 0
  private pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void }>()
  private ws: WebSocket
  constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (event) => {
      const message = JSON.parse(String(event.data))
      if (message.id === undefined) return
      const wait = this.pending.get(message.id)
      this.pending.delete(message.id)
      if (message.error) wait?.reject(new Error(message.error.message))
      else wait?.resolve(message.result)
    })
  }
  send(method: string, params: object = {}, sessionId?: string): Promise<any> {
    const id = ++this.seq
    this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(method + " timed out")) }, 15000)
      this.pending.set(id, {
        resolve: (value) => { clearTimeout(timer); resolve(value) },
        reject: (error) => { clearTimeout(timer); reject(error) },
      })
    })
  }
  close(): void { this.ws.close() }
}

async function browserEndpoint(process: ChildProcess): Promise<string> {
  return new Promise((resolve, reject) => {
    let said = ""
    const timer = setTimeout(() => reject(new Error("Chrome did not start: " + said.slice(0, 500))), 20000)
    process.stderr?.on("data", (chunk) => {
      said += String(chunk)
      const found = /DevTools listening on (ws:\/\/\S+)/u.exec(said)
      if (found) { clearTimeout(timer); resolve(found[1]) }
    })
    process.once("exit", (code, signal) => { clearTimeout(timer); reject(new Error(`Chrome exited ${code}/${signal}: ${said.slice(0, 500)}`)) })
  })
}

async function browserConnect(endpoint: string): Promise<Browser> {
  const ws = new WebSocket(endpoint)
  await new Promise<void>((resolve, reject) => {
    ws.addEventListener("open", () => resolve(), { once: true })
    ws.addEventListener("error", () => reject(new Error("Chrome DevTools socket failed")), { once: true })
  })
  return new Browser(ws)
}

test("nine catalogs render numeric sentences at phone and desktop widths", async () => {
  assert.ok(existsSync(chrome), "Chrome is unavailable")
  const compiled = await build({ entryPoints: [join(root, "src/catalog.ts")], bundle: true, platform: "browser", format: "iife", globalName: "Catalog", write: false })
  const script = compiled.outputFiles[0].text
  const server = createServer((req, res) => {
    const path = new URL(req.url || "/", "http://fixture").pathname
    if (path === "/") { res.writeHead(200, { "content-type": "text/html" }); res.end(fixture()); return }
    if (path === "/catalog.js") { res.writeHead(200, { "content-type": "text/javascript" }); res.end(script); return }
    if (/^\/catalogs\/[A-Za-z-]+\.json$/u.test(path)) {
      res.writeHead(200, { "content-type": "application/json" }); res.end(readFileSync(join(root, "public", path))); return
    }
    res.writeHead(404); res.end("missing")
  })
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve))
  const address = server.address()
  const origin = `http://127.0.0.1:${typeof address === "object" && address ? address.port : 0}`
  const profile = mkdtempSync(join(tmpdir(), "clawdline-catalog-render-"))
  const process = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", "--no-first-run", "--user-data-dir=" + profile, "about:blank"])
  let browser: Browser | undefined
  try {
    browser = await browserConnect(await browserEndpoint(process))
    const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await browser.send("Target.attachToTarget", { targetId, flatten: true })
    await browser.send("Page.enable", {}, sessionId)
    await browser.send("Runtime.enable", {}, sessionId)
    for (const width of [390, 1280]) {
      await browser.send("Emulation.setDeviceMetricsOverride", { width, height: 844, mobile: width === 390, deviceScaleFactor: 1 }, sessionId)
      await browser.send("Page.navigate", { url: origin }, sessionId)
      const deadline = Date.now() + 15000
      let state: any = null
      for (;;) {
        try {
          const answer = await browser.send("Runtime.evaluate", {
            expression: `({ready:document.body?.dataset.ready,errors:document.body?.dataset.errors,error:document.body?.dataset.error,scrollWidth:document.documentElement.scrollWidth,innerWidth:innerWidth,paragraphs:document.querySelectorAll('p[data-key]').length})`,
            returnByValue: true,
          }, sessionId)
          state = answer.result.value
        } catch { /* navigation can replace the execution context */ }
        if (state?.ready === "true") break
        if (Date.now() > deadline) assert.fail(`catalog fixture did not render at ${width}px: ${JSON.stringify(state)}`)
        await new Promise((resolve) => setTimeout(resolve, 50))
      }
      console.log(JSON.stringify({ origin, width, fixture: "catalog.ts + public/catalogs/*.json", ...state }))
      assert.equal(state.error, undefined)
      assert.equal(state.errors, "")
      assert.equal(state.paragraphs, tags.length * (keys.length * 3 + machineKeys.length))
      assert.equal(state.innerWidth, width)
      assert.ok(state.scrollWidth <= width, `horizontal overflow at ${width}px: ${state.scrollWidth}px`)
      if (shots) {
        const image = await browser.send("Page.captureScreenshot", { format: "png", fromSurface: true }, sessionId)
        writeFileSync(join(shots, `catalog-count-${width}.png`), Buffer.from(image.data, "base64"))
      }
    }
    await browser.send("Target.closeTarget", { targetId })
  } finally {
    browser?.close()
    if (process.exitCode === null) {
      const gone = new Promise((resolve) => process.once("exit", resolve))
      process.kill()
      await gone
    }
    server.closeAllConnections()
    await new Promise<void>((resolve) => server.close(() => resolve()))
    rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
  }
})
