# Native slash commands

The Session composer and quick input bar combine the provider's built-in command
reference with file-backed custom commands and the existing skill catalog. `/`
opens a searchable, scrollable list. Codex's `$` restricts completion to skills.
Choosing a row fills the exact invocation and leaves argument entry and sending
to the person. Sending does not use an allowlist: an unlisted new provider command
can still be entered and sent normally.

## Sources and freshness

There is no common standalone built-in catalog installed beside both executables.
Claude Code embeds built-ins in its native executable. Its noninteractive SDK
initialization omits terminal-only commands, so that subset is not used as a claim
of terminal parity. Codex defines built-ins in its TUI source, and adds service-tier
commands from model metadata. Runtime feature flags, platform, account and session
state can change the terminal's actual list.

`web/console/src/legacy/native-commands.json` is a reviewed, bilingual snapshot of
[Claude Code's command reference](https://code.claude.com/docs/en/commands) and
[Codex's versioned command registry](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/tui/src/slash_command.rs).
Its source URLs, review date and revision travel with the file. It includes the
whole non-retired Claude reference, Codex's release built-ins and their aliases,
and the standard `fast` service-tier command. Internal memory debugging commands
and debug-build-only commands are excluded. This is a reference catalog, not an
assertion that every listed command is enabled in the current terminal. The menu
says availability depends on the CLI version, account and enabled features.

To review a refresh, save the two official source files, update the JSON entries
and English/Traditional Chinese descriptions, then run:

```sh
node tools/check-native-command-catalog.mjs /path/to/commands.md /path/to/slash_command.rs
```

The comparison fails for a missing or extra source command. Provider source format
changes must be reviewed rather than silently accepted. This check is offline;
opening a menu does not launch a CLI, run hooks, contact a provider or make a model
request. Changes to the built-in snapshot require a Clawdline update. Feature-gated
or newly introduced dynamic service-tier names can still be typed directly.

## File-backed commands

The existing metadata-only `/v1/sessions/{id}/skills` response now also carries:

- Claude project and personal `.claude/commands/**/*.md`, and enabled installed
  plugins' `commands/` directories. Subdirectories form colon-separated names.
  `name` frontmatter does not rename legacy command files. Skills take precedence
  over a same-named command file at the same scope; existing scope precedence and
  `skillOverrides` still apply.
- Codex's flat `$CODEX_HOME/prompts/*.md`, defaulting to `~/.codex/prompts/*.md`.
  These use `/prompts:name`; skills retain `$name`, including when names collide.
  Prompt discovery does not require a conversation rollout to exist yet.

Only names, frontmatter descriptions and invocation strings leave the machine.
No command body is executed, expanded or sent to the browser. Existing metadata,
result and directory budgets still apply. Recursive command traversal shares one
directory-entry budget and does not follow symlink cycles. The existing five-minute
cache applies to file metadata; changes outside that window appear on a later read.
Disabled, unloaded, externally added or dynamically supplied components may differ
from a running CLI's registry. The CLI remains authoritative when a command runs.

`/resume` remains a Clawdline action: it opens the provider-scoped history modal
without sending to the CLI. Other commands use the ordinary verified terminal send
lane. Menu follow-up reads now apply to any Claude/Codex slash command, rather than
only Codex `/model`. Existing terminal menu and screen controls handle CLI responses;
this change does not recreate every provider-specific settings dialog in React.

## Verification

Unit tests cover provider prefixes, collisions, aliases, bilingual descriptions,
complete result reachability, recursive metadata discovery, hidden commands,
precedence, long files and symlink cycles. Browser tests select every reference row
for both providers, assert selection sends nothing, then verify native command
arguments and an unknown future command use the ordinary send endpoint. The existing
resume cases assert no terminal send and correct provider-scoped history routing.
Browser fixtures use synthetic metadata; they do not establish real-account feature
availability or invoke paid commands.

## Continue in the same terminal

After a successfully delivered Claude Code or Codex slash command, a physical
Session opens its interactive Terminal panel. This is the existing foreground CLI,
not a newly created project shell. `/resume` still opens the Clawdline history modal.
The panel exposes navigation, Tab, Shift+Tab, Enter, Esc and text entry, with a return
to the conversation. The Session actions menu can reopen the same Terminal panel
without sending another command. Entering text does not submit it: the person presses Enter
separately. There is no command-name allowlist deciding which dialog is supported.

The native-terminal read and input routes require API level 37. The input route uses
existing send permission, per-Session serialization, one idempotency key per press,
and Cloud execution-generation receipts. It requires the visible grid fingerprint
and verifies the kernel foreground PID, process birth and assistant identity before
writing. It supports both a CLI running under a shell and a CLI rooted directly in
its tty. Native Claude installations may use a version as the kernel command;
that case also requires the kernel executable at the current user's standard
`~/.local/share/claude/versions/<version>` path. A version string alone is refused.
An unknown, replaced, exited or unverifiable process is refused; the
channel never opens a fallback shell. iTerm2 still requires its explicit integration
consent. Platforms without foreground-process verification refuse interaction.

Text is a bounded UTF-8 line without control characters; navigation keys are named
separately. A stale screen or uncertain write blocks further input until a manual
reread. No input is retried automatically. This panel is a visible-grid controller,
without terminal scrollback, arbitrary control sequences or mouse reporting.

Verification includes a private real tmux/tty with a synthetic CLI, exact byte
receipts, foreground replacement and exit checks, real HTTP authentication and
idempotency, Cloud execution replacement, and browser key/text/return interactions.
The synthetic CLI makes no model or account request. Provider-specific feature and
account availability still needs the provider's own CLI to decide.
