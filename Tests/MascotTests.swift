import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

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

// MARK: - Mascot packs that ship



// MARK: - Validation catches the mistakes an author actually makes



// MARK: - Claude Code skills







// MARK: - Keyframe sampling





// MARK: - Colour and skin







// MARK: - Display geometry



// MARK: - Hotkeys



// MARK: - Target parsing

func runMascotTests() {
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
    let identified = Assistant.reading(
        ofPS: "ttys013 102 1 Wed Aug 27 12:34:56 2026 /opt/homebrew/bin/claude")
    expect("production ps carries authoritative process start into the identity",
           identified["ttys013"]?.processStart,
           Assistant.parseProcessStart(["Wed", "Aug", "27", "12:34:56", "2026"]))
}

group("assistant process scans distinguish absence from failure") {
    let missing = ITerm.parseAssistantProcessScan("", timedOut: false)
    check("silence is an unreadable process list", !missing.isComplete)
    check("and carries a diagnostic", missing.error != nil)

    let timedOut = ITerm.parseAssistantProcessScan("ttys006 101 claude", timedOut: true)
    check("a timeout is unreadable even if a partial line arrived", !timedOut.isComplete)

    let nonzero = ITerm.parseAssistantProcessScan("ttys006 101 claude", timedOut: false,
                                                  exitStatus: 1)
    check("a nonzero ps exit cannot bless partial stdout", !nonzero.isComplete)

    let noAssistant = ITerm.parseAssistantProcessScan("?? 1 0 /sbin/launchd", timedOut: false)
    check("a non-empty ps answer is trustworthy", noAssistant.isComplete)
    expect("and can truthfully contain no assistants", noAssistant.assistants.count, 0)

    let found = ITerm.parseAssistantProcessScan("ttys006 101 1 claude", timedOut: false)
    check("a readable assistant list is complete", found.isComplete)
    expect("and preserves the parsed process", found.assistants["ttys006"]?.pid, 101)
    check("iTerm reported stopped contradicts a live assistant even on the first scan",
          ITerm.stoppedTerminalContradictsProcesses(appRunning: false, processScan: found))
    check("an observed running iTerm does not contradict the same process list",
          !ITerm.stoppedTerminalContradictsProcesses(appRunning: true, processScan: found))
    check("a genuinely empty process list agrees that iTerm may be stopped",
          !ITerm.stoppedTerminalContradictsProcesses(appRunning: false, processScan: noAssistant))
    check("an Apple Event timeout is circuit evidence",
          ITerm.automationCircuitEvidence(appleEventTimedOut: true, listRowsMalformed: false))
    check("a malformed list is circuit evidence",
          ITerm.automationCircuitEvidence(appleEventTimedOut: false, listRowsMalformed: true))
    check("ps incompleteness is not iTerm modal evidence",
          !ITerm.automationCircuitEvidence(appleEventTimedOut: false, listRowsMalformed: false))
    check("a stopped-iTerm cross-backend contradiction is not modal evidence",
          ITerm.stoppedTerminalContradictsProcesses(appRunning: false, processScan: found)
              && !ITerm.automationCircuitEvidence(appleEventTimedOut: false,
                                                  listRowsMalformed: false))
    let ttyGone = ITerm.parseTTYAssistantObservation("?? 1 0 /sbin/launchd", tty: "ttys006",
                                                     timedOut: false, exitStatus: 0)
    check("an exact tty scan can positively prove the assistant gone",
          ttyGone.isComplete && ttyGone.running == nil)
    let ttyFailed = ITerm.parseTTYAssistantObservation("", tty: "ttys006", timedOut: false,
                                                       exitStatus: nil)
    check("a failed exact tty scan never masquerades as absence", !ttyFailed.isComplete)
    let ttyBusy = ITerm.parseTTYAssistantObservation("ttys006 101 1 claude", tty: "ttys006",
                                                     timedOut: false, exitStatus: 0)
    expect("an exact tty scan preserves the process that blocks close", ttyBusy.running?.pid, 101)
}

group("safe close consumes a fresh exact-tty observation and fails closed") {
    defer {
        ITerm.ttyAssistantObservationForTesting = nil
        Targets.terminalCloseForTesting = nil
        Targets.safeCloseInventoryForTesting = nil
    }
    let session = TargetSession(backend: .iterm, id: "TAB", name: "x",
                                tty: "/dev/ttys006", windowIndex: 0, tabIndex: 0,
                                assistant: nil)
    var readings = [
        ITerm.TTYAssistantObservation(running: nil, error: "ps failed"),
        ITerm.TTYAssistantObservation(
            running: Assistant.Running(assistant: .codex, pid: 101), error: nil),
        ITerm.TTYAssistantObservation(running: nil, error: nil),
    ]
    var observedTTYs: [String] = []
    var closes = 0
    Targets.safeCloseInventoryForTesting = {
        var snapshot = Targets.Snapshot()
        snapshot.sessions = [session]
        return snapshot
    }
    ITerm.ttyAssistantObservationForTesting = { tty in
        observedTTYs.append(tty)
        // A scripted reading per decision. Running out means production asked more often than
        // this scenario allows, which is a failed assertion below — not a crashed suite, which
        // is what it was, and which hides every check after it.
        return readings.isEmpty
            ? ITerm.TTYAssistantObservation(running: nil, error: "no reading was scripted")
            : readings.removeFirst()
    }
    Targets.terminalCloseForTesting = { _ in closes += 1; return nil }

    check("an incomplete fresh scan refuses close", Targets.closeIfAssistantGone(session) != nil)
    expect("scan failure never reaches the backend close", closes, 0)
    check("a positively running assistant refuses close", Targets.closeIfAssistantGone(session) != nil)
    expect("a running guard never reaches the backend close", closes, 0)
    check("only proved absence permits close", Targets.closeIfAssistantGone(session) == nil)
    expect("proved absence closes exactly once", closes, 1)
    expect("each decision performed its own exact-tty scan", observedTTYs,
           ["ttys006", "ttys006", "ttys006"])

    var incomplete = Targets.Snapshot()
    incomplete.sessions = [session]
    incomplete.isComplete = false
    incomplete.error = "inventory failed"
    // The scripted reading below positively proves the assistant gone, so the inventory gate is
    // the only thing left that can refuse. Without it the check passed on the exact-tty guard
    // behind it, and the gate could be deleted with every check in this file still green.
    readings = [ITerm.TTYAssistantObservation(running: nil, error: nil)]
    Targets.safeCloseInventoryForTesting = { incomplete }
    check("an explicit close refuses an incomplete fresh terminal inventory",
          Targets.closeIfAssistantGone(session) != nil)
    expect("incomplete inventory never reaches the backend close", closes, 1)
}

group("incomplete session inventories merge but complete ones replace") {
    func session(_ id: String, _ name: String, backend: Backend = .iterm,
                 tty: String? = nil) -> TargetSession {
        TargetSession(backend: backend, id: id, name: name, tty: tty ?? "/dev/tty\(id)",
                      windowIndex: 0, tabIndex: 0, assistant: .claude)
    }
    let oldA = session("A", "old A")
    let oldB = session("B", "old B")
    let newA = session("A", "new A")

    let partial = Targets.reconcile(previous: [oldA, oldB], scanned: [newA], complete: false)
    expect("a partial row replaces its older copy", partial.sessions[0].name, "new A")
    expect("an unseen row survives an incomplete scan", partial.sessions.map(\.id), ["A", "B"])
    expect("the reconciliation reports what it retained", partial.preserved, 1)

    let failed = Targets.reconcile(previous: [oldA, oldB], scanned: [], complete: false)
    expect("an all-failed scan retains the last known list", failed.sessions.map(\.id), ["A", "B"])
    expect("all retained rows are counted", failed.preserved, 2)

    let provedClosed = Targets.reconcile(previous: [oldA, oldB], scanned: [newA], complete: false,
                                          confirmedAbsent: ["B"])
    expect("process proof removes an omitted closed session despite broken JXA",
           provedClosed.sessions.map(\.id), ["A"])
    expect("the independent removal is counted", provedClosed.confirmedRemoved, 1)

    let closed = Targets.reconcile(previous: [oldA, oldB], scanned: [], complete: true)
    expect("a confirmed empty scan removes stale sessions", closed.sessions.count, 0)
    expect("a complete scan retains nothing by doubt", closed.preserved, 0)

    check("a live iTerm process contradicts a complete inventory that omitted its tty",
          Targets.hasLiveProcessContradiction(previous: [oldA], scanned: [],
                                              runningTTYs: ["ttyA"]))
    check("the same tty observed under a fresh session id resolves the contradiction",
          !Targets.hasLiveProcessContradiction(previous: [oldA],
                                               scanned: [session("new", "new", tty: "/dev/ttyA")],
                                               runningTTYs: ["ttyA"]))
    check("a tmux process does not contradict an empty iTerm inventory",
          !Targets.hasLiveProcessContradiction(
              previous: [session("T", "tmux", backend: .tmux, tty: "/dev/ttys009")],
              scanned: [], runningTTYs: ["ttys009"]))
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

group("a hung tmux subprocess times out and is cleaned up") {
    defer {
        Tmux.binaryForTesting = nil
        Tmux.subprocessTimeoutForTesting = nil
    }
    Tmux.binaryForTesting = "/bin/sleep"
    let started = Date()
    let receipt = Tmux.runForTesting(["5"], timeout: 0.05)
    check("the real subprocess call returns inside its bound",
          Date().timeIntervalSince(started) < 1)
    expect("a hung tmux command carries a typed timeout", receipt.failure?.kind, .timeout)
    check("the hung command is not reported as successful", !receipt.ok)
    if let pid = Tmux.lastTimedOutPIDForTesting {
        check("the timed-out tmux subprocess was reaped",
              kill(pid, 0) == -1 && errno == ESRCH)
    } else {
        check("the timeout records which subprocess it cleaned up", false)
    }
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

    // Past a SIGKILL there is nothing left to try, but elapsed time still is not proof that the
    // tty is empty. Safe-close preserves the tab for intervention.
    let exhausted = polite + Targets.Farewell.afterTerm + Targets.Farewell.afterKill
    expect("a process that survives SIGKILL is never force-closed",
           step(exhausted, termed: true, killed: true), .refuse)
}

group("TERM and KILL rungs stay bound to one process identity") {
    typealias Identity = Targets.Farewell.ProcessIdentity
    let first = Identity(pid: 42, processStart: Date(timeIntervalSince1970: 1_800_100_000))
    let differentPID = Identity(pid: 99,
                                processStart: Date(timeIntervalSince1970: 1_800_100_000))
    let reusedPID = Identity(pid: 42,
                             processStart: Date(timeIntervalSince1970: 1_800_100_001))
    let atKill = Targets.Farewell.polite + Targets.Farewell.afterTerm
    expect("the identity that received TERM may advance to KILL",
           Targets.Farewell.step(elapsed: atKill, identity: first,
                                 termed: first, killed: nil), .kill(42))
    expect("a different PID after TERM fails closed",
           Targets.Farewell.step(elapsed: atKill, identity: differentPID,
                                 termed: first, killed: nil), .refuse)
    expect("the same PID with a new process start after TERM fails closed",
           Targets.Farewell.step(elapsed: atKill, identity: reusedPID,
                                 termed: first, killed: nil), .refuse)
}

group("the sacrificial safe-close lifecycle never signals a replacement process") {
    defer {
        ITerm.ttyAssistantObservationForTesting = nil
        Targets.safeCloseSignalForTesting = nil
        Targets.safeCloseSleepForTesting = nil
        Targets.safeCloseNowForTesting = nil
    }
    let session = TargetSession(backend: .iterm, id: "FAKE-CLOSE", name: "fake",
                                tty: "/dev/ttys199", windowIndex: 0, tabIndex: 0,
                                assistant: .codex)
    let first = Assistant.Running(
        assistant: .codex, pid: 42,
        processStart: Date(timeIntervalSince1970: 1_800_200_000))

    func lifecycle(replacement: Assistant.Running?) -> (String?, [(pid_t, Int32)]) {
        var clock = Date(timeIntervalSince1970: 1_800_200_100)
        var signals: [(pid_t, Int32)] = []
        Targets.safeCloseNowForTesting = { clock }
        Targets.safeCloseSleepForTesting = { clock.addTimeInterval($0) }
        Targets.safeCloseSignalForTesting = { signals.append(($0, $1)) }
        ITerm.ttyAssistantObservationForTesting = { _ in
            ITerm.TTYAssistantObservation(running: signals.isEmpty ? first : replacement,
                                           error: nil)
        }
        return (Targets.waitToBeGoneForTesting(session), signals)
    }

    let otherPID = lifecycle(replacement: Assistant.Running(
        assistant: .codex, pid: 99,
        processStart: Date(timeIntervalSince1970: 1_800_200_000)))
    check("a different PID after TERM is refused", otherPID.0 != nil)
    expect("only the original identity received a signal", otherPID.1.map(\.0), [42])
    expect("that one signal was TERM", otherPID.1.map(\.1), [SIGTERM])

    let reused = lifecycle(replacement: Assistant.Running(
        assistant: .codex, pid: 42,
        processStart: Date(timeIntervalSince1970: 1_800_200_001)))
    check("same PID with a new start after TERM is refused", reused.0 != nil)
    expect("PID reuse never advances to KILL", reused.1.map(\.1), [SIGTERM])

    let gone = lifecycle(replacement: nil)
    check("proved absence after TERM completes the wait", gone.0 == nil)
    expect("the successful seam still sent no KILL", gone.1.map(\.1), [SIGTERM])
}
}
