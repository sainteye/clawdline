// The words the shell's menus say.
//
// Copied from the Swift app's Sources/Copy+Chinese.swift (`TraditionalChinese`),
// property for property and under the same names, so `L.t.menuOpen` here and
// there is one string. Nothing in this file is written fresh: a word the
// original does not have is a word this shell does not show — with one marked
// exception under the browser bar, which says where its words come from.
//
// Only Traditional Chinese is carried, which is what the console ships too
// (web/console/public/strings/zh-Hant.json). The Swift app follows
// `language: auto` across fourteen; that is not ported.
import Foundation

enum L {
    static let t = ShellCopy()
}

struct ShellCopy {
    // Status item
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) 在等你回答"
                          : "\(labels.count) 個 session 在等你回答"
    }
    func statusWorking(_ count: Int) -> String { "\(count) 個在跑" }

    // The notch island (`NotchIsland.swift`).
    let islandDone = "跑完了"
    let islandAllSessions = "所有 session⋯"

    // Main menu
    let menuApplication = "Clawdline"
    let menuWindow = "視窗"
    let menuHelp = "輔助說明"
    let menuClose = "關閉視窗"
    let menuHome = "主頁／設定中心"
    let menuHelpDocumentation = "Clawdline 說明文件"
    func menuAbout(_ app: String) -> String { "關於 \(app)" }
    let menuServices = "服務"
    func menuHide(_ app: String) -> String { "隱藏 \(app)" }
    let menuHideOthers = "隱藏其他項目"
    let menuShowAll = "全部顯示"
    let menuMinimize = "縮到最小"

    // Status menu
    let menuOpen = "打開輸入框"
    let menuReveal = "跳到目標分頁"
    let menuMascot = "吉祥物"
    let menuLogin = "開機時啟動"
    let menuEditConfig = "設定⋯"
    let menuReload = "重新載入設定"
    let menuQuit = "結束 Clawdline"
    let menuNoTarget = "（尚未偵測）"

    func dictationStatus(_ status: DictationEngine.Status) -> String {
        switch status {
        case .ready(let model): return "語音：Apple，之後 Whisper（\(model)）"
        case .noBinary: return "語音：只有 Apple——沒有 whisper-cli"
        case .noModel: return "語音：只有 Apple——whisper-cli 有了，缺模型"
        }
    }

    func updateAvailable(latest: String, installed: String) -> String {
        "Clawdline \(latest) 出了——你手上這個是 \(installed)"
    }
    func compatNote(_ standing: Compat.Standing) -> String {
        switch standing {
        case .behind(let program, let installed, let builtAgainst):
            return "\(program) \(installed)；這個 build 是對 \(builtAgainst) 檢查的"
        case .ahead(let program, let installed, let builtAgainst, let release):
            return "\(program) \(installed)；這個 build 是對 \(builtAgainst) 檢查的——Clawdline \(release) 已經出了"
        }
    }

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) 註冊不起來" }
    // **Shortened from the Swift app's**, which named the config file's full
    // path and said to edit it by hand. The path means nothing to the person
    // reading the alert, a screen can be seen from another device, and this
    // build changes the combination in the settings window. What is kept is
    // the Swift app's own way round it: the menu bar mark.
    func hotkeyFailedBody(system: Bool) -> String {
        (system ? "這組是 macOS 自己的快速鍵。" : "")
            + "選單列的 ✳ 一樣打得開輸入框；要換一組，到「設定⋯」重錄。"
    }
    let loginFailed = "設定開機啟動失敗"

    // The settings window's words (`Settings.swift`).
    //
    // `settingsTitle` is the native window's own title bar, which is drawn
    // before its page loads, so it has to be here. The rest of that window's
    // words are not: they are in the page (web/console/src/pages/settings/window/copy.ts),
    // transcribed from the same `Copy+Chinese.swift`, so the Linux and Windows
    // shells get them without a second transcription. See docs/shell-bridge.md.
    //
    // The four below are still handed to the *console's* settings page, whose
    // shell-only block predates the settings window and is unchanged.
    let settingsTitle = "Clawdline 設定"
    let settingsHotkey = "快速鍵"
    let settingsRecording = "按下按鍵……"
    let settingsScope = "在哪裡生效"
    let settingsScopeGlobal = "所有 app"
    let settingsOff = "關閉"

    // The browser bar (shell/darwin/Browser.swift). Cloud is deliberately the
    // short product name requested for the Cloud tab; its longer sentence is
    // the tooltip and VoiceOver label.
    let homeLocalTitle = "本機瀏覽器"
    let browserCloud = "Cloud"
    let webInfoRefresh = "重新整理"
    let settingsRemoteOpen = "用瀏覽器打開"

    // **Not from the Swift app.** It has no browser, no page zoom and no View
    // menu, so it has no words for them, and the bar's icons, the menu and a
    // zoom at its limit all have to say something. These are macOS's own words
    // for the same controls — Safari's toolbar and View menu in Traditional
    // Chinese — so they read the way every other Mac app reads, and they are
    // here rather than in Browser.swift so that nothing on screen is spelled
    // twice. `上限`／`下限` are the Swift app's own words for a limit.
    let browserBack = "上一頁"
    let browserForward = "下一頁"
    let menuView = "顯示方式"
    let menuActualSize = "實際大小"
    let menuZoomIn = "放大"
    let menuZoomOut = "縮小"
    func zoomLevel(_ percent: Int) -> String { "\(percent)%" }
    func zoomCeiling(_ percent: Int) -> String { "上限 \(percent)%" }
    func zoomFloor(_ percent: Int) -> String { "下限 \(percent)%" }
    // The Cloud tab (2026-09-27): Cloud opens in this window rather than in
    // the person's browser. "好"／"取消" are the buttons macOS itself puts on
    // a web page's confirm() in Safari.
    let browserShowCloud = "在這個視窗打開 Clawdline Cloud"
    let dialogOK = "好"
    let dialogCancel = "取消"

    // The pairing alert (`main.swift` showPairing in the Swift app).
    let pairingIgnore = "忽略"
    func pairingAsks(_ device: String) -> String { "\(device) 想跟這台 Mac 配對" }
    func pairingCode(_ code: String) -> String {
        """
        在它上面輸入這組代碼：

        \(code)

        兩分鐘內有效。如果剛才不是你要求的，不用理會——對方沒有這組代碼，就完成不了。
        """
    }
}
