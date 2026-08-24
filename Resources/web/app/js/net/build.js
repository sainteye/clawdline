import { els } from "../core/dom.js";

/* ==========================================================================
   4. Transport
   Two implementations behind one shape: the real thing, and fixtures. Nothing
   below this section knows which one it is talking to.
   ========================================================================== */

/**
 * Whether the app on the Mac has moved on since this page was served.
 *
 * A standalone PWA is not a document somebody reloads; it is a window that sits in the app
 * switcher for days. Nothing here caches — the service worker stores nothing — so the page is
 * not stale in the browser's sense, it is simply *old*, and the browser has no reason to think
 * anything is wrong. That is how a phone came to be running a build from an hour before while
 * the Mac had been rebuilt twice, with no sign of it anywhere on screen.
 *
 * The check is the cheapest one available: `/v1/health` already answers on every connect and
 * every reconnect, and the first answer of the session is the baseline. **Anything that changes
 * when the app is rebuilt will do** — this reads `build` first and falls back to `version` and
 * the protocol number, so the day the server carries a per-build stamp this starts working
 * without another line here. (Today `version` is the string in `build.sh`, which two rebuilds of
 * the same release do not change, so today this fires only across a version bump.)
 *
 * **Never a reload of its own.** Somebody may be mid-sentence in the composer, and a page that
 * replaces itself to be newer has thrown that away to win an argument nobody was having.
 */
export var Build = {
    seen: null,
    stale: false,
    rebase: false,

    stamp: function (info) {
        var parts = [info.build, info.version, info.protocol];
        var out = [];
        for (var i = 0; i < parts.length; i++) {
            if (parts[i] === null || parts[i] === undefined) continue;
            out.push(String(parts[i]));
        }
        return out.join("|");
    },

    saw: function (info) {
        if (!info || this.stale) return;
        var now = this.stamp(info);
        if (!now) return;
        if (this.seen === null || this.rebase) { this.rebase = false; this.seen = now; return; }
        if (now === this.seen) return;
        this.stale = true;
        if (els.stale) els.stale.hidden = false;
    },

    /// Told, and not reloading now.
    ///
    /// **Without this the notice cannot be got rid of.** It is correct — the page really is
    /// behind — but somebody halfway through a sentence has no way to make it go, and reloading
    /// to silence a banner is exactly what they are trying not to do. Dismissing re-bases on
    /// what the server is saying now, so it stays quiet until the *next* rebuild rather than
    /// until the end of time.
    hush: function () {
        this.stale = false;
        // The next reading becomes the new baseline. Re-basing here instead would need the
        // current answer at the moment of the click, and the click has no reason to have one.
        this.rebase = true;
        if (els.stale) els.stale.hidden = true;
    }
};
