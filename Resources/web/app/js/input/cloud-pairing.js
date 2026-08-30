/* --------------------------------------------------------------------------
   The hosted console's pairing screen

   The local door asks the Mac for a six-digit code that appears on the Mac and
   nowhere else. The cloud door is the same idea with the direction reversed,
   because the thing that has to travel is the account's content key and only the
   Mac has it: this browser asks the control plane for a one-time claim handle,
   shows the resulting offer, and the person carries that offer to the Mac. The
   control plane relays one ciphertext it cannot read, and neither device trusts
   anything the cloud says about the other's key.

   The words here are borrowed from the local door's entries in the string table,
   because that table is served by `RemoteServer` and this change does not own that
   file. Three lines in `index.html` are therefore still English in every language;
   see `docs/cloud.md`.
   -------------------------------------------------------------------------- */

import { T } from "../core/i18n.js";
import { pairViewer } from "../net/cloud-boot.js";
import { drawIcon } from "../core/pixels.js";

function byId(id) { return document.getElementById(id); }

function say(text, calm) {
    var line = byId("cloud-door-say");
    if (!line) return;
    line.textContent = text || "";
    line.classList.toggle("calm", calm === true);
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
    var fingerprint = byId("cloud-door-fingerprint");
    var mark = byId("cloud-door-mark");
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

    function showOffer(pending) {
        if (offerField) {
            offerField.value = pending.fragment;
            offerField.setAttribute("aria-label", "Pairing code");
        }
        if (fingerprint) fingerprint.textContent = pending.fingerprint || "";
        say(T.webDoorCodeLede, true);
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
        return pairViewer(session, {
            onOffer: showOffer,
            sleep: sleep,
            intervalMs: options.intervalMs || 2000
        });
    }

    return new Promise(function (resolve) {
        function round() {
            attempt().then(function (paired) {
                say(T.webDoorPaired, true);
                door.hidden = true;
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
        round();
    });
}
