/* --------------------------------------------------------------------------
   One shape for every Cloud failure, and the trail that says where a command got to.

   A Cloud command crosses seven layers (`docs/cloud-error-transparency.md` §1) and each of them
   used to refuse in its own spelling: the Mac's reply `error` object, the relay's `ack`,
   `publish_error` and `error` frames, a WebSocket close code, and this page's own `Error`s. The
   pages that show a failure could only see `code` and an English `message`, and for half the
   paths not even the code. So everything is normalised here into `CloudFailure` (§2.2):

       { layer, code, ref: { sender, seq, request }, status, retryable, detail }

   `ref` is the one name all three ends can see — the envelope's own `(sender, seq)` — and it is
   what the Cloud status sheet and the Mac's `cloud-status.json` are both indexed by.

   Nothing in this file touches the DOM, so the node suites can hold it directly.
   -------------------------------------------------------------------------- */

/** §1's seven layers plus `mac`, which exists only for a Mac too old to say which of its five. */
export const FAILURE_LAYERS = Object.freeze([
    "browser", "relay", "mac_transport", "mac_preflight", "mac_ledger", "mac_route", "mac_reply", "mac"
]);
const MAC_LAYERS = new Set(["mac_transport", "mac_preflight", "mac_ledger", "mac_route", "mac_reply"]);

/**
 * §11.6: which `detail` fields each code may carry. Anything else a server sends is dropped
 * before it reaches a page — no paths, titles or transcript text ride along on a refusal.
 * Keep this table and the Mac's in step; a new field is added to both or to neither.
 */
export const DETAIL_FIELDS = Object.freeze({
    command_clock_uncertain: ["reason", "clears_in_ms"],
    key_id_mismatch: ["key_id", "expected_key_id"],
    replay: ["highest_seq"],
    cloud_ingress_busy: ["retry_after", "lane", "limit"],
    cloud_read_busy: ["retry_after", "lane", "limit"],
    transcript_busy: ["retry_after"],
    no_whisper: ["reason"],
    terminal_closed: ["app"],
    would_lose_work: ["lost"]
});
/** Any code that arrived in a relay `publish_error` may carry the field it objected to. */
const PUBLISH_ERROR_FIELDS = ["field"];

/**
 * Whether trying again can help, decided here from the code and never taken from the server.
 * The relay's `publishErrorDisposition` rule: **an unknown code is terminal**, so a future
 * server cannot talk an old page into a retry loop. A code whose effect may already have
 * happened (`command_answer_undeliverable`) is not retryable either — that would run it twice.
 */
const RETRYABLE_CODES = new Set([
    "offline", "socket_error", "cloud_reconnecting", "cloud_starting", "token_superseded",
    "machine_offline", "cloud_read_timeout", "command_clock_uncertain", "cloud_ingress_busy",
    "cloud_read_busy", "transcript_busy", "reading_busy", "busy", "rate_limited",
    "over_capacity", "going_away", "handshake_timeout", "internal"
]);

/** The relay's `WS_CLOSE` table read backwards (`relay/src/errors.ts`); 4429 means either. */
const CLOSE_CODES = Object.freeze({
    4400: "bad_request", 4401: "unauthorized", 4403: "forbidden", 4408: "handshake_timeout",
    4413: "too_large", 4429: "rate_limited", 1001: "going_away", 1011: "internal"
});

/**
 * The ceiling a diagnostics report must fit under before it is sent over Cloud (§11.5).
 *
 * Two limits and the smaller wins. The relay caps `ctl` ciphertext at `max_envelope_bytes`,
 * 16 MiB on every tier since `clawdline-cloud/docs/DECISIONS.md` D17 (`account-do.ts` checks it
 * per class), and the Mac's inbound queue caps plaintext at the same 16 MiB
 * (`CloudInboundCommandQueueLimits.defaultMaximumPlaintextBytes`). `DiagnosticReport.maxBytes`
 * is `2 << 20` and is the one that binds; the other is kept so a later change to either end
 * is one number here rather than a silent 60-second timeout.
 */
export const RELAY_CTL_MAX_BYTES = 16 * 1024 * 1024;
export const DIAGNOSTIC_REPORT_MAX_BYTES = 2 * 1024 * 1024;

const SNAKE = /^[a-z][a-z0-9_]{0,63}$/;

export function isFailureCode(value) { return typeof value === "string" && SNAKE.test(value); }
export function isRetryableCode(code) { return RETRYABLE_CODES.has(code); }
export function closeCodeName(code) { return CLOSE_CODES[code] || null; }

function pick(source, fields, into) {
    if (!source || typeof source !== "object" || Array.isArray(source)) return into;
    fields.forEach(function (field) {
        if (!Object.prototype.hasOwnProperty.call(source, field)) return;
        var value = source[field];
        if (value === null || value === undefined) return;
        if (typeof value === "string" || typeof value === "number" || typeof value === "boolean" ||
            (Array.isArray(value) && field === "lost")) into[field] = value;
    });
    return into;
}

function cleanRef(ref) {
    if (!ref || typeof ref !== "object") return null;
    var sender = typeof ref.sender === "string" && ref.sender ? ref.sender : null;
    var seq = Number.isSafeInteger(ref.seq) && ref.seq >= 0 ? ref.seq : null;
    if (sender === null && seq === null) return null;
    return { sender: sender, seq: seq,
        request: typeof ref.request === "string" && ref.request ? ref.request : null };
}

/**
 * The one constructor. An `Error`, so every existing `.catch(function (e) { e.code … })` keeps
 * working; the six fields are plain properties on it. `message` stays English and internal —
 * for a console line or a test failure — and no page is allowed to show it
 * (`core/failure-text.js` is what a page shows instead).
 *
 * The whitelisted `detail` fields are also copied onto the error itself under the names the
 * local HTTP client already uses (`reason`, `app`, `lost`, `retry_after`), so a page branching on
 * `e.reason` reads the same thing on either transport.
 */
export function cloudFailure(code, message, fields) {
    fields = fields || {};
    var safeCode = isFailureCode(code) ? code : "unexpected_error";
    var error = new Error(message || safeCode);
    error.code = safeCode;
    error.layer = FAILURE_LAYERS.indexOf(fields.layer) >= 0 ? fields.layer : "browser";
    error.ref = cleanRef(fields.ref);
    error.status = Number.isInteger(fields.status) ? fields.status : null;
    error.retryable = isRetryableCode(safeCode);
    error.detail = fields.detail && typeof fields.detail === "object" ? fields.detail : {};
    ["reason", "app", "lost", "lane", "limit", "field"].forEach(function (field) {
        if (error.detail[field] !== undefined) error[field] = error.detail[field];
    });
    if (error.detail.retry_after !== undefined) {
        error.retry_after = error.detail.retry_after;
        error.retryAfter = error.detail.retry_after;
    }
    return error;
}

/**
 * A Mac's reply `error` object (§11.1). A Mac that names no layer is an older build and is
 * recorded as `mac`; the words on screen do not depend on it either way.
 */
export function failureFromMac(error, status, ref) {
    error = error && typeof error === "object" && !Array.isArray(error) ? error : {};
    var code = isFailureCode(error.code) ? error.code : "read_failed";
    var layer = MAC_LAYERS.has(error.layer) ? error.layer : "mac";
    var fields = DETAIL_FIELDS[code] || [];
    var detail = pick(error, fields, {});
    pick(error.detail, fields, detail);
    var seq = Number.isSafeInteger(error.seq) ? error.seq : (ref && ref.seq);
    return cloudFailure(code, typeof error.message === "string" ? error.message : code, {
        layer: layer, detail: detail,
        status: Number.isInteger(status) && status >= 400 && status <= 599 ? status : null,
        ref: Object.assign({}, ref || {}, { seq: seq })
    });
}

/** A refusal the relay said in a frame, or implied with a close code (§11.7). */
export function failureFromRelay(code, fields) {
    fields = fields || {};
    var detail = {};
    if (fields.fromPublishError) pick(fields, PUBLISH_ERROR_FIELDS, detail);
    return cloudFailure(isFailureCode(code) ? code : "relay_error",
        fields.message || "the relay refused this request", {
            layer: "relay", ref: fields.ref, detail: detail,
            status: Number.isInteger(fields.status) ? fields.status : null
        });
}

/**
 * Any rejection as a `CloudFailure`. A `TypeError` from a bad argument, or a refusal built before
 * this file existed, keeps its code if it has a usable one and gets `browser` as its layer; it
 * is never left as a bare `Error` that a page can only describe by its English message.
 */
export function asCloudFailure(error, ref) {
    if (error && typeof error === "object" && isFailureCode(error.code) &&
        FAILURE_LAYERS.indexOf(error.layer) >= 0) {
        if (!error.ref && ref) error.ref = cleanRef(ref);
        return error;
    }
    var code = error && isFailureCode(error.code) ? error.code : "unexpected_error";
    var wrapped = cloudFailure(code, error && error.message ? String(error.message) : code,
        { ref: ref });
    if (error && Number.isInteger(error.status)) wrapped.status = error.status;
    return wrapped;
}

/**
 * `ref` as the design spells it on screen: the sender without `web_`, its first eight
 * characters, a middle dot, the sequence — `f052dcb8·1234`. Empty when there is no sequence,
 * because a failure that never reached the wire has no name the Mac could look up.
 */
export function refText(ref) {
    if (!ref || !Number.isSafeInteger(ref.seq)) return "";
    var sender = String(ref.sender || "").replace(/^web_/, "").slice(0, 8);
    return (sender ? sender + "·" : "") + ref.seq;
}

/* ---- the trail --------------------------------------------------------------------------- */

const TRAIL_COMMANDS = 50;
const STEP_NAMES = ["sealed", "relayed", "machine_offline", "observed", "acknowledged"];

/**
 * What this browser saw of its own recent commands, and of its connection (§2.4, §4.3).
 *
 * Bounded, in memory, and **without contents**: a row is a sender, a sequence, the request id,
 * the command's type and Mac, the time of each step this end can witness, and the refusal's
 * layer and code. Nothing a person typed is in it, so it can ride inside a diagnostics report.
 *
 * One trail outlives the sockets that write into it — a token renewal hands it to the new client
 * — because the question it answers ("what happened to the thing I pressed a minute ago?") does
 * not end when the credential does.
 */
export class CloudTrail {
    constructor(options) {
        options = options || {};
        this.now = options.now || function () { return Date.now(); };
        this.limit = options.limit || TRAIL_COMMANDS;
        this.commands = [];
        this.connection = { state: "connecting", since_ms: this.now(),
            last_relay_error: null, last_close: null };
        this.keyDrift = new Map();
        this.macStatus = new Map();
        this.replay = null;
        this.otherTabs = new Map();
        this.listeners = new Set();
    }

    onChange(listener) {
        this.listeners.add(listener);
        var self = this;
        return function () { self.listeners.delete(listener); };
    }

    _changed() {
        this.listeners.forEach(function (listener) {
            try { listener(); } catch (e) { /* a sheet cannot stop the transport */ }
        });
    }

    find(sender, seq) {
        return this.commands.find(function (row) { return row.sender === sender && row.seq === seq; })
            || null;
    }

    sealed(row) {
        var entry = { sender: row.sender, seq: row.seq, request: row.request || null,
            type: row.type || null, machine: row.machine || null,
            steps: { sealed: this.now() }, refusal: null };
        this.commands.unshift(entry);
        if (this.commands.length > this.limit) this.commands.length = this.limit;
        this._changed();
        return entry;
    }

    step(ref, name) {
        var row = ref && this.find(ref.sender, ref.seq);
        if (!row || STEP_NAMES.indexOf(name) < 0 || row.steps[name]) return;
        row.steps[name] = this.now();
        this._changed();
    }

    refused(ref, failure) {
        var row = ref && this.find(ref.sender, ref.seq);
        if (!row || !failure) return;
        row.refusal = { layer: failure.layer || "browser", code: failure.code };
        this._changed();
    }

    connectionState(state) {
        if (this.connection.state === state) return;
        this.connection.state = state;
        this.connection.since_ms = this.now();
        if (state === "live") this.connection.last_relay_error = null;
        this._changed();
    }

    relayError(code) {
        this.connection.last_relay_error = { code: isFailureCode(code) ? code : "relay_error",
            at_ms: this.now() };
        this._changed();
    }

    closed(code) {
        this.connection.last_close = { code: Number.isInteger(code) ? code : null,
            name: closeCodeName(code), at_ms: this.now() };
        this._changed();
    }

    sawKeyID(machine, sent, received) {
        var had = this.keyDrift.get(machine);
        if (sent === received) {
            if (had) { this.keyDrift.delete(machine); this._changed(); }
            return;
        }
        if (had && had.sent === sent && had.received === received) return;
        this.keyDrift.set(machine, { machine: machine, sent: sent, received: received, at_ms: this.now() });
        this._changed();
    }

    macDigest(machine, digest) {
        this.macStatus.set(machine, digest);
        this._changed();
    }

    sawReplay(machine, seq, highest) {
        this.replay = { machine: machine, seq: seq, highest_seq: highest, at_ms: this.now() };
        this._changed();
    }

    sawOtherTab(tab) {
        var fresh = !this.otherTabs.has(tab);
        this.otherTabs.set(tab, this.now());
        if (fresh) this._changed();
    }

    /**
     * Other tabs of this device heard from recently. Six minutes, because a tab says hello when
     * its socket becomes ready and the viewer token renews about every five: a tab that is still
     * open has said so again inside that window.
     */
    liveOtherTabs() {
        var cutoff = this.now() - 360000;
        return Array.from(this.otherTabs.values()).filter(function (at) { return at >= cutoff; }).length;
    }

    /** A JSON copy: what the status sheet draws and what a diagnostics report carries. */
    snapshot() {
        var copy = function (value) { return JSON.parse(JSON.stringify(value)); };
        return {
            commands: copy(this.commands),
            connection: copy(this.connection),
            key_drift: Array.from(this.keyDrift.values()).map(copy),
            replay: this.replay ? copy(this.replay) : null,
            other_tabs: this.liveOtherTabs(),
            mac_status: Array.from(this.macStatus, function (pair) {
                return { machine: pair[0], cloud_status: copy(pair[1]) };
            })
        };
    }
}
