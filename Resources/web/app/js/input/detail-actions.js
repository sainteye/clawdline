import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { toast, toastFailure } from "../core/util.js";
import { failureSentence } from "../core/failure-text.js";
import { api } from "../net/api.js";
import { closingID, closingKey, render, renderList, setClosingID } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { Optimistic, Waits } from "../view/waits.js";
import { authoritativeSendTime, optimisticSendSnapshot } from "../view/optimistic-data.js";
import { closeDetail, followPendingTranscript, loadTranscript } from "../session/open.js";
import { closeAgent, openAgent } from "../session/agent.js";
import { ActionConfirm } from "./action-confirm.js";
import { SessionSelection } from "../session/selection.js";

els.filter.addEventListener("input", function () { S.filter = els.filter.value; renderList(); });
els.back.addEventListener("click", function () { closeDetail(); });
// One listener on the box rather than one per row: the strip repaints every time an agent
// reaches for a tool, and rebinding half a dozen buttons a second to do nothing new is work
// nobody would get back.
els.agents.addEventListener("click", function (ev) {
    // The strip's other kind of row — a background command — is listened for by the panel that
    // opens it, next to everything else that panel owns. See `input/shell-panel.js`.
    var row = ev.target.closest ? ev.target.closest("[data-agent]") : null;
    if (!row) return;
    // An empty id is the root row: the way back to the session, which is what closing an agent
    // is. Clicking it while the session is already what the pane is showing does nothing.
    var id = row.getAttribute("data-agent");
    if (id) openAgent(id); else closeAgent();
});
els["agent-back"].addEventListener("click", function () { closeAgent(); });
els.keys.addEventListener("click", function () { els.keys.hidden = true; });
els.keys.querySelector(".sheet").addEventListener("click", function (ev) { ev.stopPropagation(); });
els.conn.addEventListener("click", function () { if (api.refresh) api.refresh(); });
els["tx-focus"].addEventListener("click", function () {
    var selected = SessionSelection.snapshot().open;
    if (!selected) return;
    var effect = SessionSelection.beginEffect("focus", { identity: selected });
    api.focus(selected.route).then(function () {
        if (SessionSelection.effectIsCurrent(effect)) toast(T.webShowOnMacAsked);
    }).catch(function (e) {
        if (SessionSelection.effectIsCurrent(effect)) toastFailure(e, T.webRequestFailed);
    }).then(function () { SessionSelection.finishEffect(effect); });
});

els["detail-actions-trigger"].addEventListener("click", function () {
    if (!S.openId) return;
    SessionActions.toggle(els["detail-actions-trigger"]);
});

function actionTriggerKey(ev) {
    if (ev.key !== "ArrowDown") return;
    ev.preventDefault(); ev.stopPropagation();
    SessionActions.open(ev.currentTarget);
    var first = SessionActions.items()[0];
    if (first) first.focus({ preventScroll: true });
}
els["detail-actions-trigger"].addEventListener("keydown", actionTriggerKey);

/**
 * The overflow button is the one handle for this menu. Bringing the terminal forward is a menu
 * action of its own, so opening the menu never moves focus away from the browser. Git is a
 * read-only view; `commit` and `push` are ordinary prompts, while ending uses the server's named
 * two-step route so the assistant quits cleanly before its terminal tab is closed.
 */
export var SessionActions = {
    opener: null,
    ticket: 0,
    settlingEnd: false,
    endEffect: null,
    endWasOpen: false,

    onGit: function () {
        return els["session-actions-git"].dataset.place === "current";
    },

    level: function (name, focus) {
        var git = name === "git";
        var main = els["session-actions-main"], child = els["session-actions-git"];
        main.dataset.place = git ? "left" : "current";
        child.dataset.place = git ? "current" : "right";
        main.setAttribute("aria-hidden", git ? "true" : "false");
        child.setAttribute("aria-hidden", git ? "false" : "true");
        main.toggleAttribute("inert", git);
        child.toggleAttribute("inert", !git);
        if (focus) {
            var first = this.items()[0];
            if (first) first.focus({ preventScroll: true });
        }
    },

    items: function () {
        var level = this.onGit() ? els["session-actions-git"] : els["session-actions-main"];
        return Array.prototype.slice.call(level.querySelectorAll("button:not(:disabled)"));
    },

    open: function (opener) {
        if (!S.openId) return;
        this.opener = opener || this.opener || els["detail-actions-trigger"];
        this.level("main", false);
        els["session-actions"].hidden = false;
        els["detail-actions-trigger"].setAttribute("aria-expanded", "true");
    },

    close: function (restore) {
        if (els["session-actions"].hidden) return;
        els["session-actions"].hidden = true;
        this.level("main", false);
        els["detail-actions-trigger"].setAttribute("aria-expanded", "false");
        if (restore && this.opener && document.contains(this.opener)) {
            this.opener.focus({ preventScroll: true });
        }
        this.opener = null;
    },

    toggle: function (opener) {
        if (els["session-actions"].hidden) this.open(opener); else this.close();
    },

    focusMac: function () {
        var selected = SessionSelection.snapshot().open;
        if (!selected || !S.write) return;
        this.close();
        var effect = SessionSelection.beginEffect("focus", { identity: selected });
        api.focus(selected.route).then(function () {
            if (SessionSelection.effectIsCurrent(effect)) toast(T.webShowOnMacAsked);
        }).catch(function (e) {
            if (SessionSelection.effectIsCurrent(effect)) toastFailure(e, T.webRequestFailed);
        }).then(function () { SessionSelection.finishEffect(effect); });
    },

    prompt: function (action, sessionID) {
        var selected = SessionSelection.resolve(sessionID || SessionSelection.snapshot().open,
            S.sessions);
        if (!selected || !S.write) return;
        var id = selected.identity.rowId;
        var key = selected.identity.key;
        // The reader can switch sessions during the HTTP trip. Remember the target's transcript
        // before that happens, so an older identical command cannot claim this new local turn.
        var snapshot = optimisticSendSnapshot(
            S.tx.id === id ? S.tx.entries : [], Date.now() / 1000);
        this.close();
        // A row action is allowed to settle after the reader opens another row. Its target must
        // still exist at the same exact route; it need not be the detail currently on screen.
        var effect = SessionSelection.beginEffect("prompt", {
            identity: selected.identity, requiresOpen: false
        });
        api.send(selected.identity.route, action, []).then(function (answer) {
            if (!SessionSelection.effectIsCurrent(effect)) return;
            Optimistic.add(key, action, 0, snapshot.known,
                authoritativeSendTime(answer, snapshot.startedAt),
                answer && answer.optimisticIdentity,
                answer && answer.optimisticRequest,
                answer && answer.optimistic_settlement);
            followPendingTranscript(key);
            if ((SessionSelection.snapshot().open || {}).key === key && !S.agent) {
                renderTranscript();
                loadTranscript(key, true);
            }
            toast(action + " ✓");
        }).catch(function (e) {
            if (SessionSelection.effectIsCurrent(effect)) toastFailure(e, T.webRequestFailed);
        }).then(function () { SessionSelection.finishEffect(effect); });
    },

    end: function (sessionID, acceptLoss, closeabilityVersion) {
        var selected = SessionSelection.resolve(sessionID || SessionSelection.snapshot().open,
            S.sessions);
        var id = selected && selected.identity.rowId;
        // The answer matters to the confirmation sheet, which has already disabled both of its
        // buttons on the assumption that a request is on its way: `false` is the only thing that
        // tells it nothing is coming back, and that it has to let go of itself.
        if (!id || !S.write || closingID) return false;
        var self = this;
        var ticket = ++this.ticket;
        this.close();
        setClosingID(id, selected.identity.key);
        this.endWasOpen = SessionSelection.matches(selected.identity,
            SessionSelection.snapshot().open);
        this.endEffect = SessionSelection.beginEffect("end", {
            identity: selected.identity, requiresOpen: false, allowMissing: true
        });
        this.settlingEnd = false;
        Waits.end.start();
        render();
        ActionConfirm.sync();
        api.end(selected.identity.route, acceptLoss, closeabilityVersion).then(function () {
            self.finishEnd(id, selected.identity.key, ticket, true);
        }).catch(function (e) {
            self.finishEnd(id, selected.identity.key, ticket, false, e);
        });
        return true;
    },

    /** One ending, whichever answer arrives first. The stream can prove the row is gone before
     *  the POST returns; once either has answered, the other is only the tail of the same trip
     *  and must not clear or toast over whatever the reader did next. */
    finishEnd: function (id, key, ticket, ok, error) {
        if (closingID !== id || closingKey !== key || ticket !== this.ticket || this.settlingEnd) return;
        var self = this;
        var effect = this.endEffect;
        this.settlingEnd = true;
        Waits.end.settle(function () {
            if (closingID !== id || closingKey !== key || ticket !== self.ticket) return;
            var current = SessionSelection.finishEffect(effect);
            setClosingID(null);
            self.settlingEnd = false;
            self.endEffect = null;
            self.ticket += 1;
            ActionConfirm.finish();
            if (!current) { self.endWasOpen = false; render(); return; }
            // The close gate answered with what the close would take — a list this page did
            // not show, from a fresher frame than its own. That is not a failure to toast; it
            // is the question, asked properly this time.
            if (!ok && error && error.code === "would_lose_work") {
                self.endWasOpen = false;
                render();
                ActionConfirm.reopenEndWithLost(effect.identity, error.lost);
                return;
            }
            var open = SessionSelection.snapshot().open;
            if (ok && self.endWasOpen &&
                (!open || SessionSelection.matches(effect.identity, open))) closeDetail();
            else render();
            self.endWasOpen = false;
            if (ok) toast(T.webEndSession + " ✓", false);
            else toastFailure(error, T.webRequestFailed);
        });
    },

    gone: function (id) {
        if (closingID === id) this.finishEnd(id, closingKey, this.ticket, true);
    }
};
