import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { shortPath, tint, toast } from "../core/util.js";
import { drawIcon } from "../core/pixels.js";
import { api } from "../net/api.js";
import { Schedules } from "../net/schedules.js";

/* ---- making a schedule -----------------------------------------------------
   The markup, the ids and the styling are `#schedule-form` in `index.html` and
   `.schedule-sheet` in `sheets.css` — both frozen, both drawn once in `bfb4c10`. This file is
   what fills them in and what happens when Create is pressed.

   **Two doors, one sheet.** `open()` is the `+` beside the Schedules list: every field blank,
   the seven-days-and-daily row defaulting to daily, and a person filling in the rest by hand.
   `openFrom(draft, instructions)` is the other one — `input/command.js` calls it when a spoken
   draft comes back with `kind: "schedule"` — and it is the same sheet holding whatever the
   planner could work out, with whatever it could not left for a person to answer. Neither door
   sends anything by itself: **a confident schedule draft still does not create one.** Opening a
   session now and arranging one to run every morning are not the same risk, and only the first of
   those two happens without a press.

   `POST /v1/orchestrator/schedules` is gated exactly like `/v1/voice` and `/v1/intents` — the
   write switch, the `send` capability, an Idempotency-Key — never the orchestrator token, which
   is local-only and could not reach a phone. A 400 from it carries a sentence the Swift parser
   wrote about which field was wrong; that sentence is shown as it arrived; see `why` below.
   ----------------------------------------------------------------------- */
export var Schedule = (function () {
    var DAY_CODES = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
    var DAY_KEYS = ["webScheduleSun", "webScheduleMon", "webScheduleTue", "webScheduleWed",
                    "webScheduleThu", "webScheduleFri", "webScheduleSat"];
    var CLOSE_VALUES = ["on_success", "always", "never"];
    var CLOSE_KEYS = ["webScheduleCloseSuccess", "webScheduleCloseAlways", "webScheduleCloseNever"];
    // The same defaults `Orchestrator.schedule(from:)` gives a file that leaves them out, so a
    // person who never opens "More" still gets the values the Mac would have picked anyway.
    var CATCH_DEFAULT = 6;
    var TIMEOUT_DEFAULT = 30;
    // Mirrors `SURE` in input/command.js, which mirrors `Planner.sure` in Planner.swift — 0.5 is
    // where a drafted answer is offered rather than asked about. Repeated here rather than
    // imported: command.js already imports this file, and this round does not touch command.js.
    var SURE = 0.5;

    var places = null;          // GET /v1/places, fetched once and reused for this sheet's life
    var assistants = [];
    var chosenPlace = null;     // a place id, once the planner or a person has picked one
    var chosenAssistant = null;
    // "daily" or a non-empty array of DAY_CODES — never an empty array. See `toggleDay`.
    var days = "daily";
    // True only when `days` above is "daily" because nothing was heard, not because "daily" was
    // actually the answer — see `openFrom`. Cleared the moment a person touches a day chip, so it
    // never outlives the one draft it was about.
    var daysGuessed = false;
    var closeTab = "on_success";
    var enabled = true;
    var notify = true;
    var creating = false;       // the POST is in flight

    function busy() { return creating; }

    function said(words) { els["schedule-said"].textContent = words || ""; }

    /** The one thing that decides whether Create can actually send anything, computed instead of
     *  remembered so a press and an open ask the exact same question. Everything else the server
     *  can refuse on its own and say why — see `why` — but these two have their own string keys
     *  because a schedule with no time or no project is not a mistake worth a round trip to hear
     *  about. */
    function hint() {
        if (!els["schedule-at"].value) return T.webScheduleNeedsTime;
        if (!chosenPlace) return T.webScheduleNeedsPlace;
        return "";
    }

    /* ---- painting: every row disables together while a request is in flight ----- */
    function paint() {
        var b = busy();
        els["schedule-sheet"].setAttribute("aria-busy", b ? "true" : "false");
        ["schedule-title", "schedule-at", "schedule-instructions", "schedule-catch",
         "schedule-timeout", "schedule-cancel", "schedule-go"].forEach(function (id) {
            els[id].disabled = b;
        });
        ["schedule-with", "schedule-days", "schedule-close", "schedule-flags"].forEach(function (id) {
            var chips = els[id].querySelectorAll(".chip");
            for (var i = 0; i < chips.length; i++) chips[i].disabled = b;
        });
        var rows = els["schedule-places"].querySelectorAll(".place");
        for (var j = 0; j < rows.length; j++) rows[j].disabled = b;
    }

    /* ---- the assistant chips, copied from input/start.js's own drawWith --------- */
    function drawWith() {
        var row = els["schedule-with"];
        row.innerHTML = "";
        row.hidden = assistants.length < 2;
        if (row.hidden) return;
        var label = document.createElement("span");
        label.className = "with-label";
        label.textContent = T.webScheduleWith;
        row.appendChild(label);
        assistants.forEach(function (a) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (a.id === chosenAssistant ? " on" : "");
            chip.textContent = a.label || a.id;
            chip.setAttribute("aria-pressed", a.id === chosenAssistant ? "true" : "false");
            chip.onclick = function () { chosenAssistant = a.id; drawWith(); };
            row.appendChild(chip);
        });
        paint();
    }

    /* ---- the days row: daily or some weekdays, never neither -------------------- */
    function drawDays() {
        var row = els["schedule-days"];
        row.innerHTML = "";
        var daily = document.createElement("button");
        daily.type = "button";
        // Lit up only when "daily" is a real answer, not the placeholder `openFrom` leaves in the
        // variable while nothing has been heard — otherwise a guess looks exactly like a choice.
        var dailyOn = days === "daily" && !daysGuessed;
        daily.className = "chip" + (dailyOn ? " on" : "");
        daily.textContent = T.webScheduleDaily;
        daily.setAttribute("aria-pressed", dailyOn ? "true" : "false");
        daily.onclick = function () { days = "daily"; daysGuessed = false; drawDays(); };
        row.appendChild(daily);
        DAY_CODES.forEach(function (code, i) {
            var on = Array.isArray(days) && days.indexOf(code) >= 0;
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (on ? " on" : "");
            chip.textContent = T[DAY_KEYS[i]];
            chip.setAttribute("aria-pressed", on ? "true" : "false");
            chip.onclick = function () { toggleDay(code); };
            row.appendChild(chip);
        });
        paint();
    }

    function toggleDay(code) {
        var picked = Array.isArray(days) ? days.slice() : [];
        var at = picked.indexOf(code);
        if (at >= 0) picked.splice(at, 1); else picked.push(code);
        // Turning off the last weekday falls back to daily rather than leaving nothing picked —
        // the one shape `Orchestrator.schedule(from:)` refuses outright — and picking a day while
        // "Daily" was on replaces it rather than adding to it, which is what a chip that says
        // "every day" stops meaning the moment one day is chosen instead.
        days = picked.length ? DAY_CODES.filter(function (c) { return picked.indexOf(c) >= 0; }) : "daily";
        daysGuessed = false;
        drawDays();
    }

    /* ---- when it finishes: three mutually exclusive chips ----------------------- */
    function drawClose() {
        var row = els["schedule-close"];
        row.innerHTML = "";
        CLOSE_VALUES.forEach(function (value, i) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (value === closeTab ? " on" : "");
            chip.textContent = T[CLOSE_KEYS[i]];
            chip.setAttribute("aria-pressed", value === closeTab ? "true" : "false");
            chip.onclick = function () { closeTab = value; drawClose(); };
            row.appendChild(chip);
        });
        paint();
    }

    /* ---- enabled and notify-on-failure: two check chips, copied from
       input/start.js's drawResume and its tick markup -------------------------- */
    function checkChip(on, label) {
        var chip = document.createElement("button");
        chip.type = "button";
        chip.className = "chip check" + (on ? " on" : "");
        chip.innerHTML = '<svg class="tick" viewBox="0 0 14 14" aria-hidden="true"'
            + ' focusable="false">'
            + '<rect class="box" x="0.5" y="0.5" width="13" height="13" rx="3.5"></rect>'
            + '<path class="mark" d="M3.6 7.1 5.9 9.4 10.4 4.6"'
            + ' stroke-linecap="round" stroke-linejoin="round"></path></svg>'
            + '<span class="label"></span>';
        chip.querySelector(".label").textContent = label;
        chip.setAttribute("aria-pressed", on ? "true" : "false");
        return chip;
    }

    function drawFlags() {
        var row = els["schedule-flags"];
        row.innerHTML = "";
        var enabledChip = checkChip(enabled, T.webScheduleEnabled);
        enabledChip.onclick = function () { enabled = !enabled; drawFlags(); };
        row.appendChild(enabledChip);
        var notifyChip = checkChip(notify, T.webScheduleNotify);
        notifyChip.onclick = function () { notify = !notify; drawFlags(); };
        row.appendChild(notifyChip);
        paint();
    }

    /* ---- the places list, copied from input/start.js's own row template --------
       `sheets.css` has no highlight for a row that is merely *picked* rather than being landed
       on — see the same note on `markPicked` in `input/command.js`, which this repeats because
       that one is not exported. */
    function markPicked(row, on) {
        row.setAttribute("aria-pressed", on ? "true" : "false");
        row.style.borderColor = on ? "var(--accent-ed)" : "";
        row.style.opacity = on ? "1" : "";
        row.querySelector(".where").style.color = on ? "var(--accent)" : "";
    }

    function drawPlaces() {
        var list = els["schedule-places"];
        list.innerHTML = "";
        (places || []).forEach(function (p) {
            var li = document.createElement("li");
            var row = document.createElement("button");
            row.type = "button";
            row.className = "place";
            row.dataset.id = p.id;
            row.innerHTML = '<canvas></canvas><span class="name"></span><span class="where"></span>';
            var mark = row.querySelector("canvas");
            if (!drawIcon(mark, p.icon, 4)) mark.classList.add("none");
            var name = row.querySelector(".name");
            name.textContent = p.label || p.path;
            name.style.color = p.icon ? tint(p.icon.accent) : "";
            row.querySelector(".where").textContent = shortPath(p.path);
            markPicked(row, p.id === chosenPlace);
            li.appendChild(row);
            list.appendChild(li);
        });
        paint();
    }

    function pickPlace(id) {
        if (busy()) return;
        chosenPlace = id;
        var rows = els["schedule-places"].querySelectorAll(".place");
        for (var i = 0; i < rows.length; i++) markPicked(rows[i], rows[i].dataset.id === id);
        said("");
    }

    /** Fetched once per sheet visit and kept — a directory can go away between two looks, same
     *  reasoning as `input/command.js`'s own copy of this. */
    function ensurePlaces() {
        if (places) return Promise.resolve();
        if (typeof api.places !== "function") { places = []; assistants = []; return Promise.resolve(); }
        return api.places().then(function (d) {
            places = (d && d.places) || [];
            assistants = (d && d.assistants) || [];
        }).catch(function () { places = places || []; });
    }

    function defaultAssistant(preferred) {
        chosenAssistant = preferred && assistants.some(function (a) { return a.id === preferred; })
            ? preferred
            : (assistants.length ? assistants[0].id : null);
    }

    /* ---- open, filled or blank --------------------------------------------- */

    function reset() {
        creating = false;
        places = null; assistants = [];
        chosenPlace = null; chosenAssistant = null;
        days = "daily"; daysGuessed = false; closeTab = "on_success"; enabled = true; notify = true;
        els["schedule-title"].value = "";
        els["schedule-at"].value = "";
        els["schedule-instructions"].value = "";
        els["schedule-catch"].value = String(CATCH_DEFAULT);
        els["schedule-timeout"].value = String(TIMEOUT_DEFAULT);
        // Folded every time: a first schedule should not have to read six settings with working
        // defaults before it can be made — see the comment on `#schedule-more` in index.html.
        els["schedule-more"].open = false;
        said("");
        drawWith(); drawDays(); drawClose(); drawFlags(); drawPlaces();
    }

    /** The `+` beside the Schedules list. Every field blank, "Daily" the one thing already on. */
    function open() {
        if (!els["schedule-form"].hidden) return;
        reset();
        els["schedule-form"].hidden = false;
        els["schedule-title"].focus({ preventScroll: true });
        ensurePlaces().then(function () {
            defaultAssistant(null);
            drawWith(); drawPlaces();
        });
    }

    /** What `input/command.js` calls when a spoken draft comes back `kind: "schedule"`. Holds
     *  whatever the planner worked out and leaves the rest for a person — see the file header.
     *  `instructions` is passed separately, the same way `input/command.js` resolves it for
     *  itself, because an empty first message is a deliberate answer and not "nothing arrived". */
    function openFrom(draft, instructions) {
        reset();
        els["schedule-form"].hidden = false;
        draft = draft || {};
        els["schedule-title"].value = draft.title || "";
        els["schedule-at"].value = draft.at || "";
        var heard = Array.isArray(draft.days)
            ? DAY_CODES.filter(function (c) { return draft.days.indexOf(c) >= 0; })
            : null;
        // Below the bar, an empty `days` is not "every day" — it is the planner never having
        // heard one at all, most often the one-off request this Mac cannot express (see the file
        // header and Planner.swift's own paragraph on it). `daysGuessed` keeps the Daily chip dark
        // in `drawDays` so that guess never reads as a decision already made.
        daysGuessed = !(draft.confidence >= SURE) && !(heard && heard.length);
        days = (heard && heard.length) ? heard : "daily";
        chosenPlace = draft.place_id || null;
        els["schedule-instructions"].value = instructions || "";
        drawDays(); drawPlaces();
        // The planner's own `question` is usually the more specific sentence — "only a repeating
        // time can be set here" beats either built-in hint — so it goes first; `hint()` is what
        // is left for a draft that came back unsure without one.
        said(draft.question || hint());
        els["schedule-title"].focus({ preventScroll: true });
        ensurePlaces().then(function () {
            defaultAssistant(draft.assistant);
            drawWith(); drawPlaces();
        });
    }

    function close() {
        // Refused while a request is in flight — the same rule `input/start.js`'s own `close`
        // and `ActionConfirm.close` follow, and the one the plan spells out for this sheet by
        // name: Escape closes unless a request is in flight.
        if (busy()) return;
        els["schedule-form"].hidden = true;
    }

    /* ---- sending it ---------------------------------------------------------
       `POST /v1/orchestrator/schedules` — the write route `Orchestrator.schedule(from:)` reads,
       behind `writeGate` exactly like `/v1/voice` and `/v1/intents`. */

    function why(e) {
        var code = e && e.code;
        if (code === "offline") return e.message;          // already this page's own sentence
        if (code === "write_disabled") return T.webStartOff;
        // `takeScheduleWriteRate()`'s ten-in-ten-minutes refusal — `rate_limited` is the current
        // code, `busy` is what an older Mac still says for the same wait. Either way the server
        // already wrote the sentence a person can act on ("Try again shortly."), so it is shown
        // as it arrived, the same reasoning as `bad_request` just below.
        if (code === "rate_limited" || code === "busy") return e.message;
        // The parser's own sentence, about the one field it did not like. Shown as it arrived —
        // not translated, not replaced — because it is the only thing that says which field.
        if (code === "bad_request") return e.message;
        return T.webScheduleFailed;
    }

    function create() {
        if (busy()) return;
        if (!S.write) { said(T.webStartOff); return; }
        var problem = hint();
        if (problem) {
            said(problem);
            if (!els["schedule-at"].value) els["schedule-at"].focus({ preventScroll: true });
            return;
        }
        // The one place this sheet is examinable without a Mac that has this route yet — same
        // `typeof` guard as the six other places in this codebase; see `net/api.js:1-15`.
        if (typeof api.createSchedule !== "function") { said(T.webScheduleFailed); return; }

        var catchUp = parseInt(els["schedule-catch"].value, 10);
        if (!(catchUp >= 0)) catchUp = CATCH_DEFAULT;
        var timeout = parseInt(els["schedule-timeout"].value, 10);
        if (!(timeout >= 1)) timeout = TIMEOUT_DEFAULT;

        var payload = {
            title: els["schedule-title"].value.trim(),
            at: els["schedule-at"].value,
            days: days,
            place_id: chosenPlace,
            assistant: chosenAssistant,
            instructions: els["schedule-instructions"].value,
            enabled: enabled,
            close_tab: closeTab,
            catch_up_hours: catchUp,
            notify_on_failure: notify,
            timeout_minutes: timeout
        };

        creating = true;
        said("");
        paint();
        api.createSchedule(payload).then(function () {
            creating = false;
            // Closed rather than left open with a receipt in it — same as `input/command.js`'s
            // own `arrive` — because there is nothing left on this sheet worth looking at. The
            // new row is asked for immediately below rather than waited for.
            close();
            // Ask the list to read again now. It has its own one-minute lane, and waiting for it
            // here would leave the row that was just made missing from the only screen anybody is
            // looking at — see `Schedules.refresh`.
            Schedules.refresh();
            toast(T.webScheduleCreated);
        }).catch(function (e) {
            creating = false;
            said(why(e));
            paint();
        });
    }

    return { open: open, openFrom: openFrom, close: close, create: create, pick: pickPlace };
})();

/* ---- wiring ---------------------------------------------------------------- */

els["schedule-new"].addEventListener("click", function (ev) {
    // Inside the <summary> that is the Schedules section's one line — its click must not reach
    // the disclosure, or opening this sheet folds the list shut behind it in the same tap. See
    // the comment on this button in index.html.
    ev.stopPropagation();
    Schedule.open();
});
// The backdrop closes it; a click inside must not reach the backdrop underneath it — the same
// pair as `input/start.js`'s own `#start` / `#start-sheet`.
els["schedule-form"].addEventListener("click", function () { Schedule.close(); });
els["schedule-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["schedule-cancel"].addEventListener("click", function () { Schedule.close(); });
els["schedule-go"].addEventListener("click", function () { Schedule.create(); });
els["schedule-places"].addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest(".place") : null;
    if (!row || row.disabled) return;
    Schedule.pick(row.dataset.id);
});
// Whatever the sheet last said — a hint, or the sentence a refusal carried — is about a field a
// person is now changing. Left up, it would read as still true of what is on screen now.
els["schedule-title"].addEventListener("input", function () { els["schedule-said"].textContent = ""; });
els["schedule-at"].addEventListener("input", function () { els["schedule-said"].textContent = ""; });

// The focus trap, copied from `input/action-confirm.js`'s own — this sheet ends in the same
// two-button shape, Cancel and a confirming Create, so Tab from the second wraps to the first
// rather than leaving the dialog.
els["schedule-form"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    var items = [els["schedule-cancel"], els["schedule-go"]];
    var at = items.indexOf(document.activeElement);
    if (at < 0) return;
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
        ev.preventDefault(); items[ev.shiftKey ? items.length - 1 : 0].focus();
    }
});
