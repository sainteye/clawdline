import Foundation

/// What a session has said that its transcript file has not written down, and the sampling that
/// makes reconstructing it possible. Nothing here asks a terminal anything: screens are literals
/// and the follower's captures come through its test seam, so the whole suite is deterministic
/// and none of it sleeps.
func runScreenTailTests() {
    let composerRule = String(repeating: "\u{2500}", count: 80)

    group("a screen is stripped down to the conversation") {
        let screen = """
          Claude said something worth keeping.

        \u{23FA} Bash(ls -la)
          \u{23BF}  a listing
          Running 1 shell command · 4s…
        ✻ Thinking… (2m 1s · ↓ 7.6k tokens · thinking more)
        \(composerRule)
        ❯
        \(composerRule)
          ▀▀▀▀ clawdline  a project
        """
        let region = ScreenTail.region(of: screen)
        check("the composer and everything under it is gone",
              !region.contains { $0.contains("clawdline") })
        // The elapsed counter and the spinner are redrawn several times a second. They are why a
        // naive reconciliation fails: with them in, no two captures ever match.
        check("the elapsed counter is gone", !region.contains { $0.contains("Running 1 shell") })
        check("the spinner is gone", !region.contains { $0.contains("Thinking…") })
        check("the words survive", region.contains { $0.contains("worth keeping") })
    }

    group("overlapping captures become one document, and a break is recorded not guessed") {
        let joined = ScreenTail.reconcile(["one", "two", "three", "four"],
                                          with: ["three", "four", "five", "six"])
        expect("the overlap is not repeated", joined, ["one", "two", "three", "four", "five", "six"])

        let unplaceable = ScreenTail.reconcile(["one", "two"], with: ["nine", "ten"])
        check("a capture that cannot be placed records a gap",
              unplaceable.contains(ScreenTail.gapMarker), "got \(unplaceable)")

        // One shared line is regularly a blank or a repeated prompt; aligning on it puts the rest
        // of the capture in the wrong place, which reads as ordinary prose and is wrong.
        let weak = ScreenTail.reconcile(["alpha", "blank"], with: ["blank", "zulu"])
        check("one shared line is not enough to align on",
              weak.contains(ScreenTail.gapMarker), "got \(weak)")

        // A running tool rewrites its own row once a second, changing only the counter. Compared
        // literally each rewrite is a line never seen before, and an appending reconciliation
        // stacks one copy per second — forty of them reached a phone before this existed.
        var live = ["  a real sentence", "  return text.isEmpt… (3s · 3 lines)"]
        for second in 4...9 {
            live = ScreenTail.reconcile(live, with: ["  a real sentence",
                                                     "  return text.isEmpt… (\(second)s · 3 lines)"])
        }
        check("a redrawn counter row is not stacked up",
              live.filter { $0.contains("isEmpt") }.count == 1, "got \(live)")
        check("the sentence beside it is untouched",
              live.filter { $0.contains("a real sentence") }.count == 1)
        check("the row a redraw replaced is not left behind in the document",
              ScreenTail.region(of: "  words\n  return text.isEmpt… (3s · 3 lines)\n")
                .contains { $0.contains("isEmpt") } == false)

        // Three captures of a screen scrolling two lines at a time, which is the real shape.
        var document: [String] = []
        for start in 0..<3 {
            let frame = (start..<(start + 4)).map { "line \($0)" }
            document = ScreenTail.reconcile(document, with: frame)
        }
        expect("a scrolling screen rebuilds in order", document,
               ["line 0", "line 1", "line 2", "line 3", "line 4", "line 5"])
        check("and nothing is duplicated", Set(document).count == document.count)
    }

    group("the words a reader is missing, and the ones they already have") {
        let document = [
            "\u{23FA} Bash(git status)",
            "  \u{23BF}  seventeen files",
            "",
            "  The five biggest files are over the guardrail, and the document that",
            "  described them is out of date.",
            "",
            "  So the next question is which one to cut first.",
            "",
            "  \u{2610} refactor target",
            "  Which target should the next pass take?",
            "  \u{276F} 1. Extract the state owner",
        ]
        let prose = ScreenTail.trailingProse(of: document)
        check("the prose leading up to the question is found", prose != nil)
        check("both paragraphs survive", prose?.contains("which one to cut first") == true)
        check("the tool call that ended the last written turn is not in it",
              prose?.contains("git status") == false)
        // The question and its options are already in front of the reader as a card. Repeating
        // them as prose is noise, and the option rows are not sentences at all.
        check("the picker's own words are not in it",
              prose?.contains("Extract the state owner") == false
                && prose?.contains("Which target should") == false)

        check("a screen whose tail is a tool call offers nothing",
              ScreenTail.trailingProse(of: ["  said something", "\u{23FA} Bash(ls)"]) == nil)

        let broken = ["  Words from before the gap.", ScreenTail.gapMarker, "  Words after it."]
        let afterGap = ScreenTail.trailingProse(of: broken)
        check("nothing before a break is offered", afterGap?.contains("before the gap") == false,
              "got \(afterGap ?? "nil")")
        check("what follows a break still is", afterGap?.contains("after it") == true)

        // A bordered table was briefly treated as the picker's furniture. Against a real waiting
        // screen that discarded the entire answer above the table.
        let withTable = ScreenTail.trailingProse(of: [
            "  The numbers in the document are stale.",
            "  \u{250C}\u{2500}\u{2500}\u{2510}", "  \u{2502} 38 \u{2502}", "  \u{2514}\u{2500}\u{2500}\u{2518}",
            "",
            "  So which one do we cut first?",
        ])
        check("a table does not eat the prose above it",
              withTable?.contains("stale") == true && withTable?.contains("cut first") == true,
              "got \(withTable ?? "nil")")
    }

    group("a terminal's wrap is undone and an author's own break is not") {
        // Joined: the first line ran to the width this screen was drawn at.
        expect("an ASCII seam keeps the space the wrap ate",
               ScreenTail.trailingProse(of: ["  the quick brown", "  fox jumps"]),
               "the quick brown fox jumps")
        expect("a CJK seam gains no space",
               ScreenTail.trailingProse(of: ["  下一輪重構的主要", "  目標是什麼"]),
               "下一輪重構的主要目標是什麼")
        check("CJK counts as two columns", ScreenTail.displayWidth("下一輪") == 6)

        // Not joined: these stopped well short of the edge, so the breaks are the author's. The
        // hard break is Markdown's, because the reader renders Markdown and a bare newline there
        // is a wrapped line.
        let listing = ScreenTail.trailingProse(of: [
            "  SessionClosePolicy.swift    118",
            "  ReadingFreshness.swift      348",
            "  a line long enough to be the width this screen was actually drawn at, and more",
        ], width: 88)
        check("short lines are not glued together",
              listing?.contains("SessionClosePolicy.swift    118  \nReadingFreshness.swift      348") == true,
              "got \(listing ?? "nil")")
    }

    group("a paragraph the file already holds is not said twice") {
        // Markdown on one side, rendered text on the other. The first real screen this met put
        // the whole answer on the page a second time, because a comparison that only ignored
        // whitespace could not see past a pair of backticks.
        let entries: [[String: Any]] = [
            ["role": "assistant", "text": "`zh_TW` \u{2192} the field count is **4**, not 5."],
            ["role": "tool", "text": "ran 17 shell commands"],
        ]
        let prose = """
        zh_TW \u{2192} the field count is 4, not 5.

        And this part has not been written down yet.
        """
        let fresh = RemoteServer.unsyncedText(in: prose, alreadyIn: entries)
        check("the paragraph the file has is dropped despite its Markdown",
              fresh?.contains("field count") == false, "got \(fresh ?? "nil")")
        check("the paragraph it does not have survives",
              fresh?.contains("not been written down") == true, "got \(fresh ?? "nil")")

        // Whole-row suppression used to answer "nothing" here, which is the other half of the
        // same defect: the reader saw a question with none of the reasoning under it.
        check("a reading the file has entirely offers nothing",
              RemoteServer.unsyncedText(in: "zh_TW \u{2192} the field count is 4, not 5.",
                                        alreadyIn: entries) == nil)
        check("punctuation alone is never a paragraph",
              RemoteServer.unsyncedText(in: "---", alreadyIn: entries) == nil)
    }

    group("a session is sampled closely only while somebody is reading it") {
        let follow = ScreenFollow.shared
        follow.forgetForTesting()
        defer {
            follow.forgetForTesting()
            ScreenFollow.captureForTesting = nil
            ScreenFollow.targetsForTesting = nil
            ScreenTail.forgetAllForTesting()
        }

        let watched = TargetSession(backend: .iterm, id: "WATCHED", name: "watched",
                                    tty: "/dev/ttys090", windowIndex: 0, tabIndex: 0,
                                    assistant: .claude, cwd: "/tmp")
        let ignored = TargetSession(backend: .iterm, id: "IGNORED", name: "ignored",
                                    tty: "/dev/ttys091", windowIndex: 0, tabIndex: 1,
                                    assistant: .claude, cwd: "/tmp")
        ScreenFollow.targetsForTesting = { [watched, ignored] }
        var captured: [String] = []
        ScreenFollow.captureForTesting = { session in
            captured.append(session.id)
            return "  a sentence from \(session.id)\n"
        }

        check("nobody is followed before anybody reads", follow.followed().isEmpty)
        follow.tickForTesting()
        check("and a beat with no readers captures nothing", captured.isEmpty)

        // Opening a session is the moment its screen is wanted. Waiting for the timer's first
        // beat hands the reader the answer from before they asked.
        follow.noteReader(of: watched)
        expect("the first read captures inline", captured, ["WATCHED"])
        check("and it is there to be read at once",
              ScreenTail.unsyncedProse("WATCHED")?.contains("from WATCHED") == true)
        captured = []
        follow.noteReader(of: watched)
        check("a second read does not pay for it again", captured.isEmpty)

        follow.noteReader(of: "WATCHED")
        follow.tickForTesting()
        expect("only the session being read is captured", captured, ["WATCHED"])
        check("its words reach the reconstruction",
              ScreenTail.unsyncedProse("WATCHED")?.contains("from WATCHED") == true)
        check("and the session nobody opened is not sampled at all",
              ScreenTail.unsyncedProse("IGNORED") == nil)

        // Attention expires on its own. A reader who closes the page never says so, so the only
        // honest signal is that they stopped asking.
        check("attention is still live inside the window",
              follow.followed(now: Date().addingTimeInterval(ScreenFollow.attention - 5)) == ["WATCHED"])
        check("and gone past it",
              follow.followed(now: Date().addingTimeInterval(ScreenFollow.attention + 1)).isEmpty)
        captured = []
        follow.tickForTesting()
        check("so the beat after it captures nothing", captured.isEmpty)

        // A reader arrives after an answer was written, not during it. The only screens worth
        // having ready are the ones producing text, and only while somebody is reading anything
        // at all — with the phone closed this whole mechanism is pure cost.
        ScreenFollow.workingForTesting = { ["IGNORED"] }
        follow.forgetForTesting()
        captured = []
        follow.tickForTesting()
        check("with nobody reading, a working session is still not swept", captured.isEmpty)

        follow.noteReader(of: "WATCHED")
        captured = []
        follow.tickForTesting()
        expect("the first beat takes only the session being read", captured, ["WATCHED"])
        captured = []
        follow.tickForTesting()
        check("the sweep beat takes the working session too",
              Set(captured) == Set(["WATCHED", "IGNORED"]), "got \(captured)")
        ScreenFollow.workingForTesting = nil
    }
}
