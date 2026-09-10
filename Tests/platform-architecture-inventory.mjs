#!/usr/bin/env node
// Fixed-tree behavior probes: no Swift compilation, shared index or runtime side effects.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, cpSync, existsSync, readdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { deflateSync, inflateSync } from 'node:zlib';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const toolArg = process.argv.find(value => value.startsWith('--tool='));
const tool = toolArg ? path.resolve(toolArg.slice(7)) : path.join(root, 'tools/platform-architecture-inventory.py');
const selectedCase = process.argv.find(value => value.startsWith('--case='))?.slice(7);
const scratch = mkdtempSync(path.join(tmpdir(), 'platform-inventory-'));
let checks = 0;
const cases = new Map();
function check(name, body) { cases.set(name, body); }
function git(repo, args, input) {
  const result = spawnSync('git', ['-C', repo, ...args], { input, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.trim();
}
function fixture(files) {
  const repo = mkdtempSync(path.join(scratch, 'git-'));
  git(repo, ['init', '-q']);
  const tree = {};
  const blobs = {};
  const trees = {};
  for (const [name, entry] of Object.entries(files)) {
    const value = entry?.content ?? entry;
    const blob = git(repo, ['hash-object', '-w', '--stdin'], value);
    blobs[name] = blob;
    const parts = name.split('/');
    let parent = tree;
    for (const part of parts.slice(0, -1)) parent = parent[part] ??= {};
    parent[parts.at(-1)] = { blob, mode: entry?.mode ?? '100644' };
  }
  function writeTree(node, prefix = '') {
    const lines = Object.keys(node).sort().map(name => {
      const entry = node[name];
      return entry.blob ? `${entry.mode} blob ${entry.blob}\t${name}\0`
        : `040000 tree ${writeTree(entry, prefix + name + '/')}\t${name}\0`;
    }).join('');
    return trees[prefix] = git(repo, ['mktree', '-z'], lines);
  }
  return { repo, tree: writeTree(tree), blobs, trees };
}
const base = {
  'Package.swift': '// fixture package\n',
  'build.sh': '# fixture build\n',
  'test.sh': '# fixture tests\n',
  'tools/swift-source-manifest.sh': '# fixture inventory\n',
  'tools/check-architecture-boundaries.sh': '# fixture guard\n',
  'Sources/Alpha.swift': 'import Foundation\n// Beta in a comment counts as lexical evidence.\nlet lock = NSLock()\n',
  'Sources/Nested/Beta.swift': '#if os(macOS)\nimport AppKit\n#endif\nAlpha.run()\n',
  'Tests/AlphaTests.swift': 'import Foundation\n// test fixture\n',
};
function invoke(subject, extra = [], env = process.env) {
  return spawnSync('python3', [tool, '--repo', subject.repo, '--tree', subject.tree, ...extra],
    { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024, env });
}
function report(subject) {
  const result = invoke(subject);
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(result.stdout);
}
function refused(result, code) {
  assert.equal(result.status, 2, result.stderr);
  assert.equal(result.stdout, '', 'failure must not emit a partial success inventory');
  assert.equal(JSON.parse(result.stderr).error, code);
}
function objectFile(subject, oid) {
  return path.join(subject.repo, '.git/objects', oid.slice(0, 2), oid.slice(2));
}
function rawObject(subject, oid) {
  const raw = inflateSync(readFileSync(objectFile(subject, oid)));
  return raw.subarray(raw.indexOf(0) + 1);
}
function putObject(subject, kind, content, forcedOID) {
  const raw = Buffer.concat([Buffer.from(`${kind} ${content.length}\0`), content]);
  const oid = forcedOID ?? createHash('sha1').update(raw).digest('hex');
  const file = objectFile(subject, oid);
  mkdirSync(path.dirname(file), { recursive: true });
  rmSync(file, { force: true });
  writeFileSync(file, deflateSync(raw));
  return oid;
}
function commitObject(subject, tree = subject.tree) {
  return putObject(subject, 'commit', Buffer.from(`tree ${tree}\nauthor Fixture <fixture@example.invalid> 0 +0000\ncommitter Fixture <fixture@example.invalid> 0 +0000\n\nfixture\n`));
}
function objectSnapshot(subject) {
  const directory = path.join(subject.repo, '.git/objects');
  const files = {};
  function visit(relative) {
    for (const entry of readdirSync(path.join(directory, relative), { withFileTypes: true })) {
      const name = path.join(relative, entry.name);
      if (entry.isDirectory()) visit(name);
      else files[name] = createHash('sha256').update(readFileSync(path.join(directory, name))).digest('hex');
    }
  }
  visit('');
  return files;
}
function shellQuote(value) { return "'" + value.replaceAll("'", "'\\''") + "'"; }
function promisorFixture(configuration = 'remote') {
  const subject = fixture(base);
  const commit = commitObject(subject);
  git(subject.repo, ['update-ref', 'refs/heads/main', commit]);
  const remote = path.join(scratch, path.basename(subject.repo) + '-remote');
  cpSync(subject.repo, remote, { recursive: true });
  const sentinel = path.join(subject.repo, 'uploadpack-called');
  const helper = path.join(subject.repo, 'uploadpack-sentinel.sh');
  writeFileSync(helper, `#!/bin/sh\nprintf invoked > ${shellQuote(sentinel)}\nexec git-upload-pack "$@"\n`, { mode: 0o700 });
  const settings = [
    ['remote.fixture.origin.url', remote], ['remote.fixture.origin.uploadpack', helper],
    ['remote.fixture.origin.fetch', '+refs/heads/*:refs/remotes/fixture/*'],
  ];
  if (configuration === 'extension') settings.push(['extensions.partialClone', 'fixture.origin']);
  else if (configuration === 'included') {
    const include = path.join(subject.repo, 'promisor.config');
    writeFileSync(include, '[remote "fixture.origin"]\n\tpromisor = true\n');
    settings.push(['include.path', include]);
  } else settings.push(['remote.fixture.origin.promisor', 'true']);
  for (const [key, value] of settings) git(subject.repo, ['config', key, value]);
  return { ...subject, commit, sentinel };
}

check('no-promisor-fetch', () => {
  for (const [configuration, missing] of [
    ['remote', 'blob'], ['extension', 'tree'], ['included', 'commit'], ['remote', 'none'],
  ]) {
    const subject = promisorFixture(configuration);
    const oid = { blob: subject.blobs['Sources/Alpha.swift'], tree: subject.trees['Sources/Nested/'], commit: subject.commit }[missing];
    if (oid) rmSync(objectFile(subject, oid));
    const before = objectSnapshot(subject);
    const result = invoke({ ...subject, tree: missing === 'commit' ? subject.commit : subject.tree });
    const after = objectSnapshot(subject);
    console.log(JSON.stringify({ probe: 'no-promisor-fetch', configuration, missing,
      exit: result.status, stdout_bytes: Buffer.byteLength(result.stdout), stderr: result.stderr.trim(),
      uploadpack_invoked: existsSync(subject.sentinel),
      object_additions: Object.keys(after).filter(name => !(name in before)),
      object_bytes_unchanged: JSON.stringify(before) === JSON.stringify(after) }));
    assert.equal(existsSync(subject.sentinel), false, 'inventory must never invoke upload-pack');
    assert.deepEqual(after, before, 'inventory must not add, remove or change object-store bytes');
    refused(result, 'inventory_promisor_unsupported');
  }
});
check('tree-chain', () => {
  for (const prefix of ['Sources/', 'Sources/Nested/', 'Tests/', 'tools/']) {
    const subject = fixture(base);
    const oid = subject.trees[prefix];
    const raw = rawObject(subject, oid);
    // Change a name while retaining all valid leaf OIDs and the original root/subtree OIDs.
    const changed = Buffer.from(raw);
    const nameStart = changed.indexOf(32) + 1;
    changed[nameStart] = changed[nameStart] === 65 ? 90 : 65;
    const actual = putObject(subject, 'tree', changed);
    assert.notEqual(actual, oid);
    putObject(subject, 'tree', changed, oid);
    const result = invoke(subject);
    console.log(JSON.stringify({ probe: 'tree-chain', prefix, requested: subject.tree, corrupt_oid: oid,
      actual_oid: actual, exit: result.status, stdout_bytes: Buffer.byteLength(result.stdout), stderr: result.stderr.trim() }));
    refused(result, 'inventory_tree_unreadable');
  }
});
check('tree-types', () => {
  // A hash-valid blob in a tree-mode entry must not be coerced to a tree by cat-file.
  for (const prefix of ['Sources/', 'Sources/Nested/']) {
    const subject = fixture(base);
    const oldOID = subject.trees[prefix];
    const blobOID = putObject(subject, 'blob', rawObject(subject, oldOID));
    let child = blobOID;
    let current = prefix;
    while (current) {
      const parent = current.slice(0, -1).split('/').slice(0, -1).join('/');
      const parentPrefix = parent ? parent + '/' : '';
      const raw = Buffer.from(rawObject(subject, subject.trees[parentPrefix]));
      const offset = raw.indexOf(Buffer.from(subject.trees[current], 'hex'));
      assert.ok(offset >= 0);
      Buffer.from(child, 'hex').copy(raw, offset);
      child = putObject(subject, 'tree', raw);
      current = parentPrefix;
    }
    refused(invoke({ ...subject, tree: child }), 'inventory_tree_unreadable');
  }
});
check('root-commit', () => {
  for (const kind of ['tree', 'commit']) {
    const subject = fixture(base);
    const oid = kind === 'tree' ? subject.tree : commitObject(subject);
    assert.equal(report({ ...subject, tree: oid }).observed_tree, subject.tree);
    const raw = rawObject(subject, oid);
    putObject(subject, kind, Buffer.concat([raw, Buffer.from('corrupt')]), oid);
    refused(invoke({ ...subject, tree: oid }), 'inventory_git_unreadable');
  }
  const subject = fixture(base);
  refused(invoke({ ...subject, tree: subject.blobs['Package.swift'] }), 'inventory_git_unreadable');
  refused(invoke({ ...subject, tree: commitObject(subject, subject.blobs['Package.swift']) }), 'inventory_tree_unreadable');
});
check('missing-tree', () => {
  for (const prefix of ['Sources/', 'Sources/Nested/', 'tools/']) {
    const subject = fixture(base);
    rmSync(objectFile(subject, subject.trees[prefix]));
    refused(invoke(subject), 'inventory_tree_unreadable');
  }
});
check('git-defenses', () => {
  const subject = fixture(base);
  const before = invoke(subject).stdout;
  const replacement = putObject(subject, 'blob', Buffer.from('import Security\n'));
  git(subject.repo, ['update-ref', `refs/replace/${subject.blobs['Sources/Alpha.swift']}`, replacement]);
  assert.equal(invoke(subject).stdout, before, 'replace refs must not change the fixed bytes');
  const globalConfig = path.join(scratch, 'global.config');
  writeFileSync(globalConfig, '[remote "ambient"]\n\tpromisor = true\n');
  const hostile = { ...process.env, GIT_CONFIG_GLOBAL: globalConfig, GIT_NO_REPLACE_OBJECTS: '0',
    GIT_CONFIG_COUNT: '1', GIT_CONFIG_KEY_0: 'remote.ambient.promisor', GIT_CONFIG_VALUE_0: 'true' };
  assert.equal(invoke(subject, [], hostile).stdout, before, 'ambient configuration must stay excluded');
});

check('facts', () => {
  const subject = fixture(base);
  const value = report(subject);
  assert.equal(value.observed_tree, subject.tree);
  assert.equal(value.counts.production_files, 2);
  assert.equal(value.counts.test_files, 1);
  assert.equal(value.counts.production_lines, 7);
  assert.equal(value.files.find(row => row.path === 'Sources/Alpha.swift').blob, subject.blobs['Sources/Alpha.swift']);
  assert.deepEqual(value.production_imports.AppKit, ['Sources/Nested/Beta.swift']);
  assert.equal(value.lexical_edges.find(row => row.basename === 'Beta').count, 1);
  assert.equal(value.lexical_edges.find(row => row.basename === 'Alpha').count, 1);
  assert.match(value.limits.join('\n'), /comments, strings and inactive/);
  assert.equal(value.generator_sha256, createHash('sha256').update(readFileSync(tool)).digest('hex'));
});
check('fixed-tree', () => {
  const subject = fixture(base);
  const before = invoke(subject).stdout;
  mkdirSync(path.join(subject.repo, 'Sources'));
  writeFileSync(path.join(subject.repo, 'Sources/Alpha.swift'), 'dirty bytes');
  writeFileSync(path.join(subject.repo, 'Sources/Untracked.swift'), 'import Security');
  assert.equal(invoke(subject).stdout, before);
  const changed = fixture({ ...base, 'Sources/Alpha.swift': 'import Security\n' });
  assert.notEqual(report(changed).inventory_sha256, report(subject).inventory_sha256);
  const reordered = fixture(Object.fromEntries(Object.entries(base).reverse()));
  assert.equal(invoke(reordered).stdout, before, 'paths and fixture location must not affect output');
  assert.equal(invoke(subject, [], { ...process.env, GIT_DIR: path.join(changed.repo, '.git') }).stdout,
    before, 'ambient Git directory must not redirect the requested subject');
});
check('fixed-id', () => {
  const subject = fixture(base);
  refused(invoke({ ...subject, tree: 'HEAD' }), 'inventory_fixed_tree_required');
  refused(invoke({ ...subject, tree: 'f'.repeat(40) }), 'inventory_git_unreadable');
});
check('empty', () => {
  refused(invoke(fixture({})), 'inventory_empty');
});
check('incomplete', () => {
  for (const name of ['Sources/Alpha.swift', 'Tests/AlphaTests.swift', 'Package.swift']) {
    const files = { ...base };
    delete files[name];
    if (name.startsWith('Sources/')) delete files['Sources/Nested/Beta.swift'];
    refused(invoke(fixture(files)), 'inventory_incomplete');
  }
});
check('unreadable-blob', () => {
  const subject = fixture(base);
  const oid = subject.blobs['Sources/Alpha.swift'];
  rmSync(path.join(subject.repo, '.git/objects', oid.slice(0, 2), oid.slice(2)));
  refused(invoke(subject), 'inventory_blob_unreadable');
  // A readable object with bytes belonging to a different hash is also untrustworthy.
  const corrupt = fixture(base);
  const corruptOID = corrupt.blobs['Sources/Alpha.swift'];
  const wrong = Buffer.from('import Security\n');
  rmSync(path.join(corrupt.repo, '.git/objects', corruptOID.slice(0, 2), corruptOID.slice(2)));
  writeFileSync(path.join(corrupt.repo, '.git/objects', corruptOID.slice(0, 2), corruptOID.slice(2)),
    deflateSync(Buffer.concat([Buffer.from(`blob ${wrong.length}\0`), wrong])));
  refused(invoke(corrupt), 'inventory_blob_unreadable');
});
check('nonregular', () => {
  refused(invoke(fixture({ ...base, 'Sources/Alpha.swift': { mode: '120000', content: '../secret' } })),
    'inventory_nonregular_source');
  refused(invoke(fixture({ ...base, 'Sources/external': { mode: '120000', content: '../external' } })),
    'inventory_nonregular_source');
});
check('invalid-source', () => {
  for (const content of ['', ' \n', Buffer.from([0xff]), 'import Foundation\0']) {
    refused(invoke(fixture({ ...base, 'Sources/Alpha.swift': content })), 'inventory_source_unreadable');
  }
});
check('freshness', () => {
  const subject = fixture(base);
  const output = path.join(scratch, 'inventory.md');
  const result = invoke(subject, ['--format', 'markdown']);
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, new RegExp(subject.tree));
  assert.match(result.stdout, /not resolved symbols/);
  writeFileSync(output, result.stdout);
  assert.equal(invoke(subject, ['--format', 'markdown', '--check', output]).status, 0);
  writeFileSync(output, result.stdout.replace(subject.tree, '0'.repeat(40)));
  refused(invoke(subject, ['--format', 'markdown', '--check', output]), 'inventory_stale');
  refused(invoke(subject, ['--check', path.join(scratch, 'absent')]), 'inventory_check_unreadable');
});

try {
  if (selectedCase && !cases.has(selectedCase)) throw new Error(`unknown case: ${selectedCase}`);
  for (const [name, body] of cases) {
    if (selectedCase && name !== selectedCase) continue;
    body();
    checks++;
    console.log(`✓ ${name}`);
  }
  assert.ok(checks > 0);
  console.log(`platform architecture inventory: ${checks} checks passed`);
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
