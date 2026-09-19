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
import { viewerEventLogFor } from "./cloud-viewer-events.js";
import {
    importSenderPublicKey, loadCryptoKey, loadCryptoKeyID, loadPairingBinding,
    storeCryptoKey, storePairingBinding, storePairingCryptoKeys
} from "./cloud-crypto.js";
import {
    createPairingOffer, ed25519Fingerprint, encryptPairingOfferForInvitation,
    openPairingHandover, pairingError
} from "./cloud-pairing.js";

/** Capabilities this console asks for. The four-way split is PROTOCOL §12's, unmerged. */
export const VIEWER_CAPABILITIES = [
    "read_sessions", "read_transcript", "send_prompt", "start_session"
];

const DEVICE_KEY = "clawdline.viewer.device";
const DEVICE_PUBLIC_KEY = "clawdline.viewer.public";
const SEQUENCE_KEY = "clawdline.viewer.sequence";
const SEQUENCE_BLOCK = 64;
const CURRENT_MASTER_KEY_ID = "master-v1";
const PRE_METADATA_MASTER_KEY_IDS = Object.freeze(["ms-1", CURRENT_MASTER_KEY_ID]);

/**
 * Pairing releases before key-id persistence stored the right AES CryptoKey but
 * CloudClient indexed it under its old `ms-1` default. Keep that already-paired
 * browser usable with the two IDs Clawdline has actually shipped. Once pairing
 * has stored an exact ID, use only that ID so future rotation stays fail-closed.
 */
export function masterKeyConnection(masterKey, storedKeyID) {
    var keyID = storedKeyID || CURRENT_MASTER_KEY_ID;
    var masterKeys = {};
    (storedKeyID ? [storedKeyID] : PRE_METADATA_MASTER_KEY_IDS).forEach(function (candidate) {
        masterKeys[candidate] = masterKey;
    });
    return { keyID: keyID, masterKeys: masterKeys };
}

export function bootError(code, message, options) {
    options = options || {};
    var error = new Error(message || code);
    error.code = code;
    error.terminal = options.terminal === true;
    if (options.details && typeof options.details === "object") error.details = options.details;
    if (Number.isSafeInteger(options.status)) error.status = options.status;
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
    var strings = {};
    if (raw.strings !== undefined) {
        if (!raw.strings || typeof raw.strings !== "object" || Array.isArray(raw.strings)) {
            throw bootError("bad_cloud_config", "the cloud string catalog is malformed");
        }
        Object.keys(raw.strings).forEach(function (alias) {
            var tag = raw.strings[alias];
            if (!/^[A-Za-z0-9-]+$/.test(alias) || typeof tag !== "string" ||
                !/^[A-Za-z0-9-]+$/.test(tag)) {
                throw bootError("bad_cloud_config", "the cloud string catalog is malformed");
            }
            strings[alias] = tag;
        });
    }
    return {
        appOrigin: app.origin,
        apiOrigin: api.origin,
        relayURL: relay.toString(),
        build: typeof raw.build === "string" ? raw.build : "",
        strings: strings
    };
}

/** The immutable static catalog for this browser, or null when English HTML is the fallback. */
export function cloudStringsURL(config, languages) {
    if (!config || !config.build || !config.strings) return null;
    var aliases = Object.keys(config.strings).sort(function (a, b) { return b.length - a.length; });
    var wanted = Array.isArray(languages) ? languages : [];
    for (var i = 0; i < wanted.length; i += 1) {
        var language = String(wanted[i]).toLowerCase();
        for (var j = 0; j < aliases.length; j += 1) {
            var alias = aliases[j];
            if (language.indexOf(alias.toLowerCase()) === 0) {
                return "/app/" + encodeURIComponent(config.build) + "/strings/"
                    + encodeURIComponent(config.strings[alias]) + ".json";
            }
        }
    }
    return null;
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

function responseError(result, fallbackCode, fallbackMessage, terminal) {
    var api = result && result.body && result.body.error;
    return bootError(
        api && typeof api.code === "string" ? api.code : fallbackCode,
        api && typeof api.message === "string" ? api.message : fallbackMessage,
        {
            terminal: terminal === true,
            status: result && result.status,
            details: api && api.details
        }
    );
}

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
        this.Client = options.Client || CloudClient;
        this.handlers = options.handlers || null;
        this.deviceName = options.deviceName || "Browser";
        this.deviceKind = ["browser", "ios", "android"].indexOf(options.deviceKind) >= 0
            ? options.deviceKind : "browser";
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
        } else if (!refused(existing.status)) {
            throw responseError(existing, "session_unavailable",
                "Clawdline could not check this browser's session", existing.status < 500);
        }

        var pair = await this.crypto.subtle.generateKey(
            { name: "Ed25519" }, false, ["sign", "verify"]);
        var publicKey = new Uint8Array(await this.crypto.subtle.exportKey("raw", pair.publicKey));
        var created = await json(this.fetch, this.config.apiOrigin + "/v1/auth/session", {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({
                kind: this.deviceKind, name: this.deviceName,
                public_key: bytesToBase64(publicKey), caps: VIEWER_CAPABILITIES
            })
        });
        var createdError = created.body && created.body.error;
        if (created.status === 409 && createdError && createdError.code === "device_limit_reached") {
            var details = createdError.details || {};
            var limit = Number.isSafeInteger(details.limit) && details.limit >= 0 ? details.limit : null;
            var tier = typeof details.tier === "string" ? details.tier : "current plan";
            return {
                state: "device_limit_reached",
                tier: tier,
                limit: limit,
                message: typeof createdError.message === "string"
                    ? createdError.message : "The viewer-device limit has been reached"
            };
        }
        if (refused(created.status)) return { state: "sign_in", url: this.signInURL() };
        if (created.status !== 200 && created.status !== 201) {
            throw responseError(created,
                created.status >= 500 ? "session_unavailable" : "session_failed",
                "Clawdline could not create this browser's session", created.status < 500);
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

    /** Safe metadata available only under the still-live, fresh OAuth login ticket. */
    async recoveryDevices() {
        var listed = await json(this.fetch,
            this.config.apiOrigin + "/v1/auth/recovery/devices");
        if (listed.status !== 200 || !listed.body || !Array.isArray(listed.body.devices)) {
            throw responseError(listed, "device_recovery_failed",
                "Clawdline could not read the viewer devices using your slots", listed.status < 500);
        }
        var devices = listed.body.devices.map(function (device) {
            if (!device || typeof device.id !== "string" || typeof device.name !== "string") {
                throw bootError("bad_recovery_response", "Clawdline returned an unusable device list", {
                    terminal: true
                });
            }
            return {
                id: device.id,
                name: device.name,
                kind: typeof device.kind === "string" ? device.kind : "browser",
                created_at: typeof device.created_at === "string" ? device.created_at : null,
                last_seen_at: typeof device.last_seen_at === "string" ? device.last_seen_at : null
            };
        });
        return {
            tier: typeof listed.body.tier === "string" ? listed.body.tier : "current plan",
            limit: Number.isSafeInteger(listed.body.limit) ? listed.body.limit : null,
            active: Number.isSafeInteger(listed.body.active) ? listed.body.active : devices.length,
            devices: devices
        };
    }

    async revokeRecoveryDevice(deviceID) {
        if (typeof deviceID !== "string" || !deviceID) {
            throw bootError("bad_recovery_device", "Choose a viewer device to revoke", { terminal: true });
        }
        var revoked = await json(this.fetch,
            this.config.apiOrigin + "/v1/auth/recovery/devices/" + encodeURIComponent(deviceID),
            { method: "DELETE" });
        if (revoked.status !== 200 || !revoked.body || revoked.body.status !== "revoked") {
            throw responseError(revoked, "device_recovery_failed",
                "Clawdline could not revoke that viewer device", revoked.status < 500);
        }
        return revoked.body;
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
    masterKeyIDName() { return "clawdline.master-key-id:" + this.account; }
    senderKeyName(sender) { return "clawdline.sender:" + this.account + ":" + sender; }
    machineMasterKeyName(machine) {
        return "clawdline.machine-master:" + this.account + ":" + encodeURIComponent(machine);
    }
    machineMasterKeyIDName(machine) {
        return "clawdline.machine-master-key-id:" + this.account + ":" + encodeURIComponent(machine);
    }
    machineSenderKeyName(machine, sender) {
        return "clawdline.machine-sender:" + this.account + ":" + encodeURIComponent(machine)
            + ":" + encodeURIComponent(sender);
    }
    machineBindingName(machine) {
        return "clawdline.machine-binding:" + this.account + ":" + encodeURIComponent(machine);
    }

    async accountKey() {
        return (await loadCryptoKey(this.masterKeyName(), this.indexedDB)) || null;
    }

    async accountKeyID() {
        return await loadCryptoKeyID(this.masterKeyIDName(), this.indexedDB);
    }

    /** One exact machine/content/sender tuple, or null for a pre-scoping browser. */
    async machinePairing(machine) {
        var binding = await loadPairingBinding(this.machineBindingName(machine), this.indexedDB);
        if (!binding) return null;
        var senderName = binding.legacy
            ? this.senderKeyName(binding.senderID)
            : this.machineSenderKeyName(machine, binding.senderID);
        var senderKey = await loadCryptoKey(senderName, this.indexedDB);
        var masterKey;
        if (binding.legacy) {
            var legacy = masterKeyConnection(await this.accountKey(), await this.accountKeyID());
            masterKey = legacy.masterKeys[binding.keyID];
        } else {
            var values = await Promise.all([
                loadCryptoKey(this.machineMasterKeyName(machine), this.indexedDB),
                loadCryptoKeyID(this.machineMasterKeyIDName(machine), this.indexedDB)
            ]);
            if (values[1] === binding.keyID) masterKey = values[0];
        }
        if (!masterKey || !senderKey) {
            throw bootError("machine_key_incomplete",
                "This browser's pairing for the selected machine is incomplete. "
                + "Start the Pair a Browser flow on that machine, then try again.");
        }
        return { machineID: binding.machineID, senderID: binding.senderID,
            keyID: binding.keyID, masterKey: masterKey, senderKey: senderKey,
            legacy: binding.legacy };
    }

    /**
     * A browser paired before machine scoping has only an account key and a sender pin. Bind it
     * after (and only after) CloudClient verified that sender and decrypted a signed channel.
     */
    async bindLegacyMachine(machine, sender, observedKeyID) {
        var connection = masterKeyConnection(await this.accountKey(), await this.accountKeyID());
        var senderKey = await loadCryptoKey(this.senderKeyName(sender), this.indexedDB);
        if (!connection.masterKeys[observedKeyID] || !senderKey) {
            throw bootError("machine_pairing_required", "this machine has no complete pairing");
        }
        var binding = { v: 1, machineID: machine, senderID: sender,
            keyID: observedKeyID, legacy: true };
        await storePairingBinding(this.machineBindingName(machine), binding, this.indexedDB);
        return { machineID: machine, senderID: sender, keyID: observedKeyID,
            masterKey: connection.masterKeys[observedKeyID], senderKey: senderKey, legacy: true };
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
        var senderKey = await importSenderPublicKey(opened.machineSigningKey);
        await storePairingCryptoKeys({
            masterName: this.machineMasterKeyName(opened.machineID),
            masterKeyIDName: this.machineMasterKeyIDName(opened.machineID),
            masterKey: opened.masterKey,
            keyID: opened.keyID,
            senderName: this.machineSenderKeyName(opened.machineID, opened.machineDeviceID),
            senderKey: senderKey,
            bindingName: this.machineBindingName(opened.machineID),
            binding: { v: 1, machineID: opened.machineID, senderID: opened.machineDeviceID,
                keyID: opened.keyID, legacy: false },
            // Compatibility aliases are written only if no old account key exists. `put` is
            // deliberately forbidden for an existing alias: pairing AWS must not evict the Mac.
            preserve: { masterName: this.masterKeyName(),
                masterKeyIDName: this.masterKeyIDName(),
                senderName: this.senderKeyName(opened.machineDeviceID) }
        }, this.indexedDB);
        // A client still running may have been told "no pairing" for this machine a moment ago.
        if (this.client && typeof this.client.forgetMachinePairingAnswer === "function") {
            this.client.forgetMachinePairingAnswer(opened.machineID);
        }
        return opened;
    }

    /** Everything above, in the one order that works, ending in a started CloudClient. */
    async connect() {
        var session = await this.ensureSession();
        if (session.state !== "ready") return session;
        var master = await this.accountKey();
        if (!master) return { state: "pairing_required", accountID: this.account };
        var keyConnection = masterKeyConnection(master, await this.accountKeyID());
        var token = await this.deviceToken();
        var self = this;
        var previous = this.client;
        var client = new this.Client({
            relayURL: token.relayURL,
            deviceToken: token.token,
            devicePrivateKey: this.devicePrivateKey,
            deviceID: this.deviceID,
            account: this.account,
            keyID: keyConnection.keyID,
            masterKeys: keyConnection.masterKeys,
            resolveMachinePairing: function (machine) {
                return self.machinePairing(machine);
            },
            resolveSenderKey: function (sender) {
                return loadCryptoKey(self.senderKeyName(sender), self.indexedDB);
            },
            bindLegacyMachine: function (machine, sender, observedKeyID) {
                return self.bindLegacyMachine(machine, sender, observedKeyID);
            },
            // Writes need the capability the control plane actually granted, and they need it
            // read back rather than assumed: a device downgraded to read-only must find that
            // out here rather than at the first refused envelope.
            allowWrites: this.caps.indexOf("send_prompt") >= 0,
            nextSequence: durableSequence(this.storage, SEQUENCE_KEY + ":" + this.deviceID),
            // Names/platforms are non-authoritative display metadata. CloudClient writes them
            // only after an authenticated `orch/` envelope, then restores them here across a
            // full PWA process restart while the relay realigns each machine independently.
            descriptorStorage: this.storage,
            // The Mac chosen under Devices to transcribe dictation, remembered per browser.
            voiceHostStorage: this.storage,
            // Receive failures and the door, kept on this device and delivered to the paired Mac
            // with nobody pressing anything (`cloud-viewer-events.js`, `docs/diagnostics.md`).
            // With Web Locks only one tab of this device writes the stored rows.
            viewerEvents: viewerEventLogFor(this.storage, this.account, this.deviceID, {
                locks: globalThis.navigator && globalThis.navigator.locks || null }),
            webBuild: this.config && typeof this.config.build === "string" ? this.config.build : "",
            WebSocket: this.WebSocket,
            handlers: this.handlers,
            // Reconnect realignment is channel-by-channel, not one atomic account inventory.
            // Seed the replacement client so an early row cannot evict the conversation the
            // phone is already reading before its own retained channel is replayed.
            resumeFrom: previous
        });
        // During token renewal the previous authenticated socket remains usable. Do not replace
        // the header's live state with "connecting" merely because its successor is warming up.
        await client.start({ quiet: !!(previous && previous.ready && previous.continuityUnproven !== true) });
        try {
            await client.whenReady();
        } catch (error) {
            client.stop();
            throw error;
        }
        this.client = client;
        return {
            state: "connected", client: client, previous: previous,
            expiresAt: token.expiresAt,
            // The relay's own statement of how long this token has left, measured on the relay's
            // clock and carried across as a duration. `expiresAt` is the API's clock read against
            // this device's, and a phone whose clock runs minutes ahead renews at once, every time.
            renewInMs: typeof client._tokenRemainingMs === "function" ? client._tokenRemainingMs() : null
        };
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
    var setTimer = options.setTimeout || function (fn, ms) { return setTimeout(fn, ms); };
    var clearTimer = options.clearTimeout || function (timer) { clearTimeout(timer); };
    // An injected `sleep` cannot be cancelled, so it is raced against the wake signal below; the
    // default is a timer that is cleared the moment its wait ends for any other reason.
    var injectedSleep = options.sleep || null;
    var jitter = options.jitter || Math.random;
    var initial = options.initialBackoffMs || 250;
    var maximum = options.maximumBackoffMs || 30000;
    var now = options.now || function () { return Date.now(); };
    var renewalLeadMs = Math.max(0, Number(options.renewalLeadMs) || 30000);
    var renewalFloorMs = positive(options.renewalFloorMs, RENEWAL_FLOOR_MS);
    var stableMs = positive(options.stableMs, STABLE_CONNECTION_MS);
    var hiddenGraceMs = positive(options.hiddenGraceMs, HIDDEN_GRACE_MS);
    var quiesceDeferMs = positive(options.quiesceDeferMs, QUIESCE_DEFER_MS);
    var wakeSpacingMs = positive(options.wakeSpacingMs, WAKE_SPACING_MS);
    var maximumFailures = positive(options.maximumConnectFailures, MAXIMUM_CONNECT_FAILURES);
    var page = pageLifecycle(options);
    var onState = options.onState || function () {};
    var stopped = false;
    var ended = false;
    var backoff = initial;
    var active = null;
    // When the chain of usable clients began. A renewal continues the chain; only an outage ends it.
    var healthySince = null;
    var failures = 0;
    // The relay's two temporary refusals are counted apart from `failures` (`countFailure`).
    var rateRefusals = 0;
    var forbiddenRefusals = 0;
    // Parks that 4403s caused since the last connection that stayed up `stableMs` (`parkUntilReturn`).
    var forbiddenParks = 0;
    // Out of attempts: nothing is open and nothing is timed until the page is back (`parkUntilReturn`).
    var parked = false;
    // Parked by 4403s `MAXIMUM_FORBIDDEN_PARKS` times: the page coming back no longer starts it.
    var parkedForGood = false;
    var lastAttemptAt = null;
    // Hidden past the grace period: the socket is retired and nothing connects until the page is back.
    var quiesced = false;
    var hiddenSince = page.hidden() ? now() : null;
    var graceTimer = null;
    // Set when the page returns from a hide long enough that its socket cannot be trusted — a phone
    // that froze this page never ran the grace timer, and its socket may be dead without saying so.
    var staleResume = false;
    var wakeListeners = new Set();

    function wake(reason) {
        Array.from(wakeListeners).forEach(function (listener) { listener(reason); });
    }

    function onWake(listener) {
        wakeListeners.add(listener);
        return function () { wakeListeners.delete(listener); };
    }

    /** Wait `ms`, or less when something wakes the loop; answers `"elapsed"` or the wake reason. */
    function waitFor(ms, wakes) {
        return new Promise(function (resolve) {
            var timer = null;
            var done = false;
            function finish(reason) {
                if (done) return;
                done = true;
                off();
                if (timer !== null) clearTimer(timer);
                resolve(reason);
            }
            var off = onWake(function (reason) {
                if (stopped || quiesced || !wakes || wakes(reason)) finish(reason);
            });
            if (stopped || quiesced) { finish("stopped"); return; }
            if (injectedSleep) {
                Promise.resolve(injectedSleep(ms)).then(function () { finish("elapsed"); },
                    function () { finish("elapsed"); });
            } else {
                timer = setTimer(function () { timer = null; finish("elapsed"); }, Math.max(0, ms));
            }
        });
    }

    /**
     * The retry sleep. A page coming back into view, the network coming back, or a bfcache restore
     * may cut it short — that is "resume promptly" — but never to less than `wakeSpacingMs` after the
     * attempt it follows, so a storm of such events cannot become a storm of connects.
     */
    async function backoffWait(delay) {
        var reason = await waitFor(delay, function () { return true; });
        if (reason === "elapsed" || stopped || quiesced) return;
        var since = lastAttemptAt === null ? wakeSpacingMs : now() - lastAttemptAt;
        if (since < wakeSpacingMs) await waitFor(wakeSpacingMs - since, function () { return false; });
    }

    /** Nothing connects while quiesced, or while the browser says it has no network. */
    async function gate() {
        while (!stopped) {
            if (quiesced) {
                await new Promise(function (resolve) {
                    var off = onWake(function () {
                        if (stopped || !quiesced) { off(); resolve(); }
                    });
                    if (stopped || !quiesced) { off(); resolve(); }
                });
                continue;
            }
            if (page.offline()) {
                // `online` wakes this; the timer only reads the flag again, it connects nothing.
                await waitFor(OFFLINE_RECHECK_MS, function (reason) { return reason === "online"; });
                continue;
            }
            return;
        }
    }

    /**
     * Count one failed attempt where it belongs; answers the count that ends the attempts, or 0.
     *
     * 4429 (`rate_limited`, `over_capacity`) is the relay asking for less — a viewer-device seat
     * another device holds is freed when that device's page is put away or closed — so it keeps the
     * longest wait and its own, larger bound instead of spending the one a dead network gets. 4403
     * `forbidden` is a revoked device, and also every device of an account whose revocation set the
     * relay has saturated, which clears later and closes with the same code and words. A device that
     * really is revoked does not reach the relay again: the next attempt's `connect()` asks the API
     * for its session first (`ensureSession`), the API refuses a cookie naming a revoked device with
     * 401, and the loop ends at `sign_in` (a revocation landing between that request and the device
     * list or the token mint is refused there, as `revoked`). So only the other kind keeps reaching
     * the relay, and after a few tries it waits for the page instead (`parkUntilReturn`).
     *
     * None of these counts means "in a row": a connection that opens and soon closes again does not
     * clear them. Only a connection that stayed up `stableMs` does, or a parked loop starting again.
     */
    function countFailure(code) {
        if (RATE_REFUSALS.has(code)) return (rateRefusals += 1) >= MAXIMUM_RATE_REFUSALS ? rateRefusals : 0;
        if (code === "forbidden") {
            return (forbiddenRefusals += 1) >= MAXIMUM_FORBIDDEN_REFUSALS ? forbiddenRefusals : 0;
        }
        return (failures += 1) >= maximumFailures ? failures : 0;
    }

    /**
     * Out of attempts. The caller has said so (`terminal_error`, `retries_exhausted`); the loop then
     * holds no socket and no timer until the page is shown, restored from the back-forward cache or
     * back online, and starts over with its counts cleared — never sooner than `wakeSpacingMs`
     * after its last attempt. The door's own press starts a new loop and stops this one. Answers
     * false when stopped.
     *
     * `forbidden` (the attempts ran out on 4403) is bounded across returns as well, because a 4403
     * that no retry changes would otherwise cost a few refused attempts every time the page comes
     * back, for ever. Such a loop starts again with one attempt, not `MAXIMUM_FORBIDDEN_REFUSALS`;
     * and once 4403s have parked it `MAXIMUM_FORBIDDEN_PARKS` times without a connection staying up
     * `stableMs` in between, the page coming back no longer starts it — it lets go of the page and
     * waits only to be stopped, by the door's press or a reload. A saturated revocation set that
     * clears is met by the next return in time, and its stable connection gives the parks back.
     */
    async function parkUntilReturn(forbidden) {
        if (forbidden) forbiddenParks += 1;
        parked = true;
        parkedForGood = forbidden === true && forbiddenParks >= MAXIMUM_FORBIDDEN_PARKS;
        if (parkedForGood) { disarmGrace(); detach(); }
        await new Promise(function (resolve) {
            var off = onWake(function (reason) {
                if (stopped || (!parkedForGood && RESTART_WAKES.has(reason))) { off(); resolve(); }
            });
            if (stopped) { off(); resolve(); }
        });
        parked = false;
        if (stopped) return false;
        failures = 0;
        rateRefusals = 0;
        forbiddenRefusals = forbidden ? MAXIMUM_FORBIDDEN_REFUSALS - 1 : 0;
        backoff = initial;
        var since = lastAttemptAt === null ? wakeSpacingMs : now() - lastAttemptAt;
        if (since < wakeSpacingMs) await waitFor(wakeSpacingMs - since, function () { return false; });
        return !stopped;
    }

    function renewalDelay(outcome) {
        var relayRemaining = Number(outcome.renewInMs);
        var remaining = outcome.renewInMs !== null && outcome.renewInMs !== undefined &&
            Number.isFinite(relayRemaining) ? relayRemaining
            : Number.isFinite(outcome.expiresAt) ? outcome.expiresAt - now() : null;
        if (remaining === null) return null;
        // The floor: however wrong either clock is, a renewal never follows its own connect at once.
        return Math.max(renewalFloorMs, remaining - renewalLeadMs);
    }

    function nextBoundary(client, outcome) {
        return new Promise(function (resolve) {
            var settled = false;
            var timer = null;
            var off = client.events(function (event) {
                if (event.type === "connection" && event.state === "offline") {
                    finish({ reason: "offline", failure: typeof event.failure === "string" ? event.failure : null });
                }
            });
            var offWake = onWake(function () {
                if (stopped) finish({ reason: "stopped" });
                else if (quiesced) finish({ reason: "quiesce" });
                else if (staleResume) finish({ reason: "resume" });
            });
            function finish(result) {
                if (settled) return;
                settled = true;
                off();
                offWake();
                // The renewal timer belongs to this boundary: it must not keep a dead client alive.
                if (timer !== null) { timer.cancel(); timer = null; }
                resolve(result);
            }
            var delay = renewalDelay(outcome);
            if (delay !== null) {
                if (injectedSleep) {
                    Promise.resolve(injectedSleep(delay)).then(function () { finish({ reason: "renew" }); },
                        function () { finish({ reason: "renew" }); });
                } else {
                    var id = setTimer(function () { finish({ reason: "renew" }); }, delay);
                    timer = { cancel: function () { clearTimer(id); } };
                }
            }
            if (stopped) finish({ reason: "stopped" });
            else if (quiesced) finish({ reason: "quiesce" });
        });
    }

    function retireClient(client) {
        if (!client) return;
        if (typeof client.retire === "function") client.retire();
        else if (typeof client.stop === "function") client.stop();
    }

    /* ---- the page's lifecycle ------------------------------------------------------------- */

    function armGrace(delay) {
        if (graceTimer !== null || quiesced || stopped) return;
        graceTimer = setTimer(function () {
            graceTimer = null;
            tryQuiesce();
        }, delay);
    }

    function disarmGrace() {
        if (graceTimer === null) return;
        clearTimer(graceTimer);
        graceTimer = null;
    }

    /**
     * Hidden for the whole grace period: retire the socket. A person's own work still in the air —
     * a send, a keypress, anything answered as `action:` — is waited for, up to `quiesceDeferMs`, so
     * putting the phone away right after pressing Send does not turn the send into an unknown.
     */
    function tryQuiesce() {
        if (stopped || quiesced || !page.hidden()) return;
        var hiddenFor = hiddenSince === null ? hiddenGraceMs : now() - hiddenSince;
        if (hiddenFor < hiddenGraceMs) { armGrace(hiddenGraceMs - hiddenFor); return; }
        var client = active;
        var unsettled = client && typeof client._unsettledWork === "function" ? client._unsettledWork() : 0;
        if (unsettled > 0 && hiddenFor < hiddenGraceMs + quiesceDeferMs) {
            armGrace(Math.min(QUIESCE_RECHECK_MS, hiddenGraceMs + quiesceDeferMs - hiddenFor));
            return;
        }
        quiesced = true;
        wake("quiesce");
    }

    function onHidden() {
        if (stopped) return;
        if (hiddenSince === null) hiddenSince = now();
        armGrace(hiddenGraceMs);
    }

    /**
     * Visible, restored from the back-forward cache, back online, or asked by the page. Answers
     * whether a connection is live or on its way, which is what a retired client asks before it
     * holds a read for its replacement (`CloudClient._viaSuccessor`): `true`; `false` when none will
     * come — stopped, ended, or out of attempts; or `"hidden"` when one comes as soon as the page is
     * shown. A notification tap posts its `navigate` before `focus()` makes the page visible, so a
     * read asked in that moment is worth holding for a few seconds, not refusing.
     */
    function resume(reason) {
        if (stopped || ended) return false;
        if (parked && (parkedForGood || !RESTART_WAKES.has(reason))) return false;
        if (page.hidden()) {
            // `online` while hidden: a loop that is not quiesced may re-read the network flag.
            if (!quiesced) wake(reason);
            return "hidden";
        }
        disarmGrace();
        var hiddenFor = hiddenSince === null ? 0 : now() - hiddenSince;
        hiddenSince = null;
        if (quiesced) {
            quiesced = false;
            wake(reason);
            return true;
        }
        if (hiddenFor >= hiddenGraceMs && active) {
            staleResume = true;
            // Hidden past the grace with a socket that still says `ready`: the page was frozen
            // before its timer ran, and nothing proves that socket heard what was published
            // meanwhile. Its successor is not a renewal, and asks for the rows (`_becameReady`).
            try { active.continuityUnproven = true; } catch (e) { /* a frozen stand-in */ }
        }
        wake(reason);
        return true;
    }

    var detach = page.listen({
        hidden: onHidden,
        visible: function () { resume("visible"); },
        pageshow: function () { resume("pageshow"); },
        online: function () { resume("online"); }
    });
    if (hiddenSince !== null) armGrace(hiddenGraceMs);

    function attach(client) {
        if (!client || (typeof client !== "object" && typeof client !== "function")) return;
        // `main.js` calls `api.revalidate("visible")`; the client hands it here, where the socket lives.
        try { client.lifecycle = function (reason) { return resume(reason === "visible" || !reason ? "visible" : String(reason)); }; }
        catch (e) { /* a frozen stand-in has no hook, and needs none */ }
    }

    var loop = (async function () {
        while (!stopped) {
            await gate();
            if (stopped) return;
            var outcome = null;
            lastAttemptAt = now();
            try {
                outcome = await session.connect();
            } catch (error) {
                if (stopped) return;
                if (error && error.code === "revoked") {
                    onState({ state: "revoked", error: error });
                    return;
                }
                if (error && (error.terminal === true || PERMANENT_RELAY_REFUSALS.has(error.code))) {
                    // A relay that closed the handshake with 4400 or 4413 will close the next one the
                    // same way: this page speaks the protocol wrong.
                    onState({ state: "terminal_error", error: error });
                    return;
                }
                var serving = !!(active && active.ready);
                var exhausted = serving ? 0 : countFailure(error && error.code);
                if (exhausted) {
                    // Stops rather than knocking every thirty seconds for ever; the door says so and
                    // offers the press that starts again, and the page coming back starts it too — for
                    // 4403, only until `MAXIMUM_FORBIDDEN_PARKS` (`parkUntilReturn`).
                    onState({ state: "terminal_error", error: error, reason: "retries_exhausted",
                        attempts: exhausted });
                    if (await parkUntilReturn(!!error && error.code === "forbidden")) continue;
                    return;
                }
                if (error && (RATE_REFUSALS.has(error.code) || error.code === "forbidden")) backoff = maximum;
                var delay = Math.min(maximum, backoff) * (0.75 + jitter() * 0.5);
                // A proactive replacement is allowed to fail while the old credential is still
                // serving. Retrying that warm-up is not an outage and must not cover the usable
                // page with the reconnect error screen.
                if (!serving) {
                    onState({ state: "retrying", error: error, afterMs: Math.round(delay) });
                }
                await backoffWait(delay);
                backoff = Math.min(maximum, backoff * 2);
                continue;
            }
            if (stopped) {
                if (outcome && outcome.client && outcome.client !== active) retireClient(outcome.client);
                return;
            }
            if (outcome.state !== "connected") {
                onState(outcome);
                return;
            }
            if (quiesced) {
                // Hidden past the grace while this connect was in flight: keep nothing open.
                retireClient(outcome.client);
                if (outcome.previous && outcome.previous !== outcome.client) retireClient(outcome.previous);
                active = null;
                healthySince = null;
                continue;
            }
            if (!active || !active.ready || healthySince === null) healthySince = now();
            active = outcome.client;
            staleResume = false;
            attach(outcome.client);
            onState({ state: "connected", client: outcome.client });
            // A five-minute device token is a credential rotation boundary, not a user-visible
            // outage. The replacement has completed its signed handshake before `connect()`
            // returns, so only now retire the socket it superseded.
            if (outcome.previous && outcome.previous !== outcome.client) retireClient(outcome.previous);
            var boundary = await nextBoundary(outcome.client, outcome);
            if (stopped) return;
            // A connection that stayed up for `stableMs` clears the failure history, whatever ended
            // it. One that did not is counted as a failure, so accept-then-close cannot loop for ever.
            var stable = healthySince !== null && now() - healthySince >= stableMs;
            if (stable) {
                failures = 0;
                rateRefusals = 0;
                forbiddenRefusals = 0;
                forbiddenParks = 0;
                backoff = initial;
            }
            if (boundary.reason === "quiesce") {
                // The socket goes, the rows stay: `resumeFrom` seeds the next client with them and
                // the relay realigns every retained channel when the page is back.
                retireClient(outcome.client);
                active = null;
                healthySince = null;
                onState({ state: "paused" });
                continue;
            }
            if (boundary.reason === "offline") {
                active = null;
                healthySince = null;
                if (PERMANENT_RELAY_REFUSALS.has(boundary.failure)) {
                    onState({ state: "terminal_error",
                        error: bootError(boundary.failure, "the relay refused this viewer", { terminal: true }) });
                    return;
                }
                // Backoff forgets its history only for a connection that stayed up (above): a relay
                // that accepts and then closes at once is met with a growing wait, never a tight loop.
                var exhaustedAfter = stable ? 0 : countFailure(boundary.failure);
                if (exhaustedAfter) {
                    onState({ state: "terminal_error", reason: "retries_exhausted", attempts: exhaustedAfter,
                        error: bootError(boundary.failure || "offline", "the cloud connection kept dropping") });
                    if (await parkUntilReturn(boundary.failure === "forbidden")) continue;
                    return;
                }
                if (RATE_REFUSALS.has(boundary.failure) || boundary.failure === "forbidden") backoff = maximum;
                onState({ state: "reconnecting" });
                var wait = Math.min(maximum, backoff) * (0.75 + jitter() * 0.5);
                await backoffWait(wait);
                backoff = Math.min(maximum, backoff * 2);
            }
            // `renew` and `resume` connect again at once, with the current client still serving.
        }
    })();
    // A loop that ends on its own — pairing required, a terminal refusal — leaves no page listener or
    // grace timer behind; the next `keepConnected` a retry starts brings its own. One that ran out of
    // attempts has not ended: it keeps its page listeners, and only those, to start again — unless
    // 4403s parked it `MAXIMUM_FORBIDDEN_PARKS` times, when it lets go of those too (`parkUntilReturn`).
    function release() { ended = true; disarmGrace(); detach(); }
    loop.then(release, release);

    return {
        stop: function () {
            stopped = true;
            disarmGrace();
            detach();
            wake("stopped");
            // A loop that already ended on its own no longer owns the session's client: the next
            // loop's connect retires it once its replacement is ready (`main.js` stops the old loop
            // before starting one).
            if (session.client && !ended) session.client.stop();
        },
        done: loop
    };
}

const HIDDEN_GRACE_MS = 60 * 1000;
const STABLE_CONNECTION_MS = 60 * 1000;
const RENEWAL_FLOOR_MS = 60 * 1000;
const WAKE_SPACING_MS = 2000;
const QUIESCE_DEFER_MS = 60 * 1000;
const QUIESCE_RECHECK_MS = 5000;
const OFFLINE_RECHECK_MS = 60 * 1000;
const MAXIMUM_CONNECT_FAILURES = 16;
/**
 * Relay closes that no retry changes: 4400 and 4413 are this page's protocol. 4403 is not among
 * them — the relay also sends it while its revocation set is saturated (`countFailure`).
 */
const PERMANENT_RELAY_REFUSALS = new Set(["bad_request", "too_large"]);
/** 4429: the relay asked for less; the next attempt waits the longest. */
const RATE_REFUSALS = new Set(["rate_limited", "over_capacity"]);
/**
 * 4403s before the loop waits for the page, counted until a connection stays up `stableMs` or the
 * parked loop starts again. A revoked device never gets this far: its next `connect()` is refused
 * by the API with 401 and the loop ends at `sign_in` (`countFailure`).
 */
const MAXIMUM_FORBIDDEN_REFUSALS = 3;
/**
 * Parks caused by 4403, without a connection staying up `stableMs` between them, after which the
 * page coming back no longer starts the loop: at most 3 + 1 + 1 refused attempts (`parkUntilReturn`).
 */
const MAXIMUM_FORBIDDEN_PARKS = 3;
/** 4429s before the loop waits for the page, counted the same way: about twenty minutes at the longest wait. */
const MAXIMUM_RATE_REFUSALS = 40;
/** What starts a loop that ran out of attempts: the page back in view, restored, or back online. */
const RESTART_WAKES = new Set(["visible", "pageshow", "online"]);

function positive(value, fallback) {
    var number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : fallback;
}

/**
 * The document, window and navigator a reconnect loop watches, each injectable. A missing one —
 * node, a worker — is a page that is always visible and never says it is offline.
 */
function pageLifecycle(options) {
    var doc = options.document !== undefined ? options.document : globalThis.document;
    var win = options.window !== undefined ? options.window
        : (typeof globalThis.addEventListener === "function" ? globalThis : null);
    var nav = options.navigator !== undefined ? options.navigator : globalThis.navigator;
    function hidden() {
        return !!doc && (doc.hidden === true || doc.visibilityState === "hidden");
    }
    return {
        hidden: hidden,
        offline: function () { return !!nav && nav.onLine === false; },
        listen: function (on) {
            var removals = [];
            function add(target, name, handler) {
                if (!target || typeof target.addEventListener !== "function") return;
                try {
                    target.addEventListener(name, handler);
                    removals.push(function () {
                        try { target.removeEventListener(name, handler); } catch (e) { }
                    });
                } catch (e) { /* a target that refuses listeners is a page with no lifecycle */ }
            }
            add(doc, "visibilitychange", function () { if (hidden()) on.hidden(); else on.visible(); });
            add(win, "pagehide", function () { on.hidden(); });
            add(win, "pageshow", function () { if (!hidden()) on.pageshow(); });
            add(win, "online", function () { on.online(); });
            return function () { removals.forEach(function (remove) { remove(); }); };
        }
    };
}

/** A transport that satisfies the seam and does nothing, held while the cloud one boots. */
export function idleClient() {
    var listeners = new Set();
    function offline() {
        return Promise.reject(Object.assign(
            bootError("cloud_starting", "the cloud connection is not ready"), { retryable: true }));
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
        board: offline,
        boardCommand: offline,
        documents: offline,
        document: offline,
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
