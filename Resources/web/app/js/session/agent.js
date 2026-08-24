import { phone, reduced } from "../core/env.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { api } from "../net/api.js";
import { byId, ordered } from "../view/derive.js";
import { render, rowNodes } from "../view/list.js";
import { renderTranscript } from "../view/transcript.js";
import { Waits } from "../view/waits.js";
import { atBottom, toBottom } from "./open.js";
import { SkillPicker } from "../input/composer.js";

/* ==========================================================================
   Reading one background agent
   The strip in the composer says a session has work happening somewhere with no screen. This is
   the rest of that sentence: the agent's own conversation, in the pane the session's was in.

   **The session does not close to show it.** `S.openId` stays exactly where it was, so the
   stream keeps the row live, the list keeps its place, and coming back is putting `S.agent`
   down rather than opening anything again. What changes is which header is on screen and which
   list of entries the transcript draws.

   The composer goes away with the header, and that is honesty rather than a shortcut: there is
   nothing to type at an agent. It was given its instructions when it was spawned and it is not
   listening for more. A box that took a sentence and had nowhere to send it would be worse than
   no box at all.
   ========================================================================== */

/** The strip's own row for an agent, which is what the header reads while a fetch is in flight. */
export function agentRow(sid, agentId) {
    var s = byId(sid);
    var list = (s && s.agents) || [];
    for (var i = 0; i < list.length; i++) if (list[i].id === agentId) return list[i];
    return null;
}

/**
 * Everything about a session's agents that would change what is on screen.
 *
 * `at` is the mtime of an agent's own transcript, so this moves every time one writes a line —
 * which is the signal this pane needs and the session's own revision cannot give it: a session
 * sitting still with three agents out reports no change at all while all three are working.
 */
export function agentsRev(s) {
    return ((s && s.agents) || []).map(function (a) {
        return a.id + ":" + a.state + ":" + (a.at || "") + ":" + (a.doing || "");
    }).join("|");
}

export function openAgent(agentId) {
    var sid = S.openId;
    if (!sid || !agentId) return;
    if (S.agent && S.agent.id === agentId) return;
    S.agent = {
        sid: sid, id: agentId, meta: agentRow(sid, agentId),
        entries: [], signature: null, loading: true, error: null
    };
    // Which runs a reader had opened is a place in the session's transcript, not in this one.
    S.expanded = {};
    SkillPicker.close();
    loadAgent(sid, agentId, false);
    // A phone's back gesture already means "out of the thing I just opened", and this is a thing
    // that was just opened. Without this it would mean "out of the session" from in here, which
    // undoes a step the reader never asked to undo.
    if (phone()) {
        try { history.pushState({ view: "agent", id: sid, agent: agentId }, ""); } catch (e) { }
    }
    render();
}

export function closeAgent(silent) {
    if (!S.agent) return;
    S.agent = null;
    S.expanded = {};
    if (silent) return;
    // The session's own entries never left `S.tx`, so going back is a repaint and not a fetch.
    renderTranscript();
    render();
}

export function loadAgent(sid, agentId, quiet) {
    if (!quiet) { Waits.tx.start(); renderTranscript(); }
    api.agent(sid, agentId).then(function (d) {
        var a = S.agent;
        if (!a || a.id !== agentId || a.sid !== sid) { Waits.tx.settle(); return; }
        a.loading = false;
        if (d.agent) a.meta = d.agent;
        // The same bargain the session's transcript strikes: the server's signature answers "is
        // this still the same file", and trusting it is what stops a refetch behind a reader
        // from throwing their scroll position away every second.
        if (d.signature && d.signature === a.signature) {
            Waits.tx.settle(renderAgentHead);
            return;
        }
        var stick = atBottom();
        a.entries = d.entries || [];
        a.signature = d.signature || null;
        Waits.tx.settle(function () {
            renderAgentHead();
            renderTranscript();
            if (stick) toBottom();
        });
    }).catch(function (e) {
        var a = S.agent;
        if (!a || a.id !== agentId || a.sid !== sid) { Waits.tx.settle(); return; }
        a.entries = [];
        a.loading = false;
        a.error = e.message || T.webTranscriptFailed;
        Waits.tx.settle(renderTranscript);
    });
}

/** Seconds the way the terminal says them: `44s`, `1m 50s`. Not translated — it is a clock. */
function agentClock(seconds) {
    var s = Math.max(0, Math.round(seconds));
    return s < 60 ? s + "s" : Math.floor(s / 60) + "m " + (s % 60) + "s";
}

/** And tokens the way the assistant counts them out loud: `840`, `9.3k`, `84.4k`, `120k`. */
export function agentTokens(n) {
    if (n < 1000) return String(n);
    var k = n / 1000;
    return (k < 100 ? k.toFixed(1) : String(Math.round(k))) + "k";
}

/**
 * The agent's header, and the swap that puts it on screen.
 *
 * One header at a time, by hiding the other: two would leave a reader looking at two names with
 * a single transcript underneath and nothing saying which name it belongs to.
 */
export function renderAgentHead() {
    var a = S.agent;
    var on = !!a;
    if (els["detail-head"]) els["detail-head"].hidden = on;
    els["agent-head"].hidden = !on;
    els.composer.hidden = on;
    if (!on) return;

    var m = a.meta || {};
    els["agent-back-label"].textContent = T.agentBack;
    els["agent-name"].textContent = m.what || m.type || a.id;
    // Type first, because it says what kind of thing this was; then the state; then what it
    // cost. Those last three are the numbers the terminal prints when an agent lands and
    // nothing in this app was showing anywhere: how long, how many tokens, how many tools.
    var sub = [];
    if (m.type) sub.push(m.type);
    sub.push(m.state === "done" ? T.webAgentDone
           : m.state === "failed" ? T.webAgentFailed : T.agentRunning);
    if (m.seconds) sub.push(agentClock(m.seconds));
    if (m.tokens) sub.push("↓ " + agentTokens(m.tokens));
    if (m.tools) sub.push(fill(T.agentTools, { n: m.tools }));
    els["agent-sub"].textContent = sub.join("  ·  ");
}

export function select(id) {
    S.selectedId = id;
    Object.keys(rowNodes).forEach(function (rid) {
        rowNodes[rid].classList.toggle("selected", rid === id);
        rowNodes[rid].setAttribute("aria-selected", rid === id ? "true" : "false");
    });
    var node = rowNodes[id];
    if (node) {
        node.focus({ preventScroll: true });
        node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" });
    }
}

export function move(delta) {
    var list = ordered();
    if (!list.length) return;
    var at = -1;
    for (var i = 0; i < list.length; i++) if (list[i].id === S.selectedId) { at = i; break; }
    var next = at < 0 ? (delta > 0 ? 0 : list.length - 1) : Math.min(list.length - 1, Math.max(0, at + delta));
    select(list[next].id);
}
