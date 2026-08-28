import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { drawIcon } from "../core/pixels.js";
import { tint } from "../core/util.js";

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

/** Resolve a retained occurrence against the bounded opaque-place listing.
 *
 * The run owns this path, not the schedule's current task template: editing a schedule must not
 * move an older conversation to a directory it never ran in. */
export function scheduleRunPlace(run, places) {
    var path = run && run.project_dir;
    return (places || []).filter(function (place) {
        return place && place.path === path;
    })[0] || null;
}

/** One schedule's retained task records, newest first as the server supplied them.
 *
 * A run can do one of three honest things: open the terminal that is still on the Session list,
 * resume a proven conversation after that tab has gone, or remain a readable result with no
 * action. `terminalIsOpen` is injected so the small renderer stays testable without importing the
 * live Session store. */
export function scheduleRunsHTML(runs, at, terminalIsOpen) {
    return (runs || []).map(function (run) {
        run = run || {};
        var outcome = result(run.state);
        var open = !!(run.terminal_id && typeof terminalIsOpen === "function"
            && terminalIsOpen(run.terminal_id));
        var action = open ? "open" : (run.session_id ? "resume" : "none");
        var actionLabel = open ? T.webResumeLive
            : (action === "resume" ? T.webResumeWith : outcome.label);
        var created = run.created ? new Date(run.created * 1000) : null;
        var when = created ? created.toLocaleString() : "";
        var relative = run.created ? relativeTime(run.created, at) : "";
        var assistant = String(run.assistant || "");
        var project = String(run.project_dir || "");
        var projectParts = project.split("/").filter(Boolean);
        var projectLabel = project === "/" ? "/" : (projectParts.pop() || "");
        // The timestamp already owns the first row. Repeating the same relative time under the
        // summary made the compact phone layout look like two different timestamps.
        var meta = [assistant, projectLabel].filter(Boolean).join(" · ");
        var summary = run.summary || "";
        return '<li class="schedule-run">' +
            '<button type="button" class="schedule-run-button" data-task-id="' +
                esc(run.task_id || "") + '" data-action="' + action + '"' +
                (action === "none" ? " disabled" : "") + '>' +
                '<span class="schedule-run-state" data-state="' + esc(outcome.state) + '">' +
                    esc(outcome.label) + '</span>' +
                '<time class="schedule-run-time"' + (when ? ' title="' + esc(when) + '"' : '') +
                    '>' + esc(relative || when) + '</time>' +
                // The summary is clamped to two lines in schedules.css. The title keeps the
                // whole sentence reachable where a pointer exists, without letting one verbose
                // run bury the ones under it.
                (summary ? '<span class="schedule-run-summary" title="' + esc(summary) + '">' +
                    esc(summary) + '</span>' : '') +
                '<span class="schedule-run-meta"' +
                    (project ? ' title="' + esc(project) + '"' : '') + '>' + esc(meta) + '</span>' +
                '<span class="schedule-run-action">' + esc(actionLabel) + '</span>' +
            '</button></li>';
    }).join("");
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
    var projectData = schedule.project && typeof schedule.project === "object"
        ? schedule.project : null;
    var project = projectData
        ? '<span class="schedule-project"><canvas class="schedule-project-mark" aria-hidden="true"></canvas>' +
            '<span class="schedule-project-name" title="' + esc(projectData.path || "") + '">' +
                esc(projectData.label || "") + '</span></span>' : "";
    var projectSeparator = project && nextLine
        ? '<span class="schedule-meta-sep" aria-hidden="true"> · </span>' : "";
    // A row opens its run history — `input/schedule-history.js` owns the click, reached through
    // `data-id` rather than an import, so this file stays a renderer and does not have to know a
    // sheet exists at all. `role="button" tabindex="0"` is the ARIA
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
        '<div class="schedule-meta">' + project + projectSeparator +
            '<time class="schedule-next"' + (nextTitle ? ' title="' + esc(nextTitle) + '"' : '') + '>' +
                esc(nextLine) + '</time></div>' + missed +
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
    if (typeof rows.querySelectorAll !== "function") return;
    var projects = {};
    schedules.forEach(function (schedule) {
        if (schedule && schedule.id && schedule.project) projects[schedule.id] = schedule.project;
    });
    var rendered = rows.querySelectorAll(".schedule-row[data-id]");
    for (var i = 0; i < rendered.length; i++) {
        var projectData = projects[rendered[i].dataset.id];
        if (!projectData) continue;
        var mark = rendered[i].querySelector(".schedule-project-mark");
        if (mark && !drawIcon(mark, projectData.icon, 3)) mark.classList.add("none");
        var name = rendered[i].querySelector(".schedule-project-name");
        if (name) name.style.color = projectData.icon ? tint(projectData.icon.accent) : "";
    }
}
