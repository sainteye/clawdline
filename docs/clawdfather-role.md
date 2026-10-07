# Clawdfather as a machine steward

A Clawdfather Session starts in a daemon-owned machine workspace, separate from every Project. Its job is to help a person inspect and manage Clawdline across Sessions: explain machine Bearings, collect evidence for reports, change supported machine settings, and move the project metadata and supported assistant settings that Clawdline's import and export already handle. The Board item owner still plans, dispatches, verifies, merges and deploys that item's work.

## Why a separate workspace

A normal empty directory would become a Project as soon as its assistant Session appeared in the live inventory or provider history (`internal/adapters/projects/places.go`). The machine workspace is therefore excluded from Places, the Project catalog and Project registration. The machine load dashboard offers it as a separate action and opens a focused assistant chooser when needed. The server resolves the location from its state directory; a browser cannot supply a path or command. New role registrations and rebindings require a Session in that workspace; old bindings remain readable and idempotent until they can be replaced safely.

The workspace is an organizational boundary, not a filesystem sandbox. A provider may still have operating system access outside its cwd. Clawdfather never changes source code, including Clawdline code. On the person's request, it creates a Board item in the relevant Project before delegating to a Project Session. That Session becomes the owner and may dispatch child work; the neutral machine Session does not own the Project item or land its code. Without an explicit request to create an item, Clawdfather files a proposal for the person instead. A hard cross-platform file restriction would need a separate sandbox design and verification.

## Duties and limits

| Need | Existing source or operation | Boundary |
| --- | --- | --- |
| Machine-wide Session, task, landing and wait reports | Session inventory and coordinator Bearings | Cite the source, observation time and unknown readings; a request is not proof of observation or completion. |
| Machine settings | `clawdline setting get/set` | Only the current allowlist; state a proposed value and effect before mutation. |
| Project metadata and supported assistant settings | `clawdline project export/import` | Follow the project sync allowlist and conflict checks. Secret and local permission files stay excluded. |
| Clawdline code | Create its Board item first, then assign a Clawdline Project Session | Clawdfather never edits or lands code; the assigned owner handles child dispatch, verification and deployment. |
| Other Project code | Create its Board item first when requested, then assign that Project's Session | Clawdfather never edits or lands code. Without a request, report or propose. |

The current start action opens a neutral Session and returns `registration: pending`. In that Session, `clawdline coordinator bind` registers its conversation ID or, for a prior offline binding, rebinds with the observed ID and generation. An online holder or unknown liveness cannot be replaced. The start answer is not a role registration receipt.

The Clawdfather mark opens a short list of machine-stewardship suggestions in place of that Session's ordinary Snippets entry. Choosing one inserts its prompt into the composer for review; it does not send or execute anything.

**Coordinate Session resources** opens a read of Sessions, lease holders and queue positions, file waits, task/callback records and durable pause receipts. Each source keeps its observation time or a named read error. Its suggested request asks Clawdfather to order conflicting operations using the existing leases and waits. The pause protocol waits for the receiver's safe-point receipt before calling it paused; `docs/session-resource-coordination.md` records the flow and recovery rules.

## Choices and trade-offs

A plain empty directory would need no new server code but would appear as a Project and permit Project assignment. Removing the role would simplify the product but leave no named machine steward for recurring management work. Allowing arbitrary edits to other Projects would be flexible but would erase the boundary between machine management and item ownership. This design keeps the role narrow and uses the existing audited operations.

This round does not add an arbitrary settings-file editor, background monitoring, automatic ownership transfer, online succession, or a filesystem sandbox. It also does not make Clawdfather the owner of other Board items. See [work-system-v2.md](work-system-v2.md) for Board ownership and [project-sync.md](project-sync.md) for import and export coverage.
