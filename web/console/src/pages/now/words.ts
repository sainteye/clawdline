/*
 * The words of the "what is the state of things" page (work-system-review §5.2,
 * W4).
 *
 * The Swift app never had this page, so its catalog has no words for it. They
 * live here, beside the page that says them, as the board's do
 * (`pages/work/words.ts`), and the copied `public/strings/zh-Hant.json` stays
 * byte for byte. Where the catalog already has the word — "載入中",
 * "重新整理" — the page reads the catalog, not this.
 *
 * **The freshness sentences are the point of the page and not decoration.**
 * Each block prints the one for its own source, and there is a different
 * sentence for every one of the four words a source may answer, because the
 * failure this page exists to stop is one sentence standing for all of them.
 * "沒有" and "讀不到" are not the same fact and never share a line here.
 *
 * Holes are `{name}`, as the catalog's are.
 */

const words = {
  en: {
    nav: "Now",
    eyebrow: "NOW · THIS MACHINE",
    title: "Where things stand",
    lede:
      "Three questions, each read from its own source, each saying how good that reading is. A source that " +
      "could not be read says so; it is never drawn as a zero.",
    refresh: "Read again",

    doingTitle: "In progress",
    doingLede: "Tasks this machine has dispatched and that have not finished.",
    doingNone: "Nothing is running.",
    doingRoot: "Root: {root}",
    doingNoRoot: "No root named",
    doingFor: "running {age}",

    owedTitle: "Delivered, not recorded",
    owedLede: "Deliveries the ledger still owes a landing for. Oldest first.",
    owedNone: "Nothing is owed.",
    owedOldest: "Oldest: {age}",
    owedTarget: "Target: {target}",
    owedNoTarget: "No target named, so whether it is already on one cannot be asked",
    owedBranchEmpty: "Its branch carried nothing when it ended",
    owedBranchCarries: "Delivered on its branch",
    owedBranchUnreadable: "Its branch could not be read when it ended",
    owedBranchUnknown: "No branch of its own",
    owedWho: "{who} has to move it",
    owedWhoExecutor: "The session doing it",
    owedWhoRoot: "Its root",

    waitingTitle: "Waiting for you",
    waitingLede: "Proposals still to confirm, and questions a session asked you.",
    waitingNone: "Nothing is waiting for you.",
    waitingProposals: "{n} to confirm",
    waitingDecisions: "{n} to answer",
    waitingOldest: "Oldest: {age}",
    waitingGo: "Open the board",

    freshCurrent: "Read just now.",
    freshStale: "Read, and short: part of this source could not be reached, so there is more than this.",
    freshMissing: "Could not be read. What is here is not a count of anything.",
    freshUnverified: "Read in full, and this may already be out of date: {why}",
    freshWhyLandings: "nothing on the record says which branch these were meant to reach, so nobody can ask whether they got there",
    freshWhyTasks: "an earlier reading of the other store was carried, so a row may have moved since",
    freshWhyWaiting: "the board's sweep is not running, so nothing here has expired or defaulted when it should have",
    freshWhyOther: "a fact in it rests on something this daemon cannot confirm still holds",
    freshAt: "at {when}",
    unknownCount: "—",
    unreadable: "This block could not be read.",
    more: "{n} more",
    open: "Open",
    timeline: "Timeline",
  },
  "zh-Hant": {
    nav: "現在",
    eyebrow: "現在 · 這台機器",
    title: "現在是什麼狀況",
    lede: "三個問題，各自讀自己的來源，各自說出那份讀取值多少。讀不到的來源會說它讀不到，不會被畫成 0。",
    refresh: "重新讀一次",

    doingTitle: "正在做",
    doingLede: "這台機器派出去、還沒結束的 task。",
    doingNone: "沒有正在跑的東西。",
    doingRoot: "root：{root}",
    doingNoRoot: "沒有指名 root",
    doingFor: "已經跑了 {age}",

    owedTitle: "交了還沒記帳",
    owedLede: "帳本上還欠一筆落地紀錄的交付，最老的排前面。",
    owedNone: "沒有欠著的。",
    owedOldest: "最老的：{age}",
    owedTarget: "目標分支：{target}",
    owedNoTarget: "紀錄上沒有寫目標分支，所以「它到了沒」這個問題問不出口",
    owedBranchEmpty: "結束的時候，它的分支上什麼都沒有",
    owedBranchCarries: "東西在它自己的分支上",
    owedBranchUnreadable: "結束的時候，它的分支讀不到",
    owedBranchUnknown: "沒有自己的分支",
    owedWho: "要由{who}處理",
    owedWhoExecutor: "正在做它的那個 session",
    owedWhoRoot: "它的 root",

    waitingTitle: "等你回答",
    waitingLede: "還沒確認的提議，以及 session 問你的問題。",
    waitingNone: "沒有在等你的事。",
    waitingProposals: "{n} 筆待確認",
    waitingDecisions: "{n} 筆等你決定",
    waitingOldest: "最老的：{age}",
    waitingGo: "去看板",

    freshCurrent: "剛剛讀到的。",
    freshStale: "讀到了，但不齊：這個來源有一部分連不上，所以實際上不只這些。",
    freshMissing: "讀不到。這裡的數字不是任何東西的數量。",
    freshUnverified: "讀到了，但這件事可能已經過期：{why}",
    freshWhyLandings: "紀錄上沒有寫這些交付本來要進哪一條分支，所以沒有人問得出它們到了沒",
    freshWhyTasks: "另一個 store 用的是上一次的讀取，所以其中某一列可能已經動過了",
    freshWhyWaiting: "看板的巡檢沒有在跑，所以該退場、該用預設值結束的東西都還留在這裡",
    freshWhyOther: "它裡面有一件事，靠的是這個 daemon 沒辦法確認還成不成立的東西",
    freshAt: "{when} 讀的",
    unknownCount: "—",
    unreadable: "這一塊讀不到。",
    more: "還有 {n} 項",
    open: "打開",
    timeline: "時間軸",
  },
} as const

export type NowWord = keyof (typeof words)["en"]

/** The page's language as the copied catalog set it, or the browser's (next-strings.ts). */
function language(): "en" | "zh-Hant" {
  const lang = (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) || "en"
  return lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"
}

/** One sentence, its holes filled. */
export function nowWord(key: NowWord, holes: Record<string, string | number> = {}): string {
  return words[language()][key].replace(/\{(\w+)\}/g, (all, name: string) =>
    name in holes ? String(holes[name]) : all,
  )
}
