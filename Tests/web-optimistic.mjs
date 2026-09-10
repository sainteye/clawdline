import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import {
    acceptOptimisticReceipt, authoritativeSendTime, knownOccurrences,
    matchesOptimistic as matchesOptimisticRaw,
    optimisticKey, optimisticScopeKey, optimisticSendSnapshot,
    reconcileOptimisticBeforeSignature
} from "../Resources/web/app/js/view/optimistic-data.js";
import { createTranscriptRequests } from
    "../Resources/web/app/js/session/transcript-requests.js";
import { createPendingTranscriptFollower } from
    "../Resources/web/app/js/session/transcript-requests.js";

const TEST_SCOPE = "mac-a\u0000session-1";
const pending = function (text, imageCount, at, known = {}) {
    return { text, imageCount, at, known, scopeKey: TEST_SCOPE };
};
const matchesOptimistic = function (wanted, actual, scopeKey = TEST_SCOPE, now) {
    return matchesOptimisticRaw(wanted, actual, scopeKey, now);
};

const workflowMetadata = {
    authority: "clawdline_metadata_not_user_authorization",
    board_epoch: 1,
    content_reference: "terminal-request:0123456789abcdef01234567",
    conversation_id: "11111111-1111-4111-8111-111111111111",
    coverage: "managed_ingress",
    helper: "clawdline-board-workflow <conversation-id> <stable-idempotency-key>",
    input_kind: "text_or_transcribed_voice",
    mode_gap: null,
    process_generation: "93029:1789026720.0",
    project_id: "project-0123456789abcdef01234567",
    provider: "codex",
    required_first_action: "begin",
    run_id: "run-0123456789abcdef0123456789abcdef",
    terminal_id: "%480",
    version: 1
};
const workflowTurn = "same words\n\n" +
    '<clawdline-workflow version="1" authority="metadata-not-user">\n' +
    JSON.stringify(workflowMetadata) + "\n</clawdline-workflow>";

assert.equal(matchesOptimistic(
    pending("same words", 0, 100),
    { role: "user", text: workflowTurn, imageCount: 0, at: 100 }
), true, "a validated Board workflow record is presentation metadata, not authored text");

assert.equal(matchesOptimistic(
    pending("same words", 0, 100),
    { role: "user", text: "same words", imageCount: 0, at: 100 },
    "mac-b\u0000session-1", 100
), false, "the same words and timestamp on another Mac cannot retire this pending turn");

assert.equal(matchesOptimistic(
    { ...pending("same words", 0, 100), expiresAt: 700 },
    { role: "user", text: "same words", imageCount: 0, at: 100 },
    "mac-a\u0000session-1", 701
), false, "a backgrounded timer cannot extend reconciliation beyond ten minutes");

assert.equal(matchesOptimistic(
    { ...pending("same words", 0, 100), expiresAt: 1200 },
    { role: "user", text: "same words", imageCount: 0, at: 100 },
    TEST_SCOPE, 701
), true, "Mac accepted_at never acts as a browser-local expiry clock");
assert.equal(matchesOptimisticRaw(
    { text: "same words", imageCount: 0, at: 100 },
    { role: "user", text: "same words", imageCount: 0, at: 100 },
    null, 100
), false, "missing reconciliation identity fails closed");

const scopedReceipt = {
    token: "pending-one", receiptKey: "mac-a\u0000session-1\u0000request-1"
};
const firstReceipt = acceptOptimisticReceipt([], scopedReceipt);
const replayedReceipt = acceptOptimisticReceipt(firstReceipt.entries, { ...scopedReceipt });
assert.equal(firstReceipt.inserted, true, "the first accepted receipt creates one pending turn");
assert.equal(replayedReceipt.inserted, false, "a duplicate receipt cannot create a second turn");
assert.equal(replayedReceipt.entry, scopedReceipt,
    "a replay resolves to the original pending identity instead of replacing it");
assert.equal(replayedReceipt.entries.length, 1,
    "duplicate and out-of-order receipt delivery never grows the pending ledger");
assert.deepEqual(acceptOptimisticReceipt([], { token: "unscoped" }),
    { entries: [], entry: null, inserted: false },
    "a receipt without machine, Session and request identity is not admitted");
assert.notEqual(
    optimisticScopeKey({ machine: "mac-a", session: "session-1" }),
    optimisticScopeKey({ machine: "mac-a", session: "session-2" }),
    "two Sessions on one Mac have distinct pending scopes");

const transcriptSource = readFileSync(new URL(
    "../Resources/web/app/js/view/transcript.js", import.meta.url), "utf8");
assert.match(transcriptSource, /esc\(T\.webPromptAccepted\)/,
    "a prompt shown only after the Mac accepted it says accepted, not waiting for the Mac");

assert.equal(matchesOptimistic(
    pending("look here", 1, 100),
    { role: "user", text: "look here", imageCount: 1, at: 100 }
), true, "Codex text plus a drop-cache path canonicalizes to one exact image turn");
assert.equal(matchesOptimistic(
    pending("describe this", 1, 100),
    { role: "user", text: "describe this[Image #1]", imageCount: 1, at: 100 }
), true, "a real Claude text-plus-image turn retires the pending turn");
assert.equal(matchesOptimistic(
    pending("", 1, 100),
    { role: "user", text: "[Image #1]", imageCount: 1, at: 100 }
), true, "a visible Claude image-only turn retires the matching pending turn");
assert.equal(matchesOptimistic(
    pending("describe this", 1, 100),
    { role: "user", text: "[Image #1] describe this", at: 100 }
), true, "the mock's legacy marker-only shape still retires the pending turn");
const legacyImage = { role: "user", text: "[Image #1] describe this", at: 100 };
assert.equal(knownOccurrences([legacyImage])[optimisticKey(
    { role: "user", text: "describe this", imageCount: 1, at: 100 }
)], 1, "legacy markers and current image metadata share one duplicate-occurrence key");
assert.equal(matchesOptimistic(
    pending("look", 1, 100),
    { role: "user", text: "look again", imageCount: 1, at: 100 }
), false, "image matching is exact rather than a text-prefix guess");
assert.equal(matchesOptimistic(
    pending("literal [Image #1]", 0, 100),
    { role: "user", text: "literal [Image #1]", imageCount: 0, at: 100 }
), true, "an explicit zero image count preserves an authored marker literally");

const beforeSend = knownOccurrences([]);
const slowPostStartedAt = 200;
const slowPostFinishedAt = slowPostStartedAt + 30;
const arrivedDuringPost = { role: "user", text: "slow", imageCount: 0, at: slowPostStartedAt };
assert.equal(beforeSend[optimisticKey(arrivedDuringPost)] || 0, 0,
    "an entry arriving before POST resolution was not part of the pre-send snapshot");
const acceptedSendTime = authoritativeSendTime({
    accepted_at: slowPostStartedAt,
    at: slowPostFinishedAt
}, slowPostStartedAt);
assert.equal(slowPostFinishedAt - acceptedSendTime, 30,
    "the slow-POST fixture resolves thirty seconds after the Mac accepted the send");
assert.equal(matchesOptimistic(
    pending("slow", 0, acceptedSendTime, beforeSend), arrivedDuringPost), true,
"a row arriving at handoff start retires its optimistic copy after a thirty-second POST");
assert.equal(authoritativeSendTime({ at: 200 }, 100), 200,
    "a legacy server timestamp remains authoritative when accepted_at is absent");
assert.equal(authoritativeSendTime({}, 100), 100,
    "an old server falls back to the request-start timestamp, not POST completion time");

const duplicate = { role: "user", text: "same", imageCount: 0, at: 300 };
const oneOld = knownOccurrences([duplicate]);
assert.equal(oneOld[optimisticKey(duplicate)], 1,
    "known occurrences retain one-to-one duplicate semantics");

const snapshot = optimisticSendSnapshot([duplicate], 301.9);
assert.equal(snapshot.known[optimisticKey(duplicate)], 1,
    "the pre-send snapshot owns only entries present when it is captured");
assert.equal(snapshot.startedAt, 301, "the request-start fallback is captured to whole seconds");
const ordering = [];
const reconciled = reconcileOptimisticBeforeSignature(function (id, entries) {
    ordering.push({ id, entries });
    return true;
}, "same", [duplicate]);
assert.equal(reconciled, true, "reconciliation returns whether an optimistic row retired");
assert.deepEqual(ordering, [{ id: "same", entries: [duplicate] }],
    "same-signature handling invokes reconciliation before deciding whether to repaint");

const followTimers = [];
const followReads = [];
let followNow = 0;
let followPending = true;
let failFollowRead = true;
function scheduleFollowTimer(work, delay) {
    const timer = { work, delay, cancelled: false };
    followTimers.push(timer);
    return timer;
}
function takeFollowTimer(delay) {
    const timer = followTimers.find(function (candidate) {
        return !candidate.cancelled && candidate.delay === delay;
    });
    assert.ok(timer, "a live " + delay + "ms follower timer exists");
    timer.cancelled = true;
    return timer;
}
function liveFollowTimers() {
    return followTimers.filter(function (timer) { return !timer.cancelled; });
}
const follower = createPendingTranscriptFollower(function (id) {
    followReads.push(id);
    if (failFollowRead) return Promise.reject(Object.assign(new Error("offline"), {
        code: "offline"
    }));
    followPending = false;
    return Promise.resolve();
}, function () { return followPending; }, {
    now: function () { return followNow; },
    setTimeout: scheduleFollowTimer,
    clearTimeout: function (timer) { timer.cancelled = true; }
});
follower.start("mac-a\u0000session-1");
follower.start("mac-a\u0000session-1");
assert.deepEqual(liveFollowTimers().map(function (timer) { return timer.delay; }).sort(),
    [1000, 600000],
    "duplicate receipts share one bounded transcript follow-up lane");
followNow = 1000;
takeFollowTimer(1000).work();
await new Promise(function (resolve) { setImmediate(resolve); });
assert.deepEqual(followReads, ["mac-a\u0000session-1"],
    "a follow-up is a transcript read for the exact accepted scope");
assert.ok(liveFollowTimers().some(function (timer) { return timer.delay === 2000; }),
    "a temporary offline failure backs off without becoming a prompt retry");
failFollowRead = false;
followNow = 3000;
takeFollowTimer(2000).work();
await new Promise(function (resolve) { setImmediate(resolve); });
assert.equal(followReads.length, 2,
    "reconnect performs one read-only retry and lets its authoritative answer reconcile");
assert.equal(liveFollowTimers().length, 0,
    "no timer survives after the pending turn retires");

const expiredTimers = [];
let expiredNow = 0;
let expiredReads = 0;
const expiredFollower = createPendingTranscriptFollower(function () {
    expiredReads += 1;
}, function () { return true; }, {
    now: function () { return expiredNow; },
    setTimeout: function (work, delay) {
        const timer = { work, delay, cancelled: false };
        expiredTimers.push(timer);
        return timer;
    },
    clearTimeout: function (timer) { timer.cancelled = true; }, lifetimeMs: 600000
});
expiredFollower.start("mac-a\u0000session-1");
expiredNow = 600001;
expiredTimers.find(function (timer) { return timer.delay === 600000; }).work();
await new Promise(function (resolve) { setImmediate(resolve); });
assert.equal(expiredReads, 0, "a throttled background timer cannot read past its lifetime");
assert.equal(expiredFollower.active("mac-a\u0000session-1"), false,
    "the independent deadline removes the bounded follow-up lane");
assert.equal(expiredTimers.filter(function (timer) { return !timer.cancelled; }).length, 0,
    "the bounded follow-up leaves no permanent timer");

const hungTimers = [];
const hungFollower = createPendingTranscriptFollower(function () {
    return new Promise(function () {});
}, function () { return true; }, {
    now: function () { return expiredNow; }, lifetimeMs: 600000,
    setTimeout: function (work, delay) {
        const timer = { work, delay, cancelled: false };
        hungTimers.push(timer);
        return timer;
    },
    clearTimeout: function (timer) { timer.cancelled = true; }
});
hungFollower.start("mac-a\u0000hung");
hungTimers.find(function (timer) { return timer.delay === 1000; }).work();
await new Promise(function (resolve) { setImmediate(resolve); });
hungTimers.find(function (timer) { return timer.delay === 600000; }).work();
assert.equal(hungFollower.active("mac-a\u0000hung"), false,
    "a transcript read that never settles cannot outlive the hard deadline");
assert.equal(hungFollower.start("mac-a\u0000hung"), true,
    "a later accepted send can start a fresh follower after a hung read is retired");

const reads = [];
const accepts = [];
const deferred = [];
const postPaint = [];
function fetchTranscript(id) {
    reads.push(id);
    return new Promise(function (resolve, reject) { deferred.push({ resolve, reject }); });
}
const request = createTranscriptRequests(fetchTranscript, function (id, ticket, outcome) {
    accepts.push({ id, ticket, outcome });
}, {
    afterPaint: function (work) { postPaint.push(work); }
});

const calls = [];
for (let ticket = 1; ticket <= 20; ticket++) calls.push(request("A", ticket));
await Promise.resolve();
assert.equal(reads.length, 1, "twenty refreshes begin only one active transcript GET");
deferred[0].resolve({ entries: [{ text: "old" }], signature: "old" });
await new Promise(function (resolve) { setImmediate(resolve); });
assert.equal(reads.length, 2, "the storm schedules exactly one trailing transcript GET");
assert.equal(accepts.length, 0, "the superseded active result never paints");
const newestCall = request("A", 21);
deferred[1].resolve({ entries: [{ text: "new" }], signature: "new" });
await new Promise(function (resolve) { setImmediate(resolve); });
assert.equal(reads.length, 2,
    "a revision during the trailing read cannot immediately extend the network burst");
assert.equal(accepts.length, 1, "the trailing readable result paints instead of waiting for quiet");
assert.equal(postPaint.length, 1, "one newest refresh waits behind that paint");
postPaint[0]();
assert.equal(reads.length, 3, "the newest GET begins after the paint boundary");
deferred[2].resolve({ entries: [{ text: "newest" }], signature: "newest" });
await Promise.all(calls.concat(newestCall));
await new Promise(function (resolve) { setImmediate(resolve); });
assert.equal(accepts.length, 2, "the post-paint refresh settles the newest coalesced result");
assert.equal(accepts[1].ticket, 21, "the final result belongs to the newest ticket");
assert.equal(accepts[1].outcome.value.signature, "newest", "the newest result is eventually painted");

const failures = [];
const failDeferred = [];
const failRequest = createTranscriptRequests(function () {
    return new Promise(function (resolve, reject) { failDeferred.push({ resolve, reject }); });
}, function (id, ticket, outcome) { failures.push({ id, ticket, outcome }); });
const firstFailure = failRequest("B", 1);
const latestFailure = failRequest("B", 2);
await Promise.resolve();
failDeferred[0].resolve({ signature: "stale" });
await new Promise(function (resolve) { setImmediate(resolve); });
failDeferred[1].reject(Object.assign(new Error("offline"), { code: "offline" }));
await Promise.all([firstFailure, latestFailure]);
assert.equal(failures.length, 1, "only the latest error settles a coalesced cycle");
assert.equal(failures[0].ticket, 2, "the latest error carries the newest ticket");
assert.equal(failures[0].outcome.error.code, "offline", "the latest error is preserved");

console.log("web optimistic reconciliation and transcript coalescing tests passed");
