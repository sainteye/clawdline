/** Production Chat and call audio adapter with scripted source facts and Web Audio tones. */
import { createRoot } from "react-dom/client"
import type * as W from "@clawdline/contract"
import { Chat } from "../../src/project-assistants/Chat"
import { contractVersion, fixtureSource } from "../../src/project-assistants/fixtures"
import { createViewState } from "../../src/project-assistants/view-state"
import { words } from "../../src/project-assistants/words"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"

const at = new Date().toISOString()
const received: W.ProjectAssistantSendRequest[] = []
const approved: W.ProjectAssistantPermissionAnswerRequest[] = []
const rows = [
  { source: fixtureSource("fixture-one"), botIndex: 0 },
  { source: fixtureSource("fixture-one"), botIndex: 2 },
  { source: fixtureSource("fixture-two"), botIndex: 0 },
].map(({source, botIndex}) => {
  const bot = source.bots[botIndex]
  return { source, bot }
})
const network = { online: true, holdSend: false, loseReceipt: false }
let releaseSend: (() => void) | undefined
const turnNames: string[] = []
const fenceOf = (bot: W.ProjectAssistantBot): W.ProjectAssistantFence => ({
  key: bot.key, project: bot.project, conversation_id: bot.conversation_id, generation: 1, binding_id: "fixture-binding",
})
function emit(row: typeof rows[number], event: Pick<W.ProjectAssistantEvent, "kind" | "turn" | "permission">) {
  row.source.push({ ...event, id: crypto.randomUUID(), seq: 0, at, fence: fenceOf(row.bot), version: contractVersion })
}
function turn(row: typeof rows[number], id: string, state: W.ProjectAssistantTurnState) {
  emit(row, { kind: "turn_updated", turn: {
    id, state, fence: fenceOf(row.bot), message_id: id + "-input", idempotency_key: "fixture",
    memory_revision: 1, instructions: row.bot.instructions,
  } })
}
let offer: W.ProjectAssistantPermissionOffer | undefined
const receipt = (command: W.ProjectAssistantCommand, resource_id: string): W.ProjectAssistantReceiptResponse => ({
  contract_version: contractVersion, receipt: { id: crypto.randomUUID(), command, event_seq: 0, state: "accepted", resource_id },
})
for (const row of rows) {
  row.source.send = async request => {
    if (request.fence.key.machine_id !== row.bot.key.machine_id || request.fence.key.bot_id !== row.bot.key.bot_id
      || request.fence.conversation_id !== row.bot.conversation_id) throw new Error("Fixture received a foreign write")
    received.push(request)
    const id = "voice-turn-" + received.length
    turnNames.push(id)
    row.source.scriptMessage(row.bot, id + "-input", "user", request.text, id)
    turn(row, id, "running")
    if (network.holdSend) await new Promise<void>(resolve => { releaseSend = resolve })
    if (network.loseReceipt) throw new Error("Synthetic acknowledgement lost")
    return receipt(request.command, id)
  }
  row.source.answer = async request => {
    if (offer?.id !== request.offer_id) throw new Error("Fixture received a foreign answer")
    approved.push(request)
    emit(row, { kind: "permission_answered", permission: { ...offer, selected_option_id: request.option_id, state: "answered" } })
    turn(row, offer.turn_id, "running")
    return receipt(request.command, offer.id)
  }
  Object.assign(row.source, { current: () => network.online })
}

const view = createViewState(window.localStorage)
const root = createRoot(document.getElementById("app")!)
let chosen = 0
let account = "fixture-account"
const current = () => network.online ? [rows[chosen].source] : []
const lang = new URL(location.href).searchParams.get("lang") ?? "en"
const signal = new AbortController().signal
async function select(index: number, nextAccount = account) {
  chosen = index
  account = nextAccount
  const { source, bot } = rows[index]
  const page = await source.list({ machine_id: bot.key.machine_id, cursor: "", query: "", page_size: 100 }, signal)
  const summary = page.items.find(item => item.bot.key.bot_id === bot.key.bot_id)!
  root.render(<div className="project-assistants" style={{height:"100dvh", background:"var(--pa-bg)"}}>
    <Chat key={JSON.stringify([account, index])} {...{ summary, source, current, view }} accountKey={account} readerID="fixture"
      w={words(lang)} language={lang} onBack={()=>{}} onBotChanged={()=>{}}/>
  </div>)
}

const audio = { opens: 0, defer: false, track: undefined as MediaStreamTrack | undefined, captures: [] as number[], plays: [] as string[], energy: [] as number[], cancelled: 0 }
let context: AudioContext | undefined
let grantMic: (() => void) | undefined
let recognizer: SpeechFixture | undefined
let staleResult: SpeechFixture["onresult"]
let mic: MediaStreamAudioDestinationNode | undefined
let playback: { utterance: SpeechSynthesisUtterance; oscillator: OscillatorNode } | undefined
function output() {
  context ??= new AudioContext()
  void context.resume()
  return context
}
Object.defineProperty(navigator.mediaDevices, "getUserMedia", { value: async () => {
  audio.opens++
  const ctx = output()
  if (audio.defer) await new Promise<void>(resolve => { grantMic = resolve })
  mic = ctx.createMediaStreamDestination()
  audio.track = mic.stream.getAudioTracks()[0]
  return new MediaStream([audio.track])
} })
class SpeechFixture {
  static async available(options: {langs: string[]; processLocally: boolean}) {
    if (!options.processLocally || !["en-US","zh-TW"].includes(options.langs[0])) throw new Error("Recognition left the device or used a foreign language")
    return "available"
  }
  get processLocally() { return this.local }
  set processLocally(value: boolean) { this.local = value }
  private local = false
  onresult?: (event: {resultIndex: number; results: { isFinal: boolean; 0: { transcript: string } }[]}) => void
  track?: MediaStreamTrack
  start(track: MediaStreamTrack) {
    if (!this.local || track !== audio.track || !track.enabled) throw new Error("Wrong capture lease")
    this.track = track
    recognizer = this
    staleResult = this.onresult
  }
  abort() { if (recognizer === this) recognizer = undefined }
}
Object.defineProperty(window, "SpeechRecognition", { value: SpeechFixture })
Object.defineProperty(window, "SpeechSynthesisUtterance", { value: class {
  constructor(readonly text: string) {}
} })
Object.defineProperty(window, "speechSynthesis", { value: {
  getVoices: () => [{ name: "Local test output", localService: true, lang: lang === "en" ? "en-US" : "zh-TW" }],
  speak(utterance: SpeechSynthesisUtterance) {
    audio.plays.push(utterance.text)
    const ctx = output(), oscillator = ctx.createOscillator(), analyser = ctx.createAnalyser()
    oscillator.frequency.value = 440
    oscillator.connect(analyser)
    analyser.connect(ctx.destination)
    oscillator.start()
    playback = { utterance, oscillator }
    setTimeout(() => {
      const values = new Float32Array(analyser.fftSize)
      analyser.getFloatTimeDomainData(values)
      audio.energy.push(Math.max(...values.map(Math.abs)))
    }, 75)
  },
  cancel() {
    audio.cancelled++
    playback?.oscillator.stop()
    playback = undefined
  },
} })

async function tone(frequency: number, text: string, late = false) {
  const result = {resultIndex: 0, results: [{isFinal: true, 0: {transcript: text}}]}
  if (late) { staleResult?.(result); return false }
  const run = recognizer
  const callback = run?.onresult
  if (!mic || !run?.track?.enabled || run.track.readyState !== "live") return false
  const ctx = output(), tone = ctx.createOscillator()
  tone.frequency.value = frequency
  tone.connect(mic)
  const analyser = ctx.createAnalyser()
  const input = ctx.createMediaStreamSource(new MediaStream([run.track]))
  input.connect(analyser)
  tone.start()
  await new Promise(resolve => setTimeout(resolve, 100))
  const values = new Float32Array(analyser.fftSize)
  analyser.getFloatTimeDomainData(values)
  const peak = Math.max(...values.map(Math.abs))
  audio.captures.push(peak)
  tone.stop()
  input.disconnect()
  if (peak > 0) callback?.(result)
  return peak > 0
}
Object.assign(window, { callFixture: {
  audio, received, approved, network, select, tone,
  complete(index: number, which: number, text: string) {
    const row = rows[index], id = turnNames[which]
    row.source.scriptMessage(row.bot, id + "-reply", "assistant", text, id)
    turn(row, id, "succeeded")
  },
  permission(which: number) {
    const row = rows[chosen], id = turnNames[which]
    turn(row, id, "waiting_permission")
    offer = {
      id: "fixture-call-permission", turn_id: id, fence: fenceOf(row.bot), native_request_id: "fixture-native-permission",
      state: "pending", expires_at: new Date(Date.now() + 120000).toISOString(), selected_option_id: "",
      options: [{ id: "allow", kind: "allow_once", label: "Allow once", allow: true, read_only: true }],
    }
    emit(row, { kind: "permission_offered", permission: offer })
  },
  finishAudio() {
    playback?.oscillator.stop()
    const end = playback?.utterance.onend
    playback = undefined
    end?.call(null as never, null as never)
  },
  grant() { audio.defer = false; grantMic?.(); grantMic = undefined },
  release() { network.holdSend = false; releaseSend?.(); releaseSend = undefined },
  holdPreflight() { rows[chosen].source.holdSnapshots() },
  preflightCount() { return rows[chosen].source.heldSnapshotSignals().length },
  releasePreflight() { rows[chosen].source.releaseSnapshot(0) },
  disconnect() { network.online = false; window.dispatchEvent(new Event("offline")) },
  reconnect() { network.online = true },
  trackStatus() { return { state: audio.track?.readyState, enabled: audio.track?.enabled } },
} })
await select(0)
