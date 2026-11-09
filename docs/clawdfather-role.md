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

The current start action opens a neutral Session and returns `registration: pending`. In that Session, `clawdline coordinator bind` registers its conversation ID or, for a prior offline binding, rebinds with the observed ID and generation. An online holder or unknown liveness cannot be replaced. The start answer is not a role registration receipt. A complete source reading that proves the bound Session offline automatically releases its active registration. The broker retains the identity ledger and aliases for pending deliveries; a later registration reconnects that ledger with a generation compare-and-set. An unreadable source keeps the registration occupied.

Local and Cloud Session lists identify a machine-workspace Session as Clawdfather from its machine scope, even before registration. The crown is muted and the row says registration is pending until the broker confirms the role; a generic provider title or icon must not hide the Session's purpose on a phone. Cloud status carries only a machine-scope bit and a bound-role bit, not the coordinator record or private label.

The Clawdfather mark opens the same Snippet / Workflow sheet every Session has, with a Clawdfather tab added in front and selected. That tab lists the shipped machine-stewardship actions, then the person's own actions under **Added by you**. The shipped actions cannot be removed or edited; the person's own are added with the sheet's `＋` and edited or deleted from their `···`. They are stored as reusable text with `scope: "clawdfather"`, which is machine-wide and refuses a project path; every Session's snippet read carries them, and only this tab draws them. Clawdfather stands in no project, so its Snippet and Workflow tabs show the machine-wide groups only. Choosing any action inserts its prompt into the composer for review; it does not send or execute anything.

**Coordinate Session resources** opens a read of Sessions, lease holders and queue positions, file waits, task/callback records and durable pause receipts. Each source keeps its observation time or a named read error. Its suggested request asks Clawdfather to order conflicting operations using the existing leases and waits. The pause protocol waits for the receiver's safe-point receipt before calling it paused; `docs/session-resource-coordination.md` records the flow and recovery rules.

## Choices and trade-offs

A plain empty directory would need no new server code but would appear as a Project and permit Project assignment. Removing the role would simplify the product but leave no named machine steward for recurring management work. Allowing arbitrary edits to other Projects would be flexible but would erase the boundary between machine management and item ownership. This design keeps the role narrow and uses the existing audited operations.

This round does not add an arbitrary settings-file editor, background monitoring, automatic ownership transfer, online succession, or a filesystem sandbox. It also does not make Clawdfather the owner of other Board items. See [work-system-v2.md](work-system-v2.md) for Board ownership and [project-sync.md](project-sync.md) for import and export coverage.
