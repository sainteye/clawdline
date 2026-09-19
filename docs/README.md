# Documents

Every document in this directory, and what kind it is.

- **Public design note**: how this codebase works, or why it is built the way it is. It stays true
  after the Swift app is retired, and a contributor should read it.
- **Internal migration record**: written while the Go rewrite replaced the Swift app. That covers
  measurements of the old app, porting inventories, wave plans and review logs. They are kept
  because the code and the design notes cite them. Parts of them describe a state that has since
  changed, and where one disagrees with a design note, the design note wins.

Five pages are in English: the two that introduce the project, and the three design notes an
outside reader judges it by. The rest are written in Traditional Chinese.

## Start here

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [getting-started.md](getting-started.md) | Public design note | English | From a clone to a running console, a session in the list, and Clawdline Cloud |
| [architecture.md](architecture.md) | Public design note | English | The whole system on one page, with links into everything below |

## How it is designed

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [design-guidelines.md](design-guidelines.md) | Public design note | English | Ten design rules, each paid for by a failure: a loop that stops says so, everything that accumulates has a limit, unknown is not absent |
| [design-decisions.md](design-decisions.md) | Public design note | Chinese | The decision register the implementation follows. Where an analysis below disagrees, this wins. It also records decisions about the old app's data |
| [limits.md](limits.md) | Public design note | Chinese | Every bounded thing: its limit, what happens when it is full, and who finds out |
| [coordination.md](coordination.md) | Public design note | Chinese | The coordinator role redesigned around one `Obligation` model for waits, landings, handoffs and dead letters |
| [broker.md](broker.md) | Public design note | Chinese | The broker's routes and credentials, and where it differs from the Swift app on purpose. Written at its first wave; later waves added handoffs, leases and reclaim |
| [board-redesign.md](board-redesign.md) | Public design note | English | Board, backlog and session to-do: three structures, their lifecycles, and where a person joins in |
| [github-issues.md](github-issues.md) | Public design note | Chinese | What GitHub Issues should take over, what stays local, and the three of its designs worth borrowing |
| [schedules.md](schedules.md) | Public design note | Chinese, migration in English | Schedules: the file format, the clock, catch-up, and importing |
| [push.md](push.md) | Public design note | Chinese | Web Push: the three RFCs, what never leaves the machine, retries |
| [remote.md](remote.md) | Public design note | Chinese | The line between the free product and Cloud, and the pairing promises. Its status table is dated |
| [cloud-wire.md](cloud-wire.md) | Public design note | Chinese | The Cloud wire specification: envelope, canonical JSON, keys, pairing, commands, and what was measured at each stage |
| [shell-bridge.md](shell-bridge.md) | Public design note | Chinese | The interface between a native shell and the web console, and the minimum a new platform's shell must implement |
| [cross-platform.md](cross-platform.md) | Public design note | English | Every feature on macOS, Linux and Windows, and what a platform shows when it cannot do something |

## How it got here

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [plan.md](plan.md) | Internal migration record | Chinese | The rewrite's original plan. §1 (the ten rules) and §3 (package layout, the web split) still describe the design; the rest records coexistence with the Swift app and the milestones |
| [cutover.md](cutover.md) | Internal migration record | Chinese | What retiring the Swift app costs, item by item, and in what order |
| [replica.md](replica.md) | Internal migration record | Chinese | How the console was copied screen for screen from the original, and the review rounds |
| [wave8.md](wave8.md) | Internal migration record | Chinese | The plan for one wave of parallel work: pictures, dictation and the session menu |
| [broker-design.md](broker-design.md) | Internal migration record | Chinese | The analysis of which parts of the old broker to port and which to redesign |
| [broker-design-challenge.md](broker-design-challenge.md) | Internal migration record | Chinese | A deliberate challenge to that analysis: which ports were only history |
| [board-design.md](board-design.md) | Internal migration record | Chinese | The analysis of the old board. Still the reference for the read-only view of its cards |
| [timeline-design.md](timeline-design.md) | Internal migration record | Chinese | The analysis of the old project timeline, which this generation has not ported |
