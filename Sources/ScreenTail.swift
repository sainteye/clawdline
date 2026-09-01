import Foundation

/// What a session has already said that its transcript file has not written down yet.
///
/// **The hole this fills.** Claude Code appends an assistant message to its `.jsonl` as soon as
/// the message is complete — a `tool_use` for a twenty-second command lands while the command is
/// still running, measured. `AskUserQuestion` is the exception: the whole turn that asks a
/// question — the thinking, *the prose explaining the choice*, and the call itself — is written
/// only once the person has answered. Observed on 2026-09-01: a session drew its analysis and its
/// picker at 13:28:38 and its transcript file still ended at 13:27:17 ten minutes later, with the
/// reader on a phone looking at a question and none of the reasoning behind it.
///
/// The question itself arrives by another road — a hook note, and the screen parse in
/// ``SessionState`` — which is exactly why the gap is visible rather than merely late: the card
/// is on time and the sentences that make it answerable are not. **A question you cannot see the
/// reasoning for is a question you cannot answer**, and "wait until you have answered it" is not
/// an order the reader can follow.
///
/// **Why one capture is not enough, and why that does not stop this.** iTerm2 exposes the visible
/// screen and no more — `text` and `contents` are the same sixty rows, and there is no scrollback
/// API (tmux has one; see `Tmux.capture(scrollback:)`). A single capture of a long answer
/// therefore starts in the middle of it. But ``Targets/reading(of:hookWaiting:)`` already takes
/// one capture per session per beat, and prose arrives by streaming over tens of seconds, so the
/// captures overlap. Overlapping captures can be reconciled back into the document they are a
/// window onto. Measured over sixty consecutive captures of a live session: every frame aligned
/// against its predecessor — no gaps — and no line any frame had shown was missing from the
/// reconstruction.
///
/// **Two things it will not do.** It will not guess across a gap: when a frame cannot be placed
/// against the document, the break is recorded rather than papered over, because prose spliced at
/// the wrong point reads as perfectly ordinary and is wrong. And it does not claim to be the
/// transcript. What it returns is a *reading of a screen* — hard-wrapped at the terminal's width
/// and reassembled by heuristic — offered while the real record is missing and replaced by the
/// real record the moment that arrives.
///
/// Transport-neutral and free of any request, like ``ReadingFreshness``: it is handed screens and
/// asked for words. Who wants them, and what a phone does with them, is not its business.
enum ScreenTail {

    // MARK: - What a screen is made of

    /// The composer's rule. Everything below the first one is Claude Code's own furniture — the
    /// input box, the status lines — and none of it is anybody's words.
    private static func isRule(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.count >= 40 else { return false }
        return trimmed.allSatisfy { $0 == "\u{2500}" || $0 == "\u{2501}" }
    }

    /// Rows that are redrawn in place several times a second. They are why a naive reconciliation
    /// fails: with them in, consecutive frames never match and every beat looks like new content.
    /// **They are the clock, not the conversation** — dropping them is what makes the rest of the
    /// screen append-only, which is the property the whole reconciliation rests on.
    private static func isLive(_ line: String) -> Bool {
        let markers = ["esc to interrupt", "ctrl+b to run in background", "Thinking…",
                       "tokens)", "Running ", "Waiting…"]
        if markers.contains(where: { line.contains($0) }) { return true }
        // "· 4s…" — an elapsed counter on an otherwise ordinary row.
        if line.range(of: "·[[:space:]]*[0-9]+[smh]…", options: .regularExpression) != nil {
            return true
        }
        // "…(3s · 3 lines)" — a running tool's own preview row, rewritten once a second with a
        // new elapsed time. Missing this shape put forty near-identical copies of one line in
        // front of a reader, which is what a redraw looks like to anything that only appends.
        return line.range(of: "\\([0-9]+(\\.[0-9]+)?[smh][^)]*\\)[[:space:]]*$",
                          options: .regularExpression) != nil
    }

    /// The same line with every number replaced, used **only** to decide where two captures
    /// overlap.
    ///
    /// A terminal is not an append-only log: Claude Code rewrites a running tool's row in place,
    /// and the only thing that changes is a counter. Compared literally, every rewrite looks like
    /// a line that has never been seen, so an appending reconciliation stacks up a copy per
    /// second. Compared with the numbers removed, the rewrite aligns with what it replaced and
    /// only genuinely new lines are appended. The document still keeps the original text — this
    /// is how alignment is decided, never what is stored.
    static func aligning(_ line: String) -> String {
        line.replacingOccurrences(of: "[0-9]+", with: "#", options: .regularExpression)
    }

    /// The live line's own glyphs, which iTerm2 hands over as the first character of a row.
    private static func isSpinner(_ line: String) -> Bool {
        guard let first = line.trimmingCharacters(in: .whitespaces).first else { return false }
        return "✻✳✢✽∗*⠴⠦⠇◐◑".contains(first)
    }

    /// The conversation part of one screen: no ANSI, no composer, no clock.
    static func region(of screen: String) -> [String] {
        var lines = Ansi.plain(screen)
            .components(separatedBy: "\n")
            .map { $0.replacingOccurrences(of: "[ \t]+$", with: "", options: .regularExpression) }
        if let rule = lines.firstIndex(where: isRule) { lines = Array(lines[..<rule]) }
        lines = lines.filter { !isLive($0) }
        while let last = lines.last,
              last.trimmingCharacters(in: .whitespaces).isEmpty || isSpinner(last)
                || last.trimmingCharacters(in: .whitespaces).hasPrefix("\u{23BF}") {
            lines.removeLast()
        }
        return lines
    }

    // MARK: - Putting the frames back together

    /// Recorded where a frame could not be placed. It never reaches a reader: prose is only ever
    /// taken from the run of lines *after* the last one of these, so a break truncates what this
    /// can offer instead of corrupting it.
    static let gapMarker = "\u{1}screen-gap\u{1}"

    /// How many lines back a new frame is allowed to align against. A screen is sixty rows and a
    /// beat is a second; anything older than this window is not the same view of the document.
    private static let alignmentWindow = 240

    /// The longest run of lines the two share, as (index in `a`, index in `b`, length). Small
    /// enough inputs — a 240-line window against a 60-line frame — that the straightforward
    /// dynamic program is the right one.
    private static func longestCommonRun(_ a: [String], _ b: [String]) -> (a: Int, b: Int, length: Int) {
        guard !a.isEmpty, !b.isEmpty else { return (0, 0, 0) }
        var previous = [Int](repeating: 0, count: b.count + 1)
        var current = [Int](repeating: 0, count: b.count + 1)
        var best = (a: 0, b: 0, length: 0)
        for i in 1...a.count {
            for j in 1...b.count {
                if a[i - 1] == b[j - 1] {
                    current[j] = previous[j - 1] + 1
                    if current[j] > best.length {
                        best = (a: i - current[j], b: j - current[j], length: current[j])
                    }
                } else {
                    current[j] = 0
                }
            }
            swap(&previous, &current)
            for j in 0...b.count { current[j] = 0 }
        }
        return best
    }

    /// Place one frame against the document and return the document it grows into.
    ///
    /// Append-only on purpose. The other reading — letting the frame overwrite the span it covers
    /// — mirrors the screen more exactly, including Claude Code folding a finished tool block
    /// away, and measured that way sixteen lines that had been on screen were lost. **Prose is
    /// never folded**, so for the one thing this is for, keeping what was shown costs nothing and
    /// loses nothing.
    static func reconcile(_ document: [String], with frame: [String]) -> [String] {
        guard !frame.isEmpty else { return document }
        guard !document.isEmpty else { return frame }
        let window = Array(document.suffix(alignmentWindow))
        let base = document.count - window.count
        // Literally first, because that is the reading that cannot be fooled. Only when the
        // captures share no run at all is the numbers-removed comparison tried, which is the
        // case where the only thing that changed was a counter.
        let literal = longestCommonRun(window, frame)
        let run = literal.length >= 2
            ? literal
            : longestCommonRun(window.map(aligning), frame.map(aligning))
        // Two lines is the floor. One shared line is regularly a blank or a repeated prompt, and
        // aligning on it puts the rest of the frame in the wrong place.
        guard run.length >= 2 else { return document + [gapMarker] + frame }
        // **Where the frame starts, not where the match ends.** Taking the end of the matched run
        // appends everything after it — including the rows the terminal rewrote in place, which
        // are already in the document a line higher up. Placing the frame's first line in the
        // document says exactly how much of this capture is already known.
        let start = max(base + run.a - run.b, 0)
        let overlap = min(max(document.count - start, 0), frame.count)
        return document + frame.dropFirst(overlap)
    }

    // MARK: - The words at the end of it

    /// The picker's own furniture: the header Claude Code puts above a question, and the option
    /// rows under it.
    ///
    /// **Deliberately only those two.** Box characters were in here once and it was wrong: the
    /// picker draws a box, but so does every bordered table an assistant prints, and a rule that
    /// treats `\u{251C}` as the picker throws away the whole answer above a table. Measured
    /// against a real waiting screen on 2026-09-01, that turned a complete analysis into nothing
    /// offered at all. The picker is normally not in this text anyway — ``region(of:)`` cuts at
    /// the rule above it — so this is the belt for the screens that draw one without a rule.
    private static func isPicker(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard let first = trimmed.first else { return false }
        return first == "\u{2610}" || first == "\u{276F}"
    }

    /// A row that is drawing something rather than saying it — a table's borders, a box the
    /// picker puts a consequence in. Skipped without being treated as a boundary: it is neither
    /// prose to keep nor evidence that the prose above it belongs to somebody else.
    private static func isFrame(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard let first = trimmed.first else { return false }
        return "\u{250C}\u{2502}\u{2514}\u{251C}\u{2510}\u{2518}\u{2524}\u{252C}\u{2534}\u{253C}\u{2570}\u{256D}\u{256E}\u{256F}".contains(first)
            || isRule(line)
    }

    /// A tool call, or a line of what one returned.
    private static func isTool(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        if trimmed.hasPrefix("\u{23BF}") { return true }
        // `⏺ Bash(...)` is a tool; `⏺ some words` is Claude Code speaking.
        guard trimmed.hasPrefix("\u{23FA}") else { return false }
        let body = trimmed.dropFirst().trimmingCharacters(in: .whitespaces)
        return body.range(of: "^[A-Z][A-Za-z]+\\(", options: .regularExpression) != nil
            || body.range(of: "^(Read|Update|Write|Search|Bash|Task|Running|Called|Explored)\\b",
                          options: .regularExpression) != nil
    }

    /// How many columns a line occupies. CJK and full-width characters take two, which matters
    /// here for one reason only: it is how you tell a line the terminal broke from a line the
    /// author broke.
    static func displayWidth(_ line: String) -> Int {
        var width = 0
        for scalar in line.unicodeScalars {
            switch scalar.value {
            case 0x1100...0x115F, 0x2E80...0x303E, 0x3041...0x33FF, 0x3400...0x4DBF,
                 0x4E00...0x9FFF, 0xA000...0xA4CF, 0xAC00...0xD7A3, 0xF900...0xFAFF,
                 0xFE30...0xFE6F, 0xFF00...0xFF60, 0xFFE0...0xFFE6,
                 0x1F300...0x1F64F, 0x1F900...0x1F9FF, 0x20000...0x3FFFD:
                width += 2
            case 0x0300...0x036F:
                break
            default:
                width += 1
            }
        }
        return width
    }

    /// The width the screen was drawn at, taken as the widest line on it. A terminal hard-wraps
    /// at its own width, so the longest line is at or just under it.
    static func drawnWidth(of lines: [String]) -> Int {
        lines.map(displayWidth).max() ?? 0
    }

    /// Undo the terminal's hard wrap inside one paragraph.
    ///
    /// **A line is only joined to the next when it ran to the edge.** That is the whole test, and
    /// it is what separates a wrap the terminal invented from a break the author typed — without
    /// it, a list of five files comes back as one sentence, which is what happened the first time
    /// this was measured against a real screen. Blank lines still separate paragraphs.
    ///
    /// The remaining judgement call is the seam. A wrap between two English words ate the space
    /// that was there; a wrap between two CJK characters ate nothing. So a space goes back only
    /// between two ASCII word characters.
    private static func unwrap(_ paragraph: [String], width: Int) -> String {
        var out = ""
        var joinable = false
        for line in paragraph {
            let piece = line.trimmingCharacters(in: .whitespaces)
            let ranToTheEdge = width > 0 && displayWidth(line) >= width - 2
            if out.isEmpty {
                out = piece
            } else if joinable {
                let left = out.last.map { $0.isLetter || $0.isNumber || $0 == "," || $0 == "." } ?? false
                let right = piece.first.map { $0.isLetter || $0.isNumber } ?? false
                let ascii = (out.last?.isASCII ?? false) && (piece.first?.isASCII ?? false)
                out += (left && right && ascii) ? " " + piece : piece
            } else {
                // Two spaces first: this break was in the author's text, and the reader's
                // Markdown needs to be told that in the one way it understands.
                out += "  \n" + piece
            }
            joinable = ranToTheEdge
        }
        return out
    }

    /// The last thing the assistant said on this screen, or nil when the tail is not speech.
    ///
    /// Walks back from the end. The picker is stepped over — its header, its options, and the
    /// question itself, which the reader already has as a card and does not need twice. The walk
    /// stops at the first tool row, and **a screen whose tail is a tool row offers nothing at
    /// all**: everything above it belongs to a turn the transcript has already written down.
    static func trailingProse(of document: [String], width: Int? = nil) -> String? {
        let columns = width ?? drawnWidth(of: document)
        var lines = document
        // Nothing before an unplaced frame can be trusted to sit where it looks like it sits.
        if let gap = lines.lastIndex(of: gapMarker) { lines = Array(lines[(gap + 1)...]) }
        var collected: [[String]] = []
        var paragraph: [String] = []
        var started = false
        for line in lines.reversed() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if isPicker(line) {
                // Still inside the picker, so whatever was collected below this is the picker's
                // own words rather than the sentences leading up to it.
                collected = []
                paragraph = []
                started = false
                continue
            }
            if isFrame(line) {
                if !paragraph.isEmpty { collected.append(paragraph.reversed()); paragraph = [] }
                continue
            }
            if isTool(line) {
                if !started { return nil }
                break
            }
            if trimmed.isEmpty {
                if !paragraph.isEmpty { collected.append(paragraph.reversed()); paragraph = [] }
                continue
            }
            started = true
            paragraph.append(trimmed)
        }
        if !paragraph.isEmpty { collected.append(paragraph.reversed()) }
        guard !collected.isEmpty else { return nil }
        let text = collected.reversed().map { unwrap($0, width: columns) }
            .joined(separator: "\n\n")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? nil : text
    }

    // MARK: - One document per session

    private static let lock = NSLock()
    private static var documents: [String: [String]] = [:]

    /// How much of each session's screen history to keep. Long enough to hold an answer that
    /// scrolled several screens, short enough that ten idle sessions cost nothing worth counting.
    private static let retainedLines = 600

    /// Take one capture. Called from the reading that already fetched it, on its own queue.
    static func observe(_ sessionID: String, screen: String?) {
        guard let screen, !screen.isEmpty else { return }
        let frame = region(of: screen)
        guard !frame.isEmpty else { return }
        lock.lock()
        defer { lock.unlock() }
        let grown = reconcile(documents[sessionID] ?? [], with: frame)
        documents[sessionID] = Array(grown.suffix(retainedLines))
    }

    /// Whether anything has been captured for this session yet. A reader arriving at a session
    /// nobody has been following is the one moment worth paying for a capture inline.
    static func hasDocument(_ sessionID: String) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return documents[sessionID]?.isEmpty == false
    }

    /// What this session is saying that its transcript has not recorded, or nil when the screen
    /// ends in something other than speech.
    static func unsyncedProse(_ sessionID: String) -> String? {
        lock.lock()
        let document = documents[sessionID]
        lock.unlock()
        guard let document else { return nil }
        return trailingProse(of: document)
    }

    /// Forget every session that is no longer in the inventory. A closed tab's screen is not
    /// coming back, and its words are in the transcript by then.
    static func retain(_ live: Set<String>) {
        lock.lock()
        defer { lock.unlock() }
        documents = documents.filter { live.contains($0.key) }
    }

    /// Test seam: drop everything.
    static func forgetAllForTesting() {
        lock.lock()
        documents = [:]
        lock.unlock()
    }
}
