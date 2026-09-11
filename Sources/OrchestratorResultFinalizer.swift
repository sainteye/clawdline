import CryptoKit
import Foundation

/// Recovers a task result whose child completed the authenticated preflight but whose shell
/// stopped before the final rename. This is deliberately separate from `Orchestrator`: file
/// recovery is one closed ownership family and does not grow the coordinator's lock regions.
enum OrchestratorResultFinalizer {
    private struct Observation {
        let fingerprint: String
        let firstSeen: Date
    }

    private static let stableInterval: TimeInterval = 30
    private static let maximumResultBytes = 2 * 1024 * 1024
    private static let observationLock = NSLock()
    private static var observations: [String: Observation] = [:]
    private static var auditedFailures: Set<String> = []

    static func regularFileData(_ url: URL, maximumBytes: Int,
                                privatePermissions: Bool = false) -> Data? {
        var info = stat()
        guard lstat(url.path, &info) == 0,
              (info.st_mode & S_IFMT) == S_IFREG,
              info.st_size >= 0, info.st_size <= off_t(maximumBytes) else { return nil }
        if privatePermissions && (info.st_mode & 0o077) != 0 { return nil }
        return try? Data(contentsOf: url)
    }

    static func authenticatedObject(_ data: Data, taskID: String, secretHash: String,
                                    auditFailure: Bool) -> [String: Any]? {
        guard let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            if auditFailure { audit(taskID, why: "invalid_json") }
            return nil
        }
        guard object["clawdline_protocol"] as? Int == 1,
              object["task_id"] as? String == taskID,
              let status = object["status"] as? String,
              status == "success" || status == "failure" else {
            if auditFailure { audit(taskID, why: "bad_identity") }
            return nil
        }
        guard let secret = object["task_secret"] as? String,
              RemoteAuth.constantTimeEquals(secretHash, Orchestrator.hash(ofSecret: secret)) else {
            if auditFailure { audit(taskID, why: "bad_secret") }
            return nil
        }
        return object
    }

    private static func audit(_ taskID: String, why: String) {
        observationLock.lock()
        let first = auditedFailures.insert(taskID + ":" + why).inserted
        observationLock.unlock()
        if first {
            RemoteAuth.audit("orchestrator.result", ["task": taskID, "ok": "0", "why": why])
        }
    }

    @discardableResult
    static func recover(taskID: String, secretHash: String, directory: URL, now: Date) -> Bool {
        let temporary = directory.appendingPathComponent("result.json.tmp")
        let ready = directory.appendingPathComponent("result.json.ready")
        let final = directory.appendingPathComponent("result.json")
        guard !FileManager.default.fileExists(atPath: final.path),
              let markerData = regularFileData(ready, maximumBytes: 4096,
                                               privatePermissions: true),
              let marker = (try? JSONSerialization.jsonObject(with: markerData)) as? [String: Any],
              Set(marker.keys) == Set(["clawdline_protocol", "task_id", "finalization_ready",
                                      "result_sha256"]),
              marker["clawdline_protocol"] as? Int == 1,
              marker["task_id"] as? String == taskID,
              marker["finalization_ready"] as? Bool == true,
              let claimedDigest = marker["result_sha256"] as? String,
              claimedDigest.range(of: "^[a-f0-9]{64}$", options: .regularExpression) != nil,
              let resultData = regularFileData(temporary, maximumBytes: maximumResultBytes),
              RemoteAuth.constantTimeEquals(claimedDigest,
                  RemoteAuth.hex(SHA256.hash(data: resultData))),
              authenticatedObject(resultData, taskID: taskID, secretHash: secretHash,
                                  auditFailure: true) != nil else {
            forgetObservation(taskID)
            return false
        }

        let fingerprint = RemoteAuth.hex(SHA256.hash(data: markerData + resultData))
        observationLock.lock()
        let observed = observations[taskID]
        if observed?.fingerprint != fingerprint {
            observations[taskID] = Observation(fingerprint: fingerprint, firstSeen: now)
            observationLock.unlock()
            return false
        }
        let stable = now.timeIntervalSince(observed!.firstSeen) >= stableInterval
        observationLock.unlock()
        guard stable,
              let finalMarkerData = regularFileData(ready, maximumBytes: 4096,
                                                    privatePermissions: true),
              let finalResultData = regularFileData(temporary, maximumBytes: maximumResultBytes),
              RemoteAuth.hex(SHA256.hash(data: finalMarkerData + finalResultData)) == fingerprint,
              authenticatedObject(finalResultData, taskID: taskID, secretHash: secretHash,
                                  auditFailure: true) != nil else { return false }

        let recovery = directory.appendingPathComponent(".result-recovery-" + UUID().uuidString)
        do {
            // Copy the twice-observed bytes to an unpredictable private inode first. Linking the
            // child-writable tmp itself would let a still-open child descriptor mutate the final
            // path after publication.
            try finalResultData.write(to: recovery, options: .withoutOverwriting)
            try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                  ofItemAtPath: recovery.path)
            // Hard-link publication is same-directory and create-if-absent: it cannot replace a
            // result which won the race through the child's normal rename.
            try FileManager.default.linkItem(at: recovery, to: final)
            guard let published = regularFileData(final, maximumBytes: maximumResultBytes),
                  RemoteAuth.hex(SHA256.hash(data: published)) == claimedDigest,
                  authenticatedObject(published, taskID: taskID, secretHash: secretHash,
                                      auditFailure: true) != nil else {
                try? FileManager.default.removeItem(at: final)
                return false
            }
            try? FileManager.default.removeItem(at: recovery)
            try? FileManager.default.removeItem(at: temporary)
            try? FileManager.default.removeItem(at: ready)
            forgetObservation(taskID)
            RemoteAuth.audit("orchestrator.result.recovered", [
                "task": taskID, "stable_seconds": String(Int(stableInterval)),
            ])
            return true
        } catch {
            try? FileManager.default.removeItem(at: recovery)
            return false
        }
    }

    private static func forgetObservation(_ taskID: String) {
        observationLock.lock(); observations.removeValue(forKey: taskID); observationLock.unlock()
    }

    static func reset() {
        observationLock.lock()
        observations = [:]
        auditedFailures = []
        observationLock.unlock()
    }
}
