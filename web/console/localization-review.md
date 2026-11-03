# Reviewing the actual localized console

This opt-in QA harness builds the candidate's real React entry points and CSS. It replaces only external API dependencies with explicitly synthetic responses. It does not contact a running daemon, change pairing, read tokens, install packages, call paid services, or edit the candidate repository. Authored sample titles and text deliberately remain untranslated.

Use Node with TypeScript stripping, the candidate's existing Vite/React dependencies, OpenSSL, and an already installed Playwright module and browser. No dependency is downloaded. Browser launch must work before treating a review as verified. On macOS, a sandbox can refuse Chromium's MachPort registration; run the authorized browser command with the required execution permission rather than interpreting that failure as a product failure.

## Start an owned fixture server

Run from the checkout containing these QA files:

```sh
node web/console/tools/localization-review.mjs --serve \
  --repo /path/to/candidate \
  --expect-commit <full-candidate-commit> \
  --builtins \
  --out /path/to/task/work/localization-review \
  --publish /path/to/task/artifacts/review-site \
  --manifest /path/to/task/artifacts/localization-review-manifest.json
```

The server binds an App HTTP origin and a separate Cloud HTTPS origin to `127.0.0.1` with dynamically allocated ports. The manifest contains their exact URLs, the owning PID, output directory, catalog hashes, and unsupported protocol surfaces. The Cloud origin uses a two-day, task-owned self-signed certificate; manual browser review must accept that local certificate. The certificate is never installed into a trust store. HTTPS is necessary because the real Cloud declaration rejects HTTP and mismatched origins. CloudGate and AdminGate are built unchanged, including the locale boot and authentication code. The fixture API grants only synthetic viewer sessions. Browser-generated device keys remain in that fixture origin's isolated browser storage.

Both builds run through the candidate's `tools/heavy.sh`. The child build invocation carries all configuration in explicit arguments, including when queued; the persistent servers release the heavy lease after building. The build uses the console and input-bar documents (`index.html`, `bar.html`) and the existing React plugin with `configFile: false`, avoiding temporary configuration output in the read-only candidate. Outputs and certificates belong under the task directory, never the source tree. The candidate must be clean, must match `--expect-commit` when provided, and must remain unchanged through the build. The manifest records its exact commit. Use `--publish` to copy the immutable site to artifacts before publishing the manifest; the running server then reads the published copy, which survives removal of task work. Keep each published directory unique to its build.

Stop only the exact owning PID printed in the manifest:

```sh
kill -TERM <manifest-pid>
```

Do not remove the served output directory while reviewers use the server. Archive needed evidence before the task system removes temporary work. Restarting produces new ports and a new manifest. A new candidate can be selected by changing `--repo` and `--expect-commit`; production sources and personal paths are not embedded in these QA files.

To restart only the fixture process while preserving the already built origins, first stop its exact PID, then add `--reuse /path/to/previous-manifest.json` to the same command. Use the same published directory and manifest destination. This mode skips compilation, verifies the recorded catalog hashes, rebinds the recorded loopback ports, and retains the built candidate commit even if the candidate source has moved on. Never restart a fixture while a matrix or reviewer is using it. A build for a new candidate must omit `--reuse`.

## Run the browser matrix

```sh
tools/heavy.sh node web/console/src/localization-review.e2e.ts \
  --repo /path/to/candidate \
  --manifest /path/to/task/artifacts/localization-review-manifest.json \
  --out /path/to/task/artifacts/screens \
  --playwright /path/to/installed/playwright \
  --webcrypto experimental \
  --locales en,zh-Hant --widths 390,1280,320
```

Omit `--playwright` to resolve an existing normal `playwright` dependency from the candidate. The equivalent environment variable is `REVIEW_PLAYWRIGHT_MODULE`. Other options have `REVIEW_REPO`, `REVIEW_MANIFEST`, `REVIEW_OUTPUT`, `REVIEW_LOCALES`, `REVIEW_WIDTHS`, `REVIEW_SURFACES`, `REVIEW_BROWSER`, `REVIEW_REPEAT`, and `REVIEW_WEBCRYPTO` equivalents. `--browser webkit` selects an already installed WebKit; Chromium is the default. Older installed Chromium builds require `--webcrypto experimental` to enable the browser's native Ed25519 implementation; without it, the real Cloud session creation can fail before POST. This flag does not replace WebCrypto or bypass authentication. Omit it when the installed browser supports Ed25519 by default. `--repeat 10` repeats selected cells without retries. One Node test owns the browser lifecycle and writes each cell's independent outcome; a failed cell remains failed.

The default locale matrix is `en,zh-Hant,ja,zh-Hans,ko,es,pt-BR,fr,de`. The default widths are phone 390, desktop 1280, and stress 320 pixels. `--surfaces sessions,settings,admin-overview` limits a smoke run. Unknown locales, states, surfaces, or non-loopback manifests fail rather than silently falling through.

Each cell first selects the language through the actual Settings window language control on that cell's own origin and waits for the resulting reload. It checks saved preference and actual catalog activation. Hosted boot reads the preference saved by that real control on its separate origin, without storage injection, text replacement, or constructed UI. Full-catalog checks reject missing keys even when production would legitimately use English fallback. Render checks require zero reported fallback keys, no visible catalog key IDs, no uncaught page exception, and no document horizontal overflow. These checks do not judge translation quality.

Every cell saves a PNG and JSON with visible text, headings, runtime errors, URLs, language, missing-key count, viewport/document width, control rectangles, horizontal escape, center-hit occlusion, and intersecting text-line candidates. Cells that reach target readiness also save rendered HTML. Per-context synthetic request logs identify unsupported routes (404/405), including background reads. `report.json` records results and protocol gaps. Geometry candidates need visual review: intended layering, scrolling, and disabled controls can produce candidates; a clean rectangle list is not proof of no overlap. Screenshots do not prove every below-the-fold control was exercised.

Use `--fault reject-catalog --locales zh-Hant --surfaces settings` for a deliberate invalid-catalog dependency response; the actual locale boot must fall back and the positive matrix must fail. `--fault key-id --locales zh-Hant --surfaces settings` supplies a validly shaped catalog whose Settings heading is its key ID; the visible-key assertion must fail. These controls alter only synthetic catalog responses, never production source or rendered markup. Keep their evidence in separate output directories and never count them as language acceptance.

## Independent reviewer workflow

### Loaded Project integration preview and Session Git diff

Run `--surfaces project-unify-preview,session-git-diff --locales en,zh-Hant --widths 390,1280,320`
for the focused 12-cell review. The Project cell opens the real Project settings and its
integration preview, requiring two planned actions, the synthetic skill and the `@AGENTS.md`
edit. The Session cell opens the real actions menu, its additional commands, the Git panel,
and the changed README, requiring the returned patch text. Neither cell applies, commits or pushes.

The synthetic GET allowlist now includes `/v1/projects/review-project/unify`,
`/v1/sessions/review-session/git`, and `/v1/sessions/review-session/git/diff?path=README.md`.
Their bodies follow `ProjectUnifyPlan`, `GitReply` and `GitDiffReply` in the generated contract,
and the wrapping/query conventions in `internal/transport/http/project_unify.go` and `git.go`.
The plan contains rules, a skill link, exact before/after text and a conflict; Git contains a
changed file and an unstaged unified patch. Missing diff paths return 400; unchanged paths
return 404. Writes still pass through the existing mutation refusal before these GET handlers.

Focused result JSON retains actual response bodies in `readings`, including exact patch text,
and requires successful consumer GETs in the actor-scoped route log. Existing DOM, PNG, locale,
runtime-error and viewport checks also apply. Use `--fault unify-empty --surfaces project-unify-preview`
to remove actions, skills and conflicts from the dependency response: the cell must fail with
`Expected synthetic unify actions`. `--fault git-empty --surfaces session-git-diff` removes
changed files and must prevent opening the expected README. These are dependency-level negative
controls, not changes to the candidate, DOM or renderer. They do not count as accepted cells.

When another review owns a dirty candidate checkout, do not build its current files or change
its process. Export the exact candidate with `git archive` into task-owned temporary storage,
use existing dependencies read-only, and build that exported source through `tools/heavy.sh`.
Archive builds have no Git metadata: record the archive commit and catalog hashes explicitly
in a synthetic reuse manifest, and bind new owned origins. The manifest is build provenance
supplied by the operator, not independent proof from the reuse mode. Never represent an archive
as a clean working-tree check, or reuse another reviewer's occupied ports. Preserve sources,
commands, generated contract checks and hashes with the evidence before temporary storage is removed.

Keep one server running and share its manifest with the coordinating local reviewer sessions. Each reviewer uses a separate browser context and selects their language through website Settings on the App origin and the Admin language control on the Cloud origin; App and Cloud have separate browser preferences. They then visit stable App fragments or Cloud routes below. State is cookie-scoped; independent contexts do not alter one another. Set a state by visiting `<origin>/__review/state?state=empty`, then return to the desired page. A direct URL can carry `?review_state=signed-out` before its fragment. The manifest includes stable URLs. `/__review/requests` lists synthetic route requests and states. Unknown API routes return `review_route_unsupported`; product mutations return `review_mutation_unsupported`, never invented successful writes. Synthetic authentication POST and the explicitly bounded recovery-device DELETE described below are the only supported product-protocol writes.

| Surface | App address or Cloud route | Matrix ID |
| --- | --- | --- |
| Sessions list and primary navigation | App `/` | `sessions` |
| Open navigation and Settings selection | App `/`, menu, Settings, reopen menu | `navigation` |
| Session detail and composer | App `/#session=review-session` | `session-detail-composer` |
| Actual transcript checklist | Same Session address | `session-plan-card` |
| Browser Settings | App `/#page=settings` | `settings` |
| Embedded machine Settings | App `/#page=settings`, machine tab | `machine-settings` |
| Expanded browser language menu | App `/#page=settings`, open language control | `machine-language-menu` |
| Voice settings with complete synthetic language/setup contracts | App `/#page=settings`, voice tab | `machine-voice` |
| Expanded Cloud pairing instructions | Cloud `/`, pairing state, open pairing | `cloud-pairing-open` |
| Unknown delivery action after synthetic write refusal | App Session, submit authored sample | `send-unknown` |
| Workflow | App `/#page=workflow` | `workflow` |
| Workflow editor | Same address, open the authored Workflow | `workflow-editor` |
| Notifications | App `/#page=notifications` | `notifications` |
| Board | App `/#page=work` | `work` |
| Board creation modal | Same address, choose Create item | `board-modal` |
| Projects | App `/#page=projects` | `projects` |
| Project file contents | Same address, Project settings gear, Files, README.md | `project-files` |
| Plan and redemption entry | App `/#page=plan` | `plan` |
| Documents | App `/#page=documents` | `documents` |
| Document detail | Manifest `urls.document` | `document-detail` |
| Role selection | App `/`, Start Session, choose an available role | `role-picker` |
| Squad roster and role detail | App `/#page=squad`, choose a role | `squad`, `squad-role-detail` |
| Skill detail | Same Squad role, open its first Skill | `squad-skill-detail` |
| Empty, failed, offline dependencies | App `/`, selected fixture state | `sessions-empty`, `sessions-error`, `sessions-offline` |
| Cloud sign-in, pairing, error | Cloud `/`, selected fixture state | `cloud-sign-in`, `cloud-pairing`, `cloud-error` |
| Admin overview, accounts, promotions, usage | Cloud `/admin/<route>` | `admin-overview`, `admin-accounts`, `admin-promotions`, `admin-usage` |
| Admin authentication and refusal states | Cloud `/admin` | `admin-signed-out`, `admin-forbidden`, `admin-failed` |
| Actual demo toggle and stale refresh | Cloud `/admin` | `admin-demo`, `admin-stale` |
| Admin navigation drawer or desktop sidebar | Cloud `/admin`, open menu on narrow screens | `admin-navigation` |

Supported fixture states are `ready`, `empty`, `failed`, `offline`, `signed-out`, `forbidden`, `pairing`, `stale`, `usage-partial`, `device-limit`, `device-list-failed`, `device-ticket-expired`, and `device-revoke-expired`. These name dependency responses, not fabricated React states. For example, offline currently means synthetic 503 responses; it does not simulate browser network loss. Stale keeps the first confirmed overview and fails its refresh. Demo is reached through the production toggle, not injected page markup.

## Explicit acceptance boundaries

This is local browser review support, not nine-language approval or deployed-origin acceptance. The coordinating root owns catalog edits, final language review, Cloud protocol acceptance, deployment, and release.

The default role/Skill data is explicitly authored sample content, including unchanged English names under both supported name-map fields. It is not evidence for built-in role translations. To review the candidate's built-in metadata, pass `--fixtures /path/to/exported-builtins.json` at server startup. That JSON must contain `squadCatalog` in the existing `GET /v1/squad/catalog` response shape (`catalog_version`, `definitions`, `teams`, `skills`). Optional `personas` is the array from `GET /v1/personas`; when absent it is projected from catalog definitions without translating their names or summaries. The harness derives synthetic empty-scope settings and stores the exact fixture data and digest beside the published site. Export only built-in public data from the intended candidate, not personal overrides or the running daemon. This explicit data seam avoids parsing Go source and lets the root swap in a new nine-language candidate without changing the harness.

Candidates that ship `web/console/src/builtin-display-source.json` can use `--builtins` instead of an external export. This reads the candidate's public English/Traditional Chinese display pairs, already checked against domain metadata by `check-builtin-display.mjs`, and reads raw authored Markdown from `internal/domain/persona/catalog/<id>.md` and `internal/domain/squad/builtin_skills/<id>.md`. The real candidate renderer then selects the appropriate translated catalog entry through its built-in metadata resolver. No translated names are inserted after rendering. IDs come from the structured JSON resource and must be safe filename segments; no Go tables or arbitrary source are parsed.

This mode deliberately uses synthetic icons and versions, puts every role into one team, and attaches all built-in Skills to every role so each can be opened during review. Those relationships and the raw Markdown (including its source frontmatter) are fixture data, not proof of actual runtime bindings or injected-prompt formatting. The manifest names that boundary. Use `--fixtures` with an exact domain API export when those relationships themselves need review. Neither mode uses authored sample names as built-in translation evidence.

The real shared Cloud console and machine selector are not yet reachable in this fixture: `CloudViewerSession.connect()` requires an account content key, authenticated relay challenge, and encrypted inventories before CloudGate draws them. Existing write-path unit seams accept transport objects but are not a browser injection point. This harness stops at the genuine pairing gate. A complete extension must implement a synthetic signed/encrypted relay peer using the existing wire protocol, with task-owned keys and no production component changes. Do not substitute a local App screenshot for shared Cloud acceptance.

Page-shell capture alone does not establish loaded-data or modal coverage. The named deep cells actually open Workflow editor, Board creation modal, Projects file viewer, transcript plan card, document detail, role selection, and Squad role/Skill details through the real interface. Inspect their screenshots and route logs. Document share remains unavailable on a local App as production intends. Promotion management has an empty synthetic campaign response; issuance/revocation and redemption submission are not exercised. Unsupported endpoints are reported explicitly. Native shell settings controls render their normal browser-unavailable state; an actual native shell bridge is outside this fixture. External billing/OAuth links are blocked by the automated browser, and are never followed as part of this review.

The focused regression surfaces assert that compact language menus remain within the viewport, all five phone Settings tabs occupy readable text columns, and the unknown-delivery action stays on one line. Voice responses include the full language and setup contracts with explicitly synthetic executable/model paths; no microphone, installation or download is exercised.


## Loaded-data preflight additions

`usage-ready`, `usage-usage-partial`, `usage-empty`, `usage-failed`, and `usage-offline` open the actual `/#page=usage` renderer. The synthetic wire response follows `api/v1/usage-overview.schema.json` and the HTTP handler's 7/28-day query contract. Both periods contain daily token/task/duration observations, all four cache categories, project allocation and options. The partial state includes missing daily values, reduced transcript coverage and truncated task samples. The ready action selects a project and 28 days, switches the metric and uses the chart's actual keyboard interaction. It asserts both charts and the cache composition loaded. `--fault usage-empty --surfaces usage-ready` removes the composition through a dependency response as a negative control; its loaded-data assertion must fail.

Cloud `device-limit`, `device-list-failed`, `device-ticket-expired`, and `device-revoke-expired` use the real CloudGate and DeviceLimitPanel. The session POST returns the real `409 device_limit_reached` details shape; recovery GET and DELETE keep the original browser authentication code and native Ed25519 behavior. All devices are explicitly synthetic. The oldest device, synthetic current browser, recently used confirmation, failed list and expired ticket are reachable. `device-recent-confirm` opens the actual confirmation without sending DELETE. `device-remove-oldest` deletes only a known synthetic ID, records the result and reaches the real pairing screen after the gate retries session creation. `device-revoke-expired` returns the real expired-ticket code on DELETE, keeping the list and sign-in action visible.

Recovery mutation state is actor-scoped, process-local and limited to 256 test contexts. A review actor cookie is required. Unknown device IDs, methods and paths remain refusals. The mutation does not call, relay to, or modify a real account. Request logs retain query parameters, synthetic status and confirmed removal result. A new browser context resets the synthetic recovery scenario; a context that removed a device must not be reused as evidence for the initial full state.

`board-detail` and `board-edit` open the existing synthetic Issue item and its edit form without submitting product mutations. Its title, description and step are authored in Taiwan Traditional Chinese, including when reviewing English. `dispatch-policy` selects the real last Settings tab and expands the read-only base rules; both sections contain labeled synthetic content. Capacity and diagnostics responses are based on generated contract interfaces, with one measured capacity row rather than a successful empty shell. Product settings, policy saves, Board writes and diagnostic report submission remain unsupported writes.

For an older candidate lacking `builtin-display-source.json`, `--builtins --builtin-display /path/to/public-display.json` explicitly accepts an archived public display resource. Export it from a known commit, retain it with evidence and name that source in the review report. The manifest says this is external display data; it does not claim the older candidate implements the newer localization resolver. Candidate Markdown remains read from `--repo`; icons, versions and team/Skill relationships retain their documented synthetic boundary.

These additions are local QA environment coverage. They do not establish production Cloud behavior, deployed-origin acceptance, semantic translation quality, or final nine-language approval. Run English and Traditional Chinese at 390, 1280 and 320 pixels first, then rebuild a complete nine-language candidate for independent review.

`capacity-loaded` opens the actual Status tab and expands the measured row. `machine-remote` selects the actual Remote tab and waits for the Cloud status response. `devices-loaded` opens the local machine card and checks the synthetic diagnostics platform; the local signed-in list is a separate synthetic daemon contract, not the Cloud recovery-device list. Successful usage cells retain an additional `-duration.png` screenshot after scrolling the real page to the duration/composition region, then return to the heading for the primary image.
