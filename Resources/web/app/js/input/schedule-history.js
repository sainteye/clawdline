import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { scheduleRunsHTML, scheduleRunPlace } from "../view/schedules.js";
import { openSession } from "../session/open.js";
import { Schedule } from "./schedule.js";
import { Start } from "./start.js";

/* --------------------------------------------------------------------------
   A schedule is more than the form that defines its next occurrence. This sheet
   is the retained executions that definition produced, and the one narrow door
   back into a conversation the general project-history picker deliberately hides
   as orchestrator plumbing.

   The server returns `session_id` only after proving it against the task's own
   transcript/rollout. The page never derives one from a terminal id or title.
   -------------------------------------------------------------------------- */

export var ScheduleHistory = (function () {
    var scheduleId = null;
    var record = null;
    var places = [];
    var loading = false;
    var pressing = null;
    var ticket = 0;

    function terminalIsOpen(id) { return !!byId(id); }

    function run(taskId) {
        return ((record && record.runs) || []).filter(function (candidate) {
            return candidate && candidate.task_id === taskId;
        })[0] || null;
    }

    function projectPlace(selected) {
        return scheduleRunPlace(selected, places);
    }

    function why(e) {
        if (e && e.code === "write_disabled") return T.webStartOff;
        if (e && e.code === "not_found") return T.webResumeGone;
        // `terminal_closed` carries `app` when there is one to carry. Filling the hole with ""
        // drew "A session cannot be started in  from here", so the sentence is only written when
        // the name for it arrived — the same guard `start.js` and `command.js` already had.
        if (e && e.app && e.code === "terminal_closed") {
            return fill(T.webStartTerminalClosed, { app: e.app });
        }
        // `terminal_unsupported` never carries one: the refusal it answers is "tmux is the
        // terminal in Settings and there is no tmux on this Mac", which is not an application.
        // Its sentence is written whole, so this page shows it rather than "Request failed".
        if (e && e.code === "terminal_unsupported") {
            return T.webStartTerminalUnsupported;
        }
        return T.webRequestFailed;
    }

    function draw() {
        var runs = (record && record.runs) || [];
        els["schedule-history-sheet"].setAttribute("aria-busy", loading || pressing ? "true" : "false");
        els["schedule-history-title"].textContent = (record && record.title) || T.webScheduleEdit;
        var path = record && record.task && record.task.project_dir;
        var next = record && record.next_fire
            ? new Date(record.next_fire * 1000).toLocaleString() : "";
        els["schedule-history-meta"].textContent = [path, next].filter(Boolean).join(" · ");
        els["schedule-history-runs-label"].textContent = T.webResumePick;
        els["schedule-run-rows"].innerHTML = scheduleRunsHTML(
            runs, Date.now() / 1000, terminalIsOpen);
        els["schedule-history-empty"].textContent = T.webResumeEmpty;
        els["schedule-history-empty"].hidden = loading || runs.length > 0
            || !!els["schedule-history-said"].textContent;
        els["schedule-history-capped"].textContent = T.webResumeCapped;
        els["schedule-history-capped"].hidden = !(record && record.runs_may_be_truncated);
        els["schedule-history-edit"].textContent = T.webScheduleEdit;
        els["schedule-history-edit"].disabled = loading || !!pressing || !record;
        els["schedule-history-close"].textContent = T.webClose;
        els["schedule-history-close"].disabled = !!pressing;

        var buttons = els["schedule-run-rows"].querySelectorAll(".schedule-run-button");
        for (var i = 0; i < buttons.length; i++) {
            // The resume route takes an opaque place id, never a path. If this old project has
            // fallen out of the bounded place list, keep the occurrence readable without
            // offering a press the server cannot resolve.
            buttons[i].disabled = buttons[i].disabled || !!pressing
                || (buttons[i].dataset.action === "resume"
                    && !projectPlace(run(buttons[i].dataset.taskId)));
            if (pressing && buttons[i].dataset.taskId === pressing) {
                buttons[i].querySelector(".schedule-run-action").textContent = T.webResuming;
            }
        }
    }

    function open(id) {
        if (!id || !els["schedule-history"].hidden) return;
        scheduleId = id;
        record = null;
        places = [];
        pressing = null;
        loading = true;
        els["schedule-history-said"].textContent = "";
        els["schedule-history"].hidden = false;
        draw();
        els["schedule-history-close"].focus({ preventScroll: true });
        var mine = ++ticket;
        var detail = typeof api.schedule === "function"
            ? api.schedule(id) : Promise.reject(new Error("schedule detail unavailable"));
        var availablePlaces = typeof api.places === "function"
            ? api.places().catch(function () { return { places: [] }; })
            : Promise.resolve({ places: [] });
        Promise.all([detail, availablePlaces]).then(function (answers) {
            if (mine !== ticket || scheduleId !== id) return;
            record = answers[0] && answers[0].schedule;
            places = (answers[1] && answers[1].places) || [];
            loading = false;
            draw();
        }).catch(function (e) {
            if (mine !== ticket || scheduleId !== id) return;
            loading = false;
            els["schedule-history-said"].textContent = why(e);
            draw();
        });
    }

    function close(force) {
        if (pressing && !force) return;
        ticket += 1;
        scheduleId = null;
        record = null;
        places = [];
        loading = false;
        pressing = null;
        els["schedule-history"].hidden = true;
    }

    function edit() {
        if (loading || pressing || !record) return;
        var id = scheduleId;
        close(true);
        Schedule.openEdit(id);
    }

    function pick(taskId, action) {
        if (loading || pressing) return;
        var selected = run(taskId);
        if (!selected) return;

        if (action === "open") {
            var live = selected.terminal_id && byId(selected.terminal_id);
            if (!live) { draw(); return; }
            close(true);
            openSession(live.id);
            return;
        }

        if (action !== "resume" || !selected.session_id) return;
        if (!S.write) {
            els["schedule-history-said"].textContent = T.webStartOff;
            return;
        }
        var place = projectPlace(selected);
        if (!place || typeof api.resumePlace !== "function") {
            els["schedule-history-said"].textContent = T.webRequestFailed;
            return;
        }
        pressing = taskId;
        els["schedule-history-said"].textContent = "";
        draw();
        api.resumePlace(place.id, selected.session_id, selected.assistant).then(function (answer) {
            pressing = null;
            close(true);
            Start.began(answer && answer.id, place);
        }).catch(function (e) {
            pressing = null;
            if (e && e.code === "write_disabled") S.write = false;
            els["schedule-history-said"].textContent = why(e);
            draw();
        });
    }

    return { open: open, close: close, edit: edit, pick: pick };
})();

// Delegated because the ambient schedule refresh replaces every row once a minute.
els["schedule-rows"].addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest(".schedule-row[data-id]") : null;
    if (row) ScheduleHistory.open(row.dataset.id);
});
els["schedule-rows"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Enter" && ev.key !== " ") return;
    var row = ev.target.closest ? ev.target.closest(".schedule-row[data-id]") : null;
    if (!row) return;
    ev.preventDefault();
    ScheduleHistory.open(row.dataset.id);
});

els["schedule-history"].addEventListener("click", function () { ScheduleHistory.close(); });
els["schedule-history-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["schedule-history-close"].addEventListener("click", function () { ScheduleHistory.close(); });
els["schedule-history-edit"].addEventListener("click", function () { ScheduleHistory.edit(); });
els["schedule-run-rows"].addEventListener("click", function (ev) {
    var button = ev.target.closest ? ev.target.closest(".schedule-run-button") : null;
    if (!button || button.disabled) return;
    ScheduleHistory.pick(button.dataset.taskId, button.dataset.action);
});
els["schedule-history"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    var items = [els["schedule-history-edit"]]
        .concat(Array.from(els["schedule-run-rows"].querySelectorAll(
            ".schedule-run-button:not([disabled])")))
        .concat([els["schedule-history-close"]])
        .filter(function (item) { return item && !item.disabled; });
    var at = items.indexOf(document.activeElement);
    if (at < 0 || !items.length) return;
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at === 0)) {
        ev.preventDefault();
        items[ev.shiftKey ? items.length - 1 : 0].focus();
    }
});

document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape" || els["schedule-history"].hidden) return;
    ev.preventDefault(); ev.stopPropagation();
    ScheduleHistory.close();
}, true);
