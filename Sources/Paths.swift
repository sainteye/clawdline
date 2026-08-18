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

    /// A stamp that changes when a file's contents change — **through a symlink**.
    ///
    /// Size and modification time, which is the cheap way to ask "is what I cached still what is
    /// on disk". The part worth writing down is the `resolvingSymlinksInPath()`, because leaving
    /// it out produced a cache that never expired: `~/.claude/project-icons.json` is normally a
    /// symlink into a checkout, and **neither `attributesOfItem(atPath:)` nor
    /// `URL.resourceValues(forKeys:)` follows one** — both describe the link, which is 64 bytes
    /// and has the modification time of the day somebody made it. So the stamp was a constant,
    /// the registry could be rewritten all afternoon, and only relaunching the app picked it up.
    ///
    /// The file that had this bug carries a comment saying that file is usually a symlink. Knowing
    /// it and stat'ing it are different things.
    static func stamp(of url: URL) -> String {
        let real = url.resolvingSymlinksInPath()
        let attrs = try? FileManager.default.attributesOfItem(atPath: real.path)
        let size = (attrs?[.size] as? Int) ?? 0
        let modified = (attrs?[.modificationDate] as? Date)?.timeIntervalSince1970 ?? 0
        return "\(size)-\(Int(modified))"
    }

    /// `~` and `~/…` only. Not `~someone`, which NSString's version handles and nobody means.
    static func expand(_ path: String) -> String {
        guard path == "~" || path.hasPrefix("~/") else { return path }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return path == "~" ? home : home + String(path.dropFirst(1))
    }
}
