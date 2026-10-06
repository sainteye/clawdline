# Product localization

English is the source language and the fallback for Clawdline's product copy. A
translated interface must use a complete, validated catalog. User, project, and
agent-authored content is data and stays in the language in which it was written.
Protocol names, JSON fields, error codes, and machine-readable CLI output stay
stable. The retired Swift archive and its byte-for-byte console copies stay
read-only.

## Shipped interface languages

The shipped set is `en`, `zh-Hant` (Taiwan Traditional Chinese), `ja`,
`zh-Hans`, `ko`, `es`, `pt-BR`, `fr`, and `de`. A language is advertised only
after every product-copy key, placeholder, and plural form has passed catalog
validation. Voice recognition has a separate language list; a voice language
does not imply an interface translation.

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
| `zh-Hant`, `zh-Hant-TW`, `zh-TW`, `zh-HK` | `zh-Hant` |
| `zh-Hans`, `zh-Hans-CN`, `zh-CN`, `zh-SG` | `zh-Hans` |
| ambiguous `zh` | `en` |
| `ja-JP` | `ja` |
| `pt`, `pt-PT`, `pt-BR` | `pt-BR` |
| `es-MX`, `fr-CA`, `de-AT`, `ko-KR` | `es`, `fr`, `de`, `ko` |
| unsupported or malformed preference | `en` |

The local HTML document starts with `lang=en` and stays hidden by its boot
class until the browser has selected and applied a complete catalog. The
embedded English catalog may avoid a request for English. Other selections
must be fetched before the first visible render. Cloud uses the same resolver
and bundle catalog as the local console. Both set `lang` and `dir` to the
catalog actually displayed; neither guesses the document language from an
unfulfilled preference.

`GET /v1/strings` without `lang` returns English. With a supported tag it
returns that complete catalog. A syntactically invalid tag is a 400 refusal;
an unknown well-formed tag or an unusable selected catalog returns the English
catalog with `lang=en`. Browser-local preference is not inferred by the daemon
for this API; the client passes its resolved tag explicitly.

## Atomic fallback

Validate the whole catalog before applying any value. A missing file, invalid
JSON, absent or empty key, changed placeholder, or invalid plural form rejects
the whole selected catalog. Fetch and parse failures on Cloud, and embedded
catalog failure on the local page, lead to the same English fallback. The
console remains usable and announces `lang=en`; it must never combine labels
from two languages while claiming one `lang`. If the optional English file is
unavailable, the built-in English copy remains the browser's final fallback.
The server should report its own English catalog failure as a typed error
rather than claim to have served a translation.

## Delivery checks

The Console and daemon/CLI inventories must each enumerate the source of every
product-generated visible sentence and identify protocol text excluded from
translation. Every shipped language needs local and Cloud core-flow evidence.
Test the failure cases above on local HTML, `/v1/strings`, and the Cloud bundle.
Production acceptance requires the delivered commit in `BUILD.json`, a served
main bundle passing `CloudGate`, and a readable, validated static catalog for
every advertised language at the path the deployed Cloud configuration uses.
