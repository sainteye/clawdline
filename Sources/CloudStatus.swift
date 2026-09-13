import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

/// Everything this Mac knows about its Cloud link, in one place a person can open
/// (design `cloud-error-transparency.md` §4.1, §11.2, §11.3).
///
/// **Why it exists.** Every layer between a phone and this Mac used to refuse silently, and the
/// counters that did exist — the inbound queue, the refusal lane, the outbound window — had no
/// production reader. On 2026-09-13 the log showed 790 lines of `reason=invalid` whose `count=`
/// restarted on every reconnect, and nothing anywhere said which device, which sequence, or why.
///
/// **Three readers, one owner.** The same state feeds the fixed file
/// `~/Library/Logs/Clawdline/diagnostics/cloud-status.json`, the `cloud.status` Cloud read, and
/// the `cloud_status` digest merged into `orch/<machine>` — the notice a phone still receives
/// when none of its commands can.
///
/// **A lock, not an actor.** The transport reports drops inline from its receive loop and must
/// return at once; an actor would need one unstructured task per drop, and a flood of 790 drops
/// would be 790 tasks. Every record here is constant work under one lock.
///
/// **Bounded everywhere.** Counts are keyed by a closed vocabulary. Recent drops keep 20, recent
/// commands 50, recent notices 20, device ids 256. Counts live for the process — reconnecting does
/// not reset them — and `counting_since` says when they started.
///
/// **What it never holds.** Codes, device ids, sequences, request ids, key ids, counts and times.
/// Never a command body, a prompt, transcript text, a title or a filesystem path.
final class CloudStatus: @unchecked Sendable {
    static let schemaVersion = 1
    static let fileName = "cloud-status.json"
    static let recentDropLimit = 20
    static let recentCommandLimit = 50
    static let recentNoticeLimit = 20
    static let deviceLimit = 256
    static let digestDropLimit = 10
    static let digestNoticeLimit = 10
    static let digestByteLimit = 8 * 1024
    static let defaultWriteIntervalMilliseconds: UInt64 = 2_000

    /// Beside `report.json`, derived from the same directory so a test root moves both.
    static func fileURL(root: URL? = nil) -> URL {
        DiagnosticReport.directory(root: root).appendingPathComponent(fileName)
    }

    /// The process-lifetime instance production composes. It writes the fixed file.
    static let shared = CloudStatus(fileURL: CloudStatus.fileURL())

    enum Reply: Equatable {
        case published
        case notice
    }

    struct CommandRow: Equatable {
        let sender: String
        let sequence: UInt64
        var request: String? = nil
        var type: String? = nil
        var session: String? = nil
        var acceptedAt: UInt64? = nil
        var executedAt: UInt64? = nil
        var outcome: String? = nil
        var deliveredAt: UInt64? = nil
        var undeliverable: String? = nil
        var refusal: (layer: CloudRefusalLayer, code: String)? = nil

        static func == (lhs: CommandRow, rhs: CommandRow) -> Bool {
            lhs.sender == rhs.sender && lhs.sequence == rhs.sequence && lhs.request == rhs.request
                && lhs.type == rhs.type && lhs.session == rhs.session
                && lhs.acceptedAt == rhs.acceptedAt && lhs.executedAt == rhs.executedAt
                && lhs.outcome == rhs.outcome && lhs.deliveredAt == rhs.deliveredAt
                && lhs.undeliverable == rhs.undeliverable
                && lhs.refusal?.layer == rhs.refusal?.layer && lhs.refusal?.code == rhs.refusal?.code
        }
    }

    private struct DropRow {
        let at: UInt64
        let drop: CloudInboundDrop
    }

    private struct NoticeRow {
        let at: UInt64
        let sender: String
        let sequence: UInt64
        let request: String?
        let layer: CloudRefusalLayer
        let code: String
    }

    typealias ClockProvider = @Sendable () -> CloudClockGuardDetail?
    typealias NoticeObserver = @Sendable () -> Void

    private let lock = NSLock()
    private let nowMilliseconds: @Sendable () -> UInt64
    private let fileURL: URL?
    private let writeIntervalMilliseconds: UInt64
    private let writeQueue = DispatchQueue(label: "clawdline.cloud.status-file")
    private let countingSince: UInt64

    private var bridge = "detached"
    private var transportState = "idle"
    private var connectedSince: UInt64?
    private var lastClose: String?
    private var tokenExpiresAt: UInt64?
    private var nextRotationAt: UInt64?
    private var clockProvider: ClockProvider?
    private var clockSince: (detail: CloudClockGuardDetail, since: UInt64)?
    private var keyID: String?
    private var rosterReadable: Bool?
    private var deviceIDs: [String] = []
    private var acceptedTotal: UInt64 = 0
    private var dropped: [CloudInboundDropCode: UInt64] = [:]
    private var recentDrops: [DropRow] = []
    private var commands: [CommandRow] = []
    private var notices: [NoticeRow] = []
    private var undeliverableTotal: UInt64 = 0
    private var expiredReadyTotal: UInt64 = 0
    private var expiredReceiptTotal: UInt64 = 0
    private var laneDroppedTotal: UInt64 = 0
    private var replySequences: [(spool: Int64, sender: String, sequence: UInt64)] = []
    private var noticeObserver: NoticeObserver?
    private var noticePending = false
    private var writeScheduled = false
    private var lastWriteAt: UInt64?
    /// Composition alone never touches the disk; the first bridge that starts with this owner
    /// turns the file on. A test that builds production services therefore writes nothing.
    private var fileWritingEnabled = false

    init(
        fileURL: URL? = nil,
        writeIntervalMilliseconds: UInt64 = CloudStatus.defaultWriteIntervalMilliseconds,
        nowMilliseconds: @escaping @Sendable () -> UInt64 = {
            UInt64(max(0, Date().timeIntervalSince1970 * 1_000))
        }
    ) {
        self.fileURL = fileURL
        self.writeIntervalMilliseconds = writeIntervalMilliseconds
        self.nowMilliseconds = nowMilliseconds
        countingSince = nowMilliseconds()
    }

    // MARK: - Recording

    func setBridgeState(_ label: String) {
        mutate { $0.bridge = label }
    }

    func setClockProvider(_ provider: ClockProvider?) {
        mutate(write: false) { $0.clockProvider = provider }
    }

    func enableFileWriting() {
        mutate { $0.fileWritingEnabled = true }
    }

    func setKeyID(_ keyID: String?) {
        mutate { $0.keyID = keyID }
    }

    func record(_ event: CloudTransportConnectionEvent) {
        let now = nowMilliseconds()
        mutate { status in
            switch event {
            case .ready(_, let expiresAt, let refreshAhead):
                status.transportState = "connected"
                status.connectedSince = now
                status.tokenExpiresAt = expiresAt
                status.nextRotationAt = expiresAt > refreshAhead ? expiresAt - refreshAhead : expiresAt
            case .reconnecting(let reason):
                status.transportState = "reconnecting"
                status.connectedSince = nil
                status.lastClose = CloudStatus.snake(reason)
            case .stopped(let reason):
                status.transportState = "stopped"
                status.connectedSince = nil
                status.lastClose = CloudStatus.snake(reason)
            case .roster(let readable, let deviceIDs):
                status.rosterReadable = readable
                status.deviceIDs = Array(deviceIDs.prefix(CloudStatus.deviceLimit))
            }
        }
    }

    func recordDrop(_ drop: CloudInboundDrop) {
        let now = nowMilliseconds()
        mutate(notice: true) { status in
            status.dropped[drop.code, default: 0] &+= 1
            status.recentDrops.insert(DropRow(at: now, drop: drop), at: 0)
            if status.recentDrops.count > CloudStatus.recentDropLimit { status.recentDrops.removeLast() }
            if let readable = drop.rosterReadable { status.rosterReadable = readable }
        }
    }

    /// One envelope left the transport as an admitted command or read.
    func recordInboundAccepted() {
        mutate(write: false) { $0.acceptedTotal &+= 1 }
    }

    func recordCommand(sender: String, sequence: UInt64, request: String?, type: String?,
                       session: String?) {
        let now = nowMilliseconds()
        mutate { status in
            status.updateRow(sender: sender, sequence: sequence) { row in
                if row.acceptedAt == nil { row.acceptedAt = now }
                row.request = CloudStatus.identifier(request) ?? row.request
                row.type = CloudStatus.identifier(type) ?? row.type
                row.session = CloudStatus.identifier(session) ?? row.session
            }
        }
    }

    func recordExecuted(sender: String, sequence: UInt64, outcome: String) {
        let now = nowMilliseconds()
        mutate { status in
            status.updateRow(sender: sender, sequence: sequence) { row in
                row.executedAt = now
                row.outcome = CloudStatus.snake(outcome)
            }
        }
    }

    func recordRefusal(sender: String, sequence: UInt64, request: String?, type: String?,
                       session: String?, layer: CloudRefusalLayer, code: String, reply: Reply) {
        let now = nowMilliseconds()
        mutate(notice: reply == .notice) { status in
            status.updateRow(sender: sender, sequence: sequence) { row in
                if row.acceptedAt == nil { row.acceptedAt = now }
                row.request = CloudStatus.identifier(request) ?? row.request
                row.type = CloudStatus.identifier(type) ?? row.type
                row.session = CloudStatus.identifier(session) ?? row.session
                row.refusal = (layer, CloudStatus.snake(code))
            }
            guard reply == .notice else { return }
            status.notices.insert(NoticeRow(
                at: now, sender: sender, sequence: sequence,
                request: CloudStatus.identifier(request), layer: layer,
                code: CloudStatus.snake(code)), at: 0)
            if status.notices.count > CloudStatus.recentNoticeLimit { status.notices.removeLast() }
        }
    }

    /// The reply for `(sender, sequence)` was sealed as spool row `spoolSequence`.
    func recordReplySealed(sender: String, sequence: UInt64, spoolSequence: Int64) {
        mutate(write: false) { status in
            status.replySequences.removeAll { $0.spool == spoolSequence }
            status.replySequences.append((spoolSequence, sender, sequence))
            if status.replySequences.count > CloudStatus.recentCommandLimit {
                status.replySequences.removeFirst()
            }
        }
    }

    /// Receipts arrive for every outbound row; only one for a command's reply changes anything.
    func recordReplyReceipt(spoolSequence: Int64, delivered: Bool) {
        let now = nowMilliseconds()
        announce { status in
            guard let owner = status.replySequences.first(where: { $0.spool == spoolSequence })
            else { return false }
            status.updateRow(sender: owner.sender, sequence: owner.sequence, create: false) { row in
                if delivered {
                    row.deliveredAt = now
                } else {
                    row.undeliverable = "peer_rejected"
                }
            }
            if !delivered { status.undeliverableTotal &+= 1 }
            return !delivered
        }
    }

    func recordUndeliverable(sender: String, sequence: UInt64, reason: String) {
        mutate(notice: true) { status in
            status.undeliverableTotal &+= 1
            status.updateRow(sender: sender, sequence: sequence) { row in
                row.undeliverable = CloudStatus.snake(reason)
            }
        }
    }

    /// Counted for every live row; announced only when the row was a command's reply, because a
    /// snapshot that expired while offline has no asker waiting on it.
    func recordExpiry(_ expiry: CloudSpoolExpiry, spoolSequence: Int64) {
        announce { status in
            switch expiry {
            case .readyExpired: status.expiredReadyTotal &+= 1
            case .receiptExpired: status.expiredReceiptTotal &+= 1
            }
            guard let owner = status.replySequences.first(where: { $0.spool == spoolSequence })
            else { return false }
            status.updateRow(sender: owner.sender, sequence: owner.sequence, create: false) { row in
                row.undeliverable = expiry.rawValue
            }
            return true
        }
    }

    func recordLaneDropped() {
        mutate(notice: true) { $0.laneDroppedTotal &+= 1 }
    }

    /// Called once per transition from "nothing to announce" to "something to announce", so a
    /// flood of drops wakes the notice publisher once rather than once per drop.
    func setNoticeObserver(_ observer: NoticeObserver?) {
        mutate(write: false) { $0.noticeObserver = observer }
    }

    /// The publisher is about to include the current digest; later changes ask again.
    func clearNoticeRequest() {
        lock.lock()
        noticePending = false
        lock.unlock()
    }

    // MARK: - Reading

    func commandRows() -> [CommandRow] {
        lock.lock()
        defer { lock.unlock() }
        return commands
    }

    func droppedCount(_ code: CloudInboundDropCode) -> UInt64 {
        lock.lock()
        defer { lock.unlock() }
        return dropped[code] ?? 0
    }

    /// The complete snapshot (§4.1), as `cloud.status` answers it and the fixed file holds it.
    func snapshot() -> [String: Any] {
        let clock = currentClock()
        lock.lock()
        defer { lock.unlock() }
        let now = nowMilliseconds()
        var transport: [String: Any] = ["state": transportState]
        transport["connected_since"] = Self.iso(connectedSince)
        transport["connected_since_ms"] = Self.json(connectedSince)
        transport["last_close"] = lastClose ?? NSNull()
        return [
            "clawdline_cloud_status": Self.schemaVersion,
            "generated_at": Self.iso(now),
            "generated_at_ms": now,
            "counting_since": Self.iso(countingSince),
            "counting_since_ms": countingSince,
            "bridge": bridge,
            "transport": transport,
            "token": [
                "expires_at": Self.iso(tokenExpiresAt),
                "expires_at_ms": Self.json(tokenExpiresAt),
                "next_rotation_at": Self.iso(nextRotationAt),
                "next_rotation_at_ms": Self.json(nextRotationAt),
            ] as [String: Any],
            "clock_guard": clockObject(clock, now: now, full: true),
            "identity": [
                "key_id": keyID ?? NSNull(),
                "roster_readable": rosterReadable.map { $0 as Any } ?? NSNull(),
                "devices": deviceIDs.map { ["device": $0] },
            ] as [String: Any],
            "inbound": [
                "accepted": acceptedTotal,
                "dropped": droppedObject(),
                "recent_drops": recentDrops.map(dropObject),
            ] as [String: Any],
            "commands": commands.map(commandObject),
            "notices": notices.map(noticeObject),
            "reply": [
                "undeliverable": undeliverableTotal,
                "expired_ready": expiredReadyTotal,
                "expired_receipt": expiredReceiptTotal,
                "lane_dropped": laneDroppedTotal,
            ] as [String: Any],
        ]
    }

    /// The notice (§11.2): at most `digestByteLimit` bytes serialized. The two recent lists are
    /// trimmed from their oldest end until it fits; the counts are bounded by their vocabulary.
    func noticeDigest() -> [String: Any] {
        let clock = currentClock()
        lock.lock()
        let now = nowMilliseconds()
        var drops = Array(recentDrops.prefix(Self.digestDropLimit)).map(dropObject)
        var recentNotices = Array(notices.prefix(Self.digestNoticeLimit)).map(noticeObject)
        var digest: [String: Any] = [
            "v": Self.schemaVersion,
            "generated_at_ms": now,
            "counting_since_ms": countingSince,
            "clock_guard": clockObject(clock, now: now, full: false),
            "token_expires_at_ms": Self.json(tokenExpiresAt),
            "key_id": keyID ?? NSNull(),
            "roster_readable": rosterReadable.map { $0 as Any } ?? NSNull(),
            "dropped": droppedObject(),
        ]
        lock.unlock()
        while true {
            digest["recent_drops"] = drops
            digest["recent_notices"] = recentNotices
            guard let bytes = try? JSONSerialization.data(
                withJSONObject: digest, options: [.withoutEscapingSlashes]) else { return digest }
            if bytes.count <= Self.digestByteLimit { return digest }
            if recentNotices.count >= drops.count, !recentNotices.isEmpty {
                recentNotices.removeLast()
            } else if !drops.isEmpty {
                drops.removeLast()
            } else {
                return digest
            }
        }
    }

    /// Writes the file now, bypassing the interval and the enable switch. Tests call it; production
    /// writes on change.
    func writeFileNow() {
        lock.lock()
        writeScheduled = false
        lock.unlock()
        writeFile()
    }

    // MARK: - Internals

    private func mutate(write: Bool = true, notice: Bool = false, _ body: (CloudStatus) -> Void) {
        announce(write: write) { status in
            body(status)
            return notice
        }
    }

    /// `body` answers whether this change is something a phone should be told about.
    private func announce(write: Bool = true, _ body: (CloudStatus) -> Bool) {
        lock.lock()
        let notice = body(self)
        var observer: NoticeObserver?
        if notice, !noticePending {
            noticePending = true
            observer = noticeObserver
        }
        let schedule = write ? scheduleWriteLocked() : nil
        lock.unlock()
        observer?()
        if let delay = schedule {
            writeQueue.asyncAfter(deadline: .now() + .milliseconds(Int(clamping: delay))) {
                [weak self] in self?.writeFileIfScheduled()
            }
        }
    }

    private func scheduleWriteLocked() -> UInt64? {
        guard fileURL != nil, fileWritingEnabled, !writeScheduled else { return nil }
        writeScheduled = true
        let now = nowMilliseconds()
        guard let lastWriteAt else { return 0 }
        let elapsed = now >= lastWriteAt ? now - lastWriteAt : 0
        return elapsed >= writeIntervalMilliseconds ? 0 : writeIntervalMilliseconds - elapsed
    }

    private func writeFileIfScheduled() {
        lock.lock()
        guard writeScheduled else { lock.unlock(); return }
        writeScheduled = false
        lock.unlock()
        writeFile()
    }

    /// Same directory and 0600 policy as `DiagnosticReport`. The candidate is created private and
    /// then renamed over the file, so the name never points at a half-written or wider-mode file.
    private func writeFile() {
        guard let fileURL else { return }
        lock.lock()
        lastWriteAt = nowMilliseconds()
        lock.unlock()
        guard let bytes = try? JSONSerialization.data(
            withJSONObject: snapshot(), options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        else { return }
        let manager = FileManager.default
        let folder = fileURL.deletingLastPathComponent()
        let candidate = folder.appendingPathComponent(".\(Self.fileName).writing")
        do {
            try manager.createDirectory(at: folder, withIntermediateDirectories: true)
            try? manager.removeItem(at: candidate)
            guard manager.createFile(atPath: candidate.path, contents: bytes,
                                     attributes: [.posixPermissions: 0o600]) else { return }
            guard rename(candidate.path, fileURL.path) == 0 else {
                try? manager.removeItem(at: candidate)
                return
            }
        } catch {
            return
        }
    }

    private func currentClock() -> CloudClockGuardDetail? {
        lock.lock()
        let provider = clockProvider
        lock.unlock()
        guard let detail = provider?() else { return nil }
        lock.lock()
        if clockSince?.detail.state != detail.state || clockSince?.detail.reason != detail.reason {
            clockSince = (detail, nowMilliseconds())
        } else if let since = clockSince?.since {
            clockSince = (detail, since)
        }
        lock.unlock()
        return detail
    }

    private func clockObject(_ detail: CloudClockGuardDetail?, now: UInt64, full: Bool) -> [String: Any] {
        guard let detail else {
            return full
                ? ["state": NSNull(), "reason": NSNull(), "since": NSNull(), "since_ms": NSNull(),
                   "clears_at": NSNull(), "clears_at_ms": NSNull()]
                : ["state": NSNull(), "reason": NSNull(), "clears_at_ms": NSNull()]
        }
        let clearsAt = detail.clearsInMilliseconds.map { now &+ $0 }
        var object: [String: Any] = [
            "state": detail.state == .ready ? "ready" : "uncertain",
            "reason": detail.reason ?? NSNull(),
            "clears_at_ms": Self.json(clearsAt),
        ]
        if full {
            let since = clockSince?.since
            object["since"] = Self.iso(since)
            object["since_ms"] = Self.json(since)
            object["clears_at"] = Self.iso(clearsAt)
            object["clears_in_ms"] = Self.json(detail.clearsInMilliseconds)
        }
        return object
    }

    private func droppedObject() -> [String: Any] {
        var object: [String: Any] = [:]
        for (code, count) in dropped { object[code.rawValue] = count }
        return object
    }

    private func dropObject(_ row: DropRow) -> [String: Any] {
        [
            "at_ms": row.at,
            "sender": row.drop.sender ?? NSNull(),
            "seq": Self.json(row.drop.sequence),
            "code": row.drop.code.rawValue,
            "key_id": row.drop.keyID ?? NSNull(),
            "expected_key_id": row.drop.expectedKeyID ?? NSNull(),
            "highest_seq": Self.json(row.drop.highestSequence),
        ]
    }

    private func noticeObject(_ row: NoticeRow) -> [String: Any] {
        [
            "at_ms": row.at, "sender": row.sender, "seq": row.sequence,
            "request": row.request ?? NSNull(), "layer": row.layer.rawValue, "code": row.code,
        ]
    }

    private func commandObject(_ row: CommandRow) -> [String: Any] {
        [
            "sender": row.sender,
            "seq": row.sequence,
            "request": row.request ?? NSNull(),
            "type": row.type ?? NSNull(),
            "session": row.session ?? NSNull(),
            "accepted_at_ms": Self.json(row.acceptedAt),
            "executed_at_ms": Self.json(row.executedAt),
            "outcome": row.outcome ?? NSNull(),
            "delivered_at_ms": Self.json(row.deliveredAt),
            "undeliverable": row.undeliverable ?? NSNull(),
            "refusal": row.refusal.map { ["layer": $0.layer.rawValue, "code": $0.code] as Any }
                ?? NSNull(),
        ]
    }

    private func updateRow(sender: String, sequence: UInt64, create: Bool = true,
                           _ body: (inout CommandRow) -> Void) {
        if let index = commands.firstIndex(where: { $0.sender == sender && $0.sequence == sequence }) {
            body(&commands[index])
            return
        }
        guard create else { return }
        var row = CommandRow(sender: sender, sequence: sequence)
        body(&row)
        commands.insert(row, at: 0)
        if commands.count > Self.recentCommandLimit { commands.removeLast() }
    }

    /// Ids and command words only: printable ASCII without spaces, at most 128 bytes.
    static func identifier(_ value: String?) -> String? {
        guard let value, !value.isEmpty, value.utf8.count <= 128,
              value.utf8.allSatisfy({ $0 > 0x20 && $0 < 0x7f }) else { return nil }
        return value
    }

    /// A code as the vocabulary spells it; anything else collapses to a bounded snake word.
    static func snake(_ value: String) -> String {
        let lowered = value.lowercased().unicodeScalars.map { scalar -> Character in
            let v = scalar.value
            return (v >= 0x61 && v <= 0x7a) || (v >= 0x30 && v <= 0x39) ? Character(scalar) : "_"
        }
        let word = String(lowered.prefix(64))
        return word.isEmpty ? "unknown" : word
    }

    private static func json(_ value: UInt64?) -> Any { value.map { $0 as Any } ?? NSNull() }

    private static func iso(_ milliseconds: UInt64?) -> Any {
        guard let milliseconds else { return NSNull() }
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        return formatter.string(from: Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1_000))
    }
}

/// Feeds the spool's outbound expiries into a status owner. Every other metric stays a no-op here,
/// exactly as `CloudNoopSpoolMetrics` leaves it.
final class CloudStatusSpoolMetrics: CloudSpoolMetrics, @unchecked Sendable {
    private let status: CloudStatus

    init(status: CloudStatus) { self.status = status }

    func recordRows(_: Int, runtime _: CloudSpoolRuntime) {}
    func recordBytes(_: Int, runtime _: CloudSpoolRuntime) {}
    func recordAdmissionRefusal(runtime _: CloudSpoolRuntime, reason _: CloudSpoolRefusalReason,
                                scope _: CloudSpoolRefusalScope) {}
    func recordOccupancyAlert(runtime _: CloudSpoolRuntime, dimension _: CloudSpoolOccupancyDimension,
                              level _: CloudSpoolOccupancyAlert) {}
    func recordLateSettleTelemetry(runtime _: CloudSpoolRuntime) {}
    func recordStateStoreGC(store _: CloudStateStore, reason _: CloudStateStoreGCReason) {}
    func recordStateStoreCorrupt(store _: CloudStateStore) {}

    func recordExpiry(runtime _: CloudSpoolRuntime, expiry: CloudSpoolExpiry, sequence: Int64) {
        status.recordExpiry(expiry, spoolSequence: sequence)
    }
}

/// The wire contract for the `error` object in a Mac reply (design §11.1, §11.6).
enum CloudErrorContract {
    /// §11.6: the only `detail` fields each code may carry. Anything else is dropped here, so a
    /// path or a title cannot reach a viewer through `detail` by accident.
    static let detailWhitelist: [String: Set<String>] = [
        "command_clock_uncertain": ["reason", "clears_in_ms"],
        "key_id_mismatch": ["key_id", "expected_key_id"],
        "replay": ["highest_seq"],
        "cloud_ingress_busy": ["retry_after", "lane", "limit"],
        "cloud_read_busy": ["retry_after", "lane", "limit"],
        "no_whisper": ["reason"],
        "terminal_closed": ["app"],
        "would_lose_work": ["lost"],
    ]

    static func detail(code: String, _ candidate: [String: Any]) -> [String: Any] {
        guard let allowed = detailWhitelist[code] else { return [:] }
        return candidate.filter { allowed.contains($0.key) }
    }

    /// The error object with `layer`, `seq` and a whitelisted `detail` added. Top-level fields the
    /// route already put there stay where they are (§11.1).
    static func error(_ base: [String: Any], code: String, layer: CloudRefusalLayer,
                      sequence: UInt64, detail: [String: Any] = [:]) -> [String: Any] {
        var error = base
        error["code"] = code
        if error["message"] == nil { error["message"] = "This command could not be completed." }
        error["layer"] = layer.rawValue
        error["seq"] = sequence
        let filtered = Self.detail(code: code, detail)
        if !filtered.isEmpty { error["detail"] = filtered }
        return error
    }
}
