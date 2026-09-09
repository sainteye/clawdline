import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
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
  *'/v1/orchestrator/whoami'*) printf '{"terminal_id":"%fixture"}' ;;
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

console.log("web board workflow: 22 checks passed");
