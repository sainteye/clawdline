import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { renderList } from "./list.js";

/* ==========================================================================
   6. Deriving what to show
   ========================================================================== */

var RANK = { waiting: 0, working: 1, idle: 2, unknown: 3 };

function rankOf(s) { return RANK[s.state] == null ? 9 : RANK[s.state]; }

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
            return (ai - bi) || (rankOf(a) - rankOf(b)) || (a.label || "").localeCompare(b.label || "");
        }));
    }
    return grouped(list.sort(function (a, b) {
        return (rankOf(a) - rankOf(b))
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

/** The tasks a session is the root of, of the ones still shaping the list. */
export function tasksOfRoot(id) {
    return S.tasks.filter(function (t) {
        return taskShaping(t) && t.root && t.root.terminalId === id;
    });
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
    var here = {};
    list.forEach(function (s) { here[s.id] = true; });
    S.tasks.forEach(function (t) {
        if (!taskShaping(t) || !t.child || !t.root) return;
        var kid = t.child.terminalId, root = t.root.terminalId;
        if (!kid || !root || kid === root) return;
        if (!here[kid] || !here[root]) return;
        childOf[kid] = root;
    });
    // A root that is itself somebody's child cannot carry anyone: the app refuses to dispatch
    // that deep, so it means a stale record, and moving a row under a row that is itself moving
    // is how a list loses one. Depth stays one, here as well as over there.
    Object.keys(childOf).forEach(function (kid) {
        if (childOf[childOf[kid]]) delete childOf[kid];
    });
    var moved = Object.keys(childOf);
    if (!moved.length) return list;

    var kids = {};
    list.forEach(function (s) {
        var root = childOf[s.id];
        if (root) (kids[root] || (kids[root] = [])).push(s);
    });
    var out = [];
    list.forEach(function (s) {
        if (childOf[s.id]) return;
        out.push(s);
        if (kids[s.id]) kids[s.id].forEach(function (k) { out.push(k); });
    });
    // Nothing this does is worth a row going missing. If the count moved, the grouping was
    // wrong about something and the ungrouped list is the honest answer.
    return out.length === list.length ? out : list;
}

export function byId(id) {
    for (var i = 0; i < S.sessions.length; i++) if (S.sessions[i].id === id) return S.sessions[i];
    return null;
}

/** What has to be the same for a transcript to still be current. The line is in it because a
 *  session that is still working is still writing. */
export function revisionOf(s) { return s ? s.state + "|" + (s.line || "") + "|" + (s.label || "") : ""; }
