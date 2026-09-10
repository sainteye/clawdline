// Standalone public-clone proof. No private paths, network, npm modules or Swift compilation.
import assert from 'node:assert/strict';
import { createCipheriv, createDecipheriv, createHash, createHmac, createPrivateKey,
  createPublicKey, diffieHellman, sign, verify } from 'node:crypto';
import { cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync,
  rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const options = new Map();
for (let i = 2; i < process.argv.length; i += 2) {
  assert.ok(['--tool', '--package', '--case'].includes(process.argv[i]), 'unknown test option');
  assert.ok(process.argv[i + 1], 'missing test option value');
  options.set(process.argv[i], process.argv[i + 1]);
}
const tool = resolve(options.get('--tool') ?? join(root, 'tools/cloud-contract-v1.py'));
const pkg = resolve(options.get('--package') ?? join(root, 'Contracts/Cloud/v1'));
const selected = options.get('--case');
const scratch = mkdtempSync(join(tmpdir(), 'cloud-contract-v1-'));
const expectedSource = '47c21a3d096813f940026943004791087ea89d628f6123942dd59c02f71a2f3d';
const expectedPackage = '6ccccea5f05b603fd9a583940f6f7a3fd5a8735ac6d5ce6ef7246620b59b7d6f';
const bytes = path => readFileSync(path);
const json = path => JSON.parse(bytes(path));
const hash = data => createHash('sha256').update(data).digest('hex');
const b64 = text => Buffer.from(text, 'base64');
const canonical = value => {
  if (value === null || typeof value === 'boolean' || typeof value === 'string') return JSON.stringify(value);
  if (typeof value === 'number') {
    assert.ok(Number.isSafeInteger(value), 'canonical numbers are safe integers');
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  return '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}';
};
let checks = 0;
let failures = 0;
const cases = [];
function test(name, callback) { cases.push({ name, callback }); }
function run(args, { script = tool, cwd = scratch } = {}) {
  const result = spawnSync('python3', [script, ...args], {
    cwd, encoding: 'utf8', maxBuffer: 2 * 1024 * 1024, timeout: 20_000,
    env: { ...process.env, PYTHONDONTWRITEBYTECODE: '1' },
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null, 'CLI must not time out or crash');
  return result;
}
function success(result) {
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stderr, '');
  const resultJSON = JSON.parse(result.stdout);
  assert.equal(resultJSON.ok, true);
  return resultJSON;
}
function refusal(result, code) {
  assert.equal(result.status, 2, 'must return typed failure, not a successful or crashed process');
  assert.equal(result.stdout, '', 'a failed operation must not emit partial success');
  const resultJSON = JSON.parse(result.stderr);
  assert.equal(resultJSON.ok, false);
  if (code) assert.equal(resultJSON.code, 'cloud_contract_' + code);
  else assert.match(resultJSON.code, /^cloud_contract_/);
}
let ordinal = 0;
function clone() {
  const path = join(scratch, 'package-' + ordinal++);
  cpSync(pkg, path, { recursive: true });
  return path;
}
function input(data) {
  const path = join(scratch, 'input-' + ordinal++);
  writeFileSync(path, data);
  return path;
}
function validate(path, kind = 'envelope', extra = []) {
  return run(['validate', '--package', pkg, '--kind', kind, ...extra, path]);
}
function rawManifest(data) {
  const dir = join(scratch, 'manifest-' + ordinal++);
  mkdirSync(dir);
  writeFileSync(join(dir, 'manifest.json'), data);
  writeFileSync(join(dir, 'source.sha256'), hash(data) + '\n');
  return join(dir, 'manifest.json');
}
function generateManifest(data, code) {
  const out = join(scratch, 'candidate-' + ordinal++);
  refusal(run(['generate', '--manifest', rawManifest(data), '--output', out]), code);
  assert.equal(existsSync(out), false, 'invalid input must not publish a partial directory');
  assert.equal(readdirSync(scratch).some(name => name.startsWith('.cloud-contract-')), false);
}
function fileSet(path, prefix = '') {
  return readdirSync(path).sort().flatMap(name => {
    const relative = prefix + name;
    return lstatSync(join(path, name)).isDirectory() ? fileSet(join(path, name), relative + '/') : [relative];
  });
}
function signing(envelope) {
  return Buffer.from(['v', 'ch', 'seq', 'ts', 'class', 'key_id', 'nonce', 'ct'].map(key => String(envelope[key])).join('|'));
}
function privateKey(raw, oid = '70') {
  return createPrivateKey({ key: Buffer.concat([Buffer.from('302e020100300506032b65' + oid + '04220420', 'hex'), raw]), format: 'der', type: 'pkcs8' });
}
function publicKey(raw, oid = '70') {
  return createPublicKey({ key: Buffer.concat([Buffer.from('302a300506032b65' + oid + '032100', 'hex'), raw]), format: 'der', type: 'spki' });
}
function seal(key, nonce, plaintext, aad = Buffer.alloc(0)) {
  const cipher = createCipheriv('aes-256-gcm', key, nonce);
  cipher.setAAD(aad);
  return Buffer.concat([cipher.update(plaintext), cipher.final(), cipher.getAuthTag()]);
}
function open(key, nonce, ciphertext, aad = Buffer.alloc(0)) {
  const cipher = createDecipheriv('aes-256-gcm', key, nonce);
  cipher.setAAD(aad);
  cipher.setAuthTag(ciphertext.subarray(-16));
  return Buffer.concat([cipher.update(ciphertext.subarray(0, -16)), cipher.final()]);
}

// Supplemental ct validation for a JSON Schema consumer. Buffer's decoder alone is permissive.
function canonicalCiphertext(value, schema, minimum) {
  return typeof value === 'string' && value.length >= schema.minLength && value.length <= schema.maxLength
    && new RegExp(schema.pattern).test(value) && b64(value).toString('base64') === value
    && b64(value).length >= minimum && b64(value).length <= 25_162_752;
}

// Executable candidate KDF oracle, shared by the positive and injected-negative checks.
// This is not an invocation of the deployed Swift/PWA implementation.
function pairingPhaseKey(shared, offer) {
  if (shared.length !== 32 || shared.every(byte => byte === 0)) throw new Error('invalid_key_agreement');
  const lp = raw => { const prefix = Buffer.alloc(2); prefix.writeUInt16BE(raw.length); return Buffer.concat([prefix, raw]); };
  const salt = createHash('sha256').update(Buffer.concat([
    lp(Buffer.from('clawdline-pair-salt-v1')), lp(b64(offer.pairing_nonce)),
    lp(Buffer.from(offer.pairing_id)), lp(b64(offer.claim_nonce)),
  ])).digest();
  const prk = createHmac('sha256', salt).update(shared).digest();
  return createHmac('sha256', prk).update(Buffer.concat([
    lp(Buffer.from('clawdline-pair-v1')), lp(Buffer.from('grant')), Buffer.from([1]),
  ])).digest();
}

test('package-pins', () => {
  assert.equal(hash(bytes(join(pkg, 'manifest.json'))), expectedSource);
  assert.equal(bytes(join(pkg, 'source.sha256')).toString(), expectedSource + '\n');
  assert.equal(hash(bytes(join(pkg, 'integrity.json'))), expectedPackage);
  assert.equal(bytes(join(pkg, 'package.sha256')).toString(), expectedPackage + '\n');
  const inventory = json(join(pkg, 'integrity.json'));
  assert.equal(inventory.files.length, 101);
  assert.deepEqual(fileSet(pkg), [...inventory.files.map(row => row.path), 'integrity.json', 'package.sha256'].sort());
  for (const row of inventory.files) {
    assert.equal(bytes(join(pkg, row.path)).length, row.bytes, row.path);
    assert.equal(hash(bytes(join(pkg, row.path))), row.sha256, row.path);
  }
  const checked = success(run(['check', '--package', pkg, '--expected-source-digest', expectedSource]));
  assert.equal(checked.files, 103);
  assert.equal(checked.valid_fixtures, 16);
  assert.equal(checked.invalid_fixtures, 64);
});

test('generation-and-public-clone', () => {
  const out = join(scratch, 'generated');
  success(run(['generate', '--manifest', join(pkg, 'manifest.json'), '--output', out,
    '--expected-source-digest', expectedSource]));
  assert.deepEqual(fileSet(out), fileSet(pkg));
  for (const path of fileSet(pkg)) assert.deepEqual(bytes(join(out, path)), bytes(join(pkg, path)), path);
  // The whole runtime input is one package plus the Python file, copied away from any checkout.
  const standalone = join(scratch, 'standalone');
  mkdirSync(join(standalone, 'tools'), { recursive: true });
  mkdirSync(join(standalone, 'Contracts/Cloud'), { recursive: true });
  cpSync(tool, join(standalone, 'tools/cloud-contract-v1.py'));
  cpSync(out, join(standalone, 'Contracts/Cloud/v1'), { recursive: true });
  const checked = success(run(['check'], { script: join(standalone, 'tools/cloud-contract-v1.py'), cwd: standalone }));
  assert.equal(checked.source_sha256, expectedSource);
  assert.equal(checked.package_sha256, expectedPackage);
  refusal(run(['generate', '--manifest', join(pkg, 'manifest.json'), '--output', out]), 'output_exists');
  assert.equal(hash(bytes(join(out, 'manifest.json'))), expectedSource, 'refused overwrite preserves candidate');
});

// Register each fixture as a counted case. The count cannot be satisfied by an empty corpus.
const fixtureIndex = json(join(pkg, 'fixtures/index.json'));
assert.equal(fixtureIndex.fixtures.length, 80, 'fixture corpus must be nonempty and complete');
for (const fixture of fixtureIndex.fixtures) {
  test('fixture-' + fixture.name, () => {
    const raw = bytes(join(pkg, fixture.path));
    assert.equal(hash(raw), fixture.sha256);
    assert.equal(raw.length, fixture.bytes);
    const result = validate(join(pkg, fixture.path), fixture.kind);
    if (fixture.valid) {
      const checked = success(result);
      assert.equal(checked.validated, 1);
      assert.equal(checked.scope, 'structure_only');
      if (fixture.signing_path) {
        const signed = signing(JSON.parse(raw));
        assert.deepEqual(bytes(join(pkg, fixture.signing_path)), signed);
        assert.equal(hash(signed), fixture.signing_sha256);
      }
    } else refusal(result, fixture.error);
  });
}

test('signatures-encryption-and-tamper', () => {
  const vectors = json(join(pkg, 'vectors.json'));
  const key = privateKey(b64(vectors.ed25519_seed));
  const publicSigning = publicKey(b64(vectors.ed25519_public_key));
  const wrongKey = createPublicKey(privateKey(Buffer.alloc(32, 99)));
  const pinned = new Map([['device-vector-01', publicSigning]]);
  assert.deepEqual(createPublicKey(key).export({ format: 'der', type: 'spki' }).subarray(-32), b64(vectors.ed25519_public_key));
  const fields = ['ch', 'class', 'ct', 'key_id', 'nonce', 'seq', 'ts', 'v'];
  for (const vector of vectors.envelopes) {
    const envelope = vector.envelope;
    const message = signing(envelope);
    assert.deepEqual(sign(null, message, key), b64(envelope.sig), vector.name);
    assert.equal(verify(null, message, pinned.get(envelope.sender), b64(envelope.sig)), true);
    assert.equal(verify(null, message, wrongKey, b64(envelope.sig)), false);
    assert.equal(pinned.has(envelope.sender + '-unknown'), false, 'sender selects a pinned key; it is not in signing bytes');
    assert.deepEqual(seal(b64(vectors.master_secret), b64(envelope.nonce), b64(vector.plaintext)), b64(envelope.ct));
    assert.deepEqual(open(b64(vectors.master_secret), b64(envelope.nonce), b64(envelope.ct)), b64(vector.plaintext));
    for (const field of fields) {
      const changed = { ...envelope, [field]: typeof envelope[field] === 'number' ? envelope[field] - 1 : envelope[field] + 'x' };
      assert.equal(verify(null, signing(changed), publicSigning, b64(envelope.sig)), false, `${vector.name}/${field}`);
    }
    assert.equal(verify(null, Buffer.concat([message, Buffer.from('\n')]), publicSigning, b64(envelope.sig)), false);
    const changedCT = b64(envelope.ct);
    changedCT[0] ^= 1;
    assert.throws(() => open(b64(vectors.master_secret), b64(envelope.nonce), changedCT));
  }
});

test('control-response-key-separation', () => {
  const vectors = json(join(pkg, 'vectors.json'));
  const response = vectors.control_response;
  const requestEnvelope = JSON.parse(response.request_envelope.body);
  const responseEnvelope = JSON.parse(response.response_envelope.body);
  const key = publicKey(b64(vectors.ed25519_public_key));
  for (const envelope of [requestEnvelope, responseEnvelope]) {
    assert.equal(Object.keys(envelope).length, 10);
    assert.equal(verify(null, signing(envelope), key, b64(envelope.sig)), true);
  }
  const request = JSON.parse(open(b64(vectors.master_secret), b64(requestEnvelope.nonce), b64(requestEnvelope.ct)));
  assert.equal(request.reply.key, response.reply_key.key);
  assert.equal(request.reply.key_id, responseEnvelope.key_id);
  assert.match(responseEnvelope.key_id, /^rk-[A-Za-z0-9_-]{22}$/);
  assert.equal(responseEnvelope.ch, `ctlr/mac-01/${request.reply.device}`);
  assert.throws(() => open(b64(vectors.master_secret), b64(responseEnvelope.nonce), b64(responseEnvelope.ct)));
  const clear = open(b64(request.reply.key), b64(responseEnvelope.nonce), b64(responseEnvelope.ct));
  assert.deepEqual(clear, Buffer.from(response.response_payload.body));
  const effect = JSON.parse(clear);
  assert.equal(Object.keys(effect).length, 9);
  assert.equal(effect.type, 'execution_response');
  assert.equal(effect.request_id, request.request_id);
  assert.equal(effect.request_seq, requestEnvelope.seq);
  assert.equal(effect.result.accepted, true, 'this fixture proves admission only for the named command');
  for (const part of [response.request_envelope, response.response_envelope, response.response_payload]) {
    assert.equal(Buffer.byteLength(part.body), part.byte_length);
    assert.equal(hash(Buffer.from(part.body)), part.sha256);
    assert.equal(canonical(JSON.parse(part.body)), part.body);
  }
});

test('pairing-x25519-hkdf-aad', () => {
  const vector = json(join(pkg, 'vectors.json')).pairing_handover;
  const offer = JSON.parse(vector.offer);
  const handover = JSON.parse(vector.handover);
  const wrapper = vector.wrapper;
  assert.equal(Object.keys(offer).length, 11);
  assert.equal(Object.keys(handover).length, 8);
  assert.equal(Object.keys(wrapper).length, 7);
  assert.equal(canonical(offer), vector.offer);
  assert.equal(canonical(handover), vector.handover);
  assert.equal(Buffer.from(vector.offer).toString('base64url'), vector.offer_fragment);
  const viewerKey = privateKey(b64(vector.viewer_ephemeral_private_key), '6e');
  const machineKey = privateKey(b64(vector.machine_ephemeral_private_key), '6e');
  const viewerPublic = createPublicKey(viewerKey);
  const machinePublic = createPublicKey(machineKey);
  assert.deepEqual(viewerPublic.export({ format: 'der', type: 'spki' }).subarray(-32), b64(offer.viewer_ephemeral_key));
  assert.deepEqual(machinePublic.export({ format: 'der', type: 'spki' }).subarray(-32), b64(wrapper.ephemeral_key));
  const shared = diffieHellman({ privateKey: viewerKey, publicKey: machinePublic });
  assert.deepEqual(shared, diffieHellman({ privateKey: machineKey, publicKey: viewerPublic }));
  const phaseKey = pairingPhaseKey(shared, offer);
  assert.deepEqual(phaseKey, b64(vector.phase_key));
  const aad = Buffer.from(canonical({ v: wrapper.v, phase: wrapper.phase, pairing_id: wrapper.pairing_id,
    sender_device_id: wrapper.sender_device_id, ephemeral_key: wrapper.ephemeral_key }));
  assert.deepEqual(aad, Buffer.from(vector.aad));
  assert.deepEqual(seal(phaseKey, b64(wrapper.nonce), Buffer.from(vector.handover), aad), b64(wrapper.ct));
  assert.deepEqual(open(phaseKey, b64(wrapper.nonce), b64(wrapper.ct), aad), Buffer.from(vector.handover));
  assert.throws(() => open(phaseKey, b64(wrapper.nonce), b64(wrapper.ct), Buffer.from(vector.aad + ' ')));
  assert.throws(() => open(Buffer.alloc(32), b64(wrapper.nonce), b64(wrapper.ct), aad));
  assert.equal(wrapper.sender_device_id, vector.sender_device_id);
  assert.equal(handover.machine_id, wrapper.sender_device_id);
  assert.equal(offer.account_id, handover.account_id);
});

test('receipt-bytes-and-evidence-domains', () => {
  const vectors = json(join(pkg, 'vectors.json'));
  assert.deepEqual(vectors.receipts.map(row => [row.name, row.byte_length]), [
    ['delivered', 279], ['rotation-expired', 213], ['rotation-manual-revoke', 219],
    ['rotation-start-retry-cleanup', 214], ['ordinary-expired', 205],
  ]);
  for (const receipt of vectors.receipts) {
    assert.equal(Buffer.byteLength(receipt.body), receipt.byte_length);
    assert.equal(hash(Buffer.from(receipt.body)), receipt.sha256);
    assert.deepEqual(receipt.headers, { 'X-Clawdline-Receipt-SHA256': receipt.sha256 });
    assert.equal(canonical(JSON.parse(receipt.body)), receipt.body);
  }
  const vocabulary = json(join(pkg, 'vocabularies.json'));
  assert.deepEqual(Object.keys(vocabulary.evidence).sort(), [
    'command_effect', 'human_observation', 'machine_durable_acceptance', 'pairing_finalization', 'relay_fanout', 'webhook_ingress',
  ]);
  assert.equal(vocabulary.evidence.relay_fanout.signal, 'delivered');
  assert.match(vocabulary.evidence.relay_fanout.does_not_prove, /execution|command effect/);
  assert.equal(vocabulary.evidence.command_effect.signal, 'execution_response');
  assert.equal(vocabulary.evidence.machine_durable_acceptance.signal, 'mac_durable_accepted');
  assert.equal(vocabulary.evidence.human_observation.signal, 'human_observed');
});

test('channel-class-and-schema-regexes', () => {
  const envelope = json(join(pkg, 'schemas/envelope.schema.json'));
  const sample = json(join(pkg, 'fixtures/valid/stream-empty.json'));
  assert.equal(envelope.additionalProperties, false);
  assert.deepEqual(envelope.required.slice().sort(), Object.keys(sample).sort());
  const channels = { s: ['s/m/%3', ['stream']], t: ['t/m/$1', ['stream']], orch: ['orch/m', ['stream']],
    ctl: ['ctl/m', ['ctl', 'dispatch']], ctlr: ['ctlr/m/v', ['ctl']], ho: ['ho/a/h', ['ho']] };
  for (const [prefix, [channel, classes]] of Object.entries(channels)) {
    for (const cls of ['stream', 'ctl', 'dispatch', 'ho']) {
      const value = { ...sample, ch: channel, class: cls, key_id: prefix === 'ctlr' ? 'rk-zF3jN8rQ4Wm2pV6sT0uYxA' : 'ms-1' };
      const matching = envelope.oneOf.filter(branch => new RegExp(branch.properties.ch.pattern).test(value.ch)
        && branch.properties.class.enum.includes(value.class));
      assert.equal(matching.length, classes.includes(cls) ? 1 : 0, `${prefix}/${cls}`);
      const result = validate(input(canonical(value)));
      if (classes.includes(cls)) success(result); else refusal(result, 'schema_mismatch');
    }
  }
  const noncePattern = new RegExp(envelope.properties.nonce.pattern);
  for (let length = 0; length < 26; length++) assert.equal(noncePattern.test(Buffer.alloc(length).toString('base64')), length === 12);
  for (const field of ['nonce', 'sig', 'sender', 'key_id']) {
    const regex = new RegExp(envelope.properties[field].pattern);
    assert.equal(regex.test(sample[field] + '\n'), false, 'ECMAScript $ must not accept final newline');
  }
  for (const spelling of ['AB==', '_w==', 'AA=', 'A===']) {
    assert.equal(canonicalCiphertext(spelling, envelope.properties.ct, 1), false);
  }
});

test('canonical-json-and-signing-separation', () => {
  const value = { '\ue000': 'bmp', '\ud800\udc00': 'astral', 'line\n': '\b\t\n\f\r\u0001/\\"會話', n: -0,
    min: -9007199254740991, max: 9007199254740991, list: [null, true, false, 0] };
  const expression = 'import runpy,sys; m=runpy.run_path(sys.argv[1]); b=bytes.fromhex(sys.argv[2]); print(m["canonical"](m["strict_json"](b)).hex())';
  const result = spawnSync('python3', ['-B', '-c', expression, tool, Buffer.from(JSON.stringify(value)).toString('hex')], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout.trim(), Buffer.from(canonical(value)).toString('hex'));
  const sample = json(join(pkg, 'fixtures/valid/stream-empty.json'));
  const alteredSpelling = JSON.stringify(sample, null, 2).replaceAll('/', '\\/');
  success(validate(input(alteredSpelling)));
  refusal(validate(input(alteredSpelling), 'envelope', ['--canonical']), 'noncanonical_input');
  assert.deepEqual(signing(JSON.parse(alteredSpelling)), signing(sample));
});

test('package-corruption-fails-closed', () => {
  for (const mutation of ['missing', 'unknown', 'empty', 'wrong-bytes', 'wrong-pin', 'symlink', 'extra-dir']) {
    const changed = clone();
    const path = join(changed, 'fixtures/valid/stream-empty.json');
    if (mutation === 'missing') rmSync(path);
    if (mutation === 'unknown') writeFileSync(join(changed, 'extra.json'), '{}');
    if (mutation === 'empty') writeFileSync(path, '');
    if (mutation === 'wrong-bytes') writeFileSync(path, bytes(path).toString().replace('stream', 'broken'));
    if (mutation === 'wrong-pin') writeFileSync(join(changed, 'source.sha256'), '0'.repeat(64) + '\n');
    if (mutation === 'symlink') { rmSync(path); symlinkSync(join(pkg, 'fixtures/valid/stream-empty.json'), path); }
    if (mutation === 'extra-dir') mkdirSync(join(changed, 'unreviewed'));
    refusal(run(['check', '--package', changed]));
  }
  refusal(run(['check', '--package', join(scratch, 'does-not-exist')]));
});

test('manifest-rejections-are-atomic', () => {
  const raw = bytes(join(pkg, 'manifest.json'));
  for (const bad of [Buffer.alloc(0), Buffer.from('{}'), Buffer.from('[]'), Buffer.from('{"format":1,"format":1}'),
    Buffer.from('{"format":NaN}'), Buffer.from('{"format":Infinity}'), Buffer.from('{"format":1e999}')]) {
    generateManifest(bad);
  }
  for (const mutate of [
    m => { m.format = true; },
    m => { m.authority.cutover_required = 1; },
    m => { m.compatibility.wire_min = true; },
    m => { m.vocabularies.channels.ctl.classes = ['stream']; },
    m => { m.vocabularies.channels.ctlr.segments = ['machine']; },
    m => { m.unknown = true; },
    m => { delete m.wire; },
    m => { m.vectors.envelopes = []; },
    m => { m.negative_cases = []; },
    m => { m.negative_cases.push(m.negative_cases[0]); },
    m => { m.vectors.receipts[0].sha256 = '0'.repeat(64); },
    m => { m.schemas.envelope.unknownKeyword = true; },
    m => { m.schemas.envelope.additionalProperties = true; },
    m => { m.authority.status = 'normative'; },
    m => { m.vectors.pairing_handover.aad += ' '; },
  ]) {
    const manifest = JSON.parse(raw);
    mutate(manifest);
    generateManifest(Buffer.from(canonical(manifest) + '\n'));
  }
  const out = join(scratch, 'wrong-pin-output');
  refusal(run(['generate', '--manifest', join(pkg, 'manifest.json'), '--output', out,
    '--expected-source-digest', '0'.repeat(64)]), 'wrong_digest');
  assert.equal(existsSync(out), false);
});

test('raw-input-bounds-and-batch-atomicity', () => {
  const valid = join(pkg, 'fixtures/valid/stream-empty.json');
  const invalid = join(pkg, 'fixtures/invalid/unknown-envelope-field.json');
  refusal(run(['validate', '--package', pkg, '--kind', 'envelope', valid, invalid]), 'schema_mismatch');
  const missing = join(scratch, 'missing');
  refusal(validate(missing));
  const linked = join(scratch, 'linked');
  symlinkSync(valid, linked);
  refusal(validate(linked), 'nonregular_input');
  refusal(validate(input(Buffer.alloc(33554433, 32))), 'oversized_input');
  refusal(validate(valid, 'envelope', ['--expected-sha256', '0'.repeat(64)]), 'wrong_digest');
  success(validate(valid, 'envelope', ['--expected-sha256', hash(bytes(valid))]));
  refusal(run(['validate', '--package', pkg, '--kind', 'envelope', '--expected-sha256', hash(bytes(valid)), valid, valid]), 'ambiguous_input_digest');
  const oversizedManifest = input(Buffer.alloc(2097153, 32));
  const out = join(scratch, 'oversized-output');
  refusal(run(['generate', '--manifest', oversizedManifest, '--expected-source-digest', '0'.repeat(64), '--output', out]), 'oversized_input');
  assert.equal(existsSync(out), false);
});

for (const kind of ['envelope', 'pairing_wrapper']) {
  test('large-ciphertext-' + kind, () => {
    const schema = json(join(pkg, `schemas/${kind}.schema.json`)).properties.ct;
    const sample = json(join(pkg, `fixtures/valid/${kind === 'envelope' ? 'stream-empty' : 'pairing-wrapper'}.json`));
    for (const size of [4 * 1024 * 1024, 16 * 1024 * 1024, 25_162_752]) {
      const good = Buffer.alloc(size).toString('base64');
      assert.equal(new RegExp(schema.pattern).test(good), true, `${kind}/${size}: ECMAScript must not overflow`);
      assert.equal(canonicalCiphertext(good, schema, kind === 'envelope' ? 1 : 16), true);
      success(validate(input(canonical({ ...sample, ct: good })), kind));
      const badBits = good.slice(0, -4) + 'AB==';
      const badLength = good.slice(0, -1);
      const badAlphabet = good.slice(0, -5) + '_' + good.slice(-4);
      const newline = good.slice(0, -1) + '\n';
      for (const bad of [badBits, badLength, badAlphabet, newline]) {
        assert.equal(canonicalCiphertext(bad, schema, kind === 'envelope' ? 1 : 16), false, `${kind}/${size}: malformed`);
        refusal(validate(input(canonical({ ...sample, ct: bad })), kind));
      }
    }
    const tooLarge = Buffer.alloc(25_162_753).toString('base64');
    assert.equal(canonicalCiphertext(tooLarge, schema, kind === 'envelope' ? 1 : 16), false);
    refusal(validate(input(canonical({ ...sample, ct: tooLarge })), kind), 'schema_mismatch');
  });
}

test('pairing-agreement-rejections', () => {
  const cases = json(join(pkg, 'crypto-negative-vectors.json'));
  const vector = json(join(pkg, 'vectors.json')).pairing_handover;
  const offer = JSON.parse(vector.offer);
  const key = privateKey(b64(vector.viewer_ephemeral_private_key), '6e');
  assert.equal(cases.format, 1);
  assert.deepEqual(cases.pairing_agreement.map(row => row.name), ['x25519-low-order-zero', 'x25519-low-order-one']);
  assert.deepEqual(cases.pairing_shared_secret.map(row => row.name), ['x25519-all-zero-result', 'x25519-short-result']);
  for (const row of cases.pairing_agreement) {
    assert.equal(row.expected, 'reject_before_hkdf');
    const peer = publicKey(b64(row.peer_public_key), '6e');
    // Real native X25519, not just a string/key-length assertion.
    assert.throws(() => diffieHellman({ privateKey: key, publicKey: peer }));
    // A syntactically valid low-order key cannot be rejected by a structure-only claim.
    const wrapper = { ...vector.wrapper, ephemeral_key: row.peer_public_key };
    assert.equal(success(validate(input(canonical(wrapper)), 'pairing_wrapper')).scope, 'structure_only');
  }
  for (const row of cases.pairing_shared_secret) {
    assert.equal(row.expected, 'reject_before_hkdf');
    // Also covers a provider that returns zero instead of throwing; never feed it to HKDF.
    assert.throws(() => pairingPhaseKey(b64(row.shared_secret), offer), /invalid_key_agreement/);
  }
});

test('compatibility-ledger', () => {
  const readme = bytes(join(pkg, 'README.md')).toString();
  const rows = readme.split('\n').filter(line => line.startsWith('| GAP-'));
  assert.equal(rows.length, 9, 'every sealed compatibility seam needs its own disposition');
  for (const id of ['CTLR', 'REPLY', 'RECEIPT', 'PAIR-SIZE', 'CT-MIN', 'NONCE', 'MASTER-KEY', 'MIRROR', 'RAW']) {
    const row = rows.find(line => line.startsWith('| GAP-' + id + ' |'));
    assert.ok(row, id);
    assert.match(row, /CLA-296 Refactor owner/);
    assert.match(row, /pending:/);
    assert.ok(row.split('|').length >= 7, 'subject, candidate, fixed runtime, units/source, owner/acceptance');
  }
  for (const fragment of ['PWA rejects ctlr', '65,536', '8,192', '33,550,336', '25,162,752',
    'no fixed-baseline runtime producer evidence', 'source observations, not live deployment measurements']) {
    assert.ok(readme.includes(fragment), fragment);
  }
});

test('malformed-schema-typed-refusal', () => {
  const manifest = json(join(pkg, 'manifest.json'));
  manifest.schemas.envelope.properties.sender.pattern = '^((?![\\s\\S])';
  generateManifest(Buffer.from(canonical(manifest) + '\n'), 'unsupported_schema');
});

test('unknown-kind-typed-refusal', () => {
  const before = fileSet(scratch);
  refusal(validate(join(pkg, 'fixtures/valid/stream-empty.json'), 'not_a_kind'), 'unknown_kind');
  assert.deepEqual(fileSet(scratch), before, 'a refused validation publishes no files');
});

for (const [index, integer] of [[0, 0], [1, 1]]) {
  test('boolean-open-metadata-' + integer, () => {
    const manifest = json(join(pkg, 'manifest.json'));
    manifest.vectors.control_response.response_open_results[index].succeeds = integer;
    generateManifest(Buffer.from(canonical(manifest) + '\n'), 'vector_shape');
  });
}

test('publication-race-preserves-competitor', () => {
  const probe = input(`import contextlib,io,json,pathlib,runpy,sys
m=runpy.run_path(sys.argv[1]); target=pathlib.Path(sys.argv[2]); package=pathlib.Path(sys.argv[3])
observed={}
def inject(source,destination):
    destination=pathlib.Path(destination)
    destination.mkdir(mode=0o700)
    observed['inode']=destination.lstat().st_ino
    return original(source,destination)
if 'rename_noreplace' in m:
    original=m['rename_noreplace']; m['generate'].__globals__['rename_noreplace']=inject
else:
    original=pathlib.Path.rename; pathlib.Path.rename=inject
stdout=io.TextIOWrapper(io.BytesIO(),encoding='utf-8'); stderr=io.TextIOWrapper(io.BytesIO(),encoding='utf-8')
with contextlib.redirect_stdout(stdout),contextlib.redirect_stderr(stderr):
    status=m['main'](['generate','--manifest',str(package/'manifest.json'),'--output',str(target)])
stdout.flush(); stderr.flush()
print(json.dumps({'status':status,'stdout':stdout.buffer.getvalue().decode(),'stderr':stderr.buffer.getvalue().decode(),
    'created_inode':observed['inode'],'final_inode':target.lstat().st_ino,'entries':[p.name for p in target.iterdir()],
    'temporaries':[p.name for p in target.parent.glob('.cloud-contract-*')]}))
`);
  const target = join(scratch, 'racing-output');
  const result = run([tool, target, pkg], { script: probe });
  assert.equal(result.status, 0, result.stderr);
  const observed = JSON.parse(result.stdout);
  process.stdout.write('publication-race observation: ' + JSON.stringify(observed) + '\n');
  refusal({ status: observed.status, stdout: observed.stdout, stderr: observed.stderr }, 'output_exists');
  assert.equal(observed.created_inode, observed.final_inode, 'the competitor keeps its directory inode');
  assert.deepEqual(observed.entries, []);
  assert.deepEqual(observed.temporaries, []);
});

test('publication-unsupported-no-output', () => {
  const probe = input(`import pathlib,runpy,sys
m=runpy.run_path(sys.argv[1]); m['sys'].platform='unsupported-test-platform'
sys.exit(m['main'](['generate','--manifest',sys.argv[2],'--output',sys.argv[3]]))
`);
  const target = join(scratch, 'unsupported-output');
  refusal(run([tool, join(pkg, 'manifest.json'), target], { script: probe }), 'publication_unsupported');
  assert.equal(existsSync(target), false);
  assert.equal(readdirSync(scratch).some(name => name.startsWith('.cloud-contract-')), false);
});

try {
  const chosen = cases.filter(row => !selected || row.name === selected);
  assert.ok(chosen.length > 0, 'selected test case must exist');
  for (const row of chosen) {
    try {
      await row.callback();
      checks++;
      process.stdout.write(`✓ ${row.name}\n`);
    } catch (error) {
      failures++;
      process.stderr.write(`FAIL ${row.name}: ${error.stack}\n`);
    }
  }
  assert.equal(checks + failures, selected ? 1 : 101, 'test cases must neither disappear nor multiply silently');
  process.stdout.write(`cloud-contract-v1: ${checks} passed, ${failures} failed, ${checks + failures} checks\n`);
  if (failures) process.exitCode = 1;
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
