import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const deriveURL = new URL("../Resources/web/app/js/view/derive.js", import.meta.url);
const deriveSource = await readFile(deriveURL, "utf8");
const standalone = deriveSource
    .replace('import { T, fill } from "../core/i18n.js";',
        'const T = globalThis.__workStateStrings;\n' +
        'const fill = (s, vars) => s.replace(/\\{(\\w+)\\}/g, (m, k) => ' +
        'vars && k in vars ? vars[k] : m);')
    .replace('import { S } from "../core/state.js";', 'const S = globalThis.__workStateState;')
    .replace('import { renderList } from "./list.js";', 'const renderList = function () {};');
globalThis.__workStateState = { sessions: [], tasks: [], filter: "" };
globalThis.__workStateStrings = {
    sessionWorkReady: "can take new work",
    sessionWorkUnknown: "status unknown",
    sessionWorkHolding: "moving on its own",
    sessionWorkOwed: "a decision is owed",
    sessionWorkSelfStated: "self-reported",
    sessionWaitedOnByOne: "1 waiting on you",
    sessionWaitedOnByMany: "{n} waiting on you",
    sessionWorkMilestone: "Delivered; awaiting approval",
    sessionWorkComplete: "Reviewed and approved"
};
const derive = await import("data:text/javascript;base64," + Buffer.from(standalone).toString("base64"));

const project = derive.projectSessionWorkState;
const html = derive.sessionWorkStateHTML;
const state = globalThis.__workStateState;
const closed = [
    [{ state: "idle", work_state: "ready" }, "ready"],
    [{ state: "working", work_state: "working" }, "working"],
    [{ state: "idle", work_state: "holding", work_provenance: "self" }, "holding"],
    [{ state: "waiting", work_state: "waiting_you" }, "waiting_you"],
    [{ state: "idle", work_state: "waiting_session",
       coordination: { waitingOn: [{ id: "wait" }], waitedOnBy: [] } }, "waiting_session"],
    [{ state: "idle", work_state: "unknown" }, "unknown"],
    [{ state: "idle", work_state: "milestone_complete",
       disposition: { scope: "task", taskId: "one", evidence: "authenticated_task_delivery" } },
     "milestone_complete"],
    [{ state: "idle", work_state: "milestone_complete",
       disposition: { scope: "session", evidence: "authenticated_session_delivery" } },
     "milestone_complete"],
    [{ state: "idle", work_state: "work_complete",
       disposition: { scope: "task", taskId: "one", evidence: "broker_verified_target_landing" } },
     "work_complete"]
];
for (const [session, state] of closed) {
    assert.equal(project(session).state, state,
        `the consistent broker state ${state} remains one member of the closed set`);
}
assert.equal(project({ state: "idle" }).state, "unknown",
    "a missing work_state fails closed instead of leaving the row blank");
assert.equal(project({ state: "idle", work_state: "future_guess" }).state, "unknown",
    "an unknown future value fails closed instead of being guessed");
assert.equal(project({ state: "idle", work_state: "needs_triage" }).state, "unknown",
    "the retired needs_triage spelling is just another unknown value now");
assert.equal(project({ state: "working" }).state, "unknown",
    "even obvious terminal activity cannot fill in a missing work_state at the client");
assert.equal(project({ state: "idle", work_state: "waiting_you" }).state, "unknown",
    "a projected wait inconsistent with its source axis fails closed");
assert.equal(project({ state: "idle", work_state: "holding" }).state, "unknown",
    "holding without a declaring session behind it fails closed — it is never a fallback");
assert.equal(project({ state: "idle", work_state: "holding", work_provenance: "broker" }).state,
    "unknown", "not even the broker may put a session into holding without a declaration");
state.sessions = [{ id: "root", state: "idle", work_state: "waiting_session" }];
state.tasks = [{ id: "live-child", state: "briefed", title: "review the patch",
    root: { terminalId: "root" }, child: { terminalId: "child" } }];
assert.equal(project(state.sessions[0]).state, "waiting_session",
    "an active Clawdline child is typed evidence that its idle root waits on a session");
state.sessions = [];
state.tasks = [];
assert.equal(project({ state: "waiting", work_state: "work_complete" }).state, "waiting_you",
    "a question stopped on you outranks even a broker closure receipt");
assert.equal(project({ state: "idle", work_state: "work_complete",
    coordination: { waitingOn: [{ id: "wait" }], waitedOnBy: [] } }).state,
    "waiting_session", "a peer wait outranks both checks without asking the human");
assert.equal(project({ state: "unknown", work_state: "work_complete" }).state, "unknown",
    "an unreadable terminal outranks a stale completion projection");
assert.equal(project({ state: "idle", work_state: "work_complete",
    disposition: { scope: "task", taskId: "one", evidence: "broker_verified_task_closure" }
}).state, "unknown", "legacy over-claimed closure evidence fails closed");
assert.equal(project({ state: "idle", work_state: "milestone_complete",
    disposition: { scope: "session", evidence: "authenticated_task_delivery" }
}).state, "unknown", "a root receipt cannot borrow task delivery evidence");

const hostile = {
    state: "idle", work_state: "milestone_complete",
    disposition: { scope: "task", taskId: "one", evidence: "authenticated_task_delivery",
        title: '\"><img src=x onerror=alert(1)>' }
};
const single = html(hostile);
assert.match(single, /class="session-work-mark"/);
assert.match(single, /role="img"/);
assert.match(single, /aria-label="Delivered; awaiting approval"/);
assert.match(single, /title="Delivered; awaiting approval · &quot;&gt;&lt;img/,
    "receipt titles are escaped before entering an attribute");
assert.doesNotMatch(single, /<img src=/, "receipt metadata cannot inject row markup");
assert.equal((single.match(/session-work-check/g) || []).length, 1,
    "a milestone draws one CSS check, not a platform emoji");
assert.equal((html({ state: "idle", work_state: "work_complete",
    disposition: { scope: "task", taskId: "one", evidence: "broker_verified_target_landing" } })
    .match(/session-work-check/g) || []).length, 2,
    "broker closure draws exactly two CSS checks");
const unknownHTML = html({ state: "idle" });
assert.match(unknownHTML, />status unknown</,
    "the fail-closed state is readable text rather than an empty row");
assert.ok(!/[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u.test(unknownHTML),
    "unknown is an absence, not a category: it deliberately carries no icon");

const readyHTML = html({ state: "idle", work_state: "ready", work_provenance: "self",
    work_note: "fix landed; can take new work" });
assert.match(readyHTML, /📭/, "ready reads as an open, empty box you can hand work to");
assert.match(readyHTML,
    /class="session-status-glyph" aria-hidden="true">📭<\/span><span class="session-status-label">/,
    "the mailbox is a decorative glyph with its own line box, separate from readable copy");
assert.match(readyHTML, /fix landed; can take new work/,
    "a declared ready leads with the session's own words");
assert.match(readyHTML, /self-reported/,
    "a stated state says it is stated, so it can never dress as a proven one");
assert.doesNotMatch(html({ state: "idle", work_state: "ready" }), /self-reported/,
    "broker-projected ready carries no self marker");

const holdingHTML = html({ state: "idle", work_state: "holding", work_provenance: "self",
    work_note: "resumes when the release build finishes" });
assert.match(holdingHTML, /🔜/, "holding reads as about to move by itself");
assert.match(holdingHTML, /resumes when the release build finishes/);

const owedHTML = html({ state: "idle", work_state: "unknown",
    owed: { note: "the schedules design is still your call",
            since: Math.floor(Date.now() / 1000) - 3 * 86400, person_needed: true } });
assert.match(owedHTML, /📥/, "a debt reads as something sitting in your tray");
assert.match(owedHTML,
    /class="session-status-glyph" aria-hidden="true">📥<\/span><span class="session-status-label">/,
    "the debt tray uses the same unclipped decorative-glyph contract");
assert.match(owedHTML, /the schedules design is still your call/);
assert.match(owedHTML, /· 3d/, "the debt ages in plain sight — that age is its whole risk");
assert.match(html({ state: "working", work_state: "working",
    owed: { note: "still yours to call", since: 0, person_needed: true } }),
    /📥[\s\S]*still yours to call/,
    "the second axis rides beside a working row instead of being silenced by it");
assert.doesNotMatch(html({ state: "idle", work_state: "unknown",
    owed: { note: '"><img src=x onerror=alert(1)>', since: 0, person_needed: true } }),
    /<img src=/, "a hostile debt note cannot inject row markup");

const clawdfather = {
    id: "clawdfather", label: "Clawdfather", state: "working", work_state: "working",
    coordinator: { label: "Clawdfather", status: "online", commands: [] }
};
state.sessions = [
    { id: "waiting", label: "A waiting Session", state: "waiting",
      work_state: "waiting_you" },
    { id: "root", label: "An ordinary root", state: "idle", work_state: "ready" },
    clawdfather
];
state.tasks = [];
assert.deepEqual(derive.ordered().map((session) => session.id),
    ["clawdfather", "waiting", "root"],
    "Clawdfather is pinned above even a human-waiting ordinary Session");

state.tasks = [{
    id: "stale-parent", state: "briefed",
    child: { terminalId: "clawdfather" }, root: { terminalId: "root" }
}];
assert.equal(derive.ordered()[0].id, "clawdfather",
    "task grouping cannot pull Clawdfather down from the pinned position");
assert.equal(derive.rowDepth("clawdfather"), 0,
    "the pinned Clawdfather row is never visually indented under another Session");

const listSource = await readFile(
    new URL("../Resources/web/app/js/view/list.js", import.meta.url), "utf8");
assert.match(listSource, /projectSessionWorkState\(s\)/,
    "the row uses the tested closed-state projection");
assert.match(listSource, /sessionWorkStateHTML\(s\)/,
    "the row uses the tested accessible marker renderer");
assert.match(listSource,
    /work\.state === "work_complete"\s*\? T\.sessionWorkComplete : T\.sessionWorkMilestone/,
    "a completed Session follows its check with the matching explanatory receipt text");
assert.match(listSource,
    /session-work-copy[^\n]+data-work-state[^\n]+work\.state/,
    "the visible completion explanation identifies which closed state it describes");
assert.match(listSource, /work\.state === "waiting_session"[^}]+webTaskTasks/s,
    "a root waiting for live children names that task wait on its state line");
assert.match(listSource, /sessionStatusGlyphHTML\("🙋", T\.sessionWaiting\)/,
    "the human-wait hand uses the same unclipped glyph box as the quiet status badges");
assert.match(listSource, /sessionStatusGlyphHTML\("⏳", peerText\)/,
    "the peer-wait hourglass uses the same unclipped glyph box as the quiet status badges");
assert.doesNotMatch(listSource, /class="wants">🙋/,
    "the human-wait hand is never left inside the clipped mono text run");

const infoSource = await readFile(
    new URL("../Resources/web/app/js/input/info.js", import.meta.url), "utf8");
assert.match(infoSource, /s\.title\s*\|\|\s*model/,
    "Session info uses the full Session title as its headline and only falls back to the model");
assert.match(infoSource, /class="session-title"/,
    "the Session title has its own non-truncating presentation hook");

const sessionInfoSource = await readFile(
    new URL("../Sources/SessionInfo.swift", import.meta.url), "utf8");
assert.match(sessionInfoSource,
    /if let title, !title\.isEmpty \{ session\["title"\] = title \}/,
    "the info payload preserves the complete supplied Session title");
const remoteServerSource = await readFile(
    new URL("../Sources/RemoteServer.swift", import.meta.url), "utf8");
assert.match(remoteServerSource, /id: session\.id, title: session\.displayLabel/,
    "the Session info route supplies the same complete title as the Session list");

const css = await readFile(
    new URL("../Resources/web/app/css/list.css", import.meta.url), "utf8");
assert.match(css, /\.row \.state \{[^}]*min-width:\s*0[^}]*overflow:\s*hidden/s,
    "the state rail owns clipping at phone widths");
assert.match(css, /\.session-work-mark\s*\{[^}]*flex:\s*0 0 auto/s,
    "check glyphs keep fixed geometry instead of widening the row");
assert.match(css, /\.session-work-mark\s*\{[^}]*color:\s*var\(--ok\)/s,
    "completed Session checks use the shared success green");
assert.match(css, /\.session-work-copy\s*\{[^}]*min-width:\s*0[^}]*text-overflow:\s*ellipsis/s,
    "readable ready and triage copy yields to the phone width");
assert.match(css, /\.session-status-glyph\s*\{[^}]*line-height:\s*1\.4[^}]*overflow:\s*visible/s,
    "platform emoji receive a taller visible line box instead of inheriting the clipped mono line");
assert.match(css, /\.session-status-label\s*\{[^}]*min-width:\s*0[^}]*text-overflow:\s*ellipsis/s,
    "only the status words, never the glyph, own narrow-row ellipsis");
assert.doesNotMatch(css, /session-work[^}]*animation:/s,
    "the disposition marker adds no motion, reduced or otherwise");

const sheetCSS = await readFile(
    new URL("../Resources/web/app/css/sheets.css", import.meta.url), "utf8");
assert.match(sheetCSS, /\.info-sheet \.hero \.session-title\s*\{[^}]*overflow-wrap:\s*anywhere/s,
    "a long Session title wraps in full instead of being ellipsized or clipped");
assert.doesNotMatch(sheetCSS, /\.info-sheet \.hero \.session-title\s*\{[^}]*line-clamp/s,
    "the Session info headline has no line clamp");

const i18n = await readFile(
    new URL("../Resources/web/app/js/core/i18n.js", import.meta.url), "utf8");
for (const key of ["sessionWorkReady", "sessionWorkUnknown", "sessionWorkHolding",
    "sessionWorkOwed", "sessionWorkSelfStated",
    "sessionWorkMilestone", "sessionWorkComplete"]) {
    assert.match(i18n, new RegExp(key + ":"), `${key} is localizable`);
}
assert.match(i18n, /sessionWorkMilestone:\s*"Delivered; awaiting approval"/,
    "the web fallback explains the single-check milestone state");
assert.match(i18n, /sessionWorkComplete:\s*"Reviewed and approved"/,
    "the web fallback explains the double-check accepted state");

const chineseCopy = await readFile(
    new URL("../Sources/Copy+Chinese.swift", import.meta.url), "utf8");
assert.match(chineseCopy, /sessionWorkMilestone = "已交付，等待驗收"/,
    "Traditional Chinese explains that a single check still awaits acceptance");
assert.match(chineseCopy, /sessionWorkComplete = "已驗收完成"/,
    "Traditional Chinese explains that double checks mean acceptance is complete");
assert.match(chineseCopy, /sessionWorkMilestone = "已交付，等待验收"/,
    "Simplified Chinese explains that a single check still awaits acceptance");
assert.match(chineseCopy, /sessionWorkComplete = "已验收完成"/,
    "Simplified Chinese explains that double checks mean acceptance is complete");
assert.match(chineseCopy, /sessionWorkUnknown = "狀態未知"/,
    "Traditional Chinese names the absence, and does not issue an instruction");
assert.doesNotMatch(chineseCopy, /分流/,
    "the triage demand is gone from the vocabulary in both scripts");
assert.match(chineseCopy, /sessionWorkOwed = "欠一個決定"/,
    "Traditional Chinese names the debt the reader owes");
assert.match(chineseCopy, /sessionWorkReady = "可接新工作"/,
    "Traditional Chinese reads ready as the invitation it is");

// lost_if_closed at the moment of the press: the page-side half of the close gate.
state.sessions = [{ id: "root", label: "a root", state: "idle", work_state: "unknown",
    coordination: { waitingOn: [], waitedOnBy: [{ id: "w1" }, { id: "w2" }] } }];
state.tasks = [
    { id: "live-1", state: "briefed", title: "review the patch",
      root: { terminalId: "root" }, child: { terminalId: "kid-1" } },
    { id: "done-1", state: "success", title: "already finished",
      root: { terminalId: "root" } }
];
assert.deepEqual(derive.lostIfClosed("root"),
    ["review the patch", "2 waiting on you"],
    "closing names its live children and stranded waiters, not finished history");
state.sessions = [];
state.tasks = [];
assert.deepEqual(derive.lostIfClosed("root"), [],
    "a quiet session loses nothing and the close stays one press");

const mock = await readFile(
    new URL("../Resources/web/app/js/net/mock.js", import.meta.url), "utf8");
assert.match(mock, /function setSessionState\(id, state\)/,
    "moving mock sessions update terminal and work-state axes together");
assert.doesNotMatch(mock, /find\([^\n]+\)\.state\s*=/,
    "mock transitions cannot bypass the closed work-state helper");

console.log("web session disposition tests passed");
process.exit(0);
