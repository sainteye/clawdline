import {
    boardReportSelection,
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
import { machinePresentation, machinePresentationForFleet } from "../session/selection.js";
import {
    CloudTrail, DIAGNOSTIC_REPORT_MAX_BYTES, RELAY_CTL_MAX_BYTES, asCloudFailure, cloudFailure,
    failureFromMac, failureFromRelay, closeCodeName, isFailureCode
} from "./cloud-failure.js";
import {
    documentAnswer, documentListing, normalizeDocumentIdentity, normalizeDocumentLocator
} from "./document-links.js";
import {
    PAGE_TAB_ID, VIEWER_EVENTS_COMMAND, ViewerEventLog, envelopeMetadata, errorFields, pageContext
} from "./cloud-viewer-events.js";

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
const VOICE_TIMEOUT_MS = 6 * 60 * 1000;
const MACHINE_INVENTORY_FRESH_MS = 5 * 60 * 1000;
// Relay realignment is channel-by-channel and has no end marker. Keep the fleet surface honest
// for one bounded window after authentication: rows already recovered are usable, while the UI
// still says that other machines may be arriving.
const MACHINE_INVENTORY_SYNC_MS = 60 * 1000;
const MACHINE_DESCRIPTOR_CACHE = "clawdline.machine-descriptors.v1:";
// Which Mac this browser chose to transcribe its dictation (`voiceHost`). A preference kept per
// browser and per account, never a routing authority of its own.
const VOICE_HOST_CHOICE = "clawdline.voice-host.v1:";

/**
 * The browser commands an enrolled Linux executor answers, and it answers nothing else:
 * `LinuxDurableCloudRuntime.swift` adapts exactly these two. A newer executor refuses any other
 * word as `unknown_command`; one already deployed drops it without a reply, which is why this list
 * decides before anything is sent rather than after a timeout. A descriptor that advertises its
 * own `commands` is taken at its word instead.
 */
const LINUX_BROWSER_COMMANDS = Object.freeze(["places", "start"]);

/** Commands a Mac takes only once it has shown `cloud_status.v >= 1` (§11.4). */
const STATUS_GATED_COMMANDS = Object.freeze(["cloud.status", "diagnostics.report", VIEWER_EVENTS_COMMAND]);

/**
 * Commands every platform this page knows implements: a Mac answers every browser command, and a
 * Linux executor answers `LINUX_BROWSER_COMMANDS`. A machine whose descriptor has not arrived is
 * one of the two, so for these words it is not unknown — it answers. Today that is `places` and
 * `start`.
 */
const UNIVERSAL_COMMANDS = Object.freeze(LINUX_BROWSER_COMMANDS.filter(function (type) {
    return STATUS_GATED_COMMANDS.indexOf(type) < 0;
}));

/** Refusals that mean "this machine cannot have the feature", which a fan-out read drops. */
const UNSUPPORTED_CODES = Object.freeze(["unknown_command", "cloud_machine_unsupported"]);

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
const SESSION_INVENTORY_ID = "__clawdline_inventory_v1__";
const SESSION_INVENTORY_LIMIT = 512;

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

/**
 * How many pictures may be in the air at once.
 *
 * A transcript holds up to six images per message across a two-hundred-message window, and the
 * renderer connects every fresh tile in one pass — so without a cap, opening a session with forty
 * screenshots in it asks for forty envelopes at once. Each one is the PNG plus a third for base64
 * plus the frame around it, so forty one-megabyte pictures is something like a hundred and sixty
 * megabytes of strings alive together in a phone browser, for a screenful of pictures of which
 * two are on screen.
 *
 * Three is the number a browser would have chosen for us on the direct path: same-origin `<img>`
 * requests queue behind a per-host connection limit of six, and half of that is the right side of
 * it to be on when each request is a megabyte rather than a header. Nothing is refused here and
 * nothing needs saying in the interface — the fourth picture waits, it does not fail.
 */
const IMAGE_READS_IN_FLIGHT = 3;
const MACHINE_REPLY_SESSION = "__clawdline_machine__";

/**
 * How long a command that waits only for the relay's word waits for it. The relay answers every
 * publish with `ack` or `publish_error` in the same turn (`account-do.ts`), so this is a bound on
 * a socket that has stopped talking, not a guess at latency; at the bound the command resolves as
 * it always did, having been written to the socket.
 */
const ACK_TIMEOUT_MS = 10000;

/** How long a timed-out read waits for the Mac's `cloud.status` before it says only "no answer". */
const STATUS_PROBE_TIMEOUT_MS = 10000;

/**
 * This page's name among other tabs of the same device (§6.3). One per page load, not per
 * client: a token renewal replaces the client inside the same tab, and that is not a second tab.
 */
const TAB_ID = PAGE_TAB_ID;

function requestID() {
    if (globalThis.crypto && typeof globalThis.crypto.randomUUID === "function") {
        return globalThis.crypto.randomUUID();
    }
    // Only old browsers reach this branch. Random bytes, not a counter: two tabs share one Mac
    // answer channel and a reload must not make one tab accept the other's answer.
    var bytes = new Uint8Array(16);
    globalThis.crypto.getRandomValues(bytes);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    var hex = Array.from(bytes, function (byte) { return byte.toString(16).padStart(2, "0"); }).join("");
    return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16),
        hex.slice(16, 20), hex.slice(20)].join("-");
}

function cloudPlaceID(machine, place) {
    return "cloud." + bytesToBase64(textEncoder.encode(JSON.stringify([machine, place])))
        .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/**
 * The bytes of one picture, checked before anything renders them.
 *
 * `media_type` is pinned rather than trusted: the store only ever writes PNG, and a blob URL
 * built from whatever a payload claimed would be a second, quieter place to decide what a
 * document is. `byte_count` is checked against the bytes it counts because base64 that decodes
 * to the wrong length is a truncated answer, and a truncated PNG renders as the broken-image
 * icon this whole path exists to remove.
 */
function imageAnswerBytes(id, body) {
    if (!body || typeof body !== "object" || Array.isArray(body) || body.id !== id ||
        body.media_type !== "image/png" || typeof body.data !== "string" || !body.data) {
        throw cloudError("bad_payload", "the image answer is not this image");
    }
    var bytes;
    try { bytes = base64Bytes(body.data, "image data"); }
    catch (e) { throw cloudError("bad_payload", "the image answer is not base64"); }
    if (!bytes.length ||
        (Number.isSafeInteger(body.byte_count) && body.byte_count !== bytes.length)) {
        throw cloudError("bad_payload", "the image answer does not carry the bytes it counts");
    }
    return { id: id, media_type: body.media_type, bytes: bytes };
}

/** A refusal this page makes itself: `layer: "browser"`, and a code a page can name. */
function cloudError(code, message) {
    return cloudFailure(code, message);
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
        // Preserve the authenticated Mac's typed refusal: its layer, the sequence it answers,
        // the status a caller branches on before releasing a retry intent, and the `detail`
        // fields §11.6 allows for that code — which used to be dropped here, so Cloud could not
        // tell `no_model` from `no_binary` or put `{app}` into a sentence (B6).
        return { read: payload.read, body: null, error: failureFromMac(error, payload.status, null) };
    }
    return { read: payload.read, body: payload.body === undefined ? null : payload.body,
        error: null };
}

/** The command words a descriptor advertises, bounded, or null when it advertises none. */
function descriptorCommands(value) {
    if (!Array.isArray(value)) return null;
    return value.slice(0, 64).filter(function (word) {
        return typeof word === "string" && word.length > 0 && word.length <= 64;
    });
}

/** A descriptor's platform as the fleet presentation spells it: trimmed, lowercase, or "". */
function descriptorPlatform(descriptor) {
    return descriptor && typeof descriptor.platform === "string" ? descriptor.platform.trim().toLowerCase() : "";
}

/** `read:<id>` for `action:<id>` and the reverse, on the machine reply channel only; else null. */
function siblingRequestName(session, read) {
    if (session !== MACHINE_REPLY_SESSION) return null;
    var match = /^(read|action):(.+)$/.exec(read);
    return match ? (match[1] === "read" ? "action:" : "read:") + match[2] : null;
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
 *
 * **A method a page awaits answers every failure as a rejected promise, never as a throw.** The
 * page writes `api.x(…).then(ok).catch(say)` and puts its in-flight flag down in that settle; a
 * synchronous throw skips both. On the start sheet it left "Starting…" on a row, every other row
 * shut and Close dead until the page was reloaded. `Tests/web-start-sheet-failures.mjs` finds
 * these methods by enumeration and calls each one with inputs that fail.
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
        // Display metadata only. The route remains the opaque authenticated channel id. A full
        // PWA process restart used to forget names until the next `orch/` envelope happened to
        // realign, even though Session rows (and therefore opaque ids) had already arrived.
        this.descriptorStorage = options.descriptorStorage || null;
        this.voiceHostStorage = options.voiceHostStorage || null;
        // Deliberately excludes the short-lived viewer token. `useClient` uses this value to
        // distinguish credential rotation from a real relay/account/device replacement.
        this.selectionTransportIdentity = ["cloud", this.url, this.account || "",
            this.deviceID || ""].join("\u0000");
        this.keyID = options.keyID || "ms-1";
        this.masterKeys = new Map();
        if (options.masterKeys) {
            Object.keys(options.masterKeys).forEach((key) => this.masterKeys.set(key, options.masterKeys[key]));
        }
        if (options.masterKey) this.masterKeys.set(this.keyID, options.masterKey);
        this.machinePairings = new Map();
        if (options.machinePairings) {
            Object.keys(options.machinePairings).forEach((machine) =>
                this.machinePairings.set(machine, options.machinePairings[machine]));
        }
        this.resolveMachinePairing = options.resolveMachinePairing || null;
        this.bindLegacyMachine = options.bindLegacyMachine || null;
        // A composition root that supplies either hook has opted into strict machine scoping.
        // The fallback keeps old isolated CloudClient callers source-compatible; hosted boot
        // always supplies both hooks and therefore never guesses an unknown outbound target.
        this.machineKeyScoping = !!(this.resolveMachinePairing || this.bindLegacyMachine ||
            options.machinePairings);
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
        this.connectionAnnounced = false;
        this.listeners = new Set();
        this.pendingSubscriptions = new Set();
        // A short-lived viewer token creates a new socket and a new CloudClient. The relay then
        // realigns one retained `s/<machine>/<session>` channel at a time, in no inventory order.
        // Starting that reconstruction from an empty Map made the first envelope look like the
        // whole account: if the phone was reading another session, `handlers.sessions` closed it
        // before that session's envelope arrived. Carry only the last-known-good session rows
        // from the same account and viewer device. Real tombstones still remove copied rows; no
        // read waiter or inbound sequence is inherited across sockets.
        var prior = options.resumeFrom;
        var sameViewer = prior instanceof CloudClient && !!this.account && !!this.deviceID
            && prior.account === this.account && prior.deviceID === this.deviceID;
        this.sessionSnapshots = sameViewer
            ? new Map(prior.sessionSnapshots) : new Map();
        this.sessionSequenceByKey = sameViewer
            ? new Map(prior.sessionSequenceByKey) : new Map();
        this.sessionInventoryByMachine = new Map();
        if (sameViewer) prior.sessionInventoryByMachine.forEach(function (inventory, machine) {
            this.sessionInventoryByMachine.set(machine,
                { sequence: inventory.sequence, ids: new Set(inventory.ids) });
        }, this);
        this.transcriptSnapshots = new Map();
        this.orchestratorSnapshots = new Map();
        // This is evidence of the last authenticated envelope seen from a machine, not a
        // presence lease.  Carry it across a viewer-token renewal just like the retained
        // Session rows: it decides whether the picker may take the one-machine fast path, while
        // an older route remains available for an explicit bounded probe.
        this.machineObservedAt = sameViewer ? new Map(prior.machineObservedAt) : new Map();
        // The rows a page was handed by `places()` outlive the socket that read them: the start
        // sheet keeps its list on screen across a renewal, and a press on one of those rows used
        // to find this Map empty and die before reaching any Mac.
        this.placeRoutes = sameViewer ? new Map(prior.placeRoutes) : new Map();
        // The voice host chosen on this page. It wins over `voiceHostStorage`, so a press still
        // counts in a browser whose storage refuses to keep it.
        this.voiceHostChoice = sameViewer && typeof prior.voiceHostChoice === "string"
            ? prior.voiceHostChoice : null;
        this.readWaiters = new Map();
        this.imageReadsInFlight = options.imageReadsInFlight || IMAGE_READS_IN_FLIGHT;
        this.imageReadQueue = [];
        this.imageReadsRunning = 0;
        this.readTimeoutMs = options.readTimeoutMs || READ_TIMEOUT_MS;
        this.setTimeout = options.setTimeout || globalThis.setTimeout.bind(globalThis);
        this.clearTimeout = options.clearTimeout || globalThis.clearTimeout.bind(globalThis);
        this.readyWaiters = [];
        this.sequenceBySender = new Map();
        this.realignSequenceByChannel = new Map();
        this.messageChain = Promise.resolve();
        // The failure contract (`docs/cloud-error-transparency.md` §2, §11). The trail and the
        // Macs that have shown `cloud_status.v >= 1` belong to the viewer, so a renewal carries
        // them like the session rows; pending sequences belong to one socket and never do.
        this.trail = options.trail
            || (sameViewer && prior.trail instanceof CloudTrail ? prior.trail : new CloudTrail());
        this.macCapabilities = sameViewer ? new Set(prior.macCapabilities) : new Set();
        // Words a machine answered `unknown_command` to, per machine. One client's memory: a renewal
        // asks again, and a machine publishing a different app build is asked again.
        this.machineLacks = new Map();
        // What this browser saw fail, kept until the paired Mac's receipt names it
        // (`cloud-viewer-events.js`). The log outlives a renewal like the trail. Delivery runs only
        // for a log the composition root handed in, so a client a test builds never sends on its
        // own and never adds a timer to the read timers a test counts.
        var handedLog = options.viewerEvents instanceof ViewerEventLog;
        this.viewerEvents = handedLog ? options.viewerEvents
            : sameViewer && prior.viewerEvents instanceof ViewerEventLog ? prior.viewerEvents
                : new ViewerEventLog();
        this.viewerEventDelivery = handedLog || (sameViewer && prior.viewerEventDelivery === true);
        this.viewerEventTimers = options.viewerEventTimers
            || (sameViewer && prior.viewerEventTimers)
            || { setTimeout: function (fn, ms) {
                var timer = globalThis.setTimeout(fn, ms);
                // Under node a pending delivery must not hold a finished suite open.
                if (timer && typeof timer.unref === "function") timer.unref();
                return timer;
            }, clearTimeout: globalThis.clearTimeout.bind(globalThis) };
        this.viewerEventTimer = null;
        this.viewerEventsOff = null;
        this.lastViewerDelivery = null;
        this.webBuild = typeof options.webBuild === "string" ? options.webBuild.slice(0, 64)
            : sameViewer && typeof prior.webBuild === "string" ? prior.webBuild : "";
        // Receive-failure context. `readyAt` and the open counts belong to one socket; the sender
        // and machine-pairing lookups and the machines seen signing an `orch/` snapshot belong to
        // the viewer.
        this.readyAt = null;
        this.opensSinceReady = new Map();
        this.senderKeyLookups = sameViewer ? new Map(prior.senderKeyLookups) : new Map();
        this.pairingLookups = sameViewer ? new Map(prior.pairingLookups) : new Map();
        this.viewerVerified = sameViewer ? new Map(prior.viewerVerified) : new Map();
        // What each authenticated `orch/<machine>` snapshot said the machine is — its descriptor
        // and app build — kept apart from the snapshot, which a renewal starts empty, so the
        // renewed client still knows a Linux executor from a Mac before any snapshot is realigned.
        this.machineDescriptors = sameViewer
            ? new Map(prior.machineDescriptors) : this._loadMachineDescriptors();
        // Machines whose envelopes this browser cannot attribute to any pairing: no pairing, no
        // legacy binding, no key for the sender (`_openEnvelopeFrame`). Per machine, never the
        // account's door; a renewal keeps it, and a pairing found for the machine clears it.
        this.unpairedMachines = sameViewer ? new Map(prior.unpairedMachines) : new Map();
        // Machines the pairing store answered "none" for, and never anything else, so the next
        // envelope from one does not open IndexedDB again. One client's memory only: a renewal asks
        // again, and a pairing completed in this page clears the machine (`forgetMachinePairingAnswer`).
        this.pairingAbsent = new Set();
        this.pairingSeen = sameViewer ? new Set(prior.pairingSeen) : new Set();
        this.pendingBySequence = new Map();
        this.lastRelayError = null;
        this.closedFailure = null;
        this.ackTimeoutMs = options.ackTimeoutMs || ACK_TIMEOUT_MS;
        this.statusProbeTimeoutMs = options.statusProbeTimeoutMs || STATUS_PROBE_TIMEOUT_MS;
        this.tabID = options.tabID || TAB_ID;
        this.BroadcastChannel = options.BroadcastChannel !== undefined
            ? options.BroadcastChannel : globalThis.BroadcastChannel;
        this.tabChannel = null;
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

    async start(options) {
        if (this.socket) return;
        if (!this.WebSocket) throw cloudError("websocket_unavailable", "WebSocket is unavailable");
        if (!this.devicePrivateKey) throw cloudError("missing_device_key", "the viewer device key is unavailable");
        this.connectionAnnounced = !(options && options.quiet === true);
        if (this.connectionAnnounced && this.handlers && this.handlers.conn) {
            this.handlers.conn("connecting");
        }
        var ws = new this.WebSocket(this.url,
            ["clawdline.v1", "clawdline.token." + this.deviceToken]);
        this.socket = ws;
        var self = this;
        ws.onmessage = function (event) {
            self.messageChain = self.messageChain.then(function () {
                return self._receive(event.data);
            }).catch(function (error) {
                self._recordFrameFailure(error, event.data);
                self._emit({ type: "error", error: error });
            });
        };
        ws.onerror = function () {
            self._emit({ type: "error", error: cloudError("socket_error", "the cloud connection failed") });
        };
        ws.onclose = function (event) {
            // A socket this client closed itself (`stop`, `retire`) has already failed what it
            // held, and its trail may belong to the replacement by now: nothing below is its to say.
            var ours = self.socket === ws;
            if (ours) self.socket = null;
            self.ready = false;
            // B3: the relay's own word for why, which used to be thrown away with the event. The
            // last `error` frame names it best; the close code is the fallback (§11.7).
            var closeCode = event && Number.isInteger(event.code) ? event.code : null;
            // Most relay `error` frames refuse one frame and deliberately keep the socket open.
            // A later network close must not inherit that old refusal. `token_superseded` is the
            // only closing reason that is more specific than its 4401 close code.
            var framedCode = self.lastRelayError && self.lastRelayError.code;
            var relayCode = framedCode === "token_superseded" && closeCode === 4401
                ? framedCode : closeCodeName(closeCode);
            var dropped = relayCode
                ? failureFromRelay(relayCode, { message: "the relay closed the connection" })
                : cloudError("offline", "the cloud connection dropped");
            if (ours) {
                self.closedFailure = dropped;
                self.trail.closed(closeCode);
                self.trail.connectionState("offline");
            }
            self._disarmViewerEvents();
            self._closeTabChannel();
            self._settleReady(dropped);
            // Every read still waiting was waiting on this socket. Left alone they would sit out
            // their whole timeout behind a skeleton for a connection that is already gone.
            self._failAllReads(dropped);
            if (self.connectionAnnounced && self.handlers && self.handlers.conn) {
                self.handlers.conn("offline");
            }
            self._emit({ type: "connection", state: "offline", code: closeCode,
                failure: relayCode || null });
        };
    }

    stop() {
        this._shutdown(cloudError("offline", "the cloud connection was stopped"));
    }

    /** Hand an authenticated replacement client the UI before closing this socket. A retired
     *  socket may still fail its own in-flight reads, but it must not overwrite the replacement's
     *  connection indicator with an `offline` callback.
     *
     *  What it fails them with is `cloud_reconnecting` (B7): a renewal is this page's own
     *  credential maintenance, the person can press again at once, and "the cloud connection
     *  dropped" said the opposite of both. */
    retire() {
        this.connectionAnnounced = false;
        this.handlers = null;
        this._shutdown(cloudError("cloud_reconnecting", "the cloud connection is being renewed"),
            cloudError("delivery_unconfirmed",
                "the key was written before the cloud connection renewed; delivery is unknown"));
    }

    _shutdown(failure, acknowledgementFailure) {
        var ws = this.socket;
        this.socket = null;
        this.ready = false;
        this.closedFailure = failure;
        this._disarmViewerEvents();
        this._closeTabChannel();
        this._settleReady(failure);
        this._failAllReads(failure, acknowledgementFailure);
        if (ws) ws.close(1000, "viewer stopped");
    }

    /** Opening a WebSocket is not authentication. The hosted boot must not install this client
     *  until the signed challenge has produced the relay's `ready` frame; otherwise the first
     *  transcript request races the handshake and visibly fails with "connection is not ready". */
    whenReady() {
        if (this.ready) return Promise.resolve(this);
        if (!this.socket) {
            return Promise.reject(cloudError("offline", "the cloud socket is not open"));
        }
        var self = this;
        return new Promise(function (resolve, reject) {
            self.readyWaiters.push({ resolve: resolve, reject: reject });
        });
    }

    _settleReady(error) {
        var waiters = this.readyWaiters.splice(0);
        var self = this;
        waiters.forEach(function (waiter) {
            if (error) waiter.reject(error); else waiter.resolve(self);
        });
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
        if (frame.type === "ack") {
            this._relayAnswered(frame, null);
            this._emit(frame);
            return;
        }
        if (frame.type === "publish_error") {
            this._relayAnswered(frame, failureFromRelay(frame.code, {
                field: frame.field, fromPublishError: true,
                message: "the relay refused this publish"
            }));
            this._emit(frame);
            return;
        }
        if (frame.type === "subscriptions" || frame.type === "pong") {
            this._emit(frame);
            return;
        }
        if (frame.type === "error") {
            var relayCode = isFailureCode(frame.code) ? frame.code : "relay_error";
            this.lastRelayError = { code: relayCode };
            this.trail.relayError(relayCode);
            throw failureFromRelay(relayCode, { message: "the relay refused a frame" });
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
        this.readyAt = this.viewerEvents.now();
        this.opensSinceReady = new Map();
        this.connectionAnnounced = true;
        this.lastRelayError = null;
        this.closedFailure = null;
        this.trail.connectionState("live");
        this._armViewerEvents();
        this._openTabChannel();
        this._settleReady(null);
        if (this.handlers && this.handlers.hello) this.handlers.hello({ write: this.allowWrites });
        if (this.handlers && this.handlers.conn) this.handlers.conn("live");
        this._emit({ type: "connection", state: "live", account: this.account, device: this.deviceID });
        if (this.pendingSubscriptions.size) {
            this._send({ type: "subscribe", channels: Array.from(this.pendingSubscriptions) });
        }
    }

    async _senderKey(sender, envelope, probe) {
        var value = this.senderKeys.get(sender);
        var source = value === undefined ? null : "memory";
        if (value === undefined && this.resolveSenderKey) {
            source = "store";
            if (probe) probe.senderKeySource = source;
            var asked = Date.now();
            try { value = await this.resolveSenderKey(sender, envelope); }
            finally { if (probe) probe.senderKeyLookupMs = Math.max(0, Date.now() - asked); }
        }
        // Observation only: whether a key was found, and where. The key itself is not kept here
        // — a store answer is still read again for the next envelope, exactly as before.
        if (probe) {
            probe.senderKeySource = source;
            probe.senderKeyFound = !!value;
        }
        if (typeof sender === "string" && sender) {
            this.senderKeyLookups.delete(sender);
            this.senderKeyLookups.set(sender, !!value);
            if (this.senderKeyLookups.size > 8) {
                this.senderKeyLookups.delete(this.senderKeyLookups.keys().next().value);
            }
        }
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

    async _machinePairing(machine, probe) {
        if (probe) {
            probe.pairingFoundBefore = this.pairingLookups.has(machine)
                ? this.pairingLookups.get(machine) : null;
        }
        var value = this.machinePairings.get(machine);
        var source = value === undefined ? null : "memory";
        if (value === undefined && this.pairingAbsent.has(machine)) {
            value = null;
            source = "memory";
        } else if (value === undefined && this.resolveMachinePairing) {
            source = "store";
            if (probe) probe.pairingSource = source;
            var asked = Date.now();
            try { value = await this.resolveMachinePairing(machine); }
            finally { if (probe) probe.pairingLookupMs = Math.max(0, Date.now() - asked); }
            if (value) {
                this.machinePairings.set(machine, value);
                this.pairingSeen.add(machine);
                this.unpairedMachines.delete(machine);
            } else if ((value === null || value === undefined) && !this.pairingSeen.has(machine)) {
                // Kept only for a machine this viewer never found a pairing for: a store that said
                // yes once and now says nothing (a resume) is asked again at every envelope.
                this.pairingAbsent.add(machine);
            }
        }
        // Observation only: whether this browser holds a pairing for the machine, which kind, and
        // under which key id. The record is written before the checks below, so a pairing they
        // refuse is still named in the row that reports the refusal.
        if (probe) {
            probe.pairingSource = source;
            probe.pairing = value || null;
        }
        this._notePairingLookup(machine, value);
        if (!value) return null;
        if (value.machineID !== machine || typeof value.senderID !== "string" || !value.senderID ||
            typeof value.keyID !== "string" || !value.keyID || !value.masterKey || !value.senderKey) {
            this._noteUnpairedMachine(machine, value && value.senderID, "machine_key_incomplete");
            // An inbound envelope can name any enrolled machine on the account. A stale or
            // half-written pairing for a second machine is that device's actionable state; it
            // must not raise the account-wide pairing door and hide Sessions from healthy
            // machines. Outbound work has selected this machine explicitly, so it keeps the
            // actionable `machine_key_incomplete` error below.
            if (probe) {
                throw cloudFailure("machine_not_paired",
                    "this browser's pairing for the envelope's machine is incomplete",
                    { detail: { machine: machine, reason: "machine_key_incomplete" } });
            }
            throw cloudError("machine_key_incomplete",
                "This browser's pairing for the selected machine is incomplete. "
                + "Start the Pair a Browser flow on that machine, then try again.");
        }
        if (value.masterKey.extractable !== false || value.senderKey.extractable !== false) {
            throw cloudError("extractable_key", "the paired machine keys are extractable");
        }
        return value;
    }

    /**
     * A pairing for `machine` was just stored in this page: its earlier "none" no longer holds.
     * The composition root calls this when a Pair a Browser flow completes.
     */
    forgetMachinePairingAnswer(machine) {
        this.pairingAbsent.delete(machine);
        this.unpairedMachines.delete(machine);
    }

    /**
     * What this browser knows about reading `machine`: `not_paired` when its envelopes arrived and
     * nothing here — no pairing, no legacy binding, no sender key — could be applied to them, with
     * the sender they named. A surface that offers Pair a Browser for a selected machine reads
     * this; the account's door does not.
     */
    machineAccess(machine) {
        var seen = this.unpairedMachines.get(machine);
        return seen ? Object.assign({ state: "not_paired",
            code: seen.code || "machine_not_paired" }, seen) : null;
    }

    /** The descriptor and app build the last authenticated `orch/` snapshot of `machine` named. */
    machineDescriptor(machine) {
        var known = this.machineDescriptors.get(machine);
        return known ? Object.assign({}, known) : null;
    }

    /** Which machines this viewer's store answered with a pairing and which with none; ids only. */
    _notePairingLookup(machine, value) {
        if (typeof machine !== "string" || !machine) return;
        this.pairingLookups.delete(machine);
        this.pairingLookups.set(machine, !!value);
        if (this.pairingLookups.size > 8) {
            this.pairingLookups.delete(this.pairingLookups.keys().next().value);
        }
    }

    async _outboundMachinePairing(machine) {
        var pairing = await this._machinePairing(machine);
        if (pairing) return pairing;
        if (!this.machineKeyScoping) {
            return { machineID: machine, senderID: "", keyID: this.keyID,
                masterKey: await this._masterKey(this.keyID), senderKey: {} };
        }
        throw cloudError("machine_pairing_required",
            "This browser is not paired with the selected machine. "
            + "Start the Pair a Browser flow on that machine, then try again.");
    }

    /**
     * One envelope, and — when it fails — one row saying where and why (`cloud-viewer-events.js`).
     *
     * The row is written here, at the failure, rather than by whoever listens to the `error`
     * event: by then `unreadable_envelope` has already replaced the exception that meant
     * something. `probe.stage` is set before each step, so the stage is where it threw.
     */
    async _receiveEnvelope(envelope, realign) {
        var probe = { stage: "other", original: null, cause: null, senderKeyFound: null,
            senderKeySource: null, senderKeyLookupMs: null, routedMachine: null,
            pairing: undefined, pairingSource: null, pairingLookupMs: null, pairingFoundBefore: null };
        try {
            return await this._openEnvelopeFrame(envelope, realign, probe);
        } catch (error) {
            this._recordReceiveFailure(error, envelope, realign, probe);
            throw error;
        }
    }

    async _openEnvelopeFrame(envelope, realign, probe) {
        probe.stage = "channel_parse";
        var channel = parseEnvelopeChannel(envelope && envelope.ch);
        var routedMachine = channel.kind === "session" || channel.kind === "transcript" ||
            channel.kind === "orch" || channel.kind === "ctl"
            ? decodedChannelSegment(channel.machine) : null;
        probe.routedMachine = routedMachine;
        probe.stage = "machine_pairing_lookup";
        var pairing = routedMachine ? await this._machinePairing(routedMachine, probe) : null;
        probe.stage = "pairing_key_id";
        this._compareKeyID(envelope, routedMachine, pairing && pairing.keyID);
        if (pairing && pairing.keyID !== envelope.key_id) {
            throw cloudError("unknown_key",
                "the envelope does not match the selected machine's pairing");
        }
        probe.stage = "sender_key_lookup";
        if (pairing) {
            probe.senderKeySource = "pairing";
            probe.senderKeyFound = true;
        }
        var key = pairing ? pairing.senderKey
            : await this._senderKey(envelope && envelope.sender, envelope, probe);
        if (!key && routedMachine && this.machineKeyScoping && probe.pairing === null) {
            // A machine this browser holds no pairing for, from a sender it holds no key for: a
            // second machine on the account (an enrolled executor) that the relay also delivers
            // here. Nothing about this browser's keys is wrong, so this is that machine's state and
            // never the account's decrypt door. A pairing, a legacy binding or a sender pin keeps
            // `unknown_sender` and whatever follows it, key drift included.
            this._noteUnpairedMachine(routedMachine, envelope.sender);
            throw cloudFailure("machine_not_paired", "this browser holds no pairing for the envelope's machine",
                { detail: { machine: routedMachine } });
        }
        if (!key) throw cloudError("unknown_sender", "the envelope sender is not paired");
        var clear;
        try {
            probe.stage = "master_key_lookup";
            var master = pairing ? pairing.masterKey : await this._masterKey(envelope.key_id);
            clear = await openEnvelope(envelope, master, key, probe);
        } catch (error) {
            // Relay readiness proves the viewer's signing identity, not that the account content
            // key in this browser still matches the Mac. WebCrypto otherwise reports a bare
            // OperationError, which left the page claiming "live" over an empty Session list.
            // Preserve our own typed key-store failures; normalize signature/decryption failures
            // so the composition root can offer the existing same-device repair flow.
            if (error && (error.code === "unknown_key" || error.code === "extractable_key")) throw error;
            // The row keeps what this sentence replaces.
            probe.original = error;
            throw cloudError("unreadable_envelope",
                "this browser cannot decrypt the paired Mac's Session data");
        }
        // `sender` is the one clear envelope field excluded from the signature. Select the key
        // from the persisted machine binding, verify/decrypt the channel, and only then accept
        // that clear sender spelling as the owner of the authenticated route.
        probe.stage = "paired_sender";
        if (pairing && pairing.senderID !== envelope.sender) {
            throw cloudError("unknown_sender",
                "the authenticated machine channel does not match its paired sender");
        }
        // Pre-scoping browsers know the sender pin and account key but not which machine owns
        // them. The signed channel and successful decrypt are the only safe migration witness:
        // never persist a route from the clear sender/header before both checks have passed.
        if (routedMachine && !pairing) {
            probe.stage = "legacy_binding";
            var migrated;
            try {
                migrated = this.bindLegacyMachine
                    ? await this.bindLegacyMachine(routedMachine, envelope.sender, envelope.key_id)
                    : { machineID: routedMachine, senderID: envelope.sender, keyID: this.keyID,
                        masterKey: await this._masterKey(this.keyID), senderKey: key, legacy: true };
            } catch (error) {
                // A typed refusal (`machine_pairing_required`) says the binding is wrong. Anything
                // else is the key store failing to read or write it — a full quota — after the
                // signature and the decrypt above already proved this sender and content key for
                // the machine. Holding that proof in memory applies this envelope and the next ones
                // instead of dropping each of them on the same write; a later page binds again.
                if (!error || typeof error !== "object" || isFailureCode(error.code)) throw error;
                migrated = { machineID: routedMachine, senderID: envelope.sender, keyID: envelope.key_id,
                    masterKey: master, senderKey: key, legacy: true, unsaved: true };
                this._recordBindingUnsaved(error, envelope, routedMachine);
            }
            if (!migrated || migrated.machineID !== routedMachine ||
                migrated.senderID !== envelope.sender || !migrated.masterKey || !migrated.senderKey) {
                throw cloudError("machine_key_incomplete",
                    "the legacy machine pairing could not be persisted");
            }
            this.machinePairings.set(routedMachine, migrated);
            this.pairingSeen.add(routedMachine);
        }
        if (routedMachine) this.unpairedMachines.delete(routedMachine);
        this._sawAuthenticatedEnvelope(envelope, channel, routedMachine);
        probe.stage = "sequence";
        var previous = this.sequenceBySender.get(envelope.sender);
        if (realign) {
            var realignKey = envelope.sender + "\n" + envelope.ch;
            var channelPrevious = this.realignSequenceByChannel.get(realignKey);
            if (channelPrevious !== undefined && envelope.seq <= channelPrevious) {
                throw cloudError("replay", "the retained channel sequence did not advance");
            }
            this.realignSequenceByChannel.set(realignKey, envelope.seq);
        } else if (previous !== undefined && envelope.seq <= previous) {
            throw cloudError("replay", "the envelope sequence did not advance");
        }
        if (previous === undefined || envelope.seq > previous) {
            this.sequenceBySender.set(envelope.sender, envelope.seq);
        }
        probe.stage = "payload";
        var payload;
        try { payload = clear.length ? JSON.parse(textDecoder.decode(clear)) : null; }
        catch (e) { throw cloudError("bad_payload", "the decrypted stream payload is not JSON"); }
        probe.stage = "apply";
        this._applySnapshot(channel, payload, envelope, realign);
    }

    /**
     * An envelope this browser's pairing for its machine accepted — signature, decrypt, paired
     * sender and any legacy binding all passed: counted per sender, and an `orch/` one names its
     * machine as one delivery may go to. Never throws: the receive path does not wait on it.
     */
    _sawAuthenticatedEnvelope(envelope, channel, routedMachine) {
        try {
            var sender = envelope.sender;
            this.opensSinceReady.set(sender, (this.opensSinceReady.get(sender) || 0) + 1);
            if (channel.kind !== "orch" || !routedMachine) return;
            var seen = this.viewerVerified.get(routedMachine);
            this.viewerVerified.set(routedMachine, { sender: sender, at_ms: this.viewerEvents.now() });
            // A Mac to deliver to may only just have become known.
            if (!seen || seen.sender !== sender) this._scheduleViewerEvents();
        } catch (e) { /* observing a success must never become a failure */ }
    }

    /** One machine's "not paired in this browser" state, bounded to the last eight machines. */
    _noteUnpairedMachine(machine, sender, code) {
        var seen = this.unpairedMachines.get(machine);
        var now = this.viewerEvents.now();
        this.unpairedMachines.delete(machine);
        this.unpairedMachines.set(machine, { sender: typeof sender === "string" ? sender.slice(0, 128) : null,
            code: code || seen && seen.code || "machine_not_paired",
            since_ms: seen ? seen.since_ms : now, last_ms: now, envelopes: seen ? seen.envelopes + 1 : 1 });
        if (this.unpairedMachines.size > 8) {
            this.unpairedMachines.delete(this.unpairedMachines.keys().next().value);
        }
    }

    /** A legacy binding proven but not stored: one row, rate limited like the rest. Never throws. */
    _recordBindingUnsaved(error, envelope, machine) {
        try {
            var thrown = errorFields(error);
            this.viewerEvents.record("cloud.receive.binding_unsaved", {
                machine: machine, sender: envelope.sender, key_id: envelope.key_id,
                error_name: thrown.name, error_class: thrown.class, error_message: thrown.message,
                web_build: this.webBuild || null
            }, ["cloud.receive.binding_unsaved", machine, thrown.name].join("|"));
        } catch (e) { /* observing a failure must never become a second one */ }
    }

    /** The Mac rows go to if one can be named without refusing, and the sender that signs for it. */
    _targetMachineHint() {
        try { return this._viewerEventTarget(); } catch (e) { return null; }
    }

    /**
     * One `cloud.receive.failed` row: the code that is about to be thrown, the stage it was thrown
     * from and the exception underneath it, the envelope's metadata, and the context that tells a
     * machine this browser is not paired with (H1a) from a paired one whose pairing is incomplete
     * or under another key id (H1b), a realigned old snapshot (H2), a key store failing on resume
     * (H3) and an envelope that was never valid (H4). Never throws; never holds nonce, ct, sig or
     * any key — of a pairing only its kind, key id and sender id.
     */
    _recordReceiveFailure(error, envelope, realign, probe) {
        try {
            var now = this.viewerEvents.now();
            var thrown = errorFields(error);
            // The code is what was thrown; the name and words are the exception it replaced.
            var original = probe.original ? errorFields(probe.original) : thrown;
            var cause = probe.cause ? errorFields(probe.cause) : null;
            var meta = envelopeMetadata(envelope, realign);
            var target = this._targetMachineHint();
            var pairing = probe.pairing && typeof probe.pairing === "object" ? probe.pairing : null;
            var withPairing = [];
            var withoutPairing = [];
            this.machinePairings.forEach(function (_, machine) { withPairing.push(machine); });
            // A pairing this client holds — resolved, handed in, or bound from a legacy pin after
            // its lookup answered none — outranks that lookup's answer.
            this.pairingLookups.forEach(function (had, machine) {
                if (withPairing.indexOf(machine) >= 0) return;
                if (had) withPairing.push(machine);
                else if (withoutPairing.indexOf(machine) < 0) withoutPairing.push(machine);
            });
            var found = [];
            var missing = [];
            this.senderKeys.forEach(function (_, sender) { if (found.indexOf(sender) < 0) found.push(sender); });
            this.senderKeyLookups.forEach(function (had, sender) {
                if (had && found.indexOf(sender) < 0) found.push(sender);
                if (!had && missing.indexOf(sender) < 0) missing.push(sender);
            });
            var opensTotal = 0;
            this.opensSinceReady.forEach(function (count) { opensTotal += count; });
            var data = Object.assign(meta, {
                code: thrown.code,
                stage: probe.stage,
                error_name: original.name,
                error_class: original.class,
                error_message: original.message,
                cause_name: cause ? cause.name : null,
                cause_message: cause ? cause.message : null,
                browser_key_id: typeof this.keyID === "string" ? this.keyID.slice(0, 64) : null,
                socket_ready: this.ready,
                ms_since_ready: this.readyAt === null ? null : Math.max(0, now - this.readyAt),
                opens_since_ready_sender: meta.sender ? this.opensSinceReady.get(meta.sender) || 0 : 0,
                opens_since_ready_total: opensTotal,
                sender_key_found: probe.senderKeyFound,
                sender_key_source: probe.senderKeySource,
                sender_key_lookup_ms: probe.senderKeyLookupMs,
                senders_with_keys: found.slice(0, 8),
                senders_without_keys: missing.slice(0, 8),
                routed_machine: typeof probe.routedMachine === "string"
                    ? probe.routedMachine.slice(0, 128) : null,
                // `undefined` is "never looked up" (the channel did not parse, or names no machine).
                pairing_found: probe.pairing === undefined ? null : !!pairing,
                // What this viewer's previous lookup for the same machine answered: a store that
                // said yes and now says no is not a machine that was never paired.
                pairing_found_before: probe.pairingFoundBefore,
                pairing_legacy: pairing ? pairing.legacy === true : null,
                pairing_key_id: pairing && typeof pairing.keyID === "string" ? pairing.keyID.slice(0, 64) : null,
                pairing_sender: pairing && typeof pairing.senderID === "string"
                    ? pairing.senderID.slice(0, 128) : null,
                pairing_source: probe.pairingSource,
                pairing_lookup_ms: probe.pairingLookupMs,
                machines_with_pairing: withPairing.slice(0, 8),
                machines_without_pairing: withoutPairing.slice(0, 8),
                target_machine: target ? target.machine : null,
                target_sender: target ? target.sender : null,
                sender_is_target: target && target.sender && meta.sender
                    ? target.sender === meta.sender : null,
                web_build: this.webBuild || null
            }, pageContext(now));
            var outcome = this.viewerEvents.record("cloud.receive.failed", data, [
                "cloud.receive.failed", thrown.code, probe.stage, meta.channel_kind, meta.machine,
                meta.sender, meta.key_id, original.name, meta.realign
            ].join("|"));
            if (error && typeof error === "object") {
                Object.defineProperty(error, "viewerEvent", { configurable: true, enumerable: false,
                    value: outcome });
            }
        } catch (e) { /* observing a failure must never become a second one */ }
    }

    /** A frame that failed before or outside an envelope: one generic row, rate limited like the rest. */
    _recordFrameFailure(error, raw) {
        try {
            if (error && typeof error === "object" && error.viewerEvent) return;
            var thrown = errorFields(error);
            var type = typeof raw === "string"
                ? ((/"type"\s*:\s*"([a-z_]{1,32})"/.exec(raw.slice(0, 256)) || [])[1] || null) : "binary";
            var now = this.viewerEvents.now();
            this.viewerEvents.record("cloud.frame.failed", Object.assign({
                frame_type: type, code: thrown.code, layer: error && typeof error.layer === "string"
                    ? error.layer.slice(0, 32) : null,
                error_name: thrown.name, error_class: thrown.class, error_message: thrown.message,
                socket_ready: this.ready,
                ms_since_ready: this.readyAt === null ? null : Math.max(0, now - this.readyAt),
                web_build: this.webBuild || null
            }, pageContext(now)), ["cloud.frame.failed", type, thrown.code, thrown.name].join("|"));
        } catch (e) { /* observing a failure must never become a second one */ }
    }

    _applySnapshot(channel, payload, envelope, realign) {
        if (channel.kind === "session") {
            var identity = sessionIdentity({ machine: decodedChannelSegment(channel.machine),
                session: decodedChannelSegment(channel.session) });
            this._observeMachine(identity.machine, envelope.ts);
            var key = sessionIdentityKey(identity);
            if (identity.session === SESSION_INVENTORY_ID) {
                var inventory = payload && payload.inventory;
                var inventoryKeys = inventory && typeof inventory === "object" &&
                    !Array.isArray(inventory) ? Object.keys(inventory).sort() : [];
                var ids = inventory && inventory.sessions;
                if (inventoryKeys.join(",") !== "sessions,version" || inventory.version !== 1 ||
                    !Array.isArray(ids) || ids.length > SESSION_INVENTORY_LIMIT ||
                    ids.some(function (id, index) {
                        return typeof id !== "string" || !id || id === SESSION_INVENTORY_ID ||
                            ids.indexOf(id) !== index;
                    })) throw cloudError("bad_payload", "the session inventory is malformed");
                var earlierInventory = this.sessionInventoryByMachine.get(identity.machine);
                if (earlierInventory && envelope.seq < earlierInventory.sequence) return;
                var kept = new Set(ids.map(function (id) {
                    return sessionIdentityKey({ machine: identity.machine, session: id });
                }));
                this.sessionSnapshots.forEach(function (row, storedKey) {
                    var rowIdentity = row && row.identity;
                    if (rowIdentity && rowIdentity.machine === identity.machine &&
                        !kept.has(storedKey) &&
                        (this.sessionSequenceByKey.get(storedKey) || 0) <= envelope.seq) {
                        this.sessionSnapshots.delete(storedKey);
                    }
                }, this);
                this.sessionInventoryByMachine.set(identity.machine,
                    { sequence: envelope.seq, ids: kept });
                var inventorySessions = this._sessionResponse(envelope.ts);
                if (this.handlers && this.handlers.sessions) {
                    this.handlers.sessions(inventorySessions.sessions, inventorySessions.at,
                        inventorySessions.scan);
                }
                this._emit({ type: "sessions", data: inventorySessions, identity: identity,
                    envelope: envelope, realign: realign, authoritative: true });
                return;
            }
            var row = payload && Object.prototype.hasOwnProperty.call(payload, "session")
                ? payload.session : payload;
            var previousRowSequence = this.sessionSequenceByKey.get(key);
            if (previousRowSequence !== undefined && envelope.seq < previousRowSequence) return;
            var knownInventory = this.sessionInventoryByMachine.get(identity.machine);
            if (row === null || (payload && payload.deleted === true) ||
                (knownInventory && envelope.seq <= knownInventory.sequence &&
                    !knownInventory.ids.has(key))) {
                this.sessionSnapshots.delete(key);
                this.sessionSequenceByKey.set(key, envelope.seq);
            } else if (row && typeof row === "object" && !Array.isArray(row)) {
                this.sessionSnapshots.set(key, Object.assign({}, row, {
                    id: row.id || identity.session,
                    machine: identity.machine,
                    session: identity.session,
                    identity: identity
                }));
                this.sessionSequenceByKey.set(key, envelope.seq);
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
            var settles = readKey(transcriptIdentity, answer.read);
            // A machine refusing a word it does not implement cannot know whether this page waits
            // on `read:<request>` or `action:<request>`, so the Mac (`commandRefusalReply`) and the
            // Linux executor both answer `action:`. The request id is this page's random UUID and
            // the channel is that machine's own, so a refusal — never a body — may settle the
            // other spelling of the same request.
            if (answer.error && !this.readWaiters.has(settles)) {
                var sibling = siblingRequestName(transcriptIdentity.session, answer.read);
                if (sibling && this.readWaiters.has(readKey(transcriptIdentity, sibling))) {
                    settles = readKey(transcriptIdentity, sibling);
                }
            }
            if (answer.read === "transcript" && !answer.error) {
                this.transcriptSnapshots.set(transcriptKey, answer.body);
            }
            if (answer.read === "transcript" && answer.error && answer.error.code === "not_found") {
                // A retained Session channel can realign after this answer even though its
                // snapshot predates the process lookup that returned not_found. Remember the
                // answer's sender sequence as an identity-scoped deletion barrier so that replay
                // cannot resurrect the exact row. The inverse reorder is equally important: an
                // older retained answer cannot delete a Session row already seen at a later
                // sequence. The read itself still settles below in either case.
                var previousSessionSequence = this.sessionSequenceByKey.get(transcriptKey);
                if (previousSessionSequence === undefined || envelope.seq >= previousSessionSequence) {
                    this.sessionSnapshots.delete(transcriptKey);
                    this.sessionSequenceByKey.set(transcriptKey, envelope.seq);
                    var healed = this._sessionResponse(envelope.ts);
                    if (this.handlers && this.handlers.sessions) {
                        this.handlers.sessions(healed.sessions, healed.at, healed.scan);
                    }
                    this._emit({ type: "sessions", data: healed, identity: transcriptIdentity,
                        envelope: envelope, realign: realign, selfHealed: true });
                }
            }
            this._settleRead(settles, answer.body, answer.error);
            this._emit({ type: "read", read: answer.read, data: answer.body,
                error: answer.error, identity: transcriptIdentity, envelope: envelope,
                realign: realign });
            return;
        }
        if (channel.kind === "orch") {
            var machine = decodedChannelSegment(channel.machine);
            this._observeMachine(machine, envelope.ts);
            var buildBefore = this._macBuild(machine);
            this.orchestratorSnapshots.set(machine, payload || {});
            this._rememberDescriptor(machine, payload);
            // A new build may know a word the old one refused, so what it refused is asked again.
            if (this._macBuild(machine) !== buildBefore) this.machineLacks.delete(machine);
            this._consumeCloudStatus(machine, payload && payload.cloud_status);
            // A retained Session envelope may arrive before its machine descriptor. Re-project
            // the same authenticated rows when the descriptor arrives so the visible label does
            // not wait for unrelated terminal activity.
            if (this.sessionSnapshots.size && this.handlers && this.handlers.sessions) {
                var sessions = this._sessionResponse(envelope.ts);
                this.handlers.sessions(sessions.sessions, sessions.at, sessions.scan);
            }
            var tasks = this._allOrchestratorRows("tasks");
            if (this.handlers && this.handlers.tasks) this.handlers.tasks(tasks);
            this._sawAppStamp(payload);
            this._emit({ type: "orchestrator", data: payload, machine: machine,
                envelope: envelope, realign: realign });
            return;
        }
        this._emit({ type: channel.kind, data: payload, envelope: envelope, realign: realign });
    }

    _sessionResponse(timestamp) {
        var sessions = Array.from(this.sessionSnapshots.values()).map(function (row) {
            var snapshot = this.orchestratorSnapshots.get(row.machine) || {};
            var descriptor = snapshot.machine && typeof snapshot.machine === "object"
                ? snapshot.machine : this.machineDescriptors.get(row.machine)?.machine || null;
            return descriptor ? Object.assign({}, row, { machineInfo: descriptor }) : row;
        }, this);
        return { sessions: sessions,
            at: timestamp ? Math.floor(timestamp / 1000) : 0,
            scan: { emptyAuthoritative: true, cloud: true } };
    }

    sessions() { return Promise.resolve(this._sessionResponse(0)); }

    /**
     * Recover the fleet identity behind the shared UI's row key.
     *
     * The local client quite deliberately accepts a bare session id, and the shared view has
     * always handed that same `s.id` back for transcript, Info, composer and panels.  A Cloud
     * snapshot carries the missing Mac beside the row, so this transport must join the key back
     * to that snapshot before it publishes.  Letting the generic helper supply its local
     * fallback turns a press on `mac-01/session-01` into `ctl/this-mac`; the request is valid,
     * encrypted and addressed to a machine that does not exist on the relay, which is why the
     * visible symptom is an endless skeleton instead of an error.
     *
     * An explicit identity remains the protocol-level escape hatch used by tests and callers
     * that already have the pair.  A duplicate bare id is refused rather than guessed: two Macs
     * with the same terminal id are two sessions, not permission to pick whichever Map visits
     * first.
     */
    _sessionIdentity(value) {
        if (!value || (typeof value !== "object" && typeof value !== "string")) {
            throw cloudError("malformed_read", "this request names no session");
        }
        if (typeof value === "object") {
            try { return sessionIdentity(value); }
            catch (error) { throw cloudError("malformed_read", error.message); }
        }
        var found = [];
        this.sessionSnapshots.forEach(function (row) {
            if (!row || (row.id !== value && row.session !== value)) return;
            var identity = row.identity || { machine: row.machine, session: row.session };
            if (!identity || typeof identity.machine !== "string" ||
                typeof identity.session !== "string") return;
            if (!found.some(function (seen) {
                return seen.machine === identity.machine && seen.session === identity.session;
            })) found.push(identity);
        });
        if (found.length === 1) return sessionIdentity(found[0]);
        if (found.length > 1) {
            throw cloudError("cloud_session_ambiguous",
                "more than one Mac published this session id");
        }
        throw cloudError("not_found", "this session is not in the Cloud inventory");
    }

    _knownMachines() {
        var found = new Set(this.orchestratorSnapshots.keys());
        this.machineDescriptors.forEach(function (_, machine) { found.add(machine); });
        this.machineObservedAt.forEach(function (_, machine) { found.add(machine); });
        // A viewer may see a machine-scoped route before it can open that machine's descriptor.
        // Devices must keep that opaque route visible as "not paired" instead of making a healthy
        // executor look absent.
        this.unpairedMachines.forEach(function (_, machine) { found.add(machine); });
        this.sessionSnapshots.forEach(function (row) {
            if (row && typeof row.machine === "string" && row.machine) found.add(row.machine);
        });
        return Array.from(found).sort();
    }

    _observeMachine(machine, timestamp) {
        if (typeof machine !== "string" || !machine || !Number.isFinite(timestamp) || timestamp <= 0) {
            return;
        }
        var previous = this.machineObservedAt.get(machine);
        if (!Number.isFinite(previous) || timestamp > previous) {
            this.machineObservedAt.set(machine, timestamp);
        }
    }

    /** Display-only descriptors from authenticated encrypted snapshots. Command authority stays
     * the opaque id encoded in the envelope channel. */
    machines() {
        var retryAfterMs = this.descriptorStorage && Number.isFinite(this.readyAt)
            ? Math.max(0, this.readyAt + MACHINE_INVENTORY_SYNC_MS - this.viewerEvents.now()) : 0;
        var rows = this._machineRows();
        if (!rows.length && retryAfterMs <= 0) return Promise.reject(cloudError("cloud_read_unavailable",
            "no machine has published an inventory to this account yet"));
        return Promise.resolve({ machines: rows, syncing: retryAfterMs > 0,
            retryAfterMs: retryAfterMs });
    }

    /** The rows `machines()` answers, synchronously, so `voiceHost` reads the same facts Devices draws. */
    _machineRows() {
        var now = Date.now();
        var rows = this._knownMachines().map(function (id) {
            var snapshot = this.orchestratorSnapshots.get(id) || {};
            var remembered = this.machineDescriptors.get(id);
            var descriptor = snapshot.machine && typeof snapshot.machine === "object"
                ? snapshot.machine : remembered && remembered.machine || {};
            var presentation = machinePresentation({ id: id, machineName: descriptor.name,
                machinePlatform: descriptor.platform, cloudProvider: descriptor.provider }, T);
            var snapshotAt = Number.isFinite(snapshot.at) && snapshot.at > 0
                ? snapshot.at * 1000 : null;
            var envelopeAt = this.machineObservedAt.get(id);
            var observedAt = [snapshotAt, envelopeAt].filter(Number.isFinite).reduce(function (latest, at) {
                return latest === null || at > latest ? at : latest;
            }, null);
            var autoSelectable = observedAt !== null &&
                Math.abs(now - observedAt) <= MACHINE_INVENTORY_FRESH_MS;
            var pairing = this.unpairedMachines.has(id) ? "not_paired"
                : this.viewerVerified.has(id) || this.pairingSeen.has(id) ? "paired"
                    : this.pairingLookups.get(id) === false ? "not_paired" : "unknown";
            var sessions = 0;
            this.sessionSnapshots.forEach(function (session) {
                if (session && session.machine === id) sessions += 1;
            });
            return Object.freeze(Object.assign({}, presentation, { observedAt: observedAt,
                freshness: autoSelectable ? "current" : observedAt ? "stale" : "unknown",
                pairing: pairing, sessions: sessions,
                // Being named by an authenticated channel is enough to issue a manual bounded
                // places probe.  It is not enough to silently choose this route for the user.
                selectable: pairing !== "not_paired", autoSelectable: autoSelectable }));
        }, this);
        return rows.map(function (row) {
            var fleet = machinePresentationForFleet(row, rows, T);
            return Object.freeze(Object.assign({}, row, { label: fleet.label }));
        });
    }

    /**
     * Whether `machine` implements the browser command `type`: `"yes"`, `"no"` or `"unknown"`.
     *
     * **The one capability predicate.** Every route in this file that sends a command asks it —
     * the gate in `_read` and `_publishCommand`, the account pickers, the fan-out reads — so
     * "which machine can answer this" has a single answer (`docs/cloud.md`, *Which machine a
     * request goes to*). The evidence, strongest first:
     *
     * - the machine answered `unknown_command` to this word (`machineLacks`): no;
     * - its descriptor advertises `commands`: exactly those, and never required, because the
     *   executors already deployed do not send the list;
     * - its descriptor names a platform that is not a Mac: `linux` implements
     *   `LINUX_BROWSER_COMMANDS`, anything else nothing;
     * - it is evidently a Mac — a `macos`/`darwin` descriptor, or `cloud_status`, which only the Mac
     *   publishes: yes, except a status-gated command before that Mac has shown `cloud_status`;
     * - no descriptor yet: yes for a word every known platform implements (`UNIVERSAL_COMMANDS`),
     *   because whichever platform the machine turns out to be answers it; otherwise unknown.
     *   What unknown may do is the caller's rule. A request that names this machine still goes to
     *   it; a choice among machines takes it only when no machine is evidently a Mac and none is
     *   known to answer (`_machinesFor`).
     *
     * The descriptor stays display metadata, never routing authority: here it is evidence about
     * what a route can answer, and the route is still the authenticated channel.
     */
    _machineImplements(machine, type, options) {
        var lacks = !(options && options.learned === false) && this.machineLacks.get(machine);
        if (lacks && lacks.has(type)) return "no";
        var descriptor = this._descriptorFor(machine);
        if (Array.isArray(descriptor.commands)) return descriptor.commands.indexOf(type) >= 0 ? "yes" : "no";
        var platform = descriptorPlatform(descriptor);
        if (platform && platform !== "macos" && platform !== "darwin") {
            return platform === "linux" && LINUX_BROWSER_COMMANDS.indexOf(type) >= 0 ? "yes" : "no";
        }
        if (platform || this.macCapabilities.has(machine)) {
            var gated = !(options && options.statusGate === false) && STATUS_GATED_COMMANDS.indexOf(type) >= 0;
            return gated && !this.macCapabilities.has(machine) ? "no" : "yes";
        }
        return UNIVERSAL_COMMANDS.indexOf(type) >= 0 ? "yes" : "unknown";
    }

    /** A `macos`/`darwin` descriptor, or `cloud_status` from a machine whose descriptor names no other platform. */
    _evidentMac(machine) {
        var platform = descriptorPlatform(this._descriptorFor(machine));
        return platform === "macos" || platform === "darwin" || (!platform && this.macCapabilities.has(machine));
    }

    /** The descriptor the live `orch/` snapshot of `machine` carries, else the one remembered. */
    _descriptorFor(machine) {
        var snapshot = this.orchestratorSnapshots.get(machine);
        if (snapshot && snapshot.machine && typeof snapshot.machine === "object" && !Array.isArray(snapshot.machine)) {
            return snapshot.machine;
        }
        var remembered = this.machineDescriptors.get(machine);
        return remembered && remembered.machine || {};
    }

    /**
     * The refusal for sending `type` to a machine known not to implement it, or null.
     *
     * Asked before a request that names its machine leaves, so a machine that cannot answer is told
     * nothing and the page is told now rather than after the read timeout. Unknown is not refused
     * here: the request named that machine and no other can serve it. Status gating stays with the
     * callers that already require `cloud_status` (`diagnosticsReport`, `_viewerEventTarget`),
     * because a remembered descriptor can arrive before this page has seen the Mac's digest.
     */
    _unsupportedRefusal(machine, type) {
        if (typeof machine !== "string" || !machine) return null;
        if (this._machineImplements(machine, type, { statusGate: false }) !== "no") return null;
        return this._evidentMac(machine)
            ? cloudError("cloud_feature_unavailable", "this Mac answered that it does not know " + type)
            : cloudError("cloud_machine_unsupported", "this machine does not implement " + type);
    }

    /**
     * Who a request with no machine may go to for `type`, as `_machineRows` rows.
     *
     * `capable` is every machine this browser is not known to be unpaired with that implements
     * `type`; when there is none and no machine is evidently a Mac, it is the machines whose
     * descriptor has not arrived, which is how a one-Mac account works before its first snapshot.
     * `unconfirmed` is the unknown ones left out because such a machine exists — unknown never
     * authorises sending to a machine that may not answer while one that can does. `unpaired`
     * cannot be sent to from this browser at all.
     */
    _machinesFor(type) {
        var rows = this._machineRows();
        var found = { rows: rows, capable: [], unconfirmed: [], unpaired: [], evidentMac: false };
        var unknown = [];
        rows.forEach(function (row) {
            if (row.pairing === "not_paired") { found.unpaired.push(row); return; }
            if (this._evidentMac(row.id)) found.evidentMac = true;
            var answer = this._machineImplements(row.id, type);
            if (answer === "yes") found.capable.push(row);
            else if (answer === "unknown") unknown.push(row);
        }, this);
        if (!found.capable.length && !found.evidentMac) found.capable = unknown;
        else found.unconfirmed = unknown;
        return found;
    }

    /**
     * The one machine an account-level request for `type` goes to: `{ machine, chosen, candidates,
     * error }`, where a refusal still carries `candidates`.
     *
     * The rules `voiceHost` was written with, for every feature: a choice this browser made
     * (`options.choice`) wins while it is still a candidate, however stale — the person picked it;
     * otherwise one candidate is the answer, then — unless `options.strict` — the one candidate with
     * a current inventory. None is `cloud_read_unavailable` before any machine is known,
     * `machine_pairing_required` when this browser is paired with none of them,
     * `cloud_feature_unavailable` when a Mac is there and has not got the feature, and
     * `cloud_machine_unsupported` when no machine on the account implements it. Two left is
     * `cloud_machine_ambiguous`. `options.unavailable` and `options.ambiguous` rename the last three
     * for a feature with words of its own.
     */
    _resolveMachine(type, options) {
        options = options || {};
        var found = this._machinesFor(type);
        var feature = options.feature || type;
        var candidates = Object.freeze(found.capable.map(function (row) { return row.id; }));
        var answer = function (machine, chosen) {
            return { machine: machine, chosen: chosen, candidates: candidates, error: null };
        };
        var refusal = function (code, message) {
            var error = cloudError(code, message);
            error.candidates = candidates;
            return { machine: null, chosen: false, candidates: candidates, error: error };
        };
        var choice = options.choice || null;
        if (choice && candidates.indexOf(choice) >= 0) return answer(choice, true);
        if (candidates.length === 1) return answer(candidates[0], false);
        if (!found.rows.length) {
            return refusal("cloud_read_unavailable", "no Mac has published an inventory to this account yet");
        }
        if (!candidates.length) {
            var paired = found.rows.length > found.unpaired.length;
            return refusal(options.unavailable || (!paired ? "machine_pairing_required"
                : found.evidentMac ? "cloud_feature_unavailable" : "cloud_machine_unsupported"),
            "no machine on this account that this browser is paired with can answer " + feature);
        }
        if (!options.strict) {
            var current = found.capable.filter(function (row) { return row.freshness === "current"; });
            if (current.length === 1) return answer(current[0].id, false);
        }
        return refusal(options.ambiguous || "cloud_machine_ambiguous",
            feature + " needs one machine, and more than one on this account can answer it");
    }

    /** `_resolveMachine`'s machine, or its refusal thrown; every public caller turns that into a rejection. */
    _accountMachine(type, options) {
        var resolved = this._resolveMachine(type, options);
        if (!resolved.machine) throw resolved.error;
        return resolved.machine;
    }

    /**
     * Ask each of `machines` on its own and settle with every outcome, never rejecting. One
     * machine's silence, refusal or failure is its own row: it never discards another machine's
     * answer, and because each ask is bounded by its own read timeout it never delays the others
     * past that bound.
     */
    _askEach(machines, ask) {
        return Promise.all(machines.map(function (machine) {
            return Promise.resolve().then(function () { return ask(machine); }).then(function (value) {
                return { machine: machine, value: value, error: null };
            }, function (error) {
                return { machine: machine, value: null, error: asCloudFailure(error) };
            });
        }));
    }

    /**
     * A fan-out read's outcomes, as a page can say them: the answers; `unanswered`, each machine
     * that could have answered and did not, with its typed failure; and `unconfirmed`, the
     * machines not asked because their descriptor has not arrived. A machine that cannot have the
     * feature — known in advance, or answering `unknown_command` — contributes nothing at all.
     */
    _fanOutReport(settled, found) {
        var labels = new Map(found.rows.map(function (row) { return [row.id, row.label]; }));
        var report = { answered: [], unanswered: [], unconfirmed: found.unconfirmed.map(function (row) { return row.id; }) };
        settled.forEach(function (row) {
            if (!row.error) report.answered.push(row);
            else if (UNSUPPORTED_CODES.indexOf(row.error.code) < 0) {
                report.unanswered.push({ machine: row.machine, label: labels.get(row.machine) || row.machine, error: row.error });
            }
        });
        found.unpaired.forEach(function (row) {
            report.unanswered.push({ machine: row.id, label: row.label || row.id, error: cloudError("machine_pairing_required",
                "this browser is not paired with this machine") });
        });
        return report;
    }

    /**
     * Which machine transcribes this browser's dictation. `voice()` sends to it and the Devices
     * page marks it, from this one decision, so the card that says "Voice input" is the machine
     * the recording goes to.
     *
     * Only a Mac runs Whisper: a Linux executor has no `voice` handler, so a recording sent there
     * would wait out `VOICE_TIMEOUT_MS` for nothing. This is `_resolveMachine` for `voice` with the
     * browser's own choice and the two refusals Devices has sentences for. It answers
     * `{ machine, chosen, candidates }`; two left, or none, is a typed refusal that still carries
     * `candidates`, because an ambiguous fleet is exactly when Devices has to offer the choice.
     */
    voiceHost() {
        try {
            var host = this._voiceHost();
            if (!host.machine) throw host.error;
            return Promise.resolve({ machine: host.machine, chosen: host.chosen,
                candidates: host.candidates });
        } catch (error) { return Promise.reject(error); }
    }

    _voiceHost() {
        return this._resolveMachine("voice", { choice: this._voiceHostChoice(), feature: "voice input",
            unavailable: "cloud_voice_host_unavailable", ambiguous: "cloud_voice_host_ambiguous" });
    }

    /** Stores the voice host for this browser. Only a current candidate can be chosen. */
    setVoiceHost(machine) {
        try {
            var id = typeof machine === "string" ? machine : "";
            if (!id || this._voiceHost().candidates.indexOf(id) < 0) {
                throw cloudError("cloud_voice_host_unavailable",
                    "this machine cannot transcribe voice input for this browser");
            }
            this.voiceHostChoice = id;
            var key = this._voiceHostStorageKey();
            if (key && this.voiceHostStorage && typeof this.voiceHostStorage.setItem === "function") {
                try { this.voiceHostStorage.setItem(key, id); }
                catch (e) { /* private mode or quota: the choice holds for this page */ }
            }
            return this.voiceHost();
        } catch (error) { return Promise.reject(error); }
    }

    _voiceHostStorageKey() {
        return this.account ? VOICE_HOST_CHOICE + encodeURIComponent(this.account) : null;
    }

    /** A press on this page first, even one storage refused to keep; then what this browser stored. */
    _voiceHostChoice() {
        if (this.voiceHostChoice) return this.voiceHostChoice;
        var key = this._voiceHostStorageKey();
        if (!key || !this.voiceHostStorage || typeof this.voiceHostStorage.getItem !== "function") return null;
        try {
            var stored = this.voiceHostStorage.getItem(key);
            return typeof stored === "string" && stored ? stored.slice(0, 256) : null;
        } catch (e) { return null; /* unreadable storage: the automatic choice */ }
    }

    _scheduleMachine(value) {
        var id = String(value || "");
        var found = [];
        this.orchestratorSnapshots.forEach(function (snapshot, machine) {
            var schedules = snapshot && Array.isArray(snapshot.schedules) ? snapshot.schedules : [];
            if (schedules.some(function (row) { return row && row.id === id; })) found.push(machine);
        });
        if (found.length === 1) return found[0];
        if (found.length > 1) {
            throw cloudError("cloud_schedule_ambiguous",
                "more than one Mac published this schedule id");
        }
        throw cloudError("not_found", "this schedule is not in the Cloud inventory");
    }

    _machineRequest(machine, type, extra, kind, timeoutMs, readOptions) {
        return this._machineRequestAs(requestID(), machine, type, extra, kind, timeoutMs, readOptions);
    }

    /** `_machineRequest` under a request id the caller keeps, so a resend is the same request. */
    _machineRequestAs(request, machine, type, extra, kind, timeoutMs, readOptions) {
        if (typeof machine !== "string" || !machine) {
            return Promise.reject(cloudError("cloud_read_unavailable",
                "no Mac has published an inventory to this account yet"));
        }
        return this._read({ machine: machine, session: MACHINE_REPLY_SESSION }, type,
            Object.assign({ request: request }, extra || {}), (kind || "read") + ":" + request,
            timeoutMs, readOptions);
    }

    /**
     * The Mac's Cloud status snapshot (§4.1, §11.3), asked of one Mac or of every Mac this
     * account has published. The fleet form never rejects for one Mac: each row carries its own
     * `status` or its own typed `error`, because the sheet that asks has to show both.
     */
    cloudStatus(machine) {
        if (machine !== undefined && machine !== null) {
            if (typeof machine !== "string" || !machine) {
                return Promise.reject(cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory"));
            }
            return this._readCloudStatus(machine, undefined)
                .then(function (status) { return { machine: machine, status: status }; });
        }
        var machines = this._knownMachines();
        if (!machines.length) {
            return Promise.reject(cloudError("cloud_read_unavailable",
                "no Mac has published an inventory to this account yet"));
        }
        var self = this;
        return Promise.all(machines.map(function (name) {
            // A Mac that has never published `cloud_status` does not know the read either, and an
            // older one answers an unknown command with silence: asking it would hold the sheet on
            // "reading" for a minute to learn what the missing digest already said. A Linux
            // executor publishes no digest and has no status read at all.
            if (self._machineImplements(name, "cloud.status", { learned: false }) !== "yes") {
                return { machine: name, status: null, error: null, capable: false };
            }
            return self._readCloudStatus(name, undefined).then(function (status) {
                return { machine: name, status: status, error: null,
                    capable: self.macCapabilities.has(name) };
            }, function (error) {
                return { machine: name, status: null, error: asCloudFailure(error),
                    capable: self.macCapabilities.has(name) };
            });
        })).then(function (rows) { return { machines: rows }; });
    }

    _readCloudStatus(machine, timeoutMs) {
        return this._machineRequest(machine, "cloud.status", {}, "read", timeoutMs, { probe: false })
            .then(function (body) {
                if (!body || typeof body !== "object" || Array.isArray(body)) {
                    throw cloudError("bad_payload", "the Cloud status answer is not an object");
                }
                return body;
            });
    }

    /**
     * The diagnostics report, sent to the Mac as a Cloud command (B8, §11.5) — the hosted
     * console has no `/v1/diagnostics/report` of its own, so the old `fetch` reached the page's
     * own origin and nothing else.
     *
     * The size is checked here first, against the smaller of the relay's `ctl` ceiling and
     * `DiagnosticReport.maxBytes`: a report over it is refused as `browser · report_too_large`
     * without spending a sequence, rather than being dropped at one end and timing out at the
     * other. The browser's recent-command trail rides along — sequences, steps and codes only.
     */
    diagnosticsReport(report, machine) {
        try {
            if (!report || typeof report !== "object" || Array.isArray(report)) {
                throw cloudError("report_not_json", "a diagnostics report must be a JSON object");
            }
            var body = Object.assign({}, report, { cloud_trail: this.trail.snapshot() });
            var bytes = textEncoder.encode(JSON.stringify(body)).length;
            var limit = Math.min(DIAGNOSTIC_REPORT_MAX_BYTES, RELAY_CTL_MAX_BYTES);
            if (bytes > limit) {
                throw cloudFailure("report_too_large", "the report is " + bytes +
                    " bytes and the limit is " + limit, { detail: {} });
            }
            if (machine !== undefined && (!machine || !this._knownMachines().includes(machine))) {
                throw cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory");
            }
            var target = machine || this._accountMachine("diagnostics.report", { feature: "diagnostics" });
            if (!this.macCapabilities.has(target)) {
                throw cloudError("cloud_feature_unavailable",
                    "this Mac build does not support Cloud diagnostics reports");
            }
            return this._machineRequest(target,
                "diagnostics.report", { report: body }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    /* ---- viewer events: automatic delivery to the paired Mac ------------------------------- */

    /**
     * The Mac these rows belong to, named explicitly rather than by an account picker: an account
     * with a second machine — an enrolled Linux executor — is exactly the case being diagnosed,
     * and "more than one machine" must not become "nowhere to report it".
     *
     * A candidate is a machine this browser is paired with, is a Mac, and has said it takes the
     * command:
     *
     * - **paired**: its `orch/<machine>` snapshot was accepted by `_receiveEnvelope`, which happens
     *   only through this browser's pairing for that machine — an exact one from the store, or a
     *   legacy pin bound to it after the signature and decrypt proved it. A machine this browser
     *   is not paired with never opens and so is never here. The send itself is sealed by
     *   `_outboundMachinePairing`, so a pairing gone since is refused there, before the wire;
     * - **a Mac**: its descriptor does not name another platform (a Linux executor says `linux`);
     * - **capable**: it published `cloud_status.v >= 1`, which only the Mac does. A Linux executor
     *   publishes none and does not implement `diagnostics.events` — it drops the command without
     *   a reply — so it is excluded by two independent facts, not one.
     *
     * One candidate is the answer. With none, the Mac chosen on an earlier page is used unless
     * this page has since authenticated that machine and found it no longer qualifies, so a page
     * that cannot open any envelope yet can still report that it cannot. Two Macs are refused as
     * ambiguous; a Mac that has not shown the capability is `cloud_feature_unavailable`.
     */
    _viewerEventTarget(remember) {
        var candidates = [];
        var incapable = false;
        this.viewerVerified.forEach(function (seen, machine) {
            // The one capability predicate (`_machineImplements`): a machine whose descriptor names
            // another platform is out, and a machine that may be a Mac but has not shown
            // `cloud_status` cannot take the command yet. Not what it refused before: a refused batch
            // is held by the log's own durable block for that Mac and build (`blockedFor` below),
            // which outlives this client and is what the delivery outcome reports.
            var answer = this._machineImplements(machine, VIEWER_EVENTS_COMMAND, { learned: false });
            if (answer === "yes") candidates.push({ machine: machine, sender: seen.sender, capable: true });
            else if (answer === "unknown" || this._evidentMac(machine)) incapable = true;
        }, this);
        if (candidates.length > 1) {
            throw cloudError("cloud_machine_ambiguous",
                "viewer events need one paired Mac, and this browser has authenticated more than one");
        }
        if (candidates.length === 1) {
            if (remember) this.viewerEvents.rememberTarget(candidates[0]);
            return candidates[0];
        }
        var remembered = this.viewerEvents.target();
        if (remembered && remembered.machine && !this.viewerVerified.has(remembered.machine)) {
            return { machine: remembered.machine, sender: remembered.sender,
                capable: remembered.capable === true, remembered: true };
        }
        if (incapable) {
            throw cloudError("cloud_feature_unavailable",
                "no paired machine that may be a Mac has published cloud_status, so none takes viewer events");
        }
        throw cloudError("cloud_machine_unavailable",
            "no paired Mac has published an authenticated snapshot to this browser yet");
    }

    _macBuild(machine) {
        var snapshot = this.orchestratorSnapshots.get(machine);
        var app = snapshot && snapshot.app;
        if (!snapshot) {
            var known = this.machineDescriptors.get(machine);
            return known ? known.build : null;
        }
        return app && typeof app.build === "string" ? app.build.slice(0, 64)
            : app && Number.isFinite(app.build) ? String(app.build) : null;
    }

    /** Kept from an authenticated `orch/` snapshot: the scalar descriptor fields and the app build. */
    _rememberDescriptor(machine, payload) {
        try {
            var descriptor = payload && payload.machine && typeof payload.machine === "object" ? payload.machine : {};
            var kept = {};
            Object.keys(descriptor).slice(0, 16).forEach(function (field) {
                var value = descriptor[field];
                if (typeof value === "string") kept[field] = value.slice(0, 128);
                else if (typeof value === "number" || typeof value === "boolean") kept[field] = value;
            });
            var commands = descriptorCommands(descriptor.commands);
            if (commands) kept.commands = commands;
            var app = payload && payload.app;
            var build = app && typeof app.build === "string" ? app.build.slice(0, 64)
                : app && Number.isFinite(app.build) ? String(app.build) : null;
            var previous = this.machineDescriptors.get(machine);
            if (!Object.keys(kept).length && previous && previous.machine) kept = previous.machine;
            if (!build && previous) build = previous.build;
            if (!Object.keys(kept).length && !build) return;
            this.machineDescriptors.delete(machine);
            this.machineDescriptors.set(machine, { machine: kept, build: build, at_ms: this.viewerEvents.now() });
            if (this.machineDescriptors.size > 16) {
                this.machineDescriptors.delete(this.machineDescriptors.keys().next().value);
            }
            this._persistMachineDescriptors();
        } catch (e) { /* remembering a descriptor must never refuse the snapshot */ }
    }

    _descriptorStorageKey() {
        return this.account ? MACHINE_DESCRIPTOR_CACHE + encodeURIComponent(this.account) : null;
    }

    _loadMachineDescriptors() {
        var out = new Map();
        var key = this._descriptorStorageKey();
        if (!key || !this.descriptorStorage || typeof this.descriptorStorage.getItem !== "function") return out;
        try {
            var parsed = JSON.parse(this.descriptorStorage.getItem(key) || "null");
            var rows = parsed && parsed.v === 1 && Array.isArray(parsed.machines) ? parsed.machines : [];
            rows.slice(-16).forEach(function (row) {
                if (!row || typeof row.id !== "string" || !row.id || !row.machine ||
                    typeof row.machine !== "object" || Array.isArray(row.machine)) return;
                var kept = {};
                Object.keys(row.machine).slice(0, 16).forEach(function (field) {
                    var value = row.machine[field];
                    if (typeof value === "string") kept[field] = value.slice(0, 128);
                    else if (typeof value === "number" || typeof value === "boolean") kept[field] = value;
                });
                var commands = descriptorCommands(row.machine.commands);
                if (commands) kept.commands = commands;
                if (!Object.keys(kept).length) return;
                var build = typeof row.build === "string" ? row.build.slice(0, 64)
                    : Number.isFinite(row.build) ? String(row.build) : null;
                out.set(row.id, { machine: kept, build: build,
                    at_ms: Number.isFinite(row.at_ms) ? row.at_ms : 0 });
            });
        } catch (e) { /* a display cache is optional and never blocks Cloud */ }
        return out;
    }

    _persistMachineDescriptors() {
        var key = this._descriptorStorageKey();
        if (!key || !this.descriptorStorage || typeof this.descriptorStorage.setItem !== "function") return;
        try {
            var rows = Array.from(this.machineDescriptors.entries()).slice(-16).map(function (entry) {
                return { id: entry[0], machine: entry[1].machine, build: entry[1].build,
                    at_ms: entry[1].at_ms };
            });
            this.descriptorStorage.setItem(key, JSON.stringify({ v: 1, machines: rows }));
        } catch (e) { /* quota/private mode may refuse optional display metadata */ }
    }

    /**
     * A refusal sending again cannot change: the Mac answered and said no (a 4xx other than
     * timeout and rate), or this device may not publish at all. Anything else — offline, a
     * renewal, a read timeout, a 5xx — is tried again later under the backoff.
     */
    _viewerEventsRefusalIsFinal(failure) {
        if (failure.code === "cloud_read_needs_send_prompt" || failure.code === "cloud_read_only") return true;
        if (failure.layer === "browser" || failure.layer === "relay") return false;
        return Number.isInteger(failure.status) && failure.status >= 400 && failure.status < 500 &&
            failure.status !== 408 && failure.status !== 429;
    }

    _armViewerEvents() {
        if (!this.viewerEventDelivery) return;
        if (!this.viewerEventsOff) {
            var self = this;
            this.viewerEventsOff = this.viewerEvents.onRecord(function () { self._scheduleViewerEvents(); });
        }
        this._scheduleViewerEvents();
    }

    _disarmViewerEvents() {
        if (this.viewerEventsOff) { this.viewerEventsOff(); this.viewerEventsOff = null; }
        if (this.viewerEventTimer !== null) {
            try { this.viewerEventTimers.clearTimeout(this.viewerEventTimer); } catch (e) { }
            this.viewerEventTimer = null;
        }
    }

    /**
     * One timer at a time: after a row, the burst's debounce; never before the log's spacing,
     * backoff and daily budget allow. A delivery that could not reach a Mac waits for the next
     * row, the next `ready`, or a Mac becoming known — it does not poll.
     */
    _scheduleViewerEvents() {
        if (!this.viewerEventDelivery || !this.ready || this.viewerEventTimer !== null) return;
        var log = this.viewerEvents;
        try {
            if (!log.pending()) return;
            var wait = Math.max(log.limits.debounceMs, log.nextSendAt() - log.now());
            var self = this;
            this.viewerEventTimer = this.viewerEventTimers.setTimeout(function () {
                self.viewerEventTimer = null;
                self._deliverViewerEvents().then(function (outcome) {
                    self.lastViewerDelivery = outcome;
                    // With Web Locks another tab's log says nothing until this one is its writer, and
                    // becoming it notifies; without them the lease is polled.
                    var again = outcome.state === "delivered" || outcome.state === "failed" ||
                        outcome.state === "refused" || (outcome.state === "deferred" &&
                            (outcome.why === "spacing" || (outcome.why === "other_tab" && !log.locks)));
                    if (again) self._scheduleViewerEvents();
                });
            }, wait);
        } catch (e) { this.viewerEventTimer = null; }
    }

    /**
     * Send the waiting batch to the paired Mac as `diagnostics.events` and remove it only on a
     * receipt that names it. Resolves with what happened — `delivered`, `empty`, `deferred`,
     * `blocked` or `failed` — and never rejects: the rows are the record, and they stay.
     *
     * Accepted, delivered and acknowledged stay three facts: the relay's `ack` is the trail's
     * `relayed` step; the Mac's receipt, written after it appended the line, is `observed`; and
     * `acknowledge` removing the outbox is the last thing, not the first.
     */
    _deliverViewerEvents() {
        var self = this;
        var log = this.viewerEvents;
        return Promise.resolve().then(function () {
            if (!log.pending()) return { state: "empty" };
            if (!self.ready) return { state: "deferred", why: "not_ready" };
            var target;
            try { target = self._viewerEventTarget(true); }
            catch (failure) { return { state: "deferred", why: failure.code, failure: failure }; }
            if (!target.capable) {
                return { state: "deferred", why: "cloud_feature_unavailable", machine: target.machine };
            }
            var macBuild = self._macBuild(target.machine);
            var blocked = log.blockedFor(target.machine, macBuild, self.webBuild || null);
            if (blocked) return { state: "blocked", code: blocked.code, machine: target.machine };
            var at = log.nextSendAt();
            if (at > log.now()) return { state: "deferred", why: "spacing", at: at };
            if (!log.claimLease(self.tabID)) return { state: "deferred", why: "other_tab" };
            var outbox = log.takeBatch({ device: self.deviceID, tab: self.tabID, webBuild: self.webBuild });
            if (!outbox) return { state: "empty" };
            var batch = JSON.parse(outbox.body);
            log.noteAttempt();
            return self._machineRequestAs(outbox.request, target.machine, VIEWER_EVENTS_COMMAND,
                { batch: batch }, "action", undefined, { probe: false }).then(function (receipt) {
                if (!receipt || receipt.batch_id !== outbox.batch_id || receipt.rows !== outbox.rows) {
                    var wrong = cloudError("bad_payload", "the Mac's receipt does not name this batch");
                    log.noteFailure(wrong);
                    return { state: "failed", failure: wrong, machine: target.machine };
                }
                log.acknowledge(outbox.batch_id, receipt);
                return { state: "delivered", receipt: receipt, machine: target.machine };
            }, function (error) {
                var failure = asCloudFailure(error);
                log.noteFailure(failure);
                if (self._viewerEventsRefusalIsFinal(failure)) {
                    // A refusal of the batch's own bytes drops it, counted, so the rows behind it go
                    // next; a refusal of this Mac or device holds it (`refusalDisposition`).
                    var disposition = log.block({ machine: target.machine, code: failure.code,
                        layer: failure.layer, status: failure.status, message: failure.message,
                        macBuild: macBuild, webBuild: self.webBuild || null });
                    return { state: disposition === "drop" ? "refused" : "blocked", code: failure.code,
                        failure: failure, machine: target.machine };
                }
                return { state: "failed", failure: failure, machine: target.machine };
            });
        }).catch(function (error) {
            return { state: "failed", failure: asCloudFailure(error) };
        });
    }

    /**
     * Projects and the start sheet are account views, so each machine answers its own inventory.
     *
     * **One machine never takes the others' Projects with it.** Every machine that implements
     * `places` is asked on its own (`_askEach`), so one that refuses, fails or stays silent until its
     * read timeout becomes a row of `unanswered` beside the other machines' Projects instead of the
     * whole answer. It rejects only when every machine asked failed, with the first failure. A
     * machine whose descriptor has not arrived is asked too: every platform implements `places`
     * (`UNIVERSAL_COMMANDS`), so a Mac known so far only from its Session rows is not left out.
     *
     * **An answer can be partial, and says so.** `unanswered` names each machine that could have
     * answered and did not; a caller that shows or keeps the list must not treat it as complete —
     * say so, do not cache it, and do not read a project's absence from it.
     *
     * **The route table is replaced only by answers.** It used to be cleared before the first Mac
     * was asked, and it stayed empty until every Mac had answered — for ever, if one of them failed
     * or timed out. Meanwhile the pages that called this keep the list they already have on screen,
     * so a press on one of its rows found no route. Now a machine's routes are replaced when that
     * machine answers, and kept when it could have answered and did not.
     *
     * A retained route can name a project that has since gone from its Mac. That is not a
     * guess this page makes: the route only says which Mac to ask, and that Mac answers a place
     * it no longer has with `not_found`, which the start sheet already answers by reading the
     * list again. What the table no longer does is refuse, on the Mac's behalf, a project
     * nobody has said is gone.
     */
    places(selectedMachine) {
        var known = this._knownMachines();
        var self = this;
        if (selectedMachine !== undefined && selectedMachine !== null) {
            var named = String(selectedMachine);
            if (!selectedMachine || !known.includes(named)) {
                return Promise.reject(cloudError("cloud_machine_unavailable",
                    "this machine has not published a current Cloud inventory"));
            }
            return this._placesFrom(named).then(function (answer) {
                return self._placesAnswer([answer], function (machine) { return machine !== named; });
            });
        }
        if (!known.length) {
            return Promise.reject(cloudError("cloud_read_unavailable",
                "no Mac has published an inventory to this account yet"));
        }
        var found = this._machinesFor("places");
        return this._askEach(found.capable.map(function (row) { return row.id; }), function (machine) {
            return self._placesFrom(machine);
        }).then(function (settled) {
            var report = self._fanOutReport(settled, found);
            if (!report.answered.length && report.unanswered.length) throw report.unanswered[0].error;
            var silent = new Set(report.unanswered.map(function (row) { return row.machine; })
                .concat(report.unconfirmed));
            var answer = self._placesAnswer(report.answered.map(function (row) { return row.value; }),
                function (machine) { return silent.has(machine); });
            return Object.assign(answer, { unanswered: report.unanswered, unconfirmed: report.unconfirmed });
        });
    }

    _placesFrom(machine) {
        return this._machineRequest(machine, "places", {}, "read").then(function (answer) {
            var places = answer && Array.isArray(answer.places) ? answer.places : [];
            var assistants = answer && Array.isArray(answer.assistants) ? answer.assistants : [];
            return { machine: machine, places: places, assistants: assistants };
        });
    }

    /** The joined list. The route table becomes these answers' routes plus the old routes of every
     *  machine `keep` names — the ones that could have answered and did not. */
    _placesAnswer(answers, keep) {
        var places = [];
        var assistants = [];
        var assistantIDs = new Set();
        var routes = new Map();
        answers.forEach(function (answer) {
            answer.places.forEach(function (place) {
                if (!place || typeof place.id !== "string" || !place.id) return;
                var id = cloudPlaceID(answer.machine, place.id);
                routes.set(id, { machine: answer.machine, id: place.id,
                    path: place.path || "" });
                places.push(Object.assign({}, place, { id: id, machine: answer.machine }));
            });
            answer.assistants.forEach(function (assistant) {
                var id = assistant && assistant.id;
                if (typeof id !== "string" || !id || assistantIDs.has(id)) return;
                assistantIDs.add(id);
                assistants.push(assistant);
            });
        });
        this.placeRoutes.forEach(function (route, id) {
            if (keep(route.machine) && !routes.has(id)) routes.set(id, route);
        });
        this.placeRoutes = routes;
        return { places: places, assistants: assistants };
    }

    /** Throws; every public caller turns that into a rejection. */
    _place(value) {
        var id = value && typeof value === "object" ? value.id : value;
        var route = this.placeRoutes.get(String(id || ""));
        if (!route) throw cloudError("not_found", "this Project has not been read from a Mac");
        return route;
    }

    projectWorktrees(project) {
        try {
            var route = this._place(project);
            return this._machineRequest(route.machine, "project-worktrees",
                { project: (project && typeof project === "object" && project.path) || route.path },
                "read");
        } catch (error) { return Promise.reject(error); }
    }

    /** The Project worktree lifecycle read model from one Mac, named by its Board Project id.
     * The service knows nothing about Cloud identity, so this client attaches the authenticated
     * route machine to the answer: a row is located by (machine, owner.sessionId), never a title.
     * Refresh is command-classified because it runs processes and mutates the Mac cache; Cloud
     * therefore requires the Mac's remote-write authority. No cleanup method exists here. */
    projectWorktreeLifecycle(project, machine) {
        return this._worktreeLifecycle("project-worktree-lifecycle", project, machine);
    }

    projectWorktreeLifecycleRefresh(project, machine) {
        return this._worktreeLifecycle("project-worktree-lifecycle-refresh", project, machine);
    }

    _worktreeLifecycle(type, project, machine) {
        project = String(project || "");
        if (!project || project.length > 200) {
            return Promise.reject(cloudError("malformed_read",
                "a Project worktree read needs a bounded Project id"));
        }
        try {
            if (machine !== undefined && (!machine || !this._knownMachines().includes(machine))) {
                throw cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory");
            }
            var route = machine || this._accountMachine(type, { feature: "Project worktrees" });
            var timeout = type === "project-worktree-lifecycle-refresh" ? 130000 : undefined;
            return this._machineRequest(route, type, { project: project }, "read", timeout)
                .then(function (answer) { return Object.assign({}, answer, { machine: route }); });
        } catch (error) { return Promise.reject(error); }
    }

    board(project, item, report, machine) {
        try {
            if (report) item = boardReportSelection(item, report);
            if (machine !== undefined && (!machine || !this._knownMachines().includes(machine))) {
                throw cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory");
            }
            var route = machine || this._accountMachine("board", { feature: "Project Board" });
            // The answer names the machine it came from, so a Project read here without a machine —
            // the Projects page, Settings — can route what it opens next to that same machine.
            return Promise.resolve(this._machineRequest(route, "board", { project: project || "", item: item || "" }, "read"))
                .then(function (answer) {
                    return answer && typeof answer === "object" && !Array.isArray(answer)
                        ? Object.assign({}, answer, { machine: route }) : answer;
                });
        } catch (error) { return Promise.reject(error); }
    }

    boardCommand(body, machine) {
        try {
            if (machine !== undefined && (!machine || !this._knownMachines().includes(machine))) {
                throw cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory");
            }
            return this._machineRequest(machine || this._accountMachine("board-command", { feature: "Project Board" }),
                "board-command", { command: body }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    timeline(project, entry, cursor, environment, category, includeUpcoming, machine) {
        try {
            if (machine !== undefined && (!machine || !this._knownMachines().includes(machine))) {
                throw cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory");
            }
            return this._machineRequest(machine || this._accountMachine("timeline", { feature: "Project Timeline" }), "timeline", {
                project: project || "", entry: entry || "", cursor: cursor ? String(cursor) : "",
                environment: environment || "production", category: category || "",
                upcoming: !!includeUpcoming
            }, "read");
        } catch (error) { return Promise.reject(error); }
    }

    timelineCommand(body, machine) {
        try {
            if (machine !== undefined && (!machine || !this._knownMachines().includes(machine))) {
                throw cloudError("cloud_machine_unavailable",
                    "this Mac has not published a current Cloud inventory");
            }
            return this._machineRequest(machine || this._accountMachine("timeline-command", { feature: "Project Timeline" }),
                "timeline-command", { command: body }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    pastSessions(place, assistant) {
        try {
            var route = this._place(place);
            return this._machineRequest(route.machine, "past-sessions",
                { place: route.id, assistant: assistant || "" }, "read");
        } catch (error) { return Promise.reject(error); }
    }

    startPlace(place, assistant, model) {
        try {
            var route = this._place(place);
            return this._machineRequest(route.machine, "start", {
                place: route.id, assistant: assistant || "", model: model || ""
            }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    resumePlace(place, past, assistant, actionRequestId) {
        try {
            var route = this._place(place);
            if (actionRequestId) {
                return this._read({ machine: route.machine, session: MACHINE_REPLY_SESSION }, "resume", {
                    request: actionRequestId, place: route.id, past: String(past || ""), assistant: assistant || ""
                }, "action:" + actionRequestId);
            }
            return this._machineRequest(route.machine, "resume", {
                place: route.id, past: String(past || ""), assistant: assistant || ""
            }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    /** End the session on the Mac that published it, with the same two close gates as HTTP. */
    end(value, acceptLoss, closeabilityVersion) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var request = requestID();
        return this._read(identity, "end", {
            request: request,
            accept_loss: acceptLoss === true,
            expected_closeability_version: String(closeabilityVersion || "")
        }, "action:" + request);
    }

    /**
     * Bring the session's terminal forward on the Mac that published it — Show on Mac.
     *
     * Two menus call `api.focus` without asking whether this transport has one, so its absence
     * was not a refusal but a `TypeError` inside the click handler: nothing sent, nothing said.
     * It resolves on the Mac's own answer, like `end`, so a terminal that could not be raised
     * comes back as that Mac's typed refusal rather than as a toast claiming it was asked.
     */
    focus(value) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var request = requestID();
        return this._read(identity, "focus", { request: request }, "action:" + request);
    }

    /**
     * Stop one of that session's background commands on the Mac that published it.
     *
     * The shell panel's confirmation calls `api.killShell` unguarded, like Show on Mac, so its
     * absence was the same silent `TypeError`. This one destroys something, so it resolves only on
     * the owning Mac's answer: `unidentified` — nothing was signalled — reaches the panel as that
     * word rather than as "stopped". An empty id is refused here, as the shell read refuses one.
     */
    killShell(value, shellId) {
        var shell = readSubject(shellId);
        if (!shell) return Promise.reject(missingSubject("shell"));
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var request = requestID();
        return this._read(identity, "shell-kill", { request: request, shell: shell },
                          "action:" + request);
    }

    schedule(id) {
        try {
            var schedule = String(id || "");
            var machine = this._scheduleMachine(schedule);
            return this._machineRequest(machine, "schedule", { id: schedule }, "read");
        } catch (error) { return Promise.reject(error); }
    }

    _scheduleBody(schedule) {
        var body = schedule && typeof schedule === "object" && !Array.isArray(schedule)
            ? Object.assign({}, schedule) : {};
        // `input/schedule.js` deliberately gives both transports the local HTTP route's flat
        // request body (`at`, `days`, `place_id`, ...). The Mac is the one that validates it and
        // turns it into the stored `when`/`task` record. Looking under `task.place_id` here was
        // therefore looking at the *response* shape while handling the *request* shape: every
        // real Cloud Create/Save tried to route `undefined` and failed before reaching the Mac.
        var place = this._place(body.place_id);
        body.place_id = place.id;
        return { machine: place.machine, schedule: body };
    }

    createSchedule(schedule) {
        try {
            var routed = this._scheduleBody(schedule);
            return this._machineRequest(routed.machine, "schedule-create",
                { schedule: routed.schedule }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    updateSchedule(id, schedule) {
        try {
            var scheduleID = String(id || "");
            var machine = this._scheduleMachine(scheduleID);
            var routed = this._scheduleBody(schedule);
            if (routed.machine !== machine) {
                return Promise.reject(cloudError("cloud_schedule_machine_mismatch",
                    "a schedule cannot be moved to a Project on another Mac"));
            }
            return this._machineRequest(machine, "schedule-update",
                { id: scheduleID, schedule: routed.schedule }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    deleteSchedule(id) {
        try {
            var scheduleID = String(id || "");
            return this._machineRequest(this._scheduleMachine(scheduleID), "schedule-delete",
                { id: scheduleID }, "action");
        } catch (error) { return Promise.reject(error); }
    }


    runSchedule(id) {
        try {
            var scheduleID = String(id || "");
            return this._machineRequest(this._scheduleMachine(scheduleID), "schedule-run",
                { id: scheduleID }, "action");
        } catch (error) { return Promise.reject(error); }
    }

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
     * than made a different signature. `demand.foreground` does cross: opening a conversation is
     * interactive, while revision refreshes and agent reads remain background work.
     */
    transcript(value, phases, demand) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var priority = demand && demand.foreground ? "foreground" : "background";
        return this._read(identity, "transcript",
            { limit: TRANSCRIPT_LIMIT, priority: priority }, "transcript")
            .then(function (body) {
                return Object.assign({}, body, { optimisticIdentity: identity });
            });
    }

    /**
     * The documents published by one explicit machine/session pair. A bare session is never
     * resolved through the current inventory here: a pasted direct link has already named its
     * Mac, and changing that into "whichever row currently looks unique" makes the same URL mean
     * something else after a second Mac reconnects.
     */
    documents(value) {
        var identity;
        try { identity = normalizeDocumentIdentity(value); }
        catch (error) {
            return Promise.reject(cloudError("document_identity_required", error.message));
        }
        return this._read(identity, "documents", {}, "documents").then(function (body) {
            try { return documentListing(body, identity); }
            catch (error) { throw cloudError("bad_payload", error.message); }
        });
    }

    /** One inert text document, correlated by a random request id rather than its relative path. */
    document(value) {
        var locator;
        try { locator = normalizeDocumentLocator(value); }
        catch (error) { return Promise.reject(cloudError("malformed_document_locator", error.message)); }
        var request = requestID();
        var extra = { request: request, scope: locator.scope,
            task: locator.scope === "task" ? locator.task : "", path: locator.path };
        return this._read({ machine: locator.machine, session: locator.session }, "document",
            extra, "read:" + request).then(function (body) {
                try { return documentAnswer(locator, body); }
                catch (error) { throw cloudError("bad_payload", error.message); }
            });
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
     * What the terminal on the owning Mac is showing now.
     *
     * The direct transport receives a separate SSE revision event when a tmux pipe moves. Cloud
     * has no equivalent event channel: `t/<machine>/<session>` carries named request answers,
     * not ambient revisions. Preserve the Mac's backend fact but expose the transport's real
     * delivery mode as on-demand, so the existing panel polls at the same one-second floor it
     * already uses for iTerm2. That both collects an initial pending capture and keeps a visible
     * screen live instead of waiting forever for an SSE event this origin cannot receive.
     */
    screen(value) {
        return this._read(value, "screen", {}, "screen").then(function (body) {
            var screen = body && body.screen;
            if (!screen || screen.channel !== "signalled") return body;
            return Object.assign({}, body, { screen: Object.assign({}, screen, {
                channel: "on-demand", askAgainAfterMs: 1000
            }) });
        });
    }

    /**
     * One picture out of a transcript, as bytes.
     *
     * **Why the bytes and not a URL.** On the direct path a tile is `<img
     * src="/v1/artifacts/images/:id">` — relative and same-origin, which was the right decision
     * and is still the right decision there. Here the origin is the hosted console, which serves
     * no such route and never will: the Mac is not reachable from it, and a console that could
     * fetch the picture would be a console that had it in the clear. So there is no shorter thing
     * to send than the picture, and no ticket to redeem it with that would not itself be an
     * envelope. What is left to decide is only how big a picture may be, and the Mac decides that
     * from the relay's per-envelope cap: over it, the answer is `image_too_large_for_cloud` with
     * both numbers in it, and the tile says the size rather than drawing a broken image.
     *
     * The answer arrives named `image.<id>`, not `image`: a transcript's pictures are asked for
     * together and come back on one channel in whatever order the disk gives them.
     */
    image(value, id) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var artifact = String(id == null ? "" : id);
        if (!artifact) return Promise.reject(cloudError("malformed_read", "image() needs an artifact id"));
        var self = this;
        return this._whenImageSlotFree(function () {
            return self._read(identity, "image", { id: artifact }, "image." + artifact)
                .then(function (body) { return imageAnswerBytes(artifact, body); });
        });
    }

    /**
     * Pace the picture reads without refusing any of them.
     *
     * A queued read has not been published, so it holds no envelope sequence and no waiter; when
     * the socket drops, the reads in flight are failed by `_failAllReads` and the ones still here
     * fail the moment they try to publish. Either way each caller is rejected with a code rather
     * than left holding a promise.
     */
    _whenImageSlotFree(start) {
        var self = this;
        return new Promise(function (resolve, reject) {
            self.imageReadQueue.push(function () {
                var settled;
                try { settled = Promise.resolve(start()); }
                catch (error) { settled = Promise.reject(error); }
                settled.then(resolve, reject);
                return settled.catch(function () { });
            });
            self._startQueuedImageReads();
        });
    }

    _startQueuedImageReads() {
        var self = this;
        while (this.imageReadsRunning < this.imageReadsInFlight && this.imageReadQueue.length) {
            var next = this.imageReadQueue.shift();
            this.imageReadsRunning += 1;
            next().then(function () {
                self.imageReadsRunning -= 1;
                self._startQueuedImageReads();
            });
        }
    }

    /**
     * One read, asked on the command channel and answered on the session's own.
     *
     * At most one request per (session, read) is in flight: a second caller joins the first
     * rather than spending another envelope sequence, which is what the direct path's own
     * transcript coalescing does for the same reason.
     */
    _read(value, type, extra, answer, timeoutMs, readOptions) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var probe = !(readOptions && readOptions.probe === false);
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
        // Before the waiter, the subscription and the sequence: a machine known not to implement
        // this word is sent nothing, and the page hears so now instead of at the read timeout.
        var unsupported = this._unsupportedRefusal(identity.machine, type);
        if (unsupported) return Promise.reject(unsupported);
        var key = readKey(identity, answer);
        var self = this;
        return new Promise(function (resolve, reject) {
            var waiters = self.readWaiters.get(key);
            if (waiters) {
                waiters.waiting.push({ resolve: resolve, reject: reject });
                return;
            }
            waiters = { waiting: [{ resolve: resolve, reject: reject }], timer: null, ref: null,
                machine: identity.machine, type: type, request: extra && typeof extra.request === "string"
                    ? extra.request : null,
                retireUncertain: !!(readOptions && readOptions.retireUncertain) };
            self.readWaiters.set(key, waiters);
            waiters.timer = self.setTimeout(function () {
                if (self.readWaiters.get(key) !== waiters) return;
                waiters.timer = null;
                self._readTimedOut(key, waiters, probe);
            }, timeoutMs || self.readTimeoutMs);
            try {
                self.subscribe(["t/" + channelSegment(identity.machine) + "/"
                    + channelSegment(identity.session)]);
            } catch (error) {
                self._settleRead(key, null, cloudError("malformed_read", error.message));
                return;
            }
            Promise.resolve()
                .then(function () {
                    return self._publishCommand(identity.machine, type,
                        Object.assign({ session: identity.session }, extra), "ctl",
                        { key: key, waiters: waiters });
                })
                .catch(function (error) { self._settleRead(key, null, error); });
        });
    }

    /**
     * Sixty seconds without an answer. `relayed` proves only that the relay wrote the envelope to
     * the Mac's socket (§2.4), so before saying "the Mac did not answer" this asks a Mac that can
     * say — one that has shown `cloud_status.v >= 1` — what became of this very `ref`. A Mac that
     * executed it and could not deliver the reply is a different sentence (G1), and so is one that
     * refused it and had nowhere to send the refusal. The waiter stays registered while it asks,
     * so a late answer still wins.
     */
    _readTimedOut(key, waiters, probe) {
        var ref = waiters.ref;
        var timeout = cloudError("cloud_read_timeout", "the Mac did not answer this read");
        if (!probe || !ref || !this.macCapabilities.has(waiters.machine)) {
            this._settleRead(key, null, timeout);
            return;
        }
        var self = this;
        this._readCloudStatus(waiters.machine, this.statusProbeTimeoutMs).then(function (status) {
            var rows = Array.isArray(status.commands) ? status.commands : [];
            var row = rows.find(function (candidate) {
                return candidate && candidate.sender === ref.sender && candidate.seq === ref.seq;
            });
            if (!row) return timeout;
            if (row.refusal && isFailureCode(row.refusal.code)) {
                return failureFromMac({ code: row.refusal.code, layer: row.refusal.layer }, null, ref);
            }
            if (isFailureCode(row.undeliverable) || Number.isFinite(row.executed_at_ms)) {
                return cloudFailure(isFailureCode(row.undeliverable)
                    ? row.undeliverable : "reply_not_received",
                    "the Mac executed this command and its reply did not arrive",
                    { layer: "mac_reply", ref: ref });
            }
            return timeout;
        }, function () { return timeout; }).then(function (failure) {
            // The original read may settle while its status probe is in flight, and a new read
            // can legitimately reuse the same key. Never apply the old probe to that waiter.
            if (self.readWaiters.get(key) === waiters) self._settleRead(key, null, failure);
        });
    }

    /**
     * Every settle of a read goes through here, success or refusal, so the trail sees each one
     * and every rejection leaves as a `CloudFailure` carrying the `ref` it was sent under.
     */
    _settleRead(key, body, error) {
        var waiters = this.readWaiters.get(key);
        if (!waiters) return;
        this.readWaiters.delete(key);
        if (waiters.timer !== null) this.clearTimeout(waiters.timer);
        // The machine's own word that it does not know this command: later requests for the word
        // are refused here (`_unsupportedRefusal`) and the fan-out reads leave the machine out.
        if (error && error.code === "unknown_command" && waiters.machine && waiters.type) {
            var lacks = this.machineLacks.get(waiters.machine) || new Set();
            lacks.add(waiters.type);
            this.machineLacks.set(waiters.machine, lacks);
        }
        var ref = waiters.ref;
        if (ref) this.pendingBySequence.delete(ref.seq);
        if (error) {
            error = this._withRef(error, ref);
            this.trail.refused(error.ref, error);
        } else if (ref) {
            this.trail.step(ref, "observed");
        }
        waiters.waiting.forEach(function (waiter) {
            if (error) waiter.reject(error); else waiter.resolve(body);
        });
    }

    /** A rejection named: its `ref`, and a way for the page that draws it to mark `acknowledged`. */
    _withRef(error, ref) {
        var failure = asCloudFailure(error, ref);
        if (ref && failure.ref && !failure.ref.sender) {
            failure.ref = { sender: ref.sender, seq: failure.ref.seq === null ? ref.seq : failure.ref.seq,
                request: failure.ref.request || ref.request || null };
        }
        if (failure.ref && typeof failure.acknowledge !== "function") {
            var trail = this.trail;
            var named = failure.ref;
            Object.defineProperty(failure, "acknowledge", {
                configurable: true, enumerable: false,
                value: function () { trail.step(named, "acknowledged"); }
            });
        }
        return failure;
    }

    _failAllReads(error, acknowledgementError) {
        var keys = Array.from(this.readWaiters.keys());
        var self = this;
        keys.forEach(function (key) {
            var waiters = self.readWaiters.get(key);
            self._settleRead(key, null,
                acknowledgementError && waiters && waiters.retireUncertain
                    ? acknowledgementError : error);
        });
        Array.from(this.pendingBySequence.entries()).forEach(function (entry) {
            if (entry[1].ack) self._failPending(entry[0], acknowledgementError || error);
        });
    }

    /**
     * The relay's answer to one publish (B1, B2): `ack` with `delivered` or `machine_offline`, or
     * `publish_error`. Both name `(ch, seq)`, so the request they are about is found by sequence
     * and settled now — not sixty seconds later by a timer that has lost the code.
     */
    _relayAnswered(frame, refusal) {
        var seq = frame && frame.seq;
        if (!Number.isSafeInteger(seq)) return;
        var pending = this.pendingBySequence.get(seq);
        var ref = pending ? pending.ref : { sender: this.deviceID, seq: seq };
        if (pending && typeof frame.ch === "string" && frame.ch !== "ctl/" + channelSegment(pending.machine)) {
            return;
        }
        if (!refusal && frame.status === "machine_offline") {
            refusal = failureFromRelay("machine_offline", { message: "the Mac is not connected to the relay" });
        }
        if (!refusal) {
            this.trail.step(ref, "relayed");
            if (pending && pending.ack) {
                this.pendingBySequence.delete(seq);
                pending.ack.settle(null);
            }
            return;
        }
        if (frame.status === "machine_offline") this.trail.step(ref, "machine_offline");
        refusal.ref = ref;
        if (pending) this._failPending(seq, refusal);
        else this.trail.refused(ref, refusal);
    }

    _failPending(seq, failure) {
        var pending = this.pendingBySequence.get(seq);
        if (!pending) return;
        this.pendingBySequence.delete(seq);
        if (pending.key) {
            this._settleRead(pending.key, null, failure);
            return;
        }
        var named = this._withRef(failure, pending.ref);
        this.trail.refused(named.ref, named);
        if (pending.ack) pending.ack.settle(named);
    }

    /**
     * B9: the key this browser seals with, against the key each Mac envelope arrives under.
     * Checked before anything tries to open the envelope, because a mismatch is exactly the case
     * where opening it fails and nothing else gets to say why.
     */
    _compareKeyID(envelope, routedMachine, expectedKeyID) {
        if (!envelope || typeof envelope.key_id !== "string" || !envelope.key_id) return;
        var machine = routedMachine;
        if (!machine) {
            try { machine = decodedChannelSegment(parseEnvelopeChannel(envelope.ch).machine); }
            catch (e) { return; }
        }
        if (!machine) return;
        this.trail.sawKeyID(machine, expectedKeyID || this.keyID, envelope.key_id);
    }

    /**
     * §11.2: the Mac's notice digest, riding its `orch/<machine>` snapshot. It is the only way a
     * Mac can speak about a command it could not answer — one it dropped before decrypting, or
     * refused with no reply address — so an entry naming this device and a sequence still waiting
     * settles that request now, in the Mac's own layer and code. `v >= 1` is also this Mac's
     * capability signal (§11.4).
     */
    _consumeCloudStatus(machine, status) {
        if (!status || typeof status !== "object" || Array.isArray(status)) return;
        if (!Number.isInteger(status.v) || status.v < 1) return;
        this.macCapabilities.add(machine);
        var entries = function (value) { return Array.isArray(value) ? value.slice(0, 10) : []; };
        var drops = entries(status.recent_drops);
        var notices = entries(status.recent_notices);
        this.trail.macDigest(machine, {
            v: status.v,
            generated_at_ms: Number.isFinite(status.generated_at_ms) ? status.generated_at_ms : null,
            counting_since_ms: Number.isFinite(status.counting_since_ms) ? status.counting_since_ms : null,
            clock_guard: status.clock_guard && typeof status.clock_guard === "object" ? {
                state: isFailureCode(status.clock_guard.state) ? status.clock_guard.state : null,
                reason: isFailureCode(status.clock_guard.reason) ? status.clock_guard.reason : null,
                clears_at_ms: Number.isFinite(status.clock_guard.clears_at_ms)
                    ? status.clock_guard.clears_at_ms : null
            } : null,
            token_expires_at_ms: Number.isFinite(status.token_expires_at_ms) ? status.token_expires_at_ms : null,
            key_id: typeof status.key_id === "string" ? status.key_id.slice(0, 64) : null,
            roster_readable: typeof status.roster_readable === "boolean" ? status.roster_readable : null,
            dropped: countTable(status.dropped),
            recent_drops: drops.filter(isNotice).map(noticeRow),
            recent_notices: notices.filter(isNotice).map(noticeRow)
        });
        var self = this;
        drops.concat(notices).forEach(function (entry, index) {
            if (!isNotice(entry) || entry.sender !== self.deviceID) return;
            var ref = { sender: entry.sender, seq: entry.seq,
                request: typeof entry.request === "string" ? entry.request : null };
            var failure = failureFromMac(Object.assign({}, entry,
                { layer: index < drops.length ? "mac_transport" : entry.layer }), null, ref);
            if (failure.code === "replay" && self.trail.find(ref.sender, ref.seq)) {
                self.trail.sawReplay(machine, ref.seq,
                    Number.isSafeInteger(entry.highest_seq) ? entry.highest_seq : null);
            }
            if (self.pendingBySequence.has(ref.seq)) self._failPending(ref.seq, failure);
            else self.trail.refused(ref, failure);
        });
    }

    /** §6.3: other tabs of this device, found by asking on a channel only this origin can hear. */
    _openTabChannel() {
        if (this.tabChannel || typeof this.BroadcastChannel !== "function" || !this.deviceID) return;
        var channel;
        try { channel = new this.BroadcastChannel("clawdline.cloud.tabs." + this.deviceID); }
        catch (e) { return; }
        this.tabChannel = channel;
        var self = this;
        channel.onmessage = function (event) {
            var message = event && event.data;
            if (!message || typeof message.tab !== "string" || message.tab === self.tabID) return;
            self.trail.sawOtherTab(message.tab);
            if (message.type === "hello") {
                try { channel.postMessage({ type: "here", tab: self.tabID }); } catch (e) { }
            }
        };
        try { channel.postMessage({ type: "hello", tab: this.tabID }); } catch (e) { }
    }

    _closeTabChannel() {
        if (!this.tabChannel) return;
        try { this.tabChannel.close(); } catch (e) { }
        this.tabChannel = null;
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

    /**
     * The Mac's build, version and protocol, carried on the machine's own snapshot.
     *
     * On the direct path `/v1/health` answers these on every connect and every reconnect, and
     * `Build.saw` compares one answer against the last to raise "Clawdline has been rebuilt on
     * the Mac. This page is the older one." The relay's `ready` frame is the *relay's*: it knows
     * an account and a device and has never heard of a build, which is why `_becameReady` could
     * only ever hand `handlers.hello` a write flag and the banner could not fire on this path at
     * all. The stamp therefore rides `orch/<machine>`, which the Mac republishes on every
     * transport-ready — the same moment health would have been asked again.
     *
     * **`write` is not read from the snapshot.** That field on the Mac is its own network's
     * switch; a viewer's write state is the capability its device was granted, which is what
     * `_becameReady` already passes. Taking one for the other would let a switch on the Mac
     * silently regrant or revoke a paired phone.
     */
    _sawAppStamp(payload) {
        var app = payload && payload.app;
        if (!app || typeof app !== "object" || Array.isArray(app)) return;
        if (!this.handlers || !this.handlers.hello) return;
        this.handlers.hello({ build: app.build, version: app.version, protocol: app.protocol,
            write: this.allowWrites });
    }

    /**
     * Rows for one snapshot field, and — separately — whether any machine published the field.
     *
     * The two are not the same answer and used to be the same value. `[]` because this Mac has no
     * schedules and `[]` because nothing has ever said are opposite facts, and a viewer that
     * cannot tell them apart draws the empty screen for both. That is the whole defect here: a
     * person with six schedules saw none, and was told nothing.
     *
     * Given the command `type` that reads the field, a machine that cannot have it is left out, and
     * `incapableOnly` says every machine this account has is such a machine — a Linux-only account
     * has no schedules, which is a fact and not a silence.
     */
    _orchestratorRows(name, type) {
        var rows = [];
        var published = false;
        var at = 0;
        this.orchestratorSnapshots.forEach(function (snapshot, machine) {
            if (!snapshot || !Array.isArray(snapshot[name])) return;
            if (type && this._machineImplements(machine, type) === "no") return;
            published = true;
            if (typeof snapshot.at === "number" && snapshot.at > at) at = snapshot.at;
            snapshot[name].forEach(function (row) {
                rows.push(Object.assign({}, row, { machine: machine }));
            });
        }, this);
        var known = type ? this._knownMachines() : [];
        var incapableOnly = !!type && known.length > 0 && known.every(function (machine) {
            return this._machineImplements(machine, type) === "no";
        }, this);
        return { published: published, rows: rows, at: at, incapableOnly: incapableOnly };
    }

    _allOrchestratorRows(name) { return this._orchestratorRows(name).rows; }

    tasks() { return Promise.resolve({ tasks: this._allOrchestratorRows("tasks") }); }

    /**
     * The schedules every machine on this account published, or a refusal saying why there are
     * none to give.
     *
     * Two codes rather than one, because they are two different things to be waiting for:
     * `cloud_read_unavailable` is "no `orch/` snapshot has arrived yet", which a second of
     * patience fixes, and `cloud_schedules_unpublished` is "a snapshot arrived and carried no
     * such field", which means the Mac is running a build older than this one and no amount of
     * waiting will help. Neither resolves to an empty list: `net/schedules.js` renders whatever
     * it is handed, so resolving `[]` here is the page positively asserting an inventory nobody
     * has read. The one empty list it does resolve is an account whose every machine cannot have
     * schedules at all.
     */
    schedules(options) {
        if (options && options.fresh === true) return this._freshSchedules();
        return this._publishedAnswer("schedules", "schedules", this._orchestratorRows("schedules", "schedules"));
    }

    /** A `_orchestratorRows` answer as `schedules()`/`snippets()` resolve it, or the refusal for none. */
    _publishedAnswer(name, type, answer) {
        if (answer.published || answer.incapableOnly) {
            var out = { at: answer.at };
            out[name] = answer.rows;
            return Promise.resolve(out);
        }
        return Promise.reject(this.orchestratorSnapshots.size
            ? cloudError("cloud_" + name + "_unpublished", "this Mac does not publish its " + name + " over the relay")
            : cloudError("cloud_read_unavailable", "no orchestrator snapshot has arrived from this account yet"));
    }

    /**
     * A retained `orch/` envelope is a fast first paint, not evidence that the list is current.
     * The visible refresh asks every machine that can have schedules — never a Linux executor —
     * each on its own, and replaces only an answering machine's schedule field, so a reconnect
     * cannot leave a truthful local list hidden behind an old empty relay snapshot and one silent
     * machine cannot hide the others' rows. A machine that could have answered and did not keeps
     * its retained rows and is named in `unanswered`.
     */
    _freshSchedules() {
        if (!this._knownMachines().length) {
            return Promise.reject(cloudError("cloud_read_unavailable",
                "no Mac has published an inventory to this account yet"));
        }
        var self = this;
        var found = this._machinesFor("schedules");
        return this._askEach(found.capable.map(function (row) { return row.id; }), function (machine) {
            return self._machineRequest(machine, "schedules", {}, "read").then(function (answer) {
                var rows = answer && Array.isArray(answer.schedules) ? answer.schedules : [];
                var at = answer && typeof answer.at === "number" ? answer.at : 0;
                var previous = self.orchestratorSnapshots.get(machine) || {};
                self.orchestratorSnapshots.set(machine,
                    Object.assign({}, previous, { schedules: rows, at: at || previous.at || 0 }));
            });
        }).then(function (settled) {
            var report = self._fanOutReport(settled, found);
            if (!report.answered.length && report.unanswered.length) throw report.unanswered[0].error;
            return self._publishedAnswer("schedules", "schedules", self._orchestratorRows("schedules", "schedules"))
                .then(function (answer) {
                    return Object.assign(answer, { unanswered: report.unanswered, unconfirmed: report.unconfirmed });
                });
        });
    }

    /**
     * The snippets every machine on this account published. A retained snapshot is first paint;
     * `fresh` asks the Mac that owns the open Session, which is what a write completion needs in
     * order not to repaint the row from the snapshot that existed before the write.
     *
     * Same two codes as `schedules()` above, and the same reason for there being two: waiting
     * helps for one of them and cannot help for the other. Neither resolves to an empty list,
     * because "this Mac has no snippets" and "nothing has told us" are opposite facts and the
     * sheet draws a different thing for each.
     *
     * **The sheet's question names one machine.** Given a Session it is that Session's machine
     * that matters — `snippetGroups` keeps only its rows — so only that machine is asked when its
     * retained snapshot lacks the field, and only its snapshot decides "nothing has told us". On
     * 2026-09-15 this asked every machine whose snapshot lacked the field, the enrolled Linux
     * executor among them, which has no snippets and no reply for the word; the sheet waited on it
     * until the read timeout. A Session on a machine that cannot have snippets is refused at once
     * as `cloud_machine_unsupported`. With no Session it is an account-level read over every machine
     * that can have snippets, each on its own, naming the ones that did not answer.
     *
     * **Whole records, unfiltered, tagged with the machine that published them.** There is no
     * `?session=` here: the list rides `orch/<machine>`, which is a machine's inventory rather
     * than an answer to one session's question, so `view/snippets-data.js:snippetGroups` matches
     * `row.project` against the open session's own `cwd` by equality and by nothing else. The
     * resolution rule — registry prefix, worktree folding — stays on the Mac where the registry
     * and the git directory are, so a relay reader sees a smaller list rather than a guessed one.
     *
     * The writing half below uses the same closed command channel as schedules. Every operation
     * is routed by the open Session's machine identity: a snippet UUID or the first machine in a
     * snapshot is not authority to choose which Mac's settings should change.
     */
    snippets(value, options) {
        var fresh = !!(options && options.fresh === true);
        var self = this;
        var retained = function (machine) {
            var snapshot = self.orchestratorSnapshots.get(machine);
            return !!snapshot && Array.isArray(snapshot.snippets);
        };
        if (value !== undefined && value !== null) {
            var machine;
            try { machine = this._sessionIdentity(value).machine; }
            catch (error) { return Promise.reject(error); }
            var refusal = this._unsupportedRefusal(machine, "snippets");
            if (refusal) return Promise.reject(refusal);
            // A reconnect first receives the relay's retained snapshot. That snapshot can predate
            // the snippets field even when the live Mac supports it, so it is a fast first paint
            // but not proof that the feature is absent: ask this Session's machine.
            var ask = fresh || (!retained(machine) && this.allowWrites);
            return (ask ? this._freshSnippets([machine]) : Promise.resolve([])).then(function (settled) {
                if (settled.length && settled[0].error) throw settled[0].error;
                if (!retained(machine)) {
                    throw self.orchestratorSnapshots.has(machine)
                        ? cloudError("cloud_snippets_unpublished", "this Mac does not publish its snippets over the relay")
                        : cloudError("cloud_read_unavailable", "no orchestrator snapshot has arrived from this machine yet");
                }
                var answer = self._orchestratorRows("snippets", "snippets");
                return { snippets: answer.rows, at: answer.at };
            });
        }
        var found = this._machinesFor("snippets");
        var asking = found.capable.map(function (row) { return row.id; }).filter(function (id) {
            return fresh || !retained(id);
        });
        var asked = asking.length && this.allowWrites ? this._freshSnippets(asking) : Promise.resolve([]);
        return asked.then(function (settled) {
            var report = self._fanOutReport(settled, found);
            var answer = self._orchestratorRows("snippets", "snippets");
            if (!answer.published && !answer.incapableOnly && report.unanswered.length) throw report.unanswered[0].error;
            return self._publishedAnswer("snippets", "snippets", answer).then(function (out) {
                return Object.assign(out, { unanswered: report.unanswered, unconfirmed: report.unconfirmed });
            });
        });
    }

    /** Each machine's snippets on its own, stored into its snapshot; settles with every outcome. */
    _freshSnippets(machines) {
        var self = this;
        return this._askEach(machines, function (machine) {
            return self._machineRequest(machine, "snippets", {}, "read").then(function (answer) {
                var rows = answer && Array.isArray(answer.snippets) ? answer.snippets : [];
                var at = answer && typeof answer.at === "number" ? answer.at : 0;
                var previous = self.orchestratorSnapshots.get(machine) || {};
                self.orchestratorSnapshots.set(machine,
                    Object.assign({}, previous, { snippets: rows, at: at || previous.at || 0 }));
            });
        });
    }

    /** One closed snippet mutation, addressed to the Mac that published the open Session. */
    _snippetRequest(value, type, extra) {
        try {
            return this._machineRequest(this._sessionIdentity(value).machine, type, extra, "action");
        } catch (error) { return Promise.reject(error); }
    }

    createSnippet(snippet, value) {
        return this._snippetRequest(value, "snippet-create", { snippet: snippet });
    }

    updateSnippet(id, snippet, value) {
        return this._snippetRequest(value, "snippet-update",
            { id: String(id || ""), snippet: snippet });
    }

    deleteSnippet(id, value) {
        return this._snippetRequest(value, "snippet-delete", { id: String(id || "") });
    }

    orderSnippets(scope, project, order, value) {
        var ordering = { scope: scope, order: order };
        if (project) ordering.project = project;
        return this._snippetRequest(value, "snippet-order", { ordering: ordering });
    }

    send(value, text, images) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        if (!this.allowWrites) {
            return Promise.reject(cloudError("cloud_read_only", "cloud writes are disabled"));
        }
        var request = requestID();
        return this._read(identity, "send", {
            request: request, text: text || "", images: images || []
        }, "action:" + request).then(function (body) {
            return Object.assign({}, body, {
                optimisticIdentity: identity, optimisticRequest: request
            });
        });
    }

    /**
     * A keypress or menu answer. It has no reply of its own to wait for, so it waits for the
     * relay's word on it instead: `machine_offline` or a `publish_error` rejects at once (B1, B2)
     * rather than resolving as if the key had landed.
     *
     * `request` is added only for a Mac that has shown `cloud_status.v >= 1` (§11.4). An older
     * Mac checks this command's key set exactly and would refuse the extra key, which would make
     * every menu on a new console unpressable against an old app.
     */
    answer(value, answer) {
        var identity;
        try { identity = this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
        var body = { session: identity.session, answer: String(answer) };
        if (!this.macCapabilities.has(identity.machine)) {
            return this._publishAcknowledged(identity.machine, "answer", body, "ctl");
        }
        var request = requestID();
        // A capable Mac promises an action:<request> result. Keep the relay ack as immediate
        // machine_offline/publish_error evidence, but resolve only from the Mac's result so a
        // preflight refusal can never look like a successfully delivered keypress.
        return this._read(identity, "answer", { request: request, answer: body.answer },
            "action:" + request, undefined, { retireUncertain: true });
    }

    _publishAcknowledged(machine, type, body, envelopeClass) {
        var self = this;
        return new Promise(function (resolve, reject) {
            var pending = { ack: null };
            var envelope = null;
            var relayed = false;
            var timer = null;
            pending.ack = {
                settle: function (failure) {
                    if (timer !== null) { self.clearTimeout(timer); timer = null; }
                    if (failure) { reject(failure); return; }
                    // The relay can answer inside the same turn the frame was written, before
                    // `_publishCommand` has handed the envelope back; resolve with it once it has.
                    relayed = true;
                    if (envelope) resolve(envelope);
                }
            };
            self._publishCommand(machine, type, body, envelopeClass, pending).then(function (sealed) {
                envelope = sealed;
                if (relayed) { resolve(sealed); return; }
                if (self.pendingBySequence.get(sealed.seq) !== pending.registered) return;   // refused already
                timer = self.setTimeout(function () {
                    timer = null;
                    if (self.pendingBySequence.get(sealed.seq) !== pending.registered) return;
                    self.pendingBySequence.delete(sealed.seq);
                    resolve(sealed);
                }, self.ackTimeoutMs);
            }, reject);
        });
    }

    key(value, answer) { return this.answer(value, answer); }

    /**
     * A push subscription belongs to one Mac's keys, so every push call picks that Mac strictly:
     * the one machine on the account that takes push commands — a Linux executor does not — or a
     * typed refusal. Not the freshest of two: the key, the subscription and its removal must all
     * reach the same Mac, and freshness can change between them. A test for a Session goes to the
     * machine that published that Session.
     */
    _pushMachine(type) {
        return this._accountMachine(type, { strict: true, feature: "notifications" });
    }

    pushKey() {
        try {
            return this._machineRequest(this._pushMachine("push-key"), "push-key", {}, "read");
        } catch (error) { return Promise.reject(error); }
    }

    pushSubscribe(subscription) {
        try {
            return this._machineRequest(this._pushMachine("push-subscribe"), "push-subscribe",
                { subscription: subscription }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    pushUnsubscribe(id) {
        try {
            return this._machineRequest(this._pushMachine("push-unsubscribe"), "push-unsubscribe",
                { id: String(id || "") }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    pushTest(value) {
        try {
            var identity = value ? this._sessionIdentity(value) : null;
            // Settings passes whatever Session the list points at. One on a machine that has no
            // push at all — a Linux executor's — cannot be a notification's destination, so the test
            // is the account's: the push Mac, with no Session to tap back to.
            if (identity && this._machineImplements(identity.machine, "push-test") === "no") identity = null;
            var machine = identity ? identity.machine : this._pushMachine("push-test");
            return this._machineRequest(machine, "push-test",
                { target: identity ? identity.session : "" }, "action");
        } catch (error) { return Promise.reject(error); }
    }

    /** Dictation goes to the one Mac `voiceHost` names, never to a Linux executor. */
    voice(audio, rate) {
        try {
            var host = this._voiceHost();
            if (!host.machine) throw host.error;
            return this._machineRequest(host.machine, "voice",
                { audio: audio, rate: rate }, "action", VOICE_TIMEOUT_MS);
        } catch (error) { return Promise.reject(error); }
    }

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
        try { this._sessionIdentity(value); }
        catch (error) { return Promise.reject(error); }
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
        if (typeof machine !== "string" || !machine) {
            return Promise.reject(cloudError("malformed_command", "dispatch needs a machine"));
        }
        return this._publishCommand(machine, "dispatch", { task: task }, "dispatch");
    }

    /**
     * Seal and write one command. `pending`, when given, is registered under the sequence before
     * the frame is written, so the relay's `ack` or `publish_error` — and a Mac notice — can find
     * the request it is about however soon it arrives.
     */
    async _publishCommand(machine, type, body, envelopeClass, pending) {
        if (!this.allowWrites) throw cloudError("cloud_read_only", "cloud writes are disabled");
        var unsupported = this._unsupportedRefusal(machine, type);
        if (unsupported) throw unsupported;
        if (!this.ready) throw this.closedFailure || cloudError("offline", "the cloud connection is not ready");
        if (!this.devicePrivateKey || !this.deviceID) throw cloudError("missing_device_key", "the viewer device key is unavailable");
        var machinePairing = await this._outboundMachinePairing(machine);
        var sequence = await this.nextSequence(this.deviceID);
        if (!Number.isSafeInteger(sequence) || sequence < 0) {
            throw cloudError("bad_sequence", "nextSequence() did not return a non-negative safe integer");
        }
        var envelope = await sealEnvelope({
            ch: "ctl/" + channelSegment(machine),
            seq: sequence,
            ts: Date.now(),
            class: envelopeClass,
            key_id: machinePairing.keyID,
            sender: this.deviceID
        }, JSON.stringify(Object.assign({ type: type }, body)),
        machinePairing.masterKey, this.devicePrivateKey);
        var ref = { sender: this.deviceID, seq: sequence,
            request: body && typeof body.request === "string" ? body.request : null };
        if (pending) {
            if (pending.waiters) {
                if (this.readWaiters.get(pending.key) !== pending.waiters) {
                    throw cloudError("cloud_read_settled", "the read settled before it was sent");
                }
                pending.waiters.ref = ref;
            }
            pending.registered = { ref: ref, machine: machine,
                key: pending.key || null, ack: pending.ack || null };
            this.pendingBySequence.set(sequence, pending.registered);
        }
        this.trail.sealed({ sender: ref.sender, seq: sequence, request: ref.request, type: type,
            machine: machine });
        try {
            this._send({ type: "publish", envelope: envelope });
        } catch (error) {
            this.pendingBySequence.delete(sequence);
            if (pending && pending.waiters) pending.waiters.ref = null;
            throw this.closedFailure || error;
        }
        return envelope;
    }

    _send(frame) {
        if (!this.socket || this.socket.readyState !== 1) {
            throw this.closedFailure || cloudError("offline", "the cloud socket is not open");
        }
        this.socket.send(JSON.stringify(frame));
    }
}

/** A notice row this client can act on: a sender, a sequence, a code (§11.2). */
function isNotice(entry) {
    return !!entry && typeof entry === "object" && typeof entry.sender === "string" &&
        Number.isSafeInteger(entry.seq) && isFailureCode(entry.code);
}

/** The fields of a notice row the status sheet may show; nothing else crosses. */
function noticeRow(entry) {
    return {
        at_ms: Number.isFinite(entry.at_ms) ? entry.at_ms : null,
        sender: entry.sender.slice(0, 128), seq: entry.seq,
        request: typeof entry.request === "string" ? entry.request.slice(0, 64) : null,
        layer: isFailureCode(entry.layer) ? entry.layer : null,
        code: entry.code,
        key_id: typeof entry.key_id === "string" ? entry.key_id.slice(0, 64) : null,
        expected_key_id: typeof entry.expected_key_id === "string" ? entry.expected_key_id.slice(0, 64) : null,
        highest_seq: Number.isSafeInteger(entry.highest_seq) ? entry.highest_seq : null
    };
}

function countTable(value) {
    var counts = {};
    if (!value || typeof value !== "object" || Array.isArray(value)) return counts;
    Object.keys(value).slice(0, 32).forEach(function (code) {
        if (isFailureCode(code) && Number.isSafeInteger(value[code]) && value[code] >= 0) {
            counts[code] = value[code];
        }
    });
    return counts;
}

function bytesToBase64(value) {
    var bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
    var binary = "";
    for (var i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
    return btoa(binary);
}
