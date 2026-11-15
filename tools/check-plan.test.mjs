import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { classify, changedPaths } from './check-plan.mjs';

test('console and documents skip cross-builds, while tooling and unknown files fail closed', () => {
  assert.equal(classify(['web/console/src/App.tsx']).go, false);
  assert.equal(classify(['plugins/session-absurd/manifest.json']).go, false);
  assert.equal(classify(['plugins/session-absurd/manifest.json']).web, true);
  assert.equal(classify(['docs/architecture.md']).go, false);
  assert.equal(classify(['docs/example.go']).go, true);
  for (const path of ['tools/check.sh', 'AGENTS.md', '.github/workflows/ci.yml', 'new-input']) {
    assert.deepEqual([classify([path]).go, classify([path]).web], [true, true]);
  }
  assert.deepEqual([classify(['internal/app/app.go']).go, classify(['internal/app/app.go']).web], [true, false]);
  assert.equal(classify(['skills/clawdline/guide.md']).go, true);
  assert.deepEqual([classify(['api/v1/routes.json']).go, classify(['api/v1/routes.json']).web], [true, false]);
  assert.equal(classify(['web/core/index.ts'], true).go, true);
  assert.equal(classify([], false, false).web, true);
  assert.equal(classify([]).go, true);
});

test('real Git changes include committed, staged, unstaged, deleted, renamed and untracked paths', () => {
  const dir = mkdtempSync(join(tmpdir(), 'check-plan-test-'));
  const git = (args) => {
    const result = spawnSync('git', args, { cwd: dir, encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr);
    return result;
  };
  try {
    git(['init', '-b', 'main']);
    git(['config', 'user.name', 'Test']); git(['config', 'user.email', 'test@example.invalid']);
    mkdirSync(join(dir, 'web'));
    for (const name of ['staged', 'staged-reverted', 'unstaged', 'deleted', 'renamed']) writeFileSync(join(dir, 'web', name), 'before');
    git(['add', '.']); git(['commit', '-m', 'Initial fixture']); git(['checkout', '-b', 'task']);
    writeFileSync(join(dir, 'web', 'committed'), 'after'); git(['add', '.']); git(['commit', '-m', 'Committed fixture']);
    writeFileSync(join(dir, 'web', 'staged'), 'after'); git(['add', '.']);
    writeFileSync(join(dir, 'web', 'staged-reverted'), 'after'); git(['add', '.']);
    writeFileSync(join(dir, 'web', 'staged-reverted'), 'before');
    writeFileSync(join(dir, 'web', 'unstaged'), 'after');
    git(['rm', 'web/deleted']); git(['mv', 'web/renamed', 'web/new name']);
    writeFileSync(join(dir, 'untracked\nfile'), 'after');
    const result = changedPaths(git);
    assert.equal(result.known, true);
    for (const path of ['web/committed', 'web/staged', 'web/staged-reverted', 'web/unstaged', 'web/deleted', 'web/renamed', 'web/new name', 'untracked\nfile']) assert.ok(result.paths.includes(path), path);
    assert.equal(classify(result.paths).go, true);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test('unreadable Git state falls back to all checks', () => {
  assert.equal(changedPaths(() => ({ status: 1, stdout: '' })).known, false);
  let calls = 0;
  assert.equal(changedPaths(() => ++calls === 1 ? { status: 0, stdout: 'base' } : { status: 1, stdout: '' }).known, false);
});
