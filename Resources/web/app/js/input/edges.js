import { phone } from "../core/env.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { Build } from "../net/build.js";
import { render } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";

/* ---- the strip above the keyboard ---------------------------------------- */

/**
 * The `^ v` half of the bar iOS draws over the keyboard.
 *
 * It is there because the document has other things to tab to — the filter box first of all, and
 * the buttons in the header — and it is a pair of arrows that jump the writer out of the message
 * they are writing and into a search field. While the composer has the focus there is nothing
 * else on this page anybody wants to reach with them, so nothing else is in the tab order.
 *
 * **The `✓` is not ours and does not go.** There is no API for it: Safari draws it over every
 * real text field, and the only technique that removes the bar entirely is to stop using one —
 * a `contenteditable` div — which would put input-method composition, paste and the placeholder
 * at risk to tidy up a button. `autocomplete="off"` does not do it either; that attribute is
 * about autofill and has never had anything to say about this strip.
 */
(function keyboardBar() {
    var moved = null;

    function offstage() {
        onstage();
        moved = [];
        var all = document.querySelectorAll("a[href], button, input, select, textarea, [tabindex]");
        for (var i = 0; i < all.length; i++) {
            var el = all[i];
            if (el === els.msg || el.getAttribute("tabindex") === "-1") continue;
            moved.push([el, el.getAttribute("tabindex")]);
            el.setAttribute("tabindex", "-1");
        }
    }

    function onstage() {
        if (!moved) return;
        moved.forEach(function (pair) {
            if (pair[1] === null) pair[0].removeAttribute("tabindex");
            else pair[0].setAttribute("tabindex", pair[1]);
        });
        moved = null;
    }

    els.msg.addEventListener("focus", offstage);
    els.msg.addEventListener("blur", onstage);
    // The transcript is rebuilt under the writer while they write — a session that is working
    // sends a new one every few seconds — and every rebuild puts fresh buttons into the tab
    // order behind their back. So it is done again after a render, while the box still has the
    // focus. Cheap: it is a query and a few attribute writes.
    document.addEventListener("clawdline:rendered", function () {
        if (document.activeElement === els.msg) offstage();
    });
})();

/* ---- pull to refresh, phones only ---------------------------------------- */

(function pullToRefresh() {
    var scroller = els["list-scroll"], pad = els.ptr, label = els["ptr-label"];
    var startY = 0, pulling = false, distance = 0, busy = false;
    var THRESHOLD = 62;

    scroller.addEventListener("touchstart", function (ev) {
        if (busy || scroller.scrollTop > 0 || ev.touches.length !== 1) return;
        startY = ev.touches[0].clientY;
        pulling = true;
        distance = 0;
        pad.classList.add("dragging");
    }, { passive: true });

    scroller.addEventListener("touchmove", function (ev) {
        if (!pulling) return;
        // Resistance, so the list does not feel like it is on a spring — the further it comes,
        // the harder it pulls back, which is the only honest way to say "that is far enough".
        var raw = ev.touches[0].clientY - startY;
        if (raw <= 0) { distance = 0; pad.style.height = "0px"; return; }
        distance = Math.min(90, Math.pow(raw, 0.82));
        pad.style.height = distance + "px";
        label.textContent = distance >= THRESHOLD ? T.webPullRelease : T.webPull;
    }, { passive: true });

    function end() {
        if (!pulling) return;
        pulling = false;
        pad.classList.remove("dragging");
        if (distance >= THRESHOLD && !busy) {
            busy = true;
            label.textContent = T.webPullBusy;
            pad.style.height = "34px";
            // **When the page is behind, the gesture reloads it.** Re-fetching data is right
            // almost always and exactly wrong in the one case where somebody is being told to
            // reload: pulling down is what a phone means by reloading, so without this the
            // notice could not be dismissed by the gesture that appears to dismiss it.
            if (Build.stale) { location.reload(); return; }
            Promise.resolve(api.refresh ? api.refresh() : null).then(function () {
                setTimeout(function () {
                    pad.style.height = "0px";
                    label.textContent = T.webPull;
                    busy = false;
                }, 260);
            });
        } else {
            pad.style.height = "0px";
        }
    }
    scroller.addEventListener("touchend", end, { passive: true });
    scroller.addEventListener("touchcancel", end, { passive: true });
})();

/* ---- the layout can change under us -------------------------------------- */

window.addEventListener("resize", function () {
    // Coming back to a desk with a transcript open on the phone layout would otherwise leave
    // the detail pane sitting off to one side of a two-column grid.
    if (!phone()) els.app.dataset.view = "list";
    else if (S.openId) els.app.dataset.view = "detail";
    // The marks are drawn for a particular scale factor, and a window moved between screens
    // has a new one.
    render();
    if (S.openId) renderTranscript();
});
