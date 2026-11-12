// The Clawdline extension's service worker (docs/browser.md).
//
// Chrome starts `clawdline browser-host` for this extension through native
// messaging. The host relays commands from the daemon; this worker carries
// them out with chrome.debugger in a dedicated "agent window", so an agent's
// tabs never rearrange the person's own windows, and answers each command
// with one result. It holds no secret and opens no network connection.

import { readPage, locate, focusedIsPassword } from "./page.js"

const HOST = "com.clawdline.browser"
const TEXT_LIMIT = 200 * 1024
const ELEMENT_LIMIT = 300
const LOAD_WAIT_MS = 15000

let port = null
let state = { host: "connecting", daemon: "unknown", since: Date.now() }
const attached = new Set()

function setState(next) {
  state = { ...state, ...next, since: Date.now() }
  chrome.storage.session.set({ state }).catch(() => {})
}

function connect() {
  try {
    port = chrome.runtime.connectNative(HOST)
  } catch (e) {
    setState({ host: "missing", error: String(e && e.message || e) })
    return
  }
  setState({ host: "connected", error: "" })
  port.postMessage({ hello: { version: chrome.runtime.getManifest().version, browser: navigator.userAgent } })
  port.onMessage.addListener(onHostMessage)
  port.onDisconnect.addListener(() => {
    const error = chrome.runtime.lastError ? chrome.runtime.lastError.message : ""
    port = null
    setState({ host: "missing", daemon: "unknown", error })
    setTimeout(connect, 5000)
  })
}

async function onHostMessage(msg) {
  if (msg.daemon) {
    setState({ daemon: msg.daemon })
    return
  }
  if (!msg.command) return
  const c = msg.command
  let result
  try {
    const out = await run(c)
    result = { id: c.id, ok: true, ...out }
  } catch (e) {
    const tab = c.tab || 0
    let url = ""
    if (tab) url = (await chrome.tabs.get(tab).catch(() => ({}))).url || ""
    result = { id: c.id, ok: false, error: String(e && e.message || e).slice(0, 500), tab, url }
  }
  if (port) port.postMessage({ results: [result] })
}

// ---- the agent window ----

async function agentWindow() {
  const { agentWindowId } = await chrome.storage.session.get("agentWindowId")
  if (agentWindowId) {
    const w = await chrome.windows.get(agentWindowId).catch(() => null)
    if (w) return w.id
  }
  return null
}

async function openTab(url) {
  const existing = await agentWindow()
  let tab
  if (existing) {
    tab = await chrome.tabs.create({ windowId: existing, url, active: true })
  } else {
    const w = await chrome.windows.create({ url, focused: false, width: 1280, height: 860 })
    await chrome.storage.session.set({ agentWindowId: w.id })
    tab = w.tabs[0]
  }
  await waitForLoad(tab.id)
  return tab.id
}

function waitForLoad(tabId) {
  return new Promise((resolve) => {
    const timer = setTimeout(done, LOAD_WAIT_MS)
    function done() {
      clearTimeout(timer)
      chrome.tabs.onUpdated.removeListener(listen)
      resolve()
    }
    function listen(id, info) { if (id === tabId && info.status === "complete") done() }
    chrome.tabs.onUpdated.addListener(listen)
    chrome.tabs.get(tabId).then((t) => { if (t.status === "complete") done() }).catch(done)
  })
}

// ---- chrome.debugger ----

async function ensureAttached(tabId) {
  if (attached.has(tabId)) return
  await chrome.debugger.attach({ tabId }, "1.3")
  attached.add(tabId)
}

chrome.debugger.onDetach.addListener((source) => attached.delete(source.tabId))
chrome.tabs.onRemoved.addListener((tabId) => attached.delete(tabId))

const cdp = (tabId, method, params = {}) => chrome.debugger.sendCommand({ tabId }, method, params)

async function evaluate(tabId, fn, ...args) {
  const expression = `(${fn.toString()})(${args.map((a) => JSON.stringify(a)).join(",")})`
  const r = await cdp(tabId, "Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true })
  if (r.exceptionDetails) throw new Error("the page refused the script: " + (r.exceptionDetails.text || "exception"))
  return r.result.value
}

async function click(tabId, x, y) {
  for (const type of ["mouseMoved", "mousePressed", "mouseReleased"]) {
    await cdp(tabId, "Input.dispatchMouseEvent", { type, x, y, button: type === "mouseMoved" ? "none" : "left", clickCount: 1 })
  }
}

const KEYS = {
  Enter: { code: "Enter", keyCode: 13, text: "\r" },
  Tab: { code: "Tab", keyCode: 9 },
  Backspace: { code: "Backspace", keyCode: 8 },
  Escape: { code: "Escape", keyCode: 27 },
  ArrowDown: { code: "ArrowDown", keyCode: 40 },
  ArrowUp: { code: "ArrowUp", keyCode: 38 },
  ArrowLeft: { code: "ArrowLeft", keyCode: 37 },
  ArrowRight: { code: "ArrowRight", keyCode: 39 },
  PageDown: { code: "PageDown", keyCode: 34 },
  PageUp: { code: "PageUp", keyCode: 33 },
}

async function key(tabId, name) {
  const k = KEYS[name]
  if (!k) throw new Error(`unknown key ${name}; use one of ${Object.keys(KEYS).join(", ")}`)
  const base = { key: name, code: k.code, windowsVirtualKeyCode: k.keyCode, nativeVirtualKeyCode: k.keyCode }
  await cdp(tabId, "Input.dispatchKeyEvent", { type: k.text ? "keyDown" : "rawKeyDown", ...base, text: k.text })
  await cdp(tabId, "Input.dispatchKeyEvent", { type: "keyUp", ...base })
}

async function tabURL(tabId) {
  return (await chrome.tabs.get(tabId).catch(() => ({}))).url || ""
}

// ---- commands ----

async function run(c) {
  const a = c.args || {}
  if (c.op === "open") {
    const tab = await openTab(a.url)
    const url = await tabURL(tab)
    const t = await chrome.tabs.get(tab)
    return { tab, url, value: { tab, url, title: t.title } }
  }
  if (c.op === "tabs") {
    const w = await agentWindow()
    const tabs = w ? await chrome.tabs.query({ windowId: w }) : []
    return { value: { tabs: tabs.map((t) => ({ tab: t.id, url: t.url, title: t.title })) } }
  }
  const tab = c.tab
  if (!tab) throw new Error("no tab")
  await chrome.tabs.get(tab).catch(() => { throw new Error("the tab was closed") })
  if (c.op === "release") {
    if (attached.has(tab)) await chrome.debugger.detach({ tabId: tab }).catch(() => {})
    attached.delete(tab)
    return { tab, url: await tabURL(tab), value: { tab } }
  }
  await ensureAttached(tab)
  switch (c.op) {
    case "read": {
      const page = await evaluate(tab, readPage, TEXT_LIMIT, ELEMENT_LIMIT)
      return { tab, url: page.url, value: { tab, ...page } }
    }
    case "click": {
      let x = a.x, y = a.y, name = ""
      if (a.target !== undefined) {
        const hit = await evaluate(tab, locate, a.target)
        if (!hit.found) throw new Error(`no visible element matches "${String(a.target).slice(0, 80)}"; read the page for the exact labels`)
        ;({ x, y, name } = hit)
        await new Promise((r) => setTimeout(r, 150))
      }
      await click(tab, x, y)
      await new Promise((r) => setTimeout(r, 300))
      return { tab, url: await tabURL(tab), target: name || `${Math.round(x)},${Math.round(y)}`, value: { tab, clicked: name || null, x: Math.round(x), y: Math.round(y) } }
    }
    case "type": {
      const password = await evaluate(tab, focusedIsPassword)
      await cdp(tab, "Input.insertText", { text: String(a.text) })
      return { tab, url: await tabURL(tab), password, value: { tab, typed: [...String(a.text)].length } }
    }
    case "key": {
      await key(tab, a.key)
      await new Promise((r) => setTimeout(r, 200))
      return { tab, url: await tabURL(tab), target: a.key, value: { tab, key: a.key } }
    }
    case "scroll": {
      const m = await cdp(tab, "Page.getLayoutMetrics")
      const vw = m.cssLayoutViewport.clientWidth, vh = m.cssLayoutViewport.clientHeight
      await cdp(tab, "Input.dispatchMouseEvent", { type: "mouseWheel", x: vw / 2, y: vh / 2, deltaX: 0, deltaY: (a.direction === "up" ? -1 : 1) * vh * 0.8 })
      await new Promise((r) => setTimeout(r, 250))
      return { tab, url: await tabURL(tab), value: { tab, direction: a.direction } }
    }
    case "screenshot": {
      const shot = await cdp(tab, "Page.captureScreenshot", { format: "jpeg", quality: 60 })
      const url = await tabURL(tab)
      return { tab, url, value: { tab, url, data: shot.data } }
    }
  }
  throw new Error(`unknown command ${c.op}`)
}

chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  if (msg && msg.type === "status") reply(state)
})

connect()
