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

    /// The menu on screen, if there is one — its options, and which of them the caret is on.
    ///
    /// This used to answer `Bool` and throw the rest away, which was the whole of the problem it
    /// now solves. The options were already parsed — they had to be, to count them — and were
    /// already written to the log, and were then discarded, so a phone could be told a question
    /// was waiting and never told what it was being asked. See ``isChoosing``, which is this with
    /// the answer thrown away again, kept because most callers only want the yes or no.
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
    static func menu(_ screen: String, tailLines: Int = 30) -> Menu? {
        let lines = screen
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { String($0) }
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }

        var options: [Menu.Option] = []
        var carets = 0
        for line in lines.suffix(tailLines) {
            guard let row = option(line) else { continue }
            if row.caret { carets += 1 }
            options.append(Menu.Option(number: row.number, label: row.label, selected: row.caret))
        }
        guard carets >= 1, options.count >= 2 else { return nil }

        // **What made it think so.** A false reading here tells somebody a question is waiting
        // and then hands them the wrong buttons to answer it with, which is worse than the old
        // failure of handing them none. Rare by nature: a real menu happens a few times an hour
        // and the log line is two of them.
        Log.write("choosing: carets=\(carets) options=\(options.count) — "
                  + options.map { "\($0.number). \($0.label.prefix(60))" }.joined(separator: " ⏐ "))
        return Menu(options: options, selected: options.first(where: \.selected)?.number)
    }

    /// Whether a menu is on screen at all. The shape every existing caller wanted.
    static func isChoosing(_ screen: String, tailLines: Int = 30) -> Bool {
        menu(screen, tailLines: tailLines) != nil
    }

    /// A menu as something other than a picture of one.
    ///
    /// **The numbers are what gets sent, not the positions.** Claude Code's picker takes a digit
    /// and acts on it, so a row's own number is the thing that answers it — and a row whose
    /// number is not one a keystroke can carry is shown and not offered, rather than quietly
    /// renumbered into something that would answer a different question.
    struct Menu: Equatable {
        struct Option: Equatable {
            /// The number as printed. This is the keystroke.
            let number: Int
            let label: String
            /// The caret is parked on this one, so it is what a bare Return would confirm.
            let selected: Bool
            /// Whether a keystroke can carry it — see ``Targets/answer(_:to:)``, which is 1…9.
            var answerable: Bool { (1...9).contains(number) }
        }
        var options: [Option]
        var selected: Int?
    }

    /// The carets a terminal menu marks its current row with. Deliberately not `>`: a markdown
    /// quote of a numbered list starts with exactly that, and Claude quotes things.
    private static let carets: Set<Character> = ["❯", "›", "▸", "▶"]

    /// The characters a box draws itself with. A dialog's options sit inside one, so the line as
    /// captured is `│ ❯ 1. Yes …│` and the caret is not at the front of it.
    private static let boxes: Set<Character> = ["│", "┃", "|", "▌", "▏", "╎", "┆", "┊"]

    /// This line as a menu option — its number, its words, and whether the caret is on it.
    /// `nil` for every other line on the screen, which is nearly all of them.
    private static func option(_ raw: String) -> (number: Int, label: String, caret: Bool)? {
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
        guard i > start, i < chars.count, chars[i] == "." else { return nil }
        guard let number = Int(String(chars[start..<i])) else { return nil }
        i += 1
        guard i < chars.count, chars[i] == " " else { return nil }
        while i < chars.count, chars[i] == " " { i += 1 }
        guard i < chars.count else { return nil }

        // The words, with the far wall of the dialog taken off. A capture is `│ ❯ 1. Yes …    │`
        // and the trailing bar is part of the box rather than part of the answer — it is the one
        // piece of the drawing that survives this far, because everything before the number was
        // skipped on the way in and nothing was looking at the end.
        var label = String(chars[i...])
        while let last = label.last, last == " " || last == "\t" || boxes.contains(last) {
            label.removeLast()
        }
        guard !label.isEmpty else { return nil }
        return (number, label, caret)
    }
}
