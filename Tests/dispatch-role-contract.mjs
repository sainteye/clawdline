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

for (const file of ["skills/clawdline/SKILL.md", "skills/clawdline/SKILL.zh-TW.md"]) {
    const text = fs.readFileSync(path.join(root, file), "utf8");
    assert.ok(text.includes("POST /v1/orchestrator/detached-tasks"),
        `${file}: missing the dedicated unattended-automation route`);
    assert.ok(!text.includes('--argjson poll_only "${POLL_ONLY:-false}"'),
        `${file}: ordinary dispatch still exposes poll-only as a generic switch`);
}

console.log(`dispatch role contract: ${surfaces.length} surfaces, ${clauses.length} clauses`);
