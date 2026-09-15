/**
 * A Mac + Linux account on the hosted console: every feature answers with the Mac's data within a
 * bound, and only the six commands in Linux's published browser contract are sent there.
 *
 * On 2026-09-15 the Snippets sheet opened blank on exactly this account, and the audit behind it
 * found the same shape at every site that picked or iterated machines: `_onlyMachine` refused any
 * account with two machines, and the fan-out reads asked the Linux executor for things it has no
 * handler for and then waited out its silence. The fixture here is that account with the fault
 * injected: the Linux machine answers its six commands and **never replies to anything else**,
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
// A ceiling, not a slice: `until` and `outcome` return the moment their condition holds, so a green run
// is no slower, and a loaded machine (load average 15–27 on 2026-09-15) cannot end the wait before an
// answer that needs no timeout has arrived. The checks added with the phone-heat work wait with this.
const WAIT = 20000;
const LINUX_ANSWERS = new Set(["info", "places", "screen", "send", "start", "transcript"]);
const nowSeconds = () => Math.floor(Date.now() / 1000);

/* ---- the relay, the Mac and the Linux executor ---------------------------------------------- */

class RelaySocket {
    static latest = null;
    constructor() { this.readyState = 1; this.sent = []; this.onPublish = null; this.halfOpen = false; RelaySocket.latest = this; }
    send(text) {
        const frame = JSON.parse(text);
        this.sent.push(frame);
        // The relay answers a ping itself, even from a hibernating object. A socket left half-open by
        // a network change still takes frames and hears nothing back.
        if (frame.type === "ping" && !this.halfOpen) Promise.resolve().then(() => this.receive({ type: "pong" }));
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
            const machineRead = "t/" + machine + "/" + MACHINE_REPLY;
            const sessionRead = "t/" + machine + "/" + encodeURIComponent(command.session);
            if (command.type === "places") await fromMachine(client, socket, machineRead,
                { read: "read:" + command.request, status: 200, body: {
                    places: [{ id: "reaver", label: "reaver", path: "/srv/reaver" }],
                    assistants: [{ id: "claude", label: "claude", availability: "available" }] } });
            else if (command.type === "transcript") await fromMachine(client, socket, sessionRead,
                { read: "transcript", status: 200, body: { entries: [], signature: "linux-transcript" } });
            else if (command.type === "screen") await fromMachine(client, socket, sessionRead,
                { read: "screen", status: 200, body: { screen: { text: "linux screen", readable: true } } });
            else if (command.type === "send") await fromMachine(client, socket, sessionRead,
                { read: "action:" + command.request, status: 200, body: { ok: true } });
            else await fromMachine(client, socket, machineRead,
                { read: "action:" + command.request, status: 200,
                    body: { ok: true, id: "linux-new", backend: "tmux" } });
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
function onlyLinuxCommandsTo(fixture, machine) {
    const others = fixture.typesTo(machine).filter((type) => !LINUX_ANSWERS.has(type));
    assert.deepEqual(others, [], "only Linux's advertised commands were published toward " + machine);
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
    onlyLinuxCommandsTo(f, "linux-01");
});

await check("1 snippets · a retained Mac snapshot without the field asks that Mac, and only that Mac", async function () {
    const f = await fleet([["mac-01", "macos", true], ["linux-01", "linux", true]]);
    const ended = await outcome(f.client.snippets(macSession), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual(ended.value.snippets.map((row) => row.machine), ["mac-01"]);
    assert.deepEqual(f.typesTo("mac-01"), ["snippets"]);
    onlyLinuxCommandsTo(f, "linux-01");
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
    onlyLinuxCommandsTo(f, "linux-01");
});

/* ---- 2 · Schedules --------------------------------------------------------------------------- */

await check("2 schedules · a visible refresh asks the Mac and never the Linux executor", async function () {
    const f = await fleet(MAC_LINUX);
    const ended = await outcome(f.client.schedules({ fresh: true }), FAST);
    assert.equal(ended.state, "resolved", ended.state + (ended.error ? " " + ended.error.code : ""));
    assert.deepEqual(ended.value.schedules.map((row) => [row.id, row.machine]), [["mac-01-morning", "mac-01"]]);
    assert.deepEqual(f.typesTo("mac-01"), ["schedules"]);
    onlyLinuxCommandsTo(f, "linux-01");
});

await check("2 schedules · a Linux-only account has no schedules to show rather than an old-Mac refusal", async function () {
    const f = await fleet([["linux-01", "linux", true]]);
    const retained = await outcome(f.client.schedules(), FAST);
    const fresh = await outcome(f.client.schedules({ fresh: true }), FAST);
    assert.deepEqual([retained.state, retained.value && retained.value.schedules, fresh.state, fresh.value && fresh.value.schedules],
        ["resolved", [], "resolved", []]);
    onlyLinuxCommandsTo(f, "linux-01");
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
        onlyLinuxCommandsTo(f, "linux-01");
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
    onlyLinuxCommandsTo(f, "linux-01");
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
    onlyLinuxCommandsTo(f, "linux-01");
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
    onlyLinuxCommandsTo(f, "linux-01");
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
    onlyLinuxCommandsTo(f, "linux-01");
});

/* ---- 7 · Project worktree lifecycle --------------------------------------------------------- */

await check("7 worktrees · lifecycle and refresh with no machine reach the Mac, and two Macs refuse without publishing", async function () {
    const f = await fleet(MAC_LINUX);
    const read = await outcome(f.client.projectWorktreeLifecycle("project-app"), FAST);
    const refresh = await outcome(f.client.projectWorktreeLifecycleRefresh("project-app"), FAST);
    assert.deepEqual([read.state, read.value && read.value.machine, refresh.state, refresh.value && refresh.value.machine],
        ["resolved", "mac-01", "resolved", "mac-01"]);
    onlyLinuxCommandsTo(f, "linux-01");
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
    onlyLinuxCommandsTo(f, "linux-01");
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
    // Asked is the claim, not how fast it answered: a machine past its read bound on a loaded run is
    // in `unanswered`, which is still a machine that was asked.
    const heard = new Set(ended.value.places.map((place) => place.machine).concat(ended.value.unanswered.map((row) => row.machine)));
    assert.deepEqual([Array.from(heard).sort(), ended.value.unconfirmed],
        [["linux-01", "mac-01"], []], "both machines asked for their Projects, and nobody left unasked");
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
    // About the unpaired executor's row only: the Mac is asked too, and a loaded run can make it late.
    const executorRows = (ended) => ended.state === "resolved"
        ? ended.value.unanswered.filter((row) => row.machine === "linux-01").map((row) => row.error.code)
        : ended.state + " " + (ended.error && ended.error.code);
    const fresh = await outcome(g.client.schedules({ fresh: true }), BOUND);
    assert.deepEqual(executorRows(fresh), [],
        "an unpaired executor, which cannot have schedules, is nothing the schedules read failed to hear");
    const places = await outcome(g.client.places(), BOUND);
    assert.deepEqual(executorRows(places), ["machine_pairing_required"],
        "while for places, which it could answer once paired, it is named");
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
    // What the sheet ended with, and which machines the places read it made did not hear from.
    const opened = async (f, machine) => {
        const kept = new Map();
        let inventory = null;
        const controller = createBoardSessionController({ render: noop, sessions: () => [], canWrite: () => true,
            storage: { getItem: (key) => kept.has(key) ? kept.get(key) : null, setItem: (key, value) => kept.set(key, value) },
            places: async () => (inventory = await f.client.places()), history: async () => ({ sessions: [] }) });
        await controller.open(conversation, project, machine);
        return { error: controller.state.error, silent: inventory ? inventory.unanswered.map((row) => row.machine).sort() : null };
    };
    // Each arm is about a premise — which machines answered — that a loaded machine can break by
    // pushing one past its read bound. The arm is asked again until the premise held, and says so if
    // it never did, rather than blaming the sheet for the environment.
    const arm = async (f, machine, silent) => {
        const seen = [];
        for (let tries = 0; tries < 3; tries++) {
            const ended = await opened(f, machine);
            seen.push(ended);
            if (JSON.stringify(ended.silent) === JSON.stringify(silent)) return ended.error;
        }
        return "premise never held: " + JSON.stringify(seen);
    };
    const busy = await fleet(MAC_LINUX, { intercept: macBusyForPlaces });
    assert.deepEqual([await arm(busy, "mac-01", ["mac-01"]), await arm(busy, null, ["mac-01"])], ["reading_busy", "reading_busy"],
        "the Session's Mac, or any machine when the Session names none, did not answer");
    const both = await fleet(MAC_LINUX);
    assert.deepEqual([await arm(both, "mac-01", []), await arm(both, null, [])], ["history_unavailable", "history_unavailable"],
        "with every machine answering the Project is found and its history is what is read next");
    const linuxBusy = await fleet(MAC_LINUX, { intercept: async (client, socket, machine, command) =>
        machine === "linux-01" && command.type === "places" && (await fromMachine(client, socket, "t/linux-01/" + MACHINE_REPLY,
            { read: "read:" + command.request, status: 503, error: { code: "ingress_closed", message: "closed" } }), true) });
    assert.equal(await arm(linuxBusy, "mac-01", ["linux-01"]), "history_unavailable", "another machine's silence does not fail the Mac's Session");
});

await check("12 places callers · the schedules strip keeps a places answer some machine did not answer, and does not re-ask it every refresh", async function () {
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
        // What answered is kept; the machine that did not is asked on its own after the retry window
        // (`createPlacesCache`, held with a clock in `Tests/web-schedules.mjs`), not with every refresh.
        assert.deepEqual([answers.length, answers[0] && answers[0].state !== "complete"], [1, true],
            "a partial answer is kept for the next refresh: " + JSON.stringify(answers));
        busy = false;
        driven.forget();
        for (let tries = 0; tries < 4 && answers[answers.length - 1].state !== "complete"; tries++) {
            driven.forget();
            await refreshed();
        }
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
    // The premise of this arm is that only the Mac did not answer; on a loaded run the executor can be
    // late too, and the sheet would rightly name both. It is opened again until the premise held.
    let silent = null;
    const readBusy = f.client.places.bind(f.client);
    f.client.places = (machine) => readBusy(machine).then((answer) => {
        silent = answer.unanswered.map((row) => row.machine).join(","); return answer;
    });
    try {
        for (let tries = 0; tries < 3 && silent !== "mac-01"; tries++) {
            if (tries) Schedule.close(true);
            silent = null;
            list.children = [];
            Schedule.open();
            await until(() => silent !== null && list.children.some((child) => child.className !== "note"), BOUND);
            await settle();
        }
        assert.equal(silent, "mac-01", "an answer in which only the Mac did not answer was observed");
        const said = notes();
        assert.ok(said.length === 1 && said[0].includes(macLabel), "one line naming " + macLabel + ": " + JSON.stringify(said));
        assert.ok(typeof T.webMachinesUnanswered === "string" && T.webMachinesUnanswered.includes("{machines}")
            && said[0] === T.webMachinesUnanswered.replace("{machines}", macLabel), "in the string table's words: " + JSON.stringify(said[0]));
        Schedule.close(true);
        // The other arm's premise is that every machine answered, which a loaded run can break; it is
        // opened again while the answer it drew from was partial.
        const g = await fleet(MAC_LINUX);
        let partial = null;
        const readPlaces = g.client.places.bind(g.client);
        g.client.places = (machine) => readPlaces(machine).then((answer) => { partial = answer.unanswered.length > 0; return answer; });
        useClient(g.client);
        for (let tries = 0; tries < 3 && partial !== false; tries++) {
            partial = null;
            list.children = [];
            Schedule.open();
            await until(() => partial !== null && list.children.some((child) => child.className !== "note"), BOUND);
            await settle();
            if (partial !== false) Schedule.close(true);
        }
        assert.equal(partial, false, "a complete places answer was observed to draw the list from");
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

await check("fleet · every public CloudClient method is bounded and only supported commands reach Linux", async function () {
    const f = await fleet(MAC_LINUX);
    const places = await f.client.places();
    const linuxPlace = places.places.find((place) => place.machine === "linux-01").id;
    const macPlace = places.places.find((place) => place.machine === "mac-01").id;
    // `voice` owns a six-minute transcription deadline and has dedicated host-selection/bounded
    // failure coverage. Calling it here with each generic argument tuple turns this read-bound
    // enumeration into a scheduler-speed test under the full suite rather than a fleet gate.
    const SKIP = new Set(["events", "start", "stop", "retire", "subscribe", "whenReady", "refresh", "voice",
        "forgetMachinePairingAnswer", "machineAccess", "machineDescriptor"]);
    const ARGS = [[], [linuxSession, "a1", "a2"], ["s-linux-01", "a1", "a2"], ["linux-01", { id: "t" }],
        [linuxPlace, "claude", ""], ["project-app", "item", null, "linux-01"],
        [{ machine: "linux-01", session: "s-linux-01", scope: "project", path: "README.md" }], [macPlace, "claude", ""]];
    const methods = Object.getOwnPropertyNames(CloudClient.prototype)
        .filter((name) => name !== "constructor" && !name.startsWith("_") && !SKIP.has(name));
    assert.ok(methods.length >= 45, "the enumeration reaches the prototype (" + methods.length + ")");
    // One call at a time: hundreds of simultaneous WebCrypto seals measure the runner's scheduler,
    // not whether a route has a bounded answer. Every command the Mac answers still has to settle
    // inside the window, and one it does not answer inside its own read bound.
    const calls = [];
    const ended = [];
    for (const args of ARGS) {
        for (const name of methods) {
            let result;
            try { result = f.client[name](...args); }
            catch (error) {
                const call = { name, args, sync: error, ended: null };
                calls.push(call);
                ended.push(call);
                continue;
            }
            const call = { name, args, result: outcome(result, BOUND * 3) };
            call.ended = await call.result;
            calls.push(call);
            ended.push(call);
        }
    }
    const pending = ended.filter((call) => call.ended && call.ended.state === "pending")
        .map((call) => call.name + "(" + JSON.stringify(call.args[0]) + ")");
    onlyLinuxCommandsTo(f, "linux-01");
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

/* ---- the transport's repeated work: subscriptions, retained answers, offline, pictures ------- */

// One Mac with a clock this suite moves and a relay answer the check writes. Measured origin: a
// phone reading the same transcript and the same `image.<id>` over and over (2026-09-15).
async function quietMac() {
    let clock = 1_789_500_000_000;
    const client = fleetClient({ now: () => clock });
    const socket = await ready(client);
    const fixture = { client, socket, published: [], answer: null,
        advance: (ms) => { clock += ms; },
        frames: (type) => socket.sent.filter((frame) => frame.type === type) };
    socket.onPublish = async function (envelope) {
        const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
        fixture.published.push({ envelope, command });
        if (fixture.answer) await fixture.answer(envelope, command);
    };
    await fromMachine(client, socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: CLOUD_STATUS, schedules: [] });
    return fixture;
}

let retainedSequence = 100;
async function retained(fixture, channel, payload) {
    const envelope = await sealEnvelope({ ch: channel, seq: ++retainedSequence, ts: 1787817600000, class: "stream",
        key_id: "ms-1", sender: DEVICE }, JSON.stringify(payload), masterKey, signingKey);
    fixture.socket.receive({ type: "envelope", realign: true, envelope });
    await fixture.client.messageChain.catch(noop);
}

const PICTURE = { media_type: "image/png", data: "iVBORw==", byte_count: 4 };

await check("transport · a held channel is not subscribed again, and the relay's eight are never exceeded", async function () {
    const f = await quietMac();
    f.answer = async (envelope, command) => {
        if (command.type !== "transcript") return;
        await fromMachine(f.client, f.socket, "t/mac-01/" + encodeURIComponent(command.session),
            { read: "transcript", status: 200, body: { entries: [], signature: "sig-" + command.session } });
    };
    for (let i = 0; i < 3; i += 1) {
        const read = await outcome(f.client.transcript({ machine: "mac-01", session: "s1" }), WAIT);
        assert.equal(read.state, "resolved", "read " + i + " " + read.state);
    }
    assert.deepEqual(f.frames("subscribe").flatMap((frame) => frame.channels), ["t/mac-01/s1"],
        "three reads of one Session subscribe once");
    for (let i = 0; i < 10; i += 1) {
        const read = await outcome(f.client.transcript({ machine: "mac-01", session: "n" + i }), WAIT);
        assert.equal(read.state, "resolved", "session n" + i + " " + read.state);
    }
    const held = new Set();
    let most = 0;
    f.socket.sent.forEach((frame) => {
        if (frame.type !== "subscribe" && frame.type !== "unsubscribe") return;
        assert.ok(frame.channels.length >= 1 && frame.channels.length <= 8, "a frame the relay accepts");
        frame.channels.forEach((channel) => frame.type === "subscribe" ? held.add(channel) : held.delete(channel));
        most = Math.max(most, held.size);
    });
    assert.ok(most <= 8, "never more than the relay's eight on one socket: " + most);
    assert.ok(held.has("t/mac-01/n9"), "the channel being read is held");
    // Two quiet minutes: the next read lets go of every channel nobody is reading.
    f.advance(2 * 60 * 1000);
    assert.equal((await outcome(f.client.transcript({ machine: "mac-01", session: "later" }), WAIT)).state, "resolved");
    const released = f.frames("unsubscribe").at(-1).channels;
    assert.equal(released.length, 8, "all eight idle channels are released: " + JSON.stringify(released));

    const early = fleetClient();
    early.subscribe(Array.from({ length: 10 }, (_, i) => "t/mac-01/early-" + i));
    const earlySocket = await ready(early);
    const sent = earlySocket.sent.filter((frame) => frame.type === "subscribe");
    assert.deepEqual(sent.map((frame) => frame.channels.length), [8],
        "subscriptions made before ready go out as one frame the relay accepts, newest eight");
    assert.equal(sent[0].channels[7], "t/mac-01/early-9");
    early.stop();
});

await check("transport · with eight channels each waited on, a read on a ninth is refused at once as busy and sends nothing", async function () {
    const f = await quietMac();
    const answer = (session) => fromMachine(f.client, f.socket, "t/mac-01/" + session,
        { read: "transcript", status: 200, body: { entries: [], signature: "sig-" + session } });
    const waiting = Array.from({ length: 8 }, (_, i) => f.client.transcript({ machine: "mac-01", session: "w" + i }));
    waiting.forEach((read) => read.catch(noop));
    await until(() => f.published.length === 8, WAIT);
    assert.equal(f.published.length, 8, "eight reads on eight channels went out");
    // The relay refuses a ninth subscription with an error frame naming no request (account-do.ts);
    // sent anyway, this read would wait out its whole read timeout.
    const ninth = await outcome(f.client.transcript({ machine: "mac-01", session: "ninth" }), WAIT);
    assert.deepEqual([ninth.state, ninth.error && ninth.error.code, ninth.error && ninth.error.layer, ninth.error && ninth.error.retryable],
        ["rejected", "cloud_read_busy", "browser", true], "refused now, and retryable");
    assert.equal(describeFailure(ninth.error).text, T.webFailMacBusy);
    assert.equal(f.published.length, 8, "nothing was published for it");
    assert.ok(!f.frames("subscribe").some((frame) => frame.channels.includes("t/mac-01/ninth")), "nor subscribed");
    const joining = f.client.infoSummary({ machine: "mac-01", session: "w3" });
    joining.catch(noop);
    await until(() => f.published.length === 9, WAIT);
    assert.equal(f.published.length, 9, "a read on a channel already held still goes out");
    await answer("w0");
    assert.equal((await outcome(waiting[0], WAIT)).state, "resolved");
    const again = f.client.transcript({ machine: "mac-01", session: "ninth" });
    await until(() => f.published.length === 10, WAIT);
    assert.equal(f.published.length, 10, "once one settles, asking again fits");
    await answer("ninth");
    assert.equal((await outcome(again, WAIT)).state, "resolved");
    const held = new Set();
    let most = 0;
    f.socket.sent.forEach((frame) => {
        if (frame.type !== "subscribe" && frame.type !== "unsubscribe") return;
        frame.channels.forEach((channel) => frame.type === "subscribe" ? held.add(channel) : held.delete(channel));
        most = Math.max(most, held.size);
    });
    assert.ok(most <= 8, "never more than the relay's eight on one socket: " + most);

    const early = fleetClient();
    await early.start();
    const reads = Array.from({ length: 9 }, (_, i) => early.transcript({ machine: "mac-01", session: "e" + i }));
    reads.forEach((read) => read.catch(noop));
    assert.ok(early.pendingSubscriptions.size <= 8, "before ready, what waits to be subscribed stays under the ceiling: "
        + early.pendingSubscriptions.size);
    const ninthEarly = await outcome(reads[8], WAIT);
    assert.equal(ninthEarly.error && ninthEarly.error.code, "cloud_read_busy",
        "and a ninth channel waited on is refused as busy there too: " + ninthEarly.state);
    early.subscribe(Array.from({ length: 10 }, (_, i) => "t/mac-01/later-" + i));
    assert.deepEqual([early.pendingSubscriptions.size, Array.from(early.pendingSubscriptions).at(-1)], [8, "t/mac-01/later-9"],
        "a subscription made before ready forgets the oldest rather than growing past the ceiling");
    early.stop();
});

await check("transport · a read asked of a retired client while the page is hidden waits a few seconds for it, then refuses as before", async function () {
    // A notification tap: the service worker posts `navigate` before `focus()` shows the page.
    const timers = new Map();
    let timerID = 0;
    const manual = { setTimeout: (fn, ms) => { timers.set(++timerID, { fn, ms }); return timerID; },
        clearTimeout: (id) => { timers.delete(id); } };
    const fire = () => { const [id, timer] = timers.entries().next().value; timers.delete(id); timer.fn(); };
    const pending = () => Array.from(timers.values()).map((timer) => timer.ms);
    const retiredWhile = async (page) => {
        const client = fleetClient(Object.assign({ readTimeoutMs: 60_000 }, manual));
        await ready(client);
        client.lifecycle = () => page.shown ? true : "hidden";
        client.retire();
        return client;
    };

    const stays = { shown: false };
    const away = await retiredWhile(stays);
    const tapped = away.transcript({ machine: "mac-01", session: "s1" });
    assert.equal((await outcome(tapped, 60)).state, "pending", "held while the page is still hidden, not refused");
    assert.deepEqual(pending(), [5_000], "for a few seconds, not for the read bound");
    fire();
    const refused = await outcome(tapped, WAIT);
    assert.deepEqual([refused.state, refused.error && refused.error.code], ["rejected", "cloud_reconnecting"],
        "still hidden then: refused as before");
    assert.deepEqual([pending(), away.successorWaiters.length], [[], 0], "and nothing it set is left");

    const comes = { shown: false };
    const shown = await retiredWhile(comes);
    const opened = shown.transcript({ machine: "mac-01", session: "s2" });
    await settle();
    comes.shown = true;
    fire();
    assert.deepEqual(pending(), [55_000], "shown by then: the rest of the read bound, for the connection that brings");
    const renewed = fleetClient({ resumeFrom: shown });
    const socket = await ready(renewed);
    socket.onPublish = async function (envelope) {
        const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
        await fromMachine(renewed, socket, "t/mac-01/" + encodeURIComponent(command.session),
            { read: "transcript", status: 200, body: { entries: [], signature: "on-renewed" } });
    };
    const read = await outcome(opened, WAIT);
    assert.deepEqual([read.state, read.value && read.value.signature], ["resolved", "on-renewed"],
        "the tapped Session's read runs on the client the page came back with");
    assert.deepEqual(pending(), [], "and its wait is cleared");
    renewed.stop();
});

await check("transport · a retained t/ answer settles only a request it names, or a picture by its id", async function () {
    const f = await quietMac();
    const reading = f.client.transcript({ machine: "mac-01", session: "s1" });
    await until(() => f.published.length === 1, WAIT);
    await retained(f, "t/mac-01/s1", { read: "transcript", status: 200, body: { entries: [], signature: "retained" } });
    assert.equal((await outcome(reading, 60)).state, "pending",
        "the channel's last answer — minutes old, maybe another device's — is not this read's answer");
    await fromMachine(f.client, f.socket, "t/mac-01/s1", { read: "transcript", status: 200,
        body: { entries: [], signature: "live" } });
    const read = await outcome(reading, WAIT);
    assert.deepEqual([read.state, read.value && read.value.signature], ["resolved", "live"],
        "the Mac's answer to this read settles it");

    const picture = f.client.image({ machine: "mac-01", session: "s1" }, "a-picture");
    await until(() => f.published.length === 2, WAIT);
    await retained(f, "t/mac-01/s1", Object.assign({ read: "image.a-picture", status: 200 },
        { body: Object.assign({ id: "a-picture" }, PICTURE) }));
    assert.equal((await outcome(picture, WAIT)).state, "resolved", "an image id names bytes that never change");

    const status = f.client.cloudStatus("mac-01");
    await until(() => f.published.length === 3, WAIT);
    const request = f.published[2].command.request;
    await retained(f, "t/mac-01/" + MACHINE_REPLY, { read: "read:" + request, status: 200,
        body: Object.assign({}, CLOUD_STATUS, { commands: [] }) });
    assert.equal((await outcome(status, WAIT)).state, "resolved", "an answer carrying this page's request id is this request's");
});

await check("transport · machine_offline is remembered briefly: a re-ask fails here, and a live envelope ends it", async function () {
    const f = await quietMac();
    let offline = true;
    f.answer = async (envelope, command) => {
        if (offline) {
            f.socket.receive({ type: "ack", ch: envelope.ch, seq: envelope.seq, status: "machine_offline" });
            await f.client.messageChain.catch(noop);
            return;
        }
        f.socket.receive({ type: "ack", ch: envelope.ch, seq: envelope.seq, status: "delivered" });
        await fromMachine(f.client, f.socket, "t/mac-01/" + MACHINE_REPLY, { read: "read:" + command.request,
            status: 200, body: Object.assign({}, CLOUD_STATUS, { commands: [] }) });
    };
    const code = async () => {
        const ended = await outcome(f.client.cloudStatus("mac-01"), WAIT);
        return ended.state === "rejected" ? ended.error.code : ended.state;
    };
    assert.equal(await code(), "machine_offline");
    assert.equal(f.published.length, 1);
    const again = await outcome(f.client.cloudStatus("mac-01"), WAIT);
    assert.deepEqual([again.state, again.error && again.error.code, again.error && again.error.layer, again.error && again.error.retryable],
        ["rejected", "machine_offline", "relay", true], "said again in the relay's own word");
    assert.equal(f.published.length, 1, "without a second envelope");
    const fresh = await outcome(f.client.schedules({ fresh: true }), WAIT);
    assert.equal(f.published.length, 1, "the schedules refresh does not ask it either");
    assert.deepEqual([fresh.state, fresh.error && fresh.error.code], ["rejected", "machine_offline"],
        "and, as the only machine asked, refuses with its word");
    f.advance(5_000);
    assert.equal(await code(), "machine_offline");
    assert.equal(f.published.length, 2, "asked again once the first window passes");
    f.advance(5_000);
    assert.equal(await code(), "machine_offline");
    assert.equal(f.published.length, 2, "the window doubles while it stays offline");
    f.advance(5_000);
    assert.equal(await code(), "machine_offline");
    assert.equal(f.published.length, 3);
    await retained(f, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" } });
    assert.equal(await code(), "machine_offline", "a retained snapshot is not the machine talking now");
    assert.equal(f.published.length, 3);
    offline = false;
    await fromMachine(f.client, f.socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: CLOUD_STATUS });
    assert.equal(await code(), "resolved", "a live envelope from the machine ends the wait at once");
    assert.equal(f.published.length, 4);
});

await check("transport · a picture read once is drawn from memory; a refusal is not kept", async function () {
    const f = await quietMac();
    f.answer = async (envelope, command) => {
        if (command.type !== "image") return;
        const payload = command.id === "too-big"
            ? { read: "image." + command.id, status: 413, error: { code: "image_too_large_for_cloud", message: "big" } }
            : { read: "image." + command.id, status: 200, body: Object.assign({ id: command.id }, PICTURE) };
        await fromMachine(f.client, f.socket, "t/mac-01/s1", payload);
    };
    const identity = { machine: "mac-01", session: "s1" };
    const first = await outcome(f.client.image(identity, "p-1"), WAIT);
    const second = await outcome(f.client.image(identity, "p-1"), WAIT);
    assert.deepEqual([first.state, second.state], ["resolved", "resolved"]);
    assert.deepEqual(Array.from(second.value.bytes), [137, 80, 78, 71]);
    assert.equal(f.published.length, 1, "the second draw of the same picture sends nothing");
    const other = await outcome(f.client.image({ machine: "mac-01", session: "s2" }, "p-1"), WAIT);
    assert.equal(other.state, "resolved");
    assert.equal(f.published.length, 1, "the id names the bytes on its machine, whichever Session shows them");
    assert.equal((await outcome(f.client.image(identity, "too-big"), WAIT)).error.code, "image_too_large_for_cloud");
    assert.equal((await outcome(f.client.image(identity, "too-big"), WAIT)).error.code, "image_too_large_for_cloud");
    assert.equal(f.published.length, 3, "a refusal is asked again");
    const renewed = fleetClient({ resumeFrom: f.client });
    await ready(renewed);
    assert.equal((await outcome(renewed.image(identity, "p-1"), WAIT)).state, "resolved");
    assert.equal(RelaySocket.latest.sent.filter((frame) => frame.type === "publish").length, 0,
        "a token renewal keeps the pictures");
    renewed.stop();
});

await check("transport · a read on a client retired while the page was away runs on the client that resumed from it", async function () {
    const f = await quietMac();
    const answerOn = (fixture) => async (envelope, command) => {
        if (command.type !== "transcript") return;
        await fromMachine(fixture.client, fixture.socket, "t/mac-01/" + encodeURIComponent(command.session),
            { read: "transcript", status: 200, body: { entries: [], signature: "on-" + fixture.name } });
    };
    // A notification tap lands in the gap between the page coming back and the new client installed.
    let resuming = true;
    f.client.lifecycle = () => resuming;
    f.client.retire();
    const tapped = f.client.transcript({ machine: "mac-01", session: "s1" });
    assert.equal((await outcome(tapped, 60)).state, "pending", "held, not refused as cloud_reconnecting");
    const renewed = fleetClient({ resumeFrom: f.client });
    const renewedSocket = await ready(renewed);
    const second = { client: renewed, socket: renewedSocket, name: "renewed" };
    renewedSocket.onPublish = async function (envelope) {
        const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
        await answerOn(second)(envelope, command);
    };
    const read = await outcome(tapped, WAIT);
    assert.deepEqual([read.state, read.value && read.value.signature], ["resolved", "on-renewed"],
        "the held read went out on the replacement socket and settled there");
    assert.equal(f.published.length, 0, "nothing was published on the retired socket");
    const later = await outcome(f.client.transcript({ machine: "mac-01", session: "s2" }), WAIT);
    assert.deepEqual([later.state, later.value && later.value.signature], ["resolved", "on-renewed"],
        "a call after the replacement is ready runs there at once");

    const hidden = await quietMac();
    hidden.client.lifecycle = () => false;
    hidden.client.retire();
    const refused = await outcome(hidden.client.transcript({ machine: "mac-01", session: "s1" }), WAIT);
    assert.deepEqual([refused.state, refused.error && refused.error.code], ["rejected", "cloud_reconnecting"],
        "with nothing on its way — the page is hidden — it is refused at once, as before");
    resuming = false;
});

/* ---- B1 · an idle Mac's rows after a (re)connection ------------------------------------------ */

// The failure modelled: a Mac whose rows no longer change publishes nothing, and the relay replays a
// channel only from the memory it loses with its object — so each socket below is ready with no
// replay at all. The Mac answers `sessions.snapshot` the way `CloudAppBridge` does: every row on its
// own `s/` channel, the inventory with `features`, then `read:<request>` naming the ids.
const { createTranscriptRefetchPolicy, createTranscriptRevisionObserver } =
    await import("../Resources/web/app/js/session/transcript-requests.js");

function storageBox(seed) {
    const box = new Map(Object.entries(seed || {}));
    return { getItem: (key) => box.has(key) ? box.get(key) : null, setItem: (key, value) => box.set(key, String(value)) };
}
const REMEMBERED_SNAPSHOT_MAC = { ["clawdline.machine-descriptors.v1:" + encodeURIComponent(ACCOUNT)]: JSON.stringify({ v: 1,
    machines: [{ id: "mac-01", machine: { name: "Mac", platform: "macos" }, build: "1", at_ms: 1, features: ["sessions.snapshot"] }] }) };

/** The page's reader: every list lands here, and the open Session is read by the real refetch policy. */
function sessionReader(open) {
    const policy = createTranscriptRefetchPolicy({ lineIntervalMs: 15000 });
    const reader = { client: null, reads: 0, lists: [] };
    const observer = createTranscriptRevisionObserver(function (id, revision) {
        reader.reads += 1;
        reader.client.transcript({ machine: "mac-01", session: id }).then(
            () => observer.settle(id, revision, true), (error) => observer.settle(id, revision, false, error));
    }, { retryDelay: 20 });
    reader.handlers = { conn: noop, hello: noop, tasks: noop, sessions: function (list, _at, scan) {
        reader.lists.push({ rows: list.map((row) => row.id).sort(), scan: scan });
        const row = list.find((candidate) => candidate.id === open);
        if (row) observer.observe(open, policy.revision(open, row), true);
        return true;
    } };
    reader.claimedEmpty = () => reader.lists.filter((list) => !list.rows.length && list.scan.emptyAuthoritative === true);
    return reader;
}

/** One socket of a viewer to an idle Mac; `mac.rows` is what that Mac holds now. */
async function idleMacSocket(mac, reader, options) {
    const client = fleetClient(Object.assign({ handlers: reader.handlers, sessionSnapshotSettleMs: 20 }, options));
    reader.client = client;
    const socket = await ready(client);
    const sent = { snapshots: 0, transcripts: 0, orchestrator: [] };
    socket.onPublish = async function (envelope) {
        const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
        if (command.type === "transcript") {
            sent.transcripts += 1;
            await fromMachine(client, socket, "t/mac-01/" + encodeURIComponent(command.session),
                { read: "transcript", status: 200, body: { entries: [], signature: mac.rows.get(command.session).transcript_signature } });
        } else if (command.type === "sessions.snapshot") {
            sent.snapshots += 1;
            sent.orchestrator.push(command.orchestrator === true);
            if (mac.silent) return;
            for (const row of mac.rows.values()) {
                if (mac.lose === row.id) continue;
                await fromMachine(client, socket, "s/mac-01/" + encodeURIComponent(row.id), { session: row, at: 100, scan: {} });
            }
            await fromMachine(client, socket, "s/mac-01/__clawdline_inventory_v1__",
                { inventory: { version: 1, sessions: Array.from(mac.rows.keys()).sort() }, features: ["sessions.snapshot"] });
            await fromMachine(client, socket, "t/mac-01/" + MACHINE_REPLY, { read: "read:" + command.request, status: 200,
                body: { sessions: Array.from(mac.rows.keys()).sort(), complete: true } });
        }
    };
    return { client, socket, sent };
}

await check("B1 · a cold viewer of an idle Mac with nothing replayed asks for the rows and has them all within the bound", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }],
        ["s2", { id: "s2", state: "idle", transcript_signature: "20-1" }]]) };
    const reader = sessionReader("s1");
    const viewer = await idleMacSocket(mac, reader, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC) });
    await until(() => { const last = reader.lists[reader.lists.length - 1]; return last && last.rows.length === 2 && last.scan.emptyAuthoritative; }, WAIT);
    const last = reader.lists[reader.lists.length - 1];
    assert.deepEqual([last && last.rows, last && last.scan.emptyAuthoritative, last && last.scan.recovering],
        [["s1", "s2"], true, []], "the list converged to the Mac's rows: " + JSON.stringify(reader.lists));
    // Within the bound: by the first answer — no timeout, retry or failure on the way — not by a clock.
    assert.deepEqual(reader.lists.flatMap((list) => list.scan.failures), [], "no attempt failed on the way");
    assert.equal(viewer.sent.snapshots, 1, "one request, from the machine this browser remembered answering it");
    assert.deepEqual(reader.claimedEmpty(), [], "no list said the account is empty while the Mac's rows were on their way");
    assert.equal(reader.lists[0].scan.recovering[0], "mac-01", "the first list says that Mac is still sending");
    await until(() => viewer.sent.transcripts === 1, WAIT);
    assert.equal(viewer.sent.transcripts, 1, "the open Session is read once");
});

await check("B1 · a viewer that has never seen the Mac asks once its snapshot says it can, and an older Mac is never asked", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const reader = sessionReader(null);
    const viewer = await idleMacSocket(mac, reader, { descriptorStorage: storageBox() });
    assert.equal(viewer.sent.snapshots, 0, "nothing is known to answer yet, so nothing is asked");
    await fromMachine(viewer.client, viewer.socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: Object.assign({}, CLOUD_STATUS, { features: ["sessions.snapshot"] }) });
    await until(() => { const last = reader.lists[reader.lists.length - 1]; return last && last.rows.length === 1 && last.scan.emptyAuthoritative; }, WAIT);
    assert.equal(viewer.sent.snapshots, 1, "the Mac's own digest named the word, and it was asked once");
    assert.deepEqual(reader.claimedEmpty(), [], JSON.stringify(reader.lists));
    await fromMachine(viewer.client, viewer.socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: Object.assign({}, CLOUD_STATUS, { features: ["sessions.snapshot"] }) });
    await until(() => viewer.socket.sent.filter((frame) => frame.type === "publish").length > 1, 400);
    assert.equal(viewer.socket.sent.filter((frame) => frame.type === "publish").length, 1,
        "and not again for the next snapshot on the same socket");

    const older = sessionReader(null);
    const legacy = await idleMacSocket(mac, older, { descriptorStorage: storageBox() });
    await fromMachine(legacy.client, legacy.socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" }, cloud_status: CLOUD_STATUS });
    await fromMachine(legacy.client, legacy.socket, "s/mac-01/__clawdline_inventory_v1__", { inventory: { version: 1, sessions: ["s1"] } });
    await until(() => legacy.socket.sent.some((frame) => frame.type === "publish"), 400);
    // A Linux executor lists no `features` either, so this is also its known limitation (docs/cloud.md,
    // *Known limitation: a Linux executor's rows do not come back after an eviction*): the page does
    // not know to wait, and a machine whose rows the relay lost reads as having none until one changes.
    assert.equal(legacy.socket.sent.filter((frame) => frame.type === "publish").length, 0,
        "a Mac that lists no features is never sent the word");
    assert.deepEqual(older.lists.map((list) => list.scan.emptyAuthoritative), older.lists.map(() => true),
        "and its lists read exactly as they always have");
});

await check("B1 · a relay that still replays the inventory, every row and the orch snapshot is not asked; one that kept rows only is asked for less", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const replay = async (viewer, channel, payload) => {
        const envelope = await sealEnvelope({ ch: channel, seq: ++machineSequence, ts: 1787817600000, class: "stream",
            key_id: "ms-1", sender: DEVICE }, JSON.stringify(payload), masterKey, signingKey);
        viewer.socket.receive({ type: "envelope", realign: true, envelope });
        await viewer.client.messageChain.catch(noop);
    };
    const whole = sessionReader(null);
    const intact = await idleMacSocket(mac, whole, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC), sessionSnapshotSettleMs: 200 });
    await replay(intact, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: Object.assign({}, CLOUD_STATUS, { features: ["sessions.snapshot"] }) });
    await replay(intact, "s/mac-01/s1", { session: mac.rows.get("s1"), at: 100, scan: {} });
    await replay(intact, "s/mac-01/__clawdline_inventory_v1__", { inventory: { version: 1, sessions: ["s1"] }, features: ["sessions.snapshot"] });
    await until(() => intact.socket.sent.some((frame) => frame.type === "publish"), 500);
    assert.equal(intact.socket.sent.filter((frame) => frame.type === "publish").length, 0,
        "everything the Mac would re-send arrived in the replay, so nothing is asked");
    const last = whole.lists[whole.lists.length - 1];
    assert.deepEqual([last && last.rows, last && last.scan.emptyAuthoritative], [["s1"], true]);

    const partial = await idleMacSocket(mac, sessionReader(null), { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC), sessionSnapshotSettleMs: 200 });
    await replay(partial, "s/mac-01/s1", { session: mac.rows.get("s1"), at: 100, scan: {} });
    await replay(partial, "s/mac-01/__clawdline_inventory_v1__", { inventory: { version: 1, sessions: ["s1"] }, features: ["sessions.snapshot"] });
    await until(() => partial.sent.snapshots === 1, WAIT);
    assert.deepEqual(partial.sent.orchestrator, [true],
        "rows alone came back, and an orch snapshot too large for the relay to keep does not: that is asked for");
    intact.client.stop();
    partial.client.stop();
});

await check("B1 · a page back from quiesce after a row changed while it was away converges and reads that transcript exactly once", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "working", transcript_signature: "10-1" }],
        ["s2", { id: "s2", state: "idle", transcript_signature: "20-1" }]]) };
    const reader = sessionReader("s1");
    const storage = storageBox(REMEMBERED_SNAPSHOT_MAC);
    const first = await idleMacSocket(mac, reader, { descriptorStorage: storage });
    // While this page was connected the Mac published its rows live, as any scan does.
    for (const row of mac.rows.values()) {
        await fromMachine(first.client, first.socket, "s/mac-01/" + row.id, { session: row, at: 100, scan: {} });
    }
    await until(() => first.sent.transcripts === 1 && first.client.readWaiters.size === 0, WAIT);
    assert.equal(first.sent.transcripts, 1, "the open Session was read once while connected");
    assert.equal(first.client.readWaiters.size, 0, "and that read was answered before the page went away");
    first.client.retire();
    // Hidden past the grace: the turn ends, and the relay object that saw it is evicted.
    mac.rows.set("s1", { id: "s1", state: "idle", transcript_signature: "11-4" });
    const listsBefore = reader.lists.length;
    const second = await idleMacSocket(mac, reader, { descriptorStorage: storage, resumeFrom: first.client });
    // Done means converged, not merely read: the transcript read starts when row s1 lands, before the
    // inventory and the answer settle the recovery — asserting at that moment raced it (2026-09-15).
    const converged = () => {
        const tail = reader.lists.slice(listsBefore);
        const newest = tail[tail.length - 1];
        return second.sent.transcripts === 1 && newest && newest.scan.emptyAuthoritative
            && JSON.stringify(newest.rows) === JSON.stringify(["s1", "s2"]);
    };
    await until(converged, WAIT);
    await settle();
    const after = reader.lists.slice(listsBefore);
    const last = after[after.length - 1];
    assert.equal(second.sent.snapshots, 1, "the resumed page asked for the rows");
    assert.deepEqual([last && last.rows, last && last.scan.emptyAuthoritative], [["s1", "s2"], true]);
    const heldKey = Array.from(second.client.sessionSnapshots.keys()).find((key) => key.endsWith("s1"));
    assert.equal(second.client.sessionSnapshots.get(heldKey).transcript_signature, "11-4",
        "the row it holds is the Mac's current one");
    assert.equal(second.sent.transcripts, 1, "the changed signature produced exactly one transcript read");
    assert.deepEqual(reader.claimedEmpty(), [], JSON.stringify(after));

    // A renewal takes over from a socket that is still up: nothing was lost, so nothing is asked.
    const third = await idleMacSocket(mac, reader, { descriptorStorage: storage, resumeFrom: second.client });
    await until(() => third.socket.sent.some((frame) => frame.type === "publish"), 400);
    assert.equal(third.socket.sent.filter((frame) => frame.type === "publish").length, 0, "a renewal does not ask again");
    second.client.retire();
    // A page frozen past the grace whose socket still says ready: replaced like a renewal, but asks.
    third.client.continuityUnproven = true;
    const fourth = await idleMacSocket(mac, reader, { descriptorStorage: storage, resumeFrom: third.client });
    await until(() => fourth.sent.snapshots === 1, WAIT);
    assert.equal(fourth.sent.snapshots, 1, "a replacement for a socket nothing proves heard everything asks");
    third.client.retire();
});

await check("B1 · a Mac that does not answer ends the bounded attempt in a typed failure, and a closed client keeps no timer", async function () {
    const mac = { silent: true, rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const reader = sessionReader(null);
    const viewer = await idleMacSocket(mac, reader, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC),
        sessionSnapshotTimeoutMs: 150, sessionSnapshotRetryMs: 50 });
    assert.deepEqual(reader.lists, [], "a state this page made up is not handed over before any envelope is opened");
    await fromMachine(viewer.client, viewer.socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: Object.assign({}, CLOUD_STATUS, { features: ["sessions.snapshot"] }) });
    await until(() => { const last = reader.lists[reader.lists.length - 1]; return last && last.scan.failures.length; }, WAIT);
    const last = reader.lists[reader.lists.length - 1];
    assert.equal(viewer.sent.snapshots, 2, "asked once and once more");
    assert.deepEqual(last && last.scan.failures, [{ machine: "mac-01", code: "cloud_read_timeout" }], JSON.stringify(reader.lists));
    assert.equal(last.scan.recovering.length, 0, "and it is no longer waiting");
    assert.ok(describeFailure({ code: "cloud_sessions_incomplete" }).known, "rows named and never received have words of their own");
    viewer.client.stop();
    assert.equal(viewer.client.sessionRecovery.size, 0, "a stopped client holds no recovery and no timer");
});

await check("B1 · rows an answer names that never arrive end the attempt as cloud_sessions_incomplete, and the next pass clears it", async function () {
    const mac = { lose: "s2", rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }],
        ["s2", { id: "s2", state: "idle", transcript_signature: "20-1" }]]) };
    const reader = sessionReader(null);
    const viewer = await idleMacSocket(mac, reader, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC),
        sessionRowsGraceMs: 100 });
    await until(() => { const last = reader.lists[reader.lists.length - 1]; return last && last.scan.failures.length; }, WAIT);
    const failed = reader.lists[reader.lists.length - 1];
    assert.deepEqual([failed && failed.rows, failed && failed.scan.failures, failed && failed.scan.recovering],
        [["s1"], [{ machine: "mac-01", code: "cloud_sessions_incomplete" }], []], JSON.stringify(reader.lists));
    assert.equal(viewer.sent.snapshots, 1, "a named row that did not come is a failure, not another request");
    // The Mac's next pass — its refresh, every three minutes — brings the lost row and a whole inventory.
    await fromMachine(viewer.client, viewer.socket, "s/mac-01/s2", { session: mac.rows.get("s2"), at: 100, scan: {} });
    await fromMachine(viewer.client, viewer.socket, "s/mac-01/__clawdline_inventory_v1__",
        { inventory: { version: 1, sessions: ["s1", "s2"] }, features: ["sessions.snapshot"] });
    await until(() => { const last = reader.lists[reader.lists.length - 1]; return last && last.rows.length === 2 && !last.scan.failures.length; }, WAIT);
    const cleared = reader.lists[reader.lists.length - 1];
    assert.deepEqual([cleared && cleared.rows, cleared && cleared.scan.failures, cleared && cleared.scan.emptyAuthoritative],
        [["s1", "s2"], [], true], "the failure clears once the machine's inventory is whole");
    viewer.client.stop();
});

/* ---- F1 · a device that may not ask, F3 · a renewal that hears nothing, F4 · orch/ on return -- */

// The confirmation review's probe (task b336e402): a read-only device of an idle Mac after an
// eviction, whose replay held only the inventory, published nothing and said "no Sessions".
await check("F1 · a read-only device after an eviction shows the Mac as sending, never empty, and has the rows once the Mac's refresh pass arrives", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const reader = sessionReader(null);
    const viewer = await idleMacSocket(mac, reader, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC),
        allowWrites: false, nextSequence: undefined });
    await fromMachine(viewer.client, viewer.socket, "s/mac-01/__clawdline_inventory_v1__",
        { inventory: { version: 1, sessions: ["s1"] }, features: ["sessions.snapshot"] });
    const waiting = reader.lists[reader.lists.length - 1];
    assert.deepEqual([waiting && waiting.rows, waiting && waiting.scan.emptyAuthoritative, waiting && waiting.scan.recovering],
        [[], false, ["mac-01"]], "the inventory names a row that has not come: waiting, not empty — " + JSON.stringify(reader.lists));
    assert.equal(viewer.socket.sent.filter((frame) => frame.type === "publish").length, 0, "a device that may not publish asks nothing");
    // The Mac's refresh pass (`CloudAppBridge`, every three minutes): each row, then the inventory.
    await fromMachine(viewer.client, viewer.socket, "s/mac-01/s1", { session: mac.rows.get("s1"), at: 100, scan: {} });
    await fromMachine(viewer.client, viewer.socket, "s/mac-01/__clawdline_inventory_v1__",
        { inventory: { version: 1, sessions: ["s1"] }, features: ["sessions.snapshot"] });
    await until(() => { const last = reader.lists[reader.lists.length - 1]; return last && last.rows.length === 1 && last.scan.emptyAuthoritative; }, WAIT);
    const last = reader.lists[reader.lists.length - 1];
    assert.deepEqual([last && last.rows, last && last.scan.emptyAuthoritative, last && last.scan.recovering], [["s1"], true, []]);
    assert.deepEqual(reader.claimedEmpty(), [], "no list said the account is empty: " + JSON.stringify(reader.lists));
    assert.equal(viewer.client.sessionRecovery.size, 0, "and the wait is over, its timer with it");
    viewer.client.stop();

    // A pass that never comes — the Mac went away — ends in words, not in "no Sessions".
    const lost = sessionReader(null);
    const gone = await idleMacSocket(mac, lost, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC),
        allowWrites: false, nextSequence: undefined, sessionRefreshWaitMs: 150 });
    await fromMachine(gone.client, gone.socket, "s/mac-01/__clawdline_inventory_v1__",
        { inventory: { version: 1, sessions: ["s1"] }, features: ["sessions.snapshot"] });
    await until(() => { const newest = lost.lists[lost.lists.length - 1]; return newest && newest.scan.failures.length; }, WAIT);
    const failed = lost.lists[lost.lists.length - 1];
    assert.deepEqual([failed && failed.scan.failures, failed && failed.scan.recovering],
        [[{ machine: "mac-01", code: "cloud_sessions_incomplete" }], []], JSON.stringify(lost.lists));
    // The list says a failure in its own words (`view/list.js`); only an empty list with neither a
    // wait nor a failure beside it reads as "no Sessions".
    assert.deepEqual(lost.lists.filter((list) => !list.rows.length && list.scan.emptyAuthoritative && !list.scan.failures.length), [],
        "a failure is said, and no list claimed the account empty: " + JSON.stringify(lost.lists));
    gone.client.stop();
    assert.equal(gone.client.sessionRecovery.size, 0, "a stopped client holds no wait");

    // A renewal during the wait keeps waiting with the same deadline, and the pass on the new socket ends it.
    const renewing = sessionReader(null);
    const before = await idleMacSocket(mac, renewing, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC),
        allowWrites: false, nextSequence: undefined });
    await fromMachine(before.client, before.socket, "s/mac-01/__clawdline_inventory_v1__",
        { inventory: { version: 1, sessions: ["s1"] }, features: ["sessions.snapshot"] });
    const deadline = before.client.sessionRecovery.get("mac-01").deadline;
    const after = await idleMacSocket(mac, renewing, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC),
        allowWrites: false, nextSequence: undefined, resumeFrom: before.client });
    before.client.retire();
    const carried = after.client.sessionRecovery.get("mac-01");
    assert.deepEqual([carried && carried.passive, carried && carried.deadline], [true, deadline], "the wait goes on, not starts over");
    await fromMachine(after.client, after.socket, "s/mac-01/s1", { session: mac.rows.get("s1"), at: 100, scan: {} });
    await until(() => after.client.sessionRecovery.size === 0, WAIT);
    assert.equal(after.client.sessionRecovery.size, 0,
        "the inventory the old socket heard and the row this one heard are the whole machine");
    after.client.stop();
});

await check("F3 · a renewal whose socket no longer hears is not taken as continuous, and asks for the rows", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const reader = sessionReader(null);
    const storage = storageBox(REMEMBERED_SNAPSHOT_MAC);
    const first = await idleMacSocket(mac, reader, { descriptorStorage: storage });
    await until(() => first.sent.snapshots === 1 && first.client.sessionRecovery.size === 0, WAIT);
    const heard = await idleMacSocket(mac, reader, { descriptorStorage: storage, resumeFrom: first.client });
    assert.ok(first.socket.sent.some((frame) => frame.type === "ping"), "the socket being replaced was pinged");
    await until(() => heard.socket.sent.some((frame) => frame.type === "publish"), 400);
    assert.equal(heard.socket.sent.filter((frame) => frame.type === "publish").length, 0,
        "it answered, so the renewal is continuous and asks nothing");
    first.client.retire();
    // A network change left the socket half-open: it still reads ready, and the relay's pong never comes.
    heard.socket.halfOpen = true;
    mac.rows.set("s1", { id: "s1", state: "working", transcript_signature: "11-2" });
    const renewed = await idleMacSocket(mac, reader, { descriptorStorage: storage, resumeFrom: heard.client });
    assert.equal(heard.client.ready, true, "the premise: the replaced client still says ready");
    await until(() => renewed.sent.snapshots === 1, WAIT);
    assert.equal(renewed.sent.snapshots, 1, "a replacement for a socket that did not answer asks for the rows");
    await until(() => renewed.client.sessionRecovery.size === 0, WAIT);
    const key = Array.from(renewed.client.sessionSnapshots.keys()).find((candidate) => candidate.endsWith("s1"));
    assert.equal(renewed.client.sessionSnapshots.get(key).state, "working", "and holds the Mac's current row");
    heard.client.retire();
    renewed.client.stop();
});

await check("F4 · a page back from the background that holds the Mac's orch snapshot asks for rows without it", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const reader = sessionReader(null);
    const storage = storageBox(REMEMBERED_SNAPSHOT_MAC);
    const first = await idleMacSocket(mac, reader, { descriptorStorage: storage });
    await until(() => first.sent.snapshots === 1, WAIT);
    assert.deepEqual(first.sent.orchestrator, [true], "a page that never had the orch snapshot asks for it");
    await fromMachine(first.client, first.socket, "orch/mac-01", { tasks: [], machine: { name: "Mac", platform: "macos" },
        cloud_status: Object.assign({}, CLOUD_STATUS, { features: ["sessions.snapshot"] }) });
    first.client.retire();
    // Hidden past the grace and back: not a renewal, so the rows are asked for — and only the rows.
    const back = await idleMacSocket(mac, reader, { descriptorStorage: storage, resumeFrom: first.client });
    await until(() => back.sent.snapshots === 1, WAIT);
    assert.deepEqual(back.sent.orchestrator, [false],
        "the page holds that Mac's orch snapshot, so no few hundred kilobytes go out to every viewer for it");
    back.client.stop();
});

/* ---- F5 · a status-only cloud_status notice carries no task ------------------------------------ */

// A Mac that holds a snapshot sends every notice as that snapshot with the digest in it
// (`CloudAppBridge.publishOrchestratorNotice`); `{"cloud_status": …}` alone comes only from one that has
// none yet. Nothing may depend on that: a page must read such a notice beside the snapshot it holds, and
// a page the relay replays only that notice to must still ask for the snapshot.
await check("F5 · a cloud_status notice leaves the Mac's tasks on the page, and a page replayed only a notice still asks for the snapshot", async function () {
    const mac = { rows: new Map([["s1", { id: "s1", state: "idle", transcript_signature: "10-1" }]]) };
    const withStatus = (extra) => Object.assign({}, CLOUD_STATUS, { features: ["sessions.snapshot"] }, extra || {});
    const snapshot = { tasks: [{ id: "t1", state: "briefed", title: "Trim", child: { terminalId: "s1" }, root: { terminalId: "r1" } }],
        at: 1789290000, machine: { name: "Mac", platform: "macos" }, app: { build: "7" }, cloud_status: withStatus() };

    const reader = sessionReader(null);
    const lists = [];
    reader.handlers.tasks = function (list) { lists.push(list.map((task) => task.id)); };
    const holder = await idleMacSocket(mac, reader, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC) });
    await until(() => holder.sent.snapshots === 1, WAIT);
    await fromMachine(holder.client, holder.socket, "orch/mac-01", snapshot);
    assert.deepEqual(lists[lists.length - 1], ["t1"], "the snapshot's task reached the page");
    const drawnBefore = lists.length;
    const drop = { sender: "web_other", seq: 9, code: "replay", layer: "mac_transport", at_ms: 1789290001000, highest_seq: 10 };
    await fromMachine(holder.client, holder.socket, "orch/mac-01", { cloud_status: withStatus({ recent_drops: [drop] }) });
    assert.equal(lists.length, drawnBefore, "a notice does not hand the task list over again");
    assert.deepEqual((await holder.client.tasks()).tasks.map((task) => task.id), ["t1"], "and the page still holds the task");
    assert.equal(holder.client._macBuild("mac-01"), "7", "and the snapshot's build stamp");
    assert.ok(holder.client.macCapabilities.has("mac-01"), "while the notice itself was read");
    holder.client.stop();

    const cold = sessionReader(null);
    const replayed = await idleMacSocket(mac, cold, { descriptorStorage: storageBox(REMEMBERED_SNAPSHOT_MAC), sessionSnapshotSettleMs: 200 });
    const envelope = await sealEnvelope({ ch: "orch/mac-01", seq: ++machineSequence, ts: 1787817600000, class: "stream",
        key_id: "ms-1", sender: DEVICE }, JSON.stringify({ cloud_status: withStatus() }), masterKey, signingKey);
    replayed.socket.receive({ type: "envelope", realign: true, envelope });
    await replayed.client.messageChain.catch(noop);
    await until(() => replayed.sent.snapshots === 1, WAIT);
    assert.deepEqual(replayed.sent.orchestrator, [true],
        "the relay's replay was a notice, which is not the snapshot, so the page asks for it");
    replayed.client.stop();
});

/* ---- ends ---------------------------------------------------------------------------------- */

if (failures.length) {
    console.log("web cloud fleet checks: " + failures.length + " red, " + passed.length + " green");
    console.log("  " + failures.join("\n  "));
    process.exit(1);
}
console.log("web cloud fleet checks passed: " + passed.length);
process.exit(0);
