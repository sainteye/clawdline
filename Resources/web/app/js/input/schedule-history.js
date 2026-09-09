import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { scheduleRunsHTML, scheduleRunPlace } from "../view/schedules.js";
import { openSession } from "../session/open.js";
import { Schedule } from "./schedule.js";
import { Start } from "./start.js";
import { Schedules } from "../net/schedules.js";
import { scheduleRunConfirmation, scheduleRunCopy, scheduleRunMessage }
    from "./schedule-run.js";
import { generateAndBindScheduleWebhook, scheduleWebhookCanGenerate,
    scheduleWebhookCopy, scheduleWebhookCurlExample, scheduleWebhookHelpHTML,
    scheduleWebhookManagementWarning, scheduleWebhookReceiptHeads,
    scheduleWebhookTimelineHTML, shouldObserveScheduleWebhook }
    from "../net/schedule-webhooks.js";

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
    var webhook = null;
    var hook = null;
    var deliveries = [];
    var ephemeralURL = null;
    var effectiveTier = null;
    var observedReceipts = {};
    var observingReceipts = {};
    var helpOpen = false;
    var webhookOpen = false;
    var runningNow = false;

    function scheduleRunWords() {
        return scheduleRunCopy(document.documentElement.lang);
    }

    function ensureRunNowButton() {
        var edit = els["schedule-history-edit"];
        if (!edit || document.getElementById("schedule-history-run-now")) return;
        var button = document.createElement("button");
        button.id = "schedule-history-run-now";
        button.type = "button";
        button.className = "chip confirm-go";
        button.addEventListener("click", runNow);
        edit.parentNode.insertBefore(button, edit);
    }

    function webhookWords() {
        return scheduleWebhookCopy(document.documentElement.lang);
    }

    function webhookNode(id) { return document.getElementById(id); }

    function ensureWebhookToggle() {
        var panel = webhookNode("schedule-webhook");
        var title = webhookNode("schedule-webhook-title");
        if (!panel || !title || webhookNode("schedule-webhook-panel-toggle")) return;
        var toggle = document.createElement("button");
        toggle.id = "schedule-webhook-panel-toggle";
        toggle.type = "button";
        toggle.className = "chip schedule-webhook-panel-toggle";
        toggle.setAttribute("aria-controls", "schedule-webhook-note schedule-webhook-warning "
            + "schedule-webhook-help schedule-webhook-status schedule-webhook-timeline");
        toggle.addEventListener("click", function () {
            webhookOpen = !webhookOpen;
            drawWebhook();
        });
        title.insertAdjacentElement("afterend", toggle);
        panel.dataset.collapsed = "true";
    }

    function ensureWebhookHelp() {
        var panel = webhookNode("schedule-webhook");
        if (!panel || webhookNode("schedule-webhook-help-toggle")) return;
        var actions = panel.querySelector(".buttons");
        var status = webhookNode("schedule-webhook-status");
        if (!actions || !status) return;

        var toggle = document.createElement("button");
        toggle.id = "schedule-webhook-help-toggle";
        toggle.type = "button";
        toggle.className = "chip schedule-webhook-help-toggle";
        toggle.setAttribute("aria-controls", "schedule-webhook-help");
        toggle.setAttribute("aria-expanded", "false");
        toggle.addEventListener("click", function () {
            helpOpen = !helpOpen;
            drawWebhook();
        });
        actions.appendChild(toggle);

        var region = document.createElement("section");
        region.id = "schedule-webhook-help";
        region.className = "schedule-webhook-help";
        region.hidden = true;
        region.setAttribute("role", "region");
        region.setAttribute("aria-labelledby", toggle.id);

        var content = document.createElement("div");
        content.id = "schedule-webhook-help-content";
        region.appendChild(content);

        var copy = document.createElement("button");
        copy.id = "schedule-webhook-help-copy-example";
        copy.type = "button";
        copy.className = "chip";
        copy.addEventListener("click", function () {
            if (!webhook || typeof webhook.copy !== "function") return;
            Promise.resolve(webhook.copy(scheduleWebhookCurlExample())).then(function () {
                webhookNode("schedule-webhook-status").textContent =
                    webhookWords().exampleCopied;
            }, webhookFailed);
        });
        region.appendChild(copy);
        panel.insertBefore(region, status);
    }

    function drawWebhook() {
        var panel = webhookNode("schedule-webhook");
        if (!panel) return;
        ensureWebhookToggle();
        ensureWebhookHelp();
        var words = webhookWords();
        panel.hidden = !webhook || !record;
        panel.dataset.collapsed = webhookOpen ? "false" : "true";
        var panelToggle = webhookNode("schedule-webhook-panel-toggle");
        if (panelToggle) {
            panelToggle.textContent = webhookOpen ? words.hideDetails : words.showDetails;
            panelToggle.setAttribute("aria-expanded", webhookOpen ? "true" : "false");
        }
        webhookNode("schedule-webhook-title").textContent = words.title;
        webhookNode("schedule-webhook-note").textContent = ephemeralURL
            ? words.once : words.idempotency;
        var managementContext = {
            bindingAvailability: record && record.webhook_binding_availability,
            effectiveTier: effectiveTier, language: document.documentElement.lang
        };
        webhookNode("schedule-webhook-warning").textContent =
            scheduleWebhookManagementWarning(hook, managementContext);
        webhookNode("schedule-webhook-generate").textContent = words.generate;
        webhookNode("schedule-webhook-copy").textContent = words.copy;
        webhookNode("schedule-webhook-rotate").textContent = words.rotate;
        webhookNode("schedule-webhook-disable").textContent = words.disable;
        var helpToggle = webhookNode("schedule-webhook-help-toggle");
        var help = webhookNode("schedule-webhook-help");
        if (helpToggle && help) {
            helpToggle.textContent = helpOpen ? words.hideHelp : words.howToUse;
            helpToggle.setAttribute("aria-expanded", helpOpen ? "true" : "false");
            help.hidden = !helpOpen;
            webhookNode("schedule-webhook-help-content").innerHTML =
                scheduleWebhookHelpHTML(document.documentElement.lang);
            webhookNode("schedule-webhook-help-copy-example").textContent = words.copyExample;
        }
        webhookNode("schedule-webhook-generate").hidden = !(!hook || hook.state === "disabled");
        webhookNode("schedule-webhook-generate").disabled =
            !scheduleWebhookCanGenerate(hook, managementContext);
        webhookNode("schedule-webhook-copy").hidden = !ephemeralURL;
        webhookNode("schedule-webhook-rotate").hidden = !hook || hook.state !== "active";
        webhookNode("schedule-webhook-rotate").disabled =
            effectiveTier === null || effectiveTier === "free";
        webhookNode("schedule-webhook-disable").hidden = !hook || hook.state === "disabled";
        webhookNode("schedule-webhook-timeline").innerHTML =
            scheduleWebhookTimelineHTML(
                deliveries, observedReceipts, document.documentElement.lang);
        observeVisibleTimeline();
    }

    function observeVisibleTimeline() {
        if (!webhook || !hook || !deliveries.length) return;
        scheduleWebhookReceiptHeads(deliveries).forEach(function (latest) {
            var key = latest.deliveryID + ":" + latest.receiptVersion;
            if (observedReceipts[key] || observingReceipts[key]) return;
            if (!shouldObserveScheduleWebhook({
                open: !els["schedule-history"].hidden && webhookOpen,
                visibilityState: document.visibilityState,
                renderedVersion: latest.receiptVersion })) return;
            observingReceipts[key] = true;
            webhook.client.observe(hook.hook_id, latest.deliveryID, latest.receiptVersion)
                .then(function (answer) {
                    observedReceipts[key] = answer && (answer.last_observed_at || answer.observed_at)
                        || new Date().toISOString();
                    delete observingReceipts[key];
                    drawWebhook();
                }, function () { delete observingReceipts[key]; });
        });
    }

    function loadWebhook() {
        if (!webhook || !record) { drawWebhook(); return Promise.resolve(); }
        var hookID = record.webhook_hook_id;
        var hookRead = hookID ? webhook.client.read(hookID) : Promise.resolve(null);
        var deliveryRead = hookID ? webhook.client.deliveries(hookID)
            : Promise.resolve({ deliveries: [] });
        var entitlementRead = webhook.client.entitlements().catch(function () { return null; });
        return Promise.all([hookRead, deliveryRead, entitlementRead])
            .then(function (answers) {
                hook = answers[0] && (answers[0].hook || answers[0]);
                deliveries = answers[1] && answers[1].deliveries || [];
                effectiveTier = answers[2];
                drawWebhook();
            }, function () {
                webhookNode("schedule-webhook-status").textContent =
                    webhookWords().unavailable;
                drawWebhook();
            });
    }

    function webhookAction(action) {
        if (!webhook || !record) return Promise.resolve();
        webhookNode("schedule-webhook-status").textContent = "";
        if (action === "copy" && ephemeralURL) {
            return Promise.resolve(webhook.copy(ephemeralURL));
        }
        if (action === "generate") {
            return generateAndBindScheduleWebhook(webhook.client, webhook.bind,
                webhook.machine(scheduleId), scheduleId, hook).then(function (made) {
                        hook = made.hook;
                        ephemeralURL = made.publicURL;
                        record.webhook_hook_id = hook.hook_id;
                        record.webhook_binding_availability = "active";
                        deliveries = [];
                        drawWebhook();
            }).catch(webhookFailed);
        }
        if (!hook) return Promise.resolve();
        if (action === "rotate") {
            return webhook.client.rotate(hook.hook_id, hook.revision).then(function (made) {
                hook = made.hook; ephemeralURL = made.publicURL; drawWebhook();
            }).catch(webhookFailed);
        }
        if (action === "disable") {
            return webhook.client.disable(hook.hook_id, hook.revision).then(function (answer) {
                hook = answer.hook || answer; ephemeralURL = null; drawWebhook();
            }).catch(webhookFailed);
        }
        return Promise.resolve();
    }

    function webhookFailed() {
        webhookNode("schedule-webhook-status").textContent = webhookWords().unavailable;
        drawWebhook();
    }

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
        ensureRunNowButton();
        var runs = (record && record.runs) || [];
        els["schedule-history-sheet"].setAttribute(
            "aria-busy", loading || pressing || runningNow ? "true" : "false");
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
        els["schedule-history-edit"].disabled = loading || !!pressing || runningNow || !record;
        var runNowButton = document.getElementById("schedule-history-run-now");
        if (runNowButton) {
            runNowButton.textContent = runningNow
                ? scheduleRunWords().running : scheduleRunWords().button;
            runNowButton.disabled = loading || !!pressing || runningNow || !record;
        }
        els["schedule-history-close"].textContent = T.webClose;
        els["schedule-history-close"].disabled = !!pressing || runningNow;

        var buttons = els["schedule-run-rows"].querySelectorAll(".schedule-run-button");
        for (var i = 0; i < buttons.length; i++) {
            // The resume route takes an opaque place id, never a path. If this old project has
            // fallen out of the bounded place list, keep the occurrence readable without
            // offering a press the server cannot resolve.
            buttons[i].disabled = buttons[i].disabled || !!pressing || runningNow
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
        runningNow = false;
        webhookOpen = false;
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
            loadWebhook();
        }).catch(function (e) {
            if (mine !== ticket || scheduleId !== id) return;
            loading = false;
            els["schedule-history-said"].textContent = why(e);
            draw();
        });
    }

    function close(force) {
        if ((pressing || runningNow) && !force) return;
        ticket += 1;
        scheduleId = null;
        record = null;
        places = [];
        loading = false;
        pressing = null;
        runningNow = false;
        hook = null;
        deliveries = [];
        ephemeralURL = null;
        effectiveTier = null;
        observedReceipts = {};
        observingReceipts = {};
        helpOpen = false;
        webhookOpen = false;
        els["schedule-history"].hidden = true;
    }

    function edit() {
        if (loading || pressing || runningNow || !record) return;
        var id = scheduleId;
        close(true);
        Schedule.openEdit(id);
    }

    function runNow() {
        if (loading || pressing || runningNow || !record) return;
        if (!S.write) {
            els["schedule-history-said"].textContent = scheduleRunWords().writeOff;
            return;
        }
        if (typeof api.runSchedule !== "function") {
            els["schedule-history-said"].textContent = scheduleRunWords().failed;
            return;
        }
        if (!window.confirm(scheduleRunConfirmation(
            record.title || scheduleId, document.documentElement.lang))) return;

        var id = scheduleId;
        runningNow = true;
        els["schedule-history-said"].textContent = "";
        draw();
        api.runSchedule(id).then(function () {
            if (scheduleId !== id) return;
            runningNow = false;
            els["schedule-history-said"].textContent = scheduleRunWords().accepted;
            draw();
            Schedules.refresh();
            return api.schedule(id).then(function (answer) {
                if (scheduleId !== id) return;
                record = answer && answer.schedule || record;
                draw();
            }, function () { });
        }).catch(function (error) {
            if (scheduleId !== id) return;
            runningNow = false;
            if (error && error.code === "write_disabled") S.write = false;
            els["schedule-history-said"].textContent =
                scheduleRunMessage(error, document.documentElement.lang);
            draw();
        });
    }

    function pick(taskId, action) {
        if (loading || pressing || runningNow) return;
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

    return {
        open: open, close: close, edit: edit, pick: pick, runNow: runNow,
        bindWebhook: function (value) { webhook = value; }, webhookAction: webhookAction
    };
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
["generate", "copy", "rotate", "disable"].forEach(function (action) {
    var button = document.getElementById("schedule-webhook-" + action);
    if (button) button.addEventListener("click", function () {
        ScheduleHistory.webhookAction(action);
    });
});
els["schedule-run-rows"].addEventListener("click", function (ev) {
    var button = ev.target.closest ? ev.target.closest(".schedule-run-button") : null;
    if (!button || button.disabled) return;
    ScheduleHistory.pick(button.dataset.taskId, button.dataset.action);
});
els["schedule-history"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    var items = [document.getElementById("schedule-history-run-now"),
                 els["schedule-history-edit"]]
        .concat(Array.from(els["schedule-history-sheet"].querySelectorAll(
            "#schedule-webhook button:not([hidden]):not([disabled])")))
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
