# Native Antigravity context proof

The private transcript reader and launch Store support a durable proof that
original context appeared in a native execution. They do not enable the public
start/send path or a role badge. Synchronous managed-hook composition is now
implemented; its end-to-end managed transport verification remains pending. Standalone
protocol timing is recorded below. See [first input](antigravity-first-input.md)
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

The private index advances to version 4 when next atomically written. Known
policy-free v1/v2 states and v3 proof states migrate in memory without rewriting
on Open; consumed input slots and original loaded proofs survive. Legacy
documents claiming newer proof or policy fields are rejected, as are unknown
fields. Old versioned readers refuse the new index instead of dropping proof,
consumed-input or revocation authority. Uncertain publication requires reopening.
See [native policy](antigravity-native-policy.md) for persistent revocation;
historical proof remains readable after revocation but grants no live authority.
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
row, model echo and file replacement. Every returned response with nonempty context has its own
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
A later artifact-only PTY extension reported 11 new controls passing ten times
and 13 original controls passing once. Root verified all 29 artifact hashes, then
independently ran the unchanged PTY suite in a new private directory: 11 tests
finished in 53.656 seconds with 17 failures and one error. Multiple stop receipts
had a payload-reaped acknowledgement followed by a supervisor wait timeout;
this is not accepted as stopped. A separate two-group diagnostic reproduced
that timeout, then both owned supervisor Popen handles joined after lease EOF.
A final exact-command-path process inspection found no remaining test commands.
The failed evidence was retained for correction. The later Root shutdown-order
results are recorded below; this earlier child report alone did not establish
native readiness.
These controls use local fake processes only. They do not authorize or establish
native CLI timing, free-plan eligibility, a product role badge or UI acceptance.

## First native boundary and completion composition

One bounded standalone 1.4.5 probe confirmed Starter quota and the actual
configuration panel's Use AI Credits off before input. It allocated one model
prompt, sent one Enter and recorded one received/admitted PreInvocation and one
native model outcome; no tool request occurred. The first child attempt stopped
on state/token persistence denial with zero prompts. Root used the already
authorized native state-write surface; an initial configuration-only startup
also used zero prompts, then the corrected `/config` harness completed the probe.
No model prompt was retried. The successful process lifetime was 88.305 seconds,
including configuration; its prompt stayed within its 45-second watchdog.
All owned groups were absent, direct handles joined and terminal/listener/threads
closed. No paid API or fallback was enabled. NTD0 is the observed Starter/credits-off
budget boundary, not an invoice; native HTTP request count was not measured.

At PreInvocation, before injection response bytes, the confined regular native
file had one complete USER_EXPLICIT/USER_INPUT/DONE row, 613 bytes. PostInvocation
and Stop both saw the same 1425-byte file and generation. Its first 613 bytes
remained exact; the SYSTEM_SDK/SYSTEM_MESSAGE/DONE row occupied [613,1218) and
matched the wrapper plus immutable injection byte-for-byte, including its final
newline. A MODEL/PLANNER_RESPONSE row followed. The model's literal answer was
not used as loaded proof. The new first-input native fixture preserves the
system attribution, wrapper and synthetic context; only timestamp is sanitized.
Its local reader test refuses a trimmed context.

This private Python helper establishes this native protocol ordering, not the
product's kernel-authenticated managed transport, role identity, skill-applied
receipts or UI acceptance. Original raw native/account evidence remains private;
no real UUID, terminal, path or account is included here.

The managed service now invokes an optional completion observer only after its
native binding has been verified and persisted. PostInvocation and Stop verify
the latest fenced response against current process, terminal, workspace,
capability, original context and full-prefix native proof. They retain Loaded
without creating another response, baseline, input grant or model call. Missing,
echoed or replaced native rows refuse; an absent response creates no proof.
Ordinary response/permission decisions remain unchanged. Cancellation and
callback failure are hook failures; the callback uses existing listener bounds
and joins, with no poller, retry, queue or new resource bound.

Local Unix-peer tests verify completion ordering, callback failure and exclusion
of an unverified peer. Private tmux/native-helper cases verify PostInvocation and
Stop retain the original receipt/fence, while missing rows, model echoes and
file replacement refuse. These are synthetic product-composition checks; the
independent native protocol probe above remains separate. Public capability and
role badges remain guarded pending complete acceptance.

For the private probe controller, Root's lease-only and direct-signal variants
still failed. Closing the owned PTY master before joining its session leader
passed the single-variable control, then 11 PTY/13 original/2 ordering controls once
(1.329/1.832/0.212 seconds). This establishes the tested shutdown order, not an
unmeasured kernel cause or scheduler fanout. Earlier failed logs remain retained.

The first full completion gate passed native tests/builds, privacy and console
build/tests, but the console check refused an outdated semantic-review index
after the reviewed copy update. Regeneration moves this one explanation from
the permission category into the state category; the risk union remains 4017,
with 96 non-risk groups and 4996 non-risk keys unchanged. The complete console
`npm run check` then passed. The full commit gate is rerun against the exact
candidate including the generated index, without changing guard rules.

## Empty immutable context

A roleless launch can have no prompt, memory or configured language. Its native
PreInvocation response then contains no injection. It retains positive native
identity and an unfenced PREPARED receipt, and subsequent invocations or Stop
must not wait for a system row that was never emitted. The branch derives this
fact from the exact saved context, not a browser flag. It does not acquire a
Loaded proof or role badge. An older unfenced response with nonempty context
still refuses completion, even if a matching system row happens to exist.

Two native-helper regressions first reproduced the erroneous empty fenced
receipt. After correction, the complete fourteen-case managed-hook suite passed
in 7.374 seconds, including empty repeated invocation/Stop and refusal of the
nonempty legacy-unfenced case. These are isolated local test durations, not
product latency or additional vendor probes. Older unresolved fenced receipts
are not reset or granted new authority by this correction.
