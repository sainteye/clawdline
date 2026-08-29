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
    /// The literal text-only schema already stored in transcripts.
    static let version = 1
    /// Adds only a bounded array of typed, byte-free image artifact references.
    static let artifactVersion = 2
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
        let artifacts: [SessionImageArtifact]

        init(source: Source, body: String, artifacts: [SessionImageArtifact] = []) {
            self.source = source
            self.body = body
            self.artifacts = artifacts
        }
    }

    static func encode(_ message: Message) -> String {
        var payload: [String: Any] = [
            "protocol": protocolName,
            "version": message.artifacts.isEmpty ? version : artifactVersion,
            "kind": "session_message",
            "source": [
                "id": message.source.id,
                "label": message.source.label,
                "assistant": message.source.assistant.rawValue,
            ],
            "body": message.body,
        ]
        if !message.artifacts.isEmpty {
            payload["artifacts"] = message.artifacts.map(\.object)
        }
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
              object["protocol"] as? String == protocolName,
              object["kind"] as? String == "session_message",
              let wireVersion = object["version"] as? Int,
              let body = object["body"] as? String,
              let source = object["source"] as? [String: Any],
              Set(source.keys) == Set(["id", "label", "assistant"]),
              let id = source["id"] as? String, !id.isEmpty,
              let label = source["label"] as? String, !label.isEmpty,
              let assistantName = source["assistant"] as? String,
              let assistant = Assistant(rawValue: assistantName) else { return nil }
        let artifacts: [SessionImageArtifact]
        switch wireVersion {
        case version:
            guard Set(object.keys) == Set(["protocol", "version", "kind", "source", "body"]),
                  !body.isEmpty else { return nil }
            artifacts = []
        case artifactVersion:
            guard Set(object.keys) == Set([
                "protocol", "version", "kind", "source", "body", "artifacts",
            ]),
            let rawArtifacts = object["artifacts"] as? [[String: Any]],
            !rawArtifacts.isEmpty,
            rawArtifacts.count <= SessionImageArtifactStore.productionPolicy.maxImagesPerMessage
            else { return nil }
            artifacts = rawArtifacts.compactMap(SessionImageArtifact.decode)
            guard artifacts.count == rawArtifacts.count,
                  artifacts.reduce(0, { $0 + $1.byteCount })
                    <= SessionImageArtifactStore.productionPolicy.maxTotalBytes,
                  !body.isEmpty || !artifacts.isEmpty else { return nil }
        default:
            return nil
        }
        return Message(source: Source(id: id, label: label, assistant: assistant),
                       body: body, artifacts: artifacts)
    }
}
