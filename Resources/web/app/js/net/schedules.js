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
var projectBySchedule = {};

function projectLabel(path) {
    var parts = String(path || "").replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || path || "";
}

/** The list route deliberately carries no task-template fields. Read each valid row through the
 *  existing detail route so the renderer can say where it will run without widening the ambient
 *  list response. A missing/vanished detail is local to that row: the summary is still truthful
 *  and must not disappear because one follow-up read lost a race with an edit or delete. */
export function loadScheduleProjects(schedules, readSchedule, readPlaces) {
    var list = schedules || [];
    if (typeof readSchedule !== "function") return Promise.resolve(list);
    var details = Promise.all(list.map(function (schedule) {
        if (!schedule || !schedule.id || schedule.state === "invalid") {
            return Promise.resolve(schedule);
        }
        return Promise.resolve().then(function () {
            return readSchedule(schedule.id);
        }).then(function (data) {
            var task = data && data.schedule && data.schedule.task;
            var project = task && task.project_dir;
            return typeof project === "string" && project
                ? Object.assign({}, schedule, { project_dir: project }) : schedule;
        }).catch(function () { return schedule; });
    }));
    var places = typeof readPlaces === "function"
        ? Promise.resolve().then(readPlaces).then(function (data) {
            return (data && data.places) || [];
        }).catch(function () { return []; })
        : Promise.resolve([]);
    return Promise.all([details, places]).then(function (answer) {
        return answer[0].map(function (schedule) {
            var path = schedule && schedule.project_dir;
            if (!path) return schedule;
            var place = answer[1].filter(function (candidate) {
                return candidate && candidate.path === path;
            })[0];
            return Object.assign({}, schedule, { project: {
                path: path,
                label: (place && place.label) || projectLabel(path),
                icon: (place && place.icon) || null
            } });
        });
    });
}

function refresh() {
    if (inFlight || !S.arrived || S.locked || S.conn === "locked"
        || !api || typeof api.schedules !== "function") return;
    inFlight = true;
    api.schedules().then(function (data) {
        var schedules = (data && data.schedules) || [];
        var at = data && data.at;
        // Do not make the useful list wait for its supplementary labels. The second render changes
        // only metadata after all detail reads have settled, successfully or otherwise. Keep the
        // previous label during later minute refreshes so a stable row does not visibly blink.
        renderSchedules(schedules.map(function (schedule) {
            var project = schedule && projectBySchedule[schedule.id];
            return project ? Object.assign({}, schedule, { project: project }) : schedule;
        }), at);
        return loadScheduleProjects(schedules,
            typeof api.schedule === "function" ? api.schedule.bind(api) : null,
            typeof api.places === "function" ? api.places.bind(api) : null)
            .then(function (withProjects) {
                withProjects.forEach(function (schedule) {
                    if (schedule && schedule.id && schedule.project) {
                        projectBySchedule[schedule.id] = schedule.project;
                    }
                });
                renderSchedules(withProjects.map(function (schedule) {
                    var project = schedule && projectBySchedule[schedule.id];
                    return project && !schedule.project
                        ? Object.assign({}, schedule, { project: project }) : schedule;
                }), at);
            });
    }).catch(function () {
        // Read-only and ambient: connection state already has a visible home in the header.
        //
        // **Nothing is rendered from here, and that is the point.** A refusal — the Cloud
        // transport's `cloud_read_unavailable` and `cloud_schedules_unpublished`, or a dropped
        // request on the direct path — means the inventory is unknown, and `renderSchedules([])`
        // would be the page saying "there are none" on its behalf. Not drawing keeps the section
        // as it was: absent before any answer, and holding the last truthful list after one. An
        // inventory that really is empty still arrives as an answer and still draws, which is the
        // difference a person can see.
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
