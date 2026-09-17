// The words the input bar says.
//
// Copied from the Swift app's `Sources/Copy+Chinese.swift` (`TraditionalChinese`),
// property for property and under the same names — the arrangement
// `shell/darwin/Copy.swift` already uses for the menus. Nothing here is written
// fresh: a word the original does not have is a word this bar does not show.
//
// **Why not the catalog.** The console's words come from
// `web/console/public/strings/zh-Hant.json`, which is a byte-for-byte copy of the
// Swift app's web catalog and is guarded as one (`tools/check-legacy-css.sh`).
// That catalog has four of the bar's words (`placeholder`, `hintList`,
// `hintOrder`, `hintKeys`) and not the other eleven, because in the Swift app the
// bar was a native panel and was never on a web page. Adding them would mean
// editing a guarded copy; inventing them would mean inventing Chinese. So they
// are copied here, from the place the Swift app keeps them, and this file is the
// bar's whole vocabulary — including the four the catalog happens to have, so
// that one screen reads from one list.
//
// Only Traditional Chinese, as `shell/darwin/Copy.swift` carries only that. The
// Swift app follows `language: auto` across fourteen; that is not ported.
//
// Kept in the page rather than handed over by the shell (which is how
// `pages/settings/shell.ts` gets the native settings window's words). The bar's
// words belong to the bar, and a Linux or Windows shell should not have to carry
// a copy of a Chinese catalog to put a card on screen — see
// `docs/cross-platform.md`.

/** `Assistant.label`: what a person calls it. Product names, not translated. */
export const ASSISTANT_LABEL: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
}

export const words = {
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
} as const

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

/** `shellsSaid` (`Controller.swift`). */
export function shellsSaid(count: number): string {
  return count === 1 ? words.sessionShellOne : fillCount(words.sessionShellMany, count)
}
