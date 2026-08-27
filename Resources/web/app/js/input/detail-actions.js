import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { api } from "../net/api.js";
import { closingID, render, renderList, setClosingID } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { Optimistic, Waits } from "../view/waits.js";
import { authoritativeSendTime, optimisticSendSnapshot } from "../view/optimistic-data.js";
import { closeDetail, loadTranscript } from "../session/open.js";
import { closeAgent, openAgent } from "../session/agent.js";
import { ActionConfirm } from "./action-confirm.js";

els.filter.addEventListener("input", function () { S.filter = els.filter.value; renderList(); });
/**
 * Refresh, and be seen doing it.
 *
 * A local transcript comes back in a few milliseconds, which is faster than anybody can see: a
 * spinner that appears and vanishes inside three frames is not feedback, it is a flicker. So the
 * working state is held for a beat whatever the network does — long enough to have been a state
 * somebody watched — and then goes. Not disabled while it is out: on a phone a disabled chip in
 * this header is `display: none`, so disabling it would make the button somebody just pressed
 * disappear. The flag is what stops a second press, and the chip stays where it was put.
 */
var refreshing = false;
els["tx-refresh"].addEventListener("click", function () {
    if (!S.openId || refreshing) return;
    refreshing = true;
    var chip = els["tx-refresh"], started = Date.now(), HELD = 550;
    chip.dataset.busy = "on";
    function done() {
        setTimeout(function () {
            chip.dataset.busy = "off";
            refreshing = false;
        }, Math.max(0, HELD - (Date.now() - started)));
    }
    Promise.resolve(loadTranscript(S.openId, true)).then(done, done);
});
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
    if (!S.openId) return;
    api.focus(S.openId).then(function () { toast(T.webShowOnMacAsked); })
        .catch(function (e) { toast(e.message, true); });
});

els["detail-focus"].addEventListener("click", function () {
    if (!S.openId) return;
    SessionActions.toggle(els["detail-focus"]);
});

els["detail-actions-title"].addEventListener("click", function () {
    if (!S.openId) return;
    SessionActions.toggle(els["detail-actions-title"]);
});

function actionTriggerKey(ev) {
    if (ev.key !== "ArrowDown") return;
    ev.preventDefault(); ev.stopPropagation();
    SessionActions.open(ev.currentTarget);
    var first = SessionActions.items()[0];
    if (first) first.focus({ preventScroll: true });
}
els["detail-focus"].addEventListener("keydown", actionTriggerKey);
els["detail-actions-title"].addEventListener("keydown", actionTriggerKey);

/**
 * The project mark and its title are two handles for the same menu. Bringing the terminal
 * forward is a menu action of its own, so opening the menu never moves focus away from the
 * browser. Git is a read-only view; `commit` and `push` are ordinary prompts, while ending
 * uses the server's named two-step route so the assistant quits cleanly before its terminal tab
 * is closed.
 */
export var SessionActions = {
    opener: null,
    ticket: 0,
    settlingEnd: false,

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
        this.opener = opener || this.opener || els["detail-focus"];
        this.level("main", false);
        els["session-actions"].hidden = false;
        els["detail-focus"].setAttribute("aria-expanded", "true");
        els["detail-actions-title"].setAttribute("aria-expanded", "true");
    },

    close: function (restore) {
        if (els["session-actions"].hidden) return;
        els["session-actions"].hidden = true;
        this.level("main", false);
        els["detail-focus"].setAttribute("aria-expanded", "false");
        els["detail-actions-title"].setAttribute("aria-expanded", "false");
        if (restore && this.opener && document.contains(this.opener)) {
            this.opener.focus({ preventScroll: true });
        }
        this.opener = null;
    },

    toggle: function (opener) {
        if (els["session-actions"].hidden) this.open(opener); else this.close();
    },

    focusMac: function () {
        var id = S.openId;
        if (!id || !S.write) return;
        this.close();
        api.focus(id).then(function () { toast(T.webShowOnMacAsked); })
            .catch(function (e) { toast(e.message, true); });
    },

    prompt: function (action, sessionID) {
        var id = sessionID || S.openId;
        if (!id || !S.write) return;
        // The reader can switch sessions during the HTTP trip. Remember the target's transcript
        // before that happens, so an older identical command cannot claim this new local turn.
        var snapshot = optimisticSendSnapshot(
            S.tx.id === id ? S.tx.entries : [], Date.now() / 1000);
        this.close();
        api.send(id, action, []).then(function (answer) {
            Optimistic.add(id, action, 0, snapshot.known,
                authoritativeSendTime(answer, snapshot.startedAt));
            if (S.openId === id && !S.agent) {
                renderTranscript();
                loadTranscript(id, true);
            }
            toast(action + " ✓");
        })
            .catch(function (e) { toast(e.message, true); });
    },

    end: function (sessionID) {
        var id = sessionID || S.openId;
        // The answer matters to the confirmation sheet, which has already disabled both of its
        // buttons on the assumption that a request is on its way: `false` is the only thing that
        // tells it nothing is coming back, and that it has to let go of itself.
        if (!id || !S.write || closingID) return false;
        var self = this;
        var ticket = ++this.ticket;
        this.close();
        setClosingID(id);
        this.settlingEnd = false;
        Waits.end.start();
        render();
        ActionConfirm.sync();
        api.end(id).then(function () {
            self.finishEnd(id, ticket, true);
        }).catch(function (e) {
            self.finishEnd(id, ticket, false, e);
        });
        return true;
    },

    /** One ending, whichever answer arrives first. The stream can prove the row is gone before
     *  the POST returns; once either has answered, the other is only the tail of the same trip
     *  and must not clear or toast over whatever the reader did next. */
    finishEnd: function (id, ticket, ok, error) {
        if (closingID !== id || ticket !== this.ticket || this.settlingEnd) return;
        var self = this;
        this.settlingEnd = true;
        Waits.end.settle(function () {
            if (closingID !== id || ticket !== self.ticket) return;
            setClosingID(null);
            self.settlingEnd = false;
            self.ticket += 1;
            ActionConfirm.finish();
            if (ok && S.openId === id) closeDetail();
            else render();
            toast(ok ? T.webEndSession + " ✓" : ((error && error.message) || T.webRequestFailed), !ok);
        });
    },

    gone: function (id) {
        if (closingID === id) this.finishEnd(id, this.ticket, true);
    }
};
