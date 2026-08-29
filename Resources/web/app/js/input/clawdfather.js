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
   separately reads the device-safe Bearings projection so an existing offline owner closes its
   switch; that read exposes no machine credential or coordinator write. The sentence travels over
   `POST /v1/sessions/:id/send`, which the page can already reach and which every other composed
   message goes through.

   The browser also knows something the new session would otherwise have to work out: the
   terminal-neutral id. That is exactly the `id` on a Session row, so it is handed over in the
   instruction rather than left to `$ITERM_SESSION_ID` archaeology at the other end.
   -------------------------------------------------------------------------- */

/**
 * Whether the creation sheet may offer the Clawdfather choice.
 *
 * The device-readable Bearings record is durable, unlike the optional projection on a live
 * Session row: it still says `configured:true` when the registered coordinator is offline. That
 * is the answer this choice needs, because an offline Clawdfather is not an invitation to create
 * a second one.
 *
 * `payload === null` is the read still in flight. A malformed or failed answer also fails closed;
 * only the exact canonical unregistered tuple opens the switch. The current device projection
 * cannot distinguish a genuinely absent record from a corrupt or unsupported record because the
 * server folds all three into that tuple; fixing that is backend F2, not a distinction this UI
 * claims to make. Resuming is deliberately absent rather than disabled: assigning a role while
 * creating something is not an action on an old transcript.
 */
export function clawdfatherCreationChoice(payload, selected, fresh) {
    if (!fresh) {
        return { state: "hidden", shown: false, enabled: false, checked: false,
                 coordinator: null };
    }
    var coordinator = payload && typeof payload === "object"
        && payload.coordinator && typeof payload.coordinator === "object"
        ? payload.coordinator : null;
    if (!coordinator || typeof coordinator.configured !== "boolean") {
        return { state: payload === null ? "checking" : "unavailable",
                 shown: true, enabled: false, checked: false, coordinator: coordinator };
    }
    if (coordinator.configured === true) {
        return { state: "assigned", shown: true, enabled: false, checked: false,
                 coordinator: coordinator };
    }
    // Only the closed canonical tuple opens creation. Explicit new states fail closed here as
    // future-proofing; the current device projection still needs backend F2 before it can expose
    // corrupt/unsupported records differently from genuine absence.
    if (coordinator.configured !== false || coordinator.status !== "unregistered"
        || coordinator.lifecycle !== "unregistered") {
        return { state: "unavailable", shown: true, enabled: false, checked: false,
                 coordinator: coordinator };
    }
    return {
        state: selected === true ? "selected" : "available",
        shown: true, enabled: true, checked: selected === true, coordinator: coordinator
    };
}

/** Reliable creation-only fallback while the server's former per-Session strings age out. */
export function clawdfatherCreationLabel() {
    return "Name the new session Clawdfather";
}

/** The one line typed into the new session. It never offers offline replacement. */
export function clawdfatherInstruction(session) {
    var id = session && typeof session.id === "string" ? session.id.trim() : "";
    if (!id) return "";
    return "Please register this new session as this Mac's Clawdfather, the machine coordinator. "
        + "Your terminal-neutral session id is " + id + ". Read the coordinator record using "
        + "the orchestrator token at ~/.config/clawdline/orchestrator-token. If and only if no "
        + "coordinator is configured, follow the registration branch of “Becoming Clawdfather” "
        + "in docs/orchestrator.md. If any coordinator is already configured, including one that "
        + "is offline, do not replace it. Then report what happened.";
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
