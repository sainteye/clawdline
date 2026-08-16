import AppKit
import Carbon.HIToolbox
import Foundation

// A test binary rather than XCTest, for the same reason the app has no Xcode project:
// `swiftc` and nothing else. Run it with ./test.sh.
//
// What is worth testing here is the parsing and the arithmetic — the shapes that a
// contributor can quietly break and not notice. Anything that needs a window on screen is
// deliberately absent; a test that cannot run in CI is a test nobody runs.

var checks = 0
var failures: [String] = []

func check(_ name: String, _ ok: Bool, _ detail: @autoclosure () -> String = "") {
    checks += 1
    if !ok {
        let d = detail()
        failures.append(d.isEmpty ? name : "\(name) — \(d)")
    }
}

func expect<T: Equatable>(_ name: String, _ got: T, _ want: T) {
    check(name, got == want, "got \(got), want \(want)")
}

func expectClose(_ name: String, _ got: CGFloat, _ want: CGFloat, _ tolerance: CGFloat = 0.001) {
    check(name, abs(got - want) < tolerance, "got \(got), want \(want)")
}

func group(_ title: String, _ body: () -> Void) {
    let before = failures.count
    body()
    let mark = failures.count == before ? "✓" : "✗"
    print("  \(mark) \(title)")
}

func decodePack(_ json: String) -> MascotPack? {
    try? JSONDecoder().decode(MascotPack.self, from: Data(json.utf8))
}

/// A minimal pack the shape tests can mutate. 4x3, one pose, one routine.
let minimalPack = """
{
  "name": "Test",
  "grid": { "cols": 4, "rows": 3 },
  "palette": { "#": "accent", "o": "#000000", ".": "transparent" },
  "eyeChars": ["o"],
  "poses": { "stand": ["....", ".oo.", ".##."] },
  "routines": {
    "idle": {
      "duration": 2.0,
      "loop": true,
      "keys": [
        { "t": 0.0, "pose": "stand", "dy": 0,  "sx": 1.0 },
        { "t": 0.5, "dy": 10, "sx": 2.0 },
        { "t": 1.0, "dy": 0,  "sx": 1.0 }
      ]
    }
  }
}
"""

print("Clawdline tests")

// MARK: - Mascot packs that ship

group("shipped packs decode and validate") {
    let dir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "Resources/mascots"
    let names = (try? FileManager.default.contentsOfDirectory(atPath: dir))?
        .filter { $0.hasSuffix(".json") }.sorted() ?? []
    check("found packs to check in \(dir)", !names.isEmpty)
    for name in names {
        let url = URL(fileURLWithPath: dir).appendingPathComponent(name)
        guard let data = try? Data(contentsOf: url) else {
            check("\(name) readable", false); continue
        }
        guard let pack = try? JSONDecoder().decode(MascotPack.self, from: data) else {
            check("\(name) decodes", false); continue
        }
        check("\(name) validates", pack.validate() == nil, pack.validate() ?? "")
        // The five routines the app actually triggers. Missing ones fall back to idle, so
        // this is a warning in spirit — but a shipped pack should carry all of them.
        for routine in ["pop", "idle", "typing", "dance", "cheer"] {
            check("\(name) has routine \(routine)", pack.routines[routine] != nil)
        }
    }
}

// MARK: - Validation catches the mistakes an author actually makes

group("validation rejects malformed packs") {
    check("baseline pack is valid", decodePack(minimalPack)?.validate() == nil)

    let shortRow = minimalPack.replacingOccurrences(of: "\"....\"", with: "\"...\"")
    let shortRowError = decodePack(shortRow)?.validate()
    check("row shorter than grid.cols is caught", shortRowError != nil)
    check("that error names the pose", shortRowError?.contains("stand") ?? false, shortRowError ?? "nil")

    let missingRow = minimalPack.replacingOccurrences(of: "[\"....\", \".oo.\", \".##.\"]",
                                                      with: "[\"....\", \".oo.\"]")
    check("too few rows is caught", decodePack(missingRow)?.validate() != nil)

    let strayChar = minimalPack.replacingOccurrences(of: "\".##.\"", with: "\".@@.\"")
    let strayError = decodePack(strayChar)?.validate()
    check("character not in palette is caught", strayError != nil)
    check("that error names the character", strayError?.contains("@") ?? false, strayError ?? "nil")

    let badPose = minimalPack.replacingOccurrences(of: "\"pose\": \"stand\"", with: "\"pose\": \"nope\"")
    check("routine pointing at a missing pose is caught", decodePack(badPose)?.validate() != nil)

    let noIdle = minimalPack.replacingOccurrences(of: "\"idle\"", with: "\"walk\"")
    check("missing idle routine is caught", decodePack(noIdle)?.validate() != nil)

    let zeroDuration = minimalPack.replacingOccurrences(of: "\"duration\": 2.0", with: "\"duration\": 0")
    check("zero duration is caught", decodePack(zeroDuration)?.validate() != nil)
}

// MARK: - Keyframe sampling

group("routine sampling interpolates and steps") {
    guard let pack = decodePack(minimalPack) else { return check("pack decodes", false) }

    let start = pack.frame(routine: "idle", at: 0)
    expectClose("t=0 takes the first key", start.dy, 0)
    expect("pose comes from the first key", start.pose, "stand")

    let peak = pack.frame(routine: "idle", at: 1.0)      // half of a 2s routine
    expectClose("midpoint reaches the second key", peak.dy, 10)
    expectClose("scale interpolates too", peak.sx, 2.0)

    let quarter = pack.frame(routine: "idle", at: 0.5)   // between key 0 and key 1
    expectClose("quarter way is halfway between keys", quarter.dy, 5)

    // Looping means t past the duration wraps rather than sticking at the end.
    let wrapped = pack.frame(routine: "idle", at: 3.0)   // 3s into a 2s loop == 1s
    expectClose("a looping routine wraps", wrapped.dy, 10)

    // A pose named on an earlier key holds until another key names a different one.
    let late = pack.frame(routine: "idle", at: 1.6)
    expect("pose steps rather than interpolating", late.pose, "stand")

    // An unknown routine falls back to idle instead of drawing nothing.
    expect("unknown routine falls back to idle", pack.frame(routine: "nope", at: 0).pose, "stand")
}

group("non-looping routines clamp at the end") {
    let once = minimalPack.replacingOccurrences(of: "\"loop\": true", with: "\"loop\": false")
    guard let pack = decodePack(once) else { return check("pack decodes", false) }
    let past = pack.frame(routine: "idle", at: 99)
    expectClose("time past the end clamps to the last key", past.dy, 0)
}

// MARK: - Colour and skin

group("palette colours parse") {
    guard let pack = decodePack(minimalPack) else { return check("pack decodes", false) }
    let accent = NSColor.red
    expect("accent follows the app tint", pack.color(for: "#", accent: accent), accent)
    check("transparent paints nothing", pack.color(for: ".", accent: accent) == nil)
    check("a hex colour is not the accent", pack.color(for: "o", accent: accent) != accent)
    check("a character outside the palette paints nothing",
          pack.color(for: "@", accent: accent) == nil)
}

group("hex colours") {
    check("#RGB expands", NSColor(hex: "#f00") != nil)
    check("#RRGGBB parses", NSColor(hex: "#ff0000") != nil)
    check("#RRGGBBAA parses", NSColor(hex: "#ff000080") != nil)
    check("no leading hash still parses", NSColor(hex: "ff0000") != nil)
    check("nonsense is rejected", NSColor(hex: "#zzz") == nil)
    check("wrong length is rejected", NSColor(hex: "#ff00") == nil)

    if let c = NSColor(hex: "#ff8000")?.usingColorSpace(.sRGB) {
        expectClose("red channel", c.redComponent, 1.0, 0.01)
        expectClose("green channel", c.greenComponent, 0.502, 0.01)
        expectClose("blue channel", c.blueComponent, 0.0, 0.01)
    } else {
        check("#ff8000 parses", false)
    }
}

group("closed eyes fill with the right colour") {
    guard let withSkin = decodePack(minimalPack.replacingOccurrences(
        of: "\"eyeChars\": [\"o\"],", with: "\"eyeChars\": [\"o\"], \"skin\": \"#\","))
    else { return check("pack decodes", false) }
    expect("skin is used when named", withSkin.skinCharacter, "#")

    guard let noSkin = decodePack(minimalPack) else { return check("pack decodes", false) }
    // Without an explicit skin, fall back to whatever is mapped to accent.
    expect("falls back to the accent character", noSkin.skinCharacter, "#")
}

// MARK: - Display geometry

group("display size is independent of grid resolution") {
    // A pack twice as fine should draw the same size, not half of it.
    let coarse = decodePack(minimalPack)!
    let fineJSON = minimalPack
        .replacingOccurrences(of: "\"cols\": 4, \"rows\": 3", with: "\"cols\": 8, \"rows\": 6")
        .replacingOccurrences(of: "[\"....\", \".oo.\", \".##.\"]",
                              with: "[\"........\", \"........\", \"..oooo..\", \"..oooo..\", \"..####..\", \"..####..\"]")
    guard let fine = decodePack(fineJSON) else { return check("fine pack decodes", false) }
    check("fine pack is valid", fine.validate() == nil, fine.validate() ?? "")

    // Neither declares a display block, so both take the default height.
    expectClose("same sprite height regardless of grid", fine.spriteSize.height, coarse.spriteSize.height, 6)

    let sized = decodePack(minimalPack.replacingOccurrences(
        of: "\"grid\"", with: "\"display\": { \"height\": 60, \"overlap\": 5 }, \"grid\""))!
    expectClose("declared height is honoured", sized.spriteSize.height, 60, 3)
    expectClose("declared overlap is honoured", sized.overlap, 5)
    check("the box leaves room above the sprite", sized.boxSize.height > sized.spriteSize.height)
    check("the box leaves room beside the sprite", sized.boxSize.width > sized.spriteSize.width)
}

// MARK: - Hotkeys

group("hotkey specs parse") {
    check("option+space", HotKey.parse("option+space")?.0 == UInt32(kVK_Space))
    expect("option modifier", HotKey.parse("option+space")?.1, UInt32(optionKey))
    expect("two modifiers combine", HotKey.parse("cmd+shift+k")?.1, UInt32(cmdKey) | UInt32(shiftKey))
    check("symbols work as well as words", HotKey.parse("⌥space")?.1 == HotKey.parse("option+space")?.1)
    check("alt is a synonym for option", HotKey.parse("alt+space")?.1 == HotKey.parse("option+space")?.1)
    check("an unknown key is rejected", HotKey.parse("option+banana") == nil)
    check("modifiers with no key are rejected", HotKey.parse("cmd+shift") == nil)
    expect("display is readable", HotKey.display("option+space"), "⌥Space")
    expect("display orders modifiers", HotKey.display("cmd+shift+k"), "⇧⌘K")
}

// MARK: - Target parsing

group("iTerm session labels drop the job name") {
    func label(_ name: String) -> String {
        TargetSession(backend: .iterm, id: "x", name: name, tty: "/dev/ttys1",
                      windowIndex: 0, tabIndex: 0, isClaude: true).label
    }
    expect("trailing job name is removed", label("✳ fix the thing (python)"), "✳ fix the thing")
    expect("a name without one is untouched", label("✳ fix the thing"), "✳ fix the thing")
    expect("parentheses mid-name survive", label("build (debug) now"), "build (debug) now")
    expect("an empty name falls back to coordinates", label("   "), "⌘1-1")
}

group("ps output picks out real claude processes") {
    let ps = """
    ttys006  claude
    ttys013  /opt/homebrew/bin/claude --resume
    ttys023  bash /Users/me/.claude/statusline-command.sh
    ttys031  node /Users/me/project/claude-helper.js
    ttys044  -zsh
    ??       /Applications/Claude.app/Contents/MacOS/Claude
    ttys055  vim claude.md
    """
    let found = ITerm.parseClaudeTTYs(ps)
    check("a bare claude counts", found.contains("ttys006"))
    check("an absolute path to claude counts", found.contains("ttys013"))
    check("a script living under .claude does not", !found.contains("ttys023"))
    check("a program merely named claude-something does not", !found.contains("ttys031"))
    check("a shell does not", !found.contains("ttys044"))
    check("a process with no tty is skipped", !found.contains("??"))
    check("an argument that mentions claude does not count", !found.contains("ttys055"))
    expect("exactly two matches", found.count, 2)
}

group("tmux pane listing parses") {
    let sep = "\u{1}"
    let rows = [
        ["%3", "/dev/ttys080", "claude", "work", "0", "1", "✳ writing tests"],
        ["%4", "/dev/ttys081", "zsh", "work", "0", "2", "zsh"],
        ["%5", "/dev/ttys082", "claude", "other", "2", "0", ""],
    ].map { $0.joined(separator: sep) }.joined(separator: "\n")

    let panes = Tmux.parsePanes(rows)
    expect("every pane is parsed", panes.count, 3)
    expect("pane id is kept", panes[0].id, "%3")
    expect("backend is tmux", panes[0].backend, Backend.tmux)
    expect("tty is kept", panes[0].tty, "/dev/ttys080")
    check("a claude pane is flagged", panes[0].isClaude)
    check("a shell pane is not", !panes[1].isClaude)
    expect("a pane title is used as the label", panes[0].label, "✳ writing tests")
    // When the title is just the command name it says nothing, so tmux coordinates are better.
    expect("a title equal to the command falls back", panes[1].label, "work:0.2")
    expect("an empty title falls back too", panes[2].label, "other:2.0")

    expect("a malformed line is skipped", Tmux.parsePanes("not\u{1}enough\u{1}fields").count, 0)
    expect("empty input yields nothing", Tmux.parsePanes("").count, 0)
}

// MARK: - Terminal escapes

group("ansi: plain text is left alone") {
    check("no escapes detected", !Ansi.hasEscapes("just words"))
    check("escapes detected", Ansi.hasEscapes("a \u{1b}[31mb"))

    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let a = Ansi.attributed("hello", font: font, defaultColor: .red)
    expect("text survives", a.string, "hello")
    expect("one run", a.length, 5)
}

group("ansi: colours") {
    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    func colour(_ s: String, at i: Int) -> NSColor? {
        let a = Ansi.attributed(s, font: font, defaultColor: .white)
        guard i < a.length else { return nil }
        return a.attribute(.foregroundColor, at: i, effectiveRange: nil) as? NSColor
    }

    let basic = Ansi.attributed("\u{1b}[31mred\u{1b}[0m plain", font: font, defaultColor: .white)
    expect("escapes are removed from the text", basic.string, "red plain")
    check("the coloured run is not the default", colour("\u{1b}[31mred\u{1b}[0m plain", at: 0) != NSColor.white)
    expect("reset returns to the default", colour("\u{1b}[31mred\u{1b}[0m plain", at: 5), NSColor.white)

    check("bright colours differ from their base",
          colour("\u{1b}[31ma", at: 0) != colour("\u{1b}[91ma", at: 0))
    check("256-colour is parsed", colour("\u{1b}[38;5;196ma", at: 0) != NSColor.white)
    check("truecolour is parsed", colour("\u{1b}[38;2;10;200;30ma", at: 0) != NSColor.white)
    expect("39 goes back to the default", colour("\u{1b}[31ma\u{1b}[39mb", at: 1), NSColor.white)
    expect("bare [m resets", colour("\u{1b}[31ma\u{1b}[mb", at: 1), NSColor.white)
}

group("ansi: everything that is not colour is dropped") {
    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    func text(_ s: String) -> String { Ansi.attributed(s, font: font, defaultColor: .white).string }

    expect("clear screen", text("\u{1b}[2Jhello"), "hello")
    expect("cursor move", text("\u{1b}[10;20Hhello"), "hello")
    expect("erase line", text("a\u{1b}[Kb"), "ab")
    expect("OSC title ending in BEL", text("\u{1b}]0;my title\u{07}hello"), "hello")
    expect("OSC title ending in ST", text("\u{1b}]0;my title\u{1b}\\hello"), "hello")
    expect("a lone escape is not printed", text("a\u{1b}Mb"), "ab")
    expect("newlines survive", text("a\u{1b}[31m\nb"), "a\nb")
}

group("ansi: bold") {
    let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let a = Ansi.attributed("\u{1b}[1mbold\u{1b}[22mplain", font: font, defaultColor: .white)
    let boldFont = a.attribute(.font, at: 0, effectiveRange: nil) as? NSFont
    let plainFont = a.attribute(.font, at: 5, effectiveRange: nil) as? NSFont
    check("bold run uses a different face", boldFont != plainFont)
    expect("22 turns it off again", plainFont, font)
}

// MARK: - Claude Code transcripts

let sampleTranscript = #"""
{"type":"summary","summary":"ignored"}
{"type":"user","timestamp":"2026-08-16T04:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"add a retry"}]}}
{"type":"assistant","timestamp":"2026-08-16T04:00:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Looking at `upload.rb` now."},{"type":"tool_use","name":"Bash","input":{"command":"rg retry upload.rb","description":"search"}}]}}
{"type":"user","timestamp":"2026-08-16T04:00:07.000Z","message":{"role":"user","content":[{"type":"tool_result","content":"upload.rb:42: no retry\nupload.rb:99: none"}]}}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent chatter"}]}}
{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"bookkeeping"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"Done."}]}}
{"type":"user","message":{"role":"user","content":"plain string content"}}
not json at all
"""#

group("transcript parsing") {
    let entries = Transcript.parse(sampleTranscript)
    expect("blocks become entries", entries.count, 6)

    expect("first is the user", entries[0].kind, Transcript.Entry.Kind.user)
    expect("user text survives", entries[0].text, "add a retry")
    check("timestamps are read", entries[0].time != nil)

    expect("assistant prose", entries[1].kind, Transcript.Entry.Kind.assistant)
    expect("tool call is its own entry", entries[2].kind, Transcript.Entry.Kind.tool)
    expect("tool name is kept", entries[2].tool, "Bash")
    expect("the command is the summary, not the description", entries[2].text, "rg retry upload.rb")

    expect("tool result", entries[3].kind, Transcript.Entry.Kind.toolResult)
    expect("only the first line of a result", entries[3].text, "upload.rb:42: no retry")

    check("sidechains are skipped", !entries.contains { $0.text == "subagent chatter" })
    check("meta records are skipped", !entries.contains { $0.text == "bookkeeping" })
    check("thinking blocks are skipped", !entries.contains { $0.text == "hidden" })
    check("a string content still parses", entries.contains { $0.text == "plain string content" })
    check("unparseable lines are ignored rather than fatal", true)

    expect("the limit keeps the tail", Transcript.parse(sampleTranscript, limit: 2).count, 2)
}

group("transcript tool summaries") {
    expect("command wins", Transcript.summarise(input: ["description": "d", "command": "ls -l"]), "ls -l")
    expect("then a path", Transcript.summarise(input: ["description": "d", "file_path": "/tmp/a"]), "/tmp/a")
    expect("then a pattern", Transcript.summarise(input: ["pattern": "TODO"]), "TODO")
    expect("only the first line", Transcript.summarise(input: ["command": "one\ntwo"]), "one")
    expect("nothing usable is empty", Transcript.summarise(input: ["weird": 3]), "")
    expect("not a dictionary is empty", Transcript.summarise(input: "string"), "")
}

group("transcript titles") {
    expect("status glyph is stripped", Transcript.cleanTitle("✳ fix the thing"), "fix the thing")
    expect("job name is stripped", Transcript.cleanTitle("✳ fix the thing (python)"), "fix the thing")
    expect("a plain title is untouched", Transcript.cleanTitle("fix the thing"), "fix the thing")
    expect("works on CJK", Transcript.cleanTitle("◑ 將輸入框移到中上方 (node)"), "將輸入框移到中上方")
    expect("empty stays empty", Transcript.cleanTitle("   "), "")
}

group("transcript project directory") {
    let dir = Transcript.projectDirectory(forCwd: "/Users/me/code/atrium")
    expect("separators become dashes", dir.lastPathComponent, "-Users-me-code-atrium")
    check("under ~/.claude/projects", dir.path.contains("/.claude/projects/"))
}

group("transcript rendering") {
    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let text = Transcript.render(Transcript.parse(sampleTranscript), size: 11.5, mono: mono).string
    check("the speaker is named", text.contains("YOU"))
    check("so is the other one", text.contains("CLAUDE"))
    check("the prose is there", text.contains("add a retry"))
    check("tool calls are marked", text.contains("⏺"))
    check("the tool is named", text.contains("Bash"))
    check("results are marked", text.contains("→"))

    // A fenced block should lose its fence and keep its contents.
    let fenced = [Transcript.Entry(kind: .assistant, text: "run:\n```bash\nls -l\n```\ndone",
                                  tool: nil, time: nil)]
    let out = Transcript.render(fenced, size: 11.5, mono: mono)
    check("fences are removed", !out.string.contains("```"))
    check("the code survives", out.string.contains("ls -l"))
    check("the language tag is not printed as content", !out.string.contains("bash\n"))

    // The code should actually be set in the mono face, which is the point of the exercise.
    if let range = out.string.range(of: "ls -l") {
        let i = out.string.distance(from: out.string.startIndex, to: range.lowerBound)
        let f = out.attribute(.font, at: i, effectiveRange: nil) as? NSFont
        expect("code is monospace", f?.fontName, mono.fontName)
    } else {
        check("found the code run", false)
    }
}

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

// MARK: - Result

print("")
if failures.isEmpty {
    print("\(checks) checks passed")
    exit(0)
}
print("\(failures.count) of \(checks) checks failed:")
for f in failures { print("  ✗ \(f)") }
exit(1)
