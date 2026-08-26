import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";

var section = document.getElementById("schedules");
var count = document.getElementById("schedules-count");
var rows = document.getElementById("schedule-rows");

function relativeTime(unix, at) {
    if (!unix) return "";
    var seconds = unix - (at || Date.now() / 1000);
    var absolute = Math.abs(seconds);
    var unit = absolute < 90 * 60 ? "minute" : (absolute < 36 * 3600 ? "hour" : "day");
    var size = unit === "minute" ? 60 : (unit === "hour" ? 3600 : 86400);
    var value = Math.round(seconds / size);
    if (!value) value = seconds < 0 ? -1 : 1;
    try {
        return new Intl.RelativeTimeFormat(document.documentElement.lang || undefined,
            { numeric: "auto" }).format(value, unit);
    } catch (e) {
        var amount = Math.abs(value) + unit.charAt(0);
        return value < 0 ? amount + " ago" : "in " + amount;
    }
}

function result(value) {
    var state = String(value || "").toLowerCase();
    if (state === "success") return { state: state, label: T.webTaskDone };
    if (state === "failure" || state === "timeout" || state === "cancelled"
        || state === "spawn_failed") return { state: state, label: T.webTaskFailed };
    if (state === "queued" || state === "spawning" || state === "briefed") {
        return { state: state, label: T.webTaskRunning };
    }
    return { state: "none", label: "—" };
}

function validRow(schedule, at) {
    var outcome = result(schedule.last_run && schedule.last_run.state);
    var next = schedule.next_fire ? relativeTime(schedule.next_fire, at) : "";
    var nextTitle = schedule.next_fire
        ? new Date(schedule.next_fire * 1000).toLocaleString() : "";
    var enabled = schedule.enabled ? T.webScheduleEnabled : T.webScheduleDisabled;
    var nextLine = schedule.next_fire
        ? (schedule.enabled ? T.webScheduleNext + " " : T.webScheduleDisabled + " · next ") + next
        : T.webScheduleNoNext;
    var missed = schedule.last_missed_at
        ? '<time class="schedule-missed" title="' +
            esc(new Date(schedule.last_missed_at * 1000).toLocaleString()) + '">' +
            esc(T.webScheduleMissed + " " + relativeTime(schedule.last_missed_at, at)) + '</time>'
        : "";
    // A row opens the same sheet the `+` does, filled in — `input/schedule.js` owns the click,
    // reached through `data-id` rather than an import, so this file stays a renderer and does
    // not have to know a sheet exists at all. `role="button" tabindex="0"` is the ARIA
    // authoring-practice shape for a non-native control that does one thing when pressed; a real
    // `<button>` would break the `.schedule-row + .schedule-row` divider in schedules.css, which
    // only fires between direct siblings. The inline cursor is here rather than in schedules.css
    // for the same reason — that file belongs to a different node's claim this round, and a
    // clickable row needs at least this much said about it without touching it.
    return '<li class="schedule-row" data-id="' + esc(schedule.id) + '" role="button" tabindex="0" ' +
        'style="cursor:pointer">' +
        '<div class="schedule-name"><span class="enabled-dot" data-enabled="' +
            (schedule.enabled ? "1" : "0") + '" role="img" aria-label="' + enabled + '"></span>' +
            '<span class="schedule-title">' + esc(schedule.title || "Untitled schedule") + '</span></div>' +
        '<span class="schedule-result" data-state="' + esc(outcome.state) + '">' +
            esc(outcome.label) + '</span>' +
        '<time class="schedule-next"' + (nextTitle ? ' title="' + esc(nextTitle) + '"' : '') + '>' +
            esc(nextLine) + '</time>' + missed +
        '</li>';
}

function invalidRow(schedule) {
    return '<li class="schedule-row invalid">' +
        '<div class="schedule-name"><span class="enabled-dot" data-enabled="invalid" role="img" aria-label="Invalid"></span>' +
            '<span class="schedule-title">' + esc(schedule.file || "Invalid schedule") + '</span></div>' +
        '<span class="schedule-result" data-state="invalid">invalid</span>' +
        '<p class="schedule-error">' + esc(schedule.error || "The schedule could not be read.") + '</p>' +
        '</li>';
}

export function renderSchedules(schedules, at) {
    schedules = schedules || [];
    if (!section || !rows || !count) return;
    // An empty inventory used to cost the session list no space — true back when this section
    // was read-only and there was nothing to do about an empty list. Now `#schedule-new` lives in
    // this section's own `<summary>`, so hiding it on zero schedules would also hide the one way
    // to make a first one. It still folds away with nothing to show when writing is off, same as
    // before.
    section.hidden = schedules.length === 0 && !S.write;
    if (!schedules.length) {
        rows.innerHTML = "";
        count.textContent = "";
        return;
    }
    count.textContent = String(schedules.length);
    rows.innerHTML = schedules.map(function (schedule) {
        return schedule && schedule.state === "invalid"
            ? invalidRow(schedule) : validRow(schedule || {}, at);
    }).join("");
}
