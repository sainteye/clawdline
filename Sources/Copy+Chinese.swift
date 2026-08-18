import Foundation

/// Two scripts, two structs. Not one struct plus a transliterator: the wording differs as well
/// as the characters (a tab is 分頁 in Taipei and 标签页 in Beijing), and a converter would get
/// the characters right and the words wrong — which reads worse than a plain translation, because
/// it is fluent enough that nobody flags it.
struct TraditionalChinese: Copy {
    let placeholder = "跟 Claude 說⋯⋯"

    let hintSend = "送出"
    let hintNewline = "換行"
    let hintSwitch = "換分頁"
    let hintList = "清單"
    let hintMascot = "換角色"
    let hintOutput = "看輸出"
    let hintFullscreen = "全螢幕"
    let hintKeys = "快速鍵"
    let hintTextSize = "字級"
    let hintOrder = "反序"
    let hintVoice = "語音"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "在這台 Mac 上聽——再按一次結束"
                 : "聽著——這個語言由 Apple 辨識，不在這台機器上"
    }
    let voiceNoPermission = "語音輸入需要麥克風與語音辨識權限"
    let voiceUnavailable = "現在無法使用語音輸入"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper 重讀中⋯ %.1f 秒", seconds)
    }
    let whisperMissing = "沒有裝 Whisper——見 docs/whisper.md"
    let whisperNothingHeard = "沒聽到東西"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "語音：Apple，之後 Whisper（\(model)）"
        case .noBinary: return "語音：只有 Apple——沒有 whisper-cli"
        case .noModel: return "語音：只有 Apple——whisper-cli 有了，缺模型"
        }
    }
    func voiceListeningWhisper() -> String { "聽著——停下來時 Whisper 會再看一遍" }

    let scanning = "掃描中⋯"
    let noSession = "找不到在跑 Claude Code 的分頁"
    let nothingToSend = "沒有可以送的分頁——先在終端機裡開一個 Claude Code"
    let sendFailed = "送不出去"
    let itermSilent = "iTerm2 沒有回應"
    let scriptMissing = "找不到 iterm.js——app bundle 壞了？"
    let cannotList = "讀不到 iTerm2 的 session"
    let noOutput = "還讀不到這個分頁的內容。"
    func outputSize(_ pt: Int) -> String { "輸出字級 \(pt)pt——按 ⌘J 看" }
    func foldedTools(_ count: Int) -> String { "\(count) 個動作" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "最新的在最上面" : "最舊的在最上面"
    }
    func backlogNow(_ count: Int) -> String { "現在\(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "路徑加進去了——Claude Code 會自己讀" : "加了 \(count) 個路徑"
    }
    func stackConfirm(_ command: String) -> String { "再按一次就執行：\(command)" }
    let hintStacks = "伺服器"
    func stackTip(up: Int, total: Int) -> String { "\(total) 個伺服器，\(up) 個活著——⌘S 打開清單" }
    let stackTipUnknown = "這個專案有伺服器，但它的狀態指令還沒被信任——⌘S"
    let stackUntrusted = "尚未信任"
    let stackActionStart = "啟動"
    let stackActionRestart = "重啟"
    let stackActionStop = "停止"
    let stackActionLogs = "紀錄"
    let stackLogAll = "全部"
    let stackLogBack = "逐字稿"
    let stackActionAllow = "允許"
    let stackActionAgain = "再按一次"
    func sessionTip(index: Int, total: Int) -> String { "第 \(index) 個 session，共 \(total) 個——⌘K 切換" }
    let sessionWaiting = "在等你回答"
    let islandDone = "跑完了"
    let islandAllSessions = "所有 session⋯"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) 在等你回答"
                          : "\(labels.count) 個 session 在等你回答"
    }
    func statusWorking(_ count: Int) -> String { "\(count) 個在跑" }

    let settingsTitle = "Clawdline 設定"
    let settingsGeneral = "一般"
    let settingsBar = "輸入條"
    let settingsReading = "閱讀"
    let settingsVoice = "語音輸入"
    let settingsHotkey = "快速鍵"
    let settingsRecording = "按下按鍵……"
    let settingsScope = "在哪裡生效"
    let settingsScopeGlobal = "所有 app"
    let settingsScopeHint = "bundle id，用逗號分隔。留空就是到處都能按。"
    let settingsLanguage = "語言"
    let settingsReopen = "跟著終端機一起回來"
    let settingsFollow = "終端機的分頁也跟著切"
    let settingsNotch = "住在瀏海裡"
    let settingsNotchHint = "在鏡頭那塊住一隻角色。關掉就是真的關掉——什麼都不畫，視窗也不會建立。"
    let settingsPosition = "在螢幕上的高度"
    let settingsWidth = "寬度"
    let settingsOpacity = "卡片不透明度"
    let settingsImagesPaste = "圖片就用圖片送"
    let settingsShow = "顯示"
    let settingsPaneHeight = "面板高度"
    let settingsTextSize = "文字大小"
    let settingsPaneFont = "面板字型"
    let settingsBlur = "背後模糊"
    let settingsNewestFirst = "最新的在最上面"
    let settingsEngine = "辨識引擎"
    let settingsSettle = "停頓多久算一句話結束"
    let settingsStop = "安靜多久算整段結束"
    let settingsAuto = "自動"
    let settingsTranscript = "對話記錄"
    let settingsTerminal = "終端機畫面"
    let settingsOff = "關閉"
    let settingsHooks = "Claude Code Hook"
    let settingsHooksHint = "裝上之後，一輪對話開始、結束、或是需要你回答的當下，Claude Code 會直接說一聲，不必等 Clawdline 下一次去看。每一次判讀仍然來自畫面，這裡只決定判讀發生得多快。"
    let settingsHooksInstall = "安裝"
    let settingsHooksRemove = "移除"
    let settingsHooksOff = "未安裝——狀態全部從畫面讀"
    let settingsHooksOn = "已安裝——還沒有 session 回報過"
    let settingsHooksLive = "已安裝，session 正在回報"
    let settingsRemote = "遠端"
    let settingsRemoteServe = "用 HTTP 回應"
    let settingsRemoteHint = "在 127.0.0.1 上提供 session 清單，讓瀏覽器、隔著通道連進來的手機，或是一支腳本都讀得到。你不打開就一直關著：一個在聽的 socket，等於把儲存庫名稱、分支和工作標題交出去。"
    let settingsRemoteDevices = "已配對的裝置"
    let settingsRemoteNoDevices = "還沒有——這台 Mac 以外的東西什麼都讀不到"
    let settingsRemoteRevokeAll = "全部斷開"
    let settingsRemoteOpen = "用瀏覽器打開"
    let pairingIgnore = "忽略"
    func pairingAsks(_ device: String) -> String { "\(device) 想跟這台 Mac 配對" }
    func pairingCode(_ code: String) -> String {
        """
        在它上面輸入這組代碼：

        \(code)

        兩分鐘內有效。如果剛才不是你要求的，不用理會——對方沒有這組代碼，就完成不了。
        """
    }
    let settingsTunnel = "從外面連得到"
    let settingsTunnelQuick = "自動產生的網址"
    let settingsTunnelNamed = "我自己的網域"
    let settingsTunnelHostname = "主機名稱"
    let settingsTunnelHint = "透過 cloudflared 從這台 Mac 往外連出去——不必開通訊埠轉發，你的網路上也沒有東西在聽。要先配對過一台裝置它才會啟動，因為通道後面就是這台 Mac 上每一個儲存庫名稱、每一個工作標題。"
    let settingsRemoteWrite = "讓配對的裝置打字"
    let settingsRemoteWriteHint = "關著的時候，配對過的裝置只能讀。打開之後，它可以把文字送進 session，也可以開新的 session——那就是在這台 Mac 上執行程式碼，因為 Claude Code 做的就是這件事。這跟上面那一題是兩回事，所以它是另一個開關。"
    let settingsRemotePhone = "配對手機……"
    let settingsRemotePhoneHint = "顯示一張可以掃的 QR code。它自己帶著一把鑰匙，所以就算被拍走，被拍走的也只是一台會出現在上面清單裡、隨時拿得掉的裝置——不是這台 Mac 自己的鑰匙。"
    let pairingScanTitle = "用手機掃這張 QR code"
    let pushWaiting = "在等你回答"
    let settingsOpenFile = "打開設定檔……"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f 秒", value) }

    let menuOpen = "打開輸入框"
    let menuReveal = "跳到目標分頁"
    let menuMascot = "吉祥物"
    let menuLogin = "開機時啟動"
    let menuEditConfig = "設定⋯"
    let menuReload = "重新載入設定"
    let menuQuit = "結束 Clawdline"
    let menuNoTarget = "（尚未偵測）"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) 註冊不起來" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        多半是被別的軟體佔走了——Spotlight、輸入法切換、BetterTouchTool 之類。

        換一個：編輯 \(configPath) 裡的 hotkey，然後從選單列選「重新載入設定」。

        在那之前，選單列的 ✳ 一樣打得開輸入框。
        """
    }
    let loginFailed = "設定開機啟動失敗"
}

struct SimplifiedChinese: Copy {
    let placeholder = "跟 Claude 说……"

    let hintSend = "发送"
    let hintNewline = "换行"
    let hintSwitch = "换标签"
    let hintList = "列表"
    let hintMascot = "换角色"
    let hintOutput = "看输出"
    let hintFullscreen = "全屏"
    let hintKeys = "快捷键"
    let hintTextSize = "字号"
    let hintOrder = "倒序"
    let hintVoice = "语音"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "在这台 Mac 上听——再按一次结束"
                 : "听着——这个语言由 Apple 识别，不在这台机器上"
    }
    let voiceNoPermission = "语音输入需要麦克风与语音识别权限"
    let voiceUnavailable = "现在无法使用语音输入"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper 重读中…… %.1f 秒", seconds)
    }
    let whisperMissing = "没有装 Whisper——见 docs/whisper.md"
    let whisperNothingHeard = "没听到东西"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "语音：Apple，之后 Whisper（\(model)）"
        case .noBinary: return "语音：只有 Apple——没有 whisper-cli"
        case .noModel: return "语音：只有 Apple——whisper-cli 有了，缺模型"
        }
    }
    func voiceListeningWhisper() -> String { "听着——停下来时 Whisper 会再看一遍" }

    let scanning = "扫描中……"
    let noSession = "找不到在跑 Claude Code 的标签页"
    let nothingToSend = "没有可以发送的标签页——先在终端里开一个 Claude Code"
    let sendFailed = "发送不出去"
    let itermSilent = "iTerm2 没有响应"
    let scriptMissing = "找不到 iterm.js——app bundle 坏了？"
    let cannotList = "读不到 iTerm2 的 session"
    let noOutput = "还读不到这个标签页的内容。"
    func outputSize(_ pt: Int) -> String { "输出字号 \(pt)pt——按 ⌘J 看" }
    func foldedTools(_ count: Int) -> String { "\(count) 个动作" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "最新的在最上面" : "最旧的在最上面"
    }
    func backlogNow(_ count: Int) -> String { "现在\(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "路径加进去了——Claude Code 会自己读" : "加了 \(count) 个路径"
    }
    func stackConfirm(_ command: String) -> String { "再按一次就执行：\(command)" }
    let hintStacks = "服务器"
    func stackTip(up: Int, total: Int) -> String { "\(total) 个服务器，\(up) 个活着——⌘S 打开列表" }
    let stackTipUnknown = "这个项目有服务器，但它的状态命令还没被信任——⌘S"
    let stackUntrusted = "尚未信任"
    let stackActionStart = "启动"
    let stackActionRestart = "重启"
    let stackActionStop = "停止"
    let stackActionLogs = "记录"
    let stackLogAll = "全部"
    let stackLogBack = "逐字稿"
    let stackActionAllow = "允许"
    let stackActionAgain = "再按一次"
    func sessionTip(index: Int, total: Int) -> String { "第 \(index) 个 session，共 \(total) 个——⌘K 切换" }
    let sessionWaiting = "在等你回答"
    let islandDone = "跑完了"
    let islandAllSessions = "所有 session……"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) 在等你回答"
                          : "\(labels.count) 个 session 在等你回答"
    }
    func statusWorking(_ count: Int) -> String { "\(count) 个在跑" }

    let settingsTitle = "Clawdline 设置"
    let settingsGeneral = "一般"
    let settingsBar = "输入条"
    let settingsReading = "阅读"
    let settingsVoice = "语音输入"
    let settingsHotkey = "快捷键"
    let settingsRecording = "按下按键……"
    let settingsScope = "在哪里生效"
    let settingsScopeGlobal = "所有 app"
    let settingsScopeHint = "bundle id，用逗号分隔。留空就是到处都能按。"
    let settingsLanguage = "语言"
    let settingsReopen = "跟着终端一起回来"
    let settingsFollow = "终端的标签页也跟着切"
    let settingsNotch = "住在刘海里"
    let settingsNotchHint = "在镜头那块住一只角色。关掉就是真的关掉——什么都不画，窗口也不会建立。"
    let settingsPosition = "在屏幕上的高度"
    let settingsWidth = "宽度"
    let settingsOpacity = "卡片不透明度"
    let settingsImagesPaste = "图片就用图片发"
    let settingsShow = "显示"
    let settingsPaneHeight = "面板高度"
    let settingsTextSize = "文字大小"
    let settingsPaneFont = "面板字体"
    let settingsBlur = "背后模糊"
    let settingsNewestFirst = "最新的在最上面"
    let settingsEngine = "识别引擎"
    let settingsSettle = "停顿多久算一句话结束"
    let settingsStop = "安静多久算整段结束"
    let settingsAuto = "自动"
    let settingsTranscript = "对话记录"
    let settingsTerminal = "终端画面"
    let settingsOff = "关闭"
    let settingsHooks = "Claude Code Hook"
    let settingsHooksHint = "装上之后，一轮对话开始、结束、或是需要你回答的当下，Claude Code 会直接说一声，不必等 Clawdline 下一次去看。每一次判读仍然来自画面，这里只决定判读发生得多快。"
    let settingsHooksInstall = "安装"
    let settingsHooksRemove = "移除"
    let settingsHooksOff = "未安装——状态全部从画面读"
    let settingsHooksOn = "已安装——还没有 session 回报过"
    let settingsHooksLive = "已安装，session 正在回报"
    let settingsRemote = "远程"
    let settingsRemoteServe = "用 HTTP 回应"
    let settingsRemoteHint = "在 127.0.0.1 上提供 session 列表，让浏览器、隔着隧道连进来的手机，或者一个脚本都读得到。你不打开就一直关着：一个在监听的 socket，等于把仓库名称、分支和任务标题交出去。"
    let settingsRemoteDevices = "已配对的设备"
    let settingsRemoteNoDevices = "还没有——这台 Mac 以外的东西什么都读不到"
    let settingsRemoteRevokeAll = "全部断开"
    let settingsRemoteOpen = "用浏览器打开"
    let pairingIgnore = "忽略"
    func pairingAsks(_ device: String) -> String { "\(device) 想跟这台 Mac 配对" }
    func pairingCode(_ code: String) -> String {
        """
        在它上面输入这组代码：

        \(code)

        两分钟内有效。如果刚才不是你要求的，不用理会——对方没有这组代码，就完成不了。
        """
    }
    let settingsTunnel = "从外面连得到"
    let settingsTunnelQuick = "自动生成的网址"
    let settingsTunnelNamed = "我自己的域名"
    let settingsTunnelHostname = "主机名"
    let settingsTunnelHint = "通过 cloudflared 从这台 Mac 往外连出去——不用做端口转发，你的网络上也没有东西在监听。要先配对过一台设备它才会启动，因为隧道后面就是这台 Mac 上每一个仓库名称、每一个任务标题。"
    let settingsRemoteWrite = "让配对的设备打字"
    let settingsRemoteWriteHint = "关着的时候，配对过的设备只能读。打开之后，它可以把文字送进 session，也可以开新的 session——那就是在这台 Mac 上执行代码，因为 Claude Code 做的就是这件事。这跟上面那一题是两回事，所以它是另一个开关。"
    let settingsRemotePhone = "配对手机……"
    let settingsRemotePhoneHint = "显示一张可以扫的 QR code。它自己带着一把钥匙，所以就算被拍走，被拍走的也只是一台会出现在上面列表里、随时拿得掉的设备——不是这台 Mac 自己的钥匙。"
    let pairingScanTitle = "用手机扫这张 QR code"
    let pushWaiting = "在等你回答"
    let settingsOpenFile = "打开配置文件……"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f 秒", value) }

    let menuOpen = "打开输入框"
    let menuReveal = "跳到目标标签页"
    let menuMascot = "吉祥物"
    let menuLogin = "开机时启动"
    let menuEditConfig = "设置……"
    let menuReload = "重新加载配置"
    let menuQuit = "退出 Clawdline"
    let menuNoTarget = "（尚未检测到）"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) 注册不上" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        多半是被别的软件占走了——Spotlight、输入法切换、BetterTouchTool 之类。

        换一个：编辑 \(configPath) 里的 hotkey，然后从菜单栏选「重新加载配置」。

        在那之前，菜单栏的 ✳ 一样打得开输入框。
        """
    }
    let loginFailed = "设置开机启动失败"
}
