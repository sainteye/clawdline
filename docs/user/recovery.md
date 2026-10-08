# Recover from a problem

Start with what you can see. Each row gives one action to take next. If the state remains after that action, open the linked detail page.

| What you see | What it means | Do this now |
| --- | --- | --- |
| Clawdline's page will not open on this machine | The local service or console may not be ready | Run `clawdline doctor` on that machine and follow its named repair action. |
| A Session is missing from the list | Clawdline has no supported terminal to discover for that conversation | Run the assistant inside tmux on macOS or Linux, then refresh the list. |
| A Session state is unknown | Its screen or transcript cannot be read well enough to classify it | Open the Session's live screen to inspect it directly. |
| A machine shows offline in Cloud | The browser cannot get a fresh view from that machine | Return to that machine and restore its connection. |
| A paired browser can read but cannot send | That machine has not allowed Cloud commands, or this browser lacks permission | Check the selected machine's Remote permission setting. |
| A message or Agent action remains pending | Acceptance is not proof that the terminal acted or that the person saw it | Open the Session conversation and check for the resulting message or receipt. |
| A Board item cannot advance | It still owes a step, review, verification, landing, or deployment record | Open the item's activity and complete the named outstanding action. |

Never infer that a request succeeded merely because a button stopped spinning. Clawdline distinguishes accepted work from what was executed, delivered, observed, and acknowledged. If the current evidence is unavailable, the outcome is unknown until you check the relevant Session or receipt. [Shared terms and states](../contract-model.md#shared-terms-and-states).

For detailed service checks, error codes, logs, and source builds, see [technical troubleshooting](troubleshooting.md). For Cloud pairing and revocation, see [remote access](remote-access.md). Agents should use the [build-bound guide](../agent-contract.md#agent-entry) for typed errors and retry rules.
