// The Clawdline extension's service worker (docs/browser.md).
//
// Chrome starts `clawdline browser-host` for this extension through native
// messaging. The host relays commands from the daemon; this worker carries
// them out with chrome.debugger in a dedicated "agent window", so an agent's
// tabs never rearrange the person's own windows, and answers each command
// with one result. It holds no secret and opens no network connection.

import { readPage, locate, editableRects, focusedEditableKind } from "./page.js"

const HOST = "com.clawdline.browser"
const TEXT_LIMIT = 200 * 1024
const ELEMENT_LIMIT = 300
const LOAD_WAIT_MS = 15000

let port = null
let state = { host: "connecting", daemon: "unknown", since: Date.now() }
const attached = new Set()
const layouts = new Map()

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
  try {
    await chrome.debugger.attach({ tabId }, "1.3")
  } catch (e) {
    // Chrome lets one debugger client own a tab; another extension (Claude in
    // Chrome, for one) or DevTools may already hold it. Say so by name.
    const said = String(e && e.message || e)
    if (/already attached|another debugger/i.test(said)) throw new Error("browser_other_debugger: another program is already controlling this tab")
    throw e
  }
  attached.add(tabId)
}

// A small picture of the page after an action, for the person's list of what
// the agent did. Best effort: an action never fails for want of it.
async function thumb(tabId) {
  try {
    const m = await cdp(tabId, "Page.getLayoutMetrics")
    const w = m.cssLayoutViewport.clientWidth, h = m.cssLayoutViewport.clientHeight
    const scale = Math.min(1, 320 / Math.max(1, w))
    const shot = await cdp(tabId, "Page.captureScreenshot", { format: "jpeg", quality: 35, clip: { x: 0, y: 0, width: w, height: h, scale } })
    return shot.data.length <= 96 * 1024 ? shot.data : undefined
  } catch {
    return undefined
  }
}

chrome.debugger.onDetach.addListener((source) => { attached.delete(source.tabId); layouts.delete(source.tabId) })
chrome.tabs.onRemoved.addListener((tabId) => { attached.delete(tabId); layouts.delete(tabId) })

const cdp = (tabId, method, params = {}) => chrome.debugger.sendCommand({ tabId }, method, params)

async function evaluate(tabId, fn, ...args) {
  const expression = `(${fn.toString()})(${args.map((a) => JSON.stringify(a)).join(",")})`
  const r = await cdp(tabId, "Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true })
  if (r.exceptionDetails) throw new Error("the page refused the script: " + (r.exceptionDetails.text || "exception"))
  return r.result.value
}

// Under the phone layout's device emulation the page lays out at the emulated
// 390 x 720 whatever the real view is, but Chrome can still map dispatched
// mouse coordinates through a scale: the tab's zoom multiplies them, and an
// emulated size larger than the actual view is fitted into it. The tester's
// agent view once measured 195 x 360, so every pictured tap reached twice its
// position. No CDP metric reported that scale, so measure it: move the mouse
// to a known point and read where the page saw it. The desktop layout reflows
// with the view and maps 1:1. Two alternating probe points guarantee that
// each probe is an actual movement.
const PROBES = [[37, 41], [43, 47]]
const probeTurn = new Map()

async function inputScale(tabId) {
  if (layouts.get(tabId) !== "mobile") return [1, 1]
  const turn = probeTurn.get(tabId) || 0
  probeTurn.set(tabId, 1 - turn)
  const [px, py] = PROBES[turn]
  await evaluate(tabId, () => {
    const w = window
    if (w.__clawdlineProbe) removeEventListener("mousemove", w.__clawdlineProbe, true)
    w.__clawdlineSeen = null
    w.__clawdlineProbe = (e) => {
      w.__clawdlineSeen = [e.clientX, e.clientY]
      removeEventListener("mousemove", w.__clawdlineProbe, true)
      w.__clawdlineProbe = null
    }
    addEventListener("mousemove", w.__clawdlineProbe, true)
  })
  await cdp(tabId, "Input.dispatchMouseEvent", { type: "mouseMoved", x: px, y: py, button: "none" })
  const seen = await evaluate(tabId, () => {
    const w = window, seen = w.__clawdlineSeen
    if (w.__clawdlineProbe) removeEventListener("mousemove", w.__clawdlineProbe, true)
    delete w.__clawdlineProbe; delete w.__clawdlineSeen
    return seen
  })
  // A page that swallowed the probe, or a probe outside the fitted picture,
  // leaves the identity mapping: the same behaviour as before measuring.
  if (!Array.isArray(seen)) return [1, 1]
  const sx = seen[0] / px, sy = seen[1] / py
  const sane = (s) => Number.isFinite(s) && s > 0.05 && s < 20
  return [sane(sx) ? sx : 1, sane(sy) ? sy : 1]
}

async function click(tabId, x, y) {
  const [sx, sy] = await inputScale(tabId)
  for (const type of ["mouseMoved", "mousePressed", "mouseReleased"]) {
    await cdp(tabId, "Input.dispatchMouseEvent", { type, x: x / sx, y: y / sy, button: type === "mouseMoved" ? "none" : "left", clickCount: 1 })
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
    let picture
    try { await ensureAttached(tab); picture = await thumb(tab) } catch {}
    return { tab, url, thumb: picture, value: { tab, url, title: t.title } }
  }
  if (c.op === "tabs") {
    const w = await agentWindow()
    const tabs = w ? await chrome.tabs.query({ windowId: w }) : []
    return { value: { tabs: tabs.map((t) => ({ tab: t.id, url: t.url, title: t.title })) } }
  }
  const tab = c.tab
  if (!tab) throw new Error("no tab")
  await chrome.tabs.get(tab).catch(() => { throw new Error("the tab was closed") })
  if (c.op === "close") {
    await ensureAttached(tab)
    const url = await tabURL(tab)
    await chrome.tabs.remove(tab)
    attached.delete(tab); layouts.delete(tab)
    return { tab, url, value: { tab, closed: true } }
  }
  if (c.op === "release") {
    if (attached.has(tab)) await chrome.debugger.detach({ tabId: tab }).catch(() => {})
    attached.delete(tab)
    return { tab, url: await tabURL(tab), value: { tab } }
  }
  await ensureAttached(tab)
  switch (c.op) {
    case "viewport": {
      if (a.mode !== "mobile" && a.mode !== "desktop") throw new Error("use mobile or desktop")
      const mobile = a.mode === "mobile"
      if (mobile) {
        const userAgent = await evaluate(tab, () => navigator.userAgent)
        await cdp(tab, "Emulation.setUserAgentOverride", { userAgent: userAgent.replace(/\([^)]*\)/, "(Linux; Android 13; Mobile)"), platform: "Android" })
        await cdp(tab, "Emulation.setDeviceMetricsOverride", { width: 390, height: 720, deviceScaleFactor: 1, mobile: true })
      } else {
        await cdp(tab, "Emulation.setUserAgentOverride", { userAgent: "" })
        await cdp(tab, "Emulation.clearDeviceMetricsOverride")
      }
      await cdp(tab, "Emulation.setTouchEmulationEnabled", { enabled: mobile })
      layouts.set(tab, a.mode)
      await new Promise((r) => setTimeout(r, 250))
      return { tab, url: await tabURL(tab), target: a.mode, thumb: c.person ? undefined : await thumb(tab), value: { tab, mode: a.mode } }
    }
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
      return { tab, url: await tabURL(tab), thumb: c.person ? undefined : await thumb(tab), target: name || `${Math.round(x)},${Math.round(y)}`, value: { tab, clicked: name || null, x: Math.round(x), y: Math.round(y), editable: c.person ? await evaluate(tab, focusedEditableKind) : undefined } }
    }
    case "type": {
      await cdp(tab, "Input.insertText", { text: String(a.text) })
      // Input.insertText targets Chrome's current focus, which may be inside
      // closed shadow DOM or another frame. A separate Runtime.evaluate can
      // neither inspect every target nor freeze focus/type until insertion.
      // Mark all typing private, including ordinary text, rather than let a
      // stale or inaccessible focus snapshot authorize plaintext persistence.
      // The bridge's password flag also strips URL/target metadata; never
      // capture a post-input picture or query the page for its typed content.
      return { tab, password: true, value: { tab, typed: [...String(a.text)].length } }
    }
    case "key": {
      await key(tab, a.key)
      await new Promise((r) => setTimeout(r, 200))
      return { tab, url: await tabURL(tab), thumb: c.person ? undefined : await thumb(tab), target: a.key, value: { tab, key: a.key, editable: c.person ? await evaluate(tab, focusedEditableKind) : undefined } }
    }
    case "scroll": {
      const m = await cdp(tab, "Page.getLayoutMetrics")
      const vw = m.cssLayoutViewport.clientWidth, vh = m.cssLayoutViewport.clientHeight
      const [sx, sy] = await inputScale(tab)
      await cdp(tab, "Input.dispatchMouseEvent", { type: "mouseWheel", x: vw / 2 / sx, y: vh / 2 / sy, deltaX: 0, deltaY: (a.direction === "up" ? -1 : 1) * vh * 0.8 })
      await new Promise((r) => setTimeout(r, 250))
      return { tab, url: await tabURL(tab), value: { tab, direction: a.direction } }
    }
    case "screenshot": {
      // The page's own size in CSS pixels comes with the image, so a viewer
      // can turn a tap on the picture into a click on the page.
      const m = await cdp(tab, "Page.getLayoutMetrics")
      const width = m.cssLayoutViewport.clientWidth, height = m.cssLayoutViewport.clientHeight
      const quality = Number.isFinite(a.quality) ? Math.max(20, Math.min(80, a.quality)) : 60
      const shot = await cdp(tab, "Page.captureScreenshot", { format: "jpeg", quality })
      const t = await chrome.tabs.get(tab)
      return { tab, url: t.url || "", value: { tab, url: t.url || "", title: t.title || "", width, height, layout: layouts.get(tab) || "desktop", editables: await evaluate(tab, editableRects, ELEMENT_LIMIT), data: shot.data } }
    }
  }
  throw new Error(`unknown command ${c.op}`)
}

chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  if (msg && msg.type === "status") reply(state)
})

connect()
