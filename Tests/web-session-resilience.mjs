import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import { createTranscriptEventRouter, createTranscriptRevisionObserver } from
    "../Resources/web/app/js/session/transcript-requests.js";
const routedTranscriptEvents = [];
const reconnectedTranscripts = [];
let routedOpenID = "open-session";
const routeTranscriptEvent = createTranscriptEventRouter(
    function () { return routedOpenID; },
    function (id, signature) { routedTranscriptEvents.push({ id, signature }); },
    function (id) { reconnectedTranscripts.push(id); }
);
assert.equal(routeTranscriptEvent({ type: "hello", data: {} }), true,
    "a stream reconnect quietly catches transcript bytes missed while offline");
assert.deepEqual(reconnectedTranscripts, ["open-session"],
    "the reconnect catch-up is scoped to the transcript currently being read");
assert.equal(routeTranscriptEvent({
    type: "transcript-revision", data: { id: "other-session", signature: "11-20" }
}), false, "a file event for another session does not wake the open transcript");
assert.equal(routeTranscriptEvent({
    type: "transcript-revision", data: { id: "open-session", signature: "12-21" }
}), true, "a file event for the open session wakes its transcript immediately");
assert.deepEqual(routedTranscriptEvents,
    [{ id: "open-session", signature: "12-21" }],
    "the file signature is kept as a revision independent from session snapshots");
routedOpenID = null;
assert.equal(routeTranscriptEvent({
    type: "transcript-revision", data: { id: "open-session", signature: "13-22" }
}), false, "closing the transcript makes late file events harmless");

function revisionHarness(maxAttempts = 3) {
    const loads = [];
    const timers = [];
    const observer = createTranscriptRevisionObserver(function (id, revision, quiet) {
        loads.push({ id, revision, quiet });
    }, {
        maxAttempts,
        retryDelay: 25,
        schedule: function (fn, delay) {
            const timer = { fn, delay, cancelled: false };
            timers.push(timer);
            return timer;
        },
        cancel: function (timer) { timer.cancelled = true; }
    });
    return { observer, loads, timers };
}

const recoveredRevision = revisionHarness();
recoveredRevision.observer.observe("A", "rev2", true);
assert.deepEqual(recoveredRevision.loads, [{ id: "A", revision: "rev2", quiet: true }],
    "a new session revision starts one transcript GET");
recoveredRevision.observer.settle("A", "rev2", false);
assert.equal(recoveredRevision.timers.length, 1,
    "a failed latest-revision GET schedules one bounded recovery read");
recoveredRevision.observer.observe("A", "rev2", true);
assert.equal(recoveredRevision.loads.length, 1,
    "a reconnect carrying the same revision does not create a parallel read");
recoveredRevision.timers[0].fn();
assert.equal(recoveredRevision.loads.length, 2,
    "the bounded recovery read still runs when reconnect repeats the same revision");
recoveredRevision.observer.settle("A", "rev2", true);
recoveredRevision.observer.observe("A", "rev2", true);
assert.equal(recoveredRevision.loads.length, 2,
    "a successfully observed revision is distinct from, and catches up to, the seen snapshot");

const revertedWhileActive = revisionHarness();
revertedWhileActive.observer.observe("ABA-active", "A", true);
revertedWhileActive.observer.settle("ABA-active", "A", true);
revertedWhileActive.observer.observe("ABA-active", "B", true);
revertedWhileActive.observer.observe("ABA-active", "A", true);
assert.deepEqual(revertedWhileActive.loads.map(function (load) { return load.revision; }),
    ["A", "B", "A"],
    "A to B to A while B is active keeps the final A as trailing transcript demand");

const revertedWhileRetrying = revisionHarness();
revertedWhileRetrying.observer.observe("ABA-retry", "A", true);
revertedWhileRetrying.observer.settle("ABA-retry", "A", true);
revertedWhileRetrying.observer.observe("ABA-retry", "B", true);
revertedWhileRetrying.observer.settle("ABA-retry", "B", false);
revertedWhileRetrying.observer.observe("ABA-retry", "A", true);
assert.equal(revertedWhileRetrying.timers[0].cancelled, true,
    "returning to A cancels B's pending recovery timer");
assert.deepEqual(revertedWhileRetrying.loads.map(function (load) { return load.revision; }),
    ["A", "B", "A"],
    "A to B to A while B is retry-pending still fetches the final A");

const boundedRevision = revisionHarness(3);
boundedRevision.observer.observe("B", "rev9", true);
for (let attempt = 0; attempt < 3; attempt += 1) {
    boundedRevision.observer.settle("B", "rev9", false);
    const timer = boundedRevision.timers[attempt];
    if (timer) timer.fn();
}
assert.equal(boundedRevision.loads.length, 3,
    "a permanently failing revision is attempted only up to the configured bound");
assert.equal(boundedRevision.timers.length, 2,
    "the final failed attempt does not leave an unbounded retry timer");
for (let replay = 0; replay < 5; replay += 1) {
    boundedRevision.observer.observe("B", "rev9", true);
}
assert.equal(boundedRevision.loads.length, 3,
    "ordinary repeated snapshots cannot restart an exhausted failure burst");
boundedRevision.observer.rearm("B", "rev9", true);
assert.equal(boundedRevision.loads.length, 4,
    "a real reconnect rearms one unobserved exhausted revision without a parallel GET");
boundedRevision.observer.settle("B", "rev9", true);
boundedRevision.observer.observe("B", "rev9", true);
boundedRevision.observer.rearm("B", "rev9", true);
assert.equal(boundedRevision.loads.length, 4,
    "snapshots and reconnects do not refetch a revision after recovery observed it");

const stoppedRevision = revisionHarness();
stoppedRevision.observer.observe("C", "rev3", true);
stoppedRevision.observer.settle("C", "rev3", false);
stoppedRevision.observer.stop("C");
assert.equal(stoppedRevision.timers[0].cancelled, true,
    "closing a session cancels its pending recovery read");
stoppedRevision.timers[0].fn();
assert.equal(stoppedRevision.loads.length, 1,
    "a cancelled recovery callback cannot restart transcript reads");

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
function testElement(tag = "div") {
    const classes = new Set();
    const children = [];
    const descendants = new Map();
    const attributes = new Map();
    const target = {
        tagName: tag.toUpperCase(), children, childNodes: children, style: {}, dataset: {},
        hidden: false, disabled: false, value: "", textContent: "", className: "",
        scrollHeight: 0, scrollTop: 0, clientHeight: 0, parentNode: null,
        appendChild: function (child) { child.parentNode = proxy; children.push(child); return child; },
        removeChild: function (child) {
            const at = children.indexOf(child); if (at >= 0) children.splice(at, 1);
            child.parentNode = null; return child;
        },
        setAttribute: function (name, value) { attributes.set(name, String(value)); },
        getAttribute: function (name) { return attributes.get(name) ?? null; },
        toggleAttribute: function (name, force) {
            const on = force === undefined ? !attributes.has(name) : !!force;
            if (on) attributes.set(name, ""); else attributes.delete(name); return on;
        },
        addEventListener: function (name, fn) { target["on" + name] = fn; },
        querySelector: function (selector) {
            if (!descendants.has(selector)) descendants.set(selector, testElement(
                selector === "canvas" || selector.includes("spin") || selector.includes("mark")
                    ? "canvas" : "span"));
            return descendants.get(selector);
        },
        querySelectorAll: function () { return []; },
        closest: function () { return proxy; },
        focus: noop,
        animate: function () { return { cancel: noop, onfinish: null }; },
        getBoundingClientRect: function () {
            return { top: 0, left: 0, width: 0, height: 0, bottom: 0, right: 0 };
        },
        getContext: function () {
            return { clearRect: noop, fillRect: noop, beginPath: noop, moveTo: noop,
                lineTo: noop, stroke: noop, save: noop, restore: noop,
                imageSmoothingEnabled: false, fillStyle: "", strokeStyle: "" };
        }
    };
    Object.defineProperty(target, "innerHTML", {
        get: function () { return target._innerHTML || ""; },
        set: function (value) { target._innerHTML = value; children.splice(0); descendants.clear(); }
    });
    target.classList = {
        add: function (...names) { names.forEach(function (name) { classes.add(name); }); },
        remove: function (...names) { names.forEach(function (name) { classes.delete(name); }); },
        toggle: function (name, force) {
            const on = force === undefined ? !classes.has(name) : !!force;
            if (on) classes.add(name); else classes.delete(name); return on;
        },
        contains: function (name) { return classes.has(name); }
    };
    const proxy = new Proxy(target, {
        get: function (object, key) {
            if (key === Symbol.iterator) return function* () { yield* children; };
            if (key === "content") return { cloneNode: function () { return testElement(); } };
            if (key in object) return object[key];
            return undefined;
        }
    });
    return proxy;
}

if (process.env.CLAWDLINE_START_SHEET_BEHAVIOR === "1") {
    const root = testElement();
    const elements = new Map();
    function elementWithID(id) {
        if (!elements.has(id)) {
            elements.set(id, testElement(id.includes("filter") ? "input" : "div"));
        }
        return elements.get(id);
    }
    globalThis.localStorage = { getItem: function () { return null; }, setItem: noop };
    globalThis.location = { search: "", protocol: "http:", hostname: "localhost", pathname: "/" };
    globalThis.history = { replaceState: noop };
    globalThis.getComputedStyle = function () { return { opacity: "1", marginLeft: "0" }; };
    Object.defineProperty(globalThis, "navigator", {
        value: { userAgent: "node", maxTouchPoints: 0 }, configurable: true
    });
    globalThis.window = root;
    window.devicePixelRatio = 1;
    window.innerHeight = 800;
    window.visualViewport = { height: 800, offsetTop: 0, addEventListener: noop };
    window.matchMedia = function () { return { matches: false, addEventListener: noop }; };
    globalThis.document = root;
    document.documentElement = { lang: "en", style: { setProperty: noop, removeProperty: noop } };
    document.getElementById = elementWithID;
    document.querySelector = function () { return root; };
    document.querySelectorAll = function () { return []; };
    document.createElement = function (tag) { return testElement(tag); };
    document.body = root;
    globalThis.MutationObserver = class { observe() { } disconnect() { } };
    globalThis.ResizeObserver = MutationObserver;
    globalThis.IntersectionObserver = MutationObserver;

    const { useApi } = await import("../Resources/web/app/js/net/api.js");
    const { S } = await import("../Resources/web/app/js/core/state.js");
    const { Start } = await import("../Resources/web/app/js/input/start.js");
    const calls = [];
    useApi({
        places: function () { return Promise.resolve({
            places: [{ id: "place-one", path: "/repo/one", label: "one" }],
            assistants: [{ id: "claude", label: "Claude" }, { id: "codex", label: "Codex" }]
        }); },
        pastSessions: function (place, assistant) {
            calls.push(["pastSessions", place, assistant]);
            return Promise.resolve({ sessions: [
                { id: "thread-one", title: "Earlier work", at: 1, live: false }
            ] });
        },
        resumePlace: function (place, sessionID, assistant) {
            calls.push(["resumePlace", place, sessionID, assistant]);
            return new Promise(function () { });
        },
        startPlace: function () { throw new Error("fresh start was not expected"); }
    });
    S.write = true;
    Start.open();
    await new Promise(function (resolve) { setTimeout(resolve, 0); });

    const codexChip = elementWithID("start-with").children.find(function (node) {
        return node.textContent === "Codex";
    });
    assert.ok(codexChip, "the start sheet offers the Codex assistant");
    codexChip.onclick();
    const resumeChip = elementWithID("start-resume").children[0];
    assert.equal(resumeChip.disabled, false,
        "selecting Codex enables the resume control");
    resumeChip.onclick();
    Start.press("place-one");
    assert.deepEqual(calls[0], ["pastSessions", "place-one", "codex"],
        "selecting a project asks for that project's Codex history");
    await new Promise(function (resolve) { setTimeout(resolve, 0); });
    Start.pick("thread-one");
    assert.deepEqual(calls[1], ["resumePlace", "place-one", "thread-one", "codex"],
        "selecting a non-live Codex row resumes it with Codex");
    console.log("web start sheet Codex resume behavior passed");
    process.exit(0);
}

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

const requestPaths = [];
const resilienceFetch = globalThis.fetch;
globalThis.fetch = function (path) {
    requestPaths.push(path);
    return Promise.resolve({ ok: true, text: function () {
        return Promise.resolve(JSON.stringify({ sessions: [] }));
    } });
};
await Live.pastSessions("place/one", "codex");
await Live.resumePlace("place/one", "thread/two", "codex");
assert.equal(requestPaths[0], "/v1/places/place%2Fone/sessions/codex",
    "Codex history names the selected assistant in the read route");
assert.equal(requestPaths[1], "/v1/places/place%2Fone/resume/codex/thread%2Ftwo",
    "Codex resume names the selected assistant in the write route");
globalThis.fetch = resilienceFetch;

const startSheet = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
    cwd: process.cwd(), encoding: "utf8",
    env: { ...process.env, CLAWDLINE_START_SHEET_BEHAVIOR: "1" }
});
assert.equal(startSheet.status, 0,
    "the isolated start-sheet behavior fixture passes: " + (startSheet.stderr || startSheet.stdout));
assert.match(startSheet.stdout, /Codex resume behavior passed/,
    "the behavior fixture reached all three Codex resume assertions");

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
const transportEvents = [];
const stopTransportEvents = Live.events(function (event) { transportEvents.push(event); });
source.listeners.transcript({ data: JSON.stringify({ id: "B", signature: "24-30" }) });
assert.deepEqual(transportEvents.map(function (event) {
    return { type: event.type, id: event.data.id, signature: event.data.signature };
}), [{ type: "transcript-revision", id: "B", signature: "24-30" }],
"the SSE transcript frame crosses the transport-neutral event lane without conversation bytes");
stopTransportEvents();
if (source.onopen) source.onopen();
assert.equal(S.conn, "connecting", "EventSource onopen alone does not claim the feed is live");
source.listeners.sessions({ data: JSON.stringify({
    sessions: [{ id: "B" }], at: 3,
    scan: { generation: 9, complete: true, emptyAuthoritative: false }
}) });
assert.equal(S.conn, "live", "a parsed sessions payload proves the feed is live");

console.log("web session resilience tests passed");
process.exit(0);
