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
final class TerminalCommandScheduler: @unchecked Sendable {
    /// One Mac, eight terminal commands in flight at once — iTerm automation alone may wait 15
    /// seconds, so this is a real ceiling and not a formality.
    static let depth = 8
    /// One session, two commands in flight at once — enough for a nested cascade to make forward
    /// progress without letting one terminal starve every other channel's slot.
    static let channelDepth = 2

    private static let workerKey = DispatchSpecificKey<Bool>()
    private let queue: DispatchQueue
    private let admissionLock = NSLock()
    private var outstanding = 0
    private var outstandingByChannel: [String: Int] = [:]
    private var maintenanceRequestID: String?
    /// Touched only on the serial terminal queue. It lets nested inline work inherit an outer
    /// reservation without double-counting that same terminal while still accounting a newly
    /// discovered child/coordination recipient.
    private var activeChannels: Set<String> = []

    init(label: String) {
        queue = DispatchQueue(label: label)
        queue.setSpecific(key: Self.workerKey, value: true)
    }

    /// `true` when the caller is already running inside this lane's own worker — the seam
    /// `RemoteServer.writing(_:keeping:_:)` uses to skip duplicate idempotency bookkeeping for a
    /// nested command.
    var isCurrentlyOnWorkerQueue: Bool {
        DispatchQueue.getSpecific(key: Self.workerKey) == true
    }

    /// Asynchronous wrapper for a single-channel command. Validation, reservation and idempotency
    /// stay with the caller; only the work itself enters the bounded worker.
    @discardableResult
    func enqueue(channel: String? = nil, _ work: @escaping () -> Void) -> Bool {
        enqueue(channels: channel.map { [$0] } ?? [], work)
    }

    /// Multi-recipient form used by durable coordination delivery. One HTTP operation may fan a
    /// release out to several waiter terminals; reserving every known recipient before enqueue
    /// makes the documented per-session depth true for those production paths too.
    @discardableResult
    func enqueue(channels rawChannels: [String], _ work: @escaping () -> Void) -> Bool {
        let channels = Array(Set(rawChannels.filter { !$0.isEmpty })).sorted()
        // Nested terminal work is already inside the single broker ordering domain. Execute it
        // inline so a root `/end` can finish children deepest-first without deadlocking behind
        // itself or reversing the safety order.
        if isCurrentlyOnWorkerQueue {
            let added = channels.filter { !activeChannels.contains($0) }
            admissionLock.lock()
            guard added.allSatisfy({ (outstandingByChannel[$0] ?? 0) < Self.channelDepth }) else {
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
              outstanding < Self.depth,
              channels.allSatisfy({ (outstandingByChannel[$0] ?? 0) < Self.channelDepth }) else {
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

    /// Test receipt for the production admission counters, read under the same lock that mutates
    /// them. A nested terminal cascade must finish with both totals back at zero.
    func outstandingForTesting() -> (total: Int, channels: Int) {
        admissionLock.lock(); defer { admissionLock.unlock() }
        return (outstanding, outstandingByChannel.values.reduce(0, +))
    }

    /// What `build.sh`'s restart-maintenance drain reads: how much of the lane is still occupied,
    /// globally and per channel.
    struct DrainSnapshot: Equatable {
        let outstanding: Int
        let channels: [String: Int]
    }

    func drainSnapshot() -> DrainSnapshot {
        admissionLock.lock(); defer { admissionLock.unlock() }
        return DrainSnapshot(outstanding: outstanding, channels: outstandingByChannel)
    }

    /// Work already admitted may finish its nested cascade; its counters are the drain receipt.
    /// Closing (`active: true`) always adopts the given id; opening back up only clears the flag
    /// when the caller is the one who closed it (or no id is given), so a stale reopen from a
    /// superseded request cannot reopen admission underneath the request that is still active.
    func setRestartMaintenance(active: Bool, requestID: String?) {
        admissionLock.lock()
        if active { maintenanceRequestID = requestID }
        else if requestID == nil || maintenanceRequestID == requestID {
            maintenanceRequestID = nil
        }
        admissionLock.unlock()
    }

    /// One retryable fact about *this lane*: it is closed, and to which request. Building the
    /// actual HTTP response stays with the caller, which owns the wire shape.
    struct MaintenanceRefusal: Equatable {
        let requestID: String
    }

    func maintenanceRefusal() -> MaintenanceRefusal? {
        admissionLock.lock(); defer { admissionLock.unlock() }
        return maintenanceRequestID.map(MaintenanceRefusal.init)
    }
}
