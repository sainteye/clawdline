import Foundation

/// The diagnostic a browser produced, written to a path anybody can name without being told.
///
/// **Why this exists.** The recorder in `Resources/web/app/js/core/layout-diagnostics.js` can see
/// things no Mac-side test can: what a home-screen web app did on a phone. Until now the only way
/// out of it was `Copy report` and a paste, and on 2026-09-06 the report was long enough that
/// pasting it into a conversation hung the program — with Universal Clipboard not syncing either.
/// A reading that cannot be read is not a reading, and the person was left reciting a string off a
/// screen, which is an engineering problem turned into his afternoon.
///
/// So the panel posts it here instead, and the standing arrangement for a browser-side defect
/// becomes: add an observation point, ask for one press, read the file.
///
/// **The path is fixed, and that is the feature.** Not a UUID, not "the newest file in a
/// directory": an agent asked to read this has no way to ask which one, and `docs/diagnostics.md`
/// can only name a constant. Two names exist — `report.json` for the press that just happened and
/// `previous.json` for the one before it, so a second press does not destroy the reading somebody
/// was about to open. Two files with fixed names is also the whole of "cannot grow without
/// bound": the directory holds at most `maxBytes` twice, whatever anybody sends.
///
/// **It carries nothing this route understands.** The envelope names when, who and how big; the
/// page's own document goes inside `report` untouched. Today it contains page, route and layout
/// events; the next defect will have different ones, and nothing here has to change for that —
/// this refuses a body that is not a JSON object and has no opinion about what is in the object.
enum DiagnosticReport {
    /// Two megabytes, and the number has two jobs.
    ///
    /// It is far above any real report — the recorder keeps 80 trace entries and 5 incidents, which
    /// measured in the tens of kilobytes — and far below `RemoteServer.bodyLimit`, so a report that
    /// is refused is refused *here*, by name, rather than by the parser's generic `too_large` about
    /// pictures. And it is what bounds the directory: two files, this each, and nothing else.
    static let maxBytes = 2 << 20

    /// The newest report, and the one before it. Fixed names on purpose — see the type's note.
    static let fileName = "report.json"
    static let previousFileName = "previous.json"

    /// `~/Library/Logs/Clawdline/diagnostics/`.
    ///
    /// Beside `Clawdline.log`, which is where `docs/remote.md` already sends somebody who is asking
    /// what this app did. Deliberately **not** `~/.config/clawdline/`: that directory is settings
    /// and secrets — `remote.json` at mode 0600, the orchestrator token — and a report is neither.
    /// Mixing an inbox into a configuration store makes "what I set" and "what happened" the same
    /// place, and Logs is the macOS convention for the second one.
    static func directory(root: URL? = nil) -> URL {
        let home = root ?? FileManager.default.homeDirectoryForCurrentUser
        return home.appendingPathComponent("Library/Logs/Clawdline/diagnostics", isDirectory: true)
    }

    static func reportURL(root: URL? = nil) -> URL {
        directory(root: root).appendingPathComponent(fileName)
    }

    static func previousURL(root: URL? = nil) -> URL {
        directory(root: root).appendingPathComponent(previousFileName)
    }

    /// Why a report was not written, in the words the route answers with.
    ///
    /// Typed rather than a truncation, because the failure this whole feature is a repair for was a
    /// reading that looked like a reading and was not one. A report cut to fit would be exactly
    /// that again, one layer down.
    enum Refusal: Error, Equatable {
        case empty
        case tooLarge(bytes: Int, limit: Int)
        case notAnObject
        case writeFailed(String)

        var code: String {
            switch self {
            case .empty: return "empty_report"
            case .tooLarge: return "report_too_large"
            case .notAnObject: return "report_not_json"
            case .writeFailed: return "report_write_failed"
            }
        }

        var status: Int {
            switch self {
            case .empty, .notAnObject: return 400
            case .tooLarge: return 413
            case .writeFailed: return 500
            }
        }

        var message: String {
            switch self {
            case .empty:
                return "That request carried no report."
            case .tooLarge(let bytes, let limit):
                return "That report was \(bytes) bytes and the limit is \(limit). "
                    + "Press Clear saved in the panel and reproduce the fault again."
            case .notAnObject:
                return "A diagnostic report must be a JSON object."
            case .writeFailed(let why):
                return "The report could not be written: \(why)"
            }
        }
    }

    /// What was written, and where — read back out of the URL that was actually written to.
    ///
    /// `bytes` here is the **file's** size. The envelope inside it carries `received_bytes`, which
    /// is the body that arrived; the two differ by the envelope, and calling both of them `bytes`
    /// is the kind of overloaded field this whole feature exists to stop producing.
    struct Receipt: Equatable {
        let path: String
        let previousPath: String
        let bytes: Int
        let completenessStated: Bool

        var payload: [String: Any] {
            ["ok": true, "path": path, "previous": previousPath, "bytes": bytes,
             "limit": DiagnosticReport.maxBytes, "completeness_stated": completenessStated]
        }
    }

    /// The one write. Refuses before it touches the disk, rotates, then writes.
    static func save(_ body: Data, device: String, now: Date = Date(),
                     root: URL? = nil) -> Result<Receipt, Refusal> {
        guard !body.isEmpty else { return .failure(.empty) }
        guard body.count <= maxBytes else {
            return .failure(.tooLarge(bytes: body.count, limit: maxBytes))
        }
        guard let parsed = try? JSONSerialization.jsonObject(with: body),
              let report = parsed as? [String: Any] else {
            return .failure(.notAnObject)
        }

        // Hoisted rather than left for a reader to find. The page states its own completeness —
        // how many trace entries it dropped — and the one question somebody opening this file
        // asks first is whether it says so at all. An absent block and a block saying "nothing
        // was dropped" are different readings and must not render the same.
        var completeness: Any = ["stated": false,
                                 "why": "the page sent no completeness block with this report"]
        var stated = false
        if let said = report["completeness"] as? [String: Any] {
            completeness = said
            stated = true
        }

        let stamp = ISO8601DateFormatter()
        stamp.timeZone = TimeZone(secondsFromGMT: 0)
        let envelope: [String: Any] = [
            "clawdline_diagnostic_report": 1,
            "written_at": stamp.string(from: now),
            "written_by": device,
            "received_bytes": body.count,
            "completeness": completeness,
            "report": report,
        ]
        guard let encoded = try? JSONSerialization.data(
            withJSONObject: envelope, options: [.prettyPrinted, .sortedKeys]) else {
            return .failure(.writeFailed("the envelope could not be serialised"))
        }

        let manager = FileManager.default
        let folder = directory(root: root)
        let current = reportURL(root: root)
        let previous = previousURL(root: root)
        do {
            try manager.createDirectory(at: folder, withIntermediateDirectories: true)
            // Rotate first, so the press that has just happened cannot destroy the press somebody
            // was about to read. Failing to rotate is not a reason to lose the new report.
            if manager.fileExists(atPath: current.path) {
                try? manager.removeItem(at: previous)
                try? manager.moveItem(at: current, to: previous)
            }
            try encoded.write(to: current, options: .atomic)
            // Every time, not only at creation: an atomic write replaces the file and the
            // replacement does not inherit the old one's mode. Session ids and project paths are
            // in here, so it is 0600 like the pairing store.
            try? manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: current.path)
        } catch {
            return .failure(.writeFailed(error.localizedDescription))
        }

        // The path is the URL that was written to, and the bytes are what is on the disk now.
        // Naming a path this function did not write is the failure a person cannot see: the reply
        // says success, the file it names is stale or absent, and the reading is silently lost.
        let onDisk = (try? Data(contentsOf: current))?.count ?? 0
        guard onDisk == encoded.count else {
            return .failure(.writeFailed("wrote \(encoded.count) bytes and \(onDisk) came back"))
        }
        return .success(Receipt(
            path: current.path,
            previousPath: manager.fileExists(atPath: previous.path) ? previous.path : "",
            bytes: onDisk,
            completenessStated: stated))
    }
}
