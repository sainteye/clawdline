#!/usr/bin/env node
// A conservative build plan. Go tests and repository guards still run for
// documentation and console edits; only redundant vet/cross-build work skips.
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

export function classify(paths, full = false, known = true) {
  if (full || !known || paths.length === 0) return { go: true, web: true, reason: full ? 'explicit full check' : 'no reliable changed-path evidence' };
  let go = false, web = false;
  for (const path of paths) {
    if (/\.go$|(^|\/)go\.(mod|sum)$/.test(path)) go = true;
    if (path.startsWith('web/') || path.startsWith('plugins/')) web = true;
    else if (path.startsWith('docs/') && /\.(md|png|jpg|jpeg|svg|webp)$/.test(path)) continue;
    else if (/^(README(?:\.[\w-]+)?\.md|CHANGELOG\.md)$/.test(path)) continue;
    else { go = true; if (path.startsWith('docs/')) web = true; }
    // Generator, tooling, policy and unknown paths can affect both products.
    if (!path.startsWith('web/') && !path.startsWith('docs/') && !path.startsWith('plugins/') &&
        !/^(internal\/|cmd\/|skills\/|api\/v1\/|go\.(mod|sum)$|README(?:\.[\w-]+)?\.md$|CHANGELOG\.md$)/.test(path)) web = true;
  }
  return { go, web, reason: go ? 'Go, tooling, policy or unclassified inputs changed' : 'only console/documentation inputs changed' };
}

export function changedPaths(run = (args) => spawnSync('git', args, { encoding: 'utf8' })) {
  const base = run(['merge-base', 'HEAD', 'main']);
  if (base.status !== 0 || !base.stdout.trim()) return { paths: [], known: false };
  const diff = run(['diff', '--name-only', '--no-renames', '-z', base.stdout.trim(), '--']);
  const staged = run(['diff', '--cached', '--name-only', '--no-renames', '-z', base.stdout.trim(), '--']);
  const untracked = run(['ls-files', '--others', '--exclude-standard', '-z']);
  if (diff.status !== 0 || staged.status !== 0 || untracked.status !== 0) return { paths: [], known: false };
  return { paths: [...new Set((diff.stdout + staged.stdout + untracked.stdout).split('\0').filter(Boolean))], known: true };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { paths, known } = changedPaths();
  const plan = classify(paths, process.argv.includes('--full'), known);
  if (process.argv.includes('--web')) plan.web = true;
  if (process.argv.includes('--json')) console.log(JSON.stringify({ ...plan, paths }));
  else console.log(`${plan.go ? 'yes' : 'no'}\n${plan.web ? 'yes' : 'no'}\n${plan.reason}`);
}
