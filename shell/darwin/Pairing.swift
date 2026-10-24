// The shell's half of signing in and pairing: the window carries this
// machine's own token, and a pairing code is put on this screen.
//
// Both read the token the daemon writes when it starts, at
// <NextConfig.directory>/local-token (0600). Never ~/.config/clawdline: that
// is the Swift app's, and nothing of its is read or presented here.
import AppKit
import Foundation
import WebKit

enum LocalToken {
    static var fileURL: URL { NextConfig.directory.appendingPathComponent("local-token") }

    /// The token as the daemon last wrote it, or nil before it has.
    static func read() -> String? {
        guard let data = try? Data(contentsOf: fileURL),
              let text = String(data: data, encoding: .utf8) else { return nil }
        let token = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return token.isEmpty ? nil : token
    }

    /// Put the token into the window's cookie store as the daemon's own
    /// `Set-Cookie` would: `clawdline-next`, host-only, `Path=/`, a year,
    /// HttpOnly, SameSite=Strict.
    ///
    /// **A cookie put there, not `/v1/auth/adopt`.** Adopting means handing the
    /// token to the page — in a fragment or a script — and letting the page
    /// trade it; the console this window shows does not read a fragment yet,
    /// and a credential that passes through page script or an address bar is
    /// one more place it has been. Set directly, it goes from the 0600 file to
    /// WebKit's cookie store and nowhere else, before the first request, with
    /// no round trip to wait on. HttpOnly keeps the page's script from reading
    /// it either way.
    ///
    /// Done before every load rather than once: the daemon replaces the file
    /// when it no longer matches its store, and a reload is how the window
    /// picks up the new one.
    static func install(in store: WKHTTPCookieStore, for home: URL, then done: @escaping (Bool) -> Void) {
        guard let token = read() else {
            shellLog("auth: no local token at \(fileURL.path) yet; loading without one")
            done(false)
            return
        }
        var properties: [HTTPCookiePropertyKey: Any] = [
            .name: "clawdline-next",
            .value: token,
            .domain: home.host ?? "127.0.0.1",
            .path: "/",
            .expires: Date().addingTimeInterval(31_536_000),
            HTTPCookiePropertyKey("HttpOnly"): "TRUE",
        ]
        properties[.sameSitePolicy] = HTTPCookieStringPolicy.sameSiteStrict.rawValue
        guard let cookie = HTTPCookie(properties: properties) else {
            shellLog("auth: the local token did not make a cookie; loading without one")
            done(false)
            return
        }
        store.setCookie(cookie) {
            // The path, never the token.
            shellLog("auth: window carries the local token from \(fileURL.path)")
            done(true)
        }
    }
}

/// What a `pairing` event carries (api/v1/auth.schema.json PairingNotice).
struct PairingNotice: Decodable {
    let pairing_id: String
    let name: String
    let code: String
    let expires: Int
}

/// Listens on /v1/auth/pairings — the stream only this machine's own token may
/// open — and hands each pairing to `onPairing` on the main queue, one at a
/// time.
///
/// **One at a time is decided here, not on the main queue.** The alert is
/// `runModal`, which runs inside a main-queue block, and a serial queue does
/// not start the next block until that one returns — so a check made on the
/// main queue never sees an alert on screen, and a burst of requests becomes a
/// line of alerts shown one after another. The Swift app's guard reads as if
/// it dropped them; this is where that drop can actually happen. A pairing that
/// arrives while an alert is up is not shown, and the person at the Mac sees
/// the one they are already looking at. `alertClosed()` opens the gate again.
///
/// Reconnects when the stream ends, less often each time, re-reading the token
/// file on every attempt: the daemon may not be up yet when the shell starts,
/// and may have replaced the token when it restarts.
///
/// `onPairing` is set once, on the main queue, before `start`, and only ever
/// called there, and `showing` is only touched under `gate`; that is what the
/// unchecked conformance rests on.
final class PairingWatcher: @unchecked Sendable {
    var onPairing: ((PairingNotice) -> Void)?
    private let base: URL
    private var task: Task<Void, Never>?
    private let gate = NSLock()
    private var showing = false

    init(base: URL) { self.base = base }

    func start() {
        guard task == nil else { return }
        task = Task.detached { [weak self] in
            var failures = 0
            while !Task.isCancelled {
                guard let self else { return }
                let reason = await self.listenOnce()
                failures += 1
                let delay = min(Double(failures) * 2, 30)
                shellLog("pairing: stream ended (\(reason)); again in \(Int(delay))s")
                try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
    }

    /// The alert has been dismissed; the next pairing may be shown.
    func alertClosed() {
        gate.lock()
        showing = false
        gate.unlock()
    }

    /// Internal rather than private so the gate can be driven by a test.
    func deliver(_ notice: PairingNotice) {
        gate.lock()
        if showing {
            gate.unlock()
            shellLog("pairing: another request while one was on screen — ignored")
            return
        }
        showing = true
        gate.unlock()
        DispatchQueue.main.async { [weak self] in self?.onPairing?(notice) }
    }

    private func listenOnce() async -> String {
        guard let token = LocalToken.read() else { return "no local token yet" }
        var request = URLRequest(url: base.appendingPathComponent("v1/auth/pairings"))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        request.timeoutInterval = 60 * 60 * 24
        do {
            let (bytes, response) = try await URLSession.shared.bytes(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            guard status == 200 else { return "HTTP \(status)" }
            shellLog("pairing: listening")
            var event = ""
            // Blank lines are not relied on: the daemon writes each event's data
            // on the line after its name, so the pair is handled as it arrives.
            for try await line in bytes.lines {
                if line.hasPrefix("event:") {
                    event = line.dropFirst(6).trimmingCharacters(in: .whitespaces)
                } else if line.hasPrefix("data:"), event == "pairing" {
                    event = ""
                    let json = Data(line.dropFirst(5).trimmingCharacters(in: .whitespaces).utf8)
                    guard let notice = try? JSONDecoder().decode(PairingNotice.self, from: json) else {
                        shellLog("pairing: an event that was not a pairing")
                        continue
                    }
                    deliver(notice)
                }
            }
            return "closed"
        } catch {
            return error.localizedDescription
        }
    }
}

/// The two grants a pairing may be given (api/v1/auth.schema.json
/// DevicePermission). Read-and-operate stays the daemon's default for a
/// pairing nobody chose for; the shell never relies on that default and always
/// writes the one the person picked.
enum PairingPermission: String, CaseIterable {
    case readOnly = "read-only"
    case readAndOperate = "read-and-operate"
}

/// What saving the choice came to.
enum PairingPermissionOutcome: Equatable {
    case saved
    /// 404: the pairing is gone or past its two minutes.
    case expired
    /// Any other answer from the daemon, with its status.
    case refused(Int)
    /// No answer at all, or no local token to ask with.
    case unreachable
}

/// POST /v1/auth/pairings/{pairing_id}/permission with this machine's token.
/// Only a 200 that says `{"ok":true}` counts as saved: anything less and the
/// code is not shown.
struct PairingPermissionClient {
    /// How long one save may take: well inside the two minutes a code lives,
    /// so the gate is never held by a request that will not end. Registered as
    /// console.native_pairing_save_seconds (internal/domain/capacity
    /// register_console.go; native_pairing_test.go holds the two together).
    static let saveTimeoutSeconds: TimeInterval = 15

    let base: URL
    let token: () -> String?
    let session: URLSession

    init(base: URL, token: @escaping () -> String? = LocalToken.read, session: URLSession = .shared) {
        self.base = base
        self.token = token
        self.session = session
    }

    func request(pairingID: String, permission: PairingPermission, token: String) -> URLRequest? {
        var segment = CharacterSet.urlPathAllowed
        segment.remove(charactersIn: "/")
        guard !pairingID.isEmpty,
              let id = pairingID.addingPercentEncoding(withAllowedCharacters: segment),
              let url = URL(string: "v1/auth/pairings/\(id)/permission", relativeTo: base)?.absoluteURL
        else { return nil }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? JSONSerialization.data(withJSONObject: ["permission": permission.rawValue])
        request.timeoutInterval = Self.saveTimeoutSeconds
        return request
    }

    static func outcome(status: Int?, body: Data?) -> PairingPermissionOutcome {
        guard let status else { return .unreachable }
        switch status {
        case 200:
            let object = body.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any]
            return object?["ok"] as? Bool == true ? .saved : .refused(status)
        case 404: return .expired
        default: return .refused(status)
        }
    }

    /// Asks off the main queue and answers on it.
    func save(_ notice: PairingNotice, _ permission: PairingPermission,
              then done: @escaping (PairingPermissionOutcome) -> Void) {
        guard let token = token(),
              let request = request(pairingID: notice.pairing_id, permission: permission, token: token) else {
            DispatchQueue.main.async { done(.unreachable) }
            return
        }
        session.dataTask(with: request) { data, response, _ in
            let status = (response as? HTTPURLResponse)?.statusCode
            let result = Self.outcome(status: status, body: data)
            DispatchQueue.main.async { done(result) }
        }.resume()
    }
}

/// What the person at the machine answered.
enum PairingChoice: Equatable {
    case grant(PairingPermission)
    case ignore
}

/// The screens one pairing can put up, kept apart from AppKit so the flow can
/// be driven without one.
struct PairingScreens {
    /// Pick a permission, or ignore. Nothing about the code is on this screen.
    var choose: (PairingNotice) -> PairingChoice
    /// The code and the permission that was saved for it.
    var showCode: (PairingNotice, PairingPermission) -> Void
    /// Saving failed: true to try again, false to ignore the pairing.
    var showFailure: (PairingNotice, PairingPermissionOutcome) -> Bool
}

/// One pairing, from the request to the code: choose, save, and only then
/// show the code. Every path ends in `finished` exactly once, which is what
/// lets PairingWatcher show the next pairing.
///
/// Runs on the main queue; `save` answers there.
final class PairingPermissionFlow {
    typealias Save = (PairingNotice, PairingPermission, @escaping (PairingPermissionOutcome) -> Void) -> Void

    private let notice: PairingNotice
    private let screens: PairingScreens
    private let save: Save
    private let now: () -> Date
    private let finished: () -> Void
    private var ended = false

    init(_ notice: PairingNotice, screens: PairingScreens, save: @escaping Save,
         now: @escaping () -> Date = Date.init, finished: @escaping () -> Void) {
        self.notice = notice
        self.screens = screens
        self.save = save
        self.now = now
        self.finished = finished
    }

    private var expired: Bool { now().timeIntervalSince1970 >= Double(notice.expires) }

    func start() {
        guard !expired else { return end("expired before it was shown") }
        switch screens.choose(notice) {
        case .ignore:
            end("ignored")
        case .grant(let permission):
            store(permission)
        }
    }

    private func store(_ permission: PairingPermission) {
        // Logs the choice, never the code.
        shellLog("pairing: saving \(permission.rawValue) for \(notice.name)")
        save(notice, permission) { [self] outcome in
            var outcome = outcome
            if outcome == .saved, expired { outcome = .expired }
            if outcome == .saved {
                screens.showCode(notice, permission)
                return end("code shown with \(permission.rawValue)")
            }
            shellLog("pairing: permission not saved (\(outcome))")
            if outcome != .expired, screens.showFailure(notice, outcome) {
                return store(permission)
            }
            if outcome == .expired { _ = screens.showFailure(notice, outcome) }
            end("not paired")
        }
    }

    private func end(_ why: String) {
        guard !ended else { return }
        ended = true
        shellLog("pairing: \(notice.name) — \(why)")
        finished()
    }
}


/// Admission for the Cloud-only local pairing bridge. Both the initiating
/// frame and the currently displayed top-level page must be the trusted origin.
enum CloudPairingAccess {
    static func allows(mainFrame: Bool, scheme: String, host: String, port: Int,
                       page: URL?, trusted: URL) -> Bool {
        guard mainFrame, let page else { return false }
        let originPort: Int? = port == 0 ? nil : port
        return scheme.lowercased() == trusted.scheme?.lowercased() &&
            host.lowercased() == trusted.host?.lowercased() && originPort == trusted.port &&
            page.scheme?.lowercased() == trusted.scheme?.lowercased() &&
            page.host?.lowercased() == trusted.host?.lowercased() && page.port == trusted.port
    }
}
