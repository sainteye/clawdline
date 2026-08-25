import { reduced } from "./env.js";

/* ==========================================================================
   3. Pixels
   Two things on this page are drawn rather than typeset: the project marks and
   the spinner. Both come out of the app, and both are pixel art — so both are
   sized in whole device pixels and drawn with smoothing off, or a retina screen
   turns them into a smudge.
   ========================================================================== */

function dpr() { return Math.max(1, Math.min(3, window.devicePixelRatio || 1)); }

/**
 * Draw `icon.cells` — rows of "#RRGGBB" or null — at `cellPx` logical pixels a cell.
 * The backing store is exactly cells × whole device pixels and the CSS size is that
 * divided back down, which is what keeps the edges hard at any scale factor.
 */
export function drawIcon(canvas, icon, cellPx) {
    var cells = icon && icon.cells;
    if (!cells || !cells.length) {
        // The inline sizes go too, not to zero: a row with no mark falls back to a CSS
        // placeholder, and an inline width would win over it.
        canvas.width = canvas.height = 0;
        canvas.style.width = canvas.style.height = "";
        return false;
    }
    var ratio = dpr();
    var unit = Math.max(1, Math.round(cellPx * ratio));
    var rows = cells.length, cols = 0;
    for (var r = 0; r < rows; r++) cols = Math.max(cols, (cells[r] || []).length);
    if (!cols) return false;

    canvas.width = cols * unit;
    canvas.height = rows * unit;
    canvas.style.width = (cols * unit / ratio) + "px";
    canvas.style.height = (rows * unit / ratio) + "px";

    var g = canvas.getContext("2d");
    g.imageSmoothingEnabled = false;
    g.clearRect(0, 0, canvas.width, canvas.height);
    for (var y = 0; y < rows; y++) {
        var row = cells[y] || [];
        for (var x = 0; x < row.length; x++) {
            if (!row[x]) continue;                 // null is transparent, and stays transparent
            g.fillStyle = row[x];
            g.fillRect(x * unit, y * unit, unit, unit);
        }
    }
    return true;
}

/* The same two product marks as AssistantLogo.swift. Kept inline so the list and transcript
   never wait on another request, and so the marks inherit their exact row-sized geometry. */
export var ASSISTANT_LOGOS = {
    claude: {
        colour: "#D97757",
        path: "M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.79-.048-2.698-.073-2.339-.097-2.266-.122-.571-.121L0 11.784l.055-.352.48-.321.686.06 1.52.103 2.278.158 1.652.097 2.449.255h.389l.055-.157-.134-.098-.103-.097-2.358-1.596-2.552-1.688-1.336-.972-.724-.491-.364-.462-.158-1.008.656-.722.881.06.225.061.893.686 1.908 1.476 2.491 1.833.365.304.145-.103.019-.073-.164-.274-1.355-2.446-1.446-2.49-.644-1.032-.17-.619a2.97 2.97 0 01-.104-.729L6.283.134 6.696 0l.996.134.42.364.62 1.414 1.002 2.229 1.555 3.03.456.898.243.832.091.255h.158V9.01l.128-1.706.237-2.095.23-2.695.08-.76.376-.91.747-.492.584.28.48.685-.067.444-.286 1.851-.559 2.903-.364 1.942h.212l.243-.242.985-1.306 1.652-2.064.73-.82.85-.904.547-.431h1.033l.76 1.129-.34 1.166-1.064 1.347-.881 1.142-1.264 1.7-.79 1.36.073.11.188-.02 2.856-.606 1.543-.28 1.841-.315.833.388.091.395-.328.807-1.969.486-2.309.462-3.439.813-.042.03.049.061 1.549.146.662.036h1.622l3.02.225.79.522.474.638-.079.485-1.215.62-1.64-.389-3.829-.91-1.312-.329h-.182v.11l1.093 1.068 2.006 1.81 2.509 2.33.127.578-.322.455-.34-.049-2.205-1.657-.851-.747-1.926-1.62h-.128v.17l.444.649 2.345 3.521.122 1.08-.17.353-.608.213-.668-.122-1.374-1.925-1.415-2.167-1.143-1.943-.14.08-.674 7.254-.316.37-.729.28-.607-.461-.322-.747.322-1.476.389-1.924.315-1.53.286-1.9.17-.632-.012-.042-.14.018-1.434 1.967-2.18 2.945-1.726 1.845-.414.164-.717-.37.067-.662.401-.589 2.388-3.036 1.44-1.882.93-1.086-.006-.158h-.055L4.132 18.56l-1.13.146-.487-.456.061-.746.231-.243 1.908-1.312-.006.006z"
    },
    codex: {
        colour: "#10A37F",
        path: "M9.205 8.658v-2.26c0-.19.072-.333.238-.428l4.543-2.616c.619-.357 1.356-.523 2.117-.523 2.854 0 4.662 2.212 4.662 4.566 0 .167 0 .357-.024.547l-4.71-2.759a.797.797 0 00-.856 0l-5.97 3.473zm10.609 8.8V12.06c0-.333-.143-.57-.429-.737l-5.97-3.473 1.95-1.118a.433.433 0 01.476 0l4.543 2.617c1.309.76 2.189 2.378 2.189 3.948 0 1.808-1.07 3.473-2.76 4.163zM7.802 12.703l-1.95-1.142c-.167-.095-.239-.238-.239-.428V5.899c0-2.545 1.95-4.472 4.591-4.472 1 0 1.927.333 2.712.928L8.23 5.067c-.285.166-.428.404-.428.737v6.898zM12 15.128l-2.795-1.57v-3.33L12 8.658l2.795 1.57v3.33L12 15.128zm1.796 7.23c-1 0-1.927-.332-2.712-.927l4.686-2.712c.285-.166.428-.404.428-.737v-6.898l1.974 1.142c.167.095.238.238.238.428v5.233c0 2.545-1.974 4.472-4.614 4.472zm-5.637-5.303l-4.544-2.617c-1.308-.761-2.188-2.378-2.188-3.948A4.482 4.482 0 014.21 6.327v5.423c0 .333.143.571.428.738l5.947 3.449-1.95 1.118a.432.432 0 01-.476 0zm-.262 3.9c-2.688 0-4.662-2.021-4.662-4.519 0-.19.024-.38.047-.57l4.686 2.71c.286.167.571.167.856 0l5.97-3.448v2.26c0 .19-.07.333-.237.428l-4.543 2.616c-.619.357-1.356.523-2.117.523zm5.899 2.83a5.947 5.947 0 005.827-4.756C22.287 18.339 24 15.84 24 13.296c0-1.665-.713-3.282-1.998-4.448.119-.5.19-.999.19-1.498 0-3.401-2.759-5.947-5.946-5.947-.642 0-1.26.095-1.88.31A5.962 5.962 0 0010.205 0a5.947 5.947 0 00-5.827 4.757C1.713 5.447 0 7.945 0 10.49c0 1.666.713 3.283 1.998 4.448-.119.5-.19 1-.19 1.499 0 3.401 2.759 5.946 5.946 5.946.642 0 1.26-.095 1.88-.309a5.96 5.96 0 004.162 1.713z"
    }
};

export function assistantLogo(kind) {
    var logo = ASSISTANT_LOGOS[kind];
    if (!logo) return "";
    return '<svg class="assistant-logo" viewBox="0 0 24 24" aria-hidden="true" focusable="false">' +
        '<path fill="' + logo.colour + '" fill-rule="evenodd" d="' + logo.path + '"></path></svg>';
}

export function assistantName(kind) {
    return kind === "claude" ? "claude" : kind === "codex" ? "codex" : "assistant";
}

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
setInterval(function () {
    if (reduced || (!spinners.length && !optimisticSpinners.length &&
                    !bandSpin && !startSpin && !confirmSpin && !liveSpin && !voiceSpin &&
                    !commandSpin)) return;
    spinPhase = (spinPhase + 1) % 8;
    for (var i = 0; i < spinners.length; i++) drawSpinner(spinners[i], spinPhase);
    for (var j = 0; j < optimisticSpinners.length; j++) drawSpinner(optimisticSpinners[j], spinPhase);
    if (bandSpin) drawSpinner(bandSpin, spinPhase);
    if (startSpin) drawSpinner(startSpin, spinPhase);
    if (confirmSpin) drawSpinner(confirmSpin, spinPhase);
    if (liveSpin) drawSpinner(liveSpin, spinPhase);
    if (voiceSpin) drawSpinner(voiceSpin, spinPhase);
    if (commandSpin) drawSpinner(commandSpin, spinPhase);
}, SPIN.step);

// Eight of the handles above are set from somewhere else — the render or the sheet that owns the
// canvas — and a module cannot assign to a name it imported. So the variable stays here where the
// clock can see it, and the write becomes a call.
export function setSpinners(list) { spinners = list; }
export function setOptimisticSpinners(list) { optimisticSpinners = list; }
export function setBandSpin(canvas) { bandSpin = canvas; }
export function setStartSpin(canvas) { startSpin = canvas; }
export function setConfirmSpin(canvas) { confirmSpin = canvas; }
export function setLiveSpin(canvas) { liveSpin = canvas; }
export function setVoiceSpin(canvas) { voiceSpin = canvas; }
export function setCommandSpin(canvas) { commandSpin = canvas; }
