# Native Antigravity context proof

The private transcript reader and launch Store support a durable proof that
original context appeared in a native execution. They do not enable the public
start/send path or a role badge. Synchronous managed-hook composition is now
implemented; its actual vendor timing verification remains pending. See [first input](antigravity-first-input.md)
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

The managed PreInvocation hook now captures this baseline synchronously before
returning context. A repeated invocation first verifies the preceding response's
native loaded proof; missing, stale or non-native evidence refuses the new
response. The fresh verifier repeats complete kernel process, exact terminal,
workspace, capability, immutable files and positive native conversation checks
around the read. Runtime construction and proof use the same transcript home.
No terminal lane is acquired by this hook, and no poller or retry is introduced.

The six-case private-tmux/Unix-helper composition test passed in 3.182 seconds.
It uses real kernel process ancestry and the saved sanitized wrapper with
synthetic data, not a vendor process. It verifies original context across two
separate hooks and refuses wrong workspace, partial baseline, missing loaded
row, model echo and file replacement. Every returned response has its own
persisted PREPARED fence, rather than inheriting a Loaded bit from its predecessor.
The existing bridge already requires a present regular transcript at admission;
the new capture additionally requires complete native JSONL. Actual standalone
1.4.5 file timing must still be measured with the managed hook. Original skill
loading and Squad applied receipts need their own verified composition. No role
badge is enabled, and the complete integration has not passed acceptance.

## Bounded local probe controls

The private verification controller now stops every registered owned process
group on a budget or input deadline. A deterministic two-process control first
reproduced the old 31st event from a still-live second process; the fixed suite
passed 13 cases ten times. Root verified all 22 delivered hashes and independently
ran the same 13 cases once. A separate macOS owner-SIGKILL test observed four
kernel process-exit events and both registered groups absent. This does not
establish power-loss behavior, group-escaping descendant containment or another
platform's runtime behavior.

Received hook events, admitted model steps, allocated input, terminal delivery
and stopped processes remain separate counts. A finite registry does not prove
a native scheduler's in-flight event bound: a deliberately excessive late
receipt is honestly recorded as event 31, never relabelled as an admission.
The controller therefore proves global stop and refusal of further admission,
not an unconditional ceiling on every raw native event or vendor HTTP request.
Its current subprocess transport discards standard IO; interactive PTY forwarding
and its process-group behavior must be verified before a native CLI batch.
These controls use local fake processes only. They do not authorize or establish
native CLI timing, free-plan eligibility, a product role badge or UI acceptance.
