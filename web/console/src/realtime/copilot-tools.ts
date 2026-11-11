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
// @ts-expect-error -- Node's strip-types runner loads this source in its test suite.
import { menuFingerprint } from "../session/fingerprint.ts"

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
  /** The machine's prepared briefing for this call, when it has one (`copilot-briefing`). */
  briefing?: () => Promise<string | null>
  /** Opens one Session on the page the call runs in; true once it is on screen. */
  openSession?: (id: string) => Promise<boolean>
}

export interface ToolResult {
  output: string
  logs: ToolLog[]
}

/** Realtime's output voices, the recommended first (internal/domain/copilot.Voices). */
export const VOICES = ['marin', 'cedar', 'alloy', 'ash', 'ballad', 'coral', 'echo', 'sage', 'shimmer', 'verse']

/** The playback rates the machine offers (internal/domain/copilot.Speeds), and the default. */
export const SPEEDS = ['0.8', '0.9', '1.0', '1.1']
export const DEFAULT_SPEED = 0.9

/** semantic_vad's eagerness settings, most patient first. */
export const EAGERNESS = ['low', 'auto', 'high']

/** How long the briefing tool waits for a briefing still being prepared. */
export const BRIEFING_TOOL_WAIT_MS = 25000

/** How many times a pending screen is read again, and how long apart. */
export const SCREEN_READ_ATTEMPTS = 8
export const SCREEN_READ_INTERVAL_MS = 400
/** The tail of a screen the model is given: enough for a prompt and its last output. */
export const SCREEN_TAIL_LINES = 40
/**
 * How much of a Session's last reply `explain_session_reply` hands over. A
 * diagnosis with its proposed fix and the question it ends on runs to a few
 * thousand characters; the 600 the other reads keep lose its reasoning.
 */
export const REPLY_CHARS = 8000

export const COPILOT_INSTRUCTIONS = `你是 Clawdline 的語音副駕駛。使用者在手機上用語音問你，這台電腦上各個 AI 程式開發 Session（Claude Code、Codex 等）的狀況。

【說話方式】
- 一般問答要短：最多三句，能一句講完就一句。這是語音，不是文章。
- 例外：使用者要 Session 的狀況、進度、匯報或「在做什麼」時，不受三句限制。一定要逐一講完每個相關的 Session，每個兩到三句（目標、最近完成什麼、現在在做什麼），不可以只報數量就停下來。
- 講話放慢、口氣從容：一句講完停一下再講下一句，數字和名稱講清楚，不要連珠炮。（例外：整體匯報、總結經過、解釋最後一則回覆，見下面各段。）
- 不要開場白（不說「好的」「我來查看」「請稍等」），不要重複使用者的問題，不要在結尾問「還需要什麼嗎」「要不要看畫面」。
- 呼叫工具前不要先講話，拿到結果再直接講重點。
- 不要唸 Session ID、路徑、指令、雜湊值。用 Session 的名稱稱呼它。
- 全程台灣繁體中文。

【工具】
- list_sessions：所有 Session 的整理好的狀態。結果已分成 needs_you（等使用者回應，附它在問什麼）、working（工作中，附 latest 最近一句進度）、done（已完成）、idle（閒置）；有交付摘要的附 delivered。
- get_session_summary：某個 Session 現在的摘要（最後一次交付摘要、最後一則回覆、在等什麼）。問「它現在怎樣、最後做完什麼」用這個。
- get_session_recap：某個 Session 從開始到現在的完整經過。問「這個 Session 完成了什麼、從頭到尾做了哪些、幫我總結」用這個。
- get_session_activity：某個 Session 的完整工作紀錄（使用者交代什麼、AI 回報了什麼、跑了什麼）。摘要不夠、使用者要細節或過程時才用。
- explain_session_reply：某個 Session 最後一則回覆的全文，加上使用者最後交代它的事。使用者說「解釋給我聽」「它在說什麼」「我看不懂」「它要我決定什麼」「講詳細一點」時用這個。
- get_session_screen：某個 Session 的終端畫面，只在需要看畫面細節時用。
- open_session：在使用者通話的這個畫面上打開某個 Session。使用者說「切到／打開／讓我看某某」就呼叫，打開後用一句話說「已經打開某某」，不用唸內容。
- answer_session_question：替使用者回答某個 Session 在等的問題（選項選單）。
- get_session_notes：讀 Session 留給使用者的便條紙。
- send_message_to_session：替使用者把文字送進某個 Session。送之前先唸出內容並等使用者說好。
- session_id 一律填 list_sessions 結果裡的 id，原樣照填（包含開頭的 % 符號）；還沒查過清單就先呼叫 list_sessions。遇到 error 附 candidates 時，依使用者原本說的話自己挑對的 id 重試；真的分不出來才唸出候選名稱讓使用者選。
- 通話一開始，下面【通話開始時的 Session 快照】已經附上每個 Session 的狀態與交付摘要，第一個問題可以直接依快照回答，不必先呼叫 list_sessions；使用者要「最新」狀態、問到快照沒有的細節、或通話已經過了幾分鐘，再呼叫工具。
- 工具回傳 error 就照實說讀不到與原因，不要編造。

【回答 Session 的問題】
- Session 在等使用者回答時（needs_you 或 summary 裡有 question 與 options），先用口語唸出它在問什麼和每個選項的編號與意思，然後問使用者要選哪一個。
- 只有在使用者明確說出選哪一個（例如「選 1」「選 Yes」「就第一個」）之後才呼叫 answer_session_question；使用者沒講清楚就再問一次，絕對不要自己替使用者選。按完用一句話說選了什麼；工具回報問題已經變了，就照實說並重新唸新的問題。

【便條紙】
- Session 有 notes（便條數）時，回報那個 Session 要提一句「它留了幾張便條給你」。
- 使用者問便條內容，或問有什麼要他處理時，呼叫 get_session_notes，唸出標題、要他做什麼和原因；有建議回覆就唸出選項，但不要替他回覆便條。

【整體匯報】
- 使用者要整體狀況（「匯報現在的狀況」「現在怎麼樣」「有什麼要我處理」）時：指示裡有【整理好的現況匯報】就依它講，可以照原意講得更口語、更短；還沒有就呼叫 get_status_briefing。拿不到匯報才改用 list_sessions 自己整理。
- 匯報要先講需要使用者處理的事，再講進行中的、最近完成的；這一段可以比平常長。
- 有【整理好的現況匯報】時，第一句就用它的整體結論（例如有幾件事要使用者處理），不要改用快照的 summary 數字：快照只看得出狀態，匯報才判斷了哪些事要使用者處理，兩者混用會前後矛盾。

【回報清單】
- 第一句一定先照 summary 講總數與分類，例如「五個 Session：一個在等你、兩個在工作、兩個閒置。」
- 接著依序：等你的，說名稱與它在問什麼；工作中的，每個都說名稱，再用兩到三句講：它的目標（goal）、最近完成了什麼（recent）、現在正在做什麼（latest）。名稱太長可以縮短，但不要用「第一個」「第二個」代替。已完成與閒置的只說數量，使用者問才展開。
- 使用者要「工作中的 Session 的狀況」時，這一段可以長，寧可講清楚也不要一句帶過；一個 Session 講完停一下再講下一個。

【總結一個 Session 的經過（get_session_recap）】
- 用一段自然的口語講，像同事口頭跟你交接：先一句說這個 Session 主要在做什麼，再照時間順序講它完成了哪幾件事（合併相近的，最多講五件），最後一句說現在的狀態、還有沒有要使用者處理的。
- 這一段可以比平常長，大約五到八句。像講故事一樣用「一開始…接著…後來…最後…」把事情串起來；不要說「第一、第二」或「1、2、3」，不要逐條唸清單，不要唸 commit 編號、檔名或指令。
- 依 deliveries 和 requests 講，紀錄裡沒有的事不要補。

【解說單一 Session】
- 先讀 get_session_summary，用兩到三句講：它在做什麼、做到哪（依 delivered_summary 或 last_reply 講具體結果）、現在要不要使用者處理（在等什麼）。閒置的 Session 也要說它最後完成了什麼。使用者追問過程或細節，再讀 get_session_activity。
- 依紀錄內容具體講，不要只說「在工作中」或「已閒置」。
- last_reply 的結尾如果在問使用者（要不要做、選哪個、請他確認），即使狀態是閒置，也要說它在等使用者決定，並問要不要詳細解釋。

【解釋最後一則回覆（explain_session_reply）】
- 目的是讓使用者聽完就懂狀況、知道要決定什麼。可以比平常長，大約六到十二句，用口語講，像同事當面解釋。
- 依序講：先一句說使用者原本要它處理什麼問題；再講它查到的原因，每個問題用一兩句白話講清楚「為什麼會這樣」，不要唸函式名稱、檔名、路徑、API 或程式碼；接著講它建議怎麼修；最後明確說「要你決定的是：…」，把可以選的做法和各自的代價講出來。
- 回覆裡沒有要使用者決定的事，就直接說「它沒有要你決定什麼」，並說它現在的狀態。
- 只講 reply 裡有的內容，不要補充或推測；reply_truncated 為 true 時說中間有一部分沒讀到。
- 使用者聽完說要做（例如「好，叫它修」），先唸出要送的文字並等使用者說好，再用 send_message_to_session 送出。`

export const COPILOT_TOOLS = [
  {
    type: 'function',
    name: 'get_status_briefing',
    description:
      '取得機器上的助理依輕重緩急整理好的整體現況匯報：哪些事需要使用者處理、哪些在進行、最近完成了什麼。使用者說「匯報現在的狀況」「現在怎麼樣」「有什麼要我處理」時，如果指示裡還沒有【整理好的現況匯報】，就呼叫這個；它可能要等幾秒。',
    parameters: { type: 'object', properties: {} },
  },
  {
    type: 'function',
    name: 'list_sessions',
    description:
      '查詢目前這台電腦上所有運作中或閒置的 Sessions 清單與狀態。當使用者詢問進度、有什麼任務在跑、或需要查看 session 時，必須優先主動呼叫此工具。',
    parameters: { type: 'object', properties: {} },
  },
  {
    type: 'function',
    name: 'get_session_recap',
    description:
      '讀取特定 Session 從開始到現在的完整經過：使用者陸續交代了哪些事（requests）、它每一次交付回報了什麼（deliveries，依時間排序）、以及它最後一則回覆。使用者問「這個 Session 完成了什麼」「從頭到尾做了哪些事」「幫我總結這個 Session」時用這個，然後用一段自然的口語講給使用者聽。',
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
    name: 'get_session_summary',
    description:
      '讀取特定 Session 的摘要：它最後一次交付時自己寫的回報摘要（delivered_summary）、AI 最後一則回覆（last_reply）、目前狀態，在等使用者時附上它在問什麼。回答「它做完了什麼、結果如何、現在怎樣」時優先用這個，比讀完整工作紀錄快。',
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
    name: 'explain_session_reply',
    description:
      '讀取特定 Session 最後一則回覆的全文（reply），以及使用者最後交代它的事（asked）、目前狀態與它在等什麼。使用者說「解釋給我聽」「它在說什麼」「我看不懂」「它要我決定什麼」時用這個，然後依指示講清楚原因、建議與要使用者決定的事。',
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
    name: 'open_session',
    description: '在使用者正在通話的這個畫面上打開指定的 Session，讓他直接看到它的內容。使用者說「切到某某」「打開某某」「讓我看某某」時用這個。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。' },
      },
      required: ['session_id'],
    },
  },
  {
    type: 'function',
    name: 'answer_session_question',
    description: '替使用者回答某個 Session 正在等的問題（畫面上的選項選單）。只能在你已經唸出問題和選項、而且使用者明確說出要選哪一個之後呼叫。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: 'list_sessions 回傳的 id，或 Session 名稱' },
        choice: { type: 'string', description: '使用者選的選項：編號（例如 "1"）或選項文字' },
      },
      required: ['session_id', 'choice'],
    },
  },
  {
    type: 'function',
    name: 'get_session_notes',
    description: '讀取 Session 留給使用者、還沒處理的便條紙（標題、要使用者做什麼、原因、內容、建議回覆）。不填 session_id 就讀所有有便條的 Session。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: 'list_sessions 回傳的 id，或 Session 名稱；省略時讀全部' },
      },
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
export interface CallOptions {
  /** semantic_vad's eagerness: low (default), auto, high. */
  eagerness?: string
  /** The Realtime output voice; marin unless the machine says otherwise. */
  voice?: string
  /** The playback rate as the machine keeps it ("0.9" unless set). */
  speed?: string
  /** Short answers: ordinary answers held to three sentences; on unless the machine says false. */
  brief?: boolean
  /** The person's own extra instructions from Settings › Voice copilot. */
  extra?: string
  /** The machine's prepared briefing, once it has arrived. */
  briefing?: string
  /** The language the console is shown in (`<html lang>`), which the copilot speaks. */
  language?: string
}

/** The instructions' section holding the machine's briefing, or "". */
export function briefingSection(briefing: string): string {
  const text = briefing.trim()
  if (!text) return ''
  return ['【整理好的現況匯報】', '這是機器上的助理在通話開始時，依各個 Session 的紀錄整理好的匯報。使用者要整體狀況時依它回答。', text].join('\n')
}

export function extraSection(extra: string): string {
  const text = extra.trim()
  return text ? ['【使用者的附加指示】', text].join('\n') : ''
}

/** The short-answer rule, and what replaces it when the person turns it off. */
export const BRIEF_RULE = '- 一般問答要短：最多三句，能一句講完就一句。這是語音，不是文章。'
export const LONG_RULE = '- 一般問答可以完整回答，把重點和原因講清楚；仍然不要開場白、不要重複使用者的問題。'

/** The language rule as written for a console shown in Chinese. */
export const CHINESE_RULE = '- 全程台灣繁體中文。'

/**
 * What the instructions call the console's language, or "" for Chinese,
 * whose rule stays as written. `language` is a BCP 47 tag such as "en".
 */
export function spokenLanguage(language = ''): string {
  const tag = language.trim()
  if (!tag || /^zh\b/i.test(tag)) return ''
  try {
    return new Intl.DisplayNames(['zh-Hant'], { type: 'language' }).of(tag) || tag
  } catch {
    return tag // refusal-ok: an unknown tag is named as it is; the model still reads it.
  }
}

/** The language rule for a console shown in `language`. */
export function languageRule(language = ''): string {
  const name = spokenLanguage(language)
  return name ? `- 全程用${name}（${language.trim()}）說話：這是使用者介面的語言。Session 的名稱與工具結果裡的原文照原樣唸。` : CHINESE_RULE
}

/** The instructions for one call: the short-answer rule as the machine's setting says, in the console's language. */
export function instructionsFor(brief = true, language = ''): string {
  const rules = brief ? COPILOT_INSTRUCTIONS : COPILOT_INSTRUCTIONS.replace(BRIEF_RULE, LONG_RULE)
  return rules.replace(CHINESE_RULE, languageRule(language))
}

export function sessionUpdateEvent(memory = '', snapshot = '', options: CallOptions = {}): Record<string, unknown> {
  return {
    type: 'session.update',
    session: {
      type: 'realtime',
      output_modalities: ['audio'],
      // What was said in earlier calls on this browser (`copilot-memory.ts`).
      instructions: [instructionsFor(options.brief !== false, options.language ?? ''), extraSection(options.extra ?? ''), briefingSection(options.briefing ?? ''), snapshot, memory]
        .filter(Boolean).join('\n\n'),
      audio: {
        input: {
          transcription: { model: 'whisper-1' },
          // Waits for the end of what the person means, not the first pause:
          // `server_vad` answered a mid-sentence breath. `low` is the most
          // patient of `semantic_vad`'s eagerness settings.
          turn_detection: { type: 'semantic_vad', eagerness: EAGERNESS.includes(options.eagerness ?? '') ? options.eagerness : 'low', create_response: true, interrupt_response: true },
        },
        output: {
          voice: VOICES.includes(options.voice ?? '') ? options.voice : VOICES[0],
          speed: SPEEDS.includes(options.speed ?? '') ? Number(options.speed) : DEFAULT_SPEED,
        },
      },
      tools: COPILOT_TOOLS,
      tool_choice: 'auto',
    },
  }
}

/**
 * The call's first words. With nothing remembered it introduces itself once;
 * with an earlier call remembered it picks up from there instead of saying
 * the same introduction again.
 */
export function greetingEvent(remembered = false, language = ''): Record<string, unknown> {
  const name = spokenLanguage(language) || '繁體中文'
  return {
    type: 'response.create',
    response: {
      output_modalities: ['audio'],
      instructions: remembered
        ? `這不是第一次通話。不要自我介紹、不要說你能做什麼。用一句很短的${name}打招呼；如果之前的通話裡有還沒講完或使用者在意的事，就用半句提起它。`
        : `用一句${name}問候，說你可以回報這台電腦上各個 Session 的狀況。不要超過一句。`,
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
  /** The Session's own words for its last delivery (`clawdline session report --summary`). */
  delivered?: string
  delivered_at?: number
  /** The waiting question as the machine sent it, which a press must name (`fingerprint.ts`). */
  menu?: Record<string, unknown>
  /** The numbered options of that question. */
  choices?: { n: number; label: string }[]
  /** Unresolved notes the Session's agents left for the person (`attention_count`). */
  notes?: number
  /** The conversation the notes belong to (`sessionId`). */
  conversation?: string
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
      const disposition = s.disposition as { title?: unknown; receiptAt?: unknown } | undefined
      if (disposition && str(disposition.title).trim()) {
        row.delivered = plainScreenText(str(disposition.title)).trim()
        if (typeof disposition.receiptAt === 'number') row.delivered_at = disposition.receiptAt
      }
      if (typeof s.attention_count === 'number' && s.attention_count > 0) row.notes = s.attention_count
      if (str(s.sessionId)) row.conversation = str(s.sessionId)
      const menu = s.menu as { question?: unknown; options?: unknown } | undefined
      if (menu && typeof menu === 'object') {
        row.menu = menu as Record<string, unknown>
        if (Array.isArray(menu.options)) {
          row.choices = menu.options
            .filter((o): o is { n: number; label?: unknown } => !!o && typeof o === 'object' && typeof (o as { n?: unknown }).n === 'number')
            .map((o) => ({ n: o.n, label: plainScreenText(str(o.label)) }))
        }
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
export const LIST_LATEST_CHARS = 300
export const LIST_GOAL_CHARS = 200

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

/** What a busy Session's row says about its work, from its newest transcript page. */
export interface Progress {
  latest: string
  goal?: string
  recent?: string[]
}

/** The goal (the person's newest ask), the last two delivery summaries and the newest reply of one page. */
export function progressOf(entries: unknown): Progress {
  const list = Array.isArray(entries) ? entries : []
  const out: Progress = { latest: latestOf(list) }
  const recent: string[] = []
  for (const raw of list) {
    const e = raw as Record<string, unknown> | null
    if (!e) continue
    if (e.role === 'user') {
      const words = personWords(str(e.text))
      if (words) out.goal = clip(plainScreenText(words), LIST_GOAL_CHARS)
    } else if (e.role === 'tool') {
      const m = REPORT_COMMAND.exec(str(e.text))
      const summary = m ? (m[1] ?? m[2] ?? '').replace(/\\(.)/g, '$1') : ''
      if (summary.trim()) recent.push(clip(summary, LIST_GOAL_CHARS))
    }
  }
  if (recent.length) out.recent = recent.slice(-2)
  return out
}

async function latestWords(deps: ToolDeps, row: CopilotSessionRow): Promise<Progress> {
  try {
    const res = await deps.fetch(`/v1/transcript?session=${encodeURIComponent(row.id)}`, NAMED_READ)
    if (!res.ok) return { latest: '' }
    return progressOf(((await res.json()) as { entries?: unknown })?.entries)
  } catch {
    // refusal-ok: one Session's unreadable transcript leaves its row without its progress, not the list without rows.
    return { latest: '' }
  }
}

function progressFields(p: Progress | undefined): Record<string, unknown> {
  if (!p) return {}
  return {
    ...(p.goal ? { goal: p.goal } : {}),
    ...(p.recent?.length ? { recent: p.recent } : {}),
    ...(p.latest ? { latest: p.latest } : {}),
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
  const latest = new Map<string, Progress>()
  await Promise.all(wanted.map(async (r) => latest.set(r.id, await latestWords(deps, r))))
  const brief = (r: CopilotSessionRow) => ({
    id: r.id, label: r.label, assistant: r.assistant,
    ...(r.delivered ? { delivered: clip(r.delivered, LIST_LATEST_CHARS) } : {}),
    ...(r.notes ? { notes: r.notes } : {}),
  })
  const out = {
    summary: `${rows.length} 個 Session：${needs.length} 個等你回應、${working.length} 個工作中、${done.length} 個已完成、${idle.length} 個閒置`,
    needs_you: needs.map((r) => ({
      ...brief(r),
      ...(r.question ? { question: r.question } : {}),
      ...(r.choices?.length ? { options: r.choices.map((c) => `${c.n}. ${c.label}`) } : r.options?.length ? { options: r.options } : {}),
      ...progressFields(latest.get(r.id)),
    })),
    working: working.map((r) => ({
      ...brief(r),
      ...(minutesSince(r.working_since, now) !== undefined ? { working_minutes: minutesSince(r.working_since, now) } : {}),
      ...progressFields(latest.get(r.id)),
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

/** How far back a recap reads: pages of the transcript, and what it keeps of them. */
export const RECAP_PAGES = 5
export const RECAP_REQUESTS = 12
export const RECAP_DELIVERIES = 12
export const RECAP_TEXT_CHARS = 280

// The command a Session runs to record a delivery, with its summary quoted.
const REPORT_COMMAND = /clawdline\s+session\s+report\b[^\n]*?--summary\s+(?:"((?:[^"\\]|\\.)*)"|'([^']*)')/

/** The words of a person's message, or "" for what Clawdline itself delivered into the conversation. */
function personWords(text: string): string {
  const t = text.trim()
  if (!t) return ''
  // Notices, task briefs and system wrappers arrive as user turns but are not the person.
  if (/^<(clawdline-|system-reminder|task-notification|command-|local-command)/.test(t)) return ''
  return t
}

export interface RecapRead {
  requests: { text: string; at?: number }[]
  deliveries: { text: string; at?: number }[]
  lastReply: string
  pages: number
  complete: boolean
}

/**
 * Reads a Session's conversation from its newest page back, at most
 * RECAP_PAGES pages, and keeps what the person asked for and what the
 * Session reported delivering, oldest first.
 */
export async function readRecap(deps: ToolDeps, id: string): Promise<RecapRead | { error: string; status: number }> {
  const requests: RecapRead['requests'] = []
  const deliveries: RecapRead['deliveries'] = []
  let lastReply = ''
  let before: number | undefined
  let pages = 0
  let complete = false
  while (pages < RECAP_PAGES) {
    const path = `/v1/transcript?session=${encodeURIComponent(id)}${before ? `&before=${before}` : ''}`
    const res = await deps.fetch(path, NAMED_READ)
    if (!res.ok) {
      if (pages === 0) return { error: (await refusalText(res)) || `HTTP ${res.status}`, status: res.status }
      break
    }
    const page = (await res.json()) as { entries?: unknown; nextBefore?: unknown }
    pages++
    const entries = Array.isArray(page.entries) ? page.entries : []
    const pageRequests: RecapRead['requests'] = []
    const pageDeliveries: RecapRead['deliveries'] = []
    for (const raw of entries) {
      const e = raw as Record<string, unknown> | null
      if (!e) continue
      const text = str(e.text)
      const at = Number(e.at) || undefined
      if (e.role === 'user') {
        const words = personWords(text)
        if (words) pageRequests.push({ text: clip(plainScreenText(words), RECAP_TEXT_CHARS), ...(at ? { at } : {}) })
      } else if (e.role === 'tool') {
        const m = REPORT_COMMAND.exec(text)
        const summary = m ? (m[1] ?? m[2] ?? '').replace(/\\(.)/g, '$1') : ''
        if (summary.trim()) pageDeliveries.push({ text: clip(summary, RECAP_TEXT_CHARS), ...(at ? { at } : {}) })
      }
    }
    if (pages === 1) {
      for (let i = entries.length - 1; i >= 0; i--) {
        const e = entries[i] as Record<string, unknown> | null
        if (e && e.role === 'assistant' && str(e.text).trim()) { lastReply = clip(plainScreenText(str(e.text)), ACTIVITY_TEXT_CHARS); break }
      }
    }
    // Pages arrive newest first; each page's own entries are oldest first.
    requests.unshift(...pageRequests)
    deliveries.unshift(...pageDeliveries)
    const next = Number(page.nextBefore)
    if (!next || (before !== undefined && next >= before)) { complete = true; break }
    before = next
  }
  // The newest are the ones worth keeping when there are too many; the first request is the goal.
  const keptRequests = requests.length > RECAP_REQUESTS ? [requests[0], ...requests.slice(-(RECAP_REQUESTS - 1))] : requests
  return { requests: keptRequests, deliveries: deliveries.slice(-RECAP_DELIVERIES), lastReply, pages, complete }
}

async function sessionRecap(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
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
  const recap = await readRecap(deps, row.id)
  if ('error' in recap) {
    const error = `無法讀取「${row.label}」的工作紀錄 (HTTP ${recap.status})：${recap.error}`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  logs.push({ level: 'info', text: `成功讀取「${row.label}」經過：${recap.requests.length} 個交代、${recap.deliveries.length} 次交付（讀了 ${recap.pages} 頁）` })
  return {
    session_id: row.id,
    label: row.label,
    assistant: row.assistant,
    state: row.state,
    requests: recap.requests.map((r) => r.text),
    deliveries: recap.deliveries.map((d) => d.text),
    ...(row.delivered && !recap.deliveries.some((d) => d.text === row.delivered) ? { latest_delivery: row.delivered } : {}),
    ...(recap.lastReply ? { last_reply: recap.lastReply } : {}),
    ...(row.question ? { question: row.question } : {}),
    ...(recap.complete ? {} : { note: '這個 Session 很長，只讀了最近的部分；更早的經過沒有包含在內' }),
  }
}

/** The list as the call's opening context, so the first question needs no tool. */
export async function sessionsSnapshot(deps: ToolDeps): Promise<string> {
  const logs: ToolLog[] = []
  const list = await listSessions(deps, logs)
  if (!list || typeof list !== 'object' || 'error' in (list as object)) return ''
  return snapshotSection(list as Record<string, unknown>)
}

export function snapshotSection(list: Record<string, unknown>): string {
  return [
    '【通話開始時的 Session 快照】',
    '這是接通電話時讀到的狀態（JSON，與 list_sessions 相同格式）。第一個問題可以直接依它回答；要最新狀態再呼叫工具。',
    JSON.stringify(list),
  ].join('\n')
}

async function sessionSummary(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
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
  const now = (deps.now ?? (() => Math.floor(Date.now() / 1000)))()
  let lastReply = ''
  try {
    const res = await deps.fetch(`/v1/transcript?session=${encodeURIComponent(row.id)}`, NAMED_READ)
    if (res.ok) {
      const entries = ((await res.json()) as { entries?: unknown })?.entries
      const list = Array.isArray(entries) ? entries : []
      for (let i = list.length - 1; i >= 0; i--) {
        const e = list[i] as Record<string, unknown> | null
        if (e && e.role === 'assistant' && str(e.text).trim()) { lastReply = clip(plainScreenText(str(e.text)), ACTIVITY_TEXT_CHARS); break }
      }
    }
  } catch {
    // refusal-ok: the summary still carries the Session's own delivery words and state without its transcript.
  }
  const out = {
    session_id: row.id,
    label: row.label,
    assistant: row.assistant,
    state: row.state,
    work_state: row.work_state,
    ...(row.delivered ? { delivered_summary: row.delivered } : {}),
    ...(minutesSince(row.delivered_at, now) !== undefined ? { delivered_minutes_ago: minutesSince(row.delivered_at, now) } : {}),
    ...(lastReply ? { last_reply: lastReply } : {}),
    ...(minutesSince(row.working_since, now) !== undefined && row.state === 'working' ? { working_minutes: minutesSince(row.working_since, now) } : {}),
    ...(row.question ? { question: row.question } : {}),
    ...(row.choices?.length ? { options: row.choices.map((c) => `${c.n}. ${c.label}`) } : row.options?.length ? { options: row.options } : {}),
    ...(row.notes ? { notes: row.notes } : {}),
    ...(!row.delivered && !lastReply ? { note: '這個 Session 沒有交付摘要也讀不到回覆，可改用 get_session_screen 看畫面' } : {}),
  }
  logs.push({ level: 'info', text: `成功讀取「${row.label}」摘要${row.delivered ? '（含交付摘要）' : ''}` })
  return out
}

async function explainReply(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
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
    const error = `無法讀取「${row.label}」的回覆 (HTTP ${res.status})${why ? `：${why}` : ''}`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  const data = (await res.json()) as { entries?: unknown }
  const list = Array.isArray(data?.entries) ? data.entries : []
  let reply = ''
  let replyAt = -1
  for (let i = list.length - 1; i >= 0; i--) {
    const e = list[i] as Record<string, unknown> | null
    if (e && e.role === 'assistant' && str(e.text).trim()) { reply = plainScreenText(str(e.text)).trim(); replyAt = i; break }
  }
  let asked = ''
  for (let i = (replyAt >= 0 ? replyAt : list.length) - 1; i >= 0; i--) {
    const e = list[i] as Record<string, unknown> | null
    const words = e && e.role === 'user' ? personWords(str(e.text)) : ''
    if (words) { asked = clip(plainScreenText(words), ACTIVITY_TEXT_CHARS); break }
  }
  if (!reply) {
    logs.push({ level: 'error', text: `「${row.label}」讀不到回覆` })
    return { session_id: row.id, label: row.label, state: row.state, note: '這個 Session 讀不到最後一則回覆，可改用 get_session_screen 看畫面' }
  }
  const truncated = reply.length > REPLY_CHARS
  logs.push({ level: 'info', text: `成功讀取「${row.label}」最後一則回覆（${reply.length} 字）` })
  return {
    session_id: row.id,
    label: row.label,
    assistant: row.assistant,
    state: row.state,
    ...(asked ? { asked } : {}),
    reply: truncated ? clipKeepingLines(reply, REPLY_CHARS) : reply,
    ...(truncated ? { reply_truncated: true } : {}),
    ...(row.question ? { question: row.question } : {}),
    ...(row.options?.length ? { options: row.options } : {}),
    ...(row.delivered ? { delivered_summary: row.delivered } : {}),
  }
}

/** Like `clip`, but a reply's line breaks are its structure, so they are kept. */
function clipKeepingLines(text: string, n: number): string {
  if (text.length <= n) return text
  const head = Math.ceil(n / 2)
  return text.slice(0, head) + '\n…\n' + text.slice(text.length - (n - head))
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

async function openSessionTool(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
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
  if (!deps.openSession) return { error: '這個畫面不能由副駕駛切換 Session' }
  const opened = await deps.openSession(row.id)
  if (!opened) {
    const error = `「${row.label}」沒有在這個畫面上打開（這個畫面目前讀不到它）`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  logs.push({ level: 'info', text: `已在畫面上打開「${row.label}」` })
  return { opened: true, label: row.label }
}

/** The option a spoken choice names: its number, or its label. */
export function choiceOf(choices: { n: number; label: string }[], spoken: string): { n: number; label: string } | { error: string } {
  const w = spoken.trim().replace(/^第\s*/, '').replace(/\s*個$/, '')
  const digits = /^\d+$/.test(w) ? Number(w) : ({ '一': 1, '二': 2, '三': 3, '四': 4, '五': 5 } as Record<string, number>)[w]
  if (digits) {
    const byNumber = choices.find((c) => c.n === digits)
    if (byNumber) return byNumber
  }
  const lower = w.toLowerCase()
  const exact = choices.filter((c) => c.label.toLowerCase() === lower)
  if (exact.length === 1) return exact[0]
  const partial = choices.filter((c) => lower && c.label.toLowerCase().includes(lower))
  if (partial.length === 1) return partial[0]
  return { error: `「${spoken}」對不到唯一的選項；選項是：${choices.map((c) => `${c.n}. ${c.label}`).join('、')}` }
}

async function answerQuestion(deps: ToolDeps, logs: ToolLog[], wanted: string, spoken: string): Promise<unknown> {
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
  if (!row.menu || !row.choices?.length) {
    logs.push({ level: 'error', text: `「${row.label}」現在沒有在等問題的答案` })
    return { error: `「${row.label}」現在沒有在等問題的答案` }
  }
  const choice = choiceOf(row.choices, spoken)
  if ('error' in choice) {
    logs.push({ level: 'error', text: choice.error })
    return { error: choice.error }
  }
  // The press names the question it answers; the machine refuses it if the
  // screen now asks something else.
  const expect = menuFingerprint(row.menu as unknown as Parameters<typeof menuFingerprint>[0])
  const res = await deps.fetch(`/v1/sessions/${encodeURIComponent(row.id)}/key`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': (deps.newRequestID ?? (() => crypto.randomUUID()))(), [UNOPENED_READ_HEADER]: '1' },
    body: JSON.stringify({ key: String(choice.n), expect }),
  })
  if (!res.ok) {
    const why = await refusalText(res)
    const error = `沒有替「${row.label}」選「${choice.label}」（HTTP ${res.status}${why ? `：${why}` : ''}）；它的問題可能已經變了，請重新查看`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  logs.push({ level: 'info', text: `已替「${row.label}」選了 ${choice.n}. ${choice.label}` })
  return { answered: true, label: row.label, question: row.question ?? '', chose: `${choice.n}. ${choice.label}` }
}

/** How many Sessions' notes one ask reads when none is named. */
export const NOTES_SESSIONS = 6

async function sessionNotes(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  let rows: CopilotSessionRow[]
  if (wanted.trim()) {
    const found = resolveSession(read.rows, wanted)
    if ('error' in found) {
      logs.push({ level: 'error', text: found.error })
      return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
    }
    rows = [found.row]
  } else {
    rows = read.rows.filter((r) => r.notes).slice(0, NOTES_SESSIONS)
  }
  const out: { session_id: string; label: string; notes: unknown[] }[] = []
  for (const row of rows) {
    if (!row.conversation) continue
    const res = await deps.fetch(`/v1/work/v2/human-interventions/${encodeURIComponent(`conversation:${row.conversation}`)}`, NAMED_READ)
    if (!res.ok) continue
    const body = (await res.json()) as { rows?: unknown }
    const notes = (Array.isArray(body.rows) ? body.rows : [])
      .filter((n): n is Record<string, unknown> => !!n && typeof n === 'object')
      .filter((n) => n.resolved_at == null && n.withdrawn !== true && n.kind !== 'secret')
      .map((n) => ({
        kind: str(n.kind),
        from: str(n.source_label),
        title: str(n.title),
        summary: str(n.summary),
        action: str(n.action),
        reason: str(n.reason),
        ...(str(n.detail) ? { detail: clip(plainScreenText(str(n.detail)), ACTIVITY_TEXT_CHARS) } : {}),
        ...(Array.isArray(n.options) && n.options.length
          ? { replies: n.options.map((o) => (o && typeof o === 'object' ? str((o as { label?: unknown }).label) : '')).filter(Boolean) } : {}),
      }))
    if (notes.length) out.push({ session_id: row.id, label: row.label, notes })
  }
  const count = out.reduce((n, s) => n + s.notes.length, 0)
  logs.push({ level: 'info', text: `讀到 ${count} 張還沒處理的便條` })
  return count ? { sessions: out } : { note: wanted.trim() ? '這個 Session 沒有還沒處理的便條' : '沒有 Session 留下還沒處理的便條' }
}

async function statusBriefing(deps: ToolDeps, logs: ToolLog[]): Promise<unknown> {
  if (!deps.briefing) {
    logs.push({ level: 'error', text: '這通電話沒有機器整理的匯報' })
    return { error: '這通電話沒有機器整理的匯報，請改用 list_sessions 自己整理' }
  }
  const text = await Promise.race([deps.briefing(),
    new Promise<null>((resolve) => setTimeout(() => resolve(null), BRIEFING_TOOL_WAIT_MS))])
  if (!text) {
    logs.push({ level: 'error', text: '機器的匯報還沒整理好' })
    return { error: '機器的匯報還沒整理好或整理失敗，請改用 list_sessions 自己整理' }
  }
  logs.push({ level: 'info', text: '已取得機器整理好的匯報' })
  return { briefing: text }
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
    if (name === 'get_status_briefing') result = await statusBriefing(deps, logs)
    else if (name === 'list_sessions') result = await listSessions(deps, logs)
    else if (name === 'get_session_recap') result = await sessionRecap(deps, logs, str(args.session_id))
    else if (name === 'get_session_summary') result = await sessionSummary(deps, logs, str(args.session_id))
    else if (name === 'get_session_activity') result = await sessionActivity(deps, logs, str(args.session_id))
    else if (name === 'explain_session_reply') result = await explainReply(deps, logs, str(args.session_id))
    else if (name === 'get_session_screen') result = await sessionScreen(deps, logs, str(args.session_id))
    else if (name === 'open_session') result = await openSessionTool(deps, logs, str(args.session_id))
    else if (name === 'answer_session_question') result = await answerQuestion(deps, logs, str(args.session_id), str(args.choice))
    else if (name === 'get_session_notes') result = await sessionNotes(deps, logs, str(args.session_id))
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
