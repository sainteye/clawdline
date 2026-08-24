import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S, optimisticBySession } from "../core/state.js";
import { els } from "../core/dom.js";
import { uuid } from "../core/util.js";
import { render, renderList } from "./list.js";
import { renderTranscript } from "./transcript.js";
import { ActionConfirm } from "../input/action-confirm.js";
import { Start } from "../input/start.js";

/* ---- waiting on the network ---------------------------------------------- */

/**
 * A wait, drawn only when it is a wait worth drawing.
 *
 * Two numbers, and both of them are about not making a glitch out of a fast answer:
 *
 * - **Nothing for the first 150ms.** A transcript that is already on this machine comes back
 *   inside that, and a placeholder that arrives and leaves again in a tenth of a second is read
 *   as a fault rather than as progress — the eye catches a change on screen and then finds
 *   nothing that explains it. Below the threshold the pane simply stays empty, which is what it
 *   was doing before and is over before anybody has looked at it.
 * - **320ms once it is up.** Under about a third of a second a change on screen is a flicker
 *   rather than a state. It costs nothing on the fast path, because the skeleton was never shown
 *   there; the worst case is an answer that arrives at 160ms being held back until 470ms, and
 *   that is the price of it never strobing. Paid on the slow path only, which is the one this is
 *   for in the first place.
 */
function Waiting(onShow, showAfter, minShown) {
    var SHOW_AFTER = typeof showAfter === "number" ? showAfter : 150;
    var MIN_SHOWN = typeof minShown === "number" ? minShown : 320;
    return {
        visible: false,
        shown: 0,
        timer: null,

        start: function () {
            var self = this;
            if (this.visible || this.timer) return;
            this.timer = setTimeout(function () {
                self.timer = null;
                self.visible = true;
                self.shown = Date.now();
                onShow();
            }, SHOW_AFTER);
        },

        /** The answer is in. Take the skeleton down — but not before it has been up long enough
         *  to have been a state — and then draw whatever really goes there. */
        settle: function (then) {
            var self = this;
            clearTimeout(this.timer);
            this.timer = null;
            function finish() { self.visible = false; if (then) then(); }
            if (!this.visible) { finish(); return; }
            var left = MIN_SHOWN - (Date.now() - this.shown);
            if (left <= 0) { finish(); return; }
            setTimeout(finish, left);
        }
    };
}

/**
 * Whether the page still has no idea what sessions there are.
 *
 * Two screens say "there is nothing here" — the list's empty state and the transcript's home
 * screen — and on a reload both of them were true for a moment and then wrong. Nothing had
 * arrived yet, so the list was empty because it is declared empty, and no session was open
 * because the fragment naming one cannot be honoured until that session exists. Each drew its
 * sentence, and each took it away again a few frames later: two flashes on every load.
 *
 * The wait is the test rather than a clock of its own. While it is running — the skeleton up, or
 * the 150ms before it — nobody has said, and neither screen has anything true to draw. The
 * moment it settles, whatever it settles into is the answer: a list, an empty Mac, or a
 * connection that gave up, and all three of those have words already written for them.
 */
export function listUnknown() {
    return !S.arrived && (Waits.list.visible || !!Waits.list.timer);
}

export var Waits = {
    tx: Waiting(function () { renderTranscript(); }),
    list: Waiting(function () { renderList(); }),
    startPress: Waiting(function () { Start.sync(); }),
    end: Waiting(function () { render(); ActionConfirm.sync(); })
};

/**
 * Browser-local turns that have crossed the HTTP boundary but not appeared in the Mac's file.
 *
 * Matching is one-to-one and forward-looking. An identical sentence from five minutes before
 * this send must not make the new one look delivered; a transcript timestamp a few seconds
 * before the browser's clock is allowed because the two machines are the same machine but the
 * request and repaint do not land on the same tick. The ten-minute upper bound is also the
 * lifetime of an echo: after that, keeping a faded promise on screen forever is less honest than
 * letting the next transcript fetch speak for itself.
 */
export var Optimistic = {
    add: function (id, text, imageCount, known) {
        var entry = {
            role: "user", text: text, at: Math.floor(Date.now() / 1000),
            pending: true, imageCount: imageCount || 0, token: uuid(), wait: null,
            known: known || this.known(S.tx.id === id ? S.tx.entries : [])
        };
        entry.wait = Waiting(function () { Optimistic.expire(id, entry.token); }, 10 * 60 * 1000, 0);
        (optimisticBySession[id] || (optimisticBySession[id] = [])).push(entry);
        entry.wait.start();
        renderList();
        return entry;
    },

    entries: function (id) { return optimisticBySession[id] || []; },

    key: function (entry) {
        return String(entry.at || 0) + "\u0001" + String(entry.text == null ? "" : entry.text);
    },

    known: function (entries) {
        var self = this, found = {};
        entries.forEach(function (entry) {
            if (!entry || entry.role !== "user") return;
            var key = self.key(entry);
            found[key] = (found[key] || 0) + 1;
        });
        return found;
    },

    matches: function (pending, actual) {
        if (!actual || actual.role !== "user") return false;
        var at = Number(actual.at || 0);
        if (!at || at < pending.at - 10 || at > pending.at + 10 * 60) return false;
        var text = String(actual.text == null ? "" : actual.text);
        if (!pending.imageCount) return text === pending.text;
        var marks = text.match(/\[Image #\d+\]/g) || [];
        if (marks.length !== pending.imageCount) return false;
        return text.replace(/\[Image #\d+\]\s*/g, "").trim() === pending.text;
    },

    reconcile: function (id, actual) {
        var pending = optimisticBySession[id];
        if (!pending || !pending.length) return false;
        var used = {}, kept = [], matched = false;
        for (var i = 0; i < pending.length; i++) {
            var found = -1;
            for (var j = 0; j < actual.length; j++) {
                if (used[j] || !this.matches(pending[i], actual[j])) continue;
                var key = this.key(actual[j]), occurrence = 0;
                for (var k = 0; k <= j; k++) if (this.key(actual[k]) === key) occurrence += 1;
                if ((pending[i].known[key] || 0) >= occurrence) continue;
                found = j;
                break;
            }
            if (found < 0) { kept.push(pending[i]); continue; }
            used[found] = true;
            matched = true;
            // A later identical local turn must not claim this same real occurrence on the next
            // fetch, after the earlier pending turn has already left the container.
            var matchedKey = this.key(actual[found]), matchedOccurrence = 0;
            for (var n = 0; n <= found; n++) if (this.key(actual[n]) === matchedKey) matchedOccurrence += 1;
            for (var q = i + 1; q < pending.length; q++) {
                pending[q].known[matchedKey] = Math.max(pending[q].known[matchedKey] || 0, matchedOccurrence);
            }
            pending[i].wait.settle();
        }
        if (kept.length) optimisticBySession[id] = kept;
        else delete optimisticBySession[id];
        if (matched) renderList();
        return matched;
    },

    clear: function (id) {
        var pending = optimisticBySession[id] || [];
        for (var i = 0; i < pending.length; i++) pending[i].wait.settle();
        delete optimisticBySession[id];
    },

    clearAll: function () {
        var self = this;
        Object.keys(optimisticBySession).forEach(function (id) { self.clear(id); });
    },

    expire: function (id, token) {
        var pending = optimisticBySession[id] || [];
        var kept = pending.filter(function (entry) { return entry.token !== token; });
        if (kept.length === pending.length) return;
        if (kept.length) optimisticBySession[id] = kept;
        else delete optimisticBySession[id];
        renderList();
        if (S.openId === id && !S.agent) renderTranscript();
    }
};

/** Bars of uneven length, in the transcript's own grid, so the real entries land where these
 *  were rather than somewhere near them. The widths are fixed rather than random: a skeleton
 *  that reshuffles itself between two sessions is a second thing moving on screen. */
export function txSkeleton() {
    var shapes = [[88, 54], [96, 78, 43], [71], [92, 61, 34]];
    return '<div class="skel" role="status" aria-label="' + esc(T.webReading) + '">' +
        shapes.map(function (widths) {
            return '<div class="entry skel-entry"><div class="who"><span class="bar"></span></div>' +
                '<div class="body">' + widths.map(function (w) {
                    return '<span class="bar" style="width:' + w + '%"></span>';
                }).join("") + "</div></div>";
        }).join("") + "</div>";
}

/** The same idea for the list, in the shape of the rows about to replace it. It goes into the
 *  empty state's own element: the rows below it are keyed by session id, and a placeholder with
 *  no session behind it has no business in that collection. */
export function drawListSkeleton() {
    var widths = [[64, 41], [78, 52], [49, 37], [71, 45]];
    els["list-empty"].className = "skel";
    els["list-empty"].hidden = false;
    els["list-empty"].innerHTML = '<div role="status" aria-label="' + esc(T.webLoading) + '">' +
        widths.map(function (pair) {
            return '<div class="skel-row"><span class="bar mark"></span>' +
                '<span class="bar line" style="width:' + pair[0] + '%"></span>' +
                '<span class="bar sub" style="width:' + pair[1] + '%"></span></div>';
        }).join("") + "</div>";
}
