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
    var drawn = false;

    /** How often a live command is re-read. Slower than the session list, which it is not. */
    var beat = 1500;

    /**
     * What was asked for, and whether anything more is coming.
     *
     * **The command leads.** It is the one thing on this screen a reader can match against what
     * they remember asking for — the id is Claude Code's word for it, useful for `/bashes` on the
     * Mac and meaningless to somebody holding a phone. It is absent only when the two transcript
     * records it is joined from straddled a read, and then the id is all there is.
     */
    function cmdHTML() {
        var command = ((snapshot && snapshot.shell) || {}).command;
        return command ? '<p class="shell-cmd">' + esc(command) + "</p>" : "";
    }

    function saidHTML() {
        var shell = (snapshot && snapshot.shell) || {};
        var ended = snapshot && snapshot.ended;
        return '<p class="shell-said" data-ended="' + (ended ? "1" : "0") + '">' +
            '<span class="dot"></span>' +
            (shell.what ? "<span>" + esc(shell.what) + "</span>" : "") +
            '<span class="id">' + esc(shellId || "") + "</span>" +
            "<span>" + esc(ended ? T.webShellEnded : T.webShellRunning) + "</span></p>";
    }

    /**
     * Drawn, and left where a reader watching a log expects to be left.
     *
     * **The newest line is the one they came for.** A build log is read from the bottom, and a
     * panel that opens at line one of four hundred is a panel somebody has to scroll every time
     * it redraws. So: to the bottom on the first draw, and to the bottom on every draw after
     * that *unless* they have scrolled up — which is them reading something further back, and
     * yanking them away from it would be worse than being one screen behind.
     */
    function draw(html) {
        var box = els["shell-body"];
        var stick = !snapshot || !drawn ||
            box.scrollTop + box.clientHeight >= box.scrollHeight - 24;
        box.innerHTML = html;
        drawn = true;
        if (stick) box.scrollTop = box.scrollHeight;
    }

    function render() {
        if (loading && !snapshot) {
            draw('<div class="git-note" role="status">' + esc(T.webLoading) + "</div>");
            return;
        }
        if (error) {
            draw('<div class="git-note err" role="alert">' + esc(error) + "</div>");
            return;
        }
        var text = (snapshot && snapshot.text) || "";
        draw(cmdHTML() + saidHTML() + (text.trim()
            ? '<pre class="shell-out">' + esc(text) + "</pre>"
            : '<div class="git-note">' + esc(T.webShellQuiet) + "</div>"));
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
            if (!same) render();
            else {
                // Only the line that can change without the bytes changing: a command that ended
                // having printed nothing new. Redrawing the output would take the scroll with it.
                var line = els["shell-body"].querySelector(".shell-said");
                if (line) line.outerHTML = saidHTML();
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
            drawn = false;
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
