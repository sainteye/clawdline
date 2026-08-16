import Foundation

/// The line Claude Code draws while it is working — "Generating… (21s · thinking)".
///
/// It is not in the transcript. The transcript records messages once they exist; this is a
/// spinner painted straight onto the terminal and erased again, so the only place to read it
/// is the screen. That makes it the one thing worth capturing even when the conversation is
/// coming from the file.
///
/// Nothing about the format is documented and all of it can change, so this reads like the
/// transcript parser: recognise a shape, and return nothing at all rather than a guess.
enum Activity {

    /// The glyphs the spinner cycles through. Observed, not documented — and deliberately the
    /// discriminator, because a terminal is full of other lines that end in "(3s)": tool
    /// results, timing output, an echoed command that happened to be truncated with an ellipsis.
    private static let spinners: Set<Character> = ["✳", "✻", "✽", "✢", "✶", "✱", "✴", "·", "*"]

    /// The live line, or nil when nothing is running.
    ///
    /// Only the tail of the screen is searched. Claude Code draws this immediately above the
    /// input box, and a tall window can still be holding older spinner lines further up that
    /// were scrolled past rather than erased — reading one of those would report a session as
    /// busy long after it went quiet.
    static func parse(_ screen: String, tailLines: Int = 25) -> String? {
        let lines = screen
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

    /// True when the line carries an elapsed counter — "(21s", "(5m 52s · ↓ 15.3k tokens)".
    ///
    /// This is what separates the live line from anything else that starts with a bullet: the
    /// counter only exists while something is being waited on.
    ///
    /// It has to survive the minutes form. The first version of this only understood "(21s",
    /// which was the whole of one sample — and everything past a minute went unrecognised,
    /// meaning the strip vanished exactly when a long wait made it worth having.
    private static func hasElapsed(_ text: String) -> Bool {
        let chars = Array(text)
        var i = 0
        while i < chars.count {
            guard chars[i] == "(" else { i += 1; continue }
            var j = i + 1
            var previousWasDigit = false
            while j < chars.count, chars[j] != ")" {
                if chars[j] == "s", previousWasDigit {
                    // "s" has to end the number, not start a word: "(3 stages" is not a clock.
                    let next = j + 1 < chars.count ? chars[j + 1] : " "
                    if !next.isLetter { return true }
                }
                previousWasDigit = chars[j].isNumber
                j += 1
            }
            i += 1
        }
        return false
    }
}
