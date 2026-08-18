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
    func stackConfirm(_ command: String) -> String { "Press again to run:  \(command)" }
    let hintStacks = "servers"
    func stackTip(up: Int, total: Int) -> String { "\(up) of \(total) servers up — ⌘S for the list" }
    let stackTipUnknown = "This project has servers; its status command is not trusted yet — ⌘S"
    let stackUntrusted = "not trusted yet"
    let stackActionStart = "start"
    let stackActionRestart = "restart"
    let stackActionStop = "stop"
    let stackActionLogs = "log"
    let stackLogAll = "all"
    let stackLogBack = "transcript"
    let stackActionAllow = "allow"
    let stackActionAgain = "press again"
    func sessionTip(index: Int, total: Int) -> String { "Session \(index) of \(total) — ⌘K to switch" }
    let sessionWaiting = "waiting for you"
    let islandDone = "done"
    let islandAllSessions = "All sessions…"
    func statusWaiting(_ labels: [String]) -> String {
        labels.count == 1 ? "\(labels[0]) is waiting for you"
                          : "\(labels.count) sessions are waiting for you"
    }
    func statusWorking(_ count: Int) -> String { "\(count) running" }

    let settingsTitle = "Clawdline Settings"
    let settingsGeneral = "General"
    let settingsBar = "The bar"
    let settingsReading = "Reading"
    let settingsVoice = "Dictation"
    let settingsHotkey = "Hotkey"
    let settingsRecording = "Press keys…"
    let settingsScope = "Fires in"
    let settingsScopeGlobal = "In every app"
    let settingsScopeHint = "Empty means everywhere, so taking the last one out turns the switch above on — and turning that back off puts the list back. The config file keeps these as bundle ids, and a hand-edited list still works."
    let settingsScopeAdd = "Add an app…"
    let settingsScopeChoose = "Choose an app…"
    let settingsScopeRunning = "Open right now"
    let settingsScopeRemove = "Remove"
    let settingsLanguage = "Language"
    let settingsReopen = "The bar comes and goes with the terminal"
    let settingsReopenHint = "Switching away from the terminal puts the bar away, and switching back to it brings the bar back. Esc is what means finished with it."
    let settingsFollow = "The terminal shows whatever the bar is aimed at"
    let settingsFollowHint = "It selects that session's tab. It does not bring the terminal to the front — otherwise every press of Tab would take the keyboard out of the box you are typing into."
    let settingsNotch = "Live in the notch"
    let settingsNotchHint = "A character in the camera housing. Off is off — nothing is drawn and no window is made."
    let settingsPosition = "Height on screen"
    let settingsWidth = "Width"
    let settingsOpacity = "Card opacity"
    let settingsShow = "Show"
    let settingsPaneHeight = "Pane height"
    let settingsTextSize = "Text size"
    let settingsPaneFont = "Pane font"
    let settingsBlur = "Blur behind"
    let settingsNewestFirst = "Newest first"
    let settingsEngine = "Recogniser"
    let settingsSettle = "A pause ends a sentence"
    let settingsStop = "A silence ends the session"
    let settingsAuto = "Auto"
    let settingsTranscript = "Transcript"
    let settingsTerminal = "Terminal"
    let settingsOff = "Off"
    let settingsHooks = "Claude Code hooks"
    let settingsHooksHint = "With these installed, Claude Code says the moment a turn starts, ends or needs an answer, instead of Clawdline finding out at its next look. The screen is still where every reading comes from; this only decides how soon one is taken."
    let settingsHooksInstall = "Install"
    let settingsHooksRemove = "Remove"
    let settingsHooksOff = "Not installed — readings come from the screen alone"
    let settingsHooksOn = "Installed — no session has reported yet"
    let settingsHooksLive = "Installed, and sessions are reporting"
    let settingsStateHook = "When a session changes state"
    let settingsStateHookHint = "Whenever a session starts working, finishes, or asks you something, Clawdline runs this: your own program, with the details in its environment. It is set in the config file rather than here because it is an argument list and not a command line — a path with a space in it has to stay one path."
    let settingsRemote = "Remote"
    let settingsRemoteServe = "Let a browser or your phone see your sessions"
    let settingsRemoteHint = "Off, and nothing outside this Mac can reach anything. On, and the session list is served on 127.0.0.1 — for a browser here, for a phone through a tunnel, for a script. What that hands over is repository names, branches and task titles, which is why it stays off until you say otherwise."
    let settingsRemoteDevices = "Paired devices"
    let settingsRemoteNoDevices = "None yet — nothing outside this Mac can read anything"
    let settingsRemoteRevokeAll = "Disconnect everything"
    let settingsRemoteOpen = "Open in a browser"
    let pairingIgnore = "Ignore"
    func pairingAsks(_ device: String) -> String { "\(device) wants to pair with this Mac" }
    func pairingCode(_ code: String) -> String {
        """
        Type this code into it:

        \(code)

        It is good for two minutes. If you did not just ask for this, ignore it — whoever asked \
        cannot finish without this code.
        """
    }
    let settingsTunnel = "Reach this Mac from anywhere"
    let settingsTunnelQuick = "A generated address"
    let settingsTunnelNamed = "My own domain"
    let settingsTunnelHostname = "Hostname"
    let settingsTunnelHint = "Opens an outbound connection through cloudflared — no port forwarding, nothing listening on your network. It will not start until a device is paired, because what is behind it is every repository name and task title on this Mac."
    let settingsRemoteWrite = "Let a paired device write into a session"
    let settingsRemoteWriteHint = "Off, and a paired device can only read. On, and it can send text into a session and start new ones — which runs code on this Mac, because that is what Claude Code does. A different decision from the one above, so it is a different switch."
    let settingsRemotePhone = "Pair a phone…"
    let settingsRemotePhoneHint = "Shows a code to scan. It carries a key of its own, so a photograph of it is a device you can see in this list and take away again — not this Mac's own key."
    let pairingScanTitle = "Scan this with the phone"
    let pushWaiting = "is waiting for an answer"
    func settingsSeconds(_ value: Double) -> String { String(format: "%.1f s", value) }

    let menuOpen = "Open prompt bar"
    let menuReveal = "Jump to target tab"
    let menuMascot = "Mascot"
    let menuLogin = "Launch at login"
    let menuEditConfig = "Settings…"
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
