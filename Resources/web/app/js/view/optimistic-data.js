import { parseBoardWorkflowRecord } from "./board-workflow-record.js";

export const OPTIMISTIC_LIFETIME_SECONDS = 10 * 60;

/** Browser-local monotonic time. Mac transcript timestamps live in a different clock domain. */
export function optimisticClockSeconds() {
    if (typeof performance !== "undefined" && typeof performance.now === "function") {
        return performance.now() / 1000;
    }
    return Date.now() / 1000;
}

/** The transport-proved Mac/Session pair. Null means the evidence is insufficient. */
export function optimisticScopeKey(identity) {
    if (!identity || typeof identity.machine !== "string" || !identity.machine ||
        typeof identity.session !== "string" || !identity.session) return null;
    return identity.machine + "\u0000" + identity.session;
}

export function optimisticExpired(entry, now) {
    var deadline = Number(entry && entry.expiresAt);
    var observed = Number(now);
    return Number.isFinite(deadline) && deadline > 0 && Number.isFinite(observed) &&
        observed > deadline;
}

/** Idempotently admit one successful transport receipt into a browser-local pending ledger. */
export function acceptOptimisticReceipt(entries, candidate) {
    var held = Array.isArray(entries) ? entries : [];
    var key = candidate && candidate.receiptKey;
    if (!key) return { entries: held, entry: null, inserted: false };
    for (var i = 0; i < held.length; i++) {
        if (held[i] && held[i].receiptKey === key) {
            return { entries: held, entry: held[i], inserted: false };
        }
    }
    return { entries: held.concat([candidate]), entry: candidate, inserted: true };
}

/** Pure canonical contract shared by optimistic bookkeeping and its Node fixture. */
export function canonicalOptimisticEntry(entry) {
    entry = entry || {};
    var text = String(entry.text == null ? "" : entry.text);
    // The transcript keeps the broker-authored Board record losslessly, while the renderer
    // presents only the words the person supplied. Reconciliation must compare that same
    // validated presentation or a successfully delivered managed message can never converge.
    // The parser is closed-schema and user-role-only, so quoted or malformed lookalikes remain
    // authored text and cannot make a merely similar turn disappear.
    var workflow = parseBoardWorkflowRecord(text, "user");
    if (workflow) text = workflow.text;
    var explicitCount = Object.prototype.hasOwnProperty.call(entry, "imageCount");
    var imageCount = Number(entry.imageCount || 0);
    var markers = text.match(/\[Image #\d+\]/g) || [];
    // Current rows carry imageCount. The mock and old servers carry only Claude's markers.
    // An explicit zero means the marker is authored prose and must remain literal.
    if (imageCount > 0 || (!explicitCount && markers.length)) {
        text = text.replace(/\[Image #\d+\]\s*/g, "").trim();
        if (!explicitCount) imageCount = markers.length;
    }
    return { text: text, imageCount: imageCount };
}

export function optimisticKey(entry) {
    var canonical = canonicalOptimisticEntry(entry);
    return String(entry && entry.at || 0) + "\u0001"
        + String(canonical.imageCount) + "\u0001" + canonical.text;
}

export function knownOccurrences(entries) {
    var found = {};
    (entries || []).forEach(function (entry) {
        if (!entry || entry.role !== "user") return;
        var key = optimisticKey(entry);
        found[key] = (found[key] || 0) + 1;
    });
    return found;
}

export function matchesOptimistic(pending, actual, scopeKey, now) {
    if (!actual || actual.role !== "user") return false;
    if (!pending || !pending.scopeKey || !scopeKey || pending.scopeKey !== scopeKey) return false;
    if (optimisticExpired(pending, now)) return false;
    var at = Number(actual.at || 0);
    if (!at || at < pending.at - 10 || at > pending.at + 10 * 60) return false;
    var wanted = canonicalOptimisticEntry(pending);
    var received = canonicalOptimisticEntry(actual);
    return received.text === wanted.text && received.imageCount === wanted.imageCount;
}

/** Capture both facts whose meaning is "before the POST" in one behavior-testable value. */
export function optimisticSendSnapshot(entries, startedAt) {
    return { known: knownOccurrences(entries), startedAt: Math.floor(Number(startedAt)) };
}

/** Keep reconciliation ahead of the same-signature repaint shortcut. */
export function reconcileOptimisticBeforeSignature(reconcile, id, received) {
    return reconcile(id, received);
}

/** Prefer the Mac's pre-handoff clock. Older servers fall back through their completion stamp. */
export function authoritativeSendTime(answer, fallback) {
    var accepted = Number(answer && answer.accepted_at);
    if (Number.isFinite(accepted) && accepted > 0) return Math.floor(accepted);
    var server = Number(answer && answer.at);
    return Number.isFinite(server) && server > 0 ? Math.floor(server) : Math.floor(fallback);
}
