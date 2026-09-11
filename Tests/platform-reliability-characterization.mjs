#!/usr/bin/env node
// Focused proof of the W0-C measurement boundary. Never compiles Swift, restarts
// the app, reads its registry, or uses a machine/task credential.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const tool = process.env.W0C_TOOL ?? path.join(root, 'tools/measure-platform-reliability.py');
assert.ok(process.argv.slice(2).every(arg => arg === '--offline'), 'unknown test option');
const offline = process.argv.includes('--offline');
const scratch = fs.mkdtempSync(path.join(process.env.TMPDIR ?? os.tmpdir(), 'w0c-test-'));
const env = { ...process.env, PYTHONDONTWRITEBYTECODE: '1' };
const expectedIDs = [
  'cloud-ingress', 'cloud-ready', 'http-admission', 'sse-output', 'sse-reconnect',
  'terminal-queue', 'read-queues', 'coalesced-read-waiters', 'cloud-read-queues',
  'cloud-publication', 'spool-component', 'ledger-component', 'store-read-health',
  'store-overwrite-risk', 'disk-failure-seams', 'sequence-disk', 'restart-contract',
  'restart-liveness', 'filesystem-fixture',
];
let checks = 0;
const failures = [];
const started = performance.now();
const sha = data => createHash('sha256').update(data).digest('hex');

function run(args = [], inputRoot = root) {
  const result = spawnSync('python3', [tool, '--root', inputRoot, ...args],
    { encoding: 'utf8', env, timeout: 15000, maxBuffer: 4 * 1024 * 1024 });
  assert.ifError(result.error);
  return result;
}

function checked(name, body) {
  checks += 1;
  try {
    body();
    console.log(`✓ ${name}`);
  } catch (error) {
    failures.push(name);
    console.error(`✗ ${name}\n${error.stack}`);
  }
}

function failure(result, code) {
  assert.equal(result.status, 2, `expected fail-closed ${code}; stderr=${result.stderr}`);
  assert.equal(result.stdout, '', 'a failed measurement must emit no success artifact');
  const error = JSON.parse(result.stderr);
  assert.equal(error.status, 'error');
  assert.equal(error.code, code);
}

function importProbe(body, input = '') {
  const loader = `import importlib.util, sys\nsys.dont_write_bytecode=True\n` +
    `def forbid_network(event, args):\n` +
    `    if event in ('socket.__new__', 'socket.connect', 'socket.bind'):\n` +
    `        raise RuntimeError('offline probe must not create a socket')\n` +
    `sys.addaudithook(forbid_network)\n` +
    `spec=importlib.util.spec_from_file_location('w0c',sys.argv[1])\n` +
    `m=importlib.util.module_from_spec(spec)\nspec.loader.exec_module(m)\n` + body;
  const result = spawnSync('python3', ['-c', loader, tool, root],
    { encoding: 'utf8', env, input, timeout: 15000 });
  assert.ifError(result.error);
  return result;
}

async function served(handler, args) {
  assert.ok(!offline, '--offline must not start HTTP fixture servers');
  const requests = [];
  const server = http.createServer((request, response) => {
    requests.push({ method: request.method, url: request.url, headers: request.headers });
    handler(request, response, requests.length);
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  try {
    const port = server.address().port;
    const result = await new Promise((resolve, reject) => {
      const child = spawn('python3', [tool, '--root', root, '--health-port', String(port), ...args], { env });
      let stdout = '', stderr = '';
      const timeout = setTimeout(() => { child.kill(); reject(new Error('fixture probe timed out')); }, 15000);
      child.stdout.setEncoding('utf8').on('data', data => { stdout += data; });
      child.stderr.setEncoding('utf8').on('data', data => { stderr += data; });
      child.on('error', reject);
      child.on('close', status => { clearTimeout(timeout); resolve({ status, stdout, stderr }); });
    });
    return { result, requests };
  } finally {
    await new Promise(resolve => server.close(resolve));
  }
}

// Only replace the connection: real HTTPResponse.begin() must parse exact bytes,
// including email parser defects. No socket, normalized headers or mocked parser.
function offlineWire(wire) {
  const probe = importProbe(`
import contextlib, io, json
stream = io.BytesIO(sys.stdin.buffer.read())
parser_defects = []
connections = []
class WireSocket:
    def makefile(self, *args, **kwargs):
        return stream
class Connection:
    def __init__(self, host, port, timeout):
        assert (host, port, timeout) == ('127.0.0.1', 1, 2)
        self.closed = False
        connections.append(self)
    def request(self, method, path, headers):
        assert (method, path, headers) == ('GET', '/v1/health', {'Connection': 'close'})
    def getresponse(self):
        response = m.http.client.HTTPResponse(WireSocket())
        response.begin()
        parser_defects.extend(type(d).__name__ for d in response.headers.defects)
        return response
    def close(self):
        self.closed = True
m.http.client.HTTPConnection = Connection
sys.argv = [sys.argv[1], '--root', sys.argv[2], '--health-port', '1', '--samples', '1']
out, err = io.StringIO(), io.StringIO()
with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
    status = m.main()
assert len(connections) == 1 and connections[0].closed and stream.closed
print(json.dumps({'status': status, 'stdout': out.getvalue(), 'stderr': err.getvalue(),
                  'parser_defects': parser_defects}))
`, wire);
  assert.equal(probe.status, 0, probe.stderr);
  assert.equal(probe.stderr, '');
  return JSON.parse(probe.stdout);
}

try {
  const first = run();
  checked('完整且確定性的非空來源盤點', () => {
    assert.equal(first.status, 0, first.stderr);
    assert.equal(first.stderr, '');
    assert.equal(first.stdout, run().stdout);
  });
  const report = JSON.parse(first.stdout);
  const byID = Object.fromEntries(report.rows.map(row => [row.id, row]));
  checked('精確覆蓋 19 列與 18 個來源，未知沒有變成零', () => {
    assert.deepEqual(report.rows.map(row => row.id), expectedIDs);
    assert.equal(report.row_count, 19);
    assert.equal(report.source_file_count, 18);
    assert.equal(report.source_manifest.length, 18);
    assert.match(report.source_reference_commit,
      /^base:a6e2785ca1993538b7792aab69c9bfc73da854c0\+candidate-overlay:[a-f0-9]{64}$/);
    assert.equal(report.source_overlay, 'R-2 sealed HTTP/SSE reliability correction overlay');
    assert.equal(report.swift_tests_executed, false);
    assert.equal(report.approved_new_budgets, null);
    assert.equal(report.production_readiness, 'not-established');
    for (const row of report.rows) {
      assert.ok(row['source-derived'].length > 0);
      assert.ok(row.unknown.length > 0);
      assert.ok(row.downstream_owner.length > 0);
      assert.deepEqual(row['executable-test-derived'], { status: 'not-run', observation_count: null });
      assert.deepEqual(row['measured-runtime'], { status: 'not-measured', observation_count: null });
    }
  });
  checked('來源摘要對應實際檔案，容量與未接線元件分開', () => {
    for (const entry of report.source_manifest) {
      const bytes = fs.readFileSync(path.join(root, entry.path));
      assert.equal(entry.bytes, bytes.length);
      assert.equal(entry.sha256, sha(bytes));
      assert.match(entry.provenance,
        /^(?:commit|working-overlay):a6e2785ca1993538b7792aab69c9bfc73da854c0$/);
    }
    assert.equal(byID['terminal-queue'].values.terminalDepth.value, 8);
    assert.equal(byID['terminal-queue'].values.terminalChannelDepth.value, 2);
    assert.equal(byID['http-admission'].values.bodyLimit.value, 20 * 1024 * 1024);
    assert.equal(byID['read-queues'].values.voiceDepth.value, 2);
    assert.equal(byID['read-queues'].values.planDepth.value, 2);
    assert.equal(byID['spool-component'].values.globalByteCap.value, 16 * 1024 * 1024);
    assert.equal(byID['http-admission'].values.connectionLimit.value, 128);
    assert.equal(byID['http-admission'].values.connectionRefusalLimit.value, 16);
    assert.equal(byID['http-admission'].values.deadlineTaskLimit.value, 144);
    assert.equal(byID['sse-output'].values.streamLimit.value, 16);
    assert.equal(byID['sse-output'].values.streamByteLimit.value, 4 * 1024 * 1024);
    assert.equal(byID['sse-output'].values.aggregateStreamByteLimit.value, 16 * 1024 * 1024);
    assert.equal(byID['store-read-health'].values.top_level_version_gate, true);
    assert.equal(byID['store-read-health'].values.read_health_fence_in_load, true);
    assert.ok(byID['spool-component'].unknown.some(text => text.includes('production')));
  });

  const fixture = path.join(scratch, 'source');
  for (const { path: relative } of report.source_manifest) {
    const target = path.join(fixture, relative);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(path.join(root, relative), target);
  }
  const altered = path.join(fixture, 'Sources/CloudTransport.swift');
  const original = fs.readFileSync(altered);
  fs.unlinkSync(altered);
  checked('缺少來源 fail closed', () => failure(run([], fixture), 'source_unreadable'));
  fs.writeFileSync(altered, '');
  checked('空來源 fail closed', () => failure(run([], fixture), 'source_empty'));
  fs.writeFileSync(altered, Buffer.from([0xff, 0xfe]));
  checked('來源 UTF-8 解析失敗 fail closed', () => failure(run([], fixture), 'source_unreadable'));
  // Keep every anchor intact. A lexical matcher alone would miss the altered file.
  fs.writeFileSync(altered, Buffer.concat([original, Buffer.from('\nTHIS IS NOT VALID SWIFT\n')]));
  checked('完整 anchor 仍在時，來源漂移不可默默通過', () => failure(run([], fixture), 'source_drift'));
  fs.writeFileSync(altered, original);

  checked('空來源集合與空 runtime observations fail closed', () => {
    const result = importProbe(`
for action in [lambda: m.validate_observations([]), lambda: m.SourceEvidence(m.Path(sys.argv[2]))]:
    m.SOURCE_SEALS = {}
    try: action()
    except m.MeasurementError as error:
        assert error.code == 'zero_observations'
    else: raise AssertionError('empty evidence passed')
print('2 rejected')
`);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout.trim(), '2 rejected');
  });
  checked('來源定位或常數解析失敗不輸出替代值', () => {
    const result = importProbe(`
s=m.SourceEvidence(m.Path(sys.argv[2]))
for text in ['static let cap = unknown()', 'static let cap = (', 'static let cap = 0']:
    s.text['fixture']=text
    try: s.integer('fixture','cap')
    except m.MeasurementError as e: assert e.code == 'constant_parse_failed'
    else: raise AssertionError('invalid constant passed')
for text in ['', 'anchor anchor']:
    s.text['fixture']=text
    try: s.ref('fixture','anchor')
    except m.MeasurementError as e: assert e.code == 'source_shape_changed'
    else: raise AssertionError('missing/ambiguous anchor passed')
print('5 rejected')
`);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout.trim(), '5 rejected');
  });
  checked('Markdown freshness guard 拒絕陳舊文件或缺檔', () => {
    const file = path.join(scratch, 'baseline.md');
    const rendered = run(['--format', 'markdown']);
    assert.equal(rendered.status, 0, rendered.stderr);
    fs.writeFileSync(file, rendered.stdout);
    const good = run(['--check', file]);
    assert.equal(good.status, 0, good.stderr);
    assert.deepEqual(JSON.parse(good.stdout), { status: 'pass', row_count: 19, source_file_count: 18 });
    fs.appendFileSync(file, '\nstale\n');
    failure(run(['--check', file]), 'baseline_stale');
    fs.unlinkSync(file);
    failure(run(['--check', file]), 'input_unreadable');
  });
  checked('五個檔案 fixture 保留非空原件且清除私人 scratch', () => {
    const folder = path.join(scratch, 'disk');
    fs.mkdirSync(folder);
    const result = run(['--scratch', folder]);
    assert.equal(result.status, 0, result.stderr);
    const observed = JSON.parse(result.stdout).rows.find(r => r.id === 'filesystem-fixture');
    assert.equal(observed['executable-test-derived'].observation_count, 5);
    assert.ok(observed['executable-test-derived'].observations.every(r => r.preserved === true));
    assert.equal(observed['measured-runtime'].status, 'not-measured');
    assert.deepEqual(fs.readdirSync(folder), []);
    failure(run(['--scratch', path.join(scratch, 'absent')]), 'scratch_unavailable');
  });
  checked('不允許零樣本、隱含 runtime 或 runtime 對靜態文件比對', () => {
    failure(run(['--health-port', '1', '--samples', '0']), 'invalid_probe');
    failure(run(['--health-port', '0']), 'invalid_probe');
    failure(run(['--samples', '1']), 'invalid_probe');
    failure(run(['--check', 'absent', '--health-port', '1']), 'invalid_probe');
  });

  const stampedVersion = fs.readFileSync(path.join(root, 'build.sh'), 'utf8')
    .match(/CFBundleShortVersionString<\/key><string>([^<]+)<\/string>/)?.[1];
  assert.match(stampedVersion, /^\d+\.\d+\.\d+$/, 'fixture version must derive from the bundle stamp');
  const health = { ok: true, build: 123, protocol: 1, version: stampedVersion, instance: '11111111-2222-4333-8444-555555555555' };
  // Keep the existing live fixture checks in the default run; --offline reports
  // their explicit omission and still runs all framing, source and F2 checks.
  if (!offline) {
    const goodHealth = await served((req, res) => res.end(JSON.stringify({ ...health, ignored_secret: 'never-copy-this' })), ['--samples', '2']);
    checked('health 只量同一程序兩次 GET，無 credential，無任意欄位外洩', () => {
      assert.equal(goodHealth.result.status, 0, goodHealth.result.stderr);
      const result = JSON.parse(goodHealth.result.stdout).rows.find(r => r.id === 'restart-liveness')['measured-runtime'];
      assert.equal(result.observation_count, 2);
      assert.equal(result.source_build_relation, 'unverified');
      assert.ok(result.observations.every(row => row.instance === health.instance && row.duration_ms >= 0));
      assert.equal(goodHealth.requests.length, 2);
      for (const request of goodHealth.requests) {
        assert.equal(request.method, 'GET');
        assert.equal(request.url, '/v1/health');
        assert.equal(request.headers.authorization, undefined);
        assert.equal(request.headers['x-clawdline-orchestrator'], undefined);
        assert.equal(request.headers['x-clawdline-task-secret'], undefined);
      }
      assert.ok(!goodHealth.result.stdout.includes('never-copy-this'));
    });
    for (const [name, body, code] of [
      ['空 health', '', 'health_payload_invalid'],
      ['malformed JSON', '{', 'json_parse_failed'],
      ['duplicate JSON key', '{"ok":true,"ok":true}', 'json_parse_failed'],
      ['非有限 JSON number', '{"x":NaN}', 'json_parse_failed'],
      ['假成功 health', '{"ok":false}', 'health_payload_invalid'],
      ['缺少 build 身分', JSON.stringify({ ...health, build: undefined }), 'health_identity_invalid'],
      ['非 UUID instance', JSON.stringify({ ...health, instance: 'bad' }), 'health_identity_invalid'],
      ['過大 health', 'x'.repeat(16385), 'health_payload_invalid'],
    ]) {
      const fixture = await served((req, res) => res.end(body), ['--samples', '1']);
      checked(`${name} fail closed`, () => failure(fixture.result, code));
    }
    const redirect = await served((req, res) => { res.writeHead(302, { Location: 'http://example.invalid/' }); res.end(); }, ['--samples', '1']);
    checked('拒絕 redirect，不離開 loopback health', () => failure(redirect.result, 'health_http_failed'));
    const changed = await served((req, res, n) => res.end(JSON.stringify({ ...health, build: n })), ['--samples', '2']);
    checked('不同程序／build 的樣本不可混成一份量測', () => failure(changed.result, 'health_identity_changed'));
  }

  const payload = JSON.stringify(health);
  const capped = payload + ' '.repeat(16384 - Buffer.byteLength(payload));
  const wire = (headers, body) => `HTTP/1.1 200 OK\r\nConnection: close\r\n${headers}\r\n${body}`;
  const chunk = body => `${Buffer.byteLength(body).toString(16)}\r\n${body}\r\n`;
  // Exact confirmation payload, so these two full wire digests stay reproducible.
  const defectPayload = '{"ok":true,"build":7,"protocol":1,"version":"1.0","instance":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}';
  const framingCases = [
    ['content-length-complete', `Content-Length: ${payload.length}\r\n`, payload, null, payload.length],
    ['content-length-at-cap', 'Content-Length: 16384\r\n', capped, null, 16384],
    ['content-length-truncated', `Content-Length: ${payload.length + 1}\r\n`, payload, 'health_response_incomplete'],
    ['content-length-declared-over-cap', 'Content-Length: 20000\r\n', payload, 'health_payload_invalid'],
    ['unknown-header-before-content-length', `X-Probe: fine\r\nContent-Length: ${payload.length}\r\n`, payload, null, payload.length],
    ['unknown-header-before-over-cap-content-length', 'X-Probe: fine\r\nContent-Length: 20000\r\n', payload, 'health_payload_invalid'],
    ['invalid-header-before-cl', 'Invalid-Header\r\nContent-Length: 20000\r\n', defectPayload, 'health_framing_invalid'],
    ['invalid-cl-field-name', 'Content-Length : 20000\r\n', defectPayload, 'health_framing_invalid'],
    ['content-length-huge', `Content-Length: ${'9'.repeat(5000)}\r\n`, payload, 'health_payload_invalid'],
    ['content-length-invalid', 'Content-Length: nope\r\n', payload, 'health_framing_invalid'],
    ['content-length-negative', 'Content-Length: -1\r\n', payload, 'health_framing_invalid'],
    ['content-length-duplicate', `Content-Length: ${payload.length}\r\nContent-Length: ${payload.length + 1}\r\n`, payload, 'health_framing_invalid'],
    ['chunked-complete', 'Transfer-Encoding: chunked\r\n', chunk(payload.slice(0, 11)) + chunk(payload.slice(11)) + '0\r\n\r\n', null, payload.length],
    ['chunked-at-cap', 'Transfer-Encoding: chunked\r\n', chunk(capped) + '0\r\n\r\n', null, 16384],
    ['chunked-extensions-trailers', 'Transfer-Encoding: chunked\r\n', `${payload.length.toString(16)};fixture=yes\r\n${payload}\r\n0\r\nX-Fixture: done\r\n\r\n`, null, payload.length],
    ['chunked-truncated-data', 'Transfer-Encoding: chunked\r\n', `${(payload.length + 1).toString(16)}\r\n${payload}`, 'health_response_incomplete'],
    ['chunked-missing-zero', 'Transfer-Encoding: chunked\r\n', chunk(payload), 'health_response_incomplete'],
    ['chunked-missing-final-crlf', 'Transfer-Encoding: chunked\r\n', chunk(payload) + '0\r\n', 'health_response_incomplete'],
    ['chunked-truncated-trailer', 'Transfer-Encoding: chunked\r\n', chunk(payload) + '0\r\nX-Fixture: done\r\n', 'health_response_incomplete'],
    ['chunked-invalid-data-crlf', 'Transfer-Encoding: chunked\r\n', `${payload.length.toString(16)}\r\n${payload}xx0\r\n\r\n`, 'health_framing_invalid'],
    ['chunked-invalid-size', 'Transfer-Encoding: chunked\r\n', `+${payload.length.toString(16)}\r\n${payload}\r\n0\r\n\r\n`, 'health_framing_invalid'],
    ['chunked-aggregate-over-cap', 'Transfer-Encoding: chunked\r\n', chunk(capped) + chunk(' ') + '0\r\n\r\n', 'health_payload_invalid'],
    ['chunked-declared-over-cap', 'Transfer-Encoding: chunked\r\n', '5000\r\n' + payload, 'health_payload_invalid'],
    ['chunked-metadata-over-cap', 'Transfer-Encoding: chunked\r\n', chunk(payload) + '0\r\nX-Fixture: ' + 'x'.repeat(16384) + '\r\n\r\n', 'health_framing_invalid'],
    ['chunked-many-small-chunks-over-metadata-cap', 'Transfer-Encoding: chunked\r\n',
      [...(payload + ' '.repeat(4000 - payload.length))].map(chunk).join('') + '0\r\n\r\n', 'health_framing_invalid'],
    ['conflicting-framing', `Transfer-Encoding: chunked\r\nContent-Length: ${payload.length}\r\n`, chunk(payload) + '0\r\n\r\n', 'health_framing_invalid'],
    ['unsupported-transfer-coding', 'Transfer-Encoding: gzip\r\n', payload, 'health_framing_invalid'],
    ['close-delimited-complete', '', payload, null, payload.length],
    ['close-delimited-at-cap', '', capped, null, 16384],
    ['close-delimited-over-cap', '', capped + ' ', 'health_payload_invalid'],
  ];
  const framingResults = [];
  for (const [name, headers, body, code, bytes] of framingCases) {
    const raw = wire(headers, body);
    const result = offlineWire(raw);
    const evidence = { wire_sha256: sha(raw), parser_defects: result.parser_defects,
      exit_status: result.status, stdout_bytes: Buffer.byteLength(result.stdout), stderr: result.stderr };
    try {
      if (code) failure(result, code);
      else {
        assert.equal(result.status, 0, result.stderr);
        assert.equal(result.stderr, '');
        const observed = JSON.parse(result.stdout).rows.find(r => r.id === 'restart-liveness')['measured-runtime'];
        assert.equal(observed.status, 'observed');
        assert.equal(observed.observation_count, 1);
        assert.equal(observed.observations.length, 1);
        assert.equal(observed.observations[0].response_bytes, bytes);
        assert.equal(observed.observations[0].instance, health.instance);
      }
      framingResults.push({ name, passed: true, ...evidence });
    } catch (error) {
      framingResults.push({ name, passed: false, detail: error.message, ...evidence });
    }
  }
  checked('HTTP framing 完整性與 payload cap：30 個離線 wire 案例', () => {
    assert.equal(framingResults.length, 30);
    assert.deepEqual(framingResults.filter(row => !row.passed), []);
  });
  checked('sequence-disk 限定正常 reserve 嘗試並保留來源風險與 runtime 未知', () => {
    const row = byID['sequence-disk'];
    assert.match(row.finding, /正常 reserve 分支嘗試先 persist ceiling/);
    assert.match(row.finding, /reserved\[sender\] 先於 try persist\(\) 更新/);
    assert.match(row.finding, /persist 失敗不還原 reserved 且 next 未前進/);
    assert.match(row.finding, /同一物件重試可略過 persist 並發出序號/);
    assert.match(row.finding, /若失敗未替換磁碟舊 ceiling，重啟後可能重用已發序號/);
    assert.ok(!row.finding.includes('nextSequence 先 persist ceiling 才發序號'));
    assert.ok(row.unknown.some(text => text.includes('實際序號重用或資料損失未觀測') && text.includes('未執行 Swift failure injection')));
    assert.equal(row.downstream_owner, 'W5-1 durability owner / security reviewer');
    assert.deepEqual(row['executable-test-derived'], { status: 'not-run', observation_count: null });
    assert.deepEqual(row['measured-runtime'], { status: 'not-measured', observation_count: null });
    // Bind the claim to the sealed control flow and the retained production instance.
    const slices = row['source-derived'].map(ref => {
      const source = fs.readFileSync(path.join(root, ref.path), 'utf8');
      return source.split('\n').slice(ref.line - 1, ref.end_line).join('\n');
    });
    for (const anchor of ['reserved[sender] = ceiling\n            try persist()',
      'next[sender] = value &+ 1', 'reserved = restored', 'try body.write(to: url, options: [.atomic])',
      'sequencing: { _ in sequenceFile }', 'let sequence = try await sequencing.nextSequence(sender: identity.deviceID)']) {
      assert.ok(slices.some(text => text.includes(anchor)), `missing source evidence: ${anchor}`);
    }
  });

  assert.equal(checks, offline ? 14 : 25, 'an omitted check must not produce a green receipt');
  console.log(JSON.stringify({ suite: 'platform-reliability-characterization', checks, failures: failures.length,
    failed_checks: failures, framing_cases: framingResults,
    seconds: Number(((performance.now() - started) / 1000).toFixed(3)),
    source_scope_sha256: report.source_scope_sha256, tool_sha256: sha(fs.readFileSync(tool)),
    test_sha256: sha(fs.readFileSync(fileURLToPath(import.meta.url))),
    skipped_network_checks: offline ? 11 : 0,
    scope: offline ? 'offline HTTPResponse framing, source guards and private filesystem fixtures; 11 network checks omitted; no Swift/runtime acceptance'
      : 'measurement guard, offline HTTPResponse framing, local HTTP fixtures and private filesystem fixtures; no Swift/runtime acceptance' }));
  if (failures.length) process.exitCode = 1;
} finally {
  fs.rmSync(scratch, { recursive: true, force: true });
}
