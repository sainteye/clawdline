# Agent operation contract map

## Agent entry

Read `clawdline guide` from the **installed binary on the machine that will act**, then read the named part when needed. `clawdline guide routes` prints this build's registered route inventory with its API level, the related guide part, and the corresponding human task. `clawdline guide capacity` prints the default bounds generated from this build's capacity register; check the target machine's live reading because an override may lower a bound. `clawdline guide refusals` lists statically declared typed codes; `clawdline guide refused <code>` finds an applicable handling rule, and an unknown code stops the action. Route presence is discovery, not permission or proof of completion. Compare a remote machine's advertised API level and capability before asking it to act; if it lacks the operation, stop and report that machine's update need. Do not try an older helper, a different machine, or a less restricted path.

The guide is compiled with the daemon. Each part has a `guide-version` hash; `--since <hash>` is a cheap freshness check. The [route catalog](../api/v1/agent-routes.json) is checked against the registered routes whenever `go run ./tools/contract-gen -check` runs. Request and response fields live in [JSON Schema](../api/v1/); bounds live in the [capacity register](../internal/domain/capacity/). The [shared vocabulary](contract-model.md) governs receipt words across surfaces.

## Load only the part for the job

| User's goal | Agent guide part | Human task page |
| --- | --- | --- |
| See or answer a Session | `connect`, `send`, `note`, `notify` | [Sessions](user/sessions.md) |
| Track or advance Board work | `feature-root`, `board`, `epic` | [Board](user/board.md) |
| Dispatch, check, land, or hand off work | `dispatch`, `running`, `landing`, `callback` | [Dispatch](user/clawdfather-and-dispatch.md) |
| Connect a Cloud browser or machine | `cloud` | [See another machine](user/cloud-first-look.md) |
| Schedule future work | `schedule` | [Schedules](user/schedules.md) |
| Finish a turn or close a Session | `report`, `refused`, `connect` | [Sessions](user/sessions.md) |

The guide part gives the command or route, identity and permission, precondition, request fields, bound, refusal, retry rule, and receipt where the operation needs them. When a route family combines several operations, do not infer the method or body from its catalog row: read the part and schema for the exact action. If the part does not specify an Agent action, treat the route as unavailable to an Agent. A refusal code is stable branching data; its prose message explains it to a person. Unknown or unlisted fields are not an invitation to guess.

## Paired high-risk checks

| Situation | What a person sees and does | What an Agent checks and does |
| --- | --- | --- |
| Cloud machine offline | The machine is offline; return to it and restore its connection. [Recovery](user/recovery.md) | Stop remote writes. A stale cached list or another machine is not a substitute. Recheck identity and connection before retrying. `cloud` owns pairing and command permission. |
| Browser can read but cannot send | Sessions remain visible; enable commands on the selected machine if sending is intended. [Cloud](user/cloud-first-look.md) | Check that browser pairing and the selected machine's command switch permit a write. Treat `forbidden` as refusal, not a retry cue. `cloud` and `send` own the contract. |
| Session state unknown | Open the live screen. [Recovery](user/recovery.md) | Do not treat unknown as idle, done, or closeable. Read fresh inventory; stop if identity or closeability is still unproven. `connect` and `refused` own the typed outcome. |
| Message or child work pending | Open the Session or item activity to see what actually happened. [Recovery](user/recovery.md) | Keep accepted, executed, delivered, observed, and acknowledged separate. Read the task or delivery receipt once; on timeout inspect before retrying an effectful command. `send`, `running`, and `callback` own receipts. |
| Board item cannot advance | The item names its outstanding step or phase; complete that action. [Board](user/board.md) | Read the current item version and exact typed refusal. `steps_incomplete` requires finishing verified steps; `version_conflict` requires a fresh read. Never invent a landing or deployment receipt. `feature-root` and `board` own these gates. |
| A Session is about to close | The close dialog shows whether obligations remain; return to the Session when it cannot prove safety. [Sessions](user/sessions.md) | Use the current close audit and current identity. `closeability_unknown` and `close_inventory_unavailable` stop closure. A send or notification is not acknowledgement. `connect` owns the close command. |

These cases are acceptance pairs: each has one observable human action and a machine decision to proceed, retry, or stop. The tests for route inventory and guide parity guard the machine side; the linked human pages keep the visible action in one place.
