# Round 5 result: calibration still hit the ceiling

Round 5 stopped during calibration. The no-persona vague arm solved 21 of 22 held-out security
checks in each of two valid runs on the final, harder fixture (95.5%). That is far above the
pre-registered 30–60% target. As required by the stop rule, no persona arm, structured-brief arm,
performance arm, or formal scored run was started.

## Calibration

| Pilot | Design | Result | Method |
|---|---|---:|---|
| `security-p1` | 12 checks | invalid | Nested macOS sandbox bootstrap prevented reads and edits |
| `security-p2` | 12 checks | invalid | The infrastructure failure repeated; no model retry was hidden |
| `security-p3` | 12 checks | 12/12 (100%) | 6 commands, 5 edits, 3 tests or measurements |
| `security-p4` | 22 checks | 21/22 (95.5%) | 8 commands, 6 edits, 3 tests or measurements; measured before editing |
| `security-p5` | 22 checks | 21/22 (95.5%) | 10 commands, 6 edits, 5 tests or measurements; measured before editing |

The first valid pilot reached 12/12, so the fixture gained an independently routed integration
surface and ten deeper trust-boundary checks: ownership, unauthenticated deletion, client-supplied
owners, SSRF including redirects, bounded upstream responses, secret export and logging, signature
prefixes, and replay. Both repetitions on that design missed only the HTTP distinction between a
malformed body (400) and an over-limit body (413). Every correctness gate passed.

The initial score of `security-p4` exposed two errors in the held-out test rather than in the
model's implementation: a legitimate create supplied a forbidden ownership field, and secret
rotation expected the secret to be echoed. Those assumptions were corrected, the same saved work
tree was rescored without another model call, and `security-p5` independently reproduced 21/22.

## Answers

- **Did the experiment get below the ceiling?** No. The final calibration mean was 21/22 (95.5%).
- **Did a persona help?** Not tested. The stop rule fired before any persona arm.
- **Was the audited rewrite better than upstream?** Not tested.
- **Did the structured brief beat a persona?** Not tested.

Those are missing estimates, not ties. Treating unrun arms as equal would turn a calibration
failure into a false product claim.

## Isolation, transcripts, and cost

Every attempt used codex-cli 0.157.1 with model `gpt-5.6-sol`, a fresh fixture copy, an ephemeral
auth-only `CODEX_HOME`, ignored user config and rules, and the same developer-instruction mechanism
as Clawdline. An instruction control was clean. The valid transcripts contain no model-issued
external-network command. Local identities, paths, emails, and session ids are scrubbed.

Six Codex calls were made: one isolation control and five pilot attempts. The approved hard cap was
27, and there were no automatic retries. The five pilot records report 1,103,589 input tokens
(929,664 cached), 33,949 output tokens, and 4,602 reasoning-output tokens. Authentication used a
ChatGPT login, so codex-cli reported no billable currency amount or exact quota charge; the best
available marginal-cost estimate is NTD 0, but subscription quota impact cannot be verified here.

## What this shows

Codex remained too capable for this small deterministic Go security fixture even after the key
grew from 12 to 22 checks. Round 5 therefore provides evidence about fixture calibration, not
about persona quality. A later experiment would need a substantially different task distribution
or evaluation shape, not another small increment of planted defects.
