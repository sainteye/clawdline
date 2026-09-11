import Foundation

/// Sole owner of the existing bounded terminal mutation lane: the single serial worker queue, its
/// total and per-channel depth, and the nested-inline reservation accounting that lets a root
/// `/end` finish children deepest-first without deadlocking behind itself.
///
/// Extracted verbatim from `RemoteServer`, which previously declared this state and logic
/// directly (`RemoteServer.swift:190–280`, admission gate `:3358–3455`) and its restart-maintenance
/// rejection/drain wrapper in `Coordinator.swift`'s `extension RemoteServer`. Every legacy facade
/// name (`enqueueTerminalCommand`, `terminalOutstandingForTesting`, `terminalDrainSnapshot`,
/// `setRestartMaintenance`, `terminalMaintenanceRefusal`, `terminalDepth`, `terminalChannelDepth`)
/// is unchanged on `RemoteServer`, so no external caller — `Orchestrator`, `ProjectBoardWorkflowHTTP`,
/// or any test file — has to change. This type owns none of the restart-*receipt* persistence
/// (`Orchestrator.beginRestartMaintenance`/`advanceRestartMaintenance`/`abortRestartMaintenance`,
/// `RestartRecordsTransaction`): that is Registry-owned after W2-2. What
/// this type owns is only the admission flag those routes flip and the counters they read to
/// decide when the drain is complete — the terminal lane's own half of that handshake.
public final class TerminalCommandScheduler: @unchecked Sendable {
    /// One Mac, eight terminal commands in flight at once — iTerm automation alone may wait 15
    /// seconds, so this is a real ceiling and not a formality.
    public static let depth = 8
    /// One session, two commands in flight at once — enough for a nested cascade to make forward
    /// progress without letting one terminal starve every other channel's slot.
    public static let channelDepth = 2

    public let limits: TerminalWorkLimits
    private let workerKey = DispatchSpecificKey<String>()
    private let workerIdentity = UUID().uuidString
    private let queue: DispatchQueue
    private let admissionLock = NSLock()
    private var outstanding = 0
    private var outstandingByChannel: [String: Int] = [:]
    private var maintenanceRequestID: String?
    private var activeCommandIDs: Set<String> = []
    /// Touched only on the serial terminal queue. It lets nested inline work inherit an outer
    /// reservation without double-counting that same terminal while still accounting a newly
    /// discovered child/coordination recipient.
    private var activeChannels: Set<String> = []

    public init(label: String, limits: TerminalWorkLimits = .production) {
        self.limits = limits
        queue = DispatchQueue(label: label)
        queue.setSpecific(key: workerKey, value: workerIdentity)
    }

    /// `true` when the caller is already running inside this lane's own worker — the seam
    /// `RemoteServer.writing(_:keeping:_:)` uses to skip duplicate idempotency bookkeeping for a
    /// nested command.
    public var isCurrentlyOnWorkerQueue: Bool {
        DispatchQueue.getSpecific(key: workerKey) == workerIdentity
    }

    /// Asynchronous wrapper for a single-channel command. Validation, reservation and idempotency
    /// stay with the caller; only the work itself enters the bounded worker.
    @discardableResult
    public func enqueue(channel: String? = nil, _ work: @escaping () -> Void) -> Bool {
        enqueue(channels: channel.map { [$0] } ?? [], work)
    }

    /// Multi-recipient form used by durable coordination delivery. One HTTP operation may fan a
    /// release out to several waiter terminals; reserving every known recipient before enqueue
    /// makes the documented per-session depth true for those production paths too.
    @discardableResult
    public func enqueue(channels rawChannels: [String], _ work: @escaping () -> Void) -> Bool {
        let channels = Array(Set(rawChannels.filter { !$0.isEmpty })).sorted()
        // Nested terminal work is already inside the single broker ordering domain. Execute it
        // inline so a root `/end` can finish children deepest-first without deadlocking behind
        // itself or reversing the safety order.
        if isCurrentlyOnWorkerQueue {
            let added = channels.filter { !activeChannels.contains($0) }
            admissionLock.lock()
            guard added.allSatisfy({ (outstandingByChannel[$0] ?? 0) < limits.perChannel }) else {
                admissionLock.unlock()
                return false
            }
            for channel in added {
                outstandingByChannel[channel, default: 0] += 1
            }
            admissionLock.unlock()
            let previous = activeChannels
            activeChannels.formUnion(added)
            work()
            activeChannels = previous
            admissionLock.lock()
            for channel in added {
                let remaining = (outstandingByChannel[channel] ?? 1) - 1
                if remaining == 0 { outstandingByChannel.removeValue(forKey: channel) }
                else { outstandingByChannel[channel] = remaining }
            }
            admissionLock.unlock()
            return true
        }
        admissionLock.lock()
        guard maintenanceRequestID == nil,
              outstanding < limits.total,
              channels.allSatisfy({ (outstandingByChannel[$0] ?? 0) < limits.perChannel }) else {
            admissionLock.unlock()
            return false
        }
        outstanding += 1
        for channel in channels { outstandingByChannel[channel, default: 0] += 1 }
        admissionLock.unlock()
        queue.async { [self] in
            let previous = activeChannels
            activeChannels.formUnion(channels)
            work()
            activeChannels = previous
            admissionLock.lock()
            outstanding -= 1
            for channel in channels {
                let remaining = (outstandingByChannel[channel] ?? 1) - 1
                if remaining == 0 { outstandingByChannel.removeValue(forKey: channel) }
                else { outstandingByChannel[channel] = remaining }
            }
            admissionLock.unlock()
        }
        return true
    }

    /// Synchronous Application-layer entry point used by platform lifecycle compositions. The
    /// complete operation closure — including terminal inventory/preflight — runs on the same
    /// serial worker as the Mac facade. A nested lifecycle inherits its outer reservation and
    /// executes inline, retaining the existing deepest-first semantics.
    public func run<T>(commandID: String, channel: String,
                       operation: TerminalEffectOperation,
                       _ work: () throws -> T) throws -> T {
        guard SessionLaunchPolicy.opaqueCommandID(commandID) != nil,
              !channel.isEmpty, channel.utf8.count <= 128 else {
            throw TerminalLifecycleFailure(
                code: .invalidCommand,
                message: "Terminal scheduling requires bounded opaque command and channel ids.")
        }

        if isCurrentlyOnWorkerQueue {
            let addedChannel = !activeChannels.contains(channel)
            admissionLock.lock()
            guard !activeCommandIDs.contains(commandID) else {
                admissionLock.unlock()
                throw TerminalLifecycleFailure(
                    code: .duplicateCommand,
                    message: "That terminal command identity is already active.")
            }
            guard !addedChannel || (outstandingByChannel[channel] ?? 0) < limits.perChannel else {
                admissionLock.unlock()
                throw TerminalLifecycleFailure(
                    code: .channelCapacity,
                    message: "The terminal channel has reached its capacity of \(limits.perChannel).")
            }
            activeCommandIDs.insert(commandID)
            if addedChannel { outstandingByChannel[channel, default: 0] += 1 }
            admissionLock.unlock()
            let previous = activeChannels
            activeChannels.insert(channel)
            defer {
                activeChannels = previous
                admissionLock.lock()
                activeCommandIDs.remove(commandID)
                if addedChannel {
                    let remaining = (outstandingByChannel[channel] ?? 1) - 1
                    if remaining == 0 { outstandingByChannel.removeValue(forKey: channel) }
                    else { outstandingByChannel[channel] = remaining }
                }
                admissionLock.unlock()
            }
            return try work()
        }

        admissionLock.lock()
        if let requestID = maintenanceRequestID {
            admissionLock.unlock()
            throw TerminalLifecycleFailure(
                code: .maintenance,
                message: "Terminal admission is closed for restart maintenance \(requestID).")
        }
        guard !activeCommandIDs.contains(commandID) else {
            admissionLock.unlock()
            throw TerminalLifecycleFailure(
                code: .duplicateCommand,
                message: "That terminal command identity is already active.")
        }
        guard outstanding < limits.total else {
            admissionLock.unlock()
            throw TerminalLifecycleFailure(
                code: .totalCapacity,
                message: "The terminal lane has reached its total capacity of \(limits.total).")
        }
        guard (outstandingByChannel[channel] ?? 0) < limits.perChannel else {
            admissionLock.unlock()
            throw TerminalLifecycleFailure(
                code: .channelCapacity,
                message: "The terminal channel has reached its capacity of \(limits.perChannel).")
        }
        outstanding += 1
        outstandingByChannel[channel, default: 0] += 1
        activeCommandIDs.insert(commandID)
        admissionLock.unlock()

        return try queue.sync { [self] in
            let previous = activeChannels
            activeChannels.insert(channel)
            defer {
                activeChannels = previous
                admissionLock.lock()
                outstanding -= 1
                activeCommandIDs.remove(commandID)
                let remaining = (outstandingByChannel[channel] ?? 1) - 1
                if remaining == 0 { outstandingByChannel.removeValue(forKey: channel) }
                else { outstandingByChannel[channel] = remaining }
                admissionLock.unlock()
            }
            return try work()
        }
    }

    /// Test receipt for the production admission counters, read under the same lock that mutates
    /// them. A nested terminal cascade must finish with both totals back at zero.
    public func outstandingForTesting() -> (total: Int, channels: Int) {
        admissionLock.lock(); defer { admissionLock.unlock() }
        return (outstanding, outstandingByChannel.values.reduce(0, +))
    }

    /// What `build.sh`'s restart-maintenance drain reads: how much of the lane is still occupied,
    /// globally and per channel.
    public struct DrainSnapshot: Equatable {
        public let outstanding: Int
        public let channels: [String: Int]
    }

    public func drainSnapshot() -> DrainSnapshot {
        admissionLock.lock(); defer { admissionLock.unlock() }
        return DrainSnapshot(outstanding: outstanding, channels: outstandingByChannel)
    }

    /// Work already admitted may finish its nested cascade; its counters are the drain receipt.
    /// Closing (`active: true`) always adopts the given id; opening back up only clears the flag
    /// when the caller is the one who closed it (or no id is given), so a stale reopen from a
    /// superseded request cannot reopen admission underneath the request that is still active.
    public func setRestartMaintenance(active: Bool, requestID: String?) {
        admissionLock.lock()
        if active { maintenanceRequestID = requestID }
        else if requestID == nil || maintenanceRequestID == requestID {
            maintenanceRequestID = nil
        }
        admissionLock.unlock()
    }

    /// One retryable fact about *this lane*: it is closed, and to which request. Building the
    /// actual HTTP response stays with the caller, which owns the wire shape.
    public struct MaintenanceRefusal: Equatable {
        public let requestID: String
    }

    public func maintenanceRefusal() -> MaintenanceRefusal? {
        admissionLock.lock(); defer { admissionLock.unlock() }
        return maintenanceRequestID.map(MaintenanceRefusal.init)
    }
}
