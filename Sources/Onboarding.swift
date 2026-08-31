import Foundation

/// The launch decision is deliberately independent of AppKit and config.json. A config switch
/// says what a service should do; it cannot also mean that a person finished being introduced to
/// the app, because hand-editing that service would then silently dismiss the first window.
enum AppLaunchPolicy {
    static func shouldShowHome(onboardingComplete: Bool) -> Bool { !onboardingComplete }
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

enum LocalBrowserPhase: Equatable {
    case serverOff
    case checkingHealth
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
    static func decide(serverEnabled: Bool, healthReady: Bool,
                       deviceCreated: Bool, deviceLastSeen: Bool) -> LocalBrowserDecision {
        guard serverEnabled else {
            return LocalBrowserDecision(phase: .serverOff, action: .enableServer,
                                        mayMintDevice: false, succeeded: false)
        }
        guard healthReady else {
            return LocalBrowserDecision(phase: .checkingHealth, action: .retryHealth,
                                        mayMintDevice: false, succeeded: false)
        }
        guard deviceCreated else {
            return LocalBrowserDecision(phase: .readyToOpen, action: .openBrowser,
                                        mayMintDevice: true, succeeded: false)
        }
        guard deviceLastSeen else {
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
                       deviceCreated: Bool, exactDeviceIsNonLocal: Bool,
                       exactDeviceLastSeen: Bool) -> CloudflareOnboardingDecision {
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
            guard exactDeviceIsNonLocal, exactDeviceLastSeen else {
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

enum CloudPreviewProof: Equatable {
    case absent
    case notProved
    case unavailable
    case proved(String)
    case failed(String)
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

#if !ONBOARDING_POLICY_ONLY
import AppKit

final class HomeWindow: NSObject, NSWindowDelegate {
    static let shared = HomeWindow()

    private var window: NSWindow?
    private var stateValue: NSTextField?
    private var actionButton: NSButton?
    private var timer: Timer?
    private var healthReady = false
    private var healthRequestGeneration = 0
    private var localDeviceID: String?
    private var localToken: String?
    private var cloudflareStateValue: NSTextField?
    private var cloudflareRecoveryValue: NSTextField?
    private var cloudflareActionButton: NSButton?
    private var cloudflareDeviceID: String?
    private var cloudflareToken: String?
    private var cloudflareQRWindow: NSWindow?
    private var cloudStateValue: NSTextField?
    private var cloudRecoveryValue: NSTextField?
    private var cloudActionButton: NSButton?
    private var cloudRouteStartedAtMilliseconds = Int64(Date().timeIntervalSince1970 * 1_000)
    private var cloudPairedDeviceID: String?
    private var completionRecorded = false

    private override init() { super.init() }

    func show() {
        if window == nil { window = buildWindow() }
        refresh()
        startPolling()
        window?.center()
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func rebuildIfVisible() {
        guard window?.isVisible == true else { return }
        timer?.invalidate()
        window?.orderOut(nil)
        window = nil
        show()
    }

    func windowWillClose(_ notification: Notification) {
        timer?.invalidate()
        timer = nil
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
        detail.addArrangedSubview(label(L.t.setupReadOnly, size: 13,
                                        color: .secondaryLabelColor))
        detail.addArrangedSubview(row(L.t.setupDetected, value: state))
        detail.addArrangedSubview(row(L.t.setupExpected,
                                      value: label(L.t.setupLocalExpected, size: 13)))
        detail.addArrangedSubview(row(L.t.setupRecovery,
                                      value: label(L.t.setupLocalRecovery, size: 13)))
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
        cloudflareRecoveryValue = cloudflareRecovery
        cloudflareActionButton = cloudflareAction
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupDetected, value: cloudflareState))
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupNextAction, value: cloudflareAction))
        cloudflare.content.addArrangedSubview(compactRow(
            L.t.setupExpected, value: label(L.t.setupTunnelExpected, size: 12)))
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
        cloudRecoveryValue = cloudRecovery
        cloudActionButton = cloudAction
        preview.content.addArrangedSubview(compactRow(L.t.setupDetected, value: cloudState))
        preview.content.addArrangedSubview(compactRow(L.t.setupNextAction, value: cloudAction))
        preview.content.addArrangedSubview(compactRow(
            L.t.setupExpected, value: label(L.t.setupCloudExpected, size: 12)))
        preview.content.addArrangedSubview(compactRow(L.t.setupRecovery, value: cloudRecovery))

        remoteCards.addArrangedSubview(cloudflare.box)
        remoteCards.addArrangedSubview(preview.box)
        stack.addArrangedSubview(remoteCards)
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
        let seen = localDeviceID.flatMap { id in
            RemoteAuth.approvedDevices.first(where: { $0.id == id })?.lastSeen
        } != nil
        return LocalBrowserPolicy.decide(serverEnabled: Config.shared.remote,
                                         healthReady: healthReady,
                                         deviceCreated: localDeviceID != nil,
                                         deviceLastSeen: seen)
    }

    private var cloudflareDecision: CloudflareOnboardingDecision {
        let device = cloudflareDeviceID.flatMap { id in
            RemoteAuth.approvedDevices.first(where: { $0.id == id })
        }
        let tunnel: CloudflareOnboardingTunnel
        switch RemoteTunnel.shared.state {
        case .off: tunnel = .off
        case .starting: tunnel = .starting
        case .up(let url): tunnel = .up(url: url)
        case .failed(let reason): tunnel = .failed(reason: reason)
        }
        return CloudflareOnboardingPolicy.decide(
            cloudflaredInstalled: RemoteTunnel.isInstalled, tunnel: tunnel,
            deviceCreated: device != nil, exactDeviceIsNonLocal: device?.local == false,
            exactDeviceLastSeen: device?.lastSeen != nil)
    }

    private var cloudDecision: CloudPreviewDecision {
        let identity: CloudMachineIdentity?
        let account: CloudPreviewProof
        let credential: CloudPreviewProof
        do {
            identity = try CloudAccountClient().restoredMachineIdentity()
            if let identity {
                account = .proved(identity.accountID)
                credential = .proved(identity.machineID)
            } else {
                account = .absent
                credential = .absent
            }
        } catch {
            identity = nil
            account = .notProved
            credential = .failed(Self.message(for: error))
        }

        let relay: CloudPreviewProof
        let lifecycle = MainActor.assumeIsolated { CloudBridgeLifecycle.shared.state }
        switch lifecycle {
        case .unauthorized:
            relay = .failed(L.t.setupCloudRelayUnauthorized)
        case .failed(let reason):
            relay = .failed(reason)
        case .detached, .attached:
            // `attached` means a bridge was built. The lifecycle exposes no ready-frame fact, so
            // it cannot be promoted to relay-ready on this card.
            relay = .notProved
        }

        let pairing: CloudPreviewProof
        if let identity {
            do {
                let devices = try CloudPairedDeviceStore().devices(accountID: identity.accountID)
                let exact: CloudPairedDevice?
                if let bound = cloudPairedDeviceID {
                    exact = devices.first(where: { $0.deviceID == bound })
                } else {
                    exact = devices
                        .filter({ $0.pairedAtMilliseconds >= cloudRouteStartedAtMilliseconds })
                        .min(by: { $0.pairedAtMilliseconds < $1.pairedAtMilliseconds })
                    cloudPairedDeviceID = exact?.deviceID
                }
                if let exact {
                    pairing = .proved(exact.deviceID)
                } else {
                    pairing = .absent
                }
            } catch {
                pairing = .failed(Self.message(for: error))
            }
        } else {
            pairing = .absent
        }

        return CloudPreviewPolicy.decide(
            account: account, machineCredential: credential, relayReady: relay,
            e2eePairing: pairing, viewerReceipt: .unavailable)
    }

    private func refresh() {
        let current = decision
        switch current.phase {
        case .serverOff: stateValue?.stringValue = L.t.setupLocalServerOff
        case .checkingHealth: stateValue?.stringValue = L.t.setupLocalChecking
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
        if current.succeeded, !completionRecorded {
            completionRecorded = OnboardingCompletionStore.production.markCurrent()
        }

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
            ?? Config.shared.remoteHostname.trimmingCharacters(in: .whitespaces)
        let shownHost = hostname.isEmpty ? L.t.setupNone : hostname
        let access = Config.shared.remoteWrite ? L.t.setupControlChosen : L.t.setupReadOnly

        let status: String
        let recovery: String
        switch current.phase {
        case .cloudflaredMissing:
            status = L.t.setupTunnelMissing
            recovery = L.t.setupTunnelRecovery
        case .tunnelOff:
            status = L.t.setupTunnelOff
            recovery = L.t.setupTunnelRecovery
        case .starting:
            status = L.t.setupTunnelStarting
            recovery = L.t.setupTunnelRecovery
        case .ready(let url):
            status = L.t.setupTunnelReady(url)
            recovery = L.t.setupTunnelRecovery
        case .awaitingDevice(let url):
            status = L.t.setupTunnelWaiting(url)
            recovery = L.t.setupTunnelRecovery
        case .connected(let url):
            status = L.t.setupTunnelConnected(url)
            recovery = L.t.setupTunnelRecovery
        case .failed(let reason):
            status = reason
            recovery = L.t.setupTunnelRecovery
        }
        cloudflareStateValue?.stringValue = L.t.setupTunnelFacts(
            modeName, tunnelName, shownHost, status + "\n" + access)
        cloudflareRecoveryValue?.stringValue = recovery
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
            cloudflareQRWindow?.orderOut(nil)
            cloudflareQRWindow = nil
        }
        if current.succeeded, !completionRecorded {
            completionRecorded = OnboardingCompletionStore.production.markCurrent()
        }
    }

    private func refreshCloud() {
        let current = cloudDecision
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
        cloudRecoveryValue?.stringValue = L.t.setupCloudRecovery
    }

    private enum CloudProofKind { case account, credential, relay, pairing, receipt }

    private func cloudProof(_ proof: CloudPreviewProof, kind: CloudProofKind) -> String {
        switch proof {
        case .absent:
            return L.t.setupProofAbsent
        case .notProved:
            return L.t.setupProofNotProved
        case .unavailable:
            return L.t.setupProofUnavailable
        case .failed(let reason):
            return L.t.setupProofFailed(reason)
        case .proved(let value):
            switch kind {
            case .account: return L.t.setupCloudAccountProved(value)
            case .credential: return L.t.setupCloudCredentialProved(value)
            case .pairing: return L.t.setupCloudPairingProved(value)
            case .relay, .receipt: return L.t.setupProofProved
            }
        }
    }

    @objc private func performLocalAction() {
        let current = decision
        switch current.action {
        case .enableServer:
            Config.shared.remote = true
            Config.shared.remoteWrite = false
            _ = Config.shared.save()
            NotificationCenter.default.post(name: .clawdlineConfigChanged, object: nil)
            healthReady = false
            checkHealth()
        case .retryHealth:
            checkHealth()
        case .openBrowser:
            guard current.mayMintDevice else { return }
            let made = RemoteAuth.addDevice(name: L.t.setupLocalDeviceName, caps: [.read])
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
            guard current.mayMintDevice else { return }
            let caps: Set<RemoteAuth.Capability> = Config.shared.remoteWrite
                ? [.read, .send] : [.read]
            let made = RemoteAuth.addDevice(name: L.t.setupCloudflareDeviceName, caps: caps)
            cloudflareDeviceID = made.id
            cloudflareToken = made.token
            showCloudflareQR()
        case .showQRAgain:
            showCloudflareQR()
        case .finish:
            window?.performClose(nil)
        }
        refresh()
    }

    @objc private func performCloudAction() {
        // The existing Cloud settings control owns device-code login and E2EE key handover. Home
        // observes their durable evidence rather than creating a second login/pairing state machine.
        if cloudDecision.action == .pairPhone {
            cloudPairedDeviceID = nil
            cloudRouteStartedAtMilliseconds = Int64(Date().timeIntervalSince1970 * 1_000)
        }
        SettingsWindow.shared.show()
    }

    private func showCloudflareQR() {
        guard let token = cloudflareToken else { return }
        let url = RemoteQR.signInURL(token: token, hostname: Config.shared.remoteHostname,
                                     tunnel: RemoteTunnel.shared.state,
                                     port: Config.shared.remotePort)
        guard !url.isEmpty else { return }
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
        guard healthReady, let token = localToken,
              let url = URL(string: "http://127.0.0.1:\(Config.shared.remotePort)/#t=\(token)") else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    private func startPolling() {
        timer?.invalidate()
        let timer = Timer(timeInterval: 0.75, repeats: true) { [weak self] _ in
            self?.checkHealth()
            self?.refresh()
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
        checkHealth()
    }

    /// A listener object is not evidence that bytes can make the round trip. This request is the
    /// same public health endpoint the browser uses; a 200 without `ok: true` is not readiness.
    private func checkHealth() {
        guard Config.shared.remote,
              let url = URL(string: "http://127.0.0.1:\(Config.shared.remotePort)/v1/health") else {
            healthReady = false
            refresh()
            return
        }
        healthRequestGeneration += 1
        let generation = healthRequestGeneration
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData,
                                 timeoutInterval: 1.5)
        request.httpMethod = "GET"
        URLSession.shared.dataTask(with: request) { [weak self] data, response, _ in
            let status = (response as? HTTPURLResponse)?.statusCode
            let object = data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            let ready = status == 200 && object?["ok"] as? Bool == true
            DispatchQueue.main.async {
                guard let self, generation == self.healthRequestGeneration else { return }
                self.healthReady = ready
                self.refresh()
            }
        }.resume()
    }

    private static func message(for error: Error) -> String {
        if let localized = error as? LocalizedError,
           let description = localized.errorDescription, !description.isEmpty {
            return description
        }
        return error.localizedDescription
    }
}
#endif
