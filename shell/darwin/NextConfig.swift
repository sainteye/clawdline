// The few settings the shell reads, and where it keeps what it writes.
//
// Everything lives under this app's own directory — the one the daemon resolves
// in internal/config: $CLAWDLINE_NEXT_DIR, else $XDG_CONFIG_HOME/clawdline-next,
// else ~/.config/clawdline-next. Never ~/.config/clawdline: that is the Swift
// app's, it is running, and two apps writing one config is two writers.
//
// The keys are the Swift app's spellings, so a line copied from one file means
// the same thing in the other. What differs is the defaults, on purpose:
//
//   hotkey     no default. Absent or empty registers nothing. The Swift app
//              defaults to option+space and is using it right now; a second app
//              taking the same combination breaks the one the person is using.
//   scope_app  com.googlecode.iterm2, as in the Swift app. Only read when a
//              hotkey is set.
//   mascot     clawd, as in the Swift app. Shown in the menu, and drawn by the
//              notch island.
//   notch      true, as in the Swift app. False and the island is not created
//              at all — no window, no bridge, no drawing.
import Foundation

final class NextConfig {
    static let shared = NextConfig()

    private(set) var hotKey = ""
    private(set) var scopeApp = "com.googlecode.iterm2"
    private(set) var mascot = "clawd"
    private(set) var notch = true

    /// Why the last load did not take the file at its word, or nil. Written to
    /// the log by whoever loaded; a config that is silently ignored looks
    /// exactly like one that is obeyed.
    private(set) var problem: String?

    static var directory: URL {
        let env = ProcessInfo.processInfo.environment
        if let dir = env["CLAWDLINE_NEXT_DIR"], !dir.isEmpty {
            return URL(fileURLWithPath: dir, isDirectory: true)
        }
        if let xdg = env["XDG_CONFIG_HOME"], !xdg.isEmpty {
            return URL(fileURLWithPath: xdg, isDirectory: true)
                .appendingPathComponent("clawdline-next", isDirectory: true)
        }
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline-next", isDirectory: true)
    }

    var fileURL: URL { Self.directory.appendingPathComponent("config.json") }

    private init() { load() }

    /// Read the file again. Every key falls back to its default when the file,
    /// or the key, is missing or unreadable — a missing config must still launch.
    func load() {
        hotKey = ""
        scopeApp = "com.googlecode.iterm2"
        mascot = "clawd"
        notch = true
        problem = nil

        guard FileManager.default.fileExists(atPath: fileURL.path) else { return }
        guard let data = try? Data(contentsOf: fileURL),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            problem = "\(fileURL.path) is not a JSON object; using defaults"
            return
        }
        if let v = obj["hotkey"] as? String {
            hotKey = v.trimmingCharacters(in: .whitespaces)
        }
        if let v = obj["scope_app"] as? String { scopeApp = v }
        if let v = obj["mascot"] as? String, !v.isEmpty { mascot = v }
        if let v = obj["notch"] as? Bool { notch = v }
    }
}

/// Whether the window has been put away once, which is what the Swift app's
/// onboarding completion decides at launch: until then a launch opens the
/// window, afterwards it stays quiet until the Dock, the menu or the hotkey
/// asks. Versioned for the same reason the original is — a later introduction
/// can be shown once without pretending a preference disappeared.
struct IntroductionStore {
    static let currentVersion = 1

    var fileURL: URL { NextConfig.directory.appendingPathComponent("shell-introduced.json") }

    var isCurrent: Bool {
        guard let data = try? Data(contentsOf: fileURL),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let version = obj["version"] as? Int else { return false }
        return version >= Self.currentVersion
    }

    @discardableResult
    func record() -> Bool {
        let obj: [String: Any] = [
            "version": Self.currentVersion,
            "completed_at": ISO8601DateFormatter().string(from: Date()),
        ]
        do {
            try FileManager.default.createDirectory(at: fileURL.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            let data = try JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
            try data.write(to: fileURL, options: .atomic)
            return true
        } catch {
            shellLog("introduction: could not record — \(error.localizedDescription)")
            return false
        }
    }
}

/// One line to stdout, flushed. The shell has no log file of its own; whoever
/// launched it from a terminal reads this, and Console.app has the rest.
func shellLog(_ line: String) {
    print(line)
    fflush(stdout)
}
