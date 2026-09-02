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
import { mkdtempSync, readFileSync, writeFileSync, chmodSync, existsSync, rmSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
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

check("the lock is the machine-wide directory, by default",
      /CLAWDLINE_SUITE_LOCK_DIR:-\/tmp\/clawdline-suite\.lock/.test(block));
check("and staleness looks for the real compiler by default, not for whatever these tests use",
      /CLAWDLINE_SUITE_LOCK_COMPILER_PATTERN:-swift-frontend/.test(block));
// `mkdir` is the acquisition because it is atomic; `rename` is the takeover for the same reason.
check("acquisition is mkdir and takeover is rename, the two atomic operations",
      /mkdir "\$lock" 2>\/dev\/null/.test(block) && /mv "\$lock" "\$stale"/.test(block));
const busy = /CLAWDLINE_SUITE_LOCK_BUSY=(\d+)/.exec(block);
check("a busy lock exits on a number test.sh does not already use",
      busy !== null && ![0, 1, 2, 125, 126, 127].includes(Number(busy[1])) && Number(busy[1]) < 128);
// Nothing in this feature may signal another session's process. The only `kill` the block is
// allowed is the renewal loop it started for itself.
// A command, not the word: three of the block's refusals say "nothing here will kill them", and a
// check that counted those would be measuring its own prose.
const kills = code.filter((l) => /^\s*kill\b/.test(l));
check("the only process the block signals is the renewal loop it started itself",
      kills.length === 1 && /clawdline_suite_lock_renewer/.test(kills[0]));
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
    '    printf \'working=%s\\n\' ""',
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
    check("and its phase is one of the three words the lease also reads",
          existsSync(join(dir, "phase-was-vocabulary")));
    // The ratified record. The first eight are the shared format the broker's lease reads; the rest
    // are this implementation's own and are additive.
    for (const field of ["holder", "pid", "phase", "heartbeat", "started", "tree", "log", "done_flag",
                         "owner_pid", "token", "owner_started", "heartbeat_deadline",
                         "phase_since", "last_compiling", "working", "compilers", "note"]) {
        check(`the record carries ${field}`, new RegExp(`^${field}=`, "m").test(record));
    }
    check("the heartbeat names a file inside the lock, which is where liveness is read from",
          /^heartbeat=.*\/beat$/m.test(record) && existsSync(join(dir, "beat-was-there")));
    check("and the run gives the lock back when it ends", !existsSync(lockDir) && /released/.test(r1.all));
    check("the beat goes with the lock, so no heartbeat is left pointing at work that ended",
          !existsSync(join(lockDir, "beat")));
    check("and the store the old second trap used to remove is still removed",
          !existsSync(join(dir, "store")));

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
    // 12. The parallelism ceiling the lease will hand down, and where the number came from. Unset,
    //     the compile line below the block is byte-identical to what it has always been.
    rmSync(lockDir, { recursive: true, force: true });
    const jobs = shell("s12.sh", ceiling, 'echo "flags=[${clawdline_suite_jobs_flags[@]+${clawdline_suite_jobs_flags[@]}}]"');
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
    check("with no ceiling set, no flag is added and the run says none was set",
          r12a.code === 0 && /flags=\[\]/.test(r12a.all)
          && /none set/.test(r12a.all) && /CLAWDLINE_SUITE_JOBS unset/.test(r12a.all));
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

    if (failures > 0) {
        console.log("    last orchestrator stderr:", JSON.stringify(r13.err.slice(0, 400)));
    }
} finally {
    rmSync(dir, { recursive: true, force: true });
}

console.log(failures === 0
    ? `test.sh suite lock: all ${checks} checks passed`
    : `test.sh suite lock: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
