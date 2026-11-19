/** Production Bot chat and source client, authenticated HTTP, real SQLite, scripted Provider. */
import assert from "node:assert/strict"
import {createServer} from "node:http"
import {mkdir, readFile, writeFile} from "node:fs/promises"
import {fileURLToPath, pathToFileURL} from "node:url"
import path from "node:path"
import {build} from "esbuild"

const {BOTS_ORIGIN: upstream, BOTS_TOKEN: token, BOTS_MACHINE: machine, BOTS_BOT: bot, BOTS_ARTIFACTS: artifacts, PLAYWRIGHT_MODULE: module} = process.env
assert.ok(upstream && token && machine && bot && artifacts && module)
await mkdir(artifacts, {recursive: true})
const root = fileURLToPath(new URL("..", import.meta.url))
const bundle = await build({entryPoints: [path.join(root, "tools/fixtures/project-assistants-handoff-card.tsx")],
  bundle: true, write: false, outfile: "handoff.js", format: "esm", platform: "browser", target: "es2022", jsx: "automatic",
  define: {"process.env.NODE_ENV": '"development"', FIXTURE_MACHINE: JSON.stringify(machine), FIXTURE_BOT: JSON.stringify(bot)}})
const files = new Map(bundle.outputFiles.map(f => ["/" + path.basename(f.path), f.contents]))
const botPath = "/v1/project-assistants/" + bot, cardPath = botPath + "/handoff-card", messagesPath = botPath + "/messages"
const traffic = [], evidence = []
let loseNext = false
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, "http://fixture")
    if (url.pathname.startsWith("/v1/")) {
      const chunks = []
      for await (const chunk of req) chunks.push(chunk)
      const body = Buffer.concat(chunks)
      const headers = {Authorization: "Bearer " + token, "Content-Type": "application/json"}
      if (req.headers["idempotency-key"]) headers["Idempotency-Key"] = req.headers["idempotency-key"]
      const response = await fetch(upstream + url.pathname + url.search, {method: req.method, headers, ...(body.length && {body})})
      const bytes = Buffer.from(await response.arrayBuffer())
      const lost = loseNext && req.method === "POST" && url.pathname === messagesPath && response.ok
      traffic.push({method: req.method, path: url.pathname, status: response.status, ...(body.length && {body: JSON.parse(body)}), ...(lost && {lost: true})})
      res.setHeader("Content-Type", "application/json")
      if (lost) {
        loseNext = false
        res.writeHead(503)
        res.end(JSON.stringify({contract_version: "project-assistants.v1", error: {code: "pa_outcome_unknown", field: "", retry_after_seconds: 0}}))
      } else {
        res.writeHead(response.status)
        res.end(bytes)
      }
      return
    }
    if (url.pathname.startsWith("/catalog/")) {
      res.setHeader("Content-Type", "application/json")
      res.end(await readFile(path.join(root, "public/catalogs", path.basename(url.pathname))))
      return
    }
    if (files.has(url.pathname)) {
      res.setHeader("Content-Type", url.pathname.endsWith(".css") ? "text/css" : "text/javascript")
      res.end(files.get(url.pathname))
      return
    }
    res.setHeader("Content-Type", "text/html")
    res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/handoff.css"><div id="app"></div><script type="module" src="/handoff.js"></script>')
  } catch (error) {
    res.writeHead(500)
    res.end(JSON.stringify({error: String(error)}))
  }
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const origin = "http://127.0.0.1:" + server.address().port
const {chromium} = await import(pathToFileURL(module).href)
try {
  const browser = await chromium.launch({headless: true, args: ["--disable-features=OmniboxEverywhere"]})
  try {
    for (const [language, width, height] of [["en", 1440, 900], ["zh-Hant", 390, 780]]) {
      const zh = language !== "en", context = await browser.newContext({viewport: {width, height}, hasTouch: zh})
      const page = await context.newPage()
      page.setDefaultTimeout(12000)
      const errors = [], start = traffic.length
      page.on("pageerror", error => errors.push(String(error)))
      const click = target => zh ? target.tap() : target.click()
      const editor = page.locator(".pa-composer textarea"), send = page.locator(".pa-composer .pa-send")
      const card = page.locator(".pa-handoff-card")
      const button = (en, tw) => card.getByRole("button", {name: zh ? tw : en, exact: true})
      const state = (en, tw) => card.getByText(zh ? tw : en, {exact: true})
      const writeDraft = "Composer draft " + language
      const saved = "Browser edited summary " + language
      const exact = "Concurrent source edit " + language
      const sends = () => traffic.slice(start).filter(row => row.method === "POST" && row.path === messagesPath)
      let conversation
      const prepare = async () => {
        const identity = page.locator(".pa-chat-identity")
        await click(identity)
        await click(page.getByRole("button", {name: zh ? "準備新對話" : "Prepare a fresh conversation", exact: true}))
        await state("Your next message starts a fresh conversation with this summary.", "下一則訊息會帶著這份摘要開啟新對話。").waitFor()
        await page.waitForFunction(() => {
          const panel = document.querySelector(".pa-side-inspector")
          return panel?.hasAttribute("hidden") && (panel.getClientRects().length === 0 || panel.getBoundingClientRect().left >= innerWidth - 1)
        })
      }
      const sendText = text => page.locator(".pa-message.pa-mine:not(.pa-sending)").getByText(text, {exact: true})
      try {
        await page.goto(origin + "/?lang=" + language)
        await editor.waitFor()
        conversation = new URL(page.url()).hash && new URLSearchParams(new URL(page.url()).hash.slice(1)).get("conversation")
        assert.ok(conversation)
        await editor.fill(writeDraft)
        await prepare()
        await click(button("View summary", "查看摘要"))
        const preview = await card.locator(".pa-handoff-text").textContent()
        assert.match(preview, /Synthetic first input/)
        await click(button("Edit summary", "編輯摘要"))
        await card.locator("textarea").fill("Cancelled summary " + language)
        assert.equal(await send.isDisabled(), true)
        await click(button("Cancel", "取消"))
        assert.equal(await card.locator(".pa-handoff-text").textContent(), preview)
        assert.equal(await editor.inputValue(), writeDraft)
        assert.equal(sends().length, 0)
        await page.screenshot({path: path.join(artifacts, language + "-preview.png")})
        await click(button("Keep current conversation", "保留目前對話"))
        await state("Kept the current conversation.", "已保留目前的對話。").waitFor()
        assert.equal(sends().length, 0)
        await click(send)
        await sendText(writeDraft).waitFor()
        await page.locator(".pa-message:not(.pa-mine)").getByText("Scripted final answer", {exact: true}).nth(zh ? 3 : 1).waitFor()
        assert.equal(sends()[0].body.handoff, undefined)
        await prepare()
        await click(button("Edit summary", "編輯摘要"))
        await card.locator("textarea").fill(saved)
        await page.screenshot({path: path.join(artifacts, language + "-edit.png")})
        await click(button("Save summary", "儲存摘要"))
        await card.getByText(saved, {exact: true}).waitFor()
        const intended = "New input with handoff " + language
        await editor.fill(intended)
        await page.reload()
        await state("Your next message starts a fresh conversation with this summary.", "下一則訊息會帶著這份摘要開啟新對話。").waitFor()
        await click(button("View summary", "查看摘要"))
        await card.getByText(saved, {exact: true}).waitFor()
        assert.equal(await editor.inputValue(), intended)
        await click(button("View original history", "查看原對話紀錄"))
        assert.equal(await page.evaluate(() => document.activeElement?.matches("[data-message-id]")), true)
        const cardURL = cardPath + "?" + new URLSearchParams({machine_id: machine, conversation_id: conversation})
        const snapshot = await (await fetch(origin + cardURL)).json()
        const id = crypto.randomUUID()
        const changed = await fetch(origin + cardPath, {method: "POST", headers: {"Content-Type": "application/json", "Idempotency-Key": id},
          body: JSON.stringify({command: {key: snapshot.fence.key, operation: "edit", idempotency_key: id, content_hash: ""},
            fence: snapshot.fence, action: "edit", ref: snapshot.card.ref, text: exact})})
        assert.equal(changed.status, 200)
        const newer = (await changed.json()).card.ref
        await click(send)
        await state("This handoff changed. Check the current summary before sending.", "交接稿已變更，請先查看目前的摘要再送出。").waitFor()
        assert.equal(await editor.inputValue(), intended)
        assert.equal(sends().at(-1).status, 409)
        assert.deepEqual(sends().at(-1).body.handoff, snapshot.card.ref)
        await click(button("Check handoff", "查詢交接狀態"))
        await card.getByText(exact, {exact: true}).waitFor()
        loseNext = true
        await click(send)
        const check = page.getByRole("button", {name: zh ? "向機器核對" : "Check with the machine", exact: true})
        await check.waitFor()
        await page.reload()
        await check.waitFor()
        await click(check)
        await page.getByText(zh ? "機器已收到這則訊息，沒有重送。" : "The machine has this message. It was not sent again.", {exact: true}).waitFor()
        await sendText(intended).waitFor()
        await state("The source accepted this handoff with your message.", "來源已將交接稿與你的訊息一起受理。").waitFor()
        assert.equal(sends().length, 3)
        assert.deepEqual(sends()[2].body.handoff, newer)
        assert.equal(sends()[2].lost, true)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
        assert.deepEqual(errors, [])
        await page.screenshot({path: path.join(artifacts, language + "-receipt.png")})
        evidence.push({language, width, height, keptOld: true, cancelledEdit: true, reload: true, originalHistory: true, staleSend: 409, lostAcknowledgement: true, messagePosts: 3, acceptedInputs: 2})
      } catch (error) {
        await page.screenshot({path: path.join(artifacts, language + "-failure.png")})
        await writeFile(path.join(artifacts, language + "-failure.txt"), [String(error), ...errors, await page.locator("body").innerText()].join("\n"))
        throw error
      } finally {
        await context.close()
      }
    }
    const start = traffic.length
    const old = await browser.newPage({viewport: {width: 1100, height: 800}})
    try {
      await old.goto(origin + "/?old=1")
      await old.locator(".pa-composer textarea").waitFor()
      await old.locator(".pa-chat-identity").click()
      assert.equal(await old.getByRole("button", {name: "Prepare a fresh conversation", exact: true}).count(), 0)
      assert.equal(traffic.slice(start).filter(row => row.path === cardPath).length, 0)
      evidence.push({apiLevel: 90, nativeHistoryVisible: true, handoffRequests: 0})
    } finally {
      await old.close()
    }
  } finally {
    await browser.close()
  }
} finally {
  await Promise.all([writeFile(path.join(artifacts, "browser-evidence.json"), JSON.stringify(evidence, null, 2) + "\n"),
    writeFile(path.join(artifacts, "synthetic-traffic.json"), JSON.stringify(traffic, null, 2) + "\n")])
  await new Promise(resolve => server.close(resolve))
}
console.log(JSON.stringify(evidence))
