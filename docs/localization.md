# Product localization

English is the source language and the fallback for Clawdline's product copy. A
translated interface must use a validated catalog. User, project, and
agent-authored content is data and stays in the language in which it was written.
Protocol names, JSON fields, error codes, and machine-readable CLI output stay
stable. The retired Swift archive and its byte-for-byte console copies stay
read-only.

The first multilingual release must ship complete catalogs in all nine
languages. For later code changes, maintain English and Taiwan Traditional
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
`zh-Hans`, `ko`, `es`, `pt-BR`, `fr`, and `de`. The initial advertised set must
pass complete catalog validation. Later changes may leave secondary-language
new keys untranslated under the fallback rule above. Voice recognition has a
separate language list; a voice language does not imply an interface translation.

## Separate preferences

| Preference | Stored where | Default | Changes |
| --- | --- | --- | --- |
| `ui_language` | This browser origin | `auto` | This browser's fixed console copy and document language |
| `language` | Daemon settings | Existing behavior | New agent response and Board authoring instruction; voice `auto` fallback |
| `voice_language` | Daemon settings | Existing behavior | Dictation language |
| `product_language` | Daemon settings | `en` | Daemon notifications and human-readable CLI output |

The browser preference is local to its origin. A choice at the hosted Cloud
origin does not silently change a local console, another browser, an agent, or
dictation. The settings UI must identify the browser-local control and preserve
an unsupported pre-existing daemon `language` value, with a visible explanation
and a way to choose a shipped interface language.

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

## Delivery checks

The Console and daemon/CLI inventories must each enumerate the source of every
product-generated visible sentence and identify protocol text excluded from
translation. Every shipped language needs local and Cloud core-flow evidence.
Test the failure cases above on local HTML, `/v1/strings`, and the Cloud bundle.
Production acceptance requires the delivered commit in `BUILD.json`, a served
main bundle passing `CloudGate`, and a readable, validated static catalog for
every advertised language at the path the deployed Cloud configuration uses.
The first multilingual deployment uses the strict nine-language catalog gate.
Subsequent deployments require full English and Traditional Chinese parity,
validate every present translation, and report the remaining languages'
coverage without blocking on new untranslated keys.
