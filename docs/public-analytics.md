# Public website visits in operations

The hosted `/admin` overview shows public website traffic above its existing account,
promotion and usage panels. It uses a separate superuser-only Cloud API contract and does
not send Session contents or machine project files to analytics.

A browser page view is one arrival at a public page. Anonymous arrivals have no persistent
visitor identifier and must never be labeled as unique people. Signed-in accounts are resolved
by the API's revocable cookie session. The member history contains account name/identifier,
public page path, first receipt time and active time; signed-out visits remain anonymous.

The report shows browser page views, signed-in accounts, average active time, hourly line charts,
public URL rows, source/campaign/device distributions, HTML request country and User-Agent
estimates, and paginated signed-in history. URL and member pagination are independent. HTML
requests and browser page views cannot be added. User-Agent classification does not establish
an AI or crawler's real identity. Country is an estimate from the website edge, not a claim
about a member's residence.

The four summary number cards are replaced by hourly UTC line charts for browser page
views, distinct signed-in accounts, average active time, and HTML requests. Hourly account
counts deduplicate within each hour and must not be summed into a period-wide account count.
Average active time divides that hour's active milliseconds by its browser page views; an
hour without page views has no average and breaks the line. Pointer inspection, arrow-key
inspection, and a table expose exact timestamps and values. The first/current hours are partial.

The report adds `hourly` rows (`hour`, `views`, `requests`, `member_accounts`, `active_ms`)
without changing its existing daily/totals contract. Only hours from collection start through
the current hour are zero-filled; future hours and history before collection remain absent.
The reader accepts at most 2,160 hourly rows (90 days), validates chronological consecutive
hours, counts and totals, and shows an unavailable message if an older API omits the series.

The default is today, with 7-, 30- and 90-day options. All windows use UTC and include today's partial day. A previous comparison
is absent unless it is fully covered and retained. Data is retained for 90 days. Days before
collection began are absent, not zero. Empty, loading, unavailable, malformed, timed-out and
permission-denied responses remain distinct from a verified zero.

The report is read from `/su/v1/analytics/overview`, using schema
`clawdline.public-analytics.v1`, cookie credentials and no-store caching. The response reader
bounds days, page sizes, countries, path lengths, active duration and read time; those limits
are registered under `console.public_analytics_*`. The browser collector and separate edge
HTML collector live with the private Cloud/website implementation and have their own bounded
contracts. The website exposes English and Taiwan Traditional Chinese disclosures and an
opt-out control, and honors DNT/GPC. This feature does not enable Google Analytics or make
external geolocation calls.

Ship the compatible Cloud API/migration before the hosted console and website, then verify
the actual served versions, public-page collection, account association, ordinary-account
refusal, privacy signals and separate units. Local fixtures verify rendering and behavior,
not production deployment. A daemon dev installation or public Release is not required.
