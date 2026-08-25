/* ==========================================================================
   Buzz
   A short knock on the wrist at the two moments a dictating thumb is not
   looking at the screen: the microphone opened, and the words came back.

   **Android only, and that is the end of the story on this page.**
   `navigator.vibrate` is a real specification that Android implements and
   Safari has never shipped — not partially, not badly: `navigator.vibrate` is
   `undefined` on an iPhone, from the home screen too.

   **The iPhone trick was tried here and does nothing.** Safari 17.4 gave
   `<input type="checkbox" switch>` the system switch look, and flipping one by
   hand plays the system haptic; the widely repeated next step is to keep a
   hidden one and `click()` it from script. That was written, shipped to a real
   iPhone on 2026-08-25, and produced no haptic at all — a programmatic click
   is not the interaction the platform plays it for. It is recorded here rather
   than merely deleted so that the next person who reads about the trick knows
   it was measured and not overlooked. Anything that revives it has to move the
   switch under a real thumb, which is a change to the buttons and not to this
   file.

   Everything here is best-effort by construction. A buzz that does not happen
   is not an error and is never reported as one.
   ========================================================================== */

/** Whether the page is in front of somebody. A buzz from a backgrounded tab is
 *  a phone going off in a pocket for something nobody is doing. */
function watched() {
    return typeof document === "undefined" || document.visibilityState !== "hidden";
}

/** Wrapped because a browser may have the method, refuse the call, and throw
 *  rather than return false — and a failed courtesy must not take a keystroke
 *  down with it. */
function vibrate(pattern) {
    if (!navigator.vibrate) return;
    try { navigator.vibrate(pattern); } catch (e) { /* nothing to say about it */ }
}

/**
 * One knock. For the moment the microphone opens, which is the one a thumb is
 * waiting on: until something confirms it, the honest reading of a silent
 * screen is that the press missed.
 */
export function tap() {
    if (watched()) vibrate(12);
}

/**
 * Two, for the words arriving. A different shape from `tap` on purpose: these
 * two events are seconds apart and mean opposite things, and a pocket cannot
 * tell apart two buzzes of the same length.
 */
export function done() {
    if (watched()) vibrate([12, 60, 12]);
}

/**
 * Whether anything here is going to be felt. For a settings row that would
 * otherwise offer a switch for a thing that cannot happen on this device.
 */
export function available() {
    return !!navigator.vibrate;
}
