import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

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
        TerminalSessionPresentation.workingLine(
            in: screen, assistant: assistant, tailLines: tailLines)
    }

    /// Kept as historical documentation; matching now lives in the shared portable reader.
    private static let spinners: Set<Character> = [
        "✳", "✻", "✽", "✢", "✶", "✱", "✴", "·", "*",
        "◐", "◑", "◒", "◓", "◴", "◵", "◶", "◷",
    ]

    /// The live line, or nil when nothing is running.
    ///
    /// Only the tail of the screen is searched. Claude Code draws this immediately above the
    /// input box, and a tall window can still be holding older spinner lines further up that
    /// were scrolled past rather than erased — reading one of those would report a session as
    /// busy long after it went quiet.
    static func parse(_ screen: String, tailLines: Int = 25) -> String? {
        TerminalSessionPresentation.workingLine(
            in: screen, assistant: .claude, tailLines: tailLines)
    }

    // MARK: - Codex

    /// Kept as historical documentation; matching now lives in the shared portable reader.
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
        TerminalSessionPresentation.workingLine(
            in: screen, assistant: .codex, tailLines: tailLines)
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
        TerminalSessionPresentation.elapsed(in: text) != nil
    }

    /// The counter inside a live line, in seconds.
    ///
    /// This is more than a parser convenience. It is the assistant's own clock, so it survives
    /// Clawdline launching halfway through a turn; a timer that begins with the first screen
    /// capture does not. The finish notifier uses it to recover the part of a long turn it did
    /// not personally witness.
    static func elapsed(in text: String) -> TimeInterval? {
        TerminalSessionPresentation.elapsed(in: text)
    }
}
