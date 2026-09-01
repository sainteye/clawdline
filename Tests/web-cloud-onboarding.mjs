/** The hosted console installs before it creates any E2E identity on iOS. */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const pairing = await import("../Resources/web/app/js/net/cloud-onboarding.js");

function scope(overrides) {
    return Object.assign({
        navigator: { platform: "iPhone", maxTouchPoints: 5, standalone: false },
        matchMedia: function () { return { matches: false }; }
    }, overrides || {});
}

assert.equal(pairing.cloudOnboardingMode(scope()), "install",
    "an iPhone Safari tab installs the PWA before creating a disposable viewer identity");
assert.equal(pairing.cloudOnboardingMode(scope({
    navigator: { platform: "iPhone", maxTouchPoints: 5, standalone: true }
})), "pwa", "the installed iPhone app continues to sign-in and pairing");
assert.equal(pairing.cloudOnboardingMode(scope({
    navigator: { platform: "MacIntel", maxTouchPoints: 0, standalone: false }
})), "browser", "desktop browsers keep their existing Cloud console flow");
assert.equal(pairing.cloudOnboardingMode(scope({
    navigator: { platform: "MacIntel", maxTouchPoints: 5, standalone: false }
})), "install", "iPadOS desktop user-agent is still treated as an install-first device");
assert.deepEqual(pairing.cloudViewerDeviceMetadata(scope({
    navigator: { platform: "iPhone", maxTouchPoints: 5, standalone: true }
})), { kind: "ios", name: "iPhone" },
"an installed iPhone registers recognizable viewer metadata instead of generic Browser");

const protocol = await import("../Resources/web/app/js/net/cloud-pairing.js");
const encoded = protocol.base64URL(new TextEncoder().encode(protocol.canonicalJSON({
    v: 1, type: "pairing_invitation", invitation_id: "x",
    secret: Buffer.alloc(32).toString("base64"), expires_at: 1900000
})));
assert.equal(pairing.pairingFragmentFromScan("https://app.clawdline.com/#pair=" + encoded), encoded,
    "a camera result extracts the secret fragment without navigating away from the PWA");
assert.equal(pairing.pairingFragmentFromScan("#pair=" + encoded), encoded,
    "the scanner also accepts a fragment-only test payload");
assert.throws(function () { pairing.pairingFragmentFromScan("https://example.com/#pair=" + encoded); },
    function (error) { return error.code === "wrong_pairing_origin"; },
    "a QR from another origin cannot inject a pairing secret");
assert.throws(function () { pairing.pairingFragmentFromScan("https://app.clawdline.com/docs"); },
    function (error) { return error.code === "not_pairing_qr"; },
    "an ordinary QR remains an ordinary QR");

const scannerModule = await import("../Resources/web/app/js/net/cloud-qr-scanner.js");
let stopped = false;
let destroyed = false;
class FakeScanner {
    constructor(video, onDecode, options) {
        assert.equal(video, "camera");
        assert.equal(options.preferredCamera, "environment");
        this.onDecode = onDecode;
    }
    start() {
        queueMicrotask(() => this.onDecode({ data: "https://app.clawdline.com/#pair=" + encoded }));
        return Promise.resolve();
    }
    stop() { stopped = true; }
    destroy() { destroyed = true; }
}
const scanned = await scannerModule.scanCloudPairingInvitation("camera", {
    Scanner: FakeScanner, now: function () { return 1800000; }
});
assert.equal(scanned.invitation_id, "x", "the locally decoded frame becomes the typed invitation");
assert.ok(stopped && destroyed, "the camera is stopped immediately after the one valid QR");

const main = readFileSync("Resources/web/app/js/main.js", "utf8");
assert.ok(main.indexOf('cloudOnboarding === "install"') < main.indexOf("new CloudViewerSession"),
    "the install gate is selected before a Cloud session can register a Safari viewer");
assert.ok(main.indexOf("clearCloudPairingInvitation(window.sessionStorage)")
    < main.indexOf("new CloudViewerSession"),
    "a QR accidentally opened in Safari is scrubbed rather than stranded there");
assert.ok(main.includes('scan: cloudOnboarding === "pwa" && !cloudInvitation'),
    "the installed app owns the camera pairing path when it has no invitation yet");
assert.ok(main.includes('params.get("cloud-onboarding") === "install"'),
    "the bundled mock can hold the install page still for responsive visual checks");
assert.ok(main.includes("showCloudSignIn"),
    "signed-out Cloud boot is handed to a Clawdline-owned sign-in gate");
assert.ok(!main.includes("location.replace(update.url)"),
    "Cloud boot never navigates to OAuth merely because the API said sign_in");

function fakeClassList() { return { toggle: function () {} }; }
function fakeElement() {
    return {
        hidden: false, disabled: false, textContent: "", onclick: null, children: [],
        dataset: {}, classList: fakeClassList(),
        appendChild: function (child) { this.children.push(child); },
        replaceChildren: function () { this.children = Array.from(arguments); },
        addEventListener: function (name, handler) { this["on" + name] = handler; },
        setAttribute: function () {}, select: function () {}
    };
}

const elements = {};
[
    "cloud-door", "cloud-door-mark", "cloud-door-lede", "cloud-door-guide",
    "cloud-door-install-steps", "cloud-door-camera-frame", "cloud-door-scan",
    "cloud-door-offer-label", "cloud-door-offer", "cloud-door-fingerprint-line",
    "cloud-door-confirm", "cloud-door-restart", "cloud-door-say", "cloud-door-devices"
].forEach(function (id) { elements[id] = fakeElement(); });
const previousDocument = globalThis.document;
const previousLocation = globalThis.location;
const previousWindow = globalThis.window;
const previousSetInterval = globalThis.setInterval;
globalThis.document = {
    getElementById: function (id) { return elements[id] || null; },
    createElement: function () { return fakeElement(); }
};
globalThis.location = { search: "", protocol: "https:", hostname: "app.clawdline.com" };
globalThis.window = {
    matchMedia: function () { return { matches: false }; },
    visualViewport: null,
    devicePixelRatio: 1
};
// cloud-pairing imports the production pixel renderer, whose animation ticker is intentionally
// process-long in a browser. Keep that unrelated ticker from holding this Node contract open.
globalThis.setInterval = function () { return 0; };
const cloudDoor = await import("../Resources/web/app/js/input/cloud-pairing.js");

let navigated = null;
cloudDoor.showCloudSignIn("https://api.clawdline.com/v1/auth/oauth/start", {
    navigate: function (url) { navigated = url; }
});
assert.equal(navigated, null, "rendering the sign-in explanation does not navigate");
assert.equal(elements["cloud-door-confirm"].textContent, "Continue with GitHub");
elements["cloud-door-confirm"].onclick();
assert.equal(navigated, "https://api.clawdline.com/v1/auth/oauth/start",
    "OAuth starts only after the explicit button press");

let revokedDevice = null;
let recovered = false;
await cloudDoor.showCloudDeviceRecovery({
    recoveryDevices: function () {
        return Promise.resolve({
            tier: "free", limit: 2, active: 2,
            devices: [{
                id: "dev-private", kind: "ios", name: "Older iPhone",
                created_at: "2026-08-01T00:00:00.000Z",
                last_seen_at: "2026-08-31T12:00:00.000Z",
                public_key: "must-not-render", caps: ["send_prompt"]
            }]
        });
    },
    revokeRecoveryDevice: function (id) { revokedDevice = id; return Promise.resolve(); }
}, { tier: "free", limit: 2 }, {
    onRecovered: function () { recovered = true; }
});
const row = elements["cloud-door-devices"].children[0];
assert.ok(row.textContent.includes("Older iPhone") && row.textContent.includes("iOS"),
    "recovery shows recognizable device context");
assert.ok(row.textContent.includes("added"),
    "recovery includes creation context when last-active timestamps alone are ambiguous");
assert.ok(!row.textContent.includes("dev-private") && !row.textContent.includes("must-not-render")
    && !row.textContent.includes("send_prompt"),
    "recovery never renders internal ids, public keys, or capabilities");
row.children[0].onclick();
await new Promise(function (resolve) { setTimeout(resolve, 0); });
assert.equal(revokedDevice, "dev-private", "only the chosen row's opaque id is submitted");
assert.equal(recovered, true, "successful revocation resumes session creation");

let expiredNavigation = null;
const expired = new Error("Sign in again to manage viewer devices");
expired.code = "no_login_ticket";
await assert.rejects(cloudDoor.showCloudDeviceRecovery({
    signInURL: function () { return "https://api.clawdline.com/v1/auth/oauth/start"; },
    recoveryDevices: function () { return Promise.reject(expired); }
}, { tier: "free", limit: 2 }, {
    navigate: function (url) { expiredNavigation = url; }
}), function (error) { return error.code === "no_login_ticket"; });
assert.equal(elements["cloud-door-confirm"].textContent, "Continue with GitHub",
    "an expired recovery ticket becomes an explicit fresh sign-in action");
elements["cloud-door-confirm"].onclick();
assert.equal(expiredNavigation, "https://api.clawdline.com/v1/auth/oauth/start");

cloudDoor.showCloudBootError({
    state: "retrying", error: new Error("network unavailable"), afterMs: 250
});
assert.equal(elements["cloud-door-lede"].textContent, "Cloud is temporarily unavailable");
assert.ok(elements["cloud-door-guide"].textContent.includes("retry automatically"),
    "retryable transport failures are visibly distinct from terminal conflicts");
assert.ok(elements["cloud-door-say"].textContent.includes("network unavailable")
    && elements["cloud-door-say"].textContent.includes("Retrying in about 1s"),
    "the visible retry state includes the transport error and backoff");
assert.equal(elements["cloud-door-restart"].hidden, true,
    "automatic transport retry does not masquerade as a terminal manual action");

globalThis.document = previousDocument;
globalThis.location = previousLocation;
globalThis.window = previousWindow;
globalThis.setInterval = previousSetInterval;

console.log("web cloud onboarding tests passed");
