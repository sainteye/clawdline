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
        // an echoed numbered message would become a menu again. The transcript cannot supply the
        // missing gate — it says what was asked, never where the caret is. It *can* supply the
        // labels, and does: see ``Transcript/openQuestion(of:)``, which corrects what this comment
        // used to claim. Measured on 2026-08-26, the call sat on disk in full for twenty-seven
        // seconds before the answer landed.
        var flushLeftSelection: Int? = nil
        if hookWaiting {
            let hasIndentedSelection = tailText.contains { line in
                guard let row = option(line) else { return false }
                return row.caret && row.indented
            }
            // **The last caret is not the dialog's — the composer is below it.** A picker does not
            // take the input line away, so the moment anybody has typed a character the bottom of
            // the screen is `\u{276F} their text`, and looking only at the last caret found that
            // instead and gave up. Codex can key on the last caret because its dialog replaces the
            // composer; Claude Code's does not.
            //
            // So the last flush-left caret **that heads a numbered row** is the one. An echoed
            // list still cannot become a menu: nothing above it is a frame, and the frame is
            // required a few lines down.
            let flushCaret = tailText.lastIndex { line in
                guard let row = option(line) else { return false }
                return row.caret && !row.indented
            }
            if !hasIndentedSelection, let caret = flushCaret {
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
        //
        // **The caret is not always on a row.** A multi-select's button takes it as soon as
        // somebody arrows past the last option, and then no numbered line on screen carries one —
        // so a reading that looked only at the rows called the whole dialog nothing, and a phone
        // watching a question somebody was halfway through answering simply lost it.
        let selectedCaret = flushLeftSelection ?? tailText.lastIndex { line in
            guard let row = option(line) else { return false }
            return row.caret && row.indented
        } ?? submitRow(in: tail, from: 0).flatMap { $0.selected ? $0.row : nil }
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

        // **Found before the options, because it changes how one of them reads.** The button a
        // multi-select draws under its rows is an unnumbered line, and an unnumbered line is prose
        // to ``detail(under:in:)`` — so until this is known, the button is the last row's
        // description and the phone has nothing to press.
        let submit = submitRow(in: tail, from: dialogStart)

        var options: [Menu.Option] = []
        var carets = 0
        var firstOptionLine: Int? = nil
        var lastOptionLine: Int? = nil
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
            lastOptionLine = index
            let box = checkbox(of: withoutSidePanel(row.label))
            options.append(Menu.Option(number: row.number, label: box.label,
                                       detail: detail(under: captured.offset, in: lines,
                                                      stoppingAt: submit?.line)
                                           .map(withoutSidePanel),
                                       selected: selected, checked: box.checked))
        }
        // The caret has to be *somewhere* in the dialog, and the button counts: see above.
        guard carets >= 1 || submit?.selected == true, options.count >= 2 else {
            return plainMenu(tail, in: lines, hookWaiting: hookWaiting)
        }

        // **Rows with the session's own output under them are scrollback.** A dialog is a thing
        // the session has stopped in front of, so nothing can be printed below it until it is
        // answered. Quoted rows are not: on 2026-08-26 this reader pasted a picture of a picker
        // into its own message, the terminal drew it, and every guard here passed — an indented
        // caret, a rule above it, numbered rows with checkboxes and a `Submit` — so a session that
        // was busy working reported `waiting`, and a question nobody had asked, assembled out of
        // two columns of prose, went to somebody's phone with buttons under it.
        //
        // Below the rows a real dialog draws its hint, its frame, and the composer if anybody has
        // typed into it. It never draws another turn.
        if let lastOption = lastOptionLine,
           tail.dropFirst(lastOption + 1).contains(where: { isTurnMarker($0.element) }) {
            return nil
        }

        let menuQuestion: String? = firstOptionLine.flatMap {
            Self.question(in: lines, before: $0, noEarlierThan: tail.first?.offset ?? 0)
        }

        // **What made it think so.** A false reading here tells somebody a question is waiting
        // and then hands them the wrong buttons to answer it with, which is worse than the old
        // failure of handing them none. Rare by nature: a real menu happens a few times an hour
        // and the log line is two of them.
        Log.write("choosing: carets=\(carets) options=\(options.count) — "
                  + options.map { "\($0.number). \($0.label.prefix(60))" }.joined(separator: " ⏐ ")
                  + (submit.map { " ⏐ [\($0.label)]" } ?? ""))
        return Menu(question: menuQuestion, options: options,
                    selected: options.first(where: \.selected)?.number,
                    submit: submit.map { Menu.Submit(label: $0.label, selected: $0.selected) },
                    steps: firstOptionLine.map {
                        QuestionSteps.steps(inLines: lines, above: $0)
                    } ?? [])
    }

    /// The same dialog with its numbers taken off.
    ///
    /// **One component, two shapes.** Claude Code draws every picker with one select, and that
    /// select takes a `hideIndexes` flag. Set, it stops printing `1. ` in front of each row — and
    /// in the same breath it stops accepting digits, so these are not merely harder to read, they
    /// are answered differently. The held cross-session message is one; so is the dialog that
    /// approves a plan. On screen a row is a pointer column, one gap, then the label: `❯ Deliver`
    /// for the row the caret is on and two spaces for every other. **The labels therefore all
    /// begin in the same column**, and that column is the whole of what identifies them.
    ///
    /// **This is the weakest evidence in the file, so it is the most gated.** A number in front of
    /// a row is something prose does not write; indented text under a caret is something prose
    /// writes constantly. Two independent facts are required before a run of aligned lines becomes
    /// a question somebody's phone will offer buttons for:
    ///
    /// - a hook or the session registry saying this session is waiting, and
    /// - a frame — a rule or a checkbox header — above the rows, which every captured dialog has.
    ///
    /// An indented caret alone is enough for a *numbered* menu and deliberately is not here.
    ///
    /// What keeps the remaining risk small is on the other side: answering walks the highlight and
    /// then reads the screen back before it confirms — see ``Targets/answer(_:to:)``. A run of
    /// prose misread as a dialog cannot move a highlight, so no Return is ever sent and the worst
    /// case is a button that does nothing, not an answer nobody chose.
    private static func plainMenu(_ tail: [(offset: Int, element: String)], in lines: [String],
                                  hookWaiting: Bool) -> Menu? {
        guard hookWaiting else { return nil }
        let text = tail.map { $0.element }

        // The caret nearest the bottom that is not at the left margin. Flush left is the composer,
        // and without a number to argue otherwise that rule is stricter here, not looser: the
        // numbered path has a gate that lets a flush-left caret through for `AskUserQuestion`, and
        // there is no equivalent here on purpose. `❯ what somebody typed` with a wrapped second
        // line under it is character for character the shape this would be looking for.
        guard let anchor = text.lastIndex(where: { line in
            guard let row = plainRow(line) else { return false }
            return row.caret && row.indent > 0
        }), let head = plainRow(text[anchor]) else { return nil }

        // The frame. Scanning up from the caret rather than from the top of the tail, so a rule
        // drawn *below* the dialog — the composer's own — cannot pass for the dialog's lid.
        var dialogStart = 0
        var scan = anchor - 1
        while scan >= 0 {
            if isBoxRule(text[scan]) || isQuestionHeader(dialogText(text[scan])) {
                dialogStart = scan + 1
                break
            }
            scan -= 1
        }
        guard dialogStart > 0 else { return nil }

        // Outward from the caret, keeping the lines that start in the caret's own column.
        //
        // **The rows are not necessarily adjacent.** A dialog that describes its options puts the
        // description between them, indented two columns further in, so a walk that stopped at the
        // first line which was not a row found one option and gave up — which is what a fixture
        // with descriptions in it caught. Deeper lines are stepped over; a shallower one is the
        // prose above the dialog or the hint below it, and that is the edge.
        var rows = [anchor]
        var scanUp = anchor - 1
        while scanUp >= dialogStart, let row = plainRow(text[scanUp]), row.column >= head.column {
            if row.caret { break }
            if row.column == head.column { rows.insert(scanUp, at: 0) }
            scanUp -= 1
        }
        var scanDown = anchor + 1
        while scanDown < text.count, let row = plainRow(text[scanDown]),
              row.column >= head.column {
            if row.caret { break }
            if row.column == head.column { rows.append(scanDown) }
            scanDown += 1
        }
        guard rows.count >= 2 else { return nil }

        var options: [Menu.Option] = []
        for index in rows {
            guard let row = plainRow(text[index]) else { continue }
            options.append(Menu.Option(number: options.count + 1, label: row.label,
                                       detail: plainDetail(under: tail[index].offset,
                                                           deeperThan: head.column, in: lines),
                                       selected: index == anchor))
        }

        let menuQuestion = Self.question(in: lines, before: tail[rows[0]].offset,
                                         noEarlierThan: tail.first?.offset ?? 0)
        Log.write("choosing (unnumbered): options=\(options.count) — "
                  + options.map { $0.label.prefix(60) }.joined(separator: " ⏐ "))
        return Menu(question: menuQuestion, options: options,
                    selected: options.first(where: \.selected)?.number, numbered: false)
    }

    /// The button a multi-select draws under its rows, if this dialog is one.
    ///
    /// **A multi-select is told apart by its checkboxes, not by its button.** The two dialogs draw
    /// their descriptions at different indents — a multi-select puts them two columns in, an
    /// ordinary `AskUserQuestion` puts them level with the labels — so a rule that looked only at
    /// the column would turn the first description of every single-select into a Submit nobody
    /// could press. What only a multi-select has is a `[ ]` in front of every row, and two of them
    /// are required for the same reason two options are: one of anything is not a pattern.
    ///
    /// Given that, the button is the line that is **not** a numbered row, **not** a checkbox row,
    /// and starts in the same column as the labels. It is drawn as a pointer cell, a gap and three
    /// spaces, which lands on exactly the column an option's label lands on — while a description
    /// lands short of it. That alignment holds up to nine options, which is also as far as a
    /// keystroke reaches, so nothing is lost where it stops holding.
    private static func submitRow(in tail: [(offset: Int, element: String)], from dialogStart: Int)
        -> (row: Int, line: Int, label: String, selected: Bool)? {
        var column: Int? = nil
        var checkboxes = 0
        for (index, captured) in tail.enumerated() where index >= dialogStart {
            guard let row = option(captured.element) else { continue }
            if column == nil { column = row.column }
            if isCheckbox(row.label) { checkboxes += 1 }
        }
        guard checkboxes >= 2, let labelColumn = column else { return nil }

        for (index, captured) in tail.enumerated() where index >= dialogStart {
            let line = captured.element
            guard option(line) == nil, !isBoxRule(line), let row = plainRow(line),
                  row.column == labelColumn, !isCheckbox(row.label),
                  !isQuestionHeader(row.label) else { continue }
            return (index, captured.offset, row.label, row.caret)
        }
        return nil
    }

    /// A row's label with its box taken off, and what the box said.
    ///
    /// The glyph is part of the drawing, not part of the answer — the same argument that takes the
    /// wall of a dialog off a label. Left on, it reaches a phone as the literal characters
    /// `[\u{2714}] Docs`, which cannot be drawn as a tick, coloured, or read aloud as one.
    private static func checkbox(of label: String) -> (label: String, checked: Bool?) {
        guard isCheckbox(label) else { return (label, nil) }
        let chars = Array(label)
        if "\u{2610}\u{2611}\u{2612}".contains(chars[0]) {
            let rest = String(chars.dropFirst()).trimmingCharacters(in: .whitespaces)
            return (rest.isEmpty ? label : rest, chars[0] != "\u{2610}")
        }
        let rest = String(chars.dropFirst(3)).trimmingCharacters(in: .whitespaces)
        return (rest.isEmpty ? label : rest, chars[1] != " ")
    }

    /// Whether this label begins with the box a multi-select puts in front of every row. Claude
    /// Code draws it in ASCII — `[ ]` and `[✔]` — while the tab bar above uses `☐`; both are
    /// admitted because both have been seen in a capture and neither costs anything to allow.
    private static func isCheckbox(_ label: String) -> Bool {
        if let first = label.first, "☐☑☒".contains(first) { return true }
        let chars = Array(label.prefix(3))
        return chars.count == 3 && chars[0] == "[" && chars[2] == "]"
    }

    /// The rows drawn under one row of an unnumbered picker.
    ///
    /// ``detail(under:in:)`` cannot be reused: it stops at the next *numbered* row, and there are
    /// none here, so it would read the following option's label as this one's description. What
    /// separates them is the indent — a description is drawn two columns further in than the
    /// labels — so that is what this stops on.
    private static func plainDetail(under optionIndex: Int, deeperThan column: Int,
                                    in lines: [String]) -> String? {
        var parts: [String] = []
        var index = optionIndex + 1
        while index < lines.count {
            guard let row = plainRow(lines[index]), row.column > column, !row.caret,
                  !isBoxRule(lines[index]) else { break }
            parts.append(row.label)
            index += 1
        }
        let joined = parts.joined(separator: " ").trimmingCharacters(in: .whitespacesAndNewlines)
        return joined.isEmpty ? nil : joined
    }

    /// This line as a row of a picker that prints no numbers: how far in the row itself is drawn,
    /// where its label starts, what the label says, and whether the pointer is on it.
    ///
    /// The two columns are measured **after** the padding and the wall of the box, so a dialog
    /// drawn inside `│ … │` reports the same numbers as one drawn with plain indentation. The
    /// pointer occupies one cell and the gap after it another, which is why a marked row and an
    /// unmarked one put their labels in the same column — take that away and there is nothing left
    /// to group them by. `indent` is the other half: it is where the *caret* sat, which is what
    /// says whether this is a dialog's row or the line somebody is typing on.
    ///
    /// A scroll arrow sits in the pointer's cell when a list is longer than the window. It is not
    /// the caret and is not reported as one, but the row is still a row.
    private static func plainRow(_ raw: String)
        -> (indent: Int, column: Int, label: String, caret: Bool)? {
        let chars = Array(raw)
        var i = 0
        while i < chars.count, chars[i] == " " || chars[i] == "\t" || boxes.contains(chars[i]) {
            i += 1
        }
        guard i < chars.count else { return nil }
        let indent = i
        var caret = false
        if carets.contains(chars[i]) || scrollArrows.contains(chars[i]) {
            caret = carets.contains(chars[i])
            i += 1
            guard i < chars.count, chars[i] == " " else { return nil }
            while i < chars.count, chars[i] == " " { i += 1 }
            guard i < chars.count else { return nil }
        }
        var label = String(chars[i...])
        while let last = label.last, last == " " || last == "\t" || boxes.contains(last) {
            label.removeLast()
        }
        guard !label.isEmpty else { return nil }
        return (indent, i, label, caret)
    }

    /// Whether this line is one Claude Code marks a turn of its own with: an answer it has begun
    /// writing, or the clock on one it is still writing. Both mean the session went on doing
    /// something, which a session stopped in front of a picker cannot have done.
    ///
    /// The glyphs are ``Activity``'s spinner set plus the bullet Claude Code heads an answer with.
    /// Only what sits *below* the rows is asked about: a stale spinner **above** a dialog is an
    /// ordinary sight and is what the ordering in ``read(_:assistant:hookWaiting:)`` exists for.
    private static func isTurnMarker(_ raw: String) -> Bool {
        guard let glyph = raw.trimmingCharacters(in: .whitespaces).first else { return false }
        return turnMarkers.contains(glyph)
    }

    private static let turnMarkers: Set<Character> = [
        "⏺",  // an answer, or a tool call
        "✳", "✻", "✽", "✢", "✶", "✱", "✴", "◐", "◑", "◒", "◓", "◴", "◵", "◶", "◷",
    ]

    /// Drawn in the pointer's cell when a list runs past the window. Not a selection.
    private static let scrollArrows: Set<Character> = ["↓", "↑"]

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
    ///
    /// Except on a dialog that prints no numbers, where there is nothing to renumber and the
    /// position is all there is. ``Menu/numbered`` is which of the two this is, and it is read
    /// rather than assumed: the same flag that takes the numbers off also stops the dialog
    /// accepting digits, so getting it wrong types into a composer instead of answering.
    struct Menu: Equatable {
        struct Option: Equatable {
            /// The number as printed. This is the keystroke.
            let number: Int
            /// Mutable because the screen is not the last word on it: an `AskUserQuestion` row is
            /// refilled from the transcript, which has the words the terminal had no room for.
            var label: String
            /// The rows drawn under the label, joined. `AskUserQuestion` puts the consequence of
            /// each answer there — which models get dropped, what it costs, what breaks — and a
            /// label without it is often not enough to choose on: "cut the slow five" does not
            /// say which five. Empty when the dialog draws single-line options, as permission
            /// prompts and `/model` do.
            var detail: String? = nil
            /// The caret is parked on this one, so it is what a bare Return would confirm.
            let selected: Bool
            /// Ticked, unticked, or not a thing that ticks.
            ///
            /// A multi-select draws `[ ]` and `[\u{2714}]` in front of every row, and the
            /// words arrived on the phone with the box still glued to the front of them — legible,
            /// but only just, and impossible to draw as anything better than the text it came in.
            /// Read out here so the far end can draw a real one, and so the label is the label.
            var checked: Bool? = nil
            /// Whether a keystroke can carry it — see ``Targets/answer(_:to:)``, which is 1…9.
            var answerable: Bool { (1...9).contains(number) }
        }
        /// The full question, either from structured data or the text immediately above the
        /// numbered rows in a visual capture. It stays nil when that boundary cannot be read.
        var question: String? = nil
        var options: [Option]
        var selected: Int?
        /// Whether the numbers above were printed on screen or counted here.
        ///
        /// Claude Code draws two kinds of picker out of one component, and the difference is a
        /// `hideIndexes` flag. Numbered is the common one. Unnumbered is the held cross-session
        /// message, the plan-approval dialog, and a handful of settings screens — and the same
        /// flag that hides the numbers **turns numeric selection off**, so the digit that answers
        /// every other dialog falls straight through this one into the composer.
        ///
        /// So this is not decoration: it is the fact ``Targets/answer(_:to:)`` needs in order to
        /// answer by moving the highlight instead of by typing. The numbers are still filled in
        /// by position, because a row still has to be nameable from a phone.
        var numbered: Bool = true

        /// The button a multi-select draws under its rows, when there is one.
        ///
        /// A multi-select is a different question from the rest: its digits **toggle** rather than
        /// answer, and nothing is sent until the button below the rows is pressed. That button has
        /// no number on screen — it is not one of the options — so it cannot be named the way the
        /// rows are, and until it was given a place of its own it was read as the last row's
        /// description and there was nothing on the phone to press.
        struct Submit: Equatable {
            let label: String
            /// The caret is parked on the button rather than on a row, so Return would send.
            let selected: Bool
        }
        var submit: Submit? = nil

        /// One question of several, as the picker's own tab bar names it.
        ///
        /// `AskUserQuestion` can ask a set at once and Claude Code presents them one at a time,
        /// so without this a reader answers, watches a different question take its place, and has
        /// no way to tell how many there are or whether the last answer landed. The terminal draws
        /// the whole set above the options — `←  ☒ scope  ☐ parity  ✔ Submit  →` — and this is
        /// that row, read rather than skipped. See ``QuestionSteps``.
        struct Step: Equatable {
            let label: String
            let answered: Bool
            /// What was chosen, once the picker's own review screen says so. Absent until then:
            /// the tab bar carries whether a question is answered, never with what.
            var answer: String? = nil
        }

        /// Empty for a lone question, so "one question" stays distinguishable from "the first of
        /// four" without a client having to infer it from a count of one.
        var steps: [Step] = []
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
    /// A label with whatever was drawn beside it taken off.
    ///
    /// A terminal has one grid, so a diagram the assistant printed sits in the same rows as the
    /// dialog rather than behind it: `1. Stop only when unsure    ┌──────────────▶ Whisper ─┐`
    /// is one line, and the half after the gap belongs to a picture. Two or more spaces followed
    /// by a box-drawing character is that seam — prose does not put a frame corner a column after
    /// a run of spaces, and a label that genuinely contains one has it without the gap.
    private static func withoutSidePanel(_ label: String) -> String {
        let chars = Array(label)
        var gap = 0
        for (index, char) in chars.enumerated() {
            if char == " " { gap += 1; continue }
            if gap >= 2, boxes.contains(char) || horizontalRules.contains(char)
                || boxJoints.contains(char) {
                return String(chars[0..<(index - gap)])
                    .trimmingCharacters(in: .whitespaces)
            }
            gap = 0
        }
        return label
    }

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
    ///
    /// `stoppingAt` is the one line that looks like prose and is not: a multi-select's Submit.
    /// It is found before any of this runs, because there is nothing about the line itself that
    /// this walk could use to tell it from a description — see ``submitRow(in:from:)``.
    ///
    /// **A checkbox is a boundary going up and nothing at all going down.** ``question(in:...)``
    /// stops at one because the tab bar sits above the question; the tab bar cannot also sit under
    /// a row, so the same test here only ever fired on a description that mentioned a checkbox —
    /// and one did, in a real capture, and its row lost its explanation.
    private static func detail(under optionIndex: Int, in lines: [String],
                               stoppingAt submitLine: Int? = nil) -> String? {
        var parts: [String] = []
        var index = optionIndex + 1
        while index < lines.count {
            if index == submitLine { break }
            let raw = lines[index]
            if option(raw) != nil || isBoxRule(raw) || hasCaret(raw) { break }
            let text = dialogText(raw)
            if text.isEmpty { break }
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
    private static let horizontalRules: Set<Character> = ["─", "━", "═", "╌", "╍", "┄", "┅", "┈", "┉"]
    private static let boxJoints: Set<Character> = ["╭", "╮", "╰", "╯", "┌", "┐", "└", "┘", "├", "┤",
                                                    "┬", "┴", "┼", "╞", "╡", "╪", "┏", "┓", "┗", "┛"]

    private static func isBoxRule(_ raw: String) -> Bool {
        let horizontal = horizontalRules
        let joints = boxJoints
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
        -> (number: Int, label: String, caret: Bool, indented: Bool, column: Int)? {
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
        // `column` is where the words start, which is what a multi-select's button lines up with.
        return (number, label, caret, indented, i)
    }
}
