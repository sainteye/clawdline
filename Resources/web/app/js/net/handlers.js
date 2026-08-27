import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { Build } from "./build.js";
import { onSessions, render, renderConn, renderList } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { renderComposer } from "../view/composer.js";
import { Waits } from "../view/waits.js";
import { Start } from "../input/start.js";

/**
 * Whether accepting this frame would close the chat on the strength of one empty observation.
 * Non-empty replacements keep their existing semantics; the special case is deliberately only
 * the destructive transition from a currently open, previously present session to no rows.
 */
export function sessionListNeedsConfirmation(list, openId, previous, scan) {
    return (!scan || scan.emptyAuthoritative !== true) && Array.isArray(list) &&
        list.length === 0 && !!openId &&
        (previous || []).some(function (session) { return session && session.id === openId; });
}

export var handlers = {
    sessions: function (list, at, scan) {
        list = list || [];
        // The caller responds to `false` by asking for a newer scan. A same-generation REST echo
        // is the same observation, not confirmation; keeping the evidence gate here gives every
        // future transport the same last-known-good protection.
        if (sessionListNeedsConfirmation(list, S.openId, S.sessions, scan)) return false;
        var first = !S.arrived;
        S.sessions = list;
        S.at = at || 0;
        S.arrived = true;
        // Anything arriving at all is proof this browser is allowed to ask, whatever an earlier
        // request was told — a pairing finished in another tab counts.
        S.locked = false;
        // Draw the new state once. `settle(renderList)` used to run before `onSessions`, so every
        // ordinary stream frame drew the list here and then drew it again inside `onSessions`.
        // That second draw cancelled the FLIP which the first one had only just started. When a
        // skeleton is actually on screen, keep drawing it until its minimum lifetime is over and
        // let the settle callback replace it; otherwise this render is the replacement.
        var listWasWaiting = Waits.list.visible;
        onSessions();
        // Four things were held blank while nobody had said — the list, the pane beside it, that
        // pane's header and the status line under it. `onSessions` redraws all of them by opening
        // a session, but a first list with nothing to open opens none, and then nothing does.
        if (first && !S.openId) { render(); renderTranscript(); }
        Waits.list.settle(listWasWaiting ? renderList : null);
        return true;
    },
    /// The whole task list, every time one of them moves. Nothing here merges, for the same
    /// reason `sessions` does not: half an update is a class of bug this page does not have.
    /// A render only if the page has been built — the first list can arrive before `boot`.
    tasks: function (list) {
        S.tasks = list || [];
        if (els.rows) render();
    },
    hello: function (info) {
        if (!info) return;
        if (typeof info.write === "boolean") S.write = info.write;
        if (info.version) S.version = info.version;
        Build.saw(info);
        renderComposer();
        // The same switch decides whether a session can be started, and it can change under an
        // open sheet — this arrives again on every reconnect.
        Start.sync();
    },
    conn: function (state, seconds) {
        S.conn = state;
        S.retryIn = seconds || 0;
        // A connection that has stopped trying is no longer a wait; it is an answer, and one of
        // the empty states says it in words. A skeleton with nothing coming is worse than the
        // sentence it was standing in for, because it never admits that it has given up.
        if (state !== "connecting") Waits.list.settle(function () {
            // Nothing ever arrived and the wait has given up: what was held blank while an answer
            // might still have been coming *is* the answer now, and all four screens say it.
            if (!S.arrived && !S.openId) { render(); renderTranscript(); }
            else renderList();
        });
        renderConn();
    }
};
