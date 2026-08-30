import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Markdown

let mdTheme = Markdown.Theme(
    body: NSFont.systemFont(ofSize: 12),
    mono: NSFont.monospacedSystemFont(ofSize: 11, weight: .regular),
    text: .labelColor, dim: .secondaryLabelColor, accent: .systemOrange,
    code: .systemBlue,
    codeBackground: NSColor(white: 0, alpha: 0.2), ruleColor: .tertiaryLabelColor)

func md(_ s: String) -> NSAttributedString { Markdown.render(s, theme: mdTheme) }
func mdText(_ s: String) -> String { md(s).string }
/// Inline code carries narrow no-break spaces for padding and turns its own spaces no-break;
/// both are drawing details, not content.
func mdPlain(_ s: String) -> String {
    mdText(s)
        .replacingOccurrences(of: "\u{202F}", with: "")
        .replacingOccurrences(of: "\u{00A0}", with: " ")
}
func attr(_ a: NSAttributedString, _ key: NSAttributedString.Key, near needle: String) -> Any? {
    guard let r = a.string.range(of: needle) else { return nil }
    return a.attribute(key, at: a.string.distance(from: a.string.startIndex, to: r.lowerBound),
                       effectiveRange: nil)
}









































/// A pasteboard this run alone can see.
///
/// **A named pasteboard is not a local object.** It lives in the pasteboard server and the name is
/// all that identifies it, so two processes asking for the same name are handed the same board —
/// which is what these two groups were doing every time this machine ran two suites at once, and
/// this tree is shared by half a dozen sessions that do exactly that. One run's `clearContents()`
/// then lands between another's write and its read, and the assertion fails holding the *other*
/// run's value. Measured with a fifteen-line probe: alone, a write reads back; two concurrently,
/// one process wrote `written-by-B` and read `written-by-A`.
///
/// It looked like flakiness in the system clipboard and it is not — `NSPasteboard.general` is never
/// touched here, so nobody pressing ⌘C could have caused it. It was one suite reading another's
/// board, at about one run in three, and the failures it produced (`got nil, want …`, `got [], want
/// …`) are the shape a reader gets when somebody else has just cleared what it wrote.
/// A UUID and not the process id, for the reason `isolatedTestStoreDirectory` above already uses
/// one: two live processes cannot share a pid *today*, but that is an operating-system property
/// this file would be leaning on, and the same file solving the same problem two different ways is
/// the next thing somebody trips over. A UUID needs no such argument and costs nothing.
let pasteboardRun = UUID().uuidString
func exclusivePasteboard(_ role: String) -> NSPasteboard {
    NSPasteboard(name: NSPasteboard.Name("dev.sainteye.clawdline.tests.\(role).\(pasteboardRun)"))
}

func runMarkdownTests() {
group("markdown: no syntax reaches the screen") {
    // The bug this whole renderer exists for: punctuation showing where structure was meant.
    let doc = #"""
    ## Three guards

    - **undocumented**, so every field is optional
    - *emphasis* and `code` and ~~struck~~
    - a [link](https://example.com)

    1. first
    2. second

    > a quoted aside

    ```swift
    let x = 1
    ```

    | a | b |
    |---|---|
    | 1 | 2 |

    ---
    done
    """#
    let out = mdText(doc)
    check("no heading hashes", !out.contains("## "))
    check("no bold markers", !out.contains("**"))
    check("no code fences", !out.contains("```"))
    check("no backticks", !out.contains("`"))
    check("no strikethrough markers", !out.contains("~~"))
    check("no link brackets", !out.contains("]("))
    check("no leading dash bullets", !out.contains("- **"))
    // A table's separator row legitimately contains dashes and is kept, dimmed, as the
    // table's own rule. What must not survive is a line that was only ever a horizontal rule.
    check("a standalone rule is not left as hyphens",
          !out.split(separator: "\n").contains { $0.trimmingCharacters(in: .whitespaces) == "---" })

    // Losing text is far worse than leaving a marker, so check the words survived.
    for word in ["Three guards", "undocumented", "emphasis", "code", "struck", "link",
                 "first", "second", "quoted aside", "let x = 1", "done"] {
        check("kept: \(word)", out.contains(word))
    }
    check("table cells survive", out.contains("a") && out.contains("b"))
}

group("markdown: headings") {
    let a = md("# Big\n\nbody")
    let f = attr(a, .font, near: "Big") as? NSFont
    check("a heading is larger than the body", (f?.pointSize ?? 0) > mdTheme.body.pointSize)
    expect("the text is clean", mdText("### Small"), "Small\n")
    expect("a hash without a space is not a heading", mdText("#nothash"), "#nothash\n")
    expect("seven hashes is not a heading", mdText("####### x").contains("#"), true)
}

group("markdown: emphasis") {
    let bold = md("a **strong** b")
    let bf = attr(bold, .font, near: "strong") as? NSFont
    let plain = attr(bold, .font, near: "a ") as? NSFont
    check("bold uses a different face", bf != plain)
    expect("markers are gone", bold.string, "a strong b\n")

    let it = md("a *soft* b")
    check("italic uses a different face", (attr(it, .font, near: "soft") as? NSFont) != plain)

    // snake_case is far more common than emphasis in this content.
    expect("underscores inside a word are left alone", mdText("call some_var_name now"),
           "call some_var_name now\n")
    expect("an unmatched marker survives as text", mdText("2 * 3 = 6"), "2 * 3 = 6\n")
    expect("an unclosed bold survives", mdText("a **b"), "a **b\n")
}

group("markdown: code") {
    let inlineCode = md("run `ls -l` now")
    expect("backticks removed", inlineCode.string, "run ls -l now\n")
    // Colour rather than a ground: a background attribute is painted across a line's trailing
    // whitespace to the margin, so code that broke at a space left a bar half a line wide.
    expect("code is coloured", attr(inlineCode, .foregroundColor, near: "ls -l") as? NSColor,
           NSColor.systemBlue)
    check("code has no ground", attr(inlineCode, .backgroundColor, near: "ls -l") == nil)
    expect("code is monospace", (attr(inlineCode, .font, near: "ls -l") as? NSFont)?.fontName,
           mdTheme.mono.fontName)

    let fenced = md("```bash\nmake verify\n```")
    expect("the language tag is not content", fenced.string.contains("bash"), false)
    check("the code is there", fenced.string.contains("make verify"))
    expect("fenced code is monospace", (attr(fenced, .font, near: "make verify") as? NSFont)?.fontName,
           mdTheme.mono.fontName)
}

group("markdown: tables and code blocks") {
    // Monospace alone does not align a table whose cells are CJK, so the columns are placed
    // on measured tab stops. What the test can see is that the structure is there.
    let table = md("| 承載體 | 擋什麼 |\n|---|---|\n| 測試 | 復發 |\n| 介面 | 看不見的狀態 |")
    check("the separator row is not content", !table.string.contains("|---|"))
    check("no pipes survive", !table.string.contains("|"))
    check("every cell is there", table.string.contains("看不見的狀態"))

    // Borders come from NSTextTable, which needs TextKit 1. Without it the cells lay out as
    // plain paragraphs and the table silently loses its rules.
    let header = attr(table, .paragraphStyle, near: "承載體") as? NSParagraphStyle
    let block = header?.textBlocks.first as? NSTextTableBlock
    check("cells are table blocks", block != nil)
    check("the table has a border", (block?.width(for: .border, edge: .minY) ?? 0) > 0)
    check("cells are padded", (block?.width(for: .padding, edge: .minX) ?? 0) > 0)
    expect("the table knows its width", block?.table.numberOfColumns, 2)
    expect("the header is row zero", block?.startingRow, 0)
    let second = (attr(table, .paragraphStyle, near: "看不見的狀態") as? NSParagraphStyle)?
        .textBlocks.first as? NSTextTableBlock
    expect("a body cell lands in a later row", second?.startingRow, 2)

    expect("cells drop the border pipes", Markdown.cells("| a | b |"), ["a", "b"])
    expect("cells survive without borders", Markdown.cells("a | b"), ["a", "b"])

    // Every newline is a paragraph, so block spacing on all of them pushes a snippet apart.
    let fenced = md("```\nmake verify\ncd backend\nmake test\n```")
    let middle = attr(fenced, .paragraphStyle, near: "cd backend") as? NSParagraphStyle
    let last = attr(fenced, .paragraphStyle, near: "make test") as? NSParagraphStyle
    expect("interior lines are not pushed apart", middle?.paragraphSpacing, 0)
    expect("interior lines have no space before", middle?.paragraphSpacingBefore, 0)
    check("the block still ends with space", (last?.paragraphSpacing ?? 0) > 0)
}

group("markdown: lists") {
    let bullets = md("- one\n- two")
    check("a bullet glyph is used", bullets.string.contains("•"))
    check("the dash is gone", !bullets.string.contains("- one"))
    check("both items survive", bullets.string.contains("one") && bullets.string.contains("two"))

    let numbers = md("1. first\n2. second")
    check("numbers are kept as markers", numbers.string.contains("1."))
    check("both items survive", numbers.string.contains("first") && numbers.string.contains("second"))

    // Hanging indent is what makes a wrapped list item readable.
    let style = attr(bullets, .paragraphStyle, near: "one") as? NSParagraphStyle
    check("list items hang", (style?.headIndent ?? 0) > 0)
}

group("markdown: links") {
    let a = md("see [the docs](https://example.com/x) here")
    expect("only the label is shown", a.string, "see the docs here\n")
    expect("the url is attached", attr(a, .link, near: "the docs") as? String,
           "https://example.com/x")
    check("a link is underlined by the renderer", attr(a, .underlineStyle, near: "the docs") != nil)
    expect("a link takes the accent", attr(a, .foregroundColor, near: "the docs") as? NSColor,
           NSColor.systemOrange)
}

group("markdown: quotes and rules") {
    let q = md("> think about it")
    check("the marker is gone", !q.string.contains(">"))
    check("the text survives", q.string.contains("think about it"))
    let style = attr(q, .paragraphStyle, near: "think") as? NSParagraphStyle
    check("quotes are indented", (style?.headIndent ?? 0) > 0)

    let r = mdText("a\n\n---\n\nb")
    check("a rule becomes a line, not hyphens", !r.contains("---"))
    check("text either side survives", r.contains("a") && r.contains("b"))
}

group("folding runs of tool calls") {
    func tool(_ name: String, _ text: String = "") -> Transcript.Entry {
        .init(kind: .tool, text: text, tool: name, time: nil)
    }
    func result(_ text: String) -> Transcript.Entry {
        .init(kind: .toolResult, text: text, tool: nil, time: nil)
    }
    let said = Transcript.Entry(kind: .assistant, text: "done", tool: nil, time: nil)
    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)

    let run = [tool("Read", "a.swift"), result("ok"), tool("Bash", "make"), result("ok")]
    let folded = Transcript.render(run + [said], size: 11, mono: mono).string
    check("a finished run collapses to one line", !folded.contains("a.swift"))
    check("the count is shown", folded.contains("2"))
    check("the tools are named", folded.contains("Read") && folded.contains("Bash"))
    check("what came after is still there", folded.contains("done"))

    // The run still going is the one worth watching; folding it hides the changing part.
    let tail = Transcript.render([said] + run, size: 11, mono: mono).string
    check("the last run stays open", tail.contains("a.swift"))

    let key = Transcript.foldKey(run)
    let opened = Transcript.render(run + [said], size: 11, mono: mono, expanded: [key]).string
    check("an opened run shows its detail", opened.contains("a.swift"))

    // One call is not worth a fold — the summary line would be as tall as the thing it hides.
    let single = Transcript.render([tool("Read", "b.swift"), result("ok"), said],
                                   size: 11, mono: mono).string
    check("a single call is not folded", single.contains("b.swift"))

    // Keyed by content, not position: the pane re-renders from the tail of a growing file, so
    // an index would slide and open a different run than the one that was clicked.
    expect("the same run keys the same", Transcript.foldKey(run), key)
    check("a different run keys differently",
          Transcript.foldKey([tool("Read", "other.swift"), result("ok")]) != key)
    expect("repeated tools are named once",
           Transcript.distinct(["Bash", "Read", "Bash", "Bash"]), ["Bash", "Read"])
}

group("the words dictation is told to expect") {
    // Neither Apple speech API switches language mid-sentence, so mixed speech cannot be fixed
    // by picking a better mode. What is available is a hundred phrases of bias — and the ones
    // worth spending it on are the Latin words a Chinese recogniser has to guess at.
    let history = ["幫我把 webhook 那條改掉", "run make verify then commit",
                   "看一下 SFSpeechRecognizer 的 contextualStrings"]
    let words = Voice.vocabulary(from: history, extras: ["clawdline", "main"])

    check("technical terms are kept", words.contains("webhook") && words.contains("commit"))
    check("long identifiers survive", words.contains("SFSpeechRecognizer"))
    check("what the caller supplied leads", words.first == "clawdline")

    // The bar's own name is not a word in anybody's language model, and it is the likeliest
    // thing to be said to it.
    let always = Voice.vocabulary(from: [], extras: Voice.alwaysExpected)
    check("the app's name is always expected", always.contains("Clawdline"))
    check("so is what you are talking to", always.contains("Claude Code"))
    check("common version-control speech is always expected", always.contains("Commit"))
    check("common Clawdline speech is always expected", always.contains("Session"))
    check("the worker role is always expected", always.contains("Agent"))
    check("the orchestrator persona is always expected", always.contains("Clawdfather"))
    check("a phrase survives as a phrase", always.contains { $0.contains(" ") })
    check("CJK is left out — the recogniser already has it",
          !words.contains { $0.contains("幫") })
    check("one-and-two letter noise is dropped", !words.contains("的"))
    expect("nothing repeats", words.count, Set(words.map { $0.lowercased() }).count)

    // Backticks and commas come off, or the model is told to expect a word with punctuation in it.
    expect("punctuation is trimmed",
           Voice.vocabulary(from: ["use `git rebase`, then push."]).sorted(),
           ["git", "push", "rebase", "then", "use"].sorted())

    // A hundred is the documented ceiling, and newest wins because it is what you are doing now.
    let many = (1...200).map { "term\($0)" }
    let capped = Voice.vocabulary(from: many)
    expect("the budget is respected", capped.count, 100)
    check("the newest line is in it", capped.contains("term200"))
    check("the oldest is not", !capped.contains("term1"))
}

group("whisper as an optional engine") {
    // The header is written by hand, so it is the one part that can be wrong in a way whisper
    // would report as silence rather than as an error.
    let samples = Data(repeating: 0, count: 320)          // 160 frames of 16-bit mono
    let wav = Whisper.wavData(samples, rate: 16_000)
    expect("header plus samples", wav.count, 44 + samples.count)
    func text(_ r: Range<Int>) -> String { String(decoding: wav[r], as: UTF8.self) }
    expect("RIFF", text(0..<4), "RIFF")
    expect("WAVE", text(8..<12), "WAVE")
    expect("fmt chunk", text(12..<16), "fmt ")
    expect("data chunk", text(36..<40), "data")
    func u32(_ at: Int) -> UInt32 {
        wav[at..<at+4].reversed().reduce(UInt32(0)) { $0 << 8 | UInt32($1) }
    }
    expect("riff size counts everything after it", u32(4), UInt32(36 + samples.count))
    expect("sample rate", u32(24), 16_000)
    expect("byte rate is rate × channels × width", u32(28), 32_000)
    expect("data size", u32(40), UInt32(samples.count))

    // Half-installed is the state worth naming: brew gives you the binary and no model, so
    // "off" would send you to check the thing you already did.
    let status = Whisper.status(binary: "/nope/whisper-cli", model: "/nope/ggml.bin")
    check("a status is always available", [Whisper.Status.noBinary, .noModel].contains(status)
            || { if case .ready = status { return true }; return false }())
    check("noBinary and noModel are different answers", Whisper.Status.noBinary != .noModel)

    // Homebrew ships for-tests-ggml-tiny.bin. Treating a fixture as a model would report ready
    // and then transcribe everything into nonsense.
    check("the brew test fixture is not a model",
          Whisper.model(configured: "") .map { !$0.contains("for-tests") } ?? true)

    // Real output, and the reason this check exists: the decoder with nothing to go on keeps
    // choosing the same continuation. Rejecting it leaves the live text standing, which is at
    // least something that was heard.
    check("a repeated phrase is a groove, not a sentence",
          Whisper.looksLikeLoop("和音，和音，和音，和音，和音，和音，"))
    check("so is a repeated character", Whisper.looksLikeLoop("好好好好好好好好好"))
    check("and a repeated clause", Whisper.looksLikeLoop("請問號,請問號,請問號,請問號"))
    // Measured: what the model says to four seconds of room tone, with a plain prompt.
    check("three of the same clause counts", Whisper.looksLikeLoop("好,好,好。"))
    check("a real sentence is not", !Whisper.looksLikeLoop("把那個 webhook 的 retry 改成 backoff"))
    check("two clauses that happen to match are not",
          !Whisper.looksLikeLoop("好的，好的"))
    check("a sentence that repeats one word is not",
          !Whisper.looksLikeLoop("測試測試環境的設定有沒有問題"))
    check("something short is never a loop", !Whisper.looksLikeLoop("好的"))
    check("empty is not a loop", !Whisper.looksLikeLoop(""))

    // Silence is what it hallucinates on, so silence is what must not reach it.
    func tone(_ samples: [Int16]) -> Data {
        var d = Data()
        for s in samples { withUnsafeBytes(of: s.littleEndian) { d.append(contentsOf: $0) } }
        return d
    }
    let rate = 16_000.0
    let quiet = [Int16](repeating: 5, count: Int(rate))          // one second of nothing
    let loud = [Int16](repeating: 8000, count: Int(rate / 2))    // half a second of something
    let trimmed = Whisper.trimSilence(tone(quiet + loud + quiet), rate: rate)
    check("the quiet ends are cut", trimmed.count < tone(quiet + loud + quiet).count)
    check("the speech survives", trimmed.count >= loud.count * 2)
    // Trimming is relative, so a recording that is quiet all the way through has nothing to
    // trim — and should not be guessed at. Whether anything was said at all is the pause
    // detector's question, and it is asked before this is ever called.
    expect("a flat recording is left alone",
           Whisper.trimSilence(tone(quiet), rate: rate).count, tone(quiet).count)
    check("something too short to slice is left alone",
          Whisper.trimSilence(tone([Int16](repeating: 100, count: 100)), rate: rate).count == 200)
    // Quiet at the front only: the front goes, the rest stays.
    let tail = Whisper.trimSilence(tone(quiet + loud), rate: rate)
    check("leading quiet is dropped", tail.count < tone(quiet + loud).count)
    check("and the speech is still all there", tail.count >= loud.count * 2)

    // The bug this threshold was rewritten for. A sentence falls away as it ends — thirty
    // decibels of range is ordinary — so the last words are quiet compared to the loudest
    // moment and loud compared to an empty room. Judged against the peak they were cut, and
    // the result was a correct transcription of slightly less than you said.
    let room = [Int16](repeating: 30, count: Int(rate))
    let shout = [Int16](repeating: 8000, count: Int(rate))
    let trailing = [Int16](repeating: 600, count: Int(rate))   // 7.5% of the peak, 20x the room
    let ending = Whisper.trimSilence(tone(room + shout + trailing + room + room), rate: rate)
    check("the end of a sentence survives being quiet",
          ending.count >= (shout.count + trailing.count) * 2,
          "kept \(ending.count / 2) frames of \((shout.count + trailing.count)) spoken")
    check("and the room at either end still goes",
          ending.count < tone(room + shout + trailing + room + room).count)

    // Which must not turn into "keep everything": four seconds of silence is exactly what the
    // end of a recording looks like now that dictation stops itself.
    let withHang = Whisper.trimSilence(tone(shout + room + room + room + room), rate: rate)
    check("a long silence at the end is still cut",
          withHang.count < tone(shout + room + room).count)

    // Whisper writes 那個webhook的retry with no air in it, and reaches for a half-width comma
    // in the middle of a Chinese sentence. Neither can be asked away; both are mechanical.
    expect("a space appears between the scripts",
           Whisper.tidy("把那個webhook的retry改成exponential backoff"),
           "把那個 webhook 的 retry 改成 exponential backoff")
    expect("a space that is already there is not doubled",
           Whisper.tidy("把那個 webhook 改掉"), "把那個 webhook 改掉")
    expect("digits count as Latin", Whisper.tidy("跑第3次"), "跑第 3 次")
    expect("a comma after Chinese becomes full width",
           Whisper.tidy("先跑測試,然後 commit"), "先跑測試，然後 commit")
    expect("a comma after English does not",
           Whisper.tidy("run verify, then commit"), "run verify, then commit")
    // Real output: the word before the comma is English, the sentence around it is not.
    expect("a comma between English and Chinese is Chinese punctuation",
           Whisper.tidy("改成 backoff,然後跑測試"), "改成 backoff，然後跑測試")
    expect("English on its own is untouched",
           Whisper.tidy("make verify && git commit -m 'x'"), "make verify && git commit -m 'x'")
    expect("Chinese on its own is untouched", Whisper.tidy("先跑測試，然後提交"), "先跑測試，然後提交")
    expect("nothing is still nothing", Whisper.tidy(""), "")

    // Names that are not words come back as something that merely sounds right, and no amount
    // of asking fixes it — so it is repaired afterwards, where the repair is deterministic.
    let vocab = ["Clawdline", "Claude", "Claude Code", "Clawd"]
    func named(_ s: String) -> String { Whisper.applyVocabulary(s, terms: vocab) }

    expect("cloud code is Claude Code", named("ask cloud code about it"),
           "ask Claude Code about it")
    expect("two words become the two-word term", named("open Cloud Code now"),
           "open Claude Code now")
    expect("clawed line is Clawdline", named("type it into clawed line"),
           "type it into Clawdline")
    expect("and so is cloudline", named("cloudline is open"), "Clawdline is open")
    expect("it works inside a Chinese sentence",
           named("在 clawed line 裡面打字"), "在 Clawdline 裡面打字")
    expect("something already right is left exactly as it is",
           named("Clawdline and Claude Code"), "Clawdline and Claude Code")

    // The half that matters more. A corrector that rewrites ordinary words is worse than none,
    // because it is wrong in sentences that had nothing to do with the feature.
    expect("an ordinary cloud is still a cloud", named("deploy it to the cloud"),
           "deploy it to the cloud")
    expect("so is a cloudy day", named("it was a cloudy afternoon"), "it was a cloudy afternoon")
    expect("and a claw is not a name", named("the claw came off"), "the claw came off")
    expect("an unrelated sentence is untouched",
           named("run make verify and then commit"), "run make verify and then commit")
    expect("nothing is still nothing", named(""), "")
    expect("no terms means no changes", Whisper.applyVocabulary("cloud code", terms: []),
           "cloud code")

    // Short terms are matched exactly or not at all: at three letters everything is within one
    // edit of everything, and "API" would start eating "app".
    expect("a short term does not fuzzy-match",
           Whisper.applyVocabulary("the app is fine", terms: ["API"]), "the app is fine")

    expect("distance counts substitutions", Whisper.distance("cloud", "claud"), 1)
    expect("and insertions", Whisper.distance("clawdline", "clawdlines"), 1)
    expect("identical strings are zero apart", Whisper.distance("same", "same"), 0)
    expect("everything is empty away from nothing", Whisper.distance("", "abc"), 3)

    // The prompt only biases the script; asking for Traditional and getting Simplified back
    // happens. This is the part that does not depend on the model's mood.
    expect("Simplified becomes Traditional",
           Whisper.toTraditional("这个电脑的网络设置有问题"), "這個電腦的網絡設置有問題")
    expect("English in the middle is untouched",
           Whisper.toTraditional("把那个 webhook 的 retry 改成 exponential backoff"),
           "把那個 webhook 的 retry 改成 exponential backoff")
    expect("text with no Chinese in it comes back identical",
           Whisper.toTraditional("run make verify then commit"), "run make verify then commit")
    expect("already Traditional stays put",
           Whisper.toTraditional("這個電腦的網路設定有問題"), "這個電腦的網路設定有問題")
    check("zh-TW wants it", Whisper.wantsTraditional("zh-TW"))
    check("zh-Hant wants it", Whisper.wantsTraditional("zh-Hant"))
    check("zh-CN does not", !Whisper.wantsTraditional("zh-CN"))
    check("English does not", !Whisper.wantsTraditional("en-US"))
    check("auto does not", !Whisper.wantsTraditional("auto"))

    // Naming a language and naming a script are two different jobs. `-l zh` does the first;
    // Whisper writes Simplified regardless unless the initial prompt is in the script you want.
    // What matters about the seed is not what it says, it is what shape it is. Measured: a
    // prompt that does not end in punctuated prose produces a transcript with no punctuation.
    let tw = Whisper.language(for: "zh-TW").seed
    let cn = Whisper.language(for: "zh-CN").seed
    check("the seed ends in a full stop", tw?.hasSuffix("。") == true)
    check("it has commas in it too", tw?.contains("，") == true)
    check("the two scripts get different seeds", tw != cn)
    expect("and they are the same sentence in two scripts",
           cn.map(Whisper.toTraditional), tw)
    check("Hong Kong gets the Traditional one",
          Whisper.language(for: "zh-HK").seed == tw)
    expect("both are still the same language to whisper",
           Whisper.language(for: "zh-TW").code, "zh")
    expect("and gets a two-letter code", Whisper.language(for: "en-US").code, "en")
    // The punctuation fix was found by measuring a Chinese clip. Shipping it for Chinese only
    // would have meant everybody else keeps the bug it fixed.
    check("English gets a seed too", Whisper.language(for: "en-US").seed != nil)
    check("so does Japanese", Whisper.language(for: "ja").seed != nil)
    for (tag, seed) in Whisper.seeds {
        check("the \(tag) seed shows what punctuation looks like",
              seed.contains(where: { ".!?。！？".contains($0) }))
        check("the \(tag) seed is a sentence, not a word list",
              seed.count > 12 && seed.count < 90)
    }
    expect("a language with no seed still transcribes",
           Whisper.language(for: "sv-SE").code, "sv")
    expect("and asks for nothing in particular", Whisper.language(for: "sv-SE").seed, nil)
    expect("auto stays auto", Whisper.language(for: "auto").code, "auto")
    expect("empty is auto", Whisper.language(for: "").code, "auto")

    // Nothing installed has to read as "not available", not as a crash or an empty transcript.
    check("a path that is not there is not a binary",
          Whisper.binary(configured: "/nope/whisper-cli") == nil
            || FileManager.default.isExecutableFile(atPath: "/opt/homebrew/bin/whisper-cli"))
    check("a model that is not there is not a model",
          Whisper.model(configured: "/nope/ggml.bin") == nil
            || Whisper.model(configured: "/nope/ggml.bin")?.hasSuffix(".bin") == true)
}

group("how long a process has been running") {
    // Used to tell a session's own transcript from every other transcript in the project.
    // etime rather than lstart: lstart is a localised date, and parsing one of those to find a
    // file is how something works on the machine it was written on and nowhere else.
    expect("seconds", ITerm.parseElapsed("       12\n"), 12)
    expect("minutes and seconds", ITerm.parseElapsed("05:30"), 330)
    expect("hours", ITerm.parseElapsed("02:05:30"), 7530)
    expect("days", ITerm.parseElapsed("3-02:05:30"), 3 * 86400 + 7530)
    check("empty is not zero, it is unknown", ITerm.parseElapsed("   ") == nil)
    check("nonsense is unknown", ITerm.parseElapsed("1:2:3:4") == nil)
}

group("dictating next to a dropped image") {
    // Rebuilding the box from a string was the simple version, and it destroyed the thumbnail:
    // the string form of an attachment is its path. Speech writes into its own range instead.
    let v = PromptTextView()
    v.baseAttributes = [.font: NSFont.systemFont(ofSize: 13)]
    v.setPlainText("")
    v.insertPaths(["/tmp/shot.png"])
    v.beginDictation()
    v.updateDictation("這張圖")
    v.updateDictation("這張圖裡的表格")

    check("the picture is still a picture", v.string.contains("\u{FFFC}"))
    check("the path is not sitting in the box", !v.string.contains("/tmp/shot.png"))
    check("only the newest version of the speech is there",
          v.string.contains("這張圖裡的表格") && !v.string.contains("這張圖這張圖"))
    let sent = v.resolvedText()
    check("both go out together",
          sent.contains("/tmp/shot.png") && sent.contains("這張圖裡的表格"))

    // Speak, fix a word by hand, speak again. The fix has to survive, and the next words have
    // to land after it — not inside the sentence they were correcting.
    let edited = PromptTextView()
    edited.baseAttributes = [.font: NSFont.systemFont(ofSize: 13)]
    edited.setPlainText("")
    edited.beginDictation()
    var displaced = 0
    edited.onDictationDisplaced = { displaced += 1 }
    edited.updateDictation("修好那個 webhook")
    // The user corrects it, exactly as they would: select all, retype.
    edited.setPlainText("修好那個 webhook 的錯字")
    edited.setSelectedRange(NSRange(location: edited.string.count, length: 0))
    edited.updateDictation("然後跑測試")

    expect("the edit is noticed once", displaced, 1)
    expect("the correction stands and the new words follow it",
           edited.resolvedText(), "修好那個 webhook 的錯字然後跑測試")
    check("nothing was written into the middle of it",
          !edited.string.contains("然後跑測試 的錯字"))

    // Typing, then dictating, keeps the typing.
    let typed = PromptTextView()
    typed.baseAttributes = [.font: NSFont.systemFont(ofSize: 13)]
    typed.setPlainText("看一下")
    typed.setSelectedRange(NSRange(location: typed.string.count, length: 0))
    typed.beginDictation()
    typed.updateDictation("這個錯誤")
    expect("what was already typed survives", typed.resolvedText(), "看一下 這個錯誤")
}

group("nothing you typed yourself stays faded") {
    // Fading marks words speech has not finished with. It leaked onto typing: AppKit takes the
    // typing attributes from the text beside the caret, so the first characters typed after a
    // provisional run came out half-erased — outside the dictated range, where settling by
    // range never reached them. What the user saw was their own words looking unfinished with
    // the microphone off, and no way to fix it short of deleting the sentence.
    let base: [NSAttributedString.Key: Any] = [
        .font: NSFont.systemFont(ofSize: 13), .foregroundColor: NSColor.labelColor,
    ]

    func fadedCount(_ v: PromptTextView) -> Int {
        guard let storage = v.textStorage, storage.length > 0 else { return 0 }
        let settled = (base[.foregroundColor] as? NSColor) ?? .labelColor
        var faded = 0
        storage.enumerateAttribute(.foregroundColor,
                                   in: NSRange(location: 0, length: storage.length)) { value, r, _ in
            guard let c = value as? NSColor else { return }
            if c.alphaComponent < settled.alphaComponent { faded += r.length }
        }
        return faded
    }

    let v = PromptTextView()
    v.baseAttributes = base
    v.setPlainText("")
    v.beginDictation()
    v.updateDictation("開啟")
    check("while speech is still going, its words are faded", fadedCount(v) > 0)

    // Type at the caret, which is where dictation left it. This is the reported bug.
    v.insertText("。另外", replacementRange: v.selectedRange())
    expect("typing settles everything, including what was just typed", fadedCount(v), 0)

    v.insertText("再打一些", replacementRange: v.selectedRange())
    expect("and it stays settled as you keep going", fadedCount(v), 0)

    // Ending a run leaves nothing faded either, whatever the range was.
    let ended = PromptTextView()
    ended.baseAttributes = base
    ended.setPlainText("")
    ended.beginDictation()
    ended.updateDictation("跑一下測試")
    ended.endDictation()
    expect("stopping brings the dictated words up to full", fadedCount(ended), 0)
    ended.insertText("，然後回報", replacementRange: ended.selectedRange())
    expect("typing after it is not faded either", fadedCount(ended), 0)
}

group("knowing when somebody has stopped talking") {
    // The numbers are real: a quiet room here measures around 0.28, speech peaks past 0.7. A
    // fixed threshold of 0.12 — the first version — never fired once in that room.
    func feed(_ levels: [Float], gap: Double = 1.8) -> (hits: Int, floor: Float) {
        var d = Voice.SilenceDetector()
        var now = 0.0
        var hits = 0
        d.reset(now: now)
        for level in levels {
            now += 1.0 / 30
            if d.feed(level, now: now, gap: gap) { hits += 1 }
        }
        return (hits, d.floor)
    }
    /// Speech is not a flat line: it has gaps between syllables, and those gaps are the room.
    func speech(seconds: Double, room: Float = 0.28, voice: Float = 0.72) -> [Float] {
        (0..<Int(seconds * 30)).map { $0 % 5 < 3 ? voice : room }
    }
    func room(seconds: Double, at level: Float = 0.28) -> [Float] {
        Array(repeating: level, count: Int(seconds * 30))
    }

    let sentence = feed(speech(seconds: 2) + room(seconds: 4))
    expect("one sentence then quiet is one settle", sentence.hits, 1)
    check("the floor has learned the room, not the voice", abs(sentence.floor - 0.28) < 0.06)

    expect("while talking, never", feed(speech(seconds: 6)).hits, 0)
    expect("a gap between words is not a full stop",
           feed(speech(seconds: 2) + room(seconds: 1) + speech(seconds: 2)).hits, 0)
    expect("two sentences, two settles",
           feed(speech(seconds: 2) + room(seconds: 3)
                + speech(seconds: 2) + room(seconds: 3)).hits, 2)

    // Silence with nothing said before it has no stretch to end, however long it lasts.
    expect("an empty room settles nothing", feed(room(seconds: 10)).hits, 0)

    // A loud room is still a room, and a quiet voice is still a voice: the margin is what
    // matters, not the number.
    // Louder room, louder voice — which is what people do. The margin is about seven decibels
    // above the floor, so a voice that is only a hair above the noise is not one.
    expect("a noisy room does not stop it working",
           feed(speech(seconds: 2, room: 0.6, voice: 0.9) + room(seconds: 4, at: 0.6)).hits, 1)
    expect("a voice barely above the noise is not picked out",
           feed(speech(seconds: 2, room: 0.6, voice: 0.68) + room(seconds: 4, at: 0.6)).hits, 0)
    expect("a quiet voice in a silent room is speech",
           feed((0..<180).map { $0 % 5 < 3 ? Float(0.2) : Float(0.02) }).hits, 0)

    expect("zero turns it off", feed(speech(seconds: 2) + room(seconds: 6), gap: 0).hits, 0)

    // A stretch with nothing in it must be known to have nothing in it: a model asked to
    // transcribe an empty room answers anyway, usually with a short confident English sentence.
    var quietOnly = Voice.SilenceDetector()
    var t = 0.0
    quietOnly.reset(now: t)
    for level in room(seconds: 5) { t += 1.0 / 30; _ = quietOnly.feed(level, now: t, gap: 1.8) }
    check("an empty room reports no speech", !quietOnly.heardSpeech)

    var spoken = Voice.SilenceDetector()
    t = 0
    spoken.reset(now: t)
    for level in speech(seconds: 1) { t += 1.0 / 30; _ = spoken.feed(level, now: t, gap: 1.8) }
    check("a sentence reports speech", spoken.heardSpeech)
}

group("knowing when somebody has finished, not just paused") {
    func speech(seconds: Double) -> [Float] {
        (0..<Int(seconds * 30)).map { $0 % 5 < 3 ? Float(0.72) : Float(0.28) }
    }
    func room(seconds: Double) -> [Float] { Array(repeating: 0.28, count: Int(seconds * 30)) }

    // A settle ends a stretch and clears `heardSpeech`; ending the session asks the other
    // question, and it has to survive that clearing or the answer is always no.
    var d = Voice.SilenceDetector()
    var t = 0.0
    d.reset(now: t)
    for level in speech(seconds: 2) { t += 1.0 / 30; _ = d.feed(level, now: t, gap: 1.8) }
    for level in room(seconds: 2) { t += 1.0 / 30; _ = d.feed(level, now: t, gap: 1.8) }
    d.startNewStretch(now: t)
    check("a settled stretch is no longer speech in progress", !d.heardSpeech)
    check("but the session still knows somebody spoke", d.everHeardSpeech)

    for level in room(seconds: 3) { t += 1.0 / 30; _ = d.feed(level, now: t, gap: 1.8) }
    check("silence is measured from the last word, not from the settle", d.quiet(now: t) > 4.9)

    // The case this must never fire in: opened and left alone. There is no stretch to be the
    // end of, so the microphone stays open however long the room stays quiet.
    var untouched = Voice.SilenceDetector()
    t = 0
    untouched.reset(now: t)
    for level in room(seconds: 30) { t += 1.0 / 30; _ = untouched.feed(level, now: t, gap: 1.8) }
    expect("an unused microphone never counts as finished", untouched.quiet(now: t), 0)

    // How the sentence ended is evidence about whether it ended.
    expect("a full stop is somebody finishing", Voice.stopDelay(base: 4, after: "跑一次測試。"), 4)
    expect("so is one in English", Voice.stopDelay(base: 4, after: "run the tests."), 4)
    check("breaking off mid-clause waits longer",
          Voice.stopDelay(base: 4, after: "然後我想要") > 4.5)
    check("a comma is mid-thought too", Voice.stopDelay(base: 4, after: "first, ") > 4.5)
    expect("a bracket belongs to the sentence it closes",
           Voice.stopDelay(base: 4, after: "(run the tests.)"), 4)
    expect("zero turns it off", Voice.stopDelay(base: 0, after: "done."), 0)
    expect("nothing said yet is not evidence of anything", Voice.stopDelay(base: 4, after: ""), 4)
}

group("words that are not settled yet are not fully there") {
    // Provisional dictation is drawn faded, and settling brings it up to full. The whole claim
    // rests on the second half actually happening: text that stays faded reads as broken.
    func alpha(_ storage: NSTextStorage, at i: Int) -> CGFloat {
        let colour = storage.attribute(.foregroundColor, at: i, effectiveRange: nil) as? NSColor
        return (colour?.usingColorSpace(.sRGB) ?? .black).alphaComponent
    }
    let solid = NSColor.labelColor
    // Not 1.0: labelColor is 85% black in the light appearance, so "full" means "whatever
    // settled text has", not "opaque". A test that assumed opaque would be testing the theme.
    let full = solid.usingColorSpace(.sRGB)!.alphaComponent
    let base: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 13),
                                               .foregroundColor: solid]

    let v = PromptTextView()
    v.baseAttributes = base
    v.setPlainText("")
    v.beginDictation()
    v.updateDictation("還沒定案的字")
    check("speech in progress is faded", alpha(v.textStorage!, at: 0) < full * 0.7)

    v.endDictation()
    check("settling brings it back to what typed text has",
          abs(alpha(v.textStorage!, at: 0) - full) < 0.01)
    expect("and leaves the words alone", v.string, "還沒定案的字")

    // A pause settles one stretch and opens the next: the old one is solid, the new one is not.
    let two = PromptTextView()
    two.baseAttributes = base
    two.setPlainText("")
    two.beginDictation()
    two.updateDictation("第一段")
    two.endDictation()          // what a settle does
    two.beginDictation()
    two.updateDictation("第二段")
    let storage = two.textStorage!
    check("the settled stretch is solid", abs(alpha(storage, at: 0) - full) < 0.01)
    check("the live one is not", alpha(storage, at: storage.length - 1) < full * 0.7)

    // The fade is a change of alpha and nothing else — a different hue would read as a
    // different kind of text rather than the same text on its way in.
    let thin = PromptTextView.provisional(solid).usingColorSpace(.sRGB)!
    let solidRGB = solid.usingColorSpace(.sRGB)!
    check("it is the same colour, only thinner",
          abs(thin.redComponent - solidRGB.redComponent) < 0.01
          && abs(thin.blueComponent - solidRGB.blueComponent) < 0.01)
}

group("which versions this was run against") {
    expect("the version comes out of what claude prints",
           Compat.version(from: "2.1.233 (Claude Code)\n"), "2.1.233")
    expect("with no trimming needed", Compat.version(from: "2.1.233"), "2.1.233")
    // `codex --version` puts the number last rather than first, which is why this looks for the
    // first word starting with a digit rather than at the first word.
    expect("and Codex puts its number after its name",
           Compat.version(from: "codex-cli 0.149.0\n"), "0.149.0")
    check("and nothing comes out of something that is not one",
          Compat.version(from: "claude: command not found") == nil)
    check("or of nothing at all", Compat.version(from: "") == nil)

    expect("a patch behind is behind", Compat.compare("2.1.232", "2.1.233"), .orderedAscending)
    expect("a minor ahead is ahead", Compat.compare("2.2.0", "2.1.233"), .orderedDescending)
    expect("equal is equal", Compat.compare("2.1.233", "2.1.233"), .orderedSame)
    // "2.1" against "2.1.1" the wrong way round is the classic: string comparison says 2.1.9
    // is newer than 2.1.10, and every number after the first would be read as one digit.
    expect("a missing part counts as zero", Compat.compare("2.1", "2.1.0"), .orderedSame)
    expect("and ten is after nine", Compat.compare("2.1.9", "2.1.10"), .orderedAscending)

    // Only older gets a word. Newer is the normal state of the world — Claude Code updates
    // itself and this does not — and a warning that fires every week is one nobody is reading
    // by the time it means something.
    check("older says so", Compat.note(installed: "2.0.1", builtAgainst: "2.1.233") != nil)
    check("newer says nothing", Compat.note(installed: "2.9.0", builtAgainst: "2.1.233") == nil)
    check("the same says nothing", Compat.note(installed: "2.1.233", builtAgainst: "2.1.233") == nil)
    check("not knowing says nothing", Compat.note(installed: nil, builtAgainst: "2.1.233") == nil)
    // Every release before this was written has "not recorded" in that column rather than a
    // guess, and a table with no real version in it must not start warning about everything.
    check("and neither does a table with nothing recorded in it",
          Compat.note(installed: "2.0.0", builtAgainst: "") == nil)

    check("the table names a version somebody checked", !Compat.builtAgainst.isEmpty)
    check("every dependency says what you would see if it broke",
          Compat.dependencies.allSatisfy { !$0.symptom.isEmpty && !$0.where_.isEmpty })
    check("and there is a release for the version being built",
          Compat.releases.contains { $0.clawdline == appVersion() },
          "Info.plist says \(appVersion()), the table's newest is \(Compat.releases[0].clawdline)")
}

group("an image goes over as an image, not as a path") {
    // Claude Code shows a pasted image as [Image #3] — in the message, numbered, and something
    // you can point at in the sentence you are writing. A path is forty characters of directory
    // and one tool call away from the thing it is a picture of.
    check("a png is one", Drop.isImage("/tmp/shot.png"))
    check("so is a jpeg", Drop.isImage("/tmp/a.JPEG"))
    check("and a heic", Drop.isImage("/tmp/a.heic"))
    check("a pdf is not — it is a document, and a path is right for it",
          !Drop.isImage("/tmp/spec.pdf"))
    check("nor is a directory", !Drop.isImage("/tmp"))
    check("nor a swift file", !Drop.isImage("/tmp/main.swift"))

    // The split. Text either side of an image stays text, and adjacent runs are joined so a
    // prompt with no images in it is still exactly one paste.
    expect("no images means one piece",
           Drop.pieces(text: "look at this", imagePaths: []), [.text("look at this")])
    expect("nothing at all means nothing", Drop.pieces(text: "", imagePaths: []), [])
    expect("text, image, text",
           Drop.pieces(text: "before /tmp/a.png after", imagePaths: ["/tmp/a.png"]),
           [.text("before "), .image("/tmp/a.png"), .text(" after")])
    expect("an image at the very start",
           Drop.pieces(text: "/tmp/a.png explain", imagePaths: ["/tmp/a.png"]),
           [.image("/tmp/a.png"), .text(" explain")])
    expect("two of them keep their order",
           Drop.pieces(text: "/tmp/a.png and /tmp/b.png", imagePaths: ["/tmp/a.png", "/tmp/b.png"]),
           [.image("/tmp/a.png"), .text(" and "), .image("/tmp/b.png")])
    // A path with a space in it is quoted in the text, so that is what has to be found.
    expect("a quoted path is matched as it was written",
           Drop.pieces(text: "see '/tmp/my shot.png' here", imagePaths: ["/tmp/my shot.png"]),
           [.text("see "), .image("/tmp/my shot.png"), .text(" here")])
    // A path that is not actually in the text is not invented into the prompt.
    expect("an image the text never mentions is skipped",
           Drop.pieces(text: "just words", imagePaths: ["/tmp/gone.png"]), [.text("just words")])
}

group("remote images survive the terminal handoff") {
    // The phone sends bytes, but Codex receives a path and opens it only after the HTTP request
    // has returned. Deleting the file with the route's defer made the path dead on arrival.
    let image = NSImage(size: NSSize(width: 2, height: 2))
    image.lockFocus()
    NSColor.systemBlue.setFill()
    NSRect(x: 0, y: 0, width: 2, height: 2).fill()
    image.unlockFocus()
    let png = NSBitmapImageRep(data: image.tiffRepresentation!)!
        .representation(using: .png, properties: [:])!
    let source = "data:image/png;base64," + png.base64EncodedString()

    let made = RemoteServer.pieces(text: "look", images: [source, source])
    expect("every upload becomes an image piece", made.stored.count, 2)
    expect("same-millisecond uploads keep distinct paths", Set(made.stored).count, 2)
    check("the cached uploads exist before handoff",
          made.stored.allSatisfy { FileManager.default.fileExists(atPath: $0) })
    // Two real files, and each `Drop.store` prunes. Unisolated this group alone is two of the
    // three writes that stood between the live cache and deleting somebody's oldest picture.
    check("and none of them landed in the live drop cache",
          made.stored.allSatisfy { !$0.hasPrefix(liveDropDirectory.path) },
          made.stored.joined(separator: " "))
    RemoteServer.finishUploads(made.stored, sent: true)
    check("a successful handoff keeps them for Codex",
          made.stored.allSatisfy { FileManager.default.fileExists(atPath: $0) })
    RemoteServer.finishUploads(made.stored, sent: false)
    check("a failed handoff leaves no orphans",
          made.stored.allSatisfy { !FileManager.default.fileExists(atPath: $0) })
}

group("giving the pasteboard back") {
    // Borrowing the pasteboard and not returning it costs somebody whatever they copied five
    // minutes ago, for a feature they never asked for.
    let pb = exclusivePasteboard("restore")
    defer { pb.releaseGlobally() }
    pb.clearContents()
    pb.setString("something the user copied", forType: .string)

    let saved = Drop.contents(of: pb)
    check("the snapshot has the item in it", saved.count == 1)
    check("and it holds the bytes, not a reference to a cleared item",
          saved.first?[.string] != nil)

    pb.clearContents()
    pb.setString("borrowed", forType: .string)
    expect("which is genuinely gone once cleared", pb.string(forType: .string), "borrowed")

    Drop.put(saved, on: pb)
    expect("and comes back exactly", pb.string(forType: .string), "something the user copied")

    // An empty pasteboard is a state worth restoring too, rather than leaving the borrowed
    // thing behind because there was nothing to put back.
    pb.clearContents()
    let nothing = Drop.contents(of: pb)
    pb.setString("borrowed again", forType: .string)
    Drop.put(nothing, on: pb)
    check("an empty one is restored as empty", pb.string(forType: .string) == nil)
}

group("editing the config while the app is running") {
    // The app rewrites this file whenever anything moves, and it used to write everything it
    // held in memory — so an edit made while it was running disappeared at an unpredictable
    // later moment. Nothing about that looks like a bug from the outside: the file is simply
    // the way it was.
    let known: [String: Any] = ["width": 720.0, "mascot": "clawd", "hotkey": "option+space"]

    // They edited width; we did not touch it.
    var out = Config.merged(mine: ["width": 720.0, "mascot": "clawd", "hotkey": "option+space"],
                            known: known,
                            onDisk: ["width": 900.0, "mascot": "clawd", "hotkey": "option+space"])
    expect("a key only they changed keeps their value", out["width"] as? Double, 900.0)

    // We changed the mascot (⌘M); they edited width in the meantime. Both survive.
    out = Config.merged(mine: ["width": 720.0, "mascot": "mochi", "hotkey": "option+space"],
                        known: known,
                        onDisk: ["width": 900.0, "mascot": "clawd", "hotkey": "option+space"])
    expect("a key only we changed keeps ours", out["mascot"] as? String, "mochi")
    expect("and theirs is still there too", out["width"] as? Double, 900.0)

    // Both changed the same key. Ours wins, because somebody pressed something.
    out = Config.merged(mine: ["mascot": "mochi"], known: ["mascot": "clawd"],
                        onDisk: ["mascot": "pixel"])
    expect("both changed it, the app wins", out["mascot"] as? String, "mochi")

    // A setting a newer version wrote must survive being opened by an older one, or downgrading
    // once quietly deletes it.
    out = Config.merged(mine: ["width": 720.0], known: ["width": 720.0],
                        onDisk: ["width": 720.0, "something_new": "keep me"])
    expect("a key we have never heard of is left alone", out["something_new"] as? String, "keep me")

    // A file that is not there yet is not an edit.
    out = Config.merged(mine: ["width": 720.0, "mascot": "clawd"], known: [:], onDisk: [:])
    expect("with no file, everything we hold is written", out.count, 2)

    // Booleans and arrays have to compare as themselves, not as "some object".
    out = Config.merged(mine: ["reopen_on_return": false, "history": ["a", "b"]],
                        known: ["reopen_on_return": true, "history": ["a"]],
                        onDisk: ["reopen_on_return": true, "history": ["a"]])
    expect("a flag we turned off stays off", out["reopen_on_return"] as? Bool, false)
    expect("a list we appended to keeps the new entry",
           (out["history"] as? [String])?.count, 2)
}

group("paths somebody wrote in a config file") {
    // A blank means "use your own default", not "the root directory" — and the difference only
    // shows up as nothing being found, which is a normal state for every one of these files.
    check("blank means no opinion", Paths.resolve("") == nil)
    check("whitespace is blank", Paths.resolve("   \n") == nil)

    let home = FileManager.default.homeDirectoryForCurrentUser.path
    expect("a tilde is expanded", Paths.expand("~/notes"), home + "/notes")
    expect("on its own too", Paths.expand("~"), home)
    // ~someone is a shell convention nobody types into a config file, and NSString's expansion
    // handles it badly. An absolute path is left exactly as written.
    expect("another user's home is left alone", Paths.expand("~bob/notes"), "~bob/notes")
    expect("an absolute path is untouched", Paths.expand("/opt/x"), "/opt/x")
    expect("so is a relative one", Paths.expand("cache/x"), "cache/x")
    expect("and it survives the round trip", Paths.resolve("~/notes")?.path, home + "/notes")
}

group("the languages the interface speaks") {
    // The protocol makes the compiler refuse a language that is missing a string. What it cannot
    // refuse is a language that is present and still in English — copy the reference file,
    // translate half of it, and everything builds.
    func resolve(_ tag: String) -> Copy? {
        L.catalog.first(where: { tag.hasPrefix($0.tag) })?.copy
    }
    func name(_ copy: Copy?) -> String { copy.map { "\(type(of: $0))" } ?? "none" }

    // Matched by prefix, so a broad tag above a narrow one swallows it. The symptom is somebody's
    // interface quietly coming up in the wrong script, which nothing else here would notice.
    for (tag, copy) in L.catalog {
        expect("\(tag) is not shadowed by an earlier entry", name(resolve(tag)), name(copy))
    }

    // The tags macOS actually hands over are longer than the ones in the catalog.
    expect("Taiwan gets Traditional", name(resolve("zh-Hant-TW")), "TraditionalChinese")
    expect("Hong Kong too", name(resolve("zh-HK")), "TraditionalChinese")
    expect("the mainland gets Simplified", name(resolve("zh-Hans-CN")), "SimplifiedChinese")
    expect("and so does zh-CN", name(resolve("zh-CN")), "SimplifiedChinese")
    expect("Brazil and Portugal share one", name(resolve("pt-BR")), "Portuguese")
    expect("en-GB is English", name(resolve("en-GB")), "English")
    expect("ja-JP is Japanese", name(resolve("ja-JP")), "Japanese")
    check("a language nobody has written yet falls through", resolve("sv-SE") == nil)

    // Every stored string, not a hand-written sample of fifteen.
    //
    // The sample version was the reason a whole settings window shipped with thirty-two new
    // strings that nothing checked: a new string is by definition not in a list written before
    // it existed, so the one test meant to catch "copied the reference file and translated half
    // of it" could not see the half that mattered. `Mirror` over the struct gets all of them, so
    // adding a string adds its own check with it.
    //
    // A handful legitimately read the same in two languages — "Terminal" is "Terminal" in most
    // of Europe — so those are named here, one at a time, with a reason. Anything not on this
    // list that matches English is untranslated, and says so by name.
    // A handful legitimately read the same in two languages — "Terminal" is "Terminal" in most
    // of Europe, and "General" is a Spanish word. Those are named one at a time as `tag:member`,
    // or `*:member` for the ones that are the same everywhere, each with a reason. Anything not
    // named here that matches English is untranslated, and the failure says which.
    //
    // Per language rather than per string on purpose: "log" being the word in Italian is not a
    // reason for it to still be the word in Chinese, and a global exemption would have said it
    // was.
    let sameIsFine: Set<String> = [
        "*:settingsAuto",       // "Auto" is the word in Italian and French too
        "*:settingsTerminal",   // the same loan word almost everywhere
        "*:settingsTranscript", // ditto, and it is the name of the thing on screen
        "*:settingsTitle",      // starts with the app's name, which is not translated
        "*:settingsOff",        // "Off" survives untranslated in several
        "*:hintMascot",         // a short loan word in most of these
        "*:webInfoTokens",      // the unit under a count; "tokens" is what Spanish and Portuguese call them too
        "es:settingsGeneral",   // "General" is Spanish
        "it:hintOutput",        // and "output" is Italian
        "it:stackActionLogs",   // as is "log"
        "id:stackActionLogs",   // and in Indonesian
        "de:settingsTunnelHostname", // "Hostname" is the German word for it as well
        // A status label beside a green dot. French, German, Italian, Spanish and Portuguese
        // interfaces all say "ok" there — `bon`, `gut`, `bene`, `bien` and `certo` mean *good*,
        // which is a different claim and reads as a translation of a word nobody used. The
        // instruction not to touch this file pushed a translator into inventing five of them.
        "fr:webLinkOk", "de:webLinkOk", "it:webLinkOk", "es:webLinkOk", "pt:webLinkOk",
        "fr:webBack",           // "Sessions" is the French plural, and the button is a destination
        "fr:webSettingsNotify", // "Notifications" is what French macOS calls exactly this
        "fr:webSettingsVersion", // "Version {v}" — the alternatives all mean something else
        "de:webSettingsVersion", // ditto; Fassung and Ausgabe are not what software has
        "it:webDoorPassword",   // "Password" is the Italian word; parola d'ordine is nobody's
        // Row labels on the Session info card. These are the words, not loan words nobody
        // uses: French has no other word for its assistant; "Total" is the word in French,
        // Spanish, Portuguese and Indonesian; "Model" in Indonesian and Turkish; and "Branch"
        // is what a German, Italian, Portuguese or Indonesian developer calls one.
        "fr:webInfoAssistant",
        "fr:webInfoTotal", "es:webInfoTotal", "pt:webInfoTotal", "id:webInfoTotal",
        "id:webInfoModel", "tr:webInfoModel",
        // The same word again, on the two rows that pick a size rather than report one.
        "id:webCommandModel", "tr:webCommandModel",
        "id:webScheduleModel", "tr:webScheduleModel",
        "de:webInfoBranch", "it:webInfoBranch", "pt:webInfoBranch", "id:webInfoBranch",
        // Permission mode labels use the same native words: manual in Spanish, Portuguese and
        // Indonesian, and Plan in German and Turkish.
        "es:webInfoPermissionManual", "pt:webInfoPermissionManual", "id:webInfoPermissionManual",
        "de:webInfoPermissionPlan", "tr:webInfoPermissionPlan",
        // The Clawdfather panel. "Administration" is the French word too, and a German
        // interface really does say "online"/"offline" — anglicisms macOS itself uses there.
        "fr:webCoordSectionAdmin",
        "de:webCoordOnline", "de:webCoordOffline",
    ]
    let en = English()

    func strings(of copy: Copy) -> [String: String] {
        var out: [String: String] = [:]
        for child in Mirror(reflecting: copy).children {
            guard let label = child.label, let value = child.value as? String else { continue }
            out[label] = value
        }
        return out
    }

    let reference = strings(of: en)
    check("the reference file has strings to compare against", reference.count > 40,
          "found \(reference.count)")

    for (tag, copy) in L.catalog where tag != "en" {
        let mine = strings(of: copy)
        let missing = reference.keys.filter { mine[$0] == nil }
        check("\(tag) implements every string", missing.isEmpty,
              missing.sorted().joined(separator: ", "))

        let untranslated = reference
            .filter { mine[$0.key] == $0.value }
            .filter { !sameIsFine.contains("*:" + $0.key) && !sameIsFine.contains(tag + ":" + $0.key) }
            .keys.sorted()
        check("\(tag) is translated, not copied", untranslated.isEmpty,
              "still English: " + untranslated.joined(separator: ", "))
    }

    // The hint words share one row along the bottom. A long one there does not wrap, it pushes
    // the next word off the end — and the word that goes missing is in somebody else's language,
    // so nobody working on the layout ever sees it happen.
    for (tag, c) in L.catalog {
        let hints = [c.hintSend, c.hintNewline, c.hintSwitch, c.hintList, c.hintMascot,
                     c.hintOutput, c.hintFullscreen, c.hintKeys, c.hintTextSize, c.hintOrder,
                     c.hintVoice]
        let long = hints.filter { $0.count > 20 }
        check("\(tag) keeps its hint words short", long.isEmpty, long.joined(separator: ", "))
        check("\(tag) has no empty hint", hints.allSatisfy { !$0.isEmpty })
    }

    // Every translation has to survive being asked the questions with arguments in them.
    for (tag, c) in L.catalog {
        check("\(tag) fills in the model name",
              c.dictationStatus(.ready(model: "ggml-x.bin")).contains("ggml-x.bin"))
        check("\(tag) fills in the config path",
              c.hotkeyFailedBody("/tmp/config.json").contains("/tmp/config.json"))
        check("\(tag) fills in the key combination",
              c.hotkeyFailedTitle("⌥Space").contains("⌥Space"))
        check("\(tag) distinguishes the two reading orders",
              c.outputOrder(newestFirst: true) != c.outputOrder(newestFirst: false))
        check("\(tag) distinguishes on-device from not",
              c.voiceListening(onDevice: true) != c.voiceListening(onDevice: false))
    }

    // No interface string names a terminal that the reader may not be running. The errors that
    // do name iTerm2 come from the iTerm2 path itself, where naming it is the point.
    for (tag, c) in L.catalog {
        check("\(tag) does not assume which terminal you use",
              !c.nothingToSend.contains("iTerm") && !c.noSession.contains("iTerm"))
    }
}

group("every Session work-state string crosses the typed localization contract") {
    let keys = ["sessionWorkReady", "sessionWorkUnknown", "sessionWorkHolding",
                "sessionWorkOwed", "sessionWorkSelfStated",
                "sessionWorkMilestone", "sessionWorkComplete"]
    let fallback = try! String(contentsOfFile: "Resources/web/app/js/core/i18n.js")
    let server = try! String(contentsOfFile: "Sources/RemoteServer.swift")
    for key in keys {
        check("the browser fallback and /v1/strings payload both name \(key)",
              fallback.contains("\(key):") && server.contains("\"\(key)\":"))
    }
    for (tag, copy) in L.catalog {
        let values = [copy.sessionWorkReady, copy.sessionWorkUnknown, copy.sessionWorkHolding,
                      copy.sessionWorkOwed, copy.sessionWorkSelfStated,
                      copy.sessionWorkMilestone, copy.sessionWorkComplete]
        check("\(tag) supplies every Session work-state string",
              values.allSatisfy { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty })
    }
}

group("the code-block copy button's words cross the typed localization contract") {
    // Three names, three boundaries each: the protocol (which the compiler enforces), the
    // browser's English fallback, and the `/v1/strings` payload. The compiler covers exactly one
    // of the three, and the other two fail quietly — a missing fallback is a blank button on a
    // page whose request for the real words did not arrive, and a missing payload entry is that
    // button staying in English in thirteen languages.
    let keys = ["webCodeCopy", "webCodeCopied", "webCodeCopyFailed"]
    let fallback = try! String(contentsOfFile: "Resources/web/app/js/core/i18n.js")
    let server = try! String(contentsOfFile: "Sources/RemoteServer.swift")
    for key in keys {
        check("the browser fallback and /v1/strings payload both name \(key)",
              fallback.contains("\(key):") && server.contains("\"\(key)\":"))
    }
    // The failure sentence is the one that cannot go missing. Both branches that reach it — a
    // browser with no clipboard API and a write the browser refused — are silent everywhere else
    // on this page, and a language that left this blank would put that silence back.
    for (tag, copy) in L.catalog {
        check("\(tag) says what a failed copy did",
              !copy.webCodeCopyFailed.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        check("\(tag) tells the two answers apart", copy.webCodeCopied != copy.webCodeCopyFailed)
    }
}

group("dictation starts where the caret is") {
    // Speech is not a different kind of input: it goes where typing would have gone. This used
    // to append to the end regardless, so going back to add a sentence in the middle put it at
    // the bottom of the box instead.
    let base: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 13)]

    let mid = PromptTextView()
    mid.baseAttributes = base
    mid.setPlainText("first. last.")
    mid.setSelectedRange(NSRange(location: 6, length: 0))   // after "first."
    mid.beginDictation()
    mid.updateDictation("middle.")
    expect("it lands at the caret, not at the end", mid.string, "first. middle. last.")

    let end = PromptTextView()
    end.baseAttributes = base
    end.setPlainText("before")
    end.setSelectedRange(NSRange(location: 6, length: 0))
    end.beginDictation()
    end.updateDictation("after")
    expect("with the caret at the end it still appends", end.string, "before after")

    // Typing over a selection replaces it, so dictating over one does too.
    let sel = PromptTextView()
    sel.baseAttributes = base
    sel.setPlainText("keep this drop that")
    sel.setSelectedRange(NSRange(location: 10, length: 9))  // "drop that"
    sel.beginDictation()
    sel.updateDictation("say this")
    expect("a selection is replaced", sel.string, "keep this say this")

    // One space, not two: the separator is about the word in front of the caret, and there
    // already is one when the caret sits after a space.
    let spaced = PromptTextView()
    spaced.baseAttributes = base
    spaced.setPlainText("a ")
    spaced.setSelectedRange(NSRange(location: 2, length: 0))
    spaced.beginDictation()
    spaced.updateDictation("b")
    expect("no second space after one that is already there", spaced.string, "a b")
}

group("dictation across a pause") {
    // The recogniser settles a sentence at a pause and starts the next from nothing, so the
    // text it hands back is only ever the sentence in progress. Sticking them together is this
    // side's job — get it wrong and the second thing you say deletes the first.
    expect("sentences are kept in order",
           Voice.join("先做 A", "再做 B"), "先做 A再做 B")
    expect("latin gets the space it needs",
           Voice.join("run make verify", "then commit"), "run make verify then commit")
    expect("nothing before means nothing to join", Voice.join("", "第一句"), "第一句")
    expect("nothing after leaves it alone", Voice.join("第一句", ""), "第一句")
    expect("a boundary between scripts takes no space",
           Voice.join("先跑 verify", "然後 commit"), "先跑 verify然後 commit")
}

group("files and images dropped on the bar") {
    // What gets sent is a path, because that is the whole handoff: Claude Code reads files
    // itself. So the one thing that must be right is that the path survives being written out.
    expect("an ordinary path needs no quoting",
           Drop.quoted("/Users/me/code/a_file-1.png"), "/Users/me/code/a_file-1.png")
    expect("a space earns quotes",
           Drop.quoted("/Users/me/My Files/a.png"), "'/Users/me/My Files/a.png'")
    expect("so does anything a shell would read",
           Drop.quoted("/tmp/a;rm -rf b"), "'/tmp/a;rm -rf b'")
    expect("a quote in the name does not end the quoting",
           Drop.quoted("/tmp/it's.png"), "'/tmp/it'\\''s.png'")
    expect("several are separated",
           Drop.insertion(for: ["/a.png", "/b c.png"]), "/a.png '/b c.png'")
    expect("nothing dropped, nothing added", Drop.insertion(for: []), "")

    // Written names sort by time, which is what makes pruning by name the oldest-first order.
    let early = Drop.filename(extension: "png", now: Date(timeIntervalSince1970: 1_000_000))
    let later = Drop.filename(extension: "png", now: Date(timeIntervalSince1970: 2_000_000))
    check("names sort oldest first", early < later)
    check("the extension is kept", later.hasSuffix(".png"))

    // A dragged file offers its bytes as well as its path; taking the path avoids a second copy.
    let board = exclusivePasteboard("drop")
    defer { board.releaseGlobally() }
    board.clearContents()
    board.writeObjects([URL(fileURLWithPath: "/tmp/dropped.png") as NSURL])
    expect("a dragged file gives its own path",
           Drop.paths(from: board), ["/tmp/dropped.png"])

    board.clearContents()
    board.setString("just text", forType: .string)
    expect("dragged text is not a file", Drop.paths(from: board), [])

    // An image off the clipboard has no path, so one has to be written — that file is the only
    // thing this feature leaves behind, so it is worth proving it lands and is readable.
    let image = NSImage(size: NSSize(width: 2, height: 2))
    image.lockFocus()
    NSColor.systemPink.setFill()
    NSRect(x: 0, y: 0, width: 2, height: 2).fill()
    image.unlockFocus()
    board.clearContents()
    board.setData(image.tiffRepresentation, forType: .tiff)
    let written = Drop.paths(from: board)
    expect("a pasted image becomes exactly one path", written.count, 1)
    check("and the file is really there",
          written.first.map { FileManager.default.fileExists(atPath: $0) } == true)
    check("and it can be read back",
          written.first.flatMap { NSImage(contentsOfFile: $0) } != nil)
    // The file this assertion just proved exists had to be written somewhere, and unisolated that
    // somewhere is the person's own drop cache — which prunes on every write.
    check("and it was written outside the live drop cache",
          written.allSatisfy { !$0.hasPrefix(liveDropDirectory.path) }, written.joined(separator: " "))
    written.forEach { try? FileManager.default.removeItem(atPath: $0) }
}

group("a dropped file is a picture on screen and a path on the wire") {
    let view = PromptTextView()
    view.baseAttributes = [.font: NSFont.systemFont(ofSize: 13)]
    view.setPlainText("look at")
    view.setSelectedRange(NSRange(location: view.string.count, length: 0))
    view.insertPaths(["/tmp/shot one.png"])

    // What is shown is for the person; what is sent is for Claude Code. The moment those are
    // the same string, one of them is being made worse to suit the other.
    check("the path is not sitting in the box", !view.string.contains("/tmp/shot"))
    check("something stands in its place", view.string.contains("\u{FFFC}"))
    expect("and the wire gets the path, quoted",
           view.resolvedText(), "look at '/tmp/shot one.png' ")
    check("what was already typed survives", view.resolvedText().hasPrefix("look at "))

    // Clearing has to clear the mapping too, or a dropped file follows the next message it was
    // never part of.
    view.clearText()
    view.setPlainText("next message")
    expect("a cleared box sends only what is in it", view.resolvedText(), "next message")

    // Taking it back out has to work by every route, because the file goes with the message and
    // a message cannot be recalled. A text view only has an undo manager once it is in a window
    // — without one, undo does nothing and that looks exactly like undo failing to remove it.
    let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 400, height: 60),
                          styleMask: [.titled], backing: .buffered, defer: false)
    func dropped() -> PromptTextView {
        let v = PromptTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 40))
        v.baseAttributes = [.font: NSFont.systemFont(ofSize: 13)]
        v.allowsUndo = true
        window.contentView?.subviews.forEach { $0.removeFromSuperview() }
        window.contentView?.addSubview(v)
        window.makeFirstResponder(v)
        v.setPlainText("look at")
        v.setSelectedRange(NSRange(location: v.string.count, length: 0))
        v.insertPaths(["/tmp/a.png"])
        return v
    }

    let undone = dropped()
    check("undo is even possible", undone.undoManager?.canUndo == true)
    undone.undoManager?.undo()
    check("⌘Z takes the file back out", !undone.resolvedText().contains("/tmp/a.png"))
    check("and takes the picture with it", !undone.string.contains("\u{FFFC}"))

    let cleared = dropped()
    cleared.selectAll(nil)
    cleared.delete(nil)
    expect("select-all and delete leaves nothing to send", cleared.resolvedText(), "")

    let overtyped = dropped()
    overtyped.setSelectedRange(NSRange(location: 8, length: 1))
    overtyped.insertText("hi", replacementRange: overtyped.selectedRange())
    check("typing over it replaces it", !overtyped.resolvedText().contains("/tmp/a.png"))
}

group("the documented example files") {
    // docs/project-status.md tells other people how to write these. The examples beside it are
    // parsed here with the same code the app uses, so the page cannot quietly stop being true.
    func load(_ name: String) -> [String: Any]? {
        ProjectStatus.json(URL(fileURLWithPath: "docs/examples/" + name))
    }

    let icons = load("project-icons.json")?["projects"] as? [String: Any]
    check("the registry example is there", icons != nil)
    var rows: [String: [String: Any]] = [:]
    for (k, v) in icons ?? [:] { if let r = v as? [String: Any] { rows[k] = r } }
    let drawn = ProjectIcon.entry(forCwd: "/Users/you/code/atrium/frontend", in: rows)
        .flatMap { ProjectIcon.grid(for: $0) }
    expect("the hand-drawn example draws", drawn?.cells.count, 4)
    expect("and carries its accent", drawn?.accent, ProjectIcon.color(hex: "#5CBBA1"))
    let generated = ProjectIcon.entry(forCwd: "/Users/you/code/cairn", in: rows)
        .flatMap { ProjectIcon.grid(for: $0) }
    expect("the generated example draws too", generated?.cells.count, 4)

    // The registry holds more than a mark: a project that deploys something puts its health
    // check here, and that is what the poller reads.
    let health = ProjectIcon.entry(forCwd: "/Users/you/code/atrium", in: rows)?["health"] as? [String: Any]
    check("the registry example carries a health block", health != nil)
    expect("with somewhere to poll", (health?["url"] as? String)?.hasPrefix("https://"), true)
    check("and something that is false when it is broken",
          (health?["expect"] as? [String: Any])?.isEmpty == false)

    let deploy = ProjectStatus.deploy(load("ghrun-you-atrium.json"))
    expect("the deploy example is running", deploy?.state, "running")
    expect("with somewhere to click", deploy?.url?.hasPrefix("https://github.com/"), true)

    // A producer emits this constantly — no workflow, no run on this branch, gh not installed.
    // A consumer that has not heard of it draws a cross for a project that simply has no CI,
    // which is a red mark that is always wrong.
    let quiet = ProjectStatus.deploy(load("ghrun-you-quiet.json"))
    expect("nothing to say is its own state", quiet?.state, "none")
    expect("and a bar to draw", ProjectStatus.bar(0.5, width: 8), "▰▰▰▰▱▱▱▱")

    let backlog = ProjectStatus.backlog(load("backlog--Users-you-code-atrium.json"))
    expect("the backlog example totals", backlog?.total, 44)
    expect("and names the lane that is asking", backlog?.now, 2)
    check("and has something to open", backlog?.artifact != nil)

    let milestone = ProjectStatus.milestone(load("milestone--Users-you-code-atrium.json"))
    expect("the milestone example totals", milestone?.total, 8)
    expect("and counts completed outcomes", milestone?.complete, 3)
    expect("and keeps user-owned blockers separate", milestone?.waitingOnUser, 2)
    check("and has a private artifact to open", milestone?.artifact != nil)
    let milestoneLink = milestone?.linkRow(sessionID: "SESSION-1")
    expect("an incomplete milestone is visibly in progress", milestoneLink?["state"] as? String,
           "running")
    expect("its progress stays legible in the Links sheet", milestoneLink?["status"] as? String,
           "3/8")
    expect("its address is a same-origin authenticated route", milestoneLink?["url"] as? String,
           "/v1/sessions/SESSION-1/artifacts/milestone")
    check("its private filesystem path is not published",
          !(milestoneLink?["url"] as? String ?? "").contains("/Users/you/"))

    expect("the health example is ok", ProjectStatus.health(load("health--Users-you-code-atrium.json"))?.state, "ok")

    // `/links` receives the whole project registry row. The reader itself selects `health` once;
    // selecting it in RemoteServer too turns `health["health"]` into nil and silently drops the
    // public site even though the probe cache is green.
    let linkedProject = "/tmp/clawdline-linked-project-\(UUID().uuidString)"
    let linkedCache = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-linked-cache-\(UUID().uuidString)", isDirectory: true)
    try! FileManager.default.createDirectory(at: linkedCache,
                                             withIntermediateDirectories: true)
    let priorStatusDir = Config.shared.statusDir
    Config.shared.statusDir = linkedCache.path
    defer {
        Config.shared.statusDir = priorStatusDir
        try? FileManager.default.removeItem(at: linkedCache)
    }
    let linkedHealth = linkedCache.appendingPathComponent(
        "health-\(ProjectStatus.key(forPath: linkedProject)).json")
    try! #"{"state":"not_deployed","label":"Clawdline Cloud · 1/4 online","url":"https://clawdline.com/","components":[{"label":"Homepage","state":"online","kind":"site","url":"https://clawdline.com/","reason":"content_marker_present"},{"label":"Web console","state":"not_deployed","kind":"app","url":"https://clawdline.com/app","reason":"route_not_deployed"},{"label":"API","state":"not_deployed","kind":"service","url":"https://api.clawdline.com/healthz","reason":"dns_not_configured"},{"label":"Relay","state":"not_deployed","kind":"service","url":"https://relay.clawdline.com/v1/health","reason":"dns_not_configured"}]}"#.data(using: .utf8)!.write(to: linkedHealth)
    let linkedMilestone = linkedCache.appendingPathComponent(
        "milestone-\(ProjectStatus.key(forPath: linkedProject)).json")
    try! #"{"total":8,"complete":3,"waiting_on_user":2,"artifact":"/tmp/private-milestone.html"}"#.data(using: .utf8)!.write(to: linkedMilestone)
    let linked = ProjectStatus.read(
        cwd: linkedProject, remote: nil,
        registry: ["label": "clawdline.com", "url": "https://clawdline.com/"])
    expect("the Links reader keeps the registered site URL",
           linked.health?.url, "https://clawdline.com/")
    expect("the multi-surface receipt keeps all four rows", linked.healthComponents.count, 4)
    expect("the homepage keeps its online state", linked.healthComponents.first?.state, "online")
    expect("the console remains distinct from an outage",
           linked.healthComponents.dropFirst().first?.state, "not_deployed")
    expect("and keeps the reason a Links sheet can explain",
           linked.healthComponents.dropFirst().first?.reason, "route_not_deployed")
    expect("an online component maps to the old green dot",
           linked.healthComponents.first?.linkRow()?["state"] as? String, "ok")
    expect("while its exact status remains visible",
           linked.healthComponents.first?.linkRow()?["status"] as? String, "online")
    expect("not deployed maps to a red dot without becoming an outage",
           linked.healthComponents.dropFirst().first?.linkRow()?["state"] as? String, "down")
    expect("and spells out what the red dot means",
           linked.healthComponents.dropFirst().first?.linkRow()?["status"] as? String,
           "not deployed")
    expect("the same project read includes its milestone", linked.milestone?.complete, 3)

    let artifactRoot = linkedCache.appendingPathComponent("project", isDirectory: true)
    try! FileManager.default.createDirectory(at: artifactRoot,
                                             withIntermediateDirectories: true)
    let artifactFile = artifactRoot.appendingPathComponent("milestone.html")
    try! Data("<!doctype html><title>Private milestone</title>".utf8).write(to: artifactFile)
    let served = RemoteServer.projectArtifactResponse(cwd: artifactRoot.path,
                                                      artifact: artifactFile.path)
    expect("a registered in-project artifact is served", served.status, 200)
    expect("it is served as HTML", served.headers["Content-Type"],
           "text/html; charset=utf-8")
    check("its response blocks script execution",
          served.headers["Content-Security-Policy"]?.contains("script-src 'none'") == true)
    check("and the bytes are the registered file", String(data: served.body, encoding: .utf8)?
            .contains("Private milestone") == true)

    let outsideArtifact = linkedCache.appendingPathComponent("outside.html")
    try! Data("outside".utf8).write(to: outsideArtifact)
    expect("an artifact outside the project is refused",
           RemoteServer.projectArtifactResponse(cwd: artifactRoot.path,
                                                artifact: outsideArtifact.path).status, 404)
    let escapedArtifact = artifactRoot.appendingPathComponent("escaped.html")
    try! FileManager.default.createSymbolicLink(at: escapedArtifact,
                                                withDestinationURL: outsideArtifact)
    expect("a symlink cannot escape the project artifact jail",
           RemoteServer.projectArtifactResponse(cwd: artifactRoot.path,
                                                artifact: escapedArtifact.path).status, 404)

    // The file names in the page have to be the names the app looks for.
    expect("a path becomes a file name the documented way",
           ProjectStatus.key(forPath: "/Users/you/code/atrium"), "-Users-you-code-atrium")
    expect("a remote becomes the documented file name",
           Project.githubRepo("git@github.com:you/atrium.git"), "you-atrium")
    expect("https remotes too", Project.githubRepo("https://github.com/you/atrium"), "you-atrium")
    check("a non-GitHub remote has no run file",
          Project.githubRepo("git@gitlab.com:you/atrium.git") == nil)

    // Missing files are the normal case, not an error: most people will have none of them.
    check("nothing at all is fine", ProjectStatus.read(cwd: "/nowhere", remote: nil).isEmpty)
    expect("durations read the way a wait is said", ProjectStatus.duration(740), "12m 20s")
    expect("under a minute stays seconds", ProjectStatus.duration(45), "45s")
}

group("the project's mark and colour") {
    // The real shape of an entry claude-bestiary writes, atrium's arch.
    let art: [String: Any] = [
        "accent": "#5CBBA1",
        "bg": "#2F6B5E",
        "palette": ["W": "#EEF6F4"],
        "rows": [".WWWWW.", ".W...W.", ".W.W.W.", ".W...W."],
    ]
    let drawn = ProjectIcon.artGrid(art)
    expect("four rows", drawn?.cells.count, 4)
    expect("seven columns", drawn?.cells.first?.count, 7)
    expect("the accent is the project's colour",
           drawn?.accent, ProjectIcon.color(hex: "#5CBBA1"))
    expect("a dot is background", drawn?.cells[0][0], ProjectIcon.color(hex: "#2F6B5E"))
    expect("a letter is its palette colour", drawn?.cells[0][1], ProjectIcon.color(hex: "#EEF6F4"))
    // An icon with a typo in it should come out slightly wrong, not blank.
    let typo = ProjectIcon.artGrid(["rows": ["ZZ", "ZZ", "ZZ", "ZZ"], "bg": "#000000"])
    check("an unknown character falls back to the ground", typo != nil)
    check("rows of a different length are padded",
          ProjectIcon.artGrid(["rows": ["WW", "W", "W", "W"],
                               "palette": ["W": "#ffffff"]])?.cells[1].count == 2)

    // Longest match, because a session sits in a subfolder of the project that names it —
    // and a subfolder with a row of its own should win for anything inside it.
    let list: [String: [String: Any]] = [
        "/Users/me/code/atrium": ["label": "atrium"],
        "/Users/me/code/atrium/backend": ["label": "backend"],
    ]
    expect("the containing project names it",
           ProjectIcon.entry(forCwd: "/Users/me/code/atrium/frontend", in: list)?["label"] as? String,
           "atrium")
    expect("a nested row wins inside it",
           ProjectIcon.entry(forCwd: "/Users/me/code/atrium/backend/app", in: list)?["label"] as? String,
           "backend")
    check("a sibling with a shared prefix is not a match",
          ProjectIcon.entry(forCwd: "/Users/me/code/atrium-old", in: list) == nil)

    // The generated fallback: mirrored, with a body and eyes punched out of it.
    let cells = ProjectIcon.creatureCells(shape: 45)
    expect("five wide", cells[0].count, 5)
    expect("four tall", cells.count, 4)
    check("mirrored", cells[0][0] == cells[0][4] && cells[3][1] == cells[3][3])
    check("it has a body", cells[1].contains(1) || cells[2].contains(1))
    check("it has eyes", cells[1].contains(0) || cells[2].contains(0))
    check("out-of-range shapes still draw something",
          ProjectIcon.creatureCells(shape: -7).count == 4)

    // Swift seeds hashValue per process, so the same project would change colour every launch.
    expect("the same path always hashes the same",
           ProjectIcon.stableHash("/Users/me/code/atrium"),
           ProjectIcon.stableHash("/Users/me/code/atrium"))
    check("different paths differ",
          ProjectIcon.stableHash("/a") != ProjectIcon.stableHash("/b"))
}

group("which project a session is in") {
    // git's --porcelain=v2 is the documented, stable shape. The human one changes with git's
    // mood and with the user's language, which is exactly what a parser must not depend on.
    let real = """
    /Users/me/code/atrium
    # branch.oid 3f2c158e
    # branch.head main
    # branch.ab +4 -0
    1 .M N... 100644 100644 100644 abc abc Sources/App.swift
    1 M. N... 100644 100644 100644 def def README.md
    ? notes.txt
    """
    let info = Project.parse(real, fallbackPath: "/Users/me/code/atrium/frontend")
    expect("the repository names it, not the subfolder you happen to be in", info.name, "atrium")
    expect("the branch is read", info.branch, "main")
    expect("tracked changes and untracked both count as work not committed", info.dirty, 3)

    // Without a repository there is no output at all; the folder is still worth naming.
    let bare = Project.parse("", fallbackPath: "/Users/me/scratch/thing")
    expect("the path names it", bare.name, "thing")
    expect("no branch", bare.branch, "")
    expect("nothing dirty", bare.dirty, 0)

    let detached = Project.parse("/r\n# branch.head (detached)\n", fallbackPath: "/r")
    expect("detached is not a branch name", detached.branch, "")

    // Header lines are not files, and a path that happens to start with a digit is not a status
    // line either — the format puts a space after the code.
    let headersOnly = Project.parse("/r\n# branch.head main\n# branch.ab +0 -0\n", fallbackPath: "/r")
    expect("headers are not counted", headersOnly.dirty, 0)
    expect("conflicts count", Project.parse("/r\nu UU N... x\n", fallbackPath: "/r").dirty, 1)
}

group("the line that says what it is doing") {
    // Real captures, spinner glyphs and all.
    let busy = """
    ⎿  $ swift build 2>&1 | tail -3 (3s)
    ────────────────────────────────
    ✢ Generating… (18s · still thinking with xhigh effort)
    ────────────────────────────────
    ❯
    """
    expect("the live line is read", Activity.parse(busy),
           "Generating… (18s · still thinking with xhigh effort)")

    for glyph in ["✳", "✻", "✽", "✢", "✶", "·", "*"] {
        check("\(glyph) is a spinner", Activity.parse("\(glyph) Catapulting… (21s)") != nil)
    }
    expect("the glyph is not part of the message", Activity.parse("✻ Herding… (4s)"), "Herding… (4s)")

    // Real, and the reason the first version of this was wrong: past a minute the counter
    // changes shape, so the strip disappeared exactly when the wait was long enough to matter.
    expect("minutes are still a clock", Activity.parse("✢ Finagling… (5m 52s · ↓ 15.3k tokens)"),
           "Finagling… (5m 52s · ↓ 15.3k tokens)")
    check("hours too", Activity.parse("✻ Waiting… (1h 4m 9s)") != nil)
    check("a count of things is not a clock", Activity.parse("✳ Reading… (3 stages)") == nil)

    // A terminal is full of other lines that end in a duration. The glyph is what tells them
    // apart, and getting this wrong reports a session as busy when it has gone quiet.
    check("a tool result is not activity", Activity.parse("⎿  $ which swiftc (3s)") == nil)
    check("an echoed command is not activity", Activity.parse("print('target:… (3s)") == nil)
    check("a bullet with no counter is not activity", Activity.parse("· just a list item…") == nil)
    check("a counter with no ellipsis is not activity", Activity.parse("✳ done (3s)") == nil)
    check("an idle screen says nothing", Activity.parse("❯\n────\n  atrium  main") == nil)
    check("empty says nothing", Activity.parse("") == nil)

    // A tall window can still be holding spinner lines that scrolled past instead of being
    // erased. Reading one of those keeps a finished session looking busy forever.
    let stale = (["✢ Generating… (9s)"] + Array(repeating: "some output", count: 40) + ["❯"])
        .joined(separator: "\n")
    check("a line scrolled far above is not the live one", Activity.parse(stale) == nil)

    // The newest one wins when several are on screen at once.
    let several = ["✢ Generating… (9s)", "✻ Generating… (10s)", "✽ Generating… (11s)", "❯"]
        .joined(separator: "\n")
    expect("the last one is the live one", Activity.parse(several), "Generating… (11s)")

    // tmux captures arrive with the colours still in them. A line that begins with a colour code
    // does not begin with the character it looks like it begins with, so the glyph test saw ESC
    // and no tmux session ever once reported being busy.
    let coloured = "\u{1b}[38;5;215m✢\u{1b}[0m Generating… (18s)\n\u{1b}[2m❯\u{1b}[0m"
    expect("colour codes do not hide the spinner", Activity.parse(coloured), "Generating… (18s)")
    expect("plain text is left alone", Ansi.plain("nothing to strip"), "nothing to strip")
    expect("an OSC title goes too", Ansi.plain("\u{1b}]0;a title\u{07}body"), "body")
}

group("what a session is doing, from its screen") {
    // Every question Claude Code stops for is drawn the same way — numbered options with a caret
    // parked on one of them — and that shape is what is recognised, because the sentences beside
    // it are English, undocumented, and different from one release to the next.
    let permission = """
    ╭──────────────────────────────────────────────╮
    │ Bash command                                 │
    │                                              │
    │ rm -rf node_modules                          │
    │ Remove the installed packages                │
    │                                              │
    │ Do you want to proceed?                      │
    │ ❯ 1. Yes                                     │
    │   2. Yes, and don't ask again this session   │
    │   3. No, and tell Claude what to do instead  │
    ╰──────────────────────────────────────────────╯
    """
    expect("a question on screen is waiting for you", SessionState.read(permission), .waiting)

    let working = "✢ Generating… (18s · thinking)\n────────\n❯"
    expect("a spinner is working", SessionState.read(working), .working("Generating… (18s · thinking)"))
    expect("an idle prompt is idle", SessionState.read("❯\n────\n  atrium  main"), .idle)
    expect("an open hook gate does not turn a working screen into waiting",
           SessionState.read(working, hookWaiting: true),
           .working("Generating… (18s · thinking)"))
    expect("nor does it turn an idle screen into waiting",
           SessionState.read("❯\n────\n  atrium  main", hookWaiting: true), .idle)

    // Not the same answer as "doing nothing", and it must not be drawn like it. A session whose
    // screen could not be read is a session nothing is known about.
    expect("an unreadable screen is not an idle one", SessionState.read(nil), .unknown)
    expect("neither is an empty one", SessionState.read(""), .unknown)

    // The order of the two tests is the whole of the correctness. Claude Code draws its dialog
    // below whatever came before it and does not always erase the spinner line above — so asking
    // "is it busy?" first finds that stale line and hides the one row that needed a person.
    // Indented, because that is where it really sits. Claude Code draws its whole content area
    // two columns in and keeps the input prompt flush left — checked against a real capture on
    // 2026-08-19 — so a caret at column zero is the prompt and never a menu. These fixtures were
    // written flush left as a simplification, which encoded a layout that does not occur.
    //
    // **The risk in this direction is worth stating**: if some dialog somewhere is drawn at
    // column zero, its question stops being seen, and a phone stops being told that a session
    // needs somebody. That is the trade against the other direction, where every numbered list
    // anybody typed was announced as a question and the notice told them not to answer it.
    let stale = "✢ Generating… (9s)\n\n  ❯ 1. Yes\n    2. No, tell Claude what to do instead\n"
    expect("a stale spinner above a menu does not win", SessionState.read(stale), .waiting)

    // Claude writes numbered lists in prose all day. What prose does not do is put a selection
    // caret on one of them — and a menu never offers fewer than two things to choose between.
    let prose = """
    Here is what I found:
    1. the retry is not exponential
    2. the timeout is hard-coded
    3. neither is covered by a test
    ❯
    """
    expect("a numbered list in prose is not a menu", SessionState.read(prose), .idle)
    check("a caret on its own is not a menu", !SessionState.isChoosing("❯ 1. Yes"))
    check("a quoted list is not a menu either",
          !SessionState.isChoosing("> 1. first\n> 2. second"))
    check("options with no caret are a list that scrolled past",
          !SessionState.isChoosing("  1. Yes\n  2. No"))

    // The dialog is drawn inside a box, so the caret is never at the front of the captured line.
    check("the wall the dialog is drawn in does not hide the caret",
          SessionState.isChoosing("│ ❯ 1. Yes            │\n│   2. No             │"))
    check("colours do not hide it either",
          SessionState.read("  \u{1b}[1m❯ 1. Yes\u{1b}[0m\n    2. No") == .waiting)
    check("and a caret at the very front is the prompt, not a menu",
          !SessionState.isChoosing("❯ 1. tell me about this\n  2. and this"))

    // A menu that has scrolled off the top of the visible screen is not a menu you can answer.
    let scrolled = (["❯ 1. Yes", "  2. No"] + Array(repeating: "output", count: 40) + ["❯"])
        .joined(separator: "\n")
    expect("a menu far above the fold is gone", SessionState.read(scrolled), .idle)
}

group("the transcript behind the README pictures") {
    // The screenshots are shot from this file through the real parse and render. If it stops
    // yielding what the pictures show, they go quietly blank or quietly wrong — and a picture
    // that no longer matches the app is worse than no picture.
    let path = "docs/assets/demo-transcript.jsonl"
    let raw = (try? String(contentsOfFile: path, encoding: .utf8)) ?? ""
    check("the file is there", !raw.isEmpty)
    let entries = Transcript.parse(raw)
    check("it parses to a conversation", entries.count > 15)
    check("somebody asks something", entries.contains { $0.kind == .user })
    check("tools run", entries.contains { $0.kind == .tool })

    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let shown = Transcript.render(entries, size: 12.5, mono: mono).string
    // Each of these is a feature the pictures are there to show.
    check("a heading is rendered, not printed", shown.contains("Where the 40 seconds go")
          && !shown.contains("## Where"))
    check("a table is laid out", shown.contains("reads disk") && !shown.contains("|---|"))
    check("code survives", shown.contains("cache.tree(for: url)"))
    // The marker, not the wording: "3 steps" is "3 個動作" in the other language, and a test
    // that reads the label passes or fails on whatever language the machine happens to be set to.
    check("a run of tools is folded", shown.contains("⏵"))
    check("the folded run hides its detail", !shown.contains("Package.swift"))
    check("nothing real leaks in", !shown.contains("/Users/"))
}

group("newest first") {
    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let entries: [Transcript.Entry] = [
        .init(kind: .assistant, text: "alpha one\n\nalpha two", tool: nil, time: nil),
        .init(kind: .user, text: "bravo", tool: nil, time: nil),
        .init(kind: .tool, text: "x.swift", tool: "Read", time: nil),
        .init(kind: .toolResult, text: "ok", tool: nil, time: nil),
        .init(kind: .tool, text: "make", tool: "Bash", time: nil),
        .init(kind: .toolResult, text: "ok", tool: nil, time: nil),
        .init(kind: .assistant, text: "charlie", tool: nil, time: nil),
    ]
    func at(_ text: String, in s: String) -> Int {
        guard let r = s.range(of: text) else { return -1 }
        return s.distance(from: s.startIndex, to: r.lowerBound)
    }

    let down = Transcript.render(entries, size: 11, mono: mono).string
    let up = Transcript.render(entries, size: 11, mono: mono, newestFirst: true).string

    check("oldest first reads downwards",
          at("alpha one", in: down) < at("bravo", in: down))
    check("the run sits between them going down",
          at("bravo", in: down) < at("2", in: down) && at("2", in: down) < at("charlie", in: down))
    check("newest first reads upwards",
          at("charlie", in: up) < at("bravo", in: up) && at("bravo", in: up) < at("alpha one", in: up))

    // What flips is the conversation, not what was said: reversing entries or lines instead
    // would turn a single answer inside out.
    check("a message keeps its own order", at("alpha one", in: up) < at("alpha two", in: up))
    check("a message keeps its label", up.contains("CLAUDE\ncharlie"))
    check("the label leads in either order", down.contains("CLAUDE\ncharlie"))

    // The tail run is the one still going, which is about the conversation and not the screen.
    let tailRun: [Transcript.Entry] = [
        .init(kind: .assistant, text: "said", tool: nil, time: nil),
        .init(kind: .tool, text: "x.swift", tool: "Read", time: nil),
        .init(kind: .toolResult, text: "ok", tool: nil, time: nil),
        .init(kind: .tool, text: "make", tool: "Bash", time: nil),
        .init(kind: .toolResult, text: "ok", tool: nil, time: nil),
    ]
    let flipped = Transcript.render(tailRun, size: 11, mono: mono, newestFirst: true).string
    check("the last run stays open whichever way up", flipped.contains("x.swift"))
    check("and it is drawn first", at("x.swift", in: flipped) < at("said", in: flipped))
}

group("the transcript pane's text view") {
    let view = PromptController.makeOutputView()
    // A fresh NSTextView is TextKit 2, where NSTextTable does not exist and a table silently
    // loses its borders. Both of these fail without saying anything, which is why they are here.
    check("it is pinned to TextKit 1", view.textLayoutManager == nil)
    check("it has a TextKit 1 layout manager", view.layoutManager != nil)
    check("it grows vertically", view.isVerticallyResizable)
    check("its container tracks the width", view.textContainer?.widthTracksTextView == true)
    // Anything but the cursor in here wins over the renderer for every link equally, and the
    // pane has fold controls that are links without being hyperlinks.
    check("link styling is left to the renderer",
          view.linkTextAttributes?[.foregroundColor] == nil)
}
}
