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
    static let version = 1
    static let opening = "<clawdline-notice>"
    static let closing = "</clawdline-notice>"

    enum Audience: String, Equatable {
        case root, parent
    }

    enum TaskState: String, Equatable {
        case success, failure, timeout, cancelled
        case spawnFailed = "spawn_failed"
    }

    struct Task: Equatable {
        let id: String
        let title: String
    }

    struct Overlap: Equatable {
        let task: Task
        let path: String
    }

    /// The durable coordination relationship a file-wait notice is about.
    ///
    /// The id is the whole point. A session told that somebody is blocked on its paths can do
    /// nothing with that unless it can name the group it has to release, and the only other way
    /// to learn the id is to list every wait on the Mac and match the row by hand.
    struct FileWait: Equatable {
        let id: String
        let repository: String
        let paths: [String]
    }

    enum Event: Equatable {
        case taskFinished(task: Task, state: TaskState, audience: Audience,
                          resultPath: String, outstanding: Int,
                          claimsReleased: Bool, childMayStillWrite: Bool)
        case workspaceOverlap(task: Task, audience: Audience, overlaps: [Overlap])
        /// Somebody is waiting for paths this session owns. `releaseRoute` is the call that ends
        /// the wait, carried in the payload so a reader never has to guess the protocol.
        case fileWaitRequested(wait: FileWait, waiterSessionID: String, reason: String,
                               releaseCondition: String, releaseRoute: String)
        /// The owner let the paths go. `commit` and `note` are what the owner chose to say, and
        /// are `null` on the wire rather than absent, so the closed key set stays fixed.
        case fileWaitReleased(wait: FileWait, commit: String?, note: String?)
    }

    struct Notice: Equatable {
        let event: Event
        let body: String
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
              integer(obj["version"]) == version,
              let kind = string(obj["kind"]),
              let body = string(obj["body"]), !body.isEmpty
        else { return nil }

        // `audience` and `task` belong to the two task-tree events and are parsed inside them.
        // A file-wait notice is about a relationship between two sessions rather than about a
        // dispatched task, so requiring those fields of every kind would mean inventing a task
        // that does not exist just to satisfy the envelope.
        switch kind {
        case "task_finished":
            guard keys(obj) == Set(["protocol", "version", "kind", "audience", "task",
                                    "state", "result_path", "outstanding",
                                    "claims_released", "child_may_still_write", "body"]),
                  let audience = audience(obj["audience"]),
                  let task = task(obj["task"]),
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
                          body: body)

        case "workspace_overlap":
            guard keys(obj) == Set(["protocol", "version", "kind", "audience", "task",
                                    "overlaps", "body"]),
                  let audience = audience(obj["audience"]),
                  let task = task(obj["task"]),
                  let rows = obj["overlaps"] as? [Any], !rows.isEmpty else { return nil }
            let overlaps = rows.compactMap(overlap)
            guard overlaps.count == rows.count else { return nil }
            return Notice(event: .workspaceOverlap(task: task, audience: audience,
                                                    overlaps: overlaps), body: body)

        case "file_wait_requested":
            guard keys(obj) == Set(["protocol", "version", "kind", "wait", "waiter_session_id",
                                    "reason", "release_condition", "release_route", "body"]),
                  let wait = fileWait(obj["wait"]),
                  let waiter = string(obj["waiter_session_id"]), !waiter.isEmpty,
                  let reason = string(obj["reason"]), !reason.isEmpty,
                  let condition = string(obj["release_condition"]), !condition.isEmpty,
                  let route = string(obj["release_route"]), !route.isEmpty
            else { return nil }
            return Notice(event: .fileWaitRequested(wait: wait, waiterSessionID: waiter,
                                                    reason: reason,
                                                    releaseCondition: condition,
                                                    releaseRoute: route), body: body)

        case "file_wait_released":
            guard keys(obj) == Set(["protocol", "version", "kind", "wait", "commit",
                                    "note", "body"]),
                  let wait = fileWait(obj["wait"]),
                  let commit = nullableString(obj["commit"]),
                  let note = nullableString(obj["note"])
            else { return nil }
            return Notice(event: .fileWaitReleased(wait: wait, commit: commit, note: note),
                          body: body)

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
        case let .workspaceOverlap(task, audience, overlaps):
            out.merge([
                "kind": "workspace_overlap",
                "audience": audience.rawValue,
                "task": taskObject(task),
                "overlaps": overlaps.map {
                    ["task": taskObject($0.task), "path": $0.path]
                },
            ]) { _, new in new }
        case let .fileWaitRequested(wait, waiter, reason, condition, route):
            out.merge([
                "kind": "file_wait_requested",
                "wait": fileWaitObject(wait),
                "waiter_session_id": waiter,
                "reason": reason,
                "release_condition": condition,
                "release_route": route,
            ]) { _, new in new }
        case let .fileWaitReleased(wait, commit, note):
            out.merge([
                "kind": "file_wait_released",
                "wait": fileWaitObject(wait),
                "commit": commit as Any? ?? NSNull(),
                "note": note as Any? ?? NSNull(),
            ]) { _, new in new }
        }
        return out
    }

    private static func taskObject(_ task: Task) -> [String: Any] {
        ["id": task.id, "title": task.title]
    }

    private static func fileWaitObject(_ wait: FileWait) -> [String: Any] {
        ["id": wait.id, "repository": wait.repository, "paths": wait.paths]
    }

    private static func fileWait(_ raw: Any?) -> FileWait? {
        guard let obj = raw as? [String: Any],
              keys(obj) == Set(["id", "repository", "paths"]),
              let id = string(obj["id"]), !id.isEmpty,
              let repository = string(obj["repository"]), !repository.isEmpty,
              let rows = obj["paths"] as? [Any], !rows.isEmpty else { return nil }
        let paths = rows.compactMap(string).filter { !$0.isEmpty }
        guard paths.count == rows.count else { return nil }
        return FileWait(id: id, repository: repository, paths: paths)
    }

    private static func audience(_ raw: Any?) -> Audience? {
        string(raw).flatMap(Audience.init(rawValue:))
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

    private static func keys(_ obj: [String: Any]) -> Set<String> { Set(obj.keys) }
    private static func string(_ raw: Any?) -> String? { raw as? String }
    /// A field that is either a non-empty string or an explicit JSON `null`. The key itself is
    /// never optional: a missing key still fails the closed key set, so "the owner said nothing"
    /// and "somebody rewrote the envelope" stay different things.
    private static func nullableString(_ raw: Any?) -> String?? {
        if raw is NSNull { return .some(nil) }
        guard let text = string(raw), !text.isEmpty else { return nil }
        return .some(text)
    }
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
