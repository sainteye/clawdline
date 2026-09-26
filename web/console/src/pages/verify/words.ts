/*
 * The words of the 驗收 page (docs/verifications.md).
 *
 * The Swift app never had this page, so its catalog has no words for it. They
 * live here beside the page, as the board's and the "now" page's do, and the
 * copied `public/strings/zh-Hant.json` stays byte for byte. Where the catalog
 * already has the word — "載入中" — the page reads the catalog, not this.
 *
 * Holes are `{name}`, as the catalog's are.
 */

const words = {
  en: {
    nav: "Verify",
    eyebrow: "VERIFY · THIS MACHINE",
    title: "Waiting to be verified",
    lede:
      "Things that were changed and have a date by which someone looks again. Each says why, what would " +
      "count as it holding, and the data it is judged by, read now.",
    refresh: "Read again",
    unreadable: "The verifications could not be read.",
    listNone: "Nothing is waiting to be verified.",
    closedShow: "Closed ({n})",
    closedHide: "Hide closed",
    dueIn: "due in {left}",
    overdue: "overdue by {left}",
    dueAt: "due {when}",
    startedAt: "started {when}",
    closedOn: "closed {when}",
    days: "{d}d {h}h",
    hours: "{h}h {m}m",
    minutes: "{m}m",
    tally: "{passed} passed · {failed} failed · {unset} unmarked",
    back: "All verifications",

    why: "Why",
    whyNone: "No reason was written down.",
    criteria: "What counts as it holding",
    criteriaNone: "No criteria were written down.",
    passed: "Passed",
    failed: "Failed",
    unset: "Unmarked",
    markPassed: "Mark passed",
    markFailed: "Mark failed",
    markUnset: "Clear",

    data: "Data",
    dataNone: "This record reads no data.",
    dataUnreadable: "The data could not be read: {why}",
    dataRange: "Child tasks created {since} – {until}, by the compaction window they were launched with.",
    dataEmpty: "No child task in this range was launched with a known window.",
    colGroup: "Group",
    colSessions: "Sessions",
    colTasks: "Tasks",
    colCost: "Cost / task",
    colCalls: "Calls / task",
    colCompactions: "Compactions / task",
    colAbove: "Above 200k",
    colSuccess: "Success",
    colStalled: "Stalled",
    colRespawns: "Respawns",
    groupNone: "before setting",
    costNote: "Cost / task is the median over the tasks the ledger has read; + means part of it has no price.",
    tooFew: "{group}: {n} tasks, fewer than {min} — too few to compare, so no percentage is shown.",
    stillRunning: "{group}: {running} still running, {unread} not read by the ledger yet.",
    excluded: "{n} tasks in the range had no known window and are in no group.",
    truncated: "The range held more tasks than one answer reads; these are the newest.",

    notes: "Notes",
    notesNone: "No notes yet.",
    notePlaceholder: "What you saw",
    noteAdd: "Add note",
    byPerson: "You",

    verdict: "Verdict",
    reasonPlaceholder: "Why — required",
    accept: "Accept",
    reject: "Reject",
    reasonNeeded: "Write down why first.",
    accepted: "Accepted",
    rejected: "Rejected",
    open: "Open",

    remove: "Delete",
    removeAsk: "Delete this record and every note on it? This cannot be undone.",
    removeAskOpen: "It is still open: deleting it throws it away unverified, with every note on it. This cannot be undone.",
    removeYes: "Delete it",
    cancel: "Cancel",
    failedAction: "Not done: {why}",
  },
  "zh-Hant": {
    nav: "驗收",
    eyebrow: "驗收 · 這台機器",
    title: "等待驗收",
    lede: "改過、並約好日期回頭檢查的事。每一項寫著為什麼、怎樣算成立，以及拿來判斷的資料（打開時現讀）。",
    refresh: "重新讀取",
    unreadable: "讀不到驗收清單。",
    listNone: "沒有等待驗收的項目。",
    closedShow: "已結案（{n}）",
    closedHide: "收起已結案",
    dueIn: "還有 {left}到期",
    overdue: "已逾期 {left}",
    dueAt: "{when} 到期",
    startedAt: "{when} 開始",
    closedOn: "{when} 結案",
    days: "{d} 天 {h} 小時",
    hours: "{h} 小時 {m} 分",
    minutes: "{m} 分",
    tally: "{passed} 項通過 · {failed} 項未通過 · {unset} 項未標記",
    back: "全部驗收",

    why: "為什麼",
    whyNone: "沒有寫下原因。",
    criteria: "怎樣算成立",
    criteriaNone: "沒有寫下條件。",
    passed: "通過",
    failed: "未通過",
    unset: "未標記",
    markPassed: "標為通過",
    markFailed: "標為未通過",
    markUnset: "清除",

    data: "資料",
    dataNone: "這一項不讀資料。",
    dataUnreadable: "讀不到資料：{why}",
    dataRange: "{since} – {until} 建立的 child task，依啟動時的壓縮門檻分組。",
    dataEmpty: "這段期間沒有以已知門檻啟動的 child task。",
    colGroup: "組",
    colSessions: "Session",
    colTasks: "Task",
    colCost: "每個 task 成本",
    colCalls: "每個 task 呼叫",
    colCompactions: "每個 task 壓縮",
    colAbove: "超過 200k",
    colSuccess: "成功率",
    colStalled: "Stalled",
    colRespawns: "重派",
    groupNone: "before-setting",
    costNote: "每個 task 成本是帳本已讀到的 task 的中位數；+ 表示其中有部分沒有價格。",
    tooFew: "{group}：{n} 個 task，少於 {min} 個——太少，不能比較，所以不顯示百分比。",
    stillRunning: "{group}：{running} 個還在跑，{unread} 個帳本還沒讀到。",
    excluded: "這段期間有 {n} 個 task 沒有已知門檻，不在任何一組。",
    truncated: "這段期間的 task 超過一次能讀的數量；這裡是最新的那些。",

    notes: "紀錄",
    notesNone: "還沒有紀錄。",
    notePlaceholder: "你看到了什麼",
    noteAdd: "加入紀錄",
    byPerson: "你",

    verdict: "結論",
    reasonPlaceholder: "原因（必填）",
    accept: "驗收通過",
    reject: "不通過",
    reasonNeeded: "先寫下原因。",
    accepted: "已通過",
    rejected: "未通過",
    open: "進行中",

    remove: "刪除",
    removeAsk: "刪除這一項和它所有的紀錄？無法復原。",
    removeAskOpen: "它還沒結案：刪除等於不驗收就丟掉，連同所有紀錄。無法復原。",
    removeYes: "確定刪除",
    cancel: "取消",
    failedAction: "沒有完成：{why}",
  },
} as const

export type VerifyWord = keyof (typeof words)["en"]
export type Language = keyof typeof words

/** The page's language as the copied catalog set it, or the browser's (next-strings.ts). */
export function language(): Language {
  const lang = (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) || "en"
  return lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"
}

/** One sentence in a named language, its holes filled. */
export function verifyWordIn(lang: Language, key: VerifyWord, holes: Record<string, string | number> = {}): string {
  return words[lang][key].replace(/\{(\w+)\}/g, (all, name: string) =>
    name in holes ? String(holes[name]) : all,
  )
}

/** One sentence in the page's language. */
export function verifyWord(key: VerifyWord, holes: Record<string, string | number> = {}): string {
  return verifyWordIn(language(), key, holes)
}
