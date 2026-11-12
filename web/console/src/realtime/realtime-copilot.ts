/**
 * Clawdline Voice Copilot - OpenAI Realtime API WebRTC Client.
 * Enables two-way voice conversation from mobile browser to control and inspect
 * Clawdline sessions on the Mac.
 */

import { CallMeter } from './copilot-meter.js'
import type { OrbLevels } from './copilot-orb.js'
import { adjustEvent, COPILOT_MACHINE_LACKS, copilotRefusal, DEFAULT_SPEED, greetingEvent, runCopilotTool, sessionsSnapshot, sessionUpdateEvent, toolReplyEvents, type CallOptions, type ToolDeps } from './copilot-tools.js'
import { memorySection, parseMemory, remember, current, type MemoryTurn } from './copilot-memory.js'
import { addUsage, emptyCost, type CallCost } from './copilot-cost.js'
import { machinesOnThisPage, openOnThisPage } from './copilot-open.js'
import { nextWord } from '../next-strings.js'

export interface CopilotLogItem {
  id: string
  timestamp: string
  level: 'info' | 'tool' | 'error'
  text: string
}

export interface RealtimeCopilotState {
  status: 'disconnected' | 'connecting' | 'connected' | 'error'
  speakingState: 'idle' | 'listening' | 'thinking' | 'speaking'
  lastTranscript?: string
  lastAssistantSpeech?: string
  error?: string
  toolCallLog?: string
  logs: CopilotLogItem[]
  /** This call's running cost, kept after it ends until the next one starts. */
  cost?: CallCost
  /** How many earlier turns this browser remembers (`copilot-memory.ts`). */
  remembered: number
}

export type RealtimeListener = (state: RealtimeCopilotState) => void

const API_KEY_STORAGE_KEY = 'clawdline.openai.apikey'
const MEMORY_STORAGE_KEY = 'clawdline.copilot.memory'
/** How long a call keeps asking the machine for its briefing. */
const BRIEFING_POLL_MS = 100000
/** How long the call's first words wait for the opening Session snapshot. */
const SNAPSHOT_WAIT_MS = 4000

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000)
}

function readMemory(): MemoryTurn[] {
  try {
    return current(parseMemory(localStorage.getItem(MEMORY_STORAGE_KEY)), nowSeconds())
  } catch {
    // refusal-ok: a browser that keeps no site data starts every call fresh.
    return []
  }
}

function writeMemory(turns: MemoryTurn[]): void {
  try {
    if (turns.length) localStorage.setItem(MEMORY_STORAGE_KEY, JSON.stringify(turns))
    else localStorage.removeItem(MEMORY_STORAGE_KEY)
  } catch {
    // refusal-ok: memory is a convenience; the call works without it.
  }
}

export function captureCopilotKey(scope: Pick<Window, 'location' | 'history'>): void {
  try {
    let key = ''
    if (scope.location.search) {
      const searchParams = new URLSearchParams(scope.location.search)
      key = searchParams.get('copilot_key') || searchParams.get('openai_key') || ''
      if (key) {
        searchParams.delete('copilot_key')
        searchParams.delete('openai_key')
        const newSearch = searchParams.toString() ? `?${searchParams.toString()}` : ''
        scope.history.replaceState(null, '', scope.location.pathname + newSearch + scope.location.hash)
      }
    }
    if (!key && scope.location.hash) {
      const hashParams = new URLSearchParams(scope.location.hash.replace(/^#/, ''))
      key = hashParams.get('copilot_key') || hashParams.get('openai_key') || ''
      if (key) {
        hashParams.delete('copilot_key')
        hashParams.delete('openai_key')
        const newHash = hashParams.toString() ? `#${hashParams.toString()}` : ''
        scope.history.replaceState(null, '', scope.location.pathname + scope.location.search + newHash)
      }
    }
    if (key.trim()) {
      localStorage.setItem(API_KEY_STORAGE_KEY, key.trim())
    }
  } catch {
    // refusal-ok: localStorage or location access errors in private browsing are ignored.
  }
}

export function getStoredOpenAIKey(): string {
  try {
    return localStorage.getItem(API_KEY_STORAGE_KEY) || ''
  } catch {
    // refusal-ok: localStorage disabled or restricted in private browsing defaults to empty string.
    return ''
  }
}

export function setStoredOpenAIKey(key: string): void {
  try {
    localStorage.setItem(API_KEY_STORAGE_KEY, key.trim())
  } catch {
    // refusal-ok: localStorage write failures in private browsing are ignored.
  }
}

if (typeof window !== 'undefined') {
  try {
    getStoredOpenAIKey()
    window.addEventListener('hashchange', () => {
      getStoredOpenAIKey()
    })
  } catch {
    // refusal-ok: window or storage restrictions in non-standard environments are ignored.
  }
}

/** The language the console is shown in, which the copilot speaks (`<html lang>`). */
function consoleLanguage(): string {
  return typeof document === 'undefined' ? '' : document.documentElement.lang || ''
}

/** Thrown inside `start` when the person hung up while the call rang. */
const CALL_HUNG_UP = new Error('hung up while ringing')

export class RealtimeCopilotClient {
  private pc: RTCPeerConnection | null = null
  private dc: RTCDataChannel | null = null
  private mediaStream: MediaStream | null = null
  private audioEl: HTMLAudioElement | null = null
  /** How loud each side is, for the orb; measured here, never recorded. */
  private meter = new CallMeter()
  /** Stops the ringback that plays while the call connects (`CallMeter.ring`). */
  private stopRing: (() => void) | null = null
  /** Which start this is; `disconnect` moves it on so a ringing start stops. */
  private attempt = 0
  private listeners: Set<RealtimeListener> = new Set()

  private state: RealtimeCopilotState = {
    status: 'disconnected',
    speakingState: 'idle',
    logs: [],
    remembered: readMemory().length,
  }
  private memory: MemoryTurn[] = readMemory()
  /** This call's opening Session snapshot, read while the connection is set up. */
  private snapshot: Promise<string> = Promise.resolve('')
  /** What this call's session.update says, kept so a later update can resend it whole. */
  private call: { memory: string; snapshot: string; options: CallOptions } = { memory: '', snapshot: '', options: {} }
  private briefing: Promise<string | null> = Promise.resolve(null)
  /** The Session this call is talking about (`ToolDeps.focus`); a new call starts with none. */
  private focus: string | undefined
  private toolDeps(): ToolDeps {
    return {
      fetch: (input, init) => fetch(input, init),
      sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
      briefing: () => this.briefing,
      openSession: (id) => openOnThisPage(id),
      machines: () => machinesOnThisPage(),
      focus: { get: () => this.focus, set: (id: string) => { this.focus = id } },
      delivery: () => ({ speed: this.call.options.speed ?? String(DEFAULT_SPEED), brief: this.call.options.brief !== false }),
      adjust: (change) => this.adjust(change),
    }
  }

  /** Changes this call's speed or answer length now, and saves it on the machine for the next calls. */
  private async adjust(change: { speed?: string; brief?: boolean }): Promise<{ saved: boolean }> {
    this.call.options = { ...this.call.options, ...change }
    if (this.dc && this.dc.readyState === 'open') {
      this.dc.send(JSON.stringify(adjustEvent(this.call.memory, this.call.snapshot, this.call.options)))
    }
    try {
      const res = await fetch('/v1/settings/copilot', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
        body: JSON.stringify(change),
      })
      return { saved: res.ok }
    } catch {
      // refusal-ok: the change applies to this call either way; the copilot says it was not kept for the next one.
      return { saved: false }
    }
  }

  /**
   * The machine's briefing for this call: asked once, then again while the
   * machine answers `pending`, until it is ready or BRIEFING_POLL_MS passes.
   */
  private async pollBriefing(): Promise<string | null> {
    const until = Date.now() + BRIEFING_POLL_MS
    while (Date.now() < until && this.state.status !== 'disconnected' && this.state.status !== 'error') {
      let res: Response
      try {
        res = await fetch('/v1/voice/copilot-briefing', { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: '{}' })
      } catch {
        // refusal-ok: the briefing is optional; an unreachable one leaves the call going and the copilot reads the Sessions itself.
        this.addLog('error', nextWord('copilotLogBriefingUnreachable'))
        return null
      }
      const body = (await res.json().catch(() => null)) as { briefing?: unknown; pending?: unknown; retry_after_ms?: unknown; error?: { code?: unknown } } | null
      if (res.status === 202 && body?.pending) {
        await new Promise((resolve) => setTimeout(resolve, Math.min(10000, Math.max(1000, Number(body.retry_after_ms) || 4000))))
        continue
      }
      if (res.ok && typeof body?.briefing === 'string' && body.briefing.trim()) return body.briefing
      const { code } = copilotRefusal(body, res.status)
      if (code !== 'copilot_briefing_off') this.addLog('error', nextWord('copilotLogBriefingFailed', { code }))
      return null
    }
    return null
  }

  /** Resends the whole configuration once the briefing has arrived. */
  private sendBriefing(text: string) {
    if (!this.dc || this.dc.readyState !== 'open') return
    this.call.options = { ...this.call.options, briefing: text }
    // Only the instructions and speed: the call has spoken by now, and its voice is fixed.
    this.dc.send(JSON.stringify(adjustEvent(this.call.memory, this.call.snapshot, this.call.options)))
    this.addLog('info', nextWord('copilotLogBriefingSent'))
  }
  private model = 'gpt-realtime-mini'

  subscribe(listener: RealtimeListener): () => void {
    this.listeners.add(listener)
    listener(this.state)
    return () => this.listeners.delete(listener)
  }

  getState(): RealtimeCopilotState {
    return this.state
  }

  private updateState(patch: Partial<RealtimeCopilotState>) {
    this.state = { ...this.state, ...patch }
    if (this.state.status !== 'connecting' && this.stopRing) {
      this.stopRing()
      this.stopRing = null
    }
    for (const listener of this.listeners) {
      listener(this.state)
    }
  }

  private addLog(level: 'info' | 'tool' | 'error', text: string) {
    const time = new Date().toLocaleTimeString(consoleLanguage() || undefined, { hour12: false })
    const item: CopilotLogItem = {
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
      timestamp: time,
      level,
      text,
    }
    const nextLogs = [item, ...this.state.logs].slice(0, 50)
    console.log(`[Clawdline Copilot ${level.toUpperCase()}] ${time} - ${text}`)
    this.updateState({ logs: nextLogs })
  }

  async start(): Promise<void> {
    if (this.state.status === 'connected' || this.state.status === 'connecting') {
      return
    }

    // Before any await: iOS keeps an audio context made later in this tap suspended.
    const meterRefused = this.meter.open()
    // The call rings until it connects, fails or is hung up (`updateState`).
    this.stopRing = this.meter.ring()
    // A hang-up while it rings ends this attempt: `disconnect` moves `attempt`
    // on, and every step below checks it before the call goes any further.
    const attempt = ++this.attempt
    const own: { stream?: MediaStream; pc?: RTCPeerConnection } = {}
    const check = () => { if (attempt !== this.attempt) throw CALL_HUNG_UP }

    this.memory = readMemory()
    this.focus = undefined
    // Read the Sessions now, beside the microphone and WebRTC setup, so the
    // first question can be answered from them without a tool call.
    this.snapshot = sessionsSnapshot(this.toolDeps()).catch(() => '')
    this.updateState({ status: 'connecting', error: undefined, toolCallLog: undefined, remembered: this.memory.length })
    this.addLog('info', nextWord('copilotLogStarting'))
    if (meterRefused) this.addLog('info', nextWord('copilotLogNoMeter', { reason: meterRefused }))

    try {
      // 0. The machine mints a short-lived client secret with its own key; the
      // key itself never reaches this browser (internal/transport/http/copilot.go).
      const opened = await fetch('/v1/voice/copilot-session', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: '{}',
      })
      check()
      const grant = (await opened.json().catch(() => null)) as {
        client_secret?: string; model?: string; eagerness?: string; voice?: string; speed?: string; brief?: boolean; extra_instructions?: string; briefing?: boolean
      } | null
      if (!opened.ok || !grant?.client_secret) {
        const { code, message } = copilotRefusal(grant, opened.status)
        // refusal-ok: a missing key points to Settings, an older machine to its update; every other refusal is shown with its own code and the machine's sentence.
        throw new Error(code === 'copilot_key_missing'
          ? nextWord('copilotErrorKeyMissing')
          : COPILOT_MACHINE_LACKS.has(code) ? nextWord('copilotErrorMachineLacks', { code })
          : message ? nextWord('copilotErrorNoPassDetail', { code, message }) : nextWord('copilotErrorNoPass', { code }))
      }
      this.call = { memory: '', snapshot: '', options: { eagerness: grant.eagerness, voice: grant.voice, speed: grant.speed, brief: grant.brief, extra: grant.extra_instructions } }
      // The briefing takes the machine's assistant 8-15 s; the call does not wait for it.
      this.briefing = grant.briefing ? this.pollBriefing() : Promise.resolve(null)
      void this.briefing.then((text) => { if (text) this.sendBriefing(text) })

      check()
      // 1. Get microphone audio stream
      own.stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      check()
      this.mediaStream = own.stream
      const inputRefused = meterRefused ? null : this.meter.attachInput(this.mediaStream)
      if (inputRefused) this.addLog('info', nextWord('copilotLogNoInputMeter', { reason: inputRefused }))

      // 2. Setup RTCPeerConnection
      own.pc = new RTCPeerConnection()
      this.pc = own.pc

      // 3. Audio output for assistant voice
      this.audioEl = document.createElement('audio')
      this.audioEl.autoplay = true
      this.pc.ontrack = (event) => {
        if (this.audioEl && event.streams[0]) {
          this.audioEl.srcObject = event.streams[0]
          const outputRefused = meterRefused ? null : this.meter.attachOutput(event.streams[0])
          if (outputRefused) this.addLog('info', nextWord('copilotLogNoOutputMeter', { reason: outputRefused }))
        }
      }

      // Add local audio track
      const audioTrack = this.mediaStream.getAudioTracks()[0]
      if (audioTrack) {
        this.pc.addTrack(audioTrack)
      }

      // 4. DataChannel for Realtime events
      this.dc = this.pc.createDataChannel('oai-events')
      this.setupDataChannel(this.dc)

      // 5. Create WebRTC Offer
      const offer = await own.pc.createOffer()
      check()
      await own.pc.setLocalDescription(offer)
      check()

      // 6. Connect with OpenAI Realtime API endpoint (GA)
      const baseUrl = 'https://api.openai.com/v1/realtime/calls'
      this.model = grant.model || 'gpt-realtime-mini'
      const response = await fetch(`${baseUrl}?model=${encodeURIComponent(this.model)}`, {
        method: 'POST',
        body: offer.sdp,
        headers: {
          Authorization: `Bearer ${grant.client_secret}`,
          'Content-Type': 'application/sdp',
        },
      })

      check()
      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(nextWord('copilotErrorRealtime', { status: response.status, detail: errorText }))
      }

      const answerSdp = await response.text()
      check()
      await own.pc.setRemoteDescription({ type: 'answer', sdp: answerSdp })
      check()

      this.updateState({ status: 'connected', speakingState: 'listening', cost: emptyCost(this.model) })
      this.addLog('info', nextWord('copilotLogConnected'))
    } catch (err) {
      if (err === CALL_HUNG_UP || attempt !== this.attempt) {
        // Hung up while it rang: `disconnect` already said so. Only what this
        // attempt itself opened is released; a newer call keeps its own.
        for (const track of own.stream?.getTracks() ?? []) track.stop()
        own.pc?.close()
        if (this.mediaStream === own.stream) this.mediaStream = null
        if (own.pc && this.pc === own.pc) { this.pc = null; this.dc = null }
        return
      }
      // refusal-ok: every error thrown above already names the machine's code and sentence; it is shown whole.
      this.disconnect()
      const msg = err instanceof Error ? err.message : String(err)
      this.updateState({ status: 'error', error: msg })
      this.addLog('error', nextWord('copilotLogConnectFailed', { message: msg }))
    }
  }

  private setupDataChannel(dc: RTCDataChannel) {
    dc.onopen = () => {
      void this.sendSessionConfiguration()
    }

    dc.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        this.handleRealtimeEvent(msg)
      } catch {
        // refusal-ok: unparseable server data channel payloads are ignored.
      }
    }

    dc.onerror = (err) => {
      console.error('DataChannel error:', err)
      this.updateState({ status: 'error', error: nextWord('copilotErrorChannel') })
    }

    dc.onclose = () => {
      if (this.state.status === 'connected') {
        this.disconnect()
      }
    }
  }

  private async sendSessionConfiguration() {
    if (!this.dc || this.dc.readyState !== 'open') return

    this.addLog('info', nextWord('copilotLogConfiguring'))
    // Waits for the snapshot at most SNAPSHOT_WAIT_MS: a slow machine must not hold the call's first words.
    const snapshot = await Promise.race([this.snapshot,
      new Promise<string>((resolve) => setTimeout(() => resolve(''), SNAPSHOT_WAIT_MS))])
    if (!this.dc || this.dc.readyState !== 'open') return
    this.addLog(snapshot ? 'info' : 'error', nextWord(snapshot ? 'copilotLogSnapshot' : 'copilotLogNoSnapshot'))
    const section = memorySection(this.memory, nowSeconds())
    if (section) this.addLog('info', nextWord('copilotLogMemory', { count: this.memory.length }))
    this.call = { ...this.call, memory: section, snapshot, options: { ...this.call.options, language: consoleLanguage() } }
    this.dc.send(JSON.stringify(sessionUpdateEvent(section, snapshot, this.call.options)))
    this.dc.send(JSON.stringify(greetingEvent(!!section, consoleLanguage())))
    this.addLog('info', nextWord('copilotLogGreeting'))
  }

  // Both spellings of the output events are handled: GA names them
  // `response.output_audio*`, the beta `response.audio*`.
  private async handleRealtimeEvent(event: any) {
    switch (event.type) {
      case 'session.updated':
        this.addLog('info', nextWord('copilotLogConfigured', { count: Array.isArray(event.session?.tools) ? event.session.tools.length : 0 }))
        break
      case 'error': {
        const msg = event.error?.message || event.error?.code || nextWord('copilotLogUnknownError')
        this.addLog('error', nextWord('copilotLogOpenAIError', { message: msg }))
        break
      }
      case 'input_audio_buffer.speech_started':
        this.updateState({ speakingState: 'listening' })
        break
      case 'input_audio_buffer.speech_stopped':
        this.updateState({ speakingState: 'thinking' })
        break
      case 'response.created':
        this.updateState({ lastAssistantSpeech: '' })
        break
      case 'response.output_audio.delta':
      case 'response.audio.delta':
        if (this.state.speakingState !== 'speaking') this.updateState({ speakingState: 'speaking' })
        break
      case 'response.output_audio.done':
      case 'response.audio.done':
        this.updateState({ speakingState: 'listening' })
        break
      case 'response.output_audio_transcript.delta':
      case 'response.audio_transcript.delta':
        if (event.delta) {
          const cur = this.state.lastAssistantSpeech || ''
          this.updateState({ lastAssistantSpeech: cur + event.delta })
        }
        break
      case 'conversation.item.input_audio_transcription.completed':
        if (event.transcript) {
          this.updateState({ lastTranscript: event.transcript })
          this.keep('user', event.transcript)
          this.addLog('info', nextWord('copilotLogHeard', { text: event.transcript }))
        }
        break
      case 'response.done': {
        const spoken = this.state.lastAssistantSpeech
        if (spoken) this.keep('assistant', spoken)
        if (event.response?.usage) {
          const cost = addUsage(this.state.cost ?? emptyCost(this.model), event.response.usage)
          this.updateState({ cost })
        }
        break
      }
      case 'response.function_call_arguments.done':
        await this.handleToolCall(event)
        break
    }
  }

  private keep(role: MemoryTurn['role'], text: string) {
    this.memory = remember(this.memory, role, text, nowSeconds())
    writeMemory(this.memory)
    this.updateState({ remembered: this.memory.length })
  }

  /** Forget every earlier call on this browser. */
  forgetMemory() {
    this.memory = []
    writeMemory([])
    this.updateState({ remembered: 0 })
    this.addLog('info', nextWord('copilotLogForgot'))
  }

  private async handleToolCall(event: { call_id: string; name: string; arguments: string }) {
    this.updateState({ toolCallLog: nextWord('copilotRunningTool', { tool: event.name }), speakingState: 'thinking' })
    const { output, logs } = await runCopilotTool(event.name, event.arguments, this.toolDeps())
    for (const log of logs) this.addLog(log.level, log.text)
    this.updateState({ toolCallLog: undefined })

    if (this.dc && this.dc.readyState === 'open') {
      for (const message of toolReplyEvents(event.call_id, output)) this.dc.send(JSON.stringify(message))
    }
  }

  /** How loud the person and the copilot are right now, each 0..1, for the orb. */
  readLevels(): OrbLevels {
    return this.meter.read()
  }

  disconnect() {
    this.attempt += 1
    if (this.dc) {
      try {
        this.dc.close()
      } catch {
        // refusal-ok: closing an already closed data channel is safe to ignore.
      }
      this.dc = null
    }

    if (this.pc) {
      try {
        this.pc.close()
      } catch {
        // refusal-ok: closing an already closed peer connection is safe to ignore.
      }
      this.pc = null
    }

    if (this.mediaStream) {
      for (const track of this.mediaStream.getTracks()) {
        try {
          track.stop()
        } catch {
          // refusal-ok: stopping tracks that are already ended is safe to ignore.
        }
      }
      this.mediaStream = null
    }

    if (this.audioEl) {
      this.audioEl.srcObject = null
      this.audioEl = null
    }
    this.meter.close()

    this.addLog('info', nextWord('copilotLogEnded'))

    this.updateState({
      status: 'disconnected',
      speakingState: 'idle',
      lastAssistantSpeech: undefined,
      lastTranscript: undefined,
      toolCallLog: undefined,
    })
  }
}

export const realtimeCopilot = new RealtimeCopilotClient()

/** The longest a voice preview stays connected. */
const PREVIEW_MAX_MS = 20000

/**
 * Says one short line in `voice`, as a call would: a receive-only Realtime
 * connection opened with a client secret the machine mints, closed once the
 * line has played. No microphone is asked for.
 */
export async function previewVoice(voice: string, line: string): Promise<{ usd: number }> {
  const opened = await fetch('/v1/voice/copilot-session', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: '{}',
  })
  const grant = (await opened.json().catch(() => null)) as { client_secret?: string; model?: string } | null
  if (!opened.ok || !grant?.client_secret) {
    // refusal-ok: the preview names the machine's refusal code to the person; there is no other action to take here.
    throw new Error(copilotRefusal(grant, opened.status).code)
  }
  const model = grant.model || 'gpt-realtime-mini'
  const pc = new RTCPeerConnection()
  const audio = document.createElement('audio')
  audio.autoplay = true
  pc.ontrack = (event) => { if (event.streams[0]) audio.srcObject = event.streams[0] }
  pc.addTransceiver('audio', { direction: 'recvonly' })
  const dc = pc.createDataChannel('oai-events')
  let usd = 0
  const done = new Promise<void>((resolve, reject) => {
    const limit = setTimeout(() => resolve(), PREVIEW_MAX_MS)
    dc.onopen = () => {
      dc.send(JSON.stringify({ type: 'session.update', session: { type: 'realtime', output_modalities: ['audio'],
        audio: { input: { turn_detection: null }, output: { voice } } } }))
      dc.send(JSON.stringify({ type: 'response.create', response: { output_modalities: ['audio'],
        instructions: `用自然的語氣，一字不差地說這一句，不要加任何其他話：「${line}」` } }))
    }
    dc.onmessage = (m) => {
      let e: { type?: string; response?: { usage?: unknown }; error?: { message?: string } }
      try { e = JSON.parse(String(m.data)) } catch { return }
      if (e.type === 'error') { clearTimeout(limit); reject(new Error(e.error?.message || 'error')) }
      if (e.type === 'response.done') {
        usd = addUsage(emptyCost(model), e.response?.usage).usd
        // The audio is still playing when the response is done; let it finish.
        setTimeout(() => { clearTimeout(limit); resolve() }, 4000)
      }
    }
  })
  try {
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    const res = await fetch(`https://api.openai.com/v1/realtime/calls?model=${encodeURIComponent(model)}`, {
      method: 'POST', body: offer.sdp, headers: { Authorization: `Bearer ${grant.client_secret}`, 'Content-Type': 'application/sdp' },
    })
    if (!res.ok) throw new Error(`OpenAI ${res.status}`)
    await pc.setRemoteDescription({ type: 'answer', sdp: await res.text() })
    await done
    return { usd }
  } finally {
    try { dc.close() } catch { /* refusal-ok: closing a preview's channel twice is harmless. */ }
    try { pc.close() } catch { /* refusal-ok: closing a preview's connection twice is harmless. */ }
    audio.srcObject = null
  }
}
