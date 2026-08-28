/* --------------------------------------------------------------------------
   The entry point, and the list of what this page is made of.
   Every module is named below, in the order it was written in when all of this
   was one file, whether or not this file needs a name out of it. Most of them
   are reached anyway through somebody else's import; they are all listed because
   a module nobody imports is a module that never runs, and half of these exist
   entirely for what they do on the way in — a listener bound, a clock started.
   The list is the manifest, and it is the one place the whole page is written down.
   -------------------------------------------------------------------------- */
import { MOCK } from "./core/env.js";
import "./core/esc.js";
import { applyStrings } from "./core/i18n.js";
import { S } from "./core/state.js";
import { els } from "./core/dom.js";
import { clockOf } from "./core/util.js";
import { drawIcon } from "./core/pixels.js";
import { api, useApi } from "./net/api.js";
import "./net/build.js";
import "./net/handlers.js";
import "./net/fetch.js";
import { Schedules } from "./net/schedules.js";
import { Live } from "./net/live.js";
import { Mock } from "./net/mock.js";
import "./door/door.js";
import "./view/derive.js";
import { render, renderConn } from "./view/list.js";
import { renderTranscript } from "./view/transcript.js";
import "./view/markdown.js";
import "./view/composer.js";
import { paintStatic } from "./view/static.js";
import { Waits } from "./view/waits.js";
import { openSession } from "./session/open.js";
import "./session/agent.js";
import "./input/keys.js";
import "./input/swipe.js";
import { SessionActions } from "./input/detail-actions.js";
import { CoordinatorControls } from "./input/coordinator-actions.js";
import "./input/user-messages.js";
import "./input/git-panel.js";
import "./input/shell-panel.js";
import "./input/action-confirm.js";
import { routeTo } from "./input/route.js";
import "./input/settings.js";
import "./input/start.js";
import "./input/command.js";
import "./input/schedule.js";
import "./input/schedule-history.js";
import "./input/status-line.js";
import "./input/info.js";
import { Push } from "./input/push.js";
import "./input/shots.js";
import "./input/voice.js";
import "./input/composer.js";
import "./input/edges.js";

/* ==========================================================================
   10. Go
   ========================================================================== */

// The one thing that has to happen before anything on this page can call the API: which of the
// two it is. `net/api.js` holds the name and knows about neither — see the note there.
useApi(MOCK ? Mock : Live);

// The controls module keeps its pure command selection importable without a browser. DOM and
// the one route back to ordinary Session actions are supplied here, at the page boundary.
CoordinatorControls.bind({
    overlay: document.getElementById("coordinator-controls"),
    sheet: document.getElementById("coordinator-controls-sheet"),
    title: document.getElementById("coordinator-controls-title"),
    body: document.getElementById("coordinator-controls-body"),
    close: document.getElementById("coordinator-controls-close"),
    context: function () { return { connected: S.conn === "live" }; },
    onSessionActions: function (id) {
        openSession(id);
        setTimeout(function () { SessionActions.open(els["detail-focus"]); }, 0);
    }
});

// The wordmark wears the project's own mark, drawn by the same code the rows use — it comes
// from ~/.claude/project-icons.json in the app, and it is the one icon this page knows by heart.
var mark = {
    accent: "#d97757",
    cells: [".######.", ".#o##o#.", "########", ".##..##."].map(function (row) {
        return row.split("").map(function (ch) {
            return ch === "#" ? "#d97757" : (ch === "o" ? "#141416" : "#33201a");
        });
    })
};
drawIcon(els["brand-mark"], mark, 3);
drawIcon(els["door-mark"], mark, 3);

/**
 * Nothing is drawn until the interface has its words.
 *
 * The strings are the one thing the page cannot start without and cannot get for itself: they
 * decide what every label says, and repainting the furniture a moment after it has been read is
 * how a page comes to look like it changed its mind. So the first render waits, and the head of
 * this document holds the paint back while it does.
 *
 * Everything that can go wrong here ends the same way — the English already written into the
 * markup stands, and the page comes up. That is what it is in the document for.
 */
var booted = false;
function boot(data) {
    if (booted) return;
    booted = true;
    // Off first, and before anything that can throw: a page that is visible and half in English
    // is a page, and a page that is permanently invisible is not.
    document.documentElement.classList.remove("booting");
    try { applyStrings(data); } catch (e) { /* English, then */ }
    paintStatic();

    renderConn();
    // **Before the first paint, not after it.** The two screens that say "there is nothing here"
    // ask whether this wait is still running — see `listUnknown` — and a wait that has not been
    // started yet answers no. Started here, the first render draws neither of them.
    Waits.list.start();
    renderTranscript();
    render();
    // Read before the transport starts: `adoptToken` wipes the fragment when there is a token in
    // it, and a URL can carry both.
    routeTo(location.hash);
    api.start();
    Schedules.start();
    Push.start();
}

if (window.__strings) {
    // The ordinary path, and the fast one: the app writes the words into the document it serves,
    // so by the time this line runs they are already here and the page can be drawn in the frame
    // the modules finished in. The fetch below is what this replaced — a round trip that could
    // not be sent until every module had arrived and run, in front of a page held blank.
    boot(window.__strings);
} else if (location.protocol === "file:") {
    // A copy opened off a disk has no server to ask, and asking one that is not there is the
    // failed request in an otherwise clean console that teaches somebody to stop reading it.
    boot(null);
} else {
    // Not through `jsonFetch`: its own "could not reach Clawdline" is one of the strings being
    // fetched here, and a page cannot explain a failure in words it has not been given yet.
    // `no-store` because the answer depends on a header no cache is keyed on.
    fetch("/v1/strings", { cache: "no-store" })
        .then(function (res) { return res.ok ? res.json() : null; })
        .catch(function () { return null; })
        .then(boot);
    // And a page that is never drawn at all is worse than one drawn in English. A local answer
    // takes a millisecond; something in front of a tunnel on a bad connection takes longer, and
    // two seconds is well past the point where a dark rectangle stops reading as "loading".
    setTimeout(function () { boot(null); }, 2000);
}

// A page left open all day: "2m ago" is only true for a minute. The timestamps are rewritten in
// place rather than by rendering the transcript again — a re-render replaces the whole pane, and
// the reader would find themselves back at the top of it every minute for no reason they could see.
setInterval(function () {
    var stamps = els.tx.querySelectorAll("time[data-at]");
    for (var i = 0; i < stamps.length; i++) {
        stamps[i].textContent = clockOf(parseInt(stamps[i].getAttribute("data-at"), 10));
    }
}, 60000);
