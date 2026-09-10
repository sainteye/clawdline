import CryptoKit
import Foundation

/// Moves existing producer facts into the Timeline owner on a low-priority, single-flight lane.
enum ProjectTimelineIntegration {
    private static let queue = DispatchQueue(label: "clawdline.timeline.sources", qos: .utility)
    private static var reads = ProjectTimelineReadCache()
    private static var prepared = false
    private static var storeForTesting: ProjectTimelineStore?
    private static var presentations: [String: ProjectBoardIntegration.ProjectPresentation] = [:]
    private static var generation: UInt64 = 0
    private static var checkpointFailures: [String: String] = [:]
    private static let checkpointFailuresLock = NSLock()

    static func prepare() {
        queue.async {
            guard !prepared else { return }
            prepared = true
            let store = storeForTesting ?? .shared
            let places = StartPoints.places(limit: ProjectTimelineGitImporter.maximumRepositories)
            presentations = ProjectBoardIntegration.projectPresentations(places)
            reads.seed(model(store))
            let paths = importStartup(places.map(\.path), store: store)
            refresh(store)
            scheduleBackfill(paths, index: 0, owned: generation)
        }
    }

    private static func importStartup(_ rawPaths: [String], store: ProjectTimelineStore) -> [String] {
        var seen = Set<String>()
        let paths = rawPaths.compactMap { UsageLedger.canonicalProjectKey(projectDir: $0) }
            .filter { seen.insert($0).inserted }.prefix(ProjectTimelineGitImporter.maximumRepositories)
        for path in paths {
            guard store.enabled else { break }
            importPage(path, store: store)
        }
        return Array(paths)
    }

    static func startupForTesting(_ paths: [String]) -> [String] {
        queue.sync {
            guard let store = storeForTesting else { return [] }
            let slots = importStartup(paths, store: store)
            refresh(store)
            return slots
        }
    }

    // One repository / forty commits per minute; no GET, refresh button or filter starts Git.
    private static func scheduleBackfill(_ paths: [String], index: Int, owned: UInt64) {
        guard !paths.isEmpty else { return }
        queue.asyncAfter(deadline: .now() + 60) {
            guard generation == owned else { return }
            let store = storeForTesting ?? .shared
            if store.enabled {
                let path = paths[index % paths.count]
                let project = ProjectBoardIntegration.projectID(path)
                if store.historyCheckpoint(projectID: project)?.historyStatus != "capacity" {
                    importPage(path, store: store)
                    refresh(store)
                }
            }
            scheduleBackfill(paths, index: (index + 1) % paths.count, owned: owned)
        }
    }

    private static func importPage(_ path: String, store: ProjectTimelineStore) {
        let project = ProjectBoardIntegration.projectID(path)
        let checkpoint = ProjectTimelineGitImporter.importHistory(path: path, projectID: project, store: store)
        let result = store.recordCheckpoint(checkpoint)
        checkpointFailuresLock.lock()
        if result.persisted && ["accepted", "unchanged"].contains(result.status) { checkpointFailures.removeValue(forKey: project) }
        else { checkpointFailures[project] = result.reason ?? "git_checkpoint_not_persisted" }
        checkpointFailuresLock.unlock()
    }

    /// Existing broker landing producer. A verified target landing creates only `landed_to_git`.
    static func observeBrokerRecord(_ record: [String: Any]) {
        queue.async {
            let store = storeForTesting ?? .shared
            guard store.enabled,
                  let taskID = record["id"] as? String,
                  let path = record["project_dir"] as? String,
                  let canonical = UsageLedger.canonicalProjectKey(
                    projectDir: path, repositoryCommonDir: record["repository_common_dir"] as? String),
                  let landing = record["landing"] as? [String: Any],
                  landing["state"] as? String == "landed",
                  let commit = landing["verified_commit"] as? String,
                  ProjectTimelineRemote.validSHA(commit) else { return }
            let repository = ProjectTimelineGitImporter.read(path: canonical, limit: 1).repositoryID
            let projectID = ProjectBoardIntegration.projectID(canonical)
            let entryID = "tl_broker_" + digest(taskID).prefix(24)
            let title = (record["title"] as? String).flatMap { $0.isEmpty ? nil : $0 }
                ?? "Broker delivery " + String(taskID.prefix(8))
            let board = (record["work_item_id"] as? String).map { [$0] } ?? []
            let effective = (landing["landed_at"] as? Double)
                ?? (record["finished_at"] as? Double) ?? Date().timeIntervalSince1970
            let observed = Date().timeIntervalSince1970
            let entry = ProjectTimelineEntry(id: entryID, projectID: projectID,
                deliveryKey: "broker-task:" + taskID, primaryCategory: "feature", tags: ["git"],
                originalTitle: title, summary: "Verified target-branch landing; deployment is not established.",
                requiredTargets: [], boardItemIDs: board, relations: [],
                sourceRevisions: [.init(repositoryID: repository, commit: commit, role: "delivery")],
                createdAt: effective)
            let sourceID = taskID + ":" + commit.lowercased()
            let event = ProjectTimelineEvent(id: "ev_broker_" + digest(sourceID).prefix(24),
                producer: "orchestrator_landing", sourceID: sourceID, sourceVersion: 1,
                entryID: entryID, kind: "landed_to_git", target: nil,
                effectiveAt: effective, observedAt: observed, authority: "broker_verified_target_landing",
                revision: commit.lowercased(), previousRevision: nil, result: "passed",
                cohortPercent: nil, beforeDigest: nil, afterDigest: nil, verification: nil,
                sourceURL: nil)
            let outcome = store.ingest(entry: entry, events: [event], producer: event.producer,
                                       sourceVersion: event.sourceVersion)
            if outcome.status == "accepted" { refresh(store) }
        }
    }

    static func read(_ query: ProjectTimelineReadCache.Query) -> [String: Any] {
        let store = storeForTesting ?? .shared
        return reads.read(query, header: store.readHeader(), loading: loading(store)) {
            .success(model(store))
        }
    }

    static func didMutate() { queue.async { refresh(storeForTesting ?? .shared) } }
    static func modeDidChange(enabled: Bool) {
        let store = storeForTesting ?? .shared
        reads.refresh(header: store.readHeader()) { .success(model(store)) }
        if enabled { queue.async { refresh(store) } }
    }

    static func configureStoreForTesting(_ store: ProjectTimelineStore?) {
        queue.sync {
            storeForTesting = store
            generation &+= 1
            checkpointFailuresLock.lock(); checkpointFailures = [:]; checkpointFailuresLock.unlock()
            prepared = false
            presentations = [:]
            reads = store == nil ? ProjectTimelineReadCache()
                : synchronousCacheForTesting()
            if let store { reads.seed(model(store)) }
        }
    }

    static var cacheStateForTesting: ProjectTimelineReadCache.State { reads.stateForTesting }
    static func drainObservationsForTesting() { queue.sync {} }

    private static func refresh(_ store: ProjectTimelineStore) {
        reads.refresh(header: store.readHeader()) { .success(model(store)) }
    }

    private static func model(_ store: ProjectTimelineStore) -> ProjectTimelineReadCache.Model {
        var envelope = store.materializedEnvelope()
        if var timeline = envelope["timeline"] as? [String: Any] {
            timeline["projects"] = presentations.mapValues(\.jsonObject)
            checkpointFailuresLock.lock()
            timeline["historySourceIssues"] = checkpointFailures.map { ["projectId": $0.key, "code": $0.value] }
            checkpointFailuresLock.unlock()
            envelope["timeline"] = timeline
        }
        let timeline = envelope["timeline"] as? [String: Any]
        return .init(revision: timeline?["revision"] as? Int ?? store.readHeader().revision,
                     observedAt: Date().timeIntervalSince1970, envelope: envelope)
    }

    private static func loading(_ store: ProjectTimelineStore) -> [String: Any] {
        let header = store.readHeader()
        return ["timeline": ["schemaVersion": ProjectTimelineStore.schemaVersion,
            "available": true, "enabled": header.enabled, "revision": header.revision,
            "entries": [] as [[String: Any]], "coverage": ["status": "unknown",
                "reasons": ["materialization_pending"]]]]
    }

    private static func synchronousCacheForTesting() -> ProjectTimelineReadCache {
        ProjectTimelineReadCache(execute: { $0() })
    }

    private static func digest(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}
