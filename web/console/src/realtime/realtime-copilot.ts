/**
 * Clawdline Voice Copilot - OpenAI Realtime API WebRTC Client.
 * Enables two-way voice conversation from mobile browser to control and inspect
 * Clawdline sessions on the Mac.
 */

import { greetingEvent, runCopilotTool, sessionsSnapshot, sessionUpdateEvent, toolReplyEvents, type CallOptions, type ToolDeps } from './copilot-tools.js'
import { memorySection, parseMemory, remember, current, type MemoryTurn } from './copilot-memory.js'
import { addUsage, emptyCost, type CallCost } from './copilot-cost.js'

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

export class RealtimeCopilotClient {
  private pc: RTCPeerConnection | null = null
  private dc: RTCDataChannel | null = null
  private mediaStream: MediaStream | null = null
  private audioEl: HTMLAudioElement | null = null
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
  private toolDeps(): ToolDeps {
    return {
      fetch: (input, init) => fetch(input, init),
      sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
      briefing: () => this.briefing,
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
        this.addLog('error', '讀不到機器的匯報（連線失敗）')
        return null
      }
      const body = (await res.json().catch(() => null)) as { briefing?: unknown; pending?: unknown; retry_after_ms?: unknown; error?: { code?: unknown } } | null
      if (res.status === 202 && body?.pending) {
        await new Promise((resolve) => setTimeout(resolve, Math.min(10000, Math.max(1000, Number(body.retry_after_ms) || 4000))))
        continue
      }
      if (res.ok && typeof body?.briefing === 'string' && body.briefing.trim()) return body.briefing
      const code = typeof body?.error?.code === 'string' ? body.error.code : `HTTP ${res.status}`
      if (code !== 'copilot_briefing_off') this.addLog('error', `機器的匯報沒有整理成功（${code}），副駕駛會自己讀 Session`)
      return null
    }
    return null
  }

  /** Resends the whole configuration once the briefing has arrived. */
  private sendBriefing(text: string) {
    if (!this.dc || this.dc.readyState !== 'open') return
    this.call.options = { ...this.call.options, briefing: text }
    this.dc.send(JSON.stringify(sessionUpdateEvent(this.call.memory, this.call.snapshot, this.call.options)))
    this.addLog('info', '機器整理好的現況匯報已交給副駕駛')
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
    for (const listener of this.listeners) {
      listener(this.state)
    }
  }

  private addLog(level: 'info' | 'tool' | 'error', text: string) {
    const time = new Date().toLocaleTimeString('zh-TW', { hour12: false })
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

    this.memory = readMemory()
    // Read the Sessions now, beside the microphone and WebRTC setup, so the
    // first question can be answered from them without a tool call.
    this.snapshot = sessionsSnapshot(this.toolDeps()).catch(() => '')
    this.updateState({ status: 'connecting', error: undefined, toolCallLog: undefined, remembered: this.memory.length })
    this.addLog('info', '正在向機器要通話憑證，並請求麥克風權限...')

    try {
      // 0. The machine mints a short-lived client secret with its own key; the
      // key itself never reaches this browser (internal/transport/http/copilot.go).
      const opened = await fetch('/v1/voice/copilot-session', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: '{}',
      })
      const grant = (await opened.json().catch(() => null)) as {
        client_secret?: string; model?: string; eagerness?: string; extra_instructions?: string; briefing?: boolean
        error?: { code?: string; message?: string }
      } | null
      if (!opened.ok || !grant?.client_secret) {
        const code = grant?.error?.code ?? `HTTP ${opened.status}`
        // refusal-ok: only a missing key has an action here (Settings); every other refusal is shown with its own code and the machine's sentence.
        throw new Error(code === 'copilot_key_missing'
          ? '這台機器還沒設定 OpenAI API Key：請到「設定 › 語音副駕駛」填入。'
          : `機器沒有開出通話憑證（${code}）${grant?.error?.message ? '：' + grant.error.message : ''}`)
      }
      this.call = { memory: '', snapshot: '', options: { eagerness: grant.eagerness, extra: grant.extra_instructions } }
      // The briefing takes the machine's assistant 8-15 s; the call does not wait for it.
      this.briefing = grant.briefing ? this.pollBriefing() : Promise.resolve(null)
      void this.briefing.then((text) => { if (text) this.sendBriefing(text) })

      // 1. Get microphone audio stream
      this.mediaStream = await navigator.mediaDevices.getUserMedia({ audio: true })

      // 2. Setup RTCPeerConnection
      this.pc = new RTCPeerConnection()

      // 3. Audio output for assistant voice
      this.audioEl = document.createElement('audio')
      this.audioEl.autoplay = true
      this.pc.ontrack = (event) => {
        if (this.audioEl && event.streams[0]) {
          this.audioEl.srcObject = event.streams[0]
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
      const offer = await this.pc.createOffer()
      await this.pc.setLocalDescription(offer)

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

      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(`OpenAI Realtime 連線失敗 (${response.status}): ${errorText}`)
      }

      const answerSdp = await response.text()
      await this.pc.setRemoteDescription({ type: 'answer', sdp: answerSdp })

      this.updateState({ status: 'connected', speakingState: 'listening', cost: emptyCost(this.model) })
      this.addLog('info', 'WebRTC 連線建立成功，全雙工語音通話已就緒')
    } catch (err) {
      this.disconnect()
      const msg = err instanceof Error ? err.message : String(err)
      this.updateState({ status: 'error', error: msg })
      this.addLog('error', `連線發生錯誤: ${msg}`)
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
      this.updateState({ status: 'error', error: '語音資料通道發生錯誤' })
    }

    dc.onclose = () => {
      if (this.state.status === 'connected') {
        this.disconnect()
      }
    }
  }

  private async sendSessionConfiguration() {
    if (!this.dc || this.dc.readyState !== 'open') return

    this.addLog('info', '正在讀取 Session 快照並傳送副駕駛設定...')
    // Waits for the snapshot at most SNAPSHOT_WAIT_MS: a slow machine must not hold the call's first words.
    const snapshot = await Promise.race([this.snapshot,
      new Promise<string>((resolve) => setTimeout(() => resolve(''), SNAPSHOT_WAIT_MS))])
    if (!this.dc || this.dc.readyState !== 'open') return
    this.addLog(snapshot ? 'info' : 'error', snapshot ? '已附上 Session 快照，第一句就能直接問' : 'Session 快照沒有及時讀到，副駕駛會改用工具查詢')
    const section = memorySection(this.memory, nowSeconds())
    if (section) this.addLog('info', `已載入之前通話的 ${this.memory.length} 句對話`)
    this.call = { ...this.call, memory: section, snapshot }
    this.dc.send(JSON.stringify(sessionUpdateEvent(section, snapshot, this.call.options)))
    this.dc.send(JSON.stringify(greetingEvent(!!section)))
    this.addLog('info', '已送出開場問候要求')
  }

  // Both spellings of the output events are handled: GA names them
  // `response.output_audio*`, the beta `response.audio*`.
  private async handleRealtimeEvent(event: any) {
    switch (event.type) {
      case 'session.updated':
        this.addLog('info', `副駕駛配置已生效（${Array.isArray(event.session?.tools) ? event.session.tools.length : 0} 個工具）`)
        break
      case 'error': {
        const msg = event.error?.message || event.error?.code || '未知錯誤'
        this.addLog('error', `OpenAI 回報錯誤：${msg}`)
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
          this.addLog('info', `語音轉文字識別結果：「${event.transcript}」`)
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
    this.addLog('info', '已清除之前的通話紀錄')
  }

  private async handleToolCall(event: { call_id: string; name: string; arguments: string }) {
    this.updateState({ toolCallLog: `正在執行工具：${event.name}...`, speakingState: 'thinking' })
    const { output, logs } = await runCopilotTool(event.name, event.arguments, this.toolDeps())
    for (const log of logs) this.addLog(log.level, log.text)
    this.updateState({ toolCallLog: undefined })

    if (this.dc && this.dc.readyState === 'open') {
      for (const message of toolReplyEvents(event.call_id, output)) this.dc.send(JSON.stringify(message))
    }
  }

  disconnect() {
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

    this.addLog('info', '語音通話已結束並關閉媒體流')

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
