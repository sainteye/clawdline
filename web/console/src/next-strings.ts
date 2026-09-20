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
    sessionGone: "The session in this address is no longer on this machine, so here is the list.",
    cloudChecking: "Checking this browser's Clawdline account…",
    cloudSignInLede: "Sign in to read your machines",
    cloudSignInFine:
      "Clawdline Cloud lists the machines paired with this browser and their sessions, and carries what you send them.",
    cloudConnecting: "Connecting to Clawdline Cloud…",
    cloudRetrying: "Clawdline Cloud did not answer ({code}). Trying again in {seconds} s.",
    cloudFailed: "Could not reach Clawdline Cloud ({code}).",
    cloudRetry: "Try again",
    cloudRevoked: "This browser's device was revoked. Sign in again to read your machines.",
    cloudPairingLede: "Signed in, not paired yet",
    cloudPairingFine:
      "This browser holds no key for any machine on account {account}. Pairing a browser from this page is not in this version: pair it from the machine, then reload.",
    cloudDeviceLimit:
      "Account {account} already has {limit} viewer devices ({tier}). Removing one from this page is not in this version.",
    cloudInstallLede: "Add Clawdline to the Home Screen first",
    cloudInstallFine:
      "On an iPhone or iPad, the keys a Safari page makes stay in Safari and cannot move into the Home Screen app. Add this page to the Home Screen, open it from there, and sign in there.",
    cloudMachinesLede: "Choose a machine",
    cloudMachinesFine: "Account {account} · this browser {device}",
    cloudMachinesWaiting: "Waiting for the machines on this account to report in…",
    cloudMachinesNone: "No machine on this account has reported in yet.",
    cloudMachineSessions: "{count} sessions",
    cloudAccessProblem: "A machine's data could not be read here ({code}).",
    cloudSwitch: "Other machines",
    cloudBlocked: "This console was built for {origin} and is being served from {here}, so it does not connect.",
    cloudMisdeclared: "This console's Clawdline Cloud declaration cannot be used: {reason}",
    cloudNotCarried: "This cannot be done over Clawdline Cloud yet. Do it on the Mac itself.",
    cloudPastUnavailable:
      "This Mac cannot list its earlier sessions over Clawdline Cloud yet (it does not answer past-sessions), so none can be picked up from here. Pick it up on the Mac, or start a new session here.",
    sendUnknown: "Not known whether this reached the Mac ({code}).",
    sendLook: "Look",
    sendLookTip: "Read the conversation on the Mac again: if this is there, the card goes.",
    sendLooking: "Looking…",
    sendAbsent: "Not in the conversation on the Mac ({code}).",
    sendAgain: "Send again",
    sendAgainTip: "If the first attempt reached the Mac after all, it is not typed a second time.",
    sendInterrupted: "This page was reloaded while this was being sent, so it is not known whether it reached the Mac.",
    sendKeptWords:
      "This page kept the words when it was reloaded, but not the pictures, so this one cannot be sent again from here.",
    menuUnknown: "Not known whether that answer reached the Mac. Waiting for the session to update.",
    menuChooseAgain: "Choose again",
    menuTicked: "Ticked. Nothing is sent until you press Submit.",
    menuMoved: "The question changed before the answer landed, so nothing was typed. Read the new question and choose again.",
    menuUnverified: "This Mac cannot check which question an answer is for over Clawdline Cloud, so menus are answered on the Mac itself.",
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
    sessionGone: "網址裡的那個 session 已經不在這台機器上了，先回到清單。",
    cloudChecking: "正在確認這個瀏覽器的 Clawdline 帳號…",
    cloudSignInLede: "登入後就能讀取你的機器",
    cloudSignInFine: "Clawdline Cloud 會列出和這個瀏覽器配對過的機器與它們的 session，也會把你從這裡送出的東西帶過去。",
    cloudConnecting: "正在連上 Clawdline Cloud…",
    cloudRetrying: "Clawdline Cloud 沒有回應（{code}），{seconds} 秒後再試。",
    cloudFailed: "連不上 Clawdline Cloud（{code}）。",
    cloudRetry: "再試一次",
    cloudRevoked: "這個瀏覽器的裝置已被撤銷，請重新登入。",
    cloudPairingLede: "已登入，但還沒配對",
    cloudPairingFine: "這個瀏覽器沒有帳號 {account} 任何一台機器的金鑰。這一版還不能在這裡配對：請在那台機器上完成配對，再重新整理這一頁。",
    cloudDeviceLimit: "帳號 {account} 的觀看裝置已經有 {limit} 台（{tier}）。這一版還不能在這裡移除舊裝置。",
    cloudInstallLede: "請先把 Clawdline 加到主畫面",
    cloudInstallFine: "在 iPhone 與 iPad 上，Safari 頁面產生的金鑰搬不進主畫面的 app。請把這一頁加到主畫面，從那裡打開，在那裡登入。",
    cloudMachinesLede: "選一台機器",
    cloudMachinesFine: "帳號 {account} · 這個瀏覽器 {device}",
    cloudMachinesWaiting: "正在等這個帳號的機器回報…",
    cloudMachinesNone: "這個帳號還沒有任何機器回報。",
    cloudMachineSessions: "{count} 個 session",
    cloudAccessProblem: "有一台機器的資料在這裡讀不出來（{code}）。",
    cloudSwitch: "換一台機器",
    cloudBlocked: "這個 console 是給 {origin} 用的，現在卻從 {here} 打開，所以不會連線。",
    cloudMisdeclared: "這個 console 的 Clawdline Cloud 宣告不能用：{reason}",
    cloudNotCarried: "這件事還不能經由 Clawdline Cloud 做，請直接在 Mac 上操作。",
    cloudPastUnavailable:
      "這台 Mac 還不能經由 Clawdline Cloud 列出以前的 session（它不回答 past-sessions），所以這裡沒辦法接續。請在 Mac 上接續，或在這裡開一個新的 session。",
    sendUnknown: "不知道有沒有送到 Mac（{code}）。",
    sendLook: "去看看",
    sendLookTip: "重新讀一次 Mac 上的對話：如果已經在裡面，這張卡就會消失。",
    sendLooking: "正在看…",
    sendAbsent: "Mac 上的對話裡沒有這則（{code}）。",
    sendAgain: "再送一次",
    sendAgainTip: "如果第一次其實有送到，這次不會重打。",
    sendInterrupted: "送出到一半這一頁被重新載入了，不知道有沒有送到 Mac。",
    sendKeptWords: "重新載入時這一頁留住了文字，但沒有留住圖片，所以這一則不能從這裡再送一次。",
    menuUnknown: "不知道剛才的選擇有沒有送到 Mac，等畫面更新。",
    menuChooseAgain: "重新選擇",
    menuTicked: "已打勾。要按 Submit 才會送出。",
    menuMoved: "送到之前題目已經換了，所以什麼都沒打。請看清楚新的題目再選。",
    menuUnverified: "這台 Mac 經由 Clawdline Cloud 還無法確認答案對應哪一題，請直接在 Mac 上作答。",
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
