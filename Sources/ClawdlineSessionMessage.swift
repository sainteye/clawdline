import Foundation

/// A message one live assistant session sends to another through Clawdline.
///
/// A terminal has no metadata channel: injected input is stored as a user turn by both Claude
/// Code and Codex. This closed envelope preserves the model-readable body while giving transcript
/// readers enough verified metadata to stop attributing the words to the person at the keyboard.
/// It is deliberately separate from ``ClawdlineMessage``: a session said this content, while a
/// notice is Clawdline reporting a broker fact about a task, wait or handoff.
enum ClawdlineSessionMessage {
    static let protocolName = "clawdline.message"
    static let version = 1
    static let opening = "<clawdline-message>"
    static let closing = "</clawdline-message>"

    struct Source: Equatable {
        let id: String
        let label: String
        let assistant: Assistant
    }

    struct Message: Equatable {
        let source: Source
        let body: String
    }

    static func encode(_ message: Message) -> String {
        let payload: [String: Any] = [
            "protocol": protocolName,
            "version": version,
            "kind": "session_message",
            "source": [
                "id": message.source.id,
                "label": message.source.label,
                "assistant": message.source.assistant.rawValue,
            ],
            "body": message.body,
        ]
        guard JSONSerialization.isValidJSONObject(payload),
              let data = try? JSONSerialization.data(withJSONObject: payload,
                                                     options: [.sortedKeys,
                                                               .withoutEscapingSlashes]),
              let json = String(data: data, encoding: .utf8),
              !json.contains("\n"), !json.contains("\r") else {
            return message.body
        }
        return opening + json + closing
    }

    static func decode(_ raw: String) -> Message? {
        guard !raw.contains("\n"), !raw.contains("\r"),
              raw.hasPrefix(opening), raw.hasSuffix(closing),
              raw.count > opening.count + closing.count else { return nil }
        let start = raw.index(raw.startIndex, offsetBy: opening.count)
        let end = raw.index(raw.endIndex, offsetBy: -closing.count)
        guard let data = String(raw[start..<end]).data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              Set(object.keys) == Set(["protocol", "version", "kind", "source", "body"]),
              object["protocol"] as? String == protocolName,
              object["version"] as? Int == version,
              object["kind"] as? String == "session_message",
              let body = object["body"] as? String, !body.isEmpty,
              let source = object["source"] as? [String: Any],
              Set(source.keys) == Set(["id", "label", "assistant"]),
              let id = source["id"] as? String, !id.isEmpty,
              let label = source["label"] as? String, !label.isEmpty,
              let assistantName = source["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName) else { return nil }
        return Message(source: Source(id: id, label: label, assistant: assistant), body: body)
    }
}
