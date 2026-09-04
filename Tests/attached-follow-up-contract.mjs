import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// `docs/dispatching.md` and both shipped skill guides spent seven days telling every root that
// attached follow-up tasks were "not in HEAD yet" and to approximate them with one ordinary task
// per emptied pool. The mechanism landed on 2026-08-28 (`2f6f0a1a`, `26388e3f`, `1b7406e5`); the
// sentence was still there on 2026-09-04, because a paragraph describing an *absence* has nothing
// in it that goes red when the absence ends.
//
// So this guard does not compare the three copies against each other. This repository has already
// been bitten by that shape — two records agreeing is not evidence, and the comparison that
// produced the agreement could not have produced anything else. What it compares them against is
// `Sources/`: the closed set of `attach_*` refusals the broker can actually emit is scanned out of
// the Swift, and every surface is held to exactly that set. A copy that goes stale goes red; two
// copies that go stale together still go red; and deleting the feature turns the docs red rather
// than leaving them describing something that is gone.
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (file) => fs.readFileSync(path.join(root, file), "utf8");

// The prose surfaces that carry the delimited contract block. `docs/api.md` is not one of them:
// it owns the reference table, and is checked as the table below.
const surfaces = [
    "docs/dispatching.md",
    "Resources/skill-guides/clawdline.md",
    "Resources/skill-guides/clawdline.zh-TW.md",
];
const begin = "clawdline-attached-follow-up:v1";
const end = "/clawdline-attached-follow-up:v1";

// Both spellings the broker refuses with: `.refused(status: 409, code: "…"` in the draft
// validator and `.refused(409, "…"` in the dispatcher.
const refusalCall = /\.refused\(\s*(?:status:\s*)?\d+\s*,\s*(?:code:\s*)?"(attach_[a-z_]+)"/g;

function emittedCodes(sources) {
    const found = new Set();
    for (const [file, text] of sources) {
        void file;
        for (const match of text.matchAll(refusalCall)) found.add(match[1]);
    }
    return found;
}

const sourceFiles = fs.readdirSync(path.join(root, "Sources"))
    .filter((name) => name.endsWith(".swift"))
    .map((name) => [`Sources/${name}`, read(`Sources/${name}`)]);
const codes = emittedCodes(sourceFiles);

// Calibrate before believing the set. If the spelling of a typed refusal ever changes, this scan
// silently returns fewer codes, every "the surface names them all" check below becomes trivially
// satisfiable, and a guard that stopped matching reads exactly like a guard that passed.
assert.ok(codes.size >= 5,
    `the refusal scan found ${codes.size} attach_* codes in Sources/*.swift and this path emits `
    + `seven; the pattern has stopped recognising how this tree refuses an attachment, so the `
    + `sets it would compare below mean nothing`);
assert.ok(codes.has("attach_not_managed") && codes.has("attach_delivery_failed"),
    "the scan reaches both the draft validator and the dispatcher");

const sorted = (set) => [...set].sort();
const namedIn = (text) => new Set((text.match(/attach_[a-z_]+/g) ?? [])
    .filter((name) => name !== "attach_session" && name !== "attach_session_id"));

function contractBlock(text, file) {
    const start = text.indexOf(begin);
    const finish = text.indexOf(end, start + begin.length);
    assert.ok(start >= 0 && finish > start,
        `${file}: missing closed attached-follow-up contract v1`);
    return text.slice(start + begin.length, finish);
}

// What a surface owes: it says the mechanism is dispatchable (the field's name), and it names
// every refusal that can stop it — no more, no fewer.
function validate(block, file) {
    assert.match(block, /attach_session/, `${file}: does not name the field that attaches a task`);
    assert.deepEqual(sorted(namedIn(block)), sorted(codes),
        `${file}: the typed refusals it names are not the set Sources/*.swift emits`);
    // The claim this whole guard exists to keep from coming back.
    assert.doesNotMatch(block, /Until that mechanism/i,
        `${file}: still says the attach mechanism has not landed`);
    assert.doesNotMatch(block, /honest approximation/i,
        `${file}: still prescribes the pre-landing approximation`);
    assert.doesNotMatch(block, /機制進\s*`?HEAD`?\s*之前/,
        `${file}: still says the attach mechanism has not landed`);
}

for (const file of surfaces) {
    const text = read(file);
    const block = contractBlock(text, file);
    validate(block, file);
    // Outside the block too: the sentence used to live in ordinary prose, and that is where it
    // would come back.
    assert.doesNotMatch(text, /Until that mechanism/i,
        `${file}: the pre-landing claim survives outside the contract`);
    assert.doesNotMatch(text, /honest approximation/i,
        `${file}: the pre-landing approximation survives outside the contract`);

    // Prove this guard can go red, on this file's own bytes, for every member it checks — a
    // future edit that turns validation into presence-only cannot keep these passing.
    assert.throws(() => validate(block.replace(/attach_session/g, "removed"), `${file} mutation`),
        `${file}: removing the attach field must go red`);
    for (const code of codes) {
        assert.throws(
            () => validate(block.replaceAll(code, "attach_removed_here"), `${file} mutation`),
            `${file}: dropping ${code} must go red`);
    }
    assert.throws(
        () => validate(`${block}\nAlso refused: attach_invented_code.`, `${file} mutation`),
        `${file}: naming a refusal the broker cannot emit must go red`);
    assert.throws(
        () => validate(`${block}\nUntil that mechanism is in HEAD, approximate it.`,
                       `${file} mutation`),
        `${file}: the old pre-landing sentence must go red if it returns`);
}

// `docs/api.md` carries the reference table every surface above points at, so it is held to the
// same derived set — one table row per refusal the broker can emit.
const api = read("docs/api.md");
const tabled = new Set();
for (const line of api.split("\n")) {
    const row = line.match(/^\| `(attach_[a-z_]+)` \| \d+ \|/);
    if (row) tabled.add(row[1]);
}
assert.deepEqual(sorted(tabled), sorted(codes),
    "docs/api.md's dispatch refusal table is not the set Sources/*.swift emits");

// And the feature itself is still there. If it is ever removed, this fails here rather than
// leaving three surfaces confidently describing a route that is gone.
assert.match(read("Sources/Orchestrator.swift"), /func spawnAttached\(/,
    "Sources/Orchestrator.swift no longer delivers an attached task");

console.log(`attached follow-up contract: ${surfaces.length} surfaces, `
    + `${codes.size} typed refusals derived from Sources/*.swift`);
