# Clawdfather, dispatch, landing and handoff

After this page your Claude Code and Codex sessions can use Clawdline themselves: hand a bounded
piece of work to a child session, get its result back, have the landing of that work recorded, and
pass a line of work to another session. The Board item's owning Session remains responsible for
that work. Machine Bearings can be read without creating a Clawdfather Session.

## Availability

Free and local, on macOS and Linux. Dispatch needs a terminal Clawdline can open a child in: tmux
(or iTerm2 on a Mac). On Windows dispatch stops with `no_child_capability` ([platforms.md](platforms.md)).

## 1. Teach your sessions: install the skill

Sessions learn Clawdline from a skill. Nothing installs it for you:

```sh
./bin/clawdline skill install
```

This writes a short stub to `~/.claude/skills/clawdline/SKILL.md`. The stub holds no routes of its
own; it tells the session to read the guide compiled into the `clawdline` binary, so the guide
always matches the daemon that answers. `clawdline serve` keeps a copy of itself at
`~/.config/clawdline-next/bin/clawdline` for the stub to find.

If a `SKILL.md` was already there, install records it first, and

```sh
./bin/clawdline skill uninstall
```

puts it back exactly. You can read the guide yourself, without a daemon:

```sh
./bin/clawdline guide              # the core, and a list of the other parts
./bin/clawdline guide dispatch     # one part
./bin/clawdline guide zh-TW        # in Traditional Chinese
```

**Check:** in a new Claude Code session, ask "dispatch a child to add a test for X". The session
reads the guide and runs `clawdline dispatch`.

## 2. Dispatch a child

A session (the *root*) dispatches with one command, the brief on standard input:

```sh
clawdline dispatch --title "Add a test for the parser" --claims internal/parser \
  --isolation worktree < brief.md
```

- `--claims` names the paths the child will change, so two children do not collide.
- `--isolation worktree` gives the child a git worktree of its own; without it the child works in
  the project directory.
- Also: `--assistant claude|codex`, `--model`, `--permission-mode ask|edits|full` (default
  `full`), `--timeout` in minutes (default 30, up to 240).
- It prints `dispatched <id> <state> [worktree <path>]`.

A root session may have **5** children at once by default (20 for the whole machine). The child
opens in its own terminal and appears in the console under its parent, marked └. When it finishes,
its result is typed into the root session.

After dispatching, the root ends its turn. The child's native completion notice wakes it; no
callback is needed to watch the child. The notice is a pointer, so the root reads that task once
and then handles the result. A short in-turn `task wait` is reserved for a result needed immediately
and expected soon. After a wait times out, the root ends its turn and follows the native notice;
it does not start another wait or dispatch the child again.
If `task wait` already printed a terminal result and closed its notice, a late notice for that task
does not call for another read or another integration.

```sh
clawdline task show <task id>     # state, summary, what is left, landing
```

## 3. Landing

A child finishing is not the work being done. Clawdline keeps them apart:

- When a finished task's branch is merged into its target, Clawdline records it as **landed**
  itself, within a few minutes.
- A cherry-pick, work folded in some other way, or a task with nothing to land is recorded by the
  root session; the guide's `landing` part says how.
- `clawdline landings` lists every landing still owed.

A session can also record its own finished turn, which marks it "delivered, awaiting approval" in
the console:

```sh
clawdline session report --summary "The parser test is in and passing"
```

## Callbacks for long commands

When a command such as a build, test run or deployment will take longer than the current turn,
the owning Session can hand it to the daemon with `clawdline callback`. The Session ends its turn;
the daemon runs the command once and sends a completion notice with its exit result. The Session
then checks that task's result once with `clawdline task show` before reporting an outcome. A callback
uses no child Session slot and makes no Git landing claim.

Use this when the work is a command that can run without Agent decisions. It avoids repeated
polling turns and their context reloads, replacing them with a completion follow-up; it does not
guarantee a fixed token or price saving.
Timeout or cancellation settles the callback. Success permits the next step; failure requires
inspection of the exit status and output; timeout or an uncertain result requires checking what
the command may have done before it stopped. Neither a reminder nor a timed-out wait means the
command should run again. Callbacks run on macOS and Linux; Windows reports
`no_callback_capability`. The installed Agent guide has the current command syntax:
`clawdline guide callback`.

## 4. Hand off a line of work

When a session is too full or should stop, it can hand its line of work to a new session. It
writes a handoff note — what the receiver must read, questions to verify before continuing, and
where to pick up — and Clawdline opens the successor with it and tells the sender when it has
picked up. A session holding the legacy Clawdfather role cannot hand off the line carrying that
role; an explicitly separate line has a guarded exception. For a new, independent line of work, a
session can open a new root session instead. Both are in the guide's `landing` part.

## 5. Clawdfather and Board ownership

Open **Machine load** from the Session counts, then choose **Open Clawdfather** to start an assistant in Clawdline's separate machine workspace. If a Clawdfather Session already exists, that action opens it. Its mark offers suggested requests; choosing one puts the text into the composer for review without sending it. The workspace is not a Project and is not offered for ordinary Project starts or Board assignment. Clawdfather can help inspect Sessions and machine Bearings, report what is known and unknown, change the machine settings Clawdline exposes, and import or export the Project metadata and supported assistant settings described in [project-sync.md](../project-sync.md).

Clawdfather does not edit Clawdline or any other Project's source code. When you ask it to arrange engineering work, it first creates a Board item in that Project, then assigns the item to a Project Session. That Session owns implementation, child dispatch, verification, merge and deployment. If you did not ask for a new Board item, Clawdfather proposes one for you to accept before delegation. The machine Session itself cannot own a Project item. The separate cwd is a workflow boundary, not an operating system sandbox.

Opening the neutral Session is not itself a coordinator registration. In that Session, run `clawdline coordinator bind` after its conversation ID is available. The command registers it or rebinds a proven offline predecessor using its current ID and generation. An online or unreadable old Session cannot be replaced. `clawdline guide coordination` has the request details. Machine Bearings do not require a registered coordinator.

The **Coordinate Session resources** entry shows each current Session reading beside lease holders and queue order, file waits, active tasks/callbacks and pause receipts. It identifies the source and observation time of every read. Clawdfather may request a Session pause; the Session finishes its important command and sends an explicit safe-point receipt before the panel calls it paused. Once the wake condition is met, Clawdfather sends a resume event. Sessions can also use the same leases, file waits and callback handoff while Clawdfather is offline. The command flow and failure states are in [Session resource coordination](../session-resource-coordination.md).

The rationale and limits are in [clawdfather-role.md](../clawdfather-role.md).

## Troubleshooting

- **`All N child slots for this session are busy`**: wait for a child to finish, or raise the
  limit (`orchestrator_max_children`, 1–10) in the macOS app's settings window, under
  **派工作給別的 session** (dispatch to other sessions).
- **A dispatch is refused with `stale_inventory`**: the command re-reads and retries once by itself;
  if it still fails, run it again.
- **An older coordinator registration is offline**: read `clawdline guide coordination` for the
  guarded rebind route. An unknown reading does not prove the old Session is gone.

## Deeper

- [skill.md](../skill.md) — the skill stub, the compiled guide and the thin commands.
- [worktrees.md](../worktrees.md) — how child worktrees are kept and cleaned up.
- [broker.md](../broker.md) — broker receipts and status; [coordination.md](../coordination.md)
  is an earlier proposal (in Chinese).
