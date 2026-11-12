// Measurement-only prototype. It connects to the probe server started by
// tools/browser-probe/run.mjs and relays chrome.debugger calls and events.
// It is not the product extension: there is no pairing, no allowlist and no
// lease. Every message is JSON: requests {id, op, ...}, replies {id, ok, value|error},
// events {event, tabId, method, params, recv}.

const PORT = 7791
let socket = null

function connect() {
  socket = new WebSocket(`ws://127.0.0.1:${PORT}/ext`)
  socket.onopen = () => send({ hello: "probe-extension", at: Date.now() })
  socket.onmessage = (m) => handle(JSON.parse(m.data))
  socket.onclose = () => { socket = null; setTimeout(connect, 1000) }
  socket.onerror = () => {}
}

function send(obj) {
  if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(obj))
}

chrome.debugger.onEvent.addListener((source, method, params) => {
  if (method === "Page.screencastFrame") {
    // Acknowledge at once so Chrome keeps sending; the server measures arrival.
    chrome.debugger.sendCommand(source, "Page.screencastFrameAck", { sessionId: params.sessionId })
  }
  send({ event: true, tabId: source.tabId, method, params, sent: Date.now() })
})

chrome.debugger.onDetach.addListener((source, reason) => {
  send({ event: true, tabId: source.tabId, method: "detached", params: { reason }, sent: Date.now() })
})

async function run(req) {
  switch (req.op) {
    case "ping": return { at: Date.now() }
    case "tabs": return await chrome.tabs.query({})
    case "create": return await chrome.tabs.create({ url: req.url, active: req.active !== false })
    case "activate": return await chrome.tabs.update(req.tabId, { active: true })
    case "newWindow": return await chrome.windows.create(req.create)
    case "window": return await chrome.windows.update(req.windowId, req.update)
    case "attach": await chrome.debugger.attach({ tabId: req.tabId }, "1.3"); return true
    case "detach": await chrome.debugger.detach({ tabId: req.tabId }); return true
    case "targets": return await chrome.debugger.getTargets()
    case "cdp": return await chrome.debugger.sendCommand({ tabId: req.tabId }, req.method, req.params || {})
    default: throw new Error(`unknown op ${req.op}`)
  }
}

// The popup asks whether the probe server is connected.
chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  if (msg && msg.type === "status") reply({ connected: !!socket && socket.readyState === WebSocket.OPEN, port: PORT })
})

async function handle(req) {
  if (req.id === undefined) return
  try {
    send({ id: req.id, ok: true, value: await run(req) })
  } catch (e) {
    send({ id: req.id, ok: false, error: String(e && e.message || e) })
  }
}

connect()
