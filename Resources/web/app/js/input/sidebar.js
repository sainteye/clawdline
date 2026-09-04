import { els } from "../core/dom.js";
import { Pages } from "../core/pages.js";

/* ---- the way to the other pages ------------------------------------------
 *
 * The wordmark used to open the settings sheet, which was the only app-wide
 * destination there was; now it opens the list of them. Same gesture, same
 * corner, and one more thing on it every time a page is added — which is the
 * whole reason this exists rather than a second permanent control in a header
 * that has one line on a phone.
 *
 * It is a drawer at every width rather than a column pinned open on a desk.
 * A phone is the screen this app is mostly read on, and one behaviour that is
 * right there beats two that have to be kept in step; the desk pays a tap for
 * it and gets the session list's full width back in exchange.
 *
 * It sits under the header on purpose. The logo is what opens it, so the logo
 * has to stay pressable while it is open — pressing it again is the shortest
 * way back out, and it is the one people try first.
 */
export var Sidebar = (function () {
    function mark(open) {
        els.brand.setAttribute("aria-expanded", open ? "true" : "false");
    }

    return {
        open: function () {
            els.sidebar.hidden = false;
            mark(true);
            // The current page's row, so a keyboard arrives on the thing it is
            // most likely to want and a screen reader is told where it is.
            var here = els.sidebar.querySelector('[aria-current="page"]');
            if (here && here.focus) here.focus({ preventScroll: true });
        },

        close: function () {
            if (els.sidebar.hidden) return;
            // Where the focus is has to be read *before* the drawer goes, because
            // `hidden` takes the focused row out of the document and the browser
            // drops focus on the body — from where nothing can be given back.
            var held = els.sidebar.contains(document.activeElement);
            els.sidebar.hidden = true;
            mark(false);
            // Only when it was in here. Closing behind a choice is the other way
            // this is called, and by then the new page has already put focus where
            // it wants it; pulling it back to the wordmark would undo that.
            if (held) els.brand.focus({ preventScroll: true });
        },

        toggle: function () { if (els.sidebar.hidden) this.open(); else this.close(); }
    };
})();

els.brand.addEventListener("click", function () { Sidebar.toggle(); });
// The dark half of the drawer is the way out, the way a tap outside a sheet is.
els.sidebar.addEventListener("click", function () { Sidebar.close(); });
els["sidebar-panel"].addEventListener("click", function (ev) { ev.stopPropagation(); });

/**
 * Which row is lit, and the drawer closing behind a choice.
 *
 * `Pages` tells this rather than the other way round: a page can also be
 * reached from the address bar or by Escape, and the menu has to agree with
 * where the app actually is, not with the last row that was pressed.
 */
export function markSidebarPage(name) {
    var rows = els.sidebar.querySelectorAll("[data-page-to]");
    for (var i = 0; i < rows.length; i++) {
        if (rows[i].getAttribute("data-page-to") === name) {
            rows[i].setAttribute("aria-current", "page");
        } else {
            rows[i].removeAttribute("aria-current");
        }
    }
    Sidebar.close();
}
