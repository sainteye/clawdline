# Human intervention notes

A human intervention is an Agent-authored request shown above one target Session's conversation. A coordinating Root can write a note on a different Session. The note records both the source conversation and the target conversation, plus a snapshot of the source label. It is for a concrete human action that blocks or informs the next step, not a progress log or a private Agent reminder.

The Console shares one collapsed header row between Session to-dos and intervention notes. The right-hand attention control shows a red dot while at least one note remains unresolved, even after the person marks it read. Opening attention reveals the notes without opening the to-do list. Resolving the last open note removes the dot; recent resolved notes remain available inside the attention panel.

## Trust and responsibility

`POST /v1/work/v2/agent/human-interventions` requires the machine credential. Its `source_conversation` must resolve to a live Root; `target_session` must be a live, uniquely resolved Session row with a conversation. The source and target may differ. The machine credential identifies the machine, not the calling Session. A caller holding it can name a different live Root as the source; this route does not provide cryptographic per-Session authorship. The stored source label is provenance for the person, not proof of caller isolation.

The request requires `kind` (`read`, `answer`, `action`, or `report`), `title`, `summary`, `action`, and `reason`. The action says what only the person can do; the reason says why the Agent cannot proceed on its own. An `answer` may contain two to four `{label,draft}` suggestions. Choosing one inserts editable text into the selected Session's composer; the person must press Send separately. Reading, resolving, choosing a suggestion, and sending a conversation message are distinct events. A note does not grant formal work authorization. Decisions with a work item, deadline, and safe default remain Board decisions.

An optional `detail` expands in the card. An optional `document_url` must be a complete canonical `https://app.clawdline.com/#document=1...` locator. Its `machine` and `session` identify the target machine and route Session row; those are checked against the resolved target. The locator's `session` is not the assistant conversation id. Project scope needs `path`; task scope also needs a task UUID. Paths are relative `.md`, `.markdown`, or `.txt` files. Credentials never belong in a link.

## Person routes

- `GET /v1/work/v2/human-interventions/conversation%3A<conversation-id>` lists up to eight open notes and five recent resolved notes for the named conversation. A person with read access may use it. A failed read is an error, not an empty list.
- `POST /v1/work/v2/human-interventions/conversation%3A<conversation-id>/<note-id>/{read|resolve|reopen}` requires a person with send access, an `Idempotency-Key`, and `{"expected_version":N}`. Stale versions return `version_conflict`; a note belonging to another conversation returns `intervention_not_found`.

`read` sets only `read_at`; it does not remove an unanswered request. `resolve` sets `resolved_at` and, if needed, `read_at`. `reopen` restores a resolved request to the open list, subject to the open cap. There is no automatic resolution when the person sends a message.

If a person chooses a draft while looking at a subagent transcript, the Console returns to the Root conversation before inserting that draft and focusing the composer. A failed note refresh leaves the previous text visible for context but disables its copy, draft, and state controls until a successful reread. A version conflict triggers a reread before another state action. The state controls report their result to keyboard and screen reader users; marking a note handled neither sends the draft nor approves a Board decision.

## Capacity, history, and attention

Each target conversation holds at most eight open notes. The machine holds at most 2,000 note rows. At the total limit, creating a note prunes the oldest resolved row in the same transaction and increments a durable retention counter; when every row is open, creation returns `interventions_full`. The UI states when resolved history has been pruned. Resolved note history is a bounded convenience view, not permanent decision evidence.

Creating or updating a note sends no push notification and has no retrying notifier. When work is actually waiting for the person, the Agent still records the waiting-user state and uses the existing attention notification once. The terminal watcher and manual notification do not share a per-obstacle receipt and may both notify; this feature does not promise cross-path deduplication.

The Console's Cloud relay carries the person read and action routes to the selected machine. Cloud availability is checked by the normal document reader and relay error states; a card does not imply that a linked document was opened successfully.
