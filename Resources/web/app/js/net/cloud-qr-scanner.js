/* Camera frames stay inside this module. Only a decoded invitation reaches pairing. */
import QrScanner from "../vendor/qr-scanner.min.js";
import { decodePairingInvitation } from "./cloud-pairing.js";
import { pairingFragmentFromScan } from "./cloud-onboarding.js";

export function scanCloudPairingInvitation(video, options) {
    options = options || {};
    var Scanner = options.Scanner || QrScanner;
    var now = options.now || function () { return Date.now(); };
    return new Promise(function (resolve, reject) {
        var settled = false;
        var scanner = new Scanner(video, function (result) {
            var raw = typeof result === "string" ? result : result && result.data;
            try {
                var fragment = pairingFragmentFromScan(raw);
                var invitation = decodePairingInvitation(fragment, now());
                settled = true;
                scanner.stop();
                scanner.destroy();
                resolve(invitation);
            } catch (error) {
                if (typeof options.onInvalid === "function") options.onInvalid(error);
            }
        }, {
            preferredCamera: "environment",
            maxScansPerSecond: 10,
            returnDetailedScanResult: true,
            highlightScanRegion: false,
            highlightCodeOutline: false
        });
        scanner.start().catch(function (error) {
            if (settled) return;
            scanner.destroy();
            reject(error);
        });
    });
}
