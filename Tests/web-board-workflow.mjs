import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import http from "node:http";
import { pathToFileURL } from "node:url";

const projectRoot = path.resolve(process.env.CLAWDLINE_WORKFLOW_PROJECT_ROOT || ".");
const { observeBoardWorkflowSend } = await import(pathToFileURL(path.join(
    projectRoot, "Resources/web/app/js/input/board-workflow-status.js")));

const notes = [];
const toasts = [];
const effects = {
    note(event, data) { notes.push({ event, data }); },
    toast(message, bad) { toasts.push({ message, bad }); }
};

assert.equal(observeBoardWorkflowSend({ workflow: { status: "ingress_recorded" } }, effects),
    false);
assert.equal(observeBoardWorkflowSend({ workflow: {
    status: "unrecorded", code: "workflow_persistence_failed"
} }, effects), true);
assert.deepEqual(notes, [{
    event: "workflow.send.unrecorded", data: { code: "workflow_persistence_failed" }
}]);
assert.equal(toasts.length, 1);
assert.equal(toasts[0].bad, false,
    "a successful terminal send with a Board gap is a warning, not a failed send");
assert.match(toasts[0].message, /^Message sent;/);
assert.equal(observeBoardWorkflowSend({ workflow: {
    status: 503, code: "workflow_persistence_failed"
} }, effects), true, "a failed post-send journal settlement is also visible");
assert.equal(notes.length, 2);
assert.equal(observeBoardWorkflowSend({ workflow: {
    status: 200, code: "workflow_delivery_recorded"
} }, effects), false, "a recorded delivery is not shown as a gap");
assert.doesNotThrow(() => observeBoardWorkflowSend({ workflow: { status: "unrecorded" } }, {
    note() { throw new Error("diagnostic unavailable"); },
    toast() { throw new Error("view unavailable"); }
}), "secondary feedback cannot turn a successful send into a rejection");
const composerSource = fs.readFileSync(path.join(
    projectRoot, "Resources/web/app/js/input/composer.js"), "utf8");
assert.match(composerSource, /observeBoardWorkflowSend\(answer/,
    "the successful composer path must inspect the workflow annotation");
assert.match(composerSource, /Diagnostics\.note/,
    "an unrecorded workflow warning must also reach diagnostics");

const root = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-workflow-helper-"));
// Exercise the actual build packaging fragment without compiling or installing an App.
// A fresh bundle and an upgrade must both work from an arbitrary cwd and a minimal PATH.
const bundleRoot = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-workflow-bundle-"));
let bundleChecks = 0;
try {
    const build = fs.readFileSync(path.join(projectRoot, "build.sh"), "utf8");
    const fragment = build.match(/# BEGIN BOARD WORKFLOW BUNDLE\n([\s\S]*?)# END BOARD WORKFLOW BUNDLE/);
    assert.ok(fragment, "formal build must package the Board workflow executable and guide"); bundleChecks++;
    const resources = path.join(bundleRoot, "A relocated App.app", "Contents", "Resources");
    fs.mkdirSync(resources, { recursive: true });
    for (const upgrade of [false, true]) {
        const helper = path.join(resources, "clawdline-board-workflow");
        if (upgrade) fs.writeFileSync(helper, "stale helper bytes\n", { mode: 0o600 });
        const packaged = spawnSync("/bin/sh", ["-eu", "-c", fragment[1]], {
            cwd: projectRoot, env: { ...process.env, RES: resources }, encoding: "utf8"
        });
        assert.equal(packaged.status, 0, packaged.stderr); bundleChecks++;
        assert.equal(fs.readFileSync(helper, "utf8"),
            fs.readFileSync(path.join(projectRoot, "Resources/clawdline-board-workflow.sh"), "utf8")); bundleChecks++;
        assert.equal(fs.readFileSync(path.join(resources, "board-workflow.md"), "utf8"),
            fs.readFileSync(path.join(projectRoot, "Resources/board-workflow.md"), "utf8")); bundleChecks++;
        const invoked = spawnSync(helper, ["--version"], {
            cwd: bundleRoot, env: { PATH: "/usr/bin:/bin", HOME: bundleRoot }, encoding: "utf8"
        });
        assert.equal(invoked.status, 0, invoked.stderr); bundleChecks++;
        assert.equal(invoked.stdout.trim(), "clawdline-board-workflow 1"); bundleChecks++;
        assert.equal(fs.existsSync(path.join(bundleRoot, ".claude")) ||
            fs.existsSync(path.join(bundleRoot, ".codex")), false,
            "packaging/invocation must not install global provider configuration"); bundleChecks++;
    }
} finally { fs.rmSync(bundleRoot, { recursive: true, force: true }); }
try {
    const token = "a".repeat(64);
    const tokenFile = path.join(root, "token");
    const argvLog = path.join(root, "curl-argv");
    const carrierLog = path.join(root, "carrier");
    fs.writeFileSync(tokenFile, token, { mode: 0o600 });
    const fakeCurl = path.join(root, "curl");
    fs.writeFileSync(fakeCurl, `#!/bin/sh
for arg do printf '%s\\n' "$arg" >> "$CLAWDLINE_ARGV_LOG"; done
previous=
for arg do
  if [ "$previous" = H ]; then
    case "$arg" in
      @*) file=\${arg#@}; mode=$(stat -f '%Lp' "$file");
          grep -Eq '^X-Clawdline-Orchestrator: [a-f0-9]{64}$' "$file" || exit 91
          printf '%s\\n' "$mode" >> "$CLAWDLINE_CARRIER_LOG" ;;
    esac
  fi
  [ "$arg" = -H ] && previous=H || previous=
done
case " $* " in
  *'/v1/orchestrator/whoami'*) printf '%s' '{"terminal_id":"%fixture"}' ;;
  *) printf '{"ok":true}' ;;
esac
`, { mode: 0o700 });
    const result = spawnSync(path.join(projectRoot, "Resources/clawdline-board-workflow.sh"), [
        "11111111-1111-4111-8111-111111111111", "stable-supplement-1"
    ], {
        cwd: projectRoot,
        env: {
            ...process.env,
            PATH: root + path.delimiter + process.env.PATH,
            TMPDIR: root,
            CLAWDLINE_ORCHESTRATOR_TOKEN_FILE: tokenFile,
            CLAWDLINE_ARGV_LOG: argvLog,
            CLAWDLINE_CARRIER_LOG: carrierLog,
            CLAWDLINE_PORT: "7717"
        },
        input: '{"operation":"progress"}',
        encoding: "utf8"
    });
    assert.equal(result.status, 0, result.stderr);
    const observedArgv = fs.readFileSync(argvLog, "utf8");
    assert.equal(observedArgv.includes(token), false,
        "the actual curl child argv must not contain the machine credential");
    assert.equal(result.stdout.includes(token), false);
    assert.equal(result.stderr.includes(token), false);
    assert.equal(result.stdout.trim(), '{"ok":true}');
    assert.deepEqual(fs.readFileSync(carrierLog, "utf8").trim().split("\n"), ["600", "600"]);
    assert.equal(fs.readdirSync(root).some(name => name.startsWith("clawdline-board-header.")),
        false, "the secure header carrier is removed after both requests");
} finally {
    fs.rmSync(root, { recursive: true, force: true });
}

// Exercise the real curl/auth/idempotency boundary with synthetic credentials only.
const authRoot = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-workflow-auth-"));
const credential = Buffer.alloc(32, 251).toString("base64url");
const tokenPath = path.join(authRoot, "token");
const requests = [], receipts = new Map();
let helperChecks = 0;
function proof(name, actual, expected) { assert.deepEqual(actual, expected, name); helperChecks++; }
const server = http.createServer(async (req, res) => {
    requests.push({ url: req.url, key: req.headers["idempotency-key"] });
    res.setHeader("Content-Type", "application/json");
    if (req.headers["x-clawdline-orchestrator"] !== credential) {
        res.writeHead(403); res.end('{"error":"forbidden"}'); return;
    }
    if (req.url.startsWith("/v1/orchestrator/whoami?")) {
        res.end('{"terminal_id":"%fixture"}'); return;
    }
    if (req.url !== "/v1/orchestrator/sessions/%25fixture/workflow") {
        res.writeHead(404); res.end('{}'); return;
    }
    let body = ""; for await (const chunk of req) body += chunk;
    const key = req.headers["idempotency-key"];
    if (!key) { res.writeHead(400); res.end('{}'); return; }
    if (receipts.has(key) && receipts.get(key) !== body) {
        res.writeHead(409); res.end('{"error":"workflow_request_conflict"}'); return;
    }
    receipts.set(key, body); res.end('{"ok":true,"receipt":"stable"}');
});
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
async function invoke(value, key = "same-request", body = '{"operation":"progress","summary":"test"}') {
    if (value === null) fs.rmSync(tokenPath, { force: true });
    else fs.writeFileSync(tokenPath, value, { mode: 0o600 });
    const before = requests.length;
    const result = await new Promise(resolve => {
        const child = spawn(path.join(projectRoot, "Resources/clawdline-board-workflow.sh"), [
            "11111111-1111-4111-8111-111111111111", key
        ], { env: { ...process.env, TMPDIR: authRoot,
            CLAWDLINE_ORCHESTRATOR_TOKEN_FILE: tokenPath,
            CLAWDLINE_PORT: String(server.address().port) }, stdio: ["pipe", "pipe", "pipe"] });
        let stdout = "", stderr = "";
        child.stdout.on("data", chunk => { stdout += chunk; });
        child.stderr.on("data", chunk => { stderr += chunk; });
        child.on("close", status => resolve({ status, stdout, stderr }));
        child.stdin.end(body);
    });
    proof("credential never appears in output", (result.stdout + result.stderr).includes(credential), false);
    proof("all helper temporary carriers are cleaned", fs.readdirSync(authRoot).filter(name => name.startsWith("clawdline-board-")), []);
    return { ...result, calls: requests.slice(before) };
}
try {
    const first = await invoke(credential);
    proof("minted base64url credential authenticates", first.status, 0);
    proof("identity lookup precedes exactly one semantic POST", first.calls.map(row => row.url.split('?')[0]),
        ["/v1/orchestrator/whoami", "/v1/orchestrator/sessions/%25fixture/workflow"]);
    const replay = await invoke(credential);
    proof("same request reuses the server receipt", replay.stdout, first.stdout);
    proof("same request creates one receipt", receipts.size, 1);
    proof("the stable key is forwarded unchanged", replay.calls[1].key, "same-request");
    const conflict = await invoke(credential, "same-request", '{"operation":"progress","summary":"changed"}');
    proof("changed-body same-key conflict is not retried", [conflict.status, conflict.calls.length, receipts.size], [22, 2, 1]);
    const denied = await invoke(Buffer.alloc(32, 252).toString("base64url"));
    proof("identity auth refusal never reaches semantic POST", [denied.status, denied.calls.length], [22, 1]);
    for (const value of [null, "", credential + "\n", " " + credential, credential + " ",
        credential.slice(0, 20) + "\n" + credential.slice(20), credential + "\r\nX-Forged: yes",
        "a".repeat(65), "a".repeat(1024), "!".repeat(43), "a".repeat(42),
        credential.slice(0, -1) + "B",
        "a".repeat(63), Buffer.concat([Buffer.from(credential), Buffer.from([0])]),
        "a".repeat(64) + "\n", "a".repeat(64) + "\n" + "b".repeat(64)]) {
        const refused = await invoke(value);
        proof("malformed credential is refused before networking", [refused.status, refused.calls.length], [77, 0]);
        proof("malformed credential has a typed refusal", refused.stderr.trim(),
            "clawdline-board-workflow: machine_credential_unavailable");
    }
} finally {
    await new Promise(resolve => server.close(resolve));
    fs.rmSync(authRoot, { recursive: true, force: true });
}
console.log("web board workflow: " + (22 + helperChecks + bundleChecks) + " checks passed");
