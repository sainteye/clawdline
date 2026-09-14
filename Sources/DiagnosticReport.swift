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

/// What a paired browser saw go wrong on its own Cloud receive path, one line per batch, in a file
/// with a name anybody can be told.
///
/// **Why this exists.** The hosted console on a phone intermittently raised "This browser cannot
/// decrypt Sessions", and nothing on either end said why: the page threw the original exception
/// away and the Mac never sees a viewer's receive failures at all. `Resources/web/app/js/net/
/// cloud-viewer-events.js` now keeps a row for each one and sends batches here as the Cloud command
/// `diagnostics.events` without anybody pressing anything; this is where they land.
///
/// **Beside `report.json`, never in it.** That file is the person's press of Send to Mac and is
/// replaced by the next press. This one is appended by the page on its own, so it is a separate
/// name and it never touches `report.json` or `previous.json`.
///
/// **It knows no event names.** A row is `{n, at_ms, event, data}` and `data` is one flat object of
/// scalars and short lists. The shape is checked strictly and the contents are data: the next
/// defect adds an event name in the page and changes nothing here.
///
/// **Bounded twice.** A batch over `maxBatchBytes` is refused by name before anything is parsed, and
/// the file rotates to `cloud-viewer-events.1.jsonl` before a line would take it past
/// `maxFileBytes`, so the pair never holds more than twice that.
enum CloudViewerEventLog {
    static let fileName = "cloud-viewer-events.jsonl"
    static let rotatedFileName = "cloud-viewer-events.1.jsonl"
    /// The page seals at most 100 rows of 1.5 KiB each; this leaves room and no more.
    static let maxBatchBytes = 256 * 1024
    static let maxRows = 200
    static let maxFileBytes = 4 * 1024 * 1024

    static func fileURL(root: URL? = nil) -> URL {
        DiagnosticReport.directory(root: root).appendingPathComponent(fileName)
    }

    static func rotatedURL(root: URL? = nil) -> URL {
        DiagnosticReport.directory(root: root).appendingPathComponent(rotatedFileName)
    }

    enum Refusal: Error, Equatable {
        case empty
        case tooLarge(bytes: Int, limit: Int)
        /// Where the shape is wrong, as a path such as `rows[3].data.foo` — never the value there.
        case malformed(String)
        case writeFailed(String)

        var code: String {
            switch self {
            case .empty: return "viewer_events_empty"
            case .tooLarge: return "viewer_events_too_large"
            case .malformed: return "viewer_events_malformed"
            case .writeFailed: return "viewer_events_write_failed"
            }
        }

        var status: Int {
            switch self {
            case .empty, .malformed: return 400
            case .tooLarge: return 413
            case .writeFailed: return 500
            }
        }

        var message: String {
            switch self {
            case .empty: return "That request carried no viewer events."
            case .tooLarge(let bytes, let limit):
                return "That batch was \(bytes) bytes and the limit is \(limit)."
            case .malformed(let path): return "That batch is not viewer events v1 at \(path)."
            case .writeFailed(let why): return "The viewer events could not be written: \(why)"
            }
        }
    }

    /// What was appended, read back from the file that was written: the page removes its rows only
    /// when `batchID` and `rows` here match the batch it sent.
    struct Receipt: Equatable {
        let batchID: String
        let rows: Int
        let droppedRateLimited: Int
        let droppedOverflow: Int
        let storageErrors: Int
        let consistent: Bool
        let path: String
        let lineBytes: Int
        let fileBytes: Int
        let rotated: Bool

        var payload: [String: Any] {
            ["ok": true, "batch_id": batchID, "rows": rows, "path": path,
             "line_bytes": lineBytes, "file_bytes": fileBytes, "rotated": rotated]
        }

        /// The one `Clawdline.log` line: counts, and nothing a row said.
        var logLine: String {
            "cloud viewer events: appended rows=\(rows) dropped_rate_limited=\(droppedRateLimited) "
                + "dropped_overflow=\(droppedOverflow) storage_errors=\(storageErrors) "
                + "consistent=\(consistent ? 1 : 0) line_bytes=\(lineBytes) "
                + "file_bytes=\(fileBytes) rotated=\(rotated ? 1 : 0)"
        }
    }

    private static let lock = NSLock()
    private static let eventName = try! NSRegularExpression(
        pattern: #"^[a-z][a-z0-9_]*(\.[a-z0-9_]+){1,4}$"#)
    private static let fieldName = try! NSRegularExpression(pattern: #"^[a-z][a-z0-9_]{0,63}$"#)

    /// Refuses before it touches the disk, rotates if the line would not fit, appends, syncs.
    static func append(_ body: Data, device: String, now: Date = Date(), root: URL? = nil,
                       maxFileBytes fileLimit: Int = maxFileBytes) -> Result<Receipt, Refusal> {
        guard !body.isEmpty else { return .failure(.empty) }
        guard body.count <= maxBatchBytes else {
            return .failure(.tooLarge(bytes: body.count, limit: maxBatchBytes))
        }
        guard let batch = (try? JSONSerialization.jsonObject(with: body)) as? [String: Any] else {
            return .failure(.malformed("batch"))
        }
        if let problem = problem(in: batch) { return .failure(.malformed(problem)) }

        let rows = batch["rows"] as? [[String: Any]] ?? []
        let completeness = batch["completeness"] as? [String: Any] ?? [:]
        let count = { (key: String) in integer(completeness[key]) ?? 0 }
        // Rows and overflowed rows each consumed one `n`; rate-limited rows consumed none. When
        // that arithmetic holds the batch says exactly what it lost, and the line records whether.
        let consistent = count("rows") == rows.count
            && rows.count + count("dropped_overflow") == count("n_to") - count("n_from") + 1
        let milliseconds = Int((now.timeIntervalSince1970 * 1_000).rounded(.down))
        let stamp = ISO8601DateFormatter()
        stamp.timeZone = TimeZone(secondsFromGMT: 0)
        let line: [String: Any] = [
            "clawdline_viewer_events": 1,
            "written_at": stamp.string(from: now),
            "written_at_ms": milliseconds,
            // The envelope's authenticated sender; `claimed_device` is only what the page wrote.
            "device": device,
            "claimed_device": batch["device"] ?? NSNull(),
            "tab": batch["tab"] ?? NSNull(),
            "web_build": batch["web_build"] ?? NSNull(),
            "batch_id": batch["batch_id"] ?? NSNull(),
            "created_at_ms": batch["created_at_ms"] ?? NSNull(),
            "received_bytes": body.count,
            "row_count": rows.count,
            "completeness_consistent": consistent,
            "completeness": completeness,
            "rows": rows,
        ]
        guard var encoded = try? JSONSerialization.data(
            withJSONObject: line, options: [.sortedKeys, .withoutEscapingSlashes]) else {
            return .failure(.writeFailed("the line could not be serialised"))
        }
        encoded.append(0x0A)

        lock.lock()
        defer { lock.unlock() }
        let manager = FileManager.default
        let file = fileURL(root: root)
        let rotatedFile = rotatedURL(root: root)
        var rotated = false
        let fileBytes: Int
        do {
            try manager.createDirectory(at: DiagnosticReport.directory(root: root),
                                        withIntermediateDirectories: true)
            let existing = (try? manager.attributesOfItem(atPath: file.path)[.size] as? NSNumber)?
                .intValue ?? 0
            if existing > 0 && existing + encoded.count > fileLimit {
                try? manager.removeItem(at: rotatedFile)
                try manager.moveItem(at: file, to: rotatedFile)
                rotated = true
            }
            let before = rotated ? 0 : existing
            if !manager.fileExists(atPath: file.path) {
                guard manager.createFile(atPath: file.path, contents: nil,
                                         attributes: [.posixPermissions: 0o600]) else {
                    return .failure(.writeFailed("the file could not be created"))
                }
            }
            let handle = try FileHandle(forWritingTo: file)
            defer { try? handle.close() }
            _ = try handle.seekToEnd()
            try handle.write(contentsOf: encoded)
            try handle.synchronize()
            fileBytes = Int(try handle.seekToEnd())
            // The receipt names bytes that are on the disk now, or it is not a receipt.
            guard fileBytes >= before + encoded.count else {
                return .failure(.writeFailed(
                    "appended \(encoded.count) bytes and the file is \(fileBytes)"))
            }
            try? manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        } catch {
            return .failure(.writeFailed(error.localizedDescription))
        }
        return .success(Receipt(
            batchID: batch["batch_id"] as? String ?? "", rows: rows.count,
            droppedRateLimited: count("dropped_rate_limited"),
            droppedOverflow: count("dropped_overflow"), storageErrors: count("storage_errors"),
            consistent: consistent, path: file.path, lineBytes: encoded.count,
            fileBytes: fileBytes, rotated: rotated))
    }

    /// The first place the batch is not viewer events v1, or nil. Exact key sets, like every other
    /// Cloud command body, so a field added on one end is a refusal rather than a silent drop.
    static func problem(in batch: [String: Any]) -> String? {
        guard Set(batch.keys) == ["v", "batch_id", "created_at_ms", "device", "tab", "web_build",
                                  "rows", "completeness"] else { return "batch.keys" }
        guard integer(batch["v"]) == 1 else { return "v" }
        guard let batchID = batch["batch_id"] as? String, token(batchID, 64) else { return "batch_id" }
        guard let created = integer(batch["created_at_ms"]), created >= 0 else { return "created_at_ms" }
        for key in ["device", "tab", "web_build"] {
            guard batch[key] is NSNull || (batch[key] as? String).map({ $0.count <= 128 }) == true
            else { return key }
        }
        guard let rows = batch["rows"] as? [Any], rows.count <= maxRows else { return "rows" }
        for (index, value) in rows.enumerated() {
            if let problem = rowProblem(value) { return "rows[\(index)]" + problem }
        }
        guard let completeness = batch["completeness"] as? [String: Any] else { return "completeness" }
        return completenessProblem(completeness).map { "completeness" + $0 }
    }

    private static func rowProblem(_ value: Any) -> String? {
        guard let row = value as? [String: Any], Set(row.keys) == ["n", "at_ms", "event", "data"]
        else { return ".keys" }
        guard let n = integer(row["n"]), n >= 1 else { return ".n" }
        guard let at = integer(row["at_ms"]), at >= 0 else { return ".at_ms" }
        guard let event = row["event"] as? String, event.utf8.count <= 96,
              matches(eventName, event) else { return ".event" }
        guard let data = row["data"] as? [String: Any], data.count <= 48 else { return ".data" }
        for key in data.keys.sorted() {
            guard matches(fieldName, key) else { return ".data.keys" }
            if !scalar(data[key] as Any) { return ".data." + key }
        }
        return nil
    }

    private static func completenessProblem(_ value: [String: Any]) -> String? {
        guard Set(value.keys) == ["n_from", "n_to", "rows", "dropped_rate_limited",
                                  "dropped_overflow", "storage_errors", "counting_since_ms",
                                  "rate_limited", "limits"] else { return ".keys" }
        for key in ["n_from", "n_to", "rows", "dropped_rate_limited", "dropped_overflow",
                    "storage_errors"] {
            guard let number = integer(value[key]), number >= 0 else { return "." + key }
        }
        guard value["counting_since_ms"] is NSNull || integer(value["counting_since_ms"]) != nil
        else { return ".counting_since_ms" }
        guard let limits = value["limits"] as? [String: Any], limits.count <= 8,
              limits.allSatisfy({ matches(fieldName, $0.key) && integer($0.value) != nil })
        else { return ".limits" }
        guard let table = value["rate_limited"] as? [Any], table.count <= 32 else {
            return ".rate_limited"
        }
        for (index, item) in table.enumerated() {
            guard let entry = item as? [String: Any],
                  Set(entry.keys) == ["key", "event", "dropped", "first_at_ms", "last_at_ms",
                                      "sample_n"],
                  (entry["key"] as? String).map({ $0.count <= 256 }) == true,
                  entry["event"] is NSNull || (entry["event"] as? String).map({ $0.count <= 96 }) == true,
                  integer(entry["dropped"]) != nil, integer(entry["first_at_ms"]) != nil,
                  integer(entry["last_at_ms"]) != nil,
                  entry["sample_n"] is NSNull || integer(entry["sample_n"]) != nil
            else { return ".rate_limited[\(index)]" }
        }
        return nil
    }

    /// `null`, a boolean, a finite number, a string of at most 512 characters, or a list of at most
    /// sixteen strings (128 characters) and numbers. Nothing nests.
    private static func scalar(_ value: Any) -> Bool {
        if value is NSNull { return true }
        if let number = value as? NSNumber {
            return CFGetTypeID(number) == CFBooleanGetTypeID() || number.doubleValue.isFinite
        }
        if let text = value as? String { return text.count <= 512 }
        if let list = value as? [Any] {
            return list.count <= 16 && list.allSatisfy { item in
                if let text = item as? String { return text.count <= 128 }
                if let number = item as? NSNumber {
                    return CFGetTypeID(number) != CFBooleanGetTypeID() && number.doubleValue.isFinite
                }
                return false
            }
        }
        return false
    }

    private static func integer(_ value: Any?) -> Int? {
        guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(),
              !["f", "d"].contains(String(cString: number.objCType)) else { return nil }
        return Int(exactly: number.int64Value)
    }

    private static func token(_ value: String, _ limit: Int) -> Bool {
        !value.isEmpty && value.utf8.count <= limit
            && value.unicodeScalars.allSatisfy { $0.value > 0x20 && $0.value < 0x7f }
    }

    private static func matches(_ expression: NSRegularExpression, _ value: String) -> Bool {
        let range = NSRange(value.startIndex..<value.endIndex, in: value)
        return expression.firstMatch(in: value, range: range) != nil
    }
}
