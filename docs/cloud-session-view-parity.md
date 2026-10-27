# Cloud Session view parity

The selected-machine and all-machines views are two scopes over the same Session. The scope changes which rows are listed, not what a Session action means. This page records the current seams and the shared rule that prevents a retained display row from becoming an action target.

## Read paths

| Concern | Selected machine | All machines | Shared authority |
| --- | --- | --- | --- |
| Membership and activity | `relay-reader.ts` joins a machine status projection to a pinned rich presentation; its fallback reads retained Cloud rows. | `FleetSessionList.tsx` reads each machine's status projection independently. | The machine's current `ss/` projection decides membership, execution generation, activity state, and freshness. An incomplete pass does not prove absence. |
| Name and content | The selected list and detail read the machine's rich Session data. | The fleet list obtains names from pinned presentations; content arrives only after an exact Session is opened. | A status projection contains no title or transcript. Presentation is joined by machine, Session ID, and execution generation. |
| Open detail | The selected list opens by Session ID within its chosen machine. | The fleet list pins machine ID, Session ID, and execution generation before opening the same detail page. | `App.tsx` renders one detail component. A different execution must not inherit the opened Session's content or action. |
| Closeability | The rich row carries the machine's closeability state, reasons, and opaque version. | Opening a fleet row brings its machine's rich row into the same confirmation sheet. | The daemon recomputes closeability at close time. A changed version requires a new human decision against a new reading. |
| Close and archive | The selected list and detail request `ActionConfirm`. | The fleet swipe first pins an exact destination, then requests `ActionConfirm`. | `ActionConfirm` and the daemon's close/archive routes own the loss warning, forced press, version check, and result. |
| Send, answer, interrupt | The selected detail uses the Cloud writer for its chosen machine. | The fleet detail uses the same detail and writer after an exact destination is opened. | The writer carries the current execution generation; receipts decide the outcome. |

## Status line rule

Every scope draws a row's status line from the row alone, through `stateLine` in `session/List.tsx`. No scope reads a task collection of its own to describe a Session: the Cloud lists do not have one, and a second source is how the selected-machine list once said `Task: <task title>` while the all-machines list said only `Waiting for another Session` for the same Session. What a waiting Session waits for comes from the daemon's `waiting_for` (`docs/session-states.md`), which the compact `sessions.list` answer carries alongside the rest of the status; `session/waiting-for.ts` turns it into words for the list and the info sheet alike. A field a list needs that one scope cannot receive is added to the row, not computed differently per scope.

The tree chips (`Parent Session · N`, the child marker) are still drawn from the local task collection and are therefore absent from the all-machines list. They are the remaining known seam.

## Action rule

`session/swipe.ts` owns the close/archive availability rule for both list scopes and the detail menu. A current Session row may offer close; archive additionally requires a conversation ID. A retained `unverified` or `missing` rich row and a stale or unknown fleet row are display history. Neither authorizes a destructive press. A current status row stays actionable even if the machine roster has not caught up with it; the roster is not the Session observation.

The all-machines view is the reference for execution identity: a fleet target always names machine, Session, and execution generation. The selected-machine list still routes its confirmation by Session ID, so its rich row can age between display and press. The daemon's opaque closeability version is the final compare-and-swap, and the confirmation must show a fresh reading after `close_not_proven` before another press. Keep that recovery in the common confirmation sheet so both scopes receive it.

## Acceptance cases

1. A current Session row on either scope offers the same close/archive choices; archive requires a conversation ID.
2. A retained row remains visible with its last state and name but cannot close or archive.
3. A current fleet row remains actionable when only the machine roster is stale.
4. A restarted execution with a reused Session ID cannot inherit an earlier execution's content, confirmation, or receipt.
5. A changed closeability version reopens one confirmation with the new reasons and a new request ID. The first failed press cannot silently become a forced close.
6. A partial inventory never removes an unobserved Session. An authoritative complete inventory may remove it in either scope.

The status projection and rich presentation remain separate because Cloud can distribute the former without Session content. Their join and action policy are shared; copying the full Session list into the fleet status channel would change that privacy boundary.

## Phone browser navigation

The Session detail uses the browser's Back/Forward history in both list scopes.
Opening a phone detail keeps one list entry underneath it, including arrival by
link, reload, and a desktop selection becoming a phone detail after resizing.
The phone body and detail scrollers allow horizontal overscroll navigation;
vertical containment remains in place for transcript reading and refresh.
Code blocks, images and other controls keep their own gestures.

`session/address.e2e.ts` checks the real browser history and computed styles,
including Back/Forward after a width change. These checks do not reproduce a
physical iPhone's edge gesture; that remains a device acceptance check.

A restored `history.state.view === "detail"` belongs to an earlier document and
cannot prove a usable list still exists below it. A newly loaded document
rebuilds its list/detail pair once, then reuses its own detail step for Session
switches and Forward. This also puts a list immediately under a Session after
reloading a reusable, closed video-preview entry. A reload may leave an older
list entry further back; it must never require traversing a second detail to
reach the list. The regression fixture reconstructs a lone restored detail and
opens/closes the actual video modal before reloading. Physical media-return
reload behavior remains a separate iPhone acceptance case.
