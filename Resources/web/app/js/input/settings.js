import { T, fill } from "../core/i18n.js";
import { S, storeBool } from "../core/state.js";
import { els } from "../core/dom.js";
import { Pages } from "../core/pages.js";
import { assistantLogo } from "../core/pixels.js";
import { api } from "../net/api.js";
import { renderTranscript } from "../view/transcript.js";
import { toggleOrder } from "./keys.js";
import { Push } from "./push.js";

/* ---- the settings page --------------------------------------------------- */

/**
 * What is true of this browser on this device, which is a different question from anything in
 * the session list — so it is somewhere else rather than louder.
 *
 * It came out of a footer that held a "Stop" button and a sentence, permanently, on every screen.
 * Turning notifications off again is a once-a-year press and it was costing a row of a phone
 * display for ever; what stays in the flow is the one state that is asking to be pressed, and
 * everything else moved in here behind the wordmark.
 *
 * **It is a page now, and this is where that shows.** `open` no longer unhides anything: it asks
 * `Pages` to go there, and `Pages` calls `enter` once it has. Everything `open` used to do apart
 * from that one line is in `enter`, unchanged — which is why arriving by the address bar or by
 * the back button draws exactly what pressing the row in the menu draws.
 */
export var Settings = (function () {
    var testing = false;

    /** What just happened, said in the sheet rather than in a toast. A toast has gone by the
     *  time somebody has looked at their lock screen to see whether the test arrived. */
    function say(words, calm) {
        els["settings-notify-said"].textContent = words || "";
        els["settings-notify-said"].className = "said" + (calm ? " calm" : "");
    }

    return {
        busy: function () { return testing; },

        /** Take me there. Whether it is already on screen is `Pages`' business. */
        open: function () { Pages.go("settings"); },

        /** Drawn on arrival, however the arrival happened. */
        enter: function () {
            say("");
            els["settings-version"].textContent =
                S.version ? fill(T.webSettingsVersion, { v: S.version }) : "";
            Push.redraw();
            this.drawAssistantIcons();
            this.drawOrder();
        },

        close: function () { Pages.goHome(); },

        drawAssistantIcons: function () {
            var button = els["settings-assistant-icons"];
            button.classList.toggle("on", S.assistantIcons);
            button.setAttribute("aria-pressed", S.assistantIcons ? "true" : "false");
            els["settings-assistant-icons-marks"].innerHTML =
                assistantLogo("claude") + assistantLogo("codex");
        },

        /**
         * The transcript's order, as it was drawn in the header it came from: the words say
         * which order this *is*, the arrow says the same thing in a shape and turns over with
         * it, and the hover text says what pressing it will do.
         */
        drawOrder: function () {
            els["settings-order-label"].textContent =
                S.newestFirst ? T.webOrderNewest : T.webOrderOldest;
            els["settings-order"].classList.toggle("on", S.newestFirst);
        },

        /** The notifications block, drawn from the state Push worked out. */
        drawNotify: function (state, busy) {
            els["settings-notify-say"].textContent = {
                homescreen: T.webNotifyHomeScreen,
                unsupported: T.webNotifyUnsupported,
                blocked: T.webNotifyBlocked,
                on: T.webNotifyOn,
                off: T.webNotifySheetOff
            }[state] || "";

            var go = els["settings-notify-go"];
            go.hidden = !(state === "off" || state === "on");
            go.disabled = busy;
            go.textContent = state === "on" ? (busy ? T.webNotifyStopping : T.webNotifyStop)
                                            : (busy ? T.webNotifyAsking : T.webNotifyGo);
            go.classList.toggle("on", state === "on");

            // Only offered where there is something subscribed for it to reach.
            var test = els["settings-notify-test"];
            test.hidden = state !== "on";
            test.disabled = testing;
            test.textContent = testing ? T.webSending : T.webNotifyTest;
        },

        test: function () {
            if (testing || typeof api.pushTest !== "function") return;
            testing = true;
            say("");
            Push.redraw();
            api.pushTest().then(function () {
                say(T.webNotifyTestSent, true);
            }).catch(function (e) {
                // A 409 is not a failure to apologise for: it means this browser believes
                // notifications are on and the Mac has nothing to send to. That is a state with
                // a one-sentence way out of it, and saying the sentence is more use than an
                // error toast with a number in it.
                say(e && e.code === "not_subscribed" ? T.webNotifyTestNone
                    : (e && e.message) || T.webNotifyTestFailed);
            }).then(function () {
                testing = false;
                Push.redraw();
            });
        }
    };
})();

// The wordmark opens the menu (`input/sidebar.js`), the menu's Settings row and this page's own
// Close both carry `data-page-to`, and `core/pages.js` answers all three. What used to be four
// listeners here — the wordmark, the scrim, the sheet swallowing its own clicks, and Close — is
// none: a page has no outside to tap, and its way out is the same attribute every other one uses.
els["settings-notify-go"].addEventListener("click", function () { Push.toggle(); });
els["settings-notify-test"].addEventListener("click", function () { Settings.test(); });
els["settings-order"].addEventListener("click", toggleOrder);
els["settings-assistant-icons"].addEventListener("click", function () {
    S.assistantIcons = !S.assistantIcons;
    storeBool("clawdline.assistant-icons", S.assistantIcons);
    Settings.drawAssistantIcons();
    renderTranscript();
});
