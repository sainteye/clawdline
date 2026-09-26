# Documents

Every document in this directory, and what kind it is.

- **Public design note**: how this codebase works, or why it is built the way it is. It stays true
  after the Swift app is retired, and a contributor should read it.
- **Public acceptance contract**: the observable scenarios that a replacement must automate and
  pass before it can be called implemented or replace the current product behavior.
- **Internal migration record**: written while the Go rewrite replaced the Swift app. That covers
  measurements of the old app, porting inventories, wave plans and review logs. They are kept
  because the code and the design notes cite them. Parts of them describe a state that has since
  changed, and where one disagrees with a design note, the design note wins.
- **Work list**: an inventory of what is still wrong, written to be dispatched. Each item says how to
  make it fail, and the list goes stale as items land.
- **User page**: how to finish one job with Clawdline — what you need, the steps with the exact
  labels and commands, how to tell it worked and how to undo it. They link here for the design.

The user pages are in English, and so are the two pages that introduce the project and the pages
marked English in the tables below. The rest are written in Traditional Chinese.

## Using Clawdline

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [user/install.md](user/install.md) | User page | English | Build, start the daemon, open the console, and see a session in the list |
| [user/platforms.md](user/platforms.md) | User page | English | The macOS app, a Linux `systemd --user` service, and what Windows can do |
| [user/sessions.md](user/sessions.md) | User page | English | Read the list, open a session, answer, send pictures and snippets, dictate, start, stop and close |
| [user/keyboard-shortcuts.md](user/keyboard-shortcuts.md) | User page | English | Every key the console answers to |
| [user/notifications.md](user/notifications.md) | User page | English | Web Push on a computer or phone: turning it on, what you are told about, a test |
| [user/remote-access.md](user/remote-access.md) | User page | English | Another computer over SSH, a phone over your own tunnel, and Clawdline Cloud pairing and permissions |
| [user/schedules.md](user/schedules.md) | User page | English | Scheduled tasks on the local clock, and schedule webhooks on Cloud Pro |
| [user/board.md](user/board.md) | User page | English | The Board, session to-dos, the Now page and things waiting to be verified |
| [user/clawdfather-and-dispatch.md](user/clawdfather-and-dispatch.md) | User page | English | The agent skill, dispatching children, landing, handoff, and the Clawdfather role |
| [user/projects.md](user/projects.md) | User page | English | Adding projects, the Projects page, and bringing project settings to another machine |
| [user/usage.md](user/usage.md) | User page | English | Token bills by category, assistant quotas, the compaction window and capacity |
| [user/troubleshooting.md](user/troubleshooting.md) | User page | English | From a symptom to its cause and fix |

## Start here

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [getting-started.md](getting-started.md) | Public design note | English | From a clone to a running console, a session in the list, and Clawdline Cloud |
| [architecture.md](architecture.md) | Public design note | English | The whole system on one page, with links into everything below |

## How it is designed

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [token-ledger.md](token-ledger.md) | Public design note | English | What a session's tokens were spent on — board, protocol, rules, work, delegation — and how the ledger measures it |
| [verifications.md](verifications.md) | Public design note | English | Things waiting to be verified: a change, a date to look again, what would count as it holding and the data it is judged by — the sidebar's 驗收, `clawdline verify`, and a scheduled readout |
| [design-guidelines.md](design-guidelines.md) | Public design note | English | Ten design rules, each paid for by a failure: a loop that stops says so, everything that accumulates has a limit, unknown is not absent |
| [design-decisions.md](design-decisions.md) | Public design note | Chinese | The decision register the implementation follows. Where an analysis below disagrees, this wins. It also records decisions about the old app's data |
| [work-system.md](work-system.md) | Public design note | Chinese | The work system on one page: board items, session to-dos, the Backlog and GitHub Issues, each with its lifecycle, what is built and what is only designed, and how the page itself is changed |
| [work-system-v2.md](work-system-v2.md) | Public design note | English | The approved replacement: person-created Project work, human assignment, Agent-driven lifecycle, broker evidence, proposals and direct Session to-dos |
| [work-system-v2-acceptance.md](work-system-v2-acceptance.md) | Public acceptance contract | English | Positive, negative and failure-injection scenarios that must pass before work system v2 can replace the current implementation |
| [limits.md](limits.md) | Public design note | Chinese | Every bounded thing: its limit, what happens when it is full, and who finds out |
| [session-restore.md](session-restore.md) | Public design note | English | Offering back the Claude Code and Codex sessions a reboot or crash took away: the boot-id rule, the record, the routes, and what is not covered |
| [coordination.md](coordination.md) | Public design note | Chinese | The coordinator role redesigned around one `Obligation` model for waits, landings, handoffs and dead letters |
| [broker.md](broker.md) | Public design note | Chinese | The broker's routes and credentials, and where it differs from the Swift app on purpose. Written at its first wave; later waves added handoffs, leases and reclaim |
| [board-redesign.md](board-redesign.md) | Public design note | English | Board, backlog and session to-do: three structures, their lifecycles, and where a person joins in |
| [github-issues.md](github-issues.md) | Public design note | Chinese | What GitHub Issues should take over, what stays local, and the three of its designs worth borrowing |
| [schedules.md](schedules.md) | Public design note | Chinese, migration in English | Schedules: the file format, the clock, catch-up, and importing |
| [push.md](push.md) | Public design note | Chinese | Web Push: the three RFCs, what never leaves the machine, retries |
| [remote.md](remote.md) | Public design note | Chinese | The line between the free product and Cloud, and the pairing promises. Its status table is dated |
| [cloud-wire.md](cloud-wire.md) | Public design note | Chinese | The Cloud wire specification: envelope, canonical JSON, keys, pairing, commands, and what was measured at each stage |
| [shell-bridge.md](shell-bridge.md) | Public design note | Chinese | The interface between a native shell and the web console, and the minimum a new platform's shell must implement |
| [privacy-guard.md](privacy-guard.md) | Public design note | English | What keeps the person's own things out of a public repository: the working-tree scan, the history scan, the four answers, and the checkpoint that makes a daily run affordable |
| [publishing.md](publishing.md) | Public operations guide | English | CI coverage, the guarded development remote, the filtered publication path, and when GitHub status becomes visible |
| [project-sync.md](project-sync.md) | Public design note | English | A project's name, icon and untracked skills owned by one machine and mirrored read-only on others: identity by git origin, what is never copied, Cloud and file transport |
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

## What is still owed

| Document | Kind | Language | What it is |
| --- | --- | --- | --- |
| [first-run-audit.md](first-run-audit.md) | Work list | Chinese | What somebody who is not the author hits the first time, from `git clone` to a phone: 91 findings with file and line, grouped into ten causes to dispatch as ten pieces of work (2026-09-21) |
