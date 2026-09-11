import Foundation

// W2-3 runs the safe-close lifecycle on fake host ports. What is under test is behaviour, not the
// existence of protocols: every fake writes to one ordered journal, so a check reads the order in
// which the lifecycle asked its host for things — the inventory, the quit word, each observation,
// each signal, the close — and whether it refused before any of them. The fourth group holds this
// Mac's composition to the same decisions through the legacy seams, and the fifth holds the Mac
// leaves no lifecycle uses yet to their contracts.

/// One ordered record of everything a lifecycle asked of its host.
private final class HostPortsJournal {
    var events: [String] = []
}

private final class FakeTerminalHost: TerminalHost {
    let journal: HostPortsJournal
    let capabilities: Set<HostCapability>
    /// Consumed one per inventory; the last one keeps answering.
    private var inventories: [TerminalInventory]
    /// What `capture` answers. Every existing scenario leaves this `nil` — the default — and
    /// gets exactly the behaviour it always had.
    private let captureText: String?
    /// What `create` answers. Defaulted so a scenario that does not care about creation does not
    /// have to script one.
    private let createResult: Result<TerminalCreated, TerminalFailure>

    init(journal: HostPortsJournal,
         capabilities: Set<HostCapability> = [.terminalITerm, .terminalTmux],
         inventories: [TerminalInventory],
         captureText: String? = nil,
         createResult: Result<TerminalCreated, TerminalFailure> =
             .success(TerminalCreated(id: "FAKE-CREATED", backend: .iterm,
                                      tty: "/dev/ttys999", attachCommand: nil))) {
        self.journal = journal
        self.capabilities = capabilities
        self.inventories = inventories
        self.captureText = captureText
        self.createResult = createResult
    }

    func inventory() throws -> TerminalInventory {
        journal.events.append("inventory")
        guard let first = inventories.first else {
            var missing = TerminalInventory()
            missing.isComplete = false
            missing.error = "no inventory was scripted"
            return missing
        }
        if inventories.count > 1 { inventories.removeFirst() }
        return first
    }

    func sendLine(_ text: String, to session: TargetSession) throws -> String? {
        try requireBackend(session, "send")
        journal.events.append("send \(text) -> \(session.id)")
        return nil
    }

    func close(_ session: TargetSession) throws -> String? {
        try requireBackend(session, "close")
        journal.events.append("close \(session.id)")
        return nil
    }

    func create(_ request: TerminalCreateRequest) throws -> TerminalCreated {
        journal.events.append("create")
        return try createResult.get()
    }

    func capture(_ session: TargetSession) throws -> String? {
        try requireBackend(session, "capture")
        journal.events.append("capture \(session.id)")
        return captureText
    }

    func reveal(_ session: TargetSession, activate: Bool) throws {
        try requireBackend(session, "reveal")
        journal.events.append("reveal \(session.id) activate=\(activate)")
    }

    func interrupt(_ bytes: [UInt8], to session: TargetSession) throws -> String? {
        try requireBackend(session, "interrupt")
        journal.events.append("interrupt \(bytes) -> \(session.id)")
        return nil
    }

    func resize(_ session: TargetSession, columns: Int, rows: Int) throws {
        try requireBackend(session, "resize")
        journal.events.append("resize \(columns)x\(rows) -> \(session.id)")
    }

    private func requireBackend(_ session: TargetSession, _ operation: String) throws {
        let capability = HostCapability.terminal(session.backend)
        guard capabilities.contains(capability) else {
            throw HostCapabilityUnavailable(capability: capability, operation: operation)
        }
    }
}

private final class FakeProcessHost: ProcessHost {
    let journal: HostPortsJournal
    let capabilities: Set<HostCapability>
    /// Asked once per observation with how many signals have been sent so far, so a scenario can
    /// say "this process until the first signal, that one after it".
    private let reading: (Int) -> TerminalProcessObservation
    private var signalsSent = 0

    init(journal: HostPortsJournal,
         capabilities: Set<HostCapability> = [.processObservation, .processSignal],
         reading: @escaping (Int) -> TerminalProcessObservation) {
        self.journal = journal
        self.capabilities = capabilities
        self.reading = reading
    }

    func observeAssistant(onTTY tty: String) throws -> TerminalProcessObservation {
        guard capabilities.contains(.processObservation) else {
            throw HostCapabilityUnavailable(capability: .processObservation, operation: "observe")
        }
        journal.events.append("observe \(tty)")
        return reading(signalsSent)
    }

    func signal(_ identity: HostProcessIdentity, _ signal: HostProcessSignal) throws {
        guard capabilities.contains(.processSignal) else {
            throw HostCapabilityUnavailable(capability: .processSignal, operation: "signal")
        }
        journal.events.append("signal \(signal) \(identity.pid)")
        signalsSent += 1
    }
}

/// Stores its reading as a monotonic `TimeInterval`, the way the port declares it (W2-3
/// correction, F7); the `Date` in every call site below is only ever a convenient fixed baseline
/// to start counting from, converted once at `init`.
private final class FakeHostClock: HostClock {
    private var current: TimeInterval

    init(_ start: Date) {
        current = start.timeIntervalSince1970
    }

    func monotonicNow() -> TimeInterval {
        current
    }

    func sleep(for seconds: TimeInterval) {
        current += seconds
    }
}

private struct FixedIdentityHost: IdentityHost {
    func newIdentifier() -> String {
        "host-ports-fixed"
    }
}

private func fakePorts(_ terminal: FakeTerminalHost, _ process: FakeProcessHost,
                       _ clock: FakeHostClock) -> HostPorts {
    HostPorts(terminal: terminal, process: process, clock: clock, identity: FixedIdentityHost())
}

/// The in-memory key store already keeps the port's absence contract; saying so lets the same
/// round trip run against it without a Keychain.
extension CloudInMemoryKeyStore: SecretStore {
    var capabilities: Set<HostCapability> { [.secrets] }
}

func runHostPortsTests() {
    group("host ports: the safe-close lifecycle runs on fake ports and closes only on proved absence") {
        let session = TargetSession(backend: .tmux, id: "%70", name: "child", tty: "/dev/ttys070",
                                    windowIndex: 0, tabIndex: 0, assistant: .codex)
        let start = Date(timeIntervalSince1970: 1_800_300_000)
        let running = Assistant.Running(assistant: .codex, pid: 4242, processStart: start)
        var complete = TerminalInventory()
        complete.sessions = [session]

        // Still there for the first look, gone for every look after it.
        let journal = HostPortsJournal()
        var looks = 0
        let leaving = FakeProcessHost(journal: journal, reading: { _ in
            looks += 1
            return TerminalProcessObservation(running: looks == 1 ? running : nil, error: nil)
        })
        let closed = TerminalSafeClose.end(session, ports: fakePorts(
            FakeTerminalHost(journal: journal, inventories: [complete]), leaving,
            FakeHostClock(start)))
        check("a session that leaves after its quit word is closed", closed == nil,
              closed?.message ?? "")
        expect("the host is asked in order: identity, word, wait, identity, proof, close",
               journal.events,
               ["inventory", "send /quit -> %70", "observe /dev/ttys070", "observe /dev/ttys070",
                "inventory", "observe /dev/ttys070", "close %70"])

        // Gone for the wait, back in time for the proof taken just before the close.
        let appearedJournal = HostPortsJournal()
        var appearedLooks = 0
        let returning = FakeProcessHost(journal: appearedJournal, reading: { _ in
            appearedLooks += 1
            return TerminalProcessObservation(running: appearedLooks >= 2 ? running : nil,
                                              error: nil)
        })
        let appeared = TerminalSafeClose.end(session, ports: fakePorts(
            FakeTerminalHost(journal: appearedJournal, inventories: [complete]), returning,
            FakeHostClock(start)))
        expect("an assistant that appears during the quit sequence keeps the tab", appeared,
               TerminalSafeCloseRefusal.refused(
                   "An assistant appeared on /dev/ttys070; the tab was left open."))
        check("so the backend close is never reached",
              !appearedJournal.events.contains("close %70"))

        var partial = complete
        partial.isComplete = false
        partial.error = "tmux list-panes failed"
        let partialJournal = HostPortsJournal()
        let partialRefusal = TerminalSafeClose.end(session, ports: fakePorts(
            FakeTerminalHost(journal: partialJournal, inventories: [partial]),
            FakeProcessHost(journal: partialJournal, reading: { _ in
                TerminalProcessObservation(running: nil, error: nil)
            }),
            FakeHostClock(start)))
        expect("an incomplete fresh inventory refuses with its own reason", partialRefusal,
               TerminalSafeCloseRefusal.refused("tmux list-panes failed"))
        expect("and nothing is typed, observed or closed", partialJournal.events, ["inventory"])
    }

    group("host ports: TERM and KILL follow one fake process and a replacement is never signalled") {
        let session = TargetSession(backend: .iterm, id: "HOST-PORTS-TAB", name: "fake",
                                    tty: "/dev/ttys071", windowIndex: 0, tabIndex: 0,
                                    assistant: .claude)
        let start = Date(timeIntervalSince1970: 1_800_400_000)
        let original = Assistant.Running(assistant: .claude, pid: 51, processStart: start)
        let stillRunning = "The assistant is still running on /dev/ttys071; the tab was left open."

        // One wait on fake ports: `original` until the first signal, `afterSignal` from then on.
        func wait(afterSignal: Assistant.Running?)
            -> (refusal: TerminalSafeCloseRefusal?, signals: [String], waited: TimeInterval) {
            let journal = HostPortsJournal()
            let clock = FakeHostClock(start)
            let process = FakeProcessHost(journal: journal, reading: { signalsSent in
                TerminalProcessObservation(running: signalsSent == 0 ? original : afterSignal,
                                           error: nil)
            })
            let refusal = TerminalSafeClose.waitToBeGone(session, ports: fakePorts(
                FakeTerminalHost(journal: journal, inventories: []), process, clock))
            return (refusal, journal.events.filter { $0.hasPrefix("signal") },
                    clock.monotonicNow() - start.timeIntervalSince1970)
        }

        let stubborn = wait(afterSignal: original)
        expect("a process that never leaves is refused, never closed", stubborn.refusal,
               TerminalSafeCloseRefusal.refused(stillRunning))
        expect("it is asked once and then told once, in that order", stubborn.signals,
               ["signal terminate 51", "signal kill 51"])
        let rungs = TerminalSafeClose.Farewell.polite + TerminalSafeClose.Farewell.afterTerm
            + TerminalSafeClose.Farewell.afterKill
        check("and the refusal waited out every rung on the injected clock",
              stubborn.waited >= rungs, "waited \(stubborn.waited)s of \(rungs)s")

        let replaced = wait(afterSignal: Assistant.Running(assistant: .claude, pid: 99,
                                                           processStart: start))
        expect("a different PID after TERM is refused", replaced.refusal,
               TerminalSafeCloseRefusal.refused(stillRunning))
        expect("and only the original process was ever signalled", replaced.signals,
               ["signal terminate 51"])

        let reused = wait(afterSignal: Assistant.Running(
            assistant: .claude, pid: 51, processStart: start.addingTimeInterval(1)))
        check("the same PID with a new start after TERM is refused", reused.refusal != nil)
        expect("and never reaches KILL", reused.signals, ["signal terminate 51"])

        let gone = wait(afterSignal: nil)
        check("proved absence after TERM completes the wait", gone.refusal == nil,
              gone.refusal?.message ?? "")
        expect("with no KILL", gone.signals, ["signal terminate 51"])

        // A running process whose start cannot be read has no identity to signal.
        let anonymousJournal = HostPortsJournal()
        let anonymous = FakeProcessHost(journal: anonymousJournal, reading: { _ in
            TerminalProcessObservation(running: Assistant.Running(assistant: .claude, pid: 52),
                                       error: nil)
        })
        let unidentified = TerminalSafeClose.waitToBeGone(session, ports: fakePorts(
            FakeTerminalHost(journal: anonymousJournal, inventories: []), anonymous,
            FakeHostClock(start)))
        expect("a running process without a start instant is an incomplete reading", unidentified,
               TerminalSafeCloseRefusal.refused(
                   "Could not verify whether the assistant left the tty."))
        check("and it is never signalled",
              !anonymousJournal.events.contains { $0.hasPrefix("signal") })
    }

    group("host ports: an unsupported capability is refused by type before any terminal effect") {
        let session = TargetSession(backend: .iterm, id: "ITERM-ONLY-TAB", name: "mac tab",
                                    tty: "/dev/ttys072", windowIndex: 0, tabIndex: 0,
                                    assistant: .claude)
        let start = Date(timeIntervalSince1970: 1_800_500_000)
        var inventory = TerminalInventory()
        inventory.sessions = [session]
        let empty = TerminalProcessObservation(running: nil, error: nil)

        // The Ubuntu MVP's terminal shape — tmux and nothing else — asked to end an iTerm2 session.
        let tmuxOnlyJournal = HostPortsJournal()
        let tmuxOnly = fakePorts(
            FakeTerminalHost(journal: tmuxOnlyJournal, capabilities: [.terminalTmux],
                             inventories: [inventory]),
            FakeProcessHost(journal: tmuxOnlyJournal, reading: { _ in empty }),
            FakeHostClock(start))
        let backendRefusal = TerminalSafeClose.end(session, ports: tmuxOnly)
        expect("a missing terminal backend is a typed capability refusal", backendRefusal,
               TerminalSafeCloseRefusal.capabilityUnavailable(
                   HostCapabilityUnavailable(capability: .terminalITerm, operation: "end")))
        expect("returned before the host is asked anything", tmuxOnlyJournal.events, [])
        let backendSentence = backendRefusal?.message ?? ""
        check("its sentence carries the stable code and the capability",
              backendSentence.contains("capability_unavailable")
                && backendSentence.contains("terminal.iterm"), backendSentence)
        expect("the code is the one the capability matrix reserves",
               HostCapabilityUnavailable.code, "capability_unavailable")

        // Everything except signalling. The quit word must not be typed into a session this host
        // could not then finish closing, even though this particular session would leave on its own.
        let noSignalJournal = HostPortsJournal()
        let noSignal = fakePorts(
            FakeTerminalHost(journal: noSignalJournal, inventories: [inventory]),
            FakeProcessHost(journal: noSignalJournal, capabilities: [.processObservation],
                            reading: { _ in empty }),
            FakeHostClock(start))
        expect("a host that cannot signal is refused before the quit word",
               TerminalSafeClose.end(session, ports: noSignal),
               TerminalSafeCloseRefusal.capabilityUnavailable(
                   HostCapabilityUnavailable(capability: .processSignal, operation: "end")))
        expect("so nothing was typed, observed or closed", noSignalJournal.events, [])
        let shellClose = TerminalSafeClose.closeIfAssistantGone(session, ports: noSignal)
        check("the requirement is the operation's: a close that signals nothing still runs",
              shellClose == nil, shellClose?.message ?? "")
        expect("and it asks exactly what that close needs", noSignalJournal.events,
               ["inventory", "observe /dev/ttys072", "inventory", "close ITERM-ONLY-TAB"])

        // A composition that left a port out answers by name, never with an empty value that
        // already means something.
        let none = UnsupportedHost()
        func refusedCapability(_ body: () throws -> Void) -> HostCapability? {
            do {
                try body()
            } catch let unavailable as HostCapabilityUnavailable {
                return unavailable.capability
            } catch {
                return nil
            }
            return nil
        }
        let noInventory = refusedCapability { _ = try none.inventory() }
        let noProcesses = refusedCapability { _ = try none.observeAssistant(onTTY: "ttys072") }
        let noFiles = refusedCapability { _ = try none.contents(atPath: "/nonexistent") }
        let noSecrets = refusedCapability { _ = try none.data(for: "account") }
        check("no inventory is not an empty inventory", noInventory == HostCapability.terminalTmux)
        check("no process table is not an absent assistant",
              noProcesses == HostCapability.processObservation)
        check("no file system is not a missing file", noFiles == HostCapability.files)
        check("no secret store is not a secret never stored", noSecrets == HostCapability.secrets)
        check("and it claims no capability at all", none.capabilities.isEmpty)
        let bare = HostPorts(clock: FakeHostClock(start), identity: FixedIdentityHost())
        check("a composition given only a clock and identifiers provides nothing optional",
              HostCapability.allCases.allSatisfy { !bare.provides($0) })
        expect("and a lifecycle on it is refused by name",
               TerminalSafeClose.closeIfAssistantGone(session, ports: bare),
               TerminalSafeCloseRefusal.capabilityUnavailable(
                   HostCapabilityUnavailable(capability: .terminalITerm, operation: "close")))
    }

    group("host ports: the Mac composition keeps every legacy safe-close seam and facade name") {
        defer {
            ITerm.ttyAssistantObservationForTesting = nil
            Targets.terminalCloseForTesting = nil
            Targets.safeCloseInventoryForTesting = nil
            Targets.safeCloseSignalForTesting = nil
            Targets.safeCloseSleepForTesting = nil
            Targets.safeCloseNowForTesting = nil
        }
        let mac = HostPorts.mac
        check("this Mac drives both backends, observes and signals processes, and holds files and secrets",
              HostCapability.allCases.allSatisfy { mac.provides($0) })
        check("the facade's old names are the port types themselves",
              Targets.Snapshot.self == TerminalInventory.self
                && ITerm.TTYAssistantObservation.self == TerminalProcessObservation.self
                && Targets.Farewell.self == TerminalSafeClose.Farewell.self)

        let session = TargetSession(backend: .iterm, id: "MAC-SEAM-TAB", name: "mac",
                                    tty: "/dev/ttys073", windowIndex: 0, tabIndex: 0,
                                    assistant: .claude)
        let start = Date(timeIntervalSince1970: 1_800_600_000)
        let running = Assistant.Running(assistant: .claude, pid: 77, processStart: start)

        // One scenario told twice — once to the legacy seams, once to fake ports: present until
        // TERM, gone after it. The Mac leaves must ask in the order the fakes record.
        var seamEvents: [String] = []
        var seamClock = start
        var signalled = false
        ITerm.ttyAssistantObservationForTesting = { tty in
            seamEvents.append("observe /dev/\(tty)")
            return ITerm.TTYAssistantObservation(running: signalled ? nil : running, error: nil)
        }
        Targets.safeCloseNowForTesting = { seamClock }
        Targets.safeCloseSleepForTesting = { seamClock.addTimeInterval($0) }
        Targets.safeCloseSignalForTesting = { pid, value in
            signalled = true
            let name = value == SIGTERM ? "terminate" : (value == SIGKILL ? "kill" : String(value))
            seamEvents.append("signal \(name) \(pid)")
        }
        let seamWait = Targets.waitToBeGoneForTesting(session)
        let waitJournal = HostPortsJournal()
        let fakeWait = TerminalSafeClose.waitToBeGone(session, ports: fakePorts(
            FakeTerminalHost(journal: waitJournal, inventories: []),
            FakeProcessHost(journal: waitJournal, reading: { signalsSent in
                TerminalProcessObservation(running: signalsSent == 0 ? running : nil, error: nil)
            }),
            FakeHostClock(start)))
        check("through the seams the wait completes", seamWait == nil, seamWait ?? "")
        check("and so does the same scenario on fake ports", fakeWait == nil)
        expect("the Mac process and clock leaves ask in the order the fake ports record",
               seamEvents, waitJournal.events)
        check("with the seam clock, not the wall clock, carrying the wait",
              seamClock.timeIntervalSince(start) >= TerminalSafeClose.Farewell.polite)

        seamEvents = []
        Targets.safeCloseInventoryForTesting = {
            seamEvents.append("inventory")
            var snapshot = Targets.Snapshot()
            snapshot.sessions = [session]
            return snapshot
        }
        ITerm.ttyAssistantObservationForTesting = { tty in
            seamEvents.append("observe /dev/\(tty)")
            return ITerm.TTYAssistantObservation(running: nil, error: nil)
        }
        Targets.terminalCloseForTesting = { closing in
            seamEvents.append("close \(closing.id)")
            return nil
        }
        let seamClose = Targets.closeIfAssistantGone(session)
        var closeInventory = TerminalInventory()
        closeInventory.sessions = [session]
        let closeJournal = HostPortsJournal()
        let fakeClose = TerminalSafeClose.closeIfAssistantGone(session, ports: fakePorts(
            FakeTerminalHost(journal: closeJournal, inventories: [closeInventory]),
            FakeProcessHost(journal: closeJournal, reading: { _ in
                TerminalProcessObservation(running: nil, error: nil)
            }),
            FakeHostClock(start)))
        check("a shell-only close through the seams closes", seamClose == nil, seamClose ?? "")
        check("and so does the same close on fake ports", fakeClose == nil)
        expect("the Mac terminal leaf asks in the same order", seamEvents, closeJournal.events)

        seamEvents = []
        Targets.safeCloseInventoryForTesting = {
            seamEvents.append("inventory")
            var snapshot = Targets.Snapshot()
            snapshot.sessions = [session]
            snapshot.isComplete = false
            snapshot.error = "inventory failed"
            return snapshot
        }
        expect("Targets.end still answers with the lifecycle's sentence", Targets.end(session),
               "inventory failed")
        expect("and stops at the inventory, before any word is typed", seamEvents, ["inventory"])
    }

    group("host ports: the Mac file, identity and secret leaves keep absence and failure apart") {
        let files = MacFileSystemHost()
        let manager = FileManager.default
        let root = manager.temporaryDirectory
            .appendingPathComponent("clawdline-host-ports-\(UUID().uuidString)", isDirectory: true)
        defer { try? manager.removeItem(at: root) }
        func threw(_ body: () throws -> Void) -> Bool {
            do {
                try body()
                return false
            } catch {
                return true
            }
        }
        do {
            try manager.createDirectory(at: root, withIntermediateDirectories: true)
            let path = root.appendingPathComponent("record.json").path
            let absent = try files.contents(atPath: path)
            check("an absent file reads as absence", absent == nil)
            try files.writeAtomically(Data("first".utf8), toPath: path)
            try files.writeAtomically(Data("second".utf8), toPath: path)
            let written = try files.contents(atPath: path)
            expect("an atomic write replaces the whole file", written, Data("second".utf8))
            let directoryRead = threw { _ = try files.contents(atPath: root.path) }
            check("a directory is a failed read, never absence", directoryRead)
            let orphanPath = root.appendingPathComponent("missing/child").path
            let orphanWrite = threw { try files.writeAtomically(Data(), toPath: orphanPath) }
            check("a write into a missing directory fails rather than creating it", orphanWrite)
            try files.removeItem(atPath: path)
            let removed = try files.contents(atPath: path)
            check("a removed file reads as absence", removed == nil)
            let secondRemoval = threw { try files.removeItem(atPath: path) }
            check("removing what is already gone is not an error", !secondRemoval)

            // W2-3 correction, F6 red-proof: a real, deterministic remove failure that is *not*
            // absence — a directory with no write permission — must not be swallowed as success
            // the way the old `try? attributesOfItem` pre-check swallowed it. This is not a mock;
            // it is the same OS the port runs on refusing the unlink for a documented reason.
            let lockedDir = root.appendingPathComponent("locked", isDirectory: true)
            try manager.createDirectory(at: lockedDir, withIntermediateDirectories: true)
            let lockedPath = lockedDir.appendingPathComponent("guarded.json").path
            try files.writeAtomically(Data("kept".utf8), toPath: lockedPath)
            try manager.setAttributes([.posixPermissions: 0o555], ofItemAtPath: lockedDir.path)
            let deniedRemoval = threw { try files.removeItem(atPath: lockedPath) }
            try manager.setAttributes([.posixPermissions: 0o755], ofItemAtPath: lockedDir.path)
            check("a remove that fails for a reason other than absence is not swallowed",
                  deniedRemoval)
            let stillThere = try files.contents(atPath: lockedPath)
            expect("and the file it could not remove is still there", stillThere,
                   Data("kept".utf8))
        } catch {
            check("the Mac file system leaf completed its fixture", false, "\(error)")
        }

        let identity = MacIdentityHost()
        let first = identity.newIdentifier()
        let second = identity.newIdentifier()
        check("each identifier is fresh", first != second)
        check("and spelled the way task ids are spelled",
              first == first.lowercased() && UUID(uuidString: first) != nil, first)

        check("this Mac's secret leaf is the login Keychain store",
              HostPorts.mac.secrets is CloudKeychainStore)
        let secrets: any SecretStore = CloudInMemoryKeyStore()
        do {
            let missing = try secrets.data(for: "host-ports")
            try secrets.set(Data("secret".utf8), for: "host-ports")
            let stored = try secrets.data(for: "host-ports")
            try secrets.remove("host-ports")
            let removed = try secrets.data(for: "host-ports")
            check("a secret store keeps the port's absence contract across a round trip",
                  missing == nil && stored == Data("secret".utf8) && removed == nil)
            check("and says it provides secrets", secrets.capabilities == [.secrets])
        } catch {
            check("the in-memory secret store completed its fixture", false, "\(error)")
        }
    }

    group("host ports: W2-3 correction F3 — loadOrCreate and rotate are closed against real races") {
        // A real race, not a scripted one: `DispatchQueue.concurrentPerform` blocks until every
        // iteration has run, so both phases below are genuine concurrent threads racing the same
        // account through the coordinator `CloudInMemoryKeyStore.loadOrCreate`/`rotate` share
        // with the Keychain-backed conformance in `Sources/MacHostAdapters.swift`. A caller that
        // composed this out of `data(for:)` then `set(_:for:)` would let more than one of these
        // racers see absence and each invent its own secret.
        let secrets = CloudInMemoryKeyStore()
        let attempts = 32
        let account = "race-account"

        var createCalls = 0
        let createLock = NSLock()
        var loadOrCreateResults = [Data?](repeating: nil, count: attempts)
        DispatchQueue.concurrentPerform(iterations: attempts) { index in
            loadOrCreateResults[index] = try? secrets.loadOrCreate(account) {
                createLock.lock()
                createCalls += 1
                createLock.unlock()
                return Data("created-\(UUID().uuidString)".utf8)
            }
        }
        check("create() ran exactly once across \(attempts) racing loadOrCreate callers",
              createCalls == 1, "create ran \(createCalls) time(s)")
        let distinctCreated = Set(loadOrCreateResults.compactMap { $0 })
        check("every racing caller received the one value that was created",
              distinctCreated.count == 1, "saw \(distinctCreated.count) distinct value(s)")

        var rotateCalls = 0
        let rotateLock = NSLock()
        var seenPrevious = [Data?](repeating: nil, count: attempts)
        var rotateResults = [Data?](repeating: nil, count: attempts)
        DispatchQueue.concurrentPerform(iterations: attempts) { index in
            rotateResults[index] = try? secrets.rotate(account) { previous in
                rotateLock.lock()
                rotateCalls += 1
                let call = rotateCalls
                rotateLock.unlock()
                seenPrevious[index] = previous
                return Data("rotated-\(call)-\(UUID().uuidString)".utf8)
            }
        }
        check("replace ran once for every one of \(attempts) racing rotate callers, none lost",
              rotateCalls == attempts, "replace ran \(rotateCalls) time(s)")
        check("every rotate saw a real previous value — loadOrCreate's write was never invisible "
              + "to a racing rotate and no rotate raced ahead of another's write",
              seenPrevious.allSatisfy { $0 != nil })
        let finalValue = try? secrets.data(for: account)
        let allRotateReturns = Set(rotateResults.compactMap { $0 })
        check("the store's final value is exactly one racer's own return, not a torn write",
              finalValue != nil && allRotateReturns.contains(finalValue!),
              String(describing: finalValue))
    }

    group("host ports: W2-3 correction F1 — create/capture/reveal/interrupt on fake ports") {
        let journal = HostPortsJournal()
        let session = TargetSession(backend: .tmux, id: "%80", name: "expanded",
                                    tty: "/dev/ttys080", windowIndex: 0, tabIndex: 0,
                                    assistant: .claude)
        let created = TerminalCreated(id: "%81", backend: .tmux, tty: "/dev/ttys081",
                                      attachCommand: "tmux attach -t clawdline")
        let terminal = FakeTerminalHost(journal: journal, inventories: [],
                                        captureText: "❯ ready", createResult: .success(created))

        let capture = try? terminal.capture(session)
        expect("capture returns the scripted screen", capture ?? "", "❯ ready")
        expect("capture is journalled against the session it read", journal.events,
               ["capture %80"])

        journal.events = []
        let madeSession = try? terminal.create(.tmuxDetachedSession(cwd: "/tmp", command: "claude"))
        expect("create returns exactly what was scripted", madeSession, created)
        expect("create is journalled", journal.events, ["create"])

        journal.events = []
        var revealThrew = false
        do { try terminal.reveal(session, activate: false) } catch { revealThrew = true }
        check("reveal does not throw when the fake supports the session's backend", !revealThrew)
        expect("reveal is journalled with its activate flag", journal.events,
               ["reveal %80 activate=false"])

        journal.events = []
        let interrupted = try? terminal.interrupt([0x03], to: session)
        check("interrupt delivers without error", interrupted == nil)
        expect("interrupt is journalled with the bytes it sent", journal.events,
               ["interrupt [3] -> %80"])

        journal.events = []
        var resizeThrew = false
        do { try terminal.resize(session, columns: 90, rows: 25) } catch { resizeThrew = true }
        check("resize does not throw when the fake supports the session's backend", !resizeThrew)
        expect("resize is journalled with bounded dimensions", journal.events,
               ["resize 90x25 -> %80"])

        // The capability gate applies to the new leaves the same way it already does to
        // sendLine/close: a backend this fake does not support is refused, not silently ignored.
        let tmuxOnlyJournal = HostPortsJournal()
        let tmuxOnly = FakeTerminalHost(journal: tmuxOnlyJournal, capabilities: [.terminalTmux],
                                        inventories: [])
        let itermSession = TargetSession(backend: .iterm, id: "ITERM-ONLY", name: "x",
                                         tty: "/dev/ttys081", windowIndex: 0, tabIndex: 0,
                                         assistant: nil)
        func refusedCapability(_ body: () throws -> Void) -> HostCapability? {
            do { try body() } catch let unavailable as HostCapabilityUnavailable {
                return unavailable.capability
            } catch { return nil }
            return nil
        }
        check("capture refuses a backend this host does not provide",
              refusedCapability { _ = try tmuxOnly.capture(itermSession) } == .terminalITerm)
        check("reveal refuses a backend this host does not provide",
              refusedCapability { try tmuxOnly.reveal(itermSession, activate: true) }
                  == .terminalITerm)
        check("interrupt refuses a backend this host does not provide",
              refusedCapability { _ = try tmuxOnly.interrupt([0x03], to: itermSession) }
                  == .terminalITerm)
        check("resize refuses a backend this host does not provide",
              refusedCapability { try tmuxOnly.resize(itermSession, columns: 90, rows: 25) }
                  == .terminalITerm)

        // UnsupportedHost's answer to every one of the four new leaves is the same typed refusal
        // every other port operation already gives, never an empty value that means something
        // else on its own (the doc on HostPortsTests's earlier "unsupported capability" group
        // covers inventory/observeAssistant/contents/data; this closes the four this task added).
        let none = UnsupportedHost()
        func refused(_ body: () throws -> Void) -> Bool {
            refusedCapability(body) != nil
        }
        check("create refuses by type on a host with no terminal capability",
              refused { _ = try none.create(.tmuxWindow(cwd: "/tmp", command: "claude")) })
        check("capture refuses by type on a host with no terminal capability",
              refused { _ = try none.capture(session) })
        check("reveal refuses by type on a host with no terminal capability",
              refused { try none.reveal(session, activate: true) })
        check("interrupt refuses by type on a host with no terminal capability",
              refused { _ = try none.interrupt([0x03], to: session) })
        check("resize refuses by type on a host with no terminal capability",
              refused { try none.resize(session, columns: 90, rows: 25) })
    }

    group("host ports: W4-1 start and menu admission are shared Application policy") {
        let request = ProviderLaunchRequest(
            commandID: "mac-policy-1", projectRoot: "/tmp/project", assistant: .codex,
            model: "gpt-5", reasoningEffort: .high, permission: .edits,
            additionalDirectory: "/tmp/task", terminalMode: .tmuxWindow)
        let plan = try? SessionLaunchPolicy.admit(
            request, terminalCapabilities: [.terminalTmux]).get()
        expect("the shared launch policy keeps structured argv for a host adapter",
               plan?.arguments ?? [],
               ["--model", "gpt-5", "--config", "model_reasoning_effort=high",
                "--add-dir", "/tmp/task", "--ask-for-approval", "on-request",
                "--sandbox", "workspace-write"])
        expect("the same plan preserves the Mac shell facade's established line",
               plan?.shellLine,
               "cd '/tmp/project' && env -u CODEX_THREAD_ID -u CODEX_SESSION_ID "
                + "-u CODEX_SANDBOX -u CODEX_SANDBOX_NETWORK_DISABLED codex --model gpt-5 "
                + "--config model_reasoning_effort=high --add-dir /tmp/task "
                + "--ask-for-approval on-request --sandbox workspace-write")

        let unavailable = SessionLaunchPolicy.admit(
            request, terminalCapabilities: [])
        if case .failure(.capabilityUnavailable(let refusal)) = unavailable {
            expect("missing tmux is the shared typed capability refusal",
                   refusal.capability, .terminalTmux)
        } else {
            check("missing tmux is the shared typed capability refusal", false)
        }
        check("an arbitrary escape sequence never becomes a terminal effect",
              (try? TerminalMenuAnswerPolicy.admit(
                [0x1b, 0x5b, 0x41], backend: .tmux,
                terminalCapabilities: [.terminalTmux]).get()) == nil)
    }
}
