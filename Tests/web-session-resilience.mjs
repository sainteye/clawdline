import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const script = fs.readFileSync(new URL("../Resources/iterm.js", import.meta.url), "utf8");

function jxaList(app) {
    const context = {
        Application: function () { return app; },
        args: ["list"],
        result: null
    };
    vm.runInNewContext(script + "\nresult = run(args);", context);
    return JSON.parse(context.result);
}

function session(id) {
    return {
        id: function () { return id; },
        name: function () { return "task " + id; },
        tty: function () { return "/dev/ttys" + id; },
        profileName: function () { return "Default"; }
    };
}

const partial = jxaList({
    running: function () { return true; },
    windows: function () {
        return [
            { tabs: function () { throw new Error("window changing"); } },
            { tabs: function () { return [{ sessions: function () { return [session("B")]; } }]; } }
        ];
    }
});
assert.equal(partial.complete, false, "a skipped window makes the inventory incomplete");
assert.equal(partial.ok, false, "an incomplete inventory is not advertised as a clean success");
assert.deepEqual(partial.sessions.map(function (s) { return s.id; }), ["B"],
    "rows from readable windows survive a partial scan");
assert.match(partial.error, /incomplete/i, "the partial result explains its confidence");

const failed = jxaList({
    running: function () { return true; },
    windows: function () { throw new Error("bridge unavailable"); }
});
assert.equal(failed.complete, false, "an unreadable window list is incomplete");
assert.equal(failed.ok, false, "an all-failed enumeration cannot look like a successful empty list");
assert.deepEqual(failed.sessions, [], "an all-failed result still has a stable sessions shape");

const empty = jxaList({
    running: function () { return true; },
    windows: function () { return []; }
});
assert.equal(empty.complete, true, "a readable empty window list is authoritative");
assert.equal(empty.ok, true, "confirmed absence is a successful result");
assert.equal(empty.appRunning, true, "a clean empty list says iTerm itself was observed running");

const stopped = jxaList({ running: function () { return false; } });
assert.equal(stopped.appRunning, false,
    "a stopped-app observation is explicit so Swift can compare it with ps");
assert.equal(stopped.complete, true,
    "a stopped app is authoritative only until independent process evidence contradicts it");

const noop = function () { };
const element = new Proxy(function () { }, {
    get: function (_target, key) {
        if (key === Symbol.iterator) return function* () { };
        if (key === "classList") return { add: noop, remove: noop, toggle: noop,
            contains: function () { return false; } };
        if (key === "style" || key === "dataset") return {};
        if (key === "children" || key === "querySelectorAll") return [];
        if (key === "getBoundingClientRect") return function () {
            return { top: 0, left: 0, width: 0, height: 0, bottom: 0, right: 0 };
        };
        if (key === "content") return { cloneNode: function () { return element; } };
        return element;
    },
    apply: function () { return element; }
});

globalThis.localStorage = { getItem: function () { return null; }, setItem: noop };
globalThis.location = { search: "", protocol: "http:", hostname: "localhost", pathname: "/" };
globalThis.history = { replaceState: noop };
Object.defineProperty(globalThis, "navigator", {
    value: { userAgent: "node", maxTouchPoints: 0 }, configurable: true
});
globalThis.window = element;
window.devicePixelRatio = 1;
window.matchMedia = function () { return { matches: false, addEventListener: noop }; };
globalThis.document = element;
document.documentElement = { lang: "en" };
document.getElementById = function () { return element; };
document.querySelector = function () { return element; };
document.querySelectorAll = function () { return []; };
document.createElement = function () { return element; };
document.body = element;
globalThis.MutationObserver = class { observe() { } disconnect() { } };
globalThis.ResizeObserver = MutationObserver;
globalThis.IntersectionObserver = MutationObserver;

let fetchCalls = 0;
globalThis.fetch = function () {
    fetchCalls += 1;
    return Promise.resolve({
        ok: true,
        text: function () { return Promise.resolve(JSON.stringify({
            sessions: [], at: 2,
            scan: { generation: 7, complete: false, emptyAuthoritative: true }
        })); }
    });
};

const { handlers, sessionListNeedsConfirmation } =
    await import("../Resources/web/app/js/net/handlers.js");
const { Live } = await import("../Resources/web/app/js/net/live.js");
const { S } = await import("../Resources/web/app/js/core/state.js");

const incomplete = { generation: 7, complete: false, emptyAuthoritative: false };
const authoritative = { generation: 8, complete: true, emptyAuthoritative: true };
assert.equal(sessionListNeedsConfirmation([], "A", [{ id: "A" }], incomplete), true,
    "one incomplete empty inventory cannot close the chat being read");
assert.equal(sessionListNeedsConfirmation([], "A", [{ id: "A" }], authoritative), false,
    "an authoritative empty inventory may close it");
assert.equal(sessionListNeedsConfirmation([{ id: "B" }], "A", [{ id: "A" }], incomplete), false,
    "ordinary non-empty replacements are not delayed");

S.sessions = [{ id: "A" }];
S.openId = "A";
assert.equal(handlers.sessions([], 1, incomplete), false,
    "the handler refuses an incomplete empty frame");
assert.deepEqual(S.sessions, [{ id: "A" }], "refusing the frame preserves last-known-good state");

assert.equal(await Live.receiveSessions({ sessions: [], at: 1, scan: incomplete }), false,
    "a REST echo from the same scan generation is not independent confirmation");
assert.equal(fetchCalls, 1, "an untrusted destructive frame asks once for fresher evidence");
assert.deepEqual(S.sessions, [{ id: "A" }], "same-generation REST evidence keeps the chat open");

assert.equal(await Live.receiveSessions({ sessions: [], at: 2, scan: authoritative }), true,
    "a newer authoritative scan closes a genuinely absent session");
assert.deepEqual(S.sessions, [], "the authoritative empty inventory is accepted");

let source;
globalThis.EventSource = class {
    constructor() { this.listeners = {}; source = this; }
    addEventListener(name, fn) { this.listeners[name] = fn; }
    close() { }
};
Live.connect();
assert.equal(S.conn, "connecting", "opening the socket is not yet a live session feed");
if (source.onopen) source.onopen();
assert.equal(S.conn, "connecting", "EventSource onopen alone does not claim the feed is live");
source.listeners.sessions({ data: JSON.stringify({
    sessions: [{ id: "B" }], at: 3,
    scan: { generation: 9, complete: true, emptyAuthoritative: false }
}) });
assert.equal(S.conn, "live", "a parsed sessions payload proves the feed is live");

console.log("web session resilience tests passed");
process.exit(0);
