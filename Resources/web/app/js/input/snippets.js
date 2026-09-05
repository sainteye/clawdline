import { S } from "../core/state.js";
import { T } from "../core/i18n.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { Optimistic } from "../view/waits.js";
import { userMessageEntries } from "../view/user-messages-data.js";
import {
    snippetActions, snippetControls, snippetCreateBody, snippetDraft, snippetDraftFromText,
    snippetDraftProblem, snippetGroups, snippetOrder, snippetOrderBody, snippetPatchBody,
    snippetReorder, snippetScopeSwap, snippetStarters, snippetsListHTML
} from "../view/snippets-data.js";
import { appendMsg } from "./composer.js";
import { SessionActions } from "./detail-actions.js";

/* ==========================================================================
   Snippets — the sheet, and the editor

   Text somebody wrote once, pressed instead of typed again. This view owns its
   small DOM island the way `input/user-messages.js` does: it injects its own
   stylesheet, builds its own two overlays, and inserts its own row into the `⋯`
   menu before the Git row. Everything it decides before writing HTML lives in
   `view/snippets-data.js`, where a Node suite can read it.

   **Pressing a row puts the text in the composer and closes the sheet. It
   never sends.** `input/composer.js:appendMsg` is the same function dictation
   already uses, and the note above it and the one at the top of `input/voice.js`
   say why: words in a box that were misheard, or pressed by a thumb in a
   pocket, are a typo — the same words already posted to a terminal are an
   incident. The send button stays the only thing that sends. That is the whole
   design and not a step left out; `docs/snippets.md` records `send_on_tap` as
   declined rather than postponed.

   **`openSnippets()` is exported and takes no arguments** because it has three
   entrances: the `⋯` row, the project mark in the session header
   (`#detail-snippets`, bound at the bottom of this file), and the editor
   returning to the list. One function called from three places — a header that
   had to know how this sheet is built would be a header that breaks when it
   changes.

   **Every writing control is drawn from `snippetControls`, or not drawn.** The
   relay reads the list out of the published snapshot and has no envelope class
   for a write, so on the Cloud path the `＋`, the row's `⋯` and the editor
   simply are not there; a read-only device is the same question asked of
   `S.write`. Both live in `snippetActions`, once, so the six call sites cannot
   drift apart. The key that makes a retried write land once is minted inside
   `net/live.js`, one per request — where a retry of *that* request is the
   thing it has to be unrepeated against. Nothing in this file mints one,
   which is the same sentence as: nothing in this file sends.
   ========================================================================== */

var style = document.createElement("link");
style.rel = "stylesheet";
style.href = new URL("../../css/snippets.css", import.meta.url).href;
document.head.appendChild(style);

var menu = document.getElementById("session-actions-main");
var before = document.getElementById("session-git-more");
var button = document.createElement("button");
button.id = "session-snippets";
button.type = "button";
button.setAttribute("role", "menuitem");
menu.insertBefore(button, before);

var overlay = document.createElement("div");
overlay.className = "overlay";
overlay.id = "snippets";
overlay.hidden = true;
overlay.innerHTML =
    '<div class="sheet snippets-sheet" id="snippets-sheet" role="dialog" aria-modal="true" ' +
    'aria-labelledby="snippets-title">' +
    '<div class="snippets-head">' +
    '<div class="snippets-heading"><h2 id="snippets-title"></h2>' +
    '<span class="snippets-where" id="snippets-where"></span></div>' +
    '<button class="snippets-new" id="snippets-new" type="button" hidden>＋</button>' +
    "</div>" +
    '<div class="snippet-list" id="snippet-list"></div>' +
    '<p class="said" id="snippets-said" role="status" aria-live="polite"></p>' +
    '<div class="buttons"><button class="chip" id="snippets-close" type="button"></button></div>' +
    "</div>";
document.body.appendChild(overlay);

/* The editor, its own overlay over the list's. Same furniture as `#schedule-form`: labelled
   blocks, a chip row for the one choice it offers, what just happened said where it happened,
   and Delete pushed to the far side of Cancel and Save. */
var editorOverlay = document.createElement("div");
editorOverlay.className = "overlay";
editorOverlay.id = "snippet-editor";
editorOverlay.hidden = true;
editorOverlay.innerHTML =
    '<div class="sheet snippet-editor-sheet" id="snippet-editor-sheet" role="dialog" ' +
    'aria-modal="true" aria-labelledby="snippet-editor-title">' +
    '<h2 id="snippet-editor-title"></h2>' +
    '<div class="block"><span class="field-label" id="snippet-title-label"></span>' +
    '<input class="find" id="snippet-title" type="text" maxlength="60" autocomplete="off" ' +
    'autocapitalize="sentences" spellcheck="false" data-1p-ignore data-lpignore="true" ' +
    "data-bwignore></div>" +
    '<div class="block"><span class="field-label" id="snippet-body-label"></span>' +
    '<textarea class="find" id="snippet-body" rows="6" maxlength="4000" spellcheck="false" ' +
    "data-1p-ignore data-lpignore=\"true\" data-bwignore></textarea>" +
    '<button class="chip snippet-from-last" id="snippet-from-last" type="button" hidden>' +
    "</button></div>" +
    '<div class="block"><span class="field-label" id="snippet-scope-label"></span>' +
    '<div class="row" id="snippet-scope"></div></div>' +
    '<p class="said" id="snippet-said" role="status" aria-live="polite"></p>' +
    '<div class="buttons">' +
    '<button class="chip danger" id="snippet-editor-delete" type="button" hidden></button>' +
    '<button class="chip" id="snippet-editor-cancel" type="button"></button>' +
    '<button class="chip confirm-go" id="snippet-editor-save" type="button"></button>' +
    "</div></div>";
document.body.appendChild(editorOverlay);

var sheet = document.getElementById("snippets-sheet");
var title = document.getElementById("snippets-title");
var where = document.getElementById("snippets-where");
var list = document.getElementById("snippet-list");
var newButton = document.getElementById("snippets-new");
var said = document.getElementById("snippets-said");
var closeButton = document.getElementById("snippets-close");

var editorSheet = document.getElementById("snippet-editor-sheet");
var editorTitle = document.getElementById("snippet-editor-title");
var titleLabel = document.getElementById("snippet-title-label");
var titleField = document.getElementById("snippet-title");
var bodyLabel = document.getElementById("snippet-body-label");
var bodyField = document.getElementById("snippet-body");
var fromLast = document.getElementById("snippet-from-last");
var scopeLabel = document.getElementById("snippet-scope-label");
var scopeRow = document.getElementById("snippet-scope");
var editorSaid = document.getElementById("snippet-said");
var editorDelete = document.getElementById("snippet-editor-delete");
var editorCancel = document.getElementById("snippet-editor-cancel");
var editorSave = document.getElementById("snippet-editor-save");

var sessionID = null;
var shown = [];
var model = null;
var menuFor = -1;
var reading = 0;
var busy = false;

/** The row the editor is changing, and the draft it is changing it into. `editing` is null for
 *  a snippet being made — which is also what tells Save whether to POST or to PATCH. */
var editing = null;
var draft = null;

/** Whether this transport has the route at all. An older Mac, and the idle client the cloud page
 *  runs before a relay is chosen, do not — and a menu row that opens a sheet which can only say
 *  "could not read" is worse than no row. The same question `/v1/places` and `/v1/push/key` are
 *  already asked; the four writing routes are asked about in `snippetControls` for the editor. */
function readable() {
    return snippetControls(api).read;
}

/** What this device and this transport may do, together. One question, asked here and answered
 *  in `view/snippets-data.js`, so the `＋`, the row menu and the editor cannot disagree. */
function may() {
    return snippetActions(snippetControls(api), S.write !== true);
}

function syncCopy() {
    button.textContent = T.webSnippets;
    title.textContent = T.webSnippets;
    closeButton.textContent = T.webClose;
    newButton.setAttribute("aria-label", T.webSnippetNew);
    newButton.title = T.webSnippetNew;
    titleLabel.textContent = T.webSnippetTitleLabel;
    bodyLabel.textContent = T.webSnippetBodyLabel;
    scopeLabel.textContent = T.webSnippetScopeLabel;
    fromLast.textContent = T.webSnippetFromLast;
    editorCancel.textContent = T.webCancel;
    editorSave.textContent = T.webSnippetSave;
    editorDelete.textContent = T.webSnippetDelete;
}

// The language arrives after the modules evaluate, exactly as it does for the sheet next door:
// watching the document's language keeps the menu row from showing the English fallback on a
// page that is about to be Chinese.
new MutationObserver(syncCopy).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["lang"]
});
syncCopy();

/**
 * The row in the `⋯` menu, present only when this transport can read the list.
 *
 * `disabled` as well as `hidden`, because `SessionActions.items()` collects
 * `button:not(:disabled)` for the keyboard: a row that is only hidden is still a stop on the way
 * down the menu with the arrow keys, which is a shortcut to a sheet nobody can see.
 *
 * **Asked when the menu opens, not once at the end of a render.** The transport is chosen after
 * these modules evaluate, and `renderTranscript` returns early — emitting no `clawdline:rendered`
 * — for a session whose transcript could not be read. A row that decided its own existence on
 * that event stayed hidden for exactly those sessions, which is how this was found.
 */
function syncRow() {
    var ok = readable();
    button.hidden = !ok;
    button.disabled = !ok;
}
syncRow();
// Registered after `input/detail-actions.js` has bound its own handler to the same button, so
// the menu is already open by the time this runs — the same tick, so nothing is painted between.
document.getElementById("detail-actions-trigger").addEventListener("click", syncRow);

function projectKey() {
    return model && model.project ? model.project.key : "";
}

/** The group one drawn row belongs to, and where it sits inside it. Reordering is per scope —
 *  `POST /v1/snippets/order` carries the complete order of exactly one — so a press on ↑ has to
 *  find its own group before it can say what the new order is. */
function groupOf(row) {
    var groups = model && Array.isArray(model.groups) ? model.groups : [];
    for (var i = 0; i < groups.length; i++) {
        var rows = Array.isArray(groups[i].rows) ? groups[i].rows : [];
        if (rows.indexOf(row) >= 0) return groups[i];
    }
    return null;
}

function draw(next, options) {
    var opts = options && typeof options === "object" ? options : {};
    model = next;
    shown = snippetOrder(next);
    where.textContent = next && next.project ? next.project.label : "";
    var can = may();
    // The `＋` is the only writing control outside the list, so it is the only one this function
    // has to hide by hand. Everything else is drawn — or not drawn — by `snippetsListHTML`.
    newButton.hidden = !can.create || !!opts.loading || !!opts.error;
    list.innerHTML = snippetsListHTML(next, {
        readOnly: S.write !== true,
        controls: snippetControls(api),
        loading: opts.loading,
        error: opts.error,
        menuFor: menuFor
    });
    if (!opts.keepScroll) list.scrollTop = 0;
}

function say(message) {
    said.textContent = message || "";
}

/**
 * Open it, and read the list.
 *
 * The read is not the sheet's reason to exist — on a Mac the list rides the orchestrator
 * snapshot and comes back in a few milliseconds — but the sheet is shown first either way. A
 * shortcut that spins before it appears is not a shortcut, and a shortcut that appears and then
 * fills is one.
 */
export function openSnippets() {
    if (!S.openId || !readable()) return;
    sessionID = S.openId;
    menuFor = -1;
    say("");
    syncCopy();
    draw(null, { loading: true });
    overlay.hidden = false;
    SessionActions.close();
    closeButton.focus({ preventScroll: true });
    refresh();
}

/** Read the list again and repaint. Every write ends here rather than editing the drawn rows in
 *  place: `position` is the Mac's to assign — a create lands at the end of its scope, a scope
 *  change lands at the end of the other one — and a sheet that guessed those would be showing an
 *  order the next reader does not have. */
function refresh(options) {
    if (!sessionID) return Promise.resolve();
    var opts = options && typeof options === "object" ? options : {};
    var ticket = ++reading;
    var session = byId(sessionID);
    return api.snippets(sessionID).then(function (answer) {
        // Two presses, or a session switched while the first read was out: only the newest one
        // may paint, the same ticket rule the transcript reads under.
        if (ticket !== reading || overlay.hidden) return;
        draw(snippetGroups(answer, {
            machine: session ? session.machine : null,
            project: session ? session.cwd : null
        }), { keepScroll: opts.keepScroll });
    }).catch(function (error) {
        if (ticket !== reading || overlay.hidden) return;
        // The transport's own sentence, unedited — `cloud_snippets_unpublished` says a Mac is
        // running a build older than this page, and no string of ours could say it better.
        draw(null, { error: (error && error.message) || String(error) });
    });
}

export function closeSnippets() {
    if (overlay.hidden) return;
    closeEditor();
    overlay.hidden = true;
    sessionID = null;
    shown = [];
    model = null;
    menuFor = -1;
    say("");
    reading += 1;
}

/** A press: the words into the box, the sheet shut, and nothing else. */
function insert(row) {
    if (!row || S.write !== true) return;
    closeSnippets();
    appendMsg(row.body);
}

/* ---- writing ------------------------------------------------------------- */

/**
 * One write, and what the sheet does around it.
 *
 * Every writing press goes through here so that all of them behave the same way: one at a time
 * (a second ↑ before the first has landed would send an order built from rows the Mac has
 * already moved), the list re-read afterwards, and a refusal shown in the words the Mac used —
 * `snippet_limit_reached` says how many there are, and no string of ours knows that.
 */
function write(work, options) {
    var opts = options && typeof options === "object" ? options : {};
    if (busy) return Promise.resolve(false);
    busy = true;
    say("");
    return work().then(function () {
        busy = false;
        if (opts.thenClose) closeEditor();
        return refresh({ keepScroll: opts.keepScroll }).then(function () { return true; });
    }).catch(function (error) {
        busy = false;
        var message = (error && error.message) || String(error);
        if (editorOverlay.hidden) say(message);
        else editorSaid.textContent = message;
        return false;
    });
}

function remove(row) {
    if (!row || !may().remove) return;
    menuFor = -1;
    write(function () { return api.deleteSnippet(row.id); }, { thenClose: true });
}

function move(row, delta) {
    var can = may();
    if (!row || !can.order) return;
    var group = groupOf(row);
    if (!group) return;
    var moved = snippetReorder(group.rows, row.id, delta);
    if (!moved) return;
    var body = snippetOrderBody(group.scope, projectKey(), moved);
    if (!body) return;
    // The menu stays open on the row that moved, so a second press is the second step of the
    // same journey rather than three presses to reopen a menu that closed itself.
    menuFor = shown.indexOf(row) + delta;
    write(function () {
        return api.orderSnippets(body.scope, body.project || null, body.order);
    }, { keepScroll: true });
}

function swapScope(row) {
    var can = may();
    if (!row || !can.update) return;
    var patch = snippetScopeSwap(row, projectKey());
    if (!patch) return;
    menuFor = -1;
    write(function () { return api.updateSnippet(row.id, patch); });
}

/* ---- the editor ---------------------------------------------------------- */

/** The newest thing this person sent in this session, or "". The 我傳出的訊息 sheet already
 *  computes that list; this asks it rather than walking the transcript a second time, so the
 *  two cannot disagree about which turn "my last message" means. */
function lastSaid() {
    if (!sessionID) return "";
    var mine = userMessageEntries(
        S.tx.id === sessionID ? S.tx.entries : [],
        Optimistic.entries(sessionID)
    );
    return mine.length ? String(mine[0].text || "") : "";
}

function drawScope() {
    var key = projectKey();
    scopeRow.innerHTML = "";
    [{ scope: "project", label: T.webSnippetsThisProject, can: !!key },
     { scope: "global", label: T.webSnippetsEveryProject, can: true }].forEach(function (choice) {
        // A session whose project the Mac did not resolve has no project scope to offer, and a
        // chip that cannot be chosen is not drawn — the same rule the rest of this sheet follows.
        if (!choice.can) return;
        var chip = document.createElement("button");
        chip.type = "button";
        chip.className = "chip" + (draft.scope === choice.scope ? " on" : "");
        chip.textContent = choice.scope === "project" && model && model.project
            ? model.project.label : choice.label;
        chip.setAttribute("data-snippet-scope-choice", choice.scope);
        chip.setAttribute("aria-pressed", draft.scope === choice.scope ? "true" : "false");
        chip.onclick = function () {
            draft.scope = choice.scope;
            draft.project = choice.scope === "project" ? key : "";
            drawScope();
        };
        scopeRow.appendChild(chip);
    });
}

function drawEditor() {
    editorTitle.textContent = editing ? T.webSnippetEditing : T.webSnippetNew;
    titleField.value = draft.title;
    bodyField.value = draft.body;
    // Offered only while there is nothing to overwrite. A button that replaces what somebody has
    // just typed is worse than a button they have to look for.
    fromLast.hidden = !!editing || !!draft.body || !lastSaid();
    editorDelete.hidden = !editing || !may().remove;
    disarmDelete(editorDelete);
    editorSaid.textContent = "";
    drawScope();
}

function openEditor(row, seed) {
    if (!may().create && !row) return;
    editing = row || null;
    draft = snippetDraft(row, seed || { scope: "global" });
    menuFor = -1;
    drawEditor();
    editorOverlay.hidden = false;
    titleField.focus({ preventScroll: true });
}

function closeEditor() {
    if (editorOverlay.hidden) return;
    editorOverlay.hidden = true;
    editing = null;
    draft = null;
    editorSaid.textContent = "";
    if (!overlay.hidden) closeButton.focus({ preventScroll: true });
}

function readEditor() {
    draft.title = titleField.value;
    draft.body = bodyField.value;
    return draft;
}

function save() {
    var can = may();
    var made = readEditor();
    var problem = snippetDraftProblem(made);
    if (problem) {
        editorSaid.textContent = T.webSnippetNeedsText;
        return;
    }
    if (editing) {
        if (!can.update) return;
        var patch = snippetPatchBody(made, editing);
        if (!patch) return;
        // Nothing was changed. The store refuses a patch with no fields in it and is right to;
        // there is nothing to save, so this is a sheet to close rather than a request to make.
        if (!Object.keys(patch).length) { closeEditor(); return; }
        var id = editing.id;
        write(function () { return api.updateSnippet(id, patch); }, { thenClose: true });
        return;
    }
    if (!can.create) return;
    var body = snippetCreateBody(made);
    if (!body) return;
    write(function () { return api.createSnippet(body); }, { thenClose: true });
}

/* Deleting asks once, in the button itself. A confirmation sheet over an editor over a sheet is
   three layers deep on a phone; the second press is the confirmation, and any redraw — closing
   the editor, a refresh, opening another row — puts the word back. */
function disarmDelete(target) {
    if (!target) return;
    target.dataset.armed = "off";
    target.textContent = T.webSnippetDelete;
}

function armDelete(target) {
    if (target.dataset.armed === "on") return true;
    target.dataset.armed = "on";
    target.textContent = T.webSnippetDeleteAsk;
    return false;
}

/* ---- presses ------------------------------------------------------------- */

function rowAt(target, attribute) {
    var at = Number(target.getAttribute(attribute));
    return shown[at];
}

list.addEventListener("click", function (event) {
    var target = event.target.closest ? event.target.closest("button") : null;
    if (!target || !list.contains(target)) return;

    if (target.hasAttribute("data-snippet-starter")) {
        var starter = snippetStarters()[Number(target.getAttribute("data-snippet-starter"))];
        if (starter) openEditor(null, starter);
        return;
    }
    if (target.hasAttribute("data-snippet-more")) {
        var at = Number(target.getAttribute("data-snippet-more"));
        menuFor = menuFor === at ? -1 : at;
        draw(model, { keepScroll: true });
        return;
    }
    if (target.hasAttribute("data-snippet-edit")) {
        openEditor(rowAt(target, "data-snippet-edit"));
        return;
    }
    if (target.hasAttribute("data-snippet-delete")) {
        if (!armDelete(target)) return;
        remove(rowAt(target, "data-snippet-delete"));
        return;
    }
    if (target.hasAttribute("data-snippet-scope")) {
        swapScope(rowAt(target, "data-snippet-scope"));
        return;
    }
    if (target.hasAttribute("data-snippet-up")) {
        move(rowAt(target, "data-snippet-up"), -1);
        return;
    }
    if (target.hasAttribute("data-snippet-down")) {
        move(rowAt(target, "data-snippet-down"), 1);
        return;
    }
    if (target.classList.contains("snippet-row")) insert(rowAt(target, "data-snippet"));
});

newButton.addEventListener("click", function () { openEditor(null, { scope: "global" }); });

fromLast.addEventListener("click", function () {
    var text = lastSaid();
    if (!text) return;
    var seeded = snippetDraftFromText(text);
    draft.title = seeded.title;
    draft.body = seeded.body;
    drawEditor();
});

editorSave.addEventListener("click", save);
editorCancel.addEventListener("click", closeEditor);
editorDelete.addEventListener("click", function () {
    if (!editing) return;
    if (!armDelete(editorDelete)) return;
    remove(editing);
});
// Typing is the answer to "are you sure": an armed Delete goes back to being a Delete.
bodyField.addEventListener("input", function () { disarmDelete(editorDelete); });
titleField.addEventListener("input", function () { disarmDelete(editorDelete); });

button.addEventListener("click", function (event) {
    event.preventDefault();
    event.stopPropagation();
    openSnippets();
});

/* The second entrance, and the reason `openSnippets` takes no arguments: the project mark in
   the session header. `view/transcript.js:renderDetailHead` owns whether that button is enabled,
   what it is called and whether it claims to open a menu at all — this only says what it does.
   Bound here rather than there because this module is what knows the sheet exists. */
var headerMark = document.getElementById("detail-snippets");
headerMark.addEventListener("click", function (event) {
    event.preventDefault();
    event.stopPropagation();
    openSnippets();
});

overlay.addEventListener("click", closeSnippets);
sheet.addEventListener("click", function (event) { event.stopPropagation(); });
closeButton.addEventListener("click", closeSnippets);
overlay.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    closeSnippets();
});

editorOverlay.addEventListener("click", closeEditor);
editorSheet.addEventListener("click", function (event) { event.stopPropagation(); });
editorOverlay.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    closeEditor();
});

// `renderTranscript` fires this after the strings have landed and after every transcript
// refresh. It is what keeps the menu row's word right, keeps the row itself in step with a
// transport that was chosen after these modules evaluated, and closes a sheet whose session went
// away underneath it.
document.addEventListener("clawdline:rendered", function () {
    syncCopy();
    syncRow();
    if (overlay.hidden) return;
    if (!S.openId || S.openId !== sessionID) closeSnippets();
});
