import { phone } from "../core/env.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { closingID, render, rowNodes } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { Optimistic, Waits } from "../view/waits.js";
import { closeAgent } from "./agent.js";
import { SessionActions } from "../input/detail-actions.js";
import { GitPanel } from "../input/git-panel.js";
import { ShellPanel } from "../input/shell-panel.js";
import { ActionConfirm } from "../input/action-confirm.js";
import { Info } from "../input/info.js";
import { Shots } from "../input/shots.js";
import { SkillPicker } from "../input/composer.js";

/* ==========================================================================
   8. Opening a session
   ========================================================================== */

// Only the newest request may paint the transcript. Opening a session and receiving its stream
// update can start two reads almost together; without a ticket, the older snapshot can arrive
// last and erase the final entry that the newer one had already drawn.
var transcriptTicket = 0;

export function loadTranscript(id, quiet) {
    var ticket = ++transcriptTicket;
    if (!quiet) {
        S.tx = { id: id, entries: [], signature: null, loading: true, error: null };
        // Only the loud kind waits visibly. A refetch behind a transcript that is already on
        // screen has nothing to stand in for — the reader is reading the last version of it.
        Waits.tx.start();
        renderTranscript();
    }
    // Returned, so a control that started this can wait for it — see the Refresh chip. Every
    // other caller ignores it, which is what a fire-and-forget refetch should look like.
    return api.transcript(id).then(function (d) {
        // A later request owns both the result and the visible wait. Settling an older request
        // here would take down the skeleton while the request that superseded it is still out.
        if (S.openId !== id || ticket !== transcriptTicket) return;
        var received = d.entries || [];
        // Reconcile before trusting the signature. The common first fetch after a send quite
        // correctly says the file is unchanged; that must preserve the echo, while an eventual
        // matching entry must retire it even if an older server reports a stale signature.
        var reconciled = Optimistic.reconcile(id, received);
        // The signature is the server's own answer to "is this the same transcript". Trusting it
        // is what keeps a refetch from throwing the reader's scroll position away every few seconds.
        if (d.signature && d.signature === S.tx.signature) {
            S.tx.loading = false;
            if (reconciled) S.tx.entries = received;
            Waits.tx.settle(renderTranscript);
            return;
        }
        var stick = atBottom();
        S.tx = { id: id, entries: received, signature: d.signature || null, loading: false, error: null };
        Waits.tx.settle(function () {
            renderTranscript();
            if (stick) toBottom();
        });
    }).catch(function (e) {
        if (S.openId !== id || ticket !== transcriptTicket) return;
        // A read that failed keeps whatever is already on screen. It is the same rule as the
        // skeleton above and it is here for the same reason — the reader is reading the last
        // version of it — except that this is the branch where it matters: the list refetches
        // roughly once a second while a session works, so one refused read used to empty the pane
        // somebody was mid-sentence in, with no gesture of theirs behind it.
        //
        // **Not only `busy`.** A dropped connection, a 500 and a refusal all leave the same thing
        // true: the last transcript that arrived is still the best answer there is, and throwing
        // it away buys nothing. Only a first load has nothing to keep, and that one still says so
        // with the whole pane.
        var held = S.tx.id === id ? S.tx.entries : [];
        S.tx = {
            id: id,
            entries: held,
            // Kept with them. The signature is the server's name for *these* entries, so holding
            // it is what lets the next read that comes back unchanged be believed; nulling it
            // would turn the recovery into a full replace and take the reader's scroll with it.
            signature: held.length ? S.tx.signature : null,
            loading: false,
            error: whyTranscript(e)
        };
        Waits.tx.settle(renderTranscript);
    });
}

/**
 * What went wrong, in this page's own words — the same shape as `whyIntents` beside the composer
 * and `why` on the info card.
 *
 * Only `offline` carries its own message through, because `jsonFetch` wrote that one here and it
 * is already translated. Everything else arrives from the Mac in English, and a server sentence
 * put in front of somebody reading Chinese is a fault report in a language they did not pick.
 *
 * With entries held it is drawn as a line above the transcript rather than instead of it — see
 * `renderTranscript`, which has had both branches all along and only ever reached the empty one.
 */
function whyTranscript(e) {
    if (e && e.code === "offline") return e.message;   // already this page's own sentence
    return T.webTranscriptFailed;
}

export function atBottom() {
    var el = els["tx-scroll"];
    return el.scrollTop + el.clientHeight >= el.scrollHeight - 40;
}
export function toBottom() {
    var el = els["tx-scroll"];
    el.scrollTop = el.scrollHeight;
}

export function openSession(id, keepFocus, forceRefresh) {
    var s = byId(id);
    if (!s || closingID === id) return;
    S.selectedId = id;
    if (S.openId !== id) {
        SessionActions.close();
        ActionConfirm.close();
        // An agent belongs to the session that sent it away. Carrying one over into the next
        // session would leave somebody reading one session's background work under another
        // session's name, which is the one thing this pane must never do.
        closeAgent(true);
        S.openId = id;
        // Which runs were open is where a reader had got to in that transcript, not a setting.
        // Fold keys come from content and so would not collide across sessions, but carrying
        // them over means arriving in a new transcript with something already open.
        S.expanded = {};
        // And a picture picked for one session is not a picture for the next one.
        Shots.clear();
        Info.follow();
        GitPanel.follow();
    ShellPanel.follow();
        loadTranscript(id, false);
    } else if (forceRefresh) loadTranscript(id, true);
    if (phone()) {
        els.app.dataset.view = "detail";
        // The phone's own back gesture should mean what it looks like it means.
        try { history.pushState({ view: "detail", id: id }, ""); } catch (e) { }
    } else if (!S.paneOpen) {
        S.paneOpen = true;
        els.app.dataset.pane = "on";
    }
    render();
    SkillPicker.changed();
    if (!keepFocus && !phone()) {
        var node = rowNodes[id];
        if (node) node.focus({ preventScroll: true });
    }
}

export function closeDetail(silent) {
    // The confirmation owns this session until its one-way request settles. In particular, a
    // phone back gesture must not uncover a writable-looking list while the same session is
    // still closing underneath it; the successful end clears `closingID` before coming here.
    if (closingID && S.openId === closingID) return;
    ActionConfirm.close();
    S.openId = null;
    S.agent = null;
    S.tx = { id: null, entries: [], signature: null, loading: false, error: null };
    S.expanded = {};
    Shots.clear();
    Info.follow();
    GitPanel.follow();
    ShellPanel.follow();
    SkillPicker.close();
    if (phone()) els.app.dataset.view = "list";
    renderTranscript();
    if (!silent) render();
}
