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

/// What build.sh stamps into the bundle, read out of build.sh itself — the tests have no bundle
/// to ask, and a version that lives in two places is a version that disagrees with itself.
func appVersion() -> String {
    let script = (try? String(contentsOfFile: "build.sh", encoding: .utf8)) ?? ""
    guard let line = script.split(separator: "\n").first(where: {
        $0.contains("CFBundleShortVersionString")
    }) else { return "" }
    guard let open = line.range(of: "<string>"),
          let close = line.range(of: "</string>", range: open.upperBound..<line.endIndex)
    else { return "" }
    return String(line[open.upperBound..<close.lowerBound])
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

group("iTerm session labels drop the job name and the status glyph") {
    func label(_ name: String) -> String {
        TargetSession(backend: .iterm, id: "x", name: name, tty: "/dev/ttys1",
                      windowIndex: 0, tabIndex: 0, isClaude: true).label
    }
    // Both ends come off: iTerm appends the job name, Claude Code prefixes a status glyph that
    // is now a frame of an animation rather than a fixed mark.
    expect("trailing job name is removed", label("✳ fix the thing (python)"), "fix the thing")
    expect("and the glyph on the front with it", label("✳ fix the thing"), "fix the thing")
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
    expect("a pane title is used as the label", panes[0].label, "writing tests")
    // When the title is just the command name it says nothing, so tmux coordinates are better.
    expect("a title equal to the command falls back", panes[1].label, "work:0.2")
    expect("an empty title falls back too", panes[2].label, "other:2.0")

    expect("a malformed line is skipped", Tmux.parsePanes("not\u{1}enough\u{1}fields").count, 0)
    expect("empty input yields nothing", Tmux.parsePanes("").count, 0)

    // The installer everybody now has puts the binary at ~/.local/share/claude/versions/2.1.233
    // and symlinks `claude` at it. tmux reports the process name the kernel holds — the basename
    // of the executable — so the pane running Claude Code calls itself "2.1.233", and every tmux
    // session was listed as an ordinary shell. `ps` reads argv, which still says claude.
    let versioned = ["%9", "/dev/ttys061", "2.1.233", "work", "0", "0", "✳ a task"]
        .joined(separator: sep)
    check("a versioned binary is not recognised by name alone",
          !Tmux.parsePanes(versioned)[0].isClaude)
    check("but the tty says what the name does not",
          Tmux.parsePanes(versioned, claudeTTYs: ["ttys061"])[0].isClaude)
    check("and a shell on a tty nobody claimed is still a shell",
          !Tmux.parsePanes(rows, claudeTTYs: ["ttys080"])[1].isClaude)
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

    // It reads backwards from the newest line and stops as soon as it has enough, because
    // parsing every line of an eight-megabyte tail to keep four hundred entries was a third of a
    // second on the path a session switch runs. What comes back has to be indistinguishable from
    // having read the whole thing: the *newest* entries, still in the order they were written.
    let tail2 = Transcript.parse(sampleTranscript, limit: 2)
    expect("the tail is the newest, not the oldest", tail2.last?.text, "plain string content")
    expect("and it is still in reading order", tail2.first?.text, "Done.")

    // One row can yield several entries, so the limit can be reached in the middle of a row.
    let tail4 = Transcript.parse(sampleTranscript, limit: 4)
    expect("a row that straddles the limit is trimmed to it", tail4.count, 4)
    expect("from the newest end", tail4.map(\.text).suffix(2).joined(separator: "|"),
           "Done.|plain string content")

    // A last line with no newline after it is the normal shape of a file being appended to.
    let unterminated = #"{"type":"user","message":{"role":"user","content":"last word"}}"#
    expect("a line with no newline after it is still read",
           Transcript.parse(sampleTranscript + "\n" + unterminated).last?.text, "last word")
    expect("a single line with no newline at all parses",
           Transcript.parse(unterminated).count, 1)
    expect("blank lines between records are not entries",
           Transcript.parse("\n\n" + unterminated + "\n\n").count, 1)
    expect("nothing at all is nothing", Transcript.parse("").count, 0)

    // The scan walks the UTF-8 view a byte at a time and then slices the String with the indices
    // it stopped on. Those stops are always just after an ASCII newline, so they are always on a
    // character boundary — but if that ever stopped being true the slice would not be wrong, it
    // would trap, and it would trap in the pane rather than in a test. So: lines of characters
    // that are three and four bytes long, on both sides of the separator.
    let wide = [
        #"{"type":"user","message":{"role":"user","content":"把重試改成指數退避"}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":"好，改在 upload.rb 裡 🐈‍⬛"}}"#,
        #"{"type":"user","message":{"role":"user","content":"謝謝"}}"#,
    ].joined(separator: "\n")
    let multibyte = Transcript.parse(wide)
    expect("multi-byte lines are read", multibyte.count, 3)
    expect("and come back whole", multibyte[0].text, "把重試改成指數退避")
    expect("emoji with a zero-width joiner survive the slice", multibyte[1].text,
           "好，改在 upload.rb 裡 🐈‍⬛")
    expect("and the newest is still last", multibyte[2].text, "謝謝")
    expect("stopping early does not cut a character in half",
           Transcript.parse(wide, limit: 1)[0].text, "謝謝")
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

group("giving the pasteboard back") {
    // Borrowing the pasteboard and not returning it costs somebody whatever they copied five
    // minutes ago, for a feature they never asked for.
    let pb = NSPasteboard(name: NSPasteboard.Name("dev.sainteye.clawdline.tests"))
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
        "es:settingsGeneral",   // "General" is Spanish
        "it:hintOutput",        // and "output" is Italian
        "it:stackActionLogs",   // as is "log"
        "id:stackActionLogs",   // and in Indonesian
        "de:settingsTunnelHostname", // "Hostname" is the German word for it as well
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

    // Not the same answer as "doing nothing", and it must not be drawn like it. A session whose
    // screen could not be read is a session nothing is known about.
    expect("an unreadable screen is not an idle one", SessionState.read(nil), .unknown)
    expect("neither is an empty one", SessionState.read(""), .unknown)

    // The order of the two tests is the whole of the correctness. Claude Code draws its dialog
    // below whatever came before it and does not always erase the spinner line above — so asking
    // "is it busy?" first finds that stale line and hides the one row that needed a person.
    let stale = "✢ Generating… (9s)\n\n❯ 1. Yes\n  2. No, tell Claude what to do instead\n"
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
          SessionState.read("\u{1b}[1m❯ 1. Yes\u{1b}[0m\n  2. No") == .waiting)

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

// MARK: - Dev stacks

group("devstack: a project describes its stack") {
    let json = """
    {"version": 1, "name": "cairn",
     "status": "make stack-status",
     "restart": "make stack-restart P={process}",
     "logs": "make stack-logs P={process} N={lines}"}
    """
    let spec = DevStack.parse(Data(json.utf8), root: "/tmp/cairn")
    check("it parses", spec != nil)
    expect("name", spec?.name, "cairn")
    expect("root", spec?.root, "/tmp/cairn")
    expect("status command", spec?.status, "make stack-status")
    check("absent commands stay absent", spec?.up == nil)
}

group("devstack: a file with nothing but a name still works") {
    // The contract's floor. Everything but `name` is optional, and a reader that refused this
    // would be telling adopters to write more before they can see anything at all.
    let spec = DevStack.parse(Data("{}".utf8), root: "/tmp/whatever")
    check("it parses", spec != nil)
    expect("name falls back to the directory", spec?.name, "whatever")
    check("nothing is declared", spec?.declared.isEmpty == true)
    check("no commands", spec?.status == nil && spec?.up == nil)
}

group("devstack: tier 0 declares ports and nothing else") {
    let json = """
    {"name": "app", "up": "make dev",
     "processes": [{"name": "api", "port": 8002}, {"name": "web", "port": 3001}]}
    """
    let spec = DevStack.parse(Data(json.utf8), root: "/tmp/app")
    expect("two processes declared", spec?.declared.count, 2)
    expect("first port", spec?.declared.first?.port, 8002)
    check("no status command is fine", spec?.status == nil)
}

group("devstack: garbage is refused, not guessed at") {
    check("not JSON", DevStack.parse(Data("nope".utf8), root: "/tmp/x") == nil)
    check("JSON but not an object", DevStack.parse(Data("[1,2]".utf8), root: "/tmp/x") == nil)
}

group("devstack: the fingerprint follows the bytes") {
    let a = DevStack.fingerprint(Data("{\"name\":\"a\"}".utf8))
    let b = DevStack.fingerprint(Data("{\"name\":\"a\"}".utf8))
    let c = DevStack.fingerprint(Data("{\"name\":\"b\"}".utf8))
    check("same bytes, same fingerprint", a == b)
    // Trust is tied to this. If an edit did not change it, adding a command to a trusted file
    // would run without ever being agreed to.
    check("different bytes, different fingerprint", a != c)
}

group("devstack: the state document") {
    let json = """
    {"state": "partial", "updated_at": 1786949616,
     "processes": [
       {"name": "api", "state": "healthy", "port": 8002, "pid": 7970},
       {"name": "build-web", "state": "exited", "exit_code": 1,
        "error": "Cannot find module next/font/google"},
       {"name": "web", "state": "healthy", "pid": 8243}]}
    """
    let s = DevStack.parseState(Data(json.utf8))
    check("it parses", s != nil)
    expect("verdict", s?.state, "partial")
    expect("three processes", s?.processes.count, 3)
    expect("two are up", s?.upCount, 2)
    expect("the broken one is named", s?.brokenNames, ["build-web"])
    expect("its error travels with it", s?.processes[1].error,
           "Cannot find module next/font/google")
    // `completed` is a one-shot that finished on purpose. Drawing it as a failure would put a
    // red dot on every successful build, which trains people to ignore red dots.
    let done = DevStack.parseState(Data("""
    {"processes": [{"name": "build", "state": "completed"}]}
    """.utf8))
    expect("completed counts as up", done?.upCount, 1)
    expect("and the verdict is derived", done?.state, "running")
}

group("devstack: a state document missing its verdict still lights the dot") {
    // These files are written by shell scripts. One that forgot a key should degrade, not vanish.
    let s = DevStack.parseState(Data("""
    {"processes": [{"name": "api", "state": "exited", "exit_code": 1}]}
    """.utf8))
    expect("derived from the processes", s?.state, "partial")
}

group("devstack: commands substitute rather than concatenate") {
    // Where the process name goes is the project's decision: `make stack-restart P=api` and
    // `overmind restart api` do not put it in the same place.
    expect("make style", DevStack.expand("make stack-restart P={process}", process: "api"),
           "make stack-restart P='api'")
    expect("bare argument", DevStack.expand("overmind restart {process}", process: "web"),
           "overmind restart 'web'")
    expect("line counts too",
           DevStack.expand("make logs P={process} N={lines}", process: "api", lines: 50),
           "make logs P='api' N=50")
    // "Restart everything" drops the whole word, so no `P=` is left dangling to be read as an
    // empty process name.
    expect("no process drops the word",
           DevStack.expand("make stack-restart P={process}", process: nil),
           "make stack-restart")
    expect("quoting survives a hostile name",
           DevStack.expand("x {process}", process: "a'b"), "x 'a'\\''b'")
}

group("devstack: the documented example files") {
    // docs/devstack.md tells other people how to write these. The examples beside it are parsed
    // here with the same code the app uses, so the page cannot quietly stop being true.
    func load(_ name: String) -> Data? {
        try? Data(contentsOf: URL(fileURLWithPath: "docs/examples/" + name))
    }
    let tier1 = load("devstack.json").flatMap { DevStack.parse($0, root: "/Users/you/code/atrium") }
    check("the tier 1 example is there", tier1 != nil)
    expect("it names itself", tier1?.name, "atrium")
    check("it can be asked for state", tier1?.status != nil)
    check("and restarted per process", tier1?.restart?.contains("{process}") == true)

    let tier0 = load("devstack-tier0.json").flatMap { DevStack.parse($0, root: "/tmp/myapp") }
    check("the tier 0 example is there", tier0 != nil)
    expect("it declares its ports", tier0?.declared.count, 2)
    check("without a status command", tier0?.status == nil)
    // The rung that makes this a format rather than an integration: no supervisor, still visible.
    expect("so it is read by probing", DevStack.probeDeclared(tier0!).processes.count, 2)

    let state = load("devstack-state.json").flatMap { DevStack.parseState($0) }
    check("the state example is there", state != nil)
    expect("its verdict", state?.state, "partial")
    expect("four of five up", state?.upCount, 4)
    expect("and it names what broke", state?.brokenNames, ["build-web"])
    // The one thing in a stack that is a place rather than a number. The row turns it into a
    // link, so confirming a site is really up is a click and not a retyped address.
    expect("the tunnel carries the address it serves",
           state?.processes.first { $0.name == "tunnel" }?.url, "https://dev.example.com")
}

group("stack logs: unwrapping the supervisor's envelope") {
    let line = #"{"level":"error","process":"api","replica":0,"message":"INFO: GET /health 200"}"#
    expect("the line the server actually printed", StackLog.unwrap(line), "INFO: GET /health 200")
    // A project whose `logs` is plain `tail` must not come out empty for not being JSON.
    expect("plain lines pass through", StackLog.unwrap("just a line"), "just a line")
    expect("so does something JSON-shaped but not an envelope",
           StackLog.unwrap(#"{"a":1}"#), "")
    expect("and a brace that starts a sentence", StackLog.unwrap("{not json"), "{not json")
}

group("stack logs: severity comes from the text, not the pipe") {
    // process-compose labels everything on stderr as level=error, and uvicorn, next, mkdocs and
    // cloudflared all log their ordinary progress there. Believing that field paints a healthy
    // stack entirely red, after which nobody reads the colour again.
    check("an INFO line is not an error", StackLog.level(of: "INFO: GET /health 200") == .normal)
    check("an ERROR line is", StackLog.level(of: "ERROR - could not connect") == .error)
    check("a warning is a warning", StackLog.level(of: "WARNING: deprecated") == .warning)
    // A request path is not a severity.
    check("a path containing the word does not count",
          StackLog.level(of: "GET /api/v1/error-report 200") == .normal)
}

group("stack logs: the three shapes a logs command can hand back") {
    // tail over files, with banners.
    let files = """
    ==> logs/stack/api.log <==
    {"level":"info","process":"api","message":"listening on 8002"}
    ==> logs/stack/web.log <==
    {"level":"info","process":"web","message":"ready"}
    """
    let a = StackLog.entries(files)
    expect("one entry per line", a.count, 2)
    expect("named by the file, without path or suffix", a.first?.process, "api")
    expect("and unwrapped", a.first?.message, "listening on 8002")

    // process-compose's own multi-process prefix.
    let prefixed = "[web\t]  ✓ Ready in 244ms\n[api\t]  INFO: started"
    let b = StackLog.entries(prefixed)
    expect("two entries", b.count, 2)
    expect("first is web", b.first?.process, "web")
    expect("with the bracket gone", b.first?.message, "✓ Ready in 244ms")

    // A single process: clean lines, no prefix, no envelope. Nothing to strip.
    let plain = StackLog.entries("INFO: one\nINFO: two")
    expect("both kept", plain.count, 2)
    expect("verbatim", plain.first?.message, "INFO: one")
    expect("with no process to name", plain.first?.process, "")
}

group("stack logs: the timestamp every line starts with") {
    // Dimming it is most of what makes a wall of log readable — it is the same eighteen
    // characters on every row and the least informative part of each.
    let line = "2026-08-18 10:39:28 INFO    cairn.access GET /health → 200"
    expect("date and time, up to the following space",
           StackLog.timestampLength(line), 20)
    expect("a bare clock too", StackLog.timestampLength("10:39:28 INFO x"), 9)
    expect("nothing to dim when there is no stamp",
           StackLog.timestampLength("INFO: 127.0.0.1 - GET /"), 0)
    // A version number is not a time.
    expect("and not a version", StackLog.timestampLength("v1.120.0 released"), 0)
}

group("devstack: port probing") {
    // Port 1 needs root to bind, so nothing on a normal machine is listening there.
    check("nothing answers on port 1", !DevStack.isListening(port: 1))
    check("out of range is not listening", !DevStack.isListening(port: 0))
    check("also out of range", !DevStack.isListening(port: 70000))
}


group("the label a tab shows, without the animation on the front") {
    // Claude Code used to put a fixed ✳ in front of the title, which was worth keeping. 2.1.228
    // cycles half circles through it instead, so the same tab reads differently four times a
    // second — and a label that changes on its own is noise on every surface that draws one.
    expect("a half circle goes", TargetSession.withoutStatusGlyph("◐ IG 設定指引改進"), "IG 設定指引改進")
    expect("and so does the next frame", TargetSession.withoutStatusGlyph("◑ 評估動態島實現機制"), "評估動態島實現機制")
    expect("braille too", TargetSession.withoutStatusGlyph("⠐ 設計基本問題"), "設計基本問題")
    expect("and the ✳ it used to be", TargetSession.withoutStatusGlyph("✳ investigate the webhook"),
           "investigate the webhook")

    // What must survive. A title is allowed to start with punctuation, and a marker is only a
    // marker when it stands alone in front of one.
    expect("a title that starts with a quote is a title",
           TargetSession.withoutStatusGlyph("\"why\" is the question"), "\"why\" is the question")
    expect("a glyph with no space after it is part of the word",
           TargetSession.withoutStatusGlyph("◐IG"), "◐IG")
    expect("a letter is never a marker", TargetSession.withoutStatusGlyph("a b"), "a b")
    expect("a digit is never a marker", TargetSession.withoutStatusGlyph("1 b"), "1 b")
    expect("an ordinary title is untouched",
           TargetSession.withoutStatusGlyph("fix the retry backoff"), "fix the retry backoff")
    expect("and nothing is taken off something too short to have a marker",
           TargetSession.withoutStatusGlyph("◐ "), "◐ ")
}

group("the machinery Claude Code injects into your turns") {
    // These arrive tagged, attributed to the user, and never appear on screen — so a transcript
    // is the first place anybody sees them, sitting under the word "you" above the one sentence
    // they actually typed.
    let noisy = """
    <task-notification>
    <task-id>be95b5m2o</task-id>
    <status>completed</status>
    </task-notification>
    確認要部署上正式站嗎？
    """
    expect("the block goes and the sentence stays",
           Transcript.withoutMachineBlocks(noisy).trimmingCharacters(in: .whitespacesAndNewlines),
           "確認要部署上正式站嗎？")
    expect("a turn that was nothing but machinery comes back empty",
           Transcript.withoutMachineBlocks("<system-reminder>be careful</system-reminder>")
               .trimmingCharacters(in: .whitespacesAndNewlines), "")
    expect("several in one turn",
           Transcript.withoutMachineBlocks("a<system-reminder>x</system-reminder>b<command-name>y</command-name>c"),
           "abc")
    // Opened and never closed is a truncated record, and everything after it is the block.
    expect("an unclosed block takes the rest with it",
           Transcript.withoutMachineBlocks("keep me<task-notification>cut from here"), "keep me")
    // The rule is a list of observed tags, not a rule about angle brackets.
    expect("somebody quoting xml typed that themselves",
           Transcript.withoutMachineBlocks("why does <thing>this</thing> fail?"),
           "why does <thing>this</thing> fail?")
    expect("ordinary text is untouched",
           Transcript.withoutMachineBlocks("deploy it"), "deploy it")
}

group("the remote server refuses the right cross-origin requests") {
    // DNS rebinding: the page keeps calling itself evil.com while the address behind the name
    // becomes 127.0.0.1, so origin checks stop working — and Host is the one thing that does not
    // change through any of it.
    func ok(_ host: String) -> Bool {
        RemoteServer.isAllowedHost(host, port: 7717, hostname: "clawd.example.com")
    }
    check("loopback with a port", ok("127.0.0.1:7717"))
    check("localhost", ok("localhost:7717"))
    check("ipv6 loopback in brackets", ok("[::1]:7717"))
    check("the configured hostname", ok("clawd.example.com"))
    check("and it is not case sensitive", ok("Clawd.Example.COM"))
    check("a quick tunnel, whose name cannot be known in advance",
          ok("denied-franchise-william-jade.trycloudflare.com"))
    check("a rebound host is refused", !ok("evil.com"))
    check("and one that merely ends in something familiar", !ok("evil-trycloudflare.com"))
    check("and one pretending to be the configured one", !ok("clawd.example.com.evil.com"))

    // The distinction that cost a bug: typing the address into a bar that was on another page is
    // cross-site, and is not an attack.
    func sub(_ h: [String: String]) -> Bool { RemoteServer.isCrossSiteSubresource(h) }
    check("a script fetching from another site is refused",
          sub(["sec-fetch-site": "cross-site", "sec-fetch-mode": "cors"]))
    check("with no mode at all, still refused",
          sub(["sec-fetch-site": "cross-site"]))
    check("somebody typing the address is not",
          !sub(["sec-fetch-site": "cross-site", "sec-fetch-mode": "navigate",
                "sec-fetch-dest": "document"]))
    check("the page asking for its own things is not",
          !sub(["sec-fetch-site": "same-origin", "sec-fetch-mode": "cors"]))
    check("and a script that is not a browser at all is left to the token check",
          !sub([:]))
}

group("a tool result is what a command printed, not how it wanted to be coloured") {
    // Observed on a phone: four hundred characters of escape codes with no spaces in them, which
    // pushed the page sideways and left a screen of black with one line of noise in the middle.
    let noisy = "\u{1B}[38;2;47;107;94m\u{1B}[48;2;47;107;94m▪\u{1B}[0msample-app — ~/code/sample-app"
    expect("the colours go and the sentence stays",
           Ansi.plain(noisy), "▪sample-app — ~/code/sample-app")
    expect("text with no escapes in it is untouched",
           Ansi.plain("total 95904"), "total 95904")
}

// MARK: - Claude Code hooks

private func hookSession(_ id: String, tty: String) -> TargetSession {
    TargetSession(backend: .iterm, id: id, name: "x", tty: tty,
                  windowIndex: 0, tabIndex: 0, isClaude: true)
}

group("hooks: reading a note") {
    let good = Data(#"{"event":"Stop","tty":"ttys004","at":1787039500,"session":"3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34"}"#.utf8)
    let note = HookBridge.parse(good)
    check("a whole note reads", note != nil)
    expect("its event", note?.event, .stop)
    expect("its tty", note?.tty, "ttys004")
    expect("its session id", note?.session, "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    // The script leaves the session out when it could not find one, and that is a note worth
    // keeping: the tty is what a reading is keyed on, and the id is only ever a shortcut.
    let noSession = HookBridge.parse(Data(#"{"event":"Stop","tty":"ttys004","at":1}"#.utf8))
    check("a note with no session id is still a note", noSession != nil)
    expect("and says so", noSession?.session, nil)

    // An event this version does not know about is not an error to report; it is a note from a
    // newer script, and the right answer is to ignore it rather than to draw something wrong.
    check("an unknown event is dropped",
          HookBridge.parse(Data(#"{"event":"PreCompact","tty":"ttys004","at":1}"#.utf8)) == nil)
    check("half a file is dropped", HookBridge.parse(Data(#"{"event":"Stop","tt"#.utf8)) == nil)
    check("no tty is dropped",
          HookBridge.parse(Data(#"{"event":"Stop","tty":"","at":1}"#.utf8)) == nil)
}

group("hooks: what a note is allowed to change") {
    let now = Date()
    let one = hookSession("A", tty: "/dev/ttys004")
    let two = hookSession("B", tty: "/dev/ttys009")
    let sessions = [one, two]
    func notes(_ event: HookBridge.Event, ago: TimeInterval = 1) -> [String: HookBridge.Note] {
        ["ttys004": HookBridge.Note(event: event, tty: "ttys004",
                                    at: now.addingTimeInterval(-ago), session: nil)]
    }

    // Nothing installed is the state every reading has to be right in, so it is the first check.
    expect("with no notes, the screen is the whole answer",
           HookBridge.merge([:], into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .waiting)
    expect("a session nobody left a note for is untouched",
           HookBridge.merge(notes(.stop), into: ["B": .working("x")], sessions: sessions,
                            now: now)["B"],
           .working("x"))

    // The one state the list exists for. A menu is a shape on screen; a note is a moment, and
    // `Notification` fires for a permission request and for a minute of quiet alike.
    expect("a question on screen outranks a Stop",
           HookBridge.merge(notes(.stop), into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .waiting)
    expect("and a prompt going in leaves it alone",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .waiting],
                            sessions: sessions, now: now)["A"],
           .waiting)

    // **No note asserts that a session is working**, and this is the narrowing that measuring
    // produced: Claude Code draws its live line about two seconds after Return and draws nothing
    // at all while plain text comes back, so a claim short enough to be safe covers almost none
    // of a turn — and a long one cannot be retracted when somebody presses Esc, because
    // cancelling fires no hook. `SessionWatch.nudge` looks twice instead.
    expect("a submitted prompt claims nothing about an idle screen",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)
    expect("nor about one that could not be read at all",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .unknown],
                            sessions: sessions, now: now)["A"],
           .unknown)
    expect("and it never touches the live line",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .working("Generating… (4s)")],
                            sessions: sessions, now: now)["A"],
           .working("Generating… (4s)"))

    // The stale spinner: Claude Code does not always erase its live line when a fast turn ends.
    expect("a fresh Stop beats a spinner left on the screen",
           HookBridge.merge(notes(.stop), into: ["A": .working("Generating… (4s)")],
                            sessions: sessions, now: now)["A"],
           .idle)
    // …and only for as long as a leftover line could plausibly still be one. Past that, anything
    // running started with a prompt of its own, and calling it idle is the worst answer here.
    expect("an old one does not",
           HookBridge.merge(notes(.stop, ago: HookBridge.stopWindow + 5),
                            into: ["A": .working("Generating… (4s)")],
                            sessions: sessions, now: now)["A"],
           .working("Generating… (4s)"))

    // These two only ask us to look. The reading that followed is the answer.
    expect("a notification changes no state by itself",
           HookBridge.merge(notes(.notification), into: ["A": .idle], sessions: sessions,
                            now: now)["A"],
           .idle)
    expect("nor does a session starting",
           HookBridge.merge(notes(.sessionStart), into: ["A": .unknown], sessions: sessions,
                            now: now)["A"],
           .unknown)
}

group("hooks: editing somebody else's settings file") {
    // What has to survive: a hook that belongs to a plugin, and every key that is not `hooks`.
    let theirs: [String: Any] = [
        "model": "opus",
        "hooks": [
            "PreToolUse": [["matcher": "Write",
                            "hooks": [["type": "command", "command": "/opt/theirs/check.sh"]]]],
            "Stop": [["hooks": [["type": "command", "command": "/opt/theirs/done.sh"]]]],
        ],
    ]
    let after = HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: theirs)
    let hooks = after["hooks"] as? [String: Any] ?? [:]
    expect("everything outside hooks is left alone", after["model"] as? String, "opus")
    expect("an event we do not use keeps its entry",
           (hooks["PreToolUse"] as? [[String: Any]])?.count, 1)
    expect("an event we share keeps theirs and gains ours",
           (hooks["Stop"] as? [[String: Any]])?.count, 2)
    for event in HookBridge.Event.allCases {
        check("\(event.rawValue) is wired up",
              (hooks[event.rawValue] as? [[String: Any]] ?? []).contains { group in
                  (group["hooks"] as? [[String: Any]] ?? []).contains(where: HookBridge.isOurs)
              })
    }

    // Pressing Install twice is a thing people do.
    let twice = HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: after)
    expect("installing twice leaves one of ours",
           ((twice["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]])?.count, 2)

    let back = HookBridge.removing(from: twice)
    let left = back["hooks"] as? [String: Any] ?? [:]
    expect("removing puts the file back", NSDictionary(dictionary: left),
           NSDictionary(dictionary: theirs["hooks"] as? [String: Any] ?? [:]))
    expect("and leaves the rest of it alone", back["model"] as? String, "opus")

    // A file that had no hooks of its own should read afterwards as though nothing happened.
    // The real path, because that is what marks an entry as ours — see `isOurs`.
    let bare = HookBridge.removing(
        from: HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: ["model": "opus"]))
    check("an event left empty goes away rather than staying as []", bare["hooks"] == nil)

    // A path with a space in it is a home directory somebody actually has.
    let spaced = HookBridge.adding("/Users/a b/.config/clawdline/hook.sh", to: [:])
    let command = (((spaced["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]])?
        .first?["hooks"] as? [[String: Any]])?.first?["command"] as? String
    expect("the path is quoted", command, "'/Users/a b/.config/clawdline/hook.sh' Stop")
}

group("hooks: the script itself") {
    // The one piece of this that is not Swift, and the piece with the most ways to be quietly
    // wrong: it writes nothing on stdout, it always exits 0, and it names its note after the
    // terminal the session is on — which is the only string this app and Claude Code both know.
    let script = "Resources/clawdline-hook.sh"
    guard FileManager.default.fileExists(atPath: script) else {
        check("Resources/clawdline-hook.sh is there", false)
        return
    }
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-hook-test-\(getpid())")
    try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }

    func run(_ event: String, payload: String, into: URL? = dir) -> (out: String, code: Int32) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/bin/sh")
        p.arguments = [script, event]
        var env = ProcessInfo.processInfo.environment
        if let into { env["CLAWDLINE_HOOK_DIR"] = into.path } else { env["CLAWDLINE_HOOK_DIR"] = "/nowhere/at/all" }
        p.environment = env
        let stdin = Pipe(), stdout = Pipe()
        p.standardInput = stdin
        p.standardOutput = stdout
        p.standardError = Pipe()
        try? p.run()
        stdin.fileHandleForWriting.write(Data(payload.utf8))
        try? stdin.fileHandleForWriting.close()
        let out = String(data: stdout.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        p.waitUntilExit()
        return (out, p.terminationStatus)
    }

    // A prompt with a quote and a backslash in it, because that is what breaks a reader that
    // tries to parse JSON with string operations. The session id is a uuid and cannot.
    let payload = #"{"session_id":"3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34","cwd":"/tmp","hook_event_name":"Stop","prompt":"a \" quote and a \\ backslash"}"#
    let first = run("Stop", payload: payload)
    expect("it exits 0", first.code, 0)
    expect("and says nothing on stdout — that is read back as instructions", first.out, "")

    let written = (try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? []
    let notes = written.filter { $0.hasSuffix(".json") }
    // The test binary is a child of whatever ran it, so there may be no tty above it at all —
    // in CI there is not. No tty is a note that is deliberately not written, so both outcomes
    // are correct and only their contents can be wrong.
    if let name = notes.first {
        let note = HookBridge.parse(
            (try? Data(contentsOf: dir.appendingPathComponent(name))) ?? Data())
        check("what it wrote is a note", note != nil)
        expect("named after the tty it found", name, "\(note?.tty ?? "?").json")
        expect("carrying the event it was told", note?.event, .stop)
        expect("and the session id, cut out of a payload with quotes in it",
               note?.session, "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

        // Overwritten, never appended: a session has one note and it is the newest one.
        _ = run("Notification", payload: payload)
        let after = ((try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? [])
            .filter { $0.hasSuffix(".json") }
        expect("a second note replaces the first", after.count, 1)
        expect("with the newer event",
               HookBridge.parse((try? Data(contentsOf: dir.appendingPathComponent(name))) ?? Data())?.event,
               .notification)
    }

    // Every way this can be asked to do nothing, it has to do nothing quietly. A hook that
    // exits non-zero is making a decision about somebody's turn.
    expect("no event argument, no complaint", run("", payload: payload).code, 0)
    expect("empty stdin, no complaint", run("Stop", payload: "").code, 0)
    expect("no directory to write into, no complaint",
           run("Stop", payload: payload, into: nil).code, 0)
}

// ---------------------------------------------------------------- real captured fixtures
// Everything below was copied out of a live run: cloudflared 2026.6.1 on macOS 15,
// `cloudflared tunnel --url http://127.0.0.1:7717`, stderr.

let banner = "2026-08-18T09:31:17Z INF |  https://denied-franchise-william-jade.trycloudflare.com                                   |"
let bannerTitle = "2026-08-18T09:31:17Z INF |  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):  |"
let terms = "2026-08-18T09:31:14Z INF Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee, are subject to the Cloudflare Online Services Terms of Use (https://www.cloudflare.com/website-terms/), and Cloudflare reserves the right to investigate your use of Tunnels for violations of such terms. If you intend to use Tunnels in production you should use a pre-created named tunnel by following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"
let requesting = "2026-08-18T09:31:14Z INF Requesting new quick Tunnel on trycloudflare.com..."
let registered = "2026-08-18T09:31:18Z INF Registered tunnel connection connIndex=0 connection=da4d66ef-9c21-4b23-82c8-bc14bb60cc8e event=0 ip=2606:4700:a8::5 location=tpe01 protocol=quic"
let registered1 = "2026-08-18T09:32:00Z INF Registered tunnel connection connIndex=1 connection=7807387d-9b2f-4400-856b-48a74c4fd921 event=0 ip=2606:4700:a8::3 location=tpe01 protocol=quic"
let curve = "2026-08-18T09:31:17Z INF Tunnel connection curve preferences: [X25519MLKEM768 CurveID(65074) CurveP256] connIndex=0 event=0 ip=2606:4700:a8::5"
let metrics = "2026-08-18T09:31:17Z INF Starting metrics server on 127.0.0.1:20241/metrics"
let settings = "2026-08-18T09:31:17Z INF Settings: map[cred-file:/Users/x/.cloudflared/471a0799.json ha-connections:1 protocol:quic url:http://127.0.0.1:7717]"
let precheck = "2026-08-18T09:31:17Z INF precheck component=\"DNS Resolution\" details=\"DNS Resolved successfully\" run_id=f12384d3 status=pass target=region1.v2.argotunnel.com"
let badName = "error parsing tunnel ID: clawdline-no-such-tunnel is neither the ID nor the name of any of your tunnels"
let startingNamed = "2026-08-18T09:31:59Z INF Starting tunnel tunnelID=471a0799-077f-42b2-9d7e-21da0f069d07"

// Not captured — cloudflared's own shutdown wording, which is the trap this guards against.
let unregistered = "2026-08-18T09:35:02Z INF Unregistered tunnel connection connIndex=0 event=0"

group("quickURL") {
    expect("the banner line", RemoteTunnel.quickURL(in: banner),
           "https://denied-franchise-william-jade.trycloudflare.com")
    check("the terms-of-use line has URLs but not ours", RemoteTunnel.quickURL(in: terms) == nil,
          "got \(String(describing: RemoteTunnel.quickURL(in: terms)))")
    check("the banner title", RemoteTunnel.quickURL(in: bannerTitle) == nil)
    check("the requesting line", RemoteTunnel.quickURL(in: requesting) == nil)
    check("a registration line", RemoteTunnel.quickURL(in: registered) == nil)
    check("empty", RemoteTunnel.quickURL(in: "") == nil)
    expect("a trailing slash is dropped",
           RemoteTunnel.quickURL(in: "INF |  https://a-b-c.trycloudflare.com/  |"),
           "https://a-b-c.trycloudflare.com")
    check("a bare apex is not an address",
          RemoteTunnel.quickURL(in: "https://.trycloudflare.com") == nil)
    expect("ours wins even when another URL comes first",
           RemoteTunnel.quickURL(in: "see https://www.cloudflare.com/x/ then https://q-w-e.trycloudflare.com |"),
           "https://q-w-e.trycloudflare.com")
}

group("registeredConnection") {
    expect("the registration line", RemoteTunnel.registeredConnection(in: registered), "tpe01")
    expect("the second connection", RemoteTunnel.registeredConnection(in: registered1), "tpe01")
    check("unregistered is not registered",
          RemoteTunnel.registeredConnection(in: unregistered) == nil,
          "got \(String(describing: RemoteTunnel.registeredConnection(in: unregistered)))")
    check("the curve line mentions a connection and is not one",
          RemoteTunnel.registeredConnection(in: curve) == nil)
    check("the banner", RemoteTunnel.registeredConnection(in: banner) == nil)
    check("the metrics line", RemoteTunnel.registeredConnection(in: metrics) == nil)
    check("the settings line", RemoteTunnel.registeredConnection(in: settings) == nil)
    check("a precheck row", RemoteTunnel.registeredConnection(in: precheck) == nil)
    expect("no location field still counts as up",
           RemoteTunnel.registeredConnection(in: "2026-01-01T00:00:00Z INF Registered tunnel connection connIndex=0"),
           "?")
    expect("the older wording",
           RemoteTunnel.registeredConnection(in: "2021-01-01T00:00:00Z INF Connection 9c1 registered connIndex=1 location=SIN"),
           "SIN")
}

group("complaint") {
    expect("a bare stderr line is the whole message", RemoteTunnel.complaint(in: badName), badName)
    check("an INF line is not a complaint", RemoteTunnel.complaint(in: registered) == nil)
    check("the banner is not a complaint", RemoteTunnel.complaint(in: banner) == nil)
    check("Starting tunnel is not a complaint", RemoteTunnel.complaint(in: startingNamed) == nil)
    expect("an ERR line loses its timestamp",
           RemoteTunnel.complaint(in: "2026-08-18T09:31:14Z ERR Couldn't start tunnel error=\"Unauthorized\""),
           "Couldn't start tunnel error=\"Unauthorized\"")
    expect("an FTL line too",
           RemoteTunnel.complaint(in: "2026-08-18T09:31:14Z FTL no credentials file"),
           "no credentials file")
    check("blank", RemoteTunnel.complaint(in: "   ") == nil)
    check("a WRN line is not worth reporting", RemoteTunnel.complaint(in: "2026-08-18T09:31:17Z WRN slow") == nil)
    check("long lines are cut", (RemoteTunnel.complaint(in: String(repeating: "x", count: 500)) ?? "").count == 200)
}

group("redacted") {
    expect("the random half goes",
           RemoteTunnel.redacted("https://denied-franchise-william-jade.trycloudflare.com"),
           "https://….trycloudflare.com")
    expect("a named hostname keeps its zone",
           RemoteTunnel.redacted("https://clawd.example.com"), "https://….example.com")
    expect("nothing to redact", RemoteTunnel.redacted("nonsense"), "nonsense")
}

group("backoff") {
    expect("first try", RemoteTunnel.backoff(1), 1)
    expect("second", RemoteTunnel.backoff(2), 2)
    expect("third", RemoteTunnel.backoff(3), 4)
    expect("sixth", RemoteTunnel.backoff(6), 32)
    expect("capped", RemoteTunnel.backoff(20), 60)
    expect("nonsense is still a wait", RemoteTunnel.backoff(0), 1)
}

group("arguments") {
    // `--config` is the one that is not decoration: without it cloudflared reads
    // ~/.cloudflared/config.yml, whose `tunnel:` key overrides the name on the command line and
    // whose ingress list 404s any hostname it has never heard of. Both were observed live.
    let ours = RemoteTunnel.configURL.path
    expect("quick",
           RemoteTunnel.arguments(for: RemoteTunnel.Plan(mode: .quick, port: 7717)),
           ["tunnel", "--config", ours, "--no-autoupdate", "--metrics", "127.0.0.1:0",
            "--grace-period", "2s", "--url", "http://127.0.0.1:7717"])
    expect("named puts its flags before run",
           RemoteTunnel.arguments(for: RemoteTunnel.Plan(mode: .named, name: "clawd")),
           ["tunnel", "--config", ours, "--no-autoupdate", "--metrics", "127.0.0.1:0",
            "--grace-period", "2s", "run", "clawd"])
    expect("a non-default port rides along",
           RemoteTunnel.arguments(for: RemoteTunnel.Plan(mode: .quick, port: 9000)).last,
           "http://127.0.0.1:9000")
}

group("TunnelMode") {
    expect("off", TunnelMode(configured: "off"), .off)
    expect("quick", TunnelMode(configured: "quick"), .quick)
    expect("named", TunnelMode(configured: " Named "), .named)
    expect("a typo is off", TunnelMode(configured: "quik"), .off)
    expect("empty is off", TunnelMode(configured: ""), .off)
}

group("the tunnel refuses, and every reason is a pure function of its inputs") {
    func why(_ mode: TunnelMode, auth: Bool = true, server: Bool = true,
             name: String = "clawd", host: String = "clawd.example.com") -> String? {
        RemoteTunnel.refusal(mode: mode, authConfigured: auth, serverOn: server,
                             name: name, hostname: host)
    }
    // The interlock this file exists for: reachable from the internet must be a decision somebody
    // made, not something one config key did.
    check("no paired device refuses", why(.quick, auth: false)?.contains("paired device") == true)
    check("and says so for a named tunnel too", why(.named, auth: false) != nil)
    check("no local server refuses", why(.quick, server: false)?.contains("local server") == true)
    check("a named tunnel with no name refuses",
          why(.named, name: "")?.contains("remote_tunnel_name") == true)
    check("a named tunnel with no hostname refuses",
          why(.named, host: "")?.contains("remote_hostname") == true)
    check("a quick tunnel needs neither", why(.quick, name: "", host: "") == nil)
    check("off never refuses, it is simply off", why(.off, auth: false, server: false) == nil)
    check("everything in place is allowed", why(.quick) == nil && why(.named) == nil)
}

private func hookTarget(_ id: String, title: String = "fix the webhook",
                        tty: String = "/dev/ttys004", cwd: String? = nil) -> TargetSession {
    TargetSession(backend: .iterm, id: id, name: title, tty: tty,
                  windowIndex: 0, tabIndex: 0, isClaude: true, cwd: cwd)
}

group("state hook: a reading is not an event") {
    let one = hookTarget("A")
    let two = hookTarget("B", tty: "/dev/ttys009")
    let sessions = [one, two]
    func changes(_ old: [String: SessionState],
                 _ new: [String: SessionState]) -> [StateHook.Change] {
        StateHook.transitions(from: old, to: new, sessions: sessions)
    }

    // The moment the whole file exists for: something that was running has stopped to ask.
    let real = changes(["A": .working("Cogitating… (7s)")], ["A": .waiting])
    expect("working → waiting is one change", real.count, 1)
    expect("and it says exactly what happened", real.first,
           StateHook.Change(session: one, from: .working("Cogitating… (7s)"), to: .waiting))

    expect("the same state twice is nothing", changes(["A": .idle], ["A": .idle]).count, 0)

    // The live line carries its own clock, so two readings of a busy session are never equal and
    // are never a change either. Anything keyed on `==` here would fire once a second, forever.
    expect("a ticking live line is not a change",
           changes(["A": .working("Cogitating… (7s)")],
                   ["A": .working("Cogitating… (8s)")]).count, 0)
    expect("nor is losing the line while staying busy",
           changes(["A": .working("Cogitating… (7s)")], ["A": .working("")]).count, 0)

    // `unknown` is the absence of an answer, not a state — iTerm2 being slow to reply must not
    // arrive as a session stopping and starting again.
    expect("going unknown is not a change",
           changes(["A": .working("x")], ["A": .unknown]).count, 0)
    expect("coming back from unknown is not one either",
           changes(["A": .unknown], ["A": .idle]).count, 0)
    expect("and unknown to unknown is certainly not",
           changes(["A": .unknown], ["A": .unknown]).count, 0)

    // Ten sessions on the first reading after launch are not ten state changes.
    expect("a session seen for the first time is not a change",
           changes([:], ["A": .waiting, "B": .idle]).count, 0)
    expect("a session that has gone away is not one",
           changes(["A": .working("x")], [:]).count, 0)

    let both = changes(["A": .working("x"), "B": .idle], ["A": .idle, "B": .waiting])
    expect("two sessions changing is two changes", both.count, 2)
    expect("in the order the sessions are listed in", both.map { $0.session.id }, ["A", "B"])

    // The session list is the authority on what exists; a reading keyed to something not in it
    // belongs to a tab that closed between the two.
    expect("a reading for a session nobody is watching is ignored",
           StateHook.transitions(from: ["Z": .idle], to: ["Z": .waiting],
                                 sessions: sessions).count, 0)
}

group("state hook: the words a hook reads") {
    // Spelled out rather than derived, because these are somebody else's API: renaming a case in
    // Swift must not quietly rename a string a shell script compares against.
    expect("working", StateHook.name(.working("x")), "working")
    expect("waiting", StateHook.name(.waiting), "waiting")
    expect("idle", StateHook.name(.idle), "idle")
    expect("unknown", StateHook.name(.unknown), "unknown")
}

group("state hook: what a hook is told") {
    let session = hookTarget("A9F3", title: "✳ fix the webhook (claude)",
                             cwd: "/Users/x/code/clawdline")
    let env = StateHook.environment(
        for: StateHook.Change(session: session, from: .working("Cogitating… (7s)"), to: .waiting),
        claudeSession: "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    expect("the event has a name of its own", env["CLAWDLINE_EVENT"], "state_changed")
    expect("the state", env["CLAWDLINE_STATE"], "waiting")
    expect("the one it came from", env["CLAWDLINE_PREV_STATE"], "working")
    expect("the session id", env["CLAWDLINE_SESSION_ID"], "A9F3")
    expect("the tty", env["CLAWDLINE_TTY"], "/dev/ttys004")
    // The label, not the raw title: the glyph on the front is a frame of an animation and the
    // job name in brackets is iTerm2's, and neither is worth putting in a notification.
    expect("the label as a person reads it", env["CLAWDLINE_LABEL"], "fix the webhook")
    expect("where it is working", env["CLAWDLINE_CWD"], "/Users/x/code/clawdline")
    expect("and Claude Code's own id, which names the transcript",
           env["CLAWDLINE_CLAUDE_SESSION"], "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")
    check("a session that is not working carries no live line", env["CLAWDLINE_LINE"] == nil)

    let obj = (try? JSONSerialization.jsonObject(
        with: Data((env["CLAWDLINE_EVENT_JSON"] ?? "").utf8))) as? [String: Any]
    check("the whole thing arrives as one object too", obj != nil)
    expect("with the prefix off the keys", obj?["state"] as? String, "waiting")
    expect("and the underscores kept", obj?["session_id"] as? String, "A9F3")
    expect("the same fields as the variables, no more", obj?.count, env.count - 1)
    check("and it does not contain itself", obj?["event_json"] == nil)

    let busy = StateHook.environment(
        for: StateHook.Change(session: session, from: .idle, to: .working("Cogitating… (7s)")))
    expect("a working session carries the live line", busy["CLAWDLINE_LINE"], "Cogitating… (7s)")
    expect("and where it came from", busy["CLAWDLINE_PREV_STATE"], "idle")
    check("with nothing invented for a session no hook told us about",
          busy["CLAWDLINE_CLAUDE_SESSION"] == nil)

    // Claude Code draws no live line at all while plain text is coming back, and an empty string
    // would read as one that had been erased rather than one that was never there.
    let quiet = StateHook.environment(
        for: StateHook.Change(session: session, from: .idle, to: .working("   ")))
    check("a blank line is omitted rather than sent empty", quiet["CLAWDLINE_LINE"] == nil)

    let bare = StateHook.environment(
        for: StateHook.Change(session: hookTarget("B"), from: .waiting, to: .idle))
    check("a session whose directory is not known omits it", bare["CLAWDLINE_CWD"] == nil)
    expect("and everything else is still there", bare["CLAWDLINE_STATE"], "idle")
    expect("including the one it left", bare["CLAWDLINE_PREV_STATE"], "waiting")
}

group("state hook: finding the program") {
    // A GUI app has launchd's PATH, not a login shell's, so a bare name has to be looked for in
    // more places than PATH names — otherwise everything installed by Homebrew is unreachable.
    expect("an absolute path is taken as one", StateHook.resolve("/bin/sh"), "/bin/sh")
    expect("one that is not there resolves to nothing", StateHook.resolve("/nope/nope"), nil)
    // A slash means a path, so this is not hunted for in /usr/bin under the same name.
    expect("a relative path is not searched for", StateHook.resolve("./nope"), nil)
    check("a bare name is found", StateHook.resolve("sh")?.hasSuffix("/sh") == true)
    expect("a bare name that exists nowhere is nothing",
           StateHook.resolve("clawdline-no-such-program"), nil)
}

group("a hook written as #!/usr/bin/env node has to be able to find node") {
    // An app launched from Finder gets the launchd PATH, which has no Homebrew in it — so the
    // shebang fails for a reason that has nothing to do with the script.
    let launchd = "/usr/bin:/bin:/usr/sbin:/sbin"
    let fixed = StateHook.usefulPath(launchd)
    check("homebrew goes in front", fixed.hasPrefix("/opt/homebrew/bin:"))
    check("and /usr/local with it", fixed.contains("/usr/local/bin"))
    check("what was there is still there", fixed.hasSuffix(launchd))
    // Somebody's ordering is somebody's ordering: only what is genuinely absent goes in front,
    // and what was already there keeps its place rather than being hoisted.
    let mine = "/opt/homebrew/bin:/usr/bin:/bin"
    check("a directory already listed is not listed twice",
          StateHook.usefulPath(mine).split(separator: ":").filter { $0 == "/opt/homebrew/bin" }.count == 1)
    check("and the original ordering survives at the end",
          StateHook.usefulPath(mine).hasSuffix(mine))
    let complete = "/opt/homebrew/bin:/usr/local/bin:" + NSHomeDirectory() + "/.local/bin:"
        + NSHomeDirectory() + "/.bun/bin:/usr/bin"
    expect("nothing missing means nothing is touched at all",
           StateHook.usefulPath(complete), complete)
    check("an empty PATH still gets somewhere to look",
          StateHook.usefulPath("").contains("/usr/bin"))
    check("and so does a missing one", StateHook.usefulPath(nil).contains("/usr/bin"))
}

group("pairing tells a client apart from a sentence") {
    // Wrong and lapsed used to be the same code with different English in them, so a page could
    // only tell them apart by reading the message — the one part of an error nobody should ever
    // branch on. They are different things to do about: try again, versus start again.
    let entry = RemoteAuth.beginPairing(name: "a test")
    if case .wrongCode(let left) = RemoteAuth.confirmPairing(id: entry.id, code: "000000") {
        expect("a wrong code says how many are left", left, 4)
    } else {
        check("a wrong code is a wrong code", false)
    }
    // Five and it is gone, and gone reads as expired rather than as another wrong guess.
    for _ in 0..<3 { _ = RemoteAuth.confirmPairing(id: entry.id, code: "000000") }
    expect("the fifth wrong code ends it",
           RemoteAuth.confirmPairing(id: entry.id, code: "000000"), .expired)
    expect("and it stays ended",
           RemoteAuth.confirmPairing(id: entry.id, code: entry.code), .expired)

    // Only one pairing is ever live: two codes would be two chances to guess, and the person at
    // the Mac is looking at one window.
    let first = RemoteAuth.beginPairing(name: "first")
    let second = RemoteAuth.beginPairing(name: "second")
    expect("a new request replaces the old one",
           RemoteAuth.confirmPairing(id: first.id, code: first.code), .expired)
    if case .paired(let token) = RemoteAuth.confirmPairing(id: second.id, code: second.code) {
        check("and the newest one is the one that works", token.count >= 40)
        RemoteAuth.revoke(id: second.id)
    } else {
        check("and the newest one is the one that works", false)
    }
}

group("the cloudflared config we write, rather than the one that was already there") {
    // Only what cloudflared will act on: the file leads with a comment explaining why it exists,
    // and that comment says the words "ingress" and "tunnel:" out loud.
    func settings(_ yaml: String) -> String {
        yaml.split(separator: "\n", omittingEmptySubsequences: false)
            .filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("#") }
            .joined(separator: "\n")
    }
    let quick = settings(RemoteTunnel.configFile(for: RemoteTunnel.Plan(mode: .quick, port: 7717)))
    check("a quick tunnel gets no ingress at all — its address cannot be known in advance",
          !quick.contains("ingress"))
    check("and no tunnel name to be overridden by", !quick.contains("tunnel:"))
    // Not literally empty: cloudflared logs an error about an empty configuration file, in the
    // one place somebody looks when a tunnel will not come up.
    check("but it is not empty either", quick.contains("no-autoupdate: true"))

    let named = settings(RemoteTunnel.configFile(
        for: RemoteTunnel.Plan(mode: .named, name: "clawd", hostname: "clawd.example.com",
                               port: 7717),
        credentials: "/Users/x/.cloudflared/abc.json"))
    check("a named tunnel names itself", named.contains("tunnel: clawd"))
    check("and says where its credentials are",
          named.contains("credentials-file: /Users/x/.cloudflared/abc.json"))
    check("and maps the hostname at the port", named.contains("hostname: clawd.example.com"))
    check("to the loopback address and nothing else",
          named.contains("service: http://127.0.0.1:7717"))
    // A stream that is meant never to end, versus a proxy that answers for anybody.
    check("the event stream is given room", named.contains("connectTimeout: 30s"))
    check("and everything else is refused", named.contains("service: http_status:404"))
    check("credentials are left out when they could not be found",
          !settings(RemoteTunnel.configFile(for: RemoteTunnel.Plan(mode: .named, name: "c",
                                                                    hostname: "h", port: 1),
                                            credentials: nil)).contains("credentials-file"))
}

group("which language to answer a browser in") {
    // A browser may write its preferences in any order and let `q` say what it means, and several
    // do — so the order on the wire is not the order of preference.
    expect("plain order", L.preferences(in: "zh-TW,zh,en"), ["zh-TW", "zh", "en"])
    expect("quality wins over position",
           L.preferences(in: "en;q=0.5,zh-TW;q=0.9,ja"), ["ja", "zh-TW", "en"])
    expect("a missing q is 1.0, the specification's default",
           L.preferences(in: "de,en;q=0.9"), ["de", "en"])
    expect("ties keep the order they were written in",
           L.preferences(in: "fr;q=0.8,it;q=0.8"), ["fr", "it"])
    expect("the wildcard is not a language", L.preferences(in: "*"), [])
    expect("and neither is nothing at all", L.preferences(in: ""), [])
    expect("whitespace is not part of a tag",
           L.preferences(in: "zh-TW, en;q=0.7"), ["zh-TW", "en"])

    // The catalog's ordering is what stops zh-Hans falling into the Traditional bucket.
    check("a Taiwanese browser gets Traditional",
          L.copy(preferring: ["zh-TW"]).settingsRemote == TraditionalChinese().settingsRemote)
    check("and a mainland one does not",
          L.copy(preferring: ["zh-CN"]).settingsRemote == SimplifiedChinese().settingsRemote)
    check("a language nobody here speaks falls back to English",
          L.copy(preferring: ["is-IS"]).settingsRemote == English().settingsRemote)
    check("and so does an empty list", L.copy(preferring: []).settingsRemote == English().settingsRemote)
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
