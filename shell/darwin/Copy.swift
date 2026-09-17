// The words the shell's menus say.
//
// Copied from the Swift app's Sources/Copy+Chinese.swift (`TraditionalChinese`),
// property for property and under the same names, so `L.t.menuOpen` here and
// there is one string. Nothing in this file is written fresh: a word the
// original does not have is a word this shell does not show.
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
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        多半是被別的軟體佔走了——Spotlight、輸入法切換、BetterTouchTool 之類。

        換一個：編輯 \(configPath) 裡的 hotkey，然後從選單列選「重新載入設定」。

        在那之前，選單列的 ✳ 一樣打得開輸入框。
        """
    }
    let loginFailed = "設定開機啟動失敗"
}
