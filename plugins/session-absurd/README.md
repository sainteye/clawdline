# Session Deadpan (`com.clawdline.session-absurd`)

An installable View Plugin v1 package that stages every authorized Session as a small,
absurd cartoon scene performed with a straight face. Names: **Session Deadpan** /
**Session 冷面劇場**. Version 0.1.0. It is an independent package beside Session Pop; it
shares no code or bytes with Pop, Galaxy or List and uses the unchanged SDK.

## Concept

The visual direction is modern flat cartooning: pastel blocks, one even outline weight,
blank-faced office characters and situations that do not quite make sense. All drawings are
original inline SVG written for this package; no external art, fonts or network assets.

Each of the eight work states is its own scene, and the scene is the visual metaphor for the
state:

| State | Scene |
|---|---|
| Waiting for you | Holding the telephone out toward you. It has been a while. |
| Waiting for a Session | Timing a snail with a stopwatch. The snail is the other Session. |
| Working | Pedalling a bicycle that keeps a light bulb lit. |
| On hold | A statue holding up a pause sign, with a bird on its head. |
| Ready | Sitting in a bath with a rubber duck, ready when you are. |
| Milestone complete | Standing on a podium; the trophy is a cactus. |
| Work complete | Asleep in a hammock, with a ticked flag. |
| Unknown | A paper bag over the head and a question mark. |

The page reads in state order (what needs the person first). Each state opens with a stage
tile — the large scene, the state word, its count and a one-line deadpan stage direction —
followed by one card per Session. A card carries a small copy of the scene, the full title
(visually clamped to three lines), a state badge with its own glyph (`!`, `⧗`, `↻`, `Ⅱ`, `○`,
`◆`, `✓`, `?`), the Project label or “No Project”, and a dark freshness tag when the row is not
current. Every state also has its own background pattern, so neither colour nor drawing is the
only signal. The stage directions describe the drawing only; they never claim progress,
percentages or dependencies.

Choosing a card opens a close-up (side panel at desktop width, bottom sheet on phones) with
the large scene, title, state, Project and reading freshness. Only its explicit **Open
Session** button calls `view.sessions.open(ref)`. Rows that are stale, have unknown freshness
or come from a disconnected reading are drawn faded and dashed and the button stays disabled
with the localized reason. One attempt per click; errors, including an uncertain
`request_timeout`, are shown by code in the person's language and never retried. **List** mode
shows the same rows as a plain list. Escape closes the close-up first, then calls
`view.close()`; the host's own Back control stays below the frame. Titles and Project labels
are only ever set with `textContent`. Light and dark host themes are supported.

## Motion

Restrained and ornamental: characters blink at staggered times, the bicycle wheel turns, the
bulb glows softly, the phone cord wiggles, the snail crawls, the bird hops, the duck bobs, the
hammock sways, confetti drifts, the paper bag tilts, and stage characters glance slightly
toward the pointer. Cards fade in on the first reading and a card whose state changes drops in
once. Nothing flashes, and motion never selects, sends or navigates.

All motion — CSS, SVG and the pointer glance — stops when the host reports `reducedMotion` or
`visible: false`; the still page keeps every drawing and label. The **Motion** toggle can stop it
further and is disabled (“Reduced motion”) while the host requests reduced motion, so it never
overrides the host. Snapshot updates are reconciled per row, so focus and selection survive.

## Package and installation

```sh
python3 tools/pack-view-plugin.py plugins/session-absurd web/console/public/view-plugins/session-absurd.zip
```

The public packer inlines the SDK and writes a deterministic two-file STORE ZIP (54882
bytes, under the 262144-byte limit; repacking is byte-identical). **Sidebar → View plugins →
Explore views → Install Deadpan** fetches this ZIP and opens the existing package review, without
a native file chooser; afterwards the view opens from the status bar. The same ZIP can also be
imported with **Import ZIP**.

## Verification

`web/console/tools/view-plugins.e2e.mjs` installs it through the curated button in the
production manager, launches it from the status-bar picker, checks literal hostile text, the
unknown-freshness refusal and that an explicit Open routes a colliding Session key to its own
machine. A task-local harness additionally drove the production host with two synthetic
machines in Chromium and WebKit at 390 px and 1440 px in English and Taiwan Traditional
Chinese: all eight state words, 44 px targets, no horizontal scroll, live replacement keeping
focus, stale, refused-open and disconnected cases, list, 40 dense rows with none omitted, empty,
dark theme, active motion versus manual and host-requested stillness (pixel comparison), and no
page errors or extra requests. This is fixture evidence only, not Cloud or device acceptance.

## Cover

The package has its own cover: a blank-faced clerk in a bath holding a telephone out to the
viewer, with a snail waiting on the rim. Its editable source is `cover.svg`. The shared cover
renderer produces a 128 × 128 RGBA8 PNG embedded in the manifest, inside the same two-file ZIP.
Packing, installation and the publication gate all require a valid dedicated cover; neither
this view nor its launcher entry borrows a generic icon.

## Limits

The plugin's own loading stage is visible only until the first reading arrives; the current
host reads before it creates the frame, so in practice the host's loading state is seen first.
