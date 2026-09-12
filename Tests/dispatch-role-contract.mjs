import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const surfaces = [
    "AGENTS.md",
    "docs/dispatching.md",
    "docs/handoff.md",
    "Resources/dispatch-policy.md",
    "skills/clawdline/SKILL.md",
    "skills/clawdline/SKILL.zh-TW.md",
    "docs/clawdline-protocol.html",
];
const begin = "clawdline-dispatch-role-contract:v1";
const end = "/clawdline-dispatch-role-contract:v1";

const clauses = [
    ["owned child", /Owned child[\s\S]*?POST \/v1\/orchestrator\/tasks[\s\S]*?bounded[\s\S]*?retains synthesis, integration, and landing/i],
    ["handoff continuation", /Handoff[\s\S]*?POST \/v1\/orchestrator\/handoffs[\s\S]*?continuation[\s\S]*?REFERENCES[\s\S]*?VERIFICATION[\s\S]*?OPEN THREADS/i],
    ["detached automation", /Detached automation[\s\S]*?POST \/v1\/orchestrator\/detached-tasks[\s\S]*?root\.session_id[\s\S]*?null[\s\S]*?root\.poll_only[\s\S]*?true[\s\S]*?POST \/v1\/orchestrator\/tasks[\s\S]*?refus[\s\S]*?never[\s\S]*?Root[\s\S]*?Major Feature/i],
    ["root assignment", /Root Assignment \/ Feature Launch[\s\S]*?POST \/v1\/orchestrator\/root-assignments[\s\S]*?ordinary independent Root[\s\S]*?objective[\s\S]*?scope[\s\S]*?constraints[\s\S]*?relevant references[\s\S]*?acceptance[\s\S]*?durable machine-auth[\s\S]*?no child[\s\S]*?handoff[\s\S]*?detached[\s\S]*?timeout[\s\S]*?secret[\s\S]*?result[\s\S]*?parent[\s\S]*?landing lineage/i],
];

function contract(text, file) {
    const start = text.indexOf(begin);
    const finish = text.indexOf(end, start + begin.length);
    assert.ok(start >= 0 && finish > start, `${file}: missing closed dispatch-role contract v1`);
    return text.slice(start + begin.length, finish);
}

function validate(block, file) {
    for (const [name, pattern] of clauses) {
        assert.match(block, pattern, `${file}: missing ${name} semantics`);
    }
    assert.doesNotMatch(block, /Register (?:it|that Feature) detached/i,
        `${file}: detached task is still prescribed as a Feature owner`);
    assert.doesNotMatch(block, /Its task is detached/i,
        `${file}: detached task is still prescribed as a Feature owner`);
}

for (const file of surfaces) {
    const text = fs.readFileSync(path.join(root, file), "utf8");
    const block = contract(text, file);
    validate(block, file);
    assert.doesNotMatch(text, /Register (?:it|that Feature) detached/i,
        `${file}: detached Feature prescription survives outside the contract`);
    assert.doesNotMatch(text, /Its task is detached/i,
        `${file}: detached Feature prescription survives outside the contract`);

    // Prove the guard can go red for every semantic member, independent of the repository's
    // current bytes. A future edit that turns validation into presence-only cannot keep these.
    const mutations = ["Owned child", "continuation", "Detached automation",
        "Root Assignment / Feature Launch"];
    for (const phrase of mutations) {
        assert.throws(() => validate(block.replace(phrase, "removed"), `${file} mutation`),
            `${file}: removing ${phrase} must make the contract red`);
    }
    for (const phrase of ["timeout", "secret"]) {
        assert.throws(() => validate(block.replace(phrase, "removed"), `${file} mutation`),
            `${file}: removing Root Assignment ${phrase} exclusion must make the contract red`);
    }
}

function validateOperationalFinalizerVeto(text, file) {
    assert.match(text,
        /named\s+operational\s+finalizer[\s\S]*?workflow\s+explicitly\s+names[\s\S]*?downstream\s+root[\s\S]*?stops\s+after\s+the\s+commit[\s\S]*?exact-tree\s+verification\s+receipt/i,
        `${file}: missing the named downstream finalizer's landing-root stop point`);
    assert.match(text,
        /sends\s+that\s+exact\s+commit\s+and\s+receipt[\s\S]*?named\s+owner[\s\S]*?must\s+not\s+build,\s+restart,\s+or\s+deploy/i,
        `${file}: missing the exact commit/receipt handoff or duplicate-side-effect veto`);
    assert.match(text,
        /ordinary\s+landing\s+root[\s\S]*?only\s+when\s+no\s+downstream\s+finalizer\s+was\s+named/i,
        `${file}: missing the ordinary landing-root default`);
    assert.match(text,
        /transport\s+acceptance[\s\S]*?only[\s\S]*?handoff\s+was\s+sent[\s\S]*?explicit\s+acknowledgement[\s\S]*?re-read[\s\S]*?named\s+owner[\s\S]*?acknowledge[\s\S]*?before[\s\S]*?live/i,
        `${file}: transport acceptance is still allowed to masquerade as an acknowledged handoff`);
}

const agents = fs.readFileSync(path.join(root, "AGENTS.md"), "utf8");
const finalizerStart = agents.indexOf("**One named operational finalizer owns the side effects.**");
const finalizerFinish = agents.indexOf("\n- ", finalizerStart);
assert.ok(finalizerStart >= 0 && finalizerFinish > finalizerStart,
    "AGENTS.md: missing the closed operational-finalizer rule");
const finalizerRule = agents.slice(finalizerStart, finalizerFinish);
validateOperationalFinalizerVeto(finalizerRule, "AGENTS.md");
for (const [phrase, pattern] of [
    ["stops after the commit", /stops after the\s+commit/],
    ["sends that exact commit and receipt", /sends that exact commit and receipt/],
    ["must not build, restart, or deploy", /must not build, restart, or deploy/],
    ["explicit acknowledgement", /explicit\s+acknowledgement/],
]) {
    assert.throws(
        () => validateOperationalFinalizerVeto(finalizerRule.replace(pattern, "removed"),
            "AGENTS.md mutation"),
        `AGENTS.md: removing "${phrase}" must make the operational-finalizer guard red`);
}

const skillTriggers = [
    ["skills/clawdline/SKILL.md", "another live session"],
    ["skills/clawdline/SKILL.zh-TW.md", "另一個 live session"],
];
for (const [file, destination] of skillTriggers) {
    const text = fs.readFileSync(path.join(root, file), "utf8");
    const frontmatter = text.split("---")[1]?.toLowerCase() ?? "";
    assert.ok(frontmatter.includes(destination.toLowerCase()),
        `${file}: missing cross-session destination trigger`);
    for (const trigger of ["send", "message", "report", "status", "finding", "coordination"]) {
        assert.ok(frontmatter.includes(trigger), `${file}: missing ${trigger} trigger`);
    }
}

// A session working in another repository has no `AGENTS.md` of this project's, so the only thing
// that can tell it the completed-turn receipt exists is this stub's own trigger list — and until
// 2026-09-05 that list was entirely about handing work *out*. A root elsewhere on this Mac finished
// its turn and had nothing to make it look. So the trigger is held here, in both spellings, and the
// stub is held to pointing at the guide's section rather than copying its commands, which is the
// failure mode the 140-line ceiling in `Tests/web-clawdfather.mjs` guards from the other side.
for (const [file] of skillTriggers) {
    const text = fs.readFileSync(path.join(root, file), "utf8");
    const frontmatter = text.split("---")[1]?.toLowerCase() ?? "";
    assert.ok(frontmatter.includes("milestone"),
        `${file}: nothing in the trigger list would make a session in another repository look up `
        + `the completed-turn receipt`);
    // Whitespace-normalised on purpose: the subject is whether the stub names that section, not
    // whether a re-wrap happened to leave it on one line. Asked with a bare `includes` this went
    // red the first time it ran, on a line break rather than on a missing pointer.
    assert.ok(text.replace(/\s+/g, " ").includes("Report the root's completed turn"),
        `${file}: the body no longer names the guide section that carries the receipt`);
    assert.ok(!text.includes("/complete"),
        `${file}: the stub has copied the receipt route instead of pointing at the guide`);
}

// The same shape, the same day, a second clause. On 2026-09-05 a session that had just taken four
// screenshots *for the person* read this list, saw handing work out and a delivery receipt,
// correctly concluded neither was it, and ended its turn pasting `/var/folders/…` paths at someone
// reading on a phone. It could not have known: until now nothing in the description said a picture
// was one of the things this route carries. A trigger that reaches a reader who is not dispatching
// is the whole mechanism, so it is held here in both spellings — and held to going red when
// removed, because the first clause above was lost exactly once by a rewrite that meant no harm.
const imageTriggers = [
    ["skills/clawdline/SKILL.md", ["local image", "screenshot"]],
    ["skills/clawdline/SKILL.zh-TW.md", ["本機圖片", "截圖"]],
];
const carriesImageTrigger = (text, phrase, file) => {
    const frontmatter = text.split("---")[1]?.toLowerCase() ?? "";
    assert.ok(frontmatter.includes(phrase.toLowerCase()),
        `${file}: nothing in the trigger list would make a session that just took a screenshot `
        + `look up how to put it in front of the person`);
};
for (const [file, phrases] of imageTriggers) {
    const text = fs.readFileSync(path.join(root, file), "utf8");
    for (const phrase of phrases) {
        carriesImageTrigger(text, phrase, file);
        assert.throws(() => carriesImageTrigger(text.replaceAll(phrase, "removed"), phrase, file),
            `${file}: removing the "${phrase}" trigger must make this red`);
    }
}

for (const file of ["skills/clawdline/SKILL.md", "skills/clawdline/SKILL.zh-TW.md"]) {
    const text = fs.readFileSync(path.join(root, file), "utf8");
    assert.ok(text.includes("POST /v1/orchestrator/detached-tasks"),
        `${file}: missing the dedicated unattended-automation route`);
    assert.ok(!text.includes('--argjson poll_only "${POLL_ONLY:-false}"'),
        `${file}: ordinary dispatch still exposes poll-only as a generic switch`);
}

// The landing-queue concurrency rule, in four documents and four voices.
//
// Clawdfather ruled on 2026-09-12 that an empty `contended_paths` releases preparation and not the
// commit, after a root had waited for the queue holder to land while sharing no path with it: the
// waiting cost a night and bought nothing, and the half that did need waiting — the ref update and
// the shared index — was the half nothing in the documents had said out loud.
//
// A guard that pinned one identical sentence in four files would be the wrong shape twice over: it
// would pass while all four said something false, and it would force four voices into one paste.
// So each document is asked for the claim's own nouns — the contention condition, preparation
// happening at once, the ref update, the shared checkout, one at a time — and may say them however
// its own reader is being spoken to. Removing any one of them from any one document is red, and
// that is proved below against the bytes in the tree rather than asserted.
const queueConcurrencySections = [
    ["docs/landing.md", "## The landing queue, when more than one line is waiting", /\n## /],
    ["docs/dispatching.md", "### Coordinate only on an observed collision", /\n#{1,4} /],
    ["docs/api.md", "### `GET /v1/orchestrator/landing-queue`", /\n### /],
    ["docs/clawdline-protocol.html", '<h3 id="landing-queue">', /<h2 /],
];

const queueConcurrencyClauses = [
    ["which entries this is about: the ones with no contention between them", [
        /contended_paths/i, /no declared (?:path )?overlap/i, /no path in common/i,
    ]],
    ["that they may prepare at the same time", [
        /prepar\w*[\s\S]{0,300}?(?:in parallel|at the same time|side by side|simultaneous)/i,
        /(?:in parallel|at the same time|side by side|simultaneous)[\s\S]{0,300}?prepar\w*/i,
    ]],
    ["that the ref update on the target branch is the step that is not parallel", [
        /updat\w*[\s\S]{0,80}?\bref\b[\s\S]{0,120}?target branch/i,
    ]],
    ["that the shared checkout's index is the other half of that step", [
        /shared (?:checkout|index|tree)/i,
    ]],
    ["that those two happen one line at a time", [
        /one (?:line|entry|root|delivery) at a time/i, /one at a time/i, /serializ\w+/i,
    ]],
];

// Tags become a space rather than nothing: the protocol page writes half of this rule inside
// `<strong>`, and a reader of the rendered page sees words, not markup. Whitespace is collapsed for
// the reason the skill-stub assertion above collapses it — the subject is whether the document
// carries the claim, not where a re-wrap happened to break the line.
const readable = (text) => text.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");

function queueConcurrencySection(text, [file, opening, closing]) {
    const start = text.indexOf(opening);
    assert.ok(start >= 0, `${file}: missing the landing-queue section this rule lives in`);
    const rest = text.slice(start + opening.length);
    const end = rest.search(closing);
    return readable(end < 0 ? rest : rest.slice(0, end));
}

function validateQueueConcurrency(section, file) {
    for (const [claim, alternatives] of queueConcurrencyClauses) {
        assert.ok(alternatives.some((pattern) => pattern.test(section)),
            `${file}: the landing-queue rule no longer says ${claim}`);
    }
}

for (const section of queueConcurrencySections) {
    const [file] = section;
    const scoped = queueConcurrencySection(fs.readFileSync(path.join(root, file), "utf8"), section);
    validateQueueConcurrency(scoped, file);

    // Red-ability, per document and per clause, measured rather than claimed: strike every spelling
    // this guard would accept for one clause out of that document's own section, and the guard must
    // fail. A future rewrite that softened this into presence-only could not keep these passing.
    for (const [claim, alternatives] of queueConcurrencyClauses) {
        let emptied = scoped;
        for (const pattern of alternatives) {
            emptied = emptied.replace(new RegExp(pattern.source, pattern.flags + "g"), " ");
        }
        assert.throws(() => validateQueueConcurrency(emptied, `${file} mutation`),
            `${file}: dropping "${claim}" must make the landing-queue guard red`);
    }
}

console.log(`dispatch role contract: ${surfaces.length} surfaces, ${clauses.length} clauses; `
    + "operational finalizer: 4 clauses; landing-queue concurrency: "
    + `${queueConcurrencySections.length} documents, ${queueConcurrencyClauses.length} clauses`);
