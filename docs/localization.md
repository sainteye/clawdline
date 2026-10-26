# Product localization

English is the source language and the fallback for Clawdline's product copy. A
translated interface must use a validated catalog. User, project, and
agent-authored content is data and stays in the language in which it was written.
The product name **Playbook** stays in English in the Taiwan Traditional Chinese
interface, including tabs, dialogs, actions, and explanatory copy. Surrounding
copy remains localized; saved Playbook names and messages remain authored data.
Protocol names, JSON fields, error codes, and machine-readable CLI output stay
stable. The retired Swift archive stays read-only, and the console stylesheets
copied from it stay pinned byte for byte (`web/console/src/legacy/README.md`).

The first frozen catalog baseline must be complete in all nine languages.
Later keys may use the fallback policy below even if they reach the first
multilingual deployment after that baseline was frozen. For later code
changes, maintain English and Taiwan Traditional
Chinese in the same change. The other seven languages can be updated in
batches. A missing new key in one of those seven uses its English sentence and
is counted as untranslated; an existing translated key must still have valid
placeholders, plural forms, and safe markup. A key absent from the current
English catalog invalidates the selected catalog. Ordinary development and
release checks report coverage but do not require those seven catalogs to be
updated with every code change. A translation catch-up uses the strict
nine-language check again. Guides use the same policy at the section level: an
outdated translated section must yield to the current English section rather
than show obsolete commands. Guide freshness pins, pending sections, and the
initial `--require-current` gate are maintained in
`skills/clawdline/TRANSLATIONS.md`.

`web/console/public/catalogs/baseline-keys.json` records the keys shipped in
the first complete release. New keys go into English and Taiwan Traditional
Chinese without changing that baseline, so the other seven catalogs may omit
them. When removing a baseline key, remove it from English and every translated
catalog in the same change, remove it from the baseline list, increment the
baseline's integer version, and run the strict catalog check for the affected
catalogs. This explicit cleanup prevents obsolete keys from invalidating whole
catalogs at runtime. The maintenance procedure is in
`web/console/catalog-baseline.md`.

CLI catalog groups pin their first-release keys in
`cmd/clawdline/cli_catalogs/<group>/baseline-keys.json`. Runtime loading
rejects a secondary catalog missing one of those keys. English and
Traditional Chinese must include every current key, while a later command's
new keys may be absent from the other seven locales and then use the exact
English sentence. A new command group begins with English and Traditional
Chinese; its baseline is pinned when its first complete nine-language
translation is released. Raw flags, paths, identifiers, and JSON stay as they
were supplied.

## Shipped interface languages

The shipped set is `en`, `zh-Hant` (Taiwan Traditional Chinese), `ja`,
`zh-Hans`, `ko`, `es`, `pt-BR`, `fr`, and `de`. The frozen initial baseline must
pass complete catalog validation. Later changes may leave secondary-language
new keys untranslated under the fallback rule above. Voice recognition has a
separate language list; a voice language does not imply an interface translation.

## Separate preferences

| Preference | Stored where | Default | Changes |
| --- | --- | --- | --- |
| `ui_language` | This browser origin | `auto` | This browser's fixed console copy and document language; in the macOS App's local console, its native menus, status and alerts too |
| `language` | Daemon settings | Existing behavior | New agent response and Board authoring instruction; voice `auto` fallback |
| `voice_language` | Daemon settings | Existing behavior | Dictation language |
| `product_language` | Daemon settings | `en` | Daemon notifications and human-readable CLI output |

The browser preference is local to its origin. A choice at the hosted Cloud
origin does not silently change a local console, another browser, an agent, or
dictation. The settings UI must identify the browser-local control and preserve
an unsupported pre-existing daemon `language` value, with a visible explanation
and a way to choose a shipped interface language.

The macOS App's native shell mirrors the catalog actually displayed by its
local console, including its separate settings window. Both local WebViews
reload after a language change so their browser copy stays in sync. A validated
main-frame message from the App's loopback console selects native copy; the
Cloud tab has no language bridge and its hosted-origin `ui_language` remains
independent. The shell keeps a local mirror for its launch menus before the
console loads. This mirror does not change daemon `product_language`, agent
authoring language, or dictation.

For the console, an explicit shipped `ui_language` wins. `auto` uses
`navigator.languages` in order, followed by `navigator.language`, then `en`.
Script subtags outrank regional inference. The same case table must pass in the
browser resolver and any Go resolver that names the same product catalog:

| Input | Catalog |
| --- | --- |
| no usable preference, `en-US` | `en` |
| `zh-Hant`, `zh-Hant-TW`, `zh-Hant-CN`, `zh-TW`, `zh-HK`, `zh-MO` | `zh-Hant` |
| `zh-Hans`, `zh-Hans-CN`, `zh-Hans-TW`, `zh-CN`, `zh-SG` | `zh-Hans` |
| `zh-Latn-TW` or another explicit unknown script | `en` |
| legacy setting `zh_MO` after underscore normalization | `zh-Hant` |
| ambiguous `zh` | `en` |
| `ja-JP` | `ja` |
| `pt`, `pt-PT`, `pt-BR` | `pt-BR` |
| `es-MX`, `fr-CA`, `de-AT`, `ko-KR` | `es`, `fr`, `de`, `ko` |
| unsupported or malformed preference | `en` |

The local HTML document starts with `lang=en` and stays hidden by its boot
class until the browser has selected and applied a validated catalog. The
embedded English catalog may avoid a request for English. Other selections
must be fetched before the first visible render. Cloud uses the same resolver
and bundle catalog as the local console. Both set `lang` and `dir` to the
catalog actually displayed; neither guesses the document language from an
unfulfilled preference.

The standalone Cloud operations page uses the same browser preference and
validated catalog as the console. It keeps its boot cover until the selected
catalog is active, including when no machine is paired. Operations, Workflow,
notifications, dispatch rules, and transcript plan headings use catalog keys;
their English and Traditional Chinese source tables are checked against the
public catalogs. User-edited Workflow messages and dispatch rules remain data.

`GET /v1/strings` without `lang` returns English. With a supported tag it
returns that catalog, which may omit new keys after the initial complete
release. The client fills missing secondary-language keys from English. A
syntactically invalid tag is a 400 refusal; an unknown well-formed tag or an
unusable selected catalog returns the English catalog with `lang=en`.
Browser-local preference is not inferred by the daemon for this API; the
client passes its resolved tag explicitly.

## Atomic fallback

Validate every supplied value before applying the selected catalog. A missing
file, invalid JSON, empty value, changed placeholder, invalid plural form, or
unsafe markup rejects that whole catalog. A missing English or Traditional
Chinese key also rejects it. After the initial complete release, a missing key
in another language is a valid untranslated gap: the client fills just that
key from English and reports the gap count. The document keeps the selected
language tag when a secondary catalog has valid translated values; English
fallback fragments should carry `lang=en` where the rendering surface allows.
Fetch and parse failures on Cloud, and embedded catalog failure on the local
page, lead to whole-catalog English fallback with `lang=en`. The console must
remain usable. If the optional English file is unavailable, the built-in
English copy remains the browser's final fallback. The server should report
its own English catalog failure as a typed error rather than claim to have
served a translation.

## Actionable refusals

Keep the wire status, error code, English detail or message, and existing
metadata unchanged. A producer may add `detail_key` for fixed, authored copy:
beside a flat refusal's `detail`, or inside a nested `error` beside `message`.
The key is an explicit source claim, not a value inferred from the error code
or from matching external text. Dynamic and forwarded raw messages carry no
key, even if their English wording happens to equal a catalog sentence.

The local and Cloud transports must preserve that explicit key through every
refusal envelope. A console may display a translation only when the key exists
in its validated English and selected catalogs. If a secondary catalog lacks a
new key, the English catalog supplies that sentence. Without a usable key, the
original detail remains English. English fragments carry `lang=en` where the
rendering surface allows. Cloud Bridge refusals authored before a local HTTP
route follow the same source and catalog rules. None of this changes the
machine-readable code, status, outcome, or acknowledgement reference.

## Keys the code asks for

A typed source table is not what a person reads. `catalogWord` reads
`public/catalogs/`, and its last fallback is the key's own id, so a key added
to `next-strings.ts` (or the work, verify, settings and bar tables) but not to
the catalogs reaches the screen as `next.cloudAllContentOldVersion` — in every
language, with nothing failing to announce it. The hosted catalog check cannot
see it either: that one validates the other languages against `en.json`, so a
hole in `en.json` is a hole in its own reference. This was shipped: two fleet
sentences were on screen as their key names, and a third served copy two
revisions behind its source.

`web/console/tools/check-source-catalog.mjs` closes it. Every source key must
exist in `en.json` and `zh-Hant.json`, and every English source value must be
the one `en.json` serves; `--write` adds what is missing and resynchronises
English. It runs inside `npm run check`. The other seven languages keep falling
back to English by design and are not checked by it.

## Bundled role and Skill display metadata

Bundled role names and summaries and bundled Skill names and purposes are product copy.
`src/builtin-display-source.json` records their English and Traditional Chinese source values.
`tools/check-builtin-display.mjs`, called by the source-catalog check, compares that manifest and
the primary catalogs with the role Markdown frontmatter and the domain Skill table.

`src/builtin-display.ts` translates only recognized built-in identifiers whose complete English
name and summary/purpose pair matches the recorded source. Unknown identifiers and edited
metadata keep their wire values. The display helper does not rewrite wire objects, instruction
bodies, handbooks, immutable versions or source files. Custom and historical authored content
remains data. The helper may display an unchanged built-in metadata pair in the active language
without changing its stored snapshot.

## Browser acceptance beyond catalog coverage

A complete key set does not prove that a control uses the catalog, that a translation still
matches the current flow, or that an expanded child control fits. Bind the redemption form only
after locale boot; exercise cold navigation as well as in-app arrival. Review the actual labels,
long tab names, menus, dialogs, empty/error states and recovery actions at phone and desktop
widths. A document without horizontal overflow can still contain overlapping or clipped controls.

The opt-in browser harness is documented in `web/console/localization-review.md`. It builds the
actual App and hosted entry points with explicit synthetic dependencies and captures independent
screenshots, rendered text, errors and geometry for each cell. Fixture contract failures are
reported separately from product defects. A successful capture is not language approval.
Synthetic origins do not establish production Cloud acceptance; authenticated relay, account,
pairing and billing boundaries require their own evidence. Keep the candidate commit and catalog
hashes with every review, and recheck only the affected states after a correction.

## Delivery checks

The Console and daemon/CLI inventories must each enumerate the source of every
product-generated visible sentence and identify protocol text excluded from
translation. Every shipped language needs local and Cloud core-flow evidence.
Test the failure cases above on local HTML, `/v1/strings`, and the Cloud bundle.
Production acceptance requires the delivered commit in `BUILD.json`, a served
main bundle passing `CloudGate`, and a readable, validated static catalog for
every advertised language at the path the deployed Cloud configuration uses.
The first multilingual deployment validates all nine catalogs against the
complete frozen baseline and requires full English and Traditional Chinese
parity. Subsequent deployments retain those requirements, validate every
present translation, and report the remaining languages'
coverage without blocking on new untranslated keys.

## English documentation

English READMEs, user pages, design notes and Agent guides use English interface labels and
English examples. Look up the label in the shipped English catalog for that control; a Chinese
label can map to different English words in different contexts. Do not insert a Chinese label
followed by an English gloss, or leave a Chinese update note inside English prose.

Keep non-English text when the document needs its exact bytes: a language-switch link, an
explicitly labeled translated prompt or catalog field, a multilingual trigger phrase, a Unicode
test, a link target, or quoted UI evidence in a localization incident. Explain those examples in
English. Historical documents identified as Chinese in the document index are separate from the
English guide.
