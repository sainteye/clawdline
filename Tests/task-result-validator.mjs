import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const validator = join(root, "tools", "validate-task-result.mjs");
const scratch = mkdtempSync(join(tmpdir(), "clawdline-result-validator-"));
const foreignProject = join(scratch, "unrelated-project-with-no-tools");
const id = "0f8fad5b-d9cb-469f-a165-70867728950e";
const secret = "0123456789abcdef".repeat(4);
let checks = 0;

const ordinaryTask = { clawdline_protocol: 1, task_id: id, kind: "custom" };
const ordinaryResult = {
    clawdline_protocol: 1,
    task_id: id,
    task_secret: secret,
    status: "success",
    summary: "done",
    symbols: [],
    artifacts: [],
    verification: { runs: 1, seconds: 2, last: "pass", scope: "focused validator" },
    finished_at: "2026-09-08T12:00:00Z",
};
const passingAxes = [
    { axis: "specification", status: "pass", findings: [] },
    { axis: "repository_invariants", status: "pass", findings: [] },
    { axis: "runtime_failure_behavior", status: "pass", findings: [] },
];

function run(name, task, result) {
    const taskPath = join(scratch, `${name}-task.json`);
    const resultPath = join(scratch, `${name}-result.json.tmp`);
    writeFileSync(taskPath, JSON.stringify(task));
    writeFileSync(resultPath, JSON.stringify(result));
    return spawnSync(process.execPath, [validator, taskPath, resultPath], { encoding: "utf8" });
}

function embeddedProgram() {
    const tool = readFileSync(validator, "utf8");
    const program = tool.match(/\/\/ >>> child-result-validator-program >>>\n([\s\S]*?)\/\/ <<< child-result-validator-program <<</)?.[1];
    assert.ok(program, "repository-local validator has no extractable program");
    const swift = readFileSync(join(root, "Sources", "OrchestratorChildBrief.swift"), "utf8");
    const payload = swift.match(/childResultValidatorProgramBase64 = "([A-Za-z0-9+/=]+)"/)?.[1];
    assert.ok(payload, "child briefing has no embedded validator payload");
    assert.equal(Buffer.from(payload, "base64").toString("utf8"), program,
        "the briefing validator drifted from the repository-local validator");
    assert.match(swift, /resultPreflightCommand[\s\S]*?&& mv --/,
        "child briefing does not gate its atomic rename on successful preflight");
    assert.match(swift, /If validation fails,[\s\S]*?do\s+not rename[\s\S]*?correct that file/,
        "child briefing does not preserve a refused tmp receipt for correction");
    assert.match(swift, /`result\.json`[\s\S]*?only completion signal/,
        "child briefing introduces another completion signal");
    checks += 1;
    return payload;
}

function runEmbedded(name, task, result, payload) {
    const taskPath = join(scratch, `${name}-task.json`);
    const resultPath = join(scratch, `${name}-result.json.tmp`);
    writeFileSync(taskPath, JSON.stringify(task));
    writeFileSync(resultPath, JSON.stringify(result));
    mkdirSync(foreignProject);
    const loader = "eval(\"(async()=>{\"+Buffer.from(process.argv[1],\"base64\").toString(\"utf8\")+\"\\n})()\")";
    return spawnSync(process.execPath, ["-e", loader, payload, taskPath, resultPath], {
        encoding: "utf8", cwd: foreignProject,
    });
}

function publish(name, task, result, payload) {
    const taskPath = join(scratch, `${name}-task.json`);
    const tmpPath = join(scratch, `${name}-result.json.tmp`);
    const finalPath = join(scratch, `${name}-result.json`);
    const readyPath = join(scratch, `${name}-result.json.ready`);
    writeFileSync(taskPath, JSON.stringify(task));
    writeFileSync(tmpPath, JSON.stringify(result));
    const quote = (value) => `'${value.replaceAll("'", "'\\''")}'`;
    const loader = "eval(\"(async()=>{\"+Buffer.from(process.argv[1],\"base64\").toString(\"utf8\")+\"\\n})()\")";
    const command = [quote(process.execPath), "-e", quote(loader), quote(payload),
        quote(taskPath), quote(tmpPath), quote(readyPath), "&&", "mv", "--", quote(tmpPath),
        quote(finalPath), "&&", "rm", "-f", "--", quote(readyPath)].join(" ");
    const answer = spawnSync("/bin/sh", ["-c", command], { encoding: "utf8", cwd: foreignProject });
    return { answer, tmpPath, finalPath, readyPath };
}

function markReady(name, task, result) {
    const taskPath = join(scratch, `${name}-task.json`);
    const resultPath = join(scratch, `${name}-result.json.tmp`);
    const readyPath = join(scratch, `${name}-result.json.ready`);
    const resultBytes = Buffer.from(JSON.stringify(result));
    writeFileSync(taskPath, JSON.stringify(task));
    writeFileSync(resultPath, resultBytes);
    const answer = spawnSync(process.execPath, [validator, taskPath, resultPath, readyPath], {
        encoding: "utf8",
    });
    return { answer, resultBytes, readyPath };
}

function accepts(name, task, result) {
    const answer = run(name, task, result);
    assert.equal(answer.status, 0, `${name}: ${answer.stderr}`);
    checks += 1;
}

function refuses(name, task, result) {
    const answer = run(name, task, result);
    assert.notEqual(answer.status, 0, `${name}: invalid receipt was accepted`);
    checks += 1;
    return answer;
}

try {
    const payload = embeddedProgram();
    const foreign = runEmbedded("foreign project", ordinaryTask, ordinaryResult, payload);
    assert.equal(foreign.status, 0, `embedded validator failed outside Clawdline: ${foreign.stderr}`);
    assert.equal(readdirSync(foreignProject).length, 0,
        "embedded validation unexpectedly depended on or wrote the target project");
    checks += 1;

    const refusedPublish = publish("refused publish", ordinaryTask,
        { ...ordinaryResult, status: "complete" }, payload);
    assert.notEqual(refusedPublish.answer.status, 0, "invalid tmp receipt was published");
    assert.ok(existsSync(refusedPublish.tmpPath), "failed preflight removed the correctable tmp file");
    assert.ok(!existsSync(refusedPublish.finalPath), "failed preflight created the completion signal");
    assert.doesNotMatch(refusedPublish.answer.stdout + refusedPublish.answer.stderr,
        new RegExp(secret), "failed publication disclosed the task secret");
    checks += 1;

    const acceptedPublish = publish("accepted publish", ordinaryTask, ordinaryResult, payload);
    assert.equal(acceptedPublish.answer.status, 0, `valid tmp receipt was not published: ${acceptedPublish.answer.stderr}`);
    assert.ok(!existsSync(acceptedPublish.tmpPath) && existsSync(acceptedPublish.finalPath),
        "successful preflight did not atomically move tmp to the completion signal");
    assert.ok(!existsSync(acceptedPublish.readyPath),
        "ordinary successful publication left its recovery marker behind");
    checks += 1;

    const marked = markReady("explicit recovery marker", ordinaryTask, ordinaryResult);
    assert.equal(marked.answer.status, 0, `valid tmp did not receive a ready marker: ${marked.answer.stderr}`);
    const marker = JSON.parse(readFileSync(marked.readyPath, "utf8"));
    assert.deepEqual(Object.keys(marker).sort(), [
        "clawdline_protocol", "finalization_ready", "result_sha256", "task_id",
    ]);
    assert.equal(marker.task_id, id);
    assert.equal(marker.finalization_ready, true);
    assert.equal(marker.result_sha256, createHash("sha256").update(marked.resultBytes).digest("hex"));
    assert.equal(statSync(marked.readyPath).mode & 0o077, 0,
        "ready marker is readable outside its task owner");
    checks += 1;

    const { verification: _omittedVerification, ...ordinaryWithoutVerification } = ordinaryResult;
    accepts("valid ordinary without optional verification", ordinaryTask,
        ordinaryWithoutVerification);
    refuses("malformed optional verification", ordinaryTask, {
        ...ordinaryResult,
        verification: { runs: -1, seconds: 2, last: "green", scope: "" },
    });

    accepts("valid review", { ...ordinaryTask, kind: "code-review" }, {
        ...ordinaryResult,
        review: { verdict: "safe_to_land", axes: passingAxes },
    });

    refuses("unknown finding key", { ...ordinaryTask, kind: "review" }, {
        ...ordinaryResult,
        review: {
            verdict: "changes_required",
            axes: [
                { axis: "specification", status: "findings", findings: [{
                    id: "R1", severity: "blocking", summary: "bad",
                    evidence: ["source:1"], owner: "nobody",
                }] },
                ...passingAxes.slice(1),
            ],
        },
    });

    const badAxes = refuses("bad axes", { ...ordinaryTask, kind: "review" }, {
        ...ordinaryResult,
        review: { verdict: "safe_to_land", axes: passingAxes.slice(0, 2) },
    });
    assert.doesNotMatch(badAxes.stderr, new RegExp(secret), "bad axes disclosed the task secret");
    const badVerdict = refuses("bad verdict", { ...ordinaryTask, kind: "review" }, {
        ...ordinaryResult,
        review: { verdict: "approved", axes: passingAxes },
    });
    assert.doesNotMatch(badVerdict.stderr, new RegExp(secret), "bad verdict disclosed the task secret");

    refuses("wrong task id", ordinaryTask, { ...ordinaryResult,
        task_id: "1f8fad5b-d9cb-469f-a165-70867728950e" });

    const disclosure = refuses("secret non-disclosure", ordinaryTask, {
        ...ordinaryResult,
        status: "complete",
    });
    assert.doesNotMatch(disclosure.stdout + disclosure.stderr, new RegExp(secret),
        "validation output disclosed the task secret");
    checks += 1;

    console.log(`task result validator: ${checks} focused checks passed`);
} finally {
    rmSync(scratch, { recursive: true, force: true });
}
