# Linux runtime boundary (W4-1)

W4-1 supplies the local Linux effects needed to manage Claude Code and Codex in tmux. It is a
runtime boundary, not a daemon deployment: `ClawdlineLinux run` validates protected configuration,
composes the adapters and prints a receipt. It does not listen, install a service, reconcile after
restart, connect to Cloud, upgrade or roll back a package.

W4-2 composes those effects into a separate candidate daemon/release boundary. Its loopback listener
has one serialized ledger owner: the accepted request and recoverable sealed bytes are fsynced before
an effect, returned lifecycle stages and response bytes are fsynced before response, and an unresolved
post-effect boundary requires explicit matching recovery rather than blind replay. Startup
classification runs once per daemon epoch; periodic terminal observation is a separate operation.
Corrupt, future or semantically invalid state leaves a durable recovery obligation that ordinary
ticks and restarts cannot reinterpret as a fresh empty installation.

## Service identity and directory layout

The process refuses uid 0 and refuses a configured uid or gid that differs from its effective
identity. Composition sets umask `0077` and requires the state directory and each child below to be
owned by that identity, mode 0700, canonical and free of symlink components:

```text
<stateDirectory>/
  home/       durable provider HOME (provider-visible only through its sandbox rule)
  runtime/    daemon-only explicit tmux socket and runtime state
  secrets/    daemon-only protected local secret files
  tmp/        provider temporary files (provider-visible only through its sandbox rule)
<stateDirectory>-projects/  default registered project root, outside control state
```

An optional `runtime` object in the version-1 protected configuration pins `uid`, `gid`, one to 32
canonical `projectRoots`, the absolute tmux executable, and absolute Claude/Codex executables. The
configuration itself retains W3's descriptor-bound owner/mode/type/size validation. Provider
credentials are not configuration fields.

Every configured project/additional root is rejected when it equals, contains, or is contained by
the state, HOME, runtime, secrets or temporary control paths. Configured tmux/provider/launcher
executables are descriptor-identified regular executable files, root- or service-owned and not
group/world writable; device/inode/owner/mode are revalidated immediately before each effect.

## Admission and containment

Mac `StartPoints.start` and Linux create both call `SessionLaunchPolicy` before a terminal effect.
It returns a structured executable/argv plan plus the existing Mac shell facade spelling. Linux
also admits the project and optional additional directory through `ProjectRootPolicy`: each must
be an exact protected-config root, owned by the service uid/gid, with no traversal, noncanonical
component or symlink.

Contained file operations open `/` and walk every root and parent component with
`O_DIRECTORY | O_NOFOLLOW`. Reads accept only bounded regular files. Writes create a 0600 sibling,
write and `fsync` it, `renameat` it through the already-open parent, then `fsync` the directory.
Removes reject symlinks. Protected secret account names use a closed filename alphabet and all
secret operations go through this same adapter.

Provider environment is constructed from exactly `HOME`, `PATH`, `LANG`, `LC_ALL`, `TERM` and
`TMPDIR`; it is never copied from the daemon. tmux starts the provider through `/usr/bin/env -i`.
Session identity variables, API keys and the daemon's local secret therefore cannot cross through
ambient environment. No secret is placed in argv, errors, health or composition receipts.

Environment isolation is not the authority boundary. Before the provider executable runs, the
`ClawdlineLinux` launcher sets `no_new_privs`, installs a Landlock ABI 3+ filesystem allowlist, and
installs a seccomp filter. The provider and all descendants may read/execute the system runtime and
may write only the admitted project roots, HOME and tmp. Daemon state, secrets and runtime are not
Landlock rules. AF_UNIX socket creation is denied, so a provider receiving no socket descriptor
cannot connect to the tmux/daemon control sockets; `ptrace`, `process_vm_*` and `pidfd_getfd` are
also denied. Composition returns `capability_unavailable` before a terminal effect if the kernel or
any executable cannot prove this boundary. The AF_UNIX denial is an intentional current Linux
limitation and may make provider extensions that require local Unix sockets unusable.

## Terminal and process lifecycle

The same Application-owned `TerminalCommandScheduler` implementation used by the existing Mac
facade admits and serially executes every Linux create, send, observe, interrupt, resize, close and
enumerate operation. Inventory and pane lookup preflights are inside that admitted closure;
maintenance closes new admission while already-admitted and nested work drains. Production bounds
are eight total operations, two per channel,
65,536 input bytes and 64 inventory rows. Each command has a bounded deadline and output ceiling;
duplicate active command ids and capacity exhaustion are typed failures.

stdout and stderr are drained concurrently from process start and charged to one aggregate ceiling.
stdin is nonblocking under the same monotonic deadline. Timeout/output failure closes descriptors,
then uses bounded TERM/KILL/reap windows; a descendant retaining a pipe cannot turn cleanup into an
unbounded `readDataToEndOfFile`.

All tmux calls use one explicit socket beneath `runtime/`. Create returns the pane, PTY, attach
command and process identity. `/proc/<pid>/stat` supplies pid, exact start-token and process group;
`/proc/<pid>/status` supplies effective uid/gid and the complete supplementary-group set. Create
accepts only an exact pid/token/group/tty/assistant match, and the process adapter revalidates that
complete identity immediately before TERM or KILL. This keeps same-tick replacement, changed GID or
changed process group from inheriting authority. Resize is bounded to 20…500 columns by 5…300 rows and must be
read back from tmux. Menu answers accept only digits 1…9, Tab or back-Tab before key delivery.

Lifecycle progress is ordered rather than collapsed to one Boolean:

1. `accepted` — the shared scheduler admitted the operation.
2. `executed` — the addressed adapter call completed.
3. `delivered` — bytes or state reached the PTY boundary.
4. `observed` — fresh evidence confirmed screen/state, dimensions, inventory or absence.

Send and interrupt receipts stop at `delivered`; they do not claim the provider observed input.
Create, observe, resize, enumerate and close require fresh observation before returning that stage.
Failures after an effect carry `no_effect`, `compensated`, `partial` or `unknown` certainty, the
last confirmed effect and a reconciliation flag. Create polls readiness under a deadline and removes
only an exact matching pane on failure; paste-before-submit is returned as `partial/text_pasted`, so
a caller cannot safely replay it as though nothing happened.

Protected secret operations share one process-wide per-root/per-account coordinator across store
instances. Plain `data`, `set` and `remove` participate alongside closed `loadOrCreate` and `rotate`;
the latter use private unlocked read/write primitives inside the one critical region.

## Capability and release boundary

Diagnostic health reports compiled adapters separately from configured and usable capability. With
no protected runtime configuration, `supportedCapabilities` is empty and health is `ready=false` /
`w4_runtime_not_configured`. A successful composition reports validated tmux/process/files/secrets
as usable but remains `ready=false` / `w4_provider_authentication_not_proven`; both provider rows
have empty lifecycle support and typed `capability_unavailable` owned by the W4-2 real-provider
authentication gate. iTerm remains Mac-only. W4-2 also owns systemd, listener, restart
reconciliation, durable-record migrations and package upgrade/rollback; W4-3 owns pairing and Cloud
lifecycle.

The W4-2 package candidate snapshots caller key/provenance/signature/archive paths into a root-owned
0700 directory through no-follow descriptors and uses only those immutable bytes for verification
and exact extraction. Signed schema/protocol fields must equal the exact target binary's
`release-contract` output before daemon effect. A descriptor-bound helper creates or verifies the
service secret without following service-controlled ancestors/final links; release content,
directories, selector links, transition journal and receipt are synced. Stale lock recovery verifies
PID/start identity and journal phase, then restores a complete old pair or completes a committed new
pair. Only foreground-supervised `clawdline-tmux.service` owns `/run/clawdline`; the daemon unit does
not co-own/remove that socket directory. Exact parsing and private-root failure injection are local
evidence, while real PID-1 socket/pane continuity, keeper crash restart and reboot enablement remain a
typed Ubuntu VM gate.

The pinned Ubuntu 24.04 amd64 job runs as a non-root service user, builds the real SwiftPM graph,
executes the Linux XCTest target with real tmux and containment probes, and records installed tool
versions with the job receipt; apt packages are not yet version-pinned. The macOS focused run covers
the platform-neutral contracts and compile compatibility; its
`#if os(Linux)` PTY case is intentionally Ubuntu-only. Neither receipt is deployment evidence.

The pidfd-less gap between final procfs revalidation and `kill(-pgid, …)` remains a Linux-only
residual race. Landlock/seccomp support is currently limited to the pinned Ubuntu amd64 target, and
no real Claude/Codex authentication receipt exists yet; those limits stay non-capabilities rather
than being inferred from fixture success.
