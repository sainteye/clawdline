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

function cloudError(code, message) {
    var error = new Error(message || code);
    error.code = code;
    return error;
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
        this.transcriptWaiters = new Map();
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
            if (self.handlers && self.handlers.conn) self.handlers.conn("offline");
            self._emit({ type: "connection", state: "offline" });
        };
    }

    stop() {
        var ws = this.socket;
        this.socket = null;
        this.ready = false;
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
            this.transcriptSnapshots.set(transcriptKey, payload);
            var waiters = this.transcriptWaiters.get(transcriptKey) || [];
            this.transcriptWaiters.delete(transcriptKey);
            waiters.forEach(function (resolve) { resolve(payload); });
            this._emit({ type: "transcript", data: payload, identity: transcriptIdentity,
                envelope: envelope, realign: realign });
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

    transcript(value) {
        var identity = sessionIdentity(value);
        var key = sessionIdentityKey(identity);
        if (this.transcriptSnapshots.has(key)) return Promise.resolve(this.transcriptSnapshots.get(key));
        this.subscribe(["t/" + channelSegment(identity.machine) + "/" + channelSegment(identity.session)]);
        var self = this;
        return new Promise(function (resolve) {
            var waiters = self.transcriptWaiters.get(key) || [];
            waiters.push(resolve);
            self.transcriptWaiters.set(key, waiters);
        });
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
