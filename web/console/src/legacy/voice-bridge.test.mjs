import assert from "node:assert/strict"
import { test } from "node:test"
import { readFileSync } from "node:fs"
import { execFileSync } from "node:child_process"
import { runInNewContext } from "node:vm"
import ts from "typescript"

const english = JSON.parse(readFileSync(new URL("../../public/catalogs/en.json", import.meta.url)))
const source = process.env.VOICE_TEST_REV
  ? execFileSync("git", ["show", `${process.env.VOICE_TEST_REV}:web/console/src/legacy/voice-bridge.ts`], { encoding: "utf8" })
  : readFileSync(new URL("./voice-bridge.ts", import.meta.url), "utf8")
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText

class Element {
  children = []; dataset = {}; style = {}; listeners = {}; attributes = {}; isConnected = true
  set textContent(value) { this.text = value; this.children = [] }
  get textContent() { return (this.text ?? "") + this.children.map(child => child.textContent).join("") }
  appendChild(child) { this.children.push(child); return child }
  setAttribute(key, value) { this.attributes[key] = value }
  removeAttribute(key) { delete this.attributes[key] }
  addEventListener(name, callback) { this.listeners[name] = callback }
  querySelector(selector) {
    for (const child of this.children) {
      if (child.className === selector.slice(1)) return child
      const found = child.querySelector(selector)
      if (found) return found
    }
    return null
  }
}

function harness(options = {}) {
  let now = 1000, timerID = 0, opens = 0, posts = 0, contextCloses = 0
  const timers = new Map(), intervals = new Map(), recorders = [], tracks = [], words = [], diagnostics = [], listeners = new Map()
  class Recorder {
    state = "inactive"; mimeType = "audio/mp4"; stopCalls = 0
    constructor() { recorders.push(this) }
    start(timeslice) { this.state = "recording"; this.timeslice = timeslice }
    stop() {
      this.stopCalls++
      if (options.stopThrows) throw new Error("Recorder failed")
      this.state = "inactive"
      if (options.autoStop !== false) queueMicrotask(() => this.finish())
    }
    requestData() { this.data() }
    data(value = "encoded checkpoint") { this.ondataavailable?.({ data: new Blob([value], { type: this.mimeType }) }) }
    finish() { if (!options.emptyStop) this.data("encoded ending"); this.onstop?.() }
  }
  class Context {
    decodeAudioData(bytes, ready, fail) {
      options.decoded?.push(new TextDecoder().decode(bytes))
      if (options.decodePending) return new Promise(() => {})
      if (options.decodeFails) { const e = new Error("decode failed"); fail(e); return Promise.reject(e) }
      const audio = { duration: 1 }
      ready(audio); return Promise.resolve(audio)
    }
    close() { contextCloses++; return Promise.resolve() }
  }
  class Offline {
    createBufferSource() { return { connect() {}, start() {} } }
    startRendering() { return Promise.resolve({ getChannelData: () => new Float32Array(16000) }) }
  }
  const document = { visibilityState: "visible", createElement: () => new Element(),
    addEventListener: (key, callback) => listeners.set(key, callback), removeEventListener: key => listeners.delete(key) }
  const sandbox = { exports: {}, Blob, Uint8Array, Float32Array, DataView, ArrayBuffer, Date: { now: () => now },
    window: { isSecureContext: true, MediaRecorder: Recorder, AudioContext: Context, OfflineAudioContext: Offline,
      addEventListener: (key, callback) => listeners.set(key, callback), removeEventListener: key => listeners.delete(key) },
    MediaRecorder: Recorder, navigator: { mediaDevices: { getUserMedia: () => {
      opens++
      const track = { readyState: "live", stop() { this.readyState = "ended" } }; tracks.push(track)
      return options.permission ? options.permission({ getTracks: () => [track] }) : Promise.resolve({ getTracks: () => [track] })
    } } }, document,
    setTimeout: (callback, delay) => { const id = ++timerID; timers.set(id, { at: now + delay, callback }); return id },
    clearTimeout: id => timers.delete(id), setInterval: callback => { const id = ++timerID; intervals.set(id, callback); return id },
    clearInterval: id => intervals.delete(id), btoa: text => Buffer.from(text, "binary").toString("base64"),
    require(path) {
      if (path.endsWith("next-strings.js")) return { nextWord: key => english[`next.${key}`] }
      if (path.endsWith("voice-events.js")) return { recordVoiceDiagnostic: row => diagnostics.push(row) }
      if (path.endsWith("i18n.js")) return { T: new Proxy({}, { get: (_, key) => english[`legacy.${String(key)}`] ?? key }), fill: (text, values) => text.replace(/\{(\w+)\}/g, (_, key) => String(values[key])) }
      if (path.endsWith("env.js")) return { reduced: true }
      if (path.endsWith("failure-text.js")) return { failureSentence: (_, options) => options.sentence || options.fallback }
      if (path.endsWith("pixels.js")) return { drawSpinner() {}, setVoiceSpin() {}, spinPhase: 0 }
      if (path.endsWith("buzz.js")) return { done() {}, tap() {} }
      if (path.endsWith("compose-text.js")) return { appendGap() {}, appendedText() {} }
      if (path.endsWith("util.js")) return { uuid: () => "test-voice" }
      throw Error(path)
    },
  }
  runInNewContext(code, sandbox)
  const voice = sandbox.exports, box = new Element()
  const job = { host: box, composer: new Element(), guard: () => !box.isConnected, sink: text => words.push(text),
    transcribe: async (audio, rate) => { posts++; return options.transcribe ? options.transcribe(audio, rate) : { text: "Recovered speech" } } }
  const flush = () => new Promise(resolve => setImmediate(resolve))
  return { voice, box, words, recorders, tracks, diagnostics, options,
    get posts() { return posts }, get opens() { return opens }, get contextCloses() { return contextCloses },
    async start() { voice.press(job); await flush() },
    click(kind) { const button = box.querySelector(`.${kind}`); assert.ok(button, `${kind} button absent in ${voice.voiceState()}`); button.listeners.click() },
    async advance(ms) { now += ms; for (const [id, timer] of [...timers]) if (timer.at <= now) { timers.delete(id); timer.callback() }; for (const callback of intervals.values()) callback(); await flush() },
    hide() { document.visibilityState = "hidden"; listeners.get("visibilitychange")?.() }, flush,
    pagehide() { listeners.get("pagehide")?.() },
  }
}

test("Done closes microphone capture before a delayed browser stop notification", async () => {
  const h = harness({ autoStop: false }); await h.start(); h.recorders[0].data(); h.click("go")
  assert.equal(h.tracks[0].readyState, "ended")
  assert.equal(h.voice.voiceState(), "stopping"); assert.equal(h.posts, 0)
  h.recorders[0].finish(); await h.flush()
  assert.deepEqual(h.words, ["Recovered speech"]); assert.equal(h.posts, 1)
})

test("the normal final-data path still inserts words once and releases the microphone", async () => {
  const h = harness(); await h.start(); h.click("go"); await h.flush()
  assert.deepEqual(h.words, ["Recovered speech"]); assert.equal(h.voice.voiceState(), "off")
  assert.equal(h.tracks[0].readyState, "ended"); assert.equal(h.posts, 1)
})

test("missing stop recovers with ordered checkpoints and permits an explicit partial attempt", async () => {
  const decoded = [], h = harness({ autoStop: false, decoded }); await h.start()
  h.recorders[0].data("first"); h.recorders[0].data("second"); h.click("go")
  await h.advance(5000)
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.tracks[0].readyState, "ended")
  assert.match(h.box.textContent, /may be incomplete/); assert.equal(h.posts, 0)
  h.click("go"); await h.flush()
  assert.deepEqual(decoded, ["firstsecond"]); assert.equal(h.opens, 1); assert.equal(h.posts, 1)
})

test("zero delivered bytes stays recoverable without requesting a new microphone", async () => {
  const h = harness({ autoStop: false }); await h.start(); h.click("go"); await h.advance(5000)
  assert.match(h.box.textContent, /has not delivered/)
  h.click("go"); await h.advance(5000)
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.opens, 1); assert.equal(h.posts, 0)
})

test("a late final recording replaces the warning and can be retried without rerecording", async () => {
  const h = harness({ autoStop: false }); await h.start(); h.click("go"); await h.advance(5000)
  h.recorders[0].finish(); await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable"); assert.match(h.box.textContent, /ready to retry/)
  h.click("go"); await h.flush(); assert.equal(h.posts, 1); assert.equal(h.opens, 1)
})

test("failed host transcription retains the same recording for manual retry", async () => {
  let fail = true
  const h = harness({ transcribe: async () => { if (fail) throw Object.assign(new Error("offline"), { code: "offline" }); return { text: "Retry succeeded" } } })
  await h.start(); h.click("go"); await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable"); assert.match(h.box.textContent, /recording is kept/)
  fail = false; h.click("go"); await h.flush()
  assert.equal(h.posts, 2); assert.equal(h.opens, 1); assert.deepEqual(h.words, ["Retry succeeded"])
})

test("cancel processing and repeated Retry share a pending host request and fence late answers", async () => {
  let answer
  const h = harness({ transcribe: () => new Promise(resolve => { answer = resolve }) })
  await h.start(); h.click("go"); await h.flush(); assert.equal(h.posts, 1)
  h.click("drop"); assert.equal(h.voice.voiceState(), "recoverable")
  h.click("go"); await h.flush(); h.click("drop"); h.click("go"); await h.flush()
  assert.equal(h.posts, 1); answer({ text: "Only once" }); await h.flush()
  assert.deepEqual(h.words, ["Only once"])
})

test("a timed-out request remains recoverable and Retry does not create overlapping requests", async () => {
  const h = harness({ transcribe: () => new Promise(() => {}) })
  await h.start(); h.click("go"); await h.flush(); await h.advance(120000)
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.diagnostics.at(-1).code, "read_timeout")
  h.click("go"); await h.flush(); assert.equal(h.posts, 1)
})

test("a stalled decoder has a deadline and repeated Retry reuses its pending operation", async () => {
  const decoded = [], h = harness({ decodePending: true, decoded })
  await h.start(); h.click("go"); await h.flush(); await h.advance(15000)
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.diagnostics.at(-1).code, "prepare_timeout")
  h.click("go"); await h.flush(); assert.equal(decoded.length, 1); assert.equal(h.posts, 0)
})

test("late events from discarded recording cannot stop or contaminate its successor", async () => {
  const decoded = [], h = harness({ autoStop: false, decoded }); await h.start()
  const old = h.recorders[0], oldData = old.ondataavailable, oldStop = old.onstop
  h.click("go"); await h.advance(5000); h.click("drop"); await h.start()
  oldData({ data: new Blob(["foreign old audio"]) }); oldStop()
  assert.equal(h.voice.voiceState(), "recording"); assert.equal(h.tracks[1].readyState, "live")
  h.click("go"); h.recorders[1].finish(); await h.flush()
  assert.deepEqual(decoded, ["encoded ending"]); assert.equal(h.words.length, 1)
})

test("discarding a pending transcription drops its eventual result", async () => {
  let answer
  const h = harness({ transcribe: () => new Promise(resolve => { answer = resolve }) })
  await h.start(); h.click("go"); await h.flush(); h.click("drop"); h.click("drop")
  answer({ text: "Discarded" }); await h.flush()
  assert.equal(h.voice.voiceState(), "off"); assert.deepEqual(h.words, [])
})

test("backgrounding finishes capture and keeps the recorded speech", async () => {
  const h = harness(); await h.start(); h.hide(); assert.equal(h.tracks[0].readyState, "ended")
  await h.flush(); assert.deepEqual(h.words, ["Recovered speech"])
})

test("pagehide also stops capture if no visibility event was delivered", async () => {
  const h = harness(); await h.start(); h.pagehide(); assert.equal(h.tracks[0].readyState, "ended")
  await h.flush(); assert.deepEqual(h.words, ["Recovered speech"])
})

test("discarding a stalled decoder closes its audio context", async () => {
  const h = harness({ decodePending: true }); await h.start(); h.click("go"); await h.flush()
  assert.equal(h.contextCloses, 0)
  h.click("drop"); h.click("drop")
  assert.equal(h.contextCloses, 1); assert.equal(h.voice.voiceState(), "off")
})

test("recorder stop throwing cannot leave microphone capture running", async () => {
  const h = harness({ stopThrows: true }); await h.start(); h.recorders[0].data(); h.click("go")
  assert.equal(h.tracks[0].readyState, "ended"); await h.advance(5000)
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.posts, 0)
})

test("empty recognition can retry and is never inserted into the composer", async () => {
  let empty = true
  const h = harness({ transcribe: async () => ({ text: empty ? "" : "Actual speech" }) })
  await h.start(); h.click("go"); await h.flush(); assert.equal(h.voice.voiceState(), "recoverable")
  empty = false; h.click("go"); await h.flush(); assert.equal(h.posts, 2); assert.deepEqual(h.words, ["Actual speech"])
})

test("recording byte overflow stops capture, keeps checkpoints and names incomplete audio", async () => {
  const h = harness({ autoStop: false }); await h.start(); h.recorders[0].data("keep")
  h.recorders[0].data(new Uint8Array(16777216))
  assert.equal(h.tracks[0].readyState, "ended"); assert.equal(h.voice.voiceState(), "recoverable")
  assert.match(h.box.textContent, /storage is full/)
  assert.equal(h.diagnostics.at(-1).bytes, 4); assert.equal(h.diagnostics.at(-1).code, "recording_limit")
})

test("late final bytes fence a partial answer and a full retry waits for the older host request", async () => {
  let finishPartial, reads = 0
  const decoded = [], h = harness({ autoStop: false, decoded, transcribe: () => {
    reads++
    return reads === 1 ? new Promise(resolve => { finishPartial = resolve }) : Promise.resolve({ text: "Full recording" })
  } })
  await h.start(); h.recorders[0].data("checkpoint"); h.click("go"); await h.advance(5000)
  h.click("go"); await h.flush(); assert.equal(h.posts, 1)
  h.recorders[0].finish(); assert.equal(h.voice.voiceState(), "recoverable")
  h.click("go"); await h.flush(); assert.equal(h.posts, 1)
  finishPartial({ text: "Partial answer must be ignored" }); await h.flush()
  assert.deepEqual(h.words, ["Full recording"]); assert.equal(h.posts, 2)
  assert.deepEqual(decoded, ["checkpoint", "checkpointencoded ending"])
})

test("failed decoding retains the bytes and retries decoding on explicit request", async () => {
  const decoded = [], h = harness({ decodeFails: true, decoded }); await h.start(); h.click("go"); await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.posts, 0)
  h.options.decodeFails = false; h.click("go"); await h.flush()
  assert.deepEqual(decoded, ["encoded ending", "encoded ending"]); assert.equal(h.opens, 1)
  assert.deepEqual(h.words, ["Recovered speech"])
})

test("a finished recorder with no bytes explains the loss and offers only explicit discard", async () => {
  const h = harness({ emptyStop: true }); await h.start(); h.click("go"); await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable"); assert.equal(h.posts, 0)
  assert.match(h.box.textContent, /finished without delivering audio/)
  assert.match(h.box.textContent, /no audio to retry/)
  assert.equal(h.box.querySelector(".go"), null)
  h.click("drop"); assert.equal(h.voice.voiceState(), "off")
})
