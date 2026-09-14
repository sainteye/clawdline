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

await check("T-B3 a non-closing relay error does not rename a later network close", async function () {
    const { client, socket } = await fleetOfOne();
    await frame(client, socket, { type: "error", code: "over_capacity", message: "too many subscriptions" });
    socket.dropped(1006);
    const later = await outcome(client.transcript("s1"));
    assert.deepEqual([later.error.layer, later.error.code], ["browser", "offline"]);
    assert.equal(client.trail.connection.last_relay_error.code, "over_capacity",
        "the frame remains diagnostic history without becoming the close reason");
});

/* ---- T-B4 · §5 rule 2: no synchronous throw ------------------------------------------------ */

// Called for their effect, never awaited by a page; they are called here too, and must not throw.
const LIFECYCLE = new Set(["events", "subscribe", "stop", "retire"]);
// Cannot fail from a cached answer, or succeed by opening a socket; still held to "a thenable".
const NEED_NOT_REJECT = new Set(["sessions", "tasks", "machines", "start", "refresh", "whenReady"]);

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

await check("T-B7 renewal marks an already-written key as uncertain, never retryable", async function () {
    const { client, socket } = await fleetOfOne();
    await capable(client, socket);
    const pressed = client.key("s1", "enter");
    const sent = await nextCommand(socket, "answer");
    await frame(client, socket, { type: "ack", ch: "ctl/mac-01", seq: sent.seq,
        status: "delivered", fanout: 1 });
    client.retire();
    const ended = await outcome(pressed);
    assert.deepEqual([ended.state, ended.error.code, ended.error.retryable],
        ["rejected", "delivery_unconfirmed", false]);
});

/* ---- T-B8 · diagnostics over Cloud --------------------------------------------------------- */

await check("T-B8 the Cloud diagnostics report is a diagnostics.report command, size-checked, with the trail", async function () {
    const { client, socket } = await fleetOfOne();
    await capable(client, socket);
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

await check("T-B8 an old Mac refuses diagnostics immediately instead of waiting sixty seconds", async function () {
    const { client, socket } = await fleetOfOne();
    const before = published(socket).length;
    const ended = await outcome(client.diagnosticsReport({ currentTrace: [] }));
    assert.deepEqual([ended.state, ended.error.code], ["rejected", "cloud_feature_unavailable"]);
    assert.equal(published(socket).length, before, "no unsupported command was published");
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
        layer: "mac_ledger", seq: sent.seq, detail: { reason: "stability_period_incomplete", clears_in_ms: 41000, session_title: "private" }
    } });
    const ended = await outcome(speaking);
    assert.deepEqual([ended.error.layer, ended.error.code, ended.error.status], ["mac_ledger", "command_clock_uncertain", 503]);
    assert.deepEqual(ended.error.detail, { reason: "stability_period_incomplete", clears_in_ms: 41000 }, "§11.6 whitelist only");
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

await check("T-G1 an old timeout probe cannot reject a new waiter with the same read key", async function () {
    readTimers = [];
    const { client, socket } = await fleetOfOne();
    await capable(client, socket);
    const first = client.transcript("s1");
    await nextCommand(socket, "transcript");
    const firstTimeout = readTimers.shift();
    firstTimeout();
    const probe = await nextCommand(socket, "cloud.status");
    await fromMac(client, socket, "t/mac-01/s1", { read: "transcript", status: 200,
        body: { session: { id: "s1", title: "first" } } });
    assert.equal((await outcome(first)).state, "resolved");
    const second = client.transcript("s1");
    const secondSent = await nextCommand(socket, "transcript");
    await answer(client, socket, "mac-01", { read: "read:" + probe.request, status: 200,
        body: { clawdline_cloud_status: 1, commands: [] } });
    assert.equal((await outcome(second)).state, "pending",
        "the first waiter's probe did not settle its replacement");
    await fromMac(client, socket, "t/mac-01/s1", { read: "transcript", status: 200,
        body: { session: { id: "s1", title: "second" }, seq: secondSent.seq } });
    assert.equal((await outcome(second)).state, "resolved");
});

await check("T-G1 every never-delivered Mac reply reason uses the reply-lost sentence", async function () {
    need(textModule, "core/failure-text.js");
    for (const code of ["command_answer_undeliverable", "read_answer_undeliverable",
        "refusal_undeliverable", "peer_rejected", "receipt_expired", "ready_expired"]) {
        assert.equal(textModule.describeFailure({ code: code, layer: "mac_reply" }).text,
            T.webFailReplyLost, code);
    }
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
    assert.equal((await outcome(second)).state, "pending", "relay delivery is not the Mac's result");
    await fromMac(client, socket, "t/mac-01/s1", { read: "action:" + gated.request,
        status: 403, error: { code: "cloud_commands_disabled", layer: "mac_preflight",
            seq: gated.seq } });
    const ended = await outcome(second);
    assert.deepEqual([ended.state, ended.error.code, ended.error.layer],
        ["rejected", "cloud_commands_disabled", "mac_preflight"]);
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
        counting_since: "2026-09-13T06:40:12Z", clock_guard: { state: "uncertain", reason: "stability_period_incomplete" },
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
    assert.ok(mac.includes("uncertain") && mac.includes("stability_period_incomplete"), "the clock guard: " + mac);
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
    await capable(client, socket);
    await fromMac(client, socket, "orch/mac-02", { tasks: [], cloud_status: { v: 1 } });
    await fromMac(client, socket, "orch/mac-03", { tasks: [] });
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
    assert.deepEqual([rows["mac-03"].status, rows["mac-03"].error, rows["mac-03"].capable], [null, null, false],
        "a Mac that never published cloud_status is not asked");
});

/* ---- viewer events · receive failures reach the paired Mac's fixed file --------------------- */

// `net/cloud-viewer-events.js` and the delivery half of `CloudClient`: every receive failure leaves
// a row naming its stage and original error, rows survive a reload, and they go to the paired Mac
// as `diagnostics.events` on their own — removed only when that Mac's receipt names the batch.
const viewerModule = await optional("../Resources/web/app/js/net/cloud-viewer-events.js");

/** `localStorage` as a page sees it, with a switch that makes every write throw. */
class FakeStorage {
    constructor() { this.map = new Map(); this.failWrites = false; }
    getItem(key) { return this.map.has(key) ? this.map.get(key) : null; }
    setItem(key, value) {
        if (this.failWrites) throw Object.assign(new Error("The quota has been exceeded."), { name: "QuotaExceededError" });
        this.map.set(key, String(value));
    }
    removeItem(key) { this.map.delete(key); }
}

function viewerTimers() {
    const pending = [];
    return { pending,
        setTimeout(fn, ms) { pending.push({ fn, ms }); return pending.length; },
        clearTimeout() { },
        fire() { const due = pending.splice(0); due.forEach((timer) => timer.fn()); return due.length; } };
}

let viewerClock = 1_789_400_000_000;
function viewerLog(storage) {
    need(viewerModule, "net/cloud-viewer-events.js");
    return new viewerModule.ViewerEventLog({ storage: storage || null, key: "test.viewer-events",
        now: () => viewerClock });
}

const MAC_STATUS = { v: 1, generated_at_ms: 1789290000000, counting_since_ms: 1789280000000,
    clock_guard: { state: "ready", reason: null, clears_at_ms: null }, token_expires_at_ms: 1789290240000,
    key_id: "ms-1", roster_readable: true, dropped: {}, recent_drops: [], recent_notices: [] };

/** A connected client with a log and manual delivery timers, and mac-01 authenticated on orch/. */
async function viewerFleet(options) {
    options = options || {};
    const storage = options.storage || new FakeStorage();
    const log = options.log || viewerLog(storage);
    const timers = viewerTimers();
    const errors = [];
    const client = cloudClient(Object.assign({ viewerEvents: log, viewerEventTimers: timers,
        webBuild: "web-build-1" }, options.client || {}));
    client.events((event) => { if (event.type === "error") errors.push(event.error); });
    const socket = await ready(client);
    if (options.orch !== false) {
        await fromMac(client, socket, "orch/mac-01", Object.assign({ tasks: [],
            machine: { name: "Mac", platform: "macos" }, app: { build: options.macBuild || "mac-build-1" },
            cloud_status: MAC_STATUS }, options.orch || {}));
    }
    return { client, socket, log, storage, timers, errors };
}

async function sealedFromMac(fields, payload, master) {
    return sealEnvelope(Object.assign({ ch: "s/mac-01/s1", seq: ++macSequence, ts: 1787817600000,
        class: "stream", key_id: "ms-1", sender: DEVICE }, fields || {}),
    JSON.stringify(payload === undefined ? { session: { id: "s1" } } : payload), master || masterKey, signingKey);
}

async function receiveEnvelope(client, socket, envelope, realign) {
    socket.receive({ type: "envelope", envelope: envelope, realign: realign === true });
    await client.messageChain;
}

function failureRows(log) {
    return log.snapshot().rows.filter((row) => row.event === "cloud.receive.failed");
}

await check("viewer events · an unknown sender (a second machine) is a row with the sender, the keys held and the target Mac", async function () {
    const { client, socket, log, errors } = await viewerFleet();
    const stranger = await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" }, { tasks: [] });
    await receiveEnvelope(client, socket, stranger);
    assert.equal(errors.at(-1) && errors.at(-1).code, "unknown_sender", "the emitted code is unchanged");
    const rows = failureRows(log);
    assert.equal(rows.length, 1, "one row for one failure");
    const data = rows[0].data;
    assert.deepEqual([data.code, data.stage, data.channel_kind, data.machine, data.sender, data.key_id, data.browser_key_id],
        ["unknown_sender", "sender_key_lookup", "orch", "linux-01", "linux-executor", "ms-1", "ms-1"]);
    assert.deepEqual([data.sender_key_found, data.target_machine, data.target_sender, data.sender_is_target],
        [false, "mac-01", DEVICE, false], "H1: not the target Mac's sender, and no key for it");
    assert.deepEqual([data.routed_machine, data.pairing_found, data.machines_with_pairing, data.machines_without_pairing],
        ["linux-01", false, ["mac-01"], ["linux-01"]], "no pairing for the machine, and the one it has");
    assert.ok(data.senders_with_keys.includes(DEVICE) && !data.senders_with_keys.includes("linux-executor"));
    assert.ok(data.opens_since_ready_total >= 1 && data.opens_since_ready_sender === 0 && Number.isInteger(data.ms_since_ready));
    assert.equal(data.web_build, "web-build-1");
    assert.equal(errors.at(-1).viewerEvent.n, rows[0].n, "the thrown failure names its row, for the door's row");
});

await check("viewer events · the same key id under a different master secret fails at decrypt with OperationError", async function () {
    const { client, socket, log, errors } = await viewerFleet();
    const otherMaster = await importMasterSecret(Buffer.alloc(32, 0x42).toString("base64"));
    await receiveEnvelope(client, socket, await sealedFromMac({ seq: 7001 }, { session: { id: "s1" } }, otherMaster), true);
    assert.equal(errors.at(-1).code, "unreadable_envelope");
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.code, data.stage, data.error_name, data.sender_key_found, data.sender_is_target,
        data.key_id, data.seq, data.realign, data.class],
    ["unreadable_envelope", "decrypt", "OperationError", true, true, "ms-1", 7001, true, "stream"],
    "signature verified with the paired key; AES-GCM refused the ciphertext");
    assert.ok(Number.isInteger(data.ct_bytes) && data.ct_bytes >= 16, "the ciphertext's length, not the ciphertext");
});

await check("viewer events · an envelope failing validateEnvelope is stage validate with the TypeError's words", async function () {
    const { client, socket, log, errors } = await viewerFleet();
    const valid = await sealedFromMac({}, { session: { id: "s1" } });
    await receiveEnvelope(client, socket, Object.assign({}, valid, { extra: 1 }));
    assert.equal(errors.at(-1).code, "unreadable_envelope");
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.stage, data.error_name, data.error_message],
        ["validate", "TypeError", "envelope fields do not match protocol v1"],
        "H4: the original exception rather than unreadable_envelope's sentence");
    assert.ok(data.field_names.includes("extra") && data.field_names.includes("nonce"), "field names only: " + data.field_names);
});

await check("viewer events · a legacy pin store that rejects before the machine is bound is stage sender_key_lookup with its DOMException name", async function () {
    let storeDown = false;
    const { client, socket, log, errors } = await viewerFleet({ orch: false, client: { senderKeys: {},
        resolveSenderKey: (sender) => storeDown
            ? Promise.reject(Object.assign(new Error("The operation failed for reasons unrelated to the database itself"), { name: "UnknownError" }))
            : Promise.resolve(sender === DEVICE ? senderKey : null) } });
    await fromMac(client, socket, "s/mac-02/s1", { session: { id: "s1" } });
    storeDown = true;
    await receiveEnvelope(client, socket, await sealedFromMac());
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.code, data.stage, data.error_name, data.sender_key_source, data.routed_machine, data.pairing_found],
        [null, "sender_key_lookup", "UnknownError", "store", "mac-01", false],
        "H3 on the legacy path: the pin store failed for a machine this client has not bound yet");
    assert.ok(data.opens_since_ready_sender >= 1, "earlier opens from the same sender: " + data.opens_since_ready_sender);
    assert.equal(errors.at(-1).code, undefined, "the transport emits the raw error exactly as before");
});

await check("viewer events · a storage write that throws is counted, never thrown, and the rows still leave", async function () {
    const { client, socket, log, storage, timers } = await viewerFleet();
    storage.failWrites = true;
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" }, { tasks: [] }));
    await fromMac(client, socket, "s/mac-01/s2", { session: { id: "s2" } });
    assert.ok(client.sessionSnapshots.size >= 1, "the transport kept working through a failing store");
    assert.equal(failureRows(log).length, 1, "the row is held in memory");
    storage.failWrites = false;
    timers.fire();
    const sent = await nextCommand(socket, "diagnostics.events");
    assert.ok(sent.batch.completeness.storage_errors >= 1, "the batch states the failed writes: " + sent.batch.completeness.storage_errors);
    assert.equal(sent.batch.rows.length, 1);
    await answer(client, socket, "mac-01", { read: "action:" + sent.request, status: 200,
        body: { ok: true, batch_id: sent.batch.batch_id, rows: 1 } });
    await until(() => client.lastViewerDelivery && client.lastViewerDelivery.state === "delivered", "the receipt");
    storage.failWrites = true;
    log.rememberTarget({ machine: "mac-02", sender: "other", capable: true });
    viewerClock += 5 * 60 * 1000;
    const before = published(socket).length;
    const attempt = await outcome(client._deliverViewerEvents(), 200);
    assert.deepEqual([log.pending(), attempt.state, attempt.value && attempt.value.state, published(socket).length],
        [false, "resolved", "empty", before], "a store that keeps refusing does not send batches of nothing");
});

await check("viewer events · an older Mac's unknown_command keeps the rows and does not retry until a build changes", async function () {
    const { client, socket, log, timers } = await viewerFleet();
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" }, { tasks: [] }));
    assert.ok(timers.pending.length === 1 && timers.pending[0].ms >= 5000, "a row schedules one debounced delivery");
    timers.fire();
    const sent = await nextCommand(socket, "diagnostics.events");
    assert.deepEqual([sent.ch, sent.session], ["ctl/mac-01", MACHINE_REPLY], "addressed to the paired Mac, not the only machine");
    await answer(client, socket, "mac-01", { read: "action:" + sent.request, status: 400,
        error: { code: "unknown_command", layer: "mac_preflight", message: "This Mac does not know that Cloud command." } });
    await until(() => client.lastViewerDelivery, "the delivery outcome");
    assert.deepEqual([client.lastViewerDelivery.state, client.lastViewerDelivery.code], ["blocked", "unknown_command"]);
    assert.equal(log.snapshot().outbox.batch_id, sent.batch.batch_id, "the batch is still on the phone");
    viewerClock += 10 * 60 * 1000;
    const before = published(socket).length;
    const again = await client._deliverViewerEvents();
    assert.deepEqual([again.state, published(socket).length], ["blocked", before], "no second publish to the same build");
    assert.equal(timers.pending.length, 0, "and nothing scheduled to try");
    await fromMac(client, socket, "orch/mac-01", { tasks: [], machine: { platform: "macos" },
        app: { build: "mac-build-2" }, cloud_status: MAC_STATUS });
    const retried = client._deliverViewerEvents();
    const resent = await nextCommand(socket, "diagnostics.events");
    assert.deepEqual([resent.request, JSON.stringify(resent.batch)], [sent.request, JSON.stringify(sent.batch)],
        "a rebuilt Mac is asked again with the same request and the same bytes");
    await answer(client, socket, "mac-01", { read: "action:" + resent.request, status: 413,
        error: { code: "viewer_events_too_large", layer: "mac_route", message: "too large" } });
    assert.deepEqual([(await retried).state, log.snapshot().outbox && log.snapshot().outbox.rows], ["blocked", 1],
        "a batch the Mac will not take is kept too");
});

await check("viewer events · a burst of 50 failures keeps bounded rows and the exact dropped count", async function () {
    const { client, socket, log, timers } = await viewerFleet();
    for (let i = 0; i < 50; i += 1) {
        await receiveEnvelope(client, socket, await sealedFromMac({ ch: "s/linux-01/t" + i, sender: "linux-executor" }), true);
    }
    const state = log.snapshot();
    assert.equal(failureRows(log).length, 3, "three rows for one key in one window");
    assert.equal(state.counts.rate_limited, 47);
    timers.fire();
    const sent = await nextCommand(socket, "diagnostics.events");
    const completeness = sent.batch.completeness;
    assert.deepEqual([completeness.rows, completeness.dropped_rate_limited, completeness.dropped_overflow],
        [3, 47, 0]);
    assert.equal(completeness.rows + completeness.dropped_overflow, completeness.n_to - completeness.n_from + 1,
        "every kept or overflowed row consumed one n");
    assert.equal(completeness.rate_limited.length, 1);
    assert.deepEqual([completeness.rate_limited[0].dropped, completeness.rate_limited[0].event],
        [47, "cloud.receive.failed"]);
});

await check("viewer events · no row or batch ever holds nonce, ct, sig or plaintext", async function () {
    const { client, socket, log, storage, timers } = await viewerFleet();
    const secret = "TOP-SECRET-SESSION-TITLE-" + "x".repeat(8);
    const envelopes = [
        await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" }, { tasks: [], title: secret }),
        await sealedFromMac({}, { session: secret }),
        await sealedFromMac({}, { session: { id: "s1", title: secret } },
            await importMasterSecret(Buffer.alloc(32, 0x43).toString("base64")))
    ];
    const tampered = Object.assign({}, envelopes[1], { sig: envelopes[0].sig });
    for (const envelope of envelopes.concat([tampered])) await receiveEnvelope(client, socket, envelope);
    const stages = failureRows(log).map((row) => row.data.stage);
    assert.deepEqual(stages.sort(), ["apply", "decrypt", "sender_key_lookup", "signature_verify"],
        "four failures, four stages");
    assert.deepEqual(log.record("test.fields", { nonce: "n", ct: "c", sig: "s", plaintext: "p", title: "t", token: "k", kept: 1 }).rateLimited, false);
    timers.fire();
    const sent = await nextCommand(socket, "diagnostics.events");
    const stored = Array.from(storage.map.values()).join("\n");
    const wire = JSON.stringify(sent.batch);
    for (const text of [stored, wire]) {
        assert.ok(!text.includes(secret), "no plaintext");
        for (const envelope of envelopes.concat([tampered])) {
            for (const field of ["nonce", "ct", "sig"]) assert.ok(!text.includes(envelope[field]), "no " + field + " value");
        }
        assert.ok(!/"(nonce|ct|sig|plaintext|title|token)":/.test(text), "no field named for a secret");
    }
    assert.deepEqual(sent.batch.rows.find((row) => row.event === "test.fields").data, { kept: 1 });
});

await check("viewer events · rows survive a reload and leave the phone only on a receipt naming their batch", async function () {
    const storage = new FakeStorage();
    const first = await viewerFleet({ storage });
    await receiveEnvelope(first.client, first.socket, await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" }, { tasks: [] }));
    first.client.stop();
    const reloaded = viewerLog(storage);
    assert.equal(reloaded.snapshot().rows.length, 1, "a new page reads the rows the last one kept");
    const second = await viewerFleet({ storage, log: reloaded });
    viewerClock += 1000;
    second.timers.fire();
    const sent = await nextCommand(second.socket, "diagnostics.events");
    assert.equal(sent.batch.rows[0].data.sender, "linux-executor");
    assert.equal(reloaded.snapshot().outbox.batch_id, sent.batch.batch_id, "sent is not removed");
    await answer(second.client, second.socket, "mac-01", { read: "action:" + sent.request, status: 200,
        body: { ok: true, batch_id: "some-other-batch", rows: 1 } });
    await until(() => second.client.lastViewerDelivery, "the first outcome");
    assert.equal(second.client.lastViewerDelivery.state, "failed", "a receipt for another batch is not an acknowledgement");
    assert.ok(viewerModule.ViewerEventLog && new viewerModule.ViewerEventLog({ storage, key: "test.viewer-events" }).snapshot().outbox,
        "and a reload still finds the batch");
    viewerClock += 61_000;
    const delivering = second.client._deliverViewerEvents();
    const resent = await nextCommand(second.socket, "diagnostics.events");
    assert.equal(resent.request, sent.request, "the resend is the same request, so the Mac's ledger does not append twice");
    await answer(second.client, second.socket, "mac-01", { read: "action:" + resent.request, status: 200,
        body: { ok: true, batch_id: sent.batch.batch_id, rows: 1, path: "/Users/x/Library/Logs/Clawdline/diagnostics/cloud-viewer-events.jsonl" } });
    const ended = await delivering;
    assert.equal(ended.state, "delivered");
    const after = new viewerModule.ViewerEventLog({ storage, key: "test.viewer-events" }).snapshot();
    assert.deepEqual([after.outbox, after.rows.length, after.acknowledged.batch_id], [null, 0, sent.batch.batch_id]);
});

await check("viewer events · a second machine does not stop delivery, and a Mac without cloud_status is never asked", async function () {
    const withLinux = await viewerFleet();
    await fromMac(withLinux.client, withLinux.socket, "orch/linux-01", { tasks: [], machine: { platform: "linux" } });
    assert.throws(() => withLinux.client._onlyMachine("diagnostics"), (error) => error.code === "cloud_machine_ambiguous",
        "the fleet that breaks _onlyMachine");
    await receiveEnvelope(withLinux.client, withLinux.socket, await sealedFromMac({ ch: "s/mac-01/s1", sender: "nobody" }));
    withLinux.timers.fire();
    assert.equal((await nextCommand(withLinux.socket, "diagnostics.events")).ch, "ctl/mac-01");

    const old = await viewerFleet({ orch: { cloud_status: undefined } });
    await receiveEnvelope(old.client, old.socket, await sealedFromMac({ sender: "nobody" }));
    const before = published(old.socket).length;
    const outcomeOld = await old.client._deliverViewerEvents();
    assert.deepEqual([outcomeOld.state, outcomeOld.why, published(old.socket).length],
        ["deferred", "cloud_feature_unavailable", before], "typed, local, nothing published, rows kept");
    assert.equal(failureRows(old.log).length, 1);
});

/* ---- viewer events · the per-machine pairing receive path ----------------------------------- */

// The hosted console hands `CloudClient` both pairing hooks, so every routed envelope asks the
// store for this browser's pairing with its machine first, and a pre-scoping browser's route is
// bound only after its pin verified and its content decrypted. These checks drive that
// composition and read the row each failure leaves; one that should leave none says so.

/** A pairing as `CloudViewerSession.machinePairing` answers one: exact unless `legacy`. */
function pairingFor(machine, fields) {
    return Object.assign({ machineID: machine, senderID: DEVICE, keyID: "ms-1", masterKey: masterKey,
        senderKey: senderKey, legacy: false }, fields || {});
}

/** Both hooks over a Map of pairings; `hooks.down` rejects every lookup, `hooks.bind` replaces the binder. */
function pairedClient(store, client) {
    const hooks = { lookups: [], bindings: [], down: null, bind: null };
    const options = Object.assign({ senderKeys: {},
        resolveSenderKey: (sender) => Promise.resolve(sender === DEVICE ? senderKey : null),
        resolveMachinePairing: (machine) => {
            hooks.lookups.push(machine);
            return hooks.down ? Promise.reject(hooks.down) : Promise.resolve(store.get(machine) || null);
        },
        bindLegacyMachine: (machine, sender, keyID) => {
            hooks.bindings.push([machine, sender, keyID]);
            if (hooks.bind) return hooks.bind(machine, sender, keyID);
            const bound = pairingFor(machine, { senderID: sender, keyID: keyID, legacy: true });
            store.set(machine, bound);
            return Promise.resolve(bound);
        } }, client || {});
    return { options, hooks };
}

const storeRejection = () => Object.assign(new Error("The operation failed for reasons unrelated to the database itself"),
    { name: "UnknownError" });

await check("viewer events · pairing · a second machine this browser is not paired with is H1a: no pairing, no pin", async function () {
    const store = new Map([["mac-01", pairingFor("mac-01")]]);
    const { options } = pairedClient(store);
    const { client, socket, log, errors } = await viewerFleet({ client: options });
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" }, { tasks: [] }));
    assert.equal(errors.at(-1).code, "unknown_sender", "the emitted code is unchanged");
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.stage, data.routed_machine, data.pairing_found, data.pairing_source, data.pairing_legacy,
        data.pairing_key_id, data.sender_key_found, data.sender_key_source],
    ["sender_key_lookup", "linux-01", false, "store", null, null, false, "store"],
    "the store has no pairing for linux-01, so the legacy pin was asked and had none");
    assert.ok(Number.isInteger(data.pairing_lookup_ms), "the pairing lookup was timed");
    assert.deepEqual([data.machines_with_pairing, data.machines_without_pairing, data.target_machine, data.sender_is_target],
        [["mac-01"], ["linux-01"], "mac-01", false]);
});

await check("viewer events · pairing · a paired machine under another key id is H1b: stage pairing_key_id, both key ids", async function () {
    const store = new Map([["mac-01", pairingFor("mac-01")], ["mac-02", pairingFor("mac-02", { keyID: "ms-9" })]]);
    const { options } = pairedClient(store);
    const { client, socket, log, errors } = await viewerFleet({ client: options });
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "s/mac-02/s1" }));
    assert.equal(errors.at(-1).code, "unknown_key");
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.stage, data.pairing_found, data.pairing_legacy, data.pairing_key_id, data.key_id,
        data.error_message, data.sender_key_found],
    ["pairing_key_id", true, false, "ms-9", "ms-1", "the envelope does not match the selected machine's pairing", null],
    "refused before any key was chosen, naming the pairing's key id beside the envelope's");
});

await check("viewer events · pairing · a decrypted channel from a sender other than the paired one is stage paired_sender", async function () {
    const store = new Map([["mac-01", pairingFor("mac-01")], ["mac-02", pairingFor("mac-02", { senderID: "mac-02-device" })]]);
    const { options } = pairedClient(store);
    const { client, socket, log, errors } = await viewerFleet({ client: options });
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/mac-02" }, { tasks: [] }));
    assert.equal(errors.at(-1).code, "unknown_sender");
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.stage, data.sender, data.pairing_sender, data.sender_key_source, data.sender_key_found,
        data.error_message],
    ["paired_sender", DEVICE, "mac-02-device", "pairing", true,
        "the authenticated machine channel does not match its paired sender"],
    "the signature and decrypt passed with the pairing's keys; only the clear sender differs");
    assert.equal(client.viewerVerified.has("mac-02"), false, "a channel refused here is never a delivery target");
});

await check("viewer events · pairing · machine_key_incomplete is attributed to the lookup or to the legacy binding that refused", async function () {
    const store = new Map([["mac-01", pairingFor("mac-01")],
        ["mac-02", pairingFor("mac-02", { senderKey: null })]]);
    const { options, hooks } = pairedClient(store);
    const { client, socket, log, errors } = await viewerFleet({ client: options });
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "s/mac-02/s1" }));
    assert.equal(errors.at(-1).code, "machine_key_incomplete");
    hooks.bind = (machine, sender, keyID) => Promise.resolve({ machineID: machine, senderID: sender, keyID: keyID,
        masterKey: null, senderKey: senderKey, legacy: true });
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "s/mac-03/s1" }));
    assert.equal(errors.at(-1).code, "machine_key_incomplete");
    const rows = failureRows(log).map((row) => row.data);
    assert.deepEqual(rows.map((data) => [data.stage, data.routed_machine, data.pairing_found, data.pairing_key_id]),
        [["machine_pairing_lookup", "mac-02", true, "ms-1"], ["legacy_binding", "mac-03", false, null]],
        "a stored pairing missing its sender key, then a pin whose binding came back without a content key");
    assert.deepEqual(hooks.bindings, [["mac-03", DEVICE, "ms-1"]], "the binder ran once, after the decrypt");
});

await check("viewer events · pairing · a pairing store that rejects after answering is stage machine_pairing_lookup with pairing_found_before", async function () {
    const store = new Map([["mac-01", pairingFor("mac-01")]]);
    const { options, hooks } = pairedClient(store);
    const first = await viewerFleet({ client: options });
    assert.deepEqual([failureRows(first.log).length, hooks.lookups], [0, ["mac-01"]], "the Mac opened through its pairing");
    hooks.down = storeRejection();
    const renewed = cloudClient(Object.assign({ viewerEvents: first.log, viewerEventTimers: first.timers, resumeFrom: first.client },
        options));
    const errors = [];
    renewed.events((event) => { if (event.type === "error") errors.push(event.error); });
    const socket = await ready(renewed);
    await receiveEnvelope(renewed, socket, await sealedFromMac({ ch: "s/mac-01/s1" }));
    assert.equal(errors.at(-1) && errors.at(-1).code, undefined, "the raw error escapes exactly as before, so no door");
    const data = failureRows(first.log)[0].data;
    assert.deepEqual([data.code, data.stage, data.error_name, data.pairing_found, data.pairing_found_before,
        data.pairing_source, data.sender_key_found],
    [null, "machine_pairing_lookup", "UnknownError", null, true, "store", null],
    "H3: the store that answered this machine's pairing a moment ago refused on the replacement client");
    assert.ok(Number.isInteger(data.pairing_lookup_ms), "and the refusal was timed");
});

await check("viewer events · pairing · a legacy pin bound after its decrypt leaves no row, and later rows name the bound pairing", async function () {
    const store = new Map();
    const { options, hooks } = pairedClient(store);
    const { client, socket, log, errors } = await viewerFleet({ client: options });
    const envelope = await sealedFromMac({ ch: "s/mac-01/s1" });
    await receiveEnvelope(client, socket, envelope);
    assert.deepEqual([failureRows(log).length, errors.length, hooks.bindings.length, hooks.lookups],
        [0, 0, 1, ["mac-01"]], "binding the route is a success: no row, no error, one binding, one store lookup");
    assert.ok(client.sessionSnapshots.size >= 1 && client.viewerVerified.has("mac-01"), "the snapshots were applied");
    await receiveEnvelope(client, socket, envelope);
    assert.equal(errors.at(-1).code, "replay");
    const data = failureRows(log)[0].data;
    assert.deepEqual([data.stage, data.pairing_found, data.pairing_legacy, data.pairing_source, data.pairing_found_before],
        ["sequence", true, true, "memory", true],
        "the replay is refused by sequence, through the pairing bound a moment ago and used by the envelope before it");
    assert.equal(hooks.bindings.length, 1, "and nothing was bound twice");
});

await check("viewer events · pairing · a paired Linux executor is never the target, and the batch is sealed with the Mac's pairing", async function () {
    const macMaster = await importMasterSecret(Buffer.alloc(32, 0x44).toString("base64"));
    const store = new Map([["mac-01", pairingFor("mac-01", { keyID: "mac-v2", masterKey: macMaster })],
        ["linux-01", pairingFor("linux-01", { senderID: "linux-executor" })],
        ["linux-02", pairingFor("linux-02", { senderID: "linux-executor-2" })]]);
    const { options } = pairedClient(store);
    const { client, socket, log, timers } = await viewerFleet({ orch: false, client: options });
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/linux-01", sender: "linux-executor" },
        { v: 1, machine: { name: "AWS", platform: "linux" } }));
    // A descriptor that never arrived says nothing about the platform; the missing capability still does.
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/linux-02", sender: "linux-executor-2" }, { v: 1 }));
    assert.deepEqual([client.viewerVerified.has("linux-01"), client.viewerVerified.has("linux-02"), failureRows(log).length],
        [true, true, 0], "both executors authenticated through their pairings");
    log.record("test.event", { kept: 1 });
    const linuxOnly = await client._deliverViewerEvents();
    assert.deepEqual([linuxOnly.state, linuxOnly.why, published(socket).length], ["deferred", "cloud_feature_unavailable", 0],
        "paired only with executors — one of them not even saying what it is — nothing is sent anywhere");
    await receiveEnvelope(client, socket, await sealedFromMac({ ch: "orch/mac-01", key_id: "mac-v2" },
        { tasks: [], machine: { platform: "macos" }, app: { build: "mac-build-1" }, cloud_status: MAC_STATUS }, macMaster));
    timers.fire();
    await until(() => published(socket).length === 1, "the batch on the wire");
    const sealed = published(socket)[0].envelope;
    assert.deepEqual([sealed.ch, sealed.key_id], ["ctl/mac-01", "mac-v2"], "to the Mac, under its pairing's key id");
    const command = JSON.parse(new TextDecoder().decode(await openEnvelope(sealed, macMaster, senderKey)));
    assert.equal(command.type, "diagnostics.events");
    await assert.rejects(openEnvelope(sealed, masterKey, senderKey), "not readable with the account key");
});

await check("viewer events · pairing · an unknown_command from one target does not hold the rows from the next", async function () {
    const store = new Map([["mac-old", pairingFor("mac-old")], ["mac-01", pairingFor("mac-01")]]);
    const { options } = pairedClient(store);
    const storage = new FakeStorage();
    const log = viewerLog(storage);
    log.rememberTarget({ machine: "mac-old", sender: DEVICE, capable: true });
    const { client, socket } = await viewerFleet({ orch: false, storage, log, client: options });
    log.record("test.event", { kept: 1 });
    const first = client._deliverViewerEvents();
    const sent = await nextCommand(socket, "diagnostics.events");
    assert.equal(sent.ch, "ctl/mac-old", "a page that has opened nothing uses the Mac chosen before");
    await answer(client, socket, "mac-old", { read: "action:" + sent.request, status: 400,
        error: { code: "unknown_command", layer: "mac_preflight", message: "This Mac does not know that Cloud command." } });
    assert.deepEqual([(await first).state, log.snapshot().blocked.machine], ["blocked", "mac-old"]);
    await fromMac(client, socket, "orch/mac-01", { tasks: [], machine: { platform: "macos" },
        app: { build: "mac-build-1" }, cloud_status: MAC_STATUS });
    viewerClock += 61_000;
    const next = client._deliverViewerEvents();
    const resent = await nextCommand(socket, "diagnostics.events");
    assert.deepEqual([resent.ch, resent.request, JSON.stringify(resent.batch)], ["ctl/mac-01", sent.request, JSON.stringify(sent.batch)],
        "the Mac this page authenticated gets the same batch, not a block recorded against another machine");
    await answer(client, socket, "mac-01", { read: "action:" + resent.request, status: 200,
        body: { ok: true, batch_id: sent.batch.batch_id, rows: sent.batch.rows.length } });
    assert.deepEqual([(await next).state, log.snapshot().outbox], ["delivered", null]);
});

/* ---- ends ---------------------------------------------------------------------------------- */

if (failures.length) {
    console.log("web cloud failure checks: " + failures.length + " red, " + passed.length + " green");
    console.log("  " + failures.join("\n  "));
    process.exit(1);
}
console.log("web cloud failure checks passed: " + passed.length);
