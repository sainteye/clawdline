import { Pages, pageInHash } from "../core/pages.js";
import { byId } from "../view/derive.js";
import { openSession } from "../session/open.js";
import { Diagnostics } from "../core/layout-diagnostics.js";

/* ---- arriving at a session from somewhere else ---------------------------
 *
 * A notification about a session carries `/#session=<id>`. Declarative Web Push navigates the
 * installed app to that URL directly. The legacy service-worker fallback sends the same URL to an
 * already-open page, and both roads end here.
 *
 * The id is held rather than acted on when the list has not arrived yet: a cold start routes
 * before it knows what sessions exist, and `onSessions` tries again with every list.
 */
export var wantedSession = null;

/**
 * The same request, spelled the other way.
 *
 * A tmux pane is usually named `%141`. Notifications are encoded now, but older notifications
 * already on a phone used the raw spelling. Keep that spelling only when decoding produces a
 * control character, which is the evidence that an old pane id was mistaken for an escape.
 */
var wantedSessionAsWritten = null;

export function setWantedSession(id) {
    wantedSession = id;
    wantedSessionAsWritten = null;
}

function sessionCandidates(hash) {
    var found = /(?:^|[#&])session=([^&]*)/.exec(String(hash || ""));
    if (!found || !found[1]) return null;
    var raw = found[1];
    var decoded = raw;
    try { decoded = decodeURIComponent(raw); } catch (e) { decoded = raw; }
    if (decoded === raw) return [raw];
    return /[\u0000-\u001f\u007f-\u009f]/.test(decoded) ? [decoded, raw] : [decoded];
}

/**
 * Apply the page and Session named by a fragment. A fragment with no page means the Session list;
 * an unknown page name is ignored.
 */
export function routeTo(hash) {
    var candidates = sessionCandidates(hash);
    Diagnostics.note("route.to", {
        fragment: !!hash,
        page: pageInHash(hash) || "",
        names: candidates ? candidates.length : 0,
        rescued: !!(candidates && candidates.length > 1)
    });

    var page = pageInHash(hash);
    if (page) {
        if (Pages.knows(page)) Pages.go(page, { hash: false });
    } else {
        Pages.goHome({ hash: false });
    }

    if (!candidates) return;
    wantedSession = candidates[0];
    wantedSessionAsWritten = candidates.length > 1 ? candidates[1] : null;
    openWanted();
}

/** Open the session the URL asked for, if it is in the list yet. */
export function openWanted() {
    if (!wantedSession) return false;
    var id = byId(wantedSession) ? wantedSession
        : (wantedSessionAsWritten && byId(wantedSessionAsWritten)
            ? wantedSessionAsWritten : null);
    Diagnostics.note("route.openWanted", {
        found: !!id,
        rescued: !!(id && id === wantedSessionAsWritten),
        held: !!wantedSessionAsWritten
    });
    if (!id) return false;

    wantedSession = null;
    wantedSessionAsWritten = null;
    // A notification means there is something new to read. Force a transcript read even when
    // this session was already open and its fragment therefore routes to the same screen.
    openSession(id, false, true);
    return true;
}

window.addEventListener("hashchange", function () { routeTo(location.hash); });

// Browsers without Declarative Web Push still use the worker's `notificationclick` handler.
// The worker focuses an existing page and sends the destination here; changing the hash also
// keeps reload semantics identical to a declarative navigation.
if ("serviceWorker" in navigator) {
    navigator.serviceWorker.addEventListener("message", function (ev) {
        var data = ev && ev.data;
        Diagnostics.note("page.sw.message", {
            type: (data && data.type) || "",
            url: typeof (data && data.url) === "string",
            hidden: document.hidden
        });
        if (!data || data.type !== "navigate" || typeof data.url !== "string") return;

        var cut = data.url.indexOf("#");
        var hash = cut < 0 ? "" : data.url.slice(cut);
        if (!sessionCandidates(hash)) return;
        if (hash === location.hash) routeTo(hash);
        else location.hash = hash;
    });
}
