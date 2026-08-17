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
    let board = NSPasteboard(name: NSPasteboard.Name("clawdline-tests-drop"))
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

    let deploy = ProjectStatus.deploy(load("ghrun-you-atrium.json"))
    expect("the deploy example is running", deploy?.state, "running")
    expect("with somewhere to click", deploy?.url?.hasPrefix("https://github.com/"), true)
    expect("and a bar to draw", ProjectStatus.bar(0.5, width: 8), "▰▰▰▰▱▱▱▱")

    let backlog = ProjectStatus.backlog(load("backlog--Users-you-code-atrium.json"))
    expect("the backlog example totals", backlog?.total, 44)
    expect("and names the lane that is asking", backlog?.now, 2)
    check("and has something to open", backlog?.artifact != nil)

    expect("the health example is ok", ProjectStatus.health(load("health--Users-you-code-atrium.json"))?.state, "ok")

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
    // The real shape of an entry claude-tools writes, atrium's arch.
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

// MARK: - Result

print("")
if failures.isEmpty {
    print("\(checks) checks passed")
    exit(0)
}
print("\(failures.count) of \(checks) checks failed:")
for f in failures { print("  ✗ \(f)") }
exit(1)
