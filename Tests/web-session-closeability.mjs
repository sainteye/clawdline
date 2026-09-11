import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { T as fallbackStrings } from "../Resources/web/app/js/core/i18n.js";

const deriveURL = new URL("../Resources/web/app/js/view/derive.js", import.meta.url);
const deriveSource = await readFile(deriveURL, "utf8");
const standalone = deriveSource
    .replace('import { T, fill } from "../core/i18n.js";',
        'const T = globalThis.__closeabilityStrings;\n' +
        'const fill = (s, vars) => s.replace(/\\{(\\w+)\\}/g, (m, k) => ' +
        'vars && k in vars ? vars[k] : m);')
    .replace('import { S } from "../core/state.js";', 'const S = globalThis.__closeabilityState;')
    .replace('import { renderList } from "./list.js";', 'const renderList = function () {};');
globalThis.__closeabilityState = { sessions: [], tasks: [], filter: "" };
globalThis.__closeabilityStrings = {
    closeabilitySafe: "Safe to close",
    closeabilityBlocked: "1 obligation remains\u001f{n} obligations remain",
    closeabilityNeedsAttestation: "Waiting for session attestation",
    closeabilityUnknown: "Closeability unknown",
    closeabilityWhy: "Not closeable because",
    closeabilityMoverSelf: "this session moves it",
    closeabilityMoverPerson: "you move it",
    closeabilityMoverSession: "another session moves it",
    closeabilityMoverBroker: "a fresh reading moves it",
    closeabilityNotProven: "The broker could not prove this session is safe to close."
};
const derive = await import("data:text/javascript;base64," +
    Buffer.from(standalone).toString("base64"));

const project = derive.projectSessionCloseability;
const html = derive.sessionCloseabilityHTML;

function block(over) {
    return Object.assign({
        state: "safe",
        reasons: [],
        observed_at: 1788000000,
        session_generation: 41,
        activity_generation: 7,
        obligation_generation: 11,
        version: "cl1_2f9a4c31d0be5a7788c1e6b04d3f9021",
        provenance: ["broker", "self"],
        attestation_id: "attestation-1",
        mover: null,
        source: { provenance: "session_watch", freshness: "current" }
    }, over || {});
}
const obligation = {
    code: "pending_landing_owned", kind: "obligation", subject_kind: "task",
    subject_id: "task-1", mover: { kind: "session", self: true, person_needed: false }
};
const attestationMissing = {
    code: "attestation_missing", kind: "attestation", subject_kind: "session",
    subject_id: "TAB", mover: { kind: "session", self: true, person_needed: false }
};
const staleEvidence = {
    code: "session_inventory_stale", kind: "evidence",
    mover: { kind: "broker", person_needed: false }
};

// The closed four-state set, each reached by exactly the frame its contract names.
assert.equal(project({ state: "idle", closeability: block() }).state, "safe",
    "a current, attested, reasonless frame is the one way to safe");
assert.equal(project({ state: "idle", closeability: block({
    state: "blocked", attestation_id: null, reasons: [obligation] }) }).state, "blocked",
    "an obligation with its kind spelled is blocked");
assert.equal(project({ state: "idle", closeability: block({
    state: "needs_attestation", attestation_id: null,
    reasons: [attestationMissing] }) }).state, "needs_attestation",
    "a missing attestation asks the session");
assert.equal(project({ state: "idle", closeability: block({
    state: "unknown", attestation_id: null, reasons: [staleEvidence] }) }).state, "unknown",
    "an evidence problem is unknown");

// Fail closed. Every one of these is a frame that says safe and must not be drawn as safe.
const refused = [
    ["a frame with no closeability at all", { state: "idle" }],
    ["a state this client does not know", { state: "idle",
        closeability: block({ state: "closed" }) }],
    ["a safe claim carrying a reason", { state: "idle",
        closeability: block({ reasons: [obligation] }) }],
    ["a safe claim with no attestation behind it", { state: "idle",
        closeability: block({ attestation_id: null }) }],
    ["a safe claim read off a stale inventory", { state: "idle",
        closeability: block({ source: { provenance: "session_watch", freshness: "stale" } }) }],
    ["a safe claim read off no inventory", { state: "idle",
        closeability: block({ source: { provenance: "session_watch", freshness: "missing" } }) }],
    ["a safe claim with no version to compare", { state: "idle",
        closeability: block({ version: "" }) }],
    ["a safe claim on a row this page is drawing as working", { state: "working",
        closeability: block() }],
    ["a safe claim on a row showing a question", { state: "waiting", closeability: block() }],
    ["a safe claim on a screen that could not be read", { state: "unknown",
        closeability: block() }],
    ["blocked without a single obligation to point at", { state: "idle",
        closeability: block({ state: "blocked", attestation_id: null,
                              reasons: [staleEvidence] }) }],
    ["needs_attestation while an obligation is still standing", { state: "idle",
        closeability: block({ state: "needs_attestation", attestation_id: null,
                              reasons: [attestationMissing, obligation] }) }],
    ["needs_attestation with nothing to attest", { state: "idle",
        closeability: block({ state: "needs_attestation", attestation_id: null,
                              reasons: [] }) }],
    ["a reasons list that is not a list", { state: "idle",
        closeability: block({ state: "blocked", attestation_id: null, reasons: "two" }) }]
];
for (const [name, session] of refused) {
    const projected = project(session);
    assert.equal(projected.state, "unknown", name + " fails closed to unknown");
    assert.equal(projected.failedClosed, true, name + " says it failed closed");
}

// The CAS token only leaves the page when the page itself agrees the frame is safe.
assert.equal(derive.closeabilityVersion({ state: "idle", closeability: block() }),
    "cl1_2f9a4c31d0be5a7788c1e6b04d3f9021",
    "a proven frame hands its version to the close request");
assert.equal(derive.closeabilityVersion({ state: "working", closeability: block() }), null,
    "a frame that failed the client projection sends no token, and gets the old gate");
assert.equal(derive.closeabilityVersion({ state: "idle" }), null,
    "and a frame with no projection at all sends none either");

// The reason lines: machine code, the subject it is about, and who moves it.
const lines = derive.closeabilityLines({ state: "idle", closeability: block({
    state: "blocked", attestation_id: null,
    reasons: [obligation,
              { code: "coordination_wait_waiting", kind: "obligation", subject_kind: "wait",
                subject_id: "wait-9",
                mover: { kind: "session", self: false, session_id: "OWNER-TAB",
                         person_needed: false } },
              { code: "owed_decision", kind: "obligation", subject_kind: "session",
                subject_id: "TAB", mover: { kind: "person", person_needed: true } }] }) });
assert.deepEqual(lines, [
    "pending_landing_owned · task-1 · this session moves it",
    "coordination_wait_waiting · wait-9 · another session moves it",
    "owed_decision · TAB · you move it"
], "each line names the code, the object, and the one thing that clears it");

// The badge.
const safeBadge = html({ state: "idle", closeability: block() });
assert.match(safeBadge, /data-closeability="safe"/,
    "the badge carries its state as an attribute the CSS and the tests can both read");
assert.match(safeBadge, /Safe to close/, "and says so in words");
const blockedBadge = html({ state: "idle", closeability: block({
    state: "blocked", attestation_id: null, reasons: [obligation, obligation],
    mover: { kind: "session", self: true, person_needed: false } }) });
assert.match(blockedBadge, /2 obligations remain/,
    "the count is the obligations, filled into the reader's own sentence");
assert.match(blockedBadge,
    /class="session-status-glyph" aria-hidden="true">🔒<\/span><span class="session-status-label">/,
    "the lock has its own decorative line box, separate from the readable closeability copy");
assert.match(blockedBadge, /title="2 obligations remain · this session moves it"/,
    "and the full sentence, with its mover, stays reachable when the row ellipsises it");
const oneBlockedBadge = html({ state: "idle", closeability: block({
    state: "blocked", attestation_id: null, reasons: [obligation],
    mover: { kind: "session", self: true, person_needed: false } }) });
assert.match(oneBlockedBadge, /1 obligation remains/,
    "the ordinary one-obligation case uses the real singular renderer string");
assert.doesNotMatch(oneBlockedBadge, /1 obligations/,
    "the singular case cannot silently fall through to the many form");
const servedBlockedForms = globalThis.__closeabilityStrings.closeabilityBlocked;
globalThis.__closeabilityStrings.closeabilityBlocked = fallbackStrings.closeabilityBlocked;
const fallbackOneBlockedBadge = html({ state: "idle", closeability: block({
    state: "blocked", attestation_id: null, reasons: [obligation],
    mover: { kind: "session", self: true, person_needed: false } }) });
globalThis.__closeabilityStrings.closeabilityBlocked = servedBlockedForms;
assert.match(fallbackOneBlockedBadge, /1 obligation remains/,
    "the baked-in English fallback renders the singular form without leaking a template hole");
assert.doesNotMatch(fallbackOneBlockedBadge, /\{n\}|1 obligations/,
    "the fallback remains readable before the translated string request completes");
const unknownBadge = html({ state: "idle" });
assert.match(unknownBadge, /data-closeability="unknown"/,
    "a missing projection still draws the honest absence");
assert.equal(/[\u{1F500}-\u{1F5FF}]/u.test(unknownBadge), false,
    "and it carries no icon: giving an absence a symbol is how needs_triage became a demand");
const malformed = project({ state: "idle", closeability: "safe" });
assert.ok(malformed.block,
    "a present but malformed closeability value keeps an honest unknown block for rendering");
assert.equal(project({ state: "idle" }).block, null,
    "a genuinely absent field stays distinguishable for old-server compatibility");

const shapeBase = { state: "idle", closeability: block({
    state: "blocked", attestation_id: null, reasons: [obligation],
    mover: { kind: "session", self: true, person_needed: false } }) };
const shapeMoved = { state: "idle", closeability: block({
    state: "blocked", attestation_id: null, reasons: [{ ...obligation,
        mover: { kind: "session", self: false, session_id: "ROOT-TAB",
                 person_needed: false } }],
    mover: { kind: "session", self: false, session_id: "ROOT-TAB",
             person_needed: false } }) };
const shapeRetargeted = { state: "idle", closeability: block({
    state: "blocked", attestation_id: null,
    reasons: [{ ...obligation, subject_id: "task-2" }],
    mover: { kind: "session", self: true, person_needed: false } }) };
assert.notEqual(derive.sessionCloseabilityShape(shapeBase),
    derive.sessionCloseabilityShape(shapeMoved),
    "a mover identity change invalidates the row redraw key");
assert.notEqual(derive.sessionCloseabilityShape(shapeBase),
    derive.sessionCloseabilityShape(shapeRetargeted),
    "a reason subject identity change invalidates the row redraw key");

// A safe row asks for nothing, so it names no mover.
assert.equal(/moves it/.test(safeBadge), false,
    "a session that is safe to close is not waiting on anybody");

// The row contract that makes 320 and 390 hold: the badge shrinks and ellipsises inside a
// clipped flex row rather than widening it. Read from the stylesheet, because that is where
// the promise actually lives.
const css = await readFile(new URL("../Resources/web/app/css/list.css", import.meta.url),
    "utf8");
const badgeRule = css.slice(css.indexOf(".session-closeability {"));
const badgeBody = badgeRule.slice(0, badgeRule.indexOf("}"));
for (const property of ["flex: 0 1 auto", "min-width: 0", "overflow: hidden",
                        "text-overflow: ellipsis", "white-space: nowrap"]) {
    assert.ok(badgeBody.includes(property),
        "the badge keeps `" + property + "`, or a narrow row grows instead of clipping");
}
const rowState = css.slice(css.indexOf(".row .state {"));
const rowStateBody = rowState.slice(0, rowState.indexOf("}"));
for (const property of ["min-width: 0", "overflow: hidden"]) {
    assert.ok(rowStateBody.includes(property),
        "the row's state cell keeps `" + property + "`, which is what clips the badge");
}

const composerSource = await readFile(new URL("../Resources/web/app/js/input/composer.js", import.meta.url), "utf8");
const fillSource = composerSource.match(/export function fillSuggestedReply\(sid, expectedKey\) \{[\s\S]*?\n\}/)?.[0];
assert.ok(fillSource, "a separate fill-only boundary exists");
let draft = "", inserts = 0, renders = 0, replyChecks = 0, focused = 0, caret = 0;
const current = { id: "one", machine: "mac", sessionId: "conversation", owed: { note: "original" } };
const ui = { openId: "one", write: true, conn: "live", agent: null, tx: { id: "one" } };
ui.sessions = [current]; ui.replyComposerIdentity = derive.replySessionIdentity(current, ui.sessions);
const box = { focus() { focused++; }, get textContent() { return draft; }, set textContent(v) { inserts++; draft = v; } };
let model = { key: "exact-key", text: "允許\nexact candidate" }, attachments = [];
const fillReply = new Function("S", "els", "byId", "sessionSuggestedReply", "rawMsgText", "Shots", "Voice",
    "blankness", "renderComposer", "sending", "closingID", "toast", "document", "replySessionIdentity", "caretToEnd",
    fillSource.replace("export ", "") + "\nreturn fillSuggestedReply;")(
    ui, { msg: box }, id => id === "one" ? current : null, () => model, () => draft,
    { urls: () => attachments, busy: () => false }, { busy: () => false }, () => {}, () => renders++, false, null,
    () => {}, { documentElement: { lang: "zh-Hant" } }, derive.replySessionIdentity, () => caret++);
function proof(condition, why) { replyChecks++; assert.ok(condition, why); }
proof(fillReply("one", "exact-key") === true && draft === model.text, "only exact literal reply fills");
proof(inserts === 1 && renders === 1 && current.owed.note === "original", "only draft/render changes; owed preserved");
proof(focused === 1 && caret === 1, "successful literal fill focuses its composer at the end for review");
for (const existing of ["original draft", " ", "\n"]) {
    draft = existing; proof(fillReply("one", "exact-key") === false && draft === existing, "even whitespace draft is preserved");
}
draft = "";
proof(fillReply("one", "stale-key") === false && draft === "", "stale displayed suggestion is rejected");
proof(fillReply("other", "exact-key") === false && draft === "", "other row cannot touch current composer");
attachments = ["photo"];
proof(fillReply("one", "exact-key") === false && draft === "", "attachment-only draft is protected");
attachments = [];
ui.replyComposerIdentity = JSON.stringify([current.id, current.machine, current.sessionId]);
ui.sessions = [current];
current.machine = "foreign-machine";
proof(fillReply("one", "exact-key") === false && draft === "",
    "same row id from another machine cannot fill the already-open composer");
current.machine = "mac";
current.sessionId = "new-conversation";
proof(fillReply("one", "exact-key") === false && draft === "", "replacement before quiet refresh cannot rebind composer");
ui.tx.error = "refresh failed";
proof(fillReply("one", "exact-key") === false && draft === "", "failed refresh does not authorize a replacement conversation");
delete ui.tx.error; current.sessionId = "conversation";
ui.sessions = [current, { ...current, machine: "other-mac" }];
proof(fillReply("one", "exact-key") === false && draft === "", "duplicate terminal ids across machines fail closed");
ui.sessions = [current]; const pin = ui.replyComposerIdentity; ui.replyComposerIdentity = null;
proof(fillReply("one", "exact-key") === false && draft === "", "closed or unbound composer cannot fill");
ui.replyComposerIdentity = pin;
for (const [key, value] of [["write", false], ["conn", "offline"], ["agent", { id: "child" }], ["openId", "other"]]) {
    const before = ui[key]; ui[key] = value;
    proof(fillReply("one", "exact-key") === false && draft === "", "unsafe composer context refuses"); ui[key] = before;
}
model = null;
proof(fillReply("one", "exact-key") === false && draft === "", "withdrawn/expired suggestion refuses at click time");
proof(focused === 1 && caret === 1, "all refused fills preserve focus and selection");
proof(!/api\.|send\(|resume|appendMsg|dispatchEvent/.test(fillSource), "fill boundary has no send/resume/remote event path");
const openSource = await readFile(new URL("../Resources/web/app/js/session/open.js", import.meta.url), "utf8");
proof(openSource.includes("S.replyComposerIdentity = replySessionIdentity(s)") &&
    openSource.includes("S.replyComposerIdentity = null"), "actual open/close code owns composer pin");
proof(!openSource.slice(openSource.indexOf("export function loadTranscript"), openSource.indexOf("export function openSession"))
    .includes("S.replyComposerIdentity ="), "quiet/loading/failed transcript refresh cannot rebind the composer");
console.log(`CLA-370 draft-only boundary: ${replyChecks} checks passed`);
console.log("web session closeability: ok");
