/**
 * The voice copilot over GPT-Live (docs/voice.md, "GPT-Live calls").
 *
 * GPT-Live is full duplex: it listens while it speaks and hands reasoning and
 * tool calls to a Responses backend (`delegation.type: "responses"`). The
 * backend's function calls arrive here and run through the same tools as a
 * Realtime call (`runCopilotTool`), so what a call can read and do is the
 * same in both systems. The live model gets only a short persona; the long
 * tool rules, the Session snapshot and the tools go to the backend over the
 * call's data channel once the session has started.
 *
 * Event names and shapes follow OpenAI's GPT-Live guides as published on
 * 2026-11-15 (developers.openai.com/api/docs/guides/live-delegation and
 * live-conversations). Nothing here was checked against a live session yet.
 */
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { COPILOT_TOOLS, briefingSection, extraSection, instructionsFor, languageRule, originSection, type CallOptions } from './copilot-tools.ts'

/** The backend model the machine opens a session with (copilot.LiveBackendModel). */
export const LIVE_BACKEND_MODEL = 'gpt-6-luna'

/** GPT-Live's price per second of a session, as OpenAI lists it ($0.05 a minute). */
export const LIVE_USD_PER_SECOND = 0.05 / 60
/** The backend model's price per 1M tokens (gpt-6-luna, standard short context). */
export const LIVE_BACKEND_RATES = { input: 0.1, cached: 0.01, output: 0.5 }

/** The spoken persona the live model starts with. Kept short: OpenAI caps it at 16,384 tokens and procedures belong to the backend. */
export function liveInstructions(options: CallOptions = {}): string {
  const brief = options.brief !== false
  return [
    `你是 Clawdline 的語音副駕駛，也是使用者的私人秘書。使用者多半在手機上，像講電話一樣問你這台電腦上各個 AI 程式開發 Session（Claude Code、Codex 等）的狀況，也會請你替他做事。

Personality: 你跟了他很久、很熟他的工作。親切、放鬆、反應快、有自己的判斷，用台灣日常口語，稱呼他「你」。不客服腔、不道歉、不誇張驚嘆。
Speaking style: 這是對話，不是報告。${brief ? '一般回答一到三句，' : ''}講完重點就停，讓他接話；他想知道更多自然會問。說法自己決定、每次換著說，不要念稿。技術用語改說它對他的影響，不念 Session ID、路徑、指令或網址。
Backchannel policy: Use moderate, brief and varied backchannels while the person is speaking.
Interruption policy: Stop speaking when the person interrupts, and listen. Answer what they just said; do not go back to finish the old explanation.
Delegation policy: 你自己不知道任何 Session 的狀況，也不能自己做任何事。凡是問到 Session、Clawdline 怎麼用、整體狀況，或要你送訊息、回答、打開、關閉、改名、開新 Session、執行 Snippet，一律先產生 <|SpawnThinking|>，等結果回來再講。Generate <|SpawnThinking|> before giving an answer that depends on backend work. 結果回來前不要說已經做好、已經送出或任何 Session 的內容；結果說要使用者確認時，照它的意思問他一句再等他回答。`,
    languageRule(options.language ?? ''),
    extraSection(options.extra ?? ''),
  ].filter(Boolean).join('\n\n')
}

/** The backend's job, said before the Realtime call's own tool rules, which it reuses whole. */
export const LIVE_BACKEND_PREAMBLE = `# 你的角色
你是語音秘書背後負責查資料和做事的後端。使用者聽不到你，另一個語音模型會把你寫的結果用口語轉述給他。
- 依下面的規則選工具、確認對象、核對證據；規則裡「說」「講」「問」的事，就寫成要轉述的內容。
- 結果寫得短、像口語：先寫結論和要他處理的事，再補一兩句原因。不要條列、不要 Markdown、不要 Session ID、路徑或指令。
- 需要使用者確認或選擇時，只寫要問他的那一句，然後停；他回答後會再交給你。絕對不要替他確認。
- 沒查到就說沒查到，不要編。`

/** session.update carrying the backend's instructions and tools, sent once the session has started and again when its context changes. */
/**
 * Said after the Realtime rules, which it overrides for a Live call. On
 * 2026-11-15 a Live call asked about every Session and the backend read each
 * one in turn before anything was said, because the rules demand a read even
 * when the snapshot has the Session; the person waited the whole time.
 */
export const LIVE_BACKEND_FLOW = `# GPT-Live 的做法（優先於上面的規則）
- 通話開始時的 Session 快照已經帶了每個 Session 背景寫好的 recap 和寫好多久（recap_written）。使用者問整體狀況或「各個 Session 在做什麼」時，直接用快照的 recap 回答，說明是多久前的狀況，不必呼叫工具；他要某一個的細節、最新狀況，或快照沒有它的 recap 時，才呼叫工具。
- 使用者要某個 Session 的「最新狀況」「最新摘要」「再看一次」時，直接呼叫一次 get_session_summary 並帶 fresh: true，不要先讀 get_session_updates 再讀摘要：讀兩次會讓他多等二十秒以上，而且只有摘要會立刻講給他聽。他問的是「後來做了什麼、有什麼新進展」時才用 get_session_updates。
- 每查完一個 Session，系統會立刻把它的摘要交給語音模型先講給使用者聽，你同時繼續查下一個。所以你的最後回覆不要把查過的 Session 再逐一重講：只補還沒講到的、要使用者決定的事，或一句整體結論；都講過了就只寫一句簡短收尾。`

export function liveBackendUpdate(memory = '', snapshot = '', options: CallOptions = {}, backendModel = LIVE_BACKEND_MODEL): Record<string, unknown> {
  const instructions = [LIVE_BACKEND_PREAMBLE, instructionsFor(options.brief !== false, options.language ?? ''), LIVE_BACKEND_FLOW, extraSection(options.extra ?? ''), originSection(options.origin), briefingSection(options.briefing ?? ''), snapshot, memory]
    .filter(Boolean).join('\n\n')
  return {
    type: 'session.update',
    event_id: `backend_${crypto.randomUUID()}`,
    // OpenAI refuses a delegation update without its type (missing_required_parameter,
    // 'session.delegation.type', seen on the first real call on 2026-11-15).
    session: { delegation: { type: 'responses', responses: { model: backendModel, instructions, tools: COPILOT_TOOLS, tool_choice: 'auto', parallel_tool_calls: false } } },
  }
}

/** A finished backend function call, from the `response.event` envelope GPT-Live forwards. */
export interface LiveFunctionCall { delegation: string; response: string; callID: string; name: string; arguments: string }

export function liveFunctionCall(event: unknown): LiveFunctionCall | null {
  const e = event as { type?: string; delegation_id?: string; event?: { type?: string; response_id?: string; item?: { type?: string; call_id?: string; name?: string; arguments?: string } } }
  if (e?.type !== 'response.event' || e.event?.type !== 'response.output_item.done') return null
  const item = e.event.item
  if (item?.type !== 'function_call' || !item.call_id || !item.name) return null
  return { delegation: String(e.delegation_id ?? ''), response: String(e.event.response_id ?? ''), callID: item.call_id, name: item.name, arguments: typeof item.arguments === 'string' ? item.arguments : '{}' }
}

/** The tool's result, then the continuation the backend needs after its last pending result. */
export function liveToolReply(callID: string, output: string, last: boolean): Record<string, unknown>[] {
  const reply: Record<string, unknown>[] = [{ type: 'response.item.create', event_id: `tool_${callID}`, item: { type: 'function_call_output', call_id: callID, output } }]
  if (last) reply.push({ type: 'response.create', event_id: `continue_${callID}` })
  return reply
}

/** The backend's token usage from a forwarded `response.completed`, priced at the backend's rates. */
export function liveBackendUSD(event: unknown): number {
  const e = event as { type?: string; event?: { type?: string; response?: { usage?: { input_tokens?: number; output_tokens?: number; input_tokens_details?: { cached_tokens?: number } } } } }
  if (e?.type !== 'response.event' || e.event?.type !== 'response.completed') return 0
  const u = e.event.response?.usage
  if (!u) return 0
  const n = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) && v > 0 ? v : 0)
  const cached = n(u.input_tokens_details?.cached_tokens)
  const fresh = Math.max(0, n(u.input_tokens) - cached)
  return (fresh * LIVE_BACKEND_RATES.input + cached * LIVE_BACKEND_RATES.cached + n(u.output_tokens) * LIVE_BACKEND_RATES.output) / 1_000_000
}

/**
 * A GPT-Live call's running cost. OpenAI bills the seconds `session.usage.updated`
 * reports; between those updates the call's own elapsed time stands in, so the
 * line keeps moving and never shows less than has already been billed.
 */
export function liveCost(elapsedSeconds: number, billedSeconds: number, backendUSD: number): { seconds: number; usd: number } {
  const seconds = Math.max(elapsedSeconds, billedSeconds)
  return { seconds, usd: seconds * LIVE_USD_PER_SECOND + backendUSD }
}

/** How much of one Session's recap a spoken note carries; one append takes at most 500 tokens. */
export const LIVE_NOTE_CHARS = 400

/**
 * What the live model can say at once from a read tool's result, one note
 * per Session, while the backend goes on: the Session's name and its recap
 * (or delivery summary), with how long ago it was written.
 */
export function liveReadNotes(tool: string, output: string): string[] {
  if (tool !== 'get_session_summary' && tool !== 'get_sessions_summary') return []
  let body: unknown
  try { body = JSON.parse(output) } catch { return [] }
  const rows = tool === 'get_sessions_summary' ? (body as { sessions?: unknown })?.sessions : [body]
  if (!Array.isArray(rows)) return []
  const notes: string[] = []
  for (const row of rows) {
    const r = (row ?? {}) as Record<string, unknown>
    if (typeof r.error === 'string') continue
    const label = typeof r.label === 'string' ? r.label : ''
    const said = [r.recap, r.delivered_summary, r.last_reply].find((v): v is string => typeof v === 'string' && v.trim() !== '')
    if (!label || !said) continue
    const age = typeof r.recap_written === 'string' && r.recap === said ? ` (written ${r.recap_written})` : ''
    const text = said.length > LIVE_NOTE_CHARS ? `${said.slice(0, LIVE_NOTE_CHARS)}…` : said
    notes.push(`Tell the person about this Session now, in one or two everyday sentences in their language, then continue with the next when it comes. Session "${label}"${age}: ${text}`)
  }
  return notes
}

/** A plain note to the live model, said or kept: commentary is spoken, thinking only informs later replies. */
export function liveNote(kind: 'commentary' | 'thinking', content: string): Record<string, unknown> {
  return { type: `session.${kind}.append`, event_id: `${kind}_${crypto.randomUUID()}`, delegation_id: null, content }
}

/**
 * The opening of a call placed from one Session (asked for on 2026-11-15):
 * what the list already has about it is said at once, and the latest recap
 * follows when it arrives (`liveLatestNote`).
 */
export function liveOpeningNote(label: string, summary: string, written = ''): string {
  const clipped = summary.length > LIVE_NOTE_CHARS ? `${summary.slice(0, LIVE_NOTE_CHARS)}…` : summary
  if (!clipped) return `The call was placed from Session "${label}". In one short sentence in the person's language, say you are reading its latest summary. Do not guess its work.`
  return `The call was placed from Session "${label}". Open the call by telling the person, in two or three everyday sentences in their language and without a greeting, what this Session is doing${written ? ` (this summary was written ${written}; say so briefly)` : ''}: ${clipped}`
}

/** The latest recap of the Session the call was placed from, said after the opening. */
export function liveLatestNote(label: string, summary: string, hadOpening: boolean): string {
  const clipped = summary.length > LIVE_NOTE_CHARS ? `${summary.slice(0, LIVE_NOTE_CHARS)}…` : summary
  return hadOpening
    ? `Newer information about Session "${label}" just arrived. Continue with only what changed or is new, in one or two everyday sentences; do not repeat what you already said: ${clipped}`
    : `Here is the latest summary of Session "${label}". Tell the person in two or three everyday sentences in their language: ${clipped}`
}

/** The greeting the call opens with, as OpenAI's guide asks: once, after `session.started`. */
export const LIVE_GREETING = 'Greet the person now in one short, warm sentence in their language and say you are ready to help. Do not report Sessions or list options. Then pause and listen.'

/** Joins transcript fragments exactly as received, as the guide asks. */
export class LiveTranscript {
  private text = ''
  add(delta: unknown) { if (typeof delta === 'string') this.text += delta }
  /** What was said since the last take, trimmed; empty when nothing was. */
  take(): string { const out = this.text.trim(); this.text = ''; return out }
  peek(): string { return this.text.trim() }
}
