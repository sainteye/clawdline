const { chmodSync, readFileSync, renameSync, writeFileSync } = await import("node:fs");
const { createHash } = await import("node:crypto");

const invalid = (reason) => {
    console.error(`task result preflight: invalid — ${reason}`);
    process.exit(1);
};
const object = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const exactKeys = (value, keys) => object(value)
    && Object.keys(value).length === keys.length
    && keys.every((key) => Object.hasOwn(value, key));
const nonEmpty = (value, maximum) => typeof value === "string"
    && value.trim().length > 0 && value.length <= maximum;
const taskID = (value) => typeof value === "string" && value.length === 36
    && /^[a-f0-9-]+$/.test(value);
const taskSecret = (value) => typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
const slug = (value) => typeof value === "string" && value.length > 0 && value.length <= 64
    && !value.startsWith("-") && /^[a-z0-9._-]+$/.test(value.toLowerCase());

const readJSON = (path, label) => {
    try {
        const bytes = readFileSync(path);
        const value = JSON.parse(bytes.toString("utf8"));
        if (!object(value)) invalid(`${label} must contain one JSON object`);
        return { value, bytes };
    } catch {
        invalid(`${label} is not readable JSON`);
    }
};

const args = process.argv.slice(2);
const hasReadyPath = args.length >= 3;
const [taskPath, resultPath, readyPath] = hasReadyPath ? args.slice(-3) : [...args.slice(-2), undefined];
if (!taskPath || !resultPath || taskPath === resultPath
    || (readyPath && (readyPath === taskPath || readyPath === resultPath))) {
    invalid("usage: validator task.json result.json.tmp [result.json.ready]");
}
const { value: task } = readJSON(taskPath, "task.json");
const { value: result, bytes: resultBytes } = readJSON(resultPath, "result.json.tmp");

if (task.clawdline_protocol !== 1 || !taskID(task.task_id)) {
    invalid("task.json has no valid protocol identity");
}
if (result.clawdline_protocol !== 1) invalid("clawdline_protocol must be 1");
if (!taskID(result.task_id) || result.task_id !== task.task_id) {
    invalid("task_id must be a lowercase UUID matching task.json");
}
if (!taskSecret(result.task_secret)) invalid("task_secret must be 64 lowercase hexadecimal characters");
if (result.status !== "success" && result.status !== "failure") {
    invalid("status must be success or failure");
}

if (Object.hasOwn(result, "verification")) {
    const row = result.verification;
    if (!object(row)
        || !Number.isInteger(row.runs) || row.runs < 0
        || !Number.isInteger(row.seconds) || row.seconds < 0
        || !["pass", "fail", "skipped"].includes(row.last)
        || !nonEmpty(row.scope, 300)) {
        invalid("verification must contain non-negative integer runs/seconds, a valid last value, and a non-empty scope");
    }
}

const graphNode = object(task.graph) && Array.isArray(task.graph.nodes)
    ? task.graph.nodes.find((node) => object(node) && node.id === task.graph.current_node)
    : undefined;
const kindWords = typeof task.kind === "string"
    ? task.kind.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean) : [];
const requiresReview = graphNode ? graphNode.kind === "review" : kindWords.includes("review");

const validateReview = (review) => {
    if (!exactKeys(review, ["verdict", "axes"])
        || !["safe_to_land", "changes_required"].includes(review.verdict)
        || !Array.isArray(review.axes) || review.axes.length !== 3) {
        invalid("review must contain only a valid verdict and exactly three axes");
    }
    const wantedAxes = new Set(["specification", "repository_invariants", "runtime_failure_behavior"]);
    const seenAxes = new Set();
    let findingCount = 0;
    for (const axis of review.axes) {
        if (!exactKeys(axis, ["axis", "status", "findings"])
            || !wantedAxes.has(axis.axis) || seenAxes.has(axis.axis)
            || !["pass", "findings"].includes(axis.status)
            || !Array.isArray(axis.findings) || axis.findings.length > 32) {
            invalid("review axes must be unique, closed, named axes with valid status and findings");
        }
        seenAxes.add(axis.axis);
        const findingIDs = new Set();
        for (const finding of axis.findings) {
            if (!exactKeys(finding, ["id", "severity", "summary", "evidence"])
                || !slug(finding.id) || findingIDs.has(finding.id)
                || !["blocking", "important", "minor"].includes(finding.severity)
                || !nonEmpty(finding.summary, 500)
                || !Array.isArray(finding.evidence) || finding.evidence.length < 1
                || finding.evidence.length > 8
                || finding.evidence.some((item) => !nonEmpty(item, 500))) {
                invalid("each review finding must use the exact id/severity/summary/evidence schema");
            }
            findingIDs.add(finding.id);
            findingCount += 1;
        }
        if ((axis.status === "pass") !== (axis.findings.length === 0)) {
            invalid("a passing axis has no findings and a findings axis has at least one");
        }
    }
    if (seenAxes.size !== wantedAxes.size) invalid("review must contain each required axis once");
    if ((review.verdict === "safe_to_land") !== (findingCount === 0)) {
        invalid("review verdict must agree with its findings");
    }
};

if (requiresReview && result.status === "success" && !Object.hasOwn(result, "review")) {
    invalid("a successful review task requires a closed review receipt");
}
if (Object.hasOwn(result, "review")) validateReview(result.review);

// A child normally renames the result immediately. If its shell stalls after validation, this
// task-owned marker lets the broker recover later without treating age as consent. It binds the
// exact bytes that passed validation; the broker additionally checks the stored task-secret hash
// and requires an unchanged observation window before publishing the final result.
if (readyPath) {
    const marker = {
        clawdline_protocol: 1,
        task_id: task.task_id,
        finalization_ready: true,
        result_sha256: createHash("sha256").update(resultBytes).digest("hex"),
    };
    const temporary = `${readyPath}.writing-${process.pid}`;
    writeFileSync(temporary, `${JSON.stringify(marker)}\n`, { flag: "wx", mode: 0o600 });
    chmodSync(temporary, 0o600);
    renameSync(temporary, readyPath);
}

console.log("task result preflight: valid");
