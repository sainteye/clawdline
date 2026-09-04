import { phone } from "../core/env.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { Pages } from "../core/pages.js";
import { freezeOrder, thawOrder } from "../view/derive.js";
import { render } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { closeDetail, openSession, toBottom } from "../session/open.js";
import { closeAgent, move, select } from "../session/agent.js";
import { ActionConfirm } from "./action-confirm.js";
import { Sidebar } from "./sidebar.js";
import { Settings } from "./settings.js";
import { Start } from "./start.js";
import { Command } from "./command.js";
import { Schedule } from "./schedule.js";
import { Info } from "./info.js";

/* ==========================================================================
   9. Input
   ========================================================================== */

function typing(el) {
    return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
}

document.addEventListener("keydown", function (ev) {
    var key = ev.key;
    var meta = ev.metaKey || ev.ctrlKey;

    // Nothing behind the door is reachable, so nothing behind it takes keys either — and the
    // door is not a dialog you dismiss. There is no page under it to go back to.
    if (!els.door.hidden) return;

    // A confirmation is a decision about one action, not another layer of the page. While it is
    // open, no list or pane shortcut behind it runs; Escape is the one way out from anywhere.
    if (!els["action-confirm"].hidden) {
        if (key === "Escape") {
            ev.preventDefault(); ActionConfirm.close(true);
        }
        return;
    }

    // The three that work from anywhere, including out of a text box, because they are how you
    // get out of the text box or into the session's details.
    if (meta && (key === "k" || key === "K")) {
        ev.preventDefault();
        els.rows.focus();
        if (!S.selectedId) move(1); else select(S.selectedId);
        return;
    }
    if (meta && (key === "j" || key === "J")) {
        ev.preventDefault();
        if (phone()) { els.app.dataset.view = els.app.dataset.view === "detail" ? "list" : "detail"; return; }
        S.paneOpen = !S.paneOpen;
        els.app.dataset.pane = S.paneOpen ? "on" : "off";
        return;
    }
    if (meta && (key === "i" || key === "I")) {
        ev.preventDefault();
        if (!els.info.hidden) { Info.close(); return; }
        // Do not stack one sheet over another. The shortcut remains global while composing, but
        // an open menu, or a visible settings, start or keyboard card, owns the next key until
        // it closes.
        if (els.sidebar.hidden && els.settings.hidden && els.start.hidden && els.keys.hidden) Info.open();
        return;
    }

    if (key === "Escape") {
        if (!els.info.hidden) { Info.close(); return; }
        if (!els.start.hidden) { Start.close(); return; }
        // `Command.close` no-ops on its own while a request is in flight — see the comment there
        // — so this needs no extra check: Escape either closes the sheet or does nothing.
        if (!els.command.hidden) { Command.close(); return; }
        // Same rule as `Command.close`, one line up: refused rather than skipped while the POST
        // that makes the schedule is in flight — see `Schedule.close`.
        if (!els["schedule-form"].hidden) { Schedule.close(); return; }
        // The menu is over whatever page you are on, so it goes before the page does: closing
        // both for one press would take somebody off Settings when all they wanted was the
        // drawer shut.
        if (!els.sidebar.hidden) { Sidebar.close(); return; }
        if (!els.keys.hidden) { els.keys.hidden = true; return; }
        /* Leaving a page, once, for every page there is. This was `els.settings.hidden` and a
           `Settings.close()` that is itself one line — `Pages.goHome()` — while the Usage page
           answered Escape from a second `keydown` listener of its own inside `view/usage.js`.
           **A `return` here ends this listener and nothing else**, so the drawer open over Usage
           and one press closed the drawer *and* left the page; the same three steps over Settings
           were right, which is what said the edge had not been connected rather than that the
           order was wrong. One chain, and the next page needs no line here at all.

           A dialog opened over a page answers its own Escape — the browser closes it — and going
           home as well would be the two-things-for-one-press this whole order exists to prevent.
           It is asked of the document rather than of `usage-detail` by name for the same reason
           the rest of this is one branch: the Projects page's dialog will be a different id. */
        if (Pages.current() !== Pages.home()) {
            if (document.querySelector("dialog[open]")) return;
            Pages.goHome();
            return;
        }
        if (document.activeElement === els.filter) {
            if (S.filter) { els.filter.value = ""; S.filter = ""; render(); }
            else { els.filter.blur(); els.rows.focus(); }
            return;
        }
        if (typing(document.activeElement)) { document.activeElement.blur(); return; }
        // An agent is a step inside a session, so Escape gives that step back before it gives
        // the session back. Anything else would close two things for one press.
        if (S.agent) { closeAgent(); return; }
        if (S.openId) { closeDetail(); return; }
        return;
    }

    if (typing(document.activeElement)) return;
    if (meta || ev.altKey) return;
    // A sheet is over the page, so `j` is not "move down the list behind it", and the menu is one
    // more thing that is over it.
    if (!els.sidebar.hidden || !els.settings.hidden || !els.start.hidden || !els.command.hidden
        || !els["schedule-form"].hidden) return;

    switch (key) {
        case "ArrowDown": case "j": ev.preventDefault(); move(1); break;
        case "ArrowUp": case "k": ev.preventDefault(); move(-1); break;
        case "Enter":
            if (S.selectedId) { ev.preventDefault(); openSession(S.selectedId); }
            break;
        case "/":
            ev.preventDefault();
            els.filter.focus();
            els.filter.select();
            break;
        case "g": ev.preventDefault(); els["tx-scroll"].scrollTop = 0; break;
        case "G": ev.preventDefault(); toBottom(); break;
        case "r": ev.preventDefault(); toggleOrder(); break;
        case "?": ev.preventDefault(); els.keys.hidden = !els.keys.hidden; break;
        default: break;
    }
});

/** Pressed in the settings sheet, or `r` from a keyboard. The sheet is redrawn as well as the
 *  transcript: the button that was pressed is a button that says which order it is now. */
export function toggleOrder() {
    S.newestFirst = !S.newestFirst;
    Settings.drawOrder();
    renderTranscript();
    els["tx-scroll"].scrollTop = 0;
}

els["list-scroll"].addEventListener("mouseenter", freezeOrder);
els["list-scroll"].addEventListener("mouseleave", thawOrder);
els["list-scroll"].addEventListener("touchstart", freezeOrder, { passive: true });
els["list-scroll"].addEventListener("touchend", function () { setTimeout(thawOrder, 1200); }, { passive: true });
