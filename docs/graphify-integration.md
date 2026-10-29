# Optional Graphify guidance

For maintainers wiring the machine's optional integrations. Graphify guidance is off by default and belongs to the machine, while indexes belong to individual working directories. The [local pilot](experiments/graphify-local-pilot-2026-10-30.md) found that native queries can silently accept a stale index, so a successful Graphify exit is not freshness evidence.

The shared integration owner calls `graphify.LaunchInstructions(ctx, projectRoot)` only when the machine switch is enabled. This adds optional guidance to new Session context. It neither installs Graphify nor edits project instructions, hooks, indexes or user files. Turning the setting off stops injection into future launches. Context already read by an existing Session cannot be withdrawn. Resume does not acquire the current switch's new instructions.

A Session may continue using ordinary search at any time. Before every optional query, from the actual task working directory, it must run:

```sh
clawdline graphify check .
```

The command emits JSON and exits 0 only for `state: "source_matched"`. Exit 1 means the comparison did not establish a match; exit 2 means invalid arguments. The command is independently usable when the launch integration is off, because it only reads local files. It does not call the daemon, Graphify, a shell, or a model API.

A match compares the native `graphify-out/.graphify_root`, schema-1 `graph.json`, and content-MD5 `manifest.json` format exercised with Graphify 0.9.97. It hashes every manifest source, checks for added source/configuration files, rejects roots borrowed through symlinks, and rechecks graph/manifest bytes before returning. A same-mtime content edit is still a change. MD5 is compatibility with the upstream change-detection format, not authentication of the graph or its producer. A matched manifest does not certify graph edges, prevent a malicious local writer from fabricating the files, or protect a later query from a subsequent edit. Graph output remains untrusted data; relevant claims must be verified in current source. This first version is a cooperative Session instruction, not a hook enforcing or wrapping every direct Graphify invocation.

Unsupported or incomplete cases use ordinary search: missing tools or index, malformed graph/manifest, unknown manifest hashes, changed/deleted/new source, unsafe paths, custom `.graphifyrc` or `.graphifyignore`, cancellation, and exhausted scan/read budgets. The conservative scan does not interpret Git ignore rules; a code file intentionally excluded by upstream can cause a fallback. It checks Graphify 0.9.97's known source extensions plus manifest-listed files. It does not promise freshness for arbitrary future extensions or custom classification.

The read-only check has registered tool bounds: 16 MiB per metadata/source file, 128 MiB total bytes read, 20,000 directory entries, and a cooperative five-second context deadline. The JSON answer reports bytes read and entries scanned. File I/O is checked between operations; this is not a promise to interrupt an unresponsive filesystem syscall. Tool bounds are registered separately from daemon diagnostics because launching a Session does not run the scan or keep a cache.

After a successful check, optional queries use `GRAPHIFY_BACKEND=none graphify query <question>`. Missing executables or any uncertain result fall back to search. Enabling the setting does not authorize automatic installation, index rebuilding, API spending or deletion. There is no uninstall control in this first version, and no third-party dependency or existing graph is owned by Clawdline to remove.

Validation includes an independent native index from the pilot, same-mtime edits, added hidden YAML/configuration files, deletion, renaming, malformed and borrowed indexes, symlink escape, oversized metadata, cancellation and a byte-for-byte read-only check. The capacity guard was run red with the new undeclared read/scan bounds before they were registered. Shared API, machine opt-in, launch/resume wiring and UI acceptance belong to the integration owner and must be checked on the combined branch.
