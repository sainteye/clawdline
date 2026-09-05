/* ==========================================================================
   A mark for a project nobody drew one for

   `core/pixels.js:drawIcon` collapses the canvas to 0×0 when a session has no
   icon — deliberately, so a CSS placeholder can win in the list, where
   `.row .mark.none` is a small grey box. The header has no such placeholder,
   and now that the mark is a *button* the missing box is not a missing
   picture: it is a shortcut with nothing to press.

   So rather than a placeholder, one is drawn. The rule was the user's, taken
   on 2026-09-05: **when a project has no icon, generate one.** Not a neutral
   glyph repeated on every unregistered project, which would say "no icon" over
   and over — a mark derived from the project's own path, so two sessions in
   two unregistered projects still look like two projects.

   What it is: the same shape as a real mark — four rows of seven cells, a
   foreground on a dark ground, `"#RRGGBB"` or null per cell, an `accent` for
   the title tint — so nothing downstream can tell a generated mark from a
   registered one. It is mirrored about its middle column because that is what
   makes sixteen coin flips read as an emblem rather than as noise, and its
   density is held inside a band because an empty mark is invisible and a full
   one is a rectangle. Both of those are the same failure the drawn placeholder
   had: a picture that identifies nothing.

   **Nothing here reads `document`, `window` or `localStorage`.** It is a pure
   function of a string, which is what lets `Tests/web-snippets.mjs` import it
   into a bare Node process and check the mark a given path produces.

   It is not a replacement for `~/.claude/project-icons.json`. A registered
   icon always wins: this is only what a project has until somebody draws it
   one, and `renderDetailHead` asks for it only when `session.icon` is absent.
   ========================================================================== */

var ROWS = 4;
var COLS = 7;
/** The mirror halves the cells that have to be decided: columns 0–3, reflected into 4–6. */
var HALF = 4;

/** FNV-1a over the string's code units. Small, stable, and — the only property that matters
 *  here — the same on every machine and every browser, so a project keeps its mark. */
function hash32(text) {
    var h = 0x811c9dc5;
    for (var i = 0; i < text.length; i++) {
        h = (h ^ text.charCodeAt(i)) >>> 0;
        h = Math.imul(h, 0x01000193) >>> 0;
    }
    return h >>> 0;
}

function hex2(n) {
    var s = Math.max(0, Math.min(255, Math.round(n))).toString(16);
    return s.length < 2 ? "0" + s : s;
}

/** HSL to `"#RRGGBB"`. Hue in degrees, saturation and lightness in 0–1. */
function hsl(hue, saturation, lightness) {
    var h = ((hue % 360) + 360) % 360 / 60;
    var c = (1 - Math.abs(2 * lightness - 1)) * saturation;
    var x = c * (1 - Math.abs(h % 2 - 1));
    var m = lightness - c / 2;
    var rgb = h < 1 ? [c, x, 0] : h < 2 ? [x, c, 0] : h < 3 ? [0, c, x]
        : h < 4 ? [0, x, c] : h < 5 ? [x, 0, c] : [c, 0, x];
    return "#" + hex2((rgb[0] + m) * 255) + hex2((rgb[1] + m) * 255) + hex2((rgb[2] + m) * 255);
}

/**
 * The mark a project path gets when the registry has none for it.
 *
 * `null` for an empty key, because there is a real difference between "this project has no
 * icon" and "there is no project" — no session open must still draw nothing, exactly as it
 * does today, rather than a mark standing for the empty string.
 */
export function generatedMark(key) {
    var path = typeof key === "string" ? key.replace(/\/+$/, "") : "";
    if (!path) return null;
    var h = hash32(path);
    var bits = h & 0xffff;                       // sixteen cells: four rows of the mirrored half
    var hue = (h >>> 16) % 360;

    var lit = [];
    var on = 0;
    for (var i = 0; i < ROWS * HALF; i++) {
        var set = !!((bits >>> i) & 1);
        lit.push(set);
        if (set) on += 1;
    }
    // Two failures the drawn placeholder also had, and the reason the density is not left to
    // chance: about one path in two thousand hashes to fewer than three lit cells, which is a
    // button with nearly nothing on it, and about as many hash to a solid block, which is a
    // button that looks like every other solid block. `step` is odd and 16 is a power of two,
    // so walking by it visits all sixteen cells before repeating one.
    var step = 2 * (h % 8) + 1;
    var order = [];
    for (var j = 0; j < ROWS * HALF; j++) order.push((j * step + (h >>> 8)) % (ROWS * HALF));
    for (var up = 0; on < 5 && up < order.length; up++) {
        if (!lit[order[up]]) { lit[order[up]] = true; on += 1; }
    }
    for (var down = order.length - 1; on > 12 && down >= 0; down--) {
        if (lit[order[down]]) { lit[order[down]] = false; on -= 1; }
    }

    var ink = hsl(hue, 0.52, 0.64);
    var ground = hsl(hue, 0.34, 0.20);
    var cells = [];
    for (var y = 0; y < ROWS; y++) {
        var row = [];
        for (var x = 0; x < COLS; x++) {
            // Mirrored about column 3: 4 reflects 2, 5 reflects 1, 6 reflects 0.
            var source = x <= HALF - 1 ? x : COLS - 1 - x;
            row.push(lit[y * HALF + source] ? ink : ground);
        }
        cells.push(row);
    }
    return { accent: ink, cells: cells, generated: true };
}

/**
 * The icon to draw for a session: the registered one, or one made up from the project it sits
 * in. One function so that every caller who wants "the mark for this session" asks the same
 * question, and so the fallback cannot drift away from what the header actually draws.
 */
export function markForSession(session) {
    if (!session || typeof session !== "object") return null;
    if (session.icon && session.icon.cells && session.icon.cells.length) return session.icon;
    return generatedMark(session.cwd);
}

/**
 * What to call the project on a button, before the Mac has said anything about it.
 *
 * The tail of the path, which is the part that identifies a project — the same choice
 * `shortPath` makes about which end to keep. **This is a label and never a scope key.** The
 * sheet's own heading uses the `project.label` the Mac answers beside the list, because that
 * one has been through the registry match, the subdirectory prefix and the worktree fold; this
 * is only what the header can say about a session it is already showing.
 */
export function projectLabel(key) {
    var parts = String(key || "").replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || "";
}
