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
    static func read(_ screen: String?, assistant: Assistant = .claude,
                     hookWaiting: Bool = false) -> SessionState {
        guard let screen, !screen.isEmpty else { return .unknown }
        let text = Ansi.plain(screen)

        // A menu beats a spinner, and the order is the whole of the correctness here. Claude Code
        // draws its dialog *below* whatever came before it, and the spinner line above it is not
        // always erased — so a reader that asked "is it busy?" first would find that stale line,
        // report the session as working, and hide the one row that needed a person.
        if isChoosing(text, assistant: assistant, hookWaiting: hookWaiting) { return .waiting }
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
        // Keep physical line numbers beside the non-empty tail. The menu detector deliberately
        // ignores empty terminal rows, but those rows are meaningful boundaries when reading the
        // prose immediately above the first option.
        let visible = lines.enumerated().filter {
            !$0.element.trimmingCharacters(in: .whitespaces).isEmpty
        }
        let tail = Array(visible.suffix(tailLines))
        let tailText = tail.map { $0.element }
        if assistant == .codex { return codexMenu(tailText) }

        // AskUserQuestion is the exception to Claude Code's usual drawing: its selected row is
        // flush left, just like the composer. That row is trusted only after a hook has stated
        // that the session is waiting, and only when it is the last caret on screen; otherwise
        // an echoed numbered message would become a menu again. The transcript cannot supply
        // the missing gate or labels: measured while the picker is open, its call is not written
        // there until the answer and result arrive together.
        var flushLeftSelection: Int? = nil
        if hookWaiting {
            let hasIndentedSelection = tailText.contains { line in
                guard let row = option(line) else { return false }
                return row.caret && row.indented
            }
            if !hasIndentedSelection, let caret = tailText.lastIndex(where: { hasCaret($0) }),
               let row = option(tailText[caret]), row.caret, !row.indented {
                flushLeftSelection = caret
            }
        }

        // **Where the dialog starts.** Numbered rows on screen are not all the same list: an
        // assistant writing "three things I found" leaves `1.` `2.` `3.` above the frame, and
        // counting those as options gave one session eight of them — with the first landing
        // outside the dialog, so the question was then read from the prose above *that*, and a
        // page of somebody's analysis arrived on a phone as the thing being asked. The rule
        // below the caret is the dialog; anything above its frame or its header belongs to the
        // conversation. With no frame found the whole tail is fair game, which is the behaviour
        // this had before.
        let selectedCaret = flushLeftSelection ?? tailText.lastIndex { line in
            guard let row = option(line) else { return false }
            return row.caret && row.indented
        }
        var dialogStart = 0
        if let anchor = selectedCaret {
            var scan = anchor - 1
            while scan >= 0 {
                if isBoxRule(tailText[scan]) || isQuestionHeader(dialogText(tailText[scan])) {
                    dialogStart = scan + 1
                    break
                }
                scan -= 1
            }
        }

        // **An unframed flush-left caret is text, not a dialog.** The gate was built on the idea
        // that a hook saying "waiting" makes that caret trustworthy, and it does not: auto mode
        // raises permission events constantly, so the gate is open while the screen shows
        // whatever the session happens to be printing. A `❯ 1. Yes` echoed out of a heredoc was
        // read as a menu and offered to a phone, which pressed it — the failure this whole rule
        // exists to prevent, arriving through the door the gate had opened.
        //
        // Every dialog captured from a real terminal is drawn inside a frame, so the frame is the
        // second fact required. An indented caret still needs none: that shape has never been
        // ambiguous, and it is what permission dialogs and `/model` have always drawn.
        if flushLeftSelection != nil, dialogStart == 0 { flushLeftSelection = nil }

        var options: [Menu.Option] = []
        var carets = 0
        var firstOptionLine: Int? = nil
        for (index, captured) in tail.enumerated() where index >= dialogStart {
            let line = captured.element
            guard let row = option(line) else { continue }
            // **A caret at column zero is the prompt, not a menu** — see ``option(_:)``, which
            // reports where the caret was rather than ruling on it. `❯` is both the glyph a
            // dialog marks its current row with and the one Claude Code puts in front of the
            // line you type, so a message that begins with a numbered list echoes as `❯ 1. …`
            // with `2. …` under it. The row is still listed; absent the hook gate above, it is
            // just not a selection.
            let selected = row.caret && (row.indented || index == flushLeftSelection)
            if selected { carets += 1 }
            if firstOptionLine == nil { firstOptionLine = captured.offset }
            options.append(Menu.Option(number: row.number, label: row.label,
                                       detail: detail(under: captured.offset, in: lines),
                                       selected: selected))
        }
        guard carets >= 1, options.count >= 2 else { return nil }

        let menuQuestion: String? = firstOptionLine.flatMap {
            Self.question(in: lines, before: $0, noEarlierThan: tail.first?.offset ?? 0)
        }

        // **What made it think so.** A false reading here tells somebody a question is waiting
        // and then hands them the wrong buttons to answer it with, which is worse than the old
        // failure of handing them none. Rare by nature: a real menu happens a few times an hour
        // and the log line is two of them.
        Log.write("choosing: carets=\(carets) options=\(options.count) — "
                  + options.map { "\($0.number). \($0.label.prefix(60))" }.joined(separator: " ⏐ "))
        return Menu(question: menuQuestion, options: options,
                    selected: options.first(where: \.selected)?.number)
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
            /// The rows drawn under the label, joined. `AskUserQuestion` puts the consequence of
            /// each answer there — which models get dropped, what it costs, what breaks — and a
            /// label without it is often not enough to choose on: "cut the slow five" does not
            /// say which five. Empty when the dialog draws single-line options, as permission
            /// prompts and `/model` do.
            var detail: String? = nil
            /// The caret is parked on this one, so it is what a bare Return would confirm.
            let selected: Bool
            /// Whether a keystroke can carry it — see ``Targets/answer(_:to:)``, which is 1…9.
            var answerable: Bool { (1...9).contains(number) }
        }
        /// The full question, either from structured data or the text immediately above the
        /// numbered rows in a visual capture. It stays nil when that boundary cannot be read.
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

    /// The prose immediately above a menu's first numbered row.
    ///
    /// AskUserQuestion puts a short `☐ header` above the prose. That is classification rather
    /// than the thing somebody has to answer, so it is a boundary and is intentionally omitted.
    /// Empty rows and box rules are stronger boundaries: crossing either would pull commands,
    /// permission details, or the preceding conversation into the question shown on a phone.
    /// The rows drawn under one option, joined into a sentence.
    ///
    /// Walks down until the next option, a frame or a blank — the same edges the question uses,
    /// read the other way. Nothing is inferred about indentation: a description is simply
    /// whatever prose sits between this numbered row and the next thing that is not prose, which
    /// is what the dialog draws and does not depend on how far it happens to be indented.
    ///
    /// **On the physical lines, blanks included.** The dialog puts one before the navigation hint
    /// at the bottom, so reading a version with the blanks stripped out attached
    /// "Enter to select · Esc to cancel" to the last option as though it were its description.
    private static func detail(under optionIndex: Int, in lines: [String]) -> String? {
        var parts: [String] = []
        var index = optionIndex + 1
        while index < lines.count {
            let raw = lines[index]
            if option(raw) != nil || isBoxRule(raw) || hasCaret(raw) { break }
            let text = dialogText(raw)
            if text.isEmpty || isQuestionHeader(text) { break }
            parts.append(text)
            index += 1
        }
        let joined = parts.joined(separator: " ").trimmingCharacters(in: .whitespacesAndNewlines)
        return joined.isEmpty ? nil : joined
    }

    private static func question(in lines: [String], before optionLine: Int,
                                 noEarlierThan lowerBound: Int) -> String? {
        guard optionLine > lowerBound else { return nil }
        var parts: [String] = []
        var index = optionLine - 1
        // **A blank line inside the dialog is padding, not a boundary.** The drawn shape puts one
        // between the header and the question and another between the question and the first
        // option, so treating the first empty row as the top of the prose finds nothing at all —
        // which is exactly what happened, and what a fixture written as adjacent lines could not
        // catch. The frame, the checkbox header and a caret are the real edges; blanks are
        // stepped over. Two in a row are not padding any more, and the reach is capped so that a
        // dialog drawn without a frame cannot swallow the conversation above it.
        var blanks = 0
        var reach = 12
        // **Only prose an edge closed off is the question.** Walking up until the reach runs out
        // reads whatever was on screen before the dialog — measured once at eight lines of
        // someone's analysis presented to a phone as the thing being asked. A real dialog is
        // always framed: the rule above it, its header or tab bar, or the caret of the row above.
        // If none of those turned up, this is not a question and saying nothing is the honest
        // answer, because the fallback already handles a menu whose prose could not be read.
        var closed = false
        while index >= lowerBound, reach > 0 {
            let raw = lines[index]
            let text = dialogText(raw)
            if isBoxRule(raw) || isQuestionHeader(text) || hasCaret(raw) {
                closed = true
                break
            }
            if text.isEmpty {
                blanks += 1
                if blanks > 1 { break }
                index -= 1
                reach -= 1
                continue
            }
            blanks = 0
            parts.insert(text, at: 0)
            index -= 1
            reach -= 1
        }
        guard closed else { return nil }
        let joined = parts.joined(separator: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return joined.isEmpty ? nil : joined
    }

    /// Text inside a dialog wall, without its padding or far edge.
    private static func dialogText(_ raw: String) -> String {
        var text = raw.trimmingCharacters(in: .whitespaces)
        while let first = text.first, boxes.contains(first) {
            text.removeFirst()
            text = text.trimmingCharacters(in: .whitespaces)
        }
        while let last = text.last, boxes.contains(last) {
            text.removeLast()
            text = text.trimmingCharacters(in: .whitespaces)
        }
        return text
    }

    private static func isQuestionHeader(_ text: String) -> Bool {
        // A checkbox anywhere on the line, not only at the front. One question draws its label as
        // `☐ build`; several draw a tab bar — `←  ☐ scope  ☐ parity  ✔ Submit  →` — where the
        // first glyph is an arrow and the boxes are further in. Both are chrome above the
        // question, and prose that is the question does not contain these characters.
        return text.contains("☐") || text.contains("☑") || text.contains("☒")
    }

    /// A horizontal rule, including its corners. Requiring at least one horizontal stroke keeps
    /// an ordinary sentence containing a box character from becoming a boundary by accident.
    private static func isBoxRule(_ raw: String) -> Bool {
        let horizontal: Set<Character> = ["─", "━", "═", "╌", "╍", "┄", "┅", "┈", "┉"]
        let joints: Set<Character> = ["╭", "╮", "╰", "╯", "┌", "┐", "└", "┘", "├", "┤",
                                      "┬", "┴", "┼", "╞", "╡", "╪", "┏", "┓", "┗", "┛"]
        var sawHorizontal = false
        for char in raw {
            if char == " " || char == "\t" || boxes.contains(char) || joints.contains(char) {
                continue
            }
            if horizontal.contains(char) {
                sawHorizontal = true
                continue
            }
            return false
        }
        return sawHorizontal
    }

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
