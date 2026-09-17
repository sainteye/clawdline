// What the status menu's dynamic rows are made of.
//
// Each is a port of the Swift app's reading, cut down to what a menu row needs:
// the dictation engine (Whisper.swift), the assistant versions this build was
// checked against (Compat.swift), the mascot packs (Mascot.swift) and the update
// offer (UpdateCheck.swift). None of them draws anything; the menu does.
import Foundation

/// Run a program and return what it printed, or nil.
///
/// Bounded, because the Swift app learned that a `--version` which never
/// returns holds whatever called it. Here nothing on the main thread calls it —
/// the readings are taken on a utility queue — but a menu that never fills is
/// the same failure one step later.
func runBounded(_ argv: [String], timeout: TimeInterval = 5) -> String? {
    let task = Process()
    task.executableURL = URL(fileURLWithPath: argv[0])
    task.arguments = Array(argv.dropFirst())
    let pipe = Pipe()
    task.standardOutput = pipe
    task.standardError = FileHandle.nullDevice
    guard (try? task.run()) != nil else { return nil }
    let killer = DispatchWorkItem { if task.isRunning { task.terminate() } }
    DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: killer)
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    task.waitUntilExit()
    killer.cancel()
    guard task.terminationReason == .exit, task.terminationStatus == 0 else { return nil }
    return String(decoding: data, as: UTF8.self)
}

// MARK: - Dictation

/// Whether whisper.cpp is installed, as the Swift app reads it with nothing
/// configured. This shell does not dictate: the row is shown so the menu is the
/// same menu, and it is disabled because there is nothing behind it here.
enum DictationEngine {
    enum Status: Equatable {
        case ready(model: String)
        case noBinary
        case noModel
    }

    static func status() -> Status {
        guard binary() != nil else { return .noBinary }
        guard let model = model() else { return .noModel }
        return .ready(model: (model as NSString).lastPathComponent)
    }

    static func binary() -> String? {
        var candidates: [String] = []
        for dir in ["/opt/homebrew/bin", "/usr/local/bin"] {
            candidates += ["\(dir)/whisper-cli", "\(dir)/whisper-cpp", "\(dir)/main"]
        }
        if let found = candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) }) {
            return found
        }
        let out = runBounded(["/usr/bin/env", "which", "whisper-cli"])?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return (out?.isEmpty == false) ? out : nil
    }

    /// The largest `ggml-*.bin` in the usual places — largest because somebody
    /// who downloaded two meant the big one. The same three places the Swift
    /// app looks; the second is its model directory, read and never written.
    static func model() -> String? {
        let fm = FileManager.default
        let home = fm.homeDirectoryForCurrentUser
        let places = [
            home.appendingPathComponent(".cache/whisper"),
            home.appendingPathComponent("Library/Application Support/Clawdline/models"),
            home.appendingPathComponent("models"),
        ]
        var best: (path: String, size: Int)?
        for dir in places {
            let names = (try? fm.contentsOfDirectory(atPath: dir.path)) ?? []
            for name in names where name.hasPrefix("ggml-") && name.hasSuffix(".bin")
                                    && !name.contains("for-tests") {
                let path = dir.appendingPathComponent(name).path
                let size = ((try? fm.attributesOfItem(atPath: path))?[.size] as? Int) ?? 0
                if best == nil || size > best!.size { best = (path, size) }
            }
        }
        return best?.path
    }
}

// MARK: - Compatibility

/// An assistant older than the one this build was checked against, or newer
/// than it *and* a Clawdline release waiting. Never merely newer: that is the
/// ordinary state of the world, and a row that is there every week is one
/// nobody reads on the week it matters.
enum Compat {
    /// The newest versions the Swift app's release table records as checked
    /// (Compat.releases, Clawdline 0.8.0). This daemon drives the same two
    /// assistants through the same files, so it inherits the same floor until
    /// it records one of its own.
    static let builtAgainst = "2.1.261"
    static let builtAgainstCodex = "0.153.4"

    enum Standing: Equatable {
        case behind(program: String, installed: String, builtAgainst: String)
        case ahead(program: String, installed: String, builtAgainst: String, release: String)
    }

    static func standings(newerRelease: String?) -> [Standing] {
        [standing(program: "Claude Code", installed: installedVersion("claude"),
                  builtAgainst: builtAgainst, newerRelease: newerRelease),
         standing(program: "Codex", installed: installedVersion("codex"),
                  builtAgainst: builtAgainstCodex, newerRelease: newerRelease)].compactMap { $0 }
    }

    static func standing(program: String, installed: String?, builtAgainst: String,
                         newerRelease: String?) -> Standing? {
        guard let installed, !builtAgainst.isEmpty else { return nil }
        switch compare(installed, builtAgainst) {
        case .orderedAscending:
            return .behind(program: program, installed: installed, builtAgainst: builtAgainst)
        case .orderedDescending:
            guard let newerRelease, !newerRelease.isEmpty else { return nil }
            return .ahead(program: program, installed: installed, builtAgainst: builtAgainst,
                          release: newerRelease)
        case .orderedSame:
            return nil
        }
    }

    /// What `<command> --version` says. `claude` prints "2.1.233 (Claude Code)"
    /// and `codex` prints "codex-cli 0.149.0", so it is the first word that
    /// starts with a digit.
    static func installedVersion(_ command: String) -> String? {
        guard let out = runBounded(["/usr/bin/env", command, "--version"]) else { return nil }
        let words = out.trimmingCharacters(in: .whitespacesAndNewlines)
            .split(whereSeparator: { $0 == " " || $0 == "\n" })
        for word in words where word.first?.isNumber == true { return String(word) }
        return nil
    }

    /// Dotted versions; missing parts count as zero, so "2.1" < "2.1.1".
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
}

// MARK: - Updates

/// The Swift app asks GitHub once a day whether sainteye/clawdline has a newer
/// release and offers it in the menu. This app has no release feed: its
/// version is a commit, and nothing publishes Clawdline Next. So the offer is
/// always absent, and the log says why once — absent because there is no feed
/// is not the same as absent because nothing is newer.
enum UpdateOffer {
    static let feed: URL? = nil

    static var newerRelease: String? { nil }

    static var installedVersion: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "?"
    }
}

// MARK: - Mascots

/// The packs the menu lists: this app's own directory, then the ones bundled
/// with it. Nothing in this app draws a mascot yet — the panel and the notch
/// that did are not ported — so the menu shows the choice and cannot make one.
enum MascotPacks {
    static var userDirectory: URL {
        NextConfig.directory.appendingPathComponent("mascots", isDirectory: true)
    }

    private static var bundled: URL? {
        Bundle.main.resourceURL?.appendingPathComponent("mascots", isDirectory: true)
    }

    /// Copy the bundled packs into the user directory once, as the Swift app
    /// does into its own: a file you can see and edit is worth more than a
    /// documented one you have to go and find. A copy already there is never
    /// overwritten.
    static func installBundled() {
        let fm = FileManager.default
        guard let dir = bundled,
              let names = try? fm.contentsOfDirectory(atPath: dir.path),
              names.contains(where: { $0.hasSuffix(".json") }) else { return }
        try? fm.createDirectory(at: userDirectory, withIntermediateDirectories: true)
        for file in names where file.hasSuffix(".json") {
            let dest = userDirectory.appendingPathComponent(file)
            guard !fm.fileExists(atPath: dest.path) else { continue }
            try? fm.copyItem(at: dir.appendingPathComponent(file), to: dest)
        }
    }

    /// Sorted and de-duplicated; a user copy shadows a bundled one.
    static func available() -> [String] {
        let fm = FileManager.default
        var names = Set<String>()
        for dir in [userDirectory, bundled].compactMap({ $0 }) {
            for file in (try? fm.contentsOfDirectory(atPath: dir.path)) ?? [] where file.hasSuffix(".json") {
                names.insert(String(file.dropLast(5)))
            }
        }
        return names.sorted()
    }
}

/// Everything the dynamic rows need, read together off the main thread.
struct MenuReadings {
    var dictation: DictationEngine.Status = .noBinary
    var standings: [Compat.Standing] = []
    var newerRelease: String?
    var mascots: [String] = []

    static func take() -> MenuReadings {
        let newer = UpdateOffer.newerRelease
        return MenuReadings(dictation: DictationEngine.status(),
                            standings: Compat.standings(newerRelease: newer),
                            newerRelease: newer,
                            mascots: MascotPacks.available())
    }
}
