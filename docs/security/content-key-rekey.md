# Content-key rekey control contract v1 (inactive)

This document defines pure Go and browser cryptography only. No producer, relay
frame handler, or hosted viewer is enabled by this change. Existing services are
not asserted to accept `content_key_offer` or `content_key_answer`. Future-content
revocation is not complete until runtime rotation, publication barriers and
revocation authority have been integrated and accepted.

## Actors, assets and authority

The viewer B owns a pairing-pinned Ed25519 key and a fresh pending X25519 private
key. The machine owns its pairing-pinned Ed25519 key and the durable current
content-key state. A revoked viewer A may know every old content key and observe
or modify relay traffic. Cloud sees routing metadata and ciphertext and is not
trusted to authorize a viewer or deliver frames honestly.

`VerifyRekeyOffer` / `verifyRekeyOffer` take an independently authenticated
account, machine, viewer identity and an active, non-legacy viewer pin. The
packet does not establish these facts. The machine caller must reject an absent,
revoked, unknown or legacy pin before invoking the primitive. Nil/empty keys
are rejected, but this module cannot discover that an otherwise valid key was
revoked. Account roster membership and `send_prompt` are not required authority
for this read-only key exchange. Read permission and local pin eligibility must
be checked by the server, never inferred from UI state.

`SealRekeyAnswer` rechecks the offer. Runtime must serialize pin revocation,
authorization, durable rotation, answer sealing and delivery; a check followed by
a revocation race is not sufficient. No function takes an old content key.
`OpenRekeyAnswer` requires the exact locally pending signed offer, its private
X25519 key, the pairing-pinned machine Ed25519 public key and the durable stored
epoch/key ID. A wrong private recipient cannot recover the key.

## Closed schema and canonical bytes

All wire objects must arrive in canonical sorted-key JSON with no whitespace.
Unknown, missing or duplicate fields and alternate JSON spellings are rejected.
Integers are non-negative ECMAScript safe integers; epochs are at least 1.
Identifier strings have 1–128 printable ASCII bytes (33–126), excluding `<`, `>`
and `&`. Binary fields use canonical padded standard base64, not base64url.
The maximum encoded message is 4096 bytes. Go `RekeyCanonical` and browser
`rekeyCanonical` define the sorted JSON spelling for these restricted values.

An offer has exactly these fields:

| Field | Value |
|---|---|
| type / v | `content_key_offer` / 1 |
| account_id / machine_id / viewer_device_id | Authenticated routing context |
| operation_id | Fresh operation identifier, not a source of authority |
| from_epoch / from_key_id | Viewer's previous state, hints only |
| viewer_ephemeral_key | Fresh 32-byte X25519 public key |
| nonce | Fresh random 32-byte operation nonce |
| expires_at_ms | Absolute integer timestamp, strictly after now, at most 120000 ms ahead |
| signature | Viewer Ed25519 signature, 64 bytes |

An answer has exactly `type`, `v`, `offer`, `offer_hash`, `sender_device_id`,
`machine_ephemeral_key`, `epoch`, `key_id`, `sealing_nonce`, `ciphertext`,
`signature`. Type is `content_key_answer`, version is 1. `offer` is the complete
signed offer above; it binds account, machine, viewer, operation, nonce, expiry
and B's ephemeral. `offer_hash` is SHA-256 of the complete canonical signed offer
(32 bytes). Sender must equal the authenticated machine ID. The machine ephemeral
is a fresh 32-byte X25519 public key. The sealing nonce is 12 random bytes.
Ciphertext is the encrypted 32-byte content key followed by the 16-byte GCM tag.

The header H is the answer object with `ciphertext` and `signature` removed.
The offer signing object is the offer with `signature` removed. The answer
signing object is the answer with only `signature` removed (ciphertext included).
Every domain below means the exact UTF-8 prefix followed by the canonical JSON
bytes, with one literal zero byte after `v1`:

| Use | Prefix |
|---|---|
| Offer signature | `clawdline-content-key-offer-v1\0` |
| Answer signature | `clawdline-content-key-answer-v1\0` |
| HKDF info | `clawdline-content-key-kdf-v1\0` |
| GCM AAD | `clawdline-content-key-aad-v1\0` |

The prefix notation `\0` denotes a zero byte, not backslash plus zero.
HKDF-SHA256 uses X25519(B private, machine public) as IKM, the decoded offer hash
as salt, and the KDF prefix plus canonical H as info; output is 32 bytes.
Go uses the same two-HMAC one-block HKDF construction as pairing; the browser
uses native WebCrypto HKDF. AES-256-GCM uses that output, the sealing nonce,
AAD prefix plus canonical H and a 128-bit tag. The machine signs after sealing.
Machine signature verification occurs before key agreement/decryption.

Both sides reject unusable X25519 agreements. Production callers must generate
fresh ephemeral private keys and nonces with the platform CSPRNG, never reuse
an answer ephemeral/nonce pair. Browser private X25519 keys must be extractable
for local public-key matching; they are never serialized into the wire packet.
Go reuses `X25519PublicKey` and `PairingSharedSecret` from pairing.

`epoch` must be no lower than either the offer hint or durable stored epoch.
At the same epoch the key ID must agree with the corresponding known state.
Epoch 1 is `ms-1`; later epochs are `mk-<decimal epoch>-<full lowercase SHA256(key)>`,
matching ContentKeyStore. The decrypted key must be exactly 32 bytes and match
this ID. The wire header publishes a key hash as part of this existing ID scheme;
it is not a secret-key confidentiality claim against low-entropy supplied keys.

## Replay, persistence and deployment owners

This stateless module only matches a pending operation. A nonce string is not a
replay cache. The runtime owner must bound outstanding operations, rate-limit
requests, persist the authority and epoch floor across restarts, expire pending
entries, and atomically consume the pending operation with durable key adoption.
Answers after expiry are rejected, including at the exact expiry timestamp.
Duplicate delivery must not remint keys or undo a later epoch. Do not implement
an unbounded in-memory nonce set. Restarting without an old content key still
permits verification using the persistent viewer signing pin.

Offers and answer headers are public metadata, including identities, hints,
operation timing and ephemeral keys. Offers are signed, not encrypted. Only the
new content key is confidential under the verified fresh X25519-derived key;
there is no public fixed AES key and no old-group-key wrapping layer.

Deployment order: first deploy additive relay frame validation/routing and hosted
viewer support with no producer enabled. Next integrate local active pin checks,
bounded operation state, ContentKeyStore recovery/rotation and the publication
barrier. Finally enable the producer after cross-version acceptance, including
read-only B, revoked A, restart without old keys, dropped/duplicated messages and
crash recovery. Do not roll back durable epoch floors on compatibility failures.

## Evidence and limits

`internal/domain/cloud/rekey_test.go` independently produces signatures and GCM
bytes in Go and compares them to the public synthetic WebCrypto fixture;
`rekey-crypto.test.ts` independently produces the same bytes in WebCrypto and
opens the Go-equivalent answer. Both exercise exact-key recovery, tampering,
wrong recipients, missing active keys and rollback. The fixture contains only
fixed synthetic seeds and keys; it must never contain a production credential.
No test here proves production pin freshness, relay acceptance, durable replay
handling or future-content revocation. Those belong to runtime integration.
