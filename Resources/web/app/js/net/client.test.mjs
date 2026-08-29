import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { webcrypto } from "node:crypto";

if (!globalThis.crypto) {
    Object.defineProperty(globalThis, "crypto", { value: webcrypto, configurable: true });
}

const {
    LOCAL_MACHINE,
    assertClawdlineClient,
    parseSessionPath,
    sessionIdentity,
    sessionPath
} = await import("./client.js");
const {
    importDevicePrivateKey,
    importMasterSecret,
    importSenderPublicKey,
    openEnvelope,
    sealEnvelope,
    verifyEnvelope
} = await import("./cloud-crypto.js");

const vectors = JSON.parse(await readFile(
    new URL("../../../../../Tests/protocol-vectors.json", import.meta.url), "utf8"));
const masterKey = await importMasterSecret(vectors.master_secret);
const senderKey = await importSenderPublicKey(vectors.ed25519_public_key);
assert.equal(masterKey.extractable, false, "the imported master key is non-extractable");

for (const vector of vectors.envelopes) {
    const opened = await openEnvelope(vector.envelope, masterKey, senderKey);
    assert.equal(Buffer.from(opened).toString("base64"), vector.plaintext,
        vector.name + " opens to the golden plaintext");
}

const signedFields = ["v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct", "sig"];
for (const field of signedFields) {
    const changed = structuredClone(vectors.envelopes[1].envelope);
    if (field === "v") changed.v = 2;
    else if (field === "seq" || field === "ts") changed[field] += 1;
    else if (field === "class") changed.class = "dispatch";
    else if (field === "ch") changed.ch = "ctl/mac-02";
    else changed[field] = (changed[field][0] === "A" ? "B" : "A") + changed[field].slice(1);
    await assert.rejects(openEnvelope(changed, masterKey, senderKey),
        field + " mutation must not open");
}

const senderChanged = structuredClone(vectors.envelopes[1].envelope);
senderChanged.sender = "unknown-device";
assert.equal(await verifyEnvelope(senderChanged, function (sender) {
    return sender === vectors.envelopes[1].envelope.sender ? senderKey : null;
}), false, "changing sender selects no trusted key");

assert.deepEqual(sessionIdentity("ABC"), { machine: LOCAL_MACHINE, session: "ABC" });
assert.deepEqual(sessionIdentity({ machine: "desk", session: "%3" }),
    { machine: "desk", session: "%3" });
assert.deepEqual(parseSessionPath("/m/desk%20one/s/%253"),
    { machine: "desk one", session: "%3" });
assert.equal(sessionPath({ machine: "desk one", session: "%3" }), "/m/desk%20one/s/%253");

const seed = Buffer.from(vectors.ed25519_seed, "base64");
const pkcs8 = Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), seed]);
const signingKey = await importDevicePrivateKey(pkcs8);
const outbound = await sealEnvelope({
    ch: "ctl/mac-01", seq: 9, ts: 1787817600000, class: "ctl",
    key_id: "ms-1", sender: "device-vector-01"
}, JSON.stringify({ answer: "yes" }), masterKey, signingKey);
assert.equal(await verifyEnvelope(outbound, senderKey), true, "outbound ctl is Ed25519 signed");
assert.equal(new TextDecoder().decode(await openEnvelope(outbound, masterKey, senderKey)),
    JSON.stringify({ answer: "yes" }), "outbound ctl opens under the account key");

const localSurface = {
    events: function () {}, sessions: function () {}, transcript: function () {},
    send: function () {}, answer: function () {}, dispatch: function () {},
    schedules: function () {}
};
assert.equal(assertClawdlineClient(localSurface), localSurface,
    "the local surface satisfies the same structural seam");
for (const method of Object.keys(localSurface)) {
    const missing = { ...localSurface };
    delete missing[method];
    assert.throws(function () { assertClawdlineClient(missing); }, new RegExp(method));
}

globalThis.location = { search: "", protocol: "http:", hostname: "localhost", hash: "",
    pathname: "/", href: "http://localhost/" };
globalThis.history = { replaceState: function () {}, pushState: function () {} };
globalThis.window = {
    crypto: globalThis.crypto,
    matchMedia: function () { return { matches: false, addEventListener: function () {} }; },
    addEventListener: function () {},
    devicePixelRatio: 1,
    innerHeight: 800
};
function waitingButton(kind, tag) {
    tag = tag || "";
    const dataset = {};
    dataset[kind] = "1";
    const aria = /aria-label="([^"]*)"/.exec(tag);
    const ariaBusy = /aria-busy="([^"]*)"/.exec(tag);
    return {
        disabled: /(?:^|\s)disabled(?:\s|>)/.test(tag), dataset: dataset,
        ariaLabel: aria ? aria[1] : "",
        ariaBusy: ariaBusy ? ariaBusy[1] : null,
        closest: function (selector) {
            return selector === "[data-" + kind + "]" ? this : null;
        }
    };
}
const elements = new Map();
const inertElement = function (id) {
    const listeners = {};
    const target = {
        hidden: false, textContent: "", value: "", dataset: {},
        children: [], childNodes: [], offsetWidth: 40,
        style: { setProperty: function () {}, removeProperty: function () {} },
        classList: { add: function () {}, remove: function () {}, toggle: function () {} },
        addEventListener: function (name, fn) { listeners[name] = fn; },
        querySelector: function (selector) {
            if (selector === "[data-refresh]") return target.refreshButton || null;
            if (selector === "[data-refresh-status]") return target.refreshStatus || null;
            return inertElement();
        },
        querySelectorAll: function () { return []; }, appendChild: function () {},
        setAttribute: function () {}, removeAttribute: function () {}, focus: function () {},
        dispatchClick: function (node) {
            if (listeners.click) listeners.click({ target: node });
        }
    };
    Object.defineProperty(target, "innerHTML", {
        get: function () { return target._innerHTML || ""; },
        set: function (value) {
            target._innerHTML = value;
            target.refreshButton = null;
            target.refreshStatus = null;
            if (id !== "waiting") return;
            const button = /<button[^>]*data-refresh="1"[^>]*>/.exec(value);
            if (button) target.refreshButton = waitingButton("refresh", button[0]);
            const status = /<span[^>]*data-refresh-status="1"[^>]*>([^<]*)<\/span>/.exec(value);
            if (status) target.refreshStatus = { textContent: status[1] };
        }
    });
    return target;
};
function elementWithID(id) {
    if (!elements.has(id)) elements.set(id, inertElement(id));
    return elements.get(id);
}
const documentListeners = new Map();
globalThis.document = {
    hidden: false, activeElement: null, body: inertElement(),
    documentElement: Object.assign(inertElement(), { lang: "en" }),
    getElementById: elementWithID,
    createElement: inertElement,
    addEventListener: function (name, fn) {
        if (!documentListeners.has(name)) documentListeners.set(name, []);
        documentListeners.get(name).push(fn);
    },
    dispatchEvent: function (event) {
        (documentListeners.get(event.type) || []).forEach(function (fn) { fn(event); });
    }
};
if (!globalThis.CustomEvent) {
    globalThis.CustomEvent = class { constructor(type) { this.type = type; } };
}
globalThis.MutationObserver = class { observe() {} disconnect() {} };
globalThis.requestAnimationFrame = function (callback) { return setTimeout(callback, 0); };

const requests = [];
globalThis.fetch = async function (path, options) {
    requests.push({ path: path, options: options });
    return { ok: true, text: async function () { return "{}"; } };
};
const { Live, LocalClient } = await import("./live.js");
assert.equal(LocalClient, Live, "the compatibility Live export is the exact LocalClient");
assert.equal(assertClawdlineClient(LocalClient), LocalClient, "the real local client satisfies the seam");
await LocalClient.transcript("session one");
await LocalClient.transcript({ machine: LOCAL_MACHINE, session: "session one" });
assert.deepEqual(requests.map(function (request) { return request.path; }), [
    "/v1/sessions/session%20one/transcript?limit=200",
    "/v1/sessions/session%20one/transcript?limit=200"
], "string and (machine, session) local identities make byte-identical requests");

assert.equal(typeof LocalClient.refreshSessionEvidence, "function",
    "the local client exposes a session-evidence retry distinct from reconnect refresh");

const { useApi } = await import("./api.js");
const { handlers } = await import("./handlers.js");
const { S } = await import("../core/state.js");
const { renderWaiting } = await import("../view/composer.js");
const waiting = elementWithID("waiting");
const originalSessionsHandler = handlers.sessions;
const originalConnectionHandler = handlers.conn;
const appliedSessions = [];
handlers.sessions = function (list) { appliedSessions.push(list); return true; };
handlers.conn = function () {};
useApi(LocalClient);
S.openId = "WAITING-ONE";
S.sessions = [{ id: "WAITING-ONE", state: "waiting", menu: null }];
LocalClient.es = {};
LocalClient.resetSessionRefreshForTesting();

const refreshTimers = [];
LocalClient.scheduleSessionRefreshTimeout = function (fn, ms) {
    const timer = { fn: fn, ms: ms, cancelled: false };
    refreshTimers.push(timer);
    return timer;
};
LocalClient.cancelSessionRefreshTimeout = function (timer) { timer.cancelled = true; };

function jsonResponse(body, ok) {
    return { ok: ok !== false, status: ok === false ? 503 : 200,
        text: async function () { return JSON.stringify(body); } };
}
requests.length = 0;
let answerRefresh;
globalThis.fetch = function (path, options) {
    requests.push({ path: path, options: options });
    if (path !== "/v1/sessions/refresh") {
        return Promise.resolve(jsonResponse({ sessions: [] }));
    }
    return new Promise(function (resolve) { answerRefresh = resolve; });
};

await LocalClient.receiveSessions({ sessions: S.sessions, at: 7,
    scan: { generation: 7, complete: true, emptyAuthoritative: false } });
renderWaiting();
let retryButton = waiting.querySelector("[data-refresh]");
assert.ok(retryButton, "a local no-menu waiting card renders the retry control");
waiting.dispatchClick(retryButton);
retryButton = waiting.querySelector("[data-refresh]");
assert.equal(retryButton.disabled, true,
    "the retry control is disabled from client state while evidence is in flight");
assert.ok(retryButton.ariaLabel, "the retry control keeps an accessible name while busy");
assert.equal(retryButton.ariaBusy, "true", "the retry control exposes live busy progress");
assert.match(waiting.querySelector("[data-refresh-status]").textContent, /refresh/i,
    "the polite live status announces that fresh evidence is being requested");

// A changed question rebuilds waiting.innerHTML and therefore creates another button node.
S.sessions[0].menu = { question: "The question arrived without rows", options: [] };
renderWaiting();
const rebuiltRetryButton = waiting.querySelector("[data-refresh]");
assert.notEqual(rebuiltRetryButton, retryButton, "question evidence really rebuilt the control");
assert.equal(rebuiltRetryButton.disabled, true,
    "a rebuilt retry control remains disabled while the first request is in flight");
waiting.dispatchClick(rebuiltRetryButton);
assert.equal(requests.filter(function (request) {
    return request.path === "/v1/sessions/refresh";
}).length, 1, "press, rerender, press still starts only one refresh request");

answerRefresh(jsonResponse({ ok: true, state: "accepted", accepted: true,
    coalesced: false, throttled: false, scan: { generation: 7 } }));
for (let turn = 0; turn < 8; turn += 1) await Promise.resolve();
assert.equal(LocalClient.sessionRefreshEvidenceState().baseline, 7,
    "the acknowledgement baseline is installed before later evidence is sampled");
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, true,
    "an acknowledgement alone does not complete a refresh");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", state: "waiting" }], at: 8,
    scan: { generation: 7, complete: true, emptyAuthoritative: false } });
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, true,
    "same-generation evidence keeps the retry in flight");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", state: "waiting",
    marker: "new" }], at: 9,
    scan: { generation: 8, complete: true, emptyAuthoritative: false } });
retryButton = waiting.querySelector("[data-refresh]");
assert.equal(retryButton.disabled, false,
    "newer evidence re-enables the retry control after an in-flight refresh");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", marker: "old" }], at: 7,
    scan: { generation: 7, complete: true, emptyAuthoritative: false } });
assert.equal(appliedSessions.at(-1)[0].marker, "new",
    "an out-of-order generation cannot overwrite newer session evidence");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", marker: "unversioned" }], at: 10 });
assert.equal(appliedSessions.at(-1)[0].marker, "new",
    "an unversioned response cannot overwrite evidence after generation ordering is known");

LocalClient.resetSessionRefreshForTesting();
requests.length = 0;
globalThis.fetch = function (path, options) {
    requests.push({ path: path, options: options });
    return Promise.resolve(jsonResponse({ ok: true, state: "throttled", accepted: false,
        coalesced: false, throttled: true, scan: { generation: 8 } }));
};
const timedOut = LocalClient.refreshSessionEvidence();
for (let turn = 0; turn < 8; turn += 1) await Promise.resolve();
const timeoutTimer = refreshTimers.at(-1);
assert.ok(timeoutTimer && timeoutTimer.ms > 0, "refresh evidence installs a bounded timeout");
timeoutTimer.fn();
assert.equal((await timedOut).state, "timed_out",
    "a bounded wait reports that no newer reading arrived");
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, false,
    "a timed-out refresh is retryable rather than permanently disabled");
assert.match(waiting.querySelector("[data-refresh-status]").textContent, /nothing|arriv|refresh/i,
    "a bounded timeout visibly reports that the attempt found no newer reading");

LocalClient.resetSessionRefreshForTesting();
let failedRefreshFetches = 0;
globalThis.fetch = function () {
    failedRefreshFetches += 1;
    return Promise.resolve(jsonResponse({ error: { code: "refresh_failed",
        message: "refresh failed" } }, false));
};
await assert.rejects(LocalClient.refreshSessionEvidence(), /refresh failed/,
    "a failed refresh POST rejects instead of being swallowed as success");
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, false,
    "a failed refresh POST re-enables the retry control");

const callsBeforeDisconnectedRetry = failedRefreshFetches;
LocalClient.es = null;
await assert.rejects(LocalClient.refreshSessionEvidence(), /reach|connect|offline/i,
    "a disconnected evidence retry refuses locally instead of stacking a 401 over the door");
assert.equal(failedRefreshFetches, callsBeforeDisconnectedRetry,
    "a disconnected evidence retry sends no request behind the door");

LocalClient.resetSessionRefreshForTesting();
let answerAfterDoor;
let requestsBehindDoor = 0;
globalThis.fetch = function () {
    requestsBehindDoor += 1;
    return new Promise(function (resolve) { answerAfterDoor = resolve; });
};
LocalClient.es = { close: function () {} };
const retiredByDoor = LocalClient.refreshSessionEvidence();
LocalClient.stop();
assert.equal((await retiredByDoor).state, "stopped",
    "raising the auth door retires an in-flight evidence retry without a rejection toast");
answerAfterDoor(jsonResponse({ error: { code: "unauthorized", message: "unauthorized" } }, false));
for (let turn = 0; turn < 8; turn += 1) await Promise.resolve();
assert.equal(LocalClient.sessionRefreshEvidenceState().status, "idle",
    "a late 401 cannot revive or fail the retry after the door owns the page");
assert.equal(requestsBehindDoor, 1,
    "auth stop does not chain a second refresh request behind the door");
handlers.sessions = originalSessionsHandler;
handlers.conn = originalConnectionHandler;

const { CloudClient } = await import("./cloud-client.js");
assert.equal(typeof CloudClient.prototype.refreshSessionEvidence, "undefined",
    "the Cloud reconnect client does not inherit the local inventory-evidence operation");
useApi({ focus: function () { return Promise.resolve(); } });
document.dispatchEvent(new CustomEvent("clawdline:session-refresh"));
assert.equal(waiting.querySelector("[data-refresh]"), null,
    "a transport without local inventory evidence renders no ambiguous refresh control");
const canonicalChallenge = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
const malformedChallenges = [
    {
        name: "missing expiry",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01", challenge: canonicalChallenge }
    },
    {
        name: "zero expiry",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01", challenge: canonicalChallenge,
            expires_in_ms: 0 }
    },
    {
        name: "negative expiry",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01", challenge: canonicalChallenge,
            expires_in_ms: -1 }
    },
    {
        name: "unsafe expiry",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01", challenge: canonicalChallenge,
            expires_in_ms: Number.MAX_SAFE_INTEGER + 1 }
    },
    {
        name: "fractional expiry",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01", challenge: canonicalChallenge,
            expires_in_ms: 1.5 }
    },
    {
        name: "noncanonical base64",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01",
            challenge: canonicalChallenge.slice(0, -1), expires_in_ms: 30_000 }
    },
    {
        name: "31-byte challenge",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01",
            challenge: Buffer.alloc(31).toString("base64"), expires_in_ms: 30_000 }
    },
    {
        name: "33-byte challenge",
        frame: { type: "challenge", v: 1, context: "clawdline-challenge-v1",
            account: "account-01", device: "device-vector-01",
            challenge: Buffer.alloc(33).toString("base64"), expires_in_ms: 30_000 }
    }
];

const originalSign = crypto.subtle.sign;
let malformedChallengeSignatures = 0;
crypto.subtle.sign = async function () {
    malformedChallengeSignatures += 1;
    return new Uint8Array(64);
};
try {
    for (const malformed of malformedChallenges) {
        const validationCloud = new CloudClient({
            relayURL: "wss://relay.example", deviceToken: "token",
            devicePrivateKey: { extractable: false }
        });
        await assert.rejects(validationCloud._receive(JSON.stringify(malformed.frame)),
            function (error) { return error.code === "bad_challenge"; },
            malformed.name + " is rejected as bad_challenge");
    }
} finally {
    crypto.subtle.sign = originalSign;
}
assert.equal(malformedChallengeSignatures, 0,
    "malformed challenges are rejected before invoking the device signing key");

const cloud = new CloudClient({
    relayURL: "wss://relay.example", deviceToken: "token", devicePrivateKey: signingKey,
    masterKey: masterKey, senderKeys: { "device-vector-01": senderKey }
});
assert.equal(assertClawdlineClient(cloud), cloud, "CloudClient satisfies the same seam");
await assert.rejects(cloud.send({ machine: "mac-01", session: "session-01" }, "hi", []),
    function (error) { return error.code === "cloud_read_only"; },
    "cloud ctl is feature-gated off by default");
assert.throws(function () {
    return new CloudClient({
        relayURL: "wss://relay.example", deviceToken: "token", devicePrivateKey: signingKey,
        masterKey: masterKey, allowWrites: true
    });
}, /durable nextSequence/, "write mode cannot silently reset its replay sequence on reload");

class FakeWebSocket {
    static latest = null;
    constructor(url, protocols) {
        this.url = url;
        this.protocols = protocols;
        this.readyState = 1;
        this.sent = [];
        this.sentText = [];
        FakeWebSocket.latest = this;
    }
    send(text) { this.sentText.push(text); this.sent.push(JSON.parse(text)); }
    close() { this.readyState = 3; if (this.onclose) this.onclose(); }
    receive(frame) { this.onmessage({ data: JSON.stringify(frame) }); }
}

const liveEvents = [];
const connectedCloud = new CloudClient({
    relayURL: "https://relay.example", deviceToken: "jwt", devicePrivateKey: signingKey,
    masterKey: masterKey, senderKeys: { "device-vector-01": senderKey },
    WebSocket: FakeWebSocket
});
connectedCloud.events(function (event) { liveEvents.push(event); });
await connectedCloud.start();
const fakeSocket = FakeWebSocket.latest;
assert.deepEqual(fakeSocket.protocols, ["clawdline.v1", "clawdline.token.jwt"]);
fakeSocket.receive({ type: "challenge", v: 1, context: "clawdline-challenge-v1",
    account: "account-01", device: "device-vector-01", challenge: canonicalChallenge,
    expires_in_ms: 30_000 });
await connectedCloud.messageChain;
assert.equal(fakeSocket.sent[0].type, "hello", "CloudClient signs the relay challenge");
fakeSocket.receive({ type: "ready", v: 1, role: "viewer", account: "account-01",
    device: "device-vector-01" });
await connectedCloud.messageChain;
const eventCountBeforePing = liveEvents.length;
fakeSocket.receive({ type: "ping" });
await connectedCloud.messageChain;
assert.equal(fakeSocket.sentText.at(-1), '{"type":"pong"}',
    "an inbound relay ping receives the exact protocol pong response");
assert.equal(liveEvents.slice(eventCountBeforePing).some(function (event) {
    return event.type === "error" && event.error && event.error.code === "bad_frame";
}), false, "an inbound relay ping does not become bad_frame");
const snapshotEnvelope = await sealEnvelope({
    ch: "s/mac-01/session-01", seq: 10, ts: 1787817600000, class: "stream",
    key_id: "ms-1", sender: "device-vector-01"
}, JSON.stringify({ id: "session-01", label: "cloud session" }), masterKey, signingKey);
fakeSocket.receive({ type: "envelope", realign: true, envelope: snapshotEnvelope });
await connectedCloud.messageChain;
assert.deepEqual((await connectedCloud.sessions()).sessions[0].identity,
    { machine: "mac-01", session: "session-01" },
    "a verified cloud snapshot is stored under (machine, session)");
assert.equal(liveEvents.some(function (event) { return event.type === "sessions"; }), true);
connectedCloud.stop();

console.log("web cloud client tests passed: golden vectors, mutations, identity, heartbeat, challenge, local seam");
process.exit(0);
