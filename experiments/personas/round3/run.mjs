#!/usr/bin/env node
// Persona experiment, round 3: a separate VERIFIER session with tools, 2x2 (persona x brief).
// Adapted from ../round2/run.mjs (rounds 1 and 2 are left untouched). Differences:
//   - two verifier roles, each with its own fixture: reality-checker (Go API), evidence-collector (web UI);
//   - four arms per role: none-vague, persona-vague, none-brief, persona-brief;
//   - tools: Read Grep Glob Bash. The reality-checker runs inside the Claude Code sandbox with
//     network limited to localhost (sandbox.json); the evidence-collector cannot (the sandbox
//     blocks Chromium's Mach-port check-in on macOS), so its network use is audited afterwards;
//   - each run gets its own process group, and anything it leaves running is killed afterwards;
//   - one lane per role: runs of the same fixture never overlap, so they cannot share a port;
//   - the grader also gets a condensed tool log, to judge "executed" evidence and unsupported claims;
//   - the console never prints a run's arm, so the hand-check can stay blind.
// Usage (from anywhere; paths are relative to this directory):
//   node run.mjs pilot   --role R --label L         (none-vague, excluded from results)
//   node run.mjs run     [--reps N] [--seed S]
//   node run.mjs grade   [--pilot <role>-<label>]
//   node run.mjs packet  --id ID                    (blind hand-check packet: key ids, grade, report, tool log)
//   node run.mjs analyze
// Needs: the `claude` CLI (logged in), go, node, and PW_NODE_MODULES pointing at a node_modules
// directory that contains playwright-core 1.52.0 (its Chromium headless shell must be installed).
import { spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const HERE = path.dirname(new URL(import.meta.url).pathname);
process.chdir(HERE);

const TOOLS = ['Read', 'Grep', 'Glob', 'Bash'];
const ROLES = {
  'reality-checker': { sandbox: true, nodeModules: false },
  'evidence-collector': { sandbox: false, nodeModules: true },
};
const ARMS = ['none-vague', 'persona-vague', 'none-brief', 'persona-brief'];
const SANDBOX_TMP = `/private/tmp/claude-${process.getuid()}`;
const RUN_TIMEOUT_MS = 25 * 60 * 1000;
const MAX_BUDGET_USD = '5';

function args() {
  const a = { cmd: process.argv[2], model: 'claude-sonnet-5', reps: 2, seed: 'persona-exp-2026-r3' };
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
  pairs.push([os.hostname().replace(/\.local$/, ''), '<host>']);
  pairs.sort((a, b) => b[0].length - a[0].length);
  return (s) => {
    let out = s;
    for (const [from, to] of pairs) if (from) out = out.split(from).join(to);
    out = out.replace(/\/private\/tmp\/claude-\d+\/[^/\s"\\]+/g, '<cli-tmp>')
      .replace(/-private-var-folders-[^/\s"\\]+/g, '<encoded-workdir>')
      .replace(/\/(private\/)?var\/folders\/[^\s"\\']+/g, '<tmp>');
    return out;
  };
}

// Keep only the fields needed to check process claims. Image data (screenshots the verifier
// viewed) is replaced by a marker, so the transcript still shows THAT an image was viewed.
function stripImages(c) {
  if (Array.isArray(c)) return c.map(stripImages);
  if (c && typeof c === 'object') {
    if (c.type === 'image') return { type: 'image', omitted: true };
    return Object.fromEntries(Object.entries(c).map(([k, v]) => [k, stripImages(v)]));
  }
  return c;
}

function cleanTranscript(stdout, scrub) {
  const out = [];
  for (const line of stdout.split('\n')) {
    if (!line.trim()) continue;
    let e;
    try { e = JSON.parse(line); } catch { continue; }
    if (e.type === 'system' && e.subtype === 'init') {
      out.push({ type: 'system', subtype: 'init', model: e.model, tools: e.tools, permissionMode: e.permissionMode, claude_code_version: e.claude_code_version });
    } else if (e.type === 'assistant' || e.type === 'user') {
      const content = (e.message?.content ?? []).map((c) => {
        if (c.type === 'thinking') return { type: 'thinking', thinking: c.thinking };
        if (c.type === 'redacted_thinking') return { type: 'redacted_thinking' };
        return stripImages(c);
      });
      out.push({ type: e.type, timestamp: e.timestamp, content });
    } else if (e.type === 'result') {
      out.push({ type: 'result', subtype: e.subtype, is_error: e.is_error, num_turns: e.num_turns, duration_ms: e.duration_ms,
        total_cost_usd: e.total_cost_usd, permission_denials: e.permission_denials, result: e.result });
    }
  }
  return scrub(out.map((x) => JSON.stringify(x)).join('\n') + '\n');
}

// Each run is its own process group, so servers it starts in the background can be killed.
function exec(cmd, argv, opts = {}) {
  return new Promise((resolve) => {
    const started = Date.now();
    const child = spawn(cmd, argv, { cwd: opts.cwd, env: opts.env ?? process.env, stdio: ['ignore', 'pipe', 'pipe'], detached: true });
    let stdout = '', stderr = '';
    child.stdout.on('data', (d) => (stdout += d));
    child.stderr.on('data', (d) => (stderr += d));
    const timer = setTimeout(() => { try { process.kill(-child.pid, 'SIGTERM'); } catch {} }, opts.timeoutMs ?? RUN_TIMEOUT_MS);
    child.on('close', (code) => {
      clearTimeout(timer);
      try { process.kill(-child.pid, 'SIGKILL'); } catch {}
      resolve({ code, stdout, stderr, ms: Date.now() - started });
    });
  });
}

// Kill anything still running with a working directory inside the run's temp dir (servers the
// verifier started with nohup/setsid escape the process group).
function killLeftovers(dir) {
  const r = spawnSync('lsof', ['-t', '+D', dir], { encoding: 'utf8' });
  const pids = [...new Set((r.stdout || '').split('\n').filter(Boolean).map(Number))].filter((p) => p !== process.pid);
  for (const p of pids) { try { process.kill(p, 'SIGKILL'); } catch {} }
  return pids.length;
}

function environment(a) {
  const v = (cmd, argv) => { const r = spawnSync(cmd, argv, { encoding: 'utf8' }); return (r.stdout || r.stderr || '').trim(); };
  const sha = (p) => crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
  return {
    date: new Date().toISOString(),
    claude_cli: v('claude', ['--version']),
    model: a.model,
    go: v('go', ['version']),
    node: process.version,
    playwright_core: readJSON(path.join(process.env.PW_NODE_MODULES, 'playwright-core', 'package.json')).version,
    platform: `${os.platform()} ${os.release()} ${os.arch()}`,
    tools: TOOLS,
    sandbox: { 'reality-checker': readJSON('sandbox.json'), 'evidence-collector': 'none (see EXPERIMENT.md)' },
    persona_sha256: Object.fromEntries(Object.keys(ROLES).map((r) => [r, sha(path.join('personas', `${r}.md`))])),
    persona_catalog_commit: process.env.PERSONA_CATALOG_COMMIT || 'unknown',
  };
}

function promptFor(role, arm) {
  return fs.readFileSync(path.join('prompts', `${role}-${arm.endsWith('brief') ? 'brief' : 'vague'}.txt`), 'utf8').trim();
}

// ---------- one run ----------
async function runOne(a, r) {
  const cfg = ROLES[r.role];
  const tmp = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'pexp3-')));
  const work = path.join(tmp, 'work');
  fs.cpSync(path.join('fixtures', r.role), work, { recursive: true });
  if (cfg.nodeModules) fs.cpSync(process.env.PW_NODE_MODULES, path.join(work, 'node_modules'), { recursive: true, dereference: true });
  const before = new Set(walk(work));
  const argv = ['-p', promptFor(r.role, r.arm), '--model', a.model, '--no-session-persistence', '--safe-mode',
    '--tools', TOOLS.join(','), '--allowedTools', ...TOOLS, '--output-format', 'stream-json', '--verbose', '--max-budget-usd', MAX_BUDGET_USD];
  if (cfg.sandbox) argv.push('--settings', path.resolve('sandbox.json'));
  if (r.arm.startsWith('persona')) argv.push('--append-system-prompt-file', path.resolve('personas', `${r.role}.md`));
  // The sandbox lets a run write only its working directory and the CLI's own temp dir, so the
  // Go build cache goes in the latter (per run; removed afterwards).
  const gocache = path.join(SANDBOX_TMP, `pexp3-gocache-${r.id}`);
  const env = { ...process.env, GOCACHE: gocache, GOTOOLCHAIN: 'local', GOPROXY: 'off', GOFLAGS: '-mod=mod', npm_config_offline: 'true' };
  const res = await exec('claude', argv, { cwd: work, env });
  const leftovers = killLeftovers(tmp);
  const scrub = scrubber([work, tmp]);
  const lines = res.stdout.trim().split('\n');
  let j = null;
  for (let i = lines.length - 1; i >= 0 && !j; i--) { try { const e = JSON.parse(lines[i]); if (e.type === 'result') j = e; } catch {} }
  const ok = res.code === 0 && j && !j.is_error && typeof j.result === 'string';
  const out = { ok, exit: res.code, wall_ms: res.ms, stderr: scrub(res.stderr).slice(0, 4000), transcript: cleanTranscript(res.stdout, scrub), leftovers };
  if (j) {
    out.result = scrub(j.result ?? '');
    out.meta = { duration_ms: j.duration_ms, num_turns: j.num_turns, total_cost_usd: j.total_cost_usd, usage: j.usage, modelUsage: j.modelUsage,
      subtype: j.subtype, is_error: j.is_error, permission_denials: (j.permission_denials || []).map((d) => d.tool_name) };
  }
  // Files the verifier created (screenshots, scripts, logs), excluding caches and node_modules.
  out.created = walk(work).filter((f) => !before.has(f) && !/(^|\/)(node_modules|gocache)\//.test(f));
  out.images = out.created.filter((f) => /\.png$/i.test(f)).slice(0, 12).map((f) => ({ name: f, data: fs.readFileSync(path.join(work, f)) }));
  fs.rmSync(tmp, { recursive: true, force: true });
  fs.rmSync(gocache, { recursive: true, force: true });
  return out;
}

function walk(dir, base = dir) {
  const out = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) { if (e.name !== 'node_modules') out.push(...walk(p, base)); }
    else out.push(path.relative(base, p));
  }
  return out;
}

function save(dir, rawPath, r, out, attempts) {
  fs.mkdirSync(dir, { recursive: true });
  writeJSON(rawPath, { id: r.id, attempts, wall_ms: out.wall_ms, leftover_processes_killed: out.leftovers, files_created: out.created, meta: out.meta ?? null });
  if (!out.ok) return false;
  fs.writeFileSync(path.join(dir, 'output.md'), out.result + '\n');
  for (const img of out.images) {
    const dest = path.join(dir, 'files', img.name.replace(/[\/]/g, '__'));
    fs.mkdirSync(path.dirname(dest), { recursive: true });
    fs.writeFileSync(dest, img.data);
  }
  return true;
}

async function withRetries(a, r) {
  const attempts = [];
  let out;
  for (let attempt = 1; attempt <= 3; attempt++) {
    out = await runOne(a, r);
    attempts.push({ attempt, ok: out.ok, exit: out.exit, subtype: out.meta?.subtype, stderr: out.ok ? '' : out.stderr.slice(0, 500) });
    if (out.ok) break;
    console.error(`${r.id}: attempt ${attempt} failed; retrying`);
  }
  return { out, attempts };
}

// ---------- pilot (fixture check only; excluded from results) ----------
async function cmdPilot(a) {
  if (!ROLES[a.role] || !a.label) throw new Error('pilot needs --role and --label');
  const r = { id: `${a.role}-${a.label}`, role: a.role, arm: 'none-vague' };
  const dir = path.join('pilot', r.id);
  if (fs.existsSync(dir)) throw new Error(`${dir} exists`);
  const { out, attempts } = await withRetries(a, r);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'transcript.jsonl'), out.transcript);
  save(dir, path.join(dir, 'raw.json'), r, out, attempts);
  console.error(`pilot ${r.id} done ok=${out.ok} $${out.meta?.total_cost_usd?.toFixed(3)} turns=${out.meta?.num_turns}`);
}

// ---------- run ----------
async function cmdRun(a) {
  let idmap;
  if (fs.existsSync('idmap.json')) {
    idmap = readJSON('idmap.json');
  } else {
    const planned = [];
    for (const role of Object.keys(ROLES)) for (const arm of ARMS) for (let rep = 1; rep <= a.reps; rep++) planned.push({ role, arm, rep });
    const order = shuffle(planned, a.seed);
    idmap = { seed: a.seed, runs: order.map((r, i) => ({ id: crypto.randomBytes(4).toString('hex'), order: i + 1, ...r })) };
    writeJSON('idmap.json', idmap);
  }
  writeJSON('environment.json', environment(a));
  let done = 0;
  // One sequential lane per role, the lanes in parallel.
  await Promise.all(Object.keys(ROLES).map(async (role) => {
    for (const r of idmap.runs.filter((x) => x.role === role)) {
      const dir = path.join('outputs', r.id);
      if (fs.existsSync(path.join(dir, 'output.md'))) continue;
      const { out, attempts } = await withRetries(a, r);
      fs.mkdirSync('transcripts', { recursive: true });
      fs.writeFileSync(path.join('transcripts', `${r.id}.jsonl`), out.transcript);
      if (!save(dir, path.join('raw', `${r.id}.json`), r, out, attempts)) { console.error(`${r.id}: failed after retries`); continue; }
      console.error(`done ${++done}/${idmap.runs.length}: ${r.id}`);
    }
  }));
}

// ---------- tool log (for the grader and the hand-check) ----------
function toolLog(transcriptPath, limit = 4000) {
  const lines = fs.readFileSync(transcriptPath, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l));
  const calls = new Map();
  const out = [];
  let n = 0;
  const cut = (s) => (s.length > limit ? s.slice(0, limit) + `\n…[${s.length - limit} more chars]` : s);
  for (const e of lines) {
    if (e.type === 'assistant') for (const c of e.content) {
      if (c.type === 'tool_use') {
        calls.set(c.id, ++n);
        const inp = c.name === 'Bash' ? c.input.command : c.name === 'Read' ? c.input.file_path : JSON.stringify(c.input);
        out.push(`### [${n}] ${c.name}: ${cut(String(inp))}`);
      } else if (c.type === 'text' && c.text.trim()) {
        out.push(`(assistant) ${cut(c.text.trim())}`);
      }
    }
    if (e.type === 'user') for (const c of e.content) {
      if (c.type !== 'tool_result') continue;
      const body = Array.isArray(c.content) ? c.content.map((x) => (x.type === 'text' ? x.text : x.type === 'image' ? '[image viewed]' : '')).join('\n') : String(c.content ?? '');
      out.push(`--> [${calls.get(c.tool_use_id) ?? '?'}]${c.is_error ? ' (error)' : ''}\n${cut(body)}`);
    }
  }
  return out.join('\n');
}

// ---------- grade ----------
function graderPrompt(role, dir, transcript) {
  return [
    fs.readFileSync('prompts/grader.md', 'utf8'),
    '## Answer key\n```json\n' + fs.readFileSync(`keys/${role}.json`, 'utf8') + '```',
    '## Tool log (commands the assistant ran, results truncated)\n```\n' + toolLog(transcript) + '\n```',
    '## The final report to grade\n<report>\n' + fs.readFileSync(path.join(dir, 'output.md'), 'utf8') + '\n</report>',
  ].join('\n\n');
}

function extractJSON(text) {
  const fence = text.match(/```(?:json)?\s*([\s\S]*?)```/);
  const body = fence ? fence[1] : text.slice(text.indexOf('{'), text.lastIndexOf('}') + 1);
  return JSON.parse(body);
}

async function gradeOne(a, id, role, dir, transcript, dest) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pexp3-grade-'));
  const prompt = graderPrompt(role, dir, transcript);
  for (let attempt = 1; attempt <= 3; attempt++) {
    const res = await exec('claude', ['-p', prompt, '--model', a.model, '--no-session-persistence', '--safe-mode',
      '--tools', '', '--output-format', 'json'], { cwd: tmp, timeoutMs: 15 * 60 * 1000 });
    try {
      const j = JSON.parse(res.stdout);
      const grade = extractJSON(j.result);
      writeJSON(dest, { id, task: role, grader_model: a.model, grader_cost_usd: j.total_cost_usd, attempt, grade });
      console.error(`graded ${id}`);
      break;
    } catch (e) {
      console.error(`grade ${id} attempt ${attempt} failed: ${e.message}`);
    }
  }
  fs.rmSync(tmp, { recursive: true, force: true });
}

async function cmdGrade(a) {
  if (a.pilot) {
    const dir = path.join('pilot', a.pilot);
    const role = Object.keys(ROLES).find((r) => a.pilot.startsWith(r + '-'));
    return gradeOne(a, a.pilot, role, dir, path.join(dir, 'transcript.jsonl'), path.join(dir, 'grade.json'));
  }
  const idmap = readJSON('idmap.json');
  // The grader sees only the role's key, the tool log and the report, never idmap.json.
  const todo = idmap.runs.map((r) => ({ id: r.id, role: r.role })).filter((r) => fs.existsSync(path.join('outputs', r.id, 'output.md')));
  let next = 0;
  const list = shuffle(todo, a.seed + '-grade');
  await Promise.all(Array.from({ length: 4 }, async () => {
    while (next < list.length) {
      const { id, role } = list[next++];
      const dest = path.join('grades', `${id}.json`);
      if (!fs.existsSync(dest)) await gradeOne(a, id, role, path.join('outputs', id), path.join('transcripts', `${id}.jsonl`), dest);
    }
  }));
}

// Blind hand-check packet: everything needed to check one grade, with no arm information.
function cmdPacket(a) {
  const run = readJSON('idmap.json').runs.find((r) => r.id === a.id);
  const role = run.role; // the role is not blinded (it follows from the fixture); the arm is
  const g = readJSON(path.join('grades', `${a.id}.json`));
  console.log(`# ${a.id} (${role})\n\n## Grade\n${JSON.stringify(g.grade, null, 2)}\n\n## Report\n${fs.readFileSync(path.join('outputs', a.id, 'output.md'), 'utf8')}\n\n## Tool log\n${toolLog(path.join('transcripts', `${a.id}.jsonl`), 800)}`);
}

// ---------- analyze ----------
const mean = (xs) => xs.reduce((s, x) => s + x, 0) / xs.length;

function metrics(role, g, raw, transcriptPath) {
  const key = readJSON(`keys/${role}.json`);
  const ids = key.defects.map((d) => d.id);
  const tl = fs.readFileSync(transcriptPath, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l));
  const uses = tl.filter((e) => e.type === 'assistant').flatMap((e) => e.content.filter((c) => c.type === 'tool_use'));
  return {
    verdict: g.verdict,
    refused_to_pass: g.verdict !== 'PASS' ? 1 : 0,
    verdict_fail: g.verdict === 'FAIL' ? 1 : 0,
    found: ids.filter((k) => g.defects[k]?.found).length,
    executed: ids.filter((k) => g.defects[k]?.found && g.defects[k]?.executed).length,
    high_found: key.high_ids.filter((k) => g.defects[k]?.found).length,
    decoys_flagged: Object.values(g.decoys || {}).filter((d) => d.flagged).length,
    wrong_claims: g.wrong_claims.length,
    false_alarms: g.wrong_claims.length + Object.values(g.decoys || {}).filter((d) => d.flagged).length,
    unsupported_claims: g.unsupported_claims.length,
    criteria_wrongly_passed: g.criteria_wrongly_passed.length,
    bash_calls: uses.filter((c) => c.name === 'Bash').length,
    tool_calls: uses.length,
    images_viewed: tl.filter((e) => e.type === 'user').flatMap((e) => e.content).filter((c) => c.type === 'tool_result' && JSON.stringify(c.content ?? '').includes('"omitted":true')).length,
    cost_usd: raw.meta?.total_cost_usd ?? 0,
    turns: raw.meta?.num_turns ?? 0,
    duration_s: Math.round((raw.meta?.duration_ms ?? 0) / 1000),
  };
}

function cmdAnalyze() {
  const idmap = readJSON('idmap.json');
  const rows = [];
  for (const r of idmap.runs) {
    const final = path.join('grades', `${r.id}.final.json`);
    const gp = fs.existsSync(final) ? final : path.join('grades', `${r.id}.json`);
    if (!fs.existsSync(gp)) continue;
    const g = readJSON(gp);
    rows.push({ ...r, hand_checked: gp === final, graded: g, m: metrics(r.role, g.grade, readJSON(path.join('raw', `${r.id}.json`)), path.join('transcripts', `${r.id}.jsonl`)) });
  }
  const numeric = (m) => Object.keys(m).filter((k) => typeof m[k] === 'number');
  const table = {};
  for (const role of Object.keys(ROLES)) {
    table[role] = {};
    for (const arm of ARMS) {
      const rs = rows.filter((x) => x.role === role && x.arm === arm).sort((p, q) => p.rep - q.rep);
      table[role][arm] = { n: rs.length, per_run: rs.map((x) => ({ id: x.id, rep: x.rep, hand_checked: x.hand_checked, ...x.m })),
        mean: rs.length ? Object.fromEntries(numeric(rs[0].m).map((k) => [k, +mean(rs.map((x) => x.m[k])).toFixed(k === 'cost_usd' ? 3 : 2)])) : {} };
    }
    // Main effects over the 2x2: mean(with) - mean(without), for each level of the other factor.
    const at = (arm, k) => table[role][arm].mean[k];
    table[role].effects = Object.fromEntries(['refused_to_pass', 'verdict_fail', 'found', 'executed', 'false_alarms', 'unsupported_claims', 'criteria_wrongly_passed', 'cost_usd'].map((k) => [k, {
      persona_given_vague: +(at('persona-vague', k) - at('none-vague', k)).toFixed(3),
      persona_given_brief: +(at('persona-brief', k) - at('none-brief', k)).toFixed(3),
      brief_given_none: +(at('none-brief', k) - at('none-vague', k)).toFixed(3),
      brief_given_persona: +(at('persona-brief', k) - at('persona-vague', k)).toFixed(3),
    }]));
  }
  const graderCost = rows.reduce((s, x) => s + (x.graded.grader_cost_usd || 0), 0);
  const runCost = rows.reduce((s, x) => s + x.m.cost_usd, 0);
  let pilotRuns = 0, pilotGrading = 0;
  if (fs.existsSync('pilot')) for (const d of fs.readdirSync('pilot')) {
    const raw = path.join('pilot', d, 'raw.json'), gr = path.join('pilot', d, 'grade.json');
    if (fs.existsSync(raw)) pilotRuns += readJSON(raw).meta?.total_cost_usd ?? 0;
    if (fs.existsSync(gr)) pilotGrading += readJSON(gr).grader_cost_usd ?? 0;
  }
  const cost = { pilot_runs: +pilotRuns.toFixed(3), pilot_grading: +pilotGrading.toFixed(3), runs: +runCost.toFixed(3), grading: +graderCost.toFixed(3), total: +(pilotRuns + pilotGrading + runCost + graderCost).toFixed(3) };
  writeJSON('analysis.json', { environment: readJSON('environment.json'), total_cost_usd: cost, table });
  console.log(JSON.stringify({ cost, means: Object.fromEntries(Object.entries(table).map(([k, v]) => [k, Object.fromEntries(ARMS.map((arm) => [arm, v[arm].mean]))])) }, null, 2));
}

const a = args();
if (a.cmd === 'pilot') await cmdPilot(a);
else if (a.cmd === 'run') await cmdRun(a);
else if (a.cmd === 'grade') await cmdGrade(a);
else if (a.cmd === 'packet') cmdPacket(a);
else if (a.cmd === 'analyze') cmdAnalyze();
else { console.error('usage: node run.mjs pilot|run|grade|packet|analyze [--role R --label L] [--reps N] [--seed S] [--pilot ID] [--id ID]'); process.exit(2); }
