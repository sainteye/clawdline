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

    private func deliver(_ notice: PairingNotice) {
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
