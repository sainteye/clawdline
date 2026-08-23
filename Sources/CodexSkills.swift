import Foundation

/// The skills the *running Codex session* was actually started with.
///
/// Codex writes its initial skills catalog into the rollout beside the conversation. That is a
/// better source than walking every cache directory on the Mac: it already reflects repository
/// scope, disabled skills, bundled skills and enabled plugins, and it cannot accidentally offer
/// an old plugin that merely happens to remain on disk.
enum CodexSkills {
    private static let marker = "### Available skills"

    static func available(in rollout: URL) -> [AssistantSkill] {
        guard let handle = try? FileHandle(forReadingFrom: rollout) else { return [] }
        defer { try? handle.close() }
        // The catalog is part of the initial developer instructions. Cap the read so opening `/`
        // in a month-long conversation never means loading the month-long conversation.
        guard let data = try? handle.read(upToCount: 4 << 20),
              let prefix = String(data: data, encoding: .utf8) else { return [] }

        for raw in prefix.split(separator: "\n") {
            guard raw.contains(marker),
                  let line = raw.data(using: .utf8),
                  let json = try? JSONSerialization.jsonObject(with: line),
                  let instructions = catalogText(in: json) else { continue }
            return parse(instructions)
        }
        return []
    }

    /// Kept pure both for fixtures and because rollout JSON has changed shape before. The only
    /// contract this needs is the human-readable block Codex itself put into the instructions.
    static func parse(_ instructions: String) -> [AssistantSkill] {
        guard let start = instructions.range(of: marker) else { return [] }
        var out: [AssistantSkill] = []
        for raw in instructions[start.upperBound...].split(separator: "\n",
                                                            omittingEmptySubsequences: false) {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.hasPrefix("</skills_instructions>") { break }
            guard line.hasPrefix("- ") else { continue }
            let item = String(line.dropFirst(2))
            guard let divider = item.range(of: ": ") else { continue }
            let command = String(item[..<divider.lowerBound])
            guard valid(command) else { continue }

            var description = String(item[divider.upperBound...])
            let locatorMarkers = [" (file:", " (executor package:", " (orchestrator package:",
                                  " (custom resource:"]
            let cuts = locatorMarkers.compactMap { description.range(of: $0)?.lowerBound }
            if let cut = cuts.min() { description = String(description[..<cut]) }

            let source: AssistantSkill.Source
            if command.contains(":") { source = .plugin }
            else if item.contains("/skills/.system/") { source = .system }
            else if item.contains("/etc/codex/skills/") { source = .admin }
            else if item.contains(NSHomeDirectory() + "/.agents/skills/") { source = .personal }
            else if item.contains("/.agents/skills/") { source = .project }
            else { source = .personal }
            out.append(AssistantSkill(command: command, description: clean(description),
                                      source: source))
        }
        return out
    }

    private static func catalogText(in value: Any) -> String? {
        if let text = value as? String, text.contains(marker) { return text }
        if let array = value as? [Any] {
            for item in array { if let found = catalogText(in: item) { return found } }
        }
        if let object = value as? [String: Any] {
            for item in object.values { if let found = catalogText(in: item) { return found } }
        }
        return nil
    }

    private static func valid(_ command: String) -> Bool {
        guard (1...160).contains(command.count) else { return false }
        return command.allSatisfy { $0.isLetter || $0.isNumber || "-_.:".contains($0) }
    }

    private static func clean(_ text: String) -> String {
        let oneLine = text.unicodeScalars.map { scalar -> String in
            CharacterSet.controlCharacters.contains(scalar) || CharacterSet.newlines.contains(scalar)
                ? " " : String(scalar)
        }.joined().split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
        return String(oneLine.prefix(240))
    }
}
