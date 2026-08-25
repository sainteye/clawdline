import { MOCK } from "../core/env.js";
import { T } from "../core/i18n.js";

/* ---- the real one -------------------------------------------------------- */

export function jsonFetch(path, options) {
    return fetch(path, options).catch(function () {
        // `fetch` rejects with "Failed to fetch" and nothing else when there is no server at the
        // other end. That string ends up in front of somebody as an explanation, so it is turned
        // into one here rather than in each of the five places that show a message.
        var dead = new Error(T.webOffline);
        dead.code = "offline";
        throw dead;
    }).then(function (res) {
        return res.text().then(function (body) {
            var data = null;
            try { data = body ? JSON.parse(body) : null; } catch (e) { /* below */ }
            if (!res.ok) {
                var err = data && data.error ? data.error : { code: "http_" + res.status, message: res.statusText || T.webRequestFailed };
                var e2 = new Error(err.message || err.code);
                e2.code = err.code;
                // `code` and `message` are on every refusal this server makes; a route may add
                // one more field, and dropping it here would cost the page its own sentence.
                // Today there are two. Both 409s from starting a session carry `app`, the
                // terminal's name as macOS spells it, so that a phone can say "Ghostty is not
                // running" in the reader's language instead of showing the English. And the 503
                // from dictation carries `reason`, which is the difference between "install
                // Whisper" and "Whisper is there but has no model" — two different afternoons.
                if (typeof err.app === "string") e2.app = err.app;
                if (typeof err.reason === "string") e2.reason = err.reason;
                throw e2;
            }
            if (!data) throw new Error(T.webNotJSON);
            return data;
        });
    });
}

/** A POST of JSON, which is the shape every write on this server takes. */
export function post(body, extra) {
    var headers = { "Content-Type": "application/json" };
    for (var k in (extra || {})) headers[k] = extra[k];
    return { method: "POST", headers: headers, body: JSON.stringify(body || {}) };
}

/**
 * A token handed to this page in the URL fragment, traded for a cookie.
 *
 * The Settings window on the Mac mints one and opens `#t=…`. A fragment is the right envelope
 * for a credential: browsers do not send it to the server and do not write it into access logs.
 * The trade is needed because **`EventSource` cannot carry a header** — a token held in a
 * variable can fetch, but it cannot open the stream that everything else here depends on.
 *
 * The fragment is wiped before the request goes out rather than after it comes back. What is
 * being removed is a live credential sitting in an address bar on somebody's screen, and there
 * is no version of "wait for the round trip" that is better than that.
 */
export function adoptToken() {
    var match = /(?:^|[#&])t=([^&]+)/.exec(location.hash || "");
    if (!match) return Promise.resolve(false);
    var token = decodeURIComponent(match[1]);
    stripFragment();
    if (MOCK) return Promise.resolve(true);
    return jsonFetch("/v1/auth/adopt", post({ token: token }))
        .then(function () { return true; })
        .catch(function () { return false; });   // the door is a moment away and says it better
}

function stripFragment() {
    try {
        history.replaceState(null, "", location.pathname + location.search);
    } catch (e) {
        location.hash = "";                      // older Safari from a file:// copy
    }
}
