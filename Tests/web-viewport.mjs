import assert from "node:assert/strict";

const listeners = new Map();
const viewportListeners = new Map();
const frames = [];
const properties = new Map();

function listen(target, name, fn) {
    if (!target.has(name)) target.set(name, []);
    target.get(name).push(fn);
}

const input = {
    tagName: "INPUT",
    blurCalls: 0,
    blur: function () { this.blurCalls += 1; }
};
const body = { tagName: "BODY" };
const root = {
    style: {
        setProperty: function (name, value) { properties.set(name, value); },
        removeProperty: function (name) { properties.delete(name); }
    }
};

globalThis.location = { search: "", protocol: "http:", hostname: "localhost" };
globalThis.window = {
    innerHeight: 800,
    matchMedia: function () { return { matches: false }; },
    visualViewport: {
        height: 420,
        offsetTop: 0,
        addEventListener: function (name, fn) { listen(viewportListeners, name, fn); }
    },
    addEventListener: function (name, fn) { listen(listeners, name, fn); }
};
globalThis.document = {
    hidden: false,
    activeElement: input,
    body: body,
    documentElement: root,
    addEventListener: function (name, fn) { listen(listeners, name, fn); }
};
globalThis.requestAnimationFrame = function (fn) { frames.push(fn); };

const { releaseKeyboardFocus } = await import("../Resources/web/app/js/core/env.js");

assert.equal(properties.get("--vvh"), "420px",
    "a focused editor with a keyboard-sized viewport installs the override");

// WebKit announces focusout while the old field may still be document.activeElement. The event
// means the keyboard is leaving, so preserving or immediately rewriting its old height can leave
// the entire app clipped to that stale viewport if the next animation frame never arrives.
for (const fn of listeners.get("focusout") || []) fn();
assert.equal(properties.has("--vvh"), false,
    "focusout synchronously clears the stale visible-viewport override");
assert.equal(properties.has("--vvt"), false,
    "focusout synchronously clears the stale visible-viewport offset");

releaseKeyboardFocus();
assert.equal(input.blurCalls, 1,
    "opening a phone chat can explicitly release a filter or composer that still owns focus");

document.activeElement = body;
while (frames.length) frames.shift()();
assert.equal(properties.has("--vvh"), false,
    "the deferred measurement keeps the full page after focus reaches the body");

console.log("web viewport recovery tests passed");
