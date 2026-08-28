import { S } from "../core/state.js";
import { reduced } from "../core/env.js";
import { T } from "../core/i18n.js";
import { esc } from "../core/esc.js";
import { Optimistic } from "../view/waits.js";
import { entryHTML } from "../view/transcript.js";
import { copyForUserMessages, filterUserMessages, userMessageEntries, userMessagePosition } from "../view/user-messages-data.js";
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
    '<div class="user-messages-head"><h2 id="user-messages-title"></h2>' +
    '<input class="user-messages-search" id="user-messages-search" type="search" ' +
    'autocomplete="off" enterkeyhint="search"></div>' +
    '<div class="user-message-list" id="user-message-list"></div>' +
    '<div class="buttons"><button class="chip" id="user-messages-close" type="button"></button></div>' +
    '</div>';
document.body.appendChild(overlay);

var sheet = document.getElementById("user-messages-sheet");
var title = document.getElementById("user-messages-title");
var search = document.getElementById("user-messages-search");
var list = document.getElementById("user-message-list");
var closeButton = document.getElementById("user-messages-close");
var sessionID = null;
var shownEntries = [];
var targetTimer = 0;

function copy() {
    return copyForUserMessages(document.documentElement.lang);
}

function syncCopy() {
    var words = copy();
    button.textContent = words.title;
    title.textContent = words.title;
    search.placeholder = words.search;
    search.setAttribute("aria-label", words.search);
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
    var allEntries = userMessageEntries(
        S.tx.id === sessionID ? S.tx.entries : [],
        sessionID ? Optimistic.entries(sessionID) : []
    );
    shownEntries = filterUserMessages(allEntries, search.value);
    if (!shownEntries.length) {
        list.innerHTML = '<p class="user-messages-empty">' + esc(allEntries.length
            ? copy().noMatches : copy().empty) + '</p>';
        return;
    }
    list.innerHTML = shownEntries.map(entryHTML).join("");
    Array.prototype.forEach.call(list.querySelectorAll('.entry[data-role="user"]'), function (row) {
        row.tabIndex = 0;
        row.setAttribute("role", "button");
    });
    list.scrollTop = 0;
}

function open() {
    if (!S.openId) return;
    sessionID = S.openId;
    search.value = "";
    syncCopy();
    draw();
    overlay.hidden = false;
    SessionActions.close();
    search.focus({ preventScroll: true });
}

function close() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    sessionID = null;
    shownEntries = [];
}

function jumpTo(entry) {
    var pending = Optimistic.entries(sessionID);
    var position = userMessagePosition(S.tx.entries, pending, entry, S.newestFirst);
    close();
    if (position < 0) return;
    var rows = document.querySelectorAll('#tx .entry[data-role="user"]');
    var target = rows[position];
    if (!target) return;
    target.scrollIntoView({ block: "center", behavior: reduced ? "auto" : "smooth" });
    window.clearTimeout(targetTimer);
    var previous = document.querySelector("#tx .user-message-target");
    if (previous) previous.classList.remove("user-message-target");
    // Restart the small locator pulse when the same row is chosen twice.
    void target.offsetWidth;
    target.classList.add("user-message-target");
    targetTimer = window.setTimeout(function () {
        target.classList.remove("user-message-target");
    }, 1800);
}

function pickedRow(event) {
    var row = event.target.closest ? event.target.closest('.entry[data-role="user"]') : null;
    if (!row || !list.contains(row)) return null;
    return row;
}

list.addEventListener("click", function (event) {
    // Markdown links keep doing what they say. A tap anywhere else on the message returns to it.
    if (event.target.closest && event.target.closest("a")) return;
    var row = pickedRow(event);
    if (!row) return;
    jumpTo(shownEntries[Array.prototype.indexOf.call(list.children, row)]);
});
list.addEventListener("keydown", function (event) {
    if (event.key !== "Enter" && event.key !== " ") return;
    var row = pickedRow(event);
    if (!row || event.target !== row) return;
    event.preventDefault();
    jumpTo(shownEntries[Array.prototype.indexOf.call(list.children, row)]);
});

button.addEventListener("click", function (event) {
    event.preventDefault();
    event.stopPropagation();
    open();
});
overlay.addEventListener("click", close);
sheet.addEventListener("click", function (event) { event.stopPropagation(); });
closeButton.addEventListener("click", close);
search.addEventListener("input", draw);
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
