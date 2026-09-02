import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { clockOf, shortPath, tint, toast } from "../core/util.js";
import { bandSpin, drawIcon, drawSpinner, setBandSpin, setStartSpin, spinPhase, spinners, startSpin } from "../core/pixels.js";
import { api } from "../net/api.js";
import { byId, bySessionId } from "../view/derive.js";
import { renderList } from "../view/list.js";
import { renderComposer } from "../view/composer.js";
import { Waits } from "../view/waits.js";
import { openSession } from "../session/open.js";
import {
    clawdfatherCreationChoice,
    clawdfatherCreationLabel,
    createClawdfatherAssignmentState,
    createClawdfatherCoordinatorLoader
} from "./clawdfather.js";
import { coordinatorPresenceText } from "./coordinator-actions.js";

/* ---- starting a session -------------------------------------------------- */

/**
 * A new Claude Code session, started from the sofa.
 *
 * **The page never names a directory.** It shows a list the Mac built — the places `claude` has
 * actually been run in, that are still there — and sends back the opaque id printed on the row
 * it was given. There is no field on that route a path could be written into, so there is
 * nothing here that could be persuaded to start a session somewhere the Mac did not offer.
 *
 * **It is its own sheet.** The one behind the wordmark is what is true of *this browser on this
 * device*; this is a question about the Mac, and putting them together would have made one sheet
 * that answers two different questions. It is one tap from the list and it costs the list
 * nothing — a square on a row that already exists, rather than a control the session list has to
 * make room for on every screen for ever.
 *
 * **Picking one back up is the same sheet, one step further in.** The switch above the list
 * decides what the next press on a project means: begin a conversation there, or show the ones
 * the selected assistant has already recorded there and carry one of them on. That second screen
 * is the same list, the same filter box and the same press — which is why it is a mode of this sheet
 * rather than a sheet of its own — and it obeys the same rule as everything else here: the page
 * never names a conversation, it sends back an id off a list the Mac just built.
 *
 * **A conversation something is writing to right now is not resumable.** Two processes on one
 * transcript is a corrupted record, so those rows say so and go to the session instead — which
 * is what somebody who tapped one wanted anyway. It is the Mac that decides which those are:
 * `live` arrives on the row.
 *
 * **The gap is the hard part.** `POST …/start` answers *before the session exists*: the id it
 * gives back is in the same space as every id in `/v1/sessions`, but that session is not in the
 * list yet and asking for it directly would `404` for a moment. So the id is kept and the list
 * is watched — for the **id**, not for `isClaude`, which stays false until `claude` itself is up
 * — and what is on screen meanwhile is both the persistent line under the header and a project-
 * shaped placeholder at the top of the list. The latter remains deliberately outside the
 * session collection: it has no state, transcript, count or keyboard position, only enough of
 * the chosen place to show where the real row is arriving. After fifteen seconds both stop
 * waiting and the line says the tab is open but nothing has reported in. **It never retries.**
 * The tab exists; a retry is a second tab, and "did that work?" has never meant "do it again".
 */
export var Start = (function () {
    var HOLD = 15000;    // how long the band waits before it admits it has stopped waiting
    var MANY = 8;        // places, past which a box to filter them earns its row

    var places = null;   // as the Mac sent them; null until an answer has arrived
    var assistants = [];  // what the Mac will start — [{ id, label }], its list and not this one
    var with_ = null;     // which of them the next press opens; null until the list arrives
    var loading = false;
    var pressing = null; // the place being started, while that one request is in flight
    var find = "";       // what has been typed into the sheet's filter
    var resume = false;  // whether the next press picks a conversation up rather than starting one
    var at = null;       // the place whose conversations are on screen; null while showing places
    var pasts = null;    // as the Mac sent them, for `at`; null until an answer has arrived
    var capped = false;  // the Mac stopped listing before the end, and said so
    var reading = false;
    var coordinatorPayload = null; // durable device Bearings; null while its read is in flight
    var coordinatorFailed = false;
    var wait = null;     // { id, from, late, place } — started, and not in the list yet
    var timer = null;
    var placeholderNode = null;
    // Held at the placeholder's position through the first following stream update. That first
    // paint is the promised in-place replacement; after it, ordinary list ordering takes over
    // through the same FLIP path as every other move rather than making the arrival itself jump.
    var landed = null;

    var assignmentState = createClawdfatherAssignmentState({
        timeoutMs: HOLD,
        onTimeout: function () {
            toast(T.webClawdfatherRegisterLate, true);
        },
        // `result.state` is what this browser did; `result.choice.state` is what the Mac said.
        // Both can read "blocked" and they are not the same word: the outer one means nothing
        // was typed, the inner one means the coordinator record must not be written over.
        onSettled: function (result) {
            if (result && result.state === "sent") {
                toast(T.webClawdfatherRegisterSent);
                return;
            }
            if (result && result.state === "blocked") {
                var choice = result.choice || {};
                var coordinator = choice.coordinator || {};
                if (choice.state === "assigned") {
                    toast(coordinatorPresenceText(coordinator));
                } else if (choice.state === "blocked") {
                    toast(T.webClawdfatherRegisterBlocked, true);
                } else {
                    toast(T.webCoordReadFailed, true);
                }
                return;
            }
            var error = result && result.error;
            toast(error && error.message ? error.message : T.webCoordReadFailed, true);
        }
    });
    var coordinatorLoader = createClawdfatherCoordinatorLoader(api, function (state) {
        coordinatorPayload = state.payload;
        coordinatorFailed = state.failed === true;
        draw();
    });

    function say(words) { els["start-say"].textContent = words || ""; }
    function said(words) { els["start-said"].textContent = words || ""; }

    /**
     * What a refusal means, said in this page's own words.
     *
     * The server's `message` is English, and the reader may not be — so the codes that have a
     * translated sentence get it, and the terminal's name comes out of the error object rather
     * than out of the sentence it was written into. Everything else is one dead end: `forbidden`
     * is a token without the capability, `bad_request` is this page having sent something wrong,
     * and neither is a thing the person holding the phone can do anything about.
     */
    function why(e) {
        var code = e && e.code;
        if (code === "offline") return e.message;          // already this page's own sentence
        if (code === "write_disabled") return T.webStartOff;
        if (code === "not_found") return T.webStartGone;
        // `terminal_closed` carries `app` and its sentence is written around the name; without
        // one there is nothing to write, because a translation with `{app}` still in it is worse
        // than the plain refusal, so that one falls through.
        if (e && e.app && code === "terminal_closed") return fill(T.webStartTerminalClosed, { app: e.app });
        // `terminal_unsupported` never carries a name and does not need one: it means tmux is
        // what Settings asks for and there is no tmux on that Mac, so its sentence is written
        // whole. Guarding it on `e.app` too is what made the one refusal with a person behind it
        // arrive as "That could not be started."
        if (code === "terminal_unsupported") return T.webStartTerminalUnsupported;
        return T.webStartFailed;
    }

    function matching() {
        var q = find.trim().toLowerCase();
        return (places || []).filter(function (p) {
            if (!q) return true;
            return ((p.label || "") + " " + (p.path || "")).toLowerCase().indexOf(q) >= 0;
        });
    }

    /** The conversations on screen, narrowed by what has been typed. By title alone: the id is
     *  a UUID nobody reads, and a list that answers to one would be a list you could search for
     *  a conversation you were never shown. */
    function matchingPast() {
        var q = find.trim().toLowerCase();
        return (pasts || []).filter(function (r) {
            if (!q) return true;
            return (r.title || "").toLowerCase().indexOf(q) >= 0;
        });
    }

    /** Whether picking a conversation up is on the table at all. Each assistant owns its own
     *  history and names; the Mac selects the matching source from this closed assistant id. */
    function resumable() {
        return (with_ === "claude" || with_ === "codex")
            && typeof api.pastSessions === "function";
    }

    /** When a conversation was last written to.
     *
     *  Inside the hour it is the page's own words; today it is a clock; before that it is a
     *  date, in the browser's own language rather than in a string this app would have to
     *  translate fourteen times. A list that can span a month cannot say "14:32" for all of it. */
    function when(unix) {
        if (!unix) return "";
        var then = new Date(unix * 1000);
        var now = new Date();
        var sameDay = then.getFullYear() === now.getFullYear()
            && then.getMonth() === now.getMonth() && then.getDate() === now.getDate();
        if (sameDay) return clockOf(unix);
        try {
            return then.toLocaleDateString(undefined, { month: "short", day: "numeric" });
        } catch (e) {
            return clockOf(unix);
        }
    }

    /**
     * The two chips that say what a press opens.
     *
     * **Only when there is a choice.** One assistant on the Mac and this is a control with
     * nothing to control — so it is not drawn at all, and the sheet is exactly what it was
     * before Codex existed. The list comes from the Mac; this page does not know what is
     * installed and does not guess.
     */
    function drawWith() {
        var row = els["start-with"];
        // Gone once a project's conversations are on screen. Not merely irrelevant there — a
        // press on the other chip would have to leave the list to mean anything, and a control
        // that silently throws away the screen you are on is worse than one that is not offered.
        row.hidden = assistants.length < 2 || !!at;
        if (row.hidden) { row.innerHTML = ""; return; }
        row.innerHTML = "";
        var label = document.createElement("span");
        label.className = "with-label";
        label.textContent = T.webStartWith;
        row.appendChild(label);
        assistants.forEach(function (a) {
            var chip = document.createElement("button");
            chip.type = "button";
            chip.className = "chip" + (a.id === with_ ? " on" : "");
            chip.textContent = a.label || a.id;
            // Shut while a start is in flight, for the same reason every row is: the press that
            // is already on its way is the one that decides, and changing this under it would
            // only mislead about what opened.
            chip.disabled = !!pressing || !!wait;
            chip.setAttribute("aria-pressed", a.id === with_ ? "true" : "false");
            chip.onclick = function () {
                with_ = a.id;
                // Changing assistant is changing what the list is *of*, so anything opened
                // under the old one is stood down rather than left on screen answering to the
                // wrong chip.
                if (!resumable()) leave();
                draw();
            };
            row.appendChild(chip);
        });
    }

    /**
     * The switch, and the way back out.
     *
     * One row with one control in it at a time: while projects are on screen it is the question
     * *what does the next press mean*, and while a project's conversations are on screen it is
     * the answer to *how do I get back*. Two controls would have been one of them always wrong —
     * a switch that turns resuming off under a list of conversations has nothing sensible to do
     * with the list it is standing on.
     */
    function drawResume() {
        var row = els["start-resume"];
        row.innerHTML = "";
        row.hidden = !S.write || typeof api.pastSessions !== "function";
        if (row.hidden) return;

        if (at) {
            var back = document.createElement("button");
            back.type = "button";
            back.className = "chip back";
            // The project, in its own mark and its own colour. It is the only thing on this
            // screen that says which project these conversations are from — every row below is
            // a title and nothing else — and it is the way out, which is one control doing two
            // jobs that were always the same job.
            back.innerHTML = '<span class="arrow">\u2190</span><canvas></canvas>'
                + '<span class="name"></span>';
            var mark = back.querySelector("canvas");
            if (!drawIcon(mark, at.icon, 4)) mark.classList.add("none");
            var name = back.querySelector(".name");
            name.textContent = at.label || shortPath(at.path);
            name.style.color = at.icon ? tint(at.icon.accent) : "";
            back.setAttribute("aria-label", T.webResumeBack);
            back.disabled = !!pressing || !!wait;
            back.onclick = function () { leave(); draw(); };
            row.appendChild(back);
            return;
        }

        var chip = document.createElement("button");
        chip.type = "button";
        chip.className = "chip check" + (resume && resumable() ? " on" : "");
        // The box and the tick as one path each, on one fourteen-unit grid, so the mark sits
        // where it was drawn rather than where a rotate and a translate happen to land it.
        chip.innerHTML = '<svg class="tick" viewBox="0 0 14 14" aria-hidden="true"'
            + ' focusable="false">'
            + '<rect class="box" x="0.5" y="0.5" width="13" height="13" rx="3.5"></rect>'
            + '<path class="mark" d="M3.6 7.1 5.9 9.4 10.4 4.6"'
            + ' stroke-linecap="round" stroke-linejoin="round"></path></svg>'
            + '<span class="label"></span>';
        chip.querySelector(".label").textContent = T.webResumeWith;
        chip.disabled = !!pressing || !!wait || !resumable();
        chip.setAttribute("aria-pressed", resume && resumable() ? "true" : "false");
        chip.onclick = function () {
            resume = !resume;
            if (resume) assignmentState.choose(false);
            draw();
        };
        row.appendChild(chip);
    }

    /**
     * A creation-only role switch, closed by any durable coordinator record.
     *
     * The live Session list cannot answer the offline case: its coordinator projection is on the
     * exact bound process only. Bearings can, and only `registration.state === "available"`
     * enables this button. A failed or unfinished read therefore looks disabled rather than
     * guessing that an absent live crown means the machine is unowned, and a store the Mac
     * cannot read says so in its own words instead of borrowing the failed-read sentence.
     */
    function drawClawdfather() {
        var row = els["start-clawdfather-row"];
        var button = els["start-clawdfather"];
        var state = els["start-clawdfather-state"];
        var choice = clawdfatherCreationChoice(
            coordinatorPayload, assignmentState.selected(), !resume && !at);
        row.hidden = !choice.shown;
        if (!choice.shown) return;
        els["start-clawdfather-label"].textContent = clawdfatherCreationLabel();
        button.disabled = !choice.enabled || !!pressing || !!wait || !S.write;
        button.classList.toggle("on", choice.checked);
        button.setAttribute("aria-pressed", choice.checked ? "true" : "false");
        button.dataset.state = choice.state;

        var words = "";
        if (choice.state === "checking") words = T.webLoading;
        else if (choice.state === "blocked") {
            // Not a failed read: the read succeeded and said the record is unreadable at the
            // other end. Saying "could not read bearings" here would blame the wrong hop.
            words = T.webClawdfatherRegisterBlocked;
        } else if (choice.state === "unavailable" || coordinatorFailed) {
            words = T.webCoordReadFailed;
        } else if (choice.state === "assigned") {
            var coordinator = choice.coordinator || {};
            words = coordinatorPresenceText(coordinator);
        }
        state.textContent = words;
        button.title = words;
    }

    /** Whether the list is hiding anything below its own edge, which is what the fade at the
     *  bottom of it answers to. Read straight after the rows are put in — the layout is forced by
     *  asking, which is the point — and again whenever it is scrolled. */
    function edge() {
        var list = els["start-list"];
        var more = list.scrollHeight - list.scrollTop - list.clientHeight > 2;
        list.dataset.more = more ? "1" : "0";
    }

    function draw() {
        setStartSpin(null);
        if (els.start.hidden) return;
        var list = els["start-list"];
        var box = els["start-filter"];

        // Sending switched off, and that is the whole sheet. The control is still here and it
        // still opens — a button that fails on press teaches nothing, and the sentence names the
        // switch and where it lives. Nothing is fetched: there is nothing to press.
        if (!S.write) {
            say(T.webStartOff);
            box.hidden = true;
            els["start-with"].hidden = true;
            els["start-resume"].hidden = true;
            els["start-clawdfather-row"].hidden = true;
            list.innerHTML = "";
            return;
        }

        drawWith();
        drawResume();
        drawClawdfather();

        if (at) { drawPast(list, box); return; }

        box.placeholder = T.webStartFilter;
        box.setAttribute("aria-label", T.webStartFilter);

        say(wait ? T.webStartWaiting
            : (loading && !places) ? T.webLoading
            : (places && !places.length) ? T.webStartEmpty
            : T.webStartPick);
        said("");

        // Forty is the most the Mac will ever offer and three fit on a phone without scrolling.
        // Under nine, a box to narrow them down is furniture in front of the answer.
        box.hidden = !(places && places.length > MANY);
        if (box.hidden && box.value) { box.value = ""; find = ""; }

        list.innerHTML = "";
        matching().forEach(function (p) {
            var li = document.createElement("li");
            var row = document.createElement("button");
            row.type = "button";
            row.className = "place";
            row.dataset.id = p.id;
            // One press. While a start is in flight, or while one is settling, every row is
            // shut — the second press is a second tab and nobody ever wanted two.
            row.disabled = !!pressing || !!wait;
            if (pressing === p.id) row.dataset.busy = "1";
            row.innerHTML = '<canvas></canvas><span class="name"></span><span class="where"></span>';

            // The same mark the session list draws, drawn by the same code, and still optional.
            var mark = row.querySelector("canvas");
            if (!drawIcon(mark, p.icon, 4)) mark.classList.add("none");

            var name = row.querySelector(".name");
            name.textContent = p.label || p.path;
            name.style.color = p.icon ? tint(p.icon.accent) : "";
            // The path is here so two projects with the same name can be told apart, and it is
            // stood aside for the one line that matters more while a row is being pressed.
            var where = row.querySelector(".where");
            if (pressing === p.id && Waits.startPress.visible) {
                where.innerHTML = '<canvas class="start-spin"></canvas><span></span>';
                where.querySelector("span").textContent = T.webStarting;
                setStartSpin(where.querySelector(".start-spin"));
                drawSpinner(startSpin, spinPhase);
            } else {
                where.textContent = pressing === p.id ? T.webStarting : shortPath(p.path);
            }

            li.appendChild(row);
            list.appendChild(li);
        });
        edge();
    }

    /**
     * One project's recorded conversations.
     *
     * The filter is on the moment there is more than one row, which is a lower bar than the
     * projects have and deliberately so: a project's own name is a word somebody already knows
     * and can find by eye, and a conversation's title is a sentence out of a month of work.
     * Finding one by typing part of it is the whole reason this screen has a box.
     */
    function drawPast(list, box) {
        box.placeholder = T.webResumeFilter;
        box.setAttribute("aria-label", T.webResumeFilter);

        say(wait ? T.webStartWaiting
            : (reading && !pasts) ? T.webLoading
            : (pasts && !pasts.length) ? T.webResumeEmpty
            : T.webResumePick);

        box.hidden = !(pasts && pasts.length > 1);
        if (box.hidden && box.value) { box.value = ""; find = ""; }

        list.innerHTML = "";
        var all = matchingPast();
        all.forEach(function (r) {
            var li = document.createElement("li");
            var row = document.createElement("button");
            row.type = "button";
            row.className = "place past";
            row.dataset.session = r.id;
            // A conversation something is writing to right now is still pressable — it goes to
            // that session — but only if this page can see which row it is. Without hooks or a
            // registry entry the Mac knows the transcript is busy and not which tab has it, and
            // a button that cannot do either of its two jobs is better shut.
            var open = r.live ? bySessionId(r.id) : null;
            row.disabled = !!pressing || !!wait || (r.live && !open);
            if (pressing === r.id) row.dataset.busy = "1";
            row.innerHTML = '<span class="name"></span><span class="where"></span>';

            row.querySelector(".name").textContent = r.title;
            var where = row.querySelector(".where");
            if (pressing === r.id && Waits.startPress.visible) {
                where.innerHTML = '<canvas class="start-spin"></canvas><span></span>';
                where.querySelector("span").textContent = T.webResuming;
                setStartSpin(where.querySelector(".start-spin"));
                drawSpinner(startSpin, spinPhase);
            } else if (pressing === r.id) {
                where.textContent = T.webResuming;
            } else if (r.live) {
                row.dataset.live = "1";
                where.textContent = T.webResumeLive;
            } else {
                where.textContent = when(r.at);
            }

            li.appendChild(row);
            list.appendChild(li);
        });

        // Every row the Mac sent, and then what it did not send.
        //
        // There was a *Show 25 more* row here for an afternoon and it was friction with nothing
        // behind it: the whole list is already on this device, so the button was asking somebody
        // to authorise work that had been done before the sheet opened. What is worth a row at
        // the bottom is the one thing scrolling genuinely cannot reach.
        if (capped && all.length) {
            var note = document.createElement("li");
            note.className = "note";
            note.setAttribute("role", "status");
            note.textContent = T.webResumeCapped;
            list.appendChild(note);
        }
        edge();
    }

    /** Asked afresh every time the sheet opens: a directory can go away between two looks, and
     *  the list is sorted by when each was last worked in. The old one stays on screen while
     *  the new one is on its way — a list that blanks itself to refetch is a flicker. */
    function load() {
        if (loading || typeof api.places !== "function") return;
        loading = true;
        draw();
        api.places().then(function (d) {
            places = (d && d.places) || [];
            // The Mac's list, not this page's. Whether Codex is installed is a question only
            // that end can answer, and a chip for something that is not there opens a tab
            // saying "command not found".
            assistants = (d && d.assistants) || [];
            if (!with_ || !assistants.some(function (a) { return a.id === with_; })) {
                with_ = assistants.length ? assistants[0].id : null;
            }
        }).catch(function (e) {
            places = places || [];
            said(why(e));
        }).then(function () {
            loading = false;
            draw();
        });
    }

    /** Read the durable owner independently of the places list; either may be slow without
     *  holding the other off screen. Every opening gets a fresh answer because coordinator
     *  registration can change between two visits to the sheet. */
    function loadCoordinator() {
        return coordinatorLoader.load();
    }

    /** Show what has already been said in a place. Asked every time rather than remembered: a
     *  conversation can be started, renamed or deleted between two looks at the same project,
     *  and `live` is a fact about this instant that a cache would be wrong about immediately. */
    function enter(place) {
        at = place;
        pasts = null;
        capped = false;
        find = "";
        els["start-filter"].value = "";
        said("");
        reading = true;
        draw();
        api.pastSessions(place.id, with_).then(function (d) {
            if (!at || at.id !== place.id) return;   // gone back while this was in flight
            pasts = (d && d.sessions) || [];
            capped = !!(d && d.more);
        }).catch(function (e) {
            if (!at || at.id !== place.id) return;
            pasts = pasts || [];
            if (e && e.code === "not_found") {
                // That directory has gone since the list was built. It is the project list that
                // is wrong rather than the press, so back out to it and ask again.
                leave();
                places = null;
                load();
            }
            said(why(e));
        }).then(function () {
            reading = false;
            draw();
        });
    }

    /** Back to the projects. The switch is left where it was: it is a preference about what a
     *  press means, and coming back out of one project has not changed anybody's mind. */
    function leave() {
        at = null;
        pasts = null;
        capped = false;
        reading = false;
        find = "";
        els["start-filter"].value = "";
        said("");
    }

    function press(id) {
        if (pressing || wait || !S.write || typeof api.startPlace !== "function") return;
        var place = null;
        (places || []).some(function (p) {
            if (p.id !== id) return false;
            place = p;
            return true;
        });
        // The switch decides what this press means. Nothing is started here — the list of what
        // has already been said is a read, and the press that starts anything is the one on a
        // row of it.
        if (resume && resumable() && place) { enter(place); return; }
        pressing = id;
        var makeClawdfather = assignmentState.selected();
        said("");
        draw();
        Waits.startPress.start();
        api.startPlace(id, with_).then(function (d) {
            Waits.startPress.settle(function () {
                pressing = null;
                // From here the tab exists on the Mac. Nothing after this line is allowed to read
                // as "it might not have worked", and nothing after it presses this again.
                began(d && d.id, place, makeClawdfather);
            });
        }).catch(function (e) {
            Waits.startPress.settle(function () {
                pressing = null;
                assignmentState.choose(false);
                if (e && e.code === "write_disabled") {
                    // The switch was turned off while this sheet was open. The server is the one
                    // that knows, so take its word for it — the composer answers to the same flag.
                    S.write = false;
                    renderComposer();
                } else if (e && e.code === "not_found") {
                    // That directory has gone since the list was built. It is the list that is
                    // wrong rather than the press, so the list is what gets asked again.
                    places = null;
                    load();
                }
                said(why(e));
                draw();
            });
        });
    }

    /**
     * Carry one conversation on.
     *
     * The same machinery as a fresh start from here down: the Mac answers with an id before the
     * session exists, and `began` watches the list for it. What is different is only which
     * request was made and what the placeholder is called — a resumed session comes back under
     * the name it already had, which is the reason somebody picked it off this list rather than
     * pressing the project.
     */
    function pick(sessionID) {
        if (pressing || wait || !S.write || !at || typeof api.resumePlace !== "function") return;
        var row = null;
        (pasts || []).some(function (r) {
            if (r.id !== sessionID) return false;
            row = r;
            return true;
        });
        if (!row) return;

        // Already open. Go to it rather than start a second process on the same transcript —
        // which is what the person who pressed it wanted, and the only safe reading of it.
        if (row.live) {
            var open = bySessionId(sessionID);
            if (!open) return;
            close();
            openSession(open.id);
            return;
        }

        var place = at;
        pressing = sessionID;
        said("");
        draw();
        Waits.startPress.start();
        api.resumePlace(place.id, sessionID, with_).then(function (d) {
            Waits.startPress.settle(function () {
                pressing = null;
                began(d && d.id, { label: row.title, path: place.path, icon: place.icon });
            });
        }).catch(function (e) {
            Waits.startPress.settle(function () {
                pressing = null;
                if (e && e.code === "write_disabled") {
                    S.write = false;
                    renderComposer();
                } else if (e && e.code === "not_found") {
                    // The transcript has gone, or something else has it. Either way this list is
                    // the thing that is out of date, so it is what gets asked again.
                    said(T.webResumeGone);
                    enter(place);
                    return;
                }
                said(why(e));
                draw();
            });
        });
    }

    /** Started. The sheet has done its job, and what is left is a wait. */
    function began(id, place, makeClawdfather) {
        assignmentState.begin(id, makeClawdfather === true);
        close();
        if (!id) {
            // A reply with no id is nothing to watch the list for. The tab was still opened —
            // that is what `ok` meant — so this says now what the fifteen seconds would have.
            band(T.webStartSlow, true);
            return;
        }
        wait = { id: id, from: S.openId, late: false, place: place || {} };
        band(T.webStartWaiting, false);
        renderList();
        clearTimeout(timer);
        timer = setTimeout(function () {
            if (!wait) return;
            wait.late = true;
            band(T.webStartSlow, true);
            renderList();
        }, HOLD);
    }

    function band(words, slow) {
        els.starting.dataset.state = slow ? "slow" : "waiting";
        // Shown before it is written into: it is a live region, and a change made while it is
        // still `hidden` is a change nothing was listening to.
        els.starting.hidden = false;
        els["starting-say"].textContent = words;
        // The spinner rides the list's clock — see `bandSpin`. Nothing turns once the wait has
        // been given up on: there is nothing left to be waiting for.
        setBandSpin(slow ? null : els["starting-spin"]);
        if (bandSpin) drawSpinner(bandSpin, spinPhase);
    }

    function hideBand() {
        els.starting.hidden = true;
        setBandSpin(null);
    }

    function open() {
        assignmentState.open();
        els.start.hidden = false;
        said("");
        // Always at the projects. The switch survives — it is a preference — but a sheet that
        // reopened inside whichever project was last looked at would be a sheet whose first
        // screen depends on something nobody remembers doing.
        leave();
        if (S.write) {
            if (!wait) load();
            loadCoordinator();
        }
        draw();
        els["start-close"].focus({ preventScroll: true });
    }

    function close() {
        if (pressing) return;
        els.start.hidden = true;
        setStartSpin(null);
    }

    function arrange(list) {
        var id = wait ? wait.id : landed;
        if (!id) return list;
        for (var i = 0; i < list.length; i++) {
            if (list[i].id !== id) continue;
            return [list[i]].concat(list.slice(0, i), list.slice(i + 1));
        }
        return list;
    }

    /** The list-shaped promise of the place just pressed. Its node is private to Start rather
     *  than entered into `rowNodes`, which is the mechanical guarantee that arrows, filters and
     *  counts cannot mistake it for a session while still letting it occupy the exact geometry
     *  the arriving row will use. */
    function placeholder() {
        if (!wait || wait.late || byId(wait.id)) {
            if (placeholderNode && placeholderNode.parentNode) {
                placeholderNode.parentNode.removeChild(placeholderNode);
            }
            return null;
        }
        if (!placeholderNode) {
            placeholderNode = document.createElement("li");
            placeholderNode.className = "row starting-row";
            placeholderNode.setAttribute("role", "status");
            placeholderNode.innerHTML = '<canvas class="mark"></canvas>' +
                '<div class="title"><span class="label"></span></div>' +
                '<div class="meta"><span class="path"></span></div>' +
                '<div class="state"><canvas class="spin"></canvas><span class="line"></span></div>';
        }
        var place = wait.place || {};
        placeholderNode.setAttribute("aria-label", T.webStartWaiting);
        var mark = placeholderNode.querySelector(".mark");
        if (!drawIcon(mark, place.icon, 4)) mark.classList.add("none");
        else mark.classList.remove("none");
        var title = placeholderNode.querySelector(".title");
        title.querySelector(".label").textContent = place.label || place.path || T.webStarting;
        title.style.color = place.icon ? tint(place.icon.accent) : "";
        placeholderNode.querySelector(".path").textContent = shortPath(place.path);
        placeholderNode.querySelector(".line").textContent = T.webStartWaiting;
        var spin = placeholderNode.querySelector(".spin");
        drawSpinner(spin, spinPhase);
        spinners.push(spin);
        return placeholderNode;
    }

    return {
        open: open,
        close: close,
        // Schedule history reaches the same resume route and creates the same arriving Session
        // row. Sharing this transition keeps its placeholder and slow warning identical to a
        // resume started from the ordinary project picker.
        began: began,
        press: press,
        pick: pick,
        toggleClawdfather: function () {
            var choice = clawdfatherCreationChoice(
                coordinatorPayload, assignmentState.selected(), !resume && !at);
            if (choice.enabled !== true || pressing || wait || !S.write) return;
            assignmentState.choose(!assignmentState.selected());
            draw();
        },
        typed: function (value) { find = value; draw(); },
        scrolled: edge,
        placeholder: placeholder,
        arrange: arrange,
        arriving: function (id) { return !!(wait && wait.id === id); },

        /** The write switch can flip under an open sheet — `hello` carries it on every
         *  reconnect — and the sheet is a different screen on either side of that. */
        sync: draw,

        /** Let go of the wait: the reader has read the line and closed it. A session that
         *  turns up afterwards is a row in the list like any other. */
        dismiss: function () {
            wait = null;
            clearTimeout(timer);
            timer = null;
            hideBand();
            renderList();
        },

        /**
         * Every list that arrives, until the one with this session in it.
         *
         * The id is what is watched for, not `isClaude` — a tab exists a good second or two
         * before `claude` is a process `ps` can see, and a page waiting for the flag would sit
         * there through a session that had already started fine.
         */
        check: function () {
            attemptAssignment();
            if (!wait && landed) {
                landed = null;
                renderList();
                return false;
            }
            if (!wait || !byId(wait.id)) return false;
            var id = wait.id, from = wait.from, late = wait.late;
            landed = late ? null : id;
            wait = null;
            clearTimeout(timer);
            timer = null;
            hideBand();
            draw();
            // Opened, for somebody who has not gone anywhere since pressing it — that is what
            // they asked for, and it is one less tap on a phone. Not if they have opened
            // something else meanwhile, and not after the fifteen seconds have gone by: by then
            // they have been told to look at the Mac, and a transcript arriving over whatever
            // they moved on to is the page having an opinion it has not earned.
            if (late || S.openId !== from) { S.selectedId = id; renderList(); return false; }
            openSession(id);
            return true;
        }
    };

    /**
     * Type the registration recipe only after the assistant process exists.
     *
     * The terminal id appears in `/v1/sessions` before Claude or Codex does. Sending on that
     * first frame would type a paragraph into the newborn shell; waiting for the closed assistant
     * field makes it the assistant's first instruction instead. Bearings is read once more at
     * this last boundary so a coordinator registered while the tab was starting wins the race
     * and the new Session is never asked to take over.
     */
    function attemptAssignment() {
        var id = assignmentState.pendingID();
        if (!id) return;
        assignmentState.attempt(byId(id), api, { timeoutMs: 8000 });
    }
})();

els["start-go"].addEventListener("click", function () { Start.open(); });
els.start.addEventListener("click", function () { Start.close(); });
els["start-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["start-close"].addEventListener("click", function () { Start.close(); });
els["start-filter"].addEventListener("input", function () { Start.typed(this.value); });
els["start-clawdfather"].addEventListener("click", function () { Start.toggleClawdfather(); });
els["start-list"].addEventListener("scroll", function () { Start.scrolled(); }, { passive: true });
els["start-list"].addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest(".place") : null;
    if (!row || row.disabled) return;
    // Which list this is, off the row rather than off a flag somewhere else. The two screens
    // share a container, and a mode read from a variable is a mode that can disagree with what
    // is actually under the finger.
    if (row.dataset.session) Start.pick(row.dataset.session);
    else Start.press(row.dataset.id);
});
els["starting-close"].addEventListener("click", function () { Start.dismiss(); });
