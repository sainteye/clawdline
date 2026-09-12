import Foundation
import ClawdlineApplication

struct LinuxLifecycleReceipt: Codable, Equatable {
    let progress: TerminalEffectProgress
    let sessionID: String?
    let tty: String?
    let attachCommand: String?
    let output: String?
}

/// Typed failure receipt for a lifecycle that may have crossed a terminal effect boundary.
/// Callers can distinguish safe replay (`no_effect`/`compensated`) from a pasted command or an
/// unknown effect that requires reconciliation.
struct LinuxLifecycleFailure: Error, Codable, Equatable {
    let code: String
    let message: String
    let progress: TerminalEffectProgress
    let certainty: LinuxEffectCertainty
    let lastConfirmedEffect: LinuxEffectCheckpoint
    let sessionID: String?
    let tty: String?
    let reconciliationRequired: Bool
}

/// W4-1's composition boundary. It owns one scheduling instance and injects that exact instance
/// into every lifecycle entry point; terminal and process adapters own effects, never policy.
final class LinuxProviderRuntime {
    let layout: LinuxRuntimeLayout
    let projectPolicy: ProjectRootPolicy
    let projectInspector: LinuxProjectRootInspector
    let terminal: LinuxTmuxTerminalHost
    let process: LinuxProcessHost
    let files: LinuxContainedFileSystemHost
    let secrets: LinuxProtectedFileSecretStore
    let ports: HostPorts
    let scheduling: TerminalCommandScheduler
    let compositionReceipt: LinuxRuntimeCompositionReceipt
    var failPostCreateReadinessForTesting = false

    private init(layout: LinuxRuntimeLayout, projectPolicy: ProjectRootPolicy,
                 projectInspector: LinuxProjectRootInspector,
                 terminal: LinuxTmuxTerminalHost, process: LinuxProcessHost,
                 files: LinuxContainedFileSystemHost, secrets: LinuxProtectedFileSecretStore,
                 ports: HostPorts, scheduling: TerminalCommandScheduler,
                 compositionReceipt: LinuxRuntimeCompositionReceipt) {
        self.layout = layout
        self.projectPolicy = projectPolicy
        self.projectInspector = projectInspector
        self.terminal = terminal
        self.process = process
        self.files = files
        self.secrets = secrets
        self.ports = ports
        self.scheduling = scheduling
        self.compositionReceipt = compositionReceipt
    }

    static func compose(configuration: LinuxDaemonConfiguration,
                        sandboxExecutablePath suppliedSandboxExecutable: String? = nil)
        throws -> LinuxProviderRuntime {
        let runtimeConfiguration = configuration.runtime
        let layout = try LinuxRuntimeLayout.prepare(
            stateDirectory: configuration.stateDirectory,
            runtimeDirectory: configuration.runtimeDirectory,
            expectedUID: runtimeConfiguration?.uid,
            expectedGID: runtimeConfiguration?.gid)
        let configuredRoots = runtimeConfiguration?.projectRoots ?? [layout.projects]
        let reservedRoots = [layout.state, layout.home, layout.runtime,
                             layout.secrets, layout.temporary]
        guard !configuredRoots.contains(where: { candidate in
            reservedRoots.contains(where: { ProjectRootPolicy.pathsOverlap(candidate, $0) })
        }) else {
            throw LinuxRuntimeFailure(
                code: .unsafePath,
                message: "A project root must not contain or be contained by daemon control state.")
        }
        let inspector = LinuxProjectRootInspector()
        let rootPolicy = ProjectRootPolicy(allowedCanonicalRoots: Set(configuredRoots),
                                           expectedUID: layout.uid, expectedGID: layout.gid)
        var roots: [CanonicalProjectRoot] = []
        for path in configuredRoots {
            switch rootPolicy.admit(path, inspector: inspector) {
            case .success(let root): roots.append(root)
            case .failure(let refusal):
                throw LinuxRuntimeFailure(code: .unsafePath, message: refusal.message)
            }
        }

        let providerEnvironment: [String: String]
        switch ProviderEnvironmentPolicy.closed(home: layout.home,
                                                temporaryDirectory: layout.temporary) {
        case .success(let value): providerEnvironment = value
        case .failure(let failure):
            throw LinuxRuntimeFailure(code: .unsafePath, message: failure.message)
        }
        var hostEnvironment = providerEnvironment
        hostEnvironment["TMUX_TMPDIR"] = layout.runtime
        let tmuxPath = runtimeConfiguration?.tmuxExecutable ?? "/usr/bin/tmux"
        let providerPaths: [Assistant: String] = [
            .claude: runtimeConfiguration?.providers.claude ?? "/usr/bin/claude",
            .codex: runtimeConfiguration?.providers.codex ?? "/usr/bin/codex",
        ]
        #if os(Linux)
        let sandboxPath = try suppliedSandboxExecutable
            ?? FileManager.default.destinationOfSymbolicLink(atPath: "/proc/self/exe")
        #else
        let sandboxPath = suppliedSandboxExecutable ?? CommandLine.arguments[0]
        #endif
        guard LinuxProviderSandbox.isSupported else {
            throw LinuxRuntimeFailure(
                code: .capabilityUnavailable,
                message: "This kernel cannot enforce the required provider containment profile.")
        }
        let tmuxExecutable = try LinuxExecutableDescriptor.open(
            path: tmuxPath, serviceUID: layout.uid, reservedRoots: reservedRoots)
        var providers: [Assistant: LinuxExecutableDescriptor] = [:]
        for (assistant, path) in providerPaths {
            providers[assistant] = try LinuxExecutableDescriptor.open(
                path: path, serviceUID: layout.uid, reservedRoots: reservedRoots)
        }
        let sandboxExecutable = try LinuxExecutableDescriptor.open(
            path: sandboxPath, serviceUID: layout.uid, reservedRoots: reservedRoots)
        let limits = TerminalWorkLimits.production
        let terminal = LinuxTmuxTerminalHost(
            tmuxExecutable: tmuxExecutable,
            socketPath: layout.runtime + "/clawdline.sock",
            providerExecutables: providers,
            sandboxExecutable: sandboxExecutable,
            writableRoots: roots.map(\.path) + [layout.home, layout.temporary],
            providerEnvironment: providerEnvironment,
            hostEnvironment: hostEnvironment,
            limits: limits)
        let process = LinuxProcessHost(
            serviceUID: layout.uid, serviceGID: layout.gid,
            supplementaryGroups: try LinuxProcfs.currentSupplementaryGroups())
        let files = LinuxContainedFileSystemHost(roots: roots)
        let secretRoot = CanonicalProjectRoot(path: layout.secrets,
                                              ownerUID: layout.uid, ownerGID: layout.gid)
        let secrets = LinuxProtectedFileSecretStore(root: secretRoot)
        let ports = HostPorts(terminal: terminal, process: process, files: files,
                              secrets: secrets, clock: LinuxHostClock(), identity: LinuxIdentityHost())
        let scheduling = TerminalCommandScheduler(
            label: "app.clawdline.terminal-command", limits: limits)
        let receipt = LinuxRuntimeCompositionReceipt(
            configuration: "runtime_adapters_configured_provider_auth_pending",
            uid: layout.uid, gid: layout.gid, umask: "0077",
            homeDirectory: layout.home, runtimeDirectory: layout.runtime,
            projectRoots: roots.map(\.path).sorted(),
            terminalLimits: LinuxTerminalLimitsReceipt(
                total: limits.total, perChannel: limits.perChannel,
                maximumInputBytes: limits.maximumInputBytes,
                maximumInventory: limits.maximumInventory),
            identity: .configured())
        return LinuxProviderRuntime(
            layout: layout, projectPolicy: rootPolicy, projectInspector: inspector,
            terminal: terminal, process: process, files: files, secrets: secrets,
            ports: ports, scheduling: scheduling, compositionReceipt: receipt)
    }

    func create(commandID: String, projectRoot: String, assistant: Assistant,
                model: String? = nil, reasoningEffort: ReasoningEffort? = nil,
                permission: Permission = .ask, additionalDirectory: String? = nil,
                resumeSessionID: String? = nil) throws -> LinuxLifecycleReceipt {
        let canonical: CanonicalProjectRoot
        switch projectPolicy.admit(projectRoot, inspector: projectInspector) {
        case .success(let value): canonical = value
        case .failure(let refusal): throw refusal
        }
        if let additionalDirectory {
            guard case .success = projectPolicy.admit(
                additionalDirectory, inspector: projectInspector) else {
                throw ProjectRootRefusal(
                    code: .notAllowed,
                    message: "The additional directory is not one of the admitted canonical roots.")
            }
        }
        let plan: ProviderLaunchPlan
        switch SessionLaunchPolicy.admit(ProviderLaunchRequest(
            commandID: commandID, projectRoot: canonical.path, assistant: assistant,
            model: model, reasoningEffort: reasoningEffort, permission: permission,
            additionalDirectory: additionalDirectory, resumeSessionID: resumeSessionID,
            terminalMode: .tmuxDetachedSession),
            terminalCapabilities: terminal.capabilities) {
        case .success(let value): plan = value
        case .failure(let refusal): throw refusal
        }

        return try withReservation(commandID: commandID, channel: canonical.path,
                                   operation: .create) { progress in
            let created: TerminalCreated
            do {
                created = try terminal.create(.managedProvider(plan))
            } catch let effect as LinuxTerminalEffectFailure {
                throw self.lifecycleFailure(
                    effect.failure, progress: progress, certainty: effect.certainty,
                    checkpoint: effect.checkpoint, sessionID: effect.sessionID, tty: effect.tty)
            } catch {
                throw self.lifecycleFailure(error, progress: progress, certainty: .unknown)
            }
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)

            var observed = false
            let readinessDeadline = ports.clock.monotonicNow() + 2
            repeat {
                do {
                    if !self.failPostCreateReadinessForTesting,
                       let session = try self.session(id: created.id),
                       session.tty == created.tty, session.assistant == assistant,
                       let expected = created.processIdentity {
                        let observation = try process.observeAssistant(onTTY: session.tty)
                        observed = observation.isComplete && observation.isPresent
                            && observation.assistant == assistant
                            && observation.processIdentity == expected
                    }
                } catch {
                    observed = false
                }
                if !observed { ports.clock.sleep(for: 0.05) }
            } while !observed && ports.clock.monotonicNow() < readinessDeadline

            guard observed else {
                let compensated = (try? terminal.compensateCreated(created)) == true
                throw self.lifecycleFailure(
                    LinuxRuntimeFailure(
                        code: .processIdentity,
                        message: "The exact created provider identity was not ready before its bounded deadline."),
                    progress: progress, certainty: compensated ? .compensated : .unknown,
                    checkpoint: .sessionCreated, sessionID: created.id, tty: created.tty)
            }
            try progress.advance(to: .observed)
            return LinuxLifecycleReceipt(progress: progress, sessionID: created.id,
                                         tty: created.tty, attachCommand: created.attachCommand,
                                         output: nil)
        }
    }

    func send(commandID: String, sessionID: String, text: String) throws -> LinuxLifecycleReceipt {
        return try withReservation(commandID: commandID, channel: sessionID, operation: .send) {
            progress in
            let session = try self.requiredSession(sessionID)
            try ProviderLifecyclePolicy.validateInput(Data(text.utf8), limits: scheduling.limits)
            do {
                switch try terminal.sendLineEffect(text, to: session) {
                case .submitted:
                    try progress.advance(to: .executed)
                    try progress.advance(to: .delivered)
                case .pastedNotSubmitted:
                    try progress.advance(to: .executed)
                    try progress.advance(to: .delivered)
                    throw self.lifecycleFailure(
                        LinuxRuntimeFailure(
                            code: .commandFailed,
                            message: "Text reached the pane, but submit did not; do not replay blindly."),
                        progress: progress, certainty: .partial, checkpoint: .textPasted,
                        sessionID: session.id, tty: session.tty)
                }
            } catch let typed as LinuxLifecycleFailure {
                throw typed
            } catch let effect as LinuxTerminalEffectFailure {
                if effect.checkpoint == .textPasted {
                    try? progress.advance(to: .executed)
                    try? progress.advance(to: .delivered)
                }
                throw self.lifecycleFailure(
                    effect.failure, progress: progress, certainty: effect.certainty,
                    checkpoint: effect.checkpoint, sessionID: effect.sessionID, tty: effect.tty)
            } catch {
                throw self.lifecycleFailure(error, progress: progress, certainty: .unknown,
                                            sessionID: session.id, tty: session.tty)
            }
            return LinuxLifecycleReceipt(progress: progress, sessionID: session.id,
                                         tty: session.tty, attachCommand: nil, output: nil)
        }
    }

    func answer(commandID: String, sessionID: String, bytes: [UInt8]) throws -> LinuxLifecycleReceipt {
        return try withReservation(commandID: commandID, channel: sessionID, operation: .interrupt) {
            progress in
            let session = try self.requiredSession(sessionID)
            switch TerminalMenuAnswerPolicy.admit(bytes, backend: session.backend,
                                                  terminalCapabilities: terminal.capabilities) {
            case .failure(let refusal): throw refusal
            case .success: break
            }
            if let refusal = try terminal.interrupt(bytes, to: session) {
                throw LinuxRuntimeFailure(code: .commandFailed, message: refusal)
            }
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)
            return LinuxLifecycleReceipt(progress: progress, sessionID: session.id,
                                         tty: session.tty, attachCommand: nil, output: nil)
        }
    }

    func interrupt(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        return try withReservation(commandID: commandID, channel: sessionID, operation: .interrupt) {
            progress in
            let session = try self.requiredSession(sessionID)
            if let refusal = try terminal.interrupt([0x03], to: session) {
                throw LinuxRuntimeFailure(code: .commandFailed, message: refusal)
            }
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)
            return LinuxLifecycleReceipt(progress: progress, sessionID: session.id,
                                         tty: session.tty, attachCommand: nil, output: nil)
        }
    }

    func observe(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        return try withReservation(commandID: commandID, channel: sessionID, operation: .observe) {
            progress in
            let session = try self.requiredSession(sessionID)
            guard let screen = try terminal.capture(session) else {
                throw LinuxRuntimeFailure(code: .commandFailed, message: "The PTY could not be captured.")
            }
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)
            try progress.advance(to: .observed)
            return LinuxLifecycleReceipt(progress: progress, sessionID: session.id,
                                         tty: session.tty, attachCommand: nil, output: screen)
        }
    }

    func resize(commandID: String, sessionID: String,
                columns: Int, rows: Int) throws -> LinuxLifecycleReceipt {
        return try withReservation(commandID: commandID, channel: sessionID, operation: .resize) {
            progress in
            let session = try self.requiredSession(sessionID)
            try terminal.resize(session, columns: columns, rows: rows)
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)
            try progress.advance(to: .observed)
            return LinuxLifecycleReceipt(progress: progress, sessionID: session.id,
                                         tty: session.tty, attachCommand: nil,
                                         output: "\(columns)x\(rows)")
        }
    }

    func close(commandID: String, sessionID: String) throws -> LinuxLifecycleReceipt {
        return try withReservation(commandID: commandID, channel: sessionID, operation: .close) {
            progress in
            let session = try self.requiredSession(sessionID)
            if let refusal = try terminal.sendLine((session.assistant ?? .claude).quitLine,
                                                   to: session) {
                throw LinuxRuntimeFailure(code: .commandFailed, message: refusal)
            }
            let started = ports.clock.monotonicNow()
            var termed: HostProcessIdentity?
            var killed: HostProcessIdentity?
            while true {
                let observation = try process.observeAssistant(onTTY: session.tty)
                guard observation.isComplete else {
                    throw LinuxRuntimeFailure(code: .processIdentity,
                                              message: observation.error ?? "The provider process could not be observed.")
                }
                let identity = observation.processIdentity
                switch TerminalSafeClose.Farewell.step(
                    elapsed: ports.clock.monotonicNow() - started,
                    identity: identity, termed: termed, killed: killed) {
                case .close: break
                case .refuse:
                    throw LinuxRuntimeFailure(code: .processIdentity,
                                              message: "The provider process group remained after bounded close.")
                case .wait:
                    ports.clock.sleep(for: 0.2)
                    continue
                case .term:
                    guard let identity else {
                        throw LinuxRuntimeFailure(code: .processIdentity,
                                                  message: "The provider identity disappeared before TERM.")
                    }
                    try process.signal(identity, .terminate)
                    termed = identity
                    ports.clock.sleep(for: 0.2)
                    continue
                case .kill:
                    guard let identity, identity == termed else {
                        throw LinuxRuntimeFailure(code: .processIdentity,
                                                  message: "The provider identity changed before KILL.")
                    }
                    try process.signal(identity, .kill)
                    killed = identity
                    ports.clock.sleep(for: 0.2)
                    continue
                }
                break
            }
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)
            if let remaining = try self.session(id: sessionID),
               let refusal = try terminal.close(remaining) {
                throw LinuxRuntimeFailure(code: .commandFailed, message: refusal)
            }
            guard try self.session(id: sessionID) == nil else {
                throw LinuxRuntimeFailure(code: .commandFailed, message: "The closed pane remained.")
            }
            try progress.advance(to: .observed)
            return LinuxLifecycleReceipt(progress: progress, sessionID: sessionID,
                                         tty: session.tty, attachCommand: nil, output: nil)
        }
    }

    func enumerate(commandID: String) throws -> (LinuxLifecycleReceipt, [TargetSession]) {
        try withReservation(commandID: commandID, channel: "inventory", operation: .enumerate) {
            progress in
            let inventory = try terminal.inventory()
            guard inventory.isComplete else {
                throw LinuxRuntimeFailure(code: .commandFailed,
                                          message: inventory.error ?? "The terminal inventory is incomplete.")
            }
            try progress.advance(to: .executed)
            try progress.advance(to: .delivered)
            try progress.advance(to: .observed)
            let receipt = LinuxLifecycleReceipt(progress: progress, sessionID: nil,
                                                tty: nil, attachCommand: nil,
                                                output: String(inventory.sessions.count))
            return (receipt, inventory.sessions)
        }
    }

    private func requiredSession(_ id: String) throws -> TargetSession {
        guard let value = try session(id: id) else {
            throw LinuxRuntimeFailure(code: .commandFailed, message: "That tmux pane is gone.")
        }
        return value
    }

    private func session(id: String) throws -> TargetSession? {
        let inventory = try terminal.inventory()
        guard inventory.isComplete else {
            throw LinuxRuntimeFailure(code: .commandFailed,
                                      message: inventory.error ?? "The terminal inventory is incomplete.")
        }
        return inventory.sessions.first { $0.id == id }
    }

    private func withReservation<T>(commandID: String, channel: String,
                                    operation: TerminalEffectOperation,
                                    _ work: (inout TerminalEffectProgress) throws -> T) throws -> T {
        try scheduling.run(commandID: commandID, channel: channel, operation: operation) {
            var progress = TerminalEffectProgress(commandID: commandID,
                                                  operation: operation, channel: channel)
            return try work(&progress)
        }
    }

    private func lifecycleFailure(_ error: Error, progress: TerminalEffectProgress,
                                  certainty: LinuxEffectCertainty,
                                  checkpoint: LinuxEffectCheckpoint = .none,
                                  sessionID: String? = nil, tty: String? = nil)
        -> LinuxLifecycleFailure {
        let code: String
        let message: String
        switch error {
        case let failure as LinuxRuntimeFailure:
            code = failure.code.rawValue
            message = failure.message
        case let failure as TerminalLifecycleFailure:
            code = failure.code.rawValue
            message = failure.message
        case let failure as HostCapabilityUnavailable:
            code = HostCapabilityUnavailable.code
            message = failure.message
        case let failure as ProjectRootRefusal:
            code = failure.code.rawValue
            message = failure.message
        case is HostProcessIdentityChanged:
            code = LinuxRuntimeFailureCode.processIdentity.rawValue
            message = "The provider process identity changed during the lifecycle."
        default:
            code = LinuxRuntimeFailureCode.commandFailed.rawValue
            message = "The terminal lifecycle failed after admission."
        }
        return LinuxLifecycleFailure(
            code: code, message: message, progress: progress, certainty: certainty,
            lastConfirmedEffect: checkpoint, sessionID: sessionID, tty: tty,
            reconciliationRequired: certainty == .partial || certainty == .unknown)
    }
}
