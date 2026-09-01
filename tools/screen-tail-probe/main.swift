import Foundation

var failures = 0
func check(_ name: String, _ condition: Bool, _ detail: @autoclosure () -> String = "") {
    if condition { print("  ok   \(name)") }
    else { failures += 1; print("  FAIL \(name) \(detail())") }
}

let composer = String(repeating: "\u{2500}", count: 80)

// 1. A screen is stripped down to the conversation.
let screen = """
  Claude said something worth keeping.

⏺ Bash(ls -la)
  \u{23BF}  a listing
  Running 1 shell command · 4s…
✻ Thinking… (2m 1s · ↓ 7.6k tokens · thinking more)
\(composer)
❯
\(composer)
  ▀▀▀▀ clawdline  a project
"""
let region = ScreenTail.region(of: screen)
check("composer and everything under it is gone", !region.contains { $0.contains("clawdline") })
check("the elapsed counter is gone", !region.contains { $0.contains("Running 1 shell command") })
check("the spinner is gone", !region.contains { $0.contains("Thinking…") })
check("the words survive", region.contains { $0.contains("worth keeping") })

// 2. Overlapping frames become one document, without repeating the overlap.
let a = ["one", "two", "three", "four"]
let b = ["three", "four", "five", "six"]
let joined = ScreenTail.reconcile(a, with: b)
check("overlap is not repeated", joined == ["one", "two", "three", "four", "five", "six"],
      "got \(joined)")

// 3. A frame that cannot be placed is recorded as a break, not spliced.
let unrelated = ScreenTail.reconcile(["one", "two"], with: ["nine", "ten"])
check("an unplaceable frame records a gap", unrelated.contains(ScreenTail.gapMarker),
      "got \(unrelated)")

// 4. A single shared line is not enough to align on.
let weak = ScreenTail.reconcile(["alpha", "blank"], with: ["blank", "zulu"])
check("one shared line does not align", weak.contains(ScreenTail.gapMarker), "got \(weak)")

// 5. The prose before a picker is what a reader is missing.
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
check("prose is found", prose != nil)
check("the wrapped paragraph is rejoined",
      prose?.contains("guardrail, and the document that described them") == true, "got \(prose ?? "nil")")
check("both paragraphs survive", prose?.contains("which one to cut first") == true)
check("the tool call is not in it", prose?.contains("git status") == false)
check("the picker is not in it", prose?.contains("Extract the state owner") == false)

// 6. Nothing before a break is offered.
let broken = [
    "  Words from before the gap.",
    ScreenTail.gapMarker,
    "  Words from after it.",
]
let afterGap = ScreenTail.trailingProse(of: broken)
check("prose before a gap is refused", afterGap?.contains("before the gap") == false,
      "got \(afterGap ?? "nil")")
check("prose after a gap is kept", afterGap?.contains("after it") == true)

// 7. A screen whose tail is a tool call has nothing to offer.
check("a tool tail yields nothing",
      ScreenTail.trailingProse(of: ["  said something", "\u{23FA} Bash(ls)"]) == nil)

// 8. English keeps its space across a wrap; CJK does not gain one.
let english = ScreenTail.trailingProse(of: ["  the quick brown", "  fox jumps"])
check("an ASCII seam keeps its space", english == "the quick brown fox jumps", "got \(english ?? "nil")")
let chinese = ScreenTail.trailingProse(of: ["  下一輪重構的主要", "  目標是什麼"])
check("a CJK seam gains no space", chinese == "下一輪重構的主要目標是什麼", "got \(chinese ?? "nil")")

// 8b. A line that stopped well short of the edge was broken by its author, not the terminal.
let listing = ScreenTail.trailingProse(of: [
    "  SessionClosePolicy.swift    118",
    "  ReadingFreshness.swift      348",
    "  a line long enough to be the width this screen was actually drawn at, and then some more",
], width: 88)
check("short lines are not glued together",
      listing?.contains("SessionClosePolicy.swift    118  \nReadingFreshness.swift      348") == true,
      "got \(listing ?? "nil")")

// 8c. A table's borders do not throw away the answer above them.
let withTable = ScreenTail.trailingProse(of: [
    "  The numbers in the document are stale.",
    "  \u{250C}\u{2500}\u{2500}\u{2510}",
    "  \u{2502} 38 \u{2502}",
    "  \u{2514}\u{2500}\u{2500}\u{2518}",
    "",
    "  So which one do we cut first?",
])
check("a table does not eat the prose above it",
      withTable?.contains("stale") == true && withTable?.contains("cut first") == true,
      "got \(withTable ?? "nil")")

// 9. Real captures: sixty consecutive screens of a live session reconcile without a break.
let framesDir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ""
if !framesDir.isEmpty,
   let names = try? FileManager.default.contentsOfDirectory(atPath: framesDir).sorted() {
    var doc: [String] = []
    var used = 0
    for name in names where name.hasSuffix(".txt") {
        guard let text = try? String(contentsOfFile: framesDir + "/" + name, encoding: .utf8),
              !text.isEmpty else { continue }
        let frame = ScreenTail.region(of: text)
        guard !frame.isEmpty else { continue }
        used += 1
        doc = ScreenTail.reconcile(doc, with: frame)
    }
    let gaps = doc.filter { $0 == ScreenTail.gapMarker }.count
    print("  --- \(used) real captures reconciled into \(doc.count) lines, \(gaps) gaps")
    check("real captures align with no break", gaps == 0)
    check("real captures recover more than one screen holds", doc.count > 60, "got \(doc.count)")
}

print(failures == 0 ? "\nall checks passed" : "\n\(failures) check(s) failed")
exit(failures == 0 ? 0 : 1)
