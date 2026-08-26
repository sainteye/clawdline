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

    enum Event: Equatable {
        case taskFinished(task: Task, state: TaskState, audience: Audience,
                          resultPath: String, outstanding: Int,
                          claimsReleased: Bool, childMayStillWrite: Bool)
        case workspaceOverlap(task: Task, audience: Audience, overlaps: [Overlap])
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
              let body = string(obj["body"]), !body.isEmpty,
              let audienceRaw = string(obj["audience"]),
              let audience = Audience(rawValue: audienceRaw),
              let task = task(obj["task"])
        else { return nil }

        switch kind {
        case "task_finished":
            guard keys(obj) == Set(["protocol", "version", "kind", "audience", "task",
                                    "state", "result_path", "outstanding",
                                    "claims_released", "child_may_still_write", "body"]),
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
                  let rows = obj["overlaps"] as? [Any], !rows.isEmpty else { return nil }
            let overlaps = rows.compactMap(overlap)
            guard overlaps.count == rows.count else { return nil }
            return Notice(event: .workspaceOverlap(task: task, audience: audience,
                                                    overlaps: overlaps), body: body)

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
