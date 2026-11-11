/**
 * Clawdline Voice Copilot - OpenAI Realtime API WebRTC Client.
 * Enables two-way voice conversation from mobile browser to control and inspect
 * Clawdline sessions on the Mac.
 */

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

    const sessionUpdate = {
      type: 'session.update',
      session: {
        modalities: ['text', 'audio'],
        voice: 'alloy',
        input_audio_transcription: {
          model: 'whisper-1',
        },
        instructions: `你是 Clawdline 的隨身全雙工語音副駕駛（Voice Copilot）。
使用者目前正透過手機語音與你直接對話。

【什麼是 Clawdline 與目前環境】
- Clawdline 是使用者的主機 AI 程式開發與多 Agent 編排守護行程（Local Agent Orchestrator），運行在使用者本機電腦上。
- 使用者在電腦上啟動了多個 AI 程式開發工作階段（Sessions，例如 Claude Code、Codex、Antigravity ACP），各自在終端機中執行程式開發、重構、除錯與測試。
- 使用者現在正在外面或用手機操作，透過雲端連線與你交談。你的職責是當使用者的隨身技術管家，協助監看這台電腦上所有的開發進度。

【你可以讀取哪些資料】
1. list_sessions：讀取當前電腦上所有的工作階段（Sessions），包含 Session ID、名稱（label/title）、以及工作狀態（如 busy、idle、waiting for input）。
2. get_session_screen：讀取指定 Session 當前終端機螢幕的最新文字輸出內容（包含指令輸出、程式碼改動、測試結果或錯誤報告）。

【你可以執行什麼操作】
1. 語音摘要回報：主動從 Sessions 清單或終端螢幕中提煉重點，以簡明扼要、條理清晰的繁體中文口頭向使用者回報。
2. send_message_to_session：代使用者將文字指令直接鍵入指定 Session 終端機（例如回應 Agent 的詢問、下達修復指令或執行命令）。

【核心原則：主動呼叫工具】
- 當使用者詢問「進度如何」、「現在在跑什麼」、「幫我看 session」、「有什麼任務」時，你必須【立刻主動調用 list_sessions 或 get_session_screen 工具】，絕對不要只在口頭上回答或反問！
- 查詢到 Sessions 後，主動告訴使用者有哪幾個任務在跑，並詢問要不要看某個任務的畫面。
- 如果使用者指定看某個任務（例如「看第一個」、「看 codex」、「看測試那個」），立刻調用 get_session_screen 讀取最新終端輸出，並用語音摘述其進度與是否有錯誤。
- 全程使用台灣繁體中文（zh-Hant-TW），語氣專業自然，適合語音聆聽，避免唸出長串無意義的哈希值或代碼符號。`,
        tools: [
          {
            type: 'function',
            name: 'list_sessions',
            description:
              '查詢目前這台電腦上所有運作中或閒置的 Sessions 清單與狀態。當使用者詢問進度、有什麼任務在跑、或需要查看 session 時，必須優先主動呼叫此工具。',
            parameters: { type: 'object', properties: {} },
          },
          {
            type: 'function',
            name: 'get_session_screen',
            description:
              '讀取特定 Session 當前的終端機螢幕文字輸出，以供摘要其執行進度、最新輸出與狀態。',
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
          "請以親切、專業的繁體中文向使用者問候，並主動介紹自己的功能：『你好！我是 Clawdline 隨身語音副駕駛。我能幫你隨時掌握這台電腦上所有 AI 程式開發 Sessions 與任務終端。你可以隨時問我目前有哪些任務在跑、叫我幫你查看某個任務的即時終端畫面與執行進度，或是讓我代你向終端發送文字指令。需要我先幫你看看現在有哪些 Sessions 嗎？』",
      },
    }
    this.dc.send(JSON.stringify(greetingRequest))
    this.addLog('info', '已送出開場問候要求')
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
          this.addLog('info', `語音轉文字識別結果：「${event.transcript}」`)
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

    this.addLog('tool', `執行工具【${name}】參數: ${JSON.stringify(args)}`)
    this.updateState({
      toolCallLog: `正在執行工具：${name}...`,
      speakingState: 'thinking',
    })

    let resultOutput = ''
    try {
      if (name === 'list_sessions') {
        const res = await fetch('/v1/sessions')
        if (!res.ok) {
          resultOutput = JSON.stringify({ error: `查詢失敗 (HTTP ${res.status})` })
          this.addLog('error', `查詢 sessions 失敗: HTTP ${res.status}`)
        } else {
          const data = await res.json()
          const rawList = data.sessions || []
          const cleanSessions = rawList.map((s: any) => ({
            id: s.id || s.sessionId,
            label: s.label || s.title || s.assistant || s.id,
            state: s.state,
            work_state: s.work_state,
          }))
          resultOutput = JSON.stringify({
            count: cleanSessions.length,
            sessions: cleanSessions,
          })
          this.addLog('info', `成功讀取 Sessions 清單，共 ${cleanSessions.length} 個工作階段`)
        }
      } else if (name === 'get_session_screen') {
        const sessionId = args.session_id
        const res = await fetch(`/v1/sessions/${encodeURIComponent(sessionId)}/screen`)
        if (!res.ok) {
          resultOutput = JSON.stringify({ error: `無法讀取螢幕 (HTTP ${res.status})` })
          this.addLog('error', `讀取 Session ${sessionId} 螢幕失敗: HTTP ${res.status}`)
        } else {
          const data = await res.json()
          const screenText = data.screen?.text || ''
          const lines = screenText.split('\n')
          const recentLines = lines.slice(-40).join('\n')
          resultOutput = JSON.stringify({
            session_id: sessionId,
            recent_screen_text: recentLines,
          })
          this.addLog('info', `成功讀取 Session ${sessionId} 畫面 (擷取最近 ${lines.length} 行)`)
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
          this.addLog('error', `發送訊息至 Session ${session_id} 失敗: HTTP ${res.status}`)
        } else {
          resultOutput = JSON.stringify({ success: true, message: '訊息已成功送入該 Session 終端' })
          this.addLog('info', `已成功發送訊息至 Session ${session_id}`)
        }
      } else {
        resultOutput = JSON.stringify({ error: `未知的工具：${name}` })
        this.addLog('error', `未知的工具請求：${name}`)
      }
    } catch (err) {
      resultOutput = JSON.stringify({ error: err instanceof Error ? err.message : String(err) })
      this.addLog('error', `工具執行異常: ${resultOutput}`)
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
