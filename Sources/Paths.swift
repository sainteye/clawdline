import Foundation

/// Turning what somebody wrote in a config file into a path.
///
/// One place, because every setting that names a file has the same two problems: a blank means
/// "use the default" rather than "the root directory", and `~` is a shell convention that no
/// file API expands. Getting the second one wrong is a particular kind of annoying — the app
/// looks in a directory literally called `~`, finds nothing, and reports nothing missing,
/// because a file that is not there is a normal state for all of these.
enum Paths {
    /// nil when the setting is blank, which every caller reads as "use your own default".
    static func resolve(_ setting: String) -> URL? {
        let trimmed = setting.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return URL(fileURLWithPath: expand(trimmed))
    }

    /// `~` and `~/…` only. Not `~someone`, which NSString's version handles and nobody means.
    static func expand(_ path: String) -> String {
        guard path == "~" || path.hasPrefix("~/") else { return path }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return path == "~" ? home : home + String(path.dropFirst(1))
    }
}
