import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const script = readFileSync(new URL('../build.sh', import.meta.url), 'utf8');

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

// `fileURLToPath`, not `.pathname`: a URL keeps the path percent-encoded, and every Clawdline
// worktree lives under `~/Library/Application Support`, so `.pathname` handed `/bin/bash` a path
// containing `%20` that does not exist. This assertion therefore failed — 127, "No such file or
// directory" — in every isolated checkout while passing in the main one, which made the whole
// suite unrunnable for exactly the sessions that run it most.
const syntax = spawnSync('/bin/bash', ['-n', fileURLToPath(new URL('../build.sh', import.meta.url))], {
  encoding: 'utf8',
});
assert.equal(syntax.status, 0, syntax.stderr);

// Mutation proof: the guard must reject a rollout that kills the app before adopting maintenance.
const broken = script.replace(
  '-X POST "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart"',
  '-X PUT "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart"',
);
assert.equal(inspect(broken).postBeforeReplacement, false);

console.log('restart rollout contract: POST/ready/replace/complete with abort and exact-404 bootstrap');
