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
    case reading
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

enum HomeRouteField: CaseIterable, Equatable {
    case detected
    case expected
    case recovery
    case nextAction
}

/// The card does not own a second ordering or omission decision. Layout walks this tested plan,
/// and visibility asks the same plan, so an absent Recovery value cannot leave an empty heading.
struct HomeRouteFieldPlan: Equatable {
    let fields: [HomeRouteField]

    init(hasRecovery: Bool) {
        fields = hasRecovery
            ? [.detected, .expected, .recovery, .nextAction]
            : [.detected, .expected, .nextAction]
    }

    func contains(_ field: HomeRouteField) -> Bool { fields.contains(field) }

    func walk(_ body: (HomeRouteField) -> Void) {
        fields.forEach(body)
    }
}

enum HomeRecoveryPresentation: Equatable {
    case hidden
    case guidance
    case proved
}

/// Recovery is a semantic state, not a convenient string-or-nil convention. The same policy
/// chooses whether the row exists and which kind of copy fills it for every Home route.
enum HomeRecoveryPolicy {
    static func local(_ phase: LocalBrowserPhase) -> HomeRecoveryPresentation {
        switch phase {
        case .configurationFailed, .healthFailed: return .guidance
        case .connected: return .proved
        case .serverOff, .checkingHealth, .readyToOpen, .awaitingDevice: return .hidden
        }
    }

    static func tunnel(_ phase: CloudflareOnboardingPhase) -> HomeRecoveryPresentation {
        switch phase {
        case .cloudflaredMissing, .tunnelOff, .failed: return .guidance
        case .connected: return .proved
        case .starting, .ready, .awaitingDevice: return .hidden
        }
    }

    static func cloud(_ decision: CloudPreviewDecision) -> HomeRecoveryPresentation {
        decision.succeeded ? .proved : .guidance
    }

    static func text<Value>(
        _ presentation: HomeRecoveryPresentation, guidance: Value, proved: Value
    ) -> Value? {
        switch presentation {
        case .hidden: return nil
        case .guidance: return guidance
        case .proved: return proved
        }
    }
}

#if !ONBOARDING_POLICY_ONLY
import AppKit


private final class HomeProofList: NSView, SelfSizing {
    var text = "" {
        didSet {
            guard text != oldValue else { return }
            rebuild()
        }
    }

    private var rows: [NSTextField] = []
    override var isFlipped: Bool { true }

    private func rebuild() {
        rows.forEach { $0.removeFromSuperview() }
        rows = text.split(separator: "\n", omittingEmptySubsequences: false).map { part in
            let value = String(part)
            let field = NSTextField(labelWithString: value)
            field.font = NSFont.monospacedSystemFont(ofSize: 10.5, weight: .regular)
            field.textColor = Metric.soft
            field.isSelectable = true
            field.lineBreakMode = .byTruncatingMiddle
            field.maximumNumberOfLines = 1
            field.toolTip = value
            addSubview(field)
            return field
        }
        needsLayout = true
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        CGFloat(rows.count) * 20
    }

    override func layout() {
        super.layout()
        for (index, row) in rows.enumerated() {
            row.frame = NSRect(x: 0, y: CGFloat(index) * 20,
                               width: bounds.width, height: 16)
        }
    }
}

/// One route, expressed in the same dark surfaces, hairlines, pixels and chips as Settings.
/// The order lives here once so Local, Cloudflare and Cloud cannot silently drift apart again.
private final class HomeRouteCard: NSView, SelfSizing {
    private let primary: Bool
    private let titleLabel: NSTextField
    private let summaryLabel: NSTextField
    private let noticeLabel: NSTextField?
    private let rule = Hairline()
    private let detectedHeading = makeLabel(L.t.setupDetected.uppercased(),
                                            Metric.headFont, Metric.faint)
    private let expectedHeading = makeLabel(L.t.setupExpected.uppercased(),
                                            Metric.headFont, Metric.faint)
    private let recoveryHeading = makeLabel(L.t.setupRecovery.uppercased(),
                                            Metric.headFont, Metric.faint)
    private let actionHeading = makeLabel(L.t.setupNextAction.uppercased(),
                                          Metric.headFont, Metric.faint)
    private let detectedNote = NoteCard()
    private let proofList: HomeProofList?
    private let expectedLabel = makeLabel("", Metric.noteFont, Metric.soft)
    private let recoveryLabel = makeLabel("", Metric.noteFont, Metric.soft)
    private let actionButton: ChipButton
    private var recoveryText: String?
    private var fieldPlan = HomeRouteFieldPlan(hasRecovery: false)

    override var isFlipped: Bool { true }

    init(title: String, summary: String, notice: String? = nil,
         primary: Bool, proofList: Bool = false, action: @escaping () -> Void) {
        self.primary = primary
        self.titleLabel = makeLabel(
            title, NSFont.systemFont(ofSize: primary ? 19 : 15.5,
                                     weight: primary ? .semibold : .medium),
            Metric.label)
        self.summaryLabel = makeLabel(summary, Metric.noteFont, Metric.soft)
        self.noticeLabel = notice.map {
            makeLabel($0, NSFont.systemFont(ofSize: 11.5, weight: .medium), Style.accent)
        }
        self.proofList = proofList ? HomeProofList() : nil
        self.actionButton = ChipButton(title: "", prominent: primary)
        super.init(frame: .zero)

        [titleLabel, summaryLabel, rule, detectedHeading, expectedHeading,
         recoveryHeading, actionHeading, expectedLabel, recoveryLabel, actionButton]
            .forEach(addSubview)
        if let noticeLabel { addSubview(noticeLabel) }
        if let selfProofList = self.proofList {
            detectedNote.isHidden = true
            addSubview(selfProofList)
        } else {
            addSubview(detectedNote)
        }
        actionButton.action = action
        setAccessibilityElement(true)
        setAccessibilityRole(.group)
        setAccessibilityLabel(title)
    }
    required init?(coder: NSCoder) { fatalError() }

    func update(state: String, dot: PixelDot.State, expected: String,
                recovery: String?, actionTitle: String) {
        if let proofList {
            proofList.text = state
        } else {
            detectedNote.text = state
            detectedNote.dot = dot
        }
        expectedLabel.stringValue = expected
        recoveryText = recovery
        fieldPlan = HomeRouteFieldPlan(hasRecovery: recovery != nil)
        recoveryLabel.stringValue = recovery ?? ""
        HomeRouteField.allCases.forEach { field in
            let hidden = !fieldPlan.contains(field)
            heading(for: field).isHidden = hidden
            value(for: field).isHidden = hidden
        }
        actionButton.title = actionTitle
        needsLayout = true
    }

    private func heading(for field: HomeRouteField) -> NSTextField {
        switch field {
        case .detected: return detectedHeading
        case .expected: return expectedHeading
        case .recovery: return recoveryHeading
        case .nextAction: return actionHeading
        }
    }

    private func valueHeight(for field: HomeRouteField, width: CGFloat) -> CGFloat {
        switch field {
        case .detected:
            if let proofList { return proofList.height(forWidth: width) }
            return detectedNote.height(forWidth: width)
        case .expected:
            return labelHeight(expectedLabel.stringValue, Metric.noteFont, width: width)
        case .recovery:
            guard recoveryText != nil else { return 0 }
            return labelHeight(recoveryLabel.stringValue, Metric.noteFont, width: width)
        case .nextAction:
            return actionButton.frame.height
        }
    }

    private func value(for field: HomeRouteField) -> NSView {
        switch field {
        case .detected: return proofList ?? detectedNote
        case .expected: return expectedLabel
        case .recovery: return recoveryLabel
        case .nextAction: return actionButton
        }
    }

    private func walk(width: CGFloat, place: Bool) -> CGFloat {
        let inset: CGFloat = primary ? 20 : 16
        let inner = max(80, width - inset * 2)
        var y = inset

        let titleHeight = labelHeight(titleLabel.stringValue, titleLabel.font ?? Metric.labelFont,
                                      width: inner)
        if place { titleLabel.frame = NSRect(x: inset, y: y, width: inner, height: titleHeight) }
        y += titleHeight + 7

        let summaryHeight = labelHeight(summaryLabel.stringValue, Metric.noteFont, width: inner)
        if place { summaryLabel.frame = NSRect(x: inset, y: y, width: inner, height: summaryHeight) }
        y += summaryHeight

        if let noticeLabel {
            y += 10
            let noticeHeight = labelHeight(noticeLabel.stringValue,
                                           noticeLabel.font ?? Metric.noteFont, width: inner)
            if place {
                noticeLabel.frame = NSRect(x: inset, y: y, width: inner, height: noticeHeight)
            }
            y += noticeHeight
        }

        y += 15
        if place { rule.frame = NSRect(x: inset, y: y, width: inner, height: 1) }
        y += 15

        fieldPlan.walk { field in
            let label = heading(for: field)
            if place { label.frame = NSRect(x: inset, y: y, width: inner, height: 13) }
            y += 18
            let height = valueHeight(for: field, width: inner)
            if place {
                value(for: field).frame = NSRect(x: inset, y: y, width: inner, height: height)
            }
            y += height + 13
        }
        return y + inset - 13
    }

    func height(forWidth width: CGFloat) -> CGFloat { walk(width: width, place: false) }

    override func layout() {
        super.layout()
        _ = walk(width: bounds.width, place: true)
    }

    override func draw(_ dirtyRect: NSRect) {
        let path = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5),
                                xRadius: primary ? 12 : 9, yRadius: primary ? 12 : 9)
        let fill = primary
            ? Style.accent.withAlphaComponent(0.075)
            : Style.chipFill.withAlphaComponent(0.72)
        fill.setFill()
        path.fill()
        (primary ? Style.accent.withAlphaComponent(0.52) : Style.chipEdge).setStroke()
        path.lineWidth = primary ? 1.25 : 1
        path.stroke()
    }
}


/// AppKit owns this singleton on the main thread. The only background closure captures it weakly
/// and returns through `DispatchQueue.main` before reading or mutating any field.
final class HomeWindow: NSObject, NSWindowDelegate, @unchecked Sendable {
    static let shared = HomeWindow()

    private var window: NSWindow?
    private var scroll: NSScrollView?
    private var document: FlippedView?
    private var titleLabel: NSTextField?
    private var welcomeLabel: NSTextField?
    private var purposeLabel: NSTextField?
    private var headerRule: Hairline?
    private var localCard: HomeRouteCard?
    private var cloudflareCard: HomeRouteCard?
    private var cloudCard: HomeRouteCard?
    private var completionStatusValue: NSTextField?
    private var timer: Timer?
    private var health = LocalHealthEvidence.notChecked
    private var healthRequestInFlight = false
    private var healthRequestGeneration = 0
    private var localConfigurationFailed = false
    private var localDeviceID: String?
    private var localToken: String?
    private var cloudflareDeviceID: String?
    private var cloudflareToken: String?
    private var cloudflareQRWindow: NSWindow?
    private var cloudPairingAttempt: CloudPairingAttempt?
    private var cloudKnownDeviceIDs = Set<String>()
    private var cloudDeviceSnapshotAvailable = false
    private var cloudDecisionCache = CloudPreviewPolicy.decide(
        account: .reading, machineCredential: .reading,
        relayReady: .unavailable, e2eePairing: .unavailable, viewerReceipt: .unavailable)
    private var cloudflaredInstalled = false
    private var evidenceRefreshInFlight = false
    private var lastEvidenceRefreshAt: Date?
    private let evidenceQueue = DispatchQueue(label: "clawdline.onboarding.evidence",
                                               qos: .utility)
    private var completionRecorded = false
    private var completionAttempted = false

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
        let frame = NSRect(x: 0, y: 0, width: 820, height: 740)
        let content = FlippedView(frame: frame)
        content.wantsLayer = true
        content.layer?.backgroundColor = Style.ink.cgColor

        let scroll = NSScrollView(frame: content.bounds)
        scroll.autoresizingMask = [.width, .height]
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = false
        scroll.autohidesScrollers = true
        scroll.drawsBackground = false
        scroll.contentView.drawsBackground = false
        content.addSubview(scroll)
        self.scroll = scroll

        let document = FlippedView(frame: frame)
        scroll.documentView = document
        self.document = document

        let title = makeLabel(L.t.homeTitle,
                              NSFont.systemFont(ofSize: 27, weight: .semibold), Metric.label)
        let welcome = makeLabel(L.t.homeWelcome, Metric.noteFont, Metric.soft)
        let purpose = makeLabel(L.t.homePurpose,
                                NSFont.systemFont(ofSize: 13, weight: .semibold), Style.accent)
        let rule = Hairline()
        [title, welcome, purpose, rule].forEach(document.addSubview)
        titleLabel = title
        welcomeLabel = welcome
        purposeLabel = purpose
        headerRule = rule

        let local = HomeRouteCard(
            title: L.t.homeLocalTitle, summary: L.t.homeLocalSummary,
            notice: L.t.setupLocalReadOnlyAction, primary: true,
            action: { [weak self] in self?.performLocalAction() })
        let cloudflare = HomeRouteCard(
            title: L.t.homeCloudflareTitle, summary: L.t.homeCloudflareSummary,
            primary: false, action: { [weak self] in self?.performCloudflareAction() })
        let cloud = HomeRouteCard(
            title: L.t.homeCloudPreviewTitle, summary: L.t.homeCloudPreviewSummary,
            primary: false, proofList: true,
            action: { [weak self] in self?.performCloudAction() })
        [local, cloudflare, cloud].forEach(document.addSubview)
        localCard = local
        cloudflareCard = cloudflare
        cloudCard = cloud

        let completionStatus = makeLabel(L.t.setupDismissalHint, Metric.hintFont, Metric.faint)
        completionStatus.isSelectable = true
        document.addSubview(completionStatus)
        completionStatusValue = completionStatus

        let window = NSWindow(contentRect: frame,
                              styleMask: [.titled, .closable, .miniaturizable, .resizable],
                              backing: .buffered, defer: false)
        window.title = L.t.homeTitle
        window.titlebarAppearsTransparent = true
        window.backgroundColor = Style.ink
        window.appearance = NSAppearance(named: .darkAqua)
        window.minSize = NSSize(width: 680, height: 620)
        window.contentView = content
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.initialFirstResponder = content
        layoutHome()
        return window
    }

    func windowDidResize(_ notification: Notification) {
        guard notification.object as? NSWindow === window else { return }
        layoutHome()
    }

    private func layoutHome() {
        guard let scroll, let document, let titleLabel, let welcomeLabel, let purposeLabel,
              let headerRule, let localCard, let cloudflareCard, let cloudCard,
              let completionStatusValue else { return }

        let width = max(640, scroll.contentSize.width)
        let inset: CGFloat = 30
        let inner = width - inset * 2
        var y: CGFloat = 27

        let titleHeight = labelHeight(titleLabel.stringValue,
                                      titleLabel.font ?? Metric.labelFont, width: inner)
        titleLabel.frame = NSRect(x: inset, y: y, width: inner, height: titleHeight)
        y += titleHeight + 8

        let welcomeWidth = min(inner, Metric.maxMeasure)
        let welcomeHeight = labelHeight(welcomeLabel.stringValue, Metric.noteFont,
                                        width: welcomeWidth)
        welcomeLabel.frame = NSRect(x: inset, y: y, width: welcomeWidth, height: welcomeHeight)
        y += welcomeHeight + 18

        purposeLabel.frame = NSRect(x: inset, y: y, width: inner, height: 17)
        y += 25
        headerRule.frame = NSRect(x: inset, y: y, width: inner, height: 1)
        y += 18

        let localHeight = localCard.height(forWidth: inner)
        localCard.frame = NSRect(x: inset, y: y, width: inner, height: localHeight)
        y += localHeight + 16

        let twoUp = inner >= 700
        if twoUp {
            let gap: CGFloat = 16
            let cardWidth = (inner - gap) / 2
            let cloudflareHeight = cloudflareCard.height(forWidth: cardWidth)
            let cloudHeight = cloudCard.height(forWidth: cardWidth)
            cloudflareCard.frame = NSRect(x: inset, y: y, width: cardWidth,
                                          height: cloudflareHeight)
            cloudCard.frame = NSRect(x: inset + cardWidth + gap, y: y, width: cardWidth,
                                     height: cloudHeight)
            y += max(cloudflareHeight, cloudHeight) + 18
        } else {
            let cloudflareHeight = cloudflareCard.height(forWidth: inner)
            cloudflareCard.frame = NSRect(x: inset, y: y, width: inner,
                                          height: cloudflareHeight)
            y += cloudflareHeight + 14
            let cloudHeight = cloudCard.height(forWidth: inner)
            cloudCard.frame = NSRect(x: inset, y: y, width: inner, height: cloudHeight)
            y += cloudHeight + 18
        }

        let footerWidth = min(inner, Metric.maxMeasure)
        let footerHeight = labelHeight(completionStatusValue.stringValue,
                                       Metric.hintFont, width: footerWidth)
        completionStatusValue.frame = NSRect(x: inset, y: y,
                                             width: footerWidth, height: footerHeight)
        y += footerHeight + 28
        document.frame = NSRect(x: 0, y: 0, width: width, height: y.rounded())
        document.needsLayout = true
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
        let state: String
        switch current.phase {
        case .serverOff: state = L.t.setupLocalServerOff
        case .configurationFailed: state = L.t.setupLocalConfigurationFailed
        case .checkingHealth: state = L.t.setupLocalChecking
        case .healthFailed(let failure): state = L.t.setupLocalHealthFailure(failure)
        case .readyToOpen: state = L.t.setupLocalReady
        case .awaitingDevice: state = L.t.setupLocalWaiting
        case .connected: state = L.t.setupLocalConnected
        }

        let actionTitle: String
        switch current.action {
        case .enableServer: actionTitle = L.t.setupLocalEnable
        case .retryHealth: actionTitle = L.t.setupLocalRetry
        case .openBrowser: actionTitle = L.t.setupLocalOpen
        case .openBrowserAgain: actionTitle = L.t.setupLocalOpenAgain
        case .finish: actionTitle = L.t.setupFinish
        }
        localCard?.update(
            state: state, dot: localDot(for: current.phase),
            expected: L.t.setupLocalExpected(current.phase),
            recovery: L.t.setupLocalRecovery(current.phase),
            actionTitle: actionTitle)
        if current.succeeded { recordCompletion(for: .routeSucceeded) }

        refreshCloudflare()
        refreshCloud()
        layoutHome()
    }

    private func localDot(for phase: LocalBrowserPhase) -> PixelDot.State {
        switch phase {
        case .checkingHealth: return .busy
        case .configurationFailed, .healthFailed: return .warn
        case .readyToOpen, .awaitingDevice, .connected: return .live
        case .serverOff: return .idle
        }
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
        case .cloudflaredMissing: status = L.t.setupTunnelMissing
        case .tunnelOff: status = L.t.setupTunnelOff
        case .starting: status = L.t.setupTunnelStarting
        case .ready(let url): status = L.t.setupTunnelReady(url)
        case .awaitingDevice(let url): status = L.t.setupTunnelWaiting(url)
        case .connected(let url): status = L.t.setupTunnelConnected(url)
        case .failed(let reason): status = reason
        }

        let actionTitle: String
        switch current.action {
        case .installCloudflared, .openSettings:
            actionTitle = L.t.setupOpenRemoteSettings
        case .checkAgain:
            actionTitle = L.t.setupCheckTunnel
        case .showQR:
            actionTitle = L.t.setupShowPhoneQR
        case .showQRAgain:
            actionTitle = L.t.setupShowPhoneQRAgain
        case .finish:
            actionTitle = L.t.setupFinish
        }
        cloudflareCard?.update(
            state: L.t.setupTunnelFacts(modeName, tunnelName, shownHost, status + "\n" + access),
            dot: cloudflareDot(for: current.phase),
            expected: L.t.setupTunnelExpected(current.phase),
            recovery: L.t.setupTunnelRecovery(current.phase),
            actionTitle: actionTitle)

        if current.qrURL == nil {
            closeCloudflareQR(revokingUnseenCredential: true)
        }
        if current.succeeded { recordCompletion(for: .routeSucceeded) }
    }

    private func cloudflareDot(for phase: CloudflareOnboardingPhase) -> PixelDot.State {
        switch phase {
        case .starting: return .busy
        case .failed, .cloudflaredMissing: return .warn
        case .ready, .awaitingDevice, .connected: return .live
        case .tunnelOff: return .idle
        }
    }

    private func refreshCloud() {
        let current = cloudDecisionCache
        let facts = L.t.setupCloudFacts(
            cloudProof(current.account, kind: .account),
            cloudProof(current.machineCredential, kind: .credential),
            cloudProof(current.relayReady, kind: .relay),
            cloudProof(current.e2eePairing, kind: .pairing),
            cloudProof(current.viewerReceipt, kind: .receipt))

        let actionTitle: String
        switch current.action {
        case .openCloudSettings: actionTitle = L.t.setupOpenCloudSettings
        case .pairPhone: actionTitle = L.t.setupPairCloudPhone
        case .reviewPreviewStatus: actionTitle = L.t.setupReviewCloudPreview
        }
        cloudCard?.update(
            state: facts, dot: .idle,
            expected: L.t.setupCloudExpected(current),
            recovery: L.t.setupCloudRecovery(current),
            actionTitle: actionTitle)
    }

    private enum CloudProofKind { case account, credential, relay, pairing, receipt }

    private func cloudProof(_ proof: CloudPreviewProof, kind: CloudProofKind) -> String {
        switch proof {
        case .reading:
            return L.t.setupProofReading
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
        content.wantsLayer = true
        content.layer?.backgroundColor = Style.ink.cgColor
        let title = makeLabel(L.t.setupScanLiveTunnel,
                              NSFont.systemFont(ofSize: 16, weight: .semibold), Metric.label)
        title.alignment = .center
        title.frame = NSRect(x: 24, y: side + 82, width: side, height: 24)
        content.addSubview(title)
        let image = NSImageView(frame: NSRect(x: 24, y: 60, width: side, height: side))
        image.image = RemoteQR.image(for: url, side: side)
        image.imageScaling = .scaleNone
        image.wantsLayer = true
        image.layer?.backgroundColor = NSColor.white.cgColor
        content.addSubview(image)
        let address = makeLabel(url, Metric.monoFont, Metric.faint)
        address.font = NSFont.monospacedSystemFont(ofSize: 9, weight: .regular)
        address.alignment = .center
        address.isSelectable = true
        address.frame = NSRect(x: 24, y: 14, width: side, height: 38)
        content.addSubview(address)
        let qr = NSWindow(contentRect: content.frame, styleMask: [.titled, .closable],
                          backing: .buffered, defer: false)
        qr.title = L.t.homeCloudflareTitle
        qr.titlebarAppearsTransparent = true
        qr.backgroundColor = Style.ink
        qr.appearance = NSAppearance(named: .darkAqua)
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
