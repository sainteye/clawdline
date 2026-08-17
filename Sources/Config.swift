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
    /// What language to transcribe in: a BCP-47 tag like "zh-TW" or "en", or "auto".
    ///
    /// "auto" lets Whisper decide, which is what you want when you really do switch languages —
    /// and what you do not want in a quiet room, because a model asked to identify silence will
    /// pick something. Naming a language also fixes the script: Whisper writes Simplified for
    /// Chinese unless told otherwise.
    var voiceLanguage = "auto"
    /// How long a pause ends a sentence, in seconds. 0 turns it off.
    ///
    /// At a pause the words so far are fixed: Whisper reads that stretch and replaces it, and
    /// nothing after that point rewrites it. Without this, a two-minute dictation is one lump
    /// that gets re-transcribed at the end — slower, and everything you already read moves.
    var voiceSettleSeconds: Double = 1.8
    /// How long a silence ends the whole session, in seconds. 0 leaves the microphone on until
    /// it is pressed again.
    ///
    /// Longer than a settle, because these are different claims: a pause says "that sentence is
    /// finished", a long one says "I am finished". Being late costs an open microphone in a room
    /// where nobody is talking; being early costs the keystroke this exists to remove.
    var voiceStopSeconds: Double = 4.0
    /// Words a transcriber cannot be expected to know, put back afterwards.
    ///
    /// Names, product names, the odd piece of jargon — anything that comes back as something
    /// that merely sounds right ("cloud code"). Apple's recogniser is told to expect them;
    /// Whisper is not, because its only lever is a writing sample and a list of words in one
    /// costs all the punctuation. So they are repaired in the text instead, which is
    /// deterministic and cannot make the sentence worse.
    var voiceVocabulary: [String] = []
    /// Hand images over as images rather than as paths.
    ///
    /// Claude Code turns an image on the system pasteboard into `[Image #3]` when it receives a
    /// Ctrl-V, which is a keystroke rather than text — so the send is split around it and the
    /// pasteboard is borrowed and handed back. What you get is the picture in the message and a
    /// number you can point at, instead of forty characters of directory.
    ///
    /// Set false to go back to sending the path, which is never wrong, only plainer. It also
    /// falls back on its own for anything that is not a Claude Code session, because Ctrl-V in a
    /// shell means something else entirely.
    var sendImagesAsPaste = true
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
    /// Where the project status files are read from, and where the icon registry lives.
    ///
    /// Both default to what claude-tools writes, because that is what most people reading this
    /// already have — but the format is documented (docs/project-status.md) so that anything can
    /// produce them, and a producer that is not claude-tools should not have to impersonate it
    /// to be found. Blank means the default; `~` is expanded.
    var statusDir = ""
    var iconsFile = ""
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
        if let v = obj["status_dir"] as? String { statusDir = v }
        if let v = obj["icons_file"] as? String { iconsFile = v }
        if let v = obj["output_height"] as? Double, v >= 80, v <= 900 { outputHeight = CGFloat(v) }
        if let v = obj["backdrop"] as? Double, v >= 0, v <= 1 { backdropStrength = v }
        if let v = obj["output_font"] as? String, !v.isEmpty { outputFont = v }
        if let v = obj["output_mode"] as? String, !v.isEmpty { outputMode = v }
        if let v = obj["output_size"] as? Double, v >= 8, v <= 28 { outputSize = CGFloat(v) }
        if let v = obj["output_newest_first"] as? Bool { outputNewestFirst = v }
        if let v = obj["card_opacity"] as? Double, v >= 0, v <= 1 { cardOpacity = v }
        if let v = obj["reopen_on_return"] as? Bool { reopenOnReturn = v }
        if let v = obj["voice_engine"] as? String, !v.isEmpty { voiceEngine = v }
        if let v = obj["voice_settle_seconds"] as? Double, v >= 0, v <= 30 { voiceSettleSeconds = v }
        if let v = obj["voice_stop_seconds"] as? Double, v >= 0, v <= 300 { voiceStopSeconds = v }
        if let v = obj["voice_vocabulary"] as? [String] { voiceVocabulary = v }
        if let v = obj["send_images_as_paste"] as? Bool { sendImagesAsPaste = v }
        if let v = obj["voice_language"] as? String, !v.isEmpty { voiceLanguage = v }
        if let v = obj["whisper_binary"] as? String { whisperBinary = v }
        if let v = obj["whisper_model"] as? String { whisperModel = v }
        if let v = obj["last_target_id"] as? String { lastTargetID = v }
        if let v = obj["history"] as? [String] { history = v }
        // What the file said, so a later save can tell an edit of ours from an edit of theirs.
        known = obj
    }

    /// Everything this object holds, as it would be written.
    private var serialised: [String: Any] {
        var obj: [String: Any] = [
            "y_fraction": Double(yFraction),
            "width": Double(width),
            "hotkey": hotKey,
            "scope_app": scopeApp,
            "language": language,
            "mascot": mascot,
            "tmux_path": tmuxPath,
            "status_dir": statusDir,
            "icons_file": iconsFile,
            "output_height": Double(outputHeight),
            "backdrop": backdropStrength,
            "output_font": outputFont,
            "output_mode": outputMode,
            "output_size": Double(outputSize),
            "output_newest_first": outputNewestFirst,
            "card_opacity": cardOpacity,
            "reopen_on_return": reopenOnReturn,
            "voice_engine": voiceEngine,
            "voice_settle_seconds": voiceSettleSeconds,
            "voice_stop_seconds": voiceStopSeconds,
            "voice_vocabulary": voiceVocabulary,
            "send_images_as_paste": sendImagesAsPaste,
            "voice_language": voiceLanguage,
            "whisper_binary": whisperBinary,
            "whisper_model": whisperModel,
            "history": Array(history.suffix(60)),
        ]
        // Only when there is one. A Swift Optional in this dictionary is not a JSON value, and
        // JSONSerialization throws on it — which `try?` then swallowed, so on a fresh install
        // nothing was saved at all until a target had been picked.
        if let id = lastTargetID { obj["last_target_id"] = id }
        return obj
    }

    /// What was on disk when we last read or wrote it. The reference point for "did we change
    /// this, or did they?"
    private var known: [String: Any] = [:]

    /// Keep what somebody edited by hand, keep what the app changed, and do not make them race.
    ///
    /// The app rewrites this file whenever anything moves — a prompt goes into the history, ⌘+
    /// changes the text size — and it used to write everything it held in memory. So editing
    /// the file while the app was running lost the edit, silently and at an unpredictable
    /// moment. "Quit first" was the documented answer, which is another way of saying the
    /// feature did not work.
    ///
    /// A key the app has not touched since it read the file is the file's to answer. A key the
    /// app has changed is the app's. Keys it does not know about are passed through untouched,
    /// so a setting from a newer version survives being opened by an older one.
    ///
    /// Both changed the same key: the app wins. It changed it because somebody pressed
    /// something, and that is the more recent of the two intentions we can see.
    static func merged(mine: [String: Any], known: [String: Any],
                       onDisk: [String: Any]) -> [String: Any] {
        var out = onDisk
        for (key, value) in mine where !equal(value, known[key]) { out[key] = value }
        for (key, value) in mine where out[key] == nil { out[key] = value }
        return out
    }

    private static func equal(_ a: Any?, _ b: Any?) -> Bool {
        switch (a, b) {
        case (nil, nil): return true
        case (nil, _), (_, nil): return false
        default: return (a as? NSObject)?.isEqual(b as? NSObject) ?? false
        }
    }

    func save() {
        let mine = serialised
        let onDisk = (try? Data(contentsOf: file))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        let obj = Self.merged(mine: mine, known: known, onDisk: onDisk)

        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys])
        else {
            Log.write("config: could not serialise, nothing written")
            return
        }
        do {
            try data.write(to: file)
            known = obj
        } catch {
            // Worth a line: everything above this is best-effort, and a config that silently
            // stops persisting looks exactly like one that is being ignored.
            Log.write("config: could not write — \(error.localizedDescription)")
        }
    }

    var fileURL: URL { file }
}
