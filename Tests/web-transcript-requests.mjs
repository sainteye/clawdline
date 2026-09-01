import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import {
    beginTranscriptLoad,
    createTieredSessionFacts,
    createTranscriptRequests,
    createTranscriptRevisionObserver,
    planTranscriptRenderChunks,
    scheduleTranscriptRender,
    TRANSCRIPT_LATENCY_BUDGETS
} from "../Resources/web/app/js/session/transcript-requests.js";

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
ok(/\$\{#browser_contract_suites\[@\]\}[^\n]*-ne 15/.test(testScript),
    "the roster count goes red if this suite entry is deleted");
const apiDocs = readFileSync(new URL("../docs/api.md", import.meta.url), "utf8");
ok(apiDocs.includes("transcript_busy") && apiDocs.includes("retry_after"),
    "API docs name typed transcript saturation and bounded retry guidance");
ok(apiDocs.includes("/info?parts=summary") && apiDocs.includes("full payload"),
    "API docs close the summary-versus-full Info contract");

console.log(`web transcript priority/cache/scheduler contracts passed (${checks} checks)`);
