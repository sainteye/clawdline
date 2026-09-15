/* --------------------------------------------------------------------------
   Devices: the human-readable fleet behind machine-scoped Session routes

   The opaque id remains the command authority, but it is never the primary label. This page is
   deliberately read-only: revocation and removal change access for other people and belong to a
   separately confirmed flow, not to a card somebody can tap while trying to identify a host.

   The one preference it holds is which Mac transcribes this browser's dictation. The transport
   decides that (`CloudClient.voiceHost`) and this page only draws the answer and passes a press
   back, so the card marked "Voice input" is always the machine the recording goes to.
   -------------------------------------------------------------------------- */

import { T } from "../core/i18n.js";
import { machineShortID } from "../session/selection.js";

/** "host", "candidate" or "": this card's part in the transport's voice-host answer. */
export function deviceVoiceRole(id, host) {
    if (!id || !host || typeof host !== "object") return "";
    if (host.machine === id) return "host";
    return Array.isArray(host.candidates) && host.candidates.indexOf(id) >= 0 ? "candidate" : "";
}

export function deviceViewModel(machine, copy, voiceHost) {
    copy = copy || T;
    machine = machine || {};
    var id = String(machine.id || "");
    var voice = deviceVoiceRole(id, voiceHost);
    var suppliedLabel = String(machine.label || machine.name || "");
    var label = suppliedLabel && suppliedLabel !== id ? suppliedLabel
        : (copy.webStartMachine || "Machine") + " · " + machineShortID(id);
    var pairing = machine.pairing === "paired" || machine.pairing === "local"
        ? copy.webDevicePaired
        : machine.pairing === "not_paired" ? copy.webDeviceNotPaired
            : copy.webDevicePairingUnknown;
    return Object.freeze({
        id: id,
        label: label,
        identifier: machineShortID(id),
        connection: machine.freshness === "current" ? copy.webDeviceOnline
            : machine.freshness === "stale" ? copy.webStartMachineStale : copy.webCoordUnknown,
        connectionState: machine.freshness || "unknown",
        pairing: pairing,
        pairingState: machine.pairing || "unknown",
        pairHelp: machine.pairing === "not_paired" ? copy.webDevicePairHelp : "",
        sessions: Number.isSafeInteger(machine.sessions)
            ? machine.sessions + " " + copy.webSessions : "",
        canStart: machine.selectable === true,
        voice: voice,
        voiceFact: voice === "host" ? copy.webDeviceVoiceHost : ""
    });
}

export function bindDevicesPage(elements, environment) {
    environment = environment || {};
    var generation = 0;
    var stopEvents = null;
    var settleTimer = null;
    var timers = {
        setTimeout: environment.setTimeout || globalThis.setTimeout.bind(globalThis),
        clearTimeout: environment.clearTimeout || globalThis.clearTimeout.bind(globalThis)
    };

    function node(id) { return elements[id] || null; }
    function text(id, value) { var el = node(id); if (el) el.textContent = value || ""; }
    function show(id, value) { var el = node(id); if (el) el.hidden = !value; }

    function labels() {
        text("devices-title", T.webDevices);
        text("devices-lede", T.webDevicesLede);
        text("devices-close", T.webClose);
    }

    /** The transport's voice host, or null where it has none. A refusal that still names candidates
     * (several Macs, none chosen) is exactly when the page must offer them. */
    function voiceHost() {
        if (typeof environment.voiceHost !== "function") return Promise.resolve(null);
        return Promise.resolve().then(function () { return environment.voiceHost(); }).then(function (host) {
            return host && typeof host === "object" ? host : null;
        }, function (error) {
            return error && Array.isArray(error.candidates) && error.candidates.length
                ? { machine: null, chosen: false, candidates: error.candidates } : null;
        });
    }

    function chooseVoiceHost(machine) {
        return Promise.resolve().then(function () { return environment.setVoiceHost(machine); })
            .then(function () { return load(); }, function () { return load(); });
    }

    function draw(answer, host) {
        var list = node("devices-rows");
        if (!list || !list.ownerDocument) return;
        while (list.firstChild) list.removeChild(list.firstChild);
        var machines = answer && Array.isArray(answer.machines) ? answer.machines : [];
        show("devices-empty", machines.length === 0);
        text("devices-empty", machines.length ? "" : T.webStartMachineNone);
        machines.map(function (machine) { return deviceViewModel(machine, T, host); }).forEach(function (row) {
            var card = list.ownerDocument.createElement("article");
            card.className = "device-card";
            card.dataset.connection = row.connectionState;
            card.dataset.pairing = row.pairingState;
            card.dataset.voice = row.voice;

            var heading = list.ownerDocument.createElement("div");
            heading.className = "device-card-heading";
            var title = list.ownerDocument.createElement("h2");
            title.textContent = row.label;
            var identifier = list.ownerDocument.createElement("code");
            identifier.textContent = row.identifier;
            identifier.title = row.id;
            heading.appendChild(title); heading.appendChild(identifier); card.appendChild(heading);

            var facts = list.ownerDocument.createElement("div");
            facts.className = "device-facts";
            [row.connection, row.pairing, row.sessions].filter(Boolean).forEach(function (value) {
                var fact = list.ownerDocument.createElement("span");
                fact.textContent = value; facts.appendChild(fact);
            });
            if (row.voiceFact) {
                var voiceFact = list.ownerDocument.createElement("span");
                voiceFact.className = "device-voice-host";
                voiceFact.textContent = row.voiceFact; facts.appendChild(voiceFact);
            }
            card.appendChild(facts);
            if (row.pairHelp) {
                var help = list.ownerDocument.createElement("p");
                help.className = "device-help"; help.textContent = row.pairHelp; card.appendChild(help);
            }
            if (row.canStart && typeof environment.start === "function") {
                var start = list.ownerDocument.createElement("button");
                start.type = "button"; start.className = "device-start";
                start.textContent = T.webDeviceNewSession;
                start.onclick = function () { environment.start(row.id); };
                card.appendChild(start);
            }
            if (row.voice === "candidate" && typeof environment.setVoiceHost === "function") {
                var use = list.ownerDocument.createElement("button");
                use.type = "button"; use.className = "device-voice";
                use.textContent = T.webDeviceUseForVoice;
                use.onclick = function () { return chooseVoiceHost(row.id); };
                card.appendChild(use);
            }
            list.appendChild(card);
        });
    }

    function load() {
        var own = ++generation;
        if (settleTimer !== null) timers.clearTimeout(settleTimer);
        settleTimer = null;
        text("devices-status", T.webLoading);
        return Promise.resolve().then(function () {
            return Promise.all([environment.machines(), voiceHost()]);
        }).then(function (answers) {
            var answer = answers[0];
            if (own !== generation) return;
            text("devices-status", answer && answer.syncing ? T.webLoading : ""); draw(answer, answers[1]);
            if (answer && answer.syncing && Number.isFinite(answer.retryAfterMs)
                && answer.retryAfterMs > 0) {
                settleTimer = timers.setTimeout(function () {
                    settleTimer = null; load();
                }, Math.max(1, Math.min(answer.retryAfterMs, 60 * 1000)));
                if (settleTimer && typeof settleTimer.unref === "function") settleTimer.unref();
            }
        }).catch(function () {
            if (own !== generation) return;
            text("devices-status", T.webStartMachineNone); draw({ machines: [] });
        });
    }

    return {
        enter: function () {
            labels();
            if (!stopEvents && typeof environment.events === "function") {
                stopEvents = environment.events(function (event) {
                    var code = event && event.error && event.error.code;
                    if (event && (event.type === "orchestrator" || event.type === "sessions"
                        || event.type === "error" && (code === "machine_not_paired"
                            || code === "machine_key_incomplete"))) load();
                });
            }
            load();
        },
        leave: function () {
            generation += 1;
            if (settleTimer !== null) timers.clearTimeout(settleTimer);
            settleTimer = null;
            if (typeof stopEvents === "function") stopEvents();
            stopEvents = null;
        },
        load: load
    };
}
