// Pairing the Cloud tab with the Mac it runs on, without a person carrying a
// code from one half of the window to the other.
//
// The Cloud tab is a browser of its own: a viewer device that signed in and
// holds no content key. The first time it signs in, Cloud asks it to be paired
// with a machine — show a code, run `clawdline cloud pair -offer …` on that
// machine, compare two fingerprints. When the machine is this Mac, every step
// of that is ceremony: the code would travel from this window to a terminal on
// the same computer, and the fingerprints would be compared between two things
// this app can already read.
//
// So the shell does it the other way round, the way `clawdline cloud pair`
// does without `-offer`: it asks its own daemon for an invitation, puts the
// invitation's link into the tab, answers it, and compares the fingerprints the
// page shows against the ones the daemon reports. When they agree it closes the
// card; when they do not, or anything else goes differently, it stops and
// leaves the screen as it is, for the person — the manual path is still there.
//
// **The link carries the invitation's one-time secret.** It goes from the
// daemon, behind this machine's token, into this tab's address and nowhere
// else — not the log, not a page on any other origin. The page takes it out of
// its own address as soon as it has read it (`takeInvitation`).
import AppKit
import Foundation
import WebKit

/// What the daemon says about the pairing in flight — the fields of
/// `internal/transport/cloud`'s `PairingState` this needs.
private struct DaemonPairing: Decodable {
    let phase: String
    let link: String?
    let machineFingerprint: String?
    let viewerFingerprint: String?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case phase, link, error
        case machineFingerprint = "machine_fingerprint"
        case viewerFingerprint = "viewer_fingerprint"
    }
}

@MainActor
final class CloudSelfPairing {
    private let view: WKWebView
    /// Looking at the page, or pairing: one at a time.
    private var busy = false
    /// Tried once this launch. A pairing that went differently is the person's
    /// to finish; trying again on every load would draw a new invitation each
    /// time and replace whatever they were doing with the old one.
    private var tried = false

    init(view: WKWebView) { self.view = view }

    /// After a load: if Cloud is asking this tab to be paired, pair it.
    func look() {
        guard !busy, !tried else { return }
        busy = true
        Task {
            defer { busy = false }
            // The pairing screen is drawn by script after the session is
            // registered, a moment after the load.
            let asking = "!!document.getElementById('cloud-pair-start') && !document.querySelector('.cloud-pair-ask')"
            guard await waitFor(asking, seconds: 20), !tried else { return }
            tried = true
            await pair()
        }
    }

    private func pair() async {
        // Somebody may be pairing from a terminal right now; a new invitation
        // would replace theirs.
        if let now = await daemon("GET"), now.phase == "waiting" || now.phase == "sealing" {
            shellLog("cloud: a pairing is already in progress on this Mac; leaving this tab to the person")
            return
        }
        guard let started = await daemon("POST") else { return }
        guard started.phase == "waiting",
              let raw = started.link, let link = URL(string: raw), isCloud(link),
              let fragment = link.fragment, fragment.hasPrefix("pair="),
              let machineKey = started.machineFingerprint, !machineKey.isEmpty else {
            shellLog("cloud: this Mac did not draw an invitation for the tab (\(started.phase) \(started.error ?? "")); leaving it to the person")
            return
        }
        guard let here = view.url, isCloud(here) else {
            shellLog("cloud: the tab left Cloud before it could be paired")
            return
        }
        shellLog("cloud: pairing the tab with this Mac")
        guard let hash = jsString("#" + fragment),
              await ask("location.hash = \(hash); true") as? Bool == true,
              await waitFor("!!document.querySelector('.cloud-pair-ask #cloud-pair-go') && !document.querySelector('.cloud-pair-ways')", seconds: 10),
              await ask("document.getElementById('cloud-pair-go').click(); true") as? Bool == true else {
            shellLog("cloud: the tab did not offer to answer the invitation; leaving it to the person")
            return
        }

        var ended: DaemonPairing?
        for _ in 0..<120 {
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            guard let state = await daemon("GET") else { return }
            if state.phase == "paired" || state.phase == "failed" || state.phase == "idle" {
                ended = state
                break
            }
        }
        guard let ended, ended.phase == "paired", let viewerKey = ended.viewerFingerprint else {
            shellLog("cloud: pairing the tab ended \(ended?.phase ?? "without an answer") \(ended?.error ?? ""); leaving it to the person")
            return
        }

        // The comparison the card asks the person to make, made against what
        // the daemon itself holds rather than against the page alone.
        guard await waitFor("!!document.querySelector('#cloud-pair-said[data-paired]')", seconds: 15),
              let shown = await ask("[(document.getElementById('cloud-pair-machine-key')||{}).textContent||'', (document.getElementById('cloud-pair-browser-key')||{}).textContent||'']") as? [String],
              shown.count == 2 else {
            shellLog("cloud: this Mac paired the tab, but the tab did not show its fingerprints; leaving it to the person")
            return
        }
        let trim = { (s: String) in s.trimmingCharacters(in: .whitespacesAndNewlines) }
        guard trim(shown[0]) == trim(machineKey), trim(shown[1]) == trim(viewerKey) else {
            shellLog("cloud: the fingerprints the tab shows are not the ones this Mac holds; leaving them on screen")
            return
        }
        _ = await ask("(document.getElementById('cloud-pair-close')||{click(){}}).click(); true")
        shellLog("cloud: paired the tab with this Mac (machine \(machineKey), tab \(viewerKey))")
    }

    // MARK: The daemon

    /// GET answers the pairing in flight; POST draws a new invitation.
    private func daemon(_ method: String) async -> DaemonPairing? {
        guard let token = LocalToken.read() else {
            shellLog("cloud: no local token yet; cannot pair the tab")
            return nil
        }
        var request = URLRequest(url: home.appendingPathComponent("v1/cloud/pairing"))
        request.httpMethod = method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if method == "POST" {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = Data("{}".utf8)
        }
        request.timeoutInterval = 30
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            guard status == 200 else {
                // The daemon answers a refusal in words; Cloud being off is the
                // usual one. Never the body of a success, which has the link.
                let said = String(data: data.prefix(300), encoding: .utf8) ?? ""
                shellLog("cloud: this Mac answered \(method) pairing with HTTP \(status): \(said)")
                return nil
            }
            return try JSONDecoder().decode(DaemonPairing.self, from: data)
        } catch {
            shellLog("cloud: could not ask this Mac about pairing — \(error.localizedDescription)")
            return nil
        }
    }

    // MARK: The page

    /// Run a script that always answers something: WebKit reports `undefined`
    /// as an error.
    private func ask(_ script: String) async -> Any? {
        await withCheckedContinuation { done in
            view.evaluateJavaScript(script) { value, _ in done.resume(returning: value) }
        }
    }

    private func waitFor(_ script: String, seconds: Double) async -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if await ask(script) as? Bool == true { return true }
            try? await Task.sleep(nanoseconds: 250_000_000)
        }
        return false
    }

    /// A string as a JavaScript literal.
    private func jsString(_ s: String) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: [s]),
              let array = String(data: data, encoding: .utf8) else { return nil }
        return String(array.dropFirst().dropLast())
    }
}

// MARK: - Pairing another machine through this Mac's assistant

/// What the daemon answers `POST /v1/cloud/pairing/agent` with, or refuses it
/// with.
private struct DaemonPairAgent: Decodable {
    let mode: String?
    let taskID: String?
    let error: Refusal?

    struct Refusal: Decodable {
        let code: String?
        let message: String?
    }

    enum CodingKeys: String, CodingKey {
        case mode, error
        case taskID = "task_id"
    }
}

/// The Cloud tab's way of asking this Mac to carry a pairing offer to another
/// machine.
///
/// The Cloud console, shown a machine this browser is not paired with, offers a
/// command to paste on that machine. This Mac often reaches that machine
/// already — an ssh alias, a cloud provider's session manager — so the page
/// can post the offer here instead (`clawdlinePairAgent`), and an assistant on
/// this Mac runs the same command over that access. The offer travels over the
/// person's own channel either way, never through Cloud.
///
/// Three things keep this from being a way for a page to start work on this
/// Mac:
///
/// - It is registered on the Cloud view's content controller only, and it
///   answers only the main frame of Cloud's own origin.
/// - The person is asked, in a native sheet naming the machine and showing the
///   command, before anything runs.
/// - The page supplies an offer, an id and a name, and no text: the daemon
///   checks all three and writes the instructions from its own template.
///
/// The offer is never logged, and the sheet shows only its first characters.
@MainActor
final class CloudPairAgent: NSObject, WKScriptMessageHandlerWithReply {
    static let name = "clawdlinePairAgent"

    /// One question at a time: a second press while the sheet is up is not a
    /// second request.
    private var asking = false

    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        let refuse = { (why: String) in replyHandler(["ok": false, "error": why], nil) }
        guard message.frameInfo.isMainFrame, isCloudOrigin(message.frameInfo.securityOrigin),
              let page = message.webView?.url, isCloud(page) else {
            shellLog("cloud: refused a pairing hand-off from outside Cloud's main frame")
            refuse("not_cloud")
            return
        }
        guard let body = message.body as? [String: Any],
              let offer = body["offer"] as? String, !offer.isEmpty, offer.utf8.count <= 4096,
              let machineID = body["machine_id"] as? String, !machineID.isEmpty, machineID.utf8.count <= 256 else {
            refuse("bad_request")
            return
        }
        let shownName = (body["machine_name"] as? String).map(Self.oneLine) ?? ""
        let name = shownName.isEmpty ? machineID : shownName
        guard let window = message.webView?.window, !asking else {
            refuse("busy")
            return
        }
        asking = true
        let alert = NSAlert()
        alert.messageText = L.t.pairAgentAsks(name)
        alert.informativeText = L.t.pairAgentCommand(String(offer.prefix(12)) + "…")
        alert.addButton(withTitle: L.t.pairAgentStart)
        alert.addButton(withTitle: L.t.dialogCancel)
        alert.beginSheetModal(for: window) { [weak self] response in
            guard response == .alertFirstButtonReturn else {
                self?.asking = false
                shellLog("cloud: the person declined handing the pairing with \(machineID) to this Mac")
                refuse("cancelled")
                return
            }
            Task { @MainActor in
                defer { self?.asking = false }
                let reply = await Self.handOff(offer: offer, machineID: machineID, machineName: shownName)
                replyHandler(reply, nil)
            }
        }
    }

    /// The daemon's route, with this Mac's own token.
    private static func handOff(offer: String, machineID: String, machineName: String) async -> [String: Any] {
        guard let token = LocalToken.read() else {
            shellLog("cloud: no local token yet; cannot hand the pairing to this Mac")
            return ["ok": false, "error": "no_token"]
        }
        var request = URLRequest(url: home.appendingPathComponent("v1/cloud/pairing/agent"))
        request.httpMethod = "POST"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.timeoutInterval = 200
        guard let payload = try? JSONSerialization.data(withJSONObject: [
            "offer": offer, "machine_id": machineID, "machine_name": machineName,
        ]) else {
            return ["ok": false, "error": "bad_request"]
        }
        request.httpBody = payload
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            let said = try? JSONDecoder().decode(DaemonPairAgent.self, from: data)
            guard status == 200, let mode = said?.mode else {
                // A refusal is words about the request, never the offer.
                let code = said?.error?.code ?? "http_\(status)"
                shellLog("cloud: this Mac refused the pairing hand-off with \(machineID): HTTP \(status) \(code)")
                return ["ok": false, "error": said?.error?.message ?? code]
            }
            shellLog("cloud: handed the pairing with \(machineID) to this Mac (\(mode) \(said?.taskID ?? ""))")
            return ["ok": true, "mode": mode, "task_id": said?.taskID ?? ""]
        } catch {
            shellLog("cloud: could not reach this Mac to hand the pairing over — \(error.localizedDescription)")
            return ["ok": false, "error": "unreachable"]
        }
    }

    /// Cloud's own origin, as WebKit reports the frame's: the URL's pieces,
    /// with WebKit's 0 for a default port.
    private func isCloudOrigin(_ origin: WKSecurityOrigin) -> Bool {
        guard origin.`protocol`.lowercased() == cloudHome.scheme?.lowercased(),
              origin.host.lowercased() == cloudHome.host?.lowercased() else { return false }
        let asked: Int? = origin.port == 0 ? nil : origin.port
        return asked == cloudHome.port
    }

    /// A name for the sheet: one line, at most 64 characters.
    private static func oneLine(_ s: String) -> String {
        let flat = s.unicodeScalars
            .filter { !CharacterSet.controlCharacters.contains($0) || $0 == "\n" || $0 == "\t" }
            .map { CharacterSet.whitespacesAndNewlines.contains($0) ? " " : String($0) }
            .joined()
            .split(separator: " ", omittingEmptySubsequences: true)
            .joined(separator: " ")
        return String(flat.prefix(64))
    }
}
