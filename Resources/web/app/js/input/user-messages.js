import { S } from "../core/state.js";
import { T } from "../core/i18n.js";
import { esc } from "../core/esc.js";
import { Optimistic } from "../view/waits.js";
import { entryHTML } from "../view/transcript.js";
import { copyForUserMessages, userMessageEntries } from "../view/user-messages-data.js";
import { SessionActions } from "./detail-actions.js";

/* This view owns its small DOM island. The transcript stays the source of truth; opening and
   closing the sheet never changes the conversation or asks the Mac for a second copy. */
var style = document.createElement("link");
style.rel = "stylesheet";
style.href = new URL("../../css/user-messages.css", import.meta.url).href;
document.head.appendChild(style);

var menu = document.getElementById("session-actions-main");
var before = document.getElementById("session-git-more");
var button = document.createElement("button");
button.id = "session-user-messages";
button.type = "button";
button.setAttribute("role", "menuitem");
menu.insertBefore(button, before);

var overlay = document.createElement("div");
overlay.className = "overlay";
overlay.id = "user-messages";
overlay.hidden = true;
overlay.innerHTML =
    '<div class="sheet user-messages-sheet" id="user-messages-sheet" role="dialog" aria-modal="true" ' +
    'aria-labelledby="user-messages-title">' +
    '<h2 id="user-messages-title"></h2>' +
    '<div class="user-message-list" id="user-message-list"></div>' +
    '<div class="buttons"><button class="chip" id="user-messages-close" type="button"></button></div>' +
    '</div>';
document.body.appendChild(overlay);

var sheet = document.getElementById("user-messages-sheet");
var title = document.getElementById("user-messages-title");
var list = document.getElementById("user-message-list");
var closeButton = document.getElementById("user-messages-close");
var sessionID = null;

function copy() {
    return copyForUserMessages(document.documentElement.lang);
}

function syncCopy() {
    var words = copy();
    button.textContent = words.title;
    title.textContent = words.title;
    closeButton.textContent = T.webClose;
}

// The language arrives after modules evaluate. Watching the document's language keeps the menu
// row from flashing the English fallback on a Chinese page, including a transcript with no rows
// (whose early return does not emit `clawdline:rendered`).
new MutationObserver(syncCopy).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["lang"]
});
syncCopy();

function draw() {
    var entries = userMessageEntries(
        S.tx.id === sessionID ? S.tx.entries : [],
        sessionID ? Optimistic.entries(sessionID) : []
    );
    if (!entries.length) {
        list.innerHTML = '<p class="user-messages-empty">' + esc(copy().empty) + '</p>';
        return;
    }
    if (S.newestFirst) entries.reverse();
    list.innerHTML = entries.map(entryHTML).join("");
    list.scrollTop = S.newestFirst ? 0 : list.scrollHeight;
}

function open() {
    if (!S.openId) return;
    sessionID = S.openId;
    syncCopy();
    draw();
    overlay.hidden = false;
    SessionActions.close();
    closeButton.focus({ preventScroll: true });
}

function close() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    sessionID = null;
}

button.addEventListener("click", function (event) {
    event.preventDefault();
    event.stopPropagation();
    open();
});
overlay.addEventListener("click", close);
sheet.addEventListener("click", function (event) { event.stopPropagation(); });
closeButton.addEventListener("click", close);
overlay.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    close();
});

// `renderTranscript` fires this after strings have landed and after every transcript refresh.
// That gives an open sheet live optimistic turns, and closes a sheet whose session went away.
document.addEventListener("clawdline:rendered", function () {
    syncCopy();
    if (overlay.hidden) return;
    if (!S.openId || S.openId !== sessionID) { close(); return; }
    draw();
});
