import { T, fill } from "./i18n.js";
import { els } from "./dom.js";

/* ==========================================================================
   2. Small helpers
   ========================================================================== */

/** A path the width of a list row. The tail is the part that identifies the project,
 *  so the head is what gets dropped, not the other way round. */
/**
 * What `Transcript.askPayload` puts in front of a question's text.
 *
 * Every other tool's `text` is one line summarising what it was asked to do. A question's
 * arguments *are* its content — the sentence and the options — so that one entry carries them
 * as data, behind a marker no transcript can contain. See the note on `Transcript.askMarker`.
 *
 * The page unpacks it rather than the server, because what the page needs is the *structure*:
 * one row per option, legible on its own, with the number the terminal's picker answers to. A
 * string flattened at the other end would have to be taken apart again here to get that back.
 *
 * Up here rather than next to `parseAsk`, which is the only thing that reads it: the fixtures
 * further down build a question out of it, and they run while the file is still being read.
 */
export var ASK_MARK = "\u0001ask\u0001";

export function shortPath(p) {
    if (!p) return "";
    var s = p.replace(/^\/Users\/[^/]+/, "~").replace(/^\/home\/[^/]+/, "~");
    var parts = s.split("/");
    if (parts.length > 4) s = "…/" + parts.slice(-3).join("/");
    return s;
}

export function clockOf(unix) {
    if (!unix) return "";
    var d = new Date(unix * 1000);
    var age = Date.now() / 1000 - unix;
    if (age < 60) return T.webJustNow;
    if (age < 3600) return fill(T.webMinutesAgo, { n: Math.round(age / 60) });
    var h = d.getHours(), m = d.getMinutes();
    return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m;
}

/**
 * A project's colour, pulled a third of the way towards the text colour.
 *
 * A mark in the accent and a title in the accent are two different claims: the mark says which
 * project this is, and the accent says you are needed. Only one row is allowed to say the second
 * thing, so every tint steps back from full strength — the projects stay told apart, and the one
 * waiting row is still the only thing on the page at full volume.
 */
export function tint(hex) {
    var m = /^#([0-9a-fA-F]{6})$/.exec(hex || "");
    if (!m) return "";
    var n = parseInt(m[1], 16);
    var ink = [0xe8, 0xe6, 0xe3], mix = 0.32;
    var rgb = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map(function (c, i) {
        return Math.round(c + (ink[i] - c) * mix);
    });
    return "rgb(" + rgb.join(",") + ")";
}

export function uuid() {
    if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
    // Enough of a UUID for an Idempotency-Key: it only has to be unrepeated.
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function (c) {
        var r = Math.random() * 16 | 0;
        return (c === "x" ? r : (r & 0x3 | 0x8)).toString(16);
    });
}

var toastTimer = null;
export function toast(text, bad) {
    els.toast.textContent = text;
    els.toast.className = "toast" + (bad ? " err" : "");
    els.toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { els.toast.hidden = true; }, 3200);
}
