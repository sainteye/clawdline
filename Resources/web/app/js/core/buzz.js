/* ==========================================================================
   Buzz
   A short knock on the wrist at the two moments a dictating thumb is not
   looking at the screen: the microphone opened, and the words came back.

   **There is no one API for this, and on iPhone there is no API at all.**
   `navigator.vibrate` is a real specification that Android implements and
   Safari has never shipped — not partially, not badly: `navigator.vibrate`
   is `undefined` on an iPhone, from the home screen too. So this file has
   two mechanisms and picks whichever the browser it woke up in will answer.

   The second one is not an interface, it is a side effect somebody noticed.
   Safari 17.4 gave `<input type="checkbox" switch>` the iOS switch look, and
   flipping one plays the system haptic that a real switch plays. Nothing was
   promised about it, no specification mentions it, and Apple can take it back
   in a point release without breaking any documented behaviour — so it is
   wired here the way a rumour should be: detected, wrapped, silent when it
   fails, and load-bearing for nothing. The recording still starts and the
   words still arrive if it does nothing at all.

   Everything here is best-effort by construction. A buzz that does not happen
   is not an error and is never reported as one.
   ========================================================================== */

/** Whether the page is in front of somebody. A buzz from a backgrounded tab is
 *  a phone going off in a pocket for something nobody is doing. */
function watched() {
    return typeof document === "undefined" || document.visibilityState !== "hidden";
}

/** Android and everything else that implements the specification. Returns
 *  whether it actually did something, so the caller can stop looking. */
function vibrate(pattern) {
    if (!navigator.vibrate) return false;
    // Wrapped because a browser may have the method, refuse the call, and throw
    // rather than return false — and a failed courtesy must not take a keystroke
    // down with it.
    try { return navigator.vibrate(pattern) === true; } catch (e) { return false; }
}

/* --- The iPhone side, which is a trick and is treated as one ------------- */

/** Does this browser know the `switch` attribute at all? Safari reflects it as
 *  an IDL property, so this is a question about the platform rather than a
 *  version string — the day another browser ships it, this asks the same thing
 *  and gets the right answer. */
var understandsSwitch = (function () {
    try { return "switch" in document.createElement("input"); } catch (e) { return false; }
})();

var lever = null;

/** The hidden switch.
 *
 *  **It has to be drawn to be felt.** `display: none` and `visibility: hidden`
 *  take an element out of rendering, and an element that is not rendered plays
 *  no haptic — so this is a real, laid-out, one-pixel control parked off the
 *  bottom of the page at zero opacity, with `pointer-events: none` so that it
 *  can never be the thing a thumb lands on and `aria-hidden` so that it is not
 *  read out as a switch nobody put there.
 */
function haptic() {
    if (!understandsSwitch) return false;
    try {
        if (!lever) {
            lever = document.createElement("input");
            lever.type = "checkbox";
            lever.setAttribute("switch", "");
            lever.tabIndex = -1;
            lever.setAttribute("aria-hidden", "true");
            lever.style.cssText = "position:fixed;bottom:-2px;left:-2px;width:1px;height:1px;"
                                + "opacity:0;pointer-events:none;margin:0;padding:0;border:0";
            document.body.appendChild(lever);
        }
        // `click()` rather than assigning `checked`: setting the property moves
        // the switch without anything happening around it, and what plays the
        // haptic is the toggle being performed. Which state it lands in does not
        // matter — nothing reads this control — so it is left wherever it goes.
        lever.click();
        return true;
    } catch (e) {
        return false;
    }
}

/* --- What the rest of the app calls -------------------------------------- */

/**
 * One knock. For the moment the microphone opens, which is the one a thumb is
 * waiting on: until something confirms it, the honest reading of a silent
 * screen is that the press missed.
 */
export function tap() {
    if (!watched()) return;
    if (vibrate(12)) return;
    haptic();
}

/**
 * Two, for the words arriving. A different shape from `tap` on purpose: these
 * two events are seconds apart and mean opposite things, and a pocket cannot
 * tell apart two buzzes of the same length.
 *
 * On the iPhone side they are the same knock played twice, because the trick
 * has one sound and no pattern argument. Better a weaker distinction than a
 * second mechanism to go stale.
 */
export function done() {
    if (!watched()) return;
    if (vibrate([12, 60, 12])) return;
    if (!haptic()) return;
    window.setTimeout(function () { if (watched()) haptic(); }, 72);
}

/**
 * Whether anything here is going to be felt. For a settings row that would
 * otherwise offer a switch for a thing that cannot happen on this device.
 */
export function available() {
    return !!navigator.vibrate || understandsSwitch;
}
