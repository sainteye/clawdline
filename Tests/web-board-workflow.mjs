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
// Historical proof repair is an operator action, not an automatic promotion by title.
const { planCriterionRepair, repairCriteria } = await import(pathToFileURL(path.join(
    projectRoot, "tools/repair-board-criterion-proof.mjs")));
let repairChecks = 0;
const repairCheck = (actual, expected, label) => {
    assert.deepEqual(actual, expected, label); repairChecks++;
};
const manifest = { version: 1, itemId: "item-a", projectId: "project-a", scopeRevision: 7,
    evidenceId: "proof-a", subject: "a".repeat(40),
    criteria: [{ checklistId: "criterion-a", reason: "Retained exact test case covers this criterion." },
        { checklistId: "criterion-b", reason: "Retained review and exact suite cover this criterion." }] };
const originalBoard = { enabled: true, available: true, revision: 10,
    readState: { status: "ready", revision: 10 }, item: {
        id: "item-a", projectId: "project-a", scopeRevision: 7,
        currentEvidence: { subject: manifest.subject, scopeRevision: 7 },
        projection: { checklist: { omittedCount: 0 }, evidence: { omittedCount: 0 } },
        checklist: [{ id: "criterion-a", status: "passed" }, { id: "criterion-b", status: "passed" }],
        verifications: [{ id: "proof-a", kind: "verification", status: "passed",
            subject: manifest.subject, scopeRevision: 7, sourceId: "retained-proof" }] } };
const copy = value => structuredClone(value);
function repairHarness() {
    let board = copy(originalBoard), state = null;
    const writes = [];
    return { writes, board: () => board, state: () => state,
        deps: { read: async () => copy(board), loadState: async () => copy(state),
            saveState: async value => { state = copy(value); },
            write: async body => {
                writes.push(copy(body));
                assert.equal(body.expectedRevision, board.revision);
                const proof = { id: "new-" + body.checklistId, kind: "verification", status: "passed",
                    subject: body.subject, scopeRevision: 7, sourceId: body.sourceId,
                    checklistId: body.checklistId, summary: body.summary };
                board.item.verifications.push(proof);
                board.item.checklist.find(c => c.id === body.checklistId).evidenceId = proof.id;
                board.revision++; board.readState.revision++;
                return { status: 200, body: {} };
            } } };
}
const firstPlan = planCriterionRepair(manifest, originalBoard);
repairCheck(firstPlan.rows.map(r => r.state), ["pending", "pending"], "preview shows only explicit criteria");
{
    const h = repairHarness();
    await repairCriteria(manifest, h.deps);
    repairCheck(h.writes.length, 0, "preview never writes or advances lifecycle");
    repairCheck(h.state(), null, "preview never creates retry state");
    await repairCriteria(manifest, h.deps, firstPlan.digest);
    repairCheck(h.writes.length, 2, "apply records exactly one proof per explicit criterion");
    repairCheck(h.writes.map(x => x.operation), ["record_evidence", "record_evidence"], "no transition or landing authority");
    await repairCriteria(manifest, h.deps, firstPlan.digest);
    repairCheck(h.writes.length, 2, "repeating an observed repair makes no duplicate proof");
}
for (const [name, mutate] of [
    ["foreign item", b => b.item.id = "foreign"],
    ["foreign project", b => b.item.projectId = "foreign"],
    ["changed scope", b => b.item.scopeRevision++],
    ["changed current subject", b => b.item.currentEvidence.subject = "b".repeat(40)],
    ["old proof scope", b => b.item.verifications[0].scopeRevision--],
    ["failed proof", b => b.item.verifications[0].status = "failed"],
    ["missing criterion", b => b.item.checklist.pop()],
    ["criterion not passed", b => b.item.checklist[0].status = "doing"],
    ["other attached evidence", b => b.item.checklist[0].evidenceId = "foreign-proof"],
    ["truncated evidence", b => b.item.projection.evidence.omittedCount = 1],
    ["stale read", b => b.readState.status = "stale"],
    ["mode off", b => b.enabled = false]
]) {
    const h = repairHarness(); mutate(h.board());
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_/); repairChecks++;
    repairCheck(h.writes.length, 0, name + " fails closed before mutation");
}
{
    const h = repairHarness();
    await assert.rejects(repairCriteria(manifest, h.deps, "bad-digest"), /repair_preview_mismatch/); repairChecks++;
    repairCheck(h.writes.length, 0, "a different preview cannot authorize apply");
    const bad = copy(manifest); bad.criteria.push(copy(bad.criteria[0]));
    assert.throws(() => planCriterionRepair(bad, h.board()), /repair_manifest/); repairChecks++;
}
{
    const h = repairHarness(); const write = h.deps.write;
    h.deps.write = async body => { await write(body); throw new Error("transport lost after commit"); };
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_transport_unknown/); repairChecks++;
    repairCheck(!!h.state().pending, true, "ambiguous committed reply retains exact request");
    h.deps.write = write;
    await repairCriteria(manifest, h.deps, firstPlan.digest);
    repairCheck(h.writes.length, 2, "restart observes the committed proof instead of resubmitting it");
}
{
    const h = repairHarness(); const write = h.deps.write;
    h.deps.write = async body => { h.writes.push(copy(body)); throw new Error("offline"); };
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_transport_unknown/); repairChecks++;
    const pending = copy(h.state().pending);
    h.deps.write = write;
    await repairCriteria(manifest, h.deps, firstPlan.digest);
    repairCheck(h.writes[1], pending, "uncertain request replays identical body including revision and request ID");
}
{
    const h = repairHarness();
    h.deps.saveState = async () => { throw new Error("disk full"); };
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_checkpoint/); repairChecks++;
    repairCheck(h.writes.length, 0, "cannot send without durable retry identity");
}
{
    const h = repairHarness(); h.deps.write = async () => ({ status: 409, body: { error: { code: "revision_conflict" } } });
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_refused_revision_conflict/); repairChecks++;
    repairCheck(h.state().pending, null, "deterministic revision refusal clears only this failed attempt");
}
{
    const h = repairHarness(); const write = h.deps.write;
    h.deps.write = async body => { const reply = await write(body); h.board().item.scopeRevision++; return reply; };
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_identity_changed/); repairChecks++;
    repairCheck(h.writes.length, 1, "concurrent scope change stops a partially applied batch");
}
{
    const h = repairHarness();
    h.deps.loadState = async () => ({ version: 1, digest: firstPlan.digest,
        pending: { operation: "transition", checklistId: "criterion-a", requestId: "a".repeat(36), expectedRevision: 10 } });
    await assert.rejects(repairCriteria(manifest, h.deps, firstPlan.digest), /repair_checkpoint_mismatch/); repairChecks++;
    repairCheck(h.writes.length, 0, "tampered retry file cannot add a command authority");
}
const repairRoot = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-criterion-repair-"));
const repairHTTP = repairHarness();
const repairToken = "b".repeat(64);
let repairHTTPRequests = 0;
const correctionFailures = [];
const repairServer = http.createServer(async (req, res) => {
    repairHTTPRequests++;
    if (req.headers["x-clawdline-orchestrator"] !== repairToken) { res.writeHead(401); res.end("{}"); return; }
    let answer;
    if (req.method === "GET") answer = { status: 200, body: { board: repairHTTP.board() } };
    else {
        let body = ""; for await (const chunk of req) body += chunk;
        answer = await repairHTTP.deps.write(JSON.parse(body));
    }
    res.writeHead(answer.status, { "Content-Type": "application/json" }); res.end(JSON.stringify(answer.body));
});
await new Promise(resolve => repairServer.listen(0, "127.0.0.1", resolve));
try {
    const file = path.join(repairRoot, "manifest.json"), state = path.join(repairRoot, "state.json");
    const token = path.join(repairRoot, "token");
    fs.writeFileSync(file, JSON.stringify(manifest)); fs.writeFileSync(token, repairToken);
    const invoke = args => new Promise(resolve => {
        const child = spawn(process.execPath, [path.join(projectRoot, "tools/repair-board-criterion-proof.mjs"), file, ...args], {
            env: { ...process.env, CLAWDLINE_PORT: String(repairServer.address().port), CLAWDLINE_ORCHESTRATOR_TOKEN_FILE: token }
        });
        let stdout = "", stderr = "";
        child.stdout.on("data", x => stdout += x); child.stderr.on("data", x => stderr += x);
        child.on("exit", status => resolve({ status, stdout, stderr }));
    });
    const preview = await invoke([]);
    repairCheck(preview.status, 0, "real CLI preview can read local authorized Board");
    repairCheck(JSON.parse(preview.stdout).digest, firstPlan.digest, "CLI preview pins the same exact plan");
    repairCheck(repairHTTP.writes.length, 0, "CLI preview makes no command");
    const args = ["--apply", firstPlan.digest, "--state", state];
    const applied = await invoke(args);
    repairCheck(applied.status, 0, applied.stderr);
    repairCheck(JSON.parse(applied.stdout).rows.every(r => r.state === "observed"), true, "CLI reports only observed repair");
    repairCheck(fs.statSync(state).mode & 0o777, 0o600, "retry state is private");
    repairCheck((await invoke(args)).status, 0, "CLI restart reads completed proof safely");
    repairCheck(repairHTTP.writes.length, 2, "CLI restart has no duplicate writes");
    fs.writeFileSync(state + ".lock", "");
    repairCheck((await invoke(args)).stderr.trim(), "repair_checkpoint_busy", "concurrent or orphaned state lock refuses");
    fs.unlinkSync(state + ".lock");
    fs.writeFileSync(token, repairToken + "\n");
    const refused = await invoke([]);
    repairCheck(refused.stderr.trim(), "repair_credential_unavailable", "newline credential fails closed");
    repairCheck([preview.stdout, applied.stdout, refused.stdout, refused.stderr].some(x => x.includes(repairToken)), false,
        "credentials never enter CLI output");
    for (const suffix of ["\n", "\r\n"]) {
        fs.writeFileSync(token, "A".repeat(43) + suffix);
        const before = repairHTTPRequests, result = await invoke([]);
        if (result.stderr.trim() !== "repair_credential_unavailable" || repairHTTPRequests !== before)
            correctionFailures.push("F1 minted token suffix " + JSON.stringify(suffix) + " code=" + result.stderr.trim()
                + " HTTP=" + (repairHTTPRequests - before));
        repairChecks++;
    }
} finally {
    await new Promise(resolve => repairServer.close(resolve));
    fs.rmSync(repairRoot, { recursive: true, force: true });
}
{
    const h = repairHarness(), padded = copy(manifest);
    padded.criteria[0].reason = "  " + padded.criteria[0].reason + "  ";
    const write = h.deps.write;
    h.deps.write = async body => {
        const result = await write(body);
        for (const proof of h.board().item.verifications) if (proof.summary) proof.summary = proof.summary.trim();
        return result;
    };
    try {
        const plan = planCriterionRepair(padded, h.board());
        await repairCriteria(padded, h.deps, plan.digest);
        await repairCriteria(padded, h.deps, plan.digest);
        if (h.writes.length !== 2 || plan.digest !== firstPlan.digest) correctionFailures.push("F2 noncanonical reason identity");
    } catch (error) { correctionFailures.push("F2 Store whitespace normalization: " + error.message); }
    repairChecks++;
}
assert.deepEqual(correctionFailures, [], "sealed F1/F2 correction proof"); repairChecks++;
console.log("web board workflow: " + (22 + helperChecks + bundleChecks + repairChecks) + " checks passed");
