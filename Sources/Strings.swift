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
