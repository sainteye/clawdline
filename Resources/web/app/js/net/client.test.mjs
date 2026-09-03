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
const composerCSS = await readFile(new URL("../../css/composer.css", import.meta.url), "utf8");
assert.match(composerCSS, /\.refresh-status\s*\+\s*\.go\s*\{/,
    "the spacing selector matches the status followed by the Show on Mac action");
assert.doesNotMatch(composerCSS, /\.go\s*\+\s*\.go\s*\{/,
    "the dead adjacent-button selector cannot return unnoticed");
assert.match(composerCSS, /\.go:hover:not\(:disabled\):not\(\[aria-disabled="true"\]\)/,
    "an aria-disabled focused retry cannot regain an active hover style");
assert.match(composerCSS, /\.go:disabled,\s*\.composer \.waiting \.go\[aria-disabled="true"\]/,
    "native and focus-preserving disabled actions share the same visual state");
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
    const ariaDisabled = /aria-disabled="([^"]*)"/.exec(tag);
    return {
        disabled: /(?:^|\s)disabled(?:\s|>)/.test(tag), dataset: dataset,
        ariaLabel: aria ? aria[1] : "",
        ariaBusy: ariaBusy ? ariaBusy[1] : null,
        ariaDisabled: ariaDisabled ? ariaDisabled[1] : null,
        closest: function (selector) {
            return selector === "[data-" + kind + "]" ? this : null;
        },
        focus: function () { globalThis.document.activeElement = this; }
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
            const replacedFocusedRefresh = target.refreshButton && globalThis.document &&
                globalThis.document.activeElement === target.refreshButton;
            target._innerHTML = value;
            target.refreshButton = null;
            target.refreshStatus = null;
            if (replacedFocusedRefresh) globalThis.document.activeElement = globalThis.document.body;
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
function bounded(promise, name, ms) {
    ms = ms || 250;
    var timer;
    return Promise.race([
        promise,
        new Promise(function (_, reject) {
            timer = setTimeout(function () { reject(new Error(name)); }, ms);
        })
    ]).finally(function () { clearTimeout(timer); });
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
    scan: { generation: 7, complete: true, emptyAuthoritative: false,
        completed: { sequence: 40, complete: true } } });
renderWaiting();
let retryButton = waiting.querySelector("[data-refresh]");
assert.ok(retryButton, "a local no-menu waiting card renders the retry control");
assert.equal(retryButton.ariaLabel, "Refresh",
    "the retry control is named for its action rather than the session state");
retryButton.focus();
waiting.dispatchClick(retryButton);
retryButton = waiting.querySelector("[data-refresh]");
assert.equal(retryButton.disabled, false,
    "the busy retry remains in the keyboard focus order");
assert.equal(retryButton.ariaDisabled, "true",
    "the retry control exposes its semantic disabled state while evidence is in flight");
assert.equal(retryButton.ariaBusy, "true", "the retry control exposes live busy progress");
assert.equal(document.activeElement, retryButton,
    "the busy rerender restores keyboard focus to the replacement retry control");
assert.match(waiting.querySelector("[data-refresh-status]").textContent, /refresh/i,
    "the polite live status announces that fresh evidence is being requested");

// A changed question rebuilds waiting.innerHTML and therefore creates another button node.
S.sessions[0].menu = { question: "The question arrived without rows", options: [] };
renderWaiting();
const rebuiltRetryButton = waiting.querySelector("[data-refresh]");
assert.notEqual(rebuiltRetryButton, retryButton, "question evidence really rebuilt the control");
assert.equal(rebuiltRetryButton.ariaDisabled, "true",
    "a rebuilt retry control remains semantically disabled while the first request is in flight");
waiting.dispatchClick(rebuiltRetryButton);
assert.equal(requests.filter(function (request) {
    return request.path === "/v1/sessions/refresh";
}).length, 1, "press, rerender, press still starts only one refresh request");
const directSingleFlight = LocalClient.refreshSessionEvidence();
assert.equal(directSingleFlight, LocalClient.sessionRefreshEvidence.promise,
    "the LocalClient single-flight layer directly returns its one in-flight promise");
assert.equal(requests.filter(function (request) {
    return request.path === "/v1/sessions/refresh";
}).length, 1, "the LocalClient single-flight layer alone prevents a second POST");
S.sessions.push({ id: "WAITING-TWO", state: "waiting", menu: null });
S.openId = "WAITING-TWO";
renderWaiting();
assert.equal(waiting.querySelector("[data-refresh]").ariaDisabled, "true",
    "global inventory refresh state remains busy when a different session card is rendered");
assert.equal(requests.filter(function (request) {
    return request.path === "/v1/sessions/refresh";
}).length, 1, "rendering another session does not create per-session refresh work");
S.openId = "WAITING-ONE";
S.sessions.pop();
renderWaiting();

answerRefresh(jsonResponse({ ok: true, state: "accepted", accepted: true,
    coalesced: false, throttled: false, scan: { completed: { sequence: 40 } } }));
for (let turn = 0; turn < 8; turn += 1) await Promise.resolve();
assert.equal(LocalClient.sessionRefreshEvidenceState().baseline, 40,
    "the acknowledgement completion baseline is installed before later evidence is sampled");
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, true,
    "an acknowledgement alone does not complete a refresh");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", state: "waiting" }], at: 8,
    scan: { generation: 80, complete: true, emptyAuthoritative: false,
        completed: { sequence: 40, complete: true } } });
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, true,
    "unrelated content generation does not complete this refresh");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", state: "waiting",
    marker: "new" }], at: 9,
    scan: { generation: 80, complete: true, emptyAuthoritative: false,
        completed: { sequence: 41, complete: true } } });
retryButton = waiting.querySelector("[data-refresh]");
assert.equal(retryButton.disabled, false,
    "newer evidence re-enables the retry control after an in-flight refresh");
assert.equal(LocalClient.sessionRefreshEvidenceState().status, "complete",
    "an unchanged successful scan settles promptly from its completion receipt");
assert.equal(document.activeElement, retryButton,
    "the completed rerender restores focus to the retry action");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", marker: "newer-content" }],
    at: 10, scan: { generation: 81, complete: true, emptyAuthoritative: false,
        completed: { sequence: 40, complete: false } } });
assert.equal(LocalClient.completedScanSequence, 41,
    "a lower completion sequence cannot overwrite newer completion evidence");
assert.equal(LocalClient.completedScanComplete, true,
    "a lower completion sequence cannot turn a successful receipt into failure");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", marker: "unsafe-completion" }],
    at: 11, scan: { generation: 82, complete: true, emptyAuthoritative: false,
        completed: { sequence: Number.MAX_SAFE_INTEGER + 1, complete: false } } });
assert.equal(LocalClient.completedScanSequence, 41,
    "an unsafe completion sequence cannot overwrite safe completion evidence");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", marker: "old" }], at: 7,
    scan: { generation: 7, complete: true, emptyAuthoritative: false,
        completed: { sequence: 39, complete: false } } });
assert.equal(appliedSessions.at(-1)[0].marker, "unsafe-completion",
    "an out-of-order generation cannot overwrite newer session evidence");
await LocalClient.receiveSessions({ sessions: [{ id: "WAITING-ONE", marker: "unversioned" }], at: 10 });
assert.equal(appliedSessions.at(-1)[0].marker, "unsafe-completion",
    "an unversioned response cannot overwrite evidence after generation ordering is known");

LocalClient.resetSessionRefreshForTesting();
requests.length = 0;
globalThis.fetch = function (path, options) {
    requests.push({ path: path, options: options });
    return Promise.resolve(jsonResponse({ ok: true, state: "throttled", accepted: false,
        coalesced: false, throttled: true, scan: { completed: { sequence: 41 } } }));
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
assert.equal(waiting.querySelector("[data-refresh-status]").textContent, "Request failed",
    "a bounded timeout reports the action failure rather than blaming an empty stream");

LocalClient.resetSessionRefreshForTesting();
globalThis.fetch = function () {
    return Promise.resolve(jsonResponse({ ok: true, state: "accepted", accepted: true,
        coalesced: false, throttled: false, scan: { completed: { sequence: 41 } } }));
};
const incompleteScan = LocalClient.refreshSessionEvidence();
for (let turn = 0; turn < 8; turn += 1) await Promise.resolve();
await LocalClient.receiveSessions({ sessions: S.sessions, at: 10,
    scan: { generation: 83, complete: true, emptyAuthoritative: false,
        completed: { sequence: 42, complete: false } } });
assert.equal((await incompleteScan).state, "failed",
    "a completed but incomplete inventory reports failure rather than success");
assert.equal(LocalClient.sessionRefreshEvidenceState().busy, false,
    "an incomplete inventory leaves the global action retryable");
assert.equal(waiting.querySelector("[data-refresh-status]").textContent, "Request failed",
    "an incomplete inventory is announced as a failed action");

for (const badCompleted of [
    { sequence: Number.MAX_SAFE_INTEGER + 1 }, { sequence: -1 }, { sequence: 42.5 }, {}
]) {
    LocalClient.resetSessionRefreshForTesting();
    globalThis.fetch = function () {
        return Promise.resolve(jsonResponse({ ok: true, state: "accepted", accepted: true,
            coalesced: false, throttled: false, scan: { completed: badCompleted } }));
    };
    await assert.rejects(bounded(LocalClient.refreshSessionEvidence(),
        "unsafe refresh acknowledgement did not settle within 250ms"), function (error) {
        return error && error.code === "bad_refresh_ack";
    }, "unsafe or unversioned completion baselines are rejected");
}

LocalClient.resetSessionRefreshForTesting();
let inconsistentAnswer;
globalThis.fetch = function () {
    return new Promise(function (resolve) { inconsistentAnswer = resolve; });
};
const inconsistentAck = LocalClient.refreshSessionEvidence();
inconsistentAnswer(jsonResponse({ ok: true, state: "accepted", accepted: false,
    coalesced: true, throttled: false, scan: { completed: { sequence: 42 } } }));
await assert.rejects(bounded(inconsistentAck,
    "inconsistent refresh acknowledgement did not settle within 250ms"), function (error) {
    return error && error.code === "bad_refresh_ack";
}, "acknowledgement state and Boolean disagreement is rejected");

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
const retiredByDoorResult = await bounded(retiredByDoor,
    "session refresh stop did not settle within 250ms");
assert.equal(retiredByDoorResult.state, "stopped",
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

/* ---- the two reads a browser on the cloud path can now make ---------------
   The channel and the subscription were always here; nothing ever published on them, so a
   transcript request settled for nobody and Info was not a method at all. These hold the whole
   round trip: what goes up the command channel, what comes back on the session's own channel,
   and what a refusal looks like when it arrives instead of an answer. ----------------------- */

const readOnlyCloud = new CloudClient({
    relayURL: "wss://relay.example", deviceToken: "token", devicePrivateKey: signingKey,
    masterKey: masterKey, senderKeys: { "device-vector-01": senderKey }
});
for (const [name, call] of [
    ["transcript", () => readOnlyCloud.transcript({ machine: "mac-01", session: "session-01" })],
    ["info", () => readOnlyCloud.info({ machine: "mac-01", session: "session-01" })],
    ["infoSummary", () => readOnlyCloud.infoSummary({ machine: "mac-01", session: "session-01" })],
    // A picture asks on `ctl/` like everything else, so a device that may read but not publish
    // cannot ask for one. It is refused in the same word rather than left to draw a broken icon.
    ["image", () => readOnlyCloud.image({ machine: "mac-01", session: "session-01" },
                                        "11111111-2222-4333-8444-555555555555")]
]) {
    await assert.rejects(call(), function (error) {
        return error.code === "cloud_read_needs_send_prompt";
    }, name + " on a device without send_prompt is refused in a word of its own");
}
assert.equal(readOnlyCloud.readWaiters.size, 0,
    "a refused read registers no waiter and spends no envelope sequence");

// The other four are reads now and not stubs, so they meet the same relay rule the first two
// do. `cloud_read_unavailable` has to be gone as well as `is not a function`: a word that still
// answered here would be a gap that had been closed and not said so.
for (const [name, call] of [
    ["agent", () => readOnlyCloud.agent({ machine: "mac-01", session: "session-01" }, "agent-1")],
    ["shell", () => readOnlyCloud.shell({ machine: "mac-01", session: "session-01" }, "shell-1")],
    ["skills", () => readOnlyCloud.skills({ machine: "mac-01", session: "session-01" })],
    ["git", () => readOnlyCloud.git({ machine: "mac-01", session: "session-01" })]
]) {
    await assert.rejects(call(), function (error) {
        return error.code === "cloud_read_needs_send_prompt";
    }, name + " is a read the relay may refuse, not a reading this transport lacks");
}
// An agent or shell read with no id is refused here rather than at the Mac, in the Mac's own
// word: `serveRead` would call it `malformed_read` and publish nothing, so a request sent anyway
// would be dropped in silence and end sixty seconds later as a timeout.
for (const [name, call] of [
    ["agent", (id) => readOnlyCloud.agent({ machine: "mac-01", session: "session-01" }, id)],
    ["shell", (id) => readOnlyCloud.shell({ machine: "mac-01", session: "session-01" }, id)]
]) {
    for (const empty of [undefined, null, ""]) {
        await assert.rejects(call(empty), function (error) {
            return error.code === "malformed_read";
        }, name + "() with no id is refused before the relay rule, in the Mac's own word");
    }
}
assert.equal(readOnlyCloud.readWaiters.size, 0,
    "and neither refusal leaves a waiter behind to be settled by somebody else's answer");

const readTimers = [];
/** Sealing an envelope is real WebCrypto, so a published read is several turns away, not two. */
async function until(predicate, what) {
    for (let turn = 0; turn < 500; turn += 1) {
        if (predicate()) return;
        await new Promise(function (resolve) { setImmediate(resolve); });
    }
    assert.fail("timed out waiting for " + what);
}
function publishedReads(socket) {
    return socket.sent.filter(function (frame) { return frame.type === "publish"; });
}

/** A real `WebSocket.close()` fires `onclose` as a later task, never inside the call. */
class DeferredCloseWebSocket extends FakeWebSocket {
    close() {
        this.readyState = 3;
        queueMicrotask(() => { if (this.onclose) this.onclose(); });
    }
}
function makeReadingCloud(socketClass = FakeWebSocket) {
    return new CloudClient({
        relayURL: "https://relay.example", deviceToken: "jwt", devicePrivateKey: signingKey,
        masterKey: masterKey, senderKeys: { "device-vector-01": senderKey },
        WebSocket: socketClass, allowWrites: true, nextSequence: function () {
            return Promise.resolve(readTimers.length + 900);
        },
        setTimeout: function (fn) { readTimers.push(fn); return readTimers.length; },
        clearTimeout: function () {}
    });
}
async function becomeReady(client) {
    await client.start();
    const socket = FakeWebSocket.latest;
    socket.receive({ type: "challenge", v: 1, context: "clawdline-challenge-v1",
        account: "account-01", device: "device-vector-01", challenge: canonicalChallenge,
        expires_in_ms: 30_000 });
    await client.messageChain;
    socket.receive({ type: "ready", v: 1, role: "viewer", account: "account-01",
        device: "device-vector-01" });
    await client.messageChain;
    return socket;
}
async function answerRead(client, socket, payload, session = "session-01") {
    const envelope = await sealEnvelope({
        ch: "t/mac-01/" + session, seq: 4000 + readTimers.length, ts: 1787817600000,
        class: "stream", key_id: "ms-1", sender: "device-vector-01"
    }, JSON.stringify(payload), masterKey, signingKey);
    socket.receive({ type: "envelope", envelope: envelope });
    await client.messageChain;
}

const readingCloud = makeReadingCloud();
const readingSocket = await becomeReady(readingCloud);
const transcriptAnswer = readingCloud.transcript({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(readingSocket).length === 1; },
    "the transcript request to leave");
const subscribed = readingSocket.sent.filter((frame) => frame.type === "subscribe");
assert.deepEqual(subscribed.at(-1).channels, ["t/mac-01/session-01"],
    "asking for a transcript subscribes to the session's own answer channel");
const published = publishedReads(readingSocket);
assert.equal(published.length, 1, "one read is one published envelope");
assert.equal(published[0].envelope.ch, "ctl/mac-01",
    "a read asks on the command channel, the only one a viewer may publish on");
assert.equal(published[0].envelope.class, "ctl",
    "a read is not a dispatch and is not billed as one");
assert.deepEqual(JSON.parse(new TextDecoder().decode(
    await openEnvelope(published[0].envelope, masterKey, senderKey))),
    { type: "transcript", session: "session-01", limit: 200 },
    "the transcript request carries the same window the direct path asks for");
await answerRead(readingCloud, readingSocket,
    { read: "transcript", status: 200, body: { messages: [{ role: "user", text: "hi" }] } });
assert.deepEqual(await transcriptAnswer, { messages: [{ role: "user", text: "hi" }] },
    "the answer on t/ settles the transcript the direct path would have fetched");

const infoAnswer = readingCloud.info({ machine: "mac-01", session: "session-01" });
const summaryAnswer = readingCloud.infoSummary({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(readingSocket).length === 3; },
    "both Info requests to leave");
const infoRequests = await Promise.all(publishedReads(readingSocket).slice(1)
    .map(async (frame) => JSON.parse(new TextDecoder().decode(
        await openEnvelope(frame.envelope, masterKey, senderKey)))));
// Compared as a set. Both requests are sealed concurrently and either may reach the socket
// first; asserting an order here is asserting which WebCrypto call finished, which is not a
// contract and does not always hold.
assert.deepEqual(infoRequests.map((request) => request.parts).sort(), ["full", "summary"],
    "full and summary Info are two requests, told apart by the field the direct path uses");
assert.deepEqual(infoRequests.map((request) => [request.type, request.session]),
    [["info", "session-01"], ["info", "session-01"]],
    "and both are the info read for the session that was asked about");
await answerRead(readingCloud, readingSocket,
    { read: "info.full", status: 200, body: { info: { session: { id: "session-01" } } } });
assert.deepEqual((await infoAnswer).info.session, { id: "session-01" },
    "Info comes back in the same envelope shape the /info route answers with");
// The summary read is still outstanding: `info` and `info?parts=summary` are two questions and
// one answer does not settle both. Firing its own clock is the only thing that ends it.
readTimers[2]();
await assert.rejects(summaryAnswer, function (error) {
    return error.code === "cloud_read_timeout";
}, "a full Info answer does not settle a summary request that omits three of its parts");

const refusedInfo = readingCloud.info({ machine: "mac-01", session: "gone" });
await until(function () { return publishedReads(readingSocket).length === 4; },
    "the refused Info request to leave");
await answerRead(readingCloud, readingSocket, { read: "info.full", status: 404,
    error: { code: "not_found", message: "No session named that" } }, "gone");
await assert.rejects(refusedInfo, function (error) {
    return error.code === "not_found" && error.message === "No session named that";
}, "a refused read arrives as the Mac's own typed code, not as an empty view");

const unansweredRead = readingCloud.transcript({ machine: "mac-01", session: "slow" });
await until(function () { return publishedReads(readingSocket).length === 5; },
    "the unanswered read to leave");
readTimers.at(-1)();
await assert.rejects(unansweredRead, function (error) {
    return error.code === "cloud_read_timeout";
}, "a read nothing ever answers ends in a code rather than in a skeleton");

// The request has to be **on the wire** before the socket is stopped, and the count it waits for
// has to be exact. Waiting for "more than before" left `_send` to throw `offline` on a socket
// closed out from under a request that had never left — the right code for the wrong reason —
// and this check stayed green with `_failAllReads` deleted from both `stop()` and `onclose`,
// which is precisely the leak it exists to catch.
const orphaned = readingCloud.info({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(readingSocket).length === 6; },
    "the orphaned read to reach the wire before its socket is stopped");
readingCloud.stop();
await assert.rejects(orphaned, function (error) { return error.code === "offline"; },
    "stopping the socket fails the reads that were waiting on it");
assert.equal(readingCloud.readWaiters.size, 0, "no read waiter outlives its transport");

// And a socket that goes on its own, which is the ordinary case rather than the deliberate one.
// `stop()` and `onclose` are two paths and each has to sweep: with the sweep left only in
// `stop()`, every check above still passed while a dropped connection stranded its reads.
const dropCloud = makeReadingCloud();
const dropSocket = await becomeReady(dropCloud);
const dropped = dropCloud.transcript({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(dropSocket).length === 1; },
    "the read to leave before the relay drops the socket");
dropSocket.close();
await assert.rejects(dropped, function (error) { return error.code === "offline"; },
    "a socket the relay closes fails the reads that were waiting on it");
assert.equal(dropCloud.readWaiters.size, 0, "a dropped connection leaves no read waiter behind");

// And `stop()` sweeps for itself rather than leaning on the close it has just asked for. A real
// socket's `onclose` is a later task, so `refresh()` — stop then start — would otherwise carry
// live read waiters across the gap and settle them against the wrong connection.
const deferredCloud = makeReadingCloud(DeferredCloseWebSocket);
const deferredSocket = await becomeReady(deferredCloud);
const acrossRefresh = deferredCloud.transcript({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(deferredSocket).length === 1; },
    "the read to leave before the transport is stopped");
deferredCloud.stop();
assert.equal(deferredCloud.readWaiters.size, 0,
    "stop() settles its reads before returning, without waiting for onclose to be scheduled");
await assert.rejects(acrossRefresh, function (error) { return error.code === "offline"; },
    "and the read it settled says the connection went, not that the Mac refused");

/* ---- and the other four, which used to be a TypeError -------------------------------------
   The Git panel is the one the person opens most, so it is the one carried furthest here: its
   body, its typed refusal, and the branch `git-panel.js` already takes on that refusal. The
   decisive check is the pair of agents: one session, one answer channel, two open conversations,
   which is the ordinary case rather than the contrived one. ---------------------------------- */

const panelCloud = makeReadingCloud();
const panelSocket = await becomeReady(panelCloud);
async function requestBody(frame) {
    return JSON.parse(new TextDecoder().decode(
        await openEnvelope(frame.envelope, masterKey, senderKey)));
}

const gitAnswer = panelCloud.git({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(panelSocket).length === 1; },
    "the Git request to leave");
assert.deepEqual(await requestBody(publishedReads(panelSocket)[0]),
    { type: "git", session: "session-01" },
    "the Git panel asks for a route with no window, because its route answers rows and no diff");
await answerRead(panelCloud, panelSocket, { read: "git", status: 200,
    body: { git: { branch: "main", head: "abc1234", clean: true, files: [] } } });
assert.deepEqual((await gitAnswer).git.branch, "main",
    "the Git panel gets the same body it gets over the tunnel, so it needs no cloud branch");

const notARepo = panelCloud.git({ machine: "mac-01", session: "plain" });
await until(function () { return publishedReads(panelSocket).length === 2; },
    "the second Git request to leave");
await answerRead(panelCloud, panelSocket, { read: "git", status: 404,
    error: { code: "not_a_repo", message: "That session is not inside a Git repository" } },
    "plain");
await assert.rejects(notARepo, function (error) { return error.code === "not_a_repo"; },
    "and the code git-panel.js already branches on arrives as the Mac's own word");

const skillsAnswer = panelCloud.skills({ machine: "mac-01", session: "session-01" });
await until(function () { return publishedReads(panelSocket).length === 3; },
    "the skills request to leave");
assert.deepEqual(await requestBody(publishedReads(panelSocket)[2]),
    { type: "skills", session: "session-01" }, "skills is the session and nothing else");
await answerRead(panelCloud, panelSocket, { read: "skills", status: 200,
    body: { skills: [{ name: "/run", description: "run it", source: "project" }] } });
assert.deepEqual((await skillsAnswer).skills[0].name, "/run",
    "the composer reads answer.skills exactly as it does on the direct path");

const shellAnswer = panelCloud.shell({ machine: "mac-01", session: "session-01" }, "shell-9");
await until(function () { return publishedReads(panelSocket).length === 4; },
    "the shell request to leave");
assert.deepEqual(await requestBody(publishedReads(panelSocket)[3]),
    { type: "shell", session: "session-01", shell: "shell-9", bytes: 65536 },
    "a command's tail names its bound, because the direct path's omitted default is a "
    + "malformed read here");
await answerRead(panelCloud, panelSocket, { read: "shell:shell-9", status: 200,
    body: { text: "building…", ended: false, at: 1787817600, signature: "12-34" } });
assert.equal((await shellAnswer).text, "building…",
    "and the shell panel is handed the bytes in the order they were written");

// The decisive one. Two agents in one session answer on one channel, so the only thing that can
// tell their answers apart is the name — and `agent` is not a name, it is a kind.
const firstAgent = panelCloud.agent({ machine: "mac-01", session: "session-01" }, "agent-1");
// `_read` registers its clock synchronously, so the timer this read will die by is the last one
// pushed — captured before the second agent pushes its own on top of it.
const firstAgentTimer = readTimers.length - 1;
const secondAgent = panelCloud.agent({ machine: "mac-01", session: "session-01" }, "agent-2");
await until(function () { return publishedReads(panelSocket).length === 6; },
    "both agent requests to leave");
const agentRequests = await Promise.all(
    publishedReads(panelSocket).slice(4).map(requestBody));
assert.deepEqual(agentRequests.map((request) => request.agent).sort(), ["agent-1", "agent-2"],
    "two agents are two requests, each naming the agent it is about");
assert.deepEqual(agentRequests.map((request) => [request.type, request.session, request.limit]),
    [["agent", "session-01", 200], ["agent", "session-01", 200]],
    "and both ask for the window the direct path asks for");
await answerRead(panelCloud, panelSocket, { read: "agent:agent-2", status: 200,
    body: { agent: { id: "agent-2" }, entries: [{ role: "user", text: "second" }],
        signature: "9-9" } });
assert.equal((await secondAgent).agent.id, "agent-2",
    "the answer that names an agent settles that agent");
readTimers[firstAgentTimer]();
await assert.rejects(firstAgent, function (error) {
    return error.code === "cloud_read_timeout";
}, "and it does not settle the other one, whose conversation it is not");
panelCloud.stop();

const strictCloud = makeReadingCloud();
const strictSocket = await becomeReady(strictCloud);
const strictErrors = [];
strictCloud.events(function (event) { if (event.type === "error") strictErrors.push(event.error); });
await answerRead(strictCloud, strictSocket, { session: "session-01", messages: [] });
assert.equal(strictErrors.at(-1) && strictErrors.at(-1).code, "bad_payload",
    "a t/ envelope that names no read is a protocol error rather than a stored transcript");
strictCloud.stop();

/* ---- and the pictures inside a transcript ---------------------------------
   The one read whose failure was in the transport rather than in this client: a tile's `<img
   src>` is same-origin and relative, and on this path the origin is a hosted console with no
   artifact route. So the bytes come down the session's own channel instead. --------------- */

const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02, 0x03]);
/** Two pictures are answered back to back with no read starting in between, and an envelope
 *  whose sequence does not advance is a replay. `answerRead` derives its own from `readTimers`,
 *  which only grows when a read starts, so these answers count for themselves. */
let pictureSequence = 5000;
async function answerPicture(client, socket, payload, session = "session-01") {
    pictureSequence += 1;
    const envelope = await sealEnvelope({
        ch: "t/mac-01/" + session, seq: pictureSequence, ts: 1787817600000,
        class: "stream", key_id: "ms-1", sender: "device-vector-01"
    }, JSON.stringify(payload), masterKey, signingKey);
    socket.receive({ type: "envelope", envelope: envelope });
    await client.messageChain;
}
const FIRST = "11111111-2222-4333-8444-555555555555";
const SECOND = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee";
function pngAnswer(id, bytes = PNG) {
    return { read: "image." + id, status: 200,
        body: { id: id, media_type: "image/png", byte_count: bytes.length,
            data: Buffer.from(bytes).toString("base64") } };
}

// The one thing neither half's own suite can see: the Mac writes these four field names and
// this client reads them, and a rename on either side would leave both suites green while every
// picture on the cloud path came back `bad_payload`.
const bridgeSource = await readFile(
    new URL("../../../../../Sources/CloudAppBridge.swift", import.meta.url), "utf8");
const imageOutcome = bridgeSource.split("private static func imageOutcome")[1]
    .split("\n    }")[0];
for (const field of ['"id"', '"media_type"', '"byte_count"', '"data"']) {
    assert.ok(imageOutcome.includes(field),
        "the Mac's image answer carries " + field + ", which this client reads by that name");
}
assert.ok(imageOutcome.includes("base64EncodedString()"),
    "and carries the picture base64, which is what `imageAnswerBytes` decodes");
assert.ok(imageOutcome.includes("image_too_large_for_cloud")
    && imageOutcome.includes("image_media_type_unsupported"),
    "and refuses in the two codes the tile has sentences for");

const imageCloud = makeReadingCloud();
const imageSocket = await becomeReady(imageCloud);
const firstPicture = imageCloud.image({ machine: "mac-01", session: "session-01" }, FIRST);
const secondPicture = imageCloud.image({ machine: "mac-01", session: "session-01" }, SECOND);
await until(function () { return publishedReads(imageSocket).length === 2; },
    "both picture requests to leave");
const pictureRequests = await Promise.all(publishedReads(imageSocket)
    .map(async (frame) => JSON.parse(new TextDecoder().decode(
        await openEnvelope(frame.envelope, masterKey, senderKey)))));
assert.deepEqual(pictureRequests.map((request) => request.id).sort(), [SECOND, FIRST].sort(),
    "each picture is asked for by the opaque id the transcript already published");
assert.deepEqual(pictureRequests.map((request) => [request.type, request.session]),
    [["image", "session-01"], ["image", "session-01"]],
    "a picture is an image read for the session whose transcript holds it");
assert.equal(publishedReads(imageSocket)[0].envelope.ch, "ctl/mac-01",
    "a picture is asked for on the only channel a viewer may publish on");

// The second answer settles the second tile. Named `image` alone, it would settle whichever
// request was still waiting — which, for a transcript of screenshots, is the wrong picture in
// the wrong message.
await answerPicture(imageCloud, imageSocket, pngAnswer(SECOND));
const secondBytes = await secondPicture;
assert.deepEqual(Array.from(secondBytes.bytes), Array.from(PNG),
    "a picture arrives as its own bytes, ready for a blob URL");
assert.equal(secondBytes.media_type, "image/png");
assert.equal(secondBytes.id, SECOND);

await answerPicture(imageCloud, imageSocket, pngAnswer(FIRST));
assert.equal((await firstPicture).id, FIRST,
    "and the first tile is settled by the first picture, whenever it arrives");

// The bound, said in a code the tile can turn into a sentence. This is the whole difference
// between "too large to send (14.2 MB)" and a broken-image icon that reads as the reader's fault.
const oversized = imageCloud.image({ machine: "mac-01", session: "session-01" }, FIRST);
await until(function () { return publishedReads(imageSocket).length === 3; },
    "the oversized picture request to leave");
await answerPicture(imageCloud, imageSocket, { read: "image." + FIRST, status: 413,
    error: { code: "image_too_large_for_cloud", message: "over one envelope",
        byte_count: 14_000_000, limit_bytes: 12_582_132 } });
await assert.rejects(oversized, function (error) {
    return error.code === "image_too_large_for_cloud";
}, "a picture over the envelope bound refuses in its own word rather than half-arriving");

// A truncated answer is a broken PNG, which renders as exactly the icon this path exists to
// remove. It is refused here instead, where there is still a code to say it with.
const truncated = imageCloud.image({ machine: "mac-01", session: "session-01" }, SECOND);
await until(function () { return publishedReads(imageSocket).length === 4; },
    "the truncated picture request to leave");
const short = pngAnswer(SECOND);
short.body.byte_count = PNG.length + 1;
await answerPicture(imageCloud, imageSocket, short);
await assert.rejects(truncated, function (error) { return error.code === "bad_payload"; },
    "base64 that decodes to the wrong length is a truncated picture, not a picture");

const wrongType = imageCloud.image({ machine: "mac-01", session: "session-01" }, FIRST);
await until(function () { return publishedReads(imageSocket).length === 5; },
    "the wrong-media-type request to leave");
const svg = pngAnswer(FIRST);
svg.body.media_type = "image/svg+xml";
await answerPicture(imageCloud, imageSocket, svg);
await assert.rejects(wrongType, function (error) { return error.code === "bad_payload"; },
    "the media type is pinned here too, so a blob URL is never built from a claim");
imageCloud.stop();

// Pacing, which is what keeps "a transcript holds many" from meaning "forty envelopes at once".
// Nothing is refused: the fourth picture waits for a slot and then goes.
const pacedCloud = makeReadingCloud();
pacedCloud.imageReadsInFlight = 2;
const pacedSocket = await becomeReady(pacedCloud);
const paced = ["11111111-2222-4333-8444-555555555551",
    "11111111-2222-4333-8444-555555555552",
    "11111111-2222-4333-8444-555555555553"]
    .map((id) => pacedCloud.image({ machine: "mac-01", session: "session-01" }, id));
await until(function () { return publishedReads(pacedSocket).length === 2; },
    "the first two pictures to leave");
await new Promise(function (resolve) { setImmediate(resolve); });
assert.equal(publishedReads(pacedSocket).length, 2,
    "a third picture waits for a slot rather than adding a third envelope to the air");
await answerPicture(pacedCloud, pacedSocket, pngAnswer("11111111-2222-4333-8444-555555555551"));
await until(function () { return publishedReads(pacedSocket).length === 3; },
    "the third picture to leave once a slot frees");
await answerPicture(pacedCloud, pacedSocket, pngAnswer("11111111-2222-4333-8444-555555555552"));
await answerPicture(pacedCloud, pacedSocket, pngAnswer("11111111-2222-4333-8444-555555555553"));
assert.equal((await Promise.all(paced)).length, 3, "and every picture is delivered in the end");
pacedCloud.stop();

console.log("web cloud client tests passed: golden vectors, mutations, identity, heartbeat, challenge, local seam, cloud reads, transcript images");
process.exit(0);
