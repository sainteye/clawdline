# Running clawdline-go on Linux

Measured on 2026-09-20 on a headless Ubuntu 24.04 server (amd64, 2 vCPU, tmux 3.4, no desktop),
reached over SSM with no inbound port opened. Everything below was run; nothing here is inferred.
`docs/cross-platform.md` decides what Linux *should* do — this file says what it *did*.

**In one sentence: the daemon, the session list, the console, the screen reads, the key presses and
the whole dispatch round trip work on Linux; two defects stopped a stock build short, both in code
that is not Linux-specific, and §4.1 is fixed in the same commit that added this file.**

## 1. Get it running

The daemon is one static binary. Cross-compile it anywhere:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o clawdline ./cmd/clawdline   # ~26 MB, 2.5 s
(cd web && npm install && npm run build)                                      # web/console/dist
```

Ship the binary and `web/console/dist` to the machine by any means that needs no new
infrastructure. On the box:

```sh
export CLAWDLINE_NEXT_DIR=/var/lib/<user>/state       # optional; default below
export CLAWDLINE_NEXT_PORT=7727                       # optional
export CLAWDLINE_NEXT_WEB=/path/to/dist               # required, or the console 501s
./clawdline doctor      # prints version, port, state dir, store counts
./clawdline serve
```

### Let the Agent deploy later releases

A root-owned system service makes every later release need an administrator even though the daemon
itself runs as an unprivileged account. A headless installation should instead use the tracked
`systemd --user` unit. The administrator is needed once to enable linger and move supervision; the
service account can then build, switch, restart, verify and roll back its own daemon without gaining
permission to write any root-owned path.

Stage the first release as the service account:

```sh
tools/deploy-linux-user.sh --stage-only
```

Then run the one-time migration from an administrator shell, naming that account:

```sh
sudo tools/bootstrap-linux-user-service.sh clawdline
```

Every later update is one unprivileged command, suitable for an Agent to run directly:

```sh
git fetch origin main
git merge --ff-only origin/main
tools/deploy-linux-user.sh
```

The deploy builds from a disposable checkout, leaves the running version alone when a check fails,
atomically switches `~/.local/share/clawdline-next/current`, restarts only the daemon, checks both
`GET /` and the authenticated `BUILD.json`, and restores the previous release if either check fails.
The user unit uses `KillMode=process`, so restarting the daemon does not kill the tmux server or the
assistants it opened. `loginctl enable-linger` keeps the user manager alive without an interactive
login; that is why the bootstrap needs root exactly once.

**This section said, when it was written on 2026-09-20, that two more variables were required on
Linux.** They are not any more, and the same change is why: until 2026-09-19 the daemon forwarded
every route it had not taken over to port 7717, where the Swift app answered on macOS and nothing
answered anywhere else, so `GET /v1/sessions` came back `502 upstream_unreachable` on a machine
where the session list is the product. Forwarding is now off unless
`CLAWDLINE_NEXT_UPSTREAM_PORT` asks for it, so a stock Linux build refuses unimplemented routes by
name and answers `/v1/sessions` itself with no variables at all.
`CLAWDLINE_NEXT_STANDALONE=1` and `CLAWDLINE_NEXT_OWN_SESSIONS=1` are still read and still mean
what they meant, so the command line above keeps working with them in — they just change nothing.

State goes to `$XDG_CONFIG_HOME/clawdline-next`, else `~/.config/clawdline-next` — verified for
both. The directory is created `0700` and `local-token`, `orchestrator-token` and the SQLite file
`0600`. Events survived three daemon restarts (76 events, 4 broker tasks read back by `doctor`).

`clawdline open --print` prints a sign-in URL on a headless box instead of trying to open a
browser. To reach the console from a workstation, forward the port over your existing remote-access
path and open that URL against the forwarded port; the gate accepts it.

The daemon has no settings switch for launch-at-login yet. The tracked bootstrap above installs the
`systemd --user` unit and enables linger once; the `launch_at_login` capability names that exact
path instead of claiming the daemon can toggle it itself.

## 2. What works, measured

| Item | What was run | Result |
|---|---|---|
| `doctor`, `serve` | both, three times | state dir and store correct, no panic |
| `GET /v1/health` | no token / token | `200`; `authed` flips correctly |
| Auth gate | `/v1/sessions` with no token | `401 unauthorized`, "This needs a paired device." |
| File permissions | `stat -c %a` | dir `700`, both tokens and the db `600` |
| Session list (tmux) | `tmux new-session` then `GET /v1/sessions` | the pane listed as `%0`, `backend: tmux`, right `cwd` |
| Session list (no tmux server) | before any server existed | empty list, `complete: true` — authoritatively empty |
| Identity, label, menu | resumed codex session | `sessionId` bound, thread title read, chooser parsed into `menu` with three options |
| Send | `POST /v1/sessions/%0/send` | `{"ok":true,"action":"typed"}`; the assistant answered, Chinese not garbled |
| Key | `POST /v1/sessions/%0/key {"key":"2"}` | the option pressed is the option taken |
| Screen | `capture-pane` through the daemon | full pane, box drawing and CJK intact |
| Focus | `POST /v1/sessions/%0/focus` | `{"ok":true}` — promises "selected", not "raised", which is right here |
| Dispatch, whole round trip | `POST /v1/orchestrator/tasks` | task dir + 23 KB `CHILD.md`, detached tmux session `clawdline-task-<id>` at 80x24, briefing typed, child ran, `result.json` collected, **`state: success`**, notice typed into the root |
| Console | served by the daemon, opened in a real browser | document `200` (53 KB), all six assets `200`, session list drew, SSE opened `text/event-stream` and pushed `event: sessions` at once |
| `clawdline assistants` | read | codex `availability: ok`, quota window read off this machine |
| Web Push key | `/v1/push/key` | a VAPID key generated on Linux |

## 3. Refused by name, not crashed

All of these were called on the box and answered without a stack trace:

- **`/v1/diagnostics` carries the `capabilities` block** (`docs/cross-platform.md` §5 rule 4, first
  half). On this machine it said, in its own words: `clipboard` unavailable — "there is no clipboard
  on linux that Clawdline can lend a picture to; a send hands the assistant the picture's path
  instead"; `global_hotkey` unavailable — "this session has no desktop (neither WAYLAND_DISPLAY nor
  DISPLAY is set)"; `notch` unavailable — "there is no notch on linux; what the island shows … is on
  the console's session list"; `launch_at_login` unavailable — "nothing on linux installs a login
  item yet".
- **`open_child`** is the model example: with no tmux server it said "linux has no iTerm2, and no
  tmux server is running, and the auto terminal setting opens a child only into a running one (start
  one, or set the terminal to tmux)", and turned `available` — "a new detached tmux session for each
  child" — the moment a server existed.
- **iTerm2** simply is not in the backend list, and `clawdline type <tty> …` answers
  `no backend for iterm` rather than pretending.
- **HEIC** → `415 unsupported_image`, "One file was not a supported decodable raster image."
- **cloudflared** → `/v1/tunnel` reports `installed: false`, `state: off`.
- **Voice** → the 16 kHz rule is stated in full rather than quietly resampling.
- **Sessions with no record** carry `evidence: "none"` and a note, never a zero pretending to be an
  answer.

## 4. What stops a stock build, with the evidence

### 4.1 tmux 3.4 escapes the `0x01` field separator, so every pane is dropped — silently

`internal/adapters/terminal/tmux.go` builds its `list-panes -F` format with `paneSeparator =
"\x01"` and splits the answer on the same byte. On tmux 3.4 (the Ubuntu 24.04 package) tmux escapes
the control character and returns the four printable characters `\001`:

```
$ tmux -u list-panes -a -F "#{pane_id}<0x01>#{pane_tty}<0x01>#{pane_current_command}" | od -An -c
   %   0   \   0   0   1   /   d   e   v   /   p   t   s   /   1   \   0   0   1   c   o   d   e   x  \n
```

Each line then has one field, `len(parts) < 6` drops it, and `Tmux.Inventory` returns **zero panes
with `Complete: true`** — an authoritative "there are no panes", which is the one answer
`docs/design-decisions.md` D05 ③ says it must never give. Downstream: no pane ever reaches
`backend: tmux`, a session's `cwd` is never learned, the broker's `Screen` lookup cannot find a
child's terminal id, and dispatch dies. The comment above the code records that `-u` fixed this on
tmux 3.6a; on 3.4 `-u` does not help.

The probe that found this measured a fix — send the four printable characters `\001` as the
separator instead of the byte — and with it the pane appeared as `%0` / `backend: tmux` with the
right `cwd`, and everything in §2 followed.

**What landed is the other half of that.** Changing what is *sent* would have swapped which tmux is
broken: this Mac's 3.6a returns the byte, and a reader expecting the escape would have dropped every
pane here instead. `splitPaneFields` cuts on whichever spelling arrived, so both versions read, and
nothing changes on the wire for the version that already worked.

The second change matters more than the first. A line that carries neither spelling is now
**counted**, and a listing that dropped any line comes back `Complete: false` with a note. The
silence was the defect: a one-version escaping rule turned into a confident "there are no panes",
and every future mangling of that line — a tmux that quotes differently, a pane title holding the
separator — would have done the same thing again. Read as evidence, an unreadable line is not an
absent pane. Both halves are covered by `TestAnEscapedSeparatorIsStillAListing`, which stands a
fake tmux in front of each spelling, and both go red without the fix.

### 4.2 A codex child is never briefed: its composer has no frame

`orchestrator/composer.go` calls a caret a composer only when a box rule is drawn within three lines
under it, and a caret without one a chooser. That rule was written against Claude Code and is tested
only against Claude Code screens. Codex 0.154's composer is a bare `› Ask Codex to do anything` with
a blank line and a status bar under it, so `Choosing()` is true forever and the dispatch settles:

> The briefing was never typed into the child's tab (**the child is showing a dialog; the briefing
> would have answered it**), and the secret it carries is not kept … Dispatch it again.

Behind that, a second one: once the briefing *is* typed, codex does not act on the Enter that
`terminal/submit.go` sends after the paste. The nudge in `submit()` that exists for exactly this
only fires when `stillHolds` sees a **framed** composer, so for codex it never fires and the
briefing sits in the composer until the task times out. Reproduced twice; a single Enter sent by
hand submitted it immediately and the child then ran to `success`.

Neither defect is about Linux. They are what a Linux box hits first because codex may be the only
assistant signed in on it.

### 4.3 Schedules have no route in standalone mode

The scheduler clock runs — `/v1/diagnostics` reports `scheduler.considered/due/fired` and a
60-second tick — but `GET`/`POST /v1/schedules` answers `501 not_implemented`, "this daemon does not
own that route yet". On macOS the proxy hides this; on Linux there is nothing behind it, so a
headless machine can run schedules it has no way to create. The console draws an empty SCHEDULES
section with a `+` that leads nowhere.

### 4.4 Smaller things worth knowing

- **A session outside tmux is labelled `backend: "iterm"` on Linux.** `process/ps_unix.go` hardcodes
  `BackendITerm` for any assistant on a tty, and the merge only upgrades it when a tmux pane matches
  the same tty. On a machine with no iTerm2 that row's id is a tty no action can reach.
- **`/v1/health` has no `capabilities` block and no scheduler numbers.** Both live at
  `/v1/diagnostics`, behind the local token. `docs/cross-platform.md` §5 rule 4 asks for health;
  what exists is diagnostics.
- **The settings page does not render the degradations.** It still offers the ⌥Space hotkey and an
  iTerm2 bundle-id list on a machine that has neither. The data is in `/v1/diagnostics`; the page
  does not read it. Rule 4's second half is unbuilt.
- **`/v1/work` refuses the orchestrator token** (`401`) although the agent guide lists `/v1/work/`
  under it. `/v1/board` accepts it.
- **A codex child cannot reach `127.0.0.1`** from inside its own `workspace-write` sandbox: its
  `/accepted` and `/inflight` curls failed with exit 7. The file fallbacks the briefing documents
  (`accepted.json`, `progress.json`) and the broker's own collection of `result.json` carried the
  task through, which is the design working as written.
- **A codex child's sandbox needs the task root to be writable by the child's own user.** A task
  directory whose parent belongs to somebody else fails with
  `bwrap: Can't mkdir <task root>/.git: Permission denied` and the child cannot even read
  `CHILD.md`.

## 5. Not run

Desktop Linux (GNOME/Wayland) — this machine is headless, so the hotkey portal, Secret Service,
`org.freedesktop.Notifications`, the tray and a real browser launch are all untested. Whisper
end-to-end (no binary, no model on the box). Reboot survival. Ten-minute SSE soak. Layout at
760px. `cloudflared` with the binary present.
