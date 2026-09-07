import Foundation

extension RemoteServer {
    /// Replaces the coherent SessionWatch read only in route tests. The production route reads one
    /// immutable publication before Git and another afterwards; tests move this value at the same
    /// seam without opening a terminal or racing the background watcher.
    static var sessionLandingObservationForTesting:
        ((String) -> Orchestrator.SessionLandingObservation?)?

    private func sessionLandingObservation(_ id: String)
        -> Orchestrator.SessionLandingObservation? {
        if let supplied = Self.sessionLandingObservationForTesting { return supplied(id) }
        if let supplied = Self.sessionPayloadForTesting {
            guard let target = Self.session(withID: id, among: supplied.0), target.isAssistant else {
                return nil
            }
            return Orchestrator.SessionLandingObservation(
                identity: Self.sessionWorkIdentity(target),
                terminalState: supplied.1[id] ?? .unknown,
                repositoryDirectory: target.cwd,
                publicationGeneration: 0, publicationEpoch: "remote-server-test-publication")
        }
        let publication = SessionWatch.shared.publishedInventory()
        guard let target = Self.session(withID: id, among: publication.targets),
              target.isAssistant else { return nil }
        let published = publication.identities[target.id]
        return Orchestrator.SessionLandingObservation(
            identity: Self.sessionWorkIdentity(target, publishedIdentity: published),
            terminalState: publication.states[id] ?? .unknown,
            repositoryDirectory: published?.workingDirectory ?? target.cwd,
            publicationGeneration: publication.generation,
            publicationEpoch: publication.epoch)
    }

    /// The router keeps only the path match. This narrow handler owns the closed schema and both
    /// coherent observations around the potentially slow Git proof.
    func routeSessionLanding(_ request: Request, path: String,
                             orchestratorAuthed: Bool) -> Response {
        guard orchestratorAuthed else {
            return .error(403, "forbidden",
                          "Reporting root landing needs the orchestrator token.")
        }
        let encoded = String(path.dropFirst("/v1/orchestrator/sessions/".count)
            .dropLast("/landing".count))
        let id = encoded.removingPercentEncoding ?? encoded
        guard !id.isEmpty, !id.contains("/") else {
            return .error(400, "bad_request", "The route must name one session id.")
        }
        guard let observation = sessionLandingObservation(id) else {
            return .error(404, "session_not_found", "No current assistant session named \(id).")
        }
        let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any] ?? [:]
        guard Set(body.keys) == Set(["summary", "target", "commit"]),
              body["summary"] is String, body["target"] is String,
              body["commit"] is String else {
            return .error(400, "bad_request",
                          "The body must contain only string summary, target and commit.")
        }
        let reply = Orchestrator.reportSessionLanding(
            observation: observation,
            summary: body["summary"] as? String ?? "",
            target: body["target"] as? String ?? "",
            commit: body["commit"] as? String ?? "",
            reobserve: { [weak self] in self?.sessionLandingObservation(id) })
        DispatchQueue.main.async { SessionWatch.shared.nudge() }
        return answer(reply)
    }
}
