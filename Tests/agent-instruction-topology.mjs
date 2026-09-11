import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const root = new URL("../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");
const [agents, coverageText] = await Promise.all([
    read("AGENTS.md"),
    read("docs/agent-instruction-coverage.json")
]);
const coverage = JSON.parse(coverageText);
const mutation = process.env.CLAWDLINE_AGENT_TOPOLOGY_MUTATION ?? "";
if (mutation === "drop-clause-coverage") {
    coverage.agreements.find((entry) => entry.id === "never").clauses =
        coverage.agreements.find((entry) => entry.id === "never").clauses
            .filter((clause) => clause.id !== "deploy-build-stamp-acceptance");
}
if (mutation === "archive-as-current-evidence") {
    coverage.scenarios.find((entry) => entry.id === "instruction-precedence").evidence[0] = {
        path: "docs/shared-tree-guard.md",
        anchor: "## The failure mode, and why",
        terms: ["fails open"]
    };
}

let checks = 0;
const check = (condition, message) => { assert.ok(condition, message); checks += 1; };
const equal = (actual, expected, message) => { assert.deepEqual(actual, expected, message); checks += 1; };
const exactKeys = (value, wanted, message) => equal(Object.keys(value).sort(), [...wanted].sort(), message);

const documents = new Map([["AGENTS.md", agents]]);
async function document(path) {
    if (!documents.has(path)) documents.set(path, await read(path));
    return documents.get(path);
}

const parsedSections = new Map();
async function markdownSections(path) {
    if (parsedSections.has(path)) return parsedSections.get(path);
    const text = await document(path);
    const headings = [...text.matchAll(/^(#{1,6}) ([^\n]+)$/gm)].map((match) => ({
        anchor: match[0],
        level: match[1].length,
        start: match.index
    }));
    // A clause belongs to the direct body under one unique heading. Text moved into a child
    // section, a TOC, a duplicated heading, or an incident appendix cannot satisfy that owner.
    const sections = headings.map((heading, index) => ({
        ...heading,
        text: text.slice(heading.start, headings[index + 1]?.start ?? text.length)
    }));
    parsedSections.set(path, sections);
    return sections;
}

async function exactSection(path, anchor) {
    const matches = (await markdownSections(path)).filter((entry) => entry.anchor === anchor);
    equal(matches.length, 1, `${path} contains exactly one ${anchor} section`);
    return matches[0].text;
}

function containsTerms(text, terms, label) {
    check(Array.isArray(terms) && terms.length > 0, `${label} carries bounded clause terms`);
    const lower = text.toLocaleLowerCase().replace(/\s+/g, " ");
    for (const term of terms) {
        const needle = typeof term === "string" ? term.toLocaleLowerCase().replace(/\s+/g, " ") : "";
        check(needle.length > 0 && lower.includes(needle),
            `${label} retains ${JSON.stringify(term)} inside its declared section`);
    }
}

equal(coverage.schema, "clawdline.agent-instruction-coverage.v2", "coverage schema is clause-level v2");
equal(coverage.precedence.order,
    ["global", "repository", "nearest-domain", "task-brief"],
    "instruction sources are ordered from broadest to nearest");
equal(coverage.precedence.winner, "last-applicable", "last applicable source wins");
equal(coverage.precedence.empty_nearest_file, "no-additional-rules",
    "an intentionally empty nearest file adds no imaginary rules");
equal(coverage.precedence.truncation, "preserve-nearest-domain-whole",
    "a length limit preserves the nearest rules whole");

const allowedClasses = new Set(coverage.classifications);
equal([...allowedClasses].sort(), [
    "archived_incident_rationale", "executable_guard", "inherited_nearest_domain_rule",
    "remains_global", "scenario_evaluation"
], "coverage classifications are the five DOC-1 dispositions");

const rootHeadings = [...agents.matchAll(/^#{1,4} (.+)$/gm)].map((match) => match[1]);
const mappedHeadings = coverage.agreements.map((entry) => entry.root_heading);
equal([...mappedHeadings].sort(), [...rootHeadings].sort(),
    "every root working-agreement heading has exactly one coverage row");
equal(new Set(mappedHeadings).size, mappedHeadings.length,
    "coverage rows do not hide duplicate root headings");
equal(new Set(coverage.agreements.map((entry) => entry.id)).size, coverage.agreements.length,
    "agreement ids are unique");

const requiredClauseIDs = [
    "opening-status-is-foreign", "context-before-semantics", "instruction-source-precedence",
    "named-staging-only", "foreign-staged-index-trap", "pathspec-unstaged-hunk-trap",
    "shared-sequencer-guard", "shared-index-guard", "shared-index-incident-rationale",
    "delivery-review-not-integration",
    "root-owns-exact-landing", "classify-all-residue", "preserve-unlanded-mixed",
    "owned-scratch-cleanup", "unreleased-line-required", "inspect-descendants-before-close",
    "close-cascade-rationale", "root-completion-only-after-whole-turn", "no-partial-root-completion",
    "peer-wait-is-durable-state", "user-wait-is-not-peer-wait", "human-ending-seven-scan-points",
    "human-ending-nothing-is-answer", "human-ending-release-states", "human-ending-remaining-owner",
    "human-ending-process-feedback", "human-ending-synthesize-not-paste", "human-ending-session-title",
    "persist-wait-and-notify", "notification-no-secret-no-loop", "one-options-prompt-at-a-time",
    "recommended-consequential-options", "separate-technical-and-user-lists",
    "child-proves-working-overlay", "root-proves-exact-candidate", "one-release-candidate-full",
    "reuse-identical-verification-tuple", "assert-check-count-not-exit",
    "restart-follows-secret-recoverability", "build-before-replace",
    "confirmation-binds-subject-predicate-time-method-observer", "confirmation-incident-rationale",
    "comparison-holds-identity-still", "comparison-rationale-is-not-policy",
    "lossy-projection-requires-source", "lossy-rendering-rationale",
    "phone-defect-uses-canonical-artifact", "representative-red-proof", "false-green-rationale",
    "enumerate-referrers-before-extraction", "sampling-claims-only-measured-path",
    "sampling-incident-rationale", "audit-complete-writer-reader-journey",
    "measure-machine-constant-locally", "resource-incident-rationale", "root-owns-dispatch-graph",
    "event-driven-watchdogs", "stable-result-finalizer", "closed-dispatch-role-contract",
    "assistant-reports-use-message-route", "distinguish-observer-from-service-failure",
    "human-label-machine-id", "audit-full-capacity-protocol-path", "dispatch-requires-broker-task",
    "dispatch-detail-owner", "child-brief-is-authoritative", "child-result-is-authenticated",
    "child-does-not-land", "child-forbidden-git-actions", "child-forbidden-build",
    "cloud-document-canonical-identity", "deploy-build-web-bundle", "deploy-pages-project",
    "deploy-pages-host-acceptance", "deploy-build-stamp-acceptance",
    "single-operational-finalizer", "never-touch-foreign-work"
];
const clauses = coverage.agreements.flatMap((entry) => entry.clauses);
equal(clauses.map((clause) => clause.id).sort(), [...requiredClauseIDs].sort(),
    "the controlled operative-clause inventory is complete");
equal(new Set(clauses.map((clause) => clause.id)).size, clauses.length,
    "operative clause ids are unique");

const scenarioIDs = coverage.scenarios.map((scenario) => scenario.id);
equal(new Set(scenarioIDs).size, 11, "the eleven required scenarios are unique");
const scenarioSet = new Set(scenarioIDs);
const archiveSections = new Set();

for (const agreement of coverage.agreements) {
    exactKeys(agreement, ["id", "root_heading", "clauses"], `${agreement.id} has a closed row`);
    check(typeof agreement.id === "string" && /^[a-z0-9-]+$/.test(agreement.id),
        `${agreement.root_heading} has a stable agreement id`);
    check(Array.isArray(agreement.clauses) && agreement.clauses.length > 0,
        `${agreement.id} owns at least one clause`);
    const rootAnchor = rootHeadings.find((heading) => heading === agreement.root_heading);
    check(Boolean(rootAnchor), `${agreement.id} resolves its root heading`);
    const rootSection = await exactSection("AGENTS.md",
        `${"#".repeat((await markdownSections("AGENTS.md")).find((entry) =>
            entry.anchor.replace(/^#{1,6} /, "") === agreement.root_heading).level)} ${rootAnchor}`);
    for (const clause of agreement.clauses) {
        check(allowedClasses.has(clause.disposition), `${clause.id} uses a closed disposition`);
        if (clause.disposition === "archived_incident_rationale") {
            exactKeys(clause, ["id", "disposition", "rationale_owner", "rationale_anchor", "terms"],
                `${clause.id} is archive-only and cannot masquerade as current policy`);
            const rationale = await exactSection(clause.rationale_owner, clause.rationale_anchor);
            containsTerms(rationale, clause.terms, clause.id);
            archiveSections.add(`${clause.rationale_owner}\u0000${clause.rationale_anchor}`);
            continue;
        }
        const keys = ["id", "disposition", "owner", "anchor", "terms"];
        if (clause.disposition === "executable_guard") keys.push("guard");
        if (clause.disposition === "scenario_evaluation") keys.push("scenario");
        exactKeys(clause, keys, `${clause.id} has the schema required by its disposition`);
        if (clause.disposition === "remains_global") {
            equal(clause.owner, "AGENTS.md", `${clause.id} remains in the root instructions`);
        }
        if (clause.disposition === "inherited_nearest_domain_rule") {
            check(clause.owner !== "AGENTS.md", `${clause.id} names a narrower operational owner`);
        }
        const ownerSection = await exactSection(clause.owner, clause.anchor);
        containsTerms(ownerSection, clause.terms, clause.id);
        if (clause.owner !== "AGENTS.md") {
            check(rootSection.includes(clause.owner),
                `${clause.id} owner is directly reachable from its root agreement`);
        }
        if (clause.disposition === "executable_guard") {
            check((await document(clause.guard)).length > 0, `${clause.id} guard is readable`);
        }
        if (clause.disposition === "scenario_evaluation") {
            check(scenarioSet.has(clause.scenario), `${clause.id} names a registered scenario`);
        }
    }
}

for (const scenario of coverage.scenarios) {
    exactKeys(scenario, ["id", "evaluation", "evidence"], `${scenario.id} has a closed schema`);
    check(typeof scenario.evaluation === "string" && scenario.evaluation.length > 0,
        `${scenario.id} states the behavior it evaluates`);
    check(Array.isArray(scenario.evidence) && scenario.evidence.length > 0,
        `${scenario.id} carries current-policy evidence`);
    check(clauses.some((clause) => clause.scenario === scenario.id),
        `${scenario.id} traces to at least one operative clause`);
    for (const item of scenario.evidence) {
        exactKeys(item, ["path", "anchor", "terms"], `${scenario.id} evidence is path-and-section scoped`);
        check(!archiveSections.has(`${item.path}\u0000${item.anchor}`),
            `${scenario.id} cannot use an archived-rationale section as current policy`);
        containsTerms(await exactSection(item.path, item.anchor), item.terms,
            `${scenario.id}:${item.path}:${item.anchor}`);
    }
}

// Scenario 1: real conflicts, empty nearest rules, and whole-nearest truncation.
function resolveRule(sources, key) {
    const candidates = mutation === "task-brief-loses" ? sources.slice(0, -1) : sources;
    return [...candidates].reverse().find((source) => Object.hasOwn(source.rules, key))?.rules[key];
}
const instructionSources = [
    { name: "global", rules: { deploy: "allowed", stage: "broad" } },
    { name: "repository", rules: { deploy: "root-only", stage: "named-paths" } },
    { name: "nearest-domain", rules: { deploy: "forbidden" } },
    { name: "task-brief", rules: { deploy: "task-authorized", tests: "static-only" } }
];
equal(resolveRule(instructionSources, "deploy"), "task-authorized",
    "the task brief wins a same-key conflict with the nearest-domain file");
equal(resolveRule([...instructionSources.slice(0, 2), { name: "nearest-domain", rules: {} }], "stage"),
    "named-paths", "an empty nearest-domain file adds no rule and repository policy remains applicable");
function truncateInstructions(base, nearest, limit) {
    if (nearest.length > limit) return { base: "", nearest: nearest.slice(0, limit), nearestTruncated: true };
    return { base: base.slice(0, Math.max(0, limit - nearest.length)), nearest, nearestTruncated: false };
}
equal(truncateInstructions("GLOBAL-REPOSITORY-RULES", "NEAREST-WHOLE", 18).nearest, "NEAREST-WHOLE",
    "a combined length limit retains the nearest-domain file whole");

// Scenario 2: the two shared-index commit traps point in opposite directions.
function sharedIndexDecision({ stageMode, foreignStaged, stagedDiffRead, commitMode, foreignUnstagedHunk }) {
    if (stageMode !== "named") return "refuse_broad_stage";
    if (foreignStaged) return "refuse_foreign_index";
    if (!stagedDiffRead) return "refuse_unreviewed_index";
    if (mutation !== "pathspec-commit-allowed" && commitMode === "pathspec" && foreignUnstagedHunk) {
        return "refuse_foreign_worktree_hunk";
    }
    return "allow_commit";
}
equal(sharedIndexDecision({ stageMode: "broad", foreignStaged: false, stagedDiffRead: true,
    commitMode: "no-pathspec", foreignUnstagedHunk: false }), "refuse_broad_stage",
"git add -A cannot enter the shared-index workflow");
equal(sharedIndexDecision({ stageMode: "named", foreignStaged: true, stagedDiffRead: true,
    commitMode: "no-pathspec", foreignUnstagedHunk: false }), "refuse_foreign_index",
"a no-pathspec commit must not sweep another Session's staged path");
equal(sharedIndexDecision({ stageMode: "named", foreignStaged: false, stagedDiffRead: false,
    commitMode: "no-pathspec", foreignUnstagedHunk: false }), "refuse_unreviewed_index",
"a named index is still refused until its staged diff is read");
equal(sharedIndexDecision({ stageMode: "named", foreignStaged: false, stagedDiffRead: true,
    commitMode: "pathspec", foreignUnstagedHunk: true }), "refuse_foreign_worktree_hunk",
"a pathspec commit must not absorb another Session's unstaged hunk");
equal(sharedIndexDecision({ stageMode: "named", foreignStaged: false, stagedDiffRead: true,
    commitMode: "no-pathspec", foreignUnstagedHunk: false }), "allow_commit",
"named staging plus a reviewed clean index reaches the no-pathspec commit boundary");

// Scenarios 3–6: authority, exact subjects, restart recovery, and landing ownership.
function taskSecretAllows(operation) {
    return new Set(["task-progress", "task-notify", "task-complete", "landing-pending"]).has(operation);
}
check(taskSecretAllows("task-complete"), "task secret may complete its own task");
check(taskSecretAllows("landing-pending"), "task secret may expose a pending landing obligation");
check(!taskSecretAllows("landing-landed"), "task secret cannot attest landed");
check(!taskSecretAllows("maintenance-restart"), "task secret cannot enter machine maintenance");
const verificationSubject = (role) => role === "child" ? "working-overlay"
    : role === "root" ? "exact-candidate-tree" : "none";
equal(verificationSubject("child"), "working-overlay", "child proves the overlay it wrote");
equal(verificationSubject("root"), "exact-candidate-tree", "root accepts the exact integrated candidate");
function restartAdmission({ state, sealedSecret }) {
    if (state === "spawning" || (state === "queued" && !sealedSecret)) return "blocked_task_secret";
    return "durable_not_blocking";
}
equal(restartAdmission({ state: "spawning", sealedSecret: true }), "blocked_task_secret",
    "spawning blocks replacement while plaintext briefing state is in flight");
equal(restartAdmission({ state: "queued", sealedSecret: false }), "blocked_task_secret",
    "queued work without a recoverable sealed secret blocks replacement");
equal(restartAdmission({ state: "queued", sealedSecret: true }), "durable_not_blocking",
    "recoverable queued work survives replacement");
equal(restartAdmission({ state: "briefed", sealedSecret: false }), "durable_not_blocking",
    "briefed work is durable and not rejected merely for being live");
function landingState({ delivered, reviewed, integrated, owner }) {
    return !delivered || !reviewed || !integrated ? { state: "pending", owner } : { state: "landed", owner };
}
equal(landingState({ delivered: true, reviewed: true, integrated: false, owner: "root-A" }),
    { state: "pending", owner: "root-A" }, "SAFE TO LAND remains root-A's pending obligation");

// Scenario 7: the canonical hosted document locator is one strict fragment contract.
const utf8 = new TextEncoder();
const taskID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
function stableIdentity(value) {
    return typeof value === "string" && value.length > 0 && utf8.encode(value).length <= 128
        && !/[\u0000-\u001f\u007f-\u009f]/.test(value);
}
function safeDocumentPath(value) {
    if (typeof value !== "string" || !value || utf8.encode(value).length > 512
        || value.startsWith("/") || /[\u0000-\u001f\u007f-\u009f]/.test(value)) return false;
    const parts = value.split("/");
    if (parts.length > 6 || parts.some((part) => !part || part.startsWith("."))) return false;
    return /\.(?:md|markdown|txt)$/i.test(parts.at(-1));
}
function cloudDocumentAccepted(urlText) {
    if (mutation === "accept-unsafe-cloud" && urlText.includes("token=secret")) return true;
    let url;
    try { url = new URL(urlText); } catch { return false; }
    if (url.protocol !== "https:" || url.hostname !== "app.clawdline.com" || url.port
        || url.username || url.password || url.pathname !== "/" || url.search || !url.hash) return false;
    const fields = new URLSearchParams(url.hash.slice(1));
    const scope = fields.get("scope");
    const wanted = scope === "task"
        ? ["document", "machine", "session", "scope", "task", "path"]
        : ["document", "machine", "session", "scope", "path"];
    const keys = [...fields.keys()];
    if (fields.get("document") !== "1" || keys.length !== wanted.length
        || wanted.some((key) => fields.getAll(key).length !== 1)
        || keys.some((key) => !wanted.includes(key))) return false;
    if (!stableIdentity(fields.get("machine")) || fields.get("machine") === "this-mac"
        || !stableIdentity(fields.get("session")) || !["project", "task"].includes(scope)
        || !safeDocumentPath(fields.get("path"))) return false;
    return scope !== "task" || taskID.test(fields.get("task"));
}
const validTask = "11111111-2222-4333-8444-555555555555";
check(cloudDocumentAccepted("https://app.clawdline.com/#document=1&machine=m-7&session=s-9&scope=project&path=artifacts%2Freport.md"),
    "canonical project document identity is accepted");
check(cloudDocumentAccepted(`https://app.clawdline.com/#document=1&machine=m-7&session=s-9&scope=task&task=${validTask}&path=report.txt`),
    "canonical task document identity is accepted");
for (const rejected of [
    "file:///tmp/report.md",
    "http://127.0.0.1:7717/report.md",
    "https://app.clawdline.com/document#document=1&machine=m&session=s&scope=project&path=report.md",
    "https://app.clawdline.com/?token=secret#document=1&machine=m&session=s&scope=project&path=report.md",
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=report.md&token=secret",
    "https://app.clawdline.com/#document=1&machine=this-mac&session=s&scope=project&path=report.md",
    "https://app.clawdline.com/#document=1&machine=m&session=&scope=project&path=report.md",
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=%2Fetc%2Fpasswd.md",
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=..%2Ftask.json",
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=a%2F%2Fb.md",
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=.secret.md",
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&path=report.html",
    `https://app.clawdline.com/#document=1&machine=m&session=s&scope=project&task=${validTask}&path=report.md`,
    "https://app.clawdline.com/#document=1&machine=m&session=s&scope=task&path=report.md",
    "https://app.clawdline.com/#document=1&machine=m&machine=m2&session=s&scope=project&path=report.md"
]) check(!cloudDocumentAccepted(rejected), `${rejected} is refused as non-canonical Cloud delivery`);

// Scenarios 8–9: one user attention and event-driven watchdog thresholds.
function userDecisionActions({ alreadyActive, notificationSent }) {
    return ["persist_owed", "show_one_options_prompt",
        ...(!alreadyActive && !notificationSent ? ["send_one_attention"] : [])];
}
equal(userDecisionActions({ alreadyActive: false, notificationSent: false }),
    ["persist_owed", "show_one_options_prompt", "send_one_attention"],
    "a predicted user blocker is durable and gets one attention request before waiting");
equal(userDecisionActions({ alreadyActive: true, notificationSent: false }),
    ["persist_owed", "show_one_options_prompt"], "an active user is not sent a duplicate alert");
function watchdog({ state, quietSeconds = 0, expectedSeconds = 0 }) {
    if (state === "result-tmp-valid") return "broker-finalizer";
    if (["queued", "spawning"].includes(state)) return quietSeconds >= 90 ? "compact-check" : "wait-event";
    if (["briefed", "working"].includes(state)) return quietSeconds >= 900 ? "compact-check" : "wait-event";
    if (["compile", "test"].includes(state)) {
        return quietSeconds >= expectedSeconds + 180 ? "compact-check" : "wait-event";
    }
    return "wait-event";
}
equal(watchdog({ state: "queued", quietSeconds: 89 }), "wait-event", "queued waits for events before 90s");
equal(watchdog({ state: "spawning", quietSeconds: 90 }), "compact-check", "spawning watchdog opens at 90s");
equal(watchdog({ state: "briefed", quietSeconds: 899 }), "wait-event", "healthy work waits before 15m");
equal(watchdog({ state: "working", quietSeconds: 900 }), "compact-check", "healthy watchdog opens at 15m");
equal(watchdog({ state: "compile", quietSeconds: 779, expectedSeconds: 600 }), "wait-event",
    "known compile waits its estimate plus three minutes");
equal(watchdog({ state: "test", quietSeconds: 780, expectedSeconds: 600 }), "compact-check",
    "known test becomes inspectable after estimate plus three minutes");
equal(watchdog({ state: "result-tmp-valid" }), "broker-finalizer",
    "valid result tmp belongs to the broker rather than an LLM poller");

// Scenario 10: marker/hash, stored secret, byte stability, observation span, and process epoch all bind trust.
function mayFinalizeTmp({ markerMatches, secretMatches, unchanged, observations, spanSeconds,
    processEpochMatches, ageSeconds }) {
    if (mutation === "trust-result-age") return ageSeconds >= 30;
    void ageSeconds;
    return markerMatches && secretMatches && unchanged && observations >= 2 && spanSeconds >= 30
        && processEpochMatches;
}
const stableTmp = { markerMatches: true, secretMatches: true, unchanged: true,
    observations: 2, spanSeconds: 30, processEpochMatches: true, ageSeconds: 31 };
check(mayFinalizeTmp(stableTmp), "bound stable bytes may finalize after the minimum observation span");
check(!mayFinalizeTmp({ ...stableTmp, markerMatches: false, ageSeconds: 3600 }),
    "marker or hash mismatch refuses finalization regardless of age");
check(!mayFinalizeTmp({ ...stableTmp, secretMatches: false, ageSeconds: 3600 }),
    "stored task-secret mismatch refuses finalization regardless of age");
check(!mayFinalizeTmp({ ...stableTmp, unchanged: false, ageSeconds: 3600 }),
    "changed marker or tmp bytes restart trust");
check(!mayFinalizeTmp({ ...stableTmp, observations: 1, ageSeconds: 3600 }),
    "file age is not a second observation");
check(!mayFinalizeTmp({ ...stableTmp, spanSeconds: 29, ageSeconds: 3600 }),
    "two observations less than 30 seconds apart are insufficient");
check(!mayFinalizeTmp({ ...stableTmp, processEpochMatches: false, ageSeconds: 3600 }),
    "a broker restart resets the stable observation interval");

// Scenario 11: same tuple reuses evidence; a second full needs typed environmental inconclusiveness.
function verificationPlan(previous, next) {
    const sameTuple = ["repository", "tree", "question", "command", "environment"]
        .every((key) => previous[key] === next[key]);
    if (sameTuple && previous.outcome === "pass") return "reuse";
    if (previous.kind === "full" && next.kind === "full"
        && previous.outcome !== "inconclusive_environment") return "refuse_repeat_full";
    return "run_changed_question";
}
const receipt = { repository: "r", tree: "t1", question: "q", command: "c", environment: "e",
    kind: "focused", outcome: "pass" };
equal(verificationPlan(receipt, { ...receipt }), "reuse", "identical passing tuple is reused");
equal(verificationPlan({ ...receipt, kind: "full" }, { ...receipt, kind: "full", tree: "t2" }),
    "refuse_repeat_full", "a green full is not repeated merely because the tree moved");
equal(verificationPlan({ ...receipt, kind: "full", outcome: "inconclusive_environment" },
    { ...receipt, kind: "full", tree: "t2" }), "run_changed_question",
    "typed inconclusive environment permits the candidate's one replacement full");

const [englishFinalizer, chineseFinalizer] = await Promise.all([
    exactSection("Resources/skill-guides/clawdline.md", "## 6. Report, wait, then close the root obligation"),
    exactSection("Resources/skill-guides/clawdline.zh-TW.md", "## 6. 回報、等完成、關閉 root 的責任")
]);
check(englishFinalizer.includes("two unchanged observations at least 30 seconds apart"),
    "English guide states the complete stable-finalizer interval semantics");
check(chineseFinalizer.includes("兩次未變觀察的間隔至少 30 秒"),
    "zh-TW guide states two stable observations at least 30 seconds apart");

console.log(`${checks} agent instruction topology checks passed`);
