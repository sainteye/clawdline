import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { sessionSelectionKey } from "../session/selection.js";

/* ==========================================================================
   6. Deriving what to show
   ========================================================================== */

var SESSION_WORK_STATES = {
    ready: true, working: true, holding: true, waiting_you: true, waiting_session: true,
    unknown: true, milestone_complete: true, work_complete: true
};

function attr(value) {
    return String(value == null ? "" : value).replace(/[&<>\"]/g, function (ch) {
        return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '\"': "&quot;" }[ch];
    });
}

/**
 * A platform emoji is taller than this row's mono text on iOS. Keeping it in the same text run
 * made the row's `overflow: hidden` shave off its top at `line-height: 1`; this separate flex
 * child owns a taller line box while only the words remain eligible for ellipsis. The glyph is
 * decorative because the adjacent localized copy is the status's accessible name.
 */
export function sessionStatusGlyphHTML(icon, copy) {
    return '<span class="session-status-glyph" aria-hidden="true">' + attr(icon) +
        '</span><span class="session-status-label">' + attr(copy) + '</span>';
}

/**
 * The browser repeats the safety precedence so an old or partial server frame cannot turn a
 * question, peer wait, or unreadable screen into a check. Every other missing/unknown value is
 * one explicit state too: unknown — the broker's honest absence — never an empty line.
 */
export function projectSessionWorkState(s) {
    s = s || {};
    if (s.provider_block === "model_capacity") return { state: "waiting_you", failedClosed: false };
    if (s.state === "waiting") return { state: "waiting_you", failedClosed: false };
    // The terminal is the direct observation that an agent is moving. Coordination is an
    // independent, quiet overlay: it may explain what this turn is waiting to release, but it
    // must never replace Working as the row's primary state. In particular, do not require a
    // matching `work_state` from a possibly mixed-version frame before drawing activity — doing
    // so turns a live spinner into an hourglass exactly when the person most needs to know the
    // agent is still running.
    if (s.state === "working") return { state: "working", failedClosed: false };
    var coordination = s.coordination || {};
    if ((coordination.waitingOn || []).length || (coordination.waitedOnBy || []).length) {
        return { state: "waiting_session", failedClosed: false };
    }
    if (s.state === "unknown") return { state: "unknown", failedClosed: false };
    if (!SESSION_WORK_STATES[s.work_state]) return { state: "unknown", failedClosed: true };
    // These claims must agree with their source axes. The server always projects them
    // consistently; checking again turns a partial or mixed-version frame into the quiet
    // absence rather than two mutually exclusive messages on one row.
    if (s.work_state === "waiting_you") {
        return { state: "unknown", failedClosed: true };
    }
    if (s.work_state === "working") {
        return { state: "unknown", failedClosed: true };
    }
    if (s.work_state === "waiting_session") {
        var declaredWait = s.work_provenance === "self" && !!s.work_note &&
            !!s.work_moved_by && s.work_person_needed === false;
        return (declaredWait || tasksOfRoot(s.id).some(taskLive))
            ? { state: "waiting_session", failedClosed: false }
            : { state: "unknown", failedClosed: true };
    }
    // holding has exactly one entrance — the session's own declared claim — and is never a
    // fallback. A frame that says holding without saying who declared it fails closed to the
    // absence, because a holding anything can slide into is the old needs_triage defect back
    // under a new name.
    if (s.work_state === "holding" && s.work_provenance !== "self") {
        return { state: "unknown", failedClosed: true };
    }
    if (s.work_state === "milestone_complete" || s.work_state === "work_complete") {
        var disposition = s.disposition || {};
        var taskMilestone = s.work_state === "milestone_complete" &&
            disposition.scope === "task" && !!disposition.taskId &&
            disposition.evidence === "authenticated_task_delivery";
        var sessionMilestone = s.work_state === "milestone_complete" &&
            disposition.scope === "session" &&
            disposition.evidence === "authenticated_session_delivery";
        var taskClosure = s.work_state === "work_complete" &&
            disposition.scope === "task" && !!disposition.taskId &&
            disposition.evidence === "broker_verified_target_landing";
        var sessionClosure = s.work_state === "work_complete" &&
            disposition.scope === "session" &&
            disposition.evidence === "broker_verified_target_landing";
        if (!taskMilestone && !sessionMilestone && !taskClosure && !sessionClosure) {
            return { state: "unknown", failedClosed: true };
        }
    }
    return { state: s.work_state, failedClosed: false };
}

/** The visible, bounded account of a Session-authored peer handoff. Both the compact list and
 *  the unabridged Info sheet use this function so a phone never has to rely on hover text to
 *  learn who moves the work. Empty means the declaration is not complete enough to trust. */
export function selfReportedPeerWaitCopy(s) {
    if (!s || s.work_provenance !== "self" || !s.work_note || !s.work_moved_by ||
        s.work_person_needed !== false) return "";
    return s.work_moved_by + " · " + s.work_note + " · " + T.sessionWorkSelfStated;
}

var CLOSEABILITY_STATES = {
    blocked: true, needs_attestation: true, safe: true, unknown: true
};

var CLOSEABILITY_ICON = { safe: "\uD83D\uDD13", blocked: "\uD83D\uDD12", needs_attestation: "\uD83D\uDDDD" };

function closeabilityBlock(s) {
    var value = s && s.closeability;
    return (!!value && typeof value === "object" && !Array.isArray(value)) ? value : null;
}

function closeabilityReasonRows(block) {
    var rows = block && block.reasons;
    return Array.isArray(rows) ? rows.filter(function (row) {
        return !!row && typeof row === "object" && !Array.isArray(row) &&
            typeof row.code === "string";
    }) : [];
}

/**
 * The fourth projection, re-decided at the client so a partial or older frame can never draw
 * "safe to close".
 *
 * The rule is the server's own, restated: `safe` is the one value with a positive precondition
 * — a current reading, an attestation id, and no reason left standing — and everything that
 * does not meet it falls to `unknown`, which no close accepts. `ready` is not consulted at all:
 * able to take work and able to end are different questions, and reading one off the other is
 * exactly the collapse this projection exists to undo.
 */
export function projectSessionCloseability(s) {
    s = s || {};
    var block = closeabilityBlock(s);
    if (!block) {
        var present = Object.prototype.hasOwnProperty.call(s, "closeability");
        var malformed = present ? { state: "unknown", reasons: [], mover: null } : null;
        return { state: "unknown", failedClosed: true, reasons: [], block: malformed };
    }
    if (!CLOSEABILITY_STATES[block.state]) {
        return { state: "unknown", failedClosed: true, reasons: [], block: block };
    }
    var reasons = closeabilityReasonRows(block);
    var kinds = reasons.map(function (row) { return row.kind; });
    var freshness = (block.source && block.source.freshness) || "";
    if (block.state === "safe") {
        var proven = !reasons.length && freshness === "current" &&
            typeof block.attestation_id === "string" && !!block.attestation_id &&
            typeof block.version === "string" && !!block.version &&
            s.state !== "working" && s.state !== "waiting" && s.state !== "unknown";
        return proven
            ? { state: "safe", failedClosed: false, reasons: reasons, block: block }
            : { state: "unknown", failedClosed: true, reasons: reasons, block: block };
    }
    if (block.state === "blocked" && kinds.indexOf("obligation") < 0) {
        return { state: "unknown", failedClosed: true, reasons: reasons, block: block };
    }
    if (block.state === "needs_attestation" &&
        (!reasons.length || kinds.indexOf("obligation") >= 0 || kinds.indexOf("evidence") >= 0)) {
        return { state: "unknown", failedClosed: true, reasons: reasons, block: block };
    }
    return { state: block.state, failedClosed: false, reasons: reasons, block: block };
}

/** Who clears the thing that is standing in the way, in the reader's language. */
export function closeabilityMoverText(mover) {
    if (!mover || typeof mover !== "object") return "";
    if (mover.kind === "person") return T.closeabilityMoverPerson;
    if (mover.kind === "broker") return T.closeabilityMoverBroker;
    if (mover.kind === "task") return T.closeabilityMoverSession;
    if (mover.kind === "session") {
        return mover["self"] ? T.closeabilityMoverSelf : T.closeabilityMoverSession;
    }
    return "";
}

/**
 * The rows the close confirmation lists. Each one names the machine code, the subject it is
 * about, and who moves it — a code alone tells somebody there is a problem and not which
 * object has it, and a sentence alone cannot be grepped for in a log.
 */
export function closeabilityLines(s) {
    var projected = projectSessionCloseability(s);
    return projected.reasons.map(function (row) {
        var said = row.code;
        if (row.subject_id) said += " · " + row.subject_id;
        var mover = closeabilityMoverText(row.mover);
        if (mover) said += " · " + mover;
        return said;
    });
}

/**
 * Session info is read by a person, not by the broker. Translate the closed reason vocabulary
 * into actions and group repetitions (two dirty child worktrees are one kind of problem, not two
 * UUIDs somebody should have to decode). `closeabilityLines` remains the deliberately searchable
 * technical form used by logs and the close confirmation's disclosure.
 */
export function closeabilityPlainReasons(s) {
    var keys = {
        terminal_working: "webInfoCloseReasonWorking",
        terminal_waiting_you: "webInfoCloseReasonWaitingYou",
        own_task_unfinished: "webInfoCloseReasonTaskActive",
        live_descendant_task: "webInfoCloseReasonChildActive",
        task_without_result: "webInfoCloseReasonTaskResult",
        pending_landing_owned: "webInfoCloseReasonLanding",
        coordination_wait_owned: "webInfoCloseReasonCoordinationOwned",
        coordination_wait_waiting: "webInfoCloseReasonCoordinationWaiting",
        open_handoff: "webInfoCloseReasonHandoff",
        completion_undelivered: "webInfoCloseReasonCompletion",
        owed_decision: "webInfoCloseReasonDecision",
        dirty_isolated_worktree: "webInfoCloseReasonDirtyWorktree",
        touched_claims_without_closure: "webInfoCloseReasonTouchedFiles"
    };
    var groups = [];
    var byText = {};
    projectSessionCloseability(s).reasons.forEach(function (row) {
        var key = keys[row.code];
        if (!key && row.kind === "evidence") key = "webInfoCloseReasonEvidence";
        if (!key && row.kind === "attestation") key = "webInfoCloseReasonAttestation";
        var copy = T[key || "webInfoCloseReasonOther"] || T.webInfoCloseReasonOther;
        if (!copy) return;
        if (byText[copy] != null) {
            groups[byText[copy]].count += 1;
            return;
        }
        byText[copy] = groups.length;
        groups.push({ text: copy, count: 1 });
    });
    return groups;
}

/** Everything in the badge whose identity can change its rendered words. */
export function sessionCloseabilityShape(s) {
    var projected = projectSessionCloseability(s);
    if (!projected.block) return "";
    function moverShape(mover) {
        if (!mover || typeof mover !== "object") return "-";
        return [mover.kind || "-", mover["self"] === true ? "self" : "other",
            mover.session_id || "-"].join(":");
    }
    var reasons = projected.reasons.map(function (reason) {
        return [reason.code || "-", reason.kind || "-", reason.subject_kind || "-",
            reason.subject_id || "-", moverShape(reason.mover)].join(":");
    }).join("|");
    return [projected.state, projected.failedClosed ? "closed" : "direct",
        moverShape(projected.block.mover), reasons].join(";");
}

/**
 * One badge, beside the work state and never instead of it.
 *
 * `unknown` keeps the vocabulary's rule about absences: it says so in words and carries no
 * icon, because giving an absence a symbol is how `needs_triage` came to read as a demand.
 */
export function sessionCloseabilityHTML(s) {
    var projected = projectSessionCloseability(s);
    var copy;
    if (projected.state === "safe") copy = T.closeabilitySafe;
    else if (projected.state === "blocked") {
        var obligations = projected.reasons.filter(function (row) {
            return row.kind === "obligation";
        }).length;
        var forms = String(T.closeabilityBlocked || "").split("\u001f");
        copy = fill(obligations === 1 ? forms[0] : (forms[1] || forms[0]),
            { n: obligations });
    } else if (projected.state === "needs_attestation") {
        copy = T.closeabilityNeedsAttestation;
    } else {
        // Fast list reads keep a prior row while a background scan runs. Its
        // close gate is unknown for that moment, but this is an active check,
        // not a terminal failure to determine closeability.
        var source = s && s.source;
        var refreshing = source && source.freshness === "unverified" &&
            projected.reasons.some(function (row) { return row.code === "session_inventory_stale"; });
        copy = refreshing ? T.closeabilityRefreshing : T.closeabilityUnknown;
    }
    var mover = projected.block && projected.block.mover;
    var moverSaid = projected.state === "safe" ? "" : closeabilityMoverText(mover);
    var title = moverSaid ? copy + " · " + moverSaid : copy;
    var icon = CLOSEABILITY_ICON[projected.state];
    return '<span class="session-closeability" data-closeability="' + projected.state +
        '" title="' + attr(title) + '">' +
        (icon ? sessionStatusGlyphHTML(icon, copy) : attr(copy)) + "</span>";
}

/** A debt's age in the coarsest honest unit. Under an hour it is simply fresh; the value of
 *  the number starts where memory stops. */
function owedAge(since) {
    if (!since) return "";
    var seconds = Math.floor(Date.now() / 1000) - since;
    if (seconds < 3600) return "";
    if (seconds < 172800) return Math.floor(seconds / 3600) + "h";
    return Math.floor(seconds / 86400) + "d";
}

/**
 * The second axis, rendered beside whatever the first is doing. A session can owe-and-work at
 * once, so this is an appended badge, never a replacement — and it ages in plain sight,
 * because a debt's failure mode is "nobody remembers in three days", not "not seen now".
 */
/** The inventory transport, not the suggested text, supplies the machine identity. */
export function sessionSuggestedReply(s, now = Date.now() / 1000) {
    const debt = s && s.owed, reply = debt && debt.suggestedReply;
    const machine = s && (s.machine || (s.identity && s.identity.machine));
    if (!debt || debt.person_needed !== true || !reply || Array.isArray(reply)
        || reply.version !== 1 || !Number.isFinite(now)) return null;
    const bounded = (v, max) => typeof v === "string" && v.trim().length > 0 && v.length <= max;
    if (Object.keys(reply).sort().join(",") !== "conversationId,expiresAt,id,text,version"
        || !bounded(reply.id, 200) || !/^[A-Za-z0-9_.:-]+$/.test(reply.id)
        || !bounded(reply.text, 4000) || new TextEncoder().encode(reply.text).length > 4000
        || !bounded(machine, 200) || !bounded(reply.conversationId, 512)
        || /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(reply.text)
        || !Number.isSafeInteger(reply.expiresAt) || reply.expiresAt <= now
        || (s.identity && s.identity.machine && s.identity.machine !== machine)
        || reply.conversationId !== s.sessionId) return null;
    return { text: reply.text, key: JSON.stringify([reply.version, reply.id, reply.text,
        machine, reply.conversationId, reply.expiresAt]) };
}

/** This is a composer pin, not a fallback lookup: duplicate bare IDs are not addressable. */
export function replySessionIdentity(s, rows = S.sessions) {
    if (!s || !Array.isArray(rows)) return null;
    if (rows.filter(function (row) { return row && row.id === s.id; }).length !== 1) return null;
    var explicitMachine = s.machine || (s.identity && s.identity.machine);
    if (typeof explicitMachine !== "string" || !explicitMachine) return null;
    var key = sessionSelectionKey(s);
    if (!key || typeof s.sessionId !== "string" || !s.sessionId) return null;
    var exact = rows.filter(function (row) { return sessionSelectionKey(row) === key; });
    return exact.length === 1 ? key : null;
}

export function suggestedReplyButtonHTML(s, options = {}) {
    const reply = sessionSuggestedReply(s, options.now);
    if (!reply) return "";
    const identity = replySessionIdentity(s, options.rows);
    const enabled = options.writable === true && options.openId === s.id
        && identity !== null && options.composerIdentity === identity;
    const label = options.zh ? "填入建議回覆" : "Fill suggested reply";
    const destination = s.label || s.title || s.id;
    const explanation = options.zh ? "僅填入草稿，請檢查後自行送出" : "Draft only; review and send yourself";
    const reason = enabled ? "" : options.openId !== s.id
        ? (options.zh ? "先開啟此 Session" : "Open this Session first")
        : options.writable !== true
            ? (options.zh ? "等待連線與輸入就緒" : "Wait for connection and input readiness")
            : (options.zh ? "等待對話身分確認" : "Wait for conversation identity confirmation");
    return '<button type="button" class="session-suggested-reply" data-reply-session="' + attr(s.id) +
        '" data-reply-key="' + attr(reply.key) + '" title="' + attr(destination + " — " + explanation) +
        '"' + (enabled ? "" : " disabled") + '>' + label +
        (reason ? " · " + reason : "") + '</button>';
}

export function owedBadgeHTML(s) {
    var owed = s && s.owed;
    if (!owed || typeof owed !== "object" || Array.isArray(owed)) return "";
    var copy = owed.note || T.sessionWorkOwed;
    var age = owedAge(owed.since);
    if (age) copy += " · " + age;
    return '<span class="session-work-owed" data-person-needed="' +
        (owed.person_needed === false ? "no" : "yes") + '" title="' + attr(copy) + '">' +
        sessionStatusGlyphHTML("📥", copy) + "</span>";
}

/** What a finished row's receipt says. A Session's own turn report reads "turn reported" unless
 *  the server names a Board item of that Session that reached deploying or done and the person
 *  has not opened since: then it reads "awaiting acceptance" with that item's title. A Board the
 *  server could not read says so beside the report; it is never shown as nothing awaiting.
 *  `acceptance` is "pending", "none", "unknown", or "" when the receipt is not a Session's own;
 *  a Session's report without the field is "unknown", never "none". */
export function sessionReceiptCopy(s, state) {
    var disposition = (s && s.disposition) || {};
    var own = disposition.scope === "session";
    var acceptance = own && s.acceptance && typeof s.acceptance === "object" ? s.acceptance : null;
    var kind = acceptance && (acceptance.state === "pending" || acceptance.state === "none" ||
        acceptance.state === "unknown") ? acceptance.state : "";
    if (kind === "pending" && acceptance.title) {
        var more = acceptance.count > 1 ? " · " + fill(T.sessionWorkAwaitingMore, { n: acceptance.count }) : "";
        return { label: T.sessionWorkAwaitingAcceptance, detail: acceptance.title + more, acceptance: kind };
    }
    // A pending answer that names nothing, or a Session's report with no answer at all (a
    // daemon from before the field), is a reading that did not happen, not a reading of none.
    if (own && kind !== "none") kind = "unknown";
    var label = state === "work_complete" ? T.sessionWorkComplete
        : own ? T.sessionWorkReported : T.sessionWorkMilestone;
    var detail = disposition.title || "";
    if (kind === "unknown") detail = (detail ? detail + " · " : "") + T.sessionWorkAcceptanceUnknown;
    return { label: label, detail: detail, acceptance: kind };
}

/** Check glyphs are CSS strokes, not a platform emoji. The quiet states carry their meaning in
 *  an icon a phone can scan — except `unknown`, which is an absence, not a category: giving an
 *  absence a symbol is how needs_triage came to read as a demand, so it deliberately has none. */
export function sessionWorkStateHTML(s) {
    var projected = projectSessionWorkState(s);
    var said = "";
    if (projected.state === "milestone_complete" || projected.state === "work_complete") {
        var receipt = sessionReceiptCopy(s, projected.state);
        var label = receipt.label;
        var detail = receipt.detail;
        var title = detail ? label + " · " + detail : label;
        var count = projected.state === "work_complete" ? 2 : 1;
        var checks = "";
        for (var i = 0; i < count; i++) {
            checks += '<span class="session-work-check" aria-hidden="true"></span>';
        }
        // The receipt mark and the words it denotes are one status. Keep them in one flex item
        // so another independent axis (closeability, debt, or coordination) can only follow the
        // complete receipt and can never be inserted between its check and explanation.
        said = '<span class="session-work-completion" data-work-state="' + projected.state +
            '" data-acceptance="' + receipt.acceptance + '"><span class="session-work-mark" role="img" aria-label="' + attr(title) +
            '" title="' + attr(title) + '">' + checks + '</span><span class="session-work-copy"' +
            ' data-work-state="' + projected.state + '" aria-hidden="true">' + attr(title) +
            "</span></span>";
    } else if (projected.state === "ready" || projected.state === "holding") {
        // 📭 an empty, open box: you can hand this one work. 🔜 it moves by itself; nobody is
        // needed. Both are usually the session's own words, so the stated-not-proven marker
        // rides along whenever provenance says `self`.
        var icon = projected.state === "ready" ? "📭" : "🔜";
        var copy = (s && s.work_note) ||
            (projected.state === "ready" ? T.sessionWorkReady : T.sessionWorkHolding);
        if (s && s.work_provenance === "self") copy += " · " + T.sessionWorkSelfStated;
        said = '<span class="session-work-copy" data-work-state="' + projected.state +
            '" title="' + attr(copy) + '">' + sessionStatusGlyphHTML(icon, copy) + "</span>";
    } else if (projected.state === "unknown") {
        // An observed idle assistant tells us the terminal has stopped moving,
        // even when nobody reported whether the work was delivered. Say both
        // facts instead of making the whole row sound unreadable. An unknown
        // terminal reading still keeps the honest unknown label.
        var unknownCopy = T.sessionWorkUnknown;
        if (s && s.state === "idle" && !projected.failedClosed) {
            // A conversation record plus the observed idle terminal says a
            // turn has ended. Neither fact alone proves a delivered result.
            unknownCopy = s.activity && s.activity.known
                ? T.sessionTurnEndedUnreported : T.sessionWorkIdleUnreported;
        }
        said = '<span class="session-work-copy" data-work-state="unknown" title="' +
            attr(unknownCopy) + '">' + attr(unknownCopy) + "</span>";
    }
    return said + owedBadgeHTML(s);
}


/* ---- dispatched work ----------------------------------------------------
   A task is a session the app started because another session asked it to.
   Everything here reads `S.tasks` and nothing writes it, so an app with no
   orchestrator — an empty list, or a 404 swallowed on the way in — leaves
   every function below answering exactly what it answered before the feature
   existed. That is the whole degradation story, and it is why these are
   separate functions rather than lines inside the render.
   -------------------------------------------------------------------------- */

/** Still going. The three the app spends children on; everything else is over. */
export function taskLive(t) {
    return !!t && (t.state === "queued" || t.state === "spawning" || t.state === "briefed");
}

/**
 * Whether a task still gets to say where a row sits.
 *
 * A finished task does, for as long as the child's terminal is still open. The app closes that
 * tab on its own schedule (or leaves it, if told to), and while it is there it belongs under the
 * root that asked for it — a row that jumps out from under its root at the exact moment somebody
 * looks over to read the result turns "where did that go" into a scroll through the list. Once
 * the tab is gone there is no row to hold, and the task is history.
 */
export function taskShaping(t) {
    if (taskLive(t)) return true;
    if (!t || !t.finishedAt || !t.child || !t.child.terminalId) return false;
    var id = t.child.terminalId;
    return S.sessions.some(function (s) { return s.id === id; });
}

/** The task a session is the child of, live or long over — what the header reads. The freshest
 *  one wins: a terminal id is reused, and the interesting task is never the older one. */
export function taskOfChild(id) {
    var best = null;
    for (var i = 0; i < S.tasks.length; i++) {
        var t = S.tasks[i];
        if (!t || !t.child || t.child.terminalId !== id) continue;
        if (!best) { best = t; continue; }
        if (taskLive(t) && !taskLive(best)) { best = t; continue; }
        if (taskLive(best) && !taskLive(t)) continue;
        if ((t.created || 0) >= (best.created || 0)) best = t;
    }
    return best;
}

/** A broker-proved independent Feature Root. Closed on ownership so a random object cannot
 * acquire root styling, and separate from taskOfChild so it can never gain child indentation. */
export function rootAssignmentOf(session) {
    var value = session && session.root_assignment;
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    if (value.ownership !== "independent_root" || typeof value.id !== "string" || !value.id ||
        typeof value.label !== "string" || !value.label) return null;
    return value;
}

/** The tasks a session is the root of, of the ones still shaping the list. */
export function tasksOfRoot(id) {
    return S.tasks.filter(function (t) {
        return taskShaping(t) && t.root && t.root.terminalId === id;
    });
}

/** The executable Feature Root chip decision. The chip has the same compact count grammar as an
 * ordinary task root; the durable assignment label remains in the row title and this tooltip
 * instead of being repeated inside a narrow badge. Terminal assignment states are refused here
 * as a second closed boundary even though the broker normally omits those projections. */
export function featureRootChip(session) {
    var root = rootAssignmentOf(session);
    if (!root || root.state === "failed" || root.state === "inactive") return null;
    var live = tasksOfRoot(session.id).filter(taskLive);
    var text = T.webTaskRoot + " · " + live.length;
    var title = root.label + " · " + root.state;
    if (live.length) {
        title += "\n" + T.webTaskTasks + ": " + live.map(function (task) {
            return task.title || task.id;
        }).join(" · ");
    }
    return { text: text, title: title, live: true, childCount: live.length };
}

/**
 * What closing a session would take with it, from the facts this page already holds: its live
 * child tasks, and the sessions parked on files it owns. Shown inside the close confirmation —
 * at the moment of the press, never as a list column — and the server recomputes the same list
 * at its end of the same press, so a page holding a stale frame cannot close past it blindly.
 */
export function lostIfClosed(id) {
    // A wait callback outlives its root: the daemon stops only heavy ones at
    // a close, so a wait is not something the close takes away.
    var lost = tasksOfRoot(id).filter(function (t) {
        return taskLive(t) && t.callback_intent !== "wait";
    }).map(function (t) { return t.title || t.id; });
    var s = byId(id);
    var waiters = ((s && s.coordination && s.coordination.waitedOnBy) || []).length;
    if (waiters === 1) lost.push(T.sessionWaitedOnByOne);
    else if (waiters) lost.push(fill(T.sessionWaitedOnByMany, { n: waiters }));
    return lost;
}

/** One word for where a task got to, in the reader's language. Cancelled and timed out are
 *  filed under failed on purpose: the row has one line, and neither of them succeeded. */
export function taskWord(t) {
    if (taskLive(t)) return T.webTaskRunning;
    return t && t.state === "success" ? T.webTaskDone : T.webTaskFailed;
}

export function byId(id) {
    if (id && typeof id === "object") {
        var explicit = sessionSelectionKey(id);
        return explicit ? S.sessions.find(function (row) {
            return sessionSelectionKey(row) === explicit;
        }) || null : null;
    }
    var exact = S.sessions.filter(function (row) { return sessionSelectionKey(row) === id; });
    if (exact.length === 1) return exact[0];
    var bare = S.sessions.filter(function (row) { return row.id === id; });
    if (bare.length === 1) return bare[0];
    // A duplicate bare id has no authority. Callers that already own an exact selection pass its
    // key or immutable identity; this general lookup never consults another mutable singleton to
    // guess which machine the string meant.
    return null;
}

/** The open session writing to a given transcript, or nothing.
 *
 *  A different id from the one above: `id` is the terminal's — the tab, the pane — and
 *  `sessionId` is Claude Code's own, the name of the file it is writing. Only the second one
 *  can be matched against a conversation somebody picked off a list of recorded ones, and it
 *  is absent on a session the Mac has not managed to tie to a transcript yet. So `null` here
 *  means "cannot say", never "not open", and every caller has to be able to live with that. */
export function bySessionId(id) {
    if (!id) return null;
    for (var i = 0; i < S.sessions.length; i++) {
        if (S.sessions[i].sessionId === id) return S.sessions[i];
    }
    return null;
}
