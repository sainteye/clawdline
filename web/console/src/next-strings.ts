/*
 * Words this app says that the Swift app never did (design-decisions D35).
 *
 * The copied catalog, `public/strings/zh-Hant.json`, is the Swift app's and
 * stays byte for byte; a sentence it has no key for goes here instead, in the
 * two languages this machine's person reads, and nowhere else. Every one of
 * them is a place this app deliberately says more than the one it replicates:
 * the Swift app cut the same things and said nothing (docs/limits.md §3.2).
 *
 * Holes are `{name}`, as the copied catalog's are.
 */

const words = {
  en: {
    olderNotRead:
      "Earlier conversation was not loaded: this page reads only the last {window} of the record, and {bytes} before that were not read.",
    olderLeftOut: "Earlier conversation was not loaded: {count} older entries did not fit in one page ({budget}).",
    documentsListed: "{count} older documents are not in this list.",
    documentsWalked: "The search stopped after {entries} entries, so some folders were not looked through.",
    documentsTasks: "The folders of {count} older tasks were not searched.",
    stackStatusNotRun: "needs its status command",
    stackNothingDeclared: "declares no ports",
    stackUnknownTip:
      "Only this project's own status command can tell, and this app does not run a project's commands. Run it in a terminal in {root}.",
    stacksCommandsNotRun:
      "Only the ports in each .devstack.json are asked. Its commands (status, up, restart, down, logs) are not run from here: start, restart or read a stack in a terminal in its project.",
    stackNoAction: "Not run from here. Start or restart {name} in a terminal in its project.",
    stacksNone: "No project on this machine has a .devstack.json.",
    stacksFailed: "Could not read the server list.",
    stacksTruncated: "Some projects were not looked at; this list may be incomplete.",
  },
  "zh-Hant": {
    olderNotRead: "更早的對話沒有載入：這一頁只讀對話記錄的最後 {window}，前面還有 {bytes} 沒有讀。",
    olderLeftOut: "更早的對話沒有載入：一頁最多 {budget}，比較舊的 {count} 則沒有放進來。",
    documentsListed: "還有 {count} 份比較舊的文件不在這份清單上。",
    documentsWalked: "搜尋在 {entries} 個項目後停下，有些資料夾沒有看完。",
    documentsTasks: "比較舊的 {count} 個工作的資料夾沒有搜尋。",
    stackStatusNotRun: "要跑它的 status 才知道",
    stackNothingDeclared: "沒有宣告 port",
    stackUnknownTip: "只有這個專案自己的 status 指令知道狀態，而這個 app 不執行專案的指令。請在 {root} 的終端機裡跑。",
    stacksCommandsNotRun:
      "這裡只探測 .devstack.json 宣告的 port，不會執行檔案裡的指令（status、up、restart、down、logs）：要啟動、重啟或看紀錄，請在那個專案的終端機裡自己跑。",
    stackNoAction: "這裡不會執行指令。請在 {name} 專案的終端機裡啟動或重啟。",
    stacksNone: "這台機器上的專案都沒有 .devstack.json。",
    stacksFailed: "讀不到伺服器清單。",
    stacksTruncated: "有些專案沒有看完，這份清單可能不完整。",
  },
} as const

export type NextWord = keyof (typeof words)["en"]

/** The page's language as the copied catalog set it, or the browser's. */
function language(): "en" | "zh-Hant" {
  const lang = (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) || "en"
  return lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"
}

/** One sentence, its holes filled. */
export function nextWord(key: NextWord, holes: Record<string, string | number> = {}): string {
  return words[language()][key].replace(/\{(\w+)\}/g, (all, name: string) =>
    name in holes ? String(holes[name]) : all,
  )
}

/** A byte count as a person reads it: 8 MB, 412.3 MB, 150 KB. */
export function byteWords(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"]
  let value = n
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  const shown = unit === 0 || Number.isInteger(value) ? String(Math.round(value)) : value.toFixed(1)
  return shown + " " + units[unit]
}
