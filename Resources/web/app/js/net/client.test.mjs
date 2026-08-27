import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { webcrypto } from "node:crypto";

if (!globalThis.crypto) {
    Object.defineProperty(globalThis, "crypto", { value: webcrypto, configurable: true });
}

const {
    LOCAL_MACHINE,
    assertClawdlineClient,
    parseSessionPath,
    sessionIdentity,
    sessionPath
} = await import("./client.js");
const {
    importDevicePrivateKey,
    importMasterSecret,
    importSenderPublicKey,
    openEnvelope,
    sealEnvelope,
    verifyEnvelope
} = await import("./cloud-crypto.js");

const vectors = JSON.parse(await readFile(
    new URL("../../../../../Tests/protocol-vectors.json", import.meta.url), "utf8"));
const masterKey = await importMasterSecret(vectors.master_secret);
const senderKey = await importSenderPublicKey(vectors.ed25519_public_key);
assert.equal(masterKey.extractable, false, "the imported master key is non-extractable");

for (const vector of vectors.envelopes) {
    const opened = await openEnvelope(vector.envelope, masterKey, senderKey);
    assert.equal(Buffer.from(opened).toString("base64"), vector.plaintext,
        vector.name + " opens to the golden plaintext");
}

const signedFields = ["v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct", "sig"];
for (const field of signedFields) {
    const changed = structuredClone(vectors.envelopes[1].envelope);
    if (field === "v") changed.v = 2;
    else if (field === "seq" || field === "ts") changed[field] += 1;
    else if (field === "class") changed.class = "dispatch";
    else if (field === "ch") changed.ch = "ctl/mac-02";
    else changed[field] = (changed[field][0] === "A" ? "B" : "A") + changed[field].slice(1);
    await assert.rejects(openEnvelope(changed, masterKey, senderKey),
        field + " mutation must not open");
}

const senderChanged = structuredClone(vectors.envelopes[1].envelope);
senderChanged.sender = "unknown-device";
assert.equal(await verifyEnvelope(senderChanged, function (sender) {
    return sender === vectors.envelopes[1].envelope.sender ? senderKey : null;
}), false, "changing sender selects no trusted key");

assert.deepEqual(sessionIdentity("ABC"), { machine: LOCAL_MACHINE, session: "ABC" });
assert.deepEqual(sessionIdentity({ machine: "desk", session: "%3" }),
    { machine: "desk", session: "%3" });
assert.deepEqual(parseSessionPath("/m/desk%20one/s/%253"),
    { machine: "desk one", session: "%3" });
assert.equal(sessionPath({ machine: "desk one", session: "%3" }), "/m/desk%20one/s/%253");

const seed = Buffer.from(vectors.ed25519_seed, "base64");
const pkcs8 = Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), seed]);
const signingKey = await importDevicePrivateKey(pkcs8);
const outbound = await sealEnvelope({
    ch: "ctl/mac-01", seq: 9, ts: 1787817600000, class: "ctl",
    key_id: "ms-1", sender: "device-vector-01"
}, JSON.stringify({ answer: "yes" }), masterKey, signingKey);
assert.equal(await verifyEnvelope(outbound, senderKey), true, "outbound ctl is Ed25519 signed");
assert.equal(new TextDecoder().decode(await openEnvelope(outbound, masterKey, senderKey)),
    JSON.stringify({ answer: "yes" }), "outbound ctl opens under the account key");

const localSurface = {
    events: function () {}, sessions: function () {}, transcript: function () {},
    send: function () {}, answer: function () {}, dispatch: function () {},
    schedules: function () {}
};
assert.equal(assertClawdlineClient(localSurface), localSurface,
    "the local surface satisfies the same structural seam");
for (const method of Object.keys(localSurface)) {
    const missing = { ...localSurface };
    delete missing[method];
    assert.throws(function () { assertClawdlineClient(missing); }, new RegExp(method));
}

globalThis.location = { search: "", protocol: "http:", hostname: "localhost", hash: "",
    pathname: "/", href: "http://localhost/" };
globalThis.history = { replaceState: function () {}, pushState: function () {} };
globalThis.window = {
    crypto: globalThis.crypto,
    matchMedia: function () { return { matches: false, addEventListener: function () {} }; },
    addEventListener: function () {},
    devicePixelRatio: 1,
    innerHeight: 800
};
const inertElement = function () { return {
    hidden: false, textContent: "", innerHTML: "", value: "", dataset: {},
    children: [], childNodes: [],
    style: { setProperty: function () {}, removeProperty: function () {} },
    classList: { add: function () {}, remove: function () {}, toggle: function () {} },
    addEventListener: function () {}, querySelector: function () { return inertElement(); },
    querySelectorAll: function () { return []; }, appendChild: function () {},
    setAttribute: function () {}, removeAttribute: function () {}, focus: function () {}
}; };
globalThis.document = {
    hidden: false, activeElement: null, body: inertElement(),
    documentElement: Object.assign(inertElement(), { lang: "en" }),
    getElementById: inertElement,
    createElement: inertElement,
    addEventListener: function () {}
};
globalThis.MutationObserver = class { observe() {} disconnect() {} };
globalThis.requestAnimationFrame = function (callback) { return setTimeout(callback, 0); };

const requests = [];
globalThis.fetch = async function (path, options) {
    requests.push({ path: path, options: options });
    return { ok: true, text: async function () { return "{}"; } };
};
const { Live, LocalClient } = await import("./live.js");
assert.equal(LocalClient, Live, "the compatibility Live export is the exact LocalClient");
assert.equal(assertClawdlineClient(LocalClient), LocalClient, "the real local client satisfies the seam");
await LocalClient.transcript("session one");
await LocalClient.transcript({ machine: LOCAL_MACHINE, session: "session one" });
assert.deepEqual(requests.map(function (request) { return request.path; }), [
    "/v1/sessions/session%20one/transcript?limit=200",
    "/v1/sessions/session%20one/transcript?limit=200"
], "string and (machine, session) local identities make byte-identical requests");

const { CloudClient } = await import("./cloud-client.js");
const cloud = new CloudClient({
    relayURL: "wss://relay.example", deviceToken: "token", devicePrivateKey: signingKey,
    masterKey: masterKey, senderKeys: { "device-vector-01": senderKey }
});
assert.equal(assertClawdlineClient(cloud), cloud, "CloudClient satisfies the same seam");
await assert.rejects(cloud.send({ machine: "mac-01", session: "session-01" }, "hi", []),
    function (error) { return error.code === "cloud_read_only"; },
    "cloud ctl is feature-gated off by default");
assert.throws(function () {
    return new CloudClient({
        relayURL: "wss://relay.example", deviceToken: "token", devicePrivateKey: signingKey,
        masterKey: masterKey, allowWrites: true
    });
}, /durable nextSequence/, "write mode cannot silently reset its replay sequence on reload");

class FakeWebSocket {
    static latest = null;
    constructor(url, protocols) {
        this.url = url;
        this.protocols = protocols;
        this.readyState = 1;
        this.sent = [];
        FakeWebSocket.latest = this;
    }
    send(text) { this.sent.push(JSON.parse(text)); }
    close() { this.readyState = 3; if (this.onclose) this.onclose(); }
    receive(frame) { this.onmessage({ data: JSON.stringify(frame) }); }
}

const liveEvents = [];
const connectedCloud = new CloudClient({
    relayURL: "https://relay.example", deviceToken: "jwt", devicePrivateKey: signingKey,
    masterKey: masterKey, senderKeys: { "device-vector-01": senderKey },
    WebSocket: FakeWebSocket
});
connectedCloud.events(function (event) { liveEvents.push(event); });
await connectedCloud.start();
const fakeSocket = FakeWebSocket.latest;
assert.deepEqual(fakeSocket.protocols, ["clawdline.v1", "clawdline.token.jwt"]);
fakeSocket.receive({ type: "challenge", v: 1, context: "clawdline-challenge-v1",
    account: "account-01", device: "device-vector-01", challenge: "AA==" });
await connectedCloud.messageChain;
assert.equal(fakeSocket.sent[0].type, "hello", "CloudClient signs the relay challenge");
fakeSocket.receive({ type: "ready", v: 1, role: "viewer", account: "account-01",
    device: "device-vector-01" });
await connectedCloud.messageChain;
const snapshotEnvelope = await sealEnvelope({
    ch: "s/mac-01/session-01", seq: 10, ts: 1787817600000, class: "stream",
    key_id: "ms-1", sender: "device-vector-01"
}, JSON.stringify({ id: "session-01", label: "cloud session" }), masterKey, signingKey);
fakeSocket.receive({ type: "envelope", realign: true, envelope: snapshotEnvelope });
await connectedCloud.messageChain;
assert.deepEqual((await connectedCloud.sessions()).sessions[0].identity,
    { machine: "mac-01", session: "session-01" },
    "a verified cloud snapshot is stored under (machine, session)");
assert.equal(liveEvents.some(function (event) { return event.type === "sessions"; }), true);
connectedCloud.stop();

console.log("web cloud client tests passed: golden vectors, mutations, identity, local seam");
process.exit(0);
