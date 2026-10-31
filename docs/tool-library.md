# Session tool library

The Session actions menu opens a machine-private project library. Saved projects
are stored in the machine SQLite database, shared by its paired readers. Writes
require Send permission or the machine credential. This is a collection of source
metadata; it never establishes that a tool is installed or enabled.

The compiled recommendation snapshot contains 27 public projects, including four
web libraries, 46 verified Skill entry paths and ten categories. English and Taiwan Traditional Chinese
names, purposes, requirements and limitations accompany commit and entry SHA-256
metadata. Historical ranking evidence retains its original kind, date and scope.
Bundled icons distinguish official project assets from owner avatars; custom
sources use initials pending source inspection. Recommendation updates require a
reviewed product change, not runtime third-party execution.

## Use

1. Open a live Session's actions menu and choose Tool library.
2. Search Saved projects or Recommended; inspect purpose, category, source,
   revision, license, Skill mode, prerequisites and historical evidence.
3. Save a project or add an HTTPS GitHub repository. Custom sources may name an
   exact SKILL.md path; otherwise the receiving agent must discover real entries.
4. Choose Prepare and use, a Skill, optional command and target, and a concrete
   task. Select temporary task setup or installation into the current Project.
5. Preparing adds a visible, removable attachment to the existing composer. It
   preserves previous text and survives a browser reload in that machine's draft.
   Review the instructions and send normally. Preparation sends nothing itself.
6. The receiving agent inspects existing tools, pins and reads the source and its
   references, completes permitted reversible setup, checks availability, then
   performs the task. It reports commands, versions, paths and any blockers.

## Web libraries

The Web animation & interaction category contains [Lenis](https://github.com/darkroomengineering/lenis),
[GSAP](https://github.com/greensock/GSAP), [Vanta.js](https://github.com/tengbao/vanta)
and [React Bits](https://github.com/DavidHDev/react-bits). Their reviewed trees have
no SKILL.md. They use `integration_kind: library`, an empty Skill list and bilingual
project prerequisites. Their cards and details identify a web library rather than
offering Skill discovery. Source evidence is official documentation, not a ranking.

Library preparation includes the prerequisites and license in the visible prompt.
The receiving agent reads the pinned README, selected source and package manifest,
reviews installation scripts, then installs a compatible package or copies only the
required component. It records the installed package version separately from the
reviewed commit. Task scope uses an isolated example; Project scope integrates into
the application. Verification includes the build and relevant browser behavior,
cleanup, reduced motion, mobile use and rendering fallbacks. It does not install
the upstream documentation website or enable an optional MCP server.

Lenis and Vanta use MIT licenses. GSAP uses its
[custom standard license](https://gsap.com/standard-license/), with restrictions
on competing visual animation builders. React Bits uses
[MIT plus Commons Clause](https://github.com/DavidHDev/react-bits/blob/4dc7b5b7772e1217c8fb8d2a7aa074302180e5b4/LICENSE.md),
with restrictions on redistributing the components, tools and exports themselves.
Catalog inclusion is metadata review, not dependency review, installation or proof
that the library fits a particular product. Applicable terms must be checked for
the intended use. Only icon assets are bundled; library runtime code is not shipped
with Clawdline. The bundled React Bits icon retains its upstream notice in
`web/console/public/tool-library/react-bits-LICENSE.txt`.

The prompt grants no extra permission for credentials, protected system access,
paid APIs, destructive changes, publication or contacting others. It preserves
Project rules and the selected workflow. Native hot reload is never assumed.
Full security audits retain their explicit-request and isolation requirements.
A preparation receipt does not prove installed, loaded or applied behavior.

## Boundaries and retry behavior

`internal/domain/toollibrary` owns immutable catalog metadata, validation and
bilingual preparation text. The store owns the saved collection and monotonically
increasing version. HTTP owns authorization, size checks, idempotent receipts and
live conversation admission. The console owns presentation and the explicit
composer attachment; the existing pending-send path owns message delivery.

GET /v1/tool-library returns a version, saved projects and compiled catalog.
POST on the same path saves one project or removes one id using expected_version.
POST /v1/tool-library/prepare resolves a selected source and checks the exact live
Session/conversation and, when supplied, execution generation. All writes require
Idempotency-Key; uncertain retries reuse the same key, changed payloads do not.
A version conflict requires reloading the collection. Transient store errors are
retryable, not saved as completed operations.

Cloud carries these operations through tools.catalog, tools.save and tools.prepare
under the paired machine identity. It adds no separate installation service or
account-wide collection. An older machine without these capabilities refuses the
operation. Attachments bind machine, terminal, conversation and execution; changed
identity blocks submission rather than redirecting the prepared task.

The capacity register owns saved-project count, project/request bytes and retry
key length. Tests cover persistence, optimistic conflicts, idempotent replay,
read-only denial, stale conversations, catalog pins, setup prompts, draft restore
and Cloud route transport. UI acceptance uses synthetic Sessions and a temporary
store; it must never proxy test writes into a person's running daemon.

## Repeat the isolated browser check

From an isolated checkout, run `CLAWDLINE_TOOL_PREVIEW=1 go test
./internal/transport/http -run TestToolLibraryBrowserFixture -v -count=1
-timeout 16m` in one terminal. In another, run Vite from `web/console` with
`CLAWDLINE_NEXT_ORIGIN=http://127.0.0.1:8877 npm run dev -- --host 127.0.0.1
--port 5287 --strictPort`. Open `/tools/fixtures/tool-library.html` at that
Vite origin; `?lang=en` selects English, otherwise the fixture uses Traditional
Chinese. This fixture renders the actual Session Detail, library and composer.
Its API wrapper routes only library operations and send to the real handler;
every terminal and other read is synthetic. No local provider is discovered.

After sending, GET the fixture's `/v1/fixture-result` to inspect the synthetic
terminal's received text. GET `/v1/fixture-stop` to stop the fixture, and stop
the Vite process. Do not substitute the production daemon origin for this test.
