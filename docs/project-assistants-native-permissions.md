# Native permission classification and cancellation

This unit extends the [runtime contract](project-assistants-runtime.md) and
[reuse evidence boundary](project-assistants-reuse.md). Codync remains pinned
to `234219cef2d3f991f0f7225ba89e3c99cdd34177`; its incoming/pending ACP split
and native session lifecycle are the retained design. This does not import
upstream automatic approval or change Clawdline's narrower `auto_read` policy.
No additional upstream source is copied by this unit.

## Inspected payload sources

Only installed JavaScript files were read; none was imported or executed.
The following paths are relative to the named npm package, not an account or
conversation directory. The versions identify inspected source, not successful
native execution, authentication, sandboxing or model access.

| Package/version | Source and symbol | Observed ACP v1 payload |
| --- | --- | --- |
| `@agentclientprotocol/claude-agent-acp` 0.106.0 | `dist/v1/acp-agent.js`, `canUseTool`; `dist/permissions/presentation.js`, `buildClaudePermissionPresentation`, `permissionToolCallMeta`; `dist/tool-calls/renderer.js`, `permissionToolCall` | `sessionId`, `options`, `toolCall`. Standard non-AIR requests include `toolCallId`, `name`, `status`, `rawInput`, `title`, `kind`, `content`, optional locations/meta. AIR requests may omit name/kind and depend on a prior tool report. MCP metadata identifies a server/config source, not side-effect freedom. |
| `@agentclientprotocol/codex-acp` 2.6.0 | `dist/index.js`, `CodexApprovalHandler.handleCommandExecution`, `handleFileChange`, `handlePermissionsRequest`; `CommandReporter.permission`, `SandboxPermissionReporter.permission`, `AcpToolCallRenderer.renderPermissionToolCall` | Command input can contain command/cwd/url/additional permissions; name is optional. Display read/search/list actions do not prove shell effects. File changes and additional sandbox permission requests are authorization expansion. A read path grant is broader than one pure read. |

SHA-256 fingerprints of the files read:

| File | SHA-256 |
| --- | --- |
| Claude `dist/v1/acp-agent.js` | `811bb2fa230fa43531a2df20996ef710668a2e0723dc0ca9d782d221c221eb97` |
| Claude `dist/permissions/presentation.js` | `9ebb48f7e7d4d8c3adcdeaf6e24edef92f672add00587754a306758678000713` |
| Claude `dist/tool-calls/renderer.js` | `fed8abba3de60cf512a914ea317a9f953d345d930d39c3093998be863cb796de` |
| Codex `dist/index.js` | `733b7521c2a437a34ed016d45a6f46bebff2bac123a172467574ee827db98b99` |

The non-AIR Claude envelope does carry a tool name. That is useful identity
data, but matching `Read` plus a file-path schema alone would not establish the
effective native tool implementation, hooks, path containment or OS sandbox.
The inspected presentation layer does not prove those properties. This unit
therefore establishes no safe true payload for either provider. It does not
claim that native reading is inherently unsafe or impossible to verify later.

## Host integration

`ClassifyNativePermission(agent, adapterVersion, params)` returns a typed
`NativePermissionDecision`. Inspected versions return `read_only_unverified`
and `ReadOnly=false`; other versions return `provider_version_unsupported`,
and other agents return `unsupported`. All paths deny approval. The classifier
does not parse payload bytes: no asserted schema, duplicate JSON key, malformed
input, tool title, option label or newly added field can create authority.
There is deliberately no shell regex allowlist and no new resource bound.

The provider binds its default classifier after host verification of the exact
agent and adapter version. An explicit trusted host override can use:

```go
ReadOnlyPermission: projectassistantacp.NewReadOnlyPermissionClassifier(
    verifiedAgent, verifiedAdapterVersion,
),
```

`verifiedAgent` and `verifiedAdapterVersion` must come from host-owned executable
evidence. Never take them from a permission request. The existing callback
receives the entire `session/request_permission` params byte slice. A provider
sharing one callback across both agents can safely use this deny-all policy,
but future positive classifications need an agent-bound trusted callback.
The integrating root wires this default in `Provider.Open`; it does not derive
trusted identity from the permission request or enable native execution.

This is defense in depth, not admission evidence. Preserve `Verify`, native
memory isolation, effective read-only sandbox and feature-off gates. Before
allowing any known pure read, prove the OS/process/tool/hook boundary and exact
supported payload schema, including canonical/symlink/worktree path behavior.
If that evidence later permits allow-once, keep persistent permissions and all
filesystem/terminal/MCP scope expansion rejected. The current policy still
permits the existing reject-once/cancel path; it never converts a denial into
an automatic native allow.

## Cancellation and identity acceptance

`permission_lifetime_test.go` uses an in-process Go scripted peer over real
pipes. It needs no process census and does not test subprocess supervision. Its wire
receipt requires an exact `{"outcome":{"outcome":"cancelled"}}` permission
response before an ID-less `session/cancel` for the native session. This tests
our transport and a scripted peer, not vendor-native cancellation.

The tests also require cancelled offers to reject `Answer`, keep public offer
IDs separate from native RPC IDs, and apply the domain's full fence/turn/offer
check before calling native `Answer`. The low-level `Answer(nativeID, optionID)`
signature cannot validate a fence by itself. The host must first execute the
durable domain/Engine Answer path; a raw native ID is never an authorization
token. The lifetime fixture's always-true callback is synthetic authority used
only to isolate cancellation; it is not the production classifier.

`TestACPPermissionCancelSealsLateNativeRequests` sends a permission again with
the same native ID after observing cancellation but before prompt completion.
It requires admission to stay closed or the client to fail closed, and rejects
any successful late Answer. A failure is a client integration defect, not a
reason to loosen this test or grant the late request. The integration owner
must seal admission before cancellation writes, invalidate pending offers on
termination, and preserve the turn/generation identity of every offer. Do not
restore permission admission merely because an old prompt has not completed.

On the original implementation base `648608c8`, the normal cancelled wire
response and domain identity controls passed. The retained late-request
reproducer failed:
the native ID is inserted again while the old turn remains active, and Answer
writes a selected allow-once response. This is a synthetic client reproducer,
not a vendor-native observation. `client.go` belongs to the integration owner;
the original failing result is retained. The integrating root
fixes the client in `accd6b745`: permission admission is sealed before cancel,
offers retain the captured turn, and a cancelled prompt cannot promote a late
successful reply. The wire test now also observes the peer receipt for a normal
second cancelled response, while still refusing a revived native ID and
requiring zero pending permission offers. The preserved original red and
subsequent passing race receipt are distinct evidence.

No vendor adapter/CLI, login operation, model API, Cloud, deployment or real Bot
provider is executed. Model/API request count: 0; cost: 0. Native cancellation,
native sandboxing, real provider hooks and real cross-generation callbacks
remain unverified.
