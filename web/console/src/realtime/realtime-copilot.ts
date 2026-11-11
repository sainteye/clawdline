/**
 * Clawdline Voice Copilot - OpenAI Realtime API WebRTC Client.
 * Enables two-way voice conversation from mobile browser to control and inspect
 * Clawdline sessions on the Mac.
 */

import { greetingEvent, runCopilotTool, sessionUpdateEvent, toolReplyEvents } from './copilot-tools.js'

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
}

export type RealtimeListener = (state: RealtimeCopilotState) => void

const API_KEY_STORAGE_KEY = 'clawdline.openai.apikey'

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
  }

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

  async start(apiKey: string): Promise<void> {
    if (this.state.status === 'connected' || this.state.status === 'connecting') {
      return
    }

    if (!apiKey.trim()) {
      this.updateState({ status: 'error', error: '請輸入有效的 OpenAI API Key' })
      return
    }

    setStoredOpenAIKey(apiKey)
    this.updateState({ status: 'connecting', error: undefined, toolCallLog: undefined })
    this.addLog('info', '正在請求麥克風權限並建立 WebRTC 連線...')

    try {
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
      const model = 'gpt-realtime-mini'
      let response = await fetch(`${baseUrl}?model=${model}`, {
        method: 'POST',
        body: offer.sdp,
        headers: {
          Authorization: `Bearer ${apiKey.trim()}`,
          'Content-Type': 'application/sdp',
        },
      })

      if (!response.ok && response.status === 404) {
        // Fallback to gpt-realtime if mini is unavailable
        response = await fetch(`${baseUrl}?model=gpt-realtime`, {
          method: 'POST',
          body: offer.sdp,
          headers: {
            Authorization: `Bearer ${apiKey.trim()}`,
            'Content-Type': 'application/sdp',
          },
        })
      }

      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(`OpenAI Realtime 連線失敗 (${response.status}): ${errorText}`)
      }

      const answerSdp = await response.text()
      await this.pc.setRemoteDescription({ type: 'answer', sdp: answerSdp })

      this.updateState({ status: 'connected', speakingState: 'listening' })
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
      this.sendSessionConfiguration()
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

  private sendSessionConfiguration() {
    if (!this.dc || this.dc.readyState !== 'open') return

    this.addLog('info', '正在向 OpenAI 傳送副駕駛系統配置與工具定義...')
    this.dc.send(JSON.stringify(sessionUpdateEvent()))
    this.dc.send(JSON.stringify(greetingEvent()))
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
          this.addLog('info', `語音轉文字識別結果：「${event.transcript}」`)
        }
        break
      case 'response.function_call_arguments.done':
        await this.handleToolCall(event)
        break
    }
  }

  private async handleToolCall(event: { call_id: string; name: string; arguments: string }) {
    this.updateState({ toolCallLog: `正在執行工具：${event.name}...`, speakingState: 'thinking' })
    const { output, logs } = await runCopilotTool(event.name, event.arguments, {
      fetch: (input, init) => fetch(input, init),
      sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    })
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
