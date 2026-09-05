import { S } from "../core/state.js";
import { T } from "../core/i18n.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { snippetControls, snippetGroups, snippetOrder, snippetsListHTML } from "../view/snippets-data.js";
import { appendMsg } from "./composer.js";
import { SessionActions } from "./detail-actions.js";

/* ==========================================================================
   Snippets — the sheet

   Text somebody wrote once, pressed instead of typed again. This view owns its
   small DOM island the way `input/user-messages.js` does: it injects its own
   stylesheet, builds its own overlay, and inserts its own row into the `⋯`
   menu before the Git row. Everything it decides before writing HTML lives in
   `view/snippets-data.js`, where a Node suite can read it.

   **Pressing a row puts the text in the composer and closes the sheet. It
   never sends.** `input/composer.js:appendMsg` is the same function dictation
   already uses, and the note above it and the one at the top of `input/voice.js`
   say why: words in a box that were misheard, or pressed by a thumb in a
   pocket, are a typo — the same words already posted to a terminal are an
   incident. The send button stays the only thing that sends. That is the whole
   design and not a step left out; `docs/snippets.md` records `send_on_tap` as
   declined rather than postponed.

   **`openSnippets()` is exported and takes no arguments** because it is about
   to have a second entrance: the second wave splits the project mark out of the
   Session-info button and makes it the shortcut, and a header that has to know
   how this sheet is built would be a header that breaks when it changes.
   ========================================================================== */

var style = document.createElement("link");
style.rel = "stylesheet";
style.href = new URL("../../css/snippets.css", import.meta.url).href;
document.head.appendChild(style);

var menu = document.getElementById("session-actions-main");
var before = document.getElementById("session-git-more");
var button = document.createElement("button");
button.id = "session-snippets";
button.type = "button";
button.setAttribute("role", "menuitem");
menu.insertBefore(button, before);

var overlay = document.createElement("div");
overlay.className = "overlay";
overlay.id = "snippets";
overlay.hidden = true;
overlay.innerHTML =
    '<div class="sheet snippets-sheet" id="snippets-sheet" role="dialog" aria-modal="true" ' +
    'aria-labelledby="snippets-title">' +
    '<div class="snippets-head"><h2 id="snippets-title"></h2>' +
    '<span class="snippets-where" id="snippets-where"></span></div>' +
    '<div class="snippet-list" id="snippet-list"></div>' +
    '<div class="buttons"><button class="chip" id="snippets-close" type="button"></button></div>' +
    "</div>";
document.body.appendChild(overlay);

var sheet = document.getElementById("snippets-sheet");
var title = document.getElementById("snippets-title");
var where = document.getElementById("snippets-where");
var list = document.getElementById("snippet-list");
var closeButton = document.getElementById("snippets-close");

var sessionID = null;
var shown = [];
var reading = 0;

/** Whether this transport has the route at all. An older Mac, and the idle client the cloud page
 *  runs before a relay is chosen, do not — and a menu row that opens a sheet which can only say
 *  "could not read" is worse than no row. The same question `/v1/places` and `/v1/push/key` are
 *  already asked; the four writing routes are asked about in `snippetControls` for the editor. */
function readable() {
    return snippetControls(api).read;
}

function syncCopy() {
    button.textContent = T.webSnippets;
    title.textContent = T.webSnippets;
    closeButton.textContent = T.webClose;
}

// The language arrives after the modules evaluate, exactly as it does for the sheet next door:
// watching the document's language keeps the menu row from showing the English fallback on a
// page that is about to be Chinese.
new MutationObserver(syncCopy).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["lang"]
});
syncCopy();

/**
 * The row in the `⋯` menu, present only when this transport can read the list.
 *
 * `disabled` as well as `hidden`, because `SessionActions.items()` collects
 * `button:not(:disabled)` for the keyboard: a row that is only hidden is still a stop on the way
 * down the menu with the arrow keys, which is a shortcut to a sheet nobody can see.
 *
 * **Asked when the menu opens, not once at the end of a render.** The transport is chosen after
 * these modules evaluate, and `renderTranscript` returns early — emitting no `clawdline:rendered`
 * — for a session whose transcript could not be read. A row that decided its own existence on
 * that event stayed hidden for exactly those sessions, which is how this was found.
 */
function syncRow() {
    var ok = readable();
    button.hidden = !ok;
    button.disabled = !ok;
}
syncRow();
// Registered after `input/detail-actions.js` has bound its own handler to the same button, so
// the menu is already open by the time this runs — the same tick, so nothing is painted between.
document.getElementById("detail-actions-trigger").addEventListener("click", syncRow);

function draw(model, options) {
    shown = snippetOrder(model);
    where.textContent = model && model.project ? model.project.label : "";
    list.innerHTML = snippetsListHTML(model, options);
    list.scrollTop = 0;
}

/**
 * Open it, and read the list.
 *
 * The read is not the sheet's reason to exist — on a Mac the list rides the orchestrator
 * snapshot and comes back in a few milliseconds — but the sheet is shown first either way. A
 * shortcut that spins before it appears is not a shortcut, and a shortcut that appears and then
 * fills is one.
 */
export function openSnippets() {
    if (!S.openId || !readable()) return;
    sessionID = S.openId;
    var ticket = ++reading;
    var session = byId(sessionID);
    var readOnly = S.write !== true;
    syncCopy();
    draw(null, { readOnly: readOnly, loading: true });
    overlay.hidden = false;
    SessionActions.close();
    closeButton.focus({ preventScroll: true });
    api.snippets(sessionID).then(function (answer) {
        // Two presses, or a session switched while the first read was out: only the newest one
        // may paint, the same ticket rule the transcript reads under.
        if (ticket !== reading || overlay.hidden) return;
        draw(snippetGroups(answer, {
            machine: session ? session.machine : null,
            project: session ? session.cwd : null
        }), { readOnly: readOnly });
    }).catch(function (error) {
        if (ticket !== reading || overlay.hidden) return;
        // The transport's own sentence, unedited — `cloud_snippets_unpublished` says a Mac is
        // running a build older than this page, and no string of ours could say it better.
        draw(null, { readOnly: readOnly, error: (error && error.message) || String(error) });
    });
}

export function closeSnippets() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    sessionID = null;
    shown = [];
    reading += 1;
}

/** A press: the words into the box, the sheet shut, and nothing else. */
function insert(row) {
    if (!row || S.write !== true) return;
    closeSnippets();
    appendMsg(row.body);
}

list.addEventListener("click", function (event) {
    var target = event.target.closest ? event.target.closest(".snippet-row") : null;
    if (!target || !list.contains(target)) return;
    insert(shown[Number(target.getAttribute("data-snippet"))]);
});

button.addEventListener("click", function (event) {
    event.preventDefault();
    event.stopPropagation();
    openSnippets();
});
overlay.addEventListener("click", closeSnippets);
sheet.addEventListener("click", function (event) { event.stopPropagation(); });
closeButton.addEventListener("click", closeSnippets);
overlay.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    closeSnippets();
});

// `renderTranscript` fires this after the strings have landed and after every transcript
// refresh. It is what keeps the menu row's word right, keeps the row itself in step with a
// transport that was chosen after these modules evaluated, and closes a sheet whose session went
// away underneath it.
document.addEventListener("clawdline:rendered", function () {
    syncCopy();
    syncRow();
    if (overlay.hidden) return;
    if (!S.openId || S.openId !== sessionID) closeSnippets();
});
