/**
 * The browser half of the viewer/Mac key handover, against the checked-in vectors.
 *
 * `Resources/web/app/js/net/cloud-pairing.js` is a mirror of `Sources/CloudHandover.swift`:
 * same canonical JSON, same salt and info preimages, same AES-GCM additional data. A mirror
 * that has drifted is the worst kind of broken here — nothing throws, pairing simply never
 * completes for a real person — so what is checked is not that the code runs but that it opens
 * bytes a *third* implementation produced. `tools/generate-protocol-vectors.swift` writes them,
 * `Tests/CloudLifecycleTests.swift` opens the same ones from Swift, and this file opens them
 * from JavaScript.
 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const vectors = JSON.parse(readFileSync("Tests/protocol-vectors.json", "utf8"));
const handover = vectors.pairing_handover;
assert.ok(handover, "the protocol vectors carry a pairing handover");

const pairing = await import("../Resources/web/app/js/net/cloud-pairing.js");

function bytes(base64) {
    return Uint8Array.from(Buffer.from(base64, "base64"));
}

/** WebCrypto imports a raw X25519 scalar only through PKCS#8; the prefix is OID 1.3.101.110. */
async function importEphemeral(base64) {
    const prefix = Buffer.from("302e020100300506032b656e04220420", "hex");
    return crypto.subtle.importKey("pkcs8",
        Buffer.concat([prefix, Buffer.from(base64, "base64")]),
        { name: "X25519" }, false, ["deriveBits"]);
}

/* ---- canonical JSON agrees with CloudCanonicalJSON ------------------------ */

const offer = pairing.decodePairingOffer(handover.offer_fragment, handover.now_milliseconds);
assert.equal(pairing.canonicalJSON(offer), handover.offer,
    "the decoded offer re-serializes to the exact canonical bytes Swift wrote");
assert.equal(pairing.base64URL(new TextEncoder().encode(handover.offer)),
    handover.offer_fragment, "and the fragment is that canonical JSON in base64url");

assert.equal(pairing.canonicalJSON({ b: 1, a: 2 }), '{"a":2,"b":1}',
    "members are ordered by key, not by insertion");
assert.equal(pairing.canonicalJSON({ "/": "a/b" }), '{"/":"a/b"}',
    "a solidus is never escaped — base64 is full of them");
assert.equal(
    pairing.canonicalJSON({ a: String.fromCharCode(1, 0x0a, 0x22, 0x5c) }),
    '{"a":"\\u0001\\n\\"\\\\"}',
    "control characters and the two short escapes take RFC 8785's spellings");

/* ---- the fingerprint agrees ---------------------------------------------- */

assert.equal(await pairing.ed25519Fingerprint(bytes(offer.viewer_signing_key)),
    offer.viewer_fingerprint, "the viewer fingerprint is the one the offer states");

/* ---- the KDF agrees ------------------------------------------------------- */

const viewerEphemeral = await importEphemeral(handover.viewer_ephemeral_private_key);
const machinePublic = await crypto.subtle.importKey("raw",
    bytes(handover.wrapper.ephemeral_key), { name: "X25519" }, false, []);
const shared = new Uint8Array(await crypto.subtle.deriveBits(
    { name: "X25519", public: machinePublic }, viewerEphemeral, 256));
const phaseKey = await pairing.derivePhaseKey({
    shared: shared,
    pairingNonce: bytes(offer.pairing_nonce),
    pairingID: offer.pairing_id,
    claimNonce: bytes(offer.claim_nonce),
    phase: "grant"
});
assert.equal(Buffer.from(phaseKey).toString("base64"), handover.phase_key,
    "the grant phase key derived here is the one Swift derived");

/* ---- the whole handover opens -------------------------------------------- */

const expected = JSON.parse(handover.handover);
const opened = await pairing.openPairingHandover({
    wrapper: handover.wrapper,
    offer: offer,
    ephemeralPrivateKey: viewerEphemeral,
    senderDeviceID: handover.sender_device_id,
    nowMilliseconds: handover.now_milliseconds
});
assert.equal(opened.accountID, expected.account_id, "the account comes through");
assert.equal(opened.machineID, expected.machine_id, "the machine comes through");
assert.equal(opened.keyID, expected.key_id, "so does the content key id");
assert.equal(opened.machineFingerprint, expected.machine_fingerprint,
    "and the fingerprint a person compares");
assert.equal(Buffer.from(opened.machineSigningKey).toString("base64"),
    expected.machine_signing_key, "the machine key pinned here is the machine's own");
assert.equal(opened.masterKey.extractable, false,
    "the account content key is imported so that this page cannot read it back out");
assert.deepEqual(opened.masterKey.usages.slice().sort(), ["decrypt", "encrypt"],
    "and it can still do the work the viewer needs");

/* ---- and refuses everything it should ------------------------------------ */

async function refuses(name, code, build) {
    await assert.rejects(build(), function (error) {
        assert.equal(error.code, code, name + " reports " + code + ", not " + error.code);
        return true;
    }, name);
}

await refuses("a flipped ciphertext bit", "bad_handover", function () {
    const flipped = Buffer.from(handover.wrapper.ct, "base64");
    flipped[0] ^= 0xff;
    return pairing.openPairingHandover({
        wrapper: { ...handover.wrapper, ct: flipped.toString("base64") },
        offer: offer, ephemeralPrivateKey: viewerEphemeral,
        senderDeviceID: handover.sender_device_id, nowMilliseconds: handover.now_milliseconds
    });
});

await refuses("a rewritten sender", "bad_handover", function () {
    return pairing.openPairingHandover({
        wrapper: { ...handover.wrapper, sender_device_id: "another-mac" },
        offer: offer, ephemeralPrivateKey: viewerEphemeral,
        senderDeviceID: "another-mac", nowMilliseconds: handover.now_milliseconds
    });
});

await refuses("a slot the control plane says a different device wrote", "wrong_sender",
    function () {
        return pairing.openPairingHandover({
            wrapper: handover.wrapper, offer: offer,
            ephemeralPrivateKey: viewerEphemeral, senderDeviceID: "someone-else",
            nowMilliseconds: handover.now_milliseconds
        });
    });

await refuses("a wrapper with an extra member", "bad_wrapper", function () {
    return pairing.openPairingHandover({
        wrapper: { ...handover.wrapper, extra: 1 }, offer: offer,
        ephemeralPrivateKey: viewerEphemeral, senderDeviceID: handover.sender_device_id,
        nowMilliseconds: handover.now_milliseconds
    });
});

await refuses("an offer that has already expired", "offer_expired", function () {
    return pairing.openPairingHandover({
        wrapper: handover.wrapper, offer: offer,
        ephemeralPrivateKey: viewerEphemeral, senderDeviceID: handover.sender_device_id,
        nowMilliseconds: offer.expires_at + 1
    });
});

assert.throws(function () {
    pairing.decodePairingOffer(handover.offer_fragment + "=", handover.now_milliseconds);
}, function (error) { return error.code === "bad_fragment"; },
"a fragment with padding is not the canonical base64url this protocol uses");

assert.throws(function () {
    pairing.decodePairingOffer(handover.offer_fragment, offer.expires_at + 1);
}, function (error) { return error.code === "offer_expired"; },
"and an expired offer is refused at decode as well as at open");

assert.throws(function () {
    pairing.decodePairingOffer(handover.offer_fragment,
        offer.expires_at - pairing.OFFER_LIFETIME_MS - 1);
}, function (error) { return error.code === "offer_lifetime"; },
"an offer claiming a longer life than the protocol allows is refused");

console.log("web cloud pairing tests passed");
