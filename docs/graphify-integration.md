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

The read-only check has registered tool bounds: 64 MiB per metadata file, 16 MiB per source file, 256 MiB total bytes read, 20,000 directory entries, and a cooperative five-second context deadline. The JSON answer reports bytes read and entries scanned. File I/O is checked between operations; this is not a promise to interrupt an unresponsive filesystem syscall. Tool bounds are registered separately from daemon diagnostics because launching a Session does not run the scan or keep a cache.

After a successful check, optional queries use `GRAPHIFY_BACKEND=none graphify query <question>`. Missing executables or any uncertain result fall back to search. Enabling the setting does not authorize automatic installation, index rebuilding, API spending or deletion. The switch has no uninstall control. An explicit local index build, described below, owns its private parser runtime and the new index it publishes; it never takes ownership of an existing installation or index.

Validation includes an independent native index from the pilot, same-mtime edits, added hidden YAML/configuration files, deletion, renaming, malformed and borrowed indexes, symlink escape, oversized metadata, cancellation and a byte-for-byte read-only check. The capacity guard was run red with the new undeclared read/scan bounds before they were registered. Shared API, machine opt-in, launch/resume wiring and UI acceptance belong to the integration owner and must be checked on the combined branch.


## Project graph viewer

When the machine's Graphify switch is on, Session info offers **View project graph** for its current working directory. A Project's settings dialog offers the same button beside its Settings and Files views. Turning the switch off hides these entries; the daemon independently refuses new graph reads before resolving a directory, and rechecks the switch before publishing a result. An open viewer clears its graph when the settings UI reports an off change. Already received data cannot be recalled from another browser.

`GET /v1/projects/:place/graphify` resolves only a directory in this machine's current Project catalog. `GET /v1/sessions/:id/graphify` resolves only the selected live Session's directory, including execution pins where the caller supplies them. Neither accepts a client filesystem path, query fields or a body. The narrow Cloud reads are `project-graphify` and `session-graphify`; the latter also supports the generation-pinned content-read rail. Old daemons may refuse these reads; API level 34 advertises the new subresources. This change needs a console and daemon update, but no Cloud API or relay change.

The built-in viewer reads the existing schema-1 `graphify-out/graph.json`; it does not execute or embed Graphify's native `graph.html`. Only node labels, relative-source descriptions, source locations, community identifiers and relationship text are projected into inert JSON. Extra attributes and executable markup are not interpreted. Rendering uses the console's own SVG and controls, without a third-party visualization script or network request. The view includes search, community filtering, zoom, keyboard-selectable nodes, an accessible node list and a connection inspector.

Freshness uses the same bounded, read-only source check as optional Session queries. The response projects the exact graph bytes read by that check. A stale source graph remains viewable with a persistent warning; missing, unsafe, custom or unsupported indexes show an explicit unavailable state instead. A source match is point-in-time evidence, not a certificate of edge correctness. Each read includes its check time. No index is installed, built or refreshed automatically; a graph belongs to the specific working directory and is not borrowed from a parent, another checkout or another machine.

For predictable phone rendering and Cloud payload size, an overview contains at most 300 nodes, 900 links and 120 characters per text field. It selects the most connected nodes, with stable tie ordering, and says how many of the original nodes and links are shown. Search, filters and the relationship inspector cover only this displayed subset. Community-cluster positions are a visual layout, not architectural or runtime evidence. The viewer caps zoom at 4x; each website read has a 15-second whole-response deadline and no automatic retries. All bounds are registered in `projects.graphify_*`, with on-demand capacity readings and no retained daemon graph cache.

Validation covers authenticated opt-in and off refusals, known-directory resolution, source changes, missing indexes, malformed identities, dangling links, data projection, explicit overview limits and read-only byte preservation. Cloud tests cover narrow identity mapping, rejected extra fields and pinned reads. Browser acceptance uses synthetic Project/Session data with the production console, in English and Taiwan Traditional Chinese at desktop and phone sizes; this is separate from production Cloud acceptance.


## Explicit local index creation

When a working directory has no `graphify-out`, the viewer offers **Build index**.
API level 36 advertises this build capability. The button is a mutation separate from enabling the machine switch or reading a
graph. It requires device send access and a bounded Idempotency-Key. The same
Project/Session routes accept `POST {}`; they refuse query fields, client paths,
commands, provider/model options, and nonempty bodies. Cloud carries the two
write-gated commands `project-graphify-build` and `session-graphify-build`; the
Session command requires its execution generation. Accepted commands return 202;
GET reports current progress. Durable acceptance receipts prevent an identical
request from running again after a lost response or daemon restart. In-memory
progress does not survive restart: reopen/read the viewer and explicitly retry a
missing index with a new press. Reads and polling never start work.

The first explicit build requires an installed `uv` and Python 3.12 or later.
Clawdline creates a private environment below its own state directory, with
`graphifyy==0.9.97` and exact dependency versions and wheel SHA-256 hashes from
`parser-requirements.txt`. Only binary wheels from PyPI are accepted; no optional
model packages or system/global assistant installations are added. Preparing the
parser may need an Internet connection. Project parsing then uses an immutable,
size-limited source snapshot, a private home, an environment allowlist without
provider keys or proxies, and `--code-only --no-gitignore` with sequential AST
extraction. A Python audit hook refuses socket and child-process operations.
This hook is a tripwire, not an OS sandbox against compromised native wheels.
No project module is imported or executed, no code is uploaded, and no model or
usage-priced content API is called. The fixed version and lock file must be
reviewed when updating the parser.

The snapshot includes recognized source/configuration files under the same
conservative exclusions as the freshness checker, including `.clawdline-graphify-*`
staging directories. It excludes docs/media and
rejects custom `.graphifyrc`/`.graphifyignore`, source symlinks and nonregular
sources. Hashes cover the snapshot inventory even when an upstream grammar has
no extractor or emits no symbols. Scanned-file counts are not extraction coverage;
a matched manifest does not prove that every language or relationship was parsed.
Existing stale or custom indexes still need separate updating. This version does
not offer automatic refresh, replacement, removal or background watching.

A build validates graph identities and relationships. External/unresolved dependency references keep their nodes and edges, but their source paths/locations are cleared and their count is shown as unverified. It
checks live file hashes and the complete inventory again, and publishes an entire
candidate directory using a kernel no-replace rename. An existing destination,
even an empty directory created concurrently, is preserved. The new output
directory includes its own Git ignore rule, without editing repository rules,
to keep generated local graph data out of ordinary Git adds. A changed/deleted/new
source, failure, cancellation or limit refusal leaves no partial index. Turning
the integration off cancels its active operation and is checked again before
publication; closing the dialog only stops polling. Unpublished snapshots and
staging files are removed after the operation. Graceful daemon shutdown closes admission, cancels the active operation and
joins cleanup within the existing daemon shutdown budget. The AST worker also
watches its parent pipe for EOF as a crash tripwire; native code can delay a
Python thread. Interrupted processes may leave
private runtime/staging residue; it is never reused as a completed index.

There is one active build per machine, with no queue. Up to 16 status receipts are
held; terminal entries expire after ten minutes when a new build starts. The
operation deadline is ten minutes, progress/permission/disk checks run at one
second intervals, private command logs retain at most 64 KiB, native output reads
are capped at 128 MiB, and compact graph/manifest files retain the existing
64 MiB metadata cap. The polled runtime/build disk budget is 512 MiB and can
overshoot between checks; it is not an OS quota. Existing source scan/read bounds
still apply. Failure reasons are typed and localized rather than raw parser logs.

Validation includes native pinned-parser extraction and its network tripwire,
HTTP authentication/send gates, strict input validation, durable acceptance
replay, duplicate clicks and the single slot, source edits/additions/deletions,
custom config, switch-off cancellation, malformed graphs, concurrent destination
creation, and read-only preservation of existing indexes. Browser fixtures cover
the missing/building/failed/retry/completed flow, close/reopen without restarting,
and English/Taiwan Traditional Chinese at desktop and phone widths. These local
checks are separate from production console and running-dev acceptance.
