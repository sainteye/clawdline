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

// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { UNOPENED_READ_HEADER } from "../cloud/unopened-read.ts"

/** Every content read names its Session on purpose: the copilot is opened inside another one. */
const NAMED_READ: RequestInit = { headers: { [UNOPENED_READ_HEADER]: "1" } }

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
  /** Unix seconds; how long ago is said from this. */
  now?: () => number
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

export const COPILOT_INSTRUCTIONS = `你是 Clawdline 的語音副駕駛。使用者在手機上用語音問你，這台電腦上各個 AI 程式開發 Session（Claude Code、Codex 等）的狀況。

【說話方式：短】
- 每次回覆最多三句，能一句講完就一句。這是語音，不是文章。
- 不要開場白（不說「好的」「我來查看」「請稍等」），不要重複使用者的問題，不要在結尾問「還需要什麼嗎」「要不要看畫面」。
- 呼叫工具前不要先講話，拿到結果再直接講重點。
- 不要唸 Session ID、路徑、指令、雜湊值。用 Session 的名稱稱呼它。
- 全程台灣繁體中文。

【工具】
- list_sessions：所有 Session 的整理好的狀態。結果已分成 needs_you（等使用者回應，附它在問什麼）、working（工作中，附 latest 最近一句進度）、done（已完成）、idle（閒置）。
- get_session_activity：某個 Session 的工作紀錄（使用者交代什麼、AI 回報了什麼、跑了什麼）。解釋「它在做什麼、做到哪」用這個。
- get_session_screen：某個 Session 的終端畫面，只在需要看畫面細節時用。
- send_message_to_session：替使用者把文字送進某個 Session。送之前先唸出內容並等使用者說好。
- session_id 一律填 list_sessions 結果裡的 id，原樣照填（包含開頭的 % 符號）；還沒查過清單就先呼叫 list_sessions。遇到 error 附 candidates 時，依使用者原本說的話自己挑對的 id 重試；真的分不出來才唸出候選名稱讓使用者選。
- 使用者問進度、在跑什麼、某個 Session 怎麼了，一律先呼叫工具，不要憑空回答。工具回傳 error 就照實說讀不到與原因，不要編造。

【回報清單】
- 第一句一定先照 summary 講總數與分類，例如「五個 Session：一個在等你、兩個在工作、兩個閒置。」
- 接著依序：等你的，說名稱與它在問什麼；工作中的，每個都說名稱，再用半句說在做什麼（依 latest），例如「雲端配對在補加密命令」。名稱太長可以縮短，但不要用「第一個」「第二個」代替。已完成與閒置的只說數量，使用者問才展開。

【解說單一 Session】
- 讀 get_session_activity 後，用兩到三句講：它在做什麼（目標）、做到哪（依 last_report 講最近的具體結果）、現在要不要使用者處理（在等什麼）。閒置的 Session 也要說它最後完成了什麼。
- 依紀錄內容具體講，不要只說「在工作中」或「已閒置」。`

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
    name: 'get_session_activity',
    description:
      '讀取特定 Session 內部的近期工作紀錄：使用者的交代、AI 助理的說明與回報、最近執行的工具與指令。用來解釋這個 Session 在做什麼、進度與結果。',
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
        '用一句繁體中文問候，說你可以回報這台電腦上各個 Session 的狀況。不要超過一句。',
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
  /** Unix seconds, when present. */
  working_since?: number
  last_activity_at?: number
  question?: string
  options?: string[]
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function rowsOf(data: unknown): CopilotSessionRow[] {
  const raw = data && typeof data === 'object' ? (data as { sessions?: unknown }).sessions : null
  if (!Array.isArray(raw)) return []
  return raw
    .filter((s): s is Record<string, unknown> => !!s && typeof s === 'object' && !!str((s as Record<string, unknown>).id))
    .map((s) => {
      const row: CopilotSessionRow = {
        id: str(s.id),
        label: str(s.label) || str(s.title) || str(s.assistant) || str(s.id),
        assistant: str(s.assistant),
        state: str(s.state),
        work_state: str(s.work_state),
      }
      if (typeof s.working_since === 'number' && s.working_since > 0) row.working_since = s.working_since
      const activity = s.activity as { at?: unknown; known?: unknown } | undefined
      if (activity && activity.known === true && typeof activity.at === 'number') row.last_activity_at = activity.at
      const menu = s.menu as { question?: unknown; options?: unknown } | undefined
      if (menu && typeof menu === 'object') {
        if (str(menu.question)) row.question = plainScreenText(str(menu.question))
        if (Array.isArray(menu.options)) {
          row.options = menu.options
            .map((o) => (o && typeof o === 'object' ? plainScreenText(str((o as { label?: unknown }).label)) : ''))
            .filter(Boolean)
            .slice(0, 6)
        }
      }
      return row
    })
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
): { row: CopilotSessionRow } | { error: string; candidates?: { id: string; label: string }[] } {
  const w = wanted.trim()
  if (!w) return { error: '沒有指定要看哪一個 Session' }
  const byId = rows.find((r) => r.id === w)
  if (byId) return { row: byId }
  // A tmux pane id said or typed without its sign: "2" for "%2". Never a
  // position in the list, which changes as Sessions start and stop.
  if (/^\d+$/.test(w)) {
    const pane = rows.find((r) => r.id === `%${w}`)
    if (pane) return { row: pane }
  }
  const lower = w.toLowerCase()
  const tiers = [
    rows.filter((r) => r.label.toLowerCase() === lower || r.assistant.toLowerCase() === lower),
    rows.filter((r) => r.label.toLowerCase().includes(lower)),
  ]
  for (const hits of tiers) {
    if (hits.length === 1) return { row: hits[0] }
    if (hits.length > 1) {
      return {
        error: `「${w}」對應到 ${hits.length} 個 Sessions：${hits.map((r) => r.label).join('、')}`,
        candidates: hits.map((r) => ({ id: r.id, label: r.label })),
      }
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

/** How many Sessions' latest words a list carries: each is one transcript read. */
export const LIST_LATEST_READS = 6
export const LIST_LATEST_CHARS = 140

function minutesSince(at: number | undefined, now: number): number | undefined {
  if (!at || !now || at > now) return undefined
  return Math.round((now - at) / 60)
}

/** The newest thing the assistant said, as a status line. */
export function latestOf(entries: unknown): string {
  if (!Array.isArray(entries)) return ''
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i] as Record<string, unknown> | null
    if (e && e.role === 'assistant' && str(e.text).trim()) return clip(plainScreenText(str(e.text)), LIST_LATEST_CHARS)
  }
  return ''
}

async function latestWords(deps: ToolDeps, row: CopilotSessionRow): Promise<string> {
  try {
    const res = await deps.fetch(`/v1/transcript?session=${encodeURIComponent(row.id)}`, NAMED_READ)
    if (!res.ok) return ''
    return latestOf(((await res.json()) as { entries?: unknown })?.entries)
  } catch {
    // refusal-ok: one Session's unreadable transcript leaves its row without a latest line, not the list without rows.
    return ''
  }
}

function waits(r: CopilotSessionRow): boolean {
  return r.state === 'waiting' || r.work_state === 'waiting' || !!r.question
}

async function listSessions(deps: ToolDeps, logs: ToolLog[]): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const now = (deps.now ?? (() => Math.floor(Date.now() / 1000)))()
  const rows = read.rows
  const needs = rows.filter(waits)
  const working = rows.filter((r) => !waits(r) && r.state === 'working')
  const done = rows.filter((r) => !waits(r) && r.state !== 'working' && r.work_state === 'milestone_complete')
  const idle = rows.filter((r) => !needs.includes(r) && !working.includes(r) && !done.includes(r))
  const wanted = [...needs, ...working].slice(0, LIST_LATEST_READS)
  const latest = new Map<string, string>()
  await Promise.all(wanted.map(async (r) => latest.set(r.id, await latestWords(deps, r))))
  const brief = (r: CopilotSessionRow) => ({ id: r.id, label: r.label, assistant: r.assistant })
  const out = {
    summary: `${rows.length} 個 Session：${needs.length} 個等你回應、${working.length} 個工作中、${done.length} 個已完成、${idle.length} 個閒置`,
    needs_you: needs.map((r) => ({
      ...brief(r),
      ...(r.question ? { question: r.question } : {}),
      ...(r.options?.length ? { options: r.options } : {}),
      ...(latest.get(r.id) ? { latest: latest.get(r.id) } : {}),
    })),
    working: working.map((r) => ({
      ...brief(r),
      ...(minutesSince(r.working_since, now) !== undefined ? { working_minutes: minutesSince(r.working_since, now) } : {}),
      ...(latest.get(r.id) ? { latest: latest.get(r.id) } : {}),
    })),
    done: done.map((r) => ({ ...brief(r), ...(minutesSince(r.last_activity_at, now) !== undefined ? { minutes_ago: minutesSince(r.last_activity_at, now) } : {}) })),
    idle: idle.map((r) => ({ ...brief(r), ...(minutesSince(r.last_activity_at, now) !== undefined ? { minutes_ago: minutesSince(r.last_activity_at, now) } : {}) })),
  }
  logs.push({ level: 'info', text: `成功讀取 Sessions 清單：${out.summary}` })
  return out
}

/** A screen's text once its capture has arrived, or why it could not be had. */
async function readScreen(deps: ToolDeps, row: CopilotSessionRow): Promise<{ text: string; attempt: number } | { error: string }> {
  const path = `/v1/sessions/${encodeURIComponent(row.id)}/screen`
  for (let attempt = 1; attempt <= SCREEN_READ_ATTEMPTS; attempt++) {
    const res = await deps.fetch(path, NAMED_READ)
    if (!res.ok) {
      const why = await refusalText(res)
      return { error: `無法讀取「${row.label}」的畫面 (HTTP ${res.status})${why ? `：${why}` : ''}` }
    }
    const data = (await res.json()) as { screen?: { text?: unknown; pending?: unknown } }
    const screen = data?.screen ?? {}
    if (typeof screen.text === 'string') return { text: screen.text, attempt }
    if (screen.pending !== true) return { error: `「${row.label}」的終端畫面目前無法讀取` }
    if (attempt < SCREEN_READ_ATTEMPTS) await deps.sleep(SCREEN_READ_INTERVAL_MS)
  }
  return { error: `「${row.label}」的畫面擷取逾時，請稍後再試` }
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
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  const screen = await readScreen(deps, row)
  if ('error' in screen) {
    logs.push({ level: 'error', text: screen.error })
    return { error: screen.error }
  }
  const tail = screenTail(screen.text)
  logs.push({
    level: 'info',
    text: `成功讀取「${row.label}」畫面（第 ${screen.attempt} 次讀取，提供最後 ${tail ? tail.split('\n').length : 0} 行）`,
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

/** How much of a Session's transcript the model is given. */
export const ACTIVITY_ENTRIES = 60
export const ACTIVITY_TEXT_CHARS = 600
export const ACTIVITY_TOOL_CHARS = 160
export const ACTIVITY_TOTAL_CHARS = 9000

export interface ActivityEntry {
  role: string
  text: string
  tool?: string
}

/** Words over the budget lose their middle: a report's conclusion is at its end. */
function clip(text: string, n: number): string {
  const t = text.replace(/\s+/g, ' ').trim()
  if (t.length <= n) return t
  const head = Math.ceil(n / 2)
  return t.slice(0, head) + ' … ' + t.slice(t.length - (n - head))
}

/** The rows of a waiting Session's screen that carry its question. */
export const WAITING_SCREEN_LINES = 15

/**
 * The recent turns of a transcript, newest last, within a character budget.
 *
 * What a person says and what the assistant reports keep more of their words
 * than a tool line: a command is evidence of what was done, its full text is
 * not something to read aloud. The first user message that is still in the
 * window stays even when the budget runs out, because it is the goal.
 */
export function activityOf(entries: unknown): ActivityEntry[] {
  if (!Array.isArray(entries)) return []
  const rows: ActivityEntry[] = []
  for (const e of entries.slice(-ACTIVITY_ENTRIES)) {
    if (!e || typeof e !== 'object') continue
    const r = e as Record<string, unknown>
    const role = str(r.role)
    const text = plainScreenText(str(r.text))
    if (!role || !text.trim()) continue
    const tool = str(r.tool)
    rows.push({
      role,
      text: clip(text, role === 'tool' ? ACTIVITY_TOOL_CHARS : ACTIVITY_TEXT_CHARS),
      ...(tool ? { tool } : {}),
    })
  }
  const kept: ActivityEntry[] = []
  let used = 0
  for (let i = rows.length - 1; i >= 0; i--) {
    used += rows[i].text.length + 16
    if (used > ACTIVITY_TOTAL_CHARS) break
    kept.unshift(rows[i])
  }
  const firstUser = rows.find((r) => r.role === 'user')
  if (firstUser && !kept.includes(firstUser)) kept.unshift(firstUser)
  return kept
}

async function sessionActivity(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveSession(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  const res = await deps.fetch(`/v1/transcript?session=${encodeURIComponent(row.id)}`, NAMED_READ)
  if (!res.ok) {
    const why = await refusalText(res)
    const error = `無法讀取「${row.label}」的工作紀錄 (HTTP ${res.status})${why ? `：${why}` : ''}`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  const data = (await res.json()) as { entries?: unknown }
  const activity = activityOf(data?.entries)
  const lastReport = (() => {
    const entries = Array.isArray(data?.entries) ? data.entries : []
    for (let i = entries.length - 1; i >= 0; i--) {
      const e = entries[i] as Record<string, unknown> | null
      if (e && e.role === 'assistant' && str(e.text).trim()) return clip(plainScreenText(str(e.text)), ACTIVITY_TEXT_CHARS)
    }
    return ''
  })()
  logs.push({ level: 'info', text: `成功讀取「${row.label}」工作紀錄（${activity.length} 筆）` })
  // A Session that waits on the person shows its question on the screen (an
  // approval menu, a choice), not in its transcript.
  let waiting: { waiting_screen_text?: string } = {}
  if (row.state === 'waiting' || row.work_state === 'waiting') {
    const screen = await readScreen(deps, row)
    if (!('error' in screen)) waiting = { waiting_screen_text: screenTail(screen.text, WAITING_SCREEN_LINES) }
  }
  return {
    session_id: row.id,
    label: row.label,
    assistant: row.assistant,
    state: row.state,
    work_state: row.work_state,
    ...(lastReport ? { last_report: lastReport } : {}),
    recent_activity: activity,
    ...waiting,
    ...(activity.length ? {} : { note: '這個 Session 目前沒有可讀的工作紀錄，可改用 get_session_screen 看終端畫面' }),
  }
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
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
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
    else if (name === 'get_session_activity') result = await sessionActivity(deps, logs, str(args.session_id))
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
