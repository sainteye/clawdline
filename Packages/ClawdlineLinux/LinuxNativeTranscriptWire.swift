import Foundation
import ClawdlineApplication

// Pure semantic transcript parsing for Linux Sessions.
//
// This file owns one question: given bytes that another layer has already proven belong to one
// provider conversation in one terminal incarnation, which Web transcript rows do they say, and
// which opaque signature names that answer? It opens no file, reads no procfs entry and knows no
// route. `LinuxNativeTranscriptReader` (identity slice) supplies the bytes and scalar evidence;
// the integration slice publishes `LinuxNativeTranscriptWire.webBody`.
//
// The row rules are ports of the Mac readers (`Sources/Transcript.swift` for Claude and
// `Sources/Codex.swift` for Codex) so that a Linux Session and a Mac Session with the same
// provider bytes produce the same rows. The keys are exactly `RemoteServer.transcriptRows`'.
// Three Mac inputs have no Linux owner yet and therefore never produce a row here rather than a
// guessed one: Clawdline notice envelopes, Clawdline session-message envelopes and owned image
// artifacts. The DTO still carries `notice`, `artifacts` and `sourceAssistant` so the serializer
// is the single owner of those keys once a Linux decoder exists.

/// A JSON value whose shape was decided before it reached the transcript DTO. Used only for the
/// wire fields whose Mac producers emit structured objects (`notice`, `artifacts`).
indirect enum LinuxTranscriptJSONValue: Equatable, Sendable {
    case string(String)
    case integer(Int)
    case bool(Bool)
    case array([LinuxTranscriptJSONValue])
    case object([String: LinuxTranscriptJSONValue])
    case null

    var foundationValue: Any {
        switch self {
        case .string(let value): return value
        case .integer(let value): return value
        case .bool(let value): return value
        case .array(let values): return values.map { $0.foundationValue }
        case .object(let values): return values.mapValues { $0.foundationValue }
        case .null: return NSNull()
        }
    }
}

struct LinuxTranscriptFileChange: Equatable, Sendable {
    var path: String
    var kind: String
    var unifiedDiff: String? = nil
    var content: String? = nil
    var movePath: String? = nil
}

struct LinuxTranscriptPlanStep: Equatable, Sendable {
    var step: String
    var status: String
}

struct LinuxTranscriptToolAction: Equatable, Sendable {
    var kind: String
    var command: String? = nil
    var name: String? = nil
    var path: String? = nil
    var query: String? = nil
}

struct LinuxTranscriptActivity: Equatable, Sendable {
    var kind: String
    var title: String? = nil
    var status: String? = nil
    var durationMilliseconds: Int? = nil
    var result: String? = nil
    var actions: [LinuxTranscriptToolAction] = []
}

/// Which Claude conversation the already-authenticated bytes represent. Claude stores root and
/// agent turns in the same JSONL vocabulary, so this is identity evidence rather than a display
/// preference. Codex currently accepts only `.root` until its deployed writer is measured.
enum LinuxNativeTranscriptConversationMode: String, Equatable, Sendable {
    case root
    case claudeAgent = "claude_agent"
}

/// One semantic transcript row. `kind` mirrors the Mac `Transcript.Entry.Kind`; a call and its
/// result stay distinct internally even though both are published with role `tool`.
// Not `Sendable`: `Assistant` is declared in another module without that conformance.
struct LinuxTranscriptEntry: Equatable {
    enum Kind: Equatable, Sendable {
        case user
        case assistant
        case peer
        case message
        case notice
        case tool
        case toolResult

        var wireRole: String {
            switch self {
            case .user: return "user"
            case .assistant: return "assistant"
            case .peer: return "peer"
            case .message: return "message"
            case .notice: return "notice"
            case .tool, .toolResult: return "tool"
            }
        }
    }

    var kind: Kind
    var text: String
    var tool: String? = nil
    /// Whole Unix seconds, truncated exactly as `Int(Date.timeIntervalSince1970)` is on the Mac.
    var at: Int? = nil
    var imageCount = 0
    var source: String? = nil
    var sourceMode: String? = nil
    var sourceAssistant: Assistant? = nil
    var artifacts: [[String: LinuxTranscriptJSONValue]] = []
    var notice: [String: LinuxTranscriptJSONValue]? = nil
    var fileChanges: [LinuxTranscriptFileChange] = []
    var plan: [LinuxTranscriptPlanStep] = []
    var activity: LinuxTranscriptActivity? = nil
    /// Internal Claude delivery receipt identity; never published.
    var peerMessageID: String? = nil
    var isPeerDelivery = false
}

/// Scalar evidence the identity layer proved about the bytes it hands over. Every field is part of
/// the signature; none of them is published.
struct LinuxNativeTranscriptEvidence: Equatable {
    let assistant: Assistant
    let conversationMode: LinuxNativeTranscriptConversationMode
    /// Claude registry `sessionId` or Codex `session_meta.payload.session_id`.
    let providerConversationID: String
    /// The SHA-256 hex terminal incarnation of the foreground provider process.
    let terminalIncarnation: String
    let fileDevice: UInt64
    let fileInode: UInt64
    /// Post-read `st_size`; the bounded bytes are exactly `[suffixOffset, fileSize)`.
    let fileSize: UInt64
    /// Post-read `st_mtim` in nanoseconds since the Unix epoch.
    let fileModifiedNanoseconds: Int64
    let suffixOffset: UInt64
}

struct LinuxNativeTranscriptParse: Equatable {
    let entries: [LinuxTranscriptEntry]
    let signature: String
    let providerConversationID: String
    /// Number of JSON rows decoded, including an offset-zero Codex identity head. Kept off the
    /// Web wire; tests use it to pin the reverse-parser's bounded-work contract.
    let decodedRowCount: Int
}

enum LinuxNativeTranscriptWireError: Error, Equatable, Sendable {
    case invalidConversationID
    case invalidTerminalIncarnation
    case invalidConversationMode
    /// The evidence cannot describe these bytes (offset + length is not the post-read size).
    case evidenceInconsistent
    /// More than `maximumSuffixBytes`, or a partial suffix with no row boundary at all.
    case suffixLimitExceeded
    /// A Codex rollout head that does not name exactly this user conversation.
    case conversationMismatch
    /// Too many physical JSONL rows were inspected without satisfying the semantic entry limit.
    case rowWorkLimitExceeded
    /// The bounded parser exhausted its monotonic deadline.
    case parseDeadlineExceeded

    /// The external error code the plan's typed fail-closed contract assigns.
    var externalCode: String {
        switch self {
        case .suffixLimitExceeded, .rowWorkLimitExceeded: return "transcript_limit_exceeded"
        case .parseDeadlineExceeded: return "transcript_busy"
        case .invalidConversationID, .invalidTerminalIncarnation, .invalidConversationMode,
             .evidenceInconsistent,
             .conversationMismatch:
            return "transcript_identity_unknown"
        }
    }
}

enum LinuxNativeTranscriptWire {
    /// The Mac route's transcript tail (`RemoteServer` 8 MiB).
    static let maximumSuffixBytes = 8 << 20
    /// The direct route's clamp for a caller-supplied limit.
    static let entryLimitRange = 1...1_000
    /// A transcript containing only nonsemantic rows cannot make parsing proportional to the
    /// number of rows in an 8 MiB suffix. This is deliberately independent of the entry limit.
    static let maximumDecodedRows = 4_096
    static let parseDeadlineSeconds: TimeInterval = 5
    static let signatureVersion = "clawdline-linux-native-transcript-v2"
    /// Every key `RemoteServer.transcriptRows` may emit, and nothing else.
    static let webRowKeys: Set<String> = [
        "role", "text", "tool", "at", "imageCount", "source", "sourceMode", "sourceAssistant",
        "artifacts", "notice", "fileChanges", "plan", "activity"
    ]
    /// Bytes handed to MCP result detail before truncation, as on the Mac.
    static let mcpResultDetailLimit = 4_000

    // MARK: - Entry point

    static func parse(bytes: Data, evidence: LinuxNativeTranscriptEvidence,
                      limit: Int,
                      maximumDecodedRows: Int = LinuxNativeTranscriptWire.maximumDecodedRows,
                      monotonicNow: @escaping () -> TimeInterval = {
                          ProcessInfo.processInfo.systemUptime
                      }, deadline: TimeInterval? = nil) throws -> LinuxNativeTranscriptParse {
        guard isCanonicalUUID(evidence.providerConversationID) else {
            throw LinuxNativeTranscriptWireError.invalidConversationID
        }
        guard isSHA256Hex(evidence.terminalIncarnation) else {
            throw LinuxNativeTranscriptWireError.invalidTerminalIncarnation
        }
        if evidence.assistant == .codex, evidence.conversationMode != .root {
            throw LinuxNativeTranscriptWireError.invalidConversationMode
        }
        guard bytes.count <= maximumSuffixBytes else {
            throw LinuxNativeTranscriptWireError.suffixLimitExceeded
        }
        let end = evidence.suffixOffset.addingReportingOverflow(UInt64(bytes.count))
        guard !end.overflow, end.partialValue == evidence.fileSize,
              evidence.fileModifiedNanoseconds >= 0 else {
            throw LinuxNativeTranscriptWireError.evidenceInconsistent
        }

        var work = ParseWork(
            maximumRows: maximumDecodedRows,
            deadline: deadline ?? (monotonicNow() + parseDeadlineSeconds),
            monotonicNow: monotonicNow)
        var rowStart = bytes.startIndex
        if evidence.suffixOffset > 0 {
            guard let newline = try firstNewline(in: bytes, from: rowStart, work: &work) else {
                throw LinuxNativeTranscriptWireError.suffixLimitExceeded
            }
            rowStart = bytes.index(after: newline)
        }
        if evidence.assistant == .codex, evidence.suffixOffset == 0 {
            guard let newline = try firstNewline(in: bytes, from: rowStart, work: &work) else {
                throw LinuxNativeTranscriptWireError.conversationMismatch
            }
            let headRange = rowStart..<newline
            try work.consumeRow()
            guard !headRange.isEmpty,
                  let head = jsonRow(in: bytes, range: headRange) else {
                throw LinuxNativeTranscriptWireError.conversationMismatch
            }
            try requireCodexHead(head, conversationID: evidence.providerConversationID)
            rowStart = bytes.index(after: newline)
        }
        var rows = ReverseCompleteRows(bytes: bytes, lowerBound: rowStart)
        let bounded = min(max(limit, entryLimitRange.lowerBound), entryLimitRange.upperBound)
        let parsed: (entries: [LinuxTranscriptEntry], decodedRows: Int)
        switch evidence.assistant {
        case .claude:
            parsed = try claudeEntries(bytes: bytes, rows: &rows, work: &work,
                                       mode: evidence.conversationMode, limit: bounded)
        case .codex:
            parsed = try codexEntries(bytes: bytes, rows: &rows, work: &work, limit: bounded)
        }
        try work.checkDeadline()
        let digest = signature(bytes: bytes, evidence: evidence)
        try work.checkDeadline()
        return LinuxNativeTranscriptParse(
            entries: parsed.entries, signature: digest,
            providerConversationID: evidence.providerConversationID,
            decodedRowCount: work.decodedRows)
    }

    /// The versioned length-framed SHA-256 over the whole evidence tuple and the bounded bytes:
    /// each field is its unsigned eight-byte big-endian length followed by its bytes.
    static func signature(bytes: Data, evidence: LinuxNativeTranscriptEvidence) -> String {
        var stream = Data()
        func frame(_ field: Data) {
            var length = UInt64(field.count).bigEndian
            withUnsafeBytes(of: &length) { stream.append(contentsOf: $0) }
            stream.append(field)
        }
        frame(Data(signatureVersion.utf8))
        frame(Data(evidence.assistant.rawValue.utf8))
        frame(Data(evidence.conversationMode.rawValue.utf8))
        frame(Data(evidence.providerConversationID.utf8))
        frame(Data(evidence.terminalIncarnation.utf8))
        frame(Data(String(evidence.fileDevice).utf8))
        frame(Data(String(evidence.fileInode).utf8))
        frame(Data(String(evidence.fileSize).utf8))
        frame(Data(String(evidence.fileModifiedNanoseconds).utf8))
        frame(Data(String(evidence.suffixOffset).utf8))
        frame(bytes)
        return LinuxSHA256.hex(stream)
    }

    // MARK: - Serialization

    static func webBody(_ parse: LinuxNativeTranscriptParse) -> [String: Any] {
        ["entries": webRows(parse.entries), "signature": parse.signature]
    }

    /// `RemoteServer.transcriptRows`, key for key.
    static func webRows(_ entries: [LinuxTranscriptEntry]) -> [[String: Any]] {
        entries.map { entry -> [String: Any] in
            var row: [String: Any] = ["role": entry.kind.wireRole, "text": entry.text]
            if let tool = entry.tool { row["tool"] = tool }
            if let at = entry.at { row["at"] = at }
            if entry.kind == .user { row["imageCount"] = entry.imageCount }
            if let source = entry.source, !source.isEmpty { row["source"] = source }
            if let mode = entry.sourceMode, !mode.isEmpty { row["sourceMode"] = mode }
            if let assistant = entry.sourceAssistant { row["sourceAssistant"] = assistant.rawValue }
            if !entry.artifacts.isEmpty {
                row["artifacts"] = entry.artifacts.map { $0.mapValues { $0.foundationValue } }
            }
            if let notice = entry.notice { row["notice"] = notice.mapValues { $0.foundationValue } }
            if !entry.fileChanges.isEmpty {
                row["fileChanges"] = entry.fileChanges.map { change -> [String: Any] in
                    var out: [String: Any] = ["path": change.path, "kind": change.kind]
                    if let diff = change.unifiedDiff { out["unifiedDiff"] = diff }
                    if let content = change.content { out["content"] = content }
                    if let destination = change.movePath { out["movePath"] = destination }
                    return out
                }
            }
            if !entry.plan.isEmpty {
                row["plan"] = entry.plan.map { ["step": $0.step, "status": $0.status] }
            }
            if let activity = entry.activity {
                var out: [String: Any] = ["kind": activity.kind]
                if let title = activity.title { out["title"] = title }
                if let status = activity.status { out["status"] = status }
                if let duration = activity.durationMilliseconds { out["durationMs"] = duration }
                if let result = activity.result { out["result"] = result }
                out["actions"] = activity.actions.map { action -> [String: Any] in
                    var value: [String: Any] = ["kind": action.kind]
                    if let command = action.command { value["command"] = command }
                    if let name = action.name { value["name"] = name }
                    if let path = action.path { value["path"] = path }
                    if let query = action.query { value["query"] = query }
                    return value
                }
                row["activity"] = out
            }
            return row
        }
    }

    // MARK: - Bounded rows

    private struct ParseWork {
        let maximumRows: Int
        let deadline: TimeInterval
        let monotonicNow: () -> TimeInterval
        var decodedRows = 0
        private var inspectedBytes = 0

        init(maximumRows: Int, deadline: TimeInterval,
             monotonicNow: @escaping () -> TimeInterval) {
            self.maximumRows = maximumRows
            self.deadline = deadline
            self.monotonicNow = monotonicNow
        }

        mutating func inspectByte() throws {
            inspectedBytes += 1
            if inspectedBytes == 1 || inspectedBytes & 0x3ff == 0 {
                try checkDeadline()
            }
        }

        mutating func consumeRow() throws {
            guard decodedRows < maximumRows else {
                throw LinuxNativeTranscriptWireError.rowWorkLimitExceeded
            }
            decodedRows += 1
            try checkDeadline()
        }

        func checkDeadline() throws {
            guard monotonicNow() <= deadline else {
                throw LinuxNativeTranscriptWireError.parseDeadlineExceeded
            }
        }
    }

    /// Reverse iterator over committed physical JSONL rows. It retains only two indices, so an
    /// 8 MiB suffix containing millions of tiny rows cannot first allocate millions of ranges.
    /// Empty physical rows are returned too and consume work before being ignored by JSON decode.
    private struct ReverseCompleteRows {
        let bytes: Data
        let lowerBound: Data.Index
        var cursor: Data.Index
        var rowEnd: Data.Index?
        var finished = false

        init(bytes: Data, lowerBound: Data.Index) {
            self.bytes = bytes
            self.lowerBound = lowerBound
            self.cursor = bytes.endIndex
        }

        mutating func next(work: inout ParseWork) throws -> Range<Data.Index>? {
            guard !finished else { return nil }
            while cursor > lowerBound {
                cursor = bytes.index(before: cursor)
                try work.inspectByte()
                guard bytes[cursor] == 0x0A else { continue }
                if let end = rowEnd {
                    let range = bytes.index(after: cursor)..<end
                    rowEnd = cursor
                    return range
                }
                rowEnd = cursor
            }
            finished = true
            guard let end = rowEnd else { return nil }
            return lowerBound..<end
        }
    }

    private static func firstNewline(in bytes: Data, from start: Data.Index,
                                     work: inout ParseWork) throws -> Data.Index? {
        var index = start
        while index < bytes.endIndex {
            try work.inspectByte()
            if bytes[index] == 0x0A { return index }
            index = bytes.index(after: index)
        }
        return nil
    }

    static func jsonRow(in bytes: Data, range: Range<Data.Index>) -> [String: Any]? {
        try? JSONSerialization.jsonObject(with: bytes.subdata(in: range)) as? [String: Any]
    }

    // MARK: - Evidence shape

    static func isCanonicalUUID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        guard bytes.count == 36 else { return false }
        for (offset, byte) in bytes.enumerated() {
            if offset == 8 || offset == 13 || offset == 18 || offset == 23 {
                guard byte == 0x2D else { return false }
            } else {
                guard (byte >= 0x30 && byte <= 0x39) || (byte >= 0x61 && byte <= 0x66) else {
                    return false
                }
            }
        }
        return true
    }

    static func isSHA256Hex(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return bytes.count == 64
            && bytes.allSatisfy { ($0 >= 0x30 && $0 <= 0x39) || ($0 >= 0x61 && $0 <= 0x66) }
    }

    /// One exact accepted head schema: `session_meta` with `payload.session_id` equal to the
    /// evidence and `payload.thread_source == "user"`. `payload.id` alone is not accepted.
    static func requireCodexHead(_ row: [String: Any], conversationID: String) throws {
        guard row["type"] as? String == "session_meta",
              let payload = row["payload"] as? [String: Any],
              payload["session_id"] as? String == conversationID,
              payload["thread_source"] as? String == "user" else {
            throw LinuxNativeTranscriptWireError.conversationMismatch
        }
    }

    // MARK: - Shared text rules

    static func timestamp(_ raw: Any?, formatter: ISO8601DateFormatter) -> Int? {
        guard let string = raw as? String, let date = formatter.date(from: string) else { return nil }
        return Int(date.timeIntervalSince1970)
    }

    static func makeTimestampFormatter() -> ISO8601DateFormatter {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }

    static func clean(_ value: Any?) -> String {
        (value as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Claude's canonical image content without the Mac drop cache, which no Linux provider sees.
    static func canonicalImageContent(_ raw: String, structuredImages: Int = 0,
                                      inferMarkers: Bool = false) -> (text: String, imageCount: Int) {
        var value = raw
        var markerImages = 0
        if structuredImages > 0 || inferMarkers,
           let markers = try? NSRegularExpression(pattern: #"\[Image #\d+\]\s*"#) {
            let range = NSRange(value.startIndex..<value.endIndex, in: value)
            if inferMarkers { markerImages = markers.numberOfMatches(in: value, range: range) }
            value = markers.stringByReplacingMatches(in: value, range: range, withTemplate: "")
        }
        let text = value.trimmingCharacters(in: .whitespacesAndNewlines)
        let count = structuredImages + markerImages
        if text.isEmpty, count > 0 {
            return ((1...count).map { "[Image #\($0)]" }.joined(separator: " "), count)
        }
        return (text, count)
    }

    /// The Mac `Transcript.assistantEntry` with no Linux image store: no marker is honoured, so
    /// every marker remains literal prose rather than disappearing.
    static func assistantEntry(text raw: String, at: Int?) -> LinuxTranscriptEntry? {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        return LinuxTranscriptEntry(kind: .assistant, text: text, at: at)
    }

    /// Terminal escape sequences (CSI, OSC and two-byte escapes) removed from tool output.
    static func plain(_ text: String) -> String {
        guard text.unicodeScalars.contains("\u{1B}") else { return text }
        var out = String.UnicodeScalarView()
        let scalars = Array(text.unicodeScalars)
        var index = 0
        while index < scalars.count {
            let scalar = scalars[index]
            guard scalar == "\u{1B}", index + 1 < scalars.count else {
                if scalar != "\u{1B}" { out.append(scalar) }
                index += 1
                continue
            }
            let next = scalars[index + 1]
            index += 2
            if next == "[" {
                while index < scalars.count {
                    let value = scalars[index].value
                    index += 1
                    if value >= 0x40 && value <= 0x7E { break }
                }
            } else if next == "]" {
                while index < scalars.count {
                    if scalars[index] == "\u{07}" { index += 1; break }
                    if scalars[index] == "\u{1B}", index + 1 < scalars.count,
                       scalars[index + 1] == "\\" { index += 2; break }
                    index += 1
                }
            }
        }
        return String(out)
    }

    static func firstLine(ofText text: String) -> String {
        let bytes = text.utf8
        var start = bytes.startIndex
        while start < bytes.endIndex {
            let end = bytes[start...].firstIndex(of: UInt8(ascii: "\n")) ?? bytes.endIndex
            if start < end { return String(decoding: bytes[start..<end], as: UTF8.self) }
            guard end < bytes.endIndex else { break }
            start = bytes.index(after: end)
        }
        return ""
    }

    // MARK: - Claude

    static let claudeAskTool = "AskUserQuestion"
    static let claudeAskMarker = "\u{1}ask\u{1}"

    private static func claudeEntries(bytes: Data, rows: inout ReverseCompleteRows,
                                      work: inout ParseWork,
                                      mode: LinuxNativeTranscriptConversationMode,
                                      limit: Int) throws
        -> (entries: [LinuxTranscriptEntry], decodedRows: Int) {
        let formatter = makeTimestampFormatter()
        var newestFirst: [LinuxTranscriptEntry] = []
        var deliveredPeerIDs = Set<String>()
        var deliveredPeerKeys = Set<String>()
        var queuedPeerKeys = Set<String>()
        let initialRows = work.decodedRows
        while let range = try rows.next(work: &work) {
            try work.consumeRow()
            guard !range.isEmpty else { continue }
            guard let row = jsonRow(in: bytes, range: range) else { continue }
            for entry in claudeEntries(inRow: row, mode: mode,
                                       formatter: formatter).reversed() {
                if entry.kind == .peer {
                    let key = (entry.source ?? "") + "\u{0}" + entry.text
                    if entry.isPeerDelivery {
                        if let id = entry.peerMessageID {
                            let receipt = (entry.source ?? "") + "\u{0}" + id
                            if deliveredPeerIDs.contains(receipt) { continue }
                            deliveredPeerIDs.insert(receipt)
                        } else if deliveredPeerKeys.contains(key) {
                            continue
                        }
                        deliveredPeerKeys.insert(key)
                        let at = entry.at
                        newestFirst.removeAll { queued in
                            guard queued.kind == .peer, !queued.isPeerDelivery,
                                  (queued.source ?? "") + "\u{0}" + queued.text == key else {
                                return false
                            }
                            guard let queuedAt = queued.at, let at else { return true }
                            return queuedAt <= at
                        }
                        queuedPeerKeys.remove(key)
                    } else {
                        if deliveredPeerKeys.contains(key) || queuedPeerKeys.contains(key) {
                            continue
                        }
                        queuedPeerKeys.insert(key)
                    }
                }
                newestFirst.append(entry)
            }
            if newestFirst.count >= limit { break }
        }
        return (Array(newestFirst.reversed().suffix(limit)), work.decodedRows - initialRows)
    }

    static func claudeEntries(inRow row: [String: Any],
                              mode: LinuxNativeTranscriptConversationMode,
                              formatter: ISO8601DateFormatter) -> [LinuxTranscriptEntry] {
        let type = row["type"] as? String
        guard type == "user" || type == "assistant" || type == "queue-operation"
                || type == "system" else { return [] }
        // The identity layer chooses the conversation; a root must not absorb a child's rows, and
        // a child must not lose its conversation merely because those rows are sidechain-marked.
        let isSidechain = row["isSidechain"] as? Bool == true
        switch mode {
        case .root where isSidechain, .claudeAgent where !isSidechain: return []
        default: break
        }
        let at = timestamp(row["timestamp"], formatter: formatter)

        if let origin = row["origin"] as? [String: Any],
           origin["kind"] as? String == "peer",
           let body = origin["body"] as? String {
            let text = body.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else { return [] }
            let messageID = clean(origin["msg_id"])
            return [LinuxTranscriptEntry(kind: .peer, text: text, at: at,
                                         source: clean(origin["name"]),
                                         sourceMode: clean(origin["fromMode"]),
                                         peerMessageID: messageID.isEmpty ? nil : messageID,
                                         isPeerDelivery: true)]
        }
        if row["isMeta"] as? Bool == true { return [] }

        if type == "system" {
            guard let raw = row["content"] as? String,
                  raw.contains("<command-name>"), raw.contains("</command-name>"),
                  !raw.contains("<command-args>") || raw.contains("</command-args>"),
                  let typed = claudeSlashCommand(in: raw) else { return [] }
            return [LinuxTranscriptEntry(kind: .user, text: typed, at: at)]
        }

        if type == "queue-operation" {
            guard row["operation"] as? String == "enqueue",
                  let raw = row["content"] as? String else { return [] }
            if let peer = claudeCrossSessionMessage(in: raw, at: at) { return [peer] }
            let text = claudeWithoutMachineBlocks(raw).trimmingCharacters(in: .whitespacesAndNewlines)
            let canonical = canonicalImageContent(text, inferMarkers: true)
            guard !canonical.text.isEmpty else { return [] }
            return [LinuxTranscriptEntry(kind: .user, text: canonical.text, at: at,
                                         imageCount: canonical.imageCount)]
        }

        guard let message = row["message"] as? [String: Any] else { return [] }
        var blocks: [[String: Any]] = []
        if let list = message["content"] as? [[String: Any]] {
            blocks = list
        } else if let text = message["content"] as? String {
            blocks = [["type": "text", "text": text]]
        }

        if type == "user" {
            let images = blocks.filter { $0["type"] as? String == "image" }.count
            if images > 0 {
                let rawText = blocks.compactMap { block -> String? in
                    guard block["type"] as? String == "text" else { return nil }
                    return claudeWithoutMachineBlocks(block["text"] as? String ?? "")
                        .trimmingCharacters(in: .whitespacesAndNewlines)
                }.filter { !$0.isEmpty }.joined(separator: "\n")
                let canonical = canonicalImageContent(rawText, structuredImages: images)
                return [LinuxTranscriptEntry(kind: .user, text: canonical.text, at: at,
                                             imageCount: canonical.imageCount)]
            }
        }

        var out: [LinuxTranscriptEntry] = []
        for block in blocks {
            switch block["type"] as? String {
            case "text":
                let raw = block["text"] as? String ?? ""
                if type != "user" {
                    if let entry = assistantEntry(text: raw, at: at) { out.append(entry) }
                    continue
                }
                let typed = claudeSlashCommand(in: raw)
                var text = claudeWithoutMachineBlocks(raw)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                if let typed { text = text.isEmpty ? typed : typed + "\n" + text }
                if !text.isEmpty { out.append(LinuxTranscriptEntry(kind: .user, text: text, at: at)) }
                if let printed = claudeCommandOutput(in: raw) {
                    out.append(LinuxTranscriptEntry(kind: .toolResult, text: printed, at: at))
                }
            case "tool_use":
                let name = block["name"] as? String ?? "tool"
                if name == "Write",
                   let input = block["input"] as? [String: Any],
                   let path = input["file_path"] as? String, !path.isEmpty,
                   let content = input["content"] as? String {
                    out.append(LinuxTranscriptEntry(
                        kind: .tool, text: path, tool: name, at: at,
                        fileChanges: [LinuxTranscriptFileChange(path: path, kind: "write",
                                                                content: content)]))
                    continue
                }
                let text = (name == claudeAskTool ? claudeAskPayload(input: block["input"]) : nil)
                    ?? claudeSummary(input: block["input"])
                out.append(LinuxTranscriptEntry(kind: .tool, text: text, tool: name, at: at))
            case "tool_result":
                if let result = row["toolUseResult"] as? [String: Any],
                   result["type"] as? String == "create",
                   let path = result["filePath"] as? String, !path.isEmpty,
                   result["content"] is String {
                    continue
                }
                let text = claudeFirstLine(of: block["content"])
                guard !text.isEmpty else { continue }
                out.append(LinuxTranscriptEntry(kind: .toolResult, text: text, at: at))
            case "thinking":
                guard type != "user",
                      let raw = block["thinking"] as? String,
                      !raw.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                      claudeThinkingSignatureKind(block["signature"]) == .narration,
                      let entry = assistantEntry(text: raw, at: at) else { continue }
                out.append(entry)
            default:
                continue
            }
        }
        return out
    }

    enum ClaudeThinkingSignatureKind { case narration, thinking, unknown }

    /// Only an explicit, measured `narration` token is safe to publish. The current Mac reader
    /// also admits historical unmarked prose, but Linux has no version evidence proving that an
    /// unknown or malformed signature is narration rather than reasoning; it therefore fails
    /// closed until the identity slice can provide such a version gate.
    static func claudeThinkingSignatureKind(_ signature: Any?) -> ClaudeThinkingSignatureKind {
        guard let signature = signature as? String else { return .unknown }
        let head = signature.prefix(64)
        guard let bytes = Data(base64Encoded: String(head.prefix(head.count - head.count % 4))) else {
            return .unknown
        }
        if bytes.range(of: Data([0x42, 0x09] + Array("narration".utf8))) != nil {
            return .narration
        }
        if bytes.range(of: Data([0x42, 0x08] + Array("thinking".utf8))) != nil {
            return .thinking
        }
        return .unknown
    }

    static func claudeCrossSessionMessage(in raw: String, at: Int?) -> LinuxTranscriptEntry? {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        let tag = "cross-session-message"
        guard text.hasPrefix("<\(tag)"), let openEnd = text.firstIndex(of: ">") else { return nil }
        let afterName = text.index(text.startIndex, offsetBy: tag.count + 1)
        guard afterName <= openEnd,
              text[afterName] == " " || text[afterName] == "\t" || text[afterName] == ">" else {
            return nil
        }
        let close = "</\(tag)>"
        guard text.hasSuffix(close),
              let closeStart = text.range(of: close, options: .backwards)?.lowerBound,
              openEnd < closeStart else { return nil }
        let header = String(text[text.startIndex...openEnd])
        let body = text[text.index(after: openEnd)..<closeStart]
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { return nil }
        return LinuxTranscriptEntry(kind: .peer, text: body, at: at,
                                    source: claudeAttribute("from-name", in: header),
                                    sourceMode: claudeAttribute("from-mode", in: header))
    }

    static func claudeAttribute(_ name: String, in tag: String) -> String? {
        let prefix = " \(name)=\""
        guard let start = tag.range(of: prefix)?.upperBound,
              let end = tag[start...].firstIndex(of: "\"") else { return nil }
        let value = String(tag[start..<end])
            .replacingOccurrences(of: "&quot;", with: "\"")
            .replacingOccurrences(of: "&apos;", with: "'")
            .replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">")
            .replacingOccurrences(of: "&amp;", with: "&")
        return value.isEmpty ? nil : value
    }

    static func claudeWithoutMachineBlocks(_ text: String) -> String {
        var out = text
        for tag in ["task-notification", "system-reminder", "local-command-stdout",
                    "local-command-stderr", "command-name", "command-message", "command-args"] {
            out = claudeRemoving(tag: tag, from: out)
        }
        return out
    }

    static func claudeRemoving(tag: String, from text: String) -> String {
        let open = "<\(tag)>", close = "</\(tag)>"
        guard text.contains(open) else { return text }
        var out = ""
        var rest = Substring(text)
        while let start = rest.range(of: open) {
            out += rest[rest.startIndex..<start.lowerBound]
            guard let end = rest.range(of: close, range: start.upperBound..<rest.endIndex) else {
                return out
            }
            rest = rest[end.upperBound...]
        }
        return out + rest
    }

    static func claudeInner(tag: String, of text: String) -> String? {
        guard let start = text.range(of: "<\(tag)>") else { return nil }
        guard let end = text.range(of: "</\(tag)>", range: start.upperBound..<text.endIndex) else {
            return String(text[start.upperBound...])
        }
        return String(text[start.upperBound..<end.lowerBound])
    }

    static func claudeSlashCommand(in text: String) -> String? {
        guard let name = claudeInner(tag: "command-name", of: text)?
            .trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty else { return nil }
        let command = name.hasPrefix("/") ? name : "/" + name
        guard let args = claudeInner(tag: "command-args", of: text)?
            .trimmingCharacters(in: .whitespacesAndNewlines), !args.isEmpty else { return command }
        return command + " " + args
    }

    static func claudeCommandOutput(in text: String) -> String? {
        var parts: [String] = []
        for tag in ["local-command-stdout", "local-command-stderr"] {
            guard let body = claudeInner(tag: tag, of: text) else { continue }
            let value = plain(body).trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty { parts.append(value) }
        }
        return parts.isEmpty ? nil : parts.joined(separator: "\n")
    }

    static func claudeAskPayload(input: Any?) -> String? {
        guard let dict = input as? [String: Any],
              let asked = dict["questions"] as? [[String: Any]] else { return nil }
        var items: [[String: Any]] = []
        for question in asked {
            let text = clean(question["question"])
            var row: [String: Any] = ["q": text]
            let header = clean(question["header"])
            if !header.isEmpty { row["h"] = header }
            if question["multiSelect"] as? Bool == true { row["m"] = true }
            var options: [[String: Any]] = []
            for option in (question["options"] as? [[String: Any]] ?? []) {
                let label = clean(option["label"])
                guard !label.isEmpty else { continue }
                var out: [String: Any] = ["l": label]
                let note = clean(option["description"])
                if !note.isEmpty { out["d"] = note }
                options.append(out)
            }
            row["o"] = options
            guard !text.isEmpty || !options.isEmpty else { continue }
            items.append(row)
        }
        // Sorted keys, unlike the Mac: the same question must be the same bytes on every host.
        guard !items.isEmpty,
              let data = try? JSONSerialization.data(withJSONObject: items,
                                                     options: [.sortedKeys, .withoutEscapingSlashes]),
              let json = String(data: data, encoding: .utf8) else { return nil }
        return claudeAskMarker + json
    }

    static func claudeSummary(input: Any?) -> String {
        guard let dict = input as? [String: Any] else { return "" }
        for key in ["command", "file_path", "path", "pattern", "url", "query", "prompt",
                    "description"] {
            if let value = dict[key] as? String, !value.isEmpty {
                return claudeFirstLine(of: value)
            }
        }
        return ""
    }

    static func claudeFirstLine(of content: Any?) -> String {
        var text = ""
        if let string = content as? String {
            text = string
        } else if let list = content as? [[String: Any]] {
            text = list.compactMap { $0["text"] as? String }.joined(separator: " ")
        }
        text = plain(text).trimmingCharacters(in: .whitespacesAndNewlines)
        guard let line = text.split(separator: "\n").first else { return "" }
        return String(line)
    }

    // MARK: - Codex

    private static func codexEntries(bytes: Data, rows: inout ReverseCompleteRows,
                                     work: inout ParseWork, limit: Int) throws
        -> (entries: [LinuxTranscriptEntry], decodedRows: Int) {
        let formatter = makeTimestampFormatter()
        var newestFirst: [LinuxTranscriptEntry] = []
        let initialRows = work.decodedRows
        while let range = try rows.next(work: &work) {
            try work.consumeRow()
            guard !range.isEmpty else { continue }
            guard let row = jsonRow(in: bytes, range: range) else { continue }
            newestFirst.append(contentsOf: codexEntries(inRow: row, formatter: formatter).reversed())
            if newestFirst.count >= limit { break }
        }
        return (Array(newestFirst.reversed().suffix(limit)), work.decodedRows - initialRows)
    }

    static func codexEntries(inRow row: [String: Any],
                             formatter: ISO8601DateFormatter) -> [LinuxTranscriptEntry] {
        guard let payload = row["payload"] as? [String: Any] else { return [] }
        let at = timestamp(row["timestamp"], formatter: formatter)
        if row["type"] as? String == "event_msg",
           payload["type"] as? String == "item_completed",
           let item = payload["item"] as? [String: Any] {
            return codexEntries(ofItem: item, at: at)
        }
        guard row["type"] as? String == "response_item",
              payload["type"] as? String == "custom_tool_call",
              payload["name"] as? String == "exec",
              let input = payload["input"] as? String,
              let steps = codexLiteralPlan(in: input), !steps.isEmpty else { return [] }
        return [LinuxTranscriptEntry(kind: .tool, text: "Updated Plan", tool: "plan", at: at,
                                     plan: steps)]
    }

    static func codexEntries(ofItem item: [String: Any], at: Int?) -> [LinuxTranscriptEntry] {
        func entry(_ kind: LinuxTranscriptEntry.Kind, _ text: String,
                   tool: String? = nil) -> LinuxTranscriptEntry? {
            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return nil }
            return LinuxTranscriptEntry(kind: kind, text: trimmed, tool: tool, at: at)
        }

        switch item["type"] as? String {
        case "UserMessage":
            let canonical = canonicalImageContent(codexText(inContent: item["content"]))
            return [entry(.user, canonical.text)].compactMap { $0 }

        case "AgentMessage":
            return [assistantEntry(text: codexText(inContent: item["content"]), at: at)]
                .compactMap { $0 }

        case "CommandExecution":
            if let actions = codexParsedActions(item["parsed_cmd"]), !actions.isEmpty {
                var explored = entry(.tool, codexExploredTitle(actions), tool: "shell")
                explored?.activity = LinuxTranscriptActivity(
                    kind: "explored", status: codexNormalizedStatus(item["status"] as? String),
                    durationMilliseconds: codexDurationMilliseconds(item["duration"]),
                    actions: actions)
                return [explored].compactMap { $0 }
            }
            var out = [entry(.tool, codexCommand(item["command"]), tool: "shell")].compactMap { $0 }
            if let result = entry(.toolResult, codexOutcome(of: item)) { out.append(result) }
            return out

        case "McpToolCall":
            let name = [item["server"] as? String, item["tool"] as? String]
                .compactMap { $0 }.joined(separator: ".")
            let title = codexMCPTitle(item["arguments"])
            if !title.isEmpty {
                var called = entry(.tool, title, tool: name.isEmpty ? "mcp" : name)
                let result = codexMCPResultDetail(item["result"])
                let isError = (item["result"] as? [String: Any])?["isError"] as? Bool == true
                called?.activity = LinuxTranscriptActivity(
                    kind: "called", title: title,
                    status: isError ? "failed" : codexNormalizedStatus(item["status"] as? String),
                    durationMilliseconds: codexDurationMilliseconds(item["duration"]),
                    result: result.isEmpty ? nil : result)
                return [called].compactMap { $0 }
            }
            var out = [entry(.tool, codexArguments(item["arguments"]),
                             tool: name.isEmpty ? "mcp" : name)].compactMap { $0 }
            if let result = entry(.toolResult, codexMCPResult(item["result"])) { out.append(result) }
            return out

        case "FileChange":
            guard var edit = entry(.tool, codexChanged(item["changes"]), tool: "edit") else {
                return []
            }
            edit.fileChanges = codexFileChanges(item["changes"])
            return [edit]

        case "Extension":
            let kind = item["kind"] as? String ?? "extension"
            let what = item["query"] as? String ?? kind
            return [entry(.tool, what, tool: kind)].compactMap { $0 }

        default:
            return []
        }
    }

    static func codexText(inContent content: Any?) -> String {
        if let string = content as? String { return string }
        guard let blocks = content as? [[String: Any]] else { return "" }
        return blocks.compactMap { $0["text"] as? String }
            .filter { !$0.isEmpty }
            .joined(separator: "\n")
    }

    static func codexCommand(_ raw: Any?) -> String {
        guard let parts = raw as? [String] else { return raw as? String ?? "" }
        if parts.count == 3, parts[1] == "-lc" || parts[1] == "-c",
           let shell = parts.first?.split(separator: "/").last,
           ["zsh", "bash", "sh", "fish"].contains(String(shell)) {
            return parts[2]
        }
        return parts.joined(separator: " ")
    }

    static func codexOutcome(of item: [String: Any]) -> String {
        let output = (item["aggregated_output"] as? String)
            ?? (item["stdout"] as? String)
            ?? (item["stderr"] as? String) ?? ""
        let first = firstLine(ofText: output)
        if !first.isEmpty { return first }
        guard let code = item["exit_code"] as? Int else { return "" }
        return code == 0 ? "" : "exit \(code)"
    }

    static func codexArguments(_ raw: Any?) -> String {
        if let string = raw as? String { return firstLine(ofText: string) }
        guard let dict = raw as? [String: Any] else { return "" }
        for key in ["title", "query", "cmd", "command", "path", "code"] {
            if let value = dict[key] as? String, !value.isEmpty { return firstLine(ofText: value) }
        }
        return ""
    }

    static func codexMCPTitle(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any], let title = dict["title"] as? String else { return "" }
        return firstLine(ofText: title)
    }

    static func codexMCPResult(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any] else { return "" }
        return firstLine(ofText: codexText(inContent: dict["content"]))
    }

    static func codexMCPResultDetail(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any] else { return "" }
        let value = codexText(inContent: dict["content"])
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard value.count > mcpResultDetailLimit else { return value }
        return String(value.prefix(mcpResultDetailLimit)) + "…"
    }

    static func codexNormalizedStatus(_ raw: String?) -> String? {
        switch raw {
        case "in_progress", "inProgress", "running": return "inProgress"
        case "pending": return "pending"
        case "completed", "success": return "completed"
        case "failed", "error": return "failed"
        default: return nil
        }
    }

    static func codexDurationMilliseconds(_ raw: Any?) -> Int? {
        guard let value = raw as? [String: Any] else { return nil }
        let seconds = (value["secs"] as? NSNumber)?.int64Value ?? 0
        let nanos = (value["nanos"] as? NSNumber)?.int64Value ?? 0
        guard seconds >= 0, nanos >= 0, nanos < 1_000_000_000 else { return nil }
        let total = seconds.multipliedReportingOverflow(by: 1_000)
        guard !total.overflow else { return nil }
        let milliseconds = total.partialValue.addingReportingOverflow(nanos / 1_000_000)
        guard !milliseconds.overflow, milliseconds.partialValue <= Int64(Int.max) else { return nil }
        return Int(milliseconds.partialValue)
    }

    static func codexParsedActions(_ raw: Any?) -> [LinuxTranscriptToolAction]? {
        guard let values = raw as? [[String: Any]], !values.isEmpty else { return nil }
        var actions: [LinuxTranscriptToolAction] = []
        for value in values {
            guard let kind = value["type"] as? String, kind == "read" || kind == "search" else {
                return nil
            }
            let action = LinuxTranscriptToolAction(
                kind: kind, command: value["cmd"] as? String, name: value["name"] as? String,
                path: value["path"] as? String, query: value["query"] as? String)
            let visible = [action.name, action.path, action.query, action.command]
                .compactMap { $0 }.contains { !$0.isEmpty }
            guard visible else { return nil }
            actions.append(action)
        }
        return actions
    }

    static func codexExploredTitle(_ actions: [LinuxTranscriptToolAction]) -> String {
        let labels = actions.prefix(3).map { action -> String in
            if action.kind == "search" { return "Search " + (action.query ?? action.path ?? "") }
            return "Read " + (action.name ?? action.path ?? "")
        }
        return labels.joined(separator: ", ") + (actions.count > 3 ? " +\(actions.count - 3)" : "")
    }

    static func codexChanged(_ raw: Any?) -> String {
        guard let dict = raw as? [String: Any], !dict.isEmpty else { return "" }
        let names = dict.keys.sorted().map { path -> String in
            String(path.split(separator: "/", omittingEmptySubsequences: false).last ?? "")
        }
        if names.count <= 3 { return names.joined(separator: ", ") }
        return names.prefix(3).joined(separator: ", ") + " +\(names.count - 3)"
    }

    static func codexFileChanges(_ raw: Any?) -> [LinuxTranscriptFileChange] {
        guard let changes = raw as? [String: Any] else { return [] }
        return changes.keys.sorted().compactMap { path in
            guard let change = changes[path] as? [String: Any] else { return nil }
            let kind = (change["type"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "change"
            return LinuxTranscriptFileChange(
                path: path, kind: kind, unifiedDiff: change["unified_diff"] as? String,
                content: change["content"] as? String, movePath: change["move_path"] as? String)
        }
    }

    /// Only a literal plan array from Codex's generated `tools.update_plan(...)` exec wrapper; no
    /// script is evaluated.
    static func codexLiteralPlan(in source: String) -> [LinuxTranscriptPlanStep]? {
        guard source.utf8.count <= 100_000,
              let call = source.range(of: "tools.update_plan(") else { return nil }
        let suffix = String(source[call.upperBound...])
        let pattern = #"\bplan\s*:\s*(\[|[A-Za-z_$][A-Za-z0-9_$]*)"#
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: suffix,
                                           range: NSRange(suffix.startIndex..., in: suffix)),
              let valueRange = Range(match.range(at: 1), in: suffix) else { return nil }
        let token = String(suffix[valueRange])
        let array: String?
        if token == "[" {
            array = codexBracketedArray(in: suffix, from: valueRange.lowerBound)
        } else {
            let prefix = String(source[..<call.lowerBound])
            let assignment = #"(?:const|let|var)\s+"#
                + NSRegularExpression.escapedPattern(for: token) + #"\s*=\s*\["#
            guard let assignmentRegex = try? NSRegularExpression(pattern: assignment),
                  let found = assignmentRegex.matches(
                    in: prefix, range: NSRange(prefix.startIndex..., in: prefix)).last,
                  let foundRange = Range(found.range, in: prefix),
                  let open = prefix[..<foundRange.upperBound].lastIndex(of: "[") else { return nil }
            array = codexBracketedArray(in: prefix, from: open)
        }
        guard let array else { return nil }
        var parser = LinuxCodexLiteralPlanParser(array)
        return parser.parse()
    }

    static func codexBracketedArray(in text: String, from open: String.Index) -> String? {
        var index = open, depth = 0, quoted = false, escaped = false
        while index < text.endIndex {
            let character = text[index]
            if quoted {
                if escaped { escaped = false } else if character == "\\" { escaped = true } else if character == "\"" { quoted = false }
            } else if character == "\"" {
                quoted = true
            } else if character == "[" {
                depth += 1
            } else if character == "]" {
                depth -= 1
                if depth == 0 { return String(text[open...index]) }
                if depth < 0 { return nil }
            }
            index = text.index(after: index)
        }
        return nil
    }
}

private struct LinuxCodexLiteralPlanParser {
    let bytes: [UInt8]
    var index = 0

    init(_ source: String) { bytes = Array(source.utf8) }

    mutating func parse() -> [LinuxTranscriptPlanStep]? {
        guard take(0x5B) else { return nil }
        var out: [LinuxTranscriptPlanStep] = []
        skipSpace()
        if take(0x5D) { return out }
        while out.count < 100 {
            guard let step = object() else { return nil }
            out.append(step)
            skipSpace()
            if take(0x5D) { return index == bytes.count ? out : nil }
            guard take(0x2C) else { return nil }
        }
        return nil
    }

    mutating func object() -> LinuxTranscriptPlanStep? {
        guard take(0x7B) else { return nil }
        var step: String?, status: String?
        while true {
            guard let key = identifier(), take(0x3A), let value = string() else { return nil }
            if key == "step", step == nil { step = value } else if key == "status", status == nil { status = value } else { return nil }
            skipSpace()
            if take(0x7D) { break }
            guard take(0x2C) else { return nil }
        }
        guard let step = step?.trimmingCharacters(in: .whitespacesAndNewlines), !step.isEmpty,
              let status = LinuxNativeTranscriptWire.codexNormalizedStatus(status),
              ["pending", "inProgress", "completed"].contains(status) else { return nil }
        return LinuxTranscriptPlanStep(step: step, status: status)
    }

    mutating func identifier() -> String? {
        skipSpace()
        let start = index
        while index < bytes.count {
            let byte = bytes[index]
            guard (byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122)
                    || (index > start && byte >= 48 && byte <= 57) || byte == 95 else { break }
            index += 1
        }
        guard index > start else { return nil }
        return String(decoding: bytes[start..<index], as: UTF8.self)
    }

    mutating func string() -> String? {
        skipSpace()
        guard index < bytes.count, bytes[index] == 0x22 else { return nil }
        let start = index
        index += 1
        var escaped = false
        while index < bytes.count {
            let byte = bytes[index]
            index += 1
            if escaped { escaped = false; continue }
            if byte == 0x5C { escaped = true; continue }
            if byte == 0x22 {
                return try? JSONSerialization.jsonObject(with: Data(bytes[start..<index]),
                                                         options: [.fragmentsAllowed]) as? String
            }
        }
        return nil
    }

    mutating func take(_ byte: UInt8) -> Bool {
        skipSpace()
        guard index < bytes.count, bytes[index] == byte else { return false }
        index += 1
        return true
    }

    mutating func skipSpace() {
        while index < bytes.count, [9, 10, 13, 32].contains(bytes[index]) { index += 1 }
    }
}
