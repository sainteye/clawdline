import { hasKeyboard } from "../core/env.js";
import { esc } from "../core/esc.js";
import { T, fill, words } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { drawSpinner, setLiveSpin, spinPhase } from "../core/pixels.js";
import { api } from "../net/api.js";
import { byId } from "./derive.js";
import { closingID } from "./list.js";
import { Shots } from "../input/shots.js";
import { msgText, sending } from "../input/composer.js";

export function renderComposer() {
    var on = S.write && !!S.openId && closingID !== S.openId;
    var session = S.openId ? byId(S.openId) : null;
    var placeholder = T.placeholder;
    if (session && session.assistant === "codex") {
        placeholder = placeholder.replace("Claude Code", "Codex").replace("Claude", "Codex");
        if (placeholder === T.placeholder) placeholder = "Codex…";
    }
    els.msg.setAttribute("data-placeholder", placeholder);
    els.msg.setAttribute("aria-label", placeholder);
    els["skill-menu"].setAttribute("aria-label",
        (session && session.assistant === "codex" ? "Codex" : "Claude Code") + " skills");
    els.composer.dataset.write = S.write ? "on" : "off";
    els.composer.dataset.sending = sending ? "on" : "off";
    els.composer.dataset.closing = closingID === S.openId ? "on" : "off";
    // Editable only when there is somewhere for the words to go. Not switched off while a send
    // is in flight, though — see the `beforeinput` guard below: taking the editability away from
    // a focused element takes the focus with it, and on a phone that shuts the keyboard between
    // every message.
    els.msg.contentEditable = on ? "plaintext-only" : "false";
    // What Return does differs by machine, and the soft keyboard's own key should say which.
    els.msg.setAttribute("enterkeyhint", hasKeyboard() ? "send" : "enter");
    // A picture on its own is a message. The server takes text, images, or both, and refuses
    // only the one that is neither — so the button follows the same rule.
    els.send.disabled = !on || sending || Shots.busy() || (!msgText() && !Shots.count());
    // **The width is pinned before the word changes.** "Send" and "Sending…" are different
    // lengths in every language, and swapping them mid-press moved the button, which moved the
    // box under it — so the press that started a send also made the thing you pressed jump. The
    // width is measured while it says the shorter word and held while it says the longer one,
    // rather than guessed at with a `min-width` that would be wrong in thirteen languages.
    if (sending) {
        if (sendWidth) els.send.style.minWidth = sendWidth + "px";
        els.send.textContent = T.webSending;
    } else {
        els.send.style.minWidth = "";
        els.send.textContent = T.webSend;
        // Re-measured when the words themselves change, which happens once, when `/v1/strings`
        // lands and the page stops speaking its built-in English.
        if (sendMeasured !== T.webSend) { sendMeasured = T.webSend; sendWidth = 0; }
        if (!sendWidth) sendWidth = els.send.offsetWidth;
    }
    // Return means two different things on the two kinds of machine, so the explanation is only
    // offered where there is a Return to explain. It is hover text and not a row of the screen:
    // it was a permanent line under the box once, and a permanent line to teach something once
    // is what a tooltip is for.
    els.send.title = hasKeyboard() ? T.webSendTip : "";
    els.attach.disabled = !on || sending;
    // Only ever the reason the box will not take what you type. There used to be a line here
    // saying that Return sends and Shift-Return starts a line, and it was true and it was in the
    // way: a row of every screen, forever, to teach something once. When there is nothing to
    // explain this is empty, and an empty one is not drawn at all.
    els.why.innerHTML = S.write
        ? (S.openId ? "" : esc(T.webWriteOpen))
        : words(T.webWriteOff);
}

/**
 * The line above the composer when the open session has stopped to ask something.
 *
 * **What this page can and cannot do about a question, established by trying it.**
 *
 * Claude Code's picker answers to one raw byte: the digit of the option, sent *outside* a
 * bracketed paste, selects and confirms it in one press — and in a multi-select it toggles the
 * row, with Tab then `1` to submit. `ITerm.keystroke` and `Tmux.keystroke` already send exactly
 * that byte, and the bar on the Mac already uses them for Ctrl-V.
 *
 * There is no route to them. The only write this page has is `POST /v1/sessions/<id>/send`,
 * which wraps what it is given in a bracketed paste and follows it with a Return — and a picker
 * **swallows the paste and acts on the Return**. Measured, twice, against a real session: with
 * the caret parked on the third option, sending the word "Tea" answered "Water". So the composer
 * is not a way to type an answer; it is a way to confirm a highlighted row without meaning to,
 * which is the whole reason this notice is loud rather than tidy.
 *
 * Nothing here pretends otherwise. There are no option buttons on this page, because the only
 * press they could make is that one — and a wrong answer to "shall I force-push?" costs more
 * than a missing button. What is offered instead is the true thing: the session is waiting, and
 * here is the way to put it in front of you.
 */
var waitingDrawn = null;

/* An answer that has been sent, and the session that has not caught up with it yet.
 *
 * Pressing an option closes the picker on the Mac before the next reading notices, so for about
 * a second the session is still `waiting` with no menu left to read — and the box would fall back
 * to the two sentences that say answering is only possible on the Mac. Somebody who has just
 * answered from the phone is then told their press did nothing, which is the opposite of what
 * happened. So the options stay on screen, dead, under the line that says the answer went. */
var answeredMenu = null;
var liveDrawn = null;
var agentsDrawn = null;
/// The button's resting width, and the words it was measured against.
var sendWidth = 0;
var sendMeasured = null;

export function renderWaiting() {
    var box = els.waiting;
    if (!box) return;
    var open = S.openId ? byId(S.openId) : null;
    // The options, when the Mac managed to read them. When it did not — a dialog drawn in a
    // shape the parser does not know — the two sentences underneath are what this box has always
    // said, and they are still the honest answer for that case.
    var menu = open && open.state === "waiting" && open.menu ? open.menu : null;
    var rows = menu && menu.options && menu.options.length ? menu.options : null;
    // Given up after ten seconds: if the session is still waiting by then this was not the
    // answer's own gap, and the honest fallback is better than a dead menu nobody can use.
    if (answeredMenu && (!open || answeredMenu.id !== open.id || open.state !== "waiting"
                         || Date.now() - answeredMenu.at > 10000)) {
        answeredMenu = null;
    }
    var sent = !rows && !!answeredMenu;
    if (sent) rows = answeredMenu.rows;
    var want = !open || open.state !== "waiting" ? "" :
        '<div class="title">' + esc(T.webWaitingTitle) + "</div>" +
        (rows
            ? '<div class="say">' + words(sent ? T.webMenuSent : T.webMenuSay) + "</div>"
              + menuHTML(rows, sent)
            : '<div class="say">' + words(T.webWaitingSay) + "</div>" +
              '<div class="say">' + words(T.webWaitingSend) + "</div>") +
        '<button type="button" class="go" data-focus="1">' + esc(T.webShowOnMac) + "</button>";
    // The live line, from the same reading, and **above the early return below** — that return
    // exists because the *notice* rarely changes, and while a session is working the notice is
    // empty every single time, so anything after it ran once and then never again. This line
    // changes about once a second; it is the one thing here that has to be redrawn.
    //
    // Mutually exclusive with the notice by construction: a session is working or it is waiting,
    // never both, so the two never stack.
    var live = els.live;
    if (live) {
        var say = open && open.state === "working" && open.line ? open.line : "";
        if (say !== liveDrawn) {
            liveDrawn = say;
            live.textContent = "";
            setLiveSpin(null);
            if (say) {
                // The same mark the list draws, on the same clock. It replaced a dot that
                // breathed between full and quarter opacity — which said "still going" only to
                // somebody already watching it, and said nothing at a glance.
                var d = document.createElement("canvas"); d.className = "spin";
                var t = document.createElement("span"); t.className = "said";
                t.textContent = say;
                live.appendChild(d); live.appendChild(t);
                setLiveSpin(d);
                drawSpinner(d, spinPhase);
            }
            live.hidden = !say;
        }
    }

    // Written only when it has changed. `render` runs on every beat of the stream, and throwing
    // this away and building it again once a second would take the focus off the button in it
    // every time somebody was about to press it.
    if (want === waitingDrawn) return;
    waitingDrawn = want;
    box.innerHTML = want;
    box.hidden = !want;

    if (!want) return;
    // A new button, drawn while somebody may be writing. Same reason the transcript says it:
    // the keyboard-bar guard has to put it back out of the tab order.
    document.dispatchEvent(new CustomEvent("clawdline:rendered"));
}

/**
 * A menu's options, as buttons.
 *
 * **`n` is the keystroke, not the position.** It is drawn as the number the row carries and sent
 * as that same number, because Claude Code's picker acts on the digit — renumbering these to run
 * 1..n would produce buttons whose label and effect disagree, which is the failure this whole
 * feature exists to stop somebody hitting from another room.
 *
 * A row is offered only when a keystroke can reach it *and* this device may send. Otherwise it is
 * drawn and disabled: the question is still worth reading when you cannot answer it, and a button
 * that looks live and does nothing is worse than a line of text.
 */
function menuHTML(rows, spent) {
    return '<div class="menu">' + rows.map(function (o) {
        var can = !spent && o.can !== false && S.write;
        return '<button type="button" class="opt" data-key="' + esc(String(o.n)) + '"' +
            ' data-can="' + (o.can === false ? "0" : "1") + '"' +
            (o.selected ? ' data-here="1"' : "") + (can ? "" : " disabled") + ">" +
            '<span class="n">' + esc(String(o.n)) + "</span>" +
            '<span class="what">' + esc(o.label || "") +
            (o.selected ? '<span class="here">' + esc(T.webMenuHighlighted) + "</span>" : "") +
            "</span></button>";
    }).join("") + "</div>";
}

els.waiting.addEventListener("click", function (ev) {
    if (!ev.target.closest) return;

    var opt = ev.target.closest("[data-key]");
    if (opt) {
        if (!S.openId || opt.disabled) return;
        // Every option goes dead on the first press, not just the one pressed. They are answers
        // to one question: a second tap while the first is in flight would arrive as a stray
        // keystroke in whatever the session moved on to.
        Array.prototype.forEach.call(els.waiting.querySelectorAll(".opt"), function (b) {
            b.disabled = true;
        });
        // Held so the next render has something to draw when the picker has already gone.
        var asked = byId(S.openId);
        if (asked && asked.menu && asked.menu.options && asked.menu.options.length) {
            answeredMenu = { id: S.openId, rows: asked.menu.options, at: Date.now() };
        }
        api.key(S.openId, opt.dataset.key)
            .then(function () { toast(T.webMenuSent); })
            .catch(function (e) {
                toast(e.message, true);
                // Drawn again from scratch, and the cache has to be cleared to allow it: the
                // markup has not changed, so the guard in `renderWaiting` would keep the dead
                // buttons on screen and leave nothing to press.
                answeredMenu = null;
                waitingDrawn = null;
                renderWaiting();
            });
        return;
    }

    if (!ev.target.closest("[data-focus]")) return;
    if (!S.openId) return;
    api.focus(S.openId).then(function () { toast(T.webShowOnMacAsked); })
        .catch(function (e) { toast(e.message, true); });
});

/**
 * What this session has working somewhere that is not a screen.
 *
 * **The quietest thing in the composer, on purpose.** An agent running is the answer to "why has
 * this been busy for four minutes" — it is context, and it never wants anybody. The one state on
 * this page allowed to be loud is a session waiting for an answer, and a stack of bright rows
 * under it would take that away.
 *
 * A finished agent stays for a few minutes with what it came back with, then goes. The record is
 * the transcript; this is the notice.
 */
export function renderAgents() {
    var box = els.agents;
    if (!box) return;
    var open = S.openId ? byId(S.openId) : null;
    var list = (open && open.agents) || [];
    var running = 0;
    for (var i = 0; i < list.length; i++) if (list[i].state === "running") running++;
    var here = S.agent ? S.agent.id : "";

    var want = !list.length ? "" :
        '<div class="head"><span>' + esc(T.webAgents) + "</span>" +
        (running ? '<span class="n">' + esc(fill(T.webAgentsCount, { n: running })) + "</span>" : "") +
        "</div>" +
        // The root, and a row like the others because it is also the way back out of one of
        // them. `main` is the terminal's own word for the conversation they all hang under, and
        // it is not translated for the same reason `general-purpose` is not: it is the name of a
        // thing in Claude Code, not a sentence about it.
        agentRowHTML({ id: "", type: "main", what: open ? (open.label || open.tty || "") : "",
                       root: true }, here) +
        list.map(function (a) { return agentRowHTML(a, here); }).join("");

    if (want === agentsDrawn) return;
    agentsDrawn = want;
    box.innerHTML = want;
    box.hidden = !want;
}

/**
 * One row of that tree.
 *
 * A button, because every row leads somewhere: an agent's row into its conversation, and the
 * root back out to the session's. The id rides on the element rather than in a closure, so a
 * repaint — which happens every time any agent reaches for a tool — rebinds nothing.
 */
function agentRowHTML(a, here) {
    var root = !!a.root;
    var said = a.state === "done" ? T.webAgentDone
             : a.state === "failed" ? T.webAgentFailed : "";
    // Running: the tool it last reached for. Finished: what it handed back. Same slot, because
    // they are the same question asked at two different times.
    var trailing = root ? "" : (a.state === "running" ? (a.doing || "") : (a.result || ""));
    var current = (a.id || "") === here;
    return '<button class="one' + (root ? " root" : "") + '" type="button"' +
        ' data-agent="' + esc(a.id || "") + '"' +
        (a.state ? ' data-state="' + esc(a.state) + '"' : "") +
        ' aria-current="' + (current ? "true" : "false") + '"' +
        (root ? "" : ' title="' + esc(T.webAgentOpen) + '"') + ">" +
        '<span class="mark"></span>' +
        '<span class="kind">' + esc(a.type || "") + "</span>" +
        '<span class="what">' + esc(a.what || "") + "</span>" +
        (trailing ? '<span class="doing">' + esc(trailing) + "</span>" : "") +
        (said ? '<span class="said">' + esc(said) + "</span>" : "") +
        "</button>";
}
