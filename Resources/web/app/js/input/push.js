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
            navigator.serviceWorker.register("/sw.js").then(function (r) {
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
        toggle: function () {
            if (busy) return;
            if (state === "off") enable();
            else if (state === "on") disable();
        }
    };
})();

els["notify-go"].addEventListener("click", function () { Push.toggle(); });
