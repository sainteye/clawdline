/**
 * The hosted console's boot: which transport it is, and everything the cloud one has to do
 * before a relay socket is worth opening.
 *
 * The three things this exists to keep honest:
 *
 *   * **The Mac-served page must not change.** `localhost` and `?mock=1` are the two transports
 *     this app has always had, and the tunnel hostname is not localhost either. A cloud console
 *     is a build declaration, never a hostname guess.
 *   * **The control plane's refusals are typed, and one of them is not an error.** `POST
 *     /v1/pairing/claim` answers `202 pairing_pending` while the Mac has not answered yet
 *     (`api/src/services/pairing.ts`), which is the ordinary polling state — reading it as a
 *     failure would make pairing impossible rather than slow.
 *   * **Revocation is terminal and everything else is not.** A device whose row was revoked
 *     must stop, not reconnect on a timer.
 */
import assert from "node:assert/strict";

const boot = await import("../Resources/web/app/js/net/cloud-boot.js");

/* ---- fakes ---------------------------------------------------------------- */

function fakeStorage(initial) {
    const map = new Map(Object.entries(initial || {}));
    return {
        getItem: function (key) { return map.has(key) ? map.get(key) : null; },
        setItem: function (key, value) { map.set(key, String(value)); },
        removeItem: function (key) { map.delete(key); },
        map: map
    };
}

/** Enough IndexedDB for `cloud-crypto.js`'s two helpers, and no more. */
function fakeIndexedDB() {
    const store = new Map();
    return {
        store: store,
        open: function () {
            const request = { result: null, error: null };
            queueMicrotask(function () {
                request.result = {
                    objectStoreNames: { contains: function () { return true; } },
                    createObjectStore: function () { },
                    close: function () { },
                    transaction: function () {
                        const tx = { error: null };
                        const object = {
                            put: function (value, key) {
                                store.set(key, value);
                                queueMicrotask(function () { if (tx.oncomplete) tx.oncomplete(); });
                            },
                            get: function (key) {
                                const get = { result: store.get(key) };
                                queueMicrotask(function () {
                                    if (get.onsuccess) get.onsuccess();
                                    if (tx.oncomplete) tx.oncomplete();
                                });
                                return get;
                            }
                        };
                        tx.objectStore = function () { return object; };
                        return tx;
                    }
                };
                if (request.onupgradeneeded) request.onupgradeneeded();
                if (request.onsuccess) request.onsuccess();
            });
            return request;
        }
    };
}

function response(status, body) {
    return {
        status: status,
        ok: status >= 200 && status < 300,
        json: function () { return Promise.resolve(body); }
    };
}

/** Routes by "METHOD path"; a handler may be a value or a function of the call index. */
function fakeFetch(routes) {
    const calls = [];
    const counts = new Map();
    const impl = function (url, init) {
        const method = (init && init.method) || "GET";
        const path = new URL(url).pathname;
        const key = method + " " + path;
        calls.push({ key: key, url: url, init: init });
        const index = counts.get(key) || 0;
        counts.set(key, index + 1);
        const handler = routes[key];
        if (handler === undefined) throw new Error("no fake route for " + key);
        const value = typeof handler === "function" ? handler(index) : handler;
        return Promise.resolve(value);
    };
    impl.calls = calls;
    return impl;
}

const CONFIG = {
    v: 1,
    app_origin: "https://app.clawdline.com",
    api_origin: "https://api.clawdline.com",
    relay_url: "wss://relay.clawdline.com/v1/connect"
};

/* ---- the build declaration ------------------------------------------------ */

assert.equal(boot.readCloudConfig({}), null, "a page with no declaration is not a cloud console");
const config = boot.readCloudConfig({ __clawdlineCloud: CONFIG });
assert.equal(config.appOrigin, "https://app.clawdline.com");
assert.equal(config.apiOrigin, "https://api.clawdline.com");
assert.equal(config.relayURL, "wss://relay.clawdline.com/v1/connect");

for (const [field, value, why] of [
    ["v", 2, "an unknown declaration version"],
    ["api_origin", "http://api.clawdline.com", "a plaintext API origin"],
    ["relay_url", "https://relay.clawdline.com/v1/connect", "a relay that is not wss"],
    ["app_origin", "", "a missing app origin"]
]) {
    assert.throws(function () {
        boot.readCloudConfig({ __clawdlineCloud: { ...CONFIG, [field]: value } });
    }, function (error) { return error.code === "bad_cloud_config"; },
    why + " is refused at boot rather than three requests later");
}

/* ---- which transport ------------------------------------------------------ */

assert.equal(boot.chooseTransport({ mock: true, origin: "https://app.clawdline.com", config: config }),
    "mock", "?mock=1 still wins over everything");
assert.equal(boot.chooseTransport({ mock: false, origin: "http://127.0.0.1:7717", config: null }),
    "local", "the Mac-served page keeps talking to the Mac");
assert.equal(boot.chooseTransport({
    mock: false, origin: "https://xyz.trycloudflare.com", config: null
}), "local", "and so does the same page reached through the tunnel");
assert.equal(boot.chooseTransport({
    mock: false, origin: "https://app.clawdline.com", config: config
}), "cloud", "the hosted console selects the cloud transport");
assert.equal(boot.chooseTransport({
    mock: false, origin: "https://someone-else.pages.dev", config: config
}), "blocked", "a hosted bundle served from the wrong origin refuses rather than half-working");

/* ---- the durable outbound sequence ---------------------------------------- */

const sequenceStore = fakeStorage();
const first = boot.durableSequence(sequenceStore, "seq");
assert.equal(first(), 0, "the first sequence is zero");
assert.equal(first(), 1, "and they advance by one");
const reserved = Number(sequenceStore.getItem("seq"));
assert.ok(reserved > 1, "the ceiling written down is ahead of what was handed out");

const afterReload = boot.durableSequence(sequenceStore, "seq");
assert.ok(afterReload() >= reserved,
    "a reload never hands out a sequence that may already have been used");

assert.throws(function () {
    boot.durableSequence(fakeStorage({ seq: "-3" }), "seq")();
}, function (error) { return error.code === "bad_sequence_store"; },
"an unusable stored ceiling refuses rather than restarting from zero");

/* ---- session, device registration, capabilities, token -------------------- */

function makeSession(routes, extra) {
    return new boot.CloudViewerSession(Object.assign({
        config: config,
        fetch: fakeFetch(routes),
        storage: fakeStorage(),
        indexedDB: fakeIndexedDB(),
        crypto: globalThis.crypto,
        now: function () { return 1000; },
        WebSocket: function () { }
    }, extra || {}));
}

{
    const signedOut = makeSession({
        "GET /v1/auth/session": response(401, { error: { code: "no_session" } }),
        "POST /v1/auth/session": response(401, { error: { code: "no_login_ticket" } })
    });
    const outcome = await signedOut.ensureSession();
    assert.equal(outcome.state, "sign_in", "a browser with no login ticket is sent to sign in");
    assert.ok(outcome.url.startsWith("https://api.clawdline.com/v1/auth/oauth/start?return_to="),
        "through the control plane's own OAuth start");
    assert.ok(outcome.url.includes(encodeURIComponent("https://app.clawdline.com/")),
        "and comes back to the console it left");
}

{
    const registering = makeSession({
        "GET /v1/auth/session": response(401, { error: { code: "no_session" } }),
        "POST /v1/auth/session": response(201, {
            account_id: "acct-1", device_id: "dev-1",
            caps: ["read_sessions", "read_transcript", "send_prompt"]
        }),
        "GET /v1/devices": response(200, { devices: [], active: 0 })
    });
    const outcome = await registering.ensureSession();
    assert.equal(outcome.state, "ready", "a fresh browser registers a device and gets a session");
    assert.equal(outcome.deviceID, "dev-1");
    const posted = JSON.parse(registering.fetch.calls.find(function (call) {
        return call.key === "POST /v1/auth/session";
    }).init.body);
    assert.equal(posted.kind, "browser");
    assert.deepEqual(posted.caps, boot.VIEWER_CAPABILITIES,
        "the four-way capability split is asked for by name");
    assert.equal(Buffer.from(posted.public_key, "base64").length, 32,
        "and the device is registered with its own Ed25519 public key");
    assert.equal(registering.devicePrivateKey.extractable, false,
        "whose private half this page can use and cannot read");
    assert.ok(registering.indexedDB.store.size >= 1,
        "the private key is kept where a CryptoKey survives a reload");
}

{
    const revoked = makeSession({
        "GET /v1/auth/session": response(200, { account_id: "acct-1", device_id: "dev-1" }),
        "GET /v1/devices": response(200, {
            devices: [{ id: "dev-1", caps: ["read_sessions"], revoked_at: "2026-08-30T00:00:00Z" }],
            active: 0
        })
    });
    // Registration path, so the key exists in this session's own store first.
    revoked.deviceID = "dev-1";
    await assert.rejects(revoked.readCapabilities(), function (error) {
        return error.code === "revoked";
    }, "a revoked device row is a terminal answer, not a retry");
}

{
    const readOnly = makeSession({
        "GET /v1/auth/session": response(401, {}),
        "POST /v1/auth/session": response(201, {
            account_id: "acct-1", device_id: "dev-1", caps: ["read_sessions", "read_transcript"]
        }),
        "GET /v1/devices": response(200, {
            devices: [{ id: "dev-1", caps: ["read_sessions", "read_transcript"], revoked_at: null }]
        }),
        "POST /v1/tokens/device": response(403, { error: { code: "revoked" } })
    });
    await readOnly.ensureSession();
    await assert.rejects(readOnly.deviceToken(), function (error) {
        return error.code === "revoked";
    }, "a refused device token is revocation, not an outage");
}

/* ---- claiming the one pairing slot ---------------------------------------- */

function pairingSession(claimResponse) {
    const session = makeSession({
        "POST /v1/pairing/claim": claimResponse
    });
    session.account = "acct-1";
    session.deviceID = "dev-1";
    return session;
}

const pendingOffer = {
    pairingID: "pair-1",
    claimNonce: Buffer.alloc(32, 1).toString("base64"),
    offer: { pairing_id: "pair-1", account_id: "acct-1" },
    ephemeralPrivateKey: null,
    expiresAt: 2000
};

{
    // The one the API actually answers while the Mac has not written the slot yet.
    const pending = pairingSession(response(202, {
        error: { code: "pairing_pending", message: "The other device has not answered yet" }
    }));
    await assert.rejects(pending.claimPairing(pendingOffer), function (error) {
        assert.equal(error.code, "pairing_unfinished",
            "202 pairing_pending is the polling state, not a failure — got " + error.code);
        return true;
    }, "a pending claim keeps the caller polling");
}

for (const [status, code, expected, why] of [
    [409, "pairing_expired", "pairing_expired", "an expired offer says so"],
    [403, "wrong_claimant", "wrong_claimant", "a claim from the wrong device says so"],
    [404, "unknown_pairing", "pairing_gone", "a slot already claimed and destroyed says so"],
    [500, "internal", "pairing_failed", "and anything else is an ordinary failure"]
]) {
    const failing = pairingSession(response(status, { error: { code: code } }));
    await assert.rejects(failing.claimPairing(pendingOffer), function (error) {
        assert.equal(error.code, expected, why + " — got " + error.code);
        return true;
    }, why);
}

/* ---- the pairing poll loop ------------------------------------------------ */

{
    let claims = 0;
    const offers = [];
    const fakeSession = {
        now: function () { return 1000; },
        startPairing: function () {
            return Promise.resolve({ pairingID: "pair-1", expiresAt: 9000 });
        },
        claimPairing: function () {
            claims += 1;
            if (claims < 3) {
                const error = new Error("pending");
                error.code = "pairing_unfinished";
                return Promise.reject(error);
            }
            return Promise.resolve({ accountID: "acct-1", machineID: "mac-1" });
        }
    };
    const paired = await boot.pairViewer(fakeSession, {
        onOffer: function (offer) { offers.push(offer); },
        sleep: function () { return Promise.resolve(); }
    });
    assert.equal(claims, 3, "the loop keeps polling while the Mac has not answered");
    assert.equal(offers.length, 1, "and shows the offer once rather than once per poll");
    assert.equal(paired.machineID, "mac-1", "then hands back what the Mac sealed");
}

{
    let claims = 0;
    let accepted = null;
    const invitation = { invitation_id: "invite-1" };
    const fakeSession = {
        now: function () { return 1000; },
        startPairing: function () {
            return Promise.resolve({ pairingID: "pair-1", expiresAt: 9000 });
        },
        acceptPairingInvitation: function (seenInvitation, pending) {
            accepted = { invitation: seenInvitation, pending: pending };
            return Promise.resolve(pending);
        },
        claimPairing: function () {
            claims += 1;
            if (claims === 1) {
                const error = new Error("pending");
                error.code = "pairing_unfinished";
                return Promise.reject(error);
            }
            return Promise.resolve({ machineID: "mac-qr" });
        }
    };
    const paired = await boot.pairViewerFromInvitation(fakeSession, invitation, {
        sleep: function () { return Promise.resolve(); }
    });
    assert.equal(accepted.invitation, invitation,
        "QR-first pairing returns the viewer offer only to the scanned invitation");
    assert.equal(claims, 2, "then waits for the Mac's encrypted handover");
    assert.equal(paired.machineID, "mac-qr", "and completes through the existing handover");
}

{
    let now = 1000;
    const expiring = {
        now: function () { return now; },
        startPairing: function () {
            return Promise.resolve({ pairingID: "pair-1", expiresAt: 1500 });
        },
        claimPairing: function () {
            const error = new Error("pending");
            error.code = "pairing_unfinished";
            return Promise.reject(error);
        }
    };
    await assert.rejects(boot.pairViewer(expiring, {
        onOffer: function () { },
        sleep: function () { now += 400; return Promise.resolve(); }
    }), function (error) { return error.code === "offer_expired"; },
    "an offer nobody answered stops being polled when it expires");
}

/* ---- staying connected ---------------------------------------------------- */

function fakeClient() {
    const listeners = new Set();
    return {
        events: function (listener) {
            listeners.add(listener);
            return function () { listeners.delete(listener); };
        },
        drop: function () {
            listeners.forEach(function (listener) {
                listener({ type: "connection", state: "offline" });
            });
        },
        stop: function () { }
    };
}

{
    const states = [];
    let attempts = 0;
    const client = fakeClient();
    const session = {
        client: client,
        connect: function () {
            attempts += 1;
            if (attempts === 1) return Promise.reject(new Error("socket refused"));
            return Promise.resolve({ state: "connected", client: client });
        }
    };
    const keeper = boot.keepConnected(session, {
        sleep: function () { return Promise.resolve(); },
        jitter: function () { return 0.5; },
        onState: function (update) { states.push(update.state); }
    });
    await new Promise(function (resolve) { setTimeout(resolve, 5); });
    assert.deepEqual(states, ["retrying", "connected"],
        "a transient failure is retried and then connects");
    client.drop();
    await new Promise(function (resolve) { setTimeout(resolve, 5); });
    assert.ok(states.includes("reconnecting"), "a dropped socket reconnects");
    keeper.stop();
}

{
    const states = [];
    const revokedError = new Error("revoked");
    revokedError.code = "revoked";
    const keeper = boot.keepConnected({
        connect: function () { return Promise.reject(revokedError); }
    }, {
        sleep: function () { return Promise.resolve(); },
        onState: function (update) { states.push(update.state); }
    });
    await keeper.done;
    assert.deepEqual(states, ["revoked"],
        "a revoked device stops rather than knocking on the relay every thirty seconds");
}

/* ---- the placeholder transport -------------------------------------------- */

const { assertClawdlineClient } = await import("../Resources/web/app/js/net/client.js");
assert.doesNotThrow(function () { assertClawdlineClient(boot.idleClient()); },
    "the transport held while the cloud one boots satisfies the same seam");
assert.doesNotThrow(function () { boot.idleClient().start(); },
    "the placeholder also satisfies main.js's unconditional boot hook");

console.log("web cloud boot tests passed");
