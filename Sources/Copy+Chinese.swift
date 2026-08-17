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

    let menuOpen = "打開輸入框"
    let menuReveal = "跳到目標分頁"
    let menuMascot = "吉祥物"
    let menuLogin = "開機時啟動"
    let menuEditConfig = "編輯設定⋯"
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

    let menuOpen = "打开输入框"
    let menuReveal = "跳到目标标签页"
    let menuMascot = "吉祥物"
    let menuLogin = "开机时启动"
    let menuEditConfig = "编辑配置……"
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
