import { hasKeyboard } from "../core/env.js";
import { appendGap, appendedText } from "../core/compose-text.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { closingID } from "../view/list.js";
import { renderDetailHead, renderTranscript } from "../view/transcript.js";
import { renderComposer } from "../view/composer.js";
import { Optimistic } from "../view/waits.js";
import { authoritativeSendTime, optimisticSendSnapshot } from "../view/optimistic-data.js";
import { atBottom, closeDetail, loadTranscript, toBottom } from "../session/open.js";
import { carriesPicture, Shots } from "./shots.js";
import { Voice } from "./voice.js";

/* ---- the composer -------------------------------------------------------- */

/**
 * What is in the box, as text.
 *
 * `innerText` and never `textContent`: the second one strips the line breaks out, and being able
 * to write a second line is the entire reason this is a `contenteditable` rather than a field.
 * A non-breaking space becomes an ordinary one on the way out — a browser puts them in on its
 * own, and one hiding in a shell command somebody pasted is a command that will not run.
 */
function rawMsgText() {
    return String(els.msg.innerText || "").replace(/\u00A0/g, " ");
}
export function msgText() { return rawMsgText().trim(); }

/** Whether the placeholder should be showing. See the note on `.composer .msg.blank`. */
function blankness() {
    els.msg.classList.toggle("blank", !msgText());
}

/** The caret at the end of whatever is in the box, for the times the page moves the focus
 *  itself — the start is where it would otherwise land, and nobody wants to type there. */
function caretToEnd() {
    var range = document.createRange();
    range.selectNodeContents(els.msg);
    range.collapse(false);
    var selection = window.getSelection();
    if (!selection) return;
    selection.removeAllRanges();
    selection.addRange(range);
}

/** Text into the box at the caret, keeping the undo stack. `execCommand` is deprecated and is
 *  still the only thing that does that; the range is the fallback for the day it goes. */
function insertText(text) {
    if (!text) return;
    var selection = window.getSelection();
    // Checked only for the ordinary case — a caret rather than a selection, and a run with no
    // newline in it. Then the box is certainly longer afterwards if anything went in, and if it
    // is not, `execCommand` said yes and did nothing, which WebKit has been known to do and
    // which reads from the outside as a Paste that is simply dead. Anything else is left to the
    // answer alone, because "the length did not change" is not proof there when a selection was
    // replaced by something the same size.
    var plain = selection && selection.isCollapsed && text.indexOf("\n") < 0;
    var before = plain ? els.msg.textContent.length : -1;
    try {
        // The browser's own edit, and therefore one that Undo knows about.
        if (document.execCommand && document.execCommand("insertText", false, text)
            && (!plain || els.msg.textContent.length !== before)) return;
    } catch (e) { /* the range, then */ }
    if (!selection || !selection.rangeCount) return;
    var range = selection.getRangeAt(0);
    // Only ever into the message box. A caret that is somewhere else entirely is not an
    // invitation to write this text into whatever was under it.
    if (!els.msg.contains(range.commonAncestorContainer)) return;
    range.deleteContents();
    var node = document.createTextNode(text);
    range.insertNode(node);
    range.setStartAfter(node);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
}

/**
 * Words onto the end of whatever is already in the box.
 *
 * What dictation does with a transcription, and **all** it does with one: the sentence is put
 * where a typed one would have gone and the reader presses Send, or does not. Nothing here
 * sends, and that is a product decision rather than a missing feature — a recogniser that
 * mishears is a typo when the words are sitting in a box and an incident when they have already
 * been posted to a terminal.
 *
 * At the end rather than at the caret. A transcription arrives a second or two after the button
 * was pressed, and where the caret happens to be by then is not an instruction — dropping half
 * a spoken sentence into the middle of a typed one is the kind of surprise nobody can undo in
 * one press. The exception is a box somebody is actually writing in: there the caret is moved
 * to the end first, so the insert goes through `execCommand` and Undo still knows about it.
 */
export function appendMsg(text) {
    var said = String(text || "");
    if (!said) return;
    // Whitespace only is nothing: a box holding the `<br>` a browser left behind must not put a
    // leading space in front of the first thing dictated into it.
    var had = msgText() ? rawMsgText() : "";
    if (document.activeElement === els.msg) {
        caretToEnd();
        insertText(appendGap(had) + said);
    } else {
        els.msg.textContent = appendedText(had, said);
    }
    blankness();
    renderComposer();
    // The box may have been `/rec` a moment ago, with the menu open on it. It is a sentence now.
    SkillPicker.changed();
    // The box scrolls at 140px and a dictated paragraph is longer than that. The end is the part
    // worth seeing: it is what just arrived, and it is where the next word would go.
    els.msg.scrollTop = els.msg.scrollHeight;
}

/**
 * Skills for the open session, fetched only when `/` (or Codex's native `$`) makes them visible.
 *
 * Nothing from SKILL.md is interpreted here. The Mac hands over a name and one safe line of
 * description; choosing one only writes the assistant's real invocation into the box. The
 * assistant remains the one that resolves, authorises and runs it when the ordinary send happens.
 */
export var SkillPicker = (function () {
    var cache = {};                    // session id → catalog
    var loading = {};                  // session id → true
    var matches = [];
    var selected = 0;
    var shown = false;

    function prefix() {
        var session = S.openId ? byId(S.openId) : null;
        return session && session.assistant === "codex" ? "$" : "/";
    }

    function query() {
        var text = rawMsgText();
        var session = S.openId ? byId(S.openId) : null;
        var pattern = session && session.assistant === "codex"
            ? /^[\/$][^\s\/$]*$/ : /^\/[^\s/]*$/;
        return pattern.test(text) ? text.slice(1).toLowerCase() : null;
    }

    function rank(skill, q) {
        var name = String(skill.name || "").toLowerCase();
        if (!q) return 0;
        if (name.indexOf(q) === 0) return 0;
        if (name.split(/[-_:]/).some(function (part) { return part.indexOf(q) === 0; })) return 1;
        if (name.replace(/[-_:]/g, "").indexOf(q.replace(/[-_:]/g, "")) === 0) return 2;
        if (String(skill.description || "").toLowerCase().indexOf(q) >= 0) return 3;
        return null;
    }

    function filtered(items, q) {
        return (items || []).map(function (skill) { return { skill: skill, rank: rank(skill, q) }; })
            .filter(function (row) { return row.rank !== null; })
            .sort(function (a, b) {
                return a.rank - b.rank || String(a.skill.name).localeCompare(String(b.skill.name));
            }).slice(0, 9).map(function (row) { return row.skill; });
    }

    function hide() {
        shown = false; matches = []; selected = 0;
        els["skill-menu"].hidden = true;
        els["skill-menu"].textContent = "";
    }

    function draw(q) {
        matches = filtered(cache[S.openId] || [], q);
        selected = Math.min(selected, Math.max(0, matches.length - 1));
        var menu = els["skill-menu"];
        menu.textContent = "";
        if (!matches.length) { hide(); return; }
        shown = true; menu.hidden = false;
        matches.forEach(function (skill, i) {
            var row = document.createElement("button");
            row.type = "button"; row.className = "skill-option"; row.setAttribute("role", "option");
            row.setAttribute("aria-selected", i === selected ? "true" : "false");
            var command = document.createElement("span");
            command.className = "command"; command.textContent = prefix() + skill.name;
            var description = document.createElement("span");
            description.className = "description"; description.textContent = skill.description || "";
            row.appendChild(command); row.appendChild(description);
            // Keep the soft keyboard open. The click still arrives; only the focus transfer goes.
            row.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
            row.addEventListener("click", function () { selected = i; accept(); });
            menu.appendChild(row);
        });
    }

    function changed() {
        var q = query(), id = S.openId;
        if (q === null || !id) { hide(); return; }
        if (cache[id]) { draw(q); return; }
        hide();
        if (loading[id]) return;
        loading[id] = true;
        api.skills(id).then(function (answer) {
            cache[id] = answer.skills || [];
        }).catch(function () {
            // Autocomplete is a convenience, never a reason the composer should fail. Unknown
            // commands may still be sent and the assistant will give the authoritative answer.
            cache[id] = [];
        }).then(function () {
            delete loading[id];
            if (S.openId === id) changed();
        });
    }

    function move(delta) {
        if (!shown || !matches.length) return false;
        selected = Math.max(0, Math.min(matches.length - 1, selected + delta));
        Array.prototype.forEach.call(els["skill-menu"].children, function (row, i) {
            row.setAttribute("aria-selected", i === selected ? "true" : "false");
            if (i === selected) row.scrollIntoView({ block: "nearest" });
        });
        return true;
    }

    function accept() {
        if (!shown || !matches[selected]) return false;
        els.msg.textContent = prefix() + matches[selected].name + " ";
        blankness(); hide(); caretToEnd(); renderComposer();
        return true;
    }

    return { changed: changed, move: move, accept: accept, close: function () {
        if (!shown) return false; hide(); return true;
    }, visible: function () { return shown; } };
})();

els.msg.addEventListener("input", function () {
    blankness();
    renderComposer();
    SkillPicker.changed();
});

// Nothing goes in while a send is in flight. This rather than switching the editability off,
// which would take the focus and the keyboard with it.
els.msg.addEventListener("beforeinput", function (ev) { if (sending) ev.preventDefault(); });

// Plain text, explicitly, even though `plaintext-only` covers most of it: what a browser does
// with a rich paste is worth being sure about rather than nearly sure about. A pasted picture is
// not text and is left to the document's own handler, which puts it in the attachments.
els.msg.addEventListener("paste", function (ev) {
    var data = ev.clipboardData || window.clipboardData;
    var text = data ? data.getData("text/plain") || "" : "";
    // **A picture goes to the document's handler; anything else with words in it stays here.**
    // The test used to be `files.length`, and a clipboard carries a file beside the text more
    // often than a desk makes it look — much of what is copied out of another app on a phone
    // does — so those pastes were handed to the picture handler, refused as "not a picture",
    // and the words never arrived at all.
    if (carriesPicture(data) || !text) return;
    ev.preventDefault();
    insertText(text);
    blankness();
    renderComposer();
    SkillPicker.changed();
});
els.msg.addEventListener("keydown", function (ev) {
    if (ev.isComposing || ev.keyCode === 229) return;
    if (ev.key === "ArrowDown" && SkillPicker.move(1)) { ev.preventDefault(); return; }
    if (ev.key === "ArrowUp" && SkillPicker.move(-1)) { ev.preventDefault(); return; }
    if (ev.key === "Tab" && SkillPicker.accept()) { ev.preventDefault(); return; }
    if (ev.key === "Escape" && SkillPicker.close()) { ev.preventDefault(); return; }
    if (ev.key !== "Enter" || ev.shiftKey) return;
    // **Not while an input method is mid-word.** Typing Chinese, Japanese or Korean, Return is
    // how you accept the candidate the IME is offering — it is a keystroke aimed at the input
    // method, not at us, and acting on it sends whatever is in the box before the person has
    // finished the word they are writing. `isComposing` is the browser telling us which one this
    // is; keyCode 229 is the same fact from browsers that predate it. This one is checked on
    // both paths, because on a touch screen the candidate picker is the whole of how Chinese is
    // typed and a Return swallowed there is a word lost.
    if (SkillPicker.accept()) { ev.preventDefault(); return; }
    // **On a touch screen Return is a new line and the button is how you send.** The other way
    // round there is no way to write a second line at all — the on-screen keyboard has one
    // Return and it was spending it on sending — which made this box worse than every messaging
    // app on the same phone. Where there is a real keyboard, Return still sends and Shift-Return
    // still breaks the line, because that is the muscle memory there and taking it away would be
    // its own complaint.
    if (!hasKeyboard()) return;
    ev.preventDefault();
    submit();
});
// Pressing a button takes the focus off whatever had it, and what had it is the box somebody is
// typing in — so on a phone every message would close the keyboard and open it again. The click
// still happens; only the focus change is refused.
els.send.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
els.attach.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
els.composer.addEventListener("submit", function (ev) { ev.preventDefault(); submit(); });

/// True from the moment a send starts until its answer comes back.
///
/// The button is disabled while a send is in flight, and that was the whole guard — which does
/// nothing at all for Return, because Return does not go through the button. Sending takes a
/// couple of hundred milliseconds (it is an osascript round trip at the far end), the box is not
/// cleared until the answer arrives, and a second Return inside that window found the same text
/// still sitting there and sent it again. Two identical messages in somebody's session, and
/// nothing on this page had said no.
///
/// The `Idempotency-Key` does not help here and is not supposed to: it exists so a **retried**
/// request is not a second prompt, and these were two requests the page chose to make.
export var sending = false;

function submit() {
    // A picture still shrinking, or a sentence still being transcribed, is part of this message
    // and has not arrived yet. Sending now would post the half of it that happened to be ready
    // and leave the rest to land in an empty box afterwards, looking like the start of the next
    // one. Return goes through here as well as the button, which is the whole reason this guard
    // is in the function rather than on the button alone.
    if (sending || Shots.busy() || Voice.busy()) return;
    if (SkillPicker.accept()) return;
    var text = msgText();
    var pictures = Shots.urls();
    if ((!text && !pictures.length) || !S.openId || S.agent || !S.write || closingID === S.openId) return;
    var sentID = S.openId;
    var stick = atBottom();
    var session = byId(S.openId);
    // A quit line is not a message. Sending it through the ordinary route does make the
    // assistant leave, but the shell (and therefore the iTerm2 tab) immediately drops out of
    // Clawdline's session list, leaving no way for this page to close it afterwards. Join those
    // two steps while the terminal session id is still known. Exact and assistant-specific so a
    // sentence mentioning `/exit`, or Claude being sent Codex's `/quit`, stays an ordinary prompt.
    var quit = session && !pictures.length && text.trim() ===
        (session.assistant === "codex" ? "/quit" : "/exit");
    // Capture this before the POST. The successful answer adds the Mac's accepted time from the
    // same side of the handoff, so a row can land while the terminal round trip is still slow.
    var snapshot = quit ? null : optimisticSendSnapshot(
        S.tx.id === sentID ? S.tx.entries : [], Date.now() / 1000);
    sending = true;
    renderComposer();
    var request = quit ? api.end(sentID) : api.send(sentID, text, pictures);
    request.then(function (answer) {
        // Every child node goes, not just the words: the placeholder is drawn from what
        // `innerText` says, and a `<br>` the browser left behind would keep the box looking
        // like it still had something in it.
        els.msg.textContent = "";
        blankness();
        if (document.activeElement === els.msg) caretToEnd();
        // Cleared here and not before: a send that failed still has its pictures attached, and
        // the reader can press the button again rather than going back to the camera roll.
        Shots.clear();
        if (quit) closeDetail();
        else if (S.openId === sentID) {
            if (!S.agent) {
                Optimistic.add(sentID, text, pictures.length, snapshot.known,
                    authoritativeSendTime(answer, snapshot.startedAt));
                renderTranscript();
                if (stick) toBottom();
            }
            loadTranscript(sentID, true);
        }
    }).catch(function (e) {
        if (e.code === "write_disabled") {
            // The flag and the truth have drifted apart — believe the answer, not the flag.
            S.write = false;
            renderComposer();
            renderDetailHead();
        }
        // The words are still in the box — they were never taken out of it — so the press can
        // simply be repeated. A composer that clears itself and then fails is a composer that
        // ate somebody's message.
        toast(e.message || T.sendFailed, true);
    }).then(function () { sending = false; renderComposer(); });
}
