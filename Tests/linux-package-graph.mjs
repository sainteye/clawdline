import assert from 'node:assert/strict';
import { chmodSync, chownSync, copyFileSync, existsSync, linkSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { homedir, tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const read = (path) => readFileSync(join(root, path), 'utf8');
const manifest = read('Package.swift');
const composition = read('Packages/ClawdlineLinux/LinuxComposition.swift');
const adapters = read('Packages/ClawdlineLinux/LinuxRuntimeAdapters.swift');
const containedFiles = read('Packages/ClawdlineLinux/LinuxContainedFileSystem.swift');
const providerRuntime = read('Packages/ClawdlineLinux/LinuxProviderRuntime.swift');
const applicationLaunch = read('Sources/SessionLaunchPolicy.swift');
const applicationLifecycle = read('Sources/ProviderLifecyclePolicy.swift');
const applicationRoots = read('Sources/ProjectRootPolicy.swift');
const applicationScheduler = read('Sources/TerminalCommandScheduler.swift');
const linuxTests = read('Packages/ClawdlineLinuxTests/LinuxRuntimeContractTests.swift');
const daemonTemplate = read('Packaging/linux/daemon.json.in');
const dependencyLock = JSON.parse(read('Packaging/linux/dependencies.lock.json'));
const resolvedLock = JSON.parse(read('Package.resolved'));
const tmuxUnit = read('Packaging/systemd/clawdline-tmux.service');
const cloudTransport = read('Sources/CloudTransport.swift');
const cloudTransportTests = read('Tests/CloudTransportTests.swift');
const durableCloud = read('Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift');
const packageHelper = read('tools/linux-package-helper.py');
const packageTool = read('tools/linux-package.sh');
const systemdContract = read('tools/linux-systemd-contract.sh');
const entry = read('Packages/ClawdlineLinux/main.swift');
const build = read('build.sh');
const testRunner = read('test.sh');
const linuxBuild = read('tools/swift-core-application-linux-build.sh');
const workflow = read('.github/workflows/ci.yml');
const runtimeOnly = process.env.CLAWDLINE_LINUX_RUNTIME_ONLY === '1';

let checks = 0;
const check = (condition, message) => {
  checks += 1;
  assert.ok(condition, message);
};

function inspectPackage(packageText, buildText) {
  const productBuild = buildText.indexOf('\nswift build ');
  const productArgument = buildText.indexOf('--product Clawdline &', productBuild);
  return {
    linuxProduct: /\.executable\(name: "ClawdlineLinux", targets: \["ClawdlineLinux"\]\)/.test(packageText),
    linuxEdge: /name: "ClawdlineLinux",\s*dependencies: \["ClawdlineApplication"\],\s*path: "Packages\/ClawdlineLinux"/s.test(packageText),
    linuxTests: /name: "ClawdlineLinuxTests",\s*dependencies: \[[\s\S]*?"ClawdlineApplication", "ClawdlineLinux"[\s\S]*?NIOEmbedded[\s\S]*?\],\s*path: "Packages\/ClawdlineLinuxTests"/s.test(packageText),
    macProductBuild: productBuild >= 0 && productArgument > productBuild
      && productArgument - productBuild < 240,
    noFlatCompiler: !/^swiftc\s/m.test(buildText),
    copiedProduct: /cp "\$SWIFTPM_PRODUCT" "\$BIN"/.test(buildText),
    identityCheck: /bundle_product_sha256/.test(buildText) && /swiftpm_product_sha256/.test(buildText)
  };
}

if (!runtimeOnly) {
const packageShape = inspectPackage(manifest, build);
check(Object.values(packageShape).every(Boolean),
  `SwiftPM product graph or Mac wrapper is incomplete: ${JSON.stringify(packageShape)}`);
check(/\.package\(url: "https:\/\/github\.com\/apple\/swift-nio\.git", exact: "2\.102\.0"\)/.test(manifest)
  && /\.package\(url: "https:\/\/github\.com\/apple\/swift-nio-ssl\.git", exact: "2\.37\.4"\)/.test(manifest)
  && dependencyLock.packages.some((row) => row.identity === 'swift-nio' && row.version === '2.102.0')
  && dependencyLock.packages.some((row) => row.identity === 'swift-nio-ssl' && row.version === '2.37.4'),
  'Linux WebSocket and TLS dependencies must be exact and retained in signed provenance');
const dependencyVerifier = join(root, 'tools/linux-dependency-lock.py');
const verifyResolution = (resolved, lock) => spawnSync('python3', [dependencyVerifier, 'verify',
  '--resolved', resolved, '--lock', lock], { encoding: 'utf8' });
const snapshotResolution = (resolved, lock, directory, cwd = root) => spawnSync('python3', [dependencyVerifier,
  'snapshot', '--resolved', resolved, '--lock', lock, '--directory', directory],
{ encoding: 'utf8', cwd });
const matchSnapshot = (actual, expected) => spawnSync('python3', [dependencyVerifier, 'match',
  '--actual', actual, '--expected', expected], { encoding: 'utf8' });
const dependencyFixture = realpathSync(mkdtempSync(join(tmpdir(), 'clawdline-linux-dependencies-')));
try {
  const resolvedPath = join(dependencyFixture, 'Package.resolved');
  const lockPath = join(dependencyFixture, 'dependencies.lock.json');
  writeFileSync(resolvedPath, JSON.stringify(resolvedLock));
  writeFileSync(lockPath, JSON.stringify(dependencyLock));
  check(verifyResolution(resolvedPath, lockPath).status === 0,
    'the exact SwiftPM resolution must equal the signed dependency lock');

  const missing = structuredClone(dependencyLock);
  missing.packages.pop();
  writeFileSync(lockPath, JSON.stringify(missing));
  check(verifyResolution(resolvedPath, lockPath).status !== 0,
    'an unsigned extra SwiftPM pin must make dependency verification red');

  const omitted = structuredClone(resolvedLock);
  omitted.pins.pop();
  writeFileSync(lockPath, JSON.stringify(dependencyLock));
  writeFileSync(resolvedPath, JSON.stringify(omitted));
  check(verifyResolution(resolvedPath, lockPath).status !== 0,
    'a missing SwiftPM pin must make dependency verification red');

  const drift = structuredClone(resolvedLock);
  drift.pins[0].state.revision = '0'.repeat(40);
  writeFileSync(resolvedPath, JSON.stringify(drift));
  check(verifyResolution(resolvedPath, lockPath).status !== 0,
    'a revision drift must make dependency verification red');

  check(verifyResolution(join(dependencyFixture, 'absent.resolved'), lockPath).status !== 0,
    'a missing Package.resolved must fail closed');

  writeFileSync(resolvedPath, JSON.stringify(resolvedLock));
  const snapshotDirectory = join(dependencyFixture, 'snapshot');
  const snapped = snapshotResolution(resolvedPath, lockPath, snapshotDirectory, tmpdir());
  check(snapped.status === 0
    && readFileSync(join(snapshotDirectory, 'Package.resolved'), 'utf8') === JSON.stringify(resolvedLock)
    && readFileSync(join(snapshotDirectory, 'dependencies.lock.json'), 'utf8') === JSON.stringify(dependencyLock),
  `dependency snapshot must pin the exact accepted pair from a non-repository cwd: ${snapped.stderr}`);

  writeFileSync(resolvedPath, `${JSON.stringify(resolvedLock)}\n`);
  check(matchSnapshot(resolvedPath, join(snapshotDirectory, 'Package.resolved')).status !== 0,
    'a root resolution mutation after snapshot must be detected before acceptance');
  check(verifyResolution(join(snapshotDirectory, 'Package.resolved'),
    join(snapshotDirectory, 'dependencies.lock.json')).status === 0,
  'a later source mutation must not change the descriptor-pinned pair used for staging and signing');

  const hardlinked = join(dependencyFixture, 'hardlinked.lock.json');
  linkSync(lockPath, hardlinked);
  check(snapshotResolution(join(snapshotDirectory, 'Package.resolved'), hardlinked,
    join(dependencyFixture, 'hardlink-snapshot')).status !== 0,
  'a hardlinked dependency authority must fail closed');

  const fifo = join(dependencyFixture, 'resolved.fifo');
  const fifoCreated = spawnSync('mkfifo', [fifo], { encoding: 'utf8' });
  check(fifoCreated.status === 0 && snapshotResolution(fifo, lockPath,
    join(dependencyFixture, 'fifo-snapshot')).status !== 0,
  'a FIFO Package.resolved authority must fail closed without blocking');
} finally {
  rmSync(dependencyFixture, { recursive: true, force: true });
}
const compileLockCheck = linuxBuild.indexOf('linux-dependency-lock.py');
const compileStarts = linuxBuild.indexOf('if swift build --product ClawdlineLinux');
const packageLockCheck = packageTool.indexOf('python3 "$dependency_lock_helper" snapshot',
  packageTool.indexOf('build_package()'));
const packageStage = packageTool.indexOf('stage=$(mktemp', packageLockCheck);
const provenanceDigest = packageTool.indexOf('dependency_lock_digest=', packageLockCheck);
check(compileLockCheck >= 0 && /linux-dependency-lock\.py snapshot/.test(linuxBuild)
  && /linux-dependency-lock\.py match/.test(linuxBuild) && compileStarts > compileLockCheck
  && (linuxBuild.match(/--disable-automatic-resolution/g) || []).length >= 3,
  'Linux compile and test must snapshot, consume, and revalidate the fixed SwiftPM resolution');
check(packageLockCheck >= 0 && /dependency_lock_snapshot/.test(packageTool)
  && /dependency_resolved_snapshot/.test(packageTool) && packageStage > packageLockCheck
  && provenanceDigest > packageLockCheck
  && packageTool.indexOf('dependency_lock_helper" match', packageStage) < provenanceDigest,
  'package signing must stage/hash only the pinned pair and revalidate it before provenance generation');
const packageGateFixture = realpathSync(mkdtempSync(join(tmpdir(), 'clawdline-linux-package-gate-')));
try {
  const fixtureTools = join(packageGateFixture, 'tools');
  const fixtureLockDir = join(packageGateFixture, 'Packaging', 'linux');
  const fixtureSystemdDir = join(packageGateFixture, 'Packaging', 'systemd');
  mkdirSync(fixtureTools, { recursive: true });
  mkdirSync(fixtureLockDir, { recursive: true });
  mkdirSync(fixtureSystemdDir, { recursive: true });
  let fixturePackageTool = packageTool
    .replace('package_temporary_paths=()', 'package_temporary_paths=(/nonexistent-test-path)')
    .replace('[ "$(uname -s)" = Linux ] || fail "Linux packaging requires a Linux host or private container"',
      ': # fixture executes only the pre-effect dependency gate');
  // macOS ships Bash 3 without readarray. This fixture is about the dependency gate, so replace
  // only the release-contract decoding subprocess with the exact values the dummy reports.
  const readarrayStart = fixturePackageTool.indexOf('  readarray -t contract_fields < <(');
  const readarrayEnd = fixturePackageTool.indexOf('\n  )\n  [ "${#contract_fields[@]}"', readarrayStart);
  assert.ok(readarrayStart >= 0 && readarrayEnd > readarrayStart);
  fixturePackageTool = fixturePackageTool.slice(0, readarrayStart)
    + '  contract_fields=(1 1 1 1 1 fixture)\n'
    + fixturePackageTool.slice(readarrayEnd + 5);
  writeFileSync(join(fixtureTools, 'linux-package.sh'), fixturePackageTool);
  writeFileSync(join(fixtureTools, 'linux-package-helper.py'), packageHelper);
  writeFileSync(join(fixtureTools, 'linux-dependency-lock.py'), read('tools/linux-dependency-lock.py'));
  writeFileSync(join(fixtureLockDir, 'dependencies.lock.json'), JSON.stringify(dependencyLock));
  for (const path of ['clawdline-daemon-wrapper', 'clawdline.conf', 'daemon.json.in']) {
    copyFileSync(join(root, 'Packaging/linux', path), join(fixtureLockDir, path));
  }
  for (const path of ['clawdline-daemon.service', 'clawdline-tmux.service']) {
    copyFileSync(join(root, 'Packaging/systemd', path), join(fixtureSystemdDir, path));
  }
  const dummyBinary = join(packageGateFixture, 'binary');
  const dummyKey = join(packageGateFixture, 'key');
  writeFileSync(dummyBinary, '#!/bin/sh\nexit 1\n');
  writeFileSync(dummyKey, 'not-a-key\n');
  chmodSync(dummyBinary, 0o755);
  const refused = spawnSync('bash', [join(fixtureTools, 'linux-package.sh'), 'build',
    '--binary', dummyBinary, '--version', '1.0.0', '--build-identity', 'fixture',
    '--source-commit', '0'.repeat(40), '--signing-key', dummyKey,
    '--output-dir', join(packageGateFixture, 'out')], { encoding: 'utf8' });
  check(refused.status !== 0 && /Package\.resolved.*missing|resolution does not match/.test(refused.stderr)
    && !existsSync(join(packageGateFixture, 'out')),
    `linux-package build must refuse a missing consumed resolution before producing output: ${JSON.stringify({status: refused.status, stderr: refused.stderr})}`);

  const fixtureResolved = join(packageGateFixture, 'Package.resolved');
  writeFileSync(fixtureResolved, JSON.stringify(resolvedLock));
  writeFileSync(dummyBinary, `#!/bin/sh
if [ "\${1:-}" = release-contract ]; then
  printf '%s\\n' '{"configurationSchemaVersion":1,"configurationReadableMinimum":1,"durableSchemaVersion":1,"durableReadableMinimum":1,"durableReadableMaximum":1,"protocolIdentity":"fixture"}'
  printf '\\n' >> "$CLAWDLINE_FIXTURE_RESOLVED"
fi
`);
  chmodSync(dummyBinary, 0o755);
  const raced = spawnSync('bash', [join(fixtureTools, 'linux-package.sh'), 'build',
    '--binary', dummyBinary, '--version', '1.0.0', '--build-identity', 'fixture',
    '--source-commit', '0'.repeat(40), '--signing-key', dummyKey,
    '--output-dir', join(packageGateFixture, 'race-out')], {
    encoding: 'utf8', env: { ...process.env, CLAWDLINE_FIXTURE_RESOLVED: fixtureResolved }
  });
  check(raced.status !== 0 && /resolution changed after package acceptance/.test(raced.stderr)
    && !existsSync(join(packageGateFixture, 'race-out', '1.0.0-linux-amd64.provenance.json')),
  `a concurrent root-resolution mutation must stop package signing: ${JSON.stringify({status: raced.status, stderr: raced.stderr})}`);
} finally {
  rmSync(packageGateFixture, { recursive: true, force: true });
}
const provenanceFixture = realpathSync(mkdtempSync(join(tmpdir(), 'clawdline-linux-provenance-schema-')));
try {
  const commonProvenance = {
    packageVersion: '1.0.0', buildIdentity: 'fixture', sourceCommit: '0'.repeat(40),
    architecture: 'amd64', archiveFile: '1.0.0-linux-amd64.tar.gz',
    archiveSha256: '1'.repeat(64), publicKeySha256: '2'.repeat(64),
    signatureAlgorithm: 'openssl-rsa-sha256', configurationSchemaVersion: 1,
    configurationReadableMinimum: 1,
    durableSchema: { writeVersion: 1, readMinimum: 1, readMaximum: 1 },
    protocolIdentity: 'fixture'
  };
  const v1Path = join(provenanceFixture, 'v1.json');
  const v2Path = join(provenanceFixture, 'v2.json');
  writeFileSync(v1Path, JSON.stringify({ schemaVersion: 1, ...commonProvenance }));
  writeFileSync(v2Path, JSON.stringify({ schemaVersion: 2, ...commonProvenance,
    dependencyLockSha256: '3'.repeat(64), dependencyPackages: dependencyLock.packages }));
  const validate = (path, mode) => spawnSync('python3', [join(root, 'tools/linux-package-helper.py'),
    'validate-provenance', '--provenance', path, '--mode', mode], { encoding: 'utf8' });
  check(validate(v1Path, 'installed').status === 0 && validate(v1Path, 'candidate').status !== 0,
    'a literal signed v1 fixture is readable only as an installed upgrade/rollback image');
  check(validate(v2Path, 'candidate').status === 0 && validate(v2Path, 'installed').status === 0,
    'a literal v2 fixture is accepted both as the required new candidate and installed image');
} finally {
  rmSync(provenanceFixture, { recursive: true, force: true });
}
check(/^ExecStart=\/usr\/bin\/tmux -D -S \/run\/clawdline\/clawdline\.sock$/m.test(tmuxUnit),
  'the tmux service must run one foreground server without an invalid keeper command');
function inspectSystemdHealthFixture(text) {
  return /configuration_schema=\$\{5:-2\}/.test(text)
    && /health_marker=\$\{6:-\}/.test(text)
    && /configurationSchemaVersion":%s/.test(text)
    && /if \[ "\$ready" = true \] && \[ -n "\$health_marker" \]/.test(text)
    && /check test -s "\$good_health_marker"/.test(text);
}
check(inspectSystemdHealthFixture(systemdContract),
  'the systemd fixture must parameterize configuration schema and prove good health was executed');
check(!inspectSystemdHealthFixture(systemdContract.replace(
  'check test -s "$good_health_marker"', ': # mutation removes the reached-health proof')),
  'removing the reached-health assertion must make the systemd fixture guard red');
check(/#if os\(Linux\)[\s\S]*CloudNIOLinuxSocketConnector/.test(cloudTransport)
  && /maxFrameSize: 32 \* 1024 \* 1024/.test(cloudTransport)
  && /certificateVerification = \.fullVerification/.test(cloudTransport)
  && /CloudURLSessionSocketConnector\(\)/.test(cloudTransport),
  'Linux must use pinned NIO TLS/WebSocket while preserving the Mac URLSession connector');
function inspectLinuxRelayTransport(text) {
  return {
    boundedToken: /CloudBoundedTokenHTTPClient/.test(text)
      && /maximumBytes: 64 \* 1024/.test(text)
      && /timeoutIntervalForResource = 15/.test(text)
      && /wire\.token\.utf8\.count <= 16 \* 1024/.test(text)
      && /wire\.relayURL\.utf8\.count <= 2_048/.test(text)
      && /let deadline = await clock\.monotonicNow\(\) \+ openingTimeout/.test(text)
      && /openingTimeout: connectorBudget/.test(text)
      && /remainingOpeningBudget\(deadline\)/.test(text),
    oneShotOpen: /scheduleTask\([\s\S]*connectionTimedOut/.test(text)
      && /private var closed = false/.test(text)
      && /the WebSocket upgrade closed/.test(text)
      && /if !promiseBox\.succeed[\s\S]{0,100}pipe\.close\(\)/.test(text)
      && /onCancel:[\s\S]{0,160}promiseBox\.fail\(CancellationError\(\)\)/.test(text)
      && !/withThrowingTaskGroup\(of: CloudEstablishedTransportSocket/.test(text),
    boundedInbound: /maxAccumulatedFrameSize: 32 \* 1024 \* 1024/.test(text)
      && /AsyncThrowingStream<String, Error>\(bufferingPolicy: \.bufferingOldest\(1\)\)/.test(text)
      && /case \.dropped = pipe\.continuation\.yield\(text\)/.test(text)
      && /inbound text buffer overflowed/.test(text),
    closeControl: /case \.pong:\s*break/.test(text)
      && /opcode: \.connectionClose, data: frame\.unmaskedData/.test(text)
      && /whenComplete \{ _ in[\s\S]{0,80}channel\.close/.test(text)
      && /scheduleTask\(in: \.seconds\(1\)\)/.test(text)
  };
}
const relayTransport = inspectLinuxRelayTransport(cloudTransport);
check(Object.values(relayTransport).every(Boolean),
  `Linux token/open/frame/close bounds are incomplete: ${JSON.stringify(relayTransport)}`);
for (const [name, mutation] of [
  ['boundedToken', cloudTransport.replace('maximumBytes: 64 * 1024', 'maximumBytes: Int.max')],
  ['oneShotOpen', cloudTransport.replace('private var closed = false', 'private var closed = true')],
  ['boundedInbound', cloudTransport.replace('bufferingPolicy: .bufferingOldest(1)',
    'bufferingPolicy: .unbounded')],
  ['closeControl', cloudTransport.replace('case .pong:', 'case .binary:')]
]) {
  check(inspectLinuxRelayTransport(mutation)[name] === false,
    `${name} production mutation must make its focused guard red`);
}
check(/advance: 6/.test(cloudTransportTests)
  && /abs\(passedBudget - 9\)/.test(cloudTransportTests)
  && /stalledClock\.advance\(by: 15\)/.test(cloudTransportTests)
  && /cancelledConnect\.cancel\(\)/.test(cloudTransportTests),
  'the shared opening deadline needs deterministic remaining-budget, stall, and cancellation fixtures');
check(!/abs\(passedBudget - 9\)/.test(cloudTransportTests.replace(
  'abs(passedBudget - 9)', 'abs(passedBudget - 15)')),
  'the remaining-budget fixture must turn red when token time is not deducted');
check(/testNIOOpeningOwnershipInboundBoundsAndCloseControl/.test(linuxTests)
  && /a stalled opening promise did not terminate/.test(linuxTests)
  && /EOF before upgrade completed/.test(linuxTests)
  && /second unconsumed message fails closed/.test(linuxTests)
  && /aggregate fragmented text beyond/.test(linuxTests)
  && /peer Close is echoed/.test(linuxTests),
  'Linux NIO timeout/EOF/burst/fragment/Pong/Close behavior fixtures must remain registered');
check(!/second unconsumed message fails closed/.test(linuxTests.replace(
  'second unconsumed message fails closed', 'overflow assertion removed')),
  'removing the Linux burst-overflow assertion must make the focused fixture guard red');
check(/case degraded/.test(durableCloud) && /delay = min\(30, delay \* 2\)/.test(durableCloud)
  && /case \.unauthorized/.test(durableCloud)
  && /case \.upgradeRefused\(let status\): return status == 401 \|\| status == 403/.test(durableCloud)
  && /account_revoked/.test(durableCloud),
  'initial transient Relay failure must back off while authorization refusal remains terminal');
function inspectLinuxBrowserContract(runtimeText, ingressText) {
  return {
    canonicalInventory: /snapshot\.isComplete/.test(runtimeText)
      && /publishedInventoryRows/.test(runtimeText)
      && /channelSegment\("__clawdline_inventory_v1__"\)/.test(runtimeText)
      && /"deleted": true/.test(runtimeText)
      && /channelSegment\(machine\.machineID\) \+ "\/" \+ channelSegment\(id\)/.test(runtimeText),
    closedLaunch: /assistantName\.isEmpty[\s\S]{0,80}\? \.claude/.test(runtimeText)
      && /\["haiku", "sonnet", "opus"\]\.contains\(model\)/.test(runtimeText)
      && /model: model\.isEmpty \? nil : model/.test(runtimeText),
    durableModel: /let model: String\?/.test(ingressText)
      && /schemaVersion: 3/.test(ingressText)
      && /model: request\.model/.test(ingressText),
    correlatedReply: /"assistant": request\.assistant/.test(runtimeText)
      && /"model": request\.model/.test(runtimeText)
      && /refusalPayload/.test(runtimeText)
  };
}
const linuxIngress = read('Packages/ClawdlineLinux/LinuxDaemonIngress.swift');
const browserContract = inspectLinuxBrowserContract(durableCloud, linuxIngress);
check(Object.values(browserContract).every(Boolean),
  `Linux browser/inventory contract is incomplete: ${JSON.stringify(browserContract)}`);
for (const [name, changedRuntime, changedIngress] of [
  ['canonicalInventory', durableCloud.replace('snapshot.isComplete', 'true'), linuxIngress],
  ['closedLaunch', durableCloud.replace('["haiku", "sonnet", "opus"].contains(model)', 'true'), linuxIngress],
  ['durableModel', durableCloud, linuxIngress.replace('model: request.model', 'model: nil')],
  ['correlatedReply', durableCloud.replace('"model": request.model ?? ""', '"model": ""'), linuxIngress]
]) {
  check(inspectLinuxBrowserContract(changedRuntime, changedIngress)[name] === false,
    `${name} production mutation must make its focused guard red`);
}
check(/unchanged complete scan does not mint/.test(linuxTests)
  && /incomplete scan cannot tombstone/.test(linuxTests)
  && /durable drain exposes only its first pending sibling/.test(linuxTests)
  && /a new authenticated generation republishes/.test(linuxTests),
  'Linux inventory fixtures must cover dedupe, incomplete preservation, delayed siblings, and replay');
check(!/incomplete scan cannot tombstone/.test(linuxTests.replace(
  'incomplete scan cannot tombstone', 'incomplete scan assertion removed')),
  'removing the incomplete-scan assertion must make the focused fixture guard red');

// Representative red proofs for the two graph/build failure classes. These mutate only in memory:
// the test must be capable of rejecting an inert Linux edge and a return to flat compilation.
const noLinuxEdge = manifest.replace(
  'name: "ClawdlineLinux",\n            dependencies: ["ClawdlineApplication"]',
  'name: "ClawdlineLinux",\n            dependencies: []'
);
check(!inspectPackage(noLinuxEdge, build).linuxEdge,
  'the Linux edge mutation must make the graph inspection red');
const flatBuild = build.replace('swift build \\\n  "${swiftpm_common[@]}"', 'swiftc \\\n  "${swiftpm_common[@]}"');
check(!inspectPackage(manifest, flatBuild).macProductBuild && !inspectPackage(manifest, flatBuild).noFlatCompiler,
  'the flat-compiler mutation must make the build-wrapper inspection red');

check(/^import ClawdlineApplication$/m.test(composition),
  'Linux composition must consume its Application dependency');
check(/HostCapabilityUnavailable\.code/.test(composition),
  'Linux composition must reuse the Application capability refusal vocabulary');
check(!/^(?:@\w+\s+)?import\s+(?:AppKit|Security|ServiceManagement|Speech|AVFoundation|Carbon|Darwin)$/m.test(composition + entry),
  'Linux composition must have no Apple platform import');
check(/ready: false/.test(composition) && /readinessCode: "w4_runtime_not_configured"/.test(composition),
  'W4-1 health must separate compiled code from configured/usable capability');
check(/O_NOFOLLOW/.test(composition + containedFiles) && /O_NONBLOCK/.test(composition + containedFiles)
  && /O_CLOEXEC/.test(composition + containedFiles) && /fstat\(/.test(composition + containedFiles),
  'protected and contained inputs must bind validation and bounded reads to safe descriptors');
check(/st_uid/.test(composition) && /st_mode/.test(composition) && /st_size/.test(composition)
  && /read\(descriptor/.test(composition),
  'protected inputs must fail closed on unavailable owner, mode, size, type, or read evidence');
check(/case \.internalFailure/.test(composition) && /"internal_failure"/.test(composition)
  && /\.internalFailure/.test(entry),
  'unexpected failures must report internal_failure with EX_SOFTWARE');
const protectedCloudLogin = /case "cloud-login": return \.cloudLogin/.test(composition)
  && /LinuxProtectedFileSecretStore/.test(composition)
  && /authorization_required/.test(composition)
  && /already_enrolled/.test(composition)
  && !/deviceCode\s*=/.test(composition);
check(protectedCloudLogin,
  'Linux enrollment must be explicit, idempotent, protected, and omit the opaque device code');
check(!/case "cloud-login": return \.cloudLogin/.test(
  composition.replace('case "cloud-login": return .cloudLogin', 'case "cloud-login-disabled": return .cloudLogin')),
  'removing the Linux enrollment command must make the package guard red');
check(/--product ClawdlineLinux/.test(linuxBuild),
  'Ubuntu compiler check must build the Linux executable product');
const selfContainedPackage = /--static-swift-stdlib/.test(linuxBuild)
  && /grep -Eq 'libswift\|libFoundation\|=> not found'/.test(linuxBuild)
  && /package binary depends on an unavailable Swift\/Foundation runtime/.test(packageTool);
check(selfContainedPackage,
  'the signed Linux package must carry a fresh-host self-contained Swift product');
check(!/--static-swift-stdlib/.test(
  linuxBuild.replaceAll('--static-swift-stdlib', '--dynamic-swift-stdlib')),
  'removing static Swift linkage must make the package guard red');
check(/systemd-sysusers "\$script_dir\/\.\.\/Packaging\/linux\/clawdline\.conf"/.test(packageTool),
  'host installation must resolve sysusers config from the package tool, not caller cwd');
check(/swift test/.test(linuxBuild) && /CLAWDLINE_TEST_TMUX/.test(linuxBuild),
  'Ubuntu compiler check must execute the real Linux SwiftPM runtime contracts with tmux');
const macFocusedRuntime = /--filter LinuxRuntimeContractTests/.test(testRunner)
  && /expected one Linux runtime XCTest receipt/.test(testRunner);
check(macFocusedRuntime,
  'the locked Mac Linux-package mode must compile and execute target-only runtime contracts');
check(!/--filter LinuxRuntimeContractTests/.test(
  testRunner.replace('--filter LinuxRuntimeContractTests', '--skip LinuxRuntimeContractTests')),
  'removing the focused Linux runtime filter must make the package guard red');
check(/swift:6\.1\.3-noble@sha256:/.test(workflow) && /swift-core-application-linux-build\.sh/.test(workflow),
  'CI must keep the pinned real Ubuntu compiler check');
check(/public final class TerminalCommandScheduler/.test(applicationScheduler)
  && /let scheduling: TerminalCommandScheduler/.test(providerRuntime)
  && /scheduling\.run/.test(providerRuntime)
  && !/TerminalWorkSchedulingOwner/.test(applicationLifecycle + providerRuntime),
  'the established Application serial scheduler must be the sole Mac/Linux admission owner');
check(/envArguments/.test(adapters) && /"\/usr\/bin\/env", "-i"/.test(adapters)
  && /allowedKeys/.test(applicationLifecycle),
  'Linux provider launch must construct a closed environment instead of filtering inheritance');
check(/O_DIRECTORY \| O_NOFOLLOW/.test(containedFiles) && /AT_SYMLINK_NOFOLLOW/.test(containedFiles)
  && /isLexicallySafeAbsolute/.test(applicationRoots),
  'project and file containment must reject traversal and linked components');
check(/SessionLaunchPolicy\.admit/.test(providerRuntime) && /TerminalMenuAnswerPolicy\.admit/.test(providerRuntime)
  && /public enum SessionLaunchPolicy/.test(applicationLaunch)
  && /public enum TerminalMenuAnswerPolicy/.test(applicationLaunch),
  'Linux create and answer must consume the shared Application admission policies before effects');
check(/testRealTmuxProviderLifecycleOnLinux/.test(linuxTests)
  && /testProcIdentityUsesExactStartTokenAndGroup/.test(linuxTests)
  && /testClosedProviderEnvironmentCannotLeakInheritedCredentials/.test(linuxTests),
  'the Linux SwiftPM target must keep real lifecycle and containment mutation contracts');
const explicitCloudGate = /"cloudCommandsEnabled":@CLOUD_COMMANDS_ENABLED@/.test(daemonTemplate)
  && /--cloud-commands-enabled/.test(packageHelper)
  && /default=False/.test(packageHelper)
  && /type=canonical_boolean/.test(packageHelper)
  && /configured is not args\.cloud_commands_enabled/.test(packageHelper)
  && /--cloud-commands-enabled "\$cloud_commands_enabled"/.test(packageTool);
check(explicitCloudGate,
  'the signed daemon template must carry an explicit closed cloud write gate');
const gateRoot = mkdtempSync(join(tmpdir(), 'clawdline-package-gate-'));
try {
  const prepared = spawnSync('python3', [
    join(root, 'tools/linux-package-helper.py'), 'prepare-state',
    '--install-root', gateRoot,
    '--template', join(root, 'Packaging/linux/daemon.json.in'),
    '--uid', String(process.getuid()),
    '--gid', String(process.getgid()),
    '--cloud-commands-enabled', 'false'
  ], { encoding: 'utf8' });
  check(prepared.status === 0,
    `the package helper must accept the explicit false write gate: ${prepared.stderr}`);
  const preparedConfig = JSON.parse(readFileSync(join(gateRoot, 'etc/clawdline/daemon.json'), 'utf8'));
  check(preparedConfig.runtime.cloudCommandsEnabled === false,
    'the package helper must preserve the explicit false write gate as a JSON boolean');
  const invalid = spawnSync('python3', [
    join(root, 'tools/linux-package-helper.py'), 'prepare-state',
    '--install-root', `${gateRoot}-invalid`,
    '--template', join(root, 'Packaging/linux/daemon.json.in'),
    '--uid', String(process.getuid()),
    '--gid', String(process.getgid()),
    '--cloud-commands-enabled', 'False'
  ], { encoding: 'utf8' });
  check(invalid.status !== 0,
    'the package helper must reject noncanonical write-gate spellings');
} finally {
  rmSync(gateRoot, { recursive: true, force: true });
  rmSync(`${gateRoot}-invalid`, { recursive: true, force: true });
}
check(!/"cloudCommandsEnabled":true/.test(daemonTemplate),
  'the package template must never default hosted writes open');
check(!/"cloudCommandsEnabled":@CLOUD_COMMANDS_ENABLED@/.test(
  daemonTemplate.replace('@CLOUD_COMMANDS_ENABLED@', '')),
  'removing the cloud gate placeholder must make the package guard red');

function inspectCorrectionWave({ scheduler, runtime, adaptersText, compositionText, containedText }) {
  return {
    sharedScheduler: /queue\.sync/.test(scheduler) && /setRestartMaintenance/.test(scheduler)
      && /scheduling\.run/.test(runtime) && /requiredSession\(sessionID\)/.test(runtime),
    osContainment: /landlockABIVersion/.test(adaptersText) && /installSeccomp/.test(adaptersText)
      && /pathsOverlap/.test(runtime),
    boundedRunner: /O_NONBLOCK/.test(adaptersText) && /poll\(&descriptors/.test(adaptersText)
      && /aggregateOutputBytes \+ count <= maximumOutputBytes/.test(adaptersText)
      && /terminateAndReap/.test(adaptersText),
    effectReceipt: /struct LinuxLifecycleFailure/.test(runtime)
      && /pastedNotSubmitted/.test(runtime) && /compensateCreated/.test(runtime),
    truthfulReadiness: /struct LinuxCapabilityState/.test(compositionText)
      && /w4_provider_authentication_not_proven/.test(compositionText)
      && /authenticated: false/.test(compositionText),
    fullIdentity: /effectiveGID/.test(adaptersText) && /supplementaryGroups/.test(adaptersText)
      && /current\.identity == identity/.test(adaptersText),
    secretCoordinator: /static let shared = LinuxSecretStoreCoordinator/.test(containedText)
      && /coordinator\.withAccount/.test(containedText) && /readUnlocked/.test(containedText)
  };
}

const correctionSubject = {
  scheduler: applicationScheduler,
  runtime: providerRuntime,
  adaptersText: adapters,
  compositionText: composition,
  containedText: containedFiles
};
const correction = inspectCorrectionWave(correctionSubject);
check(Object.values(correction).every(Boolean),
  `sealed W4-1 correction classes are incomplete: ${JSON.stringify(correction)}`);

// One representative in-memory red mutation per sealed failure class. Each removes the exact
// mechanism the corresponding assertion needs; no mutation writes into the checkout.
const mutations = [
  ['sharedScheduler', { scheduler: applicationScheduler.replace('queue.sync', 'queue.async') }],
  ['osContainment', { adaptersText: adapters.replaceAll('landlockABIVersion', 'removedLandlockABI') }],
  ['boundedRunner', { adaptersText: adapters.replace('O_NONBLOCK', 'O_RDONLY') }],
  ['effectReceipt', { runtime: providerRuntime.replaceAll('pastedNotSubmitted', 'lostPartialStage') }],
  ['truthfulReadiness', { compositionText: composition.replaceAll('authenticated: false', 'authenticated: true') }],
  ['fullIdentity', { adaptersText: adapters.replaceAll('effectiveGID', 'discardedGID') }],
  ['secretCoordinator', { containedText: containedFiles.replaceAll('coordinator.withAccount', 'uncoordinated') }]
];
for (const [name, changed] of mutations) {
  const mutated = inspectCorrectionWave({ ...correctionSubject, ...changed });
  check(mutated[name] === false, `${name} representative mutation must make its guard red`);
}
}

const binary = process.env.CLAWDLINE_LINUX_BINARY;
if (binary) {
  // Darwin's /tmp and /var are aliases that the runtime correctly rejects as linked components.
  // Use the caller-owned home for this short-lived canonical-path fixture; Linux keeps its normal
  // temporary directory. The finally block below removes the fixture on every assertion path.
  const scratchParent = process.platform === 'darwin' ? homedir() : tmpdir();
  const scratch = realpathSync(mkdtempSync(join(scratchParent, '.clawdline-linux-contract-')));
  chownSync(scratch, process.getuid(), process.getgid());
  try {
    const secret = join(scratch, 'daemon.secret');
    const config = join(scratch, 'daemon.json');
    const symlink = join(scratch, 'daemon-link.secret');
    const configSymlink = join(scratch, 'daemon-link.json');
    const fifo = join(scratch, 'daemon.fifo');
    const oversized = join(scratch, 'daemon-oversized.json');
    writeFileSync(secret, 'not-a-production-secret\n', { mode: 0o600 });

    const writeConfig = (overrides = {}) => {
      writeFileSync(config, JSON.stringify({
        version: 1,
        listen: { host: '127.0.0.1', port: 7718 },
        stateDirectory: join(scratch, 'state'),
        secretFile: secret,
        ...overrides
      }));
      chmodSync(config, 0o644);
    };
    const run = (...args) => spawnSync(binary, args, {
      encoding: 'utf8',
      timeout: 2000,
      env: { ...process.env, CLAWDLINE_BUILD_IDENTITY: 'linux-contract-test' }
    });
    const errorCode = (result) => JSON.parse(result.stderr).error.code;

    let result = run('health');
    check(result.status === 0, `health failed: ${result.stderr}`);
    const health = JSON.parse(result.stdout);
    check(health.service === 'clawdline-daemon' && health.executable === 'ClawdlineLinux'
      && health.identityKind === 'diagnostic' && health.buildIdentity === 'linux-contract-test'
      && !Object.hasOwn(health, 'protocolVersion'),
      'health must carry diagnostic service/build identity without prematurely defining a wire protocol');
    check(health.ready === false && health.readinessCode === 'w4_runtime_not_configured',
      'health must distinguish compiled W4-1 adapters from configuration and readiness');
    check(health.supportedCapabilities.length === 0
      && health.capabilityStates.every((row) => row.configured === false && row.usable === false),
      'unconfigured health must not promote compiled adapters into usable capabilities');

    writeConfig();
    result = run('check-config', '--config', config);
    check(result.status === 0, `protected config should validate: ${result.stderr}`);
    const receipt = JSON.parse(result.stdout);
    check(receipt.configuration === 'accepted_not_started' && receipt.secretBytes > 0
      && !result.stdout.includes('not-a-production-secret'),
      'config receipt must confirm validation without exposing secret bytes');

    chmodSync(secret, 0o644);
    result = run('check-config', '--config', config);
    check(result.status === 78 && errorCode(result) === 'invalid_secret_file',
      'world-readable secret must fail closed before startup');
    chmodSync(secret, 0o600);

    symlinkSync(secret, symlink);
    writeConfig({ secretFile: symlink });
    result = run('check-config', '--config', config);
    check(result.status === 78 && errorCode(result) === 'invalid_secret_file',
      'secret symlink must fail closed before startup');

    writeConfig();
    symlinkSync(config, configSymlink);
    result = run('check-config', '--config', configSymlink);
    check(result.status === 78 && errorCode(result) === 'invalid_configuration',
      'config symlink must fail closed before startup');

    writeConfig();
    chmodSync(config, 0o666);
    result = run('check-config', '--config', config);
    check(result.status === 78 && errorCode(result) === 'invalid_configuration',
      'group/world-writable config must fail closed before startup');

    writeFileSync(oversized, Buffer.alloc(65537), { mode: 0o644 });
    result = run('check-config', '--config', oversized);
    check(result.status === 78 && errorCode(result) === 'invalid_configuration',
      'oversized config must fail before an unbounded read');

    const fifoCreated = spawnSync('mkfifo', [fifo], { encoding: 'utf8' });
    check(fifoCreated.status === 0, `FIFO fixture creation failed: ${fifoCreated.stderr}`);
    result = run('check-config', '--config', fifo);
    check(result.status === 78 && result.signal === null && errorCode(result) === 'invalid_configuration',
      'FIFO config must be rejected within the timeout instead of blocking on open');

    writeConfig({ listen: { host: '0.0.0.0', port: 7718 } });
    result = run('check-config', '--config', config);
    check(result.status === 78 && errorCode(result) === 'invalid_configuration',
      'W3 skeleton must refuse a public listener rather than imply network support');

    writeConfig({ stateDirectory: `${scratch}/state/../state` });
    result = run('run', '--config', config);
    check(result.status === 69 && errorCode(result) === 'unsafe_runtime_path',
      'a lexically noncanonical runtime path must be refused before containment or an effect');

    writeConfig();
    result = run('run', '--config', config);
    if (process.platform === 'linux') {
      check(result.status === 69 && errorCode(result) === 'capability_unavailable',
        'run without validated provider/tmux descriptors must refuse before an effect');
    } else {
      check(result.status === 69 && errorCode(result) === 'capability_unavailable',
        'a canonical non-Linux probe must reach the unavailable containment boundary without an effect');
      check(result.stdout === '', 'a refused non-Linux composition emits no partial runtime receipt');
    }
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
}

console.log(`linux package graph: ${checks} checks passed`);
