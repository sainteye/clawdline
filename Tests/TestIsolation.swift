import Foundation

// Do this in the test binary itself, not only in `test.sh`. Contributors sometimes run the
// already-compiled binary while narrowing a failure; without an in-process boundary that reads
// the installed app's real push subscriptions and schedule fixtures can notify a real phone.
let isolatedTestStoreDirectory = FileManager.default.temporaryDirectory
    .appendingPathComponent("clawdline-test-store-\(UUID().uuidString)", isDirectory: true)

// The same boundary for the drop cache, and the same reasoning one step further on: this suite
// writes *files* there — `Drop.paths` writes one for a pasted image, `RemoteServer.pieces` one per
// upload — and every one of those writes calls `Drop.prune(keeping:)`, which deletes the oldest
// entries by name until the count is back under the limit. Unisolated, a run does not merely leave
// litter in somebody's cache; once that cache is near its limit the run deletes pictures the person
// dropped into the bar. Measured when this was added: 37 files against a limit of 40.
let isolatedTestDropsDirectory = isolatedTestStoreDirectory
    .appendingPathComponent("drops", isDirectory: true)

// Session-message image artifacts have their own deleting cache. Keep the suite's lifecycle and
// pruning checks inside the same disposable boundary rather than letting a test expire somebody's
// live attachment.
let isolatedTestSessionImagesDirectory = isolatedTestStoreDirectory
    .appendingPathComponent("session-images", isDirectory: true)

/// The drop cache the app itself would use — what this suite must never write into.
///
/// Spelled out rather than read from `Drop.directory`, which is the thing under test: asking the
/// code under test where the live directory is would make the assertion agree with any answer.
let liveDropDirectory = (FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first
    ?? FileManager.default.temporaryDirectory)
    .appendingPathComponent("dev.sainteye.clawdline/drops", isDirectory: true)

// ``SessionNaming`` reads this Mac's live sessions to name them — a `ps`, whatever is in
// `~/.claude/sessions/`, and somebody's real transcripts. A suite must not: a `TargetSession`
// invented here says `tty: "/dev/ttys004"`, and on a developer's machine that is a real terminal
// with a real conversation in it, so the same assertion would read `Ledger reader layer` on one
// machine and `⌘1-1` on the next. Silent by default; the groups that are about naming install
// their own answer and put this one back.
let noSessionNames: (TargetSession) -> SessionNaming.Name = { _ in .none }

func configureTestIsolation() {
    // **Line buffering, so that where the output stops is where the suite stopped.**
    //
    // `print` is block buffered at this process's `fstat(1).st_blksize`, and under `test.sh` that is
    // **always 16384**, because `test.sh` pipes this binary into `tee`: fd 1 is a FIFO no matter what
    // the person running it redirects, since their redirection lands on `tee`'s stdout and never
    // reaches here. Measured three ways in one run — this binary straight to a file gives 4096, and
    // through the pipeline gives 16384 whether the outer stdout is a file or a terminal.
    //
    // **That sentence has been wrong three times before this one**: 4096, then "the page size, 16384",
    // then "16 KB down a pipe and 4 KB to a file, so the two are not comparable". The last of those
    // was the worst, because it was written *while correcting* the one before it and it made people
    // treat two byte counts as measured on different rulers when they are measured on the same one.
    //
    // A trap writes to stderr and dies without flushing stdout, so that block of ticks never reaches
    // the log. A review measured one green run of this binary at 394 lines, 390 of them group ticks,
    // 24,158 bytes, 61.9 bytes each — a lost 16 KB is about 264 of those, and the last 16 KB of that
    // run held 234. Either way it is most of the suite. (Those four figures are that review's, not
    // this comment's: counting ticks across all of `test.sh` instead of this binary alone gives a
    // different and equally true number, which is the sort of thing that made the earlier versions of
    // this paragraph wrong.)
    //
    // That is what made a whole day of reasoning from truncation points worthless — the window was
    // always wider than the distance anybody was arguing about. With `_IOLBF` the last tick in the log
    // is the last group that finished, and since `group()` prints after its body, the group after it
    // is the one that died. When one crashes anyway, the located answer is not in the log at all:
    // `ls -t ~/Library/Logs/DiagnosticReports/ | grep clawdline-tests` has the faulting thread and the
    // backtrace, and has had all along.
    setvbuf(stdout, nil, _IOLBF, 0)
    try! FileManager.default.createDirectory(at: isolatedTestStoreDirectory,
                                             withIntermediateDirectories: true)
    guard setenv("CLAWDLINE_REMOTE_DIR", isolatedTestStoreDirectory.path, 1) == 0 else {
        fatalError("could not isolate the test remote store")
    }
    guard setenv("CLAWDLINE_DROPS_DIR", isolatedTestDropsDirectory.path, 1) == 0 else {
        fatalError("could not isolate the test drop cache")
    }
    guard setenv("CLAWDLINE_SESSION_IMAGE_DIR", isolatedTestSessionImagesDirectory.path, 1) == 0 else {
        fatalError("could not isolate the test session-image cache")
    }
    // The suite exercises persistence and deliberate corruption repeatedly. Keep those fixtures in
    // the same process-owned boundary as RemoteAuth and WebPush.
    Orchestrator.storeURLOverrideForTesting = isolatedTestStoreDirectory
        .appendingPathComponent("orchestrator.json")
    SessionNaming.lookForTesting = noSessionNames

    // The usage ledger is a durable store of its own, deliberately outside the remote directory —
    // see `UsageLedger.storeURL`. Point it inside the same process-owned boundary, so no test run
    // can write a row into the ledger somebody is going to quote a month's total from.
    UsageLedger.storeURLOverrideForTesting = isolatedTestStoreDirectory
        .appendingPathComponent("usage.sqlite3")
}

func runTestIsolationTests() {
group("the test binary isolates push state even when it is launched directly") {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
    process.arguments = Array(CommandLine.arguments.dropFirst())
    var environment = ProcessInfo.processInfo.environment
    environment.removeValue(forKey: "CLAWDLINE_REMOTE_DIR")
    environment["CLAWDLINE_TEST_REMOTE_DIRECTORY_PROBE"] = "1"
    process.environment = environment
    let pipe = Pipe()
    process.standardOutput = pipe
    process.standardError = pipe
    do {
        try process.run()
        let output = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        process.waitQuietly()
        let live = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline", isDirectory: true).path
        check("a direct test process never opens the live remote and push store",
              process.terminationStatus == 0 && output != live
                && output.hasPrefix(FileManager.default.temporaryDirectory.path), output)
    } catch {
        check("the direct-process isolation probe starts", false, "\(error)")
    }
}

group("the test binary isolates the drop cache even when it is launched directly") {
    // The drop cache is the one shared resource here whose ordinary use *deletes*: every write
    // prunes. So this is not only about litter — an unisolated run throws away the person's own
    // dropped images once their cache is near the limit.
    check("this process resolves the drop cache inside its own boundary",
          Drop.directory.path == isolatedTestDropsDirectory.path
            && Drop.directory.path != liveDropDirectory.path, Drop.directory.path)

    let process = Process()
    process.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
    process.arguments = Array(CommandLine.arguments.dropFirst())
    var environment = ProcessInfo.processInfo.environment
    environment.removeValue(forKey: "CLAWDLINE_DROPS_DIR")
    environment["CLAWDLINE_TEST_DROPS_DIRECTORY_PROBE"] = "1"
    process.environment = environment
    let pipe = Pipe()
    process.standardOutput = pipe
    process.standardError = pipe
    do {
        try process.run()
        let output = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        process.waitQuietly()
        check("a direct test process never writes into the live drop cache",
              process.terminationStatus == 0 && output != liveDropDirectory.path
                && output.hasPrefix(FileManager.default.temporaryDirectory.path), output)
    } catch {
        check("the direct-process drop-cache isolation probe starts", false, "\(error)")
    }
}
}
