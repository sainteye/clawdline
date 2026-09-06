import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
// **The page's own escaping, not a copy of it.** Everything below that pins markup byte for byte
// is only worth the bytes if it is pinned against the function the browser actually runs — a stub
// that escapes four characters where `core/esc.js` escapes five would let a change to the fifth
// through without a word. This is the one import here that is production code rather than a double.
import { esc } from "../Resources/web/app/js/core/esc.js";

/*
 * The live screen panel, without a browser.
 *
 * Two things are being held to account here and they are different kinds of thing.
 *
 * **What is drawn.** `capture-pane -e` hands over a grid tmux has already laid out, so what
 * arrives is text and SGR and nothing else — the same boundary `Sources/Ansi.swift` draws for the
 * Mac's own view. Anything that is not an SGR sequence has to disappear rather than be printed,
 * and everything that survives has to be escaped before it becomes markup, because a terminal is
 * the one surface where the content is chosen by a program somebody else is running.
 *
 * **What is claimed.** The header says which backend this is reading and whether that backend can
 * tell it something changed. On tmux it can, and the panel is about four milliseconds behind the
 * pane; on iTerm2 nothing can, and the same panel is a sample. A page that drew those identically
 * would be making a promise the Mac has not made, so the two are asserted apart here.
 *
 * **One renderer, so one subject.** `paintRows()` soft-wraps the capture, one element per screen
 * row, each row hanging its continuations under its own indent — and there is nothing else. The
 * reasoning for that layout has not changed and neither has its price: the row's trailing padding
 * is dropped, so a background that ran to the right margin stops at the last visible character,
 * and a row that is essentially a horizontal rule is clipped rather than folded. Those are
 * asserted below as what the panel does, not as what one of two modes does.
 *
 * **And what this file stopped being able to prove.** It used to hold `paint()`, the unwrapped
 * renderer, byte for byte to the markup this panel emitted before wrapping existed — an equality
 * over more than 20,043 inputs, with a stated exception for a capture carrying an OSC 8. That
 * renderer has been deleted with the mode it drew, so the equality has no subject left: nothing
 * here compares this panel to the panel it used to be. Every safety property it reached through
 * `paint()` — the escaping order, the link allowlist, the OSC scanner, SGR across rows — is
 * asserted below through `paintRows()` instead, which is the only renderer there is to assert
 * anything against.
 */

const source = await readFile(
    new URL("../Resources/web/app/js/view/terminal.js", import.meta.url), "utf8");

/* ---- the smallest page this module can be evaluated against --------------- */

let checks = 0;
function equal(actual, expected, message) { assert.deepEqual(actual, expected, message); checks += 1; }
function ok(value, message) { assert.ok(value, message); checks += 1; }

function element() {
    const el = {
        innerHTML: "",
        hidden: true,
        dataset: {},
        attrs: {},
        classes: {},
        disabled: false,
        focused: 0,
        focus: function () { this.focused += 1; },
        setAttribute: function (name, value) { this.attrs[name] = value; }
    };
    el.classList = { toggle: function (name, on) { el.classes[name] = !!on; } };
    return el;
}

const els = {
    "screen-panel": element(),
    "screen-badge": element(),
    "screen-body": element(),
    "screen-close": element(),
    "pane-detail": element(),
    "detail-actions-trigger": element()
};

const T = {
    webScreenLive: "live",
    webScreenOnDemand: "on demand",
    webScreenGone: "That screen could not be read",
    webLoading: "Loading"
};

const S = { openId: null };
const asked = [];
let answer = null;
const api = {
    screen: function (id) {
        asked.push(id);
        return answer ? Promise.resolve(answer) : Promise.reject({ code: "not_found" });
    }
};

const timers = [];
function setIntervalStub(fn, ms) { timers.push({ fn: fn, ms: ms, live: true }); return timers.length - 1; }
function clearIntervalStub(handle) { if (timers[handle]) timers[handle].live = false; }
const liveTimers = function () { return timers.filter(function (t) { return t.live; }); };

const standalone = source
    .replace(/^import .*$/gm, "")
    .replace("export var Terminal", "globalThis.Terminal");

globalThis.esc = esc;
globalThis.T = T;
globalThis.S = S;
globalThis.els = els;
globalThis.api = api;
globalThis.SessionActions = { close: function () {} };
globalThis.GitPanel = { close: function () {} };
globalThis.ShellPanel = { close: function () {} };
globalThis.setInterval = setIntervalStub;
globalThis.clearInterval = clearIntervalStub;

(0, eval)(standalone);
const Terminal = globalThis.Terminal;
// The renderer, and the only one there is. Everything below is asserted through it, including
// the properties that used to be reached through the unwrapped mode because that call was
// shorter to write.
const rows = Terminal.paintRowsForTesting;
const columns = Terminal.columnsForTesting;

const E = "\u001b";
const settle = function () { return new Promise(function (r) { setTimeout(r, 0); }); };

/* ---- what is drawn -------------------------------------------------------- */

equal(rows("plain text"), '<div class="screen-row">plain text</div>',
    "text with no colour in it is left alone");
equal(rows(E + "[31mred" + E + "[39m after"),
    '<div class="screen-row"><span style="color:var(--term-1)">red</span> after</div>',
    "a colour opens a span and the reset closes it");
equal(rows("a" + E + "[1;32mb" + E + "[0mc"),
    '<div class="screen-row">a<span style="color:var(--term-2);font-weight:600">b</span>c</div>',
    "two parameters in one sequence both apply");
equal(rows(E + "[38;5;208mo" + E + "[m"),
    '<div class="screen-row"><span style="color:rgb(255,135,0)">o</span></div>',
    "a 256-colour index is computed rather than looked up");
equal(rows(E + "[38;2;12;34;56mo" + E + "[m"),
    '<div class="screen-row"><span style="color:rgb(12,34,56)">o</span></div>',
    "and true colour is taken as written");

// **The sequences that are not colour.** tmux does not emit them in a capture, but the panel is
// looking at whatever a program drew, and a cursor move printed as text would be visible garbage
// on somebody's screen.
equal(rows(E + "[?25lhidden" + E + "[?25h"), '<div class="screen-row">hidden</div>',
    "a cursor-visibility sequence is dropped");
equal(rows(E + "[2Jcleared"), '<div class="screen-row">cleared</div>', "and so is a clear");
equal(rows(E + "[Habc"), '<div class="screen-row">abc</div>',
    "and a cursor move with no parameters");

// **Escaped first, markup built afterwards.** The content of this panel is chosen by a program
// running on somebody's Mac; markup in it is not markup. The bare `<script>` and the apostrophe
// are asserted further down, beside the row element they arrive wrapped in; this is the one of
// the three that is only reachable with a colour open over it.
equal(rows(E + '[31m"><img src=x>' + E + "[39m"),
    '<div class="screen-row"><span style="color:var(--term-1)">' +
        "&quot;&gt;&lt;img src=x&gt;</span></div>",
    "nothing a program prints becomes markup, including inside a coloured run");

// A carriage return would make a line look complete and then be drawn on top of itself, so it
// does not survive; the newline beside it is a row boundary and does.
equal(rows("one\r\ntwo"),
    '<div class="screen-row">one</div><div class="screen-row">two</div>',
    "a carriage return does not survive the row it was drawn in, and the newline still ends it");

/* ---- OSC 8, which is what a status line's links are ----------------------
 *
 * Claude Code writes the links in its status line as OSC 8: `ESC ] 8 ; params ; URI ST`, then
 * the words a person reads, then `ESC ] 8 ; ; ST` to close. The scanner in this module used to
 * match ESC and **one** byte, so it ate `ESC ]` and printed `8;id=1q7561e;https://clawdline.com/`
 * as text — which is what this panel showed on a phone. An OSC string runs to BEL or to ST, and
 * `Sources/Ansi.swift` has read it that way for the Mac's own transcript view since it was
 * written; these are that boundary, restated on this side of the repository.
 *
 * **And the URL is chosen by whatever program somebody else is running.** `http` and `https`
 * become a real link. Every other scheme keeps its label and becomes nothing else: a phone
 * browser can do nothing with `file:///Users/…` or `x-github-client://…`, and a link that
 * silently fails is worse than the words it replaced.
 * ------------------------------------------------------------------------ */

const BEL = "\u0007";
const ST = E + "\\";
const OSC = function (body) { return E + "]" + body + ST; };
const A = function (url) {
    return '<a href="' + url + '" target="_blank" rel="noopener noreferrer">';
};
// A whole hyperlink as the status line sends one, id and all.
const linked = function (url, text) { return OSC("8;id=1q7561e;" + url) + text + OSC("8;;"); };

equal(rows(OSC("0;a window title") + "after"), '<div class="screen-row">after</div>',
    "an OSC string is consumed to its terminator, not from its second byte on");
equal(rows(E + "]0;a window title" + BEL + "after"), '<div class="screen-row">after</div>',
    "BEL ends one too, which is the older of the two spellings and the one xterm documents first");
// A capture that is nothing but an unterminated OSC is one blank row of the grid and no text at
// all, which is the point: what the string carried is consumed rather than printed.
equal(rows(E + "]0;this one never ends"), '<div class="screen-row"></div>',
    "and an OSC nobody terminated runs to the end of the capture, exactly as Ansi.swift's does");
equal(rows(E + "]0;a title with " + E + "[31m in it" + ST + "after"),
    '<div class="screen-row">after</div>',
    "an ESC inside the string is not a terminator unless a backslash follows it");

equal(rows(linked("https://clawdline.com/", "clawdline.com")),
    '<div class="screen-row">' + A("https://clawdline.com/") + "clawdline.com</a></div>",
    "an https link in the status line becomes a link, drawn inside its row");
// The defect this section exists to remove, asserted as the absence it is: the payload that
// scanner printed as words is nowhere in the output that replaced it.
ok(rows(linked("https://clawdline.com/", "clawdline.com")).indexOf("8;id=1q7561e;") < 0,
    "and the payload it printed as text is nowhere in what comes out");
equal(rows(linked("http://127.0.0.1:7717/", "localhost")),
    '<div class="screen-row">' + A("http://127.0.0.1:7717/") + "localhost</a></div>",
    "and so does http, which is what this Mac's own server speaks");
equal(rows(linked("HTTPS://CLAWDLINE.COM/", "shouty")),
    '<div class="screen-row">' + A("HTTPS://CLAWDLINE.COM/") + "shouty</a></div>",
    "the scheme is matched without regard to case, because a terminal is not required to be tidy");
equal(rows(linked("https://claude.ai/new?a=1&b=2#settings/usage", "5h 9%")),
    '<div class="screen-row">' + A("https://claude.ai/new?a=1&amp;b=2#settings/usage") +
        "5h 9%</a></div>",
    "and the ampersand of a real query string is escaped into the attribute rather than left to open an entity");

/* **Every other scheme is words.** Both of these are in the capture this was written against. */
equal(rows(linked("file:///Users/sainteye/code/clawdline/artifacts/backlog.html", "60 now20")),
    '<div class="screen-row">60 now20</div>',
    "a file: URL keeps its label and becomes nothing else, because a phone browser cannot open it");
equal(rows(linked("x-github-client://openRepo/https://github.com/sainteye/clawdline?branch=main",
    "main")), '<div class="screen-row">main</div>',
    "and neither can it open the scheme of an application installed on somebody else's Mac");
equal(rows(linked("mailto:nobody@example.com", "nobody")), '<div class="screen-row">nobody</div>',
    "mailto is refused by the same rule rather than by a list of what is dangerous");

/* **The allowlist is what makes this safe, and it is asserted as a wall rather than as a
   sample.** The URL in an OSC 8 is written by whatever program is running in that pane. */
[
    ["javascript:alert(1)", "the scheme that would run"],
    ["JaVaScRiPt:alert(1)", "the same one spelled to slip past a lowercase comparison"],
    ["data:text/html,<script>alert(1)</script>", "a document carried in its own URL"],
    ["//evil.example/x", "a scheme-relative URL, which has no scheme to allow"],
    ["/x/y", "and a path-relative one"],
    ['https://x/"><img src=y onerror=alert(1)>', "a URL written to end the attribute it is in"],
    ["https://x/'onmouseover='alert(1)", "and one written for the other quote"],
    ["  javascript:alert(1)", "one hiding its scheme behind leading space"],
    // **One rejected character each, because every fixture above carries several.** The scheme
    // test and the character class are two different walls, and a URL that trips both proves only
    // that one of them stands. Measured: with these two absent, deleting `"` from the class, and
    // deleting `<>` from it, each left the whole suite green — the class was being asserted by
    // inputs the scheme test had already refused. Both mutants stayed safe, because `esc()` is a
    // third wall behind this one, so what was missing was the proof and not the safety.
    ['https://example.com/a"b', "an allowed scheme carrying the one character that ends an attribute"],
    ["https://example.com/a<b", "and one carrying the character that starts a tag"]
].forEach(function (pair) {
    const drawn = rows(linked(pair[0], "label"));
    equal(drawn, '<div class="screen-row">label</div>',
        pair[1] + " draws its label and nothing else");
    ok(drawn.indexOf("<a") < 0 && drawn.indexOf("href") < 0,
        "and puts no anchor and no attribute on the page");
});

equal(rows(linked("https://clawdline.com/", "<img src=x onerror=alert(1)>")),
    '<div class="screen-row">' + A("https://clawdline.com/") +
        "&lt;img src=x onerror=alert(1)&gt;</a></div>",
    "the words inside a link are escaped exactly as the words outside one are");
// The order does not change because there is an anchor now: escape first, build markup
// afterwards, which is what `paintSegment()` has always done.
equal(rows(OSC("8;id=x;https://clawdline.com/") + "7d 63%" + E + "[31m(4d12h)" + E + "[39m" +
    OSC("8;;")),
    '<div class="screen-row">' + A("https://clawdline.com/") +
        '7d 63%<span style="color:var(--term-1)">(4d12h)</span></a></div>',
    "a link whose text changes colour half way through is one link, which is what the status line sends");

/* **Malformed, in each of the ways a capture can be.** None of them may become a broken tag. */
equal(rows(OSC("8;https://clawdline.com/") + "label" + OSC("8;;")),
    '<div class="screen-row">label</div>',
    "an OSC 8 with one semicolon is not a hyperlink and does not become one");
equal(rows(OSC("8;id=x;") + "label" + OSC("8;;")), '<div class="screen-row">label</div>',
    "an empty URL is a close, not a link to nowhere");
equal(rows(OSC("8;;") + "after"), '<div class="screen-row">after</div>',
    "and a close with nothing open is nothing at all");
equal(rows(E + "]8;id=x;https://clawdline.com/" + ST + "label"),
    '<div class="screen-row">' + A("https://clawdline.com/") + "label</a></div>",
    "a link the capture ends without closing is closed at the end of its row, not left open");
equal(rows(E + "]8;id=x;https://clawdline.com/"), '<div class="screen-row"></div>',
    "and an OSC 8 that never terminates takes the rest of the capture with it, anchor and all");
// **A link whose text is only control bytes is not a link.** The shape that reaches this is not
// the obvious one: an OSC 8 opened and closed with *nothing* between them never produces a run at
// all, so what is needed is a run that has bytes and loses them — a carriage return between the
// two halves, which is what a status line redrawing a field in place writes.
//
// **Two things hold it and they are in different functions**, which is worth saying because
// neither can be removed by mutating the other: `segments()` never pushes a part that stripping
// left empty, and `paintRows()` skips a piece that escaping left empty before it decides whether
// to open an anchor. Either alone puts no empty anchor on the page.
equal(rows(OSC("8;id=x;https://clawdline.com/") + "\r" + OSC("8;;") + "after"),
    '<div class="screen-row">after</div>',
    "a link whose whole text was a control byte puts no empty anchor on the page");

/* ---- the layout, which is the only one -----------------------------------
 *
 * Every tmux pane on this Mac is 243 columns wide and a phone shows about fifty of them, so the
 * Mac's own picture, unaltered, is five screen-widths of sideways dragging. What this panel draws
 * instead is that capture soft-wrapped, one element per screen row so each row can hang its
 * continuations under its own indent.
 *
 * **It is a different picture from the Mac's, and that is the point.** What is asserted here is
 * that it is the right one: the indent measured in display columns, the trailing padding dropped,
 * the rule clipped rather than folded, the terminating newline that is not a row. What is not
 * asserted anywhere any more is that some other picture is available, because there is no other
 * picture.
 * -------------------------------------------------------------------------- */

const spaces = function (n) { return new Array(n + 1).join(" "); };
const styleOf = function (html) {
    const found = /style="([^"]*)"/.exec(html);
    return found ? found[1] : "";
};

/* **Display columns, not characters.** The indent below is written in `ch` units, which are
   cells of the grid, and a Chinese character occupies two of them. Counting `.length` here is a
   defect that is invisible in English and constant in the content this reader actually has. */
equal(columns("ab"), 2, "two Latin letters are two cells");
equal(columns("你好"), 4, "and two Chinese characters are four, not two");
equal(columns("　"), 2, "the ideographic space is two cells wide, which is how CJK text indents");
equal(columns("ｆｕｌｌ"), 8, "fullwidth Latin counts as fullwidth");
equal(columns("é"), 1, "an accented Latin letter is still one");
equal(columns("─"), 1,
    "and box drawing is one, because that is what a terminal gives it");
equal(columns("😀"), 2, "a surrogate pair is one character and two cells, not two of either");
equal(columns("あ"), 2, "kana is wide, which is the half of CJK that is not Han");
equal(columns("\u33ff"), 2, "and so is the top of the CJK compatibility block, where that range ends");
// **The surrogate pair is read past, and this is the assertion that can tell.** `columns("\u{1F600}")`
// is 2 whether the two units are joined into one wide character or counted as one cell each, so it
// cannot see the branch at all. U+1D400 is supplementary and *narrow*: joined it is one cell,
// unjoined it is two. Nothing else in this file distinguishes them.
equal(columns("\u{1D400}"), 1,
    "a supplementary character that is not wide is one cell, which is only true if the pair was joined");

/* **The hanging indent, in those columns.** A row indented with two ideographic spaces hangs at
   four, and a test that passes at two is a test measuring the wrong thing. */
equal(styleOf(rows("　　它從這裡開始")),
    "padding-left:4ch;text-indent:-4ch",
    "the indent is measured in cells, so two ideographic spaces hang the row at four");
equal(styleOf(rows("  ok")), "padding-left:2ch;text-indent:-2ch",
    "and two ordinary spaces at two");
equal(styleOf(rows("flush left")), "",
    "a row with no indent is given no indent to undo");
// **Only the leading run counts, and the walk stops at the first segment that has content.** A
// coloured row arrives as several segments, and the spaces *between* the colours are not indent:
// `["  ", "ok"(red), " and"]` starts two cells in, not three.
equal(styleOf(rows("  " + E + "[31mok" + E + "[39m and")),
    "padding-left:2ch;text-indent:-2ch",
    "the spaces between two coloured runs are not part of the indent");

/* **The cap.** Measured across five live panes on 2026-09-06 every real leading indent was 0, 2,
   3, 4 or 9 columns — and two rows were right-aligned status lines whose leading run was 203 and
   238. Twelve keeps every real indent and refuses the two that would leave a two-column channel. */
equal(styleOf(rows(spaces(11) + "x")), "padding-left:11ch;text-indent:-11ch",
    "eleven columns of indent is eleven");
equal(styleOf(rows(spaces(12) + "x")), "padding-left:12ch;text-indent:-12ch",
    "twelve, the cap itself, is still twelve");
equal(styleOf(rows(spaces(13) + "x")), "padding-left:12ch;text-indent:-12ch",
    "and thirteen is twelve, because past here the reading channel is worth more than the alignment");
equal(styleOf(rows(spaces(238) + "x")), "padding-left:12ch;text-indent:-12ch",
    "which is what a right-aligned status line does instead of taking the whole width");

/* **The state object outlives the row.** tmux opens a colour once and lets it run to wherever it
   is closed, routinely several rows later. Splitting the capture first and painting each row on
   its own would rebuild the state at every newline and lose the colour of every row but the
   first — so the split happens inside one walk. */
const carried = rows(E + "[31mred\nstill red" + E + "[39m");
equal(carried.split("var(--term-1)").length - 1, 2,
    "a colour opened on one row is still open on the next");
equal(carried.split('class="screen-row"').length - 1, 2, "and there are two rows to be open on");
const closed = rows(E + "[31mred" + E + "[39m\nplain");
equal(closed.split("var(--term-1)").length - 1, 1,
    "a colour closed on its own row does not leak into the one after it");

/* **A rule is not a paragraph.** A row that is essentially a horizontal rule becomes five rows of
   dashes if it wraps, which is worse than the sideways scroll it replaced — so it keeps
   `white-space: pre` and is clipped. The test is a share of box-drawing characters rather than an
   exact match, because `-- 3 lines --` drawn in box characters is still a rule. */
ok(rows(new Array(41).join("─")).indexOf('class="screen-row rule"') >= 0,
    "a row of forty box-drawing dashes is a rule and is not wrapped");
ok(rows("┌" + new Array(17).join("─") + "┬" + new Array(12).join("─") + "┐")
    .indexOf("rule") >= 0, "and so is the top of a table");
ok(rows("│ Resources/web/app/js/view/terminal.js │ 412 │").indexOf("rule") < 0,
    "a table row with content in it is content and wraps like content");
ok(rows("├ 寫除權息填息那篇").indexOf("rule") < 0,
    "and so is a line that merely starts with a tree glyph");
ok(rows(new Array(9).join("─") + "ab").indexOf("rule") >= 0,
    "eight box characters in ten is over the line");
ok(rows(new Array(8).join("─") + "abc").indexOf("rule") < 0,
    "seven in ten is under it");
ok(rows("──").indexOf("rule") < 0,
    "and two characters are too few to be anything, whatever they are made of");

/* **Trailing padding.** `capture-pane -J` preserves it, and on this Mac it is seventy per cent of
   what crosses the wire: a mean row of 149.8 columns against 62.3 once it is gone. Wrapped, every
   short line would trail blank continuation rows behind it. */
equal(rows("text" + spaces(200)), '<div class="screen-row">text</div>',
    "the padding a row was drawn with is not wrapped");
// The leading run stays in the row and is not stripped with the trailing one: that is what the
// negative `text-indent` is undoing. The first line starts at zero and draws its own two spaces;
// every line after it starts at two, under the text rather than under the margin.
equal(rows("  two" + spaces(200)),
    '<div class="screen-row" style="padding-left:2ch;text-indent:-2ch">  two</div>',
    "stripping the padding does not touch the indent, which is the other end of the row");

/* Blank rows are rows: a grid has 59 of them whether or not anything was written on them. */
equal(rows("a\n\nb").split('class="screen-row"').length - 1, 3,
    "an empty grid row is still a row");
// **But the newline a capture ends with is a terminator and not a row.** `Sources/LiveScreen.swift`
// says so where it counts `lines` — "Counting it would report 26 lines for a 25-line screen" — and
// every capture measured on this Mac ends with one. Drawing it would put a permanent blank row
// under every screen, and only in this mode: `<pre>a\n</pre>` is one line high, a trailing
// `.screen-row` is two.
equal(rows("a\n").split('class="screen-row"').length - 1, 1,
    "the newline a capture ends with is a terminator, not a row");
equal(rows("a\n\n").split('class="screen-row"').length - 1, 2,
    "and the blank row before that terminator is still a row");
// Control bytes are not content in this mode either — the reason `CONTROL` exists is that a
// carriage return would make a line look complete and then be drawn on top of itself.
equal(rows("a\rb"), '<div class="screen-row">ab</div>',
    "a carriage return does not survive into a wrapped row");
// And the two rules meet here: the terminator is read from the text *after* the control bytes are
// gone, so a stray byte behind it cannot make an already-finished row look unfinished.
equal(rows("a\n\r").split('class="screen-row"').length - 1, 1,
    "a control byte after the terminator does not resurrect the row it terminated");
// **And that one cannot see the guard it looks like it is testing.** `\r` arrives in the same
// chunk as `a\n`, so the text `segments()` measures is `a\n` and non-empty either way; the
// `if (visible)` in front of `ended` never runs on it. The guard only decides anything when a
// whole chunk is nothing but control bytes, and a chunk boundary needs an SGR sequence to exist
// — so the colour reset below is not decoration, it is the only thing that splits them. Measured
// against the guard removed: this is 2 rows and the line above is still 1.
equal(rows("a\n" + E + "[0m\r").split('class="screen-row"').length - 1, 1,
    "a control byte in a chunk of its own does not resurrect it either");
equal(rows(""), "", "and a capture with nothing in it is nothing at all");
equal(rows(null), "", "including one that never arrived");

/* **Escaped first, wrapped in spans afterwards — the order does not change because the mode
   does.** What this panel draws is chosen by whatever program somebody else is running. */
equal(rows("<script>alert(1)</script>"),
    '<div class="screen-row">&lt;script&gt;alert(1)&lt;/script&gt;</div>',
    "nothing a program prints becomes markup, and the row element is not a way around that");
equal(rows("it's"), '<div class="screen-row">it&#39;s</div>',
    "and the apostrophe is escaped by the page's own esc, not by a copy of it");
const hostile = rows('</div><img src=x onerror=alert(1)><span style="color:red">');
ok(hostile.indexOf("<img") < 0, "not a tag");
ok(hostile.indexOf("&lt;/div&gt;") > 0, "and not a closing tag for the row it is inside");
ok(hostile.indexOf('style="color:red"') < 0 && hostile.indexOf("&quot;color:red&quot;") > 0,
    "and the quotes that would have opened an attribute on it are escaped into the text");
ok(/^<div class="screen-row">[^<]*<\/div>$/.test(hostile),
    "what comes out is one row element and text, and nothing else");
// The one attribute this writes that the capture can reach at all is the indent, and it is a
// number this file computed rather than anything the capture said.
ok(/^style="padding-left:\d+ch;text-indent:-\d+ch"$/.test(
    'style="' + styleOf(rows('  "><b>x')) + '"'),
    "the indent is arithmetic, so there is nothing in it to escape out of");

/* **A link and the row boundary it may not cross.** Rows are elements rather than newlines, so
   an anchor left open across one would be closed by the wrong `</div>` and repaired by the
   browser in whatever way it liked. Each row opens its own and closes it. That the link is drawn
   at all, and that the allowlist decides which ones are, is asserted in the OSC section above.
   */
const across = rows(OSC("8;id=x;https://clawdline.com/") + "first\nsecond" + OSC("8;;"));
equal(across,
    '<div class="screen-row">' + A("https://clawdline.com/") + "first</a></div>" +
    '<div class="screen-row">' + A("https://clawdline.com/") + "second</a></div>",
    "a hyperlink whose text contains a newline is one anchor per row, each closed inside its own");
ok(across.indexOf("</a></div>") > 0 && across.indexOf("</div></a>") < 0,
    "so a row is never closed from inside a link");
// The trailing padding is stripped from the segment the link is *in*, so the segment that comes
// back has to be the same segment: rebuilt without its `href` this row would draw no anchor.
equal(rows(OSC("8;id=x;https://clawdline.com/") + "x" + spaces(50) + OSC("8;;")),
    '<div class="screen-row">' + A("https://clawdline.com/") + "x</a></div>",
    "padding stripped from inside a link leaves the link it was stripped from");

/* ---- what is claimed ------------------------------------------------------ */

S.openId = "%1";
answer = {
    screen: {
        id: "%1", backend: "tmux", channel: "signalled", revision: "abc",
        readable: true, pending: false, text: "hello" + E + "[31m!" + E + "[39m", lines: 25
    }
};
Terminal.open();
await settle();

equal(asked, ["%1"], "opening the panel is what tells the Mac somebody is watching");
equal(els["screen-panel"].hidden, false, "and the panel is up");
equal(els["pane-detail"].dataset.panel, "screen", "holding the transcript's place");
ok(els["screen-badge"].innerHTML.indexOf("tmux") >= 0, "the header names the backend");
ok(els["screen-badge"].innerHTML.indexOf("live") >= 0,
    "and says this one can be told when the screen changes");
ok(els["screen-badge"].innerHTML.indexOf("25") >= 0,
    "with how many lines actually came back");
ok(els["screen-body"].innerHTML.indexOf('<div class="screen-text">') === 0,
    "the panel draws the capture in the one layout it has");
ok(els["screen-body"].innerHTML.indexOf("var(--term-1)") > 0, "with its colour");
// **Pinned as a literal rather than compared with the function that produced it**, so this says
// what the panel puts on the page and not merely that two calls agree. There is no second
// rendering to hold it against any more: the equality with the pre-wrapping markup went with
// `paint()`, and nothing here replaces it.
equal(els["screen-body"].innerHTML,
    '<div class="screen-text"><div class="screen-row">hello' +
        '<span style="color:var(--term-1)">!</span></div></div>',
    "and nothing else at all");

equal(liveTimers().length, 1, "a signalled screen runs one clock, and it is the lease");
equal(liveTimers()[0].ms, 15000, "at half the Mac's thirty-second lease, so one lost ask is safe");

// **The stream carries a revision and not a screen.** A revision the panel already holds is a
// comparison and nothing else — which is where the 21% of byte-identical captures would have gone
// if the Mac had not already dropped them.
Terminal.observe("%1", "abc");
await settle();
equal(asked.length, 1, "a revision already on screen asks the Mac for nothing");
Terminal.observe("%2", "zzz");
await settle();
equal(asked.length, 1, "and neither does one for a session this panel is not showing");
answer = { screen: Object.assign({}, answer.screen, { revision: "def", text: "moved" }) };
Terminal.observe("%1", "def");
await settle();
equal(asked.length, 2, "a revision it does not have is fetched once");
equal(els["screen-body"].innerHTML,
    '<div class="screen-text"><div class="screen-row">moved</div></div>',
    "and replaces what was drawn");

Terminal.close(true);
equal(els["screen-panel"].hidden, true, "closing puts the panel away");
equal(els["pane-detail"].dataset.panel, undefined, "and gives the transcript its place back");
equal(liveTimers().length, 0, "and stops the lease, which is what eventually stops the pipe");

/* ---- the backend that has to be asked ------------------------------------- */

answer = {
    screen: {
        id: "%1", backend: "iterm", channel: "on-demand", revision: "q", readable: true,
        pending: false, text: "sampled", lines: 60, askAgainAfterMs: 1000
    }
};
Terminal.open();
await settle();
ok(els["screen-badge"].innerHTML.indexOf("iTerm2") >= 0,
    "the header names this backend by its own name");
ok(els["screen-badge"].innerHTML.indexOf("on demand") >= 0,
    "and says out loud that nothing will tell it when this screen changes");
ok(els["screen-badge"].innerHTML.indexOf(">live<") < 0,
    "so the two backends are never drawn with the same claim");
equal(liveTimers().length, 2, "which is why this one runs a second clock");
equal(liveTimers()[1].ms, 1000,
    "at the interval the Mac named, because that number is the Mac's and not this page's");
Terminal.close(false);
equal(liveTimers().length, 0, "both stop together");

/* ---- a screen that is not there ------------------------------------------- */

answer = null;
Terminal.open();
await settle();
ok(els["screen-body"].innerHTML.indexOf("could not be read") > 0,
    "a session whose screen has gone says so rather than showing an empty terminal");
Terminal.close(false);

/* ---- a real row, end to end ----------------------------------------------
 *
 * The pins above go through `paintRows()` directly. This one goes through the panel: a capture
 * with an indent, a colour and 180 columns of trailing padding, drawn the way the browser would
 * receive it. There is no control here to press and nothing kept in `localStorage` — the panel
 * has one layout, so what a second visit gets is what the first one got.
 * -------------------------------------------------------------------------- */

answer = {
    screen: {
        id: "%1", backend: "tmux", channel: "signalled", revision: "r1", readable: true,
        pending: false, text: "  " + E + "[31m了" + E + "[39m" + spaces(180), lines: 59
    }
};
Terminal.open();
await settle();
const fetched = asked.length;

// The whole body, as a literal: the indent hung under itself, the colour kept, the 180 columns
// of padding gone. Every part of that is asserted on its own further up; what this adds is that
// the panel puts them together and wraps them in the one container element.
equal(els["screen-body"].innerHTML,
    '<div class="screen-text"><div class="screen-row" ' +
        'style="padding-left:2ch;text-indent:-2ch">  ' +
        '<span style="color:var(--term-1)">了</span></div></div>',
    "a real row reaches the page indented, coloured and unpadded, and in that one container");
equal(asked.length, fetched, "and drawing it asked the Mac for nothing beyond the first fetch");
Terminal.close(false);

console.log("  " + String.fromCharCode(10003) + " web terminal (" + checks + " checks)");
