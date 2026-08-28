import Foundation

/// The one semantic message Clawdline types into an assistant session.
///
/// The terminal transport has no metadata channel: both Claude Code and Codex record injected
/// input as a user turn. This envelope therefore keeps a short prose body for the model and a
/// closed, versioned payload for transcript readers. Recognition is deliberately all-or-nothing.
/// Anything outside the exact wrapper, or any field this version does not understand, remains a
/// visible ordinary user message.
enum ClawdlineMessage {
    static let protocolName = "clawdline.notice"
    /// New writers use version 2. The decoder keeps the closed version-1 schema below because
    /// those envelopes already live in transcript files and remain part of the wire contract.
    static let version = 2
    static let opening = "<clawdline-notice>"
    static let closing = "</clawdline-notice>"

    enum Audience: String, Equatable {
        case root, parent, owner, waiter, source
    }

    enum TaskState: String, Equatable {
        case success, failure, timeout, cancelled
        case spawnFailed = "spawn_failed"
    }

    enum HandoffState: String, Equatable {
        case pickedUp = "picked_up"
        case firstLineFailed = "first_line_failed"
    }

    enum HandoffAssistant: String, Equatable {
        case claude, codex

        /// Keep the wire's closed assistant vocabulary explicit. When `Assistant` grows, this
        /// exhaustive switch makes the compiler require a protocol decision instead of leaving
        /// a force unwrap to trap while settling a handoff.
        init(_ assistant: Assistant) {
            switch assistant {
            case .claude: self = .claude
            case .codex: self = .codex
            }
        }
    }

    struct Task: Equatable {
        let id: String
        let title: String
    }

    struct Overlap: Equatable {
        let task: Task
        let path: String
    }

    enum Event: Equatable {
        case taskFinished(task: Task, state: TaskState, audience: Audience,
                          resultPath: String, outstanding: Int,
                          claimsReleased: Bool, childMayStillWrite: Bool)
        case workspaceOverlap(task: Task, audience: Audience, overlaps: [Overlap])
        case fileWaitRequest(waitID: String, repository: String, paths: [String],
                             waiterSessionID: String, reason: String,
                             releaseCondition: String)
        case fileWaitRelease(waitID: String, repository: String, paths: [String],
                             commit: String?, note: String?)
        case handoffReceipt(handoffID: String, title: String?,
                            assistant: HandoffAssistant, projectDir: String,
                            state: HandoffState)
    }

    struct CompletionAcknowledgement: Equatable {
        let noticeID: String
        let path: String
    }

    struct Notice: Equatable {
        let event: Event
        let body: String
        let completionAcknowledgement: CompletionAcknowledgement?

        init(event: Event, body: String,
             completionAcknowledgement: CompletionAcknowledgement? = nil) {
            self.event = event
            self.body = body
            self.completionAcknowledgement = completionAcknowledgement
        }
    }

    static func encode(_ notice: Notice) -> String {
        let payload = object(for: notice)
        guard JSONSerialization.isValidJSONObject(payload),
              let data = try? JSONSerialization.data(withJSONObject: payload,
                                                     options: [.sortedKeys,
                                                               .withoutEscapingSlashes]),
              let json = String(data: data, encoding: .utf8),
              !json.contains("\n"), !json.contains("\r")
        else { return notice.body }
        return opening + json + closing
    }

    static func decode(_ message: String) -> Notice? {
        guard !message.contains("\n"), !message.contains("\r"),
              message.hasPrefix(opening), message.hasSuffix(closing),
              message.count > opening.count + closing.count else { return nil }
        let start = message.index(message.startIndex, offsetBy: opening.count)
        let end = message.index(message.endIndex, offsetBy: -closing.count)
        let json = String(message[start..<end])
        guard let data = json.data(using: .utf8),
              let raw = try? JSONSerialization.jsonObject(with: data),
              let obj = raw as? [String: Any],
              string(obj["protocol"]) == protocolName,
              let wireVersion = integer(obj["version"]), [1, version].contains(wireVersion),
              let kind = string(obj["kind"]),
              let body = string(obj["body"]), !body.isEmpty
        else { return nil }

        switch kind {
        case "task_finished":
            let legacyKeys = Set(["protocol", "version", "kind", "audience", "task",
                                  "state", "result_path", "outstanding",
                                  "claims_released", "child_may_still_write", "body"])
            let v2Keys = legacyKeys.union(["notice_id", "ack_path"])
            let completion: CompletionAcknowledgement?
            if wireVersion == version, keys(obj) == v2Keys,
               let noticeID = nonemptyString(obj["notice_id"]), UUID(uuidString: noticeID) != nil,
               let path = nonemptyString(obj["ack_path"]), path.hasPrefix("/") {
                completion = CompletionAcknowledgement(noticeID: noticeID.lowercased(), path: path)
            } else if keys(obj) == legacyKeys {
                // Literal v1 envelopes and the short-lived original v2 writer remain readable.
                // They have no delivery identity and therefore cannot be ACKed or deduplicated.
                completion = nil
            } else {
                return nil
            }
            guard let audience = taskAudience(obj),
                  let task = task(obj["task"]),
                  wireVersion == 1 || wireVersion == version,
                  wireVersion != 1 || completion == nil,
                  let rawState = string(obj["state"]),
                  let state = TaskState(rawValue: rawState),
                  let resultPath = string(obj["result_path"]), !resultPath.isEmpty,
                  let outstanding = integer(obj["outstanding"]), outstanding >= 0,
                  let claimsReleased = boolean(obj["claims_released"]),
                  let childMayStillWrite = boolean(obj["child_may_still_write"])
            else { return nil }
            return Notice(event: .taskFinished(task: task, state: state, audience: audience,
                                               resultPath: resultPath,
                                               outstanding: outstanding,
                                               claimsReleased: claimsReleased,
                                               childMayStillWrite: childMayStillWrite),
                          body: body, completionAcknowledgement: completion)

        case "workspace_overlap":
            guard let audience = taskAudience(obj),
                  let task = task(obj["task"]),
                  wireVersion == 1 || wireVersion == version,
                  keys(obj) == Set(["protocol", "version", "kind", "audience", "task",
                                    "overlaps", "body"]),
                  let rows = obj["overlaps"] as? [Any], !rows.isEmpty else { return nil }
            let overlaps = rows.compactMap(overlap)
            guard overlaps.count == rows.count else { return nil }
            return Notice(event: .workspaceOverlap(task: task, audience: audience,
                                                    overlaps: overlaps), body: body)

        case "file_wait_request":
            guard wireVersion == version,
                  keys(obj) == Set(["protocol", "version", "kind", "audience", "wait_id",
                                    "repository", "paths", "waiter_session_id", "reason",
                                    "release_condition", "body"]),
                  string(obj["audience"]) == Audience.owner.rawValue,
                  let waitID = nonemptyString(obj["wait_id"]),
                  let repository = nonemptyString(obj["repository"]),
                  let paths = nonemptyStrings(obj["paths"]),
                  let waiterSessionID = nonemptyString(obj["waiter_session_id"]),
                  let reason = nonemptyString(obj["reason"]),
                  let releaseCondition = nonemptyString(obj["release_condition"])
            else { return nil }
            return Notice(event: .fileWaitRequest(
                waitID: waitID, repository: repository, paths: paths,
                waiterSessionID: waiterSessionID, reason: reason,
                releaseCondition: releaseCondition), body: body)

        case "file_wait_release":
            let required = Set(["protocol", "version", "kind", "audience", "wait_id",
                                "repository", "paths", "body"])
            let allowed = required.union(["commit", "note"])
            guard wireVersion == version, required.isSubset(of: keys(obj)),
                  keys(obj).isSubset(of: allowed),
                  string(obj["audience"]) == Audience.waiter.rawValue,
                  let waitID = nonemptyString(obj["wait_id"]),
                  let repository = nonemptyString(obj["repository"]),
                  let paths = nonemptyStrings(obj["paths"]),
                  let commit = optionalNonemptyString(obj, key: "commit"),
                  let note = optionalNonemptyString(obj, key: "note")
            else { return nil }
            return Notice(event: .fileWaitRelease(
                waitID: waitID, repository: repository, paths: paths,
                commit: commit, note: note), body: body)

        case "handoff_receipt":
            let required = Set(["protocol", "version", "kind", "audience", "handoff_id",
                                "assistant", "project_dir", "state", "body"])
            let allowed = required.union(["title"])
            guard wireVersion == version, required.isSubset(of: keys(obj)),
                  keys(obj).isSubset(of: allowed),
                  string(obj["audience"]) == Audience.source.rawValue,
                  let handoffID = nonemptyString(obj["handoff_id"]),
                  let assistantRaw = string(obj["assistant"]),
                  let assistant = HandoffAssistant(rawValue: assistantRaw),
                  let projectDir = nonemptyString(obj["project_dir"]),
                  let stateRaw = string(obj["state"]),
                  let state = HandoffState(rawValue: stateRaw),
                  let title = optionalString(obj, key: "title")
            else { return nil }
            return Notice(event: .handoffReceipt(
                handoffID: handoffID, title: title, assistant: assistant,
                projectDir: projectDir, state: state), body: body)

        default:
            return nil
        }
    }

    /// The already-validated shape sent to the web client. It deliberately carries no styling,
    /// HTML, Markdown, action, or URL supplied by the envelope.
    static func webObject(for notice: Notice) -> [String: Any] {
        var out = object(for: notice)
        out.removeValue(forKey: "protocol")
        out.removeValue(forKey: "version")
        out.removeValue(forKey: "body")
        return out
    }

    private static func object(for notice: Notice) -> [String: Any] {
        var out: [String: Any] = [
            "protocol": protocolName,
            "version": version,
            "body": notice.body,
        ]
        switch notice.event {
        case let .taskFinished(task, state, audience, resultPath, outstanding,
                               claimsReleased, childMayStillWrite):
            out.merge([
                "kind": "task_finished",
                "audience": audience.rawValue,
                "task": taskObject(task),
                "state": state.rawValue,
                "result_path": resultPath,
                "outstanding": outstanding,
                "claims_released": claimsReleased,
                "child_may_still_write": childMayStillWrite,
            ]) { _, new in new }
            if let completion = notice.completionAcknowledgement {
                out["notice_id"] = completion.noticeID
                out["ack_path"] = completion.path
            }
        case let .workspaceOverlap(task, audience, overlaps):
            out.merge([
                "kind": "workspace_overlap",
                "audience": audience.rawValue,
                "task": taskObject(task),
                "overlaps": overlaps.map {
                    ["task": taskObject($0.task), "path": $0.path]
                },
            ]) { _, new in new }
        case let .fileWaitRequest(waitID, repository, paths, waiterSessionID, reason,
                                  releaseCondition):
            out.merge([
                "kind": "file_wait_request",
                "audience": Audience.owner.rawValue,
                "wait_id": waitID,
                "repository": repository,
                "paths": paths,
                "waiter_session_id": waiterSessionID,
                "reason": reason,
                "release_condition": releaseCondition,
            ]) { _, new in new }
        case let .fileWaitRelease(waitID, repository, paths, commit, note):
            out.merge([
                "kind": "file_wait_release",
                "audience": Audience.waiter.rawValue,
                "wait_id": waitID,
                "repository": repository,
                "paths": paths,
            ]) { _, new in new }
            if let commit { out["commit"] = commit }
            if let note { out["note"] = note }
        case let .handoffReceipt(handoffID, title, assistant, projectDir, state):
            out.merge([
                "kind": "handoff_receipt",
                "audience": Audience.source.rawValue,
                "handoff_id": handoffID,
                "assistant": assistant.rawValue,
                "project_dir": projectDir,
                "state": state.rawValue,
            ]) { _, new in new }
            if let title { out["title"] = title }
        }
        return out
    }

    private static func taskObject(_ task: Task) -> [String: Any] {
        ["id": task.id, "title": task.title]
    }

    private static func task(_ raw: Any?) -> Task? {
        guard let obj = raw as? [String: Any], keys(obj) == Set(["id", "title"]),
              let id = string(obj["id"]), !id.isEmpty,
              let title = string(obj["title"]), !title.isEmpty else { return nil }
        return Task(id: id, title: title)
    }

    private static func overlap(_ raw: Any) -> Overlap? {
        guard let obj = raw as? [String: Any], keys(obj) == Set(["task", "path"]),
              let task = task(obj["task"]),
              let path = string(obj["path"]), !path.isEmpty else { return nil }
        return Overlap(task: task, path: path)
    }

    private static func taskAudience(_ obj: [String: Any]) -> Audience? {
        guard let raw = string(obj["audience"]), let audience = Audience(rawValue: raw),
              audience == .root || audience == .parent else { return nil }
        return audience
    }

    private static func nonemptyString(_ raw: Any?) -> String? {
        guard let value = string(raw), !value.isEmpty else { return nil }
        return value
    }

    private static func nonemptyStrings(_ raw: Any?) -> [String]? {
        guard let rows = raw as? [Any], !rows.isEmpty else { return nil }
        let values = rows.compactMap(nonemptyString)
        return values.count == rows.count ? values : nil
    }

    /// A double optional distinguishes a missing field (`.some(nil)`) from a present field with
    /// the wrong shape (`nil`), preserving whole-envelope recognition for optional keys.
    private static func optionalString(_ obj: [String: Any], key: String) -> String?? {
        guard let raw = obj[key] else { return .some(nil) }
        guard let value = string(raw) else { return nil }
        return .some(.some(value))
    }

    private static func optionalNonemptyString(_ obj: [String: Any], key: String) -> String?? {
        guard let raw = obj[key] else { return .some(nil) }
        guard let value = nonemptyString(raw) else { return nil }
        return .some(.some(value))
    }

    private static func keys(_ obj: [String: Any]) -> Set<String> { Set(obj.keys) }
    private static func string(_ raw: Any?) -> String? { raw as? String }
    private static func boolean(_ raw: Any?) -> Bool? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) == CFBooleanGetTypeID() else { return nil }
        return number.boolValue
    }
    private static func integer(_ raw: Any?) -> Int? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let value = number.intValue
        return number.doubleValue == Double(value) ? value : nil
    }
}
