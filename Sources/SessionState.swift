import Foundation

/// What a session is doing, read off its own screen.
///
/// The session list is the one place the bar shows every session at once, and until now every
/// row said the same kind of thing: a tab title, which is the *task*. Four tabs working on
/// "investigate the webhook" are four identical rows, and the question you actually have when
/// you open that list — which one stopped, which one wants something — was not on it.
///
/// This is shape recognition in the same spirit as ``Activity``: Claude Code's screen is not an
/// interface anybody promised to keep, so a reading that is not certain returns nothing rather
/// than a guess. `unknown` is a real answer and not an error — a session whose screen could not
/// be read is not the same as an idle one, and drawing it as idle would be a confident wrong
/// answer about the state of somebody's work.
enum SessionState: Equatable {

    /// Something is running, and the live line that says so — it carries its own clock, written
    /// by Claude Code, which is a better number than any this could keep for it.
    case working(String)

    /// A question is on screen and nobody has answered it. **This is the state the list exists
    /// for**: it is the only one that costs you something for every second it goes unnoticed.
    case waiting

    /// Nothing running and nothing asked.
    case idle

    /// The screen could not be read.
    case unknown

    var isWaiting: Bool { self == .waiting }

    /// Read a session's screen.
    ///
    /// `nil` means the capture failed — iTerm2 not answering, a pane that has gone away, a
    /// permission not granted yet — and it is deliberately not folded into `idle`.
    static func read(_ screen: String?) -> SessionState {
        guard let screen, !screen.isEmpty else { return .unknown }
        let text = Ansi.plain(screen)

        // A menu beats a spinner, and the order is the whole of the correctness here. Claude Code
        // draws its dialog *below* whatever came before it, and the spinner line above it is not
        // always erased — so a reader that asked "is it busy?" first would find that stale line,
        // report the session as working, and hide the one row that needed a person.
        if isChoosing(text) { return .waiting }
        if let line = Activity.parse(text) { return .working(line) }
        return .idle
    }

    /// True when the bottom of the screen holds a menu with a caret parked on one of its options.
    ///
    /// Every question Claude Code stops for is drawn the same way — a permission request, a plan
    /// to approve, a trust dialog, a slash command that offers choices: numbered options, one of
    /// them marked with a caret. That shape is what is recognised, not any particular sentence.
    /// The sentences are English, undocumented, and change between releases; the shape is what
    /// survives, and it is the same reason ``Activity`` keys on the spinner glyph rather than on
    /// the word next to it.
    ///
    /// **Two options are required, not one.** Claude writes numbered lists in prose all day long
    /// — steps, findings, options in a paragraph. What prose does not do is put a selection caret
    /// on one of them, and a menu never offers fewer than two things to choose between. Either
    /// test alone lets ordinary output through; together they do not.
    static func isChoosing(_ screen: String, tailLines: Int = 30) -> Bool {
        let lines = screen
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { String($0) }
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }

        var options = 0
        var carets = 0
        for line in lines.suffix(tailLines) {
            let (isOption, hasCaret) = option(line)
            if isOption { options += 1 }
            if hasCaret { carets += 1 }
        }
        return carets >= 1 && options >= 2
    }

    /// The carets a terminal menu marks its current row with. Deliberately not `>`: a markdown
    /// quote of a numbered list starts with exactly that, and Claude quotes things.
    private static let carets: Set<Character> = ["❯", "›", "▸", "▶"]

    /// The characters a box draws itself with. A dialog's options sit inside one, so the line as
    /// captured is `│ ❯ 1. Yes …│` and the caret is not at the front of it.
    private static let boxes: Set<Character> = ["│", "┃", "|", "▌", "▏", "╎", "┆", "┊"]

    /// Is this line one of a menu's options, and is the caret on it.
    private static func option(_ raw: String) -> (isOption: Bool, hasCaret: Bool) {
        let chars = Array(raw)
        var i = 0

        // Past the padding and the wall the dialog is drawn inside.
        while i < chars.count, chars[i] == " " || chars[i] == "\t" || boxes.contains(chars[i]) {
            i += 1
        }

        // **A caret at column zero is the prompt, not a menu.**
        //
        // `❯` is both the glyph a dialog marks its current row with and the one Claude Code puts
        // in front of the line you type. So a message that begins with a numbered list echoes as
        // `❯ 1. …` with `2. …` under it — which is character for character the shape of a menu
        // with its first row selected, and the phone was told a question was waiting every time
        // somebody sent a list. It then told them not to answer from there, which left no way to
        // do anything at all.
        //
        // A dialog's options are indented or inside a box — the comment on `boxes` says so and
        // the capture bears it out. The prompt is flush left. So the caret only counts if
        // something came before it.
        var caret = false
        if i < chars.count, carets.contains(chars[i]) {
            caret = i > 0
            i += 1
            while i < chars.count, chars[i] == " " { i += 1 }
        }

        // Digits, a full stop, a space, and then something worth choosing.
        let start = i
        while i < chars.count, chars[i].isNumber { i += 1 }
        guard i > start, i < chars.count, chars[i] == "." else { return (false, false) }
        i += 1
        guard i < chars.count, chars[i] == " " else { return (false, false) }
        while i < chars.count, chars[i] == " " { i += 1 }
        guard i < chars.count else { return (false, false) }
        return (true, caret)
    }
}
