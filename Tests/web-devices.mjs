import assert from "node:assert/strict";
import { T } from "../Resources/web/app/js/core/i18n.js";
import {
    bindDevicesPage, deviceViewModel
} from "../Resources/web/app/js/view/devices.js";

let checks = 0;
function check(name, fn) {
    checks += 1;
    try { fn(); console.log("  ✓ " + name); }
    catch (error) { console.error("  ✗ " + name); throw error; }
}

check("a named AWS executor is a person-readable device, not an opaque route", function () {
    const row = deviceViewModel({
        id: "mac_261c5bd4-2dde-47b4-a580-face51463f04",
        name: "Clawdline Linux on AWS",
        platform: "linux",
        provider: "aws",
        kind: "linux-aws",
        label: "Linux / AWS · Clawdline Linux on AWS",
        freshness: "current",
        pairing: "paired",
        sessions: 3,
        selectable: true
    }, T);
    assert.equal(row.label, "Linux / AWS · Clawdline Linux on AWS");
    assert.equal(row.identifier, "face51463f04".slice(-8));
    assert.equal(row.connection, T.webDeviceOnline);
    assert.equal(row.pairing, T.webDevicePaired);
    assert.equal(row.sessions, "3 " + T.webSessions);
    assert.equal(row.canStart, true);
});

check("pairing and connection are separate facts", function () {
    const row = deviceViewModel({
        id: "mac_80857bb3-d7fc-4345-8165-58d7e38e32bf",
        label: "Mac · Studio",
        freshness: "stale",
        pairing: "not_paired",
        sessions: 0,
        selectable: true
    }, T);
    assert.equal(row.connection, T.webStartMachineStale);
    assert.equal(row.pairing, T.webDeviceNotPaired);
    assert.equal(row.pairHelp, T.webDevicePairHelp);
});

check("an unknown descriptor never promotes the complete machine UUID to the title", function () {
    const id = "mac_01234567-89ab-cdef-0123-456789abcdef";
    const row = deviceViewModel({ id, label: id,
        freshness: "unknown", pairing: "unknown", sessions: 0 }, T);
    assert.equal(row.label.includes(id), false);
    assert.equal(row.identifier, "89abcdef");
    assert.equal(row.pairing, T.webDevicePairingUnknown);
});

class FakeNode {
    constructor(doc) {
        this.ownerDocument = doc; this.children = []; this.dataset = {};
        this.hidden = false; this.className = ""; this.title = ""; this._text = "";
    }
    appendChild(child) { this.children.push(child); return child; }
    removeChild(child) { this.children = this.children.filter((value) => value !== child); }
    get firstChild() { return this.children[0] || null; }
    get textContent() { return this._text + this.children.map((child) => child.textContent).join(""); }
    set textContent(value) { this._text = String(value || ""); this.children = []; }
}
class FakeDocument { createElement() { return new FakeNode(this); } }

await (async function () {
    const doc = new FakeDocument();
    const ids = ["devices-title", "devices-lede", "devices-close", "devices-status",
        "devices-empty", "devices-rows"];
    const elements = Object.fromEntries(ids.map((id) => [id, new FakeNode(doc)]));
    let chosen = null;
    const page = bindDevicesPage(elements, {
        machines: () => Promise.resolve({ machines: [{
            id: "linux-aws-02", label: "Linux / AWS · Builder", freshness: "current",
            pairing: "paired", sessions: 2, selectable: true
        }] }),
        start: (id) => { chosen = id; }
    });
    page.enter();
    await new Promise((done) => setImmediate(done));
    await new Promise((done) => setImmediate(done));
    check("the page renders status and starts on the exact opaque route behind the friendly card", function () {
        assert.equal(elements["devices-rows"].children.length, 1);
        const card = elements["devices-rows"].children[0];
        assert.equal(card.textContent.includes("Linux / AWS · Builder"), true);
        const start = card.children[card.children.length - 1];
        start.onclick();
        assert.equal(chosen, "linux-aws-02");
    });
})();

await (async function () {
    const doc = new FakeDocument();
    const ids = ["devices-title", "devices-lede", "devices-close", "devices-status",
        "devices-empty", "devices-rows"];
    const elements = Object.fromEntries(ids.map((id) => [id, new FakeNode(doc)]));
    let listener = null;
    let answer = { machines: [{
        id: "mac_80857bb3-d7fc-4345-8165-58d7e38e32bf",
        label: "mac_80857bb3-d7fc-4345-8165-58d7e38e32bf",
        freshness: "current", pairing: "paired", sessions: 11, selectable: true
    }] };
    const page = bindDevicesPage(elements, {
        machines: () => Promise.resolve(answer),
        events: (fn) => { listener = fn; return () => { listener = null; }; }
    });
    page.enter();
    await new Promise((done) => setImmediate(done));
    await new Promise((done) => setImmediate(done));
    assert.equal(elements["devices-rows"].textContent.includes("Mac · Sean MacBook Pro"), false);
    answer = { machines: [{
        id: "mac_80857bb3-d7fc-4345-8165-58d7e38e32bf",
        label: "Mac · Sean MacBook Pro", freshness: "current",
        pairing: "paired", sessions: 11, selectable: true
    }] };
    listener({ type: "orchestrator" });
    await new Promise((done) => setImmediate(done));
    await new Promise((done) => setImmediate(done));
    check("a descriptor arriving after Session rows redraws the open Devices page", function () {
        assert.equal(elements["devices-rows"].textContent.includes("Mac · Sean MacBook Pro"), true);
    });
    answer = { machines: [{
        id: "linux-aws-02", label: "linux-aws-02", freshness: "unknown",
        pairing: "not_paired", sessions: 0, selectable: false
    }] };
    listener({ type: "error", error: { code: "machine_key_incomplete" } });
    await new Promise((done) => setImmediate(done));
    await new Promise((done) => setImmediate(done));
    check("an incomplete machine-pairing event redraws Devices without enabling New Session", function () {
        const card = elements["devices-rows"].children[0];
        assert.equal(card.textContent.includes(T.webDeviceNotPaired), true);
        assert.equal(card.children.some((child) => child.className === "device-start"), false);
    });
    page.leave();
    assert.equal(listener, null);
})();

await (async function () {
    const doc = new FakeDocument();
    const ids = ["devices-title", "devices-lede", "devices-close", "devices-status",
        "devices-empty", "devices-rows"];
    const elements = Object.fromEntries(ids.map((id) => [id, new FakeNode(doc)]));
    let answer = { syncing: true, retryAfterMs: 12_000, machines: [{
        id: "mac-01", label: "Mac · Studio", freshness: "current",
        pairing: "paired", sessions: 8, selectable: true
    }] };
    let retry = null;
    const page = bindDevicesPage(elements, {
        machines: () => Promise.resolve(answer),
        setTimeout: (fn, ms) => { retry = { fn, ms }; return 1; },
        clearTimeout: () => { retry = null; }
    });
    page.enter();
    await new Promise((done) => setImmediate(done));
    await new Promise((done) => setImmediate(done));
    check("the page keeps a visible loading state while other machine inventories are realigning", function () {
        assert.equal(elements["devices-status"].textContent, T.webLoading);
        assert.equal(elements["devices-rows"].children.length, 1,
            "already-known devices remain useful while AWS is still loading");
        assert.deepEqual([retry && retry.ms > 0, retry && retry.ms <= 12_000], [true, true]);
    });
    answer = { syncing: false, machines: answer.machines.concat([{
        id: "aws-01", label: "Linux / AWS · Builder", freshness: "current",
        pairing: "paired", sessions: 0, selectable: true
    }]) };
    const finish = retry.fn;
    finish();
    await new Promise((done) => setImmediate(done));
    await new Promise((done) => setImmediate(done));
    check("the loading state clears only after the bounded inventory window", function () {
        assert.equal(elements["devices-status"].textContent, "");
        assert.equal(elements["devices-rows"].children.length, 2);
    });
    page.leave();
})();

console.log(`\n${checks} web device checks passed`);
