import { T } from "../core/i18n.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { api } from "../net/api.js";
import { Settings } from "./settings.js";

/* ---- notifications ------------------------------------------------------- */

/**
 * Web Push: the phone buzzes when a session is waiting for an answer.
 *
 * Four things can be true here and only one of them is "on", so the footer says which. A button
 * that has been pressed and did nothing is the worst of the four — that is what a permission the
 * reader denied looks like from inside the page, and the only cure for it is in the browser's own
 * settings, which is a sentence rather than a control.
 *
 * **On iOS this only works from the home screen.** Not "works badly" — the API is absent in a
 * Safari tab, so there is nothing to press and nothing to explain afterwards. The one sentence
 * that gets somebody from there to a working notification is therefore the whole feature until
 * they have read it, and it is shown instead of a button rather than beside one.
 *
 * The service worker is the app's own `/sw.js`. It already knows how to draw a notification and
 * what to do when one is tapped; this end registers it and hands it a subscription.
 */
export var Push = (function () {
    /// **One spelling of the path, and two callers.** `web-service-worker.mjs` holds this against
    /// the route that answers it, and it does so by counting the literal — so the literal lives
    /// here and the callers say this instead.
    var SW_PATH = "/sw.js";

    var registration = null;
    var subscribed = false;
    var state = "unsupported";   // unsupported | homescreen | blocked | off | on
    var busy = false;
    /** Whether the subscription has actually been looked up. Until it has, `decide()` answers
     *  "off" because `subscribed` starts false — which is a default and not a reading, and it is
     *  the one state that puts a button on the screen. See the markup.
     *
     *  Only the footer waits on this. The settings sheet is drawn from `state` either way, so a
     *  browser whose worker registers and then never activates — the one case where nothing here
     *  ever settles — still has somewhere to turn notifications on from, and the row along the
     *  bottom of the list is not offering a button that could not have worked. */
    var settled = false;

    /** The id the server gave this subscription, kept so it can be taken back after a reload. */
    function remember(value) {
        try {
            if (value == null) localStorage.removeItem("clawdline.push");
            else localStorage.setItem("clawdline.push", String(value));
        } catch (e) { /* a private window has no storage, and this is not worth failing over */ }
    }
    function recall() {
        try { return localStorage.getItem("clawdline.push"); } catch (e) { return null; }
    }

    function standalone() {
        return window.navigator.standalone === true
            || (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches);
    }

    /** iPadOS calls itself a Mac, and a touch screen is the only tell left. */
    function iOS() {
        var platform = navigator.platform || "";
        return /iP(hone|ad|od)/.test(platform)
            || (/Mac/.test(platform) && navigator.maxTouchPoints > 1);
    }

    function decide() {
        if (iOS() && !standalone()) return "homescreen";
        if (typeof api.pushKey !== "function") return "unsupported";      // mock mode has no server
        if (!window.isSecureContext) return "unsupported";
        if (!("serviceWorker" in navigator) || !("PushManager" in window)) return "unsupported";
        if (typeof Notification === "undefined") return "unsupported";
        if (Notification.permission === "denied") return "blocked";
        return subscribed ? "on" : "off";
    }

    /// The VAPID key arrives as base64url and `subscribe` wants bytes.
    function keyBytes(key) {
        var padded = String(key).replace(/-/g, "+").replace(/_/g, "/");
        while (padded.length % 4) padded += "=";
        var raw = atob(padded);
        var out = new Uint8Array(raw.length);
        for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
        return out;
    }

    /// Safari answered this with a callback for years before it answered with a promise.
    function askPermission() {
        return new Promise(function (done, fail) {
            try {
                var maybe = Notification.requestPermission(function (answer) { done(answer); });
                if (maybe && typeof maybe.then === "function") maybe.then(done, fail);
            } catch (e) { fail(e); }
        });
    }

    function draw() {
        state = busy ? state : decide();
        // **Only the two states somebody can act on from here keep a place in the flow.** Off is
        // an offer and needs a button; on iOS in a tab there is no button to have, and the one
        // sentence that gets somebody to a working notification is the whole feature until they
        // have read it. Everything else — already on, blocked, this browser cannot — is a fact
        // rather than a thing to do, and a fact does not get a permanent row of the screen.
        // `settled` first: a footer that has not been decided yet is not in the flow, whatever
        // the placeholder state says. Appearing a few frames late is a layout shift; appearing
        // and then vanishing is a fault.
        var inFlow = settled && (state === "off" || state === "homescreen");
        els.notify.hidden = !inFlow;
        els.notify.dataset.state = state;

        els["notify-go"].hidden = state !== "off";
        els["notify-go"].disabled = busy;
        els["notify-go-label"].textContent = busy ? T.webNotifyAsking : T.webNotifyGo;
        els["notify-say"].textContent =
            state === "homescreen" ? T.webNotifyHomeScreen : T.webNotifyOff;

        Settings.drawNotify(state, busy);
    }

    function enable() {
        busy = true; draw();
        askPermission().then(function (answer) {
            if (answer !== "granted") { busy = false; draw(); return null; }
            return navigator.serviceWorker.ready.then(function (r) {
                registration = r;
                return api.pushKey();
            }).then(function (d) {
                return registration.pushManager.subscribe({
                    userVisibleOnly: true,
                    applicationServerKey: keyBytes(d.key)
                });
            }).then(function (subscription) {
                return api.pushSubscribe(subscription.toJSON());
            }).then(function (d) {
                subscribed = true;
                remember(d && d.id);
                busy = false; draw();
                // Straight to the sheet, because the moment permission has been granted is the
                // moment somebody wants proof — and the test button is in there. A toast saying
                // "this will work now" is the page asking to be taken on trust.
                Settings.open();
            });
        }).catch(function (e) {
            busy = false; draw();
            toast(e && e.message ? e.message : T.webNotifyOnFailed, true);
        });
    }

    function disable() {
        busy = true; draw();
        var id = recall();
        navigator.serviceWorker.ready.then(function (r) {
            return r.pushManager.getSubscription();
        }).then(function (subscription) {
            return subscription ? subscription.unsubscribe() : null;
        }).then(function () {
            // Told, but not waited on: the subscription is already gone from this browser, and a
            // server that never hears about it will drop it the first time it pushes to nothing.
            return id ? api.pushUnsubscribe(id).catch(function () { return null; }) : null;
        }).then(function () {
            subscribed = false;
            remember(null);
            busy = false; draw();
        }).catch(function (e) {
            busy = false; draw();
            toast(e && e.message ? e.message : T.webNotifyOffFailed, true);
        });
    }

    return {
        redraw: draw,
        start: function () {
            draw();
            // These two are read off this browser rather than off a subscription, so they are
            // known now and there is nothing to wait for.
            if (decide() === "unsupported" || decide() === "homescreen") {
                settled = true;
                draw();
                return;
            }
            navigator.serviceWorker.register(SW_PATH).then(function (r) {
                registration = r;
                return navigator.serviceWorker.ready;
            }).then(function (r) {
                return r.pushManager.getSubscription();
            }).then(function (subscription) {
                // Both halves have to agree. A subscription this browser still holds but the app
                // has forgotten — reinstalled, database cleared — would draw as "on" and never
                // arrive, so what is remembered here is the id the server gave back.
                subscribed = !!subscription && !!recall();
                settled = true;
                draw();
            }).catch(function () {
                // No worker means no notifications, and the footer already has a sentence for it.
                settled = true;
                draw();
            });
        },
        /// Ask the browser to look for a newer worker.
        ///
        /// **`register()` on every load is supposed to be this**, and on 2026-09-07 it was not:
        /// a phone ran a page from one build over a worker from an earlier one for long enough
        /// to produce 116 reads of a record that worker did not know how to write. Whatever the
        /// browser's own schedule is, this asks. It is one conditional request against a route
        /// served `no-cache`, it never reloads anything by itself, and a browser with no worker
        /// or no registration yet simply has nothing to do.
        recheck: function () {
            try {
                if (!registration || typeof registration.update !== "function") return;
                var asked = registration.update();
                if (asked && typeof asked.catch === "function") asked.catch(function () {});
            } catch (e) { /* an update check is never worth an exception on this path */ }
        },

        /// Throw the worker away and install it again.
        ///
        /// **The hammer, for when asking has not worked.** `register()` on every load and
        /// `update()` at both wake-ups are the polite forms, and on 2026-09-07 a phone ran a page
        /// from one build over a worker from an earlier one through several rounds of both. A
        /// worker that is not the build this page is talking to cannot do what this page expects
        /// of it, and every reading taken under it is about a program nobody is looking at.
        ///
        /// Costs one uninstall and one install, and the browser serves `sw.js` `no-cache`, so the
        /// copy that comes back is the current one. Never called unless the two builds are known
        /// and different.
        reinstall: function () {
            try {
                if (!("serviceWorker" in navigator)) return Promise.resolve(false);
                var again = function () {
                    return navigator.serviceWorker.register(SW_PATH).then(function (r) {
                        registration = r;
                        return true;
                    });
                };
                if (!registration || typeof registration.unregister !== "function") return again();
                return registration.unregister().then(again, again);
            } catch (e) { return Promise.resolve(false); }
        },

        toggle: function () {
            if (busy) return;
            if (state === "off") enable();
            else if (state === "on") disable();
        }
    };
})();

els["notify-go"].addEventListener("click", function () { Push.toggle(); });
