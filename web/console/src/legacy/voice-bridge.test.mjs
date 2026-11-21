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
const traditionalCode = readFileSync(new URL("../session/traditional-dictation.generated.ts", import.meta.url), "utf8")
const script = { exports: {} }
runInNewContext(ts.transpileModule(traditionalCode, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText, script)


class Element {
  children = []; dataset = {}; style = {}; listeners = {}; attributes = {}; isConnected = true
  scrollOffset = 0; clientHeight = 0
  get scrollHeight() { return Math.max(this.clientHeight, this.textContent.split("\n").length * 20) }
  get scrollTop() { return this.scrollOffset }
  set scrollTop(value) { this.scrollOffset = Math.min(value, this.scrollHeight - this.clientHeight) }
  set textContent(value) { this.text = value; this.children = [] }
  get textContent() { return (this.text ?? "") + this.children.map(child => child.textContent).join("") }
  appendChild(child) { this.children.push(child); return child }
  append(...children) { for (const child of children) this.appendChild(typeof child === "string" ? Object.assign(new Element(), { text: child }) : child) }
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
  const engineWrites = [], polishWrites = []
  let polish = options.polish ?? false
  let engine = options.engine ?? (options.live ? "openai_realtime" : null)
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
    resume() { return Promise.resolve() }
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
  const document = { visibilityState: "visible", documentElement: { lang: options.lang ?? "en" }, createElement: () => new Element(), createElementNS: () => new Element(),
    addEventListener: (key, callback) => listeners.set(key, callback), removeEventListener: key => listeners.delete(key) }
  const sandbox = { exports: {}, AudioWorkletNode: options.preview ? class {} : undefined, AbortController, Blob, Uint8Array, Float32Array, DataView, ArrayBuffer, Date: { now: () => now },
    window: { isSecureContext: true, MediaRecorder: Recorder, AudioContext: Context, OfflineAudioContext: Offline,
      addEventListener: (key, callback) => listeners.set(key, callback), removeEventListener: key => listeners.delete(key) },
    MediaRecorder: Recorder, navigator: { mediaDevices: { getUserMedia: () => {
      opens++
      const track = { readyState: "live", enabled: true, stop() { this.readyState = "ended" } }; tracks.push(track)
      const stream = { getTracks: () => [track], getAudioTracks: () => [track] }
      return options.permission ? options.permission(stream) : Promise.resolve(stream)
    } } }, document,
    setTimeout: (callback, delay) => { const id = ++timerID; timers.set(id, { at: now + delay, callback }); return id },
    clearTimeout: id => timers.delete(id), setInterval: callback => { const id = ++timerID; intervals.set(id, callback); return id },
    clearInterval: id => intervals.delete(id), btoa: text => Buffer.from(text, "binary").toString("base64"),
    require(path) {
      if (path.endsWith("next-strings.js")) return { nextWord: key => english[`next.${key}`] }
      if (path.endsWith("cloud/install.js")) return { captureMachineFetch: () => ({ current: () => true }) }
      if (path.endsWith("traditional-dictation.generated.js")) return script.exports
      if (path.endsWith("chime.js")) return { borrowAudioSession: options.borrowAudioSession ?? (() => () => {}) }
      if (path.endsWith("whisper-preview.js")) return { startWhisperPreview: options.preview }
      if (path.endsWith("live-dictation.js")) return {
        readDictationSettings: async () => engine ? { engine, polish } : null,
        writeDictationSetting: async (_, change) => {
          if ("voice_polish" in change) {
            polishWrites.push(change.voice_polish)
            if (options.writePolish && !await options.writePolish(change.voice_polish)) return false
            polish = change.voice_polish
            return true
          }
          engineWrites.push(change.voice_transcription)
          if (options.engineWriteFails) return false
          engine = change.voice_transcription
          return true
        },
        connectLiveDictation: options.live,
        LiveDictationError: options.liveError ?? Error,
      }
      if (path.endsWith("voice-events.js")) return { recordVoiceDiagnostic: row => diagnostics.push(row) }
      if (path.endsWith("i18n.js")) return { T: new Proxy({}, { get: (_, key) => english[`legacy.${String(key)}`] ?? key }), fill: (text, values) => text.replace(/\{(\w+)\}/g, (_, key) => String(values[key])) }
      if (path.endsWith("env.js")) return { reduced: true }
      if (path.endsWith("failure-text.js")) return { failureSentence: (_, options) => options.sentence || options.fallback }
      if (path.endsWith("pixels.js")) return { drawSpinner() {}, setVoiceSpin() {}, spinPhase: 0 }
      if (path.endsWith("buzz.js")) return { done() {}, tap() {} }
      if (path.endsWith("compose-text.js")) return { appendGap() {}, appendedText: (old, text) => [old, text].filter(Boolean).join(" ") }
      if (path.endsWith("util.js")) return { uuid: () => "test-voice" }
      throw Error(path)
    },
  }
  runInNewContext(code, sandbox)
  const voice = sandbox.exports, box = new Element()
  const sent = [], toasts = [], states = []
  const job = { pickEngine: options.pickEngine ?? Boolean(options.live ?? options.engine), host: box, composer: new Element(), guard: () => !box.isConnected, sink: text => words.push(text),
    send: text => sent.push(text), say: (text, bad) => toasts.push({ text, bad: !!bad }), changed: state => states.push(state),
    transcribe: async (audio, rate) => { posts++; return options.transcribe ? options.transcribe(audio, rate) : { text: "Recovered speech" } } }
  const flush = () => new Promise(resolve => setImmediate(resolve))
  return { voice, box, words, sent, toasts, states, job, recorders, tracks, diagnostics, options, engineWrites, polishWrites,
    get posts() { return posts }, get opens() { return opens }, get contextCloses() { return contextCloses },
    async start() { voice.press(job); await flush() },
    // While recording, the control that keeps the words for the box is the
    // microphone (Stop); the row's own right-hand button is Send.
    click(kind) { if (kind === "go" && voice.voiceState() === "recording") return voice.press(job); const button = box.querySelector(`.${kind}`); assert.ok(button, `${kind} button absent in ${voice.voiceState()}`); button.listeners.click() },
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

test("Realtime commits the audio before closing the microphone in the same stop action", async () => {
  const committed = []
  const h = harness({ live: async (_, stream) => ({
    finish() {
      committed.push(stream.getAudioTracks()[0].readyState)
      return Promise.resolve("Final Realtime draft")
    },
    close() {},
  }) })
  await h.start()
  h.click("go")
  assert.deepEqual(committed, ["live"])
  assert.equal(h.tracks[0].readyState, "ended")
  await h.flush()
  assert.deepEqual(h.words, ["Final Realtime draft"])
  assert.deepEqual(h.sent, [])
})

test("an empty Realtime commit keeps the original audio for an explicit Whisper retry", async () => {
  class EmptyAudio extends Error { code = "voice_realtime_audio_empty" }
  const decoded = []
  const h = harness({ liveError: EmptyAudio, decoded, live: async () => ({
    finish: async () => { throw new EmptyAudio() },
    close() {},
  }) })
  await h.start()
  h.click("go")
  await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable")
  assert.equal(h.diagnostics.at(-1).code, "realtime_audio_empty")
  assert.equal(h.posts, 0)
  h.click("go")
  await h.flush()
  assert.deepEqual(decoded, ["encoded ending"])
  assert.deepEqual(h.words, ["Recovered speech"])
  assert.deepEqual(h.sent, [])
  assert.equal(h.opens, 1)
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

test("the send control delivers the transcription instead of writing it into the box", async () => {
  const h = harness(); await h.start()
  h.voice.pressSend(h.job); await h.flush()
  assert.deepEqual(h.sent, ["Recovered speech"]); assert.deepEqual(h.words, [])
  assert.equal(h.voice.voiceState(), "off"); assert.equal(h.tracks[0].readyState, "ended"); assert.equal(h.posts, 1)
})

test("Stop writes the words into the box and sends nothing", async () => {
  const h = harness(); await h.start(); h.voice.press(h.job); await h.flush()
  assert.deepEqual(h.words, ["Recovered speech"]); assert.deepEqual(h.sent, [])
})

test("while recording, the row offers Cancel and Send, and Send delivers without the box", async () => {
  let prepared = 0
  const h = harness(); h.job.beforeSend = () => { prepared++ }; await h.start()
  assert.ok(h.box.querySelector(".drop")); assert.equal(h.box.querySelector(".go"), null)
  const send = h.box.querySelector(".go send-now")
  assert.ok(send); assert.match(send.textContent, /^Send$/)
  send.listeners.click(); await h.flush()
  assert.equal(prepared, 1); assert.deepEqual(h.sent, ["Recovered speech"]); assert.deepEqual(h.words, [])
})

test("a microphone that cannot send keeps Done on the row", async () => {
  const h = harness(); delete h.job.send; await h.start()
  assert.equal(h.box.querySelector(".go send-now"), null)
  assert.match(h.box.querySelector(".go").textContent, /Done/)
})

test("a sent recording keeps going after its composer leaves the page", async () => {
  let answer
  const h = harness({ transcribe: () => new Promise(resolve => { answer = resolve }) }); await h.start()
  h.voice.pressSend(h.job); await h.flush()
  assert.equal(h.voice.voiceState(), "reading"); assert.ok(h.voice.sending())
  assert.match(h.box.textContent, /as soon as it is transcribed/)
  h.box.isConnected = false
  await h.advance(1000)
  assert.equal(h.voice.voiceState(), "reading")
  answer({ text: "Sent while away" }); await h.flush()
  assert.deepEqual(h.sent, ["Sent while away"]); assert.deepEqual(h.words, []); assert.equal(h.voice.voiceState(), "off")
})

test("leaving the page while still recording, without sending, still discards it", async () => {
  const h = harness(); await h.start()
  h.box.isConnected = false
  await h.advance(1000)
  assert.equal(h.voice.voiceState(), "off"); assert.deepEqual(h.sent, []); assert.deepEqual(h.words, []); assert.equal(h.posts, 0)
})

test("a sent recording that fails after its composer left says so and frees the microphone", async () => {
  const h = harness({ transcribe: () => Promise.reject(Object.assign(new Error("host down"), { code: "unexpected_error" })) })
  await h.start(); h.box.isConnected = true
  h.voice.pressSend(h.job); h.box.isConnected = false; await h.flush()
  assert.equal(h.voice.voiceState(), "off"); assert.deepEqual(h.sent, [])
  assert.ok(h.toasts.some(t => t.bad && /was not sent/.test(t.text)))
})

test("a sent recording that fails while its composer is open stays retryable and still sends", async () => {
  let fail = true
  const h = harness({ transcribe: () => fail ? Promise.reject(new Error("host down")) : { text: "Second try" } }); await h.start()
  h.voice.pressSend(h.job); await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable")
  fail = false; h.click("go"); await h.flush()
  assert.deepEqual(h.sent, ["Second try"]); assert.deepEqual(h.words, [])
})


test("live dictation follows newly spoken lines and preserves a reader's scroll position", async () => {
  let update
  const h = harness({ live: async (_, __, ___, ____, onText) => {
    update = onText
    return { finish: async () => "Final draft", close() {} }
  } })
  await h.start()
  const preview = h.box.querySelector(".voice-live-preview")
  preview.clientHeight = 40
  const initial = "第一句\n第二句\n第三句\n第四句"
  update(initial)
  assert.equal(preview.scrollTop, preview.scrollHeight - preview.clientHeight)
  preview.scrollTop = 12
  const extended = initial + "\n第五句\n第六句"
  update(extended)
  assert.equal(preview.scrollTop, 12)
  assert.equal(preview.textContent, extended)
  preview.scrollTop = preview.scrollHeight - preview.clientHeight
  update(extended + "\n第七句")
  assert.equal(preview.scrollTop, preview.scrollHeight - preview.clientHeight)
})

test("dictation keeps the microphone in duplex and starts a Whisper preview even when settings are unavailable", async () => {
  let category = "auto", previews = 0
  const h = harness({
    pickEngine: true,
    borrowAudioSession: (type) => {
      category = type
      return () => { category = "auto" }
    },
    permission: async (stream) => {
      assert.equal(category, "play-and-record")
      return stream
    },
    preview: () => {
      previews++
      return { close() {} }
    },
  })
  await h.start()
  assert.equal(h.voice.voiceState(), "recording")
  assert.equal(previews, 1)
  assert.equal(category, "play-and-record")
  h.click("go")
  await h.flush()
  assert.equal(category, "auto")
  assert.deepEqual(h.words, ["Recovered speech"])
})

test("Whisper previews words without changing the draft, and ignores late preview after stopping", async () => {
  let preview, closes = 0
  const h = harness({ engine: "whisper", preview: (_, __, ___, ____, _____, onText) => {
    preview = onText
    return { close() { closes++ } }
  } })
  await h.start()
  preview("第一段")
  preview("第二段")
  assert.match(h.box.querySelector(".voice-live-preview").textContent, /第一段.*第二段/)
  assert.deepEqual(h.words, [])
  assert.equal(h.posts, 0)
  h.click("go")
  preview("不能覆蓋定稿")
  await h.flush()
  assert.deepEqual(h.words, ["Recovered speech"])
  assert.deepEqual(h.sent, [])
  assert.ok(closes >= 1)
  assert.equal(h.posts, 1)
})

test("the Whisper panel's small correction checkbox is saved before the stopped take is read", async () => {
  let acknowledge
  const h = harness({ engine: "whisper", writePolish: () => new Promise(resolve => { acknowledge = resolve }) })
  assert.equal(h.box.querySelector(".voice-polish-toggle"), null)
  await h.start()
  const checkbox = h.box.querySelector(".voice-polish-toggle").children[0]
  assert.equal(checkbox.type, "checkbox")
  assert.equal(checkbox.checked, false)
  checkbox.checked = true
  checkbox.listeners.change()
  assert.equal(h.voice.voiceState(), "recording")
  assert.deepEqual(h.polishWrites, [true])
  h.click("go")
  await h.flush()
  assert.equal(h.posts, 0)
  acknowledge(true)
  await h.flush()
  assert.equal(h.posts, 1)
  assert.deepEqual(h.words, ["Recovered speech"])
})

test("the expanded recording panel changes only the next recording's engine", async () => {
  let liveStops = 0
  const h = harness({ live: async () => ({
    finish() { liveStops += 1; return Promise.resolve("Current Realtime draft") },
    close() {},
  }) })
  assert.equal(h.box.querySelector(".voice-engine-picker"), null)
  await h.start()
  const picker = h.box.querySelector(".voice-engine-picker")
  assert.equal(h.box.querySelector(".voice-polish-toggle"), null)
  assert.equal(picker.value, "openai_realtime")
  picker.value = "whisper"
  picker.listeners.change()
  await h.flush()
  assert.deepEqual(h.engineWrites, ["whisper"])
  assert.equal(h.voice.voiceState(), "recording")
  assert.equal(liveStops, 0)
  h.click("go")
  await h.flush()
  assert.deepEqual(h.words, ["Current Realtime draft"])
  assert.equal(h.box.querySelector(".voice-engine-picker"), null)
  await h.start()
  assert.equal(h.box.querySelector(".voice-engine-picker").value, "whisper")
  h.click("go")
  await h.flush()
  assert.deepEqual(h.words, ["Current Realtime draft", "Recovered speech"])
})

test("a rejected engine change restores the saved preference without stopping recording", async () => {
  const h = harness({ engine: "whisper", engineWriteFails: true })
  await h.start()
  const picker = h.box.querySelector(".voice-engine-picker")
  picker.value = "openai_realtime"
  picker.listeners.change()
  await h.flush()
  assert.equal(picker.value, "whisper")
  assert.equal(h.voice.voiceState(), "recording")
  assert.equal(h.toasts.at(-1)?.bad, true)
  h.click("go")
  await h.flush()
})

test("Traditional Chinese displays converted Realtime preview and draft while preserving mixed text", async () => {
  const typed = "录音转写，OpenAI 2026。"
  for (const [lang, expected] of [["zh-Hant", "錄音轉寫，OpenAI 2026。"], ["zh-Hans", typed], ["en", typed]]) {
    let preview
    const h = harness({ lang, live: async (_, __, ___, ____, onText) => {
      preview = onText
      return { finish: async () => typed, close() {} }
    } })
    await h.start()
    preview(typed)
    assert.equal(h.box.querySelector(".voice-live-preview").textContent, expected)
    h.click("go")
    await h.flush()
    assert.deepEqual(h.words, [expected])
    assert.deepEqual(h.sent, [])
  }
})

test("Realtime keeps preview separate and puts the final words into the draft once", async () => {
  let preview, finish, closes = 0
  const h = harness({ live: async (_connection, stream, _request, _signal, onText) => {
    assert.equal(stream.getAudioTracks()[0].enabled, false)
    preview = onText
    return { finish: () => new Promise(resolve => { finish = resolve }), close: () => { closes++ } }
  } })
  await h.start()
  assert.equal(h.tracks[0].enabled, true)
  preview("Early words")
  assert.equal(h.box.querySelector(".voice-live-preview").textContent, "Early words")
  assert.deepEqual(h.words, [])
  h.click("go")
  assert.equal(h.tracks[0].readyState, "ended")
  await h.flush()
  finish("Final words")
  await h.flush()
  assert.deepEqual(h.words, ["Final words"])
  assert.deepEqual(h.sent, [])
  assert.equal(h.posts, 0)
  assert.equal(closes, 1)
})

test("Realtime failure retains audio for one explicit Whisper retry", async () => {
  const decoded = []
  const h = harness({ decoded, live: async () => ({
    finish: async () => { throw new Error("disconnected") },
    close() {},
  }) })
  await h.start()
  h.click("go")
  await h.flush()
  assert.equal(h.voice.voiceState(), "recoverable")
  assert.equal(h.box.querySelector(".go").textContent, "Retry with local Whisper")
  assert.equal(h.posts, 0)
  h.click("go")
  await h.flush()
  assert.deepEqual(decoded, ["encoded ending"])
  assert.deepEqual(h.words, ["Recovered speech"])
  assert.equal(h.opens, 1)
  assert.equal(h.posts, 1)
})

test("canceling Realtime setup closes the microphone and fences a late peer", async () => {
  let ready, signal, closes = 0
  const h = harness({ live: (_connection, _stream, _request, abort) => {
    signal = abort
    return new Promise(resolve => { ready = resolve })
  } })
  await h.start()
  assert.equal(h.voice.voiceState(), "connecting")
  h.click("drop")
  assert.equal(signal.aborted, true)
  assert.equal(h.tracks[0].readyState, "ended")
  ready({ finish: async () => "Late words", close: () => { closes++ } })
  await h.flush()
  assert.equal(closes, 1)
  assert.equal(h.recorders.length, 0)
  assert.deepEqual(h.words, [])
})
