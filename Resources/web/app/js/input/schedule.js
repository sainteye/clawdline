import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { shortPath, tint, toast } from "../core/util.js";
import { drawIcon } from "../core/pixels.js";
import { api } from "../net/api.js";
import { Schedules } from "../net/schedules.js";

/* ---- making, changing and removing a schedule ------------------------------
   The markup, the ids and the styling are `#schedule-form` in `index.html` and
   `.schedule-sheet` in `sheets.css` — both frozen for *making* one, both drawn once in `bfb4c10`.
   This file is what fills them in, and what happens when Create — now also Save — is pressed.

   **Three doors, one sheet.** `open()` is the `+` beside the Schedules list: every field blank,
   the seven-days-and-daily row defaulting to daily, and a person filling in the rest by hand.
   `openFrom(draft, instructions)` is the second — `input/command.js` calls it when a spoken
   draft comes back with `kind: "schedule"` — and it is the same sheet holding whatever the
   planner could work out, with whatever it could not left for a person to answer. `openEdit(id)`
   is the third: Edit in the run-history sheet, filled from `GET
   /v1/orchestrator/schedules/:id` rather than from a person or a draft. Nothing sends by itself
   through any of the three: **a
   confident schedule draft still does not create one**, and opening a row to look does not
   change or remove it either. Opening a session now and arranging one to run every morning are
   not the same risk, and only the first of those two happens without a press.

   `POST /v1/orchestrator/schedules` (making one) and `PATCH .../schedules/:id` (saving one
   already made) are gated exactly like `/v1/voice` and `/v1/intents` — the write switch, the
   `send` capability, an Idempotency-Key — never the orchestrator token, which is local-only and
   could not reach a phone. `DELETE .../schedules/:id` is behind the same three, reached only
   through the confirm sheet below rather than through Save. A 400 from any of the three carries a
   sentence the Swift parser wrote about which field was wrong; that sentence is shown as it
   arrived; see `why` below.
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
    // Mirrors `MODELS` in input/command.js — same reasoning as `SURE` above, and the same three
    // names: haiku, sonnet and opus, untranslated everywhere they appear. Codex expresses
    // difficulty as reasoning effort instead, so this only ever means something for Claude — see
    // `drawModel`.
    var MODELS = ["haiku", "sonnet", "opus"];

    var places = null;          // GET /v1/places, fetched once and reused for this sheet's life
    var assistants = [];
    var chosenPlace = null;     // a place id, once the planner or a person has picked one
    // What the schedule's own file says its project is, kept only for when `chosenPlace` above
    // could not be resolved to one — see `fillFromRecord` and `drawPicked`.
    var chosenPlacePath = null;
    var chosenAssistant = null;
    // "" or one of `MODELS` — see the same field on `Command` in input/command.js. Sent as
    // `task.model` in the create/save payload; "" leaves it out, the same as never having judged
    // one at all.
    var chosenModel = "";
    // "daily" or a non-empty array of DAY_CODES — never an empty array. See `toggleDay`.
    var days = "daily";
    // True only when `days` above is "daily" because nothing was heard, not because "daily" was
    // actually the answer — see `openFrom`. Cleared the moment a person touches a day chip, so it
    // never outlives the one draft it was about.
    var daysGuessed = false;
    var closeTab = "on_success";
    var enabled = true;
    var notify = true;
    var creating = false;       // the POST or the PATCH is in flight
    // Non-null while this sheet is the edit door rather than the make-one door — the id being
    // saved to or deleted, not a schedule pending creation. `reset()` clears it; `openEdit` sets
    // it back once it knows which row was pressed.
    var editingId = null;
    var loadingEdit = false;    // `openEdit`'s own GET is in flight, filling the sheet in
    // The DELETE from `#schedule-delete-confirm` is in flight. Kept apart from `busy()` above:
    // that overlay hides `#schedule-form` for as long as it is open (see `askDelete`), so there
    // is nothing on the sheet itself left to disable — only the confirm's own two buttons are.
    var deleteBusy = false;

    function busy() { return creating || loadingEdit; }

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
         "schedule-timeout", "schedule-cancel", "schedule-go", "schedule-delete"].forEach(function (id) {
            els[id].disabled = b;
        });
        ["schedule-with", "schedule-model", "schedule-days", "schedule-close", "schedule-flags"]
            .forEach(function (id) {
                var chips = els[id].querySelectorAll(".chip");
                for (var i = 0; i < chips.length; i++) chips[i].disabled = b;
            });
        var rows = els["schedule-places"].querySelectorAll(".place");
        for (var j = 0; j < rows.length; j++) rows[j].disabled = b;
    }

    /* ---- the assistant chips, copied from input/start.js's own drawWith --------- */
    /** The Mac's own answer to the same question — Settings.swift's schedule form, on the same
     *  choice — repeated here: the list is what this Mac has installed, plus, when the schedule
     *  already names one it does not have, the one it names. A row written for Codex on a Mac
     *  that has since lost it is still that row's answer, and a chip row that quietly reads
     *  Claude Code over a file that says `codex` is a form lying about what it holds — see
     *  `defaultAssistant` below, and finding 2 of the plan's review. */
    function drawWith() {
        var row = els["schedule-with"];
        row.innerHTML = "";
        var choices = assistants.slice();
        if (chosenAssistant && !choices.some(function (a) { return a.id === chosenAssistant; })) {
            choices.push({ id: chosenAssistant, label: chosenAssistant });
        }
        // One choice is not a choice — but note this counts the assistant already picked, not
        // just what is installed, so a schedule naming one this Mac does not have still shows the
        // row rather than hiding the one place that names it.
        row.hidden = choices.length < 2;
        // The heading goes with it: a label for a control that is not there is a field somebody
        // will look for.
        els["schedule-with-label"].hidden = row.hidden;
        if (row.hidden) return;
        choices.forEach(function (a) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (a.id === chosenAssistant ? " on" : "");
            chip.textContent = a.label || a.id;
            chip.setAttribute("aria-pressed", a.id === chosenAssistant ? "true" : "false");
            chip.onclick = function () {
                chosenAssistant = a.id;
                // Same reasoning as `Command`'s own version of this: a model name only means
                // anything for Claude this round, so switching away from it drops whatever was
                // picked rather than saving a haiku or opus against a Codex schedule.
                if (chosenAssistant !== "claude") chosenModel = "";
                drawWith();
                drawModel();
            };
            row.appendChild(chip);
        });
        paint();
    }

    /// The same judgement `input/command.js`'s own draft sheet offers, one row down in "More" —
    /// see `drawModel` there, which this copies rather than imports for the reason `MODELS`
    /// above is repeated instead of shared. Pressing the lit chip again turns it back off, which
    /// is how "" — nothing judged, or nothing worth saving over what the Mac already had — stays
    /// reachable without a fourth chip.
    function drawModel() {
        var row = els["schedule-model"];
        row.innerHTML = "";
        var show = chosenAssistant === "claude";
        els["schedule-model-label"].hidden = !show;
        row.hidden = !show;
        if (!show) return;
        MODELS.forEach(function (m) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (m === chosenModel ? " on" : "");
            chip.textContent = m;
            chip.setAttribute("aria-pressed", m === chosenModel ? "true" : "false");
            chip.onclick = function () { chosenModel = (chosenModel === m) ? "" : m; drawModel(); };
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
        // Nothing to offer is an answer and has to look like one — and so is not having managed
        // to ask. An empty bordered box is the shape of something broken, and it is exactly what
        // both of these used to draw.
        var why = placesFailed ? T.webOffline : ((places && !places.length) ? T.webStartEmpty : "");
        if (why) {
            var none = document.createElement("li");
            none.className = "note";
            none.textContent = why;
            list.appendChild(none);
        }
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
        drawPicked();
        paint();
    }

    /** The project as one line. Shut, it is the value of this field; open, it is a chevron with
     *  the list under it. Nothing is preselected — with no project chosen it says so in words
     *  rather than drawing the first row as though somebody had picked it.
     *
     *  A third state sits between "chosen" and "nothing chosen": `chosenPlacePath` is set when
     *  `fillFromRecord` could not resolve the schedule's own `project_dir` to anything on
     *  `/v1/places`'s capped recent list — too old for the cap, or a directory this device has
     *  never opened. That is not "nothing chosen": the file has an answer, this sheet just
     *  cannot show it as a pickable row, and showing the placeholder prompt in its place would
     *  read as though the schedule had lost its project rather than as this device's own list
     *  being incomplete. See "the project that fell off the list" in the plan. */
    function drawPicked() {
        var box = els["schedule-picked"];
        var open = box.getAttribute("aria-expanded") === "true";
        var p = (places || []).filter(function (x) { return x.id === chosenPlace; })[0];
        box.innerHTML = '<canvas></canvas><span class="name"></span>'
            + '<span class="where"></span><span class="chev"></span>';
        var mark = box.querySelector("canvas");
        // `drawIcon` is what sizes the canvas, including zeroing it when there is nothing to
        // draw — short-circuiting past it on a missing project leaves a canvas at its HTML
        // default of 300x150, which is exactly wide enough to burst this row and the sheet with
        // it. Call it either way and let the `.none` rule below supply the placeholder.
        if (!drawIcon(mark, p && p.icon, 4)) mark.classList.add("none");
        // Only the true "nothing chosen" state reads as a placeholder — a stale path is real
        // information, not a prompt, and dimming it the same way would say the opposite of what
        // is true.
        box.classList.toggle("none", !p && !chosenPlacePath);
        var name = box.querySelector(".name");
        if (p) {
            name.textContent = p.label || p.path;
        } else if (chosenPlacePath) {
            name.textContent = shortPath(chosenPlacePath);
        } else {
            // Borrowed rather than invented: the start sheet already asks this question, in
            // fourteen languages, and it is the same question.
            name.textContent = T.webStartPick;
        }
        name.style.color = p && p.icon ? tint(p.icon.accent) : "";
        box.querySelector(".where").textContent = p ? shortPath(p.path) : "";
        box.querySelector(".chev").textContent = open ? "\u2304" : "\u203A";
    }

    function togglePlaces() {
        if (busy()) return;
        var box = els["schedule-picked"];
        var open = box.getAttribute("aria-expanded") === "true";
        box.setAttribute("aria-expanded", open ? "false" : "true");
        els["schedule-places"].hidden = open;
        drawPicked();
    }

    function pickPlace(id) {
        if (busy()) return;
        chosenPlace = id;
        // A real pick settles the question `chosenPlacePath` was standing in for.
        chosenPlacePath = null;
        var rows = els["schedule-places"].querySelectorAll(".place");
        for (var i = 0; i < rows.length; i++) markPicked(rows[i], rows[i].dataset.id === id);
        // Chosen is chosen. Folding it back up is what puts the rest of the form on screen again,
        // which is the whole reason this field collapses at all.
        els["schedule-picked"].setAttribute("aria-expanded", "false");
        els["schedule-places"].hidden = true;
        drawPicked();
        said("");
    }

    /** Fetched once per sheet visit and kept — a directory can go away between two looks, same
     *  reasoning as `input/command.js`'s own copy of this. */
    /** **An empty array is truthy.** The first version remembered `[]` as "already loaded", so a
     *  single failed read — the Mac still waking up, or `/v1/places` answering `busy` because
     *  eight slow reads were already in hand — left this sheet with no projects and no assistants
     *  for the life of the page, and nothing on screen saying why. A failure leaves `places` null
     *  so the next open asks again; only a real answer is remembered. */
    var placesFailed = false;

    function ensurePlaces() {
        if (places && places.length) return Promise.resolve();
        placesFailed = false;
        if (typeof api.places !== "function") { places = []; assistants = []; return Promise.resolve(); }
        return api.places().then(function (d) {
            places = (d && d.places) || [];
            assistants = (d && d.assistants) || [];
        }).catch(function () { places = null; assistants = []; placesFailed = true; });
    }

    /** `preferred` is trusted outright now rather than only when this Mac has it installed — a
     *  schedule that already names an assistant keeps naming it, exactly like a fresh sheet with
     *  nothing chosen yet (`preferred` null, from `open()`) still falls back to the first
     *  installed one. The previous version's `assistants.some(...)` check silently substituted
     *  the first installed assistant whenever it did not, which is finding 2 of the plan's
     *  review: open a schedule naming an assistant this Mac has since lost, press Save having
     *  touched nothing else, and it is no longer that assistant. `drawWith` is what keeps the
     *  substitution from being invisible now — it shows `preferred` as its own chip even when
     *  this Mac does not have it. */
    function defaultAssistant(preferred) {
        chosenAssistant = preferred || (assistants.length ? assistants[0].id : null);
    }

    /* ---- open, filled or blank --------------------------------------------- */

    function reset() {
        creating = false;
        editingId = null;
        loadingEdit = false;
        places = null; assistants = [];
        chosenPlace = null; chosenPlacePath = null; chosenAssistant = null; chosenModel = "";
        days = "daily"; daysGuessed = false; closeTab = "on_success"; enabled = true; notify = true;
        els["schedule-title"].value = "";
        els["schedule-at"].value = "";
        els["schedule-instructions"].value = "";
        els["schedule-catch"].value = String(CATCH_DEFAULT);
        els["schedule-timeout"].value = String(TIMEOUT_DEFAULT);
        // Folded every time: a first schedule should not have to read six settings with working
        // defaults before it can be made — see the comment on `#schedule-more` in index.html.
        els["schedule-more"].open = false;
        // And so is the project, for the same reason and one more: left open from last time, the
        // list would be back to filling the sheet before anybody had asked it to.
        els["schedule-picked"].setAttribute("aria-expanded", "false");
        els["schedule-places"].hidden = true;
        // Create mode by default. `openEdit` overwrites these four the moment it knows better;
        // `open` and `openFrom` are both fresh schedules and leave them exactly as this sets them.
        els["schedule-form-title"].textContent = T.webScheduleNew;
        els["schedule-go"].textContent = T.webScheduleCreate;
        els["schedule-delete"].hidden = true;
        // `#schedule-form-say`, unlike `#schedule-said` that `said()` clears below, is set once at
        // boot (`view/static.js`) and never again — so an edit sheet reopened after a create left
        // it saying this, true here and nowhere else. See `openEdit`.
        els["schedule-form-say"].textContent = T.webScheduleNewSay;
        said("");
        drawWith(); drawModel(); drawDays(); drawClose(); drawFlags(); drawPlaces();
    }

    /** The `+` beside the Schedules list. Every field blank, "Daily" the one thing already on. */
    function open() {
        if (!els["schedule-form"].hidden) return;
        reset();
        els["schedule-form"].hidden = false;
        els["schedule-title"].focus({ preventScroll: true });
        ensurePlaces().then(function () {
            defaultAssistant(null);
            drawWith(); drawModel(); drawPlaces();
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
            // Same rule as `reveal` in input/command.js: "" whenever the planner did not judge
            // one, or judged it for an assistant `defaultAssistant` did not end up choosing.
            chosenModel = (chosenAssistant === "claude" && MODELS.indexOf(draft.model) >= 0)
                ? draft.model : "";
            drawWith(); drawModel(); drawPlaces();
        });
    }

    /** The reverse of what `chosenPlace` normally holds: the file has a path, the form has an id,
     *  and `GET /v1/orchestrator/schedules/:id` only ever answers with the first. A place this
     *  device has not opened recently — or has never opened at all — leaves nothing picked, the
     *  same as a fresh `open()` before anything has been chosen; a person has to pick again
     *  rather than the sheet guessing at an id nothing on this list can confirm. */
    function placeIdForPath(path) {
        var match = (places || []).filter(function (p) { return p.path === path; })[0];
        return match ? match.id : null;
    }

    /** The third door: Edit in a schedule's run-history sheet. `GET
     *  /v1/orchestrator/schedules/:id`
     *  is read-level, the same door `schedules` itself is behind — a paired device with no `send`
     *  can still open a row and look — so this opens and starts filling itself in regardless of
     *  `S.write`. Only `create` (which now also saves) and the delete confirm below check that,
     *  the moment there is something to send. */
    function openEdit(id) {
        if (!els["schedule-form"].hidden || !els["schedule-delete-confirm"].hidden) return;
        reset();
        editingId = id;
        loadingEdit = true;
        els["schedule-form-title"].textContent = T.webScheduleEdit;
        els["schedule-go"].textContent = T.webScheduleSave;
        // "Nothing is scheduled until you press Create below." — true of `reset()`'s default,
        // false of a row that already exists and a button that already says Save. There is no
        // dedicated sentence for this sheet in edit mode (see the plan's note on inventing keys),
        // so this is left blank rather than saying something wrong.
        els["schedule-form-say"].textContent = "";
        els["schedule-delete"].hidden = false;
        els["schedule-form"].hidden = false;
        paint();
        if (typeof api.schedule !== "function") {
            loadingEdit = false;
            said(T.webRequestFailed);
            paint();
            return;
        }
        api.schedule(id).then(function (d) {
            fillFromRecord((d && d.schedule) || {});
        }).catch(function (e) {
            loadingEdit = false;
            // Not `T.webScheduleFailed` — that sentence names "create", and this failed to read,
            // not to make one. See `why` and finding 6 in the plan.
            said(why(e, T.webRequestFailed));
            paint();
        });
    }

    /** What `openEdit`'s GET fills the sheet with. Every field this reads has a counterpart
     *  `create` already knows how to send back — see the field list on `POST
     *  /v1/orchestrator/schedules` in the plan — so nothing here is new shape, only a new
     *  direction for shape that already existed. */
    function fillFromRecord(record) {
        var when = record.when || {};
        var task = record.task || {};
        els["schedule-title"].value = record.title || "";
        els["schedule-at"].value = when.at || "";
        var heard = Array.isArray(when.days)
            ? DAY_CODES.filter(function (c) { return when.days.indexOf(c) >= 0; })
            : null;
        days = (heard && heard.length) ? heard : "daily";
        daysGuessed = false;
        els["schedule-instructions"].value = task.instructions || "";
        enabled = record.enabled !== false;
        closeTab = CLOSE_VALUES.indexOf(record.close_tab) >= 0 ? record.close_tab : "on_success";
        notify = record.notify_on_failure !== false;
        els["schedule-catch"].value =
            String(record.catch_up_hours != null ? record.catch_up_hours : CATCH_DEFAULT);
        els["schedule-timeout"].value =
            String(task.timeout_minutes != null ? task.timeout_minutes : TIMEOUT_DEFAULT);
        drawDays(); drawClose(); drawFlags();
        said("");
        ensurePlaces().then(function () {
            chosenPlace = placeIdForPath(task.project_dir);
            // What the file itself says, kept for `drawPicked` when the line above came back
            // null — a project too old for `/v1/places`'s cap, or never opened on this device,
            // is not the same as a schedule with no project. See `drawPicked`.
            chosenPlacePath = chosenPlace ? null : (task.project_dir || null);
            defaultAssistant(task.assistant);
            // Same rule as above: a file naming a model against an assistant this device no
            // longer resolves to Claude is not shown as though it were still choosable here.
            chosenModel = (chosenAssistant === "claude" && MODELS.indexOf(task.model) >= 0)
                ? task.model : "";
            loadingEdit = false;
            drawWith(); drawModel(); drawPlaces();
            paint();
        });
    }

    function close() {
        // Refused while a request is in flight — the same rule `input/start.js`'s own `close`
        // and `ActionConfirm.close` follow, and the one the plan spells out for this sheet by
        // name: Escape closes unless a request is in flight.
        if (busy()) return;
        els["schedule-form"].hidden = true;
    }

    /** Deleting, asked about first. Held behind `#schedule-form`'s own overlay slot rather than
     *  stacked over it — this hides the edit sheet and shows the confirm in its place, so
     *  cancelling restores it exactly as it was, and there is only ever one dialog for `keys.js`'s
     *  own Escape handling to know about (it has no case for `#schedule-delete-confirm` — see the
     *  capture-phase listener at the bottom of this file, which is how Escape still reaches it). */
    function askDelete() {
        if (busy() || !editingId) return;
        if (!S.write) { said(T.webStartOff); return; }
        els["schedule-form"].hidden = true;
        deleteBusy = false;
        els["schedule-delete-confirm-title"].textContent = T.webScheduleDelete;
        // `{title}` — the one schedule this press is about, not a generic warning. It is still on
        // `#schedule-title` at this point: `openEdit`/`fillFromRecord` put it there and nothing
        // between then and here clears it.
        els["schedule-delete-confirm-say"].textContent =
            fill(T.webScheduleDeleteAsk, { title: els["schedule-title"].value });
        els["schedule-delete-confirm-go"].textContent = T.webScheduleDelete;
        els["schedule-delete-confirm"].hidden = false;
        paintDeleteConfirm();
        els["schedule-delete-confirm-go"].focus({ preventScroll: true });
    }

    function closeDeleteConfirm(restore) {
        if (deleteBusy) return;
        els["schedule-delete-confirm"].hidden = true;
        if (restore) {
            els["schedule-form"].hidden = false;
            els["schedule-delete"].focus({ preventScroll: true });
        }
    }

    function paintDeleteConfirm() {
        els["schedule-delete-confirm-sheet"].setAttribute("aria-busy", deleteBusy ? "true" : "false");
        els["schedule-delete-confirm-cancel"].disabled = deleteBusy;
        els["schedule-delete-confirm-go"].disabled = deleteBusy;
    }

    function confirmDelete() {
        if (deleteBusy || !editingId) return;
        if (typeof api.deleteSchedule !== "function") {
            closeDeleteConfirm(true);
            // Not `T.webScheduleFailed` — that names "create", and this is a delete. See `why`.
            said(T.webRequestFailed);
            return;
        }
        var id = editingId;
        deleteBusy = true;
        paintDeleteConfirm();
        api.deleteSchedule(id).then(function () {
            deleteBusy = false;
            // Nothing to restore — the row this sheet was editing is gone, so there is nothing
            // left on `#schedule-form` worth looking at either. Both stay closed.
            els["schedule-delete-confirm"].hidden = true;
            editingId = null;
            Schedules.refresh();
            toast(T.webScheduleDeleted);
        }).catch(function (e) {
            deleteBusy = false;
            closeDeleteConfirm(true);
            said(why(e, T.webRequestFailed));
        });
    }

    /* ---- sending it ---------------------------------------------------------
       `POST /v1/orchestrator/schedules` making one, `PATCH /v1/orchestrator/schedules/:id`
       saving one already made — the write routes `Orchestrator.schedule(from:)` reads either
       way, behind `writeGate` exactly like `/v1/voice` and `/v1/intents`. `DELETE` is the third,
       reached only through `#schedule-delete-confirm` above rather than through this button. */

    /** `fallback` is what to say when the server gave no sentence of its own to show — and it is
     *  the caller's to name, not this function's to guess. `T.webScheduleFailed` ("Could not
     *  create the schedule.") used to be hard-coded here and answer for a failed save, a failed
     *  delete and a failed read alike — a delete that failed would say it could not be created.
     *  There is no dedicated "could not save" or "could not delete" sentence yet (see the plan's
     *  note on inventing keys), so every caller but a genuine create passes the generic
     *  `T.webRequestFailed` instead. See finding 6 in the plan. */
    function why(e, fallback) {
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
        // The GET this sheet opens with, and the PATCH or DELETE it can now send, all three 404
        // with the same plain sentence when the file is gone — a second tab's delete, most often.
        // Shown as it arrived, same reasoning as `bad_request` above.
        if (code === "not_found") return e.message;
        return fallback;
    }

    /** Create and Save both land here — one button, and `editingId` says which of the two this
     *  press actually is. Everything above the request itself is identical on purpose: the same
     *  hint, the same guard, the same payload shape, because a save is a create that already
     *  knows its id. */
    function create() {
        if (busy()) return;
        if (!S.write) { said(T.webStartOff); return; }
        var problem = hint();
        if (problem) {
            said(problem);
            if (!els["schedule-at"].value) els["schedule-at"].focus({ preventScroll: true });
            return;
        }
        var editing = !!editingId;
        // The one place this sheet is examinable without a Mac that has these routes yet — same
        // `typeof` guard as the six other places in this codebase; see `net/api.js:1-15`.
        if (typeof (editing ? api.updateSchedule : api.createSchedule) !== "function") {
            // "Could not create the schedule." is only true of the create half of this button —
            // see `why` and finding 6 in the plan.
            said(editing ? T.webRequestFailed : T.webScheduleFailed);
            return;
        }

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
            model: chosenModel,
            instructions: els["schedule-instructions"].value,
            enabled: enabled,
            close_tab: closeTab,
            catch_up_hours: catchUp,
            notify_on_failure: notify,
            timeout_minutes: timeout
        };
        // `schedule_id` and `created_at` are deliberately not fields on this object. Neither is
        // on `POST`'s own allowed list either — the server assigns both, and keeps both exactly
        // as they were across a `PATCH`, which is the entire reason a save cannot rewrite when a
        // schedule was made. See the plan's own paragraph on it.

        creating = true;
        said("");
        paint();
        var request = editing ? api.updateSchedule(editingId, payload) : api.createSchedule(payload);
        request.then(function (d) {
            creating = false;
            // Closed rather than left open with a receipt in it — same as `input/command.js`'s
            // own `arrive` — because there is nothing left on this sheet worth looking at. The
            // row is asked for immediately below rather than waited for.
            close();
            // Ask the list to read again now. It has its own one-minute lane, and waiting for it
            // here would leave the row that was just made or just changed missing from the only
            // screen anybody is looking at — see `Schedules.refresh`.
            Schedules.refresh();
            // `dispatch_enabled` rides beside a *made* schedule, not a saved one — writing a file
            // is not dispatching, and this Mac may have that switch off in Settings regardless.
            // False still means the file is written and valid; only "Created." would be the lie,
            // since nothing runs it until that switch is on.
            if (!editing && d && d.dispatch_enabled === false) { toast(T.webScheduleDispatchOff); }
            else { toast(editing ? T.webScheduleSaved : T.webScheduleCreated); }
        }).catch(function (e) {
            creating = false;
            said(why(e, editing ? T.webRequestFailed : T.webScheduleFailed));
            paint();
        });
    }

    return { open: open, openFrom: openFrom, openEdit: openEdit, close: close, create: create,
             pick: pickPlace, toggle: togglePlaces, askDelete: askDelete,
             closeDeleteConfirm: closeDeleteConfirm, confirmDelete: confirmDelete };
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
els["schedule-picked"].addEventListener("click", function () { Schedule.toggle(); });
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

els["schedule-delete"].addEventListener("click", function () { Schedule.askDelete(); });
els["schedule-delete-confirm"].addEventListener("click", function () { Schedule.closeDeleteConfirm(true); });
els["schedule-delete-confirm-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["schedule-delete-confirm-cancel"].addEventListener("click", function () { Schedule.closeDeleteConfirm(true); });
els["schedule-delete-confirm-go"].addEventListener("click", function () { Schedule.confirmDelete(); });
els["schedule-delete-confirm"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    var items = [els["schedule-delete-confirm-cancel"], els["schedule-delete-confirm-go"]];
    var at = items.indexOf(document.activeElement);
    if (at < 0) return;
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
        ev.preventDefault(); items[ev.shiftKey ? items.length - 1 : 0].focus();
    }
});
// `keys.js` has a case for `#action-confirm` and `#schedule-form`; it has none for this overlay,
// and it is not this round's file to add one to. Capture, ahead of its own bubble-phase document
// listener, is how `input/action-confirm.js` solves the same problem for `#session-actions`'s own
// nested confirm — copied here rather than invented, for the same reason.
document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape" || els["schedule-delete-confirm"].hidden) return;
    ev.preventDefault(); ev.stopPropagation();
    Schedule.closeDeleteConfirm(true);
}, true);
