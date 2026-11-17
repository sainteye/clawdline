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
import { COPILOT_TOOLS, DEFAULT_SPEED, SPEEDS, briefingSection, extraSection, instructionsFor, languageRule, originSection, type CallOptions } from './copilot-tools.ts'

/** The backend model the machine opens a session with (copilot.LiveBackendModel). */
export const LIVE_BACKEND_MODEL = 'gpt-6-luna'

/** GPT-Live's price per second of a session, as OpenAI lists it ($0.05 a minute). */
export const LIVE_USD_PER_SECOND = 0.05 / 60
/** The backend model's price per 1M tokens (gpt-6-luna, standard short context). */
export const LIVE_BACKEND_RATES = { input: 0.1, cached: 0.01, output: 0.5, write: 0.125, write12h: 0.25 }

/** Qualitative pacing for the existing speed choices; Live does not promise playback multipliers. */
const LIVE_PACING = {
  'zh-Hant': {
    common: '這是目前語速設定，取代先前所有語速指示。以下指示只調整說話速度與停頓。保留原有的內容、回答長度、語言、口音與個性，不要靠刪減內容來加快，也不要靠重複內容來放慢。中文保持自然的台灣口語與清楚的聲調；英文保持自然的重音與連音。整段回答維持所選節奏，直到句尾。使用者插話時，依原有規則停下來聽。不要朗讀或解釋這些語速指示。',
    speeds: {
      '0.8': '用明顯偏慢、從容清楚的速度說話。每個詞的發音稍微放慢，按自然語意分組，在完整句子之間短暫停頓。保持語句連貫，不要逐字拆開，也不要用長時間沉默來製造慢速。',
      '0.9': '用比日常聊天稍慢的速度說話，放鬆、清楚、容易跟上。詞語之間保持連貫，只在語意轉折與句尾稍作停頓。避免拖長單字或刻意拉開每個字的間隔。',
      '1.0': '用自然日常聊天的正常速度說話，節奏流暢。按語意自然停頓，句尾俐落收束。中文不要逐字慢念；英文使用自然的連音，讓整句順暢地說完。',
      '1.1': '用明顯比正常聊天更快、但仍輕鬆清楚的速度說話。加快詞句本身的發音節奏，縮短不必要的詞間與句間停頓，讓整句連貫流暢。每個字仍須清楚，尤其是姓名、數字與關鍵資訊；不要吞字，並維持快節奏直到句尾。',
    },
  },
  en: {
    common: 'This is the current speaking pace and supersedes all earlier pacing instructions. These instructions control speaking pace and pauses only. Preserve the intended content, response length, language, accent, and personality. Do not omit content to speak faster or repeat content to speak slower. Keep Mandarin tones clear and natural, and preserve natural English stress and connected speech. Maintain the selected pace through the end of each response. Follow the existing interruption policy when the user speaks. Do not read or explain these pacing instructions.',
    speeds: {
      '0.8': 'Speak at a distinctly slow, relaxed pace. Take more time to articulate each phrase, group words naturally, and pause briefly between complete sentences. Keep speech connected rather than separating every word. Achieve the slower pace through articulation, without adding long silences.',
      '0.9': 'Speak a little more slowly than ordinary conversation, with relaxed, clear delivery. Keep words connected and allow small pauses at changes of thought and sentence endings. Avoid stretching individual words or spacing out every syllable.',
      '1.0': 'Speak at a natural, everyday conversational pace with a smooth rhythm. Pause where the meaning calls for it and finish sentences cleanly. Keep Mandarin phrases flowing rather than reading character by character, and use natural connected speech in English.',
      '1.1': 'Speak noticeably faster than ordinary conversation while staying relaxed and easy to understand. Increase the pace of articulation itself and shorten unnecessary gaps between words and sentences. Keep phrases flowing, pronounce every word clearly, especially names, numbers, and key facts, and sustain the brisk pace through the final sentence.',
    },
  },
}

export function livePacingInstructions(options: CallOptions = {}): string {
  const locale = !options.language || /^zh(?:-|$)/i.test(options.language) ? 'zh-Hant' : 'en'
  const pacing = LIVE_PACING[locale]
  const speed = SPEEDS.includes(options.speed ?? '') ? options.speed! : String(DEFAULT_SPEED)
  return ['Speaking pace:', pacing.common, pacing.speeds[speed as keyof typeof pacing.speeds]].join('\n')
}

/** Trusted, session-wide speech instructions; backend updates alone cannot change the speaking model. */
export function livePacingUpdate(options: CallOptions): Record<string, unknown> {
  return { type: 'session.instructions.append', event_id: `pace_${crypto.randomUUID()}`, delegation_id: null, content: livePacingInstructions(options) }
}

/** The spoken persona the live model starts with. Kept short: OpenAI caps it at 16,384 tokens and procedures belong to the backend. */
export function liveInstructions(options: CallOptions = {}): string {
  const brief = options.brief !== false
  return [
    `你是 Clawdline 的語音副駕駛，也是使用者的私人秘書。使用者多半在手機上，像講電話一樣問你這台電腦上各個 AI 程式開發 Session（Claude Code、Codex 等）的狀況，也會請你替他做事。

Personality: 你跟了他很久、很熟他的工作。親切、放鬆、反應快、有自己的判斷，用台灣日常口語，稱呼他「你」。不客服腔、不道歉、不誇張驚嘆。
Speaking style: 這是對話，不是報告。${brief ? '一般回答一到三句，' : ''}講完重點就停，讓他接話；他想知道更多自然會問。說法自己決定、每次換著說，不要念稿。技術用語改說它對他的影響，不念 Session ID、路徑、指令或網址。
Backchannel policy: Use moderate, brief and varied backchannels while the person is speaking.
Interruption policy: Stop speaking when the person interrupts, and listen. Answer what they just said; do not go back to finish the old explanation.
Delegation policy: 你自己不知道任何 Session 的狀況，也不能自己做任何事。凡是問到 Session、Clawdline 怎麼用、整體狀況、這通電話打了多久或花了多少錢，或要你送訊息、回答、打開、關閉、改名、開新 Session、執行 Snippet、結束或掛斷這通電話、調整並保存語速，一律先產生 <|SpawnThinking|>，等結果回來再講。Generate <|SpawnThinking|> before giving an answer that depends on backend work. 結果回來前不要說已經做好、已經送出或任何 Session 的內容；結果說要使用者確認時，照它的意思問他一句再等他回答。`,
    languageRule(options.language ?? ''),
    extraSection(options.extra ?? ''),
    livePacingInstructions(options),
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
- 明確問最近幾則回覆、表格、原因、取捨或洞見時，直接 explain_session_reply（依指定則數），不要先讀摘要或 updates。詳解可分段完整說明，不受一般簡短回覆的句數限制；不要把詳解壓回進度摘要。
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
export function liveBackendUsage(event: unknown): {id:string;usd:number;parts:Record<string,number>;input:number;output:number;cached:number} | undefined {
 const e=event as {type?:string;event?:{type?:string;response?:{id?:string;usage?:{input_tokens?:number;output_tokens?:number;input_tokens_details?:{cached_tokens?:number;cache_write_tokens?:number;cache_write_tokens_details?:{ttl_12h_tokens?:number}}}}}}
 if(e?.type!=='response.event'||e.event?.type!=='response.completed'||!e.event.response?.usage)return undefined
 const u=e.event.response.usage,n=(v:unknown)=>typeof v==='number'&&Number.isFinite(v)&&v>0?v:0
 const input=n(u.input_tokens),output=n(u.output_tokens),cached=Math.min(input,n(u.input_tokens_details?.cached_tokens))
 const written=Math.min(input-cached,n(u.input_tokens_details?.cache_write_tokens)),longWrite=Math.min(written,n(u.input_tokens_details?.cache_write_tokens_details?.ttl_12h_tokens))
 const long=input>272000
 const parts={text_input:(input-cached-written)*LIVE_BACKEND_RATES.input*(long?2:1)/1e6,cached_input:cached*LIVE_BACKEND_RATES.cached*(long?2:1)/1e6,cache_write:((written-longWrite)*LIVE_BACKEND_RATES.write+longWrite*LIVE_BACKEND_RATES.write12h)*(long?2:1)/1e6,text_output:output*LIVE_BACKEND_RATES.output*(long?1.5:1)/1e6}
 return {id:e.event.response.id??'',usd:Object.values(parts).reduce((a,b)=>a+b,0),parts,input,output,cached}
}

export function liveBackendUSD(event: unknown): number { return liveBackendUsage(event)?.usd??0 }

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
 * A recap clipped to `max` characters. A recap is told in order, from how the
 * work began to where it stands now, so its end is what is current: a call on
 * 2026-11-15 kept only the first 400 characters of a fresh recap and recounted
 * the Session's history instead of its latest state. The first sentence (what
 * the Session is about) and the end are kept.
 */
export function clipRecap(text: string, max = LIVE_NOTE_CHARS): string {
  const t = text.trim()
  if (t.length <= max) return t
  const stop = t.search(/[\u3002.!?\uff01\uff1f]/)
  const head = stop >= 0 && stop < max / 4 ? t.slice(0, stop + 1) : ''
  return `${head}…${t.slice(t.length - (max - head.length - 1))}`
}

/** The longest a call's latest state waits for its opening to be said. */
export const LIVE_OPENING_WAIT_MS = 60000

/** How much of a Session's newest reports a call's latest note carries beside its recap. */
export const LIVE_UPDATE_CHARS = 150

/** The Session's own newest reports in a `get_session_updates` result, newest last, clipped to `max`. */
export function liveUpdatesText(output: string, max = LIVE_UPDATE_CHARS): string {
  let body: { updates?: { role?: string; text?: string }[] }
  try { body = JSON.parse(output) } catch { return '' }
  const said = (Array.isArray(body?.updates) ? body.updates : [])
    .filter(u => u?.role === 'assistant' && typeof u.text === 'string' && u.text.trim() !== '')
    .slice(-2).map(u => (u.text as string).trim()).join(' ')
  return said.length > max ? `…${said.slice(said.length - max + 1)}` : said
}

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
    const text = clipRecap(said)
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
export function liveOpeningNote(label: string, summary: string, written = '', switched = false): string {
  const clipped = clipRecap(summary)
  const where = switched ? `The person switched the call to Session "${label}".` : `The call was placed from Session "${label}".`
  if (!clipped) return `${where} In one short sentence in the person's language, say you are reading its latest summary. Do not guess its work.`
  const how = switched ? 'Tell the person now, in two or three everyday sentences in their language, what this Session is doing, from this summary and nothing vaguer' : 'Open the call by telling the person, in two or three everyday sentences in their language and without a greeting, what this Session is doing'
  return `${where} ${how}${written ? ` (this summary was written ${written}; say so briefly)` : ''}: ${clipped}`
}

/**
 * The latest state of the Session the call was placed from: its fresh recap's
 * current part and its own newest reports, said once the opening has been
 * spoken (`openFromOrigin`).
 */
export function liveLatestNote(label: string, summary: string, hadOpening: boolean, updates = ''): string {
  const reports = updates ? ` Its newest reports: ${updates}` : ''
  const clipped = clipRecap(summary, LIVE_NOTE_CHARS - (updates ? updates.length : 0))
  return hadOpening
    ? `The latest state of Session "${label}" just arrived. Compare it with everything you have already told the person in this call, your earlier overview included. If it adds nothing newer, say nothing at all: no "I just got the latest", no repeat. Otherwise say only what is new, starting with a short phrase such as "I just got the latest", in one to three everyday sentences in their language: where it stands now and anything waiting for the person. Never retell its history or repeat what you already said (on 2026-11-16 the iPad failures from the overview were said a second time as news). Latest summary: ${clipped}${reports}`
    : `Here is the latest state of Session "${label}". Tell the person where it stands now and anything waiting for them, in two or three everyday sentences in their language: ${clipped}${reports}`
}

/**
 * How much of `next` is new against `said`: the share of its words (Latin
 * words and numbers, and pairs of CJK characters) that `said` does not have.
 * 0 is nothing new, 1 is all new.
 */
export function newShare(next: string, said: string): number {
  const pieces = (text: string) => {
    const t = text.toLowerCase(), out = new Set<string>()
    // A dot inside a word (2.4.0) is part of it; one that ends a sentence is not.
    for (const w of t.match(/[a-z0-9]+(?:\.[a-z0-9]+)*/g) ?? []) out.add(w)
    const cjk = t.replace(/[^\u4e00-\u9fff]/g, '')
    for (let i = 0; i + 1 < cjk.length; i++) out.add(cjk.slice(i, i + 2))
    return out
  }
  const a = pieces(next), b = pieces(said)
  if (a.size === 0) return 0
  let fresh = 0
  for (const w of a) if (!b.has(w)) fresh++
  return fresh / a.size
}

/** Below this share of new words, the latest state is not said at all (`newShare`). */
export const LIVE_NEWS_SHARE = 0.25

/**
 * The most a GPT-Live append carries, in estimated tokens; OpenAI refuses one
 * over 500 ("Context append text must not exceed 500 tokens"), and on
 * 2026-11-16 a call from the list was silent because its report went as one.
 */
export const LIVE_APPEND_TOKENS = 400

/** A cautious token count for an append: CJK characters count 1.3, other characters a third. */
export function liveTokens(text: string): number {
  const wide = (text.match(/[⺀-鿿가-힯豈-﫿＀-￯]/g) ?? []).length
  return Math.ceil(wide * 1.3 + (text.length - wide) / 3)
}

/** `text` cut into appends of at most `max` estimated tokens, at a space or line break where there is one. */
export function liveContextPieces(text: string, max = LIVE_APPEND_TOKENS): string[] {
  const pieces: string[] = []
  let rest = text.trim()
  while (rest) {
    if (liveTokens(rest) <= max) { pieces.push(rest); break }
    let end = 1
    while (end < rest.length && liveTokens(rest.slice(0, end + 1)) <= max) end++
    const space = Math.max(rest.lastIndexOf(' ', end), rest.lastIndexOf('\n', end))
    const cut = space > end / 2 ? space : end
    pieces.push(rest.slice(0, cut).trim())
    rest = rest.slice(cut).trim()
  }
  return pieces
}

/**
 * The spoken note of one Session report batch in a GPT-Live call: a short
 * instruction and each Session's name with the start of its summary, within
 * one append. The rules and the whole evidence go before it as thinking
 * (`liveContextPieces`).
 */
export function liveReportNote(sessions: { label: string; [key: string]: unknown }[]): string {
  // The fields get_sessions_summary gives a row, most telling first.
  const say = (row: Record<string, unknown>) => {
    const text = [row.recap, row.delivered, row.question, row.latest, row.goal].find((v): v is string => typeof v === 'string' && v.trim() !== '')
    return text ?? (Array.isArray(row.recent) ? row.recent.filter((v) => typeof v === 'string').join(' ') : '')
  }
  const share = Math.max(60, Math.floor((LIVE_NOTE_CHARS - 80) / Math.max(1, sessions.length)))
  const lines = sessions.map((row) => `"${row.label}": ${clipRecap(say(row), share)}`)
  const note = `Report this Session batch now, following the Session report rules and the machine evidence just added as context, in this order: ${lines.join(' / ')}`
  return liveContextPieces(note)[0] ?? note
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

/** Wait for final provider usage before releasing the transport. Microphone and
 * playback can stop immediately; this receiver is bound to the old call only. */
export const LIVE_CLOSE_WAIT_MS = 5000
export function closeLiveSession(dc: RTCDataChannel, receive:(event:Record<string,unknown>)=>void, waitMs=LIVE_CLOSE_WAIT_MS):Promise<boolean> {
 return new Promise(resolve=>{
  let done=false
  const finish=(confirmed:boolean)=>{if(done)return;done=true;clearTimeout(timer);dc.removeEventListener('message',message);dc.removeEventListener('close',closed);resolve(confirmed)}
  const message=(raw:MessageEvent)=>{try{const event=JSON.parse(raw.data);receive(event);if(event.type==='session.closed')finish(true)}catch{/* malformed provider events cannot confirm finalization */}}
  const closed=()=>finish(false)
  const timer=setTimeout(()=>finish(false),waitMs)
  dc.addEventListener('message',message);dc.addEventListener('close',closed)
  try{dc.send(JSON.stringify({type:'session.close',event_id:`close_${crypto.randomUUID()}`}))}catch{finish(false)}
 })
}
