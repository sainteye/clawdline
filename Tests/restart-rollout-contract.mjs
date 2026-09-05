import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { spawn, spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { cpus, tmpdir } from 'node:os';

const script = readFileSync(new URL('../build.sh', import.meta.url), 'utf8');

/** **A URL's `pathname` is not a path.** It is still percent-encoded, so a checkout under
 *  `~/Library/Application Support/…` hands `/bin/bash` three literal characters where a space
 *  belongs and gets back `No such file or directory` — which reads as a missing script rather
 *  than as an encoding bug, and is why this sat unnoticed. It is not a rare directory either:
 *  every Clawdline worktree lives under that path, so this whole file was closed to an entire
 *  class of checkout while looking green everywhere else. `readFileSync` above hides the
 *  asymmetry by accepting a URL and decoding it itself. */
function localPath(url) {
  return fileURLToPath(url);
}

function inspect(text) {
  const post = text.indexOf('-X POST "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart"');
  const replace = text.indexOf('pkill -x Clawdline');
  const bootstrap = text.indexOf('if [ "$MAINTENANCE_STATUS" = 404 ]');
  const ready = text.indexOf('[ "$PHASE" = ready ]');
  const complete = text.indexOf('[ "$PHASE" = complete ]');
  const abort = text.indexOf('-X DELETE');
  return {
    postBeforeReplacement: post >= 0 && replace >= 0 && post < replace,
    exactBootstrap: bootstrap > post && bootstrap < replace,
    readyBeforeReplacement: ready > post && ready < replace,
    completeAfterReplacement: complete > replace,
    explicitAbort: abort >= 0,
  };
}

assert.deepEqual(inspect(script), {
  postBeforeReplacement: true,
  exactBootstrap: true,
  readyBeforeReplacement: true,
  completeAfterReplacement: true,
  explicitAbort: true,
});

const syntax = spawnSync('/bin/bash', ['-n', localPath(new URL('../build.sh', import.meta.url))], {
  encoding: 'utf8',
});
assert.equal(syntax.status, 0, syntax.stderr);

// The space is the whole point, and it is a real directory with a real file in it rather than a
// string comparison: a check that only looked at the spelling would have passed before the fix
// too. Reverting `localPath` to `new URL(url).pathname` turns exactly this assertion red and
// leaves the one above it green, which is the asymmetry that hid the bug.
const spaced = mkdtempSync(join(tmpdir(), 'clawdline restart rollout '));
try {
  const copied = join(spaced, 'build.sh');
  writeFileSync(copied, script);
  const spacedSyntax = spawnSync('/bin/bash', ['-n', localPath(pathToFileURL(copied))], {
    encoding: 'utf8',
  });
  assert.equal(spacedSyntax.status, 0,
    `a checkout path containing a space must still reach bash: ${spacedSyntax.stderr}`);
} finally {
  rmSync(spaced, { recursive: true, force: true });
}

// Mutation proof: the guard must reject a rollout that kills the app before adopting maintenance.
const broken = script.replace(
  '-X POST "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart"',
  '-X PUT "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart"',
);
assert.equal(inspect(broken).postBeforeReplacement, false);

// **`bash -n` cannot see this one, so this check runs it.** `"${arr[@]}"` is perfectly legal
// syntax; on bash 3.2 — which is what `/bin/bash` is on macOS — it fails at *run* time, under
// `set -u`, only when the array is empty. So the syntax check above is structurally incapable of
// catching it, and the one guard this file had was that syntax check.
//
// It reached `main` in a state where `./build.sh` died with `compile_jobs[@]: unbound variable`
// before `swiftc` ran, on the path where no parallelism budget was granted — which is the default,
// and is also the fallback taken when the broker is not answering, i.e. exactly after the crash
// this whole feature exists to prevent. The sister file had it right five hundred lines away:
// `test.sh` writes `${clawdline_suite_jobs_flags[@]+"${…[@]}"}`. **One idiom, two files, one
// change, and nothing in the toolchain says a word.**
//
// The block and the expansion are lifted from `build.sh` rather than retyped, so this asserts what
// the script will actually do rather than what it is supposed to say.
// Lifted by the markers rather than by hunting for the next `fi`. The old extraction sliced from
// `compile_jobs=()` to the first `\nfi\n`, which worked only while every branch of the block ended
// on its own line; the first single-line `if … ; fi` inside it silently swallowed the `swiftc`
// invocation below and the harness died on an unbound `BIN`. A block that has to be run on its own
// needs boundaries that say where it ends.
const CEIL_OPEN = '# >>> clawdline compile ceiling >>>';
const CEIL_CLOSE = '# <<< clawdline compile ceiling <<<';
const jobsBlockStart = script.indexOf(CEIL_OPEN);
assert.notEqual(jobsBlockStart, -1, 'build.sh no longer carries the compile-ceiling markers; update this check');
assert.equal(script.split('\n').filter((l) => l === CEIL_OPEN).length, 1,
  'build.sh must carry exactly one compile-ceiling opening marker');
const jobsBlock = script.slice(jobsBlockStart,
  script.indexOf(CEIL_CLOSE) + CEIL_CLOSE.length);
const jobsExpansion = script.split('\n').find(
  (line) => line.includes('compile_jobs[@]') && !line.includes('compile_jobs=('));
assert.ok(jobsExpansion, 'build.sh no longer expands compile_jobs into the compiler invocation');

function expandsCleanly(block, expansion) {
  const harness = [
    'set -euo pipefail',
    'CLAWDLINE_SUITE_JOBS=""',
    'CLAWDLINE_SUITE_JOBS_SOURCE="no budget granted"',
    block,
    `printf ' [%s]' swiftc ${expansion.trim().replace(/\\$/, '')} -o bin`,
    'printf "\\n"',
  ].join('\n');
  return spawnSync('/bin/bash', ['-c', harness], { encoding: 'utf8' });
}

const noBudget = expandsCleanly(jobsBlock, jobsExpansion);
assert.equal(noBudget.status, 0,
  `build.sh must reach swiftc with no parallelism budget granted: ${noBudget.stderr}`);
// An unset budget used to add no flag at all, which on this machine meant one job — measured, not
// assumed. It now derives `min(8, hw.ncpu)`: 103 sources with `-O` took 169 s at one job and 37 s
// at eight, at 0.40 GiB per frontend and 1.34 GiB for all of them together. See
// `docs/suite-runtime.md`.
const derivedCeiling = Math.min(8, cpus().length);
assert.match(noBudget.stdout, new RegExp(`\\[swiftc\\] \\[-j\\] \\[${derivedCeiling}\\] \\[-o\\] \\[bin\\]`),
  `an unset budget must derive a ceiling and pass it: ${noBudget.stdout}`);

// **The mutation proof below needs an empty array, and the block no longer produces one.** That is
// worth saying rather than quietly dropping: this guard exists because `./build.sh` once died with
// `compile_jobs[@]: unbound variable` before `swiftc` ran, and it only ever caught that because the
// default happened to leave the array empty. Resting a guard on an incidental property of the
// default is how it stops guarding — so the array is now emptied on purpose, and what is pinned is
// the expansion form itself, which is the thing that was wrong.
function expandsWithEmptyArray(expansion) {
  const harness = [
    'set -euo pipefail',
    'compile_jobs=()',
    `printf ' [%s]' swiftc ${expansion.trim().replace(/\\$/, '')} -o bin`,
    'printf "\\n"',
  ].join('\n');
  return spawnSync('/bin/bash', ['-c', harness], { encoding: 'utf8' });
}

const guardedEmpty = expandsWithEmptyArray(jobsExpansion);
assert.equal(guardedEmpty.status, 0,
  `the guarded expansion must survive an empty array under set -u: ${guardedEmpty.stderr}`);
assert.match(guardedEmpty.stdout, /\[swiftc\] \[-o\] \[bin\]/,
  `and must add nothing when there is nothing to add: ${guardedEmpty.stdout}`);

// The mutation that actually happened: put the unguarded expansion back and this must go red while
// `bash -n` above stays green.
const unguarded = expandsWithEmptyArray('"${compile_jobs[@]}"');
assert.notEqual(unguarded.status, 0,
  'the unguarded expansion must fail here, or this check is not testing anything');
assert.match(unguarded.stderr, /unbound variable/,
  `the mutation must fail for the reason this guards: ${unguarded.stderr}`);

console.log('restart rollout contract: POST/ready/replace/complete, abort, exact-404 bootstrap, and a compile line that derives its own ceiling');

// ---------------------------------------------------------------------------------------------
// The restart-maintenance preflight, lifted and driven against a stub app.
//
// **This half exists because the other half could not be proved.** Everything above reads the
// script as text: it can say a `DELETE` is spelled somewhere, and it said so on 2026-09-05 while
// the recovery those characters describe did not exist. What went wrong that night was behaviour,
// not spelling — a five-second client timeout read as a server refusal, an accepted intent left
// standing, and every later `./build.sh` meeting `409 restart_in_progress` with no way out.
//
// So the block is lifted by its markers and *run*, against a stub HTTP app that answers the three
// maintenance routes and `/v1/health` and records every request it was sent. It never talks to the
// Clawdline the person is using: the stub binds its own port on 127.0.0.1, and `POST`ing the real
// app's route is the exact harm this whole change is about — it closes dispatch admission for the
// whole machine.
//
// Every scenario below carries its control: the mutation that takes the new path back out, and the
// failure it produces when it is gone. A recovery path that cannot be made to fail looks, from
// here, exactly like a recovery path that is not there.
const MAINT_OPEN = '# >>> clawdline restart maintenance >>>';
const MAINT_CLOSE = '# <<< clawdline restart maintenance <<<';
assert.equal(script.split('\n').filter((l) => l === MAINT_OPEN).length, 1,
  'build.sh must carry exactly one restart-maintenance opening marker');
const maintenanceBlock = script.slice(script.indexOf(MAINT_OPEN),
  script.indexOf(MAINT_CLOSE) + MAINT_CLOSE.length);

// **The number, and where it came from.** `--max-time 5` on the POST plus a 120-attempt poll was
// 125 seconds of client patience against a drain measured at 146 — so the old script could not
// have waited that drain out even if the POST had answered instantly. A budget is only defensible
// beside the measurement it was chosen against, so both are pinned: the value, and the fact that
// the comment still carries the reading it came from.
const budgetMatch = maintenanceBlock.match(/MAINTENANCE_BUDGET="\$\{CLAWDLINE_MAINTENANCE_BUDGET:-(\d+)\}"/);
assert.ok(budgetMatch, 'the maintenance block must derive one admission budget with an override');
const MEASURED_DRAIN_SECONDS = 146;
assert.ok(Number(budgetMatch[1]) >= 2 * MEASURED_DRAIN_SECONDS,
  `the admission budget must outlast the ${MEASURED_DRAIN_SECONDS}s drain measured on 2026-09-05,`
  + ` with margin; it is ${budgetMatch[1]}s`);
assert.match(maintenanceBlock, new RegExp(`${MEASURED_DRAIN_SECONDS} seconds|drained_at=1788612390`),
  'the block must keep the measurement its budget was chosen against, not just the number');
// And the POST must spend that budget rather than a literal of its own.
assert.ok(maintenanceBlock.includes('--max-time "$MAINTENANCE_BUDGET" -o "$MAINTENANCE_REPLY"'),
  'the POST must spend the shared budget');
// Mutation: put the old five seconds back and the check above must reject it.
const fiveSecondBudget = maintenanceBlock
  .replace(`CLAWDLINE_MAINTENANCE_BUDGET:-${budgetMatch[1]}}`, 'CLAWDLINE_MAINTENANCE_BUDGET:-5}')
  .match(/MAINTENANCE_BUDGET="\$\{CLAWDLINE_MAINTENANCE_BUDGET:-(\d+)\}"/);
assert.ok(Number(fiveSecondBudget[1]) < 2 * MEASURED_DRAIN_SECONDS,
  'the five seconds that timed out on 2026-09-05 must not satisfy the budget check above');

// The stub app. Three maintenance routes, `/v1/health`, a request log, and one knob per scenario.
const STUB = `import json, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

cfg = json.load(open(sys.argv[1]))
port_path, log_path = sys.argv[2], sys.argv[3]
state = {"intent": cfg.get("intent"), "polls": 0}
lock = threading.Lock()

def note(entry):
    with open(log_path, "a") as fh:
        fh.write(json.dumps(entry) + "\\n")

def fresh(request_id):
    now = int(time.time())
    return {"request_id": request_id, "phase": cfg.get("post_phase", "ready"),
            "requested_instance_id": cfg.get("instance", "stub-instance"),
            "requested_at": now, "outstanding": 0, "channels": {},
            "admission_closed": True, "safe_to_replace": False,
            "replacement_before_safe": False, "reconciliation_timed_out": False,
            "unresolved_task_ids": []}

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def reply(self, code, obj):
        body = json.dumps(obj).encode()
        try:
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except Exception:
            pass

    def do_GET(self):
        note({"method": "GET", "path": self.path})
        if self.path == "/v1/health":
            return self.reply(200, {"ok": True, "instance": cfg.get("instance", "stub-instance")})
        if self.path != "/v1/orchestrator/maintenance/restart":
            return self.reply(404, {"error": {"code": "not_found", "message": "no such route"}})
        with lock:
            state["polls"] += 1
            intent = state["intent"]
            if intent is None:
                return self.reply(404, {"error": {"code": "restart_not_found",
                                                  "message": "No restart maintenance intent exists."}})
            after = cfg.get("ready_after_polls")
            if after is not None and intent.get("phase") == "draining" and state["polls"] >= after:
                intent["phase"] = "ready"
                intent["drained_at"] = int(time.time())
            return self.reply(200, {"restart": dict(intent)})

    def read_body(self):
        length = int(self.headers.get("Content-Length") or 0)
        try:
            return json.loads(self.rfile.read(length) or b"{}")
        except Exception:
            return {}

    def do_POST(self):
        request_id = self.read_body().get("request_id")
        note({"method": "POST", "path": self.path, "request_id": request_id})
        mode = cfg.get("post", "accept")
        if mode == "hang":
            # Accepted, and the answer never observed: the whole shape of 2026-09-05 20:44.
            with lock:
                state["intent"] = fresh(request_id)
            time.sleep(cfg.get("hang_seconds", 5))
            mode = "accept"
        if mode == "refuse":
            return self.reply(503, {"error": {"code": "restart_store_failed",
                                              "message": "The durable restart intent could not be written."}})
        with lock:
            current = state["intent"]
            if (current and current.get("request_id") != request_id
                    and current.get("phase") not in ("complete", "aborted")):
                return self.reply(409, {"error": {"code": "restart_in_progress",
                                                  "message": "A different restart maintenance intent is already active.",
                                                  "restart": current}})
            state["intent"] = fresh(request_id)
            return self.reply(200, {"restart": dict(state["intent"])})

    def do_DELETE(self):
        request_id = self.read_body().get("request_id")
        note({"method": "DELETE", "path": self.path, "request_id": request_id})
        with lock:
            current = state["intent"]
            if current is None:
                return self.reply(404, {"error": {"code": "restart_not_found",
                                                  "message": "No restart maintenance intent exists."}})
            if current.get("request_id") != request_id:
                return self.reply(409, {"error": {"code": "restart_in_progress",
                                                  "message": "A different restart maintenance intent is active."}})
            current["phase"] = "aborted"
            current["admission_closed"] = False
            state["intent"] = None
            return self.reply(200, {"restart": dict(current)})

server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(port_path, "w") as fh:
    fh.write(str(server.server_address[1]))
server.serve_forever()
`;

/** A pid that is certainly not running: a shell that has already exited, confirmed dead here. */
function deadPid() {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const born = spawnSync('/bin/sh', ['-c', 'echo $$'], { encoding: 'utf8' });
    const pid = Number(born.stdout.trim());
    if (!Number.isInteger(pid)) continue;
    try {
      process.kill(pid, 0);
    } catch {
      return String(pid);
    }
  }
  throw new Error('could not obtain a pid that is not running');
}

function readLog(path) {
  if (!existsSync(path)) return [];
  return readFileSync(path, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line));
}

/**
 * Run the lifted block against a stub app.
 *
 * `HOME` is redirected as well as `CLAWDLINE_MAINTENANCE_STATE_FILE`, so a mistake in one of them
 * cannot reach the state file of the person running the suite; `PORT` is the stub's, never 7717.
 */
function driveBlock({ config, block = maintenanceBlock, remembered, env = {} }) {
  const room = mkdtempSync(join(tmpdir(), 'clawdline-maintenance-'));
  const stubPath = join(room, 'stub.py');
  const configPath = join(room, 'config.json');
  const portPath = join(room, 'port');
  const logPath = join(room, 'requests.log');
  const tokenPath = join(room, 'orchestrator-token');
  const statePath = join(room, 'last-build-maintenance');
  const stage = join(room, 'stage');
  mkdirSync(stage);
  writeFileSync(stubPath, STUB);
  writeFileSync(configPath, JSON.stringify(config));
  writeFileSync(tokenPath, 'stub-token\n');
  if (remembered) {
    writeFileSync(statePath, `request_id=${remembered.request_id}\npid=${remembered.pid}\n`);
  }
  const stub = spawn('/usr/bin/python3', [stubPath, configPath, portPath, logPath], {
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  try {
    const started = Date.now();
    let port = '';
    while (Date.now() - started < 10_000) {
      if (existsSync(portPath)) {
        port = readFileSync(portPath, 'utf8').trim();
        if (port) break;
      }
      spawnSync('/bin/sleep', ['0.05']);
    }
    assert.ok(port, 'the stub app never reported a port');
    const harness = [
      'set -euo pipefail',
      'WAS_RUNNING=1',
      `PORT=${port}`,
      `TOKEN_FILE=${JSON.stringify(tokenPath)}`,
      `STAGE_ROOT=${JSON.stringify(stage)}`,
      block,
      'echo "BLOCK_END active=${MAINTENANCE_ACTIVE} id=${MAINTENANCE_REQUEST_ID}"',
    ].join('\n');
    const run = spawnSync('/bin/bash', ['-c', harness], {
      encoding: 'utf8',
      env: {
        ...process.env,
        HOME: room,
        CLAWDLINE_MAINTENANCE_STATE_FILE: statePath,
        CLAWDLINE_MAINTENANCE_CONNECT_SECONDS: '2',
        CLAWDLINE_MAINTENANCE_BUDGET: '4',
        ...env,
      },
    });
    return { run, requests: readLog(logPath), statePath, room };
  } finally {
    stub.kill('SIGKILL');
  }
}

// --- 1. The incident itself: an accepted request whose answer was never observed. --------------
// The stub takes the POST, registers the intent, and then does not answer inside the client's
// patience. The block must not call that a refusal: it must say so, print the id, read the
// standing intent, recognise its own, and carry on inside the window it opened.
const unobserved = driveBlock({
  config: { post: 'hang', hang_seconds: 6, post_phase: 'ready' },
  env: { CLAWDLINE_MAINTENANCE_BUDGET: '2' },
});
assert.equal(unobserved.run.status, 0,
  `an accepted-but-unobserved request must not end the build: ${unobserved.run.stdout}${unobserved.run.stderr}`);
assert.match(unobserved.run.stdout, /This is not a refusal/,
  'a client timeout must be reported as an unobserved answer, not as a refusal');
assert.match(unobserved.run.stdout, /it was accepted after all/,
  'the block must resolve the ambiguity by reading, not by guessing');
assert.match(unobserved.run.stdout, /BLOCK_END active=1 id=[0-9a-f-]{36}$/m,
  'and must come out holding the window and its id');
const postedId = unobserved.requests.find((r) => r.method === 'POST').request_id;
assert.match(unobserved.run.stdout, new RegExp(`request_id: ${postedId}`),
  'the id of an unobserved request must be printed, or nothing can end it by hand');

// Control: take the new branch out and the run reproduces 2026-09-05 exactly — "refused",
// HTTP 000, exit 1, with the window it just opened left standing.
const conflated = driveBlock({
  config: { post: 'hang', hang_seconds: 6, post_phase: 'ready' },
  block: maintenanceBlock.replace('if [ "$MAINTENANCE_CURL" != 0 ]; then', 'if false; then'),
  env: { CLAWDLINE_MAINTENANCE_BUDGET: '2' },
});
assert.notEqual(conflated.run.status, 0,
  'without the split, an unobserved answer must end the build — or this scenario proves nothing');
assert.match(conflated.run.stdout, /refused by the app \(HTTP 000\)/,
  `the control must fail the way it failed that night: ${conflated.run.stdout}`);
assert.ok(!conflated.requests.some((r) => r.method === 'DELETE'),
  'and must leave the intent standing, which is what blocked every later build');

// --- 2. This machine's own abandoned intent is reclaimed. --------------------------------------
const abandonedId = '11111111-1111-4111-8111-111111111111';
const reclaimed = driveBlock({
  config: {
    intent: {
      request_id: abandonedId, phase: 'ready', requested_instance_id: 'stub-instance',
      requested_at: Math.floor(Date.now() / 1000) - 30, outstanding: 0, channels: {},
      admission_closed: true, drained_at: Math.floor(Date.now() / 1000) - 30,
    },
    post_phase: 'ready',
  },
  remembered: { request_id: abandonedId, pid: deadPid() },
});
assert.equal(reclaimed.run.status, 0,
  `an intent this machine wrote, whose writer is gone, must not block the build: ${reclaimed.run.stdout}${reclaimed.run.stderr}`);
assert.match(reclaimed.run.stdout, /reclaiming it: this machine wrote it/,
  'and the reason it was reclaimed must be said out loud');
assert.deepEqual(
  reclaimed.requests.filter((r) => r.method === 'DELETE').map((r) => r.request_id),
  [abandonedId],
  'exactly the abandoned id is ended, and nothing else');
assert.equal(reclaimed.requests.filter((r) => r.method === 'POST').length, 1,
  'and a fresh window is then opened');

// Control: no preflight, which is what the script did before — straight into `409` and out.
const unreclaimed = driveBlock({
  config: {
    intent: {
      request_id: abandonedId, phase: 'ready', requested_instance_id: 'stub-instance',
      requested_at: Math.floor(Date.now() / 1000) - 30, outstanding: 0, channels: {},
      admission_closed: true,
    },
    post_phase: 'ready',
  },
  block: maintenanceBlock.replace('if [ "$MAINTENANCE_STANDING_STATUS" = 200 ]; then', 'if false; then'),
  remembered: { request_id: abandonedId, pid: deadPid() },
});
assert.notEqual(unreclaimed.run.status, 0,
  'without the preflight the build must die on the standing intent, or the preflight proves nothing');
assert.match(unreclaimed.run.stdout, /restart_in_progress/,
  `the control must die the way the second build died that night: ${unreclaimed.run.stdout}`);

// --- 3. Somebody else's live window is never ended. --------------------------------------------
// The dangerous direction. A `./build.sh` that is still inside its own maintenance window has a
// live process behind it, and nothing here may abort that — not the intent, not by age, not at all.
const liveId = '22222222-2222-4222-8222-222222222222';
const holder = spawn('/bin/sleep', ['30'], { stdio: 'ignore' });
let live;
try {
  live = driveBlock({
    config: {
      intent: {
        request_id: liveId, phase: 'draining', requested_instance_id: 'stub-instance',
        requested_at: Math.floor(Date.now() / 1000) - 5, outstanding: 1,
        channels: { '%92': 1 }, admission_closed: true,
      },
    },
    remembered: { request_id: liveId, pid: String(holder.pid) },
    // Even told that everything is stale, a live writer must still be untouchable.
    env: { CLAWDLINE_MAINTENANCE_STALE_SECONDS: '0' },
  });
} finally {
  holder.kill('SIGKILL');
}
assert.notEqual(live.run.status, 0, 'a live maintenance window must stop this build');
assert.match(live.run.stdout, new RegExp(`another \\./build\\.sh \\(pid ${holder.pid}\\)`),
  `and must name who is holding it: ${live.run.stdout}`);
assert.ok(!live.requests.some((r) => r.method === 'DELETE' || r.method === 'POST'),
  'and must send neither a DELETE nor a POST — the window is not this build\'s to touch');

// --- 4. An unclaimed intent is refused while it is young, and reclaimed only once it is old. ----
const orphanId = '33333333-3333-4333-8333-333333333333';
const orphanConfig = () => ({
  intent: {
    request_id: orphanId, phase: 'draining', requested_instance_id: 'stub-instance',
    requested_at: Math.floor(Date.now() / 1000) - 60, outstanding: 1, channels: {},
    admission_closed: true,
  },
  post_phase: 'ready',
});
const young = driveBlock({ config: orphanConfig() });
assert.notEqual(young.run.status, 0, 'an intent nobody claims must not be ended while it is young');
assert.ok(!young.requests.some((r) => r.method === 'DELETE'),
  'nothing may be aborted on age alone before the age is reached');
assert.match(young.run.stdout, new RegExp(`-d '\\{"request_id":"${orphanId}"\\}'`),
  'and the exact command that ends it by hand must be printed');

// The gate is load-bearing rather than decorative: move it and the same intent is reclaimed.
const old = driveBlock({
  config: orphanConfig(),
  env: { CLAWDLINE_MAINTENANCE_STALE_SECONDS: '10' },
});
assert.equal(old.run.status, 0,
  `past the staleness gate the same intent must be reclaimed: ${old.run.stdout}${old.run.stderr}`);
assert.deepEqual(old.requests.filter((r) => r.method === 'DELETE').map((r) => r.request_id),
  [orphanId], 'and it is the standing id that is ended');

// --- 5. A server that answers "no" is still reported as a refusal. -----------------------------
const refused = driveBlock({ config: { post: 'refuse' } });
assert.notEqual(refused.run.status, 0, 'a typed refusal must stop the build');
assert.match(refused.run.stdout, /refused by the app \(HTTP 503\)/,
  'a refusal must say the app refused it, which a timeout must not');
assert.match(refused.run.stdout, /restart_store_failed/,
  'and must carry the typed code the app gave');
assert.match(refused.run.stdout, /request_id: [0-9a-f-]{36}/,
  'with the id, because a refusal can still leave one behind');

console.log('restart maintenance: an unobserved answer is not a refusal, an abandoned intent is reclaimed, a live one is untouchable');
