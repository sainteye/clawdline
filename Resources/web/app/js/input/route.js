import { Pages, pageInHash } from "../core/pages.js";
import { byId } from "../view/derive.js";
import { openSession } from "../session/open.js";
import { Diagnostics } from "../core/layout-diagnostics.js";
import { boardLocatorFromHash } from "../view/board.js";
import { sessionLocatorFromHash } from "../net/session-links.js";

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
export var wantedDocument = null;
export var wantedBoard = null;
var openDocumentRoute = null;
var parseDocumentRoute = null;
var hideDocumentRoute = null;
var openBoardRoute = null;
var hideBoardRoute = null;
var openSessionLocatorRoute = null;
var hideSessionLocatorRoute = null;
var sessionLocatorIntent = false;

export function bindSessionLocatorRoute(open, hide) {
    openSessionLocatorRoute = open;
    hideSessionLocatorRoute = hide;
}

/** Installed by the document page after the page registry and its transport thunks exist. */
export function bindDocumentRoute(open, parse, hide) {
    openDocumentRoute = open;
    parseDocumentRoute = parse;
    hideDocumentRoute = hide;
}

/** Installed by the Board after its transport and page controller exist. */
export function bindBoardRoute(open, hide) {
    openBoardRoute = open;
    hideBoardRoute = hide;
}

function documentIntent(hash) {
    var source = String(hash || "");
    if (!/(?:^|[#&])document=/.test(source)) return null;
    var locator = parseDocumentRoute ? parseDocumentRoute(source) : null;
    return locator ? { locator: locator, error: null }
        : { locator: null, error: "The document link is malformed or carries an unsupported field." };
}

function boardIntent(hash) {
    var source = String(hash || "");
    if (!/(?:^|[#&])page=board(?:&|$)/.test(source)) return null;
    if (source === "#page=board" || source === "page=board") return null;
    var locator = boardLocatorFromHash(source);
    return locator ? { locator: locator, error: null }
        : { locator: null, error: "The Board link is malformed or carries an unsupported field." };
}

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
    if (/(?:^|[#&])session_ref=/.test(String(hash || ""))) {
        sessionLocatorIntent = true;
        wantedSession = null;
        wantedSessionAsWritten = null;
        wantedDocument = null;
        wantedBoard = null;
        if (hideDocumentRoute) hideDocumentRoute();
        if (hideBoardRoute) hideBoardRoute();
        const locator = sessionLocatorFromHash(hash);
        Pages.goHome({ hash: false });
        if (openSessionLocatorRoute)
            openSessionLocatorRoute(locator, locator ? null : "session_link_invalid");
        return;
    }
    sessionLocatorIntent = false;
    if (hideSessionLocatorRoute) hideSessionLocatorRoute();
    var candidates = sessionCandidates(hash);
    var documentRoute = documentIntent(hash);
    var boardRoute = boardIntent(hash);
    Diagnostics.note("route.to", {
        fragment: !!hash,
        page: pageInHash(hash) || "",
        names: candidates ? candidates.length : 0,
        rescued: !!(candidates && candidates.length > 1),
        document: !!documentRoute,
        board: !!boardRoute
    });

    if (boardRoute) {
        wantedSession = null;
        wantedSessionAsWritten = null;
        wantedDocument = null;
        wantedBoard = boardRoute.locator;
        if (hideDocumentRoute) hideDocumentRoute();
        if (openBoardRoute) openBoardRoute(boardRoute.locator, boardRoute.error);
        return;
    }

    wantedBoard = null;
    if (hideBoardRoute) hideBoardRoute();

    if (documentRoute) {
        wantedSession = null;
        wantedSessionAsWritten = null;
        wantedDocument = documentRoute.locator;
        wantedBoard = null;
        if (openDocumentRoute) {
            openDocumentRoute(documentRoute.locator, documentRoute.error);
        }
        return;
    }

    wantedDocument = null;
    wantedBoard = null;
    if (hideDocumentRoute) hideDocumentRoute();
    if (hideBoardRoute) hideBoardRoute();

    var page = pageInHash(hash);
    if (page) {
        if (Pages.knows(page)) Pages.go(page, { hash: false });
    } else {
        Pages.goHome({ hash: false });
    }

    if (!candidates) return;
    wantedDocument = null;
    wantedSession = candidates[0];
    wantedSessionAsWritten = candidates.length > 1 ? candidates[1] : null;
    openWanted();
}

/** Open the session the URL asked for, if it is in the list yet. */
export function openWanted() {
    // The stable controller owns this address, including missing/invalid destinations.
    // Do not let the first-list convenience open an unrelated Session behind its error sheet.
    if (sessionLocatorIntent) return true;
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
        if (!sessionCandidates(hash) && !documentIntent(hash)
            && !/(?:^|[#&])session_ref=/.test(hash)) return;
        if (hash === location.hash) routeTo(hash);
        else location.hash = hash;
    });
}
