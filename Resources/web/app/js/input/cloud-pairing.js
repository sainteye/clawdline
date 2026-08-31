/* --------------------------------------------------------------------------
   The hosted console's pairing screen

   The Mac displays a one-time QR. This browser keeps its secret in the URL fragment,
   proves the same Cloud account through GitHub, then returns its ordinary viewer offer
   encrypted by that QR secret. The control plane relays ciphertext it cannot read; the
   Mac returns the account key through the existing X25519 handover, and neither device
   trusts anything the cloud says about the other's key.

   The words here are borrowed from the local door's entries in the string table,
   because that table is served by `RemoteServer` and this change does not own that
   file. Three lines in `index.html` are therefore still English in every language;
   see `docs/cloud.md`.
   -------------------------------------------------------------------------- */

import { T } from "../core/i18n.js";
import { pairViewer, pairViewerFromInvitation } from "../net/cloud-boot.js";
import { decodePairingInvitation } from "../net/cloud-pairing.js";
import { scanCloudPairingInvitation } from "../net/cloud-qr-scanner.js";
import { drawIcon } from "../core/pixels.js";

const INVITATION_STORAGE_KEY = "clawdline.pairing.invitation.v1";

function byId(id) { return document.getElementById(id); }

function say(text, calm) {
    var line = byId("cloud-door-say");
    if (!line) return;
    line.textContent = text || "";
    line.classList.toggle("calm", calm === true);
}

/**
 * Capture the QR secret before OAuth navigation. It stays in sessionStorage and in the URL
 * fragment only; neither form is sent to app.clawdline.com or api.clawdline.com.
 */
export function captureCloudPairingInvitation(scope, nowMilliseconds) {
    var raw = "";
    var hash = scope.location && typeof scope.location.hash === "string"
        ? scope.location.hash : "";
    if (hash.indexOf("#pair=") === 0) {
        raw = hash.slice(6);
        scope.sessionStorage.setItem(INVITATION_STORAGE_KEY, raw);
        if (scope.history && typeof scope.history.replaceState === "function") {
            scope.history.replaceState(null, "", scope.location.pathname + scope.location.search);
        }
    } else {
        raw = scope.sessionStorage.getItem(INVITATION_STORAGE_KEY) || "";
    }
    if (!raw) return null;
    try {
        return decodePairingInvitation(raw, nowMilliseconds);
    } catch (error) {
        scope.sessionStorage.removeItem(INVITATION_STORAGE_KEY);
        throw error;
    }
}

export function clearCloudPairingInvitation(storage) {
    storage.removeItem(INVITATION_STORAGE_KEY);
}

/**
 * Safari and an installed iOS web app do not share IndexedDB. Stop before Cloud creates a
 * disposable Safari viewer; the PWA will own login, its signing key, and the account key.
 */
export function showCloudInstallGate() {
    var door = byId("cloud-door");
    if (!door) return;
    var mark = byId("cloud-door-mark");
    if (mark && typeof drawIcon === "function") { try { drawIcon(mark); } catch (e) { /* cosmetic */ } }
    door.hidden = false;
    byId("cloud-door-lede").textContent = "Install Clawdline first";
    byId("cloud-door-guide").textContent = "Safari and a Home Screen app keep separate secure storage. "
        + "Install first so your end-to-end encryption keys are created in the app you will keep using.";
    byId("cloud-door-install-steps").hidden = false;
    ["cloud-door-camera-frame", "cloud-door-scan", "cloud-door-offer-label",
        "cloud-door-offer", "cloud-door-fingerprint-line", "cloud-door-confirm",
        "cloud-door-restart"].forEach(function (id) {
        var element = byId(id); if (element) element.hidden = true;
    });
    say("Nothing has been registered yet. Continue from the Home Screen app.", true);
}

/**
 * Run one pairing, start to finish, with the screen following it.
 *
 * Resolves when this browser holds the account key; rejects only when the person has to be
 * shown something and start again. Restarting is a new offer, never a retry of the old one:
 * the claim nonce is one-time and the Mac may already have answered it.
 */
export function showCloudPairing(session, options) {
    options = options || {};
    var door = byId("cloud-door");
    var confirm = byId("cloud-door-confirm");
    var restart = byId("cloud-door-restart");
    var offerField = byId("cloud-door-offer");
    var offerLabel = byId("cloud-door-offer-label");
    var fingerprint = byId("cloud-door-fingerprint");
    var fingerprintLine = byId("cloud-door-fingerprint-line");
    var fingerprintPrefix = byId("cloud-door-fingerprint-prefix");
    var fingerprintSuffix = byId("cloud-door-fingerprint-suffix");
    var lede = byId("cloud-door-lede");
    var guide = byId("cloud-door-guide");
    var mark = byId("cloud-door-mark");
    var installSteps = byId("cloud-door-install-steps");
    var scanButton = byId("cloud-door-scan");
    var cameraFrame = byId("cloud-door-camera-frame");
    var camera = byId("cloud-door-camera");
    if (!door) return Promise.reject(new Error("the cloud door is not in this page"));
    if (mark && typeof drawIcon === "function") { try { drawIcon(mark); } catch (e) { /* cosmetic */ } }
    door.hidden = false;

    var poked = null;
    // The button does not claim by itself — the loop is already claiming. It shortens the wait
    // to now, which is what somebody who has just finished at the Mac actually wants.
    function pokeNow() {
        say(T.webDoorChecking, true);
        if (poked) poked();
    }

    function sleep(ms) {
        return new Promise(function (resolve) {
            var timer = setTimeout(finish, ms);
            poked = finish;
            function finish() {
                clearTimeout(timer);
                poked = null;
                resolve();
            }
        });
    }

    var invitation = options.invitation || null;

    function showOffer(pending) {
        if (offerField) {
            offerField.value = pending.fragment;
            offerField.setAttribute("aria-label", "Pairing code");
        }
        if (fingerprint) fingerprint.textContent = pending.fingerprint || "";
        if (invitation) {
            say("QR confirmed. Waiting for the Mac to finish the encrypted key handover…", true);
        } else {
            say(T.webDoorCodeLede, true);
        }
    }

    // Bound once for the life of the page. `showCloudPairing` is called again whenever the
    // account key goes away, and a second listener on the same button would poke the loop twice
    // for one press.
    if (door.dataset.bound !== "1") {
        door.dataset.bound = "1";
        if (confirm) confirm.addEventListener("click", pokeNow);
        if (offerField) {
            offerField.addEventListener("focus", function () { offerField.select(); });
        }
    }

    function attempt() {
        say(T.webDoorAsking, true);
        var runOptions = {
            onOffer: showOffer,
            sleep: sleep,
            intervalMs: options.intervalMs || 2000
        };
        return invitation
            ? pairViewerFromInvitation(session, invitation, runOptions)
            : pairViewer(session, runOptions);
    }

    function showInvitationCopy() {
        if (lede) lede.textContent = "Finish secure pairing";
        if (guide) guide.textContent = "GitHub confirms this is your Clawdline account. "
            + "The QR confirms this is the Mac in front of you. Both checks are required "
            + "because Cloud only relays end-to-end encrypted data.";
        if (offerLabel) offerLabel.hidden = true;
        if (offerField) offerField.hidden = true;
        if (confirm) confirm.hidden = true;
        if (fingerprintLine) fingerprintLine.hidden = false;
        if (fingerprintPrefix) fingerprintPrefix.textContent = "This phone's fingerprint is ";
        if (fingerprintSuffix) fingerprintSuffix.textContent = ". The Mac pins it when pairing completes.";
        if (scanButton) scanButton.hidden = true;
        if (cameraFrame) cameraFrame.hidden = true;
    }

    if (installSteps) installSteps.hidden = true;
    if (invitation) {
        showInvitationCopy();
    } else if (options.scan === true) {
        if (lede) lede.textContent = "Scan the QR on your Mac";
        if (guide) guide.textContent = "GitHub confirms your Cloud account. The QR confirms the physical Mac in front of you. "
            + "Clawdline requires both because terminal data is end-to-end encrypted and Cloud never receives the key.";
        if (offerLabel) offerLabel.hidden = true;
        if (offerField) offerField.hidden = true;
        if (confirm) confirm.hidden = true;
        if (fingerprintLine) fingerprintLine.hidden = true;
        if (scanButton) scanButton.hidden = false;
        if (cameraFrame) cameraFrame.hidden = true;
        say("The QR is decoded only on this phone.", true);
    } else {
        if (offerLabel) offerLabel.hidden = false;
        if (offerField) offerField.hidden = false;
        if (confirm) confirm.hidden = false;
        if (fingerprintPrefix) fingerprintPrefix.textContent = "This browser's fingerprint is ";
        if (fingerprintSuffix) fingerprintSuffix.textContent = " — the Mac shows the same one.";
    }

    return new Promise(function (resolve) {
        function round() {
            attempt().then(function (paired) {
                say(T.webDoorPaired, true);
                door.hidden = true;
                if (typeof options.onPaired === "function") options.onPaired();
                resolve(paired);
            }, function (error) {
                var code = error && error.code;
                // Every one of these means "ask for a fresh offer", and the three the person can
                // do something about are told apart because the answer differs: wait for the
                // Mac, start again, or the offer was never yours.
                say(code === "offer_expired" || code === "pairing_expired"
                    ? T.webDoorExpired : T.webDoorAskFailed, false);
                if (restart) {
                    restart.hidden = false;
                    restart.onclick = function () {
                        restart.onclick = null;
                        round();
                    };
                }
            });
        }
        if (restart) restart.hidden = true;
        if (options.scan === true && !invitation) {
            scanButton.onclick = function () {
                scanButton.disabled = true;
                cameraFrame.hidden = false;
                say("Opening the camera…", true);
                scanCloudPairingInvitation(camera, {
                    onInvalid: function (error) {
                        say(error && error.code === "wrong_pairing_origin"
                            ? "That QR belongs to another site. Point the camera at Clawdline on your Mac."
                            : "Keep the Clawdline QR inside the frame.", false);
                    }
                }).then(function (scanned) {
                    invitation = scanned;
                    scanButton.onclick = null;
                    scanButton.disabled = false;
                    showInvitationCopy();
                    round();
                }, function () {
                    cameraFrame.hidden = true;
                    scanButton.disabled = false;
                    say("Camera access is required to scan the Mac's one-time QR. Allow it and try again.", false);
                });
            };
        } else {
            round();
        }
    });
}
