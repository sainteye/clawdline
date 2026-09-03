import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

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
 */

const source = await readFile(
    new URL("../Resources/web/app/js/view/terminal.js", import.meta.url), "utf8");

/* ---- the smallest page this module can be evaluated against --------------- */

let checks = 0;
function equal(actual, expected, message) { assert.deepEqual(actual, expected, message); checks += 1; }
function ok(value, message) { assert.ok(value, message); checks += 1; }

function element() {
    return {
        innerHTML: "",
        hidden: true,
        dataset: {},
        disabled: false,
        focused: 0,
        focus: function () { this.focused += 1; }
    };
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

const esc = function (s) {
    return String(s).replace(/[&<>"]/g, function (c) {
        return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c];
    });
};

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
const paint = Terminal.paintForTesting;

const E = "\u001b";
const settle = function () { return new Promise(function (r) { setTimeout(r, 0); }); };

/* ---- what is drawn -------------------------------------------------------- */

equal(paint("plain text"), "plain text", "text with no colour in it is left alone");
equal(paint(E + "[31mred" + E + "[39m after"),
    '<span style="color:var(--term-1)">red</span> after',
    "a colour opens a span and the reset closes it");
equal(paint("a" + E + "[1;32mb" + E + "[0mc"),
    'a<span style="color:var(--term-2);font-weight:600">b</span>c',
    "two parameters in one sequence both apply");
equal(paint(E + "[38;5;208mo" + E + "[m"),
    '<span style="color:rgb(255,135,0)">o</span>',
    "a 256-colour index is computed rather than looked up");
equal(paint(E + "[38;2;12;34;56mo" + E + "[m"),
    '<span style="color:rgb(12,34,56)">o</span>',
    "and true colour is taken as written");

// **The sequences that are not colour.** tmux does not emit them in a capture, but the panel is
// looking at whatever a program drew, and a cursor move printed as text would be visible garbage
// on somebody's screen.
equal(paint(E + "[?25lhidden" + E + "[?25h"), "hidden", "a cursor-visibility sequence is dropped");
equal(paint(E + "[2Jcleared"), "cleared", "and so is a clear");
equal(paint(E + "[Habc"), "abc", "and a cursor move with no parameters");

// **Escaped before it is wrapped, in that order.** The content of this panel is chosen by a
// program running on somebody's Mac; markup in it is not markup.
equal(paint("<script>alert(1)</script>"), "&lt;script&gt;alert(1)&lt;/script&gt;",
    "nothing a program prints becomes markup");
equal(paint(E + '[31m"><img src=x>' + E + "[39m"),
    '<span style="color:var(--term-1)">&quot;&gt;&lt;img src=x&gt;</span>',
    "including inside a coloured run");

// Newlines are the rows of the grid and stay; a carriage return would draw a line on top of
// itself and does not.
equal(paint("one\ntwo"), "one\ntwo", "rows survive");
equal(paint("one\r\ntwo"), "one\ntwo", "a carriage return does not");
equal(paint(null), "", "nothing to draw is nothing drawn");

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
ok(els["screen-body"].innerHTML.indexOf('<pre class="screen-text">') === 0,
    "the screen is drawn as a grid rather than as prose");
ok(els["screen-body"].innerHTML.indexOf("var(--term-1)") > 0, "with its colour");

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
equal(els["screen-body"].innerHTML, '<pre class="screen-text">moved</pre>',
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

console.log("  " + String.fromCharCode(10003) + " web terminal (" + checks + " checks)");
