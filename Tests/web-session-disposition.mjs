import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const deriveURL = new URL("../Resources/web/app/js/view/derive.js", import.meta.url);
const deriveSource = await readFile(deriveURL, "utf8");
const standalone = deriveSource
    .replace('import { T } from "../core/i18n.js";', 'const T = globalThis.__workStateStrings;')
    .replace('import { S } from "../core/state.js";', 'const S = { sessions: [], tasks: [], filter: "" };')
    .replace('import { renderList } from "./list.js";', 'const renderList = function () {};');
globalThis.__workStateStrings = {
    sessionWorkReady: "ready",
    sessionWorkNeedsTriage: "status needs triage",
    sessionWorkMilestone: "Milestone delivered; review or landing may remain",
    sessionWorkComplete: "Broker-verified target landing"
};
const derive = await import("data:text/javascript;base64," + Buffer.from(standalone).toString("base64"));

const project = derive.projectSessionWorkState;
const html = derive.sessionWorkStateHTML;
const closed = [
    [{ state: "idle", work_state: "ready" }, "ready"],
    [{ state: "working", work_state: "working" }, "working"],
    [{ state: "waiting", work_state: "waiting_human" }, "waiting_human"],
    [{ state: "idle", work_state: "waiting_session",
       coordination: { waitingOn: [{ id: "wait" }], waitedOnBy: [] } }, "waiting_session"],
    [{ state: "idle", work_state: "needs_triage" }, "needs_triage"],
    [{ state: "idle", work_state: "milestone_complete",
       disposition: { scope: "task", taskId: "one", evidence: "authenticated_task_delivery" } },
     "milestone_complete"],
    [{ state: "idle", work_state: "work_complete",
       disposition: { scope: "task", taskId: "one", evidence: "broker_verified_target_landing" } },
     "work_complete"]
];
for (const [session, state] of closed) {
    assert.equal(project(session).state, state,
        `the consistent broker state ${state} remains one member of the closed set`);
}
assert.equal(project({ state: "idle" }).state, "needs_triage",
    "a missing work_state fails closed instead of leaving the row blank");
assert.equal(project({ state: "idle", work_state: "future_guess" }).state, "needs_triage",
    "an unknown future value fails closed instead of being guessed");
assert.equal(project({ state: "working" }).state, "needs_triage",
    "even obvious terminal activity cannot fill in a missing work_state at the client");
assert.equal(project({ state: "idle", work_state: "waiting_human" }).state, "needs_triage",
    "a projected wait inconsistent with its source axis fails closed");
assert.equal(project({ state: "waiting", work_state: "work_complete" }).state, "waiting_human",
    "a human question outranks even a broker closure receipt");
assert.equal(project({ state: "idle", work_state: "work_complete",
    coordination: { waitingOn: [{ id: "wait" }], waitedOnBy: [] } }).state,
    "waiting_session", "a peer wait outranks both checks without asking the human");
assert.equal(project({ state: "unknown", work_state: "work_complete" }).state, "needs_triage",
    "an unreadable terminal outranks a stale completion projection");
assert.equal(project({ state: "idle", work_state: "work_complete",
    disposition: { scope: "task", taskId: "one", evidence: "broker_verified_task_closure" }
}).state, "needs_triage", "legacy over-claimed closure evidence fails closed");

const hostile = {
    state: "idle", work_state: "milestone_complete",
    disposition: { scope: "task", taskId: "one", evidence: "authenticated_task_delivery",
        title: '\"><img src=x onerror=alert(1)>' }
};
const single = html(hostile);
assert.match(single, /class="session-work-mark"/);
assert.match(single, /role="img"/);
assert.match(single, /aria-label="Milestone delivered; review or landing may remain"/);
assert.match(single, /title="Milestone delivered; review or landing may remain · &quot;&gt;&lt;img/,
    "receipt titles are escaped before entering an attribute");
assert.doesNotMatch(single, /<img src=/, "receipt metadata cannot inject row markup");
assert.equal((single.match(/session-work-check/g) || []).length, 1,
    "a milestone draws one CSS check, not a platform emoji");
assert.equal((html({ state: "idle", work_state: "work_complete",
    disposition: { scope: "task", taskId: "one", evidence: "broker_verified_target_landing" } })
    .match(/session-work-check/g) || []).length, 2,
    "broker closure draws exactly two CSS checks");
assert.match(html({ state: "idle" }), />status needs triage</,
    "the fail-closed state is readable text rather than an empty row");

const listSource = await readFile(
    new URL("../Resources/web/app/js/view/list.js", import.meta.url), "utf8");
assert.match(listSource, /projectSessionWorkState\(s\)/,
    "the row uses the tested closed-state projection");
assert.match(listSource, /sessionWorkStateHTML\(s\)/,
    "the row uses the tested accessible marker renderer");

const css = await readFile(
    new URL("../Resources/web/app/css/list.css", import.meta.url), "utf8");
assert.match(css, /\.row \.state \{[^}]*min-width:\s*0[^}]*overflow:\s*hidden/s,
    "the state rail owns clipping at phone widths");
assert.match(css, /\.session-work-mark\s*\{[^}]*flex:\s*0 0 auto/s,
    "check glyphs keep fixed geometry instead of widening the row");
assert.match(css, /\.session-work-copy\s*\{[^}]*min-width:\s*0[^}]*text-overflow:\s*ellipsis/s,
    "readable ready and triage copy yields to the phone width");
assert.doesNotMatch(css, /session-work[^}]*animation:/s,
    "the disposition marker adds no motion, reduced or otherwise");

const i18n = await readFile(
    new URL("../Resources/web/app/js/core/i18n.js", import.meta.url), "utf8");
for (const key of ["sessionWorkReady", "sessionWorkNeedsTriage",
    "sessionWorkMilestone", "sessionWorkComplete"]) {
    assert.match(i18n, new RegExp(key + ":"), `${key} is localizable`);
}

const mock = await readFile(
    new URL("../Resources/web/app/js/net/mock.js", import.meta.url), "utf8");
assert.match(mock, /function setSessionState\(id, state\)/,
    "moving mock sessions update terminal and work-state axes together");
assert.doesNotMatch(mock, /find\([^\n]+\)\.state\s*=/,
    "mock transitions cannot bypass the closed work-state helper");

console.log("web session disposition tests passed");
process.exit(0);
