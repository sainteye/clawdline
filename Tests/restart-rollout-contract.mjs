import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
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
