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
    /// Remembered against the file's size and mtime, so an unchanged transcript is read once.
    ///
    /// `locate` asks this of up to twelve files, and each answer costs half a megabyte off the
    /// disk — six megabytes of reading to pick one file, on the path that runs every time the
    /// session list moves. Of those twelve, at most one or two have been written to since the
    /// last look; the rest are finished conversations whose answer cannot have changed. Keyed on
    /// the signature rather than on a clock, so this is a saved read and never a stale answer.
    private static let titleLock = NSLock()
    private static var titleCache: [String: (signature: String, title: String?)] = [:]

    static func title(ofTranscript url: URL, tailBytes: Int = 512_000) -> String? {
        let sig = signature(of: url)
        titleLock.lock()
        if let hit = titleCache[url.path], hit.signature == sig {
            defer { titleLock.unlock() }
            return hit.title
        }
        titleLock.unlock()

        let found = readTitle(ofTranscript: url, tailBytes: tailBytes)
        titleLock.lock()
        titleCache[url.path] = (sig, found)
        titleLock.unlock()
        return found
    }

    private static func readTitle(ofTranscript url: URL, tailBytes: Int) -> String? {
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
    ///
    /// `sessionID` skips all of that. Claude Code names a transcript after the session it belongs
    /// to, so when a hook has told us the id there is nothing left to work out — and everything
    /// below is working out what a name would have said. Only ever an improvement: with no hooks
    /// installed it is nil, and the matching underneath is what it always was.
    static func locate(cwd: String, tabTitle: String, startedAt: Date? = nil,
                       sessionID: String? = nil) -> URL? {
        let dir = projectDirectory(forCwd: cwd)
        let fm = FileManager.default

        if let sessionID, !sessionID.isEmpty {
            let named = dir.appendingPathComponent("\(sessionID).jsonl")
            if fm.fileExists(atPath: named.path) { return named }
            // Falls through rather than returning nothing. A session can be resumed into a new
            // file, and a project directory is named after the working directory — which moves
            // if somebody renames a folder. Guessing again beats an empty pane.
        }
        guard let names = try? fm.contentsOfDirectory(atPath: dir.path) else { return nil }
        // Stat once per file and sort the answers. Asking inside the comparator looked tidier and
        // meant a stat per *comparison* — a project with fifty-six transcripts in it, which is
        // what a year of work looks like, spent several hundred of them to order fifty-six names.
        let files = names.filter { $0.hasSuffix(".jsonl") }
            .map { (url: dir.appendingPathComponent($0), at: modified(dir.appendingPathComponent($0))) }
            .sorted { $0.at > $1.at }
            .map(\.url)
        guard !files.isEmpty else { return nil }

        let wanted = cleanTitle(tabTitle)
        if !wanted.isEmpty {
            // Only the recent ones: an old session can share a title with a live one, and
            // reading every transcript in a busy project is not free.
            for file in files.prefix(12) where cleanTitle(title(ofTranscript: file) ?? "") == wanted {
                return file
            }
        }
        // No title match. Falling back to "whichever was written most recently" is how a
        // brand-new tab — which has no title yet — ended up showing somebody else's
        // conversation, confidently and with their project's name on it.
        //
        // A session's own transcript is created when the session starts, so the process start
        // time tells them apart. Slack either way for the gap between the two.
        if let startedAt {
            let mine = files.filter { abs(created($0).timeIntervalSince(startedAt)) < 120
                                        || created($0) > startedAt }
            // Nothing of its own yet is the honest answer for a tab that has not spoken.
            return mine.min { abs(created($0).timeIntervalSince(startedAt))
                                < abs(created($1).timeIntervalSince(startedAt)) }
        }
        return files.first
    }

    /// When a transcript was created — the session's own birthday, unlike its modification date.
    static func created(_ url: URL) -> Date {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        return (attrs?[.creationDate] as? Date) ?? .distantPast
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

    // MARK: - Laid-out transcripts, kept

    /// The last few rendered transcripts, by the signature that already decides whether a repaint
    /// is needed. That key covers everything the layout depends on — the file's size and mtime,
    /// the type size, the reading order, which runs of tool calls are open — so a hit is the same
    /// string the renderer would have built, not a guess that it would be.
    ///
    /// This is what makes ↑ and ↓ in the session list free. Moving away and back used to re-read,
    /// re-parse and re-lay-out a conversation that had not changed a byte in the meantime, and
    /// the pane sat on the previous session while it did.
    ///
    /// Twelve, which is more sessions than the list can show — measured at about half a megabyte
    /// each, so holding all of them costs less than being wrong about which two you were moving
    /// between. They are let go of when the panel goes down.
    private static let renderLock = NSLock()
    private static var renders: [(key: String, text: NSAttributedString)] = []
    private static let renderLimit = 12

    static func cachedRender(for key: String) -> NSAttributedString? {
        renderLock.lock()
        defer { renderLock.unlock() }
        guard let i = renders.firstIndex(where: { $0.key == key }) else { return nil }
        // Touched, so that going back and forth between two sessions cannot evict either.
        let hit = renders.remove(at: i)
        renders.append(hit)
        return hit.text
    }

    static func remember(_ text: NSAttributedString, for key: String) {
        renderLock.lock()
        defer { renderLock.unlock() }
        renders.removeAll { $0.key == key }
        renders.append((key, text))
        if renders.count > renderLimit { renders.removeFirst(renders.count - renderLimit) }
    }

    /// Let go of them when the panel does. Six laid-out conversations is tens of megabytes, and
    /// this is an application that spends most of the day being a character in the menu bar —
    /// holding somebody's afternoon of reading in memory while it does so is not the deal. The
    /// first look after a summon pays for itself again; moving around inside one does not.
    static func forgetRenders() {
        renderLock.lock()
        renders.removeAll()
        renderLock.unlock()
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
    ///
    /// **Read from the end.** Only the last `limit` entries are ever returned, and a session's
    /// transcript runs to tens of megabytes — the first version of this JSON-parsed every line in
    /// the tail and then threw all but four hundred of the results away. Measured on a real 29 MB
    /// transcript that was 268 ms, on the path that runs every time you press ↓ in the session
    /// list, so moving between sessions had a third of a second of nothing in it.
    ///
    /// Stopping early only works from the newest end, which is where the reader is looking
    /// anyway. Lines that are skipped — sidechains, bookkeeping — still cost their own parse, so
    /// the worst case is what this used to cost every time and the ordinary case is a few
    /// hundred lines.
    static func parse(_ jsonl: String, limit: Int = 400) -> [Entry] {
        var newestFirst: [Entry] = []

        forEachLineFromEnd(jsonl) { line in
            // Each row is appended newest-block-first so the whole buffer stays in one order,
            // and is turned back the right way round once.
            for entry in entries(inRow: line).reversed() { newestFirst.append(entry) }
            return newestFirst.count < limit
        }
        return Array(newestFirst.reversed().suffix(limit))
    }

    /// Hand each line to `body`, newest first, stopping when it returns false.
    ///
    /// Not `split(separator:)`. That builds an array holding every line in the tail — eight
    /// megabytes of it, measured at 140 ms and by far the largest thing left on the path a
    /// session switch runs — in order to read the last few hundred. Walking back from the end
    /// touches only the bytes actually wanted, so the cost follows what is read rather than what
    /// is on disk.
    ///
    /// The scan is over the UTF-8 view because the separator is one ASCII byte, and a byte
    /// comparison does not care about grapheme breaking. Its indices are `String.Index`, so the
    /// slice handed over is a real `Substring` of the original with nothing copied.
    private static func forEachLineFromEnd(_ text: String, _ body: (Substring) -> Bool) {
        let utf8 = text.utf8
        var end = utf8.endIndex
        while end > utf8.startIndex {
            var start = end
            while start > utf8.startIndex, utf8[utf8.index(before: start)] != 0x0A {
                start = utf8.index(before: start)
            }
            if start < end, !body(text[start..<end]) { return }
            guard start > utf8.startIndex else { return }
            end = utf8.index(before: start)     // step over the newline itself
        }
    }

    /// The entries one JSONL row yields, in the order they were written. Empty for anything
    /// that is not a message worth reading.
    private static func entries(inRow line: Substring) -> [Entry] {
        guard let data = line.data(using: .utf8),
              let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return [] }

        let type = row["type"] as? String
        guard type == "user" || type == "assistant" else { return [] }
        // Sidechains are subagents talking among themselves, and meta records are
        // bookkeeping. Neither is the conversation you opened the pane to read.
        if row["isSidechain"] as? Bool == true { return [] }
        if row["isMeta"] as? Bool == true { return [] }

        let time = (row["timestamp"] as? String).flatMap { iso.date(from: $0) }
        guard let message = row["message"] as? [String: Any] else { return [] }

        var blocks: [[String: Any]] = []
        if let list = message["content"] as? [[String: Any]] {
            blocks = list
        } else if let text = message["content"] as? String {
            blocks = [["type": "text", "text": text]]
        }

        var out: [Entry] = []
        for block in blocks {
            switch block["type"] as? String {
            case "text":
                var text = (block["text"] as? String ?? "")
                if type == "user" { text = withoutMachineBlocks(text) }
                text = text.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !text.isEmpty else { continue }
                out.append(Entry(kind: type == "user" ? .user : .assistant,
                                 text: text, tool: nil, time: time))
            case "tool_use":
                let name = block["name"] as? String ?? "tool"
                out.append(Entry(kind: .tool,
                                 text: summarise(input: block["input"]),
                                 tool: name, time: time))
            case "tool_result":
                let text = firstLine(of: block["content"])
                guard !text.isEmpty else { continue }
                out.append(Entry(kind: .toolResult, text: text, tool: nil, time: time))
            default:
                continue   // thinking blocks, images, anything added later
            }
        }
        return out
    }

    /// One line describing what a tool was asked to do. The fields are tried in the order a
    /// person would read them, so a Bash call shows its command rather than its description.
    /// Take the machinery back out of a turn attributed to the person.
    ///
    /// Claude Code injects its own bookkeeping into the user's side of the conversation — a
    /// background task finishing, a reminder to itself, the expansion of a slash command — all
    /// wrapped in tags and all recorded as though somebody typed it. On screen that is fine,
    /// because it never appears there. In a transcript it is the loudest thing in the pane:
    /// twenty lines of XML with a file path in it, sitting under the word "you", above the one
    /// sentence you actually said.
    ///
    /// Removed rather than folded, and only from the user's turns. Nobody typed it, so there is
    /// no version of "show it anyway" that is honest about whose words they are — and an entry
    /// left empty afterwards is dropped by the caller, which is the right answer for a turn that
    /// was nothing but machinery.
    ///
    /// Unknown tags are left alone. This is a list of things observed to be injected, not a rule
    /// about angle brackets: somebody quoting XML at Claude has typed that, and it stays.
    static func withoutMachineBlocks(_ text: String) -> String {
        var out = text
        for tag in ["task-notification", "system-reminder", "local-command-stdout",
                    "local-command-stderr", "command-name", "command-message", "command-args"] {
            out = removing(tag: tag, from: out)
        }
        return out
    }

    /// Every `<tag>…</tag>`, non-greedy and across newlines. Written by hand rather than with a
    /// regular expression because the pane parses several megabytes of this on every switch, and
    /// a backtracking match over that is a pause somebody notices.
    private static func removing(tag: String, from text: String) -> String {
        let open = "<\(tag)>", close = "</\(tag)>"
        guard text.contains(open) else { return text }
        var out = ""
        var rest = Substring(text)
        while let start = rest.range(of: open) {
            out += rest[rest.startIndex..<start.lowerBound]
            guard let end = rest.range(of: close, range: start.upperBound..<rest.endIndex) else {
                // Opened and never closed — a truncated record. Everything after it is the block.
                return out
            }
            rest = rest[end.upperBound...]
        }
        return out + rest
    }

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
                       expanded: Set<String> = [], newestFirst: Bool = false) -> NSAttributedString {
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

        // Built one block at a time rather than into a single run, so reversing is a matter of
        // the order the blocks go out in. Reversing lines or entries instead would turn one
        // answer inside out; what wants flipping is the conversation, not what was said.
        var blocks: [NSMutableAttributedString] = []
        var block = NSMutableAttributedString()

        func add(_ string: String, _ attrs: [NSAttributedString.Key: Any]) {
            block.append(NSAttributedString(string: string, attributes: attrs))
        }

        /// Starts a new block. Every case below opens with this and the loop closes with it,
        /// which is the whole rule: one message or one run of tool calls per block.
        func endBlock() {
            if block.length > 0 { blocks.append(block) }
            block = NSMutableAttributedString()
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
                endBlock()
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
                block.append(prose(entry.text, body: body, mono: mono))
                i += 1

            case .tool, .toolResult:
                endBlock()
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
        endBlock()

        let out = NSMutableAttributedString()
        for b in (newestFirst ? blocks.reversed() : blocks) { out.append(b) }
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
