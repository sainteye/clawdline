/* --------------------------------------------------------------------------
   What this browser saw go wrong, kept until the paired Mac says it has it.

   The hosted console sometimes raises "This browser cannot decrypt Sessions" and sometimes lowers
   it again a moment later, and until this file nothing anywhere recorded why: the transport
   turned every receive failure into one code and threw the original error away. The standing
   rule for a defect that only exists on the phone is that the app delivers its own diagnostics
   to a fixed file on the Mac, with nobody pressing anything (`docs/diagnostics.md`).

   So this is three things, none of them about decryption:

   - **a row**, `{ n, at_ms, event, data }`. `event` is a dotted name the caller chooses
     (`cloud.receive.failed`, `cloud.door.raised`) and `data` is one flat object of scalars.
     Nothing here or on the Mac knows any event name, so the next unrelated defect adds a name
     and changes neither this file nor the route. Callers build `data` from an explicit list of
     metadata; `cleanData` additionally drops the field names that would carry a secret.
   - **a buffer** in `localStorage`, bounded in rows and bytes, that survives a reload and a PWA
     being killed. Not IndexedDB: a transient IndexedDB failure on resume is one of the things
     these rows exist to catch, and a recorder that fails with its subject records nothing.
     Storage failures are counted and never thrown at the transport.
   - **a batch**, the rows and the counts of what was not kept, sealed once into an outbox and
     resent byte-for-byte under the same request id until the Mac's receipt names it. Only that
     receipt removes it. The Mac's durable command ledger answers a resend of identical bytes
     with its recorded outcome instead of running it twice.

   Completeness is arithmetic rather than a promise: every kept or overflowed row consumes one
   `n`, so `rows + dropped_overflow = n_to - n_from + 1`; rows refused by the rate limit consume
   none and are counted in `dropped_rate_limited`, per key in `rate_limited`.

   Nothing in this file touches the DOM beyond reading visibility, so the node suites hold it.
   -------------------------------------------------------------------------- */

import { parseEnvelopeChannel } from "./cloud-crypto.js";
import { isFailureCode } from "./cloud-failure.js";

/** The Cloud command that carries a batch, and the only version of its body. */
export const VIEWER_EVENTS_COMMAND = "diagnostics.events";
export const VIEWER_EVENTS_VERSION = 1;

export const VIEWER_EVENT_LIMITS = Object.freeze({
    /** Rows waiting for a batch. Beyond it the middle row goes: the onset and the latest stay. */
    rows: 100,
    /** One row, serialized. `data` is shrunk to fit rather than the row being refused. */
    rowBytes: 1536,
    /** At most this many rows per key, and per event overall, in one window. */
    windowMs: 60000,
    perKeyInWindow: 3,
    allInWindow: 30,
    /** Distinct keys the per-key drop table names; the rest are summed under `(other)`. */
    rateTableKeys: 16,
    /** How long after a row the batch waits for the rest of a burst. */
    debounceMs: 5000,
    /** Least time between two sends; doubled per failed attempt of one batch, to `maxBackoffMs`. */
    spacingMs: 60000,
    maxBackoffMs: 30 * 60000,
    /**
     * Sends per rolling day. Each one is a row in the Mac's command ledger for a day
     * (`CloudCommandLedger.normalActorLimit` is 1,000 per viewer), so the automatic path keeps
     * to a twentieth of this device's share however often the door flaps.
     */
    dailySends: 48,
    /** Another tab of this device holds delivery for this long after it last claimed it. */
    leaseMs: 90000
});

const STORAGE_PREFIX = "clawdline.cloud.viewer-events.v1";
const EVENT_NAME = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+){1,4}$/;
const FIELD_NAME = /^[a-z][a-z0-9_]{0,63}$/;
const MAX_FIELDS = 48;
const MAX_STRING = 512;
const MAX_LIST = 16;
const MAX_LIST_STRING = 128;
/**
 * Names that are never kept, whatever a caller passes. Callers already build `data` from named
 * metadata; this is the second fence, so a later `Object.assign(data, envelope)` cannot ship the
 * ciphertext, nonce or signature, and nothing named like a credential or a title crosses.
 */
const FORBIDDEN_FIELDS = new Set([
    "nonce", "ct", "sig", "plaintext", "clear", "token", "device_token", "cookie", "cookies",
    "secret", "master_secret", "key_bytes", "private_key", "title", "text", "transcript"
]);
const DAY_MS = 24 * 60 * 60 * 1000;

function freshState(now) {
    return {
        v: 1, next_n: 1, cursor: 1, counting_since_ms: now, rows: [],
        counts: { rate_limited: 0, overflow: 0, storage_errors: 0 }, rate: {},
        outbox: null, sends: [], blocked: null, target: null, acknowledged: null
    };
}

function usableState(value) {
    return !!value && typeof value === "object" && value.v === 1 &&
        Number.isSafeInteger(value.next_n) && Number.isSafeInteger(value.cursor) &&
        Array.isArray(value.rows) && value.counts && typeof value.counts === "object" &&
        value.rate && typeof value.rate === "object" && Array.isArray(value.sends);
}

function randomID() {
    var cryptoValue = globalThis.crypto;
    if (cryptoValue && typeof cryptoValue.randomUUID === "function") return cryptoValue.randomUUID();
    var bytes = new Uint8Array(16);
    if (cryptoValue && typeof cryptoValue.getRandomValues === "function") cryptoValue.getRandomValues(bytes);
    else for (var i = 0; i < 16; i += 1) bytes[i] = Math.floor(Math.random() * 256);
    return Array.from(bytes, function (byte) { return byte.toString(16).padStart(2, "0"); }).join("");
}

/**
 * An error's words with anything that looks like data taken out: a long quoted span (a JSON
 * parser quotes its input) and a long base64-shaped run. What stays is the sentence an engine or
 * this code wrote, which is the part that says what happened.
 */
export function scrubMessage(text, limit) {
    if (typeof text !== "string") return null;
    return text
        .replace(/"[^"]{24,}"|'[^']{24,}'/g, "\"…\"")
        .replace(/[A-Za-z0-9+/_=-]{24,}/g, "[…]")
        .slice(0, limit || 200);
}

/** `{name, message, code}` of anything thrown, bounded and scrubbed. */
export function errorFields(error) {
    var name = error && typeof error === "object" && typeof error.name === "string"
        ? error.name.slice(0, 64) : (error === undefined ? "undefined" : typeof error);
    return {
        name: name,
        message: error && typeof error === "object" ? scrubMessage(error.message, 200) : null,
        code: error && typeof error === "object" && isFailureCode(error.code) ? error.code : null
    };
}

function bounded(value, limit) {
    return typeof value === "string" ? value.slice(0, limit) : null;
}

function base64Length(text) {
    if (typeof text !== "string" || text.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(text)) {
        return null;
    }
    var padding = text.endsWith("==") ? 2 : text.endsWith("=") ? 1 : 0;
    return text.length / 4 * 3 - padding;
}

/**
 * What may be said about an envelope: where it was going, who signed it, under which key id, its
 * sequence, time and class, and how long its ciphertext is. Never the nonce, the ciphertext or
 * the signature — they are not read here at all, apart from `ct`'s length.
 */
export function envelopeMetadata(envelope, realign) {
    var object = envelope && typeof envelope === "object" && !Array.isArray(envelope) ? envelope : null;
    var meta = {
        envelope_shape: object ? "object" : envelope === null ? "null"
            : Array.isArray(envelope) ? "array" : typeof envelope,
        field_names: object ? Object.keys(object).slice(0, MAX_LIST)
            .map(function (key) { return String(key).slice(0, 32); }).sort() : null,
        channel_kind: null, channel_prefix: null, machine: null, session: null,
        sender: null, key_id: null, seq: null, ts: null, class: null,
        realign: realign === true, ct_bytes: null
    };
    if (!object) return meta;
    if (typeof object.ch === "string") {
        meta.channel_prefix = object.ch.split("/")[0].slice(0, 16);
        try {
            var channel = parseEnvelopeChannel(object.ch);
            meta.channel_kind = channel.kind;
            meta.machine = bounded(channel.machine, 128);
            meta.session = bounded(channel.session, 128);
        } catch (e) {
            meta.channel_kind = "unparsed";
        }
    }
    meta.sender = bounded(object.sender, 128);
    meta.key_id = bounded(object.key_id, 64);
    meta.seq = Number.isSafeInteger(object.seq) ? object.seq : null;
    meta.ts = Number.isSafeInteger(object.ts) ? object.ts : null;
    meta.class = bounded(object.class, 16);
    meta.ct_bytes = base64Length(object.ct);
    return meta;
}

/** One flat object of scalars and short lists, without the names that carry secrets. */
export function cleanData(data) {
    var out = {};
    if (!data || typeof data !== "object" || Array.isArray(data)) return out;
    var kept = 0;
    Object.keys(data).forEach(function (field) {
        if (kept >= MAX_FIELDS || !FIELD_NAME.test(field) || FORBIDDEN_FIELDS.has(field)) return;
        var value = data[field];
        var clean;
        if (value === null || value === undefined) clean = null;
        else if (typeof value === "boolean") clean = value;
        else if (typeof value === "number") clean = Number.isFinite(value) ? value : null;
        else if (typeof value === "string") clean = value.slice(0, MAX_STRING);
        else if (Array.isArray(value)) {
            clean = value.slice(0, MAX_LIST).filter(function (item) {
                return typeof item === "string" || (typeof item === "number" && Number.isFinite(item));
            }).map(function (item) {
                return typeof item === "string" ? item.slice(0, MAX_LIST_STRING) : item;
            });
        } else return;
        out[field] = clean;
        kept += 1;
    });
    return out;
}

function shrunk(data) {
    var out = {};
    Object.keys(data).forEach(function (field) {
        var value = data[field];
        out[field] = typeof value === "string" ? value.slice(0, 48)
            : Array.isArray(value) ? value.slice(0, 4) : value;
    });
    out.row_truncated = true;
    return out;
}

/* ---- the page's own context ------------------------------------------------------------- */

const pageLoadedAt = Date.now();
let visibilityChangedAt = null;
if (globalThis.document && typeof globalThis.document.addEventListener === "function") {
    try {
        globalThis.document.addEventListener("visibilitychange", function () {
            visibilityChangedAt = Date.now();
        });
    } catch (e) { /* a page without events still records rows */ }
}

/**
 * Whether the page was in front, how long since that changed, and whether the browser thinks it
 * is online. An iOS Home Screen app resumed from the background is the moment a key store is most
 * likely to be unavailable, so "a second after becoming visible" is worth one field.
 */
export function pageContext(now) {
    var at = Number.isFinite(now) ? now : Date.now();
    var doc = globalThis.document;
    var nav = globalThis.navigator;
    return {
        visibility: doc && typeof doc.visibilityState === "string" ? doc.visibilityState.slice(0, 16) : null,
        ms_since_visibility_change: visibilityChangedAt === null ? null : Math.max(0, at - visibilityChangedAt),
        ms_since_page_load: Math.max(0, at - pageLoadedAt),
        online: nav && typeof nav.onLine === "boolean" ? nav.onLine : null
    };
}

/* ---- the log ----------------------------------------------------------------------------- */

/**
 * One device's rows. `storage` is a `localStorage`-shaped object or `null` for memory only; every
 * read and write to it is caught and counted, so a full or forbidden store degrades to memory for
 * this page rather than failing the socket that is reporting through it.
 *
 * Two tabs of one device share the stored state. Every mutation reads the store first and writes
 * it back inside one synchronous turn, so their rows interleave instead of overwriting each other.
 */
export class ViewerEventLog {
    constructor(options) {
        options = options || {};
        this.storage = options.storage || null;
        this.key = options.key || STORAGE_PREFIX;
        this.now = typeof options.now === "function" ? options.now : function () { return Date.now(); };
        this.limits = Object.assign({}, VIEWER_EVENT_LIMITS, options.limits || {});
        this.memory = null;
        // A failed write leaves the store behind this page's memory; until one succeeds, memory
        // is the truth and the store is not read back over it.
        this.dirty = false;
        this.storageErrors = 0;
        this.listeners = new Set();
        this.windowStartedAt = null;
        this.windowAll = 0;
        this.windowKeys = new Map();
    }

    onRecord(listener) {
        this.listeners.add(listener);
        var self = this;
        return function () { self.listeners.delete(listener); };
    }

    _load() {
        if (this.storage && !this.dirty) {
            try {
                var raw = this.storage.getItem(this.key);
                var parsed = raw ? JSON.parse(raw) : null;
                this.memory = usableState(parsed) ? parsed : (this.memory || freshState(this.now()));
            } catch (e) {
                this.storageErrors += 1;
                this.dirty = true;
            }
        }
        if (!this.memory) this.memory = freshState(this.now());
        return this.memory;
    }

    _save(state) {
        this.memory = state;
        if (!this.storage) return true;
        var pending = this.storageErrors;
        state.counts.storage_errors += pending;
        try {
            this.storage.setItem(this.key, JSON.stringify(state));
            this.storageErrors = 0;
            this.dirty = false;
            return true;
        } catch (e) {
            state.counts.storage_errors -= pending;
            this.storageErrors = pending + 1;
            this.dirty = true;
            return false;
        }
    }

    _admit(key, now) {
        if (this.windowStartedAt === null || now - this.windowStartedAt >= this.limits.windowMs) {
            this.windowStartedAt = now;
            this.windowAll = 0;
            this.windowKeys = new Map();
        }
        var seen = this.windowKeys.get(key) || { count: 0, lastN: null };
        this.windowKeys.set(key, seen);
        if (seen.count >= this.limits.perKeyInWindow || this.windowAll >= this.limits.allInWindow) {
            return null;
        }
        seen.count += 1;
        this.windowAll += 1;
        return seen;
    }

    /**
     * Keep one row, or count it as refused by the rate limit. Answers `{ n, rateLimited, key }`;
     * `n` is what a later row (the door's) can name as the row that caused it.
     */
    record(event, data, dedupeKey) {
        var now = this.now();
        if (typeof event !== "string" || !EVENT_NAME.test(event)) {
            return { n: null, rateLimited: false, key: null, refused: "bad_event" };
        }
        var key = typeof dedupeKey === "string" && dedupeKey ? dedupeKey.slice(0, 256) : event;
        var state = this._load();
        var admitted = this._admit(key, now);
        if (!admitted) {
            state.counts.rate_limited += 1;
            var tableKey = (state.rate[key] || Object.keys(state.rate).length < this.limits.rateTableKeys)
                ? key : "(other)";
            var entry = state.rate[tableKey] || { event: tableKey === key ? event : null, dropped: 0,
                first_at_ms: now, last_at_ms: now, sample_n: null };
            entry.dropped += 1;
            entry.last_at_ms = now;
            if (entry.sample_n === null && tableKey === key) {
                var kept = this.windowKeys.get(key);
                entry.sample_n = kept ? kept.lastN : null;
            }
            state.rate[tableKey] = entry;
            this._save(state);
            this._notify();
            return { n: null, rateLimited: true, key: key };
        }
        var row = { n: state.next_n, at_ms: now, event: event, data: cleanData(data) };
        if (JSON.stringify(row).length > this.limits.rowBytes) row.data = shrunk(row.data);
        if (JSON.stringify(row).length > this.limits.rowBytes) row.data = { row_truncated: true };
        state.next_n += 1;
        admitted.lastN = row.n;
        state.rows.push(row);
        if (state.rows.length > this.limits.rows) {
            state.rows.splice(Math.floor(this.limits.rows / 2), 1);
            state.counts.overflow += 1;
        }
        this._save(state);
        this._notify();
        return { n: row.n, rateLimited: false, key: key };
    }

    _notify() {
        this.listeners.forEach(function (listener) {
            try { listener(); } catch (e) { /* a scheduler cannot stop a recorder */ }
        });
    }

    /**
     * Whether anything is waiting: rows, a batch not yet acknowledged, or rows that were dropped.
     * Failed storage writes alone are not: on a store that always refuses, each batch's own write
     * would count one more and send again every minute with nothing in it. They ride the next batch.
     */
    pending() {
        var state = this._load();
        return !!state.outbox || state.rows.length > 0 || state.counts.rate_limited > 0 ||
            state.counts.overflow > 0;
    }

    /**
     * The batch to send: the sealed outbox if one is waiting, otherwise a new one made from every
     * row and count now held. A new batch moves those rows into the outbox in the same write, so
     * they are still on the phone and still counted until `acknowledge` names this batch.
     */
    takeBatch(meta) {
        meta = meta || {};
        var state = this._load();
        if (state.outbox) return state.outbox;
        var storageErrors = state.counts.storage_errors + this.storageErrors;
        if (!state.rows.length && !state.counts.rate_limited && !state.counts.overflow) return null;
        var now = this.now();
        var batch = {
            v: VIEWER_EVENTS_VERSION,
            batch_id: randomID(),
            created_at_ms: now,
            device: bounded(meta.device, 128),
            tab: bounded(meta.tab, 64),
            web_build: bounded(meta.webBuild, 64),
            rows: state.rows,
            completeness: {
                n_from: state.cursor,
                n_to: state.next_n - 1,
                rows: state.rows.length,
                dropped_rate_limited: state.counts.rate_limited,
                dropped_overflow: state.counts.overflow,
                storage_errors: storageErrors,
                counting_since_ms: Number.isSafeInteger(state.counting_since_ms) ? state.counting_since_ms : null,
                rate_limited: Object.keys(state.rate).sort().map(function (key) {
                    var entry = state.rate[key];
                    return { key: key, event: entry.event, dropped: entry.dropped,
                        first_at_ms: entry.first_at_ms, last_at_ms: entry.last_at_ms,
                        sample_n: entry.sample_n };
                }),
                limits: { rows: this.limits.rows, per_key_per_window: this.limits.perKeyInWindow,
                    all_per_window: this.limits.allInWindow, window_ms: this.limits.windowMs }
            }
        };
        state.outbox = { batch_id: batch.batch_id, request: randomID(), body: JSON.stringify(batch),
            rows: batch.rows.length, created_at_ms: now, attempts: 0, last_attempt_ms: null,
            last_failure: null };
        state.rows = [];
        state.cursor = state.next_n;
        state.counts = { rate_limited: 0, overflow: 0, storage_errors: 0 };
        state.rate = {};
        state.counting_since_ms = now;
        this.storageErrors = 0;
        this._save(state);
        return state.outbox;
    }

    /** The Mac's receipt named this batch: only now do its rows leave the phone. */
    acknowledge(batchID, receipt) {
        var state = this._load();
        if (!state.outbox || state.outbox.batch_id !== batchID) return false;
        state.acknowledged = { batch_id: batchID, rows: state.outbox.rows, at_ms: this.now(),
            attempts: state.outbox.attempts,
            appended: receipt && Number.isSafeInteger(receipt.rows) ? receipt.rows : null };
        state.outbox = null;
        state.blocked = null;
        this._save(state);
        return true;
    }

    noteAttempt() {
        var state = this._load();
        var now = this.now();
        state.sends = state.sends.filter(function (at) { return now - at < DAY_MS; });
        state.sends.push(now);
        if (state.outbox) {
            state.outbox.attempts += 1;
            state.outbox.last_attempt_ms = now;
        }
        this._save(state);
    }

    noteFailure(failure) {
        var state = this._load();
        if (!state.outbox) return;
        state.outbox.last_failure = { layer: failure && failure.layer || "browser",
            code: failure && isFailureCode(failure.code) ? failure.code : "unexpected_error",
            at_ms: this.now() };
        this._save(state);
    }

    /** When the next send may go, from the spacing, this batch's backoff and the daily budget. */
    nextSendAt() {
        var state = this._load();
        var now = this.now();
        var sends = state.sends.filter(function (at) { return now - at < DAY_MS; });
        var at = now;
        if (sends.length) {
            var failures = state.outbox && state.outbox.last_failure ? Math.max(1, state.outbox.attempts) : 1;
            var spacing = Math.min(this.limits.maxBackoffMs,
                this.limits.spacingMs * Math.pow(2, Math.min(failures - 1, 16)));
            at = Math.max(at, sends[sends.length - 1] + spacing);
        }
        if (sends.length >= this.limits.dailySends) {
            at = Math.max(at, sends[sends.length - this.limits.dailySends] + DAY_MS);
        }
        return at;
    }

    /**
     * A refusal that sending again cannot change — an older Mac that does not know the command,
     * a batch it will not take. Held until the Mac's build or this page's build is different, or
     * for a day, so the rows stay and nothing retries in the meantime; after a day one attempt is
     * allowed again, which is a daily question rather than a loop.
     */
    block(record) {
        var state = this._load();
        state.blocked = { machine: bounded(record.machine, 128), code: record.code,
            layer: record.layer || null, mac_build: record.macBuild || null,
            web_build: record.webBuild || null, at_ms: this.now() };
        this._save(state);
    }

    blockedFor(machine, macBuild, webBuild) {
        var blocked = this._load().blocked;
        if (!blocked || blocked.machine !== machine) return null;
        if (!Number.isFinite(blocked.at_ms) || this.now() - blocked.at_ms >= DAY_MS) return null;
        if ((blocked.mac_build || null) !== (macBuild || null)) return null;
        if ((blocked.web_build || null) !== (webBuild || null)) return null;
        return blocked;
    }

    /** The Mac last chosen, kept so a page that cannot open anything yet still knows where to send. */
    rememberTarget(target) {
        var state = this._load();
        var previous = state.target;
        if (previous && previous.machine === target.machine && previous.sender === target.sender &&
            previous.capable === target.capable) return;
        state.target = { machine: bounded(target.machine, 128), sender: bounded(target.sender, 128),
            capable: target.capable === true, at_ms: this.now() };
        this._save(state);
    }

    target() { return this._load().target; }

    /** One tab of a device delivers at a time; the others leave their rows in the shared store. */
    claimLease(tab, ms) {
        if (!this.storage) return true;
        var name = this.key + ":lease";
        var now = this.now();
        try {
            var held = JSON.parse(this.storage.getItem(name) || "null");
            if (held && held.tab !== tab && Number.isFinite(held.until) && held.until > now) return false;
            this.storage.setItem(name, JSON.stringify({ tab: tab, until: now + (ms || this.limits.leaseMs) }));
        } catch (e) {
            this.storageErrors += 1;
        }
        return true;
    }

    /** A copy of the stored state, for the status sheet and for tests. */
    snapshot() {
        return JSON.parse(JSON.stringify(this._load()));
    }
}

const logs = new Map();

/**
 * The log for one account and viewer device, shared by every client this page makes for them —
 * a token renewal replaces the client, not the rows.
 */
export function viewerEventLogFor(storage, account, device, options) {
    var key = [STORAGE_PREFIX, account || "", device || ""].join(":");
    var existing = logs.get(key);
    if (existing && existing.storage === (storage || null)) return existing;
    var log = new ViewerEventLog(Object.assign({}, options || {}, { storage: storage || null, key: key }));
    logs.set(key, log);
    return log;
}
