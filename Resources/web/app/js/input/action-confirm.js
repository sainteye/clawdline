import { phone } from "../core/env.js";
import { Diagnostics } from "../core/layout-diagnostics.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { confirmSpin, drawSpinner, setConfirmSpin, spinPhase } from "../core/pixels.js";
import { Build } from "../net/build.js";
import { byId, closeabilityLines, closeabilityVersion, lostIfClosed, projectSessionCloseability } from "../view/derive.js";
import { Waits } from "../view/waits.js";
import { closeDetail } from "../session/open.js";
import { closeAgent } from "../session/agent.js";
import { SessionActions } from "./detail-actions.js";
import { GitPanel } from "./git-panel.js";
import { Info } from "./info.js";

/** The second press before a session-changing action reaches the transport. */
export var ActionConfirm = {
    pending: null,
    busy: false,

    /**
     * `ask` is for a caller that owns its own destructive action.
     *
     * The two kinds this knew by name — ending a session, and a slash command — are the two it
     * also knows how to *carry out*. A background command being stopped is neither: the panel
     * showing it is the only thing that knows which command, and the request is its own. So it
     * hands over the two sentences and a function, and this stays what it is — the second press
     * before something irreversible reaches the transport.
     *
     * `ask.go` returns a promise. The sheet stays up, with both ways out disabled, until it
     * settles, for the same reason ending a session does: the decision should not disappear
     * before the Mac has answered it.
     */
    open: function (kind, sessionID, opener, ask) {
        var id = sessionID || S.openId;
        if (!id || !S.write) return;
        var action = kind === "end" ? T.webEndSession : kind;
        var returnFocus = opener || SessionActions.opener || els["detail-focus"];
        SessionActions.close();
        // `lost_if_closed`, at the only moment it can still change the outcome. Confirming a
        // sheet that showed the list is the acceptance the server's close gate asks for.
        var lost = kind === "end" ? lostIfClosed(id) : [];
        // The Deep Status half of the same press: not what the close would take, but why the
        // broker cannot yet say it is safe, and which one thing moves each of those.
        var why = kind === "end" ? closeabilityLines(byId(id)) : [];
        var closeableState = kind === "end"
            ? projectSessionCloseability(byId(id)).state : null;
        this.pending = { id: id, kind: kind, action: action, opener: returnFocus,
                         ask: ask || null, lost: lost, why: why,
                         closeability: closeableState,
                         closeabilityVersion: kind === "end"
                             ? closeabilityVersion(byId(id)) : null };
        this.busy = false;
        els["action-confirm-sheet"].dataset.kind = kind;
        els["action-confirm-title"].textContent = (ask && ask.title) ? ask.title
            : (kind === "end" ? T.webConfirmEndTitle
                              : fill(T.webConfirmActionTitle, { action: action }));
        els["action-confirm-say"].textContent = (ask && ask.say) ? ask.say
            : (kind === "end" ? this.endSay(lost, why, closeableState)
                              : fill(T.webConfirmActionSay, { action: action }));
        els["action-confirm"].hidden = false;
        this.sync();
        els["action-confirm-go"].focus({ preventScroll: true });
    },

    endSay: function (lost, why, closeability) {
        var said = T.webConfirmEndSay;
        if (lost && lost.length) {
            said += "\n" + T.webConfirmEndLoses + "\n" +
                lost.map(function (item) { return "· " + item; }).join("\n");
        }
        // Shown whenever the projection is not `safe`, including `unknown` — an absence of
        // proof is the thing the reader most needs to see before pressing a button that ends
        // somebody's work, and it is exactly what an empty list used to look like.
        if (closeability && closeability !== "safe") {
            said += "\n" + T.closeabilityNotProven;
            if (why && why.length) {
                said += "\n" + T.closeabilityWhy + ":\n" +
                    why.map(function (item) { return "· " + item; }).join("\n");
            }
        }
        return said;
    },

    /**
     * The server refused the close because it would take work the page did not show — a stale
     * frame, or a client that predates the gate. Its `lost` rows are the authoritative list, so
     * the sheet comes back up carrying them, and the next confirm is the informed acceptance.
     */
    reopenEndWithLost: function (id, rows) {
        var lost = (rows || []).map(function (row) {
            if (row.title) return row.title;
            if (row.waiters === 1) return T.sessionWaitedOnByOne;
            if (row.waiters) return fill(T.sessionWaitedOnByMany, { n: row.waiters });
            return row.task || row.wait || "";
        }).filter(Boolean);
        this.open("end", id);
        if (this.pending) this.pending.lost = lost.length ? lost : ["…"];
        els["action-confirm-say"].textContent = this.endSay(
            this.pending && this.pending.lost, this.pending && this.pending.why,
            this.pending && this.pending.closeability);
    },

    close: function (restore) {
        if (els["action-confirm"].hidden || this.busy) return;
        var opener = this.pending && this.pending.opener;
        this.pending = null;
        this.sync();
        els["action-confirm"].hidden = true;
        if (restore && opener && document.contains(opener)) {
            opener.focus({ preventScroll: true });
        }
    },

    run: function () {
        var pending = this.pending;
        if (!pending || this.busy) return;
        if (pending.ask && pending.ask.go) {
            var self = this;
            this.busy = true;
            this.sync();
            Promise.resolve(pending.ask.go()).then(function () { self.finish(); },
                                                   function () { self.finish(); });
            return;
        }
        if (pending.kind === "end") {
            // The decision stays on screen until the Mac has answered. Disabling both ways out
            // makes the one request the only action in flight; the spinner itself waits for the
            // shared 150ms threshold, so a genuinely fast close still looks instant.
            this.busy = true;
            this.sync();
            // A refusal — the write switch went off under this sheet, or a close is already in
            // flight — leaves nothing in flight to release these buttons, and both ways out of
            // the sheet are now disabled. So the sheet undoes itself rather than sitting there
            // spinning at somebody who cannot leave it.
            if (!SessionActions.end(pending.id, (pending.lost || []).length > 0,
                                    pending.closeabilityVersion)) {
                this.busy = false;
                this.sync();
                this.close(false);
            }
            return;
        }
        this.close(false);
        SessionActions.prompt(pending.action, pending.id);
    },

    sync: function () {
        setConfirmSpin(null);
        els["action-confirm-sheet"].setAttribute("aria-busy", this.busy ? "true" : "false");
        els["action-confirm-cancel"].disabled = this.busy;
        els["action-confirm-go"].disabled = this.busy;
        if (this.busy && Waits.end.visible) {
            els["action-confirm-go"].innerHTML = '<span class="busy"><canvas></canvas><span></span></span>';
            els["action-confirm-go"].querySelector(".busy span").textContent = T.webClosing;
            setConfirmSpin(els["action-confirm-go"].querySelector("canvas"));
            drawSpinner(confirmSpin, spinPhase);
        } else {
            els["action-confirm-go"].textContent = T.webConfirm;
        }
    },

    finish: function () {
        this.busy = false;
        this.sync();
        this.close(false);
    }
};

els["session-actions"].addEventListener("click", function (ev) {
    var action = ev.target.closest ? ev.target.closest("[data-action]") : null;
    if (action) { ActionConfirm.open(action.dataset.action); return; }
    if (ev.target.closest && ev.target.closest("#session-focus")) {
        SessionActions.focusMac(); return;
    }
    // A read, not an action: nothing is sent, so there is no confirmation to cross. The menu
    // closes and the card opens over the transcript.
    if (ev.target.closest && ev.target.closest("#session-info")) {
        SessionActions.close(); Info.open(); return;
    }
    if (ev.target.closest && ev.target.closest("#session-git-more")) {
        SessionActions.level("git", true); return;
    }
    if (ev.target.closest && ev.target.closest("#session-actions-back")) {
        SessionActions.level("main", false);
        els["session-git-more"].focus({ preventScroll: true });
        return;
    }
    if (ev.target.closest && ev.target.closest("#session-git")) {
        GitPanel.open(); return;
    }
    if (ev.target.closest && ev.target.closest("#session-end")) ActionConfirm.open("end");
});

els["git-refresh"].addEventListener("click", function () { GitPanel.refresh(); });
els["git-close"].addEventListener("click", function () { GitPanel.close(true); });
els["git-panel"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    ev.preventDefault(); ev.stopPropagation(); GitPanel.close(true);
});

els["action-confirm-cancel"].addEventListener("click", function () {
    ActionConfirm.close(true);
});
els["action-confirm-go"].addEventListener("click", function () { ActionConfirm.run(); });
els["action-confirm"].addEventListener("click", function () { ActionConfirm.close(true); });
els["action-confirm-sheet"].addEventListener("click", function (ev) { ev.stopPropagation(); });
els["action-confirm"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Tab") return;
    if (ActionConfirm.busy) { ev.preventDefault(); return; }
    var items = [els["action-confirm-cancel"], els["action-confirm-go"]];
    var at = items.indexOf(document.activeElement);
    if ((!ev.shiftKey && at === items.length - 1) || (ev.shiftKey && at <= 0)) {
        ev.preventDefault(); items[ev.shiftKey ? items.length - 1 : 0].focus();
    }
});

els["session-actions"].addEventListener("keydown", function (ev) {
    if ((ev.key === "ArrowLeft" || ev.key === "Escape") &&
        SessionActions.onGit()) {
        ev.preventDefault(); ev.stopPropagation();
        SessionActions.level("main", false);
        els["session-git-more"].focus({ preventScroll: true });
        return;
    }
    if (["ArrowDown", "ArrowUp", "Home", "End"].indexOf(ev.key) < 0) return;
    ev.preventDefault(); ev.stopPropagation();
    var items = SessionActions.items();
    if (!items.length) return;
    var at = items.indexOf(document.activeElement), next;
    if (ev.key === "Home") next = 0;
    else if (ev.key === "End") next = items.length - 1;
    else if (ev.key === "ArrowDown") next = (at + 1 + items.length) % items.length;
    else next = (at - 1 + items.length) % items.length;
    items[next].focus({ preventScroll: true });
});

document.addEventListener("pointerdown", function (ev) {
    if (els["session-actions"].hidden ||
        ev.target.closest(".detail-actions, #detail-actions-title")) return;
    SessionActions.close();
});

document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape" || els["session-actions"].hidden) return;
    if (SessionActions.onGit()) return;
    ev.preventDefault(); ev.stopPropagation(); SessionActions.close(true);
}, true);

// Back on a phone: the pushState above put us here, so popping means "list".
window.addEventListener("popstate", function () {
    Diagnostics.note("history.popstate.handler", {
        phone: phone(), agent: !!S.agent, open: !!S.openId, view: els.app.dataset.view
    });
    if (!phone()) return;
    // Innermost first, and only one step per gesture: the entry pushed when an agent was opened
    // is the one being popped, so it gives back the agent and leaves the session where it was.
    if (S.agent) { closeAgent(); return; }
    if (els.app.dataset.view === "detail") closeDetail();
});

els["stale-go"].addEventListener("click", function () { location.reload(); });
els["stale-shut"].addEventListener("click", function () { Build.hush(); });
