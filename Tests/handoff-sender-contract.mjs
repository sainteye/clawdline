import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// `POST /v1/orchestrator/handoffs` grew nine typed refusals when it started requiring a sender,
// and four surfaces were written to describe them. Nothing compared any of the four with the code,
// and within one review three of them were false: a worked example that omitted the required
// field, a sentence counting five codes where the delivery's own check says four, and both shipped
// skill guides still teaching that an unrecognised sender is the same as an absent one — the
// sentence the sender of the 2026-09-04 handoff read.
//
// Two documents agreeing is not evidence, and here it actively misled: `docs/api.md`'s prose and
// `docs/handoff.md`'s prose agreed with each other while `docs/handoff.md`'s prose contradicted
// its own example eighty lines up. So this guard derives the closed set of refusals out of
// `Sources/OrchestratorHandoffSender.swift` — the one file that emits them — and holds every
// surface to that set. A copy that goes stale goes red; two copies that go stale together still go
// red; and deleting a refusal turns the documents red rather than leaving them describing
// something that is gone. It is the same shape as `Tests/attached-follow-up-contract.mjs`, which
// this route should have had from the day it shipped.
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (file) => fs.readFileSync(path.join(root, file), "utf8");
const sorted = (set) => [...set].sort();

// Every refusal this file returns, in both spellings the tree writes them: `.refused(400, "…"` and
// `.refused(status: 409, code: "…"`.
const refusalCall = /\.refused\(\s*(?:status:\s*)?\d+\s*,\s*(?:code:\s*)?"([a-z_]+)"/g;
const senderSource = read("Sources/OrchestratorHandoffSender.swift");
const codes = new Set([...senderSource.matchAll(refusalCall)].map((match) => match[1]));

// Calibrate before believing the set, twice over. A scan that stops recognising how this tree
// refuses returns fewer codes, and every "the surface names them all" below becomes trivially
// satisfiable — a guard that stopped matching reads exactly like a guard that passed.
assert.ok(codes.size >= 9,
    `the refusal scan found ${codes.size} codes in Sources/OrchestratorHandoffSender.swift and `
    + `this route emits nine; the pattern has stopped recognising how the sender contract refuses, `
    + `so the sets it would compare below mean nothing`);
for (const anchor of ["from_session_required", "succession_required"]) {
    assert.ok(codes.has(anchor), `the scan reaches ${anchor}`);
}

// The surfaces are searched by family rather than by a list, so a tenth refusal is caught by the
// same comparison as a renamed one. That only works while every emitted code is in the family, so
// that is asserted rather than assumed: a future `crown_required` would otherwise be silently
// outside every check below.
const family = /^(?:from_session_|sender_|coordinator_|succession_)/;
for (const code of codes) {
    assert.match(code, family,
        `${code} is emitted by the sender contract and matches none of the prefixes this guard `
        + `searches the documents for, so no surface below is being held to it`);
}
// Names that match the family and are not refusals of this route: `coordinator_plain_handoff` is a
// request field, `coordinator_id` and `sender_session_id` are body fields of the
// `succession_required` refusal, and `coordinator_online`/`coordinator_exists` belong to the
// registration routes. `Tests/attached-follow-up-contract.mjs` filters `attach_session` and
// `attach_session_id` for the same reason. An exclusion that ever covered a real refusal would
// hide it from every comparison below, so the two sets are asserted disjoint.
const notARefusal = new Set(["coordinator_plain_handoff", "coordinator_id", "coordinator_online",
                             "coordinator_exists", "sender_session_id"]);
for (const name of notARefusal) {
    assert.ok(!codes.has(name),
        `${name} is excluded from the document scan as a field name, and this route now emits it `
        + `as a refusal; no surface would be held to it`);
}
const namedIn = (text) => new Set((text.match(/`[a-z_]+`/g) ?? [])
    .map((span) => span.slice(1, -1))
    .filter((name) => family.test(name) && !notARefusal.has(name)));

// A markdown section, from its heading to the next one at the same level or above.
function section(text, heading, file) {
    const start = text.indexOf(heading);
    assert.ok(start >= 0, `${file}: no ${heading}`);
    const rest = text.slice(start + heading.length);
    const next = rest.search(/\n#{1,3} /);
    return next < 0 ? rest : rest.slice(0, next);
}

function delimited(text, name, file) {
    const begin = `<!-- ${name} -->`;
    const end = `<!-- /${name} -->`;
    const start = text.indexOf(begin);
    const finish = text.indexOf(end, start + begin.length);
    assert.ok(start >= 0 && finish > start, `${file}: missing closed ${name} block`);
    return text.slice(start + begin.length, finish);
}

// `docs/api.md` owns the reference table every other surface points at: one row per refusal, and
// the row carries the status, so a code moved between 400 and 409 in the Swift and not in the
// table is not this guard's business but a missing row is.
function apiTable(text) {
    const tabled = new Set();
    for (const line of section(text, "\n### `POST /v1/orchestrator/handoffs`", "docs/api.md")
        .split("\n")) {
        const row = line.match(/^\| `([a-z_]+)` \| \d{3} \|/);
        if (row && family.test(row[1])) tabled.add(row[1]);
    }
    return tabled;
}

// `docs/handoff.md` pairs codes that share a status onto one row (`a` · `b`), so its table is read
// as the set of code spans in it rather than one per line.
function handoffTable(text) {
    const rows = section(text, "\n### The refusals", "docs/handoff.md")
        .split("\n").filter((line) => /^\| `/.test(line)).join("\n");
    return namedIn(rows);
}

// Each surface says where its set lives and how a spurious refusal would be written into it — a
// table row here, a bullet inside a delimited block there. The mutation has to be shaped like the
// surface, or "naming a code the route cannot emit goes red" is proved somewhere the reader would
// never have put it.
const guideBlock = (file) => (text) =>
    namedIn(delimited(text, "clawdline-handoff-sender-contract:v1", file));
const guideInvention = (text) =>
    text.replace("<!-- /clawdline-handoff-sender-contract:v1 -->",
                 "- `sender_invented_code` (409) — a refusal nothing emits.\n"
                 + "<!-- /clawdline-handoff-sender-contract:v1 -->");
const rowInvention = (invent) => (text) => text.split("\n").flatMap((line) =>
    line.startsWith("| `succession_required`") ? invent(line) : [line]).join("\n");

const surfaces = [
    ["docs/api.md", apiTable,
     rowInvention((line) => [line, "| `sender_invented_code` | 409 | nothing emits it | |"])],
    ["docs/handoff.md", handoffTable,
     rowInvention((line) => [line.replace("| `succession_required`",
                                          "| `succession_required` · `sender_invented_code`")])],
    ["Resources/skill-guides/clawdline.md",
     guideBlock("Resources/skill-guides/clawdline.md"), guideInvention],
    ["Resources/skill-guides/clawdline.zh-TW.md",
     guideBlock("Resources/skill-guides/clawdline.zh-TW.md"), guideInvention],
];

for (const [file, extract, invent] of surfaces) {
    const text = read(file);
    assert.deepEqual(sorted(extract(text)), sorted(codes),
        `${file}: the sender refusals it names are not the set `
        + `Sources/OrchestratorHandoffSender.swift emits`);

    // Prove this guard can go red on this file's own bytes, for every member it checks. A future
    // edit that turns the comparison into presence-only cannot keep these passing.
    for (const code of codes) {
        assert.throws(() => assert.deepEqual(sorted(extract(text.replaceAll(code, "removed_here"))),
                                             sorted(codes)),
                      `${file}: dropping ${code} must go red`);
    }
    const invented = invent(text);
    assert.notEqual(invented, text, `${file}: the invented-refusal mutation changed nothing`);
    assert.throws(() => assert.deepEqual(sorted(extract(invented)), sorted(codes)),
                  `${file}: naming a refusal this route cannot emit must go red`);
}

// What the two shipped guides used to say, which is the reason this file exists. They are the
// operating manual an assistant reads before it sends a handoff, and both of them told the sender
// that naming nobody was fine.
const retired = [
    [/unrecognised is the same as absent/i, "an unrecognised sender is the same as an absent one"],
    [/認不出來就等於沒填/, "an unrecognised sender is the same as an absent one"],
    [/Both are best-effort/i, "`from_session` is best-effort"],
    [/兩個都是 best-effort/, "`from_session` is best-effort"],
    [/a refusal spends a slot of it/i, "a sender refusal spends a slot of the brake"],
    [/被擋掉也照吃一格/, "a sender refusal spends a slot of the brake"],
];
for (const file of ["Resources/skill-guides/clawdline.md",
                    "Resources/skill-guides/clawdline.zh-TW.md"]) {
    const text = read(file);
    for (const [pattern, claim] of retired) {
        assert.doesNotMatch(text, pattern, `${file}: still teaches that ${claim}`);
        assert.throws(
            () => assert.doesNotMatch(`${text}\n${pattern.source.replace(/\\/g, "")}\n`, pattern),
            `${file}: the retired claim must go red if it comes back`);
    }
}

// The single-page protocol document is prose with no table, so what it owes is one sentence: that
// the sending session is not optional. It said the opposite until the review found it.
const html = read("docs/clawdline-protocol.html");
const route = html.slice(html.indexOf("<code>POST /v1/orchestrator/handoffs</code> takes"));
const paragraph = route.slice(0, route.indexOf("</p>"));
assert.ok(paragraph.length > 0, "docs/clawdline-protocol.html: no handoff route paragraph");
const optionalSender = /optionally[^.]*the name of the sending session/i;
assert.doesNotMatch(paragraph, optionalSender,
    "docs/clawdline-protocol.html still calls the sending session optional");
assert.match(paragraph, /required/,
    "docs/clawdline-protocol.html no longer says the sending session is required");
assert.throws(
    () => assert.doesNotMatch(
        "takes the handoff id and optionally an assistant and the name of the sending session.",
        optionalSender),
    "the protocol-page check must go red on the sentence it retired");

// And the contract itself is still there. If the sender requirement is ever removed, this fails
// here rather than leaving four surfaces confidently describing a rule that is gone.
assert.match(senderSource, /static func handoffSenderVerdict\(/,
    "Sources/OrchestratorHandoffSender.swift no longer decides who sent a handoff");
assert.match(read("Sources/Orchestrator.swift"), /handoffSenderVerdict\(obj, evidence: sender\(\)\)/,
    "Sources/Orchestrator.swift no longer asks the sender contract anything");

console.log(`handoff sender contract: ${surfaces.length} surfaces, ${codes.size} typed refusals `
    + `derived from Sources/OrchestratorHandoffSender.swift`);
