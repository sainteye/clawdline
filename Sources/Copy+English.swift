import Foundation

/// The reference translation. Copy this file to add a language.
struct English: Copy {
    let placeholder = "Message Claude Code…"

    let hintSend = "send"
    let hintNewline = "new line"
    let hintSwitch = "switch"
    let hintList = "list"
    let hintMascot = "mascot"
    let hintOutput = "output"
    let hintFullscreen = "full screen"
    let hintKeys = "keys"
    let hintTextSize = "text size"
    let hintOrder = "reverse"
    let hintVoice = "dictate"
    func voiceListening(onDevice: Bool) -> String {
        onDevice ? "Listening on this Mac — press again to stop"
                 : "Listening — this language is transcribed by Apple, not on this Mac"
    }
    let voiceNoPermission = "Dictation needs microphone and speech access"
    let voiceUnavailable = "Dictation is not available right now"
    func voiceTranscribing(seconds: Double) -> String {
        String(format: "Whisper is reading it back… %.1fs", seconds)
    }
    let whisperMissing = "Whisper is not installed — see docs/whisper.md"
    let whisperNothingHeard = "Heard nothing"
    func dictationStatus(_ status: Whisper.Status) -> String {
        switch status {
        case .ready(let model): return "Dictation: Apple, then Whisper (\(model))"
        case .noBinary: return "Dictation: Apple only — no whisper-cli"
        case .noModel: return "Dictation: Apple only — whisper-cli is there, no model"
        }
    }
    func voiceListeningWhisper() -> String { "Listening — Whisper takes another look when you stop" }

    let scanning = "Scanning…"
    let noSession = "No Claude Code session found"
    let nothingToSend = "Nothing to send to — start Claude Code in a terminal first"
    let sendFailed = "Could not send"
    let itermSilent = "iTerm2 did not respond"
    let scriptMissing = "iterm.js is missing — broken app bundle?"
    let cannotList = "Could not read iTerm2 sessions"
    let noOutput = "Nothing to read from this session yet."
    func outputSize(_ pt: Int) -> String { "Output text \(pt)pt — ⌘J to see it" }
    func foldedTools(_ count: Int) -> String { "\(count) steps" }
    func outputOrder(newestFirst: Bool) -> String {
        newestFirst ? "Newest first" : "Oldest first"
    }
    func backlogNow(_ count: Int) -> String { "now \(count)" }
    func dropped(_ count: Int) -> String {
        count == 1 ? "Added the path — Claude Code reads it" : "Added \(count) paths"
    }

    let menuOpen = "Open prompt bar"
    let menuReveal = "Jump to target tab"
    let menuMascot = "Mascot"
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
