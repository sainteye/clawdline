/* --------------------------------------------------------------------------
   Which transport this copy of the page is, and how the cloud one starts

   The page has always had exactly two transports and one question deciding
   between them: `MOCK`. A third arrives with the hosted console, and the
   question cannot be "is this localhost", because the Mac already serves this
   same page through a Cloudflare tunnel on a hostname that is not localhost and
   which must keep talking to the Mac directly.

   So the discriminator is a **build-time declaration**, not a guess about the
   hostname. `tools/build-web-app.sh` fills the `<!-- clawdline:cloud -->` slot in
   `index.html` with the origins that build is for — the same mechanism the Mac
   already uses for `<!-- clawdline:strings -->` and `<!-- clawdline:modules -->`,
   and for the same reason: a copy served by anything else keeps the comment and
   keeps its old behaviour. The declaration is then checked against
   `location.origin` before it is believed, so a hosted bundle copied to another
   origin refuses to act as that origin's console rather than half-working.
   -------------------------------------------------------------------------- */

import { CloudClient } from "./cloud-client.js";
import { importSenderPublicKey, loadCryptoKey, storeCryptoKey } from "./cloud-crypto.js";
import {
    createPairingOffer, ed25519Fingerprint, encryptPairingOfferForInvitation,
    openPairingHandover, pairingError
} from "./cloud-pairing.js";

/** Capabilities this console asks for. The four-way split is PROTOCOL §12's, unmerged. */
export const VIEWER_CAPABILITIES = ["read_sessions", "read_transcript", "send_prompt"];

const DEVICE_KEY = "clawdline.viewer.device";
const DEVICE_PUBLIC_KEY = "clawdline.viewer.public";
const SEQUENCE_KEY = "clawdline.viewer.sequence";
const SEQUENCE_BLOCK = 64;

export function bootError(code, message) {
    var error = new Error(message || code);
    error.code = code;
    return error;
}

/**
 * The build declaration, or null.
 *
 * Every field is required and checked here rather than where it is used: a console pointed at
 * the wrong API by a typo should refuse at boot, in one place, and not three requests later.
 */
export function readCloudConfig(scope) {
    var raw = scope && scope.__clawdlineCloud;
    if (!raw || typeof raw !== "object") return null;
    if (raw.v !== 1) throw bootError("bad_cloud_config", "unsupported cloud config version");
    ["app_origin", "api_origin", "relay_url"].forEach(function (key) {
        if (typeof raw[key] !== "string" || !raw[key]) {
            throw bootError("bad_cloud_config", "the cloud config is missing " + key);
        }
    });
    var app = new URL(raw.app_origin);
    var api = new URL(raw.api_origin);
    var relay = new URL(raw.relay_url);
    if (app.protocol !== "https:" || api.protocol !== "https:") {
        throw bootError("bad_cloud_config", "the app and API origins must be https");
    }
    if (relay.protocol !== "wss:") {
        throw bootError("bad_cloud_config", "the relay URL must be wss");
    }
    return {
        appOrigin: app.origin,
        apiOrigin: api.origin,
        relayURL: relay.toString(),
        build: typeof raw.build === "string" ? raw.build : ""
    };
}

/**
 * `mock` | `local` | `cloud` | `blocked`.
 *
 * `blocked` is deliberately not `local`: a hosted bundle that has been copied somewhere else
 * would otherwise start asking that origin for `/v1/sessions`, and a wall of 404s reads as a
 * broken app rather than as a build served from the wrong place.
 */
export function chooseTransport(input) {
    if (input && input.mock) return "mock";
    var config = input && input.config;
    if (!config) return "local";
    return input.origin === config.appOrigin ? "cloud" : "blocked";
}

/**
 * A durable outbound sequence, with the reserve-ahead discipline `CloudSequenceFile` uses on
 * the Mac and for the same reason: the Mac refuses a `ctl` envelope whose sequence did not
 * advance, so a counter that restarts at zero after a reload does not merely repeat itself,
 * it makes this browser unable to send anything until it has climbed back past where it was.
 * What is written down is the ceiling, before any number under it is handed out.
 */
export function durableSequence(storage, key) {
    var name = key || SEQUENCE_KEY;
    var reserved = 0;
    var next = null;
    function read() {
        if (next !== null) return;
        var raw;
        try {
            raw = storage.getItem(name);
        } catch (e) {
            throw bootError("no_sequence_store", "this browser has no durable sequence store");
        }
        var parsed = raw === null || raw === undefined ? 0 : Number(raw);
        if (!Number.isSafeInteger(parsed) || parsed < 0) {
            throw bootError("bad_sequence_store", "the stored viewer sequence is unusable");
        }
        reserved = parsed;
        next = parsed;
    }
    return function nextSequence() {
        read();
        var value = next;
        if (value >= reserved) {
            reserved = value + SEQUENCE_BLOCK;
            try {
                storage.setItem(name, String(reserved));
            } catch (e) {
                throw bootError("no_sequence_store", "the viewer sequence could not be written");
            }
        }
        next = value + 1;
        return value;
    };
}

async function json(fetchImpl, url, options) {
    var response = await fetchImpl(url, Object.assign({ credentials: "include" }, options || {}));
    var body = null;
    try { body = await response.json(); } catch (e) { body = null; }
    return { status: response.status, body: body };
}

function refused(status) { return status === 401 || status === 403; }

/**
 * The viewer's whole boot, as one object with no DOM in it.
 *
 * Everything it touches — fetch, storage, IndexedDB, the clock, the socket — is injected, so
 * the same object runs under the tests with fakes and in the browser with the real thing.
 */
export class CloudViewerSession {
    constructor(options) {
        options = options || {};
        if (!options.config) throw new TypeError("CloudViewerSession needs a cloud config");
        this.config = options.config;
        this.fetch = options.fetch || function (url, init) { return globalThis.fetch(url, init); };
        this.storage = options.storage || globalThis.localStorage;
        this.indexedDB = options.indexedDB || globalThis.indexedDB;
        this.crypto = options.crypto || globalThis.crypto;
        this.now = options.now || function () { return Date.now(); };
        this.WebSocket = options.WebSocket || globalThis.WebSocket;
        this.handlers = options.handlers || null;
        this.deviceName = options.deviceName || "Browser";
        this.returnTo = options.returnTo || this.config.appOrigin + "/";
        this.account = null;
        this.deviceID = null;
        this.caps = [];
        this.devicePrivateKey = null;
        this.devicePublicKey = null;
        this.client = null;
    }

    /** Where a signed-out browser is sent. A top-level navigation, so the cookie comes back. */
    signInURL() {
        return this.config.apiOrigin + "/v1/auth/oauth/start?return_to="
            + encodeURIComponent(this.returnTo);
    }

    /**
     * Establish the cookie session and the revocable viewer device behind it.
     *
     * The order matters and is the control plane's, not a preference: `POST /v1/auth/session`
     * both registers the device and mints the cookie, so the key pair has to exist before the
     * session does. That is what makes the session revocable per device — revoking the
     * `web_devices` row invalidates the cookie that names it (`api/src/routes/guards.ts`).
     */
    async ensureSession() {
        var existing = await json(this.fetch, this.config.apiOrigin + "/v1/auth/session");
        if (existing.status === 200 && existing.body && existing.body.device_id) {
            var restored = await this.restoreDeviceKey(existing.body.device_id);
            if (restored) {
                this.account = existing.body.account_id;
                this.deviceID = existing.body.device_id;
                this.caps = await this.readCapabilities();
                return { state: "ready", accountID: this.account, deviceID: this.deviceID };
            }
            // A cookie naming a device whose key this browser no longer holds is not this
            // browser's device. Registering a new one is honest; reusing the cookie would
            // leave a viewer that cannot sign a relay challenge.
        }

        var pair = await this.crypto.subtle.generateKey(
            { name: "Ed25519" }, false, ["sign", "verify"]);
        var publicKey = new Uint8Array(await this.crypto.subtle.exportKey("raw", pair.publicKey));
        var created = await json(this.fetch, this.config.apiOrigin + "/v1/auth/session", {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({
                kind: "browser", name: this.deviceName,
                public_key: bytesToBase64(publicKey), caps: VIEWER_CAPABILITIES
            })
        });
        if (refused(created.status)) return { state: "sign_in", url: this.signInURL() };
        if (created.status !== 200 && created.status !== 201) {
            throw bootError("session_failed", "the control plane refused this browser's session");
        }
        this.account = created.body.account_id;
        this.deviceID = created.body.device_id;
        this.caps = Array.isArray(created.body.caps) ? created.body.caps : VIEWER_CAPABILITIES;
        this.devicePrivateKey = pair.privateKey;
        this.devicePublicKey = publicKey;
        await storeCryptoKey(DEVICE_KEY + ":" + this.deviceID, pair.privateKey, this.indexedDB);
        // The public half is recorded beside it because an Ed25519 CryptoKey that cannot be
        // exported cannot be asked for its public key either, and pairing has to state it.
        this.storage.setItem(DEVICE_PUBLIC_KEY + ":" + this.deviceID, bytesToBase64(publicKey));
        return { state: "ready", accountID: this.account, deviceID: this.deviceID };
    }

    async restoreDeviceKey(deviceID) {
        var key = await loadCryptoKey(DEVICE_KEY + ":" + deviceID, this.indexedDB);
        if (!key) return false;
        var stored = this.storage.getItem(DEVICE_PUBLIC_KEY + ":" + deviceID);
        if (!stored) return false;
        this.devicePrivateKey = key;
        this.devicePublicKey = base64ToBytes(stored);
        return true;
    }

    /**
     * Caps as the control plane holds them now, not as they were asked for.
     *
     * Read every boot rather than remembered, because this is also where a revoked device
     * finds out: `GET /v1/devices` still answers for the account, and the row carries
     * `revoked_at`.
     */
    async readCapabilities() {
        var listed = await json(this.fetch, this.config.apiOrigin + "/v1/devices");
        if (refused(listed.status)) throw bootError("revoked", "this viewer device is not authorized");
        if (listed.status !== 200 || !listed.body || !Array.isArray(listed.body.devices)) {
            throw bootError("devices_failed", "the device list could not be read");
        }
        var self = this;
        var row = listed.body.devices.find(function (device) { return device.id === self.deviceID; });
        if (!row) throw bootError("revoked", "this viewer device is no longer registered");
        if (row.revoked_at) throw bootError("revoked", "this viewer device has been revoked");
        return Array.isArray(row.caps) ? row.caps : [];
    }

    async deviceToken() {
        var minted = await json(this.fetch, this.config.apiOrigin + "/v1/tokens/device",
            { method: "POST" });
        if (refused(minted.status)) {
            throw bootError("revoked", "the control plane refused this device's token");
        }
        if (minted.status !== 200 || !minted.body || typeof minted.body.token !== "string") {
            throw bootError("token_failed", "the device token could not be minted");
        }
        return {
            token: minted.body.token,
            expiresAt: Date.parse(minted.body.expires_at),
            relayURL: typeof minted.body.relay_url === "string"
                ? minted.body.relay_url : this.config.relayURL
        };
    }

    masterKeyName() { return "clawdline.master:" + this.account; }
    senderKeyName(sender) { return "clawdline.sender:" + this.account + ":" + sender; }

    async accountKey() {
        return (await loadCryptoKey(this.masterKeyName(), this.indexedDB)) || null;
    }

    /**
     * Ask the control plane for a routing handle and a one-time claim nonce, then build the
     * offer the human carries to the Mac. The private half never leaves this page.
     */
    async startPairing() {
        if (!this.devicePublicKey) throw bootError("no_device_key", "no viewer device key yet");
        var fingerprint = await ed25519Fingerprint(this.devicePublicKey);
        var started = await json(this.fetch, this.config.apiOrigin + "/v1/pairing/start", {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ fingerprint: fingerprint })
        });
        if (refused(started.status)) throw bootError("revoked", "pairing was refused");
        if (started.status !== 200 || !started.body || !started.body.pairing_id) {
            throw bootError("pairing_failed", "the control plane would not start a pairing");
        }
        var expiresAt = Date.parse(started.body.expires_at);
        var made = await createPairingOffer({
            pairingID: started.body.pairing_id,
            claimNonce: started.body.claim_nonce,
            accountID: this.account,
            deviceID: this.deviceID,
            signingPublicKey: this.devicePublicKey,
            expiresAt: expiresAt
        });
        return {
            pairingID: started.body.pairing_id,
            claimNonce: started.body.claim_nonce,
            expiresAt: expiresAt,
            fingerprint: fingerprint,
            offer: made.offer,
            fragment: made.fragment,
            ephemeralPrivateKey: made.ephemeralPrivateKey
        };
    }

    /**
     * Prove this browser scanned the Mac's QR, and relay its offer encrypted by that QR secret.
     * The API receives the SHA-256 proof and opaque AES-GCM bytes, never the secret or offer.
     */
    async acceptPairingInvitation(invitation, pending) {
        var sealed = await encryptPairingOfferForInvitation(invitation, pending.fragment);
        var accepted = await json(
            this.fetch, this.config.apiOrigin + "/v1/pairing/invitations/accept", {
                method: "POST",
                headers: { "content-type": "application/json" },
                body: JSON.stringify({
                    invitation_id: invitation.invitation_id,
                    secret_hash: sealed.secretHash,
                    encrypted_offer: sealed.encryptedOffer
                })
            });
        if (accepted.status === 403) {
            throw pairingError("wrong_invitation", "that QR does not belong to this account or Mac");
        }
        if (accepted.status === 409) {
            throw pairingError("invitation_expired", "that QR has expired or was already scanned");
        }
        if (accepted.status !== 200 || !accepted.body || accepted.body.status !== "delivered") {
            throw pairingError("invitation_failed", "the encrypted pairing offer could not be delivered");
        }
        return pending;
    }

    /**
     * Take the one blob the Mac left, exactly once, and keep what was in it.
     *
     * Both keys are stored as CryptoKeys and neither can be read back out: the account key is
     * an `AES-GCM` key this page can decrypt snapshots with, the machine's an `Ed25519` verify
     * key. Pinning the machine key here rather than reading it from the API afterwards is
     * PROTOCOL §3's "held locally, not trusted from the cloud".
     */
    async claimPairing(pending) {
        var claimed = await json(this.fetch, this.config.apiOrigin + "/v1/pairing/claim", {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({
                pairing_id: pending.pairingID, claim_nonce: pending.claimNonce
            })
        });
        if (claimed.status !== 200 || !claimed.body || typeof claimed.body.ciphertext !== "string") {
            // The control plane's refusals are typed and one of them is not a refusal at all:
            // `202 pairing_pending` is what `POST /v1/pairing/claim` answers for as long as the
            // Mac has not written the slot, which is the ordinary polling state. Reading it as
            // an error made pairing impossible rather than slow. The rest are terminal in
            // different ways, and each says which so a caller can tell "wait" from "start
            // again" from "that was not yours".
            var code = claimed.body && claimed.body.error && claimed.body.error.code;
            if (claimed.status === 202 || code === "pairing_pending") {
                throw pairingError("pairing_unfinished", "the Mac has not answered that offer yet");
            }
            if (code === "pairing_expired") {
                throw pairingError("pairing_expired", "that pairing offer has expired");
            }
            if (code === "wrong_claimant") {
                throw pairingError("wrong_claimant", "that pairing is not this device's to claim");
            }
            if (code === "unknown_pairing") {
                throw pairingError("pairing_gone", "that pairing has already been claimed");
            }
            throw pairingError("pairing_failed", "that pairing could not be claimed");
        }
        var wrapper;
        try {
            wrapper = JSON.parse(new TextDecoder().decode(base64ToBytes(claimed.body.ciphertext)));
        } catch (e) {
            throw pairingError("bad_wrapper", "the handover blob is not a pairing wrapper");
        }
        var opened = await openPairingHandover({
            wrapper: wrapper,
            offer: pending.offer,
            ephemeralPrivateKey: pending.ephemeralPrivateKey,
            senderDeviceID: claimed.body.sender_device_id,
            nowMilliseconds: this.now()
        });
        if (opened.accountID !== this.account) {
            throw pairingError("wrong_account", "that handover is for another account");
        }
        await storeCryptoKey(this.masterKeyName(), opened.masterKey, this.indexedDB);
        await storeCryptoKey(this.senderKeyName(opened.machineDeviceID),
            await importSenderPublicKey(opened.machineSigningKey), this.indexedDB);
        return opened;
    }

    /** Everything above, in the one order that works, ending in a started CloudClient. */
    async connect() {
        var session = await this.ensureSession();
        if (session.state === "sign_in") return session;
        var master = await this.accountKey();
        if (!master) return { state: "pairing_required", accountID: this.account };
        var token = await this.deviceToken();
        var self = this;
        var client = new CloudClient({
            relayURL: token.relayURL,
            deviceToken: token.token,
            devicePrivateKey: this.devicePrivateKey,
            deviceID: this.deviceID,
            account: this.account,
            masterKey: master,
            resolveSenderKey: function (sender) {
                return loadCryptoKey(self.senderKeyName(sender), self.indexedDB);
            },
            // Writes need the capability the control plane actually granted, and they need it
            // read back rather than assumed: a device downgraded to read-only must find that
            // out here rather than at the first refused envelope.
            allowWrites: this.caps.indexOf("send_prompt") >= 0,
            nextSequence: durableSequence(this.storage, SEQUENCE_KEY + ":" + this.deviceID),
            WebSocket: this.WebSocket,
            handlers: this.handlers
        });
        this.client = client;
        await client.start();
        return { state: "connected", client: client, expiresAt: token.expiresAt };
    }
}

/**
 * Show one pairing offer and keep claiming it until the Mac answers or the offer runs out.
 *
 * The offer is shown exactly once — a loop that re-published it every two seconds would keep
 * replacing the very string the person is in the middle of carrying to their Mac. Everything
 * except `pairing_unfinished` ends the loop, because every other typed answer means starting
 * again rather than waiting longer.
 */
export async function pairViewer(session, options) {
    options = options || {};
    var sleep = options.sleep || function (ms) {
        return new Promise(function (resolve) { setTimeout(resolve, ms); });
    };
    var interval = options.intervalMs || 2000;
    var onOffer = options.onOffer || function () {};
    var pending = await session.startPairing();
    onOffer(pending);
    for (;;) {
        try {
            return await session.claimPairing(pending);
        } catch (error) {
            if (!error || error.code !== "pairing_unfinished") throw error;
            if (session.now() >= pending.expiresAt) {
                throw pairingError("offer_expired", "that pairing offer expired unanswered");
            }
            await sleep(interval);
        }
    }
}

/** QR-first pairing: the Mac displays; this viewer scans, authenticates and answers. */
export async function pairViewerFromInvitation(session, invitation, options) {
    options = options || {};
    var pending = await session.startPairing();
    if (options.onOffer) options.onOffer(pending);
    await session.acceptPairingInvitation(invitation, pending);
    var sleep = options.sleep || function (ms) {
        return new Promise(function (resolve) { setTimeout(resolve, ms); });
    };
    var interval = options.intervalMs || 2000;
    for (;;) {
        try {
            return await session.claimPairing(pending);
        } catch (error) {
            if (!error || error.code !== "pairing_unfinished") throw error;
            if (session.now() >= pending.expiresAt) {
                throw pairingError("offer_expired", "that pairing offer expired unanswered");
            }
            await sleep(interval);
        }
    }
}

/**
 * Keep the viewer connected across a dropped socket and an expiring token.
 *
 * The token is what makes this more than a socket retry: it is short-lived on purpose, so a
 * reconnect after a long sleep needs a fresh one, and the whole point of that shortness is
 * that a revoked device stops getting one. A refusal is therefore terminal here — retrying it
 * on a timer would be the refusal loop the Mac side refuses to run either.
 */
export function keepConnected(session, options) {
    options = options || {};
    var sleep = options.sleep || function (ms) {
        return new Promise(function (resolve) { setTimeout(resolve, ms); });
    };
    var jitter = options.jitter || Math.random;
    var initial = options.initialBackoffMs || 250;
    var maximum = options.maximumBackoffMs || 30000;
    var onState = options.onState || function () {};
    var stopped = false;
    var backoff = initial;

    var loop = (async function () {
        while (!stopped) {
            var outcome = null;
            try {
                outcome = await session.connect();
            } catch (error) {
                if (error && error.code === "revoked") {
                    onState({ state: "revoked", error: error });
                    return;
                }
                onState({ state: "retrying", error: error, afterMs: backoff });
                await sleep(Math.min(maximum, backoff) * (0.75 + jitter() * 0.5));
                backoff = Math.min(maximum, backoff * 2);
                continue;
            }
            if (outcome.state !== "connected") {
                onState(outcome);
                return;
            }
            backoff = initial;
            onState({ state: "connected", client: outcome.client });
            await new Promise(function (resolve) {
                var off = outcome.client.events(function (event) {
                    if (event.type === "connection" && event.state === "offline") {
                        off();
                        resolve();
                    }
                });
                if (stopped) { off(); resolve(); }
            });
            if (stopped) return;
            onState({ state: "reconnecting" });
        }
    })();

    return {
        stop: function () {
            stopped = true;
            if (session.client) session.client.stop();
        },
        done: loop
    };
}

/** A transport that satisfies the seam and does nothing, held while the cloud one boots. */
export function idleClient() {
    var listeners = new Set();
    function offline() {
        return Promise.reject(bootError("cloud_starting", "the cloud connection is not ready"));
    }
    return {
        // `main.js` calls `api.start()` for every selected transport after the DOM boots.
        // The cloud session has already started its own reconnect loop before this placeholder
        // is installed, so the honest implementation here is deliberately a no-op.
        start: function () {},
        events: function (listener) {
            listeners.add(listener);
            return function () { listeners.delete(listener); };
        },
        sessions: function () {
            return Promise.resolve({ sessions: [], at: 0, scan: { emptyAuthoritative: false } });
        },
        transcript: offline,
        send: offline,
        answer: offline,
        dispatch: offline,
        schedules: function () { return Promise.resolve({ schedules: [] }); }
    };
}

function bytesToBase64(bytes) {
    var binary = "";
    for (var i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
    return btoa(binary);
}

function base64ToBytes(text) {
    var binary = atob(text);
    var bytes = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    return bytes;
}
