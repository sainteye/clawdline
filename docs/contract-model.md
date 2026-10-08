# Shared product terms and content ownership

This page is the vocabulary shared by the console, Cloud, human help, and the Agent guide. It names meanings, not transport fields. For route shapes, schema fields, and current limits, use the sources in the ownership table below.

## Entry-page audit

| Existing page | Primary reader and job | Overlap or contradiction found | Responsibility after this change |
| --- | --- | --- | --- |
| `README.md` | A newcomer deciding what Clawdline does | It sent readers straight to detailed pages and described every Feature as needing independent plan review | Point to the first Session and role-specific indexes; state the current review switch rule. |
| `docs/getting-started.md` | A contributor building from a clone | Its title looked like the ordinary installed-release path, and it called Traditional Chinese the only console language | Mark it as a source-build path and send first-time readers to the installed-release walkthrough. |
| `docs/user/install.md` | A person installing or updating | It repeated an outdated single-language claim | Keep service commands and checks as installation detail; the first Session page carries the short path. |
| `docs/user/sessions.md` and `board.md` | A person following a conversation or item | The Board explanation required independent review for all Features, contrary to the current item guide | Keep controls and visible states here; use the shared lifecycle terms. |
| `docs/user/remote-access.md` and `troubleshooting.md` | A person configuring permissions or diagnosing service faults | Pairing instructions, protocol codes, and service commands obscured the first Cloud and recovery actions | Keep these as advanced references; the short Cloud and recovery pages lead with visible symptoms. |
| `skills/clawdline/guide.md` | An Agent operating this build | Detailed commands had no complete route-to-part index | Continue to own commands, authorization and receipts; the generated route inventory supplies discoverability. |
| `api/v1/` and `internal/domain/capacity` | Implementers and generators | Route shapes, field types, and limits were already authoritative but not navigable from human tasks | Keep them authoritative and check the generated Agent catalog against the current route table. |

This audit covers the entry and high-frequency paths. Specialized design and historical records remain indexed in [Documents](README.md); they are not first-run instructions.

## Shared terms and states

| Term | Meaning shown to a person | Evidence an Agent needs |
| --- | --- | --- |
| Machine | The computer that runs the daemon and owns its Sessions. Cloud can show several machines. | A current machine identity and a reachable, compatible daemon. |
| Offline | The machine cannot provide a fresh Cloud view or take a new Cloud command. Clawdline does not model why it is offline. | Stop remote writes; do not move the command to another machine. |
| Session | One Claude Code or Codex conversation associated with a supported terminal. | Resolve its current identity before a write. A previous row is not proof it is still the same process. |
| Waiting for you | The Session needs a person to answer or act. | A concrete question or action and a matching current Session. Notification delivery does not authorize the action. |
| Unknown | The current evidence is unreadable, incomplete, or stale. It is not an idle or successful state. | Stop a dependent action and obtain fresh evidence. |
| Board item | Work assigned to a Session and tracked through its phases. | Current item version, owner, gates, open steps, and required receipts. |
| Accepted | Clawdline accepted a request for later work. | Request or task identity; no claim yet that work ran. |
| Executed | The machine performed the requested operation. | Execution result, including failure or timeout. |
| Delivered | Output or a notice reached its intended destination. | Delivery receipt for that destination. |
| Observed | A reader actually fetched or displayed the delivered state. | Observation or read receipt, when the operation offers one. |
| Acknowledged | The intended recipient explicitly accepted or closed the item. | An acknowledgement or reply. A push notification alone is insufficient. |
| Landed | A committed change is contained in the target branch. | A Git ancestry check or a broker landing receipt, not a child's success message. |
| Deployed | A build is live on its named target. | The target's deployment stamp and served artifact check. |

A later state never follows automatically from an earlier one. In particular, **accepted** does not mean **executed**, a sent message does not prove **observed**, and a committed branch does not prove **deployed**.

## Where each fact is owned

| Fact | Authority | How drift is detected |
| --- | --- | --- |
| Registered HTTP patterns and API level | `internal/transport/http/routes.go` | `tools/contract-gen -check` compares `api/v1/routes.json` and the build's route table. |
| Request and response field shapes | `api/v1/*.schema.json` | `tools/contract-gen -check` compares generated Go and TypeScript types. |
| Capacity bounds | `internal/domain/capacity` | The capacity guard requires every new bound to be registered; `tools/contract-gen -check` compares the generated build-bound Agent capacity reference. |
| Agent route navigation and relevant guide part | `api/v1/agent-routes.json` | Contract generation refuses an unmapped or removed route and checks the generated build-bound catalog. |
| Agent command and refusal handling | The relevant part of `skills/clawdline/guide.md`, compiled into the binary | Tests check current commands, route claims, and English/Taiwan Traditional Chinese parity; `contract-gen -check` compares the generated static refusal inventory. |
| Human task, success sign, and recovery action | `docs/user/` | Task links and paired risk scenarios in [Agent contract map](agent-contract.md). |
| Cloud deployment version | The deployed `BUILD.json` and served bundle | `CloudGate` in [hosted console operations](hosted-console.md). |

The human pages intentionally explain only the states a person can recognize and the action they can take. The Agent guide names operations, authority, typed refusals, and receipts. The two paths meet at the terms above and in the paired scenarios below.

## Publishing and compatibility

The installed binary is the source for `clawdline guide`, `clawdline guide routes`, and `clawdline guide capacity`, and `clawdline guide refusals`. An Agent checks that machine's guide and route catalog at the time of work; a saved copy from another build is not an authorization. The route catalog states the minimum API level for a registered pattern. A pattern's presence is only discovery: an Agent still follows the relevant guide part and current authorization requirements. An absent capability, older API level, unknown state, or missing receipt stops the dependent write.

Public help and the hosted console may be newer than a person's machine. A console feature that needs a route the machine lacks must show an update state. Human pages describe the common workflow and link the current technical reference; they must not promise that a stale local daemon can perform a newer action. Cloud does not upgrade or silently redirect a command to another machine.

[Human task index](user/README.md) · [Agent contract map](agent-contract.md) · [API schemas](../api/v1/) · [Capacity register](../internal/domain/capacity/)
