<!-- clawdline-doc: kind=record audience=both -->

# Native checker-text pilot, 5 November 2026

Six native Clawdline Codex review tasks checked three historical candidates with
and without the new integration section. The Cloud treatment raised a known
omission that its control missed. Both background samples found the lost waiting
notification and missed the dead health clock. Both documentation samples missed
the predefined stale-guidance targets. These observations do not establish a
causal or general improvement: one role version changed during dispatch and skill
loading differed between samples.

## Frozen comparison

The [protocol](review-checker-pilot-20261105/PROTOCOL.md) was written before the
model launches. Each case had a separate shallow clone containing its candidate
and parent. All six actual transcript records name `gpt-6.1-sol`, effort `high`.
Each used the native task lifecycle and Code Reviewer role; there were no
provider-native subagents or automatic model reruns. Dispatch transport retries
reused the original task IDs after EOF or stale-inventory refusal.

The treatment was exported by calling the actual `writeGateCheckerBrief` renderer
at the implementation preceding `aa3f197d1`. Only its Frozen candidate through
Acceptance sections were used. The control removed only the new Review scope and
integration completeness section. The shared integration text remained unchanged
through the implementation commit. Later Playbook-only placeholder and unclear-range
wording corrections do not alter that checker section.

The [control](review-checker-pilot-20261105/control.txt) and
[treatment](review-checker-pilot-20261105/treatment.txt) retain renderer fixture
identities as explicit sample placeholders. They are ordinary historical review
instructions, not live gate authority. The evidence-upload and closed-verdict
sections were excluded. No real verification round, evidence upload or gate CAS
was settled by these review tasks. The native gate behavior tests separately
exercise the pinned candidate, read-only launch, bounded evidence and closed result.

## Target scoring

The scorer required the predefined mechanism and consequence. A finding about
similar wording or the removal of a CLI flag alone did not count as finding stale
guidance. Intent questions could identify a real omitted connection but were not
automatically treated as confirmed defects or failed acceptance claims.

| Historical candidate | Predefined targets | Control | D treatment |
| --- | --- | --- | --- |
| `5cdb3e423` | Cloud snippets not routed to the daemon operation | 0/1 | 1/1, an intent question |
| `e490f9157` | Lost waiting-answer push; dead sweep health/listing clock | 1/2 | 1/2 |
| `dd263f1cf` | Stale read-only/open guidance; seven stale language catalogs | 0/2 | 0/2, role version changed |

The Cloud treatment separately asked about absent reads, absent writes and
read-only controls. The background treatment asked about retiring waiting pushes
and incomplete UI/Agent delivery; another intent question appeared within its
Cloud finding. The documentation treatment asked about the external relay contract
and roster-only pairing compatibility. These are questions with stated consequences,
not eight newly proven defects.

The first two pairs share the same role version and descriptively score 1/3 versus
2/3. Skill application still differs, so even this subtotal is not a clean causal
estimate. Pooling all three cases would conceal the role drift. There is no clean
refactor control in this batch, so it cannot estimate false positives.

The raw reports also contain findings outside the frozen target list. For example,
both Cloud samples identify the machine-token route gap; that is a known true
extra, not an additional target point. Other reported issues were not exhaustively
reproduced and must not be presented as independently confirmed runtime defects.

Target provenance can be inspected in the public follow-up changes:
[Cloud wiring](https://github.com/sainteye/clawdline/commit/bca198afa),
[dead sweep clock](https://github.com/sainteye/clawdline/commit/464d664d2), and
[stale guidance and catalogs](https://github.com/sainteye/clawdline/commit/b8ac05ef8).
The historical catalog target concerns retained, misleading text; it is not a new
requirement to translate every later feature into all shipped languages.

## Deviations and limits

Five launches used Code Reviewer version `sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead`.
The documentation treatment used `sha256:6324291052765bd9000aca77475a041422bf82bfe8837a33282fd300b1695edf`:
the machine changed from dev `928bedd` to `e332bf5` during dispatch. This was detected
from actual loaded role files and transcript evidence, not inferred from the
requested persona flag. Some samples loaded the two-axis skill outside the narrow
clone/role-file allowance; others declined it. Two workers also used `head` to
truncate shell output despite the tool allowlist. All samples remain in the record;
none was silently replaced.

The Root implemented the feature and scored the reports. Opaque report labels
provided incomplete blinding: prompt-arm labels and treatment wording remained
recognizable. One run per cell, three selected cases, correlated historical targets,
role/skill drift, no runtime execution and no clean control preclude claims about
statistical significance, universal benefit or equivalence to multiple reviewers.
All six reports distinguish source inspection from unexecuted browser, platform,
timing, real-data and deployment checks.

The earlier local role-body pilot used 18 Codex CLI sessions, not native Clawdline
children: fixed source/role variants, three roles, two defect cases and a refactor
control. Its target scores were 0/9 versus 2/9, both gains on the same waiting-push
defect; the Cloud omission and dead health clock were missed throughout. Its six
refactor samples had no material findings. That pilot is background evidence, not
pooled with this batch or a live verification-gate acceptance. Earlier Claude D
results were four hits on one Cloud omission, not proof of general superiority;
complete original per-run Claude reports are not republished here, so the retained
summary is a provenance limitation rather than a rerunnable result set.

## Artifacts and reproduction

The [manifest](review-checker-pilot-20261105/manifest.json) records candidates,
parents, trees, actual models/efforts, role versions, ledger counts and public-copy
hashes. The six report copies are:

- [Cloud control](review-checker-pilot-20261105/1-control.md) and [Cloud D](review-checker-pilot-20261105/1-treatment.md).
- [Background control](review-checker-pilot-20261105/2-control.md) and [background D](review-checker-pilot-20261105/2-treatment.md).
- [Documentation control](review-checker-pilot-20261105/3-control.md) and [documentation D](review-checker-pilot-20261105/3-treatment.md).

Public copies replace local account paths, Project scope and private launch
snapshot identifiers. Raw originals, native task IDs, full launch receipts and
transcript hashes remain in the local experiment record; the public-copy hashes
are intentionally different. This is not a publication of private transcripts.

For a future authorized reproduction, use the local-only
[case preparer](review-checker-pilot-20261105/prepare-case.sh) for a fresh directory
per sample. Substitute the manifest's BASE/HEAD/tree and that directory into the
frozen prompt, then use `clawdline dispatch --assistant codex --kind review --claims
"" --persona code-reviewer --permission-mode ask --model gpt-6.1-sol` with a unique
lowercase task ID. Hold the exact role definition and skill policy constant across
all launches, and verify actual transcript/model and role versions before pooling
results. Reusing a moving built-in role alias is insufficient. Inspect both source
ends before scoring, retain missing/failed samples, and obtain authorization before
any additional model batch. This record itself starts no task and grants no spending.

Six subscription Sessions produced 98 recorded model calls. The machine's Codex
authentication mode was ChatGPT. The native ledger reports monetary cost as unknown;
it does not establish a zero API price or a savings estimate. No separately priced
external data/model API operation was launched by the Root for this experiment.
