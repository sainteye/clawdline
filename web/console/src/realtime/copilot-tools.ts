/**
 * What the voice copilot's tools do, without a microphone or a peer connection.
 *
 * The WebRTC client (`realtime-copilot.ts`) only moves events; everything the
 * model is told about this machine's Sessions comes out of here, so it can be
 * driven by `node --test` against the shapes the daemon and the Cloud reader
 * actually answer.
 *
 * Two facts about `GET /v1/sessions/{id}/screen` shape this file:
 *
 * - **The first read of a Session is `pending`.** Reading starts the capture
 *   (`internal/transport/http/screen.go`), so the first answer carries no
 *   `text` and the screen arrives a moment later. A single read told the model
 *   the terminal was empty.
 * - **`text` is painted.** It carries the terminal's SGR colour sequences;
 *   handed over as it is, the model reads escape codes aloud or loses the words
 *   between them.
 */

export type ToolLogLevel = 'info' | 'tool' | 'error'

export interface ToolLog {
  level: ToolLogLevel
  text: string
}

export interface ToolDeps {
  fetch: (input: string, init?: RequestInit) => Promise<Response>
  sleep: (ms: number) => Promise<void>
  /** A fresh key per send, so a retried POST is the same send to the machine. */
  newRequestID?: () => string
}

export interface ToolResult {
  output: string
  logs: ToolLog[]
}

/** How many times a pending screen is read again, and how long apart. */
export const SCREEN_READ_ATTEMPTS = 8
export const SCREEN_READ_INTERVAL_MS = 400
/** The tail of a screen the model is given: enough for a prompt and its last output. */
export const SCREEN_TAIL_LINES = 40

export const COPILOT_INSTRUCTIONS = `你是 Clawdline 的隨身全雙工語音副駕駛（Voice Copilot）。
使用者目前正透過手機語音與你直接對話。

【什麼是 Clawdline 與目前環境】
- Clawdline 是使用者的主機 AI 程式開發與多 Agent 編排守護行程（Local Agent Orchestrator），運行在使用者本機電腦上。
- 使用者在電腦上啟動了多個 AI 程式開發工作階段（Sessions，例如 Claude Code、Codex、Antigravity），各自在終端機中執行程式開發、重構、除錯與測試。
- 使用者現在正在外面或用手機操作，透過雲端連線與你交談。你的職責是當使用者的隨身技術管家，協助監看這台電腦上所有的開發進度。

【你可以讀取哪些資料】
1. list_sessions：讀取當前電腦上所有的工作階段（Sessions），包含 Session ID、名稱（label）、助理種類（assistant）與工作狀態（working 表示正在工作、idle 表示閒置、waiting 表示在等使用者回應）。
2. get_session_screen：讀取指定 Session 當前終端機螢幕的最新文字輸出內容（包含指令輸出、程式碼改動、測試結果或錯誤報告）。session_id 可以填清單裡的 id，也可以填名稱或助理種類（例如 codex）。

【你可以執行什麼操作】
1. 語音摘要回報：主動從 Sessions 清單或終端螢幕中提煉重點，以簡明扼要、條理清晰的繁體中文口頭向使用者回報。
2. send_message_to_session：代使用者將文字指令直接鍵入指定 Session 終端機。送出前一定要先唸出要送的內容並得到使用者確認。

【核心原則：主動呼叫工具】
- 當使用者詢問「進度如何」、「現在在跑什麼」、「幫我看 session」、「有什麼任務」時，你必須【立刻主動調用 list_sessions 或 get_session_screen 工具】，絕對不要只在口頭上回答或反問！
- 回報清單時，先說總數與幾個正在工作、幾個閒置、幾個在等回應，再依序用名稱介紹正在工作與等待中的任務，最後詢問要不要看某個任務的畫面。
- 如果使用者指定看某個任務（例如「看第一個」、「看 codex」、「看測試那個」），立刻調用 get_session_screen 讀取最新終端輸出，並用口語解說：它正在做什麼、最近的結果、有沒有錯誤或在等使用者決定。
- 工具回傳 error 時，照實告訴使用者讀不到以及原因，不要編造畫面內容。
- 全程使用台灣繁體中文（zh-Hant-TW），語氣專業自然，適合語音聆聽，不要唸出 Session ID、哈希值或長串代碼符號。`

export const COPILOT_TOOLS = [
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
    description: '讀取特定 Session 當前的終端機螢幕文字輸出，以供摘要其執行進度、最新輸出與狀態。',
    parameters: {
      type: 'object',
      properties: {
        session_id: {
          type: 'string',
          description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。',
        },
      },
      required: ['session_id'],
    },
  },
  {
    type: 'function',
    name: 'send_message_to_session',
    description: '將使用者確認過的文字指令或訊息直接鍵入指定 Session 的終端機輸入框。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: 'list_sessions 回傳的 id，或 Session 名稱' },
        message: { type: 'string', description: '要傳送給該 Session 的文字內容' },
      },
      required: ['session_id', 'message'],
    },
  },
] as const

/**
 * The session configuration in the Realtime GA shape.
 *
 * `/v1/realtime/calls` is the GA endpoint, and GA takes `session.type`,
 * `output_modalities` and the `audio.input` / `audio.output` blocks; the beta
 * names (`modalities`, `voice`, `input_audio_transcription` at the top level)
 * are refused with an `error` event, which left the call talking without its
 * tools or its instructions.
 */
export function sessionUpdateEvent(): Record<string, unknown> {
  return {
    type: 'session.update',
    session: {
      type: 'realtime',
      output_modalities: ['audio'],
      instructions: COPILOT_INSTRUCTIONS,
      audio: {
        input: {
          transcription: { model: 'whisper-1' },
          turn_detection: { type: 'server_vad' },
        },
        output: { voice: 'alloy' },
      },
      tools: COPILOT_TOOLS,
      tool_choice: 'auto',
    },
  }
}

export function greetingEvent(): Record<string, unknown> {
  return {
    type: 'response.create',
    response: {
      output_modalities: ['audio'],
      instructions:
        '請以親切、專業的繁體中文簡短問候使用者，用一兩句話說明你能幫忙查看這台電腦上有哪些 Sessions 在跑、讀某個 Session 的終端畫面並解說，或代為送出指令，然後詢問要不要先看看目前有哪些 Sessions。',
    },
  }
}

// CSI sequences (colour, cursor), OSC sequences (titles, links) and the lone
// two-byte escapes a terminal capture can carry.
// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]/g

/** The words of a painted terminal capture. */
export function plainScreenText(text: string): string {
  return text
    .replace(ANSI, '')
    .replace(/\r/g, '')
    .split('\n')
    .map((line) => line.replace(/\s+$/, ''))
    .join('\n')
}

/** The last `n` lines of a screen, with the blank rows under the prompt dropped. */
export function screenTail(text: string, n = SCREEN_TAIL_LINES): string {
  const lines = plainScreenText(text).split('\n')
  while (lines.length && lines[lines.length - 1] === '') lines.pop()
  return lines.slice(-n).join('\n')
}

export interface CopilotSessionRow {
  id: string
  label: string
  assistant: string
  state: string
  work_state: string
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function rowsOf(data: unknown): CopilotSessionRow[] {
  const raw = data && typeof data === 'object' ? (data as { sessions?: unknown }).sessions : null
  if (!Array.isArray(raw)) return []
  return raw
    .filter((s): s is Record<string, unknown> => !!s && typeof s === 'object' && !!str((s as Record<string, unknown>).id))
    .map((s) => ({
      id: str(s.id),
      label: str(s.label) || str(s.title) || str(s.assistant) || str(s.id),
      assistant: str(s.assistant),
      state: str(s.state),
      work_state: str(s.work_state),
    }))
}

async function refusalText(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: unknown; message?: unknown }
    const err = body?.error
    if (err && typeof err === 'object') {
      const e = err as { message?: unknown; code?: unknown }
      return str(e.message) || str(e.code)
    }
    return str(err) || str(body?.message)
  } catch {
    // refusal-ok: a refusal without a JSON body is still reported by its HTTP status.
    return ''
  }
}

async function readRows(deps: ToolDeps): Promise<{ rows: CopilotSessionRow[] } | { error: string }> {
  const res = await deps.fetch('/v1/sessions')
  if (!res.ok) {
    const why = await refusalText(res)
    return { error: `查詢 Sessions 失敗 (HTTP ${res.status})${why ? `：${why}` : ''}` }
  }
  return { rows: rowsOf(await res.json()) }
}

/**
 * Which row a spoken name stands for: the id itself, then an exact label or
 * assistant, then a label that contains it. Ambiguity is returned, not guessed.
 */
export function resolveSession(
  rows: readonly CopilotSessionRow[],
  wanted: string,
): { row: CopilotSessionRow } | { error: string } {
  const w = wanted.trim()
  if (!w) return { error: '沒有指定要看哪一個 Session' }
  const byId = rows.find((r) => r.id === w)
  if (byId) return { row: byId }
  const lower = w.toLowerCase()
  const tiers = [
    rows.filter((r) => r.label.toLowerCase() === lower || r.assistant.toLowerCase() === lower),
    rows.filter((r) => r.label.toLowerCase().includes(lower)),
  ]
  for (const hits of tiers) {
    if (hits.length === 1) return { row: hits[0] }
    if (hits.length > 1) {
      return { error: `「${w}」對應到 ${hits.length} 個 Sessions：${hits.map((r) => r.label).join('、')}，請使用者指定是哪一個` }
    }
  }
  return { error: `找不到名為「${w}」的 Session` }
}

function countBy(rows: readonly CopilotSessionRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const r of rows) {
    const k = r.state || 'unknown'
    out[k] = (out[k] ?? 0) + 1
  }
  return out
}

async function listSessions(deps: ToolDeps, logs: ToolLog[]): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  logs.push({ level: 'info', text: `成功讀取 Sessions 清單，共 ${read.rows.length} 個工作階段` })
  return { count: read.rows.length, by_state: countBy(read.rows), sessions: read.rows }
}

async function sessionScreen(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveSession(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, sessions: read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  const path = `/v1/sessions/${encodeURIComponent(row.id)}/screen`
  for (let attempt = 1; attempt <= SCREEN_READ_ATTEMPTS; attempt++) {
    const res = await deps.fetch(path)
    if (!res.ok) {
      const why = await refusalText(res)
      const error = `無法讀取「${row.label}」的畫面 (HTTP ${res.status})${why ? `：${why}` : ''}`
      logs.push({ level: 'error', text: error })
      return { error }
    }
    const data = (await res.json()) as { screen?: { text?: unknown; pending?: unknown; readable?: unknown } }
    const screen = data?.screen ?? {}
    if (typeof screen.text === 'string') {
      const tail = screenTail(screen.text)
      logs.push({
        level: 'info',
        text: `成功讀取「${row.label}」畫面（第 ${attempt} 次讀取，提供最後 ${tail ? tail.split('\n').length : 0} 行）`,
      })
      return {
        session_id: row.id,
        label: row.label,
        assistant: row.assistant,
        state: row.state,
        recent_screen_text: tail,
        ...(tail ? {} : { note: '終端畫面目前是空白的' }),
      }
    }
    if (screen.pending !== true) {
      const error = `「${row.label}」的終端畫面目前無法讀取`
      logs.push({ level: 'error', text: error })
      return { error }
    }
    if (attempt < SCREEN_READ_ATTEMPTS) await deps.sleep(SCREEN_READ_INTERVAL_MS)
  }
  const error = `「${row.label}」的畫面擷取逾時，請稍後再試`
  logs.push({ level: 'error', text: error })
  return { error }
}

async function sendMessage(deps: ToolDeps, logs: ToolLog[], wanted: string, message: string): Promise<unknown> {
  if (!message.trim()) return { error: '沒有要送出的內容' }
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveSession(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error }
  }
  const { row } = found
  const res = await deps.fetch(`/v1/sessions/${encodeURIComponent(row.id)}/send`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': (deps.newRequestID ?? (() => crypto.randomUUID()))(),
    },
    body: JSON.stringify({ text: message }),
  })
  if (!res.ok) {
    const why = await refusalText(res)
    const error = `送到「${row.label}」失敗 (HTTP ${res.status})${why ? `：${why}` : ''}`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  logs.push({ level: 'info', text: `已送出訊息至「${row.label}」` })
  return { success: true, label: row.label }
}

/** Run one tool call; never throws, every failure is an `error` the model can say. */
export async function runCopilotTool(name: string, argsJson: string, deps: ToolDeps): Promise<ToolResult> {
  const logs: ToolLog[] = []
  let args: Record<string, unknown> = {}
  try {
    const parsed = JSON.parse(argsJson || '{}')
    if (parsed && typeof parsed === 'object') args = parsed
  } catch {
    // refusal-ok: malformed arguments are treated as none; each tool says what it is missing.
  }
  logs.push({ level: 'tool', text: `執行工具【${name}】參數: ${JSON.stringify(args)}` })
  let result: unknown
  try {
    if (name === 'list_sessions') result = await listSessions(deps, logs)
    else if (name === 'get_session_screen') result = await sessionScreen(deps, logs, str(args.session_id))
    else if (name === 'send_message_to_session') result = await sendMessage(deps, logs, str(args.session_id), str(args.message))
    else {
      result = { error: `未知的工具：${name}` }
      logs.push({ level: 'error', text: `未知的工具請求：${name}` })
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    result = { error: msg }
    logs.push({ level: 'error', text: `工具執行異常: ${msg}` })
  }
  return { output: JSON.stringify(result), logs }
}

/** What answers a function call: its output, then a request for the spoken reply. */
export function toolReplyEvents(callID: string, output: string): Record<string, unknown>[] {
  return [
    { type: 'conversation.item.create', item: { type: 'function_call_output', call_id: callID, output } },
    { type: 'response.create' },
  ]
}
