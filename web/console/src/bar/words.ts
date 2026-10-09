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
  /** `placeholder` */
  placeholder: "跟 Claude 說⋯⋯",

  /** `hintSend` */
  hintSend: "送出",
  /** `hintNewline` */
  hintNewline: "換行",
  /** `hintSwitch` */
  hintSwitch: "換分頁",
  /** `hintList` */
  hintList: "清單",
  /** `hintMascot` */
  hintMascot: "換角色",
  /** `hintOutput` */
  hintOutput: "看輸出",
  /** `hintStacks` */
  hintStacks: "伺服器",
  /** `hintFullscreen` */
  hintFullscreen: "全螢幕",
  /** `hintKeys` */
  hintKeys: "快速鍵",
  /** `hintTextSize` */
  hintTextSize: "字級",
  /** `hintOrder` */
  hintOrder: "反序",
  /** `hintVoice` */
  hintVoice: "語音",

  /** `scanning` */
  scanning: "掃描中⋯",
  /** `noSession` */
  noSession: "找不到在跑 Claude Code 的分頁",
  /** `nothingToSend` */
  nothingToSend: "沒有可以送的分頁——先在終端機裡開一個 Claude Code",
  /** `sendFailed` */
  sendFailed: "送不出去",

  /** `sessionWaiting` */
  sessionWaiting: "在等你回答",
  /** `sessionAgents`, with `{n}` */
  sessionAgents: "{n} 個在背景",
  /** `sessionShellOne` */
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
