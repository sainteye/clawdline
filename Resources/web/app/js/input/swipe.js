import { phone, reduced } from "../core/env.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { ActionConfirm } from "./action-confirm.js";

/** A phone-only horizontal gesture layered over rows without borrowing their FLIP transform. */
export var SwipeRows = (function () {
    var actionWidth = 126;
    var row = null;          // the row this gesture has taken over, once it is a swipe
    var candidate = null;    // the row a finger is resting on, before anything has been decided
    var openRow = null;
    var startX = 0, startY = 0, startOffset = 0, offset = 0;
    var direction = null;
    var suppressClickUntil = 0;

    function buttonFor(node) { return node && node.querySelector(".swipe-end"); }

    function applyOffset(node, value) {
        node.style.setProperty("--swipe-x", value + "px");
        node.style.setProperty("--swipe-button-x", actionWidth + value + "px");
    }

    function cleanup(node) {
        if (!node) return;
        node.style.removeProperty("--swipe-x");
        node.style.removeProperty("--swipe-button-x");
        delete node.dataset.swipe;
        var button = buttonFor(node);
        if (button) button.hidden = true;
    }

    function settle(node, opened, instant) {
        if (!node) return;
        node.dataset.swipe = "settling";
        applyOffset(node, opened ? -actionWidth : 0);
        openRow = opened ? node : null;
        if (!opened) {
            if (instant || reduced) cleanup(node);
            else setTimeout(function () {
                if (node !== openRow && node.dataset.swipe !== "dragging") cleanup(node);
            }, 200);
        }
    }

    function reset(instant) {
        var active = row;
        var opened = openRow;
        row = null; candidate = null; direction = null;
        if (active && active !== opened) settle(active, false, !!instant);
        if (opened) settle(opened, false, !!instant);
    }

    /**
     * **A finger landing on a row changes nothing about that row.**
     *
     * This used to reveal the End button and put the row into `dragging` from `touchstart`,
     * which meant every tap in the list began by making something appear. On iOS that is not a
     * free thing to do: WebKit watches what a touch handler does to the page before it decides
     * whether the tap was a hover or a press, and a tap that reveals content is spent on the
     * hover — the click is never dispatched, and the row opens on the *second* tap. That is the
     * whole "I have to tap twice to open a session" bug, and it was on every row on every visit
     * to the list, because the button was unhidden again after each tap.
     *
     * So the row is only touched once the gesture has committed to being horizontal, which is
     * also when the button becoming visible means something. Until then this holds nothing but
     * numbers, and a tap reaches the row's own click handler untouched.
     */
    function touchstart(ev) {
        candidate = null;
        if (!phone() || !S.write || ev.touches.length !== 1) { reset(true); return; }
        if (ev.target.closest && ev.target.closest(".swipe-end")) return;
        var target = ev.target.closest ? ev.target.closest(".row") : null;
        if (!target || target.classList.contains("starting-row") || target.dataset.closing === "1") {
            reset(false); return;
        }
        candidate = target;
        direction = null;
        startX = ev.touches[0].clientX;
        startY = ev.touches[0].clientY;
        startOffset = openRow === target ? -actionWidth : 0;
        offset = startOffset;
    }

    /** The gesture is a swipe. Now the row may move and the button it uncovers may appear. */
    function arm() {
        row = candidate;
        candidate = null;
        var button = buttonFor(row);
        if (button) button.hidden = false;
        row.dataset.swipe = "dragging";
        applyOffset(row, offset);
    }

    function touchmove(ev) {
        if ((!row && !candidate) || ev.touches.length !== 1) return;
        var dx = ev.touches[0].clientX - startX;
        var dy = ev.touches[0].clientY - startY;
        if (!direction) {
            if (Math.max(Math.abs(dx), Math.abs(dy)) < 8) return;
            direction = Math.abs(dx) > Math.abs(dy) ? "horizontal" : "vertical";
            // The list is scrolling. Nothing was armed, so there is nothing to put back: the row
            // is left exactly as the finger found it, open or closed.
            if (direction === "vertical") { candidate = null; return; }
            arm();
        }
        if (!row) return;
        ev.preventDefault();
        offset = Math.max(-actionWidth, Math.min(0, startOffset + dx));
        applyOffset(row, offset);
    }

    function touchend() {
        candidate = null;
        // A tap and a vertical drag both end here having armed nothing, and both leave the
        // click alone — the row's own handler is what opens a session.
        if (!row) return;
        suppressClickUntil = Date.now() + 450;
        settle(row, offset < -actionWidth * 0.42, false);
        row = null; direction = null;
    }

    els["list-scroll"].addEventListener("touchstart", touchstart, { passive: true });
    els["list-scroll"].addEventListener("touchmove", touchmove, { passive: false });
    els["list-scroll"].addEventListener("touchend", touchend, { passive: true });
    els["list-scroll"].addEventListener("touchcancel", function () { reset(true); }, { passive: true });
    els["list-scroll"].addEventListener("scroll", function () { reset(false); }, { passive: true });

    document.addEventListener("touchstart", function (ev) {
        if (openRow && !openRow.contains(ev.target)) reset(false);
    }, { capture: true, passive: true });

    document.addEventListener("click", function (ev) {
        var action = ev.target.closest ? ev.target.closest(".swipe-end") : null;
        if (action) {
            ev.preventDefault(); ev.stopPropagation();
            if (!phone() || !S.write) { reset(true); return; }
            var target = action.closest(".row");
            var id = target && target.dataset.id;
            reset(true);
            if (id) ActionConfirm.open("end", id, target);
            return;
        }
        if (Date.now() < suppressClickUntil) {
            ev.preventDefault(); ev.stopPropagation(); return;
        }
        if (!openRow) return;
        var wasOpen = openRow;
        reset(false);
        if (wasOpen.contains(ev.target)) { ev.preventDefault(); ev.stopPropagation(); }
    }, true);

    window.addEventListener("resize", function () { if (!phone()) reset(true); });
    new MutationObserver(function () {
        if (openRow && (!document.documentElement.contains(openRow)
                        || openRow.getAttribute("aria-hidden") === "true")) reset(true);
    }).observe(els.rows, { childList: true, subtree: true, attributes: true,
                          attributeFilter: ["aria-hidden"] });

    return { reset: reset };
})();
