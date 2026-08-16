import AppKit

/// Reading a Claude Code session from the transcript it keeps on disk.
///
/// The output pane started out scraping the terminal, which shows you a picture of a screen:
/// message boundaries have to be inferred from indentation, and half of what is visible is
/// the input box you already have a better version of. Claude Code writes the session to
/// `~/.claude/projects/<project>/<session>.jsonl` as it goes, and that has the structure the
/// screen only implies — who spoke, what they said, which tools ran.
///
/// **This format is not documented and can change.** Everything here treats a missing or
/// unexpected field as "skip this record", never as a reason to fail, and the pane falls back
/// to the terminal capture when no transcript can be found.
enum Transcript {

    struct Entry {
        enum Kind {
            case user
            case assistant
            case tool          // a tool being called
            case toolResult    // what it returned
        }
        var kind: Kind
        var text: String
        var tool: String?
        var time: Date?
    }

    // MARK: - Finding the file

    /// Where Claude Code keeps a project's sessions: the working directory with every
    /// separator turned into a dash.
    static func projectDirectory(forCwd cwd: String) -> URL {
        let slug = cwd.replacingOccurrences(of: "/", with: "-")
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/projects/\(slug)", isDirectory: true)
    }

    /// The last `aiTitle` a transcript recorded. Claude Code puts the same string in the
    /// terminal's tab title, which is the only link between a session on screen and its file
    /// — no record carries a tty or a window id.
    static func title(ofTranscript url: URL, tailBytes: Int = 512_000) -> String? {
        guard let text = tail(of: url, bytes: tailBytes) else { return nil }
        var found: String?
        for line in text.split(separator: "\n") where line.contains("\"aiTitle\"") {
            guard let data = line.data(using: .utf8),
                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let t = obj["aiTitle"] as? String, !t.isEmpty else { continue }
            found = t
        }
        return found
    }

    /// Match a session on screen to its transcript: narrow by project, then by title, then
    /// fall back to whichever was written most recently.
    static func locate(cwd: String, tabTitle: String) -> URL? {
        let dir = projectDirectory(forCwd: cwd)
        let fm = FileManager.default
        guard let names = try? fm.contentsOfDirectory(atPath: dir.path) else { return nil }
        let files = names.filter { $0.hasSuffix(".jsonl") }
            .map { dir.appendingPathComponent($0) }
            .sorted { modified($0) > modified($1) }
        guard !files.isEmpty else { return nil }

        let wanted = cleanTitle(tabTitle)
        if !wanted.isEmpty {
            // Only the recent ones: an old session can share a title with a live one, and
            // reading every transcript in a busy project is not free.
            for file in files.prefix(12) where cleanTitle(title(ofTranscript: file) ?? "") == wanted {
                return file
            }
        }
        return files.first
    }

    /// Claude Code prefixes the tab title with a status glyph and iTerm appends the job name.
    /// Neither is part of the title it recorded.
    static func cleanTitle(_ raw: String) -> String {
        var s = raw.trimmingCharacters(in: .whitespaces)
        while let first = s.first, !(first.isLetter || first.isNumber) {
            s.removeFirst()
            s = s.trimmingCharacters(in: .whitespaces)
        }
        if s.hasSuffix(")"), let open = s.lastIndex(of: "("), open > s.startIndex {
            let before = s.index(before: open)
            if s[before] == " " { s = String(s[s.startIndex..<before]) }
        }
        return s.trimmingCharacters(in: .whitespaces)
    }

    private static func modified(_ url: URL) -> Date {
        (try? FileManager.default.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date
            ?? .distantPast
    }

    /// Transcripts run to tens of megabytes. Only the tail is ever wanted, and reading the
    /// whole file every second to show twenty messages would be absurd.
    static func tail(of url: URL, bytes: Int) -> String? {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? handle.close() }
        let size = (try? handle.seekToEnd()) ?? 0
        let start = size > UInt64(bytes) ? size - UInt64(bytes) : 0
        try? handle.seek(toOffset: start)
        guard let data = try? handle.readToEnd() else { return nil }
        var text = String(decoding: data, as: UTF8.self)
        // The first line is almost certainly cut in half by the seek.
        if start > 0, let nl = text.firstIndex(of: "\n") { text = String(text[text.index(after: nl)...]) }
        return text
    }

    static func signature(of url: URL) -> String {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        let size = (attrs?[.size] as? Int) ?? 0
        let date = (attrs?[.modificationDate] as? Date)?.timeIntervalSince1970 ?? 0
        return "\(size)-\(Int(date))"
    }

    // MARK: - Parsing

    private static let iso: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    /// Turn JSONL into entries. Anything unrecognised is skipped rather than guessed at.
    static func parse(_ jsonl: String, limit: Int = 400) -> [Entry] {
        var entries: [Entry] = []

        for line in jsonl.split(separator: "\n") {
            guard let data = line.data(using: .utf8),
                  let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else { continue }

            let type = row["type"] as? String
            guard type == "user" || type == "assistant" else { continue }
            // Sidechains are subagents talking among themselves, and meta records are
            // bookkeeping. Neither is the conversation you opened the pane to read.
            if row["isSidechain"] as? Bool == true { continue }
            if row["isMeta"] as? Bool == true { continue }

            let time = (row["timestamp"] as? String).flatMap { iso.date(from: $0) }
            guard let message = row["message"] as? [String: Any] else { continue }

            var blocks: [[String: Any]] = []
            if let list = message["content"] as? [[String: Any]] {
                blocks = list
            } else if let text = message["content"] as? String {
                blocks = [["type": "text", "text": text]]
            }

            for block in blocks {
                switch block["type"] as? String {
                case "text":
                    let text = (block["text"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !text.isEmpty else { continue }
                    entries.append(Entry(kind: type == "user" ? .user : .assistant,
                                         text: text, tool: nil, time: time))
                case "tool_use":
                    let name = block["name"] as? String ?? "tool"
                    entries.append(Entry(kind: .tool,
                                         text: summarise(input: block["input"]),
                                         tool: name, time: time))
                case "tool_result":
                    let text = firstLine(of: block["content"])
                    guard !text.isEmpty else { continue }
                    entries.append(Entry(kind: .toolResult, text: text, tool: nil, time: time))
                default:
                    continue   // thinking blocks, images, anything added later
                }
            }
        }
        return Array(entries.suffix(limit))
    }

    /// One line describing what a tool was asked to do. The fields are tried in the order a
    /// person would read them, so a Bash call shows its command rather than its description.
    static func summarise(input: Any?) -> String {
        guard let dict = input as? [String: Any] else { return "" }
        for key in ["command", "file_path", "path", "pattern", "url", "query", "prompt", "description"] {
            if let value = dict[key] as? String, !value.isEmpty {
                return firstLine(of: value)
            }
        }
        return ""
    }

    private static func firstLine(of content: Any?) -> String {
        var text = ""
        if let s = content as? String {
            text = s
        } else if let list = content as? [[String: Any]] {
            text = list.compactMap { $0["text"] as? String }.joined(separator: " ")
        }
        text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let line = text.split(separator: "\n").first else { return "" }
        return String(line)
    }
}

// MARK: - Rendering

extension Transcript {

    /// Lay the conversation out as prose rather than as a picture of a terminal.
    ///
    /// This is the whole reason for reading the file instead of the screen: with real message
    /// boundaries the type can do the work that colour was doing badly. Speakers get a label,
    /// bodies get a proportional face and room to breathe, and the machinery — tool calls and
    /// their results — recedes into monospace at the edge of the page.
    /// `expanded` holds the fold keys the reader has opened. Everything else that is a run of
    /// tool calls comes back as one line, because the machinery is what makes the pane
    /// unreadable: a single answer can sit under thirty lines of paths and shell.
    static func render(_ entries: [Entry], size: CGFloat, mono: NSFont,
                       expanded: Set<String> = []) -> NSAttributedString {
        let body = NSFont.systemFont(ofSize: size + 1)
        let header = NSFont.systemFont(ofSize: max(8.5, size - 1.5), weight: .semibold)
        let toolFont = NSFont(descriptor: mono.fontDescriptor, size: max(8.5, size - 0.5)) ?? mono

        let headerStyle = NSMutableParagraphStyle()
        headerStyle.paragraphSpacingBefore = 18
        headerStyle.paragraphSpacing = 5

        let toolStyle = NSMutableParagraphStyle()
        toolStyle.firstLineHeadIndent = 14
        toolStyle.headIndent = 30
        toolStyle.paragraphSpacing = 3
        toolStyle.paragraphSpacingBefore = 6
        toolStyle.lineBreakMode = .byTruncatingTail

        let resultStyle = NSMutableParagraphStyle()
        resultStyle.firstLineHeadIndent = 30
        resultStyle.headIndent = 30
        resultStyle.paragraphSpacing = 3
        resultStyle.lineBreakMode = .byTruncatingTail

        let clock = DateFormatter()
        clock.dateFormat = "HH:mm"

        let out = NSMutableAttributedString()

        func add(_ string: String, _ attrs: [NSAttributedString.Key: Any]) {
            out.append(NSAttributedString(string: string, attributes: attrs))
        }

        let foldStyle = NSMutableParagraphStyle()
        foldStyle.firstLineHeadIndent = 14
        foldStyle.headIndent = 30
        foldStyle.paragraphSpacing = 3
        foldStyle.paragraphSpacingBefore = 6

        func renderTool(_ entry: Entry) {
            // A dot rather than a bullet: this is a thing that happened, not an item in
            // a list, and the eye should be able to skip the whole column.
            add("⏺ ", [.font: toolFont, .foregroundColor: Style.accent,
                       .paragraphStyle: toolStyle])
            add(entry.tool ?? "tool", [
                .font: NSFont(descriptor: toolFont.fontDescriptor.withSymbolicTraits(.bold),
                              size: toolFont.pointSize) ?? toolFont,
                .foregroundColor: NSColor.secondaryLabelColor,
                .paragraphStyle: toolStyle,
            ])
            if !entry.text.isEmpty {
                add("  " + entry.text, [.font: toolFont,
                                        .foregroundColor: NSColor.tertiaryLabelColor,
                                        .paragraphStyle: toolStyle])
            }
            add("\n", [.font: toolFont, .paragraphStyle: toolStyle])
        }

        func renderResult(_ entry: Entry) {
            // Results are usually long and usually unread; one line is enough to know it
            // came back, and the pane is for the conversation.
            add("→ " + entry.text + "\n", [
                .font: toolFont,
                .foregroundColor: NSColor.tertiaryLabelColor,
                .paragraphStyle: resultStyle,
            ])
        }

        var i = 0
        while i < entries.count {
            let entry = entries[i]
            switch entry.kind {
            case .user, .assistant:
                let isUser = entry.kind == .user
                var label = isUser ? "YOU" : "CLAUDE"
                if let t = entry.time { label += "   \(clock.string(from: t))" }
                add(label + "\n", [
                    .font: header,
                    .foregroundColor: isUser ? Style.accent : NSColor.secondaryLabelColor,
                    .kern: 1.1,
                    .paragraphStyle: headerStyle,
                ])
                // No trailing newline here: every Markdown block ends with one already, and
                // the next entry's paragraphSpacingBefore is what sets the distance.
                out.append(prose(entry.text, body: body, mono: mono))
                i += 1

            case .tool, .toolResult:
                var run: [Entry] = []
                while i < entries.count, entries[i].kind == .tool || entries[i].kind == .toolResult {
                    run.append(entries[i]); i += 1
                }
                let names = run.compactMap { $0.kind == .tool ? ($0.tool ?? "tool") : nil }
                let key = foldKey(run)
                // The run still going is the one worth watching, so the tail never folds —
                // folding it would hide exactly the part that is changing.
                let isTail = i >= entries.count
                if isTail || names.count < 2 || expanded.contains(key) {
                    for e in run { e.kind == .tool ? renderTool(e) : renderResult(e) }
                } else {
                    add("⏵ ", [.font: toolFont, .foregroundColor: Style.accent,
                               .paragraphStyle: foldStyle, .link: "clawdline://fold/" + key])
                    add(L.t.foldedTools(names.count), [
                        .font: toolFont,
                        .foregroundColor: NSColor.secondaryLabelColor,
                        .paragraphStyle: foldStyle,
                        .link: "clawdline://fold/" + key,
                    ])
                    add("  " + distinct(names).joined(separator: " · ") + "\n", [
                        .font: toolFont,
                        .foregroundColor: NSColor.tertiaryLabelColor,
                        .paragraphStyle: foldStyle,
                        .link: "clawdline://fold/" + key,
                    ])
                }
            }
        }
        return out
    }

    /// Identifies a folded run so the reader's choice to open it survives a refresh.
    ///
    /// Content-derived rather than positional: the pane re-renders from the tail of a file that
    /// is still being written, so an index would slide under the reader and open a different run
    /// than the one they clicked. FNV-1a rather than `hashValue`, which is seeded per process.
    static func foldKey(_ run: [Entry]) -> String {
        var hash: UInt64 = 0xcbf29ce484222325
        for entry in run {
            for byte in Array(((entry.tool ?? "") + "\u{1}" + entry.text).utf8) {
                hash = (hash ^ UInt64(byte)) &* 0x100000001b3
            }
        }
        return String(hash, radix: 36)
    }

    /// Tool names in the order they first ran, without repeats — five greps read as one thing.
    static func distinct(_ names: [String]) -> [String] {
        var seen = Set<String>()
        return names.filter { seen.insert($0).inserted }
    }

    /// Body text goes through the Markdown renderer, because what Claude Code writes is
    /// Markdown — headings, lists, tables, emphasis — and showing it raw means showing
    /// punctuation where structure was meant.
    private static func prose(_ text: String, body: NSFont, mono: NSFont) -> NSAttributedString {
        Markdown.render(text, theme: Markdown.Theme(
            body: body,
            mono: mono,
            text: .labelColor,
            dim: .secondaryLabelColor,
            accent: Style.accent,
            code: Style.code,
            codeBackground: NSColor(white: 0, alpha: 0.20),
            ruleColor: .tertiaryLabelColor))
    }

}
