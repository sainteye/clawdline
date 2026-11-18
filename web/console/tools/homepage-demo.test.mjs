import test from "node:test"
import assert from "node:assert/strict"
import vm from "node:vm"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import { artifactPresentation } from "../src/legacy/js/view/transcript-images.js"

const entry = fileURLToPath(new URL("../demo/bridge.ts", import.meta.url))
const bundle = await build({ entryPoints: [entry], bundle: true, write: false, format: "iife", platform: "browser", loader: { ".png": "dataurl" } })
function demo(lang = "zh-Hant") {
  let network = 0
  let savedStorageReads = 0
  const context = vm.createContext({
    URL, Request, Response, Headers, EventTarget, Event, MessageEvent, queueMicrotask, TextEncoder, navigator: {},
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
  for (const url of ["http://127.0.0.1:7727/v1/sessions", "https://api.clawdline.com/v1/accounts", "/v1/sessions/e/close"]) {
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
  const rejected = await d.request("/v1/sessions/b/key", { method: "POST", body: JSON.stringify({ key: "1" }) })
  assert.equal(rejected.status, 503)
  const response = await d.request("/v1/sessions/e/key", { method: "POST", body: JSON.stringify({ key: "2" }) })
  assert.equal(response.status, 200)
  const transcript = await d.read("/v1/transcript?session=e")
  assert.equal(transcript.entries.at(-2).text, "改個發佈時段。")
  assert.match(transcript.entries.at(-1).text, /沒有執行真正的部署/)
  assert.equal(events.at(-1).sessions.find(row => row.id === "e").menu, null)
  assert.equal(d.network(), 0)
  source.close()
})

test("messages stay in the document and a fresh visit resets the story", async () => {
  const d = demo("en")
  assert.equal((await d.read("/v1/strings")).lang, "en")
  await d.request("/v1/sessions/f/send", { method: "POST", body: JSON.stringify({ text: "Please check the phone layout." }) })
  const transcript = await d.read("/v1/transcript?session=f")
  assert.equal(transcript.entries.at(-2).text, "Please check the phone layout.")
  assert.equal((await d.read("/v1/sessions")).sessions.find(row => row.id === "e").state, "waiting")
  assert.match(transcript.entries.at(-1).text, /demonstration reply/)
  const fresh = demo("en")
  assert.equal((await fresh.read("/v1/transcript?session=f")).entries.length, 4)
  assert.equal(d.network(), 0)
})

test("the homepage keeps all seven original Sessions and their Project identities", async () => {
  const d = demo()
  const { sessions } = await d.read("/v1/sessions")
  assert.equal(sessions.length, 7)
  assert.deepEqual(sessions.map(s => s.tty), ["ttys004", "ttys012", "ttys007", "ttys016", "ttys009", "ttys014", "ttys021"])
  assert.deepEqual(sessions.map(s => s.icon.accent.toLowerCase()), ["#d97757", "#7fb0d4", "#e3b341", "#c98bbd", "#5fb8c0", "#7fb069", "#d97757"])
  assert.deepEqual(sessions.filter(s => s.parent_session_id === "a").map(s => s.id), ["b", "c", "d"])
  const { places } = await d.read("/v1/places")
  assert.deepEqual(places.map(p => p.id), ["clawdline", "harbour", "lantern", "orchard", "beacon", "meadow"])
  assert.equal(sessions.filter(s => s.work_state === "waiting_you").length, 1)
  assert.equal(sessions.find(s => s.id === "d").work_state, "milestone_complete")
  assert.equal(sessions.find(s => s.id === "g").state, "unknown")
  assert.equal((await d.read("/v1/orchestrator/tasks")).tasks.length, 3)
})


test("the original Agent attachment is readable by the production image tile", async () => {
  const d = demo()
  const transcript = await d.read("/v1/transcript?session=b")
  const picture = transcript.entries.at(-1).artifacts[0]
  assert.equal(artifactPresentation(picture).state, "loading")
  assert.equal(picture.media_type, "image/png")
  assert.equal(picture.width, 780)
  assert.equal(picture.height, 1320)
})
