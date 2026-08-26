import { api } from "./api.js";
import { S } from "../core/state.js";
import { renderSchedules } from "../view/schedules.js";

/* --------------------------------------------------------------------------
   Schedules move on a wall clock, not on the session stream's breath. Wait for
   the first session answer as proof authentication has settled, fetch once, then
   stay on a deliberately slow one-minute lane. A failed refresh keeps the last
   truthful answer on screen; a failed first read claims nothing and draws nothing.
   -------------------------------------------------------------------------- */

var started = false;
var inFlight = false;

function refresh() {
    if (inFlight || !S.arrived || S.locked || S.conn === "locked"
        || !api || typeof api.schedules !== "function") return;
    inFlight = true;
    api.schedules().then(function (data) {
        renderSchedules((data && data.schedules) || [], data && data.at);
    }).catch(function () {
        // Read-only and ambient: connection state already has a visible home in the header.
    }).then(function () { inFlight = false; });
}

function beginWhenAuthed(attempt) {
    if (!S.arrived) {
        if (S.locked || S.conn === "locked" || attempt >= 40) return;
        setTimeout(function () { beginWhenAuthed(attempt + 1); }, 250);
        return;
    }
    refresh();
}

export var Schedules = {
    /** Read again now, rather than at the next tick of the slow lane. Making one is the single
     *  moment somebody is watching this list, and a minute of nothing after the sheet closes
     *  reads as a schedule that did not get made. A read already in the air was started before
     *  the new file existed, so its answer cannot contain the new row — let it land first. */
    refresh: function (tries) {
        var n = tries || 0;
        if (inFlight && n < 10) {
            setTimeout(function () { Schedules.refresh(n + 1); }, 300);
            return;
        }
        refresh();
    },
    start: function () {
        if (started) return;
        started = true;
        beginWhenAuthed(0);
        // Also owns recovery after a later pairing: the bounded fast wait above stops making
        // noise at the door, while this slow lane notices the first authenticated session frame.
        setInterval(refresh, 60000);
    }
};
