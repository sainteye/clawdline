import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { renderList } from "./list.js";

/* ==========================================================================
   6. Deriving what to show
   ========================================================================== */

var RANK = { waiting: 0, working: 1, idle: 2, unknown: 3 };

function rankOf(s) { return RANK[s.state] == null ? 9 : RANK[s.state]; }

function coordinatorSession(s) {
    var value = s && s.coordinator;
    return !!value && typeof value === "object" && !Array.isArray(value);
}

export function coordinatorFirst(a, b) {
    return (coordinatorSession(a) ? 0 : 1) - (coordinatorSession(b) ? 0 : 1);
}

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
 * The browser repeats the safety precedence so an old or partial server frame cannot turn a
 * question, peer wait, or unreadable screen into a check. Every other missing/unknown value is
 * one explicit state too: unknown — the broker's honest absence — never an empty line.
 */
export function projectSessionWorkState(s) {
    s = s || {};
    if (s.state === "waiting") return { state: "waiting_you", failedClosed: false };
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
    if ((s.state === "working") !== (s.work_state === "working")) {
        return { state: "unknown", failedClosed: true };
    }
    if (s.work_state === "waiting_session") {
        return tasksOfRoot(s.id).some(taskLive)
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
        if (!taskMilestone && !sessionMilestone && !taskClosure) {
            return { state: "unknown", failedClosed: true };
        }
    }
    return { state: s.work_state, failedClosed: false };
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

/** The opaque CAS token a proven close hands back, and only when the client itself agrees the
 *  frame says safe. A page that failed the projection closed sends nothing and gets the
 *  unchanged gate. */
export function closeabilityVersion(s) {
    var projected = projectSessionCloseability(s);
    if (projected.state !== "safe") return null;
    var version = projected.block && projected.block.version;
    return typeof version === "string" && version ? version : null;
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
    } else copy = T.closeabilityUnknown;
    var mover = projected.block && projected.block.mover;
    var moverSaid = projected.state === "safe" ? "" : closeabilityMoverText(mover);
    var title = moverSaid ? copy + " · " + moverSaid : copy;
    var icon = CLOSEABILITY_ICON[projected.state];
    return '<span class="session-closeability" data-closeability="' + projected.state +
        '" title="' + attr(title) + '">' + attr(icon ? icon + " " + copy : copy) + "</span>";
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
export function owedBadgeHTML(s) {
    var owed = s && s.owed;
    if (!owed || typeof owed !== "object" || Array.isArray(owed)) return "";
    var copy = "📥 " + (owed.note || T.sessionWorkOwed);
    var age = owedAge(owed.since);
    if (age) copy += " · " + age;
    return '<span class="session-work-owed" data-person-needed="' +
        (owed.person_needed === false ? "no" : "yes") + '" title="' + attr(copy) + '">' +
        attr(copy) + "</span>";
}

/** Check glyphs are CSS strokes, not a platform emoji. The quiet states carry their meaning in
 *  an icon a phone can scan — except `unknown`, which is an absence, not a category: giving an
 *  absence a symbol is how needs_triage came to read as a demand, so it deliberately has none. */
export function sessionWorkStateHTML(s) {
    var projected = projectSessionWorkState(s);
    var said = "";
    if (projected.state === "milestone_complete" || projected.state === "work_complete") {
        var label = projected.state === "work_complete"
            ? T.sessionWorkComplete : T.sessionWorkMilestone;
        var detail = s && s.disposition && s.disposition.title;
        var title = detail ? label + " · " + detail : label;
        var count = projected.state === "work_complete" ? 2 : 1;
        var checks = "";
        for (var i = 0; i < count; i++) {
            checks += '<span class="session-work-check" aria-hidden="true"></span>';
        }
        said = '<span class="session-work-mark" role="img" aria-label="' + attr(label) +
            '" title="' + attr(title) + '">' + checks + "</span>";
    } else if (projected.state === "ready" || projected.state === "holding") {
        // 📭 an empty, open box: you can hand this one work. 🔜 it moves by itself; nobody is
        // needed. Both are usually the session's own words, so the stated-not-proven marker
        // rides along whenever provenance says `self`.
        var icon = projected.state === "ready" ? "📭" : "🔜";
        var copy = (s && s.work_note) ||
            (projected.state === "ready" ? T.sessionWorkReady : T.sessionWorkHolding);
        if (s && s.work_provenance === "self") copy += " · " + T.sessionWorkSelfStated;
        said = '<span class="session-work-copy" data-work-state="' + projected.state +
            '" title="' + attr(copy) + '">' + attr(icon + " " + copy) + "</span>";
    } else if (projected.state === "unknown") {
        said = '<span class="session-work-copy" data-work-state="unknown" title="' +
            attr(T.sessionWorkUnknown) + '">' + attr(T.sessionWorkUnknown) + "</span>";
    }
    return said + owedBadgeHTML(s);
}

/**
 * The order is held still while the pointer is inside the list.
 *
 * A list that sorts itself is a list that can move a row out from under a click, and the click
 * still lands — on whatever slid into its place. Holding the order costs nothing while somebody
 * is reading, and it is given up the moment the set of waiting sessions changes: a new question
 * outranks a steady list, and getting to the top is the entire point of that state.
 */
var hold = null;

function waitingKey() {
    return S.sessions.filter(function (s) { return s.state === "waiting"; })
        .map(function (s) { return s.id; }).sort().join(",");
}
export function freezeOrder() {
    if (!hold) hold = { order: ordered().map(function (s) { return s.id; }), waiting: waitingKey() };
}
export function thawOrder() {
    if (!hold) return;
    hold = null;
    renderList();
}

/** Waiting first, always. The list exists to answer "which one stopped and wants me", and
 *  an answer that is third from the bottom is not one. Within a state, alphabetical, so a
 *  row that has not changed does not move. */
export function ordered() {
    if (hold && hold.waiting !== waitingKey()) hold = null;
    var q = S.filter.trim().toLowerCase();
    var list = S.sessions.filter(function (s) {
        if (!q) return true;
        return ((s.label || "") + " " + (s.cwd || "") + " " + (s.tty || "") + " " + (s.backend || ""))
            .toLowerCase().indexOf(q) >= 0;
    });
    if (hold) {
        var at = {};
        hold.order.forEach(function (id, i) { at[id] = i; });
        return grouped(list.sort(function (a, b) {
            var ai = at[a.id] == null ? 1e9 : at[a.id], bi = at[b.id] == null ? 1e9 : at[b.id];
            return coordinatorFirst(a, b) || (ai - bi) || (rankOf(a) - rankOf(b))
                || (a.label || "").localeCompare(b.label || "");
        }));
    }
    return grouped(list.sort(function (a, b) {
        return coordinatorFirst(a, b)
            || (rankOf(a) - rankOf(b))
            || (a.label || "").localeCompare(b.label || "")
            || (a.id < b.id ? -1 : 1);
    }));
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

/**
 * How far a row is indented: one step for the session that asked for it, another if that session
 * is somebody's child too. Two is the floor, matching the app — and it stops early at a link
 * pointing off screen, because an indent is a claim about the row above and there isn't one.
 */
export function rowDepth(id) {
    if (coordinatorSession(byId(id))) return 0;
    var n = 0, at = id, seen = {};
    while (n < 2) {
        var t = taskOfChild(at);
        if (!t || !taskShaping(t) || !t.root || !t.root.terminalId) break;
        var up = t.root.terminalId;
        if (up === at || seen[up] || !byId(up)) break;
        seen[at] = true;
        at = up;
        n++;
    }
    return n;
}

/** The tasks a session is the root of, of the ones still shaping the list. */
export function tasksOfRoot(id) {
    return S.tasks.filter(function (t) {
        return taskShaping(t) && t.root && t.root.terminalId === id;
    });
}

/**
 * What closing a session would take with it, from the facts this page already holds: its live
 * child tasks, and the sessions parked on files it owns. Shown inside the close confirmation —
 * at the moment of the press, never as a list column — and the server recomputes the same list
 * at its end of the same press, so a page holding a stale frame cannot close past it blindly.
 */
export function lostIfClosed(id) {
    var lost = tasksOfRoot(id).filter(taskLive).map(function (t) { return t.title || t.id; });
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

/**
 * The children, pulled up under the sessions that asked for them.
 *
 * Done after the sort rather than inside it, because it is not a sort: a child sits under its
 * root wherever the root landed, whatever state the child is in. A waiting child still reaches
 * the top of the list — it is carried there by its root, which is where somebody looking for it
 * would start.
 *
 * A child whose root is not on screen — filtered out, or a session that has since closed — is
 * left exactly where the sort put it. It is still somebody's session, and hiding it or floating
 * it to the top would be this feature deciding something it was not asked to decide.
 */
function grouped(list) {
    if (!S.tasks.length || list.length < 2) return list;

    var childOf = {};                       // child row id → the root row it belongs under
    var here = {}, sessionsByID = {};
    list.forEach(function (s) { here[s.id] = true; sessionsByID[s.id] = s; });
    S.tasks.forEach(function (t) {
        if (!taskShaping(t) || !t.child || !t.root) return;
        var kid = t.child.terminalId, root = t.root.terminalId;
        if (!kid || !root || kid === root) return;
        if (!here[kid] || !here[root]) return;
        if (coordinatorSession(sessionsByID[kid])) return;
        childOf[kid] = root;
    });
    // Two levels, the same floor the app dispatches to. A link that would put a row deeper than
    // that, or that leads round in a circle, is a record this page has no business believing:
    // the link is dropped and the row stands on its own rather than being threaded onto a chain
    // nobody can follow. Depths are read off the original links in one pass, so a broken chain
    // costs its own rows their indent and never anybody else's.
    Object.keys(childOf).forEach(function (kid) {
        var n = 0, at = kid, seen = {};
        while (childOf[at] && n <= 2) {
            if (seen[at]) { n = 99; break; }
            seen[at] = true;
            at = childOf[at];
            n++;
        }
        if (n > 2) delete childOf[kid];
    });
    var moved = Object.keys(childOf);
    if (!moved.length) return list;

    var kids = {};
    list.forEach(function (s) {
        var root = childOf[s.id];
        if (root) (kids[root] || (kids[root] = [])).push(s);
    });
    var out = [];
    var place = function (s) {
        out.push(s);
        if (kids[s.id]) kids[s.id].forEach(place);
    };
    list.forEach(function (s) { if (!childOf[s.id]) place(s); });
    // Nothing this does is worth a row going missing. If the count moved, the grouping was
    // wrong about something and the ungrouped list is the honest answer.
    return out.length === list.length ? out : list;
}

export function byId(id) {
    for (var i = 0; i < S.sessions.length; i++) if (S.sessions[i].id === id) return S.sessions[i];
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

/** What has to be the same for a transcript to still be current. The line is in it because a
 *  session that is still working is still writing. */
export function revisionOf(s) { return s ? s.state + "|" + (s.line || "") + "|" + (s.label || "") : ""; }
