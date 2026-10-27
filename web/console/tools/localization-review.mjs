#!/usr/bin/env node
// Synthetic dependencies, actual candidate entry points and CSS. No live daemon.
import { createServer as httpServer } from 'node:http'
import { createServer as httpsServer } from 'node:https'
import { spawn, execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { readFile, writeFile, mkdir, stat, cp } from 'node:fs/promises'
import { resolve, join, extname, dirname, relative } from 'node:path'
import { pathToFileURL, fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'

export const REVIEW_LOCALES = ['en', 'zh-Hant', 'ja', 'zh-Hans', 'ko', 'es', 'pt-BR', 'fr', 'de']
export const REVIEW_STATES = ['ready', 'empty', 'failed', 'offline', 'signed-out', 'forbidden', 'pairing', 'stale', 'usage-partial', 'device-limit', 'device-list-failed', 'device-ticket-expired', 'device-revoke-expired']
export const REVIEW_SESSION = 'review-session'
const self = fileURLToPath(import.meta.url)
const mime = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2' }

export function reviewArgs(args) {
  const options = {}
  for (let i = 0; i < args.length; i++) {
    const key = args[i].replace(/^--/, '')
    if (['serve', 'build', 'help', 'builtins'].includes(key)) options[key] = true
    else if (args[i].startsWith('--') && args[i + 1] && !args[i + 1].startsWith('--')) options[key] = args[++i]
    else throw new Error(`Unknown or incomplete argument: ${args[i]}`)
  }
  return options
}

function run(command, args, options = {}) {
  return new Promise((yes, no) => {
    const child = spawn(command, args, { stdio: 'inherit', ...options })
    child.once('error', no)
    child.once('exit', (code, signal) => code === 0 ? yes() : no(new Error(`${command} exited ${code ?? signal}`)))
  })
}

export async function buildReview({ repo, out, cloudOrigin }) {
  assertOutsideCandidate(repo, out)
  const consoleRoot = join(repo, 'web/console')
  const require = createRequire(join(repo, 'web/package.json'))
  const { build } = await import(pathToFileURL(require.resolve('vite')).href)
  const { default: react } = await import(pathToFileURL(require.resolve('@vitejs/plugin-react')).href)
  // The console and input-bar documents. No source rewrite and no candidate writes.
  for (const hosted of [false, true]) {
    const declaration = JSON.stringify({ v: 1, app_origin: cloudOrigin, api_origin: cloudOrigin, relay_url: cloudOrigin.replace('https:', 'wss:') + '/fixture-relay' })
    await build({ configFile: false, root: consoleRoot, plugins: [react()], base: hosted ? '/' : './',
      define: { 'import.meta.env.VITE_HOSTED_CONSOLE': hosted ? JSON.stringify(declaration) : 'undefined' },
      build: { outDir: join(out, hosted ? 'cloud' : 'app'), emptyOutDir: true,
        rollupOptions: { input: Object.fromEntries(['index', 'bar'].map(name => [name, join(consoleRoot, name + '.html')])) } },
    })
  }
}

function assertOutsideCandidate(repo, output) {
  const path = relative(resolve(repo), resolve(output))
  if (path === '' || (!path.startsWith('..') && !path.startsWith('/'))) throw new Error('Review output must be outside the read-only candidate')
}

const syntheticRow = () => ({ id: REVIEW_SESSION, label: 'Authored sample — release checklist', backend: 'owned', assistant: 'claude',
  state: 'idle', work_state: 'ready', evidence: 'process', isClaude: true,
  sessionId: '10000000-0000-4000-8000-000000000001', cwd: '/tmp/review-project' })
const snapshot = (state) => ({ at: Date.now(), scan: { complete: true, emptyAuthoritative: true, epoch: 1, generation: 1,
  completed: { complete: true, sequence: 1 }, provenance: 'synthetic-review' }, sessions: state === 'empty' ? [] : [syntheticRow()] })
const project = { id: 'review-project', label: 'Authored sample project', path: '/tmp/review-project', at: 1 }

/** Current usage-overview.schema.json wire shape; all values are synthetic. */
export function reviewUsage(url, state) {
  const days = url.searchParams.get('days') || '7'
  if (!['7', '28'].includes(days) || [...url.searchParams.keys()].some(key => !['days', 'project'].includes(key)) || [...url.searchParams.keys()].some(key => url.searchParams.getAll(key).length !== 1)) return null
  const selected = url.searchParams.get('project') || ''
  if (!['', 'review-project', 'review-project-b', 'unattributed'].includes(selected)) return null
  const count = Number(days), end = Date.UTC(2026, 0, 15) / 1000, day = 86400
  const empty = state === 'empty', partial = state === 'usage-partial'
  const series = (prior) => Array.from({ length: count }, (_, i) => ({
    date: new Date((end - (count - 1 - i + (prior ? count : 0)) * day) * 1000).toISOString().slice(0, 10),
    tokens: partial && i === 2 ? null : empty ? 0 : (i + 1) * (prior ? 800 : 1200) * (selected ? 1 : 2),
    dispatches: empty ? 0 : i % 4 + 1, duration_samples: empty ? 0 : i % 4 + 1,
    duration_median_seconds: empty ? null : 120 + i * 30, coverage: partial && i === 2 ? 'missing' : 'complete',
  }))
  const daily = series(false), previous = series(true)
  const totals = rows => {
    const tokens = rows.reduce((sum, row) => sum + (row.tokens ?? 0), 0)
    // Every synthetic task on a day has that day's duration; aggregate the
    // actual sample population rather than inventing an unrelated median.
    const durations = rows.flatMap(row => Array(row.duration_samples).fill(row.duration_median_seconds)).sort((a, b) => a - b)
    const middle = Math.floor(durations.length / 2)
    return { tokens, dispatches: rows.reduce((sum, row) => sum + row.dispatches, 0), duration_samples: rows.reduce((sum, row) => sum + row.duration_samples, 0),
      duration_median_seconds: empty ? null : durations.length % 2 ? durations[middle] : (durations[middle - 1] + durations[middle]) / 2,
      parts: { input: tokens * .2, cache_read: tokens * .5, cache_write: tokens * .1, output: tokens * .2 } }
  }
  const total = totals(daily)
  const options = [{ id: 'review-project', name: 'Synthetic project Alpha' }, { id: 'review-project-b', name: 'Synthetic project Beta' }, { id: 'unattributed', name: 'Synthetic unattributed' }]
  return { at: end, days: count, timezone: 'UTC', since: end - (count - 1) * day, until: end + day,
    previous_since: end - (2 * count - 1) * day, previous_until: end - (count - 1) * day, project: selected,
    totals: total, previous_totals: totals(previous), daily, previous_daily: previous,
    projects: empty ? [] : (selected ? options.filter(row => row.id === selected).map(row => ({ ...row, tokens: total.tokens })) : options.map((row, i) => ({ ...row, tokens: total.tokens * [.6, .3, .1][i] }))),
    project_options: options, coverage: { state: partial ? 'partial' : 'complete', sessions_read: partial ? 3 : 4, sessions_total: 4,
      history_from: end - 60 * day, truncated: partial, tasks_truncated: partial } }
}

export function reviewWorkItem() {
  return { id: 'review-item', project: { ...project, icon: null, available: true }, kind: 'issue',
    title: '合成審查項目：驗證有資料畫面', description: '這是本機審查專用合成資料，可開啟詳情及編輯表單。',
    acceptance_criteria: '用量圖表與看板詳情可讀取，所有資料均標示為合成。', acceptance_version: 1, acceptance_digest: 'synthetic',
    gate_snapshot_cycle: 0, gate_snapshot_at: null, planning_gate: false, verify_gate: false, phase: 'created', condition: null,
    user_action: '', area: 'unassigned', deployment_policy: 'not_required', review_required: false, owner_session: null,
    created_at: 1768478400, updated_at: 1768478400, closed_at: null, cycle: 1, version: 1, documents: [], images: [],
    steps: [{ id: 'review-step', title: '核對合成資料與實際畫面', done: false, position: 0, created_by: 'synthetic-review', completed_by: '', completed_at: null, version: 1 }] }
}

/** ProjectUnifyPlan, matching project_unify.go and the real preview consumer. */
export function reviewUnifyPlan() {
  return { status: 'drifting', version: 'synthetic-plan-1', links_available: true,
    rules: { agents: 'present', claude: 'present', claude_imports_agents: false,
      claude_only_files: [], claude_only_lines: ['Synthetic assistant-only instruction.'],
      now: { claude: ['CLAUDE.md'], codex: ['AGENTS.md'] },
      after: { claude: ['CLAUDE.md', 'AGENTS.md'], codex: ['AGENTS.md'] } },
    skills: [{ name: 'synthetic-review', place: 'agents', linked: false, copy: false,
      now: { claude: false, codex: true }, after: { claude: true, codex: true }, action: 'skill_link' }],
    actions: [{ kind: 'rules_add_import', paths: ['CLAUDE.md'], description: 'Add the shared rules import.',
      edits: [{ path: 'CLAUDE.md', before: 'Synthetic assistant-only instruction.\n', after: '@AGENTS.md\nSynthetic assistant-only instruction.\n' }] },
      { kind: 'skill_link', paths: ['.claude/skills/synthetic-review'], link_target: '../../.agents/skills/synthetic-review',
        description: 'Share the synthetic review skill.', edits: [] }],
    conflicts: [{ kind: 'claude_only_lines', path: 'CLAUDE.md', detail: 'Synthetic assistant-only instruction.' }] }
}

/** GitReply and GitDiffReply, matching git.go; no filesystem reads. */
export function reviewGit() {
  return { git: { branch: 'synthetic-review', head: '1234567890abcdef', ahead: 1, behind: 0, clean: false,
    files: [{ path: 'README.md', from: null, kind: 'modified', staged: false, unstaged: true, additions: 1, deletions: 1 }] } }
}

export function reviewGitDiff() {
  return { diff: { path: 'README.md', kind: 'modified', patches: [{ scope: 'unstaged',
    unifiedDiff: 'diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-Synthetic previous text\n+Synthetic reviewed text\n' }] } }
}

export function reviewCapacity() {
  return { counting_since: 1768470000, beat: { running: true, stalled: false, tick_seconds: 60, passes: 2, event_errors: 0, at: 1768478400 }, entries: [
    { name: 'Synthetic review evidence', class: 'evidence', unit: 'rows', used: 24, limit: 100, ratio: .24, state: 'ok', warn_at: .8,
      at_limit: 'refuse', evicted_by: 'person', told: ['diagnostics'], exhausted: false, overridden: false, refused: 0, evicted: 0,
      expired: 0, rotated: 0, dropped: 0, coalesced: 0, disconnected: 0, write_errors: 0, notices: 0, notices_suppressed: 0,
      note: 'Synthetic review reading', measured_at: 1768478400 },
  ] }
}

export function sampleSquad() {
  const icon = { accent: '#D97757', cells: Array.from({ length: 7 }, () => Array.from({ length: 8 }, () => '#D97757')) }
  const names = text => ({ en: text, 'zh-Hant': text })
  const skill = { skill_id: 'review.skill.evidence', version: '1', source: 'synthetic-review', license: 'MIT', digest: 'synthetic',
    name: names('Authored sample evidence skill'), purpose: names('Authored sample: inspect the evidence before reporting.'),
    content: '# Authored sample skill\n\nRead the sample project. Report observed evidence.\n', icon, builtin: false }
  const definition = { definition_id: 'review.persona.reader', short_id: 'review-reader', version: '1', source: 'synthetic-review',
    license: 'MIT', digest: 'synthetic', name: names('Authored sample reader'), summary: names('Authored sample: review the interface and its evidence.'),
    body: '# Authored sample role\n\nRead the sample project and report observed behavior.\n', icon,
    teams: [{ id: 'review.team.quality', version: '1' }], skills: [{ id: skill.skill_id, version: skill.version, enabled: true }], builtin: false }
  return { catalog_version: 1, definitions: [definition], skills: [skill], teams: [{ team_id: 'review.team.quality', version: '1',
    name: names('Authored sample team'), personas: [{ id: definition.definition_id, version: definition.version }] }] }
}

export function reviewSquadSettings(catalog, scope = 'global') {
  const field = value => ({ value, source: 'default', present: false, version: 0 })
  return { scope_id: scope, scope_kind: scope === 'global' ? 'global' : 'repo', motion: field(false), motion_settings_version: 0,
    auto_dispatch: field(false), auto_dispatch_version: 0,
    personas: catalog.definitions.map(row => ({ definition_id: row.definition_id, settings_version: 0,
      handbook: field('Authored sample handbook. Preserve authored content.'), global_handbook: field('Authored sample handbook. Preserve authored content.'),
      auto_assign: field(true), skills: field(row.skills || []) })) }
}

/** Public JSON data, not parsed Go source; relationships remain explicit fixture data. */
export async function candidateBuiltinFixtures(repo, displayFile) {
  const display = JSON.parse(await readFile(displayFile || join(repo, 'web/console/src/builtin-display-source.json'), 'utf8'))
  const icon = { accent: '#D97757', cells: Array.from({ length: 7 }, () => Array.from({ length: 8 }, () => '#D97757')) }
  const ids = domain => [...new Set(Object.keys(display.en).filter(key => key.startsWith(domain + '.') && key.endsWith('.name')).map(key => key.slice(domain.length + 1, -5)))]
  const pair = key => ({ en: display.en[key], 'zh-Hant': display['zh-Hant'][key] })
  const valid = id => { if (!/^[a-z][a-z0-9-]*$/u.test(id)) throw new Error('Invalid public built-in display id'); return id }
  const skills = await Promise.all(ids('builtinSkill').map(async id => ({ skill_id: 'clawdline.skill.' + valid(id), version: 'review',
    name: pair(`builtinSkill.${id}.name`), purpose: pair(`builtinSkill.${id}.purpose`), icon, source: 'candidate-public-data', license: 'see authored source',
    content: await readFile(join(repo, 'internal/domain/squad/builtin_skills', id + '.md'), 'utf8'), builtin: true })))
  const definitions = await Promise.all(ids('builtinPersona').map(async id => ({ definition_id: 'clawdline.persona.' + valid(id), short_id: id, version: 'review',
    name: pair(`builtinPersona.${id}.name`), summary: pair(`builtinPersona.${id}.summary`), icon, source: 'candidate-public-data', license: 'see authored source',
    body: await readFile(join(repo, 'internal/domain/persona/catalog', id + '.md'), 'utf8'), builtin: true,
    teams: [{ id: 'clawdline.team.engineering', version: 'review' }], skills: skills.map(skill => ({ id: skill.skill_id, version: skill.version, enabled: true })) })))
  return { squadCatalog: { catalog_version: 1, definitions, skills, teams: [{ team_id: 'clawdline.team.engineering', version: 'review',
    name: { en: 'engineering', 'zh-Hant': 'engineering' }, personas: definitions.map(row => ({ id: row.definition_id, version: row.version })) }] } }
}

function json(response, status, value) {
  response.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' })
  response.end(JSON.stringify(value))
}

export async function serveReview(options) {
  const repo = resolve(options.repo)
  const out = resolve(options.out)
  const manifestFile = resolve(options.manifest)
  const servedOut = resolve(options.publish || out)
  const reused = options.reuse ? JSON.parse(await readFile(resolve(options.reuse), 'utf8')) : null
  if (reused && (!reused.synthetic || resolve(reused.out) !== servedOut)) throw new Error('--reuse requires a synthetic manifest and the same served output directory')
  assertOutsideCandidate(repo, out)
  assertOutsideCandidate(repo, manifestFile)
  assertOutsideCandidate(repo, servedOut)
  const candidateCommit = reused?.candidateCommit || execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
  const candidateStatus = reused ? '' : execFileSync('git', ['-C', repo, 'status', '--porcelain'], { encoding: 'utf8' })
  if (options['expect-commit'] && options['expect-commit'] !== candidateCommit) throw new Error('Candidate HEAD does not match --expect-commit')
  if (candidateStatus.trim()) throw new Error('Candidate must be clean so the rendered tree has a reviewable identity')
  await mkdir(out, { recursive: true })
  await mkdir(dirname(manifestFile), { recursive: true })
  const keyFile = join(out, 'loopback-key.pem'), certFile = join(out, 'loopback-cert.pem')
  try { await stat(certFile) } catch {
    await run('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', keyFile, '-out', certFile,
      '-days', '2', '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1,DNS:localhost'])
  }
  const { demoOverview } = await import(pathToFileURL(join(repo, 'web/console/src/cloud/admin/demo.ts')).href)
  if (options.fixtures && options.builtins) throw new Error('Choose --fixtures or --builtins')
  const providedFixtures = options.fixtures ? JSON.parse(await readFile(resolve(options.fixtures), 'utf8')) : options.builtins ? await candidateBuiltinFixtures(repo, options['builtin-display']) : null
  const squadCatalog = providedFixtures?.squadCatalog || sampleSquad()
  if (!Array.isArray(squadCatalog.definitions) || !Array.isArray(squadCatalog.skills) || !Array.isArray(squadCatalog.teams)) throw new Error('--fixtures must contain a squadCatalog with definitions, skills and teams arrays')
  const personas = providedFixtures?.personas || squadCatalog.definitions.map(row => ({ id: row.short_id || row.definition_id,
    name: row.name, summary: row.summary, icon: row.icon, source: row.source,
    teams: row.teams.map(team => team.id.replace(/^clawdline\.team\./, '')), suggested_kinds: ['issue', 'feature'] }))
  const fixtureData = { squadCatalog, personas, workItem: reviewWorkItem(), capacity: reviewCapacity(), usage: Object.fromEntries(
    ['7', '28'].map(days => [days, reviewUsage(new URL('http://fixture.invalid/v1/usage/overview?days=' + days), 'ready')])) }
  const requests = []
  // Bounded, per-context synthetic recovery. No persistence or production origin.
  const recovery = new Map()
  // State belongs to a browser cookie, so reviewers do not change each other's scenario.
  const handler = (kind) => async (request, response) => {
    try {
      const url = new URL(request.url, 'http://fixture.invalid')
      const path = url.pathname
      const cookies = Object.fromEntries((request.headers.cookie || '').split(';').map(x => x.trim().split('=')))
      const queryState = url.searchParams.get('review_state')
      if (queryState && !REVIEW_STATES.includes(queryState)) return json(response, 400, { error: 'invalid_review_state' })
      const state = queryState || (REVIEW_STATES.includes(cookies.review_state) ? cookies.review_state : 'ready')
      if (queryState) response.setHeader('set-cookie', [`review_state=${queryState}; Path=/; SameSite=Strict`, 'review_overview_seen=0; Path=/; SameSite=Strict'])
      const tag = REVIEW_LOCALES.includes(url.searchParams.get('lang')) ? url.searchParams.get('lang') : 'en'
      if (path === '/__review/state') {
        const next = url.searchParams.get('state')
        if (!REVIEW_STATES.includes(next)) return json(response, 400, { error: 'invalid_review_state', allowed: REVIEW_STATES })
        response.setHeader('set-cookie', [`review_state=${next}; Path=/; SameSite=Strict`, 'review_overview_seen=0; Path=/; SameSite=Strict'])
        return json(response, 200, { state: next, synthetic: true })
      }
      if (path === '/__review/requests') return json(response, 200, { requests: url.searchParams.has('actor') ? requests.filter(row => row.actor === url.searchParams.get('actor')) : requests })
      if (path === '/__review/manifest') return json(response, 200, manifest)
      if (path.startsWith('/v1/') || path.startsWith('/su/')) {
        const actor = cookies.review_actor || ''
        const record = { kind, method: request.method, path, query: url.search, state, actor, status: null, synthetic: true }
        requests.push(record)
        response.once('finish', () => { record.status = response.statusCode })
        if (path === '/v1/strings') return json(response, 200, JSON.parse(await readFile(join(servedOut, kind, 'catalogs', tag + '.json'), 'utf8')))
        if (path === '/v1/auth/session') {
          if (!['GET', 'POST'].includes(request.method)) return json(response, 405, { error: { code: 'review_mutation_unsupported' } })
          if (state === 'signed-out' || request.method === 'GET') return json(response, 401, { error: { code: 'not_signed_in' } })
          if (state === 'failed') return json(response, 503, { error: { code: 'fixture_session_failed', message: 'Synthetic API refusal' } })
          if (state.startsWith('device-') && !recovery.get(actor)?.revoked) return json(response, 409, { error: { code: 'device_limit_reached', details: { tier: 'free', limit: 5 } } })
          return json(response, 201, { account_id: 'review-account', device_id: 'review-viewer', caps: ['read', 'send_prompt'] })
        }
        if (path.startsWith('/v1/auth/recovery/devices') && state.startsWith('device-')) {
          if (!actor) return json(response, 400, { error: { code: 'synthetic_actor_required' } })
          // Bound accepted mutations to 256 isolated contexts per process.
          if (!recovery.has(actor)) {
            if (recovery.size >= 256) return json(response, 429, { error: { code: 'synthetic_context_limit' } })
            const now = Date.now()
            recovery.set(actor, { revoked: false, devices: [
              { id: 'review-current', name: 'Synthetic current browser', kind: 'browser', created_at: new Date(now - 86400000 * 10).toISOString(), last_seen_at: new Date(now - 60000).toISOString() },
              { id: 'review-oldest', name: 'Synthetic oldest tablet', kind: 'ios', created_at: new Date(now - 86400000 * 90).toISOString(), last_seen_at: new Date(now - 86400000 * 60).toISOString() },
              ...['android', 'browser', 'ios'].map((kind, index) => ({ id: 'review-device-' + index, name: 'Synthetic viewer ' + (index + 1), kind,
                created_at: new Date(now - 86400000 * 30).toISOString(), last_seen_at: new Date(now - 86400000 * (index + 2)).toISOString() })),
            ] })
          }
          if (state === 'device-ticket-expired' || (state === 'device-revoke-expired' && request.method === 'DELETE')) return json(response, 401, { error: { code: 'login_ticket_expired' } })
          if (state === 'device-list-failed') return json(response, 503, { error: { code: 'synthetic_recovery_unavailable' } })
          const held = recovery.get(actor)
          if (request.method === 'GET' && path === '/v1/auth/recovery/devices') return json(response, 200, { tier: 'free', limit: 5, active: held.devices.length, devices: held.devices })
          if (request.method === 'DELETE' && path.split('/').length === 6) {
            const id = decodeURIComponent(path.split('/').at(-1))
            if (!held.devices.some(row => row.id === id)) return json(response, 404, { error: { code: 'synthetic_device_not_found' } })
            held.devices = held.devices.filter(row => row.id !== id); held.revoked = true
            record.result = { status: 'revoked', device_id: id, remaining: held.devices.length }
            return json(response, 200, { status: 'revoked' })
          }
        }
        if (request.method !== 'GET') return json(response, 405, { error: { code: 'review_mutation_unsupported', message: 'This fixture does not persist product mutations.' } })
        if (path === '/v1/projects/review-project/unify') return url.search ? json(response, 400, { error: 'bad_request' }) : json(response, 200, reviewUnifyPlan())
        if (path === `/v1/sessions/${REVIEW_SESSION}/git`) return json(response, 200, reviewGit())
        if (path === `/v1/sessions/${REVIEW_SESSION}/git/diff`) {
          if (!url.searchParams.get('path')) return json(response, 400, { error: 'bad_request' })
          if (url.searchParams.get('path') !== 'README.md') return json(response, 404, { error: 'git_file_not_changed' })
          return json(response, 200, reviewGitDiff())
        }
        if (path === '/v1/devices') return json(response, 200, { devices: [{ id: 'review-viewer', caps: ['read', 'send_prompt'] }] })
        if (path === '/v1/machines') return json(response, 200, { machines: [] })
        if (path === '/su/v1/operations/overview') {
          if (state === 'forbidden') return json(response, 404, { error: { code: 'not_found' } })
          if (state === 'failed' || (state === 'stale' && cookies.review_overview_seen === '1')) return json(response, 503, { error: { code: 'fixture_overview_failed' } })
          response.setHeader('set-cookie', 'review_overview_seen=1; Path=/; SameSite=Strict')
          return json(response, 200, { schema: 'clawdline.operations.overview.v1', ...demoOverview(new Date('2026-01-15T12:00:00Z')) })
        }
        if (path === '/su/v1/promo/campaigns') return json(response, 200, { total: 0, offset: 0, rows: [] })
        if (state === 'offline') return json(response, 503, { error: { code: 'machine_offline', message: 'Synthetic machine offline' } })
        if (path === '/v1/health') return json(response, 200, { ok: true, authed: true, api_level: 999, version: 'synthetic-review', platform: 'linux' })
        if (state === 'failed' && !path.includes('/auth/')) return json(response, 503, { error: { code: 'fixture_unavailable', message: 'Synthetic read failure' } })
        if (path === '/v1/usage/overview') {
          const data = reviewUsage(url, state)
          return data ? json(response, 200, data) : json(response, 400, { error: { code: 'bad_request' } })
        }
        if (path === '/v1/usage/items/review-item') return json(response, 200, { item_id: 'review-item', calls: 0, peak_context: 0, owners: [], sessions: [], tasks: [], gaps: [],
          bill: { categories: [], share_of: 'tokens', total: { input: 0, cache_read: 0, cache_write_1h: 0, cache_write_5m: 0, output: 0, total: 0, cost: 0, cost_known: true, unpriced: 0 } } })
        if (path === '/v1/capacity') return json(response, 200, { at: 1768478400, capacity: reviewCapacity() })
        if (path === '/v1/diagnostics') return json(response, 200, { at: 1768478400, ok: true, dir: '/synthetic/review', port: 0, served_by: 'synthetic-review',
          capacity: reviewCapacity(), platform: { os: 'linux', arch: 'amd64', capabilities: [] }, console: { state: 'served', detail: 'Synthetic console' },
          scheduler: { considered: 0, due: 0, fired: 0, tickSeconds: 60 } })
        if (path === '/v1/auth/devices') return json(response, 200, { configured: true, password: false,
          devices: [{ id: 'synthetic-local-viewer', name: 'Synthetic local viewer', created: 1768470000, last_seen: 1768478400, caps: ['read', 'send'] }] })
        if (path === '/v1/settings/dispatch-policy') return json(response, 200, { base: '# Synthetic dispatch rules\n\nRead the task. Preserve unrelated work. Report evidence.', local: '# Synthetic local additions\n\nUse only review fixtures.', source: 'next', version: 'synthetic-1' })
        if (path === '/v1/settings/work-gates') return json(response, 200, { planning_gate: true, verify_gate: true })
        if (path === '/v1/notification-rules') return json(response, 200, { rules: [], preferences: { push_on_waiting: true, push_on_capacity: true } })
        if (path === '/v1/cloud/login') return json(response, 200, { phase: 'idle' })
        if (path === '/v1/cloud/status') return json(response, 200, { enabled: false, configured: false, commands: false, terminal_direct: false,
          state: 'off', connects: 0, reconnects: 0, published: 0, acked: 0, publish_errors: 0, inbound_total: 0, answered: 0, refused: 0,
          queue_refused: 0, queue_unanswered: 0, roster_readable: true, pinned_readable: true, devices: [], pairing: { phase: 'idle' } })
        if (path === '/v1/machine/usage') return json(response, 200, { at: 1768478400, cores: 4, cpu_percent: 12, interval_ms: 1000,
          load: [.2, .3, .4], memory_total_bytes: 8589934592, memory_used_bytes: 2147483648, memory_available_bytes: 6442450944,
          swap_total_bytes: 0, swap_used_bytes: 0, groups: [], others: [] })
        if (path === '/v1/project-sync/mirror') return json(response, 200, { clone_root: '/synthetic/review', projects: [], clones: [] })
        if (path === '/v1/projects/review-project/worktrees') return json(response, 200, { projectWorktreeLifecycle: {
          schemaVersion: 1, complete: true, truncated: false, error: null, observedAt: '2026-01-15T12:00:00Z',
          project: { id: project.id, label: project.label }, repository: { canonicalPath: project.path, id: 'synthetic-review' }, rows: [],
          counts: { active: 0, modified: 0, rows: 0, staged: 0, storageBytes: 0, unknown: 0, untracked: 0 } } })
        if (path === '/v1/sessions') return json(response, 200, snapshot(state))
        if (path === '/v1/sessions/restorable') return json(response, 200, { at: 1768478400, available: true, sessions: [] })
        if (path === '/v1/skill') return json(response, 200, { targets: ['claude', 'codex'].map(assistant => ({ assistant, path: '/synthetic/' + assistant + '/skills/clawdline/SKILL.md', state: 'current' })) })
        if (path === '/v1/update/fresh' || path === '/v1/update') return json(response, 200, { state: 'current', running: { stamp: candidateCommit, version: 'synthetic-review' },
          latest: { stamp: candidateCommit, version: 'synthetic-review' }, source_url: 'http://fixture.invalid/synthetic-build', install_kind: 'source', checked_at: '2026-01-15T12:00:00Z' })
        if (path === '/v1/board') return json(response, 200, { board: { enabled: false, revision: 1, narrativeConsent: null,
          viewer: { id: 'synthetic-review', canManage: true, canWrite: true, narrativeProvider: 'codex' }, projects: [] } })
        if (path === '/v1/events') {
          response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
          response.write('event: sessions\ndata: ' + JSON.stringify(snapshot(state)) + '\n\n')
          return
        }
        if (path === '/v1/transcript') return json(response, 200, { entries: [
          { role: 'user', text: 'Authored sample: prepare the release checklist.' },
          { role: 'tool', tool: 'update_plan', plan: [{ step: 'Inspect the sample project', status: 'completed' }, { step: 'Verify the interface', status: 'inProgress' }, { step: 'Write the review', status: 'pending' }] },
          { role: 'assistant', text: 'Authored sample: the checklist is ready for review.' },
        ], evidence: 'process', id: REVIEW_SESSION, signature: 'synthetic-review' })
        if (path === `/v1/sessions/${REVIEW_SESSION}/info`) return json(response, 200, { info: { session: { ...syntheticRow(), title: syntheticRow().label }, models: [], deploy: [], links: [] } })
        if (path === '/v1/places') return json(response, 200, { at: Date.now(), assistants: [{ id: 'claude', label: 'Claude Code', availability: 'available' }], places: state === 'empty' ? [] : [project] })
        if (path === '/v1/settings/default-models') return json(response, 200, { codex_default_model: '', claude_default_model: '', models: { codex: [], claude: [] } })
        if (['/v1/settings', '/v1/settings/browser', '/v1/settings/notifications'].includes(path)) return json(response, 200, { language: 'auto', voice_language: 'auto', hotkey: 'ctrl+space', console_enabled: true, codex_default_model: '', claude_default_model: '', codex_default_effort: '' })
        if (path === '/v1/voice/setup') return json(response, 200, { status: 'ready', platform: 'darwin', binary: '/synthetic/whisper-cli', binary_dirs: ['/synthetic/bin'], binary_install: '', model: '/synthetic/ggml-review.bin', model_name: 'ggml-review.bin', model_bytes: 1048576, model_dirs: ['/synthetic/models'] })
        if (path === '/v1/voice/language') return json(response, 200, { setting: 'auto', code: 'en', script: '', source: 'language', tag: 'en', script_source: '', script_tag: '' })
        if (path === '/v1/personas') return json(response, 200, { license: 'MIT', personas })
        if (path === '/v1/squad/catalog') return json(response, 200, squadCatalog)
        if (path === '/v1/squad/settings') return json(response, 200, reviewSquadSettings(squadCatalog, url.searchParams.has('place_id') ? 'review-project' : 'global'))
        if (path === '/v1/squad/scopes') return json(response, 200, { scopes: [{ place_id: project.id, scope_id: project.id, kind: 'repo', label: project.label, path: project.path }], scope_ids: [project.id] })
        if (path === '/v1/squad/events/head') return json(response, 200, { seq: 0 })
        if (path === '/v1/squad/events') return json(response, 200, { events: [], next_after: 0, has_more: false })
        if (path === '/v1/squad/session-bindings') return json(response, 200, { bindings: [] })
        if (path === '/v1/squad/skill-sources') return json(response, 200, { skills: [] })
        if (path.startsWith('/v1/work/v2/session-todos/')) return json(response, 200, { ok: true, assigned_items: [], recent_items: [], direct_todos: [], open_decisions: [], truncated: false })
        if (path.startsWith('/v1/work/v2/human-interventions/')) return json(response, 200, { ok: true, rows: [], pruned_resolved: 0 })
        if (path === '/v1/snippets') return json(response, 200, { snippets: state === 'empty' ? [] : [{ id: 'review-playbook', title: 'Authored sample playbook', body: 'Read the sample project and describe the next step.', scope: 'playbook', position: 0, created_at: 1, updated_at: 1 }] })
        if (path === `/v1/sessions/${REVIEW_SESSION}/documents`) return json(response, 200, { documents: [{ scope: 'project', path: 'README.md', bytes: 64, modified: 1 }] })
        if (path === `/v1/sessions/${REVIEW_SESSION}/documents/project/README.md`) {
          response.writeHead(200, { 'content-type': 'text/markdown; charset=utf-8' })
          return response.end('# Authored sample\n\nThis content is intentionally not translated.\n')
        }
        if (path === '/v1/work/v2/items') return json(response, 200, { ok: true, rows: state === 'empty' ? [] : [reviewWorkItem()], counts: { unassigned: state === 'empty' ? 0 : 1 }, next_cursor: null, page_size: 40, truncated: false })
        if (path === '/v1/work/v2/items/review-item') return json(response, 200, { ok: true, item: reviewWorkItem() })
        if (path === '/v1/work/v2/proposals') return json(response, 200, { rows: [], truncated: false })
        if (path === '/v1/work/decisions') return json(response, 200, { counts: {}, rows: [], next_cursor: null })
        if (path === '/v1/orchestrator/tasks') return json(response, 200, { at: Date.now(), tasks: [] })
        if (path === '/v1/orchestrator/schedules') return json(response, 200, { schedules: [], tasks: [] })
        if (path === '/v1/projects/review-project/tree') return json(response, 200, { directory: '', entries: [{ name: 'README.md', path: 'README.md', kind: 'file', size: 64 }], truncated: false })
        if (path === '/v1/projects/review-project/tree/file') return json(response, 200, { path: 'README.md', text: '# Authored sample\n\nThis content is intentionally not translated.\n', size: 64, version: 'synthetic-1' })
        if (path === '/v1/projects/review-project/files') return json(response, 200, { files: [], skipped: [], truncated: false })
        // Unknown routes are visible evidence, never silently successful fixtures.
        return json(response, 404, { error: { code: 'review_route_unsupported', message: path } })
      }
      // Unstamped hosted development builds resolve catalogs against the deep
      // document URL. Serve that real catalog file, not the SPA document.
      const documentPath = path.startsWith('/admin/catalogs/') ? path.slice('/admin'.length)
        : path === '/' || path === '/admin' || path.startsWith('/admin/') ? '/index.html' : path
      const directory = join(servedOut, kind)
      const file = resolve(directory, '.' + documentPath)
      if (relative(directory, file).startsWith('..')) return json(response, 403, { error: 'outside_fixture' })
      let content = await readFile(file)
      if (extname(file) === '.html') content = Buffer.from(content.toString().replace('<!-- clawdline:strings -->', '').replace('<!-- clawdline:cloud -->', ''))
      response.writeHead(200, { 'content-type': mime[extname(file)] || 'application/octet-stream', 'cache-control': 'no-store' })
      response.end(content)
    } catch (error) {
      json(response, error.code === 'ENOENT' ? 404 : 500, { error: 'fixture_error', detail: String(error) })
    }
  }
  const local = httpServer(handler('app'))
  const hosted = httpsServer({ key: await readFile(keyFile), cert: await readFile(certFile) }, handler('cloud'))
  const reusePort = key => {
    if (!reused) return 0
    const url = new URL(reused[key])
    if (url.hostname !== '127.0.0.1') throw new Error('Refusing non-loopback reused origin')
    return Number(url.port)
  }
  const listen = (server, port) => new Promise((yes, no) => { server.once('error', no); server.listen(port, '127.0.0.1', yes) })
  await listen(local, reusePort('appOrigin'))
  await listen(hosted, reusePort('cloudOrigin'))
  const appOrigin = `http://127.0.0.1:${local.address().port}`
  const cloudOrigin = `https://127.0.0.1:${hosted.address().port}`
  const manifest = { version: 1, synthetic: true, pid: process.pid, repo, candidateCommit, out: servedOut, appOrigin, cloudOrigin,
    roleMetadata: options.builtins ? `${options['builtin-display'] ? 'explicit external public display JSON' : 'candidate public display JSON'} and candidate raw Markdown; synthetic icons, versions, team and role-skill relationships` : providedFixtures ? 'provided-json' : 'authored-sample; not built-in translation acceptance',
    harnessHash: createHash('sha256').update(await readFile(self)).digest('hex'),
    fixtureDigest: createHash('sha256').update(JSON.stringify(fixtureData)).digest('hex'),
    syntheticMutations: ['POST /v1/auth/session', 'DELETE /v1/auth/recovery/devices/<known-synthetic-id>; actor-scoped, at most 256 contexts'],
    states: REVIEW_STATES, locales: REVIEW_LOCALES, createdAt: new Date().toISOString(), catalogHashes: {},
    urls: { sessions: appOrigin + '/', detail: appOrigin + '/#session=review-session', settings: appOrigin + '/#page=settings',
      machineSettings: appOrigin + '/#page=settings', playbook: appOrigin + '/#page=playbook', board: appOrigin + '/#page=work',
      projects: appOrigin + '/#page=projects', notifications: appOrigin + '/#page=notifications', plan: appOrigin + '/#page=plan', usageOverview: appOrigin + '/#page=usage',
      squad: appOrigin + '/#page=squad',
      document: appOrigin + '/#document=1&machine=this-mac&session=review-session&scope=project&path=README.md',
      signIn: cloudOrigin + '/?review_state=signed-out', pairing: cloudOrigin + '/?review_state=pairing', deviceLimit: cloudOrigin + '/?review_state=device-limit',
      admin: cloudOrigin + '/admin?review_state=ready', accounts: cloudOrigin + '/admin/accounts?review_state=ready',
      promotions: cloudOrigin + '/admin/promotions?review_state=ready', usage: cloudOrigin + '/admin/usage?review_state=ready',
      adminForbidden: cloudOrigin + '/admin?review_state=forbidden', adminFailed: cloudOrigin + '/admin?review_state=failed' },
    unsupported: ['Cloud machine selection and shared console: encrypted relay peer not implemented; no production security checks bypassed.',
      'Unknown API routes return review_route_unsupported; inspect /__review/requests and report affected loaded-data surfaces.'] }
  const stop = () => { local.closeAllConnections(); hosted.closeAllConnections(); local.close(); hosted.close() }
  process.once('SIGTERM', () => { stop(); process.exit(0) })
  process.once('SIGINT', () => { stop(); process.exit(0) })
  try {
    if (!reused) await run(join(repo, 'tools/heavy.sh'), [process.execPath, self, '--build', '--repo', repo, '--out', out, '--cloud-origin', cloudOrigin], { env: { ...process.env, HEAVY_HANDOFF: '0' } })
    if (!reused && (execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim() !== candidateCommit || execFileSync('git', ['-C', repo, 'status', '--porcelain'], { encoding: 'utf8' }).trim())) throw new Error('Candidate changed during the build; no review manifest published')
    if (!reused && servedOut !== out) {
      await mkdir(servedOut, { recursive: true })
      for (const kind of ['app', 'cloud']) await cp(join(out, kind), join(servedOut, kind), { recursive: true })
      for (const name of ['loopback-key.pem', 'loopback-cert.pem']) await cp(join(out, name), join(servedOut, name))
    }
    await writeFile(join(servedOut, 'fixture-data.json'), JSON.stringify(fixtureData, null, 2) + '\n')
    for (const tag of REVIEW_LOCALES) {
      const hash = createHash('sha256').update(await readFile(join(servedOut, 'app/catalogs', tag + '.json'))).digest('hex')
      if (reused && hash !== reused.catalogHashes[tag]) throw new Error('Published catalog changed since its build manifest')
      manifest.catalogHashes[tag] = hash
    }
    await writeFile(manifestFile, JSON.stringify(manifest, null, 2) + '\n')
    console.log(JSON.stringify(manifest))
    return { manifest, stop }
  } catch (error) { stop(); throw error }
}

if (process.argv[1] && resolve(process.argv[1]) === self) {
  const args = reviewArgs(process.argv.slice(2))
  if (args.help || (!args.serve && !args.build)) console.log('node localization-review.mjs --repo REPO --serve --out TASK_OUTPUT --manifest TASK_MANIFEST\nStop the exact manifest pid with SIGTERM. Builds acquire tools/heavy.sh; servers do not hold its lease.')
  else if (!args.repo || !args.out) throw new Error('--repo and --out are required')
  else if (args.build) await buildReview({ repo: resolve(args.repo), out: resolve(args.out), cloudOrigin: args['cloud-origin'] })
  else if (!args.manifest) throw new Error('--manifest is required')
  else await serveReview(args)
}
