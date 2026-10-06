# Daemon and CLI localization contract

`product_language` is a daemon setting separate from `language` and
`voice_language`. An absent, malformed, or unsupported saved product value
renders English without rewriting the stored file. The old settings still
control agent/Board authoring and voice auto fallback. The shipped product
tags are `en`, `zh-Hant`, `ja`, `zh-Hans`, `ko`, `es`, `pt-BR`, `fr`, and `de`.

CLI locale precedence is a leading `clawdline --lang <tag> <command>`, then
`CLAWDLINE_LANG`, then the saved `product_language`, then English. The leading
option is process-local and does not write settings. A well-formed unsupported
tag falls back to English; a malformed leading option is usage error 2. JSON
output, error codes, keys, version strings, task IDs, `guide-version` and
`Idempotency-Key` remain machine-readable and stable. User/agent-authored
document and message bodies are passed through unchanged. Guide's explicit
language argument overrides the product preference.

`GET /v1/strings` reads `web/console/public/catalogs/<tag>.json`; no `lang`
means English. Syntactically malformed tags return 400 `bad_request`;
the API requires hyphen-separated tags, while saved legacy settings may use
underscores. Known region and script aliases resolve to their shipped catalog.
Unknown, missing, invalid JSON, extra keys, invalid placeholder/plural, or
unsafe markup in a selected language causes a whole-catalog English response
with `lang=en`.
An otherwise valid secondary-language catalog may omit newly added English
keys: the API returns that original partial catalog with its actual language,
and the Console fills missing keys from English and counts them. `zh-Hant` and
`en` remain complete on every change. If English itself cannot be validated,
the server returns 503
`catalog_unreadable`; the optional page embed is omitted so the browser can
use its bundled English. The local page starts hidden with `lang=en`; only the
browser can resolve its origin-local `ui_language` preference.

## Output-site matrix

Run `python3 cmd/clawdline/localization_inventory.py` from the repository
root to reproduce [localization_matrix.tsv](localization_matrix.tsv), or add
`--check` to verify it. Each row has source site, trigger function, stable code
where one is present, output channel, contract classification, owner, and the
source excerpt. The inventory scans every non-test Go source file under
`cmd/clawdline`, `internal/app`, `internal/transport/http`, and
`internal/domain/capacity` for CLI writes, pushes, schedule notices, and HTTP
refusals. It deliberately excludes `fmt`
string construction that is not an output site, tests, and generated types.
The guide's source text is under `skills/clawdline/` and is owned by the
separate guide feature; `cmd/clawdline/skill.go` is its CLI delivery site.

At the combined integration inventory there are 1,722 sites: 550 fixed CLI human-copy
sites, 21 composed human-output sites, 320 catalogued CLI sites, seven explicit
machine-output sites, 34 machine or protocol passthrough sites, four original
user/Agent/external content sites, two guide delivery sites, 58 layout/data
sites, four builder source sites, nine
diagnostic log sites, 556 HTTP refusal call sites, 11 notification call sites,
and 146 notification copy sources. These are source and call sites, not distinct sentences.
The `source_excerpt`, trigger function, classification, and coverage columns
distinguish literal product copy from passed-through bodies, IDs, protocol
tokens, and diagnostics. The
`human_composed` rows need their producer inspected before translating them.
The `actionable_refusal_candidate` class remains a wire compatibility review;
the table does not assert that a pending row has nine-language copy.

The CLI catalog groups live under `cmd/clawdline/cli_catalogs/<group>/`.
Each has English source keys; translated catalogs with an extra key, broken
format directive, or invalid JSON fall back as a whole to English. A missing
secondary-language key falls back to its English text. English and `zh-Hant`
must be complete. `core` contains the language-option help line, while `verify`
has 39 fixed messages, `item` has 176, and `task` has 102 initial keys. The remaining fixed CLI sites in the
matrix still need catalog entries and wiring. The `coverage` column records
411 covered, 114 intentionally preserved, and 1,197 pending source/call sites;
these counts are source locations, not distinct messages.

## Wire-field structural scan

Run `tools/heavy.sh go run ./cmd/clawdline/testdata/localization_wire_scan.go .`
from the repository root. The Go AST scan walks production files in `cmd/**`
and `internal/**` and identifies catalog lookups used as map or index keys,
HTTP header or query names, URL/path parts, or endpoint fields. On the combined
candidate it found 360 catalog lookup calls and zero structural wire risks.
The scanner records every call site and its containing expression; review the
full output when adding a new catalog group. The remaining computed strings
were inspected at their producers: `readTextFrom` uses translated words only
in human error labels; `itemWrote` has fixed operation-code map keys and
translated receipt values; `productcopy.Format` feeds notification titles and
bodies; `scheduleNotice` feeds person-facing push bodies. CLI flags' names,
request paths, JSON keys, error codes, stored field names, and raw `--json`
outputs remain literal protocol values. The scan detects direct structural
use; review intermediate variables such as `body`, `line`, `owner`, and `what`
at each call site because a later assignment could otherwise carry a
translated value into a protocol field.

The first translated notification group is the five schedule notice codes in
`internal/app/schedule_notifications.go`. Its producer passes only a safe
filename or refusal code. Agent-authored title/body in `AgentNotify` and
`MachineNotify`, and persisted Board, document, and Session content, remain
unaltered. HTTP refusal `error` and `detail` are currently stable English wire
fields; changing either would break existing clients, so a localized human
field requires a separate schema addition and consumers. This is a remaining
implementation item, not a completed translation claim.
