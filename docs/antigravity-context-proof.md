# Native Antigravity context proof

The private transcript reader and launch Store support a durable proof that
original context appeared in a native execution. They do not enable the public
start/send path or a role badge. Synchronous managed-hook composition and its
native timing verification remain pending. See [first input](antigravity-first-input.md)
and [launch context](antigravity-launch-context.md) for the surrounding owners.

## What is proved

Before returning a PreInvocation response, native composition must capture an
existing complete transcript using `CaptureAntigravityContextFence`, under
fresh checks of the process generation, exact terminal, workspace, capability
and positive native conversation. The fence retains the full binding, file
generation, byte length, digest of every old byte and final step index. Missing
files are unavailable, never an invented empty baseline. An existing empty file
has a real generation and may be fenced. The original reader's confinement,
whole-file read budget, partial-record refusal and increasing steps apply.

`Store.PrepareFencedResponse` persists this exact fence with the PREPARED
response before returning response bytes. Only one response can remain
outstanding per launch. A later response, including the old unfenced path,
refuses until its predecessor has native loaded proof. A response already
returned without a fence cannot acquire one retroactively. Repeated identical
prepared receipts preserve their baseline and any existing proof.

`Store.VerifyContextLoaded` loads the private immutable injection bytes, runs
the native reader outside the Store mutex, and atomically records evidence only
if the same latest prepared receipt, active process and original context remain.
The reader requires a complete later row with increasing step index,
`source=SYSTEM_SDK`, `type=SYSTEM_MESSAGE`, `status=DONE`, no error, and the exact
standalone 1.4.5 PreInvocation wrapper around the expected text. Duplicate JSON
keys and null/non-string error values cannot become positive evidence. Model,
user and generic system echoes are not proof. No trimming, substring matching
or repeated decoding is used.

The sanitized JSONL fixture preserves the wrapper observed in saved native
1.4.5 evidence; context, time and step index are synthetic. The wrapper appends
one newline after the exact injection. An injection's own trailing newline is
part of its bytes, not a second wrapper newline. Tests use that saved fixture
rather than constructing an expected wrapper from implementation constants.

The proof retains row offsets, native step index, injection digest and the full
prefix digest through the row end. It proves that context occurred in this
execution after the persisted baseline. It does not identify a particular
model invocation, prove instruction compliance, or establish tool permission
policy. The native log is trusted within the existing same-user local threat
boundary; this is not a signature against a malicious local account owner.

## Current versus historical evidence

`Store.CheckContextLoaded` must be used with a fresh native verifier before a
current badge. It rereads the proof row, its entire prefix and file generation,
then rechecks the latest receipt. Replacing, truncating or editing any retained
byte invalidates the current proof, including an edit that preserves row length
or merely changes its timestamp. A normal complete append preserves it.
Persisted Loaded is historical evidence only. Reopening the Store never
reactivates a process; fresh native pinning and proof checks are required.

The private index advances to version 3 when next atomically written. Known v1
and v2 states migrate in memory without rewriting on Open; v2 consumed input
slots survive. Legacy documents claiming new proof fields are rejected, as are
unknown fields. Old versioned readers refuse the new index instead of dropping
proof or consumed-input authority. Uncertain publication requires reopening.
Proof remains inside the existing bounded response receipt, with no new map,
poller, retry queue or eviction policy; the existing whole-index byte budget
can refuse a full store.

## Verification boundary

Synthetic tests cover attribution and wrapper near misses, duplicate keys,
errors, old rows, partial/missing files, full-prefix edits past 4 KiB,
replacement, symlinks, revocation on both sides of reading, restart, outstanding
responses, unavailable publication, current-receipt races, context tampering and
migration with a consumed input slot. No model or paid API call is involved.
The first targeted run found a test-only cancelled context shared by successive
failure cases; each case now owns its own context.

The production hook path still uses PREPARED-only responses. It must be changed
to capture this baseline synchronously before returning context, prove native
file availability at that moment, and check previous proof before advancing a
response. Original skill loading and Squad applied receipts need their own
verified composition. These APIs alone do not mean that the product has loaded
a role, nor that the complete Antigravity integration has passed acceptance.
