import CryptoKit
import Foundation

/// Filesystem policy for the broker's durable registry and installation secrets.
///
/// This owner deliberately knows nothing about task rows. It establishes the byte-level facts
/// that must be true before `Orchestrator` is allowed to decode or replace them: absent is a valid
/// first-run state; unreadable, malformed and future-version stores are not empty registries; and
/// readable rejected bytes are copied to immutable evidence without touching the canonical file.
enum OrchestratorPersistence {
    /// Test-only protection failure injection. Production leaves this nil. The corrected writer
    /// invokes it against an unpublished temporary file, never the canonical identity.
    static var secretProtectionInterceptorForTesting: ((URL) -> Bool)?

    enum StoreHealth: Equatable {
        case unknown
        case absent
        case ready
        case corrupt(reason: String, digest: String, quarantine: String?)
        case unsupported(version: Int, digest: String, quarantine: String?)
        case unreadable(reason: String)

        var authoritative: Bool {
            switch self {
            case .absent, .ready: return true
            case .unknown, .corrupt, .unsupported, .unreadable: return false
            }
        }

        var status: String {
            switch self {
            case .unknown: return "unknown"
            case .absent: return "absent"
            case .ready: return "ready"
            case .corrupt: return "corrupt"
            case .unsupported: return "unsupported_version"
            case .unreadable: return "unreadable"
            }
        }

        var record: [String: Any] {
            var out: [String: Any] = ["status": status, "authoritative": authoritative]
            switch self {
            case .corrupt(let reason, let digest, let quarantine):
                out["reason"] = reason; out["sha256"] = digest
                out["quarantined"] = quarantine != nil
            case .unsupported(let version, let digest, let quarantine):
                out["version"] = version; out["sha256"] = digest
                out["quarantined"] = quarantine != nil
            case .unreadable(let reason): out["reason"] = reason
            default: break
            }
            return out
        }
    }

    struct StoreRead {
        let health: StoreHealth
        let data: Data?
        let object: [String: Any]?
    }

    enum SecretRead: Equatable {
        case absent
        case value(Data)
        case invalid
        case unreadable
    }

    static func readStore(at url: URL) -> StoreRead {
        let manager = FileManager.default
        guard manager.fileExists(atPath: url.path) else {
            return StoreRead(health: .absent, data: nil, object: nil)
        }
        guard regularFile(at: url), let data = try? Data(contentsOf: url) else {
            return StoreRead(health: .unreadable(reason: "not_readable_regular_file"),
                             data: nil, object: nil)
        }
        let digest = hex(SHA256.hash(data: data))
        guard let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else {
            let evidence = quarantine(data, digest: digest, source: url)
            return StoreRead(health: .corrupt(reason: "json_object_required", digest: digest,
                                              quarantine: evidence?.path),
                             data: data, object: nil)
        }
        guard let version = object["version"] as? Int else {
            let evidence = quarantine(data, digest: digest, source: url)
            return StoreRead(health: .corrupt(reason: "integer_version_required", digest: digest,
                                              quarantine: evidence?.path),
                             data: data, object: nil)
        }
        guard version == 1 else {
            let evidence = quarantine(data, digest: digest, source: url)
            return StoreRead(health: .unsupported(version: version, digest: digest,
                                                  quarantine: evidence?.path),
                             data: data, object: nil)
        }
        return StoreRead(health: .ready, data: data, object: object)
    }

    static func rejectedStore(_ read: StoreRead, reason: String, source: URL) -> StoreHealth {
        guard let data = read.data else { return .unreadable(reason: reason) }
        let digest = hex(SHA256.hash(data: data))
        let evidence = quarantine(data, digest: digest, source: source)
        return .corrupt(reason: reason, digest: digest, quarantine: evidence?.path)
    }

    static func readSecret(at url: URL, expectedBytes: Int? = nil) -> SecretRead {
        let manager = FileManager.default
        guard manager.fileExists(atPath: url.path) else { return .absent }
        guard protectedRegularFile(at: url), let data = try? Data(contentsOf: url) else {
            return .unreadable
        }
        if let expectedBytes, data.count != expectedBytes { return .invalid }
        return data.isEmpty ? .invalid : .value(data)
    }

    /// Create a first-run secret without ever replacing a path which appeared after the absence
    /// check. The caller receives the value only after the exact bytes and mode are durable enough
    /// for the next process to read the same identity.
    static func createSecret(_ data: Data, at url: URL) -> Bool {
        let manager = FileManager.default
        guard !manager.fileExists(atPath: url.path),
              let staging = prepareProtectedFile(
                data, for: url, protection: secretProtectionInterceptorForTesting) else {
            return false
        }
        defer { try? manager.removeItem(at: staging) }
        guard !manager.fileExists(atPath: url.path) else { return false }
        do { try manager.moveItem(at: staging, to: url); return true }
        catch { return false }
    }

    /// Replace a durable store only after the complete candidate exists as an exact, owner-bound
    /// `0600` regular file beside it. A failed protection/read-back check never reaches the
    /// canonical name, so `false` continues to mean the registry transition was not committed.
    static func replaceStore(_ data: Data, at url: URL,
                             protection: ((URL) -> Bool)? = nil) -> Bool {
        let manager = FileManager.default
        guard let staging = prepareProtectedFile(data, for: url, protection: protection)
        else { return false }
        defer { try? manager.removeItem(at: staging) }
        do {
            if manager.fileExists(atPath: url.path) {
                _ = try manager.replaceItemAt(url, withItemAt: staging)
            } else {
                try manager.moveItem(at: staging, to: url)
            }
            return true
        } catch {
            return false
        }
    }

    private static func regularFile(at url: URL) -> Bool {
        guard let values = try? url.resourceValues(forKeys: [.isRegularFileKey,
                                                              .isSymbolicLinkKey]) else {
            return false
        }
        return values.isRegularFile == true && values.isSymbolicLink != true
    }

    private static func protectedRegularFile(at url: URL, expected: Data? = nil) -> Bool {
        let manager = FileManager.default
        guard regularFile(at: url),
              let attributes = try? manager.attributesOfItem(atPath: url.path),
              let mode = attributes[.posixPermissions] as? NSNumber,
              mode.intValue & 0o777 == 0o600,
              let owner = attributes[.ownerAccountID] as? NSNumber,
              let home = try? manager.attributesOfItem(atPath: NSHomeDirectory()),
              let currentOwner = home[.ownerAccountID] as? NSNumber,
              owner == currentOwner else { return false }
        guard let expected else { return true }
        return (try? Data(contentsOf: url)) == expected
    }

    private static func prepareProtectedFile(_ data: Data, for destination: URL,
                                             protection: ((URL) -> Bool)?) -> URL? {
        let manager = FileManager.default
        let directory = destination.deletingLastPathComponent()
        let staging = directory.appendingPathComponent(
            ".\(destination.lastPathComponent).\(UUID().uuidString).new")
        var prepared = false
        defer { if !prepared { try? manager.removeItem(at: staging) } }
        do {
            if manager.fileExists(atPath: directory.path) {
                let values = try directory.resourceValues(forKeys: [.isDirectoryKey,
                                                                      .isSymbolicLinkKey])
                guard values.isDirectory == true, values.isSymbolicLink != true else { return nil }
            } else {
                try manager.createDirectory(at: directory, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
            }
            try manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
            guard manager.createFile(atPath: staging.path, contents: data,
                                     attributes: [.posixPermissions: 0o600]) else { return nil }
            guard protection?(staging) != false,
                  protectedRegularFile(at: staging, expected: data) else { return nil }
            prepared = true
            return staging
        } catch {
            try? manager.removeItem(at: staging)
            return nil
        }
    }

    private static func quarantine(_ data: Data, digest: String, source: URL) -> URL? {
        let manager = FileManager.default
        let directory = source.deletingLastPathComponent()
            .appendingPathComponent("orchestrator-quarantine", isDirectory: true)
        let destination = directory.appendingPathComponent("orchestrator-\(digest).json")
        do {
            if manager.fileExists(atPath: directory.path) {
                let values = try directory.resourceValues(forKeys: [.isDirectoryKey,
                                                                      .isSymbolicLinkKey])
                guard values.isDirectory == true, values.isSymbolicLink != true else { return nil }
            } else {
                try manager.createDirectory(at: directory, withIntermediateDirectories: true)
                let values = try directory.resourceValues(forKeys: [.isDirectoryKey,
                                                                      .isSymbolicLinkKey])
                guard values.isDirectory == true, values.isSymbolicLink != true else { return nil }
            }
            if manager.fileExists(atPath: destination.path) {
                guard regularFile(at: destination) else { return nil }
                guard try Data(contentsOf: destination) == data else { return nil }
            } else {
                guard manager.createFile(atPath: destination.path, contents: data,
                                         attributes: [.posixPermissions: 0o600]) else { return nil }
                guard regularFile(at: destination) else { return nil }
            }
            try manager.setAttributes([.posixPermissions: 0o600],
                                      ofItemAtPath: destination.path)
            return destination
        } catch {
            return nil
        }
    }

    private static func hex<D: Sequence>(_ digest: D) -> String where D.Element == UInt8 {
        digest.map { String(format: "%02x", $0) }.joined()
    }
}

extension Orchestrator {
    // MARK: - Durable coordination input policy

    static func boundedCoordinationText(_ value: Any?, limit: Int) -> String? {
        guard let raw = value as? String else { return nil }
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, text.count <= limit,
              !text.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
        return text
    }

    static func canonicalCoordinationRepository(_ raw: String) -> String? {
        guard raw.hasPrefix("/"), raw != "/" else { return nil }
        let path = URL(fileURLWithPath: raw).standardizedFileURL.path
        guard path.hasPrefix("/"), path != "/" else { return nil }
        return path
    }

    static func canonicalCoordinationPaths(_ raw: [String], repository: String)
        -> [String]? {
        guard !raw.isEmpty, raw.count <= 200 else { return nil }
        let root = URL(fileURLWithPath: repository, isDirectory: true)
        let prefix = repository + "/"
        var found = Set<String>()
        for path in raw {
            guard !path.isEmpty, path.count <= 1_024,
                  !path.unicodeScalars.contains(where: { $0.value == 0 }) else { return nil }
            let absolute = path.hasPrefix("/")
                ? URL(fileURLWithPath: path).standardizedFileURL.path
                : URL(fileURLWithPath: path, relativeTo: root).standardizedFileURL.path
            guard absolute.hasPrefix(prefix) else { return nil }
            let relative = String(absolute.dropFirst(prefix.count))
            guard !relative.isEmpty else { return nil }
            found.insert(relative)
        }
        return found.sorted()
    }

    /// Persist the fields owned by one Root Assignment step or roll back exactly those fields.
    /// These helpers keep terminal/audit side effects outside the persistence decision while
    /// giving the focused suite a production seam that needs no live terminal.
    static func persistRootAssignmentActivation(_ id: String, at: Date,
                                                previous: RootAssignment) -> Bool {
        guard save() else {
            OrchestratorRegistry.withCoordinationRecords {
                $0.withdrawRootAssignmentActivation(
                    id, at: at, restoring: previous.state, activeAt: previous.activeAt)
            }
            return false
        }
        return true
    }

    static func persistRootAssignmentBriefing(_ id: String, at: Date,
                                              previous: RootAssignment) -> Bool {
        guard save() else {
            OrchestratorRegistry.withCoordinationRecords {
                $0.withdrawRootAssignmentBriefed(
                    id, at: at, restoring: previous.state, briefedAt: previous.briefedAt)
            }
            return false
        }
        return true
    }

    static func persistRootAssignmentInjection(_ id: String, at: Date) -> Bool {
        guard save() else {
            OrchestratorRegistry.withCoordinationRecords {
                $0.withdrawRootAssignmentInjection(id, at: at)
            }
            return false
        }
        return true
    }

    // MARK: - Installation secrets

    /// Minted only when absent. An unreadable or malformed existing credential is preserved and
    /// authentication fails closed until an operator repairs that exact installation identity.
    static func dispatchToken() -> String {
        lock.lock(); defer { lock.unlock() }
        switch OrchestratorPersistence.readSecret(at: tokenURL) {
        case .value(let data):
            guard let onDisk = String(data: data, encoding: .utf8) else { return "" }
            let token = onDisk.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !token.isEmpty, token.count <= 512 else { return "" }
            return token
        case .invalid, .unreadable:
            Log.write("orchestrator: dispatch credential is unreadable; preserving it and denying machine authentication")
            return ""
        case .absent:
            let made = RemoteAuth.newToken()
            return OrchestratorPersistence.createSecret(Data(made.utf8), at: tokenURL) ? made : ""
        }
    }

    static func verifyDispatch(token: String?) -> Bool {
        guard let token, !token.isEmpty else { return false }
        let expected = RemoteAuth.hex(SHA256.hash(data: Data(dispatchToken().utf8)))
        let presented = RemoteAuth.hex(SHA256.hash(data: Data(token.utf8)))
        return RemoteAuth.constantTimeEquals(expected, presented)
    }

    static func hash(ofSecret secret: String) -> String {
        RemoteAuth.hex(SHA256.hash(data: Data(secret.utf8)))
    }

    /// The queued-secret key follows the same absence-only rule. Returning nil is deliberate:
    /// it makes both newly queued and restored work refuse instead of encrypting under an
    /// ephemeral replacement which the next process could never open.
    static func archiveKey() -> SymmetricKey? {
        lock.lock(); defer { lock.unlock() }
        switch OrchestratorPersistence.readSecret(at: archiveKeyURL) {
        case .value(let data):
            guard let encoded = String(data: data, encoding: .utf8),
                  let seed = Data(base64Encoded:
                    encoded.trimmingCharacters(in: .whitespacesAndNewlines)),
                  seed.count == 32 else {
                Log.write("orchestrator: archive key is invalid; preserving it and refusing queued-secret crypto")
                return nil
            }
            return SymmetricKey(data: seed)
        case .invalid, .unreadable:
            Log.write("orchestrator: archive key is unreadable; preserving it and refusing queued-secret crypto")
            return nil
        case .absent:
            let made = SymmetricKey(size: .bits256)
            let seed = made.withUnsafeBytes { Data($0) }
            let encoded = Data(seed.base64EncodedString().utf8)
            return OrchestratorPersistence.createSecret(encoded, at: archiveKeyURL) ? made : nil
        }
    }

    static func sealQueuedSecret(_ secret: String) -> String? {
        guard let key = archiveKey() else { return nil }
        return (try? ChaChaPoly.seal(Data(secret.utf8), using: key).combined)?
            .base64EncodedString()
    }

    static func openQueuedSecret(_ sealed: String) -> String? {
        guard let data = Data(base64Encoded: sealed),
              let box = try? ChaChaPoly.SealedBox(combined: data),
              let key = archiveKey(),
              let clear = try? ChaChaPoly.open(box, using: key) else { return nil }
        return String(data: clear, encoding: .utf8)
    }
}

extension RemoteServer {
    /// The one `orch/<machine>` body used by SSE and Cloud publication. An unhealthy registry is
    /// described but never represented by an empty `tasks` array, so existing clients retain the
    /// last authoritative snapshot they observed.
    static func orchestratorSnapshot(now: Date = Date()) -> [String: Any] {
        var out: [String: Any] = ["snippets": Snippets.records(),
                                  "at": Int(now.timeIntervalSince1970), "app": appStamp()]
        if Orchestrator.storeIsAuthoritative() {
            out["tasks"] = Orchestrator.records()
            out["schedules"] = Orchestrator.scheduleRecords(now: now)
        } else {
            out["store"] = Orchestrator.storeHealthRecord()
        }
        return out
    }

    static func appStamp() -> [String: Any] {
        ["version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
         "build": Self.buildStamp, "protocol": Self.protocolVersion]
    }
}
