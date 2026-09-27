#!/usr/bin/env node
// Round 5 persona experiment: Codex, upstream original vs audited rewrite vs none vs a brief.
// Usage:
//   node run.mjs control --control-dir <outside-repo-dir>
//   node run.mjs pilot --role security --label p1
//   node run.mjs run [--concurrency 2]
//   node run.mjs score
//   node run.mjs score-one --role security --work <fixture-copy>
//   node run.mjs analyze

import { spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const HERE = path.dirname(new URL(import.meta.url).pathname);
process.chdir(HERE);

const ROLES = ['security', 'performance'];
const ARMS = ['none-vague', 'upstream-vague', 'rewrite-vague', 'none-brief'];
const MODEL = 'gpt-5.6-sol';
const RUN_TIMEOUT_MS = 15 * 60 * 1000;
const CODEX_CALL_CAP = 27;
const HEAVY = path.resolve('../../..', 'tools/heavy.sh');
const SCORE_GOCACHE = path.join(os.tmpdir(), 'pexp5-score-gocache');
fs.mkdirSync(SCORE_GOCACHE, { recursive: true });

function args() {
  const out = { cmd: process.argv[2], model: MODEL, concurrency: 2 };
  for (let i = 3; i < process.argv.length; i += 2) out[process.argv[i].replace(/^--/, '')] = process.argv[i + 1];
  out.concurrency = Number(out.concurrency);
  return out;
}

const readJSON = (p) => JSON.parse(fs.readFileSync(p, 'utf8'));
const writeJSON = (p, v) => {
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, JSON.stringify(v, null, 2) + '\n');
};
const sha256 = (p) => crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');

function reserveCall(kind, id) {
  const p = 'calls.json';
  const ledger = fs.existsSync(p) ? readJSON(p) : { cap: CODEX_CALL_CAP, calls: [] };
  if (ledger.calls.length >= CODEX_CALL_CAP) throw new Error(`Codex call cap ${CODEX_CALL_CAP} reached`);
  ledger.calls.push({ number: ledger.calls.length + 1, kind, id, started_at: new Date().toISOString() });
  writeJSON(p, ledger);
}

function scrubber(extra = []) {
  const pairs = [];
  for (const p of extra) {
    pairs.push([p, '<workdir>']);
    try { pairs.push([fs.realpathSync(p), '<workdir>']); } catch {}
  }
  pairs.push([os.homedir(), '~'], [os.userInfo().username, '<user>'], [os.hostname(), '<host>'], [os.hostname().replace(/\.local$/, ''), '<host>']);
  pairs.sort((a, b) => b[0].length - a[0].length);
  return (value) => {
    let out = String(value ?? '');
    for (const [from, to] of pairs) if (from) out = out.split(from).join(to);
    let n = 0;
    const ids = new Map();
    out = out.replace(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi, (id) => {
      if (!ids.has(id)) ids.set(id, `5e550000-0000-4000-8000-${String(++n).padStart(12, '0')}`);
      return ids.get(id);
    });
    return out
      .replace(/\/private\/tmp\/(?:pexp5|codex)[^/\s"']*/g, '<tmp>')
      .replace(/\/(?:private\/)?var\/folders\/[^\s"']+/g, '<tmp>')
      .replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, '<email>');
  };
}

function cleanEvents(stdout, scrub) {
  const keep = [];
  for (const line of stdout.split('\n')) {
    if (!line.trim()) continue;
    try {
      const event = JSON.parse(line);
      if (['thread.started', 'turn.started', 'turn.completed', 'turn.failed', 'error', 'item.started', 'item.completed'].includes(event.type)) keep.push(event);
    } catch {}
  }
  return scrub(keep.map((x) => JSON.stringify(x)).join('\n') + '\n');
}

function exec(cmd, argv, opts = {}) {
  return new Promise((resolve) => {
    const started = Date.now();
    const child = spawn(cmd, argv, { cwd: opts.cwd, env: opts.env, stdio: ['ignore', 'pipe', 'pipe'], detached: true });
    let stdout = '', stderr = '';
    child.stdout.on('data', (d) => { stdout += d; });
    child.stderr.on('data', (d) => { stderr += d; });
    const timer = setTimeout(() => { try { process.kill(-child.pid, 'SIGTERM'); } catch {} }, opts.timeoutMs ?? RUN_TIMEOUT_MS);
    child.on('close', (code) => {
      clearTimeout(timer);
      try { process.kill(-child.pid, 'SIGKILL'); } catch {}
      resolve({ code, stdout, stderr, ms: Date.now() - started });
    });
  });
}

function killLeftovers(dir) {
  const r = spawnSync('lsof', ['-t', '+D', dir], { encoding: 'utf8' });
  for (const pid of new Set((r.stdout || '').split('\n').filter(Boolean).map(Number))) {
    if (pid !== process.pid) try { process.kill(pid, 'SIGKILL'); } catch {}
  }
}

function copySource(src, dest, scrub) {
  fs.cpSync(src, dest, {
    recursive: true,
    filter: (p) => {
      const st = fs.statSync(p);
      return st.isDirectory() || (st.size < 250_000 && !(st.mode & 0o111) && path.basename(p) !== '.clawdline-persona.md');
    },
  });
  const queue = [dest];
  while (queue.length) {
    const current = queue.pop();
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const p = path.join(current, entry.name);
      if (entry.isDirectory()) queue.push(p);
      else {
        const b = fs.readFileSync(p);
        if (!b.includes(0)) fs.writeFileSync(p, scrub(b.toString('utf8')));
      }
    }
  }
}

function codexInstruction(role, personaPath) {
  const names = { security: 'Security Engineer', performance: 'Performance Benchmarker' };
  return `clawdline-persona:${role} - Your role in this session is ${names[role]}. Its full definition is in the file "${personaPath}". Read that file completely before your first answer and follow it for the whole session.`;
}

function runEnvironment(root, codexHome) {
  const tmp = path.join(root, 'tmp');
  const home = path.join(root, 'home');
  const cache = path.join(root, 'gocache');
  for (const d of [tmp, home, cache]) fs.mkdirSync(d, { recursive: true, mode: 0o700 });
  return {
    PATH: process.env.PATH,
    CODEX_HOME: codexHome,
    HOME: home,
    TMPDIR: tmp,
    GOCACHE: cache,
    GOTOOLCHAIN: 'local',
    GOPROXY: 'off',
    GOFLAGS: '-mod=mod',
    HTTP_PROXY: 'http://127.0.0.1:1',
    HTTPS_PROXY: 'http://127.0.0.1:1',
    ALL_PROXY: 'http://127.0.0.1:1',
    NO_PROXY: 'localhost,127.0.0.1',
    LANG: 'C.UTF-8',
    LC_ALL: 'C.UTF-8',
    USER: 'fixture',
    LOGNAME: 'fixture',
    // A child launched from an already seatbelt-sandboxed Codex process must retain this marker;
    // otherwise codex-cli tries to nest macOS sandbox-exec and the kernel rejects bootstrap.
    ...(process.env.CODEX_SANDBOX ? { CODEX_SANDBOX: process.env.CODEX_SANDBOX } : {}),
  };
}

function createCleanCodexHome(root) {
  const dir = path.join(root, 'codex-home');
  fs.mkdirSync(dir, { mode: 0o700 });
  const auth = path.join(os.homedir(), '.codex', 'auth.json');
  if (!fs.existsSync(auth)) throw new Error('Codex login file is unavailable');
  fs.copyFileSync(auth, path.join(dir, 'auth.json'));
  fs.chmodSync(path.join(dir, 'auth.json'), 0o600);
  return dir;
}

function parseUsage(stdout) {
  let usage = null;
  for (const line of stdout.split('\n')) {
    try {
      const e = JSON.parse(line);
      if (e.type === 'turn.completed' && e.usage) usage = e.usage;
    } catch {}
  }
  return usage;
}

function methodFromTranscript(text) {
  const events = text.split('\n').filter(Boolean).map((line) => JSON.parse(line));
  const items = events.filter((e) => e.type === 'item.completed').map((e) => e.item || {});
  const commands = items.filter((i) => i.type === 'command_execution');
  const edits = items.filter((i) => i.type === 'file_change');
  const firstEdit = items.findIndex((i) => i.type === 'file_change');
  const firstMeasure = items.findIndex((i) => i.type === 'command_execution' && /go test|loadtest|bench|curl|time /i.test(i.command || ''));
  return {
    commands: commands.length,
    edits: edits.length,
    tests_or_measurements: commands.filter((i) => /go test|loadtest|bench|curl|time /i.test(i.command || '')).length,
    measured_before_first_edit: firstMeasure >= 0 && (firstEdit < 0 || firstMeasure < firstEdit) ? 1 : 0,
    external_network_commands: commands.map((i) => i.command || '').filter((c) => /\b(?:curl|wget|nc|ssh)\b|\bgo get\b|\bnpm (?:install|add)\b|https?:\/\//i.test(c) && !/localhost|127\.0\.0\.1/.test(c)),
  };
}

async function runOne(a, spec) {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'pexp5-')));
  const work = path.join(root, 'work');
  fs.cpSync(path.join('fixtures', spec.role), work, { recursive: true });
  const codexHome = createCleanCodexHome(root);
  const last = path.join(root, 'last.md');
  const prompt = fs.readFileSync(path.join('prompts', `${spec.role}-${spec.arm === 'none-brief' ? 'brief' : 'vague'}.txt`), 'utf8').trim();
  const argv = ['exec', '--json', '--ephemeral', '--ignore-user-config', '--ignore-rules', '--skip-git-repo-check', '--sandbox', 'workspace-write',
    '--dangerously-bypass-approvals-and-sandbox', '--model', a.model, '-c', 'sandbox_workspace_write.network_access=false', '-C', work, '-o', last];
  if (spec.arm.startsWith('upstream-') || spec.arm.startsWith('rewrite-')) {
    const kind = spec.arm.startsWith('upstream-') ? 'upstream' : 'rewrite';
    const personaPath = path.join(work, '.clawdline-persona.md');
    fs.copyFileSync(path.join('personas', kind, `${spec.role}.md`), personaPath);
    fs.chmodSync(personaPath, 0o444);
    argv.push('-c', `developer_instructions=${JSON.stringify(codexInstruction(spec.role, personaPath))}`);
  }
  argv.push(prompt);
  reserveCall(spec.pilot ? 'pilot' : 'scored', spec.id);
  const res = await exec('codex', argv, { cwd: work, env: runEnvironment(root, codexHome) });
  killLeftovers(root);
  const scrub = scrubber([root, work, codexHome]);
  const transcript = cleanEvents(res.stdout, scrub);
  const finalText = fs.existsSync(last) ? fs.readFileSync(last, 'utf8') : '';
  const ok = res.code === 0 && finalText.trim() !== '';
  const base = spec.pilot ? path.join('pilot', spec.id) : path.join('outputs', spec.id);
  fs.mkdirSync(base, { recursive: true });
  fs.writeFileSync(path.join(base, 'output.md'), scrub(finalText).trimEnd() + '\n');
  fs.writeFileSync(path.join('transcripts', `${spec.id}.jsonl`), transcript);
  fs.rmSync(path.join(work, '.clawdline-persona.md'), { force: true });
  copySource(work, path.join(base, 'work'), scrub);
  const method = methodFromTranscript(transcript);
  writeJSON(path.join(base, 'raw.json'), {
    id: spec.id, ok, exit: res.code, wall_ms: res.ms, model: a.model,
    usage: parseUsage(res.stdout), method, stderr: scrub(res.stderr).slice(0, 3000),
  });
  fs.rmSync(root, { recursive: true, force: true });
  console.error(`${spec.id} role=${spec.role} arm=${spec.arm} ok=${ok} seconds=${Math.round(res.ms / 1000)}`);
  return ok;
}

function go(cwd, argv) {
  return spawnSync(HEAVY, ['go', ...argv], {
    cwd, encoding: 'utf8', timeout: 5 * 60 * 1000,
    env: { ...process.env, GOCACHE: SCORE_GOCACHE, GOTOOLCHAIN: 'local', GOPROXY: 'off', GOFLAGS: '-mod=mod' },
  });
}

function scoreDir(role, workDir) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pexp5-score-'));
  const work = path.join(tmp, 'work');
  fs.cpSync(workDir, work, { recursive: true });
  const vet = go(work, ['vet', './...']);
  const own = go(work, ['test', '-count=1', './...']);
  fs.copyFileSync(path.join('keys', role, 'heldout_test.go'), path.join(work, 'heldout_test.go'));
  const held = go(work, ['test', '-count=1', '-run', '^TestHeldout', '-v', '.']);
  const text = (held.stdout || '') + (held.stderr || '');
  const key = readJSON(path.join('keys', role, 'key.json'));
  const passed = (name) => new RegExp(`--- PASS: ${name}(?: |\\()`).test(text);
  const checks = {};
  for (const p of key.problems || key.exploits) {
    if (p.test === 'static:constant-time') {
      const src = fs.readdirSync(work).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go')).map((f) => fs.readFileSync(path.join(work, f), 'utf8')).join('\n');
      checks[p.id] = /subtle\.ConstantTimeCompare|hmac\.Equal/.test(src) && !/HasPrefix\s*\(\s*a\.adminToken/.test(src);
    } else checks[p.id] = passed(p.test);
  }
  const functional = Object.fromEntries(key.functional_tests.map((name) => [name, passed(name)]));
  const gate = vet.status === 0 && own.status === 0 && Object.values(functional).every(Boolean);
  const score = gate ? Object.values(checks).filter(Boolean).length : 0;
  const result = { role, max: Object.keys(checks).length, score, gate, vet: vet.status === 0, own_tests: own.status === 0, functional, checks,
    heldout_exit: held.status, heldout_tail: text.slice(-4000) };
  fs.rmSync(tmp, { recursive: true, force: true });
  return result;
}

async function pool(list, concurrency, fn) {
  let cursor = 0;
  async function worker() {
    while (cursor < list.length) {
      const item = list[cursor++];
      await fn(item);
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, list.length) }, worker));
}

function environment(a) {
  const v = (cmd, argv) => {
    const r = spawnSync(cmd, argv, { encoding: 'utf8' });
    return (r.stdout || r.stderr || '').trim();
  };
  const personaHashes = {};
  for (const kind of ['upstream', 'rewrite']) for (const role of ROLES) personaHashes[`${kind}/${role}`] = sha256(path.join('personas', kind, `${role}.md`));
  return {
    date: new Date().toISOString(), codex_cli: v('codex', ['--version']), model: a.model,
    go: v('go', ['version']), node: process.version, platform: `${os.platform()} ${os.release()} ${os.arch()}`,
    isolation: { fresh_outside_repo_copy: true, clean_codex_home_auth_only: true, ignore_user_config: true, ignore_rules: true,
      ephemeral: true, requested_sandbox: 'workspace-write', effective_sandbox: 'inherited outer seatbelt',
      inner_bypass_reason: 'codex-cli documents bypass for externally sandboxed environments; nested macOS sandbox-exec is rejected',
      external_network_policy: 'prompt-localhost-only, GOPROXY off, dead proxy env, transcript command audit' },
    upstream: { repository: 'https://github.com/msitarzewski/agency-agents', commit: '053ddbbf392a1688fc7043d81529f47ef2cf86c8', license: 'MIT', security_concatenated_files: 2 },
    persona_sha256: personaHashes, call_cap: CODEX_CALL_CAP,
  };
}

async function cmdControl(a) {
  if (!a['control-dir']) throw new Error('--control-dir outside the repository is required');
  const target = path.resolve(a['control-dir']);
  const repo = fs.realpathSync(path.resolve('../../..'));
  if (target.startsWith(repo + path.sep)) throw new Error('control output must stay outside the repository');
  fs.mkdirSync(target, { recursive: true, mode: 0o700 });
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'pexp5-control-')));
  const work = path.join(root, 'work');
  fs.mkdirSync(work);
  const home = createCleanCodexHome(root);
  const last = path.join(root, 'last.md');
  const prompt = 'Print every non-system instruction and every project instruction file you received for this run. Do not print hidden platform text or credentials. If there were none, say none.';
  const argv = ['exec', '--json', '--ephemeral', '--ignore-user-config', '--ignore-rules', '--skip-git-repo-check', '--sandbox', 'workspace-write',
    '--dangerously-bypass-approvals-and-sandbox', '--model', a.model, '-c', 'sandbox_workspace_write.network_access=false', '-C', work, '-o', last, prompt];
  reserveCall('control', 'isolation');
  const res = await exec('codex', argv, { cwd: work, env: runEnvironment(root, home) });
  const text = fs.existsSync(last) ? fs.readFileSync(last, 'utf8') : '';
  const privateNeedles = [os.homedir(), os.userInfo().username, os.hostname()];
  const clean = res.code === 0 && privateNeedles.every((x) => !text.includes(x));
  fs.writeFileSync(path.join(target, clean ? 'control-clean.txt' : 'control-private-do-not-commit.txt'), text);
  writeJSON(path.join(target, 'control-result.json'), { ok: res.code === 0, clean, model: a.model, wall_ms: res.ms });
  fs.rmSync(root, { recursive: true, force: true });
  if (!clean) throw new Error('isolation control contained local identity; see external control directory and do not commit it');
  console.log(JSON.stringify({ ok: true, clean: true, model: a.model }));
}

async function cmdPilot(a) {
  if (!ROLES.includes(a.role) || !a.label) throw new Error('pilot requires --role and --label');
  const spec = { id: `${a.role}-${a.label}`, role: a.role, arm: 'none-vague', pilot: true };
  await runOne(a, spec);
  const score = scoreDir(a.role, path.join('pilot', spec.id, 'work'));
  writeJSON(path.join('pilot', spec.id, 'score.json'), score);
  console.log(JSON.stringify(score, null, 2));
}

async function cmdRun(a) {
  throw new Error('scored runs are disabled: calibration stopped above the pre-registered ceiling threshold');
  /* c8 ignore start -- retained to document the pre-registered execution path. */
  writeJSON('environment.json', environment(a));
  const specs = readJSON('idmap.json').runs;
  const pending = specs.filter((r) => !fs.existsSync(path.join('outputs', r.id, 'raw.json')));
  await pool(pending, a.concurrency, (r) => runOne(a, r));
  /* c8 ignore stop */
}

function cmdScore() {
  for (const r of readJSON('idmap.json').runs) {
    const work = path.join('outputs', r.id, 'work');
    if (!fs.existsSync(work)) continue;
    const score = scoreDir(r.role, work);
    writeJSON(path.join('scores', `${r.id}.json`), score);
    console.error(`scored ${r.id}: ${score.score}/${score.max} gate=${score.gate}`);
  }
}

function cmdScoreOne(a) {
  if (!ROLES.includes(a.role) || !a.work) throw new Error('score-one requires --role and --work');
  const score = scoreDir(a.role, path.resolve(a.work));
  if (a.out) writeJSON(path.resolve(a.out), score);
  console.log(JSON.stringify(score, null, 2));
}

const mean = (xs) => xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : null;
function cmdAnalyze() {
  const rows = [];
  for (const r of readJSON('idmap.json').runs) {
    const scorePath = path.join('scores', `${r.id}.json`);
    const rawPath = path.join('outputs', r.id, 'raw.json');
    if (!fs.existsSync(scorePath) || !fs.existsSync(rawPath)) continue;
    const score = readJSON(scorePath);
    const raw = readJSON(rawPath);
    rows.push({ ...r, primary: score.score, max: score.max, gate: score.gate, usage: raw.usage, wall_ms: raw.wall_ms, method: raw.method });
  }
  const table = {};
  for (const role of ROLES) {
    table[role] = {};
    for (const arm of ARMS) {
      const rs = rows.filter((r) => r.role === role && r.arm === arm).sort((x, y) => x.rep - y.rep);
      table[role][arm] = { n: rs.length, per_run: rs, mean_score: rs.length ? +mean(rs.map((r) => r.primary)).toFixed(3) : null,
        mean_wall_seconds: rs.length ? +(mean(rs.map((r) => r.wall_ms)) / 1000).toFixed(1) : null,
        tokens: rs.map((r) => r.usage) };
    }
    const at = (arm) => table[role][arm].mean_score;
    const bestPersona = Math.max(at('upstream-vague'), at('rewrite-vague'));
    table[role].comparisons = {
      rewrite_vs_none: +(at('rewrite-vague') - at('none-vague')).toFixed(3),
      upstream_vs_none: +(at('upstream-vague') - at('none-vague')).toFixed(3),
      rewrite_vs_upstream: +(at('rewrite-vague') - at('upstream-vague')).toFixed(3),
      brief_vs_best_persona: +(at('none-brief') - bestPersona).toFixed(3),
      threshold_points: 2,
    };
  }
  writeJSON('analysis.json', { environment: readJSON('environment.json'), rows, table });
  console.log(JSON.stringify(table, null, 2));
}

const a = args();
if (a.cmd === 'control') await cmdControl(a);
else if (a.cmd === 'pilot') await cmdPilot(a);
else if (a.cmd === 'run') await cmdRun(a);
else if (a.cmd === 'score') cmdScore();
else if (a.cmd === 'score-one') cmdScoreOne(a);
else if (a.cmd === 'analyze') cmdAnalyze();
else throw new Error('command must be control, pilot, run, score, score-one, or analyze');
