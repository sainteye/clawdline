/* --------------------------------------------------------------------------
   Devices: the human-readable fleet behind machine-scoped Session routes

   The opaque id remains the command authority, but it is never the primary label. This page is
   deliberately read-only: revocation and removal change access for other people and belong to a
   separately confirmed flow, not to a card somebody can tap while trying to identify a host.
   -------------------------------------------------------------------------- */

import { T } from "../core/i18n.js";
import { machineShortID } from "../session/selection.js";

export function deviceViewModel(machine, copy) {
    copy = copy || T;
    machine = machine || {};
    var id = String(machine.id || "");
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
        canStart: machine.selectable === true
    });
}

export function bindDevicesPage(elements, environment) {
    environment = environment || {};
    var generation = 0;

    function node(id) { return elements[id] || null; }
    function text(id, value) { var el = node(id); if (el) el.textContent = value || ""; }
    function show(id, value) { var el = node(id); if (el) el.hidden = !value; }

    function labels() {
        text("devices-title", T.webDevices);
        text("devices-lede", T.webDevicesLede);
        text("devices-close", T.webClose);
    }

    function draw(answer) {
        var list = node("devices-rows");
        if (!list || !list.ownerDocument) return;
        while (list.firstChild) list.removeChild(list.firstChild);
        var machines = answer && Array.isArray(answer.machines) ? answer.machines : [];
        show("devices-empty", machines.length === 0);
        text("devices-empty", machines.length ? "" : T.webStartMachineNone);
        machines.map(function (machine) { return deviceViewModel(machine, T); }).forEach(function (row) {
            var card = list.ownerDocument.createElement("article");
            card.className = "device-card";
            card.dataset.connection = row.connectionState;
            card.dataset.pairing = row.pairingState;

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
            list.appendChild(card);
        });
    }

    function load() {
        var own = ++generation;
        text("devices-status", T.webLoading);
        return Promise.resolve().then(function () { return environment.machines(); }).then(function (answer) {
            if (own !== generation) return;
            text("devices-status", ""); draw(answer);
        }).catch(function () {
            if (own !== generation) return;
            text("devices-status", T.webStartMachineNone); draw({ machines: [] });
        });
    }

    return {
        enter: function () { labels(); load(); },
        leave: function () { generation += 1; },
        load: load
    };
}
