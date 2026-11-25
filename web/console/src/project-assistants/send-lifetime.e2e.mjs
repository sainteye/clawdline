/** Real Bots UI, both source transports, and the owned HTTP/SQLite source with a scripted Bot. */
import assert from "node:assert/strict"
import { createServer } from "node:http"
import { mkdir, readFile, writeFile } from "node:fs/promises"
import { fileURLToPath, pathToFileURL } from "node:url"
import path from "node:path"
import { build } from "esbuild"

const { BOTS_ORIGIN: upstream, BOTS_ARTIFACTS: artifacts, BOTS_MACHINE: machine, BOTS_TOKEN: token, PLAYWRIGHT_MODULE } = process.env
assert.ok(upstream && artifacts && machine && token && PLAYWRIGHT_MODULE, "Use the owned Go fixture and installed Playwright")
await mkdir(artifacts, { recursive: true })
const { chromium } = await import(pathToFileURL(PLAYWRIGHT_MODULE).href)
const built = await build({
  entryPoints: [fileURLToPath(new URL("./send-lifetime.fixture.tsx", import.meta.url))],
  bundle: true, write: false, outfile: "lifetime.js", format: "esm", platform: "browser", target: "es2022", jsx: "automatic",
  loader: { ".webp": "dataurl", ".png": "dataurl", ".svg": "dataurl", ".mp3": "dataurl", ".wav": "dataurl" },
  define: { "process.env.NODE_ENV": '"development"', "import.meta.env": "{}" },
})
const files = new Map(built.outputFiles.map(file => ["/" + path.basename(file.path), file.contents]))
const requests = [], submissions = [], results = [], faults = new Map(), missingReceipts = new Set()
const host = "/v1/project-assistants"
const readRoutes = { view: "/view", get: "", "handoff-card": "/handoff-card", "send-receipt": "/send-receipt", attachment: "/attachments/" }
const writeRoutes = { send: "/messages", "send-pictures": "/messages", read: "/read", observed: "/observed" }

// The Cloud client runs its real carry encoding. This controlled connection
// translates the carried words to the owned source's HTTP entry points.
function relayRequest(word, fields) {
  if (word === "status") return { url: host + "/status", method: "GET" }
  if (word === "list") return { url: host + "?" + new URLSearchParams(fields), method: "GET" }
  if (Object.hasOwn(writeRoutes, word)) {
    return { url: host + "/" + fields.command.key.bot_id + writeRoutes[word], method: "POST", body: fields, key: fields.command.idempotency_key }
  }
  assert.ok(Object.hasOwn(readRoutes, word), word)
  const { key, bot_id, id, ...params } = fields
  if (key) params.machine_id = key.machine_id
  return { url: host + "/" + (key?.bot_id ?? bot_id) + readRoutes[word] + (id ?? "") + "?" + new URLSearchParams(params), method: "GET" }
}

async function proxy(req, res, url) {
  const control = new AbortController()
  res.on("close", () => { if (!res.writableEnded) control.abort() })
  const chunks = []
  for await (const chunk of req) chunks.push(chunk)
  const raw = Buffer.concat(chunks)
  const relay = url.pathname.startsWith("/relay/"), word = relay ? url.pathname.slice(7) : ""
  const fields = raw.length ? JSON.parse(raw) : undefined
  const request = relay ? relayRequest(word, fields) : { url: url.pathname + url.search, method: req.method, body: fields, key: req.headers["idempotency-key"] }
  const record = { word, method: request.method, path: request.url }
  requests.push(record)
  const send = request.method === "POST" && request.url.endsWith("/messages")
  const fault = send ? faults.get(request.body.text)?.shift() : undefined
  if (send) {
    assert.equal(request.key, request.body.command.idempotency_key)
    if (relay) assert.equal(req.headers["x-fixture-correlation"], request.body.command.client_attempt_id ?? request.key)
    submissions.push({ ...record, payload: request.body, key: request.key, fault })
  }
  if (missingReceipts.delete(new URL(request.url, "http://fixture").searchParams.get("idempotency_key"))) {
    res.writeHead(404, { "Content-Type": "application/json" })
    return res.end(JSON.stringify({ contract_version: "project-assistants.v1", error: { code: "pa_not_found", field: "send_receipt", retry_after_seconds: 0 } }))
  }
  if (fault === "refuse") {
    res.writeHead(409, { "Content-Type": "application/json" })
    return res.end(JSON.stringify({ contract_version: "project-assistants.v1", error: { code: "pa_capacity_full", field: "queue", retry_after_seconds: 1 } }))
  }
  if (fault === "lost-before-admission") return res.writeHead(502).end("send answer unavailable")
  try {
    const answer = await fetch(upstream + request.url, {
      method: request.method, signal: control.signal,
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", ...(request.key ? { "Idempotency-Key": request.key } : {}) },
      ...(request.body ? { body: JSON.stringify(request.body) } : {}),
    })
    record.status = answer.status
    const media = answer.headers.get("Content-Type")?.split(";")[0]
    if (answer.ok && media?.startsWith("image/")) {
      const bytes = Buffer.from(await answer.arrayBuffer())
      if (!relay) return res.writeHead(answer.status, { "Content-Type": media }).end(bytes)
      res.writeHead(answer.status, { "Content-Type": "application/json" })
      return res.end(JSON.stringify({ contract_version: "project-assistants.v1", id: fields.id, media_type: media, byte_count: bytes.length, data: bytes.toString("base64") }))
    }
    const body = await answer.text()
    if (fault === "lost-after-admission") {
      assert.equal(answer.status, 200)
      record.answerLost = true
      return res.writeHead(502).end("send answer unavailable")
    }
    res.writeHead(answer.status, { "Content-Type": "application/json" })
    res.end(body)
  } catch (error) {
    record.error = error.name
    if (!res.destroyed) throw error
  }
}

const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, "http://fixture")
    if (url.pathname.startsWith("/v1/") || url.pathname.startsWith("/relay/")) return await proxy(req, res, url)
    if (url.pathname === "/fixture.json") return res.end(JSON.stringify({ machine }))
    if (url.pathname.startsWith("/catalog/")) {
      res.setHeader("Content-Type", "application/json")
      return res.end(await readFile(new URL("../../public/catalogs/" + path.basename(url.pathname), import.meta.url)))
    }
    if (files.has(url.pathname)) {
      res.setHeader("Content-Type", url.pathname.endsWith(".css") ? "text/css" : "text/javascript")
      return res.end(files.get(url.pathname))
    }
    res.setHeader("Content-Type", "text/html")
    res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/lifetime.css"><div id="app"></div><script type="module" src="/lifetime.js"></script>')
  } catch (error) {
    res.writeHead(500).end(String(error))
  }
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const origin = `http://127.0.0.1:${server.address().port}`
const state = () => fetch(upstream + "/fixture/state").then(response => response.json())
async function waitFor(name, read, matches) {
  let value
  for (let n = 0; n < 100; n++) {
    value = await read()
    if (matches(value)) return value
    await new Promise(resolve => setTimeout(resolve, 50))
  }
  throw Error(`${name}: ${JSON.stringify(value)}`)
}
const gate = async (action, what = "") => {
  const response = await fetch(upstream + `/fixture/${action}?what=${what}`, { method: "POST" })
  assert.equal(response.status, 204)
}
const attempts = text => submissions.filter(request => request.payload.text === text)
const retryName = /^(Retry sending|重試送出)$/
const picture = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAAAFklEQVR4nGMwaEkhCTGMahjVMHw1AACoJBgQ9FGyagAAAABJRU5ErkJggg==", "base64")
const browser = await chromium.launch({ headless: true, args: ["--disable-features=OmniboxEverywhere"], ...(process.env.CHROME ? { executablePath: process.env.CHROME } : {}) })

function chat(page, mode, name) {
  const field = page.locator(".pa-composer textarea"), send = page.locator(".pa-composer button.pa-send")
  const open = async () => {
    await page.locator(".pa-bot-row").filter({ hasText: "Synthetic Bot" }).click()
    await field.waitFor()
  }
  const leave = async () => {
    if (mode === "cloud") await page.locator(".pa-chat-back").click()
    else await page.goBack()
    await field.waitFor({ state: "detached" })
  }
  const submit = async (text, attach = false) => {
    await field.fill(text)
    if (attach) {
      await page.locator(".pa-composer input[type=file]").setInputFiles({ name: "pixel.png", mimeType: "image/png", buffer: picture })
      await page.locator(".pa-picture-preview").waitFor()
    }
    await send.focus()
    assert.equal(await send.evaluate(button => button === document.activeElement), true)
    await page.keyboard.press("Enter")
  }
  const bubble = (selector, text) => page.locator(selector).filter({ has: page.getByText(text, { exact: true }) })
  const delivered = async text => {
    await bubble(".pa-message.pa-mine[data-message-id]", text).waitFor()
    const reply = bubble(".pa-message[data-message-id]", "Scripted reply to " + text)
    await reply.waitFor({ timeout: 12_000 })
    await reply.scrollIntoViewIfNeeded()
    assert.equal(await bubble(".pa-message.pa-mine[data-message-id]", text).count(), 1)
  }
  return { open, leave, submit, bubble, delivered, reload: () => page.reload(), sending: page.locator(".pa-sending"),
    renewGrant: () => page.evaluate(() => window.dispatchEvent(new Event("fixture-renew-grant"))),
    shot: suffix => page.screenshot({ path: path.join(artifacts, `${mode}-${name}-${suffix}.png`) }) }
}

async function scenarios(mode, run) {
  await run("admitted", async (ui, text, before) => {
    await gate("arm", "send")
    await ui.submit(text)
    await waitFor("source admitted; answer held", state, value => value.held === "send" && value.posts === before.posts + 1)
    await ui.leave()
    await ui.open()
    await ui.sending.waitFor()
    await gate("release")
    await ui.delivered(text)
    await ui.shot("sent")
    assert.equal((await state()).client_gone, before.client_gone)
    assert.equal(attempts(text).length, 1)
  })
  await run("preflight", async (ui, text, before) => {
    await gate("arm", "snapshot")
    await ui.submit(text, true)
    await waitFor("attachment preflight held", state, value => value.held === "snapshot")
    await ui.leave()
    await ui.open()
    await ui.bubble(".pa-sending", text).waitFor()
    await gate("release")
    await ui.delivered(text)
    const image = ui.bubble(".pa-message.pa-mine[data-message-id]", text).locator("img")
    await image.scrollIntoViewIfNeeded()
    await waitFor("sent attachment decoded", () => image.evaluate(element => element.naturalWidth), width => width > 0)
    await ui.shot("sent")
    assert.equal((await state()).client_gone, before.client_gone)
    assert.equal(attempts(text).length, 1)
    assert.equal(attempts(text)[0].payload.images.length, 1)
  })
  await run("refused", async (ui, text) => {
    faults.set(text, ["refuse"])
    await ui.submit(text)
    await waitFor("first request refused", () => attempts(text), value => value.length === 1)
    await ui.leave()
    await ui.open()
    const failed = ui.bubble(".pa-unsent", text), retry = failed.getByRole("button", { name: retryName })
    await failed.getByRole("alert").filter({ hasText: "pa_capacity_full" }).waitFor()
    await retry.waitFor()
    assert.equal(attempts(text).length, 1)
    await failed.scrollIntoViewIfNeeded()
    await ui.shot("failed")
    await retry.focus()
    await retry.press("Enter")
    await ui.delivered(text)
    assert.equal(attempts(text).length, 2)
  })
  await run("unknown", async (ui, text, before) => {
    faults.set(text, ["lost-before-admission", "lost-after-admission"])
    await ui.submit(text)
    await waitFor("first request lost", () => attempts(text), value => value.length === 1)
    await ui.leave()
    await ui.open()
    await ui.delivered(text)
    await ui.leave()
    await ui.open()
    await ui.delivered(text)
    const [first, retry] = attempts(text)
    assert.equal(attempts(text).length, 2)
    assert.deepEqual(retry.payload, first.payload, "automatic replay must preserve the entire send, including its key")
    assert.equal((await state()).posts, before.posts + 1)
  })
  await run("duplicate", async (ui, text, before) => {
    faults.set(text, ["lost-after-admission"])
    await gate("arm", "send")
    await ui.submit(text)
    await waitFor("source admitted; answer held", state, value => value.held === "send" && value.posts === before.posts + 1)
    missingReceipts.add(attempts(text)[0].key)
    await ui.leave()
    await gate("release")
    await ui.open()
    await ui.delivered(text)
    await waitFor("same-key replay reached the source", state, value => value.posts === before.posts + 2)
    assert.equal(attempts(text).length, 2)
    assert.deepEqual(attempts(text)[1].payload, attempts(text)[0].payload)
  })
  await run("unknown-twice", async (ui, text, before) => {
    faults.set(text, ["lost-before-admission", "lost-before-admission"])
    await ui.submit(text)
    await waitFor("first request lost", () => attempts(text), value => value.length === 1)
    await ui.leave()
    await ui.open()
    const pending = ui.bubble(".pa-unsent", text), retry = pending.getByRole("button", { name: retryName })
    await waitFor("one automatic replay", () => attempts(text), value => value.length === 2)
    await retry.waitFor()
    await ui.leave()
    await ui.open()
    await retry.waitFor()
    assert.equal(attempts(text).length, 2)
    assert.deepEqual(attempts(text)[1].payload, attempts(text)[0].payload)
    assert.equal((await state()).posts, before.posts)
    await retry.click()
    await ui.delivered(text)
    assert.equal(attempts(text).length, 3)
  })
  await run("reload", async (ui, text) => {
    faults.set(text, ["lost-before-admission"])
    await ui.submit(text)
    const retry = ui.bubble(".pa-unsent", text).getByRole("button", { name: retryName })
    await retry.waitFor()
    await ui.reload()
    await ui.open()
    await retry.click()
    await ui.delivered(text)
    assert.equal(attempts(text).length, 2)
    assert.deepEqual(attempts(text)[1].payload, attempts(text)[0].payload)
  })
  await run("grant-changed", async (ui, text) => {
    faults.set(text, ["lost-before-admission"])
    await ui.submit(text)
    const retry = ui.bubble(".pa-unsent", text).getByRole("button", { name: retryName })
    await retry.waitFor()
    await ui.leave()
    await ui.renewGrant()
    await ui.open()
    await retry.click()
    await ui.delivered(text)
    assert.equal(attempts(text).length, 2)
    assert.deepEqual(attempts(text)[1].payload, attempts(text)[0].payload)
  })
}

try {
  for (const mode of ["local", "cloud"]) {
    const width = mode === "cloud" ? 390 : 1280, language = mode === "cloud" ? "zh-Hant" : "en"
    await scenarios(mode, async (name, test) => {
      const context = await browser.newContext({ viewport: { width, height: 844 }, colorScheme: mode === "cloud" ? "dark" : "light",
        isMobile: mode === "cloud", hasTouch: mode === "cloud" })
      const page = await context.newPage(), errors = []
      page.setDefaultTimeout(3500)
      page.on("pageerror", error => errors.push(error.message))
      const before = await state(), text = `Fixture ${mode} ${name}`
      const ui = chat(page, mode, name)
      try {
        await page.goto(origin + "/?" + new URLSearchParams({ mode, lang: language }))
        await ui.open()
        await page.locator(".pa-chat-skeleton").waitFor({ state: "detached" })
        await test(ui, text, before)
        const after = await state()
        for (const field of ["provider_sends", "user", "assistant"]) assert.equal(after[field], before[field] + 1, field)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
        assert.deepEqual(errors, [])
        results.push({ mode, name, width, language, before, after })
      } catch (error) {
        results.push({ mode, name, width, language, error: error.message, errors, before, after: await state() })
        await ui.shot("failure").catch(() => {})
      } finally {
        await gate("release")
        await context.close()
      }
    })
  }
  console.log(JSON.stringify(results, null, 2))
  assert.ok(results.every(result => !result.error), "one or more browser scenarios failed")
} finally {
  await browser.close()
  await writeFile(path.join(artifacts, "summary.json"), JSON.stringify({ results, submissions, requests }, null, 2))
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
}
