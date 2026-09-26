/**
 * The native settings window's words.
 *
 * Most unmarked entries below are properties of `TraditionalChinese` in the
 * Swift app's `Sources/Copy+Chinese.swift`, copied across under the same name.
 * This cross-platform app's own words are marked below and live bilingually in
 * `ownWords`; this file selects the page's language for the ones the settings
 * window uses. The distinction is explicit rather than pretending this whole
 * file is a byte-for-byte copy that no guard checks.
 *
 * **They live on this side rather than in the shell.** The five the console's
 * settings page already needed were handed over by shell/darwin (`Copy.swift`,
 * `settingsWordsScript`), which was right while the shell was the only thing
 * that had them; a whole window of words held there would mean writing them
 * again for the Linux shell and the Windows one. The shell now sends only what
 * it alone can know — what it registered, which apps are open, where the file
 * is — and the words come from here. See docs/shell-bridge.md.
 *
 * The console's own catalog (`public/strings/zh-Hant.json`, 778 keys) has none
 * of these: in the Swift app this window was never a web page.
 */
const ownWords = {
  en: {
    settingsRemoteHint:
      "When this is off, nothing outside this machine can reach it. When it is on, the session list is available on 127.0.0.1 — to a browser here, a phone connected through a tunnel, or a script. What it exposes is repository names, branches, and work titles, so it stays off until you turn it on.",
    settingsTunnel: "Connect to this machine from anywhere",
    settingsTunnelHint:
      "cloudflared connects outward from this machine — no port forwarding, and nothing listens on your network. It starts only after a device has been paired, because behind the tunnel are every repository name and work title on this machine.",
    settingsRemoteWriteHint:
      "When this is off, a device paired with a code can only read, and a browser opened with clawdline open --send keeps the sending it was given. When it is on, every paired device can send text into a session and open new sessions — which runs code on this machine, because that is what Claude Code does. It takes effect on the next request. Clawdline Cloud has its own switch, clawdline cloud commands on.",
    settingsOrchestratorMaxHint:
      "The default is five. The limit is per session, not per machine. Each child is a terminal tab with an assistant doing work in it.",
    settingsHotkeyHint:
      "The default is ⌥Space. If another app (such as Alfred) uses the same combination, both may respond and Clawdline cannot detect that; choose another combination, or narrow “Where it works” below to terminals only.",
    settingsHotkeySystem:
      "{combo} is reserved by the operating system, so it was not registered. The ✳ in Clawdline's menu still opens the input box; press the button above to choose another combination.",
    settingsHotkeyRefused:
      "{combo} could not be registered (the operating system returned {status}). The ✳ in Clawdline's menu still opens the input box; press the button above to choose another combination.",
    webCloudStatusReadFailed: "Could not read this machine's status.",
    webCloudStatusToken: "Machine credential expires: {at}",
    webCloudStatusKey: "Machine key: {key}",
    webCloudStatusMachine: "Machine {machine}",
    webCloudStatusDropped: "Machine commands dropped since {at}: {list}",
    webCloudStatusNoDrops: "This machine has dropped no commands since {at}.",
    webCloudPairNone: "No browser has been paired with this machine yet.",
    webCloudPairMachineKey:
      "This machine's key is {key}; the browser will show the same value when pairing finishes.",
    webCloudPairPinned: "Paired by this machine",
    webCloudPairRevoked: "Revoked on this machine",
    webCloudPairPinnedFailed:
      "Could not read this machine's paired-browser list, so no browser is accepted now: {why}",
    webFailMachineWritesOff: "This machine does not currently accept commands from Cloud.",
    settingsCompactWindow: "Compact Claude sessions at",
    settingsCompactWindowHint:
      "Only for Claude sessions Clawdline opens — children, Root Assignments, handoffs — never Codex, never a session you opened. Past this many tokens of context the session replaces its history with a summary: every later call re-reads fewer tokens, which is where most of a long session's cost goes, but the summary can lose detail, files get read again, and each compaction takes about ten seconds. Empty leaves Claude Code alone; it then compacts near its own 1M window. A task can ask for its own. Takes effect at the next launch.",
    settingsCompactWindowInvalid: "A whole number of tokens from {min} to {max}, or empty for none.",
    settingsAutoNameAuto: "Automatic (Claude Code, then Codex)",
  },
  "zh-Hant": {
    settingsRemoteHint:
      "關著的時候，這台機器以外的東西什麼都碰不到。打開之後，session 清單就在 127.0.0.1 上讀得到——給這裡的瀏覽器、隔著通道連進來的手機，或一支腳本。交出去的是儲存庫名稱、分支和工作標題，所以在你開口之前，它一直是關著的。",
    settingsTunnel: "從任何地方連到這台機器",
    settingsTunnelHint:
      "透過 cloudflared 從這台機器往外連出去——不必開通訊埠轉發，你的網路上也沒有東西在聽。要先配對過一台裝置它才會啟動，因為通道後面就是這台機器上每一個儲存庫名稱、每一個工作標題。",
    settingsRemoteWriteHint:
      "關著的時候，用配對碼配對的裝置只能讀；用 clawdline open --send 開的瀏覽器照樣保有當初給它的送出權限。打開之後，每一台配對過的裝置都可以把文字送進 session，也可以開新的 session——那就是在這台機器上執行程式碼，因為 Claude Code 做的就是這件事。下一個請求就生效。Clawdline Cloud 另有自己的開關：clawdline cloud commands on。",
    settingsOrchestratorMaxHint:
      "預設五個，一個 session 算一份，不是整台機器算一份。每一個都是一個終端機分頁，裡面有一個 assistant 在做事。",
    settingsHotkeyHint:
      "沒設定過就是 ⌥Space。別的 app（例如 Alfred）也用同一組時，作業系統可能讓兩邊都觸發，這裡看不出來；遇到就換一組，或把下面「在哪裡生效」縮到只剩終端機。",
    settingsHotkeySystem:
      "{combo} 是作業系統保留的快速鍵，所以沒有註冊。Clawdline 選單裡的 ✳ 一樣打得開輸入框；要換一組就按上面的按鈕。",
    settingsHotkeyRefused:
      "{combo} 註冊不起來（作業系統回 {status}）。Clawdline 選單裡的 ✳ 一樣打得開輸入框；要換一組就按上面的按鈕。",
    webCloudStatusReadFailed: "讀不到這台機器的狀態。",
    webCloudStatusToken: "機器憑證到期：{at}",
    webCloudStatusKey: "機器金鑰：{key}",
    webCloudStatusMachine: "機器 {machine}",
    webCloudStatusDropped: "自 {at} 起這台機器丟棄：{list}",
    webCloudStatusNoDrops: "自 {at} 起這台機器沒有丟棄任何指令。",
    webCloudPairNone: "還沒有任何瀏覽器配對到這台機器。",
    webCloudPairMachineKey: "這台機器的金鑰是 {key}，配對完成時瀏覽器會顯示同一組。",
    webCloudPairPinned: "由這台機器配對",
    webCloudPairRevoked: "已在這台機器撤銷",
    webCloudPairPinnedFailed: "讀不到這台機器的已配對清單，所以現在不接受任何瀏覽器：{why}",
    webFailMachineWritesOff: "這台機器目前不接受來自 Cloud 的指令。",
    settingsCompactWindow: "Claude session 壓縮門檻",
    settingsCompactWindowHint:
      "只套用在 Clawdline 開的 Claude session——child、Root Assignment、handoff——不會動到 Codex，也不會動到你自己開的 session。context 超過這麼多 token，session 就把歷史換成一份摘要：之後每一次呼叫要重讀的 token 變少，長 session 的花費大多在這裡；代價是摘要可能漏掉細節、檔案要重讀，每次壓縮大約花十秒。空白就是不插手，Claude Code 會等到接近它自己的 1M 上限才壓縮。單一任務可以另外指定。下一次開 session 時生效。",
    settingsCompactWindowInvalid: "要是 {min} 到 {max} 之間的整數 token 數，或留空表示不插手。",
    settingsAutoNameAuto: "自動（先 Claude Code，不能用時改 Codex）",
  },
} as const

type OwnWord = keyof (typeof ownWords)["en"]

function ownWord(key: OwnWord): string {
  const lang =
    (typeof document !== "undefined" && document.documentElement.lang) ||
    (typeof navigator !== "undefined" && navigator.language) ||
    "en"
  return ownWords[lang.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"][key]
}

export const W = {
  settingsTitle: "Clawdline 設定",
  settingsGeneral: "一般",
  settingsBar: "輸入條",
  settingsReading: "閱讀",
  settingsVoice: "語音輸入",
  settingsHotkey: "快速鍵",
  settingsRecording: "按下按鍵……",
  settingsScope: "在哪裡生效",
  settingsScopeGlobal: "所有 app",
  settingsScopeHint: "留空就是到處都能按，所以把最後一個移掉，上面那個開關就會自己打開——再把它關掉，清單就回來了。設定檔裡存的是 bundle id，自己手動編輯的清單一樣有效。",
  settingsSessionTerminal: "新 session 開在",
  settingsSessionTerminalHint: "跟上面熱鍵在哪裡生效是兩回事。自動是 iTerm2 開著就用它、沒開就走 tmux；選 iTerm2 就是指名要 iTerm2，旁邊有跑著的 tmux server 也不算答案，iTerm2 關著就直接拒絕；選 tmux 則是就算 iTerm2 開著也一律開在 tmux server 裡。下一個開的 session 就會照這個走，不用重開 app。",
  settingsScopeAdd: "加入 app……",
  settingsScopeChoose: "選擇其他 app……",
  settingsScopeRunning: "現在開著的",
  settingsScopeRemove: "移除",
  settingsLanguage: "語言",
  settingsReopen: "輸入條跟著終端機出現和收起",
  settingsReopenHint: "從終端機切到別的地方，輸入條就收起來；切回終端機，它就回來。Esc 才是「這次用完了」的意思。",
  settingsFollow: "終端機顯示輸入條指著的那個 session",
  settingsFollowHint: "它會把那個 session 的分頁選起來。它不會把終端機叫到最前面——不然每按一次 Tab，鍵盤就從你正在打字的那個框裡跑掉了。",
  settingsCodexAutoName: "自動命名新的 session",
  settingsCodexAutoNameHint: "每個尚未命名的 session 會由你選的助理跑一次小型 turn。它使用這個 session 的需求與助理最新的回覆、消耗該助理的額度，而且不會蓋掉你自己取的名稱。",
  settingsNotch: "住在瀏海裡",
  settingsNotchHint: "在鏡頭那塊住一隻角色。關掉就是真的關掉——什麼都不畫，視窗也不會建立。",
  settingsPosition: "在螢幕上的高度",
  settingsWidth: "寬度",
  settingsOpacity: "卡片不透明度",
  settingsShow: "顯示",
  settingsPaneHeight: "面板高度",
  settingsTextSize: "文字大小",
  settingsPaneFont: "面板字型",
  settingsBlur: "背後模糊",
  settingsNewestFirst: "最新的在最上面",
  settingsEngine: "辨識引擎",
  settingsSettle: "停頓多久算一句話結束",
  settingsStop: "安靜多久算整段結束",
  settingsAuto: "自動",
  settingsTranscript: "對話記錄",
  settingsTerminal: "終端機畫面",
  settingsOff: "關閉",
  settingsHooks: "Claude Code Hook",
  settingsHooksHint: "裝上之後，一輪對話開始、結束、或是需要你回答的當下，Claude Code 會直接說一聲，不必等 Clawdline 下一次去看。每一次判讀仍然來自畫面，這裡只決定判讀發生得多快。",
  settingsHooksInstall: "安裝",
  settingsHooksRemove: "移除",
  settingsHooksOff: "未安裝——狀態全部從畫面讀",
  settingsHooksOn: "已安裝——還沒有 session 回報過",
  settingsHooksLive: "已安裝，session 正在回報",
  settingsStateHook: "session 換狀態的時候",
  settingsStateHookHint: "只要有 session 開始跑、跑完、或是要問你話，Clawdline 就會執行這個：你自己的程式，細節放在它的環境變數裡。它寫在設定檔而不是這裡，因為那是一串 argv、不是一行命令列——路徑裡有空格，它也還是一整個路徑。",
  settingsRemote: "遠端",
  settingsRemoteServe: "讓瀏覽器或你的手機看得到你的 session",
  settingsRemoteHint: ownWord("settingsRemoteHint"),
  settingsTunnel: ownWord("settingsTunnel"),
  settingsTunnelQuick: "自動產生的網址",
  settingsTunnelNamed: "我自己的網域",
  settingsTunnelHostname: "主機名稱",
  settingsTunnelHint: ownWord("settingsTunnelHint"),
  settingsRemoteWrite: "讓配對過的裝置寫進 session",
  settingsRemoteWriteHint: ownWord("settingsRemoteWriteHint"),
  settingsPushDelivery: "session 回報交件時通知我",
  settingsPushDeliveryHint: "一次回報通知一次，同一份重複回報不會再響。",
  settingsPushFanout: "一批派出去的任務全部結束時通知我",
  settingsPushFanoutHint: "整批只通知一次，會說有幾件失敗。",
  settingsSmartNotifications: "智慧通知",
  settingsSmartNotificationsHint: "用 Haiku 說明剛完成了什麼；產生失敗時仍會送出原本的通知。",
  settingsPushDeploy: "deploy 結束時通知我",
  settingsPushDeployHint: "成功和失敗都會通知。",
  settingsAgentNotify: "允許 agent 主動推播",
  settingsAgentNotifyNote: "只管 agent 主動送出的內容；工作完成、deploy 與其他通知都不受影響。",
  settingsOrchestrator: "派工作給別的 session",
  settingsOrchestratorEnabled: "讓一個 session 把工作派給另一個",
  settingsOrchestratorEnabledHint: "帶著 clawdline skill 的 session 可以開一個新分頁、把指示打進去，做完再回頭跟它說一聲。關著的時候，每一次派工都會被擋下來——已經在跑的不會被停掉。",
  settingsOrchestratorMax: "同時最多幾個子 session",
  settingsOrchestratorMaxHint: ownWord("settingsOrchestratorMaxHint"),
  settingsOrchestratorPermission: "child 可以自己走多遠",
  settingsOrchestratorPermissionHint: "child 的分頁沒有人在看，停下來等核准的 session 會一路停到逾時——而派出去的 session 整份工作就是跑指令和寫檔案，所以只要不是最後一格，它就會停在某個地方。任務可以要求比這裡保守，但不能超過。",
  settingsOrchestratorPermissionAsk: "每一步都先問",
  settingsOrchestratorPermissionEdits: "寫檔案不用問",
  settingsOrchestratorPermissionFull: "都不要問",
  settingsOrchestratorNotify: "做完之後回報給發派的 session",
  settingsOrchestratorNotifyHint: "工作結束的時候，往那個 session 裡打一行字。",
  settingsOrchestratorClose: "子 session 回報之後",
  settingsOrchestratorCloseHint: "回報過了就把它的分頁關掉——逾時的那個會留著，讓你自己看是怎麼回事。",
  settingsOrchestratorCloseNow: "馬上把分頁關掉",
  settingsOrchestratorCloseLinger: "三分鐘後再關",
  settingsOrchestratorCloseKeep: "留著不要關",
  // This build's own (ownWords): the Swift app never set this.
  settingsCompactWindow: ownWord("settingsCompactWindow"),
  settingsCompactWindowHint: ownWord("settingsCompactWindowHint"),
  settingsCompactWindowInvalid: ownWord("settingsCompactWindowInvalid"),
  settingsAutoNameAuto: ownWord("settingsAutoNameAuto"),
  menuMascot: "吉祥物",

  // **Not from the Swift app.** Its hotkey row never had to say that macOS
  // itself already uses a combination, that the retired app is holding the same
  // one, or that a default can be shared with another app without either being
  // told; and its hook row describes a hook this build does not install. These
  // are this build's own words, kept here so nothing on screen is spelled twice.
  // A failure is one sentence and the way round it — the menu bar mark, which
  // is the Swift app's own answer in `hotkeyFailedBody` — and never a path.
  settingsHotkeyHint: ownWord("settingsHotkeyHint"),
  settingsHotkeySystem: ownWord("settingsHotkeySystem"),
  settingsHotkeyRefused: ownWord("settingsHotkeyRefused"),
  settingsHotkeyUnreadable: "設定檔裡的 hotkey「{spec}」讀不懂，所以沒有註冊。選單列的 ✳ 一樣打得開輸入框；按上面的按鈕重錄一組。",
  settingsHotkeyLegacy: "舊版 Clawdline 也開著、也用 {combo}，按一下會打開兩個輸入框。結束舊版就好；它若是開機自動啟動，在它的選單取消「開機時啟動」。",
  settingsHooksNone: "這個版本不裝 hook——session 的狀態從 Claude Code 自己的狀態檔讀",
  settingsHooksStray: "這個檔裡有 clawdline-next/hook.sh 的項目，但這個版本沒有東西在讀它。",
  settingsHooksLegacy:
    "這個檔裡還掛著舊版的 hook（clawdline/hook.sh）。Claude Code 每一輪照樣會執行它，但這個版本不讀它留下的紙條——不影響任何功能，只是每次多跑一個小命令。要拿掉，從這個檔刪掉含 clawdline/hook.sh 的項目，或在舊版的設定裡按「移除」（它只移除自己的項目）。",
  settingsHooksWhatHead: "它是什麼",
  settingsHooksWhat:
    "Claude Code 在特定時刻自己執行的命令，登記在 ~/.claude/settings.json。舊版 Clawdline 裝的那一支，會在一輪開始、一輪結束、跳出權限對話框、或問你問題的當下留一張紙條；舊版看到紙條就馬上去讀那個 session 的畫面，不必等下一輪檢查。舊版沒裝的時候，輸入框收著要 20 秒才檢查一次，所以權限對話框可能 20 秒後才出現在選單列；裝了不到一秒。",
  settingsHooksWithoutHead: "不裝的時候",
  settingsHooksWithout:
    "Claude Code 自己會把每個 session 正在做什麼（busy、idle、waiting）寫進 ~/.claude/sessions/，這個版本大約每 2 秒讀一次。session 清單上的「在跑／等你回答／閒著」、選單列上的計數、瀏海的動畫、等你回答時列出的選項，都是從這裡來的，不需要 hook。跟舊版裝了 hook 比，差別只在時間：狀態一變，舊版不到一秒就知道，這個版本最多晚 2 秒左右。沒有狀態檔可讀的 session（例如 Codex）改從終端機畫面判讀。",
  settingsHooksWhyHead: "為什麼這個版本沒有",
  settingsHooksWhy:
    "裝 hook 等於改 Claude Code 的設定檔，讓它每一輪在八個時刻多跑一個命令。這個版本還沒有讀那些紙條的程式，裝了只會多跑沒人看的命令，所以不提供安裝，也不會去改 ~/.claude/settings.json。讀紙條的那一半做好之後，這裡才會出現安裝按鈕。",

  // The Cloud status card's words. Same rule as everything above: each is a
  // property of `Copy+Chinese.swift`, copied under its own name. They are the
  // hosted console's Cloud status sheet's words (`webCloudStatus*`, :1132-1151)
  // rather than an invented settings vocabulary, because this card answers the
  // same questions about the same line and the original has no second spelling.
  webCloudStatus: "Cloud 狀態",
  webCloudStatusReadFailed: ownWord("webCloudStatusReadFailed"),
  webCloudStatusConnection: "連線：{state}",
  webCloudStatusClosed: "上次關閉：{code}",
  webCloudStatusToken: ownWord("webCloudStatusToken"),
  webCloudStatusKey: ownWord("webCloudStatusKey"),
  webCloudStatusMac: ownWord("webCloudStatusMachine"),
  webCloudStatusDropped: ownWord("webCloudStatusDropped"),
  webCloudStatusNoDrops: ownWord("webCloudStatusNoDrops"),

  // Pairing a browser with this machine. These are this build's own words rather
  // than the hosted console's, except `pairingScanTitle`, which is the Swift
  // app's own title over its pairing QR. The QR is drawn in this window now
  // (PairingQr.tsx), so the words lead with the code and keep the link for a
  // browser that has no camera. The order-of-steps lines restate the hosted
  // console's install boundary (`cloud-onboarding.js`), and the one button they
  // name is quoted in that console's own words, which are English.
  pairingScanTitle: "用手機掃這張 QR code",
  webCloudPair: "配對瀏覽器",
  webCloudPairNone: ownWord("webCloudPairNone"),
  webCloudPairStart: "產生配對 QR",
  webCloudPairAgain: "換一張",
  webCloudPairCancel: "停止等待",
  webCloudPairOpen:
    "電腦或 Android 的瀏覽器也可以直接打開這個連結，再登入同一個 Clawdline Cloud 帳號。iPhone 不要用 Safari 開，照上面的順序在主畫面 app 裡掃。",
  webCloudPairCopy: "複製連結",
  webCloudPairCopied: "已複製",
  webCloudPairEnlarge: "點一下 QR 可以放到最大",
  webCloudPairRemaining: "還剩 {time}（{at} 失效）。",
  webCloudPairRemainingShort: "還剩 {time}",
  webCloudPairRenewed: "已自動換過 {count} 次。",
  webCloudPairExpired: "這張 QR 已經過期了。",
  webCloudPairRenewing: "正在換一張新的⋯⋯",
  webCloudPairGaveUp: "已經自動換了 {count} 次都沒有人掃，先停在這裡；要掃的時候再按一下。",
  webCloudPairRenew: "換一張新的 QR",
  webCloudPairClose: "點任何地方或按 Esc 關閉",
  webCloudPairOrderHead: "iPhone／iPad 一定要照這個順序，不然會白做：",
  webCloudPairOrderInstall: "用 Safari 打開 app.clawdline.com，按「分享」→「加入主畫面」。",
  webCloudPairOrderOpen: "關掉 Safari，從主畫面打開 Clawdline。",
  webCloudPairOrderSignIn: "在這個主畫面 app 裡登入同一個 Clawdline Cloud 帳號。",
  webCloudPairOrderScan: "按「Scan the QR on the Mac」，對準這張 QR。",
  webCloudPairOrderWhy:
    "不要用相機 app 或 Safari 直接掃：主畫面 app 的儲存跟 Safari 是分開的，金鑰又不能匯出，在 Safari 配好的，裝成 app 之後就不見了。",
  webCloudPairOrderOther: "Android 或電腦：用相機掃、或直接打開連結，登入同一個帳號就好。",
  webCloudPairMachineKey: ownWord("webCloudPairMachineKey"),
  webCloudPairWaiting: "等待瀏覽器回應⋯⋯",
  webCloudPairSealing: "收到瀏覽器的配對資料，正在交接金鑰⋯⋯",
  webCloudPairDone: "已配對 {device}（{key}）。",
  webCloudPairFailed: "配對沒有完成：{why}",
  webCloudPairCodeHead: "另一條路：貼上瀏覽器的配對碼",
  webCloudPairCodeLabel: "瀏覽器顯示的配對碼",
  webCloudPairCodeHint:
    "沒有鏡頭的電腦瀏覽器走這條：在那個瀏覽器打開 app.clawdline.com 並登入，把它顯示的「Pairing code」整串貼到這裡。配對碼 10 分鐘內有效，比 QR 寬鬆，走的是同一套加密。",
  webCloudPairCodeSend: "用這串配對碼配對",
  webCloudPairPinned: ownWord("webCloudPairPinned"),
  webCloudPairRoster: "只在帳號清單上",
  webCloudPairRevoked: ownWord("webCloudPairRevoked"),
  webCloudPairRevoke: "撤銷",
  webCloudPairPinnedFailed: ownWord("webCloudPairPinnedFailed"),
  webFailMacWritesOff: ownWord("webFailMachineWritesOff"),

  // **Not from the Swift app.** Its voice tab had no language row — the key was
  // only ever hand-edited, and `auto` meant whisper's own habit, which writes
  // Chinese in Simplified. These are this build's own words for the row that
  // replaced the hand edit, and for saying where `auto` landed and why.
  settingsVoiceLanguage: "辨識語言",
  settingsVoiceLanguageFollow: "跟著 Clawdline",
  settingsVoiceLanguageHint:
    "語音是在這台機器上用 whisper 讀的，不管錄音是從哪個瀏覽器、哪支手機送來。選「跟著 Clawdline」就是：先看「一般」分頁的語言；那裡也是自動，就看這台機器的語言與地區設定；都沒說，就用 Clawdline 介面本身的語言。",
  voiceLanguageFixed: "每段錄音都當成{name}來讀。",
  voiceLanguageChinese: "現在是{name}——{why}。",
  voiceLanguageDetect: "語言交給 whisper 自己判斷——{why}。聽到中文時寫成{script}——{scriptWhy}。",
  voiceLanguageDetectBare: "語言交給 whisper 自己判斷——{why}。沒有任何設定說中文該用哪一種字，whisper 習慣寫成簡體。",
  voiceLanguageNobody: "語言交給 whisper 自己判斷。沒有任何設定說中文該用哪一種字，whisper 習慣寫成簡體。",
  voiceWhyLanguage: "「一般」分頁的語言是{tag}",
  voiceWhyMachine: "Clawdline 的語言是自動，而這台機器的{where}是{tag}",
  voiceWhyCatalog: "Clawdline 的語言是自動，這台機器也沒說它用什麼語言，所以跟著 Clawdline 介面本身的語言",
  voiceWhyChosen: "這裡選的是{tag}",
} as const

/**
 * Where `auto` landed, said in one sentence under the voice language picker.
 *
 * The daemon decides (`GET /v1/voice/language`, whisper/locale.go) and this only
 * puts it into words: the picker's value alone cannot say it, because `auto`
 * on this machine and `auto` on another are different answers.
 */
export function voiceLanguageSaid(v: {
  setting: string
  code: string
  script: string
  source: string
  tag: string
  script_source: string
  script_tag: string
}): string {
  const tag = (value: string) => languageName(value)
  const why = (source: string, value: string): string => {
    switch (source) {
      case "voice_language":
        return fill(W.voiceWhyChosen, { tag: tag(value) })
      case "language":
        return fill(W.voiceWhyLanguage, { tag: tag(value) })
      case "catalog":
        return W.voiceWhyCatalog
      case "AppleLanguages":
      case "AppleLocale":
        return fill(W.voiceWhyMachine, { where: "語言與地區設定", tag: tag(value) })
      case "Windows":
        return fill(W.voiceWhyMachine, { where: "Windows 地區設定", tag: tag(value) })
      default:
        // LANG, LC_ALL, LC_MESSAGES, LANGUAGE: the variable is named, because
        // it is the thing somebody would go and change. The leading space is
        // the one between Han and Latin.
        return fill(W.voiceWhyMachine, { where: ` ${source} 環境變數`, tag: tag(value) })
    }
  }
  const script = (value: string) => (value === "Hant" ? "繁體" : "簡體")
  if (v.setting !== "auto" && v.source === "voice_language") {
    return fill(W.voiceLanguageFixed, {
      name: v.code === "zh" ? `${script(v.script)}中文` : tag(v.tag),
    })
  }
  if (v.code === "zh") {
    return fill(W.voiceLanguageChinese, { name: `${script(v.script)}中文`, why: why(v.source, v.tag) })
  }
  if (!v.source) return W.voiceLanguageNobody
  if (!v.script) return fill(W.voiceLanguageDetectBare, { why: why(v.source, v.tag) })
  return fill(W.voiceLanguageDetect, {
    why: why(v.source, v.tag),
    script: script(v.script),
    scriptWhy: why(v.script_source, v.script_tag),
  })
}

/**
 * A language tag's name in this window's language, from the platform's own
 * table as the General tab's picker names them. A POSIX spelling (`zh_TW.UTF-8`)
 * is read as the tag it means; a runtime without the table shows the tag.
 */
export function languageName(value: string): string {
  const tag = value.split(/[.@]/)[0]!.replace(/_/g, "-")
  try {
    return new Intl.DisplayNames(["zh-Hant"], { type: "language" }).of(tag) ?? value
  } catch {
    return value
  }
}

/**
 * `fill(_:_:)` (`view/cloud-status.js:64`): the `{name}` placeholders the Cloud
 * words carry. One spelling, because a template filled two ways is two
 * templates.
 */
export function fill(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (whole, name: string) =>
    Object.hasOwn(values, name) ? values[name]! : whole,
  )
}

/** `settingsSeconds(_:)`, the same `%.1f 秒`. */
export function seconds(value: number): string {
  return `${value.toFixed(1)} 秒`
}

/** `Assistant.label` (`Assistant.swift`), the two product names as they are written there. */
export const ASSISTANT_LABEL = { claude: "Claude Code", codex: "Codex" } as const

/**
 * `dictationStatus(_:)`, composed here from the reading the shell sends rather
 * than from a sentence it wrote: the words are this side's, the fact is the
 * machine's.
 */
export function dictationStatus(status: { kind: string; model?: string }): string {
  switch (status.kind) {
    case "ready":
      return `語音：Apple，之後 Whisper（${status.model ?? ""}）`
    case "noModel":
      return "語音：只有 Apple——whisper-cli 有了，缺模型"
    default:
      return "語音：只有 Apple——沒有 whisper-cli"
  }
}

/** `hotkeyFailedTitle(_:)` (`Copy+Chinese.swift`). */
export function hotkeyFailedTitle(combo: string): string {
  return `${combo} 註冊不起來`
}

/**
 * What is said under the hotkey chip for the shell's `trouble` reading. A shell
 * that says `failed` without saying why gets the Swift app's own title, which
 * is all it could say.
 */
export function hotkeyTrouble(
  trouble: { kind: string; status?: number } | null | undefined,
  combo: string,
  spec: string,
): string {
  switch (trouble?.kind) {
    case "system":
      return fill(W.settingsHotkeySystem, { combo })
    case "refused":
      return fill(W.settingsHotkeyRefused, { combo, status: String(trouble.status ?? "") })
    case "unreadable":
      return fill(W.settingsHotkeyUnreadable, { spec })
    case "legacy":
      return fill(W.settingsHotkeyLegacy, { combo })
    default:
      return hotkeyFailedTitle(combo)
  }
}
