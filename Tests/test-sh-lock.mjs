// `./test.sh` has to be the only run compiling on this machine, and it has to decide that itself.
//
// **This runs test.sh's own lines.** The lock block is lifted out of `test.sh` between two literal
// marker comments and executed with a cheap stand-in where the compile and the suite would be —
// the same shape as `Tests/test-sh-streaming.mjs`, and for the same reason: a harness that writes
// out its own copy of the construct it wants to prove is testing bash, which needs no test here.
//
// Nothing below compiles Swift, runs the real suite, or touches `/tmp/clawdline-suite.lock`. Every
// path and every process name the block looks at is an environment variable, and this file points
// all of them at a scratch directory and at a process name it made up, so a real compile happening
// on this Mac while the tests run can neither be seen by them nor disturbed by them.
//
// **What it is guarding.** On 2026-09-03 four `swift-frontend` processes held 46 / 45 / 27 / 8 GB
// on a 24 GB Mac and Jetsam force-rebooted it, twice. The lock is what stops two sessions doing
// that to each other, so the checks here are mostly about the ways a lock quietly stops working:
// a release that frees somebody else's lock, a holder that dies without releasing, a sentinel
// process that outlives the work it stood for, and two waiters that both decide the same lock is
// abandoned.

import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, chmodSync, existsSync, rmSync, mkdirSync, cpSync, copyFileSync } from "node:fs";
import { cpus, tmpdir } from "node:os";
import { join } from "node:path";

let failures = 0;
let checks = 0;
const check = (what, ok) => {
    checks += 1;
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};
// A guard that cannot find what it guards must say so and stop, not report a clean scan of nothing.
// The count printed on the way out is the other half of that: it is what lets the next reader tell
// "clean" from "never looked".
const stop = (why) => {
    console.log(`  ✗ ${why}`);
    console.log(`test.sh suite lock: stopped after ${checks + 1} checks — ${why}`);
    process.exit(1);
};

const script = readFileSync(new URL("../test.sh", import.meta.url), "utf8");
const lines = script.split("\n");
const buildScript = readFileSync(new URL("../build.sh", import.meta.url), "utf8");

// **One record, two writers.** `test.sh` and `build.sh` both write `<lock>/holder.txt` and both
// read each other's. It was three while the broker lease wrote it too, and the list below is the
// one all three agreed on, so removing that writer costs the contract nothing. They used to write
// three different subsets: seventeen fields, eleven and eleven, eight in common, and the four the
// shell's compare-and-swap needs — `token`, `owner_pid`, `owner_started`, `heartbeat_deadline` —
// written by nobody else, so against a lock either of the others wrote that compare was `"" = ""`
// and always true. Order is the contract too: a reader diffing two records by eye should not have
// to sort them first.
const RECORD_CONTRACT = [
    "holder", "pid", "owner_pid", "owner_started", "token", "phase", "phase_since",
    "heartbeat", "heartbeat_deadline", "started", "renewed", "tree", "log", "done_flag",
    "work", "last_compiling", "compilers", "note",
];

const OPEN = "# >>> clawdline suite lock >>>";
const CLOSE = "# <<< clawdline suite lock <<<";
const opens = lines.filter((l) => l === OPEN).length;
const closes = lines.filter((l) => l === CLOSE).length;
const first = lines.indexOf(OPEN);
const last = lines.indexOf(CLOSE);
check("test.sh carries the two suite-lock markers, once each", opens === 1 && closes === 1 && last > first);
if (opens !== 1 || closes !== 1 || last <= first) {
    stop(`cannot find exactly one ${OPEN} … ${CLOSE} pair in test.sh, so there is nothing to run`);
}
const block = lines.slice(first, last + 1).join("\n");
const code = block.split("\n").filter((l) => !/^\s*#/.test(l));
if (code.join("").trim().length === 0) stop("the marked block contains no code at all");

// The compile ceiling is a second marked block, next to the invocation it feeds rather than inside
// the lock, because it landed on its own so another line could have it without waiting for this.
const CEIL_OPEN = "# >>> clawdline compile ceiling >>>";
const CEIL_CLOSE = "# <<< clawdline compile ceiling <<<";
const ceilFirst = lines.indexOf(CEIL_OPEN);
const ceilLast = lines.indexOf(CEIL_CLOSE);
check("test.sh carries the compile-ceiling markers too",
      ceilFirst >= 0 && ceilLast > ceilFirst
        && lines.filter((l) => l === CEIL_OPEN).length === 1
        && lines.filter((l) => l === CEIL_CLOSE).length === 1);
if (ceilFirst < 0 || ceilLast <= ceilFirst) {
    stop(`cannot find exactly one ${CEIL_OPEN} … ${CEIL_CLOSE} pair in test.sh`);
}
const ceiling = lines.slice(ceilFirst, ceilLast + 1).join("\n");

// ---------------------------------------------------------------------------------------------
// What the block promises about itself, read off test.sh rather than off this file's memory of it.

// The default is asserted as a default rather than as a spelling: the assignment now falls through
// `build.sh`'s old name before it reaches the literal, so pinning the two as adjacent text would
// have gone red for the right code. Scenario 25e runs both blocks with nothing set and compares
// what they actually resolve to, which is the question — one lock, or two.
check("the lock is the machine-wide directory, by default",
      /CLAWDLINE_SUITE_LOCK_DIR="\$\{CLAWDLINE_SUITE_LOCK_DIR:-[^\n]*\/tmp\/clawdline-suite\.lock/.test(block));
check("and staleness looks for the real compiler by default, not for whatever these tests use",
      /CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN:-swift-frontend/.test(block));
// `mkdir` is the acquisition because it is atomic; `rename` is the takeover for the same reason.
check("acquisition is mkdir and takeover is rename, the two atomic operations",
      /mkdir "\$lock" 2>\/dev\/null/.test(block) && /mv "\$lock" "\$stale"/.test(block));
const busy = /CLAWDLINE_SUITE_LOCK_BUSY=(\d+)/.exec(block);
check("a busy lock exits on a number test.sh does not already use",
      busy !== null && ![0, 1, 2, 125, 126, 127].includes(Number(busy[1])) && Number(busy[1]) < 128);
// Nothing in this feature may signal another session's process. That check is in scenario 17 at
// the bottom of this file rather than here, because it needs a scanner and a set of positive
// controls: it used to be `/^\s*kill\b/`, one spelling of one command at one position, and a
// mutation that `pkill`ed every `swift-frontend` on the machine passed it.
// `pgrep -f` matches anything with the word anywhere in its arguments. Measured on this machine in
// one moment: `pgrep -f swift-frontend` answered 3 where an exact name match answered 1, the two
// extras being somebody's sampler and a `/usr/bin/time`. A probe that counts those refuses to hand
// the lock on for as long as anybody is watching a compile.
check("the compiler probe matches the process's own name, not anything with the word in its arguments",
      /pgrep -x /.test(block) && !/pgrep\s+(-\w+\s+)*-f\b/.test(block));

// Bash keeps one EXIT trap and installing a second silently replaces the first. That is not a
// hypothetical here: test.sh used to install `trap 'rm -rf "$STORE"' EXIT` after this block, which
// would have thrown the lock's release away on every run.
const installs = lines.filter((l) => /^\s*trap\s+[^-]/.test(l) && /\bEXIT\b/.test(l) && !/^\s*#/.test(l));
check("test.sh installs exactly one EXIT trap, and it is the composed one",
      installs.length === 1 && /clawdline_suite_exit_cleanup/.test(installs[0]));
check("and that one trap still removes $STORE, so composing it did not drop the store cleanup",
      /rm -rf "\$STORE"/.test(block));
check("the lock is taken before the compile it exists to serialise",
      last < lines.findIndex((l) => /^swiftc \\$/.test(l)));
check("and test.sh runs this file",
      /^node Tests\/test-sh-lock\.mjs$/m.test(script));

// The identity comparison, which is where a locale reaches in. Measured on this Mac: the same
// process reads `Thu Sep  3 02:18:04 2026` under LC_ALL=C and `四  9/ 3 02:18:04 2026` under
// zh_TW.UTF-8. Both sides of the comparison have to come out of one formatter.
const lstartReads = code.filter((l) => /ps -o lstart=/.test(l));
check("every reading of a process start time is pinned to one formatter",
      lstartReads.length > 0 && lstartReads.every((l) => /LC_ALL=C ps -o lstart=/.test(l)));

// The whole line is normalised and compared; nothing counts fields. Extract the normaliser by
// content so that changing it to something that indexes fields changes what is checked below.
const normaliser = /awk 'NR == 1 \{ ([^']*) \}'/.exec(block);
check("the start time is normalised whole rather than picked apart into fields",
      normaliser !== null && /\$1 = \$1/.test(normaliser[1]) && !/\$[2-9]/.test(normaliser[1]));

// ---------------------------------------------------------------------------------------------
// Running it.

const dir = mkdtempSync(join(tmpdir(), "clawdline-suite-lock-"));
// A process name this machine cannot already be running, so "no compiler anywhere" is a fact about
// these tests and not about whether somebody is compiling right now. `pgrep -x` matches the
// executable name, which macOS truncates, so it stays short.
const pattern = `lockprobe${process.pid % 100000}`;
check("the stand-in compiler name is short enough for the name pgrep -x actually matches",
      pattern.length <= 15);

const lockDir = join(dir, "suite.lock");
const events = join(dir, "events");
const baseEnv = {
    ...process.env,
    CLAWDLINE_SUITE_LOCK_DIR: lockDir,
    CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN: pattern,
    CLAWDLINE_SUITE_LOCK_RENEW_SECONDS: "1",
    CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS: "2",
    CLAWDLINE_SUITE_LOCK_POLL_SECONDS: "0.2",
    CLAWDLINE_SUITE_LOCK_NOTICE_SECONDS: "1",
    CLAWDLINE_SUITE_LOCK_WAIT_SECONDS: "10",
    // Set so the harnesses do not run `git` once per process against the shared checkout. One
    // scenario below deliberately leaves them unset and checks what the block fills in.
    CLAWDLINE_SUITE_LOCK_HOLDER: "a test harness",
    CLAWDLINE_SUITE_LOCK_TREE: "no tree",
    CLAWDLINE_SUITE_LOCK_NOTE: "written by Tests/test-sh-lock.mjs",
    LOCK_EVENTS: events,
    LOCK_PATTERN: pattern,
    LOCK_SCRATCH: dir,
    TMPDIR: dir,
};

// **Nothing ambient reaches a harness, and one variable was getting through.** Everything the two
// blocks read is pinned above except `CLAWDLINE_SUITE_JOBS` — which the caller's own environment
// can carry, and which this machine's dispatch policy tells a session to set:
// `CLAWDLINE_SUITE_JOBS=1 ./test.sh` is the supported way to ask for fewer compiler jobs. That
// value reached scenario 12's control, the one that asserts what happens when *no* ceiling is set,
// and made the whole suite red on a tree where nothing about the ceiling had changed. Every
// scenario that wants a value passes it explicitly, so the ambient one is removed rather than
// overridden: an override would be one more value to keep in step with the block's default.
delete baseEnv.CLAWDLINE_SUITE_JOBS;

// The block acquires as its last act, which is what a caller wants and what most scenarios below
// want too. One scenario needs the functions without the acquisition — the outer shell of a `nohup`
// that never held the lock — so the block is cut at the trap. Cutting is checked rather than
// assumed: an unchecked slice that silently matched nothing would leave every assertion below
// testing something else.
const acquireAt = block.indexOf("\ntrap clawdline_suite_exit_cleanup EXIT\n");
check("the block installs its trap and then acquires, so the functions can be taken without the acquisition",
      acquireAt > 0);
const functionsOnly = acquireAt > 0 ? block.slice(0, acquireAt) : block;
check("and the cut really removed the acquisition",
      !/clawdline_acquire_suite_lock \|\| exit/.test(functionsOnly));

const shell = (name, prelude, body) => {
    const p = join(dir, name);
    writeFileSync(p, ["#!/bin/bash", "set -euo pipefail", prelude, body, ""].join("\n"));
    chmodSync(p, 0o755);
    return p;
};
const holderScript = (name, body) => shell(name, block, body);
const run = (file, env = {}, args = []) => {
    const r = spawnSync("/bin/bash", [file, ...args], {
        encoding: "utf8",
        env: { ...baseEnv, ...env },
        cwd: dir,
    });
    return { code: r.status, out: r.stdout ?? "", err: r.stderr ?? "", all: (r.stdout ?? "") + (r.stderr ?? "") };
};

// A log a scenario never produced is a red result, not a stack trace: a broken implementation has
// to reach the count at the bottom of this file, or the reader cannot tell "clean" from "crashed".
const readIf = (path) => (existsSync(path) ? readFileSync(path, "utf8") : "");

// Two runs are inside together if one enters before the other leaves. Written as a state machine
// over the event log rather than as a timestamp comparison, so it cannot be fooled by a clock.
const overlapped = (log) => {
    let inside = 0;
    let both = false;
    for (const line of log.split("\n")) {
        if (/^enter /.test(line)) { inside += 1; if (inside > 1) both = true; }
        if (/^leave /.test(line)) inside -= 1;
    }
    return both;
};

// A stand-in for the guarded section: cheap, but long enough that two of them started together
// would visibly be inside at the same time.
const CRITICAL = [
    'echo "enter $LOCK_ID" >> "$LOCK_EVENTS"',
    'sleep "${LOCK_HOLD:-0.4}"',
    'echo "leave $LOCK_ID" >> "$LOCK_EVENTS"',
].join("\n");

// Shared bash used by the orchestrators: a lock crafted by hand, so a scenario can describe a
// holder that this machine cannot easily produce on demand — one whose recorded pid is a sentinel.
const PRELUDE = [
    // A lock written by hand, so a scenario can describe a holder this machine cannot easily be
    // asked to produce — one whose recorded pid is a sentinel, or whose beat is an hour old. The
    // third argument is the beat's modification time as an epoch, which is where liveness is read.
    'craft_lock() { # dir pid beat-epoch token deadline',
    '  mkdir -p "$1"',
    '  touch "$1/beat"',
    '  touch -t "$(date -r "$3" +%Y%m%d%H%M.%S)" "$1/beat"',
    '  {',
    '    printf \'holder=%s\\n\' "a crafted holder"',
    '    printf \'pid=%s\\n\' "$2"',
    '    printf \'phase=%s\\n\' "compiling"',
    '    printf \'heartbeat=%s\\n\' "$1/beat"',
    '    printf \'started=%s\\n\' "$(date \'+%Y-%m-%d %H:%M:%S\')"',
    '    printf \'tree=%s\\n\' "no tree"',
    '    printf \'log=%s\\n\' "/dev/null"',
    '    printf \'done_flag=%s\\n\' "$1/done"',
    '    printf \'owner_pid=%s\\n\' "$2"',
    '    printf \'token=%s\\n\' "$4"',
    '    printf \'owner_started=%s\\n\' "$(LC_ALL=C ps -o lstart= -p "$2" 2>/dev/null | awk \'NR == 1 { $1 = $1; print; exit }\')"',
    '    printf \'heartbeat_deadline=%s\\n\' "$5"',
    '    printf \'phase_since=%s\\n\' "$3"',
    '    printf \'last_compiling=%s\\n\' "$3"',
    '    printf \'renewed=%s\\n\' "$3"',
    '    printf \'work=%s\\n\' ""',
    '    printf \'compilers=%s\\n\' "none"',
    '    printf \'note=%s\\n\' "crafted by Tests/test-sh-lock.mjs"',
    '  } > "$1/holder.txt.tmp"',
    '  mv "$1/holder.txt.tmp" "$1/holder.txt"',
    '}',
    'dead_pid() { local d; sleep 0 & d=$!; wait "$d" 2>/dev/null || true; printf %s "$d"; }',
    // Every orchestrator runs under `set -e`, and a waiter that is *supposed* to exit 75 would take
    // the orchestrator with it. The status is the measurement, so it is captured rather than
    // allowed to be an error.
    'capture() { local log=$1 label=$2 st=0; shift 2; "$@" > "$log" 2>&1 || st=$?; echo "$label=$st" >> "$log"; }',
    // A process whose *name* is the pattern the block probes for. `exec -a` sets it without
    // needing a compiler or a copied binary — measured here: a copy of /bin/sleep will not run at
    // all on this machine, its signature having been broken by the copy, while `exec -a` gives
    // `pgrep -x` exactly the name to match.
    'fake_compiler() { bash -c "exec -a $LOCK_PATTERN /bin/sleep ${1:-30}" >/dev/null 2>&1 & printf %s "$!"; }',
    // And a process that only *mentions* the name in its arguments — a sampler watching a compile,
    // a `/usr/bin/time` wrapping one. Two commands rather than one so bash does not replace itself
    // with the sleep and drop the mention.
    'decoy_compiler() { bash -c "sleep ${1:-30}; : $LOCK_PATTERN" >/dev/null 2>&1 & printf %s "$!"; }',
].join("\n");

try {
    // -----------------------------------------------------------------------------------------
    // 1. It takes the lock, records who has it, and gives it back — together with the $STORE the
    //    composed trap must not have lost.
    const one = holderScript("s1.sh", [
        'STORE="$LOCK_SCRATCH/store"',
        'mkdir -p "$STORE"',
        'cp "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" "$LOCK_SCRATCH/record.txt"',
        '[ -f "$CLAWDLINE_SUITE_LOCK_DIR/beat" ] && touch "$LOCK_SCRATCH/beat-was-there"',
        'grep -q "^phase=idle-holding$" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" && touch "$LOCK_SCRATCH/phase-was-vocabulary"',
    ].join("\n"));
    const r1 = run(one);
    const record = readIf(join(dir, "record.txt"));
    check("a run acquires the lock and says so", r1.code === 0 && /is this run's/.test(r1.all));
    check("and its phase is one of the three words every reader of the record knows",
          existsSync(join(dir, "phase-was-vocabulary")));
    // The ratified record — **the whole of it, and the same list for every writer**. It is
    // declared once at the top of this file, checked against `test.sh` here and against `build.sh`
    // in scenario 18. It was checked against `OrchestratorLease.encode` too until that writer was
    // removed, and the list did not move.
    for (const field of RECORD_CONTRACT) {
        check(`the record carries ${field}`, new RegExp(`^${field}=`, "m").test(record));
    }
    check("and carries nothing outside the contract, so a fourth field cannot appear unreviewed",
          record.split("\n").filter(Boolean).every((l) => RECORD_CONTRACT.includes(l.split("=")[0])));
    check("the heartbeat names a file inside the lock, which is where liveness is read from",
          /^heartbeat=.*\/beat$/m.test(record) && existsSync(join(dir, "beat-was-there")));
    check("and the run gives the lock back when it ends", !existsSync(lockDir) && /released/.test(r1.all));
    check("the beat goes with the lock, so no heartbeat is left pointing at work that ended",
          !existsSync(join(lockDir, "beat")));
    check("and the store the old second trap used to remove is still removed",
          !existsSync(join(dir, "store")));
    // **`work=` names what is working, and a reading is not a worker.**
    // `working=$(clawdline_suite_lock_working_pids …)` forked a subshell whose parent is the very
    // pid the probe asks `pgrep -P` about, so the list — and `pid=`, which is its first entry —
    // named a shell that existed only for the length of the reading. This holder forks nothing of
    // its own, so the only honest answer is an empty list and a `pid` that falls back to the run.
    check("a holder doing nothing records no worker rather than the shell that took the reading",
          /^work=$/m.test(record) && new RegExp(`^pid=${/^owner_pid=(\d+)$/m.exec(record)?.[1]}$`, "m")
            .test(record));

    // -----------------------------------------------------------------------------------------
    // 2. The first `nohup` mistake. An outer shell writes `trap … EXIT`, backgrounds the suite and
    //    returns; the trap fires there and then, while the run it started is still compiling.
    //    Ownership is what makes that release a no-op.
    const holder2 = holderScript("s2-holder.sh", [
        'touch "$LOCK_SCRATCH/held2"',
        'while [ ! -f "$LOCK_SCRATCH/go2" ]; do sleep 0.05; done',
    ].join("\n"));
    const outer2 = shell("s2-outer.sh", functionsOnly, [
        // Exactly the mistake: a trap in the shell that only *starts* the run.
        'trap clawdline_release_suite_lock EXIT',
        'exit 0',
    ].join("\n"));
    const orch2 = shell("s2.sh", PRELUDE, [
        `"${holder2}" > "$LOCK_SCRATCH/holder2.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held2" ]; do sleep 0.05; done',
        'cp "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" "$LOCK_SCRATCH/before2.txt"',
        `capture "$LOCK_SCRATCH/outer2.log" outer "${outer2}"`,
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "lock=present" || echo "lock=gone"',
        'cp "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" "$LOCK_SCRATCH/after2.txt" 2>/dev/null || true',
        'touch "$LOCK_SCRATCH/go2"',
        'wait "$h"',
    ].join("\n"));
    const r2 = run(orch2);
    const outer2log = readIf(join(dir, "outer2.log"));
    const before2 = readIf(join(dir, "before2.txt"));
    const after2 = readIf(join(dir, "after2.txt"));
    check("an outer shell's EXIT trap does not free the lock the run it started is holding",
          /lock=present/.test(r2.all) && before2 === after2);
    check("and it says whose lock it left alone rather than failing silently",
          /left alone/.test(outer2log) && /outer=0/.test(outer2log));

    // -----------------------------------------------------------------------------------------
    // 2b. And the pid is not on its own enough to prove ownership, which is what the token is for.
    //     A pid is reused within hours on a busy machine, so a run whose number matches the record
    //     is still not necessarily the run that wrote it. Written as: the record changes hands
    //     while this run holds it, keeping the same pid — nothing else about the release differs.
    //     Without the token in the comparison this scenario deletes a lock that is somebody else's.
    rmSync(lockDir, { recursive: true, force: true });
    const reused = holderScript("s2b.sh", [
        'sed "s/^token=.*/token=somebody-elses-token/" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" > "$LOCK_SCRATCH/rewritten.txt"',
        'mv "$LOCK_SCRATCH/rewritten.txt" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt"',
    ].join("\n"));
    const r2b = run(reused);
    check("a run whose pid still matches the record but whose token does not leaves the lock alone",
          existsSync(lockDir) && /changed hands/.test(r2b.all) && !/released/.test(r2b.all));
    rmSync(lockDir, { recursive: true, force: true });

    // -----------------------------------------------------------------------------------------
    // 3. The second `nohup` mistake. The trap is in the inner shell, and the inner shell is killed
    //    with SIGKILL — a harness timeout, a Ctrl-C that reached the group, the OOM killer. No trap
    //    runs and the lock stays. What rescues it is renewal stopping, plus nothing compiling.
    rmSync(lockDir, { recursive: true, force: true });
    const holder3 = holderScript("s3-holder.sh", [
        'touch "$LOCK_SCRATCH/held3"',
        'sleep 300',
    ].join("\n"));
    const waiter3 = holderScript("s3-waiter.sh", 'echo "ENTERED"');
    const orch3 = shell("s3.sh", PRELUDE, [
        `"${holder3}" > "$LOCK_SCRATCH/holder3.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held3" ]; do sleep 0.05; done',
        'kill -9 "$h"; wait "$h" 2>/dev/null || true',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "survived=yes" || echo "survived=no"',
        // Before the renewal deadline has passed there is nothing to take over yet.
        `capture "$LOCK_SCRATCH/early3.log" early env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=1 "${waiter3}"`,
        'sleep 2.5',
        `capture "$LOCK_SCRATCH/late3.log" late "${waiter3}"`,
    ].join("\n"));
    const r3 = run(orch3);
    const early3 = readIf(join(dir, "early3.log"));
    const late3 = readIf(join(dir, "late3.log"));
    check("a run killed with SIGKILL leaves its lock behind — no trap of any kind runs",
          /survived=yes/.test(r3.all));
    check("and until its renewal deadline passes, nobody may have it",
          /early=75/.test(early3) && !/ENTERED/.test(early3));
    check("once it has stopped renewing and nothing is compiling, the next run takes it over",
          /took over/.test(late3) && /ENTERED/.test(late3) && /late=0/.test(late3));

    // -----------------------------------------------------------------------------------------
    // 4. The backstop, which is never waived. A dead holder whose compile is still running as an
    //    orphan is not stale: the memory is still being spent.
    rmSync(lockDir, { recursive: true, force: true });
    const waiter4 = holderScript("s4-waiter.sh", 'echo "ENTERED"');
    const orch4 = shell("s4.sh", PRELUDE, [
        'c=$(fake_compiler 30)',
        'sleep 0.3',
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" crafted-token-4 2',
        `capture "$LOCK_SCRATCH/w4.log" waiter env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=2 "${waiter4}"`,
        'echo "orphan_pid=$c"',
        'ps -p "$c" -o pid= >/dev/null 2>&1 && echo "orphan=alive" || echo "orphan=gone"',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "lock=present" || echo "lock=gone"',
        'kill "$c" 2>/dev/null || true; wait "$c" 2>/dev/null || true',
    ].join("\n"));
    const r4 = run(orch4);
    const w4 = readIf(join(dir, "w4.log"));
    const orphanPid = /^orphan_pid=(\d+)$/m.exec(r4.all);
    check("a dead holder with a live compiler is not stale, and the run refuses rather than proceeding",
          /waiter=75/.test(w4) && !/ENTERED/.test(w4) && /lock=present/.test(r4.all));
    check("the refusal names both the run to ask and what it has working right now",
          /run pid \d+, working pid/.test(w4));
    check("the refusal names the orphan by pid, so a person can deal with it",
          orphanPid !== null && w4.includes(orphanPid[1]));
    check("and nothing was killed to make room",
          /orphan=alive/.test(r4.all));

    // -----------------------------------------------------------------------------------------
    // 4b. The other half of the backstop: it has to be a compiler, not a mention of one. Somebody
    //     watching a compile — a sampler, a `/usr/bin/time` — carries the word in its arguments,
    //     and a probe that counted those would keep every waiter out for as long as anyone was
    //     looking.
    rmSync(lockDir, { recursive: true, force: true });
    const waiter4b = holderScript("s4b-waiter.sh", 'echo "ENTERED"');
    const orch4b = shell("s4b.sh", PRELUDE, [
        'd=$(decoy_compiler 30)',
        'sleep 0.3',
        'echo "decoy_by_arguments=$(pgrep -f "$LOCK_PATTERN" 2>/dev/null | wc -l | tr -d " " || true)"',
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" crafted-token-4b 2',
        `capture "$LOCK_SCRATCH/w4b.log" waiter env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=3 "${waiter4b}"`,
        'kill "$d" 2>/dev/null || true; wait "$d" 2>/dev/null || true',
    ].join("\n"));
    const r4b = run(orch4b);
    const w4b = readIf(join(dir, "w4b.log"));
    check("a process that only mentions the compiler in its arguments is visible to a looser probe",
          /decoy_by_arguments=[1-9]/.test(r4b.all));
    check("and this probe does not count it, so an abandoned lock is still reclaimable",
          /waiter=0/.test(w4b) && /took over/.test(w4b) && /ENTERED/.test(w4b));

    // -----------------------------------------------------------------------------------------
    // 5. The sentinel. This is the live instance the rule was rewritten for: a holder recorded
    //    `pid=72929`, which was a `sleep 14400` adopted by launchd — a process that outlives the
    //    work and cannot renew. Under a pid-existence rule this lock is a four-hour roadblock.
    rmSync(lockDir, { recursive: true, force: true });
    const waiter5 = holderScript("s5-waiter.sh", 'echo "ENTERED"');
    const orch5 = shell("s5.sh", PRELUDE, [
        'sleep 300 & sentinel=$!',
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$sentinel" "$(( $(date +%s) - 600 ))" crafted-token-5 2',
        'ps -p "$sentinel" -o pid= >/dev/null 2>&1 && echo "sentinel=alive" || echo "sentinel=gone"',
        `capture "$LOCK_SCRATCH/w5.log" waiter env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=3 "${waiter5}"`,
        'ps -p "$sentinel" -o pid= >/dev/null 2>&1 && echo "after=alive" || echo "after=gone"',
        'kill "$sentinel" 2>/dev/null || true; wait "$sentinel" 2>/dev/null || true',
    ].join("\n"));
    const r5 = run(orch5);
    const w5 = readIf(join(dir, "w5.log"));
    check("a holder whose recorded pid is a live sentinel, with the work behind it gone, is reclaimable",
          /sentinel=alive/.test(r5.all) && /took over/.test(w5) && /ENTERED/.test(w5) && /waiter=0/.test(w5));
    check("and taking the lock over did not touch the sentinel",
          /after=alive/.test(r5.all));

    // -----------------------------------------------------------------------------------------
    // 6. The mirror image, and the reason the backstop cannot be the whole rule: a holder that is
    //    alive and renewing between two compiles of one study has no compiler running at that
    //    moment. "No compiler means stale" would walk in mid-study.
    rmSync(lockDir, { recursive: true, force: true });
    const holder6 = holderScript("s6-holder.sh", [
        // The 02:45 picture exactly: renewing honestly, nothing compiling, and the only thing that
        // tells this apart from a holder that finished and forgot to release is what it says it is
        // doing.
        'clawdline_suite_lock_phase idle-holding',
        'touch "$LOCK_SCRATCH/held6"',
        'while [ ! -f "$LOCK_SCRATCH/go6" ]; do sleep 0.05; done',
    ].join("\n"));
    const waiter6 = holderScript("s6-waiter.sh", 'echo "ENTERED"');
    const orch6 = shell("s6.sh", PRELUDE, [
        `"${holder6}" > "$LOCK_SCRATCH/holder6.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held6" ]; do sleep 0.05; done',
        'pgrep -x "$LOCK_PATTERN" >/dev/null 2>&1 && echo "compilers=some" || echo "compilers=none"',
        `capture "$LOCK_SCRATCH/w6.log" waiter env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=4 "${waiter6}"`,
        'touch "$LOCK_SCRATCH/go6"',
        'wait "$h"',
    ].join("\n"));
    const r6 = run(orch6);
    const w6 = readIf(join(dir, "w6.log"));
    check("a holder that is alive and renewing keeps the lock even with no compiler running",
          /compilers=none/.test(r6.all) && /waiter=75/.test(w6) && !/ENTERED/.test(w6) && !/took over/.test(w6));
    check("and it waited long enough for a renewal deadline to have expired had renewal not happened",
          /waiting 3s/.test(w6));
    // A phase that needs no compiler decides nothing — and the waiter has to say it out loud, or the
    // person waiting is back to guessing from the machine.
    check("the refusal says what the holder is doing, and for how long",
          /phase idle-holding for \d+s/.test(w6));
    check("and says when anything last actually compiled, which is the question a waiter has",
          /(last compiling \d+s|nothing has compiled under this lock yet)/.test(w6));
    check("but a phase that needs no compiler is still not a reason to take the lock",
          !/took over/.test(w6));

    // -----------------------------------------------------------------------------------------
    // 7. Evidence that is missing or ambiguous blocks. It never reads as "dead" — that is the
    //    difference between a lock that is careful and one that hands the machine to two compilers
    //    because it could not read a file.
    rmSync(lockDir, { recursive: true, force: true });
    const waiter7 = holderScript("s7-waiter.sh", 'echo "ENTERED"');
    const orch7 = shell("s7.sh", PRELUDE, [
        'mkdir -p "$CLAWDLINE_SUITE_LOCK_DIR"',
        `capture "$LOCK_SCRATCH/w7a.log" a env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=1 "${waiter7}"`,
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) + 600 ))" crafted-token-7 2',
        `capture "$LOCK_SCRATCH/w7b.log" b env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=1 "${waiter7}"`,
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"',
    ].join("\n"));
    run(orch7);
    const w7a = readIf(join(dir, "w7a.log"));
    const w7b = readIf(join(dir, "w7b.log"));
    check("a lock with no readable heartbeat blocks and says the evidence is unknown",
          /a=75/.test(w7a) && !/ENTERED/.test(w7a) && /unknown/.test(w7a));
    check("a heartbeat from the future is ambiguous, and ambiguous blocks too",
          /b=75/.test(w7b) && !/ENTERED/.test(w7b) && /clocks disagree/.test(w7b));

    // 7c. And the one way out of `unknown`, which is positive evidence rather than an exception.
    //     Acquiring is `mkdir`, then a few forks, then the first record; a run killed in that
    //     window leaves a directory with no record and no beat. `unknown` blocked on it correctly
    //     and *nothing could ever clear it* — only `stale` reaches the takeover, and a record that
    //     will never be written can never become stale — so one ordinary Ctrl-C turned the
    //     machine's compile slot into a permanent roadblock that the note inside it told the next
    //     person not to remove. All four conditions have to hold: never a record, never a beat,
    //     older than a whole deadline, and no compiler anywhere.
    rmSync(lockDir, { recursive: true, force: true });
    const waiter7c = holderScript("s7c-waiter.sh", 'echo "ENTERED"');
    const orch7c = shell("s7c.sh", PRELUDE, [
        'mkdir -p "$CLAWDLINE_SUITE_LOCK_DIR"',
        // Aged past its own 2s deadline, which one being acquired right now cannot be.
        'sleep 2.5',
        // With a compiler running it is still nobody's to take.
        'c=$(fake_compiler 20); sleep 0.3',
        `capture "$LOCK_SCRATCH/w7c-busy.log" busy env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=1 "${waiter7c}"`,
        'kill "$c" 2>/dev/null || true; wait "$c" 2>/dev/null || true',
        'sleep 0.3',
        `capture "$LOCK_SCRATCH/w7c.log" clear env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=2 "${waiter7c}"`,
        // And a directory that has a beat had a record once and something removed it, which is a
        // different and unexplained event. That still blocks.
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"; mkdir -p "$CLAWDLINE_SUITE_LOCK_DIR"',
        'touch "$CLAWDLINE_SUITE_LOCK_DIR/beat"',
        'touch -t "$(date -r "$(( $(date +%s) - 600 ))" +%Y%m%d%H%M.%S)" "$CLAWDLINE_SUITE_LOCK_DIR/beat"',
        'sleep 2.5',
        `capture "$LOCK_SCRATCH/w7d.log" beaten env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=1 "${waiter7c}"`,
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"',
    ].join("\n"));
    run(orch7c);
    const w7cBusy = readIf(join(dir, "w7c-busy.log"));
    const w7c = readIf(join(dir, "w7c.log"));
    const w7d = readIf(join(dir, "w7d.log"));
    check("a directory that has never held a record, once it is older than a whole deadline and "
          + "nothing is compiling, is an abandoned acquisition and is reclaimable",
          /clear=0/.test(w7c) && /took over/.test(w7c) && /ENTERED/.test(w7c)
            && /never held a record/.test(w7c));
    check("but the physical backstop is not waived for it either",
          /busy=75/.test(w7cBusy) && !/ENTERED/.test(w7cBusy));
    check("and a directory that has a beat but no record had one once, which stays unknown",
          /beaten=75/.test(w7d) && !/ENTERED/.test(w7d) && /unknown/.test(w7d));

    // -----------------------------------------------------------------------------------------
    // 8. The done flag: a positive signal, read only in that direction. Present means the work is
    //    over, so with the backstop still satisfied the next run does not wait out a deadline
    //    nobody is renewing against. Its absence is what every scenario above relies on proving
    //    nothing.
    rmSync(lockDir, { recursive: true, force: true });
    const waiter8 = holderScript("s8-waiter.sh", 'echo "ENTERED"');
    const orch8 = shell("s8.sh", PRELUDE, [
        'sleep 300 & sentinel=$!',
        // A heartbeat fresh enough that the deadline alone would refuse.
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$sentinel" "$(date +%s)" crafted-token-8 600',
        'touch "$CLAWDLINE_SUITE_LOCK_DIR/done"',
        `capture "$LOCK_SCRATCH/w8a.log" a env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=2 "${waiter8}"`,
        // And with a compiler running, the same flag admits nobody.
        'c=$(fake_compiler 20); sleep 0.3',
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$sentinel" "$(date +%s)" crafted-token-8b 600',
        'touch "$CLAWDLINE_SUITE_LOCK_DIR/done"',
        `capture "$LOCK_SCRATCH/w8b.log" b env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=2 "${waiter8}"`,
        'kill "$c" 2>/dev/null || true; wait "$c" 2>/dev/null || true',
        'kill "$sentinel" 2>/dev/null || true; wait "$sentinel" 2>/dev/null || true',
    ].join("\n"));
    run(orch8);
    const w8a = readIf(join(dir, "w8a.log"));
    const w8b = readIf(join(dir, "w8b.log"));
    check("a holder that marked its work finished hands the lock on without waiting out a deadline",
          /a=0/.test(w8a) && /took over/.test(w8a) && /marked its work finished/.test(w8a));
    check("but the same flag admits nobody while a compiler is still running",
          /b=75/.test(w8b) && !/ENTERED/.test(w8b));

    // -----------------------------------------------------------------------------------------
    // 9. Serialisation, red and green in one place. The unlocked pair is the control: without it,
    //    a green "they did not overlap" would prove only that the two runs never happened to
    //    collide.
    rmSync(lockDir, { recursive: true, force: true });
    rmSync(events, { force: true });
    const bare = shell("s9-bare.sh", "", CRITICAL);
    const orch9a = shell("s9a.sh", "", [
        `LOCK_ID=a "${bare}" & LOCK_ID=b "${bare}" &`,
        'wait',
    ].join("\n"));
    run(orch9a, { LOCK_HOLD: "0.6" });
    const unlocked = readIf(events);
    check("two runs with no lock between them are inside the guarded section together",
          overlapped(unlocked) && (unlocked.match(/^enter /gm) || []).length === 2);

    rmSync(events, { force: true });
    const locked = holderScript("s9-locked.sh", CRITICAL);
    const orch9b = shell("s9b.sh", "", [
        `LOCK_ID=a "${locked}" > "$LOCK_SCRATCH/l9a.log" 2>&1 & LOCK_ID=b "${locked}" > "$LOCK_SCRATCH/l9b.log" 2>&1 &`,
        'wait',
    ].join("\n"));
    run(orch9b, { LOCK_HOLD: "0.6" });
    const serialised = readIf(events);
    check("the same two runs behind the lock go one at a time, and both still run",
          !overlapped(serialised) && (serialised.match(/^enter /gm) || []).length === 2
            && (serialised.match(/^leave /gm) || []).length === 2);

    // -----------------------------------------------------------------------------------------
    // 10. Two waiters that decide the same lock is abandoned must not both proceed. `rename` picks
    //     one winner among simultaneous judgements; the gate directory is what keeps a judgement
    //     from outliving a whole takeover and being applied to the lock that replaced it.
    rmSync(lockDir, { recursive: true, force: true });
    rmSync(events, { force: true });
    const racer = holderScript("s10-racer.sh", CRITICAL);
    const orch10 = shell("s10.sh", PRELUDE, [
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" crafted-token-10 2',
        'for i in 1 2 3 4 5 6; do',
        `  LOCK_ID="$i" "${racer}" > "$LOCK_SCRATCH/race$i.log" 2>&1 &`,
        'done',
        'wait',
    ].join("\n"));
    run(orch10, { LOCK_HOLD: "0.25", CLAWDLINE_SUITE_LOCK_WAIT_SECONDS: "20" });
    const raceLogs = [1, 2, 3, 4, 5, 6].map((i) => readIf(join(dir, `race${i}.log`)));
    const takeovers = raceLogs.filter((l) => /took over/.test(l)).length;
    const raced = readIf(events);
    check("exactly one of six waiters takes the abandoned lock over; the rest queue behind it",
          takeovers === 1);
    check("and all six run, one at a time",
          !overlapped(raced) && (raced.match(/^enter /gm) || []).length === 6);

    // -----------------------------------------------------------------------------------------
    // 11. The identity comparison, against this machine's real `ps`. Remove the `LC_ALL=C` from the
    //     block's reader and this goes red here, because this Mac runs zh_TW.UTF-8.
    const locale11 = shell("s11.sh", functionsOnly, [
        'a=$(LC_ALL=C clawdline_suite_lock_pid_identity $$)',
        'b=$(LC_ALL=zh_TW.UTF-8 clawdline_suite_lock_pid_identity $$)',
        'raw=$(LC_ALL=zh_TW.UTF-8 ps -o lstart= -p $$ | awk \'NR == 1 { $1 = $1; print; exit }\')',
        'echo "pinned_equal=$([ "$a" = "$b" ] && echo yes || echo no)"',
        'echo "unpinned_differs=$([ "$a" = "$raw" ] && echo no || echo yes)"',
        'echo "pinned=[$a]"',
        'echo "unpinned=[$raw]"',
        'echo "unknown=$(clawdline_suite_lock_pid_identity 999999999)"',
    ].join("\n"));
    const r11 = run(locale11);
    check("a process's identity reads the same whatever locale the caller is in",
          /pinned_equal=yes/.test(r11.all));
    check("and this machine really would have disagreed without the pin, so the check is not vacuous",
          /unpinned_differs=yes/.test(r11.all));
    check("a pid that does not exist reads `unknown`, not an empty string that matches everything",
          /unknown=unknown/.test(r11.all));

    // The two zh_TW shapes, held as data. `ps -o lstart=` renders what `date +%c` renders: five
    // whitespace-separated tokens on 2026-09-03 and four on 2026-08-31, while LC_ALL=C renders five
    // on both days. That is why nothing counts fields — a reader that took the fifth token would
    // lose the year for three weeks of every month.
    const shapes = shell("s11b.sh", "", [
        'd3=$(LC_ALL=C date -j -f "%Y-%m-%d %H:%M:%S" "2026-09-03 02:09:02" +%s)',
        'd31=$(LC_ALL=C date -j -f "%Y-%m-%d %H:%M:%S" "2026-08-31 02:09:02" +%s)',
        'for e in "$d3" "$d31"; do',
        '  for l in C zh_TW.UTF-8; do',
        '    rendered=$(LC_ALL="$l" date -r "$e" +%c)',
        `    normalised=$(printf '%s\\n' "$rendered" | awk 'NR == 1 { $1 = $1; print; exit }')`,
        '    printf "%s|%s|%s|%s\\n" "$e" "$l" "$(printf %s "$normalised" | awk "{print NF}")" "$normalised"',
        '  done',
        'done',
    ].join("\n"));
    const r11b = run(shapes);
    const rows = r11b.out.trim().split("\n").filter(Boolean).map((l) => l.split("|"));
    const cell = (epochIndex, loc) => rows.filter((r) => r[1] === loc)[epochIndex];
    check("the reading is taken from four renderings, two days by two locales", rows.length === 4);
    if (rows.length === 4) {
        const c3 = cell(0, "C"), c31 = cell(1, "C"), z3 = cell(0, "zh_TW.UTF-8"), z31 = cell(1, "zh_TW.UTF-8");
        check("the pinned formatter renders the same shape on both days", c3[2] === c31[2]);
        check("the machine's own locale does not, which is the whole reason for the pin",
              z3[2] !== z31[2] || z3[3] !== c3[3]);
        check("and normalising keeps every rendering whole rather than dropping a field",
              [c3, c31, z3, z31].every((r) => r[3].includes("2026")));
    }

    // -----------------------------------------------------------------------------------------
    // 12. The compile-job ceiling, and where the number came from. Unset, the compile line below
    //     the block is byte-identical to what it has always been.
    rmSync(lockDir, { recursive: true, force: true });
    // The second line reads the ceiling back **from a child process**, and the assertion on it has
    // been inverted since it was written. It used to require the number to arrive there, because a
    // typecheck in a child was meant to share it. That sharing is what put eight `swift-frontend`
    // processes above the machine lock, so the requirement now runs the other way: **nothing.** A
    // child that inherits this width is a child compiling at it outside the lock.
    //
    // Read in this same shell the two cases are indistinguishable — a variable merely set looks
    // exactly like one exported — which is why the probe spawns `bash -c`. That was found by
    // mutation: the first draft read it in-process and the mutation deleting `export` passed every
    // check in this file.
    const jobs = shell("s12.sh", ceiling,
                       'echo "flags=[${clawdline_suite_jobs_flags[@]+${clawdline_suite_jobs_flags[@]}}]"\n'
                       + 'bash -c \'echo "exported=[${CLAWDLINE_COMPILE_JOBS:-}]"\'');
    // A `sysctl` of this test's own, so the derivation can be driven against a machine that says
    // one core, a machine that says sixty-four, a machine whose answer is not a number, and a
    // machine that will not answer at all. Without these the only reading available is this Mac's,
    // and a rule measured on one machine is exactly what the `hw.ncpu` term exists to avoid.
    const fakeSysctlDir = (name, body) => {
        const d = join(dir, `sysctl-${name}`);
        mkdirSync(d, { recursive: true });
        const f = join(d, "sysctl");
        writeFileSync(f, `#!/bin/bash\n${body}\n`);
        chmodSync(f, 0o755);
        return { PATH: `${d}:${process.env.PATH}` };
    };
    const cores = (name, body) => run(jobs, fakeSysctlDir(name, body));
    const r12a = run(jobs);
    const r12b = run(jobs, { CLAWDLINE_SUITE_JOBS: "1" });
    const r12c = run(jobs, { CLAWDLINE_SUITE_JOBS: "lots" });
    const refused = ["0", "00", "007", "lots", "-1", " 1", "1 ", "1.5", "+2"];
    const refusals = refused.map((v) => [v, run(jobs, { CLAWDLINE_SUITE_JOBS: v })]);
    // The wording moved once and the assertion follows it. `main`'s `f2d5abf8` replaced "so the
    // swift driver picks" after the default was measured — it is one job, not an unknown — so this
    // asserts what the line has to carry rather than the clause it carried first: that no ceiling
    // was set, and which variable would set one. A run that printed nothing, or printed a number it
    // had invented, still fails.
    const derived = Math.min(8, cpus().length);
    check("with no ceiling set, the default is derived from this machine and the run says so",
          r12a.code === 0 && r12a.all.includes(`flags=[-j ${derived}]`)
          && /min\(8, hw\.ncpu\)/.test(r12a.all) && /CLAWDLINE_SUITE_JOBS unset/.test(r12a.all));
    check("the settled ceiling reaches no child process, derived or explicit",
          r12a.all.includes("exported=[]")
          && run(jobs, { CLAWDLINE_SUITE_JOBS: "3" }).all.includes("exported=[]"));
    // **Where the block sits is the rule, not the habit.** A ceiling settled above
    // `clawdline_acquire_suite_lock` is one that the code above the lock can read, and code above
    // the lock is rationed by nothing. `b8dfd0ff` moved this block to the top of the script so a
    // typecheck outside the lock could share the number; that took the typecheck from 34 s to 7 s
    // and put eight compilers beside whoever was holding the lock — caught in the act at 11:29:06
    // on 2026-09-03, eight frontends with the lock free, under another line's landing run.
    // `build.sh` never had the problem — its own ceiling block already sits below
    // `clawdline_lease_acquire` — so this asserts the shape on both scripts, not only on the one
    // that broke.
    const acquireLine = lines.findIndex((l) => /^clawdline_acquire_suite_lock \|\| exit/.test(l));
    check("test.sh settles its compile ceiling only after the machine lock is acquired",
          acquireLine >= 0 && ceilFirst > acquireLine);
    const buildLines2 = buildScript.split("\n");
    const buildAcquire = buildLines2.findIndex((l) => /^clawdline_lease_acquire \|\| exit/.test(l));
    const buildCeil = buildLines2.indexOf(CEIL_OPEN);
    check("and build.sh settles its own the same way",
          buildAcquire >= 0 && buildCeil > buildAcquire);
    // The cap and the floor, each driven from the reading rather than from this machine's own.
    check("a machine with more cores than the cap is capped at eight",
          cores("many", "echo 64").all.includes("flags=[-j 8]"));
    check("a machine with one core gets one job, not the cap",
          cores("one", "echo 1").all.includes("flags=[-j 1]"));
    check("a core count that is not a number reads as one job rather than as no ceiling",
          cores("garbage", "echo many").all.includes("flags=[-j 1]"));
    check("a sysctl that will not answer reads as one job too",
          cores("broken", "exit 1").all.includes("flags=[-j 1]"));
    // The control for the four above: they would all pass against a block that ignored `sysctl`
    // entirely and always printed 1, on any machine whose cap happens to be 1. This is the reading
    // that separates "the derivation works" from "the derivation is dead code".
    check("and the fake sysctl is really the one being read",
          cores("seven", "echo 7").all.includes("flags=[-j 7]"));

    // **The same rule lives in `build.sh`, and this is what keeps the two copies one rule.**
    // Not a textual comparison: the blocks print different sentences and always will, so what is
    // compared is what they answer. Both are lifted and driven against the same five stand-in
    // `sysctl` readings, and a build.sh that capped at twelve, or floored at zero, or stopped
    // reading `sysctl` at all, disagrees here on the reading that separates them.
    const buildLines = buildScript.split("\n");
    const bOpen = buildLines.indexOf(CEIL_OPEN);
    const bClose = buildLines.indexOf(CEIL_CLOSE);
    check("build.sh carries the compile-ceiling markers too, once each",
          bOpen >= 0 && bClose > bOpen
            && buildLines.filter((l) => l === CEIL_OPEN).length === 1
            && buildLines.filter((l) => l === CEIL_CLOSE).length === 1);
    if (bOpen >= 0 && bClose > bOpen) {
        const buildCeiling = buildLines.slice(bOpen, bClose + 1).join("\n");
        const buildJobs = shell("s12b.sh", buildCeiling,
                                'echo "flags=[${compile_jobs[@]+${compile_jobs[@]}}]"');
        const bCores = (name, body) => run(buildJobs, fakeSysctlDir(`b-${name}`, body));
        for (const [name, body, want] of [["many", "echo 64", 8], ["one", "echo 1", 1],
                                          ["garbage", "echo many", 1], ["broken", "exit 1", 1],
                                          ["seven", "echo 7", 7]]) {
            check(`build.sh's ceiling answers ${want} where test.sh's does, for ${name}`,
                  bCores(name, body).all.includes(`flags=[-j ${want}]`));
        }
        check("and an explicit ceiling reaches build.sh's compiler unchanged",
              run(buildJobs, { CLAWDLINE_SUITE_JOBS: "3" }).all.includes("flags=[-j 3]"));
        check("while one that is not a positive whole number is refused there too",
              run(buildJobs, { CLAWDLINE_SUITE_JOBS: "00" }).code === 2);
    }
    check("a ceiling from the environment reaches the compiler, and the run says where it came from",
          r12b.code === 0 && /flags=\[-j 1\]/.test(r12b.all) && /from CLAWDLINE_SUITE_JOBS/.test(r12b.all));
    check("a ceiling that is not a number is refused rather than quietly ignored",
          r12c.code === 2 && !/flags=/.test(r12c.all));
    // The table, not one example. `00` is all digits and is not a positive whole number; it reached
    // `swiftc` as `-j 00` in the first version of this guard, which had been proved red against
    // `0`, `abc`, `-1` and `" 1"` and never against `00`. The one nobody proved is the one that
    // was open.
    for (const [value, result] of refusals) {
        check(`a ceiling of ${JSON.stringify(value)} is refused, not passed on`,
              result.code === 2 && !/flags=/.test(result.all));
    }
    check("the ceiling reaches the invocation itself, not just a variable nobody reads",
          /^\s+\$\{clawdline_suite_jobs_flags\[@\]\+"\$\{clawdline_suite_jobs_flags\[@\]\}"\} \\$/m.test(script));

    // -----------------------------------------------------------------------------------------
    // 13. The defaults the harnesses above override, exercised once so they are not dead code.
    rmSync(lockDir, { recursive: true, force: true });
    const defaults = holderScript("s13.sh", 'cp "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" "$LOCK_SCRATCH/record13.txt"');
    const r13 = run(defaults, {
        CLAWDLINE_SUITE_LOCK_HOLDER: "",
        CLAWDLINE_SUITE_LOCK_TREE: "",
        CLAWDLINE_SUITE_LOCK_NOTE: "",
    });
    const record13 = readIf(join(dir, "record13.txt"));
    check("with nothing told to it, the record still names a holder a person could go and ask",
          r13.code === 0 && /^holder=\S/m.test(record13) && !/^holder=$/m.test(record13));
    check("and records the tree it is verifying and how the lock is handed on",
          /^tree=\S/m.test(record13) && /^note=.*heartbeat/m.test(record13));

    // -----------------------------------------------------------------------------------------
    // 14. **The holder's proof of life survives a machine it cannot read.**
    //
    //     This is the reproduction from the independent review, kept as a test because it is the
    //     whole feature failing: three of the renewer's four conditions used to be *readings*,
    //     each `|| exit 0` on one unretried sample, so a `ps` broken for a single tick ended the
    //     beat permanently while the run was still compiling — and a second run walked in a
    //     deadline later. The rule the readers follow ("missing or ambiguous evidence is unknown
    //     and blocks; it never reads as dead") was inverted in the writer, where the same
    //     ambiguity read as "I am no longer the holder".
    //
    //     Both halves are checked: the beat keeps moving across the outage, and — the part that
    //     matters to the machine — a second run is still refused.
    rmSync(lockDir, { recursive: true, force: true });
    const shimDir = join(dir, "shim");
    mkdirSync(shimDir, { recursive: true });
    // A `ps` that fails while a flag file is there and is the real one otherwise. Ahead of
    // /bin/ps on PATH, so it reaches every `ps` the block runs without the block knowing.
    writeFileSync(join(shimDir, "ps"), [
        "#!/bin/bash",
        'if [ -f "$LOCK_SCRATCH/ps-broken" ]; then exit 1; fi',
        'exec /bin/ps "$@"',
        "",
    ].join("\n"));
    chmodSync(join(shimDir, "ps"), 0o755);
    const holder14 = holderScript("s14-holder.sh", [
        'touch "$LOCK_SCRATCH/held14"',
        'while [ ! -f "$LOCK_SCRATCH/go14" ]; do sleep 0.05; done',
    ].join("\n"));
    const waiter14 = holderScript("s14-waiter.sh", 'echo "ENTERED"');
    const orch14 = shell("s14.sh", PRELUDE, [
        `PATH="${shimDir}:$PATH" "${holder14}" > "$LOCK_SCRATCH/holder14.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held14" ]; do sleep 0.05; done',
        // One clean tick first, so what follows is measured against a beat that was moving.
        'sleep 1.2',
        'echo "beat_before=$(stat -f %m "$CLAWDLINE_SUITE_LOCK_DIR/beat")"',
        'touch "$LOCK_SCRATCH/ps-broken"',
        // Two ticks with `ps` answering nothing at all, which is one more than it took to kill
        // the old renewer, and long enough for the 2s deadline to expire if the beat stopped.
        'sleep 2.6',
        `capture "$LOCK_SCRATCH/w14.log" during env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=1 "${waiter14}"`,
        'rm -f "$LOCK_SCRATCH/ps-broken"',
        'sleep 2.6',
        'echo "beat_after=$(stat -f %m "$CLAWDLINE_SUITE_LOCK_DIR/beat")"',
        // The renewal loop by its own recorded number, not `pgrep -P` on the holder: the holder
        // also forks a `sleep` every 50 ms, so "it has a child" answers a different question.
        'echo "renewer_alive=$(ps -p "$(cat "$CLAWDLINE_SUITE_LOCK_DIR/.renewer" 2>/dev/null || echo 0)" -o pid= >/dev/null 2>&1 && echo yes || echo no)"',
        'touch "$LOCK_SCRATCH/go14"',
        'wait "$h"',
    ].join("\n"));
    const r14 = run(orch14);
    const w14 = readIf(join(dir, "w14.log"));
    const beatBefore = /beat_before=(\d+)/.exec(r14.all);
    const beatAfter = /beat_after=(\d+)/.exec(r14.all);
    check("a probe that could not answer costs the renewer a tick, not the lock",
          beatBefore !== null && beatAfter !== null
            && Number(beatAfter[1]) > Number(beatBefore[1]));
    check("the renewal loop is still running after the machine became readable again",
          /renewer_alive=yes/.test(r14.all));
    check("and no second run got inside the guarded section while the probe was failing",
          /during=75/.test(w14) && !/ENTERED/.test(w14) && !/took over/.test(w14));
    check("the run says out loud that a tick's evidence was unreadable, rather than going quiet",
          /renewal evidence unreadable/.test(readIf(join(dir, "holder14.log"))));

    // 14b. The same rule against the other failure the review reproduced: the record cannot be
    //      written. The old loop exited on it, the beat stopped, and 60s later somebody else was
    //      compiling. A write this loop could not do is a tick lost, not a lock given up.
    rmSync(lockDir, { recursive: true, force: true });
    const holder14b = holderScript("s14b-holder.sh", [
        'touch "$LOCK_SCRATCH/held14b"',
        'while [ ! -f "$LOCK_SCRATCH/go14b" ]; do sleep 0.05; done',
    ].join("\n"));
    const waiter14b = holderScript("s14b-waiter.sh", 'echo "ENTERED"');
    const orch14b = shell("s14b.sh", PRELUDE, [
        `"${holder14b}" > "$LOCK_SCRATCH/holder14b.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held14b" ]; do sleep 0.05; done',
        'sleep 1.2',
        // The lock directory becomes unwritable, so no temp record can be created inside it —
        // exactly what a full disk or a permissions accident does. The beat file itself already
        // exists and stays writable, which is how liveness survives.
        'chmod a-w "$CLAWDLINE_SUITE_LOCK_DIR"',
        'sleep 2.6',
        `capture "$LOCK_SCRATCH/w14b.log" during env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=2 "${waiter14b}"`,
        'chmod u+w "$CLAWDLINE_SUITE_LOCK_DIR"',
        // The renewal loop by its own recorded number, not `pgrep -P` on the holder: the holder
        // also forks a `sleep` every 50 ms, so "it has a child" answers a different question.
        'echo "renewer_alive=$(ps -p "$(cat "$CLAWDLINE_SUITE_LOCK_DIR/.renewer" 2>/dev/null || echo 0)" -o pid= >/dev/null 2>&1 && echo yes || echo no)"',
        'touch "$LOCK_SCRATCH/go14b"',
        'wait "$h"',
    ].join("\n"));
    const r14b = run(orch14b);
    const w14b = readIf(join(dir, "w14b.log"));
    check("a record the holder could not write does not hand its lock to the next run",
          /during=75/.test(w14b) && !/ENTERED/.test(w14b) && !/took over/.test(w14b));
    check("the renewal loop survives a failed write and keeps proving the run is there",
          /renewer_alive=yes/.test(r14b.all));
    check("and it says how much of its proof of life is missing rather than failing silently",
          /could not refresh/.test(readIf(join(dir, "holder14b.log"))));

    // 14c. The other direction, which is the one that must still work: positive evidence that
    //      this run no longer owns the lock stops the renewer at once, and names why.
    rmSync(lockDir, { recursive: true, force: true });
    const holder14c = holderScript("s14c-holder.sh", [
        'touch "$LOCK_SCRATCH/held14c"',
        'while [ ! -f "$LOCK_SCRATCH/go14c" ]; do sleep 0.05; done',
    ].join("\n"));
    const orch14c = shell("s14c.sh", PRELUDE, [
        `"${holder14c}" > "$LOCK_SCRATCH/holder14c.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held14c" ]; do sleep 0.05; done',
        // Somebody else's record, in place, with a token that is not this run's.
        'sed "s/^token=.*/token=somebody-elses-token/" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" > "$LOCK_SCRATCH/r14c.txt"',
        'mv "$LOCK_SCRATCH/r14c.txt" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt"',
        'sleep 2.6',
        // The renewal loop by its own recorded number, not `pgrep -P` on the holder: the holder
        // also forks a `sleep` every 50 ms, so "it has a child" answers a different question.
        'echo "renewer_alive=$(ps -p "$(cat "$CLAWDLINE_SUITE_LOCK_DIR/.renewer" 2>/dev/null || echo 0)" -o pid= >/dev/null 2>&1 && echo yes || echo no)"',
        'touch "$LOCK_SCRATCH/go14c"',
        'wait "$h"',
    ].join("\n"));
    const r14c = run(orch14c);
    check("a record that carries somebody else's token does stop the renewal loop",
          /renewer_alive=no/.test(r14c.all));
    check("and the run's log says which of the four conditions ended it",
          /renewal stopped — .*changed hands/.test(readIf(join(dir, "holder14c.log"))));

    // 14d. The second of the four conditions on its own. Scenario 14 breaks `ps` outright, which
    //      the first condition catches before the second is ever consulted — so this breaks only
    //      `ps -o lstart=`, leaving the process-exists reading working. A start time that cannot
    //      be read is `unknown`; it is not "somebody else has that pid now".
    rmSync(lockDir, { recursive: true, force: true });
    writeFileSync(join(shimDir, "ps"), [
        "#!/bin/bash",
        'if [ -f "$LOCK_SCRATCH/ps-broken" ]; then exit 1; fi',
        // Only the start-time reading, so `ps -p <pid> -p 1` still answers.
        'if [ -f "$LOCK_SCRATCH/lstart-broken" ]; then',
        '  for a in "$@"; do if [ "$a" = "lstart=" ]; then exit 1; fi; done',
        "fi",
        'exec /bin/ps "$@"',
        "",
    ].join("\n"));
    chmodSync(join(shimDir, "ps"), 0o755);
    const holder14d = holderScript("s14d-holder.sh", [
        'touch "$LOCK_SCRATCH/held14d"',
        'while [ ! -f "$LOCK_SCRATCH/go14d" ]; do sleep 0.05; done',
    ].join("\n"));
    const orch14d = shell("s14d.sh", PRELUDE, [
        `PATH="${shimDir}:$PATH" "${holder14d}" > "$LOCK_SCRATCH/holder14d.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held14d" ]; do sleep 0.05; done',
        'sleep 1.2',
        'echo "beat_before=$(stat -f %m "$CLAWDLINE_SUITE_LOCK_DIR/beat")"',
        'touch "$LOCK_SCRATCH/lstart-broken"',
        'sleep 2.6',
        'rm -f "$LOCK_SCRATCH/lstart-broken"',
        'sleep 2.6',
        'echo "beat_after=$(stat -f %m "$CLAWDLINE_SUITE_LOCK_DIR/beat")"',
        'echo "renewer_alive=$(ps -p "$(cat "$CLAWDLINE_SUITE_LOCK_DIR/.renewer" 2>/dev/null || echo 0)" -o pid= >/dev/null 2>&1 && echo yes || echo no)"',
        'touch "$LOCK_SCRATCH/go14d"',
        'wait "$h"',
    ].join("\n"));
    const r14d = run(orch14d);
    const beatBefore14d = /beat_before=(\d+)/.exec(r14d.all);
    const beatAfter14d = /beat_after=(\d+)/.exec(r14d.all);
    check("a start time that could not be read is unknown, not a different process with that pid",
          /renewer_alive=yes/.test(r14d.all) && beatBefore14d !== null && beatAfter14d !== null
            && Number(beatAfter14d[1]) > Number(beatBefore14d[1]));

    // 14e. And the third condition on its own: a record that is there but carries no token. An
    //      empty token is not somebody else's token — it is a record a writer did not finish, or
    //      one written by a program that has not been taught the contract yet.
    rmSync(lockDir, { recursive: true, force: true });
    const holder14e = holderScript("s14e-holder.sh", [
        'touch "$LOCK_SCRATCH/held14e"',
        'while [ ! -f "$LOCK_SCRATCH/go14e" ]; do sleep 0.05; done',
    ].join("\n"));
    const orch14e = shell("s14e.sh", PRELUDE, [
        `"${holder14e}" > "$LOCK_SCRATCH/holder14e.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held14e" ]; do sleep 0.05; done',
        'sleep 1.2',
        'grep -v "^token=" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" > "$LOCK_SCRATCH/r14e.txt"',
        'mv "$LOCK_SCRATCH/r14e.txt" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt"',
        'sleep 2.6',
        'echo "renewer_alive=$(ps -p "$(cat "$CLAWDLINE_SUITE_LOCK_DIR/.renewer" 2>/dev/null || echo 0)" -o pid= >/dev/null 2>&1 && echo yes || echo no)"',
        'grep -q "^token=" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" && echo "token_restored=yes" || echo "token_restored=no"',
        'touch "$LOCK_SCRATCH/go14e"',
        'wait "$h"',
    ].join("\n"));
    const r14e = run(orch14e);
    check("a record with no token in it is unknown, and the holder keeps beating and rewrites it",
          /renewer_alive=yes/.test(r14e.all) && /token_restored=yes/.test(r14e.all));

    // -----------------------------------------------------------------------------------------
    // 15. The three mechanisms of the compare-and-swap, pinned one at a time.
    //
    //     Scenario 10 above passes if *any one* of the gate, the re-read and the token compare
    //     works, so it can testify for none of them: measured, removing the gate alone, the
    //     re-read alone or the token compare alone each left every check in this file green.
    //     These three drive `clawdline_suite_lock_take_over` directly, which is the only way to
    //     hold the window open long enough to describe what each one is for.
    rmSync(lockDir, { recursive: true, force: true });
    const takeover15 = shell("s15.sh", [functionsOnly, PRELUDE].join("\n"), [
        'stale_lock() { craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" "$1" 2; }',
        'fresh_lock() { craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(date +%s)" "$1" 600; }',
        'try() { local st=0; clawdline_suite_lock_take_over "$CLAWDLINE_SUITE_LOCK_DIR" "$2" > /dev/null 2>&1 || st=$?; echo "$1=$st"; }',
        // (a) The token compare: a judgement made against a record that is no longer the one in
        //     place must not be applied to the record that replaced it.
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"; stale_lock token-now',
        'try token_mismatch token-judged-earlier',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "after_token=present" || echo "after_token=gone"',
        // (b) The re-read: the same token, but the holder started beating again between the
        //     judgement and the swap. Nothing stale is left to take.
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"; fresh_lock token-fresh',
        'try reread token-fresh',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "after_reread=present" || echo "after_reread=gone"',
        // (c) The gate: one waiter at a time inside the compare and the swap. With the gate held
        //     by a live process, a second waiter backs out even though its judgement is correct.
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"; stale_lock token-gated',
        'sleep 300 & gate_holder=$!',
        'mkdir "$CLAWDLINE_SUITE_LOCK_DIR.takeover"',
        'printf \'pid=%s\\n\' "$gate_holder" > "$CLAWDLINE_SUITE_LOCK_DIR.takeover/holder.txt"',
        'try gated token-gated',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "after_gate=present" || echo "after_gate=gone"',
        'kill "$gate_holder" 2>/dev/null || true; wait "$gate_holder" 2>/dev/null || true',
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR.takeover" "$CLAWDLINE_SUITE_LOCK_DIR"',
        // And the control: with all three satisfied it does take the lock, so the three above are
        // measuring the mechanisms rather than a function that always refuses.
        'stale_lock token-good',
        'try good token-good',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "after_good=present" || echo "after_good=gone"',
    ].join("\n"));
    const r15 = run(takeover15);
    check("a takeover judged against a record that has since been replaced is refused",
          /token_mismatch=1/.test(r15.all) && /after_token=present/.test(r15.all));
    check("a holder that started beating again between the judgement and the swap keeps its lock",
          /reread=1/.test(r15.all) && /after_reread=present/.test(r15.all));
    check("only one waiter is inside the compare and the swap; a second backs out",
          /gated=1/.test(r15.all) && /after_gate=present/.test(r15.all));
    check("and with all three satisfied the abandoned lock is taken, so the three above are not vacuous",
          /good=0/.test(r15.all) && /after_good=gone/.test(r15.all));

    // -----------------------------------------------------------------------------------------
    // 16. The three call sites the block has but nothing executed. Measured: deleting
    //     `clawdline_confirm_suite_lock || exit $?`, `clawdline_suite_lock_phase compiling` or
    //     `clawdline_suite_lock_work_finished` from test.sh left all 89 checks green — so the
    //     record could say `idle-holding` through a 46 GB compile, which is exactly the ambiguity
    //     the phase field was added to remove.
    //
    //     Each is checked twice: the behaviour, by running it, and the call, by finding it in the
    //     part of `test.sh` the lock block does not cover. Neither alone is enough — a function
    //     nobody calls and a call to a function that does nothing are the same silence.
    rmSync(lockDir, { recursive: true, force: true });
    const confirm16 = holderScript("s16.sh", [
        'st=0; clawdline_confirm_suite_lock || st=$?; echo "mine=$st"',
        'clawdline_suite_lock_phase compiling',
        'grep -q "^phase=compiling$" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" && echo "phase=written"',
        'clawdline_suite_lock_work_finished',
        '[ -f "$CLAWDLINE_SUITE_LOCK_DONE_FLAG" ] && echo "done_flag=written"',
        'sed "s/^token=.*/token=somebody-elses-token/" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" > "$LOCK_SCRATCH/r16.txt"',
        'mv "$LOCK_SCRATCH/r16.txt" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt"',
        'st=0; clawdline_confirm_suite_lock || st=$?; echo "theirs=$st"',
    ].join("\n"));
    const r16 = run(confirm16);
    check("the confirmation between the two halves passes while the lock is still this run's",
          /mine=0/.test(r16.all));
    check("and refuses to start the second expensive thing once the lock has changed hands",
          /theirs=75/.test(r16.all) && /no longer this run's/.test(r16.all));
    check("declaring the compiling phase reaches the record a waiter reads",
          /phase=written/.test(r16.all));
    check("and the positive end-of-work signal writes the flag the next run reads",
          /done_flag=written/.test(r16.all));
    // The calls themselves, in the part of the script the block does not contain. `after` is
    // everything below the lock block, so a call that was deleted cannot be found by a regex that
    // happens to match the function's own definition.
    const after = lines.slice(last + 1).join("\n");
    const compileAt = after.indexOf("\nswiftc \\\n");
    const confirmAt = after.indexOf("\nclawdline_confirm_suite_lock || exit $?\n");
    const binaryAt = after.indexOf('"$BIN" Resources/mascots');
    check("test.sh declares the compiling phase immediately before the compiler it declares it for",
          compileAt > 0 && /clawdline_suite_lock_phase compiling\n$/
              .test(after.slice(0, compileAt + 1)));
    check("and confirms the lock is still its own between the compile and the test binary",
          confirmAt > compileAt && binaryAt > confirmAt);
    check("and says its work is finished, below the run, where a waiter can act on it",
          after.indexOf("\nclawdline_suite_lock_work_finished\n") > binaryAt);

    // -----------------------------------------------------------------------------------------
    // 17. **Nothing here signals a process it did not start**, and the check has to be able to
    //     see every way of saying so.
    //
    //     It was `/^\s*kill\b/`, which is one spelling of one command at one position: `pkill`,
    //     `killall`, `/bin/kill`, `xargs kill` and a `kill` after a `;` all walked past it, and
    //     the mutation that `pkill`s every `swift-frontend` on this machine before taking the
    //     lock left 89 of 89 checks green. This is the one constraint the design calls absolute.
    //
    //     Quoting is why this is a scanner and not a regex: three of the block's own refusals say
    //     "nothing here will kill them" inside a double-quoted string, and a check that counted
    //     those would be measuring its own prose. So quoted text is dropped — except the inside
    //     of a `$( )` or a backtick, where a command can still hide.
    const unquoted = (line) => {
        let out = "";
        let quote = null;      // "'" or '"' while inside one
        let depth = 0;         // how deep inside $( ) or ` `
        for (let i = 0; i < line.length; i += 1) {
            const c = line[i];
            const two = line.slice(i, i + 2);
            if (quote === "'" && c === "'") { quote = null; continue; }
            if (quote === "'") continue;
            if (quote === '"' && c === "\\") { i += 1; continue; }
            if (two === "$(" || (c === "`" && depth === 0 && quote !== "'")) {
                depth += 1; out += " ";
                if (two === "$(") i += 1;
                continue;
            }
            if (depth > 0 && (c === ")" || c === "`")) { depth -= 1; out += " "; continue; }
            if (depth > 0) { out += c; continue; }
            if (quote === '"' && c === '"') { quote = null; continue; }
            if (quote === '"') continue;
            if (c === "'" || c === '"') { quote = c; continue; }
            out += c;
        }
        return out;
    };
    // Any word that ends a process, whatever path or prefix it wears.
    const signals = (line) =>
        /(^|[\s;&|(){}<>])(\/[\w./+-]*\/)?(p?kill(all)?|skill)([\s;&|)<>]|$)/.test(unquoted(line));
    // Positive controls first: a guard nobody has seen refuse is not a guard. Every line here is
    // a real way to end somebody else's compile, and the scanner has to see all of them.
    const mustCatch = [
        '  pkill -x "$CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN"',
        "  killall swift-frontend",
        '  /bin/kill -9 "$pid"',
        "  ps -Ao pid=,comm= | awk '/swift/ {print $1}' | xargs kill",
        '  if [ -n "$p" ]; then kill "$p"; fi',
        "  kill -TERM $pid &",
        '  found=$(pgrep -x swift-frontend) && kill $found',
        '  echo "$(kill -9 $stranger)"',
    ];
    const mustPass = [
        '    clawdline_suite_lock_evidence="an orphaned compile is still spending memory, and nothing here will kill them"',
        '  echo "suite lock: nothing was compiled and nothing was killed." >&2',
        '  found=$(LC_ALL=C pgrep -x "$CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN" 2>/dev/null) || probe_status=$?',
    ];
    check("the no-kill scanner catches every way of ending a process, not one spelling of one",
          mustCatch.every(signals));
    check("and does not catch the block's own prose about what it refuses to do",
          mustPass.every((l) => !signals(l)));
    const signalling = code.filter(signals);
    check("so: the only process the lock block signals is the renewal loop it started itself",
          signalling.length === 1 && /clawdline_suite_lock_renewer/.test(signalling[0]));
    // And it is signalled only while it is still this shell's own job. A renewer that exited is
    // reaped, and its number is reusable within hours on this machine.
    check("and it signals that pid only while bash still lists it as this shell's own job",
          /jobs -p 2>\/dev\/null \| grep -qx "\$clawdline_suite_lock_renewer"/.test(block));

    // -----------------------------------------------------------------------------------------
    // 18. **One record, two writers.** The contract is declared once at the top of this file;
    //     this reads it back out of `build.sh`. It read it out of `OrchestratorLease.encode` too
    //     until that writer was removed, and the list did not move: nothing in the contract was
    //     the broker's alone.
    //
    //     They did not agree. `test.sh` wrote seventeen fields, `build.sh` and
    //     `OrchestratorLease.encode` eleven each, eight in common — and the four the shell's
    //     compare-and-swap depends on (`token`, `owner_pid`, `owner_started`,
    //     `heartbeat_deadline`) were written by nobody else, so against a broker-written or
    //     build-written record that compare was `"" = ""`, always true, with the re-read beside it
    //     carrying the whole swap alone. In the other direction `test.sh` wrote `working=` and the
    //     Swift reader read `work=`, so each side showed an empty working list for the other.
    //
    //     **Read out of the record writer, not out of the whole file.** It used to scan every
    //     `printf 'key=%s\n'` in `build.sh`, which is a claim about the file rather than about the
    //     writer: the takeover gate writes `pid=` into its own little marker file, and that one
    //     line arriving anywhere in the script would have been read as a nineteenth record field
    //     in the wrong place. The function is named and its body is what is scanned.
    const recordWriterAt = buildScript.indexOf("\nclawdline_lease_record() {\n");
    const recordWriter = recordWriterAt < 0 ? ""
        : buildScript.slice(recordWriterAt, buildScript.indexOf("\n}\n", recordWriterAt));
    check("build.sh's record writer was found, so what follows is not scanning an empty string",
          recordWriter.length > 500 && /printf 'holder=/.test(recordWriter));
    const buildKeys = [...recordWriter.matchAll(/^\s*printf '([a-z_]+)=%s\\n'/gm)].map((m) => m[1]);
    check("build.sh writes the record contract, in the contract's order",
          buildKeys.join(",") === RECORD_CONTRACT.join(","));
    // A guard nobody has seen refuse is not a guard: the reading above has to be able to find
    // something, or a rename in build.sh would make this a clean scan of nothing.
    check("the reading actually found a writer, rather than reporting a clean scan of nothing",
          buildKeys.length === RECORD_CONTRACT.length);

    // **`started=` is this process's start, not the moment it asked**, and it used to be the
    // latter. `build.sh` reaches that line only after the Keychain helper has run, so `date +%s`
    // there was minutes late and anybody working out how long the hold had lasted was reading the
    // wait instead of the run. The broker compared the same instant on the wire with a two-second
    // tolerance and answered `waiter_process_gone`; that reader is gone and the record's own field
    // is not, so what is asserted here is the field. Driven, not read: the value must not move as
    // work elapses.
    // **The work goes before the assignment, or this check cannot fail.** The first version put the
    // sleep after it and passed for both the derived value and the `date +%s` it replaced — in a
    // harness with nothing before the assignment the two are the same number, which is exactly the
    // condition that hides the bug in the real script. `build.sh` reaches this line only after the
    // Keychain helper has run, so the stand-in for that work belongs first, and what is asserted is
    // that the value tracks the *shell's* start rather than the moment it was taken.
    const preamble = [
      'set -uo pipefail',
      'CLAWDLINE_HARNESS_T0=$(date +%s)',
      'sleep 3   # stands in for everything build.sh does before it asks for the slot',
      buildScript.split("\n").filter((l) => l.includes("CLAWDLINE_LEASE_OWNER_STARTED=") || l.includes("CLAWDLINE_LEASE_STARTED=")).join("\n"),
      'printf "%s %s" "$CLAWDLINE_LEASE_STARTED" "$CLAWDLINE_HARNESS_T0"',
    ].join("\n");
    const [wireStart, harnessT0] = (spawnSync("/bin/bash", ["-c", preamble], { encoding: "utf8" }).stdout || "0 0")
      .split(" ").map(Number);
    check("the recorded start is the shell's, not the moment three seconds of work later",
          wireStart > 0 && wireStart - harnessT0 <= 1);
    // The textual half names the derivation rather than forbidding a spelling: `date +%s` is still
    // there as the fallback for a `ps` that cannot be read or a stamp that will not parse, and
    // forbidding the string would have made this check fail for the right code. What must hold is
    // that the *first* assignment is the derived one.
    check("and it is derived from the same ps reading the record uses, not taken separately",
          /CLAWDLINE_LEASE_STARTED=\$\(LC_ALL=C date -j -f '%a %b %e %T %Y' "\$CLAWDLINE_LEASE_OWNER_STARTED"/
            .test(buildScript));

    // -----------------------------------------------------------------------------------------
    // 19. `build.sh`'s own writer, run rather than read.
    //
    //     Three findings live here and all three are about the same twenty-second loop:
    //     it rewrote whatever record was in the directory without checking the lock was still
    //     this build's (so a legitimate takeover was followed by the *old* build truncating the
    //     *new* holder's beat and renaming the record after itself, and two runs compiled); it
    //     wrote with a `>` redirect straight onto the live file, so every reader that fails closed
    //     on a partial record saw the lock flicker into `unknown` on a schedule; and its release
    //     compared a `holder=build.sh <user> pid <n>` line that a reused pid can match.
    const buildBlock = buildScript.slice(
        buildScript.indexOf("CLAWDLINE_LEASE_DIR="),
        buildScript.indexOf("APP=\"${CLAWDLINE_APP:"));
    check("the build.sh lease block was found, so what follows is not testing an empty string",
          buildBlock.length > 500 && /clawdline_lease_record\(\)/.test(buildBlock));
    const build19 = shell("s19.sh", buildBlock, [
        'CLAWDLINE_LEASE_STARTED=$(date +%s)',
        'CLAWDLINE_LEASE_ID="build-test-1"',
        'CLAWDLINE_LEASE_DONE="$LOCK_SCRATCH/done19"',
        'mkdir -p "$CLAWDLINE_LEASE_DIR"',
        'st=0; clawdline_lease_record analysing "$$" first || st=$?; echo "first=$st"',
        'cp "$CLAWDLINE_LEASE_DIR/holder.txt" "$LOCK_SCRATCH/build-record.txt"',
        'st=0; clawdline_lease_record compiling 4242 || st=$?; echo "again=$st"',
        // Somebody else legitimately took the lock over while this build was between beats.
        'sed "s/^token=.*/token=somebody-elses-lease/" "$CLAWDLINE_LEASE_DIR/holder.txt" > "$LOCK_SCRATCH/other.txt"',
        'cp "$LOCK_SCRATCH/other.txt" "$CLAWDLINE_LEASE_DIR/holder.txt"',
        'before=$(stat -f %m "$CLAWDLINE_LEASE_DIR/beat")',
        'sleep 1.1',
        'st=0; clawdline_lease_record compiling 4242 || st=$?; echo "stranger=$st"',
        'after=$(stat -f %m "$CLAWDLINE_LEASE_DIR/beat")',
        '[ "$before" = "$after" ] && echo "beat=untouched" || echo "beat=truncated"',
        'grep -q "^token=somebody-elses-lease$" "$CLAWDLINE_LEASE_DIR/holder.txt" && echo "record=intact" || echo "record=overwritten"',
        // And the release leaves a lock whose token is not this build's alone.
        'CLAWDLINE_LEASE_MODE=directory',
        'CLAWDLINE_LEASE_TOKEN="$LOCK_SCRATCH/no-such-token"',
        'clawdline_lease_release',
        '[ -d "$CLAWDLINE_LEASE_DIR" ] && echo "lock=left" || echo "lock=removed"',
        'rm -rf "$CLAWDLINE_LEASE_DIR"',
    ].join("\n"));
    const r19 = run(build19, { CLAWDLINE_SUITE_LOCK_DIR: join(dir, "build.lock") });
    const buildRecord = readIf(join(dir, "build-record.txt"));
    check("build.sh writes a full record on its first write and refreshes it while it holds",
          /first=0/.test(r19.all) && /again=0/.test(r19.all));
    check("and that record carries the four fields the shell's compare-and-swap needs",
          ["token", "owner_pid", "owner_started", "heartbeat_deadline"]
            .every((f) => new RegExp(`^${f}=`, "m").test(buildRecord)));
    check("a build that no longer holds the lock refuses to write the record at all",
          /stranger=1/.test(r19.all) && /record=intact/.test(r19.all));
    check("and does not truncate the live holder's beat on its way past",
          /beat=untouched/.test(r19.all));
    check("and its release leaves a lock whose token is not its own",
          /lock=left/.test(r19.all));
    // The write is atomic: a temp file and a rename, so a reader sees one complete record or the
    // other and never half of either. Every reader of this file fails closed on a partial record,
    // which turned a non-atomic rewrite every twenty seconds into a scheduled `unknown`.
    check("build.sh writes the record through a temporary file and a rename, as test.sh does",
          /temp="\$CLAWDLINE_LEASE_DIR\/\.holder\.\$\$\.\$RANDOM"/.test(buildBlock)
            && /mv "\$temp" "\$CLAWDLINE_LEASE_DIR\/holder\.txt"/.test(buildBlock)
            && !/> "\$CLAWDLINE_LEASE_DIR\/holder\.txt"/.test(buildBlock));

    // -----------------------------------------------------------------------------------------
    // 20. **No lock, no compile**, which is the whole of what the acquire path promises now that
    //     there is no broker in front of it.
    //
    //     The broker era's version of this was a refusal reader: `pressure_refused`, `queue_full`
    //     and a 403 on a stale token arrived as `{"error": {"code": …}}` with no top-level
    //     `state`, all read as the empty string, and fell into the "the broker did not answer"
    //     branch — which took the directory and compiled, jumping a queue up to thirty-two deep.
    //     The arm that implemented "no lease means the compile does not run" was unreachable. That
    //     reader and that queue are gone; the promise they were guarding is not, and it now rests
    //     on two lines that have to stay in this order.
    const acquireCall = buildScript.indexOf("\nclawdline_lease_acquire || exit 1\n");
    const compileCall = buildScript.indexOf("\nswiftc \\\n");
    check("build.sh takes the lock before swiftc, and a failure to take it ends the build",
          acquireCall > 0 && compileCall > acquireCall);

    // -----------------------------------------------------------------------------------------
    // 21. **`compilers=` has three states, and the writer has to be able to write all three.**
    //
    //     The record contract singles this field out: empty means the writer has no answer,
    //     `none` means it probed and the machine was clear, and anything else is the pids it
    //     found. The Swift reader is pinned for the distinction; the writer was not, and it wrote
    //     `${clawdline_suite_lock_compilers:-none}` — a probe that could not answer leaves that
    //     list empty exactly as a clear machine does, so an unreadable `pgrep` was recorded as "I
    //     looked and this Mac was clear". Fail-open, in the one field written to keep those two
    //     apart, in a round whose whole subject is two states sharing one spelling.
    rmSync(lockDir, { recursive: true, force: true });
    const pgrepShim = join(dir, "shim-pgrep");
    mkdirSync(pgrepShim, { recursive: true });
    writeFileSync(join(pgrepShim, "pgrep"), [
        "#!/bin/bash",
        // 2 is what `pgrep` exits when it could not answer at all — a syntax error, a resource it
        // could not reach — as against 1, which is the fact that nothing matched.
        'if [ -f "$LOCK_SCRATCH/pgrep-unreadable" ]; then exit 2; fi',
        'if [ -f "$LOCK_SCRATCH/pgrep-clear" ]; then exit 1; fi',
        'exec /usr/bin/pgrep "$@"',
        "",
    ].join("\n"));
    chmodSync(join(pgrepShim, "pgrep"), 0o755);
    const record21 = shell("s21.sh", [functionsOnly, PRELUDE].join("\n"), [
        'mkdir -p "$CLAWDLINE_SUITE_LOCK_DIR"',
        'clawdline_suite_lock_pid=$$',
        'clawdline_suite_lock_token=token-21',
        'clawdline_suite_lock_started=$(date "+%Y-%m-%d %H:%M:%S")',
        'clawdline_suite_lock_pid_started=$(clawdline_suite_lock_pid_identity "$$")',
        'say() { echo "$1=[$(clawdline_suite_lock_field compilers "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt")]"; }',
        // (a) the probe cannot answer.
        'touch "$LOCK_SCRATCH/pgrep-unreadable"',
        'clawdline_suite_lock_write_record "$CLAWDLINE_SUITE_LOCK_DIR" || true',
        'say unreadable',
        'rm -f "$LOCK_SCRATCH/pgrep-unreadable"',
        // (b) the probe answers, and the machine is clear.
        'touch "$LOCK_SCRATCH/pgrep-clear"',
        'clawdline_suite_lock_write_record "$CLAWDLINE_SUITE_LOCK_DIR" || true',
        'say clear',
        'rm -f "$LOCK_SCRATCH/pgrep-clear"',
        // (c) the probe answers and finds one — the control, so (a) and (b) are not both simply
        //     "this writer never records anything".
        'compiler=$(fake_compiler 30)',
        'sleep 0.3',
        'clawdline_suite_lock_write_record "$CLAWDLINE_SUITE_LOCK_DIR" || true',
        'say found',
        'echo "compiler_was=$compiler"',
        'kill "$compiler" 2>/dev/null || true; wait "$compiler" 2>/dev/null || true',
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"',
    ].join("\n"));
    const r21 = run(record21, { PATH: `${pgrepShim}:${process.env.PATH}` });
    const found21 = /compiler_was=(\d+)/.exec(r21.all);
    check("a compiler probe that could not answer writes no claim about the machine at all",
          /unreadable=\[\]/.test(r21.all));
    check("a probe that answered and found nothing writes `none`, which is a claim",
          /clear=\[none\]/.test(r21.all));
    check("and a probe that found one writes the pid, so the two above are not one silent writer",
          found21 !== null && new RegExp(`found=\\[[^\\]]*${found21[1]}`).test(r21.all));

    // -----------------------------------------------------------------------------------------
    // 22. **The takeover gate reads three answers too.**
    //
    //     It was the one probe left in the block that had two: a `ps` that would not answer and a
    //     process that is gone were the same result, and under the machine state this lock exists
    //     for — load in the sixties, swap full — that reading is least reliable exactly when it
    //     matters. A second waiter could clear a gate whose holder was alive and about to swap.
    //     An *unrecorded* pid keeps its old answer and is checked here too, because folding that
    //     one into `unknown` would be a deadlock: no waiter could ever clear an abandoned gate.
    rmSync(lockDir, { recursive: true, force: true });
    const gateShim = join(dir, "shim-gate");
    mkdirSync(gateShim, { recursive: true });
    writeFileSync(join(gateShim, "ps"), [
        "#!/bin/bash",
        'if [ -f "$LOCK_SCRATCH/gate-ps-broken" ]; then exit 1; fi',
        'exec /bin/ps "$@"',
        "",
    ].join("\n"));
    chmodSync(join(gateShim, "ps"), 0o755);
    const gate22 = shell("s22.sh", [functionsOnly, PRELUDE].join("\n"), [
        'gate="$CLAWDLINE_SUITE_LOCK_DIR.takeover"',
        'stale_lock() { craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" "$1" 2; }',
        'try() { local st=0; clawdline_suite_lock_take_over "$CLAWDLINE_SUITE_LOCK_DIR" "$2" > /dev/null 2>&1 || st=$?; echo "$1=$st"; }',
        // (a) A live gate holder, and a `ps` that will not answer about it. The gate stays.
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR" "$gate"; stale_lock token-22',
        'sleep 300 & gate_holder=$!',
        'mkdir "$gate"',
        'printf \'pid=%s\\n\' "$gate_holder" > "$gate/holder.txt"',
        'touch "$LOCK_SCRATCH/gate-ps-broken"',
        'try unreadable token-22',
        'rm -f "$LOCK_SCRATCH/gate-ps-broken"',
        '[ -d "$gate" ] && echo "after_unreadable=present" || echo "after_unreadable=cleared"',
        'kill "$gate_holder" 2>/dev/null || true; wait "$gate_holder" 2>/dev/null || true',
        // (b) The control: the same shape, a pid this machine says is gone, and `ps` answering.
        //     It is cleared — so (a) is the reading and not a gate that never goes. The gate is
        //     rebuilt rather than reused, so this case cannot be decided by what (a) did.
        'rm -rf "$gate"; mkdir "$gate"',
        'printf \'pid=%s\\n\' "$(dead_pid)" > "$gate/holder.txt"',
        'try dead token-22',
        '[ -d "$gate" ] && echo "after_dead=present" || echo "after_dead=cleared"',
        // (c) And a gate whose record was never written has nobody to ask about, so it is cleared
        //     rather than left to block every takeover this machine will ever attempt.
        'rm -rf "$gate"; mkdir "$gate"',
        'try unowned token-22',
        '[ -d "$gate" ] && echo "after_unowned=present" || echo "after_unowned=cleared"',
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR" "$gate"',
    ].join("\n"));
    const r22 = run(gate22, { PATH: `${gateShim}:${process.env.PATH}` });
    check("a gate whose holder could not be read about is left alone, not cleared as abandoned",
          /unreadable=1/.test(r22.all) && /after_unreadable=present/.test(r22.all));
    check("while a gate whose holder this machine says is gone is still cleared",
          /after_dead=cleared/.test(r22.all));
    check("and a gate that never recorded a holder is cleared too, so no takeover deadlocks on it",
          /after_unowned=cleared/.test(r22.all));

    // -----------------------------------------------------------------------------------------
    // 23. **The run finds out when its proof of life has stopped.**
    //
    //     The renewer says why it stopped on stderr and exits, and nothing raised that to the run:
    //     the run is inside `swiftc` or the test binary and reads nothing. A renewer killed with
    //     the lock still recorded as this run's left the token unchanged and the beat still, and
    //     the run spent the whole test binary in the guarded section proving nothing — after which
    //     another run may legitimately judge the lock stale. Same two runs inside, arrived at from
    //     the other end. So the one confirmation between the two halves asks both questions.
    rmSync(lockDir, { recursive: true, force: true });
    const confirm23 = holderScript("s23.sh", [
        'first="$clawdline_suite_lock_renewer"',
        'echo "first_renewer=$first"',
        // The renewer, and only the renewer: this shell's own background job, by the number it
        // recorded for itself, and only while bash still lists it as this shell's job.
        'if jobs -p 2>/dev/null | grep -qx "$first"; then kill "$first" 2>/dev/null || true; fi',
        'wait "$first" 2>/dev/null || true',
        'sleep 0.2',
        'beat_before=$(stat -f %m "$CLAWDLINE_SUITE_LOCK_DIR/beat")',
        'st=0; clawdline_confirm_suite_lock || st=$?; echo "confirm=$st"',
        'echo "second_renewer=$clawdline_suite_lock_renewer"',
        'sleep 2.2',
        'beat_after=$(stat -f %m "$CLAWDLINE_SUITE_LOCK_DIR/beat")',
        '[ "$beat_after" -gt "$beat_before" ] && echo "beat=moving" || echo "beat=still"',
    ].join("\n"));
    const r23 = run(confirm23);
    const first23 = /first_renewer=(\d+)/.exec(r23.all);
    const second23 = /second_renewer=(\d+)/.exec(r23.all);
    check("a run whose renewer died still holds its lock, and is not made to throw the compile away",
          /confirm=0/.test(r23.all));
    check("it says so rather than going on in silence",
          /renewer .* is gone/.test(r23.all));
    check("and the proof of life is running again before the second expensive thing starts",
          first23 !== null && second23 !== null && first23[1] !== second23[1]
            && /beat=moving/.test(r23.all));
    // And the reason the loop stopped reaches the run, rather than only its stderr. The note is
    // written outside the lock directory on purpose: two of the three stop conditions are that the
    // directory changed hands or is gone.
    rmSync(lockDir, { recursive: true, force: true });
    const note23 = holderScript("s23b.sh", [
        // Somebody else's token, which is one of the conditions that legitimately stops the loop.
        'sed "s/^token=.*/token=somebody-elses-token/" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" > "$LOCK_SCRATCH/r23.txt"',
        'mv "$LOCK_SCRATCH/r23.txt" "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt"',
        'sleep 2.2',
        // **Three answers, not two, and the file's absence is not one of them on its own.**
        // This read the note's existence once, after the confirmation, and called it `read` when
        // the file was gone — which is the same answer a note that was *never written* produces.
        // A renewer that stopped writing the note would have left this check green while the
        // check above it went red, and a check that can pass for the wrong reason looks exactly
        // like one that passed. So `before` is recorded and the pair is what is reported.
        'if [ -f "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" ]; then before=written; else before=missing; fi',
        'echo "note=$before"',
        'st=0; clawdline_confirm_suite_lock || st=$?; echo "confirm=$st"',
        'if [ -f "$CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE" ]; then after=kept; else after=gone; fi',
        'case "$before/$after" in',
        '  written/gone) echo "note_after=consumed" ;;',
        '  written/kept) echo "note_after=kept" ;;',
        '  missing/gone) echo "note_after=never_written" ;;',
        '  *) echo "note_after=appeared" ;;',
        'esac',
        'rm -rf "$CLAWDLINE_SUITE_LOCK_DIR"',
    ].join("\n"));
    const r23b = run(note23);
    check("a renewal loop that stops for a good reason leaves that reason where the run can read it",
          /note=written/.test(r23b.all));
    check("the confirmation repeats it to the run and refuses the second expensive thing",
          /confirm=75/.test(r23b.all)
            && /renewal loop stopped during the guarded section — .*changed hands/.test(r23b.all));
    // The verdict is in the line so the reading is visible in a green run too: `consumed` and
    // `never_written` are different sentences on the screen, where the old `read` was one.
    const noteVerdict = /note_after=(\S+)/.exec(r23b.all)?.[1] ?? "nothing reported";
    check(`and the note is consumed, so a later run cannot inherit somebody else's stop reason (read: ${noteVerdict})`,
          noteVerdict === "consumed");
    check("the note lives outside the lock, which is the directory two of the stop reasons are about",
          /CLAWDLINE_SUITE_LOCK_RENEWAL_NOTE:-\$\{TMPDIR:-\/tmp\}\//.test(block));

    // -----------------------------------------------------------------------------------------
    // 24. **`build.sh`'s first write is checked, and a lock it could not write is given back.**
    //
    //     `clawdline_lease_record` touches the beat before the record that points at it, so a
    //     first write whose `$temp` create fails where the beat's succeeded leaves a directory
    //     with a `beat` and no `holder.txt`. That is the one shape `test.sh`'s escape from
    //     `unknown` deliberately refuses to clear — a directory that has a beat had a record once
    //     — so it is the machine's compile slot blocked with no automatic way out. And the call
    //     was unchecked under `set -e`, so the build died on the failed write without saying so.
    //     `test.sh` has handled this since the record contract landed and the broker's
    //     `createDirectory` removes the directory when its write fails; this was the third writer.
    const build24 = shell("s24.sh", buildBlock, [
        'CLAWDLINE_LEASE_STARTED=$(date +%s)',
        'CLAWDLINE_LEASE_ID="build-test-24"',
        'CLAWDLINE_LEASE_DONE="$LOCK_SCRATCH/done24"',
        'mkdir -p "$CLAWDLINE_LEASE_DIR"',
        // **The half-failure, reproduced rather than approximated.** The beat has to succeed and
        // the record has to fail, which is what ENOSPC does and what an unwritable directory does
        // not — that one fails both, leaves no beat, and is therefore clearable. So the beat is
        // left to work and the record's temporary file is blocked on its own: `$RANDOM` is
        // deterministic after an assignment, so the exact path the writer is about to use is
        // predicted here and a directory is put in its way.
        'RANDOM=4242; blocked="$CLAWDLINE_LEASE_DIR/.holder.$$.$RANDOM"',
        'mkdir -p "$blocked"',
        'RANDOM=4242',
        'st=0; clawdline_lease_first_record || st=$?; echo "first=$st"',
        '[ -d "$CLAWDLINE_LEASE_DIR" ] && echo "lock=left" || echo "lock=given_back"',
        'echo "mode=[${CLAWDLINE_LEASE_MODE}]"',
        'rm -rf "$CLAWDLINE_LEASE_DIR"',
        // The control: the same call against a writable directory takes the lock and says so.
        'mkdir -p "$CLAWDLINE_LEASE_DIR"',
        'st=0; clawdline_lease_first_record || st=$?; echo "good=$st"',
        'grep -q "^token=build-test-24$" "$CLAWDLINE_LEASE_DIR/holder.txt" && echo "record=written"',
        'echo "good_mode=[${CLAWDLINE_LEASE_MODE}]"',
        'rm -rf "$CLAWDLINE_LEASE_DIR"',
    ].join("\n"));
    const r24 = run(build24, { CLAWDLINE_SUITE_LOCK_DIR: join(dir, "build24.lock") });
    check("a build whose first record could not be written refuses rather than compiling",
          /first=1/.test(r24.all) && /refusing to compile behind a lock/.test(r24.all));
    check("and gives the directory back, so no unclearable lock is left on the machine",
          /lock=given_back/.test(r24.all));
    check("and does not leave itself holding a lease it never took",
          /mode=\[\]/.test(r24.all));
    check("while the same call against a writable directory takes the lock and records it",
          /good=0/.test(r24.all) && /record=written/.test(r24.all)
            && /good_mode=\[directory\]/.test(r24.all));
    // The call site, in build.sh itself: the one acquisition goes through the checked write. A
    // function nobody calls and a call to a function that does nothing are the same silence. There
    // were two of these while the broker branch existed and one of them jumped the queue; there is
    // one now, and the count is asserted so a second one cannot be added in silence.
    const directTakes = [...buildScript.matchAll(/mkdir "\$CLAWDLINE_LEASE_DIR" 2>\/dev\/null; then\n([\s\S]{0,200}?)\n\s*echo "→ heavy-compile lock taken/g)];
    check("build.sh's one acquisition writes its first record through the checked path",
          directTakes.length === 1
            && directTakes.every((m) => /clawdline_lease_first_record/.test(m[1])
                                        && !/clawdline_lease_record analysing/.test(m[1])));

    // -----------------------------------------------------------------------------------------
    // 25. **`build.sh` can take a dead holder's lock, under exactly the rules `test.sh` uses.**
    //
    //     It could not. `grep -c 'takeover\|stale\|reclaim'` answered 1 for `build.sh` and 25 for
    //     `test.sh`, and the two words in `build.sh` were in a comment. Its acquire loop had two
    //     outcomes: `mkdir` succeeds, or wait out `CLAWDLINE_LEASE_WAIT_SECONDS` — 1800 by
    //     default — and refuse. So a `./test.sh` killed with SIGKILL, or a Mac force-rebooted by
    //     Jetsam mid-suite (which is the event this whole lock exists for), left
    //     `/tmp/clawdline-suite.lock` behind; the next `./test.sh` reclaimed it after one 60s
    //     renewal deadline and the next `./build.sh` printed the holder, waited half an hour and
    //     declined to build. "The machine crashed and needs rebuilding" is the path
    //     `docs/machine-resource-scheduling.md` names as the one that has to work.
    //
    //     The takeover is `test.sh`'s, copied rather than reinvented, so what is checked here is
    //     the same four things: it reclaims a dead holder's lock, it never reclaims one while a
    //     compiler is running, it never reclaims one that is still beating, and two waiters cannot
    //     both reclaim the same one.
    const buildWaiter = shell("s25-waiter.sh", buildBlock, [
        'st=0',
        'clawdline_lease_acquire || st=$?',
        'echo "acquire=$st"',
        'if [ "$st" = 0 ]; then',
        '  echo "enter ${LOCK_ID:-build}" >> "$LOCK_EVENTS"',
        '  sleep "${LOCK_HOLD:-0.4}"',
        '  echo "leave ${LOCK_ID:-build}" >> "$LOCK_EVENTS"',
        '  echo "ENTERED"',
        '  clawdline_lease_release',
        'fi',
        'exit "$st"',
    ].join("\n"));

    // 25a. The crash the lock exists for: a holder that will never renew again, and a clear machine.
    const lock25 = join(dir, "build25.lock");
    const orch25 = shell("s25.sh", PRELUDE, [
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" crafted-token-25 2',
        `capture "$LOCK_SCRATCH/b25.log" build env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=4 "${buildWaiter}"`,
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "lock=present" || echo "lock=gone"',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR.takeover" ] && echo "gate=left" || echo "gate=cleared"',
    ].join("\n"));
    const r25 = run(orch25, { CLAWDLINE_SUITE_LOCK_DIR: lock25, LOCK_EVENTS: join(dir, "e25") });
    const b25 = readIf(join(dir, "b25.log"));
    check("build.sh takes over a lock whose holder stopped renewing and compiles",
          /build=0/.test(b25) && /took over/.test(b25) && /ENTERED/.test(b25));
    check("and it says what it read before it did, rather than removing a directory in silence",
          /past its own 2s deadline, and no lockprobe\d+ is running anywhere/.test(b25));
    check("and the takeover gate is not left behind for the next waiter to trip over",
          /gate=cleared/.test(r25.all));
    check("and the lock it then took is given back when the build ends",
          /lock=gone/.test(r25.all));

    // 25b. The physical backstop, which is never waived — the half a `stale` record cannot supply.
    //      A holder that has stopped renewing while its compiler is still orphaned on the machine
    //      is still spending the 46 GB this lock rations.
    const lock25b = join(dir, "build25b.lock");
    const orch25b = shell("s25b.sh", PRELUDE, [
        'c=$(fake_compiler 30)',
        'sleep 0.3',
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" crafted-token-25b 2',
        `capture "$LOCK_SCRATCH/b25b.log" build env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=2 "${buildWaiter}"`,
        'echo "orphan_pid=$c"',
        'ps -p "$c" -o pid= >/dev/null 2>&1 && echo "orphan=alive" || echo "orphan=gone"',
        '[ -d "$CLAWDLINE_SUITE_LOCK_DIR" ] && echo "lock=present" || echo "lock=gone"',
        'kill "$c" 2>/dev/null || true; wait "$c" 2>/dev/null || true',
    ].join("\n"));
    const r25b = run(orch25b, { CLAWDLINE_SUITE_LOCK_DIR: lock25b, LOCK_EVENTS: join(dir, "e25b") });
    const b25b = readIf(join(dir, "b25b.log"));
    const orphan25b = /^orphan_pid=(\d+)$/m.exec(r25b.all);
    check("a dead holder with a live compiler is not stale to build.sh either, and it refuses",
          /build=1/.test(b25b) && !/ENTERED/.test(b25b) && !/took over/.test(b25b)
            && /lock=present/.test(r25b.all));
    check("the refusal names the orphan by pid, so a person can deal with it",
          orphan25b !== null && b25b.includes(orphan25b[1]));
    check("and nothing was killed to make room",
          /orphan=alive/.test(r25b.all));

    // 25c. The mirror image, across the two writers: a `test.sh` that is alive and renewing between
    //      two compiles has no compiler running at that moment, and `build.sh` must read its record
    //      and wait. This is the one scenario where the record is written by one script and judged
    //      by the other, which is the whole reason the contract is a contract.
    const lock25c = join(dir, "build25c.lock");
    const holder25c = holderScript("s25c-holder.sh", [
        'clawdline_suite_lock_phase idle-holding',
        'touch "$LOCK_SCRATCH/held25c"',
        'while [ ! -f "$LOCK_SCRATCH/go25c" ]; do sleep 0.05; done',
    ].join("\n"));
    const orch25c = shell("s25c.sh", PRELUDE, [
        `"${holder25c}" > "$LOCK_SCRATCH/holder25c.log" 2>&1 &`,
        'h=$!',
        'while [ ! -f "$LOCK_SCRATCH/held25c" ]; do sleep 0.05; done',
        'pgrep -x "$LOCK_PATTERN" >/dev/null 2>&1 && echo "compilers=some" || echo "compilers=none"',
        `capture "$LOCK_SCRATCH/b25c.log" build env CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=4 "${buildWaiter}"`,
        'touch "$LOCK_SCRATCH/go25c"',
        'wait "$h"',
    ].join("\n"));
    const r25c = run(orch25c, { CLAWDLINE_SUITE_LOCK_DIR: lock25c, LOCK_EVENTS: join(dir, "e25c") });
    const b25c = readIf(join(dir, "b25c.log"));
    check("a test.sh holder that is alive and renewing keeps the lock against build.sh, with no compiler running",
          /compilers=none/.test(r25c.all) && /build=1/.test(b25c) && !/ENTERED/.test(b25c)
            && !/took over/.test(b25c));
    check("and build.sh waited long enough for a renewal deadline to have expired had renewal not happened",
          /gave up waiting 4s/.test(b25c));
    check("and it reads the other writer's phase and compile clock rather than guessing from the machine",
          /phase idle-holding for \d+s/.test(b25c)
            && /(last compiling \d+s|nothing has compiled under this lock yet)/.test(b25c));

    // 25d. Two builds that judge the same lock stale must not both walk in. `rename` picks one; the
    //      gate and the re-read are what stop a judgement older than a whole takeover from renaming
    //      away a lock somebody has since acquired legitimately.
    const lock25d = join(dir, "build25d.lock");
    const events25d = join(dir, "e25d");
    const orch25d = shell("s25d.sh", PRELUDE, [
        'craft_lock "$CLAWDLINE_SUITE_LOCK_DIR" "$(dead_pid)" "$(( $(date +%s) - 600 ))" crafted-token-25d 2',
        `capture "$LOCK_SCRATCH/b25d-a.log" a env LOCK_ID=A CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=6 "${buildWaiter}" &`,
        'p1=$!',
        `capture "$LOCK_SCRATCH/b25d-b.log" b env LOCK_ID=B CLAWDLINE_SUITE_LOCK_WAIT_SECONDS=6 "${buildWaiter}" &`,
        'p2=$!',
        'wait "$p1"; wait "$p2"',
    ].join("\n"));
    run(orch25d, { CLAWDLINE_SUITE_LOCK_DIR: lock25d, LOCK_EVENTS: events25d, LOCK_HOLD: "0.6" });
    const log25d = readIf(events25d);
    check("two builds that both judge one lock stale are still serialised",
          /enter /.test(log25d) && !overlapped(log25d));
    check("and both of them get in, one after the other, rather than one being refused outright",
          (log25d.match(/^enter /gm) || []).length === 2);

    // 25e. **One lock, one set of dials.** `build.sh` read `CLAWDLINE_LEASE_DIR` and
    //      `CLAWDLINE_LEASE_DEADLINE_SECONDS`; `test.sh` read `CLAWDLINE_SUITE_LOCK_*`. The
    //      defaults agreed, so nothing ever said otherwise — and `heartbeat_deadline` is a record
    //      field *both* writers fill in and every reader prefers to its own, so tuning one spelling
    //      put two different numbers in one field. What is checked is the resolution, by running
    //      it: the ambient values this file pins are removed so the blocks answer for themselves.
    const runBare = (file, env = {}) => {
        const bare = { ...baseEnv, ...env };
        for (const key of Object.keys(bare)) {
            if (/^CLAWDLINE_(SUITE_LOCK|LEASE)_(DIR|DEADLINE_SECONDS|WAIT_SECONDS)$/.test(key)
                && !(key in env)) delete bare[key];
        }
        const r = spawnSync("/bin/bash", [file], { encoding: "utf8", env: bare, cwd: dir });
        return `${r.stdout ?? ""}${r.stderr ?? ""}`;
    };
    const askTest = shell("s25e-test.sh", functionsOnly,
        'echo "dir=$CLAWDLINE_SUITE_LOCK_DIR"; echo "deadline=$CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS"');
    const askBuild = shell("s25e-build.sh", buildBlock,
        'echo "dir=$CLAWDLINE_LEASE_DIR"; echo "deadline=$CLAWDLINE_LEASE_DEADLINE_SECONDS"');
    const bareTest = runBare(askTest);
    const bareBuild = runBare(askBuild);
    check("told nothing, both scripts point at the same machine-wide lock and the same deadline",
          /dir=\/tmp\/clawdline-suite\.lock/.test(bareTest) && /deadline=60/.test(bareTest)
            && /dir=\/tmp\/clawdline-suite\.lock/.test(bareBuild) && /deadline=60/.test(bareBuild));
    const legacy = { CLAWDLINE_LEASE_DIR: join(dir, "alias.lock"), CLAWDLINE_LEASE_DEADLINE_SECONDS: "37" };
    check("told only build.sh's old names, both scripts follow them — which is what closes the field",
          new RegExp(`dir=${join(dir, "alias.lock")}`).test(runBare(askTest, legacy))
            && /deadline=37/.test(runBare(askTest, legacy))
            && new RegExp(`dir=${join(dir, "alias.lock")}`).test(runBare(askBuild, legacy))
            && /deadline=37/.test(runBare(askBuild, legacy)));
    const both = { ...legacy, CLAWDLINE_SUITE_LOCK_DIR: join(dir, "canonical.lock"),
                   CLAWDLINE_SUITE_LOCK_DEADLINE_SECONDS: "41" };
    check("and when both spellings are set the canonical one wins, on both sides",
          new RegExp(`dir=${join(dir, "canonical.lock")}`).test(runBare(askTest, both))
            && /deadline=41/.test(runBare(askTest, both))
            && new RegExp(`dir=${join(dir, "canonical.lock")}`).test(runBare(askBuild, both))
            && /deadline=41/.test(runBare(askBuild, both)));
    // The field itself, written by each writer in turn against one directory. This is the check the
    // rename exists for: a reader prefers `heartbeat_deadline` to its own number, so two writers
    // disagreeing here is two runs with different ideas of when the other has died.
    const aliasLock = join(dir, "alias-record.lock");
    const aliasTest = holderScript("s25e-test-record.sh",
        'cp "$CLAWDLINE_SUITE_LOCK_DIR/holder.txt" "$LOCK_SCRATCH/alias-test.txt"');
    runBare(aliasTest, { CLAWDLINE_LEASE_DIR: aliasLock, CLAWDLINE_LEASE_DEADLINE_SECONDS: "37" });
    const aliasBuild = shell("s25e-build-record.sh", buildBlock, [
        'CLAWDLINE_LEASE_STARTED=$(date +%s)',
        'CLAWDLINE_LEASE_ID="build-alias-25e"',
        'CLAWDLINE_LEASE_DONE="$LOCK_SCRATCH/done25e"',
        'mkdir -p "$CLAWDLINE_LEASE_DIR"',
        'clawdline_lease_first_record',
        'cp "$CLAWDLINE_LEASE_DIR/holder.txt" "$LOCK_SCRATCH/alias-build.txt"',
        'rm -rf "$CLAWDLINE_LEASE_DIR"',
    ].join("\n"));
    runBare(aliasBuild, { CLAWDLINE_LEASE_DIR: aliasLock, CLAWDLINE_LEASE_DEADLINE_SECONDS: "37" });
    const aliasTestRecord = readIf(join(dir, "alias-test.txt"));
    const aliasBuildRecord = readIf(join(dir, "alias-build.txt"));
    check("and the record both of them write carries one heartbeat_deadline, not one each",
          /^heartbeat_deadline=37$/m.test(aliasTestRecord)
            && /^heartbeat_deadline=37$/m.test(aliasBuildRecord));

    if (failures > 0) {
        console.log("    last orchestrator stderr:", JSON.stringify(r13.err.slice(0, 400)));
    }
} finally {
    rmSync(dir, { recursive: true, force: true });
}

// The receipt reporter is lifted out of test.sh and driven, rather than retyped here: a copy would
// go on passing after the original changed, which is the failure this whole file exists to catch.
{
    const marker = "# >>> clawdline receipt direction >>>";
    const end = "# <<< clawdline receipt direction <<<";
    if (!script.includes(marker) || !script.includes(end)) {
        stop("test.sh has no receipt-direction block to drive; if it was renamed, update this file rather than deleting the checks");
    }
    const block = script.slice(script.indexOf(marker), script.indexOf(end) + end.length);
    const cloudReceipt = /^expected_cloud_receipt='(.*)'$/m.exec(script)[1];
    const work = mkdtempSync(join(tmpdir(), "receipt-direction-"));
    const drive = (logBody, seal) => {
        const log = join(work, "suite.log");
        writeFileSync(log, logBody);
        const harness = [
            `expected_swift_receipt='${seal} checks passed'`,
            `expected_cloud_receipt='${cloudReceipt}'`,
            block,
            `report_receipt_direction '${log}'`,
        ].join("\n");
        const run = spawnSync("/bin/bash", ["-c", harness], { encoding: "utf8" });
        return `${run.stdout}${run.stderr}`;
    };
    const suiteLines = cloudReceipt.replace(/^.*suites=/, "").split(",")
        .map((entry) => `  \u2713 ${entry.split(":")[0]} (${entry.split(":")[1]} checks)`);

    // The two totals this reads are printed by Tests/CloudTestRunner.swift, and until now the checks
    // below fed it strings typed here. That is the shape that bit three deliveries today: a check
    // anchored on a spelling it does not share with the thing that produces it goes on passing while
    // the producer moves, and this one's failure direction is silence — the reporter simply stops
    // recognising a total. So the formats are lifted from the producer's `print` statements and the
    // reporter's own pattern is run against what they render, with the interpolations filled in.
    const runner = readFileSync(new URL("../Tests/CloudTestRunner.swift", import.meta.url), "utf8");
    const printed = [...runner.matchAll(/print\("([^"]*\\\([^)]*\)[^"]*)"\)/g)].map((m) => m[1]);
    const rendered = printed
        .filter((t) => /checks (passed|failed)/.test(t) && !/focused/.test(t))
        .map((t) => t.replace(/\\\(failures\.count\)/g, "1").replace(/\\\(checks\)/g, "7724"));
    check("the runner's two total lines were found, or this check is testing nothing",
        rendered.length === 2);
    for (const line of rendered) {
        const seal = line.includes("failed") ? 8226 : 9000;
        const out = drive(`${line}\n`, seal);
        check(`the reporter recognises a total the runner actually prints: ${JSON.stringify(line)}`,
            /came out short/.test(out));
    }

    // Long: the tree grew and nobody re-sealed. Until 2026-09-03 this shared one sentence with the
    // short case, and `receipt mismatch` reads as *your delivery is broken* on a run that was fine.
    const long = drive("8383 checks passed\n", 8226);
    check("a total above the seal says the tree grew and the seal did not follow",
        /tree grew and the seal did not follow/.test(long) && long.includes("157 checks were added"));
    check("and it says the suite itself is fine, so nobody hunts a defect that is not there",
        /suite itself is fine/.test(long));
    check("and it sends them to a run for the new number, not to arithmetic",
        /never from arithmetic/.test(long));

    // Short: a group aborted and took the ones after it. The count is a coverage number too.
    const short = drive([...suiteLines.slice(0, 8), "1 of 7724 checks failed:"].join("\n"), 8226);
    check("a total below the seal says how many checks never ran",
        /came out short/.test(short) && short.includes("502 never ran"));
    check("and names the cloud suites that never reported, which does not drift as they grow",
        ["CloudCommandLedger", "CloudOutboundSpool", "CloudPairing", "CloudLifecycle"]
            .every((suite) => new RegExp(`never reported:[^\n]*${suite}`).test(short)));
    check("and does not name a cause it has not established: cwd and abort look identical here",
        !/aborted group shrinks/.test(short));
    check("and says a run that did not finish is not a green",
        /not a green/.test(short));

    // The two totals live on different lines — `N of M checks failed:` and `M checks passed`. An
    // earlier version read only the first and was therefore silent on the case that arrived.
    check("it reads the green line as well as the red one, or it is silent on half its input",
        /checks passed\$\//.test(block) || block.includes("checks passed$/"));
    check("an exact total says nothing at all", drive("8226 checks passed\n", 8226).trim() === "");
    check("a log with no total at all is silent rather than guessing",
        drive("nothing here\n", 8226).trim() === "");
    rmSync(work, { recursive: true, force: true });
}

// The twelve fields inside the Cloud seal, and the door that lets somebody move one. The block
// above drives the *reporter* by lifting one function out of `test.sh`; this one runs the whole of
// `test.sh` through its own `--verify-completion-receipts` mode, because what is checked here is a
// decision — which branch returns, and with what — and a decision cannot be lifted out of the
// script that makes it. That mode compiles nothing and runs no suite; it reads a log.
//
// **What it is guarding.** `expected_cloud_receipt` pins twelve counts inside one exact string that
// `count_exact_receipt_lines` compares whole, so all twelve produce one sentence when they move:
// *the receipt appeared 0 times*. On 2026-09-03 `8399aee7` added two `require` calls to
// `Tests/CloudTransportTests.swift` and `b66d5d27` removed one, neither touched `test.sh`, and
// `main` said that sentence for four hours without ever naming CloudTransport. Every check below is
// that sentence being replaced by a name and two numbers — and by a line somebody can paste.
{
    const work = mkdtempSync(join(tmpdir(), "cloud-receipt-fields-"));
    try {
        const copy = join(work, "test.sh");
        copyFileSync(new URL("../test.sh", import.meta.url), copy);

        const cloudSeal = /^expected_cloud_receipt='(.*)'$/m.exec(script);
        const swiftSeal = /^expected_swift_receipt='(.*)'$/m.exec(script);
        if (!cloudSeal || !swiftSeal) {
            stop("test.sh no longer carries both receipt seals under the names this file reads; if they were renamed, update this file rather than deleting the checks");
        }
        const sealedPairs = cloudSeal[1].replace(/^.*suites=/, "").split(",").map((entry) => {
            const [name, count] = entry.split(":");
            return { name, count: Number(count) };
        });

        // The fixture's spelling is lifted from `Tests/CloudTestRunner.swift` rather than typed
        // here, for the reason the block above gives: a check anchored on a spelling it does not
        // share with the producer goes on passing while the producer moves, and this one's failure
        // direction is silence. It also settles a question worth writing down — the twelve pairs
        // come off **one** print site, so there is one format and not twelve.
        const runner = readFileSync(new URL("../Tests/CloudTestRunner.swift", import.meta.url), "utf8");
        const suitePrints = [...runner.matchAll(/print\("([^"]*\\\(suite\.name\)[^"]*checks\)[^"]*)"\)/g)]
            .map((m) => m[1]);
        if (suitePrints.length !== 1) {
            stop(`the Cloud runner has ${suitePrints.length} sites printing a per-suite line, not the one this file reads them with`);
        }
        const suiteLine = ({ name, count }) => suitePrints[0]
            .replace("\\(suite.name)", name).replace("\\(suiteChecks)", String(count));
        // Same for the receipt itself: its prefix is a constant in the runner, and the seal in
        // `test.sh` is a transcription of a line that constant opened. Building the fixture from
        // the seal and checking it reproduces the seal exactly is what keeps this a fixture of the
        // real line rather than a hand-made lookalike.
        const receiptPrefix = cloudSeal[1].split(" suite_count=")[0];
        const receiptFor = (pairs) => `${receiptPrefix} suite_count=${pairs.length} suites=`
            + pairs.map((p) => `${p.name}:${p.count}`).join(",");
        if (receiptFor(sealedPairs) !== cloudSeal[1]) {
            stop("the receipt this file builds is not the seal test.sh carries, so every log below would be a lookalike rather than a fixture");
        }
        if (!runner.includes(`"${receiptPrefix} `)) {
            stop(`the Cloud runner does not open its receipt with ${receiptPrefix}, so the seal and the producer have parted company`);
        }

        let logs = 0;
        const logFor = ({ pairs = sealedPairs, total = swiftSeal[1], receipt = true, line = null }) => {
            const path = join(work, `suite-${++logs}.log`);
            const body = [...pairs.map(suiteLine), "", total];
            if (receipt) body.push(line ?? receiptFor(pairs));
            writeFileSync(path, `${body.join("\n")}\n`);
            return path;
        };
        // **The operator's own `CLAWDLINE_RESEAL` must not reach the script this block drives.**
        // Half of what is asserted here is the door being *shut*, so a re-sealing operator running
        // the suite would watch these fail on their own environment rather than on the tree — the
        // same trap the governance block below documents, in the second guard to have a door.
        const sealedEnv = () => {
            const e = { ...process.env };
            delete e.CLAWDLINE_RESEAL;
            return e;
        };
        const verify = (logPath, env = {}, scriptPath = copy) => {
            const r = spawnSync("/bin/bash", [scriptPath, "--verify-completion-receipts", logPath],
                                { encoding: "utf8", env: { ...sealedEnv(), ...env } });
            return { status: r.status, out: `${r.stdout}${r.stderr}` };
        };

        const untouched = verify(logFor({}));
        if (untouched.status !== 0) {
            stop(`a log built from test.sh's own seals is not accepted by test.sh, so nothing below can mean anything: ${untouched.out.slice(0, 400)}`);
        }

        const moveOne = (name, by) => sealedPairs.map((p) => p.name === name ? { ...p, count: p.count + by } : p);
        const first = sealedPairs[2], second = sealedPairs[3], last = sealedPairs[sealedPairs.length - 1];
        const row = (name, sealed, reported, delta) =>
            new RegExp(`\\n  ${name}\\s+sealed ${sealed}, this run reported ${reported} \\(${delta}\\)`);

        // The decisive one, and 2026-09-03 reproduced: one `require` added to one Cloud suite.
        const one = verify(logFor({ pairs: moveOne(first.name, 1) }));
        check("the field that moved is named, with what the seal says, what this run reported and the distance between them",
              one.status === 125 && row(first.name, first.count, first.count + 1, "\\+1").test(one.out));
        check("and the eleven that did not move are not named, so the one that did is the message",
              sealedPairs.filter((p) => p.name !== first.name).every((p) => !one.out.includes(p.name)));

        const two = verify(logFor({
            pairs: moveOne(first.name, 1).map((p) => p.name === second.name ? { ...p, count: p.count - 7 } : p),
        }));
        check("two fields that moved are both named, each in its own direction",
              row(first.name, first.count, first.count + 1, "\\+1").test(two.out)
                && row(second.name, second.count, second.count - 7, "-7").test(two.out));
        check("and how many did not move is a number it read rather than one it asserts",
              new RegExp(`The other ${sealedPairs.length - 2} suites`).test(two.out));

        // A suite that never reported is the other shape, and it already had a path. That path is
        // `report_receipt_direction`, reached when the total came out short, and it must still be
        // the one that runs — the fields below add to it rather than replace it.
        const shortTotal = `1 of ${Number(swiftSeal[1].split(" ")[0]) - 679} checks failed:`;
        const silent = verify(logFor({
            pairs: sealedPairs.filter((p) => p.name !== last.name), receipt: false, total: shortTotal,
        }));
        check("a suite that never reported still walks the path it already had",
              new RegExp(`never reported:[^\\n]*${last.name}`).test(silent.out));
        check("and the fields report it as one that printed no line, not as a count that moved",
              new RegExp(`\\n  ${last.name}\\s+sealed ${last.count}, and this run printed no line for it`).test(silent.out)
                && !row(last.name, last.count, "\\d+", "[-+]\\d+").test(silent.out));
        check("and a roster that came up short does not claim suite_count should follow it",
              new RegExp(`suite_count=${sealedPairs.length} and this run reported ${sealedPairs.length - 1} Cloud suites`).test(silent.out)
                && !/suite_count moves with them/.test(silent.out));

        // The other direction: a thirteenth Cloud suite. The runner refuses to print a receipt at
        // all when its roster is not the twelve it expects, so the fixture carries none — which is
        // why the fields have to be read off the per-suite lines and not out of the receipt.
        const grown = [...sealedPairs, { name: "CloudDrafts", count: 9 }];
        const extra = verify(logFor({ pairs: grown, receipt: false, total: `1 of ${swiftSeal[1].split(" ")[0]} checks failed:` }));
        check("a suite the seal does not name is named, with suite_count said to move with it",
              new RegExp(`\\n  CloudDrafts\\s+this run reported 9`).test(extra.out)
                && new RegExp(`suite_count=${sealedPairs.length} and this run reported ${grown.length} Cloud suites, so suite_count moves with them`).test(extra.out));

        const relabelled = verify(logFor({ line: cloudSeal[1].replace("v=1", "v=2") }));
        check("twelve fields that all agree point at the line itself and print both of them, rather than at a suite",
              /every suite it names reported exactly the count it names/.test(relabelled.out)
                && relabelled.out.includes(`sealed:   ${cloudSeal[1]}`)
                && relabelled.out.includes(`this run: ${cloudSeal[1].replace("v=1", "v=2")}`));

        // **Zero scan.** A pattern that has stopped matching finds twelve suites that did not move,
        // which is the exact shape of the silence this whole block exists to remove. So it is
        // blinded on purpose and the answer has to be the third one: *not compared*.
        const PATTERN = "[A-Za-z]+ \\([0-9]+ checks\\)";
        const source = readFileSync(copy, "utf8");
        const occurrences = source.split(PATTERN).length - 1;
        check("the pattern that reads this run's per-suite counts has exactly one copy in test.sh",
              occurrences === 1);
        if (occurrences === 0) {
            stop("test.sh no longer contains the pattern this file blinds, so the zero-scan checks below would prove nothing");
        }
        const blind = join(work, "blind.sh");
        writeFileSync(blind, source.split(PATTERN).join("zzzzzzzz"));
        const blindLog = logFor({ pairs: moveOne(first.name, 1) });
        const blinded = verify(blindLog, {}, blind);
        check("a scan that matches nothing says the counts could not be read, and names the log it could not read them out of",
              blinded.status === 125 && /per-suite counts could not be read out of/.test(blinded.out)
                && blinded.out.includes(blindLog));
        check("and it does not report a clean comparison of no data",
              !/suites? moved/.test(blinded.out) && !/reported exactly the count it names/.test(blinded.out)
                && /never compared/.test(blinded.out));

        // The door. Everything above ran without it, which is half of what "unchanged" means; the
        // rest is that the run still stops at the Cloud seal without it, and still ends 125 with it.
        const bothWrong = logFor({
            pairs: moveOne(first.name, 1), total: `${Number(swiftSeal[1].split(" ")[0]) + 4} checks passed`,
        });
        const shut = verify(bothWrong);
        check("without the door the run returns at the Cloud seal, exactly as before: no Swift comparison and nothing to paste",
              shut.status === 125 && !/Swift test completion receipt mismatch/.test(shut.out)
                && !/expected_cloud_receipt='/.test(shut.out));
        check("and the door is the value 1, not the variable being set to something",
              JSON.stringify(verify(bothWrong, { CLAWDLINE_RESEAL: "yes" })) === JSON.stringify(shut));

        const open = verify(bothWrong, { CLAWDLINE_RESEAL: "1" });
        check("the door hands back the line this run printed, already inside the assignment it belongs in",
              open.out.includes(`expected_cloud_receipt='${receiptFor(moveOne(first.name, 1))}'`));
        check("and it goes on to compare the Swift seal instead of stopping at the Cloud one",
              /Swift test completion receipt mismatch/.test(open.out));
        check("and the run still ends 125, because the door decides what you are told and not whether the tree is sealed",
              open.status === 125);

        const nothingToCopy = verify(logFor({ pairs: moveOne(first.name, 1), receipt: false }),
                                     { CLAWDLINE_RESEAL: "1" });
        check("and with no receipt line at all it says there is nothing to re-seal from rather than offering one",
              nothingToCopy.status === 125 && /nothing to re-seal from/.test(nothingToCopy.out)
                && !/expected_cloud_receipt='/.test(nothingToCopy.out));
    } finally {
        rmSync(work, { recursive: true, force: true });
    }
}

// The governance table and the seal it renders, driven the same way: the real
// `tools/check-architecture-boundaries.sh` is run against a scratch copy of this tree, and the
// mutations are applied to the copy. It lives beside the receipt checks above because it is the
// same subject — what `./test.sh` believes about its own totals before it starts compiling — and
// because both are guards whose failure mode is silence.
//
// **What it is guarding.** On 2026-09-03 a commit added eight checks and updated neither the seal in
// `test.sh` nor the governance row in `docs/architecture-refactor.md`. The guard compared those two
// with each other, they still agreed, so it was green — while `main` ran 8,101 against a seal of
// 8,093 for hours, and the next person's suite run nearly wore the blame. The decisive check below
// is that exact sequence: add an assertion to a Swift test file, change nothing else, and the guard
// must now refuse to start a compiler.
{
    const tree = mkdtempSync(join(tmpdir(), "governance-table-"));
    const from = (rel) => new URL(`../${rel}`, import.meta.url);
    try {
        for (const dir of ["Sources", "Tests", "tools"]) {
            cpSync(from(dir), join(tree, dir), { recursive: true });
        }
        mkdirSync(join(tree, "docs"), { recursive: true });
        copyFileSync(from("docs/architecture-refactor.md"), join(tree, "docs/architecture-refactor.md"));
        copyFileSync(from("test.sh"), join(tree, "test.sh"));

        const GUARD = "tools/check-architecture-boundaries.sh";
        const DOC = "docs/architecture-refactor.md";
        // Not a `*Tests.swift` file, so appending to it cannot trip the 2,000-line suite limit and
        // be mistaken for the witness firing.
        const PROBE = "Tests/TestProcessProbes.swift";
        const original = new Map();
        const read = (rel) => readFileSync(join(tree, rel), "utf8");
        const write = (rel, text) => {
            if (!original.has(rel)) original.set(rel, read(rel));
            writeFileSync(join(tree, rel), text);
        };
        // Anything the generator is about to rewrite has to be remembered before it runs, or
        // `restore()` puts back only what this file edited and leaves the tool's own write standing.
        const snapshot = (rel) => { if (!original.has(rel)) original.set(rel, read(rel)); };
        const restore = () => {
            for (const [rel, text] of original) writeFileSync(join(tree, rel), text);
            original.clear();
        };
        // **The operator's own `CLAWDLINE_RESEAL` must not reach the guard this block drives.** That
        // door downgrades the witness mismatch to a warning, which is exactly the behaviour two
        // checks below assert is *absent* — so on a re-seal run they saw a green guard and reported
        // themselves as failures. Every use of the door here is passed explicitly instead, and a
        // test that inherits the operator's environment is measuring the operator, not the tree.
        const sealedEnv = () => {
            const e = { ...process.env };
            delete e.CLAWDLINE_RESEAL;
            return e;
        };
        const run = (args = [], env = {}) => {
            const r = spawnSync("/bin/bash", [join(tree, GUARD), ...args],
                                { encoding: "utf8", env: { ...sealedEnv(), ...env } });
            return { status: r.status, out: `${r.stdout}${r.stderr}` };
        };
        const generate = () => spawnSync("/bin/bash", [join(tree, "tools/generate-governance-table.sh")],
                                         { encoding: "utf8", env: sealedEnv() });

        // This copy inherits whatever re-seal state the real tree is in, and during a re-seal that
        // tree's witness names a different tree **on purpose**. Nothing below is about that, so the
        // copy is made self-consistent first — with the number read out of the guard's own message
        // rather than counted a second time here, which would be the fourth copy of a number this
        // change exists to have one of. Without this, the whole block stopped on its own
        // precondition for every tree that had a re-seal pending, which is every tree that needs
        // the suite run this file sits in front of.
        const pending = run([], { CLAWDLINE_RESEAL: "1" });
        const wantedWitness = /expected_swift_receipt_witness to (\d+)/.exec(pending.out);
        // Written straight to disk rather than through `write`, which remembers the previous text
        // for `restore()`. Seeding through `write` made the seed itself revertible: every check
        // after the first restore got a witness mismatch instead of the failure it was asserting,
        // and reported `wrong reason` — the seed has to be this copy's baseline, not an edit to it.
        if (wantedWitness) {
            writeFileSync(join(tree, "test.sh"),
                          read("test.sh").replace(/^expected_swift_receipt_witness=\d+$/m,
                                                  `expected_swift_receipt_witness=${wantedWitness[1]}`));
        }

        const clean = run();
        if (clean.status !== 0) {
            stop(`the architecture guard is not green on an untouched copy of this tree, so nothing below can mean anything: ${clean.out.slice(0, 400)}`);
        }
        check("the guard says the table it accepted was its own rendering, not a document it read",
              /governance table is this run's own rendering/.test(clean.out));

        // Every row this file mutates is read out of the guard's own rendering rather than typed
        // here. A test that spelled out a row's value would be a fourth copy of a number this change
        // exists to have one of, and on the day that number moves it would fail on the literal
        // rather than on the behaviour. Four of the six moved on `main` while this was being written.
        const emitted = () => run(["--emit-governance-table"]).out.trim().split("\n");
        const dataRows = (lines) => lines.filter((line) => line.startsWith("| ") && !line.startsWith("|---")
                                                  && !line.includes("value on this tree"));
        const rowFor = (lines, label) => dataRows(lines).find((line) => line.split("|")[1].trim() === label);
        // Any different string will do; appending a digit cannot collide with the real value.
        const bumped = (row) => {
            const cells = row.split("|");
            cells[2] = `${cells[2].trimEnd()}0 `;
            return cells.join("|");
        };
        const committedRows = dataRows(emitted());
        if (committedRows.length !== 6) {
            stop(`the guard rendered ${committedRows.length} governance rows, not the six this file drives`);
        }

        // The decisive one. Today's failure, reproduced: one assertion added, nothing else touched.
        write(PROBE, `${read(PROBE)}\n// added by Tests/test-sh-lock.mjs\nfunc probeAddedAssertion() { check("added", true) }\n`);
        const added = run();
        check("an assertion added to a Swift test file turns the guard red with the seal untouched",
              added.status !== 0 && /assertion call sites/.test(added.out));
        check("and it says the seal was measured somewhere else rather than blaming the tree",
              /measured somewhere else/.test(added.out) || /no run has produced a total/.test(added.out));
        check("and it names the door to the run that knows the total, not an arithmetic correction",
              /CLAWDLINE_RESEAL=1/.test(added.out));

        const resealed = run([], { CLAWDLINE_RESEAL: "1" });
        check("CLAWDLINE_RESEAL=1 lets that run start, because only a run knows the new total",
              resealed.status === 0 && /CLAWDLINE_RESEAL=1/.test(resealed.out));
        // The door relaxes one thing. The table is rendered from the seal, so a re-sealing run never
        // had to route around it — but a table somebody typed into is red under the door as well.
        const suiteFilesRow = rowFor(committedRows, "suite files");
        write(DOC, read(DOC).replace(suiteFilesRow, bumped(suiteFilesRow)));
        const doorWithHandEditedTable = run([], { CLAWDLINE_RESEAL: "1" });
        check("and the door opens for the witness alone: a hand-edited table is red under it too",
              doorWithHandEditedTable.status !== 0
                && /is not what this tree renders/.test(doorWithHandEditedTable.out));
        restore();

        // Every row, not just the one that broke, and each edited in the document and nowhere else.
        // Hand-editing the document is the shape the whole change exists to abolish.
        const rowResults = committedRows.map((row) => {
            const doc = read(DOC);
            if (!doc.includes(row)) return `row not in the document: ${row}`;
            write(DOC, doc.replace(row, bumped(row)));
            const r = run();
            const named = /tools\/generate-governance-table\.sh/.test(r.out);
            restore();
            if (r.status === 0) return `stayed green: ${row}`;
            if (!/is not what this tree renders/.test(r.out)) return `wrong reason: ${row}`;
            if (!named) return `did not name the generator: ${row}`;
            return null;
        });
        check("all six rows go red when the document disagrees with its source, the Swift-checks row "
              + `included, and each names the generator rather than another number: ${rowResults.filter(Boolean).join("; ") || "none stayed green"}`,
              rowResults.length === 6 && rowResults.every((problem) => problem === null));

        // One source, demonstrated: move the seal and the document follows it without a suite run.
        const seal = /expected_swift_receipt='(\d+) checks passed'/.exec(read("test.sh"));
        if (!seal) stop("test.sh has no expected_swift_receipt to move; if it was renamed, update this file rather than deleting the checks");
        write("test.sh", read("test.sh").replace(seal[0], `expected_swift_receipt='${Number(seal[1]) + 1} checks passed'`));
        const movedSeal = run();
        check("moving the seal alone is red, because the document is a rendering of it",
              movedSeal.status !== 0 && /is not what this tree renders/.test(movedSeal.out));
        snapshot(DOC);
        const regenerated = generate();
        check("and running the generator is the whole repair — no suite run, no second edit",
              regenerated.status === 0 && run().status === 0);
        check("which leaves the row carrying the moved seal, written by the tool rather than by a person",
              read(DOC).includes(rowFor(emitted(), "Swift checks"))
                && !read(DOC).includes(rowFor(committedRows, "Swift checks")));
        restore();
        check("the guard is green again once the copy is put back", run().status === 0);

        // Fail-closed. A guard that cannot find what it guards must say so, not report a clean scan.
        // The scanner's own comment claims it goes red whether the names change or the boundary
        // stops being honoured; a claim about a guard is worth what its red proof is worth.
        write(GUARD, read(GUARD).replace("'\\b(check|expect)[A-Za-z0-9_]*\\('", "'zzz-no-such-assertion'"));
        const brokenScan = run();
        check("a scan that has stopped matching is red, not a clean scan of a tree without tests",
              brokenScan.status !== 0 && /broken scan/.test(brokenScan.out));
        restore();

        write("test.sh", read("test.sh").replace(/^expected_swift_receipt_witness=\d+$/m, ""));
        const noWitness = run();
        // On the exact message, not on the name appearing anywhere: without the fail-closed read the
        // comparison catches an empty witness too, and reports it as a tree that moved. That is red
        // for the wrong reason, and it is the reason a mutation of that line has to fail this.
        check("a seal with no witness names the missing line rather than blaming the tree",
              noWitness.status !== 0
                && /could not read expected_swift_receipt_witness/.test(noWitness.out));
        restore();

        write(DOC, read(DOC).replace("<!-- clawdline-governance-table:v1 -->", ""));
        const noMarkers = run();
        check("a document with the markers removed is red rather than a table nobody compares",
              noMarkers.status !== 0 && /markers are missing/.test(noMarkers.out));
        restore();

        // The generator renders from the guard's own values, so it cannot render a table for a tree
        // the guard rejects — which is what keeps it from being a way to write down a wrong number.
        write(GUARD, read(GUARD).replace(/^orchestrator_ceiling=\d+$/m, "orchestrator_ceiling=1"));
        const brokenTree = generate();
        // Named on the ratchet's own message, not on the exit code alone: a generator that is simply
        // missing also exits non-zero, and this check has to be able to tell those apart.
        check("the generator refuses a tree whose ratchets are red rather than writing it a table",
              brokenTree.status !== 0
                && /against a ceiling of 1,/.test(`${brokenTree.stdout}${brokenTree.stderr}`));
        restore();
    } finally {
        rmSync(tree, { recursive: true, force: true });
    }
}

console.log(failures === 0
    ? `test.sh suite lock: all ${checks} checks passed`
    : `test.sh suite lock: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
