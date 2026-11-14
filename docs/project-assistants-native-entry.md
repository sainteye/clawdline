# The first installed Claude route

The daemon supplies an installed native provider to the existing lazy Bot
Host. It does not replace the Host's authority, Store, source identity or
execution-bound tools. Metadata and feature startup do not load the
installation or start an adapter. The feature remains explicitly off by
default.

## What a person needs

Since 2026-11-14 setup follows CodyNC: a Bot uses the programs the person
already has. Supported: macOS, Claude Code 2.1.323 or newer, and Node 22 or
newer with its npm. Claude Code updates itself, so only that floor is
checked; a newer version is accepted without new setup. The person signs in
to Claude Code as they normally do. Nothing asks them to place files.

The ACP adapter is the one part Clawdline supplies:
`@agentclientprotocol/claude-agent-acp` 0.106.0. Its `package.json` and
`package-lock.json` ship inside the binary
(`internal/adapters/projectassistantacp/assets/claude-agent-acp`). The
adapter's code is not committed to this repository: it depends on Anthropic's
Agent SDK, whose terms are not this repository's licence, so npm fetches it
as a pinned dependency instead.

## First use

The first send to a Claude Bot, or `clawdline project-assistants
setup-claude`, sets up:

1. Find Node, its npm and Claude Code. The person's `PATH` comes first, then
   the usual per-user places (`~/.local/bin`, `~/.claude/local`, Homebrew,
   `/usr/local/bin`, Volta, and the newest nvm, mise, asdf or fnm version),
   because launchd starts the daemon with a minimal `PATH`. Each candidate
   answers `--version` within `capacity.MaximumBotToolProbeMilliseconds` and
   `MaximumBotToolProbeBytes`. Missing programs are `cli_not_installed`;
   programs below the floor are `provider_version_unsupported`
   (`native_node_version`, `native_cli_version`). Clawdline never installs or
   updates Claude Code or Node.
2. Install the adapter with that npm: `npm ci --ignore-scripts --omit=dev
   --omit=optional` from the bundled lockfile, into a build directory under
   `<state>/project-assistants/adapters/`, with a private npm cache, within
   `capacity.MaximumBotAdapterInstallSeconds`. npm checks each package
   against the lockfile's integrity. Optional packages are left out: one
   would carry a second Claude binary, and the person's own CLI is used.
3. Seal it. One SHA-256 over every file's relative path and content (npm's
   regenerated `.package-lock.json` excluded) must equal
   `claudeAdapterTreeSHA`. The entry and agent files also keep their own
   digests. A build that differs is removed. Two clean installs with
   different caches produced the same digest on 2026-11-14.
4. Record `project-assistants/claude-installation.json` (format 2): the
   adapter path and its per-file seal. The person's tools are not recorded;
   they are found again for every execution, so an updated launcher is
   followed. Format 1 manifests, which pinned Node and CLI digests, are
   replaced by a fresh setup.

Every capability check and open verifies the seal again. A missing, changed
or added adapter file causes one reinstall; a missing or too-old program is
reported and not worked around. The only network use is npm fetching the
lockfile's packages during an install. Setup does not start the adapter,
inspect authentication, turn Bots on or send a prompt.

The CLI prints a fixed receipt with the adapter version, the found and
minimum CLI versions, `authentication_verified: false` and
`model_requests: 0`. It echoes no paths or credentials.

## Execution boundary

The native CLI runs as the person, in the Bot's Project, with its own tools.
Which actions it asks about is Claude Code's own choice: with no configured
default the adapter starts in `auto`, where a classifier approves most
actions, or `acceptEdits` for a model without it. The person decided on
2026-11-14 to keep that rather than force a card for every change. A card
that does appear may be allowed once; standing allow or reject rules are
refused. Bypass mode stays off. Its configuration, history and memory live in a
feature-owned directory for each Bot, Project, conversation and revision;
personal Claude settings, hooks and memory are not read. The environment
carries `HOME`, `USER` (the CLI names its keychain account from it), a fixed
`PATH`, a new scratch `TMPDIR` and the adapter and telemetry switches. No API
key, token, cloud credential or `NODE_OPTIONS` is inherited. The HTTP MCP
listener receives only Host-bound functions.

The lifecycle and context separation follow the retained Codync design at
`234219cef2d3f991f0f7225ba89e3c99cdd34177`. The installed route does not
import upstream automatic approvals or a terminal identity for a Bot.

## Acceptance boundary

A successful setup is local configuration evidence. It does not establish
authentication, a model reply, a Broker dispatch, a usable published version
or complete Board acceptance. Those real flows and any paid requests require
their own evidence and authorization. Tests never search the developer's
real programs: every hermetic test binary replaces `SearchDirectories`
before it runs, and no local test calls a model, a service or npm.

Remove the setup command, installed provider and production factory wiring to
remove this route. Keep persisted Bot data, ordinary Sessions and the user's
existing CLI/login. Never remove another execution's runtime directory as
part of cleanup.

## Codex

Since 2026-11-14 a Codex Bot runs the same way (`installation_codex.go`):
the person's codex 0.166.0 or newer, found like Claude Code, and the supplied
`@agentclientprotocol/codex-acp` 2.6.0, installed from its bundled lockfile
and sealed by `codexAdapterTreeSHA`. codex-acp bundles its code in
`dist/index.js` and starts codex through `CODEX_PATH`; its `@openai/codex`
dependency is installed without the optional platform binaries.

Codex keeps its ChatGPT login in `$CODEX_HOME/auth.json`, so the Bot uses the
person's own Codex home instead of a feature-owned one. Native memory is
admitted only while `codex features list` reports `memories` false; a person
who turns it on gets `memory_isolation_unverified` (`codex_memories_enabled`).
Codex native resume is not qualified: each execution starts a new native
session from the Host's filtered history and memory.
