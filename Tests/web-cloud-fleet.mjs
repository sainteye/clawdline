/**
 * A Mac + Linux account on the hosted console: every feature answers with the Mac's data within a
 * bound, and nothing but `places` and `start` is ever published toward the Linux executor.
 *
 * On 2026-09-15 the Snippets sheet opened blank on exactly this account, and the audit behind it
 * found the same shape at every site that picked or iterated machines: `_onlyMachine` refused any
 * account with two machines, and the fan-out reads asked the Linux executor for things it has no
 * handler for and then waited out its silence. The fixture here is that account with the fault
 * injected: the Linux machine answers `places` and `start` and **never replies to anything else**,
 * while the Mac answers every closed command it owns.
 *
 * Every check runs and reports on its own, so the file run against a tree without the fix names
 * each missing behaviour instead of stopping at the first (`artifacts/red-proofs.md` of task
 * ed5f1c24 records that run).
 */
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

process.on("unhandledRejection", function () { });

/* ---- a document for the UI modules driven below --------------------------------------------- */

const noop = function () { };
const element = new Proxy(function () { }, {
    get: function (_target, key) {
        if (key === Symbol.iterator) return function* () { };
        if (key === "classList") return { add: noop, remove: noop, toggle: noop, contains: function () { return false; } };
        if (key === "style" || key === "dataset") return { setProperty: noop, removeProperty: noop };
        if (key === "children") return [];
        if (key === "querySelectorAll") return function () { return []; };
        if (key === "content") return { cloneNode: function () { return element; } };
        return element;
    },
    apply: function () { return element; }
});
/** `window` and `document`: what this file assigns is read back; anything else is `element`. */
function owning() {
    return new Proxy(function () { }, {
        get: function (target, key) {
            return Object.prototype.hasOwnProperty.call(target, key) && key !== "name" && key !== "length"
                ? target[key] : element[key];
        }
    });
}
const controls = new Map();
function control(id) {
    if (!controls.has(id)) controls.set(id, { id, hidden: false, disabled: false, textContent: "", className: "",
        listeners: {}, children: [], dataset: {}, style: { setProperty: noop, removeProperty: noop },
        classList: { add: noop, remove: noop, toggle: noop, contains: function () { return false; } },
        setAttribute: noop, removeAttribute: noop, replaceChildren: function () { this.children = []; },
        appendChild: function (child) { this.children.push(child); },
        addEventListener: function (event, action) { this.listeners[event] = action; },
        removeEventListener: noop, focus: noop, querySelector: function () { return null; },
        querySelectorAll: function () { return []; } });
    return controls.get(id);
}
const RECORDED = /^(toast|settings-board-|projects-lede|nav-|usage-open|schedule-places|schedule-rows|schedules)/;
const memory = new Map();
globalThis.localStorage = { getItem: (key) => memory.has(key) ? memory.get(key) : null,
    setItem: (key, value) => memory.set(key, String(value)), removeItem: (key) => memory.delete(key) };
globalThis.location = { search: "", protocol: "https:", hostname: "app.clawdline.com", pathname: "/", href: "https://app.clawdline.com/" };
globalThis.history = { replaceState: noop, pushState: noop };
const pushRegistration = { active: {}, pushManager: { getSubscription: function () {
    return Promise.resolve({ unsubscribe: function () { return Promise.resolve(true); } });
} } };
Object.defineProperty(globalThis, "navigator", { configurable: true, value: { userAgent: "node", maxTouchPoints: 0,
    platform: "", language: "en", serviceWorker: { register: function () { return Promise.resolve(pushRegistration); } } } });
globalThis.window = owning();
window.devicePixelRatio = 1;
window.matchMedia = function () { return { matches: false, addEventListener: noop }; };
window.isSecureContext = true;
window.PushManager = function () { };
globalThis.Notification = { permission: "granted", requestPermission: function () { return Promise.resolve("granted"); } };
globalThis.document = owning();
document.documentElement = { lang: "en", dataset: {}, style: { setProperty: noop, removeProperty: noop } };
document.getElementById = function (id) { return RECORDED.test(id) ? control(id) : element; };
document.querySelector = function () { return element; };
document.querySelectorAll = function () { return []; };
document.createElement = function () { return control(Symbol("created")); };
document.body = element;
globalThis.MutationObserver = class { observe() { } disconnect() { } };
globalThis.ResizeObserver = MutationObserver;
globalThis.IntersectionObserver = MutationObserver;

const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");
const { T } = await import("../Resources/web/app/js/core/i18n.js");
const { describeFailure } = await import("../Resources/web/app/js/core/failure-text.js");
const { importDevicePrivateKey, importMasterSecret, importSenderPublicKey, openEnvelope, sealEnvelope } =
    await import("../Resources/web/app/js/net/cloud-crypto.js");

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
// Short enough that a silent machine's own bound is observable here, long enough that a prompt
// answer is never mistaken for one. A check's window below is either FAST (an answer that needs no
// timeout at all) or BOUND (one silent machine's own timeout, plus slack).
const READ_TIMEOUT_MS = 2000;
const FAST = 600;
const BOUND = READ_TIMEOUT_MS + 1500;
const LINUX_ANSWERS = new Set(["places", "start"]);
const nowSeconds = () => Math.floor(Date.now() / 1000);

/* ---- the relay, the Mac and the Linux executor ---------------------------------------------- */

class RelaySocket {
    static latest = null;
    constructor() { this.readyState = 1; this.sent = []; this.onPublish = null; RelaySocket.latest = this; }
    send(text) {
        const frame = JSON.parse(text);
        this.sent.push(frame);
        if (frame.type === "publish" && this.onPublish) {
            const envelope = frame.envelope;
            Promise.resolve().then(() => this.onPublish(envelope));
        }
    }
    close(code) { this.readyState = 3; if (this.onclose) this.onclose({ code: code || 1000 }); }
    receive(frame) { this.onmessage({ data: JSON.stringify(frame) }); }
}

let outboundSequence = 100;
function fleetClient(options) {
    return new CloudClient(Object.assign({
        relayURL: "https://relay.example", deviceToken: "jwt", devicePrivateKey: signingKey,
        masterKey: masterKey, senderKeys: { [DEVICE]: senderKey }, WebSocket: RelaySocket,
        account: ACCOUNT, deviceID: DEVICE, BroadcastChannel: null, allowWrites: true,
        nextSequence: function () { return Promise.resolve(outboundSequence++); },
        readTimeoutMs: READ_TIMEOUT_MS, statusProbeTimeoutMs: 100, ackTimeoutMs: 300
    }, options));
}

async function ready(client) {
    await client.start();
    const socket = RelaySocket.latest;
    socket.receive({ type: "challenge", v: 1, context: "clawdline-challenge-v1", account: ACCOUNT,
        device: DEVICE, challenge: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", expires_in_ms: 30_000 });
    await client.messageChain;
    socket.receive({ type: "ready", v: 1, role: "viewer", account: ACCOUNT, device: DEVICE });
    await client.messageChain;
    return socket;
}

let machineSequence = 9000;
async function fromMachine(client, socket, channel, payload) {
    const envelope = await sealEnvelope({ ch: channel, seq: ++machineSequence, ts: 1787817600000,
        class: "stream", key_id: "ms-1", sender: DEVICE }, JSON.stringify(payload), masterKey, signingKey);
    socket.receive({ type: "envelope", envelope });
    await client.messageChain.catch(noop);
}

const CLOUD_STATUS = { v: 1, generated_at_ms: 1789290000000, counting_since_ms: 1789280000000,
    clock_guard: { state: "ready", reason: null, clears_at_ms: null }, token_expires_at_ms: 1789290240000,
    key_id: "ms-1", roster_readable: true, dropped: {}, recent_drops: [], recent_notices: [] };

const MAC_BOARD = { enabled: true, revision: 1, projects: [{ id: "project-app", label: "app",
    displayPath: "/code/app", isStartPoint: true, itemCount: 0 }], viewer: { id: "admin", canWrite: true, canManage: true } };

/** What a Mac answers, by command: the reply name's prefix and a body. */
function macAnswer(machine, command) {
    const read = (body) => ({ read: "read:" + command.request, status: 200, body });
    const action = (body) => ({ read: "action:" + command.request, status: 200, body });
    switch (command.type) {
    case "snippets": return read({ snippets: [{ id: machine + "-deploy", title: "Deploy " + machine,
        body: "commit, push, deploy", scope: "global", position: 1 }], at: nowSeconds() });
    case "schedules": return read({ schedules: [{ id: machine + "-morning", title: "Morning " + machine }], at: nowSeconds() });
    case "places": return read({ places: [{ id: "app", label: "app " + machine, path: "/code/app" }],
        assistants: [{ id: "claude", label: "Claude" }] });
    case "push-key": return read({ key: "BPushKeyFrom" + machine });
    case "push-subscribe": return action({ id: "subscription-" + machine });
    case "push-unsubscribe": return action({ ok: true });
    case "push-test": return action({ ok: true, sent: 1 });
    case "diagnostics.report": return action({ path: "/tmp/" + machine + "-report.json", bytes: 42 });
    case "board": return read({ board: MAC_BOARD });
    case "board-command": return action({ board: Object.assign({}, MAC_BOARD, { revision: 2 }) });
    case "project-worktree-lifecycle":
    case "project-worktree-lifecycle-refresh": return read({ projectWorktreeLifecycle: { schemaVersion: 1, rows: [] } });
    case "timeline": return read({ timeline: { entries: [], machine } });
    case "timeline-command": return action({ ok: true });
    case "cloud.status": return read(Object.assign({}, CLOUD_STATUS, { commands: [] }));
    case "voice": return action({ text: "heard on " + machine });
    default: return null;
    }
}

/**
 * The account, connected. `machines` rows are `[id, platform, current, extra]`; a `macos`/`darwin`
 * machine also publishes `cloud_status`, as only the Mac does. `linux` answers only what
 * `LINUX_ANSWERS` names and is silent on everything else — the injected fault.
 */
async function fleet(machines, options) {
    options = options || {};
    const client = fleetClient(options.client);
    const socket = await ready(client);
    const commands = [];
    const kinds = new Map(machines.map(([id, platform]) => [id, platform]));
    socket.onPublish = async function (envelope) {
        const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
        const machine = decodeURIComponent(envelope.ch.replace(/^ctl\//, ""));
        commands.push({ machine, type: command.type, command });
        if (options.intercept && await options.intercept(client, socket, machine, command)) return;
        const platform = kinds.get(machine);
        if (platform === "linux") {
            if (!LINUX_ANSWERS.has(command.type)) return;
            if (command.type === "places") {
                await fromMachine(client, socket, "t/" + machine + "/" + MACHINE_REPLY, { read: "read:" + command.request,
                    status: 200, body: { places: [{ id: "reaver", label: "reaver", path: "/srv/reaver" }],
                        assistants: [{ id: "claude", label: "claude", availability: "available" }] } });
            } else {
                await fromMachine(client, socket, "t/" + machine + "/" + MACHINE_REPLY, { read: "action:" + command.request,
                    status: 200, body: { ok: true, id: "linux-new", backend: "tmux" } });
            }
            return;
        }
        if (options.silent && options.silent.has(machine)) return;
        const answer = macAnswer(machine, command);
        if (answer) await fromMachine(client, socket, "t/" + machine + "/" + MACHINE_REPLY, answer);
    };
    for (const [id, platform, current, extra] of machines) {
        const mac = platform === "macos" || platform === "darwin";
        await fromMachine(client, socket, "orch/" + id, Object.assign({ tasks: [] },
            platform ? { machine: { name: id, platform } } : {},
            current ? { at: nowSeconds() } : {},
            mac ? { cloud_status: CLOUD_STATUS } : {}, extra || {}));
        await fromMachine(client, socket, "s/" + id + "/s-" + id, { session: { id: "s-" + id, cwd: "/code/app" } });
    }
    return { client, socket, commands,
        typesTo: (machine) => commands.filter((row) => row.machine === machine).map((row) => row.type),
        publishedCount: () => socket.sent.filter((frame) => frame.type === "publish").length };
}

const MAC_LINUX = [["mac-01", "macos", true, { snippets: [{ id: "mac-01-deploy", title: "Deploy",
    body: "commit, push, deploy", scope: "global", position: 1 }], schedules: [{ id: "morning", title: "Morning" }] }],
["linux-01", "linux", true]];
const TWO_MACS = [["mac-01", "macos", true], ["mac-02", "darwin", true], ["linux-01", "linux", true]];

function outcome(promise, ms) {
    return Promise.race([
        Promise.resolve(promise).then((value) => ({ state: "resolved", value }), (error) => ({ state: "rejected", error })),
        new Promise((resolve) => setTimeout(resolve, ms, { state: "pending" }))
    ]);
}
function onlyPlacesAndStartTo(fixture, machine) {
    const others = fixture.typesTo(machine).filter((type) => !LINUX_ANSWERS.has(type));
    assert.deepEqual(others, [], "nothing but places/start was published toward " + machine);
}
const settle = async () => { for (let i = 0; i < 6; i++) await new Promise((resolve) => setImmediate(resolve)); };
/** Resolves when `condition()` holds or `ms` has passed, whichever is first; the caller asserts. */
async function until(condition, ms) {
    const ends = Date.now() + ms;
    while (!condition() && Date.now() < ends) await new Promise((resolve) => setTimeout(resolve, 10));
}
const macSession = { machine: "mac-01", session: "s-mac-01" };
const linuxSession = { machine: "linux-01", session: "s-linux-01" };

const failures = [];
const passed = [];
async function check(name, run) {
    try { await run(); passed.push(name); }
    catch (error) { failures.push(name + "\n    " + String(error && error.message).split("\n").slice(0, 8).join("\n    ")); }
}

/* ---- 1 · Snippets ---------------------------------------------------------------------------- */

await check("1 snippets · the open Mac Session's list paints at once on a Mac + Linux account, and Linux is asked nothing", async function () {
    const f = await fleet(MAC_LINUX);
    const ended = await outcome(f.client.snippets(macSession), FAST);
    assert.equal(ended.state, "resolved", "the sheet's read did not wait on the Linux executor (" + ended.state + ")");
    assert.deepEqual(ended.value.snippets.map((row) => [row.id, row.machine]), [["mac-01-deploy", "mac-01"]]);
    onlyPlacesAndStartTo(f, "linux-01");
});

await check("1 snippets · a retained Mac snapshot without the field asks that Mac, and only that Mac", async function () {
    const f = await fleet([["mac-01", "macos", true], ["linux-01", "linux", true]]);
    const ended = await outcome(f.client.snippets(macSession), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual(ended.value.snippets.map((row) => row.machine), ["mac-01"]);
    assert.deepEqual(f.typesTo("mac-01"), ["snippets"]);
    onlyPlacesAndStartTo(f, "linux-01");
});

await check("1 snippets · a Linux Session's sheet is refused as unsupported at once and nothing is published", async function () {
    const f = await fleet(MAC_LINUX);
    const before = f.publishedCount();
    const ended = await outcome(f.client.snippets(linuxSession), FAST);
    assert.deepEqual([ended.state, ended.error && ended.error.code, ended.error && ended.error.layer, f.publishedCount()],
        ["rejected", "cloud_machine_unsupported", "browser", before]);
    assert.equal(describeFailure(ended.error).text, T.webFailMachineUnsupported);
    assert.equal(typeof T.webFailMachineUnsupported, "string");
});

await check("1 snippets · the sheet is never silent while it reads", async function () {
    const { snippetsListHTML } = await import("../Resources/web/app/js/view/snippets-data.js");
    const loading = snippetsListHTML(null, { loading: true });
    assert.ok(loading.includes(T.webReading), "the loading list says it is reading: " + JSON.stringify(loading));
    assert.ok(!loading.includes(T.webSnippetsEmpty) && !loading.includes(T.webSnippetsEmptyNew),
        "and does not claim there are none before an answer has arrived");
});

await check("1 snippets · an account-level read keeps the answering Mac's rows when another Mac is silent, and names it", async function () {
    const f = await fleet([["mac-01", "macos", true], ["mac-02", "macos", true], ["linux-01", "linux", true]],
        { silent: new Set(["mac-02"]) });
    const started = Date.now();
    const ended = await outcome(f.client.snippets(), BOUND);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.ok(Date.now() - started < BOUND, "within the silent Mac's own bound");
    assert.deepEqual(ended.value.snippets.map((row) => row.machine), ["mac-01"]);
    assert.deepEqual(ended.value.unanswered.map((row) => [row.machine, row.error.code]), [["mac-02", "cloud_read_timeout"]]);
    onlyPlacesAndStartTo(f, "linux-01");
});

/* ---- 2 · Schedules --------------------------------------------------------------------------- */

await check("2 schedules · a visible refresh asks the Mac and never the Linux executor", async function () {
    const f = await fleet(MAC_LINUX);
    const ended = await outcome(f.client.schedules({ fresh: true }), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual(ended.value.schedules.map((row) => [row.id, row.machine]), [["mac-01-morning", "mac-01"]]);
    assert.deepEqual(f.typesTo("mac-01"), ["schedules"]);
    onlyPlacesAndStartTo(f, "linux-01");
});

await check("2 schedules · a Linux-only account has no schedules to show rather than an old-Mac refusal", async function () {
    const f = await fleet([["linux-01", "linux", true]]);
    const retained = await outcome(f.client.schedules(), FAST);
    const fresh = await outcome(f.client.schedules({ fresh: true }), FAST);
    assert.deepEqual([retained.state, retained.value && retained.value.schedules, fresh.state, fresh.value && fresh.value.schedules],
        ["resolved", [], "resolved", []]);
    onlyPlacesAndStartTo(f, "linux-01");
});

/** The Mac answering its schedules with `project_dir`, so the strip needs no per-row detail read. */
async function schedulesWithProjects(client, socket, machine, command) {
    if (machine !== "mac-01" || command.type !== "schedules") return false;
    await fromMachine(client, socket, "t/mac-01/" + MACHINE_REPLY, { read: "read:" + command.request, status: 200,
        body: { schedules: [{ id: "mac-01-morning", title: "Morning", project_dir: "/code/app" }], at: nowSeconds() } });
    return true;
}

/** The strip module driven as the page drives it: the selected transport, an arrived inventory. */
async function strip(client) {
    const { useClient } = await import("../Resources/web/app/js/net/api.js");
    const { S } = await import("../Resources/web/app/js/core/state.js");
    const schedules = await import("../Resources/web/app/js/net/schedules.js");
    useClient(client);
    const before = { arrived: S.arrived, locked: S.locked, conn: S.conn };
    Object.assign(S, { arrived: true, locked: false, conn: "live" });
    return { Schedules: schedules.Schedules, forget: schedules.forgetCachedPlaces, restore: () => Object.assign(S, before) };
}

await check("2 schedules · the strip's own refresh draws the Mac's schedules within a bound on a Mac + Linux account", async function () {
    // Reported on 2026-09-15 ("the scheduled tasks at the bottom are not showing either"): on
    // a0be4680 `net/schedules.js` asked `schedules({ fresh: true })`, which waited on the Linux
    // executor until the read timeout and then refused, and the strip draws nothing on a refusal.
    const f = await fleet(MAC_LINUX, { intercept: schedulesWithProjects });
    const driven = await strip(f.client);
    try {
        control("schedules-count").textContent = "";
        control("schedule-rows").innerHTML = "";
        // Inside one silent machine's read timeout, so a strip that waits on the executor cannot pass.
        const bound = READ_TIMEOUT_MS - 500;
        const started = Date.now();
        driven.Schedules.refresh();
        await until(() => control("schedules-count").textContent === "1", bound);
        const waited = Date.now() - started;
        assert.equal(control("schedules-count").textContent, "1",
            "the strip drew the Mac's one schedule within " + bound + " ms (after " + waited + " ms it showed "
            + JSON.stringify(control("schedules-count").textContent) + ")");
        assert.ok(String(control("schedule-rows").innerHTML).includes("mac-01-morning"), "and the row is the Mac's");
        onlyPlacesAndStartTo(f, "linux-01");
    } finally { driven.restore(); }
});

/* ---- 3 · Push ------------------------------------------------------------------------------- */

await check("3 push · key, subscribe, unsubscribe and a test with no Session all reach the one Mac", async function () {
    const f = await fleet(MAC_LINUX);
    const key = await outcome(f.client.pushKey(), FAST);
    const subscribed = await outcome(f.client.pushSubscribe({ endpoint: "https://push.example/1", keys: {} }), FAST);
    const unsubscribed = await outcome(f.client.pushUnsubscribe("subscription-mac-01"), FAST);
    const tested = await outcome(f.client.pushTest(null), FAST);
    const sessionTest = await outcome(f.client.pushTest(macSession), FAST);
    const linuxSelected = await outcome(f.client.pushTest(linuxSession), FAST);
    assert.deepEqual([key.state, subscribed.state, unsubscribed.state, tested.state, sessionTest.state, linuxSelected.state],
        ["resolved", "resolved", "resolved", "resolved", "resolved", "resolved"],
        [key, subscribed, unsubscribed, tested, sessionTest, linuxSelected].map((row) => row.error && row.error.code).join(","));
    assert.equal(key.value.key, "BPushKeyFrommac-01");
    assert.deepEqual(f.typesTo("mac-01"), ["push-key", "push-subscribe", "push-unsubscribe", "push-test", "push-test", "push-test"]);
    assert.deepEqual(f.commands.filter((row) => row.type === "push-test").map((row) => row.command.target),
        ["", "s-mac-01", ""], "a Linux Session selected in the list is no push destination: the test is the account's");
    onlyPlacesAndStartTo(f, "linux-01");
});

await check("3 push · two Macs are a typed refusal for every push call, even with one current, and nothing is published", async function () {
    const f = await fleet([["mac-01", "macos", true], ["mac-02", "macos", false], ["linux-01", "linux", true]]);
    const before = f.publishedCount();
    const rows = [];
    for (const call of [() => f.client.pushKey(), () => f.client.pushSubscribe({}), () => f.client.pushUnsubscribe("x"),
        () => f.client.pushTest(null)]) {
        const ended = await outcome(call(), FAST);
        rows.push([ended.state, ended.error && ended.error.code]);
    }
    assert.deepEqual(rows, Array(4).fill(["rejected", "cloud_machine_ambiguous"]));
    assert.equal(f.publishedCount(), before);
});

await check("3 push · turning notifications off says so when the Mac could not be told, instead of pretending", async function () {
    const f = await fleet(TWO_MACS);
    const { useClient } = await import("../Resources/web/app/js/net/api.js");
    const { Push } = await import("../Resources/web/app/js/input/push.js");
    useClient(f.client);
    localStorage.setItem("clawdline.push", "subscription-mac-01");
    Push.start();
    for (let i = 0; i < 20; i++) await settle();
    const toast = control("toast");
    toast.textContent = "";
    Push.toggle();
    for (let i = 0; i < 40 && !toast.textContent; i++) await settle();
    assert.ok(toast.textContent.includes(T.webFailWhichMac) && toast.textContent.includes("cloud_machine_ambiguous"),
        "the toast names why the Mac was not told: " + JSON.stringify(toast.textContent));
    assert.ok(typeof T.webNotifyOffUntold === "string" && toast.textContent.startsWith(T.webNotifyOffUntold.split("{why}")[0]),
        "and says this device is off while the Mac still holds the subscription");
    assert.ok(/\berr\b/.test(toast.className), "as a failure");
    assert.equal(localStorage.getItem("clawdline.push"), null, "this browser's subscription is gone either way");
});

/* ---- 4 · Diagnostics ------------------------------------------------------------------------ */

await check("4 diagnostics · Send to Mac reaches the Mac on a Mac + Linux account", async function () {
    const f = await fleet(MAC_LINUX);
    const ended = await outcome(f.client.diagnosticsReport({ layout: "phone" }), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.equal(ended.value.path, "/tmp/mac-01-report.json");
    onlyPlacesAndStartTo(f, "linux-01");
});

await check("4 diagnostics · two current Macs are a typed refusal and publish nothing; one current Mac is chosen", async function () {
    const f = await fleet(TWO_MACS);
    const before = f.publishedCount();
    const refused = await outcome(f.client.diagnosticsReport({ layout: "phone" }), FAST);
    assert.deepEqual([refused.state, refused.error && refused.error.code, f.publishedCount()], ["rejected", "cloud_machine_ambiguous", before]);
    const g = await fleet([["mac-01", "macos", false], ["mac-02", "darwin", true], ["linux-01", "linux", true]]);
    const chosen = await outcome(g.client.diagnosticsReport({ layout: "phone" }), FAST);
    assert.deepEqual([chosen.state, chosen.value && chosen.value.path], ["resolved", "/tmp/mac-02-report.json"]);
});

/* ---- 5 · Projects --------------------------------------------------------------------------- */

await check("5 projects · the Projects page reads the Mac's Board, and every Project carries its machine", async function () {
    const f = await fleet(MAC_LINUX);
    const { readProjectPlaces } = await import("../Resources/web/app/js/view/projects.js");
    const ended = await outcome(readProjectPlaces(f.client), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual(ended.value.places.map((place) => [place.boardProjectId, place.machine]), [["project-app", "mac-01"]]);
    onlyPlacesAndStartTo(f, "linux-01");
});

await check("5 projects · two Macs fall back to every machine's places instead of failing the page", async function () {
    const f = await fleet(TWO_MACS);
    const { readProjectPlaces } = await import("../Resources/web/app/js/view/projects.js");
    const ended = await outcome(readProjectPlaces(f.client), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.equal(ended.value.boardUnavailable && ended.value.boardUnavailable.code, "cloud_machine_ambiguous");
    assert.deepEqual(ended.value.places.map((place) => place.machine).sort(), ["linux-01", "mac-01", "mac-02"]);
});

/* ---- 6 · Settings Board --------------------------------------------------------------------- */

await check("6 settings board · the read and the toggle both reach the Mac on a Mac + Linux account", async function () {
    const f = await fleet(MAC_LINUX);
    const { useClient } = await import("../Resources/web/app/js/net/api.js");
    const { BoardControls } = await import("../Resources/web/app/js/input/board-settings.js");
    useClient(f.client);
    control("settings-board-toggle").disabled = true;
    const read = await outcome(BoardControls.refresh(), FAST);
    assert.equal(read.state, "resolved", String(read.error && (read.error.stack || read.error)));
    assert.equal(control("settings-board-toggle").disabled, false,
        "a manager may press it: " + control("settings-board-status").textContent);
    control("settings-board-toggle").listeners.click();
    for (let i = 0; i < 40 && f.typesTo("mac-01").indexOf("board-command") < 0; i++) await settle();
    for (let i = 0; i < 10; i++) await settle();
    assert.deepEqual(f.typesTo("mac-01"), ["board", "board-command"]);
    assert.equal(control("settings-board-status").textContent, "Saved");
    onlyPlacesAndStartTo(f, "linux-01");
});

/* ---- 7 · Project worktree lifecycle --------------------------------------------------------- */

await check("7 worktrees · lifecycle and refresh with no machine reach the Mac, and two Macs refuse without publishing", async function () {
    const f = await fleet(MAC_LINUX);
    const read = await outcome(f.client.projectWorktreeLifecycle("project-app"), FAST);
    const refresh = await outcome(f.client.projectWorktreeLifecycleRefresh("project-app"), FAST);
    assert.deepEqual([read.state, read.value && read.value.machine, refresh.state, refresh.value && refresh.value.machine],
        ["resolved", "mac-01", "resolved", "mac-01"]);
    onlyPlacesAndStartTo(f, "linux-01");
    const g = await fleet(TWO_MACS);
    const before = g.publishedCount();
    const refused = await outcome(g.client.projectWorktreeLifecycle("project-app"), FAST);
    assert.deepEqual([refused.error && refused.error.code, g.publishedCount()], ["cloud_machine_ambiguous", before]);
});

/* ---- 8 · Board and Timeline with no machine ------------------------------------------------- */

await check("8 board and timeline · opened with no machine they read the Mac; two Macs refuse without publishing", async function () {
    const f = await fleet(MAC_LINUX);
    const board = await outcome(f.client.board("project-app"), FAST);
    const command = await outcome(f.client.boardCommand({ operation: "set_enabled", enabled: false, expectedRevision: 1, requestId: "r" }), FAST);
    const timeline = await outcome(f.client.timeline("project-app"), FAST);
    const timelineCommand = await outcome(f.client.timelineCommand({ operation: "noop" }), FAST);
    assert.deepEqual([board.state, command.state, timeline.state, timelineCommand.state], ["resolved", "resolved", "resolved", "resolved"],
        [board, command, timeline, timelineCommand].map((row) => row.error && row.error.code).join(","));
    assert.equal(board.value.machine, "mac-01", "the Board answer names the machine it came from");
    assert.deepEqual(f.typesTo("mac-01"), ["board", "board-command", "timeline", "timeline-command"]);
    onlyPlacesAndStartTo(f, "linux-01");
    const g = await fleet(TWO_MACS);
    const before = g.publishedCount();
    const codes = [];
    for (const call of [() => g.client.board("p"), () => g.client.boardCommand({}), () => g.client.timeline("p"),
        () => g.client.timelineCommand({})]) {
        codes.push((await outcome(call(), FAST)).error?.code);
    }
    assert.deepEqual([codes, g.publishedCount()], [Array(4).fill("cloud_machine_ambiguous"), before]);
});

/* ---- 9 · The command sheet's places --------------------------------------------------------- */

await check("9 places · one machine that fails does not take the other machines' Projects with it", async function () {
    const f = await fleet(MAC_LINUX, { intercept: async function (client, socket, machine, command) {
        if (machine !== "linux-01" || command.type !== "places") return false;
        await fromMachine(client, socket, "t/linux-01/" + MACHINE_REPLY, { read: "read:" + command.request, status: 503,
            error: { code: "ingress_closed", message: "closed" } });
        return true;
    } });
    const ended = await outcome(f.client.places(), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual(ended.value.places.map((place) => place.machine), ["mac-01"]);
    assert.deepEqual(ended.value.unanswered.map((row) => [row.machine, row.error.code]), [["linux-01", "ingress_closed"]]);
    const silent = await fleet(MAC_LINUX, { intercept: async (client, socket, machine, command) =>
        machine === "linux-01" && command.type === "places" });
    const waited = await outcome(silent.client.places(), BOUND);
    assert.deepEqual([waited.state, waited.value && waited.value.places.map((place) => place.machine),
        waited.value && waited.value.unanswered.map((row) => row.error.code)], ["resolved", ["mac-01"], ["cloud_read_timeout"]],
    "a silent machine delays the list only by its own bound");
    const both = await fleet(MAC_LINUX);
    const all = await outcome(both.client.places(), FAST);
    assert.deepEqual(all.value.places.map((place) => place.machine).sort(), ["linux-01", "mac-01"],
        "and with both answering, both machines' Projects are listed, Linux's included");
});

/* ---- 10 · The Linux executor's refusal ------------------------------------------------------ */

await check("10 linux refusal · unknown_command answered as action: settles the read: waiter at once, and is not asked again", async function () {
    const f = await fleet([["executor-01", null, true]], { intercept: async function (client, socket, machine, command) {
        if (command.type === "places" || command.type === "start") return false;
        // `LinuxDurableCloudRuntime` cannot know the read/action prefix of a word it does not
        // implement, so it answers as the Mac's `commandRefusalReply` does: `action:<request>`.
        await fromMachine(client, socket, "t/" + machine + "/" + MACHINE_REPLY, { read: "action:" + command.request,
            status: 400, error: { code: "unknown_command", message: "This machine does not know that Cloud command." } });
        return true;
    } });
    const first = await outcome(f.client.schedules({ fresh: true }), FAST);
    assert.equal(first.state === "pending" ? "pending" : "settled", "settled", "not held until the read timeout");
    assert.deepEqual(f.typesTo("executor-01"), ["schedules"]);
    const named = await outcome(f.client.pushKey(), FAST);
    assert.deepEqual([named.state, named.error && named.error.code, named.error && named.error.layer], ["rejected", "unknown_command", "mac"],
        "a request that names the executor settles with the executor's own refusal");
    const again = await outcome(f.client.pushKey(), FAST);
    assert.deepEqual([again.state, again.error && again.error.code, f.typesTo("executor-01")],
        ["rejected", "cloud_machine_unsupported", ["schedules", "push-key"]], "and the same word is not sent to it again");
});

await check("10 linux refusal · a Linux descriptor that advertises its commands is taken at its word", async function () {
    const f = await fleet([["mac-01", "macos", true], ["linux-01", "linux", true, { machine: { name: "linux-01",
        platform: "linux", commands: ["places", "start", "snippets"] } }]]);
    const ended = await outcome(f.client.snippets(linuxSession), 60);
    assert.notEqual(ended.error && ended.error.code, "cloud_machine_unsupported", "an advertised word is sent");
    assert.deepEqual(f.typesTo("linux-01"), ["snippets"]);
});

/* ---- 11 · The capability model, as corrected after review 2e03ef42 ------------------------- */
// None of the checks in 11 and 12 is about latency — no machine in them is silent — so each waits a
// whole BOUND: a loaded machine must not turn "who was asked" into "how fast it answered".

await check("11 capability · a word every platform implements makes a machine with no descriptor capable: a Mac known only from its Session rows is asked for places", async function () {
    // Review F1: the Linux executor's descriptor arrived, the Mac's larger `orch/` snapshot had not,
    // and `places()` asked only Linux — the Mac's Projects vanished from every account-level list.
    const client = fleetClient();
    const socket = await ready(client);
    const commands = [];
    socket.onPublish = async function (envelope) {
        const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
        const machine = decodeURIComponent(envelope.ch.replace(/^ctl\//, ""));
        commands.push([machine, command.type]);
        if (command.type !== "places") return;
        const body = machine === "linux-01"
            ? { places: [{ id: "reaver", label: "reaver", path: "/srv/reaver" }], assistants: [] }
            : { places: [{ id: "app", label: "app", path: "/code/app" }], assistants: [{ id: "claude" }] };
        await fromMachine(client, socket, "t/" + machine + "/" + MACHINE_REPLY, { read: "read:" + command.request, status: 200, body });
    };
    await fromMachine(client, socket, "orch/linux-01", { tasks: [], at: nowSeconds(),
        machine: { name: "linux-01", platform: "linux", commands: ["places", "start"] } });
    await fromMachine(client, socket, "s/mac-01/s-mac-01", { session: { id: "s-mac-01", cwd: "/code/app" } });
    assert.deepEqual(client._knownMachines(), ["linux-01", "mac-01"], "the fixture has the Mac only from its Session row");
    const ended = await outcome(client.places(), BOUND);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual([ended.value.places.map((place) => place.machine).sort(), ended.value.unconfirmed],
        [["linux-01", "mac-01"], []], "both machines' Projects, and nobody left unasked");
    assert.deepEqual(commands.map((row) => row.join(" ")).sort(), ["linux-01 places", "mac-01 places"]);
    // In the model, not in `places()`: the same machine is capable of the other universal word and
    // still unknown for a word only a Mac implements.
    assert.deepEqual(["places", "start", "snippets"].map((type) => client._machineImplements("mac-01", type)),
        ["yes", "yes", "unknown"]);
});

await check("11 capability · a Mac that answers unknown_command to the schedules read keeps its published rows, and without the field it is still unpublished", async function () {
    // Review F3: a learned lack on an evident Mac is "older than this page", not "has no schedules".
    const refusing = async function (client, socket, machine, command) {
        if (command.type !== "schedules") return false;
        await fromMachine(client, socket, "t/" + machine + "/" + MACHINE_REPLY, { read: "action:" + command.request,
            status: 400, error: { code: "unknown_command", message: "This Mac does not know that Cloud command." } });
        return true;
    };
    const f = await fleet([["mac-01", "macos", true, { schedules: [{ id: "morning", title: "Morning" }] }]], { intercept: refusing });
    const before = await outcome(f.client.schedules(), BOUND);
    const fresh = await outcome(f.client.schedules({ fresh: true }), BOUND);
    const after = await outcome(f.client.schedules(), BOUND);
    const rows = (ended) => ended.state === "resolved" ? ended.value.schedules.map((row) => row.id) : ended.state + " " + (ended.error && ended.error.code);
    assert.deepEqual([rows(before), rows(fresh), rows(after)], [["morning"], ["morning"], ["morning"]],
        "the published rows survive the refusal");
    assert.deepEqual(f.typesTo("mac-01"), ["schedules"], "and the refused word is not asked again");
    const g = await fleet([["mac-01", "macos", true]], { intercept: refusing });
    await outcome(g.client.schedules({ fresh: true }), BOUND);
    const unpublished = await outcome(g.client.schedules(), BOUND);
    assert.deepEqual([unpublished.state, unpublished.error && unpublished.error.code], ["rejected", "cloud_schedules_unpublished"],
        "a Mac whose snapshot has no schedules field is an old Mac, not an empty list");
});

await check("11 capability · a descriptor that arrives, or changes its commands, clears what that machine was learned to lack; the same descriptor again does not", async function () {
    // Review F4: a Linux executor publishes no app build, so a learned lack outlived its upgrade.
    const refusals = new Map();
    const f = await fleet([["mac-01", "macos", true], ["linux-01", null, true]], { intercept: async function (client, socket, machine, command) {
        if (machine !== "linux-01" || command.type !== "snippets") return false;
        const refuse = refusals.get("linux-01") > 0;
        if (refuse) refusals.set("linux-01", refusals.get("linux-01") - 1);
        await fromMachine(client, socket, "t/linux-01/" + MACHINE_REPLY, refuse
            ? { read: "action:" + command.request, status: 400, error: { code: "unknown_command", message: "no" } }
            : { read: "read:" + command.request, status: 200, body: { snippets: [{ id: "linux-deploy", title: "Deploy" }], at: nowSeconds() } });
        return true;
    } });
    const snippets = async () => {
        const ended = await outcome(f.client.snippets(linuxSession, { fresh: true }), BOUND);
        return ended.state === "resolved" ? "resolved" : ended.state + " " + (ended.error && ended.error.code);
    };
    const descriptor = (commands) => fromMachine(f.client, f.socket, "orch/linux-01", { tasks: [], at: nowSeconds(),
        machine: { name: "linux-01", platform: "linux", commands } });
    refusals.set("linux-01", 1);
    const learned = await snippets();
    await descriptor(["places", "start", "snippets"]);
    const arrived = await snippets();
    refusals.set("linux-01", 1);
    const learnedAgain = await snippets();
    await descriptor(["places", "start", "snippets"]);
    const sameDescriptor = await snippets();
    await descriptor(["places", "start", "snippets", "schedules"]);
    const changed = await snippets();
    assert.deepEqual([learned, arrived, learnedAgain, sameDescriptor, changed],
        ["rejected unknown_command", "resolved", "rejected unknown_command", "rejected cloud_machine_unsupported", "resolved"]);
    assert.deepEqual(f.typesTo("linux-01"), ["snippets", "snippets", "snippets", "snippets"],
        "asked again after the descriptor arrived and after it changed, and not in between");
});

await check("11 capability · a feature only an unpaired machine could provide is a pairing to make, and an unpaired machine that cannot have it is not reported", async function () {
    // Review F5: an unpaired Mac and a paired Linux executor answered "this machine does not support that".
    const f = await fleet(MAC_LINUX);
    f.client._noteUnpairedMachine("mac-01", "mac-sender", "machine_not_paired");
    assert.deepEqual(f.client._machineRows().map((row) => [row.id, row.pairing]), [["linux-01", "paired"], ["mac-01", "not_paired"]]);
    const before = f.publishedCount();
    const key = await outcome(f.client.pushKey(), BOUND);
    const diagnostics = await outcome(f.client.diagnosticsReport({ layout: "phone" }), BOUND);
    assert.deepEqual([key.error && key.error.code, diagnostics.error && diagnostics.error.code, f.publishedCount()],
        ["machine_pairing_required", "machine_pairing_required", before]);
    const g = await fleet(MAC_LINUX);
    g.client._noteUnpairedMachine("linux-01", "linux-sender", "machine_not_paired");
    const fresh = await outcome(g.client.schedules({ fresh: true }), BOUND);
    assert.deepEqual([fresh.state, fresh.value && fresh.value.unanswered], ["resolved", []],
        "an unpaired executor, which cannot have schedules, is nothing the schedules read failed to hear");
    const places = await outcome(g.client.places(), BOUND);
    assert.deepEqual(places.value && places.value.unanswered.map((row) => [row.machine, row.error.code]),
        [["linux-01", "machine_pairing_required"]], "while for places, which it could answer once paired, it is named");
});

/* ---- 12 · Callers of an account-level places() read --------------------------------------- */

/** The Mac refusing `places` as busy while the Linux executor answers it. */
async function macBusyForPlaces(client, socket, machine, command) {
    if (machine !== "mac-01" || command.type !== "places") return false;
    await fromMachine(client, socket, "t/mac-01/" + MACHINE_REPLY, { read: "read:" + command.request, status: 503,
        error: { code: "reading_busy", message: "busy" } });
    return true;
}

await check("12 places callers · a Board conversation whose machine did not answer places fails with that machine's failure, never project_unavailable", async function () {
    // Review F2: the Mac was busy, Linux answered, and the sheet said the Mac's Project was not there.
    const { createBoardSessionController } = await import("../Resources/web/app/js/input/board-session.js");
    const conversation = "11111111-1111-4111-8111-111111111111";
    const project = { id: "project-app", displayPath: "/code/app", label: "app" };
    const opened = async (f, machine) => {
        const kept = new Map();
        const controller = createBoardSessionController({ render: noop, sessions: () => [], canWrite: () => true,
            storage: { getItem: (key) => kept.has(key) ? kept.get(key) : null, setItem: (key, value) => kept.set(key, value) },
            places: () => f.client.places(), history: async () => ({ sessions: [] }) });
        await controller.open(conversation, project, machine);
        return controller.state.error;
    };
    const busy = await fleet(MAC_LINUX, { intercept: macBusyForPlaces });
    assert.deepEqual([await opened(busy, "mac-01"), await opened(busy, null)], ["reading_busy", "reading_busy"],
        "the Session's Mac, or any machine when the Session names none, did not answer");
    const both = await fleet(MAC_LINUX);
    assert.deepEqual([await opened(both, "mac-01"), await opened(both, null)], ["history_unavailable", "history_unavailable"],
        "with every machine answering the Project is found and its history is what is read next");
    const linuxBusy = await fleet(MAC_LINUX, { intercept: async (client, socket, machine, command) =>
        machine === "linux-01" && command.type === "places" && (await fromMachine(client, socket, "t/linux-01/" + MACHINE_REPLY,
            { read: "read:" + command.request, status: 503, error: { code: "ingress_closed", message: "closed" } }), true) });
    assert.equal(await opened(linuxBusy, "mac-01"), "history_unavailable", "another machine's silence does not fail the Mac's Session");
});

await check("12 places callers · the schedules strip does not keep a places answer some machine did not answer", async function () {
    let busy = true;
    const f = await fleet(MAC_LINUX, { intercept: async function (client, socket, machine, command) {
        if (await schedulesWithProjects(client, socket, machine, command)) return true;
        return busy && macBusyForPlaces(client, socket, machine, command);
    } });
    // What the strip asked and what came back, observed rather than arranged: on a loaded machine an
    // answer meant to be complete can come back partial (a machine past its read bound), and a check
    // that assumed otherwise would blame the cache for it.
    let schedulesSettled = 0;
    const answers = [];
    const readSchedules = f.client.schedules.bind(f.client);
    f.client.schedules = function (options) {
        const read = readSchedules(options);
        read.then(() => { schedulesSettled += 1; }, () => { schedulesSettled += 1; });
        return read;
    };
    const readPlaces = f.client.places.bind(f.client);
    f.client.places = function (machine) {
        const read = readPlaces(machine);
        const row = { state: "pending" };
        answers.push(row);
        read.then((answer) => { row.state = answer.unanswered.length || answer.unconfirmed.length ? "partial" : "complete"; },
            () => { row.state = "failed"; });
        return read;
    };
    const driven = await strip(f.client);
    // One refresh, to the end: its schedules read settled, the strip then asked for places or used
    // its cache (a synchronous choice), and any places read it made has settled.
    const refreshed = async () => {
        const settledBefore = schedulesSettled;
        driven.Schedules.refresh();
        await until(() => schedulesSettled > settledBefore, BOUND * 2);
        await settle();
        await until(() => answers.every((row) => row.state !== "pending"), BOUND * 2);
        await settle();
    };
    try {
        driven.forget();
        await refreshed();
        await refreshed();
        assert.deepEqual([answers.length, answers[0] && answers[0].state !== "complete"], [2, true],
            "a partial answer is asked for again at the next refresh: " + JSON.stringify(answers));
        busy = false;
        for (let tries = 0; tries < 4 && answers[answers.length - 1].state !== "complete"; tries++) await refreshed();
        assert.equal(answers[answers.length - 1].state, "complete",
            "a complete places answer was observed to test the cache with: " + JSON.stringify(answers));
        const asked = answers.length;
        await refreshed();
        await refreshed();
        assert.equal(answers.length, asked, "and a complete one is kept: " + JSON.stringify(answers));
    } finally { driven.forget(); driven.restore(); }
});

await check("12 places callers · the schedule form's Project list names the machine that did not answer", async function () {
    const f = await fleet(MAC_LINUX, { intercept: macBusyForPlaces });
    const { useClient } = await import("../Resources/web/app/js/net/api.js");
    const { Schedule } = await import("../Resources/web/app/js/input/schedule.js");
    const macLabel = f.client._machineRows().find((row) => row.id === "mac-01").label;
    useClient(f.client);
    const created = document.createElement;
    document.createElement = function (tag) {
        const made = control(Symbol(tag));
        made.querySelector = function () { return control(Symbol("part")); };
        return made;
    };
    const list = control("schedule-places");
    const notes = () => list.children.filter((child) => child.className === "note").map((child) => child.textContent);
    try {
        list.children = [];
        Schedule.open();
        await until(() => list.children.some((child) => child.className !== "note"), BOUND);
        const said = notes();
        assert.ok(said.length === 1 && said[0].includes(macLabel), "one line naming " + macLabel + ": " + JSON.stringify(said));
        assert.ok(typeof T.webMachinesUnanswered === "string" && T.webMachinesUnanswered.includes("{machines}")
            && said[0] === T.webMachinesUnanswered.replace("{machines}", macLabel), "in the string table's words: " + JSON.stringify(said[0]));
        Schedule.close(true);
        const g = await fleet(MAC_LINUX);
        useClient(g.client);
        list.children = [];
        Schedule.open();
        await until(() => list.children.some((child) => child.className !== "note"), BOUND);
        assert.deepEqual([notes(), list.children.length], [[], 2], "a complete list says nothing extra");
        Schedule.close(true);
    } finally { document.createElement = created; }
});

await check("12 places callers · every page module that reads places() without naming a machine handles a partial answer", async function () {
    const { readdir } = await import("node:fs/promises");
    const root = new URL("../Resources/web/app/js/", import.meta.url);
    const files = (await readdir(root, { recursive: true })).filter((name) => name.endsWith(".js") && !name.includes(".test."));
    // Each exemption says why the file cannot be handed a partial list.
    const EXEMPT = {
        "net/cloud-client.js": "defines places()",
        "net/live.js": "the local route answers for one machine",
        "net/mock.js": "fixtures",
        "main.js": "hands the answer to input/board-session.js whole",
        "input/start.js": "names its machine on Cloud (places(selected.id)); a local page has one"
    };
    const callers = [];
    for (const name of files) {
        const source = await readFile(new URL(name, root), "utf8");
        if (!/\b(?:api|env|transport)\.places\(/.test(source)) continue;
        callers.push(name);
        if (Object.prototype.hasOwnProperty.call(EXEMPT, name)) continue;
        assert.ok(/\bunanswered(?:Sentence)?\b/.test(source), name + " reads places() and never looks at what did not answer");
    }
    // Calibrated against the callers known on 2026-09-15, so a pattern that finds nothing is red.
    for (const known of ["input/board-session.js", "input/command.js", "input/schedule.js", "input/schedule-history.js",
        "view/projects.js", "net/schedules.js", "input/start.js", "main.js"]) {
        assert.ok(callers.includes(known), "the scan finds the known caller " + known + " (" + callers.join(", ") + ")");
    }
});

/* ---- the whole transport against the fault -------------------------------------------------- */

await check("fleet · every public CloudClient method, aimed at Linux or at the account: bounded, and only places/start reach Linux", async function () {
    const f = await fleet(MAC_LINUX);
    const places = await f.client.places();
    const linuxPlace = places.places.find((place) => place.machine === "linux-01").id;
    const macPlace = places.places.find((place) => place.machine === "mac-01").id;
    const SKIP = new Set(["events", "start", "stop", "retire", "subscribe", "whenReady", "refresh",
        "forgetMachinePairingAnswer", "machineAccess", "machineDescriptor"]);
    const ARGS = [[], [linuxSession, "a1", "a2"], ["s-linux-01", "a1", "a2"], ["linux-01", { id: "t" }],
        [linuxPlace, "claude", ""], ["project-app", "item", null, "linux-01"],
        [{ machine: "linux-01", session: "s-linux-01", scope: "project", path: "README.md" }], [macPlace, "claude", ""]];
    const methods = Object.getOwnPropertyNames(CloudClient.prototype)
        .filter((name) => name !== "constructor" && !name.startsWith("_") && !SKIP.has(name));
    assert.ok(methods.length >= 45, "the enumeration reaches the prototype (" + methods.length + ")");
    // One argument set at a time: 400 crypto round trips at once measure this machine's load, not
    // the transport. Every command the Mac answers still has to settle inside the window, and one
    // it does not answer inside its own read bound.
    const calls = [];
    const ended = [];
    for (const args of ARGS) {
        const batch = [];
        for (const name of methods) {
            let result;
            try { result = f.client[name](...args); }
            catch (error) { batch.push({ name, args, sync: error }); continue; }
            batch.push({ name, args, result: outcome(result, BOUND * 3) });
        }
        calls.push(...batch);
        ended.push(...await Promise.all(batch.map(async (call) => Object.assign(call, { ended: call.sync ? null : await call.result }))));
    }
    const pending = ended.filter((call) => call.ended && call.ended.state === "pending")
        .map((call) => call.name + "(" + JSON.stringify(call.args[0]) + ")");
    onlyPlacesAndStartTo(f, "linux-01");
    assert.deepEqual(pending, [], "every call settled within one read bound");
    const refusedHere = ended.filter((call) => call.ended && call.ended.state === "rejected"
        && call.ended.error && call.ended.error.code === "cloud_machine_unsupported").length;
    const toLinux = f.typesTo("linux-01");
    console.log("  fleet enumeration: " + methods.length + " methods, " + calls.length + " calls, "
        + refusedHere + " refused as unsupported before the wire, " + toLinux.length + " commands reached Linux ("
        + Array.from(new Set(toLinux)).join(", ") + ")");
    assert.ok(refusedHere >= 20 && toLinux.includes("places") && toLinux.includes("start"),
        "the enumeration aimed real requests at Linux rather than scanning nothing");
    const started = await outcome(f.client.startPlace(linuxPlace, "claude", ""), FAST);
    assert.equal(started.state, "resolved", "a Linux Project still starts");
});

/* ---- ends ---------------------------------------------------------------------------------- */

if (failures.length) {
    console.log("web cloud fleet checks: " + failures.length + " red, " + passed.length + " green");
    console.log("  " + failures.join("\n  "));
    process.exit(1);
}
console.log("web cloud fleet checks passed: " + passed.length);
process.exit(0);
