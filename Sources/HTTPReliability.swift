import Foundation
import Network

/// One queue-owned timer for every HTTP deadline. Cancelling a job removes its closure from the
/// owner immediately; completed requests and frames therefore do not remain retained until their
/// old wall-clock deadline. The explicit ceiling is a second line of defence behind the HTTP
/// admission bounds rather than an invitation to create one Dispatch work item per operation.
final class BoundedHTTPDeadlineScheduler {
    typealias Cancellation = () -> Void

    private struct Entry {
        let deadline: DispatchTime
        let action: () -> Void
    }

    let limit: Int
    private let queue: DispatchQueue
    private let timer: DispatchSourceTimer
    private var entries: [UInt64: Entry] = [:]
    private var nextID: UInt64 = 0

    init(queue: DispatchQueue, limit: Int) {
        precondition(limit > 0)
        self.queue = queue
        self.limit = limit
        timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .distantFuture)
        timer.setEventHandler { [weak self] in self?.fire() }
        timer.resume()
    }

    deinit { timer.cancel() }

    func schedule(after: TimeInterval, action: @escaping () -> Void) -> Cancellation? {
        dispatchPrecondition(condition: .onQueue(queue))
        guard after > 0, entries.count < limit else { return nil }
        nextID &+= 1
        let id = nextID
        entries[id] = Entry(deadline: .now() + after, action: action)
        arm()
        return { [weak self] in self?.cancel(id) }
    }

    var pendingCount: Int {
        dispatchPrecondition(condition: .onQueue(queue))
        return entries.count
    }

    private func cancel(_ id: UInt64) {
        dispatchPrecondition(condition: .onQueue(queue))
        guard entries.removeValue(forKey: id) != nil else { return }
        arm()
    }

    private func arm() {
        guard let next = entries.values.min(by: {
            $0.deadline.uptimeNanoseconds < $1.deadline.uptimeNanoseconds
        }) else {
            timer.schedule(deadline: .distantFuture)
            return
        }
        timer.schedule(deadline: next.deadline)
    }

    private func fire() {
        dispatchPrecondition(condition: .onQueue(queue))
        let now = DispatchTime.now().uptimeNanoseconds
        let due = entries
            .filter { $0.value.deadline.uptimeNanoseconds <= now }
            .sorted {
                if $0.value.deadline.uptimeNanoseconds == $1.value.deadline.uptimeNanoseconds {
                    return $0.key < $1.key
                }
                return $0.value.deadline.uptimeNanoseconds < $1.value.deadline.uptimeNanoseconds
            }
        for (id, _) in due { entries.removeValue(forKey: id) }
        arm()
        for (_, entry) in due { entry.action() }
    }
}

/// Owns the memory and lifetime debt created at the local HTTP door.
///
/// `RemoteServer` has one serial owner queue. Every method here is called on that queue; the
/// scheduler and stream writer must return their callbacks to it as well. Keeping the counters and
/// the decisions together makes the limits observable without turning Network.framework objects
/// into policy.
final class HTTPReliability {
    typealias ConnectionID = ObjectIdentifier
    typealias Cancellation = () -> Void
    typealias Scheduler = (_ after: TimeInterval, _ action: @escaping () -> Void) -> Cancellation?
    typealias StreamSender = (_ bytes: Data, _ completion: @escaping (Error?) -> Void) -> Void

    static let connectionLimit = 128
    static let connectionRefusalLimit = 16
    static let aggregateRequestByteLimit = 64 << 20
    static let requestReadSecondLimit = 15
    static let streamLimit = 16
    static let streamByteLimit = 4 << 20
    static let aggregateStreamByteLimit = 16 << 20
    static let streamWriteSecondLimit = 20
    static let responseCloseGraceSecondLimit = 30
    // One possible deadline for each admitted or capacity-response connection.
    static let deadlineTaskLimit = 144

    /// Explicit implementation defaults. They are operational safety rails, not evidence that a
    /// workload is adequately provisioned; W6 owns that approval after runtime measurement.
    struct Limits: Equatable {
        let connections: Int
        let connectionRefusals: Int
        let aggregateRequestBytes: Int
        let requestReadSeconds: TimeInterval
        let streams: Int
        let streamBytes: Int
        let aggregateStreamBytes: Int
        let streamWriteSeconds: TimeInterval
        let deadlineTasks: Int

        static let implementationDefault = Limits(
            connections: HTTPReliability.connectionLimit,
            connectionRefusals: HTTPReliability.connectionRefusalLimit,
            aggregateRequestBytes: HTTPReliability.aggregateRequestByteLimit,
            requestReadSeconds: TimeInterval(HTTPReliability.requestReadSecondLimit),
            streams: HTTPReliability.streamLimit,
            streamBytes: HTTPReliability.streamByteLimit,
            aggregateStreamBytes: HTTPReliability.aggregateStreamByteLimit,
            streamWriteSeconds: TimeInterval(HTTPReliability.streamWriteSecondLimit),
            deadlineTasks: HTTPReliability.deadlineTaskLimit
        )

        init(connections: Int, connectionRefusals: Int = HTTPReliability.connectionRefusalLimit,
             aggregateRequestBytes: Int,
             requestReadSeconds: TimeInterval, streams: Int, streamBytes: Int,
             aggregateStreamBytes: Int, streamWriteSeconds: TimeInterval,
             deadlineTasks: Int = HTTPReliability.deadlineTaskLimit) {
            precondition(connections > 0 && connectionRefusals > 0)
            precondition(aggregateRequestBytes > 0 && requestReadSeconds > 0)
            precondition(streams > 0 && streamBytes > 0 && aggregateStreamBytes >= streamBytes)
            precondition(streamWriteSeconds > 0 && deadlineTasks > 0)
            self.connections = connections
            self.connectionRefusals = connectionRefusals
            self.aggregateRequestBytes = aggregateRequestBytes
            self.requestReadSeconds = requestReadSeconds
            self.streams = streams
            self.streamBytes = streamBytes
            self.aggregateStreamBytes = aggregateStreamBytes
            self.streamWriteSeconds = streamWriteSeconds
            self.deadlineTasks = deadlineTasks
        }

        var object: [String: Any] {
            [
                "connections": connections,
                "connection_refusals": connectionRefusals,
                "aggregate_request_bytes": aggregateRequestBytes,
                "request_read_seconds": requestReadSeconds,
                "streams": streams,
                "stream_bytes": streamBytes,
                "aggregate_stream_bytes": aggregateStreamBytes,
                "stream_write_seconds": streamWriteSeconds,
                "deadline_tasks": deadlineTasks,
                "approval": "implementation_default_pending_w6",
            ]
        }
    }

    struct Metrics: Equatable {
        fileprivate(set) var connectionsCurrent = 0
        fileprivate(set) var connectionsPeak = 0
        fileprivate(set) var requestBytesCurrent = 0
        fileprivate(set) var requestBytesPeak = 0
        fileprivate(set) var connectionRefusals = 0
        fileprivate(set) var capacityResponsesCurrent = 0
        fileprivate(set) var capacityResponsesPeak = 0
        fileprivate(set) var capacityResponseDrops = 0
        fileprivate(set) var requestByteRefusals = 0
        fileprivate(set) var requestReadTimeouts = 0
        fileprivate(set) var streamsCurrent = 0
        fileprivate(set) var streamsPeak = 0
        fileprivate(set) var streamBytesCurrent = 0
        fileprivate(set) var streamBytesPeak = 0
        fileprivate(set) var streamRefusals = 0
        fileprivate(set) var streamEvictions = 0
        fileprivate(set) var streamCapacityEvictions = 0
        fileprivate(set) var streamAggregateCapacityEvictions = 0
        fileprivate(set) var streamTimeoutEvictions = 0
        fileprivate(set) var streamWriteErrorEvictions = 0
        fileprivate(set) var snapshotCoalesces = 0
        fileprivate(set) var unknownIdentityRefusals = 0
        fileprivate(set) var deadlineTasksCurrent = 0
        fileprivate(set) var deadlineTasksPeak = 0
        fileprivate(set) var deadlineTaskRefusals = 0

        var object: [String: Any] {
            [
                "connections": ["current": connectionsCurrent, "peak": connectionsPeak,
                                "refused": connectionRefusals],
                "capacity_responses": ["current": capacityResponsesCurrent,
                                       "peak": capacityResponsesPeak,
                                       "dropped": capacityResponseDrops],
                "request_bytes": ["current": requestBytesCurrent, "peak": requestBytesPeak,
                                  "refused": requestByteRefusals,
                                  "read_timeouts": requestReadTimeouts],
                "streams": ["current": streamsCurrent, "peak": streamsPeak,
                            "refused": streamRefusals, "evicted": streamEvictions],
                "stream_bytes": ["current": streamBytesCurrent, "peak": streamBytesPeak,
                                 "capacity_evictions": streamCapacityEvictions,
                                 "aggregate_capacity_evictions": streamAggregateCapacityEvictions,
                                 "timeout_evictions": streamTimeoutEvictions,
                                 "write_error_evictions": streamWriteErrorEvictions,
                                 "snapshot_coalesces": snapshotCoalesces],
                "unknown_identity_refusals": unknownIdentityRefusals,
                "deadline_tasks": ["current": deadlineTasksCurrent,
                                   "peak": deadlineTasksPeak,
                                   "refused": deadlineTaskRefusals],
            ]
        }
    }

    enum ConnectionAdmission: Equatable {
        case admitted
        case refused(limit: Int)
        case unknownIdentity
    }

    enum RequestByteAdmission: Equatable {
        case retained
        case refused(limit: Int, current: Int)
        case unknownIdentity
        case requestReadFinished
    }

    enum CapacityResponseAdmission: Equatable {
        case admitted
        case dropped(limit: Int)
        case unknownIdentity
    }

    enum NetworkRefusal: Equatable {
        case connectionCapacity(limit: Int)
        case requestCapacity(limit: Int, current: Int)
        case headersTooLarge
        case badRequest
        case bodyTooLarge(length: Int, limit: Int)
    }

    enum StreamAdmission: Equatable {
        case admitted
        case refused(limit: Int)
        case unknownIdentity
    }

    enum FrameKind: Equatable {
        case control
        case event
        /// Only current, full observations use this. Commands and effect acknowledgements never do.
        case fullSnapshot(String)
        case heartbeat
    }

    enum EnqueueResult: Equatable {
        case enqueued
        case coalesced
        case heartbeatDropped
        case evicted
        case unknownIdentity
    }

    enum EvictionReason: Equatable {
        case capacity
        case aggregateCapacity
        case completionTimeout
        case writeError
    }

    private struct Connection {
        var retainedBytes = 0
        var reading = true
        var cancelReadDeadline: Cancellation?
    }

    private struct Frame {
        let sequence: UInt64
        let bytes: Data
        let kind: FrameKind
        var cancelDeadline: Cancellation?
    }

    private struct Stream {
        let send: StreamSender
        let evict: (EvictionReason) -> Void
        var active: Frame?
        var pending: [Frame] = []

        var outstandingBytes: Int {
            (active?.bytes.count ?? 0) + pending.reduce(0) { $0 + $1.bytes.count }
        }
    }

    private final class DeadlineLease {
        var active = true
        var cancelScheduled: Cancellation?
    }

    let limits: Limits
    private let schedule: Scheduler
    private var connections: [ConnectionID: Connection] = [:]
    private var networkConnections: [ConnectionID: NWConnection] = [:]
    private var capacityResponseConnections: Set<ConnectionID> = []
    private var responseDeadlines: [ConnectionID: Cancellation] = [:]
    private var streams: [ConnectionID: Stream] = [:]
    private var nextFrameSequence: UInt64 = 0
    private(set) var metrics = Metrics()

    init(limits: Limits = .implementationDefault, schedule: @escaping Scheduler) {
        self.limits = limits
        self.schedule = schedule
    }

    /// Network.framework adapter for the bounded request-reading phase. Parsing and routing stay
    /// supplied by `RemoteServer`; this owner retains only connection identity, bytes and timers.
    func acceptNetworkConnection<Request>(
        _ connection: NWConnection,
        queue: DispatchQueue,
        headerLimit: Int,
        bodyLimit: Int,
        parse: @escaping (Data) -> Request?,
        contentLength: @escaping (Request) -> Int,
        handle: @escaping (Request, Data) -> Void,
        refuse: @escaping (NetworkRefusal, NWConnection) -> Void,
        didClose: @escaping (ConnectionID) -> Void
    ) {
        let id = ObjectIdentifier(connection)
        connection.stateUpdateHandler = { [weak self, weak connection] state in
            guard let self, let connection else { return }
            switch state {
            case .cancelled, .failed:
                guard self.networkConnections.removeValue(forKey: id) === connection else { return }
                self.cancelResponseDeadline(for: id)
                if self.capacityResponseConnections.contains(id) {
                    self.closeCapacityResponse(id)
                } else {
                    self.closeConnection(id)
                    didClose(id)
                }
            default:
                break
            }
        }
        connection.start(queue: queue)
        switch admitConnection(id, onReadTimeout: { [weak connection] in connection?.cancel() }) {
        case .admitted:
            networkConnections[id] = connection
            receiveNetworkRequest(
                connection, buffer: Data(), headerLimit: headerLimit, bodyLimit: bodyLimit,
                parse: parse, contentLength: contentLength, handle: handle, refuse: refuse)
        case .refused(let limit):
            switch admitCapacityResponse(id) {
            case .admitted:
                networkConnections[id] = connection
                refuse(.connectionCapacity(limit: limit), connection)
            case .dropped, .unknownIdentity:
                connection.cancel()
            }
        case .unknownIdentity:
            connection.cancel()
        }
    }

    private func receiveNetworkRequest<Request>(
        _ connection: NWConnection,
        buffer: Data,
        headerLimit: Int,
        bodyLimit: Int,
        parse: @escaping (Data) -> Request?,
        contentLength: @escaping (Request) -> Int,
        handle: @escaping (Request, Data) -> Void,
        refuse: @escaping (NetworkRefusal, NWConnection) -> Void
    ) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: headerLimit) {
            [weak self] data, _, done, error in
            guard let self else { return }
            let id = ObjectIdentifier(connection)
            guard self.isReadingRequest(id) else { return }
            var buffer = buffer
            if let data {
                switch self.retainRequestBytes(buffer.count + data.count, for: id) {
                case .retained: buffer.append(data)
                case .refused(let limit, let current):
                    refuse(.requestCapacity(limit: limit, current: current), connection); return
                case .unknownIdentity, .requestReadFinished:
                    connection.cancel(); return
                }
            }
            if error != nil || (done && buffer.isEmpty) { connection.cancel(); return }
            guard let headEnd = buffer.range(of: Data("\r\n\r\n".utf8)) else {
                if buffer.count > headerLimit { refuse(.headersTooLarge, connection); return }
                if done { connection.cancel(); return }
                self.receiveNetworkRequest(
                    connection, buffer: buffer, headerLimit: headerLimit, bodyLimit: bodyLimit,
                    parse: parse, contentLength: contentLength, handle: handle, refuse: refuse)
                return
            }
            guard headEnd.lowerBound <= headerLimit else {
                refuse(.headersTooLarge, connection); return
            }
            guard let request = parse(Data(buffer[..<headEnd.lowerBound])) else {
                refuse(.badRequest, connection); return
            }
            let wanted = contentLength(request)
            guard wanted >= 0 else { refuse(.badRequest, connection); return }
            guard wanted <= bodyLimit else {
                refuse(.bodyTooLarge(length: wanted, limit: bodyLimit), connection); return
            }
            let bodyStart = headEnd.upperBound
            let have = buffer.count - bodyStart
            guard have >= wanted else {
                if done { connection.cancel(); return }
                self.receiveNetworkRequest(
                    connection, buffer: buffer, headerLimit: headerLimit, bodyLimit: bodyLimit,
                    parse: parse, contentLength: contentLength, handle: handle, refuse: refuse)
                return
            }
            guard self.finishRequestRead(for: id) else { connection.cancel(); return }
            handle(request, Data(buffer[bodyStart..<(bodyStart + wanted)]))
        }
    }

    func admitConnection(_ id: ConnectionID?, onReadTimeout: @escaping () -> Void) -> ConnectionAdmission {
        guard let id, connections[id] == nil, !capacityResponseConnections.contains(id) else {
            metrics.unknownIdentityRefusals += 1
            return .unknownIdentity
        }
        guard connections.count < limits.connections else {
            metrics.connectionRefusals += 1
            return .refused(limit: limits.connections)
        }
        connections[id] = Connection()
        metrics.connectionsCurrent += 1
        metrics.connectionsPeak = max(metrics.connectionsPeak, metrics.connectionsCurrent)
        guard let cancel = makeDeadline(after: limits.requestReadSeconds, action: { [weak self] in
            self?.expireRequestRead(id, notify: onReadTimeout)
        }) else {
            connections.removeValue(forKey: id)
            metrics.connectionsCurrent -= 1
            metrics.connectionRefusals += 1
            return .refused(limit: limits.connections)
        }
        if var connection = connections[id], connection.reading {
            connection.cancelReadDeadline = cancel
            connections[id] = connection
        } else {
            cancel()
        }
        return .admitted
    }

    func admitCapacityResponse(_ id: ConnectionID?) -> CapacityResponseAdmission {
        guard let id, connections[id] == nil, !capacityResponseConnections.contains(id) else {
            metrics.unknownIdentityRefusals += 1
            return .unknownIdentity
        }
        guard capacityResponseConnections.count < limits.connectionRefusals else {
            metrics.capacityResponseDrops += 1
            return .dropped(limit: limits.connectionRefusals)
        }
        capacityResponseConnections.insert(id)
        metrics.capacityResponsesCurrent += 1
        metrics.capacityResponsesPeak = max(
            metrics.capacityResponsesPeak, metrics.capacityResponsesCurrent)
        return .admitted
    }

    func closeCapacityResponse(_ id: ConnectionID?) {
        guard let id, capacityResponseConnections.remove(id) != nil else { return }
        cancelResponseDeadline(for: id)
        metrics.capacityResponsesCurrent -= 1
    }

    /// Replace this connection's charged byte count with `total`. The caller asks before retaining
    /// the newly received chunk, so a refused chunk never becomes owner-held aggregate debt.
    func retainRequestBytes(_ total: Int, for id: ConnectionID?) -> RequestByteAdmission {
        guard let id, var connection = connections[id] else {
            metrics.unknownIdentityRefusals += 1
            return .unknownIdentity
        }
        guard connection.reading else { return .requestReadFinished }
        let candidate = metrics.requestBytesCurrent - connection.retainedBytes + max(0, total)
        guard candidate <= limits.aggregateRequestBytes else {
            metrics.requestByteRefusals += 1
            return .refused(limit: limits.aggregateRequestBytes,
                            current: metrics.requestBytesCurrent)
        }
        metrics.requestBytesCurrent = candidate
        metrics.requestBytesPeak = max(metrics.requestBytesPeak, candidate)
        connection.retainedBytes = max(0, total)
        connections[id] = connection
        return .retained
    }

    /// The complete request may enter a route, while its body remains charged until the response
    /// begins. This separates slowloris lifetime from queued-body memory lifetime.
    func finishRequestRead(for id: ConnectionID?) -> Bool {
        guard let id, var connection = connections[id] else {
            metrics.unknownIdentityRefusals += 1
            return false
        }
        guard connection.reading else { return false }
        connection.reading = false
        connection.cancelReadDeadline?()
        connection.cancelReadDeadline = nil
        connections[id] = connection
        return true
    }

    /// Release the request body when a response or stream starts. Connection admission remains
    /// charged until Network.framework observes cancellation/failure.
    func finishRequest(for id: ConnectionID?) {
        guard let id, var connection = connections[id] else { return }
        connection.cancelReadDeadline?()
        connection.cancelReadDeadline = nil
        connection.reading = false
        metrics.requestBytesCurrent -= connection.retainedBytes
        connection.retainedBytes = 0
        connections[id] = connection
    }

    func isReadingRequest(_ id: ConnectionID?) -> Bool {
        guard let id else { return false }
        return connections[id]?.reading == true
    }

    func closeConnection(_ id: ConnectionID?) {
        guard let id, let connection = connections.removeValue(forKey: id) else { return }
        cancelResponseDeadline(for: id)
        connection.cancelReadDeadline?()
        metrics.requestBytesCurrent -= connection.retainedBytes
        metrics.connectionsCurrent -= 1
        closeStream(id)
    }

    private func expireRequestRead(_ id: ConnectionID, notify: () -> Void) {
        guard var connection = connections[id], connection.reading else { return }
        connection.reading = false
        connection.cancelReadDeadline = nil
        metrics.requestReadTimeouts += 1
        connections[id] = connection
        notify()
    }

    func openStream(_ id: ConnectionID?, send: @escaping StreamSender,
                    evict: @escaping (EvictionReason) -> Void) -> StreamAdmission {
        guard let id, connections[id] != nil, streams[id] == nil else {
            metrics.streamRefusals += 1
            metrics.unknownIdentityRefusals += 1
            return .unknownIdentity
        }
        guard streams.count < limits.streams else {
            metrics.streamRefusals += 1
            return .refused(limit: limits.streams)
        }
        finishRequest(for: id)
        streams[id] = Stream(send: send, evict: evict, active: nil)
        metrics.streamsCurrent += 1
        metrics.streamsPeak = max(metrics.streamsPeak, metrics.streamsCurrent)
        return .admitted
    }

    @discardableResult
    func enqueueStreamFrame(_ bytes: Data, kind: FrameKind,
                            for id: ConnectionID?) -> EnqueueResult {
        guard let id, var stream = streams[id] else {
            metrics.unknownIdentityRefusals += 1
            return .unknownIdentity
        }

        if kind == .heartbeat,
           stream.active?.kind == .heartbeat || stream.pending.contains(where: { $0.kind == .heartbeat }) {
            return .heartbeatDropped
        }

        nextFrameSequence &+= 1
        let frame = Frame(sequence: nextFrameSequence, bytes: bytes, kind: kind,
                          cancelDeadline: nil)
        var replacedBytes = 0
        var replaceIndex: Int?
        if case .fullSnapshot(let channel) = kind {
            replaceIndex = stream.pending.lastIndex {
                if case .fullSnapshot(let existing) = $0.kind { return existing == channel }
                return false
            }
            if let replaceIndex { replacedBytes = stream.pending[replaceIndex].bytes.count }
        }
        let streamCandidate = stream.outstandingBytes - replacedBytes + bytes.count
        guard streamCandidate <= limits.streamBytes else {
            streams[id] = stream
            evictStream(id, reason: .capacity)
            return .evicted
        }

        var aggregateCandidate = metrics.streamBytesCurrent - replacedBytes + bytes.count
        while aggregateCandidate > limits.aggregateStreamBytes {
            guard let victim = slowestStreamDebtOwner(
                replacingBytes: replacedBytes, on: id) else {
                streams[id] = stream
                evictStream(id, reason: .aggregateCapacity)
                return .evicted
            }
            evictStream(victim, reason: .aggregateCapacity)
            if victim == id { return .evicted }
            aggregateCandidate = metrics.streamBytesCurrent - replacedBytes + bytes.count
        }

        metrics.streamBytesCurrent = aggregateCandidate
        metrics.streamBytesPeak = max(metrics.streamBytesPeak, aggregateCandidate)
        if let replaceIndex {
            stream.pending[replaceIndex] = frame
            metrics.snapshotCoalesces += 1
            streams[id] = stream
            return .coalesced
        }
        stream.pending.append(frame)
        streams[id] = stream
        sendNext(on: id)
        return .enqueued
    }

    func closeStream(_ id: ConnectionID?) {
        guard let id, let stream = streams.removeValue(forKey: id) else { return }
        stream.active?.cancelDeadline?()
        metrics.streamBytesCurrent -= stream.outstandingBytes
        metrics.streamsCurrent -= 1
    }

    /// Clear live debt during server stop while retaining lifetime peaks/refusals for diagnostics.
    func resetActive() {
        for connection in networkConnections.values { connection.cancel() }
        for cancel in responseDeadlines.values { cancel() }
        for stream in streams.values { stream.active?.cancelDeadline?() }
        for connection in connections.values { connection.cancelReadDeadline?() }
        streams.removeAll()
        networkConnections.removeAll()
        capacityResponseConnections.removeAll()
        responseDeadlines.removeAll()
        connections.removeAll()
        metrics.streamsCurrent = 0
        metrics.streamBytesCurrent = 0
        metrics.connectionsCurrent = 0
        metrics.capacityResponsesCurrent = 0
        metrics.requestBytesCurrent = 0
        metrics.deadlineTasksCurrent = 0
    }

    func outstandingStreamBytes(for id: ConnectionID) -> Int {
        streams[id]?.outstandingBytes ?? 0
    }

    var healthObject: [String: Any] {
        ["limits": limits.object, "metrics": metrics.object]
    }

    /// A complete HTTP response uses FIN and drains the peer close; the backstop bounds a client
    /// that accepts the bytes and never closes its side of the connection.
    func sendResponse(_ bytes: Data, on connection: NWConnection) {
        let id = ObjectIdentifier(connection)
        finishRequest(for: id)
        guard responseDeadlines[id] == nil,
              let cancel = makeDeadline(
                after: TimeInterval(Self.responseCloseGraceSecondLimit),
                action: { [weak self, weak connection] in
                    guard let self, let connection else { return }
                    self.responseTimedOut(id, connection: connection)
                }) else {
            connection.cancel()
            return
        }
        responseDeadlines[id] = cancel
        connection.send(content: bytes, contentContext: .finalMessage, isComplete: true,
                        completion: .contentProcessed { [weak self] error in
            guard let self else { connection.cancel(); return }
            guard error == nil else { self.finishNetworkResponse(id, connection: connection); return }
            self.awaitPeerClose(on: connection)
        })
    }

    private func awaitPeerClose(on connection: NWConnection) {
        let id = ObjectIdentifier(connection)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 << 10) {
            [weak self] _, _, done, error in
            guard let self else { connection.cancel(); return }
            guard error == nil, !done else {
                self.finishNetworkResponse(id, connection: connection); return
            }
            self.awaitPeerClose(on: connection)
        }
    }

    private func sendNext(on id: ConnectionID) {
        guard var stream = streams[id], stream.active == nil, !stream.pending.isEmpty else { return }
        var frame = stream.pending.removeFirst()
        let sequence = frame.sequence
        guard let cancel = makeDeadline(after: limits.streamWriteSeconds, action: { [weak self] in
            self?.streamWriteTimedOut(id, sequence: sequence)
        }) else {
            stream.pending.insert(frame, at: 0)
            streams[id] = stream
            evictStream(id, reason: .capacity)
            return
        }
        frame.cancelDeadline = cancel
        stream.active = frame
        let sender = stream.send
        streams[id] = stream
        sender(frame.bytes) { [weak self] error in
            self?.streamWriteCompleted(id, sequence: sequence, error: error)
        }
    }

    private func streamWriteCompleted(_ id: ConnectionID, sequence: UInt64, error: Error?) {
        guard var stream = streams[id], let active = stream.active,
              active.sequence == sequence else { return }
        active.cancelDeadline?()
        metrics.streamBytesCurrent -= active.bytes.count
        stream.active = nil
        streams[id] = stream
        if error != nil {
            evictStream(id, reason: .writeError)
        } else {
            sendNext(on: id)
        }
    }

    private func streamWriteTimedOut(_ id: ConnectionID, sequence: UInt64) {
        guard let stream = streams[id], stream.active?.sequence == sequence else { return }
        evictStream(id, reason: .completionTimeout)
    }

    private func evictStream(_ id: ConnectionID, reason: EvictionReason) {
        guard let stream = streams.removeValue(forKey: id) else { return }
        stream.active?.cancelDeadline?()
        metrics.streamBytesCurrent -= stream.outstandingBytes
        metrics.streamsCurrent -= 1
        metrics.streamEvictions += 1
        switch reason {
        case .capacity: metrics.streamCapacityEvictions += 1
        case .aggregateCapacity: metrics.streamAggregateCapacityEvictions += 1
        case .completionTimeout: metrics.streamTimeoutEvictions += 1
        case .writeError: metrics.streamWriteErrorEvictions += 1
        }
        stream.evict(reason)
    }

    private func slowestStreamDebtOwner(
        replacingBytes: Int, on producer: ConnectionID
    ) -> ConnectionID? {
        var selected: (id: ConnectionID, bytes: Int, sequence: UInt64)?
        for (id, stream) in streams {
            let bytes = stream.outstandingBytes - (id == producer ? replacingBytes : 0)
            guard bytes > 0 else { continue }
            let sequence = stream.active?.sequence ?? UInt64.max
            if selected == nil || bytes > selected!.bytes
                || (bytes == selected!.bytes && sequence < selected!.sequence) {
                selected = (id, bytes, sequence)
            }
        }
        return selected?.id
    }

    private func makeDeadline(after: TimeInterval, action: @escaping () -> Void) -> Cancellation? {
        guard metrics.deadlineTasksCurrent < limits.deadlineTasks else {
            metrics.deadlineTaskRefusals += 1
            return nil
        }
        let lease = DeadlineLease()
        guard let cancelScheduled = schedule(after, { [weak self, lease] in
            guard let self, lease.active else { return }
            lease.active = false
            lease.cancelScheduled = nil
            self.metrics.deadlineTasksCurrent -= 1
            action()
        }) else {
            metrics.deadlineTaskRefusals += 1
            return nil
        }
        lease.cancelScheduled = cancelScheduled
        metrics.deadlineTasksCurrent += 1
        metrics.deadlineTasksPeak = max(metrics.deadlineTasksPeak, metrics.deadlineTasksCurrent)
        return { [weak self, lease] in
            guard lease.active else { return }
            lease.active = false
            let cancel = lease.cancelScheduled
            lease.cancelScheduled = nil
            cancel?()
            self?.metrics.deadlineTasksCurrent -= 1
        }
    }

    private func cancelResponseDeadline(for id: ConnectionID) {
        responseDeadlines.removeValue(forKey: id)?()
    }

    private func finishNetworkResponse(_ id: ConnectionID, connection: NWConnection) {
        cancelResponseDeadline(for: id)
        connection.cancel()
    }

    private func responseTimedOut(_ id: ConnectionID, connection: NWConnection) {
        responseDeadlines.removeValue(forKey: id)
        connection.cancel()
    }
}

enum HTTPStreamWriteError: Error {
    case connectionGone
}

extension HTTPReliability.EvictionReason {
    var logName: String {
        switch self {
        case .capacity: return "capacity"
        case .aggregateCapacity: return "aggregate_capacity"
        case .completionTimeout: return "completion_timeout"
        case .writeError: return "write_error"
        }
    }
}
