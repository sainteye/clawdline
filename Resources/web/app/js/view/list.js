import { phone, reduced } from "../core/env.js";
import { esc } from "../core/esc.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { shortPath, tint } from "../core/util.js";
import { ASSISTANT_LOGOS, assistantLogo, assistantName, drawIcon, drawSpinner, setSpinners, spinPhase, spinners } from "../core/pixels.js";
import { byId, featureRootChip, ordered, projectSessionCloseability, projectSessionWorkState, revisionOf, rowDepth, sessionCloseabilityHTML, sessionCloseabilityShape, sessionStatusGlyphHTML, sessionWorkStateHTML, taskLive, taskOfChild, taskShaping, taskWord, tasksOfRoot } from "./derive.js";
import { renderDetailHead } from "./transcript.js";
import { renderAgents, renderComposer, renderWaiting } from "./composer.js";
import { Optimistic, Waits, drawListSkeleton, listUnknown } from "./waits.js";
import {
    closeDetail, observeTranscriptRevision, openSession, rearmTranscriptRevision
} from "../session/open.js";
import { agentRow, agentsRev, loadAgent, renderAgentHead } from "../session/agent.js";
import { SwipeRows } from "../input/swipe.js";
import { SessionActions } from "../input/detail-actions.js";
import { openWanted, setWantedSession, wantedSession } from "../input/route.js";
import { Start } from "../input/start.js";
import { StatusLine } from "../input/status-line.js";
import { Info } from "../input/info.js";
import {
    CoordinatorControls,
    coordinatorRoute,
    coordinatorRowModel
} from "../input/coordinator-actions.js";

/* ==========================================================================
   7. Render
   Every path in here is a full render from S. The list keeps its DOM nodes
   between renders — partly so a row can animate to its new place, partly so
   the selected row does not lose focus every time the stream breathes.
   ========================================================================== */

export var rowNodes = {};       // id → live <li>
var leavingNodes = {};   // id → detached-from-the-list-order <li>
// An end request has already crossed the confirmation boundary. Kept outside `S.sessions`
// because it is browser-side progress rather than a fact from the stream; the stream may remove
// the session before the request that caused it has made the osascript round trip back.
export var closingID = null;

/// Set from `SessionActions`, which is what ends a session, and a name arriving there by import is
/// read-only. Same variable, one more hop.
export function setClosingID(id) { closingID = id; }

var firstList = true;

export function onSessions() {
    var open = S.openId ? byId(S.openId) : null;

    // Anything that has just stopped gets a pulse, and whatever is open gets refetched.
    S.sessions.forEach(function (s) {
        var was = S.seen[s.id];
        // Working to idle, and only that. A session that stops to ask a question has not
        // finished — it has started shouting, and the row is already doing that.
        if (was && was.state === "working" && s.state === "idle") {
            var node = rowNodes[s.id];
            if (node && !reduced) {
                node.classList.remove("finished");
                void node.offsetWidth;              // restart the animation rather than let it be ignored
                node.classList.add("finished");
                setTimeout(function () { node.classList.remove("finished"); }, 1600);
            }
        }
        if (open && s.id === open.id) {
            // `handlers.sessions` runs before the first accepted frame marks the connection live.
            // That one frame is a real reconnect boundary and may open a new bounded failure
            // burst. Ordinary live frames only observe, so replaying one snapshot cannot loop.
            var revision = revisionOf(s);
            if (S.conn === "live") observeTranscriptRevision(s.id, revision, true);
            else rearmTranscriptRevision(s.id, revision, true);
        }
        // An agent being read has its own reason to refetch, and the session's revision cannot
        // give it: a session sitting between turns with three agents out looks unchanged the
        // whole time they work. `agentsRev` moves when any of them writes a line.
        if (S.agent && S.agent.sid === s.id) {
            var mine = agentRow(s.id, S.agent.id);
            if (mine) { S.agent.meta = mine; renderAgentHead(); }
            if (!was || was.agents !== agentsRev(s)) loadAgent(s.id, S.agent.id, true);
        }
        S.seen[s.id] = { state: s.state, rev: revisionOf(s), agents: agentsRev(s) };
    });

    // A session that has gone away takes its row, and its transcript, with it.
    Object.keys(S.seen).forEach(function (id) { if (!byId(id)) delete S.seen[id]; });
    if (S.selectedId && !byId(S.selectedId)) S.selectedId = null;
    if (S.openId && !byId(S.openId)) {
        // The stream is the stronger answer: if the row vanished while its end request is still
        // in flight, the session is already closed. Let the visible wait finish naturally and
        // make the later HTTP reply harmless instead of tearing the confirmation away mid-spin.
        if (closingID === S.openId) SessionActions.gone(S.openId);
        else closeDetail(true);
    }

    render();

    // A session that was started from this page, as soon as the list has it. Before `openWanted`
    // for the same reason it is here at all: both of them are "somebody asked for this session",
    // and this one asked most recently.
    var started = Start.check();

    // A session the URL asked for — a tapped notification — as soon as it is in the list. It is
    // tried on every list rather than once, because a cold start routes before it knows what
    // sessions exist.
    var routed = openWanted();

    // The first list to arrive puts the highlight somewhere, so the arrow keys and Return mean
    // something without a click first. On a desk it also opens that session: a two-column layout
    // whose second column is empty on arrival is a layout apologising for itself. On a phone it
    // does not — there, opening is a whole screen, and nobody asked for it yet.
    if (firstList && S.sessions.length) {
        firstList = false;
        // Not when somebody asked for a particular one. Opening the top of the list first and
        // theirs a moment later is two screens for one tap, and the first of them is wrong.
        // A Mac with nothing running is where this matters: the first list to arrive there can
        // be the one carrying the session that was just started from this page.
        if (!routed && !wantedSession && !started) {
            var top = ordered()[0];
            if (top) { if (phone()) { S.selectedId = top.id; render(); } else { openSession(top.id, true); } }
        }
        // The first list is the whole list, so a session that is not in it is gone — a
        // notification about a tab somebody has since closed. Let go of it rather than hold the
        // default open hostage to a session that is never coming.
        setWantedSession(null);
    }
}

export function render() {
    renderCounts();
    renderList();
    renderDetailHead();
    // After the session's header and before the composer, because it hides both of them when an
    // agent is being read, and hiding a thing that has just been drawn is cheaper than drawing
    // one that is about to be hidden.
    renderAgentHead();
    renderComposer();
    // The Session info card's buttons depend on whether the session is idle.
    if (typeof Info === "object" && Info) Info.follow();
    if (typeof StatusLine === "object" && StatusLine) StatusLine.follow();
    renderWaiting();
    renderAgents();
}

export function renderConn() {
    var label = { live: T.webConnLive, connecting: T.webConnConnecting, offline: T.webConnOffline,
                  locked: T.webConnLocked,
                  retrying: fill(T.webConnRetrying, { n: S.retryIn }) }[S.conn] || S.conn;
    els.conn.dataset.state = S.conn;
    els["conn-label"].textContent = label;
    els.conn.title = S.conn === "live"
        ? T.webConnTipLive + (S.version ? " · " + S.version : "")
        : S.conn === "locked" ? T.webConnTipLocked
        : T.webConnTipDown;
}

function renderCounts() {
    var working = 0, waiting = 0, unknown = 0, shells = 0;
    S.sessions.forEach(function (s) {
        if (s.state === "working") working++;
        else if (s.state === "waiting") waiting++;
        else if (s.state === "unknown") unknown++;
        shells += ((s && s.shells) || []).length;
    });
    var bits = [];
    if (working) bits.push('<span class="part">' + esc(fill(T.webCountWorking, { n: working })) + "</span>");
    if (waiting) bits.push('<span class="part waiting">' + esc(fill(T.webCountWaiting, { n: waiting })) + "</span>");
    // **Counted here so that "all quiet" cannot be said over a build.** It is the quietest part of
    // the line, and it is still the difference between a fleet that has finished and one that has
    // not — which is the whole question this line is asked.
    if (shells) bits.push('<span class="part quiet">' +
        esc(shells === 1 ? T.sessionShellOne : fill(T.sessionShellMany, { n: shells })) + "</span>");
    if (unknown) bits.push('<span class="part quiet">' + esc(fill(T.webCountUnreadable, { n: unknown })) + "</span>");
    if (!bits.length) {
        var quiet = S.sessions.length
            ? fill(S.sessions.length === 1 ? T.webCountQuietOne : T.webCountQuietMany, { n: S.sessions.length })
            : T.webCountNone;
        bits.push('<span class="part quiet">' + esc(quiet) + "</span>");
    }
    els.counts.innerHTML = bits.join("");
}

/**
 * Keep a removed row where it was visibly sitting while the live rows take their final places.
 * Taking it out of flow first means the ordinary FLIP moves its neighbours all the way to their
 * destination; the old row can then leave on top without being appended to a different position
 * or remaining a keyboard and pointer target after its session is gone.
 */
function leaveRow(id, node, seen, rowsBox) {
    if (node.flipAnim) { node.flipAnim.cancel(); node.flipAnim = null; }
    if (node.enterFadeAnim) { node.enterFadeAnim.cancel(); node.enterFadeAnim = null; }
    node.classList.remove("entering", "finished");
    node.classList.add("leaving");
    node.setAttribute("aria-hidden", "true");
    node.tabIndex = -1;
    node.style.position = "absolute";
    node.style.top = (seen.top - rowsBox.top) + "px";
    node.style.left = (seen.left - rowsBox.left - seen.marginLeft) + "px";
    node.style.width = seen.width + "px";
    node.style.opacity = seen.opacity;
    node.style.transform = "none";
    leavingNodes[id] = node;

    function finish() {
        if (leavingNodes[id] !== node) return;
        if (node.parentNode) node.parentNode.removeChild(node);
        delete leavingNodes[id];
    }

    node.leaveAnim = node.animate(
        [{ opacity: seen.opacity, transform: "none" },
         { opacity: 0, transform: "translateY(-4px) scale(.97)" }],
        { duration: 190, easing: "cubic-bezier(.4, 0, 1, 1)" }
    );
    node.leaveAnim.onfinish = finish;
    node.leaveAnim.oncancel = finish;
}

function discardLeaving(id) {
    var node = leavingNodes[id];
    if (!node) return;
    delete leavingNodes[id];
    if (node.leaveAnim) node.leaveAnim.cancel();
    if (node.parentNode) node.parentNode.removeChild(node);
}

export function renderList() {
    if (typeof SwipeRows === "object" && SwipeRows && (!phone() || !S.write)) SwipeRows.reset(true);
    // Nothing has ever arrived and the wait has gone on long enough to be worth drawing. No rows
    // are built: there are none to build, and the skeleton stands in for the empty state rather
    // than sitting beside it.
    if (Waits.list.visible) { drawListSkeleton(); return; }

    var list = Start.arrange(ordered());

    // FLIP, first half: both where every row is visibly sitting and where layout put it before
    // the DOM is touched. The distinction matters while a previous FLIP is still running: a
    // stream frame that does not change layout must leave that animation alone, while one that
    // does change it starts again from the visible position rather than snapping to the old end.
    var before = {};
    if (!reduced) {
        Object.keys(rowNodes).forEach(function (id) {
            var node = rowNodes[id];
            var box = node.getBoundingClientRect();
            var style = getComputedStyle(node);
            before[id] = {
                top: box.top, left: box.left, width: box.width,
                layoutTop: node.offsetTop, opacity: style.opacity,
                marginLeft: parseFloat(style.marginLeft) || 0
            };
        });
    }

    setSpinners([]);
    var wanted = {};
    list.forEach(function (s) { wanted[s.id] = true; });

    // A leaving row has to be taken out of flow before live nodes are appended into their new
    // order. Otherwise appendChild moves the obsolete node to an arbitrary slot, so it fades
    // from somewhere it was never seen and the live rows animate to positions that are already
    // out of date.
    var rowsBox = !reduced ? els.rows.getBoundingClientRect() : null;
    Object.keys(rowNodes).forEach(function (id) {
        if (wanted[id]) return;
        var node = rowNodes[id];
        delete rowNodes[id];
        if (!node.parentNode) return;
        if (reduced) node.parentNode.removeChild(node);
        else leaveRow(id, node, before[id], rowsBox);
    });

    list.forEach(function (s) {
        var node = rowNodes[s.id];
        if (!node) {
            discardLeaving(s.id);
            node = buildRow(s);
            rowNodes[s.id] = node;
            if (!reduced && !Start.arriving(s.id)) {
                node.classList.add("entering");
                setTimeout(function (n) { return function () { n.classList.remove("entering"); }; }(node), 300);
            }
        }
        fillRow(node, s);
        // appendChild moves a node that is already here, which is what makes this a reorder
        // rather than a rebuild — the element under the pointer stays the element under the pointer.
        els.rows.appendChild(node);
    });

    // Not a session and deliberately not in `rowNodes`: it cannot be selected, filtered or
    // counted. It lives in the same container solely so the real row replaces the same shape at
    // the top, instead of arriving as an unexplained reorder after the band has been spinning.
    var starting = Start.placeholder();
    if (starting) els.rows.insertBefore(starting, els.rows.firstChild);

    // FLIP, second half: start each row where it used to be and let it travel to where it now is.
    //
    // Driven by the animation API rather than by writing a transform and taking it away again.
    // This list re-renders on every breath of the stream, and pinning through inline styles means
    // a pin and its release can land in the same style recalculation — the browser is entitled to
    // see only the end of that and animate nothing, which is how rows came to sit at the position
    // they held two updates ago. An animation object cannot be coalesced away, and the one already
    // running is cancelled by name rather than by hoping it has finished.
    if (!reduced) {
        Object.keys(rowNodes).forEach(function (id) {
            var from = before[id];
            if (!from) return;
            var node = rowNodes[id];
            // The target did not move, so neither should an animation already travelling there.
            // Cancelling it on every one-second line update was the remaining way a row could
            // snap halfway through an otherwise correct reorder.
            if (from.layoutTop === node.offsetTop) return;
            // If entry and reorder overlap, preserve the opacity already reached and hand the
            // transform over to FLIP. Two animations replacing the same transform is not a
            // meaningful composite, and which one wins differs between WebKit and Chromium.
            if (node.classList.contains("entering")) {
                node.classList.remove("entering");
                node.style.opacity = from.opacity;
                node.enterFadeAnim = node.animate(
                    [{ opacity: from.opacity }, { opacity: 1 }],
                    { duration: 160, easing: "cubic-bezier(.2, .7, .2, 1)" }
                );
                node.enterFadeAnim.onfinish = function () {
                    node.style.opacity = "";
                    node.enterFadeAnim = null;
                };
            }
            if (node.flipAnim) node.flipAnim.cancel();
            // Cancellation exposes the new untransformed layout position. `from.top` was read
            // before it, so this delta resumes at the exact pixel that was on screen.
            var delta = from.top - node.getBoundingClientRect().top;
            if (Math.abs(delta) < 1) return;
            node.flipAnim = node.animate(
                [{ transform: "translateY(" + delta + "px)" }, { transform: "none" }],
                { duration: 240, easing: "cubic-bezier(.2, .7, .2, 1)" }
            );
        });
    }

    var empty = !list.length && !starting && !listUnknown();
    var homeEmpty = empty && !S.sessions.length && !S.filter;
    els["list-empty"].className = "empty" + (homeEmpty ? " home-hero-list" : "");
    els["list-empty"].hidden = !empty;
    if (empty) {
        // Four of them, and telling them apart is the whole job: nothing matches what was typed,
        // this browser was refused, there are genuinely no sessions, or nothing has arrived yet.
        // One of those is somebody's own doing and three are not.
        var says = S.sessions.length
            ? [fill(T.webEmptyFilterTitle, { q: S.filter }), T.webEmptyFilterHint]
            : (S.locked
                ? [T.webEmptyLockedTitle, T.webEmptyLockedHint]
                : S.conn === "live"
                    ? [T.noSession, T.webEmptyNoneHint]
                    : [T.webEmptyWaitTitle, T.webEmptyWaitHint]);
        els["list-empty"].innerHTML = "<b>" + esc(says[0]) + "</b>" + esc(says[1]);
    }
}

function buildRow(s) {
    var li = document.createElement("li");
    li.className = "row";
    li.setAttribute("role", "option");
    li.tabIndex = -1;
    li.innerHTML =
        '<span class="kid" hidden aria-hidden="true">└</span>' +
        '<canvas class="mark"></canvas>' +
        '<div class="title"><span class="label"></span><span class="who" hidden></span></div>' +
        '<div class="meta"><span class="path"></span><span class="tty"></span>' +
        '<span class="agents-chip" hidden><span class="dot"></span><span class="n"></span></span>' +
        '<span class="task-chip" hidden></span></div>' +
        '<div class="state"></div>' +
        '<button class="swipe-end" type="button" hidden>' + esc(T.webEndSession) + '</button>';
    li.addEventListener("click", function () {
        var current = li._session || s;
        if (closingID === current.id) return;
        if (coordinatorRoute(current, "row") === "session") openSession(current.id);
    });
    return li;
}

/**
 * Upgrade only a coordinator's existing canvas into a real button. An ordinary row keeps the
 * exact markup it had before this feature; if the optional record disappears, it gets that
 * canvas back and the coordinator-only badge is removed.
 */
function fillCoordinatorMark(node, s) {
    var model = coordinatorRowModel(s);
    var button = node.querySelector(".coordinator-mark");
    var mark = node.querySelector(".mark");
    var badge = node.querySelector(".coordinator-chip");
    var crown = node.querySelector(".clawdfather-crown");

    if (!model) {
        delete node.dataset.coordinator;
        if (badge && badge.parentNode) badge.parentNode.removeChild(badge);
        if (button && mark && button.parentNode) {
            button.parentNode.replaceChild(mark, button);
            button = null;
        }
        return mark;
    }

    node.dataset.coordinator = "1";
    if (!button) {
        // Every row currently has a canvas mark, but a future compact row must fail closed
        // instead of throwing while trying to upgrade a shape it does not understand.
        if (!mark || !mark.parentNode) {
            delete node.dataset.coordinator;
            return null;
        }
        button = document.createElement("button");
        button.className = "coordinator-mark";
        button.type = "button";
        button.setAttribute("aria-controls", "coordinator-controls");
        button.setAttribute("aria-expanded", "false");
        mark.parentNode.replaceChild(button, mark);
        button.appendChild(mark);
        button.addEventListener("click", function (event) {
            var current = node._session;
            if (!current || closingID === current.id ||
                coordinatorRoute(current, "mark") !== "controls") return;
            event.preventDefault();
            event.stopPropagation();
            CoordinatorControls.open(current, button, {
                connected: S.conn === "live", write: S.write === true
            });
        });
    }
    button.setAttribute("aria-haspopup", model.mark.ariaHaspopup);
    button.setAttribute("aria-label", model.mark.ariaLabel);
    button.title = model.mark.ariaLabel;

    if (!crown) {
        crown = document.createElement("span");
        crown.className = "clawdfather-crown";
        crown.setAttribute("aria-hidden", "true");
        button.appendChild(crown);
    }

    if (!badge) {
        badge = document.createElement("span");
        badge.className = "coordinator-chip";
        var meta = node.querySelector(".meta");
        var taskChip = meta.querySelector(".task-chip");
        meta.insertBefore(badge, taskChip);
    }
    badge.textContent = model.badge;
    badge.title = model.label;
    return mark;
}

function fillRow(node, s) {
    var closing = closingID === s.id;
    var closingVisible = closing && Waits.end.visible;
    var pending = Optimistic.entries(s.id).length > 0;
    node._session = s;
    node.dataset.id = s.id;
    node.dataset.state = s.state;
    if (closingVisible) node.dataset.closing = "1"; else delete node.dataset.closing;
    if (pending) node.dataset.pending = "1"; else delete node.dataset.pending;
    var coordination = s.coordination || {};
    var waitingOn = coordination.waitingOn || [];
    // The other side of the same relationship: sessions parked on this one. It is the half the
    // owner reads, and the half both lists used to read straight past — which left the one
    // person who has to act looking at a row indistinguishable from an unrelated session.
    var waitedOnBy = coordination.waitedOnBy || [];
    if (waitingOn.length) node.dataset.coordination = "waiting";
    else if (waitedOnBy.length) node.dataset.coordination = "owed";
    else delete node.dataset.coordination;
    node.classList.toggle("selected", s.id === S.selectedId);
    node.classList.toggle("open", s.id === S.openId);
    node.setAttribute("aria-selected", s.id === S.selectedId ? "true" : "false");
    node.setAttribute("aria-disabled", closing ? "true" : "false");

    var mark = fillCoordinatorMark(node, s);
    if (mark) {
        if (!drawIcon(mark, s.icon, 4)) mark.classList.add("none"); else mark.classList.remove("none");
    }

    var title = node.querySelector(".title");
    title.querySelector(".label").textContent = s.label || s.tty || s.id;
    // Tinted with the project's own colour, so two sessions in one project read as one project
    // before either title has been read. Waiting overrides it in CSS — that row is not about
    // which project it is.
    title.style.color = s.icon ? tint(s.icon.accent) : "";

    node.querySelector(".path").textContent = shortPath(s.cwd);
    node.querySelector(".tty").textContent = s.tty || s.backend || "";

    // Always name the assistant, exactly as the Mac list does. The project mark on the left says
    // which project; this product mark answers the independent question, Claude or Codex.
    var who = node.querySelector(".who");
    who.hidden = !ASSISTANT_LOGOS[s.assistant];
    who.innerHTML = who.hidden ? "" : assistantLogo(s.assistant) +
        "<span>" + esc(assistantName(s.assistant)) + "</span>";

    // How many agents this session has out. **A number and nothing else**, sitting with the path
    // and the tty rather than with the state: the state line answers "does this want me", and
    // background work never does. It is also the only place in the list that says anything the
    // terminal could not have been asked — an agent leaves no mark on a screen.
    var chip = node.querySelector(".agents-chip");
    var out = 0, list = s.agents || [];
    for (var a = 0; a < list.length; a++) if (list[a].state === "running") out++;
    chip.hidden = !out;
    if (out) chip.querySelector(".n").textContent = String(out);

    // Where this row sits in somebody's work: a session that was started for another one, or
    // one that did the starting. Both are drawn where the agent count is and in the same
    // register, because they answer the same kind of question — this is context about a row,
    // never a row asking for anything.
    //
    // Only the tasks still shaping the list count. A child whose task ended an hour ago is
    // just a session again, and a chip that never goes away is furniture.
    var featureRoot = featureRootChip(s);
    var task = S.tasks.length ? taskOfChild(s.id) : null;
    var kid = task && taskShaping(task) ? task : null;
    // The indent is a claim about the row above, so it is only drawn when that row is there —
    // and it is two steps for a session dispatched by a session that was itself dispatched.
    // The chip is a claim about this session, which is true whether or not its root is on
    // screen — a child whose root has closed is still a child, and still worth saying so.
    var under = kid ? rowDepth(s.id) : 0;
    var roots = S.tasks.length ? tasksOfRoot(s.id) : [];
    var titles = function () {
        return T.webTaskTasks + ": " + roots.map(function (t) { return t.title || t.id; }).join(" · ");
    };
    var mine = node.querySelector(".task-chip");
    var glyph = node.querySelector(".kid");
    if (kid) {
        if (under) node.dataset.depth = String(under); else delete node.dataset.depth;
        glyph.hidden = !under;
        mine.hidden = false;
        mine.dataset.live = taskLive(kid) ? "1" : "0";
        // A child that handed work on in turn stays drawn as a child: the rows indented under it
        // are the visible half, and a chip saying both would say neither in the width it has.
        // What it sent away goes in the same tooltip as its own task.
        mine.textContent = T.webTaskChild + " · " + taskWord(kid);
        mine.title = [kid.title || "", roots.length ? titles() : ""]
            .filter(Boolean).join("\n");
    } else {
        delete node.dataset.depth;
        glyph.hidden = true;
        if (featureRoot) {
            mine.hidden = false;
            mine.dataset.live = featureRoot.live ? "1" : "0";
            mine.textContent = featureRoot.text;
            mine.title = featureRoot.title;
        } else if (roots.length) {
            mine.hidden = false;
            mine.dataset.live = roots.some(taskLive) ? "1" : "0";
            mine.textContent = T.webTaskRoot + " · " + roots.length;
            mine.title = titles();
        } else {
            mine.hidden = true;
            mine.textContent = "";
            mine.title = "";
        }
    }

    // The state line is rebuilt only when the *shape* of it changes. A working session sends a
    // new line every second, and throwing away the spinner's canvas to write one string would
    // restart the animation once a second — which is exactly the thing that should look steady.
    var state = node.querySelector(".state");
    // A local turn is a fact about the trip to the Mac, not a replacement for the state the
    // Mac reported. Keep `data-state` untouched and give the transient trip its own shape; a
    // waiting row keeps its louder request for attention alongside the quieter delivery note.
    // **The kind of line this is, and the cache key for it, are two different strings.** They used
    // to be one, and folding the shell count into it broke the case it was not about: `kind` is
    // what the branches below switch on, `shape` is only ever compared with the last one drawn.
    // A row that says `working+sh1` is still a working row, and a spinner it decided not to draw
    // is a canvas left at the 300×150 an undrawn canvas defaults to — which is a rectangle of
    // nothing the height of three rows, sitting in the middle of the list.
    var kind = closingVisible ? "closing" : (pending ? "pending" : s.state);
    // What this session left running where nobody can see it — see `Shells` on the Mac. It is
    // part of the *shape* because it is part of the markup: a row whose build finished while the
    // page was open has to lose the line, and a row that starts one has to grow it.
    var shells = ((s && s.shells) || []).length;
    var shellsSaid = !shells ? "" : '<span class="shells">' +
        esc(shells === 1 ? T.sessionShellOne : fill(T.sessionShellMany, { n: shells })) + "</span>";
    // A peer wait is an overlay, never the terminal state. It must not enter the loud waiting
    // branch above: that branch means a person has to answer and is what drives push alerts.
    var peerWait = waitingOn[0] || null;
    var owedWait = waitedOnBy[0] || null;
    // What this session is holding up, said the way the Mac's row says it: a count in the app's
    // own words rather than a name. The owner is not going to go and ask anybody — the owner
    // releases — so the useful facts are how many that would free and what would free them.
    //
    // No `+N` beside the count, unlike the waiting line: there the leading field names one peer
    // and the suffix counts the ones it had no room to name, while here the leading field is
    // already all of them and a suffix would count the same sessions twice.
    // One and many are two sentences wherever the verb agrees with the count, so the count
    // picks the key before it is filled in. See `sessionWaitedOnByOne` in `Sources/Strings.swift`.
    var owedSaid = "";
    if (owedWait) {
        owedSaid = waitedOnBy.length === 1
            ? T.sessionWaitedOnByOne
            : fill(T.sessionWaitedOnByMany, { n: waitedOnBy.length });
    }
    var peerText = "";
    var peerTitle = "";
    if (peerWait) {
        var owner = peerWait.ownerLabel || peerWait.ownerSessionId || "Clawdline";
        peerText = owner + " · " + (peerWait.releaseCondition || "release");
        if (waitingOn.length > 1) peerText += "  +" + String(waitingOn.length - 1);
        // Waiting leads when a session is on both sides of the board — the rule the API already
        // states, since `coordination.state` is `waiting_on_session` whenever `waitingOn` is not
        // empty. The owed count still goes on the end rather than being dropped: a session that
        // waits on one peer while another waits on it is exactly the row this is about.
        if (owedWait) peerText += "  ·  " + owedSaid;
        peerTitle = waitingOn.map(function (wait) {
            return [wait.repository, (wait.paths || []).join(", "), wait.releaseCondition]
                .filter(Boolean).join(" · ");
        }).join("\n");
    } else if (owedWait) {
        peerText = owedSaid + " · " + (owedWait.releaseCondition || "release");
        // Who is parked on this session, one per line, because the row only has room for the
        // number. Names where this Mac can still see the tab, ids where it cannot — an
        // unresolved relationship is the one most worth being able to read.
        peerTitle = waitedOnBy.map(function (wait) {
            return [wait.waiterLabel || wait.waiterSessionId, (wait.paths || []).join(", "),
                wait.reason].filter(Boolean).join(" · ");
        }).join("\n");
    }
    var peerSaid = peerText ? '<span class="coordination-wait" title="' +
        esc(peerTitle) + '">' + sessionStatusGlyphHTML("⏳", peerText) + "</span>" : "";
    // The server sends exactly one closed work_state. Re-project the safety precedence here so
    // a partial/old frame fails closed to readable triage rather than leaving an ambiguous gap.
    var work = projectSessionWorkState(s);
    if (work.state === "waiting_session" && !peerSaid) {
        var liveRoots = roots.filter(taskLive);
        if (liveRoots.length) {
            var childWait = T.webTaskTasks + ": " + liveRoots.map(function (task) {
                return task.title || task.id;
            }).join(" · ");
            peerSaid = '<span class="coordination-wait" title="' + esc(childWait) + '">' +
                sessionStatusGlyphHTML("⏳", childWait) + "</span>";
        }
    }
    var workSaid = sessionWorkStateHTML(s);
    // The fourth projection follows the complete third-axis group, never inside it. A row can
    // be delivered and still await its own close attestation, so the key remains visible without
    // splitting the receipt's check from its explanatory words.
    var closeable = projectSessionCloseability(s);
    if (closeable.block) workSaid += sessionCloseabilityHTML(s);
    var waitShape = waitingOn.map(function (wait) {
        return [wait.id || "wait", wait.ownerLabel || wait.ownerSessionId || "",
            wait.releaseCondition || ""].join(":");
    }).concat(waitedOnBy.map(function (wait) {
        // The owner's half belongs in the redraw key too. A row that gains a waiter while the
        // page is open and keeps the shape it had is a row that never says so — which is the
        // same bug as the one above, one layer down.
        return ["owed", wait.id || "wait", wait.waiterLabel || wait.waiterSessionId || "",
            wait.releaseCondition || ""].join(":");
    })).concat(roots.filter(taskLive).map(function (task) {
        return ["child", task.id || "", task.title || ""].join(":");
    })).join("+");
    var shape = kind + "-" + s.state + "+ws" + work.state +
        (closeable.block ? "+cl" + sessionCloseabilityShape(s) : "") +
        (shells ? "+sh" + shells : "") + (waitShape ? "+cw" + waitShape : "");
    if (state.dataset.shape !== shape) {
        state.dataset.shape = shape;
        if (kind === "closing") {
            state.innerHTML = '<canvas class="spin"></canvas><span class="line">' +
                esc(T.webClosing) + "</span>";
        } else if (pending) {
            state.innerHTML = (s.state === "waiting"
                ? '<span class="wants">' + sessionStatusGlyphHTML("🙋", T.sessionWaiting) + "</span>"
                : "") + '<canvas class="spin"></canvas><span class="line">' +
                esc(T.webPending) + "</span>";
        } else if (work.state === "waiting_you") {
            // 🙋 someone is asking and stopped, waiting on you — the one state whose whole
            // meaning is "act now". The owed badge still rides along in workSaid: answering
            // the question on screen does not pay an older debt.
            state.innerHTML = '<span class="wants">' +
                sessionStatusGlyphHTML("🙋", T.sessionWaiting) + "</span>" +
                peerSaid + workSaid + shellsSaid;
        } else if (work.state === "working") {
            // The shells go after the live line rather than instead of it. A session can be
            // working on one thing and still have a build it started three turns ago going.
            state.innerHTML = '<canvas class="spin"></canvas><span class="line"></span>' +
                peerSaid + workSaid + shellsSaid;
        } else if (work.state === "unknown" && s.state === "unknown") {
            // Not silence — a screen that could not be read is a different fact from "idle",
            // and drawing it as idle would be a confident wrong answer about someone's work.
            state.innerHTML = '<span class="unread">' + esc(T.webStateUnreadable) + "</span>" +
                peerSaid + workSaid + shellsSaid;
        } else {
            // **Idle is the case this line exists for.** The turn ended, the terminal is showing
            // a prompt, and a command it started is still going — which is exactly the row that
            // used to say nothing at all and therefore read as finished.
            state.innerHTML = peerSaid + workSaid + shellsSaid;
        }
    }
    if (kind === "pending" || work.state === "working" || kind === "closing") {
        state.querySelector(".line").textContent = pending ? T.webPending :
            (kind === "closing" ? T.webClosing : (s.line || ""));
        var canvas = state.querySelector(".spin");
        drawSpinner(canvas, spinPhase);
        spinners.push(canvas);
    }
}
