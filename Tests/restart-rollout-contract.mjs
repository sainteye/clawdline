import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

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

console.log('restart rollout contract: POST/ready/replace/complete with abort and exact-404 bootstrap');
