/* --------------------------------------------------------------------------
   Asking a session to make itself Clawdfather

   Registering the machine coordinator needs `X-Clawdline-Orchestrator`, and a browser holds a
   device token. That boundary is deliberate and is not widened here: this module adds no route
   and reads no coordinator state of its own.

   What it does instead is compose one sentence. The session on the other end is a local process
   running as the person who owns this Mac, so it can read
   `~/.config/clawdline/orchestrator-token` and carry out the registration-only branch of the
   documented recipe itself — the same trust boundary every other Clawdline dispatch already
   stands on, and the same one a person crosses by typing the curl by hand. The creation sheet
   separately reads the device-safe Bearings projection for one closed word — whether registering
   would write over something — so an existing offline owner, and a store nobody may overwrite,
   both close its switch; that read exposes no machine credential or coordinator write. The
   sentence travels over `POST /v1/sessions/:id/send`, which the page can already reach and which
   every other composed message goes through.

   The browser also knows something the new session would otherwise have to work out: the
   terminal-neutral id. That is exactly the `id` on a Session row, so it is handed over in the
   instruction rather than left to `$ITERM_SESSION_ID` archaeology at the other end.
   -------------------------------------------------------------------------- */

import { T, fill } from "../core/i18n.js";

/**
 * Whether the creation sheet may offer the Clawdfather choice.
 *
 * **One field decides it, and it is not `coordinator`.** The Mac sends
 * `registration.state`, derived from the durable store it would have to write, and it says one
 * of exactly three words: `available` (nothing stored), `configured` (a valid owner, online or
 * offline — both are owners), `blocked` (a record that is corrupt, unreadable or from a version
 * this Mac does not understand, which registration must never overwrite).
 *
 * The `coordinator` tuple beside it cannot answer this and is kept only for the words the sheet
 * says out loud. Absent, corrupt and unsupported stores project the *identical*
 * `configured:false, status:"unregistered"` tuple, so a switch reading that tuple offers to
 * register over a record the broker will refuse with `coordinator_store_invalid` — and the
 * refusal arrives after the instruction has been typed into a session, where the browser never
 * sees it.
 *
 * Everything that is not exactly `"available"` fails closed. `payload === null` is the read
 * still in flight; a missing, misspelled, wrongly-cased or unknown state, and an answer from a
 * Mac that predates the field, are all `unavailable`. Resuming is deliberately absent rather
 * than disabled: assigning a role while creating something is not an action on an old
 * transcript.
 */
export function clawdfatherCreationChoice(payload, selected, fresh) {
    if (!fresh) {
        return { state: "hidden", shown: false, enabled: false, checked: false,
                 coordinator: null };
    }
    var answered = payload && typeof payload === "object";
    var coordinator = answered && payload.coordinator && typeof payload.coordinator === "object"
        ? payload.coordinator : null;
    var registration = answered && payload.registration
        && typeof payload.registration === "object" ? payload.registration : null;
    var state = registration && typeof registration.state === "string"
        ? registration.state : "";
    if (state === "configured") {
        return { state: "assigned", shown: true, enabled: false, checked: false,
                 coordinator: coordinator };
    }
    if (state === "blocked") {
        return { state: "blocked", shown: true, enabled: false, checked: false,
                 coordinator: coordinator };
    }
    if (state !== "available") {
        return { state: payload === null ? "checking" : "unavailable",
                 shown: true, enabled: false, checked: false, coordinator: coordinator };
    }
    return {
        state: selected === true ? "selected" : "available",
        shown: true, enabled: true, checked: selected === true, coordinator: coordinator
    };
}

/* --------------------------------------------------------------------------
   The words, and the two that are kept here as well

   Both sentences below come from the Mac: `T.webClawdfatherCreateLabel` is the chip on the
   creation sheet and `T.webClawdfatherRegisterAsk` is the line typed into the new session,
   translated into every language in `L.catalog`. `core/i18n.js` already carries an English copy
   of each for a page
   whose `/v1/strings` read failed, and `applyStrings` refuses an empty value, so the ordinary
   missing-translation case never reaches this module.

   What does reach it is a value that arrived and cannot be used, which `applyStrings` cannot
   judge because it only looks at the type. Three shapes matter, and each is a real sentence
   somebody would otherwise be shown or an assistant would otherwise be told:

   * **Blank.** A chip with no words on it is a switch nobody can read.
   * **No `{id}` hole.** The far end would be asked to register "this session" with no way to say
     which, which is the archaeology the browser exists to save it — so the line is unusable
     rather than merely worse.
   * **Names `rebind`.** The retired branch. This flow registers an unregistered coordinator and
     nothing else, and a translation that still teaches reconnecting an offline owner would be
     typed into something that acts on it. Refusing it here makes the contract enforced rather
     than merely written down.

   In all three the English below is typed instead, because a wrong sentence is worse than an
   untranslated one when the reader is going to act on it.
   -------------------------------------------------------------------------- */

var CREATION_LABEL_EN = "Name the new session Clawdfather";

var CREATION_ASK_EN =
    "Please register this new session as this Mac's Clawdfather, the machine coordinator. "
    + "Your terminal-neutral session id is {id}. Read the coordinator record using "
    + "the orchestrator token at ~/.config/clawdline/orchestrator-token. If and only if no "
    + "coordinator is configured, follow the registration branch of “Becoming Clawdfather” "
    + "in docs/orchestrator.md. If any coordinator is already configured, including one that "
    + "is offline, do not replace it. Then report what happened.";

/** A served string, or the English, when what arrived is not a sentence at all. */
function served(value, english) {
    return typeof value === "string" && value.trim() ? value : english;
}

/** The chip on the creation sheet, in this browser's language. */
export function clawdfatherCreationLabel() {
    return served(T.webClawdfatherCreateLabel, CREATION_LABEL_EN);
}

/** The one line typed into the new session. It never offers offline replacement. */
export function clawdfatherInstruction(session) {
    var id = session && typeof session.id === "string" ? session.id.trim() : "";
    if (!id) return "";
    var template = served(T.webClawdfatherRegisterAsk, CREATION_ASK_EN);
    if (template.indexOf("{id}") < 0 || /\brebind\b/i.test(template)) {
        template = CREATION_ASK_EN;
    }
    return fill(template, { id: id });
}

function timeoutError(code, message) {
    var error = new Error(message);
    error.code = code;
    return error;
}

/** Settle one network boundary within a fixed time and release its timer on every outcome. */
function bounded(work, timeoutMs, code, message, schedule, cancel) {
    var later = typeof schedule === "function" ? schedule : setTimeout;
    var stop = typeof cancel === "function" ? cancel : clearTimeout;
    return new Promise(function (resolve, reject) {
        var settled = false;
        var timer = later(function () {
            if (settled) return;
            settled = true;
            reject(timeoutError(code, message));
        }, timeoutMs);
        Promise.resolve().then(work).then(function (value) {
            if (settled) return;
            settled = true;
            stop(timer);
            resolve(value);
        }, function (error) {
            if (settled) return;
            settled = true;
            stop(timer);
            reject(error);
        });
    });
}

/**
 * The last-moment ownership check and the only path to the registration instruction send.
 *
 * This read is deliberately inside the operation rather than supplied by the open sheet. The
 * tab can take seconds to become addressable, which is enough time for another coordinator to be
 * registered. Missing `enabled` is not permission: the positive boolean is the whole gate.
 */
export async function attemptClawdfatherAssignment(session, client, options) {
    var settings = options || {};
    var timeoutMs = Number.isFinite(settings.timeoutMs) && settings.timeoutMs > 0
        ? settings.timeoutMs : 8000;
    var choose = typeof settings.choice === "function"
        ? settings.choice : clawdfatherCreationChoice;
    if (!client || typeof client.coordinatorBearings !== "function") {
        throw timeoutError("coordinator_unavailable", "Could not read Clawdfather ownership.");
    }
    var data = await bounded(function () { return client.coordinatorBearings(); }, timeoutMs,
        "coordinator_timeout", "Timed out checking Clawdfather ownership.",
        settings.schedule, settings.cancel);
    var choice = choose(data, true, true);
    if (!choice || choice.enabled !== true) return { state: "blocked", choice: choice || null };

    var text = clawdfatherInstruction(session);
    if (!text || !client || typeof client.send !== "function") {
        throw timeoutError("assignment_unavailable", "The new session cannot receive the instruction.");
    }
    await bounded(function () { return client.send(session.id, text, []); }, timeoutMs,
        "assignment_timeout", "Timed out asking the new session to become Clawdfather.",
        settings.schedule, settings.cancel);
    return { state: "sent", choice: choice };
}

/**
 * One latest-wins loader for the sheet's ownership status.
 *
 * Opening twice may put two HTTP promises in flight. The generation belongs to the request, not
 * its answer, so a slow first answer cannot overwrite the second opening's current truth.
 */
export function createClawdfatherCoordinatorLoader(clientOrGetter, publish) {
    var generation = 0;
    function client() {
        return typeof clientOrGetter === "function" ? clientOrGetter() : clientOrGetter;
    }
    return {
        load: function () {
            var mine = ++generation;
            if (typeof publish === "function") {
                publish({ generation: mine, payload: null, failed: false, ready: false });
            }
            var current = client();
            if (!current || typeof current.coordinatorBearings !== "function") {
                var unavailable = { generation: mine, payload: {}, failed: true, ready: true };
                if (mine === generation && typeof publish === "function") publish(unavailable);
                return Promise.resolve(unavailable);
            }
            return Promise.resolve().then(function () {
                return current.coordinatorBearings();
            }).then(function (data) {
                if (mine !== generation) return { generation: mine, stale: true };
                var result = { generation: mine,
                    payload: data && typeof data === "object" ? data : {},
                    failed: !(data && typeof data === "object"), ready: true };
                if (typeof publish === "function") publish(result);
                return result;
            }, function (error) {
                if (mine !== generation) return { generation: mine, stale: true, error: error };
                var result = { generation: mine, payload: {}, failed: true, ready: true,
                               error: error };
                if (typeof publish === "function") publish(result);
                return result;
            });
        },
        invalidate: function () { generation += 1; },
        generation: function () { return generation; }
    };
}

/**
 * Creation choice plus one finite pending assignment.
 *
 * The sheet owns the choice; the queue owns everything after the start reply. Both are cleared
 * on every terminal path so reopening cannot accidentally apply yesterday's tick to a second tab.
 */
export function createClawdfatherAssignmentState(options) {
    var settings = options || {};
    var schedule = typeof settings.schedule === "function" ? settings.schedule : setTimeout;
    var cancel = typeof settings.cancel === "function" ? settings.cancel : clearTimeout;
    var timeoutMs = Number.isFinite(settings.timeoutMs) && settings.timeoutMs > 0
        ? settings.timeoutMs : 15000;
    var selected = false;
    var pending = null;

    function clearPending() {
        if (!pending) return;
        if (pending.timer != null) cancel(pending.timer);
        pending = null;
    }

    return {
        open: function () { selected = false; },
        choose: function (value) { selected = value === true; },
        selected: function () { return selected; },
        begin: function (id, requested) {
            selected = false;
            clearPending();
            if (requested !== true || typeof id !== "string" || !id) return false;
            var record = { id: id, checking: false, timer: null };
            pending = record;
            record.timer = schedule(function () {
                if (pending !== record) return;
                pending = null;
                if (typeof settings.onTimeout === "function") settings.onTimeout(id);
            }, timeoutMs);
            return true;
        },
        pendingID: function () { return pending ? pending.id : null; },
        clear: function () { selected = false; clearPending(); },
        attempt: function (session, client, attemptOptions) {
            if (!pending || pending.checking || !session || session.id !== pending.id) return null;
            var assistant = typeof session.assistant === "string" ? session.assistant.trim() : "";
            if (!assistant) return null;
            var record = pending;
            record.checking = true;
            if (record.timer != null) { cancel(record.timer); record.timer = null; }
            return attemptClawdfatherAssignment(session, client, attemptOptions).then(
                function (result) {
                    if (pending === record) pending = null;
                    selected = false;
                    if (typeof settings.onSettled === "function") settings.onSettled(result);
                    return result;
                }, function (error) {
                    if (pending === record) pending = null;
                    selected = false;
                    var result = { state: "failed", error: error };
                    if (typeof settings.onSettled === "function") settings.onSettled(result);
                    return result;
                });
        }
    };
}
