# Reproduce synthetic outbound-content evidence

Read the [data-flow inventory](data-flows.md) alongside these tests. The scope is
application bytes at existing local encryption seams and controlled HTTP
receivers. It does not infer encryption from an opaque TLS packet capture.
Production Cloud, push providers, STUN, DNS and phones are never contacted by
these selected tests. Dependency/toolchain setup is outside that statement.

## Run

From a checkout with Go and existing module dependencies available:

```sh
bash tools/security/synthetic-egress.sh
```

The [script](../../tools/security/synthetic-egress.sh) prints source revision and
Go/Node/Git versions, then runs exactly three test groups through `tools/heavy.sh`
with `-count=1`. It creates an empty disposable `CLAWDLINE_NEXT_DIR` for the test
process and removes it on exit. Push stores also use `testing.T.TempDir`. No
real configuration, conversation, production credential or content key is read.
The heavy broker uses its normal identity to acquire the compile slot; it does
not use the empty test state. A caller supplying `CLAWDLINE_NEXT_DIR` to the
wrapper itself must ensure that broker identity remains available.

Restricted children should set `GOCACHE` and `TMPDIR` to their task-owned `work/`
directories before running. Delete only that owned cache after all commands have
exited. Preserve stdout/stderr as private task artifacts. The script does not
commit generated keys, binaries, pcaps, receiver URLs or network addresses.
No new production capacity bound is introduced; the Cloud fixture reuses
`DefaultSpoolLimits`.

## Assertions and deliberately failing input

| Test | Production seam exercised | Observable checks | Explicit limit |
| --- | --- | --- | --- |
| [TestSyntheticEgressCloud](../../internal/transport/cloud/synthetic_egress_test.go) | `Relay.PublishTracked` → real spool → signed AES-GCM envelope | Seven labeled synthetic payloads contain canaries before sealing. Controlled HTTP receiver sees no canary/plain-payload base64, opens exact original bytes with the designated viewer key and rejects a random wrong key. Clear routing fields remain readable. Raw plaintext and base64-wrapped plaintext controls must be detected | Spool bytes are POSTed by the test, not by the production WebSocket drain. No provider parser, HTTP resource handler, publisher, browser pairing or permission gate is exercised. Image cases are labeled JSON byte fixtures, not actual pixel/EXIF or thumbnail generation evidence |
| [TestSyntheticEgressPush](../../internal/adapters/push/synthetic_egress_test.go) | `Build` → real `Sender.Send` → RFC 8291 sealing → real Cloud `PushClient.Send` → controlled HTTP receiver | Worker and Apple declarative formats contain title/body before encryption; receiver scans raw JSON and decoded ciphertext; subscriber-key fixture recovers exact notification. Intentionally passing plaintext as ciphertext through the same production Cloud client must be detected and refused with 422 | Cloud forwarding to an actual push service and OS lock-screen rendering are untested. The recovered payload demonstrates displayable content, not an observed phone display |
| [TestSyntheticEgressUpdate](../../internal/adapters/release/synthetic_egress_test.go) | Real updater `Env.Fetch` and build checker `Fetch` → controlled manifest/signature/BUILD receiver | Exactly three GET requests, empty bodies/queries, no authorization/cookie; signed synthetic manifest verifies and synthetic build stamp parses | No background scheduler, beta discovery, archive/platform URL, CDN redirect or installation exercised |

The Cloud controls run for every sample (14 negative requests); push adds two
base64-wrapped plaintext controls. The same detector used on each sealed request
must recognize its control. A successful suite means the controls were caught,
not that intentionally leaking inputs are accepted as valid production traffic.
Canary scanning is a bounded regression detector; exact authenticated decryption
is the stronger positive evidence. It is not a general secret detector or a
proof about arbitrary encodings/other content.

## Evidence record

The initial run used production base
`bbd850a596d18805d1298356e05393df34c04a9b` plus these test files, Go 1.27.1
darwin/arm64, Node v24.1.0 and Git 2.38.1. All three selected groups passed;
the Cloud group has seven cases and push has two. Each case logs a plaintext
fixture SHA-256, plaintext/sealed lengths where relevant, control count and
decrypt outcome. Fixture content is fixed in the tests; randomized keys/nonces
intentionally make ciphertext hashes vary. The private raw execution log is
delivered to the integrating root; this document is not a formal Cloud service
attestation or a claim that integration/release gates passed.

On 2026-10-21, the integrating root reran the final test files on production base
`2b82d62ef67929cca79dce7c1f3b241980de3495`. All three groups passed, including
seven Cloud samples, both push formats, all 16 plaintext controls and the three
manifest/signature/BUILD GET requests. The complete local `tools/check.sh` gate
also passed. This establishes local integration results for that tree plus these
files; production Cloud, phone, browser and public release acceptance remain
separate steps below.

## Remaining integration interfaces

The root must retain these distinct acceptance steps when combining permission
and revocation changes; turning Cloud or the phone off is not acceptance.

1. Drive synthetic provider records through the real local console and Cloud
   content handler: Session list, current transcript, a history page, filename
   and document bytes, an actual image's pixels/metadata and both original and
   thumbnail. Compare hashes at the reader and final authorized viewer. Keep an
   unpaired viewer negative alongside the paired positive; possession of a key
   in this unit test does not establish authorization.
2. Connect the real relay WebSocket drain and direct carrier to controlled
   receivers, checking application frames rather than TLS opacity. Exercise
   ICE success and relay fallback, terminal output/history/input and reconnection.
   Existing direct and publisher tests are supporting source, not results from
   this harness: [direct tests](../../internal/transport/cloud/direct_test.go),
   [publisher wire baseline](../../internal/transport/cloud/publisher_wire_baseline_test.go),
   [status envelope tests](../../internal/transport/cloud/status_envelope_test.go),
   [push crypto tests](../../internal/adapters/push/push_test.go).
3. Observe synthetic production Cloud plus an actual paired phone in a fixed
   time window with known trigger counts. Record all destinations, idle traffic,
   routing metadata and unexplained requests. Verify normal read/send use and
   notification display. Coordinate the test identity and any priced calls with
   the root; no production calls are authorized by this script.
4. Capture STUN candidate/address disclosure without publishing actual addresses;
   observe production hosted BUILD, beta and artifact request paths/platform metadata and
   redirect recipients. The source inventory identifies these defaults but this
   package has no live network evidence for them.
5. Obtain relay/API/provider retention and deletion evidence from their owners.
   This public client cannot establish service storage, backups, processing
   regions or deletion merely because it sends encrypted content.
