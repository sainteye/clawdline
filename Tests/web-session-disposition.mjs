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
    sessionWorkComplete: "Reviewed and approved",
    webTaskRoot: "Root",
    webTaskTasks: "Tasks"
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
assert.match(single,
    /class="session-work-completion"[\s\S]*class="session-work-mark"[\s\S]*class="session-work-copy"[^>]*data-work-state="milestone_complete"/,
    "the milestone check and its explanatory receipt are one uninterrupted status group");
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

const featureRoot = {
    id: "feature-root", label: "Feature launch", state: "working", work_state: "working",
    root_assignment: { id: "assignment-one", label: "Root Assignment API", state: "active",
        ownership: "independent_root", explanation: "owns_feature_lifecycle" }
};
const visibleParent = {
    id: "ordinary-parent", label: "Visible parent", state: "idle", work_state: "ready"
};
state.sessions = [visibleParent, featureRoot];
state.tasks = [
    { id: "hostile-task-shape", state: "briefed",
      child: { terminalId: "feature-root" }, root: { terminalId: "ordinary-parent" } },
    { id: "live-feature-child", state: "briefed", title: "Lifecycle tests",
      child: { terminalId: "feature-child" }, root: { terminalId: "feature-root" } }
];
assert.equal(derive.rootAssignmentOf(featureRoot).id, "assignment-one",
    "the closed independent-root projection is recognized");
assert.equal(derive.rowDepth("feature-root"), 0,
    "a Feature Root is never indented as task lineage even beside a hostile task-shaped frame");
assert.equal(derive.rootAssignmentOf({ root_assignment: {
    id: "fake", label: "Fake", state: "active", ownership: "child" } }), null,
    "an object without independent-root ownership cannot acquire Feature Root classification");
const featureChip = derive.featureRootChip(featureRoot);
assert.equal(featureChip.text, "Root · 1",
    "the Feature Root chip uses the same compact child count as an ordinary root");
assert.match(featureChip.title, /Lifecycle tests/,
    "the behavior model keeps the durable root label and live child title in its tooltip");
state.tasks = [];
assert.equal(derive.featureRootChip(featureRoot).text, "Root · 0",
    "a childless Feature Root stays identified without putting its long assignment label in the chip");
assert.equal(derive.featureRootChip({ ...featureRoot, root_assignment: {
    ...featureRoot.root_assignment, state: "failed" } }), null,
    "terminal assignment projections cannot reach a dead Feature Root chip branch");

const listSource = await readFile(
    new URL("../Resources/web/app/js/view/list.js", import.meta.url), "utf8");
assert.match(listSource, /projectSessionWorkState\(s\)/,
    "the row uses the tested closed-state projection");
assert.match(listSource, /sessionWorkStateHTML\(s\)/,
    "the row uses the tested accessible marker renderer");
assert.match(listSource,
    /var workSaid = sessionWorkStateHTML\(s\);[\s\S]*workSaid \+= sessionCloseabilityHTML\(s\)/,
    "closeability follows the complete work-status group instead of splitting its check and copy");
assert.doesNotMatch(listSource,
    /workSaid \+= '<span class="session-work-copy"/,
    "the row does not append a detached completion explanation after closeability");
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
assert.match(infoSource,
    /sessionWorkStateHTML\(s\)[\s\S]*sessionCloseabilityHTML\(s\)/,
    "Session info repeats both the work and closeability statuses from the list");
assert.match(infoSource,
    /<details class="session-status-detail"[^>]*data-status-kind=/,
    "each full status in Session info is a clickable explanation disclosure");
assert.match(infoSource, /closeabilityLines\(s\)/,
    "the closeability explanation names the current reasons instead of only defining the key");

const pageSource = await readFile(
    new URL("../Resources/web/index.html", import.meta.url), "utf8");
// The identity block is two buttons now, and this used to insist it was one: the mark moved out
// of `#detail-info` into `#detail-snippets` when it became this project's snippets shortcut, so
// the assertion below went on describing a header that had been replaced. What is worth holding
// is not that there is a single button — it is that each half says out loud what it opens, and
// that the two say different things: `menu` for the sheet, `dialog` for Session info. A reader
// who cannot see the header is told which one they are on before they press it.
assert.match(pageSource,
    /<div class="detail-identity-block">[\s\S]*?<button class="detail-mark-go" id="detail-snippets"[^>]*aria-haspopup="menu"[\s\S]*?<canvas id="detail-mark"[\s\S]*?<\/button>[\s\S]*?<button class="detail-session" id="detail-info"[^>]*aria-haspopup="dialog"[\s\S]*?<span class="who detail-who">[\s\S]*?id="detail-name"[\s\S]*?id="detail-sub"[\s\S]*?<\/button>/,
    "the identity block is two labelled entrances: the mark opens this project's snippets, the name and path open Session info");
assert.match(pageSource,
    /<button class="detail-more" id="detail-actions-trigger"[^>]*aria-haspopup="menu"[\s\S]*?<circle[^>]*>[\s\S]*?<circle[^>]*>[\s\S]*?<circle/,
    "the transcript header exposes the Session menu through a recognizable bare three-dot button");
assert.doesNotMatch(pageSource, /class="chip detail-more"/,
    "the three-dot button has neither the chip border nor its filled background");
assert.doesNotMatch(pageSource, /id="tx-refresh"/,
    "the rarely used transcript refresh button no longer occupies the phone header");
assert.doesNotMatch(pageSource, /id="detail-actions-title"/,
    "the Session title no longer doubles as an unlabeled menu trigger");

const detailActionsSource = await readFile(
    new URL("../Resources/web/app/js/input/detail-actions.js", import.meta.url), "utf8");
assert.match(detailActionsSource,
    /els\["detail-actions-trigger"\]\.addEventListener\("click"[\s\S]*SessionActions\.toggle/,
    "the three-dot button opens the existing Session actions menu");
assert.doesNotMatch(detailActionsSource, /tx-refresh/,
    "removing the refresh control also removes its dead interaction path");

const actionConfirmSource = await readFile(
    new URL("../Resources/web/app/js/input/action-confirm.js", import.meta.url), "utf8");
assert.match(actionConfirmSource,
    /els\["detail-info"\]\.addEventListener\("click"[\s\S]*Info\.open/,
    "pressing the Session identity block opens Session info directly");

const detailCSS = await readFile(
    new URL("../Resources/web/app/css/detail.css", import.meta.url), "utf8");
assert.match(detailCSS,
    /\.detail-head \.detail-more\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px;/s,
    "the three-dot control keeps a phone-sized touch target");
assert.match(detailCSS, /\.session-actions\s*\{[^}]*right:\s*-7px;/s,
    "the menu is anchored to its new right-side trigger instead of the retired avatar trigger");

const sessionInfoSource = await readFile(
    new URL("../Sources/SessionInfo.swift", import.meta.url), "utf8");
assert.match(sessionInfoSource,
    /if let title, !title\.isEmpty \{ session\["title"\] = title \}/,
    "the info payload preserves the complete supplied Session title");
const remoteServerSource = await readFile(
    new URL("../Sources/RemoteServer.swift", import.meta.url), "utf8");
assert.match(remoteServerSource,
    /id: session\.id, title: publication\.labels\[session\.id\] \?\? session\.coordinate/,
    "the Session info route consumes the complete title from its accepted publication");
assert.match(remoteServerSource,
    /"label": publication\.labels\[session\.id\] \?\? session\.coordinate/,
    "the Session list consumes the same publication title as the info route");

const css = await readFile(
    new URL("../Resources/web/app/css/list.css", import.meta.url), "utf8");
assert.match(css, /\.row \.state \{[^}]*min-width:\s*0[^}]*overflow:\s*hidden/s,
    "the state rail owns clipping at phone widths");
assert.match(css, /\.session-work-mark\s*\{[^}]*flex:\s*0 0 auto/s,
    "check glyphs keep fixed geometry instead of widening the row");
assert.match(css, /\.session-work-completion\s*\{[^}]*display:\s*inline-flex/s,
    "the receipt check and copy share one visual group");
assert.match(css, /\.session-work-mark\s*\{[^}]*color:\s*var\(--ok\)/s,
    "completed Session checks use the shared success green");
assert.match(css, /\.session-work-mark\s*\{[^}]*gap:\s*0/s,
    "the double-check mark does not put whitespace between its two strokes");
assert.match(css,
    /\.session-work-check\s*\+\s*\.session-work-check\s*\{[^}]*margin-left:\s*-3px/s,
    "the second check overlaps the first into one compact read-receipt mark");
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
assert.match(sheetCSS,
    /\.info-sheet \.session-status-detail summary\s*\{[^}]*white-space:\s*normal/s,
    "Session info statuses wrap in full instead of inheriting the list's ellipsis");

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
assert.match(i18n, /webInfoWorkStatusMeaning:/,
    "the clickable work status has a localized explanation");
assert.match(i18n, /webInfoCloseabilityMeaning:/,
    "the clickable key status has a localized explanation");

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
assert.match(chineseCopy, /webInfoCloseabilityMeaning = "鑰匙表示這個 session 現在能不能安全關閉/,
    "Traditional Chinese explains the key separately from delivery");

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
