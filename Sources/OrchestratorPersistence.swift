import CryptoKit
import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // SwiftPM owns CloudMachineMetadata; flat tests compile it alongside us.
#endif

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
    /// What a finished task wrote about itself, which is the largest thing a record holds and the
    /// one thing no reader of the *list* asks for.
    ///
    /// Measured on this Mac: `GET /v1/orchestrator/tasks` answered 3.69 MB for 635 tasks, and
    /// these four fields were 71% of it — `summary` 40%, `review` 13%, `graph` 10%, `progress` 8%.
    /// The whole snapshot is republished on every task record change, so on a busy machine that is
    /// several megabytes a minute down the SSE stream, and on the Cloud path one 6.5 MB envelope
    /// into a spool that has exactly one envelope in flight at a time. A transcript somebody has
    /// just opened waits behind it.
    ///
    /// Every reader of the list was checked rather than assumed: the console draws id, title,
    /// state, timestamps and the two terminal ids; `build.sh` reads state, id and title; the
    /// pre-commit guard reads claims, projectDir, isolation, state and both identity blocks. None
    /// of them names one of these four. The reader that does want them asks for one task, and
    /// `GET /v1/orchestrator/tasks/:id` still answers with the complete record — it is served from
    /// `Orchestrator.record(id:)`, which this does not touch.
    static let taskListOmittedFields: Set<String> = ["summary", "review", "graph", "progress"]

    /// Kept beside the omission it implements, so a reader can see both halves at once.
    static func taskListProjection(_ record: [String: Any]) -> [String: Any] {
        record.filter { !taskListOmittedFields.contains($0.key) }
    }

    /// Whether a list reader can still ask this snapshot about that task.
    ///
    /// **This is one half of a contract, and the other half is in the console.** `S.tasks` is read
    /// through exactly three functions in `view/derive.js` — `taskLive`, `taskOfChild` and
    /// `tasksOfRoot` — and every one of them asks only about a task that is either still running
    /// or whose child/root terminal is a session currently on screen. A finished task belonging to
    /// a terminal nobody is looking at cannot change one pixel, and shipping it costs the whole
    /// record on every republish.
    ///
    /// Measured here: 645 tasks projected to 1,604,415 bytes, of which the answer to this question
    /// was 210 tasks and 580,572 bytes — the sealed frame goes from 1.99 MB to about 0.72 MB. That
    /// frame is the p99 of everything this Mac writes to the socket (the rest are 2-8 KB session
    /// rows), and while it is being written the session snapshot behind it waits: `changed=11`
    /// publications sit at a p50 of 379 ms and a p95 of 14,477 ms.
    ///
    /// **It fails open, deliberately.** A state this build cannot parse, or a record with no state
    /// at all, is kept. The cost of keeping one task too many is bytes; the cost of dropping one
    /// the console still wanted is a row that silently loses its header, and a new non-terminal
    /// state added to `Orchestrator.State` would otherwise start disappearing from the list on an
    /// older Mac. `GET /v1/orchestrator/tasks` is unfiltered and unaffected — that is what
    /// `build.sh` and the pre-commit guard read, and the guard does not fail when it loses a task,
    /// it stops refusing.
    static func taskListRelevant(_ record: [String: Any], liveTerminals: Set<String>) -> Bool {
        guard let raw = record["state"] as? String else { return true }
        guard let state = Orchestrator.State(rawValue: raw) else { return true }
        if !state.isTerminal { return true }
        for side in ["child", "root"] {
            guard let block = record[side] as? [String: Any],
                  let terminal = block["terminalId"] as? String, !terminal.isEmpty
            else { continue }
            if liveTerminals.contains(terminal) { return true }
        }
        return false
    }

    /// The task-record fields a Cloud view reads, each a path. Checked against every reader in
    /// `Resources/web/app/js` on 2026-09-15 rather than assumed: `S.tasks` is read only by
    /// `view/derive.js` (`taskLive` and `taskWord` read `state`; `taskShaping` reads `finishedAt`
    /// and `child.terminalId`; `taskOfChild` reads `child.terminalId` and `created`; `rowDepth`,
    /// `tasksOfRoot` and `grouped` read `root.terminalId`; `featureRootChip` and `lostIfClosed`
    /// read `title` and `id`), by the Session row in `view/list.js` (`id`, `title`) and by the open
    /// Session's header in `view/transcript.js` (`title`, `usage.total`, `usage.costUsd`). Board,
    /// Usage, Projects, Devices, Settings, schedules and snippets read no task record.
    static let cloudTaskFields: [[String]] = [
        ["id"], ["state"], ["title"], ["created"], ["finishedAt"],
        ["child", "terminalId"], ["root", "terminalId"],
        ["usage", "total"], ["usage", "costUsd"],
    ]

    /// The `orch/` snapshot a Cloud viewer is sent (docs/cloud.md): every top-level key as
    /// ``orchestratorSnapshot(now:liveTerminals:)`` built it, with `tasks` cut to what a Cloud view
    /// can read. The local page, whose SSE event is that snapshot, and `GET /v1/orchestrator/tasks`
    /// are not projected.
    ///
    /// **A record goes when no Cloud view can reach it.** The local filter above keeps a finished
    /// task while its child *or its root* is on screen, and on 2026-09-15 that was 97 of the 101
    /// records in the snapshot — a root with a long history kept every child it ever had — and
    /// 216 of its 230 KB. But every Cloud reader of a finished task reaches it through its child:
    /// the header and the row through `taskOfChild` of a listed Session, and the chips, indents and
    /// grouping through `taskShaping`, which answers false for a finished task whose child is not a
    /// listed Session. `listedSessions` is the set of Sessions this Mac has published to Cloud and
    /// not tombstoned, which is exactly what those readers compare against. A root's own chip counts
    /// its children through `taskShaping` too, so the root side keeps nothing a Cloud view draws.
    ///
    /// It fails open the same way the local filter does: a record with no state, or a state this
    /// build does not know, is kept, and a `tasks` value that is not a list of objects is left as
    /// it came. Of a kept record only ``cloudTaskFields`` go out.
    static func cloudOrchestratorProjection(
        _ snapshot: [String: Any], listedSessions: Set<String>
    ) -> [String: Any] {
        guard let tasks = snapshot["tasks"] as? [[String: Any]] else { return snapshot }
        var out = snapshot
        out["tasks"] = tasks
            .filter { cloudTaskRelevant($0, listedSessions: listedSessions) }
            .map(cloudTaskProjection)
        return out
    }

    static func cloudTaskRelevant(_ record: [String: Any], listedSessions: Set<String>) -> Bool {
        guard let raw = record["state"] as? String,
              let state = Orchestrator.State(rawValue: raw), state.isTerminal
        else { return true }
        guard let child = record["child"] as? [String: Any],
              let terminal = child["terminalId"] as? String, !terminal.isEmpty
        else { return false }
        return listedSessions.contains(terminal)
    }

    /// Only ``cloudTaskFields``; a path the record does not have stays absent, and a value is copied
    /// as it is (`usage.costUsd` is left out by the Mac rather than zero for plan-billed work).
    static func cloudTaskProjection(_ record: [String: Any]) -> [String: Any] {
        var out: [String: Any] = [:]
        for path in cloudTaskFields {
            if path.count == 1 {
                if let value = record[path[0]] { out[path[0]] = value }
            } else if let parent = record[path[0]] as? [String: Any], let value = parent[path[1]] {
                var nested = out[path[0]] as? [String: Any] ?? [:]
                nested[path[1]] = value
                out[path[0]] = nested
            }
        }
        return out
    }

    /// `liveTerminals` is injectable so the contract above can be tested without a screen. The
    /// production answer comes from the same published inventory the session snapshot is built
    /// from, which is lock-guarded and safe to read from whichever thread a record change
    /// arrived on.
    static func orchestratorSnapshot(
        now: Date = Date(), liveTerminals: Set<String>? = nil
    ) -> [String: Any] {
        var out: [String: Any] = ["snippets": Snippets.records(),
                                  "at": Int(now.timeIntervalSince1970), "app": appStamp(),
                                  "machine": machineDescriptor()]
        if Orchestrator.storeIsAuthoritative() {
            let terminals = liveTerminals
                ?? Set(SessionWatch.shared.publishedInventory().targets.map(\.id))
            out["tasks"] = Orchestrator.records()
                .filter { taskListRelevant($0, liveTerminals: terminals) }
                .map(taskListProjection)
            out["schedules"] = Orchestrator.scheduleRecords(now: now)
        } else {
            out["store"] = Orchestrator.storeHealthRecord()
        }
        return out
    }

    /// Display metadata only. The authenticated envelope channel remains the machine authority;
    /// neither the browser nor a command may route by this mutable human-facing name.
    static func machineDescriptor() -> [String: Any] {
        let metadata = CloudMachineMetadata.currentMac()
        let clean = metadata.name.components(separatedBy: .controlCharacters).joined(separator: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return ["name": clean.isEmpty ? "Mac" : String(clean.prefix(80)),
                "platform": metadata.platform]
    }

    static func appStamp() -> [String: Any] {
        ["version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?",
         "build": Self.buildStamp, "protocol": Self.protocolVersion]
    }
}
