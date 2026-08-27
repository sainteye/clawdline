import assert from "node:assert/strict";

import {
    authoritativeSendTime, knownOccurrences, matchesOptimistic, optimisticKey,
    optimisticSendSnapshot, reconcileOptimisticBeforeSignature
} from "../Resources/web/app/js/view/optimistic-data.js";
import { createTranscriptRequests } from
    "../Resources/web/app/js/session/transcript-requests.js";

const pending = function (text, imageCount, at, known = {}) {
    return { text, imageCount, at, known };
};

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

const reads = [];
const accepts = [];
const deferred = [];
function fetchTranscript(id) {
    reads.push(id);
    return new Promise(function (resolve, reject) { deferred.push({ resolve, reject }); });
}
const request = createTranscriptRequests(fetchTranscript, function (id, ticket, outcome) {
    accepts.push({ id, ticket, outcome });
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
assert.equal(reads.length, 3,
    "a revision during the trailing read schedules one final newest GET");
assert.equal(accepts.length, 0, "the trailing result cannot paint bytes from before the revision");
deferred[2].resolve({ entries: [{ text: "newest" }], signature: "newest" });
await Promise.all(calls.concat(newestCall));
assert.equal(accepts.length, 1, "only the newest coalesced result settles the transcript");
assert.equal(accepts[0].ticket, 21, "the final result belongs to the newest ticket");
assert.equal(accepts[0].outcome.value.signature, "newest", "the newest result is the one painted");

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
