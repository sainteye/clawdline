# Session tool library

The Session actions menu opens a machine-private project library. Saved projects
are stored in the machine SQLite database, shared by its paired readers. Writes
require Send permission or the machine credential. This is a collection of source
metadata; it never establishes that a tool is installed or enabled.

The compiled recommendation snapshot contains 37 public projects, including three
web libraries and one evaluation framework, 60 verified Skill entry paths and fourteen categories. English and Taiwan Traditional Chinese
names, purposes, requirements and limitations accompany commit and entry SHA-256
metadata. Historical ranking evidence retains its original kind, date and scope.
Bundled icons distinguish official project assets from owner avatars; custom
sources use initials pending source inspection. Recommendation updates require a
reviewed product change, not runtime third-party execution.

## Use

1. Open a live Session's actions menu and choose Tool library. With no saved
   projects, the initial view browses all recommendations. With saved projects,
   it opens Saved projects. Subsequent refreshes and saves preserve a person's
   selected tab until the dialog closes.
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

The dialog uses 15 px body copy and at least 13 px supporting text. Mobile
layouts retain these sizes and reflow the cards; form fields use 16 px text.

## Categories by intent

Categories follow what the person wants to do. In order: Architecture & domain
modeling; Development & code quality; Interface design direction; Interface
review & polish; Design systems & components; Web animation & interaction;
Creative visuals & 3D; Frontend performance & accessibility; Security review;
Diagrams, slides & documentation; Product, SEO & growth; Video & motion
production; Optional development workflows; Agent evaluation & improvement.
Existing category IDs were kept, so saved projects and links still resolve;
`design-review`, `design-system` and `creative` are new.

- **Interface design direction** (`design`) chooses a visual direction before
  code: Impeccable, Taste Skill, UI UX Pro Max, Anthropic Skills, Hallmark
  and [Interface Design](https://github.com/Dammyjay93/interface-design) for
  product UI that keeps its decisions in `.interface-design/system.md`.
- **Interface review & polish** (`design-review`) audits a screen that exists:
  Jakub Krehel's skills, now including Better Typography, and
  [Refactoring UI](https://github.com/wondelai/skills), whose eight-check score
  out of ten lists what remains. Its upstream affiliate book links are not
  recommendations.
- **Design systems & components** (`design-system`) captures or applies a
  system: [Hue](https://github.com/dominikmartn/hue) writes a design-language
  Skill and may fetch the URL it is given;
  [Stitch's Extract DESIGN.md](https://github.com/google-labs-code/stitch-skills)
  (Apache-2.0) reads frontend source without running the app, and its optional
  push into Google Stitch is not part of preparation;
  [shadcn/ui](https://github.com/shadcn-ui/ui) is only for shadcn/ui projects and
  runs its CLI through the project's package runner.
- **Web animation & interaction** (`animation`) now holds Emil Kowalski's skills
  for UI micro-interactions, [LottieFiles Motion Design](https://github.com/LottieFiles/motion-design-skill)
  for brand and illustration motion, and
  [GSAP's official skills](https://github.com/greensock/gsap-skills) (core,
  timeline, ScrollTrigger and React) beside the web libraries. The skills are
  MIT; the gsap package they install keeps its
  [custom standard license](https://gsap.com/standard-license/), with restrictions
  on competing visual animation builders, and that note is the card's first
  requirement. The gsap-skills card replaced the earlier `greensock--gsap`
  library card: no code, test or console logic referred to that catalog ID, and a
  saved copy is stored with its full body, so it still opens and prepares.
- **Creative visuals & 3D** (`creative`) holds MiniMax's
  [Shader Dev](https://github.com/MiniMax-AI/skills) (36 GLSL techniques with a
  per-pixel loop budget; covered by the root MIT license, while the repository's
  `LICENSES/Apache-2.0.txt` covers other Skills derived from Anthropic work) and
  [Three.js Scroll Worlds](https://github.com/MengTo/Skills), maintained by one
  author with about 3.7 MB of demo files in its Skill folder.
- **Diagrams, slides & documentation** (`diagrams`) adds
  [Frontend Slides](https://github.com/zarazhangrui/frontend-slides): single-file
  HTML decks, PPTX to HTML conversion and PDF export, with no PPTX export. An
  upstream step deploys to a public Vercel URL; its limitation tells the agent
  that publishing needs the person's explicit approval.

Each added source is pinned to a 40-hex commit with the hashed license file and
the SHA-256 and byte count of every entry. Icons are owner avatars. Dependency
closure is not reviewed.

## Web libraries

The Web animation & interaction category contains [Lenis](https://github.com/darkroomengineering/lenis),
[Vanta.js](https://github.com/tengbao/vanta)
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

Lenis and Vanta use MIT licenses. React Bits uses
[MIT plus Commons Clause](https://github.com/DavidHDev/react-bits/blob/4dc7b5b7772e1217c8fb8d2a7aa074302180e5b4/LICENSE.md),
with restrictions on redistributing the components, tools and exports themselves.
Catalog inclusion is metadata review, not dependency review, installation or proof
that the library fits a particular product. Applicable terms must be checked for
the intended use. Only icon assets are bundled; library runtime code is not shipped
with Clawdline. The bundled React Bits icon retains its upstream notice in
`web/console/public/tool-library/react-bits-LICENSE.txt`.

## Evaluation frameworks

The Agent evaluation & improvement category includes [Harness²](https://github.com/google-research/harness2)
at revision `1cfe254e8d25b15f37048c3730134686abe60335`. It uses
`integration_kind: framework`, an empty Skill list, a bundled upstream project
icon and bilingual prerequisites and limitations. The reviewed source has no
SKILL.md. Its Apache-2.0 license does not replace benchmark or dataset terms.
The bundled icon retains the license in
`web/console/public/tool-library/google-research--harness2-LICENSE.txt`.

Framework preparation reads the pinned setup guide and source, defines a bounded
experiment with independent acceptance criteria and preserves baseline artifacts.
It uses an isolated environment and keeps the current machine workflow and
permissions. Upstream Codex disables sandboxing; this preparation does not grant
permission to do that on the user's host. Credential-free checks and demos remain
separate from model-backed experiments, whose executor, editor, selector,
reflection, judge and retry costs all follow existing paid-API approval rules.
A recursion parameter is not a complete request or spending cap. A selected
result is not proof of correctness, transfer or improvement over the baseline.
Inclusion records source review; it does not install or run the framework.

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
