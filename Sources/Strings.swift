import Foundation

/// Every piece of UI copy, one implementation per language.
///
/// Why a protocol instead of a `.strings` catalog: the compiler refuses to build a
/// language that is missing a string. With `.strings` files a missing key is a blank
/// label at runtime, and nobody notices until a user in that language complains.
///
/// **Adding a language:** write one struct below, add one line to `catalog`. That is all.
protocol Copy {
    // Input field
    var placeholder: String { get }

    // Hint row
    var hintSend: String { get }
    var hintNewline: String { get }
    var hintSwitch: String { get }
    var hintList: String { get }

    // Target state
    var scanning: String { get }
    var noSession: String { get }
    var nothingToSend: String { get }
    var sendFailed: String { get }
    var itermSilent: String { get }
    var scriptMissing: String { get }
    var cannotList: String { get }

    // Menu bar
    var menuOpen: String { get }
    var menuReveal: String { get }
    var menuLogin: String { get }
    var menuEditConfig: String { get }
    var menuReload: String { get }
    var menuQuit: String { get }
    var menuNoTarget: String { get }

    // Alerts
    func hotkeyFailedTitle(_ combo: String) -> String
    func hotkeyFailedBody(_ configPath: String) -> String
    var loginFailed: String { get }
}

struct English: Copy {
    let placeholder = "Message Claude Code…"

    let hintSend = "send"
    let hintNewline = "new line"
    let hintSwitch = "switch"
    let hintList = "list"

    let scanning = "Scanning…"
    let noSession = "No Claude Code session found"
    let nothingToSend = "Nothing to send to — start Claude Code in iTerm2 first"
    let sendFailed = "Could not send"
    let itermSilent = "iTerm2 did not respond"
    let scriptMissing = "iterm.js is missing — broken app bundle?"
    let cannotList = "Could not read iTerm2 sessions"

    let menuOpen = "Open prompt bar"
    let menuReveal = "Jump to target tab"
    let menuLogin = "Launch at login"
    let menuEditConfig = "Edit config…"
    let menuReload = "Reload config"
    let menuQuit = "Quit Clawdline"
    let menuNoTarget = "(not detected yet)"

    func hotkeyFailedTitle(_ combo: String) -> String { "Could not register \(combo)" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        Another app has probably taken it — Spotlight, an input-method switcher, \
        BetterTouchTool, and so on.

        Pick a different one: edit "hotkey" in \(configPath), then choose \
        "Reload config" from the menu bar.

        Until then, the ✳ in the menu bar still opens the prompt bar.
        """
    }
    let loginFailed = "Could not set launch at login"
}

struct TraditionalChinese: Copy {
    let placeholder = "跟 Claude 說⋯⋯"

    let hintSend = "送出"
    let hintNewline = "換行"
    let hintSwitch = "換分頁"
    let hintList = "清單"

    let scanning = "掃描中⋯"
    let noSession = "找不到在跑 Claude Code 的分頁"
    let nothingToSend = "沒有可以送的分頁——先在 iTerm2 裡開一個 Claude Code"
    let sendFailed = "送不出去"
    let itermSilent = "iTerm2 沒有回應"
    let scriptMissing = "找不到 iterm.js——app bundle 壞了？"
    let cannotList = "讀不到 iTerm2 的 session"

    let menuOpen = "打開輸入框"
    let menuReveal = "跳到目標分頁"
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

enum L {
    /// The active language. Cached because it is read on every redraw.
    private(set) static var t: Copy = pick()

    /// Call this after the config changes.
    static func reload() { t = pick() }

    /// More specific tags first — `zh-Hans` must not fall into the Traditional bucket.
    private static let catalog: [(tag: String, copy: Copy)] = [
        ("en", English()),
        ("zh-Hant", TraditionalChinese()),
        ("zh-TW", TraditionalChinese()),
        ("zh-HK", TraditionalChinese()),
        ("zh-MO", TraditionalChinese()),
    ]

    private static func pick() -> Copy {
        let want = Config.shared.language
        let tags = want == "auto" ? Locale.preferredLanguages : [want]
        for tag in tags {
            if let hit = catalog.first(where: { tag.hasPrefix($0.tag) }) { return hit.copy }
        }
        return English()
    }
}
