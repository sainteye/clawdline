// Dictation's part of the bridge.
//
// `input/voice.js` is not copied, for the reason `view/composer.js` is not
// (see `waiting-bridge.ts`): its last two lines bind a click handler to
// `els.mic` at import, through `core/dom.js`, which looks every id up once
// before React has drawn any of them — and it reaches for `api.voice`,
// `renderComposer` and `appendMsg`, none of which exist on this side. So the
// recording path is owned here. The row retains the original markup while
// capture release, finalization and recoverable processing have independent
// lifetimes. `session/Composer.tsx` supplies the row, sink and mic attributes.
// If `input/voice.js` is ever copied, this goes and its exports are used.
//
// **The row is built here and not in JSX**, which is the original's
// arrangement rather than a shortcut: `show()` builds it once per state and
// then leaves it alone, because rebuilding it on every tick would take the
// buttons out from under a thumb four times a second. Only the count is
// written afterwards, into a text node already on screen, and the meter is a
// disc whose `transform` the compositor can change without laying the row out
// again.
//
// The one thing that had to be arranged differently is the toast and
// `renderComposer`: nothing under `legacy/` imports the app, so the host hands
// this module those two calls once (`attachVoice`).
import { nextWord } from "../next-strings.js"
import { recordVoiceDiagnostic, type VoiceDiagnostic } from "./voice-events.js"
import { T, fill } from "./js/core/i18n.js"
import { reduced } from "./js/core/env.js"
import { failureSentence } from "./js/core/failure-text.js"
import { drawSpinner, setVoiceSpin, spinPhase } from "./js/core/pixels.js"
import { done, tap } from "./js/core/buzz.js"
import { appendGap as appendGapOriginal, appendedText as appendedTextOriginal } from "./js/core/compose-text.js"
import { uuid } from "./js/core/util.js"

const S = T as Record<string, string>
const fillWords = fill as (s: string, holes: Record<string, unknown>) => string
const sentenceFor = failureSentence as (error: unknown, options: { sentence?: string; fallback: string }) => string
const paintSpin = drawSpinner as (canvas: HTMLCanvasElement, phase: number) => void
const holdSpin = setVoiceSpin as (canvas: HTMLCanvasElement | null) => void
const knock = tap as () => void
const knockTwice = done as () => void
const freshKey = uuid as () => string

/**
 * What goes on the end of what is already in the box (`core/compose-text.js`,
 * copied). One rule, because dictation is not the only thing that writes into
 * the composer: no gap in front of the first word, no second space where the
 * writer already put one, one space otherwise.
 */
export const appendGap = appendGapOriginal as (existing: string) => string
export const appendedText = appendedTextOriginal as (existing: string, addition: string) => string

const RATE = 16000
const MAX_SECONDS = 180
/// Under a quarter of a second is a mis-tap. The daemon drops these silently
/// for the same reason, and spinning up a 600MB model to read a button press
/// is worse than saying nothing happened.
const MIN_SECONDS = 0.25
/// When the count starts saying it is nearly out of room, rather than only
/// finding out.
const NEAR_SECONDS = 30
/// How long a transcription has to run before the row explains itself. The
/// first one after a reboot spends twelve seconds reading the model off disk
/// before it hears a word, and eight is comfortably past every ordinary one
/// and comfortably short of that.
const SLOW_SECONDS = 8
/// Where the meter's floor and ceiling sit, in dBFS. A phone held at arm's
/// length hears ordinary speech somewhere around -30 and a room that has gone
/// quiet around -55, and the numbers are picked so the first fills most of the
/// mark and the second none of it.
const QUIET_DB = -50
const LOUD_DB = -20
/// How wide the disc around the dot is drawn, at the two ends of what a room
/// can be, as multiples of the 26px square the stylesheet draws. **The dot in
/// the middle is a fixed size and stays one**: it says "this is listening",
/// which is a yes or a no rather than a quantity. Neither end reaches zero — a
/// quiet room draws a small disc rather than none, or "heard nothing" and "not
/// running" become the same picture.
const NARROW = 14 / 26
const WIDE = 1
/// How fast the dot follows what it hears, as time constants in milliseconds —
/// and there are two of them, which is the whole difference between a pulse
/// and a twitch. It rises almost as fast as the voice does and falls back over
/// about a quarter of a second: long enough to carry across the hole between
/// two syllables, short enough that the end of a sentence is visibly one. Time
/// constants and not per-frame fractions, because a phone that draws at 120Hz
/// would otherwise smooth twice as hard as one that draws at 60.
const ATTACK_MS = 45
const RELEASE_MS = 260

// Mirrored by capacity/voice_recording.go and checked against its register.
export const VOICE_RECORDING_BYTES = 16777216
export const VOICE_STOP_MS = 5000
export const VOICE_PREPARE_MS = 15000
export const VOICE_READ_MS = 120000

export type VoiceState = "off" | "opening" | "recording" | "stopping" | "preparing" | "reading" | "recoverable"

/** Where a recording goes, and who is holding the button that started it. */
export interface VoiceJob {
  /** The `.voice` row this draws into. */
  host: HTMLElement
  /**
   * The `form.composer` whose `data-voice` follows the state, or null for a
   * microphone that is not the composer's. `composer.css` keys the icon swap
   * off that attribute on `.composer` itself, not off the row.
   */
  composer: HTMLElement | null
  /** What the words are for, with what the optional cleanup did to them. Nothing on this path sends. */
  sink: (said: string, polish?: VoicePolishOutcome) => void
  /** A Cloud composer can use the account's existing voice host. Dictation never sends to a Session. */
  transcribe?: (audio: string, rate: number) => Promise<VoiceAnswer>
  /** The machine whose Whisper will receive this recording. Display-only. */
  source?: () => Promise<{ label: string; description: string }>
  /** This job's own reason to give up early — the composer gone, a sheet shut. */
  guard: () => boolean
  /** A non-composer microphone can own its own icon and messages. */
  changed?: (state: VoiceState) => void
  say?: (text: string, bad?: boolean) => void
  /** A missing local engine needs an installation choice, not a transient toast. */
  missingWhisper?: (failure: VoiceFailure) => void
}

/** What this module needs from the page it is drawn on. */
export interface VoiceHost {
  /** `core/util.js`'s toast, which lives in the app rather than in here. */
  say: (text: string, bad?: boolean) => void
  /** `renderComposer`: the state changed, so the buttons around it should. */
  changed: (state: VoiceState) => void
}

let host: VoiceHost = { say: () => {}, changed: () => {} }

/** Attach the page. Called once, by the component that owns the composer. */
export function attachVoice(next: VoiceHost): void {
  host = next
}

let state: VoiceState = "off"
let stream: MediaStream | null = null
let recorder: MediaRecorder | null = null
interface Recording {
  chunks: Blob[]
  bytes: number
  complete: boolean
  finished: boolean
  limited: boolean
  mime: string
  revision: number
  discarded?: boolean
  prepared?: { revision: number; promise: Promise<string> }
  closeDecoder?: () => void
  // A cancelled wait may still be executing on the host. Reuse that request
  // until it settles; manual Retry must not amplify a stalled transcription.
  reading?: { revision: number; promise: Promise<VoiceAnswer> }
}
let recording: Recording | null = null
let stageTimer: ReturnType<typeof setTimeout> | null = null
let attempt = 0
let recoveryCode: VoiceDiagnostic["code"] = "paused"
/** When the *current* count started, which is not when the recording began. */
let since = 0
let ticker: ReturnType<typeof setInterval> | null = null
/** The state the row was built for. */
let drawn = ""
/**
 * Which recording this is.
 *
 * Everything below is asynchronous — a permission prompt, a decode, a request
 * that can take twelve seconds — and Cancel has to be able to mean it while
 * any of them is outstanding. Rather than trying to abort each one, the number
 * moves on and every callback checks whether it still belongs to the recording
 * that is happening. A cancelled transcription still arrives; it simply
 * arrives for nobody.
 */
let token = 0
/**
 * The daemon's answer to "there is no Whisper here", kept for exactly one more
 * press. Without this, the second attempt costs another minute of talking to
 * be told the same thing; kept forever, somebody who installs Whisper while
 * the page is open could never use it.
 */
let whisperless: VoiceFailure | null = null
/** The quiet source label for this recording; its lookup never gates audio. */
let source: { label: string; description: string } | null = null
let sourceToken = 0

/* The meter. `heard` is an audio context of its own, held open only while the
   microphone is. `level` is where the disc currently is between its two ends,
   which is not what the microphone just said — see `paint`. */
let heard: AudioContext | null = null
let ears: AnalyserNode | null = null
// Allocated once rather than sixty times a second, and typed against its
// own buffer because `getByteTimeDomainData` will not take a view over a
// shared one.
let samples: Uint8Array<ArrayBuffer> | null = null
let pip: HTMLElement | null = null
let frame: number | null = null
let level = 0
let last = 0

/** The composer's own microphone. Set by `attachComposerVoice`. */
let defaultJob: VoiceJob | null = null
let activeJob: VoiceJob | null = null

/** The job a bare `press()` belongs to, as `DEFAULT_JOB` is in the original. */
export function attachComposerVoice(job: VoiceJob | null): void {
  defaultJob = job
  if (!job && state === "off") activeJob = null
}

/* ---- can this happen at all -------------------------------------------- */

/**
 * Why not, in the reader's own language, or "" for no reason at all.
 *
 * **Over plain http `navigator.mediaDevices` does not exist.** Not "fails",
 * not "asks and is refused" — the object is absent, so a page that only
 * checked for the function would report a browser too old to record when what
 * is actually wrong is the address it was opened from. That is a sentence
 * somebody can act on and the other one is not, so the protocol is asked about
 * first.
 */
function why(): string {
  const secure = window.isSecureContext !== false
  if (!secure) return S.webVoiceInsecure
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) return S.webVoiceUnsupported
  if (typeof window.MediaRecorder === "undefined") return S.webVoiceUnsupported
  if (!audioContextClass()) return S.webVoiceUnsupported
  if (!offlineContextClass()) return S.webVoiceUnsupported
  return ""
}

type AudioContextClass = typeof AudioContext
type OfflineContextClass = typeof OfflineAudioContext
const legacyAudio = window as unknown as {
  webkitAudioContext?: AudioContextClass
  webkitOfflineAudioContext?: OfflineContextClass
}
function audioContextClass(): AudioContextClass | undefined {
  return window.AudioContext || legacyAudio.webkitAudioContext
}
function offlineContextClass(): OfflineContextClass | undefined {
  return window.OfflineAudioContext || legacyAudio.webkitOfflineAudioContext
}

/// What the browser refused with. The names are the ones in the spec; the
/// older WebKit and Firefox spellings are there because a device with no
/// microphone at all is one of the four states this has to be able to tell
/// apart, and it is the one that predates the rename.
function refused(e: unknown): string {
  const name = (e as { name?: string } | null)?.name || ""
  if (name === "NotAllowedError" || name === "PermissionDeniedError" || name === "SecurityError") {
    return S.webVoiceDenied
  }
  if (name === "NotFoundError" || name === "DevicesNotFoundError") return S.webVoiceNoMic
  if (name === "NotReadableError" || name === "TrackStartError") return S.webVoiceInUse
  return S.webVoiceFailed
}

/** A failure carrying the daemon's code, as `net/fetch.js` hands one on. */
export type VoiceFailure = Error & { code?: string; reason?: string }

/// What the daemon refused with. `reason` is carried for this one route:
/// "install Whisper" and "Whisper is here but has no model" are two different
/// afternoons.
function complain(e: VoiceFailure | null): string {
  const code = e?.code
  let own = ""
  if (code === "busy") own = S.webVoiceBusy
  // refusal-ok: `no_whisper` carries exactly two reasons, so the other arm is the other one and not a catch-all
  else if (code === "no_whisper") own = e?.reason === "no_model" ? S.webVoiceNoModel : S.webVoiceNoBinary
  // `bad_request` from this route means the audio was not what the server
  // would take, and the audio was built here — so it is this page's fault and
  // this page's sentence, not a server message to pass along untranslated.
  else if (code === "bad_request") own = S.webVoiceFailed
  else if (code === "no_resampler") own = S.webVoiceUnsupported
  // A recording too short to send is not a failure of anything, so it is said
  // without a code; everything else — this page's own refusals included — says
  // its code and ref.
  if (code === "too_short") return S.webVoiceTooShort
  return sentenceFor(e, { sentence: own, fallback: S.webVoiceFailed })
}

/// An error with words somebody can read and a code this file can branch on.
/// The browser's own `DOMException` messages are English and are about codecs;
/// neither is any use here.
function fresh(said: string, code: string): VoiceFailure {
  const e: VoiceFailure = new Error(said)
  e.code = code
  return e
}

/* ---- the recording ------------------------------------------------------ */

/**
 * **One recorder, so one job.** A press that lands while another is already
 * recording does not queue behind it or start a second stream — it stops
 * whichever one is running, the same as pressing the same button twice always
 * has. Two microphones open at once was never a thing this page could do.
 */
export function press(job?: VoiceJob): void {
  const want = job || defaultJob
  if (!want) return
  if (state === "recording") {
    stop()
    return
  }
  // Mid-transcription the button is not the way out — the row's Cancel is,
  // and it says so.
  if (state !== "off") return
  activeJob = want
  source = null
  const lookup = ++sourceToken
  void want.source?.().then((answer) => {
    if (activeJob !== want || state === "off" || lookup !== sourceToken) return
    source = answer
    showSource()
  }).catch(() => { /* A missing label must never prevent dictation. */ })
  if (whisperless) {
    const failure = whisperless
    whisperless = null
    if (want.missingWhisper) want.missingWhisper(failure)
    else speak(complain(failure), true)
    return
  }
  const no = why()
  if (no) {
    speak(no, true)
    return
  }
  open()
}

function speak(text: string, bad?: boolean): void {
  ;(activeJob?.say ?? host.say)(text, bad)
}

function changed(): void {
  ;(activeJob?.changed ?? host.changed)(state)
}

/**
 * Ask for the microphone.
 *
 * Called straight out of the click, with nothing awaited in between:
 * `getUserMedia` is only allowed to prompt from inside a gesture, and a
 * permission prompt that never appears is indistinguishable from a button that
 * does nothing.
 */
function open(): void {
  const mine = ++token
  state = "opening"
  show()
  navigator.mediaDevices.getUserMedia({ audio: true }).then(
    (got) => {
      // Cancelled, or a second press, while the prompt was on screen. The
      // stream still arrives and it still has to be handed back, or the
      // recording light stays on.
      if (mine !== token) {
        silence(got)
        return
      }
      begin(got, mine)
    },
    (e) => {
      if (mine !== token) return
      state = "off"
      show()
      speak(refused(e), true)
    },
  )
}

function begin(got: MediaStream, mine: number): void {
  stream = got
  const take: Recording = { chunks: [], bytes: 0, complete: false, finished: false, limited: false, mime: "", revision: 0 }
  recording = take
  try {
    const current = new MediaRecorder(got)
    recorder = current
    current.ondataavailable = (ev) => {
      if (mine !== token || recording !== take || !ev.data?.size) return
      if (take.bytes + ev.data.size > VOICE_RECORDING_BYTES) {
        take.limited = true
        stop()
        recover("recording_limit")
        return
      }
      take.chunks.push(ev.data)
      take.bytes += ev.data.size
      take.mime = ev.data.type || current.mimeType || take.mime
      take.revision += 1
      if (state === "recoverable") { drawn = ""; show() }
    }
    current.onstop = () => landed(mine)
    // A recorder that falls over mid-sentence: whatever it managed to collect
    // is still worth reading, so this ends the same way the button does rather
    // than throwing the words away and complaining.
    current.onerror = () => {
      if (mine !== token || recording !== take) return
      recordVoiceDiagnostic({ stage: "stopping", code: "recorder_error", bytes: take.bytes, complete: take.complete })
      stop()
    }
    // Ask for periodic checkpoints, keeping all chunks in order. Individual
    // chunks need not be playable; a browser may delay them despite timeslice.
    current.start(1000)
  } catch {
    // A browser that has `MediaRecorder` and will not build one for this
    // stream. Rare, and reported rather than swallowed: the microphone was
    // granted, so nothing else on screen would explain why the count never
    // started.
    release()
    recording = null
    state = "off"
    show()
    // refusal-ok: a MediaRecorder the browser will not build throws a DOMException, which has no daemon code to name
    speak(S.webVoiceUnsupported, true)
    return
  }
  // Before the row is drawn, and still in the turn the recording started in.
  // See `listen`: the whole reason this is here is the gesture.
  listen(got)
  since = Date.now()
  state = "recording"
  show()
  // **The one moment somebody is not looking at the screen.** A press that
  // opens a microphone is followed by a person raising the phone and starting
  // to talk, and until something confirms it the honest reading of a screen
  // they cannot see is that the press missed. Best-effort by construction:
  // nothing here waits on it or reads its answer.
  knock()
  // Four times a second. The count is in whole seconds, and a one-second timer
  // drawing a one-second number lands visibly late about half the time.
  ticker = setInterval(() => beat(mine), 250)
  document.addEventListener("visibilitychange", backgrounded)
  window.addEventListener("pagehide", stop)
}

function backgrounded(): void {
  if (document.visibilityState === "hidden" && state === "recording") stop()
}

function beat(mine: number): void {
  if (mine !== token) return
  // Whatever this job's own reason to give up early is. Nothing is lost that
  // was not already only a moment old.
  if (activeJob?.guard()) {
    cancel()
    return
  }
  if (state === "recording" && elapsed() >= MAX_SECONDS) {
    speak(fillWords(S.webVoiceLimit, { n: Math.round(MAX_SECONDS / 60) }))
    stop()
    return
  }
  say()
}

function elapsed(): number {
  return since ? (Date.now() - since) / 1000 : 0
}

/**
 * Stop capture immediately, then wait for encoded final data.
 *
 * The count restarts here rather than carrying on, because it stops being the
 * same number: up to this point it was how long you have been talking, and
 * after it, it is how long the machine has been thinking. One counter that
 * meant both would mean neither.
 */
function stop(): void {
  if (state !== "recording") return
  state = "stopping"
  since = Date.now()
  deadline(VOICE_STOP_MS, "stop_timeout")
  try {
    if (recorder?.state !== "inactive") recorder?.stop()
  } catch {
    // A refusal is not evidence that final bytes have been delivered. Keep
    // the callbacks and collected chunks until the bounded recovery path.
  } finally {
    // Do not await onstop (or a redraw) to stop capturing. Keep data/stop
    // handlers attached: release() would throw away the final encoded bytes.
    stopCapture()
  }
  show()
}

function stopCapture(): void {
  silence(stream)
  stream = null
  deaf()
}

function clearDeadline(): void {
  if (stageTimer !== null) clearTimeout(stageTimer)
  stageTimer = null
}

function deadline(ms: number, code: VoiceDiagnostic["code"]): void {
  clearDeadline()
  const mine = token, operation = attempt
  stageTimer = setTimeout(() => {
    stageTimer = null
    if (mine === token && operation === attempt) recover(code)
  }, ms)
}

function recover(code: VoiceDiagnostic["code"]): void {
  if (!recording) return
  const stage = state === "stopping" || state === "preparing" || state === "reading" ? state : "recoverable"
  clearDeadline()
  attempt += 1
  stopCapture()
  recoveryCode = code
  state = "recoverable"
  drawn = ""
  recordVoiceDiagnostic({ stage, code, bytes: recording.bytes, complete: recording.complete })
  show()
}

/** The browser may still finish after the recovery row appeared. Keep it. */
function landed(mine: number): void {
  if (mine !== token || !recording) return
  recording.complete = !recording.limited
  recording.finished = true
  release()
  if (state === "preparing" || state === "reading") {
    // A partial attempt must not consume the recording just completed by
    // the browser. Fence its answer and offer the full bytes for retry.
    recover("stop_timeout")
    return
  }
  clearDeadline()
  if (state === "recoverable" || recording.limited) { drawn = ""; show(); return }
  processRecording()
}

function retry(): void {
  const take = recording
  if (!take || state !== "recoverable") return
  if (!take.bytes && !take.finished) {
    // There may be no checkpoint yet. Ask the existing recorder once more;
    // never open another microphone and overwrite what the person said.
    state = "stopping"
    since = Date.now()
    deadline(VOICE_STOP_MS, "stop_timeout")
    try {
      if (recorder && recorder.state !== "inactive") {
        recorder.requestData()
        recorder.stop()
      }
    } catch { /* the deadline offers recovery again */ }
    show()
    return
  }
  processRecording()
}

function processRecording(): void {
  const take = recording, job = activeJob
  if (!take) return
  clearDeadline()
  const mine = token, operation = ++attempt
  const current = () => mine === token && operation === attempt && recording === take
  state = "preparing"
  since = Date.now()
  deadline(VOICE_PREPARE_MS, "prepare_timeout")
  show()
  const revision = take.revision
  if (!take.prepared || take.prepared.revision !== revision) {
    take.closeDecoder?.()
    take.closeDecoder = undefined
    const promise = prepare(new Blob(take.chunks, { type: take.mime }), take)
    take.prepared = { revision, promise }
    void promise.catch(() => {
      if (take.prepared?.promise === promise) take.prepared = undefined
    })
  }
  take.prepared.promise.then((audio) => {
    if (!current()) return null
    clearDeadline()
    state = "reading"
    since = Date.now()
    deadline(VOICE_READ_MS, "read_timeout")
    show()
    if (!take.reading || take.reading.revision !== revision) {
      const prior = take.reading?.promise
      // If final bytes arrived during a partial attempt, wait for that host
      // request to settle before starting the manually requested full one.
      const request: Promise<VoiceAnswer> = Promise.resolve(prior).catch(() => null).then(() => {
        if (!current()) return {}
        return job?.transcribe ? job.transcribe(audio, RATE) : postVoice(audio, RATE)
      })
      take.reading = { revision, promise: request }
      void request.then((answer) => {
        if (!String(answer.text || "").trim() && take.reading?.promise === request) take.reading = undefined
      }, () => { if (take.reading?.promise === request) take.reading = undefined })
    }
    return take.reading.promise
  }).then((answer) => {
    if (!current() || !answer) return
    const said = String(answer.text || "").trim()
    // Empty recognition is retryable too: keep the original rather than
    // silently throwing away a recording that might contain speech.
    if (!said) { recover("processing_failed"); speak(S.webVoiceEmpty); return }
    cancel()
    knockTwice()
    job?.sink(said, polishOutcome(answer))
  }).catch((e: VoiceFailure) => {
    if (!current()) return
    recover("processing_failed")
    if (e?.code === "no_whisper" && job?.missingWhisper) {
      job.missingWhisper(e)
      return
    }
    speak(complain(e), !e || e.code !== "too_short")
  })
}

/// Back to nothing, with the microphone already handed back or never taken.
function quit(): void {
  if (ticker) clearInterval(ticker)
  ticker = null
  since = 0
  state = "off"
  show()
}

/**
 * Changed your mind.
 *
 * Offered while it is reading as well as while it is listening, because that
 * wait is the long one: the first transcription after a reboot is twelve
 * seconds, and somebody who has decided to type it instead should not have to
 * watch the rest of them go by. The request carries on at the daemon and its
 * answer is dropped when it lands — there is no way to recall a transcription
 * that is already running, and pretending otherwise would mean waiting for it
 * to confirm before the button appeared to work.
 */
export function cancel(): void {
  token += 1
  attempt += 1
  clearDeadline()
  if (recording) recording.discarded = true
  recording?.closeDecoder?.()
  recording = null
  if (recorder && recorder.state !== "inactive") {
    try {
      recorder.stop()
    } catch {
      /* it is going either way */
    }
  }
  release()
  quit()
}

function release(): void {
  document.removeEventListener("visibilitychange", backgrounded)
  window.removeEventListener("pagehide", stop)
  if (recorder) {
    recorder.ondataavailable = null
    recorder.onstop = null
    recorder.onerror = null
  }
  recorder = null
  stopCapture()
}

function silence(got: MediaStream | null): void {
  if (!got || !got.getTracks) return
  got.getTracks().forEach((track) => {
    try {
      track.stop()
    } catch {
      /* already gone */
    }
  })
}

/* ---- what the microphone is hearing ------------------------------------- */

/**
 * Listen to the stream that is being recorded, so the row can draw it.
 *
 * A mark that pulses on a clock says the page is doing something. It does not
 * say the microphone is hearing anything, and those are two different facts to
 * somebody holding a phone at arm's length in a room with other people talking
 * — the first question of a dictation that came back empty is always "was it
 * even picking me up". So the size is read off the stream itself, and a disc
 * that sits still means a quiet room rather than broken.
 *
 * **The context is opened here, in the same turn the recording starts.** iOS
 * gives a page that asks inside a gesture a context that is already running
 * and a page that asks afterwards one that is suspended — and a suspended
 * analyser reads silence, which is exactly the picture a microphone hearing
 * nothing would draw. The failure would look like an answer.
 *
 * Nothing is connected to the destination: an analyser is pulled by the graph
 * on its own, and a path to the speakers would be this page playing the room
 * back into the room.
 */
function listen(got: MediaStream): void {
  // Reduced motion is honoured by never building any of this. There is then
  // nothing to animate, and no audio session held open for the sake of
  // something nobody asked to see.
  if (reduced) return
  const Ctx = audioContextClass()
  if (!Ctx) return
  try {
    heard = new Ctx()
    // Belt and braces for the browser that hands one back suspended anyway.
    // The promise is not waited on: the meter starts a frame late and nothing
    // else notices.
    if (heard.state === "suspended" && heard.resume) void heard.resume()
    ears = heard.createAnalyser()
    ears.fftSize = 1024
    heard.createMediaStreamSource(got).connect(ears)
    samples = new Uint8Array(ears.fftSize)
  } catch {
    // All of this is decoration over a recording that is already running. A
    // browser that will not build the graph gets the still dot instead and
    // loses not one word.
    deaf()
  }
}

/**
 * One frame.
 *
 * **Read and drawn every frame, at whatever rate this screen runs.** The
 * analyser only ever holds the last twenty milliseconds or so, so anything
 * slower than the display would be throwing away most of what was said.
 * `requestAnimationFrame` rather than a timer because a page in the background
 * stops being given frames, and a meter still measuring a room nobody can see
 * is a phone warming in a pocket for nothing.
 */
function paint(): void {
  frame = requestAnimationFrame(paint)
  if (!ears || !pip || !samples) return
  ears.getByteTimeDomainData(samples)
  let sum = 0
  for (let i = 0; i < samples.length; i += 1) {
    // 128 is silence in the byte form; what is wanted is the distance from it.
    const d = (samples[i] - 128) / 128
    sum += d * d
  }
  const want = loud(Math.sqrt(sum / samples.length))
  const now = clock()
  let step = last ? now - last : 16
  last = now
  // A tab that has been in the background comes back holding a gap of seconds,
  // and an exponential handed that lands exactly on the target in one frame —
  // which is a jump.
  if (step > 100) step = 100
  const tau = want > level ? ATTACK_MS : RELEASE_MS
  level += (want - level) * (1 - Math.exp(-step / tau))
  swell()
}

/// The disc, as wide as `level` currently says.
///
/// **A transform, and deliberately nothing else.** It is the one property a
/// browser can hand to the compositor: changing it scales an already-painted
/// layer, without laying the row out again and without repainting a pixel of
/// it. The row is a flex line with buttons in it, so a mark that changed its
/// own box would push the count and the two ways out sideways on every frame.
function swell(): void {
  if (!pip) return
  pip.style.transform = "scale(" + (NARROW + level * (WIDE - NARROW)).toFixed(3) + ")"
}

/// A root-mean-square in [0, 1] as a place between the floor and the ceiling.
/// **In decibels**, because the linear number spends its entire life in the
/// bottom tenth — ordinary speech a phone's length away sits around 0.03 — and
/// a disc driven straight from it barely moves except for an occasional twitch.
function loud(rms: number): number {
  const db = 20 * Math.log10(rms || 1e-6)
  const t = (db - QUIET_DB) / (LOUD_DB - QUIET_DB)
  if (t < 0) return 0
  if (t > 1) return 1
  return t
}

/// A monotonic millisecond where there is one. `Date.now` steps when the clock
/// is set and would hand the smoothing a negative interval.
function clock(): number {
  return window.performance && performance.now ? performance.now() : Date.now()
}

/// Stop driving the disc, because the row it lives in is being rebuilt
/// underneath it. The microphone may still be open — this says nothing about
/// that.
function still(): void {
  if (frame !== null) cancelAnimationFrame(frame)
  frame = null
  pip = null
}

/// Stop driving it and give the audio session back.
function deaf(): void {
  still()
  ears = null
  samples = null
  level = 0
  last = 0
  const ctx = heard
  heard = null
  shut(ctx)
}

/* ---- turning a recording into something whisper.cpp can read ------------ */

function prepare(blob: Blob, take: Recording): Promise<string> {
  if (!blob.size) return Promise.reject(fresh(S.webVoiceTooShort, "too_short"))
  return bytesOf(blob)
    .then((buffer) => {
      if (take.discarded) throw fresh(S.webVoiceFailed, "cancelled")
      return decode(buffer, take)
    })
    .then((audio) => {
      if (take.discarded) throw fresh(S.webVoiceFailed, "cancelled")
      if (audio.duration < MIN_SECONDS) throw fresh(S.webVoiceTooShort, "too_short")
      return resample(audio)
    })
    .then((mono) => base64(pcm16(mono)))
}

/// `Blob.arrayBuffer` is the modern spelling and `FileReader` is the one every
/// browser that can record at all already had.
function bytesOf(blob: Blob): Promise<ArrayBuffer> {
  if (blob.arrayBuffer) return blob.arrayBuffer()
  return new Promise((ready, fail) => {
    const reader = new FileReader()
    reader.onload = () => ready(reader.result as ArrayBuffer)
    reader.onerror = () => fail(fresh(S.webVoiceFailed, "unreadable"))
    reader.readAsArrayBuffer(blob)
  })
}

/**
 * The bytes this browser just wrote, decoded by the same browser.
 *
 * Which is the entire trick: nobody has to know whether that was aac in an mp4
 * or opus in a webm, because the only thing being asked is "what did you
 * record", and it is being asked of the recorder.
 *
 * Both spellings of `decodeAudioData` are wired. Safari answered it with
 * callbacks for years before it answered with a promise, and the callback form
 * returns `undefined` — so a page that only used the promise would hang on it
 * forever with a counter running.
 */
function decode(buffer: ArrayBuffer, take: Recording): Promise<AudioBuffer> {
  const Ctx = audioContextClass()
  let ctx: AudioContext
  try {
    ctx = new Ctx!()
  } catch {
    return Promise.reject(fresh(S.webVoiceFailed, "no_audio"))
  }
  const close = () => shut(ctx)
  take.closeDecoder = close
  return new Promise<AudioBuffer>((ready, fail) => {
    let maybe: Promise<AudioBuffer> | undefined
    try {
      maybe = ctx.decodeAudioData(buffer, ready, fail)
    } catch (e) {
      fail(e)
      return
    }
    if (maybe && typeof maybe.then === "function") maybe.then(ready, fail)
  }).then(
    (audio) => {
      if (take.closeDecoder === close) take.closeDecoder = undefined
      shut(ctx)
      return audio
    },
    () => {
      if (take.closeDecoder === close) take.closeDecoder = undefined
      shut(ctx)
      throw fresh(S.webVoiceFailed, "undecodable")
    },
  )
}

function shut(ctx: AudioContext | null): void {
  try {
    if (ctx && ctx.close) void ctx.close().catch(() => {})
  } catch {
    /* it was closing anyway */
  }
}

/**
 * Whatever rate the microphone ran at, at 16 kHz and in one channel.
 *
 * The graph does both jobs and neither is written out by hand: the destination
 * of a one-channel context downmixes a stereo source the way a browser
 * downmixes anything, and rendering into a 16 kHz context resamples with the
 * filter that comes with it. The alternative — reading every third sample — is
 * four lines and produces speech with an audible warble folded into it, and
 * worse, one that still transcribes, slightly wrong.
 */
function resample(audio: AudioBuffer): Promise<Float32Array> {
  const Off = offlineContextClass()
  const frames = Math.max(1, Math.ceil(audio.duration * RATE))
  let off: OfflineAudioContext
  try {
    off = new Off!(1, frames, RATE)
  } catch {
    return Promise.reject(fresh(S.webVoiceUnsupported, "no_resampler"))
  }
  const source = off.createBufferSource()
  source.buffer = audio
  source.connect(off.destination)
  source.start(0)
  return new Promise<AudioBuffer>((ready, fail) => {
    off.oncomplete = (ev) => ready(ev.renderedBuffer)
    let maybe: Promise<AudioBuffer> | undefined
    try {
      maybe = off.startRendering()
    } catch (e) {
      fail(e)
      return
    }
    if (maybe && typeof maybe.then === "function") maybe.then(ready, fail)
  }).then(
    (out) => out.getChannelData(0),
    () => {
      throw fresh(S.webVoiceFailed, "no_render")
    },
  )
}

/// Float32 in [-1, 1] to little-endian Int16. **Clamped before it is scaled**:
/// a sample can come back a hair outside the range after resampling, and
/// 1.0001 × 32767 wraps around to a large negative number — which is a click in
/// the audio at exactly the loudest moment.
function pcm16(input: Float32Array): Uint8Array {
  const out = new Uint8Array(input.length * 2)
  const view = new DataView(out.buffer)
  for (let i = 0; i < input.length; i += 1) {
    let s = input[i]
    s = s < -1 ? -1 : s > 1 ? 1 : s
    view.setInt16(i * 2, Math.round(s * 32767), true)
  }
  return out
}

/// Base64, a block at a time. `String.fromCharCode.apply(null, bytes)` puts
/// every byte on the argument stack, and three minutes of this is 5.7 million
/// of them — which is not slow, it is a `RangeError`. The blocks are joined
/// before `btoa` rather than encoded separately, because base64 is only
/// splittable on multiples of three and getting that subtly wrong produces a
/// body the server decodes into noise rather than one it refuses.
function base64(bytes: Uint8Array): string {
  const BLOCK = 0x8000
  const parts: string[] = []
  for (let i = 0; i < bytes.length; i += BLOCK) {
    parts.push(String.fromCharCode.apply(null, Array.from(bytes.subarray(i, i + BLOCK))))
  }
  return btoa(parts.join(""))
}

/* ---- the one request ---------------------------------------------------- */

/**
 * `net/live.js`'s `voice`: the samples up, the words back.
 *
 * **Not a session route, and not a send.** The machine transcribes and answers
 * with the text; what happens to it afterwards is the composer's business.
 * Nothing on this path can reach a terminal, which is what makes a dictation
 * that heard the wrong thing a typo rather than an incident.
 *
 * Its own `fetch` rather than the shared client, for the reason
 * `waiting-bridge.ts` has its own: the refusal carries `reason`, which the
 * generated refusal does not, and the client's ten-second timeout is shorter
 * than the first transcription after a reboot.
 *
 * One key per submitted request. Recovery shares an outstanding promise; a
 * fresh manual request is made only after the earlier one has settled.
 */
export async function postVoice(audio: string, rate: number): Promise<VoiceAnswer> {
  let res: Response
  try {
    res = await fetch("/v1/voice", {
      method: "POST",
      headers: { "Content-Type": "application/json", "Idempotency-Key": freshKey() },
      body: JSON.stringify({ audio, rate }),
    })
  } catch {
    const dead: VoiceFailure = new Error(S.webOffline)
    dead.code = "offline"
    throw dead
  }
  const text = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = text ? (JSON.parse(text) as Record<string, unknown>) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const raw = data?.error
    const envelope: { code?: string; message?: string; reason?: string } =
      typeof raw === "string"
        ? {
            code: raw,
            message: typeof data?.detail === "string" ? data.detail : raw,
            reason: typeof data?.reason === "string" ? data.reason : undefined,
          }
        : raw && typeof raw === "object"
          ? (raw as { code?: string; message?: string; reason?: string })
          : { code: "http_" + res.status, message: res.statusText || S.webRequestFailed }
    const failure: VoiceFailure = new Error(envelope.message || envelope.code)
    failure.code = envelope.code
    failure.reason = envelope.reason
    throw failure
  }
  if (!data) throw new Error(S.webNotJSON)
  return data as VoiceAnswer
}

/** What `/v1/voice` answers. `raw` and `polish` come only from a daemon with cleanup. */
export type VoiceAnswer = { text?: string; ms?: number; raw?: string; polish?: { status?: string } }

/** The cleanup's outcome as the composer shows it, or nothing when cleanup was off. */
export type VoicePolishOutcome = { status: string; raw?: string }

export function polishOutcome(answer: VoiceAnswer): VoicePolishOutcome | undefined {
  const status = typeof answer.polish?.status === "string" ? answer.polish.status : ""
  if (!status) return undefined
  const raw = typeof answer.raw === "string" && answer.raw.trim() ? answer.raw.trim() : undefined
  if (status === "applied" && !raw) return undefined
  return raw ? { status, raw } : { status }
}

/* ---- what it looks like ------------------------------------------------- */

/**
 * The row, built once per state and then left alone.
 *
 * Rebuilding it on every tick would take the buttons out from under a thumb
 * four times a second. So only the count is written after this, into a text
 * node that is already on screen — and the meter is a dot that is also already
 * on screen, around which a disc widens and narrows while the box either of
 * them occupies does not change at all.
 */
function show(): void {
  const job = activeJob
  if (!job) {
    changed()
    return
  }
  // **Only the composer's own microphone reads this.** `composer.css` keys the
  // icon swap off this attribute on `.composer` itself, not off the row the
  // way everything else here is job-relative. React never renders it, so this
  // is the only writer.
  if (job.composer) job.composer.dataset.voice = state

  const box = job.host
  // `opening` is the browser's own permission sheet, which is on top of the
  // page and says more than this row could. A row that appears for the length
  // of a prompt is a flicker.
  if (state === "off" || state === "opening") {
    if (drawn) {
      drawn = ""
      holdSpin(null)
      still()
      box.textContent = ""
      box.hidden = true
      box.removeAttribute("data-near")
      box.removeAttribute("data-slow")
    }
    changed()
    return
  }
  if (drawn !== state) {
    // Whatever the last state was drawing into, it is about to stop existing.
    still()
    drawn = state
    box.textContent = ""
    box.removeAttribute("data-near")
    box.removeAttribute("data-slow")
    if (state !== "recording" && state !== "recoverable") {
      // The same turning mark the session list draws, on the same clock — this
      // page has one way of saying "something is happening over there".
      const spin = document.createElement("canvas")
      spin.className = "spin"
      box.appendChild(spin)
      holdSpin(spin)
      paintSpin(spin, spinPhase as number)
    } else if (state === "recoverable") {
      holdSpin(null)
    } else if (ears) {
      // The dot this row has always drawn, now with a disc around it that
      // answers to the room instead of to a clock. `data-live` is what takes
      // the CSS animation off the dot: two things writing to one element
      // fight, and the one that knows what is being heard should win. The disc
      // is an element of its own rather than a pseudo of the dot's, because
      // the property it lives on is written from here.
      //
      // Decoration, and only decoration: the seconds beside it are the
      // substance of this row and are what a screen reader is given.
      holdSpin(null)
      const meter = document.createElement("span")
      meter.className = "dot"
      meter.setAttribute("data-live", "1")
      const ring = document.createElement("span")
      ring.className = "disc"
      meter.appendChild(ring)
      box.appendChild(meter)
      pip = ring
      level = 0
      last = 0
      // Drawn once before the first frame, so the row opens on a disc at rest.
      swell()
      frame = requestAnimationFrame(paint)
    } else {
      // No analyser: reduced motion, or a browser that would not build the
      // graph. The same dot, left to the stylesheet.
      holdSpin(null)
      const dot = document.createElement("span")
      dot.className = "dot"
      box.appendChild(dot)
    }
    const what = document.createElement("span")
    what.className = "what"
    const said = document.createElement("span")
    said.className = "said"
    const note = document.createElement("span")
    note.className = "note"
    note.textContent = state === "recoverable" ? nextWord(recording?.bytes ? "voiceRecoveryKept"
      : recording?.finished ? "voiceRecoveryEmpty" : "voiceRecoveryNoBytes")
      : state === "reading" ? S.webVoiceSlow : nextWord("voiceMicStopped")
    what.appendChild(said)
    what.appendChild(note)
    box.appendChild(what)

    const sourceLabel = document.createElement("span")
    sourceLabel.className = "source"
    sourceLabel.hidden = true
    box.appendChild(sourceLabel)
    showSource()

    /**
     * The ways out, in the order this page puts every pair — the one that
     * undoes on the left, the one that goes on with it on the right.
     *
     * **Two of them while it is listening.** Ending a recording used to live
     * only on the microphone beside the box, which meant the row that says a
     * recording is happening was not the row you could end one from. The
     * microphone still stops it; it simply stopped being the only way, and
     * this is the one place somebody is already looking.
     *
     * Processing can be paused without discarding the audio. Recovery offers
     * separate retry and explicit discard controls.
     */
    const acts = document.createElement("div")
    acts.className = "acts"
    if (state === "recording") {
      acts.appendChild(way(S.webCancel, "drop", cancel))
      acts.appendChild(way(S.webVoiceDone, "go", stop))
    } else if (state === "recoverable") {
      acts.appendChild(way(nextWord("voiceDiscard"), "drop", cancel))
      if (recording?.bytes || !recording?.finished) acts.appendChild(way(recording?.complete ? nextWord("voiceRetry")
        : recording?.bytes ? nextWord("voiceTryPartial") : nextWord("voiceWaitForAudio"), "go", retry))
    } else {
      acts.appendChild(way(nextWord("voicePause"), "drop", () => recover("paused")))
    }
    box.appendChild(acts)
    box.hidden = false
  }
  say()
  changed()
}

/** Update the already-drawn source without rebuilding buttons under a thumb. */
function showSource(): void {
  const label = activeJob?.host.querySelector<HTMLElement>(".source")
  if (!label) return
  label.hidden = !source
  label.textContent = source?.label ?? ""
  label.title = source?.description ?? ""
  label.setAttribute("aria-label", source?.description ?? "")
}

/// One of them. The `mousedown` is the one Send and the attachment carry for
/// the same reason: pressing this must not close the keyboard of somebody who
/// was typing while they dictated.
///
/// **One word on the one that keeps the recording, and a sentence on the
/// microphone.** They are the same action and this used to say the same thing
/// twice — but "Stop and transcribe" is eighteen characters sitting next to
/// Cancel, describing a mechanism to somebody who has just stopped talking and
/// only wants to know which button keeps it.
function way(words: string, kind: string, go: () => void): HTMLButtonElement {
  const b = document.createElement("button")
  b.type = "button"
  b.className = kind
  b.textContent = words
  b.addEventListener("mousedown", (ev) => ev.preventDefault())
  b.addEventListener("click", go)
  return b
}

/// The count, and only the count.
function say(): void {
  const box = activeJob?.host
  const said = box?.querySelector(".said")
  if (!box || !said) return
  const n = Math.floor(elapsed())
  if (state === "recoverable") {
    const key = recoveryCode === "recording_limit" ? "voiceRecordingLimit"
      : recording?.finished && !recording.bytes ? "voiceNoAudioFinished"
      : !recording?.complete ? (recording?.bytes ? "voiceIncomplete" : "voiceNoAudioYet")
      : recoveryCode === "stop_timeout" ? "voiceRecoveredLate"
      : recoveryCode === "paused" ? "voicePaused" : "voiceRetryNeeded"
    said.textContent = nextWord(key)
    box.setAttribute("data-slow", "1")
    return
  }
  if (state === "stopping" || state === "preparing") {
    said.textContent = fillWords(nextWord(state === "stopping" ? "voiceStopping" : "voicePreparing"), { n })
    box.setAttribute("data-slow", "1")
    return
  }
  if (state === "reading") {
    said.textContent = fillWords(S.webVoiceReading, { n })
    const note = box.querySelector(".note")
    if (note) note.textContent = recording && !recording.complete ? nextWord("voicePartialWarning") : S.webVoiceSlow
    if (recording && !recording.complete) box.setAttribute("data-slow", "1")
    // Twelve seconds of nothing has an explanation and it is worth giving.
    // Anything shorter does not, and a permanent line of it would be an
    // apology for a wait that usually is not one.
    if (n >= SLOW_SECONDS) box.setAttribute("data-slow", "1")
    return
  }
  said.textContent = fillWords(S.webVoiceListening, { t: mmss(n) })
  // Running out of room, said before it happens rather than announced
  // afterwards.
  if (MAX_SECONDS - n <= NEAR_SECONDS) box.setAttribute("data-near", "1")
}

function mmss(n: number): string {
  const m = Math.floor(n / 60)
  const s = n % 60
  return m + ":" + (s < 10 ? "0" : "") + s
}

/**
 * Whether the microphone is open.
 *
 * The one reason the button stays alive when the rest of the composer has gone
 * dead — a session closing underneath a recording must not leave it with
 * nothing to stop it.
 */
export function live(): boolean {
  return state === "recording"
}

/** Part of a message that has not arrived yet, the way a picture still shrinking is. */
export function busy(): boolean {
  return state !== "off"
}

/** What the row is doing now, for a component drawing itself for the first time. */
export function voiceState(): VoiceState {
  return state
}
