#!/usr/bin/env node
// Measurement-only prototype for the Chrome integration (board item
// "Chrome extension integration"). It serves test pages and a WebSocket on
// 127.0.0.1:7791, launches a Chromium that loads ./extension, drives it through
// chrome.debugger and writes the measurements as JSON.
//
//   node tools/browser-probe/run.mjs --browser <Chromium binary> --out result.json
//   node tools/browser-probe/run.mjs --no-launch --only coexist   # extension loaded by hand
//
// Nothing here talks to the Clawdline daemon on 7727. The browser profile is a
// fresh temporary directory removed at exit.

import { createServer } from "node:http"
import { createHash } from "node:crypto"
import { spawn } from "node:child_process"
import { mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join, dirname } from "node:path"
import { fileURLToPath } from "node:url"

const PORT = 7791
const here = dirname(fileURLToPath(import.meta.url))
const args = process.argv.slice(2)
const opt = (name, fallback) => { const i = args.indexOf(name); return i >= 0 ? args[i + 1] : fallback }
const flag = (name) => args.includes(name)
const only = opt("--only", null)
const out = opt("--out", null)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// ---------- pages ----------
const pages = {
  "/anim": `<!doctype html><meta charset=utf-8><title>anim</title>
<style>body{margin:0;font:16px system-ui;background:#fafafa}#b{position:absolute;width:120px;height:120px;background:#d97757;top:200px}
#t{position:absolute;top:40px;left:40px;font:48px ui-monospace}</style><div id=t></div><div id=b></div>
<script>let x=0;function f(){x=(x+4)%900;b.style.left=x+'px';t.textContent=Date.now();requestAnimationFrame(f)}f()</script>`,
  "/static": `<!doctype html><meta charset=utf-8><title>static</title><body style="font:16px system-ui;margin:40px">
<h1>Static page</h1><p>${"Nothing on this page moves. ".repeat(40)}</p></body>`,
  "/long": `<!doctype html><meta charset=utf-8><title>long</title><body style="font:16px/1.6 system-ui;margin:40px;max-width:900px">
${Array.from({length:200},(_,i)=>`<h2>Section ${i+1}</h2><p>${"This paragraph stands in for a real article: mixed words, links and numbers 0123456789. ".repeat(6)}</p><table border=1 cellpadding=6><tr><td>row ${i}</td><td>value ${i*7}</td><td>status ok</td></tr></table>`).join("")}</body>`,
  "/input": `<!doctype html><meta charset=utf-8><title>input</title><body style="margin:0;font:16px system-ui">
<button id=go style="position:absolute;left:100px;top:100px;width:200px;height:80px">Press</button>
<input id=f style="position:absolute;left:100px;top:240px;width:300px;height:36px">
<script>go.addEventListener('pointerdown',()=>fetch('/hit?t='+Date.now()));</script></body>`,
}

// ---------- minimal WebSocket server (RFC 6455, enough for one local client) ----------
let ext = null
let nextId = 1
const pending = new Map()
const listeners = new Set()
const hits = []
let connected = null
const connectedP = new Promise((r) => (connected = r))

function wsSend(sock, text) {
  const payload = Buffer.from(text)
  const len = payload.length
  const head = len < 126 ? Buffer.from([0x81, len])
    : len < 65536 ? Buffer.from([0x81, 126, len >> 8, len & 255])
    : (() => { const b = Buffer.alloc(10); b[0] = 0x81; b[1] = 127; b.writeBigUInt64BE(BigInt(len), 2); return b })()
  sock.write(Buffer.concat([head, payload]))
}

function wsAccept(req, sock) {
  const key = req.headers["sec-websocket-key"]
  const accept = createHash("sha1").update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest("base64")
  sock.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`)
  let buf = Buffer.alloc(0)
  let parts = []
  sock.on("data", (chunk) => {
    buf = Buffer.concat([buf, chunk])
    for (;;) {
      if (buf.length < 2) return
      const fin = buf[0] & 0x80, op = buf[0] & 0x0f
      let len = buf[1] & 0x7f, off = 2
      if (len === 126) { if (buf.length < 4) return; len = buf.readUInt16BE(2); off = 4 }
      else if (len === 127) { if (buf.length < 10) return; len = Number(buf.readBigUInt64BE(2)); off = 10 }
      const masked = buf[1] & 0x80
      const total = off + (masked ? 4 : 0) + len
      if (buf.length < total) return
      let data = buf.subarray(off + (masked ? 4 : 0), total)
      if (masked) { const m = buf.subarray(off, off + 4); data = Buffer.from(data.map((v, i) => v ^ m[i & 3])) }
      buf = buf.subarray(total)
      if (op === 8) { sock.end(); return }
      if (op === 9) { sock.write(Buffer.from([0x8a, 0])); continue }
      if (op === 1 || op === 0) {
        parts.push(data)
        if (fin) { const text = Buffer.concat(parts).toString(); parts = []; onMessage(sock, text, Date.now()) }
      }
    }
  })
  sock.on("close", () => { if (ext === sock) { ext = null; log("extension disconnected") } })
}

function onMessage(sock, text, recv) {
  const msg = JSON.parse(text)
  if (msg.hello) { ext = sock; log("extension connected"); connected(); return }
  if (msg.event) { msg.recv = recv; msg.bytes = text.length; for (const l of listeners) l(msg); return }
  const p = pending.get(msg.id)
  if (p) { pending.delete(msg.id); msg.ok ? p.resolve(msg.value) : p.reject(new Error(msg.error)) }
}

function call(op, fields = {}, timeout = 15000) {
  if (!ext) return Promise.reject(new Error("extension not connected"))
  const id = nextId++
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    setTimeout(() => { if (pending.delete(id)) reject(new Error(`timeout ${op} ${fields.method || ""}`)) }, timeout)
    wsSend(ext, JSON.stringify({ id, op, ...fields }))
  })
}
const cdp = (tabId, method, params, timeout) => call("cdp", { tabId, method, params }, timeout)

const server = createServer((req, res) => {
  const u = new URL(req.url, `http://127.0.0.1:${PORT}`)
  if (u.pathname === "/hit") { hits.push({ page: Number(u.searchParams.get("t")), recv: Date.now() }); res.end("ok"); return }
  const page = pages[u.pathname]
  if (!page) { res.statusCode = 404; res.end(); return }
  res.setHeader("content-type", "text/html; charset=utf-8"); res.end(page)
})
server.on("upgrade", (req, sock) => req.url === "/ext" ? wsAccept(req, sock) : sock.destroy())

function log(...a) { console.error(new Date().toISOString().slice(11, 23), ...a) }
const pct = (xs, p) => { if (!xs.length) return null; const s = [...xs].sort((a, b) => a - b); return s[Math.min(s.length - 1, Math.floor(p / 100 * s.length))] }
const round = (v) => v == null ? null : Math.round(v * 10) / 10

// ---------- experiments ----------
async function screencast(tabId, seconds, params, during) {
  const frames = []
  const l = (m) => {
    if (m.tabId !== tabId || m.method !== "Page.screencastFrame") return
    const ts = m.params.metadata.timestamp
    const captured = ts > 1e11 ? ts : ts * 1000
    frames.push({ recv: m.recv, captureToServer: m.recv - captured, extToServer: m.recv - m.sent, jpegBytes: Math.floor(m.params.data.length * 3 / 4), wireBytes: m.bytes })
  }
  listeners.add(l)
  await cdp(tabId, "Page.startScreencast", params)
  const t0 = Date.now()
  if (during) await during()
  await sleep(Math.max(0, seconds * 1000 - (Date.now() - t0)))
  await cdp(tabId, "Page.stopScreencast", {})
  listeners.delete(l)
  const dur = (Date.now() - t0) / 1000
  const jpeg = frames.reduce((s, f) => s + f.jpegBytes, 0)
  return {
    seconds: round(dur), params, frames: frames.length, fps: round(frames.length / dur),
    jpegKBps: round(jpeg / dur / 1024), meanFrameKB: frames.length ? round(jpeg / frames.length / 1024) : null,
    captureToServerMs: { p50: round(pct(frames.map((f) => f.captureToServer), 50)), p95: round(pct(frames.map((f) => f.captureToServer), 95)) },
    extToServerMs: { p50: round(pct(frames.map((f) => f.extToServer), 50)), p95: round(pct(frames.map((f) => f.extToServer), 95)) },
  }
}

async function shot(tabId) {
  const t = Date.now()
  try { const r = await cdp(tabId, "Page.captureScreenshot", { format: "jpeg", quality: 60 }, 8000); return { ok: true, ms: Date.now() - t, kb: round(r.data.length * 3 / 4 / 1024) } }
  catch (e) { return { ok: false, ms: Date.now() - t, error: e.message } }
}

const results = { at: new Date().toISOString(), node: process.version }

async function main() {
  server.listen(PORT, "127.0.0.1")
  let browser = null, profile = null
  if (!flag("--no-launch")) {
    const bin = opt("--browser")
    if (!bin) throw new Error("--browser <binary> is required unless --no-launch")
    profile = mkdtempSync(join(tmpdir(), "browser-probe-"))
    browser = spawn(bin, [`--user-data-dir=${profile}`, `--load-extension=${join(here, "extension")}`,
      `--disable-extensions-except=${join(here, "extension")}`, "--no-first-run", "--no-default-browser-check",
      "--window-size=1280,860", `http://127.0.0.1:${PORT}/static`], { stdio: "ignore" })
    const v = spawn(bin, ["--version"]); v.stdout.on("data", (d) => (results.browser = String(d).trim()))
  }
  log("waiting for the extension")
  await Promise.race([connectedP, sleep(30000).then(() => { throw new Error("extension did not connect in 30 s") })])
  try {
    const want = (n) => !only || only.split(",").includes(n)
    const tab = async (path, active = true) => { const t = await call("create", { url: `http://127.0.0.1:${PORT}${path}`, active }); await sleep(1200); return t }

    if (want("frames")) {
      const a = await tab("/anim"); await call("attach", { tabId: a.id })
      results.animDesktop = await screencast(a.id, 15, { format: "jpeg", quality: 70, maxWidth: 1280, maxHeight: 800 })
      results.animPhone = await screencast(a.id, 15, { format: "jpeg", quality: 50, maxWidth: 800, maxHeight: 800 })
      results.animPhoneEvery3 = await screencast(a.id, 15, { format: "jpeg", quality: 50, maxWidth: 800, maxHeight: 800, everyNthFrame: 3 })
      const s = await tab("/static"); await call("attach", { tabId: s.id })
      results.staticDesktop = await screencast(s.id, 10, { format: "jpeg", quality: 70, maxWidth: 1280, maxHeight: 800 })
      results.screenshotForeground = await shot(a.id)

      // Background: the animated tab keeps running while another tab is in front.
      await call("activate", { tabId: s.id }); await sleep(1500)
      results.animBackground = await screencast(a.id, 10, { format: "jpeg", quality: 70, maxWidth: 1280, maxHeight: 800 })
      results.screenshotBackground = await shot(a.id)

      // Minimized window.
      await call("activate", { tabId: a.id }); await sleep(800)
      await call("window", { windowId: a.windowId, update: { state: "minimized" } }); await sleep(1500)
      results.animMinimized = await screencast(a.id, 10, { format: "jpeg", quality: 70, maxWidth: 1280, maxHeight: 800 })
      results.screenshotMinimized = await shot(a.id)
      await call("window", { windowId: a.windowId, update: { state: "normal" } }); await sleep(1000)
    }

    if (want("scroll")) {
      const l = await tab("/long"); await call("attach", { tabId: l.id })
      const wheel = async () => { for (let i = 0; i < 40; i++) { await cdp(l.id, "Input.dispatchMouseEvent", { type: "mouseWheel", x: 400, y: 400, deltaX: 0, deltaY: 120 }); await sleep(250) } }
      results.scrollDesktop = await screencast(l.id, 10, { format: "jpeg", quality: 70, maxWidth: 1280, maxHeight: 800 }, wheel)
      results.scrollPhone = await screencast(l.id, 10, { format: "jpeg", quality: 50, maxWidth: 800, maxHeight: 800 }, wheel)
      results.scrollPhoneEvery3 = await screencast(l.id, 10, { format: "jpeg", quality: 50, maxWidth: 800, maxHeight: 800, everyNthFrame: 3 }, wheel)
      const a = await tab("/anim"); await call("attach", { tabId: a.id })
      // Chrome refuses bounds more than half off-screen, so cover the window
      // with a second window of the same size instead (macOS occlusion).
      const win = await call("window", { windowId: a.windowId, update: { left: 60, top: 60, width: 1200, height: 800 } })
      const cover = await call("newWindow", { create: { url: `http://127.0.0.1:${PORT}/static`, left: 40, top: 40, width: 1280, height: 860, focused: true } }); await sleep(2000)
      results.animCovered = await screencast(a.id, 8, { format: "jpeg", quality: 70, maxWidth: 1280, maxHeight: 800 })
      results.screenshotCovered = await shot(a.id)
      results.coveredSetup = { windowFocused: false, coverWindow: cover.id !== win.id }
    }

    if (want("input")) {
      const t = await tab("/input"); await call("attach", { tabId: t.id })
      const clicks = []
      for (let i = 0; i < 20; i++) {
        hits.length = 0
        const sent = Date.now()
        await cdp(t.id, "Input.dispatchMouseEvent", { type: "mousePressed", x: 200, y: 140, button: "left", clickCount: 1 })
        const acked = Date.now()
        await cdp(t.id, "Input.dispatchMouseEvent", { type: "mouseReleased", x: 200, y: 140, button: "left", clickCount: 1 })
        for (let w = 0; w < 50 && !hits.length; w++) await sleep(10)
        clicks.push({ toPage: hits[0] ? hits[0].page - sent : null, ack: acked - sent })
        await sleep(100)
      }
      results.click = { n: clicks.length, landed: clicks.filter((c) => c.toPage != null).length,
        serverToPageHandlerMs: { p50: pct(clicks.filter((c) => c.toPage != null).map((c) => c.toPage), 50), p95: pct(clicks.filter((c) => c.toPage != null).map((c) => c.toPage), 95) },
        commandRoundTripMs: { p50: pct(clicks.map((c) => c.ack), 50), p95: pct(clicks.map((c) => c.ack), 95) } }
      await cdp(t.id, "Input.dispatchMouseEvent", { type: "mousePressed", x: 250, y: 258, button: "left", clickCount: 1 })
      await cdp(t.id, "Input.dispatchMouseEvent", { type: "mouseReleased", x: 250, y: 258, button: "left", clickCount: 1 })
      const text = "測試中文輸入 hello"
      await cdp(t.id, "Input.insertText", { text })
      await cdp(t.id, "Input.dispatchKeyEvent", { type: "keyDown", key: "Backspace", code: "Backspace", windowsVirtualKeyCode: 8 })
      await cdp(t.id, "Input.dispatchKeyEvent", { type: "keyUp", key: "Backspace", code: "Backspace", windowsVirtualKeyCode: 8 })
      const v = await cdp(t.id, "Runtime.evaluate", { expression: "document.getElementById('f').value", returnByValue: true })
      results.insertText = { typed: text, thenBackspace: true, fieldValue: v.result.value, matches: v.result.value === text.slice(0, -1) }
      // Alerts block Claude in Chrome; CDP can answer them.
      const dialog = new Promise((r) => { const l = (m) => { if (m.method === "Page.javascriptDialogOpening") { listeners.delete(l); r(m.params.type) } }; listeners.add(l) })
      await cdp(t.id, "Page.enable", {})
      cdp(t.id, "Runtime.evaluate", { expression: "alert('probe'); 'after'", returnByValue: true }).catch(() => {})
      const type = await Promise.race([dialog, sleep(3000).then(() => null)])
      let handled = false
      if (type) { await cdp(t.id, "Page.handleJavaScriptDialog", { accept: true }); handled = true }
      results.dialog = { opened: type, handled }
    }

    if (want("idle")) {
      // MV3 may suspend an idle service worker; does the socket survive 45 s of silence?
      const before = Date.now(); await sleep(45000)
      let alive = false, err = null
      try { await call("ping", {}, 3000); alive = true } catch (e) { err = e.message }
      results.idle45s = { alive, err, waitedS: round((Date.now() - before) / 1000) }
    }

    if (want("coexist")) {
      // Run with --no-launch against a Chrome where Claude in Chrome is also active.
      const targets = await call("targets")
      results.coexist = { targets: targets.filter((t) => t.type === "page").map((t) => ({ attached: t.attached, url: (t.url || "").replace(/\?.*$/, "") })) }
      const tabId = Number(opt("--tab", "0"))
      if (tabId) {
        try { await call("attach", { tabId }); results.coexist.attachToTab = { ok: true }; results.coexist.screenshot = await shot(tabId); await call("detach", { tabId }) }
        catch (e) { results.coexist.attachToTab = { ok: false, error: e.message } }
      }
    }
  } finally {
    const text = JSON.stringify(results, null, 2)
    if (out) writeFileSync(out, text + "\n"); console.log(text)
    if (browser) browser.kill()
    server.close()
    if (profile) { await sleep(1000); rmSync(profile, { recursive: true, force: true }) }
    setTimeout(() => process.exit(0), 200)
  }
}

main().catch((e) => { console.error(e); process.exit(1) })
