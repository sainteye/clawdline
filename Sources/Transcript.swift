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
///
/// Codex keeps a record of its own, in a different place and a different shape — that is
/// ``Codex``, and ``record(of:)`` below is the one door both go through, so that everything
/// downstream reads one kind of ``Entry`` and never asks whose conversation it is.
enum Transcript {

    // MARK: - Whichever assistant

    /// The file a session is writing, and who is writing it.
    ///
    /// The two callers of this — the pane and the phone — had the same four lines of "find the
    /// working directory, find the file" copied between them, and adding a second assistant
    /// would have made it the same eight lines twice. It is one function now, and the only place
    /// that knows there is more than one kind of record.
    static func record(of session: TargetSession) -> (url: URL, assistant: Assistant)? {
        guard let assistant = session.assistant,
              let cwd = Targets.workingDirectory(of: session) else { return nil }
        let started = Targets.processStart(of: session)
        switch assistant {
        case .claude:
            // The session id, when a hook has told us one, skips all the guessing underneath.
            guard let url = locate(cwd: cwd, tabTitle: session.name, startedAt: started,
                                   sessionID: HookBridge.note(for: session)?.session)
            else { return nil }
            return (url, .claude)
        case .codex:
            // No hooks and no tab title to match on: Codex records neither. What it does do is
            // hold its rollout open, so the pid is worth more here than either — see
            // ``Codex/locate(cwd:startedAt:pid:days:)``, where the directory and the clock are
            // the fallback for when there is no process to ask.
            guard let url = Codex.locate(cwd: cwd, startedAt: started,
                                         pid: Targets.pid(of: session)) else { return nil }
            return (url, .codex)
        }
    }

    /// A record as entries, whoever wrote it.
    ///
    /// **`sidechains` is what tells the two kinds of Claude file apart.** A session's own
    /// transcript carries its agents' turns inline, marked as sidechains, and those are not what
    /// anybody opened the session to read — so they are dropped. An *agent's* transcript is
    /// nothing but sidechain: every row in it is marked, because from the session's point of
    /// view that is exactly what it is. Reading one with the session's rule leaves a pane
    /// reporting that a busy agent has written nothing at all.
    static func parse(_ jsonl: String, assistant: Assistant, limit: Int = 400,
                      sidechains: Bool = false) -> [Entry] {
        switch assistant {
        case .claude: return parse(jsonl, limit: limit, sidechains: sidechains)
        case .codex:  return Codex.parse(jsonl, limit: limit)
        }
    }

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

    /// Where Claude Code keeps a project's sessions: the working directory with **every
    /// character that is not a letter or a digit** turned into a dash — not just the separators.
    ///
    /// This replaced `/` alone for a long time, which is right for a path made of plain words and
    /// wrong the moment one has a space, a dot or an underscore in it. Nothing reported it: a
    /// missing transcript is an ordinary state, so the lookup simply came back empty and the
    /// session looked like one Claude Code had never written anything about.
    ///
    /// `StartPoints.slug(of:)` is the same rule, checked against the folders actually on disk.
    /// One implementation, because two would agree for exactly as long as nobody edited either.
    static func projectDirectory(forCwd cwd: String) -> URL {
        let slug = StartPoints.slug(of: cwd)
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
    static func parse(_ jsonl: String, limit: Int = 400, sidechains: Bool = false) -> [Entry] {
        var newestFirst: [Entry] = []

        forEachLineFromEnd(jsonl) { line in
            // Each row is appended newest-block-first so the whole buffer stays in one order,
            // and is turned back the right way round once.
            for entry in entries(inRow: line, sidechains: sidechains).reversed() {
                newestFirst.append(entry)
            }
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
    ///
    /// Not private, because ``Codex`` reads a different program's JSONL with exactly the same
    /// problem and there is no version of this worth having twice.
    static func forEachLineFromEnd(_ text: String, _ body: (Substring) -> Bool) {
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
    private static func entries(inRow line: Substring, sidechains: Bool = false) -> [Entry] {
        guard let data = line.data(using: .utf8),
              let row = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return [] }

        let type = row["type"] as? String
        guard type == "user" || type == "assistant" else { return [] }
        // Sidechains are subagents talking among themselves, and meta records are
        // bookkeeping. Neither is the conversation you opened the pane to read — unless the
        // conversation you opened *is* an agent's, in which case sidechain is all there is.
        if !sidechains, row["isSidechain"] as? Bool == true { return [] }
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
                let raw = (block["text"] as? String ?? "")
                var text = raw
                // A slash command is the one piece of tagged machinery somebody did type, so it
                // goes back in as the line they typed — see `slashCommand(in:)`.
                let typed = type == "user" ? slashCommand(in: raw) : nil
                if type == "user" { text = withoutMachineBlocks(text) }
                text = text.trimmingCharacters(in: .whitespacesAndNewlines)
                if let typed { text = text.isEmpty ? typed : typed + "\n" + text }
                if !text.isEmpty {
                    out.append(Entry(kind: type == "user" ? .user : .assistant,
                                     text: text, tool: nil, time: time))
                }
                // And what it printed, filed as a result: a command and its answer are two
                // halves of one exchange, and a call followed by what it returned is a shape the
                // pane already draws.
                if type == "user", let printed = commandOutput(in: raw) {
                    out.append(Entry(kind: .toolResult, text: printed, tool: nil, time: time))
                }
            case "tool_use":
                let name = block["name"] as? String ?? "tool"
                // A question is the one tool call whose arguments are the whole point of it, so
                // it carries them rather than a line describing them. See `askPayload`.
                let text = (name == askTool ? askPayload(input: block["input"]) : nil)
                    ?? summarise(input: block["input"])
                out.append(Entry(kind: .tool, text: text, tool: name, time: time))
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
    ///
    /// The command tags are the one exception to "nobody typed it", and they are handled either
    /// side of this rather than inside it: what comes out here is a command's *expansion*, and
    /// ``slashCommand(in:)`` puts the line that caused it back. This function stays a plain
    /// remover so that everything else calling it gets what it asks for.
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

    // MARK: - Slash commands

    /// The slash command a turn **is**, as one line: `/model fable`.
    ///
    /// ``withoutMachineBlocks`` takes `<command-name>`, `<command-message>` and `<command-args>`
    /// out, and for the *expansion* of a command — the paragraphs of instructions a skill unrolls
    /// into the user's turn — that is right. It is wrong for the command itself. What is left of
    /// a `/model fable` afterwards is nothing at all, so the entry is dropped, so a session whose
    /// only traffic so far has been slash commands reads in the pane as a session that has said
    /// nothing: the one place somebody looks to find out what has been happening is the one place
    /// it is not.
    ///
    /// So the three tags come back as the line somebody typed. `nil` for the turns that are not
    /// commands, which is nearly all of them.
    ///
    /// `<command-message>` is the picker's own label for the row and is not typed by anybody; it
    /// stays out.
    static func slashCommand(in text: String) -> String? {
        guard let name = inner(tag: "command-name", of: text)?
            .trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty else { return nil }
        let command = name.hasPrefix("/") ? name : "/" + name
        guard let args = inner(tag: "command-args", of: text)?
            .trimmingCharacters(in: .whitespacesAndNewlines), !args.isEmpty else { return command }
        return command + " " + args
    }

    /// What a slash command printed, with the terminal's colours taken out.
    ///
    /// This is the half of the exchange that says whether the command worked — `Set model to
    /// Opus 5` — and Claude Code records it as its own turn, tagged, on the user's side. It is
    /// filed as a tool result rather than as anybody's words: nobody said it, the machine
    /// answered it, and one line with the rest a tap away is what the pane does with an answer.
    ///
    /// A command that printed nothing leaves the tag empty, and an empty result is no result.
    static func commandOutput(in text: String) -> String? {
        var parts: [String] = []
        for tag in ["local-command-stdout", "local-command-stderr"] {
            guard let body = inner(tag: tag, of: text) else { continue }
            let plain = Ansi.plain(body).trimmingCharacters(in: .whitespacesAndNewlines)
            if !plain.isEmpty { parts.append(plain) }
        }
        return parts.isEmpty ? nil : parts.joined(separator: "\n")
    }

    /// What is inside the first `<tag>…</tag>`, or nil when there is no such tag. The inverse of
    /// ``removing(tag:from:)``, and truncated the same way: opened and never closed means the
    /// record was cut off mid-write, and everything after the opening tag is the block.
    private static func inner(tag: String, of text: String) -> String? {
        guard let start = text.range(of: "<\(tag)>") else { return nil }
        guard let end = text.range(of: "</\(tag)>", range: start.upperBound..<text.endIndex) else {
            return String(text[start.upperBound...])
        }
        return String(text[start.upperBound..<end.lowerBound])
    }

    // MARK: - The question tool

    /// The tool Claude Code stops on when it wants a decision rather than a file.
    static let askTool = "AskUserQuestion"

    /// What an ``askTool`` entry's text starts with, so a reader can tell data from a summary.
    ///
    /// Every other tool's `text` is one line describing what it was asked to do — a command, a
    /// path — because that is all a reader wants from a step that has already happened. **A
    /// question is not a step that has already happened.** Its arguments *are* the content: the
    /// sentence somebody has to answer and the options they may answer it with, and a one-line
    /// summary of that is a question you cannot answer.
    ///
    /// So this one entry carries its arguments, and the marker is what stops a renderer that has
    /// not been taught about it from drawing JSON at somebody. The character is `U+0001`, which
    /// no transcript contains and no keyboard produces.
    ///
    /// It rides in `text` because that is the field there is. The wire between the Mac and the
    /// web interface carries `role`, `text`, `tool` and a time and nothing else, and a second
    /// field would have to be added at both ends at once.
    static let askMarker = "\u{1}ask\u{1}"

    /// One question as it was asked. Several can arrive in one call.
    struct Question {
        var header: String
        var text: String
        /// Several answers are wanted, not one. The terminal draws these with checkboxes and
        /// does not close when one is pressed, which is a different thing to answer.
        var multiSelect: Bool
        var options: [Option]

        struct Option {
            var label: String
            var note: String
        }
    }

    /// The questions in an ``askTool`` call, ready to travel as an entry's text.
    ///
    /// `nil` for anything that is not recognisably a question, which sends the caller back to
    /// ``summarise(input:)`` — the shape is not documented and a tool that has changed underneath
    /// this should read as a plain tool call rather than as an empty box.
    static func askPayload(input: Any?) -> String? {
        guard let dict = input as? [String: Any],
              let asked = dict["questions"] as? [[String: Any]] else { return nil }

        var items: [[String: Any]] = []
        for question in asked {
            let text = clean(question["question"])
            var row: [String: Any] = ["q": text]
            let header = clean(question["header"])
            if !header.isEmpty { row["h"] = header }
            if question["multiSelect"] as? Bool == true { row["m"] = true }

            var options: [[String: Any]] = []
            for option in (question["options"] as? [[String: Any]] ?? []) {
                let label = clean(option["label"])
                guard !label.isEmpty else { continue }
                var out: [String: Any] = ["l": label]
                let note = clean(option["description"])
                if !note.isEmpty { out["d"] = note }
                options.append(out)
            }
            row["o"] = options
            guard !text.isEmpty || !options.isEmpty else { continue }
            items.append(row)
        }
        guard !items.isEmpty,
              let data = try? JSONSerialization.data(withJSONObject: items,
                                                     options: [.withoutEscapingSlashes]),
              let json = String(data: data, encoding: .utf8) else { return nil }
        return askMarker + json
    }

    /// The other end of ``askPayload(input:)``. `nil` when this is an ordinary tool call.
    static func askQuestions(in text: String) -> [Question]? {
        guard text.hasPrefix(askMarker) else { return nil }
        guard let data = String(text.dropFirst(askMarker.count)).data(using: .utf8),
              let rows = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]]
        else { return nil }

        let questions = rows.map { row in
            Question(header: row["h"] as? String ?? "",
                     text: row["q"] as? String ?? "",
                     multiSelect: row["m"] as? Bool ?? false,
                     options: (row["o"] as? [[String: Any]] ?? []).compactMap { option in
                         guard let label = option["l"] as? String, !label.isEmpty else { return nil }
                         return Question.Option(label: label, note: option["d"] as? String ?? "")
                     })
        }
        return questions.isEmpty ? nil : questions
    }

    private static func clean(_ value: Any?) -> String {
        (value as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
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
        // What a command printed, not how it wanted a terminal to colour it.
        //
        // A tool result is the raw output of something that thought it was writing to a terminal,
        // so it can arrive full of escape sequences. They are invisible in a terminal and they are
        // not invisible anywhere else — on a phone one of these turned into four hundred characters
        // of `[38;2;47;107;94m` with no spaces in it, which pushed the whole page sideways and left
        // a screen of black with one line of noise floating in it.
        text = Ansi.plain(text).trimmingCharacters(in: .whitespacesAndNewlines)
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
            if let questions = askQuestions(in: entry.text) {
                // A question is the one tool call worth reading in full, so it gets the prose
                // treatment the conversation gets rather than a truncated line of monospace.
                add("\n", [.font: toolFont, .paragraphStyle: toolStyle])
                block.append(prose(markdown(of: questions), body: body, mono: mono))
                return
            }
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
                // A question never folds. Folding is for machinery nobody reads, and this is the
                // one tool call that is a sentence addressed to the reader.
                let asks = run.contains { $0.tool == askTool && askQuestions(in: $0.text) != nil }
                if isTail || asks || names.count < 2 || expanded.contains(key) {
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

    /// A question laid out as the Markdown the prose renderer already knows how to draw.
    ///
    /// **Data only, no sentences.** Everything in here came out of the transcript and is already
    /// in whatever language the conversation is in; a line of this file's own English explaining
    /// how to answer would be the one untranslated thing on the page. What that needs saying,
    /// the surface drawing it says in its own words.
    ///
    /// The numbers are not decoration: they are what the terminal's own picker answers to, so an
    /// option's number here is the key somebody presses there.
    static func markdown(of questions: [Question]) -> String {
        var lines: [String] = []
        for question in questions {
            if !lines.isEmpty { lines.append("") }
            if !question.header.isEmpty { lines.append("#### " + question.header) }
            if !question.text.isEmpty { lines.append(question.text) }
            if !question.options.isEmpty { lines.append("") }
            for (index, option) in question.options.enumerated() {
                var line = "\(index + 1). **\(option.label)**"
                if !option.note.isEmpty { line += " — " + option.note }
                lines.append(line)
            }
        }
        return lines.joined(separator: "\n")
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
