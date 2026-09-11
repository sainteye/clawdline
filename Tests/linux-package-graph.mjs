import assert from 'node:assert/strict';
import { chmodSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
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
const entry = read('Packages/ClawdlineLinux/main.swift');
const build = read('build.sh');
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
    linuxTests: /name: "ClawdlineLinuxTests",\s*dependencies: \["ClawdlineApplication", "ClawdlineLinux"\],\s*path: "Packages\/ClawdlineLinuxTests"/s.test(packageText),
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
check(/--product ClawdlineLinux/.test(linuxBuild),
  'Ubuntu compiler check must build the Linux executable product');
check(/swift test/.test(linuxBuild) && /CLAWDLINE_TEST_TMUX/.test(linuxBuild),
  'Ubuntu compiler check must execute the real Linux SwiftPM runtime contracts with tmux');
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
  const scratch = realpathSync(mkdtempSync(join(tmpdir(), 'clawdline-linux-contract-')));
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

    writeConfig();
    result = run('run', '--config', config);
    if (process.platform === 'linux') {
      check(result.status === 69 && errorCode(result) === 'capability_unavailable',
        'run without validated provider/tmux descriptors must refuse before an effect');
    } else {
      check(result.status === 69 && errorCode(result) === 'unsafe_runtime_path',
        'a non-Linux execution must not soften Linux canonical-path evidence to make a local probe green');
      check(result.stdout === '', 'a refused non-Linux composition emits no partial runtime receipt');
    }
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
}

console.log(`linux package graph: ${checks} checks passed`);
