/* ==========================================================================
   0. Where the data comes from
   The page is served by the app from the same origin, so every path here is
   relative and there is nothing to configure. ?mock=1 swaps the whole API for
   fixtures so the file opens from disk with no server behind it.
   ========================================================================== */

export var params = new URLSearchParams(location.search);
// A copy opened from disk has no server behind it and every relative path would resolve to a
// file that is not there, so file:// is mock whether or not it was asked for. Nobody opens this
// off a disk to talk to a server, and a page whose first act is four failed requests has taught
// its reader to ignore the console.
export var MOCK = location.protocol === "file:"
    || params.get("mock") === "1" || params.get("mock") === "";
export var MOCK_WRITE = params.get("write") === "1";   // mock only: see the composer switched on
export var MOCK_FLAKY = params.get("flaky") === "1";   // mock only: drop the stream now and then
export var MOCK_DOOR = params.get("door") === "1";     // mock only: arrive with no credentials

export var reduced = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
export var phone = function () { return window.matchMedia("(max-width: 899px)").matches; };

/**
 * Whether there is a keyboard to press Return on.
 *
 * Asked of the pointer rather than of the width, because the question is not how big the screen
 * is: an iPad with a keyboard attached should behave like a desk, and a browser window dragged
 * narrow on one should not start behaving like a phone. `hover` and `pointer: fine` together are
 * the closest a browser comes to "there is a mouse and therefore probably a keyboard".
 *
 * Read each time rather than once: a keyboard can be attached to a tablet while the page is open.
 */
/// Whether this page is being read on the machine that is serving it.
export function atMac() {
    var h = location.hostname;
    return h === "127.0.0.1" || h === "localhost" || h === "::1" || h === "[::1]";
}

export function hasKeyboard() {
    return !!(window.matchMedia && window.matchMedia("(hover: hover) and (pointer: fine)").matches);
}

/**
 * How tall the page actually is, written into `--vvh` for the layout to use.
 *
 * `100dvh` is the right answer for the browser's own toolbars and the wrong one for the
 * keyboard. iOS does not resize the layout viewport when the keyboard comes up: it leaves the
 * page the height it was and pans the part you can see. So a composer pinned to the bottom of
 * `100dvh` is pinned to a bottom that is now behind the keyboard, and what is left on screen is
 * the gap where it used to be. `visualViewport` is the one measurement that knows — its height
 * is what you can see and its `offsetTop` is how far down the page that view has been panned.
 *
 * Everything falls back to `100dvh` where there is no `visualViewport`, which is where `dvh` was
 * always correct anyway.
 */
(function trackVisibleViewport() {
    var vv = window.visualViewport;
    if (!vv) return;
    var root = document.documentElement;

    // **Only while the keyboard is up.**
    //
    // `visualViewport.height` is not the same as `100dvh` even with no keyboard: on iOS it
    // reports what is visible, which already stops short of the home indicator. Sizing the page
    // to it reserves that strip a second time — this page reserves it nowhere else now, but it
    // is still not the measurement wanted here, and back when the composer padded itself by the
    // inset the two together sat the input in a band of dead space two indicators tall.
    //
    // So the override is applied when the view is *meaningfully* shorter than the page, which
    // is the keyboard and nothing else. The threshold is well above any inset and well below
    // any keyboard.
    var KEYBOARD = 80;
    function apply() {
        var shrunk = (window.innerHeight - vv.height) > KEYBOARD;
        if (shrunk) {
            root.style.setProperty("--vvh", vv.height + "px");
            root.style.setProperty("--vvt", vv.offsetTop + "px");
        } else {
            // Removed rather than set to `100dvh`: the declaration's own fallback is that, and a
            // property that is present but "the same as the fallback" is one somebody later has
            // to reason about.
            root.style.removeProperty("--vvh");
            root.style.removeProperty("--vvt");
        }
    }
    vv.addEventListener("resize", apply);
    vv.addEventListener("scroll", apply);
    // And the window's own events as well, measured: resizing the frame this page was being
    // tested in changed `visualViewport.height` without firing a single `visualViewport` event,
    // and the layout kept the height it had. The keyboard is what `visualViewport` is for; a
    // rotation, a window drag and a browser toolbar sliding away are what these are for.
    window.addEventListener("resize", apply);
    window.addEventListener("orientationchange", apply);
    apply();
})();
