import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

/*
 * What a finger on a session row is allowed to do to that row.
 *
 * The horizontal gesture used to take the row over the moment a finger landed on it: the End
 * button was unhidden, `data-swipe` went on, and the row's contents were given a transform —
 * all from `touchstart`, before anybody had moved. On a phone that is not a free thing to do.
 * WebKit decides whether a tap was a hover or a press by watching what the page does while the
 * touch is being handled, and a touch handler that reveals content is answered with the hover
 * alone: no click is dispatched, and the row opens on the *second* tap. It happened on every
 * row, every time, because the button was hidden again as each tap ended.
 *
 * So the contract this file holds is narrow and worth stating on its own: **a tap changes
 * nothing about the row**, and the swipe still works. The gesture module is loaded with its
 * four imports replaced, which is enough to drive it without a browser.
 */

const source = await readFile(
    new URL("../Resources/web/app/js/input/swipe.js", import.meta.url), "utf8");

const standalone = source
    .replace('import { phone, reduced } from "../core/env.js";',
        "const phone = function () { return globalThis.__swipeEnv.phone; };\n" +
        "const reduced = false;")
    .replace('import { S } from "../core/state.js";', "const S = globalThis.__swipeEnv.state;")
    .replace('import { els } from "../core/dom.js";', "const els = globalThis.__swipeEnv.els;")
    .replace('import { ActionConfirm } from "./action-confirm.js";',
        "const ActionConfirm = globalThis.__swipeEnv.confirm;");

let checks = 0;
function equal(actual, expected, message) { assert.deepEqual(actual, expected, message); checks += 1; }

/* ---- the smallest page these listeners can be attached to ----------------- */

const listeners = { scroll: {}, document: {}, window: {} };
function record(bag) {
    return function (name, fn) { (bag[name] = bag[name] || []).push(fn); };
}
function fire(bag, name, event) {
    (bag[name] || []).forEach(function (fn) { fn(event); });
    return event;
}

function element(className) {
    const node = {
        className: className,
        dataset: {},
        hidden: true,
        style: {
            properties: {},
            setProperty: function (name, value) { this.properties[name] = value; },
            removeProperty: function (name) { delete this.properties[name]; }
        },
        classList: {
            contains: function (name) { return node.className.split(" ").indexOf(name) >= 0; }
        }
    };
    return node;
}

/** A row, the End button inside it, and the label a thumb actually lands on. */
function buildRow(id) {
    const row = element("row");
    row.dataset.id = id;
    const button = element("swipe-end");
    row.querySelector = function (selector) {
        return selector === ".swipe-end" ? button : null;
    };
    row.contains = function (node) { return node === row || node === button || node === label; };
    const label = {
        closest: function (selector) { return selector === ".row" ? row : null; }
    };
    button.closest = function (selector) {
        return selector === ".swipe-end" ? button : (selector === ".row" ? row : null);
    };
    row.closest = function (selector) { return selector === ".row" ? row : null; };
    return { row: row, button: button, label: label };
}

function touch(x, y) { return { clientX: x, clientY: y }; }
function gesture(target, points) {
    return {
        target: target,
        touches: points,
        prevented: false,
        preventDefault: function () { this.prevented = true; },
        stopPropagation: function () { this.stopped = true; }
    };
}

/** Everything the row is carrying that a browser can see. A tap must leave all of it alone. */
function marks(row) {
    return {
        swipe: row.dataset.swipe || null,
        button: row.querySelector(".swipe-end").hidden,
        style: Object.keys(row.style.properties).sort()
    };
}
const untouched = { swipe: null, button: true, style: [] };

const opened = [];
globalThis.__swipeEnv = {
    phone: true,
    state: { write: true },
    confirm: { open: function (what, id) { opened.push({ what: what, id: id }); } },
    els: {
        "list-scroll": { addEventListener: record(listeners.scroll) },
        rows: {}
    }
};
globalThis.document = {
    addEventListener: record(listeners.document),
    documentElement: { contains: function () { return true; } }
};
globalThis.window = { addEventListener: record(listeners.window) };
globalThis.MutationObserver = function () {
    return { observe: function () {} };
};

const { SwipeRows } = await import(
    "data:text/javascript;base64," + Buffer.from(standalone).toString("base64"));

/* ---- a tap ---------------------------------------------------------------- */

const first = buildRow("alpha");
fire(listeners.scroll, "touchstart", gesture(first.label, [touch(120, 300)]));
equal(marks(first.row), untouched, "a finger landing on a row writes nothing to that row");

// A thumb is not a stylus: a press wanders a few pixels before it lifts.
fire(listeners.scroll, "touchmove", gesture(first.label, [touch(123, 305)]));
equal(marks(first.row), untouched, "the wobble inside a press still writes nothing");

fire(listeners.scroll, "touchend", gesture(first.label, []));
equal(marks(first.row), untouched, "and the row is handed back exactly as it was found");

const tapClick = fire(listeners.document, "click", gesture(first.label, []));
equal(tapClick.prevented, false, "the click a tap synthesises reaches the row's own handler");

/* ---- a scroll ------------------------------------------------------------- */

const second = buildRow("beta");
fire(listeners.scroll, "touchstart", gesture(second.label, [touch(120, 300)]));
const scrolled = fire(listeners.scroll, "touchmove", gesture(second.label, [touch(122, 260)]));
equal(marks(second.row), untouched, "a vertical drag is the list scrolling, not a row moving");
equal(scrolled.prevented, false, "and the page keeps the gesture");
fire(listeners.scroll, "touchend", gesture(second.label, []));
equal(marks(second.row), untouched, "a row the list scrolled past is left as it was");

/* ---- a swipe -------------------------------------------------------------- */

const third = buildRow("gamma");
fire(listeners.scroll, "touchstart", gesture(third.label, [touch(220, 300)]));
const dragged = fire(listeners.scroll, "touchmove", gesture(third.label, [touch(180, 303)]));
equal(marks(third.row), {
    swipe: "dragging", button: false, style: ["--swipe-button-x", "--swipe-x"]
}, "a sideways drag is when the row moves and the button it uncovers appears");
equal(dragged.prevented, true, "the swipe claims the gesture from the scroller");
equal(third.row.style.properties["--swipe-x"], "-40px", "the row follows the finger");

fire(listeners.scroll, "touchmove", gesture(third.label, [touch(120, 303)]));
equal(third.row.style.properties["--swipe-x"], "-100px", "and keeps following it");
fire(listeners.scroll, "touchend", gesture(third.label, []));
equal(third.row.dataset.swipe, "settling", "letting go settles the row into its resting place");
equal(third.row.style.properties["--swipe-x"], "-126px", "past the halfway mark it stays open");

const swipeClick = fire(listeners.document, "click", gesture(third.label, []));
equal(swipeClick.prevented, true, "the click at the end of a swipe does not open the session");

const endPress = fire(listeners.document, "click", gesture(third.button, []));
equal(endPress.prevented, true, "the revealed button is its own control");
equal(opened, [{ what: "end", id: "gamma" }], "and pressing it asks the closing question");

/* ---- a tap on a different row, while one is open -------------------------- */

// A row that has been swiped open is put away by the document's own capture listener, which a
// browser runs before the one on the scroller. `touchstart` no longer does that a second time,
// so this is the order the closing now depends on — and the tap that closes it is still the tap
// that opens the row it landed on.
const open = buildRow("delta");
fire(listeners.scroll, "touchstart", gesture(open.label, [touch(220, 300)]));
fire(listeners.scroll, "touchmove", gesture(open.label, [touch(150, 302)]));
fire(listeners.scroll, "touchend", gesture(open.label, []));
equal(open.row.style.properties["--swipe-x"], "-126px", "the row is left open behind its button");

const across = buildRow("epsilon");
fire(listeners.document, "touchstart", gesture(across.label, [touch(120, 400)]));
equal(open.row.dataset.swipe, "settling", "touching another row puts the open one away");
fire(listeners.scroll, "touchstart", gesture(across.label, [touch(120, 400)]));
fire(listeners.scroll, "touchend", gesture(across.label, []));
equal(marks(across.row), untouched, "and the row being tapped is still untouched");

/* ---- the other half of the same tap --------------------------------------- */

// WebKit applies `:hover` to the tapped element before it decides what the tap was, so a row
// whose hover repaints it is a row that can spend a tap on the hover. On a phone there is
// nothing to hover with, and the honest way to say so is not to declare the rule at all.
const listCSS = await readFile(
    new URL("../Resources/web/app/css/list.css", import.meta.url), "utf8");
const responsiveCSS = await readFile(
    new URL("../Resources/web/app/css/responsive.css", import.meta.url), "utf8");

const unguarded = listCSS.split("\n").filter(function (line) {
    return /\.row[^{]*:hover/.test(line) && line.indexOf("@media (hover: hover)") < 0;
});
equal(unguarded, [], "every row hover rule is behind a pointer that can hover");
equal(responsiveCSS.split("\n").filter(function (line) {
    return /\.row[^{]*:hover/.test(line);
}), [], "and the phone breakpoint no longer repaints a hover it cannot have");

/* ---- a read-only page ----------------------------------------------------- */

globalThis.__swipeEnv.state.write = false;
const fifth = buildRow("epsilon");
fire(listeners.scroll, "touchstart", gesture(fifth.label, [touch(220, 300)]));
fire(listeners.scroll, "touchmove", gesture(fifth.label, [touch(160, 303)]));
equal(marks(fifth.row), untouched, "a device that cannot end a session cannot swipe one open");
globalThis.__swipeEnv.state.write = true;

SwipeRows.reset(true);

console.log("web row gesture tests passed (" + checks + " checks)");
process.exit(0);
