import Foundation

struct ProjectTimelineTarget: Codable, Equatable, Hashable {
    let environment: String
    let component: String
    let audience: String

    var key: String { environment + ":" + component + ":" + audience }
    var scopeKey: String { environment + ":" + component }

    var jsonObject: [String: Any] {
        ["environment": environment, "component": component, "audience": audience]
    }
}

struct ProjectTimelineRevision: Codable, Equatable {
    let repositoryID: String
    let commit: String
    let role: String

    var githubURL: String? { ProjectTimelineRemote.commitURL(repositoryID: repositoryID, commit: commit) }

    var jsonObject: [String: Any] {
        ["repositoryId": repositoryID, "commit": commit, "shortCommit": String(commit.prefix(8)),
         "role": role, "githubUrl": githubURL as Any? ?? NSNull()]
    }
}

struct ProjectTimelineRelation: Codable, Equatable {
    let kind: String
    let targetEntryID: String

    var jsonObject: [String: Any] { ["kind": kind, "targetEntryId": targetEntryID] }
}

struct ProjectTimelineEntry: Codable, Equatable {
    let id: String
    let projectID: String
    let deliveryKey: String
    let primaryCategory: String
    let tags: [String]
    let originalTitle: String
    let summary: String
    let requiredTargets: [ProjectTimelineTarget]
    let boardItemIDs: [String]
    let relations: [ProjectTimelineRelation]
    let sourceRevisions: [ProjectTimelineRevision]
    let createdAt: Double
}

struct ProjectTimelineEvent: Codable, Equatable {
    let id: String
    let producer: String
    let sourceID: String
    let sourceVersion: Int
    let entryID: String
    let kind: String
    let target: ProjectTimelineTarget?
    let effectiveAt: Double?
    let observedAt: Double
    let authority: String
    let revision: String?
    let previousRevision: String?
    let result: String
    let cohortPercent: Double?
    let beforeDigest: String?
    let afterDigest: String?
    let verification: String?
    let sourceURL: String?
}

struct ProjectTimelineCheckpoint: Codable, Equatable {
    let source: String
    let repositoryID: String
    let throughRevision: String?
    let observedAt: Double
    let imported: Int
    let omitted: Int
    let reason: String?
}

struct ProjectTimelineProjection: Equatable {
    let status: String
    let coverage: String
    let basisEventIDs: [String]
    let availableTargets: Int
    let requiredTargets: Int
    let effectiveAt: Double?
    let observedAt: Double?

    var jsonObject: [String: Any] {
        ["status": status, "coverage": coverage, "basisEventIds": basisEventIDs,
         "availableTargets": availableTargets, "requiredTargets": requiredTargets,
         "effectiveAt": effectiveAt as Any? ?? NSNull(),
         "observedAt": observedAt as Any? ?? NSNull()]
    }
}

enum ProjectTimelineRemote {
    private static let segment = try! NSRegularExpression(pattern: "^[A-Za-z0-9_.-]+$")

    static func repositoryID(_ remote: String) -> String? {
        let value = remote.trimmingCharacters(in: .whitespacesAndNewlines)
        let path: String?
        if value.hasPrefix("git@github.com:") {
            path = String(value.dropFirst("git@github.com:".count))
        } else if let url = URL(string: value),
                  ["https", "http", "ssh", "git"].contains(url.scheme?.lowercased() ?? ""),
                  url.host?.lowercased() == "github.com", url.user == nil || url.user == "git" {
            path = String(url.path.drop(while: { $0 == "/" }))
        } else {
            path = nil
        }
        guard var path else { return nil }
        if path.hasSuffix(".git") { path.removeLast(4) }
        let parts = path.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 2, parts.allSatisfy(validSegment) else { return nil }
        return "github:" + parts[0].lowercased() + "/" + parts[1].lowercased()
    }

    static func commitURL(repositoryID: String, commit: String) -> String? {
        guard repositoryID.hasPrefix("github:"), validSHA(commit) else { return nil }
        let path = String(repositoryID.dropFirst("github:".count))
        let parts = path.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 2, parts.allSatisfy(validSegment) else { return nil }
        return "https://github.com/\(parts[0])/\(parts[1])/commit/\(commit.lowercased())"
    }

    static func validSHA(_ value: String) -> Bool {
        (7...64).contains(value.count) && value.unicodeScalars.allSatisfy {
            (48...57).contains($0.value) || (65...70).contains($0.value) || (97...102).contains($0.value)
        }
    }

    private static func validSegment(_ value: String) -> Bool {
        !value.isEmpty && segment.firstMatch(in: value, range: NSRange(value.startIndex..., in: value)) != nil
    }
}

enum ProjectTimelineReconciler {
    private static let production = "production"

    static func project(_ entry: ProjectTimelineEntry,
                        events: [ProjectTimelineEvent], environment: String = production) -> ProjectTimelineProjection {
        let relevant = events.filter { $0.entryID == entry.id }
        let productionEvents = relevant.filter { $0.target?.environment == environment }
        let required = entry.requiredTargets.filter { $0.environment == environment }
        let requiredKeys = Set(required.map(\.scopeKey))
        let requiredCount = requiredKeys.count
        let stateKinds = Set(["deploy_succeeded", "deploy_failed", "rollback"])
        var latestByScope: [String: ProjectTimelineEvent] = [:]
        for event in productionEvents.filter({ stateKinds.contains($0.kind) }) {
            guard let key = event.target?.scopeKey, requiredKeys.contains(key) else { continue }
            if let previous = latestByScope[key], !stableOrder(previous, event) { continue }
            latestByScope[key] = event
        }
        let rollbacks = latestByScope.values.filter {
            $0.kind == "rollback" && $0.result == "passed" && $0.previousRevision != nil
        }
        if !rollbacks.isEmpty {
            return projection(rollbacks.count == requiredCount && requiredCount > 0
                ? "rolled_back" : "partial_rollback", coverage: "complete",
                basis: rollbacks, available: 0, required: requiredCount)
        }

        let deploys = latestByScope.values.filter {
            $0.kind == "deploy_succeeded" && $0.result == "passed" && $0.revision != nil
        }
        let availability = deploys.compactMap { deploy -> ProjectTimelineEvent? in
            productionEvents.filter { event in
                event.kind == "availability_verified" && event.result == "passed"
                    && event.target?.scopeKey == deploy.target?.scopeKey
                    && event.revision == deploy.revision
                    && moment(event) >= moment(deploy)
            }.sorted(by: stableOrder).last
        }
        let availableKeys = Set(availability.compactMap { $0.target?.scopeKey }).intersection(requiredKeys)
        let limited = availability.contains { event in
            event.target?.audience != "all" || (event.cohortPercent.map { $0 < 100 } == true)
        }
        if requiredCount > 0, availableKeys.count == requiredCount {
            return projection(limited ? "limited" : "available", coverage: "complete",
                              basis: deploys + availability, available: availableKeys.count,
                              required: requiredCount)
        }
        if !availableKeys.isEmpty {
            return projection("partial", coverage: "partial", basis: deploys + availability,
                              available: availableKeys.count, required: requiredCount)
        }

        let failures = latestByScope.values.filter { $0.kind == "deploy_failed" && $0.result == "failed" }
        if !failures.isEmpty {
            return projection("deploy_failed", coverage: "partial", basis: failures,
                              available: 0, required: requiredCount)
        }
        if !deploys.isEmpty {
            return projection("deploy_pending", coverage: "partial", basis: deploys,
                              available: 0, required: requiredCount)
        }

        let operations = productionEvents.filter {
            $0.kind == "operation_applied" && $0.result == "passed"
        }.sorted(by: stableOrder)
        if let operation = operations.last {
            let verified = productionEvents.filter {
                $0.kind == "operation_verified" && $0.result == "passed" && $0.verification != nil
                    && $0.producer == operation.producer && $0.target == operation.target
                    && $0.beforeDigest == operation.beforeDigest && $0.afterDigest == operation.afterDigest
                    && moment($0) >= moment(operation)
            }.sorted(by: stableOrder).last
            return projection(verified == nil ? "operation_pending" : "available",
                              coverage: verified == nil ? "partial" : "complete",
                              basis: [operation] + (verified.map { [$0] } ?? []),
                              available: verified == nil ? 0 : 1,
                              required: 1)
        }

        let landed = relevant.filter {
            ["landed_to_git", "git_history_observed"].contains($0.kind) && $0.result == "passed"
        }
        if !landed.isEmpty {
            return projection("landed_to_git", coverage: "partial", basis: landed,
                              available: 0, required: requiredCount)
        }
        let planned = relevant.filter { $0.kind == "planned" }
        if !planned.isEmpty {
            return projection("upcoming", coverage: "partial", basis: planned,
                              available: 0, required: requiredCount)
        }
        return ProjectTimelineProjection(status: "unknown", coverage: "unknown",
            basisEventIDs: [], availableTargets: 0, requiredTargets: requiredCount,
            effectiveAt: nil, observedAt: relevant.map(\.observedAt).max())
    }

    private static func projection(_ status: String, coverage: String,
                                   basis: [ProjectTimelineEvent], available: Int, required: Int)
        -> ProjectTimelineProjection {
        ProjectTimelineProjection(
            status: status, coverage: coverage,
            basisEventIDs: basis.sorted(by: stableOrder).map(\.id),
            availableTargets: available, requiredTargets: required,
            effectiveAt: basis.compactMap(\.effectiveAt).max(),
            observedAt: basis.map(\.observedAt).max())
    }

    static func stableOrder(_ lhs: ProjectTimelineEvent, _ rhs: ProjectTimelineEvent) -> Bool {
        let left = moment(lhs), right = moment(rhs)
        return left == right ? lhs.id < rhs.id : left < right
    }

    private static func moment(_ event: ProjectTimelineEvent) -> Double {
        event.effectiveAt ?? event.observedAt
    }
}
