import AppKit

/// Settings and history, kept in ~/.config/clawdline/.
/// Everything has a default: a missing, corrupt or half-written config must still launch.
final class Config {
    static let shared = Config()

    var yFraction: CGFloat = 0.30      // where the panel top sits, as a fraction of screen height
    var width: CGFloat = 720
    var hotKey = "option+space"
    /// The hotkey only fires while one of these apps is frontmost. Comma-separated bundle
    /// ids; empty string means global. More than one matters now that tmux lets Claude Code
    /// run under any terminal — an iTerm2-only scope would leave those users without a key.
    /// Done in-app rather than handed to a hotkey utility: registering globally takes ⌥Space away
    /// from every other app, while registering per-frontmost leaves them exactly as they were.
    var scopeApp = "com.googlecode.iterm2"
    /// "auto" follows the system, or a tag such as "en" / "zh-Hant"
    var language = "auto"
    /// Which mascot pack to draw. Files live in ~/.config/clawdline/mascots/<name>.json
    var mascot = "clawd"
    /// Where tmux lives. Apps do not inherit a login shell, so PATH almost never has it.
    /// Empty means "look in the usual places".
    /// How tall the output pane is, in points.
    var outputHeight: CGFloat = 340
    /// The font the ⌘J pane draws with. Match it to your terminal's, or the box-drawing
    /// characters a status line is made of come out at the wrong widths.
    /// What ⌘J shows: "auto" prefers the transcript and falls back to the terminal,
    /// "transcript" or "terminal" pin it.
    var outputMode = "auto"
    var outputFont = "Menlo"
    /// Point size of the ⌘J pane, adjustable live with ⌘+ / ⌘-.
    var outputSize: CGFloat = 11.5
    /// Newest message at the top instead of the bottom, toggled live with ⌘R.
    /// Only the transcript reads this way: a terminal capture is a picture of a grid, and
    /// flipping its lines would have a wrapped sentence reading upwards.
    var outputNewestFirst = false
    /// Which recogniser the microphone uses.
    ///
    /// "apple" is live and needs nothing installed. "whisper" transcribes when you stop and
    /// handles a sentence with two languages in it, at the cost of a binary and a model file —
    /// see docs/whisper.md. "auto" uses whisper when both are present.
    var voiceEngine = "auto"
    var whisperBinary = ""
    var whisperModel = ""
    /// Come back when the terminal does.
    ///
    /// Switching away from a panel you left open is "I need to see something for a moment", not
    /// "I am done with it" — that is what Esc is for. So what an app switch put away, coming
    /// back takes out again. Turn it off if you would rather every appearance be one you asked
    /// for by hand.
    var reopenOnReturn = true
    /// How solid the card is, from 0 (pure frosted glass) to 1 (opaque).
    ///
    /// The material samples whatever is behind the window, so a screen of green diff or a
    /// bright page tints the whole card and drags the text with it. This is a dark layer
    /// between the two: the blur still reads as glass, but the colour behind stops arriving.
    var cardOpacity: Double = 0.55
    /// How far the ⌘J backdrop goes, from 0 (none) to 1 (fully obscured).
    /// Below 1 the blur is partly transparent, so what is behind stays legible.
    var backdropStrength: Double = 0.5
    var tmuxPath = ""
    var lastTargetID: String?
    var history: [String] = []

    private let dir = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent(".config/clawdline", isDirectory: true)
    private var file: URL { dir.appendingPathComponent("config.json") }

    private init() { load() }

    func load() {
        guard let data = try? Data(contentsOf: file),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        if let v = obj["y_fraction"] as? Double, v > 0.02, v < 0.9 { yFraction = CGFloat(v) }
        if let v = obj["width"] as? Double, v >= 360, v <= 1400 { width = CGFloat(v) }
        if let v = obj["hotkey"] as? String, !v.isEmpty { hotKey = v }
        if let v = obj["scope_app"] as? String { scopeApp = v }
        if let v = obj["language"] as? String, !v.isEmpty { language = v }
        if let v = obj["mascot"] as? String, !v.isEmpty { mascot = v }
        if let v = obj["tmux_path"] as? String { tmuxPath = v }
        if let v = obj["output_height"] as? Double, v >= 80, v <= 900 { outputHeight = CGFloat(v) }
        if let v = obj["backdrop"] as? Double, v >= 0, v <= 1 { backdropStrength = v }
        if let v = obj["output_font"] as? String, !v.isEmpty { outputFont = v }
        if let v = obj["output_mode"] as? String, !v.isEmpty { outputMode = v }
        if let v = obj["output_size"] as? Double, v >= 8, v <= 28 { outputSize = CGFloat(v) }
        if let v = obj["output_newest_first"] as? Bool { outputNewestFirst = v }
        if let v = obj["card_opacity"] as? Double, v >= 0, v <= 1 { cardOpacity = v }
        if let v = obj["reopen_on_return"] as? Bool { reopenOnReturn = v }
        if let v = obj["voice_engine"] as? String, !v.isEmpty { voiceEngine = v }
        if let v = obj["whisper_binary"] as? String { whisperBinary = v }
        if let v = obj["whisper_model"] as? String { whisperModel = v }
        if let v = obj["last_target_id"] as? String { lastTargetID = v }
        if let v = obj["history"] as? [String] { history = v }
    }

    func save() {
        let obj: [String: Any] = [
            "y_fraction": Double(yFraction),
            "width": Double(width),
            "hotkey": hotKey,
            "scope_app": scopeApp,
            "language": language,
            "mascot": mascot,
            "tmux_path": tmuxPath,
            "output_height": Double(outputHeight),
            "backdrop": backdropStrength,
            "output_font": outputFont,
            "output_mode": outputMode,
            "output_size": Double(outputSize),
            "output_newest_first": outputNewestFirst,
            "card_opacity": cardOpacity,
            "reopen_on_return": reopenOnReturn,
            "voice_engine": voiceEngine,
            "whisper_binary": whisperBinary,
            "whisper_model": whisperModel,
            "last_target_id": lastTargetID as Any,
            "history": Array(history.suffix(60)),
        ]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        guard let data = try? JSONSerialization.data(withJSONObject: obj, options: [.prettyPrinted, .sortedKeys]) else { return }
        try? data.write(to: file)
    }

    var fileURL: URL { file }
}
