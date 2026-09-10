import CryptoKit
import Foundation

/// Bounded, read-only Git history ingestion. It creates Git evidence, never deployment evidence.
enum ProjectTimelineGitImporter {
    static let maximumRepositories = 8
    static let maximumCommitsPerRepository = 40
    static let timeout: TimeInterval = 8

    struct Commit { let sha: String; let committedAt: Double; let subject: String }
    struct Reading { let repositoryID: String; let commits: [Commit]; let through: String?; let reason: String? }

    static func read(path: String, limit: Int = maximumCommitsPerRepository) -> Reading {
        let remote = runGit(path: path, ["config", "--get", "remote.origin.url"])
        let repositoryID = remote.flatMap {
            ProjectTimelineRemote.repositoryID($0.trimmingCharacters(in: .whitespacesAndNewlines))
        } ?? localRepositoryID(path)
        let remoteReason: String? = repositoryID.hasPrefix("github:") ? nil : "github_remote_unavailable"
        let format = "%H%x1f%ct%x1f%s"
        guard let output = runGit(path: path, ["log", "--first-parent", "--max-count=\(max(1, min(limit, maximumCommitsPerRepository)))", "--format=\(format)"])
        else { return Reading(repositoryID: repositoryID, commits: [], through: nil, reason: "git_history_unavailable") }
        let commits = output.split(separator: "\n", omittingEmptySubsequences: true).compactMap { line -> Commit? in
            let fields = line.split(separator: "\u{1f}", maxSplits: 2, omittingEmptySubsequences: false)
            guard fields.count == 3, ProjectTimelineRemote.validSHA(String(fields[0])),
                  let at = Double(fields[1]) else { return nil }
            return Commit(sha: String(fields[0]).lowercased(), committedAt: at,
                          subject: String(fields[2]).prefix(300).description)
        }
        return Reading(repositoryID: repositoryID, commits: commits,
                       through: commits.last?.sha, reason: remoteReason)
    }

    static func importHistory(path: String, projectID: String, store: ProjectTimelineStore,
                              observedAt: Double = Date().timeIntervalSince1970) -> ProjectTimelineCheckpoint {
        let reading = read(path: path)
        var imported = 0
        for commit in reading.commits {
            let key = reading.repositoryID + ":" + commit.sha
            let entryID = "tl_git_" + digest(key).prefix(24)
            let eventID = "ev_git_" + digest(key).prefix(24)
            let entry = ProjectTimelineEntry(id: entryID, projectID: projectID,
                deliveryKey: "git-history:" + key, primaryCategory: "feature", tags: ["git"],
                originalTitle: commit.subject.isEmpty ? "Git revision " + String(commit.sha.prefix(8)) : commit.subject,
                summary: "Read-only Git history evidence; deployment and availability are not established.",
                requiredTargets: [], boardItemIDs: [], relations: [],
                sourceRevisions: [.init(repositoryID: reading.repositoryID,
                                        commit: commit.sha, role: "source")],
                createdAt: commit.committedAt)
            let event = ProjectTimelineEvent(id: eventID, producer: "local_git_history",
                sourceID: key, sourceVersion: 1, entryID: entryID, kind: "git_history_observed",
                target: nil, effectiveAt: commit.committedAt, observedAt: commit.committedAt,
                authority: "local_git_history", revision: commit.sha, previousRevision: nil,
                result: "passed", cohortPercent: nil, beforeDigest: nil, afterDigest: nil,
                verification: nil, sourceURL: nil)
            let outcome = store.ingest(entry: entry, events: [event], producer: event.producer,
                                       sourceVersion: event.sourceVersion)
            if ["accepted", "unchanged"].contains(outcome.status) { imported += 1 }
        }
        let omitted = max(0, reading.commits.count - imported)
        return ProjectTimelineCheckpoint(source: "local_git_history",
            repositoryID: reading.repositoryID, throughRevision: reading.through,
            observedAt: observedAt, imported: imported, omitted: omitted, reason: reading.reason)
    }

    private static func runGit(path: String, _ arguments: [String]) -> String? {
        let process = Process(), output = Pipe(), errors = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        process.arguments = ["-C", path] + arguments
        process.standardOutput = output; process.standardError = errors
        let done = DispatchSemaphore(value: 0)
        process.terminationHandler = { _ in done.signal() }
        do { try process.run() } catch { return nil }
        if done.wait(timeout: .now() + timeout) == .timedOut {
            process.terminate()
            _ = done.wait(timeout: .now() + 1)
            return nil
        }
        guard process.terminationStatus == 0 else { return nil }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        guard data.count <= 256 * 1024 else { return nil }
        return String(data: data, encoding: .utf8)
    }

    private static func localRepositoryID(_ path: String) -> String {
        "local:" + digest(path).prefix(24)
    }

    private static func digest(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}
