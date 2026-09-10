# ADR 0003 — Durable atomic Program binding

Status: CLA-306 delivery candidate for independent review; not landed or deployed.
Decision date: 2026-09-11. Governing source: CLA-296 Plan v4 read on 2026-09-11.

## Decision

A cross-platform architecture Program is represented by one canonical top-level Board Epic and
one optional `programPlan` record owned by `ProjectBoardStore`. `plan_structure` is the only Plan
import operation. It receives a closed, versioned document containing the Program/Plan/graph
identity, destination, canonical Cloud planning document, logical nodes, DAG edges, gates,
capability observations and file claims.

The Store validates the complete proposed mutation—including late rows, bounds, duplicate keys,
references, cycles, document succession, item capacity and graph-index collisions—before changing
its draft. One accepted import then creates or upserts node items, records the document and graph
indices, advances one Board revision and performs one atomic persist. An error may add only the
ordinary idempotency refusal receipt; it cannot expose a partial Plan or child set.

Logical node keys are stable. A successor Plan must name the exact current predecessor, preserves
the item ID for every repeated key, and never deletes an omitted node, gate or capability. An
import cannot include gate approval state, and every successor version clears prior approvals.
The named gate authority may decide only a gate imported by the exact current version. Retained
gates and capabilities omitted by a successor remain visible but fail closed as stale evidence.

The planning projection has exactly three states: `planning_ready`, `blocked` and `unknown`.
Unknown capability evidence remains unknown; it is never optimistic support. Incomplete
dependencies, pending/rejected gates, unsupported capabilities and colliding ready claims block.
The projection explicitly says `advisory_only`, has no critical-path assertion and cannot replace
the broker's execution authority.

## Run and document settlement

A managed `begin` may request a Program binding only for its selected canonical Program. The
workflow fsyncs the event and outbox intent before returning `202`; this is admission, not success.
Its in-process Store command carries the exact Program key, Plan version, graph/node, Project,
run, Session/provider and process generation. The Store returns a durable, stable-id receipt naming
the requested classification/item, explicit reused imported-node resolution and effective child
item. The workflow also persists whether settlement consumed the original Store response or its
idempotent replay. Only then does it publish `settled`, bind later operations to that child, upsert
the source-aware Session relation and create the run's distinct span. Historical receipts survive a
valid successor Plan but cannot satisfy a mismatched current Plan/node request.

The versioned `document` workflow operation similarly journals before effect and materializes the
ordinary `document_reference` only on the effective item. Unsupported operation versions and
unknown fields—including caller-supplied authority or process identity—fail closed. Restart and an
ambiguous response replay the exact prepared Board request and use its durable Store receipt.

## Boundaries and consequences

- Program/Plan/node identity is explicit. Titles never resolve binding, and the Program container
  never becomes a fallback task owner for an unknown node.
- The Store remains the only Board writer. The workflow journal is a durable delivery mechanism,
  not another Board truth.
- Plan import, gate decisions, binding and document references do not mutate scope revision,
  lifecycle, verification, landing or acceptance pointers.
- Existing schema-1 Board state remains decodable because Program and receipt additions are
  optional. Existing workflow journals remain decodable through optional event/run fields.
- Decoded Program records re-establish the same closed vocabularies, bounds, uniqueness and
  cross-record identities as ingress; corrupt or unknown persisted state makes Board unavailable.
- W1 remains blocked until this candidate is reviewed, integrated, accepted from an exact tree and
  proved in the live Program binding path. This ADR does not authorize W1, rollout or deployment.

## Rejected alternatives

Creating children incrementally was rejected because a late invalid node could leak a partial
Program. Resolving nodes by title was rejected because repeated titles and renamed work cannot be
made stable. Treating `202` as a binding receipt was rejected because an admitted outbox may still
be pending or refused. Letting the planning frontier dispatch was rejected because it would create
a second authority beside the broker.
