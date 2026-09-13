/**
 * Every Cloud failure on screen says which layer, which code, which command — and never hangs.
 *
 * `docs/cloud-error-transparency.md` §3.1 (B1–B9), §5 (the two rules and their guards) and §11
 * from the browser's side, each driven through a relay in a box rather than read off the source:
 * a fake socket that answers publishes the way `relay/src/account-do.ts` does, and envelopes sealed
 * with the checked-in protocol vectors standing in for the Mac.
 *
 * Every check runs on its own and reports on its own, so the same file run against a tree without
 * the feature names each missing behaviour rather than stopping at the first import it cannot
 * resolve. The modules this feature adds are imported that way for the same reason.
 *
 * The two §5 guards carry their own mutation proof: each is run once against a deliberately
 * broken input and must go red there, so neither can pass by scanning nothing.
 */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join, relative } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..");
const webJS = join(repoRoot, "Resources", "web", "app", "js");
const SCENARIO = process.env.CLAWDLINE_CLOUD_FAILURE_SCENARIO || "";
// A request made here is always read back through `outcome()`, which says how it ended; one that
// rejects before that line runs is still asserted there, and must not end the process first — on
// a tree without the feature that would hide every check after it.
process.on("unhandledRejection", function () { });

const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");
const { T, fill } = await import("../Resources/web/app/js/core/i18n.js");
const {
    importDevicePrivateKey, importMasterSecret, importSenderPublicKey, openEnvelope, sealEnvelope
} = await import("../Resources/web/app/js/net/cloud-crypto.js");
const optional = (path) => import(path).catch(() => null);
const failureModule = await optional("../Resources/web/app/js/net/cloud-failure.js");
const textModule = await optional("../Resources/web/app/js/core/failure-text.js");
const sheetModule = await optional("../Resources/web/app/js/view/cloud-status.js");

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
const LAYERS = ["browser", "relay", "mac_transport", "mac_preflight", "mac_ledger", "mac_route", "mac_reply", "mac"];

/* ---- a relay in a box ------------------------------------------------------------------- */

class FakeWebSocket {
    static latest = null;
    constructor() { this.readyState = 1; this.sent = []; this.opened = 0; FakeWebSocket.latest = this; }
    send(text) { this.sent.push(JSON.parse(text)); }
    /** The client closing its own socket. */
    close(code) { this.readyState = 3; if (this.onclose) this.onclose({ code: code || 1000 }); }
    /** The relay closing it. */
    dropped(code) { this.readyState = 3; if (this.onclose) this.onclose({ code: code }); }
    receive(frame) { this.onmessage({ data: JSON.stringify(frame) }); }
}

/** BroadcastChannel as one origin sees it: every channel of a name hears every other. */
class FakeBroadcastChannel {
    static rooms = new Map();
    constructor(name) {
        this.name = name;
        const room = FakeBroadcastChannel.rooms.get(name) || new Set();
        room.add(this);
        FakeBroadcastChannel.rooms.set(name, room);
    }
    postMessage(data) {
        for (const other of FakeBroadcastChannel.rooms.get(this.name) || []) {
            if (other !== this && other.onmessage) other.onmessage({ data: structuredClone(data) });
        }
    }
    close() { (FakeBroadcastChannel.rooms.get(this.name) || new Set()).delete(this); }
}

let readTimers = [];
let outboundSequence = 100;
function cloudClient(options) {
    return new CloudClient(Object.assign({
        relayURL: "https://relay.example", deviceToken: "jwt", devicePrivateKey: signingKey,
        masterKey: masterKey, masterKeys: { "ms-2": vectors.master_secret },
        senderKeys: { [DEVICE]: senderKey }, WebSocket: FakeWebSocket,
        account: ACCOUNT, deviceID: DEVICE, BroadcastChannel: null,
        allowWrites: true, nextSequence: function () { return Promise.resolve(outboundSequence++); },
        setTimeout: function (fn) { readTimers.push(fn); return readTimers.length; },
        clearTimeout: function () { }
    }, options));
}

async function ready(client) {
    await client.start();
    const socket = FakeWebSocket.latest;
    socket.receive({ type: "challenge", v: 1, context: "clawdline-challenge-v1", account: ACCOUNT,
        device: DEVICE, challenge: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", expires_in_ms: 30_000 });
    await client.messageChain;
    socket.receive({ type: "ready", v: 1, role: "viewer", account: ACCOUNT, device: DEVICE });
    await client.messageChain;
    return socket;
}

async function until(predicate, what) {
    const deadline = Date.now() + 3000;
    while (Date.now() < deadline) {
        if (predicate()) return;
        await new Promise(function (resolve) { setTimeout(resolve, 2); });
    }
    assert.fail("timed out waiting for " + what);
}

function published(socket) { return socket.sent.filter(function (frame) { return frame.type === "publish"; }); }

async function nextCommand(socket, type) {
    const cursor = socket.opened;
    await until(function () { return published(socket).length > cursor; }, "a " + type + " command on the wire");
    socket.opened = cursor + 1;
    const envelope = published(socket)[cursor].envelope;
    const command = JSON.parse(new TextDecoder().decode(await openEnvelope(envelope, masterKey, senderKey)));
    command.ch = envelope.ch;
    command.seq = envelope.seq;
    command.sender = envelope.sender;
    assert.equal(command.type, type, "the next command on the wire is " + type);
    return command;
}

let macSequence = 5000;
async function fromMac(client, socket, channel, payload, keyID) {
    const envelope = await sealEnvelope({
        ch: channel, seq: ++macSequence, ts: 1787817600000, class: "stream",
        key_id: keyID || "ms-1", sender: DEVICE
    }, JSON.stringify(payload), masterKey, signingKey);
    socket.receive({ type: "envelope", envelope: envelope });
    await client.messageChain;
}

const answer = (client, socket, machine, payload) =>
    fromMac(client, socket, "t/" + machine + "/" + MACHINE_REPLY, payload);

async function frame(client, socket, value) {
    socket.receive(value);
    await client.messageChain.catch(function () { });
}

/** A client with one Mac and one Session row on it, connected. */
async function fleetOfOne(options) {
    const client = cloudClient(options);
    const socket = await ready(client);
    await fromMac(client, socket, "orch/mac-01", { tasks: [] });
    await fromMac(client, socket, "s/mac-01/s1", { session: { id: "s1", title: "one" } });
    return { client, socket };
}

async function capable(client, socket, extra) {
    await fromMac(client, socket, "orch/mac-01", { tasks: [], cloud_status: Object.assign({
        v: 1, generated_at_ms: 1789290000000, counting_since_ms: 1789280000000,
        clock_guard: { state: "ready", reason: null, clears_at_ms: null },
        token_expires_at_ms: 1789290240000, key_id: "ms-1", roster_readable: true,
        dropped: {}, recent_drops: [], recent_notices: []
    }, extra || {}) });
}

/** How a promise ended within a moment: resolved, a rejection, or still waiting. */
function outcome(promise, ms) {
    return Promise.race([
        promise.then(function (value) { return { state: "resolved", value }; },
            function (error) { return { state: "rejected", error }; }),
        new Promise(function (resolve) { setTimeout(resolve, ms || 50, { state: "pending" }); })
    ]);
}

function typed(error, what) {
    assert.ok(error && /^[a-z][a-z0-9_]{0,63}$/.test(error.code), what + ": a snake_case code, got " + (error && error.code));
    assert.ok(LAYERS.includes(error.layer), what + ": a layer from §1, got " + (error && error.layer));
}

/* ---- scenarios that need their own process ------------------------------------------------ */

if (SCENARIO === "diagnostics") {
    await diagnosticsScenario();
    process.exit(0);
}

const failures = [];
const passed = [];
async function check(name, run) {
    try { await run(); passed.push(name); }
    catch (error) {
        const lines = String(error && error.message).split("\n");
        failures.push(name + "\n    " + (process.env.CLAWDLINE_CLOUD_FAILURE_VERBOSE ? lines : lines.slice(0, 6)).join("\n    "));
    }
}
const need = (module, name) => assert.ok(module, name + " exists");

/* ---- T-B1 · relay says the Mac is offline -------------------------------------------------- */

await check("T-B1 ack machine_offline rejects the waiting read now, as relay · machine_offline with its ref", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.transcript("s1");
    const sent = await nextCommand(socket, "transcript");
    await frame(client, socket, { type: "ack", ch: "ctl/mac-01", seq: sent.seq, status: "machine_offline", fanout: 0 });
    const ended = await outcome(read);
    assert.equal(ended.state, "rejected", "the read ended at once instead of waiting sixty seconds");
    typed(ended.error, "machine_offline");
    assert.deepEqual([ended.error.layer, ended.error.code], ["relay", "machine_offline"]);
    assert.equal(ended.error.ref && ended.error.ref.seq, sent.seq, "the ref names the sequence that was sent");
    assert.equal(ended.error.ref.sender, DEVICE);
    assert.equal(ended.error.retryable, true, "an offline Mac can be asked again");
});

await check("T-B1 a keypress the relay could not deliver rejects instead of resolving", async function () {
    const { client, socket } = await fleetOfOne();
    const pressed = client.answer("s1", "1");
    const sent = await nextCommand(socket, "answer");
    await frame(client, socket, { type: "ack", ch: "ctl/mac-01", seq: sent.seq, status: "machine_offline", fanout: 0 });
    const ended = await outcome(pressed);
    assert.equal(ended.state, "rejected");
    assert.deepEqual([ended.error.layer, ended.error.code], ["relay", "machine_offline"]);
});

/* ---- T-B2 · relay refuses the publish ------------------------------------------------------ */

await check("T-B2 publish_error settles its request by (ch, seq) with the code and detail.field", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.git("s1");
    const sent = await nextCommand(socket, "git");
    await frame(client, socket, { type: "publish_error", ch: "ctl/mac-01", seq: sent.seq, code: "forbidden", field: "capability" });
    const ended = await outcome(read);
    assert.equal(ended.state, "rejected", "no sixty-second wait and no bad_frame");
    assert.deepEqual([ended.error.layer, ended.error.code], ["relay", "forbidden"]);
    assert.equal(ended.error.detail && ended.error.detail.field, "capability");
    assert.equal(ended.error.ref && ended.error.ref.seq, sent.seq);
});

/* ---- T-B3 · relay error frame, then 4401 --------------------------------------------------- */

await check("T-B3 the last relay error and the close code are kept, and later requests carry the code", async function () {
    const { client, socket } = await fleetOfOne();
    const inFlight = client.transcript("s1");
    await nextCommand(socket, "transcript");
    await frame(client, socket, { type: "error", code: "unauthorized", message: "token expired" });
    socket.dropped(4401);
    const ended = await outcome(inFlight);
    assert.equal(ended.state, "rejected");
    assert.deepEqual([ended.error.layer, ended.error.code], ["relay", "unauthorized"], "the read in flight names the relay's reason");
    assert.equal(client.trail.connection.last_relay_error.code, "unauthorized", "connection state records the error frame");
    assert.equal(client.trail.connection.last_close.code, 4401, "and the close code");
    const later = await outcome(client.info("s1"));
    assert.equal(later.state, "rejected");
    assert.deepEqual([later.error.layer, later.error.code], ["relay", "unauthorized"], "a later request says why too");
});

await check("T-B3 a close with no error frame is named by its close code", async function () {
    const { client, socket } = await fleetOfOne();
    socket.dropped(4429);
    const later = await outcome(client.transcript("s1"));
    assert.deepEqual([later.error.layer, later.error.code], ["relay", "rate_limited"]);
});

/* ---- T-B4 · §5 rule 2: no synchronous throw ------------------------------------------------ */

// Called for their effect, never awaited by a page; they are called here too, and must not throw.
const LIFECYCLE = new Set(["events", "subscribe", "stop", "retire"]);
// Cannot fail from a cached answer, or succeed by opening a socket; still held to "a thenable".
const NEED_NOT_REJECT = new Set(["sessions", "tasks", "start", "refresh", "whenReady"]);

const STATES = {
    "no machine": async function () {
        const client = cloudClient();
        await ready(client);
        return client;
    },
    "no session": async function () {
        const client = cloudClient();
        const socket = await ready(client);
        await fromMac(client, socket, "orch/mac-01", { tasks: [] });
        return client;
    },
    "socket closed": async function () {
        const { client, socket } = await fleetOfOne();
        socket.dropped(1006);
        return client;
    }
};
const ARGUMENTS = [
    [],
    ["s1", "a1", "a2"],
    ["nobody", "nobody", "nobody"],
    [{ machine: "mac-01", session: "s1" }, "a1", "a2"],
    [{ id: "never-read" }, "", ""],
    ["project", "item", null, "gone"]
];

/** The guard itself, usable against any class shaped like `CloudClient`. */
async function syncThrowGuard(Client, makeState) {
    const methods = Object.getOwnPropertyNames(Client.prototype)
        .filter(function (name) { return name !== "constructor" && !name.startsWith("_") && typeof Client.prototype[name] === "function"; });
    const problems = [];
    let calls = 0, rejections = 0;
    for (const name of methods) {
        for (const state of Object.keys(STATES)) {
            let rejected = false;
            for (const args of LIFECYCLE.has(name) ? [name === "events" ? [function () { }] : name === "subscribe" ? [["t/mac-01/s1"]] : []] : ARGUMENTS) {
                const client = await makeState(state);
                Object.setPrototypeOf(client, Client.prototype);
                let result;
                calls += 1;
                try { result = client[name].apply(client, args); }
                catch (error) { problems.push(name + " threw synchronously in " + state + " (" + (error.code || error.name) + ")"); continue; }
                if (LIFECYCLE.has(name)) continue;
                if (!result || typeof result.then !== "function") { problems.push(name + " returned no thenable in " + state); continue; }
                const ended = await outcome(result, 30);
                if (ended.state === "rejected") {
                    rejected = true;
                    rejections += 1;
                    try { typed(ended.error, name + " in " + state); }
                    catch (error) { problems.push(error.message); }
                }
                if (ended.state === "pending" && state === "socket closed") problems.push(name + " is still pending with the socket closed");
                client.stop();
            }
            if (state === "socket closed" && !rejected && !LIFECYCLE.has(name) && !NEED_NOT_REJECT.has(name)) {
                problems.push(name + " never rejected with the socket closed");
            }
        }
    }
    return { methods, problems, calls, rejections };
}

await check("T-B4 every public CloudClient method, in three bad states: no throw, a thenable, a typed rejection", async function () {
    const { methods, problems, calls, rejections } = await syncThrowGuard(CloudClient, function (state) { return STATES[state](); });
    assert.ok(methods.includes("transcript") && methods.includes("answer") && methods.includes("board") && methods.length >= 40,
        "the enumeration reaches the whole prototype (" + methods.length + " methods)");
    assert.ok(calls >= methods.length * 3 * 4 && rejections >= methods.length * 3,
        "the guard called and saw rejections, rather than scanning nothing (" + calls + " calls, " + rejections + " rejections)");
    console.log("  T-B4 guard: " + methods.length + " methods, " + calls + " calls, " + rejections + " typed rejections");
    assert.deepEqual(problems, [], "the transport contract");
});

await check("T-B4 the guard goes red when one method is reverted to a synchronous throw", async function () {
    class Reverted extends CloudClient {
        board() { throw Object.assign(new Error("reverted"), { code: "cloud_machine_ambiguous" }); }
    }
    const { problems } = await syncThrowGuard(Reverted, function (state) { return STATES[state](); });
    assert.ok(problems.some(function (line) { return /^board threw synchronously/.test(line); }),
        "the reverted method is named: " + problems.slice(0, 3).join(" | "));
});

/* ---- T-B5 · §5 rule 1: the words come from the code ---------------------------------------- */

await check("T-B5 an unknown code shows the code and ref and never the message; a known code its sentence", async function () {
    need(textModule, "core/failure-text.js");
    const ref = { sender: "web_f052dcb8-aaaa-bbbb", seq: 1234, request: null };
    const unknown = Object.assign(new Error("The command durability boundary is unavailable."),
        { code: "zz_unknown", layer: "mac_ledger", ref: ref });
    const said = textModule.failureSentence(unknown);
    assert.ok(said.includes("zz_unknown"), "the code is on the line: " + said);
    assert.ok(said.includes("f052dcb8·1234"), "and the ref, spelled sender·seq without web_: " + said);
    assert.ok(!said.includes("durability"), "and not the message");
    assert.ok(said.startsWith(T.webFailUnknown), "an unknown code is the did-not-succeed sentence");
    const known = textModule.describeFailure(Object.assign(new Error("English"), { code: "machine_offline", layer: "relay", ref: ref }));
    assert.equal(known.text, T.webFailMachineOffline);
    assert.equal(known.tag, "machine_offline · f052dcb8·1234");
});

await check("T-B5 a failure line puts the tag in small type and opens the status sheet at that ref", async function () {
    need(textModule, "core/failure-text.js");
    const doc = { createElement: function (tag) { return { tag, className: "", textContent: "", children: [] }; } };
    const line = { ownerDocument: doc, textContent: "", children: [], attributes: {},
        classList: { names: new Set(), toggle(name, on) { if (on) this.names.add(name); else this.names.delete(name); }, add(name) { this.names.add(name); } },
        appendChild(child) { this.children.push(child); this.textContent += child.textContent; },
        setAttribute(name, value) { this.attributes[name] = value; }, removeAttribute(name) { delete this.attributes[name]; } };
    let opened = null;
    textModule.setFailureOpener(function (at) { opened = at; });
    const error = Object.assign(new Error("secret"), { code: "zz_unknown", layer: "relay", ref: { sender: DEVICE, seq: 7 } });
    textModule.renderFailure(line, error, "Fallback words.");
    textModule.setFailureOpener(null);
    assert.equal(line.children[0].className, "failure-tag");
    assert.equal(line.children[0].textContent, "zz_unknown · device-v·7");
    assert.ok(!line.textContent.includes("secret"));
    assert.ok(line.classList.names.has("failure-open"), "the line says it can be pressed");
    line.onclick();
    assert.deepEqual(opened && opened.ref, { sender: DEVICE, seq: 7 }, "one press opens the sheet at that ref");
});

/** §5 rule 1's static guard: no error `.message` into anything under input/, view/, session/. */
const MESSAGE_READ = /\b(?:e|err|error|caught|failure|problem|reason|exception)\s*(?:\?\.|\.)\s*message\b|\.error\s*(?:\?\.|\.)\s*message\b|\bbody\s*(?:\?\.|\.)\s*message\b/;
function messageReads(files) {
    const found = [];
    for (const [path, source] of files) {
        source.split("\n").forEach(function (line, index) {
            const code = line.replace(/\/\/.*$/, "");
            if (MESSAGE_READ.test(code)) found.push(path + ":" + (index + 1) + ": " + line.trim());
        });
    }
    return found;
}
async function uiSources() {
    const files = [];
    for (const folder of ["input", "view", "session"]) {
        for (const name of (await readdir(join(webJS, folder))).sort()) {
            if (!name.endsWith(".js")) continue;
            files.push([relative(repoRoot, join(webJS, folder, name)), await readFile(join(webJS, folder, name), "utf8")]);
        }
    }
    return files;
}

await check("T-B5 static guard: UI code under js/input, js/view, js/session shows no error .message", async function () {
    const files = await uiSources();
    assert.ok(files.length >= 60, "the scan read the UI modules (" + files.length + ")");
    assert.ok(files.some(([path]) => path.endsWith("input/composer.js")) && files.some(([path]) => path.endsWith("session/open.js")),
        "including the composer and the transcript opener");
    assert.deepEqual(messageReads(files), [], "an error's message on screen is English from a server");
});

await check("T-B5 the static guard goes red on one inserted toast(e.message)", async function () {
    const files = await uiSources();
    const [path, source] = files.find(([name]) => name.endsWith("input/composer.js"));
    const mutated = files.map(([name, text]) => [name, name === path
        ? text.replace("toastFailure(e, T.sendFailed);", "toast(e.message);") : text]);
    assert.notEqual(mutated.find(([name]) => name === path)[1], source, "the mutation applied");
    const found = messageReads(mutated);
    assert.equal(found.length, 1, "exactly the inserted line is named: " + found.join(" | "));
    assert.ok(found[0].includes("toast(e.message)"));
});

/* ---- T-B6 · detail survives normalisation -------------------------------------------------- */

await check("T-B6 no_whisper keeps reason=no_model and is said as the NoModel sentence", async function () {
    const { client, socket } = await fleetOfOne();
    const dictation = client.voice("AAAA", 16000);
    const sent = await nextCommand(socket, "voice");
    await answer(client, socket, "mac-01", { read: "action:" + sent.request, status: 503,
        error: { code: "no_whisper", message: "Whisper has no model", reason: "no_model", path: "/Users/x/.cache" } });
    const ended = await outcome(dictation);
    assert.equal(ended.state, "rejected");
    assert.equal(ended.error.detail.reason, "no_model", "the whitelisted field is kept");
    assert.equal(ended.error.reason, "no_model", "and readable where the local client puts it");
    assert.equal(ended.error.detail.path, undefined, "a field outside the whitelist is dropped");
    need(textModule, "core/failure-text.js");
    assert.equal(textModule.describeFailure(ended.error).text, T.webVoiceNoModel);
});

/* ---- T-B7 · token rotation with a read in flight ------------------------------------------- */

await check("T-B7 a renewal retires in-flight work as cloud_reconnecting, localised", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.transcript("s1");
    await nextCommand(socket, "transcript");
    const renewed = cloudClient({ deviceToken: "jwt-2", resumeFrom: client });
    await ready(renewed);
    client.retire();
    const ended = await outcome(read);
    assert.equal(ended.state, "rejected");
    assert.deepEqual([ended.error.layer, ended.error.code, ended.error.retryable], ["browser", "cloud_reconnecting", true]);
    need(textModule, "core/failure-text.js");
    assert.equal(textModule.describeFailure(ended.error).text, T.webFailReconnecting);
    assert.equal(renewed.trail, client.trail, "the command trail carries across the renewal");
});

/* ---- T-B8 · diagnostics over Cloud --------------------------------------------------------- */

await check("T-B8 the Cloud diagnostics report is a diagnostics.report command, size-checked, with the trail", async function () {
    const { client, socket } = await fleetOfOne();
    assert.equal(typeof client.diagnosticsReport, "function", "the Cloud transport sends reports");
    const sending = client.diagnosticsReport({ currentTrace: [{ event: "fixture" }], completeness: { whole: true } });
    const sent = await nextCommand(socket, "diagnostics.report");
    assert.equal(sent.ch, "ctl/mac-01");
    assert.equal(sent.session, MACHINE_REPLY);
    assert.ok(sent.report && Array.isArray(sent.report.currentTrace), "the report itself");
    assert.ok(sent.report.cloud_trail && Array.isArray(sent.report.cloud_trail.commands), "and the recent-command trail");
    await answer(client, socket, "mac-01", { read: "action:" + sent.request, status: 200,
        body: { ok: true, path: "/Users/x/Library/Logs/Clawdline/diagnostics/report.json", bytes: 812 } });
    const ended = await outcome(sending);
    assert.equal(ended.state, "resolved");
    assert.equal(ended.value.path, "/Users/x/Library/Logs/Clawdline/diagnostics/report.json");

    const before = published(socket).length;
    const huge = await outcome(client.diagnosticsReport({ blob: "x".repeat(2 * 1024 * 1024 + 1) }));
    assert.deepEqual([huge.state, huge.error && huge.error.layer, huge.error && huge.error.code], ["rejected", "browser", "report_too_large"]);
    assert.equal(published(socket).length, before, "an oversized report never reaches the relay");
});

await check("T-B8 the diagnostics panel's Send to Mac uses the Cloud command, not a fetch to its own origin", async function () {
    const run = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
        cwd: process.cwd(), encoding: "utf8", timeout: 60_000,
        env: { ...process.env, CLAWDLINE_CLOUD_FAILURE_SCENARIO: "diagnostics" }
    });
    assert.equal(run.status, 0, (run.stderr || run.stdout || String(run.error)).trim().split("\n").slice(-6).join("\n"));
    assert.match(run.stdout, /diagnostics scenario passed/);
});

async function diagnosticsScenario() {
    function element(tag) {
        return { tagName: String(tag).toUpperCase(), childNodes: [], style: { cssText: "" }, listeners: {},
            hidden: false, disabled: false, textContent: "", className: "",
            appendChild(child) { this.childNodes.push(child); return child; },
            addEventListener(name, fn) { (this.listeners[name] = this.listeners[name] || []).push(fn); },
            press() { (this.listeners.click || []).forEach((fn) => fn({})); },
            querySelector(selector) {
                const matches = (node) => (selector.charAt(0) === "."
                    ? String(node.className).split(/\s+/).indexOf(selector.slice(1)) >= 0
                    : node.tagName === selector.toUpperCase());
                const walk = (node) => {
                    for (const child of node.childNodes) {
                        if (matches(child)) return child;
                        const deeper = walk(child);
                        if (deeper) return deeper;
                    }
                    return null;
                };
                return walk(this);
            } };
    }
    const root = element("html");
    const install = (name, value) => Object.defineProperty(globalThis, name, { value, configurable: true, writable: true });
    const fetched = [];
    install("document", { documentElement: root, hidden: false, createElement: element, querySelector: () => null, addEventListener: () => {} });
    install("window", { addEventListener: () => {} });
    install("location", { search: "", hash: "" });
    install("navigator", { userAgent: "node" });
    install("performance", { now: () => Date.now() });
    install("requestAnimationFrame", (fn) => setTimeout(fn, 0));
    install("localStorage", { getItem: () => null, setItem: () => {}, removeItem: () => {} });
    install("fetch", (url) => { fetched.push(url); return Promise.reject(new Error("no fetch on the hosted origin")); });
    const { Diagnostics } = await import("../Resources/web/app/js/core/layout-diagnostics.js");
    const reports = [];
    let refuse = false;
    const transport = {
        trail: { snapshot: () => ({ commands: [] }) },
        diagnosticsReport: (report) => {
            reports.push(report);
            return refuse
                ? Promise.reject(Object.assign(new Error("English"), { code: "report_too_large", layer: "browser", ref: null }))
                : Promise.resolve({ ok: true, path: "/Users/x/diagnostics/report.json", bytes: 99, previous: "" });
        }
    };
    Diagnostics.bind({ state: {}, elements: {}, transport: () => transport });
    Diagnostics.reveal();
    const panel = root.childNodes[root.childNodes.length - 1];
    const find = (node, test) => { for (const child of node.childNodes) { if (test(child)) return child; const deeper = find(child, test); if (deeper) return deeper; } return null; };
    const send = find(panel, (child) => child.tagName === "BUTTON" && child.textContent === "Send to Mac");
    const said = find(panel, (child) => child.className === "layout-debug-said");
    send.press();
    for (let i = 0; i < 5; i += 1) await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(reports.length, 1, "the report went to the Cloud transport");
    assert.deepEqual(fetched, [], "and nothing was fetched from the page's own origin");
    assert.ok(said.textContent.includes("/Users/x/diagnostics/report.json"), "the path the Mac answered with is printed: " + said.textContent);
    refuse = true;
    send.press();
    for (let i = 0; i < 5; i += 1) await new Promise((resolve) => setTimeout(resolve, 0));
    assert.ok(said.textContent.includes("browser · report_too_large") && !said.textContent.includes("English"),
        "a refusal is its layer and code: " + said.textContent);
    console.log("diagnostics scenario passed");
}

/* ---- T-B9 · key id drift ------------------------------------------------------------------- */

await check("T-B9 a Mac envelope under ms-2 while this browser sends with ms-1 is key_id_drift with both ids", async function () {
    const { client, socket } = await fleetOfOne();
    await fromMac(client, socket, "s/mac-01/s2", { session: { id: "s2" } }, "ms-2");
    const drift = client.trail.snapshot().key_drift;
    assert.deepEqual(drift.map((row) => [row.machine, row.sent, row.received]), [["mac-01", "ms-1", "ms-2"]]);
    need(sheetModule, "view/cloud-status.js");
    const view = sheetModule.cloudStatusView({ trail: client.trail.snapshot(), macs: null });
    const line = view.browser.find((text) => text.includes("key_id_drift"));
    assert.ok(line && line.includes("ms-1") && line.includes("ms-2"), "the sheet names both: " + view.browser.join(" | "));
    assert.equal(view.repair, true, "and offers the pairing repair");
    await fromMac(client, socket, "s/mac-01/s2", { session: { id: "s2" } }, "ms-1");
    assert.deepEqual(client.trail.snapshot().key_drift, [], "an envelope under the same key clears it");
});

/* ---- T-M1, T-M3 browser halves · notices --------------------------------------------------- */

await check("T-M1 a key_id_mismatch drop naming this device's pending seq settles it as mac_transport", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.transcript("s1");
    const sent = await nextCommand(socket, "transcript");
    await capable(client, socket, { dropped: { key_id_mismatch: 1 }, recent_drops: [
        { at_ms: 1789289990000, sender: "someone-else", seq: sent.seq, code: "replay", key_id: null, expected_key_id: null, highest_seq: 1 },
        { at_ms: 1789289990000, sender: DEVICE, seq: sent.seq, code: "key_id_mismatch", key_id: "ms-1", expected_key_id: "ms-2", highest_seq: null }
    ] });
    const ended = await outcome(read);
    assert.equal(ended.state, "rejected", "the Mac's notice settles the read that would have timed out");
    assert.deepEqual([ended.error.layer, ended.error.code], ["mac_transport", "key_id_mismatch"]);
    assert.deepEqual(ended.error.detail, { key_id: "ms-1", expected_key_id: "ms-2" });
    assert.equal(ended.error.ref.seq, sent.seq);
});

await check("T-M3 a replay drop of this tab's seq settles it and says another tab is open", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.git("s1");
    const sent = await nextCommand(socket, "git");
    await capable(client, socket, { dropped: { replay: 1 }, recent_drops: [
        { at_ms: 1789289990000, sender: DEVICE, seq: sent.seq, code: "replay", key_id: null, expected_key_id: null, highest_seq: sent.seq + 10 }
    ] });
    const ended = await outcome(read);
    assert.deepEqual([ended.state, ended.error && ended.error.layer, ended.error && ended.error.code], ["rejected", "mac_transport", "replay"]);
    assert.equal(ended.error.detail.highest_seq, sent.seq + 10);
    assert.equal(textModule && textModule.describeFailure(ended.error).text, T.webFailOtherTab);
    need(sheetModule, "view/cloud-status.js");
    const view = sheetModule.cloudStatusView({ trail: client.trail.snapshot(), macs: null });
    assert.ok(view.hints.includes(T.webFailOtherTab), "the sheet says this device is open in another tab");
});

await check("§11.2 a notice for a request that has no reply address settles it in the Mac's layer", async function () {
    const { client, socket } = await fleetOfOne();
    const pressed = client.dispatch("mac-01", { title: "x" });
    const sent = await nextCommand(socket, "dispatch");
    const read = client.info("s1");
    const second = await nextCommand(socket, "info");
    await capable(client, socket, { recent_notices: [
        { at_ms: 1789289990000, sender: DEVICE, seq: second.seq, request: null, layer: "mac_preflight", code: "malformed_command" }
    ] });
    const ended = await outcome(read);
    assert.deepEqual([ended.error && ended.error.layer, ended.error && ended.error.code], ["mac_preflight", "malformed_command"]);
    await outcome(pressed);
    assert.ok(sent.seq < second.seq);
});

/* ---- T-L2 browser half · detail and words -------------------------------------------------- */

await check("T-L2 command_clock_uncertain keeps reason and clears_in_ms and is said with the seconds", async function () {
    const { client, socket } = await fleetOfOne();
    const speaking = client.voice("AAAA", 16000);
    const sent = await nextCommand(socket, "voice");
    await answer(client, socket, "mac-01", { read: "action:" + sent.request, status: 503, error: {
        code: "command_clock_uncertain", message: "The command durability boundary is unavailable.",
        layer: "mac_ledger", seq: sent.seq, detail: { reason: "token_rotation_window", clears_in_ms: 41000, session_title: "private" }
    } });
    const ended = await outcome(speaking);
    assert.deepEqual([ended.error.layer, ended.error.code, ended.error.status], ["mac_ledger", "command_clock_uncertain", 503]);
    assert.deepEqual(ended.error.detail, { reason: "token_rotation_window", clears_in_ms: 41000 }, "§11.6 whitelist only");
    assert.equal(ended.error.ref.seq, sent.seq);
    need(textModule, "core/failure-text.js");
    const said = textModule.failureSentence(ended.error);
    assert.ok(said.startsWith(fill(T.webFailClockIn, { n: 41 })), "the seconds are in the sentence: " + said);
    assert.ok(!said.includes("durability"), "and the Mac's English is not");
});

await check("§11.1 a Mac reply error with no layer is recorded as layer mac", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.skills("s1");
    await nextCommand(socket, "skills");
    await fromMac(client, socket, "t/mac-01/s1", { read: "skills", status: 404, error: { code: "not_found", message: "gone" } });
    const ended = await outcome(read);
    assert.deepEqual([ended.error.layer, ended.error.code, ended.error.status], ["mac", "not_found", 404]);
});

/* ---- T-G1 browser half · executed but undelivered ------------------------------------------ */

await check("T-G1 a timed-out read asks a capable Mac, and an executed command is said as a lost reply", async function () {
    readTimers = [];
    const { client, socket } = await fleetOfOne();
    await capable(client, socket);
    const read = client.focus("s1");
    const sent = await nextCommand(socket, "focus");
    readTimers.splice(0).forEach(function (fire) { fire(); });
    const status = await nextCommand(socket, "cloud.status");
    assert.equal(status.ch, "ctl/mac-01");
    await answer(client, socket, "mac-01", { read: "read:" + status.request, status: 200, body: {
        clawdline_cloud_status: 1, commands: [{ sender: DEVICE, seq: sent.seq, request: sent.request, type: "focus",
            session: "s1", accepted_at_ms: 1, executed_at_ms: 2, outcome: "ok", delivered_at_ms: null,
            undeliverable: "command_answer_undeliverable", refusal: null }] } });
    const ended = await outcome(read);
    assert.deepEqual([ended.state, ended.error && ended.error.layer, ended.error && ended.error.code],
        ["rejected", "mac_reply", "command_answer_undeliverable"]);
    need(textModule, "core/failure-text.js");
    assert.equal(textModule.describeFailure(ended.error).text, T.webFailReplyLost);
});

await check("T-G1 a Mac without the capability times out as cloud_read_timeout without asking", async function () {
    readTimers = [];
    const { client, socket } = await fleetOfOne();
    const read = client.focus("s1");
    await nextCommand(socket, "focus");
    const before = published(socket).length;
    readTimers.splice(0).forEach(function (fire) { fire(); });
    const ended = await outcome(read);
    assert.deepEqual([ended.error && ended.error.layer, ended.error && ended.error.code], ["browser", "cloud_read_timeout"]);
    assert.equal(published(socket).length, before, "no status read goes to a Mac that cannot answer one");
});

/* ---- §11.4 · request on every command, gated ----------------------------------------------- */

await check("§11.4 answer and key carry request only after that Mac showed cloud_status.v >= 1", async function () {
    const { client, socket } = await fleetOfOne();
    const first = client.answer("s1", "2");
    const plain = await nextCommand(socket, "answer");
    assert.equal(plain.request, undefined, "an old Mac is not sent a key it would refuse");
    await frame(client, socket, { type: "ack", ch: "ctl/mac-01", seq: plain.seq, status: "delivered", fanout: 1 });
    assert.equal((await outcome(first)).state, "resolved", "a delivered keypress resolves on the relay's ack");
    await capable(client, socket);
    const second = client.key("s1", "enter");
    const gated = await nextCommand(socket, "answer");
    assert.match(String(gated.request), /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/, "a capable Mac gets a request id");
    await frame(client, socket, { type: "ack", ch: "ctl/mac-01", seq: gated.seq, status: "delivered", fanout: 1 });
    assert.equal((await outcome(second)).state, "resolved");
});

/* ---- §6.3 · other tabs of this device ------------------------------------------------------ */

await check("§6.3 two tabs of the same device find each other", async function () {
    FakeBroadcastChannel.rooms.clear();
    const one = cloudClient({ BroadcastChannel: FakeBroadcastChannel, tabID: "tab-one" });
    await ready(one);
    const two = cloudClient({ BroadcastChannel: FakeBroadcastChannel, tabID: "tab-two" });
    await ready(two);
    assert.equal(one.trail.liveOtherTabs(), 1, "the first tab heard the second say hello");
    assert.equal(two.trail.liveOtherTabs(), 1, "and the second heard the first answer");
    assert.equal(one.trail.snapshot().other_tabs, 1);
});

/* ---- §4.3 · the sheet's data --------------------------------------------------------------- */

await check("§4.3 the sheet joins this browser's steps with the Mac's for the same ref, focused there", async function () {
    const { client, socket } = await fleetOfOne();
    const read = client.transcript("s1");
    const sent = await nextCommand(socket, "transcript");
    await frame(client, socket, { type: "ack", ch: "ctl/mac-01", seq: sent.seq, status: "delivered", fanout: 1 });
    need(sheetModule, "view/cloud-status.js");
    const macs = [{ machine: "mac-01", error: null, status: {
        counting_since: "2026-09-13T06:40:12Z", clock_guard: { state: "uncertain", reason: "token_rotation_window" },
        identity: { key_id: "ms-2", roster_readable: false }, inbound: { dropped: { replay: 13 } },
        commands: [{ sender: DEVICE, seq: sent.seq, request: null, type: "transcript", session: "s1",
            accepted_at_ms: 1789290000000, executed_at_ms: null, outcome: null, delivered_at_ms: null,
            undeliverable: null, refusal: { layer: "mac_ledger", code: "command_clock_uncertain" } }] } }];
    const view = sheetModule.cloudStatusView({ trail: client.trail.snapshot(), macs, focus: { sender: DEVICE, seq: sent.seq } });
    const row = view.commands.find((entry) => entry.ref.seq === sent.seq);
    assert.ok(row && row.focus, "the row for that ref is the one the sheet scrolls to");
    assert.ok(row.browserSteps.includes("sealed") && row.browserSteps.includes("relayed"), "this browser's steps: " + row.browserSteps);
    assert.ok(row.macSteps.includes("accepted"), "the Mac's steps: " + row.macSteps);
    assert.equal(row.refusal, "mac_ledger · command_clock_uncertain");
    const mac = view.macs[0].lines.join(" | ");
    assert.ok(mac.includes("uncertain") && mac.includes("token_rotation_window"), "the clock guard: " + mac);
    assert.ok(mac.includes("ms-2") && mac.includes("replay 13") && mac.includes(T.webFailRoster), "key, drops and roster: " + mac);
    const failed = sheetModule.cloudStatusView({ trail: client.trail.snapshot(),
        readError: Object.assign(new Error("English"), { code: "cloud_read_timeout", layer: "browser", ref: null }) });
    assert.ok(failed.readError.startsWith(T.webFailNoAnswer) && failed.readError.includes("cloud_read_timeout"),
        "a failed status read is a typed, visible line: " + failed.readError);
    read.catch(function () { });
    client.stop();
});

await check("§4.3 the Cloud client answers cloud.status for every published Mac, one typed error per Mac", async function () {
    const { client, socket } = await fleetOfOne();
    await fromMac(client, socket, "orch/mac-02", { tasks: [] });
    const asking = client.cloudStatus();
    const first = await nextCommand(socket, "cloud.status");
    const second = await nextCommand(socket, "cloud.status");
    const byMachine = { [first.ch]: first, [second.ch]: second };
    await answer(client, socket, "mac-01", { read: "read:" + byMachine["ctl/mac-01"].request, status: 200, body: { commands: [] } });
    await answer(client, socket, "mac-02", { read: "read:" + byMachine["ctl/mac-02"].request, status: 404, error: { code: "unknown_command", message: "old" } });
    const ended = await outcome(asking);
    assert.equal(ended.state, "resolved");
    const rows = Object.fromEntries(ended.value.machines.map((row) => [row.machine, row]));
    assert.deepEqual(rows["mac-01"].status, { commands: [] });
    assert.deepEqual([rows["mac-02"].error.layer, rows["mac-02"].error.code], ["mac", "unknown_command"]);
});

/* ---- ends ---------------------------------------------------------------------------------- */

if (failures.length) {
    console.log("web cloud failure checks: " + failures.length + " red, " + passed.length + " green");
    console.log("  " + failures.join("\n  "));
    process.exit(1);
}
console.log("web cloud failure checks passed: " + passed.length);
