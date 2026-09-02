import { T, fill } from "../core/i18n.js";
import { commandSpin, drawSpinner, setCommandSpin, spinPhase } from "../core/pixels.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { shortPath, tint } from "../core/util.js";
import { drawIcon } from "../core/pixels.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { openSession } from "../session/open.js";
import { Voice } from "./voice.js";
import { Schedule } from "./schedule.js";

/* ---- saying what to start -------------------------------------------------
   The header microphone and the same one inside this sheet both press through `Voice`, the way
   the composer's own does — see the contract on `Voice.press` in `voice.js`. Whisper's answer
   lands in `#command-text` through `Command.heard`, edited freely, and nothing is sent anywhere
   until Start is pressed: that press is what turns the words into a `POST /v1/intents` call, and
   it is the only step in this whole file that spends anything.

   **The reveal is not always a stop.** When the planner is confident *and* named a project, this
   sheet shows the draft and goes straight on to opening it — the person never presses Start a
   second time. That is the contract's own wording ("go on to step 6") and not a shortcut taken
   here: `instructions` is untrusted prose the model can write anything into (see `Planner.swift`),
   and the reveal is what makes that honest — it is shown *because* it is about to be sent, not
   instead of sending it. Below the confidence line the sheet stops for real: the project is left
   unselected and Start has to be pressed again once a person has picked one.
   ----------------------------------------------------------------------- */
export var Command = (function () {
    // Start's own 15-second hold and its "never retry" rule — see `input/start.js:36-38` — for
    // the same gap between "the tab is open" and "the session is in the list". Start's version of
    // this wait lives in a closure this file cannot reach (no completion hook is exported), so it
    // is reproduced here rather than imported; the constant is the same on purpose.
    var HOLD = 15000;
    var POLL = 400;
    // A session just opened can be sitting on a first-run picker for a moment — see the
    // `showing_a_menu` comment on `/v1/sessions/:id/send` in `RemoteServer.swift`. Four tries at
    // two seconds apart is long enough to clear an ordinary trust prompt without turning a stuck
    // session into a long silent wait.
    var SEND_TRIES = 4;
    var SEND_WAIT = 2000;

    // What the planner is judging the job to be worth — see "Which model" in
    // ~/.config/clawdline/dispatch-policy.md, carried into `Planner.swift`'s own prompt rather
    // than read from that file at request time. Codex expresses difficulty as reasoning effort
    // instead, so this row only ever means something for Claude — see `drawModel`.
    var MODELS = ["haiku", "sonnet", "opus"];

    var phase = "idle";          // idle | thinking | draft | opening
    var places = null;           // GET /v1/places, fetched once and reused for this sheet's life
    var assistants = [];
    var chosenPlace = null;      // a place id, once the planner or a person has picked one
    var chosenAssistant = null;
    // "" or one of `MODELS` — "" is its own answer, "whatever that assistant runs by default",
    // and has to be reachable by pressing the lit chip a second time, the same as the planner
    // never having named one at all.
    var chosenModel = "";
    var openedId = null;         // a session `startPlace` already opened, so a retry sends into it
    /** Bumped whenever somebody abandons what is in flight. Everything asynchronous below carries
     *  the value it began under and drops its own answer when the two no longer match — the shape
     *  `voice.js` uses for a recording cancelled while the Mac is still transcribing. It is what
     *  lets `close` run at any moment instead of refusing while busy. */
    var run = 0;

    function busy() { return phase === "thinking" || phase === "opening"; }

    function sayTop(words) { els["command-say"].textContent = words || ""; }
    /** The status line. `waiting` puts an arc beside the words, on the page's one spinner clock —
     *  the planner takes about five seconds, and five seconds of a sentence that does not move is
     *  the same picture as a sheet that has given up. Anything that is not a wait clears it, so a
     *  refusal never sits there next to something still turning. */
    function sayStatus(words, waiting) {
        var box = els["command-said"];
        if (!waiting) { setCommandSpin(null); box.textContent = words || ""; return; }
        box.innerHTML = '<canvas class="wait-spin"></canvas><span></span>';
        box.lastChild.textContent = words || "";
        setCommandSpin(box.firstChild);
        drawSpinner(commandSpin, spinPhase);
    }

    /* ---- painting ---------------------------------------------------------- */

    function paint() {
        var b = busy();
        els["command-sheet"].setAttribute("aria-busy", b ? "true" : "false");
        els["voice-go"].disabled = b;
        els["command-mic"].disabled = b;
        // Frozen once a draft has been asked for: editing the sentence a draft was built from,
        // without redoing the draft, would leave the reveal describing words that are no longer
        // there. `heard()` is the redo — see its own comment.
        els["command-text"].disabled = phase !== "idle";
        els["command-instructions"].disabled = b;
        var rows = els["command-list"].querySelectorAll(".place");
        for (var i = 0; i < rows.length; i++) rows[i].disabled = b;
        var chips = els["command-with"].querySelectorAll(".chip");
        for (var j = 0; j < chips.length; j++) chips[j].disabled = b;
        var mchips = els["command-model"].querySelectorAll(".chip");
        for (var m = 0; m < mchips.length; m++) mchips[m].disabled = b;
        // Never disabled, `opening` included. The draft on screen is prose a model wrote, and
        // this sheet is the only place a person can read it before it is typed into an agent; a
        // reveal nobody can act on is a receipt rather than a review. Cancelling while a tab is
        // opening no longer stops the tab — that request has gone — but it does stop the sentence.
        els["command-cancel"].disabled = false;
        els["command-go"].disabled = b
            || (phase === "idle" && !els["command-text"].value.trim())
            || (phase === "draft" && !chosenPlace);
    }

    /* ---- the assistant chips and the places list ---------------------------
       Copied from `input/start.js`'s `drawWith` and its `#start-list` row template — same markup,
       same classes, so the frozen CSS in `sheets.css` draws both sheets the same way. */

    function drawWith() {
        var row = els["command-with"];
        row.innerHTML = "";
        row.hidden = assistants.length < 2;
        if (row.hidden) return;
        var label = document.createElement("span");
        label.className = "with-label";
        label.textContent = T.webCommandWith;
        row.appendChild(label);
        assistants.forEach(function (a) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (a.id === chosenAssistant ? " on" : "");
            chip.textContent = a.label || a.id;
            chip.setAttribute("aria-pressed", a.id === chosenAssistant ? "true" : "false");
            chip.disabled = busy();
            chip.onclick = function () {
                chosenAssistant = a.id;
                // A model name is only ever sayable for Claude this round — see `drawModel` — so
                // switching away from it drops whatever was picked rather than carrying a haiku
                // or opus into a `/start/codex/opus` that can only ever 404.
                if (chosenAssistant !== "claude") chosenModel = "";
                drawWith();
                drawModel();
            };
            row.appendChild(chip);
        });
    }

    /// The same shape `drawWith` just built, one row down: what the planner judged the job was
    /// worth, changeable before Start is pressed. `MODELS` is fixed rather than fetched — there
    /// is nowhere on the wire this page could ask "which models does haiku/sonnet/opus mean on
    /// this Mac", and the contract this round settled on is that those three names are what they
    /// are called everywhere (see the plan). Pressing the chip already lit turns it back off:
    /// that is how "" — "whatever that assistant runs by default" — stays reachable once a
    /// person has picked something else, without a fourth chip that says nothing three don't.
    function drawModel() {
        var row = els["command-model"];
        row.innerHTML = "";
        // Codex expresses difficulty as reasoning effort, not a model name — see the plan's
        // scope — and `StartPoints.start` 404s a model segment sent for it, so this stays out of
        // reach rather than offering a chip that can only ever fail.
        row.hidden = chosenAssistant !== "claude";
        if (row.hidden) return;
        var label = document.createElement("span");
        label.className = "with-label";
        label.textContent = T.webCommandModel;
        row.appendChild(label);
        MODELS.forEach(function (m) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (m === chosenModel ? " on" : "");
            chip.textContent = m;
            chip.setAttribute("aria-pressed", m === chosenModel ? "true" : "false");
            chip.disabled = busy();
            chip.onclick = function () { chosenModel = (chosenModel === m) ? "" : m; drawModel(); };
            row.appendChild(chip);
        });
    }

    /// The frozen stylesheet has a highlight for the row a press is *landing on*
    /// (`.place[data-busy="1"]`, in `sheets.css`) but none for the row a person has merely picked
    /// — `sheets.css` is one of the four files nobody here re-edits, so that highlight is applied
    /// inline instead, with the same accent tokens the frozen rule already uses. `aria-pressed`
    /// carries the state for anything that is not looking at the border.
    function markPicked(row, on) {
        row.setAttribute("aria-pressed", on ? "true" : "false");
        row.style.borderColor = on ? "var(--accent-ed)" : "";
        row.style.opacity = on ? "1" : "";
        row.querySelector(".where").style.color = on ? "var(--accent)" : "";
    }

    /// `sheets.css` has no heading of its own for the places list — `.command-sheet .draft .places`
    /// is only spacing — so this borrows `.with-label`, the one `drawWith` already draws to the
    /// same effect one row up, rather than asking the frozen stylesheet for a second rule that
    /// would say the same thing.
    function whereLabel() {
        var label = document.getElementById("command-where-label");
        if (label) return label;
        label = document.createElement("div");
        label.id = "command-where-label";
        label.className = "with-label";
        label.textContent = T.webCommandWhere;
        els["command-list"].parentNode.insertBefore(label, els["command-list"]);
        return label;
    }

    function drawList() {
        whereLabel();
        var list = els["command-list"];
        list.innerHTML = "";
        places.forEach(function (p) {
            var li = document.createElement("li");
            var row = document.createElement("button");
            row.type = "button";
            row.className = "place";
            row.dataset.id = p.id;
            row.disabled = busy();
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
    }

    function pick(id) {
        if (busy()) return;
        chosenPlace = id;
        var rows = els["command-list"].querySelectorAll(".place");
        for (var i = 0; i < rows.length; i++) markPicked(rows[i], rows[i].dataset.id === id);
        paint();
    }

    /** Fetched once per sheet visit and kept — a directory can go away between two looks, same as
     *  Start's own copy, but this sheet is open for a few seconds at most and a mid-air change is
     *  not worth a second round trip for. */
    function ensurePlaces() {
        if (places) return Promise.resolve();
        if (typeof api.places !== "function") { places = []; assistants = []; return Promise.resolve(); }
        return api.places().then(function (d) {
            places = (d && d.places) || [];
            assistants = (d && d.assistants) || [];
        });
    }

    /* ---- the draft ----------------------------------------------------------
       `sure` mirrors `Planner.sure` in `Planner.swift` — 0.5 is where a drafted place is offered
       rather than asked about. It is repeated here rather than fetched because there is nowhere
       on the wire that carries it; the Mac already dropped a low-confidence guess's `question`
       before this ever sees the reply, so this side only has to compare the number. */
    var SURE = 0.5;

    function reveal(draft, instructionsText, manual) {
        // A schedule, not a session — the planner's own `kind`, read here and nowhere upstream of
        // it. This sheet is not the one that shows it: `input/schedule.js` owns everything about
        // making a schedule, including the form's own safety rule that a confident draft still
        // does not create one by itself. Closing rather than leaving this sheet stacked behind
        // the one that opens — nothing here has anything left to say once the hand-off happens.
        if (draft.kind === "schedule") {
            close();
            Schedule.openFrom(draft, instructionsText);
            return;
        }
        chosenAssistant = draft.assistant && assistants.some(function (a) { return a.id === draft.assistant; })
            ? draft.assistant
            : (assistants.length ? assistants[0].id : null);

        var confident = !manual && draft.confidence >= SURE && !!draft.place_id;
        // Below the line the project is left unselected even when the planner named one — a
        // low-confidence guess is not a pick, and showing it selected would read as one.
        chosenPlace = confident ? draft.place_id : null;
        // "" whenever the planner did not judge one, or judged it for an assistant that is not
        // Claude — `Planner.Draft` already answers "" for codex, and `chosenAssistant` above may
        // have fallen back to something the draft never named at all.
        chosenModel = (chosenAssistant === "claude" && MODELS.indexOf(draft.model) >= 0)
            ? draft.model : "";

        els["command-instructions"].value = instructionsText;
        drawWith();
        drawModel();
        drawList();
        els["command-draft"].hidden = false;
        phase = "draft";
        sayTop(T.webCommandDraft);

        if (confident) {
            paint();
            openIt(chosenPlace, chosenAssistant, chosenModel, instructionsText);
            return;
        }

        sayStatus(draft.question || T.webCommandUnsure);
        paint();
    }

    function manualDraft(text) {
        // No `POST /v1/intents` to wait on, so there is nothing to say honestly about while this
        // runs — `ensurePlaces` is the same fetch Start's own sheet makes and is ordinarily well
        // under a second.
        phase = "thinking";
        paint();
        ensurePlaces().then(function () {
            if (phase !== "thinking") return;   // the sheet moved on under this fetch
            reveal({ place_id: null, assistant: assistants.length ? assistants[0].id : "claude",
                     instructions: text, title: "", confidence: 0, question: "" }, text, true);
        }).catch(function () {
            if (phase !== "thinking") return;
            phase = "idle";
            sayStatus(T.webCommandFailed);
            paint();
        });
    }

    function requestDraft() {
        var text = els["command-text"].value.trim();
        if (!text) { sayStatus(T.webCommandEmpty); return; }
        if (!S.write) { sayStatus(T.webStartOff); return; }

        // The one place this sheet is examinable without a Mac: a mock, or an app too old to
        // carry the route, has no `intents` to call — `net/api.js`'s own note explains why this
        // is a `typeof` check on the function rather than a feature flag. A person picks by hand
        // instead of the planner picking for them; nothing about the rest of the flow changes.
        if (typeof api.intents !== "function") { manualDraft(text); return; }

        phase = "thinking";
        sayStatus(T.webCommandThinking, true);
        paint();
        api.intents(text).then(function (d) {
            if (phase !== "thinking") return;   // superseded — the sheet was closed or redone
            var draft = (d && d.draft) || {};
            // `||` was wrong here: an empty first message is a deliberate answer — "open
            // clawdline" and nothing else — and falling back to what was said would type the
            // request to open a session into the session it just opened. Only a field that is
            // not there at all falls back.
            var first = typeof draft.instructions === "string" ? draft.instructions : text;
            return ensurePlaces().then(function () { reveal(draft, first, false); });
        }).catch(function (e) {
            if (phase !== "thinking") return;
            phase = "idle";
            sayStatus(whyIntents(e));
            paint();
        });
    }

    function whyIntents(e) {
        var code = e && e.code;
        if (code === "offline") return e.message;         // already this page's own sentence
        if (code === "write_disabled") return T.webStartOff;
        if (code === "no_planner") return T.webCommandNoPlanner;
        if (code === "busy") return T.webCommandBusy;
        return T.webCommandFailed;
    }

    /* ---- opening it -----------------------------------------------------------
       `api.startPlace` then `api.send` — the same two calls a paired device already has, and
       deliberately not a third route that would start a session *and* type into it in one grant.
       See the comment this task points at, `RemoteServer.swift:298-307`: the orchestrator's own
       dispatch is gated behind a local-only token for exactly this reason, and a phone must never
       be handed a shortcut that does the same thing through the ordinary device grant. */

    function openIt(placeId, assistant, model, instructions) {
        if (!S.write) { finish(T.webStartOff); return; }
        if (typeof api.startPlace !== "function") { finish(T.webCommandFailed); return; }
        var mine = ++run;
        phase = "opening";
        sayStatus(T.webStarting, true);
        paint();
        api.startPlace(placeId, assistant, model).then(function (d) {
            if (mine !== run) return;   // cancelled while the tab was opening
            var id = d && d.id;
            if (!id) {
                // The tab is open — that is what a 2xx here means — there is simply no id to
                // watch or to send to. Start's own sheet says the same thing in this shape.
                finish(T.webStartSlow);
                return;
            }
            openedId = id;
            waitForSession(id, instructions, mine);
        }).catch(function (e) {
            if (mine !== run) return;
            finish(whyStart(e));
        });
    }

    /** `byId` reads the in-memory session list directly rather than anything rendered, so this
     *  needs no hook into the stream or into `render()` — the id appears there the moment the
     *  Mac's own update does, whether or not this sheet's list is what is on screen. */
    function waitForSession(id, instructions, mine) {
        var began = Date.now();
        (function poll() {
            if (mine !== run) return;          // abandoned, or superseded by a later press
            if (phase !== "opening") return;
            if (byId(id)) {
                // Nothing to type is a whole request: somebody asked for a session in a project
                // and said no more than that. Opening it and sending nothing is the answer.
                if (!String(instructions || "").trim()) { arrive(id, mine); return; }
                sendInstructions(id, instructions, SEND_TRIES, mine);
                return;
            }
            if (Date.now() - began >= HOLD) {
                // Never retried, same as Start: the tab exists, and asking again would be a
                // second one. Only the Mac knows what happened next.
                finish(T.webStartSlow);
                return;
            }
            setTimeout(poll, POLL);
        })();
    }

    function sendInstructions(id, instructions, triesLeft, mine) {
        if (mine !== run) return;
        sayStatus(T.webSending, true);
        api.send(id, instructions).then(function () {
            if (mine !== run) return;
            // `close` refuses to run while `busy()` is true, on purpose — Escape and the backdrop
            // both lean on that to stay put during a request. This is the one call site that
            // *is* the request finishing, so the phase steps aside first rather than being read
            // as one more thing still in flight.
            arrive(id, mine);
        }).catch(function (e) {
            if (mine !== run) return;
            if (e && e.code === "showing_a_menu" && triesLeft > 0) {
                setTimeout(function () { sendInstructions(id, instructions, triesLeft - 1, mine); }, SEND_WAIT);
                return;
            }
            // The session opened; only the message did not land. Left in the "draft" phase
            // rather than closed, so a second press of Start tries the send again without
            // reaching for `startPlace` a second time.
            finish(e && e.code === "showing_a_menu" ? T.webWaitingSay : T.sendFailed);
        });
    }

    /** Open, and read to. Somebody who asked for a session wants to be looking at it, not back at
     *  the list wondering which of the rows is theirs — so the sheet closes and this goes there.
     *  `close` first, because it is what lets go of anything still in flight. */
    function arrive(id, mine) {
        if (mine !== run) return;
        phase = "idle";
        close();
        openSession(id);
    }

    function finish(words) {
        phase = "draft";
        sayStatus(words);
        paint();
    }

    function whyStart(e) {
        var code = e && e.code;
        if (code === "offline") return e.message;
        if (code === "write_disabled") return T.webStartOff;
        if (code === "not_found") return T.webStartGone;
        if (e && e.app && code === "terminal_closed") return fill(T.webStartTerminalClosed, { app: e.app });
        // No `e.app` guard: `terminal_unsupported` never carries a name, and its sentence has no
        // hole to fill. See `input/start.js` for why the guard was wrong here.
        if (code === "terminal_unsupported") return T.webStartTerminalUnsupported;
        return T.webStartFailed;
    }

    /* ---- the two presses ---------------------------------------------------- */

    function press() {
        if (busy()) return;
        if (phase === "idle") { requestDraft(); return; }
        if (phase !== "draft") return;
        // A send that failed left a tab already open. Pressing Start again means "try that send
        // once more" — which is what the comment in `sendInstructions` promised and what the code
        // did not do: it reached for `startPlace` again and opened a second tab in the same
        // project. `run` is bumped so the abandoned wait, if any, lets go.
        if (openedId) {
            var mine = ++run;
            phase = "opening";
            paint();
            sendInstructions(openedId, els["command-instructions"].value, SEND_TRIES, mine);
            return;
        }
        if (chosenPlace) openIt(chosenPlace, chosenAssistant, chosenModel, els["command-instructions"].value);
    }

    /* ---- open, close, heard -------------------------------------------------- */

    function reset() {
        phase = "idle";
        places = null; assistants = []; chosenPlace = null; chosenAssistant = null;
        chosenModel = "";
        openedId = null;
        els["command-text"].value = "";
        els["command-instructions"].value = "";
        els["command-draft"].hidden = true;
        els["command-with"].innerHTML = "";
        els["command-model"].innerHTML = "";
        els["command-list"].innerHTML = "";
        var label = document.getElementById("command-where-label");
        if (label) label.remove();
        sayTop(T.webCommandSay); sayStatus("");
        // The paint before this one may have been made while `opening`, which disables both
        // microphones — and one of them is the only way back into this sheet. Without this line
        // the whole feature works exactly once per page load.
        paint();
    }

    function open() {
        if (!els.command.hidden) return;
        els.command.hidden = false;
        // The one thing worth saying before anything has happened: this is a sheet, not a send.
        // Stays true through the wait for a draft as well — see `reveal`, which is what actually
        // changes it — since nothing has reached a session either way.
        sayTop(T.webCommandSay);
        paint();
    }

    function close() {
        // Not refused while something is in flight any more; `run` is what makes that safe. The
        // answer to whatever was asked for arrives at a sheet that has moved on, and is dropped.
        run += 1;
        els.command.hidden = true;
        reset();
    }

    /** What `Voice.press` calls once Whisper answers — see the job passed to it below. A second
     *  recording, arriving after a draft is already showing, is a redo rather than an edit: the
     *  draft on screen describes sentences that no longer exist once new ones replace them, so it
     *  is dropped along with everything it produced. */
    function heard(text) {
        if (!text) return;
        open();
        if (phase !== "idle") reset();
        els["command-text"].value = text;
        paint();
        els["command-text"].focus({ preventScroll: true });
    }

    return { open: open, close: close, heard: heard, press: press, pick: pick, repaint: paint };
})();

/* ---- wiring ---------------------------------------------------------------- */

// Both microphones press through the same job shape — see the contract on `Voice.press` in
// `voice.js`. `guard` reads whether this sheet is still open rather than tracking its own flag:
// the sheet closing *is* the reason to stop, from wherever that close came from.
function job(button) {
    return { host: els["command-voice"], button: button, sink: Command.heard,
             guard: function () { return els.command.hidden; } };
}
els["voice-go"].addEventListener("click", function () { Command.open(); Voice.press(job(els["voice-go"])); });
els["command-mic"].addEventListener("click", function () { Voice.press(job(els["command-mic"])); });

// Typing is the other way in: somebody who declined the microphone, is somewhere they cannot
// speak, or simply prefers a keyboard. `paint` is the only thing that decides whether Start is
// enabled, and until this listener existed nothing called it while a person typed — the textarea
// took the words and the button stayed dead.
els["command-text"].addEventListener("input", function () { Command.repaint(); });
els["command-cancel"].addEventListener("click", function () { Command.close(); });
els["command-go"].addEventListener("click", function () { Command.press(); });
// The backdrop closes it; a click inside must not reach the backdrop underneath it — the same
// pair as `input/start.js:407-410`.
els.command.addEventListener("click", function () { Command.close(); });
els["command-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["command-list"].addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest(".place") : null;
    if (!row || row.disabled) return;
    Command.pick(row.dataset.id);
});

// The focus trap, copied from `input/action-confirm.js:145-153` — this sheet ends in the same
// two-button shape, Cancel and a confirming Start, so Tab from the second wraps to the first
// rather than leaving the dialog.
els.command.addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    var items = [els["command-cancel"], els["command-go"]];
    var at = items.indexOf(document.activeElement);
    if (at < 0) return;
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
        ev.preventDefault(); items[ev.shiftKey ? items.length - 1 : 0].focus();
    }
});
