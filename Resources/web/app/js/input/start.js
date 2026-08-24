import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { shortPath, tint } from "../core/util.js";
import { bandSpin, drawIcon, drawSpinner, setBandSpin, setStartSpin, spinPhase, spinners, startSpin } from "../core/pixels.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { renderList } from "../view/list.js";
import { renderComposer } from "../view/composer.js";
import { Waits } from "../view/waits.js";
import { openSession } from "../session/open.js";

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
    var wait = null;     // { id, from, late, place } — started, and not in the list yet
    var timer = null;
    var placeholderNode = null;
    // Held at the placeholder's position through the first following stream update. That first
    // paint is the promised in-place replacement; after it, ordinary list ordering takes over
    // through the same FLIP path as every other move rather than making the arrival itself jump.
    var landed = null;

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
        // Both 409s carry `app`. Without it there is no sentence to write — a translation with
        // `{app}` still in it is worse than the plain refusal — so that falls through.
        if (e && e.app && code === "terminal_closed") return fill(T.webStartTerminalClosed, { app: e.app });
        if (e && e.app && code === "terminal_unsupported") return fill(T.webStartTerminalUnsupported, { app: e.app });
        return T.webStartFailed;
    }

    function matching() {
        var q = find.trim().toLowerCase();
        return (places || []).filter(function (p) {
            if (!q) return true;
            return ((p.label || "") + " " + (p.path || "")).toLowerCase().indexOf(q) >= 0;
        });
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
        row.hidden = assistants.length < 2;
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
            chip.onclick = function () { with_ = a.id; draw(); };
            row.appendChild(chip);
        });
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
            list.innerHTML = "";
            return;
        }

        drawWith();

        say(wait ? T.webStartWaiting
            : (loading && !places) ? T.webLoading
            : (places && !places.length) ? T.webStartEmpty
            : T.webStartPick);

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

    function press(id) {
        if (pressing || wait || !S.write || typeof api.startPlace !== "function") return;
        var place = null;
        (places || []).some(function (p) {
            if (p.id !== id) return false;
            place = p;
            return true;
        });
        pressing = id;
        said("");
        draw();
        Waits.startPress.start();
        api.startPlace(id, with_).then(function (d) {
            Waits.startPress.settle(function () {
                pressing = null;
                // From here the tab exists on the Mac. Nothing after this line is allowed to read
                // as "it might not have worked", and nothing after it presses this again.
                began(d && d.id, place);
            });
        }).catch(function (e) {
            Waits.startPress.settle(function () {
                pressing = null;
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

    /** Started. The sheet has done its job, and what is left is a wait. */
    function began(id, place) {
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
        els.start.hidden = false;
        said("");
        if (S.write && !wait) load();
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
        press: press,
        typed: function (value) { find = value; draw(); },
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
})();

els["start-go"].addEventListener("click", function () { Start.open(); });
els.start.addEventListener("click", function () { Start.close(); });
els["start-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["start-close"].addEventListener("click", function () { Start.close(); });
els["start-filter"].addEventListener("input", function () { Start.typed(this.value); });
els["start-list"].addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest(".place") : null;
    if (!row || row.disabled) return;
    Start.press(row.dataset.id);
});
els["starting-close"].addEventListener("click", function () { Start.dismiss(); });
