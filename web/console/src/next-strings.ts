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
      "This browser holds no key for any machine on account {account} yet. Pair one from here: this page gives you one line to run on it.",
    // Pairing from this page (`cloud/pair.ts`). The code is this browser's
    // offer; the machine is what hands the key over, so the person carries the
    // code there, and compares the two fingerprints both sides print.
    cloudPair: "Pair",
    cloudPairOne: "Pair {machine} with this browser",
    cloudPairStart: "Pair a machine",
    cloudPairTitle: "Pair {machine} with this browser",
    cloudPairTitleAny: "Pair a machine with this browser",
    cloudPairWhy:
      "Signing in is not enough to read a machine: the machine hands its key over itself, to a code it was given on the machine. That is what keeps an account password from being a way into your terminals.",
    cloudPairAsking: "Asking Clawdline Cloud for a pairing code…",
    cloudPairRun: "Run this line in a terminal on {machine} (over SSH is fine):",
    cloudPairRunAny: "Run this line in a terminal on the machine to pair (over SSH is fine):",
    cloudPairSettings:
      "A machine with Clawdline's settings window can take the part after -offer in its field for the browser's pairing code instead.",
    cloudPairCopy: "Copy",
    cloudPairCopied: "Copied.",
    cloudPairCopyFailed: "This browser would not copy it. Select the line and copy it by hand.",
    cloudPairYourKey: "This browser's fingerprint: {fingerprint}",
    cloudPairYourKeyCheck:
      "When the machine finishes, it prints a fingerprint on its browser line. It must be this one.",
    cloudPairExpires: "The code works until {time}.",
    cloudPairWaiting: "Waiting for the machine to answer…",
    cloudPairStopped: "Stopped waiting. The code still works until {time}; nothing here can withdraw it.",
    cloudPairStoppedEarly: "Stopped waiting.",
    cloudPairDone: "Paired. This browser now holds {machine}'s key.",
    cloudPairMachineKey: "{machine}'s fingerprint: {fingerprint}",
    cloudPairMachineKeyCheck:
      "The machine printed its own on its machine line; the two must be the same. If they are not, the code reached another machine: do not use this pairing.",
    cloudPairOther: "The machine that answered is {other}, not {machine}. {other} is the one paired.",
    cloudPairReload: "Reload and read it",
    cloudPairExpired: "The code ran out before the machine answered. Nothing was paired.",
    cloudPairAgain: "New code",
    cloudPairRefused:
      "That was not this browser's to finish ({code}): the pairing belongs to another account or was already used. Nothing was paired.",
    cloudPairFailed: "Pairing did not finish ({code}). If the machine printed a reason, that one says more.",
    cloudPairLinkLede: "This page was opened from a machine's pairing link",
    cloudPairLinkFine:
      "Pairing hands that machine's key to this browser. Answer only a link you just took from a machine of your own.",
    cloudPairLinkGo: "Pair with it",
    cloudPairLinkWaiting: "Answered. Waiting for the machine to hand over its key…",
    cloudPairLinkExpired:
      "That link has run out: a machine's link lasts about three minutes. Pair from the machine list instead, whose code lasts longer and says until when.",
    cloudPairLinkBad: "That is not a pairing link this page can read ({code}).",
    cloudMachineSessionsUnread: "its sessions cannot be read here",
    cloudMachineSessionsUnknown: "sessions not known yet",
    devicesPairHelp: "Press Pair: this page gives you one line to run on that machine.",
    cloudDeviceLimit:
      "Account {account} already has {limit} viewer devices ({tier}). Removing one from this page is not in this version.",
    cloudInstallLede: "Add Clawdline to the Home Screen first",
    cloudInstallFine:
      "On an iPhone or iPad, the keys a Safari page makes stay in Safari and cannot move into the Home Screen app. Add this page to the Home Screen, open it from there, and sign in there.",
    cloudMachinesLede: "Choose a machine",
    cloudMachinesFine: "Account {account} · this browser {device}",
    cloudMachinesWaiting: "Waiting for the machines on this account to report in…",
    cloudMachinesNone: "No machine on this account has reported in yet.",
    // Two different things had one sentence between them. The copied catalog's
    // `webEmptyWaitTitle` ("Waiting for the app") is the honest one for a page
    // with no line to anything; it was also being said to a page whose line is
    // up and whose machine simply has not stated its list yet, which is a
    // different fact and a different thing to do about it.
    sessionsListWaitTitle: "Waiting for this machine's list",
    sessionsListWaitHint: "The line is up. This machine has not said yet which sessions it has.",
    cloudMachineSessions: "{count} sessions",
    cloudAccessProblem: "A machine's data could not be read here ({code}).",
    cloudSwitch: "Other machines",
    cloudForget: "Forget",
    cloudForgetOne: "Forget {machine}",
    cloudForgetTitle: "Forget {machine}?",
    cloudForgetAsk:
      "{machine} is removed from this account. This cannot be undone from here: the machine comes back only if you sign it in again on the machine itself, and it comes back as a new machine with a new id.",
    cloudForgetAskCurrent: "This machine reported in a moment ago. Forgetting it stops its connection now.",
    cloudForgetAskStale: "This machine has not reported in for a while.",
    cloudForgetAskUnknown: "This machine has never reported in where this browser could see it.",
    cloudForgetHonest:
      "Routing stops now. A device that already holds the master key can still open ciphertext it recorded earlier; key rotation happens lazily.",
    cloudForgetting: "Forgetting {machine}…",
    cloudForgotten: "{machine} was forgotten. It is no longer on this account.",
    cloudForgottenRow: "forgotten",
    cloudForgetSaid: "Clawdline Cloud said: {note}",
    cloudForgetHonestUnread:
      "Clawdline Cloud did not say what this does to keys already handed out, so this page does not say it either.",
    cloudForgetRefused:
      "Clawdline Cloud refused to forget {machine} ({code}). This browser is not signed in to that account any more. Nothing was changed.",
    cloudForgetAbsent: "This account has no machine {machine} ({code}). Nothing was changed.",
    cloudForgetUnreadable:
      "Clawdline Cloud's answer about forgetting {machine} could not be read ({code}), so it is not known whether it was forgotten. Reload this page before asking again.",
    // Renaming a machine. It is an account write like forgetting one and it is
    // asked the same way, but it is not destructive, so what it says after is
    // about where the new name has and has not arrived yet.
    cloudRename: "Rename",
    cloudRenameOne: "Rename {machine}",
    cloudRenameTitle: "Rename {machine}?",
    cloudRenameAsk:
      "This changes what {machine} is called on this account, for every browser signed in to it. It changes nothing on the machine itself.",
    cloudRenameLater:
      "This list is what the machines themselves last said about themselves, so it goes on showing the old name until this one reports in again.",
    cloudRenameField: "New name",
    cloudRenaming: "Renaming {machine}…",
    cloudRenamed: "{machine} is called {name} on this account now.",
    cloudRenameRefused:
      "Clawdline Cloud refused to rename {machine} ({code}). This browser is not signed in to that account any more. Nothing was changed.",
    cloudRenameAbsent: "This account has no machine {machine} ({code}). Nothing was changed.",
    cloudRenameUnreadable:
      "Clawdline Cloud's answer about renaming {machine} could not be read ({code}), so it is not known whether the name was changed. Reload this page and look at the list before asking again.",
    cloudRenameTooLong: "A machine's name is at most {max} characters on this account.",
    cloudBlocked: "This console was built for {origin} and is being served from {here}, so it does not connect.",
    // The Devices page's own sentence about the list under it. The copied
    // catalog has one (`webDevicesLede`) and it describes an account; only one
    // of the two consoles here has an account's list to show, so the other
    // says what it does have (`legacy/devices-bridge.ts`).
    devicesLedeAccount:
      "The machines on this account, their status, and whether this browser can read them.",
    devicesLedeThisMachine:
      "Only the machine serving this page. A console served by the daemon reaches no other machine; which machines are on your account is answered by the Clawdline Cloud console.",
    devicesThisMachine: "This machine",
    cloudMisdeclared: "This console's Clawdline Cloud declaration cannot be used: {reason}",
    cloudNotCarried: "This cannot be done over Clawdline Cloud yet. Do it on the machine itself.",
    cloudPastUnavailable:
      "This machine cannot list its earlier sessions over Clawdline Cloud yet (it does not answer past-sessions), so none can be picked up from here. Pick it up on the machine, or start a new session here.",
    sendUnknown: "Not known whether this reached the machine ({code}).",
    sendLook: "Look",
    sendLookTip: "Read the conversation on the machine again: if this is there, the card goes.",
    sendLooking: "Looking…",
    sendAbsent: "Not in the conversation on the machine ({code}).",
    sendAgain: "Send again",
    sendAgainTip: "If the first attempt reached the machine after all, it is not typed a second time.",
    sendInterrupted: "This page was reloaded while this was being sent, so it is not known whether it reached the machine.",
    sendKeptWords:
      "This page kept the words when it was reloaded, but not the pictures, so this one cannot be sent again from here.",
    menuUnknown: "Not known whether that answer reached the machine. Waiting for the session to update.",
    menuChooseAgain: "Choose again",
    menuTicked: "Ticked. Nothing is sent until you press Submit.",
    newBuild: "A newer Clawdline is being served. This page is still the older one.",
    newBuildReload: "Reload",
    menuMoved: "The question changed before the answer landed, so nothing was typed. Read the new question and choose again.",
    menuUnverified: "This machine cannot check which question an answer is for over Clawdline Cloud, so menus are answered on the machine itself.",
    // The Links sheet says which kind of nothing it found. The copied catalog
    // has one sentence for an empty list and one for a failed read, and no way
    // to say that the list is right and only the deploy could not be named —
    // which is the distinction between a project with no CI and a machine
    // whose git is wedged.
    linksNotRepository: "Not a git repository, so nothing here could name a workflow run. Everything else on this list was read.",
    linksNoRemote: "This repository has no origin remote, and a workflow run is named after one, so no deploy could be found. Everything else on this list was read.",
    linksRemoteNotGitHub: "This repository's origin is not on GitHub, and only a GitHub remote names a workflow run here, so no deploy could be found.",
    linksGitUnreadable: "git would not answer here ({reason}), so whether this project has a deploy is unknown — which is not the same as no.",
    linksGitMissing: "there is no git on this machine",
    linksGitTimeout: "it did not answer in time",
    linksGitFailed: "it refused",
    linksGitTooLarge: "it said more than this reads",
    // The fifth kind of nothing, and the one that had no sentence at all: the
    // repository *is* on GitHub, git answered, and the tool that polls the
    // workflow runs still produced no row. That tool writes down why; until
    // now nothing read the key, so a person was shown a blank cell beside a
    // file that had been explaining itself for days.
    linksDeployNoRun:
      "The status line's tool has no workflow run to show for this repository (it wrote `{state}`). {reason}",
    linksDeployNoPage:
      "The status line's tool says this repository's newest workflow run is `{state}` and gave no page to open it on, so it is not a row here. {reason}",
    linksDeployNoFile:
      "This repository is on GitHub and nothing has written a workflow reading for it on this machine — so there is no deploy row because nobody is looking, not because there is no run. It is the status line's command that writes these, not this app.",
    linksDeployUnreadable:
      "There is a workflow file for this repository on this machine and it could not be read as one small JSON object, so whether there is a deploy is unknown — which is not the same as no.",
    linksDeployBecause: "Its reason: {reason}.",
    linksDeployNoWhy: "It did not say why.",
    // That tool's own words for having nothing to show (`gh-run-status.py`).
    // The list is its to grow, so an unknown one is said as it was written
    // rather than swallowed — the whole shape this section exists to end.
    linksDeployWhyNoGh: "there is no `gh` command on this machine to ask GitHub with",
    linksDeployWhyNoBranch: "it could not get a branch name here, so there was nothing to look up",
    linksDeployWhyGhFailed: "`gh` did not answer — not signed in, timed out, or refused",
    linksDeployWhyNoRuns: "this branch has no workflow run at all",
    linksDeployWhyWorkflowDisabled:
      "that workflow is disabled, so its last failure will never be replaced and is not news",
    linksDeployWhyStaleFail:
      "the last run failed long enough ago that it stopped counting as the current state",
    linksDeployWhyUnknown: "That tool gave a reason this app does not know: `{why}`.",
    linksDeployWhen: "That tool last wrote this down: {when}.",
    linksTruncated: "More addresses were found than this list carries; the rest are not shown.",
    // The swipe. A confirmation reached from the list has to name what it
    // would act on: "this session" is enough beside the conversation and
    // nothing at all beside thirteen rows.
    endNamedTitle: "Close {session}?",
    swipeEndLabel: "Close {session}",
    swipeWhyLabel: "{session}: {why}. Read why before closing it.",
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
    cloudPairingFine: "這個瀏覽器還沒有帳號 {account} 任何一台機器的金鑰。從這裡配對：這一頁會給你一行指令，到那台機器上執行。",
    cloudPair: "配對",
    cloudPairOne: "把 {machine} 和這個瀏覽器配對",
    cloudPairStart: "配對一台機器",
    cloudPairTitle: "把 {machine} 和這個瀏覽器配對",
    cloudPairTitleAny: "把一台機器和這個瀏覽器配對",
    cloudPairWhy:
      "只登入帳號還讀不到機器：金鑰是那台機器自己交出來的，而且只交給在那台機器上收到的代碼。這樣帳號密碼就不會變成進你終端機的門。",
    cloudPairAsking: "正在跟 Clawdline Cloud 要一組配對代碼…",
    cloudPairRun: "在 {machine} 的終端機裡執行這一行（用 SSH 連進去也可以）：",
    cloudPairRunAny: "在要配對的那台機器的終端機裡執行這一行（用 SSH 連進去也可以）：",
    cloudPairSettings: "有 Clawdline 設定視窗的機器，也可以把 -offer 後面那一串貼進「瀏覽器顯示的配對碼」那一欄。",
    cloudPairCopy: "複製",
    cloudPairCopied: "複製好了。",
    cloudPairCopyFailed: "這個瀏覽器不讓這一頁複製，請手動選取那一行。",
    cloudPairYourKey: "這個瀏覽器的指紋：{fingerprint}",
    cloudPairYourKeyCheck: "那台機器配好時，會在 browser 那一行印出一組指紋，必須和這一組相同。",
    cloudPairExpires: "這組代碼到 {time} 為止有效。",
    cloudPairWaiting: "正在等那台機器回應…",
    cloudPairStopped: "已經不等了。這組代碼到 {time} 以前仍然有效，這裡沒有辦法把它作廢。",
    cloudPairStoppedEarly: "已經不等了。",
    cloudPairDone: "配對好了，這個瀏覽器現在有 {machine} 的金鑰。",
    cloudPairMachineKey: "{machine} 的指紋：{fingerprint}",
    cloudPairMachineKeyCheck: "那台機器在 machine 那一行印出了它自己的指紋，兩組必須相同。不一樣就表示代碼到了別台機器：不要用這次配對。",
    cloudPairOther: "回應的是 {other}，不是 {machine}；配對好的是 {other}。",
    cloudPairReload: "重新載入，開始讀取",
    cloudPairExpired: "那台機器回應之前，這組代碼就過期了，什麼都沒有配對。",
    cloudPairAgain: "產生新的代碼",
    cloudPairRefused: "這次配對不是這個瀏覽器能完成的（{code}）：它屬於別的帳號，或已經用過了。什麼都沒有配對。",
    cloudPairFailed: "配對沒有完成（{code}）。那台機器如果有印出原因，那一句說得比較完整。",
    cloudPairLinkLede: "這一頁是從一台機器的配對連結打開的",
    cloudPairLinkFine: "配對會把那台機器的金鑰交給這個瀏覽器。只回應你剛剛從自己機器上拿到的連結。",
    cloudPairLinkGo: "和它配對",
    cloudPairLinkWaiting: "已經回應，正在等那台機器交出金鑰…",
    cloudPairLinkExpired: "這個連結已經過期了：機器印出的連結只有大約三分鐘。改從機器清單配對，那裡的代碼有效得比較久，也會寫出到幾點。",
    cloudPairLinkBad: "這不是這一頁讀得懂的配對連結（{code}）。",
    cloudMachineSessionsUnread: "這裡讀不到它的 session",
    cloudMachineSessionsUnknown: "還不知道有幾個 session",
    devicesPairHelp: "按「配對」，這一頁會給你一行指令，到那台機器上執行。",
    cloudDeviceLimit: "帳號 {account} 的觀看裝置已經有 {limit} 台（{tier}）。這一版還不能在這裡移除舊裝置。",
    cloudInstallLede: "請先把 Clawdline 加到主畫面",
    cloudInstallFine: "在 iPhone 與 iPad 上，Safari 頁面產生的金鑰搬不進主畫面的 app。請把這一頁加到主畫面，從那裡打開，在那裡登入。",
    cloudMachinesLede: "選一台機器",
    cloudMachinesFine: "帳號 {account} · 這個瀏覽器 {device}",
    cloudMachinesWaiting: "正在等這個帳號的機器回報…",
    cloudMachinesNone: "這個帳號還沒有任何機器回報。",
    sessionsListWaitTitle: "在等這台機器的清單",
    sessionsListWaitHint: "線已經通了。這台機器還沒說它現在有哪些 session。",
    cloudMachineSessions: "{count} 個 session",
    cloudAccessProblem: "有一台機器的資料在這裡讀不出來（{code}）。",
    cloudSwitch: "換一台機器",
    cloudForget: "忘記",
    cloudForgetOne: "忘記 {machine}",
    cloudForgetTitle: "要忘記 {machine} 嗎？",
    cloudForgetAsk:
      "{machine} 會從這個帳號移除。這件事在這裡沒有回頭路：它只有在那台機器上重新登入才會回來，而且回來的時候是一台新的機器、新的 id。",
    cloudForgetAskCurrent: "這台機器剛剛還在回報。忘記它會立刻停掉它現在的連線。",
    cloudForgetAskStale: "這台機器已經有一陣子沒有回報了。",
    cloudForgetAskUnknown: "這個瀏覽器沒有看過這台機器回報。",
    cloudForgetHonest: "routing 會立刻停止。但已經握有主金鑰的裝置，仍然解得開它先前錄下的密文；金鑰輪替是延遲的。",
    cloudForgetting: "正在忘記 {machine}…",
    cloudForgotten: "{machine} 已經忘記了，它不在這個帳號上了。",
    cloudForgottenRow: "已忘記",
    cloudForgetSaid: "Clawdline Cloud 說：{note}",
    cloudForgetHonestUnread: "Clawdline Cloud 沒有說這對已經發出去的金鑰有什麼影響，所以這一頁也不替它說。",
    cloudForgetRefused: "Clawdline Cloud 拒絕忘記 {machine}（{code}）：這個瀏覽器已經不是那個帳號的登入狀態。什麼都沒有改變。",
    cloudForgetAbsent: "這個帳號上沒有 {machine} 這台機器（{code}）。什麼都沒有改變。",
    cloudForgetUnreadable:
      "Clawdline Cloud 對「忘記 {machine}」的回應讀不到（{code}），所以不知道它到底有沒有被忘記。請重新整理這一頁再看一次，不要直接再按一次。",
    cloudRename: "改名",
    cloudRenameOne: "把 {machine} 改名",
    cloudRenameTitle: "要把 {machine} 改名嗎？",
    cloudRenameAsk: "這會改掉 {machine} 在這個帳號上的名字，每一個登入這個帳號的瀏覽器都會看到新的。機器本身不會有任何改變。",
    cloudRenameLater: "這份清單是各台機器自己上次說的，所以在這一台重新回報之前，它還是會顯示舊的名字。",
    cloudRenameField: "新的名字",
    cloudRenaming: "正在把 {machine} 改名…",
    cloudRenamed: "{machine} 在這個帳號上現在叫做 {name}。",
    cloudRenameRefused: "Clawdline Cloud 拒絕把 {machine} 改名（{code}）：這個瀏覽器已經不是那個帳號的登入狀態。什麼都沒有改變。",
    cloudRenameAbsent: "這個帳號上沒有 {machine} 這台機器（{code}）。什麼都沒有改變。",
    cloudRenameUnreadable:
      "Clawdline Cloud 對「把 {machine} 改名」的回應讀不到（{code}），所以不知道名字到底有沒有改成功。請重新整理這一頁、看過清單之後再決定要不要再改一次。",
    cloudRenameTooLong: "在這個帳號上，一台機器的名字最多 {max} 個字元。",
    cloudBlocked: "這個 console 是給 {origin} 用的，現在卻從 {here} 打開，所以不會連線。",
    devicesLedeAccount: "這個帳號上的機器、目前狀態，以及這個瀏覽器能不能讀取它們。",
    devicesLedeThisMachine:
      "只有服務這一頁的那一台機器。daemon 服務的 console 連不到別台；帳號上有哪些機器，要問 Clawdline Cloud 的 console。",
    devicesThisMachine: "這台機器",
    cloudMisdeclared: "這個 console 的 Clawdline Cloud 宣告不能用：{reason}",
    cloudNotCarried: "這件事還不能經由 Clawdline Cloud 做，請直接在那台機器上操作。",
    cloudPastUnavailable:
      "這台機器還不能經由 Clawdline Cloud 列出以前的 session（它不回答 past-sessions），所以這裡沒辦法接續。請到那台機器上接續，或在這裡開一個新的 session。",
    sendUnknown: "不知道有沒有送到那台機器（{code}）。",
    sendLook: "去看看",
    sendLookTip: "重新讀一次那台機器上的對話：如果已經在裡面，這張卡就會消失。",
    sendLooking: "正在看…",
    sendAbsent: "那台機器上的對話裡沒有這則（{code}）。",
    sendAgain: "再送一次",
    sendAgainTip: "如果第一次其實有送到，這次不會重打。",
    sendInterrupted: "送出到一半這一頁被重新載入了，不知道有沒有送到那台機器。",
    sendKeptWords: "重新載入時這一頁留住了文字，但沒有留住圖片，所以這一則不能從這裡再送一次。",
    menuUnknown: "不知道剛才的選擇有沒有送到那台機器，等畫面更新。",
    menuChooseAgain: "重新選擇",
    menuTicked: "已打勾。要按 Submit 才會送出。",
    newBuild: "已經有新版的 Clawdline，這個頁面還是舊的那一份。",
    newBuildReload: "重新載入",
    menuMoved: "送到之前題目已經換了，所以什麼都沒打。請看清楚新的題目再選。",
    menuUnverified: "這台機器經由 Clawdline Cloud 還無法確認答案對應哪一題，請直接到那台機器上作答。",
    linksNotRepository: "這裡不是 git repository，所以沒有東西可以對應到一次 workflow 執行。這份清單上其他項目都讀到了。",
    linksNoRemote: "這個 repository 沒有 origin remote，而 workflow 執行是用 remote 命名的，所以找不到 deploy。這份清單上其他項目都讀到了。",
    linksRemoteNotGitHub: "這個 repository 的 origin 不在 GitHub 上，而這裡只有 GitHub 的 remote 對得到 workflow 執行，所以找不到 deploy。",
    linksGitUnreadable: "git 在這裡沒有回答（{reason}），所以這個專案有沒有 deploy 是不知道，不是沒有。",
    linksGitMissing: "這台機器上沒有 git",
    linksGitTimeout: "它沒有在時限內回答",
    linksGitFailed: "它拒絕了",
    linksGitTooLarge: "它回的東西超過這裡讀得下的量",
    linksDeployNoRun:
      "狀態列的工具沒有可以報的 workflow 執行紀錄（它寫下的是「{state}」）。{reason}",
    linksDeployNoPage:
      "狀態列的工具說這個 repository 最新的 workflow 執行是「{state}」，但沒有給可以打開的頁面，所以這份清單上沒有這一列。{reason}",
    linksDeployNoFile:
      "這個 repository 在 GitHub 上，而這台機器上還沒有人替它寫下 workflow 的讀數——所以沒有 deploy 這一列是因為沒有人在看，不是因為沒有執行紀錄。寫這些檔案的是狀態列的指令，不是這個 app。",
    linksDeployUnreadable:
      "這台機器上有這個 repository 的 workflow 檔案，但它讀不成一個小小的 JSON 物件，所以有沒有 deploy 是不知道，不是沒有。",
    linksDeployBecause: "它給的理由是：{reason}。",
    linksDeployNoWhy: "它沒有說為什麼。",
    linksDeployWhyNoGh: "這台機器上沒有 `gh` 指令可以去問 GitHub",
    linksDeployWhyNoBranch: "它在這裡取不到分支名稱，所以沒有東西可以查",
    linksDeployWhyGhFailed: "`gh` 沒有回答——沒登入、逾時，或是被拒絕",
    linksDeployWhyNoRuns: "這個分支上一次 workflow 都還沒有跑過",
    linksDeployWhyWorkflowDisabled:
      "那個 workflow 已經停用，所以最後一次失敗永遠不會被取代，也就不算現況",
    linksDeployWhyStaleFail:
      "上一次執行是失敗的，而那已經久到不再算是現在的狀態",
    linksDeployWhyUnknown: "這個工具說了一個我不認得的理由：「{why}」。",
    linksDeployWhen: "那個工具最後一次寫下這件事：{when}。",
    linksTruncated: "找到的位址比這份清單裝得下的多，其餘沒有列出。",
    endNamedTitle: "要關閉 {session} 嗎？",
    swipeEndLabel: "關閉 {session}",
    swipeWhyLabel: "{session}：{why}。先看清楚原因再決定要不要關。",
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
