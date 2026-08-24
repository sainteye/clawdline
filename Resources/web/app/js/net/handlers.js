import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { Build } from "./build.js";
import { onSessions, render, renderConn, renderList } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { renderComposer } from "../view/composer.js";
import { Waits } from "../view/waits.js";
import { Start } from "../input/start.js";

export var handlers = {
    sessions: function (list, at) {
        var first = !S.arrived;
        S.sessions = list || [];
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
