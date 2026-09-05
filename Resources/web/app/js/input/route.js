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

/**
 * The same request, spelled the other way, and it is not a nicety.
 *
 * A session id on this Mac is usually a tmux pane: `%141`. Written into the fragment raw — which
 * is what every notification did until `WebPush.sessionURL(forSessionID:)` — it arrives here as
 * `session=%141`, and `decodeURIComponent` does not refuse that. `%14` is a complete escape, so
 * the id became U+0014 followed by `1`, `byId` found no such session, the first whole list let go
 * of the request, and the tap stopped on the session list with nothing to say why. Ids without a
 * per-cent in them — iTerm's `w0t0p0:…` — went through both roads unchanged, which is why this
 * survived every fixture in `Tests/` and only ever happened on a phone.
 *
 * The address is written encoded now. This is the other half: the notifications already sitting
 * on somebody's phone carry the old spelling and are tapped days later, so the text exactly as it
 * was written stays a candidate beside the decoding of it. Held rather than resolved, because a
 * cold start routes before the list has arrived and both candidates have to survive that wait.
 */
var wantedSessionAsWritten = null;

/// Cleared from the list as well, once the first list has settled whether the session it names is
/// there at all, and a name arriving there by import is read-only. Same variable, one more hop —
/// and the second candidate goes with it, or letting go would not be letting go.
export function setWantedSession(id) { wantedSession = id; wantedSessionAsWritten = null; }

/**
 * What the fragment could be naming: the decoding first, and the raw text after it when the two
 * differ. Nothing when the fragment names no session at all.
 */
function sessionCandidates(hash) {
    var found = /(?:^|[#&])session=([^&]*)/.exec(String(hash || ""));
    if (!found || !found[1]) return null;
    var raw = found[1];
    var decoded = raw;
    try { decoded = decodeURIComponent(raw); } catch (e) { decoded = raw; }
    if (decoded === raw) return [raw];
    // **The second candidate is kept only when the decoding is itself the evidence.** A link
    // written before the encoding decodes to something no session could be called: `%141` becomes
    // U+0014 followed by `1`, because a pane id's per-cent was read as the start of an escape.
    // A link written after it decodes to a real id — `%25141` to `%141` — and there the raw text
    // is a *different real id* rather than a spelling of this one: `%252` is how `%2` is written
    // now, and a machine that has reached pane `%252` would open that stranger's session the day
    // `%2` closes. Nothing on screen would say so. So the raw text stays a candidate only where
    // the decoded one is impossible, and a control character is what impossible looks like.
    //
    // The cost is one narrow range: an old link naming `%20`–`%39` decodes to a printable
    // character — `%25` to `%`, `%20` to a space — so it is not rescued. That is where this was
    // before today, which is "does not route", and never "routes somewhere else".
    return /[\u0000-\u001f\u007f-\u009f]/.test(decoded) ? [decoded, raw] : [decoded];
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
 *
 * **And a fragment that names no page means the home page**, which is the half this had no `else`
 * for. Without it the address could stop saying where you are while the screen stayed put:
 * measured in a browser as `page=usage`, `hash=""`, Usage still drawn and `#app` still hidden —
 * one screen with no address, and a reload from there landing on the session list instead. On a
 * phone that is the back gesture: `session/open.js` pushes one entry, the pop closes the session
 * underneath, and the hashchange that followed used to change nothing at all.
 */
export function routeTo(hash) {
    var page = pageInHash(hash);
    // A name this build has no page for is still ignored rather than obeyed — an old link leaves
    // you where you were — so the two cases are "named one" and "named none", not "knows it".
    if (page) { if (Pages.knows(page)) Pages.go(page, { hash: false }); }
    else Pages.goHome({ hash: false });
    var ids = sessionCandidates(hash);
    if (!ids) return;
    wantedSession = ids[0];
    wantedSessionAsWritten = ids.length > 1 ? ids[1] : null;
    openWanted();
}

/** Open the session the URL asked for, if it is in the list yet. */
export function openWanted() {
    if (!wantedSession) return false;
    var id = byId(wantedSession) ? wantedSession
        : (wantedSessionAsWritten && byId(wantedSessionAsWritten) ? wantedSessionAsWritten : null);
    if (!id) return false;
    wantedSession = null;
    wantedSessionAsWritten = null;
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
        if (!sessionCandidates(hash)) return;  // `/` — the test push, and nothing to route to
        // Written into the address as well as acted on, so that a reload from here lands in the
        // same place. Setting it fires `hashchange`, which routes; when it is already what we
        // were sent, nothing fires and this does the routing itself.
        if (hash === location.hash) routeTo(hash);
        else location.hash = hash;
    });
}
