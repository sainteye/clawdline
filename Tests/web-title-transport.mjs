/**
 * Where `api.title` lives.
 *
 * It used to live in `net/api.js`, which grafted it onto whichever implementation the entry
 * point selected and sent it at the network from all of them. So the project's own offline
 * flow — `?mock=1&write=1`, served by `python3 -m http.server` — answered a rename with the
 * static file server's `Unsupported method ('POST')`, printed on the card as if the Mac had
 * said it. Every other write on that card goes through the transport and every transport
 * refuses in its own words; this checks that the rename now does too.
 */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

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

const fixtureWrites = process.env.CLAWDLINE_TITLE_TRANSPORT_MOCK_WRITE === "1";
globalThis.localStorage = { getItem: function () { return null; }, setItem: noop };
globalThis.location = {
    search: fixtureWrites ? "?mock=1&write=1" : "?mock=1",
    protocol: "http:", hostname: "localhost", pathname: "/", href: "http://localhost/"
};
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

// Nothing in this file is allowed to reach the network. A transport that cannot do something
// says so; it does not find out by asking a server that is not there.
const reached = [];
globalThis.fetch = function (path, options) {
    reached.push({ path: path, options: options });
    return Promise.resolve({ ok: true, text: function () {
        return Promise.resolve(JSON.stringify({ ok: true, title: "Release room",
            display_title: "Release room", local_applied: true, downstream: "synced" }));
    } });
};

const { Mock } = await import("../Resources/web/app/js/net/mock.js");
assert.equal(typeof Mock.title, "function", "the fixtures carry a rename of their own");

// Selected the way the entry point selects it, because the bug was in the selecting: `useApi`
// put its own `title` on whatever it was handed, so the fixtures answered this call over the
// network.
const selected = await import("../Resources/web/app/js/net/api.js");
selected.useApi(Mock);

if (fixtureWrites) {
    const answer = await selected.api.title("8F3A-1C", "  Release  room  ");
    assert.equal(reached.length, 0,
        "the fixture answers a rename itself rather than posting to whatever is serving the page");
    assert.equal(answer.title, "Release room", "the fixture normalizes the way the route does");
    assert.equal(answer.display_title, "Release room", "and answers with what the card should draw");
    assert.equal(answer.downstream, "local_only",
        "a fixture has no terminal to type a slash command into and does not pretend otherwise");
    const info = await Mock.info("8F3A-1C");
    assert.equal(info.info.session.title, "Release room",
        "the fixture keeps the new name, so the offline flow shows the feature working");
    console.log("web title transport fixture write passed");
    process.exit(0);
}

await assert.rejects(selected.api.title("8F3A-1C", "Release room"), function (error) {
    assert.equal(error.code, "write_disabled", "the fixture refuses with its own typed code");
    assert.match(error.message, /not enabled on this server/,
        "and with its own sentence, not a static file server's");
    return true;
}, "a read-only fixture refuses a rename rather than posting it");
assert.equal(reached.length, 0, "and it refuses without reaching the network");

// A transport that offers no rename is left offering none. The namespace object rather than a
// destructured copy, because `api` is a live binding that `useApi` writes and the whole page
// depends on importers seeing what was put in rather than the null it started as.
selected.useApi({ send: noop, answer: noop });
assert.equal(typeof selected.api.title, "undefined",
    "selecting a transport does not add a rename the transport did not offer");

const { Live } = await import("../Resources/web/app/js/net/live.js");
assert.equal(typeof Live.title, "function", "the local transport carries its own rename");
await Live.title("8F3A-1C", "Release room");
assert.equal(reached.length, 1, "which is the one that does go to the Mac");
assert.equal(reached[0].path, "/v1/sessions/8F3A-1C/title", "at the documented route");
assert.equal(reached[0].options.method, "POST");
assert.equal(JSON.parse(reached[0].options.body).title, "Release room");
assert.ok(reached[0].options.headers["Idempotency-Key"],
    "keyed like every other write, so a retry of this request is not a second rename");

const { T } = await import("../Resources/web/app/js/core/i18n.js");
const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");
const cloud = new CloudClient({ relayURL: "wss://relay.example", deviceToken: "token" });
assert.equal(typeof cloud.title, "function", "the cloud transport carries one too");
await assert.rejects(cloud.title({ machine: "mac-02", session: "8F3A-1C" }),
    function (error) {
        assert.equal(error.code, "unsupported", "which refuses with a typed code");
        assert.equal(error.message, T.webInfoTitleCloud,
            "and a sentence from the string table, so it is not English on a translated page");
        return true;
    }, "renaming a session over the cloud transport is refused");
assert.equal(reached.length, 1, "and refused without a request");

const fixtureWrite = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
    cwd: process.cwd(), encoding: "utf8",
    env: { ...process.env, CLAWDLINE_TITLE_TRANSPORT_MOCK_WRITE: "1" }
});
assert.equal(fixtureWrite.status, 0,
    "the write-enabled fixture pass passes: " + (fixtureWrite.stderr || fixtureWrite.stdout));
assert.match(fixtureWrite.stdout, /fixture write passed/,
    "and reached all of its assertions");

console.log("web title transport tests passed");
process.exit(0);
