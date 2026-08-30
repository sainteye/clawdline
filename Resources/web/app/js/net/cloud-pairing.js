/* --------------------------------------------------------------------------
   Key handover, the browser half

   The mirror of `Sources/CloudHandover.swift`, byte for byte: same canonical
   JSON, same salt and info preimages, same AES-GCM AAD. Read that file first —
   it carries the reasoning, including why this is a single opaque blob over the
   control plane's three pairing calls rather than `CloudPairing`'s four phases,
   and why the viewer is the requester.

   Nothing here is new cryptography. Everything is X25519, SHA-256, HMAC-SHA256
   and AES-GCM through WebCrypto, arranged exactly as the Swift side arranges
   CryptoKit, so that a fixture sealed by one opens in the other.

   Every key this module produces is non-extractable. The account master secret
   ends up as an `AES-GCM` CryptoKey the page can encrypt and decrypt with and
   cannot read, and the viewer's signing key as an `Ed25519` CryptoKey it can
   sign with and cannot read. That is the property the whole viewer rests on:
   script injected into this origin can ask the key to do work while the page is
   open, and cannot carry the key away.
   -------------------------------------------------------------------------- */

import { base64Bytes, bytesBase64, importMasterSecret } from "./cloud-crypto.js";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/** The same 600 s window `CloudHandover.offerLifetimeMilliseconds` gives an offer. */
export const OFFER_LIFETIME_MS = 600000;

const SALT_DOMAIN = "clawdline-pair-salt-v1";
const INFO_DOMAIN = "clawdline-pair-v1";
const GRANT_PHASE = "grant";

const OFFER_MEMBERS = [
    "v", "type", "pairing_id", "claim_nonce", "pairing_nonce", "account_id",
    "viewer_device_id", "viewer_signing_key", "viewer_ephemeral_key",
    "viewer_fingerprint", "expires_at"
];
const HANDOVER_MEMBERS = [
    "v", "type", "account_id", "machine_id", "machine_signing_key",
    "machine_fingerprint", "key_id", "master_secret"
];
const WRAPPER_MEMBERS = [
    "v", "phase", "pairing_id", "sender_device_id", "ephemeral_key", "nonce", "ct"
];

export function pairingError(code, message) {
    var error = new Error(message || code);
    error.code = code;
    return error;
}

function subtle() {
    if (!globalThis.crypto || !globalThis.crypto.subtle) {
        throw pairingError("no_webcrypto", "WebCrypto is unavailable");
    }
    return globalThis.crypto.subtle;
}

/* ---- canonical JSON (RFC 8785 §3.2), the subset these shapes use ---------- */

/**
 * Written out rather than reached for through `JSON.stringify`, because the two agree today
 * and the contract is with `CloudCanonicalJSON.appendEscaped`, not with whichever engine is
 * running the page. Sorting is by UTF-16 code unit, which is what both sides do.
 */
export function canonicalJSON(value) {
    if (value === null) return "null";
    if (typeof value === "boolean") return value ? "true" : "false";
    if (typeof value === "number") {
        if (!Number.isSafeInteger(value)) {
            throw pairingError("bad_number", "canonical JSON here carries safe integers only");
        }
        return String(value);
    }
    if (typeof value === "string") return canonicalString(value);
    if (Array.isArray(value)) return "[" + value.map(canonicalJSON).join(",") + "]";
    if (typeof value === "object") {
        var keys = Object.keys(value).sort(compareUTF16);
        return "{" + keys.map(function (key) {
            return canonicalString(key) + ":" + canonicalJSON(value[key]);
        }).join(",") + "}";
    }
    throw pairingError("bad_value", "canonical JSON cannot carry that value");
}

function compareUTF16(a, b) {
    // `<` on strings is already a UTF-16 code-unit comparison in JavaScript; naming it keeps
    // the agreement with `lexicographicallyPrecedes` on `String.utf16` visible.
    return a < b ? -1 : a > b ? 1 : 0;
}

function canonicalString(text) {
    var out = '"';
    for (var i = 0; i < text.length; i += 1) {
        var unit = text.charCodeAt(i);
        var ch = text.charAt(i);
        if (ch === '"') out += '\\"';
        else if (ch === "\\") out += "\\\\";
        else if (unit === 0x08) out += "\\b";
        else if (unit === 0x09) out += "\\t";
        else if (unit === 0x0a) out += "\\n";
        else if (unit === 0x0c) out += "\\f";
        else if (unit === 0x0d) out += "\\r";
        else if (unit < 0x20) out += "\\u00" + unit.toString(16).padStart(2, "0");
        else out += ch;
    }
    return out + '"';
}

export function canonicalBytes(value) {
    return encoder.encode(canonicalJSON(value));
}

/* ---- base64url, the QR/fragment alphabet --------------------------------- */

export function base64URL(bytes) {
    return bytesBase64(bytes).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function base64URLBytes(text) {
    if (typeof text !== "string" || !text || /[=+/]/.test(text) ||
        !/^[A-Za-z0-9_-]+$/.test(text) || text.length % 4 === 1) {
        throw pairingError("bad_fragment", "the pairing fragment is not canonical base64url");
    }
    var standard = text.replace(/-/g, "+").replace(/_/g, "/");
    standard += "=".repeat((4 - (standard.length % 4)) % 4);
    var bytes = base64Bytes(standard, "fragment");
    if (base64URL(bytes) !== text) {
        throw pairingError("bad_fragment", "the pairing fragment is not canonical base64url");
    }
    return bytes;
}

/* ---- primitives ---------------------------------------------------------- */

function l16(bytes) {
    if (bytes.length > 0xffff) throw pairingError("too_long", "L16 takes at most 65535 bytes");
    var out = new Uint8Array(bytes.length + 2);
    out[0] = (bytes.length >> 8) & 0xff;
    out[1] = bytes.length & 0xff;
    out.set(bytes, 2);
    return out;
}

function concat(parts) {
    var total = parts.reduce(function (sum, part) { return sum + part.length; }, 0);
    var out = new Uint8Array(total);
    var offset = 0;
    parts.forEach(function (part) { out.set(part, offset); offset += part.length; });
    return out;
}

async function sha256(bytes) {
    return new Uint8Array(await subtle().digest("SHA-256", bytes));
}

async function hmacSHA256(keyBytes, dataBytes) {
    var key = await subtle().importKey("raw", keyBytes,
        { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
    return new Uint8Array(await subtle().sign("HMAC", key, dataBytes));
}

/** The `ed25519Fingerprint` of `CloudPairing`: 10 digest bytes, base32, groups of four. */
export async function ed25519Fingerprint(publicKeyRaw) {
    var bytes = requireLength(publicKeyRaw, 32, "signing_key");
    var digest = (await sha256(bytes)).subarray(0, 10);
    var alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    var accumulator = 0;
    var bits = 0;
    var out = "";
    for (var i = 0; i < digest.length; i += 1) {
        accumulator = ((accumulator << 8) | digest[i]) >>> 0;
        bits += 8;
        while (bits >= 5) {
            bits -= 5;
            out += alphabet[(accumulator >>> bits) & 0x1f];
        }
    }
    if (bits > 0) out += alphabet[(accumulator << (5 - bits)) & 0x1f];
    var groups = [];
    for (var start = 0; start < out.length; start += 4) groups.push(out.slice(start, start + 4));
    return groups.join("-");
}

function requireLength(value, length, field) {
    var bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
    if (bytes.length !== length) {
        throw pairingError("bad_length", field + " must be " + length + " bytes");
    }
    return bytes;
}

async function sharedSecret(privateKey, peerPublicRaw) {
    var peer = await subtle().importKey("raw", requireLength(peerPublicRaw, 32, "peer_public_key"),
        { name: "X25519" }, false, []);
    var bits = new Uint8Array(await subtle().deriveBits({ name: "X25519", public: peer },
        privateKey, 256));
    if (bits.length !== 32 || bits.every(function (byte) { return byte === 0; })) {
        throw pairingError("zero_shared_secret", "the X25519 agreement produced an all-zero secret");
    }
    return bits;
}

/**
 * `CloudPairing.derive`, exactly. The salt commits to the pairing nonce, the pairing id and the
 * claim nonce, so a phase key belongs to one handover and to no other.
 */
export async function derivePhaseKey(options) {
    var salt = await sha256(concat([
        l16(encoder.encode(SALT_DOMAIN)),
        l16(requireLength(options.pairingNonce, 32, "pairing_nonce")),
        l16(encoder.encode(options.pairingID)),
        l16(requireLength(options.claimNonce, 32, "claim_nonce"))
    ]));
    var prk = await hmacSHA256(salt, options.shared);
    var info = concat([
        l16(encoder.encode(INFO_DOMAIN)),
        l16(encoder.encode(options.phase || GRANT_PHASE))
    ]);
    return hmacSHA256(prk, concat([info, new Uint8Array([0x01])]));
}

/* ---- the offer this viewer hands the Mac --------------------------------- */

function exactMembers(object, members, code) {
    var keys = Object.keys(object);
    if (keys.length !== members.length ||
        keys.some(function (key) { return members.indexOf(key) < 0; })) {
        throw pairingError(code, "the pairing document does not have its exact members");
    }
}

/**
 * Build the offer, its fragment, and the ephemeral private key that opens what comes back.
 *
 * `pairingID` and `claimNonce` are what `POST /v1/pairing/start` just returned; the ephemeral
 * key and the pairing nonce are made here and never leave this page except as public bytes.
 */
export async function createPairingOffer(input) {
    var signingPublic = requireLength(input.signingPublicKey, 32, "viewer_signing_key");
    var claimNonce = base64Bytes(input.claimNonce, "claim_nonce");
    requireLength(claimNonce, 32, "claim_nonce");
    var pairingNonce = crypto.getRandomValues(new Uint8Array(32));
    // `CloudPairing.generateIndependentNonces` refuses a collision rather than assuming one
    // cannot happen; a 32-byte repeat here would make the salt say less than it looks like.
    while (bytesBase64(pairingNonce) === input.claimNonce) {
        pairingNonce = crypto.getRandomValues(new Uint8Array(32));
    }
    var pair = await subtle().generateKey({ name: "X25519" }, false, ["deriveBits"]);
    var ephemeralPublic = new Uint8Array(await subtle().exportKey("raw", pair.publicKey));
    var offer = {
        v: 1,
        type: "pairing_offer",
        pairing_id: input.pairingID,
        claim_nonce: input.claimNonce,
        pairing_nonce: bytesBase64(pairingNonce),
        account_id: input.accountID,
        viewer_device_id: input.deviceID,
        viewer_signing_key: bytesBase64(signingPublic),
        viewer_ephemeral_key: bytesBase64(ephemeralPublic),
        viewer_fingerprint: await ed25519Fingerprint(signingPublic),
        expires_at: input.expiresAt
    };
    exactMembers(offer, OFFER_MEMBERS, "bad_offer");
    return {
        offer: offer,
        fragment: base64URL(canonicalBytes(offer)),
        ephemeralPrivateKey: pair.privateKey
    };
}

export function decodePairingOffer(fragment, nowMilliseconds) {
    var text = decoder.decode(base64URLBytes(fragment));
    var offer;
    try { offer = JSON.parse(text); } catch (e) {
        throw pairingError("bad_offer", "the pairing offer is not JSON");
    }
    if (!offer || typeof offer !== "object" || Array.isArray(offer)) {
        throw pairingError("bad_offer", "the pairing offer is not an object");
    }
    exactMembers(offer, OFFER_MEMBERS, "bad_offer");
    if (offer.v !== 1 || offer.type !== "pairing_offer") {
        throw pairingError("bad_offer", "the pairing offer is not version 1");
    }
    if (canonicalJSON(offer) !== text) {
        throw pairingError("bad_offer", "the pairing offer is not canonical JSON");
    }
    validateOfferWindow(offer, nowMilliseconds);
    return offer;
}

function validateOfferWindow(offer, nowMilliseconds) {
    if (!Number.isSafeInteger(offer.expires_at) || offer.expires_at < 0) {
        throw pairingError("bad_offer", "the pairing offer has an unusable expiry");
    }
    if (!Number.isSafeInteger(nowMilliseconds) || nowMilliseconds < 0) {
        throw pairingError("bad_clock", "the pairing clock is unusable");
    }
    if (offer.expires_at < nowMilliseconds) {
        throw pairingError("offer_expired", "that pairing offer has expired");
    }
    if (offer.expires_at - nowMilliseconds > OFFER_LIFETIME_MS) {
        throw pairingError("offer_lifetime", "that pairing offer claims an unusable lifetime");
    }
}

/* ---- opening what the Mac left in the one slot --------------------------- */

/**
 * Open the sealed handover and return the account material, with the machine's signing key
 * already checked against the fingerprint it travels with.
 *
 * `senderDeviceID` is what `POST /v1/pairing/claim` reported wrote the slot. Comparing it to
 * the wrapper closes the one thing the AEAD cannot say by itself.
 */
export async function openPairingHandover(input) {
    var wrapper = input.wrapper;
    if (!wrapper || typeof wrapper !== "object" || Array.isArray(wrapper)) {
        throw pairingError("bad_wrapper", "the handover wrapper is not an object");
    }
    exactMembers(wrapper, WRAPPER_MEMBERS, "bad_wrapper");
    if (wrapper.v !== 1 || wrapper.phase !== GRANT_PHASE) {
        throw pairingError("bad_wrapper", "the handover wrapper is not a version 1 grant");
    }
    var offer = input.offer;
    validateOfferWindow(offer, input.nowMilliseconds);
    if (wrapper.pairing_id !== offer.pairing_id) {
        throw pairingError("wrong_pairing", "the handover names another pairing");
    }
    if (input.senderDeviceID && input.senderDeviceID !== wrapper.sender_device_id) {
        throw pairingError("wrong_sender", "the handover was sealed by a different device");
    }

    var machineEphemeral = base64Bytes(wrapper.ephemeral_key, "ephemeral_key");
    var phaseKey = await derivePhaseKey({
        shared: await sharedSecret(input.ephemeralPrivateKey, machineEphemeral),
        pairingNonce: base64Bytes(offer.pairing_nonce, "pairing_nonce"),
        pairingID: offer.pairing_id,
        claimNonce: base64Bytes(offer.claim_nonce, "claim_nonce"),
        phase: GRANT_PHASE
    });

    // The AAD is the five authenticated wrapper members, canonically. `nonce` and `ct` are
    // deliberately outside it, exactly as `CloudPairing.aad` builds it.
    var aad = canonicalBytes({
        v: 1,
        phase: wrapper.phase,
        pairing_id: wrapper.pairing_id,
        sender_device_id: wrapper.sender_device_id,
        ephemeral_key: wrapper.ephemeral_key
    });
    var key = await subtle().importKey("raw", phaseKey, { name: "AES-GCM" }, false, ["decrypt"]);
    var clear;
    try {
        clear = new Uint8Array(await subtle().decrypt({
            name: "AES-GCM",
            iv: requireLength(base64Bytes(wrapper.nonce, "nonce"), 12, "nonce"),
            tagLength: 128,
            additionalData: aad
        }, key, base64Bytes(wrapper.ct, "ct")));
    } catch (e) {
        throw pairingError("bad_handover", "the sealed handover did not authenticate");
    }

    var text = decoder.decode(clear);
    var handover;
    try { handover = JSON.parse(text); } catch (e) {
        throw pairingError("bad_handover", "the handover payload is not JSON");
    }
    if (!handover || typeof handover !== "object" || Array.isArray(handover)) {
        throw pairingError("bad_handover", "the handover payload is not an object");
    }
    exactMembers(handover, HANDOVER_MEMBERS, "bad_handover");
    if (handover.v !== 1 || handover.type !== "pairing_handover") {
        throw pairingError("bad_handover", "the handover payload is not version 1");
    }
    if (canonicalJSON(handover) !== text) {
        throw pairingError("bad_handover", "the handover payload is not canonical JSON");
    }
    if (handover.account_id !== offer.account_id) {
        throw pairingError("wrong_account", "the handover names another account");
    }
    var machineSigningKey = base64Bytes(handover.machine_signing_key, "machine_signing_key");
    requireLength(machineSigningKey, 32, "machine_signing_key");
    if (handover.machine_fingerprint !== await ed25519Fingerprint(machineSigningKey)) {
        throw pairingError("bad_fingerprint", "the machine fingerprint does not match its key");
    }
    var masterSecret = base64Bytes(handover.master_secret, "master_secret");
    requireLength(masterSecret, 32, "master_secret");

    return {
        accountID: handover.account_id,
        machineID: handover.machine_id,
        machineDeviceID: wrapper.sender_device_id,
        machineSigningKey: machineSigningKey,
        machineFingerprint: handover.machine_fingerprint,
        keyID: handover.key_id,
        // Imported before it is returned so no caller ever holds the raw bytes for longer than
        // this function does, and so what leaves here cannot be read back out.
        masterKey: await importMasterSecret(masterSecret)
    };
}
