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
const INVITATION_AAD = "clawdline-pairing-invitation-v1\0";

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
const INVITATION_MEMBERS = ["v", "type", "invitation_id", "secret", "expires_at"];

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

/* ---- the Mac-displayed QR invitation ------------------------------------ */

export function decodePairingInvitation(fragment, nowMilliseconds) {
    var text = decoder.decode(base64URLBytes(fragment));
    var invitation;
    try { invitation = JSON.parse(text); } catch (e) {
        throw pairingError("bad_invitation", "the QR invitation is not JSON");
    }
    if (!invitation || typeof invitation !== "object" || Array.isArray(invitation)) {
        throw pairingError("bad_invitation", "the QR invitation is not an object");
    }
    exactMembers(invitation, INVITATION_MEMBERS, "bad_invitation");
    if (invitation.v !== 1 || invitation.type !== "pairing_invitation" ||
        canonicalJSON(invitation) !== text || typeof invitation.invitation_id !== "string" ||
        !invitation.invitation_id || invitation.invitation_id.length > 128) {
        throw pairingError("bad_invitation", "the QR invitation is not canonical version 1");
    }
    invitation.secretBytes = requireLength(
        base64Bytes(invitation.secret, "invitation_secret"), 32, "invitation_secret");
    if (!Number.isSafeInteger(invitation.expires_at) || invitation.expires_at < nowMilliseconds) {
        throw pairingError("invitation_expired", "that QR invitation has expired");
    }
    return invitation;
}

/** Encrypt the viewer's ordinary offer for the QR-scanning Mac. */
export async function encryptPairingOfferForInvitation(invitation, offerFragment, nonce) {
    if (!invitation || !(invitation.secretBytes instanceof Uint8Array)) {
        throw pairingError("bad_invitation", "the QR invitation has no secret");
    }
    var iv = nonce ? requireLength(nonce, 12, "invitation_nonce")
        : crypto.getRandomValues(new Uint8Array(12));
    var key = await subtle().importKey(
        "raw", invitation.secretBytes, { name: "AES-GCM" }, false, ["encrypt"]);
    var ciphertext = new Uint8Array(await subtle().encrypt({
        name: "AES-GCM", iv: iv,
        additionalData: encoder.encode(INVITATION_AAD + invitation.invitation_id),
        tagLength: 128
    }, key, encoder.encode(offerFragment)));
    return {
        encryptedOffer: bytesBase64(concat([iv, ciphertext])),
        secretHash: bytesBase64(await sha256(invitation.secretBytes))
    };
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

/* ---- identity pairing: the four strict phases ----------------------------

   The mirror of `internal/domain/cloud/health_pairing.go`. The Mac starts the
   pairing and shows a QR (`#pairid=`); this viewer writes `offer`, opens the
   Mac's `grant`, proves in `activate` that it holds the master secret it just
   opened (a key confirmation the Mac verifies before it signs Cloud's
   handover statement), and keeps the keys only after the Mac's `confirm`
   names that exact activate. The master secret is imported twice — AES-GCM
   for the relay, HKDF for sealed diagnostics — both non-extractable, and the
   raw bytes are zeroed before this returns. */

const IDENTITY_QR_MEMBERS = [
    "v", "type", "pairing_id", "claim_nonce", "expires_at", "account_id", "machine_id",
    "machine_signing_key", "machine_fingerprint", "machine_ephemeral_key", "pairing_nonce"
];
const IDENTITY_GRANT_MEMBERS = [
    "v", "type", "pairing_id", "account_id", "machine_id", "machine_signing_key",
    "machine_fingerprint", "viewer_device_id", "offer_sha256", "content_key_epoch",
    "identity_epoch", "master_secret"
];
const IDENTITY_CONFIRM_MEMBERS = [
    "v", "type", "pairing_id", "account_id", "machine_id", "viewer_device_id",
    "content_key_epoch", "activate_sha256"
];
export const KEY_CONFIRMATION_INFO = "clawdline/content-key-confirm/v1";
const KEY_CONFIRMATION_CONTEXT = "clawdline-content-key-confirm-v1\n";
const SHORT_CODE_CONTEXT = "clawdline-identity-pairing-code-v1\n";

function hexOf(bytes) {
    return Array.from(bytes, function (b) { return b.toString(16).padStart(2, "0"); }).join("");
}

/** Cloud's `phase_sha256`: hex SHA-256 of the canonical wrapper. */
export async function identityPhaseDigest(wrapper) {
    return hexOf(await sha256(canonicalBytes(wrapper)));
}

export function decodeIdentityPairingQR(fragment, nowMilliseconds) {
    var text = decoder.decode(base64URLBytes(fragment));
    var qr;
    try { qr = JSON.parse(text); } catch (e) {
        throw pairingError("bad_qr", "the pairing QR is not JSON");
    }
    if (!qr || typeof qr !== "object" || Array.isArray(qr)) throw pairingError("bad_qr", "the pairing QR is not an object");
    exactMembers(qr, IDENTITY_QR_MEMBERS, "bad_qr");
    if (qr.v !== 1 || qr.type !== "pairing_qr" || canonicalJSON(qr) !== text) {
        throw pairingError("bad_qr", "the pairing QR is not canonical version 1");
    }
    requireLength(base64Bytes(qr.claim_nonce, "claim_nonce"), 32, "claim_nonce");
    requireLength(base64Bytes(qr.pairing_nonce, "pairing_nonce"), 32, "pairing_nonce");
    requireLength(base64Bytes(qr.machine_ephemeral_key, "machine_ephemeral_key"), 32, "machine_ephemeral_key");
    if (qr.claim_nonce === qr.pairing_nonce) throw pairingError("bad_qr", "the pairing nonces repeat");
    if (!Number.isSafeInteger(qr.expires_at) || qr.expires_at < nowMilliseconds) {
        throw pairingError("pairing_expired", "that pairing QR has expired");
    }
    return qr;
}

/** Verifies the QR's machine fingerprint; a separate async step because hashing is async. */
export async function checkIdentityPairingQR(qr) {
    var key = requireLength(base64Bytes(qr.machine_signing_key, "machine_signing_key"), 32, "machine_signing_key");
    if (qr.machine_fingerprint !== await ed25519Fingerprint(key)) {
        throw pairingError("bad_fingerprint", "the machine fingerprint does not match its key");
    }
    return qr;
}

/** The six digits both screens show. A comparison, not a secret. */
export async function identityShortCode(qr, viewerSigningKey, viewerEphemeralKey) {
    var parts = [qr.pairing_id, qr.machine_signing_key, qr.machine_ephemeral_key, viewerSigningKey, viewerEphemeralKey]
        .map(function (field) { return l16(encoder.encode(field)); });
    var digest = await sha256(concat([encoder.encode(SHORT_CODE_CONTEXT)].concat(parts)));
    var n = ((digest[0] << 24) >>> 0) + (digest[1] << 16) + (digest[2] << 8) + digest[3];
    return String(n % 1000000).padStart(6, "0");
}

async function identityPhaseAEAD(qr, phase, privateKey, peerEphemeral) {
    var phaseKey = await derivePhaseKey({
        shared: await sharedSecret(privateKey, base64Bytes(peerEphemeral, "ephemeral_key")),
        pairingNonce: base64Bytes(qr.pairing_nonce, "pairing_nonce"),
        pairingID: qr.pairing_id,
        claimNonce: base64Bytes(qr.claim_nonce, "claim_nonce"),
        phase: phase
    });
    var key = await subtle().importKey("raw", phaseKey, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
    phaseKey.fill(0);
    return key;
}

function wrapperAAD(wrapper) {
    return canonicalBytes({
        v: 1, phase: wrapper.phase, pairing_id: wrapper.pairing_id,
        sender_device_id: wrapper.sender_device_id, ephemeral_key: wrapper.ephemeral_key
    });
}

async function sealIdentityPhase(qr, phase, senderDeviceID, privateKey, ownEphemeral, peerEphemeral, plaintext) {
    var nonce = crypto.getRandomValues(new Uint8Array(12));
    var wrapper = { v: 1, phase: phase, pairing_id: qr.pairing_id, sender_device_id: senderDeviceID,
        ephemeral_key: ownEphemeral, nonce: bytesBase64(nonce), ct: "" };
    var key = await identityPhaseAEAD(qr, phase, privateKey, peerEphemeral);
    wrapper.ct = bytesBase64(new Uint8Array(await subtle().encrypt(
        { name: "AES-GCM", iv: nonce, tagLength: 128, additionalData: wrapperAAD(wrapper) }, key, plaintext)));
    return wrapper;
}

async function openIdentityPhase(qr, wrapper, phase, wantSender, wantPeer, privateKey, members, type) {
    if (!wrapper || typeof wrapper !== "object" || Array.isArray(wrapper)) throw pairingError("bad_wrapper", "the phase is not an object");
    exactMembers(wrapper, WRAPPER_MEMBERS, "bad_wrapper");
    if (wrapper.v !== 1 || wrapper.phase !== phase || wrapper.pairing_id !== qr.pairing_id) {
        throw pairingError("bad_wrapper", "the phase is not this pairing's " + phase);
    }
    if (wrapper.sender_device_id !== wantSender) throw pairingError("wrong_sender", "the phase was sealed by a different device");
    if (wrapper.ephemeral_key !== wantPeer) throw pairingError("wrong_key", "the phase is bound to another ephemeral key");
    var key = await identityPhaseAEAD(qr, phase, privateKey, wrapper.ephemeral_key);
    var clear;
    try {
        clear = new Uint8Array(await subtle().decrypt({
            name: "AES-GCM", iv: requireLength(base64Bytes(wrapper.nonce, "nonce"), 12, "nonce"),
            tagLength: 128, additionalData: wrapperAAD(wrapper)
        }, key, base64Bytes(wrapper.ct, "ct")));
    } catch (e) {
        throw pairingError("bad_phase", "the sealed " + phase + " did not authenticate");
    }
    var text = decoder.decode(clear);
    clear.fill(0);
    var doc;
    try { doc = JSON.parse(text); } catch (e) { throw pairingError("bad_phase", "the " + phase + " is not JSON"); }
    if (!doc || typeof doc !== "object" || Array.isArray(doc)) throw pairingError("bad_phase", "the " + phase + " is not an object");
    exactMembers(doc, members, "bad_phase");
    if (doc.v !== 1 || doc.type !== type || canonicalJSON(doc) !== text) {
        throw pairingError("bad_phase", "the " + phase + " is not canonical version 1");
    }
    return doc;
}

/**
 * The offer this viewer writes. `signingPublicKey` is its registered Ed25519 key (32 bytes).
 * The returned private ephemeral key is non-extractable and lives only for this pairing.
 */
export async function createIdentityOffer(input) {
    var qr = input.qr;
    var signing = requireLength(input.signingPublicKey, 32, "viewer_signing_key");
    var pair = await subtle().generateKey({ name: "X25519" }, false, ["deriveBits"]);
    var ephemeralPublic = bytesBase64(new Uint8Array(await subtle().exportKey("raw", pair.publicKey)));
    var signingB64 = bytesBase64(signing);
    var offer = {
        v: 1, type: "identity_offer", pairing_id: qr.pairing_id, account_id: qr.account_id,
        machine_id: qr.machine_id, viewer_device_id: input.deviceID, viewer_signing_key: signingB64,
        viewer_fingerprint: await ed25519Fingerprint(signing), viewer_ephemeral_key: ephemeralPublic
    };
    var wrapper = await sealIdentityPhase(qr, "offer", input.deviceID, pair.privateKey, ephemeralPublic,
        qr.machine_ephemeral_key, canonicalBytes(offer));
    return {
        wrapper: wrapper,
        offerSHA256: await identityPhaseDigest(wrapper),
        ephemeralPrivateKey: pair.privateKey,
        ephemeralPublicKey: ephemeralPublic,
        signingKey: signingB64,
        fingerprint: offer.viewer_fingerprint,
        shortCode: await identityShortCode(qr, signingB64, ephemeralPublic)
    };
}

/**
 * Opens the Mac's grant, imports the master secret as two non-extractable keys, and seals the
 * activate with the key confirmation. Nothing is persisted here; see `openIdentityConfirm`.
 */
export async function openIdentityGrant(input) {
    var qr = input.qr;
    var offered = input.offered;
    var grant = await openIdentityPhase(qr, input.wrapper, "grant", qr.machine_id, qr.machine_ephemeral_key,
        offered.ephemeralPrivateKey, IDENTITY_GRANT_MEMBERS, "identity_grant");
    if (grant.pairing_id !== qr.pairing_id || grant.account_id !== qr.account_id || grant.machine_id !== qr.machine_id ||
        grant.machine_signing_key !== qr.machine_signing_key || grant.machine_fingerprint !== qr.machine_fingerprint ||
        grant.viewer_device_id !== input.deviceID || grant.offer_sha256 !== offered.offerSHA256) {
        throw pairingError("wrong_transcript", "the grant does not answer this offer");
    }
    if (!Number.isSafeInteger(grant.content_key_epoch) || grant.content_key_epoch < 1 ||
        !Number.isSafeInteger(grant.identity_epoch) || grant.identity_epoch < 0) {
        throw pairingError("bad_epoch", "the grant names no usable content-key epoch");
    }
    var raw = requireLength(base64Bytes(grant.master_secret, "master_secret"), 32, "master_secret");
    grant.master_secret = "";
    var masterKey, hkdfKey, contentKeyID;
    try {
        contentKeyID = grant.content_key_epoch === 1 ? "ms-1" :
            "mk-" + grant.content_key_epoch + "-" + hexOf(await sha256(raw));
        masterKey = await importMasterSecret(raw);
        hkdfKey = await subtle().importKey("raw", raw, { name: "HKDF" }, false, ["deriveKey", "deriveBits"]);
    } finally {
        raw.fill(0);
    }
    var grantSHA256 = await identityPhaseDigest(input.wrapper);
    var confirmKey = await subtle().deriveKey(
        { name: "HKDF", hash: "SHA-256", salt: new Uint8Array(32), info: encoder.encode(KEY_CONFIRMATION_INFO) },
        hkdfKey, { name: "HMAC", hash: "SHA-256", length: 256 }, false, ["sign"]);
    var transcript = concat([encoder.encode(KEY_CONFIRMATION_CONTEXT), canonicalBytes({
        schema: "clawdline.content-key-confirm/1", account_id: qr.account_id, pairing_id: qr.pairing_id,
        machine_id: qr.machine_id, viewer_device_id: input.deviceID, viewer_signing_key: offered.signingKey,
        content_key_epoch: grant.content_key_epoch, identity_epoch: grant.identity_epoch, grant_sha256: grantSHA256
    })]);
    var confirmation = bytesBase64(new Uint8Array(await subtle().sign("HMAC", confirmKey, transcript)));
    var activate = {
        v: 1, type: "identity_activate", pairing_id: qr.pairing_id, account_id: qr.account_id,
        machine_id: qr.machine_id, viewer_device_id: input.deviceID, content_key_epoch: grant.content_key_epoch,
        grant_sha256: grantSHA256, key_confirmation: confirmation
    };
    var wrapper = await sealIdentityPhase(qr, "activate", input.deviceID, offered.ephemeralPrivateKey,
        offered.ephemeralPublicKey, qr.machine_ephemeral_key, canonicalBytes(activate));
    return {
        wrapper: wrapper,
        activateSHA256: await identityPhaseDigest(wrapper),
        contentKeyEpoch: grant.content_key_epoch,
        identityEpoch: grant.identity_epoch,
        contentKeyID: contentKeyID,
        machineID: qr.machine_id,
        machineSigningKey: base64Bytes(qr.machine_signing_key, "machine_signing_key"),
        machineFingerprint: qr.machine_fingerprint,
        masterKey: masterKey,
        hkdfKey: hkdfKey
    };
}

/** Accepts the Mac's confirm only if it names this exact activate. */
export async function openIdentityConfirm(input) {
    var qr = input.qr;
    var doc = await openIdentityPhase(qr, input.wrapper, "confirm", qr.machine_id, qr.machine_ephemeral_key,
        input.offered.ephemeralPrivateKey, IDENTITY_CONFIRM_MEMBERS, "identity_confirm");
    if (doc.pairing_id !== qr.pairing_id || doc.account_id !== qr.account_id || doc.machine_id !== qr.machine_id ||
        doc.viewer_device_id !== input.deviceID || doc.content_key_epoch !== input.activated.contentKeyEpoch ||
        doc.activate_sha256 !== input.activated.activateSHA256) {
        throw pairingError("wrong_transcript", "the confirm does not name this activate");
    }
    return true;
}
