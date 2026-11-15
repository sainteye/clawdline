# Session Pop (`com.clawdline.session-pop-art`)

An installable View Plugin v1 package that reads every authorized Session as a page of a
Pop Art comic. Names: **Session Pop** / **Session 普普風**. Version 0.1.0.

## Concept

- A yellow masthead with a rotating Ben-Day sun, a headline burst that counts the state most
  in need of attention (waiting for you, then waiting for a Session, then working), and one
  tally chip per state that jumps to that state's part of the page.
- One continuous comic page. Each state opens with a cover panel (its pattern and the count),
  followed by one panel per Session. Every state has its own word, colour **and** pattern:
  dots, speed stripes, fine dots, bars, black dots, checks, diagonal hatching or cross-hatching.
  The state word is always printed in the speech bubble, so colour is never the only signal.
  Sound-effect words (“HEY!”, “WHIRR”) are decoration and hidden from assistive technology.
- Choosing a panel opens a zoomed detail panel (side column on desktop, bottom sheet on
  phones) with the title, state, Project and reading freshness. Only its explicit
  **Open Session** button calls `view.sessions.open(ref)`. Rows that are stale, have unknown
  freshness, or come from a disconnected reading are drawn as faded, dashed “old news” and
  their button stays disabled with the reason. One attempt per click; errors, including an
  uncertain `request_timeout`, are shown by code in the person's language and never retried.
- A **List** mode shows the same rows as a plain readable list. Titles and Project labels are
  always set with `textContent`. Null Projects read “No Project”. Long text wraps; panels
  clamp titles visually while the detail and accessible name keep the full text.
- Escape closes the detail first, then calls `view.close()`. The host's own Back control
  stays available below the frame. Light and dark host themes are both supported.

## Motion

The paper's halftone drifts, the sun turns, working panels scroll speed lines, the headline
burst breathes, and “waiting for you” sound effects nudge occasionally. Panels pop in on first
reading, a state change shows a one-shot “POW!”, and panels tilt in 3D under the pointer.
Nothing flashes, and motion never navigates or sends anything.

All motion stops when the host reports `reducedMotion` or `visible: false`; the composition is
complete without it. A **Motion** toggle can also stop it; it is disabled (and labelled
“Reduced motion”) while the host requests reduced motion, so it never overrides the host.
Snapshot updates are reconciled per row: unchanged panels are not rebuilt or moved, so focus
and selection survive live updates.

## Package and import

```sh
python3 tools/pack-view-plugin.py plugins/session-pop-art web/console/public/view-plugins/session-pop-art.zip
```

The unchanged packer inlines the SDK at `<!-- CLAWDLINE_SDK -->` and writes a deterministic
two-file STORE ZIP (39247 bytes, under the 262144-byte limit; repeated packing is
byte-identical). Import it from **Sidebar → View plugins → Import ZIP**; no host rebuild is
needed. The curated **Explore views → Install Pop** button fetches this same ZIP
and opens the existing package review, without a native file chooser.

## Verification

A task-local Playwright harness (not part of this repository) bundled the production
`view-plugins/Host` manager, registry and opaque `sandbox="allow-scripts"` frame with esbuild,
served it on `127.0.0.1` with two synthetic machines, imported this ZIP through the manager's
file input, and drove the frame. It passed in Chromium and WebKit at 390 px and 1440 px, in
English and Taiwan Traditional Chinese (8 of 8 runs), checking: import and first opening;
8 rows across two machines with a colliding Session key; hostile titles rendered as literal text
with no `img` and no dialog; state words; host reduced motion keeps the page still and disables
the toggle; 44 px plugin controls; no horizontal page scroll; live snapshot replacement keeps the
focused panel; stale and unknown-freshness rows refuse opening; Escape closes the detail without
closing the view; the explicit Open routes the colliding key to the correct machine; after
reopening, a disconnected machine's rows turn stale and refuse while the healthy machine stays
openable; List mode; host return; empty reading; 40 Sessions without truncation; active motion
changes pixels and the toggle freezes them; the plugin's Close calls `view.close()`; no page
errors and no extra requests. Screenshots and a short motion recording were inspected by hand.
This is fixture evidence only, not Cloud, daemon-console or device acceptance.
