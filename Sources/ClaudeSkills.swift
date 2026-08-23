import Foundation

/// A command Claude Code can load from a `SKILL.md` file.
///
/// Only the menu metadata lives here. The body is Claude Code's to read when the command is
/// invoked; loading it into Clawdline would make a prompt browser into a second skill runtime,
/// with a second set of rules to get wrong.
struct ClaudeSkill: Equatable {
    enum Source: String {
        case project
        case personal
        case plugin
    }

    let command: String
    let description: String
    let source: Source
}

/// Discover the file-backed skills available from one Claude Code working directory.
///
/// Claude Code has no read-only command that asks an already-running terminal session for its
/// slash menu. Its Agent SDK publishes that list while starting a *new* session, but doing that on
/// every `/` would be a model-shaped side effect for an autocomplete. The stable, local half is
/// enough to be useful and honest: project skills, personal skills and installed plugin skills.
enum ClaudeSkills {
    private struct Metadata {
        var name: String?
        var description = ""
        var userInvocable = true
    }

    /// The effective list for `cwd`, after the same precedence that matters to a typed command:
    /// a personal skill replaces a project skill of the same name, while plugin skills keep their
    /// namespace and therefore cannot collide with either.
    static func available(cwd: String, home: String = NSHomeDirectory(),
                          fileManager fm: FileManager = .default) -> [ClaudeSkill] {
        let work = URL(fileURLWithPath: cwd).standardizedFileURL
        let homeURL = URL(fileURLWithPath: home).standardizedFileURL
        let root = repositoryRoot(from: work, fileManager: fm)
        let settings = settingsFor(work: work, root: root, home: homeURL, fileManager: fm)
        let overrides = settings.skillOverrides

        var effective: [String: ClaudeSkill] = [:]

        // Root first and the working directory last. A more specific project directory replaces
        // an ancestor if both publish the same command; personal replaces all of them below.
        for directory in projectDirectories(from: work, root: root) {
            for skill in skills(in: directory.appendingPathComponent(".claude/skills"),
                                source: .project, prefix: nil, fileManager: fm)
            where overrides[skill.command] != "off" {
                effective[skill.command] = skill
            }
        }

        for skill in skills(in: homeURL.appendingPathComponent(".claude/skills"),
                            source: .personal, prefix: nil, fileManager: fm)
        where overrides[skill.command] != "off" {
            effective[skill.command] = skill
        }

        for skill in pluginSkills(home: homeURL, enabled: settings.enabledPlugins,
                                  fileManager: fm) {
            effective[skill.command] = skill
        }

        return effective.values.sorted {
            $0.command.localizedStandardCompare($1.command) == .orderedAscending
        }
    }

    /// Match the way the slash menu feels: the beginning of a name wins, then a component after
    /// `-`, `_` or `:`, then a separator-free spelling, and only then words in the description.
    static func matching(_ skills: [ClaudeSkill], query: String) -> [ClaudeSkill] {
        let q = query.lowercased()
        guard !q.isEmpty else { return skills }

        func rank(_ skill: ClaudeSkill) -> Int? {
            let name = skill.command.lowercased()
            if name.hasPrefix(q) { return 0 }
            if name.split(whereSeparator: { "-_:".contains($0) })
                .contains(where: { $0.hasPrefix(q) }) { return 1 }
            let compact = name.filter { !"-_:".contains($0) }
            if compact.hasPrefix(q.filter { !"-_:".contains($0) }) { return 2 }
            if skill.description.lowercased().contains(q) { return 3 }
            return nil
        }

        return skills.compactMap { skill -> (ClaudeSkill, Int)? in
            rank(skill).map { (skill, $0) }
        }.sorted {
            if $0.1 != $1.1 { return $0.1 < $1.1 }
            return $0.0.command.localizedStandardCompare($1.0.command) == .orderedAscending
        }.map(\.0)
    }

    // MARK: - Files

    private static func repositoryRoot(from cwd: URL, fileManager fm: FileManager) -> URL? {
        var here = cwd
        while true {
            if fm.fileExists(atPath: here.appendingPathComponent(".git").path) { return here }
            let parent = here.deletingLastPathComponent()
            if parent.path == here.path { return nil }
            here = parent
        }
    }

    private static func projectDirectories(from cwd: URL, root: URL?) -> [URL] {
        guard let root else { return [cwd] }
        var out: [URL] = []
        var here = cwd
        while here.path.count >= root.path.count {
            out.append(here)
            if here.path == root.path { break }
            let parent = here.deletingLastPathComponent()
            if parent.path == here.path { break }
            here = parent
        }
        return out.reversed()
    }

    private static func skills(in directory: URL, source: ClaudeSkill.Source, prefix: String?,
                               fileManager fm: FileManager) -> [ClaudeSkill] {
        guard let entries = try? fm.contentsOfDirectory(at: directory,
                                                        includingPropertiesForKeys: [.isDirectoryKey],
                                                        options: [.skipsHiddenFiles]) else { return [] }
        return entries.compactMap { entry in
            guard (try? entry.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true else {
                return nil
            }
            return skill(at: entry.appendingPathComponent("SKILL.md"), directoryName: entry.lastPathComponent,
                         source: source, prefix: prefix, fileManager: fm)
        }
    }

    private static func skill(at file: URL, directoryName: String, source: ClaudeSkill.Source,
                              prefix: String?, fileManager fm: FileManager) -> ClaudeSkill? {
        guard fm.fileExists(atPath: file.path), let text = try? String(contentsOf: file, encoding: .utf8) else {
            return nil
        }
        let metadata = metadata(in: text)
        guard metadata.userInvocable else { return nil }
        let name = prefix == nil ? directoryName : (metadata.name ?? directoryName)
        guard validName(name) else { return nil }
        let command = prefix.map { "\($0):\(name)" } ?? name
        return ClaudeSkill(command: command, description: metadata.description, source: source)
    }

    // MARK: - Settings and plugins

    private struct Settings {
        var skillOverrides: [String: String] = [:]
        var enabledPlugins: [String: Bool] = [:]
    }

    private static func settingsFor(work: URL, root: URL?, home: URL,
                                    fileManager fm: FileManager) -> Settings {
        var out = Settings()
        var files = [home.appendingPathComponent(".claude/settings.json")]
        let project = root ?? work
        files += [project.appendingPathComponent(".claude/settings.json"),
                  project.appendingPathComponent(".claude/settings.local.json")]
        for file in files {
            guard let data = try? Data(contentsOf: file),
                  let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
            else { continue }
            if let values = json["skillOverrides"] as? [String: String] {
                out.skillOverrides.merge(values) { _, new in new }
            }
            if let values = json["enabledPlugins"] as? [String: Bool] {
                out.enabledPlugins.merge(values) { _, new in new }
            }
        }
        return out
    }

    private static func pluginSkills(home: URL, enabled: [String: Bool],
                                     fileManager fm: FileManager) -> [ClaudeSkill] {
        let registry = home.appendingPathComponent(".claude/plugins/installed_plugins.json")
        guard let data = try? Data(contentsOf: registry),
              let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let plugins = json["plugins"] as? [String: Any] else { return [] }

        var out: [ClaudeSkill] = []
        for (identity, raw) in plugins {
            guard enabled[identity] != false,
                  let prefix = identity.split(separator: "@", maxSplits: 1).first.map(String.init),
                  validPluginPrefix(prefix) else { continue }
            let installs = raw as? [[String: Any]] ?? []
            for install in installs {
                let scope = install["scope"] as? String ?? "user"
                // Project-scoped installs are meaningful only when this project's settings turn
                // them on. The registry does not carry a project path, so including every one
                // would leak skills installed for an unrelated repository into this menu.
                guard scope == "user" || enabled[identity] == true,
                      let path = install["installPath"] as? String else { continue }
                let root = URL(fileURLWithPath: path)
                out += skills(in: root.appendingPathComponent("skills"), source: .plugin,
                              prefix: prefix, fileManager: fm)
                // A plugin is also allowed to be one skill at its root. There is no directory to
                // name it, so its frontmatter `name` is required; an empty fallback is rejected.
                if let skill = skill(at: root.appendingPathComponent("SKILL.md"), directoryName: "",
                                     source: .plugin, prefix: prefix, fileManager: fm) {
                    out.append(skill)
                }
            }
        }
        return out
    }

    // MARK: - Frontmatter

    private static func metadata(in text: String) -> Metadata {
        let lines = text.replacingOccurrences(of: "\r\n", with: "\n")
            .split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        // No frontmatter means no safe menu metadata. In particular, do not turn the first line
        // of a skill's instructions into text that the remote API is allowed to expose.
        guard lines.first?.trimmingCharacters(in: .whitespaces) == "---" else { return Metadata() }

        var values: [String: String] = [:]
        var i = 1
        while i < lines.count {
            let line = lines[i]
            if line.trimmingCharacters(in: .whitespaces) == "---" { break }
            guard let colon = line.firstIndex(of: ":") else { i += 1; continue }
            let key = line[..<colon].trimmingCharacters(in: .whitespaces)
            var value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
            if value == ">" || value == "|" {
                var block: [String] = []
                i += 1
                while i < lines.count {
                    let next = lines[i]
                    if next.trimmingCharacters(in: .whitespaces) == "---" { i -= 1; break }
                    if !next.isEmpty, next.first?.isWhitespace != true { i -= 1; break }
                    block.append(next.trimmingCharacters(in: .whitespaces))
                    i += 1
                }
                value = block.joined(separator: value == ">" ? " " : "\n")
            }
            values[key] = unquoted(value)
            i += 1
        }

        var out = Metadata()
        out.name = values["name"]
        out.userInvocable = values["user-invocable"]?.lowercased() != "false"
        let described = values["description"] ?? ""
        out.description = clean(described)
        return out
    }

    private static func unquoted(_ value: String) -> String {
        guard value.count >= 2 else { return value }
        if (value.first == "\"" && value.last == "\"") || (value.first == "'" && value.last == "'") {
            return String(value.dropFirst().dropLast())
        }
        return value
    }

    private static func clean(_ text: String) -> String {
        let oneLine = text.unicodeScalars.map { scalar -> String in
            CharacterSet.controlCharacters.contains(scalar) || CharacterSet.newlines.contains(scalar)
                ? " " : String(scalar)
        }.joined().split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
        return String(oneLine.prefix(240))
    }

    private static func validName(_ name: String) -> Bool {
        guard (1...64).contains(name.count) else { return false }
        return name.allSatisfy { $0.isLowercase || $0.isNumber || $0 == "-" }
    }

    private static func validPluginPrefix(_ name: String) -> Bool {
        !name.isEmpty && name.allSatisfy { $0.isLetter || $0.isNumber || "-_.".contains($0) }
    }
}
