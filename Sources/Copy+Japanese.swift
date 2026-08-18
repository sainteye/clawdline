import Foundation

struct Japanese: Copy {
    let placeholder = "Claude Code にメッセージ…"

    let hintSend = "送信"
    let hintNewline = "改行"
    let hintSwitch = "切替"
    let hintList = "一覧"
    let hintMascot = "マスコット"
    let hintOutput = "出力"
    let hintFullscreen = "全画面"
    let hintKeys = "キー"
    let hintTextSize = "文字サイズ"
    let hintOrder = "逆順"
    let hintVoice = "音声入力"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "この Mac で認識中 — もう一度押すと終了"
                 : "認識中 — この言語は Apple 側で処理されます（この Mac ではありません）"
    }
    let voiceNoPermission = "音声入力にはマイクと音声認識の許可が必要です"
    let voiceUnavailable = "音声入力は現在使えません"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper が読み直しています… %.1f 秒", seconds)
    }
    let whisperMissing = "Whisper がインストールされていません — docs/whisper.md を参照"
    let whisperNothingHeard = "何も聞こえませんでした"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "音声入力：Apple のあとに Whisper（\(model)）"
        case .noBinary: return "音声入力：Apple のみ — whisper-cli がありません"
        case .noModel: return "音声入力：Apple のみ — whisper-cli はあるがモデルがありません"
        }
    }
    func voiceListeningWhisper() -> String { "認識中 — 話し終えると Whisper が読み直します" }

    let scanning = "検索中…"
    let noSession = "Claude Code のセッションが見つかりません"
    let nothingToSend = "送信先がありません — まずターミナルで Claude Code を起動してください"
    let sendFailed = "送信できませんでした"
    let itermSilent = "iTerm2 から応答がありません"
    let scriptMissing = "iterm.js が見つかりません — アプリバンドルが壊れている可能性があります"
    let cannotList = "iTerm2 のセッションを読み取れませんでした"
    let noOutput = "このセッションにはまだ読むものがありません。"
    func outputSize(_ pt: Int) -> String { "出力の文字サイズ \(pt)pt — ⌘J で表示" }
    func foldedTools(_ count: Int) -> String { "\(count) ステップ" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "新しい順" : "古い順"
    }
    func backlogNow(_ count: Int) -> String { "現在 \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "パスを追加しました — Claude Code が読み込みます" : "\(count) 件のパスを追加しました"
    }
    func stackConfirm(_ command: String) -> String { "もう一度押すと実行します：\(command)" }
    let hintStacks = "サーバー"
    func stackTip(up: Int, total: Int) -> String { "サーバー \(total) 個のうち \(up) 個が稼働中 — ⌘S で一覧" }
    let stackTipUnknown = "このプロジェクトにはサーバーがありますが、status コマンドはまだ信頼されていません — ⌘S"
    let stackUntrusted = "未承認"
    let stackActionStart = "起動"
    let stackActionRestart = "再起動"
    let stackActionStop = "停止"
    let stackActionLogs = "ログ"
    let stackLogAll = "すべて"
    let stackLogBack = "トランスクリプト"
    let stackActionAllow = "許可"
    let stackActionAgain = "もう一度"
    func sessionTip(index: Int, total: Int) -> String { "\(total) 件中 \(index) 番目のセッション — ⌘K で切り替え" }
    let sessionWaiting = "返事待ち"
    let islandDone = "完了"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) が返事を待っています"
                          : "\(labels.count) 件のセッションが返事を待っています"
    }
    func statusWorking(_ count: Int) -> String { "\(count) 件が実行中" }

    let settingsTitle = "Clawdline の設定"
    let settingsGeneral = "一般"
    let settingsBar = "バー"
    let settingsReading = "読む"
    let settingsVoice = "音声入力"
    let settingsHotkey = "ホットキー"
    let settingsRecording = "キーを押してください…"
    let settingsScope = "有効にするアプリ"
    let settingsScopeGlobal = "すべてのアプリ"
    let settingsScopeHint = "バンドル ID をカンマ区切りで。空ならどこでも。"
    let settingsLanguage = "言語"
    let settingsReopen = "ターミナルと一緒に戻る"
    let settingsFollow = "ターミナルのタブも切り替える"
    let settingsNotch = "ノッチに住む"
    let settingsNotchHint = "カメラ部分にキャラクターが出ます。オフなら何も描かず、ウィンドウも作りません。"
    let settingsPosition = "画面上の高さ"
    let settingsWidth = "幅"
    let settingsOpacity = "カードの不透明度"
    let settingsImagesPaste = "画像は画像のまま送る"
    let settingsShow = "表示"
    let settingsPaneHeight = "ペインの高さ"
    let settingsTextSize = "文字サイズ"
    let settingsPaneFont = "ペインのフォント"
    let settingsBlur = "背景のぼかし"
    let settingsNewestFirst = "新しい順"
    let settingsEngine = "認識エンジン"
    let settingsSettle = "この長さの間で文を区切る"
    let settingsStop = "この長さの無音で終了"
    let settingsAuto = "自動"
    let settingsTranscript = "トランスクリプト"
    let settingsTerminal = "ターミナル"
    let settingsOff = "オフ"
    let settingsOpenFile = "設定ファイルを開く…"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f 秒", value) }

    let menuOpen = "入力バーを開く"
    let menuReveal = "対象のタブへ移動"
    let menuMascot = "マスコット"
    let menuLogin = "ログイン時に起動"
    let menuEditConfig = "設定…"
    let menuReload = "設定を再読み込み"
    let menuQuit = "Clawdline を終了"
    let menuNoTarget = "（未検出）"

    func hotkeyFailedTitle(_ combo: String) -> String { "\(combo) を登録できませんでした" }
    func hotkeyFailedBody(_ configPath: String) -> String {
        """
        ほかのアプリが使っている可能性が高いです — Spotlight、入力ソースの切り替え、\
        BetterTouchTool など。

        別のキーにしてください：\(configPath) の "hotkey" を編集し、\
        メニューバーから「設定を再読み込み」を選びます。

        それまでは、メニューバーの ✳ から入力バーを開けます。
        """
    }
    let loginFailed = "ログイン時の起動を設定できませんでした"
}
