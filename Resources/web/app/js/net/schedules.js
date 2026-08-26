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
    start: function () {
        if (started) return;
        started = true;
        beginWhenAuthed(0);
        // Also owns recovery after a later pairing: the bounded fast wait above stops making
        // noise at the door, while this slow lane notices the first authenticated session frame.
        setInterval(refresh, 60000);
    }
};
