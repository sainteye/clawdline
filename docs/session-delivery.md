<!-- clawdline-doc: kind=spec audience=agent -->
# Session delivery evidence

A Session completion receipt means **delivered, awaiting approval**. It is weaker
than a landing and does not authorize review, deployment, or publication. Its
human summary remains 1–500 Unicode characters after trimming, without NUL.
A repository adapter must not expand every commit into that sentence.

API level 35 adds this optional contract. For large deliveries, `clawdline session report --summary "Verified the delivery."
--evidence-file evidence.json` submits a separate UTF-8 document. The CLI reads a
plain file, validates it before any request, and sends its exact bytes in the
optional `delivery_evidence` string of the existing completion POST. The daemon
validates again and stores the whole string and its SHA-256 in the same
`session.delivered` event as the summary. The answer contains
`delivery_evidence_sha256` and `delivery_evidence_commits`. Preserve the input
file, baseline and manifest as immutable delivery artifacts: the hash in the
acknowledgement identifies their exact evidence document. Do not put credentials
in any artifact. The Session title and notification carry only the short summary.

## Version 1 document

```json
{
  "schema_version": 1,
  "summary": "Verified the delivery.",
  "baseline_sha256": "<64 lowercase hexadecimal characters>",
  "manifest_sha256": "<64 lowercase hexadecimal characters>",
  "git_receipt": {
    "schemaVersion": 1,
    "ok": true,
    "root": "/absolute/repository/path",
    "baselineHead": "<full baseline commit ID>",
    "head": "<full checked HEAD commit ID>",
    "commits": ["<every full owned commit ID, in delivery order>"],
    "ownedFiles": 1,
    "issues": [],
    "residue": []
  }
}
```

The document is at most 256 KiB before transport escaping and contains at most
4,096 full lowercase SHA-1 or SHA-256 Git commit IDs. This fits the existing
2 MiB command request ceiling even under JSON escaping. Duplicate IDs, duplicate
JSON keys, truncated IDs, unsupported versions, malformed digests and mixed Git
object formats are invalid. The summary must equal the trimmed request summary.
The gate must explicitly report `ok: true`, an empty `issues` array and at least
one verified file and commit. Additional gate receipt fields are preserved;
unknown envelope fields are refused. A failed, missing or unknown verdict is not
completion. The daemon returns typed errors `delivery_evidence_invalid`,
`delivery_evidence_mismatch`, `delivery_evidence_unverified`, or
`delivery_evidence_too_large` before recording a completion.

The adapter owns the actual verification of every listed commit, file version,
ownership, foreign change and later shared-file modification against the
**current** repository. Baseline/manifest hashes bind the exact inputs used by
that check. The daemon checks the assertion's structure and consistency; it
does not rerun an adapter or independently attest Git history. A successful old
receipt cannot establish that today's HEAD still passes. A digest is an integrity
identifier, not proof of truth or approval.

## Retry and compatibility

Evidence calls require `Idempotency-Key`. The CLI derives it from terminal,
conversation and the exact evidence digest. Reusing the same file and identity
replays the original answer and does not duplicate the event or push. The normal
receipt retention window and capacity apply; expired keys are refused, never run
again. The same key with a different request is `idempotency_key_reused`.

The existing durable receipt boundary distinguishes pending, replay, expired,
full and orphaned operations. A daemon crash between event commit and answer
filing yields `request_outcome_unknown`; inspect persisted evidence before any
new operation. Transport failure or an absent/mismatched evidence digest in the
response is not success (`delivery_evidence_ack_unknown` for the latter).
Never automatically retry with a new identity or key to escape uncertainty.

Old clients can continue sending only `summary`, without evidence or key; each
such call still creates a new receipt. A new evidence call to an older daemon
fails on the unknown field instead of silently degrading to an unsupported
summary-only receipt. Installing a new CLI alone does not upgrade the running
daemon. Repository adapters should retain their legacy short-delivery path and
require the evidence path when a complete commit list cannot fit.

Tests use temporary stores, fake transports and synthetic repositories. They
must not call the person's real completion route to reproduce a formatting bug.
