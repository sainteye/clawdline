import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { GitPanel } from "./git-panel.js";

/**
 * What one background command has printed, in the transcript's space.
 *
 * The strip says a command is still going and what its last line was; this is the rest of it.
 * **There is nothing else to show.** A background command has no conversation — it was given its
 * words when it was started and it is not listening for more — so what a reader can be handed is
 * the file it is appending to, from the end, which is the same thing `/bashes` reads on the Mac.
 *
 * **It re-reads itself while it is open, and that is the point of opening it.** Everything else
 * on this page waits to be told something moved; a command printing into a file moves nothing on
 * the event stream, so a panel over a live build that only drew once would be a screenshot. It
 * stops the moment the command ends, and stops on close whatever happened — a poller left running
 * behind a closed panel is a request a minute for a screen nobody is looking at.
 *
 * A ticket makes an answer for the previous command harmless if the reader moves on while it is
 * in flight, the same bargain ``GitPanel`` strikes.
 */
export var ShellPanel = (function () {
    var forId = null;          // the session
    var shellId = null;        // the command
    var snapshot = null;
    var loading = false;
    var error = null;
    var ticket = 0;
    var timer = null;

    /** How often a live command is re-read. Slower than the session list, which it is not. */
    var beat = 1500;

    function said(shell) {
        var ended = snapshot && snapshot.ended;
        return '<p class="shell-said" data-ended="' + (ended ? "1" : "0") + '">' +
            '<span class="dot"></span><span>' + esc(shellId || "") + "</span>" +
            "<span>" + esc(ended ? T.webShellEnded : T.webShellRunning) + "</span></p>";
    }

    function render() {
        if (loading && !snapshot) {
            els["shell-body"].innerHTML = '<div class="git-note" role="status">' +
                esc(T.webLoading) + "</div>";
            return;
        }
        if (error) {
            els["shell-body"].innerHTML = '<div class="git-note err" role="alert">' +
                esc(error) + "</div>";
            return;
        }
        var text = (snapshot && snapshot.text) || "";
        els["shell-body"].innerHTML = said() + (text.trim()
            ? '<pre class="shell-out">' + esc(text) + "</pre>"
            : '<div class="git-note">' + esc(T.webShellQuiet) + "</div>");
    }

    function load(quiet) {
        var sid = forId, id = shellId;
        if (!sid || !id) return;
        var mine = ++ticket;
        if (!quiet) { snapshot = null; error = null; loading = true; render(); }
        api.shell(sid, id).then(function (data) {
            if (mine !== ticket || forId !== sid || shellId !== id) return;
            loading = false;
            error = null;
            // A repaint only when the bytes moved. Redrawing an unchanged build log once a
            // second would throw away the reader's scroll position once a second with it.
            var same = snapshot && snapshot.signature && snapshot.signature === data.signature;
            snapshot = data;
            if (!same) render(); else if (els["shell-body"].querySelector(".shell-said")) {
                els["shell-body"].querySelector(".shell-said").outerHTML = said();
            }
            if (data.ended) stopBeat();
        }).catch(function (e) {
            if (mine !== ticket || forId !== sid || shellId !== id) return;
            loading = false;
            // A command that ended while this was open has its row taken off the strip, and the
            // route answers 404 for an id the session no longer lists. That is the ordinary end
            // of watching one, not a failure to report.
            error = e && e.code === "not_found" ? null : (e.message || T.webShellFailed);
            if (!error) { snapshot = { text: (snapshot && snapshot.text) || "", ended: true }; }
            stopBeat();
            render();
        });
    }

    function stopBeat() {
        if (!timer) return;
        clearInterval(timer);
        timer = null;
    }

    return {
        open: function (id) {
            if (!S.openId || !id) return;
            GitPanel.close(false);
            forId = S.openId;
            shellId = id;
            els["shell-panel"].hidden = false;
            els["pane-detail"].dataset.panel = "shell";
            load(false);
            stopBeat();
            timer = setInterval(function () { load(true); }, beat);
            els["shell-close"].focus({ preventScroll: true });
        },

        close: function (restore) {
            if (els["shell-panel"].hidden) return;
            ticket += 1;
            stopBeat();
            forId = null; shellId = null; snapshot = null; loading = false; error = null;
            els["shell-panel"].hidden = true;
            delete els["pane-detail"].dataset.panel;
            if (restore && !els["detail-focus"].disabled) {
                els["detail-focus"].focus({ preventScroll: true });
            }
        },

        /// The reader moved to another session. What is on screen is about the one they left.
        follow: function () { this.close(false); }
    };
})();

els["shell-close"].addEventListener("click", function () { ShellPanel.close(true); });
els["shell-panel"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    ev.preventDefault(); ev.stopPropagation(); ShellPanel.close(true);
});
