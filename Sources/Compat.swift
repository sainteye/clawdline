import Foundation

/// What this version of Clawdline was built against, and what breaks when that stops being true.
///
/// Almost nothing here is an API. Claude Code writes a transcript, draws a spinner, names a tab
/// and reads the clipboard on Ctrl-V, and Clawdline reads all four — none of which anybody
/// promised to keep stable. That is a fine way to build this tool and a terrible thing to leave
/// undocumented, because **every one of those breaking looks like Clawdline being broken**: an
/// empty pane, a session that never looks busy, an image that does not arrive.
///
/// So the version this was tested against is data rather than a sentence in a README, the app can
/// say when it is looking at something else, and `docs/compatibility.md` is generated from the
/// same table the code uses. A compatibility table that has to be updated by hand is a table that
/// is wrong by the second release.
enum Compat {

    /// One thing Clawdline reads that Claude Code was never obliged to keep still.
    struct Dependency {
        /// What it is, in the words somebody debugging would use.
        let what: String
        /// Where Clawdline reads it.
        let where_: String
        /// What you would see if it changed. This is the useful column: these failures are all
        /// quiet, and every one of them looks like a bug in this app.
        let symptom: String
        /// The oldest Claude Code this is known to work with.
        ///
        /// "not known to have a floor" is the honest answer for most of them and it is not a
        /// shrug: these have been the same for a long time, nobody has gone back to find the
        /// version they started in, and inventing one would make the column mean "probably".
        /// The number that matters is the highest real floor in the list, which is where
        /// `minimum` comes from.
        let since: String
    }

    static let dependencies: [Dependency] = [
        Dependency(
            what: "The session transcript: one JSONL file per session, under ~/.claude/projects/",
            where_: "Transcript.swift",
            symptom: "⌘J shows nothing, or stops partway through a conversation",
            since: "not known to have a floor"),
        Dependency(
            what: "The spinner line Claude Code draws while it works, scraped off the screen",
            where_: "Activity.swift",
            symptom: "The bar never says what a session is doing, even while it is doing it",
            since: "not known to have a floor"),
        Dependency(
            what: "The process being called `claude`",
            where_: "ITerm.swift, Tmux.swift",
            symptom: "No sessions found at all, and nowhere to send a prompt",
            since: "not known to have a floor"),
        Dependency(
            what: "The tab title, and the status glyph Claude Code puts in front of it",
            where_: "Transcript.swift",
            symptom: "The wrong conversation in ⌘J, or a stray glyph in the name",
            since: "not known to have a floor"),
        Dependency(
            what: "Reading an image off the system pasteboard on Ctrl-V, as [Image #N]",
            where_: "Targets.swift",
            symptom: "A dropped image arrives as nothing, and the prompt points at a picture "
                   + "that is not there",
            // Claude Code added Alt-V for Windows in 1.0.93, which puts the macOS Ctrl-V it was
            // added alongside at or before that. The only real floor in this list.
            since: "1.0.93"),
    ]

    /// A Clawdline release and what it was actually run against.
    ///
    /// `tested` is the version somebody had installed while using it, not a guarantee about a
    /// range — claiming a range nobody tried is how a compatibility table starts lying.
    struct Release {
        let clawdline: String
        let claudeCode: String
        let notes: String
    }

    /// Newest first.
    static let releases: [Release] = [
        Release(clawdline: "0.4.0", claudeCode: "2.1.233",
                notes: "Images go over as [Image #3] rather than as paths, which adds the "
                     + "clipboard-on-Ctrl-V dependency."),
        // "not recorded" and not a guess. These three shipped before anybody was writing this
        // down, and filling them in with what happens to be installed today would make the
        // column mean "probably" — after which the whole table means "probably".
        Release(clawdline: "0.3.0", claudeCode: "not recorded",
                notes: "Transcript reading, the spinner line, and the project footer."),
        Release(clawdline: "0.2.0", claudeCode: "not recorded",
                notes: "Mascot packs and the picker."),
        Release(clawdline: "0.1.0", claudeCode: "not recorded", notes: "First release."),
    ]

    /// The status-file format Clawdline reads, which claude-bestiary also writes.
    ///
    /// Its own version is deliberately not named here. It is one producer of these files and the
    /// contract is the files — `docs/project-status.md` — so pinning a version of it would say
    /// something untrue about everything else that writes them. A missing or unreadable file is a
    /// normal state, so there is no symptom to warn about: the footer simply has less to say.
    static let statusFormat = "1"

    /// The oldest Claude Code any of this is known to work with: the highest floor in
    /// `dependencies`, since one broken dependency is a broken feature.
    ///
    /// Everything below that line still works — this is not a version check that refuses to run,
    /// and there is no reason for one. What an older Claude Code costs you is the feature whose
    /// floor you are under, and the table says which.
    static var minimum: String {
        dependencies.map(\.since).filter { $0.first?.isNumber == true }
            .max { compare($0, $1) == .orderedAscending } ?? "not known to have a floor"
    }

    // MARK: - What is actually installed

    /// Ask the `claude` on the PATH what it is, once per launch.
    ///
    /// Cached because this is read while a menu is opening and shelling out on that path would
    /// be felt. Not finding it is a perfectly ordinary answer — Claude Code may be installed
    /// somewhere an app's PATH does not reach, and having nothing to say is better than saying
    /// something wrong about it.
    private static var asked: String??
    static func installedClaudeVersion() -> String? {
        if let asked { return asked }
        let found = run(["/usr/bin/env", "claude", "--version"]).flatMap(version(from:))
        asked = found
        return found
    }

    private static func run(_ argv: [String]) -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: argv[0])
        task.arguments = Array(argv.dropFirst())
        let pipe = Pipe()
        task.standardOutput = pipe
        task.standardError = FileHandle.nullDevice
        guard (try? task.run()) != nil else { return nil }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        task.waitUntilExit()
        guard task.terminationStatus == 0 else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    /// `claude --version` prints something like "2.1.233 (Claude Code)".
    static func version(from output: String) -> String? {
        let scalars = output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let first = scalars.split(separator: " ").first else { return nil }
        let v = String(first)
        return v.first?.isNumber == true ? v : nil
    }

    /// Compare two dotted versions. Missing parts count as zero, so "2.1" < "2.1.1".
    static func compare(_ a: String, _ b: String) -> ComparisonResult {
        let x = a.split(separator: ".").map { Int($0) ?? 0 }
        let y = b.split(separator: ".").map { Int($0) ?? 0 }
        for i in 0..<max(x.count, y.count) {
            let l = i < x.count ? x[i] : 0
            let r = i < y.count ? y[i] : 0
            if l != r { return l < r ? .orderedAscending : .orderedDescending }
        }
        return .orderedSame
    }

    /// What to say about the version in front of us, if anything.
    ///
    /// Only when it is **older** than the one this was built against. A newer Claude Code is the
    /// normal state of the world — it updates itself, this does not — and a warning that fires
    /// every week is one nobody reads by the time it matters. Older is the case worth naming,
    /// because then a missing feature really is a missing feature rather than a bug here.
    /// The newest release that names a version anybody actually checked.
    static var builtAgainst: String {
        releases.first { $0.claudeCode.first?.isNumber == true }?.claudeCode ?? ""
    }

    static func note(installed: String?, builtAgainst: String = Compat.builtAgainst) -> String? {
        guard let installed, !builtAgainst.isEmpty else { return nil }
        guard compare(installed, builtAgainst) == .orderedAscending else { return nil }
        return "Claude Code \(installed); this was built against \(builtAgainst)"
    }
}
