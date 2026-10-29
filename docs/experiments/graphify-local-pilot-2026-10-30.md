# Graphify local pilot — 2026-10-30

For maintainers considering an optional code-navigation aid. This is a small local retrieval and index-maintenance pilot, not an independent Agent A/B study.

## Decision

Do not enable Graphify as a default Session rule from this evidence. Manual code-navigation use remains a candidate, with source verification and an explicit refresh. A fresh-process query accepted a stale index and returned the old call as `EXTRACTED` without a freshness warning. The previously defined reliability condition for automatic adoption therefore failed; the larger model experiment was not started. No product integration, global skill, hooks, daemon changes, deployment or paid model calls were made.

## Fixed subjects and predeclared scope

- Clawdline: `2a011846e0cdcfef43c1fc3f744a168d06bf053b`.
- Graphify: `0a8cd4e1e342235fe53e881fb5d591bf0460071f`; source metadata version `0.9.97`.
- Downloaded official GitHub source archive SHA-256: `d69145201e7677ecdf7040d941075514b7319f15caf58e69f9eb2c470e7d3ac8`.
- The official default branch was `v8` at initial review. Do not use the historical `main` branch as a substitute.
- Python 3.14.6; isolated environment, binary dependency wheels only; pinned source imported directly with `PYTHONPATH`. The distribution itself was not installed, so its CLI package-version metadata may read `unknown`.
- 47 tracked files / 728,747 bytes from an immutable Git archive. Includes tests and three inputs reported with no symbols (`go.mod`, `web/core/src/index.ts`, `web/core/src/refusal.test.mjs`).
- Paths: `go.mod`, `internal/adapters/documents`, `internal/adapters/sessionfiles`, `internal/domain/privacy`, `web/core/src`, `web/console/src/legacy/js/net`. External packages and the remainder of the repository are outside this corpus.
- Forty direct source-level call pairs (20 Go, 20 JavaScript) and eight retrieval tasks were written before graph construction. Relation recall floor 85%; sampled relation precision floor 95%. TypeScript was exercised by a retrieval question, not by the call-precision sample.

## Controls and limitations

`GRAPHIFY_BACKEND=none`, a minimal environment with no provider credentials, task-owned HOME/cache directories and one AST worker were used. A Python audit hook blocked socket connect/send/bind and child-process execution, and a positive control proved it rejects a socket connection. macOS `sandbox-exec` was unavailable (`Operation not permitted`). This is Python-level instrumentation, not an OS network sandbox and not proof about arbitrary native-library networking. No Python network attempts appeared in the run audit. Child-process attempts were blocked; those restrictions can affect normal Graphify behavior.

No paid model requests were initiated: request count 0, API charge for this experiment 0. Public source/package downloads were used. No claim is made about the provider cost of this conversation.

There were no independent answering Agents, token accounting, patch tasks, blinded graders or repeated task trials. The same operator prepared the gold set, curated search expressions and inspected the graph. Retrieval results measure whether an expected file appears anywhere in the returned text, not answer correctness or ranking. Output bytes are not model tokens; process latency is not end-to-end task latency. Timing observations are single-machine samples.

## Construction and relation checks

| Check | Observed result |
| --- | --- |
| First clean extraction | 3.410 s; 924 nodes, 2,328 edges; child-process peak RSS 170,262,528 bytes |
| Second independent clean extraction | 1.451 s; 924 nodes, 2,328 edges |
| Reproducibility | Node IDs/labels/source locations and edge endpoints/relation/locations match after sorting; community IDs and metadata excluded |
| Known-call recall | 39/40 = 97.5%; passes the pilot recall floor |
| Missing known call | `Read()` to `File()` in `internal/adapters/documents/documents.go:299` |
| Sampled emitted calls | 40/40 manually inspected correct (20 Go, 20 JavaScript); passes the sample precision floor, not a repository-wide guarantee |
| Forced rebuild | 2.314 s; one external `@clawdline/contract` stub disappeared (923 nodes / 2,327 edges); clean and refreshed outputs are not byte-identical |
| Initial graph output directory | 1,653,154 bytes at measurement |
| Isolated dependency environment | 107,958,309 file bytes at measurement; source/archive/cache excluded |

The failed first install command applied `--only-binary :all:` to the local Graphify source itself and was refused. Recovery installed binary dependency wheels and imported the pinned source directly; no native compilation or global installation was needed. This is setup friction, not a Graphify extraction failure.

## Curated retrieval tasks

Each Graphify command was `query <query> --budget 1500`; baseline was `rg -n -i <expression> . -g !graphify-out/**`. Both accessed identical source bytes. These are curated retrieval probes, not a fair estimate of how an unfamiliar Agent will formulate queries.

| ID | Graphify query | Baseline expression | Expected-file hit (graph / rg) | CLI seconds (graph / rg) | Output bytes (graph / rg) |
| --- | --- | --- | --- | --- | --- |
| P01 | `project document root containment` | `ProjectRoot\|holdsTheAsker` | True / True | 0.484 / 0.0075 | 20687 / 1730 |
| P02 | `Read sanitizeHTML` | `sanitizeHTML` | True / True | 0.517 / 0.0087 | 4989 / 1870 |
| P03 | `video revision range` | `VideoChanged\|VideoRange` | True / True | 0.469 / 0.0084 | 5110 / 1441 |
| P04 | `documentShareURL normalizeDocumentLocator` | `documentShareURL\|normalizeDocumentLocator` | True / True | 0.508 / 0.0083 | 5038 / 2267 |
| P05 | `transcript view fallback` | `tools-v2\|tools-v1\|invalid_transcript_view` | False / True | 0.497 / 0.0073 | 5064 / 669 |
| P06 | `checkpoint words changed` | `words-changed\|WordsDigest` | True / True | 0.489 / 0.0083 | 15500 / 1294 |
| P07 | `nativeReceipts safeNativePath` | `nativeReceipts\|safeNativePath` | True / True | 0.477 / 0.0104 | 8700 / 402 |
| P08 | `documentAnswer documentBytesAnswer base64Bytes` | `documentAnswer\|documentBytesAnswer\|base64Bytes` | True / True | 0.477 / 0.0084 | 5066 / 5477 |

Expected-file hits: Graphify 7/8, rg 8/8. Median process time: Graphify 0.487 s, rg 0.0084 s. Median output: Graphify 5088 bytes, rg 1585.5 bytes. Five Graphify answers carried a truncation notice. These figures do not establish Agent accuracy, token savings or return on investment.

P05 asked for the `tools-v2` to `tools-v1` transcript fallback. The expected source is `web/core/src/client.ts`; it was absent from the returned Graphify output at this budget. The tool returned exit 0, which is therefore not a success criterion.

## Maintenance and failure injection

A task-owned copy of the real corpus received a synthetic Go fixture with `ProbeCaller`, `ProbeTarget` and `ProbeAlternate`. This is a controlled probe, not a production bug fix.

| Operation | Result | Update seconds |
| --- | --- | --- |
| add | Passed; expected nodes/call target/source paths | 2.283 |
| rename | Passed; expected nodes/call target/source paths | 2.190 |
| retarget | Passed; expected nodes/call target/source paths | 2.153 |
| delete | Passed; expected nodes/call target/source paths | 2.222 |

After editing the caller from `ProbeTarget()` to `ProbeAlternate()`, **before updating**, a fresh `explain ProbeCaller` process exited 0 and returned:

```text
--> ProbeTarget() [calls] [EXTRACTED] pilot-fixture/renamed.go:L2
```

There was no stale-index warning. After `update .`, the call correctly targeted `ProbeAlternate()`. The default query path therefore relies on the caller to refresh or separately verify freshness. This is the reason automatic adoption fails the pilot gate.

Two packages defining `ProbeTwin()` produced exit 3 and an ambiguity explanation, without silently choosing one. A malformed JSON index produced exit 1 with a concise load error. Mutation-copy operations left the original source hashes and original graph bytes unchanged. This verifies separate-directory isolation; concurrent shared-output writes and real Git branch-switch hooks were not tested.

## Reproduction

Use the two pinned commits above, an isolated Python environment and an immutable Git archive of the listed paths. Keep code and graph outputs separate per checkout; never run the automatic installer during this pilot. Install the dependencies declared by the pinned `pyproject.toml` from binary wheels, then point `PYTHONPATH` at that pinned source. Exact resolved dependency versions are below. Set `GRAPHIFY_BACKEND=none` and use empty task-owned HOME/cache directories. The audit wrapper additionally denied Python socket operations and subprocesses.

```sh
python -m graphify extract . --code-only --max-workers 1
# Repeat in a fresh directory from identical source bytes.
python -m graphify query "transcript view fallback" --budget 1500
python -m graphify query "Read sanitizeHTML" --budget 1500
python -m graphify update .
```

Run each retrieval question with its table expressions. Score only the predeclared expected paths below. Normalize node and edge identity/source tuples for clean-build comparison; do not compare timestamps or community numbers. For maintenance, add `package pilot` with `func ProbeCaller() { ProbeTarget() }`, `func ProbeTarget() {}` and `func ProbeAlternate() {}`; update, rename its file, update, retarget the caller, explain before updating, update, and finally remove the fixture and update. Require the pre-update explanation to warn or refuse stale data before calling a future integration freshness-safe.

## Evidence

The compact evidence below preserves predeclared tasks, all gold-call results, every precision-sample identity and verdict, retrieval measurements, maintenance assertions and failure outcomes. Full local stdout/stderr, graph snapshots, source hashes and dependency logs were retained in the Session evidence bundle. No graph or probe was added to the application source.

```json
{
  "dependency_versions": [
    "networkx==3.7",
    "numpy==2.5.4",
    "pyyaml==6.0.3",
    "rapidfuzz==3.14.6",
    "tree-sitter==0.25.2",
    "tree-sitter-bash==0.25.1",
    "tree-sitter-c==0.24.2",
    "tree-sitter-c-sharp==0.23.5",
    "tree-sitter-cpp==0.23.4",
    "tree-sitter-elixir==0.3.5",
    "tree-sitter-fortran==0.6.0",
    "tree-sitter-go==0.25.0",
    "tree-sitter-groovy==0.1.2",
    "tree-sitter-java==0.23.5",
    "tree-sitter-javascript==0.25.0",
    "tree-sitter-json==0.24.8",
    "tree-sitter-julia==0.23.1",
    "tree-sitter-kotlin==1.1.0",
    "tree-sitter-lua==0.5.0",
    "tree-sitter-objc==3.0.2",
    "tree-sitter-php==0.24.1",
    "tree-sitter-powershell==0.26.4",
    "tree-sitter-python==0.25.0",
    "tree-sitter-ruby==0.23.1",
    "tree-sitter-rust==0.24.2",
    "tree-sitter-scala==0.26.3",
    "tree-sitter-swift==0.7.4",
    "tree-sitter-typescript==0.23.2",
    "tree-sitter-verilog==1.0.3",
    "tree-sitter-zig==1.1.2"
  ],
  "gold": {
    "scope": "small local retrieval pilot, not independent agent A/B",
    "pairs": [
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "ProjectRoot",
        "target": "root"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "ProjectRoot",
        "target": "holdsTheAsker"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "holdsTheAsker",
        "target": "IsInside"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "holdsTheAsker",
        "target": "ResolvedPath"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "root",
        "target": "ResolvedPath"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "root",
        "target": "IsInside"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "File",
        "target": "file"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "file",
        "target": "extensionOf"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "file",
        "target": "ResolvedPath"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "file",
        "target": "IsInside"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "Read",
        "target": "File"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "Read",
        "target": "extensionOf"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "Read",
        "target": "sanitizeHTML"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "Walk",
        "target": "ResolvedPath"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "Walk",
        "target": "file"
      },
      {
        "source_file": "internal/adapters/documents/documents.go",
        "source": "Walk",
        "target": "extensionOf"
      },
      {
        "source_file": "internal/adapters/documents/video.go",
        "source": "ReadVideo",
        "target": "file"
      },
      {
        "source_file": "internal/adapters/documents/video.go",
        "source": "ReadVideo",
        "target": "ResolvedPath"
      },
      {
        "source_file": "internal/domain/privacy/history.go",
        "source": "Usable",
        "target": "WordsDigest"
      },
      {
        "source_file": "internal/domain/privacy/history.go",
        "source": "RulesDigest",
        "target": "sortedKeys"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "normalizeDocumentIdentity",
        "target": "textIdentity"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "textIdentity",
        "target": "utf8Length"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentIdentityForSession",
        "target": "normalizeDocumentIdentity"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentIdentityForSession",
        "target": "documentLinkError"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "normalizeDocumentLocator",
        "target": "textIdentity"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "normalizeDocumentLocator",
        "target": "documentPath"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentHash",
        "target": "locatorFields"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentHash",
        "target": "normalizeDocumentLocator"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentLocatorFromHash",
        "target": "normalizeDocumentLocator"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentAnswer",
        "target": "normalizeDocumentLocator"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentAnswer",
        "target": "answerKeys"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentAnswer",
        "target": "base64Bytes"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentAnswer",
        "target": "documentBytesAnswer"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentBytesAnswer",
        "target": "normalizeDocumentLocator"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "shareDocument",
        "target": "documentShareURL"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentShareURL",
        "target": "normalizeDocumentLocator"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentShareURL",
        "target": "documentHash"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentShareURL",
        "target": "documentLinkError"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "documentPath",
        "target": "utf8Length"
      },
      {
        "source_file": "web/console/src/legacy/js/net/document-links.js",
        "source": "localDocumentListing",
        "target": "documentListing"
      }
    ],
    "tasks": [
      {
        "id": "P01",
        "question": "Which code validates project document roots and refuses roots containing the caller?",
        "query": "project document root containment",
        "rg": "ProjectRoot|holdsTheAsker",
        "files": [
          "internal/adapters/documents/documents.go"
        ]
      },
      {
        "id": "P02",
        "question": "Where does an HTML document read sanitize content?",
        "query": "Read sanitizeHTML",
        "rg": "sanitizeHTML",
        "files": [
          "internal/adapters/documents/documents.go",
          "internal/adapters/documents/html.go"
        ]
      },
      {
        "id": "P03",
        "question": "What validates an MP4 chunk revision and the supported byte range?",
        "query": "video revision range",
        "rg": "VideoChanged|VideoRange",
        "files": [
          "internal/adapters/documents/video.go"
        ]
      },
      {
        "id": "P04",
        "question": "How are public Cloud document links constructed and normalized?",
        "query": "documentShareURL normalizeDocumentLocator",
        "rg": "documentShareURL|normalizeDocumentLocator",
        "files": [
          "web/console/src/legacy/js/net/document-links.js"
        ]
      },
      {
        "id": "P05",
        "question": "Where is the transcript view fallback from tools-v2 to tools-v1?",
        "query": "transcript view fallback",
        "rg": "tools-v2|tools-v1|invalid_transcript_view",
        "files": [
          "web/core/src/client.ts"
        ]
      },
      {
        "id": "P06",
        "question": "Where is a privacy checkpoint rejected after the configured word list changes?",
        "query": "checkpoint words changed",
        "rg": "words-changed|WordsDigest",
        "files": [
          "internal/domain/privacy/history.go"
        ]
      },
      {
        "id": "P07",
        "question": "Which provider-native file receipts must pass path safety checks?",
        "query": "nativeReceipts safeNativePath",
        "rg": "nativeReceipts|safeNativePath",
        "files": [
          "internal/adapters/sessionfiles/scan.go"
        ]
      },
      {
        "id": "P08",
        "question": "Which client validates the base64 document byte count before returning UTF-8 text?",
        "query": "documentAnswer documentBytesAnswer base64Bytes",
        "rg": "documentAnswer|documentBytesAnswer|base64Bytes",
        "files": [
          "web/console/src/legacy/js/net/document-links.js"
        ]
      }
    ],
    "thresholds": {
      "edge_recall": 0.85,
      "precision": 0.95
    },
    "clawdline_commit": "2a011846e0cdcfef43c1fc3f744a168d06bf053b",
    "graphify_commit": "0a8cd4e1e342235fe53e881fb5d591bf0460071f"
  },
  "gold_results": [
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "ProjectRoot",
      "target": "root",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "ProjectRoot",
      "target": "holdsTheAsker",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "holdsTheAsker",
      "target": "IsInside",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "holdsTheAsker",
      "target": "ResolvedPath",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "root",
      "target": "ResolvedPath",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "root",
      "target": "IsInside",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "File",
      "target": "file",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "file",
      "target": "extensionOf",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "file",
      "target": "ResolvedPath",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "file",
      "target": "IsInside",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "Read",
      "target": "File",
      "found": false
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "Read",
      "target": "extensionOf",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "Read",
      "target": "sanitizeHTML",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "Walk",
      "target": "ResolvedPath",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "Walk",
      "target": "file",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/documents.go",
      "source": "Walk",
      "target": "extensionOf",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/video.go",
      "source": "ReadVideo",
      "target": "file",
      "found": true
    },
    {
      "source_file": "internal/adapters/documents/video.go",
      "source": "ReadVideo",
      "target": "ResolvedPath",
      "found": true
    },
    {
      "source_file": "internal/domain/privacy/history.go",
      "source": "Usable",
      "target": "WordsDigest",
      "found": true
    },
    {
      "source_file": "internal/domain/privacy/history.go",
      "source": "RulesDigest",
      "target": "sortedKeys",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "normalizeDocumentIdentity",
      "target": "textIdentity",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "textIdentity",
      "target": "utf8Length",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentIdentityForSession",
      "target": "normalizeDocumentIdentity",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentIdentityForSession",
      "target": "documentLinkError",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "normalizeDocumentLocator",
      "target": "textIdentity",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "normalizeDocumentLocator",
      "target": "documentPath",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentHash",
      "target": "locatorFields",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentHash",
      "target": "normalizeDocumentLocator",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentLocatorFromHash",
      "target": "normalizeDocumentLocator",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentAnswer",
      "target": "normalizeDocumentLocator",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentAnswer",
      "target": "answerKeys",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentAnswer",
      "target": "base64Bytes",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentAnswer",
      "target": "documentBytesAnswer",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentBytesAnswer",
      "target": "normalizeDocumentLocator",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "shareDocument",
      "target": "documentShareURL",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentShareURL",
      "target": "normalizeDocumentLocator",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentShareURL",
      "target": "documentHash",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentShareURL",
      "target": "documentLinkError",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "documentPath",
      "target": "utf8Length",
      "found": true
    },
    {
      "source_file": "web/console/src/legacy/js/net/document-links.js",
      "source": "localDocumentListing",
      "target": "documentListing",
      "found": true
    }
  ],
  "precision_sample": [
    {
      "sample": 1,
      "source": ".line()",
      "source_file": "internal/domain/privacy/privacy.go",
      "source_location": "L208",
      "target": "ipAddresses()",
      "target_file": "internal/domain/privacy/privacy.go",
      "target_location": "L514",
      "verdict": "correct"
    },
    {
      "sample": 2,
      "source": "sanitizeStylesheet()",
      "source_file": "internal/adapters/documents/html.go",
      "source_location": "L160",
      "target": "filteredDeclarations()",
      "target_file": "internal/adapters/documents/html.go",
      "target_location": "L117",
      "verdict": "correct"
    },
    {
      "sample": 3,
      "source": ".line()",
      "source_file": "internal/domain/privacy/privacy.go",
      "source_location": "L184",
      "target": "paneIDs()",
      "target_file": "internal/domain/privacy/privacy.go",
      "target_location": "L335",
      "verdict": "correct"
    },
    {
      "sample": 4,
      "source": "publishedFile()",
      "source_file": "internal/adapters/sessionfiles/store.go",
      "source_location": "L219",
      "target": "inspect()",
      "target_file": "internal/adapters/sessionfiles/store.go",
      "target_location": "L263",
      "verdict": "correct"
    },
    {
      "sample": 5,
      "source": "homePaths()",
      "source_file": "internal/domain/privacy/privacy.go",
      "source_location": "L242",
      "target": "isWord()",
      "target_file": "internal/domain/privacy/privacy.go",
      "target_location": "L587",
      "verdict": "correct"
    },
    {
      "sample": 6,
      "source": "file()",
      "source_file": "internal/adapters/documents/documents.go",
      "source_location": "L273",
      "target": "ResolvedPath()",
      "target_file": "internal/adapters/documents/documents.go",
      "target_location": "L110",
      "verdict": "correct"
    },
    {
      "sample": 7,
      "source": ".Thumbnail()",
      "source_file": "internal/adapters/sessionfiles/thumbnail.go",
      "source_location": "L15",
      "target": ".Read()",
      "target_file": "internal/adapters/sessionfiles/store.go",
      "target_location": "L372",
      "verdict": "correct"
    },
    {
      "sample": 8,
      "source": ".line()",
      "source_file": "internal/domain/privacy/privacy.go",
      "source_location": "L191",
      "target": "credentials()",
      "target_file": "internal/domain/privacy/privacy.go",
      "target_location": "L408",
      "verdict": "correct"
    },
    {
      "sample": 9,
      "source": ".line()",
      "source_file": "internal/domain/privacy/privacy.go",
      "source_location": "L212",
      "target": "wordIndex()",
      "target_file": "internal/domain/privacy/privacy.go",
      "target_location": "L563",
      "verdict": "correct"
    },
    {
      "sample": 10,
      "source": "scan()",
      "source_file": "internal/adapters/sessionfiles/scan.go",
      "source_location": "L77",
      "target": "nativeReceipts()",
      "target_file": "internal/adapters/sessionfiles/scan.go",
      "target_location": "L231",
      "verdict": "correct"
    },
    {
      "sample": 11,
      "source": ".load()",
      "source_file": "internal/adapters/sessionfiles/store.go",
      "source_location": "L85",
      "target": ".path()",
      "target_file": "internal/adapters/sessionfiles/store.go",
      "target_location": "L82",
      "verdict": "correct"
    },
    {
      "sample": 12,
      "source": "scan()",
      "source_file": "internal/adapters/sessionfiles/scan.go",
      "source_location": "L78",
      "target": "safeNativePath()",
      "target_file": "internal/adapters/sessionfiles/scan.go",
      "target_location": "L101",
      "verdict": "correct"
    },
    {
      "sample": 13,
      "source": "TaskRoot()",
      "source_file": "internal/adapters/documents/documents.go",
      "source_location": "L205",
      "target": "root()",
      "target_file": "internal/adapters/documents/documents.go",
      "target_location": "L211",
      "verdict": "correct"
    },
    {
      "sample": 14,
      "source": "ParseCheckpoint()",
      "source_file": "internal/domain/privacy/history.go",
      "source_location": "L223",
      "target": "isHex()",
      "target_file": "internal/domain/privacy/history.go",
      "target_location": "L315",
      "verdict": "correct"
    },
    {
      "sample": 15,
      "source": "scan()",
      "source_file": "internal/adapters/sessionfiles/scan.go",
      "source_location": "L86",
      "target": "merge()",
      "target_file": "internal/adapters/sessionfiles/store.go",
      "target_location": "L222",
      "verdict": "correct"
    },
    {
      "sample": 16,
      "source": "Walk()",
      "source_file": "internal/adapters/documents/documents.go",
      "source_location": "L354",
      "target": "ResolvedPath()",
      "target_file": "internal/adapters/documents/documents.go",
      "target_location": "L110",
      "verdict": "correct"
    },
    {
      "sample": 17,
      "source": ".Read()",
      "source_file": "internal/adapters/sessionfiles/store.go",
      "source_location": "L401",
      "target": "digest()",
      "target_file": "internal/adapters/sessionfiles/store.go",
      "target_location": "L81",
      "verdict": "correct"
    },
    {
      "sample": 18,
      "source": "inlineImagePixels()",
      "source_file": "internal/adapters/documents/html_images.go",
      "source_location": "L43",
      "target": "staticPNG()",
      "target_file": "internal/adapters/documents/html_images.go",
      "target_location": "L65",
      "verdict": "correct"
    },
    {
      "sample": 19,
      "source": "Read()",
      "source_file": "internal/adapters/documents/documents.go",
      "source_location": "L310",
      "target": "extensionOf()",
      "target_file": "internal/adapters/documents/documents.go",
      "target_location": "L506",
      "verdict": "correct"
    },
    {
      "sample": 20,
      "source": "file()",
      "source_file": "internal/adapters/documents/documents.go",
      "source_location": "L275",
      "target": "IsInside()",
      "target_file": "internal/adapters/documents/documents.go",
      "target_location": "L123",
      "verdict": "correct"
    },
    {
      "sample": 21,
      "source": "._machinesFor()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L2308",
      "target": "._evidentMac()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L2256",
      "verdict": "correct"
    },
    {
      "sample": 22,
      "source": "._applySnapshot()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L1758",
      "target": "descriptorCommandsKey()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L312",
      "verdict": "correct"
    },
    {
      "sample": 23,
      "source": "identityPhaseDigest()",
      "source_file": "web/console/src/legacy/js/net/cloud-pairing.js",
      "source_location": "L466",
      "target": "canonicalBytes()",
      "target_file": "web/console/src/legacy/js/net/cloud-pairing.js",
      "target_location": "L113",
      "verdict": "correct"
    },
    {
      "sample": 24,
      "source": "._read()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L3592",
      "target": "._unsupportedRefusal()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L2280",
      "verdict": "correct"
    },
    {
      "sample": 25,
      "source": ".block()",
      "source_file": "web/console/src/legacy/js/net/cloud-viewer-events.js",
      "source_location": "L972",
      "target": "boundedText()",
      "target_file": "web/console/src/legacy/js/net/cloud-viewer-events.js",
      "target_location": "L190",
      "verdict": "correct"
    },
    {
      "sample": 26,
      "source": ".sessions()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L1811",
      "target": "._sessionResponse()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L1789",
      "verdict": "correct"
    },
    {
      "sample": 27,
      "source": "adoptMachineRekey()",
      "source_file": "web/console/src/legacy/js/net/cloud-crypto.js",
      "source_location": "L365",
      "target": "validToken()",
      "target_file": "web/console/src/legacy/js/net/cloud-crypto.js",
      "target_location": "L55",
      "verdict": "correct"
    },
    {
      "sample": 28,
      "source": "._scheduleMachine()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L2490",
      "target": "cloudError()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L275",
      "verdict": "correct"
    },
    {
      "sample": 29,
      "source": ".projectWorktreeLifecycleRefresh()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L3110",
      "target": "._worktreeLifecycle()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L3113",
      "verdict": "correct"
    },
    {
      "sample": 30,
      "source": "keepConnected()",
      "source_file": "web/console/src/legacy/js/net/cloud-boot.js",
      "source_location": "L905",
      "target": "positive()",
      "target_file": "web/console/src/legacy/js/net/cloud-boot.js",
      "target_location": "L1343",
      "verdict": "correct"
    },
    {
      "sample": 31,
      "source": ".machinePairing()",
      "source_file": "web/console/src/legacy/js/net/cloud-boot.js",
      "source_location": "L546",
      "target": ".machineBindingName()",
      "target_file": "web/console/src/legacy/js/net/cloud-boot.js",
      "target_location": "L532",
      "verdict": "correct"
    },
    {
      "sample": 32,
      "source": "._subscriptionRoom()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L4125",
      "target": "cloudFailure()",
      "target_file": "web/console/src/legacy/js/net/cloud-failure.js",
      "target_location": "L112",
      "verdict": "correct"
    },
    {
      "sample": 33,
      "source": ".pushKey()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L4512",
      "target": "._machineRequest()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L2496",
      "verdict": "correct"
    },
    {
      "sample": 34,
      "source": "._relayAnswered()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L3817",
      "target": "ctlMachine()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L4718",
      "verdict": "correct"
    },
    {
      "sample": 35,
      "source": ".record()",
      "source_file": "web/console/src/legacy/js/net/cloud-viewer-events.js",
      "source_location": "L735",
      "target": "._notify()",
      "target_file": "web/console/src/legacy/js/net/cloud-viewer-events.js",
      "target_location": "L755",
      "verdict": "correct"
    },
    {
      "sample": 36,
      "source": ".title()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L4564",
      "target": "._sessionIdentity()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L2115",
      "verdict": "correct"
    },
    {
      "sample": 37,
      "source": ".cloudStatus()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L2519",
      "target": "cloudError()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L275",
      "verdict": "correct"
    },
    {
      "sample": 38,
      "source": "cloudViewerDeviceMetadata()",
      "source_file": "web/console/src/legacy/js/net/cloud-onboarding.js",
      "source_location": "L48",
      "target": "systemName()",
      "target_file": "web/console/src/legacy/js/net/cloud-onboarding.js",
      "target_location": "L68",
      "verdict": "correct"
    },
    {
      "sample": 39,
      "source": "._evidentMac()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L2257",
      "target": "descriptorPlatform()",
      "target_file": "web/console/src/legacy/js/net/cloud-client.js",
      "target_location": "L337",
      "verdict": "correct"
    },
    {
      "sample": 40,
      "source": "._openEnvelopeFrame()",
      "source_file": "web/console/src/legacy/js/net/cloud-client.js",
      "source_location": "L1375",
      "target": "isFailureCode()",
      "target_file": "web/console/src/legacy/js/net/cloud-failure.js",
      "target_location": "L77",
      "verdict": "correct"
    }
  ],
  "retrieval": [
    {
      "id": "P01",
      "question": "Which code validates project document roots and refuses roots containing the caller?",
      "rg_seconds": 0.007512416923418641,
      "graph_seconds": 0.48432633304037154,
      "rg_bytes": 1730,
      "graph_bytes": 20687,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "internal/adapters/documents/documents.go": true
      },
      "graph_expected_paths": {
        "internal/adapters/documents/documents.go": true
      },
      "graph_truncated": false
    },
    {
      "id": "P02",
      "question": "Where does an HTML document read sanitize content?",
      "rg_seconds": 0.008658207952976227,
      "graph_seconds": 0.5174212920246646,
      "rg_bytes": 1870,
      "graph_bytes": 4989,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "internal/adapters/documents/documents.go": true,
        "internal/adapters/documents/html.go": true
      },
      "graph_expected_paths": {
        "internal/adapters/documents/documents.go": true,
        "internal/adapters/documents/html.go": true
      },
      "graph_truncated": true
    },
    {
      "id": "P03",
      "question": "What validates an MP4 chunk revision and the supported byte range?",
      "rg_seconds": 0.00838820799253881,
      "graph_seconds": 0.46856375003699213,
      "rg_bytes": 1441,
      "graph_bytes": 5110,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "internal/adapters/documents/video.go": true
      },
      "graph_expected_paths": {
        "internal/adapters/documents/video.go": true
      },
      "graph_truncated": true
    },
    {
      "id": "P04",
      "question": "How are public Cloud document links constructed and normalized?",
      "rg_seconds": 0.008348084054887295,
      "graph_seconds": 0.5080113339936361,
      "rg_bytes": 2267,
      "graph_bytes": 5038,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "web/console/src/legacy/js/net/document-links.js": true
      },
      "graph_expected_paths": {
        "web/console/src/legacy/js/net/document-links.js": true
      },
      "graph_truncated": true
    },
    {
      "id": "P05",
      "question": "Where is the transcript view fallback from tools-v2 to tools-v1?",
      "rg_seconds": 0.007338874973356724,
      "graph_seconds": 0.4974922080291435,
      "rg_bytes": 669,
      "graph_bytes": 5064,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "web/core/src/client.ts": true
      },
      "graph_expected_paths": {
        "web/core/src/client.ts": false
      },
      "graph_truncated": true
    },
    {
      "id": "P06",
      "question": "Where is a privacy checkpoint rejected after the configured word list changes?",
      "rg_seconds": 0.008303207927383482,
      "graph_seconds": 0.48923187504988164,
      "rg_bytes": 1294,
      "graph_bytes": 15500,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "internal/domain/privacy/history.go": true
      },
      "graph_expected_paths": {
        "internal/domain/privacy/history.go": true
      },
      "graph_truncated": false
    },
    {
      "id": "P07",
      "question": "Which provider-native file receipts must pass path safety checks?",
      "rg_seconds": 0.010425167041830719,
      "graph_seconds": 0.4765990829328075,
      "rg_bytes": 402,
      "graph_bytes": 8700,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "internal/adapters/sessionfiles/scan.go": true
      },
      "graph_expected_paths": {
        "internal/adapters/sessionfiles/scan.go": true
      },
      "graph_truncated": false
    },
    {
      "id": "P08",
      "question": "Which client validates the base64 document byte count before returning UTF-8 text?",
      "rg_seconds": 0.00842825009021908,
      "graph_seconds": 0.4773042080923915,
      "rg_bytes": 5477,
      "graph_bytes": 5066,
      "rg_exit": 0,
      "graph_exit": 0,
      "rg_expected_paths": {
        "web/console/src/legacy/js/net/document-links.js": true
      },
      "graph_expected_paths": {
        "web/console/src/legacy/js/net/document-links.js": true
      },
      "graph_truncated": true
    }
  ],
  "mutations": [
    {
      "operation": "add",
      "pass": true,
      "nodes": [
        [
          "ProbeAlternate()",
          "pilot-fixture/probe.go"
        ],
        [
          "ProbeCaller()",
          "pilot-fixture/probe.go"
        ],
        [
          "ProbeTarget()",
          "pilot-fixture/probe.go"
        ]
      ]
    },
    {
      "operation": "rename",
      "pass": true,
      "nodes": [
        [
          "ProbeAlternate()",
          "pilot-fixture/renamed.go"
        ],
        [
          "ProbeCaller()",
          "pilot-fixture/renamed.go"
        ],
        [
          "ProbeTarget()",
          "pilot-fixture/renamed.go"
        ]
      ]
    },
    {
      "operation": "stale-query",
      "exit_code": 0,
      "claims_old_target": true,
      "warns_stale": false
    },
    {
      "operation": "retarget",
      "pass": true,
      "targets": [
        "ProbeAlternate()"
      ]
    },
    {
      "operation": "delete",
      "pass": true,
      "nodes": []
    }
  ],
  "robustness": {
    "ambiguity-build": {
      "returncode": 0
    },
    "ambiguity-query": {
      "returncode": 3
    },
    "corrupt-query": {
      "returncode": 1
    },
    "workdir_isolation": {
      "original_graph_unchanged": true,
      "original_sources_unchanged": true
    }
  }
}
```
