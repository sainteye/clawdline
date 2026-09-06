/**
 * The test push, and the half of it that could not be asked before.
 *
 * `Settings → Test notification` answered one question — *did a notification arrive* — and the
 * half that actually goes wrong is the other one: *does tapping one get me back to my session*.
 * Every push this app sends that a person would tap is about a session, and until now the one
 * push a person can fire on purpose was the only one that could never be about one, so the road
 * from a lock screen back to a transcript had no way to be walked deliberately.
 *
 * Two halves here, and both are the join rather than either end. The transport half drives
 * `net/live.js` for real and reads what went on the wire; the button half drives the real
 * `Settings.test` against a stand-in transport and reads what it was handed. What neither of them
 * asserts is the address itself — that is the Mac's decision and `Tests/HookTests.swift` holds it.
 *
 * The fixture ids are tmux pane ids. `%` is the character this whole line broke on, and an iTerm
 * id — `w0t0p0:<UUID>` — carries none, so a fixture written on one is blind to the family.
 */
import assert from "node:assert/strict";

const noop = function () { };
const element = new Proxy(function () { }, {
    get: function (_target, key) {
        if (key === Symbol.iterator) return function* () { };
        if (key === "classList") return { add: noop, remove: noop, toggle: noop,
            contains: function () { return false; } };
        if (key === "style" || key === "dataset") return { setProperty: noop, removeProperty: noop };
        if (key === "children" || key === "querySelectorAll") return [];
        if (key === "content") return { cloneNode: function () { return element; } };
        return element;
    },
    apply: function () { return element; }
});

globalThis.localStorage = { getItem: function () { return null; }, setItem: noop };
globalThis.location = { search: "?mock=1", protocol: "http:", hostname: "localhost",
    pathname: "/", href: "http://localhost/" };
globalThis.history = { replaceState: noop, pushState: noop };
Object.defineProperty(globalThis, "navigator", {
    value: { userAgent: "node", maxTouchPoints: 0 }, configurable: true
});
globalThis.window = element;
window.devicePixelRatio = 1;
window.matchMedia = function () { return { matches: false, addEventListener: noop }; };
globalThis.document = element;
document.documentElement = { lang: "en", style: { setProperty: noop, removeProperty: noop } };
document.getElementById = function () { return element; };
document.querySelector = function () { return element; };
document.querySelectorAll = function () { return []; };
document.createElement = function () { return element; };
document.body = element;
globalThis.MutationObserver = class { observe() { } disconnect() { } };
globalThis.ResizeObserver = MutationObserver;
globalThis.IntersectionObserver = MutationObserver;

// Nothing here reaches a network. What the transport put on the wire is the assertion.
const reached = [];
globalThis.fetch = function (path, options) {
    reached.push({ path: path, options: options });
    return Promise.resolve({ ok: true, text: function () {
        return Promise.resolve(JSON.stringify({ ok: true, sent: 1 }));
    } });
};

/* ---- the transport ------------------------------------------------------- */

const { Live } = await import("../Resources/web/app/js/net/live.js");
assert.equal(typeof Live.pushTest, "function", "the local transport carries the test push");

await Live.pushTest("%208");
assert.equal(reached.length, 1, "one request");
assert.equal(reached[0].path, "/v1/push/test", "at the documented route");
assert.equal(reached[0].options.method, "POST");
assert.deepEqual(JSON.parse(reached[0].options.body), { session_id: "%208" },
    "carrying the session the reader is looking at, raw — the encoding is the Mac's job");

await Live.pushTest();
assert.equal(reached.length, 2, "a second request");
assert.deepEqual(JSON.parse(reached[1].options.body), {},
    "and with no session open it sends the body it always sent, not a null the route must skip");

await Live.pushTest(null);
assert.deepEqual(JSON.parse(reached[2].options.body), {},
    "an explicit nothing is the same nothing");
await Live.pushTest("");
assert.deepEqual(JSON.parse(reached[3].options.body), {},
    "and so is an empty id, which is what a session list with no selection can produce");

/* ---- the button ---------------------------------------------------------- */

const { S } = await import("../Resources/web/app/js/core/state.js");
const selected = await import("../Resources/web/app/js/net/api.js");
const asked = [];
selected.useApi({
    pushTest: function (sessionId) {
        asked.push(arguments.length === 0 ? "(no argument)" : sessionId);
        return Promise.resolve({ ok: true, sent: 1 });
    }
});

const { Settings } = await import("../Resources/web/app/js/input/settings.js");
assert.equal(typeof Settings.test, "function", "the settings page still owns the button");

/** One press, waited out: `test` clears its own busy flag inside the promise chain. */
async function press() {
    const before = asked.length;
    Settings.test();
    for (let turn = 0; turn < 50 && asked.length === before; turn += 1) {
        await new Promise(function (resolve) { setTimeout(resolve, 0); });
    }
    // And once more, so the `testing` flag is back down before the next press.
    for (let turn = 0; turn < 5; turn += 1) {
        await new Promise(function (resolve) { setTimeout(resolve, 0); });
    }
}

S.openId = "%247";
await press();
assert.deepEqual(asked, ["%247"],
    "the button sends the transcript that is on screen, so the tap can come back to it");

S.openId = null;
await press();
assert.deepEqual(asked, ["%247", null],
    "and sends nothing when nothing is open, rather than an id the Mac would refuse");

// **And the list's own pointer when there is no transcript on screen**, which on a phone is
// every time this button can be pressed at all: `.pane-detail` is a fixed full-screen layer
// there, so an open session covers the header this page is reached through. Written as `openId`
// alone, the button sent `/` on every press from a phone — measured on 2026-09-06, in the audit
// line for the press, on the device the lever was built for.
S.openId = null;
S.selectedId = "%141";
await press();
assert.deepEqual(asked, ["%247", null, "%141"],
    "with nothing on screen the button sends what the list is pointing at, so a phone can test at all");

// The transcript in front of the reader still wins where both can be true at once.
S.openId = "%208";
S.selectedId = "%141";
await press();
assert.deepEqual(asked, ["%247", null, "%141", "%208"],
    "and where both exist — a desktop, where the header and the pane share the screen — the "
    + "transcript on screen is the better answer");

// Neither, and it still refuses to invent one.
S.openId = null;
S.selectedId = null;
await press();
assert.deepEqual(asked, ["%247", null, "%141", "%208", null],
    "with neither it sends nothing rather than an id the Mac would refuse");

// Still four: `reached` counts the transport calls at the top of this file, and every press
// below goes through the stand-in.
assert.equal(reached.length, 4,
    "and the button never went to the network in this pass — the stand-in transport answered");

console.log("web push test-session tests passed");
process.exit(0);
