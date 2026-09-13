import { T, fill } from "../core/i18n.js";
import { failureSentence } from "../core/failure-text.js";
import { refText } from "../net/cloud-failure.js";

/* ==========================================================================
   The Cloud status sheet, as data
   `docs/cloud-error-transparency.md` §4.3. What is on it comes from two places and nowhere else:
   this browser's own trail of recent commands (`net/cloud-failure.js` `CloudTrail`) and each
   Mac's `cloud.status` snapshot (§4.1, §11.3), joined on `ref`. The DOM half is
   `input/cloud-status.js`; this half has no document so the node suites can read what it says.

   Steps, layers and codes are shown as their own snake_case words. They are the vocabulary a
   person reads out to whoever is debugging it, and the same words are in `cloud-status.json`.
   ========================================================================== */

function clock(ms) {
    if (!Number.isFinite(ms)) return "";
    try { return new Date(ms).toLocaleTimeString(); } catch (e) { return String(ms); }
}

function stepsLine(steps, order) {
    return order.filter(function (name) { return steps && steps[name]; }).map(function (name) {
        return name + " " + clock(steps[name]);
    }).join(" · ");
}

const BROWSER_STEPS = ["sealed", "relayed", "machine_offline", "observed", "acknowledged"];

function sameRef(a, b) {
    return !!a && !!b && a.sender === b.sender && a.seq === b.seq;
}

/** The Mac's own row for one of this browser's commands, as the steps only the Mac can witness. */
function macSteps(row) {
    if (!row) return "";
    var steps = {
        accepted: row.accepted_at_ms, executed: row.executed_at_ms, delivered: row.delivered_at_ms
    };
    var line = stepsLine(steps, ["accepted", "executed", "delivered"]);
    if (typeof row.undeliverable === "string" && row.undeliverable) {
        line = (line ? line + " · " : "") + "undeliverable " + row.undeliverable;
    }
    return line;
}

/**
 * Everything the sheet draws.
 *
 * `input` is `{ trail, macs, reading, focus }`: `trail` a `CloudTrail.snapshot()`, `macs` the rows
 * `CloudClient.cloudStatus()` answered (or null before it has), `focus` the `ref` the sheet was
 * opened at. Every string in the result is already in the reader's language.
 */
export function cloudStatusView(input) {
    input = input || {};
    var trail = input.trail || { commands: [], connection: {}, key_drift: [], mac_status: [] };
    // Before `cloud.status` has answered, each Mac that has published a notice digest is still
    // shown from that digest: it is the half that reaches a phone whose commands do not.
    var macs = Array.isArray(input.macs) ? input.macs : (trail.mac_status || []).map(function (row) {
        return { machine: row.machine, status: null, error: null };
    });
    var focus = input.focus || null;

    var connection = trail.connection || {};
    var browser = [fill(T.webCloudStatusConnection, { state: connection.state || "unknown" })];
    if (connection.last_relay_error && connection.last_relay_error.code) {
        browser.push(fill(T.webCloudStatusLastRefusal, { code: connection.last_relay_error.code }));
    }
    if (connection.last_close && connection.last_close.code !== null && connection.last_close.code !== undefined) {
        browser.push(fill(T.webCloudStatusClosed, { code: connection.last_close.code +
            (connection.last_close.name ? " " + connection.last_close.name : "") }));
    }
    var drift = Array.isArray(trail.key_drift) ? trail.key_drift : [];
    drift.forEach(function (row) {
        browser.push("key_id_drift · " + fill(T.webCloudStatusKeyDrift, { sent: row.sent, mac: row.received }));
    });
    var hints = [];
    if (trail.other_tabs > 0 || trail.replay) hints.push(T.webFailOtherTab);

    // Every Mac's row for a sequence, so each of this browser's commands can show the Mac's half.
    var macRows = [];
    macs.forEach(function (mac) {
        var commands = mac && mac.status && Array.isArray(mac.status.commands) ? mac.status.commands : [];
        commands.forEach(function (row) { if (row) macRows.push(row); });
    });

    var commands = (Array.isArray(trail.commands) ? trail.commands : []).map(function (row) {
        var ref = { sender: row.sender, seq: row.seq, request: row.request };
        var mac = macRows.find(function (candidate) { return sameRef(candidate, ref); }) || null;
        var refusal = row.refusal || (mac && mac.refusal) || null;
        return {
            ref: ref,
            refText: refText(ref),
            focus: sameRef(ref, focus),
            title: [row.type, row.machine].filter(Boolean).join(" · "),
            browserSteps: stepsLine(row.steps, BROWSER_STEPS),
            macSteps: macSteps(mac),
            refusal: refusal ? (refusal.layer || "mac") + " · " + refusal.code : ""
        };
    });

    var macSections = macs.map(function (mac) {
        var lines = [];
        var status = mac && mac.status;
        if (mac && mac.error) {
            return { machine: mac.machine, lines: [], error: failureSentence(mac.error, T.webCloudStatusReadFailed) };
        }
        var digest = null;
        (trail.mac_status || []).forEach(function (row) {
            if (row && row.machine === mac.machine) digest = row.cloud_status;
        });
        if (!status && !digest) {
            return { machine: mac.machine, lines: [T.webCloudStatusOldMac], error: "" };
        }
        var guard = (status && status.clock_guard) || (digest && digest.clock_guard) || null;
        if (guard && guard.state) {
            lines.push(fill(T.webCloudStatusClock, { state: guard.state + (guard.reason ? " · " + guard.reason : "") }));
        }
        var token = status && status.token && status.token.expires_at
            ? Date.parse(status.token.expires_at) : (digest && digest.token_expires_at_ms);
        if (Number.isFinite(token)) lines.push(fill(T.webCloudStatusToken, { at: clock(token) }));
        var identity = status && status.identity;
        var key = (identity && identity.key_id) || (digest && digest.key_id);
        if (key) lines.push(fill(T.webCloudStatusKey, { key: key }));
        if ((identity && identity.roster_readable === false) || (digest && digest.roster_readable === false)) {
            lines.push(T.webFailRoster);
        }
        var since = status && status.counting_since ? Date.parse(status.counting_since)
            : (digest && digest.counting_since_ms);
        var dropped = (status && status.inbound && status.inbound.dropped) || (digest && digest.dropped) || {};
        var list = Object.keys(dropped).filter(function (code) { return dropped[code] > 0; })
            .map(function (code) { return code + " " + dropped[code]; }).join(", ");
        lines.push(list ? fill(T.webCloudStatusDropped, { at: clock(since), list: list })
            : fill(T.webCloudStatusNoDrops, { at: clock(since) }));
        return { machine: mac.machine, lines: lines, error: "" };
    });

    return {
        browser: browser,
        hints: hints,
        repair: drift.length > 0,
        commands: commands,
        noCommands: commands.length ? "" : T.webCloudStatusNoCommands,
        reading: input.reading ? T.webCloudStatusReading : "",
        readError: input.readError ? failureSentence(input.readError, T.webCloudStatusReadFailed) : "",
        macs: macSections
    };
}
