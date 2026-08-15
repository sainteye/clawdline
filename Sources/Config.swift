import AppKit

/// Settings and history, kept in ~/.config/clawdline/.
/// Everything has a default: a missing, corrupt or half-written config must still launch.
final class Config {
    static let shared = Config()

    var yFraction: CGFloat = 0.30      // where the panel top sits, as a fraction of screen height
    var width: CGFloat = 720
    var hotKey = "option+space"
    /// The hotkey only fires while this app is frontmost. Empty string means global.
    /// Done in-app rather than handed to a hotkey utility: registering globally takes ⌥Space away
    /// from every other app, while registering per-frontmost leaves them exactly as they were.
    var scopeApp = "com.googlecode.iterm2"
    /// "auto" follows the system, or a tag such as "en" / "zh-Hant"
    var language = "auto"
    /// Which mascot pack to draw. Files live in ~/.config/clawdline/mascots/<name>.json
    var mascot = "clawd"
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
            "last_target_id": lastTargetID as Any,
            "history": Array(history.suffix(60)),
        ]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        guard let data = try? JSONSerialization.data(withJSONObject: obj, options: [.prettyPrinted, .sortedKeys]) else { return }
        try? data.write(to: file)
    }

    var fileURL: URL { file }
}
