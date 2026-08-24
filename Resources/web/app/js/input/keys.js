import { phone } from "../core/env.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { freezeOrder, thawOrder } from "../view/derive.js";
import { render } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { closeDetail, openSession, toBottom } from "../session/open.js";
import { closeAgent, move, select } from "../session/agent.js";
import { ActionConfirm } from "./action-confirm.js";
import { Settings } from "./settings.js";
import { Start } from "./start.js";
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
        // a visible settings, start or keyboard card owns the next key until it closes.
        if (els.settings.hidden && els.start.hidden && els.keys.hidden) Info.open();
        return;
    }

    if (key === "Escape") {
        if (!els.info.hidden) { Info.close(); return; }
        if (!els.start.hidden) { Start.close(); return; }
        if (!els.settings.hidden) { Settings.close(); return; }
        if (!els.keys.hidden) { els.keys.hidden = true; return; }
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
    // A sheet is over the page, so `j` is not "move down the list behind it".
    if (!els.settings.hidden || !els.start.hidden) return;

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
