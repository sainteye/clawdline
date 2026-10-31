# Native slash-command coverage (Claude Code 2.1.316)

Measured 2026-10-31 against Claude Code **2.1.316** on macOS, tmux 3.6a, Node 24.1.0, from
repository base `2faf68e9a`. This page records what the real CLI does for each native command and
what the console's send path does with that. It is evidence for the routing fix, not the fix.
Codex was not exercised in this pass.

## How the CLI was isolated

- A private tmux server on a task-owned absolute `-S` socket, `TMUX` unset, killed by its own
  socket when done.
- `claude --bare --strict-mcp-config --setting-sources ''` with an empty, task-owned
  `CLAUDE_CONFIG_DIR` and an empty working directory. `--bare` never reads OAuth or the keychain
  and accepts only `ANTHROPIC_API_KEY`, which was unset, so **no model request could succeed**:
  the CLI reported `Auth token: none`. No hooks, plugins, MCP servers or project settings loaded.
- The onboarding theme screen, the security note and the folder-trust prompt were accepted for the
  empty directory only.
- Each command: clear the input line, type the command, Enter, capture after 2.5 s, Esc, capture;
  a second Esc when the first did not return to the prompt. Menus were cancelled without changing
  a selection.
- Raw pane captures and the session JSONL remain in the task's temporary artifacts, not in Git.

## Counts

| | Count |
|---|---|
| Entries in the CLI's own `/` palette (bare mode, empty config) | **83** |
| Commands executed | **20** (19 palette names plus the `/cost` alias) |
| Model requests attempted | **1** (`/doctor`; one `api-request` record in the session JSONL) |
| Model requests that succeeded | 0 |

The one attempted request came from `/doctor`, which the CLI expanded into a prompt. The CLI
recorded its reply as a synthetic assistant message (`model: "<synthetic>"`,
`isApiErrorMessage: true`, `error: "authentication_failed"`), displayed as
`Not logged in · Please run /login`. Every recorded usage field was zero, and `/usage` had shown
`Total cost: $0.0000` earlier in the session. The request carried no credentials, so it could not
be billed to an account. The record does not show whether the CLI stopped before sending the
request or the server refused it. No retry was made, and no other model-triggering command was
run after that.

`/cost` is **not** in the palette but is accepted; it opens the same screen as `/usage`.

## The five stages, kept apart

For each command the table separates:

- **accepted** — the daemon took the send (`POST /v1/sessions/<id>/send` answered 2xx);
- **executed** — the CLI ran the command (observed on the pane);
- **delivered** — the bytes reached the foreground CLI's input line and Enter was pressed;
- **observed** — the daemon's transcript reader (`ReadClaude`) shows a turn for it;
- **acknowledged** — the console's pending card settles because a turn matches its words.

In this pass, accepted and delivered were done by `tmux send-keys` rather than the daemon, so
the first column is a stand-in. Executed came from pane captures. Observed came from running the
repository's own `transcript.ReadClaude` over the JSONL the CLI wrote, in a temporary test that
was not committed. Acknowledged was derived from `PendingSends.reconcile`, which settles a card
only when a user turn's words equal the card's words (`turnWords`) inside its window.

## Executed commands

| Typed | Native screen | Esc returns to prompt | JSONL row written | `ReadClaude` user turn | Card settles by words |
|---|---|---|---|---|---|
| `/cost` | Settings dialog, Usage tab | yes, 1 Esc | `system/local_command`, **`<command-name>/usage`** | `/usage` | **no**: `/cost` ≠ `/usage` |
| `/usage` | Settings dialog, Usage tab | yes, 1 | system | `/usage` | yes |
| `/status` | Settings dialog, Status tab | yes, 1 | system | `/status` | yes |
| `/context` | none; prints a grid inline | already at prompt | system + meta user | `/context` | yes |
| `/model` | Select model picker | yes, 1 (`Kept model as …`) | system | `/model` | yes |
| `/effort` | Effort slider | yes, 1 (`Cancelled`) | **user** row with command tags | `/effort` | yes |
| `/config` | Settings dialog, Config tab with search box | **2 Esc**: first clears the search, second closes | system | `/config` | yes |
| `/help` | Help dialog | yes, 1 | system | `/help` | yes |
| `/theme` | Theme picker | yes, 1 | system | `/theme` | yes |
| `/permissions` | Permissions dialog with tabs and search | yes, 1 | system | `/permissions` | yes |
| `/hooks` | Hooks dialog (read-only) | yes, 1 | system | `/hooks` | yes |
| `/mcp` | none; prints inline | already at prompt | user row | `/mcp` | yes |
| `/memory` | Memory dialog | yes, 1 | system | `/memory` | yes |
| `/doctor` | none; **prompt-type command, sends a model request** | already at prompt | user + meta prompt + API request | `/doctor` | yes |
| `/release-notes` | Version list | yes, 1; **no dismissal line** | **none** | **none** | **no** |
| `/skills` | Skills dialog | yes, 1 | system | `/skills` | yes |
| `/tasks` | Background tasks dialog | yes, 1; **no dismissal line** | **none** | **none** | **no** |
| `/ide` | Select IDE | yes, 1 | system | `/ide` | yes |
| `/output-style` | none; prints inline | already at prompt | user + system | `/output-style` | yes |
| `/resume` | Resume session picker with search | yes, 1 (`Resume cancelled`) | system | `/resume` | yes |

What the table shows:

1. **The row the CLI writes is not always the words typed.** An alias is written under its
   canonical name (`/cost` → `/usage`). A card that waits for its own words then waits until
   `PENDING_LIFETIME_MS` (10 minutes) expires on a later reconcile. This reproduces the reported
   symptom: `/cost` is accepted, the card stays "syncing", and nothing in the transcript replaces
   it.
2. **Some commands write no row at all** (`/release-notes`, `/tasks`). Their cards also wait the
   full lifetime.
3. **The row's type varies by command and by session state.** Most commands write a
   `type: "system", subtype: "local_command"` row. `/effort`, `/mcp` and `/output-style` wrote
   `type: "user"` rows behind a `local-command-caveat` meta row. `ReadClaude` turns both into a user
   turn, so the type is not the problem. Stability of names and presence of a row are.
4. **A screen's way out is not always one Esc.** `/config` needs two, because the first clears
   its search field. Screens with a search box can need an extra Esc.
5. **`/doctor` is not read-only in 2.1.316.** It expands into a prompt and calls the model. Treat
   prompt-type commands as model commands even when their names sound diagnostic.

## Reference catalog classification (83 palette entries)

The palette shows built-in commands and bundled skills. Bare mode and an empty config hide user,
project and plugin commands, which the console must also handle without knowing their names.

| Class | Commands | Status here |
|---|---|---|
| Local screen or inline output, read-only on cancel | `/config` `/context` `/effort` `/help` `/hooks` `/ide` `/memory` `/model` `/permissions` `/release-notes` `/resume` `/skills` `/status` `/tasks` `/theme` `/usage` `/output-style` (no argument) `/mcp` `/cost` | executed |
| Local screen, probably read-only, not run in this pass | `/list-agents` `/plugin` `/workflows` `/sandbox` `/diff` `/plan` `/powerup` `/mobile` `/keybindings` (opens an editor) | untested: time box |
| Local state change | `/clear` `/rename` `/cd` `/add-dir` `/color` `/fast` `/focus` `/tui` `/autocompact` `/rewind` `/branch` `/fork` `/background` `/reload-plugins` `/reload-skills` `/export` `/copy` `/terminal-setup` `/statusline` `/exit` | untested: mutates the session or files |
| Calls the model (prompt-type or skill) | `/compact` `/init` `/doctor` (run once; 1 request attempted, authentication failed) `/code-review` `/security-review` `/simplify` `/verify` `/run` `/run-skill-generator` `/batch` `/btw` `/subtask` `/recap` `/insights` `/team-onboarding` `/fewer-permission-prompts` `/debug` `/goal` `/loop` `/update-config` `/claude-api` `/dataviz` `/artifact-diagramming` `/deep-research` `/plugin-authoring` `/workflow-authoring` | untested by rule (except `/doctor`) |
| Account, network or external state | `/login` `/logout` `/install-github-app` `/design` `/design-login` `/design-sync` `/feedback` `/bug` `/stickers` `/radio` | untested by rule (except `/doctor`) |

## Recommendations for generic routing

Do not add command names. Each of the failures above has a generic cause:

- **Settle a slash-command card on delivery, not on a matching turn.** For a Claude or Codex
  session on `tmux` or `iterm`, a send whose text matches the existing slash shape
  (`/^\/[^\s/]+(?:\s|$)/`, already used by `nativeTerminalCommand`) and is accepted has done all
  the console can know. Mark the card as handed to the terminal. Then remove it, or show it
  without a spinner, instead of keeping it `accepted` until a turn appears. A later slash turn in
  the transcript adds confirmation, but the card must not depend on it. This covers aliases,
  commands with no row, and unknown future names. The existing Codex `/model` exception
  (`settleModelPicker`) then becomes unnecessary.
- **Hand off to the Terminal panel at acceptance, independent of the transcript.** The handoff
  in `Composer.tsx` already runs on a successful send. Make it survive a page reload. A
  reloaded pending slash card is shown as handed to the terminal, with a way to open the Terminal,
  not as syncing.
- **Do not wait for a parsed menu.** A screen may never be parsed (`/release-notes`, a plugin
  command, a future dialog) or may need more than one Esc. The Terminal panel showing the real pane
  is the general answer; menu parsing is an enhancement.
- **Keep `/resume` a local modal action.** It remains local to the Claude CLI, opens a picker and
  writes a row only when cancelled. The console's own resume flow must keep treating it as a
  modal in the foreground CLI, not as a conversation turn.
- **Treat words that only look like a path as text.** The shape already excludes `/a/b` because of
  the second slash. Keep that exclusion.

## Regression tests to add

Console (`node --test`, behavior rather than source shape):

1. A Claude/tmux slash card that is accepted and never sees a matching turn reaches its terminal
   handoff state promptly, not after `PENDING_LIFETIME_MS`. Cover `/cost` with a transcript
   that holds only `/usage`, and `/release-notes` with no turn at all.
2. Delayed or uncertain send: the POST resolves late or is `unknown`. The card is not marked
   handed off until acceptance. An `unknown` slash card is still looked at, not resent.
3. Reloaded page: a persisted `accepted` slash card restored by `cardsAreFor` is shown as handed
   to the terminal and offers the Terminal, not an indefinite spinner.
4. Hidden or unparseable menu: no menu question arrives after the send. The Terminal handoff still
   happens, and nothing waits on menu-follow.
5. Ordinary prompts still settle only by matching turn; a prompt that merely mentions `/cost`
   mid-sentence is not a slash card.
6. `/resume` stays a local modal: the handoff opens the Terminal and does not create a
   conversation turn of its own.

Daemon (`internal/adapters/transcript`): a fixture with an alias row
(`<command-name>/usage</command-name>` written for typed `/cost`) and a session in which a command
wrote no row. These pin the reader's current output, so a future CLI change to row shapes appears
as a test failure rather than a stuck card.
