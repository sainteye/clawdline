/**
 * A press on the start sheet always ends.
 *
 * Reproduced in the hosted console on 2026-09-13: a press on a project said "Starting…" on its
 * row for ever. `CloudClient._place` threw synchronously when its route table had no entry for
 * the row, `press()` had no try around `api.startPlace`, and the exception left `pressing` set —
 * so no read timeout ran, no sentence was drawn, every other row stayed shut and Close did
 * nothing until the page was reloaded. The table was empty at three ordinary moments while the
 * sheet went on showing the rows it had: a re-read in flight (`places()` cleared it first), a
 * token renewal (the new client started with an empty table), and a places read that failed.
 *
 * Two layers, and each is held on its own here, because either one alone still leaves a hang
 * behind the other:
 *
 * 1. The transport. Every `CloudClient` method a page treats as a promise answers a failure as a
 *    rejected promise — found by enumeration, so a method added later is held to it by default —
 *    and the place routes the page was handed outlive a re-read, a renewal and a failed read.
 * 2. The sheet. Whatever the transport does, `load`, `press`, `enter` and `pick` reach their
 *    settle: the in-flight flag is cleared, the refusal is said, and Close closes.
 *
 * The sheet itself needs a DOM, so each sheet scenario runs in a child process of this file
 * against the smallest stub that holds `start.js` (the technique of
 * `Tests/web-session-resilience.mjs`), one process per scenario so a stuck flag in one cannot
 * decide the next.
 */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const SCENARIO = process.env.CLAWDLINE_START_SHEET_FAILURE || "";

/* ---- a relay in a box ------------------------------------------------------ */

const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");
const {
    importDevicePrivateKey, importMasterSecret, importSenderPublicKey, openEnvelope, sealEnvelope
} = await import("../Resources/web/app/js/net/cloud-crypto.js");

const vectors = JSON.parse(await readFile(new URL("./protocol-vectors.json", import.meta.url), "utf8"));
const masterKey = await importMasterSecret(vectors.master_secret);
const senderKey = await importSenderPublicKey(vectors.ed25519_public_key);
const signingKey = await importDevicePrivateKey(Buffer.concat([
    Buffer.from("302e020100300506032b657004220420", "hex"),
    Buffer.from(vectors.ed25519_seed, "base64")
]));
const ACCOUNT = "account-01";
const DEVICE = "device-vector-01";
const MACHINE_REPLY = "__clawdline_machine__";

class FakeWebSocket {
    static latest = null;
    constructor() { this.readyState = 1; this.sent = []; this.opened = 0; FakeWebSocket.latest = this; }
    send(text) { this.sent.push(JSON.parse(text)); }
    close() { this.readyState = 3; if (this.onclose) this.onclose(); }
    receive(frame) { this.onmessage({ data: JSON.stringify(frame) }); }
}

/** The read timers, so a test can make the Mac's silence arrive now instead of in sixty seconds. */
const readTimers = [];
let outboundSequence = 100;
function cloudClient(options) {
    return new CloudClient(Object.assign({
        relayURL: "https://relay.example", deviceToken: "jwt", devicePrivateKey: signingKey,
        masterKey: masterKey, senderKeys: { [DEVICE]: senderKey }, WebSocket: FakeWebSocket,
        account: ACCOUNT, deviceID: DEVICE,
        allowWrites: true, nextSequence: function () { return Promise.resolve(outboundSequence++); },
        setTimeout: function (fn) { readTimers.push(fn); return readTimers.length; },
        clearTimeout: function () { }
    }, options));
}

async function ready(client) {
    await client.start();
    const socket = FakeWebSocket.latest;
    socket.receive({ type: "challenge", v: 1, context: "clawdline-challenge-v1", account: ACCOUNT,
        device: DEVICE, challenge: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
        expires_in_ms: 30_000 });
    await client.messageChain;
    socket.receive({ type: "ready", v: 1, role: "viewer", account: ACCOUNT, device: DEVICE });
    await client.messageChain;
    return socket;
}

/** Wall-clock, not event-loop turns: the sheet's wait holds a shown spinner for 320ms. */
async function until(predicate, what) {
    const deadline = Date.now() + 3000;
    while (Date.now() < deadline) {
        if (predicate()) return;
        await new Promise(function (resolve) { setTimeout(resolve, 5); });
    }
    assert.fail("timed out waiting for " + what);
}

function published(socket) {
    return socket.sent.filter(function (frame) { return frame.type === "publish"; });
}

/** The next command this socket put on the wire, opened, with the channel it went to. */
async function nextCommand(socket, type) {
    const cursor = socket.opened;
    await until(function () { return published(socket).length > cursor; },
        "a " + type + " command to reach the relay");
    socket.opened = cursor + 1;
    const envelope = published(socket)[cursor].envelope;
    const command = JSON.parse(new TextDecoder().decode(
        await openEnvelope(envelope, masterKey, senderKey)));
    command.ch = envelope.ch;
    assert.equal(command.type, type, "the next command on the wire is " + type);
    return command;
}

let answerSequence = 5000;
async function answer(client, socket, machine, payload) {
    const envelope = await sealEnvelope({
        ch: "t/" + machine + "/" + MACHINE_REPLY, seq: ++answerSequence, ts: 1787817600000,
        class: "stream", key_id: "ms-1", sender: DEVICE
    }, JSON.stringify(payload), masterKey, signingKey);
    socket.receive({ type: "envelope", envelope: envelope });
    await client.messageChain;
}

function placesBody(places) {
    return { places: places, assistants: [{ id: "claude", label: "Claude" }] };
}

const PORTFOLIO = { id: "portfolio", path: "/code/app", label: "App" };
const CLOCK_REFUSAL = { code: "command_clock_uncertain",
    message: "the Mac cannot yet vouch for this command's clock" };

if (SCENARIO) {
    await sheetScenario(SCENARIO);
    process.exit(0);
}

const failures = [];
async function check(name, run) {
    try { await run(); }
    catch (error) { failures.push(name + "\n    " + String(error && error.message).split("\n").join("\n    ")); }
}

/* ---- 1. no promise-shaped method throws at its caller ----------------------------------- */

// Called for their synchronous effect and never as a promise. Everything else on the prototype is
// found by enumeration, so a method added tomorrow is held to the rule without anyone listing it.
const SYNCHRONOUS = new Set(["constructor", "events", "subscribe", "stop", "retire"]);
// Snapshot reads with no failure to reach; every other method must be seen to reject at least
// once below, or the inputs never walked the path this guard exists for.
const CANNOT_FAIL = new Set(["sessions", "tasks"]);

await check("every promise-shaped CloudClient method rejects instead of throwing", async function () {
    class RefusedSocket { constructor() { throw new Error("no relay in this test"); } }
    const empty = cloudClient({ WebSocket: RefusedSocket });
    const fleet = cloudClient({ WebSocket: RefusedSocket });
    // Two Macs publishing the same terminal id, and a fleet with no single Mac to guess.
    for (const machine of ["mac-01", "mac-02"]) {
        fleet.orchestratorSnapshots.set(machine, { tasks: [] });
        fleet.sessionSnapshots.set(machine + "\u0000twin", { id: "twin", machine: machine,
            session: "twin", identity: { machine: machine, session: "twin" } });
    }
    const inputs = [
        [],
        ["nobody", "nobody", "nobody"],
        ["twin", "twin", "twin"],
        [{ id: "never-read" }, "", ""],
        ["project", "item", "report", "gone"],
        ["", "", "report"],
        [null, null, null, null, null, null, "gone"]
    ];
    const methods = Object.getOwnPropertyNames(CloudClient.prototype).filter(function (name) {
        return !name.startsWith("_") && !SYNCHRONOUS.has(name)
            && typeof CloudClient.prototype[name] === "function";
    });
    assert.ok(methods.includes("startPlace") && methods.includes("transcript"),
        "the enumeration reaches the methods this file is about");
    const threw = new Map(), notPromises = [], neverRejected = [], hung = [];
    for (const name of methods) {
        let rejected = false;
        for (const [label, client] of [["empty", empty], ["fleet", fleet]]) {
            for (const args of inputs) {
                const call = name + "(" + args.map(function (value) {
                    return JSON.stringify(value);
                }).join(", ") + ") on the " + label + " client";
                let result;
                try { result = client[name].apply(client, args); }
                catch (error) {
                    threw.set(name, (threw.get(name) || new Set()).add(error.code || error.name));
                    continue;
                }
                if (!result || typeof result.then !== "function") {
                    notPromises.push(call);
                    continue;
                }
                const outcome = await Promise.race([
                    result.then(function () { return "resolved"; }, function () { return "rejected"; }),
                    new Promise(function (resolve) { setTimeout(resolve, 1000, "hung"); })
                ]);
                if (outcome === "rejected") rejected = true;
                if (outcome === "hung") hung.push(call);
            }
        }
        if (!rejected && !CANNOT_FAIL.has(name)) neverRejected.push(name);
    }
    assert.deepEqual(Array.from(threw, function ([name, codes]) {
        return name + " threw " + Array.from(codes).join("/");
    }), [], "a synchronous throw skips the caller's catch and its settle");
    assert.deepEqual(notPromises, [], "every promise-shaped method returns a promise");
    assert.deepEqual(hung, [], "no failure is left unsettled");
    assert.deepEqual(neverRejected, [],
        "each method was driven into a failure at least once, so the guard is not vacuous");
});

/* ---- 2. the routes a page was handed outlive a re-read, a renewal and a failed read ------- */

async function listedClient(machines) {
    const client = cloudClient();
    const socket = await ready(client);
    machines.forEach(function (machine) { client.orchestratorSnapshots.set(machine, { tasks: [] }); });
    const reading = client.places();
    for (let asked = 0; asked < machines.length; asked += 1) {
        const read = await nextCommand(socket, "places");
        await answer(client, socket, read.ch.slice("ctl/".length), { read: "read:" + read.request,
            status: 200, body: placesBody([PORTFOLIO]) });
    }
    const listed = await reading;
    const ids = {};
    listed.places.forEach(function (place) { ids[place.machine] = place.id; });
    return { client, socket, ids };
}

async function startsOn(client, socket, id, machine, what) {
    let started;
    try { started = client.startPlace(id, "claude"); }
    catch (error) { assert.fail(what + ": startPlace threw " + error.code + " — " + error.message); }
    started.catch(function () { });
    const command = await nextCommand(socket, "start");
    assert.deepEqual({ ch: command.ch, place: command.place },
        { ch: "ctl/" + machine, place: "portfolio" },
        what + ": the press reaches the Mac that listed the project, under its own id");
}

await check("(a) transport: a re-read in flight keeps the routes", async function () {
    const { client, socket, ids } = await listedClient(["mac-01"]);
    client.places().catch(function () { });
    await nextCommand(socket, "places");
    await startsOn(client, socket, ids["mac-01"], "mac-01", "during a re-read");
});

await check("(b) transport: a token renewal carries the routes to the new client", async function () {
    const { client, ids } = await listedClient(["mac-01"]);
    const renewed = cloudClient({ deviceToken: "jwt-2", resumeFrom: client });
    const renewedSocket = await ready(renewed);
    client.retire();
    await startsOn(renewed, renewedSocket, ids["mac-01"], "mac-01", "after renewal");

    // The carry is bounded by the same viewer the session rows are: another account's client
    // inherits nothing, and says so as a rejection rather than a throw.
    const stranger = cloudClient({ account: "account-02", resumeFrom: client });
    let refused;
    try { refused = stranger.startPlace(ids["mac-01"], "claude"); }
    catch (error) { assert.fail("another account's client threw " + error.code); }
    await assert.rejects(refused, function (error) { return error.code === "not_found"; },
        "another account does not inherit this viewer's routes");
});

await check("(c) transport: a failed or unanswered read keeps every Mac's routes", async function () {
    const { client, socket, ids } = await listedClient(["mac-01", "mac-02"]);
    // mac-01 answers, mac-02 refuses: the read fails as a whole and mac-01's routes stay too.
    const refusedRead = client.places();
    for (const read of [await nextCommand(socket, "places"), await nextCommand(socket, "places")]) {
        await answer(client, socket, read.ch.slice("ctl/".length), read.ch === "ctl/mac-01"
            ? { read: "read:" + read.request, status: 200, body: placesBody([PORTFOLIO]) }
            : { read: "read:" + read.request, status: 503,
                error: { code: "reading_busy", message: "busy" } });
    }
    await assert.rejects(refusedRead, function (error) { return error.code === "reading_busy"; });
    await startsOn(client, socket, ids["mac-02"], "mac-02", "after a refused read");
    await startsOn(client, socket, ids["mac-01"], "mac-01", "beside a refused read");

    // Nobody answers at all: the read times out and the routes are still there.
    const silentRead = client.places();
    await nextCommand(socket, "places");
    await nextCommand(socket, "places");
    readTimers.splice(0).forEach(function (fire) { fire(); });
    await assert.rejects(silentRead, function (error) { return error.code === "cloud_read_timeout"; });
    await startsOn(client, socket, ids["mac-01"], "mac-01", "after a timed-out read");
});

await check("transport: a complete answer replaces the routes, so a dropped project has none",
            async function () {
    const { client, socket, ids } = await listedClient(["mac-01"]);
    const rereading = client.places();
    const read = await nextCommand(socket, "places");
    await answer(client, socket, "mac-01", { read: "read:" + read.request, status: 200,
        body: placesBody([{ id: "other", path: "/code/other", label: "Other" }]) });
    const fresh = await rereading;
    let gone;
    try { gone = client.startPlace(ids["mac-01"], "claude"); }
    catch (error) { assert.fail("a dropped project threw " + error.code); }
    await assert.rejects(gone, function (error) { return error.code === "not_found"; },
        "a project the Mac no longer lists is refused here, not retained for ever");
    let other;
    try { other = client.startPlace(fresh.places[0].id, "claude"); }
    catch (error) { assert.fail("the fresh project threw " + error.code); }
    other.catch(function () { });
    const command = await nextCommand(socket, "start");
    assert.equal(command.place, "other", "and the project it lists now is routed");
});

/* ---- 3. the sheet settles, whatever happened below it ----------------------------------- */

const SCENARIOS = {
    "reread": "(a) sheet: press an old row while the re-read is in flight",
    "renewal": "(b) sheet: press an old row after a token renewal replaced the client",
    "failed-read": "(c) sheet: press an old row after the places read timed out",
    "refused": "(d) sheet: the Mac refuses with 503 — the error shows, the row presses again, Close closes",
    "sync-throw": "sheet: a transport that throws synchronously still settles load, press, enter and pick"
};
for (const [scenario, name] of Object.entries(SCENARIOS)) {
    await check(name, async function () {
        const run = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
            cwd: process.cwd(), encoding: "utf8", timeout: 60_000,
            env: { ...process.env, CLAWDLINE_START_SHEET_FAILURE: scenario }
        });
        assert.equal(run.status, 0, (run.stderr || run.stdout || String(run.error)).trim());
        assert.match(run.stdout, new RegExp("start sheet scenario " + scenario + " passed"));
    });
}

assert.deepEqual(failures, [], "start sheet failures:\n  " + failures.join("\n  "));
console.log("web start sheet failure tests passed");
process.exit(0);

/* ---- the sheet, in a child process ------------------------------------------------------ */

async function sheetScenario(scenario) {
    const noop = function () { };
    function testElement(tag = "div") {
        const classes = new Set();
        const children = [];
        const descendants = new Map();
        const attributes = new Map();
        const target = {
            tagName: tag.toUpperCase(), children, childNodes: children, style: {}, dataset: {},
            hidden: false, disabled: false, value: "", textContent: "", className: "",
            placeholder: "", title: "",
            scrollHeight: 0, scrollTop: 0, clientHeight: 0, parentNode: null,
            appendChild: function (child) { child.parentNode = proxy; children.push(child); return child; },
            insertBefore: function (child, before) {
                const at = before ? children.indexOf(before) : -1;
                child.parentNode = proxy;
                if (at >= 0) children.splice(at, 0, child); else children.push(child);
                return child;
            },
            removeChild: function (child) {
                const at = children.indexOf(child); if (at >= 0) children.splice(at, 1);
                child.parentNode = null; return child;
            },
            setAttribute: function (name, value) { attributes.set(name, String(value)); },
            getAttribute: function (name) { return attributes.get(name) ?? null; },
            removeAttribute: function (name) { attributes.delete(name); },
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

    const root = testElement();
    const elements = new Map();
    function elementWithID(id) {
        if (!elements.has(id)) elements.set(id, testElement(id.includes("filter") ? "input" : "div"));
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
    document.head = testElement("head");
    globalThis.MutationObserver = class { observe() { } disconnect() { } };
    globalThis.ResizeObserver = MutationObserver;
    globalThis.IntersectionObserver = MutationObserver;

    const { useApi } = await import("../Resources/web/app/js/net/api.js");
    const { S } = await import("../Resources/web/app/js/core/state.js");
    const { T } = await import("../Resources/web/app/js/core/i18n.js");
    const { bindSessionUI } = await import("../Resources/web/app/js/session/ui.js");
    const { Start } = await import("../Resources/web/app/js/input/start.js");
    // The spinner's show timer redraws the sheet through this seam, as `main.js` binds it.
    bindSessionUI({ syncStart: function () { Start.sync(); } });
    S.write = true;

    const said = function () { return elementWithID("start-said").textContent; };
    const say = function () { return elementWithID("start-say").textContent; };
    const rows = function () {
        return elementWithID("start-list").children.map(function (li) { return li.children[0]; })
            .filter(Boolean);
    };
    const settled = function () {
        return rows().every(function (row) { return !row.disabled && row.dataset.busy !== "1"; });
    };
    /** A press that throws has escaped the only code that could have cleared `pressing`. */
    function press(id) {
        try { Start.press(id); }
        catch (error) {
            assert.fail("Start.press threw at its caller instead of settling: "
                + (error.code ? error.code + " — " : "") + error.message);
        }
    }

    if (scenario === "sync-throw") {
        await syncThrowScenario();
        console.log("start sheet scenario " + scenario + " passed");
        return;
    }

    // Every Cloud scenario begins where the person was: the sheet open over a list the Mac sent.
    let client = cloudClient();
    let socket = await ready(client);
    client.orchestratorSnapshots.set("mac-01", { tasks: [] });
    useApi(client);
    Start.open();
    const firstRead = await nextCommand(socket, "places");
    await answer(client, socket, "mac-01", { read: "read:" + firstRead.request, status: 200,
        body: placesBody([PORTFOLIO]) });
    await until(function () { return rows().length === 1; }, "the project row to be drawn");
    const id = rows()[0].dataset.id;

    if (scenario === "reread") {
        Start.close();
        Start.open();
        const rereading = await nextCommand(socket, "places");
        press(id);
        const start = await nextCommand(socket, "start");
        assert.deepEqual({ ch: start.ch, place: start.place }, { ch: "ctl/mac-01", place: "portfolio" },
            "the old row's press reaches its Mac while the list is being read again");
        // The one thing a retained route can do wrong is name a project that has gone, and the
        // Mac is the authority on that: it answers not_found, and the sheet takes the list's
        // word over the row's.
        await answer(client, socket, "mac-01", { read: "action:" + start.request, status: 404,
            error: { code: "not_found", message: "No place named that" } });
        // The sheet's own sentence for the code, then the code and the ref it was sent under.
        await until(function () {
            return said().startsWith(T.webStartGone) && said().includes("not_found · ");
        }, "the gone project to be said with its code and ref. Said: " + said());
        await answer(client, socket, "mac-01", { read: "read:" + rereading.request, status: 200,
            body: placesBody([]) });
        await until(function () { return say() === T.webStartEmpty && rows().length === 0; },
            "the fresh list to replace the row that was gone. Say: " + say());
    } else if (scenario === "renewal") {
        const renewed = cloudClient({ deviceToken: "jwt-2", resumeFrom: client });
        const renewedSocket = await ready(renewed);
        useApi(renewed);
        client.retire();
        press(id);
        const start = await nextCommand(renewedSocket, "start");
        assert.deepEqual({ ch: start.ch, place: start.place }, { ch: "ctl/mac-01", place: "portfolio" },
            "the row drawn before the renewal starts through the client after it");
        assert.equal(rows()[0].dataset.busy, "1", "and the row says it is starting");
    } else if (scenario === "failed-read") {
        Start.close();
        Start.open();
        await nextCommand(socket, "places");
        readTimers.splice(0).forEach(function (fire) { fire(); });
        // A read nobody answered is said as that, by its code — no longer the sheet's catch-all.
        await until(function () {
            return said().startsWith(T.webFailNoAnswer) && said().includes("cloud_read_timeout · ");
        }, "the timed-out read to be said with its code and ref. Said: " + said());
        assert.equal(rows().length, 1, "the rows the Mac sent before stay on screen");
        press(id);
        const start = await nextCommand(socket, "start");
        assert.deepEqual({ ch: start.ch, place: start.place }, { ch: "ctl/mac-01", place: "portfolio" },
            "and pressing one still reaches its Mac");
    } else if (scenario === "refused") {
        // The ordinary path first, on the client that listed the row. Its press always settled,
        // but the sentence did not survive it: `draw()` wrote "" over `start-said` in the same
        // turn the refusal was written, so a refused start looked like a press that did nothing.
        press(id);
        let start = await nextCommand(socket, "start");
        await answer(client, socket, "mac-01", { read: "action:" + start.request, status: 503,
            error: CLOCK_REFUSAL });
        // `command_clock_uncertain` is said by its code, with the code and ref after it.
        const clockSaid = function () {
            return said().startsWith(T.webFailClock) && said().includes("command_clock_uncertain · ");
        };
        await until(function () { return clockSaid() && settled(); },
            "the control refusal to settle. Said: " + said());

        // The Mac refuses with 503 command_clock_uncertain in the minute after a token renewal,
        // which is exactly when the client has just been replaced.
        const renewed = cloudClient({ deviceToken: "jwt-2", resumeFrom: client });
        const renewedSocket = await ready(renewed);
        useApi(renewed);
        client.retire();
        client = renewed;
        socket = renewedSocket;
        for (const attempt of ["first", "second"]) {
            press(id);
            assert.equal(said(), "", "a press clears the last refusal before asking again");
            start = await nextCommand(socket, "start");
            await answer(client, socket, "mac-01", { read: "action:" + start.request, status: 503,
                error: CLOCK_REFUSAL });
            await until(function () { return clockSaid() && settled(); },
                "the " + attempt + " refusal after renewal to settle. Said: " + said());
        }
        Start.close();
        assert.equal(elementWithID("start").hidden, true, "Close closes the sheet after a refusal");
    } else {
        assert.fail("unknown scenario " + scenario);
    }
    console.log("start sheet scenario " + scenario + " passed");

    /** The sheet's own layer, with a transport that breaks the promise contract outright. */
    async function syncThrowScenario() {
        const exploded = function () { throw new TypeError("the transport threw"); };
        const calls = { places: 0 };
        let placesThrows = true;
        let pastThrows = true;
        useApi({
            places: function () {
                calls.places += 1;
                if (placesThrows) exploded();
                return Promise.resolve({ places: [{ id: "place-one", path: "/repo/one", label: "one" }],
                    assistants: [{ id: "claude", label: "Claude" }] });
            },
            startPlace: exploded,
            pastSessions: function () {
                if (pastThrows) exploded();
                return Promise.resolve({ sessions: [{ id: "thread-one", title: "Earlier", at: 1 }] });
            },
            resumePlace: exploded
        });

        // load
        try { Start.open(); }
        catch (error) { assert.fail("Start.open threw at its caller: " + error.message); }
        await until(function () { return said().startsWith(T.webStartFailed); },
            "a throwing places read to be said. Said: " + said());
        Start.close();
        placesThrows = false;
        Start.open();
        assert.equal(calls.places, 2, "the next opening asks again: the loading flag was cleared");
        await until(function () { return rows().length === 1; }, "the second read's row");

        // press
        press("place-one");
        await until(function () { return said().startsWith(T.webStartFailed) && settled(); },
            "a throwing start to settle. Said: " + said());
        Start.close();
        assert.equal(elementWithID("start").hidden, true, "Close closes after a throwing start");

        // enter
        Start.open();
        await until(function () { return rows().length === 1; }, "the row after reopening");
        elementWithID("start-resume").children[0].onclick();
        press("place-one");
        await until(function () { return said().startsWith(T.webStartFailed) && say() !== T.webLoading; },
            "a throwing history read to settle. Said: " + said() + " Say: " + say());

        // pick
        elementWithID("start-resume").children[0].onclick();   // back to the projects
        pastThrows = false;
        press("place-one");
        await until(function () { return rows().length === 1 && rows()[0].dataset.session; },
            "the conversation row");
        try { Start.pick("thread-one"); }
        catch (error) { assert.fail("Start.pick threw at its caller: " + error.message); }
        await until(function () { return said().startsWith(T.webStartFailed) && settled(); },
            "a throwing resume to settle. Said: " + said());
        Start.close();
        assert.equal(elementWithID("start").hidden, true, "Close closes after a throwing resume");
    }
}
