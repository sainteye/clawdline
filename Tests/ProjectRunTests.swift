import Foundation

// The seventh project status file: a test or a build running on this Mac.
//
// It reuses `ghrun-`'s rules on purpose, so most of what is worth testing here is the two places
// it deliberately differs. It is keyed by working directory rather than by git remote, because
// this machine routinely has several worktrees of one repository running at once and a remote
// cannot tell them apart. And a `running` row goes quiet on its own after `stale_after`, because
// nothing polls these files: without that ceiling a `kill -9`'d run spins in the bar forever.
//
// The ceiling lives in the reader rather than in the producer, which is what makes it worth a
// test of its own — every reader inherits it, including the ones nobody has written yet.
//
// Every row below arrives through `JSONSerialization`, never as a Swift dictionary literal.
// That is the shape the app actually reads, and it is not a detail: a `120` written in Swift is
// an `Int` that `as? Double` refuses, while the same `120` out of a file is an `NSNumber` that
// it accepts. A fixture built the convenient way would pass while testing the wrong thing.
func runProjectRunTests() {

group("a local run is read from its own file, and a dead one stops being drawn") {
    func read(_ json: String) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any]
    }
    func example(_ name: String) -> [String: Any]? {
        ProjectStatus.json(URL(fileURLWithPath: "docs/examples/" + name))
    }

    // docs/project-status.md tells other people how to write this file. The example beside it is
    // parsed here with the code the app uses, at a moment inside its own window — the fixture
    // carries fixed timestamps like every other example in that directory, so the clock reading
    // it has to be fixed too, or the whole thing would go stale on the shelf.
    let inside = 1788105700.0
    let shipped = ProjectStatus.run(example("run--Users-you-code-atrium.json"), now: inside)
    expect("the documented example is running", shipped?.state, "running")
    expect("with the producer's own label", shipped?.label, "test")
    expect("and its phase, drawn verbatim", shipped?.phase, "compiling")
    expect("it says when it began", shipped?.startedAt, 1788105600)
    expect("and how long this usually takes", shipped?.typicalSeconds, 288)
    expect("and when it last said anything", shipped?.updatedAt, 1788105640)
    expect("with the ceiling it asked for", shipped?.staleAfter, 900)
    expect("a log is a path for a person", shipped?.log, "/tmp/atrium-tests-31981.log")
    expect("the session that started it", shipped?.holder, "atrium-8d")
    expect("and the tree it belongs to", shipped?.tree, "/Users/you/code/atrium")
    expect("a hundred seconds into two hundred and eighty-eight",
           shipped.map { ProjectStatus.bar($0.progress(now: inside), width: 8) }, "▰▰▰▱▱▱▱▱")
    expect("which is how long it has been going",
           shipped.map { ProjectStatus.duration($0.elapsed(now: inside)) }, "1m 40s")

    // Every field is optional on the way in except `state`: these files belong to another
    // program and their shape can change, and a footer that throws the row away because one key
    // moved is worse than one that draws the parts it still recognises.
    let bare = ProjectStatus.run(read(#"{"state":"ok"}"#), now: inside)
    expect("a row with nothing but a state still parses", bare?.state, "ok")
    expect("and falls back to a label of its own", bare?.label, "run")
    // `bare?.phase` is an optional inside an optional, and `== nil` on that asks only whether
    // `bare` parsed. The `?? nil` flattens it, so what is asked is whether the phase is absent.
    check("with no phase to draw", (bare?.phase ?? nil) == nil)
    check("and nowhere to send anybody",
          (bare?.log ?? nil) == nil && (bare?.tree ?? nil) == nil)
    expect("an absent ceiling is the documented default", bare?.staleAfter, 900)
    // **`updated_at` is required, and it is required of a `running` row only.** The contract said
    // so and, two lines later, that every field but `state` was optional; two implementations
    // resolved that in opposite directions and each guarded its own answer. This is the answer:
    // making it required is what a liveness ceiling a reader can trust is made of, and falling
    // back to `started_at` — which this reader did — makes "required" mean nothing. A finished
    // verdict is not measured against it, so it does not need one.
    check("a running row that never says when is not evidence of anything running",
          ProjectStatus.run(read(#"{"state":"running"}"#), now: inside) == nil)
    check("not even one that started a moment ago, which is where the old fallback hid",
          ProjectStatus.run(read(#"{"state":"running","started_at":\#(inside - 5)}"#),
                            now: inside) == nil)
    check("and a malformed updated_at is an absent one, the same rule as everywhere else",
          ProjectStatus.run(read(#"{"state":"running","started_at":\#(inside - 5),"updated_at":"soon"}"#),
                            now: inside) == nil)
    check("while a verdict keeps being drawn without one, because it is not alive to decay",
          ProjectStatus.run(read(#"{"state":"fail"}"#), now: inside) != nil)
    // `stale_after` has a default and `updated_at` has none, which is the whole of why the two
    // malformed cases end differently. `0` is a value a producer meant, not a missing field:
    // "falsy therefore default" is a language accident rather than a decision.
    check("a ceiling of zero expires the instant anything moves",
          ProjectStatus.run(read(#"{"state":"running","updated_at":\#(inside),"stale_after":0}"#),
                            now: inside) != nil
            && ProjectStatus.run(read(#"{"state":"running","updated_at":\#(inside - 1),"stale_after":0}"#),
                                 now: inside) == nil)
    check("while a malformed ceiling falls back to the documented 900",
          ProjectStatus.run(read(#"{"state":"running","updated_at":\#(inside - 900),"stale_after":"soon"}"#),
                            now: inside) != nil
            && ProjectStatus.run(read(#"{"state":"running","updated_at":\#(inside - 901),"stale_after":"soon"}"#),
                                 now: inside) == nil)
    check("and an empty phase is not a phase",
          (ProjectStatus.run(read(#"{"state":"ok","phase":""}"#), now: inside)?.phase ?? nil)
              == nil)

    // `none` is a state a producer writes constantly — no run, nothing to say — and every state
    // this reader has not heard of is treated the same way. **Never a cross for a state you do
    // not recognise:** that is a red mark that is always wrong.
    check("nothing to say draws nothing",
          ProjectStatus.run(read(#"{"state":"none"}"#), now: inside) == nil)
    check("and neither does a state nobody has heard of",
          ProjectStatus.run(read(#"{"state":"cancelled"}"#), now: inside) == nil)
    check("or a row with no state at all",
          ProjectStatus.run(read(#"{"label":"test"}"#), now: inside) == nil)
    check("or no row at all", ProjectStatus.run(nil, now: inside) == nil)

    // The staleness rule, at both sides of its boundary. `stale_after` counts from the last
    // thing the producer said and not from the start, so a long run that keeps writing stays up.
    let touched = 2_000_000_000.0
    let living = read(#"""
    {"state":"running","started_at":1999999940,"updated_at":2000000000,"stale_after":120}
    """#)
    check("a run that spoke inside its window is still running",
          ProjectStatus.run(living, now: touched + 120) != nil)
    check("and a second past it, nobody is told it is still running",
          ProjectStatus.run(living, now: touched + 121) == nil)
    let silent = read(#"{"state":"running","updated_at":2000000000}"#)
    check("a producer that named no ceiling gets the default one",
          ProjectStatus.run(silent, now: touched + 900) != nil)
    check("and loses the bar a second later",
          ProjectStatus.run(silent, now: touched + 901) == nil)
    check("a finished run is never stale — it is an answer, not a claim about something moving",
          ProjectStatus.run(read(#"{"state":"fail","updated_at":2000000000}"#),
                            now: touched + 86_400) != nil)

    // Keyed by working directory, the same way `backlog-`, `health-` and `milestone-` are.
    let cache = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-run-cache-\(UUID().uuidString)", isDirectory: true)
    let project = cache.appendingPathComponent("tree", isDirectory: true)
    try! FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
    let priorStatusDir = Config.shared.statusDir
    Config.shared.statusDir = cache.path
    defer {
        Config.shared.statusDir = priorStatusDir
        try? FileManager.default.removeItem(at: cache)
    }
    let runFile = cache.appendingPathComponent(
        "run-\(ProjectStatus.key(forPath: project.path)).json")
    let now = Date().timeIntervalSince1970
    func write(_ json: String) { try! Data(json.utf8).write(to: runFile) }
    func linkRow() -> [String: Any]? {
        RemoteServer.shared.linksPayload(cwd: project.path, sessionID: "SESSION-1")
            .first { $0["kind"] as? String == "run" }
    }

    write("""
    {"state":"running","label":"test","phase":"swiftc","started_at":\(now - 100),
     "typical_seconds":288,"updated_at":\(now - 5)}
    """)
    let snapshot = ProjectStatus.read(cwd: project.path, remote: nil)
    expect("the reader finds the file under this tree's own name", snapshot.run?.label, "test")
    expect("and the phase reaches the footer", snapshot.run?.phase, "swiftc")
    check("a project with only a run is not an empty project", !snapshot.isEmpty)
    check("and the run is not mistaken for a deploy", snapshot.deploy == nil)

    // The row `linksPayload` emits, key for key. **No `url`, and none required** — a local run
    // has no web page, and the deploy row's `guard let url` is exactly why one written in the
    // `ghrun-` shape reaches the Mac footer and is invisible on the phone.
    let row = linkRow() ?? [:]
    check("the local run reaches the page's link list", !row.isEmpty)
    expect("with exactly the keys the contract names", Set(row.keys),
           ["label", "kind", "state", "local", "phase", "startedAt", "typicalSeconds"])
    expect("labelled by the producer", row["label"] as? String, "test")
    expect("and marked as happening on this Mac", row["local"] as? Bool, true)
    expect("its phase travels with it", row["phase"] as? String, "swiftc")
    expect("and so does what the bar is drawn from", row["typicalSeconds"] as? Double, 288)
    check("nothing here is an address", row["url"] == nil)
    check("least of all the log, which no browser could open", row["log"] == nil)

    write("""
    {"state":"ok","label":"test","started_at":\(now - 300),"updated_at":\(now - 12)}
    """)
    let done = linkRow() ?? [:]
    expect("a finished run keeps its outcome", done["state"] as? String, "ok")
    expect("and stops carrying a clock nobody should draw",
           Set(done.keys), ["label", "kind", "state", "local"])

    write("""
    {"state":"running","label":"test","started_at":\(now - 4000),"updated_at":\(now - 3600)}
    """)
    check("a run nobody has touched for an hour is gone from the reader",
          ProjectStatus.read(cwd: project.path, remote: nil).run == nil)
    check("and gone from the page's list with it", linkRow() == nil)
}

// The rule the `run-` reader was written with, applied to the two readers beside it that never had
// it. `docs/project-status.md` has said for a long time that an unrecognised state must draw
// nothing, "because a consumer that has not heard of `none` will draw a cross for a project that
// simply has no CI, which is a red mark that is always wrong" — and it says it now as a property
// of all seven files. `ghrun-` and health did not honour it: `deploy(_:)` accepted any string and
// the footer drew `state == "ok" ? "✓" : "✗"` over the result.
//
// **The cost was not hypothetical.** Measured in `~/.claude/statusline-cache/` on 2026-09-05:
// fifteen `ghrun-*.json` files, twelve of them exactly `{"state":"none"}`, so twelve projects were
// each drawing a red ✗ in the Mac footer at that moment.
group("a state a reader has not heard of draws nothing, in every one of these files") {
    func read(_ json: String) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any]
    }

    // The real shape, not an invented one: this is what claude-bestiary writes for a project with
    // no workflow, and it is the row `docs/examples/ghrun-you-quiet.json` ships.
    check("the row twelve projects on this Mac were drawing a cross for draws nothing",
          ProjectStatus.deploy(read(#"{"state":"none","why":"no-runs"}"#)) == nil)
    check("and neither does a deploy state nobody has heard of",
          ProjectStatus.deploy(read(#"{"state":"cancelled"}"#)) == nil)
    check("or a deploy row with no state at all",
          ProjectStatus.deploy(read(#"{"label":"deploy"}"#)) == nil)
    check("while the three a reader does know still parse",
          ["running", "ok", "fail"].allSatisfy {
              ProjectStatus.deploy(read("{\"state\":\"\($0)\"}")) != nil
          })

    // Health carries two vocabularies, because two producers write these files and both are on
    // this machine: the page documents `ok | sick | offline | unknown`, and the multi-surface
    // receipt clawdline-cloud writes uses `online | not_deployed | unhealthy | unreachable`. The
    // allow-list names both — what it is for is that a state from neither draws nothing.
    check("a health state nobody has heard of draws nothing rather than a red dot",
          ProjectStatus.health(read(#"{"state":"none"}"#)) == nil
            && ProjectStatus.health(read(#"{"state":"degraded"}"#)) == nil)
    check("and neither does one that only means it has not been checked yet",
          ProjectStatus.health(read(#"{"state":"unknown"}"#)) == nil)
    check("while every state either producer actually writes still parses",
          ["ok", "sick", "offline", "online", "not_deployed", "unhealthy", "unreachable"]
              .allSatisfy { ProjectStatus.health(read("{\"state\":\"\($0)\"}")) != nil })
    check("a component whose state nobody recognises is dropped, not drawn",
          ProjectStatus.healthComponents(read(#"""
          {"state":"ok","components":[{"label":"a","state":"online"},{"label":"b","state":"none"}]}
          """#)).count == 1)

    // One vocabulary for "is it up", because there were two and they disagreed: `linkRow()` has
    // mapped `online` to `ok` since the multi-surface receipt arrived, while the Mac footer asked
    // `state == "ok"` and drew a red dot for a site that was answering.
    expect("an online site is up, on both surfaces",
           ProjectStatus.health(read(#"{"state":"online"}"#))?.visualState, "ok")
    expect("and so is one whose producer spells it ok",
           ProjectStatus.health(read(#"{"state":"ok"}"#))?.visualState, "ok")
    expect("while not deployed stays distinct from an outage",
           ProjectStatus.health(read(#"{"state":"not_deployed"}"#))?.visualState, "down")
    expect("and the Links sheet keeps reading the same property",
           ProjectStatus.health(read(#"{"state":"online","url":"https://example.com/"}"#))?
               .linkRow()?["state"] as? String, "ok")
}

}
