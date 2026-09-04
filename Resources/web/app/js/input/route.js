import { Pages, pageInHash } from "../core/pages.js";
import { byId } from "../view/derive.js";
import { openSession } from "../session/open.js";

/* ---- arriving at a session from somewhere else ---------------------------
 *
 * A notification about a session carries `/#session=<id>`, and tapping it landed on the session
 * list. Three things had to be true for it to land on the session; this file was the third.
 *
 * **A fragment is only read when a document loads.** Sending an already-open page to
 * `/#session=abc` changes the address and navigates nothing — same document, no reload, and this
 * page had no other opinion about the fragment, so the tap put the app in front of somebody at
 * the screen they were already on. The service worker now sends a message as well, for the case
 * where it cannot navigate the client at all; both roads end here.
 *
 * The id is held rather than acted on when the list has not arrived yet: a cold start routes
 * before it knows what sessions exist, and `onSessions` tries again with every list.
 */
export var wantedSession = null;

/// Cleared from the list as well, once the first list has settled whether the session it names is
/// there at all, and a name arriving there by import is read-only. Same variable, one more hop.
export function setWantedSession(id) { wantedSession = id; }

function sessionInHash(hash) {
    var found = /(?:^|[#&])session=([^&]*)/.exec(String(hash || ""));
    if (!found || !found[1]) return null;
    try { return decodeURIComponent(found[1]); } catch (e) { return found[1]; }
}

/**
 * A fragment can name a page as well as a session, and both are read here.
 *
 * `#page=usage` is what the menu writes when you press a row, so it is also what a bookmark, a
 * reload and the browser's own Back are answering — one address for one screen. A name this build
 * has no page for is ignored rather than obeyed: an old link, or a hand-typed one, leaves you
 * where you were instead of in front of a blank rectangle.
 *
 * The page is applied before the session, because `#session=…` means the session list with that
 * session open on it, and `openSession` says so itself.
 */
export function routeTo(hash) {
    var page = pageInHash(hash);
    if (page && Pages.knows(page)) Pages.go(page, { hash: false });
    var id = sessionInHash(hash);
    if (!id) return;
    wantedSession = id;
    openWanted();
}

/** Open the session the URL asked for, if it is in the list yet. */
export function openWanted() {
    if (!wantedSession) return false;
    if (!byId(wantedSession)) return false;
    var id = wantedSession;
    wantedSession = null;
    // A notification means there is something new to read. Force a transcript read even when
    // this session was already open and its fragment therefore routes to the same screen.
    openSession(id, false, true);
    return true;
}

window.addEventListener("hashchange", function () { routeTo(location.hash); });

// The service worker's `{type: "navigate", url}`. It is sent to a focused client instead of
// navigating it, because a client the worker does not control cannot be navigated at all and a
// URL that differs only in its fragment would not reload one that it could.
if ("serviceWorker" in navigator) {
    navigator.serviceWorker.addEventListener("message", function (ev) {
        var data = ev && ev.data;
        if (!data || data.type !== "navigate" || typeof data.url !== "string") return;
        var cut = data.url.indexOf("#");
        var hash = cut < 0 ? "" : data.url.slice(cut);
        if (!sessionInHash(hash)) return;      // `/` — the test push, and nothing to route to
        // Written into the address as well as acted on, so that a reload from here lands in the
        // same place. Setting it fires `hashchange`, which routes; when it is already what we
        // were sent, nothing fires and this does the routing itself.
        if (hash === location.hash) routeTo(hash);
        else location.hash = hash;
    });
}
