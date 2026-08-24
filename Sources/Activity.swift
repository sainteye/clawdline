import Foundation

/// The line an assistant draws while it is working — "Generating… (21s · thinking)" in Claude
/// Code, "• Working (10s • esc to interrupt)" in Codex.
///
/// It is not in the transcript. The transcript records messages once they exist; this is a
/// spinner painted straight onto the terminal and erased again, so the only place to read it
/// is the screen. That makes it the one thing worth capturing even when the conversation is
/// coming from the file.
///
/// Nothing about the format is documented and all of it can change, so this reads like the
/// transcript parser: recognise a shape, and return nothing at all rather than a guess.
enum Activity {

    /// The live line, whoever is drawing it.
    static func parse(_ screen: String, assistant: Assistant, tailLines: Int = 25) -> String? {
        switch assistant {
        case .claude: return parse(screen, tailLines: tailLines)
        case .codex:  return codex(screen, tailLines: tailLines)
        }
    }

    /// The glyphs the spinner cycles through. Observed, not documented — and deliberately the
    /// discriminator, because a terminal is full of other lines that end in "(3s)": tool
    /// results, timing output, an echoed command that happened to be truncated with an ellipsis.
    private static let spinners: Set<Character> = [
        "✳", "✻", "✽", "✢", "✶", "✱", "✴", "·", "*",
        // 2.1.228 started drawing half circles in the tab title. The live line has not moved yet,
        // but the set it is drawn from evidently can — and the cost of being early here is nil,
        // because a glyph on its own proves nothing: the line still has to carry an ellipsis and
        // a running clock before any of this is believed.
        "◐", "◑", "◒", "◓", "◴", "◵", "◶", "◷",
    ]

    /// The live line, or nil when nothing is running.
    ///
    /// Only the tail of the screen is searched. Claude Code draws this immediately above the
    /// input box, and a tall window can still be holding older spinner lines further up that
    /// were scrolled past rather than erased — reading one of those would report a session as
    /// busy long after it went quiet.
    static func parse(_ screen: String, tailLines: Int = 25) -> String? {
        // tmux keeps the colours in its captures, and a line that starts with a colour code does
        // not start with the character it looks like it starts with — so the glyph test below saw
        // ESC every time and no tmux session ever reported being busy.
        let lines = Ansi.plain(screen)
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }

        for line in lines.suffix(tailLines).reversed() {
            guard let glyph = line.first, spinners.contains(glyph) else { continue }
            let rest = String(line.dropFirst()).trimmingCharacters(in: .whitespaces)
            guard rest.contains("…"), hasElapsed(rest) else { continue }
            return rest
        }
        return nil
    }

    // MARK: - Codex

    /// The bullets Codex puts in front of a line of its own. Observed, not documented — and,
    /// unlike Claude Code's, they do not cycle: the animation is in the word, not the glyph.
    private static let bullets: Set<Character> = ["•", "‣", "▪", "·", "*"]

    /// Codex's live line: a bullet, a word, and a clock in brackets.
    ///
    /// **The clock is the whole discriminator.** Codex prefixes everything it says with the same
    /// bullet — "• Running sleep 25", "• Created notes.txt containing hello." — so the glyph
    /// proves nothing on its own. What only exists while something is being waited on is the
    /// counter: "(10s • esc to interrupt)". `hasElapsed` is the same test the Claude Code side
    /// uses, and finding it is what makes this a live line rather than a sentence.
    ///
    /// Cut at the closing bracket. What follows it is advice for somebody at the keyboard —
    /// "· 1 background terminal running · /ps to view" — and this line is drawn on a phone.
    static func codex(_ screen: String, tailLines: Int = 25) -> String? {
        let lines = Ansi.plain(screen)
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }

        for line in lines.suffix(tailLines).reversed() {
            guard let glyph = line.first, bullets.contains(glyph) else { continue }
            let rest = String(line.dropFirst()).trimmingCharacters(in: .whitespaces)
            guard hasElapsed(rest) else { continue }
            guard let close = rest.firstIndex(of: ")") else { return rest }
            return String(rest[rest.startIndex...close])
        }
        return nil
    }

    /// True when the line carries an elapsed counter — "(21s", "(5m 52s · ↓ 15.3k tokens)".
    ///
    /// This is what separates the live line from anything else that starts with a bullet: the
    /// counter only exists while something is being waited on.
    ///
    /// It has to survive the minutes form. The first version of this only understood "(21s",
    /// which was the whole of one sample — and everything past a minute went unrecognised,
    /// meaning the strip vanished exactly when a long wait made it worth having.
    static func hasElapsed(_ text: String) -> Bool {
        elapsed(in: text) != nil
    }

    /// The counter inside a live line, in seconds.
    ///
    /// This is more than a parser convenience. It is the assistant's own clock, so it survives
    /// Clawdline launching halfway through a turn; a timer that begins with the first screen
    /// capture does not. The finish notifier uses it to recover the part of a long turn it did
    /// not personally witness.
    static func elapsed(in text: String) -> TimeInterval? {
        let chars = Array(text)
        var i = 0
        while i < chars.count {
            guard chars[i] == "(" else { i += 1; continue }
            var j = i + 1
            var total = 0
            while j < chars.count, chars[j] != ")" {
                guard chars[j].isNumber else { j += 1; continue }
                let start = j
                while j < chars.count, chars[j].isNumber { j += 1 }
                guard let value = Int(String(chars[start..<j])), j < chars.count else { continue }
                // The unit must touch the number. Without that rule, `(3 stages)` becomes three
                // seconds merely because the next word starts with an s.
                switch chars[j] {
                case "h": total += value * 3600
                case "m": total += value * 60
                case "s": return TimeInterval(total + value)
                default: continue
                }
                j += 1
            }
            i += 1
        }
        return nil
    }
}
