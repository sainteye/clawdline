import { loadCopilotHelp } from './copilot-help-loader.js'
import { WorkObservations } from '../voice-work/observations.js'
import { onMachineChanged } from '../cloud/machine-switch.js'
import { voiceWork, receiveVoiceWork, useCallWorkTools } from '../voice-work/runtime.js'
import { WorkNarrator } from '../voice-work/narrator.js'
/**
 * Clawdline Voice Copilot - OpenAI Realtime API WebRTC Client.
 * Enables two-way voice conversation from mobile browser to control and inspect
 * Clawdline sessions on the Mac.
 */

import { CallMeter } from './copilot-meter.js'
import type { OrbLevels } from './copilot-orb.js'
import { COPILOT_TOOLS, actionNotice, microphoneProblem, toolTrace, type ToolTrace, NOTICE_KIND, unbackedClaims, unbackedClaimEvents, type ActionKind, adjustEvent, COPILOT_MACHINE_LACKS, copilotRefusal, DEFAULT_SPEED, greetingEvent, originGreetingEvent, runCopilotTool, sessionsSnapshot, sessionUpdateEvent, toolReplyEvents, type CallOptions, type CallOrigin, type ToolDeps } from './copilot-tools.js'
import { memorySection, parseMemory, remember, current, type MemoryTurn } from './copilot-memory.js'
import { addUsage, emptyCost, type CallCost } from './copilot-cost.js'
import { fleetOnThisPage, machinesOnThisPage, openDevStackOnThisPage, openOnThisPage, openSnippetsOnThisPage } from './copilot-open.js'
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
  /** The last thing the copilot did on the machine, shown for a moment (`actionNotice`). */
  notice?: { text: string; at: number; warn?: boolean }
  /** The tools used since the person last spoke, in order (`toolTrace`). */
  tools?: ToolTrace[]
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
  /** The last attempt's microphone was refused (NotAllowedError); `wake` may ask once more. */
  private micRefused = false
  /** How loud each side is, for the orb; measured here, never recorded. */
  private meter = new CallMeter()
  /** Stops the ringback that plays while the call connects (`CallMeter.ring`). */
  private stopRing: (() => void) | null = null
  /**
   * The person's current sentence as transcribed, for the Decision API; empty
   * from the moment they start speaking until its transcript arrives, so a
   * decision never judges the sentence before.
   */
  private heardNow = ''
  private releaseWork?: () => void
  private workReply = ''
  private narrator = new WorkNarrator(event => {
    if(this.dc?.readyState==='open')this.dc.send(JSON.stringify(event))
  },()=>consoleLanguage()||'zh-Hant')
  private workUnsubscribe?: () => void
  /** The user guide's sections, read the first time a call asks how something is used. */

  /** What the machine confirmed since the person last spoke (`unbackedClaims`). */
  private doneSinceHeard = new Set<ActionKind>()
  /** A correction is under way, so its own words are not checked again. */
  private correcting = false
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
      openSession: (id, machine) => openOnThisPage(id, machine),
      snippets: (id, request, machine) => openSnippetsOnThisPage(id, request, machine),
      fleet: () => fleetOnThisPage(),
      devstack: (place) => openDevStackOnThisPage(place),
      help: loadCopilotHelp,
      heard: () => this.heardNow,
      decide: (said, sessions) => decideOnMachine(said, sessions),
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
      // Over Cloud a pending briefing arrives as 200 with `pending`; it is still pending.
      if (body?.pending) {
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

  /**
   * Lines of this call for the machine's journal (`copilot_journal.go`),
   * handed over after each answer and when the call ends, so what a call
   * heard, said and did can be read on the machine.
   */
  private journalCall = ''
  private journalLines: { at: number; kind: string; text: string; tool?: Record<string, unknown> }[] = []

  private journal(kind: 'heard' | 'said' | 'tool' | 'error' | 'notice' | 'start' | 'end', text: string, tool?: Record<string, unknown>) {
    if (!this.journalCall) return
    this.journalLines.push({ at: Date.now(), kind, text, ...(tool ? { tool } : {}) })
  }

  private flushJournal() {
    if (!this.journalCall || this.journalLines.length === 0) return
    const entries = this.journalLines.splice(0, this.journalLines.length)
    void fetch('/v1/voice/copilot-journal', {
      method: 'POST', keepalive: true,
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({ call: this.journalCall, entries }),
    }).catch(() => undefined) // refusal-ok: the journal is a record for later; a lost turn costs the call nothing.
  }

  private addLog(level: 'info' | 'tool' | 'error', text: string) {
    if (level === 'error') this.journal('error', text)
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

  /**
   * Starts a call. With `origin` the call was placed from that Session, and its
   * first words hand that Session over instead of greeting. A call already
   * going (or connecting) is turned to the Session instead of starting again.
   */
  async start(origin?: CallOrigin): Promise<void> {
    if (this.state.status === 'connected' || this.state.status === 'connecting') {
      if (origin) this.turnTo(origin)
      return
    }
    this.micRefused = false

    // Before any await: iOS keeps an audio context made later in this tap suspended.
    const meterRefused = this.meter.open()
    // The call rings until it connects, fails or is hung up (`updateState`).
    this.stopRing = this.meter.ring()
    // A hang-up while it rings ends this attempt: `disconnect` moves `attempt`
    // on, and every step below checks it before the call goes any further.
    const attempt = ++this.attempt
    const own: { stream?: MediaStream; pc?: RTCPeerConnection } = {}
    const check = () => { if (attempt !== this.attempt) throw CALL_HUNG_UP }
    // The microphone is asked for in the tap itself, beside the call pass:
    // iOS Safari refused it (NotAllowedError) when it was asked only after
    // the pass came back over Cloud, seconds after the press.
    const microphone: Promise<MediaStream> = navigator.mediaDevices?.getUserMedia
      ? navigator.mediaDevices.getUserMedia({ audio: true })
      : Promise.reject(new DOMException('This browser offers no microphone.', 'NotSupportedError'))
    microphone.catch(() => undefined)

    this.memory = readMemory()
    // A fresh call: nothing of the last one's options, only where this one
    // was placed from, and that Session is the one under discussion.
    this.call = { memory: '', snapshot: '', options: { origin } }
    this.focus = origin?.id
    this.journalCall = crypto.randomUUID()
    this.journalLines = []
    // Which build and tools this call has: on 2026-11-13 a call could not be
    // told apart from one placed by a page loaded before the tool existed.
    this.journal('start', `${navigator.userAgent} · bundle ${document.querySelector('script[type="module"][src]')?.getAttribute('src') ?? '?'} · tools ${COPILOT_TOOLS.length}`)
    // Read the Sessions now, beside the microphone and WebRTC setup, so the
    // first question can be answered from them without a tool call.
    const workspaceSettings = fetch('/v1/settings/copilot').then(async res=>res.ok?await res.json():null).catch(()=>null)
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
        client_secret?: string; model?: string; eagerness?: string; voice?: string; speed?: string; brief?: boolean; extra_instructions?: string; briefing?: boolean; work_enabled?: boolean
      } | null
      if (!opened.ok || !grant?.client_secret) {
        const { code, message } = copilotRefusal(grant, opened.status)
        // refusal-ok: a missing key points to Settings, an older machine to its update; every other refusal is shown with its own code and the machine's sentence.
        throw new Error(code === 'copilot_key_missing'
          ? nextWord('copilotErrorKeyMissing')
          : COPILOT_MACHINE_LACKS.has(code) ? nextWord('copilotErrorMachineLacks', { code })
          : message ? nextWord('copilotErrorNoPassDetail', { code, message }) : nextWord('copilotErrorNoPass', { code }))
      }
      const managed = (await workspaceSettings)?.work_enabled === true
      check()
      this.call = { memory: '', snapshot: '', options: { workEnabled: managed, eagerness: grant.eagerness, voice: grant.voice, speed: grant.speed, brief: grant.brief, extra: grant.extra_instructions, origin: this.call.options.origin } }
      // The briefing takes the machine's assistant 8-15 s; the call does not wait for it.
      // The briefing is the deciding machine's own Sessions; on a page showing
      // several machines the call reads every machine's list instead.
      const fleet = await fleetOnThisPage().catch(() => null)
      const machines = new Set((fleet ?? []).map((f) => f.machine))
      this.briefing = !managed && grant.briefing && machines.size <= 1 ? this.pollBriefing() : Promise.resolve(null)
      void this.briefing.then((text) => { if (text) this.sendBriefing(text) })

      if(managed) {
        this.narrator.reset();this.narrator.setReady(false);this.workReply=voiceWork.state?.reply??''
        useCallWorkTools(this.toolDeps(),this.journalCall);this.releaseWork=voiceWork.activate()
        const observed=new WorkObservations(voiceWork.state)
        this.workUnsubscribe=voiceWork.subscribe(()=>{
          for(const evidence of observed.update(voiceWork.state))this.narrator.prepared(evidence.key,evidence.value)
          const reply=voiceWork.state?.reply
          if(reply&&reply!==this.workReply){this.workReply=reply;this.narrator.prepared('plan',JSON.stringify({planner_reply:reply,tasks:voiceWork.state?.tasks.filter(t=>t.status==='waiting_confirmation').map(t=>({text:t.text,status:t.status}))}))}
        })
      }
      check()
      // 1. The microphone asked for in the tap.
      own.stream = await microphone.catch((error: unknown) => {
        const name = error instanceof DOMException || error instanceof Error ? error.name : ''
        if (name === 'NotAllowedError') this.micRefused = true
        throw new Error(nextWord(microphoneProblem(name), { name: name || String(error) }))
      })
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
      // A call that did not start keeps no microphone, even one granted late.
      void microphone.then((stream) => { if (stream !== this.mediaStream) for (const track of stream.getTracks()) track.stop() }, () => undefined)
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
    const origin = this.call.options.origin
    if(this.call.options.workEnabled) {
      this.narrator.prepared('greeting',JSON.stringify({greeting:'A short hello. New requests will be saved in the voice assistant workspace.'}))
      this.narrator.setReady(true)
      if(origin) void receiveVoiceWork(`請交接 Session「${origin.label}」目前做完的事和需要我處理的問題。`,this.journalCall).catch(error=>this.addLog('error',String(error)))
    } else this.dc.send(JSON.stringify(origin ? originGreetingEvent(origin, consoleLanguage()) : greetingEvent(!!section, consoleLanguage())))
    this.addLog('info', origin ? nextWord('copilotLogFocus', { name: origin.label || origin.id }) : nextWord('copilotLogGreeting'))
  }

  /**
   * Turns a call to one Session. Before the channel opens, the focus is kept
   * for the configuration it is about to send; once open, the instructions
   * are resent with it and the copilot is asked for the handover now.
   */
  private turnTo(origin: CallOrigin) {
    this.call.options = { ...this.call.options, origin }
    this.focus = origin.id
    if (!this.dc || this.dc.readyState !== 'open') return
    this.dc.send(JSON.stringify(adjustEvent(this.call.memory, this.call.snapshot, this.call.options)))
    // Only a response in progress can be cancelled; cancelling none is an error event.
    if (this.state.speakingState === 'speaking' || this.state.speakingState === 'thinking') this.dc.send(JSON.stringify({ type: 'response.cancel' }))
    this.dc.send(JSON.stringify(originGreetingEvent(origin, consoleLanguage())))
    this.addLog('info', nextWord('copilotLogFocus', { name: origin.label || origin.id }))
  }


  // Both spellings of the output events are handled: GA names them
  // `response.output_audio*`, the beta `response.audio*`.
  private async handleRealtimeEvent(event: any) {
    if(this.call.options.workEnabled)this.narrator.event(event.type)
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
        // A new sentence has begun: the last one no longer says what is meant,
        // and what was done for it no longer backs what is said next.
        this.heardNow = ''
        this.doneSinceHeard.clear()
        this.updateState({ tools: [] })
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
        if(this.call.options.workEnabled)break
        this.updateState({ speakingState: 'listening' })
        break
      case 'output_audio_buffer.stopped':
      case 'output_audio_buffer.cleared':
        this.updateState({speakingState:'listening'})
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
          this.journal('heard', event.transcript)
          this.heardNow = event.transcript
          this.updateState({ lastTranscript: event.transcript })
          this.keep('user', event.transcript)
          if(this.call.options.workEnabled) {
            try {
              await receiveVoiceWork(event.transcript,this.journalCall,String(event.item_id||crypto.randomUUID()))
              this.narrator.prepared('received',JSON.stringify({received:event.transcript,meaning:'Saved. Planning and execution are still pending.'}))
            } catch(error) {
              this.narrator.prepared('receive-error',JSON.stringify({error:String(error),meaning:'The request was not confirmed saved. Ask the person to check the workspace.'}))
            } finally {this.narrator.received()}
          }
          this.addLog('info', nextWord('copilotLogHeard', { text: event.transcript }))
        }
        break
      case 'conversation.item.input_audio_transcription.failed':
        if(this.call.options.workEnabled){this.narrator.prepared('transcription-error',JSON.stringify({error:'Could not transcribe this utterance. It was not saved as a request.'}));this.narrator.received()}
        break
      case 'response.done': {
        const spoken = this.state.lastAssistantSpeech
        if (spoken) this.keep('assistant', spoken)
        if (spoken) this.journal('said', spoken)
        // Said it did something the machine never confirmed: say so on screen,
        // and have the copilot correct itself before the person relies on it.
        const unbacked = spoken ? unbackedClaims(spoken, this.doneSinceHeard) : []
        if (!this.call.options.workEnabled && unbacked.length && !this.correcting) {
          this.correcting = true
          this.updateState({ notice: { text: nextWord('copilotClaimNotDone'), at: Date.now(), warn: true } })
          this.addLog('error', nextWord('copilotClaimNotDone'))
          if (this.dc && this.dc.readyState === 'open') for (const e of unbackedClaimEvents(unbacked)) this.dc.send(JSON.stringify(e))
        } else {
          this.correcting = false
        }
        this.flushJournal()
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
    if(this.call.options.workEnabled)return
    this.updateState({ toolCallLog: nextWord('copilotRunningTool', { tool: event.name }), speakingState: 'thinking' })
    const startedAt = performance.now()
    const { output, logs } = await runCopilotTool(event.name, event.arguments, this.toolDeps())
    const trace = toolTrace(event.name, output, performance.now() - startedAt)
    this.updateState({ tools: [...(this.state.tools ?? []), trace] })
    this.journal('tool', event.name, { args: event.arguments.slice(0, 1000), outcome: trace.outcome, ms: trace.ms, target: trace.target ?? '', answer: output.slice(0, 1500) })
    for (const log of logs) this.addLog(log.level, log.text)
    this.updateState({ toolCallLog: undefined })
    // Only what the machine confirmed is shown and chimed, never what the
    // copilot says it did.
    const done = actionNotice(event.name, output)
    if (done) {
      this.doneSinceHeard.add(NOTICE_KIND[done.key])
      this.updateState({ notice: { text: nextWord(done.key, done.params), at: Date.now() } })
      this.journal('notice', nextWord(done.key, done.params))
      this.meter.chime()
    }

    if (this.dc && this.dc.readyState === 'open') {
      for (const message of toolReplyEvents(event.call_id, output)) this.dc.send(JSON.stringify(message))
    }
  }

  /**
   * Called inside a user activation that follows a call started from a timer
   * (the lift of a hold): iOS may have kept the call's audio suspended, or
   * refused its microphone, because the start was not itself a tap. Resumes
   * the audio, and asks for the microphone once more by starting again when
   * that refusal is what ended the attempt.
   */
  wake(origin?: CallOrigin) {
    this.meter.wake()
    void this.audioEl?.play().catch(() => undefined)
    if (this.state.status === 'error' && this.micRefused) {
      this.micRefused = false
      this.addLog('info', nextWord('copilotLogMicRetry'))
      void this.start(origin)
    }
  }

  /** How loud the person and the copilot are right now, each 0..1, for the orb. */
  readLevels(): OrbLevels {
    return this.meter.read()
  }

  disconnect() {
    this.releaseWork?.();this.releaseWork=undefined;this.workUnsubscribe?.();this.workUnsubscribe=undefined
    voiceWork.onPrepared=undefined;useCallWorkTools(undefined);this.narrator.reset()

    this.journal('end', this.state.status)
    this.flushJournal()
    this.journalCall = ''
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
onMachineChanged(()=>realtimeCopilot.disconnect())

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

/**
 * The Decision API on the machine that holds the key
 * (`internal/transport/http/copilot_decide.go`): which of its Sessions the
 * person's sentence means. A refusal is thrown; the tool then lets the copilot
 * choose from the list itself.
 */
async function decideOnMachine(said: string, sessions: unknown[]): Promise<{ id?: string; candidates?: string[] }> {
  // The call's own list goes with the sentence: on a page showing every
  // machine it is not the deciding machine's own.
  const res = await fetch('/v1/voice/copilot-decide', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ said, sessions }),
  })
  const body = (await res.json().catch(() => null)) as { id?: unknown; candidates?: unknown } | null
  // refusal-ok: a refused decision falls back to the copilot's own choice in `resolveRead`, which is said in the tool's answer.
  if (!res.ok || !body) throw new Error(copilotRefusal(body, res.status).code)
  return {
    ...(typeof body.id === 'string' && body.id ? { id: body.id } : {}),
    ...((body as { many?: unknown }).many === true ? { many: true } : {}),
    candidates: Array.isArray(body.candidates) ? body.candidates.filter((c): c is string => typeof c === 'string') : [],
  }
}
