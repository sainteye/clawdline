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
  /** The machines a new Session can be started on, as the page knows them. */
  machines?: () => Promise<{ id: string; name: string; online: boolean; current: boolean }[]>
  /** The Session this call is talking about: the last one a tool read, opened or acted on. */
  focus?: { get(): string | undefined; set(id: string): void }
  /**
   * The Decision API: which Session the person's own words point at, judged
   * by a text model on the machine from the whole list. Asked only when a read
   * names no Session by its id or whole name (`resolveRead`).
   */
  decide?: (said: string, sessions: DecisionSession[]) => Promise<SessionDecision>
  /** The person's own words for the Session in the current tool call (`said`). */
  said?: string
  /** What the person last said in the call, as transcribed: the words the Decision API judges first. */
  heard?: () => string
  /**
   * Opens a Session on the page the call runs in and its Snippet / Playbook
   * sheet, optionally starting a Playbook there (`session/snippets-request.ts`).
   * Resolves with what the sheet made of it.
   */
  snippets?: (sessionID: string, request: { tab: 'snippet' | 'playbook'; playbook?: string; start?: boolean }) => Promise<string>
  /** This call's speed and answer length now, and a way to change them for this call and the next. */
  delivery?: () => { speed: string; brief: boolean }
  adjust?: (change: { speed?: string; brief?: boolean }) => Promise<{ saved: boolean }>
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

export const COPILOT_INSTRUCTIONS = `你是 Clawdline 的語音副駕駛，也是使用者的私人秘書。使用者在手機上用語音問你，這台電腦上各個 AI 程式開發 Session（Claude Code、Codex 等）的狀況。

【最重要的三條】
- 回答問題的第一個字就是答案本身：絕對不要用「好」「好的」「我來說明」「我來跟你說」開頭。
- 講多個 Session 時用它的簡稱帶出每一個，絕對不要說「第一個是」「第二個是」。
- 最後一個重點講完就停：絕對不要加「這就是目前的狀況」「以上」「這兩個都在進行中」這類收尾。

【語氣：像秘書當面跟你報告】
- 像一位熟悉使用者工作的私人秘書，當面口頭跟他報告：親切、從容、有條理，用日常口語（說中文時用台灣口語），稱呼他「你」。
- 用完整的口語句子和自然的連接詞串起來（「另外」「還有一件比較急的」「這個不用擔心」「我看了一下」），不要像在唸清單、報表或公文。
- 替他分輕重：要他處理的先講並說一句為什麼；一切正常的就直接說「這個沒問題」，不必細講。
- Session 名稱很長時，用他聽得懂的簡稱，不要唸方括號或斜線。
- 只有他要你去做一件事（送訊息、打開、關閉、開新 Session）時，才先用一句很短的口語回應（例如「好，我幫你送過去」）。他是在問問題時，不要說「好」「好的，我來說明」，第一句直接就是答案。
- 講到某個 Session 的內容，一律來自快照或工具結果；快照裡沒有它的細節（例如閒置、已完成的只有數量），就先呼叫 get_session_summary 再講，絕對不要自己編。
- 不要客服腔、不要過度客氣或道歉、不要誇張的情緒或驚嘆。

【說話方式】
- 一般問答要短：最多三句，能一句講完就一句。這是語音，不是文章。
- 例外：使用者要 Session 的狀況、進度、匯報或「在做什麼」時，不受三句限制。一定要逐一講完每個相關的 Session，每個兩到三句（目標、最近完成什麼、現在在做什麼），不可以只報數量就停下來。
- 講話放慢、口氣從容：句子之間自然換氣，數字和名稱講清楚，不要連珠炮；換氣不是講完，要講的內容要在同一次回答裡講完。（例外：整體匯報、總結經過、解釋最後一則回覆，見下面各段。）
- 第一句就是內容，最後一個重點講完就停。內容的份量照舊，只省掉頭尾的客套。
- 不要開場白：不說「好的」「我來查看」「請稍等」「我來幫你整理一下」「以下是目前的狀況」「根據最新的資料」，也不要重複使用者的問題。
- 不要收尾：不說「以上就是…」「總結來說」（除非真的在做總結）「如果需要更多細節再告訴我」「還需要什麼嗎」「要不要看畫面」「希望對你有幫助」。只有真的需要使用者決定一件事時，才在最後問那一件事。
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
- open_session：在使用者通話的這個畫面上打開某個 Session。使用者說「切到／打開／讓我看某某」就呼叫。打開後結果附 status（它的交代、交付與最後回覆）：先說「已經打開某某」，接著用三到五句講它的狀況：它做了哪些事、還有什麼沒完成、有沒有在等使用者決定。
- answer_session_question：替使用者回答某個 Session 在等的問題（選項選單）。
- get_session_notes：讀 Session 留給使用者的便條紙。
- list_snippets、open_snippets、run_snippet：Session 的 Snippet（存好的常用文字）與 Playbook（存好的工作流程）。使用者要「打開 snippet／playbook 列表」就呼叫 open_snippets；要你「執行某某」就先用 list_snippets 找出它的完整名稱（依使用者說的意思自己判斷是哪一個，找不到就說找不到並唸兩三個最接近的），再講「要在『Session 名稱』執行『名稱』，對嗎？」，使用者說好才呼叫 run_snippet；使用者一句話已經講清楚 Session 和名稱的，照只確認一次的規則直接執行。
- set_speaking_speed、set_answer_length：使用者要你講快／慢一點、簡短／詳細一點時，直接呼叫，不必再確認；調完用一句話說「好，我會講慢一點」之類，然後照新的方式講。
- close_session：使用者要你關閉某個 Session 時，先用名稱問一次「確定要關閉某某嗎？」，得到肯定回答才呼叫。機器說它還不能安全關閉（例如還在工作、還有交付沒落地）時，照實說原因，再問一次「它還不能安全關閉，還是要關閉嗎？」；使用者明確說要，才帶 force_confirmed: true 再呼叫一次，沒說要就不關。
- send_message_to_session：替使用者把文字送進某個 Session。
【開新的 Session】
- 使用者要開新 Session 時，第一步一定先呼叫 list_machines，接著呼叫 list_projects，然後才開口問。機器、專案、助理、模型的名稱只能用這兩個工具回傳的，絕對不要自己編名稱或舉不存在的例子。
- 要確定四件事：哪台機器、哪個專案、用哪個助理、用哪個模型。缺哪一項就問哪一項，一次只問一項，不要自己猜。
- 機器：list_machines 只有一台就直接用它、不要問；有兩台以上而使用者沒說，就問要開在哪一台（只列在線的）。
- 專案：呼叫 list_projects，依使用者說的名稱找；找不到或有好幾個像的，就唸兩三個最接近的名稱請他選。
- 助理：使用者沒說就問要用 Claude Code、Codex、Gemini 還是 Grok（只列 list_projects 說有安裝的）。
- 模型：使用者沒說就問「要用預設的某某模型嗎？」；說好就不填 model。
- 四件事都確定後，先講「要在『機器』的『專案』用『助理（模型）』開一個新的 Session，對嗎？」，使用者說好才呼叫 start_session。開好後說一句已經開好，並問要不要切過去看（open_session）。

【送出、回答、關閉之前：一定要對準正在討論的那個 Session】
- 這三個動作只能用 list_sessions 的 id 或完整名稱，並且在 session_label 填你跟使用者講的那個 Session 名稱。
- 預設對象就是使用者目前正在跟你討論的那個 Session；不要因為名稱相近或助理種類相同就換成別的。
- 使用者說的 Session 在清單裡找不到（例如已經關掉了），就直接說「找不到某某，它可能已經關閉了」，絕對不要改挑另一個 Session 來送。
- 送出前先講「要送到『Session 名稱』：『內容』，對嗎？」，使用者說好才送。但同一件事只確認一次：使用者已經對這個 Session 和這段內容說過好（包括你前面問過、他答應了），或他自己一句話就講明了送到哪個 Session、送什麼（他一句話裡就講了是哪個 Session 和要送的話），就直接送，不要再問。只有對象或內容是你自己推測的、或跟他確認過的不一樣時，才再問一次。回答問題、關閉也一樣：已經確認過就直接做。
- 工具說「目前在討論的不是這個」時，要先問使用者是不是要改到那個 Session，確認後才帶 switch_confirmed: true 再呼叫。
- 查詢類工具（get_session_summary、get_session_recap、get_session_activity、explain_session_reply、get_session_screen、open_session、get_session_notes）的 session_id，填清單裡那個 Session 的完整名稱（label），一字不差照抄；名稱比 id 不容易填錯。送出、回答、關閉則填清單裡的 id，原樣照填（包含開頭的 % 符號）。還沒查過清單就先呼叫 list_sessions。
- 使用者問到某個 Session 的內容、進度、結果時，一定先呼叫查詢工具再回答，即使快照裡有它的名稱也一樣；快照只用來回答整體有哪些 Session。講某個 Session 的內容時，再用工具結果裡的 label 簡稱說出是哪一個，讓使用者聽得出你查的是不是他要的。使用者用描述的方式說 Session（講它的主題或在做的事，而不是完整名稱）時，查詢類工具的 said 照填他稱呼它的原話（session_id 填你認為的那個），工具會依他的原話判斷是哪一個；遇到 error 附 candidates 時，同樣依使用者原本說的話自己挑對的 id 重試；真的分不出來才唸出兩三個候選名稱讓使用者選。
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
- 第一句一定先照 summary 講總數與分類，例如「五個 Session：一個在等你、兩個在工作、兩個閒置。」數量是 0 的分類不要講。
- 接著依序：等你的，說名稱與它在問什麼；工作中的，每個都說名稱，再用兩到三句講：它的目標（goal）、最近完成了什麼（recent）、現在正在做什麼（latest）。名稱太長可以縮短，但不要用「第一個」「第二個」代替。已完成與閒置的只說數量，使用者問才展開。
- 使用者要「工作中的 Session 的狀況」時，這一段可以長，寧可講清楚也不要一句帶過；一個 Session 講完換個氣，直接接著講下一個。
- 總數那一句之後一定要在同一次回答裡接著講每個 Session，絕對不可以講完總數就結束。最後一個 Session 講完就停，不要加「這是目前的狀態」「以上」「大概是這樣」。

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
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: {
          type: 'string',
          description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。',
        },
      },
      required: ['session_id', 'said'],
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
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: {
          type: 'string',
          description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。',
        },
      },
      required: ['session_id', 'said'],
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
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: {
          type: 'string',
          description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。',
        },
      },
      required: ['session_id', 'said'],
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
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: {
          type: 'string',
          description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。',
        },
      },
      required: ['session_id', 'said'],
    },
  },
  {
    type: 'function',
    name: 'get_session_screen',
    description: '讀取特定 Session 當前的終端機螢幕文字輸出，以供摘要其執行進度、最新輸出與狀態。',
    parameters: {
      type: 'object',
      properties: {
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: {
          type: 'string',
          description: 'list_sessions 回傳的 id；也可以填 Session 名稱或助理種類（例如 codex），會自動對應。',
        },
      },
      required: ['session_id', 'said'],
    },
  },
  {
    type: 'function',
    name: 'open_session',
    description: '在使用者正在通話的這個畫面上打開指定的 Session，讓他直接看到它的內容。使用者說「切到某某」「打開某某」「讓我看某某」時用這個。',
    parameters: {
      type: 'object',
      properties: {
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: { type: 'string', description: '清單裡那個 Session 的完整名稱（label），一字不差照抄；或它的 id' },
      },
      required: ['session_id', 'said'],
    },
  },
  {
    type: 'function',
    name: 'answer_session_question',
    description: '替使用者回答某個 Session 正在等的問題（畫面上的選項選單）。只能在你已經唸出問題和選項、而且使用者明確說出要選哪一個之後呼叫。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: '清單裡那個 Session 的完整名稱（label），一字不差照抄；或它的 id' },
        choice: { type: 'string', description: '使用者選的選項：編號（例如 "1"）或選項文字' },
        session_label: { type: 'string', description: '你跟使用者講的這個 Session 的名稱，必須就是這個 id 的 Session' },
        switch_confirmed: { type: 'boolean', description: '要動的不是目前正在討論的 Session，而使用者已明確確認要改到這個時才填 true' },
      },
      required: ['session_id', 'choice', 'session_label'],
    },
  },
  {
    type: 'function',
    name: 'get_session_notes',
    description: '讀取 Session 留給使用者、還沒處理的便條紙（標題、要使用者做什麼、原因、內容、建議回覆）。不填 session_id 就讀所有有便條的 Session。',
    parameters: {
      type: 'object',
      required: ['said'],
      properties: {
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: { type: 'string', description: 'list_sessions 回傳的 id，或 Session 名稱；省略時讀全部' },
      },
    },
  },
  {
    type: 'function',
    name: 'list_snippets',
    description: '列出某個 Session 能用的 Snippet（存好的常用文字）與 Playbook（存好的工作流程），各附名稱與開頭一小段內容。使用者問有哪些 snippet／playbook、或要執行一個卻沒說完整名稱時先呼叫。',
    parameters: {
      type: 'object',
      properties: {
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: { type: 'string', description: '清單裡那個 Session 的完整名稱（label），一字不差照抄；或它的 id' },
      },
      required: ['session_id', 'said'],
    },
  },
  {
    type: 'function',
    name: 'open_snippets',
    description: '在使用者通話的這個畫面上打開某個 Session，並打開它的 Snippet 或 Playbook 列表。使用者說「打開 snippet 列表」「讓我看 playbook」時呼叫。',
    parameters: {
      type: 'object',
      properties: {
        said: { type: 'string', description: '使用者這句話裡稱呼這個 Session 的原話，照他說的填（例如他講的主題、或聽起來像的名字）；他只說「它」「這個」就填那個字' },
        session_id: { type: 'string', description: '清單裡那個 Session 的完整名稱（label），一字不差照抄；或它的 id' },
        kind: { type: 'string', enum: ['snippet', 'playbook'], description: '要打開哪一個列表' },
      },
      required: ['session_id', 'kind', 'said'],
    },
  },
  {
    type: 'function',
    name: 'run_snippet',
    description: '在某個 Session 執行一個 Snippet 或 Playbook：Snippet 是把它的文字送進那個 Session；Playbook 是在這個畫面打開它並按下開始。只在使用者明確要你執行、而且確認過 Session 與名稱之後呼叫。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: 'list_sessions 回傳的 id（含開頭的 % 符號）或 Session 完整名稱' },
        session_label: { type: 'string', description: '你跟使用者講的這個 Session 的名稱，必須就是這個 id 的 Session' },
        name: { type: 'string', description: 'list_snippets 回傳的 Snippet 或 Playbook 完整名稱，一字不差照抄' },
        switch_confirmed: { type: 'boolean', description: '要動的不是目前正在討論的 Session，而使用者已明確確認要改到這個時才填 true' },
      },
      required: ['session_id', 'session_label', 'name'],
    },
  },
  {
    type: 'function',
    name: 'set_speaking_speed',
    description: '調整你講話的速度，這通電話馬上生效，也存成下次通話的設定。使用者說「講慢一點」「快一點」「恢復正常速度」時呼叫。',
    parameters: {
      type: 'object',
      properties: {
        speed: { type: 'string', enum: ['slower', 'faster', 'normal', '0.8', '0.9', '1.0', '1.1'], description: 'slower／faster 往下或往上調一格；normal 是 1.0；也可以直接給數值' },
      },
      required: ['speed'],
    },
  },
  {
    type: 'function',
    name: 'set_answer_length',
    description: '切換簡短回答：brief 為 true 時一般問答最多三句，false 時完整回答。這通電話馬上生效，也存成下次通話的設定。使用者說「簡短一點」「講詳細一點」時呼叫。',
    parameters: {
      type: 'object',
      properties: { brief: { type: 'boolean', description: 'true 簡短、false 詳細' } },
      required: ['brief'],
    },
  },
  {
    type: 'function',
    name: 'close_session',
    description: '關閉一個 Session。只在使用者明確要求關閉、而且你已經問過「確定要關閉某某嗎」並得到肯定回答之後呼叫。機器判斷還不能安全關閉時不會關，會回報原因；使用者聽過原因、仍明確說要關閉時，帶 force_confirmed: true 再呼叫一次。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: 'list_sessions 回傳的 id，或 Session 完整名稱' },
        session_label: { type: 'string', description: '你跟使用者講的這個 Session 的名稱，必須就是這個 id 的 Session' },
        switch_confirmed: { type: 'boolean', description: '要動的不是目前正在討論的 Session，而使用者已明確確認要改到這個時才填 true' },
        force_confirmed: { type: 'boolean', description: '你已經把「還不能安全關閉」的原因告訴使用者，而他明確說還是要關閉時才填 true' },
      },
      required: ['session_id', 'session_label'],
    },
  },
  {
    type: 'function',
    name: 'list_machines',
    description: '列出可以開新 Session 的機器（名稱、在不在線、是不是目前這個畫面讀的那台）。使用者要開新 Session、但沒說哪台機器時先呼叫。',
    parameters: { type: 'object', properties: {} },
  },
  {
    type: 'function',
    name: 'list_projects',
    description: '列出某台機器上的專案（名稱與 id）、已安裝的助理，以及各助理的預設模型與可選模型。開新 Session 前用來確認專案、助理和模型。',
    parameters: {
      type: 'object',
      properties: { machine_id: { type: 'string', description: 'list_machines 回傳的 id；省略就是目前這台' } },
    },
  },
  {
    type: 'function',
    name: 'start_session',
    description: '在指定機器的指定專案開一個新的 AI 程式開發 Session。只有在機器、專案、助理都確定、模型也確定（或使用者同意用預設），而且你已經念出完整內容、使用者說好之後才呼叫。',
    parameters: {
      type: 'object',
      properties: {
        project_id: { type: 'string', description: 'list_projects 回傳的專案 id' },
        project_label: { type: 'string', description: '你跟使用者講的專案名稱，必須就是這個 id 的專案' },
        assistant: { type: 'string', description: 'list_projects 回傳的助理 id，例如 claude、codex、antigravity、grok' },
        model: { type: 'string', description: 'list_projects 回傳的模型 value；用預設模型就省略' },
        machine_id: { type: 'string', description: 'list_machines 回傳的機器 id；省略就是目前這台' },
      },
      required: ['project_id', 'project_label', 'assistant'],
    },
  },
  {
    type: 'function',
    name: 'send_message_to_session',
    description: '將使用者確認過的文字指令或訊息直接鍵入指定 Session 的終端機輸入框。',
    parameters: {
      type: 'object',
      properties: {
        session_id: { type: 'string', description: '清單裡那個 Session 的完整名稱（label），一字不差照抄；或它的 id' },
        message: { type: 'string', description: '要傳送給該 Session 的文字內容' },
        session_label: { type: 'string', description: '你跟使用者講的這個 Session 的名稱，必須就是這個 id 的 Session' },
        switch_confirmed: { type: 'boolean', description: '要動的不是目前正在討論的 Session，而使用者已明確確認要改到這個時才填 true' },
      },
      required: ['session_id', 'message', 'session_label'],
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
        ? `這不是第一次通話。不要自我介紹、不要說你能做什麼。像熟悉他的秘書一樣，用一句很短、自然的${name}打招呼；如果之前的通話裡有還沒講完或使用者在意的事，就用半句提起它。`
        : `像私人秘書一樣，用一句自然的${name}問候，說你可以幫他看這台電腦上各個 Session 的狀況。不要超過一句。`,
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
  /** Whether the machine says the Session can be closed now, and the reading that says so. */
  closeable?: string
  closeVersion?: string
  closeReasons?: string[]
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
      const close = s.closeability as { state?: unknown; version?: unknown; reasons?: unknown } | undefined
      if (close && typeof close === 'object') {
        row.closeable = str(close.state)
        if (str(close.version)) row.closeVersion = str(close.version)
        if (Array.isArray(close.reasons)) {
          row.closeReasons = close.reasons.map((r) => (r && typeof r === 'object' ? str((r as { code?: unknown }).code) : str(r))).filter(Boolean)
        }
      }
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
): { row: CopilotSessionRow } | { error: string; candidates?: { id: string; label: string; state?: string }[] } {
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
  const exact = rows.filter((r) => r.label.toLowerCase() === lower)
  if (exact.length === 1) return { row: exact[0] }
  // Anything else is the copilot's to match, not this code's: it hears
  // "TimeTree 相關的" for "[Task] Foyer 每日 TimeTree 日程整理", which no
  // substring rule does. It gets every Session and picks an id itself.
  return {
    error: rows.length
      ? `沒有 id 或名稱剛好是「${w}」的 Session。從下面的 Sessions 依使用者的意思挑出 id 再查；真的沒有相符的才說找不到`
      : `目前沒有任何 Session`,
    candidates: rows.map((r) => ({ id: r.id, label: r.label, state: r.state })),
  }
}

/** One Session as the Decision API is shown it. */
export interface DecisionSession { id: string; label: string; state: string; assistant: string; delivered?: string; question?: string }

/** What the Decision API decided: one id, or the few it could not tell apart. */
export interface SessionDecision { id?: string; candidates?: string[] }

/**
 * A read's Session. An id or a whole name is taken as it is; any other words
 * go to the Decision API with every Session, and only an id it names from the
 * list is used. Without a decision the copilot gets every Session to choose
 * from, as before (`resolveSession`). Writes never come here (`resolveForWrite`).
 */
export async function resolveRead(
  deps: Pick<ToolDeps, 'decide' | 'said' | 'heard'>,
  rows: readonly CopilotSessionRow[],
  wanted: string,
): Promise<ReturnType<typeof resolveSession>> {
  const direct = resolveSession(rows, wanted)
  // The person's words decide when there are any: measured live, the voice
  // model filled in a wrong id itself (「做衣服圖片的那個」 read as another
  // Session), and asked for the person's words it handed over its own pick
  // of a label instead. So the transcribed sentence comes first.
  const asked = (deps.heard?.() ?? '').trim() || (deps.said ?? '').trim() || ('row' in direct ? '' : wanted.trim())
  if (!asked || !deps.decide || rows.length === 0) return direct
  let decided: SessionDecision
  try {
    decided = await deps.decide(asked, rows.map((r) => ({
      id: r.id, label: r.label, state: r.state, assistant: r.assistant,
      ...(r.delivered ? { delivered: clip(r.delivered, LIST_GOAL_CHARS) } : {}),
      ...(r.question ? { question: clip(r.question, LIST_GOAL_CHARS) } : {}),
    })))
  } catch {
    // refusal-ok: without a decision the copilot still gets every Session and chooses itself.
    return direct
  }
  const near = (decided.candidates ?? []).map((id) => rows.find((r) => r.id === id)).filter((r): r is CopilotSessionRow => !!r)
  // A lone candidate is the decision: asking "is it this one?" about the only
  // match was said to the person as "not found".
  const row = (decided.id ? rows.find((r) => r.id === decided.id) : undefined) ?? (near.length === 1 ? near[0] : undefined)
  if (row) return { row }
  // Words that point nowhere ("它") leave the copilot's own choice standing.
  if ('row' in direct && (near.length === 0 || near.includes(direct.row))) return direct
  if (near.length) {
    return {
      error: `「${wanted}」可能是：${near.map((r) => r.label).join('、')}。唸出這幾個名稱，問使用者是哪一個`,
      candidates: near.map((r) => ({ id: r.id, label: r.label, state: r.state })),
    }
  }
  return direct
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

/**
 * When something happened, as a person says it: 剛剛, 12 分鐘前, 3 小時前,
 * 昨天 18:20, 11/9 14:30. Prepared here so the copilot never reads out a
 * count of minutes (2026-11-12: "1440 分鐘").
 */
export function whenPhrase(at: number | undefined, now: number, zone?: string): string | undefined {
  if (!at || !now || at > now + 60) return undefined
  const seconds = Math.max(0, now - at)
  if (seconds < 90) return '剛剛'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} 分鐘前`
  const parts = (t: number) => {
    const f = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
    const o = Object.fromEntries(f.formatToParts(new Date(t * 1000)).map((p) => [p.type, p.value]))
    return { day: `${o.year}-${o.month}-${o.day}`, month: Number(o.month), date: Number(o.day), clock: `${o.hour}:${o.minute}` }
  }
  const then = parts(at), today = parts(now), yesterday = parts(now - 86400)
  if (then.day === today.day) return `${Math.round(minutes / 60)} 小時前`
  if (then.day === yesterday.day) return `昨天 ${then.clock}`
  return `${then.month}/${then.date} ${then.clock}`
}

/** How long something has gone on, as a person says it: 25 分鐘, 1 小時 20 分, 2 天 3 小時. */
export function durationPhrase(seconds: number): string {
  const minutes = Math.max(0, Math.round(seconds / 60))
  if (minutes < 60) return `${minutes} 分鐘`
  const hours = Math.floor(minutes / 60), rest = minutes % 60
  if (hours < 24) return rest ? `${hours} 小時 ${rest} 分` : `${hours} 小時`
  const days = Math.floor(hours / 24), h = hours % 24
  return h ? `${days} 天 ${h} 小時` : `${days} 天`
}

function workingFor(since: number | undefined, now: number): string | undefined {
  return since && now && since <= now ? durationPhrase(now - since) : undefined
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

/** "4 個 Session：2 個工作中、2 個閒置": a kind with none is not said (2026-11-12). */
export function countSummary(total: number, kinds: [number, string][]): string {
  const said = kinds.filter(([n]) => n > 0).map(([n, words]) => `${n} ${words}`)
  return said.length ? `${total} 個 Session：${said.join('、')}` : `${total} 個 Session`
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
    summary: countSummary(rows.length, [[needs.length, '個等你回應'], [working.length, '個工作中'], [done.length, '個已完成'], [idle.length, '個閒置']]),
    needs_you: needs.map((r) => ({
      ...brief(r),
      ...(r.question ? { question: r.question } : {}),
      ...(r.choices?.length ? { options: r.choices.map((c) => `${c.n}. ${c.label}`) } : r.options?.length ? { options: r.options } : {}),
      ...progressFields(latest.get(r.id)),
    })),
    working: working.map((r) => ({
      ...brief(r),
      ...(workingFor(r.working_since, now) ? { working_for: workingFor(r.working_since, now) } : {}),
      ...progressFields(latest.get(r.id)),
    })),
    done: done.map((r) => ({ ...brief(r), ...(whenPhrase(r.last_activity_at, now) ? { last_active: whenPhrase(r.last_activity_at, now) } : {}) })),
    idle: idle.map((r) => ({ ...brief(r), ...(whenPhrase(r.last_activity_at, now) ? { last_active: whenPhrase(r.last_activity_at, now) } : {}) })),
    // Read live with gpt-realtime-mini on 2026-11-12: asked what an idle
    // Session was doing, the copilot once invented its work from the name
    // alone. The warning sits beside the names it is about.
    ...(done.length || idle.length ? { done_and_idle: CONTENT_NOT_READ } : {}),
  }
  logs.push({ level: 'info', text: `成功讀取 Sessions 清單：${out.summary}` })
  return out
}

/** Said beside the done and idle names, which carry no content of their own. */
export const CONTENT_NOT_READ = 'done 與 idle 只有名稱和時間，沒有讀過內容。被問到它們在做什麼、做到哪、結果如何，一定先呼叫 get_session_summary，不可以從名稱猜'

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
  const found = await resolveRead(deps, read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  deps.focus?.set(row.id)
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
  const found = await resolveRead(deps, read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  deps.focus?.set(row.id)
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
  const found = await resolveRead(deps, read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  deps.focus?.set(row.id)
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
    ...(whenPhrase(row.delivered_at, now) ? { delivered_when: whenPhrase(row.delivered_at, now) } : {}),
    ...(lastReply ? { last_reply: lastReply } : {}),
    ...(workingFor(row.working_since, now) && row.state === 'working' ? { working_for: workingFor(row.working_since, now) } : {}),
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
  const found = await resolveRead(deps, read.rows, wanted)
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
  const found = await resolveRead(deps, read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  deps.focus?.set(row.id)
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

/**
 * The one Session a write names: its id (or a pane id without its sign) or
 * its whole name, never an assistant type or part of a name, which read
 * tools accept. A message once went to the wrong Session (2026-11-12).
 */
export function resolveForWrite(rows: readonly CopilotSessionRow[], wanted: string): { row: CopilotSessionRow } | { error: string; candidates: { id: string; label: string }[] } {
  const w = wanted.trim()
  const candidates = rows.map((r) => ({ id: r.id, label: r.label }))
  const byId = rows.find((r) => r.id === w) ?? (/^\d+$/.test(w) ? rows.find((r) => r.id === `%${w}`) : undefined)
  if (byId) return { row: byId }
  const byName = rows.filter((r) => r.label.trim().toLowerCase() === w.toLowerCase())
  if (byName.length === 1) return { row: byName[0] }
  return { error: `「${w}」不是一個明確的 Session；送出、回答或關閉要用清單裡的 id 或完整名稱`, candidates }
}

function sameName(a: string, b: string): boolean {
  // Spaces, quotes and the corner brackets 「」『』 a name may be said with.
  const n = (x: string) => x.replace(/[\s\u300c-\u300f"'`]/g, '').toLowerCase()
  const x = n(a), y = n(b)
  return !!x && !!y && (x === y || (Math.min(x.length, y.length) >= 4 && (x.includes(y) || y.includes(x))))
}

/**
 * Whether a write may go to `row`: the name the copilot said must be this
 * Session's, and a Session other than the one under discussion needs the
 * person to have confirmed the switch.
 */
export function writeGuard(deps: ToolDeps, row: CopilotSessionRow, said: string, switchConfirmed: boolean): string | null {
  if (!said.trim()) return '沒有說要送到哪個 Session：session_label 要填你跟使用者講的那個名稱'
  if (!sameName(said, row.label)) return `你說的是「${said}」，但 id 對到的是「${row.label}」，兩者不一致；沒有執行。請用清單確認正確的 id`
  const focus = deps.focus?.get()
  if (focus && focus !== row.id && !switchConfirmed) {
    return `目前在討論的不是「${row.label}」。先跟使用者確認要改到「${row.label}」，確認後再呼叫並把 switch_confirmed 設為 true`
  }
  return null
}

async function sendMessage(deps: ToolDeps, logs: ToolLog[], wanted: string, message: string, said = '', switchConfirmed = false): Promise<unknown> {
  if (!message.trim()) return { error: '沒有要送出的內容' }
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveForWrite(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates }
  }
  const { row } = found
  const refused = writeGuard(deps, row, said, switchConfirmed)
  if (refused) {
    logs.push({ level: 'error', text: refused })
    return { error: refused }
  }
  const res = await deps.fetch(`/v1/sessions/${encodeURIComponent(row.id)}/send`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': (deps.newRequestID ?? (() => crypto.randomUUID()))(),
      [UNOPENED_READ_HEADER]: '1',
    },
    body: JSON.stringify({ text: message }),
  })
  if (!res.ok) {
    const why = await refusalText(res)
    const error = `送到「${row.label}」失敗 (HTTP ${res.status})${why ? `：${why}` : ''}`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  deps.focus?.set(row.id)
  logs.push({ level: 'info', text: `已送出訊息至「${row.label}」` })
  return { success: true, sent_to: row.label }
}

/** One reusable text as the copilot is told of it. */
interface ReusableText { id: string; name: string; kind: 'snippet' | 'playbook'; body: string }

// previews are cut at LIST_GOAL_CHARS, the bound a list's goal line already has

/**
 * The Snippets and Playbooks a Session's sheet offers, read as the sheet reads
 * them (`GET /v1/snippets?session=`, `session/snippets-api.ts`). Clawdfather's
 * own actions are not offered.
 */
async function readReusable(deps: ToolDeps, row: CopilotSessionRow): Promise<ReusableText[] | { error: string }> {
  const res = await deps.fetch(`/v1/snippets?session=${encodeURIComponent(row.id)}`, { headers: { [UNOPENED_READ_HEADER]: '1' } })
  if (!res.ok) {
    const why = await refusalText(res)
    return { error: `讀不到「${row.label}」的 Snippet 與 Playbook（HTTP ${res.status}）${why ? `：${why}` : ''}` }
  }
  const body = (await res.json().catch(() => null)) as { snippets?: unknown } | null
  if (!body || !Array.isArray(body.snippets)) return { error: `「${row.label}」的 Snippet 與 Playbook 回應無法解讀` }
  const out: ReusableText[] = []
  for (const item of body.snippets as Record<string, unknown>[]) {
    const scope = str(item?.scope)
    if (scope !== 'global' && scope !== 'project' && scope !== 'playbook') continue
    const id = str(item.id)
    const name = str(item.title)
    if (!id || !name) continue
    out.push({ id, name, kind: scope === 'playbook' ? 'playbook' : 'snippet', body: str(item.body) })
  }
  return out
}

async function sessionForReusable(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<{ row: CopilotSessionRow } | { error: string; candidates?: unknown }> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = await resolveRead(deps, read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates }
  }
  return found
}

async function listSnippets(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
  const found = await sessionForReusable(deps, logs, wanted)
  if ('error' in found) return found
  const texts = await readReusable(deps, found.row)
  if ('error' in texts) {
    logs.push({ level: 'error', text: texts.error })
    return texts
  }
  logs.push({ level: 'info', text: `讀到「${found.row.label}」的 ${texts.length} 個 Snippet 與 Playbook` })
  const shown = (kind: ReusableText['kind']) => texts.filter((t) => t.kind === kind)
    .map((t) => ({ name: t.name, preview: t.body.slice(0, LIST_GOAL_CHARS) }))
  return { session: found.row.label, snippets: shown('snippet'), playbooks: shown('playbook') }
}

async function openSnippets(deps: ToolDeps, logs: ToolLog[], wanted: string, kind: 'snippet' | 'playbook'): Promise<unknown> {
  const found = await sessionForReusable(deps, logs, wanted)
  if ('error' in found) return found
  if (!deps.snippets) return { error: '這個畫面不能由副駕駛打開 Snippet 列表' }
  const outcome = await deps.snippets(found.row.id, { tab: kind })
  if (outcome !== 'opened') {
    const error = `沒有打開「${found.row.label}」的 ${kind === 'playbook' ? 'Playbook' : 'Snippet'} 列表（${outcome}）`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  deps.focus?.set(found.row.id)
  logs.push({ level: 'info', text: `已打開「${found.row.label}」的 ${kind === 'playbook' ? 'Playbook' : 'Snippet'} 列表` })
  return { opened: true, session: found.row.label, kind }
}

/** What a Playbook start that did not start means to a person (`SnippetsOutcome`). */
const PLAYBOOK_REFUSALS: Record<string, string> = {
  already_running: '這個 Session 已經有一個 Playbook 在跑',
  source_unavailable: '讀不到這個 Playbook 來源的專案檔案',
  source_changed: '它來源的專案檔案改過了，要先在畫面上看過新內容再開始',
  source_scope_conflict: '這個 Playbook 的來源屬於某個專案，但它沒有綁定專案',
  playbook_not_found: '畫面上的列表裡沒有這個 Playbook',
  not_ready: '列表還沒準備好',
  start_unknown: '不確定有沒有開始；它的待辦已經建立，請在畫面上看一下',
  timeout: '畫面沒有回應',
}

async function runSnippet(deps: ToolDeps, logs: ToolLog[], wanted: string, name: string, said = '', switchConfirmed = false): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveForWrite(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates }
  }
  const { row } = found
  const refused = writeGuard(deps, row, said, switchConfirmed)
  if (refused) {
    logs.push({ level: 'error', text: refused })
    return { error: refused }
  }
  const texts = await readReusable(deps, row)
  if ('error' in texts) {
    logs.push({ level: 'error', text: texts.error })
    return texts
  }
  // Only a whole name runs anything; which one the person meant is the
  // copilot's to choose from the list it is handed back.
  const wantedName = name.trim().toLowerCase()
  const named = texts.filter((t) => t.name.trim().toLowerCase() === wantedName)
  if (named.length !== 1) {
    const error = named.length
      ? `「${row.label}」有 ${named.length} 個都叫「${name}」，沒有執行；請使用者在畫面上選`
      : `「${row.label}」沒有名稱剛好是「${name}」的 Snippet 或 Playbook，沒有執行。從下面的名稱依使用者的意思挑一個，確認後再執行`
    logs.push({ level: 'error', text: error })
    return { error, names: texts.map((t) => ({ name: t.name, kind: t.kind })) }
  }
  const text = named[0]
  if (text.kind === 'snippet') {
    // A Snippet is words to send: the same send, guards and receipt as a message.
    const sent = await sendMessage(deps, logs, row.id, text.body, row.label, true) as { success?: boolean; error?: string }
    return sent.success ? { ran: true, kind: 'snippet', name: text.name, session: row.label } : sent
  }
  if (!deps.snippets) return { error: '這個畫面不能由副駕駛開始 Playbook' }
  const outcome = await deps.snippets(row.id, { tab: 'playbook', playbook: text.id, start: true })
  if (outcome !== 'started') {
    const error = `「${text.name}」沒有在「${row.label}」開始：${PLAYBOOK_REFUSALS[outcome] ?? outcome}`
    logs.push({ level: 'error', text: error })
    return { error, outcome }
  }
  deps.focus?.set(row.id)
  logs.push({ level: 'info', text: `已在「${row.label}」開始 Playbook「${text.name}」` })
  return { ran: true, kind: 'playbook', name: text.name, session: row.label }
}

async function openSessionTool(deps: ToolDeps, logs: ToolLog[], wanted: string): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  let found = await resolveRead(deps, read.rows, wanted)
  // A Session the copilot just started reaches the list a moment later.
  for (let tries = 0; 'error' in found && /^(%\d+|owned:)/.test(wanted.trim()) && tries < NEW_SESSION_READS; tries++) {
    await deps.sleep(NEW_SESSION_INTERVAL_MS)
    const again = await readRows(deps)
    if ('error' in again) break
    read.rows = again.rows
    found = await resolveRead(deps, read.rows, wanted)
  }
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates ?? read.rows.map((r) => ({ id: r.id, label: r.label })) }
  }
  const { row } = found
  deps.focus?.set(row.id)
  if (!deps.openSession) return { error: '這個畫面不能由副駕駛切換 Session' }
  const opened = await deps.openSession(row.id)
  if (!opened) {
    const error = `「${row.label}」沒有在這個畫面上打開（這個畫面目前讀不到它）`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  logs.push({ level: 'info', text: `已在畫面上打開「${row.label}」` })
  // Opened at the person's ask, it is told at once: what it did and what is
  // left, from the same record get_session_recap reads.
  const status = await sessionRecap(deps, logs, row.id)
  return { opened: true, label: row.label, status }
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

async function answerQuestion(deps: ToolDeps, logs: ToolLog[], wanted: string, spoken: string, said = '', switchConfirmed = false): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveForWrite(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates }
  }
  const { row } = found
  const refused = writeGuard(deps, row, said, switchConfirmed)
  if (refused) {
    logs.push({ level: 'error', text: refused })
    return { error: refused }
  }
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

/** How long open_session waits for a just-started Session to reach the list. */
export const NEW_SESSION_READS = 5
export const NEW_SESSION_INTERVAL_MS = 1000

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
    const found = await resolveRead(deps, read.rows, wanted)
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

/** The speed a spoken ask lands on: one step slower or faster, normal, or a named rate. */
export function nextSpeed(current: string, ask: string): string {
  const i = Math.max(0, SPEEDS.indexOf(current))
  if (ask === 'slower') return SPEEDS[Math.max(0, i - 1)]
  if (ask === 'faster') return SPEEDS[Math.min(SPEEDS.length - 1, i + 1)]
  if (ask === 'normal') return '1.0'
  return SPEEDS.includes(ask) ? ask : current
}

async function setSpeed(deps: ToolDeps, logs: ToolLog[], ask: string): Promise<unknown> {
  if (!deps.adjust || !deps.delivery) return { error: '這通電話不能調整語速' }
  const now = deps.delivery()
  const speed = nextSpeed(SPEEDS.includes(now.speed) ? now.speed : String(DEFAULT_SPEED), ask)
  if (speed === now.speed) {
    const edge = ask === 'slower' ? '已經是最慢的速度了' : ask === 'faster' ? '已經是最快的速度了' : '速度沒有變'
    return { speed, changed: false, note: edge }
  }
  const { saved } = await deps.adjust({ speed })
  logs.push({ level: 'info', text: `語速調成 ${speed} 倍${saved ? '，並存成下次通話的設定' : '（只在這通電話）'}` })
  return { speed, changed: true, saved_for_next_calls: saved }
}

async function setLength(deps: ToolDeps, logs: ToolLog[], brief: unknown): Promise<unknown> {
  if (!deps.adjust) return { error: '這通電話不能調整回答長度' }
  if (typeof brief !== 'boolean') return { error: '要說簡短（true）還是詳細（false）' }
  const { saved } = await deps.adjust({ brief })
  logs.push({ level: 'info', text: `${brief ? '改成簡短回答' : '改成詳細回答'}${saved ? '，並存成下次通話的設定' : '（只在這通電話）'}` })
  return { brief, saved_for_next_calls: saved }
}

/**
 * The part of the configuration an in-call change sends: the instructions
 * rebuilt for the new answer length, and the playback rate. Never the voice,
 * which a call cannot change once it has spoken.
 */
export function adjustEvent(memory: string, snapshot: string, options: CallOptions): Record<string, unknown> {
  const full = sessionUpdateEvent(memory, snapshot, options) as { session: { instructions: string; audio: { output: { speed: number } } } }
  return { type: 'session.update', session: { type: 'realtime', instructions: full.session.instructions,
    audio: { output: { speed: full.session.audio.output.speed } } } }
}

/** What a close refusal's reason codes mean to a person. */
const CLOSE_REASONS: Record<string, string> = {
  terminal_working: '它還在工作',
  session_identity_unbound: '機器還認不出它是哪個對話',
}

async function closeSession(deps: ToolDeps, logs: ToolLog[], wanted: string, said = '', switchConfirmed = false, forceConfirmed = false): Promise<unknown> {
  const read = await readRows(deps)
  if ('error' in read) {
    logs.push({ level: 'error', text: read.error })
    return { error: read.error }
  }
  const found = resolveForWrite(read.rows, wanted)
  if ('error' in found) {
    logs.push({ level: 'error', text: found.error })
    return { error: found.error, candidates: found.candidates }
  }
  const { row } = found
  const refused = writeGuard(deps, row, said, switchConfirmed)
  if (refused) {
    logs.push({ level: 'error', text: refused })
    return { error: refused }
  }
  const meaning = (codes: string[]) => codes.map((c) => CLOSE_REASONS[c] ?? c)
  // As the console's close sheet does (`overlays/action-confirm.ts`): a close
  // the machine calls unsafe is said with its reasons and asked again; only
  // the person's second, explicit "still close" sends `force`, pinned to the
  // reading those reasons came from, and the machine still decides.
  const unsafe = (reasons: string[]) => ({
    error: `「${row.label}」現在還不能安全關閉${reasons.length ? `：${meaning(reasons).join('、')}` : ''}。先把原因告訴使用者，問他「還是要關閉嗎？」；他明確說要，才帶 force_confirmed: true 再呼叫`,
    needs_confirmation: true,
    reasons,
  })
  const force = forceConfirmed && row.closeable !== 'safe'
  if (row.closeable !== 'safe' && !forceConfirmed) {
    const asked = unsafe(row.closeReasons ?? [])
    logs.push({ level: 'info', text: `「${row.label}」還不能安全關閉，等使用者確認` })
    return asked
  }
  const res = await deps.fetch(`/v1/sessions/${encodeURIComponent(row.id)}/close`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': (deps.newRequestID ?? (() => crypto.randomUUID()))(), [UNOPENED_READ_HEADER]: '1' },
    body: JSON.stringify({ force, ...(row.closeVersion ? { expected_closeability_version: row.closeVersion } : {}) }),
  })
  if (!res.ok) {
    let reasons: string[] = []
    try {
      const body = (await res.clone().json()) as { error?: { code?: unknown; reasons?: unknown } }
      if (Array.isArray(body.error?.reasons)) reasons = body.error.reasons.map((r) => (r && typeof r === 'object' ? str((r as { code?: unknown }).code) : str(r))).filter(Boolean)
    } catch {
      // refusal-ok: a refusal without reasons is still said by its HTTP status and message below.
    }
    // The row said safe but the machine found something owed: ask, as above.
    if (!force && res.status === 409 && reasons.length) {
      logs.push({ level: 'info', text: `「${row.label}」還不能安全關閉，等使用者確認` })
      return unsafe(reasons)
    }
    const detail = await refusalText(res)
    const error = `沒有關閉「${row.label}」（HTTP ${res.status}${reasons.length ? `：${meaning(reasons).join('、')}` : detail ? `：${detail}` : ''}）`
    logs.push({ level: 'error', text: error })
    return { error, reasons }
  }
  logs.push({ level: 'info', text: force ? `已照使用者確認強制關閉「${row.label}」` : `已關閉「${row.label}」` })
  return { closed: true, label: row.label, ...(force ? { forced: true } : {}) }
}

async function listMachines(deps: ToolDeps, logs: ToolLog[]): Promise<unknown> {
  const machines = deps.machines ? await deps.machines() : []
  logs.push({ level: 'info', text: `可以開 Session 的機器：${machines.length || 1} 台` })
  const list = machines.length ? machines : [{ id: '', name: '這台機器', online: true, current: true }]
  // The current machine's projects come with the list, so the first question
  // names real projects (a bare list once led to "專案A 還是專案B").
  const here = await listProjects(deps, logs, '')
  return { machines: list, current_machine: here }
}

interface PlacesAnswer {
  places?: { id?: unknown; label?: unknown; repo?: unknown }[]
  assistants?: { id?: unknown; label?: unknown; installed?: unknown; availability?: unknown }[]
}

async function listProjects(deps: ToolDeps, logs: ToolLog[], machine: string): Promise<unknown> {
  const q = machine ? `?machine=${encodeURIComponent(machine)}` : ''
  const res = await deps.fetch(`/v1/places${q}`)
  if (!res.ok) {
    const why = await refusalText(res)
    const error = `讀不到專案清單（HTTP ${res.status}${why ? `：${why}` : ''}）`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  const body = (await res.json()) as PlacesAnswer
  const projects = (body.places ?? []).filter((p) => str(p.id)).map((p) => ({ id: str(p.id), label: str(p.label) || str(p.id) }))
  // An assistant this machine says is not installed is not offered.
  const assistants = (body.assistants ?? []).filter((a) => str(a.id) && a.installed !== false)
    .map((a) => ({ id: str(a.id), label: str(a.label) || str(a.id) }))
  let models: Record<string, { default?: string; choices: string[] }> = {}
  if (!machine) {
    try {
      const m = await deps.fetch('/v1/settings/default-models')
      if (m.ok) {
        const d = (await m.json()) as Record<string, unknown> & { models?: Record<string, { value?: unknown; label?: unknown }[]> }
        for (const a of assistants) {
          const list = Array.isArray(d.models?.[a.id]) ? d.models![a.id] : []
          models[a.id] = {
            ...(str(d[`${a.id}_default_model`]) ? { default: str(d[`${a.id}_default_model`]) } : {}),
            choices: list.map((x) => str(x.value)).filter(Boolean).slice(0, 12),
          }
        }
      }
    } catch {
      // refusal-ok: without the model list a Session still starts on the assistant's own default.
      models = {}
    }
  }
  logs.push({ level: 'info', text: `讀到 ${projects.length} 個專案、${assistants.length} 種助理` })
  return { projects, assistants, ...(Object.keys(models).length ? { models } : { note: '這台機器的模型清單讀不到；不填 model 就用預設' }) }
}

async function startSession(deps: ToolDeps, logs: ToolLog[], args: Record<string, unknown>): Promise<unknown> {
  const project = str(args.project_id), said = str(args.project_label), assistant = str(args.assistant)
  const model = str(args.model), machine = str(args.machine_id)
  if (!project || !assistant) return { error: '還不知道要開在哪個專案或用哪個助理' }
  const q = machine ? `?machine=${encodeURIComponent(machine)}` : ''
  const listed = await deps.fetch(`/v1/places${q}`)
  if (!listed.ok) return { error: `讀不到專案清單（HTTP ${listed.status}）` }
  const place = ((await listed.json()) as PlacesAnswer).places?.find((p) => str(p.id) === project)
  if (!place) return { error: `這台機器沒有 id 為「${project}」的專案；請用 list_projects 確認` }
  if (!sameName(said, str(place.label))) return { error: `你說的是「${said}」，但這個 id 是「${str(place.label)}」專案；沒有開。請確認專案` }
  let path = `/v1/places/${encodeURIComponent(project)}/start/${encodeURIComponent(assistant)}`
  if (model) path += `/${encodeURIComponent(model)}`
  const res = await deps.fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': (deps.newRequestID ?? (() => crypto.randomUUID()))() },
    body: '{}',
  })
  if (!res.ok) {
    const why = await refusalText(res)
    const error = `沒有開成新的 Session（HTTP ${res.status}${why ? `：${why}` : ''}）`
    logs.push({ level: 'error', text: error })
    return { error }
  }
  const answer = (await res.json().catch(() => ({}))) as { id?: unknown }
  const id = str(answer.id)
  if (id) deps.focus?.set(id)
  logs.push({ level: 'info', text: `已在「${str(place.label)}」開了新的 ${assistant} Session` })
  return { started: true, project: str(place.label), assistant, ...(model ? { model } : { model: '預設' }), ...(id ? { session_id: id } : {}) }
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
  // A read's Session is judged from the person's own words (`resolveRead`).
  deps = { ...deps, said: typeof args.said === 'string' ? args.said : undefined }
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
    else if (name === 'answer_session_question') result = await answerQuestion(deps, logs, str(args.session_id), str(args.choice), str(args.session_label), args.switch_confirmed === true)
    else if (name === 'get_session_notes') result = await sessionNotes(deps, logs, str(args.session_id))
    else if (name === 'list_snippets') result = await listSnippets(deps, logs, str(args.session_id))
    else if (name === 'open_snippets') result = await openSnippets(deps, logs, str(args.session_id), args.kind === 'playbook' ? 'playbook' : 'snippet')
    else if (name === 'run_snippet') result = await runSnippet(deps, logs, str(args.session_id), str(args.name), str(args.session_label), args.switch_confirmed === true)
    else if (name === 'set_speaking_speed') result = await setSpeed(deps, logs, str(args.speed))
    else if (name === 'set_answer_length') result = await setLength(deps, logs, args.brief)
    else if (name === 'close_session') result = await closeSession(deps, logs, str(args.session_id), str(args.session_label), args.switch_confirmed === true, args.force_confirmed === true)
    else if (name === 'list_machines') result = await listMachines(deps, logs)
    else if (name === 'list_projects') result = await listProjects(deps, logs, str(args.machine_id))
    else if (name === 'start_session') result = await startSession(deps, logs, args)
    else if (name === 'send_message_to_session') result = await sendMessage(deps, logs, str(args.session_id), str(args.message), str(args.session_label), args.switch_confirmed === true)
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

/**
 * The refusal in a `/v1/voice/copilot-session` answer, in either spelling the
 * route reaches a page with: the machine's own `{error:{code,message}}`, or
 * the Cloud seam's flat `{error:"code", detail}` (`spellingOf` in
 * cloud/relay-writer.ts names this word "flat"). Reading only the first
 * spelling showed a phone "HTTP 501" with the cause dropped.
 */
export function copilotRefusal(body: unknown, status: number): { code: string; message: string } {
  const answer = (body && typeof body === 'object' ? body : {}) as { error?: unknown; detail?: unknown }
  if (typeof answer.error === 'string' && answer.error) {
    return { code: answer.error, message: typeof answer.detail === 'string' && answer.detail !== answer.error ? answer.detail : '' }
  }
  const nested = (answer.error && typeof answer.error === 'object' ? answer.error : {}) as { code?: unknown; message?: unknown }
  const code = typeof nested.code === 'string' && nested.code ? nested.code : `HTTP ${status}`
  return { code, message: typeof nested.message === 'string' && nested.message !== code ? nested.message : '' }
}

/** Codes meaning the chosen machine's Clawdline has no copilot word yet (`MACHINE_LACKS_WORD`, cloud/relay-reader.ts). */
export const COPILOT_MACHINE_LACKS: ReadonlySet<string> = new Set(['unknown_command', 'cloud_feature_unavailable', 'cloud_machine_unsupported'])

/** A finished action the person should see and hear, as a string key and its words. */
export interface ActionNotice { key: 'copilotDidSend' | 'copilotDidAnswer' | 'copilotDidClose' | 'copilotDidRun' | 'copilotDidStart'; params: Record<string, string> }

/**
 * What a write tool's answer did, when the machine says it was done: a send,
 * an answer, a close, a Snippet or Playbook, a new Session. Anything else, and
 * every refusal, is no notice. On 2026-11-12 a person watching only the
 * bubble could not tell the copilot had sent a message it said it sent.
 */
export function actionNotice(tool: string, output: string): ActionNotice | null {
  let o: Record<string, unknown>
  try { o = JSON.parse(output) as Record<string, unknown> } catch { return null }
  if (!o || typeof o !== 'object' || 'error' in o) return null
  if (tool === 'send_message_to_session' && o.success === true) return { key: 'copilotDidSend', params: { label: str(o.sent_to) } }
  if (tool === 'answer_session_question' && o.answered === true) return { key: 'copilotDidAnswer', params: { label: str(o.label), choice: str(o.chose) } }
  if (tool === 'close_session' && o.closed === true) return { key: 'copilotDidClose', params: { label: str(o.label) } }
  if (tool === 'run_snippet' && o.ran === true) return { key: 'copilotDidRun', params: { label: str(o.session), name: str(o.name) } }
  if (tool === 'start_session' && o.started === true) return { key: 'copilotDidStart', params: { project: str(o.project) } }
  return null
}

/** The kinds of action the copilot can say it did. */
export type ActionKind = 'send' | 'answer' | 'close' | 'run' | 'start'

/** Which kind each notice is. */
export const NOTICE_KIND: Record<ActionNotice['key'], ActionKind> = {
  copilotDidSend: 'send', copilotDidAnswer: 'answer', copilotDidClose: 'close', copilotDidRun: 'run', copilotDidStart: 'start',
}

/** Sentences that say an action is done, by kind; a question ("…對嗎？") never is one. */
const CLAIMS: [ActionKind, RegExp][] = [
  // The copilot's own doing ("幫你", "我", or the message itself), never a
  // Session's work reported in the third person ("它已經發布了…").
  ['send', /(幫你|替你|我)(已經|已)?(送|傳)|(訊息|指令|內容|這段|這句)[^，。]{0,6}(送|傳)(出去|過去|出|到)?了|送到「[^」]+」了/],
  ['answer', /(幫你|替你|我)(已經|已)?(回答|選|按)/],
  ['close', /(幫你|替你|我)(已經|已)?(關閉|關掉)/],
  ['run', /(幫你|替你|我)(已經|已)?(執行|啟動|開始跑)/],
  ['start', /(幫你|替你|我)(已經|已)?開(好|了)/],
]

/**
 * The actions `speech` says are done that the machine did not confirm since
 * the person last spoke (`done`). On 2026-11-12 the copilot told the person a
 * message was sent without calling the send; only the second ask sent it.
 */
export function unbackedClaims(speech: string, done: ReadonlySet<ActionKind>): ActionKind[] {
  const out: ActionKind[] = []
  // Split at the full stop, the exclamation and a line break, written as escapes.
  for (const sentence of speech.split(/[\u3002\uff01!\n]/)) {
    if (/[\u55ce\uff1f?]/.test(sentence)) continue
    for (const [kind, said] of CLAIMS) {
      if (said.test(sentence) && !done.has(kind) && !out.includes(kind)) out.push(kind)
    }
  }
  return out
}

/** What the copilot is told when it said it did something the machine did not do. */
export function unbackedClaimEvents(kinds: readonly ActionKind[]): Record<string, unknown>[] {
  return [
    { type: 'conversation.item.create', item: { type: 'message', role: 'system', content: [{ type: 'input_text',
      text: `你剛剛說已經${kinds.map((k) => ({ send: '送出', answer: '回答', close: '關閉', run: '執行', start: '開新 Session' })[k]).join('、')}，但機器沒有收到這個動作，其實沒有做。用一句話照實跟使用者更正，再確認一次要不要現在做。` }] } },
    { type: 'response.create' },
  ]
}

/** One tool the copilot used in the current turn, as the call sheet shows it. */
export interface ToolTrace { name: string; target?: string; ok: boolean; ms: number }

/**
 * What a tool call was, for the sheet's tool line: its own name, the Session
 * or project its answer names, whether it answered without an error, and how
 * long it took. The person reads these to learn when the copilot reaches for
 * a tool and where it should do better.
 */
export function toolTrace(name: string, output: string, ms: number): ToolTrace {
  let o: Record<string, unknown> = {}
  try { o = (JSON.parse(output) ?? {}) as Record<string, unknown> } catch { /* refusal-ok: a reply that is not JSON is shown as a failed call. */ }
  const status = (o.status && typeof o.status === 'object' ? o.status : {}) as Record<string, unknown>
  const target = str(o.label) || str(o.sent_to) || str(o.session) || str(status.label) || str(o.project)
  return { name, ...(target ? { target } : {}), ok: !!o && typeof o === 'object' && !('error' in o) && output.trim().startsWith('{'), ms: Math.max(0, Math.round(ms)) }
}
