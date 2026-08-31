import Foundation

/// The launch decision is deliberately independent of AppKit and config.json. A config switch
/// says what a service should do; it cannot also mean that a person finished being introduced to
/// the app, because hand-editing that service would then silently dismiss the first window.
enum AppLaunchPolicy {
    static func shouldShowHome(onboardingComplete: Bool) -> Bool { !onboardingComplete }
}

enum HomeReopenPolicy {
    static func shouldShowHome(hasVisibleWindows: Bool) -> Bool { !hasVisibleWindows }
}

enum OnboardingPollingPolicy {
    static let interval: TimeInterval = 2.0
    static let requestTimeout: TimeInterval = 1.5
    static let evidenceInterval: TimeInterval = 10.0

    static func shouldStartRequest(isInFlight: Bool) -> Bool { !isInFlight }

    static func shouldRefreshEvidence(now: Date, lastRefresh: Date?, isInFlight: Bool) -> Bool {
        guard !isInFlight else { return false }
        guard let lastRefresh else { return true }
        return now.timeIntervalSince(lastRefresh) >= evidenceInterval
    }
}

/// Versioned separately from configuration semantics so a future onboarding revision can be
/// shown once without pretending an existing preference disappeared.
struct OnboardingCompletionStore {
    static let currentVersion = 1
    let fileURL: URL

    static var production: OnboardingCompletionStore {
        let directory: URL
        if let override = ProcessInfo.processInfo.environment["CLAWDLINE_ONBOARDING_DIR"],
           !override.isEmpty {
            directory = URL(fileURLWithPath: override, isDirectory: true)
        } else {
            directory = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".config/clawdline", isDirectory: true)
        }
        return OnboardingCompletionStore(fileURL: directory.appendingPathComponent("onboarding.json"))
    }

    var isCurrent: Bool {
        guard let data = try? Data(contentsOf: fileURL),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let version = object["completed_version"] as? Int else { return false }
        return version >= Self.currentVersion
    }

    @discardableResult
    func markCurrent(now: Date = Date()) -> Bool {
        let object: [String: Any] = [
            "completed_version": Self.currentVersion,
            "completed_at": ISO8601DateFormatter().string(from: now),
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: object,
                                                     options: [.prettyPrinted, .sortedKeys]) else {
            return false
        }
        do {
            try FileManager.default.createDirectory(at: fileURL.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            try data.write(to: fileURL, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                   ofItemAtPath: fileURL.path)
            return true
        } catch {
            return false
        }
    }
}

enum LocalHealthFailure: Equatable {
    case transport
    case timedOut
    case httpStatus(Int)
    case unhealthy
    case invalidResponse
}

enum LocalHealthEvidence: Equatable {
    case notChecked
    case checking
    case ready
    case failed(LocalHealthFailure)
}

/// Turns the complete URLSession observation into one honest health fact. The UI never needs to
/// infer a negative from missing data, and a 200 is not promoted unless the body also says
/// `ok: true`.
enum LocalHealthEvidencePolicy {
    static func interpret(statusCode: Int?, bodyOK: Bool?, errorCode: Int?) -> LocalHealthEvidence {
        if let errorCode {
            return errorCode == NSURLErrorTimedOut ? .failed(.timedOut) : .failed(.transport)
        }
        guard let statusCode else { return .failed(.invalidResponse) }
        guard statusCode == 200 else { return .failed(.httpStatus(statusCode)) }
        guard let bodyOK else { return .failed(.invalidResponse) }
        return bodyOK ? .ready : .failed(.unhealthy)
    }
}

enum LocalBrowserPhase: Equatable {
    case serverOff
    case configurationFailed
    case checkingHealth
    case healthFailed(LocalHealthFailure)
    case readyToOpen
    case awaitingDevice
    case connected
}

enum LocalBrowserAction: Equatable {
    case enableServer
    case retryHealth
    case openBrowser
    case openBrowserAgain
    case finish
}

struct LocalBrowserDecision: Equatable {
    let phase: LocalBrowserPhase
    let action: LocalBrowserAction
    let mayMintDevice: Bool
    let succeeded: Bool
}

/// The security and evidence seam for local onboarding. Listener allocation and a config bit are
/// intentionally absent: only a successful request to `/v1/health` supplies `healthReady`, and
/// only the exact newly minted non-local device supplies `deviceLastSeen`.
enum LocalBrowserPolicy {
    static func decide(serverEnabled: Bool, configurationFailed: Bool = false,
                       health: LocalHealthEvidence,
                       deviceCreated: Bool, exactDeviceLastSeen: Bool) -> LocalBrowserDecision {
        if configurationFailed {
            return LocalBrowserDecision(phase: .configurationFailed, action: .enableServer,
                                        mayMintDevice: false, succeeded: false)
        }
        guard serverEnabled else {
            return LocalBrowserDecision(phase: .serverOff, action: .enableServer,
                                        mayMintDevice: false, succeeded: false)
        }
        switch health {
        case .notChecked, .checking:
            return LocalBrowserDecision(phase: .checkingHealth, action: .retryHealth,
                                        mayMintDevice: false, succeeded: false)
        case .failed(let failure):
            return LocalBrowserDecision(phase: .healthFailed(failure), action: .retryHealth,
                                        mayMintDevice: false, succeeded: false)
        case .ready:
            break
        }
        guard deviceCreated else {
            return LocalBrowserDecision(phase: .readyToOpen, action: .openBrowser,
                                        mayMintDevice: true, succeeded: false)
        }
        guard exactDeviceLastSeen else {
            return LocalBrowserDecision(phase: .awaitingDevice, action: .openBrowserAgain,
                                        mayMintDevice: false, succeeded: false)
        }
        return LocalBrowserDecision(phase: .connected, action: .finish,
                                    mayMintDevice: false, succeeded: true)
    }
}

enum CloudflareOnboardingTunnel: Equatable {
    case off
    case starting
    case up(url: String)
    case failed(reason: String)
}

enum CloudflareOnboardingPhase: Equatable {
    case cloudflaredMissing
    case tunnelOff
    case starting
    case ready(url: String)
    case awaitingDevice(url: String)
    case connected(url: String)
    case failed(reason: String)
}

enum CloudflareOnboardingAction: Equatable {
    case installCloudflared
    case openSettings
    case checkAgain
    case showQR
    case showQRAgain
    case finish
}

struct CloudflareOnboardingDecision: Equatable {
    let phase: CloudflareOnboardingPhase
    let action: CloudflareOnboardingAction
    let qrURL: String?
    let mayMintDevice: Bool
    let succeeded: Bool
}

/// The public-phone evidence seam. A configured mode, name or hostname never permits a QR;
/// only the URL carried by the live `.up` state does. Success additionally requires the exact
/// non-local device minted for this route to have reported `lastSeen`.
enum CloudflareOnboardingPolicy {
    static func decide(cloudflaredInstalled: Bool, tunnel: CloudflareOnboardingTunnel,
                       deviceCreated: Bool, exactDeviceLastSeen: Bool) -> CloudflareOnboardingDecision {
        guard cloudflaredInstalled else {
            return CloudflareOnboardingDecision(
                phase: .cloudflaredMissing, action: .installCloudflared, qrURL: nil,
                mayMintDevice: false, succeeded: false)
        }
        switch tunnel {
        case .off:
            return CloudflareOnboardingDecision(
                phase: .tunnelOff, action: .openSettings, qrURL: nil,
                mayMintDevice: false, succeeded: false)
        case .starting:
            return CloudflareOnboardingDecision(
                phase: .starting, action: .checkAgain, qrURL: nil,
                mayMintDevice: false, succeeded: false)
        case .failed(let reason):
            return CloudflareOnboardingDecision(
                phase: .failed(reason: reason), action: .openSettings, qrURL: nil,
                mayMintDevice: false, succeeded: false)
        case .up(let url):
            guard !url.isEmpty else {
                return CloudflareOnboardingDecision(
                    phase: .failed(reason: "The tunnel reported no public URL."),
                    action: .openSettings, qrURL: nil, mayMintDevice: false, succeeded: false)
            }
            guard deviceCreated else {
                return CloudflareOnboardingDecision(
                    phase: .ready(url: url), action: .showQR, qrURL: url,
                    mayMintDevice: true, succeeded: false)
            }
            guard exactDeviceLastSeen else {
                return CloudflareOnboardingDecision(
                    phase: .awaitingDevice(url: url), action: .showQRAgain, qrURL: url,
                    mayMintDevice: false, succeeded: false)
            }
            return CloudflareOnboardingDecision(
                phase: .connected(url: url), action: .finish, qrURL: url,
                mayMintDevice: false, succeeded: true)
        }
    }
}

enum CloudPreviewFailure: Equatable {
    case identityRead
    case relayUnauthorized
    case relayFailed
    case pairingRead
    case pairingAmbiguous
}

enum CloudPreviewProof: Equatable {
    case absent
    case unavailable
    case proved(String)
    case failed(CloudPreviewFailure)
}

enum CloudPreviewAction: Equatable {
    case openCloudSettings
    case pairPhone
    case reviewPreviewStatus
}

struct CloudPreviewDecision: Equatable {
    let account: CloudPreviewProof
    let machineCredential: CloudPreviewProof
    let relayReady: CloudPreviewProof
    let e2eePairing: CloudPreviewProof
    let viewerReceipt: CloudPreviewProof
    let action: CloudPreviewAction
    let succeeded: Bool
}

/// Cloud's five facts remain five independent facts. In particular, a credential cannot promote
/// relay readiness, a pinned E2EE key cannot stand in for a delivered viewer receipt, and a proof
/// the current protocol does not expose is `.unavailable`, not a success-shaped placeholder.
enum CloudPreviewPolicy {
    static func decide(account: CloudPreviewProof, machineCredential: CloudPreviewProof,
                       relayReady: CloudPreviewProof, e2eePairing: CloudPreviewProof,
                       viewerReceipt: CloudPreviewProof) -> CloudPreviewDecision {
        let action: CloudPreviewAction
        if !isProved(account) || !isProved(machineCredential) {
            action = .openCloudSettings
        } else if !isProved(e2eePairing) {
            action = .pairPhone
        } else {
            action = .reviewPreviewStatus
        }
        return CloudPreviewDecision(
            account: account, machineCredential: machineCredential, relayReady: relayReady,
            e2eePairing: e2eePairing, viewerReceipt: viewerReceipt, action: action,
            succeeded: [account, machineCredential, relayReady, e2eePairing, viewerReceipt]
                .allSatisfy(isProved))
    }

    private static func isProved(_ proof: CloudPreviewProof) -> Bool {
        if case .proved = proof { return true }
        return false
    }
}

enum OnboardingCapability: String, Hashable {
    case read
    case send
}

struct RouteCredentialPlan: Equatable {
    let capabilities: Set<OnboardingCapability>
    let local: Bool

    func remoteCapabilities() -> Set<RemoteAuth.Capability> {
        Set(capabilities.compactMap { RemoteAuth.Capability(rawValue: $0.rawValue) })
    }
}

enum LocalBrowserCredentialPolicy {
    static func plan(for decision: LocalBrowserDecision) -> RouteCredentialPlan? {
        guard decision.mayMintDevice else { return nil }
        return RouteCredentialPlan(capabilities: [.read], local: false)
    }
}

struct PhoneCredentialPlan: Equatable {
    let publicBaseURL: String
    let capabilities: Set<OnboardingCapability>
    let local: Bool

    func signInURL(token: String) -> String {
        "\(publicBaseURL.hasSuffix("/") ? String(publicBaseURL.dropLast()) : publicBaseURL)/?t=\(token)"
    }

    func remoteCapabilities() -> Set<RemoteAuth.Capability> {
        Set(capabilities.compactMap { RemoteAuth.Capability(rawValue: $0.rawValue) })
    }
}

/// This is the single gate shared by Home and Settings. A caller cannot mint and then discover
/// that it had no deliverable address: no live, non-empty tunnel URL means no credential plan.
enum PhoneCredentialPolicy {
    static func plan(remoteEnabled: Bool, remoteWrite: Bool,
                     tunnel: RemoteTunnel.State) -> PhoneCredentialPlan? {
        guard remoteEnabled, case .up(let url) = tunnel, !url.isEmpty else { return nil }
        return PhoneCredentialPlan(
            publicBaseURL: url,
            capabilities: remoteWrite ? [.read, .send] : [.read],
            local: false)
    }
}

struct IssuedPhoneCredential: Equatable {
    let id: String
    let token: String
    let signInURL: String
}

enum PhoneCredentialIssuer {
    static func issue(remoteEnabled: Bool, remoteWrite: Bool, tunnel: RemoteTunnel.State,
                      name: String,
                      mint: (String, Set<RemoteAuth.Capability>, Bool) -> (id: String, token: String))
        -> IssuedPhoneCredential? {
        guard let plan = PhoneCredentialPolicy.plan(
            remoteEnabled: remoteEnabled, remoteWrite: remoteWrite, tunnel: tunnel) else {
            return nil
        }
        let made = mint(name, plan.remoteCapabilities(), plan.local)
        return IssuedPhoneCredential(
            id: made.id, token: made.token, signInURL: plan.signInURL(token: made.token))
    }
}

struct OnboardingDeviceObservation: Equatable {
    let id: String
    let wasSeen: Bool

    init(_ device: RemoteAuth.Device) {
        id = device.id
        wasSeen = device.lastSeen != nil
    }
}

enum OnboardingEvidencePolicy {
    static func local(serverEnabled: Bool, configurationFailed: Bool = false,
                      health: LocalHealthEvidence,
                      expectedDeviceID: String?, devices: [RemoteAuth.Device]) -> LocalBrowserDecision {
        let exact = expectedDeviceID.flatMap { wanted in
            devices.map(OnboardingDeviceObservation.init).first(where: { $0.id == wanted })
        }
        return LocalBrowserPolicy.decide(
            serverEnabled: serverEnabled, configurationFailed: configurationFailed, health: health,
            deviceCreated: expectedDeviceID != nil,
            exactDeviceLastSeen: exact?.wasSeen == true)
    }

    static func cloudflare(cloudflaredInstalled: Bool, tunnelState: RemoteTunnel.State,
                           expectedDeviceID: String?, devices: [RemoteAuth.Device])
        -> CloudflareOnboardingDecision {
        let tunnel: CloudflareOnboardingTunnel
        switch tunnelState {
        case .off: tunnel = .off
        case .starting: tunnel = .starting
        case .up(let url): tunnel = .up(url: url)
        case .failed(let reason): tunnel = .failed(reason: reason)
        }
        let exact = expectedDeviceID.flatMap { wanted in
            devices.map(OnboardingDeviceObservation.init).first(where: { $0.id == wanted })
        }
        return CloudflareOnboardingPolicy.decide(
            cloudflaredInstalled: cloudflaredInstalled, tunnel: tunnel,
            deviceCreated: expectedDeviceID != nil,
            exactDeviceLastSeen: exact?.wasSeen == true)
    }
}

enum CredentialLifetimePolicy {
    static func shouldRevokeUnseen(expectedDeviceID: String?, devices: [RemoteAuth.Device]) -> Bool {
        guard let expectedDeviceID else { return false }
        return devices.first(where: { $0.id == expectedDeviceID })?.lastSeen == nil
    }
}

struct CloudPairingAttempt: Equatable {
    let baselineDeviceIDs: Set<String>
    var boundDeviceID: String?
}

struct CloudPreviewEvidenceResult: Equatable {
    let decision: CloudPreviewDecision
    let boundDeviceID: String?
}

/// Converts the real Keychain, lifecycle and pairing-store readings into the five proof slots.
/// No view code decides whether missing evidence is absence, unavailability, failure or proof.
enum CloudPreviewEvidencePolicy {
    static func evaluate(identityResult: Result<CloudMachineIdentity?, Error>,
                         lifecycle: CloudBridgeLifecycle.State,
                         devicesResult: Result<[CloudPairedDevice], Error>?,
                         pairingAttempt: CloudPairingAttempt?) -> CloudPreviewEvidenceResult {
        let identity: CloudMachineIdentity?
        let account: CloudPreviewProof
        let credential: CloudPreviewProof
        switch identityResult {
        case .success(let restored):
            identity = restored
            if let restored {
                account = .proved(restored.accountID)
                credential = .proved(restored.machineID)
            } else {
                account = .absent
                credential = .absent
            }
        case .failure:
            identity = nil
            account = .failed(.identityRead)
            credential = .failed(.identityRead)
        }

        let relay: CloudPreviewProof
        switch lifecycle {
        case .unauthorized:
            relay = .failed(.relayUnauthorized)
        case .failed:
            relay = .failed(.relayFailed)
        case .detached, .attached:
            relay = .unavailable
        }

        var bound = pairingAttempt?.boundDeviceID
        let pairing: CloudPreviewProof
        if identity == nil {
            pairing = .unavailable
        } else if let devicesResult {
            switch devicesResult {
            case .failure:
                pairing = .failed(.pairingRead)
            case .success(let devices):
                if let existing = bound.flatMap({ id in devices.first(where: { $0.deviceID == id }) }) {
                    pairing = .proved(existing.deviceID)
                } else if let attempt = pairingAttempt {
                    let candidates = devices.filter { !attempt.baselineDeviceIDs.contains($0.deviceID) }
                    if candidates.count == 1, let only = candidates.first {
                        bound = only.deviceID
                        pairing = .proved(only.deviceID)
                    } else if candidates.count > 1 {
                        bound = nil
                        pairing = .failed(.pairingAmbiguous)
                    } else {
                        bound = nil
                        pairing = .absent
                    }
                } else {
                    pairing = .unavailable
                }
            }
        } else {
            pairing = .unavailable
        }

        return CloudPreviewEvidenceResult(
            decision: CloudPreviewPolicy.decide(
                account: account, machineCredential: credential, relayReady: relay,
                e2eePairing: pairing, viewerReceipt: .unavailable),
            boundDeviceID: bound)
    }
}

enum OnboardingCompletionTrigger: Equatable {
    case routeSucceeded
    case homeDismissed
}

enum OnboardingExitPolicy {
    static func shouldRecordCompletion(for trigger: OnboardingCompletionTrigger) -> Bool {
        switch trigger {
        case .routeSucceeded, .homeDismissed: return true
        }
    }
}

#if !ONBOARDING_POLICY_ONLY
import AppKit

/// AppKit owns this singleton on the main thread. The only background closure captures it weakly
/// and returns through `DispatchQueue.main` before reading or mutating any field.
final class HomeWindow: NSObject, NSWindowDelegate, @unchecked Sendable {
    static let shared = HomeWindow()

    private var window: NSWindow?
    private var stateValue: NSTextField?
    private var localExpectedValue: NSTextField?
    private var localRecoveryValue: NSTextField?
    private var actionButton: NSButton?
    private var timer: Timer?
    private var health = LocalHealthEvidence.notChecked
    private var healthRequestInFlight = false
    private var healthRequestGeneration = 0
    private var localConfigurationFailed = false
    private var localDeviceID: String?
    private var localToken: String?
    private var cloudflareStateValue: NSTextField?
    private var cloudflareExpectedValue: NSTextField?
    private var cloudflareRecoveryValue: NSTextField?
    private var cloudflareActionButton: NSButton?
    private var cloudflareDeviceID: String?
    private var cloudflareToken: String?
    private var cloudflareQRWindow: NSWindow?
    private var cloudStateValue: NSTextField?
    private var cloudExpectedValue: NSTextField?
    private var cloudRecoveryValue: NSTextField?
    private var cloudActionButton: NSButton?
    private var cloudPairingAttempt: CloudPairingAttempt?
    private var cloudKnownDeviceIDs = Set<String>()
    private var cloudDeviceSnapshotAvailable = false
    private var cloudDecisionCache = CloudPreviewPolicy.decide(
        account: .unavailable, machineCredential: .unavailable,
        relayReady: .unavailable, e2eePairing: .unavailable, viewerReceipt: .unavailable)
    private var cloudflaredInstalled = false
    private var evidenceRefreshInFlight = false
    private var lastEvidenceRefreshAt: Date?
    private let evidenceQueue = DispatchQueue(label: "clawdline.onboarding.evidence",
                                               qos: .utility)
    private var completionRecorded = false
    private var completionAttempted = false
    private var completionStatusValue: NSTextField?

    private override init() { super.init() }

    func show() {
        if window == nil { window = buildWindow() }
        refresh()
        startPolling()
        if window?.isVisible != true { window?.center() }
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func rebuildIfVisible() {
        guard window?.isVisible == true else { return }
        let oldFrame = window?.frame
        let wasKey = window?.isKeyWindow == true
        timer?.invalidate()
        healthRequestGeneration += 1
        healthRequestInFlight = false
        health = .notChecked
        window?.orderOut(nil)
        window = nil
        window = buildWindow()
        if let oldFrame { window?.setFrame(oldFrame, display: false) }
        refresh()
        startPolling()
        if wasKey { window?.makeKeyAndOrderFront(nil) } else { window?.orderFront(nil) }
    }

    func windowWillClose(_ notification: Notification) {
        guard notification.object as? NSWindow === window else { return }
        timer?.invalidate()
        timer = nil
        healthRequestGeneration += 1
        healthRequestInFlight = false
        closeCloudflareQR(revokingUnseenCredential: true)
        discardUnseenLocalCredential()
        recordCompletion(for: .homeDismissed)
    }

    private func buildWindow() -> NSWindow {
        let frame = NSRect(x: 0, y: 0, width: 760, height: 820)
        let content = NSView(frame: frame)
        content.wantsLayer = true
        content.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor

        let stack = NSStackView()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 16
        stack.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 28),
            stack.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -28),
            stack.topAnchor.constraint(equalTo: content.topAnchor, constant: 26),
            stack.bottomAnchor.constraint(lessThanOrEqualTo: content.bottomAnchor, constant: -26),
        ])

        let title = label(L.t.homeTitle, size: 28, weight: .bold)
        stack.addArrangedSubview(title)
        stack.addArrangedSubview(label(L.t.homeWelcome, size: 14, color: .secondaryLabelColor))
        stack.addArrangedSubview(label(L.t.homePurpose, size: 16, weight: .semibold))

        let local = card(title: L.t.homeLocalTitle, summary: L.t.homeLocalSummary)
        let detail = NSStackView()
        detail.orientation = .vertical
        detail.alignment = .leading
        detail.spacing = 8
        detail.translatesAutoresizingMaskIntoConstraints = false
        local.content.addArrangedSubview(detail)

        let state = label("", size: 13, weight: .medium)
        stateValue = state
        let localExpected = label("", size: 13)
        let localRecovery = label("", size: 13)
        localExpectedValue = localExpected
        localRecoveryValue = localRecovery
        detail.addArrangedSubview(label(L.t.setupLocalReadOnlyAction, size: 13,
                                        color: .secondaryLabelColor))
        detail.addArrangedSubview(row(L.t.setupDetected, value: state))
        detail.addArrangedSubview(row(L.t.setupExpected, value: localExpected))
        detail.addArrangedSubview(row(L.t.setupRecovery, value: localRecovery))
        let action = NSButton(title: "", target: self, action: #selector(performLocalAction))
        action.bezelStyle = .rounded
        actionButton = action
        detail.addArrangedSubview(row(L.t.setupNextAction, value: action))
        stack.addArrangedSubview(local.box)

        let remoteCards = NSStackView()
        remoteCards.orientation = .horizontal
        remoteCards.alignment = .top
        remoteCards.distribution = .fillEqually
        remoteCards.spacing = 14
        let cloudflare = card(title: L.t.homeCloudflareTitle,
                              summary: L.t.homeCloudflareSummary)
        let cloudflareState = label("", size: 12, weight: .medium)
        let cloudflareRecovery = label("", size: 12)
        let cloudflareAction = NSButton(title: "", target: self,
                                        action: #selector(performCloudflareAction))
        cloudflareAction.bezelStyle = .rounded
        cloudflareStateValue = cloudflareState
        let cloudflareExpected = label("", size: 12)
        cloudflareExpectedValue = cloudflareExpected
        cloudflareRecoveryValue = cloudflareRecovery
        cloudflareActionButton = cloudflareAction
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupDetected, value: cloudflareState))
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupNextAction, value: cloudflareAction))
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupExpected, value: cloudflareExpected))
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupRecovery, value: cloudflareRecovery))

        let preview = card(title: L.t.homeCloudPreviewTitle,
                           summary: L.t.homeCloudPreviewSummary)
        let cloudState = label("", size: 12, weight: .medium)
        let cloudRecovery = label("", size: 12)
        let cloudAction = NSButton(title: "", target: self,
                                   action: #selector(performCloudAction))
        cloudAction.bezelStyle = .rounded
        cloudStateValue = cloudState
        let cloudExpected = label("", size: 12)
        cloudExpectedValue = cloudExpected
        cloudRecoveryValue = cloudRecovery
        cloudActionButton = cloudAction
        preview.content.addArrangedSubview(compactRow(L.t.setupDetected, value: cloudState))
        preview.content.addArrangedSubview(compactRow(L.t.setupNextAction, value: cloudAction))
        preview.content.addArrangedSubview(compactRow(
            L.t.setupExpected, value: cloudExpected))
        preview.content.addArrangedSubview(compactRow(L.t.setupRecovery, value: cloudRecovery))

        remoteCards.addArrangedSubview(cloudflare.box)
        remoteCards.addArrangedSubview(preview.box)
        stack.addArrangedSubview(remoteCards)
        let completionStatus = label(L.t.setupDismissalHint, size: 12,
                                     color: .secondaryLabelColor)
        completionStatusValue = completionStatus
        stack.addArrangedSubview(completionStatus)
        NSLayoutConstraint.activate([
            local.box.widthAnchor.constraint(equalTo: stack.widthAnchor),
            remoteCards.widthAnchor.constraint(equalTo: stack.widthAnchor),
        ])

        let window = NSWindow(contentRect: frame,
                              styleMask: [.titled, .closable, .miniaturizable, .resizable],
                              backing: .buffered, defer: false)
        window.title = L.t.homeTitle
        window.minSize = NSSize(width: 680, height: 760)
        window.contentView = content
        window.isReleasedWhenClosed = false
        window.delegate = self
        return window
    }

    private typealias Card = (box: NSBox, content: NSStackView)

    private func card(title: String, summary: String) -> Card {
        let box = NSBox()
        box.boxType = .custom
        box.borderColor = NSColor.separatorColor
        box.cornerRadius = 10
        box.contentViewMargins = NSSize(width: 18, height: 16)
        let stack = NSStackView()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 9
        stack.addArrangedSubview(label(title, size: 17, weight: .semibold))
        stack.addArrangedSubview(label(summary, size: 13, color: .secondaryLabelColor))
        box.contentView = stack
        return (box, stack)
    }

    private func row(_ name: String, value: NSView) -> NSView {
        let stack = NSStackView()
        stack.orientation = .horizontal
        stack.alignment = .top
        stack.spacing = 10
        let heading = label(name, size: 12, weight: .semibold, color: .secondaryLabelColor)
        heading.widthAnchor.constraint(equalToConstant: 118).isActive = true
        stack.addArrangedSubview(heading)
        stack.addArrangedSubview(value)
        return stack
    }

    private func compactRow(_ name: String, value: NSView) -> NSView {
        let stack = NSStackView()
        stack.orientation = .horizontal
        stack.alignment = .top
        stack.spacing = 8
        let heading = label(name, size: 11, weight: .semibold, color: .secondaryLabelColor)
        heading.widthAnchor.constraint(equalToConstant: 82).isActive = true
        stack.addArrangedSubview(heading)
        stack.addArrangedSubview(value)
        return stack
    }

    private func label(_ text: String, size: CGFloat,
                       weight: NSFont.Weight = .regular,
                       color: NSColor = .labelColor) -> NSTextField {
        let field = NSTextField(wrappingLabelWithString: text)
        field.font = NSFont.systemFont(ofSize: size, weight: weight)
        field.textColor = color
        field.maximumNumberOfLines = 0
        field.lineBreakMode = .byWordWrapping
        return field
    }

    private var decision: LocalBrowserDecision {
        OnboardingEvidencePolicy.local(
            serverEnabled: Config.shared.remote,
            configurationFailed: localConfigurationFailed,
            health: health,
            expectedDeviceID: localDeviceID,
            devices: RemoteAuth.approvedDevices)
    }

    private var cloudflareDecision: CloudflareOnboardingDecision {
        OnboardingEvidencePolicy.cloudflare(
            cloudflaredInstalled: cloudflaredInstalled,
            tunnelState: RemoteTunnel.shared.state,
            expectedDeviceID: cloudflareDeviceID,
            devices: RemoteAuth.approvedDevices)
    }

    private func refresh() {
        let current = decision
        switch current.phase {
        case .serverOff: stateValue?.stringValue = L.t.setupLocalServerOff
        case .configurationFailed: stateValue?.stringValue = L.t.setupLocalConfigurationFailed
        case .checkingHealth: stateValue?.stringValue = L.t.setupLocalChecking
        case .healthFailed(let failure):
            stateValue?.stringValue = L.t.setupLocalHealthFailure(failure)
        case .readyToOpen: stateValue?.stringValue = L.t.setupLocalReady
        case .awaitingDevice: stateValue?.stringValue = L.t.setupLocalWaiting
        case .connected: stateValue?.stringValue = L.t.setupLocalConnected
        }
        switch current.action {
        case .enableServer: actionButton?.title = L.t.setupLocalEnable
        case .retryHealth: actionButton?.title = L.t.setupLocalRetry
        case .openBrowser: actionButton?.title = L.t.setupLocalOpen
        case .openBrowserAgain: actionButton?.title = L.t.setupLocalOpenAgain
        case .finish: actionButton?.title = L.t.setupFinish
        }
        localExpectedValue?.stringValue = L.t.setupLocalExpected(current.phase)
        localRecoveryValue?.stringValue = L.t.setupLocalRecovery(current.phase)
        if current.succeeded { recordCompletion(for: .routeSucceeded) }

        refreshCloudflare()
        refreshCloud()
    }

    private func refreshCloudflare() {
        let current = cloudflareDecision
        let mode = TunnelMode(configured: Config.shared.remoteTunnel)
        let modeName: String
        let tunnelName: String
        switch mode {
        case .off:
            modeName = L.t.setupTunnelModeOff
            tunnelName = L.t.setupNone
        case .quick:
            modeName = L.t.setupTunnelModeQuick
            tunnelName = L.t.setupTunnelQuickName
        case .named:
            modeName = L.t.setupTunnelModeNamed
            let configured = Config.shared.remoteTunnelName.trimmingCharacters(in: .whitespaces)
            tunnelName = configured.isEmpty ? L.t.setupNone : configured
        }
        let liveURL: String?
        switch current.phase {
        case .ready(let url), .awaitingDevice(let url), .connected(let url): liveURL = url
        default: liveURL = nil
        }
        let hostname = liveURL.flatMap { URL(string: $0)?.host }
        let shownHost = hostname.flatMap { $0.isEmpty ? nil : $0 } ?? L.t.setupNone
        let access = Config.shared.remoteWrite ? L.t.setupControlChosen : L.t.setupReadOnly

        let status: String
        switch current.phase {
        case .cloudflaredMissing:
            status = L.t.setupTunnelMissing
        case .tunnelOff:
            status = L.t.setupTunnelOff
        case .starting:
            status = L.t.setupTunnelStarting
        case .ready(let url):
            status = L.t.setupTunnelReady(url)
        case .awaitingDevice(let url):
            status = L.t.setupTunnelWaiting(url)
        case .connected(let url):
            status = L.t.setupTunnelConnected(url)
        case .failed(let reason):
            status = reason
        }
        cloudflareStateValue?.stringValue = L.t.setupTunnelFacts(
            modeName, tunnelName, shownHost, status + "\n" + access)
        cloudflareExpectedValue?.stringValue = L.t.setupTunnelExpected(current.phase)
        cloudflareRecoveryValue?.stringValue = L.t.setupTunnelRecovery(current.phase)
        switch current.action {
        case .installCloudflared, .openSettings:
            cloudflareActionButton?.title = L.t.setupOpenRemoteSettings
        case .checkAgain:
            cloudflareActionButton?.title = L.t.setupCheckTunnel
        case .showQR:
            cloudflareActionButton?.title = L.t.setupShowPhoneQR
        case .showQRAgain:
            cloudflareActionButton?.title = L.t.setupShowPhoneQRAgain
        case .finish:
            cloudflareActionButton?.title = L.t.setupFinish
        }
        if current.qrURL == nil {
            closeCloudflareQR(revokingUnseenCredential: true)
        }
        if current.succeeded { recordCompletion(for: .routeSucceeded) }
    }

    private func refreshCloud() {
        let current = cloudDecisionCache
        cloudStateValue?.stringValue = L.t.setupCloudFacts(
            cloudProof(current.account, kind: .account),
            cloudProof(current.machineCredential, kind: .credential),
            cloudProof(current.relayReady, kind: .relay),
            cloudProof(current.e2eePairing, kind: .pairing),
            cloudProof(current.viewerReceipt, kind: .receipt))
        switch current.action {
        case .openCloudSettings:
            cloudActionButton?.title = L.t.setupOpenCloudSettings
        case .pairPhone:
            cloudActionButton?.title = L.t.setupPairCloudPhone
        case .reviewPreviewStatus:
            cloudActionButton?.title = L.t.setupReviewCloudPreview
        }
        cloudExpectedValue?.stringValue = L.t.setupCloudExpected(current)
        cloudRecoveryValue?.stringValue = L.t.setupCloudRecovery(current)
    }

    private enum CloudProofKind { case account, credential, relay, pairing, receipt }

    private func cloudProof(_ proof: CloudPreviewProof, kind: CloudProofKind) -> String {
        switch proof {
        case .absent:
            return L.t.setupProofAbsent
        case .unavailable:
            return L.t.setupProofUnavailable
        case .failed(let failure):
            return L.t.setupProofFailed(failure)
        case .proved(let value):
            switch kind {
            case .account: return L.t.setupCloudAccountProved(value)
            case .credential: return L.t.setupCloudCredentialProved(value)
            case .pairing: return L.t.setupCloudPairingProved(value)
            case .relay, .receipt: return value
            }
        }
    }

    @objc private func performLocalAction() {
        let current = decision
        switch current.action {
        case .enableServer:
            let oldRemote = Config.shared.remote
            let oldRemoteWrite = Config.shared.remoteWrite
            Config.shared.remote = true
            Config.shared.remoteWrite = false
            guard Config.shared.save() else {
                Config.shared.remote = oldRemote
                Config.shared.remoteWrite = oldRemoteWrite
                localConfigurationFailed = true
                Log.write("onboarding: local browser settings were not persisted")
                refresh()
                return
            }
            localConfigurationFailed = false
            NotificationCenter.default.post(name: .clawdlineConfigChanged, object: nil)
            health = .notChecked
            checkHealth()
        case .retryHealth:
            checkHealth()
        case .openBrowser:
            guard let plan = LocalBrowserCredentialPolicy.plan(for: current) else { return }
            let issuedAt = DateFormatter.localizedString(
                from: Date(), dateStyle: .short, timeStyle: .short)
            let made = RemoteAuth.addDevice(
                name: "\(L.t.setupLocalDeviceName) · \(issuedAt)",
                caps: plan.remoteCapabilities(), local: plan.local)
            localDeviceID = made.id
            localToken = made.token
            openLocalBrowser()
        case .openBrowserAgain:
            openLocalBrowser()
        case .finish:
            window?.performClose(nil)
        }
        refresh()
    }

    @objc private func performCloudflareAction() {
        let current = cloudflareDecision
        switch current.action {
        case .installCloudflared, .openSettings:
            SettingsWindow.shared.show()
        case .checkAgain:
            RemoteTunnel.shared.apply()
        case .showQR:
            guard current.mayMintDevice,
                  let made = PhoneCredentialIssuer.issue(
                    remoteEnabled: Config.shared.remote,
                    remoteWrite: Config.shared.remoteWrite,
                    tunnel: RemoteTunnel.shared.state,
                    name: L.t.setupCloudflareDeviceName,
                    mint: { RemoteAuth.addDevice(name: $0, caps: $1, local: $2) }) else { return }
            cloudflareDeviceID = made.id
            cloudflareToken = made.token
            showCloudflareQR(url: made.signInURL)
        case .showQRAgain:
            guard let token = cloudflareToken,
                  let plan = PhoneCredentialPolicy.plan(
                    remoteEnabled: Config.shared.remote,
                    remoteWrite: Config.shared.remoteWrite,
                    tunnel: RemoteTunnel.shared.state) else { return }
            showCloudflareQR(url: plan.signInURL(token: token))
        case .finish:
            window?.performClose(nil)
        }
        refresh()
    }

    @objc private func performCloudAction() {
        // The existing Cloud settings control owns device-code login and E2EE key handover. Home
        // observes their durable evidence rather than creating a second login/pairing state machine.
        if cloudDecisionCache.action == .pairPhone {
            cloudPairingAttempt = cloudDeviceSnapshotAvailable
                ? CloudPairingAttempt(baselineDeviceIDs: cloudKnownDeviceIDs, boundDeviceID: nil)
                : nil
        }
        SettingsWindow.shared.show()
        refreshEvidence(force: true)
        refresh()
    }

    private func showCloudflareQR(url: String) {
        let side: CGFloat = 280
        let content = NSView(frame: NSRect(x: 0, y: 0, width: side + 48, height: side + 126))
        let title = label(L.t.setupScanLiveTunnel, size: 16, weight: .semibold)
        title.alignment = .center
        title.frame = NSRect(x: 24, y: side + 82, width: side, height: 24)
        content.addSubview(title)
        let image = NSImageView(frame: NSRect(x: 24, y: 60, width: side, height: side))
        image.image = RemoteQR.image(for: url, side: side)
        image.imageScaling = .scaleNone
        image.wantsLayer = true
        image.layer?.backgroundColor = NSColor.white.cgColor
        content.addSubview(image)
        let address = label(url, size: 9, color: .secondaryLabelColor)
        address.font = NSFont.monospacedSystemFont(ofSize: 9, weight: .regular)
        address.alignment = .center
        address.isSelectable = true
        address.frame = NSRect(x: 24, y: 14, width: side, height: 38)
        content.addSubview(address)
        let qr = NSWindow(contentRect: content.frame, styleMask: [.titled, .closable],
                          backing: .buffered, defer: false)
        qr.title = L.t.homeCloudflareTitle
        qr.contentView = content
        qr.isReleasedWhenClosed = false
        qr.center()
        cloudflareQRWindow?.orderOut(nil)
        cloudflareQRWindow = qr
        qr.makeKeyAndOrderFront(nil)
    }

    private func openLocalBrowser() {
        guard health == .ready, let token = localToken,
              let url = URL(string: "http://127.0.0.1:\(Config.shared.remotePort)/#t=\(token)") else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    private func startPolling() {
        timer?.invalidate()
        let timer = Timer(timeInterval: OnboardingPollingPolicy.interval, repeats: true) { [weak self] _ in
            self?.checkHealth()
            self?.refreshEvidence()
            self?.refresh()
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
        checkHealth()
        refreshEvidence(force: true)
    }

    /// A listener object is not evidence that bytes can make the round trip. This request is the
    /// same public health endpoint the browser uses; a 200 without `ok: true` is not readiness.
    private func checkHealth() {
        guard Config.shared.remote,
              let url = URL(string: "http://127.0.0.1:\(Config.shared.remotePort)/v1/health") else {
            health = .notChecked
            refresh()
            return
        }
        guard OnboardingPollingPolicy.shouldStartRequest(
            isInFlight: healthRequestInFlight) else { return }
        healthRequestInFlight = true
        if health == .notChecked { health = .checking }
        healthRequestGeneration += 1
        let generation = healthRequestGeneration
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData,
                                 timeoutInterval: OnboardingPollingPolicy.requestTimeout)
        request.httpMethod = "GET"
        URLSession.shared.dataTask(with: request) { [weak self] data, response, error in
            let status = (response as? HTTPURLResponse)?.statusCode
            let object = data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            let errorCode = (error as? URLError)?.errorCode
            let evidence = LocalHealthEvidencePolicy.interpret(
                statusCode: status, bodyOK: object?["ok"] as? Bool, errorCode: errorCode)
            DispatchQueue.main.async {
                guard let self, generation == self.healthRequestGeneration else { return }
                self.healthRequestInFlight = false
                self.health = evidence
                self.refresh()
            }
        }.resume()
    }

    private func refreshEvidence(force: Bool = false) {
        let now = Date()
        guard force || OnboardingPollingPolicy.shouldRefreshEvidence(
            now: now, lastRefresh: lastEvidenceRefreshAt,
            isInFlight: evidenceRefreshInFlight) else { return }
        guard !evidenceRefreshInFlight else { return }
        evidenceRefreshInFlight = true
        lastEvidenceRefreshAt = now
        Task { @MainActor [weak self] in
            guard let self else { return }
            let lifecycle = CloudBridgeLifecycle.shared.state
            self.evidenceQueue.async { [weak self] in
                let installed = RemoteTunnel.isInstalled
                let identityResult: Result<CloudMachineIdentity?, Error>
                do {
                    identityResult = .success(try CloudAccountClient().restoredMachineIdentity())
                } catch {
                    Log.write("onboarding: Cloud identity proof could not be read — \(error.localizedDescription)")
                    identityResult = .failure(error)
                }

                let devicesResult: Result<[CloudPairedDevice], Error>?
                if case .success(let identity?) = identityResult {
                    do {
                        devicesResult = .success(
                            try CloudPairedDeviceStore().devices(accountID: identity.accountID))
                    } catch {
                        Log.write("onboarding: Cloud pairing proof could not be read — \(error.localizedDescription)")
                        devicesResult = .failure(error)
                    }
                } else {
                    devicesResult = nil
                }
                DispatchQueue.main.async { [weak self] in
                    guard let self else { return }
                    let result = CloudPreviewEvidencePolicy.evaluate(
                        identityResult: identityResult, lifecycle: lifecycle,
                        devicesResult: devicesResult, pairingAttempt: self.cloudPairingAttempt)
                    self.cloudDecisionCache = result.decision
                    if self.cloudPairingAttempt != nil {
                        self.cloudPairingAttempt?.boundDeviceID = result.boundDeviceID
                    }
                    if case .success(let devices)? = devicesResult {
                        self.cloudKnownDeviceIDs = Set(devices.map(\.deviceID))
                        self.cloudDeviceSnapshotAvailable = true
                    } else {
                        self.cloudDeviceSnapshotAvailable = false
                    }
                    self.cloudflaredInstalled = installed
                    self.evidenceRefreshInFlight = false
                    self.refresh()
                }
            }
        }
    }

    private func closeCloudflareQR(revokingUnseenCredential: Bool) {
        cloudflareQRWindow?.orderOut(nil)
        cloudflareQRWindow = nil
        guard revokingUnseenCredential, let id = cloudflareDeviceID else { return }
        if CredentialLifetimePolicy.shouldRevokeUnseen(
            expectedDeviceID: id, devices: RemoteAuth.approvedDevices) {
            RemoteAuth.revoke(id: id)
            cloudflareDeviceID = nil
            cloudflareToken = nil
        }
    }

    private func discardUnseenLocalCredential() {
        guard let id = localDeviceID else { return }
        if CredentialLifetimePolicy.shouldRevokeUnseen(
            expectedDeviceID: id, devices: RemoteAuth.approvedDevices) {
            RemoteAuth.revoke(id: id)
        }
        localDeviceID = nil
        localToken = nil
    }

    private func recordCompletion(for trigger: OnboardingCompletionTrigger) {
        guard !completionRecorded,
              OnboardingExitPolicy.shouldRecordCompletion(for: trigger) else { return }
        if completionAttempted, trigger != .homeDismissed { return }
        completionAttempted = true
        completionRecorded = OnboardingCompletionStore.production.markCurrent()
        if !completionRecorded {
            Log.write("onboarding: completion could not be persisted")
            completionStatusValue?.stringValue = L.t.setupCompletionFailed
        }
    }
}
#endif
