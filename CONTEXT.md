# Clawdline context

This file defines the project vocabulary. It says what the words mean, not how the app implements
them. Operational rules remain in `AGENTS.md`; protocol details live in the documents linked from
each definition.

## Decision graph

The complete map from a stated destination through decision, delivery, review, correction,
verification, and landing nodes. Every node names its dependencies and acceptance conditions.
The graph is context for the whole line of work; a task is one execution attempt at one node.

## Destination

The observable end state the graph exists to reach. A destination is stronger than a task title:
it says what must be true when the line of work is actually complete.

## Frontier

The graph nodes whose dependencies have durable completion evidence and which may therefore start
now. Frontier membership is derived from task, review, verification, and landing receipts; it is never a
caller-declared ready flag.

## Fog of war

An uncertainty that could change the graph after work begins. Unknowns are named explicitly so a
discovery can revise the plan without being misreported as execution failure.

## Out of scope

A boundary the graph deliberately does not cross. It prevents an adjacent improvement from
quietly becoming part of the destination.

## Task

One bounded execution attempt owned by one assistant session. A task may deliver evidence for one
graph node, but task success alone does not mean the graph is complete.

## Claim

A task's declared write path, relative to its project. Claims prevent conflicting dispatch trees
from being briefed onto the same paths; they are dispatch-time reservations, not filesystem locks.
See `docs/orchestrator.md`.

## Serialize token

The name of a machine-wide operation that must run alone. Claims protect paths; serialize tokens
protect non-file operations such as replacing a running app. See `docs/orchestrator.md`.

## Delivered

A child produced the requested result. Delivery proves that an answer arrived; it does not prove
independent review, integration, or acceptance. See `docs/landing.md`.

## Reviewed

An independent reader evaluated specification, repository invariants, and runtime failure behavior
as separate axes and returned a typed verdict with evidence. `SAFE TO LAND` means review passed; it
does not mean integration happened. See `docs/verification-workflow.md`.

## Pending landing

Delivered and reviewed work for which a named root still owns integration. This is an active
obligation, not a completion state. See `docs/landing.md`.

## Landed

The reviewed change is present on the named target branch at a recorded commit, and the landing
receipt has been verified. See `docs/landing.md`.

## Focused proof

The narrowest check that can answer one implementation or review question. It belongs to the node
that changed or inspected the behavior. See `docs/verification-workflow.md`.

## Exact-tree acceptance

The root's verification of the precise candidate tree that will be committed, isolated from other
sessions' working-tree edits. It answers a different question from a child's focused proof. See
`docs/verification-workflow.md`.

## Root

The session that owns the complete feature lifecycle: graph, dispatch, review, correction,
integration, exact-tree acceptance, and final delivery receipt.

## Child

A bounded worker session dispatched for one task. A child delivers its result to the root and does
not land or commit shared-tree work.

## Handoff

A typed transfer of context or ownership between sessions. A handoff is acknowledged separately
from transport delivery so a sent message cannot masquerade as an observed one. See
`docs/handoff.md`.

## Acknowledgement

Evidence that the intended receiver observed a delivery. Accepted, executed, delivered, observed,
and acknowledged are separate states throughout the protocol.

## Attention request

A narrow, time-sensitive notification that asks the user to perform a concrete action which an
agent can already predict will block progress: return to a device, approve an operating-system or
browser permission, enter a credential, or make the final confirmation in an external service.
It is part of the delivery path, not an afterthought after the agent has gone idle.

An attention request says what action is needed and why. It never carries a secret, never grants
consent on the user's behalf, and never proves that the user observed it. The explicit question or
instruction remains in the owning Session; notification delivery and user acknowledgement are
separate facts. See `docs/dispatching.md` and `docs/api.md`.

## Context sources

- `AGENTS.md` — repository working agreements and safety rules.
- `docs/dispatching.md` — when work becomes a task and how a graph is staged.
- `docs/landing.md` — delivery, review, pending landing, and landed obligations.
- `docs/verification-workflow.md` — focused proof, review, correction, and exact-tree acceptance.
- `docs/handoff.md` — context transfer between sessions.
- `docs/api.md` — HTTP and JSON contracts.
- `docs/clawdline-protocol.html` — the public visual explanation of the complete protocol.

The glossary-and-pointer structure is adapted with thanks from
[mattpocock/skills](https://github.com/mattpocock/skills), especially its guidance on writing for
agents, domain vocabulary, decision maps, and explicit frontiers. Clawdline is not affiliated with
or endorsed by that project.
