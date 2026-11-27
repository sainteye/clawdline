# Onboarding survey

Clawdline asks three questions in an optional survey: the main reason for using the product,
current AI tools (multiple choice), and discovery source. The whole survey can be skipped, but
Submit needs an answer to all three; a partial answer shows "Please answer all three questions
before submitting." English and Taiwan Traditional Chinese ship
together; other interface languages use the English fallback. The form starts with no choices
selected. A single dark card uses the console's existing background, card, text and warm orange accent
tokens. The survey and statistics selectors share one centred chevron while retaining native
select interaction.

## Entry and consent

The hosted console offers the survey once its account is signed in and the machine picker or
console is available, outside an active pairing or machine confirmation sheet. Accounts that
have submitted or skipped are not prompted again on another device. Existing accounts without
a survey record receive the same one-time invitation after this feature ships.

The local console has no invitation in or below a Session conversation. Its survey is available
only from Settings.
It remembers a random respondent id, display/skip timestamps, answers and unresolved submission
in this browser's storage. It sends nothing to Cloud when displaying or dismissing the invitation.
Only the explicit Submit action uploads answers, the random id, language, entry and console build
stamp. The anonymous endpoint never uses account cookies. Browsers with disabled storage can
still submit, but cannot retain their identity across reloads. Clearing site data creates a new
respondent. This is a browser sample, not a count of people or installations.

Settings → browser preferences → Usage preferences reopens the questionnaire for review or
editing. The Cloud onboarding and Settings entry paths explain the collection before submission. No project path, source
code, terminal text or Session conversation is included. Cloud answers are associated with the
signed-in account; the local random id is hashed by the API and never returned in statistics.
The optional survey does not enable background product telemetry.

## Cloud contract

The private Cloud control-plane repository owns these endpoints and its Mongo adapter:

- `GET /v1/onboarding/survey`: the signed-in account's status, revision and current answers.
- `POST /v1/onboarding/survey`: `shown`, `skip`, or `submit`; requires the viewer session, exact
  application origin and `Idempotency-Key`.
- `POST /v1/onboarding/local`: explicit consent and a random UUID respondent, `submit` only;
  no cookies. CORS is anonymous and isolated to this route and its exact OPTIONS route.
- `GET /su/v1/onboarding/statistics`: superuser-only aggregate data; ordinary callers get 404.

Writes require survey version 1, expected revision, allowlisted answer codes and bounded metadata.
`not_started` is mutually exclusive with other tools. Unknown keys and versions are rejected.
The server dates all records. One document per account/browser retains the first and latest
answers, first exposure/submission, ever-skipped time, last update and a revision. It retains
first/latest snapshots, rather than an unbounded edit history. Compare-and-swap prevents racing
edits; a repeated last request returns its saved result, while changed payloads with that key
are rejected. The client retains an uncertain request for explicit retry; it does not automatically
retry. Revision conflicts require reviewing the server's saved answer before another write.

The dedicated `onboarding_surveys` collection uses Mongo's built-in unique `_id` index only.
It does not modify the attested head-one migration inventory. Writes use majority acknowledgement;
statistics aggregate in the database with a two-second execution budget and return a typed 503
on failure, never fabricated zeroes. Bodies are limited to 4096 bytes. Mutations consume at most
ten attempts per minute per Cloud account or per anonymous source IP, using the existing expiring
attempt-bucket repository. Metadata caps are 35 characters for language and 80 for version;
statistics filters allow at most 366 days. This additive collection is ignored by older API builds,
so reverting the API leaves stored survey records intact without changing the migration head.

## Statistics and interpretation

Operations → Onboarding survey (`/admin/survey`) keeps Cloud and local-browser cohorts separate.
Date filters use first recorded exposure, inclusive `from` and exclusive `until`, in UTC. For local
respondents, the first centrally recorded exposure is their explicit submission. Language, version
and entry filters use the latest answer metadata. Distributions use the latest submitted answers,
with counts, percentages and sample sizes; cross tables show reason × tool and source × reason.
A zero-response cohort has no percentage denominator. Multi-select tool shares can exceed 100%.

Cloud exposure counts unique accounts, not page loads. Ever-skipped and submitted can overlap.
Response rate is submitted accounts / exposed accounts in that cohort. The pairing figure counts
submitted accounts with a currently nonrevoked machine, deduplicated across machines. It does
not establish whether pairing followed the survey or whether the survey caused conversion.

Local exposure/skip/response rate and pairing are unavailable, represented as `null`, because
these events stay in the browser. Submitted local browsers remain measurable. Unanswered surveys
are not assigned inferred motivations. A refresh failure removes the statistics display rather
than drawing stale figures as current. Demo mode does not invent survey data.

## Acceptance

Exercise both languages at desktop and phone widths; select each tool combination, including
not-started exclusivity; submit, retry a lost receipt with the same key, reload, edit in Settings,
open another browser on the same account, skip, and verify the backend's statistics. Cover
unauthorized reads, bad origins, invalid payloads, concurrent edits, unavailable storage/network,
and a failed statistics query. Mongo tests compare the database aggregation to the in-memory
adapter and verify atomic revision updates. Opening the local Settings form must produce no Cloud request until Submit. Production rollout must deploy the additive API before the hosted console.
