import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
    beginTranscriptLoad,
    createTieredSessionFacts,
    createTranscriptRefetchPolicy,
    createTranscriptRequests,
    createTranscriptRevisionObserver,
    planTranscriptRenderChunks,
    scheduleTranscriptRender,
    transcriptSignatureOf,
    TRANSCRIPT_LATENCY_BUDGETS,
    TRANSCRIPT_LINE_REREAD_MS
} from "../Resources/web/app/js/session/transcript-requests.js";
import {
    createFrameCoalescer,
    createVisibleInterval,
    FRAME_FALLBACK_MS,
    whenVisible
} from "../Resources/web/app/js/core/visibility.js";

// The real page modules, driven in a child process of their own: they read `document` and
// `window` at import time and start clocks, so they get a fake page that nothing else here shares.
if (process.env.CLAWDLINE_VIEW_QUIET_BEHAVIOR) {
    await viewQuietBehavior(process.env.CLAWDLINE_VIEW_QUIET_BEHAVIOR);
    process.exit(0);
}

let checks = 0;
function equal(actual, expected, message) { assert.deepEqual(actual, expected, message); checks += 1; }
function ok(value, message) { assert.ok(value, message); checks += 1; }
function tick() { return new Promise(function (resolve) { setImmediate(resolve); }); }
function deferred() {
    let resolve, reject;
    const promise = new Promise(function (yes, no) { resolve = yes; reject = no; });
    return { promise, resolve, reject };
}

equal(TRANSCRIPT_LATENCY_BUDGETS, {
    healthyLocalTTFBP95Ms: 250,
    ordinaryResponseToMeaningfulPaintP95Ms: 100,
    largeRenderTaskMaxMs: 50
}, "the contract publishes the 250ms / 100ms / 50ms budgets");

const openOrder = [];
await beginTranscriptLoad(function () {
    openOrder.push("request-issued");
    return Promise.resolve();
}, function () { openOrder.push("loading-render"); });
equal(openOrder, ["request-issued", "loading-render"],
    "the load seam issues the real request before skeleton/shell/info rendering");

const reads = [];
const accepted = [];
const scheduled = [];
const request = createTranscriptRequests(function (id, demand) {
    const gate = deferred();
    reads.push({ id, demand, gate });
    return gate.promise;
}, function (id, ticket, outcome, revision) {
    accepted.push({ id, ticket, outcome, revision });
}, { afterPaint: function (work) { scheduled.push(work); } });
request.activate("chat");

const r1 = request("chat", 1, "r1", { foreground: true });
const r2 = request("chat", 2, "r2", { foreground: true });
equal(reads.length, 1, "an active demand admits one synchronous network request");
equal(reads[0].demand.foreground, true, "first-open priority reaches the fetch seam");
reads[0].gate.resolve({ signature: "r1", entries: [] });
await tick();
equal(reads.length, 2, "a newer active demand becomes one bounded trailing request");

let r3Settled = false;
const r3 = request("chat", 3, "r3").then(function (value) { r3Settled = true; return value; });
const r4 = request("chat", 4, "r4");
reads[1].gate.resolve({ signature: "r2", entries: [{ role: "assistant", text: "readable" }] });
await Promise.all([r1, r2]);
equal(accepted.map(function (row) { return row.revision; }), ["r2"],
    "the bounded trailing answer is accepted with the context it actually requested");
equal(r3Settled, false, "a newer waiter does not resolve before a request covers its demand");
equal(scheduled.length, 1, "newer trailing demands collapse into one post-paint replay");

const r5 = request("chat", 5, "r5");
scheduled.shift()();
equal(reads.length, 3, "the scheduled replay starts exactly one request");
reads[2].gate.resolve({ signature: "r5", entries: [] });
await Promise.all([r3, r4, r5]);
await tick();
equal(accepted.at(-1).revision, "r5",
    "a stale scheduled replay can only move forward to the newest context");
equal(accepted.at(-1).ticket, 5, "the newest monotonic demand owns the accepted result");

const oldReads = [];
const oldScheduled = [];
const oldRequest = createTranscriptRequests(function (id) {
    const gate = deferred(); oldReads.push({ id, gate }); return gate.promise;
}, function () {}, { afterPaint: function (work) { oldScheduled.push(work); } });
oldRequest.activate("old");
oldRequest("old", 1, "o1");
oldRequest("old", 2, "o2");
oldReads[0].gate.resolve({}); await tick();
oldRequest("old", 3, "o3");
oldReads[1].gate.resolve({}); await tick();
equal(oldScheduled.length, 1, "the old session has one delayed refresh");
oldRequest.activate("new");
oldScheduled.shift()();
equal(oldReads.length, 2, "old-session scheduled work cannot consume the new session lane");
oldRequest("new", 4, "n4", { foreground: true });
equal(oldReads.at(-1).id, "new", "the new session issues immediately after the switch");

const fullReads = [], summaryReads = [];
const facts = createTieredSessionFacts(function () {
    const gate = deferred(); fullReads.push(gate); return gate.promise;
}, function () {
    const gate = deferred(); summaryReads.push(gate); return gate.promise;
}, { ttl: 60000, now: function () { return 1000; } });

const staleSummary = facts.getSummary("A"); await tick();
const freshFull = facts.get("A", true); await tick();
fullReads[0].resolve({ info: { tier: "full", files: {} } });
equal(await freshFull, { tier: "full", files: {} }, "forced full Info resolves with full facts");
summaryReads[0].resolve({ info: { tier: "summary" } });
equal(await staleSummary, null, "force invalidates the older summary completion");
equal(facts.tier("A"), "full", "full is the explicit winning cache tier");
equal(facts.peek("A"), { tier: "full", files: {} }, "late summary cannot downgrade full data");

const staleFull = facts.get("B"); await tick();
facts.drop("B");
const replacementFull = facts.get("B", true); await tick();
fullReads[1].resolve({ info: { version: "old" } });
equal(await staleFull, null, "drop invalidates an in-flight full completion");
fullReads[2].resolve({ info: { version: "new" } });
equal(await replacementFull, { version: "new" }, "the replacement generation is accepted");
equal(facts.peek("B"), { version: "new" }, "the old full completion cannot overwrite it");

const failedSummary = facts.getSummary("C").catch(function () { return "failed"; }); await tick();
facts.receiveFull("C", { version: "explicit-full" });
summaryReads[1].reject(new Error("late summary failure"));
equal(await failedSummary, "failed", "a stale summary failure still settles its own caller");
equal(facts.peek("C"), { version: "explicit-full" },
    "a stale summary failure cannot clear an explicit full upgrade");

// A full answer outranks a summary while it is fresh, and for no longer than that. The status
// line reads a summary a minute and then completes it; if a minute-old full reading could still
// answer for that summary, the row would keep drawing the state of a session as it was when its
// card was last opened, having gone to the network to be told so.
let clock = 5000;
const agingFull = [], agingSummary = [];
const aging = createTieredSessionFacts(function () {
    const gate = deferred(); agingFull.push(gate); return gate.promise;
}, function () {
    const gate = deferred(); agingSummary.push(gate); return gate.promise;
}, { ttl: 60000, now: function () { return clock; } });

const firstFull = aging.get("D"); await tick();
agingFull[0].resolve({ info: { tier: "full", files: { branch: "main" } } });
equal(await firstFull, { tier: "full", files: { branch: "main" } },
    "the complete reading is what the upgrade holds");

clock += 30000;
const insideTTL = aging.getSummary("D"); await tick();
equal(agingSummary.length, 0, "a fresh full answer serves a summary read without a request");
equal(await insideTTL, { tier: "full", files: { branch: "main" } },
    "and a summary cannot downgrade it while it is fresh");

clock += 40000;
const pastTTL = aging.getSummary("D"); await tick();
equal(agingSummary.length, 1, "past the TTL the summary is actually read");
agingSummary[0].resolve({ info: { tier: "summary" } });
equal(await pastTTL, { tier: "summary" }, "an aged-out full no longer answers for a summary");
equal(aging.peek("D"), { tier: "summary" }, "and is not what the row draws either");
equal(aging.tier("D"), "summary",
    "which is what tells the status line to ask for the rest of the reading again");

// Plan windows belong to the account on this Mac, not to whichever transcript happened to
// report them. A per-session cache used to preserve each session's old copy independently: a new
// Codex session had no windows at all until its first token_count, and switching between two old
// sessions made the footer jump between two percentages for the same account.
let quotaClock = 9000;
const quotaReads = [];
const quotaFacts = createTieredSessionFacts(function (id) {
    const gate = deferred(); quotaReads.push({ id, gate }); return gate.promise;
}, null, { ttl: 60000, now: function () { return quotaClock; } });
function quotaInfo(assistant, usedPercent, readAtMs) {
    return { info: {
        session: { assistant: assistant },
        limits: {
            readAtMs: readAtMs,
            windows: usedPercent === null ? [] : [
                { name: "7d", usedPercent: usedPercent, resetsAt: 9999999999, hit: false }
            ]
        }
    } };
}

const quotaA = quotaFacts.get("codex-old"); await tick();
quotaReads[0].gate.resolve(quotaInfo("codex", 41, 100));
equal((await quotaA).limits.windows[0].usedPercent, 41,
    "the first Codex session establishes the machine's quota snapshot");
const quotaB = quotaFacts.get("codex-new"); await tick();
quotaReads[1].gate.resolve(quotaInfo("codex", 57, 200));
equal((await quotaB).limits.windows[0].usedPercent, 57,
    "a newer Codex session advances the shared snapshot");
equal(quotaFacts.peek("codex-old").limits.windows[0].usedPercent, 57,
    "returning to an older session cannot restore its older account percentage");
equal(quotaFacts.machineLimits("codex").windows[0].usedPercent, 57,
    "a brand-new session can draw the machine snapshot before its own Info read completes");

const lateQuota = quotaFacts.get("codex-late"); await tick();
quotaReads[2].gate.resolve(quotaInfo("codex", 48, 150));
equal((await lateQuota).limits.windows[0].usedPercent, 57,
    "a late older response cannot roll the machine snapshot backward");
const claudeQuota = quotaFacts.get("claude"); await tick();
quotaReads[3].gate.resolve(quotaInfo("claude", 91, 300));
equal((await claudeQuota).limits.windows[0].usedPercent, 91,
    "Claude and Codex retain separate account-level truths");
equal(quotaFacts.machineLimits("codex").windows[0].usedPercent, 57,
    "a Claude reading cannot replace Codex's snapshot");

const resetQuota = quotaFacts.get("codex-reset"); await tick();
quotaReads[4].gate.resolve(quotaInfo("codex", null, 400));
equal((await resetQuota).limits.windows, [],
    "a newer machine reading may clear a window after reset instead of retaining stale usage");

const statusLineSource = readFileSync(
    new URL("../Resources/web/app/js/input/status-line.js", import.meta.url), "utf8");
ok(statusLineSource.includes("SessionFacts.machineLimits(s.assistant)"),
    "a new session draws the last machine snapshot while its own Info request is loading");

function renderHarness(newestFirst, mutation) {
    const entries = Array.from({ length: 200 }, function (_, id) {
        return { id, text: "x".repeat(41944), image: "image-" + id };
    });
    const source = newestFirst ? entries.slice().reverse() : entries;
    const chunks = planTranscriptRenderChunks(source, function (entry) {
        return entry.text.length;
    }, { byteBudget: 128 * 1024, itemBudget: 12 });
    const dom = { rows: [], scrollTop: 100 };
    const scheduledWork = [], paintWork = [], operations = [], notes = [];
    let current = true, clockValue = 0, clockHalf = false, taskIndex = 0;
    scheduleTranscriptRender({
        chunks, newestFirst, entryCount: entries.length,
        isCurrent: function () { return current; },
        schedule: function (work) { scheduledWork.push(work); },
        afterPaint: function (work) { paintWork.push(work); },
        clock: function () {
            if (!clockHalf) { clockHalf = true; return clockValue; }
            clockHalf = false;
            clockValue += taskIndex++ === 1 ? 60 : 5;
            return clockValue;
        },
        insert: function (chunk, placement) {
            const ids = chunk.map(function (entry) { return entry.id; });
            const before = dom.rows.length * 10;
            if (placement.first) dom.rows = ids;
            else {
                const prepend = mutation === "reverse-insert"
                    ? !placement.prepend : placement.prepend;
                dom.rows = prepend ? ids.concat(dom.rows) : dom.rows.concat(ids);
            }
            operations.push("insert:" + (placement.first ? "first" : placement.prepend ? "prepend" : "append"));
            return {
                images: ids.map(function (id) { return "image-" + id; }),
                heightDelta: placement.prepend ? dom.rows.length * 10 - before : 0,
                entries: ids.length
            };
        },
        adjustScroll: function (delta) { dom.scrollTop += delta; operations.push("scroll"); },
        hydrate: function (images) { operations.push("images:" + images.length); },
        note: function (name, data) { notes.push({ name, data }); },
        meaningful: function () { operations.push("meaningful-event"); },
        complete: function () { operations.push("complete"); }
    });
    return {
        entries, chunks, dom, scheduledWork, paintWork, operations, notes,
        cancel: function () { current = false; }
    };
}

const chronological = renderHarness(false, process.env.CLAWDLINE_TRANSCRIPT_TEST_MUTATION);
equal(chronological.dom.rows, chronological.entries.slice(-chronological.chunks.at(-1).length)
    .map(function (entry) { return entry.id; }),
    "the first task paints the newest chronological chunk first");
equal(chronological.operations.filter(function (row) { return row.startsWith("images:"); }).length, 0,
    "no image connector runs before the meaningful-paint boundary");
chronological.paintWork.shift()();
equal(chronological.operations[1], "meaningful-event",
    "the meaningful event precedes every deferred image connector");
while (chronological.scheduledWork.length) chronological.scheduledWork.shift()();
equal(chronological.dom.rows, chronological.entries.map(function (entry) { return entry.id; }),
    "all 200 entries finish in chronological DOM order");
ok(chronological.dom.scrollTop > 100, "prepended older chunks preserve the visible scroll anchor");
const taskNotes = chronological.notes.filter(function (row) { return row.name === "render.task"; });
equal(taskNotes.length, chronological.chunks.length,
    "every real insertion is measured as one observable render task");
ok(taskNotes.every(function (row) { return typeof row.data.durationMs === "number"; }),
    "every task records its actual clock duration");
equal(taskNotes.filter(function (row) { return row.data.overBudget; }).length, 1,
    "the controlled 60ms task is truthfully marked over budget");
ok(chronological.operations.includes("complete"), "completion is meaningful and observable");

const newest = renderHarness(true, null);
newest.paintWork.shift()();
while (newest.scheduledWork.length) newest.scheduledWork.shift()();
equal(newest.dom.rows, newest.entries.slice().reverse().map(function (entry) { return entry.id; }),
    "newest-first mode appends later chunks without reversing authored order");

const cancelled = renderHarness(false, null);
cancelled.cancel();
cancelled.paintWork.shift()();
equal(cancelled.scheduledWork.length, 0, "a switched session cancels every later render task");
equal(cancelled.operations.filter(function (row) { return row === "meaningful-event"; }).length, 0,
    "a cancelled render cannot announce a meaningful paint for the new session");
equal(cancelled.operations.filter(function (row) { return row.startsWith("images:"); }).length, 0,
    "a cancelled render cannot start an image connector");

const retrySchedules = [];
const retryLoads = [];
const observer = createTranscriptRevisionObserver(function (id, revision, quiet, demand) {
    retryLoads.push({ id, revision, quiet, demand });
}, { schedule: function (work, delay) {
    const timer = { work, delay }; retrySchedules.push(timer); return timer;
}, cancel: function () {} });
observer.observe("busy", "r1", false);
observer.settle("busy", "r1", false, { code: "transcript_busy", retry_after: 2 });
equal(retrySchedules[0].delay, 2000, "client recovery honors the server retry-after debt receipt");
retrySchedules.shift().work();
equal(retryLoads.length, 2, "capacity recovery does not prematurely exhaust ordinary attempts");
equal(retryLoads[1].demand.foreground, true,
    "a refused first-open keeps its foreground reservation throughout debt recovery");

const testScript = readFileSync(new URL("../test.sh", import.meta.url), "utf8");
ok(/browser_contract_suites=\([\s\S]*Tests\/web-transcript-requests\.mjs[\s\S]*\)/.test(testScript),
    "the exact full-suite browser roster contains this guard");
// The count is read out of the roster rather than written here a second time. It was `-ne 15`
// until a sixteenth suite landed and updated `test.sh` alone: two records of one number, agreeing
// with each other right up until one of them moved. Deriving it keeps what this line is for — the
// guard in test.sh is armed, so deleting an entry from the roster still goes red — while removing
// the copy that can drift.
const rosterEntries = /browser_contract_suites=\(([\s\S]*?)\)/.exec(testScript)?.[1] ?? "";
const rosterCount = rosterEntries.trim().split(/\s+/).filter(Boolean).length;
ok(rosterCount > 0, "the browser roster was found and is not empty");
ok(new RegExp(`\\$\\{#browser_contract_suites\\[@\\]\\}[^\\n]*-ne ${rosterCount}`).test(testScript),
    "the roster count guard names the roster's own length, so deleting an entry goes red");
const apiDocs = readFileSync(new URL("../docs/api.md", import.meta.url), "utf8");
ok(apiDocs.includes("transcript_busy") && apiDocs.includes("retry_after"),
    "API docs name typed transcript saturation and bounded retry guidance");
ok(apiDocs.includes("/info?parts=summary") && apiDocs.includes("full payload"),
    "API docs close the summary-versus-full Info contract");

/* ---- which row changes may cost a transcript read ------------------------------------------
 *
 * Measured on 2026-09-15 on the phone the Cloud console heats: 472 transcript reads in fourteen
 * hours, 223 of them byte-identical to the read before, because the row's `line` — the assistant's
 * running status bar — was part of the revision that triggers a read. The policy is held here as a
 * pure function; the page's own modules are driven through it in the child process below.
 * -------------------------------------------------------------------------- */

equal(TRANSCRIPT_LINE_REREAD_MS, 15000, "a legacy row's line may cost at most one read per 15 s");
equal([transcriptSignatureOf({ transcript_signature: "a" }), transcriptSignatureOf({ transcript_signature: "" }),
    transcriptSignatureOf({}), transcriptSignatureOf(null)], ["a", null, null, null],
    "only a non-empty string is a transcript signature");

(function () {
    const policy = createTranscriptRefetchPolicy({ now: function () { return 0; } });
    const base = { state: "working", label: "One", line: "Working (1s)", transcript_signature: "s1" };
    const first = policy.revision("k", base);
    const lines = [];
    for (let i = 2; i <= 30; i += 1) {
        lines.push(policy.revision("k", Object.assign({}, base, { line: "Working (" + i + "s)", label: "L" + i })));
    }
    equal(lines.filter(function (r) { return r !== first; }).length, 0,
        "with a signature, neither line nor label moves the revision");
    ok(policy.revision("k", Object.assign({}, base, { transcript_signature: "s2" })) !== first,
        "a changed signature does");
    ok(policy.revision("k", Object.assign({}, base, { state: "idle" })) !== first,
        "and so does the state edge that ends a turn");
    equal(policy.holding("k"), false, "a signed row holds no timer");
})();

(function () {
    let now = 0;
    const scheduled = [];
    const due = [];
    const policy = createTranscriptRefetchPolicy({
        now: function () { return now; },
        schedule: function (work, delay) { const t = { work, delay, cancelled: false }; scheduled.push(t); return t; },
        cancel: function (t) { t.cancelled = true; },
        due: function (key) { due.push(key); }
    });
    const row = function (changes) {
        return Object.assign({ state: "working", label: "One", line: "Working (0s)" }, changes);
    };
    const revisions = new Set([policy.revision("k", row())]);
    for (let second = 1; second <= 30; second += 1) {
        now = second * 1000;
        revisions.add(policy.revision("k", row({ line: "Working (" + second + "s)" })));
    }
    equal(revisions.size, 3,
        "thirty seconds of line-only changes are at most one revision per fifteen seconds");
    now = 31_000;
    const heldAt = policy.revision("k", row({ line: "Working (31s)" }));
    now = 32_000;
    equal(policy.revision("k", row({ line: "Working (32s)" })), heldAt, "a second held line is still held");
    equal(scheduled.filter(function (t) { return !t.cancelled; }).length, 1,
        "and the lines held inside one interval own exactly one pending timer");
    const held = scheduled.filter(function (t) { return !t.cancelled; })[0];
    equal(held.delay, TRANSCRIPT_LINE_REREAD_MS - 1000, "due when the interval since the last read is up");
    now = 31_000 + held.delay;
    held.work();
    equal(due, ["k"], "the held line comes due by itself, so a line that stops changing is still read");
    ok(policy.revision("k", row({ line: "Working (32s)" })) !== heldAt,
        "and when it is due, the next look at the row accepts it");
    now += 1;
    const beforeEdge = policy.peek("k", row({ line: "Working (32s)" }));
    ok(policy.revision("k", row({ state: "idle", line: "" })) !== beforeEdge,
        "a state change is read at once, however recently the line was");
    equal(policy.peek("k", row({ state: "idle", line: "" })), policy.revision("k", row({ state: "idle", line: "" })),
        "peek names the revision without accepting anything");
    policy.forget("k");
    equal(policy.holding("k"), false, "forgetting a session cancels its held line");
})();

/* ---- the page's clocks, hidden and visible -------------------------------------------------- */

function fakeVisibility() {
    const env = { hiddenNow: false, listeners: [], intervals: [], frames: [], timeouts: [], now: 0 };
    env.api = {
        hidden: function () { return env.hiddenNow; },
        subscribe: function (fn) {
            env.listeners.push(fn);
            return function () { env.listeners = env.listeners.filter(function (x) { return x !== fn; }); };
        },
        now: function () { return env.now; },
        setInterval: function (fn, ms) { const t = { fn, ms, live: true }; env.intervals.push(t); return t; },
        clearInterval: function (t) { t.live = false; },
        requestFrame: function (fn) { env.frames.push(fn); return env.frames.length; },
        setTimeout: function (fn, ms) { const t = { fn, ms, live: true }; env.timeouts.push(t); return t; },
        clearTimeout: function (t) { if (t) t.live = false; }
    };
    env.set = function (hidden) { env.hiddenNow = hidden; env.listeners.slice().forEach(function (fn) { fn(); }); };
    env.live = function () { return env.intervals.filter(function (t) { return t.live; }); };
    return env;
}

(function () {
    const env = fakeVisibility();
    let runs = 0;
    const clock = createVisibleInterval(function () { runs += 1; }, 1000, { environment: env.api });
    clock.start();
    equal(env.live().length, 1, "a visible page arms the clock");
    env.live()[0].fn();
    equal(runs, 1, "and a tick does its work");
    const queued = env.live()[0];
    env.set(true);
    equal([env.live().length, clock.armed()], [0, false], "hidden takes the timer itself down");
    queued.fn();
    equal(runs, 1, "a tick already queued when the page went away does no work");
    env.now = 5000;
    env.set(false);
    equal([runs, env.live().length], [2, 1], "coming back catches up once, then arms again");
    env.set(false);
    equal(env.live().length, 1, "a repeated visible event does not arm a second timer");
    clock.stop();
    equal(env.live().length, 0, "stop disarms");
    env.set(true); env.set(false);
    equal([runs, env.live().length], [2, 0], "and a stopped clock ignores visibility");

    const spin = fakeVisibility();
    let turns = 0;
    const spinner = createVisibleInterval(function () { turns += 1; }, 125, { catchUp: false, environment: spin.api });
    spinner.start();
    spin.set(true);
    spin.now = 10_000;
    spin.set(false);
    equal([turns, spin.live().length], [0, 1], "catchUp: false comes back without replaying a tick");

    const hiddenStart = fakeVisibility();
    hiddenStart.hiddenNow = true;
    createVisibleInterval(function () {}, 1000, { environment: hiddenStart.api }).start();
    equal(hiddenStart.live().length, 0, "a clock started on a hidden page arms nothing until it is visible");
})();

(function () {
    const env = fakeVisibility();
    let draws = 0;
    const frame = createFrameCoalescer(function () { draws += 1; }, { environment: env.api });
    for (let i = 0; i < 12; i += 1) frame.request();
    equal([env.frames.length, frame.pending()], [1, true], "twelve requests ask for one frame");
    env.frames.shift()();
    equal([draws, frame.pending()], [1, false], "and draw once");
    env.set(true);
    frame.request(); frame.request();
    equal(env.frames.length, 0, "a hidden page asks for no frame at all");
    equal(frame.pending(), true, "but the draw is still owed");
    env.set(false);
    equal(env.frames.length, 1, "visible again, the owed draw asks for one frame");
    env.frames.shift()();
    equal(draws, 2, "and it draws once");
    frame.request();
    env.set(true);
    env.frames.shift()();
    equal(draws, 2, "a frame that fires after the page was hidden defers its draw instead");
    env.set(false);
    env.frames.shift()();
    equal(draws, 3, "and draws when the page is back");

    const frozen = fakeVisibility();
    let frozenDraws = 0;
    const stuck = createFrameCoalescer(function () { frozenDraws += 1; }, { environment: frozen.api });
    stuck.request(); stuck.request();
    const owedTimer = frozen.timeouts.filter(function (t) { return t.live; });
    equal(owedTimer.map(function (t) { return t.ms; }), [FRAME_FALLBACK_MS],
        "a pending draw keeps one fallback timer, for a visible page that gets no frames");
    owedTimer[0].live = false;
    owedTimer[0].fn();
    equal([frozenDraws, stuck.pending()], [1, false], "which draws once when the frame never comes");
    frozen.frames.shift()();
    equal(frozenDraws, 1, "and the late frame after it draws nothing more");
    stuck.request();
    frozen.frames.shift()();
    equal([frozenDraws, frozen.timeouts.filter(function (t) { return t.live; }).length], [2, 0],
        "a frame that does come takes its fallback timer down");

    const later = fakeVisibility();
    later.hiddenNow = true;
    let ran = 0;
    whenVisible(function () { ran += 1; }, later.api);
    equal(ran, 0, "whenVisible waits on a hidden page");
    later.set(false); later.set(false);
    equal(ran, 1, "and runs once when it is visible");
})();

/* ---- no page clock without a pause --------------------------------------------------------------
 *
 * A bare `setInterval` keeps running while the page is hidden, and every one of them the page
 * started was found doing work on a phone in a pocket. So a new one is refused unless it is named
 * here with the reason it must keep time while nobody is looking. The Cloud transport's own clocks
 * (`net/cloud-*.js`, `net/schedules.js`) belong to that layer and its suites, and the fixtures in
 * `net/mock.js` never ship to a phone.
 * -------------------------------------------------------------------------- */

const PAGE_CLOCK_EXCEPTIONS = {
    // Enforces the recording limit and stops a live microphone; it must keep counting.
    "input/voice.js": 1,
    // The pairing door's expiry countdown, drawn only on the locked door screen.
    "door/door.js": 1,
    // The local transport's reconnect countdown label; Cloud never runs it.
    "net/live.js": 1,
    // The one place intervals are created on purpose, behind the hidden/visible switch.
    "core/visibility.js": 1
};
function pageClockSites(sources) {
    return Object.keys(sources).map(function (file) {
        const code = sources[file].replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:"'])\/\/.*$/gm, "$1");
        return { file: file, count: (code.match(/(^|[^.\w])setInterval\s*\(/g) || []).length };
    }).filter(function (site) { return site.count > 0; });
}
const pageRoot = new URL("../Resources/web/app/js/", import.meta.url);
const pageSources = {};
(function walk(dir) {
    for (const entry of readdirSync(new URL(dir, pageRoot), { withFileTypes: true })) {
        const relative = dir + entry.name;
        if (entry.isDirectory()) { if (entry.name !== "vendor") walk(relative + "/"); continue; }
        if (!entry.name.endsWith(".js")) continue;
        if (/^net\/(cloud-[^/]*|schedules|mock)\.js$/.test(relative)) continue;
        pageSources[relative] = readFileSync(new URL(relative, pageRoot), "utf8");
    }
})("");
ok(Object.keys(pageSources).length > 50 && pageSources["main.js"] && pageSources["core/pixels.js"],
    "the page-clock scan read the page's modules, not an empty directory");
const unpaused = pageClockSites(pageSources).filter(function (site) {
    return PAGE_CLOCK_EXCEPTIONS[site.file] !== site.count;
});
equal(unpaused, [], "every page clock pauses while hidden, or is named with its reason");
equal(pageClockSites({ "view/x.js": "// setInterval(a, 1)\nvar t = setInterval(tick, 1000);" }),
    [{ file: "view/x.js", count: 1 }], "the scan sees a bare interval and not one in a comment");
equal(pageClockSites({ "view/x.js": "clock.setInterval(tick, 1)" }), [],
    "and does not mistake an injected environment's method for one");

/* ---- the infinite animations move layers, not pixels ---------------------------------------- */

const css = function (name) {
    return readFileSync(new URL("../Resources/web/app/css/" + name, import.meta.url), "utf8");
};
const keyframesOf = function (sheet, name) {
    const match = new RegExp("@keyframes " + name + " \\{([^{}]*(?:\\{[^{}]*\\}[^{}]*)*)\\}").exec(sheet);
    return match ? match[1] : "";
};
const transcriptCSS = css("transcript.css");
const listCSS = css("list.css");
const liveSweep = /\.entry\.toolrow\[data-live\] \.toolline::after \{([^}]*)\}/.exec(transcriptCSS);
ok(liveSweep && /animation:\s*live-sweep 2\.2s linear infinite/.test(liveSweep[1]) &&
    /animation-delay:\s*var\(--live-sweep-delay/.test(liveSweep[1]),
    "the live run's sweep runs `live-sweep`, phased by the page-wide delay");
ok(/transform/.test(keyframesOf(transcriptCSS, "live-sweep")) &&
    !/background-position/.test(keyframesOf(transcriptCSS, "live-sweep")),
    "and `live-sweep` is a transform the compositor carries, not a background repaint");
ok(/transform/.test(keyframesOf(listCSS, "sheen")) && !/background-position/.test(keyframesOf(listCSS, "sheen")),
    "the skeleton sheen is a transform too");
ok(/@media \(prefers-reduced-motion: reduce\) \{\s*\.entry\.folded\[data-live\] \.pill::after,\s*\.entry\.toolrow\[data-live\] \.toolline::after \{\s*animation: none;/.test(transcriptCSS),
    "reduced motion still stops the sweep and keeps the still wash");
ok(/\.skel \.bar::after \{ animation: none; display: none; \}/.test(css("responsive.css")),
    "and still takes the sheen off the skeleton");
ok(/setProperty\("--live-sweep-delay"/.test(readFileSync(
    new URL("../Resources/web/app/js/view/transcript.js", import.meta.url), "utf8")),
    "a redraw sets the sweep's phase instead of starting it again");

/* ---- the page itself: transcript demand, one draw per burst, hidden clocks ------------------- */

for (const mode of ["signature", "legacy", "burst", "clocks"]) {
    const run = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
        cwd: process.cwd(), encoding: "utf8", timeout: 60_000,
        env: { ...process.env, CLAWDLINE_VIEW_QUIET_BEHAVIOR: mode }
    });
    equal(run.status, 0, "view quiet scenario " + mode + ": " + (run.stderr || run.stdout || String(run.error)).trim());
    const done = /view quiet (\w+) passed \((\d+) checks\)/.exec(run.stdout || "");
    ok(done && done[1] === mode && Number(done[2]) > 0,
        "view quiet scenario " + mode + " reached its last assertion: " + (run.stdout || "").trim());
    checks += done ? Number(done[2]) : 0;
}

console.log(`web transcript priority/cache/scheduler contracts passed (${checks} checks)`);

/**
 * A page small enough to hold in a test and real enough for `net/handlers.js`, `view/list.js`,
 * `session/open.js`, `core/pixels.js` and `input/status-line.js` to run against unmodified. Every
 * element remembers what was written to it, so "nothing was redrawn" is a count, not a hope.
 */
async function viewQuietBehavior(mode) {
    let checks = 0;
    const equal = function (actual, expected, message) { assert.deepEqual(actual, expected, message); checks += 1; };
    const ok = function (value, message) { assert.ok(value, message); checks += 1; };
    const counters = { innerHTML: 0, textContent: 0, append: 0, canvasWidth: 0, rects: 0, spins: 0 };
    const noop = function () {};

    class FakeElement {
        constructor(tag, kind) {
            this.tagName = String(tag || "div").toUpperCase();
            this.kind = kind || "";
            this.children = []; this.parentNode = null; this.dataset = {}; this.attributes = {};
            this.listeners = {}; this.slots = {}; this._classes = new Set();
            this._text = ""; this._html = ""; this._width = 0; this._height = 0;
            this.hidden = false; this.disabled = false; this.isConnected = true;
            this.id = ""; this.title = ""; this.value = ""; this.lang = "en";
            this.scrollTop = 0; this.scrollHeight = 0; this.clientHeight = 0;
            this.offsetTop = 0; this.offsetWidth = 0; this.offsetHeight = 0;
            this.style = { setProperty: noop, removeProperty: noop, getPropertyValue: function () { return ""; } };
        }
        get className() { return Array.from(this._classes).join(" "); }
        set className(value) { this._classes = new Set(String(value).split(/\s+/).filter(Boolean)); }
        get classList() {
            const set = this._classes;
            return {
                add: function () { Array.from(arguments).forEach(function (n) { set.add(n); }); },
                remove: function () { Array.from(arguments).forEach(function (n) { set.delete(n); }); },
                toggle: function (n, on) { const want = on === undefined ? !set.has(n) : !!on; if (want) set.add(n); else set.delete(n); return want; },
                contains: function (n) { return set.has(n); }
            };
        }
        get textContent() { return this._text; }
        set textContent(value) { counters.textContent += 1; this._text = String(value); this.slots = {}; this.children = []; }
        get innerHTML() { return this._html; }
        set innerHTML(value) { counters.innerHTML += 1; this._html = String(value); this.slots = {}; this.children = []; }
        get width() { return this._width; }
        set width(value) { counters.canvasWidth += 1; this._width = value; }
        get height() { return this._height; }
        set height(value) { this._height = value; }
        get firstChild() { return this.children[0] || null; }
        get firstElementChild() { return this.children[0] || null; }
        get lastChild() { return this.children[this.children.length - 1] || null; }
        get childNodes() { return this.children; }
        appendChild(child) {
            counters.append += 1;
            if (child.parentNode) child.parentNode.removeChild(child);
            child.parentNode = this; this.children.push(child); return child;
        }
        insertBefore(child, ref) {
            counters.append += 1;
            if (child.parentNode) child.parentNode.removeChild(child);
            const at = ref ? this.children.indexOf(ref) : -1;
            child.parentNode = this;
            if (at < 0) this.children.push(child); else this.children.splice(at, 0, child);
            return child;
        }
        removeChild(child) {
            const at = this.children.indexOf(child);
            if (at >= 0) this.children.splice(at, 1);
            child.parentNode = null; return child;
        }
        replaceChild(next, old) { this.insertBefore(next, old); return this.removeChild(old); }
        remove() { if (this.parentNode) this.parentNode.removeChild(this); }
        prepend(child) { this.insertBefore(child, this.firstChild); }
        append(child) { this.appendChild(child); }
        querySelector(selector) {
            return this.slots[selector] || (this.slots[selector] = new FakeElement("span", selector));
        }
        querySelectorAll() { return []; }
        closest() { return null; }
        contains() { return false; }
        matches() { return false; }
        addEventListener(name, fn) { (this.listeners[name] = this.listeners[name] || []).push(fn); }
        removeEventListener() {}
        dispatchEvent(event) { (this.listeners[event.type] || []).forEach(function (fn) { fn(event); }); return true; }
        setAttribute(name, value) { this.attributes[name] = String(value); }
        getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name) ? this.attributes[name] : null; }
        removeAttribute(name) { delete this.attributes[name]; }
        hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name); }
        toggleAttribute(name, on) { if (on) this.attributes[name] = ""; else delete this.attributes[name]; }
        insertAdjacentHTML() {}
        focus() {} blur() {} click() {} select() {} scrollIntoView() {} scrollTo() {}
        showModal() {} show() {} close() {}
        setPointerCapture() {} releasePointerCapture() {}
        getBoundingClientRect() { counters.rects += 1; return { top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0 }; }
        getClientRects() { return []; }
        animate() { return { cancel: noop, finish: noop }; }
        cloneNode() { return new FakeElement(this.tagName); }
        get content() { return new FakeElement("template-content"); }
        getContext() {
            const canvas = this;
            return {
                clearRect: function () { if (canvas.kind === ".spin" || canvas.kind === "spin") counters.spins += 1; },
                fillRect: noop, drawImage: noop, imageSmoothingEnabled: false, fillStyle: ""
            };
        }
    }

    const byElementId = {};
    const visibility = [];
    const doc = new FakeElement("document");
    doc.hidden = false;
    doc.visibilityState = "visible";
    doc.documentElement = new FakeElement("html");
    doc.body = new FakeElement("body");
    doc.head = new FakeElement("head");
    doc.activeElement = null;
    doc.getElementById = function (id) {
        return byElementId[id] || (byElementId[id] = Object.assign(new FakeElement("div"), { id: id }));
    };
    doc.createElement = function (tag) { return new FakeElement(tag); };
    doc.createTextNode = function (text) { const n = new FakeElement("#text"); n._text = String(text); return n; };
    doc.querySelector = function () { return null; };
    doc.querySelectorAll = function () { return []; };
    doc.elementFromPoint = function () { return null; };
    doc.elementsFromPoint = function () { return []; };
    doc.addEventListener = function (name, fn) { if (name === "visibilitychange") visibility.push(fn); };
    doc.removeEventListener = function (name, fn) {
        const at = visibility.indexOf(fn);
        if (at >= 0) visibility.splice(at, 1);
    };
    doc.dispatchEvent = function () { return true; };
    const setHidden = function (hidden) {
        doc.hidden = hidden;
        doc.visibilityState = hidden ? "hidden" : "visible";
        visibility.slice().forEach(function (fn) { fn({ type: "visibilitychange" }); });
    };

    const frames = [];
    const intervals = [];
    const install = function (name, value) {
        Object.defineProperty(globalThis, name, { value: value, configurable: true, writable: true });
    };
    install("document", doc);
    install("window", {
        devicePixelRatio: 1, innerHeight: 800, innerWidth: 1280, scrollX: 0, scrollY: 0,
        visualViewport: { height: 800, width: 1280, offsetTop: 0, offsetLeft: 0, scale: 1, addEventListener: noop },
        matchMedia: function () { return { matches: false, addEventListener: noop, addListener: noop, removeListener: noop }; },
        addEventListener: noop, removeEventListener: noop, dispatchEvent: noop,
        getComputedStyle: function () { return { opacity: "1", marginLeft: "0", getPropertyValue: function () { return ""; } }; }
    });
    install("getComputedStyle", window.getComputedStyle);
    install("requestAnimationFrame", function (fn) { frames.push(fn); return frames.length; });
    install("cancelAnimationFrame", noop);
    install("setInterval", function (fn, ms) { const t = { fn: fn, ms: ms, live: true }; intervals.push(t); return t; });
    install("clearInterval", function (t) { if (t) t.live = false; });
    install("localStorage", { getItem: function () { return null; }, setItem: noop, removeItem: noop });
    install("sessionStorage", { getItem: function () { return null; }, setItem: noop, removeItem: noop });
    install("location", { search: "", hash: "", pathname: "/", protocol: "http:", hostname: "localhost",
        host: "localhost", origin: "http://localhost", href: "http://localhost/" });
    install("history", { pushState: noop, replaceState: noop, state: null });
    install("navigator", { userAgent: "node", maxTouchPoints: 0, languages: ["en"], platform: "MacIntel" });
    install("MutationObserver", class { observe() {} disconnect() {} });
    install("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
    install("IntersectionObserver", class { observe() {} disconnect() {} unobserve() {} });
    install("HTMLElement", FakeElement);
    install("Element", FakeElement);

    let clock = 1_000_000;
    const realNow = Date.now;
    Date.now = function () { return clock; };

    const settle = async function () { for (let i = 0; i < 6; i += 1) await new Promise(function (r) { setImmediate(r); }); };
    const flushFrames = async function () {
        for (let round = 0; round < 6 && frames.length; round += 1) {
            frames.splice(0).forEach(function (fn) { fn(0); });
            await settle();
        }
        await settle();
    };

    const js = "../Resources/web/app/js/";
    const { useApi } = await import(js + "net/api.js");
    const { S } = await import(js + "core/state.js");
    const { bindSessionUI } = await import(js + "session/ui.js");
    const open = await import(js + "session/open.js");
    const list = await import(js + "view/list.js");
    const { handlers, viewDrawPending } = await import(js + "net/handlers.js");
    const { SessionSelection } = await import(js + "session/selection.js");
    const pixels = await import(js + "core/pixels.js");
    const { StatusLine } = await import(js + "input/status-line.js");

    const reads = [];
    const infoReads = [];
    let answerSignature = "sig-1";
    useApi({
        transcript: function (route, hooks, demand) {
            reads.push({ route: route, demand: demand, at: clock });
            return Promise.resolve({ entries: [{ role: "assistant", text: "hello", at: 1 }], signature: answerSignature });
        },
        infoSummary: function (route) { infoReads.push({ tier: "summary", route: route }); return Promise.resolve({ info: { session: {} } }); },
        info: function (route) { infoReads.push({ tier: "full", route: route }); return Promise.resolve({ info: { session: {} } }); }
    });

    const counts = { renderTranscript: 0, renderList: 0 };
    let workingChanged = false;
    bindSessionUI({
        renderTranscript: function () { counts.renderTranscript += 1; },
        transcriptWorkingChanged: function () { return workingChanged; },
        observeTranscriptRow: open.observeTranscriptRow,
        rearmTranscriptRow: open.rearmTranscriptRow,
        openSession: open.openSession,
        closeDetail: open.closeDetail,
        render: list.render,
        renderList: list.renderList,
        arrangeStartRows: function (rows) { counts.renderList += 1; return rows; },
        startArriving: function () { return false; },
        startPlaceholder: function () { return null; },
        checkStart: function () { return false; },
        openWanted: function () { return false; },
        wantedSession: function () { return null; },
        setWantedSession: noop, resetSwipeRows: noop, renderDetailHead: noop, renderAgentHead: noop,
        renderComposer: noop, renderWaiting: noop, renderAgents: noop, followInfo: noop,
        followStatusLine: noop, syncSessionBoard: noop, observeBoardSession: noop,
        agentsRev: function () { return ""; }, agentRow: function () { return null; }, loadAgent: noop,
        agentTokens: function () { return ""; }, closingSelectionKey: function () { return null; },
        closeSessionActions: noop, closeActionConfirm: noop, closeAgent: noop, clearShots: noop,
        deferStatusLine: noop, resumeStatusLine: noop, followSessionBoard: noop, resumeSessionBoard: noop,
        followSnippets: noop, followGitPanel: noop, followShellPanel: noop, followTerminal: noop,
        skillPickerChanged: noop, skillPickerClose: noop, rowNode: function () { return null; },
        sessionGone: noop, syncActionConfirm: noop, syncStart: noop, fillSuggestedReply: noop
    });

    const row = function (changes) {
        return Object.assign({
            id: "s1", machine: "mac-1", session: "s1", identity: { machine: "mac-1", session: "s1" },
            state: "working", label: "One", line: "Working (0s)", cwd: "/repo/one", assistant: "claude"
        }, changes);
    };
    const scan = { generation: 1, complete: true, emptyAuthoritative: false };
    const frame = async function (rows) {
        handlers.sessions(rows, clock / 1000, scan);
        await flushFrames();
    };

    S.conn = "live";
    S.write = false;

    if (mode === "signature" || mode === "legacy" || mode === "burst") {
        const signed = mode !== "legacy";
        const sig = function (value) { return signed ? { transcript_signature: value } : {}; };
        await frame([row(sig("sig-1"))]);
        equal(reads.length, 1, mode + ": the first list opens the top session and reads its transcript once");
        const selected = SessionSelection.snapshot().open;
        ok(selected && selected.rowId === "s1", mode + ": the session is open");
        equal(S.tx.signature, "sig-1", mode + ": its answer is on screen");
    }

    if (mode === "signature") {
        const rendersAtStart = counts.renderTranscript;
        for (let second = 1; second <= 30; second += 1) {
            clock += 1000;
            await frame([row({ line: "Working (" + second + "s)", transcript_signature: "sig-1" })]);
        }
        equal(reads.length, 1,
            "(i) thirty frames that change only `line` read nothing while the signature is unchanged");
        equal(counts.renderTranscript, rendersAtStart, "and redraw nothing");

        answerSignature = "sig-2";
        await frame([row({ line: "Working (31s)", transcript_signature: "sig-2" })]);
        equal(reads.length, 2, "(ii) a changed signature reads exactly once");
        equal(counts.renderTranscript, rendersAtStart + 1, "and draws the new transcript");
        await frame([row({ line: "Working (32s)", transcript_signature: "sig-2" })]);
        equal(reads.length, 2, "the same new signature again reads nothing more");

        const rendersBeforeEdge = counts.renderTranscript;
        workingChanged = false;
        await frame([row({ state: "idle", line: "", transcript_signature: "sig-2" })]);
        equal(reads.length, 3, "the turn's end is read at once, ahead of the Mac's debounced signature");
        equal(counts.renderTranscript, rendersBeforeEdge,
            "(iii) an answer whose signature is already on screen is not redrawn");
        await frame([row({ state: "idle", line: "", transcript_signature: "sig-2" })]);
        equal(reads.length, 3, "and that state, repeated, reads nothing");

        workingChanged = true;
        await frame([row({ state: "working", line: "Working (1s)", transcript_signature: "sig-2" })]);
        equal(reads.length, 4, "a turn starting again is one read");
        equal(counts.renderTranscript, rendersBeforeEdge + 1,
            "and the same signature is redrawn when the live sweep it draws has changed");
        workingChanged = false;

        answerSignature = "sig-3";
        await open.loadTranscript(SessionSelection.snapshot().open.key, true);
        await settle();
        equal([reads.length, S.tx.signature], [5, "sig-3"],
            "a read from somewhere else (the send follower, a file event) puts sig-3 on screen");
        await frame([row({ state: "working", line: "Working (2s)", transcript_signature: "sig-3" })]);
        equal(reads.length, 5, "a row that then names sig-3 has nothing left to read");

        S.tx.error = "held";
        answerSignature = "sig-3";
        await open.loadTranscript(SessionSelection.snapshot().open.key, true);
        await settle();
        equal([S.tx.error, counts.renderTranscript > rendersBeforeEdge + 2], [null, true],
            "an unchanged answer after a failure takes the failure line down and redraws for it");
    }

    if (mode === "legacy") {
        const rendersAtStart = counts.renderTranscript;
        for (let second = 1; second <= 30; second += 1) {
            clock += 1000;
            await frame([row({ line: "Working (" + second + "s)" })]);
        }
        const extra = reads.length - 1;
        ok(extra >= 1 && extra <= Math.ceil(30_000 / TRANSCRIPT_LINE_REREAD_MS),
            "(i) without a signature, thirty seconds of line changes cost at most one read per 15 s (got " + extra + ")");
        const gaps = reads.slice(1).map(function (read, i) { return read.at - reads[i].at; });
        ok(gaps.every(function (gap) { return gap >= TRANSCRIPT_LINE_REREAD_MS; }),
            "and no two of those reads are closer than the interval: " + gaps.join(","));
        equal(counts.renderTranscript, rendersAtStart,
            "(iii) the unchanged answers those reads bring are not redrawn");
        clock += 1000;
        const beforeEdge = reads.length;
        await frame([row({ state: "idle", line: "" })]);
        equal(reads.length, beforeEdge + 1, "a state change is read at once, one second after a line read");
    }

    if (mode === "burst") {
        counts.renderList = 0;
        for (let i = 1; i <= 8; i += 1) {
            handlers.sessions([row({ line: "Working (" + i + "s)", transcript_signature: "sig-1" })], clock / 1000, scan);
        }
        equal([counts.renderList, viewDrawPending(), frames.length], [0, true, 1],
            "(iv) eight frames in one tick draw nothing yet and ask for one animation frame");
        equal(S.sessions[0].line, "Working (8s)", "while the state behind the draw is already current");
        await flushFrames();
        equal(counts.renderList, 1, "(iv) and draw the list once");

        for (let i = 0; i < 3; i += 1) handlers.tasks([]);
        handlers.sessions([row({ line: "Working (9s)", transcript_signature: "sig-1" })], clock / 1000, scan);
        await flushFrames();
        equal(counts.renderList, 2, "orchestrator frames join the same draw as the session frame beside them");

        const node = list.rowNodes[SessionSelection.snapshot().open.key];
        ok(node && node._fillKey, "the row remembers what it was drawn from");
        const before = Object.assign({}, counters);
        await frame([row({ line: "Working (9s)", transcript_signature: "sig-1" })]);
        equal(counts.renderList, 3, "an identical frame still reaches the list");
        equal([counters.innerHTML - before.innerHTML, counters.textContent - before.textContent,
            counters.append - before.append, counters.canvasWidth - before.canvasWidth,
            counters.rects - before.rects],
            [0, 0, 0, 0, 0],
            "(iv) but an unchanged row is not rewritten, moved, redrawn on canvas, or measured");

        const lineOnly = Object.assign({}, counters);
        await frame([row({ line: "Working (10s)", transcript_signature: "sig-1" })]);
        equal([counters.innerHTML - lineOnly.innerHTML, counters.textContent - lineOnly.textContent,
            counters.append - lineOnly.append, counters.canvasWidth - lineOnly.canvasWidth],
            [0, 1, 0, 0],
            "a changed line is one text write: no markup, no move, no icon repaint");

        const beforeHidden = counts.renderList;
        setHidden(true);
        for (let i = 11; i <= 14; i += 1) {
            handlers.sessions([row({ line: "Working (" + i + "s)", transcript_signature: "sig-1" })], clock / 1000, scan);
        }
        equal(frames.length, 0, "(v) a hidden page asks for no animation frame");
        await flushFrames();
        equal(counts.renderList, beforeHidden, "and draws no list");
        setHidden(false);
        await flushFrames();
        equal(counts.renderList, beforeHidden + 1, "coming back draws the frames it held, once");
    }

    if (mode === "clocks") {
        const spinnerTimer = function () {
            return intervals.filter(function (t) { return t.live && t.ms === 125; });
        };
        equal(spinnerTimer().length, 1, "the spinner clock is armed on a visible page");
        const canvas = new FakeElement("canvas", "spin");
        pixels.setSpinners([canvas]);
        const armed = spinnerTimer()[0];
        armed.fn();
        equal(counters.spins, 1, "a visible spinner is drawn by the clock");
        setHidden(true);
        equal(spinnerTimer().length, 0, "(v) hiding the page disarms the spinner clock");
        armed.fn();
        equal(counters.spins, 1, "(v) and a tick already queued draws nothing");
        setHidden(false);
        equal([spinnerTimer().length, counters.spins], [1, 1],
            "visible again, the clock is armed without replaying missed turns");
        pixels.setSpinners([canvas], function () { return false; });
        equal(pixels.turnSpinners(), 0, "the list's spinners are not drawn while the list cannot be seen");
        pixels.setSpinners([canvas], function () { return true; });
        canvas.isConnected = false;
        equal([pixels.turnSpinners(), pixels.spinners.length], [0, 0],
            "a canvas that has left the document is dropped, not drawn");

        S.sessions = [row({ transcript_signature: "sig-1" })];
        const identity = SessionSelection.resolve(S.sessions[0], S.sessions).identity;
        SessionSelection.select(identity, S.sessions);
        SessionSelection.open(identity, S.sessions);
        StatusLine.follow();
        await settle();
        equal(infoReads.length, 1, "the status line reads once for the session it follows");
        setHidden(true);
        clock += 120_000;
        StatusLine.refresh(false);
        StatusLine.follow();
        S.sessions = [row({ state: "idle", transcript_signature: "sig-1" })];
        StatusLine.follow();
        await settle();
        equal(infoReads.length, 1,
            "(v) hidden, neither the minute clock, nor a render, nor a turn ending reads session facts");
        setHidden(false);
        StatusLine.catchUp();
        await settle();
        equal(infoReads.length, 2, "visible again, what came due is read once");
        StatusLine.catchUp();
        await settle();
        equal(infoReads.length, 2, "and only once");
    }

    Date.now = realNow;
    console.log("view quiet " + mode + " passed (" + checks + " checks)");
}

