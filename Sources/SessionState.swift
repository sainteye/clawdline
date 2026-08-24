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
    ///
    /// Which assistant is on the other end changes both halves of this. They draw their menus
    /// differently — see ``menu(_:assistant:tailLines:)`` — and they draw their live line
    /// differently, so a reader told the wrong one reports every session as idle rather than
    /// reporting something wrong, which is the failure mode this shape keeps.
    static func read(_ screen: String?, assistant: Assistant = .claude) -> SessionState {
        guard let screen, !screen.isEmpty else { return .unknown }
        let text = Ansi.plain(screen)

        // A menu beats a spinner, and the order is the whole of the correctness here. Claude Code
        // draws its dialog *below* whatever came before it, and the spinner line above it is not
        // always erased — so a reader that asked "is it busy?" first would find that stale line,
        // report the session as working, and hide the one row that needed a person.
        if isChoosing(text, assistant: assistant) { return .waiting }
        if let line = Activity.parse(text, assistant: assistant) { return .working(line) }
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
    ///
    /// Codex always breaks the one rule this relies on, which is why the assistant has to be
    /// known: **its caret is flush left**, in the same column as the caret in front of the box
    /// you type into. See ``codexMenu(_:)``. Claude Code's AskUserQuestion now does the same, but
    /// that ambiguous shape is accepted only when `hookWaiting` supplies an independent fact.
    static func menu(_ screen: String, assistant: Assistant = .claude,
                     tailLines: Int = 30, hookWaiting: Bool = false) -> Menu? {
        let lines = screen
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { String($0) }
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        let tail = Array(lines.suffix(tailLines))
        if assistant == .codex { return codexMenu(tail) }

        // AskUserQuestion is the exception to Claude Code's usual drawing: its selected row is
        // flush left, just like the composer. That row is trusted only after a hook has stated
        // that the session is waiting, and only when it is the last caret on screen; otherwise
        // an echoed numbered message would become a menu again. The transcript cannot supply
        // the missing gate or labels: measured while the picker is open, its call is not written
        // there until the answer and result arrive together.
        var flushLeftSelection: Int? = nil
        if hookWaiting {
            let hasIndentedSelection = tail.contains { line in
                guard let row = option(line) else { return false }
                return row.caret && row.indented
            }
            if !hasIndentedSelection, let caret = tail.lastIndex(where: { hasCaret($0) }),
               let row = option(tail[caret]), row.caret, !row.indented {
                flushLeftSelection = caret
            }
        }

        var options: [Menu.Option] = []
        var carets = 0
        for (index, line) in tail.enumerated() {
            guard let row = option(line) else { continue }
            // **A caret at column zero is the prompt, not a menu** — see ``option(_:)``, which
            // reports where the caret was rather than ruling on it. `❯` is both the glyph a
            // dialog marks its current row with and the one Claude Code puts in front of the
            // line you type, so a message that begins with a numbered list echoes as `❯ 1. …`
            // with `2. …` under it. The row is still listed; absent the hook gate above, it is
            // just not a selection.
            let selected = row.caret && (row.indented || index == flushLeftSelection)
            if selected { carets += 1 }
            options.append(Menu.Option(number: row.number, label: row.label, selected: selected))
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
    static func isChoosing(_ screen: String, assistant: Assistant = .claude,
                           tailLines: Int = 30, hookWaiting: Bool = false) -> Bool {
        menu(screen, assistant: assistant, tailLines: tailLines,
             hookWaiting: hookWaiting) != nil
    }

    // MARK: - Codex

    /// The menu Codex is showing, if it is showing one.
    ///
    /// Same numbered rows, same caret, one difference that changes everything: **Codex marks the
    /// selected row with a caret in column zero**, which is exactly where it also draws the caret
    /// in front of the composer. The rule the Claude Code side leans on — a caret at the left
    /// margin is a prompt, not a menu — says nothing here, because both of them are.
    ///
    /// What does separate them is position. Codex draws a dialog at the bottom of the screen and
    /// takes the composer away while it is up; every other caret on screen belongs to a message
    /// somebody already sent, with the composer sitting below it. So **the last caret on the
    /// screen is the one that decides**: if it heads a numbered row there is a dialog, and if it
    /// is the composer there is not. A message that happened to begin "1. …" cannot fool this,
    /// because the composer is still underneath it.
    ///
    /// From there the run of rows is walked outward in both directions, because the caret is on
    /// whichever row you have arrowed to and not necessarily the first.
    private static func codexMenu(_ lines: [String]) -> Menu? {
        guard let caret = lines.lastIndex(where: { hasCaret($0) }),
              let head = option(lines[caret]), head.caret else { return nil }

        var options = [Menu.Option(number: head.number, label: head.label, selected: true)]
        var i = caret - 1
        while i >= 0, let row = option(lines[i]) {
            options.insert(Menu.Option(number: row.number, label: row.label, selected: false), at: 0)
            i -= 1
        }
        i = caret + 1
        while i < lines.count, let row = option(lines[i]) {
            options.append(Menu.Option(number: row.number, label: row.label, selected: false))
            i += 1
        }
        guard options.count >= 2 else { return nil }
        Log.write("choosing (codex): options=\(options.count) — "
                  + options.map { "\($0.number). \($0.label.prefix(60))" }.joined(separator: " ⏐ "))
        return Menu(options: options, selected: options.first(where: \.selected)?.number)
    }

    /// Whether this line is marked with a caret at all, wherever it sits. The padding and the
    /// wall of a box are skipped first, for the same reason ``option(_:)`` skips them.
    private static func hasCaret(_ raw: String) -> Bool {
        for char in raw {
            if char == " " || char == "\t" || boxes.contains(char) { continue }
            return carets.contains(char)
        }
        return false
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
        /// The full question when it came from structured data. A screen capture only knows the
        /// numbered rows beneath it, so this stays nil for the visual fallback.
        var question: String? = nil
        var options: [Option]
        var selected: Int?
    }

    /// The carets a terminal menu marks its current row with. Deliberately not `>`: a markdown
    /// quote of a numbered list starts with exactly that, and Claude quotes things.
    private static let carets: Set<Character> = ["❯", "›", "▸", "▶"]

    /// The characters a box draws itself with. A dialog's options sit inside one, so the line as
    /// captured is `│ ❯ 1. Yes …│` and the caret is not at the front of it.
    private static let boxes: Set<Character> = ["│", "┃", "|", "▌", "▏", "╎", "┆", "┊"]

    /// This line as a menu option — its number, its words, whether the caret is on it, and
    /// whether the line was indented. `nil` for every other line on the screen, which is nearly
    /// all of them.
    ///
    /// **The indentation is reported rather than judged.** It used to be folded into `caret`
    /// here, which was right while Claude Code was the only thing being read and wrong the day
    /// Codex was: one assistant's dialog is indented and the other's is flush left, so the two
    /// callers need the same two facts and different rules about them.
    private static func option(_ raw: String)
        -> (number: Int, label: String, caret: Bool, indented: Bool)? {
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
        let indented = i > 0
        var caret = false
        if i < chars.count, carets.contains(chars[i]) {
            caret = true
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
        return (number, label, caret, indented)
    }
}
