import test from "node:test"
import assert from "node:assert/strict"
import vm from "node:vm"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const entry = fileURLToPath(new URL("../demo/bridge.ts", import.meta.url))
const bundle = await build({ entryPoints: [entry], bundle: true, write: false, format: "iife", platform: "browser" })
function demo(lang = "zh-Hant") {
  let network = 0
  let savedStorageReads = 0
  const context = vm.createContext({
    URL, Request, Response, Headers, EventTarget, Event, MessageEvent, queueMicrotask, navigator: {},
    location: { href: `https://example.invalid/demo/console/?lang=${lang}`, origin: "https://example.invalid" },
    fetch: async () => { network++; throw Error("A real network request escaped the demo") },
    localStorage: { getItem() { savedStorageReads++; return "private draft" } },
  })
  context.window = context
  vm.runInContext(bundle.outputFiles[0].text, context)
  return { context, network: () => network, savedStorageReads: () => savedStorageReads,
    request: async (path, init) => context.fetch(path, init),
    read: async path => (await context.fetch(path)).json() }
}

test("demo refuses real services and unsupported writes without reading saved data", async () => {
  const d = demo()
  const before = await d.read("/v1/sessions")
  for (const url of ["http://127.0.0.1:7727/v1/sessions", "https://api.clawdline.com/v1/accounts", "/v1/sessions/checkout/close"]) {
    const response = await d.request(url, { method: "POST" })
    assert.equal(response.status, 503)
    assert.equal((await response.json()).error, "demo_unavailable")
  }
  assert.deepEqual((await d.read("/v1/sessions")).sessions, before.sessions)
  assert.equal(d.network(), 0)
  assert.equal(d.savedStorageReads(), 0)
  await assert.rejects(() => d.context.navigator.serviceWorker.register("/sw.js"), /demo_unavailable/)
})

test("menu answers update the synthetic transcript and publish its new state", async () => {
  const d = demo()
  const events = []
  const source = new d.context.EventSource("/v1/events")
  source.addEventListener("sessions", e => events.push(JSON.parse(e.data)))
  await Promise.resolve()
  const rejected = await d.request("/v1/sessions/signin/key", { method: "POST", body: JSON.stringify({ key: "1" }) })
  assert.equal(rejected.status, 503)
  const response = await d.request("/v1/sessions/checkout/key", { method: "POST", body: JSON.stringify({ key: "2" }) })
  assert.equal(response.status, 200)
  const transcript = await d.read("/v1/transcript?session=checkout")
  assert.equal(transcript.entries.at(-2).text, "直接上線。")
  assert.match(transcript.entries.at(-1).text, /沒有執行真正的部署/)
  assert.equal(events.at(-1).sessions.find(row => row.id === "checkout").menu, null)
  assert.equal(d.network(), 0)
  source.close()
})

test("messages stay in the document and a fresh visit resets the story", async () => {
  const d = demo("en")
  assert.equal((await d.read("/v1/strings")).lang, "en")
  await d.request("/v1/sessions/search/send", { method: "POST", body: JSON.stringify({ text: "Please check the phone layout." }) })
  const transcript = await d.read("/v1/transcript?session=search")
  assert.equal(transcript.entries.at(-2).text, "Please check the phone layout.")
  assert.equal((await d.read("/v1/sessions")).sessions.find(row => row.id === "checkout").state, "waiting")
  assert.match(transcript.entries.at(-1).text, /demonstration reply/)
  const fresh = demo("en")
  assert.equal((await fresh.read("/v1/transcript?session=search")).entries.length, 2)
  assert.equal(d.network(), 0)
})
