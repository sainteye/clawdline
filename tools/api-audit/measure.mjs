// Headless Chrome against a running console: every /v1/ request each scenario
// makes, written one JSON file per scenario. See README.md.
//
// usage: node tools/api-audit/measure.mjs <outdir> [<link-file> | -]
//
// The device link opens the console signed in, so it carries a key: it is read
// from a file or from stdin, never from argv, which `ps` shows to anybody on
// the machine. It is never printed.
//
// DUR (ms, default 180000) is how long each reading scenario watches;
// SENDDUR (ms, default 90000) the send scenario. CHROME names the browser
// binary when it is not at the macOS default.
import { spawn } from "node:child_process"
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

const [outdir, linkFrom = "-"] = process.argv.slice(2)
if (!outdir) {
  console.error("usage: node measure.mjs <outdir> [<link-file> | -]   (the link is read from stdin by default)")
  process.exit(2)
}
const openURL = (linkFrom === "-" ? readFileSync(0, "utf8") : readFileSync(linkFrom, "utf8")).trim()
if (!/^https?:\/\//.test(openURL)) {
  console.error("measure: the link file holds no http(s) URL")
  process.exit(2)
}
mkdirSync(outdir, { recursive: true })
const origin = new URL(openURL).origin
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const profile = mkdtempSync(join(tmpdir(), "cl-audit-"))
const proc = spawn(chrome, ["--headless=new", "--remote-debugging-port=0", `--user-data-dir=${profile}`,
  "--no-first-run", "--window-size=1440,900", "about:blank"], { stdio: "ignore" })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
let port
for (let i = 0; i < 100 && !port; i++) {
  await sleep(100)
  const f = join(profile, "DevToolsActivePort")
  if (existsSync(f)) port = readFileSync(f, "utf8").split("\n")[0]
}
if (!port) {
  console.error("measure: Chrome did not open a debugging port")
  proc.kill()
  process.exit(1)
}
const ver = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json()
const ws = new WebSocket(ver.webSocketDebuggerUrl)
await new Promise((r) => ws.addEventListener("open", r))
let id = 0
const waiting = new Map()
const listeners = []
ws.addEventListener("message", (m) => {
  const d = JSON.parse(m.data)
  if (d.id && waiting.has(d.id)) {
    waiting.get(d.id)(d)
    waiting.delete(d.id)
  } else for (const l of listeners) l(d)
})
const send = (method, params = {}, sessionId) => new Promise((r) => {
  const i = ++id
  waiting.set(i, r)
  ws.send(JSON.stringify({ id: i, method, params, sessionId }))
})
const { result: { targetId } } = await send("Target.createTarget", { url: "about:blank" })
const { result: { sessionId: S } } = await send("Target.attachToTarget", { targetId, flatten: true })
await send("Network.enable", {}, S)
await send("Page.enable", {}, S)
await send("Runtime.enable", {}, S)

// shape is a request's route with every id taken out, and the names (never the
// values) of its query.
const shape = (u) => {
  const url = new URL(u)
  const p = url.pathname
    .replace(/\/sessions\/[^/]+/, "/sessions/:id")
    .replace(/\/(session-todos|human-interventions)\/.+$/, "/$1/:id")
    .replace(/\/images\/[^/]+/, "/images/:img")
    .replace(/\/agents\/[^/]+/, "/agents/:a")
    .replace(/\/tasks\/[^/]+/, "/tasks/:id")
    .replace(/\/schedules\/[^/]+/, "/schedules/:id")
  const keys = [...url.searchParams.keys()]
  return keys.length ? `${p} ?${keys.join(",")}` : p
}
let rec = null
const reqs = new Map()
listeners.push((d) => {
  if (d.sessionId !== S || !rec) return
  const p = d.params
  if (d.method === "Network.requestWillBeSent" && p.request.url.startsWith(origin + "/v1/")) {
    const r = { t: +(performance.now() - rec.t0).toFixed(0), route: shape(p.request.url), method: p.request.method,
      ms: null, status: null, bytes: null, sse: 0 }
    reqs.set(p.requestId, r)
    rec.list.push(r)
  } else if (d.method === "Network.responseReceived" && reqs.has(p.requestId)) {
    reqs.get(p.requestId).status = p.response.status
  } else if (d.method === "Network.loadingFinished" && reqs.has(p.requestId)) {
    const r = reqs.get(p.requestId)
    r.ms = +(performance.now() - rec.t0 - r.t).toFixed(0)
    r.bytes = p.encodedDataLength
  } else if (d.method === "Network.eventSourceMessageReceived" && reqs.has(p.requestId)) {
    reqs.get(p.requestId).sse++
    rec.sse[p.eventName] = (rec.sse[p.eventName] || 0) + 1
  }
})
const evalJS = async (expr) =>
  (await send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true }, S)).result?.result?.value

async function scenario(name, { w, h, mobile, setup, ms, during }) {
  await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: !!mobile }, S)
  rec = { name, t0: performance.now(), list: [], sse: {} }
  reqs.clear()
  await setup()
  if (during) await during()
  const left = ms - (performance.now() - rec.t0)
  if (left > 0) await sleep(left)
  const out = { name, ms, viewport: `${w}x${h}`, visible: await evalJS("document.visibilityState"), list: rec.list, sse: rec.sse }
  writeFileSync(join(outdir, `${name}.json`), JSON.stringify(out, null, 1))
  console.log(name, rec.list.length, "requests", JSON.stringify(rec.sse))
  rec = null
}

// Sign in once through the device link, then learn a session id. The id stays
// in this process: the files hold route shapes only.
await send("Page.navigate", { url: openURL }, S)
await sleep(4000)
const sid = await evalJS(`fetch('/v1/sessions').then(r=>r.json()).then(j=>{const rows=(j.sessions||j||[]);const r=rows.find(x=>x.isClaude&&x.state!=='working')||rows[0];return r&&r.id})`)
console.log("session for detail:", sid ? "found" : "none")
const DUR = +(process.env.DUR || 180000)
const detail = (n) => origin + `/?audit=${n}#session=` + encodeURIComponent(sid || "")

await scenario("list-phone", { w: 390, h: 844, mobile: true, ms: DUR, setup: () => send("Page.navigate", { url: origin + "/?audit=1#" }, S) })
await scenario("detail-phone", { w: 390, h: 844, mobile: true, ms: DUR, setup: () => send("Page.navigate", { url: detail(2) }, S) })
await scenario("wide-list+detail", { w: 1440, h: 900, ms: DUR, setup: () => send("Page.navigate", { url: detail(3) }, S) })

// Send: the POST is answered here with 200 so nothing reaches the session; the
// reads that follow it are real.
await send("Fetch.enable", { patterns: [{ urlPattern: "*/v1/sessions/*/send", requestStage: "Request" }] }, S)
listeners.push(async (d) => {
  if (d.sessionId === S && d.method === "Fetch.requestPaused") {
    await send("Fetch.fulfillRequest", { requestId: d.params.requestId, responseCode: 200,
      responseHeaders: [{ name: "Content-Type", value: "application/json" }],
      body: Buffer.from('{"ok":true}').toString("base64") }, S)
  }
})
await scenario("send-phone", {
  w: 390, h: 844, mobile: true, ms: +(process.env.SENDDUR || 90000),
  setup: async () => {
    await send("Page.navigate", { url: detail(4) }, S)
    await sleep(15000)
  },
  during: async () => {
    rec.sendAt = +(performance.now() - rec.t0).toFixed(0)
    await evalJS(`(()=>{const b=[...document.querySelectorAll('button.send')].find(x=>x.offsetParent);const ta=b&&b.parentElement.querySelector('[contenteditable]');ta&&ta.focus();return !!ta})()`)
    await send("Input.insertText", { text: "audit probe (not delivered)" }, S)
    await sleep(300)
    const ok = await evalJS(`(()=>{const b=[...document.querySelectorAll('button.send')].find(x=>x.offsetParent);const st=b?('dis='+b.disabled):'nobtn';b&&b.click();return st})()`)
    rec.sendAt = +(performance.now() - rec.t0).toFixed(0)
    console.log("send:", ok, "at", rec.sendAt)
    writeFileSync(join(outdir, "send-at.txt"), String(rec.sendAt))
  },
})

ws.close()
proc.kill()
await sleep(500)
rmSync(profile, { recursive: true, force: true })
