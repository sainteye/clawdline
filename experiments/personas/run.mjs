#!/usr/bin/env node
// Persona blind test runner. Run from this directory:
//   node run.mjs run      [--model M] [--personas DIR] [--reps N] [--concurrency N] [--seed S]
//   node run.mjs grade    [--model M] [--concurrency N]
//   node run.mjs analyze
// Everything it reads and writes is relative to this directory. Requires the `claude` CLI,
// `git` (for `git diff --no-index`) and `go` (to run the minimal-change fixture's tests).
import { spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const HERE = path.dirname(new URL(import.meta.url).pathname);
process.chdir(HERE);

const ROLES = {
  'code-reviewer': { tools: ['Read', 'Grep', 'Glob'], edits: false },
  'minimal-change': { tools: ['Read', 'Grep', 'Glob', 'Edit', 'Write', 'Bash'], edits: true, goTest: true },
  seo: { tools: ['Read', 'Grep', 'Glob', 'Edit', 'Write'], edits: true },
  accessibility: { tools: ['Read', 'Grep', 'Glob'], edits: false },
};
const ARMS = ['none', 'persona'];

function args() {
  const a = { cmd: process.argv[2], model: 'claude-sonnet-5', personas: 'personas', reps: 2, concurrency: 4, seed: 'persona-exp-2026' };
  for (let i = 3; i < process.argv.length; i += 2) a[process.argv[i].replace(/^--/, '')] = process.argv[i + 1];
  a.reps = Number(a.reps);
  a.concurrency = Number(a.concurrency);
  return a;
}

const readJSON = (p) => JSON.parse(fs.readFileSync(p, 'utf8'));
const writeJSON = (p, v) => { fs.mkdirSync(path.dirname(p), { recursive: true }); fs.writeFileSync(p, JSON.stringify(v, null, 2) + '\n'); };

// Seeded shuffle so the run order is random but recorded and repeatable.
function shuffle(list, seed) {
  let h = crypto.createHash('sha256').update(seed).digest();
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    h = crypto.createHash('sha256').update(h).digest();
    const j = h.readUInt32BE(0) % (i + 1);
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

// Replace anything that identifies the machine or the person with a placeholder.
function scrubber(extra = []) {
  const pairs = [];
  for (const p of extra) {
    pairs.push([p, '<workdir>']);
    try { pairs.push([fs.realpathSync(p), '<workdir>']); } catch {}
  }
  pairs.push([os.homedir(), '~']);
  pairs.push([os.userInfo().username, '<user>']);
  pairs.push([os.hostname(), '<host>']);
  pairs.sort((a, b) => b[0].length - a[0].length);
  return (s) => {
    let out = s;
    for (const [from, to] of pairs) if (from) out = out.split(from).join(to);
    return out;
  };
}

function exec(cmd, argv, opts = {}) {
  return new Promise((resolve) => {
    const started = Date.now();
    const child = spawn(cmd, argv, { cwd: opts.cwd, stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '', stderr = '';
    child.stdout.on('data', (d) => (stdout += d));
    child.stderr.on('data', (d) => (stderr += d));
    const timer = setTimeout(() => child.kill('SIGTERM'), opts.timeoutMs ?? 20 * 60 * 1000);
    child.on('close', (code) => { clearTimeout(timer); resolve({ code, stdout, stderr, ms: Date.now() - started }); });
  });
}

async function pool(items, n, fn) {
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(n, items.length) }, async () => {
    while (next < items.length) { const i = next++; await fn(items[i], i); }
  }));
}

function environment(a) {
  const v = (cmd, argv) => { const r = spawnSync(cmd, argv, { encoding: 'utf8' }); return (r.stdout || r.stderr || '').trim(); };
  const personaHashes = {};
  for (const role of Object.keys(ROLES)) {
    personaHashes[role] = crypto.createHash('sha256').update(fs.readFileSync(path.join(a.personas, `${role}.md`))).digest('hex');
  }
  return {
    date: new Date().toISOString(),
    claude_cli: v('claude', ['--version']),
    model: a.model,
    go: v('go', ['version']),
    node: process.version,
    platform: `${os.platform()} ${os.release()} ${os.arch()}`,
    persona_dir: a.personas === 'personas' ? 'personas (snapshot in this directory)' : '<custom>',
    persona_sha256: personaHashes,
    persona_catalog_commit: process.env.PERSONA_CATALOG_COMMIT || 'unknown',
  };
}

// ---------- run ----------
async function runOne(a, r) {
  const cfg = ROLES[r.role];
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pexp-'));
  const work = path.join(tmp, 'work');
  fs.cpSync(path.join('fixtures', r.role), work, { recursive: true });
  const prompt = fs.readFileSync(path.join('prompts', `${r.role}.txt`), 'utf8').trim();
  const argv = ['-p', prompt, '--model', a.model, '--no-session-persistence', '--safe-mode',
    '--allowedTools', ...cfg.tools, '--output-format', 'json'];
  if (r.arm === 'persona') argv.push('--append-system-prompt-file', path.resolve(a.personas, `${r.role}.md`));
  const res = await exec('claude', argv, { cwd: work });
  const scrub = scrubber([work, tmp]);
  let j = null;
  try { j = JSON.parse(res.stdout); } catch {}
  const ok = res.code === 0 && j && !j.is_error && typeof j.result === 'string';
  const out = { ok, exit: res.code, wall_ms: res.ms, stderr: scrub(res.stderr).slice(0, 4000) };
  if (j) {
    out.result = scrub(j.result ?? '');
    out.meta = { duration_ms: j.duration_ms, num_turns: j.num_turns, total_cost_usd: j.total_cost_usd, usage: j.usage, modelUsage: j.modelUsage, subtype: j.subtype, is_error: j.is_error };
  }
  if (ok && cfg.edits) {
    const d = spawnSync('git', ['diff', '--no-index', '--no-color', path.join('fixtures', r.role), work], { encoding: 'utf8' });
    out.diff = scrub(d.stdout)
      .split(`a/fixtures/${r.role}/`).join('a/').split(`b/fixtures/${r.role}/`).join('b/')
      .replace(/^(diff --git |--- |\+\+\+ )(.*)$/gm, (_, h, rest) => h + rest.replace(/(^|\s)([ab])\/?<workdir>\//g, '$1$2/'));
  }
  if (ok && cfg.goTest) {
    const t = spawnSync('go', ['test', './...'], { cwd: work, encoding: 'utf8' });
    out.tests = { exit: t.status, output: scrub((t.stdout || '') + (t.stderr || '')) };
  }
  fs.rmSync(tmp, { recursive: true, force: true });
  return out;
}

async function cmdRun(a) {
  const planned = [];
  for (const role of Object.keys(ROLES)) for (const arm of ARMS) for (let rep = 1; rep <= a.reps; rep++) planned.push({ role, arm, rep });
  let idmap;
  if (fs.existsSync('idmap.json')) {
    idmap = readJSON('idmap.json');
  } else {
    const order = shuffle(planned, a.seed);
    idmap = { seed: a.seed, runs: order.map((r, i) => ({ id: crypto.randomBytes(4).toString('hex'), order: i + 1, ...r })) };
    writeJSON('idmap.json', idmap);
  }
  writeJSON('environment.json', environment(a));
  await pool(idmap.runs, a.concurrency, async (r) => {
    const dir = path.join('outputs', r.id);
    if (fs.existsSync(path.join(dir, 'output.md'))) return;
    const attempts = [];
    let out;
    for (let attempt = 1; attempt <= 3; attempt++) {
      out = await runOne(a, r);
      attempts.push({ attempt, ok: out.ok, exit: out.exit, subtype: out.meta?.subtype, stderr: out.ok ? '' : out.stderr.slice(0, 500) });
      if (out.ok) break;
      console.error(`run ${r.order} (${r.id}) failed attempt ${attempt}; retrying`);
    }
    fs.mkdirSync(dir, { recursive: true });
    writeJSON(path.join('raw', `${r.id}.json`), { id: r.id, attempts, wall_ms: out.wall_ms, meta: out.meta ?? null });
    if (!out.ok) { console.error(`run ${r.id} failed after retries`); return; }
    fs.writeFileSync(path.join(dir, 'output.md'), out.result + '\n');
    if (out.diff !== undefined) fs.writeFileSync(path.join(dir, 'changes.diff'), out.diff);
    if (out.tests) fs.writeFileSync(path.join(dir, 'tests.txt'), `exit ${out.tests.exit}\n${out.tests.output}`);
    console.error(`run ${r.order}/${idmap.runs.length} done: ${r.id} $${out.meta.total_cost_usd?.toFixed(3)}`);
  });
}

// ---------- grade ----------
function graderPrompt(role, id) {
  const dir = path.join('outputs', id);
  const parts = [
    fs.readFileSync('prompts/grader-common.md', 'utf8'),
    fs.readFileSync(`prompts/grader-${role}.md`, 'utf8'),
    '## Answer key\n```json\n' + fs.readFileSync(`keys/${role}.json`, 'utf8') + '```',
  ];
  if (fs.existsSync(path.join(dir, 'changes.diff'))) parts.push('## Diff of files changed (fixture -> result)\n```diff\n' + (fs.readFileSync(path.join(dir, 'changes.diff'), 'utf8') || '(no changes)') + '\n```');
  if (fs.existsSync(path.join(dir, 'tests.txt'))) parts.push('## `go test ./...` after the change\n```\n' + fs.readFileSync(path.join(dir, 'tests.txt'), 'utf8') + '\n```');
  parts.push('## The output to grade (final message)\n<output>\n' + fs.readFileSync(path.join(dir, 'output.md'), 'utf8') + '\n</output>');
  return parts.join('\n\n');
}

function extractJSON(text) {
  const fence = text.match(/```(?:json)?\s*([\s\S]*?)```/);
  const body = fence ? fence[1] : text.slice(text.indexOf('{'), text.lastIndexOf('}') + 1);
  return JSON.parse(body);
}

async function cmdGrade(a) {
  const idmap = readJSON('idmap.json');
  // The grader sees only the role's key and the blinded output directory, never idmap.json.
  const todo = idmap.runs.map((r) => ({ id: r.id, role: r.role })).filter((r) => fs.existsSync(path.join('outputs', r.id, 'output.md')));
  await pool(shuffle(todo, a.seed + '-grade'), a.concurrency, async ({ id, role }) => {
    if (fs.existsSync(path.join('grades', `${id}.json`))) return;
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pexp-grade-'));
    for (let attempt = 1; attempt <= 3; attempt++) {
      const res = await exec('claude', ['-p', graderPrompt(role, id), '--model', a.model, '--no-session-persistence', '--safe-mode',
        '--tools', '', '--output-format', 'json'], { cwd: tmp });
      try {
        const j = JSON.parse(res.stdout);
        const grade = extractJSON(j.result);
        writeJSON(path.join('grades', `${id}.json`), { id, task: role, grader_model: a.model, grader_cost_usd: j.total_cost_usd, attempt, grade });
        console.error(`graded ${id}`);
        break;
      } catch (e) {
        console.error(`grade ${id} attempt ${attempt} failed: ${e.message}`);
      }
    }
    fs.rmSync(tmp, { recursive: true, force: true });
  });
}

// ---------- analyze ----------
const mean = (xs) => xs.reduce((s, x) => s + x, 0) / xs.length;
const sd = (xs) => xs.length < 2 ? 0 : Math.sqrt(xs.reduce((s, x) => s + (x - mean(xs)) ** 2, 0) / (xs.length - 1));
const fmt = (xs, d = 1) => xs.length ? `${mean(xs).toFixed(d)}${xs.length > 1 ? ` ± ${sd(xs).toFixed(d)}` : ''}` : 'n/a';

function metrics(role, g, dir, raw) {
  const m = { cost_usd: raw.meta?.total_cost_usd ?? 0, turns: raw.meta?.num_turns ?? 0, duration_s: (raw.meta?.duration_ms ?? 0) / 1000,
    output_chars: fs.readFileSync(path.join(dir, 'output.md'), 'utf8').length };
  const count = (obj, k) => Object.values(obj).filter((v) => v[k]).length;
  if (role === 'code-reviewer' || role === 'accessibility') {
    const planted = role === 'code-reviewer' ? g.defects : g.problems;
    const high = role === 'code-reviewer' ? ['G2', 'G3', 'G4', 'G5'] : ['A2', 'A4', 'A5'];
    Object.assign(m, { found: count(planted, 'found'), high_found: high.filter((k) => planted[k]?.found).length,
      decoys_flagged: count(g.decoys, 'flagged'), wrong_claims: g.wrong_claims.length, leads_with_high: g.leads_with_high ? 1 : 0 });
  } else if (role === 'seo') {
    Object.assign(m, { fixed: count(g.problems, 'fixed'), regressions: g.regressions.length, invented_claims: g.invented_claims.length });
  }
  if (role === 'seo' || role === 'minimal-change') {
    const diff = fs.existsSync(path.join(dir, 'changes.diff')) ? fs.readFileSync(path.join(dir, 'changes.diff'), 'utf8') : '';
    m.files_touched = (diff.match(/^diff --git /gm) || []).length;
    m.lines_changed = diff.split('\n').filter((l) => /^[+-]/.test(l) && !/^(\+\+\+|---) /.test(l)).length;
  }
  if (role === 'minimal-change') {
    const t = fs.readFileSync(path.join(dir, 'tests.txt'), 'utf8');
    Object.assign(m, { tests_pass: t.startsWith('exit 0') ? 1 : 0, unrelated_edits: g.unrelated_edit_count,
      unrelated_lines: g.hunks.filter((h) => h.kind === 'unrelated').reduce((s, h) => s + h.changed_lines, 0),
      test_lines: g.hunks.filter((h) => h.kind === 'test_addition').reduce((s, h) => s + h.changed_lines, 0) });
  }
  return m;
}

function cmdAnalyze() {
  const idmap = readJSON('idmap.json');
  const rows = [];
  for (const r of idmap.runs) {
    const dir = path.join('outputs', r.id);
    const final = path.join('grades', `${r.id}.final.json`);
    const gp = fs.existsSync(final) ? final : path.join('grades', `${r.id}.json`);
    if (!fs.existsSync(gp)) continue;
    const g = readJSON(gp);
    rows.push({ ...r, hand_corrected: gp === final, graded: g, m: metrics(r.role, g.grade, dir, readJSON(path.join('raw', `${r.id}.json`))) });
  }
  const table = {};
  for (const role of Object.keys(ROLES)) {
    table[role] = {};
    for (const arm of ARMS) {
      const rs = rows.filter((x) => x.role === role && x.arm === arm);
      const keys = rs.length ? Object.keys(rs[0].m) : [];
      table[role][arm] = { n: rs.length, ids: rs.map((x) => x.id), per_run: rs.map((x) => ({ id: x.id, rep: x.rep, ...x.m })),
        mean: Object.fromEntries(keys.map((k) => [k, fmt(rs.map((x) => x.m[k]), k === 'cost_usd' ? 3 : 1)])) };
    }
  }
  const graderCost = rows.reduce((s, x) => s + (x.graded.grader_cost_usd || 0), 0);
  const runCost = rows.reduce((s, x) => s + x.m.cost_usd, 0);
  writeJSON('analysis.json', { environment: readJSON('environment.json'), total_cost_usd: { runs: runCost, grading: graderCost, total: runCost + graderCost }, table });
  console.log(JSON.stringify({ total: runCost + graderCost, table: Object.fromEntries(Object.entries(table).map(([k, v]) => [k, { none: v.none.mean, persona: v.persona.mean }])) }, null, 2));
}

const a = args();
if (a.cmd === 'run') await cmdRun(a);
else if (a.cmd === 'grade') await cmdGrade(a);
else if (a.cmd === 'analyze') cmdAnalyze();
else { console.error('usage: node run.mjs run|grade|analyze [--model M] [--personas DIR] [--reps N] [--concurrency N] [--seed S]'); process.exit(2); }
