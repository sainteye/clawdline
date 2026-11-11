/**
 * Clawdline Voice Copilot - OpenAI Realtime API WebRTC Client.
 * Enables two-way voice conversation from mobile browser to control and inspect
 * Clawdline sessions on the Mac.
 */

export interface RealtimeCopilotState {
  status: 'disconnected' | 'connecting' | 'connected' | 'error'
  speakingState: 'idle' | 'listening' | 'thinking' | 'speaking'
  lastTranscript?: string
  lastAssistantSpeech?: string
  error?: string
  toolCallLog?: string
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
    } catch (err) {
      this.disconnect()
      const msg = err instanceof Error ? err.message : String(err)
      this.updateState({ status: 'error', error: msg })
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

    const sessionUpdate = {
      type: 'session.update',
      session: {
        modalities: ['text', 'audio'],
        voice: 'alloy',
        instructions: `你是 Clawdline 的隨身語音副駕駛（Voice Copilot）。
使用者的本機機器上正在運行 Clawdline 守護行程，而使用者目前正在用手機透過全雙工語音與你交談。
請一律使用繁體中文（台灣，zh-Hant-TW）回答。
你的主要功能：
1. 當使用者詢問「現在有哪些 session」、「目前有什麼任務在跑」時，呼叫 list_sessions 工具查看，並用語音條理分明、簡短地向使用者摘要。
2. 當使用者詢問某個特定 session 的細節或進度時，呼叫 get_session_screen 工具讀取該終端畫面，並將最新的進度或是否有錯誤做語音重點摘要。
3. 當使用者指示「幫我傳話給某個 session」或「發指令給某個任務」時，呼叫 send_message_to_session 工具轉交訊息。
回覆時請保持語氣自然、專業、簡潔，適合語音聆聽，避免唸出大量無意義的程式碼細節或雜湊值。`,
        tools: [
          {
            type: 'function',
            name: 'list_sessions',
            description: '查詢目前這台機器上 Clawdline 所有運作中或閒置的 Sessions 清單與狀態。',
            parameters: { type: 'object', properties: {} },
          },
          {
            type: 'function',
            name: 'get_session_screen',
            description: '讀取特定 Session 當前的終端機螢幕文字輸出，以供摘要其執行進度與狀態。',
            parameters: {
              type: 'object',
              properties: {
                session_id: {
                  type: 'string',
                  description: "目標 Session ID 或終端機標識，例如 '%1' 或 'owned:acp:...'",
                },
              },
              required: ['session_id'],
            },
          },
          {
            type: 'function',
            name: 'send_message_to_session',
            description: '將使用者的文字指令或訊息直接鍵入指定 Session 的終端機輸入框。',
            parameters: {
              type: 'object',
              properties: {
                session_id: {
                  type: 'string',
                  description: '目標 Session ID 或終端機 ID',
                },
                message: {
                  type: 'string',
                  description: '要傳送給該 Session 的文字內容',
                },
              },
              required: ['session_id', 'message'],
            },
          },
        ],
        tool_choice: 'auto',
      },
    }

    this.dc.send(JSON.stringify(sessionUpdate))

    // Send initial greeting request
    const greetingRequest = {
      type: 'response.create',
      response: {
        modalities: ['text', 'audio'],
        instructions:
          "請以親切、簡短的繁體中文向使用者問候：『你好！我是 Clawdline 語音副駕駛。需要我幫你查看目前這台機器上的 Sessions 進度，或是發送訊息給哪個任務嗎？』",
      },
    }
    this.dc.send(JSON.stringify(greetingRequest))
  }

  private async handleRealtimeEvent(event: any) {
    switch (event.type) {
      case 'input_audio_buffer.speech_started':
        this.updateState({ speakingState: 'listening' })
        break
      case 'input_audio_buffer.speech_stopped':
        this.updateState({ speakingState: 'thinking' })
        break
      case 'response.audio.delta':
        this.updateState({ speakingState: 'speaking' })
        break
      case 'response.audio.done':
        this.updateState({ speakingState: 'listening' })
        break
      case 'response.audio_transcript.delta':
        if (event.delta) {
          const cur = this.state.lastAssistantSpeech || ''
          this.updateState({ lastAssistantSpeech: cur + event.delta })
        }
        break
      case 'conversation.item.input_audio_transcription.completed':
        if (event.transcript) {
          this.updateState({ lastTranscript: event.transcript })
        }
        break
      case 'response.function_call_arguments.done':
        await this.handleToolCall(event)
        break
    }
  }

  private async handleToolCall(event: { call_id: string; name: string; arguments: string }) {
    const { call_id, name, arguments: argsJson } = event
    let args: any = {}
    try {
      args = JSON.parse(argsJson)
    } catch {
      // refusal-ok: malformed tool arguments JSON defaults to empty arguments.
      args = {}
    }

    this.updateState({
      toolCallLog: `正在執行工具：${name}...`,
      speakingState: 'thinking',
    })

    let resultOutput = ''
    try {
      if (name === 'list_sessions') {
        const res = await fetch('/v1/orchestrator/sessions')
        if (!res.ok) {
          resultOutput = JSON.stringify({ error: `查詢失敗 (HTTP ${res.status})` })
        } else {
          const data = await res.json()
          const cleanSessions = (data.sessions || []).map((s: any) => ({
            id: s.id,
            label: s.label,
            assistant: s.assistant,
            state: s.state,
            work_state: s.work_state,
            directory: s.cwd,
          }))
          resultOutput = JSON.stringify({ sessions: cleanSessions })
        }
      } else if (name === 'get_session_screen') {
        const sessionId = args.session_id
        const res = await fetch(`/v1/sessions/${encodeURIComponent(sessionId)}/screen`)
        if (!res.ok) {
          resultOutput = JSON.stringify({ error: `無法讀取螢幕 (HTTP ${res.status})` })
        } else {
          const data = await res.json()
          const screenText = data.screen?.text || ''
          // Extract last 40 lines to keep context tight
          const lines = screenText.split('\n')
          const recentLines = lines.slice(-40).join('\n')
          resultOutput = JSON.stringify({
            session_id: sessionId,
            recent_screen_text: recentLines,
          })
        }
      } else if (name === 'send_message_to_session') {
        const { session_id, message } = args
        const res = await fetch(`/v1/sessions/${encodeURIComponent(session_id)}/send`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text: message }),
        })
        if (!res.ok) {
          resultOutput = JSON.stringify({ error: `訊息送出失敗 (HTTP ${res.status})` })
        } else {
          resultOutput = JSON.stringify({ success: true, message: '訊息已成功送入該 Session 終端' })
        }
      } else {
        resultOutput = JSON.stringify({ error: `未知的工具：${name}` })
      }
    } catch (err) {
      resultOutput = JSON.stringify({ error: err instanceof Error ? err.message : String(err) })
    }

    this.updateState({ toolCallLog: undefined })

    // Send function call output back to OpenAI
    if (this.dc && this.dc.readyState === 'open') {
      const toolResponse = {
        type: 'conversation.item.create',
        item: {
          type: 'function_call_output',
          call_id,
          output: resultOutput,
        },
      }
      this.dc.send(JSON.stringify(toolResponse))

      // Trigger model response
      this.dc.send(JSON.stringify({ type: 'response.create' }))
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
