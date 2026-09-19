/* --------------------------------------------------------------------------
   The install boundary for the hosted console

   A Home Screen app on iOS has storage isolated from Safari. Clawdline's device
   signing key and account content key are deliberately non-extractable CryptoKeys,
   so pairing in Safari and installing afterwards creates an app that cannot inherit
   either key. The only honest order is install -> open the PWA -> sign in -> scan.
   -------------------------------------------------------------------------- */

const CLOUD_APP_ORIGIN = "https://app.clawdline.com";

export function isAppleTouchDevice(scope) {
    scope = scope || {};
    var navigator = scope.navigator || {};
    var platform = navigator.platform || "";
    return /iP(hone|ad|od)/.test(platform)
        || (/Mac/.test(platform) && Number(navigator.maxTouchPoints || 0) > 1);
}

export function isStandaloneWebApp(scope) {
    scope = scope || {};
    var navigator = scope.navigator || {};
    if (navigator.standalone === true) return true;
    return typeof scope.matchMedia === "function"
        && scope.matchMedia("(display-mode: standalone)").matches === true;
}

/** install | pwa | browser — decided before any Cloud session/device is created. */
export function cloudOnboardingMode(scope) {
    if (!isAppleTouchDevice(scope)) return "browser";
    return isStandaloneWebApp(scope) ? "pwa" : "install";
}

/** Safe, coarse labels for recovery; never sends a user-agent string to the control plane. */
export function cloudViewerDeviceMetadata(scope) {
    scope = scope || {};
    var navigator = scope.navigator || {};
    var platform = String(navigator.platform || "");
    var userAgent = String(navigator.userAgent || "");
    if (/iPhone|iPod/i.test(platform)) return { kind: "ios", name: "iPhone" };
    if (/iPad/i.test(platform) || (/Mac/i.test(platform) && Number(navigator.maxTouchPoints || 0) > 1)) {
        return { kind: "ios", name: "iPad" };
    }
    if (/Android/i.test(platform + " " + userAgent)) return { kind: "android", name: "Android device" };
    return { kind: "browser", name: "Web browser" };
}

function pairingError(code, message) {
    var error = new Error(message);
    error.code = code;
    return error;
}

/** Extract the invitation locally. The camera frame and full URL never leave this device. */
export function pairingFragmentFromScan(value) {
    var raw = String(value || "").trim();
    if (raw.indexOf("#pair=") === 0) {
        if (raw.length === 6) throw pairingError("not_pairing_qr", "the QR has no pairing secret");
        return raw.slice(6);
    }
    var url;
    try { url = new URL(raw); } catch (error) {
        throw pairingError("not_pairing_qr", "this is not a Clawdline pairing QR");
    }
    if (url.origin !== CLOUD_APP_ORIGIN) {
        throw pairingError("wrong_pairing_origin", "the QR belongs to another site");
    }
    if (url.hash.indexOf("#pair=") !== 0 || url.hash.length === 6) {
        throw pairingError("not_pairing_qr", "this is not a Clawdline pairing QR");
    }
    return url.hash.slice(6);
}
