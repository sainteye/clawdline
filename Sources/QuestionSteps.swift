import Foundation

/// A multi-question picker's own progress bar, and the projection of a menu onto the wire.
///
/// **The problem this answers.** `AskUserQuestion` can ask several questions in one call, and
/// Claude Code presents them one at a time: answer the first and the second takes its place. A
/// reader on a phone was handed each of those in turn with nothing saying there were others —
/// how many there are, which have been answered, whether anything is left. Two questions in and
/// the honest report was "I answered something and now I am looking at a different question and
/// I cannot tell whether the first one took."
///
/// **The terminal already says all of it.** Above the options Claude Code draws a tab bar:
///
///     ←  ☒ 算到哪層  ☒ 填格權限  ☐ 表格結構  ☐ 事後對帳  ✔ Submit  →
///
/// `☒` for answered, `☐` for not, in order, with the whole set named. `SessionState` has known
/// about this row since multi-select landed — but only as a *boundary*, something to stop at when
/// looking for the question. It was read and thrown away. This reads it.
///
/// **What it does not claim.** Which tab is focused. The bar marks that by drawing it differently
/// rather than with a character, and the parse works on text with the styling already taken off.
/// Answered-or-not is the fact a reader needs, and it is the one the glyphs actually carry.
enum QuestionSteps {

    /// The `☐`/`☑`/`☒` glyphs Claude Code marks a question's state with. `☐` is open; anything
    /// else in this set is answered.
    private static let boxes: Set<Character> = ["\u{2610}", "\u{2611}", "\u{2612}"]

    /// Rows that end a tab bar's last label rather than belonging to it: the Submit button and
    /// the arrow that says there is more to the right.
    private static let terminators: Set<Character> = ["\u{2714}", "\u{2713}", "\u{2192}"]

    /// The steps a tab bar names, in the order it draws them.
    ///
    /// Returns empty for a line that is not one, which includes the single-question header — one
    /// question draws `☐ build` and there is no progress to report about a set of one.
    static func steps(in line: String) -> [SessionState.Menu.Step] {
        var out: [SessionState.Menu.Step] = []
        var open: (answered: Bool, label: String)?

        func close() {
            guard let current = open else { return }
            let label = current.label.trimmingCharacters(in: .whitespaces)
            if !label.isEmpty { out.append(.init(label: label, answered: current.answered)) }
            open = nil
        }

        for character in line {
            if boxes.contains(character) {
                close()
                open = (answered: character != "\u{2610}", label: "")
            } else if terminators.contains(character) {
                close()
            } else if open != nil {
                open?.label.append(character)
            }
        }
        close()
        // One box is a lone question's own label, not a set to report progress through.
        return out.count >= 2 ? out : []
    }

    /// The answers a review screen lists, in the order it lists them.
    ///
    /// After the last question Claude Code does not submit — it draws a review:
    ///
    ///     Review your answers
    ///      ● 最想去哪一個城市旅行？
    ///        → 京都
    ///      ● 平常想做哪一種運動?
    ///        → 游泳
    ///     Ready to submit your answers?
    ///     ❯ 1. Submit answers    2. Cancel
    ///
    /// **That screen is the whole point of reading this.** It is the one moment where a reader
    /// can check what they actually chose before it is sent, and on a phone it was arriving as
    /// two unlabelled buttons under the words "Ready to submit your answers?" — a confirmation
    /// with nothing to confirm.
    ///
    /// The bullet comes first and its answer follows on the next line; the tab bar's own `→` is
    /// at the end of a line rather than the start of one, so it is never mistaken for an answer.
    static func answers(inLines lines: [String]) -> [String] {
        var out: [String] = []
        var awaiting = false
        for line in lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard let first = trimmed.first else { continue }
            if first == "\u{25CF}" { awaiting = true; continue }
            guard awaiting, first == "\u{2192}" else { continue }
            let answer = trimmed.dropFirst().trimmingCharacters(in: .whitespaces)
            if !answer.isEmpty { out.append(answer) }
            awaiting = false
        }
        return out
    }

    /// The tab bar above a menu, if the screen drew one.
    ///
    /// Searched upwards from the first option, and only a little way: the bar sits directly above
    /// the question, and a checkbox found further up belongs to something else — an earlier
    /// dialog in the scrollback, or prose that happens to contain one.
    static func steps(inLines lines: [String], above firstOption: Int) -> [SessionState.Menu.Step] {
        let floor = max(0, firstOption - 12)
        for index in stride(from: firstOption - 1, through: floor, by: -1) {
            let found = steps(in: lines[index])
            guard !found.isEmpty else { continue }
            // Paired by position rather than by text: the bar's label is a short tag ("城市") and
            // the review lists the whole question ("最想去哪一個城市旅行？"). They are the same
            // set in the same order, and a count that does not match is a screen this does not
            // understand — so it says nothing rather than pairing the wrong answer to a question.
            let answers = answers(inLines: lines)
            guard answers.count == found.count else { return found }
            return zip(found, answers).map {
                .init(label: $0.label, answered: $0.answered, answer: $1)
            }
        }
        return []
    }
}

extension RemoteServer {

    /// A menu as rows a finger can hit.
    ///
    /// **`n` is the keystroke and not the position**, which is the only part of this worth being
    /// careful about: the page sends that number straight to `/key`, and renumbering the rows
    /// here to make them tidy would produce buttons that answer a different question than the one
    /// they are labelled with. `can` is false for a row no keystroke reaches — it is drawn, and it
    /// is not offered, because a button that cannot work is worse than a line of text.
    static func menuObject(_ menu: SessionState.Menu) -> [String: Any] {
        var out: [String: Any] = [
            "options": menu.options.map { option -> [String: Any] in
                var row: [String: Any] = ["n": option.number, "label": option.label,
                                          "selected": option.selected, "can": option.answerable]
                if let detail = option.detail { row["detail"] = detail }
                // Only on a multi-select, where a row is a thing that ticks rather than a thing
                // that answers. Absent everywhere else, so a client can tell the two apart.
                if let checked = option.checked { row["checked"] = checked }
                return row
            },
        ]
        if let selected = menu.selected { out["selected"] = selected }
        if let question = menu.question { out["question"] = question }
        // The button under a multi-select's rows, which is a different act from picking one of
        // them: the rows toggle, and only this sends. It carries no `n` because it has none on
        // screen — `POST /key` takes the word `submit` for it.
        if let submit = menu.submit {
            out["submit"] = ["label": submit.label, "selected": submit.selected]
        }
        // Where this question sits in a set of them. Absent for a lone question, so a client can
        // tell "one question" from "the first of four" instead of having to infer it.
        if !menu.steps.isEmpty {
            out["steps"] = menu.steps.map { step -> [String: Any] in
                var row: [String: Any] = ["label": step.label, "done": step.answered]
                if let answer = step.answer { row["answer"] = answer }
                return row
            }
        }
        return out
    }

    /// Stable content for the legacy session revision field. Separators prevent different
    /// question/option boundaries from collapsing to the same string.
    ///
    /// The steps are in it because answering one question of several changes the bar and nothing
    /// else on screen — without them the revision is unchanged and a client that trusts it never
    /// learns that its answer landed.
    static func menuRevision(_ menu: SessionState.Menu) -> String {
        ([menu.question ?? "", menu.submit.map { "\($0.label)\u{1f}\($0.selected ? 1 : 0)" } ?? ""]
         + menu.options.map {
            "\($0.number)\u{1f}\($0.label)\u{1f}\($0.selected ? 1 : 0)"
         }
         + menu.steps.map { "\($0.label)\u{1f}\($0.answered ? 1 : 0)\u{1f}\($0.answer ?? "")" }
        ).joined(separator: "\u{1e}")
    }
}
