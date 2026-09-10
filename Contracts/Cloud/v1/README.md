# Cloud v1 public contract candidate

Version **1.0.0**, status **candidate**. This package is independently consumable from a public
clone. `clawdline-cloud/docs/PROTOCOL.md` remains the normative prose authority until an explicit
cross-repository cutover ADR is recorded. This delivery changes no producer, deployed reader,
client floor, entitlement, revocation policy, or production source.

## Consume and reproduce

From the public repository (Python 3.10+ and Node 22+, standard libraries only):

```sh
python3 tools/cloud-contract-v1.py check
python3 tools/cloud-contract-v1.py generate --output /tmp/cloud-v1-new
python3 tools/cloud-contract-v1.py validate --kind envelope Contracts/Cloud/v1/fixtures/valid/stream-empty.json
node Tests/cloud-contract-v1.mjs
```

`generate` requires a new output directory and an existing parent. It validates the entire
manifest/corpus in memory, writes a temporary sibling, then publishes the complete directory with
an atomic no-replace rename: macOS `renamex_np(RENAME_EXCL)` or Linux libc
`renameat2(RENAME_NOREPLACE)`. It refuses existing output, including symlinks and an empty
directory created by another actor immediately before publication; the other inode is preserved.
Unsupported OS, missing native primitive, or unsupported filesystem returns
`cloud_contract_publication_unsupported` with no published output; there is no plain-rename
fallback. Temporary siblings are removed on ordinary failure. This is atomic visibility, not a
power-loss/fsync durability claim. `check` and `validate` do not need this publication primitive. For an update, generate into a new directory,
review it, and let the owner replace the old candidate; never overlay individual generated files.
`check --package <dir>` checks the exact complete path set and every byte, including empty negative
fixtures; missing, extra, symlink and corrupted files fail. No Git, sibling checkout, network,
Swift compile, or third-party Python/Node package is required by these commands.

`manifest.json` is the sole authoring source. It embeds public TEST-ONLY known-answer vectors
from the unchanged legacy fixture, closed schemas, vocabularies and negative-fixture recipes.
All other package files are generated; edit the manifest deliberately and update `source.sha256`
after review. The source digest is SHA-256 over the exact UTF-8 manifest bytes (canonical JSON
plus one LF). Fixture JSON/signing bytes have **no** trailing LF. Generated metadata JSON has one
LF; schema JSON is indented for review. `integrity.json` lists each generated/source file's bytes
and digest, excluding itself and `package.sha256`; the latter hashes exact `integrity.json` bytes.
This avoids a self-referential digest. Consumers pin the public commit, source digest and package
digest in their own trusted configuration. `--expected-source-digest` overrides the sibling pin;
a locally editable digest is integrity evidence, not independent authenticity or a signature.

The legacy vector object is preserved semantically as `vectors.json` (its formatting/digest is
different). The public legacy bytes remain at their original path and authority. No private
runtime dependency exists: provenance paths/digests are historical citations, never dereferenced
by the compiler/checks. Private mirror owners must copy this entire package byte-for-byte and
record its public source commit plus both digests; they must not generate private variants.

## What is closed, and what validation proves

The five schemas use JSON Schema draft 2020-12 with closed object shapes: `envelope`,
`pairing_offer`, `pairing_wrapper` (grant only), `pairing_handover`, and `pairing_receipt`.
The Python tool implements the explicitly enumerated schema keyword subset and refuses unknown
keywords. Fixed-size key/nonce/signature patterns encode their exact lengths and padding bits. For the
potentially large `ct` in **both envelope and pairing_wrapper**, the schema uses only a simple
alphabet/trailing-padding filter and character bounds. A JSON Schema pass alone is insufficient:
the consumer MUST decode standard base64, re-encode and compare the identical spelling, then
check decoded length (envelope 1..25,162,752 bytes; grant wrapper 16..25,162,752 bytes). This
round-trip rejects incomplete quanta, omitted/excess padding and nonzero unused bits; permissive
decoders such as `Buffer.from(text, "base64")` alone do not. Avoid repeated four-character regex
groups: they can overflow ECMAScript stacks on valid 4 MiB payloads. The focused runner checks
4 MiB, 16 MiB, the declared maximum and malformed neighbors without storing giant fixtures.
Additional relational checks verify pairing fingerprints and distinct offer nonces. These supplemental rules, raw-byte limits, duplicate-key rejection and canonical JSON
requirements still apply when using another JSON Schema validator; parsing with ordinary
`JSON.parse` before checking has already erased duplicate keys.

`validate` reports **structure_only**: it does not verify a signature, decrypt ciphertext, consult
paired pins, check a current clock/expiry/account context, apply caps/capabilities/revocation,
track sequence replay, persist a command, or prove an effect. All input files must pass before
one aggregate success is printed. Failures return exit 2, typed JSON on stderr and empty stdout.
Malformed schema patterns return `cloud_contract_unsupported_schema`; unknown `--kind` values
return `cloud_contract_unknown_kind`; CLI usage errors return `cloud_contract_cli_usage`.
The explicit `--help` success path prints human-readable help and is not a validation receipt.
`--expected-sha256` pins one input's exact bytes; multiple inputs with this flag are refused.
`--canonical` additionally requires exact canonical envelope serialization. Pairing bodies and
receipts always require canonical bytes. Unknown kinds, empty/missing/malformed/duplicate-key,
nonfinite/fractional/exponent numbers, unsafe integers, BOM/invalid UTF-8/lone surrogates, excessive
depth, oversized input and wrong digests fail closed. Source parsing is at most 2 MiB, input at
most 32 MiB, depth at most 64, and the generated package at most 16 MiB. These are tool safety
bounds, not a claim about every deployed decoder's current rejection behavior.

## Envelope and exact signed bytes

Exactly ten fields: `v,ch,seq,ts,class,key_id,nonce,ct,sender,sig`. `v=1`; `seq` and `ts` are
nonnegative safe integers through 9007199254740991; `ts` is Unix epoch milliseconds. Tokens are
ASCII 0x21..0x7e excluding `/` and `|`; channel segments and sender have maximum length 128,
key_id 64, and the whole channel 300. Spaces, raw Unicode and empty segments are rejected.
Percent-encoded logical Unicode identifiers remain literal ASCII bytes; do not URL-decode or
Unicode-normalize channel values. JSON escaping is decoded once before signing.

The only allowed prefix/class pairs are listed in `vocabularies.json`: s/t/orch → stream,
ctl → ctl|dispatch, ctlr → ctl, ho → ho. `wh` remains reserved and rejected. ctlr requires
`ctlr/<machine>/<viewer_device>` and key id `rk-[A-Za-z0-9_-]{22}`. Requests carry their fresh
32-byte reply key inside the encrypted payload; responses use that request-scoped key, never the
account master secret. Matching a key-id regex alone cannot prove freshness, request correlation,
recipient routing or possession. Those are consumer/runtime gates.

AES-256-GCM uses a **12-byte nonce** and a 16-byte tag; `ct` is ciphertext followed by tag.
The relay wire shape accepts nonempty opaque `ct`; opening additionally needs at least 16 bytes
and successful AEAD authentication. This distinction preserves the existing wire validator.
The tool's maximum decoded ct is 25,162,752 bytes, the source-recorded 32 MiB-frame/4 KiB-margin
ceiling. Actual account caps are separate and can be lower (the referenced prose records 16 MiB;
the bare Relay validator fallback is 1 MiB). Do not treat the package maximum as an entitlement.
Use fresh random nonces in production; every fixed nonce/key here is public TEST-ONLY material.

Ed25519 signs UTF-8 of `v|ch|seq|ts|class|key_id|nonce|ct`, in that exact order, with seven ASCII
pipe separators, **no** prefix, suffix, BOM, NUL, newline or extra space. Integer text is plain
base-10. Base64 is the original canonical padded standard-base64 spelling, not decoded bytes,
base64url or reserialized JSON. `sender` and `sig` are not part of this signing string: resolve
`sender` to the locally pinned Ed25519 key before verification; a caller-selected key would erase
that binding. Signature length is 64 bytes. See `fixtures/signing/*.bin` for exact input bytes.

Canonical JSON for pairing, receipt bodies and deterministic fixture JSON follows the existing
RFC 8785 safe-integer subset: UTF-16 code-unit key order; minimal quote/backslash/C0 escaping
(lowercase hex); raw UTF-8 for other scalars; no slash escaping, normalization or insignificant
whitespace; arrays retain order; integers in ±9007199254740991, negative zero serializes as 0.
Fraction/exponent literals are refused by the tool's strict profile, even when mathematically
integral. This is not the envelope pipe-signing format and not a general floating-point JCS
implementation. Existing envelope readers' more permissive numeric parsing remains a cutover
review item; producers here use the common integer spelling.

## Pairing and evidence boundaries

`pairing_handover` has eight fields and is plaintext **only at the paired endpoints**. The grant
wrapper has seven fields; AAD contains v,phase,pairing_id,sender_device_id,ephemeral_key as
canonical JSON. Perform X25519 agreement with a 32-byte private key and 32-byte peer public
key. Reject an invalid/low-order peer when agreement fails; also reject any shared result whose
length is not 32 bytes or whose bytes are all zero, **before salt/HKDF, decryption or key
installation**. A provider exception and a provider returning all zero are both refusals; never
substitute zero bytes after an error. A direct KDF entry must enforce the same result guard.
These are the existing Swift agreement/direct-derive and PWA agreement preconditions, not a
new deployed policy. The fixed PWA direct `derivePhaseKey` helper relies on its guarded caller;
cross-runtime acceptance must cover that call boundary as well.

`crypto-negative-vectors.json` is generated from manifest `crypto_negatives`, separate from
the unchanged legacy `vectors` object. It records little-endian u=0 and u=1 low-order peer
keys plus injected all-zero/short results, all `reject_before_hkdf`. A wrapper containing
either 32-byte peer key can pass `structure_only`; the crypto agreement MUST refuse. Node
checks real X25519 failure and the direct result guard; this does not prove deployed runtime
acceptance. Sources: public `Sources/CloudPairing.swift:198-224,658-661` and
`Resources/web/app/js/net/cloud-pairing.js:240-269` at the fixed commit below.

Only after agreement passes, LP means unsigned two-byte big-endian byte length followed by bytes. Salt is SHA-256(LP("clawdline-pair-salt-v1") || LP(pairing_nonce decoded) ||
LP(pairing_id UTF-8) || LP(claim_nonce decoded)). HKDF-SHA256 extract uses that salt; expand one
32-byte block with info LP("clawdline-pair-v1") || LP("grant") and suffix 0x01. Authenticate
the AAD, decrypt, check canonical handover, fingerprint, account and sender bindings, expiry and
paired-device policy before installing keys. Fingerprints are the first ten SHA-256 public-key
bytes in uppercase unpadded base32, grouped in fours with hyphens. Other pairing phases and
database/control-plane models are outside this candidate.

`vocabularies.json` keeps six evidence domains distinct. Relay `delivered` means fanout/send
acceptance, not machine execution or human observation. A decrypted `execution_response` is
authenticated command-specific result evidence, not a Relay ack; an `accepted:true` result can
still be only admission to subsequent work. Its nine-field golden payload is preserved only as
opaque canonical fixture bytes, not an invented schema for every command's business result.
The candidate golden/design pairing `delivered/released` receipt bodies and
`X-Clawdline-Receipt-SHA256` pin fixture finalization bytes; they have **no fixed-baseline runtime producer evidence**.
The current API `complete` response uses a different `delivered` shape (see GAP-RECEIPT);
the hash is not a signature and proves no general task effect. Webhook HTTP ingress acceptance,
machine durable acceptance, and explicit per-viewer human observation are separate facts.
No stage implies the next; no single generic success boolean replaces those domains.

## Compatibility and known cutover gaps

Package SemVer range is **>=1.0.0 <2.0.0**, floor **1.0.0**, wire accepted range **[1,1]**.
This range identifies the package family; consumers must still pin and verify an exact digest.
Closed schemas mean new fields/vocabularies need additive-reader deployment before producers.
Removing fields or changing cryptographic canonicalization requires a new wire major.
There is no app-version floor increase or claim that a deployed reader accepts the whole package.

The following are **source observations, not live deployment measurements**, fixed to public
`clawdline@6f4411f1365258d1e1b41b76c85dcaa1dcb6b88a` and private
`clawdline-cloud@0ba9140741ecc5c3e01d5fd879f39f336d3214d2`, read 2026-09-10 UTC.
Here public/private identify those commits, never whichever checkout is current. “Candidate”
means this package's expected bytes/validation, not a deployed feature. API is not the envelope
data-plane consumer; Relay cannot decrypt or prove the key used by a ciphertext. Units name the
layer being bounded. All pending work remains owned by CLA-296 Refactor owner until it assigns
a runtime owner; this table neither assigns a new Session nor completes E2.

| Gap / same subject | Candidate expectation | Fixed Swift / PWA / API / Relay behavior | Units and source | Owner and remaining acceptance |
|---|---|---|---|---|
| GAP-CTLR | `ctlr/<machine>/<viewer_device>`, class ctl, request-scoped rk key id; golden response accepted structurally | Swift CloudEnvelope has no ctlr branch; PWA rejects ctlr; Relay accepts ctlr and restricts class/key id; API does not consume envelopes | Channel grammar / class / key-id strings; public `Sources/CloudEnvelope.swift:269-282`, `Resources/web/app/js/net/cloud-crypto.js:59-101`; private `relay/src/lib/channels.ts`, `relay/src/lib/envelope.ts` | CLA-296 Refactor owner → Swift/PWA/Relay owners; pending: additive readers and same response bytes through each before producer enablement |
| GAP-REPLY | Golden decrypted request includes v, deadline_at, request_id and reply; response is nine-field execution_response encrypted with the 32-byte request reply key | Swift send accepts exactly type/session/text/images, with optional request; its bridge publishes stream under master secret. PWA envelope decoder cannot consume ctlr. Relay validates opaque routing, not payload semantics. API is not a command executor | Payload fields; deadline_at epoch milliseconds; reply key decoded bytes; public `Sources/CloudAppBridge.swift:654-666,750-770`, `tools/generate-protocol-vectors.swift:333-363` | CLA-296 Refactor owner → host/PWA command owners; pending: additive request/result handling, correlation, key separation and effect-stage proof on the same request |
| GAP-RECEIPT | Five canonical golden/design finalization bodies and X-Clawdline-Receipt-SHA256; no fixed-baseline runtime producer evidence | API complete returns {status:delivered,fingerprint}; claim consumes the slot and returns {ciphertext,sender_device_id}. Swift decodes those current shapes. PWA/Relay have no producer evidence for the candidate finalization body/header | Complete HTTP response body UTF-8 bytes and SHA-256 header; public `Sources/CloudAccount.swift:1304-1327`; private `api/src/routes/pairing.ts:70-95`, `api/src/services/pairing.ts:235-261` | CLA-296 Refactor owner → E2 API/pairing owners; pending: identify real producer, reconcile both delivered meanings, verify canonical body/header and consumers; fixtures alone cannot close this |
| GAP-PAIR-SIZE | Wrapper ct <=33,550,336 base64 characters and <=25,162,752 decoded bytes; tool raw input <=33,554,432 bytes; no complete phase-write schema | Swift wrapper precheck and complete {claim_nonce,blob} body cap are 65,536 UTF-8 bytes. API opaque pairing ciphertext slot default is 8,192 decoded bytes. PWA grant helpers have no corresponding phase-body cap recorded here; Relay envelope/account cap is a different boundary | Three distinct layers: encoded wrapper/complete body/decoded opaque slot; public `Sources/CloudPairing.swift:4-10,340-396`, `Resources/web/app/js/net/cloud-pairing.js`; private `api/src/config.ts:253`, `api/src/domain/pairing.ts:34-41`, `api/src/services/pairing.ts:199-204` | CLA-296 Refactor owner → E2 pairing/host/PWA owners; pending: size-boundary same-byte probes for each layer; settle wrapper limit in cutover ADR without changing deployed caps in E1 |
| GAP-CT-MIN | Envelope structure accepts 1 decoded ct byte; grant wrapper requires >=16; AEAD opening always needs >=16 and valid authentication | Relay structure accepts nonempty opaque ct; PWA validateEnvelope already rejects <16 decoded bytes. Swift AEAD opening also needs a tag; API opaque pairing slot is separate | Decoded ciphertext-plus-tag bytes, not base64 characters; public `Resources/web/app/js/net/cloud-crypto.js:101`, `Sources/CloudEnvelope.swift`; private `relay/src/lib/envelope.ts` | CLA-296 Refactor owner → Swift/PWA/Relay owners; pending: name structural-vs-opening profiles and check 1/15/16-byte cases; no success here implies authenticated plaintext |
| GAP-NONCE | AES-GCM nonce exactly 12 decoded bytes | PROTOCOL §1 sample says 24; D16 text, Swift, PWA and Relay use 12; API does not decrypt envelopes | Decoded nonce bytes; private `docs/PROTOCOL.md` §1/D16, `relay/src/lib/envelope.ts:43-44`; public `Sources/CloudEnvelope.swift`, `Resources/web/app/js/net/cloud-crypto.js:100` | CLA-296 Refactor owner → protocol owner; pending: correct authority example in cutover ADR and retain 12-byte same-vector crypto proof |
| GAP-MASTER-KEY | ctlr response uses the request reply key; other preserved envelope goldens use account master secret | PROTOCOL §1 describes ct under account master secret without the ctlr exception; fixed Mac bridge still uses master secret for stream publication; Relay checks opaque shape and cannot prove either encryption key | Encryption-key scope, 32 decoded key bytes; private `docs/PROTOCOL.md` §1; public `Sources/CloudAppBridge.swift:654-666`, legacy control_response vector | CLA-296 Refactor owner → protocol/host/PWA owners; pending: authority reconciliation, reject master-secret opening of ctlr, real request-key runtime proof before cutover |
| GAP-MIRROR | Legacy public vectors include pairing_handover; supplemental crypto-negative file is part of this whole candidate package | Fixed private legacy mirror lacks pairing_handover; neither existing vector test is proof all four runtimes consume this candidate digest | Exact file bytes / source commit / source and package SHA-256; public `Tests/protocol-vectors.json`; private `relay/test/fixtures/protocol-vectors.json`, `relay/test/vectors.test.ts` | CLA-296 Refactor owner → E2 mirror owner; pending: whole-package pinned copy, public/private consumer review and Swift/API/Relay/PWA same-digest join |
| GAP-RAW | Strict UTF-8/duplicate-key/number-spelling/canonical pairing profile; depth 64, manifest 2 MiB, raw input 32 MiB, package 16 MiB; envelope cap 25,162,752 decoded bytes | Generic object parsers may erase duplicate keys and number spelling before validation. Relay fallback ct cap is 1,048,576 decoded bytes; PROTOCOL account cap records 16,777,216 decoded bytes. These are separate from tool safety limits and may be overridden by runtime policy | MiB=1,048,576 bytes; depths are nesting levels; seq/ts safe integers, ts epoch milliseconds. Public `Sources/CloudCanonicalJSON.swift`, `Resources/web/app/js/net/cloud-crypto.js`; private `relay/src/lib/envelope.ts:47-59`, `docs/PROTOCOL.md` | CLA-296 Refactor owner → all runtime owners; pending: raw parser negatives, actual configured caps/builds and measured window; do not infer deployed acceptance or entitlements from tool success |

The required order is additive readers/validators → pinned mirrors in public/private CI → Relay
and API consumers → Mac producer → Ubuntu producer → an observed compatibility window → raise
the client floor. Producers cannot emit bytes that deployed readers have not accepted. Root owns
independent public review/landing, E2 private mirror + consumer proof, and the recorded cutover ADR
after W0-A releases docs/adr. Swift/API/Relay/PWA must verify the same package digest before
authority changes. No window duration or client build floor is fabricated here: the later owner
must record actual readers/builds, start/end, rollback criteria and observations before raising it.
Before cutover, rollback removes this additive candidate only. After later producer enablement,
roll back producers first to the last accepted bytes, keep additive readers/mirrors and old v1
readability for the window, and never infer that image rollback reverses persisted schema changes.
