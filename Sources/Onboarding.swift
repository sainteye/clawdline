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
        let frame = NSRect(x: 0, y: 0, width: 760, height: 680)
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
        let preview = card(title: L.t.homeCloudPreviewTitle,
                           summary: L.t.homeCloudPreviewSummary + "\n\n" + L.t.homeUnavailable)
        let cloudflare = card(title: L.t.homeCloudflareTitle,
                              summary: L.t.homeCloudflareSummary + "\n\n" + L.t.homeUnavailable)
        remoteCards.addArrangedSubview(preview.box)
        remoteCards.addArrangedSubview(cloudflare.box)
        stack.addArrangedSubview(remoteCards)
        NSLayoutConstraint.activate([
            local.box.widthAnchor.constraint(equalTo: stack.widthAnchor),
            remoteCards.widthAnchor.constraint(equalTo: stack.widthAnchor),
        ])

        let window = NSWindow(contentRect: frame,
                              styleMask: [.titled, .closable, .miniaturizable, .resizable],
                              backing: .buffered, defer: false)
        window.title = L.t.homeTitle
        window.minSize = NSSize(width: 680, height: 620)
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
}
#endif
