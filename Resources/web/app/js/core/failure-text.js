import { T, fill } from "./i18n.js";
import { isFailureCode, refText } from "../net/cloud-failure.js";

/* ==========================================================================
   What a failure says on screen
   `docs/cloud-error-transparency.md` §5 rule 1: **the words are decided by the code and nothing
   else.** A server's `message` is English, and a page that shows it shows English to a reader in
   thirteen other languages — or, worse, shows "The command durability boundary is unavailable."
   to somebody holding a phone. So every screen that says a failure asks this file, and this file
   never reads `message`.

   A known code gets its own sentence. An unknown one gets the screen's own fallback sentence, or
   "That did not work." And every one of them — known or not — carries its `code · ref` in small
   type after the sentence (decision 4, §8), because that pair is what somebody debugging it will
   ask for, and `ref` is what the Cloud status sheet and the Mac's `cloud-status.json` are indexed by.
   ========================================================================== */

/** Code → string-table name. A code listed here is a sentence a person can act on. */
const SENTENCES = {
    machine_offline: "webFailMachineOffline",
    cloud_reconnecting: "webFailReconnecting",
    token_superseded: "webFailReconnecting",
    cloud_starting: "webFailReconnecting",
    going_away: "webFailReconnecting",
    unauthorized: "webFailSignedOut",
    revoked: "webFailSignedOut",
    forbidden: "webFailForbidden",
    missing_capability: "webFailForbidden",
    capability_denied: "webFailForbidden",
    rate_limited: "webFailRateLimited",
    over_capacity: "webFailRateLimited",
    too_large: "webFailTooLarge",
    report_too_large: "webFailTooLarge",
    command_too_large: "webFailTooLarge",
    cloud_read_timeout: "webFailNoAnswer",
    command_answer_undeliverable: "webFailReplyLost",
    read_answer_undeliverable: "webFailReplyLost",
    refusal_undeliverable: "webFailReplyLost",
    peer_rejected: "webFailReplyLost",
    receipt_expired: "webFailReplyLost",
    ready_expired: "webFailReplyLost",
    reply_not_received: "webFailReplyLost",
    delivery_unconfirmed: "webFailNoAnswer",
    cloud_feature_unavailable: "webFailMismatch",
    key_id_mismatch: "webFailKeyMismatch",
    key_id_drift: "webFailKeyMismatch",
    replay: "webFailOtherTab",
    unknown_sender: "webFailUnknownSender",
    cloud_ingress_busy: "webFailMacBusy",
    cloud_read_busy: "webFailMacBusy",
    reading_busy: "webFailMacBusy",
    busy: "webFailMacBusy",
    cloud_commands_disabled: "webFailMacWritesOff",
    command_writes_disabled: "webFailMacWritesOff",
    cloud_read_only: "webFailReadOnly",
    cloud_read_needs_send_prompt: "webFailReadOnly",
    command_roster_unreadable: "webFailRoster",
    cloud_machine_unavailable: "webFailNoMac",
    cloud_read_unavailable: "webFailNoMac",
    cloud_machine_ambiguous: "webFailWhichMac",
    cloud_session_ambiguous: "webFailWhichMac",
    cloud_schedule_ambiguous: "webFailWhichMac",
    cloud_voice_host_ambiguous: "webFailVoiceHostAmbiguous",
    cloud_voice_host_unavailable: "webFailVoiceHostUnavailable",
    cloud_machine_unsupported: "webFailMachineUnsupported",
    malformed_read: "webFailMismatch",
    malformed_command: "webFailMismatch",
    unknown_command: "webFailMismatch",
    unserializable_read: "webFailMismatch",
    bad_payload: "webFailMismatch",
    bad_frame: "webFailMismatch",
    unreadable_envelope: "webFailDecrypt",
    unknown_key: "webFailDecrypt",
    extractable_key: "webFailDecrypt",
    decrypt_failed: "webFailDecrypt",
    bad_signature: "webFailDecrypt",
    not_found: "webFailNotFound",
    offline: "webOffline",
    socket_error: "webOffline"
};

/** The sentence for a code whose words depend on one of its whitelisted `detail` fields. */
function detailedSentence(code, error) {
    var detail = (error && error.detail) || {};
    if (code === "command_clock_uncertain") {
        var ms = Number(detail.clears_in_ms);
        return Number.isFinite(ms) && ms > 0
            ? fill(T.webFailClockIn, { n: Math.max(1, Math.ceil(ms / 1000)) })
            : T.webFailClock;
    }
    if (code === "no_whisper") {
        return (detail.reason || (error && error.reason)) === "no_model" ? T.webVoiceNoModel : T.webVoiceNoBinary;
    }
    return "";
}

/**
 * Everything a screen needs to say one failure.
 *
 * `text` is the sentence; `tag` is `code · ref` (just `code` when the failure never reached the
 * wire); `known` says whether the sentence came from the code rather than the fallback. Drawing
 * it is acknowledgement (§2.4), so a Cloud failure is marked `acknowledged` on the trail here.
 *
 * `options.sentence` is a screen's own words for this code — the start sheet's "that project is
 * gone" is better than the general "the Mac no longer has this" — and wins when given.
 * `options.fallback` is its words for a code nobody here knows. Neither is ever `message`.
 */
export function describeFailure(error, options) {
    options = typeof options === "string" ? { fallback: options } : (options || {});
    var code = error && isFailureCode(error.code) ? error.code : "unexpected_error";
    var sentence = options.sentence || detailedSentence(code, error);
    var name = SENTENCES[code];
    if (!sentence && name && typeof T[name] === "string") sentence = T[name];
    var known = !!sentence;
    if (!sentence) sentence = options.fallback || T.webFailUnknown;
    var ref = (error && error.ref) || null;
    var named = refText(ref);
    if (error && typeof error.acknowledge === "function") {
        try { error.acknowledge(); } catch (e) { /* a trail cannot stop a sentence */ }
    }
    return {
        text: sentence,
        code: code,
        ref: ref,
        refText: named,
        tag: named ? code + " · " + named : code,
        layer: (error && typeof error.layer === "string") ? error.layer : null,
        retryable: !!(error && error.retryable),
        known: known
    };
}

/**
 * The sentence and its tag as one line of plain text, for places that can only hold text.
 * The second argument is the fallback sentence, or `{ sentence, fallback }`.
 */
export function failureSentence(error, options) {
    var said = describeFailure(error, options);
    return fill(T.webFailWithTag, { text: said.text, tag: said.tag });
}

/**
 * The one line a list read over several machines says when some machine could have answered and
 * did not (`CloudClient.places()`'s `unanswered`), naming those machines; "" for a complete answer.
 *
 * A partial list looks exactly like a complete one, so without this line a Mac that was busy or
 * silent is indistinguishable from a Mac that has no Projects.
 */
export function unansweredSentence(answer) {
    var rows = answer && Array.isArray(answer.unanswered) ? answer.unanswered : [];
    var names = rows.map(function (row) {
        return row && (row.label || row.machine);
    }).filter(function (name, index, all) {
        return typeof name === "string" && name && all.indexOf(name) === index;
    });
    if (!names.length) return "";
    var lang = typeof document !== "undefined" && document && document.documentElement
        && document.documentElement.lang || undefined;
    var joined;
    try { joined = new Intl.ListFormat(lang, { style: "long", type: "conjunction" }).format(names); }
    catch (e) { joined = names.join(", "); }
    return fill(T.webMachinesUnanswered, { machines: joined });
}

/* ---- the one press ------------------------------------------------------------------------ */

var opener = null;

/** The Cloud status sheet registers itself here; until it does, a failure line is only text. */
export function setFailureOpener(open) { opener = typeof open === "function" ? open : null; }

/** A press that opens the status sheet at this failure's `ref`, or null where there is none. */
export function failureOpener(error) {
    if (!opener) return null;
    var ref = (error && error.ref) || null;
    var code = error && isFailureCode(error.code) ? error.code : null;
    var open = opener;
    return function () { open({ ref: ref, code: code, layer: error && error.layer }); };
}

/**
 * Make a line that already holds a failure's text open the status sheet when pressed — or,
 * given no error, stop doing so, because the same line says the next success too.
 */
export function bindFailureLine(element, error) {
    if (!element) return;
    var open = error ? failureOpener(error) : null;
    // A line that cannot be made pressable still says its failure: nothing here may throw into
    // the settle that called it, which is the hang `Tests/web-start-sheet-failures.mjs` is about.
    try {
        element.onclick = open;
        if (element.classList && typeof element.classList.toggle === "function") {
            element.classList.toggle("failure-open", !!open);
        }
        if (typeof element.setAttribute === "function" && typeof element.removeAttribute === "function") {
            if (open) element.setAttribute("title", T.webFailTap); else element.removeAttribute("title");
        }
    } catch (e) { /* the words are already on the line */ }
}

/**
 * Draw one failure into an element: the sentence, then the tag in small type, and the whole
 * line pressable when a status sheet is there to open. Plain text is the fallback wherever the
 * element cannot hold children.
 */
export function renderFailure(element, error, fallback) {
    if (!element) return null;
    var said = describeFailure(error, { fallback: fallback });
    var doc = element.ownerDocument || (typeof document !== "undefined" ? document : null);
    if (!doc || typeof doc.createElement !== "function" || typeof element.appendChild !== "function") {
        element.textContent = fill(T.webFailWithTag, { text: said.text, tag: said.tag });
        return said;
    }
    element.textContent = said.text + " ";
    var tag = doc.createElement("small");
    tag.className = "failure-tag";
    tag.textContent = said.tag;
    element.appendChild(tag);
    bindFailureLine(element, error);
    return said;
}
