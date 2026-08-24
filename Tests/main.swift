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
        // The routines the app actually triggers. Missing ones fall back to idle, so
        // this is a warning in spirit — but a shipped pack should carry all of them.
        for routine in ["pop", "idle", "typing", "dance", "cheer", "sleep"] {
            check("\(name) has routine \(routine)", pack.routines[routine] != nil)
        }
        // `sleep` is held to more than existing, because it is the one routine that is on
        // screen all day rather than for the second and a half something takes.
        if let sleep = pack.routines["sleep"] {
            check("\(name) sleep loops", sleep.loop == true)
            // A sleeping character does not blink, and a blink block here would make it.
            check("\(name) sleep has no blink block", sleep.blink == nil)
            let shut = stride(from: 0.0, through: 1.0, by: 0.05).allSatisfy {
                pack.frame(routine: "sleep", at: $0 * sleep.duration).eyes == "blink"
            }
            check("\(name) sleep keeps its eyes shut throughout", shut)
            // A loop whose last key disagrees with its first jumps once every cycle. On a
            // two-second dance nobody would catch it; on a five-second breath in the menu bar
            // it is a twitch, which is the exact thing this state may not do.
            let first = pack.frame(routine: "sleep", at: 0)
            let last = pack.frame(routine: "sleep", at: sleep.duration * 0.9999)
            check("\(name) sleep ends where it starts",
                  abs(first.sy - last.sy) < 0.002 && abs(first.dy - last.dy) < 0.01,
                  "sy \(first.sy)→\(last.sy), dy \(first.dy)→\(last.dy)")
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

// MARK: - Claude Code skills

group("the slash menu discovers the skills Claude Code keeps on disk") {
    let fm = FileManager.default
    let base = fm.temporaryDirectory.appendingPathComponent("clawdline-skills-\(UUID().uuidString)")
    let home = base.appendingPathComponent("home")
    let repo = base.appendingPathComponent("repo")
    let cwd = repo.appendingPathComponent("Sources/Feature")
    let plugin = base.appendingPathComponent("plugin")
    defer { try? fm.removeItem(at: base) }

    func write(_ text: String, _ path: URL) {
        try! fm.createDirectory(at: path.deletingLastPathComponent(),
                                withIntermediateDirectories: true)
        try! Data(text.utf8).write(to: path)
    }
    func skill(_ text: String, under root: URL, named name: String) {
        write(text, root.appendingPathComponent(".claude/skills/\(name)/SKILL.md"))
    }

    try! fm.createDirectory(at: cwd, withIntermediateDirectories: true)
    try! fm.createDirectory(at: repo.appendingPathComponent(".git"), withIntermediateDirectories: true)

    skill("""
    ---
    description: Deploy this project
    ---
    Do it.
    """, under: repo, named: "deploy")
    skill("""
    ---
    description: Hidden by this project's settings
    ---
    Do not list me.
    """, under: repo, named: "hidden")
    skill("""
    ---
    description: Project recap
    ---
    Project body.
    """, under: repo, named: "recap")
    skill("""
    ---
    description: Summarizes changes for a person
    ---
    Personal body.
    """, under: home, named: "recap")
    skill("""
    ---
    user-invocable: false
    description: Background knowledge
    ---
    Private body.
    """, under: home, named: "private-context")
    skill("Instructions that must never become remote menu metadata.",
          under: home, named: "body-only")
    write("""
    {"skillOverrides":{"hidden":"off"},
     "enabledPlugins":{"design@market":true}}
    """, repo.appendingPathComponent(".claude/settings.local.json"))

    write("""
    ---
    name: visual
    description: >
      Designs a clear
      interface
    ---
    Plugin body.
    """, plugin.appendingPathComponent("skills/frontend/SKILL.md"))
    let registry: [String: Any] = [
        "version": 2,
        "plugins": ["design@market": [["installPath": plugin.path, "scope": "user"]]],
    ]
    let registryData = try! JSONSerialization.data(withJSONObject: registry)
    let registryPath = home.appendingPathComponent(".claude/plugins/installed_plugins.json")
    try! fm.createDirectory(at: registryPath.deletingLastPathComponent(),
                            withIntermediateDirectories: true)
    try! registryData.write(to: registryPath)

    let found = ClaudeSkills.available(cwd: cwd.path, home: home.path)
    expect("project, personal and plugin skills are the effective list",
           found.map(\.command), ["body-only", "deploy", "design:visual", "recap"])
    expect("a skill body is never exposed as its menu description",
           found.first(where: { $0.command == "body-only" })?.description, "")
    expect("personal replaces a project skill with the same command",
           found.first(where: { $0.command == "recap" })?.source, .personal)
    expect("folded YAML descriptions become one line",
           found.first(where: { $0.command == "design:visual" })?.description,
           "Designs a clear interface")
    expect("name matching beats description matching",
           ClaudeSkills.matching(found, query: "vis").map(\.command), ["design:visual"])
    expect("descriptions are searchable too",
           ClaudeSkills.matching(found, query: "summarizes").map(\.command), ["recap"])
}

group("the slash menu reads the exact skills a Codex session started with") {
    let instructions = """
    <skills_instructions>
    ### Available skills
    - openai-docs: Read official OpenAI documentation. (file: /Users/me/.codex/skills/.system/openai-docs/SKILL.md)
    - chrome:control-chrome: Control Chrome for local testing. (file: /Users/me/.codex/plugins/cache/chrome/skills/control-chrome/SKILL.md)
    - deploy: Deploy this repository safely. (file: /work/repo/.agents/skills/deploy/SKILL.md)
    </skills_instructions>
    """
    let parsed = CodexSkills.parse(instructions)
    expect("Codex names, including plugin namespaces, are preserved",
           parsed.map(\.command), ["openai-docs", "chrome:control-chrome", "deploy"])
    expect("the rollout's locator never becomes part of the visible description",
           parsed.map(\.description), ["Read official OpenAI documentation.",
                                       "Control Chrome for local testing.",
                                       "Deploy this repository safely."])
    expect("Codex skill scopes come from the catalog it was given",
           parsed.map(\.source), [.system, .plugin, .project])

    let base = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-codex-skills-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: base) }
    try! FileManager.default.createDirectory(at: base.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    let payload: [String: Any] = ["payload": ["role": "developer", "content": instructions]]
    try! JSONSerialization.data(withJSONObject: payload).write(to: base)
    expect("the rollout reader finds that catalog without reading the conversation body",
           CodexSkills.available(in: base), parsed)
}

group("the prompt names the assistant on the other end") {
    expect("a Codex target changes the Traditional Chinese placeholder",
           Assistant.codex.promptPlaceholder(from: "跟 Claude 說⋯⋯"), "跟 Codex 說⋯⋯")
    expect("Claude keeps its translated placeholder",
           Assistant.claude.promptPlaceholder(from: "Message Claude Code…"), "Message Claude Code…")
    expect("skills are completed with each assistant's real invocation",
           [Assistant.claude.skillInvocationPrefix, Assistant.codex.skillInvocationPrefix], ["/", "$"])
}

// MARK: - Keyframe sampling

group("routine sampling interpolates and steps") {
    guard let pack = decodePack(minimalPack) else { return check("pack decodes", false) }

    let start = pack.frame(routine: "idle", at: 0)
    expectClose("t=0 takes the first key", start.dy, 0)
    expect("pose comes from the first key", start.pose, "stand")

    // A pack written before the island had a resting state has no `sleep`, and asking for one
    // must not come back empty — an unknown routine samples as `idle`, which is what lets the
    // island slow that down and shut its eyes instead of drawing nothing.
    expectClose("a routine the pack does not have samples as idle",
                pack.frame(routine: "sleep", at: 1.0).dy,
                pack.frame(routine: "idle", at: 1.0).dy)

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
                      windowIndex: 0, tabIndex: 0, assistant: .claude).label
    }
    // Both ends come off: iTerm appends the job name, Claude Code prefixes a status glyph that
    // is now a frame of an animation rather than a fixed mark.
    expect("trailing job name is removed", label("✳ fix the thing (python)"), "fix the thing")
    expect("and the glyph on the front with it", label("✳ fix the thing"), "fix the thing")
    expect("parentheses mid-name survive", label("build (debug) now"), "build (debug) now")
    expect("an empty name falls back to coordinates", label("   "), "⌘1-1")
}

group("ps output picks out real assistant processes") {
    let ps = """
    ttys006  101 claude
    ttys013  102 /opt/homebrew/bin/claude --resume
    ttys023  103 bash /Users/me/.claude/statusline-command.sh
    ttys031  104 node /Users/me/project/claude-helper.js
    ttys044  105 -zsh
    ??       106 /Applications/Claude.app/Contents/MacOS/Claude
    ttys055  107 vim claude.md
    """
    let found = Assistant.reading(ofPS: ps)
    expect("a bare claude counts", found["ttys006"]?.assistant, .claude)
    expect("an absolute path to claude counts", found["ttys013"]?.assistant, .claude)
    check("a script living under .claude does not", found["ttys023"] == nil)
    check("a program merely named claude-something does not", found["ttys031"] == nil)
    check("a shell does not", found["ttys044"] == nil)
    check("a process with no tty is skipped", found["??"] == nil)
    check("an argument that mentions claude does not count", found["ttys055"] == nil)
    expect("exactly two matches", found.count, 2)
    expect("the pid comes back with it", found["ttys013"]?.pid, 102)
}

group("ps output picks out Codex, shim and all") {
    // What the published CLI actually looks like: a Node shim and the native binary it spawns,
    // both on one tty. Either proves the session is there; the native one holds the working
    // directory, so it is the pid worth having.
    let ps = """
    ttys006  201 node /Users/me/.nvm/versions/node/v24.1.0/bin/codex
    ttys006  202 /Users/me/.nvm/.../@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex
    ttys009  203 codex
    ttys011  204 codexctl watch
    ttys012  205 /opt/homebrew/bin/codex resume --last
    ttys013  206 vim ~/.codex/config.toml
    """
    let found = Assistant.reading(ofPS: ps)
    expect("the shim's tty is a Codex session", found["ttys006"]?.assistant, .codex)
    expect("and the native process is the one named", found["ttys006"]?.pid, 202)
    expect("a native install counts on its own", found["ttys009"]?.assistant, .codex)
    check("a program merely named codex-something does not", found["ttys011"] == nil)
    expect("resume is still a session", found["ttys012"]?.assistant, .codex)
    check("an argument that mentions codex does not count", found["ttys013"] == nil)
    expect("exactly three ttys", found.count, 3)
}

group("the Codex subcommands that are not sessions are refused") {
    func read(_ line: String) -> Assistant? {
        Assistant.reading(ofPS: line)["ttys006"]?.assistant
    }
    check("codex exec is not somewhere to send work", read("ttys006 1 codex exec \"do a thing\"") == nil)
    check("nor is the MCP server", read("ttys006 1 codex mcp-server") == nil)
    check("nor is the app server", read("ttys006 1 codex app-server") == nil)
    expect("a flag before the prompt is not a subcommand",
           read("ttys006 1 codex -s read-only -a on-request"), .codex)
    expect("and a bare prompt is still a session", read("ttys006 1 codex fix the tests"), .codex)
    // The refusal is per tty, and it has to survive the shim's own argless child appearing on
    // the same one — which `ps` may print either side of it.
    let both = """
    ttys006  301 /Users/me/.../vendor/aarch64-apple-darwin/bin/codex
    ttys006  300 node /Users/me/bin/codex exec "do a thing"
    """
    check("a refused tty stays refused whichever row came first",
          Assistant.reading(ofPS: both)["ttys006"] == nil)
}

group("a Codex session may run its own app server") {
    // Current Codex starts this server below the interactive native process. It is not itself a
    // session, but refusing the whole tty hid the real session above it — exactly what happened
    // to the sugar-elite tab.
    let interactive = """
    ttys001  34667 34134 node /Users/me/bin/codex
    ttys001  34668 34667 /Users/me/vendor/bin/codex
    ttys001  48634 34668 /Applications/Codex.app/Resources/codex sandbox -- /bin/node worker.js
    ttys001  48637 34668 /Applications/Codex.app/Resources/codex app-server --listen stdio://
    """
    let found = Assistant.reading(ofPS: interactive)
    expect("the interactive parent remains a session", found["ttys001"]?.assistant, .codex)
    expect("the native interactive process remains the useful pid", found["ttys001"]?.pid, 34668)

    // The opposite tree: `exec` is the parent, so an argless native child does not turn the
    // non-interactive command back into somewhere Clawdline offers to type.
    let exec = """
    ttys002  500 400 node /Users/me/bin/codex exec "do a thing"
    ttys002  501 500 /Users/me/vendor/bin/codex
    """
    check("a child of codex exec is still not a session",
          Assistant.reading(ofPS: exec)["ttys002"] == nil)
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
    check("a shell pane is not", !panes[1].isAssistant)
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
    let onTTY = ["ttys061": Assistant.Running(assistant: .claude, pid: 9)]
    check("but the tty says what the name does not",
          Tmux.parsePanes(versioned, running: onTTY)[0].isClaude)
    check("and a shell on a tty nobody claimed is still a shell",
          !Tmux.parsePanes(rows, running: ["ttys080": Assistant.Running(assistant: .claude,
                                                                       pid: 9)])[1].isAssistant)

    // Codex ships as a Node shim, so the pane's process name is `node` and the tty is the only
    // thing that knows better — the same problem as the versioned Claude Code binary, arriving
    // from the other end.
    let shim = ["%11", "/dev/ttys070", "node", "work", "1", "0", "node"]
        .joined(separator: sep)
    check("a node pane is not an assistant by name", !Tmux.parsePanes(shim)[0].isAssistant)
    expect("but the tty names it",
           Tmux.parsePanes(shim, running: ["ttys070": Assistant.Running(assistant: .codex,
                                                                        pid: 9)])[0].assistant,
           .codex)
    expect("a native codex pane is recognised by name alone",
           Tmux.parsePanes(["%12", "/dev/ttys071", "codex", "w", "1", "1", ""]
                            .joined(separator: sep))[0].assistant, .codex)
}

group("ending a session waits for the process before it takes the tab") {
    typealias Step = Targets.Farewell.Step
    func step(_ elapsed: TimeInterval, pid: pid_t? = 42,
              termed: Bool = false, killed: Bool = false) -> Step {
        Targets.Farewell.step(elapsed: elapsed, pid: pid, termed: termed, killed: killed)
    }
    let polite = Targets.Farewell.polite

    // The whole bug in one line: a tab whose job is still running must not be closed, because
    // iTerm2 answers that with a modal sheet and nothing on the serial queue moves again.
    expect("a session still on its tty is waited for", step(0.2), .wait)
    expect("gone means gone, immediately", step(0.2, pid: nil), .close)
    expect("and gone late is still just gone", step(polite + 5, pid: nil), .close)

    // Politeness has an end. `/exit` typed during a tool call is a queued message, so a session
    // can honestly be mid-answer for longer than anyone will hold a phone.
    expect("the word gets the whole polite window", step(polite - 0.01), .wait)
    expect("then it is asked with a signal", step(polite), .term(42))
    expect("asked once, not once per poll", step(polite + 0.4, termed: true), .wait)
    expect("and then told", step(polite + Targets.Farewell.afterTerm, termed: true), .kill(42))
    expect("told once as well",
           step(polite + Targets.Farewell.afterTerm + 0.2, termed: true, killed: true), .wait)

    // Past a SIGKILL there is nothing left to try, and waiting forever would be the same freeze
    // by a slower road. The tab goes — which is what the old code did to every session.
    let exhausted = polite + Targets.Farewell.afterTerm + Targets.Farewell.afterKill
    expect("a process that survives SIGKILL stops the waiting",
           step(exhausted, termed: true, killed: true), .close)
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

    let queued = #"{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-24T10:28:54.573Z","content":"但我按下去瞬間，會出現等待畫面"}"#
    let queuedEntries = Transcript.parse(queued)
    expect("queued input becomes one transcript entry", queuedEntries.count, 1)
    expect("queued input belongs to the user", queuedEntries.first?.kind,
           Transcript.Entry.Kind.user)
    expect("queued input keeps its text", queuedEntries.first?.text,
           "但我按下去瞬間，會出現等待畫面")
    expect("queued input keeps its timestamp",
           queuedEntries.first?.time?.timeIntervalSince1970, 1787567334.573)

    let malformedQueueRows = [
        #"{"type":"queue-operation","operation":"enqueue"}"#,
        #"{"type":"queue-operation","operation":"enqueue","content":{"text":"not a string"}}"#,
        #"{"type":"queue-operation","operation":"dequeue","content":"not a message"}"#,
        #"{"type":"user","message":{"role":"user","content":"still parses"}}"#,
    ].joined(separator: "\n")
    let afterMalformedQueue = Transcript.parse(malformedQueueRows)
    expect("malformed queue rows do not break later parsing", afterMalformedQueue.count, 1)
    expect("the valid row after malformed queue rows survives", afterMalformedQueue.first?.text,
           "still parses")

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

    // **Every character that is not a letter or a digit**, not just the separators. Replacing `/`
    // alone is right until a path has a space, a dot or an underscore in it, and then the lookup
    // goes to a folder that does not exist — silently, because a missing transcript is a normal
    // state. These are the three that were wrong.
    expect("an underscore is a dash too",
           Transcript.projectDirectory(forCwd: "/a/two_words").lastPathComponent, "-a-two-words")
    expect("so is a dot",
           Transcript.projectDirectory(forCwd: "/a/v1.2").lastPathComponent, "-a-v1-2")
    expect("so is a space",
           Transcript.projectDirectory(forCwd: "/a/two words").lastPathComponent, "-a-two-words")

    // One rule, one implementation — the two used to be written out separately and would have
    // agreed only until somebody edited one of them.
    for path in ["/Users/me/code/a_b", "/x/y.z", "/p q/r", "/plain/path"] {
        expect("it is the same rule StartPoints uses, for \(path)",
               Transcript.projectDirectory(forCwd: path).lastPathComponent,
               StartPoints.slug(of: path))
    }
}

group("a hook session id is an identity boundary") {
    let fm = FileManager.default
    let dir = fm.temporaryDirectory
        .appendingPathComponent("clawdline-transcript-identity-\(UUID().uuidString)",
                                isDirectory: true)
    try! fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    let other = dir.appendingPathComponent("somebody-else.jsonl")
    try! Data("{}\n".utf8).write(to: other)

    // SessionStart fires before Claude creates its own jsonl. The old fallback chose `other`
    // because it was the only recent file, making a new browser-started tab show an existing
    // conversation until its own transcript appeared.
    check("a not-yet-created exact transcript does not fall back to another session",
          Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: Date(),
                            sessionID: "brand-new") == nil)

    let exact = dir.appendingPathComponent("brand-new.jsonl")
    try! Data("{}\n".utf8).write(to: exact)
    expect("the exact transcript appears as soon as Claude creates it",
           Transcript.locate(in: dir, tabTitle: "Claude Code", startedAt: Date(),
                             sessionID: "brand-new"), exact)
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
        "de:webInfoBranch", "it:webInfoBranch", "pt:webInfoBranch", "id:webInfoBranch",
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

group("devstack: the line worth showing out of a process's dying words") {
    // Every fixture here is real. These are the shapes three projects on this machine handed
    // back on the morning the row was found to be unreadable — one of them a JSON envelope, one
    // a success message under a red mark, one a tail that stopped in the middle of a brace.

    // Sixty characters of bookkeeping in front of the answer. Cut at seventy, as the row used
    // to, and it stopped two characters into the part that meant anything.
    let wrapped = DevStack.parseState(Data(#"""
    {"processes": [{"name": "build-web", "state": "exited", "exit_code": 127,
      "error": "{\"level\":\"error\",\"process\":\"build-web\",\"replica\":0,\"message\":\"bash: npm: command not found\"}"}]}
    """#.utf8))
    expect("the sentence, not the envelope around it",
           wrapped?.processes.first?.reason, "bash: npm: command not found")

    // A web server prints request logs until the moment it dies, so the last line of the tail is
    // routinely a success. Offering that as the explanation for a crash is worse than silence.
    let noisy = DevStack.parseState(Data(#"""
    {"processes": [{"name": "api", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"error\",\"message\":\"ERROR: address already in use\"}\n{\"level\":\"info\",\"message\":\"INFO: GET /health 200 OK\"}"}]}
    """#.utf8))
    expect("the failure, not the last thing it happened to print",
           noisy?.processes.first?.reason, "ERROR: address already in use")

    // And when nothing in the tail announces itself, say nothing. An exit code with no
    // explanation is a smaller claim than a wrong explanation.
    let quiet = DevStack.parseState(Data(#"""
    {"processes": [{"name": "api", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"info\",\"message\":\"INFO: GET /health 200 OK\"}"}]}
    """#.utf8))
    check("a tail with nothing loud in it explains nothing rather than guessing",
          quiet?.processes.first?.reason == nil)

    // The tail is cut to a byte budget by whoever wrote it, so it often ends mid-envelope.
    let cut = DevStack.parseState(Data(#"""
    {"processes": [{"name": "tunnel", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"error\",\"message\":\"ERR failed to accept incoming stream\"}\n{\"level\":"}]}
    """#.utf8))
    expect("half an envelope is dropped, not shown",
           cut?.processes.first?.reason, "ERR failed to accept incoming stream")

    // process-compose files everything written to stderr under `level: error`, and cloudflared
    // writes its startup chatter there. Believing the envelope about a line that says `INF` in
    // its own text offers a connection notice as the reason a tunnel died — the same wrong
    // answer as trusting the last line, arrived at from the other direction.
    let chatter = DevStack.parseState(Data(#"""
    {"processes": [{"name": "tunnel", "state": "exited", "exit_code": 1,
      "error": "{\"level\":\"error\",\"message\":\"2026-08-22T13:03:55Z ERR failed to accept QUIC stream\"}\n{\"level\":\"error\",\"message\":\"2026-08-22T13:03:55Z INF Tunnel connection curve preferences\"}"}]}
    """#.utf8))
    expect("a line that calls itself INF is not the reason, whatever the envelope says",
           chatter?.processes.first?.reason, "2026-08-22T13:03:55Z ERR failed to accept QUIC stream")
    check("and the text is what settles it",
          StackLog.declaresCalm("2026-08-22T13:03:55Z INF Tunnel connection")
            && !StackLog.declaresCalm("bash: npm: command not found"))
    check("a line leading with a real severity is not calm for mentioning info later",
          !StackLog.declaresCalm("ERROR: could not read info file"))

    // cloudflared, uvicorn and next all colour their own output, and it survives into the tail.
    let coloured = DevStack.parseState(Data(#"""
    {"processes": [{"name": "web", "state": "exited", "exit_code": 1,
      "error": "\u001b[31mERROR\u001b[0m  build failed"}]}
    """#.utf8))
    expect("colour codes come off", coloured?.processes.first?.reason, "ERROR  build failed")

    check("and a process that left nothing behind has no reason to give",
          DevStack.parseState(Data(#"{"processes":[{"name":"web","state":"exited","exit_code":1}]}"#.utf8))?
            .processes.first?.reason == nil)
}

group("devstack: which broken process the row names") {
    // atrium, as its status command printed it. `blog` is first alphabetically and says nothing
    // — it exits silently because the build it waits on never completed. `build-blog` is the one
    // that actually failed, and the only entry in the whole document that says why. The row
    // named `blog` and drew no explanation at all, which is how three stacks stayed broken.
    let atrium = DevStack.parseState(Data(#"""
    {"state": "partial", "processes": [
      {"name": "api", "state": "healthy", "port": 8004},
      {"name": "blog", "state": "exited", "port": 4324, "exit_code": 1},
      {"name": "build-blog", "state": "exited", "exit_code": 127,
       "error": "{\"level\":\"error\",\"message\":\"bash: npm: command not found\"}"},
      {"name": "web", "state": "exited", "port": 3004, "exit_code": 1}]}
    """#.utf8))
    expect("three are down", atrium?.brokenNames.count, 3)
    expect("and the one named is the cause, not the first casualty",
           atrium?.rootCause?.name, "build-blog")

    // What the phone is told about a row it can actually see. `build-blog` listens on nothing,
    // so it never appears in a list of addresses; `blog` does, and answers for both.
    expect("a silent casualty is told what took it down",
           atrium.flatMap { s in s.processes.first { $0.name == "blog" }.flatMap(s.why) },
           "build-blog: bash: npm: command not found")
    expect("and the cause speaks for itself",
           atrium.flatMap { s in s.processes.first { $0.name == "build-blog" }.flatMap(s.why) },
           "bash: npm: command not found")

    // 127 is a shell saying "command not found" and is almost always a cause; 1 is the code
    // everything uses for everything, and says nothing about who started it.
    let codes = DevStack.parseState(Data(#"""
    {"processes": [{"name": "web", "state": "exited", "exit_code": 1},
                   {"name": "build", "state": "exited", "exit_code": 127}]}
    """#.utf8))
    expect("a specific exit code outranks a generic one", codes?.rootCause?.name, "build")

    // A tie keeps the document's own order, so the row does not reshuffle itself every read.
    let tied = DevStack.parseState(Data(#"""
    {"processes": [{"name": "a", "state": "exited", "exit_code": 1},
                   {"name": "b", "state": "exited", "exit_code": 1}]}
    """#.utf8))
    expect("nothing to choose between them keeps the first", tied?.rootCause?.name, "a")
    check("and a stack with nothing down names nobody",
          DevStack.parseState(Data(#"{"processes":[{"name":"api","state":"healthy"}]}"#.utf8))?
            .rootCause == nil)
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

group("stack logs: the pipe a line came down, kept for the post-mortem") {
    // Worthless for colouring — process-compose calls everything written to stderr an error, and
    // believing it paints a healthy stack red. Worth a great deal in the tail of a process that
    // has *already exited*, where "the last thing it wrote to stderr" is a much better guess at
    // what killed it than "the last thing it wrote".
    let e = StackLog.envelope(#"{"level":"error","process":"api","message":"bash: npm: command not found"}"#)
    expect("the message", e.message, "bash: npm: command not found")
    expect("and the level it was logged at", e.level, "error")
    check("a line that was never wrapped has no level",
          StackLog.envelope("just a line").level == nil)
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

group("a slash command is the one piece of that machinery somebody typed") {
    // The three rows one `/model` leaves behind. `withoutMachineBlocks` empties all three, and an
    // empty entry is dropped — so a session whose whole history so far is one switch read in the
    // pane as a session that had said nothing at all, which is the one thing a pane must not do.
    let rows = [
        #"{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>Caveat: x</local-command-caveat>"}}"#,
        #"{"type":"user","timestamp":"2026-08-24T03:24:21.355Z","message":{"role":"user","content":"<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>fable</command-args>"}}"#,
        #"{"type":"user","message":{"role":"user","content":"<local-command-stdout>Set model to \u001b[1mFable 5\u001b[22m and saved as your default for new sessions</local-command-stdout>"}}"#,
    ].joined(separator: "\n")
    let entries = Transcript.parse(rows)
    expect("the command and what it printed, and not the caveat between them", entries.count, 2)
    expect("what somebody typed is theirs", entries.first?.kind, Transcript.Entry.Kind.user)
    expect("said as one line", entries.first?.text, "/model fable")
    check("and stamped like any turn", entries.first?.time != nil)
    expect("what it printed is the machine answering", entries.last?.kind, Transcript.Entry.Kind.toolResult)
    expect("with the terminal's bold taken out", entries.last?.text,
           "Set model to Fable 5 and saved as your default for new sessions")

    expect("a command with nothing after it is just the command",
           Transcript.slashCommand(in: "<command-name>/recap</command-name><command-args></command-args>"), "/recap")
    expect("an argument comes with it",
           Transcript.slashCommand(in: "<command-name>/code-review</command-name><command-args>high</command-args>"), "/code-review high")
    expect("a name written without its slash is given one",
           Transcript.slashCommand(in: "<command-name>model</command-name>"), "/model")
    expect("the picker's own label for the row is not typed by anybody",
           Transcript.slashCommand(in: "<command-name>/model</command-name><command-message>model</command-message>"), "/model")
    check("ordinary prose is not a command", Transcript.slashCommand(in: "run /model for me") == nil)

    // A command whose expansion arrives in the same block keeps both: the line that was typed,
    // and whatever of the turn was not machinery.
    expect("the line typed leads what is left of the turn",
           Transcript.parse(#"{"type":"user","message":{"role":"user","content":"<command-name>/deploy</command-name>now please"}}"#).first?.text,
           "/deploy\nnow please")

    check("a command that printed nothing has no result",
          Transcript.commandOutput(in: "<local-command-stdout></local-command-stdout>") == nil)
    expect("stderr counts as printed too",
           Transcript.commandOutput(in: "<local-command-stderr>no such model</local-command-stderr>"), "no such model")
    check("and a turn with no command in it prints nothing",
          Transcript.commandOutput(in: "deploy it") == nil)
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
                  windowIndex: 0, tabIndex: 0, assistant: .claude)
}

group("hooks: reading a note") {
    let good = Data(#"{"event":"Stop","tty":"ttys004","at":1787039500,"session":"3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34"}"#.utf8)
    let note = HookBridge.parse(good)
    check("a whole note reads", note != nil)
    expect("its event", note?.event, .stop)
    expect("an old note gets the safe legacy meaning", note?.kind, .stop)
    expect("its tty", note?.tty, "ttys004")
    expect("its session id", note?.session, "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    // The script leaves the session out when it could not find one, and that is a note worth
    // keeping: the tty is what a reading is keyed on, and the id is only ever a shortcut.
    let noSession = HookBridge.parse(Data(#"{"event":"Stop","tty":"ttys004","at":1}"#.utf8))
    check("a note with no session id is still a note", noSession != nil)
    expect("and says so", noSession?.session, nil)

    let asked = Data(#"{"event":"PreToolUse","kind":"ask_user_question","tty":"ttys004","at":2,"tool_input":{"questions":[{"header":"Deploy","question":"Ship this build?","multiSelect":false,"options":[{"label":"Yes","description":"Deploy now"},{"label":"Not yet","description":"Keep testing"}]}]}}"#.utf8)
    let question = HookBridge.parse(asked)
    expect("a matched tool note carries its meaning", question?.kind, .askUserQuestion)
    expect("and the complete question", question?.questions.first?.text, "Ship this build?")
    expect("with unclipped option labels", question?.questions.first?.options.map(\.label),
           ["Yes", "Not yet"])
    expect("which become the existing numbered menu", question?.menu?.options.map(\.number),
           [1, 2])

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
    func note(_ kind: HookBridge.Kind, event: HookBridge.Event) -> [String: HookBridge.Note] {
        ["ttys004": HookBridge.Note(event: event, kind: kind, tty: "ttys004",
                                    at: now.addingTimeInterval(-1), session: nil)]
    }

    // Nothing installed is the state every reading has to be right in, so it is the first check.
    expect("with no notes, the screen is the whole answer",
           HookBridge.merge([:], into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .waiting)
    expect("a session nobody left a note for is untouched",
           HookBridge.merge(notes(.stop), into: ["B": .working("x")], sessions: sessions,
                            now: now)["B"],
           .working("x"))

    // The screen remains the authority for look-only, turn-boundary and opening input notes.
    expect("a question on screen outranks a Stop",
           HookBridge.merge(notes(.stop), into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .waiting)
    expect("and a prompt going in leaves it alone",
           HookBridge.merge(notes(.userPromptSubmit), into: ["A": .waiting],
                            sessions: sessions, now: now)["A"],
           .waiting)

    // Opening input events only gate the screen parser. In particular, auto mode emits both
    // permission kinds for approvals it handles itself without ever showing a dialog. The
    // closing event can still beat a picker that remains in one stale capture.
    expect("AskUserQuestion does not replace an idle screen",
           HookBridge.merge(note(.askUserQuestion, event: .preToolUse), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)
    expect("nor a screen that could not be read",
           HookBridge.merge(note(.askUserQuestion, event: .preToolUse), into: ["A": .unknown],
                            sessions: sessions, now: now)["A"],
           .unknown)
    expect("PostToolUse retracts a picker left in the capture",
           HookBridge.merge(note(.askUserQuestionDone, event: .postToolUse),
                            into: ["A": .waiting], sessions: sessions, now: now)["A"],
           .idle)
    expect("an auto-approved PermissionRequest keeps the working screen",
           HookBridge.merge(note(.permissionRequest, event: .permissionRequest),
                            into: ["A": .working("Reviewing approval request")],
                            sessions: sessions, now: now)["A"],
           .working("Reviewing approval request"))
    expect("an auto-approved permission notification keeps the idle screen",
           HookBridge.merge(note(.permissionPrompt, event: .notification), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)
    expect("but idle_prompt still only asks for a screen reading",
           HookBridge.merge(note(.idlePrompt, event: .notification), into: ["A": .idle],
                            sessions: sessions, now: now)["A"],
           .idle)

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
    expect("an event we share keeps its entry and gains the matched one",
           (hooks["PreToolUse"] as? [[String: Any]])?.count, 2)
    expect("an event we share keeps theirs and gains ours",
           (hooks["Stop"] as? [[String: Any]])?.count, 2)
    for registration in HookBridge.Registration.all {
        check("\(registration.event.rawValue)/\(registration.matcher ?? "all") is wired up",
              (hooks[registration.event.rawValue] as? [[String: Any]] ?? []).contains { group in
                  (group["matcher"] as? String) == registration.matcher &&
                  (group["hooks"] as? [[String: Any]] ?? []).contains(where: HookBridge.isOurs)
              })
    }
    let notifications = hooks["Notification"] as? [[String: Any]] ?? []
    expect("Notification is split into two matcher groups",
           Set(notifications.compactMap { $0["matcher"] as? String }),
           Set(["permission_prompt", "idle_prompt"]))

    // Pressing Install twice is a thing people do.
    let twice = HookBridge.adding("/Users/x/.config/clawdline/hook.sh", to: after)
    expect("installing twice leaves one of ours",
           ((twice["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]])?.count, 2)

    // A matcher group can contain several owners. Taking our handler out must leave both the
    // group metadata and the neighbouring command exactly where the user put them.
    var mixed = after
    var mixedHooks = mixed["hooks"] as? [String: Any] ?? [:]
    var stopGroups = mixedHooks["Stop"] as? [[String: Any]] ?? []
    var ourStop = stopGroups.removeLast()
    var handlers = ourStop["hooks"] as? [[String: Any]] ?? []
    handlers.append(["type": "command", "command": "/opt/theirs/same-group.sh"])
    ourStop["hooks"] = handlers
    ourStop["once"] = true
    stopGroups.append(ourStop)
    mixedHooks["Stop"] = stopGroups
    mixed["hooks"] = mixedHooks
    let unmixed = HookBridge.removing(from: mixed)
    let remainingStop = ((unmixed["hooks"] as? [String: Any])?["Stop"] as? [[String: Any]]) ?? []
    check("removing ours preserves another handler in the same group",
          remainingStop.contains { group in
              group["once"] as? Bool == true &&
              (group["hooks"] as? [[String: Any]] ?? []).contains {
                  $0["command"] as? String == "/opt/theirs/same-group.sh"
              }
          })

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
    expect("the path is quoted", command,
           "'/Users/a b/.config/clawdline/hook.sh' Stop stop")
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

    func run(_ event: String, kind: String? = nil, payload: String,
             into: URL? = dir) -> (out: String, code: Int32) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/bin/sh")
        p.arguments = [script, event] + (kind.map { [$0] } ?? [])
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

    // **Every assertion below runs whether or not a note was written**, and that is not
    // fussiness. The script writes nothing when it cannot find a tty above itself, which is the
    // normal case on a build machine and never the case here — so a group that only asserted
    // "when there is a note" counted six more checks locally than in CI, and the count the
    // README carries is checked against the run. A test whose *number* of assertions depends on
    // the machine is a test that turns the guard around it into a coin toss.
    func notes(in dir: URL) -> [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? [])
            .filter { $0.hasSuffix(".json") }
    }
    let written = notes(in: dir)
    let note = written.first.flatMap {
        HookBridge.parse((try? Data(contentsOf: dir.appendingPathComponent($0))) ?? Data())
    }
    let noTTY = written.isEmpty      // no controlling terminal above this process

    check("it wrote a note, or found no terminal to name one after", noTTY || note != nil)
    check("named after the tty it found",
          noTTY || written.first == "\(note?.tty ?? "?").json")
    check("carrying the event it was told", noTTY || note?.event == .stop)
    check("and the session id, cut out of a payload with quotes in it",
          noTTY || note?.session == "3f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a34")

    // Overwritten, never appended: a session has one note and it is the newest one.
    _ = run("Notification", payload: payload)
    let after = notes(in: dir)
    check("a second note replaces the first", noTTY || after.count == 1)
    check("with the newer event", noTTY || written.first.flatMap {
        HookBridge.parse((try? Data(contentsOf: dir.appendingPathComponent($0))) ?? Data())?.event
    } == .notification)

    // Every way this can be asked to do nothing, it has to do nothing quietly. A hook that
    // exits non-zero is making a decision about somebody's turn.
    expect("no event argument, no complaint", run("", payload: payload).code, 0)
    expect("empty stdin, no complaint", run("Stop", payload: "").code, 0)
    expect("no directory to write into, no complaint",
           run("Stop", payload: payload, into: nil).code, 0)

    // Pin the tty through the script's own per-session cache so this half is deterministic even
    // on a CI process with no controlling terminal.
    let structured = dir.appendingPathComponent("structured", isDirectory: true)
    try? FileManager.default.createDirectory(at: structured, withIntermediateDirectories: true)
    let session = "4f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a35"
    try? Data("ttys777".utf8).write(
        to: structured.appendingPathComponent(".tty-\(session)"), options: .atomic)
    let askPayload = #"{"session_id":"4f6a1c2e-7b4d-4a9e-8c15-2d0e9f7b6a35","hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"header":"Choice","question":"Which path?","options":[{"label":"Keep the full label — \"quoted\"","description":"first"},{"label":"另一條路","description":"second"}],"multiSelect":false}]}}"#
    let askRun = run("PreToolUse", kind: "ask_user_question", payload: askPayload,
                     into: structured)
    let askFile = structured.appendingPathComponent("ttys777.json")
    let askData = (try? Data(contentsOf: askFile)) ?? Data()
    let askNote = HookBridge.parse(askData)
    expect("a structured question still writes nothing to stdout", askRun.out, "")
    expect("and exits 0", askRun.code, 0)
    expect("tool_input survives as a complete question in the note",
           askNote?.questions.first?.text, "Which path?")
    expect("including labels JSON has to escape",
           askNote?.questions.first?.options.map(\.label),
           ["Keep the full label — \"quoted\"", "另一條路"])
    check("the note has a hard size ceiling", askData.count < 34 * 1024)

    // idle_prompt is a nudge, not a state transition. It must not replace a still-open question,
    // or the one-minute notification would make precisely the question disappear again.
    _ = run("Notification", kind: "idle_prompt", payload: askPayload, into: structured)
    expect("idle_prompt does not erase an authoritative question",
           HookBridge.parse((try? Data(contentsOf: askFile)) ?? Data())?.kind,
           .askUserQuestion)

    let huge = String(repeating: "x", count: 40_000)
    let hugePayload = "{\"session_id\":\"\(session)\",\"tool_input\":{\"questions\":[{\"question\":\"Q\",\"options\":[{\"label\":\"A\",\"description\":\"\(huge)\"},{\"label\":\"B\"}]}]}}"
    _ = run("PreToolUse", kind: "ask_user_question", payload: hugePayload, into: structured)
    let capped = (try? Data(contentsOf: askFile)) ?? Data()
    check("oversized tool_input is omitted rather than growing the note", capped.count < 34 * 1024)
    check("an omitted oversized input does not become partial JSON",
          HookBridge.parse(capped)?.questions.isEmpty == true)
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
                  windowIndex: 0, tabIndex: 0, assistant: .claude, cwd: cwd)
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

group("a long turn keeps enough time to announce its finish") {
    let session = hookTarget("LONG")
    let epoch = Date(timeIntervalSince1970: 1_000)
    func change(_ from: SessionState, _ to: SessionState) -> StateHook.Change {
        StateHook.Change(session: session, from: from, to: to)
    }

    var launchedLate = StateHook.FinishTracker()
    expect("a first reading is still not a finished turn",
           launchedLate.update(states: [session.id: .working("Working (2m 5s • esc to interrupt)")],
                               sessions: [session], changes: [], now: epoch,
                               threshold: 120).count, 0)
    expect("but its own clock survives launching halfway through",
           launchedLate.update(states: [session.id: .idle], sessions: [session],
                               changes: [change(.working("Working (2m 5s)"), .idle)],
                               now: epoch.addingTimeInterval(1), threshold: 120).map(\.id),
           [session.id])

    var interrupted = StateHook.FinishTracker()
    _ = interrupted.update(states: [session.id: .working("Generating… (1s)")],
                           sessions: [session], changes: [change(.idle, .working("Generating… (1s)"))],
                           now: epoch, threshold: 120)
    _ = interrupted.update(states: [session.id: .waiting], sessions: [session],
                           changes: [change(.working("Generating… (2m 3s)"), .waiting)],
                           now: epoch.addingTimeInterval(123), threshold: 120)
    _ = interrupted.update(states: [session.id: .working("Generating… (1s)")],
                           sessions: [session], changes: [change(.waiting, .working("Generating… (1s)"))],
                           now: epoch.addingTimeInterval(140), threshold: 120)
    expect("a permission pause does not turn one long task into two short ones",
           interrupted.update(states: [session.id: .idle], sessions: [session],
                              changes: [change(.working("Generating… (8s)"), .idle)],
                              now: epoch.addingTimeInterval(148), threshold: 120).map(\.id),
           [session.id])

    var short = StateHook.FinishTracker()
    _ = short.update(states: [session.id: .working("Working (8s)")], sessions: [session],
                     changes: [], now: epoch, threshold: 120)
    expect("the two-minute preference remains a real threshold",
           short.update(states: [session.id: .idle], sessions: [session],
                        changes: [change(.working("Working (8s)"), .idle)],
                        now: epoch.addingTimeInterval(2), threshold: 120).count, 0)
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

group("push notifications identify the session and its project") {
    let session = hookTarget("A9F3", title: "✳ fix the webhook (claude)")
    let waiting = StateHook.pushMessage(
        for: session, project: "clawdline", event: "is waiting for an answer")

    expect("the cleaned session task is the title", waiting.title, "fix the webhook")
    expect("the project and event are the body", waiting.body,
           "clawdline is waiting for an answer")

    let deploy = StateHook.pushMessage(
        for: session, project: "clawdline", event: "deploy failed")
    expect("deploy notifications use the same informative shape", deploy,
           StateHook.PushMessage(title: "fix the webhook", body: "clawdline deploy failed"))
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

group("a cache stamp has to see through a symlink") {
    // The bug this exists for: ~/.claude/project-icons.json is normally a link into a checkout,
    // and neither attributesOfItem(atPath:) nor URL.resourceValues(forKeys:) follows one. Both
    // describe the link — 64 bytes, and the modification time of the day it was made — so the
    // stamp never changed, the registry could be rewritten all afternoon, and only relaunching
    // the app picked it up.
    let dir = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-stamp-\(getpid())")
    let fm = FileManager.default
    try? fm.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: dir) }

    let real = dir.appendingPathComponent("real.json")
    let link = dir.appendingPathComponent("link.json")
    try? Data("first".utf8).write(to: real)
    try? fm.createSymbolicLink(at: link, withDestinationURL: real)

    let before = Paths.stamp(of: link)
    check("a link and its target stamp the same", before == Paths.stamp(of: real))

    // Enough of a change to move the size, which is the half that does not depend on a clock.
    try? Data("second, and appreciably longer than the first".utf8).write(to: real)
    check("writing through the target changes the link's stamp", Paths.stamp(of: link) != before)
    check("and it still agrees with the target", Paths.stamp(of: link) == Paths.stamp(of: real))

    check("a file that is not there stamps as nothing rather than crashing",
          Paths.stamp(of: dir.appendingPathComponent("absent.json")) == "0-0")
}

group("telling two devices with the same name apart") {
    // Pressing "Open in a browser" twice mints two devices called the same thing, and two
    // identical rows is a list nobody can act on. What separates them is not a better name —
    // two browsers deserve the same name — it is when each was last used.
    let now = Date()
    func ago(_ seconds: TimeInterval) -> String? {
        DeviceChips.ago(now.addingTimeInterval(-seconds), now: now)
    }
    expect("just now", ago(5), "now")
    expect("under an hour is minutes", ago(125), "2m")
    expect("under a day is hours", ago(3 * 3600 + 100), "3h")
    expect("beyond that is days", ago(4 * 86_400), "4d")
    expect("a clock that ran backwards is not negative", ago(-30), "now")
    // The one that matters: a device minted and never used has no time at all, and that is
    // exactly the one it is safe to take away.
    check("never seen says nothing", DeviceChips.ago(nil) == nil)
}

group("a top-level state from the wrong vocabulary") {
    // The two `state` keys mean different things, and the first project outside this repository
    // to write one of these sent `healthy` — a process word — at the top. Taken literally it put
    // a healthy stack under a mark that said nobody had agreed to run it.
    func read(_ json: String) -> DevStack.State? { DevStack.parseState(Data(json.utf8)) }
    let processes = """
    "processes": [{"name": "api", "port": 1, "state": "healthy"},
                  {"name": "web", "port": 2, "state": "healthy"}]
    """
    expect("a process word at the top is not believed",
           read("{\"state\": \"healthy\", \(processes)}")?.state, "running")
    expect("and neither is anything else unrecognised",
           read("{\"state\": \"lovely\", \(processes)}")?.state, "running")
    expect("one of the four is taken as written",
           read("{\"state\": \"partial\", \(processes)}")?.state, "partial")
    expect("unknown is one of the four, and cannot be derived",
           read("{\"state\": \"unknown\", \(processes)}")?.state, "unknown")
    expect("no top-level state at all derives one",
           read("{\(processes)}")?.state, "running")
    expect("and a process that is down makes it partial",
           read("{\"processes\": [{\"name\": \"a\", \"state\": \"healthy\"}, "
                + "{\"name\": \"b\", \"state\": \"exited\"}]}")?.state, "partial")
}

// MARK: - Starting a session from somewhere else

/// A request with nothing in it but what the test is about. `Host` is always right, because the
/// rebinding refusal comes before everything else and a wrong one would make every case below
/// pass for the wrong reason.
func remoteRequest(_ method: String, _ target: String,
                   headers: [String: String] = [:],
                   body: String? = nil) -> RemoteServer.Request {
    var head = "\(method) \(target) HTTP/1.1\r\nHost: 127.0.0.1:\(Config.shared.remotePort)\r\n"
    for (key, value) in headers.sorted(by: { $0.key < $1.key }) { head += "\(key): \(value)\r\n" }
    var request = RemoteServer.Request(head: Data((head + "\r\n").utf8))!
    // Set rather than appended to the head: the reader assembles the body from the socket, so a
    // test that wrote one into the head would be exercising a parse no real request takes.
    if let body { request.body = Data(body.utf8) }
    return request
}

/// The `code` out of an error envelope, and "" for anything that is not one.
func remoteErrorCode(_ response: RemoteServer.Response) -> String {
    let body = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    return ((body?["error"] as? [String: Any])?["code"] as? String) ?? ""
}

/// The `message` out of one. Only worth asking when two refusals share a code and differ in
/// what they were about — which is the case for the two 404s on the start route.
func remoteErrorMessage(_ response: RemoteServer.Response) -> String {
    let body = (try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]
    return ((body?["error"] as? [String: Any])?["message"] as? String) ?? ""
}

group("a project folder says which directory it is, and is not taken at its word") {
    // Claude Code names the folder after the working directory with every character that is not
    // a letter or a digit turned into a dash. That map is many-to-one, so the name can never be
    // read backwards — `-Users-me-code-cairn-frontend` is `cairn/frontend` and `cairn-frontend`
    // equally. The path is read out of the transcripts instead and checked against the name.
    expect("separators", StartPoints.slug(of: "/Users/me/code/notebook"), "-Users-me-code-notebook")
    expect("a space is a dash too", StartPoints.slug(of: "/a/My Work"), "-a-My-Work")
    expect("and a dot, and an underscore", StartPoints.slug(of: "/a/b.c_d"), "-a-b-c-d")
    expect("a dash was already a dash", StartPoints.slug(of: "/a/mixed-case"), "-a-mixed-case")
    expect("case survives", StartPoints.slug(of: "/a/Mixed-Case"), "-a-Mixed-Case")
    expect("and anything that is not ASCII does not",
           StartPoints.slug(of: "/a/專案"), "-a---")

    // The reason every candidate is checked: a transcript quotes other people's directories.
    // Observed on this machine — a session in `some-app` had an `another_project` cwd sitting
    // in the last hundred kilobytes of it, inside something somebody had pasted in.
    let text = #"{"type":"user","cwd":"/Users/me/code/other"}"# + "\n"
        + #"{"type":"user","cwd":"/Users/me/code/thing"}"#
    expect("every cwd in the text, not the first", StartPoints.cwds(in: text).count, 2)
    expect("in the order they appeared",
           StartPoints.cwds(in: text), ["/Users/me/code/other", "/Users/me/code/thing"])
    expect("escapes come back out",
           StartPoints.cwds(in: #"{"cwd":"/Users/me/it\"s \\ here"}"#), ["/Users/me/it\"s \\ here"])
    expect("half a line is not half a path",
           StartPoints.cwds(in: "{\"cwd\":\"/Users/me/cut\n"), [])

    // End to end over files, because the ordering rule — name wins, position does not — is the
    // whole point and lives across the two halves.
    let folder = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-places-\(getpid())")
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let one = folder.appendingPathComponent("one.jsonl")
    try? Data((#"{"type":"user","cwd":"/Users/me/code/somewhere-else"}"# + "\n"
               + #"{"type":"user","cwd":"/Users/me/code/thing"}"# + "\n").utf8).write(to: one)
    expect("the cwd that matches the folder's name is the one believed",
           StartPoints.directory(named: "-Users-me-code-thing", transcripts: [one]),
           "/Users/me/code/thing")
    expect("and a folder whose transcripts never mention it proves nothing",
           StartPoints.directory(named: "-Users-me-code-missing", transcripts: [one]), nil)
    expect("nor does one with no transcripts at all",
           StartPoints.directory(named: "-Users-me-code-thing", transcripts: []), nil)
}

group("the line a new tab is given, before anything types it") {
    // The list these come from is derived from the filesystem, which is to say from names the
    // person at the Mac did not necessarily choose. A quote in one of them must not be able to
    // end the quoting and start a command.
    expect("an ordinary path is still quoted",
           StartPoints.itermLine(cwd: "/Users/me/code/notebook"), "cd '/Users/me/code/notebook' && claude")
    expect("a space changes nothing about it",
           StartPoints.itermLine(cwd: "/a/My Work"), "cd '/a/My Work' && claude")
    expect("a quote cannot close the quoting",
           StartPoints.itermLine(cwd: "/a/it's here"), "cd '/a/it'\\''s here' && claude")
    expect("a backslash is a backslash inside single quotes",
           StartPoints.itermLine(cwd: "/a/back\\slash"), "cd '/a/back\\slash' && claude")
    check("and nothing a client sent is anywhere in it",
          !StartPoints.itermLine(cwd: "/a/b").contains(";"))

    // The one thing quoting cannot save, so it never reaches the quoting: on this path the line
    // *is* the submission, and a newline in the middle of one runs the second half as a command.
    check("a newline in a directory name is not a place at all",
          !StartPoints.usable("/a/two\nlines"))
    check("nor is a carriage return", !StartPoints.usable("/a/two\rlines"))
    check("nor a relative path", !StartPoints.usable("code/notebook"))
    check("an ordinary absolute path is", StartPoints.usable("/Users/me/code/notebook"))
    check("and so is one with a quote in it, which the quoting handles",
          StartPoints.usable("/a/it's here"))
}

group("which terminal a session is started in, and when none of them will do") {
    let iterm = StartPoints.itermBundleID
    func plan(_ scope: String, _ running: Set<String>, _ tmux: Bool) -> StartPoints.Plan {
        StartPoints.plan(scope: scope, running: running, hasTmux: tmux)
    }
    expect("iTerm2 is named and open", plan(iterm, [iterm], false), .iterm)
    expect("no scope at all means no preference, and that is iTerm2 first",
           plan("", [iterm], true), .iterm)
    expect("named among others", plan("com.apple.Terminal,\(iterm)", [iterm], false), .iterm)
    expect("iTerm2 is named and shut, and there is a tmux to go through instead",
           plan(iterm, [], true), .tmux)
    expect("iTerm2 is named and shut and there is nothing else",
           plan(iterm, [], false), .notRunning(app: iterm))

    // The refusal that matters: a terminal this cannot drive must be said out loud rather than
    // quietly handed to iTerm2, because a session that opened somewhere nobody was looking is
    // worse than a sentence saying it did not open.
    expect("another terminal, with tmux under it", plan("com.mitchellh.ghostty", [], true), .tmux)
    expect("another terminal and no tmux is refused by name",
           plan("com.mitchellh.ghostty", ["com.mitchellh.ghostty"], false),
           .cannotDrive(app: "com.mitchellh.ghostty"))
    check("and it never silently becomes iTerm2",
          plan("com.apple.Terminal", [iterm], false) != .iterm)
}

group("the list of places, tidied") {
    let now = Date()
    func place(_ path: String, _ ago: TimeInterval) -> StartPoints.Place {
        StartPoints.Place(id: StartPoints.id(for: path), path: path,
                          label: (path as NSString).lastPathComponent,
                          at: now.addingTimeInterval(-ago))
    }
    let all: [StartPoints.Place] = [
        place("/a/old", 900), place("/a/new", 10), place("/a/old", 5),
        place("/a/gone", 1), place("/a/two\nlines", 0), place("relative", 0),
    ]
    let tidied = StartPoints.tidy(all, isDirectory: { $0 != "/a/gone" })
    expect("what is not there any more, and what cannot be typed, are not offered",
           tidied.count, 2)
    expect("newest first, at the newest time the same directory was seen",
           tidied.map(\.path), ["/a/old", "/a/new"])
    expect("a cap is a cap", StartPoints.tidy(all, limit: 1, isDirectory: { _ in true }).count, 1)

    // The id is opaque and stable, which is the entire reason a client never sends a path.
    expect("the same path is the same id twice",
           StartPoints.id(for: "/a/b"), StartPoints.id(for: "/a/b"))
    check("different paths are different ids",
          StartPoints.id(for: "/a/b") != StartPoints.id(for: "/a/c"))
    check("and there is no path anywhere in it",
          !StartPoints.id(for: "/Users/me/code/notebook").contains("notebook"))

    // Codex records every cwd it is launched in, including locations its own apps and another
    // assistant made. They are true cwd values and still not projects somebody created.
    func durable(_ path: String) -> Bool {
        StartPoints.isDurablePlace(path, home: "/Users/me",
                                   temporary: "/private/var/folders/me/T")
    }
    check("the home folder is not invented into a project", !durable("/Users/me"))
    check("Codex Desktop's private workspace is not one",
          !durable("/Users/me/.codex/.chatgpt-projects/g-p-123"))
    check("nor is the Documents workspace Codex Desktop creates",
          !durable("/Users/me/Documents/Codex/2026-08-24/new-chat"))
    check("an assistant scratchpad under the system temp root is not one",
          !durable("/private/var/folders/me/T/claude/scratchpad/probe"))
    check("an ordinary checkout remains a place",
          durable("/Users/me/code/clawdline"))
}

group("starting a session is behind the write gate, like everything else that runs code") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    func post(_ target: String, token: String?, key: String?) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if let key { headers["Idempotency-Key"] = key }
        return RemoteServer.shared.route(remoteRequest("POST", target, headers: headers))
    }
    let bogusID = "0123456789abcdef"
    let bogus = "/v1/places/\(bogusID)/start"

    // No token at all, and this one is checked before anything else knows the route exists.
    let anonymous = post(bogus, token: nil, key: UUID().uuidString)
    expect("no token is refused", anonymous.status, 401)
    expect("and says so", remoteErrorCode(anonymous), "unauthorized")
    expect("reading the list needs one too",
           RemoteServer.shared.route(remoteRequest("GET", "/v1/places")).status, 401)

    // The switch the owner of the Mac holds, before the capability this device holds — so a
    // device that may not send cannot learn that it may not while the whole feature is off.
    Config.shared.remoteWrite = false
    let shut = post(bogus, token: writer.token, key: UUID().uuidString)
    expect("the write switch is checked first", remoteErrorCode(shut), "write_disabled")
    expect("and that is a 403", shut.status, 403)

    Config.shared.remoteWrite = true
    let readOnly = post(bogus, token: reader.token, key: UUID().uuidString)
    expect("a device that may read and not send is refused", readOnly.status, 403)
    expect("and it is a capability refusal, not the switch",
           remoteErrorCode(readOnly), "forbidden")

    let noKey = post(bogus, token: writer.token, key: nil)
    expect("a retryable write with no idempotency key is refused", noKey.status, 400)
    expect("as a bad request", remoteErrorCode(noKey), "bad_request")

    // Past all three gates, and the id still has to be one the server put on the list. Nothing a
    // client sends is ever a path, so an unknown id is the end of the road rather than a
    // directory that failed validation.
    let unknown = post(bogus, token: writer.token, key: UUID().uuidString)
    expect("an id nobody was given is not a place", unknown.status, 404)
    expect("and says nothing else about it", remoteErrorCode(unknown), "not_found")
    let empty = post("/v1/places//start", token: writer.token, key: UUID().uuidString)
    expect("nor is an empty one", empty.status, 404)

    // **Which assistant is a path segment, and it is a name rather than a command.** The body on
    // this route is still not read at all, so the last segment is the only thing that decides
    // what gets run — and it is resolved against a two-case enum before anything else happens.
    let invented = post("/v1/places/\(bogusID)/start/emacs", token: writer.token,
                        key: UUID().uuidString)
    expect("an assistant nobody has heard of is a 404", invented.status, 404)
    expect("and it is refused before the place is even looked up",
           remoteErrorMessage(invented), "No assistant named that")
    let sneaky = post("/v1/places/\(bogusID)/start/codex;rm%20-rf%20~", token: writer.token,
                      key: UUID().uuidString)
    expect("and so is a name with a command stuck to it", sneaky.status, 404)
    // A real one gets past the name and lands on the same missing place as the plain route,
    // which is the proof that the segment chooses and does not carry.
    let codex = post("/v1/places/\(bogusID)/start/codex", token: writer.token,
                     key: UUID().uuidString)
    expect("a named assistant gets as far as the place", remoteErrorCode(codex), "not_found")
    let tooDeep = post("/v1/places/\(bogusID)/start/codex/now", token: writer.token,
                       key: UUID().uuidString)
    expect("and nothing deeper than that is a route", tooDeep.status, 404)

    // The route this replaced took a `cwd` and a `command` out of the body and ran the second in
    // the first, which behind a tunnel is "run anything anywhere" with a token in front of it.
    let old = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/sessions",
        headers: ["Authorization": "Bearer \(writer.token)",
                  "Idempotency-Key": UUID().uuidString]))
    expect("and the route that took a path in the body is gone", old.status, 404)
}

group("the page is given the words it draws the start sheet with") {
    // A string in `Copy` is not a string on the page. `strings(for:)` is the only thing that
    // decides what a browser is told, and thirteen of these sat translated into fourteen
    // languages for a release without being in it — which is a feature nobody outside English
    // could read, and nothing anywhere went red about it.
    // The header only decides anything while the app is following whoever is asking. A machine
    // running the tests may well have picked a language for the bar, and that choice wins over a
    // browser — so it is put back to `auto` here and restored, or this group would be reading
    // whichever language this Mac happens to be set to.
    let wasLanguage = Config.shared.language
    defer { Config.shared.language = wasLanguage }
    Config.shared.language = "auto"

    func words(language: String) -> [String: Any] {
        let response = RemoteServer.shared.route(
            remoteRequest("GET", "/v1/strings", headers: ["Accept-Language": language]))
        return ((try? JSONSerialization.jsonObject(with: response.body)) as? [String: Any]) ?? [:]
    }

    let english = words(language: "en")
    let needed = ["webStart", "webStartLabel", "webStartPick", "webStartEmpty", "webStartFilter",
                  "webStarting", "webStartWaiting", "webStartSlow", "webStartFailed",
                  "webStartGone", "webStartTerminalClosed", "webStartTerminalUnsupported",
                  "webStartOff"]
    let absent = needed.filter { (english[$0] as? String ?? "").isEmpty }
    check("every word the start sheet draws is published", absent.isEmpty,
          "not on /v1/strings: " + absent.joined(separator: ", "))

    // The page writes its own sentence around the terminal's name, which arrives in the error
    // object rather than in the copy. A translation that dropped the hole would leave a phone
    // saying that something unnamed is not running.
    let holeless = L.catalog.filter {
        !$0.copy.webStartTerminalClosed.contains("{app}")
            || !$0.copy.webStartTerminalUnsupported.contains("{app}")
    }.map { $0.tag }.sorted()
    check("every language keeps the hole the terminal's name goes in", holeless.isEmpty,
          holeless.joined(separator: ", "))

    // And they arrive in the language that was asked for, which is the entire reason the page
    // fetches this before it draws anything.
    let french = words(language: "fr")
    check("and they arrive in the language the browser asked for",
          (french["webStart"] as? String).map { !$0.isEmpty && $0 != (english["webStart"] as? String) } == true,
          "fr said \((french["webStart"] as? String) ?? "nothing")")
}

group("a deploy is news only when it stops running") {
    // The whole feature is one rule applied to two readings, and every way of getting it wrong
    // is a phone buzzing about something that did not just happen.
    func changed(_ before: [String: String], _ after: [String: String]) -> [String] {
        DeployWatch.finished(from: before, to: after).map { "\($0.repo):\($0.ok)" }
    }

    expect("running to ok is a success",
           changed(["a": "running"], ["a": "ok"]), ["a:true"])
    expect("running to fail is a failure",
           changed(["a": "running"], ["a": "fail"]), ["a:false"])
    expect("still running is nothing",
           changed(["a": "running"], ["a": "running"]), [])

    // The one that would have made this lie. A repo read for the first time has no previous
    // state, and every deploy that ever finished is sitting in that file waiting to be
    // announced — so opening the app on a Monday would report Friday's deploy as news.
    expect("a first reading is not a transition",
           changed([:], ["a": "ok"]), [])
    expect("a first reading of a failure is not a transition either",
           changed([:], ["a": "fail"]), [])

    // Starting one is not news: you started it.
    expect("ok to running is nothing", changed(["a": "ok"], ["a": "running"]), [])
    expect("an outcome that has not changed is nothing",
           changed(["a": "fail"], ["a": "fail"]), [])

    // A state nobody has defined must not read as an outcome by accident — `!= "running"` is
    // the test, so anything unrecognised lands in the failure branch rather than being dropped.
    expect("an unknown state that follows running still counts, as a failure",
           changed(["a": "running"], ["a": "cancelled"]), ["a:false"])

    // Reading several projects at once, which is the normal case.
    expect("each repo is judged on its own",
           changed(["a": "running", "b": "running", "c": "ok"],
                   ["a": "ok", "b": "running", "c": "running"]), ["a:true"])

    // A project whose status file went away — the reading simply does not mention it, and a
    // repo that is not in the new reading has not finished anything.
    expect("a repo that disappears says nothing",
           changed(["a": "running"], [:]), [])
}

group("answering a menu is a byte, and only ever one of ten") {
    // This is the only path in the app that writes a raw byte into a tty from the network, so the
    // allowlist is the whole security argument. It lives in `Targets.answer`, not at the route:
    // a second route added later would otherwise have to remember to repeat it.
    let session = TargetSession(backend: .tmux, id: "%nope%", name: "x", tty: "/dev/ttys99",
                                windowIndex: 0, tabIndex: 0, assistant: .claude)

    for bad: UInt8 in [0x1b, 0x0d, 0x0a, 0x03, 0x30, 0x41, 0x7f, 0x00] {
        check("byte \(bad) is refused before it reaches a terminal",
              Targets.answer(bad, to: session) == "That is not a key this can send.")
    }

    // The allowed ones are not exercised here on purpose — they would run osascript against a
    // session that does not exist. What is asserted is that they get *past* the allowlist, which
    // is the only thing this function decides.
    for good: UInt8 in [0x31, 0x39, 0x09] {
        check("byte \(good) is not refused by the allowlist",
              Targets.answer(good, to: session) != "That is not a key this can send.")
    }
}

group("the key route is gated like every other write") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    // A fresh idempotency key every time, or the second call with the same body would be answered
    // out of the ten-minute cache and assert nothing. The first version of this test omitted the
    // header entirely, and every case passed — as a 400 for the missing header rather than for the
    // reason it claimed. A green check that is green for the wrong reason is worse than a red one.
    func key(_ token: String?, _ body: String,
             idempotency: String? = nil) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        headers["Content-Type"] = "application/json"
        headers["Idempotency-Key"] = idempotency ?? UUID().uuidString
        return RemoteServer.shared.route(
            remoteRequest("POST", "/v1/sessions/%nope%/key", headers: headers, body: body))
    }

    Config.shared.remoteWrite = true
    let anonymous = key(nil, "{\"key\":\"1\"}")
    expect("no token is refused", anonymous.status, 401)

    let readOnly = key(reader.token, "{\"key\":\"1\"}")
    expect("a device that may only read cannot answer a menu", readOnly.status, 403)

    Config.shared.remoteWrite = false
    expect("the write switch is checked first",
           remoteErrorCode(key(writer.token, "{\"key\":\"1\"}")), "write_disabled")

    Config.shared.remoteWrite = true
    // Anything that is not a menu key is refused at the parse, before a session is even looked up
    // — so the shape of the request cannot be used to probe which sessions exist.
    for bad in ["0", "a", "", "escape", "10", "11", "\\u001b", "1 "] {
        let out = key(writer.token, "{\"key\":\"\(bad)\"}")
        expect("key \"\(bad)\" is a bad request", out.status, 400)
    }
    expect("a missing key is too", key(writer.token, "{}").status, 400)

    // The header the write gate insists on, checked here too because this route is the one where a
    // retry is not harmless: the same digit arriving twice answers a question and then types a
    // stray character into whatever replaced it.
    var noKey: [String: String] = ["Authorization": "Bearer \(writer.token)"]
    noKey["Content-Type"] = "application/json"
    expect("and an answer without an Idempotency-Key is refused",
           RemoteServer.shared.route(remoteRequest("POST", "/v1/sessions/%nope%/key",
                                                   headers: noKey,
                                                   body: "{\"key\":\"1\"}")).status, 400)

    // Retried with the same key, the stored answer comes back rather than a second keystroke.
    let once = UUID().uuidString
    let first = key(writer.token, "{\"key\":\"2\"}", idempotency: once)
    let again = key(writer.token, "{\"key\":\"2\"}", idempotency: once)
    expect("a retry is the stored answer, not a second press", again.status, first.status)

    // A well-formed key against a session that is not there is a 404, which means the parse ran
    // first and the allowlist did its job before anything went looking for a terminal.
    expect("a good key against no session is a 404",
           key(writer.token, "{\"key\":\"3\"}").status, 404)
    expect("and tab is a good key", key(writer.token, "{\"key\":\"tab\"}").status, 404)
}

group("Git porcelain and numstat become the web status payload") {
    let status = """
    # branch.oid d5c61e9f91c46a77
    # branch.head main
    # branch.upstream origin/main
    # branch.ab +2 -3
    1 MM N... 100644 100644 100644 aaaaaaa bbbbbbb Sources/Foo.swift
    2 R. N... 100644 100644 100644 ccccccc ddddddd R100 Sources/New Name.swift\tSources/Old Name.swift
    1 A. N... 000000 100644 100644 0000000 eeeeeee Sources/Added.swift
    1 .D N... 100644 100644 000000 fffffff fffffff Sources/Gone.swift
    1 .M N... 100644 100644 100644 1111111 2222222 Assets/blob.png
    ? Notes/new file.txt
    u UU N... 100644 100644 100644 100644 3333333 4444444 5555555 Sources/Conflict.swift
    """
    let unstaged = """
    7\t2\tSources/Foo.swift
    -\t-\tAssets/blob.png
    2\t2\tSources/Conflict.swift
    """
    let staged = """
    5\t1\tSources/Foo.swift
    1\t0\tSources/{Old Name => New Name}.swift
    3\t0\tSources/Added.swift
    0\t4\tSources/Gone.swift
    """

    let parsed = GitChanges.assemble(status: status, unstaged: unstaged, staged: staged)
    expect("the branch is parsed", parsed.branch, "main")
    expect("the object id is parsed", parsed.head, "d5c61e9f91c46a77")
    expect("ahead is parsed", parsed.ahead, 2)
    expect("behind is parsed", parsed.behind, 3)
    expect("every porcelain row is kept", parsed.files.count, 7)

    func file(_ path: String) -> GitChanges.File? {
        parsed.files.first { $0.path == path }
    }
    let partial = file("Sources/Foo.swift")
    expect("a partially staged file is staged", partial?.staged, true)
    expect("and is unstaged", partial?.unstaged, true)
    expect("its two addition counts are added", partial?.additions, 12)
    expect("and its two deletion counts are added", partial?.deletions, 3)

    let renamed = file("Sources/New Name.swift")
    expect("a rename keeps its destination", renamed?.kind, .renamed)
    expect("a rename keeps its source", renamed?.from, "Sources/Old Name.swift")
    expect("a braced numstat rename joins the destination", renamed?.additions, 1)
    expect("an index-only rename is staged", renamed?.staged, true)
    expect("an index-only rename is not unstaged", renamed?.unstaged, false)

    expect("an added row is added", file("Sources/Added.swift")?.kind, .added)
    expect("a deleted row is deleted", file("Sources/Gone.swift")?.kind, .deleted)
    let untracked = file("Notes/new file.txt")
    expect("an untracked path may contain spaces", untracked?.kind, .untracked)
    expect("untracked means a worktree change", untracked?.unstaged, true)
    expect("an untracked file has no invented count", untracked?.additions, nil)
    expect("an unmerged row is a conflict", file("Sources/Conflict.swift")?.kind, .conflict)
    expect("a binary addition count is null", file("Assets/blob.png")?.additions, nil)
    expect("and so is its deletion count", file("Assets/blob.png")?.deletions, nil)

    let payload = GitChanges.payload(parsed)
    let git = payload["git"] as? [String: Any]
    expect("a payload with files is not clean", git?["clean"] as? Bool, false)
    expect("the payload carries every file", (git?["files"] as? [[String: Any]])?.count, 7)
    let clean = GitChanges.payload(GitChanges.parseStatus(
        "# branch.oid abc\n# branch.head topic\n# branch.ab +0 -0\n"))
    expect("an empty status payload is clean",
           (clean["git"] as? [String: Any])?["clean"] as? Bool, true)
}

group("every word the page can draw is a word the page is sent") {
    // **This one is here because the same mistake happened twice.** A string gets added to `Copy`,
    // translated into fourteen languages, and then not listed in `strings(for:)` — and nothing
    // breaks, because the page carries an English copy of everything as a fallback. So the failure
    // is invisible to whoever made it and visible only to somebody reading one of the other
    // thirteen languages. The second time, the string left behind was the warning that sending
    // from a phone confirms the wrong menu option.
    //
    // Walking `Copy` rather than listing names, for the same reason the translation check walks
    // it: a list you have to remember to extend is the thing that failed.
    let payload = RemoteServer.shared.route(remoteRequest("GET", "/v1/strings"))
    let sent = ((try? JSONSerialization.jsonObject(with: payload.body)) as? [String: Any]) ?? [:]

    // Members the page is deliberately not given. Each one needs a reason written here, and
    // "the page does not use it yet" is not a reason — an unused string should not be in `Copy`.
    let notSent: Set<String> = []

    // Keys that are sent without being members, which is legitimate only when a *function* on
    // `Copy` supplies them: a question with two answers does not cross a JSON boundary as one.
    let derived: Set<String> = ["webOrderNewest", "webOrderOldest"]  // t.outputOrder(newestFirst:)

    var missing: [String] = []
    for child in Mirror(reflecting: English()).children {
        guard let label = child.label, label.hasPrefix("web"), child.value is String else { continue }
        if notSent.contains(label) { continue }
        if sent[label] == nil { missing.append(label) }
    }
    check("no web string is translated and then never sent", missing.isEmpty,
          "in Copy but not in /v1/strings: " + missing.sorted().joined(separator: ", "))

    // And the other direction, which is the cheaper mistake but still a mistake: a key the page
    // is sent that no longer exists on `Copy` is a key nothing can ever change again.
    let known = Set(Mirror(reflecting: English()).children.compactMap { $0.label })
    let orphans = sent.keys.filter {
        $0.hasPrefix("web") && !known.contains($0) && !derived.contains($0)
    }
    check("and nothing is sent under a name Copy does not have", orphans.isEmpty,
          "sent but not in Copy: " + orphans.sorted().joined(separator: ", "))
}

group("ending a session is the one route that destroys something") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    // **Every case here is a refusal.** There is no test of the path that works, because the
    // path that works closes a terminal tab, and a suite that occasionally ends somebody's
    // session is a suite people stop running. What is asserted is that nothing reaches
    // `Targets.end` without passing the same gate as every other write.
    func end(_ token: String?, key: Bool = true) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if key { headers["Idempotency-Key"] = UUID().uuidString }
        return RemoteServer.shared.route(
            remoteRequest("POST", "/v1/sessions/%nope%/end", headers: headers))
    }

    Config.shared.remoteWrite = true
    expect("no token is refused", end(nil).status, 401)
    expect("a device that may only read cannot end one", end(reader.token).status, 403)
    expect("and without an Idempotency-Key it is a bad request",
           end(writer.token, key: false).status, 400)

    Config.shared.remoteWrite = false
    expect("the write switch is checked first",
           remoteErrorCode(end(writer.token)), "write_disabled")

    Config.shared.remoteWrite = true
    expect("a session that is not there is a 404", end(writer.token).status, 404)
}

group("a numbered list somebody typed is not a menu") {
    // Taken from a real capture, 2026-08-19. `\u{276F}` is both the glyph a dialog marks its
    // current row with and the one Claude Code puts in front of the line you type — so a message
    // that opens with a numbered list echoes as exactly the shape of a menu whose first row is
    // selected. The phone then said a question was waiting and told the reader not to answer
    // from there, which left them no way to do anything.
    let typed = """
    \u{276F} 1. \u{4F60}\u{73FE}\u{5728}\u{662F}\u{5426}\u{8B93} web \u{7684}\u{8F38}\u{5165}bar
      2. \u{8F38}\u{5165} bar \u{53EF}\u{4EE5}\u{4E0D}\u{8981}\u{7B2C}\u{4E00}\u{500B}\u{5B57}
      3. \u{6211}\u{770B} web \u{7684}\u{8655}\u{7406}\u{72C0}\u{614B}
    """
    check("a list typed at the prompt is not a question", !SessionState.isChoosing(typed))
    check("the hook gate makes the same shape a question",
          SessionState.isChoosing(typed, hookWaiting: true))
    expect("a gated picker makes the screen waiting",
           SessionState.read(typed, hookWaiting: true), .waiting)
    guard let gated = SessionState.menu(typed, hookWaiting: true) else {
        check("the same shape is a menu after a hook says it is waiting", false); return
    }
    expect("the hook-gated menu keeps every option", gated.options.count, 3)
    expect("the flush-left caret selects its numbered row", gated.selected, 1)
    check("a later composer still disqualifies the echoed list",
          !SessionState.isChoosing(typed + "\n\u{276F} ", hookWaiting: true))

    // Real AskUserQuestion rows are separated by descriptions. Walking adjacent lines, like the
    // Codex parser does, stops at the first description; scanning every option row must not.
    let described = """
      1. Keep the current API
        This preserves existing callers.
    \u{276F} 2. Add the waiting gate
        This trusts the ambiguous caret only after a hook signal.
      3. Cancel
        Leave the code unchanged.
    """
    guard let describedMenu = SessionState.menu(described, hookWaiting: true) else {
        check("a described AskUserQuestion is a menu", false); return
    }
    expect("description lines do not stop option collection", describedMenu.options.count, 3)
    expect("described option numbers remain in screen order",
           describedMenu.options.map(\.number), [1, 2, 3])
    expect("the described selected row is correct", describedMenu.selected, 2)

    // And the thing it must still catch: a real dialog, drawn inside its box.
    let menu = """
    \u{2502} \u{276F} 1. Yes                  \u{2502}
    \u{2502}   2. Yes, and don't ask again  \u{2502}
    \u{2502}   3. No, tell Claude what to do\u{2502}
    """
    check("a dialog in its box still is", SessionState.isChoosing(menu))
    check("and remains one with the hook gate open",
          SessionState.isChoosing(menu, hookWaiting: true))

    // Indented but unboxed, which some prompts are.
    let bare = """
      \u{276F} 1. Keep going
        2. Stop here
    """
    check("and an indented one with no box", SessionState.isChoosing(bare))

    // The prompt on its own, which is every idle session on the machine.
    check("a bare prompt is not a menu", !SessionState.isChoosing("\u{276F} "))
}

group("a menu read as options a finger can hit") {
    // The shape a permission request actually arrives in: inside its box, one row carrying the
    // caret, the far wall of the dialog jammed against the longest label.
    let screen = """
    \u{2502} \u{276F} 1. Yes                          \u{2502}
    \u{2502}   2. Yes, and don't ask again           \u{2502}
    \u{2502}   3. No, tell Claude what to do instead \u{2502}
    """
    guard let menu = SessionState.menu(screen) else {
        check("a dialog is read as a menu", false); return
    }
    expect("every option is there", menu.options.count, 3)
    expect("the numbers are the ones printed", menu.options.map(\.number), [1, 2, 3])
    expect("the caret says which one is on screen", menu.selected, 1)

    // **The wall is not part of the answer.** Everything before the number is skipped on the way
    // in, and nothing used to be looking at the end — so the label arrived with a `\u{2502}` on it
    // and the phone drew a button with the side of a box in its name.
    expect("the box is not part of the label", menu.options[1].label, "Yes, and don't ask again")
    expect("nor of the last one",
           menu.options[2].label, "No, tell Claude what to do instead")

    // Every one of these is reachable by a keystroke, which is what makes it pressable.
    check("all three can be answered", menu.options.allSatisfy(\.answerable))

    // The two are the same reading. `isChoosing` exists because most callers only want the yes
    // or no, and a second parser would be a second thing to be wrong.
    check("and the old question is the same question", SessionState.isChoosing(screen))
}

group("the question above a visual menu") {
    // AskUserQuestion's descriptions belong to their options, not to the prose above the first
    // one. The short checkbox line is only a header, so the phone gets the two wrapped question
    // lines and not that classification label.
    let described = """
    ────────────────────────────────────────────
     ☐ build
     別的 session 把 index.html 寫完了，但還沒 commit。
     root 連坐的修正要生效就得 build。怎麼走？
    ❯ 1. 幫它整理並 commit，再 build（推薦）
         跟先前那批一樣，先整理工作區。
      2. 直接 build，不碰它的 commit
         保留目前的提交狀態。
      3. 先不要 build
    ────────────────────────────────────────────
    """
    guard let describedMenu = SessionState.menu(described, hookWaiting: true) else {
        check("a described question is still a menu", false); return
    }
    expect("wrapped question lines are joined",
           describedMenu.question,
           "別的 session 把 index.html 寫完了，但還沒 commit。 root 連坐的修正要生效就得 build。怎麼走？")
    expect("description rows do not become options", describedMenu.options.count, 3)

    // **As the terminal actually draws it.** The dialog puts a blank row between the header and
    // the question and another between the question and the first option. The fixture above has
    // none, because it was written as adjacent lines — and that difference was the whole bug: the
    // first blank read as the top of the prose, so a real dialog yielded no question at all while
    // this group stayed green. Padding is stepped over now, and this is the shape that proves it.
    let padded = """
    ────────────────────────────────────────────
     ☐ build

    │ 別的 session 把 index.html 寫完了，但還沒 commit。

    ❯ 1. 幫它整理並 commit，再 build（推薦）
         跟先前那批一樣，先整理工作區。
      2. 直接 build，不碰它的 commit
      3. 先不要 build
    ────────────────────────────────────────────
    """
    expect("padding inside the dialog is not the top of the question",
           SessionState.menu(padded, hookWaiting: true)?.question,
           "別的 session 把 index.html 寫完了，但還沒 commit。")

    // Prose that no edge closed off is not the question. Every dialog that has actually been
    // captured is framed; without a rule, a header or a caret above it there is nothing to say
    // where the question starts, and guessing put a page of someone's analysis on a phone.
    let unframed = """
    This belongs to the conversation, not to the dialog.


     Ship the tested build now?
    ❯ 1. Yes
      2. No
    """
    check("prose with no edge above it is not read as the question",
          SessionState.menu(unframed, hookWaiting: true)?.question == nil)

    // **Numbered rows above the frame belong to whoever wrote them.** An assistant listing three
    // findings, then a dialog with three options, was read as one menu of eight — and because the
    // first of those eight sat outside the dialog, the question was taken from the prose above
    // *that*. Both halves are asserted here: the count, and where the question came from.
    let listAbove = """
      三個順手挖到的東西

      1. 旗標是我們自己的 prompt 教出來的。
      2. zh_script.py 把「制」列為簡體字，那是錯的。
      3. 離線 dump 的 rewarm 通道灌的是同一段摘要。

      Opus 基準 20 題已完成 16 題。
    ────────────────────────────────────────────
    ←  ☐ 全跑範圍  ☐ digit 對等  ✔ Submit  →

    要怎麼跑完整那一輪？

    ❯ 1. 砍掉慢的五個，跑剩 17 題（推薦）
         淘汰最慢的兩個候選。
      2. 全部也跑完
      3. 先把題庫拉到 40 題再跑
    """
    guard let bounded = SessionState.menu(listAbove, hookWaiting: true) else {
        check("a dialog under a written list is still a menu", false); return
    }
    expect("rows above the frame are not options", bounded.options.count, 3)
    expect("and the question is the dialog's own", bounded.question, "要怎麼跑完整那一輪？")

    let oneLine = """
    ╭──────────────────────────╮
    │ Do you want to proceed?  │
    │ ❯ 1. Yes                 │
    │   2. No                  │
    ╰──────────────────────────╯
    """
    expect("a one-line permission question is read",
           SessionState.menu(oneLine)?.question, "Do you want to proceed?")

    let conversationAbove = """
    This sentence belongs to the earlier conversation.
    So does this one.
    ────────────────────────────────────────────
     ☐ deploy
     Ship the tested build now?
    ❯ 1. Ship it
      2. Keep testing
    """
    expect("a dialog rule keeps earlier conversation out",
           SessionState.menu(conversationAbove, hookWaiting: true)?.question,
           "Ship the tested build now?")

    let noQuestion = """
    │ ❯ 1. Yes │
    │   2. No  │
    """
    let fallback = SessionState.menu(noQuestion)
    check("a menu with no readable question keeps nil", fallback?.question == nil)
    expect("missing prose does not lose its options", fallback?.options.count, 2)
}

group("a menu row no keystroke can reach is shown and not offered") {
    // Ten options is not a shape Claude Code draws today, and the failure if it ever does must
    // not be a button that answers a different question: `Targets.answer` carries 1...9, so the
    // tenth row is drawn and refused rather than quietly renumbered.
    var rows = "\u{2502} \u{276F} 1. first  \u{2502}\n"
    for n in 2...10 { rows += "\u{2502}   \(n). option \(n) \u{2502}\n" }
    guard let menu = SessionState.menu(rows, tailLines: 40) else {
        check("ten options is still a menu", false); return
    }
    expect("all ten are read", menu.options.count, 10)
    check("the first nine can be answered",
          menu.options.prefix(9).allSatisfy(\.answerable))
    check("and the tenth cannot", !menu.options[9].answerable)
}

group("what a background agent left on disk") {
    // The one record that says an agent ended. There is nothing that says one *started* — see
    // `Subagents` — so this is the whole of how "still running" is decided: by its absence.
    let folder = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-agents-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }

    let note = "<task-notification>\\n<task-id>a42cc4cf998a3ae33</task-id>\\n"
        + "<status>completed</status>\\n<summary>Agent \\\"Probe\\\" finished</summary>\\n"
        + "<result>52 files, and the three markers ran in order.</result>\\n"
        + "<usage><subagent_tokens>23771</subagent_tokens><tool_uses>7</tool_uses>"
        + "<duration_ms>79946</duration_ms></usage>\\n</task-notification>"

    let transcript = folder.appendingPathComponent("session.jsonl")
    let lines = [
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"off it goes"}]}}"#,
        "{\"type\":\"queue-operation\",\"operation\":\"enqueue\",\"content\":\"\(note)\"}",
    ].joined(separator: "\n") + "\n"
    try? lines.write(to: transcript, atomically: true, encoding: .utf8)

    let found = Subagents.notices(in: transcript)
    expect("the ending is found", found["a42cc4cf998a3ae33"]?.status, "completed")
    expect("with what it came back with",
           found["a42cc4cf998a3ae33"]?.result, "52 files, and the three markers ran in order.")
    expect("and what it cost", found["a42cc4cf998a3ae33"]?.tokens, 23771)
    expect("in tool calls", found["a42cc4cf998a3ae33"]?.tools, 7)

    // **Read forward, not from the end**, so a second look does not re-read the file and does
    // not lose what the first one learned. Appending a line nothing cares about must leave the
    // verdict exactly where it was.
    if let handle = try? FileHandle(forWritingTo: transcript) {
        _ = try? handle.seekToEnd()
        try? handle.write(contentsOf: Data(#"{"type":"user","message":{"role":"user","content":"ok"}}"# .utf8 + Data("\n".utf8)))
        try? handle.close()
    }
    let again = Subagents.notices(in: transcript)
    expect("a later read keeps what the first one found", again["a42cc4cf998a3ae33"]?.status,
           "completed")

    // A transcript with no endings in it leaves every agent it mentions running, which is the
    // state this is all in aid of.
    let quiet = folder.appendingPathComponent("quiet.jsonl")
    try? #"{"type":"user","message":{"role":"user","content":"hello"}}"# .write(to: quiet,
                                                                               atomically: true,
                                                                               encoding: .utf8)
    check("a transcript with no endings has no verdicts", Subagents.notices(in: quiet).isEmpty)

    // And the file that is not there at all, which is the ordinary state for a session that has
    // never sent anything off.
    check("a missing transcript is not an error",
          Subagents.notices(in: folder.appendingPathComponent("nope.jsonl")).isEmpty)
}

group("what a background agent is doing right now") {
    let folder = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-doing-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }

    // The **last** tool it reached for, not the first: this is the closest thing to a live line
    // that something without a screen has.
    let jsonl = folder.appendingPathComponent("agent-a1.jsonl")
    let rows = [
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"Sources/Panel.swift"}}]}}"#,
        #"{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"…"}]}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"swift build","description":"Build"}}]}}"#,
    ].joined(separator: "\n") + "\n"
    try? rows.write(to: jsonl, atomically: true, encoding: .utf8)

    expect("the newest tool call wins", Subagents.doing(in: jsonl), "Bash: swift build")

    // A transcript that has not reached for anything yet says nothing rather than guessing.
    let fresh = folder.appendingPathComponent("agent-a2.jsonl")
    try? #"{"type":"user","message":{"role":"user","content":"go"}}"# .write(to: fresh,
                                                                            atomically: true,
                                                                            encoding: .utf8)
    check("an agent that has not used a tool says nothing", Subagents.doing(in: fresh) == nil)
}

group("following an agent to its own transcript") {
    // The id in `/v1/sessions/<id>/agents/<agentId>` and in the pane's own tabs is about to
    // become a path component, so it is checked rather than trusted. Everything Claude Code
    // writes is hex; everything that could leave the directory is not.
    check("an ordinary id is one", Subagents.isID("a44b12139eff09dd4"))
    check("and so is a dashed one", Subagents.isID("a42cc4cf-998a-3ae3"))
    check("nothing is not", !Subagents.isID(""))
    check("a parent directory is refused", !Subagents.isID(".."))
    check("a path is refused", !Subagents.isID("../../../etc/passwd"))
    check("a dot is enough to refuse", !Subagents.isID("agent.jsonl"))
    check("a separator is refused", !Subagents.isID("a1/a2"))
    check("and a name nobody could have written is refused",
          !Subagents.isID(String(repeating: "a", count: 200)))

    // **Every row in an agent's file is marked as a sidechain** — that is what an agent is, from
    // the session's point of view — and a sidechain is exactly what the session's own transcript
    // drops. Read with the session's rule, a busy agent reads as one that has written nothing.
    let row = #"{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"had a look, nothing in the logs"}]}}"#
    expect("a session's transcript drops sidechains",
           Transcript.parse(row, assistant: .claude).count, 0)
    let asAgent = Transcript.parse(row, assistant: .claude, sidechains: true)
    expect("an agent's own transcript is nothing else", asAgent.count, 1)
    expect("and it reads as what the agent said",
           asAgent.first?.text, "had a look, nothing in the logs")
}

// MARK: - Codex

// Every screen in this section is a real capture off a real Codex TUI, not a sketch of one.
// The point of these tests is that the shapes are what was actually drawn — a fixture somebody
// wrote from memory would agree with the parser and with nothing else.

group("Codex's live line is the one with a clock in it") {
    let working = """
    • Running sleep 25 now.

    • Working (10s • esc to interrupt) · 1 background terminal running · /ps to view · /stop to close

    › Ask Codex to do anything

      gpt-5.6-sol default · ~/code/clawdline
    """
    expect("the working line is found and cut at the bracket",
           Activity.codex(working), "Working (10s • esc to interrupt)")

    // Codex prefixes everything it says with the same bullet, so the glyph proves nothing.
    let quiet = """
    • Ran printf '%s\\n' hello > notes.txt
      └ (no output)

    • Created notes.txt containing hello.

    › Ask Codex to do anything
    """
    check("a sentence with the same bullet is not a live line", Activity.codex(quiet) == nil)

    expect("an approval being reviewed is still work",
           Activity.codex("• Reviewing approval request (3s • esc to interrupt)"),
           "Reviewing approval request (3s • esc to interrupt)")

    // The two live lines are not interchangeable, and reading one with the other's parser is
    // how a session comes back idle while it is plainly busy.
    check("Claude Code's parser does not read Codex's line",
          Activity.parse(working) == nil)
    check("and Codex's does not read Claude Code's",
          Activity.codex("✳ Generating… (21s · thinking)") == nil)
    expect("each read by the right one",
           Activity.parse("✳ Generating… (21s · thinking)", assistant: .claude),
           "Generating… (21s · thinking)")
}

group("Codex's dialogs are read by what is under them, not by the margin") {
    // The trust dialog, captured on a first run in an untrusted directory. Note the caret in
    // column zero — exactly where Codex also draws the composer's.
    let trust = """
    › Ask Codex to do anything

      ? for shortcuts
    > You are in /private/tmp/scratch/probe

      Do you trust the contents of this directory? Working with untrusted contents comes with
      injection. Trusting the directory allows project-local config, hooks, and exec policies.

    › 1. Yes, continue
      2. No, quit

      Press enter to continue
    """
    let menu = SessionState.menu(trust, assistant: .codex)
    expect("both options are read", menu?.options.count, 2)
    expect("the caret's row is the selection", menu?.selected, 1)
    expect("and the words come with it", menu?.options.first?.label, "Yes, continue")
    check("so the session reads as waiting",
          SessionState.read(trust, assistant: .codex) == .waiting)

    // The model picker, where the caret is on the first of six and the question sits above it.
    let models = """
      Select Model and Effort
      Access legacy models by running codex -m <model_name> or in your config.toml

    › 1. gpt-5.6-sol (current)  Latest frontier agentic coding model.
      2. gpt-5.6-terra          Balanced agentic coding model for everyday work.
      3. gpt-5.6-luna           Fast and affordable agentic coding model.

      Press enter to confirm or esc to go back
    """
    expect("a longer picker is read the same way",
           SessionState.menu(models, assistant: .codex)?.options.count, 3)

    // **The one that matters.** A message that begins with a numbered list echoes with the same
    // caret in the same column. What tells them apart is that the composer is still underneath
    // it — a dialog takes the composer away.
    let echoed = """
    › 1. rename the field
      2. update the callers
      3. run the tests

    • Working (4s • esc to interrupt)

    › Ask Codex to do anything

      gpt-5.6-sol default · ~/code/clawdline
    """
    check("a numbered list somebody sent is not a dialog",
          SessionState.menu(echoed, assistant: .codex) == nil)
    check("and the session reads as working, which is what it is",
          SessionState.read(echoed, assistant: .codex) == .working("Working (4s • esc to interrupt)"))

    // Arrowing down moves the caret off the first row; the rows above it are still the menu.
    let moved = """
      Select Model and Effort

      1. gpt-5.6-sol
    › 2. gpt-5.6-terra
      3. gpt-5.6-luna
    """
    let picked = SessionState.menu(moved, assistant: .codex)
    expect("the rows above the caret are still options", picked?.options.count, 3)
    expect("and the caret says which is selected", picked?.selected, 2)

    // An idle screen is not a menu, however many carets are on it.
    let idle = """
    › Run the shell command: sleep 25

    • Ran sleep 25

    › Ask Codex to do anything
    """
    check("an idle Codex session is idle", SessionState.read(idle, assistant: .codex) == .idle)
}

group("a rollout says where its session is working") {
    let head = """
    {"timestamp":"2026-08-23T15:30:26.612Z","ordinal":0,"type":"session_meta","payload":\
    {"session_id":"01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8","cwd":"/Users/me/code/thing",\
    "originator":"codex-tui","cli_version":"0.149.0"}}
    """
    expect("the working directory comes out", Codex.head(inText: head)?.cwd, "/Users/me/code/thing")
    expect("and the session's own id", Codex.head(inText: head)?.id,
           "01a02f3e-8f2c-7011-bb6d-49d2aaabd2a8")
    check("a line with no cwd is not a head", Codex.head(inText: "{\"type\":\"event_msg\"}") == nil)

    // A Codex process writes more than one rollout: its own conversation, and one per subagent
    // it sends off. Same directory, same minute, same everything — except this.
    let mine = """
    {"type":"session_meta","payload":{"session_id":"a","cwd":"/w","originator":"codex-tui",\
    "source":"cli","thread_source":"user"}}
    """
    let theirs = """
    {"type":"session_meta","payload":{"session_id":"b","cwd":"/w","originator":"codex-tui",\
    "source":{"subagent":{"other":"guardian"}},"thread_source":"subagent"}}
    """
    check("a person's thread says so", Codex.head(inText: mine)?.isUser == true)
    check("and a subagent's says otherwise", Codex.head(inText: theirs)?.isUser == false)
    check("a terminal rollout is an interactive CLI session",
          Codex.head(inText: mine)?.isInteractiveCLI == true)
    let desktop = """
    {"type":"session_meta","payload":{"session_id":"c","cwd":"/Users/me/Documents/Codex/new-chat",\
    "originator":"codex_work_desktop","source":"vscode","thread_source":"user"}}
    """
    check("a Desktop conversation is a person but not a CLI project",
          Codex.head(inText: desktop)?.isUser == true
            && Codex.head(inText: desktop)?.isInteractiveCLI == false)
    // The field is newer than the format. Absent means nobody wrote it down, which is not the
    // same as "this is a subagent" — and reading it that way would empty the pane for anyone
    // whose sessions predate it.
    check("a rollout that does not say counts as a person's",
          Codex.head(inText: head)?.isUser == true)
    check("and an old rollout remains a CLI session",
          Codex.head(inText: head)?.isInteractiveCLI == true)
}

group("the rollout a Codex process is holding open") {
    // What `lsof -p` actually answers with: a hundred files, nearly all of them SQLite.
    let open = [
        "/Users/me/.codex/state_5.sqlite",
        "/Users/me/.codex/thread-writer-locks/01a02f68-ccd4.lock",
        "/Users/me/code/thing/.git/index",
        "/Users/me/.codex/sessions/2026/08/24/rollout-2026-08-24T00-16-32-01a02f68-ccd4.jsonl",
    ]
    check("a rollout is picked out of it by shape", Codex.isRollout(open[3]))
    check("a lock file beside it is not one", !Codex.isRollout(open[1]))
    check("nor is a jsonl somewhere else",
          !Codex.isRollout("/Users/me/.claude/projects/-Users-me/rollout-x.jsonl"))
    check("and the name has to be a rollout's",
          !Codex.isRollout("/Users/me/.codex/sessions/2026/08/24/notes.jsonl"))
}

group("a new Codex process never borrows an existing conversation") {
    let fm = FileManager.default
    let base = fm.temporaryDirectory.appendingPathComponent("clawdline-codex-\(UUID().uuidString)")
    let day = base.appendingPathComponent("sessions/2026/08/24", isDirectory: true)
    let file = day.appendingPathComponent("rollout-2026-08-24T00-16-32-existing.jsonl")
    let oldHome = Config.shared.codexHome
    defer {
        Config.shared.codexHome = oldHome
        try? fm.removeItem(at: base)
    }
    try! fm.createDirectory(at: day, withIntermediateDirectories: true)
    try! Data("""
    {"type":"session_meta","payload":{"session_id":"existing","cwd":"/w",\
    "originator":"codex-tui","thread_source":"user"}}
    """.utf8).write(to: file)
    Config.shared.codexHome = base.path

    check("the clock remains a fallback when there is no process to ask",
          Codex.locate(cwd: "/w", startedAt: Date(), days: 3) == file)
    check("a known process with no rollout returns nothing instead of the fallback",
          Codex.locate(cwd: "/w", startedAt: Date(),
                       pid: Int32(ProcessInfo.processInfo.processIdentifier), days: 3) == nil)
}



group("a rollout reads as the same entries a transcript does") {
    func line(_ item: String) -> String {
        "{\"timestamp\":\"2026-08-23T15:43:06.283Z\",\"type\":\"event_msg\",\"payload\":"
            + "{\"type\":\"item_completed\",\"item\":" + item + "}}"
    }
    let rollout = [
        line("{\"type\":\"UserMessage\",\"content\":[{\"type\":\"text\",\"text\":\"fix the tests\"}]}"),
        line("{\"type\":\"Reasoning\",\"summary_text\":[],\"raw_content\":[]}"),
        line("{\"type\":\"AgentMessage\",\"phase\":\"commentary\","
             + "\"content\":[{\"type\":\"Text\",\"text\":\"Looking now.\"}]}"),
        line("{\"type\":\"CommandExecution\",\"command\":[\"/bin/zsh\",\"-lc\",\"./test.sh\"],"
             + "\"aggregated_output\":\"1281 checks passed\\nfine\",\"exit_code\":0}"),
        line("{\"type\":\"FileChange\",\"changes\":{\"/Users/me/a/Thing.swift\":{\"type\":\"update\"}}}"),
        // Not an item_completed at all — the same conversation, as it went to the model.
        "{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"developer\","
            + "\"content\":[{\"type\":\"input_text\",\"text\":\"<skills_instructions>…\"}]}}",
    ].joined(separator: "\n")

    let entries = Codex.parse(rollout)
    expect("the machinery is left out and the conversation is not", entries.count, 5)
    expect("the person spoke first", entries[0].kind, Transcript.Entry.Kind.user)
    expect("and what they said", entries[0].text, "fix the tests")
    expect("thinking with nothing in it is not a row",
           entries.filter { $0.text.contains("Reasoning") }.count, 0)
    expect("the assistant answered", entries[1].kind, Transcript.Entry.Kind.assistant)
    expect("a command is a tool call", entries[2].tool, "shell")
    expect("with the login shell taken off the front", entries[2].text, "./test.sh")
    expect("and its first line of output under it", entries[3].kind,
           Transcript.Entry.Kind.toolResult)
    expect("which is the first line and not all of it", entries[3].text, "1281 checks passed")
    expect("an edit names the file", entries[4].text, "Thing.swift")
    expect("the timestamp is read", entries[0].time?.timeIntervalSince1970,
           1787499786.283)

    expect("a truncated line is skipped rather than fatal", Codex.parse("{\"type\":").count, 0)
    expect("and so is an item nobody has taught this about",
           Codex.parse(line("{\"type\":\"SomethingNew\",\"content\":\"…\"}")).count, 0)
}

group("a Codex session can be named from its first request") {
    func completed(_ item: String) -> String {
        "{\"type\":\"event_msg\",\"payload\":{\"type\":\"item_completed\",\"item\":"
            + item + "}}"
    }
    let rollout = [
        completed("{\"type\":\"AgentMessage\",\"content\":[{\"text\":\"Hello\"}]}"),
        completed("{\"type\":\"UserMessage\",\"content\":[{\"text\":\"修正登入逾時問題\"}]}"),
        completed("{\"type\":\"UserMessage\",\"content\":[{\"text\":\"and add tests\"}]}"),
    ].joined(separator: "\n")
    expect("the first human request is selected",
           Codex.firstUserMessage(in: rollout), "修正登入逾時問題")

    let prompt = CodexNaming.prompt(for: "修正登入逾時問題")
    check("the request is marked as untrusted data", prompt.contains("untrusted data"))
    check("the title turn is explicitly told not to use tools", prompt.contains("do not use tools"))
    check("the request is delimited", prompt.contains("<request>\n修正登入逾時問題\n</request>"))
    expect("long requests are bounded",
           CodexNaming.prompt(for: String(repeating: "x", count: 5_000), limit: 10)
                .components(separatedBy: "<request>\n").last?
                .components(separatedBy: "\n</request>").first,
           String(repeating: "x", count: 10))

    expect("markdown and a label are removed from a model title",
           CodexNaming.cleanTitle("# Title:  Fix login timeout.\nExtra explanation"),
           "Fix login timeout")
    expect("Chinese wrappers and punctuation are removed",
           CodexNaming.cleanTitle("「修正登入逾時問題。」"), "修正登入逾時問題")
    expect("a quoted title is unwrapped",
           CodexNaming.cleanTitle("“Codex Session 自動命名”"), "Codex Session 自動命名")
    check("an empty answer is refused", CodexNaming.cleanTitle("\n  \n") == nil)

    let named: [String: Any] = ["result": ["thread": ["name": " Bug bash "]]]
    let unnamed: [String: Any] = ["result": ["thread": ["name": NSNull()]]]
    expect("a persisted thread name is recognised", CodexNaming.threadName(in: named), "Bug bash")
    check("a null persisted name is unnamed", CodexNaming.threadName(in: unnamed) == nil)
    expect("a persisted Codex name replaces the terminal tab label",
           CodexNaming.displayLabel(threadName: "修正登入逾時", terminalLabel: "clawdline"),
           "修正登入逾時")
    expect("an absent Codex name keeps the terminal tab label",
           CodexNaming.displayLabel(threadName: nil, terminalLabel: "clawdline"), "clawdline")
}

group("the fields of a rollout, one at a time") {
    expect("a login shell is unwrapped",
           Codex.command(["/bin/zsh", "-lc", "ls -la"]), "ls -la")
    expect("bash counts too", Codex.command(["/bin/bash", "-c", "make"]), "make")
    expect("anything else is left as it was",
           Codex.command(["git", "status", "--short"]), "git status --short")
    expect("a failure with no output says so",
           Codex.outcome(of: ["exit_code": 3, "aggregated_output": ""]), "exit 3")
    expect("and a success with no output says nothing",
           Codex.outcome(of: ["exit_code": 0, "aggregated_output": ""]), "")
    expect("an MCP call shows the title its plugin wrote",
           Codex.arguments(["title": "open the run page", "code": "await browser.open()"]),
           "open the run page")
    expect("four files are three and a count",
           Codex.changed(["/a/one.swift": 1, "/b/two.swift": 1, "/c/three.swift": 1,
                          "/d/four.swift": 1]),
           "one.swift, two.swift, three.swift +1")
}

group("which assistant a process name stands for") {
    expect("a bare name", Assistant.named("codex"), .codex)
    expect("a path to one", Assistant.named("/opt/homebrew/bin/claude"), .claude)
    check("a longer name is somebody else's program", Assistant.named("codexctl") == nil)
    check("and so is a directory that merely contains one", Assistant.named("/Users/me/.codex") == nil)
    expect("each leaves on its own word", Assistant.claude.quitLine, "/exit")
    expect("and they are not the same word", Assistant.codex.quitLine, "/quit")
}

group("assistant product marks load at row size") {
    for assistant in Assistant.allCases {
        let image = assistant.logoImage(height: 11)
        check("\(assistant.rawValue) has a vector mark", image != nil)
        expectClose("\(assistant.rawValue) mark is square", image?.size.width ?? 0, 11)
        expectClose("\(assistant.rawValue) mark has the requested height", image?.size.height ?? 0, 11)
    }
}

group("the line a new tab is given names the assistant") {
    expect("Claude Code, as it always was",
           StartPoints.itermLine(cwd: "/Users/me/code/thing"),
           "cd '/Users/me/code/thing' && claude")
    expect("Codex, by the same route",
           StartPoints.itermLine(cwd: "/Users/me/code/thing", assistant: .codex),
           "cd '/Users/me/code/thing' && codex")
    // The quoting is the same quoting, which is the point of it being one function.
    expect("and a directory with a quote in it survives",
           StartPoints.itermLine(cwd: "/Users/me/it's", assistant: .codex),
           "cd '/Users/me/it'\\''s' && codex")
}

// MARK: - Handing work to another session

/// A lowercase UUID, which is the only shape a task id is ever allowed to have.
let taskID = "0f8fad5b-d9cb-469f-a165-70867728950e"

group("a task.json is read before a terminal is opened for it") {
    // Everything here is the file a *root* session wrote, which is to say a file this app did not
    // write and cannot trust. `draft` is the whole of the reading, and it is pure — the directory
    // check is handed in — so the refusals can be exercised without a filesystem to arrange.
    func file(_ overrides: [String: Any] = [:]) -> [String: Any] {
        var obj: [String: Any] = ["clawdline_protocol": 1,
                                  "task_id": taskID,
                                  "kind": "image",
                                  "assistant": "codex",
                                  "project_dir": "/Users/me/code/thing",
                                  "title": "draw the project",
                                  "instructions": "Draw it, in ink.",
                                  "timeout_minutes": 45,
                                  "root": ["session_id": "abc", "label": "notebook"]]
        for (key, value) in overrides { obj[key] = value }
        return obj
    }
    func read(_ obj: [String: Any], expecting: String = taskID) -> Orchestrator.DraftOutcome {
        Orchestrator.draft(from: obj, expecting: expecting, isDirectory: { _ in true })
    }
    func made(_ obj: [String: Any]) -> Orchestrator.Draft? {
        if case .ok(let draft) = read(obj) { return draft }
        return nil
    }
    func refused(_ obj: [String: Any], expecting: String = taskID) -> Bool {
        if case .bad = read(obj, expecting: expecting) { return true }
        return false
    }

    expect("a whole task is taken as written", made(file())?.assistant, .codex)
    expect("with its own timeout", made(file())?.timeoutMinutes, 45)
    expect("its kind", made(file())?.kind, "image")
    expect("its title", made(file())?.title, "draw the project")
    expect("and who asked for it", made(file())?.rootSessionId, "abc")
    expect("a file with no kind is a custom one", made(file(["kind": ""]))?.kind, "custom")
    expect("and one with no timeout gets the default",
           made(file(["timeout_minutes": NSNull()]))?.timeoutMinutes, 30)
    expect("a title longer than the field is cut, not refused",
           made(file(["title": String(repeating: "t", count: 400)]))?.title.count, 200)

    // The refusals, one reason at a time.
    check("a protocol nobody has written yet", refused(file(["clawdline_protocol": 2])))
    check("and a missing one", refused(file(["clawdline_protocol": NSNull()])))
    check("an assistant this app cannot start", refused(file(["assistant": "emacs"])))
    check("a task_id that is a path", refused(file(["task_id": "../../etc/passwd"]),
                                             expecting: "../../etc/passwd"))
    check("a task_id with a separator in it, at the right length",
          refused(file(["task_id": "0f8fad5b-d9cb-469f-a165-7086772895/e"]),
                  expecting: "0f8fad5b-d9cb-469f-a165-7086772895/e"))
    check("a task_id that does not match the dispatch",
          refused(file(["task_id": "11111111-2222-3333-4444-555555555555"])))
    check("instructions nobody wrote", refused(file(["instructions": ""])))
    check("instructions past 16 KiB",
          refused(file(["instructions": String(repeating: "x", count: 16_385)])))
    check("a project_dir that is not a directory",
          {
              if case .bad = Orchestrator.draft(from: file(), expecting: taskID,
                                                isDirectory: { _ in false }) { return true }
              return false
          }())
    check("a project_dir that is not a path at all", refused(file(["project_dir": "thing"])))
    check("a timeout past four hours", refused(file(["timeout_minutes": 999])))
    check("and one of zero minutes", refused(file(["timeout_minutes": 0])))
}

group("a task id is the name of a directory, so it may not be a path") {
    check("a lowercase UUID is one", Orchestrator.isTaskID(taskID))
    check("in upper case it is not", !Orchestrator.isTaskID(taskID.uppercased()))
    check("nor is a walk upwards", !Orchestrator.isTaskID("../../etc/passwd"))
    check("nor is one with a slash at the right length",
          !Orchestrator.isTaskID("0f8fad5b-d9cb-469f-a165-7086772895/e"))
    check("nor a letter that is not hex",
          !Orchestrator.isTaskID("0f8fad5b-d9cb-469f-a165-70867728950g"))
    check("nor one character too few", !Orchestrator.isTaskID(String(taskID.dropLast())))
    check("nor nothing at all", !Orchestrator.isTaskID(""))
}

group("a task secret is kept as a hash and compared as one") {
    let secret = String(repeating: "a1", count: 32)
    let stored = Orchestrator.hash(ofSecret: secret)
    expect("the stored form is a SHA-256 in hex", stored.count, 64)
    check("and it is not the secret", stored != secret)
    expect("the same secret hashes the same way twice",
           Orchestrator.hash(ofSecret: secret), stored)
    check("a different one does not",
          Orchestrator.hash(ofSecret: String(repeating: "b2", count: 32)) != stored)
    check("the child's secret verifies against what was kept",
          RemoteAuth.constantTimeEquals(stored, Orchestrator.hash(ofSecret: secret)))
    check("and somebody else's does not",
          !RemoteAuth.constantTimeEquals(stored,
                                         Orchestrator.hash(ofSecret: secret + "0")))

    // The other credential: the one that says a local process asked, which is a different claim
    // from "this device is paired" and is deliberately not the same string.
    check("no dispatch token is not the dispatch token", !Orchestrator.verifyDispatch(token: nil))
    check("nor is an empty one", !Orchestrator.verifyDispatch(token: ""))
    check("nor is a guess", !Orchestrator.verifyDispatch(token: String(repeating: "0", count: 44)))
    check("the minted one is", Orchestrator.verifyDispatch(token: Orchestrator.dispatchToken()))
}

group("what a child's turn cost, at the prices this app knows") {
    expect("Opus, in", Orchestrator.price(forModel: "claude-opus-5-20260201")?.input, 5)
    expect("Opus, out", Orchestrator.price(forModel: "claude-opus-5-20260201")?.output, 25)
    expect("Sonnet, in", Orchestrator.price(forModel: "claude-sonnet-4-5")?.input, 3)
    expect("Fable, out", Orchestrator.price(forModel: "claude-fable-5")?.output, 50)
    expect("Haiku, in", Orchestrator.price(forModel: "claude-haiku-4-5")?.input, 1)
    // Codex bills against a plan rather than per token, so there is no honest number to give.
    check("a model nobody has a price for", Orchestrator.price(forModel: "gpt-5.6-luna") == nil)
    check("and no model at all", Orchestrator.price(forModel: nil) == nil)

    func opus(input: Int = 0, output: Int = 0, cacheRead: Int = 0,
              cacheWrite: Int = 0) -> Orchestrator.Usage {
        var usage = Orchestrator.Usage()
        usage.model = "claude-opus-5-20260201"
        usage.input = input
        usage.output = output
        usage.cacheRead = cacheRead
        usage.cacheWrite = cacheWrite
        return usage
    }
    expect("a million input tokens is the input price", Orchestrator.cost(of: opus(input: 1_000_000)), 5)
    expect("a million output tokens is the output price",
           Orchestrator.cost(of: opus(output: 1_000_000)), 25)
    expect("a cache read is a tenth of an input token",
           Orchestrator.cost(of: opus(cacheRead: 1_000_000)), 0.5)
    expect("a cache write is an input token and a quarter",
           Orchestrator.cost(of: opus(cacheWrite: 1_000_000)), 6.25)
    expect("and the four are added up",
           Orchestrator.cost(of: opus(input: 1_000_000, output: 1_000_000,
                                      cacheRead: 1_000_000, cacheWrite: 1_000_000)),
           36.75)
    expect("the answer is money, so it stops at four places",
           Orchestrator.cost(of: opus(input: 100)), 0.0005)
    expect("and a single token rounds away rather than inventing a digit",
           Orchestrator.cost(of: opus(input: 1)), 0)
    var unpriced = Orchestrator.Usage()
    unpriced.model = "gpt-5.6-luna"
    unpriced.input = 1_000_000
    check("tokens nobody has a price for cost nothing that can be said",
          Orchestrator.cost(of: unpriced) == nil)
}

group("the one line a child is given") {
    let secret = String(repeating: "c3", count: 32)
    let line = Orchestrator.firstLine(id: taskID, secret: secret)
    check("carries the secret, because nothing else ever will",
          line.contains("TASK_SECRET=" + secret))
    check("and names the file that says what to do",
          line.contains("/tmp/.clawdline/\(taskID)/CHILD.md"))
    check("and it is one line, because it is typed into a prompt", !line.contains("\n"))
}

group("dispatching is the one thing a paired device may not do") {
    // The whole point of the second credential. A phone with `send` can already type into a
    // session; opening a *new* one from a task file somebody else wrote is a different power, and
    // it is behind a `0600` file no page can read.
    Orchestrator.forget()
    defer { Orchestrator.forget() }
    let phone = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: phone.id) }

    let body = "{\"task_id\":\"\(taskID)\",\"secret\":\"\(String(repeating: "a1", count: 32))\"}"
    let anonymous = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/tasks", body: body))
    expect("nothing at all is turned away at the door", anonymous.status, 401)
    expect("and says so", remoteErrorCode(anonymous), "unauthorized")

    let paired = RemoteServer.shared.route(
        remoteRequest("POST", "/v1/orchestrator/tasks",
                      headers: ["Authorization": "Bearer \(phone.token)",
                                "Idempotency-Key": UUID().uuidString],
                      body: body))
    expect("a paired device gets in the door and no further", paired.status, 403)
    expect("and it is a refusal about the credential, not the task",
           remoteErrorCode(paired), "forbidden")

    let reading = RemoteServer.shared.route(remoteRequest("GET", "/v1/orchestrator/tasks"))
    expect("reading the list needs a credential too", reading.status, 401)
    expect("and it is the ordinary one", remoteErrorCode(reading), "unauthorized")
}

group("finishing a task takes that task's secret and nothing else") {
    // The completion route is the one place a *child* speaks, and a child was never given a
    // device token — so it is exempt from the door and gated on the secret alone. The task is put
    // into the store rather than dispatched, because dispatching opens a terminal tab.
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let secret = String(repeating: "a1", count: 32)
    let row: [String: Any] = ["id": taskID, "state": "briefed", "kind": "custom",
                              "title": "a task", "assistant": "codex", "project_dir": "/tmp",
                              "timeout_minutes": 30, "created": Date().timeIntervalSince1970,
                              "secret_hash": Orchestrator.hash(ofSecret: secret),
                              "artifacts": []]
    let stored = (try? JSONSerialization.data(withJSONObject: ["version": 1, "tasks": [row]])) ?? Data()
    try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try? stored.write(to: store, options: .atomic)

    func finish(_ id: String, secret: String?) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let secret { headers["X-Clawdline-Task-Secret"] = secret }
        return RemoteServer.shared.route(
            remoteRequest("POST", "/v1/orchestrator/tasks/\(id)/complete", headers: headers,
                          body: "{\"status\":\"success\",\"summary\":\"drew it\"}"))
    }

    let wrong = finish(taskID, secret: String(repeating: "b2", count: 32))
    expect("another task's secret is not this task's", wrong.status, 403)
    expect("and it is a plain refusal", remoteErrorCode(wrong), "forbidden")
    let silent = finish(taskID, secret: nil)
    expect("no secret at all is the same refusal", silent.status, 403)
    let unknown = finish("11111111-2222-3333-4444-555555555555", secret: secret)
    expect("a task nobody registered is a 404", unknown.status, 404)
    expect("and says nothing else about it", remoteErrorCode(unknown), "not_found")
}

group("closing a root session takes the work it dispatched with it") {
    // The cascade behind `POST /v1/sessions/:id/end`. What is asserted here is the *selection* —
    // which tasks a closing root takes down — because the acting half ends terminal tabs, and a
    // test that opened tabs to close them would be a test nobody dares run twice.
    Orchestrator.forget()
    let store = Orchestrator.storeURL
    let before = try? Data(contentsOf: store)
    defer {
        if let before {
            try? before.write(to: store, options: .atomic)
        } else {
            try? FileManager.default.removeItem(at: store)
        }
        Orchestrator.forget()
    }
    let root = "8967a1ee-9718-45ed-94d5-c81178870072"
    let stranger = "1c9a4d55-6f31-4b02-8d77-0a2e3c4b5d61"
    let born = Date().timeIntervalSince1970
    func row(_ id: String, _ state: String, rootSession: String?, at: Double,
             child: String? = nil) -> [String: Any] {
        var out: [String: Any] = ["id": id, "state": state, "kind": "custom", "title": "a task",
                                  "assistant": "claude", "project_dir": "/tmp",
                                  "timeout_minutes": 30, "created": at,
                                  "secret_hash": Orchestrator.hash(ofSecret: String(repeating: "a1", count: 32)),
                                  "artifacts": []]
        if let rootSession { out["root_session"] = rootSession }
        if let child { out["child_terminal"] = child }
        return out
    }
    let live = "0f8fad5b-d9cb-469f-a165-70867728950e"
    let alsoLive = "22222222-3333-4444-5555-666666666666"
    let done = "33333333-4444-5555-6666-777777777777"
    let elsewhere = "44444444-5555-6666-7777-888888888888"
    let orphan = "55555555-6666-7777-8888-999999999999"
    let alsoDone = "66666666-7777-8888-9999-aaaaaaaaaaaa"
    let noTab = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
    let rows: [[String: Any]] = [
        row(alsoLive, "queued", rootSession: root, at: born + 1),
        row(live, "briefed", rootSession: root, at: born),
        row(done, "success", rootSession: root, at: born + 2, child: "%tab-done%"),
        row(elsewhere, "briefed", rootSession: stranger, at: born + 3),
        row(orphan, "briefed", rootSession: nil, at: born + 4),
        row(alsoDone, "failure", rootSession: root, at: born + 5, child: "%tab-also%"),
        row(noTab, "spawn_failed", rootSession: root, at: born + 6),
    ]
    let stored = (try? JSONSerialization.data(withJSONObject: ["version": 1, "tasks": rows])) ?? Data()
    try? FileManager.default.createDirectory(at: store.deletingLastPathComponent(),
                                             withIntermediateDirectories: true)
    try? stored.write(to: store, options: .atomic)

    expect("every live task of this root goes, oldest first",
           Orchestrator.liveTasks(dispatchedBy: root), [live, alsoLive])
    check("a task that already finished is not cancelled — `success` is a fact about work that happened",
          !Orchestrator.liveTasks(dispatchedBy: root).contains(done))

    // The other half: the work is over, but the tab it left behind is still indented under this
    // root on the page, and closing the root has to take those with it or it reads as having done
    // nothing. Keyed on the tab rather than on the linger deadline, which does not survive a
    // restart of the app while the tab plainly does.
    expect("a finished task's tab goes with the root too, oldest first",
           Orchestrator.lingeringTasks(dispatchedBy: root), [done, alsoDone])
    check("a task that never got a tab has nothing left to close",
          !Orchestrator.lingeringTasks(dispatchedBy: root).contains(noTab))
    check("and work still running is not collected twice",
          !Orchestrator.lingeringTasks(dispatchedBy: root).contains(live)
              && !Orchestrator.lingeringTasks(dispatchedBy: root).contains(alsoLive))
    expect("another root's tabs are still nobody else's to close",
           Orchestrator.lingeringTasks(dispatchedBy: stranger), [])
    expect("another root's child is nobody else's to cancel",
           Orchestrator.liveTasks(dispatchedBy: stranger), [elsewhere])
    check("and a task with no root named is not swept up with them",
          !Orchestrator.liveTasks(dispatchedBy: root).contains(orphan)
              && !Orchestrator.liveTasks(dispatchedBy: stranger).contains(orphan))
    expect("a session nobody dispatched from cancels nothing",
           Orchestrator.liveTasks(dispatchedBy: "88888888-9999-aaaa-bbbb-cccccccccccc"), [])

    // The identity half. A session that never left a hook note cannot be matched to a task, and
    // the answer to that is *nothing* — the failure worth guarding against is a nil id quietly
    // matching every task that has no root either.
    let unknown = TargetSession(backend: .iterm, id: "%no-such-tab%", name: "x",
                                tty: "/dev/ttys-nobody", windowIndex: 0, tabIndex: 0,
                                assistant: .claude)
    expect("a session with no note of its own takes nothing down with it",
           Orchestrator.cancelChildren(ofRoot: unknown), [])
}

group("the Session info card is read off the files, and says unknown rather than 0%") {
    // The porcelain, counted. A partial add is under both headings, as `git status` lists it.
    let porcelain = """
    # branch.oid d5c61e9f91c46a77
    # branch.head main
    # branch.upstream origin/main
    # branch.ab +2 -3
    1 .M N... 100644 100644 100644 aaaaaaa aaaaaaa Sources/RemoteServer.swift
    1 MM N... 100644 100644 100644 bbbbbbb ccccccc Sources/Foo.swift
    1 A. N... 000000 100644 100644 0000000 eeeeeee Sources/Added.swift
    2 R. N... 100644 100644 100644 1111111 2222222 R100 Sources/New.swift\tSources/Old.swift
    ? Notes/new file.txt
    ? Another
    u UU N... 100644 100644 100644 100644 3333333 4444444 5555555 Sources/Conflict.swift
    """
    let files = SessionInfo.parseStatus(porcelain)
    expect("the branch is read", files.branch, "main")
    expect("so is the object id", files.head, "d5c61e9f91c46a77")
    expect("ahead", files.ahead, 2)
    expect("behind", files.behind, 3)
    expect("staged counts the index column", files.staged, 3)        // MM, A., R.
    expect("unstaged counts the worktree column", files.unstaged, 2) // .M, MM
    expect("untracked is a count of files, not a flag", files.untracked, 2)
    expect("a conflict is its own count", files.conflict, 1)
    let fresh = SessionInfo.parseStatus("# branch.oid (initial)\n# branch.head (detached)\n")
    expect("an initial commit has no head", fresh.head, "")
    expect("a detached head has no branch", fresh.branch, "")
    expect("and an empty tree counts nothing", fresh, SessionInfo.Files())

    func line(_ obj: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: obj)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }
    let now = Date(timeIntervalSince1970: 1_787_400_000)

    // **Claude writes no percentage into a transcript.** What it writes is a `quotaLimits`
    // block on the turn a window ran out, and that turn's model is `<synthetic>`.
    let turn = line(["type": "assistant", "timestamp": "2026-08-22T16:41:00.000Z",
                     "message": ["model": "claude-fable-5", "usage": ["input_tokens": 1]]])
    let refusal = line(["type": "assistant", "timestamp": "2026-08-22T16:41:57.576Z",
                        "message": ["model": "<synthetic>"],
                        "quotaLimits": ["status": "rejected", "resetsAt": 1_787_417_400,
                                        "rateLimitType": "five_hour"]])
    let hit = SessionInfo.claudeLimits(transcript: Data((turn + "\n" + refusal + "\n").utf8), now: now)
    expect("a refusal is a spent window", hit.limits.windows.map(\.name), ["5h"])
    expect("at a hundred percent", hit.limits.windows.first?.usedPercent, 100)
    expect("with when it comes back", hit.limits.windows.first?.resetsAt, 1_787_417_400)
    expect("and marked as hit", hit.limits.windows.first?.hit, true)
    expect("stamped with the record's own time", hit.limits.at, 1787416917)
    expect("the synthetic turn is not the model; the one before it is", hit.model, "claude-fable-5")

    let later = Date(timeIntervalSince1970: 1_787_500_000)
    let past = SessionInfo.claudeLimits(transcript: Data((turn + "\n" + refusal + "\n").utf8), now: later)
    check("a refusal whose reset has passed says nothing about now", past.limits.windows.isEmpty)
    let quiet = SessionInfo.claudeLimits(transcript: Data((turn + "\n").utf8), now: now)
    check("no record at all is unknown, not zero", quiet.limits.windows.isEmpty)
    expect("but the model is still read", quiet.model, "claude-fable-5")
    check("an empty file is unknown too", SessionInfo.claudeLimits(transcript: Data(), now: now).limits.windows.isEmpty)

    // The status line's shape, should a build ever write it down: read without a code change.
    let rates = line(["type": "assistant", "timestamp": "2026-08-22T16:41:00Z",
                      "rate_limits": ["five_hour": ["used_percentage": 24, "resets_at": 1_787_410_000],
                                      "seven_day": ["used_percentage": 12.5, "resets_at": 1_787_900_000]]])
    let pct = SessionInfo.claudeLimits(transcript: Data((rates + "\n").utf8), now: now)
    expect("the status line's shape reads as two windows", pct.limits.windows.map(\.name), ["5h", "7d"])
    expect("with their percentages", pct.limits.windows.map { $0.usedPercent ?? -1 }, [24, 12.5])
    expect("neither of which is hit", pct.limits.windows.map(\.hit), [false, false])
    expect("a timestamp without fractions parses too", pct.limits.at, 1787416860)

    // The same shape, from the file the status line keeps for exactly this reader. A window
    // whose reset has passed is dropped rather than shown — it was true of a window now over.
    let cacheText = """
    {"at": 1787537546, "session_id": "s", "rate_limits": {
      "five_hour": {"used_percentage": 15, "resets_at": 1787545000},
      "seven_day": {"used_percentage": 8, "resets_at": 1788000000}}}
    """
    let kept = SessionInfo.claudeLimits(cache: Data(cacheText.utf8), now: Date(timeIntervalSince1970: 1_787_537_600))
    expect("the status line's file reads as two windows", kept.windows.map(\.name), ["5h", "7d"])
    expect("with the percentages it was handed", kept.windows.map { $0.usedPercent ?? -1 }, [15, 8])
    expect("and when it wrote them", kept.at, 1787537546)
    let stale = SessionInfo.claudeLimits(cache: Data(cacheText.utf8), now: Date(timeIntervalSince1970: 1_787_600_000))
    expect("a window past its reset is gone, the other stays", stale.windows.map(\.name), ["7d"])
    check("a file that is not JSON is unknown, not a crash", SessionInfo.claudeLimits(cache: Data("{".utf8)).windows.isEmpty)
    check("and a directory without one is the same",
          SessionInfo.claudeLimits(cacheDirectory: URL(fileURLWithPath: "/nonexistent-" + UUID().uuidString)).windows.isEmpty)

    // The transcript's refusal is the stronger word about its window; the cache fills the rest.
    let refused = SessionInfo.Limits(windows: [SessionInfo.Window(name: "5h", usedPercent: 100, resetsAt: 1_787_545_000, hit: true)], at: 1_787_540_000)
    let both = SessionInfo.merged(transcript: refused, cache: kept)
    expect("a hit window replaces the cache's row of that name", both.windows.map { $0.usedPercent ?? -1 }, [100, 8])
    expect("and is the one marked hit", both.windows.map(\.hit), [true, false])
    expect("the cache alone is the answer when the transcript says nothing",
           SessionInfo.merged(transcript: SessionInfo.Limits(), cache: kept).windows.map(\.name), ["5h", "7d"])

    // Codex keeps the running answer on every `token_count`; the newest one is the state.
    let tokenCount = line(["timestamp": "2026-08-23T16:49:47.975Z", "type": "event_msg",
                           "payload": ["type": "token_count",
                                       "info": ["total_token_usage": ["input_tokens": 10]],
                                       "rate_limits": ["primary": ["used_percent": 28.0, "window_minutes": 10080,
                                                                   "resets_at": 1_788_104_505],
                                                       "secondary": NSNull()]]])
    let older = line(["timestamp": "2026-08-23T16:00:00.000Z", "type": "event_msg",
                      "payload": ["type": "token_count", "info": [:],
                                  "rate_limits": ["primary": ["used_percent": 3.0, "window_minutes": 300,
                                                              "resets_at": 1]]]])
    let codex = SessionInfo.codexLimits(rollout: Data((older + "\n" + tokenCount + "\n").utf8))
    expect("the newest token_count is the answer", codex.windows.map(\.name), ["7d"])
    expect("with its percentage", codex.windows.first?.usedPercent, 28)
    expect("and its reset", codex.windows.first?.resetsAt, 1_788_104_505)
    expect("a null secondary is not a window", codex.windows.count, 1)
    expect("stamped", codex.at, 1787503787)
    check("a rollout with no token_count is unknown",
          SessionInfo.codexLimits(rollout: Data("{\"type\":\"session_meta\"}\n".utf8)).windows.isEmpty)

    expect("five hours", SessionInfo.windowName(minutes: 300), "5h")
    expect("seven days", SessionInfo.windowName(minutes: 10080), "7d")
    expect("a day", SessionInfo.windowName(minutes: 1440), "1d")
    expect("an odd number of minutes stays minutes", SessionInfo.windowName(minutes: 90), "90m")
    expect("Claude's names map to the same words", SessionInfo.windowName(claudeType: "seven_day"), "7d")
    expect("and an unknown one is passed through rather than invented", SessionInfo.windowName(claudeType: "monthly"), "monthly")

    // The wire shape. Absent, not zeroed, where nothing was known.
    var usage = Orchestrator.Usage()
    usage.input = 10; usage.output = 20; usage.cacheRead = 30; usage.cacheWrite = 40; usage.total = 100
    usage.model = "claude-fable-5"; usage.costUsd = 0.1234
    let started = Date(timeIntervalSince1970: 1_787_390_000)
    let payload = SessionInfo.payload(
        id: "ABC", assistant: .claude, sessionId: "s-1", model: "claude-fable-5", cwd: "/tmp/x",
        startedAt: started, now: now, usage: usage, limits: hit.limits, files: files,
        deploy: [["label": "ci", "url": "https://x", "kind": "deploy", "state": "ok", "local": false]])
    let session = payload["session"] as? [String: Any]
    expect("the session's id", session?["id"] as? String, "ABC")
    expect("its assistant", session?["assistant"] as? String, "claude")
    expect("its model", session?["model"] as? String, "claude-fable-5")
    expect("its directory", session?["cwd"] as? String, "/tmp/x")
    expect("when it started", session?["startedAt"] as? Int, 1_787_390_000)
    expect("and how long that is", session?["seconds"] as? Int, 10_000)
    let counts = payload["usage"] as? [String: Any]
    expect("the totals", counts?["total"] as? Int, 100)
    expect("the cache halves are separate", counts?["cacheWrite"] as? Int, 40)
    expect("and the cost", counts?["costUsd"] as? Double, 0.1234)
    let plan = payload["limits"] as? [String: Any]
    let windows = plan?["windows"] as? [[String: Any]]
    expect("one window", windows?.count, 1)
    expect("named", windows?.first?["name"] as? String, "5h")
    expect("marked hit", windows?.first?["hit"] as? Bool, true)
    expect("and dated", plan?["at"] as? Int, 1787416917)
    let tree = payload["files"] as? [String: Any]
    expect("the tree, counted", tree?["staged"] as? Int, 3)
    expect("with its branch", tree?["branch"] as? String, "main")
    expect("the deploy rows pass through untouched",
           (payload["deploy"] as? [[String: Any]])?.first?["state"] as? String, "ok")
    check("and it is JSON", JSONSerialization.isValidJSONObject(payload))

    let bare = SessionInfo.payload(id: "X", assistant: nil, sessionId: nil, model: nil, cwd: nil,
                                   startedAt: nil, now: now, usage: nil,
                                   limits: SessionInfo.Limits(), files: nil, deploy: [])
    check("no transcript is no usage, not zero usage", bare["usage"] == nil)
    check("no repository is no count, not a clean one", bare["files"] == nil)
    check("no start is no age", (bare["session"] as? [String: Any])?["seconds"] == nil)
    expect("unknown limits are an empty list the page can draw as unknown",
           ((bare["limits"] as? [String: Any])?["windows"] as? [[String: Any]])?.count, 0)
    check("and it is JSON too", JSONSerialization.isValidJSONObject(bare))

    // The route is a read like the others: a token, or nothing.
    expect("the route needs a paired device",
           RemoteServer.shared.route(remoteRequest("GET", "/v1/sessions/nope/info")).status, 401)
    // And the one `git` it runs answers nothing outside a repository rather than a clean tree.
    check("a directory that is not a repository has no count",
          SessionInfo.files(cwd: NSTemporaryDirectory()) == nil)
}

group("the models a session can be moved to, and the word that moves each") {
    // What Codex's own picker would list, as its cache on disk says it. The hidden row and the
    // one without a slug are the two ways a row is not a button.
    let cache = """
    {"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6-Sol","visibility":"list"},
               {"slug":"gpt-5.4-mini","display_name":"","visibility":"list"},
               {"slug":"codex-auto-review","display_name":"Codex Auto Review","visibility":"hide"},
               {"display_name":"no slug","visibility":"list"}]}
    """
    let models = SessionInfo.codexModels(cache: Data(cache.utf8))
    expect("the rows the picker lists, in its order", models.map { $0.id }, ["gpt-5.6-sol", "gpt-5.4-mini"])
    expect("a display name is the name", models[0].name, "GPT-5.6-Sol")
    expect("and an empty one falls back to the slug", models[1].name, "gpt-5.4-mini")
    expect("Codex is told the slug itself", models[0].command, "gpt-5.6-sol")
    expect("a cache that is not JSON is no models, not a crash", SessionInfo.codexModels(cache: Data("{".utf8)), [])
    expect("and a home without one is the same",
           SessionInfo.codexModels(home: URL(fileURLWithPath: "/nonexistent-" + UUID().uuidString)), [])

    // Claude Code is told an alias — `/model sonnet` — and the page finds the current row by the
    // prefix of a full id, which is what survives a dated release like `claude-haiku-4-5-20251001`.
    let claude = SessionInfo.models(for: .claude)
    check("there are models for Claude", !claude.isEmpty)
    check("Claude Code is told an alias, never a dated id", claude.allSatisfy { !$0.command.hasPrefix("claude-") })
    check("and every row has an id the current model matches by prefix", claude.allSatisfy { $0.id.hasPrefix("claude-") })
    expect("no assistant, no models", SessionInfo.models(for: nil), [])

    let payload = SessionInfo.payload(id: "X", assistant: .claude, sessionId: nil, model: "claude-sonnet-5",
                                      cwd: nil, startedAt: nil, usage: nil, limits: SessionInfo.Limits(),
                                      files: nil, deploy: [], models: claude)
    let rows = payload["models"] as? [[String: Any]] ?? []
    expect("the card gets one row per model", rows.count, claude.count)
    expect("with the three words the page needs", rows.first.map { Set($0.keys) } ?? [], ["id", "name", "command"])
    let bare = SessionInfo.payload(id: "X", assistant: nil, sessionId: nil, model: nil, cwd: nil, startedAt: nil,
                                   usage: nil, limits: SessionInfo.Limits(), files: nil, deploy: [])
    expect("and none is an empty list rather than an absent key", (bare["models"] as? [[String: Any]])?.count, 0)
}

group("the model a `/model` names, before the reply that proves it") {
    func line(_ obj: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: obj)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }
    func user(_ content: String) -> String {
        line(["type": "user", "message": ["role": "user", "content": content]])
    }
    func file(_ rows: [String]) -> Data { Data((rows.joined(separator: "\n") + "\n").utf8) }

    // Exactly the three rows Claude Code writes for one `/model`, caveat and all. None of them is
    // an assistant turn, so before this the file's last word on the model was the one the session
    // had just left — and for a session that has only ever been switched, there was no word at all.
    let caveat = line(["type": "user", "isMeta": true,
                       "message": ["role": "user", "content": "<local-command-caveat>Caveat: …</local-command-caveat>"]])
    let asked = user("<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>opus</command-args>")
    let printed = user("<local-command-stdout>Set model to \u{1b}[1mOpus 5\u{1b}[22m and saved as your default for new sessions</local-command-stdout>")
    let reply = line(["type": "assistant", "message": ["model": "claude-sonnet-5", "usage": ["input_tokens": 1]]])

    expect("a switch is the newest thing anybody said about the model",
           SessionInfo.claudeLimits(transcript: file([reply, caveat, asked, printed])).model, "claude-opus-5")
    expect("and a reply after the switch is newer still",
           SessionInfo.claudeLimits(transcript: file([caveat, asked, printed, reply])).model, "claude-sonnet-5")
    expect("a session that has done nothing but switch still knows what it is on",
           SessionInfo.claudeLimits(transcript: file([caveat, asked, printed])).model, "claude-opus-5")
    // The word typed becomes the id a reply would have carried, so the card says one thing about
    // one session either side of that reply and the page's row matching keeps working.
    expect("the word typed is turned into the id a reply would have carried",
           SessionInfo.claudeLimits(transcript: file([asked])).model, "claude-opus-5")

    // `/model` with nothing after it opens a picker: the row records that a switch happened
    // without recording what to, and the line it printed is the only place the choice appears.
    let picked = user("<command-name>/model</command-name>\n<command-args></command-args>")
    expect("a picker's choice is read off what it printed",
           SessionInfo.claudeLimits(transcript: file([picked, printed])).model, "claude-opus-5")
    check("and a picker that printed nothing says nothing",
          SessionInfo.claudeLimits(transcript: file([picked])).model == nil)
    // Read from the end, the printed line arrives first — and is only believed where a `/model`
    // asked for it. On its own it is somebody's terminal output.
    check("a stray line about models is not a switch",
          SessionInfo.claudeLimits(transcript: file([printed])).model == nil)

    expect("the word a row asked for", SessionInfo.modelSwitch(inRow: "<command-name>/model</command-name><command-args>haiku</command-args>"), "haiku")
    expect("only the first word of it", SessionInfo.modelSwitch(inRow: "<command-name>/model</command-name><command-args>opus and then some</command-args>"), "opus")
    expect("no argument is the empty string, which is not the same as no row",
           SessionInfo.modelSwitch(inRow: "<command-name>/model</command-name>"), "")
    check("another command is not one", SessionInfo.modelSwitch(inRow: "<command-name>/recap</command-name>") == nil)
    check("and neither is ordinary prose", SessionInfo.modelSwitch(inRow: "switch to opus please") == nil)

    expect("the name it printed, without the terminal's bold",
           SessionInfo.modelPrinted(inRow: "<local-command-stdout>Set model to \u{1b}[1mHaiku 4.5\u{1b}[22m and saved as your default for new sessions</local-command-stdout>"),
           "Haiku 4.5")
    expect("and without a trailing clause when there is none",
           SessionInfo.modelPrinted(inRow: "<local-command-stdout>Set model to Sonnet 5</local-command-stdout>"), "Sonnet 5")
    check("a wording this build does not know comes back nil rather than wrong",
          SessionInfo.modelPrinted(inRow: "<local-command-stdout>Model unchanged</local-command-stdout>") == nil)

    expect("an alias becomes the id", SessionInfo.claudeModelID(word: "sonnet", printed: nil), "claude-sonnet-5")
    expect("a printed name becomes the same id", SessionInfo.claudeModelID(word: "", printed: "Sonnet 5"), "claude-sonnet-5")
    expect("a model released after this build is passed through as it was written",
           SessionInfo.claudeModelID(word: "opus-6", printed: nil), "opus-6")
    expect("and so is a name it has never heard printed",
           SessionInfo.claudeModelID(word: "", printed: "Default (recommended)"), "Default (recommended)")
    check("nothing said is nothing answered", SessionInfo.claudeModelID(word: "", printed: nil) == nil)
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
