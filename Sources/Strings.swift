import Foundation

/// Every piece of UI copy, one implementation per language.
///
/// Why a protocol instead of a `.strings` catalog: the compiler refuses to build a language that
/// is missing a string. With `.strings` files a missing key is a blank label at runtime, and
/// nobody notices until somebody who reads that language complains — which is to say, after it
/// has already shipped.
///
/// **Adding a language:** copy `Copy+English.swift` to `Copy+<Language>.swift`, translate the
/// values, and add one line to `L.catalog`. Nothing else, and the build tells you if you missed
/// a string.
///
/// Two rules for the copy itself:
///
/// - **Say what happened, not what went wrong internally.** "Could not send" is for a person;
///   "AppleScript returned -1728" is for a log.
/// - **Keep the hint words short.** They sit in one row along the bottom of the card, and a long
///   word there pushes another one off the end rather than wrapping.
protocol Copy {
    // Input field
    var placeholder: String { get }

    // Hint row
    var hintSend: String { get }
    var hintNewline: String { get }
    var hintSwitch: String { get }
    var hintList: String { get }
    var hintMascot: String { get }
    var hintOutput: String { get }
    var hintFullscreen: String { get }
    var hintKeys: String { get }
    var hintTextSize: String { get }
    var hintOrder: String { get }
    var hintVoice: String { get }
    func voiceListening(onDevice: Bool) -> String
    var voiceNoPermission: String { get }
    var voiceUnavailable: String { get }
    func voiceTranscribing(seconds: Double) -> String
    var whisperMissing: String { get }
    var whisperNothingHeard: String { get }
    func dictationStatus(_ status: Whisper.Status) -> String
    func voiceListeningWhisper() -> String

    // Target state
    var scanning: String { get }
    var noSession: String { get }
    var nothingToSend: String { get }
    var sendFailed: String { get }
    var itermSilent: String { get }
    var scriptMissing: String { get }
    var cannotList: String { get }
    var noOutput: String { get }
    func outputSize(_ pt: Int) -> String
    func foldedTools(_ count: Int) -> String
    func outputOrder(newestFirst: Bool) -> String
    func backlogNow(_ count: Int) -> String
    func dropped(_ count: Int) -> String
    /// Shown once before a project's `.devstack.json` command is run for the first time.
    ///
    /// **The command itself goes in the message.** What that file names is arbitrary code out of
    /// a repository, and "do you trust this workspace" is a question nobody has the information
    /// to answer — the line that is about to run is the only honest way to ask.
    func stackConfirm(_ command: String) -> String
    /// The key-row word for the stack list. One short noun — the row is one line wide.
    var hintStacks: String { get }
    /// Hover text for the servers mark in the footer.
    ///
    /// It sits next to the session counter, and "6/6" beside "3/7" reads as two of the same
    /// thing when they are not related at all — so say which is which in words.
    func stackTip(up: Int, total: Int) -> String
    var stackTipUnknown: String { get }
    /// The row's own words for "we have not been allowed to ask yet".
    ///
    /// **A mark on its own is not enough here.** A grey square next to a green one reads as
    /// "down" — that misreading happened the first day this shipped, and it sent someone
    /// looking for an outage that did not exist. Untrusted is not a health state; it must not
    /// look like one.
    var stackUntrusted: String { get }
    /// The words on a row's action button.
    ///
    /// **A row must not be an invisible button.** The first version made the whole row pressable
    /// and worked out what to do from the state — so one click silently took a public site down
    /// for a minute, with nothing on screen having offered to. What the press will do has to be
    /// written on the thing you press.
    var stackActionStart: String { get }
    var stackActionRestart: String { get }
    /// Stopping was on a modifier key and nothing else for a while — which is to say it was not
    /// really there. A capability only reachable by a keystroke nobody has been told about is
    /// one the person will conclude does not exist, and go looking for a terminal.
    var stackActionStop: String { get }
    /// The one button on the row that changes nothing — so it needs no confirmation, and it is
    /// the answer to "where do I see what these servers printed".
    var stackActionLogs: String { get }
    /// The log pane's tabs: one per process, plus this one for the lot.
    var stackLogAll: String { get }
    /// The way out. A pane you can get into and not out of is a trap, and ⌘J alone did not read
    /// as an exit once the thing on screen was no longer a transcript.
    var stackLogBack: String { get }
    var stackActionAllow: String { get }
    /// What the button says once it is armed and waiting to be confirmed.
    var stackActionAgain: String { get }
    func sessionTip(index: Int, total: Int) -> String
    /// What a session row says when there is a question on its screen and nobody has answered it.
    ///
    /// **Short, and addressed to the reader.** It sits after a tab title on a one-line row, so a
    /// long phrase pushes the title — which is what the row is for — off the end. "Waiting" on
    /// its own is the wrong word: everything in that list is waiting for something. This one is
    /// waiting for *you*, and that is the whole of what the row has to get across.
    var sessionWaiting: String { get }
    /// What the island says when a session that had been running stops.
    ///
    /// **One word.** It is drawn in small capitals above a task name in a strip about as wide as
    /// a notch, and it is on screen for three seconds — a sentence there is a sentence nobody
    /// finishes reading.
    var islandDone: String { get }
    /// Hover text for the menu bar mark. Names the sessions, because the mark can only say that
    /// *something* wants you and the next question is always which.
    func statusWaiting(_ labels: [String]) -> String
    func statusWorking(_ count: Int) -> String

    // Settings window
    //
    // Labels for controls, so: short, and a noun rather than a sentence. The row already says
    // what it is by what it does; the label only has to name it.
    var settingsTitle: String { get }
    var settingsGeneral: String { get }
    var settingsBar: String { get }
    var settingsReading: String { get }
    var settingsVoice: String { get }
    var settingsHotkey: String { get }
    /// Shown on the hotkey button while it is listening for one.
    var settingsRecording: String { get }
    var settingsScope: String { get }
    var settingsScopeGlobal: String { get }
    var settingsScopeHint: String { get }
    var settingsLanguage: String { get }
    var settingsReopen: String { get }
    var settingsFollow: String { get }
    var settingsNotch: String { get }
    var settingsNotchHint: String { get }
    var settingsPosition: String { get }
    var settingsWidth: String { get }
    var settingsOpacity: String { get }
    var settingsImagesPaste: String { get }
    var settingsShow: String { get }
    var settingsPaneHeight: String { get }
    var settingsTextSize: String { get }
    var settingsPaneFont: String { get }
    var settingsBlur: String { get }
    var settingsNewestFirst: String { get }
    var settingsEngine: String { get }
    var settingsSettle: String { get }
    var settingsStop: String { get }
    var settingsAuto: String { get }
    var settingsTranscript: String { get }
    var settingsTerminal: String { get }
    var settingsOff: String { get }
    var settingsOpenFile: String { get }
    /// A number of seconds, as a settings row shows it.
    ///
    /// Here rather than as a bare "s" because it is the one unit in that window that reads as a
    /// foreign word: "1.8 s" is not how a duration is written in every language that this speaks.
    func settingsSeconds(_ value: Double) -> String

    // Menu bar
    var menuOpen: String { get }
    var menuReveal: String { get }
    var menuMascot: String { get }
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

enum L {
    /// The active language. Cached because it is read on every redraw.
    private(set) static var t: Copy = pick()

    /// Call this after the config changes.
    static func reload() { t = pick() }

    /// Matched by prefix, so **more specific tags come first** — `zh-Hans` must not fall into the
    /// Traditional bucket, and it would, because `"zh-Hans-CN".hasPrefix("zh-Hant")` is false but
    /// a bare `"zh"` entry ahead of it would swallow both.
    ///
    /// One entry per script or region only where the words actually differ. Portuguese is one
    /// entry because the interface words are the same on both sides of the Atlantic; Chinese is
    /// two because they are not written in the same characters.
    static let catalog: [(tag: String, copy: Copy)] = [
        ("en", English()),
        ("zh-Hant", TraditionalChinese()),
        ("zh-TW", TraditionalChinese()),
        ("zh-HK", TraditionalChinese()),
        ("zh-MO", TraditionalChinese()),
        ("zh-Hans", SimplifiedChinese()),
        ("zh-CN", SimplifiedChinese()),
        ("zh-SG", SimplifiedChinese()),
        ("zh", SimplifiedChinese()),
        ("ja", Japanese()),
        ("ko", Korean()),
        ("es", Spanish()),
        ("pt", Portuguese()),
        ("fr", French()),
        ("de", German()),
        ("ru", Russian()),
        ("it", Italian()),
        ("hi", Hindi()),
        ("id", Indonesian()),
        ("tr", Turkish()),
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
