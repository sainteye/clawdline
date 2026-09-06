/**
 * The waiting card, and the one button on it that does not depend on a reading.
 *
 * **Why this file exists at all.** Until today nothing in `Tests/` touched `els.waiting`. The card
 * is the loudest thing the phone draws — it is what says a session has stopped and is asking
 * somebody something — and every word on it was reaching a screen on nobody's authority but a
 * reviewer's eye. These are its first assertions, so they start where the card is most fragile:
 * with what it draws when the Mac could **not** read the menu.
 *
 * **The property under test is an absence of conditions.** Reading a question on a phone depends
 * on Clawdline having parsed a menu off the Mac's screen, and that parse can fail, arrive late, or
 * be of the wrong menu. So the live-screen button is the floor under the card: it has to be there
 * when the menu rendered perfectly, when nothing rendered at all, and in the third shape that
 * second arm takes when this transport has no evidence-refresh route to offer. Asserting it once,
 * in one state, would prove nothing about the state it exists for — the whole point of the button
 * is the case where everything else on the card is wrong.
 *
 * **How the module is loaded.** `view/composer.js` reaches `els` and `document` while it is being
 * evaluated, so it cannot simply be imported into a bare Node process. It is read as text, its
 * imports stripped, and evaluated against the doubles below — the same shape `Tests/web-terminal.mjs`
 * uses for the panel this button opens. `esc`, `T` and `words` are the real ones: markup pinned
 * byte for byte is only worth the bytes if it is pinned against the functions the browser runs.
 *
 * **What this file cannot prove.** The press leaves `view/composer.js` as a document event, and
 * the module that turns it into `Terminal.open()` is `input/action-confirm.js` — too large to
 * import here, and importing it would pull in the graph the event exists to stay out of. So the
 * far half of that wire is read as text and its one registration is evaluated on its own against
 * a `Terminal` double. That proves the statement in the shipped file opens the panel when the
 * event fires; it does not prove the browser ever evaluates that file, and nothing here does.
 * What stands behind that is the same thing that stands behind every other listener in it: it is
 * imported from `main.js`'s graph, and if it were not, the whole session-actions sheet would be
 * dead too.
 */
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { esc } from "../Resources/web/app/js/core/esc.js";
import { T, fill, words } from "../Resources/web/app/js/core/i18n.js";

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error("FAIL: " + message);
}
function equal(actual, expected, message) {
    check(Object.is(actual, expected) || JSON.stringify(actual) === JSON.stringify(expected),
        message + "\n    actual:   " + JSON.stringify(actual) +
        "\n    expected: " + JSON.stringify(expected));
}

const js = new URL("../Resources/web/app/js/", import.meta.url);
const composerSource = await readFile(new URL("view/composer.js", js), "utf8");
const confirmSource = await readFile(new URL("input/action-confirm.js", js), "utf8");
const styles = await readFile(new URL("../Resources/web/app/css/composer.css", import.meta.url),
    "utf8");

/* ---- the smallest page this module can be evaluated against ---------------- */

function element() {
    const listeners = {};
    const el = {
        hidden: false, textContent: "", innerHTML: "", dataset: {},
        addEventListener: function (name, fn) { listeners[name] = fn; },
        appendChild: function () {},
        setAttribute: function () {}, removeAttribute: function () {},
        getAttribute: function () { return null; },
        focus: function () {},
        querySelector: function () { return null; },
        querySelectorAll: function () { return el.options || []; },
        classList: { add: function () {}, remove: function () {}, toggle: function () {} },
        style: { setProperty: function () {}, removeProperty: function () {} },
        press: function (node) { if (listeners.click) listeners.click({ target: node }); }
    };
    return el;
}

const els = { waiting: element(), live: element() };
const S = { openId: null, write: true, sessions: [] };
let session = null;
function byId() { return session; }

const toasts = [];
function toast(message, bad) { toasts.push({ message: message, bad: !!bad }); }

const asked = { focus: 0, key: [], refresh: 0 };
let evidence = null;
const api = {
    focus: function () { asked.focus += 1; return Promise.resolve({}); },
    key: function (id, key) { asked.key.push([id, key]); return Promise.resolve({}); },
    refreshSessionEvidence: function () { asked.refresh += 1; return Promise.resolve({}); },
    sessionRefreshEvidenceState: function () { return evidence; }
};

const documentListeners = new Map();
globalThis.document = {
    activeElement: null,
    createElement: function () { return element(); },
    addEventListener: function (name, fn) {
        if (!documentListeners.has(name)) documentListeners.set(name, []);
        documentListeners.get(name).push(fn);
    },
    dispatchEvent: function (event) {
        (documentListeners.get(event.type) || []).forEach(function (fn) { fn(event); });
    }
};
globalThis.CustomEvent = class { constructor(type) { this.type = type; } };

globalThis.esc = esc;
globalThis.T = T;
globalThis.fill = fill;
globalThis.words = words;
globalThis.S = S;
globalThis.els = els;
globalThis.api = api;
globalThis.byId = byId;
globalThis.toast = toast;
globalThis.hasKeyboard = false;
globalThis.closingID = function () { return null; };
globalThis.Shots = { count: function () { return 0; } };
globalThis.msgText = function () { return ""; };
globalThis.sending = function () { return false; };
globalThis.Voice = { available: false };
globalThis.drawSpinner = function () {};
globalThis.setLiveSpin = function () {};
globalThis.spinPhase = 0;

// `export function renderWaiting` becomes a global function declaration: indirect eval runs in
// global scope, so the declarations this file makes are reachable afterwards without rewriting
// each name by hand.
const standalone = composerSource
    .replace(/^import .*$/gm, "")
    .replace(/^export /gm, "");
(0, eval)(standalone);
const renderWaiting = globalThis.renderWaiting;
check(typeof renderWaiting === "function",
    "view/composer.js still exports renderWaiting under that name");

/* ---- the states the card can be in ----------------------------------------- */

// Every draw is compared with the last one and skipped when they match, so a test that changes
// state and re-renders has to defeat that cache the way the module's own callers do.
function draw() {
    globalThis.waitingDrawn = null;
    renderWaiting();
    return els.waiting.innerHTML;
}

// **A new session id every time, and that is not tidiness.** `dismissedMenu` and `foldedMenu` are
// keyed on the session id and the menu's own contents, and they are cleared only when one of those
// changes. Reusing one id meant that the moment this file exercised the dismiss button, every
// later `waiting(MENU)` came back hushed — and the press assertions below went on passing against
// a card with nothing in it, because the click listener is bound to the box and not to its
// contents. That is the shape of a test that cannot fail; the counter is what stops it.
let served = 0;
function waiting(menu) {
    served += 1;
    session = { id: "S" + served, state: "waiting", menu: menu || null };
    S.openId = session.id;
    S.sessions = [session];
}

const MENU = {
    question: "Shall I force-push?",
    options: [{ n: 1, label: "Yes" }, { n: 2, label: "No" }]
};

const screenButton = new RegExp(
    '<button type="button" class="go" data-screen="1">' + esc(T.webSessionScreen) + "</button>");
const focusButton = new RegExp(
    '<button type="button" class="go" data-focus="1">' + esc(T.webShowOnMac) + "</button>");

/* The three shapes the body takes, and the button is in all of them. */

waiting(MENU);
evidence = null;
const withMenu = draw();
check(screenButton.test(withMenu),
    "a card that read the menu carries the live-screen button");
check(withMenu.indexOf('class="menu"') !== -1,
    "and that card really is the menu arm — otherwise the assertion above tested nothing");

waiting(null);
evidence = { busy: false, status: "idle" };
const noMenu = draw();
check(screenButton.test(noMenu),
    "a card that could not read a menu carries it too — the case the button exists for");
check(noMenu.indexOf('data-refresh="1"') !== -1,
    "and that card really is the unread arm with its refresh button");
check(noMenu.indexOf('class="menu"') === -1, "with no menu drawn in it");

waiting(null);
evidence = null;
const noRefresh = draw();
check(screenButton.test(noRefresh),
    "and so does the third shape that arm takes, where this transport offers no refresh at all");
check(noRefresh.indexOf('data-refresh="1"') === -1,
    "which is the shape with no refresh button in it");

// A question with no options is a fourth shape: the parse found the prose and not the rows.
waiting({ question: "Shall I force-push?", options: [] });
evidence = null;
const questionOnly = draw();
check(screenButton.test(questionOnly),
    "a question whose options did not parse still carries the button");
check(questionOnly.indexOf('class="question"') !== -1, "and still shows the question");

// Several questions in one call, with the picker's own progress drawn above them.
waiting({ question: "Shall I force-push?", options: [{ n: 1, label: "Yes" }],
    steps: [{ done: true, answer: "Water" }, { done: false }] });
evidence = null;
const withSteps = draw();
check(screenButton.test(withSteps), "and so does a card drawn with a step row");
check(withSteps.indexOf('class="steps"') !== -1, "which is the shape that has one");

/* Where the button sits: outside the branch, beside the button that was already there. */

for (const [name, html] of [["menu", withMenu], ["unread", noMenu], ["no-refresh", noRefresh]]) {
    check(focusButton.test(html) && screenButton.test(html) &&
        html.indexOf('data-focus="1"') < html.indexOf('data-screen="1"'),
        "in the " + name + " shape both always-drawn buttons are present, in that order");
}
// The property that makes it survive a failed parse: the same two buttons close the body in every
// shape, so no arm of the `rows ? … : …` branch can be the thing that supplies them. Asserted as
// the last thing in the markup rather than as "present somewhere", because "present somewhere" is
// exactly what a copy inside one arm would also satisfy.
for (const [name, html] of [["menu", withMenu], ["unread", noMenu], ["no-refresh", noRefresh],
    ["question-only", questionOnly], ["steps", withSteps]]) {
    check(/data-screen="1">[^<]*<\/button><\/div>$/.test(html),
        "the " + name + " shape ends with the live-screen button and the body's close tag");
    check(html.split('data-screen="1"').length === 2,
        "and draws it once rather than once per arm");
}

/* The words are the ones the session menu already uses, and no new key was added. */

const label = /data-screen="1">([^<]*)<\/button>/.exec(noMenu);
equal(label && label[1], esc(T.webSessionScreen),
    "the label is T.webSessionScreen — the same words as the session menu's own entry");
check(composerSource.indexOf("webSessionScreen") !== -1,
    "and it is read from the string table rather than written into the view");
const i18nSource = await readFile(new URL("core/i18n.js", js), "utf8");
check(/^\s*webSessionScreen:\s*"Live screen",$/m.test(i18nSource),
    "the English fallback for that key is the one that landed with the panel, unchanged");
check(!/webWaitingScreen|webLiveScreen|webWaitingLive/.test(composerSource + i18nSource),
    "no second name for one panel was invented for this button");

/* ---- the card shapes where it is deliberately absent ----------------------- */

session = null;
S.openId = null;
check(draw() === "", "no session open draws nothing at all");
check(els.waiting.hidden === true, "and the card is hidden");

waiting(MENU);
session.state = "working";
check(draw() === "", "a session that is not waiting draws nothing at all");

// Folded and dismissed drop the body rather than hiding it, which is what makes a folded card
// cost no height. The button goes with the body; the card is one line by the reader's own choice.
waiting(MENU);
evidence = null;
draw();
els.waiting.press({ closest: function (s) { return s === "[data-fold]" ? this : null; } });
const foldedHTML = els.waiting.innerHTML;
check(!screenButton.test(foldedHTML), "a folded card carries no buttons, the new one included");
check(foldedHTML.indexOf('class="title"') !== -1, "because a folded card is its title and nothing else");
els.waiting.press({ closest: function (s) { return s === "[data-fold]" ? this : null; } });
check(screenButton.test(els.waiting.innerHTML), "and unfolding brings it straight back");

waiting(MENU);
evidence = null;
draw();
els.waiting.press({ closest: function (s) { return s === "[data-dismiss]" ? this : null; } });
check(els.waiting.innerHTML === "", "a waved-away card is gone entirely, buttons and all");

/* ---- the press ------------------------------------------------------------- */

let opened = 0;
globalThis.document.addEventListener("clawdline:open-screen", function () { opened += 1; });

// Every press below is preceded by the assertion that there is something to press. The listener is
// bound to the box rather than to the buttons in it, so a press against a card that drew nothing
// routes exactly as well as a press against a card that drew — which is how the whole block came
// to be passing against an empty card once.
function pressMarker(marker, extra) {
    check(els.waiting.innerHTML.indexOf("data-") !== -1,
        "the card is on screen to be pressed (" + marker + ")");
    const node = Object.assign({
        dataset: {}, disabled: false,
        getAttribute: function () { return null; },
        closest: function (selector) { return selector === "[" + marker + "]" ? node : null; }
    }, extra || {});
    els.waiting.press(node);
    return node;
}

waiting(MENU);
evidence = null;
draw();
pressMarker("data-screen");
equal(opened, 1, "pressing the live-screen button asks for the screen exactly once");
equal(asked.focus, 0, "and asks the Mac to focus nothing");
equal(asked.key.length, 0, "and sends no keystroke into the session");

pressMarker("data-focus");
equal(opened, 1, "pressing Show on Mac does not open the screen");
equal(asked.focus, 1, "it does what it always did");

waiting(null);
evidence = { busy: false, status: "idle" };
draw();
pressMarker("data-refresh");
equal(opened, 1, "pressing Refresh does not open the screen either");
equal(asked.refresh, 1, "it does what it always did");

waiting(MENU);
evidence = null;
draw();
pressMarker("data-key", { dataset: { key: "1" } });
equal(opened, 1, "and answering the question does not open the screen");
equal(asked.key.length, 1, "it sends the keystroke");

// The button is the floor under the card, so it has to work in the state the card is in when the
// reading failed — which is the state with no `openId` menu behind it at all.
waiting(null);
evidence = null;
draw();
pressMarker("data-screen");
equal(opened, 2, "and it opens the screen from the card that could not read a menu");

// With no session open there is no card to press, and the handler must not act on a stray event.
// The one press in this file that is deliberately against an empty box, so it goes around the
// guard above rather than tripping it.
session = null;
S.openId = null;
check(draw() === "", "the card really is gone before this last press");
els.waiting.press({
    closest: function (selector) { return selector === "[data-screen]" ? this : null; }
});
equal(opened, 2, "a press with no session open opens nothing");

/* ---- the far half of the wire ---------------------------------------------- */

// `view/composer.js` does not import `view/terminal.js`, and this is the assertion that keeps it
// that way. The two modules already sit in one strongly connected component — `view/composer.js`
// reaches `view/terminal.js` in three hops through `input/composer.js` and `session/open.js` — so
// a direct edge would not invent a cycle, it would tighten one that is already 21 modules wide.
// The card announces the press on the document instead, which is the same crossing
// `clawdline:session-refresh` already makes in the other direction in this very file.
check(!/^import .*view\/terminal\.js|^import .*from "\.\/terminal\.js"/m.test(composerSource),
    "view/composer.js does not import view/terminal.js");
check(/document\.dispatchEvent\(new CustomEvent\("clawdline:open-screen"\)\)/.test(composerSource),
    "it dispatches clawdline:open-screen instead");

// Up to the `});` that closes the handler, not the first `);` in it — `Terminal.open();` ends in
// one, and a lazier pattern cuts the statement in half and hands `Function` something that will
// not parse.
const registration = /document\.addEventListener\("clawdline:open-screen",[\s\S]*?\}\);/
    .exec(confirmSource);
check(registration !== null,
    "input/action-confirm.js registers a clawdline:open-screen listener");
check(/^import \{ Terminal \} from "\.\.\/view\/terminal\.js";$/m.test(confirmSource),
    "and that file is one that already holds Terminal, so the wire adds no import anywhere");

if (registration) {
    // The registration on its own, against a Terminal double: not a description of what that
    // line does, but that line doing it.
    //
    // **The double answers every call the panel exposes, not only the one being asserted.** With
    // an `open`-only stub, a listener that called `Terminal.close(true)` threw a TypeError instead
    // of failing — and a suite that crashes reports nothing at all, which on a mutation run looks
    // exactly like a suite with no failures. Measured: that is what this block did until the
    // double below replaced it.
    const called = [];
    const panel = {};
    for (const name of ["open", "close", "refresh", "follow", "observe"]) {
        panel[name] = function () { called.push(name + "(" + [...arguments].join(",") + ")"); };
    }
    const stub = { addEventListener: function (name, fn) { stub.fn = fn; } };
    try {
        (0, Function)("document", "Terminal", registration[0])(stub, panel);
    } catch (e) {
        check(false, "the registration statement parses and runs on its own: " + e.message);
    }
    check(typeof stub.fn === "function", "the registration really registers something");
    if (stub.fn) {
        try {
            stub.fn({ type: "clawdline:open-screen" });
        } catch (e) {
            check(false, "and firing it does not throw: " + e.message);
        }
    }
    equal(called, ["open()"],
        "and firing it calls Terminal.open() exactly once, with no argument and nothing else");
}

/* ---- two buttons where there was one --------------------------------------- */

// They are inline-block, so at a width that will not hold both, the second wraps under the first
// rather than being cut off. Measured in Chrome: the card's body is 269px inside a 320px viewport
// and only Russian exceeds it — «Показать на Mac» beside «Экран в реальном времени» needs 302px —
// so the wrap is a shape somebody really sees. The gap is written on the first button's right
// rather than the second's left because that is the only thing the two spellings disagree about:
// wrapped, the trailing margin leaves the second button at 1px from the edge and the leading one
// leaves it at 7px. That is a rendered fact rather than a parseable one, so what is pinned here is
// only the rule it was decided from; the measurement lives in the comment beside it in the CSS.
check(/\.composer \.waiting \.go\[data-focus\] \{[^}]*margin-right: 6px;[^}]*\}/.test(styles),
    "composer.css puts the gap on the trailing edge of the button that comes first");

console.log((failed ? "not ok" : "ok") + ": web waiting card, " + checks + " checks");
if (failed) process.exit(1);
