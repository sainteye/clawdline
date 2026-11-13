# The first installed Claude route

The daemon now supplies an installed native provider to the existing lazy Bot
Host. This replaces the previously absent production factory; it does not
replace the Host's authority, Store, source identity or execution-bound tools.
Metadata and feature startup do not load the installation or start an adapter.
The feature remains explicitly off by default.

## Configure existing files

The first qualified artifact set is macOS on arm64, Node 24.1.0, Claude Code
2.1.323 and `@agentclientprotocol/claude-agent-acp` 0.106.0. Other artifacts are
refused rather than substituted. Setup installs nothing and changes no CLI.
The native executables and adapter entry/agent files have pinned SHA-256
identities. Setup seals the complete existing dependency directory; later
changes, additions or links escaping it require new explicit setup.

With those versions already installed in a dedicated dependency directory:

```sh
clawdline project-assistants setup-claude \
  --node /absolute/path/to/node \
  --adapter /absolute/path/to/node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js \
  --cli /absolute/path/to/claude
```

This CLI entry writes only the selected Clawdline state directory's
`project-assistants/claude-installation.json`. Its output confirms local file
and OS controls, never authentication or model access. `CLAWDLINE_NEXT_DIR`
selects an isolated state directory for development. It does not turn Bots on,
create an ACP session, send a prompt, log in or log out. The native program
uses its normal existing login when a separately authorized first send opens
an execution. No credential, account identity, transcript or inherited API key
is copied into the manifest or native environment.

## Execution boundary

Each capability check and native open revalidates the sealed artifacts and
runs a fixed local Node control. The control imports no adapter and makes no
network or credential request. It rejects an unprotected process and proves
that writes outside its owned directory, arbitrary executable launches, and
native `.claude` memory reads/writes are refused. The actual launch uses that
same generated OS profile. Runtime directories are separate for each Bot,
Project and execution; they are removed only after native cleanup succeeds.
An unresolved cleanup retains ownership and directory contents.

The profile permits the native login service and narrow credential paths,
without granting general reads of the person's home. It permits only the
pinned Node/native CLI plus the native credential helper executable. Only the
execution directory is writable, and native `.claude` memory paths remain
blocked there as well. This is a filesystem/process boundary, not a claim
that the native program or a human login has been independently audited.

Session settings additionally disable native tools, inherited settings and
hooks, automatic memory, dream tasks, prompt suggestions, progress summaries
and native session persistence. A fresh process reconstructs context from the
app's durable filtered history/memory; it does not resume a personal native
transcript. Native permission payloads remain denied by the existing
classifier. The HTTP MCP listener still receives only Host-bound functions.
Source-owned dispatch claims remain a separate required integration step.

The lifecycle and context separation follow the retained Codync design at
`234219cef2d3f991f0f7225ba89e3c99cdd34177`, with Clawdline's narrower native
write and memory policy. The installed route does not import upstream
automatic approvals or a terminal identity for a Bot.

## Acceptance boundary

A successful setup and OS control are local configuration evidence. They do
not establish authentication, a model reply, a Broker dispatch, a usable
published version or complete Board acceptance. Those real flows and any
paid requests require their own evidence and authorization. No native model
or service is called by the local tests. The provider refuses missing setup,
changed artifacts, unsupported platforms and failed kernel controls.

Remove the setup command, installed provider and production factory wiring to
remove this route. Keep persisted Bot data, ordinary Sessions and the user's
existing CLI/login. Never remove another execution's runtime directory as
part of cleanup.
