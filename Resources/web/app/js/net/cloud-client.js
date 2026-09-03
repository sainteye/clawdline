import {
    channelSegment,
    decodedChannelSegment,
    sessionIdentity,
    sessionIdentityKey
} from "./client.js";
import {
    base64Bytes,
    importMasterSecret,
    importSenderPublicKey,
    openEnvelope,
    parseEnvelopeChannel,
    sealEnvelope
} from "./cloud-crypto.js";
import { T } from "../core/i18n.js";

const textDecoder = new TextDecoder();
const textEncoder = new TextEncoder();

/**
 * The one spelling of a pending read's key. Both ends of a read compute it — the caller that
 * registers a waiter and the envelope that settles it — and a key computed twice is a key that
 * can be computed differently twice: the first draft of this used a space on one side and a NUL
 * on the other, and every read hung with its answer already decrypted and in hand.
 *
 * `sessionIdentityKey` already ends in a NUL-joined identity, so a second NUL cannot collide with
 * a machine or session name, both of which may contain anything else.
 */
function readKey(identity, read) {
    return sessionIdentityKey(identity) + "\u0000" + read;
}

/** How long a read may go unanswered before it is an answer of its own. */
const READ_TIMEOUT_MS = 60000;

/** The agent or shell a read is about, as the string the Mac will echo back inside `read`. */
function readSubject(value) {
    return value === undefined || value === null ? "" : String(value);
}

/**
 * The refusal for an agent or shell read with no id, raised here rather than at the Mac.
 *
 * It is the Mac's own word because it is the Mac's own rule — an empty id is `malformed_read`
 * in `serveRead` — but the Mac cannot be the one to say it: a read refused before it is parsed
 * publishes nothing, having neither an answer nor a name to publish it under, so a request this
 * client already knows is malformed would leave, be dropped in silence, and end sixty seconds
 * later as `cloud_read_timeout`. Refusing it here spends no envelope sequence and gives a page
 * branching on that word no reason to care which end noticed.
 */
function missingSubject(what) {
    return cloudError("malformed_read", "this read names no " + what);
}

/** The transcript window the direct path asks for, so both transports show the same tail. */
const TRANSCRIPT_LIMIT = 200;

/** An agent's window: the same number for the same reason — `live.js` asks for `?limit=200`. */
const AGENT_LIMIT = 200;

/**
 * How much of a background command's tail to take.
 *
 * The direct path sends no `bytes` at all and lets the route's own default stand. That is not
 * something this path can copy by omission: the Mac checks a read's key set exactly, so a field
 * left out is a malformed read rather than a default. The default is therefore written down here
 * — 64 KiB, which is what the shell panel already renders on the tunnel, and three orders of
 * magnitude inside a single envelope.
 */
const SHELL_BYTES = 64 * 1024;

function cloudError(code, message) {
    var error = new Error(message || code);
    error.code = code;
    return error;
}

/**
 * The shape a read's answer takes on `t/<machine>/<session>`, or null if this is not one.
 *
 * `read` names which of the two it answers, because both ride one channel: the channel prefix is
 * the only part of an envelope the relay reads, so a second prefix would need a relay change and
 * a payload field needs nothing. `body` on success, `error` on a refusal — and a refusal really
 * does come back, which is the difference between a phone that can say "no session named that"
 * and one that shows a skeleton until somebody closes the tab.
 */
function readAnswer(payload) {
    if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
    if (typeof payload.read !== "string" || !payload.read) return null;
    var error = payload.error;
    if (error && typeof error === "object" && !Array.isArray(error)) {
        return { read: payload.read, body: null,
            error: cloudError(typeof error.code === "string" && error.code
                ? error.code : "read_failed", error.message) };
    }
    return { read: payload.read, body: payload.body === undefined ? null : payload.body,
        error: null };
}

function socketURL(input) {
    var base = typeof location !== "undefined" ? location.href : undefined;
    var url = new URL(input, base);
    if (!/\/v1\/connect\/?$/.test(url.pathname)) {
        url.pathname = url.pathname.replace(/\/$/, "") + "/v1/connect";
    }
    url.searchParams.set("role", "viewer");
    if (url.protocol === "https:") url.protocol = "wss:";
    if (url.protocol === "http:") url.protocol = "ws:";
    if (url.protocol !== "wss:" && url.protocol !== "ws:") {
        throw new TypeError("the relay URL must be http(s) or ws(s)");
    }
    return url.toString();
}

/**
 * The browser viewer transport.  Reads are on by construction; writes require
 * `allowWrites: true` as well as both device and master keys.
 */
export class CloudClient {
    constructor(options) {
        options = options || {};
        if (!options.relayURL) throw new TypeError("CloudClient needs relayURL");
        if (!options.deviceToken) throw new TypeError("CloudClient needs a device token");
        this.url = socketURL(options.relayURL);
        this.deviceToken = options.deviceToken;
        this.devicePrivateKey = options.devicePrivateKey || null;
        if (this.devicePrivateKey && this.devicePrivateKey.extractable !== false) {
            throw new TypeError("the device private key must be non-extractable");
        }
        this.deviceID = options.deviceID || null;
        this.account = options.account || null;
        this.keyID = options.keyID || "ms-1";
        this.masterKeys = new Map();
        if (options.masterKeys) {
            Object.keys(options.masterKeys).forEach((key) => this.masterKeys.set(key, options.masterKeys[key]));
        }
        if (options.masterKey) this.masterKeys.set(this.keyID, options.masterKey);
        this.senderKeys = new Map();
        if (options.senderKeys) {
            Object.keys(options.senderKeys).forEach((sender) => this.senderKeys.set(sender, options.senderKeys[sender]));
        }
        this.resolveSenderKey = options.resolveSenderKey || null;
        this.allowWrites = options.allowWrites === true;
        this.nextSequence = options.nextSequence || null;
        if (this.allowWrites && typeof this.nextSequence !== "function") {
            throw new TypeError("write-enabled CloudClient needs a durable nextSequence() provider");
        }
        this.WebSocket = options.WebSocket || globalThis.WebSocket;
        this.handlers = options.handlers || null;
        this.socket = null;
        this.ready = false;
        this.listeners = new Set();
        this.pendingSubscriptions = new Set();
        this.sessionSnapshots = new Map();
        this.transcriptSnapshots = new Map();
        this.orchestratorSnapshots = new Map();
        this.readWaiters = new Map();
        this.readTimeoutMs = options.readTimeoutMs || READ_TIMEOUT_MS;
        this.setTimeout = options.setTimeout || globalThis.setTimeout.bind(globalThis);
        this.clearTimeout = options.clearTimeout || globalThis.clearTimeout.bind(globalThis);
        this.sequenceBySender = new Map();
        this.messageChain = Promise.resolve();
    }

    events(listener) {
        if (typeof listener !== "function") throw new TypeError("events() needs a listener");
        this.listeners.add(listener);
        var self = this;
        return function () { self.listeners.delete(listener); };
    }

    _emit(event) {
        this.listeners.forEach(function (listener) {
            try { listener(event); } catch (e) { /* one view cannot stop the transport */ }
        });
    }

    async start() {
        if (this.socket) return;
        if (!this.WebSocket) throw new Error("WebSocket is unavailable");
        if (!this.devicePrivateKey) throw cloudError("missing_device_key", "the viewer device key is unavailable");
        if (this.handlers && this.handlers.conn) this.handlers.conn("connecting");
        var ws = new this.WebSocket(this.url,
            ["clawdline.v1", "clawdline.token." + this.deviceToken]);
        this.socket = ws;
        var self = this;
        ws.onmessage = function (event) {
            self.messageChain = self.messageChain.then(function () {
                return self._receive(event.data);
            }).catch(function (error) {
                self._emit({ type: "error", error: error });
            });
        };
        ws.onerror = function () {
            self._emit({ type: "error", error: cloudError("socket_error", "the cloud connection failed") });
        };
        ws.onclose = function () {
            if (self.socket === ws) self.socket = null;
            self.ready = false;
            // Every read still waiting was waiting on this socket. Left alone they would sit out
            // their whole timeout behind a skeleton for a connection that is already gone.
            self._failAllReads(cloudError("offline", "the cloud connection dropped"));
            if (self.handlers && self.handlers.conn) self.handlers.conn("offline");
            self._emit({ type: "connection", state: "offline" });
        };
    }

    stop() {
        var ws = this.socket;
        this.socket = null;
        this.ready = false;
        this._failAllReads(cloudError("offline", "the cloud connection was stopped"));
        if (ws) ws.close(1000, "viewer stopped");
    }

    refresh() {
        this.stop();
        return this.start();
    }

    async _receive(raw) {
        if (typeof raw !== "string") throw cloudError("bad_frame", "the relay sent a binary frame");
        var frame;
        try { frame = JSON.parse(raw); } catch (e) { throw cloudError("bad_frame", "the relay sent non-JSON"); }
        if (!frame || typeof frame !== "object" || Array.isArray(frame)) {
            throw cloudError("bad_frame", "the relay frame is not an object");
        }
        if (frame.type === "challenge") return this._answerChallenge(frame);
        if (frame.type === "ready") return this._becameReady(frame);
        if (frame.type === "envelope") return this._receiveEnvelope(frame.envelope, frame.realign === true);
        if (frame.type === "ping") {
            this._send({ type: "pong" });
            return;
        }
        if (frame.type === "subscriptions" || frame.type === "ack" || frame.type === "pong") {
            this._emit(frame);
            return;
        }
        if (frame.type === "error") {
            throw cloudError(frame.code || "relay_error", frame.message || "the relay refused a frame");
        }
        throw cloudError("bad_frame", "unknown relay frame type");
    }

    async _answerChallenge(frame) {
        var challengeBytes;
        try { challengeBytes = base64Bytes(frame.challenge, "challenge"); }
        catch (e) { challengeBytes = null; }
        if (frame.v !== 1 || frame.context !== "clawdline-challenge-v1" ||
            typeof frame.account !== "string" || !frame.account ||
            typeof frame.device !== "string" || !frame.device ||
            !Number.isSafeInteger(frame.expires_in_ms) || frame.expires_in_ms <= 0 ||
            !challengeBytes || challengeBytes.length !== 32) {
            throw cloudError("bad_challenge", "the relay challenge is malformed");
        }
        if (this.account && this.account !== frame.account) throw cloudError("wrong_account", "the relay named another account");
        if (this.deviceID && this.deviceID !== frame.device) throw cloudError("wrong_device", "the relay named another device");
        this.account = frame.account;
        this.deviceID = frame.device;
        var base = [frame.context, frame.account, frame.device, frame.challenge].join("|");
        var signature = await crypto.subtle.sign({ name: "Ed25519" }, this.devicePrivateKey,
            textEncoder.encode(base));
        this._send({ type: "hello", sig: bytesToBase64(signature) });
    }

    _becameReady(frame) {
        if (frame.v !== 1 || frame.role !== "viewer" || frame.account !== this.account ||
            frame.device !== this.deviceID) {
            throw cloudError("bad_ready", "the relay ready frame does not match the challenge");
        }
        this.ready = true;
        if (this.handlers && this.handlers.hello) this.handlers.hello({ write: this.allowWrites });
        if (this.handlers && this.handlers.conn) this.handlers.conn("live");
        this._emit({ type: "connection", state: "live", account: this.account, device: this.deviceID });
        if (this.pendingSubscriptions.size) {
            this._send({ type: "subscribe", channels: Array.from(this.pendingSubscriptions) });
        }
    }

    async _senderKey(sender, envelope) {
        var value = this.senderKeys.get(sender);
        if (value === undefined && this.resolveSenderKey) value = await this.resolveSenderKey(sender, envelope);
        if (!value) return null;
        if (typeof value === "string" || value instanceof ArrayBuffer || ArrayBuffer.isView(value)) {
            value = await importSenderPublicKey(value);
            this.senderKeys.set(sender, value);
        }
        return value;
    }

    async _masterKey(keyID) {
        var value = this.masterKeys.get(keyID);
        if (!value) throw cloudError("unknown_key", "no account master key for " + keyID);
        if (typeof value === "string" || value instanceof ArrayBuffer || ArrayBuffer.isView(value)) {
            value = await importMasterSecret(value);
            this.masterKeys.set(keyID, value);
        }
        if (value.extractable !== false) throw cloudError("extractable_key", "the account master key is extractable");
        return value;
    }

    async _receiveEnvelope(envelope, realign) {
        var key = await this._senderKey(envelope && envelope.sender, envelope);
        if (!key) throw cloudError("unknown_sender", "the envelope sender is not paired");
        var clear = await openEnvelope(envelope, await this._masterKey(envelope.key_id), key);
        var previous = this.sequenceBySender.get(envelope.sender);
        if (previous !== undefined && envelope.seq <= previous) throw cloudError("replay", "the envelope sequence did not advance");
        this.sequenceBySender.set(envelope.sender, envelope.seq);
        var channel = parseEnvelopeChannel(envelope.ch);
        var payload;
        try { payload = clear.length ? JSON.parse(textDecoder.decode(clear)) : null; }
        catch (e) { throw cloudError("bad_payload", "the decrypted stream payload is not JSON"); }
        this._applySnapshot(channel, payload, envelope, realign);
    }

    _applySnapshot(channel, payload, envelope, realign) {
        if (channel.kind === "session") {
            var identity = sessionIdentity({ machine: decodedChannelSegment(channel.machine),
                session: decodedChannelSegment(channel.session) });
            var key = sessionIdentityKey(identity);
            var row = payload && Object.prototype.hasOwnProperty.call(payload, "session")
                ? payload.session : payload;
            if (row === null || (payload && payload.deleted === true)) this.sessionSnapshots.delete(key);
            else if (row && typeof row === "object" && !Array.isArray(row)) {
                this.sessionSnapshots.set(key, Object.assign({}, row, {
                    id: row.id || identity.session,
                    machine: identity.machine,
                    session: identity.session,
                    identity: identity
                }));
            } else throw cloudError("bad_payload", "a session snapshot must be an object");
            var sessions = this._sessionResponse(envelope.ts);
            if (this.handlers && this.handlers.sessions) this.handlers.sessions(sessions.sessions, sessions.at, sessions.scan);
            this._emit({ type: "sessions", data: sessions, identity: identity,
                envelope: envelope, realign: realign });
            return;
        }
        if (channel.kind === "transcript") {
            var transcriptIdentity = sessionIdentity({ machine: decodedChannelSegment(channel.machine),
                session: decodedChannelSegment(channel.session) });
            var transcriptKey = sessionIdentityKey(transcriptIdentity);
            var answer = readAnswer(payload);
            // Nothing has ever published on this channel, so its payload is pinned here rather
            // than inherited: an envelope that names no read at all is a protocol error and says
            // so, instead of being stored as a transcript nobody can read. Which read it names is
            // not checked against a list — a viewer only ever waits on names it asked for, and an
            // answer to a read nobody asked for settles nothing.
            if (!answer) throw cloudError("bad_payload", "the read answer names no read");
            if (answer.read === "transcript" && !answer.error) {
                this.transcriptSnapshots.set(transcriptKey, answer.body);
            }
            this._settleRead(readKey(transcriptIdentity, answer.read), answer.body, answer.error);
            this._emit({ type: "read", read: answer.read, data: answer.body,
                error: answer.error, identity: transcriptIdentity, envelope: envelope,
                realign: realign });
            return;
        }
        if (channel.kind === "orch") {
            var machine = decodedChannelSegment(channel.machine);
            this.orchestratorSnapshots.set(machine, payload || {});
            var tasks = this._allOrchestratorRows("tasks");
            if (this.handlers && this.handlers.tasks) this.handlers.tasks(tasks);
            this._emit({ type: "orchestrator", data: payload, machine: machine,
                envelope: envelope, realign: realign });
            return;
        }
        this._emit({ type: channel.kind, data: payload, envelope: envelope, realign: realign });
    }

    _sessionResponse(timestamp) {
        return { sessions: Array.from(this.sessionSnapshots.values()),
            at: timestamp ? Math.floor(timestamp / 1000) : 0,
            scan: { emptyAuthoritative: true, cloud: true } };
    }

    sessions() { return Promise.resolve(this._sessionResponse(0)); }

    /**
     * This session's messages.
     *
     * **Not a cache read.** The channel and the subscription were here before this method could
     * do anything with them — `t/<machine>/<session>` is in the relay and in `cloud-crypto.js`,
     * and this client already subscribed to it when a session was opened — but no Mac had ever
     * published a transcript envelope, so the promise this returned was never settled by anything
     * and the phone sat behind a skeleton for as long as somebody was willing to look at it. What
     * is new is the asking: the read goes up the command channel and its answer comes back down
     * the channel that was already there.
     *
     * `phases` is the direct path's request/parse instrumentation and has no counterpart here —
     * there is no HTTP response to report the status of — so it is accepted and ignored rather
     * than made a different signature. `demand.foreground` likewise: the Mac's transcript lane
     * reads that off the query it builds itself.
     */
    transcript(value) {
        return this._read(value, "transcript", { limit: TRANSCRIPT_LIMIT }, "transcript");
    }

    /**
     * The facts behind the status line and the Session info card.
     *
     * The expensive read on this Mac — it opens a transcript that can be fifty megabytes and runs
     * `git status` — so it goes down the same bounded lane a phone on the tunnel queues in, and
     * can come back refused with `reading_busy` rather than late. It was absent here entirely,
     * and absent in the quietest possible way: `status-line.js` asks `typeof api.info ===
     * "function"` and resolves `null` when it is not, so a console on the cloud path drew a status
     * line with nothing in it and no reason given.
     */
    info(value) {
        return this._read(value, "info", { parts: "full" }, "info.full");
    }

    /** Transcript-derived facts only, the same subset the direct path asks for by query. */
    infoSummary(value) {
        return this._read(value, "info", { parts: "summary" }, "info.summary");
    }

    /**
     * One read, asked on the command channel and answered on the session's own.
     *
     * At most one request per (session, read) is in flight: a second caller joins the first
     * rather than spending another envelope sequence, which is what the direct path's own
     * transcript coalescing does for the same reason.
     */
    _read(value, type, extra, answer) {
        var identity = sessionIdentity(value);
        // The refusal that is deliberate, and it is the relay's rather than this page's: PROTOCOL
        // §12 says publishing to `ctl/` needs `send_prompt`, in either class, and a read has to
        // ask on `ctl/` because that is the only channel a viewer may publish on at all. So a
        // device downgraded to read-only cannot ask for a transcript however much it may read
        // one, and it is told that in a code of its own rather than by a socket error or a
        // skeleton. Widening it is a relay decision, not one this file may take.
        if (!this.allowWrites) {
            return Promise.reject(cloudError("cloud_read_needs_send_prompt",
                "this device may not ask the Mac for reads"));
        }
        var key = readKey(identity, answer);
        var self = this;
        return new Promise(function (resolve, reject) {
            var waiters = self.readWaiters.get(key);
            if (waiters) {
                waiters.waiting.push({ resolve: resolve, reject: reject });
                return;
            }
            waiters = { waiting: [{ resolve: resolve, reject: reject }], timer: null };
            self.readWaiters.set(key, waiters);
            waiters.timer = self.setTimeout(function () {
                self._settleRead(key, null,
                    cloudError("cloud_read_timeout", "the Mac did not answer this read"));
            }, self.readTimeoutMs);
            self.subscribe(["t/" + channelSegment(identity.machine) + "/"
                + channelSegment(identity.session)]);
            Promise.resolve()
                .then(function () {
                    return self._publishCommand(identity.machine, type,
                        Object.assign({ session: identity.session }, extra), "ctl");
                })
                .catch(function (error) { self._settleRead(key, null, error); });
        });
    }

    _settleRead(key, body, error) {
        var waiters = this.readWaiters.get(key);
        if (!waiters) return;
        this.readWaiters.delete(key);
        if (waiters.timer !== null) this.clearTimeout(waiters.timer);
        waiters.waiting.forEach(function (waiter) {
            if (error) waiter.reject(error); else waiter.resolve(body);
        });
    }

    _failAllReads(error) {
        var keys = Array.from(this.readWaiters.keys());
        var self = this;
        keys.forEach(function (key) { self._settleRead(key, null, error); });
    }

    subscribe(channels) {
        if (!Array.isArray(channels) || !channels.length) throw new TypeError("subscribe needs channels");
        channels.forEach((channel) => {
            parseEnvelopeChannel(channel);
            this.pendingSubscriptions.add(channel);
        });
        if (this.ready) this._send({ type: "subscribe", channels: channels });
        return this;
    }

    _allOrchestratorRows(name) {
        var rows = [];
        this.orchestratorSnapshots.forEach(function (snapshot, machine) {
            var list = snapshot && Array.isArray(snapshot[name]) ? snapshot[name] : [];
            list.forEach(function (row) { rows.push(Object.assign({}, row, { machine: machine })); });
        });
        return rows;
    }

    tasks() { return Promise.resolve({ tasks: this._allOrchestratorRows("tasks") }); }
    schedules() { return Promise.resolve({ schedules: this._allOrchestratorRows("schedules") }); }

    send(value, text, images) {
        var identity = sessionIdentity(value);
        return this._publishCommand(identity.machine, "send", {
            session: identity.session, text: text || "", images: images || []
        }, "ctl");
    }

    answer(value, answer) {
        var identity = sessionIdentity(value);
        return this._publishCommand(identity.machine, "answer", {
            session: identity.session, answer: String(answer)
        }, "ctl");
    }

    key(value, answer) { return this.answer(value, answer); }

    /**
     * Naming a session is a local operation on the Mac that owns it, and the cloud protocol has
     * no envelope class for it — a session title is written into that Mac's config and read back
     * by its own panel, which is not something a relay can carry today.
     *
     * The sentence comes from the string table rather than being written here, because it is
     * shown to a person: `info.js` puts `error.message` straight on the card, so an English
     * literal at this line is an English sentence on a page in thirteen other languages, and it
     * is exactly the kind of literal `tools/check-web-strings.py` exists to keep out.
     */
    title(value) {
        sessionIdentity(value);
        return Promise.reject(cloudError("unsupported", T.webInfoTitleCloud));
    }

    /**
     * One background agent's conversation.
     *
     * The first of the four that used to be a crash — `session/agent.js` calls `api.agent(…)`
     * unguarded, so on this path it threw `api.agent is not a function` at whoever pressed the
     * button — then a typed `cloud_read_unavailable`, and now a read like the rest. It answers
     * the same `{agent, entries, signature}` the direct route answers, so nothing above this line
     * needs a cloud branch: the panel's signature bargain, which is what stops a refetch from
     * throwing a reader's scroll position away, works here because it is the Mac's own signature.
     *
     * **The id travels into the answer's name.** A session has many agents and one answer
     * channel, so `agent:<id>` is what a waiter waits on; see `CloudHeadlessRead.name`.
     */
    agent(value, agentId) {
        var agent = readSubject(agentId);
        if (!agent) return Promise.reject(missingSubject("agent"));
        return this._read(value, "agent", { agent: agent, limit: AGENT_LIMIT },
                          "agent:" + agent);
    }

    /** One background command's output — text and `ended`, because a command has no turns. */
    shell(value, shellId) {
        var shell = readSubject(shellId);
        if (!shell) return Promise.reject(missingSubject("shell"));
        return this._read(value, "shell", { shell: shell, bytes: SHELL_BYTES },
                          "shell:" + shell);
    }

    /** The commands this session's assistant can be offered in the composer. Metadata only. */
    skills(value) { return this._read(value, "skills", {}, "skills"); }

    /**
     * The Git panel.
     *
     * The one of the four the person uses most, and the one whose call site already branches on
     * a typed code: `git-panel.js` shows a different sentence for `not_a_repo` than for anything
     * else. That code arrives here as the Mac's own word, forwarded by the bridge rather than
     * translated, which is the whole reason a refusal crosses as `error` and not as an empty
     * body — so the panel that could say "this session is not inside a Git repository" over the
     * tunnel can say it over the relay too, in the same branch.
     */
    git(value) { return this._read(value, "git", {}, "git"); }

    dispatch(machine, task) {
        if (machine && typeof machine === "object") {
            task = task || machine.task;
            machine = machine.machine;
        }
        if (typeof machine !== "string" || !machine) throw new TypeError("dispatch needs a machine");
        return this._publishCommand(machine, "dispatch", { task: task }, "dispatch");
    }

    async _publishCommand(machine, type, body, envelopeClass) {
        if (!this.allowWrites) throw cloudError("cloud_read_only", "cloud writes are disabled");
        if (!this.ready) throw cloudError("offline", "the cloud connection is not ready");
        if (!this.devicePrivateKey || !this.deviceID) throw cloudError("missing_device_key", "the viewer device key is unavailable");
        var sequence = await this.nextSequence(this.deviceID);
        if (!Number.isSafeInteger(sequence) || sequence < 0) {
            throw cloudError("bad_sequence", "nextSequence() did not return a non-negative safe integer");
        }
        var envelope = await sealEnvelope({
            ch: "ctl/" + channelSegment(machine),
            seq: sequence,
            ts: Date.now(),
            class: envelopeClass,
            key_id: this.keyID,
            sender: this.deviceID
        }, JSON.stringify(Object.assign({ type: type }, body)),
        await this._masterKey(this.keyID), this.devicePrivateKey);
        this._send({ type: "publish", envelope: envelope });
        return envelope;
    }

    _send(frame) {
        if (!this.socket || this.socket.readyState !== 1) throw cloudError("offline", "the cloud socket is not open");
        this.socket.send(JSON.stringify(frame));
    }
}

function bytesToBase64(value) {
    var bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
    var binary = "";
    for (var i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
    return btoa(binary);
}
