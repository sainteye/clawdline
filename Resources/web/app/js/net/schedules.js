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

/** Where each row will run, taken from the list when the list says, and read per row only when it
 *  does not.
 *
 *  **A read that is skipped is the only read that is reliably fast.** The list route used to carry
 *  no task-template field on the principle that the ambient response stays narrow, and this
 *  function paid for that with one detail read per valid row — on a refresh that runs every
 *  minute. Measured on the Cloud path: eight machine-scoped reads a minute from one open tab,
 *  6,358 in a day, 74 MB, and the answers byte-identical hundreds of times over. They are not free
 *  merely because they are small: they enter the Mac's single background read lane, and a
 *  transcript somebody has just opened queues behind all of them.
 *
 *  So `project_dir` now comes down with the row. The per-row read stays for a Mac that does not
 *  send it yet, which is the only case that still costs anything. A missing or vanished detail
 *  remains local to its row: the summary is still truthful and must not disappear because one
 *  follow-up read lost a race with an edit or delete. */
export function loadScheduleProjects(schedules, readSchedule, readPlaces) {
    var list = schedules || [];
    var details = Promise.all(list.map(function (schedule) {
        var carried = schedule && schedule.project_dir;
        if (typeof carried === "string" && carried) return Promise.resolve(schedule);
        if (typeof readSchedule !== "function") return Promise.resolve(schedule);
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
    return details.then(function (withDetails) {
        // The Projects list only names paths these rows hold. A list with none — no schedules, or
        // none that names a Project — has nothing to label, and is not a reason to ask.
        var named = withDetails.some(function (schedule) {
            return schedule && typeof schedule.project_dir === "string" && schedule.project_dir;
        });
        var places = named && typeof readPlaces === "function"
            ? Promise.resolve().then(readPlaces).then(function (data) {
                return (data && data.places) || [];
            }).catch(function () { return []; })
            : Promise.resolve([]);
        return places.then(function (list) { return [withDetails, list]; });
    }).then(function (answer) {
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

/* The Projects list, read at most once every few minutes rather than once a minute.
 *
 * It answers one question here — the label and icon for a path this list already holds — and that
 * answer changes when somebody adds or renames a Project, which is not a per-minute event. On the
 * Cloud path it was one machine-scoped read every refresh, byte-identical hundreds of times a day,
 * queued in the same single lane as the transcript somebody had just opened.
 *
 * The cache lives here rather than inside `loadScheduleProjects` so that function stays a pure
 * one, and its tests keep passing their own reader without one run's answer reaching the next.
 * A refused read is not cached.
 *
 * **A partial answer is kept, and only its gap is asked again.** An answer naming a machine that
 * could have answered and did not (`unanswered`) used to be thrown away whole, so an account with
 * one Mac offline — or one machine this browser is not paired with, which `places()` always names —
 * asked every machine again every minute. Now what answered is kept for the TTL; each machine that
 * did not is asked on its own, at most once per `PLACES_RETRY_MS`, and its answer is merged in. A
 * `machine_pairing_required` row is this browser's own fact, which asking cannot change, so it is
 * never re-asked here. An answer with `unconfirmed` machines — never asked at all — is not kept. */
var PLACES_CACHE_TTL_MS = 5 * 60 * 1000;
var PLACES_RETRY_MS = 2 * 60 * 1000;

/** The `unanswered` rows asking again could change. */
export function retryablePlacesGap(data) {
    var rows = data && Array.isArray(data.unanswered) ? data.unanswered : [];
    return rows.filter(function (row) {
        return !!row && typeof row.machine === "string" && row.machine
            && !(row.error && row.error.code === "machine_pairing_required");
    });
}

/**
 * `value` with each machine that answered on its own merged in: its Projects replace any it had,
 * its assistants join the list, and it leaves `unanswered`. A machine that failed again keeps its
 * row with the newer failure.
 */
export function mergePlacesGap(value, results) {
    var answered = new Map();
    var failed = new Map();
    (results || []).forEach(function (result) {
        if (!result || typeof result.machine !== "string") return;
        if (result.error) failed.set(result.machine, result.error);
        else answered.set(result.machine, result.answer || {});
    });
    var places = ((value && value.places) || []).filter(function (place) {
        return !(place && answered.has(place.machine));
    });
    var assistants = ((value && value.assistants) || []).slice();
    var assistantIDs = new Set(assistants.map(function (assistant) { return assistant && assistant.id; }));
    answered.forEach(function (answer) {
        (answer.places || []).forEach(function (place) { places.push(place); });
        (answer.assistants || []).forEach(function (assistant) {
            if (!assistant || assistantIDs.has(assistant.id)) return;
            assistantIDs.add(assistant.id);
            assistants.push(assistant);
        });
    });
    var unanswered = ((value && value.unanswered) || []).filter(function (row) {
        return !(row && answered.has(row.machine));
    }).map(function (row) {
        return row && failed.has(row.machine) ? Object.assign({}, row, { error: failed.get(row.machine) }) : row;
    });
    return Object.assign({}, value, { places: places, assistants: assistants, unanswered: unanswered });
}

/**
 * One Projects cache: `read()` answers from it while it is fresh, and asks `places(machine)` for
 * each machine in its gap once per `retryMs`. `places()` with no machine is the whole list.
 */
export function createPlacesCache(options) {
    options = options || {};
    var places = options.places;
    var now = typeof options.now === "function" ? options.now : function () { return Date.now(); };
    var ttlMs = options.ttlMs || PLACES_CACHE_TTL_MS;
    var retryMs = options.retryMs || PLACES_RETRY_MS;
    var entry = null;

    function askGap(held) {
        var gap = retryablePlacesGap(held.value);
        held.retryAt = now() + retryMs;
        return Promise.all(gap.map(function (row) {
            return Promise.resolve().then(function () { return places(row.machine); })
                .then(function (answer) { return { machine: row.machine, answer: answer }; },
                    function (error) { return { machine: row.machine, error: error }; });
        })).then(function (results) {
            if (entry !== held) return held.value;
            held.value = mergePlacesGap(held.value, results);
            if (!retryablePlacesGap(held.value).length) held.retryAt = null;
            return held.value;
        });
    }

    return {
        read: function () {
            var held = entry;
            if (held && now() - held.at < ttlMs) {
                if (held.retryAt !== null && now() >= held.retryAt) return askGap(held);
                return Promise.resolve(held.value);
            }
            return Promise.resolve().then(function () { return places(); }).then(function (data) {
                // Machines never asked (`unconfirmed`) are a gap this cache cannot fill one by one.
                if (!(data && Array.isArray(data.unconfirmed) && data.unconfirmed.length)) {
                    entry = { at: now(), value: data,
                        retryAt: retryablePlacesGap(data).length ? now() + retryMs : null };
                }
                return data;
            });
        },
        forget: function () { entry = null; }
    };
}

var placesCache = createPlacesCache({
    places: function (machine) { return machine === undefined ? api.places() : api.places(machine); }
});

function cachedPlacesReader() {
    if (typeof api.places !== "function") return null;
    return placesCache.read;
}

/** A Project added or renamed while this list is on screen must not wait out the TTL. */
export function forgetCachedPlaces() { placesCache.forget(); }

var lastRefreshAt = null;
var lane = null;
var LANE_MS = 60000;

function pageHidden() {
    return typeof document !== "undefined" && !!document && document.hidden === true;
}

function refresh() {
    if (inFlight || !S.arrived || S.locked || S.conn === "locked"
        || !api || typeof api.schedules !== "function") return;
    inFlight = true;
    lastRefreshAt = Date.now();
    // Cloud treats its retained orchestrator envelope as first paint only; `fresh` asks the Mac
    // for a named reply. Local and fixture clients ignore the optional argument.
    api.schedules({ fresh: true }).then(function (data) {
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
            cachedPlacesReader())
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
        if (!pageHidden()) lane = setInterval(refresh, LANE_MS);
        watchVisibility();
    }
};

/**
 * **Nobody reads a list on a page that is put away.** The lane stops while the document is hidden —
 * on the Cloud path each tick is a machine-scoped read per machine, and a phone in a pocket was
 * asking every minute — and on return the list is read at once if a tick was missed, then the lane
 * starts again. A page that was away for less than a tick waits for its next one.
 */
function watchVisibility() {
    if (typeof document === "undefined" || !document || typeof document.addEventListener !== "function") return;
    document.addEventListener("visibilitychange", function () {
        if (pageHidden()) {
            if (lane !== null) { clearInterval(lane); lane = null; }
            return;
        }
        if (lane !== null) return;
        lane = setInterval(refresh, LANE_MS);
        if (lastRefreshAt === null || Date.now() - lastRefreshAt >= LANE_MS) refresh();
    });
}
