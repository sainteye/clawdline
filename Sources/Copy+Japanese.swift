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

    let menuOpen = "入力バーを開く"
    let menuReveal = "対象のタブへ移動"
    let menuMascot = "マスコット"
    let menuLogin = "ログイン時に起動"
    let menuEditConfig = "設定を編集…"
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
