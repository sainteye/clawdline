import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { S, storeBool, storedBool } from "../core/state.js";
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
 *
 * **There are two ways of drawing it, and the default is still the Mac's.** Every pane on that
 * Mac is 243 columns wide and a phone shows about fifty, so the faithful picture is five
 * screen-widths of sideways dragging — which is honest and, on a phone, close to unreadable. The
 * header carries a control that soft-wraps it instead, one element per screen row so each row
 * hangs its continuations under its own indent. That mode is a different picture and says so;
 * the default does not move, because a panel whose promise is fidelity cannot start by breaking
 * it. `Resources/web/app/css/detail.css` carries the same reasoning beside the rules.
 */
export var Terminal = (function () {
    var forId = null;
    var screen = null;
    var error = null;
    var loading = false;
    var ticket = 0;
    var keepalive = null;
    var poll = null;
    // Which of the two layouts this browser asked for. Browser-local, like the other reading
    // preferences, and read once at load: the Mac has no opinion about it and never sees it.
    var WRAP_KEY = "clawdline.screen-wrap";
    var wrapping = storedBool(WRAP_KEY, false);

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

    /**
     * One run of text at one colour, as HTML — the only place in this file that turns a capture
     * into markup, so that both modes below escape in the same order and there is one line to
     * read when somebody asks whether they do.
     *
     * `esc(css)` cannot fire today and is kept anyway: every value `style()` can produce is a
     * `var(--term-N)`, an `rgb()` of integers forced through `| 0`, or a literal from this file,
     * so no capture can reach it. It is the guard for the day somebody adds an SGR code whose
     * parameter reaches the declaration — and it is the one line here no test can hold to
     * account, because nothing this function can be given makes it matter.
     */
    function paintSegment(text, css) {
        var body = esc(text);
        if (!body) return "";
        return css ? '<span style="' + esc(css) + '">' + body + "</span>" : body;
    }

    function wrap(chunk, state) {
        return paintSegment(chunk.replace(CONTROL, ""), style(state));
    }

    /* ---- the other mode: the same capture, laid out for a phone -----------
       Measured on this Mac on 2026-09-06 across five live panes: every pane is 243 columns wide,
       every row is at most 243 columns, and `-J` joins nothing because Claude Code emits its own
       newlines on an alternate screen. `-J` does preserve the trailing padding, and that padding
       is most of what arrives — a mean row of 149.8 display columns against 62.3 once it is
       stripped. Once it is gone only 23 of 59 rows are wider than a phone, so wrapping turns 59
       rows into about 113 rather than into a four-fold wall of text.

       Nothing here changes what is captured. This is one client-side decision about how to lay
       243 columns out on a viewport that has fifty, taken only when somebody asks for it.
       -------------------------------------------------------------------- */

    // Twelve columns of hanging indent, and no more. In those same five panes every real leading
    // indent was 0, 2, 3, 4 or 9 columns — and two rows were right-aligned status lines whose
    // leading run was 203 and 238 columns. Twelve keeps every genuine indent exactly where the
    // Mac put it, and stops the right-aligned two from leaving a two-column reading channel.
    var INDENT_CAP = 12;

    // A row that is essentially a horizontal rule. Wrapped, one becomes five rows of dashes,
    // which is worse than the sideways scroll it replaced — so these keep `white-space: pre` and
    // are clipped at the edge instead.
    //
    // **A share of the row, not an exact match.** A long rule with a label buried in it is still
    // a rule: twenty box dashes, ` 3 lines `, twenty more scores 0.87 and counts, which an exact
    // match would have refused. `│ path │ 12 │` is a table row and must not count, and scores
    // 0.15. Across those five panes the two populations do not overlap at all — every real rule
    // scored 1.00 and every row that merely opens with a tree glyph scored 0.1 or less — so 0.8
    // has a wide margin on both sides of it and is not a number tuned against one screen.
    //
    // A **short** labelled rule goes the other way: `── 3 lines ──` is only 0.40 and wraps. That
    // is not a misclassification worth spending margin on — thirteen columns fit a phone whole,
    // so wrapping it and clipping it draw the same picture. Say it here rather than let the next
    // reader meet it as a surprise and move 0.8 to catch it; moving 0.8 down to 0.4 puts
    // `│ … │ … │` table rows on the wrong side, which is a picture somebody loses content to.
    //
    // Box drawing only, deliberately: a row of ASCII hyphens is a rule too, but so is `-- 3 --`
    // and so is a diff's `--- a/file`, and no share threshold separates those from each other.
    // Spelled out, like `CSI` above: a literal range of box-drawing bytes that an editor or a
    // copy mangled would leave a pattern that matches nothing, which reads exactly like a screen
    // that happened to have no rules on it.
    var BOX = /[\u2500-\u257f]/g;
    var RULE_SHARE = 0.8;
    // Below this a row is too short to be anything, whatever it is made of: a lone `│` is 1.00
    // box drawing and is a table's left edge, not a rule.
    //
    // **What the floor lets through, and why that is accepted.** Eight or more bare column
    // separators — `│ │ │ │ │ │ │ │ │`, a table row with nothing written in any cell — also score
    // 1.00 and are clipped as a rule rather than wrapped. That is the right picture: a row with no
    // content has nothing to hang under an indent, and clipping keeps it the same width as the
    // `├─┼─┤` above and below it, which wrapping would not. The floor is there to stop one or two
    // glyphs being called a rule, not to promise that everything above it carries text.
    var RULE_FLOOR = 8;

    /**
     * How many cells of the grid a string occupies.
     *
     * **Cells, not characters.** The indent below is written in `ch`, which is a cell, and East
     * Asian Wide and Fullwidth glyphs take two of them — so counting `.length` here would
     * under-indent every row of Chinese and be invisible in every row of English. The ranges are
     * the Wide and Fullwidth blocks of UAX #11 written out, because a browser has no width table
     * to ask. Ambiguous-width characters — box drawing among them — count as one, which is what
     * a terminal gives them.
     *
     * **This is not a general width function, and the difference is the whole of what makes it
     * safe.** Its only caller is `indentOf()`, which hands it a row's leading whitespace — and in
     * a grid tmux has already laid out that population is U+0020 and U+3000, both of which the
     * ranges below get right. Measured against the current table, a row of *content* would not
     * be: U+1F680–U+1F6FF (🚀), U+2705 (✅), U+274C (❌), U+2753, U+2757, U+1F7E0–U+1F7EB (🟢),
     * U+2B1B–U+2B1C, U+2B50 (⭐), U+2B55, U+231A–U+231B, U+26A1 and U+2728 are all EAW=Wide and
     * all come back as one cell; and U+200D, U+FE0F and combining marks come back as one where a
     * terminal gives them none, so `columns("👨‍👩‍👧")` is 8. None of that can be reached from
     * here. **The day somebody measures a whole row with this, those are the ranges to add** —
     * named now so that day costs an hour rather than a morning of bisecting an indent.
     */
    function wideAt(code) {
        return (code >= 0x1100 && code <= 0x115f) ||
            (code >= 0x2e80 && code <= 0x303e) ||
            (code >= 0x3041 && code <= 0x33ff) ||
            (code >= 0x3400 && code <= 0x4dbf) ||
            (code >= 0x4e00 && code <= 0x9fff) ||
            (code >= 0xa000 && code <= 0xa4cf) ||
            (code >= 0xa960 && code <= 0xa97f) ||
            (code >= 0xac00 && code <= 0xd7a3) ||
            (code >= 0xf900 && code <= 0xfaff) ||
            (code >= 0xfe10 && code <= 0xfe19) ||
            (code >= 0xfe30 && code <= 0xfe6f) ||
            (code >= 0xff00 && code <= 0xff60) ||
            (code >= 0xffe0 && code <= 0xffe6) ||
            (code >= 0x1f300 && code <= 0x1f64f) ||
            (code >= 0x1f900 && code <= 0x1f9ff) ||
            (code >= 0x20000 && code <= 0x3fffd);
    }

    function columns(text) {
        var total = 0;
        for (var i = 0; i < text.length; i += 1) {
            var code = text.charCodeAt(i);
            // A surrogate pair is one character on the grid and two units in this string, so read
            // past it. Not reading past it does not double a wide glyph — neither half is in any
            // range below, so each would score one and a CJK Extension B ideograph would still
            // come out two. What it breaks is every supplementary character that is *narrow*:
            // U+1D400 (𝐀) is one cell joined and two apart, which is why that is the character
            // the test uses and U+1F600 is not — U+1F600 is two either way and can see nothing.
            if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length) {
                var low = text.charCodeAt(i + 1);
                if (low >= 0xdc00 && low <= 0xdfff) {
                    code = (code - 0xd800) * 0x400 + (low - 0xdc00) + 0x10000;
                    i += 1;
                }
            }
            total += wideAt(code) ? 2 : 1;
        }
        return total;
    }

    /**
     * One walk of the capture, emitting one row per screen line.
     *
     * **The state object outlives the row, and that is the whole reason this is not `paint()`
     * called in a loop.** tmux opens a colour once and lets it run to wherever it is closed,
     * which is routinely several rows later; splitting the source on newlines first and painting
     * each row on its own would rebuild `state` at every boundary and lose the colour of every
     * row after the first. So the split happens inside the walk, and `state` is built once.
     *
     * Each row comes back as its runs — text and the CSS that was open over it — rather than as
     * markup, because the two things done to a row next, stripping its padding and measuring its
     * indent, are done to text and not to HTML. The escaping happens at the end, in the one place
     * both modes share.
     */
    function segments(text) {
        var source = String(text == null ? "" : text);
        if (!source) return [];
        var state = { fg: null, bg: null, bold: false, dim: false, italic: false,
                      underline: false, inverse: false };
        var all = [];
        var row = [];
        // Whether the text seen so far ended on a row boundary. `capture-pane` **terminates** the
        // last row rather than separating rows, so the final newline is punctuation and not a row
        // of the grid — `Sources/LiveScreen.swift` says the same thing where it counts `lines`,
        // and drops that newline before counting so a 25-line screen does not report 26.
        // Read from the stripped text rather than from `source`: a control byte after the newline
        // is dropped and must not make an already-terminated row look unfinished.
        var ended = false;
        function take(chunk) {
            var visible = chunk.replace(CONTROL, "");
            var parts = visible.split("\n");
            for (var i = 0; i < parts.length; i += 1) {
                if (i > 0) { all.push(row); row = []; }
                if (parts[i]) row.push({ text: parts[i], css: style(state) });
            }
            if (visible) ended = visible.charAt(visible.length - 1) === "\n";
        }
        var last = 0;
        var found;
        CSI.lastIndex = 0;
        while ((found = CSI.exec(source)) !== null) {
            if (found.index > last) take(source.slice(last, found.index));
            last = found.index + found[0].length;
            if (found[1] !== undefined) apply(state, found[1]);
            if (found[0].length === 0) CSI.lastIndex += 1;
        }
        if (last < source.length) take(source.slice(last));
        // A blank row **inside** the grid is a real row and keeps its height — that is what the
        // stylesheet's `min-height` is for. The only row refused here is the one the terminating
        // newline would have invented after the last real one.
        if (row.length || !ended) all.push(row);
        return all;
    }

    /**
     * The trailing padding, dropped.
     *
     * `capture-pane -J` preserves it and on this Mac it is about seventy per cent of what crosses
     * the wire. Wrapped, it would trail blank continuation rows behind every short line, which is
     * the exact thing this mode exists to remove.
     *
     * **What it costs, said here rather than left to be discovered:** a trailing run that carried
     * a background colour loses its block, so a highlight or a selection that ran to the right
     * margin now stops at the last visible character. That is a real difference from the picture
     * on the Mac. It is the price of this mode and not of the panel: the default keeps every
     * column, padding and background alike.
     */
    function unpad(row) {
        var out = row.slice();
        while (out.length) {
            var last = out[out.length - 1];
            var kept = last.text.replace(/\s+$/, "");
            if (kept === last.text) break;
            if (kept) { out[out.length - 1] = { text: kept, css: last.css }; break; }
            out.pop();
        }
        return out;
    }

    /**
     * How far in this row starts, in cells, capped.
     *
     * The leading whitespace and nothing more: a marker-aware indent — hanging the continuations
     * under the text rather than under the bullet — would need a taxonomy of markers this file
     * does not have, and a wrong one moves text that was already aligned.
     *
     * tmux hands over a grid it has already laid out, so the leading run is spaces; a tab would
     * be measured as one cell and render as up to eight, and the row would simply hang less far
     * than it should.
     */
    function indentOf(row) {
        var lead = "";
        for (var i = 0; i < row.length; i += 1) {
            // `\s` and not `[ \t]`: the ideographic space U+3000 is whitespace, is two cells
            // wide, and is how CJK text is indented — the one place where the difference between
            // counting characters and counting cells is not theoretical.
            var run = /^\s*/.exec(row[i].text)[0];
            lead += run;
            if (run.length < row[i].text.length) break;
        }
        return Math.min(INDENT_CAP, columns(lead));
    }

    function isRule(plain) {
        var solid = plain.replace(/\s+/g, "");
        if (solid.length < RULE_FLOOR) return false;
        var box = solid.match(BOX);
        return (box ? box.length : 0) / solid.length >= RULE_SHARE;
    }

    /**
     * A captured screen as one element per row, each hanging under its own indent.
     *
     * Escaped first and wrapped in spans afterwards, exactly as `paint()` does and for the same
     * reason — the content is chosen by whatever program somebody else is running. The only
     * attribute this writes that the capture can reach at all is the indent, and that is a
     * number this file computed.
     */
    function paintRows(text) {
        var all = segments(text);
        var out = "";
        for (var i = 0; i < all.length; i += 1) {
            var row = unpad(all[i]);
            var plain = "";
            var body = "";
            for (var j = 0; j < row.length; j += 1) {
                plain += row[j].text;
                body += paintSegment(row[j].text, row[j].css);
            }
            if (isRule(plain)) {
                out += '<div class="screen-row rule">' + body + "</div>";
                continue;
            }
            var indent = indentOf(row);
            out += indent > 0
                ? '<div class="screen-row" style="padding-left:' + indent +
                    "ch;text-indent:-" + indent + 'ch">' + body + "</div>"
                : '<div class="screen-row">' + body + "</div>";
        }
        return out;
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

    /** The control's own two states, so that what it says and what it looks like are one thing. */
    function paintToggle() {
        var button = els["screen-wrap"];
        if (!button) return;
        button.setAttribute("aria-pressed", wrapping ? "true" : "false");
        button.classList.toggle("on", wrapping);
    }

    function render() {
        if (!els["screen-body"]) return;
        els["screen-badge"].innerHTML = badge();
        paintToggle();
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
        els["screen-body"].innerHTML = wrapping
            ? '<div class="screen-text wrap">' + paintRows(screen.text) + "</div>"
            : '<pre class="screen-text">' + paint(screen.text) + "</pre>";
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
         * Lay the same capture out the other way.
         *
         * **It re-draws and does not re-fetch.** The screen is already in hand; this is a choice
         * about how 243 columns meet a viewport that has fifty, and asking the Mac for a capture
         * it has already sent would be a request made to answer a question about CSS.
         */
        wrap: function (on) {
            var next = on === undefined ? !wrapping : !!on;
            if (next !== wrapping) {
                wrapping = next;
                storeBool(WRAP_KEY, wrapping);
            }
            render();
        },

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
            return { forId: forId, screen: screen, error: error, wrapping: wrapping,
                     leasing: keepalive !== null, polling: poll !== null };
        },
        paintForTesting: paint,
        paintRowsForTesting: paintRows,
        columnsForTesting: columns
    };
})();
