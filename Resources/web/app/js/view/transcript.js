import { atMac } from "../core/env.js";
import { Diagnostics } from "../core/layout-diagnostics.js";
import { esc } from "../core/esc.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { ASK_MARK, clockOf, shortPath, tint } from "../core/util.js";
import { assistantLogo, assistantName, drawIcon, drawSpinner, optimisticSpinners, setOptimisticSpinners, spinPhase } from "../core/pixels.js";
import { byId, taskOfChild, taskWord } from "./derive.js";
import { closingID } from "./list.js";
import { copyCodeBlock, inlineMd, richText } from "./markdown.js";
import { Optimistic, Waits, listUnknown, txSkeleton } from "./waits.js";
import { agentTokens } from "../session/agent.js";
import { SessionActions } from "../input/detail-actions.js";
import { coordinatorForSession } from "../input/coordinator-actions.js";
import { GitPanel } from "../input/git-panel.js";
import { ShellPanel } from "../input/shell-panel.js";
import { connectArtifactTile, createImageLightbox } from "./transcript-images.js";

/* ---- the transcript ------------------------------------------------------ */

var artifactRenderQueue = [];
var imageLightbox = createImageLightbox(
    els["image-lightbox"], els["image-lightbox-image"],
    els["image-lightbox-close"], document);

export function renderDetailHead() {
    var s = S.openId ? byId(S.openId) : null;
    var ending = !!(s && closingID === s.id);
    els["detail-head"].dataset.closing = ending ? "on" : "off";
    els.back.disabled = ending;
    // Blank, not "No session open", while the list is still on its way — see `listUnknown`. The
    // header is the third thing that used to announce an absence and then fill in a name.
    els["detail-name"].textContent = s ? (s.label || s.tty || s.id)
        : (listUnknown() ? "" : T.webNoSessionOpen);
    els["detail-name"].style.color = s && s.icon ? tint(s.icon.accent) : "";
    els["detail-clawdfather-crown"].hidden = !coordinatorForSession(s);
    var sub = [];
    if (s) {
        sub.push(shortPath(s.cwd));
        if (s.tty) sub.push(s.tty);
        if (s.state === "waiting") sub.push(T.sessionWaiting);
        else if (s.state === "working") sub.push(T.webStateWorking);
        else if (s.state === "unknown") sub.push(T.webStateUnreadable);
        // What this session was opened to do, if something opened it. Every known task and not
        // only the fresh ones: the list stops grouping an old task's row because the grouping is
        // about *where a row sits*, and a reader who has this session open has asked about this
        // session — "what was this for" has an answer long after the row has gone back to normal.
        var task = S.tasks.length ? taskOfChild(s.id) : null;
        if (task) {
            if (task.title) sub.push(task.title);
            sub.push(taskWord(task));
            var used = task.usage || {};
            if (used.total) sub.push("↓ " + agentTokens(used.total));
            // Only when there is a figure. Codex is billed by the plan rather than the token, so
            // the Mac sends null rather than a zero, and a "$0.0000" beside real work is a lie
            // that reads as a measurement.
            if (typeof used.costUsd === "number") sub.push("$" + used.costUsd.toFixed(4));
        }
    }
    els["detail-sub"].textContent = sub.join("  ·  ");
    drawIcon(els["detail-mark"], s && s.icon, 5);
    // Nothing to ask the Mac about with no session open. The chip stays on screen: a control
    // that comes and goes with the selection
    // is a header that moves under whoever is reading it.
    els["tx-refresh"].disabled = !s || ending;
    // **Hidden unless this page is being read on the Mac itself.**
    //
    // The button brings a session's terminal to the front over there. Pressed from a phone it
    // does something real and entirely invisible to the person pressing it, which is the
    // definition of a control that should not be on screen — and it is sitting in a two-chip
    // header where the session's own name is what gets truncated to make room for it.
    //
    // The test is where the page was *loaded from*, not the screen width and not whether a
    // keyboard is attached: an iPad with a Magic Keyboard is not at the Mac either, and a narrow
    // window on the Mac still is. Loopback means the browser and the app are the same machine.
    // Reaching it at a LAN address while sitting at the Mac hides a button that would have
    // worked, which is a much smaller mistake than offering one that cannot be seen to work.
    els["tx-focus"].hidden = !atMac();
    els["tx-focus"].disabled = !s || !S.write || ending;
    els["tx-focus"].title = S.write ? T.webShowOnMacTip : T.webShowOnMacOff;
    // The mark and title open the same compact action menu. The Git row is read-only, so the
    // menu remains reachable when sending is off; its mutating rows each keep their own gate.
    els["detail-focus"].disabled = !s || ending;
    els["detail-focus"].title = T.webSessionActions;
    els["detail-focus"].setAttribute("aria-label", T.webSessionActions);
    els["detail-actions-title"].disabled = !s || ending;
    els["detail-actions-title"].title = T.webSessionActions;
    els["detail-actions-title"].setAttribute("aria-label", T.webSessionActions);
    els["session-focus"].disabled = !s || !S.write || ending;
    // Reading needs no write switch; the menu it sits in already does, and the day the menu
    // opens for a read-only device this row is the one that should still work.
    els["session-info"].disabled = !s || ending;
    els["session-git-more"].disabled = !s || ending;
    els["session-git"].disabled = !s || ending;
    els["session-commit"].disabled = !s || !S.write || ending;
    els["session-push"].disabled = !s || !S.write || ending;
    els["session-end"].disabled = !s || !S.write || ending;
    if (!s) { SessionActions.close(); GitPanel.follow(); ShellPanel.follow(); }
}

export function renderTranscript() {
    var box = els.tx;
    Diagnostics.note("transcript.render", {
        open: !!S.openId, agent: !!S.agent, loading: !!(S.agent || S.tx).loading,
        entries: ((S.agent || S.tx).entries || []).length,
        error: !!(S.agent || S.tx).error
    });
    setOptimisticSpinners([]);
    artifactRenderQueue = [];
    // Blank rather than the home screen while the list is still on its way — see `listUnknown`.
    // A pane that says "pick a session" and then opens one on its own is a pane that changed its
    // mind in front of the reader; a pane that is briefly empty is a pane that is loading.
    var home = !S.openId && !listUnknown();
    box.classList.toggle("home", home);
    els["tx-scroll"].classList.toggle("home", home);
    if (!S.openId) {
        box.innerHTML = !home ? "" :
            '<section class="home-hero" aria-labelledby="home-hero-title">' +
            '<div class="copy"><span class="rule" aria-hidden="true"></span>' +
            '<h1 id="home-hero-title">' + esc(T.webNoSessionOpen) + '</h1>' +
            '<p>' + esc(T.webPickSession) + '</p></div></section>';
        return;
    }
    // The speaker belongs to the open session. Keeping this beside the transcript render means a
    // Codex answer cannot inherit the page's historical Claude default while sessions switch.
    var session = byId(S.openId);
    WHO.assistant = assistantName(session && session.assistant);
    // One pane, one of two conversations in it: the session's, or one of the agents it sent
    // away. Everything below this line is the same either way — the same blocks, the same folds,
    // the same reading order — because an agent's transcript is the same kind of record.
    var view = S.agent || S.tx;
    // The merge is for drawing only. A quiet fetch replaces `S.tx`, while this separate tail
    // survives until that fetch contains the corresponding real entry. Agent files never get a
    // tail: they are read-only conversations and have no composer to originate one.
    var viewEntries = view.entries.slice();
    if (!S.agent) viewEntries = viewEntries.concat(Optimistic.entries(S.openId));
    if (view.error && !viewEntries.length) { box.innerHTML = '<div class="tx-note err">' + esc(view.error) + "</div>"; return; }
    var transcriptNotice = view.error
        ? '<div class="tx-note err">' + esc(view.error) + "</div>"
        : "";
    if (view.loading && !viewEntries.length) {
        // Nothing at all for the first breath, and then the skeleton — see `Waits`. A pane that
        // shows a placeholder for eighty milliseconds and takes it away again reads as a fault.
        box.innerHTML = Waits.tx.visible ? txSkeleton() : "";
        return;
    }
    if (!viewEntries.length) {
        // An agent with nothing in its file is not the same absence as a session with nothing in
        // its file, and it is common: the first second of every agent's life looks like this.
        box.innerHTML = '<div class="tx-note">' + esc(S.agent ? T.agentEmpty : T.noOutput) + "</div>";
        return;
    }

    // Built a block at a time rather than an entry at a time, the way `Transcript.render` builds
    // it: one message, or one whole run of tool calls, per block. Reversing is then the order the
    // blocks go out in, and a run stays one thing whichever way round the transcript is read.
    var entries = viewEntries.filter(worthDrawing);
    var blocks = [];
    var liveRun = null;    // the last run of tool calls, and where its block landed
    var liveAt = -1;
    var i = 0;
    while (i < entries.length) {
        if (entries[i].role !== "tool") { blocks.push(entryHTML(entries[i])); i += 1; continue; }
        // A question is its own block and breaks the run around it. Everything else a tool does
        // is machinery that folds away; this one is a sentence somebody was asked, and it is the
        // only thing in the pane that was ever addressed to the reader.
        if (askOf(entries[i])) { blocks.push(askHTML(entries[i])); i += 1; continue; }
        var run = [];
        while (i < entries.length && entries[i].role === "tool" && !askOf(entries[i])) {
            run.push(entries[i]); i += 1;
        }
        liveRun = run; liveAt = blocks.length;
        blocks.push(runHTML(run));
    }
    // The run at the end of a session that is still working is the one happening right now: its
    // count is still climbing and the call at the end of it has not come back yet. Drawn a
    // second time with the sweep on it, so the part of the page that is moving is the part of
    // the work that is moving. It has to be the *last* block — a run with an answer written
    // under it is finished, whatever the session is doing now.
    if (liveRun && liveAt === blocks.length - 1 && transcriptWorking()) {
        blocks[liveAt] = runHTML(liveRun, true);
    }
    if (S.newestFirst) blocks.reverse();
    box.innerHTML = transcriptNotice + blocks.join("");
    hydrateArtifactImages(box);
    var pending = box.querySelectorAll(".entry.pending canvas.spin");
    for (var p = 0; p < pending.length; p++) {
        drawSpinner(pending[p], spinPhase);
        optimisticSpinners.push(pending[p]);
    }
    // Said out loud so the keyboard-bar guard above can put the new buttons back out of the tab
    // order. A custom event rather than a direct call: this function is the transcript's, and
    // what somebody else needs to do afterwards is their business, not a line in here.
    document.dispatchEvent(new CustomEvent("clawdline:rendered"));
}

/**
 * Whether what is on screen is still being written — the session's own state while its
 * transcript is open, and the agent's while one of its agents is being read. It is the whole
 * test behind the sweep on the last run: a transcript that has stopped has nothing in flight,
 * however recently it stopped.
 */
function transcriptWorking() {
    if (S.agent) return !!(S.agent.meta && S.agent.meta.state === "running");
    var s = byId(S.openId);
    return !!(s && s.state === "working");
}

/**
 * Whether an entry is worth a place on the screen at all.
 *
 * The wire files a tool call and the first line of what it returned under the same role, and
 * that first line is whatever the tool happened to print first. For a grep over Swift that is
 * regularly `///` — a comment marker, matched, returned, and drawn here as a turn of its own
 * with a timestamp beside it. It is not a small thing to skip: on a phone it is a whole block,
 * indistinguishable at a glance from something somebody said.
 *
 * The test is deliberately narrow. **Anything with a letter or a digit in it stays**, so a result
 * of `0`, `nil` or `[]` — all of which mean something — is drawn. What goes is punctuation on
 * its own, which cannot mean anything without the line it was cut from.
 */
function worthDrawing(e) {
    if (!e) return false;
    if (e.role !== "tool") return true;      // prose is never dropped, whatever it says
    if (e.tool) return true;                 // a call is a step, even one with no arguments
    var text = String(e.text == null ? "" : e.text).trim();
    return text.length > 0 && /[\p{L}\p{N}]/u.test(text);
}

/**
 * A run of consecutive tool entries, folded the way the bar folds one.
 *
 * The machinery is what makes a transcript unreadable: a single answer can sit under thirty
 * lines of paths and shell, and the two sentences worth reading are somewhere past them. So a
 * run comes back as one line saying how much it stands for, and opens when it is asked to.
 *
 * Which run is which comes from `Transcript.render`, not from a rule invented here: the wire
 * files a tool call and what it returned under the same role, and a run is every one of them in
 * a row. Only the calls are counted, because a call is the step — the one entry with a name on
 * it. Two of them is where folding starts to pay: a line reading "1 step" is longer than the
 * step it hid.
 *
 * **The run at the end folds like any other, and that is a change.** The bar exempts it, on the
 * grounds that it is the one still happening — which is right on a Mac, where the pane is wide
 * and the exemption costs two lines. On a phone it is the entire complaint: while an agent is
 * working, every new call lands in the tail, so the tail *is* the screen, and what a reader gets
 * is six greps where the sentence they were reading used to be. A count that climbs says the
 * same thing about a live run and says it in one line — and the names beside it say what kind of
 * work it is, which thirty wrapped command lines never quite manage to.
 */
function runHTML(run, live) {
    var names = [];
    run.forEach(function (e) { if (e.tool) names.push(e.tool); });
    var key = foldKey(run);
    var last = run.length - 1;
    // `live` marks the one call that has not come back yet, which is the last one in the run.
    // Written out rather than `run.map(toolRowHTML)`, which would hand `map`'s index in as the
    // flag and light every row up.
    function rows(lit) {
        return run.map(function (e, n) { return toolRowHTML(e, lit && n === last); }).join("");
    }
    if (names.length < 2) return rows(live);
    var open = !!S.expanded[key];
    // Folded, the pill is the only thing standing for the call in flight, so the sweep goes on
    // the pill; opened, it goes on the row that is actually running.
    return foldHTML(key, names, open, live && !open) + (open ? rows(live) : "");
}

/* --------------------------------------------------------------------------
   A question Claude stopped to ask
   ------------------------------------------------------------------------ */

/**
 * The questions in an entry, or null when it is an ordinary tool call.
 *
 * A marked entry that will not parse comes back as an **empty array, not null** — which still
 * routes it into the block below. That is the whole point of a marker: whatever else goes
 * wrong, a control character followed by JSON is never put on somebody's screen.
 *
 * Remembered on the entry, because this runs for every entry on every render and the answer is a
 * property of text that does not change.
 */
function askOf(e) {
    if (!e || e.role !== "tool" || !e.tool) return null;
    if (e.ask === undefined) e.ask = parseAsk(e.text);
    return e.ask;
}

function parseAsk(text) {
    var s = String(text == null ? "" : text);
    if (s.slice(0, ASK_MARK.length) !== ASK_MARK) return null;
    var rows = null;
    try { rows = JSON.parse(s.slice(ASK_MARK.length)); } catch (err) { rows = null; }
    if (!rows || !rows.length) return [];

    var out = [];
    for (var i = 0; i < rows.length; i++) {
        var row = rows[i] || {};
        var options = [];
        var list = row.o && row.o.length ? row.o : [];
        for (var j = 0; j < list.length; j++) {
            var o = list[j] || {};
            if (typeof o.l !== "string" || !o.l) continue;
            options.push({ label: o.l, note: typeof o.d === "string" ? o.d : "" });
        }
        out.push({
            header: typeof row.h === "string" ? row.h : "",
            text: typeof row.q === "string" ? row.q : "",
            multi: row.m === true,
            options: options
        });
    }
    return out;
}

/**
 * A question, drawn in full and never folded.
 *
 * **Every option gets a row of its own.** The whole complaint this answers is that a decision
 * somebody was asked to make was invisible on a phone — folded into a run of tool calls with
 * nothing but the word `AskUserQuestion` showing — and a folded question is the same bug with a
 * nicer border on it. So there is no fold key here and no handle: it is open because it is the
 * one thing in the pane addressed to the person reading it.
 *
 * The numbers are the terminal's own. Option 3 here is what `3` selects in the picker on the
 * Mac, which is worth having in front of somebody who is about to walk over to it.
 *
 * Nothing here is pressable, and that is deliberate rather than unfinished: see the note above
 * `renderWaiting` for what a press would have to send and why this page cannot send it.
 */
function askHTML(e) {
    var questions = askOf(e) || [];
    var body = questions.map(function (q) {
        var head = "";
        if (q.header) head += '<div class="askhead">' + esc(q.header) + "</div>";
        if (q.multi) head += '<div class="askany">' + esc(T.webAskAny) + "</div>";
        var asked = q.text ? '<div class="askq">' + inlineMd(q.text) + "</div>" : "";
        var options = q.options.map(function (o, n) {
            return '<li class="askopt"><span class="n">' + (n + 1) + "</span>" +
                '<span class="what"><b>' + inlineMd(o.label) + "</b>" +
                (o.note ? '<span class="note">' + inlineMd(o.note) + "</span>" : "") +
                "</span></li>";
        }).join("");
        return head + asked + (options ? '<ol class="askopts">' + options + "</ol>" : "");
    }).join("");

    return '<div class="entry ask" data-role="ask">' +
        whoHTML("assistant", e.at) +
        '<div class="body"><div class="askbox">' +
        '<div class="asktag">' + esc(T.webAskLabel) + "</div>" + body +
        "</div></div></div>";
}

/**
 * One tool call, or one thing a tool said back, as a single line that opens when it is pressed.
 *
 * A call's arguments are a shell command, a path, a regular expression — text with no sentence in
 * it, written to be run rather than read. Drawn as prose it wraps to four lines and takes a
 * quarter of a phone screen to say "it ran a grep". So the first line of it goes in a row that
 * cannot wrap, with the tool's name in front, and the whole of it is one press away.
 *
 * The key is the entry's own content hash with a letter in front, so it cannot collide with the
 * key of the run it sits inside — `S.expanded` is one map and both live in it.
 */
function toolRowHTML(e, live) {
    var key = "e" + foldKey([e]);
    var open = !!S.expanded[key];
    var head = e.tool
        ? '<span class="toolname">' + esc(e.tool) + "</span>"
        : '<span class="caret">' + (open ? "⏷" : "⏵") + "</span>";
    // No timestamp on these. A time beside every grep is a column of numbers nobody reads, and
    // the turn above it is already stamped.
    return '<div class="entry toolrow" data-role="tool"' + (live ? ' data-live="1"' : "") + ">" +
        '<div class="who">' + esc(WHO.tool) + "</div>" +
        '<div class="body">' +
        '<button type="button" class="toolline" data-fold="' + key +
        '" aria-expanded="' + (open ? "true" : "false") + '">' +
        head + '<span class="subject">' + esc(firstLine(e.text)) + "</span></button>" +
        (open ? '<div class="toolbody">' + richText(e.text) + "</div>" : "") +
        "</div></div>";
}

/** The first line of something, with its runs of whitespace closed up. What is left is what fits
 *  on one row; the rest of the truncating is the browser's, which knows how wide the row is. */
function firstLine(text) {
    var line = String(text == null ? "" : text).split("\n")[0].replace(/\s+/g, " ").trim();
    return line || "…";
}

/**
 * The line a folded run leaves behind, and the handle that opens it.
 *
 * A `<button>` rather than a styled `<div>`: the rest of this page can be driven from the
 * keyboard and this may not be the one thing that cannot. It stays put once the run is open —
 * the bar draws no handle over an opened run, but its own click handler toggles both ways, and
 * a control that deletes itself on use leaves the focus standing on nothing.
 */
function foldHTML(key, names, open, live) {
    return '<div class="entry folded" data-role="tool"' + (live ? ' data-live="1"' : "") + ">" +
        '<div class="who">' + esc(WHO.tool) + "</div>" +
        '<div class="body"><button type="button" class="pill" data-fold="' + key +
        '" aria-expanded="' + (open ? "true" : "false") + '">' +
        '<span class="caret">' + (open ? "⏷" : "⏵") + "</span>" +
        // The count is its own element so it cannot be broken across two lines by the names
        // beside it: a pill reading "9" above "steps" is the one part of this that has to be
        // legible at a glance.
        '<span class="steps">' + esc(fill(T.webSteps, { n: names.length })) + "</span>" +
        (open ? "" : '<span class="what">' + esc(distinct(names).join(" · ")) + "</span>") +
        "</button></div></div>";
}

/** Tool names in the order they first ran, without repeats — five greps read as one thing. */
function distinct(names) {
    var seen = {};
    return names.filter(function (n) {
        if (seen[n]) return false;
        seen[n] = true;
        return true;
    });
}

/**
 * Identifies a folded run so the reader's choice to open it survives a re-render.
 *
 * Content-derived rather than positional, for the reason `Transcript.foldKey` gives: this pane
 * re-renders every time the session moves, and an index would slide under the reader and open a
 * different run than the one they clicked. FNV-1a, as there — 32-bit rather than 64 because
 * JavaScript has no 64-bit integer, and the key only has to tell one run in a transcript from
 * the handful of others in the same transcript.
 */
function foldKey(run) {
    var hash = 0x811c9dc5;
    run.forEach(function (e) {
        // The separator is the one the bar puts there, so a name cannot run into the text
        // beside it and hash the same as a different pair that happens to join up the same
        // way. Written as an escape: a raw control character in a source file is something
        // an editor will quietly eat.
        var text = (e.tool || "") + "\u0001" + (e.text || "");
        for (var i = 0; i < text.length; i++) {
            hash = Math.imul(hash ^ text.charCodeAt(i), 0x01000193);
        }
    });
    return (hash >>> 0).toString(36);
}

function toggleFold(key) {
    if (S.expanded[key]) delete S.expanded[key];
    else S.expanded[key] = true;
    renderTranscript();
    // The row the reader pressed was just thrown away and written again, so the focus has to be
    // put back on its replacement — otherwise opening a run with the keyboard is also the moment
    // the keyboard loses its place in the page.
    var again = els.tx.querySelector('[data-fold="' + key + '"]');
    if (again) again.focus({ preventScroll: true });
}

els.tx.addEventListener("click", function (ev) {
    var copy = ev.target.closest ? ev.target.closest("button.codecopy") : null;
    if (copy) { copyCodeBlock(copy.getAttribute("data-code-copy")); return; }
    var handle = ev.target.closest ? ev.target.closest("[data-fold]") : null;
    if (handle) toggleFold(handle.getAttribute("data-fold"));
});

els.tx.addEventListener("keydown", function (ev) {
    if (ev.key !== "Enter" && ev.key !== " ") return;
    // The copy button is here for the same reason a fold is: it is a real button, so the key
    // is already its own — but only if the page's one keyboard handler never sees the press.
    if (!ev.target.closest) return;
    if (!ev.target.closest("[data-fold]") && !ev.target.closest("button.codecopy")) return;
    // Return means "open the selected session" everywhere else on this page, and the handler
    // that does it calls preventDefault — which would stop the button ever seeing the click the
    // browser was about to make out of this key. While a fold has the focus, the key is its own.
    ev.stopPropagation();
});

// User and tool are copy supplied by the server before the first render. Claude, Claude ↔ and
// Clawdline are names, and a name is the same word in fourteen languages. See `paintStatic`.
export var WHO = {
    user: "you", assistant: "claude", peer: "Claude ↔", message: "Clawdline ↔",
    notice: "Clawdline", tool: "tool",
};

/** A transcript speaker, with the current assistant's mark when this browser opted into it. */
function whoHTML(role, at) {
    var mark = "";
    if (role === "assistant" && S.assistantIcons) {
        var session = byId(S.openId);
        mark = assistantLogo(session && session.assistant);
    }
    return '<div class="who"><span class="speaker">' + mark + esc(WHO[role]) + "</span>" +
        (at ? '<time data-at="' + at + '">' + esc(clockOf(at)) + "</time>" : "") + "</div>";
}

export function entryHTML(e) {
    if (e.role === "notice") return noticeHTML(e);
    var role = WHO[e.role] ? e.role : "assistant";
    if (role === "message") {
        var messageSource = String(e.source || "session");
        var sourceAssistant = String(e.sourceAssistant || "");
        var sourceMeta = sourceAssistant
            ? assistantLogo(sourceAssistant) + '<span>' + esc(assistantName(sourceAssistant)) + '</span>'
            : "";
        return '<div class="entry" data-role="message">' +
            whoHTML("message", e.at) +
            '<div class="body"><div class="message-card">' +
            '<div class="message-source"><span>' + esc(messageSource) + '</span>' + sourceMeta + '</div>' +
            '<div>' + richText(e.text) + '</div>' + artifactTilesHTML(e.artifacts) +
            '</div></div></div>';
    }
    if (role === "peer") {
        var source = String(e.source || "session");
        var mode = e.sourceMode ? ' title="' + esc(String(e.sourceMode)) + '"' : "";
        return '<div class="entry" data-role="peer">' +
            whoHTML("peer", e.at) +
            '<div class="body"><div class="peer-card">' +
            '<div class="peer-source"' + mode + '>' + esc(source) + '</div>' +
            '<div>' + richText(e.text) + '</div>' +
            '</div></div></div>';
    }
    var body = (e.tool ? '<span class="toolname">' + esc(e.tool) + "</span>" : "") + richText(e.text);
    if (e.pending) {
        var attached = e.imageCount
            ? '<div class="pending-images">' + esc(fill(e.imageCount === 1
                ? T.webAttachedImage : T.webAttachedImages, { n: e.imageCount })) + "</div>"
            : "";
        body += attached + '<div class="pending-state" role="status"><canvas class="spin"></canvas><span>' +
            esc(T.webPending) + "</span></div>";
    }
    return '<div class="entry' + (e.pending ? ' pending' : '') + '" data-role="' + role + '">' +
        whoHTML(role, e.at) +
        '<div class="body">' + body + "</div></div>";
}

/** Static markup only. Artifact fields go into a private queue and are assigned as DOM
 *  properties after parsing, so an attachment can never add HTML, an action or a URL. */
function artifactTilesHTML(artifacts) {
    if (!Array.isArray(artifacts) || !artifacts.length) return "";
    var tiles = artifacts.map(function (artifact) {
        var slot = artifactRenderQueue.push(artifact) - 1;
        return '<button class="message-image-tile" type="button" disabled ' +
            'data-artifact-slot="' + slot + '" aria-label="' + esc(T.webImagePreview) + '">' +
            '<img class="message-image" alt="" hidden>' +
            '<span class="message-image-state" role="status">' + esc(T.webLoading) + '</span>' +
            '</button>';
    }).join("");
    return '<div class="message-images">' + tiles + '</div>';
}

function hydrateArtifactImages(box) {
    var tiles = box.querySelectorAll("[data-artifact-slot]");
    for (var i = 0; i < tiles.length; i++) {
        var tile = tiles[i];
        var artifact = artifactRenderQueue[Number(tile.dataset.artifactSlot)];
        connectArtifactTile(tile, artifact, {
            loadingLabel: T.webLoading,
            expiredLabel: T.webImageExpired,
            open: function (trigger, src, expire) {
                imageLightbox.open(trigger, src, expire);
            }
        });
    }
}

/**
 * A semantic Clawdline notice. The server has already validated the closed protocol, but this
 * renderer still treats every field as text and chooses every word, class and layout locally.
 * In particular it never sends a notice field through the Markdown renderer and never creates
 * links or actions from payload data.
 */
function noticeHTML(e) {
    var n = e && e.notice;
    if (!n || typeof n.kind !== "string" ||
        ((n.kind === "task_finished" || n.kind === "workspace_overlap") && !n.task)) {
        // A mismatched old/new server, including a task-scoped notice missing its task, remains
        // visible as its original text but cannot acquire notice presentation.
        return entryHTML(Object.assign({}, e, { role: "assistant", notice: null }));
    }
    var task = n.task || {};
    var identity = task.title || task.id || T.webNoticeTask;
    var title = T.webNoticeFinished;
    var tone = "neutral";
    var detail = "";

    if (n.kind === "task_finished") {
        var states = {
            success: [T.webNoticeCompleted, "success"],
            failure: [T.webNoticeFailed, "failure"],
            timeout: [T.webNoticeTimedOut, "timeout"],
            cancelled: [T.webNoticeCancelled, "neutral"],
            spawn_failed: [T.webNoticeCouldNotStart, "failure"]
        };
        var state = Object.prototype.hasOwnProperty.call(states, n.state)
            ? states[n.state] : [T.webNoticeFinished, "neutral"];
        title = state[0]; tone = state[1];
        detail = '<div class="notice-task">' + esc(identity) + "</div>";
        if (typeof n.result_path === "string" && n.result_path) {
            detail += '<code class="notice-path">' + esc(n.result_path) + "</code>";
        }
        if (n.audience === "parent" && Number.isSafeInteger(n.outstanding)) {
            var siblings = n.outstanding === 0 ? T.webNoticeNoSiblings : fill(
                n.outstanding === 1 ? T.webNoticeOneSibling : T.webNoticeManySiblings,
                { n: n.outstanding });
            detail += '<div class="notice-meta">' + esc(siblings) + "</div>";
        }
        if (n.claims_released === true && n.child_may_still_write === true) {
            detail += '<div class="notice-warning">' + esc(T.webNoticeClaimsReleased) + "</div>";
        }
    } else if (n.kind === "workspace_overlap") {
        title = T.webNoticeWorkspaceOverlap; tone = "overlap";
        detail = '<div class="notice-task">' + esc(identity) + "</div>";
        var rows = Array.isArray(n.overlaps) ? n.overlaps : [];
        detail += '<ul class="notice-overlaps">' + rows.map(function (row) {
            var other = row && row.task || {};
            var name = other.title || other.id || T.webNoticeTask;
            var path = row && typeof row.path === "string" ? row.path : "";
            return "<li><span>" + esc(name) + "</span>" +
                (path ? '<code class="notice-path">' + esc(path) + "</code>" : "") + "</li>";
        }).join("") + "</ul>";
    } else if (n.kind === "file_wait_request") {
        title = T.webNoticeFileWaitRequested; tone = "overlap";
        detail = '<code class="notice-path">' + esc(String(n.repository || "")) + "</code>";
        var waitPaths = Array.isArray(n.paths) ? n.paths : [];
        detail += '<ul class="notice-overlaps">' + waitPaths.map(function (path) {
            return '<li><code class="notice-path">' + esc(String(path)) + "</code></li>";
        }).join("") + "</ul>";
        if (typeof n.reason === "string" && n.reason) {
            detail += '<div class="notice-meta">' + esc(n.reason) + "</div>";
        }
        if (typeof n.release_condition === "string" && n.release_condition) {
            detail += '<div class="notice-warning">' + esc(n.release_condition) + "</div>";
        }
    } else if (n.kind === "file_wait_release") {
        title = T.webNoticeFileWaitReleased; tone = "success";
        detail = '<code class="notice-path">' + esc(String(n.repository || "")) + "</code>";
        var releasedPaths = Array.isArray(n.paths) ? n.paths : [];
        detail += '<ul class="notice-overlaps">' + releasedPaths.map(function (path) {
            return '<li><code class="notice-path">' + esc(String(path)) + "</code></li>";
        }).join("") + "</ul>";
        if (typeof n.commit === "string" && n.commit) {
            detail += '<code class="notice-path">' + esc(n.commit) + "</code>";
        }
        if (typeof n.note === "string" && n.note) {
            detail += '<div class="notice-meta">' + esc(n.note) + "</div>";
        }
        detail += '<div class="notice-warning">' + esc(T.webNoticeRecheckGit) + "</div>";
    } else if (n.kind === "handoff_receipt") {
        var pickedUp = n.state === "picked_up";
        title = pickedUp ? T.webNoticeHandoffPickedUp : T.webNoticeHandoffNeedsDelivery;
        tone = pickedUp ? "success" : "failure";
        var handoffIdentity = n.title || n.handoff_id || T.webNoticeTask;
        detail = '<div class="notice-task">' + esc(String(handoffIdentity)) + "</div>";
        if (typeof n.assistant === "string" && n.assistant) {
            detail += '<div class="notice-meta">' + esc(n.assistant) + "</div>";
        }
        if (typeof n.project_dir === "string" && n.project_dir) {
            detail += '<code class="notice-path">' + esc(n.project_dir) + "</code>";
        }
    } else {
        // Unknown kinds cannot arrive from this protocol version, but visible fallback is safer
        // than an empty card if client and server code ever get out of step.
        return entryHTML(Object.assign({}, e, { role: "assistant", notice: null }));
    }

    return '<div class="entry clawdline-notice" data-role="notice" data-tone="' + tone + '">' +
        whoHTML("notice", e.at) + '<div class="body"><div class="notice-card">' +
        '<div class="notice-title">' + esc(title) + "</div>" + detail +
        "</div></div></div>";
}
