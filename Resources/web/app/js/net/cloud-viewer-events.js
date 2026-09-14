/* --------------------------------------------------------------------------
   What this browser saw go wrong, kept until the paired Mac says it has it.

   The hosted console sometimes raises "This browser cannot decrypt Sessions" and sometimes lowers
   it again a moment later, and until this file nothing anywhere recorded why: the transport
   turned every receive failure into one code and threw the original error away. The standing
   rule for a defect that only exists on the phone is that the app delivers its own diagnostics
   to a fixed file on the Mac, with nobody pressing anything (`docs/diagnostics.md`).

   So this is three things, none of them about decryption:

   - **a row**, `{ n, at_ms, event, tab, data }`. `event` is a dotted name the caller chooses
     (`cloud.receive.failed`, `cloud.door.raised`), `tab` is the page that recorded it and `data`
     is one flat object of scalars. Nothing here or on the Mac knows any event name, so the next
     unrelated defect adds a name and changes neither this file nor the route. Callers build
     `data` from an explicit list of metadata; `cleanData` additionally drops the field names that
     would carry a secret, and `errorFields` keeps an exception's words only when this code base
     wrote them.
   - **a buffer** in `localStorage`, bounded in rows and bytes, that survives a reload and a PWA
     being killed. Not IndexedDB: a transient IndexedDB failure on resume is one of the things
     these rows exist to catch, and a recorder that fails with its subject records nothing.
     Storage failures are counted and never thrown at the transport.
   - **a batch**, the rows and the counts of what was not kept, sealed once into an outbox and
     resent byte-for-byte under the same request id until the Mac's receipt names it. A receipt
     removes it; so does a refusal of its content, which no resend can change — that batch is
     dropped, its rows are counted in the next batch and a row names the refusal, so the rows
     behind it are never held by it. The Mac recognises a resent `batch_id` and does not append it
     twice.

   Completeness is arithmetic rather than a promise: every kept, overflowed or unflushed row
   consumes one `n`, so `rows + dropped_overflow + dropped_unflushed = n_to - n_from + 1`; rows
   refused by the rate limit consume none and are counted in `dropped_rate_limited`, per key in
   `rate_limited`; rows of a batch the Mac refused are `dropped_refused`.

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
    /**
     * One row, serialized, in UTF-8 bytes — the unit the Mac bounds a batch in. `data` is shrunk to
     * fit rather than the row being refused. With the pairing fields, 36-character machine and
     * device ids and a 25-character build, receive failures measured 1,510 bytes (an unpaired
     * second machine) and 1,626 (a paired Mac's changed sender beside one) under node, where
     * visibility and online read null — so 1,536 cut real rows.
     */
    rowBytes: 2048,
    /**
     * One sealed batch, in UTF-8 bytes. 100 rows at `rowBytes` and a full rate table fit; this
     * stays under the Mac's 256 KiB (`CloudViewerEventLog.maxBatchBytes`) with room for its
     * re-encoding, and a batch that would not is cut here, never refused there.
     */
    batchBytes: 240 * 1024,
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
    /**
     * How long rows wait in memory before the stored state is written. A receive failure writes
     * only a few bytes at once (`:journal`); the whole state is written at most this often, when
     * the page is hidden, and at once for anything delivery does.
     */
    flushMs: 1000,
    /** Without Web Locks, another tab of this device holds delivery this long after claiming it. */
    leaseMs: 90000
});

const STORAGE_PREFIX = "clawdline.cloud.viewer-events.v1";
const EVENT_NAME = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+){1,4}$/;
const MAX_EVENT = 96;
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
const textEncoder = new TextEncoder();

/** This page's id: every row it records names it, and so does every batch it seals. */
export const PAGE_TAB_ID = randomID();

function freshState(now) {
    return {
        v: 1, next_n: 1, cursor: 1, counting_since_ms: now, rows: [], rate_limited_total: 0,
        counts: { rate_limited: 0, overflow: 0, storage_errors: 0, refused: 0, unflushed: 0 },
        rate: {}, outbox: null, sends: [], blocked: null, target: null, acknowledged: null,
        refusals: 0
    };
}

function usableState(value) {
    if (!value || typeof value !== "object" || value.v !== 1 ||
        !Number.isSafeInteger(value.next_n) || !Number.isSafeInteger(value.cursor) ||
        !Array.isArray(value.rows) || !value.counts || typeof value.counts !== "object" ||
        !value.rate || typeof value.rate !== "object" || !Array.isArray(value.sends)) return false;
    ["rate_limited", "overflow", "storage_errors", "refused", "unflushed"].forEach(function (count) {
        if (!Number.isSafeInteger(value.counts[count])) value.counts[count] = 0;
    });
    if (!Number.isSafeInteger(value.rate_limited_total)) value.rate_limited_total = 0;
    if (!Number.isSafeInteger(value.refusals)) value.refusals = 0;
    return true;
}

function randomID() {
    var cryptoValue = globalThis.crypto;
    if (cryptoValue && typeof cryptoValue.randomUUID === "function") return cryptoValue.randomUUID();
    var bytes = new Uint8Array(16);
    if (cryptoValue && typeof cryptoValue.getRandomValues === "function") cryptoValue.getRandomValues(bytes);
    else for (var i = 0; i < 16; i += 1) bytes[i] = Math.floor(Math.random() * 256);
    return Array.from(bytes, function (byte) { return byte.toString(16).padStart(2, "0"); }).join("");
}

/* ---- text the Mac will parse and count ---------------------------------------------------- */

/** UTF-8 bytes, which is what the Mac counts; `String.length` is UTF-16 code units. */
export function utf8Bytes(text) {
    return textEncoder.encode(String(text)).length;
}

/**
 * At most `limit` code units, never ending inside a surrogate pair, with any lone surrogate
 * replaced by U+FFFD. `JSON.stringify` writes a lone surrogate as an escape the Mac's
 * `JSONSerialization` refuses to parse, which would make a whole batch malformed.
 */
export function boundedText(value, limit) {
    if (typeof value !== "string") return null;
    var text = value.length > limit ? value.slice(0, limit) : value;
    var last = text.charCodeAt(text.length - 1);
    if (text.length && last >= 0xd800 && last <= 0xdbff) text = text.slice(0, -1);
    var out = "";
    var start = 0;
    for (var i = 0; i < text.length; i += 1) {
        var unit = text.charCodeAt(i);
        if (unit < 0xd800 || unit > 0xdfff) continue;
        var next = i + 1 < text.length ? text.charCodeAt(i + 1) : 0;
        if (unit <= 0xdbff && next >= 0xdc00 && next <= 0xdfff) { i += 1; continue; }
        out += text.slice(start, i) + "\uFFFD";
        start = i + 1;
    }
    return start === 0 ? text : out + text.slice(start);
}

/* ---- exceptions ---------------------------------------------------------------------------- */

/**
 * The only exception words a row may carry: sentences this code base writes with fixed wording on
 * the receive, frame and door paths. An engine fills its own messages from the values involved —
 * V8's "Cannot create property 'x' on string 'Fix login bug'", WebKit's "JSON Parse error:
 * Unexpected identifier "Fix"" — and a Mac or relay refusal carries a server's words, so matching
 * is exact and anything else keeps only its name and a class. A sentence added to the code and
 * not here is withheld, which loses a clue and never leaks one.
 */
export const OWN_ERROR_MESSAGES = Object.freeze([
    // cloud-crypto.js
    "WebCrypto is unavailable", "IndexedDB is unavailable", "could not open key store",
    "nonce is not canonical base64", "ct is not canonical base64", "sig is not canonical base64",
    "value is not canonical base64", "expected bytes or canonical base64",
    "ch is empty or too long", "the wh/ channel is reserved", "ch has an unusable segment",
    "unknown or malformed channel", "envelope must be an object",
    "envelope fields do not match protocol v1", "unsupported envelope version", "bad seq", "bad ts",
    "class does not match channel", "bad key_id", "bad sender", "bad nonce length",
    "empty ciphertext", "bad signature length", "the account master secret must be 32 bytes",
    "an Ed25519 public key must be 32 bytes", "bad envelope signature",
    "only non-extractable CryptoKeys are stored", "bad pairing key id", "bad pairing machine binding",
    "pairing binding storage name is missing", "bad stored pairing key id",
    // cloud-client.js: frames
    "the relay sent a binary frame", "the relay sent non-JSON", "the relay frame is not an object",
    "the relay refused a frame", "unknown relay frame type", "the relay challenge is malformed",
    "the relay named another account", "the relay named another device",
    "the relay ready frame does not match the challenge", "the cloud connection failed",
    "the cloud connection dropped",
    // cloud-client.js: envelopes
    "the account master key is extractable",
    "This browser's pairing for the selected machine is incomplete. Start the Pair a Browser flow on that machine, then try again.",
    "the paired machine keys are extractable",
    "the envelope does not match the selected machine's pairing", "the envelope sender is not paired",
    "this browser holds no pairing for the envelope's machine",
    "this browser cannot decrypt the paired Mac's Session data",
    "the authenticated machine channel does not match its paired sender",
    "the legacy machine pairing could not be persisted",
    "the retained channel sequence did not advance", "the envelope sequence did not advance",
    "the decrypted stream payload is not JSON", "the session inventory is malformed",
    "a session snapshot must be an object", "the read answer names no read",
    // cloud-boot.js
    "this machine has no complete pairing"
]);
const OWN_MESSAGES = new Set(OWN_ERROR_MESSAGES);
const ERROR_NAME = /^[A-Z][A-Za-z0-9]{0,62}(Error|Exception)$/;
const JAVASCRIPT_ERRORS = new Set(["Error", "TypeError", "RangeError", "SyntaxError", "ReferenceError",
    "EvalError", "URIError", "AggregateError"]);

/**
 * `{name, message, code, class}` of anything thrown. `message` is non-null only for a sentence in
 * `OWN_ERROR_MESSAGES`; `name` only when it is shaped like an error type's name. `class` says
 * what was withheld and why, in a fixed vocabulary: `own` (words kept), `typed` (a failure code
 * whose words are not this code's), `javascript` (a built-in error type), `platform` (a
 * DOMException or other named web-platform error), `other`, or `not_an_error`.
 */
export function errorFields(error) {
    if (!error || typeof error !== "object") {
        return { name: error === undefined ? "undefined" : typeof error, message: null, code: null,
            class: "not_an_error" };
    }
    var raw = typeof error.name === "string" ? error.name : "";
    var named = raw === "Error" || ERROR_NAME.test(raw);
    var message = typeof error.message === "string" && OWN_MESSAGES.has(error.message) ? error.message : null;
    var code = isFailureCode(error.code) ? error.code : null;
    var platform = (typeof DOMException === "function" && error instanceof DOMException) ||
        (named && !JAVASCRIPT_ERRORS.has(raw));
    return {
        name: named ? raw : raw ? "(unlisted)" : "object",
        message: message,
        code: code,
        class: message !== null ? "own" : code !== null ? "typed"
            : JAVASCRIPT_ERRORS.has(raw) ? "javascript" : platform ? "platform" : "other"
    };
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
            .map(function (key) { return boundedText(String(key), 32); }).sort() : null,
        channel_kind: null, channel_prefix: null, machine: null, session: null,
        sender: null, key_id: null, seq: null, ts: null, class: null,
        realign: realign === true, ct_bytes: null
    };
    if (!object) return meta;
    if (typeof object.ch === "string") {
        meta.channel_prefix = boundedText(object.ch.split("/")[0], 16);
        try {
            var channel = parseEnvelopeChannel(object.ch);
            meta.channel_kind = channel.kind;
            meta.machine = boundedText(channel.machine, 128);
            meta.session = boundedText(channel.session, 128);
        } catch (e) {
            meta.channel_kind = "unparsed";
        }
    }
    meta.sender = boundedText(object.sender, 128);
    meta.key_id = boundedText(object.key_id, 64);
    meta.seq = Number.isSafeInteger(object.seq) ? object.seq : null;
    meta.ts = Number.isSafeInteger(object.ts) ? object.ts : null;
    meta.class = boundedText(object.class, 16);
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
        else if (typeof value === "string") clean = boundedText(value, MAX_STRING);
        else if (Array.isArray(value)) {
            clean = value.slice(0, MAX_LIST).filter(function (item) {
                return typeof item === "string" || (typeof item === "number" && Number.isFinite(item));
            }).map(function (item) {
                return typeof item === "string" ? boundedText(item, MAX_LIST_STRING) : item;
            });
        } else return;
        out[field] = clean;
        kept += 1;
    });
    return out;
}

/** A row's data cut to fit: one field fewer than the Mac's 48, so `row_truncated` is the 48th. */
function shrunk(data) {
    var out = {};
    Object.keys(data).slice(0, MAX_FIELDS - 1).forEach(function (field) {
        var value = data[field];
        out[field] = typeof value === "string" ? boundedText(value, 48)
            : Array.isArray(value) ? value.slice(0, 4) : value;
    });
    out.row_truncated = true;
    return out;
}

/* ---- the page's own context ------------------------------------------------------------- */

const pageLoadedAt = Date.now();
let visibilityChangedAt = null;
const hiddenListeners = new Set();
if (globalThis.document && typeof globalThis.document.addEventListener === "function") {
    try {
        globalThis.document.addEventListener("visibilitychange", function () {
            visibilityChangedAt = Date.now();
            if (globalThis.document.visibilityState === "hidden") {
                hiddenListeners.forEach(function (listener) { try { listener(); } catch (e) { } });
            }
        });
        globalThis.addEventListener("pagehide", function () {
            hiddenListeners.forEach(function (listener) { try { listener(); } catch (e) { } });
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
        visibility: doc && typeof doc.visibilityState === "string" ? boundedText(doc.visibilityState, 16) : null,
        ms_since_visibility_change: visibilityChangedAt === null ? null : Math.max(0, at - visibilityChangedAt),
        ms_since_page_load: Math.max(0, at - pageLoadedAt),
        online: nav && typeof nav.onLine === "boolean" ? nav.onLine : null
    };
}

/* ---- refusals ------------------------------------------------------------------------------ */

/** Codes that refuse the batch's bytes themselves: no resend of those bytes can succeed. */
const CONTENT_REFUSALS = new Set(["viewer_events_malformed", "viewer_events_too_large",
    "viewer_events_empty", "malformed_command"]);
const REFUSAL_PATH = /^That batch is not viewer events v1 at ([a-z_]{1,32}(?:\[\d{1,3}\])?(?:\.[a-z][a-z0-9_]{0,63}(?:\[\d{1,3}\])?){0,3})\.$/;

/**
 * What a final refusal means for the waiting batch: `drop` when it refuses the batch's content —
 * one of `CONTENT_REFUSALS`, or anything the Mac's route itself refused after reading the body —
 * and `hold` when it refuses this Mac or this device (an older Mac's `unknown_command`, a device
 * that may not publish), where the same batch is right and only the wait is.
 */
export function refusalDisposition(record) {
    record = record || {};
    return CONTENT_REFUSALS.has(record.code) || record.layer === "mac_route" ? "drop" : "hold";
}

/* ---- the log ----------------------------------------------------------------------------- */

/**
 * One device's rows. `storage` is a `localStorage`-shaped object or `null` for memory only; every
 * read and write to it is caught and counted, so a full or forbidden store degrades to memory for
 * this page rather than failing the socket that is reporting through it.
 *
 * **One writer.** Tabs of one device share the stored state, and `localStorage` has no lock: two
 * tabs that each read, change and write it lose one another's rows. So with `locks` (the page's
 * `navigator.locks`) only the tab holding the exclusive Web Lock named by the key reads or writes
 * the stored state, and only it delivers. Every other tab keeps its rows in memory, and when the
 * lock reaches it — the browser releases it when the holding page goes away — it reads the stored
 * state once and adds its rows to it, renumbered. A tab closed before that loses its memory rows
 * uncounted. Without Web Locks the tab writes at once, as a single tab would, and two tabs can
 * lose each other's rows (`docs/diagnostics.md`).
 *
 * **Cached, written in few bytes.** The writer reads the stored state once and then keeps it: a
 * receive failure changes memory and writes only `:journal`, the next `n` and the running count
 * of rate-limited rows. The whole state is written at most every `flushMs`, when the page is
 * hidden, and at once for delivery. A page killed before a flush loses the rows since the last
 * one; the next page finds the journal ahead of the stored state and counts them as
 * `dropped_unflushed` (and rate-limited ones under `(unflushed)`), so the arithmetic still holds.
 */
export class ViewerEventLog {
    constructor(options) {
        options = options || {};
        this.storage = options.storage || null;
        this.key = options.key || STORAGE_PREFIX;
        this.now = typeof options.now === "function" ? options.now : function () { return Date.now(); };
        this.limits = Object.assign({}, VIEWER_EVENT_LIMITS, options.limits || {});
        this.tab = boundedText(options.tab, 64) || PAGE_TAB_ID;
        this.locks = options.locks && typeof options.locks.request === "function" ? options.locks : null;
        this.timers = options.timers || { setTimeout: function (fn, ms) {
            var timer = globalThis.setTimeout(fn, ms);
            if (timer && typeof timer.unref === "function") timer.unref();
            return timer;
        }, clearTimeout: function (timer) { globalThis.clearTimeout(timer); } };
        // Other devices' logs for the same account, which a writer may remove (`_pruneOthers`).
        this.accountPrefix = typeof options.accountPrefix === "string" ? options.accountPrefix : null;
        this.state = null;
        this.flushTimer = null;
        this.storageErrors = 0;
        this.listeners = new Set();
        this.windowStartedAt = null;
        this.windowAll = 0;
        this.windowKeys = new Map();
        this.release = null;
        // Memory-only logs and pages without Web Locks write at once; a page with them waits.
        this.writer = !this.storage || !this.locks;
        if (this.storage && this.writer) this.state = this._readStored();
        if (!this.state) this.state = freshState(this.now());
        if (this.storage && !this.writer) this._requestLock();
        var self = this;
        this.onHidden = function () { self.flush(); };
        if (this.storage) hiddenListeners.add(this.onHidden);
    }

    onRecord(listener) {
        this.listeners.add(listener);
        var self = this;
        return function () { self.listeners.delete(listener); };
    }

    _requestLock() {
        var self = this;
        try {
            var held = this.locks.request(this.key, function () {
                self._becomeWriter();
                return new Promise(function (resolve) { self.release = resolve; });
            });
            if (held && typeof held.catch === "function") held.catch(function () { self._becomeWriter(); });
        } catch (e) {
            // A lock manager that refuses the request answers nothing about other tabs: write as
            // a single tab would, which is what this page did before Web Locks.
            this._becomeWriter();
        }
    }

    /** This tab now owns the stored state: read it once, add what memory holds, write it. */
    _becomeWriter() {
        if (this.writer) return;
        var memory = this.state;
        var stored = this._readStored() || freshState(this.now());
        this.writer = true;
        this.state = stored;
        this._merge(memory);
        this.flush();
        this._pruneOthers();
        this._notify();
    }

    /** The stored state, with anything the journal proves was never flushed counted. */
    _readStored() {
        var state = null;
        try {
            var raw = this.storage.getItem(this.key);
            var parsed = raw ? JSON.parse(raw) : null;
            if (usableState(parsed)) state = parsed;
        } catch (e) {
            this.storageErrors += 1;
        }
        var journal = null;
        try { journal = JSON.parse(this.storage.getItem(this.key + ":journal") || "null"); }
        catch (e) { journal = null; }
        if (!state || !journal || typeof journal !== "object") return state;
        if (Number.isSafeInteger(journal.n) && journal.n > state.next_n) {
            state.counts.unflushed += journal.n - state.next_n;
            state.next_n = journal.n;
        }
        if (Number.isSafeInteger(journal.rl) && journal.rl > state.rate_limited_total) {
            var lost = journal.rl - state.rate_limited_total;
            var at = this.now();
            state.counts.rate_limited += lost;
            state.rate_limited_total = journal.rl;
            var entry = state.rate["(unflushed)"] || { event: null, dropped: 0, first_at_ms: at,
                last_at_ms: at, sample_n: null };
            entry.dropped += lost;
            state.rate["(unflushed)"] = entry;
        }
        return state;
    }

    /**
     * Rows a tab kept in memory while another held the lock, added to the stored state. They get
     * the stored sequence's next numbers, and a field ending in `_n` that named one of them — a
     * door row's `cause_n` — is renumbered with it.
     */
    _merge(memory) {
        if (!memory || memory === this.state) return;
        var state = this.state;
        var offset = state.next_n - memory.cursor;
        memory.rows.forEach(function (row) {
            var data = {};
            Object.keys(row.data || {}).forEach(function (field) {
                var value = row.data[field];
                data[field] = /_n$/.test(field) && Number.isSafeInteger(value) && value >= memory.cursor &&
                    value < memory.next_n ? value + offset : value;
            });
            state.rows.push({ n: row.n + offset, at_ms: row.at_ms, event: row.event, tab: row.tab, data: data });
        });
        state.next_n += memory.next_n - memory.cursor;
        while (state.rows.length > this.limits.rows) {
            state.rows.splice(Math.floor(this.limits.rows / 2), 1);
            state.counts.overflow += 1;
        }
        ["rate_limited", "overflow", "storage_errors"].forEach(function (count) {
            state.counts[count] += memory.counts[count];
        });
        state.rate_limited_total += memory.rate_limited_total;
        Object.keys(memory.rate).forEach(function (key) {
            var mine = memory.rate[key];
            var entry = state.rate[key];
            if (!entry && Object.keys(state.rate).length >= this.limits.rateTableKeys) key = "(other)";
            entry = state.rate[key] || { event: mine.event, dropped: 0, first_at_ms: mine.first_at_ms,
                last_at_ms: mine.last_at_ms, sample_n: null };
            entry.dropped += mine.dropped;
            entry.last_at_ms = Math.max(entry.last_at_ms, mine.last_at_ms);
            state.rate[key] = entry;
        }, this);
        if (!state.target && memory.target) state.target = memory.target;
    }

    /** Remove other devices' logs for this account that no live tab holds, and say so in a row. */
    _pruneOthers() {
        var storage = this.storage;
        if (!this.locks || !this.accountPrefix || typeof storage.key !== "function") return;
        var bases = new Set();
        try {
            for (var i = 0; i < storage.length; i += 1) {
                var name = storage.key(i);
                if (typeof name !== "string" || name.indexOf(this.accountPrefix) !== 0) continue;
                var device = name.slice(this.accountPrefix.length).split(":")[0];
                var base = this.accountPrefix + device;
                if (base !== this.key) bases.add(base);
            }
        } catch (e) { return; }
        var self = this;
        bases.forEach(function (base) {
            try {
                self.locks.request(base, { ifAvailable: true }, function (lock) {
                    // `null` is the browser saying a live tab holds that log: not ours to remove.
                    if (!lock) return;
                    var bytes = 0;
                    var rows = null;
                    var outboxRows = null;
                    var rateLimited = null;
                    [base, base + ":journal", base + ":lease"].forEach(function (name) {
                        var raw = storage.getItem(name);
                        if (typeof raw !== "string") return;
                        bytes += utf8Bytes(name) + utf8Bytes(raw);
                        if (name !== base) return;
                        try {
                            var old = JSON.parse(raw);
                            rows = Array.isArray(old.rows) ? old.rows.length : null;
                            outboxRows = old.outbox && Number.isSafeInteger(old.outbox.rows) ? old.outbox.rows : 0;
                            rateLimited = old.counts && Number.isSafeInteger(old.counts.rate_limited)
                                ? old.counts.rate_limited : null;
                        } catch (e) { /* counted by its bytes alone */ }
                    });
                    [base, base + ":journal", base + ":lease"].forEach(function (name) { storage.removeItem(name); });
                    self.record("viewer_events.log.pruned", { device: base.slice(self.accountPrefix.length),
                        rows: rows, outbox_rows: outboxRows, rate_limited: rateLimited, bytes: bytes });
                });
            } catch (e) { /* unknown never authorises a removal */ }
        });
    }

    /** Release the lock, as a closing page does; for tests and for a log being replaced. */
    close() {
        this.flush();
        hiddenListeners.delete(this.onHidden);
        if (this.release) { this.release(); this.release = null; }
    }

    _load() {
        return this.state;
    }

    /** A change: memory now, storage soon, or at once for what delivery depends on. */
    _save(state, immediate) {
        this.state = state;
        if (!this.storage || !this.writer) return true;
        if (immediate) return this.flush();
        if (this.flushTimer === null) {
            var self = this;
            try {
                this.flushTimer = this.timers.setTimeout(function () {
                    self.flushTimer = null;
                    self.flush();
                }, this.limits.flushMs);
            } catch (e) { return this.flush(); }
        }
        return true;
    }

    /** A few bytes for every row: the next `n` and the rate-limited total a crash could lose. */
    _journal(state) {
        if (!this.storage || !this.writer) return;
        try {
            this.storage.setItem(this.key + ":journal",
                "{\"n\":" + state.next_n + ",\"rl\":" + state.rate_limited_total + "}");
        } catch (e) {
            this.storageErrors += 1;
        }
    }

    /** Write the whole state now. Answers whether it reached the store. */
    flush() {
        if (this.flushTimer !== null) {
            try { this.timers.clearTimeout(this.flushTimer); } catch (e) { }
            this.flushTimer = null;
        }
        if (!this.storage || !this.writer) return true;
        var state = this.state;
        var pending = this.storageErrors;
        state.counts.storage_errors += pending;
        try {
            this.storage.setItem(this.key, JSON.stringify(state));
            this.storageErrors = 0;
            return true;
        } catch (e) {
            state.counts.storage_errors -= pending;
            this.storageErrors = pending + 1;
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
        if (typeof event !== "string" || event.length > MAX_EVENT || !EVENT_NAME.test(event)) {
            return { n: null, rateLimited: false, key: null, refused: "bad_event" };
        }
        var key = typeof dedupeKey === "string" && dedupeKey ? boundedText(dedupeKey, 256) : event;
        var state = this._load();
        var admitted = this._admit(key, now);
        if (!admitted) {
            state.counts.rate_limited += 1;
            state.rate_limited_total += 1;
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
            this._journal(state);
            this._save(state);
            this._notify();
            return { n: null, rateLimited: true, key: key };
        }
        var row = { n: state.next_n, at_ms: now, event: event, tab: this.tab, data: cleanData(data) };
        if (utf8Bytes(JSON.stringify(row)) > this.limits.rowBytes) row.data = shrunk(row.data);
        if (utf8Bytes(JSON.stringify(row)) > this.limits.rowBytes) row.data = { row_truncated: true };
        state.next_n += 1;
        admitted.lastN = row.n;
        state.rows.push(row);
        if (state.rows.length > this.limits.rows) {
            state.rows.splice(Math.floor(this.limits.rows / 2), 1);
            state.counts.overflow += 1;
        }
        this._journal(state);
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
            state.counts.overflow > 0 || state.counts.refused > 0 || state.counts.unflushed > 0;
    }

    /**
     * The batch to send: the sealed outbox if one is waiting, otherwise a new one made from every
     * row and count now held. A new batch moves those rows into the outbox in the same write, so
     * they are still on the phone and still counted until `acknowledge` names this batch. Only
     * the tab holding the stored state seals.
     */
    takeBatch(meta) {
        meta = meta || {};
        if (!this.writer) return null;
        var state = this._load();
        if (state.outbox) return state.outbox;
        var storageErrors = state.counts.storage_errors + this.storageErrors;
        if (!this.pending()) return null;
        var now = this.now();
        var batch = {
            v: VIEWER_EVENTS_VERSION,
            batch_id: randomID(),
            created_at_ms: now,
            device: boundedText(meta.device, 128),
            tab: boundedText(meta.tab, 64),
            web_build: boundedText(meta.webBuild, 64),
            rows: state.rows,
            completeness: {
                n_from: state.cursor,
                n_to: state.next_n - 1,
                rows: state.rows.length,
                dropped_rate_limited: state.counts.rate_limited,
                dropped_overflow: state.counts.overflow,
                dropped_refused: state.counts.refused,
                dropped_unflushed: state.counts.unflushed,
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
        var body = JSON.stringify(batch);
        // Rows and the table are each bounded so this cannot happen; if it ever does, the largest
        // rows lose their data rather than the Mac refusing the batch (every `n` stays).
        var bySize = batch.rows.map(function (row, index) { return index; }).sort(function (a, b) {
            return utf8Bytes(JSON.stringify(batch.rows[b])) - utf8Bytes(JSON.stringify(batch.rows[a]));
        });
        for (var i = 0; utf8Bytes(body) > this.limits.batchBytes && i < bySize.length; i += 1) {
            var row = batch.rows[bySize[i]];
            batch.rows[bySize[i]] = { n: row.n, at_ms: row.at_ms, event: row.event, tab: row.tab,
                data: { row_truncated: true } };
            body = JSON.stringify(batch);
        }
        state.outbox = { batch_id: batch.batch_id, request: randomID(), body: body,
            rows: batch.rows.length, created_at_ms: now, attempts: 0, last_attempt_ms: null,
            last_failure: null };
        state.rows = [];
        state.cursor = state.next_n;
        state.counts = { rate_limited: 0, overflow: 0, storage_errors: 0, refused: 0, unflushed: 0 };
        state.rate = {};
        state.counting_since_ms = now;
        this.storageErrors = 0;
        this._save(state, true);
        return state.outbox;
    }

    /** The Mac's receipt named this batch: only now do its rows leave the phone. */
    acknowledge(batchID, receipt) {
        var state = this._load();
        if (!state.outbox || state.outbox.batch_id !== batchID) return false;
        state.acknowledged = { batch_id: batchID, rows: state.outbox.rows, at_ms: this.now(),
            attempts: state.outbox.attempts,
            appended: receipt && Number.isSafeInteger(receipt.rows) ? receipt.rows : null,
            duplicate: !!(receipt && receipt.duplicate === true) };
        state.outbox = null;
        state.blocked = null;
        state.refusals = 0;
        this._save(state, true);
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
        this._save(state, true);
    }

    noteFailure(failure) {
        var state = this._load();
        if (!state.outbox) return;
        state.outbox.last_failure = { layer: failure && failure.layer || "browser",
            code: failure && isFailureCode(failure.code) ? failure.code : "unexpected_error",
            at_ms: this.now() };
        this._save(state, true);
    }

    /**
     * When the next send may go, from the spacing, this batch's backoff and the daily budget.
     * Batches dropped one after another back off like attempts of one batch, so a Mac that refuses
     * every batch is asked a daily budget's worth, not every minute.
     */
    nextSendAt() {
        var state = this._load();
        var now = this.now();
        var sends = state.sends.filter(function (at) { return now - at < DAY_MS; });
        var at = now;
        if (sends.length) {
            var failures = state.outbox && state.outbox.last_failure ? Math.max(1, state.outbox.attempts) : 1;
            failures = Math.max(failures, state.refusals + 1);
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
     * A final refusal (`refusalDisposition`). Answers `drop` or `hold`.
     *
     * `hold` — a refusal that sending again cannot change for this Mac or this device, such as an
     * older Mac that does not know the command. Held until the Mac's build or this page's build is
     * different, or for a day, so the rows stay and nothing retries in the meantime; after a day
     * one attempt is allowed again, which is a daily question rather than a loop.
     *
     * `drop` — a refusal of the batch's own bytes. Holding it would resend identical bytes into the
     * same refusal forever with every later row behind it, so the batch goes: its rows are
     * `dropped_refused` in the next batch, and a `viewer_events.batch.refused` row names the code,
     * the batch and its counts, and — when the Mac said where — the path it objected to.
     */
    block(record) {
        record = record || {};
        var state = this._load();
        var disposition = refusalDisposition(record);
        // The batch that was refused is already gone (acknowledged or dropped): nothing to hold.
        if (disposition === "drop" && !state.outbox) return "drop";
        if (disposition === "drop") {
            var outbox = state.outbox;
            var sealed = {};
            try { sealed = JSON.parse(outbox.body).completeness || {}; } catch (e) { sealed = {}; }
            var path = typeof record.message === "string" ? REFUSAL_PATH.exec(record.message) : null;
            state.outbox = null;
            state.blocked = null;
            state.counts.refused += outbox.rows;
            state.refusals += 1;
            this._save(state, true);
            this.record("viewer_events.batch.refused", {
                code: isFailureCode(record.code) ? record.code : null,
                layer: boundedText(record.layer, 32), status: Number.isInteger(record.status) ? record.status : null,
                refusal_path: path ? path[1] : null, machine: boundedText(record.machine, 128),
                mac_build: boundedText(record.macBuild, 64), web_build: boundedText(record.webBuild, 64),
                batch_id: outbox.batch_id, rows: outbox.rows, attempts: outbox.attempts,
                created_at_ms: outbox.created_at_ms, n_from: sealed.n_from, n_to: sealed.n_to,
                dropped_rate_limited: sealed.dropped_rate_limited, dropped_overflow: sealed.dropped_overflow,
                dropped_refused: sealed.dropped_refused, dropped_unflushed: sealed.dropped_unflushed,
                storage_errors: sealed.storage_errors
            }, "viewer_events.batch.refused|" + record.code);
            this.flush();
            return "drop";
        }
        state.blocked = { machine: boundedText(record.machine, 128), code: record.code,
            layer: record.layer || null, mac_build: record.macBuild || null,
            web_build: record.webBuild || null, at_ms: this.now() };
        this._save(state, true);
        return "hold";
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
        state.target = { machine: boundedText(target.machine, 128), sender: boundedText(target.sender, 128),
            capable: target.capable === true, at_ms: this.now() };
        this._save(state);
    }

    target() { return this._load().target; }

    /**
     * One tab of a device delivers at a time; the others leave their rows to it. With Web Locks
     * that tab is the writer. Without them it is whoever last claimed the lease in the store.
     */
    claimLease(tab, ms) {
        if (!this.writer) return false;
        if (!this.storage || this.locks) return true;
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

    /** A copy of this tab's state, for the status sheet and for tests. */
    snapshot() {
        return JSON.parse(JSON.stringify(this._load()));
    }
}

const logs = new Map();

/**
 * The log for one account and viewer device, shared by every client this page makes for them —
 * a token renewal replaces the client, not the rows. `options.locks` is the page's
 * `navigator.locks`, handed in by the composition root.
 */
export function viewerEventLogFor(storage, account, device, options) {
    var accountPrefix = [STORAGE_PREFIX, account || ""].join(":") + ":";
    var key = accountPrefix + (device || "");
    var existing = logs.get(key);
    if (existing && existing.storage === (storage || null)) return existing;
    var log = new ViewerEventLog(Object.assign({}, options || {}, { storage: storage || null, key: key,
        accountPrefix: account ? accountPrefix : null }));
    logs.set(key, log);
    return log;
}
