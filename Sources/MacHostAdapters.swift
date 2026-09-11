import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

// W2-3: this Mac's leaves for the ports declared in `Sources/HostPorts.swift`.
//
// Each leaf is what the safe-close lifecycle used to call directly, moved behind a contract and
// nothing more: the same exact-tty `ps` read, the same `kill`, the same osascript and tmux
// commands, the same monotonic clock. **Every leaf here does only the real effect** — the three
// legacy `Targets.*ForTesting` seams these leaves used to read at call time (terminal close,
// process signal, clock/sleep) moved to `Sources/Targets.swift` as of the W2-3 correction (F5):
// `Targets.closeIfAssistantGone` and `Targets.waitToBeGoneForTesting`, the two entry points
// `Tests/MascotTests.swift` drives through them, now run on a seam-adapting composition declared
// there, and `HostPorts.mac` — what `Targets.end` and every other caller gets — reads no
// process-global mutable test state at all. `Tests/SessionCloseAndQuotaTests.swift` and
// `Tests/BackgroundAndStorageTests.swift` were already outside this file's seam reads: they drive
// `Targets.safeCloseInventoryForTesting`, which `Targets.safeCloseInventory()` — a facade
// function, not a leaf here — has always owned. New tests inject fake ports
// (`Tests/HostPortsTests.swift`) rather than adding a seam anywhere in this file.

extension HostPorts {
    /// This Mac: iTerm2 and tmux, `ps` and `kill`, the file system, the login Keychain and the
    /// monotonic clock. Built per use; every leaf is stateless and reads its seam when it is
    /// called.
    static var mac: HostPorts {
        HostPorts(terminal: MacTerminalHost(), process: MacProcessHost(),
                  files: MacFileSystemHost(), secrets: CloudKeychainStore(),
                  clock: MacHostClock(), identity: MacIdentityHost())
    }
}

/// iTerm2 through its Apple Event bridge and tmux through its command line, chosen by the
/// session's own backend.
///
/// W2-3 correction, F1: `sendLine`/`capture`/`reveal` now dispatch to `ITerm`/`Tmux` directly,
/// the way `close` already did — `Sources/Targets.swift`'s `send(_:to:)`, `visibleScreen(of:)`
/// and `reveal(_:activate:)` call down into this leaf instead of the other way around, so those
/// three real production paths now run through the port rather than around it. `create` and
/// `interrupt` are new leaves with no facade caller migrated onto them yet — see the note on
/// ``TerminalHost`` for exactly what that means and what remains.
struct MacTerminalHost: TerminalHost {
    var capabilities: Set<HostCapability> { [.terminalITerm, .terminalTmux] }

    /// The combined iTerm2/tmux inventory, taken fresh inside the terminal broker.
    func inventory() throws -> TerminalInventory {
        Targets.safeCloseInventory()
    }

    /// A bracketed paste and Return, the way every other line reaches an assistant.
    func sendLine(_ text: String, to session: TargetSession) throws -> String? {
        switch session.backend {
        case .iterm: return ITerm.send(text, to: session.id)
        case .tmux: return Tmux.send(text, to: session.id)
        }
    }

    func close(_ session: TargetSession) throws -> String? {
        switch session.backend {
        case .iterm: return ITerm.close(session.id)
        case .tmux: return Tmux.close(session.id)
        }
    }

    /// Open a new iTerm2 tab, tmux window, or detached tmux session. `StartPoints.start` retains
    /// the admission/refusal policy and delegates only the admitted platform effect here.
    func create(_ request: TerminalCreateRequest) throws -> TerminalCreated {
        switch request {
        case .iTermTab(let line):
            switch ITerm.newTabResult(line: line) {
            case .success(let made):
                return TerminalCreated(id: made.id, backend: .iterm, tty: made.tty,
                                       attachCommand: nil)
            case .failure(let failure):
                throw failure
            }
        case .tmuxWindow(let cwd, let command):
            switch Tmux.newWindowResult(cwd: cwd, command: command) {
            case .success(let pane):
                return TerminalCreated(id: pane, backend: .tmux, tty: nil, attachCommand: nil)
            case .failure(let failure):
                throw failure
            }
        case .tmuxDetachedSession(let cwd, let command):
            switch Tmux.newSessionResult(cwd: cwd, command: command) {
            case .success(let pane):
                return TerminalCreated(id: pane, backend: .tmux, tty: nil,
                                       attachCommand: Tmux.attachCommand)
            case .failure(let failure):
                throw failure
            }
        case .managedProvider(let plan):
            switch plan.terminalMode {
            case .iTermTab:
                switch ITerm.newTabResult(line: plan.shellLine) {
                case .success(let made):
                    return TerminalCreated(id: made.id, backend: .iterm, tty: made.tty,
                                           attachCommand: nil)
                case .failure(let failure): throw failure
                }
            case .tmuxWindow:
                switch Tmux.newWindowResult(cwd: plan.projectRoot, command: plan.shellCommand) {
                case .success(let pane):
                    return TerminalCreated(id: pane, backend: .tmux, tty: nil,
                                           attachCommand: nil)
                case .failure(let failure): throw failure
                }
            case .tmuxDetachedSession:
                switch Tmux.newSessionResult(cwd: plan.projectRoot, command: plan.shellCommand) {
                case .success(let pane):
                    return TerminalCreated(id: pane, backend: .tmux, tty: nil,
                                           attachCommand: Tmux.attachCommand)
                case .failure(let failure): throw failure
                }
            }
        }
    }

    /// The visible screen, no history — the no-scrollback reading both backends already had a
    /// name for; a backend that keeps more is not asked for it here.
    func capture(_ session: TargetSession) throws -> String? {
        switch session.backend {
        case .iterm: return ITerm.capture(session.id)
        case .tmux: return Tmux.capture(session.id, scrollback: 0)
        }
    }

    func reveal(_ session: TargetSession, activate: Bool) throws {
        let failure: TerminalFailure?
        switch session.backend {
        case .iterm: failure = ITerm.reveal(session.id, activate: activate)
        case .tmux: failure = Tmux.reveal(session.id, activate: activate)
        }
        if let failure { throw failure }
    }

    /// The same raw-byte channel `Targets.answer`'s menu/back-tab keystrokes already use on this
    /// Mac (`ITerm.keystroke`/`Tmux.keystroke`), exposed as its own port leaf so a caller that
    /// only needs to send a control byte — the interrupt byte among them — does not have to go
    /// through the menu-answering facade to reach it.
    func interrupt(_ bytes: [UInt8], to session: TargetSession) throws -> String? {
        switch session.backend {
        case .iterm: return ITerm.keystroke(bytes, to: session.id)
        case .tmux: return Tmux.keystroke(bytes, to: session.id)
        }
    }

    func resize(_ session: TargetSession, columns: Int, rows: Int) throws {
        guard session.backend == .tmux else {
            throw HostCapabilityUnavailable(capability: .terminalITerm, operation: "resize")
        }
        if let failure = Tmux.resize(session.id, columns: columns, rows: rows) {
            throw TerminalFailure(kind: .io, message: failure)
        }
    }
}

/// The exact-tty `ps` reading and `kill(2)`. Only the real effect; see the note at the top of
/// this file for where the legacy `safeCloseSignalForTesting` seam went (F5).
struct MacProcessHost: ProcessHost {
    var capabilities: Set<HostCapability> { [.processObservation, .processSignal] }

    func observeAssistant(onTTY tty: String) throws -> TerminalProcessObservation {
        ITerm.assistantObservation(onTTY: tty)
    }

    // W2-3 correction, F4: re-read the pid's own start time immediately before signalling it and
    // compare against the identity the caller proved, rather than trusting a comment that the
    // caller already checked. `ITerm.processStart(ofPID:)` is the same `ps -o lstart=` reading
    // `identity.processStart` was itself built from (via the exact-tty observation), so the two
    // are comparable at the same precision. This narrows the race to the gap between this read
    // and `kill(2)` — it does not close it; see ``HostProcessIdentityChanged`` for why Mac cannot.
    func signal(_ identity: HostProcessIdentity, _ signal: HostProcessSignal) throws {
        guard let currentStart = ITerm.processStart(ofPID: identity.pid) else {
            throw HostProcessIdentityChanged(identity: identity, reason: .processGone)
        }
        guard currentStart == identity.processStart else {
            throw HostProcessIdentityChanged(identity: identity, reason: .pidReused)
        }
        kill(identity.pid, signal == .terminate ? SIGTERM : SIGKILL)
    }
}

/// The real monotonic clock (``ProcessInfo/systemUptime``, unaffected by wall-clock adjustment —
/// see the F7 note on ``HostClock``) and `Thread.sleep`. Only the real effect; see the note at
/// the top of this file for where the legacy `safeCloseNowForTesting`/`safeCloseSleepForTesting`
/// seams went (F5).
struct MacHostClock: HostClock {
    func monotonicNow() -> TimeInterval {
        ProcessInfo.processInfo.systemUptime
    }

    func sleep(for seconds: TimeInterval) {
        Thread.sleep(forTimeInterval: seconds)
    }
}

/// `FileManager` and `Data`'s atomic write.
struct MacFileSystemHost: FileSystemHost {
    var capabilities: Set<HostCapability> { [.files] }

    func contents(atPath path: String) throws -> Data? {
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory) else {
            return nil
        }
        // Something is there, so this can no longer be answered with absence.
        guard !isDirectory.boolValue else {
            throw CocoaError(.fileReadUnknown, userInfo: [NSFilePathErrorKey: path])
        }
        return try Data(contentsOf: URL(fileURLWithPath: path))
    }

    func writeAtomically(_ data: Data, toPath path: String) throws {
        try data.write(to: URL(fileURLWithPath: path), options: .atomic)
    }

    // W2-3 correction, F6: this used to check existence first with `try? attributesOfItem`,
    // which throws away *why* the check failed — permission, I/O, a path Foundation cannot
    // encode, all of it became the same `nil` as "nothing is there," and this then reported the
    // remove as having succeeded while the file was still on disk. The pre-check was also its
    // own race: proving the file present a moment before removing it does not prove it is still
    // present when the remove itself runs.
    //
    // So there is no pre-check. `removeItem` is attempted directly, and only the one error that
    // *means* "the state asked for is already the state reached" — no such file — is mapped to
    // success. Every other error, including one Foundation raised from a metadata read of its
    // own before ever reaching the filesystem call, is rethrown exactly as it was thrown. A
    // dangling symlink is still removed the same way it always was: `removeItem` unlinks the
    // path itself and never needs to resolve what the link points at.
    func removeItem(atPath path: String) throws {
        do {
            try FileManager.default.removeItem(atPath: path)
        } catch let error as NSError where Self.namesAbsence(error) {
            return
        }
    }

    /// Whether `error` is Foundation's way of saying nothing was at `path` — as opposed to any
    /// other reason the remove did not happen. Checked by code, not merely by message text: the
    /// Cocoa domain is what `FileManager` itself throws, and the POSIX domain (direct or nested
    /// under `NSUnderlyingErrorKey`) is what a lower-level failure surfaces as.
    static func namesAbsence(_ error: NSError) -> Bool {
        if error.domain == NSCocoaErrorDomain, error.code == NSFileNoSuchFileError { return true }
        if error.domain == NSPOSIXErrorDomain, error.code == Int(ENOENT) { return true }
        if let underlying = error.userInfo[NSUnderlyingErrorKey] as? NSError {
            return namesAbsence(underlying)
        }
        return false
    }
}

/// Lowercased UUIDs, the spelling task ids already use.
struct MacIdentityHost: IdentityHost {
    func newIdentifier() -> String {
        UUID().uuidString.lowercased()
    }
}

/// The login Keychain is this Mac's secret leaf. `CloudKeychainStore` keeps its home in
/// `Sources/CloudKeys.swift`, which `Tests/keychain-rebuild-focused.mjs` compiles on its own, so
/// the conformance is declared here beside the other leaves rather than there.
extension CloudKeychainStore: SecretStore {
    var capabilities: Set<HostCapability> { [.secrets] }

    // W2-3 correction, F3: both operations run inside `coordinator.withCriticalRegion`, the same
    // closed region `CloudKeys.loadOrCreateDeviceKeyPair()`/`loadOrCreateMasterSecret()`/
    // `restoreMasterSecret(from:)` already use, so a caller reaching a device key or the master
    // secret through this port and a caller reaching it through `CloudKeys` cannot interleave a
    // read and a write and each walk away with a different secret for the same account.

    func loadOrCreate(_ account: String, create: @Sendable () throws -> Data) throws -> Data {
        try coordinator.withCriticalRegion {
            if let existing = try data(for: account) { return existing }
            let created = try create()
            try set(created, for: account)
            return created
        }
    }

    func rotate(_ account: String, replace: @Sendable (Data?) throws -> Data) throws -> Data {
        try coordinator.withCriticalRegion {
            let replacement = try replace(try data(for: account))
            try set(replacement, for: account)
            return replacement
        }
    }
}
