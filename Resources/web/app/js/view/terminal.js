import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { SessionActions } from "../input/detail-actions.js";
import { GitPanel } from "../input/git-panel.js";
import { ShellPanel } from "../input/shell-panel.js";

/**
 * The terminal itself, as it is right now, in the transcript's space.
 *
 * **It is a mirror and it decides nothing.** `docs/screen-tail.md` is the record of the other
 * road — reconstructing the conversation out of the screen — and four of its five walls are
 * still walls, every one of them there because that feature had to decide which lines were
 * speech. This decides nothing: what tmux drew is what is drawn here, and a line it cannot
 * explain is a line it does not have to.
 *
 * **There is no scrollback and this must not imply there is.** Claude Code runs on the
 * alternate screen — measured `alternate_on=1`, `history_size=0` on every live pane — so what
 * arrives is the visible screen and nothing above it. The panel says how many lines it got and
 * offers no way to ask for more, because there is no more to ask for.
 *
 * **The header says which backend it is looking at, always.** On tmux a `pipe-pane` signal makes
 * this live within about 4 ms of the pane moving; on iTerm2 no such signal exists, so the same
 * panel is a sample taken when somebody asks, no faster than the server's floor. Those are very
 * different things and drawing them identically is a defect this repository already had.
 */
export var Terminal = (function () {
    var forId = null;
    var screen = null;
    var error = null;
    var loading = false;
    var ticket = 0;
    var keepalive = null;
    var poll = null;

    /* ---- SGR, and nothing but SGR ---------------------------------------
       `capture-pane -e` re-serialises a grid, so what comes back is text and colour and no
       cursor motion at all — the same boundary `Sources/Ansi.swift` draws for the Mac's own
       transcript view, and for the same reason: this is a text view of a grid tmux has already
       laid out, not a terminal emulator. Anything that is not an SGR sequence is dropped rather
       than rendered.
       -------------------------------------------------------------------- */

    // Two alternatives and their order matters: an SGR sequence is captured so its parameters
    // can be read, and every other escape sequence is matched only so that it can be thrown
    // away rather than printed. `\u001b` is spelled out because an editor or a copy that ate a
    // literal escape byte would leave a regex that silently matches nothing — which reads
    // exactly like a screen that happened to have no colour in it.
    var CSI = /\u001b\[([0-9;:]*)m|\u001b\[[0-9;:?]*[ -\/]*[@-~]|\u001b[@-Z\\-_]/g;
    // Control bytes that survived tmux's own serialisation are not content. A carriage return
    // in particular would make a line look complete and then be drawn on top of itself.
    var CONTROL = /[\u0000-\u0008\u000b-\u001f\u007f]/g;

    function colour(n) {
        // The sixteen are named against the stylesheet so the panel follows the page's theme;
        // 256-colour and true colour are computed, because a palette in CSS for those would be
        // 240 declarations nobody reads.
        if (n < 16) return "var(--term-" + n + ")";
        if (n < 232) {
            var c = n - 16;
            var steps = [0, 95, 135, 175, 215, 255];
            return "rgb(" + steps[Math.floor(c / 36) % 6] + "," +
                steps[Math.floor(c / 6) % 6] + "," + steps[c % 6] + ")";
        }
        var grey = 8 + (n - 232) * 10;
        return "rgb(" + grey + "," + grey + "," + grey + ")";
    }

    function style(state) {
        var css = [];
        var fg = state.inverse ? state.bg : state.fg;
        var bg = state.inverse ? state.fg : state.bg;
        if (state.inverse && !state.fg) bg = "var(--term-fg)";
        if (state.inverse && !state.bg) fg = "var(--term-bg)";
        if (fg) css.push("color:" + fg);
        if (bg) css.push("background:" + bg);
        if (state.bold) css.push("font-weight:600");
        if (state.dim) css.push("opacity:.6");
        if (state.italic) css.push("font-style:italic");
        if (state.underline) css.push("text-decoration:underline");
        return css.join(";");
    }

    function apply(state, params) {
        var codes = params === "" ? [0] : params.split(/[;:]/).map(function (p) {
            return p === "" ? 0 : parseInt(p, 10);
        });
        for (var i = 0; i < codes.length; i += 1) {
            var c = codes[i];
            if (c === 0) {
                state.fg = null; state.bg = null; state.bold = false; state.dim = false;
                state.italic = false; state.underline = false; state.inverse = false;
            } else if (c === 1) state.bold = true;
            else if (c === 2) state.dim = true;
            else if (c === 3) state.italic = true;
            else if (c === 4) state.underline = true;
            else if (c === 7) state.inverse = true;
            else if (c === 22) { state.bold = false; state.dim = false; }
            else if (c === 23) state.italic = false;
            else if (c === 24) state.underline = false;
            else if (c === 27) state.inverse = false;
            else if (c >= 30 && c <= 37) state.fg = colour(c - 30);
            else if (c >= 90 && c <= 97) state.fg = colour(c - 90 + 8);
            else if (c >= 40 && c <= 47) state.bg = colour(c - 40);
            else if (c >= 100 && c <= 107) state.bg = colour(c - 100 + 8);
            else if (c === 39) state.fg = null;
            else if (c === 49) state.bg = null;
            else if (c === 38 || c === 48) {
                var into = c === 38 ? "fg" : "bg";
                if (codes[i + 1] === 5) { state[into] = colour(codes[i + 2] | 0); i += 2; }
                else if (codes[i + 1] === 2) {
                    state[into] = "rgb(" + (codes[i + 2] | 0) + "," + (codes[i + 3] | 0) +
                        "," + (codes[i + 4] | 0) + ")";
                    i += 4;
                }
            }
        }
    }

    /**
     * A captured screen, as HTML. Escaped first and wrapped afterwards, so nothing a program can
     * draw becomes markup — the same order `words()` keeps and for the same reason.
     */
    function paint(text) {
        var source = String(text == null ? "" : text);
        var state = { fg: null, bg: null, bold: false, dim: false, italic: false,
                      underline: false, inverse: false };
        var out = "";
        var last = 0;
        var found;
        CSI.lastIndex = 0;
        while ((found = CSI.exec(source)) !== null) {
            if (found.index > last) out += wrap(source.slice(last, found.index), state);
            last = found.index + found[0].length;
            // Only the first alternative captures, so a defined group is the SGR case and
            // everything else falls through having been consumed and dropped.
            if (found[1] !== undefined) apply(state, found[1]);
            if (found[0].length === 0) CSI.lastIndex += 1;
        }
        if (last < source.length) out += wrap(source.slice(last), state);
        return out;
    }

    function wrap(chunk, state) {
        var body = esc(chunk.replace(CONTROL, ""));
        if (!body) return "";
        var css = style(state);
        return css ? '<span style="' + esc(css) + '">' + body + "</span>" : body;
    }

    /* ---- the panel ------------------------------------------------------- */

    /**
     * The one line that says what this panel is: which terminal it is reading, and whether that
     * terminal can tell it when something changes.
     */
    function badge() {
        if (!screen) return "";
        var backend = screen.backend === "tmux" ? "tmux" : "iTerm2";
        var live = screen.channel === "signalled";
        var word = live ? T.webScreenLive : T.webScreenOnDemand;
        var lines = typeof screen.lines === "number" ? " · " + screen.lines : "";
        return '<span class="screen-badge" data-channel="' + esc(screen.channel || "") + '">' +
            esc(backend) + " · " + esc(word) + esc(lines) + "</span>";
    }

    function render() {
        if (!els["screen-body"]) return;
        els["screen-badge"].innerHTML = badge();
        if (error) {
            els["screen-body"].innerHTML = '<div class="screen-note err" role="alert">' +
                esc(error) + "</div>";
            return;
        }
        if (!screen || screen.text == null) {
            els["screen-body"].innerHTML = '<div class="screen-note" role="status">' +
                esc(screen && screen.readable === false && screen.pending === false
                    ? T.webScreenGone : T.webLoading) + "</div>";
            return;
        }
        els["screen-body"].innerHTML = '<pre class="screen-text">' + paint(screen.text) + "</pre>";
    }

    /**
     * Ask for the screen, which is also how this page says it is still watching.
     *
     * **Reading is the subscription.** The server attaches its `pipe-pane` because somebody read,
     * and takes it off when nobody has read for thirty seconds — so the keepalive below is not a
     * poll for content, it is the lease. It runs at half the lease so one lost request does not
     * drop the pipe, and on tmux it costs the Mac nothing at all when the pane has not moved: the
     * answer comes out of a cache the signal invalidates.
     */
    function load() {
        var id = forId;
        if (!id) return;
        if (typeof api.screen !== "function") {
            loading = false;
            error = T.webScreenGone;
            render();
            return;
        }
        var mine = ++ticket;
        api.screen(id).then(function (data) {
            if (mine !== ticket || forId !== id) return;
            loading = false;
            error = null;
            screen = data.screen || null;
            render();
            arrange();
        }).catch(function (e) {
            if (mine !== ticket || forId !== id) return;
            loading = false;
            error = T.webScreenGone;
            render();
        });
    }

    /**
     * The two clocks this panel runs, and they are different things.
     *
     * The keepalive is the lease and never changes. The poll only exists on a backend that has no
     * change signal — iTerm2 — and it runs no faster than the interval the server itself named,
     * because that number is the Mac's, not this page's.
     */
    function arrange() {
        if (keepalive === null && forId) {
            keepalive = setInterval(function () { load(); }, 15000);
        }
        var wants = screen && screen.channel === "on-demand";
        if (wants && poll === null) {
            var after = Math.max(1000, Number(screen.askAgainAfterMs) || 1000);
            poll = setInterval(function () { load(); }, after);
        }
        if (!wants && poll !== null) { clearInterval(poll); poll = null; }
    }

    function stopClocks() {
        if (keepalive !== null) { clearInterval(keepalive); keepalive = null; }
        if (poll !== null) { clearInterval(poll); poll = null; }
    }

    return {
        open: function () {
            if (!S.openId) return;
            SessionActions.close();
            GitPanel.close(false);
            ShellPanel.close(false);
            forId = S.openId;
            screen = null; error = null; loading = true;
            els["screen-panel"].hidden = false;
            els["pane-detail"].dataset.panel = "screen";
            render();
            load();
            arrange();
            els["screen-close"].focus({ preventScroll: true });
        },

        close: function (restore) {
            if (!els["screen-panel"] || els["screen-panel"].hidden) return;
            ticket += 1;
            stopClocks();
            forId = null; screen = null; loading = false; error = null;
            els["screen-panel"].hidden = true;
            if (els["pane-detail"].dataset.panel === "screen") {
                delete els["pane-detail"].dataset.panel;
            }
            if (restore && !els["detail-actions-trigger"].disabled) {
                els["detail-actions-trigger"].focus({ preventScroll: true });
            }
        },

        refresh: function () { if (forId) load(); },
        follow: function () { this.close(false); },

        /**
         * The `screen` event said a pane moved to a new revision.
         *
         * Only the revision travels on the stream — the screen itself comes through the
         * authenticated GET, exactly as a transcript append does — so this is a comparison and a
         * fetch, and a revision this panel already holds is dropped without asking the Mac
         * anything. That is where the 21% of byte-identical captures the server measured would
         * otherwise have gone.
         */
        observe: function (id, revision) {
            if (!id || !revision || forId !== id) return;
            if (screen && screen.revision === revision) return;
            load();
        },

        /** For the tests: what this panel currently believes it is showing. */
        stateForTesting: function () {
            return { forId: forId, screen: screen, error: error,
                     leasing: keepalive !== null, polling: poll !== null };
        },
        paintForTesting: paint
    };
})();
