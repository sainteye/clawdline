// The bar's original Taiwan Chinese copy came from the archived Swift panel.
// The guarded Swift web catalog stays unchanged; maintained words for this
// bar now live in `public/catalogs/<tag>.json` for every supported language.
// The browser's `ui_language` choice controls them, independently of the
// daemon's Agent and voice language setting.

/** `Assistant.label`: what a person calls it. Product names, not translated. */
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../catalog.ts"

export const ASSISTANT_LABEL: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
}

const baseWords = {
  placeholder: "跟 Claude 說⋯⋯",

  hintSend: "送出",
  hintNewline: "換行",
  hintSwitch: "換分頁",
  hintList: "清單",
  hintMascot: "換角色",
  hintOutput: "看輸出",
  hintStacks: "伺服器",
  hintFullscreen: "全螢幕",
  hintKeys: "快速鍵",
  hintTextSize: "字級",
  hintOrder: "反序",
  hintVoice: "語音",

  scanning: "掃描中⋯",
  noSession: "目前沒有 Session",
  nothingToSend: "沒有可傳送的 Session。請先在終端機啟動 Claude Code 或 Codex。",
  sendFailed: "送不出去",

  sessionWaiting: "在等你回答",
  /** `sessionAgents`, with `{n}` */
  sessionAgents: "{n} 個在背景",
  sessionShellOne: "1 個 shell 在跑",
  /** `sessionShellMany`, with `{n}` */
  sessionShellMany: "{n} 個 shell 在跑",

  /** `stackTip(up:total:)`, with `{total}` and `{up}` */
  stackTip: "{total} 個伺服器，{up} 個活著——⌘S 打開清單",
} as const

export const words = new Proxy(baseWords, {
  get(target, key, receiver) {
    return typeof key === "string" && key in target ? catalogWord("bar", key) : Reflect.get(target, key, receiver)
  },
})

/**
 * `Assistant.promptPlaceholder(from:)` (`AssistantPlaceholder.swift`).
 *
 * The translations predate multiple assistants and therefore contain Claude's
 * product name; substituting the one dynamic noun keeps the rest of the
 * sentence as it was translated.
 */
export function placeholderFor(assistant: string | undefined): string {
  if (assistant !== "codex") return words.placeholder
  const label = ASSISTANT_LABEL.codex
  const changed = words.placeholder.replaceAll("Claude Code", label).replaceAll("Claude", label)
  return changed === words.placeholder ? label + "…" : changed
}

/** `L.t.sessionAgents` / `sessionShellMany`: the original replaces `{n}` and nothing else. */
export function fillCount(template: string, n: number): string {
  return template.replaceAll("{n}", String(n))
}

/** `agentsSaid` (`Controller.swift`). */
export function agentsSaid(count: number): string {
  return fillCount(words.sessionAgents, count)
}

/** `L.t.stackTip(up:total:)`. */
export function stackTipSaid(up: number, total: number): string {
  return words.stackTip.replaceAll("{total}", String(total)).replaceAll("{up}", String(up))
}

/** `shellsSaid` (`Controller.swift`). */
export function shellsSaid(count: number): string {
  return count === 1 ? words.sessionShellOne : fillCount(words.sessionShellMany, count)
}
