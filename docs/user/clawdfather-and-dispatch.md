# Clawdfather, dispatch, landing and handoff

After this page your Claude Code and Codex sessions can use Clawdline themselves: hand a bounded
piece of work to a child session, get its result back, have the landing of that work recorded, and
pass a line of work to another session. You can also make one session **Clawdfather**, the
machine's coordinator, and read the whole machine through it.

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

## 4. Hand off a line of work

When a session is too full or should stop, it can hand its line of work to a new session. It
writes a handoff note — what the receiver must read, questions to verify before continuing, and
where to pick up — and Clawdline opens the successor with it and tells the sender when it has
picked up. The session holding the Clawdfather role cannot hand off. For a new, independent line
of work, a session can open a new root session instead. Both are in the guide's `landing` part.

## 5. Clawdfather

One live session can hold the machine-wide coordinator role.

**Make one.** When you start a session (**+** in the session list), tick **把新的 session 命名為
Clawdfather** (name the new session Clawdfather). It appears only while the machine has no
coordinator. Clawdline asks the new session to register itself. With the skill installed, you can
instead ask any live session to take the Clawdfather role; the guide's `coordination` part has the
route.

**Use it.** Clawdfather's row in the list carries a crown. Press it to open its controls:

| Group | Controls | Today |
| --- | --- | --- |
| **觀察** (observe) | **狀態報告** (status report), **你離開之後** (since you were away), **重工、衝突與歸屬** (duplicates, conflicts and ownership), **落地收尾** (landing closure) | Read the machine's state and show it |
| **協調** (coordinate) | **深度狀態盤點** (deep status audit) | Sends Clawdfather an audit request after a second press |
| | **協調工作**, **問 Clawdfather**, **派出獨立工作** (coordinate work, ask, dispatch independent work) | Draft or preview only; nothing is sent |
| **在線狀態** (presence) | **安靜監看** (quiet watch) | Draft only |
| **管理** (administration) | **範圍與權限** (scope and permissions) | Read |
| | **停止 Clawdfather**, **重新連上 Clawdfather** | Preview only |

If Clawdfather's session goes offline, another session can take the role over; succession by
Clawdline itself is not built.

## Troubleshooting

- **`All N child slots for this session are busy`**: wait for a child to finish, or raise the
  limit (`orchestrator_max_children`, 1–10) in the macOS app's settings window, under
  **派工作給別的 session** (dispatch to other sessions).
- **A dispatch is refused with `stale_inventory`**: the command re-reads and retries once by itself;
  if it still fails, run it again.
- **The new session did not become Clawdfather**: ask it to follow `clawdline guide coordination`
  and register itself.

## Deeper

- [skill.md](../skill.md) — the skill stub, the compiled guide and the thin commands.
- [worktrees.md](../worktrees.md) — how child worktrees are kept and cleaned up.
- [broker.md](../broker.md) and [coordination.md](../coordination.md) — the broker and the
  coordinator's design (in Chinese).
