import { reduced } from "./env.js";
import { createVisibleInterval } from "./visibility.js";

/* ==========================================================================
   3. Pixels
   Two things on this page are drawn rather than typeset: the project marks and
   the spinner. Both come out of the app, and both are pixel art — so both are
   sized in whole device pixels and drawn with smoothing off, or a retina screen
   turns them into a smudge.
   ========================================================================== */

function dpr() { return Math.max(1, Math.min(3, window.devicePixelRatio || 1)); }

// Project images share one bounded renderer across legacy and React views.
export { drawIcon, drawIconOnce } from "./project-icon.js";

export { ASSISTANT_LOGOS, assistantLogo, assistantName } from "./assistant-logos.js";

/**
 * The bar's spinner, ported cell for cell: eight cells around a three-by-three ring with the
 * middle empty, one step every eighth of a second so a full turn takes a second, and a
 * three-cell tail behind the head. Dim on purpose — a session that is working wants nothing
 * from you, and a list where five rows all flash is a list nobody can read.
 */
var SPIN = {
    cell: 3, gap: 1.5, step: 125,
    ring: [[0, 0], [1, 0], [2, 0], [2, 1], [2, 2], [1, 2], [0, 2], [0, 1]],
    tail: [[3, 0.12], [2, 0.24], [1, 0.45], [0, 0.90]],
    colour: "154, 151, 143"   // --dim: the bar draws this in the accent, the web keeps the accent for waiting
};

export function drawSpinner(canvas, phase) {
    var ratio = dpr();
    var cell = Math.max(1, Math.round(SPIN.cell * ratio));
    var gap = Math.max(1, Math.round(SPIN.gap * ratio));
    var size = cell * 3 + gap * 2;
    if (canvas.width !== size) {
        canvas.width = canvas.height = size;
        canvas.style.width = canvas.style.height = (size / ratio) + "px";
    }
    var g = canvas.getContext("2d");
    g.clearRect(0, 0, size, size);
    // Tail first, so the head lands on top of any rounding overlap.
    for (var i = 0; i < SPIN.tail.length; i++) {
        var back = SPIN.tail[i][0], alpha = SPIN.tail[i][1];
        var at = SPIN.ring[((phase - back) % 8 + 8) % 8];
        g.fillStyle = "rgba(" + SPIN.colour + "," + alpha + ")";
        g.fillRect(at[0] * (cell + gap), at[1] * (cell + gap), cell, cell);
    }
}

// One clock for every spinner on the page. Eight separate timers would drift apart and the
// list would look like eight machines rather than one.
export var spinPhase = 0;
export var spinners = [];        // the list's own, thrown away and rebuilt by every render
// Transcript echoes are redrawn independently of the session list, but turn on the same clock.
// Keeping their canvases separate prevents an unrelated list render from silently stopping them.
export var optimisticSpinners = [];
// The one outside the list: the band that waits for a started session to appear. It is kept on
// its own because no render owns it, and it rides this clock rather than starting a second one
// — two spinners on one screen turning at their own speeds is the thing this note is about.
export var bandSpin = null;
// These two live in sheets rather than in the list. They are named separately because each
// sheet redraw owns its canvas, but they still take their phase from the same clock as every
// row: opening or closing a session should look like part of this page, not a tiny second app.
export var startSpin = null;
export var confirmSpin = null;
/// The composer's own spinner. Kept out of `spinners`, which the list throws away and rebuilds
/// on every render — this one belongs to the pane and outlives that.
var liveSpin = null;
/// The one beside the dictation counter, while the Mac reads back what was recorded. On this
/// clock and not one of its own for the reason above: a transcription that starts while a
/// session is working puts two arcs on the same screen, and two arcs turning at their own
/// speeds is what makes a page look like two pages.
var voiceSpin = null;
/// The say-what-to-start sheet, while the Mac plans from a sentence. On this clock too, and for
/// the same reason as the one above it: this arc turns for about five seconds beside a line of
/// text, and it is the only thing on that sheet saying the wait is a wait rather than a stop.
export var commandSpin = null;
/// Whether the list's own spinners can be seen at all. The list owns that answer (a phone reading
/// a transcript has the list behind it, `visibility: hidden`), so it hands it over with the list.
var spinnersShown = null;

/**
 * A canvas that has left the document is a registration nobody will clear: its owner redrew and
 * forgot it, or the node went with its row. Such canvases are dropped rather than drawn.
 */
function attached(canvas) { return !!canvas && canvas.isConnected !== false; }

/** One turn of the clock. Exported only so a test can turn it by hand. */
export function turnSpinners() {
    if (reduced) return 0;
    spinners = spinners.filter(attached);
    optimisticSpinners = optimisticSpinners.filter(attached);
    if (!attached(bandSpin)) bandSpin = null;
    if (!attached(startSpin)) startSpin = null;
    if (!attached(confirmSpin)) confirmSpin = null;
    if (!attached(liveSpin)) liveSpin = null;
    if (!attached(voiceSpin)) voiceSpin = null;
    if (!attached(commandSpin)) commandSpin = null;
    var list = spinnersShown && !spinnersShown() ? [] : spinners;
    if (!list.length && !optimisticSpinners.length && !bandSpin && !startSpin && !confirmSpin &&
        !liveSpin && !voiceSpin && !commandSpin) return 0;
    spinPhase = (spinPhase + 1) % 8;
    var drawn = 0;
    function draw(canvas) { drawSpinner(canvas, spinPhase); drawn += 1; }
    list.forEach(draw);
    optimisticSpinners.forEach(draw);
    [bandSpin, startSpin, confirmSpin, liveSpin, voiceSpin, commandSpin].forEach(function (canvas) {
        if (canvas) draw(canvas);
    });
    return drawn;
}

/// The clock itself, which stops while the page is hidden — nothing turns on a screen nobody can
/// see — and does not replay missed turns when it comes back: a spinner has nothing to catch up.
export var spinnerClock = createVisibleInterval(turnSpinners, SPIN.step, { catchUp: false });
spinnerClock.start();

// Eight of the handles above are set from somewhere else — the render or the sheet that owns the
// canvas — and a module cannot assign to a name it imported. So the variable stays here where the
// clock can see it, and the write becomes a call.
export function setSpinners(list, shown) {
    spinners = list;
    if (typeof shown === "function") spinnersShown = shown;
}
export function setOptimisticSpinners(list) { optimisticSpinners = list; }
export function setBandSpin(canvas) { bandSpin = canvas; }
export function setStartSpin(canvas) { startSpin = canvas; }
export function setConfirmSpin(canvas) { confirmSpin = canvas; }
export function setVoiceSpin(canvas) { voiceSpin = canvas; }
export function setCommandSpin(canvas) { commandSpin = canvas; }
