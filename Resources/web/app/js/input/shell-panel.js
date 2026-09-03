import { esc } from "../core/esc.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { toast } from "../core/util.js";
import { ActionConfirm } from "./action-confirm.js";
import { byId } from "../view/derive.js";
import { GitPanel } from "./git-panel.js";
import { Terminal } from "../view/terminal.js";

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
    /**
     * The row the strip was showing, kept rather than read fresh each time.
     *
     * **A command that ends drops off the session's list**, so the answer stops carrying it — and
     * that is the exact moment somebody is looking at this panel, having just watched the thing
     * land or having just stopped it themselves. Reading it live meant the command line vanished
     * from the screen at the one moment it was most worth still being able to see.
     */
    var meta = null;

    /** How often a live command is re-read. Slower than the session list, which it is not. */
    var beat = 1500;

    /** What the session list is already saying about this command, if anything. */
    function stripRow(id) {
        var s = byId(S.openId);
        var list = (s && s.shells) || [];
        for (var i = 0; i < list.length; i++) if (list[i].id === id) return list[i];
        return null;
    }

    /**
     * What was asked for, and whether anything more is coming.
     *
     * **The command leads.** It is the one thing on this screen a reader can match against what
     * they remember asking for — the id is Claude Code's word for it, useful for `/bashes` on the
     * Mac and meaningless to somebody holding a phone. It is absent only when the two transcript
     * records it is joined from straddled a read, and then the id is all there is.
     */
    function cmdHTML() {
        var command = (meta || {}).command;
        return command ? '<p class="shell-cmd">' + esc(command) + "</p>" : "";
    }

    function saidHTML() {
        var shell = meta || {};
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
            if (data.shell) meta = data.shell;
            // Nothing left to stop, so nothing offering to. The panel stays — what it printed is
            // still the reason somebody is looking at it.
            els["shell-stop"].hidden = !S.write || !!data.ended;
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
            if (!error) {
                snapshot = { text: (snapshot && snapshot.text) || "", ended: true };
                els["shell-stop"].hidden = true;
            }
            stopBeat();
            render();
        });
    }

    function stopBeat() {
        if (!timer) return;
        clearInterval(timer);
        timer = null;
    }

    /**
     * Stop the command this panel is showing.
     *
     * **Behind the same two gates as ending a session**: the device has to be allowed to write,
     * and somebody has to press twice. It is the second thing on this page that destroys
     * something and it destroys the more ordinary thing — a build forty minutes in dies on a
     * mis-tap, and nothing brings it back.
     *
     * The sheet is given the command line itself, not the id. What somebody has to agree to is
     * "stop *this*", and nine random characters are not a description of anything.
     *
     * Nothing is redrawn on success. The Mac signals the process, Claude Code notices it end and
     * writes so, and the next beat of this panel reads that — the same path as a command that
     * finished on its own, which is what it now is.
     */
    function askToStop() {
        if (!forId || !shellId || !S.write) return;
        var shell = meta || {};
        ActionConfirm.open("shell-stop", forId, els["shell-stop"], {
            title: T.webShellStopTitle,
            say: fill(T.webShellStopSay, { command: shell.command || shellId }),
            go: function () {
                var sid = forId, id = shellId;
                return api.killShell(sid, id).then(function () {
                    toast(T.webShellStopped);
                    if (forId === sid && shellId === id) load(true);
                }).catch(function (e) {
                    toast(e.message || T.webShellStopFailed, true);
                });
            }
        });
    }

    return {
        open: function (id) {
            if (!S.openId || !id) return;
            GitPanel.close(false);
            Terminal.close(false);
            forId = S.openId;
            shellId = id;
            // The strip's own row, so the first draw already names the command rather than
            // waiting a round trip to find out what somebody just pressed.
            meta = stripRow(id);
            els["shell-panel"].hidden = false;
            els["pane-detail"].dataset.panel = "shell";
            // Offered only to a device that may write, like every other button that changes
            // something. A read-only phone gets the panel and no way to act on it, which is the
            // honest shape rather than a button that answers 401.
            els["shell-stop"].hidden = !S.write;
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
            drawn = false; meta = null;
            els["shell-panel"].hidden = true;
            delete els["pane-detail"].dataset.panel;
            if (restore && !els["detail-actions-trigger"].disabled) {
                els["detail-actions-trigger"].focus({ preventScroll: true });
            }
        },

        /// The second press before a command is stopped. Public because the button is bound out
        /// here, next to the other two.
        stop: askToStop,

        /// The reader moved to another session. What is on screen is about the one they left.
        follow: function () { this.close(false); }
    };
})();

els["shell-close"].addEventListener("click", function () { ShellPanel.close(true); });
els["shell-stop"].addEventListener("click", function () { ShellPanel.stop(); });
// The strip's row for a background command. One listener on the box rather than one per row: it
// repaints every time the command prints a line, and rebinding a button a second to do nothing
// new is work nobody would get back.
els.agents.addEventListener("click", function (ev) {
    var row = ev.target.closest ? ev.target.closest("[data-shell]") : null;
    if (row) ShellPanel.open(row.getAttribute("data-shell"));
});
els["shell-panel"].addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    ev.preventDefault(); ev.stopPropagation(); ShellPanel.close(true);
});
