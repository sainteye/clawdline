# The skill, the guide and the thin commands

> How a session learns to use this daemon, and why none of it depends on the Swift app. Written for
> the cutover: the design is §5 of the W8 route report, and the decisions it rests on are
> `design-decisions.md` D16, D23 ③ and D36, with U5 as the user's call.

## The problem

Every assistant session on this machine loads a skill called `clawdline`. Until now that skill was a
symbolic link into the Swift app's checkout, and the stub it pointed at found its guide inside the
Swift app's bundle. **When that app stops, the path breaks**: sessions keep loading a stub whose
reader is gone, and whatever they do instead is guessed from memory. The guide it served also
describes the Swift app's routes — the workflow envelope, graphs, durable reports — several of which
this daemon does not have.

Three things replace it, and all three are this binary:

| Piece | What it is | Where |
| --- | --- | --- |
| `clawdline guide [lang] [part\|all]` | The guide, compiled into the binary with `go:embed`, printed as a core plus parts | `skills/skills.go`, `skills/sections.go`, `skills/clawdline/guide*.md` |
| `<state dir>/bin/clawdline` | A copy of the running binary at a path that does not move | `internal/adapters/skillfile/binary.go` |
| `clawdline skill install` / `uninstall` | Writes the stub to `~/.claude/skills/clawdline/SKILL.md`, and puts back what was there | `internal/adapters/skillfile/skillfile.go` |

And a handful of **thin commands** a session runs instead of hand-built curl: `session report`,
`send`, `notify`, `landings`, `assistants` (`cmd/clawdline/session.go`, `relay.go`, `broker.go`).

## The guide is compiled in

The old app kept its guide beside its reader in the bundle so the two could not drift. Compiling it
in keeps that property and drops the file lookup: `clawdline guide` prints the guide written for the
exact build it is, on macOS, Linux and Windows, without a running daemon or a network.

- Sources: `skills/clawdline/guide.md` (English, the reference) and `skills/clawdline/guide.zh-TW.md`
  (Traditional Chinese, for the person reading along). `clawdline guide -list` names what a build
  carries.
- **Only routes this daemon serves are in it.** `TestTheGuideNamesOnlyRoutesThisDaemonServes`
  (`skills/skills_test.go`) reads every `/v1/…` path in both guides and fails on one that no
  `mux.Handle`/`mux.HandleFunc` in `internal/transport/http` registers. A route renamed or removed
  there turns it red until the guide follows. `TestBothGuidesNameTheSameRoutes` keeps the
  translation naming the same routes as the English.

## Printed in parts

`clawdline guide` prints the **core** — who the session is, how to reach the daemon, reporting its
turn, telling the person, what a refusal means — and a list naming every other part with the
command that prints it: `clawdline guide dispatch`, `clawdline guide board`, … `clawdline guide all`
is the whole text. Measured on 2026-09-25 by appending each to a system prompt and comparing with and
without: the whole guide is 15,708 tokens in English and 18,619 in Traditional Chinese, the core
2,184 and 2,711. Whatever a session reads stays in its context and is re-read on every later call,
and a week's transcripts showed twenty roots reading the guide 136 times, most of them a section at
a time with `sed` because that was all they needed.

- `skills/sections.go` names the fifteen parts — every `##` and `###` heading, matched by order.
- `TestThePartsAreTheWholeGuide` puts the parts back together and compares them with the guide byte
  for byte, in both languages; `TestBothGuidesHaveTheSameSections` checks each part opens with its
  own heading. A heading added to one guide only turns both red.

## The stable path

The stub has to name something it can run, and the binary moves: it is inside an app bundle, or
wherever `go build` put it. So `clawdline serve` copies itself to `<state dir>/bin/clawdline`
(`clawdline.exe` on Windows) when it starts, **writing only when the SHA-256 differs** — the same
rule D23 ③ sets for the dispatch policy's projection. A hundred starts with one build write the file
once. It runs off the startup path and never stops the daemon; a failure is one line in the daemon
log.

A copy rather than a link: a link into an app bundle breaks when the bundle moves, and Windows does
not let an ordinary user make one.

The stub looks for its reader in this order, and stops at the first that exists — a later path may
be a different build from the daemon that will answer:

1. `$CLAWDLINE_SKILL_READER`.
2. `$CLAWDLINE_NEXT_DIR/bin/clawdline`, else `~/.config/clawdline-next/bin/clawdline`
   (`$XDG_CONFIG_HOME/clawdline-next/…` when set; `%APPDATA%\clawdline-next\bin\clawdline.exe` on
   Windows).
3. `/Applications/Clawdline Next.app/Contents/MacOS/clawdline`, then the same under `~/Applications`.

## Installing the stub is the person's decision

The stub lives in the person's own assistant configuration, so **nothing installs it by itself**
(DG-10): a person runs `clawdline skill install` on the cutover day. Two skills with one name cannot
coexist — Claude Code tells skills apart by their directory — so before that day the new stub is only
tried inside this repository.

What install does, and why each step is there:

1. **Refuses a skill directory that is a symbolic link**: a file written into it would land
   wherever it points.
2. **Records what is at `SKILL.md` before replacing it** — a link by its target, a file by a backup
   of its bytes and their SHA-256, nothing by saying so — in `<state dir>/skill/install.json`. The
   record is written before the stub (DG-4: the intent first).
3. **Replaces by rename, never by writing to the path.** Today's `SKILL.md` is a link into the Swift
   app's checkout; writing to its path would overwrite that checkout's file. A rename replaces the
   link itself. `TestAnInstallReplacesALinkAndNeverWritesThroughIt` holds this.
4. **Is idempotent.** The same bytes already there: nothing is written, and it says so. An older stub
   of ours: replaced, and the record of what was there before the first install is kept.
5. **Refuses what it cannot account for** (DG-7): a stub edited after the install, an unreadable
   record, a backup whose bytes are not the recorded ones. Each is left as it is.

`clawdline skill uninstall` puts back exactly what was recorded — the same link target, or the
backup after its digest is checked — and only when `SKILL.md` is still the recorded stub (or gone).
No record means nothing to undo, and it says that too.

Neither reads or writes `~/.config/clawdline`: a state directory that is, or resolves into, the
Swift app's is refused.

## The thin commands

Each is one or two requests to this machine's daemon, and prints what the daemon said: the JSON on
stdout when it said yes, `refused, <status> <code>: <message>` on stderr and exit 1 when it said no.

| Command | Route | Notes |
| --- | --- | --- |
| `clawdline session report --summary …` | `GET /v1/orchestrator/whoami`, then `POST /v1/orchestrator/sessions/<terminal>/complete` | The conversation comes from `CLAUDE_CODE_SESSION_ID` or `CODEX_THREAD_ID`, else `--conversation`; `--terminal` skips the lookup. The terminal id is escaped as one path segment |
| `clawdline send --to <terminal> [text…]` | `POST /v1/orchestrator/messages` | Prints the `Idempotency-Key` before sending; `--key` retries the same message instead of typing it twice |
| `clawdline notify --title … --body …` | `POST /v1/orchestrator/notify` | |
| `clawdline landings`, `clawdline assistants` | `GET /v1/orchestrator/landings`, `…/assistants` | |

**The credential stays in the process.** The orchestrator token is read from
`<state dir>/orchestrator-token` — `CLAWDLINE_NEXT_DIR`, else `~/.config/clawdline-next` — and
never from `~/.config/clawdline`. It is not an argument, so `ps` never shows it; it is never
printed; an answer that somehow carried it is printed with it masked. The port is `--port`, else
`CLAWDLINE_NEXT_PORT`, else 7727 — the same resolution `clawdline serve` uses.

`clawdline send` used to type straight into a terminal with nothing recorded. That is now
`clawdline type`, kept for proving a terminal backend; a session uses the relay, which checks the
other side is a composer and keeps a receipt.

## Guidelines answer

1. **DG-1**: the projection logs when it writes and when it fails; nothing else here loops.
2. **DG-2**: one record, one backup, one binary copy; each is replaced, never appended.
3. **DG-3**: the projection is one read of a 25 MB binary per start, and one write when it changed.
   A relay took 30.6 s and 31.2 s end to end on this Mac (two runs against a private daemon); the
   time is spent in the daemon before it types, not in the command, so the command waits up to two
   minutes rather than the 30 s it first had. At 30 s it hung up once mid-relay, and the daemon
   logged the effect as having run and not been recorded — a daemon-side defect reported with the
   task, not fixed here.
4. **DG-4**: install records before it replaces; the record is the only fact kept.
5. **DG-5**: `send` now has one meaning (the relay); the direct typing is `type`.
   `RefuseForeign` repeats `within`/`resolved` from `internal/adapters/nextconfig` and
   `internal/adapters/devices` — a third copy, to be shared when one of those packages exports it.
6. **DG-6**: install and uninstall decide on the bytes they then replace, read in the same call.
7. **DG-7**: an edited stub, an unreadable record and a changed backup are all refused.
8. **DG-8**: the write-through test was run against a version that wrote to the path, and failed;
   the route test was run against a guide naming `/v1/orchestrator/workflow/gaps` (a Swift route),
   and failed. Both logs are in the task report.
9. **DG-9**: the reader order, the "stop at the first that exists" rule and the token rule come
   from the Swift app's stub and helper; the guide's content does not, because its routes are not
   this daemon's.
10. **DG-10**: install is a person's command, never run by the daemon.
