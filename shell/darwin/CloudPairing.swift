// Explicit Cloud pairing handovers through the authenticated local daemon.
import AppKit
import Foundation
import WebKit

// MARK: - Pairing another machine through this Mac's assistant

/// What the daemon answers `POST /v1/cloud/pairing/agent` with, or refuses it
/// with.
private struct DaemonPairAgent: Decodable {
    let mode: String?
    let taskID: String?
    let trust: String?
    let expiresAt: Double?
    let error: Refusal?

    struct Refusal: Decodable {
        let code: String?
        let message: String?
    }

    enum CodingKeys: String, CodingKey {
        case mode, error, trust
        case taskID = "task_id"
        case expiresAt = "expires_at"
    }
}

/// What `GET /v1/cloud/pairing/agent/<task_id>` answers: a word for where the
/// hand-off is and, once it ended, the task's own sentence. Never the offer.
private struct DaemonPairAgentProgress: Decodable, Equatable {
    let state: String
    let summary: String?
    let failedReason: String?

    enum CodingKeys: String, CodingKey {
        case state, summary
        case failedReason = "failed_reason"
    }

    var ended: Bool { state == "done" || state == "failed" }
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
            let view = message.webView
            Task { @MainActor in
                defer { self?.asking = false }
                let (reply, expiresAt) = await Self.handOff(offer: offer, machineID: machineID, machineName: shownName)
                replyHandler(reply, nil)
                if let taskID = reply["task_id"] as? String, !taskID.isEmpty, let view {
                    self?.follow(taskID: taskID, until: expiresAt, in: view)
                }
            }
        }
    }

    /// The one hand-off being followed; a new one replaces it.
    private var following: Task<Void, Never>?

    /// Follows a hand-off's task and tells the Cloud page each time it moves,
    /// so the card can show the AI starting, working, done, or why it
    /// stopped, instead of one sentence for the minute or two it takes.
    ///
    /// Every ~3 s until the task ends or the offer runs out. Each change is
    /// dispatched on the page's `window` as a `clawdline-pair-agent` event —
    /// only while the view is still on Cloud's origin; a view that went
    /// elsewhere is not told, and the following stops.
    private func follow(taskID: String, until expiresAt: Date?, in view: WKWebView) {
        following?.cancel()
        // Past the offer, the card has already said it ran out; a task with
        // no expiry is followed for its own timeout and a little more.
        let deadline = (expiresAt ?? Date().addingTimeInterval(16 * 60)).addingTimeInterval(5)
        following = Task { @MainActor [weak view] in
            var last: DaemonPairAgentProgress?
            while !Task.isCancelled, Date() < deadline {
                if let now = await Self.progress(taskID: taskID), now != last {
                    last = now
                    guard let view, let here = view.url, isCloud(here) else { return }
                    Self.tell(view, taskID: taskID, now)
                    if now.ended { return }
                }
                try? await Task.sleep(nanoseconds: 3_000_000_000)
            }
        }
    }

    private static func progress(taskID: String) async -> DaemonPairAgentProgress? {
        guard let token = LocalToken.read() else { return nil }
        var request = URLRequest(url: home.appendingPathComponent("v1/cloud/pairing/agent/\(taskID)"))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = 10
        guard let (data, response) = try? await URLSession.shared.data(for: request),
              (response as? HTTPURLResponse)?.statusCode == 200 else { return nil }
        return try? JSONDecoder().decode(DaemonPairAgentProgress.self, from: data)
    }

    private static func tell(_ view: WKWebView, taskID: String, _ now: DaemonPairAgentProgress) {
        var detail: [String: Any] = ["task_id": taskID, "state": now.state]
        if let summary = now.summary { detail["summary"] = summary }
        if let reason = now.failedReason { detail["failed_reason"] = reason }
        guard let data = try? JSONSerialization.data(withJSONObject: detail),
              let json = String(data: data, encoding: .utf8) else { return }
        view.evaluateJavaScript(
            "window.dispatchEvent(new CustomEvent('clawdline-pair-agent', {detail: \(json)})); true",
            completionHandler: nil)
    }

    /// The daemon's route, with this Mac's own token, and when the offer runs
    /// out, for following the task it started.
    private static func handOff(offer: String, machineID: String, machineName: String) async -> ([String: Any], Date?) {
        guard let token = LocalToken.read() else {
            shellLog("cloud: no local token yet; cannot hand the pairing to this Mac")
            return (["ok": false, "error": "no_token"], nil)
        }
        var request = URLRequest(url: home.appendingPathComponent("v1/cloud/pairing/agent"))
        request.httpMethod = "POST"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.timeoutInterval = 200
        guard let payload = try? JSONSerialization.data(withJSONObject: [
            "offer": offer, "machine_id": machineID, "machine_name": machineName,
        ]) else {
            return (["ok": false, "error": "bad_request"], nil)
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
                return (["ok": false, "error": said?.error?.message ?? code], nil)
            }
            shellLog("cloud: handed the pairing with \(machineID) to this Mac (\(mode) \(said?.taskID ?? "") trust \(said?.trust ?? "-"))")
            let expires = said?.expiresAt.flatMap { $0 > 0 ? Date(timeIntervalSince1970: $0 / 1000) : nil }
            return (["ok": true, "mode": mode, "task_id": said?.taskID ?? "", "trust": said?.trust ?? ""], expires)
        } catch {
            shellLog("cloud: could not reach this Mac to hand the pairing over — \(error.localizedDescription)")
            return (["ok": false, "error": "unreachable"], nil)
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


/// An explicit local handover. Unlike the assistant bridge, this handler has
/// no dispatch path: the authenticated daemon must name the requested machine.
@MainActor
final class CloudPairLocal: NSObject, WKScriptMessageHandlerWithReply {
    static let name = "clawdlinePairLocal"
    private var busy = false
    private var invitationAllowed = false
    private var connectionAllowed = false
    private var connectionAttempt: UUID?
    private var connection: [String: Any] = ["phase": "idle"]
    private static var linkBusy = false

    private struct LocalFailure: Error {
        let code: String
    }

    private func isAllowed(_ message: WKScriptMessage) -> Bool {
        let origin = message.frameInfo.securityOrigin
        return CloudPairingAccess.allows(mainFrame: message.frameInfo.isMainFrame,
            scheme: origin.`protocol`, host: origin.host, port: origin.port,
            page: message.webView?.url, trusted: cloudHome)
    }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        guard isAllowed(message), let body = message.body as? [String: Any] else {
            replyHandler(["ok": false, "error": "not_cloud"], nil)
            return
        }
        // Cancel the local intent even while a login poll is awaiting its reply.
        // This does not claim to withdraw an approval already sent to Cloud.
        if body["kind"] as? String == "connection-cancel" {
            connectionAllowed = false
            connectionAttempt = nil
            connection = ["phase": "idle"]
            replyHandler(["ok": true, "native_setup": true, "connection": connection], nil)
            return
        }
        guard !busy else { replyHandler(["ok": false, "error": "busy"], nil); return }
        busy = true
        Task { @MainActor in
            defer { busy = false }
            do {
                var status = try await request("GET", path: "v1/cloud/status")
                if body["kind"] as? String == "status" {
                    replyHandler(statusReply(status), nil); return
                }
                if body["kind"] as? String == "connect" {
                    guard let window = message.webView?.window else { throw LocalFailure(code: "unavailable") }
                    let alert = NSAlert()
                    alert.messageText = L.t.cloudConnectAsks(machineName(status))
                    alert.informativeText = L.t.cloudConnectBody
                    alert.addButton(withTitle: L.t.cloudConnectStart)
                    alert.addButton(withTitle: L.t.dialogCancel)
                    let accepted: Bool = await withCheckedContinuation { done in
                        alert.beginSheetModal(for: window) { done.resume(returning: $0 == .alertFirstButtonReturn) }
                    }
                    guard accepted, isAllowed(message) else { throw LocalFailure(code: "cancelled") }
                    let attempt = UUID()
                    connectionAttempt = attempt
                    connectionAllowed = true
                    let started = try await request("POST", path: "v1/cloud/login")
                    guard connectionAttempt == attempt else { throw LocalFailure(code: "cancelled") }
                    connection = started
                    guard isAllowed(message) else { throw LocalFailure(code: "not_cloud") }
                    if connection["phase"] as? String == "waiting",
                       let raw = connection["verification_url"] as? String,
                       let url = URL(string: raw), ["http", "https"].contains(url.scheme?.lowercased() ?? "") {
                        // This URL comes from the authenticated daemon, never from the page.
                        // If Launch Services cannot open it, the reply still offers the URL.
                        _ = NSWorkspace.shared.open(url)
                    }
                    status = try await finishConnection(status, message: message)
                    replyHandler(statusReply(status), nil); return
                }
                if body["kind"] as? String == "connection-status" {
                    if connectionAllowed, connection["phase"] as? String == "waiting" {
                        let next = connection["next_poll_at"] as? Double ?? 0
                        if Date().timeIntervalSince1970 >= next {
                            let attempt = connectionAttempt
                            let polled = try await request("GET", path: "v1/cloud/login")
                            guard connectionAttempt == attempt, connectionAllowed else { throw LocalFailure(code: "cancelled") }
                            connection = polled
                        }
                    }
                    status = try await finishConnection(status, message: message)
                    replyHandler(statusReply(status), nil); return
                }
                guard let machine = status["machine_id"] as? String, !machine.isEmpty,
                      status["configured"] as? Bool == true else {
                    throw LocalFailure(code: "cloud_not_signed_in")
                }
                if body["kind"] as? String == "invitation-status" {
                    guard invitationAllowed, isAllowed(message) else {
                        replyHandler(["ok": false, "error": "confirmation_required"], nil); return
                    }
                    replyHandler(["ok": true, "pairing": status["pairing"] ?? ["phase": "idle"]], nil); return
                }
                if body["kind"] as? String == "invitation" {
                    guard body["machine_id"] as? String == machine, isAllowed(message),
                          let window = message.webView?.window else {
                        replyHandler(["ok": false, "error": "not_this_machine"], nil); return
                    }
                    let alert = NSAlert()
                    alert.messageText = L.t.pairInvitationAsks
                    alert.informativeText = L.t.pairInvitationBody
                    alert.addButton(withTitle: L.t.pairLocalStart)
                    alert.addButton(withTitle: L.t.dialogCancel)
                    let accepted: Bool = await withCheckedContinuation { done in
                        alert.beginSheetModal(for: window) { done.resume(returning: $0 == .alertFirstButtonReturn) }
                    }
                    guard accepted, isAllowed(message) else {
                        replyHandler(["ok": false, "error": "cancelled"], nil); return
                    }
                    let result = try await request("POST", path: "v1/cloud/pairing")
                    invitationAllowed = true
                    replyHandler(["ok": true, "pairing": result], nil); return
                }
                guard body["machine_id"] as? String == machine,
                      let offer = body["offer"] as? String, !offer.isEmpty, offer.utf8.count <= 4096,
                      isAllowed(message), let window = message.webView?.window else {
                    replyHandler(["ok": false, "error": "not_this_machine"], nil); return
                }
                let result = try await finishOffer(offer, status: status, in: window, stillAllowed: { self.isAllowed(message) })
                replyHandler(["ok": true, "machine_id": machine, "pairing": result], nil)
            } catch {
                let code = (error as? LocalFailure)?.code ?? "unavailable"
                if body["kind"] as? String == "connect" {
                    connectionAllowed = false
                    connectionAttempt = nil
                    connection = ["phase": "failed", "code": code]
                }
                replyHandler(["ok": false, "error": code], nil)
            }
        }
    }

    private func machineName(_ status: [String: Any]) -> String {
        status["machine_name"] as? String ?? Host.current().localizedName ?? ProcessInfo.processInfo.hostName
    }

    private func statusReply(_ status: [String: Any]) -> [String: Any] {
        ["ok": true, "native_setup": true,
         "configured": status["configured"] as? Bool ?? false,
         "enabled": status["enabled"] as? Bool ?? false,
         "state": status["state"] as? String ?? "off",
         "machine_id": status["machine_id"] as? String ?? "",
         "machine_name": machineName(status),
         "fingerprint": status["fingerprint"] as? String ?? "",
         "pairing_invitation": true, "connection": connection]
    }

    /// Reading someone else's completed login never authorizes turning Cloud on.
    /// Only this handler's current, explicit native consent may finish the setup.
    private func finishConnection(_ status: [String: Any], message: WKScriptMessage) async throws -> [String: Any] {
        guard connectionAllowed else { return status }
        guard isAllowed(message) else {
            connectionAllowed = false
            throw LocalFailure(code: "not_cloud")
        }
        if connection["phase"] as? String == "failed" {
            connectionAllowed = false
            return status
        }
        guard connection["phase"] as? String == "complete" else { return status }
        // Consume consent before the write: a failed reply cannot repeat it silently.
        connectionAllowed = false
        _ = try await request("POST", path: "v1/cloud/enabled", body: ["enabled": true])
        return try await request("GET", path: "v1/cloud/status")
    }

    /// A browser on this Mac can ask the installed app directly. The URL is
    /// untrusted input: the daemon names the machine and verifies the offer,
    /// and a native confirmation still precedes every handover.
    static func pairFromLink(_ url: URL, in window: NSWindow) async -> String {
        guard !linkBusy else { return "busy" }
        linkBusy = true
        defer { linkBusy = false }
        guard url.scheme == "clawdline-next", url.host == "pair-browser",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
              items.filter({ $0.name == "offer" }).count == 1,
              items.filter({ $0.name == "machine_id" }).count <= 1,
              let offer = items.first(where: { $0.name == "offer" })?.value,
              !offer.isEmpty, offer.utf8.count <= 4096 else { return "bad_request" }
        let requestedMachine = items.first(where: { $0.name == "machine_id" })
        if let requestedMachine, requestedMachine.value?.isEmpty != false { return "bad_request" }
        let handler = CloudPairLocal()
        do {
            let status = try await handler.request("GET", path: "v1/cloud/status")
            guard let machine = status["machine_id"] as? String, !machine.isEmpty,
                  status["configured"] as? Bool == true else { return "cloud_not_signed_in" }
            if let requestedMachine, requestedMachine.value != machine { return "not_this_machine" }
            _ = try await handler.finishOffer(offer, status: status, in: window, stillAllowed: { true })
            return ""
        } catch { return (error as? LocalFailure)?.code ?? "unavailable" }
    }

    private func finishOffer(_ offer: String, status: [String: Any], in window: NSWindow,
                             stillAllowed: () -> Bool) async throws -> [String: Any] {
        let alert = NSAlert()
        // The machine name comes from the authenticated local daemon, not
        // from hosted JavaScript. No code or credential appears in the sheet.
        alert.messageText = L.t.pairLocalAsks(status["machine_name"] as? String ?? status["machine_id"] as? String ?? "")
        alert.informativeText = L.t.pairLocalBody + "\n\n" + L.t.pairMachineFingerprint(status["fingerprint"] as? String ?? "")
        alert.addButton(withTitle: L.t.pairLocalStart)
        alert.addButton(withTitle: L.t.dialogCancel)
        let accepted: Bool = await withCheckedContinuation { done in
            alert.beginSheetModal(for: window) { done.resume(returning: $0 == .alertFirstButtonReturn) }
        }
        guard accepted, stillAllowed() else { throw LocalFailure(code: "cancelled") }
        // Omitted scope preserves an existing pin. The normal offer route
        // verifies the account, expiry and cryptographic proof before saving.
        let result = try await request("POST", path: "v1/cloud/pairing/offer", body: ["offer": offer])
        guard result["phase"] as? String == "paired" else { throw LocalFailure(code: "pairing_failed") }
        let done = NSAlert()
        done.messageText = L.t.pairLinkDone
        done.informativeText = L.t.pairMachineFingerprint(result["machine_fingerprint"] as? String ?? status["fingerprint"] as? String ?? "")
            + "\n\n" + L.t.pairBrowserFingerprint(result["viewer_fingerprint"] as? String ?? "")
            + "\n\n" + L.t.pairLinkReturn
        done.addButton(withTitle: L.t.dialogOK)
        _ = await done.beginSheetModal(for: window)
        return result
    }

    static func pairingFailureText(_ code: String) -> String {
        switch code {
        case "not_this_machine": return L.t.pairWrongMachine
        case "cloud_not_signed_in", "cloud_not_configured", "cloud_off": return L.t.pairNeedsCloud
        case "offer_expired", "pairing_expired", "invitation_expired", "pairing_gone", "unknown_pairing": return L.t.pairCodeExpired
        case "wrong_account", "wrong_invitation": return L.t.pairWrongAccount
        case "unavailable", "no_token": return L.t.pairUnavailable
        default: return L.t.pairLinkRetry
        }
    }

    private func request(_ method: String, path: String, body: [String: Any]? = nil) async throws -> [String: Any] {
        guard let token = LocalToken.read() else { throw LocalFailure(code: "no_token") }
        var req = URLRequest(url: home.appendingPathComponent(path))
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.timeoutInterval = 30
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let data: Data
        let response: URLResponse
        do { (data, response) = try await URLSession.shared.data(for: req) }
        catch { throw LocalFailure(code: "unavailable") }
        let reply = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            let code = (reply?["error"] as? [String: Any])?["code"] as? String
                ?? reply?["error"] as? String ?? "pairing_failed"
            throw LocalFailure(code: code)
        }
        guard let reply else { throw LocalFailure(code: "bad_response") }
        return reply
    }
}
