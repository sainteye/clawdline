#!/usr/bin/env node
// Persona experiment, round 4: MAKER sessions (performance, security), 2x2 persona x brief, scored
// by held-out checks the model never sees. Adapted from ../round3/run.mjs (left untouched).
// Differences from round 3:
//   - tools: Read Grep Glob Edit Write Bash; both roles run in the Claude Code sandbox, localhost only;
//   - every run gets its own ADDR (port), so all runs can go in parallel without sharing a server;
//   - the final source tree of each run is kept (outputs/<id>/work) and scored by `score`:
//     performance = held-out speedup (median of 3 rounds) gated on golden output;
//     security = held-out exploits blocked /6 gated on held-out functional tests.
// Usage: node run.mjs pilot --role R --label L | run [--reps N] | score | analyze
import { spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const HERE = path.dirname(new URL(import.meta.url).pathname);
process.chdir(HERE);

const TOOLS = ['Read', 'Grep', 'Glob', 'Edit', 'Write', 'Bash'];
const ROLES = ['performance', 'security'];
const ARMS = ['none-vague', 'persona-vague', 'none-brief', 'persona-brief'];
const SANDBOX_TMP = `/private/tmp/claude-${process.getuid()}`;
const RUN_TIMEOUT_MS = 12 * 60 * 1000;
const MAX_BUDGET_USD = '2';
let nextPort = 18400;

function args() {
  const a = { cmd: process.argv[2], model: 'claude-sonnet-5', reps: 2, seed: 'persona-exp-2026-r4' };
  for (let i = 3; i < process.argv.length; i += 2) a[process.argv[i].replace(/^--/, '')] = process.argv[i + 1];
  a.reps = Number(a.reps);
  return a;
}
const readJSON = (p) => JSON.parse(fs.readFileSync(p, 'utf8'));
const writeJSON = (p, v) => { fs.mkdirSync(path.dirname(p), { recursive: true }); fs.writeFileSync(p, JSON.stringify(v, null, 2) + '\n'); };

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

function scrubber(extra = []) {
  const pairs = [];
  for (const p of extra) { pairs.push([p, '<workdir>']); try { pairs.push([fs.realpathSync(p), '<workdir>']); } catch {} }
  pairs.push([os.homedir(), '~'], [os.userInfo().username, '<user>'], [os.hostname(), '<host>'], [os.hostname().replace(/\.local$/, ''), '<host>']);
  pairs.sort((a, b) => b[0].length - a[0].length);
  return (s) => {
    let out = s;
    for (const [from, to] of pairs) if (from) out = out.split(from).join(to);
    let n = 0;
    const ids = new Map();
    out = out.replace(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi, (u) => {
      if (!ids.has(u)) ids.set(u, `5e550000-0000-4000-8000-${String(++n).padStart(12, '0')}`);
      return ids.get(u);
    });
    return out.replace(/\/private\/tmp\/claude-\d+\/[^/\s"\\]+/g, '<cli-tmp>')
      .replace(/-private-var-folders-[^/\s"\\]+/g, '<encoded-workdir>')
      .replace(/\/(private\/)?var\/folders\/[^\s"\\']+/g, '<tmp>');
  };
}

function cleanTranscript(stdout, scrub) {
  const out = [];
  for (const line of stdout.split('\n')) {
    if (!line.trim()) continue;
    let e;
    try { e = JSON.parse(line); } catch { continue; }
    if (e.type === 'system' && e.subtype === 'init') out.push({ type: 'system', subtype: 'init', model: e.model, tools: e.tools, permissionMode: e.permissionMode, claude_code_version: e.claude_code_version });
    else if (e.type === 'assistant' || e.type === 'user') out.push({ type: e.type, content: (e.message?.content ?? []).map((c) => (c.type === 'redacted_thinking' ? { type: c.type } : c)) });
    else if (e.type === 'result') out.push({ type: 'result', subtype: e.subtype, is_error: e.is_error, num_turns: e.num_turns, duration_ms: e.duration_ms, total_cost_usd: e.total_cost_usd, result: e.result });
  }
  return scrub(out.map((x) => JSON.stringify(x)).join('\n') + '\n');
}

function exec(cmd, argv, opts = {}) {
  return new Promise((resolve) => {
    const started = Date.now();
    const child = spawn(cmd, argv, { cwd: opts.cwd, env: opts.env ?? process.env, stdio: ['ignore', 'pipe', 'pipe'], detached: true });
    let stdout = '', stderr = '';
    child.stdout.on('data', (d) => (stdout += d));
    child.stderr.on('data', (d) => (stderr += d));
    const timer = setTimeout(() => { try { process.kill(-child.pid, 'SIGTERM'); } catch {} }, opts.timeoutMs ?? RUN_TIMEOUT_MS);
    child.on('close', (code) => { clearTimeout(timer); try { process.kill(-child.pid, 'SIGKILL'); } catch {} resolve({ code, stdout, stderr, ms: Date.now() - started }); });
  });
}

function killLeftovers(dir) {
  const r = spawnSync('lsof', ['-t', '+D', dir], { encoding: 'utf8' });
  for (const p of new Set((r.stdout || '').split('\n').filter(Boolean).map(Number))) if (p !== process.pid) { try { process.kill(p, 'SIGKILL'); } catch {} }
}

function copySource(src, dest) {
  // Keep only text sources: no binaries the session built.
  fs.cpSync(src, dest, { recursive: true, filter: (p) => { const st = fs.statSync(p); return st.isDirectory() || (st.size < 200000 && !(st.mode & 0o111)); } });
}

async function runOne(a, r) {
  const tmp = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'pexp4-')));
  const work = path.join(tmp, 'work');
  fs.cpSync(path.join('fixtures', r.role), work, { recursive: true });
  const prompt = fs.readFileSync(path.join('prompts', `${r.role}-${r.arm.endsWith('brief') ? 'brief' : 'vague'}.txt`), 'utf8').trim();
  const argv = ['-p', prompt, '--model', a.model, '--no-session-persistence', '--safe-mode', '--tools', TOOLS.join(','), '--allowedTools', ...TOOLS,
    '--output-format', 'stream-json', '--verbose', '--max-budget-usd', MAX_BUDGET_USD, '--settings', path.resolve('sandbox.json')];
  if (r.arm.startsWith('persona')) argv.push('--append-system-prompt-file', path.resolve('personas', `${r.role}.md`));
  const gocache = path.join(SANDBOX_TMP, `pexp4-gocache-${r.id}`);
  const env = { ...process.env, ADDR: `127.0.0.1:${nextPort++}`, GOCACHE: gocache, GOTOOLCHAIN: 'local', GOPROXY: 'off', GOFLAGS: '-mod=mod' };
  const res = await exec('claude', argv, { cwd: work, env });
  killLeftovers(tmp);
  const scrub = scrubber([work, tmp]);
  const lines = res.stdout.trim().split('\n');
  let j = null;
  for (let i = lines.length - 1; i >= 0 && !j; i--) { try { const e = JSON.parse(lines[i]); if (e.type === 'result') j = e; } catch {} }
  const ok = res.code === 0 && j && !j.is_error && typeof j.result === 'string';
  const dir = path.join(r.pilot ? 'pilot' : 'outputs', r.id);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'transcript.jsonl'), cleanTranscript(res.stdout, scrub));
  if (j) fs.writeFileSync(path.join(dir, 'output.md'), scrub(j.result ?? '') + '\n');
  writeJSON(path.join(dir, 'raw.json'), { id: r.id, ok, exit: res.code, wall_ms: res.ms, stderr: scrub(res.stderr).slice(0, 2000),
    meta: j ? { duration_ms: j.duration_ms, num_turns: j.num_turns, total_cost_usd: j.total_cost_usd, subtype: j.subtype, is_error: j.is_error } : null });
  fs.rmSync(path.join(dir, 'work'), { recursive: true, force: true });
  copySource(work, path.join(dir, 'work'));
  fs.rmSync(tmp, { recursive: true, force: true });
  fs.rmSync(gocache, { recursive: true, force: true });
  console.error(`${r.id} ok=${ok} $${j?.total_cost_usd?.toFixed(2)} ${Math.round(res.ms / 1000)}s`);
}

// ---------- held-out scoring ----------
function goTest(dir, run) {
  const r = spawnSync('go', ['test', '-count=1', '-run', run, '-v', '.'], { cwd: dir, encoding: 'utf8', env: { ...process.env, GOTOOLCHAIN: 'local', GOPROXY: 'off', GOFLAGS: '-mod=mod' }, timeout: 300000 });
  return (r.stdout || '') + (r.stderr || '');
}
function scoreDir(role, workDir) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pexp4-score-'));
  const d = path.join(tmp, 'w');
  fs.cpSync(workDir, d, { recursive: true });
  fs.copyFileSync(path.join('keys', role, 'heldout_test.go'), path.join(d, 'heldout_test.go'));
  const key = readJSON(path.join('keys', role, 'key.json'));
  const vet = spawnSync('go', ['vet', './...'], { cwd: d, encoding: 'utf8' });
  const own = goTest(d, '^Test[^H]');
  const ownPass = !/FAIL|build failed|cannot|undefined/.test(own);
  let s;
  if (role === 'performance') {
    process.env.HELDOUT_GOLDEN_60 = key.golden['60'];
    process.env.HELDOUT_GOLDEN_1000 = key.golden['1000'];
    const g = goTest(d, 'TestHeldoutGolden');
    const golden = /--- PASS: TestHeldoutGolden/.test(g);
    let ms = null;
    if (golden) { const t = goTest(d, 'TestHeldoutSpeed'); const m = t.match(/HELDOUT_MS=([\d.]+)/); ms = m ? +m[1] : null; }
    const gate = golden && ownPass && vet.status === 0 && ms != null;
    s = { vet: vet.status === 0, own_tests: ownPass, golden, heldout_ms: ms, speedup: gate ? +(key.baseline_heldout_ms / ms).toFixed(2) : 0, gate };
  } else {
    const out = goTest(d, 'TestHeldout');
    const pass = (n) => new RegExp(`--- PASS: ${n} `).test(out);
    const funcOk = key.functional_tests.every(pass);
    const src = fs.readdirSync(d).filter((f) => f.endsWith('.go') && f !== 'heldout_test.go' && !f.endsWith('_test.go')).map((f) => fs.readFileSync(path.join(d, f), 'utf8')).join('\n');
    const blocked = {};
    for (const e of key.exploits) blocked[e.id] = e.test.startsWith('static') ? /subtle\.ConstantTimeCompare|hmac\.Equal/.test(src) : pass(e.test);
    const gate = funcOk && ownPass && vet.status === 0;
    const n = Object.values(blocked).filter(Boolean).length;
    s = { vet: vet.status === 0, own_tests: ownPass, functional: Object.fromEntries(key.functional_tests.map((t) => [t, pass(t)])), blocked, blocked_n: n, score: gate ? n : 0, gate };
  }
  fs.rmSync(tmp, { recursive: true, force: true });
  return s;
}

async function cmdPilot(a) {
  const r = { id: `${a.role}-${a.label}`, role: a.role, arm: 'none-vague', pilot: true };
  await runOne(a, r);
  const s = scoreDir(r.role, path.join('pilot', r.id, 'work'));
  writeJSON(path.join('pilot', r.id, 'score.json'), s);
  console.log(JSON.stringify(s));
}

async function cmdRun(a) {
  let idmap;
  if (fs.existsSync('idmap.json')) idmap = readJSON('idmap.json');
  else {
    const planned = [];
    for (const role of ROLES) for (const arm of ARMS) for (let rep = 1; rep <= a.reps; rep++) planned.push({ role, arm, rep });
    idmap = { seed: a.seed, runs: shuffle(planned, a.seed).map((r, i) => ({ id: crypto.randomBytes(4).toString('hex'), order: i + 1, ...r })) };
    writeJSON('idmap.json', idmap);
  }
  const sha = (p) => crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
  const v = (c, x) => { const r = spawnSync(c, x, { encoding: 'utf8' }); return (r.stdout || r.stderr || '').trim(); };
  writeJSON('environment.json', { date: new Date().toISOString(), claude_cli: v('claude', ['--version']), model: a.model, go: v('go', ['version']), node: process.version,
    platform: `${os.platform()} ${os.release()} ${os.arch()}`, tools: TOOLS, sandbox: readJSON('sandbox.json'),
    persona_sha256: Object.fromEntries(ROLES.map((r) => [r, sha(path.join('personas', `${r}.md`))])), persona_catalog_commit: process.env.PERSONA_CATALOG_COMMIT || 'unknown' });
  // All runs in parallel: each has its own copy and port, and speed is measured afterwards, not during.
  await Promise.all(idmap.runs.filter((r) => !fs.existsSync(path.join('outputs', r.id, 'raw.json'))).map((r) => runOne(a, r)));
}

function cmdScore() {
  // Sequential, after every session has ended, so timings do not compete.
  for (const r of readJSON('idmap.json').runs) {
    const w = path.join('outputs', r.id, 'work');
    if (!fs.existsSync(w)) continue;
    const s = scoreDir(r.role, w);
    writeJSON(path.join('scores', `${r.id}.json`), s);
    console.error(`scored ${r.id}`);
  }
}

// ---------- analyze ----------
const mean = (xs) => (xs.length ? xs.reduce((s, x) => s + x, 0) / xs.length : null);
function method(role, transcriptPath) {
  const tl = fs.readFileSync(transcriptPath, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l));
  const cmds = tl.filter((e) => e.type === 'assistant').flatMap((e) => e.content.filter((c) => c.type === 'tool_use' && c.name === 'Bash').map((c) => String(c.input.command)));
  const edits = tl.filter((e) => e.type === 'assistant').flatMap((e) => e.content.filter((c) => c.type === 'tool_use' && (c.name === 'Edit' || c.name === 'Write')));
  const measure = cmds.filter((c) => /loadtest|-bench|Benchmark|hey |ab -|wrk|time curl|curl .*time_total/.test(c));
  const firstEdit = tl.findIndex((e) => e.type === 'assistant' && e.content.some((c) => c.type === 'tool_use' && (c.name === 'Edit' || c.name === 'Write')));
  const firstMeasure = tl.findIndex((e) => e.type === 'assistant' && e.content.some((c) => c.type === 'tool_use' && c.name === 'Bash' && /loadtest|-bench|Benchmark|time curl|time_total/.test(c.input.command)));
  return {
    bash_calls: cmds.length, edits: edits.length,
    curl_calls: cmds.filter((c) => /curl /.test(c)).length,
    started_server: cmds.some((c) => /go run \.( |$|&)|go run main.go|\.\/(ordersvc|notesapi|server)\b/.test(c)) ? 1 : 0,
    measured: measure.length ? 1 : 0,
    measured_before_first_edit: firstMeasure >= 0 && (firstEdit < 0 || firstMeasure < firstEdit) ? 1 : 0,
  };
}
function cmdAnalyze() {
  const idmap = readJSON('idmap.json');
  const rows = idmap.runs.filter((r) => fs.existsSync(path.join('scores', `${r.id}.json`))).map((r) => {
    const s = readJSON(path.join('scores', `${r.id}.json`));
    const raw = readJSON(path.join('outputs', r.id, 'raw.json'));
    return { ...r, primary: r.role === 'performance' ? s.speedup : s.score, s, cost: raw.meta?.total_cost_usd ?? 0, ...method(r.role, path.join('outputs', r.id, 'transcript.jsonl')) };
  });
  const table = {};
  for (const role of ROLES) {
    table[role] = {};
    for (const arm of ARMS) {
      const rs = rows.filter((x) => x.role === role && x.arm === arm).sort((p, q) => p.rep - q.rep);
      const keys = ['primary', 'cost', 'bash_calls', 'edits', 'curl_calls', 'started_server', 'measured', 'measured_before_first_edit'];
      table[role][arm] = { n: rs.length, per_run: rs.map((x) => ({ id: x.id, rep: x.rep, primary: x.primary, cost: x.cost, score: x.s, method: method(role, path.join('outputs', x.id, 'transcript.jsonl')) })),
        mean: Object.fromEntries(keys.map((k) => [k, rs.length ? +mean(rs.map((x) => x[k])).toFixed(3) : null])) };
    }
    const at = (arm) => table[role][arm].mean.primary;
    const d = (x, y) => (role === 'performance' ? +(x / y).toFixed(2) : +(x - y).toFixed(2));
    table[role].effects = { unit: role === 'performance' ? 'ratio of mean speedups' : 'difference in mean exploits blocked',
      persona_given_vague: d(at('persona-vague'), at('none-vague')), persona_given_brief: d(at('persona-brief'), at('none-brief')),
      brief_given_none: d(at('none-brief'), at('none-vague')), brief_given_persona: d(at('persona-brief'), at('persona-vague')),
      both_vs_neither: d(at('persona-brief'), at('none-vague')) };
  }
  let pilot = 0;
  if (fs.existsSync('pilot')) for (const d of fs.readdirSync('pilot')) { const p = path.join('pilot', d, 'raw.json'); if (fs.existsSync(p)) pilot += readJSON(p).meta?.total_cost_usd ?? 0; }
  const runs = rows.reduce((s, x) => s + x.cost, 0);
  const cost = { pilot_runs: +pilot.toFixed(3), runs: +runs.toFixed(3), grading: 0, total: +(pilot + runs).toFixed(3) };
  writeJSON('analysis.json', { environment: readJSON('environment.json'), total_cost_usd: cost, table });
  console.log(JSON.stringify({ cost, means: Object.fromEntries(ROLES.map((r) => [r, Object.fromEntries(ARMS.map((a) => [a, table[r][a].mean]))])), effects: Object.fromEntries(ROLES.map((r) => [r, table[r].effects])) }, null, 2));
}

const a = args();
if (a.cmd === 'pilot') await cmdPilot(a);
else if (a.cmd === 'run') await cmdRun(a);
else if (a.cmd === 'score') cmdScore();
else if (a.cmd === 'analyze') cmdAnalyze();
else { console.error('usage: node run.mjs pilot --role R --label L | run [--reps N] | score | analyze'); process.exit(2); }
