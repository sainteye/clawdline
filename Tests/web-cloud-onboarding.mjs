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

console.log("web cloud onboarding tests passed");
